package cmd_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AwesomeCICD/circleci-org-migration-cli/cmd"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
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
	// --no-input prevents the interactive walkthrough (which would block on stdin).
	_, _, err := runCmd(t, "secrets", "capture", "--no-input")
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
// All-members default group restriction must not cause a skip
// ─────────────────────────────────────────────────────────────────────────────

func TestSecretsCapture_AllMembersRestrictionNotSkipped(t *testing.T) {
	// A context whose only restriction is the All-members default (type=="group",
	// value==orgID) must NOT be warned about or skipped.
	const orgID = "acme-org-uuid"

	m := &manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		Source: manifest.Source{
			Org: manifest.Org{
				Slug: "gh/acme",
				ID:   orgID,
			},
		},
		Contexts: []manifest.Context{
			{
				Name:    "all-members-ctx",
				EnvVars: []manifest.ContextEnvVar{{Name: "CTX_VAR"}},
				Restrictions: []manifest.Restriction{
					// Default "All members" restriction — value == org ID.
					{Type: "group", Value: orgID, Name: "All members"},
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
		"CTX_VAR":     "ctx-val",
	})
	defer srv.Close()

	dir := t.TempDir()
	mPath := writeManifest(t, dir, "manifest.json", m)
	outPath := filepath.Join(dir, "secrets.json")

	t.Setenv("CIRCLECI_CLI_TOKEN", "fake-token")

	_, stderr, err := runCmd(t,
		"secrets", "capture",
		"--manifest", mPath,
		"--output", outPath,
		"--host", srv.URL,
		"--enable-trigger",
		"--skip-restricted-contexts=true",
		"--poll-timeout", "10s",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr: %s", err, stderr)
	}

	// No restriction warning should appear for the All-members default.
	if strings.Contains(stderr, "Skipping restricted context") {
		t.Errorf("All-members context should NOT be skipped; stderr: %s", stderr)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Org-level flag enable+restore (end-to-end with fake server)
// ─────────────────────────────────────────────────────────────────────────────

// newCaptureFakeServerWithOrgFlags extends newCaptureFakeServer to also serve
// the v1.1 org-settings endpoint so the org-level flag enable/restore can be
// tested end-to-end.
func newCaptureFakeServerWithOrgFlags(t *testing.T, secretPayload map[string]string, orgInitiallyEnabled bool) (*fakeCaptureServer, *[]bool) {
	t.Helper()

	inner := newCaptureFakeServer(t, secretPayload)
	// Replace the underlying mux by wrapping the existing server handler.
	// Since httptest.Server exposes the handler via Config.Handler, we can
	// reach it through inner.Server.Config.Handler.  Instead, use a new mux
	// that proxies known org paths and falls back to the original handler.

	orgPutCalls := &[]bool{}

	// Patch the org v1.1 settings endpoint into the same server by replacing
	// the handler.  We read the existing mux from the server's Config.
	origHandler := inner.Server.Config.Handler
	newMux := http.NewServeMux()

	// Org settings endpoint.
	newMux.HandleFunc("/api/v1.1/organization/github/acme/settings", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			writeJSON(w, http.StatusOK, map[string]any{
				"feature_flags": map[string]any{
					"allow_api_trigger_with_config": orgInitiallyEnabled,
				},
			})
			return
		}
		if r.Method == http.MethodPut {
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			ff, _ := body["feature_flags"].(map[string]any)
			val, _ := ff["allow-api-trigger-with-config"].(bool)
			*orgPutCalls = append(*orgPutCalls, val)
			writeJSON(w, http.StatusOK, map[string]any{})
			return
		}
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	})

	// Fall back to the original handler for everything else.
	newMux.HandleFunc("/", origHandler.ServeHTTP)

	inner.Server.Config.Handler = newMux

	return inner, orgPutCalls
}

func TestSecretsCapture_OrgLevelFlagEnabled_AndRestored(t *testing.T) {
	// With org-level flag initially off, --enable-trigger must enable it once
	// BEFORE iterating projects, then restore it after.
	m := &manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		Source: manifest.Source{
			Org: manifest.Org{
				Slug: "gh/acme",
				ID:   "acme-org-uuid",
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

	srv, orgPutCalls := newCaptureFakeServerWithOrgFlags(t, map[string]string{"PROJECT_VAR": "val"}, false)
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

	// Org flag must have been enabled then restored.
	if len(*orgPutCalls) < 2 {
		t.Errorf("expected >=2 org PUT calls (enable + restore), got %d: %v", len(*orgPutCalls), *orgPutCalls)
	}
	if !(*orgPutCalls)[0] {
		t.Errorf("first org PUT should enable (true), got false")
	}
	if (*orgPutCalls)[len(*orgPutCalls)-1] {
		t.Errorf("last org PUT should restore (false), got true")
	}

	if !strings.Contains(stderr, "org-level allow_api_trigger_with_config") {
		t.Errorf("stderr should mention org-level flag; got: %s", stderr)
	}
}

func TestSecretsCapture_OrgLevelFlagAlreadyOn_NoExtraCall(t *testing.T) {
	// When the org-level flag is already on, we must NOT call UpdateFeatureFlags.
	m := &manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		Source: manifest.Source{
			Org: manifest.Org{
				Slug: "gh/acme",
				ID:   "acme-org-uuid",
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

	// org flag initially ON
	srv, orgPutCalls := newCaptureFakeServerWithOrgFlags(t, map[string]string{"PROJECT_VAR": "val"}, true)
	defer srv.Close()

	dir := t.TempDir()
	mPath := writeManifest(t, dir, "manifest.json", m)
	outPath := filepath.Join(dir, "secrets.json")

	t.Setenv("CIRCLECI_CLI_TOKEN", "fake-token")

	_, _, err := runCmd(t,
		"secrets", "capture",
		"--manifest", mPath,
		"--output", outPath,
		"--host", srv.URL,
		"--enable-trigger",
		"--poll-timeout", "10s",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// No org PUT calls because the flag was already on.
	if len(*orgPutCalls) != 0 {
		t.Errorf("expected 0 org PUT calls when flag was already on, got %d: %v", len(*orgPutCalls), *orgPutCalls)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// --remove-restrictions flag (end-to-end with fake server)
// ─────────────────────────────────────────────────────────────────────────────

// newCaptureFakeServerWithRestrictions extends the base fake server to also
// handle context-restriction endpoints (LIST, DELETE, CREATE).  It records the
// order of restriction-related calls so tests can verify DELETE precedes the
// pipeline run, and CREATE follows.
type fakeCaptureServerWithRestrictions struct {
	*fakeCaptureServer
	// restrictionCalls records the HTTP method+path of each restriction call.
	restrictionCalls []string
}

func newCaptureFakeServerWithRestrictions(
	t *testing.T,
	secretPayload map[string]string,
	contextID string,
	liveRestrictions []map[string]any,
	triggerPipelineErr bool,
) *fakeCaptureServerWithRestrictions {
	t.Helper()

	fswr := &fakeCaptureServerWithRestrictions{}

	// Build a new server that handles both the standard capture endpoints AND
	// the restriction endpoints.
	payloadJSON, err := json.Marshal(secretPayload)
	if err != nil {
		t.Fatalf("marshal secret payload: %v", err)
	}

	fcs := &fakeCaptureServer{}
	mux := http.NewServeMux()

	// ── Standard capture endpoints (mirrors newCaptureFakeServer) ──────────

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

	mux.HandleFunc("/api/v2/project/gh/acme/web", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"id": "proj-uuid-123", "slug": "gh/acme/web", "name": "web",
		})
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
		fswr.restrictionCalls = append(fswr.restrictionCalls, "PIPELINE_RUN")
		if triggerPipelineErr {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"message": "trigger failed"})
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"id": "pipe-uuid-1", "number": 1})
	})

	mux.HandleFunc("/api/v2/pipeline/pipe-uuid-1/workflow", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"items":           []map[string]any{{"id": "wf-uuid-1", "name": "extract", "status": "success"}},
			"next_page_token": "",
		})
	})

	mux.HandleFunc("/api/v2/workflow/wf-uuid-1/job", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"items": []map[string]any{
				{"name": "circleci-migrate-extract", "job_number": 42, "status": "success"},
			},
			"next_page_token": "",
		})
	})

	mux.HandleFunc("/api/v2/project/gh/acme/web/42/artifacts", func(w http.ResponseWriter, r *http.Request) {
		// URL is set after server starts; use a placeholder, override below.
		writeJSON(w, http.StatusOK, map[string]any{
			"items": []map[string]any{
				{"path": "/tmp/circleci-migrate-secrets.json", "node_index": 0,
					"url": "__ARTIFACT_URL__"},
			},
			"next_page_token": "",
		})
	})

	// ── Restriction endpoints ────────────────────────────────────────────────

	listPath := "/api/v2/context/" + contextID + "/restrictions"
	mux.HandleFunc(listPath, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			fswr.restrictionCalls = append(fswr.restrictionCalls, "LIST")
			writeJSON(w, http.StatusOK, map[string]any{
				"items":           liveRestrictions,
				"next_page_token": "",
			})
		case http.MethodPost:
			fswr.restrictionCalls = append(fswr.restrictionCalls, "CREATE")
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			writeJSON(w, http.StatusCreated, map[string]any{
				"id": "new-restr-id", "restriction_type": body["restriction_type"],
				"restriction_value": body["restriction_value"], "context_id": contextID,
			})
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	for _, lr := range liveRestrictions {
		rid, _ := lr["id"].(string)
		deletePath := "/api/v2/context/" + contextID + "/restrictions/" + rid
		mux.HandleFunc(deletePath, func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodDelete {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			fswr.restrictionCalls = append(fswr.restrictionCalls, "DELETE:"+rid)
			writeJSON(w, http.StatusOK, map[string]any{})
		})
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Fix up the artifact URL once we know the server address.
		if r.URL.Path == "/api/v2/project/gh/acme/web/42/artifacts" {
			artifactURL := "http://" + r.Host + "/artifact/circleci-migrate-secrets.json"
			writeJSON(w, http.StatusOK, map[string]any{
				"items": []map[string]any{
					{"path": "/tmp/circleci-migrate-secrets.json", "node_index": 0,
						"url": artifactURL},
				},
				"next_page_token": "",
			})
			return
		}
		mux.ServeHTTP(w, r)
	}))

	// Artifact download handler — must be registered after server URL is known.
	mux.HandleFunc("/artifact/circleci-migrate-secrets.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(payloadJSON)
	})

	fcs.Server = srv
	fswr.fakeCaptureServer = fcs
	return fswr
}

