package project

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// ---------------------------------------------------------------------------
// ListProjectTokens
// ---------------------------------------------------------------------------

// TestListProjectTokens_HappyPath verifies that the client parses the token
// array from the v1.1 response into ProjectAPIToken values (no value field).
func TestListProjectTokens_HappyPath(t *testing.T) {
	const slug = "gh/acme/web"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		wantPath := "/api/v1.1/project/gh/acme/web/token"
		if r.URL.EscapedPath() != wantPath {
			t.Errorf("path: got %q want %q", r.URL.EscapedPath(), wantPath)
		}
		// Return two tokens; the "token" value field is intentionally absent
		// (always null on the list endpoint).
		respondJSON(w, http.StatusOK, []map[string]any{
			{"id": "tok-id-1", "label": "deploy-bot", "scope": "all"},
			{"id": "tok-id-2", "label": "status-check", "scope": "status"},
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	tokens, err := c.ListProjectTokens(context.Background(), slug)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tokens) != 2 {
		t.Fatalf("expected 2 tokens, got %d", len(tokens))
	}

	t0 := tokens[0]
	if t0.ID != "tok-id-1" {
		t.Errorf("ID[0]: got %q, want tok-id-1", t0.ID)
	}
	if t0.Label != "deploy-bot" {
		t.Errorf("Label[0]: got %q, want deploy-bot", t0.Label)
	}
	if t0.Scope != "all" {
		t.Errorf("Scope[0]: got %q, want all", t0.Scope)
	}

	t1 := tokens[1]
	if t1.Label != "status-check" {
		t.Errorf("Label[1]: got %q, want status-check", t1.Label)
	}
	if t1.Scope != "status" {
		t.Errorf("Scope[1]: got %q, want status", t1.Scope)
	}
}

// TestListProjectTokens_EmptyList verifies that an empty array returns nil.
func TestListProjectTokens_EmptyList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusOK, []any{})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	tokens, err := c.ListProjectTokens(context.Background(), "gh/acme/web")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tokens) != 0 {
		t.Errorf("expected 0 tokens, got %d", len(tokens))
	}
}

// TestListProjectTokens_StandaloneSlug verifies the circleci/<uuid>/<uuid> slug
// form is correctly encoded in the request path.
func TestListProjectTokens_StandaloneSlug(t *testing.T) {
	const orgUUID = "org-uuid-abc"
	const projUUID = "proj-uuid-def"
	slug := "circleci/" + orgUUID + "/" + projUUID

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wantPath := "/api/v1.1/project/circleci/" + orgUUID + "/" + projUUID + "/token"
		if r.URL.EscapedPath() != wantPath {
			t.Errorf("path: got %q want %q", r.URL.EscapedPath(), wantPath)
		}
		respondJSON(w, http.StatusOK, []map[string]any{
			{"id": "tok-standalone", "label": "ci-read", "scope": "view-builds"},
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	tokens, err := c.ListProjectTokens(context.Background(), slug)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tokens) != 1 {
		t.Fatalf("expected 1 token, got %d", len(tokens))
	}
	if tokens[0].Scope != "view-builds" {
		t.Errorf("Scope: got %q, want view-builds", tokens[0].Scope)
	}
}

// TestListProjectTokens_APIError verifies that a non-2xx response returns an error.
func TestListProjectTokens_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusForbidden, map[string]string{"message": "forbidden"})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	_, err := c.ListProjectTokens(context.Background(), "gh/acme/web")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ---------------------------------------------------------------------------
// CreateProjectToken
// ---------------------------------------------------------------------------

// TestCreateProjectToken_HappyPath verifies that the client POSTs the correct
// body and returns the plaintext token from the 201 response.
func TestCreateProjectToken_HappyPath(t *testing.T) {
	const slug = "gh/acme/web"

	var gotBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		wantPath := "/api/v1.1/project/gh/acme/web/token"
		if r.URL.EscapedPath() != wantPath {
			t.Errorf("path: got %q want %q", r.URL.EscapedPath(), wantPath)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		respondJSON(w, http.StatusCreated, map[string]any{
			"id":    "new-tok-id",
			"label": "deploy-bot",
			"scope": "all",
			"token": "ccipat_PLACEHOLDER_new_token_value",
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	tok, err := c.CreateProjectToken(context.Background(), slug, "all", "deploy-bot")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tok != "ccipat_PLACEHOLDER_new_token_value" {
		t.Errorf("token value: got %q", tok)
	}
	if gotBody["scope"] != "all" {
		t.Errorf("body.scope: got %q, want all", gotBody["scope"])
	}
	if gotBody["label"] != "deploy-bot" {
		t.Errorf("body.label: got %q, want deploy-bot", gotBody["label"])
	}
}

// TestCreateProjectToken_EmptySlug verifies that an empty slug returns an error
// without making an HTTP request.
func TestCreateProjectToken_EmptySlug(t *testing.T) {
	c := &Client{}
	_, err := c.CreateProjectToken(context.Background(), "", "all", "label")
	if err == nil {
		t.Fatal("expected error for empty slug, got nil")
	}
}

// TestCreateProjectToken_EmptyScope verifies that an empty scope returns an error.
func TestCreateProjectToken_EmptyScope(t *testing.T) {
	c := &Client{}
	_, err := c.CreateProjectToken(context.Background(), "gh/acme/web", "", "label")
	if err == nil {
		t.Fatal("expected error for empty scope, got nil")
	}
}

// TestCreateProjectToken_EmptyLabel verifies that an empty label returns an error.
func TestCreateProjectToken_EmptyLabel(t *testing.T) {
	c := &Client{}
	_, err := c.CreateProjectToken(context.Background(), "gh/acme/web", "all", "")
	if err == nil {
		t.Fatal("expected error for empty label, got nil")
	}
}

// TestCreateProjectToken_APIError verifies that a non-2xx response returns an error.
func TestCreateProjectToken_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusForbidden, map[string]string{"message": "forbidden"})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	_, err := c.CreateProjectToken(context.Background(), "gh/acme/web", "all", "deploy-bot")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// TestCreateProjectToken_StandaloneSlug verifies the circleci/<uuid>/<uuid> form.
func TestCreateProjectToken_StandaloneSlug(t *testing.T) {
	const orgUUID = "org-uuid-abc"
	const projUUID = "proj-uuid-def"
	slug := "circleci/" + orgUUID + "/" + projUUID

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wantPath := "/api/v1.1/project/circleci/" + orgUUID + "/" + projUUID + "/token"
		if r.URL.EscapedPath() != wantPath {
			t.Errorf("path: got %q want %q", r.URL.EscapedPath(), wantPath)
		}
		respondJSON(w, http.StatusCreated, map[string]any{
			"id":    "new-tok-standalone",
			"label": "ci-status",
			"scope": "status",
			"token": "ccipat_PLACEHOLDER_standalone_value",
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	tok, err := c.CreateProjectToken(context.Background(), slug, "status", "ci-status")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tok != "ccipat_PLACEHOLDER_standalone_value" {
		t.Errorf("token value: got %q", tok)
	}
}
