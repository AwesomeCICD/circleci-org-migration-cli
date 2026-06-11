package rest_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/AwesomeCICD/circleci-org-migration-cli/api/rest"
	"github.com/AwesomeCICD/circleci-org-migration-cli/settings"
	"github.com/AwesomeCICD/circleci-org-migration-cli/version"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func mustParseURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("url.Parse(%q): %v", raw, err)
	}
	return u
}

// relURL returns a *url.URL with only a path component so that
// Client.NewRequest resolves it against BaseURL.
func relURL(path string) *url.URL {
	return &url.URL{Path: path}
}

// ---------------------------------------------------------------------------
// New / NewFromConfig
// ---------------------------------------------------------------------------

func TestNew_StoresBaseURLAndToken(t *testing.T) {
	base := mustParseURL(t, "https://circleci.com/api/v2/")
	c := rest.New(base, "tok123", &http.Client{})
	if c.BaseURL.String() != base.String() {
		t.Errorf("BaseURL = %q; want %q", c.BaseURL, base)
	}
}

func TestNewFromConfig_BaseURLHasTrailingSlash(t *testing.T) {
	cfg := &settings.Config{
		RestEndpoint: "api/v2",
		HTTPClient:   &http.Client{},
	}
	c, err := rest.NewFromConfig("https://circleci.com", cfg, "tok")
	if err != nil {
		t.Fatalf("NewFromConfig: %v", err)
	}
	if !strings.HasSuffix(c.BaseURL.String(), "/") {
		t.Errorf("BaseURL = %q; must end with '/'", c.BaseURL)
	}
}

func TestNewFromConfig_InvalidHostReturnsError(t *testing.T) {
	cfg := &settings.Config{
		RestEndpoint: "api/v2",
		HTTPClient:   &http.Client{},
	}
	for _, host := range []string{"", "://nope", "not-a-url"} {
		c, err := rest.NewFromConfig(host, cfg, "tok")
		if err == nil {
			t.Errorf("NewFromConfig(%q): expected error, got client %v", host, c)
		}
		if c != nil {
			t.Errorf("NewFromConfig(%q): expected nil client on error, got %v", host, c)
		}
	}
}

func TestNewFromConfig_BaseURLContainsHostAndEndpoint(t *testing.T) {
	cfg := &settings.Config{
		RestEndpoint: "api/v2",
		HTTPClient:   &http.Client{},
	}
	c, err := rest.NewFromConfig("https://example.circleci.com", cfg, "tok")
	if err != nil {
		t.Fatalf("NewFromConfig: %v", err)
	}
	got := c.BaseURL.String()
	if !strings.Contains(got, "example.circleci.com") {
		t.Errorf("BaseURL = %q; expected to contain host", got)
	}
	if !strings.Contains(got, "api/v2") {
		t.Errorf("BaseURL = %q; expected to contain endpoint path", got)
	}
}

