package project

import (
	"fmt"
	"net/url"
)

// projectOIDCClaimsResponse mirrors GET /api/v2/org/{orgID}/project/{projID}/oidc-custom-claims.
//
// JSON shape confirmed from oidc-tasks-service openapi.yaml:
//
//	{"org_id":"...","project_id":"...","audience":[...],"ttl":"..."}
type projectOIDCClaimsResponse struct {
	OrgID     string   `json:"org_id"`
	ProjectID string   `json:"project_id"`
	Audience  []string `json:"audience"`
	TTL       string   `json:"ttl"`
}

// GetProjectOIDCClaims retrieves the OIDC custom claims for a project.
//
// Endpoint: GET /api/v2/org/{orgID}/project/{projID}/oidc-custom-claims
//
// orgID and projID are bare UUIDs; they are percent-escaped individually.
// Mirrors api/org GetOIDCClaims exactly, scoped to a project.
func (c *Client) GetProjectOIDCClaims(orgID, projID string) (audience []string, ttl string, err error) {
	path := "org/" + url.PathEscape(orgID) + "/project/" + url.PathEscape(projID) + "/oidc-custom-claims"
	u := &url.URL{Path: path}

	req, err := c.v2.NewRequest("GET", u, nil)
	if err != nil {
		return nil, "", fmt.Errorf("GetProjectOIDCClaims: build request: %w", err)
	}

	var raw projectOIDCClaimsResponse
	if _, err := c.v2.DoRequest(req, &raw); err != nil {
		return nil, "", fmt.Errorf("GetProjectOIDCClaims %s/%s: %w", orgID, projID, err)
	}
	return raw.Audience, raw.TTL, nil
}

// SetProjectOIDCClaims writes OIDC audience and TTL for a project.
//
// Endpoint: PATCH /api/v2/org/{orgID}/project/{projID}/oidc-custom-claims
//
// Confirmed by oidc-tasks-service openapi.yaml: PATCH with {"audience":[...],"ttl":"..."}.
// Mirrors api/org SetOIDCClaims exactly, scoped to a project.
func (c *Client) SetProjectOIDCClaims(orgID, projID string, audience []string, ttl string) error {
	path := "org/" + url.PathEscape(orgID) + "/project/" + url.PathEscape(projID) + "/oidc-custom-claims"
	u := &url.URL{Path: path}

	body := map[string]any{}
	if len(audience) > 0 {
		body["audience"] = audience
	}
	if ttl != "" {
		body["ttl"] = ttl
	}
	if len(body) == 0 {
		return nil // nothing to set
	}

	req, err := c.v2.NewRequest("PATCH", u, body)
	if err != nil {
		return fmt.Errorf("SetProjectOIDCClaims: build request: %w", err)
	}

	var ignored map[string]any
	if _, err := c.v2.DoRequest(req, &ignored); err != nil {
		return fmt.Errorf("SetProjectOIDCClaims %s/%s: %w", orgID, projID, err)
	}
	return nil
}
