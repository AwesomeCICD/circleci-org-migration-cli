package org

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
)

// OrgSettings holds organization-level settings retrieved from the v1.1 API.
type OrgSettings struct {
	// RequireContextGroupRestriction mirrors the
	// feature_flags.require_context_group_restriction field in the v1.1
	// organization settings response.  It is nil when the field is absent from
	// the response (e.g. the flag is not supported for this org type).
	RequireContextGroupRestriction *bool
}

// orgSettingsResponse mirrors the JSON shape of
// GET /api/v1.1/organization/{vcsType}/{orgName}/settings.
//
// The outer structure is:
//
//	{
//	  "feature_flags": {
//	    "require_context_group_restriction": true
//	  }
//	}
//
// We use json.RawMessage for the nested object so we can detect whether the
// individual flag key is present or merely absent.
type orgSettingsResponse struct {
	FeatureFlags json.RawMessage `json:"feature_flags"`
}

// GetOrgSettings retrieves organization-level feature flags via the v1.1 API.
//
// Endpoint: GET /api/v1.1/organization/{vcsType}/{orgName}/settings
//
// If the require_context_group_restriction flag is absent from the response,
// OrgSettings.RequireContextGroupRestriction will be nil.
func (c *Client) GetOrgSettings(ctx context.Context, vcsType, orgName string) (*OrgSettings, error) {
	// Both path segments are user-supplied; encode them individually so that
	// internal slashes (unlikely but defensive) don't corrupt the path.
	path := "organization/" + url.PathEscape(vcsType) + "/" + url.PathEscape(orgName) + "/settings"
	u := &url.URL{Path: path}

	req, err := c.v11.NewRequest(ctx, "GET", u, nil)
	if err != nil {
		return nil, fmt.Errorf("GetOrgSettings: build request: %w", err)
	}

	var raw orgSettingsResponse
	if _, err := c.v11.DoRequest(req, &raw); err != nil {
		return nil, fmt.Errorf("GetOrgSettings %s/%s: %w", vcsType, orgName, err)
	}

	result := &OrgSettings{}

	if len(raw.FeatureFlags) > 0 {
		// Decode only the fields we care about; unknown flags are silently ignored.
		var flags struct {
			RequireContextGroupRestriction *bool `json:"require_context_group_restriction"`
		}
		if err := json.Unmarshal(raw.FeatureFlags, &flags); err != nil {
			return nil, fmt.Errorf("GetOrgSettings: parse feature_flags: %w", err)
		}
		result.RequireContextGroupRestriction = flags.RequireContextGroupRestriction
	}

	return result, nil
}
