package org

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

const releaseTrackerOrgUUID = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"

// ─────────────────────────────────────────────────────────────────────────────
// GetReleaseTrackerSettings
// ─────────────────────────────────────────────────────────────────────────────

func TestGetReleaseTrackerSettings_WithTTL(t *testing.T) {
	wantPath := "/private/release-tracker/v1/organization/" + releaseTrackerOrgUUID + "/settings"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != wantPath {
			t.Errorf("expected path %q, got %q", wantPath, r.URL.Path)
		}
		if got := r.Header.Get("Circle-Token"); got != "test-token" {
			t.Errorf("Circle-Token header: got %q want test-token", got)
		}
		respondJSON(w, http.StatusOK, map[string]any{
			"inconclusive_release_ttl": "1h",
		})
	}))
	defer srv.Close()

	c := newTestClientWithAppServer(t, srv)
	settings, err := c.GetReleaseTrackerSettings(releaseTrackerOrgUUID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if settings == nil {
		t.Fatal("expected non-nil settings")
	}
	if settings.InconclusiveReleaseTTL != "1h" {
		t.Errorf("InconclusiveReleaseTTL: got %q want %q", settings.InconclusiveReleaseTTL, "1h")
	}
}

func TestGetReleaseTrackerSettings_Empty(t *testing.T) {
	// Server returns {} — no settings configured.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusOK, map[string]any{})
	}))
	defer srv.Close()

	c := newTestClientWithAppServer(t, srv)
	settings, err := c.GetReleaseTrackerSettings(releaseTrackerOrgUUID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if settings != nil {
		t.Errorf("expected nil settings for empty response, got %+v", settings)
	}
}

func TestGetReleaseTrackerSettings_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusForbidden, map[string]string{"message": "forbidden"})
	}))
	defer srv.Close()

	c := newTestClientWithAppServer(t, srv)
	if _, err := c.GetReleaseTrackerSettings(releaseTrackerOrgUUID); err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// SetReleaseTrackerSettings
// ─────────────────────────────────────────────────────────────────────────────

func TestSetReleaseTrackerSettings_HappyPath(t *testing.T) {
	wantPath := "/private/release-tracker/v1/organization/" + releaseTrackerOrgUUID + "/settings"
	var receivedBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("expected PATCH, got %s", r.Method)
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
	err := c.SetReleaseTrackerSettings(releaseTrackerOrgUUID, ReleaseTrackerSettings{
		InconclusiveReleaseTTL: "2h",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if receivedBody["inconclusive_release_ttl"] != "2h" {
		t.Errorf("inconclusive_release_ttl in body: got %v want %q", receivedBody["inconclusive_release_ttl"], "2h")
	}
}

func TestSetReleaseTrackerSettings_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusBadRequest, map[string]string{"message": "invalid request"})
	}))
	defer srv.Close()

	c := newTestClientWithAppServer(t, srv)
	err := c.SetReleaseTrackerSettings(releaseTrackerOrgUUID, ReleaseTrackerSettings{InconclusiveReleaseTTL: "1h"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}
