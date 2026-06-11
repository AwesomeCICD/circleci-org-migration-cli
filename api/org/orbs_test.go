package org

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

const orbsOrgUUID = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"

// ─────────────────────────────────────────────────────────────────────────────
// GetOrgOrbs — single page
// ─────────────────────────────────────────────────────────────────────────────

func TestGetOrgOrbs_SinglePage(t *testing.T) {
	wantPath := "/api/private/orb"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != wantPath {
			t.Errorf("expected path %q, got %q", wantPath, r.URL.Path)
		}
		if got := r.URL.Query().Get("org-id"); got != orbsOrgUUID {
			t.Errorf("org-id query param: got %q want %q", got, orbsOrgUUID)
		}
		if got := r.Header.Get("Circle-Token"); got != "test-token" {
			t.Errorf("Circle-Token header: got %q want test-token", got)
		}
		respondJSON(w, http.StatusOK, map[string]any{
			"orbs": []map[string]any{
				{
					"orb_name":              "acme/my-orb",
					"latest_version_number": "0.3.0",
					"orb_id":                "orb-uuid-1",
					"is_private":            true,
					"hidden":                false,
					"description":           "A custom orb",
				},
				{
					"orb_name":              "acme/other-orb",
					"latest_version_number": "1.0.0",
					"orb_id":                "orb-uuid-2",
					"is_private":            false,
					"hidden":                true,
					"description":           "",
				},
			},
			"next_page_token": nil,
		})
	}))
	defer srv.Close()

	c := newTestClientWithAppServer(t, srv)
	orbs, err := c.GetOrgOrbs(context.Background(), orbsOrgUUID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(orbs) != 2 {
		t.Fatalf("expected 2 orbs, got %d", len(orbs))
	}
	if orbs[0].OrbName != "acme/my-orb" || orbs[0].LatestVersionNumber != "0.3.0" {
		t.Errorf("unexpected orb[0]: %+v", orbs[0])
	}
	if !orbs[0].IsPrivate {
		t.Error("expected orbs[0].IsPrivate=true")
	}
	if orbs[0].Description != "A custom orb" {
		t.Errorf("orbs[0].Description: got %q want %q", orbs[0].Description, "A custom orb")
	}
	if orbs[1].OrbName != "acme/other-orb" || !orbs[1].Hidden {
		t.Errorf("unexpected orb[1]: %+v", orbs[1])
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// GetOrgOrbs — pagination (two pages)
// ─────────────────────────────────────────────────────────────────────────────

func TestGetOrgOrbs_Pagination(t *testing.T) {
	callCount := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		pageToken := r.URL.Query().Get("page-token")

		switch callCount {
		case 1:
			// First page — no page-token in query.
			if pageToken != "" {
				t.Errorf("call 1: expected no page-token, got %q", pageToken)
			}
			respondJSON(w, http.StatusOK, map[string]any{
				"orbs": []map[string]any{
					{"orb_name": "acme/orb-a", "latest_version_number": "1.0.0", "orb_id": "id-a", "is_private": false, "hidden": false},
				},
				"next_page_token": "tok-page2",
			})
		case 2:
			// Second page — must carry the page-token from the first response.
			if pageToken != "tok-page2" {
				t.Errorf("call 2: expected page-token %q, got %q", "tok-page2", pageToken)
			}
			respondJSON(w, http.StatusOK, map[string]any{
				"orbs": []map[string]any{
					{"orb_name": "acme/orb-b", "latest_version_number": "2.0.0", "orb_id": "id-b", "is_private": true, "hidden": false},
				},
				"next_page_token": nil,
			})
		default:
			t.Errorf("unexpected call %d", callCount)
			respondJSON(w, http.StatusOK, map[string]any{"orbs": []any{}, "next_page_token": nil})
		}
	}))
	defer srv.Close()

	c := newTestClientWithAppServer(t, srv)
	orbs, err := c.GetOrgOrbs(context.Background(), orbsOrgUUID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if callCount != 2 {
		t.Errorf("expected 2 HTTP calls, got %d", callCount)
	}
	if len(orbs) != 2 {
		t.Fatalf("expected 2 orbs (1 per page), got %d", len(orbs))
	}
	if orbs[0].OrbName != "acme/orb-a" || orbs[1].OrbName != "acme/orb-b" {
		t.Errorf("unexpected orbs: %+v", orbs)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// GetOrgOrbs — empty response
// ─────────────────────────────────────────────────────────────────────────────

func TestGetOrgOrbs_Empty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusOK, map[string]any{
			"orbs":            []any{},
			"next_page_token": nil,
		})
	}))
	defer srv.Close()

	c := newTestClientWithAppServer(t, srv)
	orbs, err := c.GetOrgOrbs(context.Background(), orbsOrgUUID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(orbs) != 0 {
		t.Errorf("expected empty slice, got %v", orbs)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// GetOrgOrbs — server error
// ─────────────────────────────────────────────────────────────────────────────

func TestGetOrgOrbs_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusForbidden, map[string]string{"message": "forbidden"})
	}))
	defer srv.Close()

	c := newTestClientWithAppServer(t, srv)
	if _, err := c.GetOrgOrbs(context.Background(), orbsOrgUUID); err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// GetOrgOrbs — pagination stops on empty orbs list (guards infinite loop)
// ─────────────────────────────────────────────────────────────────────────────

func TestGetOrgOrbs_PaginationStopsOnEmptyOrbs(t *testing.T) {
	callCount := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		// Simulate a pathological server that keeps returning a non-empty
		// next_page_token but an empty orbs list on the second call.
		if callCount == 1 {
			respondJSON(w, http.StatusOK, map[string]any{
				"orbs":            []map[string]any{{"orb_name": "acme/x", "orb_id": "x", "latest_version_number": "0.1.0"}},
				"next_page_token": "tok-never-ends",
			})
			return
		}
		respondJSON(w, http.StatusOK, map[string]any{
			"orbs":            []any{},
			"next_page_token": "tok-never-ends",
		})
	}))
	defer srv.Close()

	c := newTestClientWithAppServer(t, srv)
	orbs, err := c.GetOrgOrbs(context.Background(), orbsOrgUUID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if callCount != 2 {
		t.Errorf("expected 2 HTTP calls, got %d", callCount)
	}
	if len(orbs) != 1 {
		t.Errorf("expected 1 orb, got %d", len(orbs))
	}
}

// Ensure OrbID and Statistics fields: confirm statistics are NOT in the struct.
func TestGetOrgOrbs_FieldsCaptured(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Include a "statistics" field in the response to confirm it is ignored.
		raw := `{"orbs":[{"orb_name":"acme/stat-orb","orb_id":"stat-id","latest_version_number":"0.1.0","is_private":true,"hidden":false,"description":"desc","statistics":{"usage":99}}],"next_page_token":null}`
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(raw))
	}))
	defer srv.Close()

	c := newTestClientWithAppServer(t, srv)
	orbs, err := c.GetOrgOrbs(context.Background(), orbsOrgUUID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(orbs) != 1 {
		t.Fatalf("expected 1 orb, got %d", len(orbs))
	}
	o := orbs[0]
	if o.OrbID != "stat-id" {
		t.Errorf("OrbID: got %q want %q", o.OrbID, "stat-id")
	}
	// Confirm the struct has no Statistics field by round-tripping through JSON.
	data, _ := json.Marshal(o)
	var m map[string]any
	_ = json.Unmarshal(data, &m)
	if _, hasStats := m["statistics"]; hasStats {
		t.Error("statistics field must not be present in captured OrgOrb")
	}
}
