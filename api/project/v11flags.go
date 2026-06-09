package project

import (
	"fmt"
	"strings"
)

// v11ProjectSettingsResponse mirrors the relevant parts of the
// GET /api/v1.1/project/{slug}/settings response.
//
// JSON shape confirmed from the CircleCI v1.1 API:
//
//	{"feature_flags": {"api-trigger-with-config": bool, "drop-all-build-requests": bool, ...}}
//
// The feature_flags map may also contain non-boolean values (e.g. arrays for
// some orb-related flags); we decode as map[string]any and extract only the
// two bool keys we care about, ignoring the rest.
type v11ProjectSettingsResponse struct {
	FeatureFlags map[string]any `json:"feature_flags"`
}

// GetV11ProjectFeatureFlags returns the two project-level feature flags that
// live in the v1.1 project settings endpoint.  It returns a map with at most
// two keys: "api-trigger-with-config" and "drop-all-build-requests" (kebab-case,
// as returned by the API).  Non-bool values in the feature_flags blob are
// silently ignored.
//
// Endpoint: GET /api/v1.1/project/{slug}/settings
//
// The slug is encoded using the same slugSubresource convention as other v1.1
// calls (each component percent-encoded, literal '/' separators kept).
func (c *Client) GetV11ProjectFeatureFlags(slug string) (map[string]bool, error) {
	u, err := slugSubresource(slug, "settings")
	if err != nil {
		return nil, fmt.Errorf("GetV11ProjectFeatureFlags: %w", err)
	}

	req, err := c.v11.NewRequest("GET", u, nil)
	if err != nil {
		return nil, fmt.Errorf("GetV11ProjectFeatureFlags: build request: %w", err)
	}

	var raw v11ProjectSettingsResponse
	if _, err := c.v11.DoRequest(req, &raw); err != nil {
		return nil, fmt.Errorf("GetV11ProjectFeatureFlags %q: %w", slug, err)
	}

	const keyAPI = "api-trigger-with-config"
	const keyDrop = "drop-all-build-requests"

	result := make(map[string]bool, 2)
	for _, k := range []string{keyAPI, keyDrop} {
		if v, ok := raw.FeatureFlags[k]; ok {
			if b, ok := v.(bool); ok {
				result[k] = b
			}
		}
	}
	return result, nil
}

// SetV11ProjectFeatureFlags writes the provided feature flags to the project
// via a PUT to the v1.1 settings endpoint.  Keys should be snake_case
// (e.g. "api_trigger_with_config"); they are converted to kebab-case on the
// wire (e.g. "api-trigger-with-config") to match the CircleCI v1.1 write API.
//
// Endpoint: PUT /api/v1.1/project/{slug}/settings
// Request body: {"feature_flags": {"<kebab-key>": <bool>, ...}}
//
// Mirrors api/org UpdateFeatureFlags + snakeToKebab exactly, scoped to a
// project.
func (c *Client) SetV11ProjectFeatureFlags(slug string, flags map[string]bool) error {
	u, err := slugSubresource(slug, "settings")
	if err != nil {
		return fmt.Errorf("SetV11ProjectFeatureFlags: %w", err)
	}

	// Convert snake_case keys to kebab-case for the write path.
	kebab := make(map[string]bool, len(flags))
	for k, v := range flags {
		kebab[projectSnakeToKebab(k)] = v
	}

	body := map[string]any{"feature_flags": kebab}
	req, err := c.v11.NewRequest("PUT", u, body)
	if err != nil {
		return fmt.Errorf("SetV11ProjectFeatureFlags: build request: %w", err)
	}

	var ignored map[string]any
	if _, err := c.v11.DoRequest(req, &ignored); err != nil {
		return fmt.Errorf("SetV11ProjectFeatureFlags %q: %w", slug, err)
	}
	return nil
}

// projectSnakeToKebab converts a snake_case string to kebab-case.
// E.g. "api_trigger_with_config" → "api-trigger-with-config".
// Mirrors the snakeToKebab helper in api/org/orgsettings.go.
func projectSnakeToKebab(s string) string {
	return strings.ReplaceAll(s, "_", "-")
}
