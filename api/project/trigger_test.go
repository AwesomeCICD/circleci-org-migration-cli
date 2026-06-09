package project

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// ---- ListTriggers -----------------------------------------------------------

func TestListTriggers_GithubApp(t *testing.T) {
	// Tests the github_app event_source variant.
	const (
		projectID = "proj-uuid-abc"
		defID     = "def-uuid-xyz"
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		wantPath := "/api/v2/projects/" + projectID + "/pipeline-definitions/" + defID + "/triggers"
		if r.URL.Path != wantPath {
			t.Errorf("path: got %q want %q", r.URL.Path, wantPath)
		}
		respondJSON(w, http.StatusOK, map[string]interface{}{
			"items": []map[string]interface{}{
				{
					"id":           "trig-1",
					"name":         "push trigger",
					"event_name":   "push",
					"description":  "fires on push",
					"created_at":   "2024-01-01T00:00:00Z",
					"checkout_ref": "main",
					"config_ref":   "HEAD",
					"event_preset": "default",
					"disabled":     false,
					"event_source": map[string]interface{}{
						"provider": "github_app",
						"repo": map[string]string{
							"full_name":   "acme/web",
							"external_id": "repo-ext-1",
						},
					},
				},
			},
			"next_page_token": "",
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	got, err := c.ListTriggers(projectID, defID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 trigger, got %d", len(got))
	}
	trig := got[0]
	if trig.ID != "trig-1" {
		t.Errorf("ID: got %q want trig-1", trig.ID)
	}
	if trig.Name != "push trigger" {
		t.Errorf("Name: got %q want push trigger", trig.Name)
	}
	if trig.EventName != "push" {
		t.Errorf("EventName: got %q want push", trig.EventName)
	}
	if trig.CheckoutRef != "main" {
		t.Errorf("CheckoutRef: got %q want main", trig.CheckoutRef)
	}
	if trig.ConfigRef != "HEAD" {
		t.Errorf("ConfigRef: got %q want HEAD", trig.ConfigRef)
	}
	if trig.EventPreset != "default" {
		t.Errorf("EventPreset: got %q want default", trig.EventPreset)
	}
	if trig.Disabled {
		t.Error("Disabled: expected false")
	}
	if trig.EventSource.Provider != "github_app" {
		t.Errorf("EventSource.Provider: got %q want github_app", trig.EventSource.Provider)
	}
	if trig.EventSource.Repo.FullName != "acme/web" {
		t.Errorf("EventSource.Repo.FullName: got %q want acme/web", trig.EventSource.Repo.FullName)
	}
	if trig.EventSource.Repo.ExternalID != "repo-ext-1" {
		t.Errorf("EventSource.Repo.ExternalID: got %q want repo-ext-1", trig.EventSource.Repo.ExternalID)
	}
}

func TestListTriggers_Webhook(t *testing.T) {
	// Tests the webhook event_source variant (URL contains ?secret=**REDACTED**).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusOK, map[string]interface{}{
			"items": []map[string]interface{}{
				{
					"id":       "trig-wh",
					"name":     "webhook trigger",
					"disabled": false,
					"event_source": map[string]interface{}{
						"provider": "webhook",
						"webhook": map[string]string{
							"url":    "https://circleci.com/hooks/...?secret=**REDACTED**",
							"sender": "bot-user",
						},
					},
				},
			},
			"next_page_token": "",
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	got, err := c.ListTriggers("proj-id", "def-id")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 trigger, got %d", len(got))
	}
	trig := got[0]
	if trig.EventSource.Provider != "webhook" {
		t.Errorf("Provider: got %q want webhook", trig.EventSource.Provider)
	}
	// The URL field is present in the raw API response and captured on the
	// Trigger struct; it is the exporter's job to not store it in the manifest.
	if trig.EventSource.Webhook.Sender != "bot-user" {
		t.Errorf("Webhook.Sender: got %q want bot-user", trig.EventSource.Webhook.Sender)
	}
}

func TestListTriggers_Schedule(t *testing.T) {
	// Tests the schedule event_source variant.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusOK, map[string]interface{}{
			"items": []map[string]interface{}{
				{
					"id":       "trig-sched",
					"name":     "nightly",
					"disabled": false,
					"event_source": map[string]interface{}{
						"provider": "schedule",
						"schedule": map[string]string{
							"cron_expression":   "0 2 * * *",
							"attribution_actor": "system",
						},
					},
				},
			},
			"next_page_token": "",
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	got, err := c.ListTriggers("proj-id", "def-id")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 trigger, got %d", len(got))
	}
	trig := got[0]
	if trig.EventSource.Provider != "schedule" {
		t.Errorf("Provider: got %q want schedule", trig.EventSource.Provider)
	}
	if trig.EventSource.Schedule.CronExpression != "0 2 * * *" {
		t.Errorf("Schedule.CronExpression: got %q want '0 2 * * *'", trig.EventSource.Schedule.CronExpression)
	}
	if trig.EventSource.Schedule.AttributionActor != "system" {
		t.Errorf("Schedule.AttributionActor: got %q want system", trig.EventSource.Schedule.AttributionActor)
	}
}

func TestListTriggers_Disabled(t *testing.T) {
	// Disabled=true must be deserialized correctly.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusOK, map[string]interface{}{
			"items": []map[string]interface{}{
				{
					"id":       "trig-dis",
					"name":     "disabled one",
					"disabled": true,
					"event_source": map[string]interface{}{
						"provider": "github_app",
					},
				},
			},
			"next_page_token": "",
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	got, err := c.ListTriggers("p", "d")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1, got %d", len(got))
	}
	if !got[0].Disabled {
		t.Error("Disabled: expected true")
	}
}

func TestListTriggers_Pagination(t *testing.T) {
	page1 := map[string]interface{}{
		"items": []map[string]interface{}{
			{"id": "t1", "name": "alpha", "disabled": false, "event_source": map[string]interface{}{"provider": "github_app"}},
		},
		"next_page_token": "trig-tok",
	}
	page2 := map[string]interface{}{
		"items": []map[string]interface{}{
			{"id": "t2", "name": "beta", "disabled": false, "event_source": map[string]interface{}{"provider": "webhook"}},
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
			if got := r.URL.Query().Get("page-token"); got != "trig-tok" {
				t.Errorf("page-token: got %q want trig-tok", got)
			}
			respondJSON(w, http.StatusOK, page2)
		default:
			t.Error("unexpected third call")
		}
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	got, err := c.ListTriggers("proj-id", "def-id")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if callCount != 2 {
		t.Errorf("expected 2 calls, got %d", callCount)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 triggers, got %d", len(got))
	}
}

func TestListTriggers_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusNotFound, map[string]string{"message": "definition not found"})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	_, err := c.ListTriggers("proj-id", "def-id")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}
