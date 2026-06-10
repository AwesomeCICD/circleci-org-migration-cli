package org

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// ─────────────────────────────────────────────────────────────────────────────
// Feature flags
// ─────────────────────────────────────────────────────────────────────────────

// GetFeatureFlags returns the org's feature flags as a bool map, handling BOTH
// v1.1 settings response shapes:
//   - GitHub OAuth orgs: {"feature_flags": {"key": bool, ...}}
//   - GitHub-App / standalone (circleci-type) orgs: a FLAT object where flags are
//     top-level bool fields (some "?"-suffixed, e.g. "allow_api_trigger_with_config_enabled?"),
//     interleaved with non-bool fields ("name", "users", "analytics_id", ...).
//
// A trailing "?" is stripped from keys; non-bool fields are ignored. (Previously
// only the nested shape was parsed, so standalone orgs captured ZERO flags.)
//
// Endpoint: GET /api/v1.1/organization/{vcsType}/{orgName}/settings
func (c *Client) GetFeatureFlags(vcsType, orgName string) (map[string]bool, error) {
	path := "organization/" + url.PathEscape(vcsType) + "/" + url.PathEscape(orgName) + "/settings"
	u := &url.URL{Path: path}

	req, err := c.v11.NewRequest("GET", u, nil)
	if err != nil {
		return nil, fmt.Errorf("GetFeatureFlags: build request: %w", err)
	}

	var raw map[string]json.RawMessage
	if _, err := c.v11.DoRequest(req, &raw); err != nil {
		return nil, fmt.Errorf("GetFeatureFlags %s/%s: %w", vcsType, orgName, err)
	}

	flags := make(map[string]bool)
	if ff, ok := raw["feature_flags"]; ok {
		// Nested shape (OAuth orgs).
		nested := map[string]bool{}
		if jsonErr := json.Unmarshal(ff, &nested); jsonErr == nil {
			for k, v := range nested {
				flags[strings.TrimSuffix(k, "?")] = v
			}
		}
		return flags, nil
	}
	// Flat shape (GH-App / standalone orgs): keep only bool-valued keys.
	for k, v := range raw {
		var b bool
		if json.Unmarshal(v, &b) == nil {
			flags[strings.TrimSuffix(k, "?")] = b
		}
	}
	return flags, nil
}

// UpdateFeatureFlags writes the provided flag map to the org via PUT.
//
// Endpoint: PUT /api/v1.1/organization/{vcsType}/{orgName}/settings
//
// The v1.1 API reads keys in snake_case but expects writes in kebab-case
// (e.g. "allow_certified_public_orbs" → "allow-certified-public-orbs").
// This is confirmed by the web-ui useUpdateAllowCertifiedPublicOrbs hook.
func (c *Client) UpdateFeatureFlags(vcsType, orgName string, flags map[string]bool) error {
	path := "organization/" + url.PathEscape(vcsType) + "/" + url.PathEscape(orgName) + "/settings"
	u := &url.URL{Path: path}

	// Convert snake_case keys to kebab-case for the write path.
	kebab := make(map[string]bool, len(flags))
	for k, v := range flags {
		kebab[snakeToKebab(k)] = v
	}

	body := map[string]any{"feature_flags": kebab}
	req, err := c.v11.NewRequest("PUT", u, body)
	if err != nil {
		return fmt.Errorf("UpdateFeatureFlags: build request: %w", err)
	}

	// Ignore the response body: the v1.1 settings PUT returns different shapes
	// per org type (object, plain string, or empty), so decoding it into a map
	// would spuriously fail (e.g. "cannot unmarshal string into map").
	if _, err := c.v11.DoRequest(req, nil); err != nil {
		return fmt.Errorf("UpdateFeatureFlags %s/%s: %w", vcsType, orgName, err)
	}
	return nil
}

// snakeToKebab converts a snake_case string to kebab-case.
// E.g. "allow_certified_public_orbs" → "allow-certified-public-orbs".
func snakeToKebab(s string) string {
	return strings.ReplaceAll(s, "_", "-")
}

// ─────────────────────────────────────────────────────────────────────────────
// OIDC custom claims
// ─────────────────────────────────────────────────────────────────────────────

// oidcClaimsResponse mirrors GET /api/v2/org/{orgID}/oidc-custom-claims.
type oidcClaimsResponse struct {
	OrgID    string   `json:"org_id"`
	Audience []string `json:"audience"`
	TTL      string   `json:"ttl"`
}

// GetOIDCClaims retrieves the org's OIDC custom claims.
//
// Endpoint: GET /api/v2/org/{orgID}/oidc-custom-claims
func (c *Client) GetOIDCClaims(orgID string) (audience []string, ttl string, err error) {
	// orgID is always a bare UUID — no slash, no encoding needed; use url.URL{Path}
	// which is safe for plain UUIDs.
	u := &url.URL{Path: "org/" + url.PathEscape(orgID) + "/oidc-custom-claims"}

	req, err := c.v2.NewRequest("GET", u, nil)
	if err != nil {
		return nil, "", fmt.Errorf("GetOIDCClaims: build request: %w", err)
	}

	var raw oidcClaimsResponse
	if _, err := c.v2.DoRequest(req, &raw); err != nil {
		return nil, "", fmt.Errorf("GetOIDCClaims %s: %w", orgID, err)
	}
	return raw.Audience, raw.TTL, nil
}

