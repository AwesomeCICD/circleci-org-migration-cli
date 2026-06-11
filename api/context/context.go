package context

import (
	"context"
	"fmt"
	"net/url"
)

// Context represents a CircleCI context as returned by GET /api/v2/context.
// JSON source: https://circleci.com/docs/api/v2/index.html#operation/listContexts
type Context struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	CreatedAt string `json:"created_at"`
}

type listContextsResponse struct {
	Items         []Context `json:"items"`
	NextPageToken string    `json:"next_page_token"`
}

// ListContexts returns all contexts visible to the given owner.
// Exactly one of ownerID or ownerSlug must be non-empty; ownerID is preferred.
// ownerSlug has the form "<vcs>/<org>" (e.g. "github/myorg").
// All pages are fetched automatically.
func (c *Client) ListContexts(ctx context.Context, ownerID, ownerSlug string) ([]Context, error) {
	if ownerID == "" && ownerSlug == "" {
		return nil, fmt.Errorf("context: ListContexts requires ownerID or ownerSlug")
	}

	var all []Context
	pageToken := ""

	for {
		u, err := c.rest.BaseURL.Parse("context")
		if err != nil {
			return nil, err
		}
		q := url.Values{}
		if ownerID != "" {
			q.Set("owner-id", ownerID)
		} else {
			q.Set("owner-slug", ownerSlug)
		}
		if pageToken != "" {
			q.Set("page-token", pageToken)
		}
		u.RawQuery = q.Encode()

		req, err := c.rest.NewRequest(ctx, "GET", u, nil)
		if err != nil {
			return nil, err
		}

		var resp listContextsResponse
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
