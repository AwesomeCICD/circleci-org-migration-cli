package syncer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/AwesomeCICD/circleci-org-migration-cli/api/project"
	"github.com/AwesomeCICD/circleci-org-migration-cli/api/rest"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/clog"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
)

// The cutover is the switch from the source org to the destination:
//
//   - pause the source so nothing builds there any more. A source OAuth project
//     is paused reversibly with the v1.1 feature flag drop-all-build-requests (the
//     org-level flag overrides every project); source triggers that are enabled
//     and are not github_oauth triggers (those cannot be disabled through the
//     API) are disabled with PATCH /projects/{id}/triggers/{id} {"disabled":true}.
//     Unfollowing a project is destructive and is never used;
//   - enable the destination triggers whose source counterpart was enabled.
//
// Everything changed is recorded in a state file (mode 0600) BEFORE the change
// is made, so --undo can restore it exactly even after a partial failure.

// cutoverFlag is the v1.1 flag that pauses a project (snake_case; the clients
// convert to kebab on write).
const cutoverFlag = "drop_all_build_requests"

const (
	kindCutoverSourceFlag    = "cutover-source-flag"
	kindCutoverSourceTrigger = "cutover-source-trigger"
	kindCutoverOrgFlag       = "cutover-org-flag"
	kindCutoverDestTrigger   = "cutover-dest-trigger"
)

// TriggerToggler disables or enables a trigger.
type TriggerToggler interface {
	SetTriggerDisabled(ctx context.Context, projectID, triggerID string, disabled bool) error
}

// CutoverSourceClient is the source-org project API the cutover needs (the
// project client, built with the SOURCE token).
type CutoverSourceClient interface {
	TriggerToggler
	GetProject(ctx context.Context, slug string) (*project.Project, error)
	GetV11ProjectFeatureFlags(ctx context.Context, slug string) (map[string]bool, error)
	SetV11ProjectFeatureFlags(ctx context.Context, slug string, flags map[string]bool) error
	ListPipelineDefinitions(ctx context.Context, projectID string) ([]project.PipelineDefinition, error)
	ListTriggers(ctx context.Context, projectID, defID string) ([]project.Trigger, error)
}

// CutoverOrgFlags reads and writes org-level v1.1 feature flags on the source.
type CutoverOrgFlags interface {
	GetFeatureFlags(ctx context.Context, vcsType, orgName string) (map[string]bool, error)
	UpdateFeatureFlags(ctx context.Context, vcsType, orgName string, flags map[string]bool) error
}

// Cutover performs the pause/enable/undo steps.
type Cutover struct {
	// Source and SourceOrg act on the source org (source token). Required for
	// PauseSource / PauseOrg / Undo of source changes.
	Source    CutoverSourceClient
	SourceOrg CutoverOrgFlags
	// Dest is the destination Syncer (destination token): its Org and Projects
	// resolve destination projects and triggers, and EnableBuilds enables them.
	Dest *Syncer
	// DestToggle disables destination triggers on --undo (the destination
	// project client).
	DestToggle TriggerToggler
}

// CutoverOptions selects what a cutover run does.
type CutoverOptions struct {
	Apply       bool
	PauseSource bool
	PauseOrg    bool
	EnableDest  bool
	Undo        bool
	// StatePath is the state file, read at the start and rewritten (0600) after
	// every change when Apply is set.
	StatePath string
}

// CutoverFlagRecord is a project feature flag this tool set, with its prior value.
type CutoverFlagRecord struct {
	Project string `json:"project"`
	Flag    string `json:"flag"`
	Prior   bool   `json:"prior"`
}

// CutoverOrgFlagRecord is an org feature flag this tool set, with its prior value.
type CutoverOrgFlagRecord struct {
	Org   string `json:"org"`
	Flag  string `json:"flag"`
	Prior bool   `json:"prior"`
}

