// Package exporter orchestrates the read-only API clients to produce a
// manifest describing a source CircleCI organization. It maps each client's
// API-shaped types into the shared manifest contract and records warnings for
// anything that cannot be captured via API (most importantly, secret values).
package exporter

import (
	"fmt"
	"io"
	"sort"
	"strings"

	cctx "github.com/CircleCI-Public/circleci-org-migration-cli/api/context"
	"github.com/CircleCI-Public/circleci-org-migration-cli/api/org"
	"github.com/CircleCI-Public/circleci-org-migration-cli/api/project"
	apirunner "github.com/CircleCI-Public/circleci-org-migration-cli/api/runner"
	"github.com/CircleCI-Public/circleci-org-migration-cli/internal/clog"
	"github.com/CircleCI-Public/circleci-org-migration-cli/internal/manifest"
	"github.com/CircleCI-Public/circleci-org-migration-cli/version"
)

// OrgAPI is the subset of the org client the exporter needs.
type OrgAPI interface {
	GetOrganization(slugOrID string) (*org.Organization, error)
	GetOrgSettings(vcsType, orgName string) (*org.OrgSettings, error)
	GetFeatureFlags(vcsType, orgName string) (map[string]bool, error)
	GetOIDCClaims(orgID string) (audience []string, ttl string, err error)
	GetURLOrbAllowList(slugOrID string) ([]org.URLOrbAllowEntry, error)
	GetPolicyBundle(ownerID string) (map[string]string, error)
	GetPolicyEnforcement(ownerID string) (bool, error)
	GetAuditLogConfigs(orgID string) ([]org.AuditLogConfig, error)
	GetSSOEnforced(orgID string) (bool, error)
	GetSSOConnection(orgID string) (connection map[string]any, found bool, err error)
	GetOTelExporters(orgID string) ([]org.OTelExporter, error)
	GetContacts(orgID string) (primary, security []string, err error)
	ListGroups(orgID string) ([]org.Group, error)
}

// ContextAPI is the subset of the context client the exporter needs.
type ContextAPI interface {
	ListContexts(ownerID, ownerSlug string) ([]cctx.Context, error)
	ListEnvVars(contextID string) ([]cctx.EnvVar, error)
	ListRestrictions(contextID string) ([]cctx.Restriction, error)
}

// RunnerAPI is the subset of the runner client the exporter needs.
// When Runner is nil on the Exporter, runner resource classes are not captured.
type RunnerAPI interface {
	GetResourceClassesByNamespace(namespace string) ([]apirunner.ResourceClass, error)
}

// ProjectAPI is the subset of the project client the exporter needs.
type ProjectAPI interface {
	GetProject(slug string) (*project.Project, error)
	GetSettings(provider, org, proj string) (*project.AdvancedSettings, error)
	ListEnvVars(slug string) ([]project.EnvVar, error)
	ListCheckoutKeys(slug string) ([]project.CheckoutKey, error)
	ListWebhooks(projectID string) ([]project.Webhook, error)
	ListSchedules(slug string) ([]project.Schedule, error)
	FollowedProjectsForOrg(orgName string) ([]project.FollowedProject, error)
	GetProjectOIDCClaims(orgID, projID string) (audience []string, ttl string, err error)
	GetV11ProjectFeatureFlags(slug string) (map[string]bool, error)
	// ListOrgProjects returns all projects in an org by org UUID, covering both
	// GitHub OAuth and GitHub App org types.
	ListOrgProjects(orgID string) ([]project.OrgProject, error)
	// ListPipelineDefinitions returns all App-pipeline definitions for a project
	// identified by its UUID.
	ListPipelineDefinitions(projectID string) ([]project.PipelineDefinition, error)
	// ListTriggers returns all triggers for the given pipeline definition.
	ListTriggers(projectID, defID string) ([]project.Trigger, error)
}

// Options configures an export run.
type Options struct {
	Host string
	// OrgSlug is "gh/<org>" (GitHub OAuth) or "circleci/<org-id>" (GitHub App).
	OrgSlug string
	// ProjectSlugs, when non-empty, is the explicit set of project slugs to
	// export. When empty, the exporter discovers followed projects. Explicit
	// slugs are merged with discovered ones.
	ProjectSlugs []string
	// IncludeContexts / IncludeProjects toggle the two top-level resource kinds.
	IncludeContexts bool
	IncludeProjects bool
	// IncludeExtras captures the "safety net" project metadata: checkout-key
	// metadata, webhooks, and scheduled pipelines.
	IncludeExtras bool
	// RunnerNamespace, when non-empty, captures all self-hosted runner resource
	// classes registered under this namespace via the runner API. The namespace
	// must be supplied explicitly — there is no clean org→namespace lookup.
	// When empty, runner resource classes are silently skipped.
	RunnerNamespace string
}

