package cmd_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AwesomeCICD/circleci-org-migration-cli/cmd"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
	"github.com/spf13/cobra"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// runMigrateCmd executes the migrate subcommand and returns stdout, stderr, error.
func runMigrateCmd(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	root := cmd.MakeCommands()

	var outBuf, errBuf strings.Builder
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	root.SetArgs(append([]string{"migrate"}, args...))

	err = root.Execute()
	return outBuf.String(), errBuf.String(), err
}

// findMigrateCmd returns the cobra migrate subcommand from a freshly built root.
func findMigrateCmd(t *testing.T) *cobra.Command {
	t.Helper()
	root := cmd.MakeCommands()
	for _, sub := range root.Commands() {
		if strings.HasPrefix(sub.Use, "migrate") {
			return sub
		}
	}
	t.Fatal("migrate subcommand not found")
	return nil
}

// ---------------------------------------------------------------------------
// Flag registration
// ---------------------------------------------------------------------------

// TestMigrateCmd_FlagsRegistered verifies that all expected flags are present
// on the migrate subcommand.
func TestMigrateCmd_FlagsRegistered(t *testing.T) {
	migSub := findMigrateCmd(t)

	wantFlags := []string{
		"source-org", "dest-org",
		"secrets", "mapping",
		"apply",
		"yes",
		"missing-secrets",
		"github-token",
		"skip-contexts", "skip-projects", "skip-org-settings", "skip-extras",
		"output", "report",
	}
	for _, name := range wantFlags {
		if migSub.Flags().Lookup(name) == nil {
			t.Errorf("migrate flag --%s not registered", name)
		}
	}
}

// TestMigrateCmd_YesShorthandRegistered verifies that -y is the shorthand for
// --yes on the migrate subcommand.
func TestMigrateCmd_YesShorthandRegistered(t *testing.T) {
	migSub := findMigrateCmd(t)

	if migSub.Flags().ShorthandLookup("y") == nil {
		t.Error("migrate flag -y (shorthand for --yes) not registered")
	}
}

// TestMigrateCmd_OutputShorthandRegistered verifies that -o is the shorthand
// for --output on the migrate subcommand.
func TestMigrateCmd_OutputShorthandRegistered(t *testing.T) {
	migSub := findMigrateCmd(t)

	if migSub.Flags().ShorthandLookup("o") == nil {
		t.Error("migrate flag -o (shorthand for --output) not registered")
	}
}

// ---------------------------------------------------------------------------
// Required-flag + token validation errors
// ---------------------------------------------------------------------------

// TestMigrateCmd_NoSourceOrg_ReturnsError verifies that omitting --source-org
// returns an error mentioning "source-org".  --no-input prevents the interactive
// walkthrough from triggering (which would block waiting for stdin input).
func TestMigrateCmd_NoSourceOrg_ReturnsError(t *testing.T) {
	t.Setenv("CIRCLECI_CLI_TOKEN", "")
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "")
	t.Setenv("CIRCLECI_DEST_TOKEN", "")

	_, _, err := runMigrateCmd(t, "--no-input")
	if err == nil {
		t.Fatal("expected error when --source-org is missing, got nil")
	}
	if !strings.Contains(err.Error(), "source-org") {
		t.Errorf("error %q does not mention 'source-org'", err.Error())
	}
}

// TestMigrateCmd_NoDestOrg_ReturnsError verifies that omitting --dest-org
// returns an error mentioning "dest-org".  --no-input prevents the interactive
// walkthrough from triggering.
func TestMigrateCmd_NoDestOrg_ReturnsError(t *testing.T) {
	t.Setenv("CIRCLECI_CLI_TOKEN", "")
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "")
	t.Setenv("CIRCLECI_DEST_TOKEN", "")

	_, _, err := runMigrateCmd(t, "--no-input", "--source-org", "gh/acme")
	if err == nil {
		t.Fatal("expected error when --dest-org is missing, got nil")
	}
	if !strings.Contains(err.Error(), "dest-org") {
		t.Errorf("error %q does not mention 'dest-org'", err.Error())
	}
}

// TestMigrateCmd_NoSourceToken_ReturnsError verifies that when no source API
// token is available the command returns an error mentioning "source" and
// "token". Token validation fires after --source-org and --dest-org are
// supplied.
func TestMigrateCmd_NoSourceToken_ReturnsError(t *testing.T) {
	t.Setenv("CIRCLECI_CLI_TOKEN", "")
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "")
	t.Setenv("CIRCLECI_DEST_TOKEN", "")

	_, _, err := runMigrateCmd(t, "--source-org", "gh/acme", "--dest-org", "gh/acme-new")
	if err == nil {
		t.Fatal("expected error when no source token is set, got nil")
	}
	if !strings.Contains(err.Error(), "token") {
		t.Errorf("error %q does not mention 'token'", err.Error())
	}
	if !strings.Contains(err.Error(), "source") {
		t.Errorf("error %q does not mention 'source'", err.Error())
	}
}

// TestMigrateCmd_NoDestToken_ReturnsError verifies that when a source token is
// set but no destination token is available the command returns an error
// mentioning "destination" (or "dest") and "token".
func TestMigrateCmd_NoDestToken_ReturnsError(t *testing.T) {
	t.Setenv("CIRCLECI_CLI_TOKEN", "")
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "fake-source-token")
	t.Setenv("CIRCLECI_DEST_TOKEN", "")

	_, _, err := runMigrateCmd(t, "--source-org", "gh/acme", "--dest-org", "gh/acme-new")
	if err == nil {
		t.Fatal("expected error when no destination token is set, got nil")
	}
	if !strings.Contains(err.Error(), "token") {
		t.Errorf("error %q does not mention 'token'", err.Error())
	}
	// Should mention destination context, not source.
	if !strings.Contains(strings.ToLower(err.Error()), "dest") {
		t.Errorf("error %q does not mention 'dest'", err.Error())
	}
}

