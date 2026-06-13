package cmd

// secrets_capture_trigger_flag_test.go — white-box unit tests for the
// guided org-trigger-flag detection introduced by issue #250.
//
// Tests live in package cmd (not cmd_test) so they can:
//   - override the unexported stdinIsTerminal var (TTY simulation)
//   - call checkAndMaybeEnableOrgTriggerFlag directly with a fake OrgFlagManager
//
// Four canonical scenarios:
//  1. flag OFF + interactive TTY, user says YES → enables flag + returns restore func
//  2. flag OFF + interactive TTY, user says NO  → clean error, no trigger
//  3. flag OFF + non-interactive                → fail-fast actionable error
//  4. flag already ON                           → proceeds, no prompt, no-op restore

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/capture"
	"github.com/spf13/cobra"
)

// ─────────────────────────────────────────────────────────────────────────────
// fake OrgFlagManager
// ─────────────────────────────────────────────────────────────────────────────

// fakeTriggerFlagMgr is a minimal test double for capture.OrgFlagManager.
type fakeTriggerFlagMgr struct {
	flags       map[string]bool
	updateCalls []map[string]bool
	getErr      error
}

func (f *fakeTriggerFlagMgr) GetFeatureFlags(_ context.Context, _, _ string) (map[string]bool, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	out := make(map[string]bool, len(f.flags))
	for k, v := range f.flags {
		out[k] = v
	}
	return out, nil
}

