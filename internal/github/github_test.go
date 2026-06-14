package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestResolveRepoID_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/repos/acme/web" {
			t.Errorf("path = %q, want /repos/acme/web", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer my-token" {
			t.Errorf("Authorization: got %q, want %q", got, "Bearer my-token")
		}
		if got := r.Header.Get("Accept"); got != "application/vnd.github+json" {
			t.Errorf("Accept: got %q, want application/vnd.github+json", got)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":12345678,"name":"web","full_name":"acme/web"}`))
	}))
	defer srv.Close()

	id, err := ResolveRepoID(context.Background(), "acme/web", "my-token", srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "12345678" {
		t.Errorf("id: got %q, want 12345678", id)
	}
}

func TestResolveRepoID_NoToken(t *testing.T) {
	// When token is empty, the Authorization header must NOT be sent.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Errorf("Authorization header must not be set when token is empty, got %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":99}`))
	}))
	defer srv.Close()

	id, err := ResolveRepoID(context.Background(), "acme/web", "", srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "99" {
		t.Errorf("id: got %q, want 99", id)
	}
}

func TestResolveRepoID_DefaultBaseURL(t *testing.T) {
	// When baseURL is empty, DefaultBaseURL is used. We can only verify no
	// error is returned for validation; we can't intercept the real GitHub call,
	// so just verify the constant is set correctly.
	if DefaultBaseURL != "https://api.github.com" {
		t.Errorf("DefaultBaseURL: got %q, want https://api.github.com", DefaultBaseURL)
	}
}

func TestResolveRepoID_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"Not Found"}`))
	}))
	defer srv.Close()

	_, err := ResolveRepoID(context.Background(), "acme/missing", "token", srv.URL)
	if err == nil {
		t.Fatal("expected error for 404, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error %q should mention 'not found'", err.Error())
	}
	// 404 must wrap ErrRepoNotFound so callers can distinguish it.
	if !errors.Is(err, ErrRepoNotFound) {
		t.Errorf("errors.Is(err, ErrRepoNotFound) = false; 404 must wrap ErrRepoNotFound")
	}
}

// TestResolveRepoID_NonNotFoundDoesNotWrapErrRepoNotFound verifies that non-404
// errors do NOT satisfy errors.Is(err, ErrRepoNotFound).
func TestResolveRepoID_NonNotFoundDoesNotWrapErrRepoNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	_, err := ResolveRepoID(context.Background(), "acme/web", "token", srv.URL)
	if err == nil {
		t.Fatal("expected error for 500, got nil")
	}
	if errors.Is(err, ErrRepoNotFound) {
		t.Error("500 error must NOT wrap ErrRepoNotFound")
	}
}

func TestResolveRepoID_NonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	_, err := ResolveRepoID(context.Background(), "acme/web", "token", srv.URL)
	if err == nil {
		t.Fatal("expected error for 500, got nil")
	}
}

func TestResolveRepoID_EmptyFullName(t *testing.T) {
	if _, err := ResolveRepoID(context.Background(), "", "token", ""); err == nil {
		t.Error("expected error for empty fullName, got nil")
	}
}

func TestResolveRepoID_InvalidFullName(t *testing.T) {
	cases := []string{"noslash", "/noowner", "owner/"}
	for _, name := range cases {
		if _, err := ResolveRepoID(context.Background(), name, "token", ""); err == nil {
			t.Errorf("ResolveRepoID(context.Background(), %q): expected error, got nil", name)
		}
	}
}

// ---------------------------------------------------------------------------
// ListOrgRepos tests
// ---------------------------------------------------------------------------

func TestListOrgRepos_SinglePage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/orgs/acme/repos" {
			t.Errorf("path = %q, want /orgs/acme/repos", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer my-token" {
			t.Errorf("Authorization: got %q, want %q", got, "Bearer my-token")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[{"name":"web","archived":false},{"name":"api","archived":false}]`))
	}))
	defer srv.Close()

	repos, err := ListOrgRepos(context.Background(), "acme", "my-token", srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(repos) != 2 {
		t.Fatalf("expected 2 repos, got %d", len(repos))
	}
	if repos[0].Name != "web" || repos[1].Name != "api" {
		t.Errorf("repos: got %v", repos)
	}
}