// Exporter reads a source org via the injected clients.
type Exporter struct {
	Org      OrgAPI
	Contexts ContextAPI
	Projects ProjectAPI
	// Runner, when set, is used to capture self-hosted runner resource classes.
	// When nil, runner resource classes are silently skipped even if
	// Options.RunnerNamespace is set.
	Runner RunnerAPI
	// Out receives human-readable progress lines. If nil, progress is silent.
	Out io.Writer
}

// logf writes a human-progress line to both e.Out (for user-facing output) and
// the package-level clog logger at Info level. Progress lines are always sent
// to e.Out for backward compatibility; clog routing allows --debug to add detail.
func (e *Exporter) logf(format string, args ...any) {
	if e.Out != nil {
		fmt.Fprintf(e.Out, format+"\n", args...)
	}
	clog.Infof(format, args...)
}

// Export walks the source organization and returns a populated manifest. It
// fails fast on errors fetching the organization itself; per-resource errors
// are recorded as warnings so a partial export still completes.
func (e *Exporter) Export(opts Options) (*manifest.Manifest, error) {
	m := &manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		ToolVersion:   version.UserAgent(),
		Source:        manifest.Source{Host: opts.Host},
	}

	e.logf("Resolving organization %q...", opts.OrgSlug)
	o, err := e.Org.GetOrganization(opts.OrgSlug)
	if err != nil {
		return nil, fmt.Errorf("resolving organization %q: %w", opts.OrgSlug, err)
	}
	m.Source.Org = manifest.Org{Slug: o.Slug, ID: o.ID, Name: o.Name, VCSType: o.VCSType}
	e.logf("  → %s (id %s, %s)", o.Name, o.ID, o.VCSType)

	// Org settings: best-effort capture. Each sub-read is independent so a
	// failure in one (e.g. App org 404s on feature flags) does not prevent the
	// others from being captured.
	e.exportOrgSettings(m, o, opts.OrgSlug)

	if opts.IncludeContexts {
		if err := e.exportContexts(m, o); err != nil {
			m.AddWarning("contexts", "contexts_unreadable", fmt.Sprintf("could not list contexts: %v", err))
		}
	}

	if opts.IncludeProjects {
		e.exportProjects(m, opts, o)
	}

	e.exportRunnerResourceClasses(m, opts)

	m.SortStable()
	return m, nil
}

// exportRunnerResourceClasses captures self-hosted runner resource classes for
// the namespace named in opts.RunnerNamespace. When the namespace is empty or
// the Runner client is not set, the step is silently skipped. On API error an
// "org"-scoped warning (code "runner_unreadable") is added and the export
// continues — runner classes are never a fatal failure.
func (e *Exporter) exportRunnerResourceClasses(m *manifest.Manifest, opts Options) {
	if opts.RunnerNamespace == "" {
		clog.Debugf("runner_namespace not set; skipping runner resource class capture")
		return
	}
	if e.Runner == nil {
		clog.Debugf("Runner client not set; skipping runner resource class capture")
		return
	}

	e.logf("Listing runner resource classes for namespace %q...", opts.RunnerNamespace)
	clog.Debugf("GetResourceClassesByNamespace namespace=%s", opts.RunnerNamespace)

	classes, err := e.Runner.GetResourceClassesByNamespace(opts.RunnerNamespace)
	if err != nil {
		m.AddWarning("org", "runner_unreadable",
			fmt.Sprintf("could not list runner resource classes for namespace %q: %v", opts.RunnerNamespace, err))
		return
	}

	m.RunnerNamespace = opts.RunnerNamespace
	for _, rc := range classes {
		m.RunnerResourceClasses = append(m.RunnerResourceClasses, manifest.RunnerResourceClass{
			Name:        rc.ResourceClass,
			Description: rc.Description,
		})
	}

	clog.Debugf("captured %d runner resource class(es) for namespace %s", len(classes), opts.RunnerNamespace)
	e.logf("  → captured %d runner resource class(es)", len(classes))
}

