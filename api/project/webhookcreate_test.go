package project

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// ---- CreateWebhook ----------------------------------------------------------

func TestCreateWebhook_HappyPath(t *testing.T) {
	const destProjectID = "proj-uuid-abc"
	verifyTLS := true

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		wantPath := "/api/v2/webhook"
		if r.URL.EscapedPath() != wantPath {
			t.Errorf("path: got %q want %q", r.URL.EscapedPath(), wantPath)
		}

		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["name"] != "my-hook" {
			t.Errorf("name: got %v want my-hook", body["name"])
		}
		if body["url"] != "https://hooks.example.com/ci" {
			t.Errorf("url: got %v", body["url"])
		}
		if body["verify-tls"] != true {
			t.Errorf("verify-tls: got %v want true", body["verify-tls"])
		}
		if body["signing-secret"] != "" {
			t.Errorf("signing-secret must be empty string, got %v", body["signing-secret"])
		}
		scope, ok := body["scope"].(map[string]any)
		if !ok {
			t.Fatalf("scope: not present or wrong type: %v", body["scope"])
		}
		if scope["id"] != destProjectID {
			t.Errorf("scope.id: got %v want %v", scope["id"], destProjectID)
		}
		if scope["type"] != "project" {
			t.Errorf("scope.type: got %v want project", scope["type"])
		}
		events, ok := body["events"].([]any)
		if !ok || len(events) != 1 || events[0] != "workflow-completed" {
			t.Errorf("events: got %v", body["events"])
		}

		respondJSON(w, http.StatusCreated, map[string]any{"id": "new-wh-uuid"})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	wh := Webhook{
		Name:      "my-hook",
		URL:       "https://hooks.example.com/ci",
		Events:    []string{"workflow-completed"},
		VerifyTLS: &verifyTLS,
	}
	if err := c.CreateWebhook(context.Background(), destProjectID, wh); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCreateWebhook_VerifyTLSFalse_WhenNil(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		// When VerifyTLS is nil, the wire value should be false.
		if body["verify-tls"] != false {
			t.Errorf("verify-tls: got %v want false when VerifyTLS is nil", body["verify-tls"])
		}
		respondJSON(w, http.StatusCreated, map[string]any{})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	wh := Webhook{Name: "hook", URL: "https://example.com", Events: []string{"job-completed"}, VerifyTLS: nil}
	if err := c.CreateWebhook(context.Background(), "proj-id", wh); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCreateWebhook_EmptyProjectID_Error(t *testing.T) {
	c := &Client{}
	wh := Webhook{Name: "hook", URL: "https://example.com"}
	if err := c.CreateWebhook(context.Background(), "", wh); err == nil {
		t.Fatal("expected error for empty destProjectID")
	}
}

func TestCreateWebhook_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusUnauthorized, map[string]string{"message": "unauthorized"})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	wh := Webhook{Name: "hook", URL: "https://example.com", Events: []string{"workflow-completed"}}
	if err := c.CreateWebhook(context.Background(), "proj-id", wh); err == nil {
		t.Fatal("expected error, got nil")
	}
}
