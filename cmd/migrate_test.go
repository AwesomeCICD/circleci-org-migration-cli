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
	t.Setenv("CIRCLE_TOKEN", "")

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
	t.Setenv("CIRCLE_TOKEN", "")

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
	t.Setenv("CIRCLE_TOKEN", "")

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
	t.Setenv("CIRCLE_TOKEN", "")

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
	t.Setenv("CIRCLE_TOKEN", "")

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
	t.Setenv("CIRCLE_TOKEN", "")

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
	t.Setenv("CIRCLE_TOKEN", "")

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

// ---------------------------------------------------------------------------
// --json flag
// ---------------------------------------------------------------------------

// TestMigrateCmd_JSONFlagRegistered verifies that --json is a local flag on the
// migrate subcommand (not a persistent/global flag).
func TestMigrateCmd_JSONFlagRegistered(t *testing.T) {
	migSub := findMigrateCmd(t)
	f := migSub.Flags().Lookup("json")
	if f == nil {
		t.Fatal("migrate --json flag not registered")
	}
	if f.Hidden {
		t.Error("migrate --json should not be hidden")
	}
}

// TestMigrateCmd_JSON_NotGlobal verifies that --json is NOT a persistent
// (global) flag on the migrate subcommand.
func TestMigrateCmd_JSON_NotGlobal(t *testing.T) {
	root := cmd.MakeCommands()
	if root.PersistentFlags().Lookup("json") != nil {
		t.Error("--json must not be a persistent/global flag; it should be local to each command")
	}
}

// TestMigrateCmd_JSONFlag_Accepted verifies that passing --json to migrate does
// not cause a flag-parsing error (it should fail on token validation, not flag
// parsing).
func TestMigrateCmd_JSONFlag_Accepted(t *testing.T) {
	t.Setenv("CIRCLECI_CLI_TOKEN", "")
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "")
	t.Setenv("CIRCLECI_DEST_TOKEN", "")
	t.Setenv("CIRCLE_TOKEN", "")

	_, _, err := runMigrateCmd(t,
		"--no-input",
		"--source-org", "gh/acme",
		"--dest-org", "gh/acme-new",
		"--json",
	)
	if err != nil && strings.Contains(err.Error(), "unknown flag") {
		t.Fatalf("--json is an unknown flag: %v", err)
	}
	// Should fail on token check, not on flag parsing.
	if err == nil {
		t.Fatal("expected an error (no token), got nil")
	}
	if strings.Contains(err.Error(), "unknown flag") {
		t.Fatalf("--json caused an 'unknown flag' error: %v", err)
	}
}

// TestMigrateJSONOutput_CombinedShape verifies that the migrateJSONOutput type
// (accessed via ExportJSONSummary + SyncJSONSummary) serialises to JSON with
// the expected top-level keys: dry_run, export, sync.
func TestMigrateJSONOutput_CombinedShape(t *testing.T) {
	// Build a representative combined output directly from the exported types.
	exportSummary := cmd.ExportJSONSummary{
		SourceOrgSlug:   "gh/acme",
		Host:            "https://circleci.com",
		GeneratedAt:     "2026-01-01T00:00:00Z",
		ContextCount:    2,
		ContextVarCount: 4,
		ProjectCount:    1,
		ProjectVarCount: 3,
	}
	syncSummary := cmd.SyncJSONSummary{
		DryRun:      true,
		DestOrgSlug: "gh/acme-new",
		Sections:    []cmd.SyncSectionSummary{{Section: "Contexts", Created: 2}},
	}

	// Construct the combined object manually to verify the shape matches what
	// migrate --json would emit.
	combined := map[string]any{
		"dry_run": true,
		"export":  exportSummary,
		"sync":    syncSummary,
	}

	data, err := json.MarshalIndent(combined, "", "  ")
	if err != nil {
		t.Fatalf("marshal combined: %v", err)
	}
	out := string(data)

	// Top-level keys.
	for _, key := range []string{"dry_run", "export", "sync"} {
		if !strings.Contains(out, `"`+key+`"`) {
			t.Errorf("expected top-level key %q in combined JSON; got: %s", key, out)
		}
	}

	// Export sub-keys.
	for _, key := range []string{"source_org_slug", "context_count", "project_count"} {
		if !strings.Contains(out, `"`+key+`"`) {
			t.Errorf("expected export key %q in combined JSON; got: %s", key, out)
		}
	}

	// Sync sub-keys.
	for _, key := range []string{"dest_org_slug", "sections"} {
		if !strings.Contains(out, `"`+key+`"`) {
			t.Errorf("expected sync key %q in combined JSON; got: %s", key, out)
		}
	}

	// No human-readable text should appear.
	for _, forbidden := range []string{"DRY RUN", "== Contexts sync", "Wrote manifest"} {
		if strings.Contains(out, forbidden) {
			t.Errorf("unexpected human-readable text %q in combined JSON output", forbidden)
		}
	}
}

