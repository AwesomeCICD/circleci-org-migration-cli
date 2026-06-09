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
	"github.com/CircleCI-Public/circleci-org-migration-cli/api/org"
	"github.com/CircleCI-Public/circleci-org-migration-cli/api/project"
	"github.com/CircleCI-Public/circleci-org-migration-cli/internal/github"
	"github.com/CircleCI-Public/circleci-org-migration-cli/internal/manifest"
)

// resolveRepoID is a package-level variable so tests can inject a stub.
// Production code points to github.ResolveRepoID.
var resolveRepoID = github.ResolveRepoID

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
	// destination org. When empty, the captured RepoExternalID from the source
	// manifest is reused directly (valid for same-org migrations). When set, the
	// token is used to call the GitHub API and remap the external_id — useful when
	// the destination org is connected to a different GitHub org.
	//
	// TODO: cross-owner full_name remapping via a mapping file is a future follow-on.
	GitHubToken string
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
				s.syncAppProject(report, p, destOrgID, destSlug, bundle, opts,
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
			s.syncAppProject(report, p, destOrgID, destSlug, bundle, opts,
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
	nDefs := len(p.PipelineDefinitions)
	if !opts.Apply {
		report.add("project", name, "created",
			fmt.Sprintf("would create App project + %d pipeline-definition(s)", nDefs))
		// Still run configure helpers so dry-run shows what would happen.
		drySlug := "circleci/" + destOrgID + "/<new>"
		s.syncProjectSettings(report, p, drySlug, opts)
		s.syncProjectVars(report, p, bundle, drySlug, opts)
		s.syncProjectWebhooks(report, p, drySlug, "", opts)
		s.syncProjectSchedules(report, p, drySlug, opts)
		s.syncProjectOIDCClaims(report, p, drySlug, destOrgID, "", opts)
		s.syncProjectV11Flags(report, p, drySlug, opts)
		return
	}

	// Apply: create the project.
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

	// Create pipeline definitions and triggers.
	for _, def := range p.PipelineDefinitions {
		s.syncAppPipelineDefinition(report, name, newProjectID, def, destSlug, opts)
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
	opts Options,
) {
	// Resolve the external_id for config and checkout sources.
	// Strategy (per spec):
	//  1. If GitHubToken is set and RepoFullName is non-empty, call ResolveRepoID.
	//  2. Otherwise reuse the captured RepoExternalID.
	//  3. If neither is available, emit "manual".
	configExtID, configOK := s.resolveExternalID(report, projectName+"/def:"+def.Name+"/config",
		def.ConfigSource.RepoFullName, def.ConfigSource.RepoExternalID, opts)
	if !configOK {
		return
	}
	checkoutExtID, checkoutOK := s.resolveExternalID(report, projectName+"/def:"+def.Name+"/checkout",
		def.CheckoutSource.RepoFullName, def.CheckoutSource.RepoExternalID, opts)
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

	defID, err := s.Projects.CreatePipelineDefinition(projectID, spec)
	if err != nil {
		report.add("project-pipeline-def", projectName+"/def:"+def.Name, "error",
			fmt.Sprintf("create pipeline definition: %v", err))
		return
	}
	report.add("project-pipeline-def", projectName+"/def:"+def.Name, "created",
		"created pipeline definition")

	// Create triggers.
	for _, trig := range def.Triggers {
		s.syncAppTrigger(report, projectName, projectID, def.Name, defID, trig, opts)
	}
}

// syncAppTrigger creates one trigger (disabled) on a pipeline definition and
// queues an App EnableTarget for later enablement.
func (s *Syncer) syncAppTrigger(
	report *Report,
	projectName, projectID, defName, defID string,
	trig manifest.Trigger,
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
		trig.EventSource.RepoFullName, trig.EventSource.RepoExternalID, opts)
	if !ok {
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
// trigger. Resolution order:
//  1. If opts.GitHubToken != "" and fullName != "", call github.ResolveRepoID.
//     On success → use the resolved id. On failure → warn + fall through to 2.
//  2. Use capturedID if non-empty (normal same-GitHub-org case — silent).
//  3. Neither available → emit "manual" action and return ok=false.
func (s *Syncer) resolveExternalID(report *Report, target, fullName, capturedID string, opts Options) (string, bool) {
	if opts.GitHubToken != "" && fullName != "" {
		id, err := resolveRepoID(fullName, opts.GitHubToken, "")
		if err != nil {
			// Resolution failed — warn, fall back to captured id.
			report.add("project-ext-id", target, "manual",
				fmt.Sprintf("GitHub repo ID resolution failed for %q: %v — using captured id %q if available", fullName, err, capturedID))
			// Fall through to capturedID below.
		} else {
			return id, true
		}
	}
	if capturedID != "" {
		return capturedID, true
	}
	report.add("project-ext-id", target, "manual",
		"no external_id available (no GitHub token and no captured id) — create pipeline definition manually")
	return "", false
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
