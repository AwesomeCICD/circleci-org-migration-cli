package cmd_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/AwesomeCICD/circleci-org-migration-cli/cmd"
	"github.com/AwesomeCICD/circleci-org-migration-cli/version"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func runCmd(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	root := cmd.MakeCommands()

	var outBuf, errBuf bytes.Buffer
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	root.SetArgs(args)

	err = root.Execute()
	return outBuf.String(), errBuf.String(), err
}

// ---------------------------------------------------------------------------
// Subcommand registration
// ---------------------------------------------------------------------------

func TestMakeCommands_HasSubcommands(t *testing.T) {
	root := cmd.MakeCommands()
	wantSubs := []string{"version", "export", "sync", "migrate"}
	found := map[string]bool{}
	for _, sub := range root.Commands() {
		found[sub.Use] = true
	}
	// cobra's Use field may include arg placeholders like "sync <manifest>".
	// We match on the first word.
	for _, want := range wantSubs {
		matched := false
		for use := range found {
			if strings.HasPrefix(use, want) {
				matched = true
				break
			}
		}
		if !matched {
			t.Errorf("subcommand %q not found; registered: %v", want, root.Commands())
		}
	}
}

// ---------------------------------------------------------------------------
// Global flag registration
// ---------------------------------------------------------------------------

func TestMakeCommands_GlobalFlagsRegistered(t *testing.T) {
	root := cmd.MakeCommands()
	wantFlags := []string{"host", "token", "source-token", "dest-token", "debug"}
	pf := root.PersistentFlags()
	for _, name := range wantFlags {
		if pf.Lookup(name) == nil {
			t.Errorf("persistent flag --%s not registered", name)
		}
	}
}

// ---------------------------------------------------------------------------
// version subcommand
// ---------------------------------------------------------------------------

func TestVersionCommand_PrintsVersionInfo(t *testing.T) {
	out, _, err := runCmd(t, "version")
	if err != nil {
		t.Fatalf("version command error: %v", err)
	}
	if !strings.Contains(out, version.Version) {
		t.Errorf("output %q does not contain version %q", out, version.Version)
	}
	if !strings.Contains(out, "circleci-migrate") {
		t.Errorf("output %q does not contain product name 'circleci-migrate'", out)
	}
}

func TestVersionCommand_PrintsCommit(t *testing.T) {
	out, _, err := runCmd(t, "version")
	if err != nil {
		t.Fatalf("version command error: %v", err)
	}
	if !strings.Contains(out, version.Commit) {
		t.Errorf("output %q does not contain commit %q", out, version.Commit)
	}
}

// ---------------------------------------------------------------------------
// export subcommand
// ---------------------------------------------------------------------------

func TestExportCommand_RequiresOrgFlag(t *testing.T) {
	_, _, err := runCmd(t, "export")
	if err == nil {
		t.Fatal("export without --org should return an error")
	}
	if !strings.Contains(err.Error(), "org") {
		t.Errorf("error %q does not mention 'org'", err.Error())
	}
}

// ---------------------------------------------------------------------------
// sync subcommand
// ---------------------------------------------------------------------------

func TestSyncCommand_RequiresManifest(t *testing.T) {
	// The sync command is implemented and requires --manifest.
	_, _, err := runCmd(t, "sync")
	if err == nil {
		t.Fatal("sync without --manifest should return an error")
	}
	if !strings.Contains(err.Error(), "manifest") {
		t.Errorf("error %q does not mention 'manifest'", err.Error())
	}
}

// ---------------------------------------------------------------------------
// migrate subcommand
// ---------------------------------------------------------------------------

func TestMigrateCommand_NoSourceOrg_ReturnsError(t *testing.T) {
	t.Setenv("CIRCLECI_CLI_TOKEN", "")
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "")
	t.Setenv("CIRCLECI_DEST_TOKEN", "")

	// --no-input prevents the interactive walkthrough from triggering (the test
	// process stdin may appear as a char device depending on the OS/runner).
	_, _, err := runCmd(t, "migrate", "--no-input")
	if err == nil {
		t.Fatal("migrate without --source-org should return an error")
	}
	if !strings.Contains(err.Error(), "source-org") {
		t.Errorf("error %q does not mention 'source-org'", err.Error())
	}
}

// ---------------------------------------------------------------------------
// Help / no-args
// ---------------------------------------------------------------------------

func TestRootCommand_HelpDoesNotError(t *testing.T) {
	_, _, err := runCmd(t, "--help")
	if err != nil {
		t.Fatalf("--help returned error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Exit-code normalisation (issue #78)
// ---------------------------------------------------------------------------

// TestExecute_ReturnsErrorOnBadArgs verifies that Execute() returns a non-nil
// error when a subcommand is invoked with invalid arguments. main.go maps this
// to os.Exit(1) — not os.Exit(-1) / os.Exit(255).
func TestExecute_ReturnsErrorOnBadArgs(t *testing.T) {
	// "secrets merge" with no positional args must return an error so that main
	// calls os.Exit(1). The exit code itself cannot be tested in-process, but
	// we confirm the error path is taken.
	_, _, err := runCmd(t, "secrets", "merge")
	if err == nil {
		t.Fatal("expected Execute to return an error for 'secrets merge' with no args")
	}
}

// TestExecute_NoErrorOnHelp verifies that --help does NOT return an error,
// consistent with standard CLI behaviour (exit 0 on --help).
func TestExecute_NoErrorOnHelp(t *testing.T) {
	_, _, err := runCmd(t, "secrets", "merge", "--help")
	if err != nil {
		t.Fatalf("'secrets merge --help' should return no error, got: %v", err)
	}
}
