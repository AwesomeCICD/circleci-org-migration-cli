package project

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// CreateProjectToken validates its inputs before any HTTP call.
func TestCreateProjectToken_Validation(t *testing.T) {
	c := newTestClient(t, httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("no HTTP call expected for validation errors")
	})))
	cases := []struct{ slug, scope, label, want string }{
		{"", "all", "l", "slug is required"},
		{"gh/acme/web", "", "l", "scope is required"},
		{"gh/acme/web", "all", "", "label is required"},
	}
	for _, tc := range cases {
		if _, err := c.CreateProjectToken(tc.slug, tc.scope, tc.label); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("CreateProjectToken(%q,%q,%q): want error %q, got %v", tc.slug, tc.scope, tc.label, tc.want, err)
		}
	}
}

// ListProjectTokens returns (nil, nil) when the project has no tokens.
func TestListProjectTokens_Empty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusOK, []map[string]any{})
	}))
	defer srv.Close()
	tokens, err := newTestClient(t, srv).ListProjectTokens("gh/acme/web")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tokens) != 0 {
		t.Fatalf("want no tokens, got %d", len(tokens))
	}
}

// Both calls surface a non-2xx server response as an error.
func TestProjectTokens_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	c := newTestClient(t, srv)
	if _, err := c.ListProjectTokens("gh/acme/web"); err == nil {
		t.Error("ListProjectTokens: expected error on 500")
	}
	if _, err := c.CreateProjectToken("gh/acme/web", "all", "l"); err == nil {
		t.Error("CreateProjectToken: expected error on 500")
	}
}