func TestNewFromConfig_ExplicitTokenIsUsed(t *testing.T) {
	cfg := &settings.Config{
		Token:        "config-token",
		RestEndpoint: "api/v2",
		HTTPClient:   &http.Client{},
	}
	// Pass a different token explicitly — it must be the one used.
	c, err := rest.NewFromConfig("https://circleci.com", cfg, "explicit-token")
	if err != nil {
		t.Fatalf("NewFromConfig: %v", err)
	}

	base := mustParseURL(t, "https://circleci.com/")
	req, err := c.NewRequest(context.Background(), http.MethodGet, base, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if got := req.Header.Get("Circle-Token"); got != "explicit-token" {
		t.Errorf("Circle-Token = %q; want %q", got, "explicit-token")
	}
}

func TestNewFromConfig_TimeoutFromEnv(t *testing.T) {
	t.Setenv("CIRCLECI_CLI_TIMEOUT", "5s")
	cfg := &settings.Config{
		RestEndpoint: "api/v2",
		HTTPClient:   &http.Client{},
	}
	// Should not error; timeout applied silently.
	if _, err := rest.NewFromConfig("https://circleci.com", cfg, "tok"); err != nil {
		t.Fatalf("NewFromConfig: %v", err)
	}
}

func TestNewFromConfig_InvalidTimeoutDoesNotPanic(t *testing.T) {
	t.Setenv("CIRCLECI_CLI_TIMEOUT", "not-a-duration")
	cfg := &settings.Config{
		RestEndpoint: "api/v2",
		HTTPClient:   &http.Client{},
	}
	// Should not error; warning is printed to stderr but execution continues.
	if _, err := rest.NewFromConfig("https://circleci.com", cfg, "tok"); err != nil {
		t.Fatalf("NewFromConfig: %v", err)
	}
}

func TestNewFromConfig_NilHTTPClientIsHandled(t *testing.T) {
	cfg := &settings.Config{
		RestEndpoint: "api/v2",
		HTTPClient:   nil,
	}
	// Must not error when HTTPClient is nil.
	if _, err := rest.NewFromConfig("https://circleci.com", cfg, "tok"); err != nil {
		t.Fatalf("NewFromConfig: %v", err)
	}
}

// ---------------------------------------------------------------------------
// NewRequest headers
// ---------------------------------------------------------------------------

func TestNewRequest_CircleTokenSetWhenNonEmpty(t *testing.T) {
	c := rest.New(mustParseURL(t, "https://circleci.com/api/v2/"), "mytoken", &http.Client{})
	req, err := c.NewRequest(context.Background(), http.MethodGet, relURL("me"), nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if got := req.Header.Get("Circle-Token"); got != "mytoken" {
		t.Errorf("Circle-Token = %q; want %q", got, "mytoken")
	}
}

func TestNewRequest_CircleTokenNotSetWhenEmpty(t *testing.T) {
	c := rest.New(mustParseURL(t, "https://circleci.com/api/v2/"), "", &http.Client{})
	req, err := c.NewRequest(context.Background(), http.MethodGet, relURL("me"), nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if got := req.Header.Get("Circle-Token"); got != "" {
		t.Errorf("Circle-Token = %q; want empty (token should not be set)", got)
	}
}

func TestNewRequest_AcceptHeaderIsJSON(t *testing.T) {
	c := rest.New(mustParseURL(t, "https://circleci.com/api/v2/"), "tok", &http.Client{})
	req, err := c.NewRequest(context.Background(), http.MethodGet, relURL("me"), nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if got := req.Header.Get("Accept"); got != "application/json" {
		t.Errorf("Accept = %q; want %q", got, "application/json")
	}
}

func TestNewRequest_UserAgentIsSet(t *testing.T) {
	c := rest.New(mustParseURL(t, "https://circleci.com/api/v2/"), "tok", &http.Client{})
	req, err := c.NewRequest(context.Background(), http.MethodGet, relURL("me"), nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	want := version.UserAgent()
	if got := req.Header.Get("User-Agent"); got != want {
		t.Errorf("User-Agent = %q; want %q", got, want)
	}
}

func TestNewRequest_ContentTypeSetWhenPayloadProvided(t *testing.T) {
	c := rest.New(mustParseURL(t, "https://circleci.com/api/v2/"), "tok", &http.Client{})
	payload := map[string]string{"key": "value"}
	req, err := c.NewRequest(context.Background(), http.MethodPost, relURL("resource"), payload)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if got := req.Header.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q; want %q", got, "application/json")
	}
}

func TestNewRequest_ContentTypeNotSetWhenNoPayload(t *testing.T) {
	c := rest.New(mustParseURL(t, "https://circleci.com/api/v2/"), "tok", &http.Client{})
	req, err := c.NewRequest(context.Background(), http.MethodGet, relURL("resource"), nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if got := req.Header.Get("Content-Type"); got != "" {
		t.Errorf("Content-Type = %q; want empty (no payload)", got)
	}
}

func TestNewRequest_JSONBodyEncoding(t *testing.T) {
	c := rest.New(mustParseURL(t, "https://circleci.com/api/v2/"), "tok", &http.Client{})
	payload := map[string]string{"name": "hello"}
	req, err := c.NewRequest(context.Background(), http.MethodPost, relURL("resource"), payload)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if req.Body == nil {
		t.Fatal("expected non-nil request body")
	}
	var decoded map[string]string
	if err := json.NewDecoder(req.Body).Decode(&decoded); err != nil {
		t.Fatalf("body is not valid JSON: %v", err)
	}
	if decoded["name"] != "hello" {
		t.Errorf("decoded[name] = %q; want %q", decoded["name"], "hello")
	}
}

// ---------------------------------------------------------------------------
// DoRequest
// ---------------------------------------------------------------------------

type responseBody struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func newTestClient(t *testing.T, handler http.Handler) (*rest.Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	base, err := url.Parse(srv.URL + "/api/v2/")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	c := rest.New(base, "tok", srv.Client())
	return c, srv
}

func TestDoRequest_200DecodesJSON(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{"id":"abc","name":"project"}`)
	}))

	req, err := c.NewRequest(context.Background(), http.MethodGet, relURL("project/x"), nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}

	var out responseBody
	code, err := c.DoRequest(req, &out)
	if err != nil {
		t.Fatalf("DoRequest error: %v", err)
	}
	if code != http.StatusOK {
		t.Errorf("status = %d; want 200", code)
	}
	if out.ID != "abc" || out.Name != "project" {
		t.Errorf("decoded body = %+v; want {abc project}", out)
	}
}

func TestDoRequest_400WithJSONMessageReturnsHTTPError(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintln(w, `{"message":"bad request param"}`)
	}))

	req, err := c.NewRequest(context.Background(), http.MethodGet, relURL("anything"), nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}

	code, err := c.DoRequest(req, nil)
	if err == nil {
		t.Fatal("expected error for 400 response, got nil")
	}
	if code != http.StatusBadRequest {
		t.Errorf("code = %d; want 400", code)
	}
	var httpErr *rest.HTTPError
	ok := false
	// Use type assertion via the error interface.
	if he, isHTTP := err.(*rest.HTTPError); isHTTP {
		httpErr = he
		ok = true
	}
	if !ok || httpErr == nil {
		t.Fatalf("error is %T, want *rest.HTTPError", err)
	}
	if httpErr.Error() != "bad request param" {
		t.Errorf("HTTPError.Error() = %q; want %q", httpErr.Error(), "bad request param")
	}
}

func TestDoRequest_404WithNonJSONBodyReturnsRawBody(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, "not found here")
	}))

	req, err := c.NewRequest(context.Background(), http.MethodGet, relURL("missing"), nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}

	code, err := c.DoRequest(req, nil)
	if err == nil {
		t.Fatal("expected error for 404 response, got nil")
	}
	if code != http.StatusNotFound {
		t.Errorf("code = %d; want 404", code)
	}
	he, ok := err.(*rest.HTTPError)
	if !ok {
		t.Fatalf("error is %T, want *rest.HTTPError", err)
	}
	if he.Message != "not found here" {
		t.Errorf("HTTPError.Message = %q; want %q", he.Message, "not found here")
	}
}

func TestDoRequest_NonJSONContentTypeOnSuccessReturnsError(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "plain text response")
	}))

	req, err := c.NewRequest(context.Background(), http.MethodGet, relURL("plain"), nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}

	var out responseBody
	_, err = c.DoRequest(req, &out)
	if err == nil {
		t.Error("expected error for non-JSON content-type, got nil")
	}
}

func TestDoRequest_NilRespPointerIsOK(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, `{}`)
	}))

	req, err := c.NewRequest(context.Background(), http.MethodGet, relURL("resource"), nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}

	code, err := c.DoRequest(req, nil)
	if err != nil {
		t.Errorf("DoRequest(nil resp) error: %v", err)
	}
	if code != http.StatusOK {
		t.Errorf("code = %d; want 200", code)
	}
}

// ---------------------------------------------------------------------------
// HTTPError.Error()
// ---------------------------------------------------------------------------

func TestHTTPError_MessagePresent(t *testing.T) {
	e := &rest.HTTPError{Code: 400, Message: "something went wrong"}
	if e.Error() != "something went wrong" {
		t.Errorf("Error() = %q; want %q", e.Error(), "something went wrong")
	}
}

func TestHTTPError_MessageEmpty_FallsBackToStatusText(t *testing.T) {
	e := &rest.HTTPError{Code: 404, Message: ""}
	got := e.Error()
	if !strings.Contains(got, "404") {
		t.Errorf("Error() = %q; expected to contain status code 404", got)
	}
}

func TestHTTPError_CodeZero_DefaultsTo500(t *testing.T) {
	e := &rest.HTTPError{Code: 0, Message: ""}
	got := e.Error()
	// After Error() is called, Code should have been set to 500.
	if e.Code != http.StatusInternalServerError {
		t.Errorf("Code after Error() = %d; want 500", e.Code)
	}
	if !strings.Contains(got, "500") {
		t.Errorf("Error() = %q; expected to contain '500'", got)
	}
}

// ---------------------------------------------------------------------------
// HTTPError.RequestID / Method / Path fields
// ---------------------------------------------------------------------------

func TestHTTPError_RequestIDAndPath_CapturedFrom4xx(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Request-Id", "req-abc-123")
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprintln(w, `{"message":"Unauthorized"}`)
	}))

	req, err := c.NewRequest(context.Background(), http.MethodGet, relURL("me"), nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	_, apiErr := c.DoRequest(req, nil)
	he, ok := apiErr.(*rest.HTTPError)
	if !ok {
		t.Fatalf("expected *rest.HTTPError, got %T", apiErr)
	}
	if he.RequestID != "req-abc-123" {
		t.Errorf("RequestID = %q; want %q", he.RequestID, "req-abc-123")
	}
	if he.Method != http.MethodGet {
		t.Errorf("Method = %q; want GET", he.Method)
	}
	if he.Path == "" {
		t.Errorf("Path should not be empty")
	}
}

// ---------------------------------------------------------------------------
// ActionableError
// ---------------------------------------------------------------------------

func TestActionableError_NilErrorReturnsEmpty(t *testing.T) {
	if got := rest.ActionableError("op", nil); got != "" {
		t.Errorf("ActionableError(nil) = %q; want empty string", got)
	}
}

func TestActionableError_NonHTTPError_NoHintAdded(t *testing.T) {
	err := fmt.Errorf("file not found")
	got := rest.ActionableError("load manifest", err)
	// For non-HTTP errors the output is "op: msg" without the debug hint.
	want := "load manifest: file not found"
	if got != want {
		t.Errorf("ActionableError = %q; want %q", got, want)
	}
	if strings.Contains(got, "--debug") {
		t.Errorf("non-HTTP error should not include --debug hint, got: %q", got)
	}
}

func TestActionableError_HTTPError_IncludesHint(t *testing.T) {
	err := &rest.HTTPError{Code: 401, Message: "Unauthorized", Method: "GET", Path: "/api/v2/me"}
	got := rest.ActionableError("list contexts", err)
	if !strings.Contains(got, "--debug") {
		t.Errorf("expected --debug hint in output, got: %q", got)
	}
	if !strings.Contains(got, rest.IssueURL) {
		t.Errorf("expected issue URL in output, got: %q", got)
	}
	if !strings.Contains(got, "list contexts") {
		t.Errorf("expected operation name in output, got: %q", got)
	}
}

func TestActionableError_HTTPError_IncludesRequestID(t *testing.T) {
	err := &rest.HTTPError{Code: 500, Message: "server error", RequestID: "rid-xyz", Method: "POST", Path: "/api/v2/context"}
	got := rest.ActionableError("create context", err)
	if !strings.Contains(got, "rid-xyz") {
		t.Errorf("expected request-id in output, got: %q", got)
	}
}

func TestActionableError_HTTPError_NoRequestID_NoIDInOutput(t *testing.T) {
	err := &rest.HTTPError{Code: 403, Message: "Forbidden", Method: "GET", Path: "/api/v2/org"}
	got := rest.ActionableError("resolve org", err)
	if strings.Contains(got, "request-id:  ") {
		t.Errorf("should not include empty request-id label, got: %q", got)
	}
	// Should still include the method/path context.
	if !strings.Contains(got, "GET") {
		t.Errorf("expected method in output, got: %q", got)
	}
}

// ---------------------------------------------------------------------------
// Debug logging of request/response (behavior, not output content)
// ---------------------------------------------------------------------------

func TestDoRequest_DebugLogging_DoesNotAffectResult(t *testing.T) {
	// Verify that enabling debug logging doesn't change the return value.
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, `{"id":"x","name":"y"}`)
	}))

	req, err := c.NewRequest(context.Background(), http.MethodGet, relURL("project/x"), nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	var out responseBody
	code, err := c.DoRequest(req, &out)
	if err != nil {
		t.Fatalf("DoRequest error: %v", err)
	}
	if code != http.StatusOK {
		t.Errorf("code = %d; want 200", code)
	}
}

// TestDoRequest_ContextCancellationAborts verifies that a cancelled context
// (e.g. the operator hitting Ctrl-C) aborts an in-flight request promptly with
// a context error, rather than blocking until the per-request timeout. The
// httptest server blocks until the client disconnects, so the only way the
// request returns is via context cancellation.
func TestDoRequest_ContextCancellationAborts(t *testing.T) {
	// Server that blocks until the request's context is done (i.e. the client
	// cancels). It never writes a response on its own.
	blocked := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		close(blocked)
		<-r.Context().Done()
	}))
	defer srv.Close()

	base := mustParseURL(t, srv.URL+"/")
	// Generous client timeout so the test fails (hangs → -timeout) if cancellation
	// is NOT respected, instead of passing for the wrong reason.
	c := rest.New(base, "tok", &http.Client{Timeout: 30 * time.Second})

	ctx, cancel := context.WithCancel(context.Background())

	req, err := c.NewRequest(ctx, http.MethodGet, relURL("anything"), nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}

	// Cancel as soon as the server has received the request.
	go func() {
		<-blocked
		cancel()
	}()

	done := make(chan error, 1)
	start := time.Now()
	go func() {
		_, derr := c.DoRequest(req, nil)
		done <- derr
	}()

	select {
	case derr := <-done:
		if derr == nil {
			t.Fatal("DoRequest returned nil error; want a context cancellation error")
		}
		// The error chain (or the request context) must reflect cancellation.
		if !errors.Is(derr, context.Canceled) && !errors.Is(ctx.Err(), context.Canceled) {
			t.Fatalf("DoRequest error = %v; want context.Canceled", derr)
		}
		if elapsed := time.Since(start); elapsed > 5*time.Second {
			t.Fatalf("DoRequest took %s to return after cancellation; want prompt return", elapsed)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("DoRequest did not return within 10s after context cancellation")
	}
}
