package runner_test

// extra_test.go adds error-path and edge-case tests for the runner package
// to raise coverage above 80%.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AwesomeCICD/circleci-org-migration-cli/api/runner"
	"github.com/AwesomeCICD/circleci-org-migration-cli/settings"
)

// ---------------------------------------------------------------------------
// newClientWithHTTP error paths
// ---------------------------------------------------------------------------

func TestNewClientWithBase_EmptyBaseHost(t *testing.T) {
	cfg := &settings.Config{HTTPClient: &http.Client{}}
	_, err := runner.NewClientWithBase("", "tok", cfg)
	if err == nil {
		t.Fatal("expected error for empty base host, got nil")
	}
}

func TestNewClientWithBase_NoSchemeNoHost(t *testing.T) {
	// A URL with no scheme and no host should error.
	cfg := &settings.Config{HTTPClient: &http.Client{}}
	_, err := runner.NewClientWithBase("not-a-url", "tok", cfg)
	if err == nil {
		t.Fatal("expected error for host-less URL, got nil")
	}
}

func TestNewClientWithBase_NilConfig_UsesDefaultHTTPClient(t *testing.T) {
	// cfg=nil should use a default http.Client (no panic).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{}})
	}))
	defer srv.Close()

	c, err := runner.NewClientWithBase(srv.URL, "tok", nil)
	if err != nil {
		t.Fatalf("unexpected error with nil cfg: %v", err)
	}
	if c == nil {
		t.Fatal("expected non-nil client")
	}
}

// ---------------------------------------------------------------------------
// GetResourceClassesByNamespace — error paths
// ---------------------------------------------------------------------------

func TestGetResourceClassesByNamespace_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"internal error"}`, http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	_, err := c.GetResourceClassesByNamespace("broken-ns")
	if err == nil {
		t.Fatal("expected error for server error, got nil")
	}
}

func TestGetResourceClassesByNamespace_Unauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"unauthorized"}`, http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	_, err := c.GetResourceClassesByNamespace("ns")
	if err == nil {
		t.Fatal("expected error for unauthorized, got nil")
	}
}

// ---------------------------------------------------------------------------
// CreateResourceClass — error paths
// ---------------------------------------------------------------------------

func TestCreateResourceClass_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"conflict"}`, http.StatusConflict)
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	_, err := c.CreateResourceClass("acme/dup-runner", "duplicate")
	if err == nil {
		t.Fatal("expected error for conflict, got nil")
	}
}

func TestCreateResourceClass_EmptyDescription(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		// description should be absent (omitempty) when empty.
		if _, ok := body["description"]; ok {
			t.Errorf("description should be omitted when empty, got: %v", body["description"])
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":             "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
			"resource_class": "acme/no-desc",
			"description":    "",
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	rc, err := c.CreateResourceClass("acme/no-desc", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rc.ResourceClass != "acme/no-desc" {
		t.Errorf("ResourceClass = %q", rc.ResourceClass)
	}
}

// ---------------------------------------------------------------------------
// DeleteResourceClass — error paths
// ---------------------------------------------------------------------------

func TestDeleteResourceClass_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"not found"}`, http.StatusNotFound)
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	err := c.DeleteResourceClass("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")
	if err == nil {
		t.Fatal("expected error for not found, got nil")
	}
}

// ---------------------------------------------------------------------------
// NewClient (production constructor)
// ---------------------------------------------------------------------------

func TestNewClient_UsesDefaultRunnerHost(t *testing.T) {
	// NewClient always uses runner.circleci.com; with a fake token it will
	// succeed construction but fail on actual API calls. Just verify it builds.
	cfg := &settings.Config{HTTPClient: &http.Client{}}
	c, err := runner.NewClient(cfg, "fake-token-for-test")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if c == nil {
		t.Fatal("expected non-nil client")
	}
}

// ---------------------------------------------------------------------------
// GetResourceClassesByNamespace — namespace query parameter
// ---------------------------------------------------------------------------

func TestGetResourceClassesByNamespace_NamespaceQueryParam(t *testing.T) {
	const wantNS = "my-namespace"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ns := r.URL.Query().Get("namespace"); ns != wantNS {
			t.Errorf("expected namespace=%q, got %q", wantNS, ns)
		}
		if !strings.HasSuffix(r.URL.Path, "/runner/resource") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{}})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	items, err := c.GetResourceClassesByNamespace(wantNS)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("expected 0 items, got %d", len(items))
	}
}
