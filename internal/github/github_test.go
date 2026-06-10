package github

import (
	"errors"
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

	id, err := ResolveRepoID("acme/web", "my-token", srv.URL)
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

	id, err := ResolveRepoID("acme/web", "", srv.URL)
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

	_, err := ResolveRepoID("acme/missing", "token", srv.URL)
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

	_, err := ResolveRepoID("acme/web", "token", srv.URL)
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

	_, err := ResolveRepoID("acme/web", "token", srv.URL)
	if err == nil {
		t.Fatal("expected error for 500, got nil")
	}
}

func TestResolveRepoID_EmptyFullName(t *testing.T) {
	if _, err := ResolveRepoID("", "token", ""); err == nil {
		t.Error("expected error for empty fullName, got nil")
	}
}

func TestResolveRepoID_InvalidFullName(t *testing.T) {
	cases := []string{"noslash", "/noowner", "owner/"}
	for _, name := range cases {
		if _, err := ResolveRepoID(name, "token", ""); err == nil {
			t.Errorf("ResolveRepoID(%q): expected error, got nil", name)
		}
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

	id, err := ResolveRepoID("owner/repo", "tok", srv.URL+"/")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "42" {
		t.Errorf("id: got %q, want 42", id)
	}
}
