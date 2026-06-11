package org

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

const blockUsersOrgUUID = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"

// ─────────────────────────────────────────────────────────────────────────────
// GetBlockUnregisteredUsers
// ─────────────────────────────────────────────────────────────────────────────

func TestGetBlockUnregisteredUsers_Enabled(t *testing.T) {
	wantPath := "/private/orgs/" + blockUsersOrgUUID + "/features/block-unregistered-users"

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
		respondJSON(w, http.StatusOK, map[string]any{"enabled": true})
	}))
	defer srv.Close()

	c := newTestClientWithAppServer(t, srv)
	enabled, err := c.GetBlockUnregisteredUsers(context.Background(), blockUsersOrgUUID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !enabled {
		t.Error("expected enabled=true")
	}
}

func TestGetBlockUnregisteredUsers_Disabled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusOK, map[string]any{"enabled": false})
	}))
	defer srv.Close()

	c := newTestClientWithAppServer(t, srv)
	enabled, err := c.GetBlockUnregisteredUsers(context.Background(), blockUsersOrgUUID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if enabled {
		t.Error("expected enabled=false")
	}
}

func TestGetBlockUnregisteredUsers_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusForbidden, map[string]string{"message": "forbidden"})
	}))
	defer srv.Close()

	c := newTestClientWithAppServer(t, srv)
	if _, err := c.GetBlockUnregisteredUsers(context.Background(), blockUsersOrgUUID); err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// SetBlockUnregisteredUsers
// ─────────────────────────────────────────────────────────────────────────────

func TestSetBlockUnregisteredUsers_Enable(t *testing.T) {
	wantPath := "/private/orgs/" + blockUsersOrgUUID + "/features/block-unregistered-users"
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
	if err := c.SetBlockUnregisteredUsers(context.Background(), blockUsersOrgUUID, true); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if receivedBody["enabled"] != true {
		t.Errorf("enabled in body: got %v want true", receivedBody["enabled"])
	}
}

func TestSetBlockUnregisteredUsers_Disable(t *testing.T) {
	var receivedBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("expected PUT, got %s", r.Method)
		}
		if err := json.NewDecoder(r.Body).Decode(&receivedBody); err != nil {
			t.Errorf("decode body: %v", err)
		}
		respondJSON(w, http.StatusOK, map[string]any{})
	}))
	defer srv.Close()

	c := newTestClientWithAppServer(t, srv)
	if err := c.SetBlockUnregisteredUsers(context.Background(), blockUsersOrgUUID, false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if receivedBody["enabled"] != false {
		t.Errorf("enabled in body: got %v want false", receivedBody["enabled"])
	}
}

// TestSetBlockUnregisteredUsers_EmptyContentType reproduces #166: the feature
// PUT can return a successful status with an empty body and no Content-Type
// header. This must be treated as success, not an "unexpected content-type"
// error.
func TestSetBlockUnregisteredUsers_EmptyContentType(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("expected PUT, got %s", r.Method)
		}
		// Deliberately do not set a Content-Type header and write no body.
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := newTestClientWithAppServer(t, srv)
	if err := c.SetBlockUnregisteredUsers(context.Background(), blockUsersOrgUUID, true); err != nil {
		t.Fatalf("unexpected error for 200 with empty content-type/body: %v", err)
	}
}

func TestSetBlockUnregisteredUsers_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusBadRequest, map[string]string{"message": "invalid request"})
	}))
	defer srv.Close()

	c := newTestClientWithAppServer(t, srv)
	if err := c.SetBlockUnregisteredUsers(context.Background(), blockUsersOrgUUID, true); err == nil {
		t.Fatal("expected error, got nil")
	}
}
