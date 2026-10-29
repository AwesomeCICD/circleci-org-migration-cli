package cmd_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AwesomeCICD/circleci-org-migration-cli/cmd"
)

// pipelineExitServer is a minimal destination API: an App org holding one
// existing project "web" whose pipeline definitions can be made to fail.
func pipelineExitServer(t *testing.T, listDefsStatus int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/v2/organization/"):
			_, _ = w.Write([]byte(`{"id":"11111111-1111-1111-1111-111111111111","name":"dest","slug":"circleci/dest","vcs_type":"circleci"}`))
		case r.URL.Path == "/api/private/project":
			_, _ = w.Write([]byte(`{"items":[{"id":"proj-1","slug":"circleci/dest/proj-1","name":"web"}],"next_page_token":""}`))
		case strings.HasSuffix(r.URL.Path, "/pipeline-definitions"):
			w.WriteHeader(listDefsStatus)
			if listDefsStatus == http.StatusOK {
				_, _ = w.Write([]byte(`{"items":[],"next_page_token":""}`))
				return
			}
			_, _ = w.Write([]byte(`{"message":"definitions unavailable"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"not found"}`))
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// writeAppPipelineFixture writes a manifest with one App project (one captured
// definition + trigger) and a mapping that retargets to the fake dest org.
func writeAppPipelineFixture(t *testing.T) (manifestPath, mappingPath string) {
	t.Helper()
	dir := t.TempDir()
	m := map[string]any{
		"schema_version": "1",
		"source":         map[string]any{"host": "https://circleci.com", "org": map[string]any{"slug": "circleci/src", "name": "src"}},
		"contexts":       []any{},
		"projects": []any{map[string]any{
			"slug": "circleci/src/web", "name": "web",
			"pipeline_definitions": []any{map[string]any{
				"name":            "default",
				"config_source":   map[string]any{"provider": "github_app", "repo_full_name": "acme/web", "repo_external_id": "1", "file_path": ".circleci/config.yml"},
				"checkout_source": map[string]any{"provider": "github_app", "repo_full_name": "acme/web", "repo_external_id": "1"},
				"triggers": []any{map[string]any{
					"name": "push", "event_preset": "all-pushes",
					"event_source": map[string]any{"provider": "github_app", "repo_full_name": "acme/web", "repo_external_id": "1"},
				}},
			}},
		}},
	}
	data, _ := json.Marshal(m)
	manifestPath = filepath.Join(dir, "manifest.json")
	if err := os.WriteFile(manifestPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	mapping := map[string]any{"org": map[string]any{"from": "circleci/src", "to": "circleci/22222222-2222-2222-2222-222222222222"}}
	data, _ = json.Marshal(mapping)
	mappingPath = filepath.Join(dir, "mapping.json")
	if err := os.WriteFile(mappingPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return manifestPath, mappingPath
}

// TestSyncCmd_PipelineDefError_ExitsNonZeroAfterSummary: a failed read of the
// destination's pipeline definitions is recorded per item — sync still prints
// the summary, then exits non-zero.
func TestSyncCmd_PipelineDefError_ExitsNonZeroAfterSummary(t *testing.T) {
	srv := pipelineExitServer(t, http.StatusBadRequest)
	mPath, mapPath := writeAppPipelineFixture(t)

	stdout, stderr, err := runSyncCmd(t,
		"--manifest", mPath, "--mapping", mapPath, "--host", srv.URL, "--token", "tok",
		"--skip-preflight", "--skip-contexts", "--skip-org-settings")
	if err == nil {
		t.Fatalf("a pipeline-definition error must make sync exit non-zero\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if !strings.Contains(err.Error(), "pipeline definition/trigger") {
		t.Errorf("error should name the failed pipeline actions, got: %v", err)
	}
	if !strings.Contains(stdout, "list destination pipeline definitions") {
		t.Errorf("the per-item error must still be printed before failing:\n%s", stdout)
	}
}

// TestSyncCmd_PipelineDefs_NoError_ExitsZero: the same fixture with a healthy
// API still exits 0 (the dry run only plans).
func TestSyncCmd_PipelineDefs_NoError_ExitsZero(t *testing.T) {
	srv := pipelineExitServer(t, http.StatusOK)
	mPath, mapPath := writeAppPipelineFixture(t)

	_, stderr, err := runSyncCmd(t,
		"--manifest", mPath, "--mapping", mapPath, "--host", srv.URL, "--token", "tok",
		"--skip-preflight", "--skip-contexts", "--skip-org-settings")
	if err != nil {
		t.Fatalf("healthy dry run must exit 0, got %v\nstderr:\n%s", err, stderr)
	}
}

// TestEnableExistingTriggersFlag_RegisteredOnSyncAndMigrate: the opt-in flag
// exists on both commands and defaults to off.
func TestEnableExistingTriggersFlag_RegisteredOnSyncAndMigrate(t *testing.T) {
	for _, sub := range []string{"sync", "migrate"} {
		root := cmd.MakeCommands()
		var found bool
		for _, c := range root.Commands() {
			if strings.HasPrefix(c.Use, sub) {
				f := c.Flags().Lookup("enable-existing-triggers")
				if f == nil {
					t.Fatalf("%s: --enable-existing-triggers not registered", sub)
				}
				if f.DefValue != "false" {
					t.Errorf("%s: --enable-existing-triggers must default to false, got %q", sub, f.DefValue)
				}
				found = true
			}
		}
		if !found {
			t.Fatalf("%s command not found", sub)
		}
	}
}
