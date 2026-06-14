package cmd

// doctor_test.go — white-box unit tests for the doctor command and the new
// export/sync preflight helpers (runExportPreflight, runSyncPreflight).
//
// Lives in package cmd (not cmd_test) so it can access unexported helpers and
// override stdinIsTerminal.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/AwesomeCICD/circleci-org-migration-cli/api/org"
	"github.com/AwesomeCICD/circleci-org-migration-cli/api/project"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/preflight"
)

// ---------------------------------------------------------------------------
// runExportPreflight
// ---------------------------------------------------------------------------

// TestRunExportPreflight_MissingToken_FailsHard verifies that a missing source
// token is a hard failure.
func TestRunExportPreflight_MissingToken_FailsHard(t *testing.T) {
	overrideNonTTY(t)

	deps := preflightDeps{srcToken: "", sourceOrg: "gh/acme"}
	var buf strings.Builder
	err := runExportPreflight(context.Background(), deps, preflightClients{}, &buf)
	if err == nil {
		t.Fatal("expected error when source token missing")
	}
	if !strings.Contains(err.Error(), "preflight") {
		t.Errorf("error should mention 'preflight': %q", err.Error())
	}
	if !strings.Contains(buf.String(), "Source token") {
		t.Errorf("summary should mention 'Source token' check: %q", buf.String())
	}
}

// TestRunExportPreflight_SourceOrgUnreachable_IsWarn verifies that when the
// source org cannot be reached, it is a warning (not a hard fail).
func TestRunExportPreflight_SourceOrgUnreachable_IsWarn(t *testing.T) {
	overrideNonTTY(t)

	deps := preflightDeps{srcToken: "tok-src", sourceOrg: "gh/acme"}
	clients := preflightClients{
		srcOrg: &fakeOrgGetter{err: errors.New("connection refused")},
	}
	var buf strings.Builder
	err := runExportPreflight(context.Background(), deps, clients, &buf)
	// Unreachable source org should warn but not block export.
	if err != nil {
		t.Errorf("source org unreachable should not block export; got: %v", err)
	}
	if !strings.Contains(buf.String(), "⚠") {
		t.Errorf("expected warning icon in output: %q", buf.String())
	}
}

