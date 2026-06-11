package project

import (
	"context"
	"fmt"
)

// ProjectAPIToken is the metadata for one project API token.
// The token value is NEVER returned by the list endpoint and is
// intentionally excluded from this struct.  The plaintext value
// is available ONCE from CreateProjectToken immediately after creation.
//
// Scopes (UI label → API value):
//
//	Status    → "status"
//	Read Only → "view-builds"
//	Admin     → "all"
type ProjectAPIToken struct {
	// ID is the server-assigned UUID for this token.
	ID string `json:"id"`
	// Label is the human-readable name given to the token.
	Label string `json:"label"`
	// Scope is one of "status", "view-builds", or "all".
	Scope string `json:"scope"`
}

// v11TokenListResponse mirrors the v1.1 token list array element.
// The token value field is always null on list — only returned at create time.
type v11TokenListResponse []struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Scope string `json:"scope"`
}

// createTokenRequest is the POST body for the create endpoint.
type createTokenRequest struct {
	Scope string `json:"scope"`
	Label string `json:"label"`
}

// v11CreateTokenResponse mirrors the v1.1 POST response (token present once).
type v11CreateTokenResponse struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Scope string `json:"scope"`
	Token string `json:"token"`
}

// ListProjectTokens returns the metadata for every API token configured on a
// project. Token values are NEVER returned by the CircleCI list API; only
// ID, Label, and Scope are available.
//
// Endpoint: GET /api/v1.1/project/{vcs}/{org}/{repo}/token
//
// The slug format follows the same conventions as other v1.1 calls:
//   - GitHub OAuth: "gh/<org>/<repo>"
//   - Standalone/App: "circleci/<org-uuid>/<proj-uuid>"
//
// On a non-2xx response an error is returned; callers should treat it as
// non-fatal and record a manifest warning rather than aborting the export.
func (c *Client) ListProjectTokens(ctx context.Context, slug string) ([]ProjectAPIToken, error) {
	u, err := slugSubresource(slug, "token")
	if err != nil {
		return nil, fmt.Errorf("ListProjectTokens: %w", err)
	}

	req, err := c.v11.NewRequest(ctx, "GET", u, nil)
	if err != nil {
		return nil, fmt.Errorf("ListProjectTokens: build request: %w", err)
	}

	var raw v11TokenListResponse
	if _, err := c.v11.DoRequest(req, &raw); err != nil {
		return nil, fmt.Errorf("ListProjectTokens %q: %w", slug, err)
	}

	if len(raw) == 0 {
		return nil, nil
	}
	out := make([]ProjectAPIToken, len(raw))
	for i, t := range raw {
		out[i] = ProjectAPIToken{ID: t.ID, Label: t.Label, Scope: t.Scope}
	}
	return out, nil
}

// CreateProjectToken creates a new API token on a project with the given scope
// and label, and returns the plaintext token value. The plaintext is returned
// ONCE by the API and is NEVER available again after this call — callers must
// surface it immediately to the operator.
//
// Endpoint: POST /api/v1.1/project/{vcs}/{org}/{repo}/token
// Request body: {"scope": "<scope>", "label": "<label>"}
//
// Scopes: "status" | "view-builds" | "all"
func (c *Client) CreateProjectToken(ctx context.Context, slug, scope, label string) (string, error) {
	if slug == "" {
		return "", fmt.Errorf("CreateProjectToken: slug is required")
	}
	if scope == "" {
		return "", fmt.Errorf("CreateProjectToken: scope is required")
	}
	if label == "" {
		return "", fmt.Errorf("CreateProjectToken: label is required")
	}

	u, err := slugSubresource(slug, "token")
	if err != nil {
		return "", fmt.Errorf("CreateProjectToken: %w", err)
	}

	body := createTokenRequest{Scope: scope, Label: label}
	req, err := c.v11.NewRequest(ctx, "POST", u, &body)
	if err != nil {
		return "", fmt.Errorf("CreateProjectToken: build request: %w", err)
	}

	var resp v11CreateTokenResponse
	if _, err := c.v11.DoRequest(req, &resp); err != nil {
		return "", fmt.Errorf("CreateProjectToken %q: %w", slug, err)
	}
	return resp.Token, nil
}
