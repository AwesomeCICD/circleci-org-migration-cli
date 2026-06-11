// Package exporter orchestrates the read-only API clients to produce a
// manifest describing a source CircleCI organization. It maps each client's
// API-shaped types into the shared manifest contract and records warnings for
// anything that cannot be captured via API (most importantly, secret values).
package exporter

import (
	"context"
	"fmt"
	"io"
	"strings"

	cctx "github.com/AwesomeCICD/circleci-org-migration-cli/api/context"
	"github.com/AwesomeCICD/circleci-org-migration-cli/api/org"
	"github.com/AwesomeCICD/circleci-org-migration-cli/api/project"
	apirunner "github.com/AwesomeCICD/circleci-org-migration-cli/api/runner"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/clog"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
	"github.com/AwesomeCICD/circleci-org-migration-cli/version"
)

// OrgAPI is the subset of the org client the exporter needs.
type OrgAPI interface {
	GetOrganization(ctx context.Context, slugOrID string) (*org.Organization, error)
	GetOrgSettings(ctx context.Context, vcsType, orgName string) (*org.OrgSettings, error)
	GetFeatureFlags(ctx context.Context, vcsType, orgName string) (map[string]bool, error)
	GetOIDCClaims(ctx context.Context, orgID string) (audience []string, ttl string, err error)
	GetURLOrbAllowList(ctx context.Context, slugOrID string) ([]org.URLOrbAllowEntry, error)
	GetPolicyBundle(ctx context.Context, ownerID string) (map[string]string, error)
	GetPolicyEnforcement(ctx context.Context, ownerID string) (bool, error)
	GetAuditLogConfigs(ctx context.Context, orgID string) ([]org.AuditLogConfig, error)
	GetSSOEnforced(ctx context.Context, orgID string) (bool, error)
	GetSSOConnection(ctx context.Context, orgID string) (connection map[string]any, found bool, err error)
	GetOTelExporters(ctx context.Context, orgID string) ([]org.OTelExporter, error)
	GetContacts(ctx context.Context, orgID string) (primary, security []string, err error)
	ListGroups(ctx context.Context, orgID string) ([]org.Group, error)
	GetStorageRetention(ctx context.Context, orgUUID string) (*org.StorageRetention, error)
	GetBudgets(ctx context.Context, orgUUID string) ([]org.Budget, error)
	GetBlockUnregisteredUsers(ctx context.Context, orgUUID string) (bool, error)
	GetOrgOrbs(ctx context.Context, orgUUID string) ([]org.OrgOrb, error)
	GetReleaseTrackerSettings(ctx context.Context, orgUUID string) (*org.ReleaseTrackerSettings, error)
	GetEnvironmentHierarchy(ctx context.Context, orgUUID string) (*org.EnvHierarchyConfig, error)
	// CIAM role/group endpoints (circleci-type orgs only).
	ListOrgRoleGrants(ctx context.Context, orgID string) ([]org.OrgRoleGrant, error)
	ListProjectUserRoleGrants(ctx context.Context, orgID, projectID string) ([]org.ProjectUserRoleGrant, error)
	ListProjectGroupRoleGrants(ctx context.Context, orgID, projectID string) ([]org.ProjectGroupRoleGrant, error)
}

// ContextAPI is the subset of the context client the exporter needs.
type ContextAPI interface {
	ListContexts(ctx context.Context, ownerID, ownerSlug string) ([]cctx.Context, error)
	ListEnvVars(ctx context.Context, contextID string) ([]cctx.EnvVar, error)
	ListRestrictions(ctx context.Context, contextID string) ([]cctx.Restriction, error)
}

// RunnerAPI is the subset of the runner client the exporter needs.
// When Runner is nil on the Exporter, runner resource classes are not captured.
type RunnerAPI interface {
	GetResourceClassesByNamespace(ctx context.Context, namespace string) ([]apirunner.ResourceClass, error)
}