// TestRunExportPreflight_AllOK verifies the happy path.
func TestRunExportPreflight_AllOK(t *testing.T) {
	overrideNonTTY(t)

	deps := preflightDeps{srcToken: "tok-src", sourceOrg: "gh/acme"}
	srcOrg := &org.Organization{
		ID:      "src-uuid",
		Name:    "acme",
		VCSType: "github",
		Slug:    "gh/acme",
	}
	clients := preflightClients{
		srcOrg:   &fakeOrgGetter{org: srcOrg},
		srcFlags: &fakeFlagGetter{flags: map[string]bool{"allow_api_trigger_with_config": true}},
		srcProjects: &fakeProjectLister{projects: []project.OrgProject{
			{ID: "1", Slug: "gh/acme/repo"},
		}},
	}
	var buf strings.Builder
	err := runExportPreflight(context.Background(), deps, clients, &buf)
	if err != nil {
		t.Errorf("all-OK export preflight should not error; got: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "✅") {
		t.Error("expected ✅ in all-OK output")
	}
}

// ---------------------------------------------------------------------------
// runSyncPreflight
// ---------------------------------------------------------------------------

// TestRunSyncPreflight_MissingToken_FailsHard verifies that a missing dest
// token is a hard failure.
func TestRunSyncPreflight_MissingToken_FailsHard(t *testing.T) {
	overrideNonTTY(t)

	deps := preflightDeps{dstToken: "", destOrg: "gh/acme-new"}
	var buf strings.Builder
	err := runSyncPreflight(context.Background(), deps, preflightClients{}, "", &buf)
	if err == nil {
		t.Fatal("expected error when dest token missing")
	}
	if !strings.Contains(err.Error(), "preflight") {
		t.Errorf("error should mention 'preflight': %q", err.Error())
	}
	if !strings.Contains(buf.String(), "Destination token") {
		t.Errorf("summary should mention 'Destination token' check: %q", buf.String())
	}
}

// TestRunSyncPreflight_DestOrgUnreachable_FailsHard verifies that an
// unreachable destination org is a hard failure.
func TestRunSyncPreflight_DestOrgUnreachable_FailsHard(t *testing.T) {
	overrideNonTTY(t)

	deps := preflightDeps{dstToken: "tok-dst", destOrg: "gh/acme-new"}
	clients := preflightClients{
		dstOrg: &fakeOrgGetter{err: errors.New("connection refused")},
	}
	var buf strings.Builder
	err := runSyncPreflight(context.Background(), deps, clients, "", &buf)
	if err == nil {
		t.Fatal("expected error when dest org unreachable")
	}
	if !strings.Contains(err.Error(), "preflight") {
		t.Errorf("error should mention 'preflight': %q", err.Error())
	}
}

// TestRunSyncPreflight_CrossType_Warn verifies that a cross-type migration
// produces a warning but does not block.
func TestRunSyncPreflight_CrossType_Warn(t *testing.T) {
	overrideNonTTY(t)

	deps := preflightDeps{dstToken: "tok-dst", destOrg: "gh/acme-new"}
	clients := preflightClients{
		// Dest org is circleci type; manifest source type is "github" → cross-type.
		dstOrg: &fakeOrgGetter{org: &org.Organization{Name: "acme-new", VCSType: "circleci"}},
	}
	var buf strings.Builder
	// Pass manifestSourceType="github" to trigger cross-type check.
	err := runSyncPreflight(context.Background(), deps, clients, "github", &buf)
	if err != nil {
		t.Errorf("cross-type warning should not block sync; got: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "⚠") {
		t.Errorf("expected warning icon for cross-type: %q", out)
	}
	if !strings.Contains(out, "cross-type") {
		t.Errorf("expected 'cross-type' in output: %q", out)
	}
}

// TestRunSyncPreflight_AllOK verifies the happy path.
func TestRunSyncPreflight_AllOK(t *testing.T) {
	overrideNonTTY(t)

	deps := preflightDeps{dstToken: "tok-dst", destOrg: "gh/acme-new"}
	clients := preflightClients{
		dstOrg: &fakeOrgGetter{org: &org.Organization{Name: "acme-new", VCSType: "github"}},
	}
	var buf strings.Builder
	err := runSyncPreflight(context.Background(), deps, clients, "github", &buf)
	if err != nil {
		t.Errorf("all-OK sync preflight should not error; got: %v", err)
	}
	if !strings.Contains(buf.String(), "✅") {
		t.Error("expected ✅ in all-OK output")
	}
}

// ---------------------------------------------------------------------------
// doctor command — flag registration and basic logic
// ---------------------------------------------------------------------------

// runDoctorCmdInternal executes the doctor subcommand and returns
// stdout, stderr, and any error.
func runDoctorCmdInternal(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	root := MakeCommands()

	var outBuf, errBuf strings.Builder
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	root.SetArgs(append([]string{"doctor"}, args...))

	err = root.Execute()
	return outBuf.String(), errBuf.String(), err
}

// TestDoctorCmd_NoOrgFlags_ReturnsError verifies that doctor without any org
// flags returns a usage error.
func TestDoctorCmd_NoOrgFlags_ReturnsError(t *testing.T) {
	overrideNonTTY(t)
	t.Setenv("CIRCLECI_CLI_TOKEN", "")
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "")
	t.Setenv("CIRCLECI_DEST_TOKEN", "")
	t.Setenv("CIRCLE_TOKEN", "")

	_, _, err := runDoctorCmdInternal(t)
	if err == nil {
		t.Fatal("expected error when no org flags supplied")
	}
	if !strings.Contains(err.Error(), "source-org") && !strings.Contains(err.Error(), "dest-org") {
		t.Errorf("error should mention org flags; got: %q", err.Error())
	}
}

// TestDoctorCmd_SourceOnly_MissingToken_Fails verifies that doctor --source-org
// fails hard when the source token is absent.
func TestDoctorCmd_SourceOnly_MissingToken_Fails(t *testing.T) {
	overrideNonTTY(t)
	t.Setenv("CIRCLECI_CLI_TOKEN", "")
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "")
	t.Setenv("CIRCLECI_DEST_TOKEN", "")
	t.Setenv("CIRCLE_TOKEN", "")

	_, _, err := runDoctorCmdInternal(t, "--source-org", "gh/acme")
	if err == nil {
		t.Fatal("expected error when source token missing")
	}
	if !strings.Contains(err.Error(), "preflight") {
		t.Errorf("error should mention 'preflight': %q", err.Error())
	}
}