func (e *Exporter) exportContexts(m *manifest.Manifest, o *org.Organization) error {
	e.logf("Listing contexts...")
	clog.Debugf("ListContexts org_id=%s slug=%s", o.ID, o.Slug)
	contexts, err := e.Contexts.ListContexts(o.ID, o.Slug)
	if err != nil {
		return err
	}
	e.logf("  → %d context(s)", len(contexts))

	for _, c := range contexts {
		mc := manifest.Context{Name: c.Name, SourceID: c.ID, CreatedAt: c.CreatedAt}

		if vars, verr := e.Contexts.ListEnvVars(c.ID); verr != nil {
			m.AddWarning("context:"+c.Name, "env_vars_unreadable", fmt.Sprintf("could not list env vars: %v", verr))
		} else {
			for _, v := range vars {
				mc.EnvVars = append(mc.EnvVars, manifest.ContextEnvVar{Name: v.Name, CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt})
			}
			if len(vars) > 0 {
				m.AddWarning("context:"+c.Name, "context_values_excluded",
					fmt.Sprintf("%d context variable value(s) are not in the manifest; capture them with the in-pipeline secrets step", len(vars)))
			}
		}

		// Restrictions (v2) now return the group name directly, so security
		// groups are derived from the group-type restrictions — no GraphQL.
		if rs, rerr := e.Contexts.ListRestrictions(c.ID); rerr != nil {
			m.AddWarning("context:"+c.Name, "restrictions_unreadable", fmt.Sprintf("could not list restrictions: %v", rerr))
		} else {
			for _, r := range rs {
				mc.Restrictions = append(mc.Restrictions, manifest.Restriction{Type: r.Type, Value: r.Value, Name: r.Name})
				if r.Type == "group" {
					mc.SecurityGroups = append(mc.SecurityGroups, manifest.Group{ID: r.Value, Name: r.Name})
					// The default "All members" group restriction (value == org ID)
					// is auto-created on every context and is synced via CIAM — no
					// manual action is required.  Only emit the warning for real
					// (non-All-members) group restrictions.
					if r.Value != o.ID {
						m.AddWarning("context:"+c.Name, "group_restriction_manual",
							fmt.Sprintf("group restriction %q must be recreated manually (group-restriction writes are not yet GA)", restrictionName(r)))
					}
				}
			}
		}

		m.Contexts = append(m.Contexts, mc)
		e.logf("  • context %q: %d var(s), %d restriction(s), %d group(s)", mc.Name, len(mc.EnvVars), len(mc.Restrictions), len(mc.SecurityGroups))
	}
	return nil
}

func restrictionName(r cctx.Restriction) string {
	if r.Name != "" {
		return r.Name
	}
	return r.Value
}

func (e *Exporter) exportProjects(m *manifest.Manifest, opts Options, o *org.Organization) {
	slugs, followedSlugs, followedFallback := e.resolveProjectSlugs(m, opts, o)
	e.logf("Exporting %d project(s)...", len(slugs))
	// Emit the followed-only warning only when discovery actually fell back to
	// the v1.1 followed-projects list (ListOrgProjects failed or was unavailable).
	// When the private project-list API succeeded we have a complete project set
	// and the warning is misleading.
	if followedFallback {
		m.AddWarning("projects", "project_discovery_followed_only",
			"projects were discovered from the followed-projects list (v1.1); repositories not followed by the source token's user may be missing — pass an explicit project list to be exhaustive")
	}

	for _, slug := range slugs {
		mp := manifest.Project{Slug: slug}

		// Set the Followed flag from the cross-reference set built during discovery.
		if followedSlugs != nil {
			followed := followedSlugs[slug]
			mp.Followed = &followed
		}

		clog.Debugf("GetProject slug=%s", slug)
		p, perr := e.Projects.GetProject(slug)
		if perr != nil {
			m.AddWarning("project:"+slug, "project_unreadable", fmt.Sprintf("could not read project: %v", perr))
			m.Projects = append(m.Projects, mp)
			continue
		}
		mp.SourceID = p.ID
		mp.Name = p.Name
		mp.VCS = manifest.ProjectVCS{Provider: p.VCS.Provider, URL: p.VCS.URL, DefaultBranch: p.VCS.DefaultBranch}

		if provider, orgName, projName, serr := project.SplitSlug(slug); serr == nil {
			if s, gerr := e.Projects.GetSettings(provider, orgName, projName); gerr != nil {
				m.AddWarning("project:"+slug, "settings_unreadable", fmt.Sprintf("could not read advanced settings: %v", gerr))
			} else if s != nil {
				mp.Settings = mapAdvancedSettings(s)
			}
		}

		if vars, verr := e.Projects.ListEnvVars(slug); verr != nil {
			m.AddWarning("project:"+slug, "env_vars_unreadable", fmt.Sprintf("could not list env vars: %v", verr))
		} else {
			for _, v := range vars {
				mp.EnvVars = append(mp.EnvVars, manifest.ProjectEnvVar{Name: v.Name, MaskedValue: v.MaskedValue, CreatedAt: v.CreatedAt})
			}
			if len(vars) > 0 {
				m.AddWarning("project:"+slug, "project_values_excluded",
					fmt.Sprintf("%d project variable value(s) are masked; capture them with the in-pipeline secrets step", len(vars)))
			}
		}

		if opts.IncludeExtras {
			e.exportProjectExtras(m, &mp, p)
		}

		// Project OIDC custom claims (best-effort; requires org ID and project UUID).
		if o.ID != "" && p.ID != "" {
			if audience, ttl, oerr := e.Projects.GetProjectOIDCClaims(o.ID, p.ID); oerr != nil {
				m.AddWarning("project:"+slug, "oidc_claims_unreadable", fmt.Sprintf("could not read project OIDC claims: %v", oerr))
			} else if len(audience) > 0 || ttl != "" {
				mp.OIDCAudience = audience
				mp.OIDCTTL = ttl
			}
		}

		// Per-project v1.1 feature flags (best-effort).
		if flags, ferr := e.Projects.GetV11ProjectFeatureFlags(slug); ferr != nil {
			m.AddWarning("project:"+slug, "v11_feature_flags_unreadable", fmt.Sprintf("could not read project v1.1 feature flags: %v", ferr))
		} else if len(flags) > 0 {
			if mp.Settings == nil {
				mp.Settings = &manifest.AdvancedSettings{}
			}
			if v, ok := flags["api-trigger-with-config"]; ok {
				v := v
				mp.Settings.APITriggerWithConfig = &v
			}
			if v, ok := flags["drop-all-build-requests"]; ok {
				v := v
				mp.Settings.DropAllBuildRequests = &v
			}
		}

		m.Projects = append(m.Projects, mp)
		e.logf("  • project %q: %d var(s)", slug, len(mp.EnvVars))
	}
}

