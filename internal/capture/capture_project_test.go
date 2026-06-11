package capture

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	apicontext "github.com/AwesomeCICD/circleci-org-migration-cli/api/context"
	"github.com/AwesomeCICD/circleci-org-migration-cli/api/project"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
)

// fakeCaptureClient implements the full capture.Client interface for driving
// CaptureProject end-to-end without any network calls.
type fakeCaptureClient struct {
	// project feature flags
	flags         map[string]bool
	getFlagsErr   error
	setFlagsCalls []map[string]bool

	// project metadata + pipeline definitions
	proj    *project.Project
	projErr error
	defs    []project.PipelineDefinition
	defsErr error

	// extract.Deps behaviour
	triggerID     string
	triggerErr    error
	workflows     [][]project.Workflow
	workflowIdx   int
	jobs          []project.Job
	jobsErr       error
	artifacts     []project.Artifact
	artifactsErr  error
	artifactBytes []byte
	downloadErr   error

	// restrictions
	restrictions []apicontext.Restriction
}

func (f *fakeCaptureClient) GetV11ProjectFeatureFlags(context.Context, string) (map[string]bool, error) {
	if f.getFlagsErr != nil {
		return nil, f.getFlagsErr
	}
	out := map[string]bool{}
	for k, v := range f.flags {
		out[k] = v
	}
	return out, nil
}

func (f *fakeCaptureClient) SetV11ProjectFeatureFlags(_ context.Context, _ string, flags map[string]bool) error {
	f.setFlagsCalls = append(f.setFlagsCalls, flags)
	return nil
}

func (f *fakeCaptureClient) GetProject(context.Context, string) (*project.Project, error) {
	return f.proj, f.projErr
}

func (f *fakeCaptureClient) ListPipelineDefinitions(context.Context, string) ([]project.PipelineDefinition, error) {
	return f.defs, f.defsErr
}

func (f *fakeCaptureClient) TriggerPipelineRun(context.Context, string, string, string, string, map[string]any) (string, error) {
	return f.triggerID, f.triggerErr
}

func (f *fakeCaptureClient) GetPipelineWorkflows(context.Context, string) ([]project.Workflow, error) {
	if f.workflowIdx >= len(f.workflows) {
		return nil, nil
	}
	wf := f.workflows[f.workflowIdx]
	f.workflowIdx++
	return wf, nil
}

func (f *fakeCaptureClient) GetWorkflowJobs(context.Context, string) ([]project.Job, error) {
	return f.jobs, f.jobsErr
}

func (f *fakeCaptureClient) ListJobArtifacts(context.Context, string, int) ([]project.Artifact, error) {
	return f.artifacts, f.artifactsErr
}

func (f *fakeCaptureClient) DownloadArtifact(context.Context, string) ([]byte, error) {
	return f.artifactBytes, f.downloadErr
}

func (f *fakeCaptureClient) ListRestrictions(context.Context, string) ([]apicontext.Restriction, error) {
	return f.restrictions, nil
}

func (f *fakeCaptureClient) CreateRestriction(context.Context, string, string, string) error {
	return nil
}
func (f *fakeCaptureClient) DeleteRestriction(context.Context, string, string) error { return nil }

// happyClient returns a fake client that completes one extraction run returning
// the given name→value map as the (plaintext) artifact body.
func happyClient(t *testing.T, vals map[string]string) *fakeCaptureClient {
	t.Helper()
	body, err := json.Marshal(vals)
	if err != nil {
		t.Fatalf("marshal artifact: %v", err)
	}
	return &fakeCaptureClient{
		flags:     map[string]bool{apiTriggerKey: true},
		proj:      &project.Project{Slug: "gh/acme/web", ID: "proj-uuid"},
		defs:      []project.PipelineDefinition{{ID: "def-1", Name: "build"}},
		triggerID: "pipe-1",
		workflows: [][]project.Workflow{
			{{ID: "wf-1", Name: "extract", Status: "success"}},
		},
		jobs: []project.Job{{Name: "circleci-migrate-extract", JobNumber: 7, Status: "success"}},
		artifacts: []project.Artifact{
			{Path: "/tmp/circleci-migrate-secrets.json", URL: "https://art/circleci-migrate-secrets.json"},
		},
		artifactBytes: body,
	}
}

