// Package syncer writes an exported manifest (plus captured secret values) into
// a destination CircleCI organization. It is idempotent — existing resources are
// reused by name — and defaults to a dry run, recording planned actions in a
// report rather than mutating the org until apply is set.
package syncer

import (
	"errors"
	"fmt"
	"io"
	"strings"

	cctx "github.com/AwesomeCICD/circleci-org-migration-cli/api/context"
	"github.com/AwesomeCICD/circleci-org-migration-cli/api/org"
	"github.com/AwesomeCICD/circleci-org-migration-cli/api/project"
	apirunner "github.com/AwesomeCICD/circleci-org-migration-cli/api/runner"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/clog"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/github"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
)

// resolveRepoID is a package-level variable so tests can inject a stub.
// Production code points to github.ResolveRepoID.
var resolveRepoID = github.ResolveRepoID

// RunnerWriter is the subset of the runner client the syncer needs.
// When Runner is nil on the Syncer, runner resource classes are never written.
type RunnerWriter interface {
	GetResourceClassesByNamespace(namespace string) ([]apirunner.ResourceClass, error)
	CreateResourceClass(resourceClass, description string) (*apirunner.ResourceClass, error)
}

// DefaultPlaceholder is the value used for variables whose real value was not
// captured, when the placeholder policy is selected.
const DefaultPlaceholder = "REPLACE_ME"

// Missing-secret policies.
const (
	MissingSkip        = "skip"
	MissingPlaceholder = "placeholder"
)

// OrgResolver resolves a destination org slug to its UUID and retrieves org
// metadata needed to decide which project-creation path to take.
type OrgResolver interface {
	ResolveOrgID(slug string) (string, error)
	// GetOrganization returns the full organization record (id, name, vcs_type, …)
	// for the given slug or bare UUID.
	GetOrganization(slug string) (*org.Organization, error)
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

	// App-org (circleci/ vcs_type) project management.
	CreateAppProject(orgID, name string) (*project.Project, error)
	CreatePipelineDefinition(projectID string, spec project.PipelineDefinitionSpec) (string, error)
	CreateTrigger(projectID, defID string, spec project.TriggerSpec) (string, error)
	EnableTrigger(projectID, triggerID string) error
	ListOrgProjects(orgID string) ([]project.OrgProject, error)
}

// EnableTarget holds the coordinates needed to enable builds for a
// newly-created project.
//
// Kind selects the enable mechanism:
//   - "follow": OAuth/Bitbucket path — calls FollowProject(VCSType, Org, Repo).
//   - "trigger": GitHub App path — calls EnableTrigger(ProjectID, TriggerID).
type EnableTarget struct {
	// Kind is "follow" for OAuth projects and "trigger" for App trigger enablement.
	Kind string

	// OAuth / follow fields (Kind == "follow").
	Slug    string // full project slug, e.g. "gh/acme/web"
	VCSType string // vcs type as expected by v1.1 follow, e.g. "github"
	Org     string // org name, e.g. "acme"
	Repo    string // repo name, e.g. "web"

	// App trigger fields (Kind == "trigger").
	ProjectID string // project UUID
	TriggerID string // trigger UUID
}

// Options configures a sync run.
type Options struct {
	// Apply performs writes. When false (the default), the run is a dry run.
	Apply bool
	// MissingSecrets is "skip" (default) or "placeholder".
	MissingSecrets string
	// Placeholder overrides DefaultPlaceholder when the placeholder policy is used.
	Placeholder string
	// GitHubToken is an optional GitHub personal access token used to resolve
	// repository external IDs when creating pipeline definitions in a GitHub App
	// destination org. When empty and the GitHub org has not changed, the
	// captured RepoExternalID from the source manifest is reused directly (valid
	// for same-org migrations). When the GitHub org has changed (repos moved),
	// a token is required to look up the new external_id; without one the
	// project is skipped with a "manual" action.
	GitHubToken string
	// DestGitHubOrg is a convenience shorthand for the destination GitHub
	// organization owner when all repos have moved to the same new GH org. It
	// acts as the dest owner for every repo that does not have an explicit
	// GitHubOrg mapping in the Mapping file.
	//
	// Precedence: Mapping.GitHubOrg > DestGitHubOrg > unchanged (source owner).
	DestGitHubOrg string
	// DestRunnerNamespace is the destination namespace to recreate runner resource
	// classes into. When set, the syncer translates "<srcNs>/<name>" →
	// "<destNs>/<name>" for each class in the manifest. When empty and the
	// manifest contains runner classes, each is flagged as needing manual
	// recreation (the syncer never guesses the destination namespace).
	DestRunnerNamespace string
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
	// Runner, when set, is used to create runner resource classes in the
	// destination namespace. When nil, runner classes are flagged as manual.
	Runner RunnerWriter
	Out    io.Writer
}

