package org

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

const testOrgID = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
const testUserID = "user-111-222-333-444-555"
const testGroupID = "grp-aaa-bbb-ccc"
const testProjectID = "proj-111-222-333"

// ─────────────────────────────────────────────────────────────────────────────
// ListOrgRoleGrants
// ─────────────────────────────────────────────────────────────────────────────

func TestListOrgRoleGrants_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		wantPath := "/private/ciam/orgs/" + testOrgID + "/role-grants"
		if r.URL.Path != wantPath {
			t.Errorf("expected path %q, got %q", wantPath, r.URL.Path)
		}
		if got := r.Header.Get("Circle-Token"); got != "test-token" {
			t.Errorf("Circle-Token: got %q, want %q", got, "test-token")
		}
		respondJSON(w, http.StatusOK, map[string]any{
			"items": []map[string]any{
				{"user_id": testUserID, "email": "alice@example.com", "username": "alice", "role": "org-admin"},
				{"user_id": "user-999", "email": "bob@example.com", "username": "bob", "role": "org-viewer"},
			},
		})
	}))
	defer srv.Close()

	c := newTestClientWithAppServer(t, srv)
	grants, err := c.ListOrgRoleGrants(testOrgID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(grants) != 2 {
		t.Fatalf("expected 2 grants, got %d", len(grants))
	}
	if grants[0].UserID != testUserID || grants[0].Email != "alice@example.com" || grants[0].Role != "org-admin" {
		t.Errorf("unexpected grant[0]: %+v", grants[0])
	}
	if grants[1].Email != "bob@example.com" || grants[1].Role != "org-viewer" {
		t.Errorf("unexpected grant[1]: %+v", grants[1])
	}
}

func TestListOrgRoleGrants_Empty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusOK, map[string]any{"items": []any{}})
	}))
	defer srv.Close()

	c := newTestClientWithAppServer(t, srv)
	grants, err := c.ListOrgRoleGrants(testOrgID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(grants) != 0 {
		t.Errorf("expected empty, got %v", grants)
	}
}

func TestListOrgRoleGrants_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusForbidden, map[string]string{"message": "forbidden"})
	}))
	defer srv.Close()

	c := newTestClientWithAppServer(t, srv)
	_, err := c.ListOrgRoleGrants(testOrgID)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// SetOrgUserRole
// ─────────────────────────────────────────────────────────────────────────────

func TestSetOrgUserRole_HappyPath(t *testing.T) {
	var gotMethod, gotPath, gotBody string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		buf := make([]byte, 256)
		n, _ := r.Body.Read(buf)
		gotBody = string(buf[:n])
		respondJSON(w, http.StatusOK, map[string]any{})
	}))
	defer srv.Close()

	c := newTestClientWithAppServer(t, srv)
	err := c.SetOrgUserRole(testOrgID, testUserID, "org-admin")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantPath := "/private/ciam/orgs/" + testOrgID + "/role-grants/user/" + testUserID
	if gotMethod != http.MethodPut {
		t.Errorf("expected PUT, got %s", gotMethod)
	}
	if gotPath != wantPath {
		t.Errorf("expected path %q, got %q", wantPath, gotPath)
	}
	if gotBody == "" {
		t.Error("expected non-empty body")
	}
}

func TestSetOrgUserRole_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusBadRequest, map[string]string{"message": "invalid role"})
	}))
	defer srv.Close()

	c := newTestClientWithAppServer(t, srv)
	err := c.SetOrgUserRole(testOrgID, testUserID, "bad-role")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// CreateGroup
// ─────────────────────────────────────────────────────────────────────────────

func TestCreateGroup_HappyPath(t *testing.T) {
	var gotMethod, gotPath string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		respondJSON(w, http.StatusOK, map[string]any{
			"id":   testGroupID,
			"name": "security-team",
		})
	}))
	defer srv.Close()

	c := newTestClientWithAppServer(t, srv)
	g, err := c.CreateGroup(testOrgID, "security-team", "Security team group")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("expected POST, got %s", gotMethod)
	}
	wantPath := "/private/ciam/orgs/" + testOrgID + "/groups"
	if gotPath != wantPath {
		t.Errorf("expected path %q, got %q", wantPath, gotPath)
	}
	if g.ID != testGroupID || g.Name != "security-team" {
		t.Errorf("unexpected group: %+v", g)
	}
}

func TestCreateGroup_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusConflict, map[string]string{"message": "already exists"})
	}))
	defer srv.Close()

	c := newTestClientWithAppServer(t, srv)
	_, err := c.CreateGroup(testOrgID, "existing", "")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// AddUsersToGroup
// ─────────────────────────────────────────────────────────────────────────────