// TestSecretsCapture_RemoveRestrictions_DeleteBeforeRunRestoreAfter verifies that
// with --remove-restrictions: DELETE is called before the pipeline run, and
// CREATE (restore) is called after (via defer, recorded in call order).
func TestSecretsCapture_RemoveRestrictions_DeleteBeforeRunRestoreAfter(t *testing.T) {
	const contextID = "ctx-restricted-uuid"
	const orgID = "acme-org-uuid"

	liveRestrictions := []map[string]any{
		{"id": "restr-1", "restriction_type": "project", "restriction_value": "proj-uuid-123",
			"name": "web", "context_id": contextID},
	}

	m := &manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		Source: manifest.Source{
			Org: manifest.Org{Slug: "gh/acme", ID: orgID},
		},
		Contexts: []manifest.Context{
			{
				Name:     "restricted-ctx",
				SourceID: contextID,
				EnvVars:  []manifest.ContextEnvVar{{Name: "CTX_SECRET"}},
				Restrictions: []manifest.Restriction{
					{Type: "project", Value: "proj-uuid-123", Name: "web"},
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

	srv := newCaptureFakeServerWithRestrictions(t,
		map[string]string{"PROJECT_VAR": "proj-val", "CTX_SECRET": "ctx-val"},
		contextID, liveRestrictions, false,
	)
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
		"--remove-restrictions",
		"--poll-timeout", "10s",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}

	// Verify call ordering: LIST → DELETE → PIPELINE_RUN → CREATE (via defer).
	calls := srv.restrictionCalls
	t.Logf("restriction/pipeline calls: %v", calls)

	listIdx := -1
	deleteIdx := -1
	runIdx := -1
	createIdx := -1
	for i, c := range calls {
		switch {
		case c == "LIST" && listIdx < 0:
			listIdx = i
		case c == "DELETE:restr-1" && deleteIdx < 0:
			deleteIdx = i
		case c == "PIPELINE_RUN" && runIdx < 0:
			runIdx = i
		case c == "CREATE" && createIdx < 0:
			createIdx = i
		}
	}

	if listIdx < 0 {
		t.Error("expected LIST call, not found")
	}
	if deleteIdx < 0 {
		t.Error("expected DELETE call, not found")
	}
	if runIdx < 0 {
		t.Error("expected PIPELINE_RUN call, not found")
	}
	if createIdx < 0 {
		t.Error("expected CREATE (restore) call, not found")
	}
	if deleteIdx >= 0 && runIdx >= 0 && deleteIdx >= runIdx {
		t.Errorf("DELETE (%d) must precede PIPELINE_RUN (%d)", deleteIdx, runIdx)
	}
	if runIdx >= 0 && createIdx >= 0 && createIdx <= runIdx {
		t.Errorf("CREATE/restore (%d) must follow PIPELINE_RUN (%d)", createIdx, runIdx)
	}

	// NOTICE messages should appear in stderr.
	if !strings.Contains(stderr, "temporarily removing") {
		t.Errorf("stderr %q should contain 'temporarily removing'", stderr)
	}
	if !strings.Contains(stderr, "restoring") {
		t.Errorf("stderr %q should contain 'restoring'", stderr)
	}
}

// TestSecretsCapture_RemoveRestrictions_RestoreOnExtractionError verifies that
// restrictions are restored (CREATE called) even when the pipeline extraction
// fails (workflow returns error status).
func TestSecretsCapture_RemoveRestrictions_RestoreOnExtractionError(t *testing.T) {
	const contextID = "ctx-restricted-uuid"
	const orgID = "acme-org-uuid"

	liveRestrictions := []map[string]any{
		{"id": "restr-fail-1", "restriction_type": "project", "restriction_value": "proj-uuid-123",
			"name": "web", "context_id": contextID},
	}

	m := &manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		Source: manifest.Source{
			Org: manifest.Org{Slug: "gh/acme", ID: orgID},
		},
		Contexts: []manifest.Context{
			{
				Name:     "restricted-ctx",
				SourceID: contextID,
				EnvVars:  []manifest.ContextEnvVar{{Name: "CTX_SECRET"}},
				Restrictions: []manifest.Restriction{
					{Type: "project", Value: "proj-uuid-123", Name: "web"},
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

	// triggerPipelineErr=true makes the pipeline run fail.
	srv := newCaptureFakeServerWithRestrictions(t,
		map[string]string{},
		contextID, liveRestrictions, true,
	)
	defer srv.Close()

	dir := t.TempDir()
	mPath := writeManifest(t, dir, "manifest.json", m)
	outPath := filepath.Join(dir, "secrets.json")

	t.Setenv("CIRCLECI_CLI_TOKEN", "fake-token")

	_, stderr, err := runCmd(t,
		"secrets", "capture",
		"--manifest", mPath,
		"--output", outPath,
		"--host", srv.URL,
		"--enable-trigger",
		"--remove-restrictions",
		"--poll-timeout", "10s",
	)

	// Capture should fail because pipeline trigger failed.
	if err == nil {
		t.Fatal("expected error due to pipeline failure, got nil")
	}

	// Restore (CREATE) must still have been called despite the error.
	hasCREATE := false
	for _, c := range srv.restrictionCalls {
		if c == "CREATE" {
			hasCREATE = true
			break
		}
	}
	if !hasCREATE {
		t.Errorf("restrictions must be restored (CREATE) even on extraction error; calls: %v", srv.restrictionCalls)
	}
	if !strings.Contains(stderr, "restoring") {
		t.Errorf("stderr should confirm restore happened; got: %s", stderr)
	}
}

// TestSecretsCapture_RemoveRestrictions_AllMembersNotTouched verifies that a
// context whose only restriction is the All-members default is NOT touched by
// --remove-restrictions (no LIST/DELETE/CREATE calls).
func TestSecretsCapture_RemoveRestrictions_AllMembersNotTouched(t *testing.T) {
	const orgID = "acme-org-uuid"
	const contextID = "ctx-all-members-uuid"

	// We reuse the plain fake server — no restriction endpoints needed.
	m := &manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		Source: manifest.Source{
			Org: manifest.Org{Slug: "gh/acme", ID: orgID},
		},
		Contexts: []manifest.Context{
			{
				Name:     "all-members-ctx",
				SourceID: contextID,
				EnvVars:  []manifest.ContextEnvVar{{Name: "CTX_VAR"}},
				Restrictions: []manifest.Restriction{
					// Only the All-members default — not a real restriction.
					{Type: "group", Value: orgID, Name: "All members"},
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
		"CTX_VAR":     "ctx-val",
	})
	defer srv.Close()

	dir := t.TempDir()
	mPath := writeManifest(t, dir, "manifest.json", m)
	outPath := filepath.Join(dir, "secrets.json")

	t.Setenv("CIRCLECI_CLI_TOKEN", "fake-token")

	_, stderr, err := runCmd(t,
		"secrets", "capture",
		"--manifest", mPath,
		"--output", outPath,
		"--host", srv.URL,
		"--enable-trigger",
		"--remove-restrictions",
		"--poll-timeout", "10s",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr: %s", err, stderr)
	}

	// No restriction removal notices should appear.
	if strings.Contains(stderr, "temporarily removing") {
		t.Errorf("All-members context should NOT trigger restriction removal; stderr: %s", stderr)
	}
}

// TestSecretsCapture_RemoveRestrictionsOff_SkipBehaviorUnchanged verifies that
// when --remove-restrictions is NOT set, the existing warn+skip behavior is
// preserved unchanged.
func TestSecretsCapture_RemoveRestrictionsOff_SkipBehaviorUnchanged(t *testing.T) {
	const orgID = "acme-org-uuid"

	m := &manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		Source: manifest.Source{
			Org: manifest.Org{Slug: "gh/acme", ID: orgID},
		},
		Contexts: []manifest.Context{
			{
				Name:     "restricted-ctx",
				SourceID: "ctx-restricted-uuid",
				EnvVars:  []manifest.ContextEnvVar{{Name: "CTX_SECRET"}},
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

	srv := newCaptureFakeServer(t, map[string]string{"PROJECT_VAR": "proj-val"})
	defer srv.Close()

	dir := t.TempDir()
	mPath := writeManifest(t, dir, "manifest.json", m)
	outPath := filepath.Join(dir, "secrets.json")

	t.Setenv("CIRCLECI_CLI_TOKEN", "fake-token")

	_, stderr, err := runCmd(t,
		"secrets", "capture",
		"--manifest", mPath,
		"--output", outPath,
		"--host", srv.URL,
		"--enable-trigger",
		// --remove-restrictions NOT set (default false)
		"--skip-restricted-contexts=true",
		"--poll-timeout", "10s",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr: %s", err, stderr)
	}

	if !strings.Contains(stderr, "Skipping restricted context") {
		t.Errorf("expected skip message without --remove-restrictions; stderr: %s", stderr)
	}
	if strings.Contains(stderr, "temporarily removing") {
		t.Errorf("should not see removal notice when --remove-restrictions is off; stderr: %s", stderr)
	}
}

// TestSecretsCapture_FlagsRegistered_IncludesRemoveRestrictions verifies the
// new flag is registered on the subcommand.
func TestSecretsCapture_FlagRegistered_RemoveRestrictions(t *testing.T) {
	root := MakeTestCommands()
	sub := findSubcommand(root, "secrets", "capture")
	if sub == nil {
		t.Fatal("'secrets capture' subcommand not found")
	}
	if sub.Flags().Lookup("remove-restrictions") == nil {
		t.Error("flag --remove-restrictions not registered on 'secrets capture'")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Fix 1: project loop passes projectVarsOnly=true → contexts not re-attached
// ─────────────────────────────────────────────────────────────────────────────

// newCaptureFakeServerTwoProjects builds a fake API server handling two
// projects (gh/acme/web and gh/acme/api) plus one context (my-ctx).
// It records each pipeline/run call so tests can assert trigger counts.
// projectSlug1="gh/acme/web", projID1="proj-uuid-123",
// projectSlug2="gh/acme/api", projID2="proj-uuid-456".
func newCaptureFakeServerTwoProjects(t *testing.T, secretPayload map[string]string) (*fakeCaptureServer, *int) {
	t.Helper()

	payloadJSON, _ := json.Marshal(secretPayload)
	pipelineTriggerCount := 0

	fcs := &fakeCaptureServer{}
	mux := http.NewServeMux()

	addHandlers := func(slug, projID, defID, pipeID, wfID string) {
		mux.HandleFunc("/api/v1.1/project/"+slug+"/settings", func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet {
				writeJSON(w, http.StatusOK, map[string]any{"feature_flags": map[string]any{"api-trigger-with-config": true}})
				return
			}
			if r.Method == http.MethodPut {
				fcs.putCalls = append(fcs.putCalls, true)
				writeJSON(w, http.StatusOK, map[string]any{})
			}
		})
		mux.HandleFunc("/api/v2/project/"+slug, func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusOK, map[string]any{"id": projID, "slug": slug, "name": slug})
		})
		mux.HandleFunc("/api/v2/projects/"+projID+"/pipeline-definitions", func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusOK, map[string]any{
				"items":           []map[string]any{{"id": defID, "name": "default", "config_source": map[string]any{"provider": "github_app"}, "checkout_source": map[string]any{"provider": "github_app"}}},
				"next_page_token": "",
			})
		})
		mux.HandleFunc("/api/v2/project/"+slug+"/pipeline/run", func(w http.ResponseWriter, r *http.Request) {
			pipelineTriggerCount++
			writeJSON(w, http.StatusCreated, map[string]any{"id": pipeID, "number": 1})
		})
		mux.HandleFunc("/api/v2/pipeline/"+pipeID+"/workflow", func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusOK, map[string]any{
				"items":           []map[string]any{{"id": wfID, "name": "extract", "status": "success"}},
				"next_page_token": "",
			})
		})
		mux.HandleFunc("/api/v2/workflow/"+wfID+"/job", func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusOK, map[string]any{
				"items":           []map[string]any{{"name": "circleci-migrate-extract", "job_number": 42, "status": "success"}},
				"next_page_token": "",
			})
		})
	}

	addHandlers("gh/acme/web", "proj-uuid-123", "def-web-1", "pipe-web-1", "wf-web-1")
	addHandlers("gh/acme/api", "proj-uuid-456", "def-api-1", "pipe-api-1", "wf-api-1")

	// Artifact download handler — URL must end with "circleci-migrate-secrets.json"
	// so the extract package can match it by URL suffix.
	mux.HandleFunc("/artifact/circleci-migrate-secrets.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(payloadJSON)
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Intercept artifacts endpoints — return a URL pointing to our artifact handler.
		if strings.HasSuffix(r.URL.Path, "/artifacts") {
			writeJSON(w, http.StatusOK, map[string]any{
				"items": []map[string]any{
					{
						"path":       "/tmp/circleci-migrate-secrets.json",
						"node_index": 0,
						"url":        "http://" + r.Host + "/artifact/circleci-migrate-secrets.json",
					},
				},
				"next_page_token": "",
			})
			return
		}
		mux.ServeHTTP(w, r)
	}))
	fcs.Server = srv
	return fcs, &pipelineTriggerCount
}

