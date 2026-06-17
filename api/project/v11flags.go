package project

import (
	"context"
	"fmt"
	"strings"
)

// v11ProjectSettingsResponse mirrors the relevant parts of the
// GET /api/v1.1/project/{slug}/settings response.
//
// JSON shape confirmed from the CircleCI v1.1 API:
//
//	{"following": bool, "has_usable_key": bool, "feature_flags": {...}}
//
// The feature_flags map may also contain non-boolean values (e.g. arrays for
// some orb-related flags); we decode as map[string]any and extract only the
// bool keys we care about, ignoring the rest.
type v11ProjectSettingsResponse struct {
	Following    bool           `json:"following"`
	HasUsableKey bool           `json:"has_usable_key"`
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
func (c *Client) GetV11ProjectFeatureFlags(ctx context.Context, slug string) (map[string]bool, error) {
	u, err := slugSubresource(slug, "settings")
	if err != nil {
		return nil, fmt.Errorf("GetV11ProjectFeatureFlags: %w", err)
	}

	req, err := c.v11.NewRequest(ctx, "GET", u, nil)
	if err != nil {
		return nil, fmt.Errorf("GetV11ProjectFeatureFlags: build request: %w", err)
	}

	var raw v11ProjectSettingsResponse
	if _, err := c.v11.DoRequest(req, &raw); err != nil {
		return nil, fmt.Errorf("GetV11ProjectFeatureFlags %q: %w", slug, err)
	}

	return boolFlags(raw.FeatureFlags), nil
}

// GetV11ProjectSettings returns BOTH the authoritative "following" state and the
// bool-valued feature flags from a single GET of the v1.1 project settings
// endpoint. The "following" field is the authoritative signal for whether a
// project is followed/enabled (a webhook / deploy key is installed) — it is
// fresher than the followed-projects discovery list, which can lag immediately
// after a follow. Callers that need an accurate Followed flag should prefer this
// over the discovery cross-reference.
//
// Endpoint: GET /api/v1.1/project/{slug}/settings
func (c *Client) GetV11ProjectSettings(ctx context.Context, slug string) (following bool, flags map[string]bool, err error) {
	u, err := slugSubresource(slug, "settings")
	if err != nil {
		return false, nil, fmt.Errorf("GetV11ProjectSettings: %w", err)
	}

	req, err := c.v11.NewRequest(ctx, "GET", u, nil)
	if err != nil {
		return false, nil, fmt.Errorf("GetV11ProjectSettings: build request: %w", err)
	}

	var raw v11ProjectSettingsResponse
	if _, err := c.v11.DoRequest(req, &raw); err != nil {
		return false, nil, fmt.Errorf("GetV11ProjectSettings %q: %w", slug, err)
	}
	return raw.Following, boolFlags(raw.FeatureFlags), nil
}

// boolFlags extracts the bool-valued entries from a feature_flags blob, ignoring
// any non-bool values (some orb-related flags are arrays).
func boolFlags(ff map[string]any) map[string]bool {
	result := make(map[string]bool, len(ff))
	for k, v := range ff {
		if b, ok := v.(bool); ok {
			result[k] = b
		}
	}
	return result
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
func (c *Client) SetV11ProjectFeatureFlags(ctx context.Context, slug string, flags map[string]bool) error {
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
	req, err := c.v11.NewRequest(ctx, "PUT", u, body)
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

// IsProjectFollowed returns true when the authenticated user's token is already
// following the project (i.e., a webhook / deploy key is installed).  It reads
// the "following" field from GET /api/v1.1/project/{slug}/settings.
//
// If the settings call fails, the function returns false and a non-nil error.
// Callers in the syncer treat this as a non-fatal warning and queue the project
// for the enable-builds step rather than skipping it.
func (c *Client) IsProjectFollowed(ctx context.Context, vcsType, org, repo string) (bool, error) {
	slug := vcsType + "/" + org + "/" + repo
	u, err := slugSubresource(slug, "settings")
	if err != nil {
		return false, fmt.Errorf("IsProjectFollowed: %w", err)
	}

	req, err := c.v11.NewRequest(ctx, "GET", u, nil)
	if err != nil {
		return false, fmt.Errorf("IsProjectFollowed: build request: %w", err)
	}

	var raw v11ProjectSettingsResponse
	if _, err := c.v11.DoRequest(req, &raw); err != nil {
		return false, fmt.Errorf("IsProjectFollowed %q: %w", slug, err)
	}
	return raw.Following, nil
}

// projectSnakeToKebab converts a snake_case string to kebab-case.
// E.g. "api_trigger_with_config" → "api-trigger-with-config".
// Mirrors the snakeToKebab helper in api/org/orgsettings.go.
func projectSnakeToKebab(s string) string {
	return strings.ReplaceAll(s, "_", "-")
}
