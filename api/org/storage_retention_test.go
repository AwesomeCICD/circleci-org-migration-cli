package org

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

const testOrgUUID = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"

// ─────────────────────────────────────────────────────────────────────────────
// GetStorageRetention
// ─────────────────────────────────────────────────────────────────────────────

func TestGetStorageRetention_HappyPath(t *testing.T) {
	wantPath := "/private/orgs/" + testOrgUUID + "/storage-retention-controls"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != wantPath {
			t.Errorf("expected path %q, got %q", wantPath, r.URL.Path)
		}
		respondJSON(w, http.StatusOK, map[string]any{
			"storage_retention_controls": map[string]any{
				"retention_days_cache":     15,
				"retention_days_artifact":  30,
				"retention_days_workspace": 15,
			},
			"storage_retention_limits": map[string]any{
				"retention_days_cache":     map[string]any{"min": 1, "max": 15},
				"retention_days_workspace": map[string]any{"min": 1, "max": 15},
				"retention_days_artifact":  map[string]any{"min": 1, "max": 30},
			},
		})
	}))
	defer srv.Close()

	c := newTestClientWithAppServer(t, srv)
	got, err := c.GetStorageRetention(testOrgUUID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Controls.CacheDays != 15 {
		t.Errorf("CacheDays: got %d want 15", got.Controls.CacheDays)
	}
	if got.Controls.ArtifactDays != 30 {
		t.Errorf("ArtifactDays: got %d want 30", got.Controls.ArtifactDays)
	}
	if got.Controls.WorkspaceDays != 15 {
		t.Errorf("WorkspaceDays: got %d want 15", got.Controls.WorkspaceDays)
	}
	if got.Limits.Artifact.Min != 1 || got.Limits.Artifact.Max != 30 {
		t.Errorf("Limits.Artifact: got min=%d max=%d want min=1 max=30",
			got.Limits.Artifact.Min, got.Limits.Artifact.Max)
	}
}

func TestGetStorageRetention_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusForbidden, map[string]string{"message": "forbidden"})
	}))
	defer srv.Close()

	c := newTestClientWithAppServer(t, srv)
	_, err := c.GetStorageRetention(testOrgUUID)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// SetStorageRetention
// ─────────────────────────────────────────────────────────────────────────────

func TestSetStorageRetention_HappyPath(t *testing.T) {
	wantPath := "/private/orgs/" + testOrgUUID + "/storage-retention-controls"
	var receivedBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("expected PUT, got %s", r.Method)
		}
		if r.URL.Path != wantPath {
			t.Errorf("expected path %q, got %q", wantPath, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&receivedBody); err != nil {
			t.Errorf("decode body: %v", err)
		}
		respondJSON(w, http.StatusOK, map[string]any{})
	}))
	defer srv.Close()

	c := newTestClientWithAppServer(t, srv)
	err := c.SetStorageRetention(testOrgUUID, StorageRetentionControls{
		CacheDays:     10,
		WorkspaceDays: 7,
		ArtifactDays:  1,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// JSON key names must be the snake_case retention_days_* names.
	if v, ok := receivedBody["retention_days_artifact"]; !ok || v != float64(1) {
		t.Errorf("retention_days_artifact: got %v (ok=%v), want 1", v, ok)
	}
	if v, ok := receivedBody["retention_days_cache"]; !ok || v != float64(10) {
		t.Errorf("retention_days_cache: got %v (ok=%v), want 10", v, ok)
	}
	if v, ok := receivedBody["retention_days_workspace"]; !ok || v != float64(7) {
		t.Errorf("retention_days_workspace: got %v (ok=%v), want 7", v, ok)
	}
}

func TestSetStorageRetention_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusBadRequest, map[string]string{"message": "invalid"})
	}))
	defer srv.Close()

	c := newTestClientWithAppServer(t, srv)
	err := c.SetStorageRetention(testOrgUUID, StorageRetentionControls{ArtifactDays: 1})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}
