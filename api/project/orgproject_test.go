package project

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// newTestClientWithPrivate wires a Client whose v2, v1.1, and private bases all
// point to the given httptest.Server.  Because newClientFromBases derives the
// private base from v2Base's host at /api/private/, a single test server
// handles all three API roots.
func newTestClientWithPrivate(t *testing.T, srv *httptest.Server) *Client {
	t.Helper()
	serverURL, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}
	v2Base := serverURL.ResolveReference(&url.URL{Path: "/api/v2/"})
	v11Base := serverURL.ResolveReference(&url.URL{Path: "/api/v1.1/"})
	return newClientFromBases(v2Base, v11Base, "test-token", srv.Client())
}

// ---- ListOrgProjects --------------------------------------------------------

func TestListOrgProjects_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/api/private/project" {
			t.Errorf("unexpected path: %q", r.URL.Path)
		}
		if got := r.URL.Query().Get("organization-id"); got != "org-uuid-abc" {
			t.Errorf("organization-id: got %q want org-uuid-abc", got)
		}
		if got := r.URL.Query().Get("page-size"); got != "50" {
			t.Errorf("page-size: got %q want 50", got)
		}
		respondJSON(w, http.StatusOK, map[string]interface{}{
			"items": []map[string]interface{}{
				{"id": "proj-1", "slug": "gh/myorg/web", "name": "web"},
				{"id": "proj-2", "slug": "circleci/org-uuid/proj-uuid", "name": "app"},
			},
			"next_page_token": "",
		})
	}))
	defer srv.Close()

	c := newTestClientWithPrivate(t, srv)
	got, err := c.ListOrgProjects("org-uuid-abc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 projects, got %d", len(got))
	}
	// OAuth slug
	if got[0].ID != "proj-1" || got[0].Slug != "gh/myorg/web" || got[0].Name != "web" {
		t.Errorf("unexpected first project: %+v", got[0])
	}
	// App slug
	if got[1].ID != "proj-2" || got[1].Slug != "circleci/org-uuid/proj-uuid" || got[1].Name != "app" {
		t.Errorf("unexpected second project: %+v", got[1])
	}
}

func TestListOrgProjects_Pagination(t *testing.T) {
	page1 := map[string]interface{}{
		"items":           []map[string]interface{}{{"id": "p1", "slug": "gh/org/repo1", "name": "repo1"}},
		"next_page_token": "tok-page2",
	}
	page2 := map[string]interface{}{
		"items":           []map[string]interface{}{{"id": "p2", "slug": "circleci/org/proj2", "name": "proj2"}},
		"next_page_token": "",
	}

	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		switch callCount {
		case 1:
			if r.URL.Query().Get("page-token") != "" {
				t.Errorf("first call should have no page-token, got %q", r.URL.Query().Get("page-token"))
			}
			respondJSON(w, http.StatusOK, page1)
		case 2:
			if got := r.URL.Query().Get("page-token"); got != "tok-page2" {
				t.Errorf("second call page-token: got %q want tok-page2", got)
			}
			respondJSON(w, http.StatusOK, page2)
		default:
			t.Errorf("unexpected third call")
			http.Error(w, "unexpected", http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	c := newTestClientWithPrivate(t, srv)
	got, err := c.ListOrgProjects("org-id")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if callCount != 2 {
		t.Errorf("expected 2 HTTP calls, got %d", callCount)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 projects, got %d", len(got))
	}
	if got[0].Slug != "gh/org/repo1" {
		t.Errorf("first project slug: got %q", got[0].Slug)
	}
	if got[1].Slug != "circleci/org/proj2" {
		t.Errorf("second project slug: got %q", got[1].Slug)
	}
}

func TestListOrgProjects_OAuthSlug(t *testing.T) {
	// Verify a GitHub OAuth slug (gh/org/repo) is returned correctly.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusOK, map[string]interface{}{
			"items":           []map[string]interface{}{{"id": "p-oauth", "slug": "gh/acme/web", "name": "web"}},
			"next_page_token": "",
		})
	}))
	defer srv.Close()

	c := newTestClientWithPrivate(t, srv)
	got, err := c.ListOrgProjects("oauth-org-id")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].Slug != "gh/acme/web" {
		t.Errorf("unexpected result: %+v", got)
	}
}

func TestListOrgProjects_AppSlug(t *testing.T) {
	// Verify a GitHub App slug (circleci/<orgUUID>/<projUUID>) is returned correctly.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusOK, map[string]interface{}{
			"items":           []map[string]interface{}{{"id": "p-app", "slug": "circleci/aaaa-1111/bbbb-2222", "name": "myapp"}},
			"next_page_token": "",
		})
	}))
	defer srv.Close()

	c := newTestClientWithPrivate(t, srv)
	got, err := c.ListOrgProjects("aaaa-1111")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].Slug != "circleci/aaaa-1111/bbbb-2222" {
		t.Errorf("unexpected result: %+v", got)
	}
}

func TestListOrgProjects_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusForbidden, map[string]string{"message": "forbidden"})
	}))
	defer srv.Close()

	c := newTestClientWithPrivate(t, srv)
	_, err := c.ListOrgProjects("some-org")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}
