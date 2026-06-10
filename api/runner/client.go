// Package runner provides a client for the CircleCI self-hosted runner API,
// which lives on a separate host (runner.circleci.com) from the main v2 API.
// Auth uses the same Circle-Token header as the rest of the CircleCI API.
package runner

import (
	"fmt"
	"net/http"
	"net/url"

	"github.com/AwesomeCICD/circleci-org-migration-cli/api/rest"
	"github.com/AwesomeCICD/circleci-org-migration-cli/settings"
)

const (
	// RunnerHost is the base URL for the CircleCI runner API.
	RunnerHost = "https://runner.circleci.com"
	// runnerAPIBase is the path prefix for runner API v3.
	runnerAPIBase = "api/v3/"
)

// ResourceClass is a self-hosted runner resource class definition.
// Only the class definition is captured; ephemeral runner instances
// (returned in a "runners" array by the live API) are intentionally ignored.
type ResourceClass struct {
	// ID is the UUID assigned by CircleCI to this resource class.
	ID string `json:"id"`
	// ResourceClass is the full "<namespace>/<name>" identifier.
	ResourceClass string `json:"resource_class"`
	// Description is the human-readable description set at creation time.
	Description string `json:"description"`
}

// Client is a CircleCI runner API v3 client.
type Client struct {
	rest *rest.Client
}

// NewClient constructs a Client using the runner API host.
// cfg and token follow the same convention as the other API clients: cfg
// provides the HTTP client (timeout, transport), and token is the
// Circle-Token value.  The runner host is always runner.circleci.com
// regardless of cfg.Host (which points at circleci.com).
func NewClient(cfg *settings.Config, token string) (*Client, error) {
	return NewClientWithBase(RunnerHost, token, cfg)
}

// NewClientWithBase constructs a Client pointed at baseHost instead of the
// default runner.circleci.com. It is exported so that tests can substitute
// an httptest.Server URL. cfg supplies the HTTP client.
func NewClientWithBase(baseHost, token string, cfg *settings.Config) (*Client, error) {
	var httpClient *http.Client
	if cfg != nil {
		httpClient = cfg.HTTPClient
	}
	return newClientWithHTTP(baseHost, token, httpClient)
}

// newClientWithHTTP is the low-level constructor used internally.
func newClientWithHTTP(baseHost, token string, httpClient *http.Client) (*Client, error) {
	base, err := url.Parse(baseHost)
	if err != nil || base.Host == "" {
		return nil, fmt.Errorf("runner: invalid host URL %q: %w", baseHost, err)
	}
	apiBase, err := base.Parse(runnerAPIBase)
	if err != nil {
		return nil, fmt.Errorf("runner: building API base URL: %w", err)
	}
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	return &Client{rest: rest.New(apiBase, token, httpClient)}, nil
}

// listResourceClassesResponse is the JSON envelope returned by the runner list API.
type listResourceClassesResponse struct {
	Items []ResourceClass `json:"items"`
}

// createResourceClassRequest is the POST body for creating a resource class.
type createResourceClassRequest struct {
	ResourceClass string `json:"resource_class"`
	Description   string `json:"description,omitempty"`
}

// GetResourceClassesByNamespace lists all resource classes registered under
// namespace. namespace must be a bare name (e.g. "acme"), not a slug.
//
// GET https://runner.circleci.com/api/v3/runner/resource?namespace=<ns>
func (c *Client) GetResourceClassesByNamespace(namespace string) ([]ResourceClass, error) {
	u, err := url.Parse("runner/resource")
	if err != nil {
		return nil, fmt.Errorf("runner: building URL: %w", err)
	}
	q := u.Query()
	q.Set("namespace", namespace)
	u.RawQuery = q.Encode()

	req, err := c.rest.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("runner: GetResourceClassesByNamespace: building request: %w", err)
	}

	var resp listResourceClassesResponse
	if _, err := c.rest.DoRequest(req, &resp); err != nil {
		return nil, fmt.Errorf("runner: GetResourceClassesByNamespace: %w", err)
	}
	return resp.Items, nil
}

// CreateResourceClass creates a new runner resource class with the given
// full name (<namespace>/<class-name>) and optional description.
//
// POST https://runner.circleci.com/api/v3/runner/resource
func (c *Client) CreateResourceClass(resourceClass, description string) (*ResourceClass, error) {
	u, err := url.Parse("runner/resource")
	if err != nil {
		return nil, fmt.Errorf("runner: building URL: %w", err)
	}

	body := createResourceClassRequest{
		ResourceClass: resourceClass,
		Description:   description,
	}
	req, err := c.rest.NewRequest(http.MethodPost, u, body)
	if err != nil {
		return nil, fmt.Errorf("runner: CreateResourceClass: building request: %w", err)
	}

	var created ResourceClass
	if _, err := c.rest.DoRequest(req, &created); err != nil {
		return nil, fmt.Errorf("runner: CreateResourceClass: %w", err)
	}
	return &created, nil
}

// DeleteResourceClass deletes the resource class with the given UUID.
//
// DELETE https://runner.circleci.com/api/v3/runner/resource/<id>
func (c *Client) DeleteResourceClass(id string) error {
	u, err := url.Parse("runner/resource/" + id)
	if err != nil {
		return fmt.Errorf("runner: building URL: %w", err)
	}

	req, err := c.rest.NewRequest(http.MethodDelete, u, nil)
	if err != nil {
		return fmt.Errorf("runner: DeleteResourceClass: building request: %w", err)
	}

	if _, err := c.rest.DoRequest(req, nil); err != nil {
		return fmt.Errorf("runner: DeleteResourceClass: %w", err)
	}
	return nil
}
