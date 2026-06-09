package project

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// ---- GetProjectOIDCClaims ---------------------------------------------------

func TestGetProjectOIDCClaims_HappyPath(t *testing.T) {
	const orgID = "org-uuid-111"
	const projID = "proj-uuid-222"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		wantPath := "/api/v2/org/" + orgID + "/project/" + projID + "/oidc-custom-claims"
		if r.URL.EscapedPath() != wantPath {
			t.Errorf("path: got %q want %q", r.URL.EscapedPath(), wantPath)
		}
		respondJSON(w, http.StatusOK, map[string]any{
			"org_id":     orgID,
			"project_id": projID,
			"audience":   []string{"https://example.com", "https://other.com"},
			"ttl":        "4h",
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	audience, ttl, err := c.GetProjectOIDCClaims(orgID, projID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(audience) != 2 || audience[0] != "https://example.com" || audience[1] != "https://other.com" {
		t.Errorf("audience: got %v", audience)
	}
	if ttl != "4h" {
		t.Errorf("ttl: got %q want %q", ttl, "4h")
	}
}

func TestGetProjectOIDCClaims_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusNotFound, map[string]string{"message": "not found"})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	_, _, err := c.GetProjectOIDCClaims("org-id", "proj-id")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ---- SetProjectOIDCClaims ---------------------------------------------------

func TestSetProjectOIDCClaims_HappyPath(t *testing.T) {
	const orgID = "org-uuid-111"
	const projID = "proj-uuid-222"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("expected PATCH, got %s", r.Method)
		}
		wantPath := "/api/v2/org/" + orgID + "/project/" + projID + "/oidc-custom-claims"
		if r.URL.EscapedPath() != wantPath {
			t.Errorf("path: got %q want %q", r.URL.EscapedPath(), wantPath)
		}

		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		aud, ok := body["audience"].([]any)
		if !ok || len(aud) != 1 || aud[0] != "https://example.com" {
			t.Errorf("body.audience: got %v", body["audience"])
		}
		if body["ttl"] != "2h" {
			t.Errorf("body.ttl: got %v want 2h", body["ttl"])
		}

		respondJSON(w, http.StatusOK, map[string]any{})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	err := c.SetProjectOIDCClaims(orgID, projID, []string{"https://example.com"}, "2h")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSetProjectOIDCClaims_EmptyBody_NoRequest(t *testing.T) {
	// When both audience and ttl are empty, no request should be made.
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		respondJSON(w, http.StatusOK, map[string]any{})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	err := c.SetProjectOIDCClaims("org", "proj", nil, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if called {
		t.Error("no request should be made when audience and ttl are both empty")
	}
}

func TestSetProjectOIDCClaims_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusForbidden, map[string]string{"message": "forbidden"})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	err := c.SetProjectOIDCClaims("org", "proj", []string{"aud"}, "1h")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestSetProjectOIDCClaims_PathEncoding(t *testing.T) {
	// Verify that orgID and projID with special characters are percent-encoded.
	const orgID = "org-uuid"
	const projID = "proj-uuid"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wantPath := "/api/v2/org/" + orgID + "/project/" + projID + "/oidc-custom-claims"
		if r.URL.EscapedPath() != wantPath {
			t.Errorf("path: got %q want %q", r.URL.EscapedPath(), wantPath)
		}
		respondJSON(w, http.StatusOK, map[string]any{})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	_ = c.SetProjectOIDCClaims(orgID, projID, []string{"aud"}, "")
}