// CutoverTriggerRecord is a trigger this tool disabled (source) or enabled (destination).
type CutoverTriggerRecord struct {
	Project    string `json:"project"`
	ProjectID  string `json:"project_id"`
	Definition string `json:"definition,omitempty"`
	TriggerID  string `json:"trigger_id"`
	Provider   string `json:"provider,omitempty"`
}

// CutoverState is the content of the state file.
type CutoverState struct {
	Version        int                    `json:"version"`
	SourceOrg      string                 `json:"source_org,omitempty"`
	DestOrg        string                 `json:"dest_org,omitempty"`
	UpdatedAt      string                 `json:"updated_at,omitempty"`
	SourceFlags    []CutoverFlagRecord    `json:"source_flags,omitempty"`
	OrgFlag        *CutoverOrgFlagRecord  `json:"org_flag,omitempty"`
	SourceTriggers []CutoverTriggerRecord `json:"source_triggers,omitempty"`
	DestTriggers   []CutoverTriggerRecord `json:"dest_triggers,omitempty"`
}

// LoadCutoverState reads the state file; a missing file is an empty state.
func LoadCutoverState(path string) (*CutoverState, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- operator-chosen state path
	if errors.Is(err, os.ErrNotExist) {
		return &CutoverState{Version: 1}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read cutover state %s: %w", path, err)
	}
	st := &CutoverState{}
	if err := json.Unmarshal(data, st); err != nil {
		return nil, fmt.Errorf("parse cutover state %s: %w", path, err)
	}
	if st.Version == 0 {
		st.Version = 1
	}
	return st, nil
}

// Save writes the state to path with mode 0600.
func (st *CutoverState) Save(path string) error {
	st.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("encode cutover state: %w", err)
	}
	return writeSecretFile(path, append(data, '\n'))
}

// Empty reports whether nothing is recorded.
func (st *CutoverState) Empty() bool {
	return len(st.SourceFlags) == 0 && st.OrgFlag == nil && len(st.SourceTriggers) == 0 && len(st.DestTriggers) == 0
}

func (st *CutoverState) findFlag(slug string) int {
	for i, r := range st.SourceFlags {
		if r.Project == slug {
			return i
		}
	}
	return -1
}

func findTriggerRec(recs []CutoverTriggerRecord, triggerID string) int {
	for i, r := range recs {
		if r.TriggerID == triggerID {
			return i
		}
	}
	return -1
}

func removeTriggerRec(recs []CutoverTriggerRecord, triggerID string) []CutoverTriggerRecord {
	if i := findTriggerRec(recs, triggerID); i >= 0 {
		return append(recs[:i:i], recs[i+1:]...)
	}
	return recs
}

// cutoverRun carries per-run state.
type cutoverRun struct {
	c      *Cutover
	report *Report
	st     *CutoverState
	opts   CutoverOptions
}

// save persists the state (apply only). A failure is recorded as an error
// action and returned so the caller stops before changing anything else.
func (r *cutoverRun) save(target string) error {
	if !r.opts.Apply {
		return nil
	}
	if err := r.st.Save(r.opts.StatePath); err != nil {
		r.report.add("cutover-state", target, "error", fmt.Sprintf("could not save the state file %s: %v — stopping so nothing is changed that --undo could not restore", r.opts.StatePath, err))
		return err
	}
	return nil
}

// Run performs the selected steps and returns the per-item report. A report
// action with status "error" means a step failed (callers exit non-zero); every
// step is idempotent, so re-running after fixing the cause is safe.
func (c *Cutover) Run(ctx context.Context, m *manifest.Manifest, mapping *manifest.Mapping, opts CutoverOptions) (*Report, error) {
	if opts.StatePath == "" {
		return nil, fmt.Errorf("cutover: a state file path is required")
	}
	st, err := LoadCutoverState(opts.StatePath)
	if err != nil {
		return nil, err
	}
	report := &Report{Applied: opts.Apply}
	r := &cutoverRun{c: c, report: report, st: st, opts: opts}

	if opts.Undo {
		return report, r.undo(ctx)
	}
	if m == nil {
		return nil, fmt.Errorf("cutover: a manifest is required")
	}
	if mapping == nil {
		mapping = manifest.IdentityMapping(m.Source.Org.Slug)
	}
	st.SourceOrg = m.Source.Org.Slug
	report.DestOrgSlug = mapping.Org.To
	st.DestOrg = mapping.Org.To

	if opts.PauseSource {
		if err := r.pauseSource(ctx, m); err != nil {
			return report, err
		}
	}
	if opts.PauseOrg {
		if err := r.pauseOrg(ctx, m); err != nil {
			return report, err
		}
	}
	if opts.EnableDest {
		if err := r.enableDest(ctx, m, mapping); err != nil {
			return report, err
		}
	}
	return report, nil
}