// ProjectAPI is the subset of the project client the exporter needs.
type ProjectAPI interface {
	GetProject(ctx context.Context, slug string) (*project.Project, error)
	GetSettings(ctx context.Context, provider, org, proj string) (*project.AdvancedSettings, error)
	ListEnvVars(ctx context.Context, slug string) ([]project.EnvVar, error)
	ListCheckoutKeys(ctx context.Context, slug string) ([]project.CheckoutKey, error)
	ListWebhooks(ctx context.Context, projectID string) ([]project.Webhook, error)
	ListSchedules(ctx context.Context, slug string) ([]project.Schedule, error)
	FollowedProjectsForOrg(ctx context.Context, orgName string) ([]project.FollowedProject, error)
	GetProjectOIDCClaims(ctx context.Context, orgID, projID string) (audience []string, ttl string, err error)
	GetV11ProjectFeatureFlags(ctx context.Context, slug string) (map[string]bool, error)
	// ListAdditionalSSHKeys returns the public metadata for every additional
	// SSH key configured on a project. Private key material is never returned
	// by the API. On error the caller should record a non-fatal warning.
	ListAdditionalSSHKeys(ctx context.Context, slug string) ([]project.SSHKeyMeta, error)
	// ListOrgProjects returns all projects in an org by org UUID, covering both
	// GitHub OAuth and GitHub App org types.
	ListOrgProjects(ctx context.Context, orgID string) ([]project.OrgProject, error)
	// ListPipelineDefinitions returns all App-pipeline definitions for a project
	// identified by its UUID.
	ListPipelineDefinitions(ctx context.Context, projectID string) ([]project.PipelineDefinition, error)
	// ListTriggers returns all triggers for the given pipeline definition.
	ListTriggers(ctx context.Context, projectID, defID string) ([]project.Trigger, error)
	// ListProjectTokens returns the metadata (ID, label, scope) for every API
	// token configured on a project. Token values are never returned by the
	// list API. On error the caller should record a non-fatal warning.
	ListProjectTokens(ctx context.Context, slug string) ([]project.ProjectAPIToken, error)
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
	// Write progress to e.Out (the operator-facing stream) when set; otherwise
	// fall back to the leveled logger. Doing BOTH double-prints every line when
	// e.Out is already stderr (the common case), so pick one.
	if e.Out != nil {
		fmt.Fprintf(e.Out, format+"\n", args...)
		return
	}
	clog.Infof(format, args...)
}

// Export walks the source organization and returns a populated manifest. It
// fails fast on errors fetching the organization itself; per-resource errors
// are recorded as warnings so a partial export still completes.
func (e *Exporter) Export(ctx context.Context, opts Options) (*manifest.Manifest, error) {
	m := &manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		ToolVersion:   version.UserAgent(),
		Source:        manifest.Source{Host: opts.Host},
	}

	e.logf("Resolving organization %q...", opts.OrgSlug)
	o, err := e.Org.GetOrganization(ctx, opts.OrgSlug)
	if err != nil {
		return nil, fmt.Errorf("resolving organization %q: %w", opts.OrgSlug, err)
	}
	m.Source.Org = manifest.Org{Slug: o.Slug, ID: o.ID, Name: o.Name, VCSType: o.VCSType}
	e.logf("  → %s (id %s, %s)", o.Name, o.ID, o.VCSType)

	// Org settings: best-effort capture. Each sub-read is independent so a
	// failure in one (e.g. App org 404s on feature flags) does not prevent the
	// others from being captured.
	e.exportOrgSettings(ctx, m, o, opts.OrgSlug)

	if opts.IncludeContexts {
		if err := e.exportContexts(ctx, m, o); err != nil {
			m.AddWarning("contexts", "contexts_unreadable", fmt.Sprintf("could not list contexts: %v", err))
		}
	}

	if opts.IncludeProjects {
		e.exportProjects(ctx, m, opts, o)
	}

	e.exportRunnerResourceClasses(ctx, m, opts)
	e.exportCIAM(ctx, m, o)

	m.SortStable()
	return m, nil
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
