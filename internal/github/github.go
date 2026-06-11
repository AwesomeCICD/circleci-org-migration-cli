// Package github provides a minimal GitHub REST API client for resolving
// repository IDs needed when creating pipeline definitions on GitHub App
// CircleCI organizations.  It uses only stdlib net/http — no third-party SDK.
package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// ErrRepoNotFound is returned by ResolveRepoID when the repository does not
// exist or is not accessible (HTTP 404).  Callers that need to distinguish a
// missing repo from other errors should use errors.Is(err, ErrRepoNotFound).
var ErrRepoNotFound = errors.New("repository not found")

// DefaultBaseURL is the public GitHub API base URL.
const DefaultBaseURL = "https://api.github.com"

// ResolveRepoID resolves a GitHub repository's numeric ID from its full name
// (e.g. "acme/web") and returns it as a string for use in pipeline-definition
// and trigger API calls.
//
// Endpoint: GET {baseURL}/repos/{owner}/{repo}
// Header:   Authorization: Bearer {token}  (only when token != "")
//
// The baseURL defaults to DefaultBaseURL when the empty string is passed.
// A 404 response is mapped to a descriptive error; other non-2xx responses
// are also returned as errors.
//
// JSON field used: id (a JSON number, returned as a string).
func ResolveRepoID(ctx context.Context, fullName, token, baseURL string) (string, error) {
	if fullName == "" {
		return "", fmt.Errorf("github: ResolveRepoID: fullName must not be empty")
	}
	parts := strings.SplitN(fullName, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", fmt.Errorf("github: ResolveRepoID: fullName %q must be owner/repo", fullName)
	}

	base := baseURL
	if base == "" {
		base = DefaultBaseURL
	}
	// Remove any trailing slash so we don't end up with //.
	base = strings.TrimRight(base, "/")

	apiURL := base + "/repos/" + parts[0] + "/" + parts[1]

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return "", fmt.Errorf("github: ResolveRepoID: build request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("github: ResolveRepoID %q: %w", fullName, err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode == http.StatusNotFound {
		return "", fmt.Errorf("github: ResolveRepoID %q: %w (HTTP 404)", fullName, ErrRepoNotFound)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("github: ResolveRepoID %q: unexpected status %d", fullName, resp.StatusCode)
	}

	var payload struct {
		ID json.Number `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", fmt.Errorf("github: ResolveRepoID %q: decode response: %w", fullName, err)
	}
	if payload.ID.String() == "" || payload.ID.String() == "0" {
		return "", fmt.Errorf("github: ResolveRepoID %q: id missing or zero in response", fullName)
	}
	return payload.ID.String(), nil
}
