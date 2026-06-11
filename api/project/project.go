package project

import (
	"context"
	"fmt"
	"net/url"
)

// Project represents a CircleCI project as returned by GET /api/v2/project/{project-slug}.
//
// JSON field names confirmed from:
//   - https://circleci.com/docs/api/v2/index.html (getProjectBySlug response schema)
//   - github.com/CircleCI-Public/circleci-cli api/project/project_rest.go (createProjectResponse)
type Project struct {
	Slug             string     `json:"slug"`
	ID               string     `json:"id"`
	Name             string     `json:"name"`
	OrganizationName string     `json:"organization_name"`
	OrganizationSlug string     `json:"organization_slug"`
	OrganizationID   string     `json:"organization_id"`
	VCS              ProjectVCS `json:"vcs_info"`
}

// ProjectVCS contains VCS-related metadata for a project.
// JSON field names confirmed from the API spec: vcs_url, provider, default_branch.
type ProjectVCS struct {
	Provider      string `json:"provider"`
	URL           string `json:"vcs_url"`
	DefaultBranch string `json:"default_branch"`
}

// GetProject retrieves a project by its slug (e.g. "gh/acme/web" or
// "circleci/<org-id>/<proj-id>").
//
// Endpoint: GET /api/v2/project/{project-slug}
//
// The project slug contains two '/' separators that the API treats as delimiters
// between three components (provider, org, repo).  Each component is
// percent-encoded individually via slugPath so that names containing spaces or
// other special characters are safe on the wire.  The literal '/' separators are
// preserved because the API accepts them unescaped.
func (c *Client) GetProject(ctx context.Context, slug string) (*Project, error) {
	u, err := slugPath("project/", slug)
	if err != nil {
		return nil, fmt.Errorf("GetProject: %w", err)
	}

	req, err := c.v2.NewRequest(ctx, "GET", u, nil)
	if err != nil {
		return nil, fmt.Errorf("GetProject: build request: %w", err)
	}

	var p Project
	if _, err := c.v2.DoRequest(req, &p); err != nil {
		return nil, fmt.Errorf("GetProject %q: %w", slug, err)
	}
	return &p, nil
}

// AdvancedSettings holds the project advanced settings returned by
// GET /api/v2/project/{provider}/{organization}/{project}/settings.
//
// All fields are *bool so that absent fields remain nil (distinguishable from
// an explicit false).  JSON field names confirmed from the API spec at
// https://circleci.com/docs/api/v2/index.html (getProjectSettings response schema).
type AdvancedSettings struct {
	AutocancelBuilds           *bool    `json:"autocancel_builds"`
	BuildForkPRs               *bool    `json:"build_fork_prs"`
	BuildPRsOnly               *bool    `json:"build_prs_only"`
	DisableSSH                 *bool    `json:"disable_ssh"`
	ForksReceiveSecretEnvVars  *bool    `json:"forks_receive_secret_env_vars"`
	OSS                        *bool    `json:"oss"`
	SetGithubStatus            *bool    `json:"set_github_status"`
	SetupWorkflows             *bool    `json:"setup_workflows"`
	WriteSettingsRequiresAdmin *bool    `json:"write_settings_requires_admin"`
	PROnlyBranchOverrides      []string `json:"pr_only_branch_overrides"`
}

// advancedSettingsResponse is the wire format wrapper for getProjectSettings.
// The API nests the advanced object inside a top-level "advanced" key.
type advancedSettingsResponse struct {
	Advanced AdvancedSettings `json:"advanced"`
}

// GetSettings returns the advanced project settings for the given project.
//
// Endpoint: GET /api/v2/project/{provider}/{organization}/{project}/settings
//
// The path uses the three components decomposed (not a pre-joined slug) so that
// each segment is encoded individually.  This is the same decomposed form used
// by the API spec parameter list for this endpoint.
func (c *Client) GetSettings(ctx context.Context, provider, org, proj string) (*AdvancedSettings, error) {
	path := "project/" +
		url.PathEscape(provider) + "/" +
		url.PathEscape(org) + "/" +
		url.PathEscape(proj) + "/settings"

	u, err := url.Parse(path)
	if err != nil {
		return nil, fmt.Errorf("GetSettings: build URL: %w", err)
	}

	req, err := c.v2.NewRequest(ctx, "GET", u, nil)
	if err != nil {
		return nil, fmt.Errorf("GetSettings: build request: %w", err)
	}

	var raw advancedSettingsResponse
	if _, err := c.v2.DoRequest(req, &raw); err != nil {
		return nil, fmt.Errorf("GetSettings %s/%s/%s: %w", provider, org, proj, err)
	}
	return &raw.Advanced, nil
}
