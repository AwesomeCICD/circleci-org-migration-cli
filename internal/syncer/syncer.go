// Package syncer writes an exported manifest (plus captured secret values) into
// a destination CircleCI organization. It is idempotent — existing resources are
// reused by name — and defaults to a dry run, recording planned actions in a
// report rather than mutating the org until apply is set.
package syncer

import (
	"fmt"
	"io"
	"strings"

	cctx "github.com/CircleCI-Public/circleci-org-migration-cli/api/context"
	"github.com/CircleCI-Public/circleci-org-migration-cli/api/project"
	"github.com/CircleCI-Public/circleci-org-migration-cli/internal/manifest"
)

// DefaultPlaceholder is the value used for variables whose real value was not
// captured, when the placeholder policy is selected.
const DefaultPlaceholder = "REPLACE_ME"

// Missing-secret policies.
const (
	MissingSkip        = "skip"
	MissingPlaceholder = "placeholder"
)

// OrgResolver resolves a destination org slug to its UUID.
type OrgResolver interface {
	ResolveOrgID(slug string) (string, error)
}

// Group is a destination CIAM group (id + name) used to resolve group-type
// context restrictions by name.
type Group struct {
	ID   string
	Name string
}

// GroupLister lists the destination org's CIAM groups so the syncer can resolve
// a source group restriction (captured by name) to the destination group UUID.
type GroupLister interface {
	ListGroups(orgID string) ([]Group, error)
}

// ContextWriter is the destination context API the syncer needs.
type ContextWriter interface {
	ListContexts(ownerID, ownerSlug string) ([]cctx.Context, error)
	CreateContext(name, ownerID string) (*cctx.Context, error)
	UpsertEnvVar(contextID, name, value string) error
	ListRestrictions(contextID string) ([]cctx.Restriction, error)
	CreateRestriction(contextID, restrictionType, restrictionValue string) error
}

// ProjectWriter is the destination project API the syncer needs.
type ProjectWriter interface {
	GetProject(slug string) (*project.Project, error)
	CreateProjectShell(provider, org, name string) (*project.Project, error)
	FollowProject(vcsType, org, repo string) (*project.FollowResult, error)
	ListEnvVars(slug string) ([]project.EnvVar, error)
	CreateEnvVar(slug, name, value string) error
	UpdateSettings(provider, org, proj string, s *project.AdvancedSettings) error
	ListWebhooks(projectID string) ([]project.Webhook, error)
	CreateWebhook(destProjectID string, w project.Webhook) error
	ListSchedules(slug string) ([]project.Schedule, error)
	CreateSchedule(destSlug, name, description, attributionActor string, timetable, parameters map[string]any) error
	GetProjectOIDCClaims(orgID, projID string) (audience []string, ttl string, err error)
	SetProjectOIDCClaims(orgID, projID string, audience []string, ttl string) error
	GetV11ProjectFeatureFlags(slug string) (map[string]bool, error)
	SetV11ProjectFeatureFlags(slug string, flags map[string]bool) error
}

// EnableTarget holds the coordinates needed to follow (enable builds for) a
// newly-created OAuth project.  App-org projects (circleci/ slugs) are not
// included here — App-org enable-builds support is a separate milestone.
type EnableTarget struct {
	Slug    string // full project slug, e.g. "gh/acme/web"
	VCSType string // vcs type as expected by v1.1 follow, e.g. "github"
	Org     string // org name, e.g. "acme"
	Repo    string // repo name, e.g. "web"
}

// Options configures a sync run.
type Options struct {
	// Apply performs writes. When false (the default), the run is a dry run.
	Apply bool
	// MissingSecrets is "skip" (default) or "placeholder".
	MissingSecrets string
	// Placeholder overrides DefaultPlaceholder when the placeholder policy is used.
	Placeholder string
}

func (o Options) placeholder() string {
	if o.Placeholder != "" {
		return o.Placeholder
	}
	return DefaultPlaceholder
}

// Syncer writes into a destination org via the injected clients.
type Syncer struct {
	Org         OrgResolver
	Contexts    ContextWriter
	Projects    ProjectWriter
	OrgSettings OrgSettingsWriter
	// Groups, when set, resolves destination group restrictions by name. When
	// nil, group-type restrictions fall back to "manual" (the previous behaviour).
	Groups GroupLister
	Out    io.Writer
}

