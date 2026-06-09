package project

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// ---- CreateProjectShell -------------------------------------------------------

func TestCreateProjectShell_HappyPath(t *testing.T) {
	want := Project{
		ID:             "proj-uuid-new",
		Name:           "myrepo",
		Slug:           "github/myorg/myrepo",
		OrganizationID: "org-uuid-123",
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify HTTP method.
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}

		// Verify URL path.
		wantPath := "/api/v2/organization/github/myorg/project"
		if r.URL.EscapedPath() != wantPath {
			t.Errorf("expected path %q, got %q", wantPath, r.URL.EscapedPath())
		}

		// Verify request body.
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		var got map[string]any
		if err := json.Unmarshal(body, &got); err != nil {
			t.Fatalf("unmarshal body: %v", err)
		}
		if got["name"] != "myrepo" {
			t.Errorf("body.name: got %q, want myrepo", got["name"])
		}

		respondJSON(w, http.StatusOK, map[string]any{
			"id":              want.ID,
			"name":            want.Name,
			"slug":            want.Slug,
			"organization_id": want.OrganizationID,
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	got, err := c.CreateProjectShell("github", "myorg", "myrepo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != want.ID {
		t.Errorf("ID: got %q, want %q", got.ID, want.ID)
	}
	if got.Name != want.Name {
		t.Errorf("Name: got %q, want %q", got.Name, want.Name)
	}
	if got.Slug != want.Slug {
		t.Errorf("Slug: got %q, want %q", got.Slug, want.Slug)
	}
	if got.OrganizationID != want.OrganizationID {
		t.Errorf("OrganizationID: got %q, want %q", got.OrganizationID, want.OrganizationID)
	}
}

func TestCreateProjectShell_PathEncoding(t *testing.T) {
	// Verify that provider and org with special chars are percent-encoded.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wantPath := "/api/v2/organization/github/my%20org/project"
		if r.URL.EscapedPath() != wantPath {
			t.Errorf("path = %q, want %q", r.URL.EscapedPath(), wantPath)
		}
		respondJSON(w, http.StatusOK, Project{ID: "new-id", Name: "repo", Slug: "github/my org/repo"})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	if _, err := c.CreateProjectShell("github", "my org", "repo"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCreateProjectShell_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusUnprocessableEntity, map[string]string{"message": "project already exists"})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	_, err := c.CreateProjectShell("github", "acme", "web")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestCreateProjectShell_MissingArgsError(t *testing.T) {
	c := &Client{} // no server needed — validation fires before the request
	cases := []struct {
		provider, org, name string
	}{
		{"", "org", "name"},
		{"provider", "", "name"},
		{"provider", "org", ""},
	}
	for _, tc := range cases {
		if _, err := c.CreateProjectShell(tc.provider, tc.org, tc.name); err == nil {
			t.Errorf("CreateProjectShell(%q,%q,%q): expected error, got nil", tc.provider, tc.org, tc.name)
		}
	}
}
