package exporter

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/AwesomeCICD/circleci-org-migration-cli/api/org"
	"github.com/AwesomeCICD/circleci-org-migration-cli/api/project"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/clog"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
)

func (e *Exporter) exportProjects(ctx context.Context, m *manifest.Manifest, opts Options, o *org.Organization) {
	slugs, followedSlugs, followedFallback := e.resolveProjectSlugs(ctx, m, opts, o)
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
		p, perr := e.Projects.GetProject(ctx, slug)
		if perr != nil {
			m.AddWarning("project:"+slug, "project_unreadable", fmt.Sprintf("could not read project: %v", perr))
			m.Projects = append(m.Projects, mp)
			continue
		}
		mp.SourceID = p.ID
		mp.Name = p.Name
		mp.VCS = manifest.ProjectVCS{Provider: p.VCS.Provider, URL: p.VCS.URL, DefaultBranch: p.VCS.DefaultBranch}

		if provider, orgName, projName, serr := project.SplitSlug(slug); serr == nil {
			if s, gerr := e.Projects.GetSettings(ctx, provider, orgName, projName); gerr != nil {
				m.AddWarning("project:"+slug, "settings_unreadable", fmt.Sprintf("could not read advanced settings: %v", gerr))
			} else if s != nil {
				mp.Settings = mapAdvancedSettings(s)
			}
		}

		if vars, verr := e.Projects.ListEnvVars(ctx, slug); verr != nil {
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
			e.exportProjectExtras(ctx, m, &mp, p)
		}

		// Project OIDC custom claims (best-effort; requires org ID and project UUID).
		if o.ID != "" && p.ID != "" {
			if audience, ttl, oerr := e.Projects.GetProjectOIDCClaims(ctx, o.ID, p.ID); oerr != nil {
				m.AddWarning("project:"+slug, "oidc_claims_unreadable", fmt.Sprintf("could not read project OIDC claims: %v", oerr))
			} else if len(audience) > 0 || ttl != "" {
				mp.OIDCAudience = audience
				mp.OIDCTTL = ttl
			}
		}

		// Per-project v1.1 feature flags (best-effort). The full map is stored in
		// V11FeatureFlags; the two well-known keys are also copied into the
		// existing explicit fields for backward compatibility.
		if flags, ferr := e.Projects.GetV11ProjectFeatureFlags(ctx, slug); ferr != nil {
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
			// Store the full map so additional flags are not silently dropped.
			mp.Settings.V11FeatureFlags = flags
		}

		m.Projects = append(m.Projects, mp)
		if mp.Name != "" {
			e.logf("  • project %q (%s): %d var(s)", mp.Name, slug, len(mp.EnvVars))
		} else {
			e.logf("  • project %q: %d var(s)", slug, len(mp.EnvVars))
		}
	}
}

