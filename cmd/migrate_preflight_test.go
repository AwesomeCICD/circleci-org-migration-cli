package cmd

// migrate_preflight_test.go — white-box unit tests for the individual
// preflight checks.  Lives in package cmd (not cmd_test) so it can access
// unexported helpers and override stdinIsTerminal.
//
// All checks are exercised with both real and nil clients; the latter tests
// the best-effort / graceful-degradation paths.

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
// Fake implementations
// ---------------------------------------------------------------------------

type fakeOrgGetter struct {
	org *org.Organization
	err error
}

func (f *fakeOrgGetter) GetOrganization(_ context.Context, _ string) (*org.Organization, error) {
	return f.org, f.err
}

type fakeFlagGetter struct {
	flags map[string]bool
	err   error
}

func (f *fakeFlagGetter) GetFeatureFlags(_ context.Context, _, _ string) (map[string]bool, error) {
	return f.flags, f.err
}

type fakeProjectLister struct {
	projects []project.OrgProject
	err      error
}

func (f *fakeProjectLister) ListOrgProjects(_ context.Context, _ string) ([]project.OrgProject, error) {
	return f.projects, f.err
}

// ---------------------------------------------------------------------------
// checkTokens
// ---------------------------------------------------------------------------

func TestCheckTokens_BothSet(t *testing.T) {
	deps := preflightDeps{srcToken: "tok-src", dstToken: "tok-dst"}
	r := checkTokens(deps)
	if r.Status != preflight.StatusOK {
		t.Errorf("want OK, got %s: %s", r.Status, r.Detail)
	}
}

func TestCheckTokens_MissingSrc(t *testing.T) {
	deps := preflightDeps{srcToken: "", dstToken: "tok-dst"}
	r := checkTokens(deps)
	if r.Status != preflight.StatusFail {
		t.Errorf("want Fail, got %s", r.Status)
	}
	if !strings.Contains(r.Detail, "source") {
		t.Errorf("detail should mention 'source': %q", r.Detail)
	}
}

func TestCheckTokens_MissingDst(t *testing.T) {
	deps := preflightDeps{srcToken: "tok-src", dstToken: ""}
	r := checkTokens(deps)
	if r.Status != preflight.StatusFail {
		t.Errorf("want Fail, got %s", r.Status)
	}
	if !strings.Contains(r.Detail, "destination") {
		t.Errorf("detail should mention 'destination': %q", r.Detail)
	}
}

func TestCheckTokens_BothMissing(t *testing.T) {
	deps := preflightDeps{}
	r := checkTokens(deps)
	if r.Status != preflight.StatusFail {
		t.Errorf("want Fail, got %s", r.Status)
	}
}

// ---------------------------------------------------------------------------
// checkDestOrg
// ---------------------------------------------------------------------------

func TestCheckDestOrg_Reachable(t *testing.T) {
	client := &fakeOrgGetter{org: &org.Organization{Name: "acme-new", VCSType: "github"}}
	r, o := checkDestOrg(context.Background(), client, "gh/acme-new")
	if r.Status != preflight.StatusOK {
		t.Errorf("want OK, got %s: %s", r.Status, r.Detail)
	}
	if o == nil || o.Name != "acme-new" {
		t.Errorf("expected org to be returned")
	}
}

func TestCheckDestOrg_Unreachable(t *testing.T) {
	client := &fakeOrgGetter{err: errors.New("connection refused")}
	r, o := checkDestOrg(context.Background(), client, "gh/acme-new")
	if r.Status != preflight.StatusFail {
		t.Errorf("want Fail, got %s", r.Status)
	}
	if o != nil {
		t.Error("expected nil org on failure")
	}
}

func TestCheckDestOrg_NilClient(t *testing.T) {
	r, _ := checkDestOrg(context.Background(), nil, "gh/acme-new")
	// Nil client should warn, not fail hard.
	if r.Status == preflight.StatusFail {
		t.Error("nil client should produce Warn, not Fail")
	}
}

// ---------------------------------------------------------------------------
// checkSrcOrg
// ---------------------------------------------------------------------------

