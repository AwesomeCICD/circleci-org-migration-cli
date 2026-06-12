package cmd

// migrate_nontty_test.go verifies the non-TTY fast-fail gate introduced for
// GitHub issue #217.
//
// The gate must:
//  1. Fire BEFORE any banner or prompt output is written.
//  2. Return a clear, actionable error (mentioning TTY / org flags).
//  3. NOT produce the banner text (e.g. "guided mode").
//  4. NOT produce "EOF" — the old failure mode when the walkthrough ran on a
//     non-TTY and the prompter hit io.EOF while waiting for input.
//
// These tests live in package cmd (white-box) rather than package cmd_test so
// they can override the stdinIsTerminal var without exporting it.

import (
	"strings"
	"testing"
)

// overrideNonTTY replaces stdinIsTerminal with a stub that always returns
// false (simulating a non-TTY stdin such as /dev/null or a CI pipe) and
// restores the original via t.Cleanup.
func overrideNonTTY(t *testing.T) {
	t.Helper()
	orig := stdinIsTerminal
	stdinIsTerminal = func() bool { return false }
	t.Cleanup(func() { stdinIsTerminal = orig })
}

// runMigrateCmdInternal executes the migrate subcommand using the internal
// MakeCommands constructor and returns stdout, stderr, and any error.
// It is a white-box equivalent of the cmd_test.runMigrateCmd helper.
func runMigrateCmdInternal(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	root := MakeCommands()

	var outBuf, errBuf strings.Builder
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	root.SetArgs(append([]string{"migrate"}, args...))

	err = root.Execute()
	return outBuf.String(), errBuf.String(), err
}

// ---------------------------------------------------------------------------
// Non-TTY fast-fail gate (#217)
// ---------------------------------------------------------------------------

// TestMigrateCmd_NonTTY_NoOrgFlags_FailsFast is the primary regression test
// for issue #217. When migrate is called without --source-org and --dest-org
// on a non-TTY stdin (e.g. CI, stdin=/dev/null), it must:
//   - return a non-nil, actionable error mentioning TTY and the org flags
//   - NOT write any banner text to stderr
//   - NOT return an "EOF" error
func TestMigrateCmd_NonTTY_NoOrgFlags_FailsFast(t *testing.T) {
	overrideNonTTY(t)
	t.Setenv("CIRCLECI_CLI_TOKEN", "")
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "")
	t.Setenv("CIRCLECI_DEST_TOKEN", "")
	t.Setenv("CIRCLE_TOKEN", "")

	stdout, stderr, err := runMigrateCmdInternal(t)

	// Must return an error.
	if err == nil {
		t.Fatal("expected error on non-TTY stdin with no org flags, got nil")
	}

	// Error must be actionable — mention TTY (or interactive) and the flags.
	errMsg := err.Error()
	if !strings.Contains(strings.ToLower(errMsg), "tty") &&
		!strings.Contains(strings.ToLower(errMsg), "interactive") {
		t.Errorf("error must mention TTY or interactive mode; got: %q", errMsg)
	}
	if !strings.Contains(errMsg, "source-org") && !strings.Contains(errMsg, "dest-org") {
		t.Errorf("error must mention --source-org or --dest-org flags; got: %q", errMsg)
	}

	// Must NOT be an EOF error — that was the old failure mode.
	if strings.Contains(strings.ToLower(errMsg), "eof") {
		t.Errorf("error must not be an EOF error (old failure mode); got: %q", errMsg)
	}

	// Must NOT have printed the guided-mode banner.
	combined := stdout + stderr
	if strings.Contains(combined, "guided mode") {
		t.Errorf("banner ('guided mode') must not appear before the TTY check fires; got stderr: %q", stderr)
	}
	if strings.Contains(combined, "CircleCI Organization Migration") {
		t.Errorf("banner ('CircleCI Organization Migration') must not appear before the TTY check; got stderr: %q", stderr)
	}

	// Standard check: nothing written to stdout.
	if stdout != "" {
		t.Errorf("expected empty stdout, got: %q", stdout)
	}
}

