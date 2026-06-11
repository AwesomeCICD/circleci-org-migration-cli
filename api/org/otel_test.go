package org

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// ─────────────────────────────────────────────────────────────────────────────
// GetOTelExporters
// ─────────────────────────────────────────────────────────────────────────────

func TestGetOTelExporters_HappyPath(t *testing.T) {
	const orgID = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/api/v2/otel/exporters" {
			t.Errorf("expected path %q, got %q", "/api/v2/otel/exporters", r.URL.Path)
		}
		if got := r.URL.Query().Get("org-id"); got != orgID {
			t.Errorf("org-id query param: got %q want %q", got, orgID)
		}
		respondJSON(w, http.StatusOK, []map[string]any{
			{
				"id":       "exp-1",
				"endpoint": "https://otel.example.com:4318",
				"protocol": "http/protobuf",
				"insecure": false,
				"headers":  map[string]string{"Authorization": "xxxx"},
			},
			{
				"id":       "exp-2",
				"endpoint": "grpc.example.com:4317",
				"protocol": "grpc",
				"insecure": true,
			},
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	exporters, err := c.GetOTelExporters(context.Background(), orgID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(exporters) != 2 {
		t.Fatalf("expected 2 exporters, got %d", len(exporters))
	}

	e0 := exporters[0]
	if e0.ID != "exp-1" {
		t.Errorf("exporters[0].ID: got %q want %q", e0.ID, "exp-1")
	}
	if e0.Endpoint != "https://otel.example.com:4318" {
		t.Errorf("exporters[0].Endpoint: got %q", e0.Endpoint)
	}
	if e0.Protocol != "http/protobuf" {
		t.Errorf("exporters[0].Protocol: got %q want %q", e0.Protocol, "http/protobuf")
	}
	if e0.Insecure {
		t.Error("exporters[0].Insecure should be false")
	}
	if e0.Headers["Authorization"] != "xxxx" {
		t.Errorf("exporters[0].Headers[Authorization]: got %q want xxxx", e0.Headers["Authorization"])
	}

	e1 := exporters[1]
	if !e1.Insecure {
		t.Error("exporters[1].Insecure should be true")
	}
	if e1.Protocol != "grpc" {
		t.Errorf("exporters[1].Protocol: got %q want grpc", e1.Protocol)
	}
}

func TestGetOTelExporters_Empty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusOK, []any{})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	exporters, err := c.GetOTelExporters(context.Background(), "some-org")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(exporters) != 0 {
		t.Errorf("expected empty, got %v", exporters)
	}
}

func TestGetOTelExporters_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusForbidden, map[string]string{"message": "forbidden"})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	_, err := c.GetOTelExporters(context.Background(), "some-org")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// CreateOTelExporter
// ─────────────────────────────────────────────────────────────────────────────

func TestCreateOTelExporter_HappyPath(t *testing.T) {
	const orgID = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	var receivedBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/v2/otel/exporters" {
			t.Errorf("expected path %q, got %q", "/api/v2/otel/exporters", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&receivedBody); err != nil {
			t.Errorf("decode body: %v", err)
		}
		respondJSON(w, http.StatusCreated, map[string]any{"id": "new-exp-id"})
	}))
	defer srv.Close()

	headers := map[string]string{"X-Api-Key": "secret", "X-Trace-Id": "tid"}
	c := newTestClient(t, srv)
	if err := c.CreateOTelExporter(context.Background(), orgID, "https://otel.example.com:4318", "http/protobuf", false, headers); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if receivedBody["org_id"] != orgID {
		t.Errorf("org_id in body: got %v want %q", receivedBody["org_id"], orgID)
	}
	if receivedBody["endpoint"] != "https://otel.example.com:4318" {
		t.Errorf("endpoint in body: got %v", receivedBody["endpoint"])
	}
	if receivedBody["protocol"] != "http/protobuf" {
		t.Errorf("protocol in body: got %v", receivedBody["protocol"])
	}
	if receivedBody["insecure"] != false {
		t.Errorf("insecure in body: got %v want false", receivedBody["insecure"])
	}
	hdrs, ok := receivedBody["headers"].(map[string]any)
	if !ok || len(hdrs) != 2 {
		t.Errorf("headers in body: got %v", receivedBody["headers"])
	}
	if hdrs["X-Api-Key"] != "secret" {
		t.Errorf("headers[X-Api-Key]: got %v want secret", hdrs["X-Api-Key"])
	}
}

func TestCreateOTelExporter_NilHeaders(t *testing.T) {
	// When headers is nil the field must be omitted from the JSON body.
	var receivedBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&receivedBody); err != nil {
			t.Errorf("decode body: %v", err)
		}
		respondJSON(w, http.StatusCreated, map[string]any{})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	if err := c.CreateOTelExporter(context.Background(), "org-id", "https://otel.example.com", "grpc", true, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, hasHeaders := receivedBody["headers"]; hasHeaders {
		t.Error("headers key should be absent when nil (omitempty)")
	}
}

func TestCreateOTelExporter_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusUnprocessableEntity, map[string]string{"message": "invalid"})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	err := c.CreateOTelExporter(context.Background(), "org-id", "", "grpc", false, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}