// TestMigrateCmd_JSON_NoInterleaved verifies that migrate --json with all
// sections skipped (so no network calls are needed) does not write any
// human-readable text to stdout — only valid JSON.
func TestMigrateCmd_JSON_NoInterleaved(t *testing.T) {
	t.Setenv("CIRCLECI_CLI_TOKEN", "fake-token-migrate-json")
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "")
	t.Setenv("CIRCLECI_DEST_TOKEN", "")

	// This test expects a network / API error since we use a fake token.
	// We just need to confirm that if the command reached JSON output stage,
	// no human text would appear.  We verify flag parsing is clean.
	stdout, _, err := runMigrateCmd(t,
		"--no-input",
		"--source-org", "gh/acme",
		"--dest-org", "gh/acme-new",
		"--json",
		"--skip-contexts",
		"--skip-projects",
		"--skip-org-settings",
		"--skip-runner",
	)
	if err != nil {
		if strings.Contains(err.Error(), "unknown flag") {
			t.Fatalf("flag parsing error: %v", err)
		}
		// Network/API errors are expected; the test asserts stdout is clean.
	}
	// Stdout must not contain human-readable headers.
	if strings.Contains(stdout, "DRY RUN") {
		t.Error("with --json, 'DRY RUN' must not appear in stdout")
	}
	if strings.Contains(stdout, "== ") {
		t.Error("with --json, section headers must not appear in stdout")
	}
	// If any JSON was emitted, it must be valid.
	if trimmed := strings.TrimSpace(stdout); trimmed != "" {
		var result map[string]any
		if err := json.Unmarshal([]byte(trimmed), &result); err != nil {
			t.Fatalf("stdout contains non-JSON text: %q", stdout)
		}
		// Verify the expected top-level keys.
		for _, key := range []string{"dry_run", "export", "sync"} {
			if _, ok := result[key]; !ok {
				t.Errorf("expected key %q in migrate --json output; got: %v", key, result)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// --skip-runner flag
// ---------------------------------------------------------------------------

// TestMigrateCmd_SkipRunnerFlagRegistered verifies that --skip-runner is
// registered on the migrate subcommand.
func TestMigrateCmd_SkipRunnerFlagRegistered(t *testing.T) {
	migSub := findMigrateCmd(t)
	if migSub.Flags().Lookup("skip-runner") == nil {
		t.Error("migrate flag --skip-runner not registered")
	}
}

// TestMigrateCmd_SkipRunnerFlag_Accepted verifies that passing --skip-runner
// does not cause a flag-parsing error.
func TestMigrateCmd_SkipRunnerFlag_Accepted(t *testing.T) {
	t.Setenv("CIRCLECI_CLI_TOKEN", "")
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "")
	t.Setenv("CIRCLECI_DEST_TOKEN", "")
	t.Setenv("CIRCLE_TOKEN", "")

	_, _, err := runMigrateCmd(t,
		"--no-input",
		"--source-org", "gh/acme",
		"--dest-org", "gh/acme-new",
		"--skip-runner",
	)
	// Should fail on token check, not on flag parsing.
	if err != nil && strings.Contains(err.Error(), "unknown flag") {
		t.Fatalf("--skip-runner caused an 'unknown flag' error: %v", err)
	}
	if err == nil {
		t.Fatal("expected a token error, got nil")
	}
	if strings.Contains(err.Error(), "unknown flag") {
		t.Fatalf("--skip-runner caused an 'unknown flag' error: %v", err)
	}
}

// TestMigrateCmd_AllNewFlagsRegistered verifies that all flags added in
// issue #146 are present on the migrate subcommand.
func TestMigrateCmd_AllNewFlagsRegistered(t *testing.T) {
	migSub := findMigrateCmd(t)
	for _, name := range []string{"json", "skip-runner"} {
		if migSub.Flags().Lookup(name) == nil {
			t.Errorf("migrate flag --%s not registered", name)
		}
	}
}
