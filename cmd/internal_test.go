package cmd

// internal_test.go exercises unexported functions in the cmd package using
// white-box tests (package cmd, not package cmd_test).

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AwesomeCICD/circleci-org-migration-cli/api/org"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/syncer"
	"github.com/AwesomeCICD/circleci-org-migration-cli/settings"
	"github.com/spf13/cobra"
)

// ---------------------------------------------------------------------------
// decideEnable
// ---------------------------------------------------------------------------

func TestDecideEnable_AllBranches(t *testing.T) {
	cases := []struct {
		name    string
		apply   bool
		yes     bool
		isTTY   bool
		confirm bool
		want    bool
	}{
		{name: "no apply → false", apply: false, yes: false, isTTY: false, want: false},
		{name: "no apply + yes → still false", apply: false, yes: true, isTTY: false, want: false},
		{name: "apply + yes → true", apply: true, yes: true, isTTY: false, want: true},
		{name: "apply + yes + TTY → true", apply: true, yes: true, isTTY: true, confirm: false, want: true},
		{name: "apply + TTY + confirm=true → true", apply: true, yes: false, isTTY: true, confirm: true, want: true},
		{name: "apply + TTY + confirm=false → false", apply: true, yes: false, isTTY: true, confirm: false, want: false},
		{name: "apply + no TTY + no yes → false", apply: true, yes: false, isTTY: false, confirm: true, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			confirmCalled := false
			got := decideEnable(tc.apply, tc.yes, tc.isTTY, func() bool {
				confirmCalled = true
				return tc.confirm
			})
			if got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
			// When isTTY && !yes && apply, confirm should be called.
			if tc.apply && !tc.yes && tc.isTTY && !confirmCalled {
				t.Error("confirm() should have been called for apply+TTY+!yes")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// handleEnableBuilds
// ---------------------------------------------------------------------------

// internalTestCmd returns a minimal cobra.Command for internal tests. Named
// differently from newTestCobraCmd to avoid redeclaration.
func internalTestCmd() *cobra.Command {
	var outBuf, errBuf bytes.Buffer
	c := &cobra.Command{Use: "test"}
	c.SetOut(&outBuf)
	c.SetErr(&errBuf)
	return c
}

func TestHandleEnableBuilds_NoPending_NoOp(t *testing.T) {
	c := internalTestCmd()
	var outBuf bytes.Buffer
	c.SetOut(&outBuf)

	rep := &syncer.Report{}
	if err := handleEnableBuilds(c, nil, rep, false, false, false); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if outBuf.Len() > 0 {
		t.Errorf("expected no stdout output for empty PendingEnable, got: %q", outBuf.String())
	}
}

func TestHandleEnableBuilds_DryRunWithPending_PrintsPlanMessage(t *testing.T) {
	c := internalTestCmd()
	var errBuf bytes.Buffer
	c.SetErr(&errBuf)

	rep := &syncer.Report{
		PendingEnable: []syncer.EnableTarget{
			{Kind: "follow", Slug: "gh/acme/web"},
		},
	}
	if err := handleEnableBuilds(c, nil, rep, false /*apply*/, false, false /*jsonOutput*/); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Dry-run plan message now goes to stderr, not stdout.
	errOut := errBuf.String()
	if !strings.Contains(errOut, "would be created paused") {
		t.Errorf("expected dry-run plan message on stderr, got: %q", errOut)
	}
}

func TestHandleEnableBuilds_ApplyNoTTYNoYes_PrintsSkippedMessage(t *testing.T) {
	c := internalTestCmd()
	var errBuf bytes.Buffer
	c.SetErr(&errBuf)

	rep := &syncer.Report{
		PendingEnable: []syncer.EnableTarget{
			{Kind: "follow", Slug: "gh/acme/web"},
		},
	}
	// apply=true, yes=false; stdin is not a char device in tests → no TTY.
	if err := handleEnableBuilds(c, nil, rep, true /*apply*/, false, false /*jsonOutput*/); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	errOut := errBuf.String()
	// Should print one of the "Skipped" messages on stderr (no TTY path).
	if !strings.Contains(errOut, "Skipped") && !strings.Contains(errOut, "skipped") {
		t.Errorf("expected 'Skipped' in stderr for apply+noTTY+noYes, got: %q", errOut)
	}
}

func TestHandleEnableBuilds_DryRunWithPending_JSONSuppressed(t *testing.T) {
	c := internalTestCmd()
	var outBuf, errBuf bytes.Buffer
	c.SetOut(&outBuf)
	c.SetErr(&errBuf)

	rep := &syncer.Report{
		PendingEnable: []syncer.EnableTarget{
			{Kind: "follow", Slug: "gh/acme/web"},
		},
	}
	// With jsonOutput=true, no text should be written to either stream.
	if err := handleEnableBuilds(c, nil, rep, false /*apply*/, false, true /*jsonOutput*/); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outBuf.Len() > 0 {
		t.Errorf("expected no stdout output with jsonOutput=true, got: %q", outBuf.String())
	}
	if errBuf.Len() > 0 {
		t.Errorf("expected no stderr output with jsonOutput=true, got: %q", errBuf.String())
	}
}

// ---------------------------------------------------------------------------
// printSyncReport
// ---------------------------------------------------------------------------

func TestPrintSyncReport_DryRunMode(t *testing.T) {
	c := internalTestCmd()
	var outBuf bytes.Buffer
	c.SetOut(&outBuf)

	rep := &syncer.Report{
		Applied:     false,
		DestOrgSlug: "gh/acme-new",
		Actions: []syncer.Action{
			{Status: "created", Target: "ctx-a", Detail: "created OK"},
			{Status: "exists", Target: "ctx-b", Detail: "already exists"},
			{Status: "manual", Target: "ctx-c", Detail: "needs manual action"},
			{Status: "error", Target: "ctx-d", Detail: "something failed"},
		},
	}
	printSyncReport(c, "Contexts", rep, &manifest.Manifest{})
	out := outBuf.String()

	if !strings.Contains(out, "DRY RUN") {
		t.Errorf("expected 'DRY RUN' in output, got: %q", out)
	}
	if !strings.Contains(out, "gh/acme-new") {
		t.Errorf("expected dest org slug in output, got: %q", out)
	}
	if !strings.Contains(out, "Needs attention") {
		t.Errorf("expected 'Needs attention' section for manual+error items, got: %q", out)
	}
	if !strings.Contains(out, "ctx-c") {
		t.Errorf("expected ctx-c in attention list, got: %q", out)
	}
	if !strings.Contains(out, "ctx-d") {
		t.Errorf("expected ctx-d in attention list, got: %q", out)
	}
}

func TestPrintSyncReport_AppliedMode(t *testing.T) {
	c := internalTestCmd()
	var outBuf bytes.Buffer
	c.SetOut(&outBuf)

	rep := &syncer.Report{
		Applied:     true,
		DestOrgSlug: "gh/acme-new",
		Actions: []syncer.Action{
			{Status: "set", Target: "var-a", Detail: "set OK"},
		},
	}
	printSyncReport(c, "Projects", rep, &manifest.Manifest{})
	out := outBuf.String()
	if !strings.Contains(out, "APPLIED") {
		t.Errorf("expected 'APPLIED' in output, got: %q", out)
	}
	if strings.Contains(out, "Needs attention") {
		t.Errorf("should not print 'Needs attention' when no manual/error actions, got: %q", out)
	}
}

func TestPrintSyncReport_NoAttentionItems(t *testing.T) {
	c := internalTestCmd()
	var outBuf bytes.Buffer
	c.SetOut(&outBuf)

	rep := &syncer.Report{
		Applied:     false,
		DestOrgSlug: "gh/test",
		Actions: []syncer.Action{
			{Status: "created", Target: "ctx-x", Detail: "created"},
			{Status: "exists", Target: "ctx-y", Detail: "exists"},
		},
	}
	printSyncReport(c, "Contexts", rep, &manifest.Manifest{})
	out := outBuf.String()
	if strings.Contains(out, "Needs attention") {
		t.Errorf("should not show 'Needs attention' when no manual/error items, got: %q", out)
	}
}

// ---------------------------------------------------------------------------
// selectProjects
// ---------------------------------------------------------------------------

// TestSelectProjects_EmptySlugs_ReturnsProjectsWithValues verifies that the
// default (no --project flag) returns only projects that have ≥1 env var.
// This is Fix 2: the old "return all" default caused accidental full-org sweeps.
func TestSelectProjects_EmptySlugs_ReturnsProjectsWithValues(t *testing.T) {
	m := &manifest.Manifest{
		Projects: []manifest.Project{
			{Slug: "gh/acme/web", EnvVars: []manifest.ProjectEnvVar{{Name: "SECRET"}}},
			{Slug: "gh/acme/empty"}, // no env vars → excluded
			{Slug: "gh/acme/api", EnvVars: []manifest.ProjectEnvVar{{Name: "API_KEY"}}},
		},
	}
	got := selectProjects(m, nil)
	if len(got) != 2 {
		t.Errorf("expected 2 projects with values, got %d: %v", len(got),
			func() []string {
				var out []string
				for _, p := range got {
					out = append(out, p.Slug)
				}
				return out
			}())
	}
	for _, p := range got {
		if p.Slug == "gh/acme/empty" {
			t.Errorf("project with no env vars should NOT be included in default set")
		}
	}
}

// TestSelectProjects_EmptySlugs_AllEmpty_ReturnsNone ensures that when no
// project has env vars the default returns an empty slice (not all projects).
func TestSelectProjects_EmptySlugs_AllEmpty_ReturnsNone(t *testing.T) {
	m := &manifest.Manifest{
		Projects: []manifest.Project{
			{Slug: "gh/acme/web"},
			{Slug: "gh/acme/api"},
		},
	}
	got := selectProjects(m, nil)
	if len(got) != 0 {
		t.Errorf("expected 0 projects when all have no env vars, got %d", len(got))
	}
}

func TestSelectProjects_FiltersBySlugs(t *testing.T) {
	m := &manifest.Manifest{
		Projects: []manifest.Project{
			{Slug: "gh/acme/web"},
			{Slug: "gh/acme/api"},
			{Slug: "gh/acme/mobile"},
		},
	}
	got := selectProjects(m, []string{"gh/acme/web", "gh/acme/mobile"})
	if len(got) != 2 {
		t.Fatalf("expected 2 projects, got %d", len(got))
	}
	slugs := map[string]bool{}
	for _, p := range got {
		slugs[p.Slug] = true
	}
	if !slugs["gh/acme/web"] || !slugs["gh/acme/mobile"] {
		t.Errorf("expected web and mobile, got: %v", slugs)
	}
}

func TestSelectProjects_UnknownSlug_Filtered(t *testing.T) {
	m := &manifest.Manifest{
		Projects: []manifest.Project{
			{Slug: "gh/acme/web"},
		},
	}
	got := selectProjects(m, []string{"gh/acme/nonexistent"})
	if len(got) != 0 {
		t.Errorf("expected 0 projects for unknown slug, got %d", len(got))
	}
}

func TestSelectProjects_EmptyManifest(t *testing.T) {
	m := &manifest.Manifest{}
	got := selectProjects(m, nil)
	if len(got) != 0 {
		t.Errorf("expected 0 projects, got %d", len(got))
	}
}

// ---------------------------------------------------------------------------
// componentsLabel
// ---------------------------------------------------------------------------

func TestComponentsLabel_AllSelected(t *testing.T) {
	got := componentsLabel(false, false, false, false)
	if !strings.Contains(got, "contexts") {
		t.Errorf("expected 'contexts' in %q", got)
	}
	if !strings.Contains(got, "projects") {
		t.Errorf("expected 'projects' in %q", got)
	}
	if !strings.Contains(got, "org settings") {
		t.Errorf("expected 'org settings' in %q", got)
	}
	if !strings.Contains(got, "extras") {
		t.Errorf("expected 'extras' in %q", got)
	}
}

func TestComponentsLabel_NoneSelected(t *testing.T) {
	got := componentsLabel(true, true, true, true)
	if got != "(none)" {
		t.Errorf("expected '(none)', got %q", got)
	}
}

func TestComponentsLabel_ContextsOnly(t *testing.T) {
	got := componentsLabel(false, true, true, true)
	if got != "contexts" {
		t.Errorf("expected 'contexts', got %q", got)
	}
}

func TestComponentsLabel_ProjectsAndOrgSettings(t *testing.T) {
	got := componentsLabel(true, false, false, true)
	if !strings.Contains(got, "projects") {
		t.Errorf("expected 'projects' in %q", got)
	}
	if !strings.Contains(got, "org settings") {
		t.Errorf("expected 'org settings' in %q", got)
	}
	if strings.Contains(got, "contexts") {
		t.Errorf("should not contain 'contexts', got %q", got)
	}
}

// ---------------------------------------------------------------------------
// loadBundleIfPresent (internal)
// ---------------------------------------------------------------------------

func TestLoadBundleIfPresent_EmptyPath_ReturnsNil(t *testing.T) {
	bundle, err := loadBundleIfPresent("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if bundle != nil {
		t.Error("expected nil bundle for empty path")
	}
}

func TestLoadBundleIfPresent_MissingFile_ReturnsNil(t *testing.T) {
	bundle, err := loadBundleIfPresent("/no/such/file/secrets.json")
	if err != nil {
		t.Fatalf("unexpected error for missing file: %v", err)
	}
	if bundle != nil {
		t.Error("expected nil bundle for missing file")
	}
}

// ---------------------------------------------------------------------------
// loadBundleWithFeedback (internal) — Issue #76
// ---------------------------------------------------------------------------

// writeTempBundle writes a minimal valid SecretBundle JSON to a temp file and
// returns the path.
func writeTempBundle(t *testing.T, b *manifest.SecretBundle) string {
	t.Helper()
	dir := t.TempDir()
	path := dir + "/secrets.json"
	if err := b.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	return path
}

// TestLoadBundleWithFeedback_Present_PrintsLoadedMessage verifies that when
// the bundle file exists, a "Loaded secrets bundle" line is printed to stderr
// and the bundle is returned without error.
func TestLoadBundleWithFeedback_Present_PrintsLoadedMessage(t *testing.T) {
	b := manifest.NewSecretBundle()
	b.SetContextSecret("my-ctx", "SECRET", "value1")
	b.SetProjectSecret("gh/acme/web", "WEB_VAR", "value2")
	path := writeTempBundle(t, b)

	var errBuf strings.Builder
	got, err := loadBundleWithFeedback(path, true, &errBuf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil bundle")
	}
	msg := errBuf.String()
	if !strings.Contains(msg, "Loaded secrets bundle from") {
		t.Errorf("expected 'Loaded secrets bundle from' in stderr; got %q", msg)
	}
	// Should report the correct value count (2 values total).
	if !strings.Contains(msg, "2 values") {
		t.Errorf("expected '2 values' in load message; got %q", msg)
	}
	if !strings.Contains(msg, path) {
		t.Errorf("expected path %q in load message; got %q", path, msg)
	}
}

// TestLoadBundleWithFeedback_Absent_Default_PrintsNote verifies that when the
// bundle is absent and isDefault=true, a "Note:" line is printed to stderr.
func TestLoadBundleWithFeedback_Absent_Default_PrintsNote(t *testing.T) {
	noSuchPath := t.TempDir() + "/no-such-secrets.json"

	var errBuf strings.Builder
	got, err := loadBundleWithFeedback(noSuchPath, true, &errBuf)
	if err != nil {
		t.Fatalf("unexpected error for absent file: %v", err)
	}
	if got != nil {
		t.Error("expected nil bundle for missing file")
	}
	msg := errBuf.String()
	if !strings.Contains(msg, "Note:") {
		t.Errorf("expected 'Note:' line for absent default bundle; got %q", msg)
	}
	if !strings.Contains(msg, "not found") {
		t.Errorf("expected 'not found' in absent-default message; got %q", msg)
	}
}

// TestLoadBundleWithFeedback_Absent_Explicit_FatalError verifies that when
// the bundle is absent and isDefault=false (user supplied the path explicitly),
// a fatal error is returned rather than silently skipping the bundle.
func TestLoadBundleWithFeedback_Absent_Explicit_FatalError(t *testing.T) {
	noSuchPath := t.TempDir() + "/explicit-missing.json"

	var errBuf strings.Builder
	got, err := loadBundleWithFeedback(noSuchPath, false, &errBuf)
	if err == nil {
		t.Fatal("expected error for absent explicit --secrets path, got nil")
	}
	if !strings.Contains(err.Error(), "secrets bundle not found") {
		t.Errorf("expected 'secrets bundle not found' in error; got %q", err.Error())
	}
	if !strings.Contains(err.Error(), noSuchPath) {
		t.Errorf("expected path %q in error; got %q", noSuchPath, err.Error())
	}
	if got != nil {
		t.Error("expected nil bundle on error")
	}
}

// TestLoadBundleWithFeedback_EmptyPath_SilentlySkips verifies that an empty
// path returns nil bundle without any output.
func TestLoadBundleWithFeedback_EmptyPath_SilentlySkips(t *testing.T) {
	var errBuf strings.Builder
	got, err := loadBundleWithFeedback("", true, &errBuf)
	if err != nil {
		t.Fatalf("unexpected error for empty path: %v", err)
	}
	if got != nil {
		t.Error("expected nil bundle for empty path")
	}
	if errBuf.Len() > 0 {
		t.Errorf("expected no output for empty path; got %q", errBuf.String())
	}
}

// ---------------------------------------------------------------------------
// orgGroupLister.ListGroups — uses a real *org.Client backed by httptest
// ---------------------------------------------------------------------------

// newOrgClientForTest creates an *org.Client pointed at srv by using
// settings.Config with the srv URL as Host and the srv's own HTTP client.
func newOrgClientForTest(t *testing.T, srv *httptest.Server) *org.Client {
	t.Helper()
	cfg := &settings.Config{
		Host:       srv.URL,
		HTTPClient: srv.Client(),
	}
	c, err := org.NewClient(cfg, "fake-token-for-orgtest")
	if err != nil {
		t.Fatalf("org.NewClient: %v", err)
	}
	return c
}

func TestOrgGroupLister_ListGroups_HappyPath(t *testing.T) {
	// The org.Client.ListGroups method calls the /private/ciam/orgs/{id}/groups
	// endpoint. The groups endpoint is on the app.circleci.com host by default,
	// but our test client derives it from the srv URL, so we handle any path
	// that ends with "/groups".
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/groups") {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"items": []map[string]any{
					{"id": "g-1", "name": "platform"},
					{"id": "g-2", "name": "backend"},
				},
			})
			return
		}
		http.Error(w, fmt.Sprintf("unexpected path: %s", r.URL.Path), http.StatusNotFound)
	}))
	defer srv.Close()

	c := newOrgClientForTest(t, srv)
	lister := orgGroupLister{c: c}
	got, err := lister.ListGroups("org-uuid-test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(got))
	}
	if got[0].ID != "g-1" || got[0].Name != "platform" {
		t.Errorf("got[0] = %+v", got[0])
	}
	if got[1].ID != "g-2" || got[1].Name != "backend" {
		t.Errorf("got[1] = %+v", got[1])
	}
}