func (e *Exporter) exportProjectExtras(m *manifest.Manifest, mp *manifest.Project, p *project.Project) {
	if keys, kerr := e.Projects.ListCheckoutKeys(mp.Slug); kerr != nil {
		m.AddWarning("project:"+mp.Slug, "checkout_keys_unreadable", fmt.Sprintf("could not list checkout keys: %v", kerr))
	} else {
		for _, k := range keys {
			mp.CheckoutKeys = append(mp.CheckoutKeys, manifest.CheckoutKey{
				Type: k.Type, Fingerprint: k.Fingerprint, PublicKey: k.PublicKey, Preferred: k.Preferred, CreatedAt: k.CreatedAt,
			})
		}
	}

	if p.ID != "" {
		if hooks, herr := e.Projects.ListWebhooks(p.ID); herr != nil {
			m.AddWarning("project:"+mp.Slug, "webhooks_unreadable", fmt.Sprintf("could not list webhooks: %v", herr))
		} else {
			for _, h := range hooks {
				mp.Webhooks = append(mp.Webhooks, manifest.Webhook{Name: h.Name, URL: h.URL, Events: h.Events, VerifyTLS: h.VerifyTLS})
			}
		}
	}

	if scheds, serr := e.Projects.ListSchedules(mp.Slug); serr != nil {
		m.AddWarning("project:"+mp.Slug, "schedules_unreadable", fmt.Sprintf("could not list schedules: %v", serr))
	} else {
		for _, s := range scheds {
			mp.Schedules = append(mp.Schedules, manifest.Schedule{
				Name: s.Name, Description: s.Description, Timetable: s.Timetable, Parameters: s.Parameters,
			})
		}
	}

	if p.ID != "" {
		e.exportPipelineDefinitions(m, mp, p.ID)
	}
}

// exportPipelineDefinitions fetches App-pipeline definitions and their triggers
// for the given project (identified by UUID) and appends them to mp. Each
// sub-read is best-effort: on error a project-scoped warning is added and the
// loop continues so a partial capture still completes.
func (e *Exporter) exportPipelineDefinitions(m *manifest.Manifest, mp *manifest.Project, projectID string) {
	defs, derr := e.Projects.ListPipelineDefinitions(projectID)
	if derr != nil {
		m.AddWarning("project:"+mp.Slug, "pipeline_definitions_unreadable",
			fmt.Sprintf("could not list pipeline definitions: %v", derr))
		return
	}

	for _, d := range defs {
		md := manifest.PipelineDefinition{
			Name:        d.Name,
			Description: d.Description,
			ConfigSource: manifest.PipelineSource{
				Provider:       d.ConfigSource.Provider,
				RepoFullName:   d.ConfigSource.Repo.FullName,
				RepoExternalID: d.ConfigSource.Repo.ExternalID,
				FilePath:       d.ConfigSource.FilePath,
			},
			CheckoutSource: manifest.PipelineSource{
				Provider:       d.CheckoutSource.Provider,
				RepoFullName:   d.CheckoutSource.Repo.FullName,
				RepoExternalID: d.CheckoutSource.Repo.ExternalID,
			},
		}

		triggers, terr := e.Projects.ListTriggers(projectID, d.ID)
		if terr != nil {
			m.AddWarning("project:"+mp.Slug, "triggers_unreadable",
				fmt.Sprintf("could not list triggers for definition %q: %v", d.Name, terr))
		} else {
			for _, t := range triggers {
				md.Triggers = append(md.Triggers, mapTrigger(t))
			}
		}

		mp.PipelineDefinitions = append(mp.PipelineDefinitions, md)
	}
}

