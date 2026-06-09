package cmd_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CircleCI-Public/circleci-org-migration-cli/cmd"
	"github.com/CircleCI-Public/circleci-org-migration-cli/internal/manifest"
	"github.com/spf13/cobra"
)

// ─────────────────────────────────────────────────────────────────────────────
// helpers
// ─────────────────────────────────────────────────────────────────────────────

// MakeTestCommands is an alias for cmd.MakeCommands used only in cmd_test.
// It is needed here so we can inspect the command tree without re-exporting.
func MakeTestCommands() *cobra.Command { return cmd.MakeCommands() }

// findSubcommand walks the cobra command tree to find a subcommand by name
// path, e.g. findSubcommand(root, "secrets", "capture").
func findSubcommand(root *cobra.Command, names ...string) *cobra.Command {
	cur := root
	for _, name := range names {
		var found *cobra.Command
		for _, sub := range cur.Commands() {
			if sub.Name() == name {
				found = sub
				break
			}
		}
		if found == nil {
			return nil
		}
		cur = found
	}
	return cur
}

// newCaptureFakeServer starts a fake API server that satisfies the minimal
// sequence of requests the "secrets capture" command makes for a single
// project with no contexts. It records flag-toggle calls so tests can
// assert restoration happened.
//
// Sequence:
//  1. GET  /api/v1.1/project/{slug}/settings        → feature flags (api-trigger-with-config=false)
//  2. PUT  /api/v1.1/project/{slug}/settings        → enable flag
//  3. GET  /api/v2/project/{slug}                   → project details (ID)
//  4. GET  /api/v2/projects/{id}/pipeline-definitions → definition ID
//  5. POST /api/v2/project/{slug}/pipeline/run       → trigger → pipelineID
//  6. GET  /api/v2/pipeline/{id}/workflow             → success status
//  7. GET  /api/v2/workflow/{id}/job                  → jobs list
//  8. GET  /api/v2/project/{slug}/{n}/artifacts       → artifact list
//  9. GET  (artifact URL)                             → JSON payload
//
// 10. PUT  /api/v1.1/project/{slug}/settings         → restore flag (=false)
type fakeCaptureServer struct {
	*httptest.Server
	// putCalls records the boolean value written on each v1.1 PUT settings call.
	putCalls []bool
}

func newCaptureFakeServer(t *testing.T, secretPayload map[string]string) *fakeCaptureServer {
	t.Helper()

	fcs := &fakeCaptureServer{}

	payloadJSON, err := json.Marshal(secretPayload)
	if err != nil {
		t.Fatalf("marshal secret payload: %v", err)
	}

	mux := http.NewServeMux()

	// v1.1 project settings (GET + PUT) — called for feature-flag read/write.
	mux.HandleFunc("/api/v1.1/project/gh/acme/web/settings", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			writeJSON(w, http.StatusOK, map[string]any{
				"feature_flags": map[string]any{"api-trigger-with-config": false},
			})
			return
		}
		if r.Method == http.MethodPut {
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			ff, _ := body["feature_flags"].(map[string]any)
			val, _ := ff["api-trigger-with-config"].(bool)
			fcs.putCalls = append(fcs.putCalls, val)
			writeJSON(w, http.StatusOK, map[string]any{})
			return
		}
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	})

	// v2 project details.
	mux.HandleFunc("/api/v2/project/gh/acme/web", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"id":   "proj-uuid-123",
			"slug": "gh/acme/web",
			"name": "web",
		})
	})

	// Pipeline definitions.
	mux.HandleFunc("/api/v2/projects/proj-uuid-123/pipeline-definitions", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"items": []map[string]any{
				{"id": "def-uuid-1", "name": "default",
					"config_source":   map[string]any{"provider": "github_app"},
					"checkout_source": map[string]any{"provider": "github_app"},
				},
			},
			"next_page_token": "",
		})
	})

	// Trigger pipeline run.
	mux.HandleFunc("/api/v2/project/gh/acme/web/pipeline/run", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusCreated, map[string]any{"id": "pipe-uuid-1", "number": 1})
	})

	// Poll workflow.
	mux.HandleFunc("/api/v2/pipeline/pipe-uuid-1/workflow", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"items":           []map[string]any{{"id": "wf-uuid-1", "name": "extract", "status": "success"}},
			"next_page_token": "",
		})
	})

	// Workflow jobs.
	mux.HandleFunc("/api/v2/workflow/wf-uuid-1/job", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"items": []map[string]any{
				{"name": "circleci-migrate-extract", "job_number": 42, "status": "success"},
			},
			"next_page_token": "",
		})
	})

	// Artifacts list.
	mux.HandleFunc("/api/v2/project/gh/acme/web/42/artifacts", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"items": []map[string]any{
				{"path": "/tmp/circleci-migrate-secrets.json", "node_index": 0,
					"url": fcs.URL + "/artifact/circleci-migrate-secrets.json"},
			},
			"next_page_token": "",
		})
	})

	// Artifact download.
	mux.HandleFunc("/artifact/circleci-migrate-secrets.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(payloadJSON)
	})

	srv := httptest.NewServer(mux)
	fcs.Server = srv
	return fcs
}

