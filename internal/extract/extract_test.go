package extract

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/CircleCI-Public/circleci-org-migration-cli/api/project"
)

// ─────────────────────────────────────────────────────────────────────────────
// buildExtractConfig unit tests
// ─────────────────────────────────────────────────────────────────────────────

func TestBuildExtractConfig_ContainsJobName(t *testing.T) {
	cfg := buildExtractConfig([]string{"SECRET_KEY"}, nil)
	if !strings.Contains(cfg, dumpJobName) {
		t.Errorf("config does not contain job name %q:\n%s", dumpJobName, cfg)
	}
}

func TestBuildExtractConfig_ContainsImage(t *testing.T) {
	cfg := buildExtractConfig([]string{"FOO"}, nil)
	if !strings.Contains(cfg, "cimg/base:current") {
		t.Errorf("config does not contain expected image:\n%s", cfg)
	}
}

func TestBuildExtractConfig_ContainsResourceClassSmall(t *testing.T) {
	cfg := buildExtractConfig([]string{"FOO"}, nil)
	if !strings.Contains(cfg, "resource_class: small") {
		t.Errorf("config does not contain resource_class: small:\n%s", cfg)
	}
}

func TestBuildExtractConfig_ContainsStoreArtifacts(t *testing.T) {
	cfg := buildExtractConfig([]string{"FOO"}, nil)
	if !strings.Contains(cfg, "store_artifacts") {
		t.Errorf("config missing store_artifacts step:\n%s", cfg)
	}
	if !strings.Contains(cfg, artifactPath) {
		t.Errorf("config missing artifact path %q:\n%s", artifactPath, cfg)
	}
}

func TestBuildExtractConfig_ContainsEnvVarNames(t *testing.T) {
	vars := []string{"SECRET_KEY", "DB_PASS", "API_TOKEN"}
	cfg := buildExtractConfig(vars, nil)
	for _, v := range vars {
		if !strings.Contains(cfg, v) {
			t.Errorf("config missing env var %q:\n%s", v, cfg)
		}
	}
}

func TestBuildExtractConfig_ContainsContextNames(t *testing.T) {
	ctxs := []string{"deploy-prod", "staging-creds"}
	cfg := buildExtractConfig([]string{"FOO"}, ctxs)
	for _, c := range ctxs {
		if !strings.Contains(cfg, c) {
			t.Errorf("config missing context %q:\n%s", c, cfg)
		}
	}
	if !strings.Contains(cfg, "context:") {
		t.Errorf("config missing 'context:' block:\n%s", cfg)
	}
}

func TestBuildExtractConfig_NoContextNames_NoContextBlock(t *testing.T) {
	cfg := buildExtractConfig([]string{"FOO"}, nil)
	if strings.Contains(cfg, "context:") {
		t.Errorf("config should not have 'context:' when no contexts provided:\n%s", cfg)
	}
}

func TestBuildExtractConfig_IsValidYAMLish(t *testing.T) {
	cfg := buildExtractConfig([]string{"A", "B"}, []string{"ctx-1"})
	// Must start with version line.
	if !strings.HasPrefix(cfg, "version: 2.1") {
		t.Errorf("config must start with 'version: 2.1':\n%s", cfg)
	}
	// Must have jobs and workflows sections.
	if !strings.Contains(cfg, "jobs:") {
		t.Errorf("config missing 'jobs:' section:\n%s", cfg)
	}
	if !strings.Contains(cfg, "workflows:") {
		t.Errorf("config missing 'workflows:' section:\n%s", cfg)
	}
}

