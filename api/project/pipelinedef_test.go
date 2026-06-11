package project

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// ---- ListPipelineDefinitions ------------------------------------------------

func TestListPipelineDefinitions_HappyPath(t *testing.T) {
	const projectID = "proj-uuid-123"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		wantPath := "/api/v2/projects/" + projectID + "/pipeline-definitions"
		if r.URL.Path != wantPath {
			t.Errorf("path: got %q want %q", r.URL.Path, wantPath)
		}
		respondJSON(w, http.StatusOK, map[string]interface{}{
			"items": []map[string]interface{}{
				{
					"id":          "def-uuid-1",
					"name":        "main pipeline",
					"description": "default pipeline definition",
					"created_at":  "2024-01-01T00:00:00Z",
					"config_source": map[string]interface{}{
						"provider":  "github_app",
						"repo":      map[string]string{"full_name": "acme/web", "external_id": "ext-1"},
						"file_path": ".circleci/config.yml",
					},
					"checkout_source": map[string]interface{}{
						"provider": "github_app",
						"repo":     map[string]string{"full_name": "acme/web", "external_id": "ext-1"},
					},
				},
			},
			"next_page_token": "",
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	got, err := c.ListPipelineDefinitions(context.Background(), projectID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 definition, got %d", len(got))
	}
	d := got[0]
	if d.ID != "def-uuid-1" {
		t.Errorf("ID: got %q want def-uuid-1", d.ID)
	}
	if d.Name != "main pipeline" {
		t.Errorf("Name: got %q want main pipeline", d.Name)
	}
	if d.Description != "default pipeline definition" {
		t.Errorf("Description: got %q", d.Description)
	}
	if d.ConfigSource.Provider != "github_app" {
		t.Errorf("ConfigSource.Provider: got %q want github_app", d.ConfigSource.Provider)
	}
	if d.ConfigSource.Repo.FullName != "acme/web" {
		t.Errorf("ConfigSource.Repo.FullName: got %q want acme/web", d.ConfigSource.Repo.FullName)
	}
	if d.ConfigSource.Repo.ExternalID != "ext-1" {
		t.Errorf("ConfigSource.Repo.ExternalID: got %q want ext-1", d.ConfigSource.Repo.ExternalID)
	}
	if d.ConfigSource.FilePath != ".circleci/config.yml" {
		t.Errorf("ConfigSource.FilePath: got %q", d.ConfigSource.FilePath)
	}
	if d.CheckoutSource.Provider != "github_app" {
		t.Errorf("CheckoutSource.Provider: got %q want github_app", d.CheckoutSource.Provider)
	}
}

func TestListPipelineDefinitions_Pagination(t *testing.T) {
	page1 := map[string]interface{}{
		"items": []map[string]interface{}{
			{"id": "def-1", "name": "alpha", "config_source": map[string]interface{}{"provider": "github_app"}, "checkout_source": map[string]interface{}{"provider": "github_app"}},
		},
		"next_page_token": "def-tok",
	}
	page2 := map[string]interface{}{
		"items": []map[string]interface{}{
			{"id": "def-2", "name": "beta", "config_source": map[string]interface{}{"provider": "github_app"}, "checkout_source": map[string]interface{}{"provider": "github_app"}},
		},
		"next_page_token": "",
	}

	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		switch callCount {
		case 1:
			if r.URL.Query().Get("page-token") != "" {
				t.Errorf("first call should have no page-token")
			}
			respondJSON(w, http.StatusOK, page1)
		case 2:
			if got := r.URL.Query().Get("page-token"); got != "def-tok" {
				t.Errorf("page-token: got %q want def-tok", got)
			}
			respondJSON(w, http.StatusOK, page2)
		default:
			t.Error("unexpected third call")
		}
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	got, err := c.ListPipelineDefinitions(context.Background(), "proj-id")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if callCount != 2 {
		t.Errorf("expected 2 calls, got %d", callCount)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 definitions, got %d", len(got))
	}
}

func TestListPipelineDefinitions_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusForbidden, map[string]string{"message": "forbidden"})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	_, err := c.ListPipelineDefinitions(context.Background(), "proj-id")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}
