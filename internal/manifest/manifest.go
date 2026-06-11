// Package manifest defines the on-disk data contract shared by every
// circleci-migrate command.
//
// Three documents make up a migration:
//
//   - Manifest (manifest.json): a complete, NON-SECRET description of a source
//     organization — its contexts, projects, settings, and the *names* of every
//     environment variable. It deliberately contains no secret values, so it is
//     safe to review, diff, and store.
//   - SecretBundle (secrets.json): the plaintext environment-variable values,
//     produced only by the in-pipeline `secrets` command (see secrets.go).
//   - Mapping (mapping.json): how source identities map onto the destination
//     org during `sync` (see mapping.go).
//
// Every document carries a SchemaVersion so the tool can evolve the format
// without silently misreading older exports.
package manifest

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
)

// SchemaVersion is the version of the manifest format this build understands.
const SchemaVersion = "1"

// Manifest is a non-secret snapshot of a source organization.
type Manifest struct {
	SchemaVersion string `json:"schema_version"`
	// GeneratedAt is an RFC 3339 timestamp stamped when the export is written.
	GeneratedAt string `json:"generated_at,omitempty"`
	// ToolVersion records the circleci-migrate build that produced the export.
	ToolVersion string    `json:"tool_version,omitempty"`
	Source      Source    `json:"source"`
	Contexts    []Context `json:"contexts"`
	Projects    []Project `json:"projects"`
	// RunnerNamespace is the source CircleCI namespace whose self-hosted runner
	// resource classes were captured. Empty when the export was run without
	// --runner-namespace. The namespace is stored here so that the syncer can
	// translate "<srcNs>/<name>" → "<destNs>/<name>" when recreating classes.
	RunnerNamespace       string                `json:"runner_namespace,omitempty"`
	RunnerResourceClasses []RunnerResourceClass `json:"runner_resource_classes,omitempty"`
	// Warnings records anything that could not be fully captured (for example
	// a context secret value, which CircleCI never exposes via API). These are
	// surfaced in the audit report so nothing is dropped silently.
	Warnings []Warning `json:"warnings,omitempty"`
}

// RunnerResourceClass is a self-hosted runner resource class definition
// captured from the CircleCI runner API. Only the class definition is stored;
// ephemeral runner instances are not captured (they are re-registered by the
// runner agent at startup and do not need to be migrated).
type RunnerResourceClass struct {
	// Name is the full "<namespace>/<class-name>" identifier as it appears in
	// the source namespace (e.g. "acme/my-runner").
	Name string `json:"name"`
	// Description is the human-readable description of the resource class.
	Description string `json:"description,omitempty"`
}

// Source identifies the organization an export was taken from.
type Source struct {
	Host string `json:"host"`
	Org  Org    `json:"org"`
}

// Org describes a CircleCI organization.
type Org struct {
	// Slug is "gh/<org>" for GitHub OAuth orgs or "circleci/<org-id>" for
	// GitHub App / GitLab orgs.
	Slug    string `json:"slug"`
	ID      string `json:"id,omitempty"`
	Name    string `json:"name"`
	VCSType string `json:"vcs_type,omitempty"` // github | bitbucket | circleci

	Settings *OrgSettings `json:"settings,omitempty"`
}

