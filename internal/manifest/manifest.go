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
	// Warnings records anything that could not be fully captured (for example
	// a context secret value, which CircleCI never exposes via API). These are
	// surfaced in the audit report so nothing is dropped silently.
	Warnings []Warning `json:"warnings,omitempty"`
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

	// Followed records whether the source token's user follows this project.
	Followed *bool `json:"followed,omitempty"`
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

// SortStable orders contexts, projects, and their environment variables by
// name so repeated exports of unchanged data produce identical files (clean
// diffs). It does not touch the warnings order (kept in discovery order).
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
