package project

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// ─────────────────────────────────────────────────────────────────────────────
// TriggerPipelineRun
// ─────────────────────────────────────────────────────────────────────────────

func TestTriggerPipelineRun_201Created(t *testing.T) {
	const (
		slug     = "gh/acme/web"
		defID    = "def-uuid-1"
		branch   = "main"
		wantID   = "pipeline-uuid-abc"
		wantYAML = "version: 2.1\nworkflows:\n  extract:\n    jobs:\n      - dump\n"
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		wantPath := "/api/v2/project/gh/acme/web/pipeline/run"
		if r.URL.EscapedPath() != wantPath {
			t.Errorf("path: got %q want %q", r.URL.EscapedPath(), wantPath)
		}

		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if got := body["definition_id"]; got != defID {
			t.Errorf("definition_id: got %v want %q", got, defID)
		}
		cfg, _ := body["config"].(map[string]any)
		if cfg["branch"] != branch {
			t.Errorf("config.branch: got %v want %q", cfg["branch"], branch)
		}
		if cfg["content"] != wantYAML {
			t.Errorf("config.content: got %v want %q", cfg["content"], wantYAML)
		}
		checkout, _ := body["checkout"].(map[string]any)
		if checkout["branch"] != branch {
			t.Errorf("checkout.branch: got %v want %q", checkout["branch"], branch)
		}

		respondJSON(w, http.StatusCreated, map[string]any{"id": wantID, "number": 42})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	got, err := c.TriggerPipelineRun(context.Background(), slug, defID, branch, wantYAML, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != wantID {
		t.Errorf("pipelineID: got %q want %q", got, wantID)
	}
}

func TestTriggerPipelineRun_200Skipped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusOK, map[string]string{"message": "no changes detected"})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	id, err := c.TriggerPipelineRun(context.Background(), "gh/acme/web", "def-1", "main", "version: 2.1\n", nil)
	if id != "" {
		t.Errorf("expected empty pipelineID on skip, got %q", id)
	}
	if err != ErrPipelineSkipped {
		t.Errorf("expected ErrPipelineSkipped, got %v", err)
	}
}

