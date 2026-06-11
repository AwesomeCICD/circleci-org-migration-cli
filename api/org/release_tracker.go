package org

import (
	"context"
	"fmt"
	"net/url"
)

// ReleaseTrackerSettings holds the org-level release-tracker configuration.
// InconclusiveReleaseTTL is a duration string (e.g. "1h") that controls how
// long an inconclusive release is retained before being marked expired.
// The struct is nil-safe: an empty response body ({}) is treated as no settings.
type ReleaseTrackerSettings struct {
	InconclusiveReleaseTTL string `json:"inconclusive_release_ttl,omitempty"`
}

// releaseTrackerPath returns the relative URL for the release-tracker settings
// endpoint for the given org UUID.
func releaseTrackerPath(orgUUID string) (*url.URL, error) {
	return url.Parse("private/release-tracker/v1/organization/" + url.PathEscape(orgUUID) + "/settings")
}

// GetReleaseTrackerSettings returns the org's release-tracker settings.
// When no settings are configured the server returns {} and this method
// returns nil (not an error).
//
// Endpoint: GET https://app.circleci.com/private/release-tracker/v1/organization/{orgUUID}/settings
func (c *Client) GetReleaseTrackerSettings(ctx context.Context, orgUUID string) (*ReleaseTrackerSettings, error) {
	u, err := releaseTrackerPath(orgUUID)
	if err != nil {
		return nil, fmt.Errorf("GetReleaseTrackerSettings: build URL: %w", err)
	}

	req, err := c.app.NewRequest(ctx, "GET", u, nil)
	if err != nil {
		return nil, fmt.Errorf("GetReleaseTrackerSettings: build request: %w", err)
	}

	var raw ReleaseTrackerSettings
	if _, err := c.app.DoRequest(req, &raw); err != nil {
		return nil, fmt.Errorf("GetReleaseTrackerSettings %s: %w", orgUUID, err)
	}

	// An all-zero value means the server returned {} — treat as no settings.
	if raw.InconclusiveReleaseTTL == "" {
		return nil, nil
	}
	return &raw, nil
}

// SetReleaseTrackerSettings applies release-tracker settings to the org via
// PATCH.
//
// Endpoint: PATCH https://app.circleci.com/private/release-tracker/v1/organization/{orgUUID}/settings
func (c *Client) SetReleaseTrackerSettings(ctx context.Context, orgUUID string, settings ReleaseTrackerSettings) error {
	u, err := releaseTrackerPath(orgUUID)
	if err != nil {
		return fmt.Errorf("SetReleaseTrackerSettings: build URL: %w", err)
	}

	req, err := c.app.NewRequest(ctx, "PATCH", u, settings)
	if err != nil {
		return fmt.Errorf("SetReleaseTrackerSettings: build request: %w", err)
	}

	var ignored map[string]any
	if _, err := c.app.DoRequest(req, &ignored); err != nil {
		return fmt.Errorf("SetReleaseTrackerSettings %s: %w", orgUUID, err)
	}
	return nil
}
