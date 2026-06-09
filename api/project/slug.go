package project

import (
	"fmt"
	"net/url"
	"strings"
)

// SplitSlug splits a project slug of the form "vcs/org/repo" (e.g. "gh/acme/web"
// or "circleci/<org-id>/<proj-id>") into its three components.
// It returns an error if the slug does not contain exactly two slash separators.
func SplitSlug(slug string) (provider, org, proj string, err error) {
	parts := strings.SplitN(slug, "/", 3)
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return "", "", "", fmt.Errorf("project: invalid slug %q: expected vcs/org/repo", slug)
	}
	return parts[0], parts[1], parts[2], nil
}

// slugPath builds a URL for a project slug path of the form:
//
//	project/<provider>/<org>/<repo>
//
// The three slug components are percent-encoded individually so that spaces and
// other special characters in org/repo names are safe on the wire.  The slash
// separators between components are left as literal '/' because the CircleCI API
// treats them as delimiters, not as part of a single path segment.
//
// IMPORTANT: we use url.Parse on the pre-built string (rather than assigning to
// url.URL{Path:…}) so that url.Parse sets both the Path and RawPath fields.
// This prevents net/url's resolver from double-encoding any percent signs that
// are already present in the escaped segments.
func slugPath(prefix, slug string) (*url.URL, error) {
	provider, org, proj, err := SplitSlug(slug)
	if err != nil {
		return nil, err
	}
	raw := prefix +
		url.PathEscape(provider) + "/" +
		url.PathEscape(org) + "/" +
		url.PathEscape(proj)
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("project: build URL for slug %q: %w", slug, err)
	}
	return u, nil
}

// slugSubresource builds a URL for a project slug sub-resource path of the form:
//
//	project/<provider>/<org>/<repo>/<subresource>
//
// It works exactly like slugPath but appends "/<subresource>" after the encoded
// slug.  The subresource string must contain only safe ASCII characters (no
// encoding needed, e.g. "envvar", "checkout-key", "schedule").
func slugSubresource(slug, subresource string) (*url.URL, error) {
	provider, org, proj, err := SplitSlug(slug)
	if err != nil {
		return nil, err
	}
	raw := "project/" +
		url.PathEscape(provider) + "/" +
		url.PathEscape(org) + "/" +
		url.PathEscape(proj) + "/" + subresource
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("project: build URL for slug %q subresource %q: %w", slug, subresource, err)
	}
	return u, nil
}