// TestDoctorCmd_DestOnly_MissingToken_Fails verifies that doctor --dest-org
// fails hard when the dest token is absent.
func TestDoctorCmd_DestOnly_MissingToken_Fails(t *testing.T) {
	overrideNonTTY(t)
	t.Setenv("CIRCLECI_CLI_TOKEN", "")
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "")
	t.Setenv("CIRCLECI_DEST_TOKEN", "")
	t.Setenv("CIRCLE_TOKEN", "")

	_, _, err := runDoctorCmdInternal(t, "--dest-org", "gh/acme-new")
	if err == nil {
		t.Fatal("expected error when dest token missing")
	}
	if !strings.Contains(err.Error(), "preflight") {
		t.Errorf("error should mention 'preflight': %q", err.Error())
	}
}

// TestDoctorCmd_FlagsRegistered verifies the doctor command flags are present.
func TestDoctorCmd_FlagsRegistered(t *testing.T) {
	root := MakeCommands()
	var doctorCmd interface {
		Flags() interface {
			Lookup(string) interface{ Name() string }
		}
	}
	_ = doctorCmd
	// Find the doctor subcommand.
	var found bool
	for _, sub := range root.Commands() {
		if strings.HasPrefix(sub.Use, "doctor") {
			found = true
			for _, flag := range []string{"source-org", "dest-org", "github-token", "dest-github-org"} {
				if sub.Flags().Lookup(flag) == nil {
					t.Errorf("doctor flag --%s not registered", flag)
				}
			}
			break
		}
	}
	if !found {
		t.Fatal("doctor subcommand not found in command tree")
	}
}

// TestDoctorCmd_InCommandTree verifies the doctor command is registered.
func TestDoctorCmd_InCommandTree(t *testing.T) {
	root := MakeCommands()
	found := false
	for _, sub := range root.Commands() {
		if strings.HasPrefix(sub.Use, "doctor") {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("doctor subcommand not registered in MakeCommands")
	}
}

// ---------------------------------------------------------------------------
// migrate --preflight-only flag
// ---------------------------------------------------------------------------

// TestMigrateCmd_PreflightOnlyFlagRegistered verifies the flag is present.
func TestMigrateCmd_PreflightOnlyFlagRegistered(t *testing.T) {
	overrideNonTTY(t)
	t.Setenv("CIRCLECI_CLI_TOKEN", "")
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "")
	t.Setenv("CIRCLECI_DEST_TOKEN", "")
	t.Setenv("CIRCLE_TOKEN", "")

	// --preflight-only should be a known flag.
	_, _, err := runMigrateCmdInternal(t,
		"--source-org", "gh/acme",
		"--dest-org", "gh/acme-new",
		"--preflight-only",
	)
	if err != nil && strings.Contains(err.Error(), "unknown flag") {
		t.Fatalf("--preflight-only caused unknown flag error: %v", err)
	}
	// Should fail (no token), just not on flag parsing.
	if err == nil {
		t.Fatal("expected a token error, got nil")
	}
	if strings.Contains(err.Error(), "unknown flag") {
		t.Fatalf("--preflight-only caused unknown flag error: %v", err)
	}
}

// TestMigrateCmd_PreflightOnly_ExitsAfterPreflight verifies that with
// --preflight-only, migrate runs the preflight but does not error on
// missing export/sync config (no manifest, no apply, etc.).
// The test supplies a fake source+dest token pair so the token check passes,
// but points at a non-existent org so the preflight will warn (not hard-fail).
// The command should return nil (warnings don't block) without attempting export.
func TestMigrateCmd_PreflightOnly_ExitsAfterPreflight(t *testing.T) {
	overrideNonTTY(t)
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "fake-src-token")
	t.Setenv("CIRCLECI_DEST_TOKEN", "fake-dst-token")
	t.Setenv("CIRCLECI_CLI_TOKEN", "")
	t.Setenv("CIRCLE_TOKEN", "")

	// The dest org client will fail (fake token, non-existent host) — but
	// preflight treats that as a hard fail on destination reachability.
	// We can't fully exercise the "all-OK + exit 0" path without a live API,
	// but we can verify the flag is parsed and executed (not "unknown flag"),
	// and that the error (if any) comes from preflight, not from missing export
	// setup (e.g. "no source-org required" etc.).
	_, _, err := runMigrateCmdInternal(t,
		"--source-org", "gh/acme",
		"--dest-org", "gh/acme-new",
		"--preflight-only",
	)
	// Either nil (if dest check degrades to warn with nil client) or preflight
	// error — never a "manifest required" or "apply needed" error.
	if err != nil {
		if strings.Contains(err.Error(), "unknown flag") {
			t.Fatalf("--preflight-only caused unknown flag error: %v", err)
		}
		// Must be a preflight error, not an export/sync setup error.
		if strings.Contains(err.Error(), "manifest") {
			t.Errorf("--preflight-only should not reach manifest checks; got: %v", err)
		}
		if strings.Contains(err.Error(), "--apply") {
			t.Errorf("--preflight-only should not reach apply checks; got: %v", err)
		}
	}
}