// ---- pause the source ------------------------------------------------------

func (r *cutoverRun) pauseSource(ctx context.Context, m *manifest.Manifest) error {
	if r.c.Source == nil {
		return fmt.Errorf("cutover: no source client")
	}
	for _, p := range m.Projects {
		provider, _, _, err := project.SplitSlug(p.Slug)
		if err != nil {
			r.report.add(kindCutoverSourceFlag, p.Slug, "error", fmt.Sprintf("invalid source slug: %v", err))
			continue
		}
		isApp := strings.EqualFold(provider, "circleci")
		if !isApp {
			if err := r.pauseProjectFlag(ctx, p.Slug); err != nil {
				return err
			}
		}
		if err := r.disableSourceTriggers(ctx, p, isApp); err != nil {
			return err
		}
	}
	return nil
}

func (r *cutoverRun) pauseProjectFlag(ctx context.Context, slug string) error {
	target := slug + "/feature_flag:" + cutoverFlag
	cur, err := r.c.Source.GetV11ProjectFeatureFlags(ctx, slug)
	if err != nil {
		r.report.add(kindCutoverSourceFlag, target, "error", fmt.Sprintf("read source flags: %v", err))
		return nil
	}
	have := cur[strings.ReplaceAll(cutoverFlag, "_", "-")]
	idx := r.st.findFlag(slug)

	if have {
		if idx < 0 {
			// Paused before this tool ran: remember that so --undo leaves it paused.
			r.st.SourceFlags = append(r.st.SourceFlags, CutoverFlagRecord{Project: slug, Flag: cutoverFlag, Prior: true})
			if err := r.save(target); err != nil {
				return err
			}
			r.report.add(kindCutoverSourceFlag, target, "exists", "already paused (drop-all-build-requests=true) before this run; --undo will leave it paused")
			return nil
		}
		r.report.add(kindCutoverSourceFlag, target, "exists", "already paused (drop-all-build-requests=true)")
		return nil
	}

	if !r.opts.Apply {
		r.report.add(kindCutoverSourceFlag, target, "set", "would pause: set drop-all-build-requests=true (currently false)")
		return nil
	}
	added := false
	if idx < 0 {
		r.st.SourceFlags = append(r.st.SourceFlags, CutoverFlagRecord{Project: slug, Flag: cutoverFlag, Prior: false})
		added = true
		if err := r.save(target); err != nil {
			return err
		}
	}
	if err := r.c.Source.SetV11ProjectFeatureFlags(ctx, slug, map[string]bool{cutoverFlag: true}); err != nil {
		if added {
			r.st.SourceFlags = r.st.SourceFlags[:len(r.st.SourceFlags)-1]
			if serr := r.save(target); serr != nil {
				return serr
			}
		}
		r.report.add(kindCutoverSourceFlag, target, "error", fmt.Sprintf("set drop-all-build-requests=true: %v", err))
		return nil
	}
	r.report.add(kindCutoverSourceFlag, target, "set", "paused: set drop-all-build-requests=true (was false)")
	return nil
}