// mapTrigger converts a project-API trigger into the manifest representation,
// flattening the discriminated-union event_source into named fields.
func mapTrigger(t project.Trigger) manifest.Trigger {
	es := manifest.TriggerEventSource{
		Provider: t.EventSource.Provider,
	}
	switch t.EventSource.Provider {
	case "github_app", "github_server", "github_oauth":
		es.RepoFullName = t.EventSource.Repo.FullName
		es.RepoExternalID = t.EventSource.Repo.ExternalID
	case "webhook":
		es.WebhookSender = t.EventSource.Webhook.Sender
	case "schedule":
		es.ScheduleCron = t.EventSource.Schedule.CronExpression
		es.ScheduleActor = t.EventSource.Schedule.AttributionActor
	}
	return manifest.Trigger{
		Name:        t.Name,
		EventName:   t.EventName,
		Description: t.Description,
		EventPreset: t.EventPreset,
		Disabled:    t.Disabled,
		CheckoutRef: t.CheckoutRef,
		ConfigRef:   t.ConfigRef,
		EventSource: es,
	}
}

// resolveProjectSlugs returns the set of project slugs to export, a set of
// followed-project slugs (used to populate the Followed flag on each project),
// and whether discovery fell back to the v1.1 followed-projects list (without
// any explicit slugs supplied by the caller).
//
// The third return value (followedFallback) is true only when:
//   - no explicit project slugs were provided, AND
//   - the primary ListOrgProjects discovery path failed or was unavailable,
//     causing the exporter to fall back to FollowedProjectsForOrg.
//
// When the private ListOrgProjects API succeeded, followedFallback is false
// even if no explicit slugs were given — the project set is complete.
//
// Discovery priority:
//  1. ListOrgProjects (private API, org UUID) — covers both GitHub OAuth and
//     GitHub App orgs.  On error, fall back to FollowedProjectsForOrg and emit
//     a "discovery_fallback" warning.
//  2. When discovery succeeds via ListOrgProjects, FollowedProjectsForOrg is
//     called separately to populate the followedSlugs cross-reference (for the
//     Followed flag).  If the org has no v1.1 slug form (circleci/ prefix) the
//     followed list is not fetched and followedSlugs is nil.
//
// followedSlugs is nil when it could not be determined (no v1.1 form for the
// org, or ListOrgProjects fell back to FollowedProjectsForOrg as discovery).
func (e *Exporter) resolveProjectSlugs(m *manifest.Manifest, opts Options, o *org.Organization) (slugs []string, followedSlugs map[string]bool, followedFallback bool) {
	set := map[string]struct{}{}
	for _, s := range opts.ProjectSlugs {
		if s = strings.TrimSpace(s); s != "" {
			set[s] = struct{}{}
		}
	}
	explicit := len(set)
	usedFollowedFallback := false

	switch {
	case explicit > 0:
		// Explicit --projects given: export EXACTLY those; do NOT run org-wide
		// discovery (which would otherwise add every project in the org). Still
		// build the followed cross-reference so the per-project Followed flag is set.
		e.logf("Exporting %d explicitly requested project(s)...", explicit)
		followedSlugs = e.buildFollowedSet(m, opts, o)
	case o.ID != "":
		e.logf("Discovering projects for org %q via private API...", o.ID)
		orgProjects, oerr := e.Projects.ListOrgProjects(o.ID)
		if oerr != nil {
			// Fall back to followed-projects list (preserves old behavior).
			m.AddWarning("projects", "discovery_fallback",
				fmt.Sprintf("private project list unavailable (%v); falling back to followed-projects list (v1.1)", oerr))
			e.discoverViaFollowed(m, opts, o, set)
			usedFollowedFallback = true
		} else {
			e.logf("  → %d project(s) found via private API", len(orgProjects))
			for _, op := range orgProjects {
				set[op.Slug] = struct{}{}
			}
			// Build followed-project cross-reference (best-effort; only for orgs
			// that have a v1.1 slug form).
			followedSlugs = e.buildFollowedSet(m, opts, o)
		}
	default:
		// No org ID available — fall back to followed-projects list.
		e.discoverViaFollowed(m, opts, o, set)
		usedFollowedFallback = true
	}

	slugs = make([]string, 0, len(set))
	for s := range set {
		slugs = append(slugs, s)
	}
	sort.Strings(slugs)
	// followedFallback is true only when discovery used the followed list AND the
	// caller did not supply any explicit slugs.
	return slugs, followedSlugs, explicit == 0 && usedFollowedFallback
}

