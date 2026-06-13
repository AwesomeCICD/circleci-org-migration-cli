package project

import (
	"context"
	"fmt"
	"net/url"
)

// createEnvVarRequest is the wire format for POST /project/{slug}/envvar.
//
// JSON shape confirmed from:
//   - github.com/CircleCI-Public/circleci-cli api/project/project_rest.go (createProjectEnvVarRequest)
//   - CircleCI API v2 docs: POST /project/{project-slug}/envvar body {"name","value"}, response 201
type createEnvVarRequest struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// CreateEnvVar creates a new environment variable on the given project.
// If a variable with the same name already exists it is replaced.
//
// Endpoint: POST /api/v2/project/{project-slug}/envvar
// Request body: {"name": "<name>", "value": "<value>"}
// The slug is encoded using the same slug-path convention as the read methods
// (each component is percent-encoded individually; literal '/' separators are
// kept as delimiters).
func (c *Client) CreateEnvVar(ctx context.Context, slug, name, value string) error {
	if slug == "" {
		return fmt.Errorf("project: CreateEnvVar requires slug")
	}
	if name == "" {
		return fmt.Errorf("project: CreateEnvVar requires name")
	}

	u, err := slugSubresource(slug, "envvar")
	if err != nil {
		return fmt.Errorf("project: CreateEnvVar: %w", err)
	}

	body := createEnvVarRequest{Name: name, Value: value}
	req, err := c.v2.NewRequest(ctx, "POST", u, &body)
	if err != nil {
		return fmt.Errorf("project: CreateEnvVar: build request: %w", err)
	}

	var resp EnvVar
	if _, err := c.v2.DoRequest(req, &resp); err != nil {
		return fmt.Errorf("project: CreateEnvVar %q/%q: %w", slug, name, err)
	}
	return nil
}

// advancedSettingsPatch mirrors AdvancedSettings but uses omitempty on every
// field so that nil *bool fields and nil/empty slices are omitted from the
// PATCH request body.  This keeps the PATCH semantics correct — only the fields
// the caller actually sets are forwarded to the API — without changing the
// AdvancedSettings read type.
//
// JSON shape confirmed from CircleCI API v2 docs:
//
//	PATCH /project/{provider}/{organization}/{project}/settings
//	Body: {"advanced": { ... only the fields to update ... }}
//	Response: 200 with the full advancedSettingsResponse.
//
// NOTE: the "oss" field is intentionally absent from this patch struct.
// The CircleCI project-settings PATCH endpoint rejects it with
// "Unexpected field 'advanced.oss'" for all project types (GitHub OAuth and
// GitHub App).  The Terraform provider has no "oss" attribute either (same
// root issue on the imperative path).  See issue #247.
// The field is kept on AdvancedSettings (the READ type) so that captured
// manifests still record the value for reporting; it is simply never sent
// in write calls.
type advancedSettingsPatch struct {
	AutocancelBuilds           *bool    `json:"autocancel_builds,omitempty"`
	BuildForkPRs               *bool    `json:"build_fork_prs,omitempty"`
	BuildPRsOnly               *bool    `json:"build_prs_only,omitempty"`
	DisableSSH                 *bool    `json:"disable_ssh,omitempty"`
	ForksReceiveSecretEnvVars  *bool    `json:"forks_receive_secret_env_vars,omitempty"`
	SetGithubStatus            *bool    `json:"set_github_status,omitempty"`
	SetupWorkflows             *bool    `json:"setup_workflows,omitempty"`
	WriteSettingsRequiresAdmin *bool    `json:"write_settings_requires_admin,omitempty"`
	PROnlyBranchOverrides      []string `json:"pr_only_branch_overrides,omitempty"`
}

type updateSettingsRequest struct {
	Advanced advancedSettingsPatch `json:"advanced"`
}

// UpdateSettings applies a partial update to a project's advanced settings.
//
// Endpoint: PATCH /api/v2/project/{provider}/{organization}/{project}/settings
// Request body: {"advanced": { <only the non-nil fields of s> }}
// The path uses the decomposed form (provider/org/project as separate URL
// path segments, each individually percent-encoded), matching the convention
// already used by GetSettings.
//
// Only fields explicitly set in s (non-nil *bool, non-empty PROnlyBranchOverrides)
// are included in the request body; all other fields are omitted via omitempty.
func (c *Client) UpdateSettings(ctx context.Context, provider, org, proj string, s *AdvancedSettings) error {
	if provider == "" || org == "" || proj == "" {
		return fmt.Errorf("project: UpdateSettings requires provider, org, and proj")
	}
	if s == nil {
		return fmt.Errorf("project: UpdateSettings requires non-nil settings")
	}

	path := "project/" +
		url.PathEscape(provider) + "/" +
		url.PathEscape(org) + "/" +
		url.PathEscape(proj) + "/settings"

	u, err := url.Parse(path)
	if err != nil {
		return fmt.Errorf("project: UpdateSettings: build URL: %w", err)
	}

	// OSS is intentionally omitted — the API rejects it with
	// "Unexpected field 'advanced.oss'" for all project types. (#247)
	patch := advancedSettingsPatch{
		AutocancelBuilds:           s.AutocancelBuilds,
		BuildForkPRs:               s.BuildForkPRs,
		BuildPRsOnly:               s.BuildPRsOnly,
		DisableSSH:                 s.DisableSSH,
		ForksReceiveSecretEnvVars:  s.ForksReceiveSecretEnvVars,
		SetGithubStatus:            s.SetGithubStatus,
		SetupWorkflows:             s.SetupWorkflows,
		WriteSettingsRequiresAdmin: s.WriteSettingsRequiresAdmin,
		PROnlyBranchOverrides:      s.PROnlyBranchOverrides,
	}
	body := updateSettingsRequest{Advanced: patch}
	req, err := c.v2.NewRequest(ctx, "PATCH", u, &body)
	if err != nil {
		return fmt.Errorf("project: UpdateSettings: build request: %w", err)
	}

	var raw advancedSettingsResponse
	if _, err := c.v2.DoRequest(req, &raw); err != nil {
		return fmt.Errorf("project: UpdateSettings %s/%s/%s: %w", provider, org, proj, err)
	}
	return nil
}