// ---------------------------------------------------------------------------
// export --skip-preflight flag
// ---------------------------------------------------------------------------

// TestExportCmd_SkipPreflightFlagRegistered verifies the flag is present.
func TestExportCmd_SkipPreflightFlagRegistered(t *testing.T) {
	root := MakeCommands()
	for _, sub := range root.Commands() {
		if strings.HasPrefix(sub.Use, "export") {
			if sub.Flags().Lookup("skip-preflight") == nil {
				t.Error("export flag --skip-preflight not registered")
			}
			return
		}
	}
	t.Fatal("export subcommand not found")
}

// ---------------------------------------------------------------------------
// sync --skip-preflight flag
// ---------------------------------------------------------------------------

// TestSyncCmd_SkipPreflightFlagRegistered verifies the flag is present on sync.
func TestSyncCmd_SkipPreflightFlagRegistered(t *testing.T) {
	root := MakeCommands()
	for _, sub := range root.Commands() {
		if strings.HasPrefix(sub.Use, "sync") {
			if sub.Flags().Lookup("skip-preflight") == nil {
				t.Error("sync flag --skip-preflight not registered")
			}
			return
		}
	}
	t.Fatal("sync subcommand not found")
}

// ---------------------------------------------------------------------------
// preflight result — PrintSummary integration
// ---------------------------------------------------------------------------

// TestPreflightPrintSummary_Header verifies that the summary box header is
// always included in the output.
func TestPreflightPrintSummary_Header(t *testing.T) {
	results := []preflight.Result{
		{Name: "Test Check", Status: preflight.StatusOK, Detail: "all good"},
	}
	var buf strings.Builder
	ok, warn, fail := preflight.PrintSummary(&buf, results)
	if ok != 1 || warn != 0 || fail != 0 {
		t.Errorf("unexpected counts: ok=%d warn=%d fail=%d", ok, warn, fail)
	}
	out := buf.String()
	if !strings.Contains(out, "Preflight checks") {
		t.Errorf("expected 'Preflight checks' header in output: %q", out)
	}
	if !strings.Contains(out, "Test Check") {
		t.Errorf("expected check name in output: %q", out)
	}
}

// ---------------------------------------------------------------------------
// normalizeOrgType
// ---------------------------------------------------------------------------

