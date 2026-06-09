// Package rest provides an HTTP client for the CircleCI v2 REST API.
// It mirrors github.com/CircleCI-Public/circleci-cli/api/rest/client.go so
// that merging into that repo in the future requires minimal adaptation.
package rest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/CircleCI-Public/circleci-org-migration-cli/settings"
	"github.com/CircleCI-Public/circleci-org-migration-cli/version"
)

const defaultTimeout = 30 * time.Second

// Client is a CircleCI REST API client.
type Client struct {
	BaseURL     *url.URL
	circleToken string
	client      *http.Client
}

// New constructs a Client from explicit parameters.
func New(baseURL *url.URL, token string, httpClient *http.Client) *Client {
	return &Client{
		BaseURL:     baseURL,
		circleToken: token,
		client:      httpClient,
	}
}

// NewFromConfig constructs a Client from a settings.Config.  The token
// parameter is passed explicitly so callers can choose the source or
// destination token without mutating the shared Config.
func NewFromConfig(host string, cfg *settings.Config, token string) *Client {
	endpoint := cfg.RestEndpoint
	if !strings.HasSuffix(endpoint, "/") {
		endpoint += "/"
	}

	baseURL, err := url.Parse(host)
	if err != nil || baseURL.Host == "" {
		panic("circleci-migrate: invalid CircleCI host URL: " + host)
	}

	timeout := defaultTimeout
	if v, ok := os.LookupEnv("CIRCLECI_CLI_TIMEOUT"); ok {
		if d, err := time.ParseDuration(v); err == nil {
			timeout = d
		} else {
			fmt.Fprintf(os.Stderr, "warning: failed to parse CIRCLECI_CLI_TIMEOUT %q: %v\n", v, err)
		}
	}

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	httpClient.Timeout = timeout

	return New(
		baseURL.ResolveReference(&url.URL{Path: endpoint}),
		token,
		httpClient,
	)
}

// NewRequest builds an *http.Request, JSON-encoding payload when non-nil.
func (c *Client) NewRequest(method string, u *url.URL, payload interface{}) (*http.Request, error) {
	var r io.Reader
	if payload != nil {
		buf := &bytes.Buffer{}
		if err := json.NewEncoder(buf).Encode(payload); err != nil {
			return nil, err
		}
		r = buf
	}

	req, err := http.NewRequest(method, c.BaseURL.ResolveReference(u).String(), r)
	if err != nil {
		return nil, err
	}

	c.enrichRequestHeaders(req, payload)
	return req, nil
}

func (c *Client) enrichRequestHeaders(req *http.Request, payload interface{}) {
	if c.circleToken != "" {
		req.Header.Set("Circle-Token", c.circleToken)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", version.UserAgent())
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
}

// DoRequest executes req and JSON-decodes a successful response into resp.
// It returns the HTTP status code and any error.
func (c *Client) DoRequest(req *http.Request, resp interface{}) (int, error) {
	// The request URL is built from the operator-provided CircleCI host and
	// fixed API paths; issuing it is the entire purpose of this client.
	httpResp, err := c.client.Do(req) // #nosec G704 -- request target is operator-configured, not attacker-controlled
	if err != nil {
		return 0, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer httpResp.Body.Close() //nolint:errcheck

	if httpResp.StatusCode >= 400 {
		body, err := io.ReadAll(httpResp.Body)
		if err != nil {
			return httpResp.StatusCode, err
		}
		var msgErr struct {
			Message string `json:"message"`
		}
		if jsonErr := json.Unmarshal(body, &msgErr); jsonErr == nil && msgErr.Message != "" {
			return httpResp.StatusCode, &HTTPError{Code: httpResp.StatusCode, Message: msgErr.Message}
		}
		return httpResp.StatusCode, &HTTPError{Code: httpResp.StatusCode, Message: string(body)}
	}

	if resp != nil {
		ct := httpResp.Header.Get("Content-Type")
		if !strings.Contains(ct, "application/json") {
			body, _ := io.ReadAll(httpResp.Body)
			return httpResp.StatusCode, fmt.Errorf(
				"unexpected content-type %q for %s %s: %s",
				ct, req.Method, req.URL.Path, string(body),
			)
		}
		if err := json.NewDecoder(httpResp.Body).Decode(resp); err != nil {
			return httpResp.StatusCode, err
		}
	}

	return httpResp.StatusCode, nil
}

// EnrichDownloadRequest adds the Circle-Token authentication header (and
// User-Agent) to req so that private artifacts on circle-artifacts.com can be
// downloaded using the same token as the rest of the API calls. It does NOT
// set Content-Type (artifact requests have no body).
func (c *Client) EnrichDownloadRequest(req *http.Request) {
	if c.circleToken != "" {
		req.Header.Set("Circle-Token", c.circleToken)
	}
	req.Header.Set("User-Agent", version.UserAgent())
}

// RawDo executes req and returns the raw *http.Response without decoding.
// The caller is responsible for closing resp.Body. Unlike DoRequest it does
// not interpret the response body; it is used for artifact downloads where the
// content-type may not be JSON.
func (c *Client) RawDo(req *http.Request) (*http.Response, error) {
	resp, err := c.client.Do(req) // #nosec G704 -- caller-provided URL, not attacker-controlled
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	return resp, nil
}

// HTTPError represents an HTTP-level error response from the CircleCI API.
type HTTPError struct {
	Code    int
	Message string
}

func (e *HTTPError) Error() string {
	if e.Code == 0 {
		e.Code = http.StatusInternalServerError
	}
	if e.Message != "" {
		return e.Message
	}
	return fmt.Sprintf("response %d (%s)", e.Code, http.StatusText(e.Code))
}