func (f *fakeTriggerFlagMgr) UpdateFeatureFlags(_ context.Context, _, _ string, flags map[string]bool) error {
	f.updateCalls = append(f.updateCalls, flags)
	for k, v := range flags {
		f.flags[k] = v
	}
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// helpers
// ─────────────────────────────────────────────────────────────────────────────

// overrideTTY replaces stdinIsTerminal with a stub that returns tty and
// restores the original via t.Cleanup.
func overrideTTY(t *testing.T, tty bool) {
	t.Helper()
	orig := stdinIsTerminal
	stdinIsTerminal = func() bool { return tty }
	t.Cleanup(func() { stdinIsTerminal = orig })
}

// newTestCobraCmd builds a minimal *cobra.Command backed by errBuf for stderr
// so the function can write output without a real process stream.
func newTestCobraCmd(errBuf *bytes.Buffer) *cobra.Command {
	cmd := &cobra.Command{}
	cmd.SetErr(errBuf)
	cmd.SetContext(context.Background())
	return cmd
}

// newTriggerTestFlags builds a minimal *captureFlags with enableTrigger set.
func newTriggerTestFlags(enableTrigger bool) *captureFlags {
	return &captureFlags{enableTrigger: enableTrigger}
}

// replaceTriggerStdin replaces os.Stdin with a pipe containing content and
// returns the original *os.File. Callers must call restoreTriggerStdin(orig)
// when done.
func replaceTriggerStdin(t *testing.T, content string) (orig *os.File) {
	t.Helper()
	orig = os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdin = r
	if _, werr := w.WriteString(content); werr != nil {
		t.Fatalf("write to stdin pipe: %v", werr)
	}
	if cerr := w.Close(); cerr != nil {
		t.Fatalf("close stdin pipe write-end: %v", cerr)
	}
	return orig
}

func restoreTriggerStdin(orig *os.File) {
	os.Stdin = orig
}

// ─────────────────────────────────────────────────────────────────────────────
// Scenario 1: flag OFF + interactive TTY, user says YES
// ─────────────────────────────────────────────────────────────────────────────

func TestCheckAndMaybeEnableOrgTriggerFlag_FlagOff_Interactive_Yes_EnablesAndRestores(t *testing.T) {
	overrideTTY(t, true)

	mgr := &fakeTriggerFlagMgr{flags: map[string]bool{capture.OrgAPITriggerKey: false}}

	var errBuf bytes.Buffer
	cobraCmd := newTestCobraCmd(&errBuf)
	cf := newTriggerTestFlags(false)

	// Feed "y\n" via stdin — simulate user typing "y" at the prompt.
	origStdin := replaceTriggerStdin(t, "y\n")
	defer restoreTriggerStdin(origStdin)

	restore, err := checkAndMaybeEnableOrgTriggerFlag(cobraCmd, cf, mgr, "github", "myorg")
	if err != nil {
		t.Fatalf("expected no error when user says yes, got: %v", err)
	}

	// Flag must have been enabled (one UpdateFeatureFlags call with true).
	if len(mgr.updateCalls) != 1 {
		t.Fatalf("expected 1 update call (enable), got %d: %v", len(mgr.updateCalls), mgr.updateCalls)
	}
	if !mgr.updateCalls[0][capture.OrgAPITriggerKey] {
		t.Error("first update call should enable the flag (true)")
	}

	// restore() should set the flag back to false.
	restore()
	if len(mgr.updateCalls) != 2 {
		t.Fatalf("expected 2 update calls (enable + restore), got %d", len(mgr.updateCalls))
	}
	if mgr.updateCalls[1][capture.OrgAPITriggerKey] {
		t.Error("restore call should set the flag to false")
	}

	// Stderr must contain a NOTICE about the flag being off.
	if !strings.Contains(errBuf.String(), "allow_api_trigger_with_config") {
		t.Errorf("stderr should mention the flag name; got: %s", errBuf.String())
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Scenario 2: flag OFF + interactive TTY, user says NO
// ─────────────────────────────────────────────────────────────────────────────

func TestCheckAndMaybeEnableOrgTriggerFlag_FlagOff_Interactive_No_ReturnsCleanError(t *testing.T) {
	overrideTTY(t, true)

	mgr := &fakeTriggerFlagMgr{flags: map[string]bool{capture.OrgAPITriggerKey: false}}

	var errBuf bytes.Buffer
	cobraCmd := newTestCobraCmd(&errBuf)
	cf := newTriggerTestFlags(false)

	origStdin := replaceTriggerStdin(t, "n\n")
	defer restoreTriggerStdin(origStdin)

	restore, err := checkAndMaybeEnableOrgTriggerFlag(cobraCmd, cf, mgr, "github", "myorg")

	// Must return a clean, actionable error.
	if err == nil {
		t.Fatal("expected error when user declines, got nil")
	}
	// Error must mention --enable-trigger and Org Settings.
	if !strings.Contains(err.Error(), "--enable-trigger") {
		t.Errorf("error should mention --enable-trigger; got: %v", err)
	}
	if !strings.Contains(err.Error(), "Org Settings") {
		t.Errorf("error should mention 'Org Settings'; got: %v", err)
	}

	// No flag update should have occurred.
	if len(mgr.updateCalls) != 0 {
		t.Errorf("expected 0 update calls when user says no, got %d: %v", len(mgr.updateCalls), mgr.updateCalls)
	}

	// restore should be a no-op.
	restore()
	if len(mgr.updateCalls) != 0 {
		t.Errorf("restore should be a no-op; got update calls: %v", mgr.updateCalls)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Scenario 3: flag OFF + non-interactive → fail-fast
// ─────────────────────────────────────────────────────────────────────────────

func TestCheckAndMaybeEnableOrgTriggerFlag_FlagOff_NonInteractive_FailsFast(t *testing.T) {
	overrideTTY(t, false)

	mgr := &fakeTriggerFlagMgr{flags: map[string]bool{capture.OrgAPITriggerKey: false}}

	var errBuf bytes.Buffer
	cobraCmd := newTestCobraCmd(&errBuf)
	cf := newTriggerTestFlags(false)

	restore, err := checkAndMaybeEnableOrgTriggerFlag(cobraCmd, cf, mgr, "github", "myorg")

	// Must return an actionable error immediately.
	if err == nil {
		t.Fatal("expected fail-fast error in non-interactive mode, got nil")
	}
	// Error must mention --enable-trigger.
	if !strings.Contains(err.Error(), "--enable-trigger") {
		t.Errorf("error should mention --enable-trigger; got: %v", err)
	}
	// Error must mention Org Settings.
	if !strings.Contains(err.Error(), "Org Settings") {
		t.Errorf("error should mention 'Org Settings'; got: %v", err)
	}
	// Error must mention the org identity.
	if !strings.Contains(err.Error(), "github/myorg") {
		t.Errorf("error should identify the org (github/myorg); got: %v", err)
	}

	// No flag update should have been attempted.
	if len(mgr.updateCalls) != 0 {
		t.Errorf("no UpdateFeatureFlags calls expected; got %d: %v", len(mgr.updateCalls), mgr.updateCalls)
	}

	// restore should be a no-op.
	restore()
	if len(mgr.updateCalls) != 0 {
		t.Errorf("restore must be a no-op; got update calls: %v", mgr.updateCalls)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Scenario 4: flag already ON → proceeds, no prompt, no-op restore
// ─────────────────────────────────────────────────────────────────────────────

func TestCheckAndMaybeEnableOrgTriggerFlag_FlagAlreadyOn_ProceedsNoPrompt(t *testing.T) {
	// TTY state does not matter when the flag is already ON.
	for _, tty := range []bool{true, false} {
		tty := tty
		t.Run(fmt.Sprintf("tty=%v", tty), func(t *testing.T) {
			overrideTTY(t, tty)

			mgr := &fakeTriggerFlagMgr{flags: map[string]bool{capture.OrgAPITriggerKey: true}}

			var errBuf bytes.Buffer
			cobraCmd := newTestCobraCmd(&errBuf)
			cf := newTriggerTestFlags(false)

			restore, err := checkAndMaybeEnableOrgTriggerFlag(cobraCmd, cf, mgr, "github", "myorg")
			if err != nil {
				t.Fatalf("expected no error when flag is already ON, got: %v", err)
			}

			// No update calls — flag was already on.
			if len(mgr.updateCalls) != 0 {
				t.Errorf("expected 0 update calls, got %d: %v", len(mgr.updateCalls), mgr.updateCalls)
			}

			// restore is a no-op.
			restore()
			if len(mgr.updateCalls) != 0 {
				t.Errorf("restore should be a no-op; got update calls: %v", mgr.updateCalls)
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// enableTrigger=true: delegates to MaybeEnableOrgTriggerFlag unchanged
// ─────────────────────────────────────────────────────────────────────────────

func TestCheckAndMaybeEnableOrgTriggerFlag_EnableTriggerFlag_EnablesAndRestores(t *testing.T) {
	// When --enable-trigger is set the flag must be enabled regardless of TTY
	// state. Test with non-TTY to confirm the TTY check is bypassed.
	overrideTTY(t, false)

	mgr := &fakeTriggerFlagMgr{flags: map[string]bool{capture.OrgAPITriggerKey: false}}

	var errBuf bytes.Buffer
	cobraCmd := newTestCobraCmd(&errBuf)
	cf := newTriggerTestFlags(true) // --enable-trigger

	restore, err := checkAndMaybeEnableOrgTriggerFlag(cobraCmd, cf, mgr, "github", "myorg")
	if err != nil {
		t.Fatalf("expected no error with --enable-trigger, got: %v", err)
	}

	// Flag must have been enabled.
	if len(mgr.updateCalls) != 1 {
		t.Fatalf("expected 1 update call (enable), got %d: %v", len(mgr.updateCalls), mgr.updateCalls)
	}
	if !mgr.updateCalls[0][capture.OrgAPITriggerKey] {
		t.Error("first update call should enable the flag (true)")
	}

	// restore() must set it back to false.
	restore()
	if len(mgr.updateCalls) != 2 {
		t.Fatalf("expected 2 update calls (enable + restore), got %d", len(mgr.updateCalls))
	}
	if mgr.updateCalls[1][capture.OrgAPITriggerKey] {
		t.Error("restore call should set flag to false")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Read error → warn + no-op (best-effort)
// ─────────────────────────────────────────────────────────────────────────────

func TestCheckAndMaybeEnableOrgTriggerFlag_ReadError_WarnsAndNoOp(t *testing.T) {
	overrideTTY(t, false)

	mgr := &fakeTriggerFlagMgr{getErr: fmt.Errorf("network timeout")}

	var errBuf bytes.Buffer
	cobraCmd := newTestCobraCmd(&errBuf)
	cf := newTriggerTestFlags(false)

	restore, err := checkAndMaybeEnableOrgTriggerFlag(cobraCmd, cf, mgr, "github", "myorg")
	if err != nil {
		t.Fatalf("read error should not hard-block; expected nil, got: %v", err)
	}

	// Must warn.
	if !strings.Contains(errBuf.String(), "WARNING") {
		t.Errorf("expected WARNING in stderr on read error; got: %s", errBuf.String())
	}

	// No update calls.
	if len(mgr.updateCalls) != 0 {
		t.Errorf("no updates expected on read error; got: %v", mgr.updateCalls)
	}

	// restore is a no-op.
	restore()
	if len(mgr.updateCalls) != 0 {
		t.Errorf("restore must be a no-op; got update calls: %v", mgr.updateCalls)
	}
}