func (s *Syncer) logf(format string, args ...any) {
	if s.Out != nil {
		fmt.Fprintf(s.Out, format+"\n", args...)
	}
}

// Action records one planned or performed change.
type Action struct {
	Kind   string // "context" | "context-var" | "restriction"
	Target string // context name (with var/restriction detail in Detail)
	Status string // created | exists | set | skipped | manual | error
	Detail string
}

// Report is the outcome of a sync run.
type Report struct {
	DestOrgSlug   string
	DestOrgID     string
	Applied       bool
	Actions       []Action
	PendingEnable []EnableTarget // OAuth projects created paused; waiting for FollowProject
}

func (r *Report) add(kind, target, status, detail string) {
	r.Actions = append(r.Actions, Action{Kind: kind, Target: target, Status: status, Detail: detail})
}

// Counts returns the number of actions with each status.
func (r *Report) Counts() map[string]int {
	c := map[string]int{}
	for _, a := range r.Actions {
		c[a.Status]++
	}
	return c
}

// SyncContexts recreates the manifest's contexts (and their captured variable
// values and restrictions) in the destination org. The destination org slug is
// mapping.Org.To.
func (s *Syncer) SyncContexts(m *manifest.Manifest, bundle *manifest.SecretBundle, mapping *manifest.Mapping, opts Options) (*Report, error) {
	if mapping == nil {
		mapping = manifest.IdentityMapping(m.Source.Org.Slug)
	}
	destSlug := mapping.Org.To
	if destSlug == "" {
		destSlug = m.Source.Org.Slug
	}
	report := &Report{DestOrgSlug: destSlug, Applied: opts.Apply}

	destOrgID, err := s.Org.ResolveOrgID(destSlug)
	if err != nil {
		return nil, fmt.Errorf("resolving destination org %q: %w", destSlug, err)
	}
	report.DestOrgID = destOrgID
	s.logf("Destination org: %s (id %s)%s", destSlug, destOrgID, dryRunSuffix(opts.Apply))

	existing, err := s.Contexts.ListContexts(destOrgID, "")
	if err != nil {
		return nil, fmt.Errorf("listing destination contexts: %w", err)
	}
	byName := map[string]cctx.Context{}
	for _, c := range existing {
		byName[c.Name] = c
	}

	// groupCache memoizes the destination group list (name → UUID) for the run.
	// nil until first needed; the bool guards a single lookup even on failure.
	var groupCache map[string]string
	groupCacheLoaded := false

	for _, c := range m.Contexts {
		ctxID, err := s.ensureContext(report, c.Name, destOrgID, byName, opts)
		if err != nil {
			report.add("context", c.Name, "error", err.Error())
			continue
		}
		s.syncContextVars(report, c, bundle, ctxID, opts)
		s.syncContextRestrictions(report, c, ctxID, destOrgID, &groupCache, &groupCacheLoaded, opts)
	}
	return report, nil
}

// ensureContext returns the destination context ID, creating it if absent.
// In dry-run mode a missing context yields an empty ID (nothing to write into).
func (s *Syncer) ensureContext(report *Report, name, destOrgID string, byName map[string]cctx.Context, opts Options) (string, error) {
	if c, ok := byName[name]; ok {
		report.add("context", name, "exists", "reusing existing context")
		return c.ID, nil
	}
	if !opts.Apply {
		report.add("context", name, "created", "would create context")
		return "", nil
	}
	created, err := s.Contexts.CreateContext(name, destOrgID)
	if err != nil {
		return "", err
	}
	report.add("context", name, "created", "created context")
	byName[name] = *created
	return created.ID, nil
}

func (s *Syncer) syncContextVars(report *Report, c manifest.Context, bundle *manifest.SecretBundle, ctxID string, opts Options) {
	values := map[string]string{}
	if bundle != nil {
		values = bundle.ContextSecrets[c.Name]
	}
	for _, v := range c.EnvVars {
		target := c.Name + "/" + v.Name
		val, ok := values[v.Name]
		if !ok {
			if opts.MissingSecrets == MissingPlaceholder {
				if err := s.writeVar(ctxID, v.Name, opts.placeholder(), opts.Apply); err != nil {
					report.add("context-var", target, "error", err.Error())
					continue
				}
				report.add("context-var", target, "set", "placeholder — value not captured; replace manually")
			} else {
				report.add("context-var", target, "manual", "value not captured; set manually")
			}
			continue
		}
		if err := s.writeVar(ctxID, v.Name, val, opts.Apply); err != nil {
			report.add("context-var", target, "error", err.Error())
			continue
		}
		report.add("context-var", target, "set", "value set from bundle")
	}
}

