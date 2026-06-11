package org

import (
	"context"
	"fmt"
	"net/url"
)

// Group is a CIAM org group (a named set of members used for context group
// restrictions). The "All members" group's UUID equals the org id.
//
// JSON field names confirmed against web-ui
// org-settings/hooks/groups/useGroups.ts (APIGroup): {id, name, description,
// created_at, creator_id, org_id, updated_at, updater_id, members_count?}.
// There is no group_type field on this endpoint.
type Group struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// groupsResponse mirrors GET /private/ciam/orgs/{orgID}/groups.
type groupsResponse struct {
	Items []Group `json:"items"`
}

// ListGroups returns the org's CIAM groups.
//
// Endpoint: GET https://app.circleci.com/private/ciam/orgs/{orgID}/groups
//
// NOTE: this is served by app.circleci.com (NOT circleci.com); the org client's
// app base URL handles that host rewrite. The token travels via the Circle-Token
// header like every other request.
func (c *Client) ListGroups(ctx context.Context, orgID string) ([]Group, error) {
	u, err := url.Parse("private/ciam/orgs/" + url.PathEscape(orgID) + "/groups")
	if err != nil {
		return nil, fmt.Errorf("ListGroups: build URL: %w", err)
	}

	req, err := c.app.NewRequest(ctx, "GET", u, nil)
	if err != nil {
		return nil, fmt.Errorf("ListGroups: build request: %w", err)
	}

	var raw groupsResponse
	if _, err := c.app.DoRequest(req, &raw); err != nil {
		return nil, fmt.Errorf("ListGroups %s: %w", orgID, err)
	}
	return raw.Items, nil
}
