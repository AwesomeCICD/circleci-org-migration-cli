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

// GetV11ProjectFeatureFlags returns the project-level feature flags from the
// v1.1 project settings endpoint.  It returns the full map of bool-valued flags
// (kebab-case keys, as returned by the API).  Non-bool values in the
// feature_flags blob are silently ignored.
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

	// Capture the entire bool-valued feature-flags map so nothing is lost.
	// The caller (exporter) also extracts the two well-known keys separately
	// for backward-compat fields; the full map is stored alongside them.
	result := make(map[string]bool, len(raw.FeatureFlags))
	for k, v := range raw.FeatureFlags {
		if b, ok := v.(bool); ok {
			result[k] = b
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

	// Pass nil so the response body is discarded without decoding.
	// The live v1.1 PUT /settings endpoint may return a plain string or a
	// non-map JSON value — any attempt to unmarshal it into map[string]any
	// would fail with "cannot unmarshal string into Go value of type
	// map[string]interface {}".  A 2xx status is sufficient for success;
	// the caller never needs the response body.
	if _, err := c.v11.DoRequest(req, nil); err != nil {
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
