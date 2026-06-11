package context

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ---- CreateContext ----------------------------------------------------------

func TestCreateContext_HappyPath(t *testing.T) {
	want := Context{ID: "ctx-new-1", Name: "production", CreatedAt: "2024-06-01T00:00:00Z"}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/v2/context" {
			t.Errorf("expected path /api/v2/context, got %s", r.URL.Path)
		}

		// Assert request body.
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if body["name"] != "production" {
			t.Errorf("name: got %v, want production", body["name"])
		}
		owner, ok := body["owner"].(map[string]interface{})
		if !ok {
			t.Fatalf("owner field missing or not an object: %v", body["owner"])
		}
		if owner["id"] != "org-uuid-123" {
			t.Errorf("owner.id: got %v, want org-uuid-123", owner["id"])
		}
		if owner["type"] != "organization" {
			t.Errorf("owner.type: got %v, want organization", owner["type"])
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(jsonBody(t, want)) //nolint:errcheck
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	got, err := c.CreateContext(context.Background(), "production", "org-uuid-123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil Context, got nil")
		return
	}
	if *got != want {
		t.Errorf("got %+v, want %+v", *got, want)
	}
}

func TestCreateContext_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"message":"permission denied"}`)
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	_, err := c.CreateContext(context.Background(), "some-ctx", "org-uuid")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "permission denied") {
		t.Errorf("error should contain 'permission denied', got: %v", err)
	}
}

func TestCreateContext_EmptyName(t *testing.T) {
	c := &Client{}
	_, err := c.CreateContext(context.Background(), "", "org-uuid")
	if err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestCreateContext_EmptyOwnerID(t *testing.T) {
	c := &Client{}
	_, err := c.CreateContext(context.Background(), "myctx", "")
	if err == nil {
		t.Fatal("expected error for empty ownerID")
	}
}

// ---- UpsertEnvVar -----------------------------------------------------------

func TestUpsertEnvVar_PutPathAndBody(t *testing.T) {
	const contextID = "ctx-abc-123"
	const varName = "MY_SECRET"
	const varValue = "super-secret-value"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("expected PUT, got %s", r.Method)
		}
		wantPath := "/api/v2/context/" + contextID + "/environment-variable/" + varName
		if r.URL.Path != wantPath {
			t.Errorf("expected path %s, got %s", wantPath, r.URL.Path)
		}

		// Assert body contains only the "value" key.
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if body["value"] != varValue {
			t.Errorf("value: got %v, want %s", body["value"], varValue)
		}
		if len(body) != 1 {
			t.Errorf("body should have exactly 1 key (value), got %v", body)
		}

		// API returns the env var after upsert.
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(jsonBody(t, map[string]string{ //nolint:errcheck
			"variable":   varName,
			"created_at": "2024-01-01T00:00:00Z",
			"updated_at": "2024-06-01T00:00:00Z",
		}))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	if err := c.UpsertEnvVar(context.Background(), contextID, varName, varValue); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestUpsertEnvVar_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"message":"context not found"}`)
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	err := c.UpsertEnvVar(context.Background(), "missing-ctx", "VAR", "val")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestUpsertEnvVar_EmptyContextID(t *testing.T) {
	c := &Client{}
	if err := c.UpsertEnvVar(context.Background(), "", "VAR", "val"); err == nil {
		t.Fatal("expected error for empty contextID")
	}
}

func TestUpsertEnvVar_EmptyName(t *testing.T) {
	c := &Client{}
	if err := c.UpsertEnvVar(context.Background(), "ctx-123", "", "val"); err == nil {
		t.Fatal("expected error for empty name")
	}
}

// ---- CreateRestriction ------------------------------------------------------

func TestCreateRestriction_ProjectType(t *testing.T) {
	const contextID = "ctx-def-456"
	const restrictionType = "project"
	const restrictionValue = "proj-uuid-789"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		wantPath := "/api/v2/context/" + contextID + "/restrictions"
		if r.URL.Path != wantPath {
			t.Errorf("expected path %s, got %s", wantPath, r.URL.Path)
		}

		// Assert request body.
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if body["restriction_type"] != restrictionType {
			t.Errorf("restriction_type: got %v, want %s", body["restriction_type"], restrictionType)
		}
		if body["restriction_value"] != restrictionValue {
			t.Errorf("restriction_value: got %v, want %s", body["restriction_value"], restrictionValue)
		}

		// API returns 201 Created with the restriction.
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		w.Write(jsonBody(t, Restriction{ //nolint:errcheck
			ID:        "restr-uuid-1",
			Type:      restrictionType,
			Value:     restrictionValue,
			ContextID: contextID,
		}))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	if err := c.CreateRestriction(context.Background(), contextID, restrictionType, restrictionValue); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCreateRestriction_ExpressionType(t *testing.T) {
	const contextID = "ctx-ghi-789"
	const restrictionType = "expression"
	const restrictionValue = `pipeline.git.branch == "main"`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if body["restriction_type"] != restrictionType {
			t.Errorf("restriction_type: got %v, want %s", body["restriction_type"], restrictionType)
		}
		if body["restriction_value"] != restrictionValue {
			t.Errorf("restriction_value: got %v, want %s", body["restriction_value"], restrictionValue)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		w.Write(jsonBody(t, Restriction{ //nolint:errcheck
			ID:        "restr-uuid-2",
			Type:      restrictionType,
			Value:     restrictionValue,
			ContextID: contextID,
		}))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	if err := c.CreateRestriction(context.Background(), contextID, restrictionType, restrictionValue); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCreateRestriction_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"message":"invalid restriction_type"}`)
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	err := c.CreateRestriction(context.Background(), "ctx-123", "bad-type", "some-value")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestCreateRestriction_EmptyContextID(t *testing.T) {
	c := &Client{}
	if err := c.CreateRestriction(context.Background(), "", "project", "proj-uuid"); err == nil {
		t.Fatal("expected error for empty contextID")
	}
}

func TestCreateRestriction_EmptyType(t *testing.T) {
	c := &Client{}
	if err := c.CreateRestriction(context.Background(), "ctx-123", "", "some-value"); err == nil {
		t.Fatal("expected error for empty restrictionType")
	}
}

// ---- DeleteRestriction ------------------------------------------------------

func TestDeleteRestriction_DeletePathAndMethod(t *testing.T) {
	const contextID = "ctx-abc-111"
	const restrictionID = "restr-uuid-999"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		wantPath := "/api/v2/context/" + contextID + "/restrictions/" + restrictionID
		if r.URL.Path != wantPath {
			t.Errorf("expected path %s, got %s", wantPath, r.URL.Path)
		}
		// API returns 200 with empty body on successful delete.
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{}`)
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	if err := c.DeleteRestriction(context.Background(), contextID, restrictionID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDeleteRestriction_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"message":"restriction not found"}`)
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	err := c.DeleteRestriction(context.Background(), "ctx-123", "missing-restr-id")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "restriction not found") {
		t.Errorf("error should contain 'restriction not found', got: %v", err)
	}
}

func TestDeleteRestriction_EmptyContextID(t *testing.T) {
	c := &Client{}
	if err := c.DeleteRestriction(context.Background(), "", "restr-uuid"); err == nil {
		t.Fatal("expected error for empty contextID")
	}
}

func TestDeleteRestriction_EmptyRestrictionID(t *testing.T) {
	c := &Client{}
	if err := c.DeleteRestriction(context.Background(), "ctx-123", ""); err == nil {
		t.Fatal("expected error for empty restrictionID")
	}
}
