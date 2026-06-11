package project

import (
	"context"
	"fmt"
	"net/url"
)

// FollowedProject represents a project entry as returned by
// GET /api/v1.1/projects (the list of all projects the authenticated user follows).
//
// JSON field names confirmed from:
//   - https://circleci.com/docs/api/v1/index.html#projects (Get All Followed Projects)
//
// The response is a flat JSON array (not paginated).
type FollowedProject struct {
	VCSType  string `json:"vcs_type"`
	Username string `json:"username"`
	Reponame string `json:"reponame"`
	VCSURL   string `json:"vcs_url"`
	Followed bool   `json:"followed"`
}

// FollowResult is returned by FollowProject when the follow POST succeeds.
//
// JSON field names confirmed from:
//   - https://circleci.com/docs/api/v1/index.html#follow-a-new-project-on-circleci
type FollowResult struct {
	Followed bool `json:"followed"`
}

// ListFollowedProjects returns all projects that the authenticated user follows.
//
// Endpoint: GET /api/v1.1/projects
//
// The response is a flat JSON array and is not paginated; all results are
// returned in a single request.
func (c *Client) ListFollowedProjects(ctx context.Context) ([]FollowedProject, error) {
	u := &url.URL{Path: "projects"}

	req, err := c.v11.NewRequest(ctx, "GET", u, nil)
	if err != nil {
		return nil, fmt.Errorf("ListFollowedProjects: build request: %w", err)
	}

	var projects []FollowedProject
	if _, err := c.v11.DoRequest(req, &projects); err != nil {
		return nil, fmt.Errorf("ListFollowedProjects: %w", err)
	}
	return projects, nil
}

// FollowedProjectsForOrg filters the results of ListFollowedProjects to those
// whose Username (org name) matches orgName.
func (c *Client) FollowedProjectsForOrg(ctx context.Context, orgName string) ([]FollowedProject, error) {
	all, err := c.ListFollowedProjects(ctx)
	if err != nil {
		return nil, err
	}

	var filtered []FollowedProject
	for _, p := range all {
		if p.Username == orgName {
			filtered = append(filtered, p)
		}
	}
	return filtered, nil
}

// FollowProject follows a project on CircleCI using the v1.1 API.
//
// Endpoint: POST /api/v1.1/project/{vcsType}/{org}/{repo}/follow
//
// WARNING: This is a WRITE operation.  Following a project installs a deploy
// key and webhook on the VCS repository, and may trigger an initial build.
// The command layer MUST gate this behind an explicit user opt-in (e.g. a
// --follow flag or confirmation prompt) before calling this method.
func (c *Client) FollowProject(ctx context.Context, vcsType, org, repo string) (*FollowResult, error) {
	path := "project/" +
		url.PathEscape(vcsType) + "/" +
		url.PathEscape(org) + "/" +
		url.PathEscape(repo) + "/follow"

	u, err := url.Parse(path)
	if err != nil {
		return nil, fmt.Errorf("FollowProject: build URL: %w", err)
	}

	req, err := c.v11.NewRequest(ctx, "POST", u, nil)
	if err != nil {
		return nil, fmt.Errorf("FollowProject: build request: %w", err)
	}

	var result FollowResult
	if _, err := c.v11.DoRequest(req, &result); err != nil {
		return nil, fmt.Errorf("FollowProject %s/%s/%s: %w", vcsType, org, repo, err)
	}
	return &result, nil
}