// writeJSON is a helper for fake server handlers.
func writeJSON(w http.ResponseWriter, status int, body any) {
	b, _ := json.Marshal(body)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(b)
}

// ─────────────────────────────────────────────────────────────────────────────
// Flag validation tests
// ─────────────────────────────────────────────────────────────────────────────

func TestSecretsCapture_NoManifest(t *testing.T) {
	_, _, err := runCmd(t, "secrets", "capture")
	if err == nil {
		t.Fatal("expected error when --manifest is missing, got nil")
	}
	if !strings.Contains(err.Error(), "manifest") {
		t.Errorf("error %q does not mention 'manifest'", err.Error())
	}
}

func TestSecretsCapture_FlagsRegistered(t *testing.T) {
	root := MakeTestCommands()
	sub := findSubcommand(root, "secrets", "capture")
	if sub == nil {
		t.Fatal("'secrets capture' subcommand not found")
	}
	required := []string{
		"manifest", "output", "project", "context", "branch",
		"enable-trigger", "poll-timeout", "skip-restricted-contexts",
	}
	for _, name := range required {
		if sub.Flags().Lookup(name) == nil {
			t.Errorf("flag --%s not registered on 'secrets capture'", name)
		}
	}
}

func TestSecretsCapture_NoToken_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	m := captureTestManifest()
	mPath := writeManifest(t, dir, "manifest.json", m)

	t.Setenv("CIRCLECI_CLI_TOKEN", "")
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "")
	t.Setenv("CIRCLECI_DEST_TOKEN", "")

	_, _, err := runCmd(t, "secrets", "capture", "--manifest", mPath)
	if err == nil {
		t.Fatal("expected error when no token set, got nil")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "token") {
		t.Errorf("error %q does not mention 'token'", err.Error())
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Restriction-warning path (end-to-end with fake server)
// ─────────────────────────────────────────────────────────────────────────────

func TestSecretsCapture_RestrictedContextWarning(t *testing.T) {
	// Build manifest with a project AND a restricted context.
	m := &manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		Contexts: []manifest.Context{
			{
				Name:    "restricted-ctx",
				EnvVars: []manifest.ContextEnvVar{{Name: "CTX_SECRET"}},
				Restrictions: []manifest.Restriction{
					{Type: "project", Value: "proj-uuid-123"},
				},
			},
		},
		Projects: []manifest.Project{
			{
				Slug:     "gh/acme/web",
				SourceID: "proj-uuid-123",
				EnvVars:  []manifest.ProjectEnvVar{{Name: "PROJECT_VAR"}},
			},
		},
	}

	srv := newCaptureFakeServer(t, map[string]string{
		"PROJECT_VAR": "proj-val",
		// CTX_SECRET absent (context skipped due to restriction)
	})
	defer srv.Close()

	dir := t.TempDir()
	mPath := writeManifest(t, dir, "manifest.json", m)
	outPath := filepath.Join(dir, "secrets.json")

	t.Setenv("CIRCLECI_CLI_TOKEN", "fake-token")

	_, stderr, _ := runCmd(t,
		"secrets", "capture",
		"--manifest", mPath,
		"--output", outPath,
		"--host", srv.URL,
		"--enable-trigger",
		"--skip-restricted-contexts=true",
		"--poll-timeout", "10s",
	)

	if !strings.Contains(stderr, "restrictions") {
		t.Errorf("stderr %q does not contain 'restrictions' warning", stderr)
	}
	if !strings.Contains(stderr, "Skipping restricted context") {
		t.Errorf("stderr %q does not contain skip message for restricted context", stderr)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Happy path (end-to-end with fake server)
// ─────────────────────────────────────────────────────────────────────────────

func TestSecretsCapture_HappyPath(t *testing.T) {
	m := captureTestManifest()
	secretPayload := map[string]string{"PROJECT_VAR": "super-secret"}

	srv := newCaptureFakeServer(t, secretPayload)
	defer srv.Close()

	dir := t.TempDir()
	mPath := writeManifest(t, dir, "manifest.json", m)
	outPath := filepath.Join(dir, "secrets.json")

	t.Setenv("CIRCLECI_CLI_TOKEN", "fake-token")

	stdout, stderr, err := runCmd(t,
		"secrets", "capture",
		"--manifest", mPath,
		"--output", outPath,
		"--host", srv.URL,
		"--enable-trigger",
		"--poll-timeout", "10s",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}

	// Bundle must exist with the captured value.
	bundle, loadErr := manifest.LoadSecretBundle(outPath)
	if loadErr != nil {
		t.Fatalf("load bundle: %v", loadErr)
	}
	if v, ok := bundle.ProjectSecrets["gh/acme/web"]["PROJECT_VAR"]; !ok || v != "super-secret" {
		t.Errorf("PROJECT_VAR = %q (ok=%v), want super-secret", v, ok)
	}

	// Restoration: the feature flag must have been toggled on then back off.
	if len(srv.putCalls) < 2 {
		t.Errorf("expected >=2 PUT calls (enable + restore), got %d: %v", len(srv.putCalls), srv.putCalls)
	}
	// Last call must be false (restore to original disabled state).
	if last := srv.putCalls[len(srv.putCalls)-1]; last {
		t.Errorf("last flag PUT should be false (restore), got true")
	}

	// Stdout must have the artifact retention warning.
	if !strings.Contains(stderr, "artifact") {
		t.Errorf("stderr %q missing artifact retention warning", stderr)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Flag-restore on error path
// ─────────────────────────────────────────────────────────────────────────────

func TestSecretsCapture_FlagRestoredOnError(t *testing.T) {
	// Server returns a failed workflow so Capture returns an error.
	// We still expect the flag to be restored.
	m := captureTestManifest()

	fcs := &fakeCaptureServer{}
	mux := http.NewServeMux()

	mux.HandleFunc("/api/v1.1/project/gh/acme/web/settings", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			writeJSON(w, http.StatusOK, map[string]any{
				"feature_flags": map[string]any{"api-trigger-with-config": false},
			})
			return
		}
		if r.Method == http.MethodPut {
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			ff, _ := body["feature_flags"].(map[string]any)
			val, _ := ff["api-trigger-with-config"].(bool)
			fcs.putCalls = append(fcs.putCalls, val)
			writeJSON(w, http.StatusOK, map[string]any{})
			return
		}
	})
	mux.HandleFunc("/api/v2/project/gh/acme/web", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"id": "proj-uuid-123", "slug": "gh/acme/web", "name": "web"})
	})
	mux.HandleFunc("/api/v2/projects/proj-uuid-123/pipeline-definitions", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"items": []map[string]any{
				{"id": "def-uuid-1", "name": "default",
					"config_source":   map[string]any{"provider": "github_app"},
					"checkout_source": map[string]any{"provider": "github_app"},
				},
			},
			"next_page_token": "",
		})
	})
	mux.HandleFunc("/api/v2/project/gh/acme/web/pipeline/run", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusCreated, map[string]any{"id": "pipe-uuid-1", "number": 1})
	})
	// Return a failed workflow to force an error in Capture.
	mux.HandleFunc("/api/v2/pipeline/pipe-uuid-1/workflow", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"items":           []map[string]any{{"id": "wf-uuid-1", "name": "extract", "status": "failed"}},
			"next_page_token": "",
		})
	})

	srv := httptest.NewServer(mux)
	fcs.Server = srv
	defer srv.Close()

	dir := t.TempDir()
	mPath := writeManifest(t, dir, "manifest.json", m)
	outPath := filepath.Join(dir, "secrets.json")

	t.Setenv("CIRCLECI_CLI_TOKEN", "fake-token")

	// The command should return an error (workflow failed) but still restore.
	_, stderr, err := runCmd(t,
		"secrets", "capture",
		"--manifest", mPath,
		"--output", outPath,
		"--host", srv.URL,
		"--enable-trigger",
		"--poll-timeout", "10s",
	)
	if err == nil {
		t.Fatal("expected error due to failed workflow, got nil")
	}
	if !strings.Contains(stderr, "Restoring api-trigger-with-config=false") {
		t.Errorf("stderr %q does not confirm flag was restored", stderr)
	}
	// putCalls: [true (enable), false (restore)]
	if len(fcs.putCalls) < 2 {
		t.Errorf("expected >=2 PUT calls (enable + restore), got %d: %v", len(fcs.putCalls), fcs.putCalls)
	}
	if last := fcs.putCalls[len(fcs.putCalls)-1]; last {
		t.Errorf("last PUT should be false (restore), got true; calls: %v", fcs.putCalls)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

// captureTestManifest returns a minimal manifest with one project, no contexts.
func captureTestManifest() *manifest.Manifest {
	return &manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		Projects: []manifest.Project{
			{
				Slug:     "gh/acme/web",
				SourceID: "proj-uuid-123",
				EnvVars:  []manifest.ProjectEnvVar{{Name: "PROJECT_VAR"}},
			},
		},
	}
}
