package org

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

const envHierarchyOrgUUID = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"

// ─────────────────────────────────────────────────────────────────────────────
// GetEnvironmentHierarchy
// ─────────────────────────────────────────────────────────────────────────────

func TestGetEnvironmentHierarchy_WithHierarchy(t *testing.T) {
	wantPath := "/private/release-tracker/v1/environment-hierarchy/resolve"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != wantPath {
			t.Errorf("expected path %q, got %q", wantPath, r.URL.Path)
		}
		if got := r.URL.Query().Get("org-id"); got != envHierarchyOrgUUID {
			t.Errorf("org-id query param: got %q want %q", got, envHierarchyOrgUUID)
		}
		if got := r.Header.Get("Circle-Token"); got != "test-token" {
			t.Errorf("Circle-Token header: got %q want test-token", got)
		}
		respondJSON(w, http.StatusOK, map[string]any{
			"resolved_scope": "ORGANIZATION",
			"hierarchy": map[string]any{
				"name":        "prod-hierarchy",
				"description": "Production environment hierarchy",
				"levels": []map[string]any{
					{
						"position":         1,
						"integration_id":   "uuid-should-be-omitted",
						"integration_name": "orbs-dev",
					},
					{
						"position":         2,
						"integration_id":   "uuid-also-omitted",
						"integration_name": "prod-integration",
					},
				},
			},
		})
	}))
	defer srv.Close()

	c := newTestClientWithAppServer(t, srv)
	h, err := c.GetEnvironmentHierarchy(envHierarchyOrgUUID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if h == nil {
		t.Fatal("expected non-nil hierarchy")
	}
	if h.Name != "prod-hierarchy" {
		t.Errorf("Name: got %q want %q", h.Name, "prod-hierarchy")
	}
	if h.Description != "Production environment hierarchy" {
		t.Errorf("Description: got %q want %q", h.Description, "Production environment hierarchy")
	}
	if len(h.Levels) != 2 {
		t.Fatalf("expected 2 levels, got %d", len(h.Levels))
	}
	if h.Levels[0].Position != 1 || h.Levels[0].IntegrationName != "orbs-dev" {
		t.Errorf("Level[0]: got %+v", h.Levels[0])
	}
	if h.Levels[1].Position != 2 || h.Levels[1].IntegrationName != "prod-integration" {
		t.Errorf("Level[1]: got %+v", h.Levels[1])
	}
}

func TestGetEnvironmentHierarchy_ScopeNone(t *testing.T) {
	// Server returns NONE scope with null hierarchy — should return nil.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusOK, map[string]any{
			"resolved_scope": "NONE",
			"hierarchy":      nil,
		})
	}))
	defer srv.Close()

	c := newTestClientWithAppServer(t, srv)
	h, err := c.GetEnvironmentHierarchy(envHierarchyOrgUUID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if h != nil {
		t.Errorf("expected nil hierarchy for NONE scope, got %+v", h)
	}
}

func TestGetEnvironmentHierarchy_NullHierarchy(t *testing.T) {
	// Hierarchy field absent/null even if scope is not NONE.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusOK, map[string]any{
			"resolved_scope": "ORGANIZATION",
			"hierarchy":      nil,
		})
	}))
	defer srv.Close()

	c := newTestClientWithAppServer(t, srv)
	h, err := c.GetEnvironmentHierarchy(envHierarchyOrgUUID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if h != nil {
		t.Errorf("expected nil hierarchy when hierarchy is null, got %+v", h)
	}
}

func TestGetEnvironmentHierarchy_IntegrationIDNotExposed(t *testing.T) {
	// Confirm that integration_id from the API is NOT present in the captured struct.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusOK, map[string]any{
			"resolved_scope": "ORGANIZATION",
			"hierarchy": map[string]any{
				"name": "test",
				"levels": []map[string]any{
					{"position": 1, "integration_id": "secret-uuid", "integration_name": "my-integration"},
				},
			},
		})
	}))
	defer srv.Close()

	c := newTestClientWithAppServer(t, srv)
	h, err := c.GetEnvironmentHierarchy(envHierarchyOrgUUID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if h == nil || len(h.Levels) != 1 {
		t.Fatal("expected 1 level")
	}
	// The EnvHierarchyLevel struct has no IntegrationID field — the type system
	// enforces the omission. Verify integration_name was captured correctly.
	if h.Levels[0].IntegrationName != "my-integration" {
		t.Errorf("IntegrationName: got %q want %q", h.Levels[0].IntegrationName, "my-integration")
	}
}

func TestGetEnvironmentHierarchy_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusForbidden, map[string]string{"message": "forbidden"})
	}))
	defer srv.Close()

	c := newTestClientWithAppServer(t, srv)
	if _, err := c.GetEnvironmentHierarchy(envHierarchyOrgUUID); err == nil {
		t.Fatal("expected error, got nil")
	}
}
