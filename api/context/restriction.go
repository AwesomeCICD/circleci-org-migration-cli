package context

import (
	"context"
	"fmt"
)

// Restriction represents a project, expression, or group restriction on a
// CircleCI context as returned by GET /api/v2/context/{id}/restrictions.
//
// JSON shape confirmed from CircleCI API v2 docs:
//
//	{
//	  "id": "<uuid>",
//	  "context_id": "<uuid>",
//	  "name": "<human-readable name or null>",
//	  "restriction_type": "project" | "expression" | "group",
//	  "restriction_value": "<project-uuid | expression-string | group-uuid>"
//	}
type Restriction struct {
	ID        string `json:"id"`
	Type      string `json:"restriction_type"`
	Value     string `json:"restriction_value"`
	Name      string `json:"name"`
	ContextID string `json:"context_id"`
}

type listRestrictionsResponse struct {
	Items         []Restriction `json:"items"`
	NextPageToken string        `json:"next_page_token"`
}

// ListRestrictions returns all restrictions attached to a context.
// All pages are fetched automatically.
func (c *Client) ListRestrictions(ctx context.Context, contextID string) ([]Restriction, error) {
	if contextID == "" {
		return nil, fmt.Errorf("context: ListRestrictions requires contextID")
	}

	var all []Restriction
	pageToken := ""

	for {
		path := fmt.Sprintf("context/%s/restrictions", contextID)
		u, err := c.rest.BaseURL.Parse(path)
		if err != nil {
			return nil, err
		}
		if pageToken != "" {
			q := u.Query()
			q.Set("page-token", pageToken)
			u.RawQuery = q.Encode()
		}

		req, err := c.rest.NewRequest(ctx, "GET", u, nil)
		if err != nil {
			return nil, err
		}

		var resp listRestrictionsResponse
		if _, err := c.rest.DoRequest(req, &resp); err != nil {
			return nil, err
		}

		all = append(all, resp.Items...)

		if resp.NextPageToken == "" {
			break
		}
		pageToken = resp.NextPageToken
	}

	return all, nil
}
