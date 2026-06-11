package project

import (
	"context"
	"fmt"
	"net/url"
)

// PipelineSource describes the code-source (config or checkout) attached to a
// pipeline definition.  The provider field indicates where the source lives;
// the remaining fields are populated depending on provider type.
//
// JSON field names confirmed from live HTTP 200 response of:
//
//	GET /api/v2/projects/{projectID}/pipeline-definitions
//
// Response shape per item:
//
//	{"id":"…","name":"…","description":"…","created_at":"…",
//	 "config_source":{"provider":"…","repo":{"full_name":"…","external_id":"…"},"file_path":"…"},
//	 "checkout_source":{"provider":"…","repo":{"full_name":"…","external_id":"…"}}}
type PipelineSource struct {
	Provider string             `json:"provider"`
	Repo     PipelineSourceRepo `json:"repo,omitempty"`
	FilePath string             `json:"file_path,omitempty"`
}

// PipelineSourceRepo holds the repository identity embedded in a PipelineSource.
type PipelineSourceRepo struct {
	FullName   string `json:"full_name,omitempty"`
	ExternalID string `json:"external_id,omitempty"`
}

// PipelineDefinition represents a pipeline definition as returned by
// GET /api/v2/projects/{projectID}/pipeline-definitions.
//
// JSON field names confirmed from live HTTP 200 response.
type PipelineDefinition struct {
	ID             string         `json:"id"`
	Name           string         `json:"name"`
	Description    string         `json:"description,omitempty"`
	CreatedAt      string         `json:"created_at,omitempty"`
	ConfigSource   PipelineSource `json:"config_source"`
	CheckoutSource PipelineSource `json:"checkout_source"`
}

type listPipelineDefinitionsResponse struct {
	Items         []PipelineDefinition `json:"items"`
	NextPageToken string               `json:"next_page_token"`
}

// ListPipelineDefinitions returns all pipeline definitions for the given project
// ID, fetching all pages automatically.
//
// Endpoint: GET /api/v2/projects/{projectID}/pipeline-definitions
//
// projectID must be the UUID of the project (not the slug).
func (c *Client) ListPipelineDefinitions(ctx context.Context, projectID string) ([]PipelineDefinition, error) {
	var all []PipelineDefinition
	pageToken := ""

	for {
		path := "projects/" + url.PathEscape(projectID) + "/pipeline-definitions"
		u, err := url.Parse(path)
		if err != nil {
			return nil, fmt.Errorf("ListPipelineDefinitions: build URL: %w", err)
		}
		if pageToken != "" {
			q := url.Values{}
			q.Set("page-token", pageToken)
			u.RawQuery = q.Encode()
		}

		req, err := c.v2.NewRequest(ctx, "GET", u, nil)
		if err != nil {
			return nil, fmt.Errorf("ListPipelineDefinitions: build request: %w", err)
		}

		var resp listPipelineDefinitionsResponse
		if _, err := c.v2.DoRequest(req, &resp); err != nil {
			return nil, fmt.Errorf("ListPipelineDefinitions %q: %w", projectID, err)
		}

		all = append(all, resp.Items...)

		if resp.NextPageToken == "" {
			break
		}
		pageToken = resp.NextPageToken
	}

	return all, nil
}
