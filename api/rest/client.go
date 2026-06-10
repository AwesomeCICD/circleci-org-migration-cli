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

	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/clog"
	"github.com/AwesomeCICD/circleci-org-migration-cli/settings"
	"github.com/AwesomeCICD/circleci-org-migration-cli/version"
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
//
// At debug level it logs the METHOD + URL (no auth headers or secrets) and the
// response status. On non-2xx it also logs the CircleCI request-id header and
// a snippet of the response body to aid troubleshooting.
func (c *Client) DoRequest(req *http.Request, resp interface{}) (int, error) {
	// Log the outgoing request at debug level. We deliberately do NOT log any
	// headers (which would expose the Circle-Token / Authorization values).
	clog.Debugf("→ %s %s", req.Method, req.URL.String())

	// The request URL is built from the operator-provided CircleCI host and
	// fixed API paths; issuing it is the entire purpose of this client.
	httpResp, err := c.client.Do(req) // #nosec G704 -- request target is operator-configured, not attacker-controlled
	if err != nil {
		return 0, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer httpResp.Body.Close() //nolint:errcheck

	clog.Debugf("← %d %s", httpResp.StatusCode, req.URL.Path)

	if httpResp.StatusCode >= 400 {
		body, err := io.ReadAll(httpResp.Body)
		if err != nil {
			return httpResp.StatusCode, err
		}

		reqID := httpResp.Header.Get("X-Request-Id")
		if reqID == "" {
			reqID = httpResp.Header.Get("X-Request-ID")
		}
		if reqID == "" {
			reqID = httpResp.Header.Get("Request-Id")
		}

		snippet := bodySnippet(body, 256)
		if reqID != "" {
			clog.Debugf("request-id: %s  body: %s", reqID, snippet)
		} else {
			clog.Debugf("body: %s", snippet)
		}

		var msgErr struct {
			Message string `json:"message"`
		}
		if jsonErr := json.Unmarshal(body, &msgErr); jsonErr == nil && msgErr.Message != "" {
			return httpResp.StatusCode, &HTTPError{
				Code:      httpResp.StatusCode,
				Message:   msgErr.Message,
				RequestID: reqID,
				Path:      req.URL.Path,
				Method:    req.Method,
			}
		}
		return httpResp.StatusCode, &HTTPError{
			Code:      httpResp.StatusCode,
			Message:   string(body),
			RequestID: reqID,
			Path:      req.URL.Path,
			Method:    req.Method,
		}
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

// bodySnippet returns at most maxBytes bytes of body as a printable string,
// appending "…" if truncated. It replaces newlines with spaces so the result
// fits on a single log line.
func bodySnippet(body []byte, maxBytes int) string {
	truncated := len(body) > maxBytes
	if truncated {
		body = body[:maxBytes]
	}
	s := strings.ReplaceAll(string(body), "\n", " ")
	s = strings.ReplaceAll(s, "\r", "")
	if truncated {
		s += "…"
	}
	return s
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
// It carries structured context (status code, request-id, method+path) so
// that top-level command handlers can format actionable error messages.
type HTTPError struct {
	Code      int
	Message   string
	RequestID string // value of X-Request-Id response header, if present
	Method    string // HTTP method of the failed request
	Path      string // URL path of the failed request
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

// IssueURL is the canonical URL for filing a bug report for this CLI.
const IssueURL = "https://github.com/AwesomeCICD/circleci-org-migration-cli/issues"

// ActionableError wraps an error with an operation context and the standard
// hint to re-run with --debug and file an issue if the problem persists.
// It is called once at the outermost command-error boundary (not on every
// nested wrap) so the hint appears exactly once.
//
// Example output:
//
//	list contexts: response 401 (Unauthorized) [GET /api/v2/context request-id: abc123]
//	  → re-run with --debug for full request/response details
//	  → to report this, open an issue at https://github.com/AwesomeCICD/circleci-org-migration-cli/issues with the --debug output
func ActionableError(op string, err error) string {
	if err == nil {
		return ""
	}
	var he *HTTPError
	base := fmt.Sprintf("%s: %v", op, err)

	// Only emit the extended hint for HTTP errors (where request/debug context
	// is meaningful). For non-HTTP errors (e.g. file not found) the plain
	// message is more helpful.
	if !isHTTPError(err, &he) {
		return base
	}

	// Include structured HTTP context if available.
	var detail string
	if he.Method != "" && he.Path != "" {
		detail = fmt.Sprintf(" [%s %s", he.Method, he.Path)
		if he.RequestID != "" {
			detail += " request-id: " + he.RequestID
		}
		detail += "]"
	} else if he.RequestID != "" {
		detail = " [request-id: " + he.RequestID + "]"
	}

	return fmt.Sprintf("%s%s\n  → re-run with --debug for full request/response details\n  → to report this, open an issue at %s with the --debug output",
		base, detail, IssueURL)
}

// isHTTPError unwraps err to find an *HTTPError and stores it in out when found.
func isHTTPError(err error, out **HTTPError) bool {
	var he *HTTPError
	// Walk the error chain manually (errors.As would need the errors package).
	type unwrapper interface{ Unwrap() error }
	for e := err; e != nil; {
		if h, ok := e.(*HTTPError); ok {
			*out = h
			he = h
			_ = he
			return true
		}
		if u, ok := e.(unwrapper); ok {
			e = u.Unwrap()
		} else {
			break
		}
	}
	return false
}