func TestCheckSrcOrg_Reachable(t *testing.T) {
	client := &fakeOrgGetter{org: &org.Organization{Name: "acme", VCSType: "github", Slug: "gh/acme"}}
	r, o := checkSrcOrg(context.Background(), client, "gh/acme")
	if r.Status != preflight.StatusOK {
		t.Errorf("want OK, got %s: %s", r.Status, r.Detail)
	}
	if o == nil {
		t.Error("expected org to be returned")
	}
}

func TestCheckSrcOrg_Unreachable_IsWarnNotFail(t *testing.T) {
	client := &fakeOrgGetter{err: errors.New("connection refused")}
	r, o := checkSrcOrg(context.Background(), client, "gh/acme")
	// Source org unreachable should be a WARN, not a hard FAIL.
	if r.Status != preflight.StatusWarn {
		t.Errorf("want Warn (soft), got %s: %s", r.Status, r.Detail)
	}
	if o != nil {
		t.Error("expected nil org on failure")
	}
}

// ---------------------------------------------------------------------------
// checkCrossType
// ---------------------------------------------------------------------------

func TestCheckCrossType_SameType(t *testing.T) {
	src := &org.Organization{VCSType: "github"}
	dst := &org.Organization{VCSType: "github"}
	r := checkCrossType(src, dst)
	if r.Status != preflight.StatusOK {
		t.Errorf("want OK for same type, got %s: %s", r.Status, r.Detail)
	}
}

func TestCheckCrossType_DifferentTypes(t *testing.T) {
	src := &org.Organization{VCSType: "github"}
	dst := &org.Organization{VCSType: "circleci"}
	r := checkCrossType(src, dst)
	if r.Status != preflight.StatusWarn {
		t.Errorf("want Warn for cross-type, got %s", r.Status)
	}
	if !strings.Contains(r.Detail, "cross-type") {
		t.Errorf("detail should mention 'cross-type': %q", r.Detail)
	}
	if !strings.Contains(r.Detail, "playbooks") {
		t.Errorf("detail should mention 'playbooks' doc link: %q", r.Detail)
	}
}

// ---------------------------------------------------------------------------
// checkAPITriggerFlag
// ---------------------------------------------------------------------------

func TestCheckAPITriggerFlag_Enabled(t *testing.T) {
	client := &fakeFlagGetter{flags: map[string]bool{"allow_api_trigger_with_config": true}}
	srcOrg := &org.Organization{VCSType: "gh", Name: "acme", Slug: "gh/acme"}
	r := checkAPITriggerFlag(context.Background(), client, srcOrg)
	if r.Status != preflight.StatusOK {
		t.Errorf("want OK when flag enabled, got %s: %s", r.Status, r.Detail)
	}
}

func TestCheckAPITriggerFlag_Disabled(t *testing.T) {
	client := &fakeFlagGetter{flags: map[string]bool{"allow_api_trigger_with_config": false}}
	srcOrg := &org.Organization{VCSType: "gh", Name: "acme", Slug: "gh/acme"}
	r := checkAPITriggerFlag(context.Background(), client, srcOrg)
	if r.Status != preflight.StatusWarn {
		t.Errorf("want Warn when flag disabled, got %s: %s", r.Status, r.Detail)
	}
	if !strings.Contains(r.Detail, "capture") {
		t.Errorf("detail should mention 'capture': %q", r.Detail)
	}
}

func TestCheckAPITriggerFlag_FlagReadError_IsWarn(t *testing.T) {
	client := &fakeFlagGetter{err: errors.New("API error")}
	srcOrg := &org.Organization{VCSType: "gh", Name: "acme", Slug: "gh/acme"}
	r := checkAPITriggerFlag(context.Background(), client, srcOrg)
	if r.Status != preflight.StatusWarn {
		t.Errorf("want Warn on read error, got %s: %s", r.Status, r.Detail)
	}
}

func TestCheckAPITriggerFlag_NilClient(t *testing.T) {
	srcOrg := &org.Organization{VCSType: "gh", Name: "acme", Slug: "gh/acme"}
	r := checkAPITriggerFlag(context.Background(), nil, srcOrg)
	// Nil client → skip (OK) rather than a noisy error.
	if r.Status == preflight.StatusFail {
		t.Errorf("nil client should not produce Fail: %s", r.Detail)
	}
}

// ---------------------------------------------------------------------------
// checkProjectDiscovery
// ---------------------------------------------------------------------------