// discoverViaFollowed adds followed-project slugs to set from FollowedProjectsForOrg.
// It is the fallback discovery path for orgs without a UUID or when the private
// project-list API is unavailable.
func (e *Exporter) discoverViaFollowed(m *manifest.Manifest, opts Options, o *org.Organization, set map[string]struct{}) {
	if vcs, name, ok := splitOrgSlug(opts.OrgSlug, o.VCSType); ok {
		_ = vcs
		e.logf("Discovering followed projects for %q...", name)
		if followed, ferr := e.Projects.FollowedProjectsForOrg(name); ferr != nil {
			m.AddWarning("projects", "discovery_failed", fmt.Sprintf("could not discover followed projects: %v", ferr))
		} else {
			for _, fp := range followed {
				set[opts.OrgSlug+"/"+fp.Reponame] = struct{}{}
			}
		}
	}
}

// buildFollowedSet calls FollowedProjectsForOrg and returns a map of slug →
// followed (true) for use as a cross-reference when stamping the Followed flag
// on each project.  Returns nil when the org has no v1.1 name form (i.e.
// circleci/ prefix orgs) or when the call fails (in which case a warning is
// added).
func (e *Exporter) buildFollowedSet(m *manifest.Manifest, opts Options, o *org.Organization) map[string]bool {
	_, name, ok := splitOrgSlug(opts.OrgSlug, o.VCSType)
	if !ok {
		return nil
	}
	followed, ferr := e.Projects.FollowedProjectsForOrg(name)
	if ferr != nil {
		m.AddWarning("projects", "followed_list_unreadable",
			fmt.Sprintf("could not fetch followed-projects list to set Followed flag: %v", ferr))
		return nil
	}
	result := make(map[string]bool, len(followed))
	for _, fp := range followed {
		slug := opts.OrgSlug + "/" + fp.Reponame
		result[slug] = true
	}
	return result
}

// mapAdvancedSettings converts the project client's settings into the manifest
// representation field-for-field.
func mapAdvancedSettings(s *project.AdvancedSettings) *manifest.AdvancedSettings {
	return &manifest.AdvancedSettings{
		AutocancelBuilds:           s.AutocancelBuilds,
		BuildForkPRs:               s.BuildForkPRs,
		BuildPRsOnly:               s.BuildPRsOnly,
		DisableSSH:                 s.DisableSSH,
		ForksReceiveSecretEnvVars:  s.ForksReceiveSecretEnvVars,
		OSS:                        s.OSS,
		SetGitHubStatus:            s.SetGithubStatus,
		SetupWorkflows:             s.SetupWorkflows,
		WriteSettingsRequiresAdmin: s.WriteSettingsRequiresAdmin,
		PROnlyBranchOverrides:      s.PROnlyBranchOverrides,
	}
}