// OrgSettings holds org-level settings captured from the API.
type OrgSettings struct {
	// FeatureFlags is the full org feature-flag map from the v1.1 settings
	// endpoint (orb security, drop_all_build_requests, disable_user_checkout_keys,
	// require_context_group_restriction, AI/brownout toggles, …). Captured whole
	// so nothing is lost; sync writes back the safe/relevant ones.
	FeatureFlags map[string]bool `json:"feature_flags,omitempty"`

	// OIDC custom claims (v2 /org/{id}/oidc-custom-claims).
	OIDCAudience []string `json:"oidc_audience,omitempty"`
	OIDCTTL      string   `json:"oidc_ttl,omitempty"`

	// URLOrbAllowList entries (v2; GitHub App / circleci-type orgs only).
	URLOrbAllowList []URLOrbAllowEntry `json:"url_orb_allow_list,omitempty"`

	// ConfigPolicies maps policy name -> Rego content (v2, Scale plan), with the
	// enforcement toggle. Empty/nil when none or not on Scale.
	ConfigPolicies           map[string]string `json:"config_policies,omitempty"`
	PolicyEnforcementEnabled *bool             `json:"policy_enforcement_enabled,omitempty"`

	// RequireContextGroupRestriction mirrors the same-named feature flag, kept as
	// a convenience pointer (also present in FeatureFlags). Nil when not captured.
	RequireContextGroupRestriction *bool `json:"require_context_group_restriction,omitempty"`

	// AuditLogConfigs are the org's audit-log streaming configurations (v2). These
	// are captured for the record but NOT auto-synced: their S3 ARN/region/bucket/
	// endpoint are environment-specific and point at the SOURCE org's AWS account,
	// so sync surfaces them as manual actions to recreate in the destination.
	AuditLogConfigs []AuditLogConfig `json:"audit_log_configs,omitempty"`

	// SSO captures the org's SSO (SAML) state. It is recorded for reference only:
	// recreating SSO on a destination org is NOT automatable (it requires DNS TXT
	// domain verification plus IdP-side SAML app / iframe-origin setup), so sync
	// surfaces it as a manual action and never writes it. Nil when the org has no
	// SSO configured and enforcement is off.
	SSO *SSOSettings `json:"sso,omitempty"`

	// OTelExporters are the org's OpenTelemetry exporter configurations
	// (EXPERIMENTAL; up to 5 per org). Header values are redacted ("xxxx") by
	// the server and are captured for reference only — they cannot be replayed
	// automatically to the destination. Sync creates each exporter (without
	// headers) and emits manual actions for header keys so operators can
	// re-add the secret values.
	OTelExporters []OTelExporter `json:"otel_exporters,omitempty"`

	// Contacts holds the org's technical (primary) and security contact email
	// lists. Up to 5 addresses per list. Sync uses PUT (overwrites).
	Contacts *OrgContacts `json:"contacts,omitempty"`

	// Groups captures the org's CircleCI group DEFINITIONS (names/IDs only) so the
	// cutover runbook can tell the operator which groups to recreate in the
	// destination org — context group-restriction sync resolves destination groups
	// BY NAME, so they must already exist there. The default "All members" group
	// (auto-created on every org) is excluded. Group MEMBERSHIP is NOT captured:
	// it is managed via the IdP/SSO and recreated there, not migrated by this tool.
	Groups []OrgGroup `json:"groups,omitempty"`

	// StorageRetention captures the org's artifact/cache/workspace storage-retention
	// controls (days) at time of export. When present, sync transfers these values
	// to the destination org via POST; the server clamps them to the dest plan's
	// limits, so the resulting values may differ from the source. Nil when the org
	// has no custom retention or when the export lacked permission to read it.
	StorageRetention *StorageRetentionControls `json:"storage_retention,omitempty"`

	// StorageRetentionLimits captures the plan-enforced min/max bounds for each
	// retention type at time of export. Recorded for reference so operators know
	// the destination plan's limits before applying sync. Nil when not captured.
	StorageRetentionLimits *StorageRetentionLimits `json:"storage_retention_limits,omitempty"`

	// Budgets captures the org's spend-budget configuration. The org-level budget
	// is transferred directly to the destination. Per-project budgets reference
	// source org project UUIDs, which must be mapped to destination project UUIDs;
	// unmapped projects are flagged for manual recreation. EnforcementType is
	// captured for reference but may not be transferable via the PUT endpoint
	// (credits only). Nil when not captured.
	Budgets *OrgBudgets `json:"budgets,omitempty"`

	// BlockUnregisteredUsers captures whether the org has enabled the
	// "block unregistered user spend" feature. When non-nil, sync applies the
	// captured value to the destination org via PUT. Nil when not captured.
	BlockUnregisteredUsers *bool `json:"block_unregistered_users,omitempty"`

	// Orbs captures the orbs published in the source org (read from the private
	// orb-list API). Orbs CANNOT be auto-migrated (the destination org has a
	// different namespace, and orb source is only available via GraphQL/republish),
	// so sync surfaces each as a manual action. Nil/empty when not captured.
	Orbs []OrgOrb `json:"orbs,omitempty"`

	// OrbNamespace is the orb namespace claimed by this org (best-effort; derived
	// from the org name). For circleci-type orgs the org UUID is used as the
	// namespace base; for VCS orgs the org name is used. Captured so the republish
	// runbook can reference the source namespace. Empty when not determinable.
	OrbNamespace string `json:"orb_namespace,omitempty"`

	// ReleaseTracker captures the org's release-tracker settings. When non-nil,
	// sync transfers these to the destination via PATCH. Nil when not configured
	// or when the export lacked permission to read them.
	ReleaseTracker *ReleaseTrackerSettings `json:"release_tracker,omitempty"`

	// EnvironmentHierarchy captures the org's environment-hierarchy configuration
	// for reference. This CANNOT be auto-migrated (the POST endpoint needs
	// destination deploy-integration IDs that cannot be mapped automatically), so
	// sync surfaces it as a manual action. Nil when no hierarchy is configured.
	EnvironmentHierarchy *EnvironmentHierarchy `json:"environment_hierarchy,omitempty"`
}