// TestMigrateCmd_InvalidMissingSecrets_ReturnsError verifies that an
// unrecognised --missing-secrets value returns an error.
func TestMigrateCmd_InvalidMissingSecrets_ReturnsError(t *testing.T) {
	t.Setenv("CIRCLECI_CLI_TOKEN", "")
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "")
	t.Setenv("CIRCLECI_DEST_TOKEN", "")

	_, _, err := runMigrateCmd(t,
		"--source-org", "gh/acme",
		"--dest-org", "gh/acme-new",
		"--missing-secrets", "bogus",
	)
	if err == nil {
		t.Fatal("expected error for invalid --missing-secrets value, got nil")
	}
	if !strings.Contains(err.Error(), "missing-secrets") {
		t.Errorf("error %q does not mention 'missing-secrets'", err.Error())
	}
}

// TestMigrateCmd_SourceTokenFromEnv_PassesSourceTokenCheck verifies that
// CIRCLECI_SOURCE_TOKEN satisfies the source-token validation. The destination
// token check fires next (no CIRCLECI_DEST_TOKEN set), so we expect a
// "no destination API token" error rather than a "no source API token" error.
func TestMigrateCmd_SourceTokenFromEnv_PassesSourceTokenCheck(t *testing.T) {
	t.Setenv("CIRCLECI_CLI_TOKEN", "")
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "fake-src-token")
	t.Setenv("CIRCLECI_DEST_TOKEN", "")

	_, _, err := runMigrateCmd(t, "--source-org", "gh/acme", "--dest-org", "gh/acme-new")
	if err == nil {
		t.Fatal("expected error when no destination token is set, got nil")
	}
	// Should NOT fail on the source-token check.
	if strings.Contains(err.Error(), "no source API token") {
		t.Errorf("with CIRCLECI_SOURCE_TOKEN set, should not get source token error; got: %v", err)
	}
	// Should fail on the destination-token check.
	if !strings.Contains(err.Error(), "no destination API token") {
		t.Errorf("expected destination-token error when no dest token set; got: %v", err)
	}
}

// TestMigrateCmd_SharedTokenFallback verifies that CIRCLECI_CLI_TOKEN serves
// as a fallback for both source and destination tokens, advancing past both
// token validation checks (the early "no ... API token" guards).
func TestMigrateCmd_SharedTokenFallback(t *testing.T) {
	t.Setenv("CIRCLECI_CLI_TOKEN", "fake-shared-token")
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "")
	t.Setenv("CIRCLECI_DEST_TOKEN", "")

	_, _, err := runMigrateCmd(t, "--source-org", "gh/acme", "--dest-org", "gh/acme-new")
	// Both early token-validation guards should pass; we expect a downstream
	// network/API error rather than either "no source API token" or
	// "no destination API token" message.
	if err != nil && strings.Contains(err.Error(), "no source API token") {
		t.Errorf("with CIRCLECI_CLI_TOKEN set, should not get source token error; got: %v", err)
	}
	if err != nil && strings.Contains(err.Error(), "no destination API token") {
		t.Errorf("with CIRCLECI_CLI_TOKEN set, should not get destination token error; got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// buildMigrateMapping
// ---------------------------------------------------------------------------

// TestBuildMigrateMapping_NoFile_ConstructsFromOrgs verifies that when no
// mapping file path is supplied, buildMigrateMapping returns a mapping whose
// Org.From/To equal the provided org slugs.
func TestBuildMigrateMapping_NoFile_ConstructsFromOrgs(t *testing.T) {
	got, err := cmd.BuildMigrateMapping("", "gh/src", "gh/dst")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil mapping")
		return
	}
	if got.Org.From != "gh/src" {
		t.Errorf("Org.From = %q, want %q", got.Org.From, "gh/src")
	}
	if got.Org.To != "gh/dst" {
		t.Errorf("Org.To = %q, want %q", got.Org.To, "gh/dst")
	}
}

// TestBuildMigrateMapping_WithFile_LoadsFromDisk verifies that when a mapping
// file path is supplied, buildMigrateMapping reads and returns the mapping from
// disk.
func TestBuildMigrateMapping_WithFile_LoadsFromDisk(t *testing.T) {
	dir := t.TempDir()

	m := manifest.Mapping{
		SchemaVersion: "1",
		Org: manifest.OrgMapping{
			From: "gh/old",
			To:   "gh/new",
		},
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		t.Fatalf("marshal mapping: %v", err)
	}
	mappingPath := filepath.Join(dir, "mapping.json")
	if err := os.WriteFile(mappingPath, data, 0o644); err != nil {
		t.Fatalf("write mapping file: %v", err)
	}

	got, err := cmd.BuildMigrateMapping(mappingPath, "gh/ignored-src", "gh/ignored-dst")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil mapping")
		return
	}
	if got.Org.From != "gh/old" {
		t.Errorf("Org.From = %q, want %q", got.Org.From, "gh/old")
	}
	if got.Org.To != "gh/new" {
		t.Errorf("Org.To = %q, want %q", got.Org.To, "gh/new")
	}
}

// TestBuildMigrateMapping_MissingFile_ReturnsError verifies that a non-existent
// mapping file path is surfaced as an error.
func TestBuildMigrateMapping_MissingFile_ReturnsError(t *testing.T) {
	_, err := cmd.BuildMigrateMapping("/nonexistent/path/mapping.json", "gh/src", "gh/dst")
	if err == nil {
		t.Fatal("expected error for missing mapping file, got nil")
	}
}
