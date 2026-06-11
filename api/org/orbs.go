package org

import (
	"context"
	"fmt"
	"net/url"
)

// OrgOrb is one orb entry returned by the private orb-list API.
// Only configuration fields are captured; runtime statistics are omitted.
type OrgOrb struct {
	// OrbName is the fully-qualified orb name (e.g. "acme/my-orb").
	OrbName string `json:"orb_name"`
	// LatestVersionNumber is the newest published version (e.g. "0.3.0").
	LatestVersionNumber string `json:"latest_version_number"`
	// OrbID is the server-assigned UUID for this orb.
	OrbID string `json:"orb_id"`
	// IsPrivate reports whether the orb is private (not listed publicly).
	IsPrivate bool `json:"is_private"`
	// Hidden reports whether the orb is hidden from search results.
	Hidden bool `json:"hidden"`
	// Description is the human-readable orb description.
	Description string `json:"description,omitempty"`
}

// orbListResponse mirrors GET /api/private/orb?org-id={orgUUID}.
type orbListResponse struct {
	Orbs          []OrgOrb `json:"orbs"`
	NextPageToken string   `json:"next_page_token"`
}

// GetOrgOrbs returns all orbs published in the given org. The endpoint is
// paginated; this method follows next_page_token until it is empty or the
// server returns an empty orbs list.
//
// Endpoint: GET https://app.circleci.com/api/private/orb?org-id={orgUUID}
// Pagination: append &page-token={next_page_token} for subsequent pages.
func (c *Client) GetOrgOrbs(ctx context.Context, orgUUID string) ([]OrgOrb, error) {
	var all []OrgOrb
	pageToken := ""

	for {
		raw, err := c.fetchOrbPage(ctx, orgUUID, pageToken)
		if err != nil {
			return nil, fmt.Errorf("GetOrgOrbs %s: %w", orgUUID, err)
		}
		all = append(all, raw.Orbs...)
		if raw.NextPageToken == "" || len(raw.Orbs) == 0 {
			break
		}
		pageToken = raw.NextPageToken
	}
	return all, nil
}

// fetchOrbPage fetches a single page of orbs. pageToken is empty for the first
// page; subsequent pages pass the token from the previous response.
func (c *Client) fetchOrbPage(ctx context.Context, orgUUID, pageToken string) (*orbListResponse, error) {
	rawPath := "api/private/orb?org-id=" + url.QueryEscape(orgUUID)
	if pageToken != "" {
		rawPath += "&page-token=" + url.QueryEscape(pageToken)
	}
	u, err := url.Parse(rawPath)
	if err != nil {
		return nil, fmt.Errorf("fetchOrbPage: build URL: %w", err)
	}

	req, err := c.app.NewRequest(ctx, "GET", u, nil)
	if err != nil {
		return nil, fmt.Errorf("fetchOrbPage: build request: %w", err)
	}

	var raw orbListResponse
	if _, err := c.app.DoRequest(req, &raw); err != nil {
		return nil, err
	}
	return &raw, nil
}