// exportOrgSettings fills m.Source.Org.Settings with all readable org-level
// settings. Every sub-read is best-effort: on error a manifest warning is
// added and the field is left empty. App orgs (circleci/<uuid>) will 404 on
// the v1.1 feature-flags endpoint — that is normal and treated as empty.
func (e *Exporter) exportOrgSettings(m *manifest.Manifest, o *org.Organization, orgSlug string) {
	s := &manifest.OrgSettings{}
	hasAny := false

	// Feature flags (v1.1; only available for VCS-type orgs with a name slug).
	if vcs, name, ok := splitOrgSlug(orgSlug, o.VCSType); ok {
		if flags, ferr := e.Org.GetFeatureFlags(vcs, name); ferr != nil {
			m.AddWarning("org", "feature_flags_unreadable", fmt.Sprintf("could not read feature flags: %v", ferr))
		} else if len(flags) > 0 {
			s.FeatureFlags = flags
			// Convenience copy of the context-group-restriction flag.
			if v, present := flags["require_context_group_restriction"]; present {
				v := v
				s.RequireContextGroupRestriction = &v
			}
			hasAny = true
		}

		// Legacy RequireContextGroupRestriction via the old GetOrgSettings path
		// (belt-and-suspenders; covers orgs where GetFeatureFlags returned empty).
		if s.RequireContextGroupRestriction == nil {
			if os, serr := e.Org.GetOrgSettings(vcs, name); serr == nil && os != nil && os.RequireContextGroupRestriction != nil {
				s.RequireContextGroupRestriction = os.RequireContextGroupRestriction
				hasAny = true
			}
		}
	}

	// OIDC custom claims (v2; keyed by org UUID).
	if o.ID != "" {
		if audience, ttl, oerr := e.Org.GetOIDCClaims(o.ID); oerr != nil {
			m.AddWarning("org", "oidc_claims_unreadable", fmt.Sprintf("could not read OIDC claims: %v", oerr))
		} else if len(audience) > 0 || ttl != "" {
			s.OIDCAudience = audience
			s.OIDCTTL = ttl
			hasAny = true
		}
	}

	// URL-orb allow list (v2; keyed by slug-or-id).
	if urlList, uerr := e.Org.GetURLOrbAllowList(orgSlug); uerr != nil {
		m.AddWarning("org", "url_orb_allow_list_unreadable", fmt.Sprintf("could not read URL-orb allow list: %v", uerr))
	} else if len(urlList) > 0 {
		for _, entry := range urlList {
			s.URLOrbAllowList = append(s.URLOrbAllowList, manifest.URLOrbAllowEntry{
				Name:   entry.Name,
				Prefix: entry.Prefix,
				Auth:   entry.Auth,
			})
		}
		hasAny = true
	}

	// Config policies (v2; Scale plan only — 404 / 403 treated as empty).
	if o.ID != "" {
		if bundle, perr := e.Org.GetPolicyBundle(o.ID); perr != nil {
			m.AddWarning("org", "policy_bundle_unreadable", fmt.Sprintf("could not read config policies (Scale plan required): %v", perr))
		} else if len(bundle) > 0 {
			s.ConfigPolicies = bundle
			hasAny = true
		}

		if enabled, eerr := e.Org.GetPolicyEnforcement(o.ID); eerr != nil {
			m.AddWarning("org", "policy_enforcement_unreadable", fmt.Sprintf("could not read policy enforcement setting: %v", eerr))
		} else {
			s.PolicyEnforcementEnabled = &enabled
			hasAny = true
		}

		// Audit-log streaming configs (v2; org-scoped). Captured for the record
		// only — never auto-synced (their S3 ARN/region/bucket/endpoint are
		// environment-specific to the source org's AWS account).
		if configs, aerr := e.Org.GetAuditLogConfigs(o.ID); aerr != nil {
			m.AddWarning("org", "audit_log_configs_unreadable", fmt.Sprintf("could not read audit-log configs: %v", aerr))
		} else if len(configs) > 0 {
			for _, cfg := range configs {
				s.AuditLogConfigs = append(s.AuditLogConfigs, manifest.AuditLogConfig{
					ID:         cfg.ID,
					Purpose:    cfg.Purpose,
					TargetType: cfg.TargetType,
					IsDisabled: cfg.IsDisabled,
					Config: manifest.AuditLogTarget{
						ARN:          cfg.Config.ARN,
						Region:       cfg.Config.Region,
						BucketName:   cfg.Config.BucketName,
						BucketPrefix: cfg.Config.BucketPrefix,
						Endpoint:     cfg.Config.Endpoint,
					},
				})
			}
			hasAny = true
		}

		// SSO (SAML): best-effort, reference-only capture. SSO cannot be
		// auto-synced (recreation needs DNS domain verification + IdP setup), so
		// it is recorded for the operator and surfaced as a manual sync action.
		if e.exportSSO(m, o.ID, s) {
			hasAny = true
		}

		// OTel exporters (EXPERIMENTAL; up to 5 per org). Header values are
		// redacted by the server and captured for reference only.
		if exporters, oerr := e.Org.GetOTelExporters(o.ID); oerr != nil {
			m.AddWarning("org", "otel_exporters_unreadable", fmt.Sprintf("could not read OTel exporters: %v", oerr))
		} else if len(exporters) > 0 {
			for _, ex := range exporters {
				s.OTelExporters = append(s.OTelExporters, manifest.OTelExporter{
					Endpoint: ex.Endpoint,
					Protocol: ex.Protocol,
					Insecure: ex.Insecure,
					Headers:  ex.Headers,
				})
			}
			hasAny = true
		}

		// Org contacts (primary/security email lists).
		if primary, security, cerr := e.Org.GetContacts(o.ID); cerr != nil {
			m.AddWarning("org", "contacts_unreadable", fmt.Sprintf("could not read org contacts: %v", cerr))
		} else if len(primary) > 0 || len(security) > 0 {
			s.Contacts = &manifest.OrgContacts{Primary: primary, Security: security}
			hasAny = true
		}

		// Group definitions (names/IDs only). Captured so the cutover runbook can
		// tell the operator which groups to recreate in the destination org —
		// context group-restriction sync resolves destination groups by name. The
		// default "All members" group (ID == org ID) is auto-created on every org,
		// so it is excluded. Group MEMBERSHIP is never captured (managed via IdP).
		if groups, gerr := e.Org.ListGroups(o.ID); gerr != nil {
			m.AddWarning("org", "groups_unreadable", fmt.Sprintf("could not read org groups: %v", gerr))
		} else {
			var captured []manifest.OrgGroup
			for _, g := range groups {
				if g.ID == o.ID {
					// "All members" default group — auto-created everywhere; skip.
					continue
				}
				captured = append(captured, manifest.OrgGroup{ID: g.ID, Name: g.Name})
			}
			if len(captured) > 0 {
				s.Groups = captured
				hasAny = true
			}
		}
	}

	if hasAny {
		m.Source.Org.Settings = s
	}
}