// TestMigrateCmd_NonTTY_OnlySourceOrg_FailsFast verifies that even when
// --source-org is supplied but --dest-org is absent on a non-TTY, the command
// still fast-fails with the actionable error before any banner.
func TestMigrateCmd_NonTTY_OnlySourceOrg_FailsFast(t *testing.T) {
	overrideNonTTY(t)
	t.Setenv("CIRCLECI_CLI_TOKEN", "")
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "")
	t.Setenv("CIRCLECI_DEST_TOKEN", "")
	t.Setenv("CIRCLE_TOKEN", "")

	_, stderr, err := runMigrateCmdInternal(t, "--source-org", "gh/acme")

	if err == nil {
		t.Fatal("expected error on non-TTY stdin with missing --dest-org, got nil")
	}

	errMsg := err.Error()
	// Must mention the TTY / interactive requirement.
	if !strings.Contains(strings.ToLower(errMsg), "tty") &&
		!strings.Contains(strings.ToLower(errMsg), "interactive") {
		t.Errorf("error must mention TTY or interactive mode; got: %q", errMsg)
	}

	// Must NOT print the banner.
	if strings.Contains(stderr, "guided mode") || strings.Contains(stderr, "CircleCI Organization Migration") {
		t.Errorf("banner must not appear on non-TTY fast-fail; got stderr: %q", stderr)
	}

	// Must NOT return EOF.
	if strings.Contains(strings.ToLower(errMsg), "eof") {
		t.Errorf("error must not be an EOF error; got: %q", errMsg)
	}
}

// TestMigrateCmd_NonTTY_BothOrgsProvided_NoFailFast verifies that when BOTH
// org flags are supplied on a non-TTY, the non-TTY gate does NOT fire: the
// command proceeds past it and eventually fails on token validation (not on
// the TTY check), confirming the gate is correctly scoped to the interactive
// path only.
func TestMigrateCmd_NonTTY_BothOrgsProvided_NoFailFast(t *testing.T) {
	overrideNonTTY(t)
	t.Setenv("CIRCLECI_CLI_TOKEN", "")
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "")
	t.Setenv("CIRCLECI_DEST_TOKEN", "")
	t.Setenv("CIRCLE_TOKEN", "")

	_, _, err := runMigrateCmdInternal(t, "--source-org", "gh/acme", "--dest-org", "gh/acme-new")

	// Must still error (no token), but NOT with the TTY check message.
	if err == nil {
		t.Fatal("expected token error, got nil")
	}
	errMsg := err.Error()
	if strings.Contains(strings.ToLower(errMsg), "tty") {
		t.Errorf("TTY check must not fire when both org flags are provided; got: %q", errMsg)
	}
	if !strings.Contains(errMsg, "token") {
		t.Errorf("expected token validation error when both orgs provided; got: %q", errMsg)
	}
}

// TestMigrateCmd_NonTTY_NoInput_BothOrgs_NoFailFast verifies that --no-input
// with both org flags on non-TTY also bypasses the TTY gate and falls through
// to token validation.
func TestMigrateCmd_NonTTY_NoInput_BothOrgs_NoFailFast(t *testing.T) {
	overrideNonTTY(t)
	t.Setenv("CIRCLECI_CLI_TOKEN", "")
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "")
	t.Setenv("CIRCLECI_DEST_TOKEN", "")
	t.Setenv("CIRCLE_TOKEN", "")

	_, _, err := runMigrateCmdInternal(t,
		"--no-input",
		"--source-org", "gh/acme",
		"--dest-org", "gh/acme-new",
	)

	if err == nil {
		t.Fatal("expected token error, got nil")
	}
	errMsg := err.Error()
	if strings.Contains(strings.ToLower(errMsg), "tty") {
		t.Errorf("TTY check must not fire when both orgs+--no-input are provided; got: %q", errMsg)
	}
	if !strings.Contains(errMsg, "token") {
		t.Errorf("expected token validation error; got: %q", errMsg)
	}
}
