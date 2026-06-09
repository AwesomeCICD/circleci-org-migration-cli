// Package context provides a client for CircleCI context-related APIs:
// the v2 REST API (contexts, env vars, restrictions) and the GraphQL
// unstable endpoint (org/context groups).
package context

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/CircleCI-Public/circleci-org-migration-cli/api/rest"
	"github.com/CircleCI-Public/circleci-org-migration-cli/settings"
)

const graphQLPath = "graphql-unstable"

// Client holds the REST v2 client and the parameters needed for GraphQL calls.
type Client struct {
	rest       *rest.Client
	gqlBaseURL *url.URL
	token      string
	httpClient *http.Client
}

// NewClient constructs a Client from the provided settings and API token.
// The REST base URL is built as <host>/api/v2/ (trailing slash) and the
// GraphQL URL as <host>/graphql-unstable.
func NewClient(cfg *settings.Config, token string) (*Client, error) {
	return newClientWithURLs(cfg.Host, token, cfg.HTTPClient)
}

// newClientWithURLs is the injectable constructor used by tests.
func newClientWithURLs(host, token string, httpClient *http.Client) (*Client, error) {
	base, err := url.Parse(host)
	if err != nil || base.Host == "" {
		return nil, fmt.Errorf("context: invalid host URL %q: %w", host, err)
	}

	// REST base: <host>/api/v2/  — trailing slash so that relative paths
	// resolve correctly with url.URL.ResolveReference.
	restEndpoint := "api/v2/"
	if !strings.HasSuffix(restEndpoint, "/") {
		restEndpoint += "/"
	}
	restBase, err := base.Parse(restEndpoint)
	if err != nil {
		return nil, fmt.Errorf("context: building REST base URL: %w", err)
	}

	// GraphQL endpoint: <host>/graphql-unstable
	gqlBase, err := base.Parse(graphQLPath)
	if err != nil {
		return nil, fmt.Errorf("context: building GraphQL base URL: %w", err)
	}

	if httpClient == nil {
		httpClient = &http.Client{}
	}

	return &Client{
		rest:       rest.New(restBase, token, httpClient),
		gqlBaseURL: gqlBase,
		token:      token,
		httpClient: httpClient,
	}, nil
}