// OrgGroup is a CircleCI group DEFINITION (name/ID) captured for the migration
// runbook. Only the identity is recorded — group membership is managed via the
// IdP/SSO and is recreated there, never migrated by this tool.
type OrgGroup struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// OrgBudgets holds the captured spend-budget configuration for an org.
type OrgBudgets struct {
	// OrgBudget is the org-level budget (project_id == null in the API response).
	// Nil when no org-level budget is configured.
	OrgBudget *BudgetEntry `json:"org_budget,omitempty"`
	// ProjectBudgets lists per-project budget entries. Each entry's ProjectID is
	// the SOURCE org project UUID; it must be mapped to the destination project UUID
	// before sync can transfer it.
	ProjectBudgets []BudgetEntry `json:"project_budgets,omitempty"`
}

// BudgetEntry is one budget entry captured from the budgets API. Only the
// configuration fields (Credits, EnforcementType, ProjectID) are captured;
// runtime stats (Consumption, Percentage, ThresholdExceeded) are omitted.
type BudgetEntry struct {
	// Credits is the configured credit limit for this budget.
	Credits int `json:"credits"`
	// BudgetID is the server-assigned UUID for this budget entry (used for DELETE).
	BudgetID string `json:"budget_id,omitempty"`
	// EnforcementType is one of "warn" or "block". Captured for reference; the PUT
	// endpoint only accepts credits (+ project_id), so enforcement_type cannot be
	// transferred automatically.
	EnforcementType string `json:"enforcement_type,omitempty"`
	// ProjectID is the source org project UUID for per-project budgets. Nil for the
	// org-level budget.
	ProjectID *string `json:"project_id,omitempty"`
}

// OrgOrb is one orb captured from the private orb-list API.
// Only configuration fields are stored; runtime statistics are omitted.
// Orbs cannot be auto-migrated (the destination org has a different namespace),
// so they are surfaced as manual actions in sync.
type OrgOrb struct {
	// OrbName is the fully-qualified orb name (e.g. "acme/my-orb").
	OrbName string `json:"orb_name"`
	// LatestVersionNumber is the newest published version (e.g. "0.3.0").
	LatestVersionNumber string `json:"latest_version_number"`
	// OrbID is the server-assigned UUID for this orb.
	OrbID string `json:"orb_id,omitempty"`
	// IsPrivate reports whether the orb is private.
	IsPrivate bool `json:"is_private"`
	// Hidden reports whether the orb is hidden from search results.
	Hidden bool `json:"hidden"`
	// Description is the human-readable orb description.
	Description string `json:"description,omitempty"`
}

// ReleaseTrackerSettings holds org-level release-tracker configuration.
// Sync transfers these to the destination via PATCH when non-nil.
type ReleaseTrackerSettings struct {
	// InconclusiveReleaseTTL is a duration string (e.g. "1h") controlling how
	// long an inconclusive release is retained before being expired.
	InconclusiveReleaseTTL string `json:"inconclusive_release_ttl,omitempty"`
}

// EnvironmentHierarchy captures the org's environment-hierarchy configuration.
// It is recorded for reference only — sync surfaces it as a manual action
// because recreating it requires destination deploy-integration IDs that cannot
// be mapped automatically from the source.
type EnvironmentHierarchy struct {
	Name        string              `json:"name"`
	Description string              `json:"description,omitempty"`
	Levels      []EnvHierarchyLevel `json:"levels,omitempty"`
}