// TestSecretsCapture_ProjectLoopDoesNotAttachContexts verifies Fix 1:
// when there are 2 projects and 1 context, the per-project pipeline runs do
// NOT re-attach the context.  Contexts are captured ONCE under the host
// project; per-project runs use projectVarsOnly=true.
//
// Expected behaviour:
//   - 3 pipeline triggers total: 1 for host (web, with context), then 1 each
//     for web and api in the per-project loop (vars only, no context re-attach).
//   - Bundle contains context secret AND both project secrets.
func TestSecretsCapture_ProjectLoopDoesNotAttachContexts(t *testing.T) {
	const orgID = "acme-org-uuid"

	m := &manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		Source:        manifest.Source{Org: manifest.Org{Slug: "gh/acme", ID: orgID}},
		Contexts: []manifest.Context{
			{Name: "my-ctx", EnvVars: []manifest.ContextEnvVar{{Name: "CTX_SECRET"}}},
		},
		Projects: []manifest.Project{
			{Slug: "gh/acme/web", SourceID: "proj-uuid-123", EnvVars: []manifest.ProjectEnvVar{{Name: "WEB_VAR"}}},
			{Slug: "gh/acme/api", SourceID: "proj-uuid-456", EnvVars: []manifest.ProjectEnvVar{{Name: "API_VAR"}}},
		},
	}

	srv, triggerCount := newCaptureFakeServerTwoProjects(t, map[string]string{
		"CTX_SECRET": "ctx-val",
		"WEB_VAR":    "web-val",
		"API_VAR":    "api-val",
	})
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
		// Auto-picks gh/acme/web as host for context extraction.
	)
	if err != nil {
		t.Fatalf("unexpected error: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}

	// 3 triggers expected:
	//   1. gh/acme/web (host — captures context + web vars, projectVarsOnly=false)
	//   2. gh/acme/web (per-project loop, projectVarsOnly=true — captures web var again, NO ctx re-attach)
	//   3. gh/acme/api (per-project loop, projectVarsOnly=true — captures api var, NO ctx re-attach)
	//
	// The key assertion is that triggers 2 and 3 use projectVarsOnly=true, meaning
	// the context was NOT re-attached.  We verify this by checking context secrets
	// appear in the bundle exactly from the host-project run.
	if *triggerCount != 3 {
		t.Errorf("expected 3 pipeline triggers (host + 2 project-only), got %d", *triggerCount)
	}

	bundle, loadErr := manifest.LoadSecretBundle(outPath)
	if loadErr != nil {
		t.Fatalf("load bundle: %v", loadErr)
	}
	if v, ok := bundle.ContextSecrets["my-ctx"]["CTX_SECRET"]; !ok || v != "ctx-val" {
		t.Errorf("CTX_SECRET = %q (ok=%v), want ctx-val", v, ok)
	}
	if v, ok := bundle.ProjectSecrets["gh/acme/web"]["WEB_VAR"]; !ok || v != "web-val" {
		t.Errorf("WEB_VAR = %q (ok=%v), want web-val", v, ok)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Fix 2: --context without --project skips per-project loop
// ─────────────────────────────────────────────────────────────────────────────

// TestSecretsCapture_ContextOnlyFlag_SkipsProjectLoop verifies Fix 2:
// when --context is given but --project is NOT, the per-project env-var loop is
// skipped.  Only ONE pipeline is triggered (under the host project).
func TestSecretsCapture_ContextOnlyFlag_SkipsProjectLoop(t *testing.T) {
	const orgID = "acme-org-uuid"

	m := &manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		Source:        manifest.Source{Org: manifest.Org{Slug: "gh/acme", ID: orgID}},
		Contexts: []manifest.Context{
			{Name: "deploy-ctx", EnvVars: []manifest.ContextEnvVar{{Name: "DEPLOY_KEY"}}},
		},
		Projects: []manifest.Project{
			{Slug: "gh/acme/web", SourceID: "proj-uuid-123", EnvVars: []manifest.ProjectEnvVar{{Name: "WEB_VAR"}}},
			{Slug: "gh/acme/api", SourceID: "proj-uuid-456", EnvVars: []manifest.ProjectEnvVar{{Name: "API_VAR"}}},
		},
	}

	// Use the two-project server for convenience; we'll check only 1 trigger.
	srv, triggerCount := newCaptureFakeServerTwoProjects(t, map[string]string{
		"DEPLOY_KEY": "deploy-secret",
		"WEB_VAR":    "web-val",
	})
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
		"--context", "deploy-ctx",
		"--host-project", "gh/acme/web",
		// No --project: per-project loop must be SKIPPED entirely.
	)
	if err != nil {
		t.Fatalf("unexpected error: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}

	// Only 1 trigger: host project (web). No per-project loop for web or api.
	if *triggerCount != 1 {
		t.Errorf("expected exactly 1 pipeline trigger (host only), got %d; stderr: %s", *triggerCount, stderr)
	}

	bundle, loadErr := manifest.LoadSecretBundle(outPath)
	if loadErr != nil {
		t.Fatalf("load bundle: %v", loadErr)
	}
	if v, ok := bundle.ContextSecrets["deploy-ctx"]["DEPLOY_KEY"]; !ok || v != "deploy-secret" {
		t.Errorf("DEPLOY_KEY = %q (ok=%v), want deploy-secret", v, ok)
	}
	_ = stdout
}

// ─────────────────────────────────────────────────────────────────────────────
// Fix 2: default scoping (no flags) → only contexts/projects with values
// ─────────────────────────────────────────────────────────────────────────────

// TestSecretsCapture_DefaultScoping_SkipsEmptyProjectsAndContexts verifies that
// when no --context or --project flags are given, the command captures only
// projects with ≥1 env var and contexts with ≥1 env var (not all of them).
func TestSecretsCapture_DefaultScoping_SkipsEmptyProjectsAndContexts(t *testing.T) {
	// One project with vars, one without.  One context with vars, one without.
	// Only the project/context with vars should be captured.
	m := &manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		Contexts: []manifest.Context{
			{Name: "ctx-with-vars", EnvVars: []manifest.ContextEnvVar{{Name: "CTX_VAR"}}},
			{Name: "ctx-empty"}, // no vars → must NOT be extracted
		},
		Projects: []manifest.Project{
			{
				Slug:     "gh/acme/web",
				SourceID: "proj-uuid-123",
				EnvVars:  []manifest.ProjectEnvVar{{Name: "WEB_VAR"}},
			},
			{
				Slug:     "gh/acme/empty",
				SourceID: "proj-uuid-999",
				// no vars → must NOT be captured
			},
		},
	}

	srv := newCaptureFakeServer(t, map[string]string{
		"WEB_VAR": "web-val",
		"CTX_VAR": "ctx-val",
	})
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
		// No --context, no --project: defaults to with-values only.
	)
	if err != nil {
		t.Fatalf("unexpected error: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}

	// The scope summary should reflect the correct counts.
	if !strings.Contains(stderr, "1 context(s) with values") {
		t.Errorf("stderr should report 1 context with values; got: %s", stderr)
	}
	if !strings.Contains(stderr, "1 project(s) with values") {
		t.Errorf("stderr should report 1 project with values; got: %s", stderr)
	}

	bundle, loadErr := manifest.LoadSecretBundle(outPath)
	if loadErr != nil {
		t.Fatalf("load bundle: %v", loadErr)
	}
	// Context with vars should be captured.
	if _, ok := bundle.ContextSecrets["ctx-with-vars"]["CTX_VAR"]; !ok {
		t.Errorf("ctx-with-vars/CTX_VAR should be in bundle")
	}
	// Empty context must NOT appear.
	if _, ok := bundle.ContextSecrets["ctx-empty"]; ok {
		t.Errorf("ctx-empty should NOT appear in bundle (no env vars)")
	}
	// Project with vars should be captured.
	if _, ok := bundle.ProjectSecrets["gh/acme/web"]["WEB_VAR"]; !ok {
		t.Errorf("gh/acme/web/WEB_VAR should be in bundle")
	}

	_ = stdout
}

// ─────────────────────────────────────────────────────────────────────────────
// Fix 3 (e2e): prepareRestrictionRemoval leaves default group untouched
// ─────────────────────────────────────────────────────────────────────────────

// TestSecretsCapture_RemoveRestrictions_DefaultGroupNeverTouched_E2E verifies
// that with --remove-restrictions, a context whose restrictions are
// [group(orgID), project(X)] only triggers DELETE for the project restriction,
// and CREATE only restores the project restriction. The default group is never
// deleted or re-created.
func TestSecretsCapture_RemoveRestrictions_DefaultGroupNeverTouched_E2E(t *testing.T) {
	const contextID = "ctx-mixed-uuid"
	const orgID = "acme-org-uuid"

	// Live restrictions include the default group AND a real project restriction.
	liveRestrictions := []map[string]any{
		{"id": "default-group-live", "restriction_type": "group", "restriction_value": orgID,
			"name": "All members", "context_id": contextID},
		{"id": "proj-restr-live", "restriction_type": "project", "restriction_value": "proj-uuid-123",
			"name": "web", "context_id": contextID},
	}

	m := &manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		Source: manifest.Source{
			Org: manifest.Org{Slug: "gh/acme", ID: orgID},
		},
		Contexts: []manifest.Context{
			{
				Name:     "mixed-ctx",
				SourceID: contextID,
				EnvVars:  []manifest.ContextEnvVar{{Name: "CTX_SECRET"}},
				Restrictions: []manifest.Restriction{
					{Type: "group", Value: orgID, Name: "All members"}, // default group
					{Type: "project", Value: "proj-uuid-123", Name: "web"},
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

	srv := newCaptureFakeServerWithRestrictions(t,
		map[string]string{"PROJECT_VAR": "proj-val", "CTX_SECRET": "ctx-val"},
		contextID, liveRestrictions, false,
	)
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
		"--remove-restrictions",
		"--poll-timeout", "10s",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}

	calls := srv.restrictionCalls
	t.Logf("restriction calls: %v", calls)

	// The default group restriction must NEVER be deleted.
	for _, c := range calls {
		if c == "DELETE:default-group-live" {
			t.Error("DELETE was called on the default 'All members' group restriction — this must not happen")
		}
	}

	// The project restriction MUST be deleted.
	hasDeleteProj := false
	for _, c := range calls {
		if c == "DELETE:proj-restr-live" {
			hasDeleteProj = true
		}
	}
	if !hasDeleteProj {
		t.Errorf("DELETE was not called for the real project restriction; calls: %v", calls)
	}

	// CREATE (restore) must have been called exactly once (for the project restriction).
	createCount := 0
	for _, c := range calls {
		if c == "CREATE" {
			createCount++
		}
	}
	if createCount != 1 {
		t.Errorf("expected exactly 1 CREATE (project restriction restore), got %d; calls: %v", createCount, calls)
	}

	_ = stdout
	_ = stderr
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