func TestCheckProjectDiscovery_Found(t *testing.T) {
	client := &fakeProjectLister{projects: []project.OrgProject{
		{ID: "1", Slug: "gh/acme/repo-a"},
		{ID: "2", Slug: "gh/acme/repo-b"},
	}}
	srcOrg := &org.Organization{ID: "org-uuid-123"}
	r := checkProjectDiscovery(context.Background(), client, srcOrg)
	if r.Status != preflight.StatusOK {
		t.Errorf("want OK, got %s: %s", r.Status, r.Detail)
	}
	if !strings.Contains(r.Detail, "2") {
		t.Errorf("detail should mention count '2': %q", r.Detail)
	}
}

func TestCheckProjectDiscovery_Empty(t *testing.T) {
	client := &fakeProjectLister{projects: nil}
	srcOrg := &org.Organization{ID: "org-uuid-123"}
	r := checkProjectDiscovery(context.Background(), client, srcOrg)
	if r.Status != preflight.StatusWarn {
		t.Errorf("want Warn for 0 projects, got %s: %s", r.Status, r.Detail)
	}
}

func TestCheckProjectDiscovery_APIError_IsWarn(t *testing.T) {
	client := &fakeProjectLister{err: errors.New("500 error")}
	srcOrg := &org.Organization{ID: "org-uuid-123"}
	r := checkProjectDiscovery(context.Background(), client, srcOrg)
	if r.Status != preflight.StatusWarn {
		t.Errorf("want Warn on API error, got %s: %s", r.Status, r.Detail)
	}
	if !strings.Contains(r.Detail, "followed") {
		t.Errorf("detail should mention followed-projects fallback: %q", r.Detail)
	}
}

func TestCheckProjectDiscovery_NoOrgID(t *testing.T) {
	client := &fakeProjectLister{}
	srcOrg := &org.Organization{ID: ""} // no UUID
	r := checkProjectDiscovery(context.Background(), client, srcOrg)
	if r.Status != preflight.StatusWarn {
		t.Errorf("want Warn when no org ID, got %s: %s", r.Status, r.Detail)
	}
}

// ---------------------------------------------------------------------------
// checkGitHubToken
// ---------------------------------------------------------------------------

func TestCheckGitHubToken_Set(t *testing.T) {
	r := checkGitHubToken("ghp_fake")
	if r.Status != preflight.StatusOK {
		t.Errorf("want OK when token set, got %s", r.Status)
	}
}

func TestCheckGitHubToken_Missing(t *testing.T) {
	r := checkGitHubToken("")
	if r.Status != preflight.StatusWarn {
		t.Errorf("want Warn when token absent, got %s", r.Status)
	}
	if !strings.Contains(r.Detail, "--github-token") {
		t.Errorf("detail should mention --github-token: %q", r.Detail)
	}
}

// ---------------------------------------------------------------------------
// runMigratePreflight — integration path (non-TTY, token fail)
// ---------------------------------------------------------------------------

// TestRunMigratePreflight_MissingTokensFailsHard verifies the hard-fail path
// when both tokens are absent.
func TestRunMigratePreflight_MissingTokensFailsHard(t *testing.T) {
	overrideNonTTY(t)

	deps := preflightDeps{
		srcToken:  "",
		dstToken:  "",
		sourceOrg: "gh/acme",
		destOrg:   "gh/acme-new",
	}
	var buf strings.Builder
	err := runMigratePreflight(context.Background(), deps, preflightClients{}, &buf)
	if err == nil {
		t.Fatal("expected error when tokens missing")
	}
	if !strings.Contains(err.Error(), "preflight") {
		t.Errorf("error should mention 'preflight': %q", err.Error())
	}
	// Summary should still be printed.
	if !strings.Contains(buf.String(), "Tokens") {
		t.Errorf("summary should mention 'Tokens' check: %q", buf.String())
	}
}