func (s *Syncer) writeVar(ctxID, name, value string, apply bool) error {
	if !apply || ctxID == "" {
		return nil // dry run, or context that would be created
	}
	return s.Contexts.UpsertEnvVar(ctxID, name, value)
}

func (s *Syncer) syncContextRestrictions(report *Report, c manifest.Context, ctxID, destOrgID string, groupCache *map[string]string, groupCacheLoaded *bool, opts Options) {
	var existing []cctx.Restriction
	if opts.Apply && ctxID != "" {
		if rs, err := s.Contexts.ListRestrictions(ctxID); err == nil {
			existing = rs
		}
	}
	for _, r := range c.Restrictions {
		target := c.Name + " [" + r.Type + "]"
		switch r.Type {
		case "expression":
			s.syncExpressionRestriction(report, target, ctxID, existing, r, opts)
		case "group":
			s.syncGroupRestriction(report, target, ctxID, destOrgID, existing, r, groupCache, groupCacheLoaded, opts)
		default:
			// project-type values are source-org UUIDs (need remap) and have no
			// name-based equivalent in the destination — manual handling.
			report.add("restriction", target, "manual", fmt.Sprintf("%s restriction %q must be recreated manually", r.Type, restrictionLabel(r)))
		}
	}
}

func (s *Syncer) syncExpressionRestriction(report *Report, target, ctxID string, existing []cctx.Restriction, r manifest.Restriction, opts Options) {
	if hasExpressionRestriction(existing, r.Value) {
		report.add("restriction", target, "exists", "expression restriction already present")
		return
	}
	if !opts.Apply || ctxID == "" {
		report.add("restriction", target, "set", "would add expression restriction")
		return
	}
	if err := s.Contexts.CreateRestriction(ctxID, "expression", r.Value); err != nil {
		report.add("restriction", target, "error", err.Error())
		return
	}
	report.add("restriction", target, "set", "added expression restriction")
}

// syncGroupRestriction resolves a source group restriction (captured by name) to
// a destination group UUID and recreates it. The special "All members" group's
// UUID equals the destination org id; other groups are matched by name against
// the destination group list. When no GroupLister is wired (s.Groups == nil) the
// restriction falls back to "manual", preserving the previous behaviour.
func (s *Syncer) syncGroupRestriction(report *Report, target, ctxID, destOrgID string, existing []cctx.Restriction, r manifest.Restriction, groupCache *map[string]string, groupCacheLoaded *bool, opts Options) {
	name := restrictionLabel(r)

	if s.Groups == nil {
		report.add("restriction", target, "manual", fmt.Sprintf("group restriction %q must be recreated manually", name))
		return
	}

	destUUID, resolved := s.resolveDestGroup(name, destOrgID, groupCache, groupCacheLoaded)
	if !resolved {
		report.add("restriction", target, "manual", fmt.Sprintf("group %q not found in destination — create it, then re-run", name))
		return
	}

	if hasGroupRestriction(existing, destUUID) {
		report.add("restriction", target, "exists", fmt.Sprintf("group restriction %q already present", name))
		return
	}
	if !opts.Apply || ctxID == "" {
		report.add("restriction", target, "set", fmt.Sprintf("would add group restriction %q", name))
		return
	}
	if err := s.Contexts.CreateRestriction(ctxID, "group", destUUID); err != nil {
		report.add("restriction", target, "error", err.Error())
		return
	}
	report.add("restriction", target, "set", fmt.Sprintf("added group restriction %q", name))
}

// resolveDestGroup returns the destination UUID for a group named name. The
// "All members" group resolves to the destination org id; other names are looked
// up in the destination group list (loaded once and cached for the run).
func (s *Syncer) resolveDestGroup(name, destOrgID string, groupCache *map[string]string, groupCacheLoaded *bool) (string, bool) {
	if name == "All members" {
		return destOrgID, true
	}
	if !*groupCacheLoaded {
		*groupCacheLoaded = true
		*groupCache = map[string]string{}
		if groups, err := s.Groups.ListGroups(destOrgID); err == nil {
			for _, g := range groups {
				(*groupCache)[g.Name] = g.ID
			}
		}
	}
	uuid, ok := (*groupCache)[name]
	return uuid, ok
}