func baseOptions(t *testing.T, client Client, proj *manifest.Project) CaptureProjectOptions {
	t.Helper()
	out := filepath.Join(t.TempDir(), "secrets.json")
	return CaptureProjectOptions{
		Client:          client,
		Manifest:        &manifest.Manifest{},
		Bundle:          manifest.NewSecretBundle(),
		Project:         proj,
		Branch:          "main",
		Output:          out,
		PollTimeout:     time.Second,
		ProjectVarsOnly: true,
		Stdout:          &bytes.Buffer{},
		Stderr:          &bytes.Buffer{},
	}
}

func TestCaptureProject_ProjectVars_HappyPath(t *testing.T) {
	client := happyClient(t, map[string]string{"API_KEY": "s3cret"})
	proj := &manifest.Project{
		Slug:    "gh/acme/web",
		EnvVars: []manifest.ProjectEnvVar{{Name: "API_KEY"}},
	}
	opts := baseOptions(t, client, proj)

	if err := CaptureProject(context.Background(), opts); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := opts.Bundle.ProjectSecrets["gh/acme/web"]["API_KEY"]; got != "s3cret" {
		t.Errorf("project secret API_KEY = %q, want s3cret", got)
	}
	// Bundle persisted to disk.
	if _, err := os.Stat(opts.Output); err != nil {
		t.Errorf("bundle not saved: %v", err)
	}
}

func TestCaptureProject_NoDefinitions_Skips(t *testing.T) {
	client := happyClient(t, nil)
	client.defs = nil // no pipeline definitions → skip
	proj := &manifest.Project{Slug: "gh/acme/web", EnvVars: []manifest.ProjectEnvVar{{Name: "X"}}}
	opts := baseOptions(t, client, proj)

	err := CaptureProject(context.Background(), opts)
	if !errors.Is(err, ErrSkipProject) {
		t.Fatalf("expected ErrSkipProject, got %v", err)
	}
}

func TestCaptureProject_FlagDisabled_NoEnableTrigger_Errors(t *testing.T) {
	client := happyClient(t, nil)
	client.flags = map[string]bool{apiTriggerKey: false}
	proj := &manifest.Project{Slug: "gh/acme/web", EnvVars: []manifest.ProjectEnvVar{{Name: "X"}}}
	opts := baseOptions(t, client, proj)
	opts.EnableTrigger = false

	err := CaptureProject(context.Background(), opts)
	if err == nil {
		t.Fatal("expected error when api-trigger-with-config is off and --enable-trigger not set")
	}
}

func TestCaptureProject_FlagDisabled_EnableTrigger_TogglesAndRestores(t *testing.T) {
	client := happyClient(t, map[string]string{"X": "1"})
	client.flags = map[string]bool{apiTriggerKey: false}
	proj := &manifest.Project{Slug: "gh/acme/web", EnvVars: []manifest.ProjectEnvVar{{Name: "X"}}}
	opts := baseOptions(t, client, proj)
	opts.EnableTrigger = true

	if err := CaptureProject(context.Background(), opts); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Expect an enable (true) followed by a restore (false).
	if len(client.setFlagsCalls) != 2 {
		t.Fatalf("expected 2 SetV11ProjectFeatureFlags calls (enable+restore), got %d", len(client.setFlagsCalls))
	}
	if !client.setFlagsCalls[0][apiTriggerKey] {
		t.Error("first flag call should enable api-trigger-with-config")
	}
	if client.setFlagsCalls[1][apiTriggerKey] {
		t.Error("second flag call should restore api-trigger-with-config to false")
	}
}

func TestCaptureProject_GetFlagsError(t *testing.T) {
	client := happyClient(t, nil)
	client.getFlagsErr = errors.New("boom")
	proj := &manifest.Project{Slug: "gh/acme/web"}
	opts := baseOptions(t, client, proj)

	if err := CaptureProject(context.Background(), opts); err == nil {
		t.Fatal("expected error when reading feature flags fails")
	}
}

func TestCaptureProject_NoVarNames_SkipsExtraction(t *testing.T) {
	client := happyClient(t, nil)
	// Project has no env vars in project-vars mode → nothing to extract.
	proj := &manifest.Project{Slug: "gh/acme/web"}
	opts := baseOptions(t, client, proj)

	if err := CaptureProject(context.Background(), opts); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// No trigger should have happened (workflows untouched).
	if client.workflowIdx != 0 {
		t.Errorf("expected no pipeline run when there are no var names, workflowIdx=%d", client.workflowIdx)
	}
}

