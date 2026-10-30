package org

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

const deployOrgUUID = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"

func TestListDeployEnvironments_Paginates(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v3/deploy/environments" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if got := r.URL.Query().Get("filter[org_id]"); got != deployOrgUUID {
			t.Errorf("filter[org_id] = %q", got)
		}
		calls++
		if r.URL.Query().Get("page[cursor]") == "" {
			respondJSON(w, http.StatusOK, map[string]any{
				"data": []map[string]any{{"id": "e1", "attributes": map[string]any{"name": "production"}}},
				"page": map[string]any{"next": "cur2"},
			})
			return
		}
		if r.URL.Query().Get("page[cursor]") != "cur2" {
			t.Errorf("cursor = %q", r.URL.Query().Get("page[cursor]"))
		}
		respondJSON(w, http.StatusOK, map[string]any{
			"data": []map[string]any{{"id": "e2", "attributes": map[string]any{"name": "staging"}}},
		})
	}))
	defer srv.Close()

	envs, err := newTestClientWithAppServer(t, srv).ListDeployEnvironments(context.Background(), deployOrgUUID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 2 || len(envs) != 2 || envs[0].Name != "production" || envs[1].Name != "staging" {
		t.Errorf("calls=%d envs=%+v", calls, envs)
	}
}

func TestGetDeployEnvironment_KindAndDescription(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v3/deploy/environments/e1" {
			t.Errorf("path = %q", r.URL.Path)
		}
		respondJSON(w, http.StatusOK, map[string]any{"data": map[string]any{
			"id": "e1",
			"attributes": map[string]any{
				"name": "production", "kind": "custom", "description": "prod",
				"labels": []map[string]any{{"key": "tier", "value": "1"}},
			},
		}})
	}))
	defer srv.Close()
	e, err := newTestClientWithAppServer(t, srv).GetDeployEnvironment(context.Background(), "e1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if e.Kind != "custom" || e.Description != "prod" || len(e.Labels) != 1 || e.Labels[0].Key != "tier" {
		t.Errorf("env = %+v", e)
	}
}

func TestListDeployComponents_ProjectReference(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v3/deploy/components" {
			t.Errorf("path = %q", r.URL.Path)
		}
		respondJSON(w, http.StatusOK, map[string]any{"data": []map[string]any{
			{
				"id":         "c1",
				"attributes": map[string]any{"name": "web", "release_count": 0},
				"references": map[string]any{"org": map[string]any{"id": deployOrgUUID}, "project": map[string]any{"id": "proj-1"}},
			},
			{"id": "c2", "attributes": map[string]any{"name": "loose"}},
		}})
	}))
	defer srv.Close()
	comps, err := newTestClientWithAppServer(t, srv).ListDeployComponents(context.Background(), deployOrgUUID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(comps) != 2 || comps[0].ProjectID != "proj-1" || comps[1].ProjectID != "" {
		t.Errorf("components = %+v", comps)
	}
}

func TestGetDeploySettings_Pipelines(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v3/deploy/settings" || r.URL.Query().Get("filter[project_id]") != "proj-1" {
			t.Errorf("request = %s", r.URL.String())
		}
		respondJSON(w, http.StatusOK, map[string]any{"data": map[string]any{
			"id": "proj-1",
			"references": map[string]any{
				"project":   map[string]any{"id": "proj-1"},
				"pipelines": []map[string]any{{"id": "p1"}, {"id": "p2"}},
			},
		}})
	}))
	defer srv.Close()
	s, err := newTestClientWithAppServer(t, srv).GetDeploySettings(context.Background(), "proj-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.ProjectID != "proj-1" || len(s.PipelineIDs) != 2 {
		t.Errorf("settings = %+v", s)
	}
}

func TestGetOrgOrbs_NamespaceName(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		respondJSON(w, http.StatusOK, map[string]any{"orbs": []map[string]any{
			{"orb_name": "acme/tool", "orb_id": "o1", "namespace_name": "acme"},
		}})
	}))
	defer srv.Close()
	orbs, err := newTestClientWithAppServer(t, srv).GetOrgOrbs(context.Background(), orbsOrgUUID)
	if err != nil || len(orbs) != 1 || orbs[0].NamespaceName != "acme" {
		t.Errorf("orbs=%+v err=%v", orbs, err)
	}
}
