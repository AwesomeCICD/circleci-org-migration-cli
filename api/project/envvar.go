package project

import (
	"context"
	"fmt"
	"net/url"
)

// EnvVar represents a project environment variable as returned by
// GET /api/v2/project/{project-slug}/envvar.
//
// JSON field names confirmed from:
//   - https://circleci.com/docs/api/v2/index.html (listEnvVars response schema)
//   - github.com/CircleCI-Public/circleci-cli api/project/project_rest.go
//
// The API returns a masked value (e.g. "xxxx1234") rather than the real secret.
// We store it as MaskedValue to make the intent explicit in calling code.
type EnvVar struct {
	Name        string `json:"name"`
	MaskedValue string `json:"value"`
	CreatedAt   string `json:"created-at"`
}

type listEnvVarsResponse struct {
	Items         []EnvVar `json:"items"`
	NextPageToken string   `json:"next_page_token"`
}

// ListEnvVars returns all environment variables for the given project slug,
// fetching all pages automatically.
//
// Endpoint: GET /api/v2/project/{project-slug}/envvar
//
// The value field in each EnvVar is the API's masked representation
// (e.g. "xxxx1234") and is never the real secret.
func (c *Client) ListEnvVars(ctx context.Context, slug string) ([]EnvVar, error) {
	var all []EnvVar
	pageToken := ""

	for {
		u, err := slugSubresource(slug, "envvar")
		if err != nil {
			return nil, fmt.Errorf("ListEnvVars: %w", err)
		}
		if pageToken != "" {
			q := url.Values{}
			q.Set("page-token", pageToken)
			u.RawQuery = q.Encode()
		}

		req, err := c.v2.NewRequest(ctx, "GET", u, nil)
		if err != nil {
			return nil, fmt.Errorf("ListEnvVars: build request: %w", err)
		}

		var resp listEnvVarsResponse
		if _, err := c.v2.DoRequest(req, &resp); err != nil {
			return nil, fmt.Errorf("ListEnvVars %q: %w", slug, err)
		}

		all = append(all, resp.Items...)

		if resp.NextPageToken == "" {
			break
		}
		pageToken = resp.NextPageToken
	}

	return all, nil
}