func TestBuildExtractConfig_EmptyVarList(t *testing.T) {
	// Should not panic; produces a valid (if useless) config.
	cfg := buildExtractConfig(nil, nil)
	if !strings.Contains(cfg, dumpJobName) {
		t.Errorf("config does not contain job name with empty var list:\n%s", cfg)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Fake Deps for Capture tests
// ─────────────────────────────────────────────────────────────────────────────

type fakeDeps struct {
	// TriggerPipelineRun behaviour
	triggerPipelineID string
	triggerErr        error

	// GetPipelineWorkflows behaviour: list of responses returned sequentially
	workflowResponses [][]project.Workflow
	workflowErrs      []error
	workflowCallIdx   int

	// GetWorkflowJobs behaviour
	jobsResult []project.Job
	jobsErr    error

	// ListJobArtifacts behaviour
	artifactsResult []project.Artifact
	artifactsErr    error

	// DownloadArtifact behaviour
	downloadData []byte
	downloadErr  error
}

func (f *fakeDeps) TriggerPipelineRun(_, _, _, _ string, _ map[string]any) (string, error) {
	return f.triggerPipelineID, f.triggerErr
}

func (f *fakeDeps) GetPipelineWorkflows(_ string) ([]project.Workflow, error) {
	if f.workflowCallIdx >= len(f.workflowResponses) {
		return nil, nil
	}
	idx := f.workflowCallIdx
	f.workflowCallIdx++
	var err error
	if idx < len(f.workflowErrs) {
		err = f.workflowErrs[idx]
	}
	return f.workflowResponses[idx], err
}

func (f *fakeDeps) GetWorkflowJobs(_ string) ([]project.Job, error) {
	return f.jobsResult, f.jobsErr
}

func (f *fakeDeps) ListJobArtifacts(_ string, _ int) ([]project.Artifact, error) {
	return f.artifactsResult, f.artifactsErr
}

func (f *fakeDeps) DownloadArtifact(_ string) ([]byte, error) {
	return f.downloadData, f.downloadErr
}

// ─────────────────────────────────────────────────────────────────────────────
// Capture happy-path tests
// ─────────────────────────────────────────────────────────────────────────────

// secretPayload builds the JSON artifact body.
func secretPayload(t *testing.T, vals map[string]string) []byte {
	t.Helper()
	data, err := json.Marshal(vals)
	if err != nil {
		t.Fatalf("marshal secret payload: %v", err)
	}
	return data
}

func TestCapture_HappyPath(t *testing.T) {
	want := map[string]string{
		"SECRET_KEY": "s3cret",
		"DB_PASS":    "hunter2",
	}

	deps := &fakeDeps{
		triggerPipelineID: "pipe-1",
		workflowResponses: [][]project.Workflow{
			// First poll: still running
			{{ID: "wf-1", Name: "extract", Status: "running"}},
			// Second poll: success
			{{ID: "wf-1", Name: "extract", Status: "success"}},
		},
		jobsResult: []project.Job{
			{Name: dumpJobName, JobNumber: 42, Status: "success"},
		},
		artifactsResult: []project.Artifact{
			{Path: artifactPath, NodeIndex: 0, URL: "https://circle-artifacts.com/0/circleci-migrate-secrets.json"},
		},
		downloadData: secretPayload(t, want),
	}

	got, err := Capture(
		context.Background(),
		deps,
		"gh/acme/web",
		[]string{"SECRET_KEY", "DB_PASS"},
		nil,
		Options{DefinitionID: "def-1", Branch: "main", PollInterval: time.Millisecond},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("key %q: got %q want %q", k, got[k], v)
		}
	}
}

func TestCapture_MissingDefinitionID(t *testing.T) {
	_, err := Capture(context.Background(), &fakeDeps{}, "gh/acme/web", nil, nil, Options{})
	if err == nil {
		t.Fatal("expected error when DefinitionID is empty, got nil")
	}
	if !strings.Contains(err.Error(), "DefinitionID") {
		t.Errorf("error %q does not mention DefinitionID", err.Error())
	}
}

func TestCapture_TriggerError(t *testing.T) {
	deps := &fakeDeps{
		triggerErr: fmt.Errorf("network error"),
	}
	_, err := Capture(context.Background(), deps, "gh/acme/web", nil, nil, Options{DefinitionID: "def-1"})
	if err == nil {
		t.Fatal("expected trigger error, got nil")
	}
}

func TestCapture_PipelineSkipped(t *testing.T) {
	deps := &fakeDeps{
		triggerErr: project.ErrPipelineSkipped,
	}
	_, err := Capture(context.Background(), deps, "gh/acme/web", nil, nil, Options{DefinitionID: "def-1"})
	if err == nil {
		t.Fatal("expected error on skipped pipeline, got nil")
	}
}

func TestCapture_WorkflowFailed(t *testing.T) {
	deps := &fakeDeps{
		triggerPipelineID: "pipe-1",
		workflowResponses: [][]project.Workflow{
			{{ID: "wf-1", Name: "extract", Status: "failed"}},
		},
	}
	_, err := Capture(
		context.Background(), deps, "gh/acme/web", nil, nil,
		Options{DefinitionID: "def-1", PollInterval: time.Millisecond},
	)
	if err == nil {
		t.Fatal("expected error on failed workflow, got nil")
	}
	if !strings.Contains(err.Error(), "failed") {
		t.Errorf("error %q does not mention 'failed'", err.Error())
	}
}

func TestCapture_NoArtifact(t *testing.T) {
	deps := &fakeDeps{
		triggerPipelineID: "pipe-1",
		workflowResponses: [][]project.Workflow{
			{{ID: "wf-1", Name: "extract", Status: "success"}},
		},
		jobsResult:      []project.Job{{Name: dumpJobName, JobNumber: 1, Status: "success"}},
		artifactsResult: []project.Artifact{}, // empty — no artifact
	}
	_, err := Capture(
		context.Background(), deps, "gh/acme/web", nil, nil,
		Options{DefinitionID: "def-1", PollInterval: time.Millisecond},
	)
	if !strings.Contains(err.Error(), "no secrets artifact") {
		t.Errorf("expected ErrNoArtifact, got: %v", err)
	}
}

func TestCapture_PollTimeout(t *testing.T) {
	// Every workflow call returns "running"; the timeout should fire.
	deps := &fakeDeps{
		triggerPipelineID: "pipe-1",
		// Return "running" indefinitely by making the slice very long.
		workflowResponses: func() [][]project.Workflow {
			s := make([][]project.Workflow, 100)
			for i := range s {
				s[i] = []project.Workflow{{ID: "wf-1", Name: "extract", Status: "running"}}
			}
			return s
		}(),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := Capture(ctx, deps, "gh/acme/web", nil, nil,
		Options{DefinitionID: "def-1", PollInterval: 5 * time.Millisecond})
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
}

func TestCapture_JobNotFound(t *testing.T) {
	deps := &fakeDeps{
		triggerPipelineID: "pipe-1",
		workflowResponses: [][]project.Workflow{
			{{ID: "wf-1", Name: "extract", Status: "success"}},
		},
		jobsResult: []project.Job{
			{Name: "some-other-job", JobNumber: 1, Status: "success"},
		},
	}
	_, err := Capture(
		context.Background(), deps, "gh/acme/web", nil, nil,
		Options{DefinitionID: "def-1", PollInterval: time.Millisecond},
	)
	if err == nil {
		t.Fatal("expected error when dump job not found, got nil")
	}
}

func TestCapture_ArtifactDownloadError(t *testing.T) {
	deps := &fakeDeps{
		triggerPipelineID: "pipe-1",
		workflowResponses: [][]project.Workflow{
			{{ID: "wf-1", Name: "extract", Status: "success"}},
		},
		jobsResult: []project.Job{{Name: dumpJobName, JobNumber: 1, Status: "success"}},
		artifactsResult: []project.Artifact{
			{Path: artifactPath, URL: "https://circle-artifacts.com/0/circleci-migrate-secrets.json"},
		},
		downloadErr: fmt.Errorf("download failed"),
	}
	_, err := Capture(
		context.Background(), deps, "gh/acme/web", nil, nil,
		Options{DefinitionID: "def-1", PollInterval: time.Millisecond},
	)
	if err == nil {
		t.Fatal("expected download error, got nil")
	}
}

func TestCapture_BadArtifactJSON(t *testing.T) {
	deps := &fakeDeps{
		triggerPipelineID: "pipe-1",
		workflowResponses: [][]project.Workflow{
			{{ID: "wf-1", Name: "extract", Status: "success"}},
		},
		jobsResult: []project.Job{{Name: dumpJobName, JobNumber: 1, Status: "success"}},
		artifactsResult: []project.Artifact{
			{Path: artifactPath, URL: "https://circle-artifacts.com/0/circleci-migrate-secrets.json"},
		},
		downloadData: []byte("NOT_JSON"),
	}
	_, err := Capture(
		context.Background(), deps, "gh/acme/web", nil, nil,
		Options{DefinitionID: "def-1", PollInterval: time.Millisecond},
	)
	if err == nil {
		t.Fatal("expected JSON parse error, got nil")
	}
}