// EnvHierarchyLevel is one level in an environment hierarchy.
// IntegrationID is intentionally NOT captured here — it is source-org-specific.
type EnvHierarchyLevel struct {
	// Position is the 1-based ordering of this level in the hierarchy.
	Position int `json:"position"`
	// IntegrationName is the human-readable name of the deploy integration.
	IntegrationName string `json:"integration_name"`
}

// StorageRetentionControls mirrors the storage_retention_controls payload for
// the BFF private API (GET/POST /private/orgs/{uuid}/storage-retention-controls).
// Field names are the same retention_days_* keys sent in the POST body.
type StorageRetentionControls struct {
	// CacheDays is the retention period for cache artifacts in days.
	CacheDays int `json:"retention_days_cache"`
	// WorkspaceDays is the retention period for workspace artifacts in days.
	WorkspaceDays int `json:"retention_days_workspace"`
	// ArtifactDays is the retention period for job artifacts in days.
	ArtifactDays int `json:"retention_days_artifact"`
}

// StorageRetentionLimits mirrors the plan-enforced min/max bounds for each
// retention type as returned alongside the controls by the BFF API.
type StorageRetentionLimits struct {
	Cache     StorageRetentionBound `json:"retention_days_cache"`
	Workspace StorageRetentionBound `json:"retention_days_workspace"`
	Artifact  StorageRetentionBound `json:"retention_days_artifact"`
}

// StorageRetentionBound is a min/max pair within StorageRetentionLimits.
type StorageRetentionBound struct {
	Min int `json:"min"`
	Max int `json:"max"`
}

// SSOSettings is a reference snapshot of an org's SSO (SAML) configuration.
type SSOSettings struct {
	// Enforced reports whether SSO login is enforced for the org.
	Enforced bool `json:"enforced"`
	// Realm is the SSO realm/identifier from the connection, when configured.
	Realm string `json:"realm,omitempty"`
	// Connection is the raw SSO connection body (IdP fields per the web-ui
	// SSOConnection shape), captured whole for reference. Nil when no connection
	// is configured. It is never written back to the destination.
	Connection map[string]any `json:"connection,omitempty"`
}

// OTelExporter is one OpenTelemetry exporter configuration on an org
// (EXPERIMENTAL feature; max 5 per org). Header values come back redacted as
// "xxxx" (encrypted at rest) and are captured for reference only.
type OTelExporter struct {
	Endpoint string            `json:"endpoint"`
	Protocol string            `json:"protocol"`
	Insecure bool              `json:"insecure"`
	Headers  map[string]string `json:"headers,omitempty"`
}

// OrgContacts holds the org's technical (primary) and security contact email
// lists. Each list may contain up to 5 addresses.
type OrgContacts struct {
	Primary  []string `json:"primary,omitempty"`
	Security []string `json:"security,omitempty"`
}

// AuditLogConfig is one audit-log streaming configuration on an org.
type AuditLogConfig struct {
	ID         string         `json:"id,omitempty"`
	Purpose    string         `json:"purpose,omitempty"`
	TargetType string         `json:"target_type,omitempty"`
	IsDisabled bool           `json:"is_disabled,omitempty"`
	Config     AuditLogTarget `json:"config"`
}

// AuditLogTarget is the destination (typically S3) of an audit-log config. All
// fields are environment-specific to the source org's AWS account.
type AuditLogTarget struct {
	ARN          string `json:"arn,omitempty"`
	Region       string `json:"region,omitempty"`
	BucketName   string `json:"bucket_name,omitempty"`
	BucketPrefix string `json:"bucket_prefix,omitempty"`
	Endpoint     string `json:"endpoint,omitempty"`
}

// URLOrbAllowEntry is one entry of a circleci-type org's URL-orb allow list.
type URLOrbAllowEntry struct {
	Name   string `json:"name"`
	Prefix string `json:"prefix"`
	Auth   string `json:"auth,omitempty"`
}

