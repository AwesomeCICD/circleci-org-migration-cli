// Package project provides a CircleCI API client for project-level operations.
// It uses both API v2 (for project details, env vars, checkout keys, webhooks,
// schedules, and advanced settings) and API v1.1 (for followed-projects list and
// the follow-project write action).
package project

import (
	"fmt"
	"net/http"
	"net/url"

	"github.com/CircleCI-Public/circleci-org-migration-cli/api/rest"
	"github.com/CircleCI-Public/circleci-org-migration-cli/settings"
)

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
		return nil, fmt.Errorf("project.NewClient: invalid host %q: %w", host, err)
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
