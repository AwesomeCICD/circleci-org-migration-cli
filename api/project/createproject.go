package project

import (
	"fmt"
	"net/url"
)

// createProjectShellRequest is the wire format for
// POST /api/v2/organization/{provider}/{org}/project.
//
// JSON shape confirmed from live API:
//
//	POST /api/v2/organization/{provider}/{org}/project
//	Body: {"name": "<repo>"}
//	Response 200: {"id","name","slug","organization_id",...}
type createProjectShellRequest struct {
	Name string `json:"name"`
}

// CreateProjectShell creates a project shell in the destination org without
// installing a webhook or triggering a build.  The caller must subsequently
// call FollowProject to install the webhook and enable builds.
//
// For OAuth destinations provider is the VCS short code (e.g. "github" or
// "gh") and org is the plain org name (not a UUID).
//
// Endpoint: POST /api/v2/organization/{provider}/{org}/project
// Request body: {"name": "<repo>"}
// Response 200: Project JSON (id, name, slug, organization_id, ...)
func (c *Client) CreateProjectShell(provider, org, name string) (*Project, error) {
	if provider == "" || org == "" || name == "" {
		return nil, fmt.Errorf("project: CreateProjectShell requires provider, org, and name")
	}

	path := "organization/" +
		url.PathEscape(provider) + "/" +
		url.PathEscape(org) + "/project"

	u, err := url.Parse(path)
	if err != nil {
		return nil, fmt.Errorf("project: CreateProjectShell: build URL: %w", err)
	}

	body := createProjectShellRequest{Name: name}
	req, err := c.v2.NewRequest("POST", u, &body)
	if err != nil {
		return nil, fmt.Errorf("project: CreateProjectShell: build request: %w", err)
	}

	var p Project
	if _, err := c.v2.DoRequest(req, &p); err != nil {
		return nil, fmt.Errorf("project: CreateProjectShell %s/%s/%s: %w", provider, org, name, err)
	}
	return &p, nil
}
