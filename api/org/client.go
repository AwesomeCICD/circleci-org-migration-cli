// Package org provides a CircleCI API client for organization-level operations.
// It uses both API v2 (for organization lookup and collaborations) and API v1.1
// (for legacy org settings such as feature flags).
package org

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/CircleCI-Public/circleci-org-migration-cli/api/rest"
	"github.com/CircleCI-Public/circleci-org-migration-cli/settings"
)

// uuidLen is the length of a canonical UUID (8-4-4-4-12 hex groups). We detect
// bare UUIDs cheaply by length + hyphen positions; full RFC-4122 validation is
// unnecessary here.
const uuidLen = 36

// Client holds REST clients for API v2 and v1.1.
type Client struct {
	v2  *rest.Client
	v11 *rest.Client
}

// NewClient constructs a Client from the provided config and token.
// Both the v2 and v1.1 base URLs are derived from cfg.Host.
func NewClient(cfg *settings.Config, token string) (*Client, error) {
	host := cfg.Host
	if host == "" {
		host = settings.DefaultHost
	}

	base, err := url.Parse(host)
	if err != nil || base.Host == "" {
		return nil, fmt.Errorf("org.NewClient: invalid host %q: %w", host, err)
	}

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{}
	}

	v2Base := base.ResolveReference(&url.URL{Path: "/api/v2/"})
	v11Base := base.ResolveReference(&url.URL{Path: "/api/v1.1/"})

	return &Client{
		v2:  rest.New(v2Base, token, httpClient),
		v11: rest.New(v11Base, token, httpClient),
	}, nil
}

// newClientFromBases is an unexported constructor used by tests to inject
// explicit base URLs without going through settings.Config.
func newClientFromBases(v2Base, v11Base *url.URL, token string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	return &Client{
		v2:  rest.New(v2Base, token, httpClient),
		v11: rest.New(v11Base, token, httpClient),
	}
}

// isBareUUID returns true if s looks like a raw UUID (xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx).
func isBareUUID(s string) bool {
	if len(s) != uuidLen {
		return false
	}
	for i, c := range s {
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			if !isHexRune(c) {
				return false
			}
		}
	}
	return true
}

func isHexRune(r rune) bool {
	return (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')
}

// slugIsCIRCLECIUUID returns (uuid, true) when slug has the form "circleci/<uuid>".
func slugIsCIRCLECIUUID(slug string) (string, bool) {
	const prefix = "circleci/"
	if !strings.HasPrefix(slug, prefix) {
		return "", false
	}
	rest := slug[len(prefix):]
	if isBareUUID(rest) {
		return rest, true
	}
	return "", false
}
