package org

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// ─────────────────────────────────────────────────────────────────────────────
// GetSSOEnforced
// ─────────────────────────────────────────────────────────────────────────────

func TestGetSSOEnforced(t *testing.T) {
	const orgID = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"

	cases := []struct {
		name     string
		enforced bool
	}{
		{"enforced true", true},
		{"enforced false", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Errorf("expected GET, got %s", r.Method)
				}
				wantPath := "/private/ciam/orgs/" + orgID + "/sso/enforced"
				if r.URL.Path != wantPath {
					t.Errorf("expected path %q, got %q", wantPath, r.URL.Path)
				}
				if got := r.Header.Get("Circle-Token"); got != "test-token" {
					t.Errorf("Circle-Token header: got %q want %q", got, "test-token")
				}
				respondJSON(w, http.StatusOK, map[string]any{
					"id":           orgID,
					"vcs_type":     "github",
					"name":         "acme",
					"display_name": "Acme",
					"enforced":     tc.enforced,
				})
			}))
			defer srv.Close()

			c := newTestClientWithAppServer(t, srv)
			got, err := c.GetSSOEnforced(context.Background(), orgID)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.enforced {
				t.Errorf("GetSSOEnforced = %v, want %v", got, tc.enforced)
			}
		})
	}
}

func TestGetSSOEnforced_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusForbidden, map[string]string{"message": "forbidden"})
	}))
	defer srv.Close()

	c := newTestClientWithAppServer(t, srv)
	if _, err := c.GetSSOEnforced(context.Background(), "some-org"); err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// GetSSOConnection
// ─────────────────────────────────────────────────────────────────────────────

func TestGetSSOConnection_Found(t *testing.T) {
	const orgID = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		wantPath := "/private/ciam/orgs/" + orgID + "/sso/connection"
		if r.URL.Path != wantPath {
			t.Errorf("expected path %q, got %q", wantPath, r.URL.Path)
		}
		respondJSON(w, http.StatusOK, map[string]any{
			"realm":          "acme-saml",
			"idp_entity_id":  "https://idp.example.com/entity",
			"idp_sso_target": "https://idp.example.com/sso",
		})
	}))
	defer srv.Close()

	c := newTestClientWithAppServer(t, srv)
	conn, found, err := c.GetSSOConnection(context.Background(), orgID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !found {
		t.Fatal("expected found=true")
	}
	if conn["realm"] != "acme-saml" {
		t.Errorf("realm: got %v want %q", conn["realm"], "acme-saml")
	}
	if conn["idp_entity_id"] != "https://idp.example.com/entity" {
		t.Errorf("idp_entity_id: got %v", conn["idp_entity_id"])
	}
}

func TestGetSSOConnection_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusNotFound, map[string]string{"message": "connection not found"})
	}))
	defer srv.Close()

	c := newTestClientWithAppServer(t, srv)
	conn, found, err := c.GetSSOConnection(context.Background(), "some-org")
	if err != nil {
		t.Fatalf("404 must not be an error, got: %v", err)
	}
	if found {
		t.Error("expected found=false on 404")
	}
	if conn != nil {
		t.Errorf("expected nil connection on 404, got %v", conn)
	}
}

func TestGetSSOConnection_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"message": "boom"})
	}))
	defer srv.Close()

	c := newTestClientWithAppServer(t, srv)
	_, found, err := c.GetSSOConnection(context.Background(), "some-org")
	if err == nil {
		t.Fatal("expected error for non-404 failure, got nil")
	}
	if found {
		t.Error("expected found=false on error")
	}
}