// SyncProjects applies project advanced settings and environment-variable
// values to EXISTING destination projects. Projects missing in the destination
// are reported for manual handling — creation/follow is a separate opt-in step.
func (s *Syncer) SyncProjects(m *manifest.Manifest, bundle *manifest.SecretBundle, mapping *manifest.Mapping, opts Options) (*Report, error) {
	if mapping == nil {
		mapping = manifest.IdentityMapping(m.Source.Org.Slug)
	}
	destSlug := mapping.Org.To
	if destSlug == "" {
		destSlug = m.Source.Org.Slug
	}
	report := &Report{DestOrgSlug: destSlug, Applied: opts.Apply}

	// Resolve the destination org ID once — needed for OIDC PATCH calls.
	destOrgID, err := s.Org.ResolveOrgID(destSlug)
	if err != nil {
		return nil, fmt.Errorf("resolving destination org %q: %w", destSlug, err)
	}
	report.DestOrgID = destOrgID

	s.logf("Syncing %d project(s)%s", len(m.Projects), dryRunSuffix(opts.Apply))

	for _, p := range m.Projects {
		dst, ok := mapping.ResolveProjectSlug(p.Slug)
		if !ok {
			report.add("project", p.Slug, "manual", "no destination mapping (a GitHub App destination needs an explicit project mapping)")
			continue
		}
		destProj, err := s.Projects.GetProject(dst)
		if err != nil {
			// Project does not exist in destination — decide whether to create it.
			provider, org, repo, splitErr := project.SplitSlug(dst)
			if splitErr != nil {
				report.add("project", dst, "error", fmt.Sprintf("invalid destination slug: %v", splitErr))
				continue
			}
			if strings.ToLower(provider) == "circleci" {
				// App-org project creation is a separate milestone.
				report.add("project", dst, "manual", "project not found in destination — App-org project creation is a future milestone; create/follow it, then re-run")
				continue
			}

			// OAuth/Bitbucket path: create the project shell (paused — not followed).
			if !opts.Apply {
				report.add("project", dst, "created", "would create project (paused — not followed)")
			} else {
				created, createErr := s.Projects.CreateProjectShell(provider, org, repo)
				if createErr != nil {
					report.add("project", dst, "error", fmt.Sprintf("create project shell: %v", createErr))
					continue
				}
				destProj = created
				report.add("project", dst, "created", "created (paused — not followed)")
			}

			// Queue this project for the enable-builds step regardless of dry-run.
			report.PendingEnable = append(report.PendingEnable, EnableTarget{
				Slug:    dst,
				VCSType: provider,
				Org:     org,
				Repo:    repo,
			})

			// For apply mode, if destProj is still nil (create succeeded but
			// returned no useful ID), do a best-effort re-fetch so webhooks work.
			if opts.Apply && (destProj == nil || destProj.ID == "") {
				if fetched, fetchErr := s.Projects.GetProject(dst); fetchErr == nil {
					destProj = fetched
				} else {
					s.logf("warning: could not re-fetch %q after create (webhooks skipped): %v", dst, fetchErr)
					destProj = &project.Project{Slug: dst}
				}
			}
			if !opts.Apply {
				// In dry-run mode we have no real project ID — use empty string so
				// downstream helpers behave as normal dry-run (they check !opts.Apply
				// before using the ID).
				destProj = &project.Project{Slug: dst}
			}
		}

		s.syncProjectSettings(report, p, dst, opts)
		s.syncProjectVars(report, p, bundle, dst, opts)
		s.syncProjectWebhooks(report, p, dst, destProj.ID, opts)
		s.syncProjectSchedules(report, p, dst, opts)
		s.syncProjectOIDCClaims(report, p, dst, destOrgID, destProj.ID, opts)
		s.syncProjectV11Flags(report, p, dst, opts)
	}
	return report, nil
}

