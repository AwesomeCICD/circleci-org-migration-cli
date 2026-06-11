package org

import (
	"context"
	"fmt"
	"net/url"
)

// StorageRetentionControls holds the per-org artifact/cache/workspace retention
// settings that can be read and written via the BFF private API.
type StorageRetentionControls struct {
	// CacheDays is the retention period for cache artifacts in days.
	CacheDays int `json:"retention_days_cache"`
	// WorkspaceDays is the retention period for workspace artifacts in days.
	WorkspaceDays int `json:"retention_days_workspace"`
	// ArtifactDays is the retention period for job artifacts in days.
	ArtifactDays int `json:"retention_days_artifact"`
}

// StorageRetention is the response from GET
// /private/orgs/{orgUUID}/storage-retention-controls. The current controls are
// in Controls; Limits holds the plan-enforced min/max bounds.
type StorageRetention struct {
	Controls StorageRetentionControls `json:"storage_retention_controls"`
	Limits   StorageRetentionLimits   `json:"storage_retention_limits"`
}

// StorageRetentionLimits mirrors the plan's min/max bounds for each retention
// type as returned by the server.
type StorageRetentionLimits struct {
	Cache     StorageRetentionBound `json:"retention_days_cache"`
	Workspace StorageRetentionBound `json:"retention_days_workspace"`
	Artifact  StorageRetentionBound `json:"retention_days_artifact"`
}

// StorageRetentionBound is one min/max pair within StorageRetentionLimits.
type StorageRetentionBound struct {
	Min int `json:"min"`
	Max int `json:"max"`
}

// storageRetentionResponse mirrors GET /private/orgs/{orgUUID}/storage-retention-controls.
type storageRetentionResponse struct {
	Controls StorageRetentionControls `json:"storage_retention_controls"`
	Limits   StorageRetentionLimits   `json:"storage_retention_limits"`
}

// storageRetentionPath returns the relative URL path for the storage-retention
// endpoint for the given org UUID.
func storageRetentionPath(orgUUID string) (*url.URL, error) {
	return url.Parse("private/orgs/" + url.PathEscape(orgUUID) + "/storage-retention-controls")
}

// GetStorageRetention returns the current storage-retention controls and
// plan limits for the given org.
//
// Endpoint: GET https://app.circleci.com/private/orgs/{orgUUID}/storage-retention-controls
//
// Like the other private BFF endpoints (groups, SSO) this is served by
// app.circleci.com, not circleci.com; the org client's app base URL handles
// the host rewrite and the token travels in the Circle-Token header.
func (c *Client) GetStorageRetention(ctx context.Context, orgUUID string) (*StorageRetention, error) {
	u, err := storageRetentionPath(orgUUID)
	if err != nil {
		return nil, fmt.Errorf("GetStorageRetention: build URL: %w", err)
	}

	req, err := c.app.NewRequest(ctx, "GET", u, nil)
	if err != nil {
		return nil, fmt.Errorf("GetStorageRetention: build request: %w", err)
	}

	var raw storageRetentionResponse
	if _, err := c.app.DoRequest(req, &raw); err != nil {
		return nil, fmt.Errorf("GetStorageRetention %s: %w", orgUUID, err)
	}
	return &StorageRetention{
		Controls: raw.Controls,
		Limits:   raw.Limits,
	}, nil
}

// SetStorageRetention writes storage-retention controls for the given org via
// PUT (the server clamps values to the plan's limits). PUT returns 204 No
// Content; POST/PATCH are not routed (they 404 "no such page").
//
// Endpoint: PUT https://app.circleci.com/private/orgs/{orgUUID}/storage-retention-controls
func (c *Client) SetStorageRetention(ctx context.Context, orgUUID string, controls StorageRetentionControls) error {
	u, err := storageRetentionPath(orgUUID)
	if err != nil {
		return fmt.Errorf("SetStorageRetention: build URL: %w", err)
	}

	req, err := c.app.NewRequest(ctx, "PUT", u, controls)
	if err != nil {
		return fmt.Errorf("SetStorageRetention: build request: %w", err)
	}

	// 204 No Content on success — no response body to decode.
	if _, err := c.app.DoRequest(req, nil); err != nil {
		return fmt.Errorf("SetStorageRetention %s: %w", orgUUID, err)
	}
	return nil
}
