package cmd

// secrets_transfer_test.go — white-box unit tests for the new Fix 1 & Fix 2
// helpers introduced in cmd/secrets_transfer.go.
//
// Tests live in package cmd (not cmd_test) so they can call unexported
// functions (triggerFlagHook, maybeEnableOrgTrigger,
// maybeEnableProjectTrigger) and override stdinIsTerminal.

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/capture"
)

// ─────────────────────────────────────────────────────────────────────────────
// Fake capture.FlagReaderWriter
// ─────────────────────────────────────────────────────────────────────────────

// fakeProjFlagClient is a minimal test double for capture.FlagReaderWriter.
type fakeProjFlagClient struct {
	flags    map[string]map[string]bool // slug → flag map
	setCalls map[string][]map[string]bool
	getErr   error
	setErr   error
}

func newFakeProjFlagClient() *fakeProjFlagClient {
	return &fakeProjFlagClient{
		flags:    make(map[string]map[string]bool),
		setCalls: make(map[string][]map[string]bool),
	}
}

func (f *fakeProjFlagClient) withFlags(slug string, flags map[string]bool) *fakeProjFlagClient {
	f.flags[slug] = flags
	return f
}

func (f *fakeProjFlagClient) GetV11ProjectFeatureFlags(_ context.Context, slug string) (map[string]bool, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	out := make(map[string]bool)
	for k, v := range f.flags[slug] {
		out[k] = v
	}
	return out, nil
}

