package org

import (
	"context"
	"fmt"
	"net/url"
)

// Organization represents a CircleCI organization as returned by API v2.
//
// JSON field names confirmed from:
//   - https://circleci.com/docs/api/v2/index.html  (getOrganization response schema)
//   - github.com/CircleCI-Public/circleci-cli api/collaborators/collaborators.go
type Organization struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Slug    string `json:"slug"`
	VCSType string `json:"vcs_type"`
}

// GetOrganization retrieves an organization by its slug (e.g. "gh/acme") or UUID.
//
// Endpoint: GET /api/v2/organization/{org-slug-or-id}
//
// The slug may contain a slash (e.g. "gh/acme").  The API treats the entire
// path segment as a single opaque identifier, so the slash is percent-encoded
// (%2F) before being embedded in the URL path.  This is confirmed by the API
// spec: https://circleci.com/docs/api/v2/index.html – the parameter is a
// single path component labelled "org-slug-or-id".
func (c *Client) GetOrganization(ctx context.Context, slugOrID string) (*Organization, error) {
	// The slug may contain a '/' (e.g. "gh/acme") that must travel as a single,
	// percent-encoded path segment (gh%2Facme). Building the URL via url.Parse on
	// the pre-escaped string sets both Path and RawPath, so ResolveReference and
	// String emit the escaping exactly once. Assigning an already-escaped string
	// to url.URL{Path: ...} double-encodes the '%' (gh%252Facme) — a subtle bug.
	u, err := url.Parse("organization/" + url.PathEscape(slugOrID))
	if err != nil {
		return nil, fmt.Errorf("GetOrganization: build URL for %q: %w", slugOrID, err)
	}

	req, err := c.v2.NewRequest(ctx, "GET", u, nil)
	if err != nil {
		return nil, fmt.Errorf("GetOrganization: build request: %w", err)
	}

	var org Organization
	if _, err := c.v2.DoRequest(req, &org); err != nil {
		return nil, fmt.Errorf("GetOrganization %q: %w", slugOrID, err)
	}
	return &org, nil
}

// ListCollaborations returns every organization the authenticated user
// collaborates with.
//
// Endpoint: GET /api/v2/me/collaborations
//
// The response is a flat JSON array (not paginated).  JSON field names
// confirmed from github.com/CircleCI-Public/circleci-cli
// api/collaborators/collaborators.go:
//
//	vcs_type, slug, name, id, avatar_url
//
// We map into []Organization (ignoring avatar_url which is not part of
// Organization).
func (c *Client) ListCollaborations(ctx context.Context) ([]Organization, error) {
	u := &url.URL{Path: "me/collaborations"}

	req, err := c.v2.NewRequest(ctx, "GET", u, nil)
	if err != nil {
		return nil, fmt.Errorf("ListCollaborations: build request: %w", err)
	}

	// The raw response includes avatar_url which we intentionally discard.
	var raw []struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Slug    string `json:"slug"`
		VCSType string `json:"vcs_type"`
	}
	if _, err := c.v2.DoRequest(req, &raw); err != nil {
		return nil, fmt.Errorf("ListCollaborations: %w", err)
	}

	orgs := make([]Organization, len(raw))
	for i, r := range raw {
		orgs[i] = Organization{
			ID:      r.ID,
			Name:    r.Name,
			Slug:    r.Slug,
			VCSType: r.VCSType,
		}
	}
	return orgs, nil
}

// ResolveOrgID resolves a slug or UUID to a plain organization UUID string.
//
// Rules (applied in order):
//  1. If slug is already a bare UUID (xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx),
//     return it directly.
//  2. If slug has the form "circleci/<uuid>", extract and return the UUID.
//  3. Otherwise call GetOrganization and return its ID field.
func (c *Client) ResolveOrgID(ctx context.Context, slug string) (string, error) {
	// Case 1: bare UUID.
	if isBareUUID(slug) {
		return slug, nil
	}

	// Case 2: circleci/<uuid> slug.
	if uuid, ok := slugIsCIRCLECIUUID(slug); ok {
		return uuid, nil
	}

	// Case 3: full slug lookup.
	org, err := c.GetOrganization(ctx, slug)
	if err != nil {
		return "", fmt.Errorf("ResolveOrgID %q: %w", slug, err)
	}
	return org.ID, nil
}
