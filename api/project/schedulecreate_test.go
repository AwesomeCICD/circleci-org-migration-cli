package project

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// ---- CreateSchedule ---------------------------------------------------------

func TestCreateSchedule_HappyPath(t *testing.T) {
	const slug = "gh/acme/web"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		wantPath := "/api/v2/project/gh/acme/web/schedule"
		if r.URL.EscapedPath() != wantPath {
			t.Errorf("path: got %q want %q", r.URL.EscapedPath(), wantPath)
		}

		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["name"] != "nightly" {
			t.Errorf("name: got %v want nightly", body["name"])
		}
		if body["description"] != "Nightly build" {
			t.Errorf("description: got %v", body["description"])
		}
		// attribution-actor must always be "system".
		if body["attribution-actor"] != "system" {
			t.Errorf("attribution-actor: got %v want system", body["attribution-actor"])
		}
		if body["timetable"] == nil {
			t.Error("timetable must be present")
		}
		if body["parameters"] == nil {
			t.Error("parameters must be present")
		}

		respondJSON(w, http.StatusCreated, map[string]any{"id": "sched-uuid-new"})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	timetable := map[string]any{"per-hour": 1, "hours-of-day": []int{2}, "days-of-week": []string{"MON"}}
	params := map[string]any{"branch": "main"}
	if err := c.CreateSchedule(slug, "nightly", "Nightly build", "system", timetable, params); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCreateSchedule_SlugEncoding(t *testing.T) {
	// Verify that a GitHub App-style slug is properly encoded.
	const slug = "gh/my org/my repo"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wantPath := "/api/v2/project/gh/my%20org/my%20repo/schedule"
		if r.URL.EscapedPath() != wantPath {
			t.Errorf("path: got %q want %q", r.URL.EscapedPath(), wantPath)
		}
		respondJSON(w, http.StatusCreated, map[string]any{})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	if err := c.CreateSchedule(slug, "s", "", "system", nil, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCreateSchedule_EmptySlug_Error(t *testing.T) {
	c := &Client{}
	if err := c.CreateSchedule("", "s", "", "system", nil, nil); err == nil {
		t.Fatal("expected error for empty destSlug")
	}
}

func TestCreateSchedule_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusForbidden, map[string]string{"message": "forbidden"})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	if err := c.CreateSchedule("gh/acme/web", "s", "", "system", nil, nil); err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestCreateSchedule_AttributionActorAlwaysSystem(t *testing.T) {
	// Even if the caller passes a non-"system" actor, the body must still say system.
	// (The current implementation ignores the attributionActor param and always uses "system".)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["attribution-actor"] != "system" {
			t.Errorf("attribution-actor: got %v want system", body["attribution-actor"])
		}
		respondJSON(w, http.StatusCreated, map[string]any{})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	// Pass "user" — implementation should override to "system".
	if err := c.CreateSchedule("gh/acme/web", "s", "", "user", nil, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