// Context is a CircleCI context and everything about it we can read.
type Context struct {
	Name string `json:"name"`
	// SourceID is the context's UUID in the source org. Informational only —
	// the destination assigns its own ID on creation.
	SourceID  string `json:"source_id,omitempty"`
	CreatedAt string `json:"created_at,omitempty"`
	// EnvVars lists variable names only; CircleCI never returns context values.
	EnvVars []ContextEnvVar `json:"environment_variables"`
	// Restrictions are project/expression/group restrictions from the REST API.
	Restrictions []Restriction `json:"restrictions,omitempty"`
	// SecurityGroups are the named groups allowed to use this context, derived
	// from the group-type restrictions (the v2 restrictions endpoint returns
	// group names).
	SecurityGroups []Group `json:"security_groups,omitempty"`
}

// ContextEnvVar is a context environment variable. Values are never available.
type ContextEnvVar struct {
	Name      string `json:"name"`
	CreatedAt string `json:"created_at,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

// Restriction is a context restriction as returned by the REST API.
type Restriction struct {
	Type string `json:"type"` // project | expression | group
	// Value is a project UUID, an expression string, or a group UUID depending
	// on Type.
	Value string `json:"value"`
	Name  string `json:"name,omitempty"`
}

// Group is a CircleCI security group (typically a VCS team).
type Group struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	GroupType string `json:"group_type,omitempty"` // TEAM | ORGANIZATION | ACCOUNT
}

// Project is a CircleCI project and the data we can read about it.
type Project struct {
	// Slug is "gh/<org>/<repo>" (GitHub OAuth) or
	// "circleci/<org-id>/<project-id>" (GitHub App / GitLab).
	Slug     string `json:"slug"`
	SourceID string `json:"source_id,omitempty"`
	Name     string `json:"name"`

	VCS      ProjectVCS        `json:"vcs"`
	Settings *AdvancedSettings `json:"settings,omitempty"`
	// EnvVars lists names plus the masked hint CircleCI returns (e.g.
	// "xxxx1234"); never the real value.
	EnvVars []ProjectEnvVar `json:"environment_variables"`

	// The fields below are part of the "capture everything" safety net. They
	// are recorded for completeness; recreation may land in a later milestone.
	CheckoutKeys []CheckoutKey `json:"checkout_keys,omitempty"`
	Webhooks     []Webhook     `json:"webhooks,omitempty"`
	Schedules    []Schedule    `json:"schedules,omitempty"`

	// SSHKeys captures the PUBLIC metadata for each additional SSH key
	// configured on the project (Project Settings → SSH Keys → Additional SSH
	// Keys).  The PRIVATE key is intentionally excluded — it is never returned
	// by the CircleCI API.  Private-key material must be captured separately
	// (e.g. via the secrets-extraction step) and re-added manually on the
	// destination.
	SSHKeys []ProjectSSHKey `json:"ssh_keys,omitempty"`

	// PipelineDefinitions captures the App-pipeline definitions for this project,
	// including their config/checkout sources and all attached triggers.
	PipelineDefinitions []PipelineDefinition `json:"pipeline_definitions,omitempty"`

	// OIDCAudience and OIDCTTL are per-project OIDC custom claims captured from
	// GET /api/v2/org/{orgID}/project/{projID}/oidc-custom-claims.
	// They mirror the org-level OIDCAudience/OIDCTTL fields in OrgSettings.
	OIDCAudience []string `json:"oidc_audience,omitempty"`
	OIDCTTL      string   `json:"oidc_ttl,omitempty"`

	// Followed records whether the source token's user follows this project.
	Followed *bool `json:"followed,omitempty"`
}

// ProjectSSHKey is the PUBLIC metadata for one additional SSH key on a project.
// The private key is intentionally excluded — it is NEVER returned by the
// CircleCI API.  Private-key material must be captured separately (via the
// ssh-key extraction step) and re-added on the destination after migration.
type ProjectSSHKey struct {
	// Hostname is the target host this key is scoped to (e.g. "github.com").
	// May be empty for globally-scoped additional SSH keys.
	Hostname string `json:"hostname,omitempty"`
	// PublicKey is the SSH public-key material (e.g. "ssh-rsa AAAA... user@host").
	PublicKey string `json:"public_key,omitempty"`
	// Fingerprint is the SHA256 fingerprint without the "SHA256:" prefix
	// (e.g. "Cv1BbZPFHMZzCPx+1CsJqO0kRBIlOm7DEqR/jPbHnBg=").
	Fingerprint string `json:"fingerprint,omitempty"`
}

// ProjectVCS holds version-control details for a project.
type ProjectVCS struct {
	Provider      string `json:"provider,omitempty"`
	URL           string `json:"url,omitempty"`
	DefaultBranch string `json:"default_branch,omitempty"`
}

// AdvancedSettings mirrors a project's advanced settings. Each field is a
// pointer so the export records only what the API actually returned, and so
// sync can apply exactly what was set.
type AdvancedSettings struct {
	AutocancelBuilds           *bool    `json:"autocancel_builds,omitempty"`
	BuildForkPRs               *bool    `json:"build_fork_prs,omitempty"`
	BuildPRsOnly               *bool    `json:"build_prs_only,omitempty"`
	DisableSSH                 *bool    `json:"disable_ssh,omitempty"`
	ForksReceiveSecretEnvVars  *bool    `json:"forks_receive_secret_env_vars,omitempty"`
	OSS                        *bool    `json:"oss,omitempty"`
	SetGitHubStatus            *bool    `json:"set_github_status,omitempty"`
	SetupWorkflows             *bool    `json:"setup_workflows,omitempty"`
	WriteSettingsRequiresAdmin *bool    `json:"write_settings_requires_admin,omitempty"`
	PROnlyBranchOverrides      []string `json:"pr_only_branch_overrides,omitempty"`

	// APITriggerWithConfig and DropAllBuildRequests are per-project feature
	// flags from the v1.1 project settings endpoint (GET/PUT
	// /api/v1.1/project/{slug}/settings, feature_flags blob).
	APITriggerWithConfig *bool `json:"api_trigger_with_config,omitempty"`
	DropAllBuildRequests *bool `json:"drop_all_build_requests,omitempty"`

	// V11FeatureFlags is the full set of bool-valued feature flags from the v1.1
	// project settings endpoint, capturing any flags beyond the two explicit
	// fields above. Keys are kebab-case (as returned by the API, e.g.
	// "api-trigger-with-config"). Nil/empty when no flags were returned.
	V11FeatureFlags map[string]bool `json:"v11_feature_flags,omitempty"`
}

// ProjectEnvVar is a project environment variable. MaskedValue is the
// "xxxx1234" hint, useful for verification but not the real value.
type ProjectEnvVar struct {
	Name        string `json:"name"`
	MaskedValue string `json:"masked_value,omitempty"`
	CreatedAt   string `json:"created_at,omitempty"`
}

// CheckoutKey is a project checkout/deploy key. Only public material is ever
// returned by the API; private keys must be regenerated on the destination.
type CheckoutKey struct {
	Type        string `json:"type"` // deploy-key | github-user-key
	Fingerprint string `json:"fingerprint,omitempty"`
	PublicKey   string `json:"public_key,omitempty"`
	Preferred   bool   `json:"preferred,omitempty"`
	CreatedAt   string `json:"created_at,omitempty"`
}

// Webhook is a CircleCI-managed outbound webhook on a project.
type Webhook struct {
	Name      string   `json:"name"`
	URL       string   `json:"url"`
	Events    []string `json:"events,omitempty"`
	VerifyTLS *bool    `json:"verify_tls,omitempty"`
}

// Schedule is a scheduled pipeline definition.
type Schedule struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Timetable   map[string]any `json:"timetable,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
	// ActorLogin is the CircleCI login of the attribution actor for this
	// schedule (the user identity under which the scheduled pipeline runs).
	// Captured for the migration runbook: the operator must attribute the
	// destination schedule to a valid user in the new org.
	// Empty when the actor login was not returned by the API.
	ActorLogin string `json:"actor_login,omitempty"`
}

