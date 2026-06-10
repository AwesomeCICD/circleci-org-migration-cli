package org

import (
	"fmt"
	"net/url"
)

// EnvHierarchyLevel is one level in an environment hierarchy.
// IntegrationName is the human-readable name of the deploy integration
// (e.g. "orbs-dev"). IntegrationID is intentionally NOT captured here —
// it is a source-org-specific deploy-integration UUID that will not exist
// in the destination org.
type EnvHierarchyLevel struct {
	Position        int    `json:"position"`
	IntegrationName string `json:"integration_name"`
}

// EnvHierarchyConfig holds the hierarchy definition (name, description, levels).
type EnvHierarchyConfig struct {
	Name        string              `json:"name"`
	Description string              `json:"description,omitempty"`
	Levels      []EnvHierarchyLevel `json:"levels,omitempty"`
}

// envHierarchyRawLevel is the raw API level shape, including integration_id
// which we parse but do not export to the manifest.
type envHierarchyRawLevel struct {
	Position        int    `json:"position"`
	IntegrationID   string `json:"integration_id"`
	IntegrationName string `json:"integration_name"`
}

// envHierarchyRawConfig mirrors the "hierarchy" object in the API response.
type envHierarchyRawConfig struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	Levels      []envHierarchyRawLevel `json:"levels,omitempty"`
}

// envHierarchyResponse mirrors GET
// /private/release-tracker/v1/environment-hierarchy/resolve?org-id={orgUUID}.
type envHierarchyResponse struct {
	ResolvedScope string                 `json:"resolved_scope"`
	Hierarchy     *envHierarchyRawConfig `json:"hierarchy"`
}

// GetEnvironmentHierarchy returns the org's environment hierarchy configuration.
// When no hierarchy is configured (scope "NONE" or null hierarchy) the method
// returns nil without error.
//
// Endpoint: GET https://app.circleci.com/private/release-tracker/v1/environment-hierarchy/resolve?org-id={orgUUID}
func (c *Client) GetEnvironmentHierarchy(orgUUID string) (*EnvHierarchyConfig, error) {
	rawPath := "private/release-tracker/v1/environment-hierarchy/resolve?org-id=" + url.QueryEscape(orgUUID)
	u, err := url.Parse(rawPath)
	if err != nil {
		return nil, fmt.Errorf("GetEnvironmentHierarchy: build URL: %w", err)
	}

	req, err := c.app.NewRequest("GET", u, nil)
	if err != nil {
		return nil, fmt.Errorf("GetEnvironmentHierarchy: build request: %w", err)
	}

	var raw envHierarchyResponse
	if _, err := c.app.DoRequest(req, &raw); err != nil {
		return nil, fmt.Errorf("GetEnvironmentHierarchy %s: %w", orgUUID, err)
	}

	// No hierarchy configured: scope is NONE or the hierarchy object is null.
	if raw.Hierarchy == nil || raw.ResolvedScope == "NONE" {
		return nil, nil
	}

	cfg := &EnvHierarchyConfig{
		Name:        raw.Hierarchy.Name,
		Description: raw.Hierarchy.Description,
	}
	for _, l := range raw.Hierarchy.Levels {
		cfg.Levels = append(cfg.Levels, EnvHierarchyLevel{
			Position:        l.Position,
			IntegrationName: l.IntegrationName,
			// IntegrationID intentionally omitted: it is source-org-specific.
		})
	}
	return cfg, nil
}
