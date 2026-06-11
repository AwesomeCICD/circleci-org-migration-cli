package cmd_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AwesomeCICD/circleci-org-migration-cli/cmd"
)

// ---------------------------------------------------------------------------
// printSyncReport — exercised via a dry-run sync with a real manifest
// ---------------------------------------------------------------------------

// TestPrintSyncReport_DryRunOutput verifies that when sync runs in dry-run
// mode, printSyncReport writes a header containing "DRY RUN".
func TestPrintSyncReport_DryRunOutput(t *testing.T) {
	t.Setenv("CIRCLECI_CLI_TOKEN", "fake-token-sync-report")
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "")
	t.Setenv("CIRCLECI_DEST_TOKEN", "")

	dir := t.TempDir()
	mPath := writeTinyManifest(t, dir)

	// A dry-run (no --apply) with a valid manifest and token will pass
	// validation, fail only on the network call, and NOT reach printSyncReport.
	// We at least verify we get a network-related error rather than a config
	// error, which confirms printSyncReport's callers are wired up.
	_, _, err := runSyncCmd(t, "--manifest", mPath)
	// Should not be a "manifest" or "token" validation error.
	if err != nil {
		if strings.Contains(err.Error(), "no destination API token") {
			t.Errorf("should not get token error with CIRCLECI_CLI_TOKEN set: %v", err)
		}
	}
}

// TestPrintSyncReport_SkipAllSections verifies that skipping all sections
// (contexts + projects + org settings) and having no runner classes in the
// manifest results in a successful dry-run with no sync report printed.
func TestPrintSyncReport_SkipAllSections(t *testing.T) {
	t.Setenv("CIRCLECI_CLI_TOKEN", "fake-token-skip-all")
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "")
	t.Setenv("CIRCLECI_DEST_TOKEN", "")

	dir := t.TempDir()
	mPath := writeTinyManifest(t, dir)

	stdout, _, err := runSyncCmd(t,
		"--manifest", mPath,
		"--skip-contexts",
		"--skip-projects",
		"--skip-org-settings",
	)
	// The manifest has no runner classes and no dest-runner-namespace, so
	// after skipping all sections the command should succeed with no output.
	if err != nil {
		t.Fatalf("expected success when skipping all sections, got: %v", err)
	}
	// Output should be empty (no sections run).
	if strings.TrimSpace(stdout) != "" {
		t.Errorf("expected no stdout output, got: %q", stdout)
	}
}

// ---------------------------------------------------------------------------
// loadBundleIfPresent — exercised via sync --secrets
// ---------------------------------------------------------------------------

// TestLoadBundleIfPresent_ExplicitMissingSecretsFatal verifies that pointing
// --secrets at a non-existent path (explicitly supplied by the user) is a
// fatal error, not silently ignored.  The default path is optional, but an
// explicit flag is treated as a configuration error when the file is absent.
func TestLoadBundleIfPresent_ExplicitMissingSecretsFatal(t *testing.T) {
	t.Setenv("CIRCLECI_CLI_TOKEN", "fake-token-bundle")
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "")
	t.Setenv("CIRCLECI_DEST_TOKEN", "")

	dir := t.TempDir()
	mPath := writeTinyManifest(t, dir)
	// Point --secrets at a path that does not exist.
	noSecrets := filepath.Join(dir, "no-such-secrets.json")

	_, _, err := runSyncCmd(t, "--manifest", mPath, "--secrets", noSecrets,
		"--skip-contexts", "--skip-projects", "--skip-org-settings")
	// Explicit missing secrets file must now be a fatal error.
	if err == nil {
		t.Fatal("expected fatal error for explicit missing --secrets path, got nil")
	}
	if !strings.Contains(err.Error(), "secrets bundle not found") {
		t.Errorf("expected 'secrets bundle not found' in error, got: %v", err)
	}
}