func (e *Exporter) exportProjectExtras(ctx context.Context, m *manifest.Manifest, mp *manifest.Project, p *project.Project) {
	if keys, kerr := e.Projects.ListCheckoutKeys(ctx, mp.Slug); kerr != nil {
		m.AddWarning("project:"+mp.Slug, "checkout_keys_unreadable", fmt.Sprintf("could not list checkout keys: %v", kerr))
	} else {
		for _, k := range keys {
			mp.CheckoutKeys = append(mp.CheckoutKeys, manifest.CheckoutKey{
				Type: k.Type, Fingerprint: k.Fingerprint, PublicKey: k.PublicKey, Preferred: k.Preferred, CreatedAt: k.CreatedAt,
			})
		}
	}

	// Additional SSH keys (public metadata only; private key is never returned
	// by the API). On error a non-fatal warning is recorded and the export
	// continues — missing SSH key metadata is not a fatal failure.
	if sshKeys, skerr := e.Projects.ListAdditionalSSHKeys(ctx, mp.Slug); skerr != nil {
		m.AddWarning("project:"+mp.Slug, "ssh_keys_unreadable",
			fmt.Sprintf("could not list additional SSH keys: %v", skerr))
	} else {
		for _, k := range sshKeys {
			mp.SSHKeys = append(mp.SSHKeys, manifest.ProjectSSHKey{
				Hostname:    k.Hostname,
				PublicKey:   k.PublicKey,
				Fingerprint: k.Fingerprint,
			})
		}
		if len(sshKeys) > 0 {
			m.AddWarning("project:"+mp.Slug, "ssh_keys_private_excluded",
				fmt.Sprintf("%d additional SSH key(s) captured (public metadata only); private keys are not exported — re-add them on the destination via the SSH-key extraction step", len(sshKeys)))
		}
	}

	if p.ID != "" {
		if hooks, herr := e.Projects.ListWebhooks(ctx, p.ID); herr != nil {
			m.AddWarning("project:"+mp.Slug, "webhooks_unreadable", fmt.Sprintf("could not list webhooks: %v", herr))
		} else {
			for _, h := range hooks {
				mp.Webhooks = append(mp.Webhooks, manifest.Webhook{Name: h.Name, URL: h.URL, Events: h.Events, VerifyTLS: h.VerifyTLS})
			}
			// Warn once per project when webhooks are present: signing secrets are
			// masked by the API and cannot be exported. HMAC-validating receivers
			// will reject calls until the secret is re-set on the destination.
			if len(hooks) > 0 {
				m.AddWarning("project:"+mp.Slug, "webhook_signing_secret_excluded",
					fmt.Sprintf("%d webhook(s) captured; signing secrets are not exported by the API — re-set each webhook's signing secret on the destination so HMAC-validating receivers continue to accept calls", len(hooks)))
			}
		}
	}

	if scheds, serr := e.Projects.ListSchedules(ctx, mp.Slug); serr != nil {
		m.AddWarning("project:"+mp.Slug, "schedules_unreadable", fmt.Sprintf("could not list schedules: %v", serr))
	} else {
		for _, s := range scheds {
			mp.Schedules = append(mp.Schedules, manifest.Schedule{
				Name: s.Name, Description: s.Description, Timetable: s.Timetable, Parameters: s.Parameters,
				ActorLogin: s.Actor.Login,
			})
		}
	}

	if p.ID != "" {
		e.exportPipelineDefinitions(ctx, m, mp, p.ID)
	}

	// Project API tokens (metadata only — label + scope; values are never
	// returned by the list API). Non-fatal on error: a warning is added and
	// the export continues so partial data is not silently dropped.
	if apiTokens, aterr := e.Projects.ListProjectTokens(ctx, mp.Slug); aterr != nil {
		m.AddWarning("project:"+mp.Slug, "api_tokens_unreadable",
			fmt.Sprintf("could not list project API tokens: %v", aterr))
	} else if len(apiTokens) > 0 {
		for _, t := range apiTokens {
			mp.APITokens = append(mp.APITokens, manifest.ProjectAPIToken{
				Label: t.Label,
				Scope: t.Scope,
			})
		}
		m.AddWarning("project:"+mp.Slug, "api_tokens_values_excluded",
			fmt.Sprintf("%d project API token(s) captured (label+scope only); token values are not retrievable — recreate them on the destination project and repoint every consumer", len(apiTokens)))
	}
}

// exportPipelineDefinitions fetches App-pipeline definitions and their triggers
// for the given project (identified by UUID) and appends them to mp. Each
// sub-read is best-effort: on error a project-scoped warning is added and the
// loop continues so a partial capture still completes.
func (e *Exporter) exportPipelineDefinitions(ctx context.Context, m *manifest.Manifest, mp *manifest.Project, projectID string) {
	defs, derr := e.Projects.ListPipelineDefinitions(ctx, projectID)
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

		triggers, terr := e.Projects.ListTriggers(ctx, projectID, d.ID)
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
func (e *Exporter) resolveProjectSlugs(ctx context.Context, m *manifest.Manifest, opts Options, o *org.Organization) (slugs []string, followedSlugs map[string]bool, followedFallback bool) {
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
		followedSlugs = e.buildFollowedSet(ctx, m, opts, o)
	case o.ID != "":
		e.logf("Discovering projects for org %q via private API...", o.ID)
		orgProjects, oerr := e.Projects.ListOrgProjects(ctx, o.ID)
		if oerr != nil {
			// Fall back to followed-projects list (preserves old behavior).
			m.AddWarning("projects", "discovery_fallback",
				fmt.Sprintf("private project list unavailable (%v); falling back to followed-projects list (v1.1)", oerr))
			e.discoverViaFollowed(ctx, m, opts, o, set)
			usedFollowedFallback = true
		} else {
			e.logf("  → %d project(s) found via private API", len(orgProjects))
			for _, op := range orgProjects {
				set[op.Slug] = struct{}{}
			}
			// Build followed-project cross-reference (best-effort; only for orgs
			// that have a v1.1 slug form).
			followedSlugs = e.buildFollowedSet(ctx, m, opts, o)
		}
	default:
		// No org ID available — fall back to followed-projects list.
		e.discoverViaFollowed(ctx, m, opts, o, set)
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
func (e *Exporter) discoverViaFollowed(ctx context.Context, m *manifest.Manifest, opts Options, o *org.Organization, set map[string]struct{}) {
	if vcs, name, ok := splitOrgSlug(opts.OrgSlug, o.VCSType); ok {
		_ = vcs
		e.logf("Discovering followed projects for %q...", name)
		if followed, ferr := e.Projects.FollowedProjectsForOrg(ctx, name); ferr != nil {
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
func (e *Exporter) buildFollowedSet(ctx context.Context, m *manifest.Manifest, opts Options, o *org.Organization) map[string]bool {
	_, name, ok := splitOrgSlug(opts.OrgSlug, o.VCSType)
	if !ok {
		return nil
	}
	followed, ferr := e.Projects.FollowedProjectsForOrg(ctx, name)
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