// disableSourceTriggers disables the enabled, non-github_oauth triggers of a
// source project. github_oauth triggers cannot be disabled through the API; an
// OAuth project is paused by its drop-all-build-requests flag instead.
func (r *cutoverRun) disableSourceTriggers(ctx context.Context, p manifest.Project, isApp bool) error {
	projectID := p.SourceID
	if projectID == "" && isApp {
		got, err := r.c.Source.GetProject(ctx, p.Slug)
		if err != nil {
			r.report.add(kindCutoverSourceTrigger, p.Slug, "error", fmt.Sprintf("resolve source project id: %v", err))
			return nil
		}
		projectID = got.ID
	}
	if projectID == "" {
		return nil
	}
	defs, err := r.c.Source.ListPipelineDefinitions(ctx, projectID)
	if err != nil {
		if isApp {
			r.report.add(kindCutoverSourceTrigger, p.Slug, "error", fmt.Sprintf("list source pipeline definitions: %v", err))
		} else {
			clog.Debugf("cutover: no pipeline definitions readable for OAuth source project %s: %v", p.Slug, err)
		}
		return nil
	}
	for _, def := range defs {
		trigs, err := r.c.Source.ListTriggers(ctx, projectID, def.ID)
		if err != nil {
			r.report.add(kindCutoverSourceTrigger, p.Slug+"/def:"+def.Name, "error", fmt.Sprintf("list source triggers: %v", err))
			continue
		}
		for _, t := range trigs {
			if t.EventSource.Provider == "github_oauth" {
				continue
			}
			target := p.Slug + "/def:" + def.Name + "/trigger:" + t.ID
			rec := CutoverTriggerRecord{Project: p.Slug, ProjectID: projectID, Definition: def.Name, TriggerID: t.ID, Provider: t.EventSource.Provider}
			if t.Disabled {
				if findTriggerRec(r.st.SourceTriggers, t.ID) >= 0 {
					r.report.add(kindCutoverSourceTrigger, target, "exists", "already disabled by this tool")
				}
				continue
			}
			if !r.opts.Apply {
				r.report.add(kindCutoverSourceTrigger, target, "set", fmt.Sprintf("would disable %s trigger", t.EventSource.Provider))
				continue
			}
			added := findTriggerRec(r.st.SourceTriggers, t.ID) < 0
			if added {
				r.st.SourceTriggers = append(r.st.SourceTriggers, rec)
				if err := r.save(target); err != nil {
					return err
				}
			}
			if err := r.c.Source.SetTriggerDisabled(ctx, projectID, t.ID, true); err != nil {
				if added {
					r.st.SourceTriggers = removeTriggerRec(r.st.SourceTriggers, t.ID)
					if serr := r.save(target); serr != nil {
						return serr
					}
				}
				r.report.add(kindCutoverSourceTrigger, target, "error", fmt.Sprintf("disable %s trigger: %v", t.EventSource.Provider, err))
				continue
			}
			r.report.add(kindCutoverSourceTrigger, target, "set", fmt.Sprintf("disabled %s trigger (was enabled)", t.EventSource.Provider))
		}
	}
	return nil
}

func (r *cutoverRun) pauseOrg(ctx context.Context, m *manifest.Manifest) error {
	target := m.Source.Org.Slug + "/feature_flag:" + cutoverFlag
	if r.c.SourceOrg == nil {
		return fmt.Errorf("cutover: no source org client")
	}
	vcs, name := flagOrgCoords(m.Source.Org.Slug)
	if vcs == "" || name == "" {
		r.report.add(kindCutoverOrgFlag, target, "error", fmt.Sprintf("cannot address source org %q for the org-level flag", m.Source.Org.Slug))
		return nil
	}
	cur, err := r.c.SourceOrg.GetFeatureFlags(ctx, vcs, name)
	if err != nil {
		r.report.add(kindCutoverOrgFlag, target, "error", fmt.Sprintf("read source org flags: %v", err))
		return nil
	}
	if cur[cutoverFlag] {
		if r.st.OrgFlag == nil {
			r.st.OrgFlag = &CutoverOrgFlagRecord{Org: m.Source.Org.Slug, Flag: cutoverFlag, Prior: true}
			if err := r.save(target); err != nil {
				return err
			}
		}
		r.report.add(kindCutoverOrgFlag, target, "exists", "source org already paused (drop-all-build-requests=true)")
		return nil
	}
	if !r.opts.Apply {
		r.report.add(kindCutoverOrgFlag, target, "set", "would pause the whole source org: set drop-all-build-requests=true (overrides every project)")
		return nil
	}
	added := r.st.OrgFlag == nil
	if added {
		r.st.OrgFlag = &CutoverOrgFlagRecord{Org: m.Source.Org.Slug, Flag: cutoverFlag, Prior: false}
		if err := r.save(target); err != nil {
			return err
		}
	}
	if err := r.c.SourceOrg.UpdateFeatureFlags(ctx, vcs, name, map[string]bool{cutoverFlag: true}); err != nil {
		if added {
			r.st.OrgFlag = nil
			if serr := r.save(target); serr != nil {
				return serr
			}
		}
		r.report.add(kindCutoverOrgFlag, target, "error", fmt.Sprintf("set org drop-all-build-requests=true: %v", err))
		return nil
	}
	r.report.add(kindCutoverOrgFlag, target, "set", "paused the whole source org: drop-all-build-requests=true (was false)")
	return nil
}