func TestCaptureProject_ContextMode_CapturesContextVars(t *testing.T) {
	client := happyClient(t, map[string]string{"CTX_TOKEN": "abc"})
	hostProj := &manifest.Project{Slug: "gh/acme/web"}
	opts := baseOptions(t, client, hostProj)
	opts.ProjectVarsOnly = false
	opts.SelectedCtxNames = map[string]bool{"deploy": true}
	opts.Manifest = &manifest.Manifest{
		Contexts: []manifest.Context{
			{Name: "deploy", EnvVars: []manifest.ContextEnvVar{{Name: "CTX_TOKEN"}}},
		},
	}

	if err := CaptureProject(context.Background(), opts); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := opts.Bundle.ContextSecrets["deploy"]["CTX_TOKEN"]; got != "abc" {
		t.Errorf("context secret CTX_TOKEN = %q, want abc", got)
	}
}

func TestCaptureProject_RestrictedContext_RemoveAndRestore(t *testing.T) {
	client := happyClient(t, map[string]string{"SECRET": "v"})
	// Live restriction the remove path will delete then re-create.
	client.restrictions = []apicontext.Restriction{{ID: "live-1", Type: "project", Value: "proj-X"}}
	hostProj := &manifest.Project{Slug: "gh/acme/web"}
	opts := baseOptions(t, client, hostProj)
	opts.ProjectVarsOnly = false
	opts.RemoveRestrictions = true
	opts.SelectedCtxNames = map[string]bool{"locked": true}
	opts.Manifest = &manifest.Manifest{
		Source: manifest.Source{Org: manifest.Org{ID: "org-uuid"}},
		Contexts: []manifest.Context{
			{
				Name:         "locked",
				SourceID:     "ctx-uuid",
				EnvVars:      []manifest.ContextEnvVar{{Name: "SECRET"}},
				Restrictions: []manifest.Restriction{{Type: "project", Value: "proj-X"}},
			},
		},
	}

	if err := CaptureProject(context.Background(), opts); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The context's secret should have been captured (restrictions removed).
	if got := opts.Bundle.ContextSecrets["locked"]["SECRET"]; got != "v" {
		t.Errorf("context secret SECRET = %q, want v", got)
	}
}

func TestCaptureProject_RestrictDecider_Skips(t *testing.T) {
	client := happyClient(t, nil)
	hostProj := &manifest.Project{Slug: "gh/acme/web"}
	opts := baseOptions(t, client, hostProj)
	opts.ProjectVarsOnly = false
	opts.SelectedCtxNames = map[string]bool{"locked": true}
	opts.RestrictDecider = func(string, int) (bool, error) { return false, nil } // choose to skip
	opts.Manifest = &manifest.Manifest{
		Source: manifest.Source{Org: manifest.Org{ID: "org-uuid"}},
		Contexts: []manifest.Context{
			{
				Name:         "locked",
				SourceID:     "ctx-uuid",
				EnvVars:      []manifest.ContextEnvVar{{Name: "SECRET"}},
				Restrictions: []manifest.Restriction{{Type: "project", Value: "proj-X"}},
			},
		},
	}

	if err := CaptureProject(context.Background(), opts); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client.workflowIdx != 0 {
		t.Errorf("decider chose skip; no pipeline run expected, workflowIdx=%d", client.workflowIdx)
	}
}

func TestCaptureProject_RestrictedContext_SkippedByDefault(t *testing.T) {
	client := happyClient(t, nil)
	hostProj := &manifest.Project{Slug: "gh/acme/web"}
	opts := baseOptions(t, client, hostProj)
	opts.ProjectVarsOnly = false
	opts.SkipRestricted = true
	opts.SelectedCtxNames = map[string]bool{"locked": true}
	opts.Manifest = &manifest.Manifest{
		Source: manifest.Source{Org: manifest.Org{ID: "org-uuid"}},
		Contexts: []manifest.Context{
			{
				Name:    "locked",
				EnvVars: []manifest.ContextEnvVar{{Name: "SECRET"}},
				// A real (non-All-members) project restriction.
				Restrictions: []manifest.Restriction{{Type: "project", Value: "proj-X"}},
			},
		},
	}
	stderr := &bytes.Buffer{}
	opts.Stderr = stderr

	if err := CaptureProject(context.Background(), opts); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client.workflowIdx != 0 {
		t.Errorf("restricted context should have been skipped (no pipeline run), workflowIdx=%d", client.workflowIdx)
	}
	if !bytes.Contains(stderr.Bytes(), []byte("Skipping restricted context")) {
		t.Errorf("expected skip notice, got: %s", stderr.String())
	}
}
