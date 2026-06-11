package project

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// ---- CreateAppProject --------------------------------------------------------

func TestCreateAppProject_HappyPath(t *testing.T) {
	want := Project{
		ID:             "proj-uuid-app",
		Name:           "myrepo",
		Slug:           "circleci/org-uuid-abc/proj-uuid-app",
		OrganizationID: "org-uuid-abc",
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		wantPath := "/api/v2/organization/org-uuid-abc/project"
		if r.URL.EscapedPath() != wantPath {
			t.Errorf("path = %q, want %q", r.URL.EscapedPath(), wantPath)
		}
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
	got, err := c.CreateAppProject(context.Background(), "org-uuid-abc", "myrepo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != want.ID {
		t.Errorf("ID: got %q, want %q", got.ID, want.ID)
	}
	if got.Slug != want.Slug {
		t.Errorf("Slug: got %q, want %q", got.Slug, want.Slug)
	}
	if got.OrganizationID != want.OrganizationID {
		t.Errorf("OrganizationID: got %q, want %q", got.OrganizationID, want.OrganizationID)
	}
}

func TestCreateAppProject_MissingArgsError(t *testing.T) {
	c := &Client{}
	cases := []struct{ orgID, name string }{
		{"", "repo"},
		{"org-uuid", ""},
	}
	for _, tc := range cases {
		if _, err := c.CreateAppProject(context.Background(), tc.orgID, tc.name); err == nil {
			t.Errorf("CreateAppProject(%q,%q): expected error, got nil", tc.orgID, tc.name)
		}
	}
}

func TestCreateAppProject_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusUnprocessableEntity, map[string]string{"message": "project already exists"})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	_, err := c.CreateAppProject(context.Background(), "org-uuid-abc", "myrepo")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestCreateAppProject_PathEncoding(t *testing.T) {
	// Verify that an org UUID with special chars is percent-encoded.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wantPath := "/api/v2/organization/org%2Fuuid/project"
		if r.URL.EscapedPath() != wantPath {
			t.Errorf("path = %q, want %q", r.URL.EscapedPath(), wantPath)
		}
		respondJSON(w, http.StatusOK, Project{ID: "p1", Name: "repo", Slug: "circleci/org/uuid/p1"})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	if _, err := c.CreateAppProject(context.Background(), "org/uuid", "repo"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ---- CreatePipelineDefinition -----------------------------------------------

func TestCreatePipelineDefinition_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		wantPath := "/api/v2/projects/proj-uuid-123/pipeline-definitions"
		if r.URL.EscapedPath() != wantPath {
			t.Errorf("path = %q, want %q", r.URL.EscapedPath(), wantPath)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		var got map[string]any
		if err := json.Unmarshal(body, &got); err != nil {
			t.Fatalf("unmarshal body: %v", err)
		}
		if got["name"] != "default" {
			t.Errorf("body.name: got %v, want default", got["name"])
		}
		cfg, ok := got["config_source"].(map[string]any)
		if !ok {
			t.Fatalf("config_source missing or wrong type: %T", got["config_source"])
		}
		if cfg["provider"] != "github_app" {
			t.Errorf("config_source.provider: got %v, want github_app", cfg["provider"])
		}
		repo, _ := cfg["repo"].(map[string]any)
		if repo["external_id"] != "98765" {
			t.Errorf("config_source.repo.external_id: got %v, want 98765", repo["external_id"])
		}
		if cfg["file_path"] != ".circleci/config.yml" {
			t.Errorf("config_source.file_path: got %v, want .circleci/config.yml", cfg["file_path"])
		}
		respondJSON(w, http.StatusOK, map[string]any{"id": "def-uuid-abc"})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	id, err := c.CreatePipelineDefinition(context.Background(), "proj-uuid-123", PipelineDefinitionSpec{
		Name:               "default",
		ConfigProvider:     "github_app",
		ConfigExternalID:   "98765",
		ConfigFilePath:     ".circleci/config.yml",
		CheckoutProvider:   "github_app",
		CheckoutExternalID: "98765",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "def-uuid-abc" {
		t.Errorf("id: got %q, want def-uuid-abc", id)
	}
}

func TestCreatePipelineDefinition_MissingArgsError(t *testing.T) {
	c := &Client{}
	cases := []struct {
		projectID string
		spec      PipelineDefinitionSpec
	}{
		{"", PipelineDefinitionSpec{Name: "foo"}},
		{"proj-id", PipelineDefinitionSpec{Name: ""}},
	}
	for _, tc := range cases {
		if _, err := c.CreatePipelineDefinition(context.Background(), tc.projectID, tc.spec); err == nil {
			t.Errorf("CreatePipelineDefinition(%q, spec.Name=%q): expected error, got nil",
				tc.projectID, tc.spec.Name)
		}
	}
}

func TestCreatePipelineDefinition_DescriptionOmitted(t *testing.T) {
	// Description should be omitted from the JSON body when empty.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var got map[string]any
		_ = json.Unmarshal(body, &got)
		if _, has := got["description"]; has {
			t.Error("description should be omitted from request body when empty")
		}
		respondJSON(w, http.StatusOK, map[string]any{"id": "def-id"})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	if _, err := c.CreatePipelineDefinition(context.Background(), "proj-id", PipelineDefinitionSpec{
		Name:               "noDesc",
		ConfigProvider:     "github_app",
		ConfigExternalID:   "1",
		CheckoutProvider:   "github_app",
		CheckoutExternalID: "1",
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCreatePipelineDefinition_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"message": "internal error"})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	_, err := c.CreatePipelineDefinition(context.Background(), "proj-id", PipelineDefinitionSpec{
		Name:             "default",
		ConfigProvider:   "github_app",
		ConfigExternalID: "1",
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ---- CreateTrigger ----------------------------------------------------------

func TestCreateTrigger_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		wantPath := "/api/v2/projects/proj-uuid-123/pipeline-definitions/def-uuid-456/triggers"
		if r.URL.EscapedPath() != wantPath {
			t.Errorf("path = %q, want %q", r.URL.EscapedPath(), wantPath)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		var got map[string]any
		if err := json.Unmarshal(body, &got); err != nil {
			t.Fatalf("unmarshal body: %v", err)
		}
		if got["event_preset"] != "code_push" {
			t.Errorf("event_preset: got %v, want code_push", got["event_preset"])
		}
		if got["disabled"] != true {
			t.Errorf("disabled: got %v, want true", got["disabled"])
		}
		es, _ := got["event_source"].(map[string]any)
		if es["provider"] != "github_app" {
			t.Errorf("event_source.provider: got %v, want github_app", es["provider"])
		}
		repo, _ := es["repo"].(map[string]any)
		if repo["external_id"] != "11111" {
			t.Errorf("event_source.repo.external_id: got %v, want 11111", repo["external_id"])
		}
		respondJSON(w, http.StatusOK, map[string]any{"id": "trigger-uuid-789"})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	id, err := c.CreateTrigger(context.Background(), "proj-uuid-123", "def-uuid-456", TriggerSpec{
		Provider:    "github_app",
		ExternalID:  "11111",
		EventPreset: "code_push",
		Disabled:    true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "trigger-uuid-789" {
		t.Errorf("id: got %q, want trigger-uuid-789", id)
	}
}

func TestCreateTrigger_MissingArgsError(t *testing.T) {
	c := &Client{}
	cases := []struct{ projectID, defID string }{
		{"", "def-id"},
		{"proj-id", ""},
	}
	for _, tc := range cases {
		if _, err := c.CreateTrigger(context.Background(), tc.projectID, tc.defID, TriggerSpec{}); err == nil {
			t.Errorf("CreateTrigger(%q,%q): expected error, got nil", tc.projectID, tc.defID)
		}
	}
}

func TestCreateTrigger_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusUnprocessableEntity, map[string]string{"message": "bad request"})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	_, err := c.CreateTrigger(context.Background(), "proj-id", "def-id", TriggerSpec{Provider: "github_app", ExternalID: "1"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ---- EnableTrigger ----------------------------------------------------------

func TestEnableTrigger_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("expected PATCH, got %s", r.Method)
		}
		wantPath := "/api/v2/projects/proj-uuid-123/triggers/trigger-uuid-789"
		if r.URL.EscapedPath() != wantPath {
			t.Errorf("path = %q, want %q", r.URL.EscapedPath(), wantPath)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		var got map[string]any
		if err := json.Unmarshal(body, &got); err != nil {
			t.Fatalf("unmarshal body: %v", err)
		}
		if got["disabled"] != false {
			t.Errorf("disabled: got %v, want false", got["disabled"])
		}
		respondJSON(w, http.StatusOK, map[string]any{"id": "trigger-uuid-789", "disabled": false})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	if err := c.EnableTrigger(context.Background(), "proj-uuid-123", "trigger-uuid-789"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEnableTrigger_MissingArgsError(t *testing.T) {
	c := &Client{}
	cases := []struct{ projectID, triggerID string }{
		{"", "trigger-id"},
		{"proj-id", ""},
	}
	for _, tc := range cases {
		if err := c.EnableTrigger(context.Background(), tc.projectID, tc.triggerID); err == nil {
			t.Errorf("EnableTrigger(%q,%q): expected error, got nil", tc.projectID, tc.triggerID)
		}
	}
}

func TestEnableTrigger_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusNotFound, map[string]string{"message": "not found"})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	if err := c.EnableTrigger(context.Background(), "proj-id", "trigger-id"); err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ---- DeleteProject ----------------------------------------------------------

func TestDeleteProject_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		wantPath := "/api/v2/project/circleci/org-id/proj-id"
		if r.URL.EscapedPath() != wantPath {
			t.Errorf("path = %q, want %q", r.URL.EscapedPath(), wantPath)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	if err := c.DeleteProject(context.Background(), "circleci/org-id/proj-id"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDeleteProject_MissingSlugError(t *testing.T) {
	c := &Client{}
	if err := c.DeleteProject(context.Background(), ""); err == nil {
		t.Error("DeleteProject with empty slug: expected error, got nil")
	}
}

func TestDeleteProject_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusNotFound, map[string]string{"message": "not found"})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	if err := c.DeleteProject(context.Background(), "gh/acme/web"); err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestDeleteProject_SlugPathEncoding(t *testing.T) {
	// Verify that special chars in slug components are encoded.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wantPath := "/api/v2/project/gh/my%20org/my%20repo"
		if r.URL.EscapedPath() != wantPath {
			t.Errorf("path = %q, want %q", r.URL.EscapedPath(), wantPath)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	if err := c.DeleteProject(context.Background(), "gh/my org/my repo"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
