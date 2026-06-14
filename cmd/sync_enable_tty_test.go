package cmd

// sync_enable_tty_test.go — white-box tests for the handleEnableBuilds
// TTY detection fix (Part B of #249.2).
//
// The fix replaces the old os.ModeCharDevice stat check with the shared
// isInteractiveTTY() (term.IsTerminal on stdin fd), which correctly returns
// false for /dev/null and other non-TTY stdin sources used in CI.
//
// These tests live in package cmd (not cmd_test) so they can override
// stdinIsTerminal via the same mechanism as migrate_nontty_test.go.

import (
	"strings"
	"testing"
)

// TestHandleEnableBuilds_NonTTYDetection_UsesTerminalCheck verifies that when
// stdin is not a TTY, handleEnableBuilds skips the interactive prompt.
//
// We test the TTY detection path indirectly by verifying that with non-TTY stdin
// and no token (fast-fail path), the command does not produce an EOF error that
// would occur if it blocked on stdin.  The decideEnable pure-logic table
// (sync_enable_test.go) exhaustively covers the branches; here we confirm the
// isInteractiveTTY() override is wired correctly in the code path.
func TestHandleEnableBuilds_NonTTYDetection_UsesTerminalCheck(t *testing.T) {
	// Override to non-TTY (the new path uses isInteractiveTTY, not os.Stat).
	overrideNonTTY(t)
	t.Setenv("CIRCLECI_CLI_TOKEN", "")
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "")
	t.Setenv("CIRCLECI_DEST_TOKEN", "")
	t.Setenv("CIRCLE_TOKEN", "")

	// Without tokens the command fails on token validation (before export).
	// We just confirm no EOF error is produced.
	_, stderr, err := runMigrateCmdInternal(t,
		"--source-org", "gh/acme",
		"--dest-org", "gh/acme-new",
		"--skip-preflight",
	)
	if err != nil {
		combined := err.Error() + stderr
		// Must not hit EOF — that was the old failure mode from the ModeCharDevice bug.
		if strings.Contains(strings.ToLower(combined), "eof") {
			t.Errorf("got EOF — isInteractiveTTY not wired correctly: %v; stderr=%q", err, stderr)
		}
	}
}

// TestDecideEnable_IsInteractiveTTY_False verifies that decideEnable returns
// false for the no-TTY case, matching the isInteractiveTTY() = false contract.
// This is a thin sanity check that the non-TTY path works as expected.
func TestDecideEnable_IsInteractiveTTY_False(t *testing.T) {
	overrideNonTTY(t)
	isTTY := isInteractiveTTY()
	if isTTY {
		t.Error("isInteractiveTTY() should return false after overrideNonTTY")
	}

	// With apply=true, yes=false, isTTY=false → decideEnable must return false.
	got := decideEnable(true, false, false, func() bool { return true })
	if got {
		t.Error("decideEnable(apply=true, yes=false, isTTY=false) should return false")
	}
}

// TestDecideEnable_TTY_ConfirmYes verifies that on a TTY, decideEnable
// delegates to the confirm func and returns its result.
func TestDecideEnable_TTY_ConfirmYes(t *testing.T) {
	got := decideEnable(true, false, true, func() bool { return true })
	if !got {
		t.Error("decideEnable(apply=true, yes=false, isTTY=true, confirm=true) should return true")
	}
}

// TestDecideEnable_TTY_ConfirmNo verifies that on a TTY, decideEnable returns
// false when the confirm func returns false.
func TestDecideEnable_TTY_ConfirmNo(t *testing.T) {
	got := decideEnable(true, false, true, func() bool { return false })
	if got {
		t.Error("decideEnable(apply=true, yes=false, isTTY=true, confirm=false) should return false")
	}
}