func TestAddUsersToGroup_HappyPath(t *testing.T) {
	var gotMethod, gotPath string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		respondJSON(w, http.StatusOK, map[string]any{})
	}))
	defer srv.Close()

	c := newTestClientWithAppServer(t, srv)
	err := c.AddUsersToGroup(testOrgID, testGroupID, []string{"uid-1", "uid-2"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("expected POST, got %s", gotMethod)
	}
	wantPath := "/private/ciam/orgs/" + testOrgID + "/groups/" + testGroupID + "/add-users"
	if gotPath != wantPath {
		t.Errorf("expected path %q, got %q", wantPath, gotPath)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ListProjectUserRoleGrants
// ─────────────────────────────────────────────────────────────────────────────

func TestListProjectUserRoleGrants_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		wantPath := "/private/ciam/orgs/" + testOrgID + "/projects/" + testProjectID + "/role-grants"
		if r.URL.Path != wantPath {
			t.Errorf("expected path %q, got %q", wantPath, r.URL.Path)
		}
		respondJSON(w, http.StatusOK, map[string]any{
			"items": []map[string]any{
				{"user_id": testUserID, "email": "alice@example.com", "username": "alice", "role": "project-admin"},
			},
		})
	}))
	defer srv.Close()

	c := newTestClientWithAppServer(t, srv)
	grants, err := c.ListProjectUserRoleGrants(testOrgID, testProjectID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(grants) != 1 {
		t.Fatalf("expected 1 grant, got %d", len(grants))
	}
	if grants[0].UserID != testUserID || grants[0].Email != "alice@example.com" || grants[0].Role != "project-admin" {
		t.Errorf("unexpected grant: %+v", grants[0])
	}
}

func TestListProjectUserRoleGrants_Empty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusOK, map[string]any{"items": []any{}})
	}))
	defer srv.Close()

	c := newTestClientWithAppServer(t, srv)
	grants, err := c.ListProjectUserRoleGrants(testOrgID, testProjectID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(grants) != 0 {
		t.Errorf("expected empty, got %v", grants)
	}
}

func TestListProjectUserRoleGrants_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusForbidden, map[string]string{"message": "forbidden"})
	}))
	defer srv.Close()

	c := newTestClientWithAppServer(t, srv)
	_, err := c.ListProjectUserRoleGrants(testOrgID, testProjectID)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// SetProjectUserRole
// ─────────────────────────────────────────────────────────────────────────────

func TestSetProjectUserRole_HappyPath(t *testing.T) {
	var gotMethod, gotPath string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		respondJSON(w, http.StatusOK, map[string]any{})
	}))
	defer srv.Close()

	c := newTestClientWithAppServer(t, srv)
	err := c.SetProjectUserRole(testOrgID, testProjectID, testUserID, "project-contributor")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotMethod != http.MethodPut {
		t.Errorf("expected PUT, got %s", gotMethod)
	}
	wantPath := "/private/ciam/orgs/" + testOrgID + "/projects/" + testProjectID + "/role-grants/user/" + testUserID
	if gotPath != wantPath {
		t.Errorf("expected path %q, got %q", wantPath, gotPath)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ListProjectGroupRoleGrants
// ─────────────────────────────────────────────────────────────────────────────

func TestListProjectGroupRoleGrants_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wantPath := "/private/ciam/orgs/" + testOrgID + "/projects/" + testProjectID + "/groups"
		if r.URL.Path != wantPath {
			t.Errorf("expected path %q, got %q", wantPath, r.URL.Path)
		}
		respondJSON(w, http.StatusOK, map[string]any{
			"items": []map[string]any{
				{"group_id": testGroupID, "role": "project-viewer"},
			},
		})
	}))
	defer srv.Close()

	c := newTestClientWithAppServer(t, srv)
	grants, err := c.ListProjectGroupRoleGrants(testOrgID, testProjectID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(grants) != 1 {
		t.Fatalf("expected 1 grant, got %d", len(grants))
	}
	if grants[0].GroupID != testGroupID || grants[0].Role != "project-viewer" {
		t.Errorf("unexpected grant: %+v", grants[0])
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// AddProjectGroupRole
// ─────────────────────────────────────────────────────────────────────────────

func TestAddProjectGroupRole_HappyPath(t *testing.T) {
	var gotMethod, gotPath string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		respondJSON(w, http.StatusOK, map[string]any{})
	}))
	defer srv.Close()

	c := newTestClientWithAppServer(t, srv)
	err := c.AddProjectGroupRole(testOrgID, testProjectID, []string{testGroupID}, "project-admin")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("expected POST, got %s", gotMethod)
	}
	wantPath := "/private/ciam/orgs/" + testOrgID + "/projects/" + testProjectID + "/groups"
	if gotPath != wantPath {
		t.Errorf("expected path %q, got %q", wantPath, gotPath)
	}
}

func TestAddProjectGroupRole_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusBadRequest, map[string]string{"message": "bad request"})
	}))
	defer srv.Close()

	c := newTestClientWithAppServer(t, srv)
	err := c.AddProjectGroupRole(testOrgID, testProjectID, []string{testGroupID}, "project-admin")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestAddUsersToGroup_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"message": "server error"})
	}))
	defer srv.Close()

	c := newTestClientWithAppServer(t, srv)
	err := c.AddUsersToGroup(testOrgID, testGroupID, []string{"uid-1"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestSetProjectUserRole_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusBadRequest, map[string]string{"message": "bad role"})
	}))
	defer srv.Close()

	c := newTestClientWithAppServer(t, srv)
	err := c.SetProjectUserRole(testOrgID, testProjectID, testUserID, "bad-role")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestListProjectGroupRoleGrants_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusForbidden, map[string]string{"message": "forbidden"})
	}))
	defer srv.Close()

	c := newTestClientWithAppServer(t, srv)
	_, err := c.ListProjectGroupRoleGrants(testOrgID, testProjectID)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}