// SetOIDCClaims writes OIDC audience and TTL for the org.
//
// Endpoint: PATCH /api/v2/org/{orgID}/oidc-custom-claims
//
// Confirmed by oidc-tasks-service openapi.yaml: PATCH with {"audience":[...],"ttl":"..."}.
func (c *Client) SetOIDCClaims(orgID string, audience []string, ttl string) error {
	u := &url.URL{Path: "org/" + url.PathEscape(orgID) + "/oidc-custom-claims"}

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
		return fmt.Errorf("SetOIDCClaims: build request: %w", err)
	}

	var ignored map[string]any
	if _, err := c.v2.DoRequest(req, &ignored); err != nil {
		return fmt.Errorf("SetOIDCClaims %s: %w", orgID, err)
	}
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// URL-orb allow list
// ─────────────────────────────────────────────────────────────────────────────

// URLOrbAllowEntry represents one entry on the org's URL-orb allow list.
type URLOrbAllowEntry struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Prefix string `json:"prefix"`
	Auth   string `json:"auth"`
}

// urlOrbAllowListResponse mirrors GET /api/v2/organization/{slug-or-id}/url-orb-allow-list.
type urlOrbAllowListResponse struct {
	Items []URLOrbAllowEntry `json:"items"`
}

// GetURLOrbAllowList retrieves the org's URL-orb allow list.
//
// Endpoint: GET /api/v2/organization/{slug-or-id}/url-orb-allow-list
func (c *Client) GetURLOrbAllowList(slugOrID string) ([]URLOrbAllowEntry, error) {
	// slugOrID may contain a slash (e.g. "gh/acme") — use url.Parse on the
	// pre-escaped string to avoid double-encoding, matching the pattern in org.go.
	u, err := url.Parse("organization/" + url.PathEscape(slugOrID) + "/url-orb-allow-list")
	if err != nil {
		return nil, fmt.Errorf("GetURLOrbAllowList: build URL: %w", err)
	}

	req, err := c.v2.NewRequest("GET", u, nil)
	if err != nil {
		return nil, fmt.Errorf("GetURLOrbAllowList: build request: %w", err)
	}

	var raw urlOrbAllowListResponse
	if _, err := c.v2.DoRequest(req, &raw); err != nil {
		return nil, fmt.Errorf("GetURLOrbAllowList %s: %w", slugOrID, err)
	}
	return raw.Items, nil
}

