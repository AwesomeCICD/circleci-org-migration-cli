package org

import (
	"fmt"
	"net/url"
)

// ─────────────────────────────────────────────────────────────────────────────
// Block unregistered-user spend
// ─────────────────────────────────────────────────────────────────────────────

// blockUnregisteredUsersResponse mirrors GET/PUT
// /private/orgs/{orgUUID}/features/block-unregistered-users.
type blockUnregisteredUsersResponse struct {
	Enabled bool `json:"enabled"`
}

// blockUnregisteredUsersPath returns the relative URL for this feature endpoint.
func blockUnregisteredUsersPath(orgUUID string) (*url.URL, error) {
	return url.Parse("private/orgs/" + url.PathEscape(orgUUID) + "/features/block-unregistered-users")
}

// GetBlockUnregisteredUsers returns whether the org has enabled the
// "block unregistered user spend" feature.
//
// Endpoint: GET https://app.circleci.com/private/orgs/{orgUUID}/features/block-unregistered-users
func (c *Client) GetBlockUnregisteredUsers(orgUUID string) (bool, error) {
	u, err := blockUnregisteredUsersPath(orgUUID)
	if err != nil {
		return false, fmt.Errorf("GetBlockUnregisteredUsers: build URL: %w", err)
	}

	req, err := c.app.NewRequest("GET", u, nil)
	if err != nil {
		return false, fmt.Errorf("GetBlockUnregisteredUsers: build request: %w", err)
	}

	var raw blockUnregisteredUsersResponse
	if _, err := c.app.DoRequest(req, &raw); err != nil {
		return false, fmt.Errorf("GetBlockUnregisteredUsers %s: %w", orgUUID, err)
	}
	return raw.Enabled, nil
}

// SetBlockUnregisteredUsers enables or disables the "block unregistered user
// spend" feature for the org.
//
// Endpoint: PUT https://app.circleci.com/private/orgs/{orgUUID}/features/block-unregistered-users
func (c *Client) SetBlockUnregisteredUsers(orgUUID string, enabled bool) error {
	u, err := blockUnregisteredUsersPath(orgUUID)
	if err != nil {
		return fmt.Errorf("SetBlockUnregisteredUsers: build URL: %w", err)
	}

	body := blockUnregisteredUsersResponse{Enabled: enabled}
	req, err := c.app.NewRequest("PUT", u, body)
	if err != nil {
		return fmt.Errorf("SetBlockUnregisteredUsers: build request: %w", err)
	}

	// The PUT response body is not used. Pass a nil decode target so the
	// client treats any 2xx as success without inspecting the response
	// content-type: this endpoint can return a 200 with an empty body and no
	// Content-Type header, which must not be treated as an error (#166).
	// Non-2xx responses are still surfaced as errors by DoRequest.
	if _, err := c.app.DoRequest(req, nil); err != nil {
		return fmt.Errorf("SetBlockUnregisteredUsers %s: %w", orgUUID, err)
	}
	return nil
}
