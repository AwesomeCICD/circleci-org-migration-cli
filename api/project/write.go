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

// ossOnlyPatch is a minimal PATCH body that sets only the oss field.
// It is used for the best-effort SetOSS call (a separate PATCH from the main
// advanced-settings update) so that a rejection by the API ("Unexpected field
// 'advanced.oss'" on GitHub App projects) does not affect the main settings
// write.
type ossOnlyPatch struct {
	Advanced ossOnlyAdvanced `json:"advanced"`
}

type ossOnlyAdvanced struct {
	OSS bool `json:"oss"`
}

// SetOSS attempts to enable the "Free and Open Source" flag on the given
// project via a dedicated PATCH {"advanced":{"oss":true}}.
//
// It returns applied=true when the API echoed oss=true back in its response,
// and applied=false when:
//   - the API returned an error (including the "Unexpected field 'advanced.oss'"
//     400 that GitHub App projects always return), or
//   - the response did not include oss=true (e.g. private-repo no-op on OAuth).
//
// The error return is nil in the "Unexpected field" case — it is treated as
// "not applied" rather than a hard failure, so callers can record a warning
// without failing the overall sync.  Any other non-2xx response is returned
// as an error so callers can distinguish a transient failure from a silent
// no-op.
//
// Endpoint: PATCH /api/v2/project/{provider}/{organization}/{project}/settings
// Body:     {"advanced":{"oss":true}}
func (c *Client) SetOSS(ctx context.Context, provider, org, proj string) (applied bool, err error) {
	if provider == "" || org == "" || proj == "" {
		return false, fmt.Errorf("project: SetOSS requires provider, org, and proj")
	}

	path := "project/" +
		url.PathEscape(provider) + "/" +
		url.PathEscape(org) + "/" +
		url.PathEscape(proj) + "/settings"

	u, err := url.Parse(path)
	if err != nil {
		return false, fmt.Errorf("project: SetOSS: build URL: %w", err)
	}

	body := ossOnlyPatch{Advanced: ossOnlyAdvanced{OSS: true}}
	req, err := c.v2.NewRequest(ctx, "PATCH", u, &body)
	if err != nil {
		return false, fmt.Errorf("project: SetOSS: build request: %w", err)
	}

	var raw advancedSettingsResponse
	if _, doErr := c.v2.DoRequest(req, &raw); doErr != nil {
		// "Unexpected field 'advanced.oss'" (400) means the project type does
		// not support the oss field (GitHub App projects, or any other future
		// rejection).  Treat it as not-applied rather than an error so the
		// caller can record a warning and keep going.
		msg := doErr.Error()
		if containsUnexpectedOSSField(msg) {
			return false, nil
		}
		return false, fmt.Errorf("project: SetOSS %s/%s/%s: %w", provider, org, proj, doErr)
	}

	// The API echoed the settings back; check whether oss is now true.
	applied = raw.Advanced.OSS != nil && *raw.Advanced.OSS
	return applied, nil
}

// containsUnexpectedOSSField returns true when the error message indicates the
// API rejected the oss field with "Unexpected field 'advanced.oss'" or the
// similar lower-case variant.  This is the canonical signal that the project
// type does not support the oss field.
func containsUnexpectedOSSField(msg string) bool {
	return containsFold(msg, "unexpected field") && containsFold(msg, "oss")
}

// containsFold is a case-insensitive substring search without importing strings.
func containsFold(s, sub string) bool {
	if len(sub) == 0 {
		return true
	}
	if len(sub) > len(s) {
		return false
	}
	for i := 0; i <= len(s)-len(sub); i++ {
		match := true
		for j := 0; j < len(sub); j++ {
			sc := s[i+j]
			tc := sub[j]
			// ASCII-only tolower.
			if sc >= 'A' && sc <= 'Z' {
				sc += 'a' - 'A'
			}
			if tc >= 'A' && tc <= 'Z' {
				tc += 'a' - 'A'
			}
			if sc != tc {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
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