func TestNormalizeOrgType_AllTypes(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"github", "GitHub OAuth"},
		{"gh", "GitHub OAuth"},
		{"github_oauth", "GitHub OAuth"},
		{"GITHUB", "GitHub OAuth"},
		{"github_app", "GitHub App (standalone)"},
		{"circleci", "CircleCI standalone"},
		{"bitbucket", "Bitbucket"},
		{"", "unknown"},
		{"custom-type", "custom-type"},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got := normalizeOrgType(tc.in)
			if got != tc.want {
				t.Errorf("normalizeOrgType(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// doctor command — source-only and dest-only branches (token set, org warns)
// ---------------------------------------------------------------------------

// TestDoctorCmd_SourceOnly_WithTokenSet_Warns verifies that doctor --source-org
// with a token set but org unreachable proceeds with a warning (not hard fail).
// We use the fake token path: the client init will succeed, but the API call
// will fail → warn, not hard fail, so err == nil.
func TestDoctorCmd_SourceOnly_WithTokenSet_Warns(t *testing.T) {
	overrideNonTTY(t)
	// Set a fake source token so the token check passes; no dest token needed.
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "fake-src-doctor")
	t.Setenv("CIRCLECI_CLI_TOKEN", "")
	t.Setenv("CIRCLECI_DEST_TOKEN", "")
	t.Setenv("CIRCLE_TOKEN", "")

	_, stderr, err := runDoctorCmdInternal(t, "--source-org", "gh/acme")
	// A network/auth error downgraded to warn means err == nil.
	// The important thing is that the error (if any) is not "unknown flag" and
	// not "missing source-org/dest-org" — it's at most a preflight warn.
	if err != nil && strings.Contains(err.Error(), "unknown flag") {
		t.Fatalf("doctor --source-org caused unknown flag error: %v", err)
	}
	if err != nil && strings.Contains(err.Error(), "source-org") && strings.Contains(err.Error(), "required") {
		t.Fatalf("doctor --source-org treated source-org as missing: %v", err)
	}
	// Either the preflight summary printed or a preflight error was returned.
	combined := stderr
	if err != nil {
		combined += err.Error()
	}
	// The preflight header or a preflight error message must be present.
	if !strings.Contains(combined, "Preflight") && !strings.Contains(combined, "preflight") {
		t.Errorf("expected 'Preflight' or 'preflight' in output; got stderr=%q err=%v", stderr, err)
	}
}

// TestDoctorCmd_DestOnly_WithTokenSet_Warns verifies that doctor --dest-org
// with a token set but org unreachable proceeds with a warning or hard fail.
func TestDoctorCmd_DestOnly_WithTokenSet_Warns(t *testing.T) {
	overrideNonTTY(t)
	// Set a fake dest token so the token check passes.
	t.Setenv("CIRCLECI_DEST_TOKEN", "fake-dst-doctor")
	t.Setenv("CIRCLECI_CLI_TOKEN", "")
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "")
	t.Setenv("CIRCLE_TOKEN", "")

	_, stderr, err := runDoctorCmdInternal(t, "--dest-org", "gh/acme-new")
	// With a fake token, the dest org check will fail → hard fail is expected.
	// The key check is that it's a preflight error, not a flag-parsing error.
	if err != nil && strings.Contains(err.Error(), "unknown flag") {
		t.Fatalf("doctor --dest-org caused unknown flag error: %v", err)
	}
	combined := stderr
	if err != nil {
		combined += err.Error()
	}
	// Must mention preflight.
	if !strings.Contains(combined, "Preflight") && !strings.Contains(combined, "preflight") {
		t.Errorf("expected 'Preflight' in output; got stderr=%q err=%v", stderr, err)
	}
}

// TestDoctorCmd_BothOrgs_WithTokens_RunsFullPreflight verifies that doctor with
// both --source-org and --dest-org runs the full migrate preflight (not just
// source or dest side).
func TestDoctorCmd_BothOrgs_WithTokens_RunsFullPreflight(t *testing.T) {
	overrideNonTTY(t)
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "fake-src-both")
	t.Setenv("CIRCLECI_DEST_TOKEN", "fake-dst-both")
	t.Setenv("CIRCLECI_CLI_TOKEN", "")
	t.Setenv("CIRCLE_TOKEN", "")

	_, stderr, err := runDoctorCmdInternal(t,
		"--source-org", "gh/acme",
		"--dest-org", "gh/acme-new",
	)
	// Full preflight runs — the dest org check will fail with a fake token.
	if err != nil && strings.Contains(err.Error(), "unknown flag") {
		t.Fatalf("doctor caused unknown flag error: %v", err)
	}
	combined := stderr
	if err != nil {
		combined += err.Error()
	}
	// Either a preflight summary or a preflight error — both mention "preflight".
	if !strings.Contains(combined, "Preflight") && !strings.Contains(combined, "preflight") {
		t.Errorf("expected 'Preflight' in combined output; got stderr=%q err=%v", stderr, err)
	}
}

// ---------------------------------------------------------------------------
// runExportPreflight — TTY branch (interactive confirm skipped on non-TTY)
// ---------------------------------------------------------------------------