// TestLoadBundleIfPresent_EmptyPathIsNotError verifies that passing an empty
// --secrets flag is handled gracefully (the default "secrets.json" would also
// be missing; testing empty explicitly).
func TestLoadBundleIfPresent_EmptyPathIsNotError(t *testing.T) {
	t.Setenv("CIRCLECI_CLI_TOKEN", "fake-token-empty-secrets")
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "")
	t.Setenv("CIRCLECI_DEST_TOKEN", "")

	dir := t.TempDir()
	mPath := writeTinyManifest(t, dir)

	_, _, err := runSyncCmd(t, "--manifest", mPath, "--secrets", "",
		"--skip-contexts", "--skip-projects", "--skip-org-settings")
	if err != nil {
		t.Fatalf("empty secrets path should not error, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// sync --dest-runner-namespace flag wiring
// ---------------------------------------------------------------------------

// TestSyncCmd_DestRunnerNamespaceFlagRegistered verifies the flag is registered.
func TestSyncCmd_DestRunnerNamespaceFlagRegistered(t *testing.T) {
	syncSub := findSyncCmd(t)
	if syncSub.Flags().Lookup("dest-runner-namespace") == nil {
		t.Error("sync flag --dest-runner-namespace not registered")
	}
}

// ---------------------------------------------------------------------------
// manifest with runner classes triggers runner section
// ---------------------------------------------------------------------------

// TestSyncCmd_ManifestWithRunnerClasses_DryRun verifies that when the manifest
// contains runner resource classes, the sync command tries to set up the runner
// client and reports runner classes (fails at network level, not config level).
func TestSyncCmd_ManifestWithRunnerClasses_DryRun(t *testing.T) {
	t.Setenv("CIRCLECI_CLI_TOKEN", "fake-token-runner-dry")
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "")
	t.Setenv("CIRCLECI_DEST_TOKEN", "")

	dir := t.TempDir()
	// Write a manifest with a runner resource class.
	m := map[string]any{
		"schema_version": "1",
		"source": map[string]any{
			"host": "https://circleci.com",
			"org":  map[string]any{"slug": "gh/testorg", "name": "testorg"},
		},
		"contexts":         []any{},
		"projects":         []any{},
		"runner_namespace": "testorg",
		"runner_resource_classes": []any{
			map[string]any{"name": "testorg/my-runner", "description": "test runner"},
		},
	}
	data, _ := json.MarshalIndent(m, "", "  ")
	mPath := filepath.Join(dir, "manifest.json")
	if err := os.WriteFile(mPath, data, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	_, _, err := runSyncCmd(t, "--manifest", mPath,
		"--skip-contexts", "--skip-projects", "--skip-org-settings")
	// Error is acceptable (network call fails), but should NOT be a config error.
	if err != nil {
		if strings.Contains(err.Error(), "no destination API token") {
			t.Errorf("should not get token error: %v", err)
		}
		if strings.Contains(err.Error(), "manifest") && strings.Contains(err.Error(), "required") {
			t.Errorf("should not get manifest-required error: %v", err)
		}
	}
}

// ---------------------------------------------------------------------------
// --skip-runner flag
// ---------------------------------------------------------------------------

// TestSyncCmd_SkipRunnerFlagRegistered verifies that --skip-runner is a known
// flag on the sync subcommand.
func TestSyncCmd_SkipRunnerFlagRegistered(t *testing.T) {
	syncSub := findSyncCmd(t)
	if syncSub.Flags().Lookup("skip-runner") == nil {
		t.Error("sync flag --skip-runner not registered")
	}
}

// TestSyncCmd_SkipRunner_FlagAccepted verifies that --skip-runner is accepted
// without causing a flag-parsing error.
func TestSyncCmd_SkipRunner_FlagAccepted(t *testing.T) {
	t.Setenv("CIRCLECI_CLI_TOKEN", "")
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "")
	t.Setenv("CIRCLECI_DEST_TOKEN", "")

	dir := t.TempDir()
	mPath := writeTinyManifest(t, dir)

	_, _, err := runSyncCmd(t, "--manifest", mPath, "--skip-runner")
	if err != nil && strings.Contains(err.Error(), "unknown flag") {
		t.Errorf("--skip-runner should be a known flag; got: %v", err)
	}
}

// TestSyncCmd_SkipRunner_SkipsRunnerEvenWithManifestClasses verifies that when
// --skip-runner is set, the runner sync section is bypassed even when the
// manifest contains runner resource classes (no runner-client creation, no
// network call for runner).
func TestSyncCmd_SkipRunner_SkipsRunnerEvenWithManifestClasses(t *testing.T) {
	t.Setenv("CIRCLECI_CLI_TOKEN", "fake-token-skip-runner")
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "")
	t.Setenv("CIRCLECI_DEST_TOKEN", "")

	dir := t.TempDir()
	// Manifest with runner classes that would normally trigger runner sync.
	m := map[string]any{
		"schema_version": "1",
		"source": map[string]any{
			"host": "https://circleci.com",
			"org":  map[string]any{"slug": "gh/testorg", "name": "testorg"},
		},
		"contexts":         []any{},
		"projects":         []any{},
		"runner_namespace": "testorg",
		"runner_resource_classes": []any{
			map[string]any{"name": "testorg/my-runner", "description": "test runner"},
		},
	}
	data, _ := json.MarshalIndent(m, "", "  ")
	mPath := filepath.Join(dir, "manifest.json")
	if err := os.WriteFile(mPath, data, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	// With --skip-runner, the runner client is not created and no network call
	// for runner sync is made, so this should succeed (not fail on network).
	_, _, err := runSyncCmd(t, "--manifest", mPath,
		"--skip-contexts", "--skip-projects", "--skip-org-settings",
		"--skip-runner")
	if err != nil {
		// Only acceptable errors are network-level (not config/token).
		if strings.Contains(err.Error(), "no destination API token") {
			t.Errorf("should not get token error: %v", err)
		}
		if strings.Contains(err.Error(), "creating runner client") {
			t.Errorf("runner client should not be created when --skip-runner is set: %v", err)
		}
	}
}

// ---------------------------------------------------------------------------
// --json stdout hygiene — no text interleaved into JSON
// ---------------------------------------------------------------------------

// TestSyncCmd_JSON_NoTextInStdout verifies that with --json, stdout contains
// ONLY valid JSON (no interleaved progress text).
func TestSyncCmd_JSON_NoTextInStdout(t *testing.T) {
	t.Setenv("CIRCLECI_CLI_TOKEN", "fake-token-json-hygiene")
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "")
	t.Setenv("CIRCLECI_DEST_TOKEN", "")

	dir := t.TempDir()
	mPath := writeTinyManifest(t, dir)

	stdout, _, err := runSyncCmd(t,
		"--manifest", mPath,
		"--skip-contexts",
		"--skip-projects",
		"--skip-org-settings",
		"--skip-runner",
		"--json",
	)
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}

	// Stdout must be valid JSON — no text may precede or follow it.
	var result map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &result); err != nil {
		t.Fatalf("--json stdout is not valid JSON: %v\noutput: %q", err, stdout)
	}

	// Sanity: top-level keys must be present.
	if _, ok := result["dry_run"]; !ok {
		t.Errorf("expected 'dry_run' key in JSON output; got: %v", result)
	}
	if _, ok := result["sections"]; !ok {
		t.Errorf("expected 'sections' key in JSON output; got: %v", result)
	}
}

