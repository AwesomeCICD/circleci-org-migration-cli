package org

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func decodeBody(t *testing.T, r *http.Request) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.NewDecoder(r.Body).Decode(&m); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	return m
}

func TestListEnvironmentHierarchies_Paginates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/private/release-tracker/v1/environment-hierarchy" {
			t.Errorf("path %q", r.URL.Path)
		}
		if r.URL.Query().Get("org-id") != envHierarchyOrgUUID {
			t.Errorf("org-id %q", r.URL.Query().Get("org-id"))
		}
		if got := r.URL.Query().Get("page-size"); got != "50" {
			t.Errorf("page-size = %q, want 50 (the server rejects larger pages)", got)
		}
		if r.URL.Query().Get("page-token") == "" {
			respondJSON(w, 200, map[string]any{"items": []map[string]any{{"id": "h1", "name": "alpha"}}, "next_page_token": "tok"})
			return
		}
		respondJSON(w, 200, map[string]any{"items": []map[string]any{{"id": "h2", "name": "beta"}}, "next_page_token": ""})
	}))
	defer srv.Close()
	hs, err := newTestClientWithAppServer(t, srv).ListEnvironmentHierarchies(context.Background(), envHierarchyOrgUUID)
	if err != nil {
		t.Fatal(err)
	}
	if len(hs) != 2 || hs[0].Name != "alpha" || hs[1].ID != "h2" {
		t.Fatalf("got %+v", hs)
	}
}

func TestGetEnvironmentHierarchyDetailAndLevels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/private/release-tracker/v1/environment-hierarchy/h1":
			respondJSON(w, 200, map[string]any{"id": "h1", "name": "alpha",
				"assigned_project_ids": []string{"p1"}, "assigned_component_ids": []string{"c1", "c2"}})
		case "/private/release-tracker/v1/environment-hierarchy/h1/levels":
			respondJSON(w, 200, map[string]any{"items": []map[string]any{
				{"position": 1, "integration_id": "i1", "integration_name": "dev"},
				{"position": 2, "integration_id": "i2", "integration_name": "prod"}}})
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
		}
	}))
	defer srv.Close()
	c := newTestClientWithAppServer(t, srv)
	d, err := c.GetEnvironmentHierarchyDetail(context.Background(), "h1")
	if err != nil || len(d.AssignedProjectIDs) != 1 || len(d.AssignedComponentIDs) != 2 {
		t.Fatalf("detail %+v err %v", d, err)
	}
	ls, err := c.ListEnvironmentHierarchyLevels(context.Background(), "h1")
	if err != nil || len(ls) != 2 || ls[1].IntegrationName != "prod" || ls[0].IntegrationID != "i1" {
		t.Fatalf("levels %+v err %v", ls, err)
	}
}

func TestResolveOrgEnvironmentHierarchy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, 200, map[string]any{"resolved_scope": "ORGANIZATION",
			"hierarchy": map[string]any{"id": "h9", "name": "default-path"}})
	}))
	defer srv.Close()
	scope, id, name, err := newTestClientWithAppServer(t, srv).ResolveOrgEnvironmentHierarchy(context.Background(), envHierarchyOrgUUID)
	if err != nil || scope != "ORGANIZATION" || id != "h9" || name != "default-path" {
		t.Fatalf("%q %q %q %v", scope, id, name, err)
	}
}

func TestCreateEnvironment_Body(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/private/release-tracker/v1/integration" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		b := decodeBody(t, r)
		if b["name"] != "prod" || b["organization_id"] != envHierarchyOrgUUID || b["integration_type"] != "CUSTOM" || b["description"] != "d" {
			t.Errorf("body %v", b)
		}
		respondJSON(w, 201, map[string]any{"id": "int-1", "name": "prod", "type": "CUSTOM"})
	}))
	defer srv.Close()
	id, err := newTestClientWithAppServer(t, srv).CreateEnvironment(context.Background(), envHierarchyOrgUUID, "prod", "CUSTOM", "d")
	if err != nil || id != "int-1" {
		t.Fatalf("%q %v", id, err)
	}
}

func TestCreateEnvironmentHierarchy_Body(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("org-id") != envHierarchyOrgUUID {
			t.Errorf("org-id %q", r.URL.Query().Get("org-id"))
		}
		b := decodeBody(t, r)
		if b["is_default"] != true || b["name"] != "path" {
			t.Errorf("body %v", b)
		}
		levels := b["levels"].([]any)
		l1 := levels[1].(map[string]any)
		if len(levels) != 2 || l1["position"].(float64) != 2 || l1["integration_id"] != "i2" {
			t.Errorf("levels %v", levels)
		}
		if _, leaked := l1["integration_name"]; leaked {
			t.Error("integration_name must not be sent")
		}
		respondJSON(w, 201, map[string]any{"id": "h-new"})
	}))
	defer srv.Close()
	c := newTestClientWithAppServer(t, srv)
	in := EnvHierarchyCreate{Name: "path", IsDefault: true, Levels: []EnvHierarchyLevelRef{
		{Position: 1, IntegrationID: "i1", IntegrationName: "dev"}, {Position: 2, IntegrationID: "i2", IntegrationName: "prod"}}}
	id, err := c.CreateEnvironmentHierarchy(context.Background(), envHierarchyOrgUUID, in)
	if err != nil || id != "h-new" {
		t.Fatalf("%q %v", id, err)
	}
}

func TestUpsertEnvironmentHierarchyAssignment_AndErrorClassifiers(t *testing.T) {
	status := 204
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/private/release-tracker/v1/environment-hierarchy-assignment" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		b := decodeBody(t, r)
		if b["hierarchy_id"] != "h1" || b["entity_type"] != "PROJECT" || len(b["entity_ids"].([]any)) != 2 {
			t.Errorf("body %v", b)
		}
		if status != 204 {
			respondJSON(w, status, map[string]any{"message": "nope"})
			return
		}
		w.WriteHeader(204)
	}))
	defer srv.Close()
	c := newTestClientWithAppServer(t, srv)
	if err := c.UpsertEnvironmentHierarchyAssignment(context.Background(), "h1", "PROJECT", []string{"a", "b"}); err != nil {
		t.Fatal(err)
	}
	for _, code := range []int{401, 403, 404} {
		status = code
		err := c.UpsertEnvironmentHierarchyAssignment(context.Background(), "h1", "PROJECT", []string{"a", "b"})
		if !IsBFFWriteDenied(err) {
			t.Errorf("%d should classify as write-denied: %v", code, err)
		}
	}
	status = 409
	if err := c.UpsertEnvironmentHierarchyAssignment(context.Background(), "h1", "PROJECT", []string{"a", "b"}); !IsBFFConflict(err) || IsBFFWriteDenied(err) {
		t.Errorf("409 misclassified: %v", err)
	}
}

func TestListBFFEnvironments(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/private/release-tracker/v1/environment" {
			t.Errorf("path %q", r.URL.Path)
		}
		respondJSON(w, 200, map[string]any{"items": []map[string]any{{"id": "e1", "name": "dev"}}})
	}))
	defer srv.Close()
	es, err := newTestClientWithAppServer(t, srv).ListBFFEnvironments(context.Background(), envHierarchyOrgUUID)
	if err != nil || len(es) != 1 || es[0].ID != "e1" {
		t.Fatalf("%+v %v", es, err)
	}
}