// ---- enable the destination ------------------------------------------------

func (r *cutoverRun) enableDest(ctx context.Context, m *manifest.Manifest, mapping *manifest.Mapping) error {
	s := r.c.Dest
	if s == nil || s.Org == nil || s.Projects == nil {
		return fmt.Errorf("cutover: no destination client")
	}
	destSlug := mapping.Org.To
	if destSlug == "" {
		return fmt.Errorf("cutover --enable-dest needs the destination org: pass --mapping with org.to set")
	}
	orgID, err := s.Org.ResolveOrgID(ctx, destSlug)
	if err != nil {
		return fmt.Errorf("resolving destination org %q: %w", destSlug, err)
	}
	projs, err := s.Projects.ListOrgProjects(ctx, orgID)
	if err != nil {
		return fmt.Errorf("list destination projects: %w", err)
	}
	byName := make(map[string]project.OrgProject, len(projs))
	for _, p := range projs {
		byName[p.Name] = p
	}

	for _, p := range m.Projects {
		name := p.Name
		if name == "" {
			if _, _, repo, err := project.SplitSlug(p.Slug); err == nil {
				name = repo
			}
		}
		dp, ok := byName[name]
		if !ok {
			r.report.add(kindCutoverDestTrigger, name, "manual", fmt.Sprintf("destination project %q not found — run sync --apply first, then re-run the cutover", name))
			continue
		}
		defs := p.PipelineDefinitions
		if len(defs) == 0 {
			fullName, ok := oauthSourceFullName(p.Slug)
			if !ok {
				r.report.add(kindCutoverDestTrigger, name, "manual", fmt.Sprintf("source project %q has no pipeline definitions to match and no GitHub repo can be derived — enable its destination trigger by hand", p.Slug))
				continue
			}
			defs = []manifest.PipelineDefinition{synthesizedDefinition(p, fullName)}
		}
		destDefs, err := s.Projects.ListPipelineDefinitions(ctx, dp.ID)
		if err != nil {
			r.report.add(kindCutoverDestTrigger, name, "error", fmt.Sprintf("list destination pipeline definitions: %v", err))
			continue
		}
		for _, def := range defs {
			if err := r.enableDefTriggers(ctx, name, dp.ID, def, findDestDef(destDefs, def.Name)); err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *cutoverRun) enableDefTriggers(ctx context.Context, projectName, projectID string, def manifest.PipelineDefinition, dd *project.PipelineDefinition) error {
	if len(def.Triggers) == 0 {
		return nil
	}
	defTarget := projectName + "/def:" + def.Name
	if dd == nil {
		r.report.add(kindCutoverDestTrigger, defTarget, "manual", fmt.Sprintf("pipeline definition %q not found in the destination project — run sync --apply, then re-run the cutover", def.Name))
		return nil
	}
	destTriggers, err := r.c.Dest.Projects.ListTriggers(ctx, projectID, dd.ID)
	if err != nil {
		r.report.add(kindCutoverDestTrigger, defTarget, "error", fmt.Sprintf("list destination triggers: %v", err))
		return nil
	}
	providers := make([]string, len(def.Triggers))
	for i, trig := range def.Triggers {
		providers[i], _ = translateTriggerProvider(trig.EventSource.Provider)
	}
	matches := matchDestTriggers(def.Triggers, providers, destTriggers)

	for i, trig := range def.Triggers {
		target := defTarget + "/trigger:" + trig.Name
		if trig.Disabled {
			r.report.add(kindCutoverDestTrigger, target, "skipped", "disabled in the source — left disabled")
			continue
		}
		if providers[i] == "" {
			r.report.add(kindCutoverDestTrigger, target, "manual", fmt.Sprintf("trigger has unsupported provider %q — enable its destination counterpart by hand", trig.EventSource.Provider))
			continue
		}
		mi := matches[i]
		if mi < 0 {
			r.report.add(kindCutoverDestTrigger, target, "manual", "no matching trigger in the destination — run sync --apply (or create it), then re-run the cutover")
			continue
		}
		d := destTriggers[mi]
		if !d.Disabled {
			r.report.add(kindCutoverDestTrigger, target+"/"+d.ID, "exists", "destination trigger already enabled")
			continue
		}
		if trig.EventSource.Provider == "schedule" && len(trig.Parameters) == 0 {
			r.report.add(kindCutoverDestTrigger, target+"/"+d.ID, "manual", "schedule trigger has no pipeline parameters in the manifest — verify them against the source schedule and enable it by hand")
			continue
		}
		et := EnableTarget{Kind: "trigger", ProjectID: projectID, ProjectName: projectName, DefName: def.Name, TriggerID: d.ID}
		rec := CutoverTriggerRecord{Project: projectName, ProjectID: projectID, Definition: def.Name, TriggerID: d.ID, Provider: d.EventSource.Provider}
		if !r.opts.Apply {
			a, _ := r.c.Dest.EnableBuilds(ctx, et, false)
			r.report.add(kindCutoverDestTrigger, target+"/"+d.ID, "set", a.Detail)
			continue
		}
		added := findTriggerRec(r.st.DestTriggers, d.ID) < 0
		if added {
			r.st.DestTriggers = append(r.st.DestTriggers, rec)
			if err := r.save(target); err != nil {
				return err
			}
		}
		a, eerr := r.c.Dest.EnableBuilds(ctx, et, true)
		if eerr != nil {
			if added {
				r.st.DestTriggers = removeTriggerRec(r.st.DestTriggers, d.ID)
				if serr := r.save(target); serr != nil {
					return serr
				}
			}
			r.report.add(kindCutoverDestTrigger, target+"/"+d.ID, "error", a.Detail)
			continue
		}
		r.report.add(kindCutoverDestTrigger, target+"/"+d.ID, "set", a.Detail)
	}
	return nil
}

// ---- undo ------------------------------------------------------------------

func isNotFound404(err error) bool {
	var he *rest.HTTPError
	return errors.As(err, &he) && he.Code == 404
}

// undo restores everything recorded in the state file: source flags to their
// prior values, source triggers re-enabled, destination triggers disabled, and
// the org flag restored. Restored items are removed from the state as they go.
func (r *cutoverRun) undo(ctx context.Context) error {
	st := r.st
	if st.Empty() {
		r.report.add("cutover-state", r.opts.StatePath, "exists", "nothing recorded in the state file — nothing to undo")
		return nil
	}

	var keepFlags []CutoverFlagRecord
	for _, rec := range st.SourceFlags {
		target := rec.Project + "/feature_flag:" + rec.Flag
		if r.c.Source == nil {
			return fmt.Errorf("cutover: no source client")
		}
		cur, err := r.c.Source.GetV11ProjectFeatureFlags(ctx, rec.Project)
		if err != nil {
			r.report.add(kindCutoverSourceFlag, target, "error", fmt.Sprintf("read source flags: %v", err))
			keepFlags = append(keepFlags, rec)
			continue
		}
		if cur[strings.ReplaceAll(rec.Flag, "_", "-")] == rec.Prior {
			r.report.add(kindCutoverSourceFlag, target, "exists", fmt.Sprintf("already %v — nothing to restore", rec.Prior))
			continue
		}
		if !r.opts.Apply {
			r.report.add(kindCutoverSourceFlag, target, "set", fmt.Sprintf("would restore %s=%v", rec.Flag, rec.Prior))
			keepFlags = append(keepFlags, rec)
			continue
		}
		if err := r.c.Source.SetV11ProjectFeatureFlags(ctx, rec.Project, map[string]bool{rec.Flag: rec.Prior}); err != nil {
			r.report.add(kindCutoverSourceFlag, target, "error", fmt.Sprintf("restore %s=%v: %v", rec.Flag, rec.Prior, err))
			keepFlags = append(keepFlags, rec)
			continue
		}
		r.report.add(kindCutoverSourceFlag, target, "set", fmt.Sprintf("restored %s=%v", rec.Flag, rec.Prior))
	}
	st.SourceFlags = keepFlags

	st.SourceTriggers = r.undoTriggers(ctx, st.SourceTriggers, r.c.Source, kindCutoverSourceTrigger, false, "re-enable")
	st.DestTriggers = r.undoTriggers(ctx, st.DestTriggers, r.c.DestToggle, kindCutoverDestTrigger, true, "disable")

	if rec := st.OrgFlag; rec != nil {
		target := rec.Org + "/feature_flag:" + rec.Flag
		vcs, name := flagOrgCoords(rec.Org)
		switch {
		case r.c.SourceOrg == nil:
			return fmt.Errorf("cutover: no source org client")
		case !r.opts.Apply:
			r.report.add(kindCutoverOrgFlag, target, "set", fmt.Sprintf("would restore org %s=%v", rec.Flag, rec.Prior))
		default:
			if err := r.c.SourceOrg.UpdateFeatureFlags(ctx, vcs, name, map[string]bool{rec.Flag: rec.Prior}); err != nil {
				r.report.add(kindCutoverOrgFlag, target, "error", fmt.Sprintf("restore org %s=%v: %v", rec.Flag, rec.Prior, err))
			} else {
				r.report.add(kindCutoverOrgFlag, target, "set", fmt.Sprintf("restored org %s=%v", rec.Flag, rec.Prior))
				st.OrgFlag = nil
			}
		}
	}
	return r.save("undo")
}

// undoTriggers reverses the recorded trigger changes and returns the records
// that are still outstanding. disable=true disables the trigger (destination),
// false re-enables it (source).
func (r *cutoverRun) undoTriggers(ctx context.Context, recs []CutoverTriggerRecord, tog TriggerToggler, kind string, disable bool, verb string) []CutoverTriggerRecord {
	var keep []CutoverTriggerRecord
	for _, rec := range recs {
		target := rec.Project + "/def:" + rec.Definition + "/trigger:" + rec.TriggerID
		switch {
		case tog == nil:
			r.report.add(kind, target, "error", "no client available to "+verb+" this trigger")
			keep = append(keep, rec)
		case !r.opts.Apply:
			r.report.add(kind, target, "set", "would "+verb+" trigger")
			keep = append(keep, rec)
		default:
			err := tog.SetTriggerDisabled(ctx, rec.ProjectID, rec.TriggerID, disable)
			switch {
			case err == nil:
				r.report.add(kind, target, "set", verb+"d trigger")
			case !disable && isNotFound404(err):
				r.report.add(kind, target, "manual", "trigger no longer exists (the source GitHub App was probably uninstalled, which removes its triggers) — nothing to re-enable")
			default:
				r.report.add(kind, target, "error", fmt.Sprintf("%s trigger: %v", verb, err))
				keep = append(keep, rec)
			}
		}
	}
	return keep
}