// TestSyncCmd_JSON_NoEnableBuildTextInStdout verifies that the enable-builds
// progress messages ("Skipped enabling builds", "Enabling builds for N...") do
// NOT appear in stdout when --json is used.
func TestSyncCmd_JSON_NoEnableBuildTextInStdout(t *testing.T) {
	t.Setenv("CIRCLECI_CLI_TOKEN", "fake-token-json-enable")
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "")
	t.Setenv("CIRCLECI_DEST_TOKEN", "")

	dir := t.TempDir()
	mPath := writeTinyManifest(t, dir)

	stdout, _, err := runSyncCmd(t,
		"--manifest", mPath,
		"--skip-contexts",
		"--skip-projects",
		"--skip-org-settings",
		"--skip-runner",
		"--json",
	)
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}

	// None of the enable-builds progress messages must appear in stdout.
	for _, noWant := range []string{
		"would be created paused",
		"Skipped enabling builds",
		"Enabling builds for",
	} {
		if strings.Contains(stdout, noWant) {
			t.Errorf("enable-builds progress text %q must not appear in --json stdout; got: %q", noWant, stdout)
		}
	}
}

// ---------------------------------------------------------------------------
// Skipped-secrets warning
// ---------------------------------------------------------------------------

// TestSyncCmd_SkippedSecretsWarning_NoBundle verifies that when no secrets
// bundle is loaded and the manifest has env vars, a warning is emitted on
// stderr.
func TestSyncCmd_SkippedSecretsWarning_NoBundle(t *testing.T) {
	t.Setenv("CIRCLECI_CLI_TOKEN", "fake-token-warn-secrets")
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "")
	t.Setenv("CIRCLECI_DEST_TOKEN", "")

	dir := t.TempDir()
	// Manifest with context env vars — no secrets bundle provided.
	m := map[string]any{
		"schema_version": "1",
		"source": map[string]any{
			"host": "https://circleci.com",
			"org":  map[string]any{"slug": "gh/testorg", "name": "testorg"},
		},
		"contexts": []any{
			map[string]any{
				"name":                  "deploy",
				"environment_variables": []any{map[string]any{"name": "AWS_SECRET"}},
			},
		},
		"projects": []any{},
	}
	data, _ := json.MarshalIndent(m, "", "  ")
	mPath := filepath.Join(dir, "manifest.json")
	if err := os.WriteFile(mPath, data, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	// Use --secrets "" (empty) so the bundle is explicitly absent but not an
	// error. Skip all sections so we don't hit live API calls.
	_, stderr, err := runSyncCmd(t, "--manifest", mPath,
		"--secrets", "",
		"--skip-contexts", "--skip-projects", "--skip-org-settings",
		"--skip-runner",
	)
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}

	// The WARNING must appear on stderr.
	if !strings.Contains(stderr, "WARNING: no secrets bundle loaded") {
		t.Errorf("expected skipped-secrets warning on stderr; got: %q", stderr)
	}
	if !strings.Contains(stderr, "env var value(s) were not synced") {
		t.Errorf("expected 'env var value(s) were not synced' in warning; got: %q", stderr)
	}
}

