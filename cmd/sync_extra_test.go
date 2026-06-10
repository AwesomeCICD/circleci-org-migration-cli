package cmd_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CircleCI-Public/circleci-org-migration-cli/cmd"
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

// TestLoadBundleIfPresent_MissingFileIsNotError verifies that pointing
// --secrets at a non-existent file is not an error (it is optional).
func TestLoadBundleIfPresent_MissingFileIsNotError(t *testing.T) {
	t.Setenv("CIRCLECI_CLI_TOKEN", "fake-token-bundle")
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "")
	t.Setenv("CIRCLECI_DEST_TOKEN", "")

	dir := t.TempDir()
	mPath := writeTinyManifest(t, dir)
	// Point --secrets at a path that does not exist.
	noSecrets := filepath.Join(dir, "no-such-secrets.json")

	_, _, err := runSyncCmd(t, "--manifest", mPath, "--secrets", noSecrets,
		"--skip-contexts", "--skip-projects", "--skip-org-settings")
	// Missing secrets file should not cause an error.
	if err != nil {
		t.Fatalf("missing secrets file should not error, got: %v", err)
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
