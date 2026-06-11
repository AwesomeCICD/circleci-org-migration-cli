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
	t.Setenv("CIRCLE_TOKEN", "")

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

// ---------------------------------------------------------------------------
// Lipgloss-styled help (issue #108)
// ---------------------------------------------------------------------------

// TestHelp_ContainsExpectedSections verifies that the lipgloss-styled help
// renderer includes all expected section headings and key content words.
// The test captures output from the in-process command so it exercises the
// real SetHelpFunc path without spawning a subprocess.
func TestHelp_ContainsExpectedSections(t *testing.T) {
	out, _, err := runCmd(t, "--help")
	if err != nil {
		t.Fatalf("--help returned error: %v", err)
	}

	// Global Flags only appear for sub-commands (root has no parent).
	sections := []string{
		"circleci-migrate",
		"Usage:",
		"Available Commands:",
		"Flags:",
	}

	for _, want := range sections {
		if !strings.Contains(out, want) {
			t.Errorf("--help output missing section %q\ngot:\n%s", want, out)
		}
	}
}

// TestHelp_ContainsSubcommandNames verifies that visible sub-commands appear
// in the root help output.
func TestHelp_ContainsSubcommandNames(t *testing.T) {
	out, _, err := runCmd(t, "--help")
	if err != nil {
		t.Fatalf("--help returned error: %v", err)
	}

	for _, sub := range []string{"export", "sync", "migrate", "version", "secrets", "orb"} {
		if !strings.Contains(out, sub) {
			t.Errorf("--help output missing subcommand %q\ngot:\n%s", sub, out)
		}
	}
}

// TestHelp_NoANSIEscapeCodes verifies that help output written to a non-TTY
// writer contains no ANSI escape sequences (lipgloss colour/bold codes).
// The runCmd helper captures output in a bytes.Buffer (not a TTY), so lipgloss
// should auto-strip all colour codes, giving clean plain text.
func TestHelp_NoANSIEscapeCodes(t *testing.T) {
	out, _, err := runCmd(t, "--help")
	if err != nil {
		t.Fatalf("--help returned error: %v", err)
	}
	if strings.Contains(out, "\x1b[") {
		t.Errorf("--help output contains ANSI escape codes when written to a non-TTY buffer:\n%q", out[:min(200, len(out))])
	}
}

// TestHelp_SubcommandInheritsStyle verifies that a sub-command also produces
// styled help (section headers present) and no ANSI codes on a non-TTY writer.
func TestHelp_SubcommandInheritsStyle(t *testing.T) {
	out, _, err := runCmd(t, "export", "--help")
	if err != nil {
		t.Fatalf("export --help returned error: %v", err)
	}

	for _, want := range []string{"circleci-migrate export", "Usage:", "Flags:"} {
		if !strings.Contains(out, want) {
			t.Errorf("export --help missing %q\ngot:\n%s", want, out)
		}
	}
	if strings.Contains(out, "\x1b[") {
		t.Errorf("export --help contains ANSI codes on non-TTY writer")
	}
}

// TestHelp_WorkflowExamplesPresent checks that the Typical workflow examples
// in the Long description are preserved in the root help output.
func TestHelp_WorkflowExamplesPresent(t *testing.T) {
	out, _, err := runCmd(t, "--help")
	if err != nil {
		t.Fatalf("--help returned error: %v", err)
	}
	for _, phrase := range []string{
		"Typical workflow",
		"Export the source organisation",
		"--source-token",
		"--dest-token",
	} {
		if !strings.Contains(out, phrase) {
			t.Errorf("--help output missing expected phrase %q", phrase)
		}
	}
}

// min is a local helper for Go versions before 1.21 where min is not built-in.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