// TestRunMigratePreflight_DestOrgFailsHard verifies that an unreachable
// destination org is a hard blocker.
func TestRunMigratePreflight_DestOrgFailsHard(t *testing.T) {
	overrideNonTTY(t)

	deps := preflightDeps{
		srcToken:  "tok",
		dstToken:  "tok",
		sourceOrg: "gh/acme",
		destOrg:   "gh/acme-new",
	}
	clients := preflightClients{
		dstOrg: &fakeOrgGetter{err: errors.New("connection refused")},
	}
	var buf strings.Builder
	err := runMigratePreflight(context.Background(), deps, clients, &buf)
	if err == nil {
		t.Fatal("expected error when dest org unreachable")
	}
	if !strings.Contains(err.Error(), "preflight") {
		t.Errorf("error should mention 'preflight': %q", err.Error())
	}
}

// TestRunMigratePreflight_NonTTY_WarningsDoNotBlock verifies that on a
// non-TTY, warnings do NOT block the migration (no interactive confirm).
func TestRunMigratePreflight_NonTTY_WarningsDoNotBlock(t *testing.T) {
	overrideNonTTY(t)

	deps := preflightDeps{
		srcToken:  "tok-src",
		dstToken:  "tok-dst",
		sourceOrg: "gh/acme",
		destOrg:   "gh/acme-new",
	}
	// Dest org reachable (required); src org fails → WARN (not blocking).
	clients := preflightClients{
		dstOrg: &fakeOrgGetter{org: &org.Organization{Name: "acme-new", VCSType: "github"}},
		srcOrg: &fakeOrgGetter{err: errors.New("src unavailable")},
	}
	var buf strings.Builder
	err := runMigratePreflight(context.Background(), deps, clients, &buf)
	// Warning should not block on non-TTY — err must be nil.
	if err != nil {
		t.Errorf("non-TTY warnings should not block; got error: %v", err)
	}
	if !strings.Contains(buf.String(), "Preflight:") {
		t.Errorf("expected summary line in output: %q", buf.String())
	}
}

// TestRunMigratePreflight_AllOK verifies the happy path.
func TestRunMigratePreflight_AllOK(t *testing.T) {
	overrideNonTTY(t)

	deps := preflightDeps{
		srcToken:  "tok-src",
		dstToken:  "tok-dst",
		sourceOrg: "gh/acme",
		destOrg:   "gh/acme-new",
	}
	srcOrg := &org.Organization{ID: "src-uuid", Name: "acme", VCSType: "github", Slug: "gh/acme"}
	dstOrg := &org.Organization{ID: "dst-uuid", Name: "acme-new", VCSType: "github", Slug: "gh/acme-new"}
	clients := preflightClients{
		srcOrg:   &fakeOrgGetter{org: srcOrg},
		dstOrg:   &fakeOrgGetter{org: dstOrg},
		srcFlags: &fakeFlagGetter{flags: map[string]bool{"allow_api_trigger_with_config": true}},
		srcProjects: &fakeProjectLister{projects: []project.OrgProject{
			{ID: "1", Slug: "gh/acme/repo"},
		}},
	}
	var buf strings.Builder
	err := runMigratePreflight(context.Background(), deps, clients, &buf)
	if err != nil {
		t.Errorf("all-OK preflight should not error; got: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "✅") {
		t.Error("expected ✅ in all-OK output")
	}
}

// ---------------------------------------------------------------------------
// --skip-preflight flag is registered on migrate
// ---------------------------------------------------------------------------

// TestMigrateCmd_SkipPreflightFlagRegistered verifies the flag is present via
// the cmd output (accepted without "unknown flag" error).
func TestMigrateCmd_SkipPreflightFlagRegistered(t *testing.T) {
	overrideNonTTY(t)
	t.Setenv("CIRCLECI_CLI_TOKEN", "")
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "")
	t.Setenv("CIRCLECI_DEST_TOKEN", "")
	t.Setenv("CIRCLE_TOKEN", "")

	// --skip-preflight should be a known flag — any error must not mention "unknown flag".
	_, _, err := runMigrateCmdInternal(t,
		"--source-org", "gh/acme",
		"--dest-org", "gh/acme-new",
		"--skip-preflight",
	)
	if err != nil && strings.Contains(err.Error(), "unknown flag") {
		t.Fatalf("--skip-preflight caused unknown flag error: %v", err)
	}
	// Should still error (no token), just not on flag parsing.
	if err == nil {
		t.Fatal("expected a token error, got nil")
	}
	if strings.Contains(err.Error(), "unknown flag") {
		t.Fatalf("--skip-preflight caused unknown flag error: %v", err)
	}
}
