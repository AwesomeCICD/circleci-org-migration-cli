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
	"github.com/CircleCI-Public/circleci-org-migration-cli/internal/manifest"
	"github.com/CircleCI-Public/circleci-org-migration-cli/version"
)

// OrgAPI is the subset of the org client the exporter needs.
type OrgAPI interface {
	GetOrganization(slugOrID string) (*org.Organization, error)
	GetOrgSettings(vcsType, orgName string) (*org.OrgSettings, error)
}

// ContextAPI is the subset of the context client the exporter needs.
type ContextAPI interface {
	ListContexts(ownerID, ownerSlug string) ([]cctx.Context, error)
	ListEnvVars(contextID string) ([]cctx.EnvVar, error)
	ListRestrictions(contextID string) ([]cctx.Restriction, error)
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
}

// Exporter reads a source org via the injected clients.
type Exporter struct {
	Org      OrgAPI
	Contexts ContextAPI
	Projects ProjectAPI
	// Out receives human-readable progress lines. If nil, progress is silent.
	Out io.Writer
}

func (e *Exporter) logf(format string, args ...any) {
	if e.Out != nil {
		fmt.Fprintf(e.Out, format+"\n", args...)
	}
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

	// Org settings (best-effort; only the context-group-restriction flag is
	// readable, and only via v1.1 with a vcs/name slug).
	if vcs, name, ok := splitOrgSlug(opts.OrgSlug, o.VCSType); ok {
		if s, serr := e.Org.GetOrgSettings(vcs, name); serr != nil {
			m.AddWarning("org", "org_settings_unreadable", fmt.Sprintf("could not read org settings: %v", serr))
		} else if s != nil {
			m.Source.Org.Settings = &manifest.OrgSettings{RequireContextGroupRestriction: s.RequireContextGroupRestriction}
		}
	}

	if opts.IncludeContexts {
		if err := e.exportContexts(m, o); err != nil {
			m.AddWarning("contexts", "contexts_unreadable", fmt.Sprintf("could not list contexts: %v", err))
		}
	}

	if opts.IncludeProjects {
		e.exportProjects(m, opts, o)
	}

	m.SortStable()
	return m, nil
}

func (e *Exporter) exportContexts(m *manifest.Manifest, o *org.Organization) error {
	e.logf("Listing contexts...")
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
					m.AddWarning("context:"+c.Name, "group_restriction_manual",
						fmt.Sprintf("group restriction %q must be recreated manually (group-restriction writes are not yet GA)", restrictionName(r)))
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
	slugs, discoveredOnly := e.resolveProjectSlugs(m, opts, o)
	e.logf("Exporting %d project(s)...", len(slugs))
	if discoveredOnly {
		m.AddWarning("projects", "project_discovery_followed_only",
			"projects were discovered from the followed-projects list (v1.1); repositories not followed by the source token's user may be missing — pass an explicit project list to be exhaustive")
	}

	for _, slug := range slugs {
		mp := manifest.Project{Slug: slug}

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
}

// resolveProjectSlugs returns the set of project slugs to export and whether
// the set came purely from discovery (no explicit slugs supplied).
func (e *Exporter) resolveProjectSlugs(m *manifest.Manifest, opts Options, o *org.Organization) (slugs []string, discoveredOnly bool) {
	set := map[string]struct{}{}
	for _, s := range opts.ProjectSlugs {
		if s = strings.TrimSpace(s); s != "" {
			set[s] = struct{}{}
		}
	}
	explicit := len(set)

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

	slugs = make([]string, 0, len(set))
	for s := range set {
		slugs = append(slugs, s)
	}
	sort.Strings(slugs)
	return slugs, explicit == 0
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
