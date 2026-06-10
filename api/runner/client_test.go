package runner_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AwesomeCICD/circleci-org-migration-cli/api/runner"
	"github.com/AwesomeCICD/circleci-org-migration-cli/settings"
)

// newTestClient builds a runner.Client pointed at srv, with the API base
// resolved to srv.URL/api/v3/ — matching the runner client's URL layout.
func newTestClient(t *testing.T, srv *httptest.Server) *runner.Client {
	t.Helper()
	cfg := &settings.Config{HTTPClient: srv.Client()}
	c, err := runner.NewClientWithBase(srv.URL, "fake-token-value", cfg)
	if err != nil {
		t.Fatalf("NewClientWithBase: %v", err)
	}
	return c
}

func TestGetResourceClassesByNamespace(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/runner/resource") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		ns := r.URL.Query().Get("namespace")
		if ns != "acme" {
			t.Errorf("expected namespace=acme, got %q", ns)
		}
		// Verify auth header is present (value not logged per security policy).
		if r.Header.Get("Circle-Token") == "" {
			t.Error("Circle-Token header missing")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []map[string]any{
				{
					"id":             "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
					"resource_class": "acme/my-runner",
					"description":    "my runner class",
					// These fields exist in live responses but must be ignored.
					"active_tasks": 2,
					"runners":      []map[string]any{{"id": "runner-uuid"}},
				},
			},
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	items, err := c.GetResourceClassesByNamespace("acme")
	if err != nil {
		t.Fatalf("GetResourceClassesByNamespace: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	rc := items[0]
	if rc.ID != "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee" {
		t.Errorf("ID = %q", rc.ID)
	}
	if rc.ResourceClass != "acme/my-runner" {
		t.Errorf("ResourceClass = %q", rc.ResourceClass)
	}
	if rc.Description != "my runner class" {
		t.Errorf("Description = %q", rc.Description)
	}
}

func TestGetResourceClassesByNamespace_Empty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{}})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	items, err := c.GetResourceClassesByNamespace("empty-ns")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("expected 0 items, got %d", len(items))
	}
}

func TestCreateResourceClass(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/runner/resource") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["resource_class"] != "acme/new-runner" {
			t.Errorf("resource_class = %v", body["resource_class"])
		}
		if body["description"] != "brand new runner" {
			t.Errorf("description = %v", body["description"])
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":             "11111111-2222-3333-4444-555555555555",
			"resource_class": "acme/new-runner",
			"description":    "brand new runner",
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	rc, err := c.CreateResourceClass("acme/new-runner", "brand new runner")
	if err != nil {
		t.Fatalf("CreateResourceClass: %v", err)
	}
	if rc.ID != "11111111-2222-3333-4444-555555555555" {
		t.Errorf("ID = %q", rc.ID)
	}
	if rc.ResourceClass != "acme/new-runner" {
		t.Errorf("ResourceClass = %q", rc.ResourceClass)
	}
}

func TestDeleteResourceClass(t *testing.T) {
	deletedID := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		parts := strings.Split(r.URL.Path, "/")
		deletedID = parts[len(parts)-1]
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{}"))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	err := c.DeleteResourceClass("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")
	if err != nil {
		t.Fatalf("DeleteResourceClass: %v", err)
	}
	if deletedID != "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee" {
		t.Errorf("deleted ID = %q", deletedID)
	}
}

func TestNewClient_InvalidHost(t *testing.T) {
	cfg := &settings.Config{HTTPClient: &http.Client{}}
	_, err := runner.NewClientWithBase("://bad-url", "tok", cfg)
	if err == nil {
		t.Fatal("expected error for invalid host, got nil")
	}
}