func (f *fakeProjFlagClient) SetV11ProjectFeatureFlags(_ context.Context, slug string, flags map[string]bool) error {
	if f.setErr != nil {
		return f.setErr
	}
	if f.flags[slug] == nil {
		f.flags[slug] = make(map[string]bool)
	}
	for k, v := range flags {
		f.flags[slug][k] = v
	}
	f.setCalls[slug] = append(f.setCalls[slug], flags)
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// triggerFlagHook (transfer.Options.BeforeMutate)
// ─────────────────────────────────────────────────────────────────────────────

// TestTriggerFlagHook_EnablesOrgAndProjectFlagsThenRestoresInReverse verifies
// the hook enables the org flag and each project flag, and its cleanup restores
// the project flags before the org flag.
func TestTriggerFlagHook_EnablesOrgAndProjectFlagsThenRestoresInReverse(t *testing.T) {
	overrideTTY(t, false)
	orgMgr := &fakeTriggerFlagMgr{flags: map[string]bool{capture.OrgAPITriggerKey: false}}
	proj := newFakeProjFlagClient().
		withFlags("gh/old-org/web", map[string]bool{projectTriggerKey: false}).
		withFlags("gh/old-org/api", map[string]bool{projectTriggerKey: false})
	var errBuf bytes.Buffer
	cmd := newTestCobraCmd(&errBuf)

	hook := triggerFlagHook(cmd, orgMgr, "gh/old-org", proj, true)
	cleanup, err := hook([]string{"gh/old-org/web", "gh/old-org/api"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !orgMgr.flags[capture.OrgAPITriggerKey] || !proj.flags["gh/old-org/web"][projectTriggerKey] || !proj.flags["gh/old-org/api"][projectTriggerKey] {
		t.Fatalf("expected org and both project flags enabled; org=%v proj=%v", orgMgr.flags, proj.flags)
	}

	cleanup()
	if orgMgr.flags[capture.OrgAPITriggerKey] || proj.flags["gh/old-org/web"][projectTriggerKey] || proj.flags["gh/old-org/api"][projectTriggerKey] {
		t.Errorf("expected all flags restored to false; org=%v proj=%v", orgMgr.flags, proj.flags)
	}
	out := errBuf.String()
	if strings.Index(out, "Restoring api-trigger-with-config=false for project gh/old-org/api") > strings.Index(out, "Restoring org-level") {
		t.Errorf("project flags must be restored before the org flag; got:\n%s", out)
	}
}

// TestTriggerFlagHook_FlagOff_NoEnableTrigger_ErrorsWithoutChanges verifies the
// hook fails (non-interactive, no --enable-trigger) without flipping anything.
func TestTriggerFlagHook_FlagOff_NoEnableTrigger_ErrorsWithoutChanges(t *testing.T) {
	overrideTTY(t, false)
	orgMgr := &fakeTriggerFlagMgr{flags: map[string]bool{capture.OrgAPITriggerKey: false}}
	proj := newFakeProjFlagClient()
	cmd := newTestCobraCmd(&bytes.Buffer{})

	hook := triggerFlagHook(cmd, orgMgr, "gh/old-org", proj, false)
	if _, err := hook([]string{"gh/old-org/web"}); err == nil {
		t.Fatal("expected error when the org flag is off and --enable-trigger is not set")
	}
	if len(orgMgr.updateCalls) != 0 || len(proj.setCalls) != 0 {
		t.Errorf("no flag may change on failure; org=%v proj=%v", orgMgr.updateCalls, proj.setCalls)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// maybeEnableOrgTrigger
// ─────────────────────────────────────────────────────────────────────────────

// TestMaybeEnableOrgTrigger_AlreadyOn verifies that when the org-level flag is
// already enabled, no update is made and the returned bool is true.
func TestMaybeEnableOrgTrigger_AlreadyOn(t *testing.T) {
	overrideTTY(t, false)
	mgr := &fakeTriggerFlagMgr{flags: map[string]bool{capture.OrgAPITriggerKey: true}}
	var errBuf bytes.Buffer
	cmd := newTestCobraCmd(&errBuf)

	enabled, restore, err := maybeEnableOrgTrigger(cmd, mgr, "gh", "myorg", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !enabled {
		t.Error("enabled should be true when flag is already on")
	}
	if restore != nil {
		t.Error("restore should be nil when no change was made")
	}
	if len(mgr.updateCalls) != 0 {
		t.Errorf("no update calls expected; got %d", len(mgr.updateCalls))
	}
}

// TestMaybeEnableOrgTrigger_EnableTrigger_EnablesAndRestores verifies that
// --enable-trigger=true enables the flag and returns a restore function.
func TestMaybeEnableOrgTrigger_EnableTrigger_EnablesAndRestores(t *testing.T) {
	overrideTTY(t, false) // non-interactive; enableTrigger must bypass TTY check
	mgr := &fakeTriggerFlagMgr{flags: map[string]bool{capture.OrgAPITriggerKey: false}}
	var errBuf bytes.Buffer
	cmd := newTestCobraCmd(&errBuf)

	enabled, restore, err := maybeEnableOrgTrigger(cmd, mgr, "gh", "myorg", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if enabled {
		t.Error("enabled should be false (flag was off before we enabled it)")
	}
	if restore == nil {
		t.Fatal("restore func expected")
	}
	if len(mgr.updateCalls) != 1 || !mgr.updateCalls[0][capture.OrgAPITriggerKey] {
		t.Errorf("expected one enable call; got %v", mgr.updateCalls)
	}

	restore()
	if len(mgr.updateCalls) != 2 || mgr.updateCalls[1][capture.OrgAPITriggerKey] {
		t.Errorf("expected restore to disable flag; calls = %v", mgr.updateCalls)
	}
}

// TestMaybeEnableOrgTrigger_NonInteractive_FlagOff_Errors verifies that when
// the flag is off and neither interactive nor --enable-trigger, a hard error
// is returned.
func TestMaybeEnableOrgTrigger_NonInteractive_FlagOff_Errors(t *testing.T) {
	overrideTTY(t, false)
	mgr := &fakeTriggerFlagMgr{flags: map[string]bool{capture.OrgAPITriggerKey: false}}
	var errBuf bytes.Buffer
	cmd := newTestCobraCmd(&errBuf)

	_, restore, err := maybeEnableOrgTrigger(cmd, mgr, "gh", "myorg", false)
	if err == nil {
		t.Fatal("expected error in non-interactive mode with flag off")
	}
	if restore != nil {
		t.Error("restore should be nil when an error is returned")
	}
}

// TestMaybeEnableOrgTrigger_Interactive_Yes_EnablesAndRestores verifies the
// interactive YES path.
func TestMaybeEnableOrgTrigger_Interactive_Yes_EnablesAndRestores(t *testing.T) {
	overrideTTY(t, true)
	mgr := &fakeTriggerFlagMgr{flags: map[string]bool{capture.OrgAPITriggerKey: false}}
	var errBuf bytes.Buffer
	cmd := newTestCobraCmd(&errBuf)

	origStdin := replaceTriggerStdin(t, "y\n")
	defer restoreTriggerStdin(origStdin)

	enabled, restore, err := maybeEnableOrgTrigger(cmd, mgr, "gh", "myorg", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if enabled {
		t.Error("enabled should be false (flag was off before we enabled it)")
	}
	if restore == nil {
		t.Fatal("restore func expected")
	}
	if len(mgr.updateCalls) != 1 || !mgr.updateCalls[0][capture.OrgAPITriggerKey] {
		t.Errorf("expected enable call; got %v", mgr.updateCalls)
	}
	restore()
}

// TestMaybeEnableOrgTrigger_Interactive_No_Errors verifies the interactive NO
// path returns a clean error.
func TestMaybeEnableOrgTrigger_Interactive_No_Errors(t *testing.T) {
	overrideTTY(t, true)
	mgr := &fakeTriggerFlagMgr{flags: map[string]bool{capture.OrgAPITriggerKey: false}}
	var errBuf bytes.Buffer
	cmd := newTestCobraCmd(&errBuf)

	origStdin := replaceTriggerStdin(t, "n\n")
	defer restoreTriggerStdin(origStdin)

	_, restore, err := maybeEnableOrgTrigger(cmd, mgr, "gh", "myorg", false)
	if err == nil {
		t.Fatal("expected error when user declines")
	}
	if restore != nil {
		t.Error("restore should be nil on error")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// maybeEnableProjectTrigger
// ─────────────────────────────────────────────────────────────────────────────

const projectTriggerKey = "api-trigger-with-config"

// TestMaybeEnableProjectTrigger_AlreadyOn verifies that no update occurs and
// restore is nil when the flag is already enabled.
func TestMaybeEnableProjectTrigger_AlreadyOn(t *testing.T) {
	overrideTTY(t, false)
	client := newFakeProjFlagClient().withFlags("gh/old-org/web", map[string]bool{projectTriggerKey: true})
	var errBuf bytes.Buffer
	cmd := newTestCobraCmd(&errBuf)

	restore, err := maybeEnableProjectTrigger(cmd, client, "gh/old-org/web", false, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if restore != nil {
		t.Error("restore should be nil when flag is already on")
	}
	if len(client.setCalls["gh/old-org/web"]) != 0 {
		t.Errorf("no set calls expected; got %v", client.setCalls)
	}
}

// TestMaybeEnableProjectTrigger_EnableTrigger_EnablesAndRestores verifies the
// auto-enable path with --enable-trigger=true.
func TestMaybeEnableProjectTrigger_EnableTrigger_EnablesAndRestores(t *testing.T) {
	overrideTTY(t, false)
	client := newFakeProjFlagClient().withFlags("gh/old-org/web", map[string]bool{projectTriggerKey: false})
	var errBuf bytes.Buffer
	cmd := newTestCobraCmd(&errBuf)

	restore, err := maybeEnableProjectTrigger(cmd, client, "gh/old-org/web", true, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if restore == nil {
		t.Fatal("restore func expected")
	}
	calls := client.setCalls["gh/old-org/web"]
	if len(calls) != 1 || !calls[0][projectTriggerKey] {
		t.Errorf("expected one enable call; got %v", calls)
	}

	restore()
	calls = client.setCalls["gh/old-org/web"]
	if len(calls) != 2 || calls[1][projectTriggerKey] {
		t.Errorf("expected restore to disable; got %v", calls)
	}
}

// TestMaybeEnableProjectTrigger_NonInteractive_FlagOff_Errors verifies a hard
// error is returned in non-interactive, no --enable-trigger mode.
func TestMaybeEnableProjectTrigger_NonInteractive_FlagOff_Errors(t *testing.T) {
	overrideTTY(t, false)
	client := newFakeProjFlagClient().withFlags("gh/old-org/web", map[string]bool{projectTriggerKey: false})
	var errBuf bytes.Buffer
	cmd := newTestCobraCmd(&errBuf)

	restore, err := maybeEnableProjectTrigger(cmd, client, "gh/old-org/web", false, false)
	if err == nil {
		t.Fatal("expected error in non-interactive mode with flag off")
	}
	if restore != nil {
		t.Error("restore should be nil on error")
	}
}

// TestMaybeEnableProjectTrigger_ReadError_WarnsAndNoOp verifies that a flag
// read error is treated as non-fatal: a warning is printed and nil restore/error
// is returned so the pipeline trigger can still run (and fail clearly).
func TestMaybeEnableProjectTrigger_ReadError_WarnsAndNoOp(t *testing.T) {
	overrideTTY(t, false)
	client := newFakeProjFlagClient()
	client.getErr = fmt.Errorf("network error")
	var errBuf bytes.Buffer
	cmd := newTestCobraCmd(&errBuf)

	restore, err := maybeEnableProjectTrigger(cmd, client, "gh/old-org/web", false, false)
	if err != nil {
		t.Fatalf("read error should not hard-fail; got: %v", err)
	}
	if restore != nil {
		t.Error("restore should be nil when no change was made")
	}
	if !containsAny(errBuf.String(), "WARNING", "warning") {
		t.Errorf("expected WARNING in stderr; got: %s", errBuf.String())
	}
}

// TestMaybeEnableProjectTrigger_Interactive_Yes verifies the interactive YES
// path enables the flag and returns a restore function.
func TestMaybeEnableProjectTrigger_Interactive_Yes(t *testing.T) {
	overrideTTY(t, true)
	client := newFakeProjFlagClient().withFlags("gh/old-org/web", map[string]bool{projectTriggerKey: false})
	var errBuf bytes.Buffer
	cmd := newTestCobraCmd(&errBuf)

	origStdin := replaceTriggerStdin(t, "y\n")
	defer restoreTriggerStdin(origStdin)

	restore, err := maybeEnableProjectTrigger(cmd, client, "gh/old-org/web", false, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if restore == nil {
		t.Fatal("restore func expected")
	}
	calls := client.setCalls["gh/old-org/web"]
	if len(calls) != 1 || !calls[0][projectTriggerKey] {
		t.Errorf("expected enable call; got %v", calls)
	}
	restore()
}

// TestMaybeEnableProjectTrigger_Interactive_No verifies the interactive NO
// path returns a clean error.
func TestMaybeEnableProjectTrigger_Interactive_No(t *testing.T) {
	overrideTTY(t, true)
	client := newFakeProjFlagClient().withFlags("gh/old-org/web", map[string]bool{projectTriggerKey: false})
	var errBuf bytes.Buffer
	cmd := newTestCobraCmd(&errBuf)

	origStdin := replaceTriggerStdin(t, "n\n")
	defer restoreTriggerStdin(origStdin)

	restore, err := maybeEnableProjectTrigger(cmd, client, "gh/old-org/web", false, false)
	if err == nil {
		t.Fatal("expected error when user declines")
	}
	if restore != nil {
		t.Error("restore should be nil on error")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

// containsAny returns true if s contains any of the given substrings.
func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
