package project

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// ---- ListSchedules actor field ------------------------------------------------

// TestListSchedules_ActorLoginCaptured verifies that the actor.login field in
// the GET /api/v2/project/{slug}/schedule response is decoded into the Actor
// field on each Schedule.
func TestListSchedules_ActorLoginCaptured(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		respondJSON(w, http.StatusOK, map[string]any{
			"items": []map[string]any{
				{
					"id":          "sched-uuid-1",
					"name":        "nightly",
					"description": "Nightly build",
					"timetable":   map[string]any{"per-hour": 1},
					"parameters":  map[string]any{"branch": "main"},
					"actor": map[string]any{
						"login":      "bot-user",
						"avatar_url": "https://example.com/avatar.png",
					},
				},
			},
			"next_page_token": "",
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	scheds, err := c.ListSchedules("gh/acme/web")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(scheds) != 1 {
		t.Fatalf("expected 1 schedule, got %d", len(scheds))
	}
	if scheds[0].Actor.Login != "bot-user" {
		t.Errorf("Actor.Login: got %q want %q", scheds[0].Actor.Login, "bot-user")
	}
}

// TestListSchedules_ActorLoginEmpty verifies that a schedule with no actor in
// the response decodes without error and leaves Actor.Login empty.
func TestListSchedules_ActorLoginEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusOK, map[string]any{
			"items": []map[string]any{
				{
					"id":          "sched-uuid-2",
					"name":        "weekly",
					"description": "",
					"timetable":   map[string]any{},
					"parameters":  map[string]any{},
					// actor field intentionally absent
				},
			},
			"next_page_token": "",
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	scheds, err := c.ListSchedules("gh/acme/web")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(scheds) != 1 {
		t.Fatalf("expected 1 schedule, got %d", len(scheds))
	}
	if scheds[0].Actor.Login != "" {
		t.Errorf("Actor.Login should be empty when absent from response, got %q", scheds[0].Actor.Login)
	}
}

// TestListSchedules_MultiplePages_ActorCapturedAcrossPages verifies that actor
// login is captured correctly across paginated responses.
func TestListSchedules_MultiplePages_ActorCapturedAcrossPages(t *testing.T) {
	page := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page++
		if page == 1 {
			respondJSON(w, http.StatusOK, map[string]any{
				"items": []map[string]any{
					{
						"id":    "s1",
						"name":  "first",
						"actor": map[string]any{"login": "alice"},
					},
				},
				"next_page_token": "tok2",
			})
		} else {
			respondJSON(w, http.StatusOK, map[string]any{
				"items": []map[string]any{
					{
						"id":    "s2",
						"name":  "second",
						"actor": map[string]any{"login": "bob"},
					},
				},
				"next_page_token": "",
			})
		}
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	scheds, err := c.ListSchedules("gh/acme/web")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(scheds) != 2 {
		t.Fatalf("expected 2 schedules, got %d", len(scheds))
	}
	if scheds[0].Actor.Login != "alice" {
		t.Errorf("page1 Actor.Login: got %q want alice", scheds[0].Actor.Login)
	}
	if scheds[1].Actor.Login != "bob" {
		t.Errorf("page2 Actor.Login: got %q want bob", scheds[1].Actor.Login)
	}
}