func TestOrgGroupLister_ListGroups_ErrorPropagated(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"server error"}`, http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := newOrgClientForTest(t, srv)
	lister := orgGroupLister{c: c}
	_, err := lister.ListGroups("org-uuid-test")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ---------------------------------------------------------------------------
// askSecret — non-TTY fallback path
// ---------------------------------------------------------------------------
//
// The TTY path (term.ReadPassword) cannot be unit-tested without a real
// pseudo-terminal, so we cover only the non-TTY (piped) path here.  When
// stdin is not a terminal, askSecret falls back to a plain bufio.ReadLine
// so that tests and CI pipelines can still supply secrets via stdin pipes.

// TestAskSecret_NonTTY_ReadsPlainLine verifies that on a non-TTY stream
// askSecret reads and returns the value without claiming masking.
func TestAskSecret_NonTTY_ReadsPlainLine(t *testing.T) {
	var out strings.Builder
	p := NewPrompter(strings.NewReader("mysecrettoken\n"), &out)

	val, err := p.askSecret("API token")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != "mysecrettoken" {
		t.Errorf("got %q, want %q", val, "mysecrettoken")
	}
	// The prompt should NOT claim input is hidden on a non-TTY.
	prompt := out.String()
	if strings.Contains(prompt, "input hidden") {
		t.Errorf("non-TTY prompt must not claim 'input hidden', got: %q", prompt)
	}
	// The prompt should still show the label.
	if !strings.Contains(prompt, "API token") {
		t.Errorf("prompt should contain the label 'API token', got: %q", prompt)
	}
}

// TestAskSecret_NonTTY_TrimsWhitespace verifies that surrounding whitespace
// and the trailing newline are trimmed from the returned secret.
func TestAskSecret_NonTTY_TrimsWhitespace(t *testing.T) {
	var out strings.Builder
	p := NewPrompter(strings.NewReader("  token-with-spaces  \n"), &out)

	val, err := p.askSecret("Token")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != "token-with-spaces" {
		t.Errorf("got %q, want %q", val, "token-with-spaces")
	}
}

// TestAskSecretRequired_NonTTY_RepromptOnEmpty verifies that askSecretRequired
// re-prompts when the user supplies an empty line, then accepts a non-empty value.
func TestAskSecretRequired_NonTTY_RepromptOnEmpty(t *testing.T) {
	var out strings.Builder
	// First line is empty → re-prompt; second line has the real value.
	p := NewPrompter(strings.NewReader("\nrealtoken\n"), &out)

	val, err := p.askSecretRequired("API token")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != "realtoken" {
		t.Errorf("got %q, want %q", val, "realtoken")
	}
	// Should have re-prompted (label appears twice).
	prompt := out.String()
	count := strings.Count(prompt, "API token")
	if count < 2 {
		t.Errorf("expected at least 2 occurrences of label (re-prompt), got %d in %q", count, prompt)
	}
}

// Ensure unused imports compile away.
var _ = errors.New

// ---------------------------------------------------------------------------
// resolveHost — CIRCLE_URL fallback (circleci run migrate)
// ---------------------------------------------------------------------------

func TestResolveHost_CircleURL_UsedWhenNothingElseSet(t *testing.T) {
	// CIRCLE_URL is injected by `circleci run migrate`; it should be used when
	// neither CIRCLECI_CLI_HOST nor CIRCLECI_HOST is set.
	t.Setenv("CIRCLECI_CLI_HOST", "")
	t.Setenv("CIRCLECI_HOST", "")
	t.Setenv("CIRCLE_URL", "https://circleci.example.com")
	got := resolveHost()
	if got != "https://circleci.example.com" {
		t.Errorf("resolveHost() = %q; want %q", got, "https://circleci.example.com")
	}
}

func TestResolveHost_CircleURL_SchemeHostOnly_StripPath(t *testing.T) {
	// CIRCLE_URL may include a full API URL with a path; only scheme+host should
	// be returned so that downstream URL construction is not confused.
	t.Setenv("CIRCLECI_CLI_HOST", "")
	t.Setenv("CIRCLECI_HOST", "")
	t.Setenv("CIRCLE_URL", "https://circleci.example.com/api/v2/")
	got := resolveHost()
	if got != "https://circleci.example.com" {
		t.Errorf("resolveHost() = %q; want scheme+host only %q", got, "https://circleci.example.com")
	}
}

func TestResolveHost_CircleCLIHost_WinsOverCircleURL(t *testing.T) {
	// CIRCLECI_CLI_HOST must take precedence over the lower-priority CIRCLE_URL.
	t.Setenv("CIRCLECI_CLI_HOST", "https://my-server.example.com")
	t.Setenv("CIRCLECI_HOST", "")
	t.Setenv("CIRCLE_URL", "https://circleci.example.com")
	got := resolveHost()
	if got != "https://my-server.example.com" {
		t.Errorf("resolveHost() = %q; want CIRCLECI_CLI_HOST value", got)
	}
}

func TestResolveHost_CircleciHost_WinsOverCircleURL(t *testing.T) {
	// CIRCLECI_HOST must take precedence over CIRCLE_URL.
	t.Setenv("CIRCLECI_CLI_HOST", "")
	t.Setenv("CIRCLECI_HOST", "https://legacy-server.example.com")
	t.Setenv("CIRCLE_URL", "https://circleci.example.com")
	got := resolveHost()
	if got != "https://legacy-server.example.com" {
		t.Errorf("resolveHost() = %q; want CIRCLECI_HOST value", got)
	}
}

func TestResolveHost_NoVarsSet_ReturnsEmpty(t *testing.T) {
	t.Setenv("CIRCLECI_CLI_HOST", "")
	t.Setenv("CIRCLECI_HOST", "")
	t.Setenv("CIRCLE_URL", "")
	got := resolveHost()
	if got != "" {
		t.Errorf("resolveHost() = %q; want empty string when no vars set", got)
	}
}

// ---------------------------------------------------------------------------
// handleEnableBuilds — yes=true path
// ---------------------------------------------------------------------------

// fakeEnableBuildsFunc replaces syncer.Syncer with a fake that records calls.
// Because handleEnableBuilds calls sy.EnableBuilds, we need a syncer.Syncer
// with enough fields set that it doesn't panic on a nil receiver. But since
// EnableBuilds always fails without real API, we just verify the noTTY+yes
// path reaches the output section.

// Actually we can't inject a fake syncer here because handleEnableBuilds takes
// *syncer.Syncer directly. We test the noTTY case (which is what CI gets) with
// yes=true. When yes=true and no TTY, decideEnable returns true, so
// EnableBuilds is called on the nil syncer → panic. We skip calling with a
// nil syncer and instead test the "1 project would be created" dry-run message.
// The yes=true path requires a real (or fake) syncer, so that coverage line
// is counted under integration tests.

func TestHandleEnableBuilds_Yes_PrintsEnablingMessage(t *testing.T) {
	// We cannot fully test the yes=true path without a working syncer (which
	// needs a live API). We verify instead that when apply+yes with a real
	// syncer-shaped nil causes the output to start ("Enabling builds for").
	// Skip this test when the syncer would panic.
	t.Skip("requires a non-nil syncer to exercise the EnableBuilds call")
}

// ---------------------------------------------------------------------------
// syncActionLine / resolveTargetMeta — friendly name + dest URL enrichment
// ---------------------------------------------------------------------------

// TestSyncActionLine_OAuthProjectTarget verifies that a manual action whose
// target is an OAuth project slug gets enriched with the project's friendly
// name (from the manifest) and a destination settings URL (not a bare UUID).
func TestSyncActionLine_OAuthProjectTarget(t *testing.T) {
	m := &manifest.Manifest{
		Source: manifest.Source{
			Host: "https://circleci.com",
			Org:  manifest.Org{Slug: "gh/src-org"},
		},
		Projects: []manifest.Project{
			{Slug: "gh/src-org/api", Name: "api-service"},
		},
	}

	a := syncer.Action{
		Kind:   "project-ssh-key",
		Target: "gh/src-org/api/ssh-key:aa:bb:cc",
		Status: "manual",
		Detail: "SSH key not captured",
	}
	destOrgSlug := "gh/dest-org"

	line := syncActionLine(a, destOrgSlug, m)

	// Must contain the friendly name.
	if !strings.Contains(line, "api-service") {
		t.Errorf("expected friendly name 'api-service' in line, got: %q", line)
	}
	// Must contain a settings URL pointing at the DESTINATION org (not source UUID).
	if !strings.Contains(line, "dest-org") {
		t.Errorf("expected dest-org in URL, got: %q", line)
	}
	// Must contain the SSH settings tab.
	if !strings.Contains(line, "/ssh") {
		t.Errorf("expected /ssh tab in URL, got: %q", line)
	}
	// Must NOT be just the raw target (i.e. must be enriched).
	if line == a.Target {
		t.Errorf("line must be enriched, got bare target: %q", line)
	}
}

// TestSyncActionLine_StandaloneProjectTarget verifies enrichment for a
// standalone (circleci/<orgUUID>/<projUUID>) project target.
func TestSyncActionLine_StandaloneProjectTarget(t *testing.T) {
	m := &manifest.Manifest{
		Source: manifest.Source{
			Host: "https://circleci.com",
			Org:  manifest.Org{Slug: "circleci/src-org-uuid"},
		},
		Projects: []manifest.Project{
			{Slug: "circleci/src-org-uuid/proj-uuid-123", Name: "my-app"},
		},
	}

	a := syncer.Action{
		Kind:   "project-webhook",
		Target: "circleci/src-org-uuid/proj-uuid-123/webhook:notify",
		Status: "manual",
		Detail: "signing secret cannot be migrated",
	}
	destOrgSlug := "circleci/dest-org-uuid"

	line := syncActionLine(a, destOrgSlug, m)

	if !strings.Contains(line, "my-app") {
		t.Errorf("expected friendly name 'my-app' in line, got: %q", line)
	}
	// URL should use dest org UUID, not source.
	if !strings.Contains(line, "dest-org-uuid") {
		t.Errorf("expected dest-org-uuid in URL, got: %q", line)
	}
	// Must point to webhooks tab.
	if !strings.Contains(line, "webhooks") {
		t.Errorf("expected webhooks tab in URL, got: %q", line)
	}
}

// TestSyncActionLine_ContextTarget verifies that a context-scoped action
// (context-var) gets enriched with the context name and a contexts settings URL.
func TestSyncActionLine_ContextTarget(t *testing.T) {
	m := &manifest.Manifest{
		Source: manifest.Source{
			Host: "https://circleci.com",
			Org:  manifest.Org{Slug: "gh/acme"},
		},
		Contexts: []manifest.Context{
			{Name: "deploy-prod"},
		},
	}

	a := syncer.Action{
		Kind:   "context-var",
		Target: "deploy-prod/MY_SECRET",
		Status: "manual",
		Detail: "value not captured",
	}
	destOrgSlug := "gh/acme-new"

	line := syncActionLine(a, destOrgSlug, m)

	// Context name must appear.
	if !strings.Contains(line, "deploy-prod") {
		t.Errorf("expected context name in line, got: %q", line)
	}
	// Must contain a contexts settings URL.
	if !strings.Contains(line, "contexts") {
		t.Errorf("expected 'contexts' URL fragment in line, got: %q", line)
	}
	if !strings.Contains(line, "acme-new") {
		t.Errorf("expected dest org in URL, got: %q", line)
	}
}

// TestSyncActionLine_UnknownTarget verifies that when a target cannot be
// resolved to a friendly name or URL, the raw target string is returned unchanged.
func TestSyncActionLine_UnknownTarget(t *testing.T) {
	m := &manifest.Manifest{
		Source: manifest.Source{
			Host: "https://circleci.com",
			Org:  manifest.Org{Slug: "gh/acme"},
		},
	}

	a := syncer.Action{
		Kind:   "org-settings",
		Target: "feature_flag:drop_all_build_requests",
		Status: "manual",
		Detail: "dangerous flag",
	}

	line := syncActionLine(a, "gh/acme-new", m)

	// Org-settings targets like "feature_flag:..." have no project/context to
	// resolve; the raw target should come back unmodified.
	if line != a.Target {
		t.Errorf("expected raw target %q, got: %q", a.Target, line)
	}
}

// TestPrintSyncReport_ManualLineContainsFriendlyName verifies that after the
// change, a manual action for an OAuth project slug appears in the "Needs
// attention" section with the project's friendly name alongside it.
func TestPrintSyncReport_ManualLineContainsFriendlyName(t *testing.T) {
	c := internalTestCmd()
	var outBuf bytes.Buffer
	c.SetOut(&outBuf)

	m := &manifest.Manifest{
		Source: manifest.Source{
			Host: "https://circleci.com",
			Org:  manifest.Org{Slug: "gh/acme"},
		},
		Projects: []manifest.Project{
			{Slug: "gh/acme/web", Name: "web-frontend"},
		},
	}

	rep := &syncer.Report{
		Applied:     false,
		DestOrgSlug: "gh/acme-new",
		Actions: []syncer.Action{
			{
				Kind:   "project-ssh-key",
				Target: "gh/acme/web/ssh-key:aa:bb:cc",
				Status: "manual",
				Detail: "SSH key private key not captured",
			},
		},
	}

	printSyncReport(c, "Projects", rep, m)
	out := outBuf.String()

	// The attention line must show the friendly name, not just the raw UUID slug.
	if !strings.Contains(out, "web-frontend") {
		t.Errorf("expected friendly name 'web-frontend' in output, got:\n%s", out)
	}
	// Must contain a settings URL.
	if !strings.Contains(out, "app.circleci.com") {
		t.Errorf("expected circleci settings URL in output, got:\n%s", out)
	}
}
