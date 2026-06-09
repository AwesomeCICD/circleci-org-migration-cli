// Package context provides a CircleCI API v2 client for context operations:
// contexts, environment variables, and restrictions. Group/security-group
// names are read from the restrictions endpoint (which returns them), so no
// GraphQL is needed.
package context

import (
	"fmt"
	"net/http"
	"net/url"

	"github.com/CircleCI-Public/circleci-org-migration-cli/api/rest"
	"github.com/CircleCI-Public/circleci-org-migration-cli/settings"
)

// Client is a CircleCI v2 REST client for context operations.
type Client struct {
	rest *rest.Client
}

// NewClient constructs a Client from the provided settings and API token. The
// REST base URL is built as <host>/api/v2/ (trailing slash).
func NewClient(cfg *settings.Config, token string) (*Client, error) {
	return newClientWithURLs(cfg.Host, token, cfg.HTTPClient)
}

// newClientWithURLs is the injectable constructor used by tests.
func newClientWithURLs(host, token string, httpClient *http.Client) (*Client, error) {
	base, err := url.Parse(host)
	if err != nil || base.Host == "" {
		return nil, fmt.Errorf("context: invalid host URL %q: %w", host, err)
	}
	restBase, err := base.Parse("api/v2/")
	if err != nil {
		return nil, fmt.Errorf("context: building REST base URL: %w", err)
	}
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	return &Client{rest: rest.New(restBase, token, httpClient)}, nil
}