// exportSSO reads the org's SSO enforcement and connection (best-effort) into s.
// It returns true when SSO state worth recording was found (enforcement on or a
// connection present); the all-empty case (enforcement off + no connection) is
// skipped so it does not appear in the manifest. Read failures add an "org"
// warning and never fail the export.
func (e *Exporter) exportSSO(m *manifest.Manifest, orgID string, s *manifest.OrgSettings) bool {
	enforced, eerr := e.Org.GetSSOEnforced(orgID)
	if eerr != nil {
		m.AddWarning("org", "sso_unreadable", fmt.Sprintf("could not read SSO enforcement: %v", eerr))
	}

	connection, found, cerr := e.Org.GetSSOConnection(orgID)
	if cerr != nil {
		m.AddWarning("org", "sso_unreadable", fmt.Sprintf("could not read SSO connection: %v", cerr))
	}

	if !enforced && !found {
		// No SSO configured and not enforced — nothing to record.
		return false
	}

	sso := &manifest.SSOSettings{Enforced: enforced}
	if found {
		// Extract the (non-sensitive) realm before redacting.
		if realm, ok := connection["realm"].(string); ok {
			sso.Realm = realm
		}
		// The manifest must never contain secret values. SSO connection bodies
		// carry IdP material (signing certs, client secrets, metadata XML); keep
		// the field NAMES for reference but redact their values.
		redacted, redactedKeys := redactSSOConnection(connection)
		sso.Connection = redacted
		if len(redactedKeys) > 0 {
			m.AddWarning("org", "sso_secret_redacted", fmt.Sprintf(
				"redacted %d sensitive SSO connection field(s) from the manifest "+
					"(SSO/SAML is reference-only and must be recreated manually on the "+
					"destination): %s", len(redactedKeys), strings.Join(redactedKeys, ", ")))
		}
	}
	s.SSO = sso
	return true
}

// ssoRedactionPlaceholder marks an SSO connection field whose value held IdP
// secret material and is intentionally NOT recorded in the manifest.
const ssoRedactionPlaceholder = "<redacted: SSO IdP material is not migrated; recreate SSO manually>"

// ssoSensitiveKeySubstrings are case-insensitive substrings that mark an SSO
// connection field as containing secret/IdP material (signing certificate,
// client secret, metadata XML which embeds certs, private keys, etc.).
var ssoSensitiveKeySubstrings = []string{
	"secret", "password", "credential", "token",
	"private", "cert", "x509", "metadata",
}

// redactSSOConnection returns a copy of the SSO connection map with the values
// of secret-shaped keys replaced by ssoRedactionPlaceholder, plus the sorted
// list of keys that were redacted. Field names are preserved (so the manifest
// still documents WHICH IdP fields existed) but their values never leak into
// the manifest, which is contractually free of secret values.
func redactSSOConnection(conn map[string]any) (map[string]any, []string) {
	if conn == nil {
		return nil, nil
	}
	out := make(map[string]any, len(conn))
	var redactedKeys []string
	for k, v := range conn {
		lk := strings.ToLower(k)
		sensitive := false
		for _, sub := range ssoSensitiveKeySubstrings {
			if strings.Contains(lk, sub) {
				sensitive = true
				break
			}
		}
		if sensitive {
			out[k] = ssoRedactionPlaceholder
			redactedKeys = append(redactedKeys, k)
		} else {
			out[k] = v
		}
	}
	sort.Strings(redactedKeys)
	return out, redactedKeys
}

// splitOrgSlug returns the (vcs, orgName) pair for a "vcs/org" slug. For
// GitHub App orgs ("circleci/<uuid>") there is no vcs/name form, so ok is false.
func splitOrgSlug(orgSlug, vcsType string) (vcs, name string, ok bool) {
	parts := strings.SplitN(orgSlug, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	if parts[0] == "circleci" {
		return "", "", false
	}
	// Prefer the canonical vcs type from the org object when available
	// (v1.1 settings expects e.g. "github"/"bitbucket").
	v := vcsType
	if v == "" {
		v = parts[0]
	}
	return v, parts[1], true
}
