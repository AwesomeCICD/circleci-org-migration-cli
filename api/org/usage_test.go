package org

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

const usageOrgID = "aaaaaaaa-bbbb-cccc-dddd-111111111111"

// ─────────────────────────────────────────────────────────────────────────────
// CreateUsageExportJob
// ─────────────────────────────────────────────────────────────────────────────

func TestCreateUsageExportJob_HappyPath(t *testing.T) {
	wantPath := "/api/v2/organizations/" + usageOrgID + "/usage_export_job"
	const wantJobID = "job-uuid-abc123"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != wantPath {
			t.Errorf("expected path %q, got %q", wantPath, r.URL.Path)
		}
		if got := r.Header.Get("Circle-Token"); got != "test-token" {
			t.Errorf("Circle-Token header: got %q want %q", got, "test-token")
		}

		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		if body["start"] != "2026-05-01T00:00:00Z" {
			t.Errorf("start: got %v", body["start"])
		}
		if body["end"] != "2026-05-31T23:59:59Z" {
			t.Errorf("end: got %v", body["end"])
		}

		respondJSON(w, http.StatusOK, map[string]any{
			"usage_export_job_id": wantJobID,
			"state":               "created",
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	jobID, err := c.CreateUsageExportJob(context.Background(), usageOrgID, "2026-05-01T00:00:00Z", "2026-05-31T23:59:59Z")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if jobID != wantJobID {
		t.Errorf("jobID: got %q, want %q", jobID, wantJobID)
	}
}

func TestCreateUsageExportJob_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusBadRequest, map[string]string{"message": "invalid date range"})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	_, err := c.CreateUsageExportJob(context.Background(), usageOrgID, "bad", "bad")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// GetUsageExportJob — pending → completed with download URLs
// ─────────────────────────────────────────────────────────────────────────────

func TestGetUsageExportJob_Pending(t *testing.T) {
	const jobID = "job-uuid-pending"
	wantPath := "/api/v2/organizations/" + usageOrgID + "/usage_export_job/" + jobID

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != wantPath {
			t.Errorf("expected path %q, got %q", wantPath, r.URL.Path)
		}
		respondJSON(w, http.StatusOK, map[string]any{
			"usage_export_job_id": jobID,
			"state":               "processing",
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	state, urls, err := c.GetUsageExportJob(context.Background(), usageOrgID, jobID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state != "processing" {
		t.Errorf("state: got %q, want %q", state, "processing")
	}
	if len(urls) != 0 {
		t.Errorf("expected no download URLs in pending state, got %v", urls)
	}
}

func TestGetUsageExportJob_Completed(t *testing.T) {
	const jobID = "job-uuid-done"
	wantURLs := []string{
		"https://example-bucket.s3.amazonaws.com/usage-1.csv.gz?sig=abc",
		"https://example-bucket.s3.amazonaws.com/usage-2.csv.gz?sig=def",
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusOK, map[string]any{
			"usage_export_job_id": jobID,
			"state":               "completed",
			"download_urls":       wantURLs,
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	state, urls, err := c.GetUsageExportJob(context.Background(), usageOrgID, jobID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state != "completed" {
		t.Errorf("state: got %q, want %q", state, "completed")
	}
	if len(urls) != 2 {
		t.Fatalf("expected 2 download URLs, got %d", len(urls))
	}
	if urls[0] != wantURLs[0] || urls[1] != wantURLs[1] {
		t.Errorf("download URLs: got %v, want %v", urls, wantURLs)
	}
}

func TestGetUsageExportJob_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusNotFound, map[string]string{"message": "job not found"})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	_, _, err := c.GetUsageExportJob(context.Background(), usageOrgID, "missing-job-id")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Full poll sequence: create → processing → completed with URLs
// ─────────────────────────────────────────────────────────────────────────────

func TestUsageExportJob_PollSequence(t *testing.T) {
	const jobID = "job-uuid-poll"
	pollCount := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost:
			// Create call
			respondJSON(w, http.StatusOK, map[string]any{
				"usage_export_job_id": jobID,
				"state":               "created",
			})
		case r.Method == http.MethodGet:
			pollCount++
			if pollCount < 3 {
				// First two polls return processing
				respondJSON(w, http.StatusOK, map[string]any{
					"usage_export_job_id": jobID,
					"state":               "processing",
				})
			} else {
				// Third poll returns completed
				respondJSON(w, http.StatusOK, map[string]any{
					"usage_export_job_id": jobID,
					"state":               "completed",
					"download_urls": []string{
						"https://s3.example.com/usage.csv.gz?token=xyz",
					},
				})
			}
		default:
			t.Errorf("unexpected method %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	c := newTestClient(t, srv)

	// Create
	createdJobID, err := c.CreateUsageExportJob(context.Background(), usageOrgID, "2026-05-01T00:00:00Z", "2026-05-31T23:59:59Z")
	if err != nil {
		t.Fatalf("CreateUsageExportJob: %v", err)
	}
	if createdJobID != jobID {
		t.Errorf("jobID: got %q, want %q", createdJobID, jobID)
	}

	// Poll until completed (simulate the cmd-layer poll loop logic)
	var finalState string
	var downloadURLs []string
	for i := 0; i < 5; i++ {
		state, urls, err := c.GetUsageExportJob(context.Background(), usageOrgID, createdJobID)
		if err != nil {
			t.Fatalf("GetUsageExportJob poll %d: %v", i, err)
		}
		if state == "completed" {
			finalState = state
			downloadURLs = urls
			break
		}
	}

	if finalState != "completed" {
		t.Errorf("final state: got %q, want %q", finalState, "completed")
	}
	if len(downloadURLs) != 1 {
		t.Errorf("expected 1 download URL, got %d", len(downloadURLs))
	}
	if pollCount != 3 {
		t.Errorf("expected 3 GET polls, got %d", pollCount)
	}
}
