// Package project provides a CircleCI API client for project-level operations.
// It uses API v2 (for project details, env vars, checkout keys, webhooks,
// schedules, advanced settings, pipeline definitions, and triggers), API v1.1
// (for followed-projects list and the follow-project write action), and the
// private API (for org-scoped project discovery that covers both GitHub OAuth
// and GitHub App orgs).
package project

import (
	"fmt"
	"net/http"
	"net/url"

	"github.com/AwesomeCICD/circleci-org-migration-cli/api/rest"
	"github.com/AwesomeCICD/circleci-org-migration-cli/settings"
)

// Client holds REST clients for API v2, v1.1, and the private API.
//
// private is a client against the same host as v2 but rooted at
// /api/private/, used for org-scoped project discovery
// (GET /api/private/project?organization-id=…) which covers both GitHub OAuth
// and GitHub App orgs.
type Client struct {
	v2      *rest.Client
	v11     *rest.Client
	private *rest.Client
}

// NewClient constructs a Client from the provided config and token.
// The v2, v1.1, and private base URLs are all derived from cfg.Host.
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
	// The private API lives on the same host as v2 (e.g. circleci.com) but
	// under /api/private/.
	privateBase := base.ResolveReference(&url.URL{Path: "/api/private/"})

	return &Client{
		v2:      rest.New(v2Base, token, httpClient),
		v11:     rest.New(v11Base, token, httpClient),
		private: rest.New(privateBase, token, httpClient),
	}, nil
}

// newClientFromBases is an unexported constructor used by tests to inject
// explicit base URLs without going through settings.Config.  The private base
// is derived from v2Base's host at /api/private/.
func newClientFromBases(v2Base, v11Base *url.URL, token string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	privateBase := v2Base.ResolveReference(&url.URL{Path: "/api/private/"})
	return &Client{
		v2:      rest.New(v2Base, token, httpClient),
		v11:     rest.New(v11Base, token, httpClient),
		private: rest.New(privateBase, token, httpClient),
	}
}
