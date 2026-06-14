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
	"net/url"
	"strings"
)

// ErrRepoNotFound is returned by ResolveRepoID when the repository does not
// exist or is not accessible (HTTP 404).  Callers that need to distinguish a
// missing repo from other errors should use errors.Is(err, ErrRepoNotFound).
var ErrRepoNotFound = errors.New("repository not found")

// DefaultBaseURL is the public GitHub API base URL.
const DefaultBaseURL = "https://api.github.com"

// Repo is the minimal representation of a GitHub repository as returned by the
// list-org-repos endpoint.  Only the fields needed by the follow-all feature are
// decoded; additional JSON fields are silently ignored.
type Repo struct {
	// Name is the repository name without the owner prefix (e.g. "web").
	Name string `json:"name"`
	// Archived is true when the repository has been archived on GitHub.
	Archived bool `json:"archived"`
}

// ListOrgRepos returns all repositories in the given GitHub organisation.
//
// Endpoint: GET {baseURL}/orgs/{org}/repos?per_page=100&page=N
// Header:   Authorization: Bearer {token}
//
// The response is paginated; ListOrgRepos follows the Link header to retrieve
// all pages automatically.  The baseURL defaults to DefaultBaseURL when empty.
// A non-2xx response is returned as an error.
func ListOrgRepos(ctx context.Context, org, token, baseURL string) ([]Repo, error) {
	if org == "" {
		return nil, fmt.Errorf("github: ListOrgRepos: org must not be empty")
	}
	base := baseURL
	if base == "" {
		base = DefaultBaseURL
	}
	base = strings.TrimRight(base, "/")

	var all []Repo
	// Start with page 1; continue until the Link header no longer contains "next".
	nextURL := base + "/orgs/" + url.PathEscape(org) + "/repos?per_page=100&type=all"
	for nextURL != "" {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, nextURL, nil)
		if err != nil {
			return nil, fmt.Errorf("github: ListOrgRepos: build request: %w", err)
		}
		req.Header.Set("Accept", "application/vnd.github+json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("github: ListOrgRepos %q: %w", org, err)
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			resp.Body.Close() //nolint:errcheck
			return nil, fmt.Errorf("github: ListOrgRepos %q: unexpected status %d", org, resp.StatusCode)
		}

		var page []Repo
		if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
			resp.Body.Close() //nolint:errcheck
			return nil, fmt.Errorf("github: ListOrgRepos %q: decode response: %w", org, err)
		}
		resp.Body.Close() //nolint:errcheck
		all = append(all, page...)

		// Follow the Link: <url>; rel="next" header for pagination.
		nextURL = parseLinkNext(resp.Header.Get("Link"))
	}
	return all, nil
}

// parseLinkNext extracts the "next" URL from a GitHub Link header.
// Returns "" when there is no next page.
//
// Example header value:
//
//	<https://api.github.com/orgs/acme/repos?page=2>; rel="next", <...>; rel="last"
func parseLinkNext(link string) string {
	if link == "" {
		return ""
	}
	// Split on comma to handle multiple relations.
	for _, part := range strings.Split(link, ",") {
		part = strings.TrimSpace(part)
		// Each part looks like: <URL>; rel="xxx"
		semi := strings.Index(part, ";")
		if semi < 0 {
			continue
		}
		urlPart := strings.TrimSpace(part[:semi])
		relPart := strings.TrimSpace(part[semi+1:])
		if !strings.Contains(relPart, `rel="next"`) {
			continue
		}
		// Strip the angle brackets around the URL.
		if len(urlPart) >= 2 && urlPart[0] == '<' && urlPart[len(urlPart)-1] == '>' {
			return urlPart[1 : len(urlPart)-1]
		}
	}
	return ""
}

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
