package cmd_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CircleCI-Public/circleci-org-migration-cli/cmd"
	"github.com/spf13/cobra"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// writeTinyManifest writes a minimal valid manifest JSON to dir and returns
// its path.
func writeTinyManifest(t *testing.T, dir string) string {
	t.Helper()
	m := map[string]any{
		"schema_version": "1",
		"source": map[string]any{
			"host": "https://circleci.com",
			"org": map[string]any{
				"slug": "gh/testorg",
				"name": "testorg",
			},
		},
		"contexts": []any{},
		"projects": []any{},
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		t.Fatalf("marshal tiny manifest: %v", err)
	}
	path := filepath.Join(dir, "manifest.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write tiny manifest: %v", err)
	}
	return path
}

// runSyncCmd executes the sync subcommand and returns stdout, stderr, error.
func runSyncCmd(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	root := cmd.MakeCommands()

	var outBuf, errBuf strings.Builder
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	root.SetArgs(append([]string{"sync"}, args...))

	err = root.Execute()
	return outBuf.String(), errBuf.String(), err
}

// findSyncCmd returns the cobra sync subcommand from a freshly built root.
func findSyncCmd(t *testing.T) *cobra.Command {
	t.Helper()
	root := cmd.MakeCommands()
	for _, sub := range root.Commands() {
		if strings.HasPrefix(sub.Use, "sync") {
			return sub
		}
	}
	t.Fatal("sync subcommand not found")
	return nil
}

// ---------------------------------------------------------------------------
// --manifest missing
// ---------------------------------------------------------------------------

// TestSyncCmd_MissingManifest_ReturnsError verifies that omitting --manifest
// returns an error that mentions "manifest".  The manifest check fires before
// token validation, so no token env vars are needed.
func TestSyncCmd_MissingManifest_ReturnsError(t *testing.T) {
	t.Setenv("CIRCLECI_CLI_TOKEN", "")
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "")
	t.Setenv("CIRCLECI_DEST_TOKEN", "")

	_, _, err := runSyncCmd(t)
	if err == nil {
		t.Fatal("expected error when --manifest is missing, got nil")
	}
	if !strings.Contains(err.Error(), "manifest") {
		t.Errorf("error %q does not mention 'manifest'", err.Error())
	}
}

// ---------------------------------------------------------------------------
// --missing-secrets invalid value
// ---------------------------------------------------------------------------

// TestSyncCmd_InvalidMissingSecrets_ReturnsError verifies that an unrecognised
// --missing-secrets value returns an error before any network call is made.
// A valid --manifest is provided so that flag/manifest validation proceeds to
// the missing-secrets check.
func TestSyncCmd_InvalidMissingSecrets_ReturnsError(t *testing.T) {
	t.Setenv("CIRCLECI_CLI_TOKEN", "")
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "")
	t.Setenv("CIRCLECI_DEST_TOKEN", "")

	dir := t.TempDir()
	mPath := writeTinyManifest(t, dir)

	_, _, err := runSyncCmd(t, "--manifest", mPath, "--missing-secrets", "bogus")
	if err == nil {
		t.Fatal("expected error for invalid --missing-secrets value, got nil")
	}
	if !strings.Contains(err.Error(), "missing-secrets") {
		t.Errorf("error %q does not mention 'missing-secrets'", err.Error())
	}
}

// ---------------------------------------------------------------------------
// no token
// ---------------------------------------------------------------------------

// TestSyncCmd_NoToken_ReturnsError verifies that when no API token is available
// the command returns an error mentioning "token".  Token validation happens
// after --manifest and --missing-secrets validation, so we supply both valid
// flags to reach the token check.
func TestSyncCmd_NoToken_ReturnsError(t *testing.T) {
	t.Setenv("CIRCLECI_CLI_TOKEN", "")
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "")
	t.Setenv("CIRCLECI_DEST_TOKEN", "")

	dir := t.TempDir()
	mPath := writeTinyManifest(t, dir)

	_, _, err := runSyncCmd(t, "--manifest", mPath)
	if err == nil {
		t.Fatal("expected error when no API token is set, got nil")
	}
	if !strings.Contains(err.Error(), "token") {
		t.Errorf("error %q does not mention 'token'", err.Error())
	}
}

// TestSyncCmd_TokenFromEnv_PassesTokenCheck verifies that with a token
// available via CIRCLECI_CLI_TOKEN the token validation passes and we proceed
// past it (we expect a network/client error, not the "no destination API token"
// message).
func TestSyncCmd_TokenFromEnv_PassesTokenCheck(t *testing.T) {
	t.Setenv("CIRCLECI_CLI_TOKEN", "fake-token-for-test")
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "")
	t.Setenv("CIRCLECI_DEST_TOKEN", "")

	dir := t.TempDir()
	mPath := writeTinyManifest(t, dir)

	_, _, err := runSyncCmd(t, "--manifest", mPath)
	if err != nil && strings.Contains(err.Error(), "no destination API token") {
		t.Errorf("with CIRCLECI_CLI_TOKEN set, should not get token error; got: %v", err)
	}
}

// TestSyncCmd_DestTokenFromEnv_PassesTokenCheck verifies that
// CIRCLECI_DEST_TOKEN is also accepted as a token source.
func TestSyncCmd_DestTokenFromEnv_PassesTokenCheck(t *testing.T) {
	t.Setenv("CIRCLECI_CLI_TOKEN", "")
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "")
	t.Setenv("CIRCLECI_DEST_TOKEN", "fake-dest-token")

	dir := t.TempDir()
	mPath := writeTinyManifest(t, dir)

	_, _, err := runSyncCmd(t, "--manifest", mPath)
	if err != nil && strings.Contains(err.Error(), "no destination API token") {
		t.Errorf("with CIRCLECI_DEST_TOKEN set, should not get token error; got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Flag registration
// ---------------------------------------------------------------------------

// TestSyncCmd_FlagsRegistered verifies that all expected flags are present on
// the sync subcommand.
func TestSyncCmd_FlagsRegistered(t *testing.T) {
	syncSub := findSyncCmd(t)
	if syncSub == nil {
		return
	}

	wantFlags := []string{
		"manifest", "secrets", "mapping", "apply", "missing-secrets",
		"skip-contexts", "skip-projects", "skip-org-settings",
	}
	for _, name := range wantFlags {
		if syncSub.Flags().Lookup(name) == nil {
			t.Errorf("sync flag --%s not registered", name)
		}
	}
}

// TestSyncCmd_SkipOrgSettings_FlagAccepted verifies that --skip-org-settings
// is accepted as a valid flag and does not cause an error in validation.
func TestSyncCmd_SkipOrgSettings_FlagAccepted(t *testing.T) {
	t.Setenv("CIRCLECI_CLI_TOKEN", "")
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "")
	t.Setenv("CIRCLECI_DEST_TOKEN", "")

	dir := t.TempDir()
	mPath := writeTinyManifest(t, dir)

	// With --skip-org-settings we should still fail on token (not flag parsing).
	_, _, err := runSyncCmd(t, "--manifest", mPath, "--skip-org-settings")
	if err != nil && strings.Contains(err.Error(), "unknown flag") {
		t.Errorf("--skip-org-settings should be a known flag; got: %v", err)
	}
	// It should fail on the token check, not on the flag.
	if err != nil && !strings.Contains(err.Error(), "token") {
		t.Logf("error (expected token error): %v", err)
	}
}