// PipelineSource describes the code-source (config or checkout) for a pipeline
// definition. Provider is the VCS or config provider; RepoFullName and
// RepoExternalID identify the repository when applicable; FilePath is the
// config-file path within that repository.
type PipelineSource struct {
	Provider       string `json:"provider,omitempty"`
	RepoFullName   string `json:"repo_full_name,omitempty"`
	RepoExternalID string `json:"repo_external_id,omitempty"`
	FilePath       string `json:"file_path,omitempty"`
}

// PipelineDefinition is an App-pipeline definition captured from
// GET /api/v2/projects/{projectID}/pipeline-definitions.
type PipelineDefinition struct {
	Name           string         `json:"name"`
	Description    string         `json:"description,omitempty"`
	ConfigSource   PipelineSource `json:"config_source"`
	CheckoutSource PipelineSource `json:"checkout_source"`
	Triggers       []Trigger      `json:"triggers,omitempty"`
}

// TriggerEventSource is the flattened event-source of a pipeline trigger.
// Provider is one of github_app | github_server | github_oauth | webhook |
// schedule; the remaining fields are populated depending on provider type.
// The webhook URL is intentionally NOT stored (it contains a
// ?secret=**REDACTED** query parameter); only the sender identity is captured.
type TriggerEventSource struct {
	Provider       string `json:"provider"`
	RepoFullName   string `json:"repo_full_name,omitempty"`
	RepoExternalID string `json:"repo_external_id,omitempty"`
	WebhookSender  string `json:"webhook_sender,omitempty"`
	ScheduleCron   string `json:"schedule_cron,omitempty"`
	ScheduleActor  string `json:"schedule_actor,omitempty"`
}

