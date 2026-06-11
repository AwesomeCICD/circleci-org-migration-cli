// Package syncer writes an exported manifest (plus captured secret values) into
// a destination CircleCI organization. It is idempotent — existing resources are
// reused by name — and defaults to a dry run, recording planned actions in a
// report rather than mutating the org until apply is set.
//
// This file holds the package core: the injected-client interfaces, the Syncer
// and its Options/Report types, and small shared helpers. Per-domain sync logic
// lives in sibling files (contexts.go, project_sync.go, projects.go, orgsettings.go,
// ciam.go, runner.go, sshkeys.go, synthesize.go).
package syncer

import (
	"context"
	"fmt"
	"io"

	cctx "github.com/AwesomeCICD/circleci-org-migration-cli/api/context"
	"github.com/AwesomeCICD/circleci-org-migration-cli/api/org"
	"github.com/AwesomeCICD/circleci-org-migration-cli/api/project"
	apirunner "github.com/AwesomeCICD/circleci-org-migration-cli/api/runner"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/clog"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/github"
)

// resolveRepoID is a package-level variable so tests can inject a stub.
// Production code points to github.ResolveRepoID.
var resolveRepoID = github.ResolveRepoID

// RunnerWriter is the subset of the runner client the syncer needs.
// When Runner is nil on the Syncer, runner resource classes are never written.
type RunnerWriter interface {
	GetResourceClassesByNamespace(ctx context.Context, namespace string) ([]apirunner.ResourceClass, error)
	CreateResourceClass(ctx context.Context, resourceClass, description string) (*apirunner.ResourceClass, error)
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
	ResolveOrgID(ctx context.Context, slug string) (string, error)
	// GetOrganization returns the full organization record (id, name, vcs_type, …)
	// for the given slug or bare UUID.
	GetOrganization(ctx context.Context, slug string) (*org.Organization, error)
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
	ListGroups(ctx context.Context, orgID string) ([]Group, error)
}

// ContextWriter is the destination context API the syncer needs.
type ContextWriter interface {
	ListContexts(ctx context.Context, ownerID, ownerSlug string) ([]cctx.Context, error)
	CreateContext(ctx context.Context, name, ownerID string) (*cctx.Context, error)
	UpsertEnvVar(ctx context.Context, contextID, name, value string) error
	ListRestrictions(ctx context.Context, contextID string) ([]cctx.Restriction, error)
	CreateRestriction(ctx context.Context, contextID, restrictionType, restrictionValue string) error
}

// ProjectWriter is the destination project API the syncer needs.
type ProjectWriter interface {
	GetProject(ctx context.Context, slug string) (*project.Project, error)
	CreateProjectShell(ctx context.Context, provider, org, name string) (*project.Project, error)
	FollowProject(ctx context.Context, vcsType, org, repo string) (*project.FollowResult, error)
	ListEnvVars(ctx context.Context, slug string) ([]project.EnvVar, error)
	CreateEnvVar(ctx context.Context, slug, name, value string) error
	UpdateSettings(ctx context.Context, provider, org, proj string, s *project.AdvancedSettings) error
	ListWebhooks(ctx context.Context, projectID string) ([]project.Webhook, error)
	CreateWebhook(ctx context.Context, destProjectID string, w project.Webhook) error
	ListSchedules(ctx context.Context, slug string) ([]project.Schedule, error)
	CreateSchedule(ctx context.Context, destSlug, name, description, attributionActor string, timetable, parameters map[string]any) error
	GetProjectOIDCClaims(ctx context.Context, orgID, projID string) (audience []string, ttl string, err error)
	SetProjectOIDCClaims(ctx context.Context, orgID, projID string, audience []string, ttl string) error
	GetV11ProjectFeatureFlags(ctx context.Context, slug string) (map[string]bool, error)
	SetV11ProjectFeatureFlags(ctx context.Context, slug string, flags map[string]bool) error

	// SSH key management (additional SSH keys, not checkout keys).
	ListAdditionalSSHKeys(ctx context.Context, slug string) ([]project.SSHKeyMeta, error)
	AddAdditionalSSHKey(ctx context.Context, slug, hostname, privateKey string) error

	// Project API token management (optional auto-create path).
	ListProjectTokens(ctx context.Context, slug string) ([]project.ProjectAPIToken, error)
	CreateProjectToken(ctx context.Context, slug, scope, label string) (string, error)

	// App-org (circleci/ vcs_type) project management.
	CreateAppProject(ctx context.Context, orgID, name string) (*project.Project, error)
	CreatePipelineDefinition(ctx context.Context, projectID string, spec project.PipelineDefinitionSpec) (string, error)
	CreateTrigger(ctx context.Context, projectID, defID string, spec project.TriggerSpec) (string, error)
	EnableTrigger(ctx context.Context, projectID, triggerID string) error
	ListOrgProjects(ctx context.Context, orgID string) ([]project.OrgProject, error)
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
	// CreateProjectTokens controls whether captured project API tokens are
	// automatically recreated on the destination project. Default false —
	// creating a token mints a NEW one-time secret and every consumer must be
	// repointed; the default behaviour is to surface a manual step instead.
	//
	// When true AND Apply is true, the syncer calls CreateProjectToken for each
	// captured token that does not already exist by label+scope on the destination,
	// and prints the plaintext values to stderr with a "save these now" notice.
	// The plaintext values are NEVER written to stdout, JSON output, or log files.
	CreateProjectTokens bool
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
	// CIAM, when set, is used to sync CIAM role/group data for circleci-type
	// destination orgs. When nil, CIAM sync is silently skipped.
	CIAM CIAMWriter
	Out  io.Writer
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

func dryRunSuffix(apply bool) string {
	if apply {
		return ""
	}
	return "  [dry run]"
}