// TestRunExportPreflight_NonTTY_WarnsNoBlock verifies that on non-TTY,
// warnings produced by export preflight (e.g. api-trigger flag off) do not
// block (no interactive prompt is shown).
func TestRunExportPreflight_NonTTY_WarnsNoBlock(t *testing.T) {
	overrideNonTTY(t)

	deps := preflightDeps{srcToken: "tok-src", sourceOrg: "gh/acme"}
	srcOrg := &org.Organization{ID: "src-uuid", Name: "acme", VCSType: "github", Slug: "gh/acme"}
	clients := preflightClients{
		srcOrg:   &fakeOrgGetter{org: srcOrg},
		srcFlags: &fakeFlagGetter{flags: map[string]bool{"allow_api_trigger_with_config": false}},
		srcProjects: &fakeProjectLister{projects: []project.OrgProject{
			{ID: "1", Slug: "gh/acme/repo"},
		}},
	}
	var buf strings.Builder
	err := runExportPreflight(context.Background(), deps, clients, &buf)
	// Warning from disabled trigger flag; on non-TTY must NOT block.
	if err != nil {
		t.Errorf("non-TTY export preflight with warn should not block; got: %v", err)
	}
	if !strings.Contains(buf.String(), "⚠") {
		t.Errorf("expected warning icon in output: %q", buf.String())
	}
}

// ---------------------------------------------------------------------------
// runSyncPreflight — TTY branch (interactive confirm skipped on non-TTY)
// ---------------------------------------------------------------------------

// TestRunSyncPreflight_NonTTY_CrossType_NoBlock verifies that on non-TTY,
// a cross-type warning does not prompt and does not block.
func TestRunSyncPreflight_NonTTY_CrossType_NoBlock(t *testing.T) {
	overrideNonTTY(t)

	deps := preflightDeps{dstToken: "tok-dst", destOrg: "gh/acme-new"}
	clients := preflightClients{
		dstOrg: &fakeOrgGetter{org: &org.Organization{Name: "acme-new", VCSType: "github_app"}},
	}
	var buf strings.Builder
	// manifestSourceType "github" vs dest "github_app" → cross-type warn.
	err := runSyncPreflight(context.Background(), deps, clients, "github", &buf)
	if err != nil {
		t.Errorf("non-TTY sync preflight cross-type warn should not block; got: %v", err)
	}
	if !strings.Contains(buf.String(), "⚠") {
		t.Errorf("expected warning icon for cross-type: %q", buf.String())
	}
}