func TestTriggerPipelineRun_WithParams(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		params, _ := body["parameters"].(map[string]any)
		if params["run_mode"] != "extract" {
			t.Errorf("parameters.run_mode: got %v want extract", params["run_mode"])
		}
		respondJSON(w, http.StatusCreated, map[string]any{"id": "pipe-1", "number": 1})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	_, err := c.TriggerPipelineRun(context.Background(), "gh/acme/web", "def-1", "main", "v: 2.1\n", map[string]any{"run_mode": "extract"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestTriggerPipelineRun_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusForbidden, map[string]string{"message": "forbidden"})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	_, err := c.TriggerPipelineRun(context.Background(), "gh/acme/web", "def-1", "main", "v: 2.1\n", nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// GetPipelineWorkflows
// ─────────────────────────────────────────────────────────────────────────────

func TestGetPipelineWorkflows_HappyPath(t *testing.T) {
	const pipelineID = "pipe-uuid-1"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		wantPath := "/api/v2/pipeline/" + pipelineID + "/workflow"
		if r.URL.Path != wantPath {
			t.Errorf("path: got %q want %q", r.URL.Path, wantPath)
		}
		respondJSON(w, http.StatusOK, map[string]any{
			"items": []map[string]any{
				{"id": "wf-uuid-1", "name": "extract", "status": "success"},
				{"id": "wf-uuid-2", "name": "notify", "status": "running"},
			},
			"next_page_token": "",
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	got, err := c.GetPipelineWorkflows(context.Background(), pipelineID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 workflows, got %d", len(got))
	}
	if got[0].ID != "wf-uuid-1" || got[0].Name != "extract" || got[0].Status != "success" {
		t.Errorf("first workflow: %+v", got[0])
	}
}

func TestGetPipelineWorkflows_Pagination(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		switch callCount {
		case 1:
			respondJSON(w, http.StatusOK, map[string]any{
				"items":           []map[string]any{{"id": "wf-1", "name": "alpha", "status": "running"}},
				"next_page_token": "wf-tok",
			})
		case 2:
			if r.URL.Query().Get("page-token") != "wf-tok" {
				t.Errorf("page-token: got %q", r.URL.Query().Get("page-token"))
			}
			respondJSON(w, http.StatusOK, map[string]any{
				"items":           []map[string]any{{"id": "wf-2", "name": "beta", "status": "success"}},
				"next_page_token": "",
			})
		default:
			t.Error("unexpected third call")
		}
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	got, err := c.GetPipelineWorkflows(context.Background(), "pipe-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 workflows, got %d", len(got))
	}
}

func TestGetPipelineWorkflows_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusNotFound, map[string]string{"message": "not found"})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	_, err := c.GetPipelineWorkflows(context.Background(), "missing-pipe")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// GetWorkflowJobs
// ─────────────────────────────────────────────────────────────────────────────

func TestGetWorkflowJobs_HappyPath(t *testing.T) {
	const wfID = "wf-uuid-1"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		wantPath := "/api/v2/workflow/" + wfID + "/job"
		if r.URL.Path != wantPath {
			t.Errorf("path: got %q want %q", r.URL.Path, wantPath)
		}
		respondJSON(w, http.StatusOK, map[string]any{
			"items": []map[string]any{
				{"name": "dump-secrets", "job_number": 42, "status": "success"},
			},
			"next_page_token": "",
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	got, err := c.GetWorkflowJobs(context.Background(), wfID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 job, got %d", len(got))
	}
	if got[0].Name != "dump-secrets" || got[0].JobNumber != 42 || got[0].Status != "success" {
		t.Errorf("job: %+v", got[0])
	}
}

func TestGetWorkflowJobs_Pagination(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		switch callCount {
		case 1:
			respondJSON(w, http.StatusOK, map[string]any{
				"items":           []map[string]any{{"name": "job-1", "job_number": 1, "status": "success"}},
				"next_page_token": "job-tok",
			})
		case 2:
			if r.URL.Query().Get("page-token") != "job-tok" {
				t.Errorf("page-token: got %q", r.URL.Query().Get("page-token"))
			}
			respondJSON(w, http.StatusOK, map[string]any{
				"items":           []map[string]any{{"name": "job-2", "job_number": 2, "status": "success"}},
				"next_page_token": "",
			})
		default:
			t.Error("unexpected third call")
		}
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	got, err := c.GetWorkflowJobs(context.Background(), "wf-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 jobs, got %d", len(got))
	}
}

func TestGetWorkflowJobs_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"message": "server error"})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	_, err := c.GetWorkflowJobs(context.Background(), "wf-bad")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ListJobArtifacts
// ─────────────────────────────────────────────────────────────────────────────

func TestListJobArtifacts_HappyPath(t *testing.T) {
	const (
		slug      = "gh/acme/web"
		jobNumber = 99
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		wantPath := "/api/v2/project/gh/acme/web/99/artifacts"
		if r.URL.EscapedPath() != wantPath {
			t.Errorf("path: got %q want %q", r.URL.EscapedPath(), wantPath)
		}
		respondJSON(w, http.StatusOK, map[string]any{
			"items": []map[string]any{
				{"path": "tmp/secrets.json", "node_index": 0, "url": "https://circle-artifacts.com/0/tmp/secrets.json"},
			},
			"next_page_token": "",
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	got, err := c.ListJobArtifacts(context.Background(), slug, jobNumber)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 artifact, got %d", len(got))
	}
	a := got[0]
	if a.Path != "tmp/secrets.json" {
		t.Errorf("Path: got %q", a.Path)
	}
	if a.NodeIndex != 0 {
		t.Errorf("NodeIndex: got %d", a.NodeIndex)
	}
	if a.URL != "https://circle-artifacts.com/0/tmp/secrets.json" {
		t.Errorf("URL: got %q", a.URL)
	}
}

func TestListJobArtifacts_Pagination(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		switch callCount {
		case 1:
			respondJSON(w, http.StatusOK, map[string]any{
				"items":           []map[string]any{{"path": "a.json", "node_index": 0, "url": "https://example.com/a"}},
				"next_page_token": "art-tok",
			})
		case 2:
			if r.URL.Query().Get("page-token") != "art-tok" {
				t.Errorf("page-token: got %q", r.URL.Query().Get("page-token"))
			}
			respondJSON(w, http.StatusOK, map[string]any{
				"items":           []map[string]any{{"path": "b.json", "node_index": 0, "url": "https://example.com/b"}},
				"next_page_token": "",
			})
		default:
			t.Error("unexpected third call")
		}
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	got, err := c.ListJobArtifacts(context.Background(), "gh/acme/web", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 artifacts, got %d", len(got))
	}
}

func TestListJobArtifacts_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusNotFound, map[string]string{"message": "job not found"})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	_, err := c.ListJobArtifacts(context.Background(), "gh/acme/web", 999)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// DownloadArtifact
// ─────────────────────────────────────────────────────────────────────────────

func TestDownloadArtifact_HappyPath(t *testing.T) {
	const wantBody = `{"SECRET_KEY":"s3cret"}`
	wantToken := "test-token"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if got := r.Header.Get("Circle-Token"); got != wantToken {
			t.Errorf("Circle-Token: got %q want %q", got, wantToken)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, wantBody)
	}))
	defer srv.Close()

	c := newTestClientWithToken(t, srv, wantToken)
	got, err := c.DownloadArtifact(context.Background(), srv.URL+"/some/artifact.json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got) != wantBody {
		t.Errorf("body: got %q want %q", string(got), wantBody)
	}
}

func TestDownloadArtifact_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	_, err := c.DownloadArtifact(context.Background(), srv.URL+"/missing.json")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// newTestClientWithToken is like newTestClient but uses an explicit token so
// tests can assert the Circle-Token header is forwarded.
func newTestClientWithToken(t *testing.T, srv *httptest.Server, token string) *Client {
	t.Helper()
	serverURL, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}
	v2Base := serverURL.ResolveReference(&url.URL{Path: "/api/v2/"})
	v11Base := serverURL.ResolveReference(&url.URL{Path: "/api/v1.1/"})
	return newClientFromBases(v2Base, v11Base, token, srv.Client())
}
