package org

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

const budgetOrgUUID = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"

// ─────────────────────────────────────────────────────────────────────────────
// GetBudgets
// ─────────────────────────────────────────────────────────────────────────────

func TestGetBudgets_HappyPath(t *testing.T) {
	wantPath := "/private/orgs/" + budgetOrgUUID + "/budgets"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != wantPath {
			t.Errorf("expected path %q, got %q", wantPath, r.URL.Path)
		}
		if got := r.Header.Get("Circle-Token"); got != "test-token" {
			t.Errorf("Circle-Token header: got %q want test-token", got)
		}
		respondJSON(w, http.StatusOK, map[string]any{
			"budgets": []map[string]any{
				{
					"credits":            1000000,
					"budget_id":          "budget-uuid-1",
					"enforcement_type":   "warn",
					"project_id":         nil,
					"consumption":        0,
					"percentage":         0.0,
					"threshold_exceeded": false,
				},
				{
					"credits":            50000,
					"budget_id":          "budget-uuid-2",
					"enforcement_type":   "block",
					"project_id":         "proj-uuid-1",
					"consumption":        1000,
					"percentage":         2.0,
					"threshold_exceeded": false,
				},
			},
		})
	}))
	defer srv.Close()

	c := newTestClientWithAppServer(t, srv)
	budgets, err := c.GetBudgets(budgetOrgUUID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(budgets) != 2 {
		t.Fatalf("expected 2 budgets, got %d", len(budgets))
	}

	// Org-level budget (project_id == nil)
	org := budgets[0]
	if org.Credits != 1000000 {
		t.Errorf("org budget credits: got %d want 1000000", org.Credits)
	}
	if org.EnforcementType != "warn" {
		t.Errorf("org budget enforcement_type: got %q want %q", org.EnforcementType, "warn")
	}
	if org.ProjectID != nil {
		t.Errorf("org budget project_id: got %v want nil", org.ProjectID)
	}

	// Per-project budget
	proj := budgets[1]
	if proj.Credits != 50000 {
		t.Errorf("project budget credits: got %d want 50000", proj.Credits)
	}
	if proj.ProjectID == nil || *proj.ProjectID != "proj-uuid-1" {
		t.Errorf("project budget project_id: got %v want %q", proj.ProjectID, "proj-uuid-1")
	}
}

func TestGetBudgets_Empty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusOK, map[string]any{"budgets": []any{}})
	}))
	defer srv.Close()

	c := newTestClientWithAppServer(t, srv)
	budgets, err := c.GetBudgets(budgetOrgUUID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(budgets) != 0 {
		t.Errorf("expected empty budgets, got %v", budgets)
	}
}

func TestGetBudgets_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusForbidden, map[string]string{"message": "forbidden"})
	}))
	defer srv.Close()

	c := newTestClientWithAppServer(t, srv)
	_, err := c.GetBudgets(budgetOrgUUID)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// SetBudget
// ─────────────────────────────────────────────────────────────────────────────

func TestSetBudget_OrgLevel(t *testing.T) {
	wantPath := "/private/orgs/" + budgetOrgUUID + "/budgets"
	var receivedBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("expected PUT, got %s", r.Method)
		}
		if r.URL.Path != wantPath {
			t.Errorf("expected path %q, got %q", wantPath, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&receivedBody); err != nil {
			t.Errorf("decode body: %v", err)
		}
		respondJSON(w, http.StatusOK, map[string]any{})
	}))
	defer srv.Close()

	c := newTestClientWithAppServer(t, srv)
	if err := c.SetBudget(budgetOrgUUID, nil, 2000000); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if v, ok := receivedBody["credits"]; !ok || v != float64(2000000) {
		t.Errorf("credits in body: got %v (ok=%v), want 2000000", v, ok)
	}
	// project_id must be present and null for the org-level budget.
	if _, ok := receivedBody["project_id"]; !ok {
		t.Error("project_id key must be present in body")
	}
	if receivedBody["project_id"] != nil {
		t.Errorf("project_id must be null for org-level budget, got %v", receivedBody["project_id"])
	}
}

func TestSetBudget_ProjectLevel(t *testing.T) {
	var receivedBody map[string]any
	projID := "proj-uuid-99"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("expected PUT, got %s", r.Method)
		}
		if err := json.NewDecoder(r.Body).Decode(&receivedBody); err != nil {
			t.Errorf("decode body: %v", err)
		}
		respondJSON(w, http.StatusOK, map[string]any{})
	}))
	defer srv.Close()

	c := newTestClientWithAppServer(t, srv)
	if err := c.SetBudget(budgetOrgUUID, &projID, 75000); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if v, ok := receivedBody["credits"]; !ok || v != float64(75000) {
		t.Errorf("credits: got %v (ok=%v), want 75000", v, ok)
	}
	if receivedBody["project_id"] != projID {
		t.Errorf("project_id: got %v want %q", receivedBody["project_id"], projID)
	}
}

func TestSetBudget_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusBadRequest, map[string]string{"message": "invalid"})
	}))
	defer srv.Close()

	c := newTestClientWithAppServer(t, srv)
	if err := c.SetBudget(budgetOrgUUID, nil, 0); err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// DeleteBudget
// ─────────────────────────────────────────────────────────────────────────────

func TestDeleteBudget_HappyPath(t *testing.T) {
	const budgetID = "budget-uuid-to-delete"
	wantPath := "/private/orgs/" + budgetOrgUUID + "/budgets/" + budgetID

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		if r.URL.Path != wantPath {
			t.Errorf("expected path %q, got %q", wantPath, r.URL.Path)
		}
		respondJSON(w, http.StatusOK, map[string]any{})
	}))
	defer srv.Close()

	c := newTestClientWithAppServer(t, srv)
	if err := c.DeleteBudget(budgetOrgUUID, budgetID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDeleteBudget_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusNotFound, map[string]string{"message": "not found"})
	}))
	defer srv.Close()

	c := newTestClientWithAppServer(t, srv)
	if err := c.DeleteBudget(budgetOrgUUID, "missing-id"); err == nil {
		t.Fatal("expected error, got nil")
	}
}