// logf writes a human-progress line to both s.Out (for user-facing output) and
// the package-level clog logger at Info level.
func (s *Syncer) logf(format string, args ...any) {
	if s.Out != nil {
		fmt.Fprintf(s.Out, format+"\n", args...)
	}
	clog.Infof(format, args...)
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

	clog.Debugf("ListContexts dest_org_id=%s", destOrgID)
	existing, err := s.Contexts.ListContexts(destOrgID, "")
	if err != nil {
		return nil, fmt.Errorf("listing destination contexts: %w", err)
	}
	clog.Debugf("found %d existing context(s) in destination", len(existing))
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

// SyncProjects creates and configures destination projects from the manifest.
//
// Destination org type detection: the first time a project needs creation, the
// syncer calls GetOrganization on the destination slug to read its vcs_type.
// If vcs_type == "circleci" (GitHub App), the App creation path is used for all
// projects; otherwise the existing OAuth/Bitbucket path is used.
//
// App path per project: find by name in the dest org via ListOrgProjects; if
// found reuse the existing project; if not found create it with CreateAppProject,
// then create pipeline definitions and (disabled) triggers, queuing each trigger
// as an App EnableTarget for the enable-builds step.
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

	// Detect destination org type once (lazy: only when a project actually needs
	// to be looked up/created by name).
	var destVCSType string // "circleci" for App; other values for OAuth
	destTypeLoaded := false

	// destOrgProjectsByName is the name→OrgProject map for App destinations,
	// built once on first use.
	var destOrgProjectsByName map[string]project.OrgProject
	destOrgProjectsLoaded := false

	loadDestType := func() error {
		if destTypeLoaded {
			return nil
		}
		destTypeLoaded = true
		o, err := s.Org.GetOrganization(destSlug)
		if err != nil {
			return fmt.Errorf("get destination org %q: %w", destSlug, err)
		}
		destVCSType = strings.ToLower(o.VCSType)
		return nil
	}

	loadDestOrgProjects := func() error {
		if destOrgProjectsLoaded {
			return nil
		}
		destOrgProjectsLoaded = true
		projs, err := s.Projects.ListOrgProjects(destOrgID)
		if err != nil {
			return fmt.Errorf("list destination org projects: %w", err)
		}
		destOrgProjectsByName = make(map[string]project.OrgProject, len(projs))
		for _, p := range projs {
			destOrgProjectsByName[p.Name] = p
		}
		return nil
	}

	for _, p := range m.Projects {
		// --- determine destination slug / path ---
		dst, ok := mapping.ResolveProjectSlug(p.Slug)
		if !ok {
			// No explicit mapping: need to detect dest org type to decide.
			if err := loadDestType(); err != nil {
				report.add("project", p.Slug, "error", err.Error())
				continue
			}
			if destVCSType == "circleci" {
				// App org: route through App path (dst will be derived after create).
				s.syncAppProject(report, p, destOrgID, destSlug, bundle, mapping, opts,
					loadDestOrgProjects, &destOrgProjectsByName)
				continue
			}
			// OAuth: no mapping → manual.
			report.add("project", p.Slug, "manual", "no destination mapping (a GitHub App destination needs an explicit project mapping)")
			continue
		}

		// Explicit mapping provided — detect dest type to decide path.
		if err := loadDestType(); err != nil {
			report.add("project", dst, "error", err.Error())
			continue
		}

		if destVCSType == "circleci" {
			// App org with explicit mapping: still go through App path.
			s.syncAppProject(report, p, destOrgID, destSlug, bundle, mapping, opts,
				loadDestOrgProjects, &destOrgProjectsByName)
			continue
		}

		// --- OAuth / Bitbucket path (existing behaviour) ---
		destProj, err := s.Projects.GetProject(dst)
		if err != nil {
			provider, orgName, repo, splitErr := project.SplitSlug(dst)
			if splitErr != nil {
				report.add("project", dst, "error", fmt.Sprintf("invalid destination slug: %v", splitErr))
				continue
			}
			if strings.ToLower(provider) == "circleci" {
				// App-org slug given explicitly via mapping but dest org is not
				// a circleci type (rare/misconfigured) — surface as manual.
				report.add("project", dst, "manual", "project not found in destination — App-org project creation requires a circleci-type destination org; create/follow it, then re-run")
				continue
			}

			// Create the project shell (paused — not followed).
			if !opts.Apply {
				report.add("project", dst, "created", "would create project (paused — not followed)")
			} else {
				created, createErr := s.Projects.CreateProjectShell(provider, orgName, repo)
				if createErr != nil {
					report.add("project", dst, "error", fmt.Sprintf("create project shell: %v", createErr))
					continue
				}
				destProj = created
				report.add("project", dst, "created", "created (paused — not followed)")
			}

			// Queue for the enable-builds step regardless of dry-run.
			report.PendingEnable = append(report.PendingEnable, EnableTarget{
				Kind:    "follow",
				Slug:    dst,
				VCSType: provider,
				Org:     orgName,
				Repo:    repo,
			})

			// For apply mode, re-fetch so we have a real project ID.
			if opts.Apply && (destProj == nil || destProj.ID == "") {
				if fetched, fetchErr := s.Projects.GetProject(dst); fetchErr == nil {
					destProj = fetched
				} else {
					s.logf("warning: could not re-fetch %q after create (webhooks skipped): %v", dst, fetchErr)
					destProj = &project.Project{Slug: dst}
				}
			}
			if !opts.Apply {
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

// syncAppProject handles project sync when the destination org is a circleci-type
// (GitHub App) org. It finds or creates the project by name, then creates pipeline
// definitions and disabled triggers, queuing enable targets.
func (s *Syncer) syncAppProject(
	report *Report,
	p manifest.Project,
	destOrgID, destSlug string,
	bundle *manifest.SecretBundle,
	mapping *manifest.Mapping,
	opts Options,
	loadDestOrgProjects func() error,
	destOrgProjectsByName *map[string]project.OrgProject,
) {
	name := p.Name
	if name == "" {
		// Fall back to the last component of the source slug.
		_, _, repoName, err := project.SplitSlug(p.Slug)
		if err != nil {
			report.add("project", p.Slug, "error", fmt.Sprintf("project name and slug both invalid: %v", err))
			return
		}
		name = repoName
	}

	// Load existing dest-org projects once.
	if err := loadDestOrgProjects(); err != nil {
		report.add("project", name, "error", err.Error())
		return
	}

	existing, exists := (*destOrgProjectsByName)[name]

	if exists {
		// Reuse the existing project — configure settings/vars with the real slug.
		dst := existing.Slug
		s.syncProjectSettings(report, p, dst, opts)
		s.syncProjectVars(report, p, bundle, dst, opts)
		s.syncProjectWebhooks(report, p, dst, existing.ID, opts)
		s.syncProjectSchedules(report, p, dst, opts)
		s.syncProjectOIDCClaims(report, p, dst, destOrgID, existing.ID, opts)
		s.syncProjectV11Flags(report, p, dst, opts)
		return
	}

	// Project does not exist — create it.
	//
	// An OAuth-source project (gh//github slug) has no captured pipeline
	// definitions — OAuth uses an implicit pipeline — so we synthesize one App
	// pipeline definition + trigger, translating its build flags. An App-source
	// project (circleci/ slug) carries its own definitions and is recreated.
	synthesize := len(p.PipelineDefinitions) == 0
	nDefs := len(p.PipelineDefinitions)
	if !opts.Apply {
		if synthesize {
			report.add("project", name, "created",
				"would create App project + 1 synthesized pipeline-definition (OAuth source)")
		} else {
			report.add("project", name, "created",
				fmt.Sprintf("would create App project + %d pipeline-definition(s)", nDefs))
		}
		// Run resolve checks so the dry-run preview accurately shows which repos
		// are found in the destination GH org and which will be skipped.
		// syncAppPipelineDefinition is read-only at resolve time (no API writes).
		drySlug := "circleci/" + destOrgID + "/<new>"
		if synthesize {
			s.synthesizeOAuthPipelineDefinition(report, name, "", p, mapping, opts)
		}
		for _, def := range p.PipelineDefinitions {
			s.syncAppPipelineDefinition(report, name, "", def, destSlug, mapping, opts)
		}
		s.syncProjectSettings(report, p, drySlug, opts)
		s.syncProjectVars(report, p, bundle, drySlug, opts)
		s.syncProjectWebhooks(report, p, drySlug, "", opts)
		s.syncProjectSchedules(report, p, drySlug, opts)
		s.syncProjectOIDCClaims(report, p, drySlug, destOrgID, "", opts)
		s.syncProjectV11Flags(report, p, drySlug, opts)
		return
	}

	// Apply: create the project.
	clog.Debugf("CreateAppProject dest_org_id=%s name=%s", destOrgID, name)
	created, err := s.Projects.CreateAppProject(destOrgID, name)
	if err != nil {
		report.add("project", name, "error", fmt.Sprintf("create App project: %v", err))
		return
	}
	report.add("project", name, "created", "created App project")
	newSlug := created.Slug
	newProjectID := created.ID

	// Update the local cache so subsequent projects (if re-run) see this one.
	(*destOrgProjectsByName)[name] = project.OrgProject{
		ID:   newProjectID,
		Slug: newSlug,
		Name: name,
	}

	// Create pipeline definitions and triggers. OAuth-source projects (no
	// captured definitions) get one synthesized App pipeline-def + trigger;
	// App-source projects recreate their captured definitions.
	if synthesize {
		s.synthesizeOAuthPipelineDefinition(report, name, newProjectID, p, mapping, opts)
	}
	for _, def := range p.PipelineDefinitions {
		s.syncAppPipelineDefinition(report, name, newProjectID, def, destSlug, mapping, opts)
	}

	// Configure settings, vars, etc. on the new slug.
	s.syncProjectSettings(report, p, newSlug, opts)
	s.syncProjectVars(report, p, bundle, newSlug, opts)
	s.syncProjectWebhooks(report, p, newSlug, newProjectID, opts)
	s.syncProjectSchedules(report, p, newSlug, opts)
	s.syncProjectOIDCClaims(report, p, newSlug, destOrgID, newProjectID, opts)
	s.syncProjectV11Flags(report, p, newSlug, opts)
}

// syncAppPipelineDefinition creates one pipeline definition plus its triggers
// for a freshly-created App project.
func (s *Syncer) syncAppPipelineDefinition(
	report *Report,
	projectName, projectID string,
	def manifest.PipelineDefinition,
	destSlug string,
	mapping *manifest.Mapping,
	opts Options,
) {
	// Resolve the external_id for config and checkout sources.
	configExtID, configOK := s.resolveExternalID(report, projectName+"/def:"+def.Name+"/config",
		def.ConfigSource.RepoFullName, def.ConfigSource.RepoExternalID, mapping, opts)
	if !configOK {
		return
	}
	checkoutExtID, checkoutOK := s.resolveExternalID(report, projectName+"/def:"+def.Name+"/checkout",
		def.CheckoutSource.RepoFullName, def.CheckoutSource.RepoExternalID, mapping, opts)
	if !checkoutOK {
		return
	}

	filePath := def.ConfigSource.FilePath
	if filePath == "" {
		filePath = ".circleci/config.yml"
	}
	configProvider := def.ConfigSource.Provider
	if configProvider == "" {
		configProvider = "github_app"
	}
	checkoutProvider := def.CheckoutSource.Provider
	if checkoutProvider == "" {
		checkoutProvider = "github_app"
	}

	spec := project.PipelineDefinitionSpec{
		Name:               def.Name,
		Description:        def.Description,
		ConfigProvider:     configProvider,
		ConfigExternalID:   configExtID,
		ConfigFilePath:     filePath,
		CheckoutProvider:   checkoutProvider,
		CheckoutExternalID: checkoutExtID,
	}

	// Dry-run or no real project yet (preflight resolve pass): record intent only.
	if !opts.Apply || projectID == "" {
		report.add("project-pipeline-def", projectName+"/def:"+def.Name, "created",
			fmt.Sprintf("would create pipeline definition (config ext-id %s, checkout ext-id %s)", configExtID, checkoutExtID))
		// Still preview trigger actions.
		for _, trig := range def.Triggers {
			s.syncAppTrigger(report, projectName, projectID, def.Name, "", trig, mapping, opts)
		}
		return
	}

	defID, err := s.Projects.CreatePipelineDefinition(projectID, spec)
	if err != nil {
		// "Installation does not have access to repository" (and similar
		// access/installation errors) mean the dest GitHub App installation
		// cannot see the repo — this is an operator action, not a code bug.
		// Record as "manual" with a clear remediation note so the run continues.
		if isRepoAccessError(err) {
			report.add("project-pipeline-def", projectName+"/def:"+def.Name, "manual",
				fmt.Sprintf("create pipeline definition: %v — "+
					"the destination GitHub org must have the repository connected to the CircleCI GitHub App; "+
					"verify the App installation has access to the repo, and that the external_id resolves to a "+
					"repository the dest installation can access (use --github-token to resolve a moved repo's id)", err))
		} else {
			report.add("project-pipeline-def", projectName+"/def:"+def.Name, "error",
				fmt.Sprintf("create pipeline definition: %v", err))
		}
		return
	}
	report.add("project-pipeline-def", projectName+"/def:"+def.Name, "created",
		"created pipeline definition")

	// Create triggers.
	for _, trig := range def.Triggers {
		s.syncAppTrigger(report, projectName, projectID, def.Name, defID, trig, mapping, opts)
	}
}

// syncAppTrigger creates one trigger (disabled) on a pipeline definition and
// queues an App EnableTarget for later enablement.
func (s *Syncer) syncAppTrigger(
	report *Report,
	projectName, projectID, defName, defID string,
	trig manifest.Trigger,
	mapping *manifest.Mapping,
	opts Options,
) {
	target := projectName + "/def:" + defName + "/trigger:" + trig.Name
	provider := trig.EventSource.Provider

	// Only github_app / github_server triggers are recreated automatically.
	// webhook and schedule triggers must be handled manually.
	switch provider {
	case "github_app", "github_server":
		// Continue below.
	case "webhook":
		report.add("project-trigger", target, "manual",
			fmt.Sprintf("trigger %q has provider %q (webhook secret cannot be migrated automatically) — recreate manually", trig.Name, provider))
		return
	case "schedule":
		report.add("project-trigger", target, "manual",
			fmt.Sprintf("trigger %q has provider %q (schedule-trigger creation is a follow-on) — recreate manually", trig.Name, provider))
		return
	default:
		report.add("project-trigger", target, "manual",
			fmt.Sprintf("trigger %q has unsupported provider %q — recreate manually", trig.Name, provider))
		return
	}

	extID, ok := s.resolveExternalID(report, target,
		trig.EventSource.RepoFullName, trig.EventSource.RepoExternalID, mapping, opts)
	if !ok {
		return
	}

	// Dry-run or no real definition yet (preflight resolve pass): record intent only.
	if !opts.Apply || defID == "" {
		report.add("project-trigger", target, "created", "would create trigger (disabled — not yet enabled)")
		return
	}

	trigSpec := project.TriggerSpec{
		Provider:    provider,
		ExternalID:  extID,
		EventPreset: trig.EventPreset,
		Disabled:    true,
	}

	trigID, err := s.Projects.CreateTrigger(projectID, defID, trigSpec)
	if err != nil {
		report.add("project-trigger", target, "error",
			fmt.Sprintf("create trigger: %v", err))
		return
	}
	report.add("project-trigger", target, "created", "created trigger (disabled — not yet enabled)")

	// Queue for the enable-builds step.
	report.PendingEnable = append(report.PendingEnable, EnableTarget{
		Kind:      "trigger",
		ProjectID: projectID,
		TriggerID: trigID,
	})
}

// resolveExternalID returns the external_id to use for a pipeline-definition or
// trigger source, implementing the repo-move scenario logic:
//
//  1. Compute destFullName: apply GitHubOrg mapping from the Mapping (if set),
//     then fall back to DestGitHubOrg option, then leave unchanged.
//
//  2. If GitHubToken != "" and fullName != "": call ResolveRepoID(destFullName).
//     - success → use the new external_id (repo found in dest GH org).
//     - ErrRepoNotFound → emit "manual" with remediation note; return ok=false
//     (skip project creation — repo not accessible to dest App installation).
//     - other error → emit "error" action; return ok=false.
//
//  3. If GitHubToken == "":
//     - destFullName == fullName (GH org unchanged) → reuse capturedID silently
//     (current same-org behaviour).
//     - destFullName != fullName (org changed, no token) → emit "manual"; return
//     ok=false (cannot resolve without a token).
//
//  4. If capturedID is non-empty and no name to resolve → reuse capturedID.
//
//  5. No external_id at all → emit "manual"; return ok=false.
func (s *Syncer) resolveExternalID(report *Report, target, fullName, capturedID string, mapping *manifest.Mapping, opts Options) (string, bool) {
	// Step 1: compute destination full-name by applying the GH-org mapping.
	destFullName := s.mapRepoFullName(fullName, mapping, opts)

	if opts.GitHubToken != "" && destFullName != "" {
		// Step 2: token available — call the GitHub API.
		id, err := resolveRepoID(destFullName, opts.GitHubToken, "")
		if err != nil {
			if isNotFound(err) {
				// Repo missing in dest GH org → skip onboarding, emit manual.
				report.add("project-ext-id", target, "manual",
					fmt.Sprintf("repo %s not found in the destination GitHub org — "+
						"move/connect it to the dest CircleCI GitHub App, then re-run", destFullName))
				return "", false
			}
			// Other error → surface as error action.
			report.add("project-ext-id", target, "error",
				fmt.Sprintf("GitHub repo ID resolution failed for %q: %v", destFullName, err))
			return "", false
		}
		// Success: log that the repo was found (helpful in dry-run preview too).
		report.add("project-ext-id", target, "resolved",
			fmt.Sprintf("repo %s found in destination GitHub org (id %s)", destFullName, id))
		return id, true
	}

	// Step 3: no token.
	if destFullName != "" && destFullName != fullName {
		// GH org changed but no token to verify — must skip.
		report.add("project-ext-id", target, "manual",
			fmt.Sprintf("cannot resolve %s without --github-token; "+
				"provide a GitHub token (repo read) to verify/resolve repos in the new GitHub org", destFullName))
		return "", false
	}

	// Step 4: same GH org (or no full-name) → reuse captured id.
	if capturedID != "" {
		return capturedID, true
	}

	// Step 5: nothing available.
	report.add("project-ext-id", target, "manual",
		"no external_id available (no GitHub token and no captured id) — create pipeline definition manually")
	return "", false
}

// mapRepoFullName applies the GH-org mapping to a source repo full-name.
// Precedence: Mapping.GitHubOrg > opts.DestGitHubOrg > unchanged.
func (s *Syncer) mapRepoFullName(sourceFullName string, mapping *manifest.Mapping, opts Options) string {
	if sourceFullName == "" {
		return sourceFullName
	}
	// Explicit mapping wins.
	if mapping != nil && mapping.GitHubOrg != nil {
		return mapping.MapRepoFullName(sourceFullName)
	}
	// DestGitHubOrg convenience option.
	if opts.DestGitHubOrg != "" {
		// Replace the owner portion (everything before the first "/").
		slash := strings.Index(sourceFullName, "/")
		if slash > 0 {
			return opts.DestGitHubOrg + sourceFullName[slash:]
		}
	}
	return sourceFullName
}

// isNotFound returns true when err wraps or equals github.ErrRepoNotFound.
func isNotFound(err error) bool {
	return errors.Is(err, github.ErrRepoNotFound)
}

// EnableBuilds enables builds for a project that was previously created paused.
//
// For OAuth projects (Kind == "follow" or legacy empty Kind): calls FollowProject
// which installs the webhook and may trigger an initial build.
//
// For App trigger targets (Kind == "trigger"): calls EnableTrigger to set
// disabled=false on the specified trigger.
//
// In dry-run mode (apply=false) no API call is made and the returned Action has
// status "manual" with a detail explaining what would happen.  In apply mode the
// appropriate API is called and the Action status reflects the result.
func (s *Syncer) EnableBuilds(t EnableTarget, apply bool) (Action, error) {
	switch t.Kind {
	case "trigger":
		target := t.ProjectID + "/trigger/" + t.TriggerID
		if !apply {
			return Action{
				Kind:   "project",
				Target: target,
				Status: "manual",
				Detail: "would enable trigger (set disabled=false)",
			}, nil
		}
		if err := s.Projects.EnableTrigger(t.ProjectID, t.TriggerID); err != nil {
			return Action{
				Kind:   "project",
				Target: target,
				Status: "error",
				Detail: fmt.Sprintf("enable trigger: %v", err),
			}, err
		}
		return Action{
			Kind:   "project",
			Target: target,
			Status: "set",
			Detail: "enabled trigger (disabled=false)",
		}, nil
	default: // "follow" or legacy empty Kind
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

	settings := toProjectSettings(p.Settings)

	// GitHub App (circleci/ provider) projects reject OAuth-only fork/PR fields
	// with "Unexpected field 'advanced.oss'" (observed in live App→App migration).
	// App projects never build fork PRs, so these fields are not applicable.
	// Strip them from the PATCH so the write succeeds; do not mutate the manifest.
	if strings.ToLower(provider) == "circleci" {
		settings = stripOAuthOnlySettings(settings)
	}

	if err := s.Projects.UpdateSettings(provider, org, proj, settings); err != nil {
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

// stripOAuthOnlySettings returns a copy of s with the four OAuth-only / fork-PR
// fields zeroed out.  GitHub App projects reject these fields with
// "Unexpected field 'advanced.oss'" because App never builds fork PRs.
//
// Stripped fields: OSS, BuildForkPRs, ForksReceiveSecretEnvVars, PROnlyBranchOverrides.
// All other fields (autocancel_builds, set_github_status, setup_workflows, etc.)
// are valid for App and are preserved.
func stripOAuthOnlySettings(s *project.AdvancedSettings) *project.AdvancedSettings {
	cp := *s // shallow copy — all pointer fields stay independent
	cp.OSS = nil
	cp.BuildForkPRs = nil
	cp.ForksReceiveSecretEnvVars = nil
	cp.PROnlyBranchOverrides = nil
	return &cp
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

// isRepoAccessError returns true when err indicates that the GitHub App
// installation does not have access to the target repository.  These errors
// require the operator to connect the repo to the CircleCI App; they are not
// transient or code bugs, so callers record them as "manual" rather than "error".
func isRepoAccessError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "does not have access to repository") ||
		strings.Contains(msg, "installation does not have access")
}

func dryRunSuffix(apply bool) string {
	if apply {
		return ""
	}
	return "  [dry run]"
}

// SyncRunnerResourceClasses recreates runner resource classes from the manifest
// in the destination namespace specified by opts.DestRunnerNamespace.
//
//   - When DestRunnerNamespace is empty but the manifest has runner classes, each
//     is flagged as "manual" (the syncer never guesses the destination namespace).
//   - When DestRunnerNamespace is set, the class name is translated from
//     "<srcNs>/<name>" → "<destNs>/<name>" before creation.
//   - Idempotent: a class that already exists in the destination namespace is
//     treated as "exists" rather than an error.
//   - Dry-run aware: when opts.Apply is false, planned creations are reported
//     without making any API calls.
func (s *Syncer) SyncRunnerResourceClasses(m *manifest.Manifest, opts Options) (*Report, error) {
	report := &Report{Applied: opts.Apply}

	if len(m.RunnerResourceClasses) == 0 {
		clog.Debugf("manifest has no runner resource classes; skipping runner sync")
		return report, nil
	}

	// No destination namespace supplied — flag everything for manual recreation.
	if opts.DestRunnerNamespace == "" {
		s.logf("No --dest-runner-namespace set; runner resource classes require manual recreation")
		for _, rc := range m.RunnerResourceClasses {
			report.add("runner-resource-class", rc.Name, "manual",
				fmt.Sprintf("runner resource class %q must be recreated manually (no --dest-runner-namespace provided)", rc.Name))
		}
		return report, nil
	}

	s.logf("Syncing %d runner resource class(es) → namespace %q%s",
		len(m.RunnerResourceClasses), opts.DestRunnerNamespace, dryRunSuffix(opts.Apply))

	// Build existing-class set in the destination namespace (best-effort: if
	// listing fails we still attempt creation and let the API return 409/conflict).
	existingByName := map[string]bool{}
	if s.Runner != nil && opts.Apply {
		existing, lerr := s.Runner.GetResourceClassesByNamespace(opts.DestRunnerNamespace)
		if lerr != nil {
			clog.Debugf("could not pre-fetch existing runner classes in %s: %v", opts.DestRunnerNamespace, lerr)
		} else {
			for _, ex := range existing {
				existingByName[ex.ResourceClass] = true
			}
		}
	}

	srcNs := m.RunnerNamespace

	for _, rc := range m.RunnerResourceClasses {
		// Translate "<srcNs>/<name>" → "<destNs>/<name>".
		destName := translateResourceClass(rc.Name, srcNs, opts.DestRunnerNamespace)
		target := destName

		if existingByName[destName] {
			report.add("runner-resource-class", target, "exists",
				fmt.Sprintf("runner resource class %q already exists in destination namespace", destName))
			continue
		}

		if !opts.Apply {
			report.add("runner-resource-class", target, "created",
				fmt.Sprintf("would create runner resource class %q", destName))
			continue
		}

		if s.Runner == nil {
			report.add("runner-resource-class", target, "manual",
				fmt.Sprintf("runner resource class %q must be created manually (no runner client configured)", destName))
			continue
		}

		clog.Debugf("CreateResourceClass resource_class=%s", destName)
		_, err := s.Runner.CreateResourceClass(destName, rc.Description)
		if err != nil {
			// Treat "already exists" / conflict responses as idempotent success.
			if isAlreadyExists(err) {
				report.add("runner-resource-class", target, "exists",
					fmt.Sprintf("runner resource class %q already exists (conflict on create)", destName))
				continue
			}
			report.add("runner-resource-class", target, "error",
				fmt.Sprintf("create runner resource class %q: %v", destName, err))
			continue
		}
		report.add("runner-resource-class", target, "created",
			fmt.Sprintf("created runner resource class %q", destName))
	}

	return report, nil
}

// translateResourceClass replaces the srcNs portion of a "<ns>/<name>" resource
// class identifier with destNs. When the name does not contain a "/" or the
// source namespace cannot be determined, it falls back to prepending destNs.
func translateResourceClass(name, srcNs, destNs string) string {
	if srcNs != "" && strings.HasPrefix(name, srcNs+"/") {
		return destNs + name[len(srcNs):]
	}
	// Fallback: replace whatever prefix precedes the first "/" with destNs.
	if idx := strings.Index(name, "/"); idx >= 0 {
		return destNs + name[idx:]
	}
	return destNs + "/" + name
}

// isAlreadyExists returns true when err indicates a resource-already-exists
// condition (HTTP 409 Conflict or a message containing "already exists").
func isAlreadyExists(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "already exists") || strings.Contains(msg, "conflict")
}