// Trigger is one pipeline trigger attached to a pipeline definition.
type Trigger struct {
	Name        string             `json:"name"`
	EventName   string             `json:"event_name,omitempty"`
	Description string             `json:"description,omitempty"`
	EventPreset string             `json:"event_preset,omitempty"`
	Disabled    bool               `json:"disabled"`
	CheckoutRef string             `json:"checkout_ref,omitempty"`
	ConfigRef   string             `json:"config_ref,omitempty"`
	EventSource TriggerEventSource `json:"event_source"`
}

// Warning records something that could not be fully captured or migrated.
type Warning struct {
	// Scope identifies what the warning is about, e.g. "org",
	// "context:deploy-prod", or "project:gh/acme/web".
	Scope string `json:"scope"`
	// Code is a stable machine-readable identifier, e.g.
	// "context_value_unavailable" or "group_restriction_write_unsupported".
	Code    string `json:"code"`
	Message string `json:"message"`
}

// AddWarning appends a warning to the manifest.
func (m *Manifest) AddWarning(scope, code, message string) {
	m.Warnings = append(m.Warnings, Warning{Scope: scope, Code: code, Message: message})
}

// SortStable orders contexts, projects, their environment variables, pipeline
// definitions, and triggers by name so repeated exports of unchanged data
// produce identical files (clean diffs). It does not touch the warnings order
// (kept in discovery order).
func (m *Manifest) SortStable() {
	sort.SliceStable(m.Contexts, func(i, j int) bool { return m.Contexts[i].Name < m.Contexts[j].Name })
	sort.SliceStable(m.Projects, func(i, j int) bool { return m.Projects[i].Slug < m.Projects[j].Slug })
	for i := range m.Contexts {
		ev := m.Contexts[i].EnvVars
		sort.SliceStable(ev, func(a, b int) bool { return ev[a].Name < ev[b].Name })
	}
	for i := range m.Projects {
		ev := m.Projects[i].EnvVars
		sort.SliceStable(ev, func(a, b int) bool { return ev[a].Name < ev[b].Name })
		defs := m.Projects[i].PipelineDefinitions
		sort.SliceStable(defs, func(a, b int) bool { return defs[a].Name < defs[b].Name })
		for d := range defs {
			trigs := defs[d].Triggers
			sort.SliceStable(trigs, func(a, b int) bool { return trigs[a].Name < trigs[b].Name })
		}
	}
}

// Save writes the manifest to path as indented JSON.
func (m *Manifest) Save(path string) error {
	if m.SchemaVersion == "" {
		m.SchemaVersion = SchemaVersion
	}
	return writeJSON(path, m, 0o644)
}

// Load reads and validates a manifest from path.
func Load(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parsing manifest %s: %w", path, err)
	}
	if m.SchemaVersion != SchemaVersion {
		return nil, fmt.Errorf("manifest %s has unsupported schema version %q (this build supports %q)", path, m.SchemaVersion, SchemaVersion)
	}
	return &m, nil
}

// writeJSON marshals v as indented JSON and writes it to path with the given
// permissions, trailing newline included.
func writeJSON(path string, v any, perm os.FileMode) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, perm)
}
