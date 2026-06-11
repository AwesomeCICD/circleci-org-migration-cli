package project

import (
	"context"
	"fmt"
	"net/url"
)

// Webhook represents a CircleCI outbound webhook as returned by
// GET /api/v2/webhook?scope-id={projectID}&scope-type=project.
//
// JSON field names confirmed from:
//   - https://circleci.com/docs/api/v2/index.html (getWebhooks response schema)
//
// Note: The API uses hyphenated field names (verify-tls) matching the spec.
// VerifyTLS is a pointer so that it is distinguishable from an absent field,
// consistent with the spec marking it as required but defensive against servers
// that omit it.
type Webhook struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	URL       string   `json:"url"`
	Events    []string `json:"events"`
	VerifyTLS *bool    `json:"verify-tls"`
}

type listWebhooksResponse struct {
	Items         []Webhook `json:"items"`
	NextPageToken string    `json:"next_page_token"`
}

// ListWebhooks returns all outbound webhooks scoped to the given project ID,
// fetching all pages automatically.
//
// Endpoint: GET /api/v2/webhook?scope-id={projectID}&scope-type=project
//
// projectID must be the UUID of the project (not the slug).  The scope-type is
// always "project" for this helper.
func (c *Client) ListWebhooks(ctx context.Context, projectID string) ([]Webhook, error) {
	var all []Webhook
	pageToken := ""

	for {
		u := &url.URL{Path: "webhook"}
		q := url.Values{}
		q.Set("scope-id", projectID)
		q.Set("scope-type", "project")
		if pageToken != "" {
			q.Set("page-token", pageToken)
		}
		u.RawQuery = q.Encode()

		req, err := c.v2.NewRequest(ctx, "GET", u, nil)
		if err != nil {
			return nil, fmt.Errorf("ListWebhooks: build request: %w", err)
		}

		var resp listWebhooksResponse
		if _, err := c.v2.DoRequest(req, &resp); err != nil {
			return nil, fmt.Errorf("ListWebhooks %q: %w", projectID, err)
		}

		all = append(all, resp.Items...)

		if resp.NextPageToken == "" {
			break
		}
		pageToken = resp.NextPageToken
	}

	return all, nil
}
