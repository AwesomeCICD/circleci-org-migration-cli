package project

import (
	"context"
	"fmt"
	"net/url"
)

// OrgProject represents a project entry as returned by the private project-list
// endpoint.  Both GitHub OAuth projects (slug "gh/org/repo") and GitHub App
// projects (slug "circleci/<orgUUID>/<projUUID>") are included.
//
// JSON field names confirmed from live HTTP 200 response of:
//
//	GET /api/private/project?organization-id={orgID}&page-size=50
//
// Response shape: {"items":[{"id":"…","slug":"…","name":"…"},...],
//
//	"next_page_token":"…"}
type OrgProject struct {
	ID   string `json:"id"`
	Slug string `json:"slug"`
	Name string `json:"name"`
}

type listOrgProjectsResponse struct {
	Items         []OrgProject `json:"items"`
	NextPageToken string       `json:"next_page_token"`
}

// ListOrgProjects returns all projects belonging to the given organization ID,
// fetching all pages automatically.  It covers both GitHub OAuth orgs (slugs
// of the form "gh/org/repo") and GitHub App orgs (slugs of the form
// "circleci/<orgUUID>/<projUUID>"), which the v1.1 followed-projects list does
// not include.
//
// Endpoint: GET /api/private/project?organization-id={orgID}&page-size=100
//
// Pagination is driven by the next_page_token field; additional pages are
// requested with the &page-token= query parameter.
func (c *Client) ListOrgProjects(ctx context.Context, orgID string) ([]OrgProject, error) {
	var all []OrgProject
	pageToken := ""

	for {
		q := url.Values{}
		q.Set("organization-id", orgID)
		// page-size is capped server-side: 50 succeeds, 100 returns HTTP 500.
		q.Set("page-size", "50")
		if pageToken != "" {
			q.Set("page-token", pageToken)
		}
		u := &url.URL{Path: "project", RawQuery: q.Encode()}

		req, err := c.private.NewRequest(ctx, "GET", u, nil)
		if err != nil {
			return nil, fmt.Errorf("ListOrgProjects: build request: %w", err)
		}

		var resp listOrgProjectsResponse
		if _, err := c.private.DoRequest(req, &resp); err != nil {
			return nil, fmt.Errorf("ListOrgProjects %q: %w", orgID, err)
		}

		all = append(all, resp.Items...)

		if resp.NextPageToken == "" {
			break
		}
		pageToken = resp.NextPageToken
	}

	return all, nil
}