// EnableBuilds follows a project that was previously created paused, installing
// the webhook and enabling builds (which may trigger an initial build).
//
// In dry-run mode (apply=false) no API call is made and the returned Action has
// status "manual" with a detail explaining what would happen.  In apply mode
// FollowProject is called and the Action status reflects the result.
func (s *Syncer) EnableBuilds(t EnableTarget, apply bool) (Action, error) {
	if !apply {
		return Action{
			Kind:   "project",
			Target: t.Slug,
			Status: "manual",
			Detail: "would enable builds (follow)",
		}, nil
	}
	_, err := s.Projects.FollowProject(t.VCSType, t.Org, t.Repo)
	if err != nil {
		return Action{
			Kind:   "project",
			Target: t.Slug,
			Status: "error",
			Detail: fmt.Sprintf("enable builds (follow): %v", err),
		}, err
	}
	return Action{
		Kind:   "project",
		Target: t.Slug,
		Status: "set",
		Detail: "enabled builds (followed)",
	}, nil
}

func (s *Syncer) syncProjectSettings(report *Report, p manifest.Project, dst string, opts Options) {
	if p.Settings == nil {
		return
	}
	provider, org, proj, err := project.SplitSlug(dst)
	if err != nil {
		report.add("project-settings", dst, "error", err.Error())
		return
	}
	if !opts.Apply {
		report.add("project-settings", dst, "set", "would update advanced settings")
		return
	}
	if err := s.Projects.UpdateSettings(provider, org, proj, toProjectSettings(p.Settings)); err != nil {
		report.add("project-settings", dst, "error", err.Error())
		return
	}
	report.add("project-settings", dst, "set", "updated advanced settings")
}

func (s *Syncer) syncProjectVars(report *Report, p manifest.Project, bundle *manifest.SecretBundle, dst string, opts Options) {
	values := map[string]string{}
	if bundle != nil {
		values = bundle.ProjectSecrets[p.Slug] // keyed by the SOURCE slug
	}
	// Project env vars are not idempotent (no upsert), so skip names that
	// already exist in the destination.
	existing := map[string]bool{}
	if vars, err := s.Projects.ListEnvVars(dst); err == nil {
		for _, v := range vars {
			existing[v.Name] = true
		}
	}
	for _, v := range p.EnvVars {
		target := dst + "/" + v.Name
		if existing[v.Name] {
			report.add("project-var", target, "exists", "variable already present")
			continue
		}
		val, ok := values[v.Name]
		if !ok {
			if opts.MissingSecrets == MissingPlaceholder {
				if err := s.createVar(dst, v.Name, opts.placeholder(), opts.Apply); err != nil {
					report.add("project-var", target, "error", err.Error())
					continue
				}
				report.add("project-var", target, "set", "placeholder — value not captured; replace manually")
			} else {
				report.add("project-var", target, "manual", "value not captured; set manually")
			}
			continue
		}
		if err := s.createVar(dst, v.Name, val, opts.Apply); err != nil {
			report.add("project-var", target, "error", err.Error())
			continue
		}
		report.add("project-var", target, "set", "value set from bundle")
	}
}

func (s *Syncer) createVar(slug, name, value string, apply bool) error {
	if !apply {
		return nil
	}
	return s.Projects.CreateEnvVar(slug, name, value)
}

func toProjectSettings(s *manifest.AdvancedSettings) *project.AdvancedSettings {
	return &project.AdvancedSettings{
		AutocancelBuilds:           s.AutocancelBuilds,
		BuildForkPRs:               s.BuildForkPRs,
		BuildPRsOnly:               s.BuildPRsOnly,
		DisableSSH:                 s.DisableSSH,
		ForksReceiveSecretEnvVars:  s.ForksReceiveSecretEnvVars,
		OSS:                        s.OSS,
		SetGithubStatus:            s.SetGitHubStatus,
		SetupWorkflows:             s.SetupWorkflows,
		WriteSettingsRequiresAdmin: s.WriteSettingsRequiresAdmin,
		PROnlyBranchOverrides:      s.PROnlyBranchOverrides,
	}
}

func hasExpressionRestriction(existing []cctx.Restriction, value string) bool {
	for _, e := range existing {
		if e.Type == "expression" && e.Value == value {
			return true
		}
	}
	return false
}

func hasGroupRestriction(existing []cctx.Restriction, value string) bool {
	for _, e := range existing {
		if e.Type == "group" && e.Value == value {
			return true
		}
	}
	return false
}

func restrictionLabel(r manifest.Restriction) string {
	if r.Name != "" {
		return r.Name
	}
	return r.Value
}

func dryRunSuffix(apply bool) string {
	if apply {
		return ""
	}
	return "  [dry run]"
}