// CreateURLOrbAllowEntry adds a new entry to the org's URL-orb allow list.
//
// Endpoint: POST /api/v2/organization/{slug-or-id}/url-orb-allow-list
//
// Confirmed by circle.clj and useCreateURLOrbAllowListEntry.ts:
// body {"name":"...","prefix":"...","auth":"..."}.
func (c *Client) CreateURLOrbAllowEntry(slugOrID, name, prefix, auth string) error {
	u, err := url.Parse("organization/" + url.PathEscape(slugOrID) + "/url-orb-allow-list")
	if err != nil {
		return fmt.Errorf("CreateURLOrbAllowEntry: build URL: %w", err)
	}

	body := map[string]string{"name": name, "prefix": prefix, "auth": auth}
	req, err := c.v2.NewRequest("POST", u, body)
	if err != nil {
		return fmt.Errorf("CreateURLOrbAllowEntry: build request: %w", err)
	}

	var ignored map[string]any
	if _, err := c.v2.DoRequest(req, &ignored); err != nil {
		return fmt.Errorf("CreateURLOrbAllowEntry %s: %w", slugOrID, err)
	}
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Audit-log configs
// ─────────────────────────────────────────────────────────────────────────────

// AuditLogConfig is one audit-log streaming configuration on an org, as returned
// by GET /api/v2/organizations/{orgID}/audit-log/configs.
type AuditLogConfig struct {
	ID         string         `json:"id"`
	OrgID      string         `json:"org_id"`
	Purpose    string         `json:"purpose"`
	TargetType string         `json:"target_type"`
	IsDisabled bool           `json:"is_disabled"`
	Config     AuditLogTarget `json:"config"`
}

// AuditLogTarget is the (typically S3) destination of an audit-log config.
type AuditLogTarget struct {
	ARN          string `json:"arn"`
	Region       string `json:"region"`
	BucketName   string `json:"bucket_name"`
	BucketPrefix string `json:"bucket_prefix"`
	Endpoint     string `json:"endpoint"`
}

// auditLogConfigsResponse mirrors GET /api/v2/organizations/{orgID}/audit-log/configs.
type auditLogConfigsResponse struct {
	Items []AuditLogConfig `json:"items"`
}

// GetAuditLogConfigs retrieves the org's audit-log streaming configurations.
//
// Endpoint: GET /api/v2/organizations/{orgID}/audit-log/configs
func (c *Client) GetAuditLogConfigs(orgID string) ([]AuditLogConfig, error) {
	u, err := url.Parse("organizations/" + url.PathEscape(orgID) + "/audit-log/configs")
	if err != nil {
		return nil, fmt.Errorf("GetAuditLogConfigs: build URL: %w", err)
	}

	req, err := c.v2.NewRequest("GET", u, nil)
	if err != nil {
		return nil, fmt.Errorf("GetAuditLogConfigs: build request: %w", err)
	}

	var raw auditLogConfigsResponse
	if _, err := c.v2.DoRequest(req, &raw); err != nil {
		return nil, fmt.Errorf("GetAuditLogConfigs %s: %w", orgID, err)
	}
	return raw.Items, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Config policies (policy-bundle + enforcement)
// ─────────────────────────────────────────────────────────────────────────────

// policyEnforcementResponse mirrors GET/PATCH /api/v2/owner/{id}/context/config/decision/settings.
type policyEnforcementResponse struct {
	Enabled bool `json:"enabled"`
}

// GetPolicyBundle retrieves all policies in the org's config policy bundle.
//
// Endpoint: GET /api/v2/owner/{ownerID}/context/config/policy-bundle
//
// Returns a map of policyName → Rego content.  Returns an empty map if the
// org has no bundle or is not on Scale.
func (c *Client) GetPolicyBundle(ownerID string) (map[string]string, error) {
	u, err := url.Parse("owner/" + url.PathEscape(ownerID) + "/context/config/policy-bundle")
	if err != nil {
		return nil, fmt.Errorf("GetPolicyBundle: build URL: %w", err)
	}

	req, err := c.v2.NewRequest("GET", u, nil)
	if err != nil {
		return nil, fmt.Errorf("GetPolicyBundle: build request: %w", err)
	}

	var raw map[string]string
	if _, err := c.v2.DoRequest(req, &raw); err != nil {
		return nil, fmt.Errorf("GetPolicyBundle %s: %w", ownerID, err)
	}
	if raw == nil {
		raw = map[string]string{}
	}
	return raw, nil
}

// PutPolicyBundle replaces the org's config policy bundle.
//
// Endpoint: POST /api/v2/owner/{ownerID}/context/config/policy-bundle
//
// Confirmed by circleci-cli api/policy/policy.go:
// POST with {"policies": {name: rego}}.
func (c *Client) PutPolicyBundle(ownerID string, policies map[string]string) error {
	u, err := url.Parse("owner/" + url.PathEscape(ownerID) + "/context/config/policy-bundle")
	if err != nil {
		return fmt.Errorf("PutPolicyBundle: build URL: %w", err)
	}

	body := map[string]any{"policies": policies}
	req, err := c.v2.NewRequest("POST", u, body)
	if err != nil {
		return fmt.Errorf("PutPolicyBundle: build request: %w", err)
	}

	var ignored map[string]any
	if _, err := c.v2.DoRequest(req, &ignored); err != nil {
		return fmt.Errorf("PutPolicyBundle %s: %w", ownerID, err)
	}
	return nil
}

// GetPolicyEnforcement retrieves whether config-policy enforcement is enabled.
//
// Endpoint: GET /api/v2/owner/{ownerID}/context/config/decision/settings
func (c *Client) GetPolicyEnforcement(ownerID string) (bool, error) {
	u, err := url.Parse("owner/" + url.PathEscape(ownerID) + "/context/config/decision/settings")
	if err != nil {
		return false, fmt.Errorf("GetPolicyEnforcement: build URL: %w", err)
	}

	req, err := c.v2.NewRequest("GET", u, nil)
	if err != nil {
		return false, fmt.Errorf("GetPolicyEnforcement: build request: %w", err)
	}

	var raw policyEnforcementResponse
	if _, err := c.v2.DoRequest(req, &raw); err != nil {
		return false, fmt.Errorf("GetPolicyEnforcement %s: %w", ownerID, err)
	}
	return raw.Enabled, nil
}

// SetPolicyEnforcement enables or disables config-policy enforcement.
//
// Endpoint: PATCH /api/v2/owner/{ownerID}/context/config/decision/settings
//
// Confirmed by circleci-cli cmd/policy/policy_test.go:
// PATCH with {"enabled": bool}.
func (c *Client) SetPolicyEnforcement(ownerID string, enabled bool) error {
	u, err := url.Parse("owner/" + url.PathEscape(ownerID) + "/context/config/decision/settings")
	if err != nil {
		return fmt.Errorf("SetPolicyEnforcement: build URL: %w", err)
	}

	body := map[string]any{"enabled": enabled}
	req, err := c.v2.NewRequest("PATCH", u, body)
	if err != nil {
		return fmt.Errorf("SetPolicyEnforcement: build request: %w", err)
	}

	var ignored map[string]any
	if _, err := c.v2.DoRequest(req, &ignored); err != nil {
		return fmt.Errorf("SetPolicyEnforcement %s: %w", ownerID, err)
	}
	return nil
}
