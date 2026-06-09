package cmd_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/CircleCI-Public/circleci-org-migration-cli/cmd"
	"github.com/CircleCI-Public/circleci-org-migration-cli/version"
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

func TestSyncCommand_NotImplementedMessage(t *testing.T) {
	out, _, err := runCmd(t, "sync")
	if err != nil {
		t.Fatalf("sync command error: %v", err)
	}
	if !strings.Contains(out, "not implemented") {
		t.Errorf("sync output %q does not contain 'not implemented'", out)
	}
}

// ---------------------------------------------------------------------------
// migrate subcommand
// ---------------------------------------------------------------------------

func TestMigrateCommand_NotImplementedMessage(t *testing.T) {
	out, _, err := runCmd(t, "migrate")
	if err != nil {
		t.Fatalf("migrate command error: %v", err)
	}
	if !strings.Contains(out, "not implemented") {
		t.Errorf("migrate output %q does not contain 'not implemented'", out)
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