func TestListOrgRepos_Pagination(t *testing.T) {
	page := 0
	var srvURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page++
		w.Header().Set("Content-Type", "application/json")
		if page == 1 {
			// Return link header pointing to page 2.
			w.Header().Set("Link", fmt.Sprintf(`<%s/orgs/acme/repos?page=2>; rel="next"`, srvURL))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[{"name":"repo1"}]`))
		} else {
			// Last page: no Link header.
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[{"name":"repo2"}]`))
		}
	}))
	defer srv.Close()
	srvURL = srv.URL

	repos, err := ListOrgRepos(context.Background(), "acme", "tok", srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(repos) != 2 {
		t.Errorf("expected 2 repos across pages, got %d: %v", len(repos), repos)
	}
}

func TestListOrgRepos_ArchivedField(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[{"name":"active","archived":false},{"name":"old","archived":true}]`))
	}))
	defer srv.Close()

	repos, err := ListOrgRepos(context.Background(), "acme", "tok", srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(repos) != 2 {
		t.Fatalf("expected 2 repos, got %d", len(repos))
	}
	if repos[1].Archived != true {
		t.Errorf("archived field: got %v, want true", repos[1].Archived)
	}
}

func TestListOrgRepos_NonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	_, err := ListOrgRepos(context.Background(), "acme", "bad-token", srv.URL)
	if err == nil {
		t.Fatal("expected error for 401, got nil")
	}
}

func TestListOrgRepos_EmptyOrg(t *testing.T) {
	_, err := ListOrgRepos(context.Background(), "", "token", "")
	if err == nil {
		t.Fatal("expected error for empty org, got nil")
	}
}

func TestParseLinkNext(t *testing.T) {
	cases := []struct {
		header string
		want   string
	}{
		{`<https://api.github.com/orgs/acme/repos?page=2>; rel="next", <...>; rel="last"`, "https://api.github.com/orgs/acme/repos?page=2"},
		{`<https://api.github.com/orgs/acme/repos?page=3>; rel="next"`, "https://api.github.com/orgs/acme/repos?page=3"},
		{`<...>; rel="last"`, ""},
		{``, ""},
	}
	for _, c := range cases {
		got := parseLinkNext(c.header)
		if got != c.want {
			t.Errorf("parseLinkNext(%q) = %q, want %q", c.header, got, c.want)
		}
	}
}

func TestListOrgRepos_BadJSON(t *testing.T) {
	// Server returns invalid JSON → decode error.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`not-json`))
	}))
	defer srv.Close()

	_, err := ListOrgRepos(context.Background(), "acme", "tok", srv.URL)
	if err == nil {
		t.Fatal("expected error for bad JSON, got nil")
	}
	if !strings.Contains(err.Error(), "decode response") {
		t.Errorf("expected 'decode response' in error, got: %v", err)
	}
}

func TestResolveRepoID_BadJSON(t *testing.T) {
	// Server returns invalid JSON → decode error.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`not-json`))
	}))
	defer srv.Close()

	_, err := ResolveRepoID(context.Background(), "acme/web", "tok", srv.URL)
	if err == nil {
		t.Fatal("expected error for bad JSON, got nil")
	}
	if !strings.Contains(err.Error(), "decode response") {
		t.Errorf("expected 'decode response' in error, got: %v", err)
	}
}

func TestResolveRepoID_ZeroID(t *testing.T) {
	// Server returns id:0 → should be treated as missing.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":0}`))
	}))
	defer srv.Close()

	_, err := ResolveRepoID(context.Background(), "acme/web", "tok", srv.URL)
	if err == nil {
		t.Fatal("expected error for id=0, got nil")
	}
	if !strings.Contains(err.Error(), "id missing or zero") {
		t.Errorf("expected 'id missing or zero' in error, got: %v", err)
	}
}

func TestResolveRepoID_TrailingSlashOnBaseURL(t *testing.T) {
	// Trailing slashes on the baseURL should not produce double-slashes in the path.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "//") {
			t.Errorf("path contains double slashes: %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":42}`))
	}))
	defer srv.Close()

	id, err := ResolveRepoID(context.Background(), "owner/repo", "tok", srv.URL+"/")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "42" {
		t.Errorf("id: got %q, want 42", id)
	}
}