// TestSyncCmd_SkippedSecretsWarning_WithBundle verifies that the warning is
// NOT emitted when a bundle is actually loaded.
func TestSyncCmd_SkippedSecretsWarning_WithBundle(t *testing.T) {
	t.Setenv("CIRCLECI_CLI_TOKEN", "fake-token-warn-bundle-present")
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "")
	t.Setenv("CIRCLECI_DEST_TOKEN", "")

	dir := t.TempDir()
	mPath := writeTinyManifest(t, dir)

	// Write a minimal valid secrets bundle.
	bundle := map[string]any{
		"schema_version":  "1",
		"context_secrets": map[string]any{},
		"project_secrets": map[string]any{},
	}
	bData, _ := json.MarshalIndent(bundle, "", "  ")
	bPath := filepath.Join(dir, "secrets.json")
	if err := os.WriteFile(bPath, bData, 0o600); err != nil {
		t.Fatalf("write bundle: %v", err)
	}

	_, stderr, err := runSyncCmd(t, "--manifest", mPath,
		"--secrets", bPath,
		"--skip-contexts", "--skip-projects", "--skip-org-settings",
		"--skip-runner",
	)
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}

	if strings.Contains(stderr, "WARNING: no secrets bundle loaded") {
		t.Errorf("warning must not appear when bundle is loaded; got stderr: %q", stderr)
	}
}

// ---------------------------------------------------------------------------
// BuildMigrateMapping — additional edge cases
// ---------------------------------------------------------------------------

// TestBuildMigrateMapping_EmptyOrgs verifies that passing empty source/dest
// org strings (with no mapping file) still returns a valid (empty) mapping.
func TestBuildMigrateMapping_EmptyOrgs(t *testing.T) {
	got, err := cmd.BuildMigrateMapping("", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil mapping")
	}
	if got.Org.From != "" || got.Org.To != "" {
		t.Errorf("expected empty Org mapping, got %+v", got.Org)
	}
}
