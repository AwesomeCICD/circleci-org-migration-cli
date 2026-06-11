package project

import (
	"context"
	"fmt"
	"net/url"
)

// CheckoutKey represents a project checkout key as returned by
// GET /api/v2/project/{project-slug}/checkout-key.
//
// JSON field names confirmed from:
//   - https://circleci.com/docs/api/v2/index.html (listCheckoutKeys response schema)
//
// Note: JSON uses hyphenated field names (public-key, created-at) rather than
// underscored, matching the v2 API spec.
type CheckoutKey struct {
	Type        string `json:"type"`
	Fingerprint string `json:"fingerprint"`
	PublicKey   string `json:"public-key"`
	Preferred   bool   `json:"preferred"`
	CreatedAt   string `json:"created-at"`
}

type listCheckoutKeysResponse struct {
	Items         []CheckoutKey `json:"items"`
	NextPageToken string        `json:"next_page_token"`
}

// ListCheckoutKeys returns all checkout keys for the given project slug,
// fetching all pages automatically.
//
// Endpoint: GET /api/v2/project/{project-slug}/checkout-key
func (c *Client) ListCheckoutKeys(ctx context.Context, slug string) ([]CheckoutKey, error) {
	var all []CheckoutKey
	pageToken := ""

	for {
		u, err := slugSubresource(slug, "checkout-key")
		if err != nil {
			return nil, fmt.Errorf("ListCheckoutKeys: %w", err)
		}
		if pageToken != "" {
			q := url.Values{}
			q.Set("page-token", pageToken)
			u.RawQuery = q.Encode()
		}

		req, err := c.v2.NewRequest(ctx, "GET", u, nil)
		if err != nil {
			return nil, fmt.Errorf("ListCheckoutKeys: build request: %w", err)
		}

		var resp listCheckoutKeysResponse
		if _, err := c.v2.DoRequest(req, &resp); err != nil {
			return nil, fmt.Errorf("ListCheckoutKeys %q: %w", slug, err)
		}

		all = append(all, resp.Items...)

		if resp.NextPageToken == "" {
			break
		}
		pageToken = resp.NextPageToken
	}

	return all, nil
}