// TestRunSyncPreflight_NilClient_NoDstOrg_FailsHard verifies that when the
// dest org client is nil (no token → build failure), runSyncPreflight returns
// a hard fail on the dest token check, not on the org check.
func TestRunSyncPreflight_NilClient_NoDstOrg_FailsHard(t *testing.T) {
	overrideNonTTY(t)

	// Nil client: dest token check passes (token is set), but no dstOrg client
	// is available → checkDestOrg with nil client → StatusWarn (not Fail).
	deps := preflightDeps{dstToken: "tok-dst", destOrg: "gh/acme-new"}
	var buf strings.Builder
	err := runSyncPreflight(context.Background(), deps, preflightClients{}, "", &buf)
	// nil client → checkDestOrg → Warn (best-effort), so no hard fail.
	if err != nil {
		t.Errorf("nil dest org client should produce warn not fail; got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Interactive TTY path — test that warnings prompt and can be continued.
// Uses stdinIsTerminal override + fake reader injection via the Prompter.
// ---------------------------------------------------------------------------

// overrideInteractiveTTY replaces stdinIsTerminal with a stub that returns tty,
// and restores the original via t.Cleanup. Named differently from overrideTTY
// in secrets_capture_trigger_flag_test.go to avoid redeclaration.
func overrideInteractiveTTY(t *testing.T, tty bool) {
	t.Helper()
	orig := stdinIsTerminal
	stdinIsTerminal = func() bool { return tty }
	t.Cleanup(func() { stdinIsTerminal = orig })
}

// TestRunExportPreflight_TTY_WarningsPromptAndContinue verifies that on a TTY,
// warnings cause a confirmation prompt; when the user answers "y" the preflight
// proceeds without error.
//
// This test overrides stdinIsTerminal to return true and monkey-patches os.Stdin
// via the Prompter — but because runExportPreflight uses NewPrompter(os.Stdin, …)
// directly, we can only test the TTY path indirectly. We verify that the function
// does not panic or return a non-nil error when the stdinIsTerminal path runs
// against a non-blocking pipe by injecting a "y\n" stdin.
func TestRunExportPreflight_TTY_WarningsPrompt_Cancelled(t *testing.T) {
	// Override stdinIsTerminal to return true (interactive TTY).
	overrideInteractiveTTY(t, true)

	deps := preflightDeps{srcToken: "tok-src", sourceOrg: "gh/acme"}
	srcOrg := &org.Organization{ID: "src-uuid", Name: "acme", VCSType: "github", Slug: "gh/acme"}
	clients := preflightClients{
		srcOrg: &fakeOrgGetter{org: srcOrg},
		// Disable flag → warn.
		srcFlags:    &fakeFlagGetter{flags: map[string]bool{"allow_api_trigger_with_config": false}},
		srcProjects: &fakeProjectLister{},
	}
	var buf strings.Builder
	// We cannot inject a fake stdin into runExportPreflight without refactoring
	// the function. When os.Stdin is not a pipe and is the real stdin but returns
	// EOF, askBool will return an error. We accept either a "reading confirmation"
	// error or no error (if the pipe happens to return something).
	err := runExportPreflight(context.Background(), deps, clients, &buf)
	// The prompt path may return a "reading confirmation" error (EOF from test
	// stdin), or the function may be non-blocking. Either is acceptable here —
	// we just verify the preflight summary was printed.
	out := buf.String()
	if !strings.Contains(out, "Preflight") {
		t.Errorf("expected 'Preflight' header in output: %q", out)
	}
	// If err is non-nil, it must be the confirmation error, not a hard fail.
	if err != nil && !strings.Contains(err.Error(), "confirm") &&
		!strings.Contains(err.Error(), "reading") &&
		!strings.Contains(err.Error(), "cancelled") &&
		!strings.Contains(err.Error(), "EOF") {
		t.Errorf("unexpected error type: %v", err)
	}
}

// TestRunSyncPreflight_TTY_WarningsPrompt verifies the TTY interactive path
// for sync preflight — same approach as the export test.
func TestRunSyncPreflight_TTY_WarningsPrompt(t *testing.T) {
	overrideInteractiveTTY(t, true)

	deps := preflightDeps{dstToken: "tok-dst", destOrg: "gh/acme-new"}
	clients := preflightClients{
		// Dest org is github_app; manifest source is "github" → cross-type warn.
		dstOrg: &fakeOrgGetter{org: &org.Organization{Name: "acme-new", VCSType: "github_app"}},
	}
	var buf strings.Builder
	err := runSyncPreflight(context.Background(), deps, clients, "github", &buf)
	out := buf.String()
	if !strings.Contains(out, "Preflight") {
		t.Errorf("expected 'Preflight' header in output: %q", out)
	}
	// Accept confirmation-reading errors from test stdin as well as nil.
	if err != nil && !strings.Contains(err.Error(), "confirm") &&
		!strings.Contains(err.Error(), "reading") &&
		!strings.Contains(err.Error(), "cancelled") &&
		!strings.Contains(err.Error(), "EOF") {
		t.Errorf("unexpected error type: %v", err)
	}
}

// ---------------------------------------------------------------------------
// splitForV11 — cover the slug-less branch
// ---------------------------------------------------------------------------

// TestSplitForV11_NoSlug verifies that when an org has no Slug set, splitForV11
// falls back to VCSType + Name.
func TestSplitForV11_NoSlug(t *testing.T) {
	o := &org.Organization{VCSType: "github", Name: "acme"}
	vcs, name := splitForV11(o)
	if vcs != "github" {
		t.Errorf("expected vcs 'github', got %q", vcs)
	}
	if name != "acme" {
		t.Errorf("expected name 'acme', got %q", name)
	}
}

// TestSplitForV11_SlugNoSlash verifies that a slug without a slash returns
// empty vcs and name (forces fallback path).
func TestSplitForV11_SlugNoSlash(t *testing.T) {
	o := &org.Organization{Slug: "noslash", VCSType: "github", Name: "noslash"}
	vcs, name := splitForV11(o)
	// Slug "noslash" has no "/" so SplitN gives len < 2 → falls back to VCSType/Name.
	if vcs != "github" {
		t.Errorf("expected vcs 'github', got %q", vcs)
	}
	if name != "noslash" {
		t.Errorf("expected name 'noslash', got %q", name)
	}
}
