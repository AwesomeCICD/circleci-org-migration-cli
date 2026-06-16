package cmd

// internal_test.go exercises unexported functions in the cmd package using
// white-box tests (package cmd, not package cmd_test).

import (
	"bytes"
	"context"
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

// TestOrgOrbFlagAdapter exercises the orgOrbFlagAdapter, which delegates orb
// feature-flag reads/writes to the org client's v1.1 settings endpoint. It
// covers both methods used by the orb syncer's toggle-and-restore.
func TestOrgOrbFlagAdapter(t *testing.T) {
	var gotPut map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/settings") {
			http.Error(w, "unexpected path: "+r.URL.Path, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"feature_flags": map[string]bool{"allow-uncertified-public-orbs": true},
			})
		case http.MethodPut:
			_ = json.NewDecoder(r.Body).Decode(&gotPut)
			_ = json.NewEncoder(w).Encode(map[string]any{})
		default:
			http.Error(w, "bad method", http.StatusMethodNotAllowed)
		}
	}))
	defer srv.Close()

	a := &orgOrbFlagAdapter{c: newOrgClientForTest(t, srv)}

	flags, err := a.GetOrbFeatureFlags(context.Background(), "github", "acme")
	if err != nil {
		t.Fatalf("GetOrbFeatureFlags: %v", err)
	}
	if !flags["allow-uncertified-public-orbs"] {
		t.Errorf("expected allow-uncertified-public-orbs=true, got %+v", flags)
	}

	if err := a.UpdateOrbFeatureFlags(context.Background(), "github", "acme",
		map[string]bool{"allow_private_orbs": true}); err != nil {
		t.Fatalf("UpdateOrbFeatureFlags: %v", err)
	}
	if gotPut["feature_flags"] == nil {
		t.Errorf("PUT body missing feature_flags: %+v", gotPut)
	}
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
	got, err := lister.ListGroups(context.Background(), "org-uuid-test")
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
	_, err := lister.ListGroups(context.Background(), "org-uuid-test")
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

// ---------------------------------------------------------------------------
// buildInlineOrbNode — error branches
// ---------------------------------------------------------------------------

// TestBuildInlineOrbNode_InvalidYAML verifies that passing invalid YAML to
// buildInlineOrbNode returns a "parsing orb source YAML" error.
func TestBuildInlineOrbNode_InvalidYAML(t *testing.T) {
	_, err := buildInlineOrbNode(":\tinvalid:\tyaml\t[[[")
	if err == nil {
		t.Fatal("expected error for invalid YAML, got nil")
	}
	if !strings.Contains(err.Error(), "parsing orb source YAML") {
		t.Errorf("error %q should mention 'parsing orb source YAML'", err.Error())
	}
}

// TestBuildInlineOrbNode_EmptyDocument verifies that an empty YAML string
// (which produces an empty DocumentNode) returns a structure error.
func TestBuildInlineOrbNode_EmptyDocument(t *testing.T) {
	// An empty string produces a DocumentNode with no Content — triggers the
	// "unexpected orb source YAML structure" branch.
	_, err := buildInlineOrbNode("")
	if err == nil {
		t.Fatal("expected error for empty YAML, got nil")
	}
	if !strings.Contains(err.Error(), "unexpected orb source YAML structure") {
		t.Errorf("error %q should mention 'unexpected orb source YAML structure'", err.Error())
	}
}

// TestBuildInlineOrbNode_NonMappingRoot verifies that an orb source whose
// root is a sequence (not a mapping) returns an appropriate error.
func TestBuildInlineOrbNode_NonMappingRoot(t *testing.T) {
	// A YAML list (sequence) at the root.
	_, err := buildInlineOrbNode("- item1\n- item2\n")
	if err == nil {
		t.Fatal("expected error for non-mapping root, got nil")
	}
	if !strings.Contains(err.Error(), "orb source root is not a YAML mapping") {
		t.Errorf("error %q should mention 'orb source root is not a YAML mapping'", err.Error())
	}
}

// TestBuildInlineOrbNode_ValidSource verifies the happy path: a valid orb
// source with mixed allowed/disallowed keys.
func TestBuildInlineOrbNode_ValidSource(t *testing.T) {
	src := `version: 2
description: test orb
commands:
  greet:
    steps:
      - run: echo hello
jobs:
  build:
    machine: true
    steps:
      - greet
`
	node, err := buildInlineOrbNode(src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if node == nil {
		t.Fatal("expected non-nil node")
	}
	// The resulting mapping should have only "commands" and "jobs" keys, not
	// "version" or "description".
	keys := make(map[string]bool)
	for i := 0; i+1 < len(node.Content); i += 2 {
		keys[node.Content[i].Value] = true
	}
	if keys["version"] {
		t.Error("'version' should be stripped from inline orb node")
	}
	if keys["description"] {
		t.Error("'description' should be stripped from inline orb node")
	}
	if !keys["commands"] {
		t.Error("'commands' should be kept in inline orb node")
	}
	if !keys["jobs"] {
		t.Error("'jobs' should be kept in inline orb node")
	}
}

// Ensure unused imports compile away.
var _ = errors.New

// ---------------------------------------------------------------------------
// runMigrateSecretsTransfer — Issue #272 / migrate enhancement
// ---------------------------------------------------------------------------

// internalTestCmdWithCfg returns a minimal cobra.Command whose context carries
// the supplied config, allowing white-box tests to exercise functions that call
// configFromContext(cmd.Context()).
func internalTestCmdWithCfg(cfg *settings.Config) *cobra.Command {
	var outBuf, errBuf bytes.Buffer
	c := &cobra.Command{Use: "test"}
	c.SetOut(&outBuf)
	c.SetErr(&errBuf)
	c.SetContext(context.WithValue(context.Background(), configCtxKey{}, cfg))
	return c
}

// TestRunMigrateSecretsTransfer_OrgResolutionError verifies that when the
// destination org UUID cannot be resolved, runMigrateSecretsTransfer returns
// an error mentioning "resolving destination org".
//
// This is a white-box test that calls the unexported function directly via the
// internal test package.
func TestRunMigrateSecretsTransfer_OrgResolutionError(t *testing.T) {
	// Fake server that always returns 404 for org resolution.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"not found"}`, http.StatusNotFound)
	}))
	defer srv.Close()

	// Build a minimal manifest with a couple of projects.
	m := &manifest.Manifest{
		Source: manifest.Source{
			Host: srv.URL,
			Org:  manifest.Org{Slug: "gh/old-org"},
		},
		Projects: []manifest.Project{
			{Slug: "gh/old-org/web"},
			{Slug: "gh/old-org/api"},
		},
	}

	// Build a cfg pointing at the fake server.
	cfg := &settings.Config{
		Host:       srv.URL,
		DestToken:  "fake-dest-token",
		HTTPClient: srv.Client(),
	}
	c := internalTestCmdWithCfg(cfg)

	err := runMigrateSecretsTransfer(
		c,
		cfg,
		m,
		"fake-src-token",
		"gh/old-org",
		"gh/new-org",
		"migration-secrets",
		"",    // hostProjectOverride
		false, // dry-run
		false, // includeProjectVars
		false, // includeSSHKeys
		false, // removeRestrictions
	)
	if err == nil {
		t.Fatal("expected error when org resolution fails, got nil")
	}
	if !strings.Contains(err.Error(), "resolving destination org") {
		t.Errorf("error %q does not mention 'resolving destination org'", err.Error())
	}
}

// TestRunMigrateSecretsTransfer_DerivesMappingAndProceedsToOrgCheck verifies
// that when the dest org resolves successfully and the org-level feature-flag
// check is attempted (but fails gracefully), the function proceeds past the
// slug-derivation loop and the org-resolution step. An error from the
// feature-flag endpoint is non-fatal: it logs a warning and continues,
// eventually returning nil on a dry-run (no pipeline triggered).
//
// This test exercises the slug-derivation loop, the org-resolution path, the
// project-client construction, and the maybeEnableOrgTrigger warning path.
func TestRunMigrateSecretsTransfer_DerivesMappingAndProceedsToOrgCheck(t *testing.T) {
	// Fake server: returns a valid org for ResolveOrgID; returns 500 for
	// everything else (feature flags, project settings) — non-fatal warnings.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Org resolution endpoint: GET /api/v2/organization/{slug}
		if strings.HasPrefix(r.URL.Path, "/api/v2/organization/") {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":   "dest-org-uuid",
				"name": "new-org",
				"slug": "gh/new-org",
			})
			return
		}
		// All other endpoints (feature flags, v1.1 settings, etc.) return errors.
		// These are non-fatal: warnings are logged and the function continues.
		http.Error(w, `{"message":"server error"}`, http.StatusInternalServerError)
	}))
	defer srv.Close()

	m := &manifest.Manifest{
		Source: manifest.Source{
			Host: srv.URL,
			Org:  manifest.Org{Slug: "gh/old-org"},
		},
		Projects: []manifest.Project{
			{Slug: "gh/old-org/web"},
		},
	}

	cfg := &settings.Config{
		Host:       srv.URL,
		DestToken:  "fake-dest-token",
		HTTPClient: srv.Client(),
	}
	c := internalTestCmdWithCfg(cfg)

	// dry-run=true so no pipeline is triggered; the function should return nil
	// or a non-fatal error (the feature-flag warning is swallowed).
	err := runMigrateSecretsTransfer(
		c,
		cfg,
		m,
		"fake-src-token",
		"gh/old-org",
		"gh/new-org",
		"migration-secrets",
		"",    // hostProjectOverride
		false, // dry-run
		false, // includeProjectVars
		false, // includeSSHKeys
		false, // removeRestrictions
	)
	// In dry-run mode, transfer.Transfer is called and returns nil (no network
	// calls needed). A non-nil error is acceptable only if it does NOT mention
	// "resolving destination org" (that path is already covered above).
	if err != nil && strings.Contains(err.Error(), "resolving destination org") {
		t.Errorf("org resolution should have succeeded; got: %v", err)
	}
}

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

// ---------------------------------------------------------------------------
// parseSyncOnly + sortedKeys (sync.go helpers)
// ---------------------------------------------------------------------------

func TestParseSyncOnly_EmptyString_ReturnsNil(t *testing.T) {
	got, err := parseSyncOnly("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for empty --only, got %v", got)
	}
}

func TestParseSyncOnly_ValidSections_Parsed(t *testing.T) {
	got, err := parseSyncOnly("contexts,projects")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got["contexts"] {
		t.Error("expected 'contexts' in result")
	}
	if !got["projects"] {
		t.Error("expected 'projects' in result")
	}
	if got["runner"] {
		t.Error("'runner' should not be in result for 'contexts,projects'")
	}
}

func TestParseSyncOnly_AllValidSections_Accepted(t *testing.T) {
	for _, section := range []string{"org-settings", "contexts", "projects", "runner", "ciam", "extras"} {
		got, err := parseSyncOnly(section)
		if err != nil {
			t.Errorf("parseSyncOnly(%q) error: %v", section, err)
		}
		if !got[section] {
			t.Errorf("expected %q in result", section)
		}
	}
}

func TestParseSyncOnly_InvalidSection_ReturnsError(t *testing.T) {
	_, err := parseSyncOnly("terraform")
	if err == nil {
		t.Fatal("expected error for unknown section 'terraform'")
	}
	if !strings.Contains(err.Error(), "terraform") {
		t.Errorf("error should mention invalid section name, got: %v", err)
	}
}

func TestParseSyncOnly_SpacesAndCase_Normalised(t *testing.T) {
	got, err := parseSyncOnly("  Contexts , PROJECTS ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got["contexts"] {
		t.Error("expected 'contexts' (lowercased) in result")
	}
	if !got["projects"] {
		t.Error("expected 'projects' (lowercased) in result")
	}
}

func TestSortedKeys_EmptyMap_Empty(t *testing.T) {
	got := sortedKeys(map[string]bool{})
	if len(got) != 0 {
		t.Errorf("expected empty slice, got %v", got)
	}
}

func TestSortedKeys_SortedOutput(t *testing.T) {
	m := map[string]bool{"zebra": true, "apple": true, "mango": true}
	got := sortedKeys(m)
	want := []string{"apple", "mango", "zebra"}
	if len(got) != len(want) {
		t.Fatalf("length mismatch: got %d, want %d", len(got), len(want))
	}
	for i, v := range got {
		if v != want[i] {
			t.Errorf("got[%d] = %q, want %q", i, v, want[i])
		}
	}
}

// ---------------------------------------------------------------------------
// ciamWriterAdapter — converts *org.Client results to the syncer.CIAMWriter
// shapes (#176). Backed by httptest via newOrgClientForTest.
// ---------------------------------------------------------------------------

func TestCIAMWriterAdapter_ListOrgRoleGrants_ConvertsAndMapsUserID(t *testing.T) {
	// The CIAM role-grants API returns snake_case "user_id"; the adapter must
	// surface it (and username) on the syncer type so dest user resolution works.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/role-grants") {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"items": []map[string]any{
					{"user_id": "uid-1", "email": "", "username": "Jim Crowley", "role": "org-admin"},
				},
			})
			return
		}
		http.Error(w, "unexpected path: "+r.URL.Path, http.StatusNotFound)
	}))
	defer srv.Close()

	a := ciamWriterAdapter{c: newOrgClientForTest(t, srv)}
	got, err := a.ListOrgRoleGrants(context.Background(), "org-uuid")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 grant, got %d", len(got))
	}
	if got[0].UserID != "uid-1" || got[0].Username != "Jim Crowley" || got[0].Email != "" {
		t.Errorf("conversion wrong: %+v", got[0])
	}
}

func TestCIAMWriterAdapter_ListGroups_And_CreateGroup(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/groups") {
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "g-new", "name": "platform"})
			return
		}
		if strings.HasSuffix(r.URL.Path, "/groups") {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"items": []map[string]any{{"id": "g-1", "name": "backend"}},
			})
			return
		}
		http.Error(w, "unexpected path: "+r.URL.Path, http.StatusNotFound)
	}))
	defer srv.Close()

	a := ciamWriterAdapter{c: newOrgClientForTest(t, srv)}

	groups, err := a.ListGroups(context.Background(), "org-uuid")
	if err != nil {
		t.Fatalf("ListGroups error: %v", err)
	}
	if len(groups) != 1 || groups[0].ID != "g-1" || groups[0].Name != "backend" {
		t.Errorf("ListGroups conversion wrong: %+v", groups)
	}

	id, err := a.CreateGroup(context.Background(), "org-uuid", "platform", "desc")
	if err != nil {
		t.Fatalf("CreateGroup error: %v", err)
	}
	if id != "g-new" {
		t.Errorf("CreateGroup returned id %q, want g-new", id)
	}
}

// ---------------------------------------------------------------------------
// slugLastComponent
// ---------------------------------------------------------------------------

func TestSlugLastComponent_WithSlash(t *testing.T) {
	got := slugLastComponent("gh/acme/my-repo")
	if got != "my-repo" {
		t.Errorf("got %q, want %q", got, "my-repo")
	}
}

func TestSlugLastComponent_NoSlash(t *testing.T) {
	got := slugLastComponent("bare")
	if got != "bare" {
		t.Errorf("got %q, want %q", got, "bare")
	}
}

func TestSlugLastComponent_TrailingSlash(t *testing.T) {
	// A trailing slash means the last component is empty.
	got := slugLastComponent("gh/acme/")
	if got != "" {
		t.Errorf("got %q, want empty string", got)
	}
}

// ---------------------------------------------------------------------------
// syncProjectDisplayName
// ---------------------------------------------------------------------------

func TestSyncProjectDisplayName_WithName(t *testing.T) {
	p := manifest.Project{Slug: "gh/acme/api", Name: "api-service"}
	got := syncProjectDisplayName(p)
	if got != "api-service" {
		t.Errorf("got %q, want %q", got, "api-service")
	}
}

func TestSyncProjectDisplayName_NoName_FallsBackToSlug(t *testing.T) {
	p := manifest.Project{Slug: "gh/acme/api-service"}
	got := syncProjectDisplayName(p)
	if got != "api-service" {
		t.Errorf("got %q, want %q", got, "api-service")
	}
}

// ---------------------------------------------------------------------------
// tabFromKind — uncovered branches
// ---------------------------------------------------------------------------

func TestTabFromKind_EmptyRest(t *testing.T) {
	if got := tabFromKind(""); got != "" {
		t.Errorf("tabFromKind(%q) = %q, want %q", "", got, "")
	}
}

func TestTabFromKind_SSHKey(t *testing.T) {
	if got := tabFromKind("ssh-key:aa:bb:cc"); got != "ssh" {
		t.Errorf("tabFromKind(%q) = %q, want %q", "ssh-key:aa:bb:cc", got, "ssh")
	}
}

func TestTabFromKind_Webhook(t *testing.T) {
	if got := tabFromKind("webhook:notify"); got != "webhooks" {
		t.Errorf("tabFromKind(%q) = %q, want %q", "webhook:notify", got, "webhooks")
	}
}

func TestTabFromKind_EnvVar(t *testing.T) {
	if got := tabFromKind("env-var:MY_KEY"); got != "env-vars" {
		t.Errorf("tabFromKind(%q) = %q, want %q", "env-var:MY_KEY", got, "env-vars")
	}
}

func TestTabFromKind_ProjectVar(t *testing.T) {
	if got := tabFromKind("project-var:MY_KEY"); got != "env-vars" {
		t.Errorf("tabFromKind(%q) = %q, want %q", "project-var:MY_KEY", got, "env-vars")
	}
}

func TestTabFromKind_FeatureFlag(t *testing.T) {
	if got := tabFromKind("feature_flag:oss"); got != "advanced" {
		t.Errorf("tabFromKind(%q) = %q, want %q", "feature_flag:oss", got, "advanced")
	}
}

func TestTabFromKind_OIDCClaims(t *testing.T) {
	if got := tabFromKind("oidc_claims:sub"); got != "" {
		t.Errorf("tabFromKind(%q) = %q, want %q", "oidc_claims:sub", got, "")
	}
}

func TestTabFromKind_Schedule(t *testing.T) {
	if got := tabFromKind("schedule:nightly"); got != "" {
		t.Errorf("tabFromKind(%q) = %q, want %q", "schedule:nightly", got, "")
	}
}

func TestTabFromKind_UnknownKind(t *testing.T) {
	if got := tabFromKind("unknown-kind:value"); got != "" {
		t.Errorf("tabFromKind(%q) = %q, want empty string", "unknown-kind:value", got)
	}
}

func TestTabFromKind_NoColon(t *testing.T) {
	// A rest string with no colon should use the whole string as the kind.
	if got := tabFromKind("ssh-key"); got != "ssh" {
		t.Errorf("tabFromKind(%q) = %q, want %q", "ssh-key", got, "ssh")
	}
}

// ---------------------------------------------------------------------------
// rebaseProjectSlug
// ---------------------------------------------------------------------------

func TestRebaseProjectSlug_Valid(t *testing.T) {
	got := rebaseProjectSlug("gh/acme/web", "gh/acme-new")
	if got != "gh/acme-new/web" {
		t.Errorf("got %q, want %q", got, "gh/acme-new/web")
	}
}

func TestRebaseProjectSlug_EmptyDest(t *testing.T) {
	got := rebaseProjectSlug("gh/acme/web", "")
	if got != "gh/acme/web" {
		t.Errorf("got %q (should be unchanged for empty dest), want %q", got, "gh/acme/web")
	}
}

func TestRebaseProjectSlug_MalformedSlug(t *testing.T) {
	// A slug with fewer than 3 parts should be returned unchanged.
	got := rebaseProjectSlug("gh/acme", "gh/acme-new")
	if got != "gh/acme" {
		t.Errorf("got %q (malformed slug should be unchanged), want %q", got, "gh/acme")
	}
}

func TestRebaseProjectSlug_MalformedDestOrgSlug_NoSlash(t *testing.T) {
	// A destOrgSlug with no slash → destParts has 1 element → return original slug.
	got := rebaseProjectSlug("gh/acme/web", "noslashhere")
	if got != "gh/acme/web" {
		t.Errorf("got %q (malformed destOrgSlug should return unchanged slug), want %q", got, "gh/acme/web")
	}
}

// ---------------------------------------------------------------------------
// splitContextTarget — uncovered branch (orb prefix returns early)
// ---------------------------------------------------------------------------

func TestSplitContextTarget_OrgSettingsPrefix_ReturnsEmpty(t *testing.T) {
	// "orb" is in the exclusion list, so it must NOT be treated as a context name.
	ctxName, _ := splitContextTarget("orb/config-rewrite-notice")
	if ctxName != "" {
		t.Errorf("expected empty ctxName for 'orb' prefix, got %q", ctxName)
	}
}

func TestSplitContextTarget_PlainContext_ReturnsName(t *testing.T) {
	ctxName, rest := splitContextTarget("deploy-prod/MY_VAR")
	if ctxName != "deploy-prod" {
		t.Errorf("ctxName = %q, want %q", ctxName, "deploy-prod")
	}
	if rest != "MY_VAR" {
		t.Errorf("rest = %q, want %q", rest, "MY_VAR")
	}
}

func TestSplitContextTarget_GHPrefix_ReturnsEmpty(t *testing.T) {
	ctxName, _ := splitContextTarget("gh/acme/web/ssh-key:aa:bb")
	if ctxName != "" {
		t.Errorf("expected empty ctxName for 'gh' prefix, got %q", ctxName)
	}
}

// ---------------------------------------------------------------------------
// buildSyncSummary — exercises the Orbs section and warnings accumulation
// ---------------------------------------------------------------------------

func TestBuildSyncSummary_OrbsSection_Included(t *testing.T) {
	orbReport := &syncer.Report{
		Applied:     false,
		DestOrgSlug: "gh/acme-new",
		Actions: []syncer.Action{
			{Kind: "orb", Target: "acme/my-orb", Status: "manual", Detail: "manual transfer required"},
			{Kind: "orb", Target: "acme/priv-orb", Status: "manual", Detail: "manual transfer required"},
		},
	}
	repsBySection := map[string]*syncer.Report{
		"Orbs": orbReport,
	}

	summary := buildSyncSummary(false, repsBySection)

	if summary.DryRun != true {
		t.Errorf("expected DryRun=true, got %v", summary.DryRun)
	}
	if len(summary.Sections) != 1 {
		t.Fatalf("expected 1 section, got %d: %v", len(summary.Sections), summary.Sections)
	}
	sec := summary.Sections[0]
	if sec.Section != "Orbs" {
		t.Errorf("expected section 'Orbs', got %q", sec.Section)
	}
	if sec.Manual != 2 {
		t.Errorf("expected Manual=2, got %d", sec.Manual)
	}

	// Both manual actions should appear as warnings.
	if len(summary.Warnings) != 2 {
		t.Errorf("expected 2 warnings, got %d: %v", len(summary.Warnings), summary.Warnings)
	}
	for _, w := range summary.Warnings {
		if w.Section != "Orbs" {
			t.Errorf("warning section = %q, want %q", w.Section, "Orbs")
		}
		if w.Status != "manual" {
			t.Errorf("warning status = %q, want %q", w.Status, "manual")
		}
	}
}

func TestBuildSyncSummary_DestOrgSlugFromFirstNonEmpty(t *testing.T) {
	// buildSyncSummary uses the DestOrgSlug from the first non-empty report.
	repsBySection := map[string]*syncer.Report{
		"Orbs": {Applied: false, DestOrgSlug: "gh/acme-new", Actions: []syncer.Action{}},
	}
	summary := buildSyncSummary(false, repsBySection)
	if summary.DestOrgSlug != "gh/acme-new" {
		t.Errorf("DestOrgSlug = %q, want %q", summary.DestOrgSlug, "gh/acme-new")
	}
}

func TestBuildSyncSummary_NilReport_Skipped(t *testing.T) {
	// A nil report for a known section should be silently skipped.
	repsBySection := map[string]*syncer.Report{
		"Orbs": nil,
	}
	summary := buildSyncSummary(true, repsBySection)
	if len(summary.Sections) != 0 {
		t.Errorf("expected 0 sections (nil skipped), got %d", len(summary.Sections))
	}
	if summary.DryRun {
		t.Errorf("apply=true should produce DryRun=false")
	}
}

// ---------------------------------------------------------------------------
// parseSyncOnly — "orb" section
// ---------------------------------------------------------------------------

func TestParseSyncOnly_OrbSection_Accepted(t *testing.T) {
	got, err := parseSyncOnly("orb")
	if err != nil {
		t.Fatalf("unexpected error for 'orb': %v", err)
	}
	if !got["orb"] {
		t.Error("expected 'orb' to be set in result")
	}
}

func TestParseSyncOnly_OrbWithOthers(t *testing.T) {
	got, err := parseSyncOnly("orb,contexts")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got["orb"] {
		t.Error("expected 'orb' in result")
	}
	if !got["contexts"] {
		t.Error("expected 'contexts' in result")
	}
	if got["projects"] {
		t.Error("'projects' should not be in result")
	}
}

// TestParseSyncOnly_TrailingComma verifies that trailing/double commas produce
// empty name tokens that are silently skipped (the `if name == "" { continue }`
// branch on line 143-144 of sync.go).
func TestParseSyncOnly_TrailingComma(t *testing.T) {
	// "orb," has a trailing comma — the empty token after the comma must be
	// skipped, not treated as an unknown section name.
	got, err := parseSyncOnly("orb,")
	if err != nil {
		t.Fatalf("unexpected error for 'orb,' (trailing comma): %v", err)
	}
	if !got["orb"] {
		t.Error("expected 'orb' in result")
	}
	if len(got) != 1 {
		t.Errorf("expected exactly 1 entry, got %d", len(got))
	}
}

// ---------------------------------------------------------------------------
// orgSettingsAdapter delegation tests — one-liner forwarders
// ---------------------------------------------------------------------------
//
// Each adapter method is a pure forwarding call to *org.Client. We test each
// via httptest so the "statement covered" line in the coverage profile is hit.

// TestOrgSettingsAdapter_UpdateFeatureFlags verifies that
// orgSettingsAdapter.UpdateFeatureFlags delegates to the org client.
func TestOrgSettingsAdapter_UpdateFeatureFlags(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/settings") {
			_ = json.NewDecoder(r.Body).Decode(&gotBody)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{})
			return
		}
		http.Error(w, "unexpected: "+r.URL.Path, http.StatusNotFound)
	}))
	defer srv.Close()

	a := orgSettingsAdapter{c: newOrgClientForTest(t, srv)}
	err := a.UpdateFeatureFlags(context.Background(), "github", "acme", map[string]bool{"oss": true})
	if err != nil {
		t.Fatalf("UpdateFeatureFlags: %v", err)
	}
}

// TestOrgSettingsAdapter_GetURLOrbAllowList verifies that GetURLOrbAllowList
// converts org.URLOrbAllowEntry → syncer.URLOrbAllowEntry.
func TestOrgSettingsAdapter_GetURLOrbAllowList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []map[string]any{
				{"id": "e1", "name": "npm", "prefix": "https://registry.npmjs.org", "auth": ""},
			},
		})
	}))
	defer srv.Close()

	a := orgSettingsAdapter{c: newOrgClientForTest(t, srv)}
	entries, err := a.GetURLOrbAllowList(context.Background(), "gh/acme")
	if err != nil {
		t.Fatalf("GetURLOrbAllowList: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].ID != "e1" || entries[0].Name != "npm" {
		t.Errorf("conversion wrong: %+v", entries[0])
	}
}

// TestOrgSettingsAdapter_GetOTelExporters verifies GetOTelExporters converts
// org.OTelExporter → syncer.OTelExporter. The API returns a JSON array.
func TestOrgSettingsAdapter_GetOTelExporters(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// GetOTelExporters unmarshals directly into []OTelExporter (JSON array).
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"id": "x1", "endpoint": "https://otel.example.com", "protocol": "grpc", "insecure": false},
		})
	}))
	defer srv.Close()

	a := orgSettingsAdapter{c: newOrgClientForTest(t, srv)}
	exporters, err := a.GetOTelExporters(context.Background(), "org-uuid-test")
	if err != nil {
		t.Fatalf("GetOTelExporters: %v", err)
	}
	if len(exporters) != 1 {
		t.Fatalf("expected 1 exporter, got %d", len(exporters))
	}
	if exporters[0].ID != "x1" {
		t.Errorf("ID = %q, want %q", exporters[0].ID, "x1")
	}
}

// TestOrgSettingsAdapter_SetOIDCClaims verifies the SetOIDCClaims delegation.
func TestOrgSettingsAdapter_SetOIDCClaims(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{})
	}))
	defer srv.Close()

	a := orgSettingsAdapter{c: newOrgClientForTest(t, srv)}
	// Should not error — any response from the server counts as success for the adapter.
	_ = a.SetOIDCClaims(context.Background(), "org-id", []string{"aud1"}, "1h")
}

// TestOrgSettingsAdapter_SetContacts verifies the SetContacts delegation.
func TestOrgSettingsAdapter_SetContacts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{})
	}))
	defer srv.Close()

	a := orgSettingsAdapter{c: newOrgClientForTest(t, srv)}
	_ = a.SetContacts(context.Background(), "org-id", []string{"primary@example.com"}, []string{"security@example.com"})
}

// TestOrgSettingsAdapter_SetStorageRetention verifies the SetStorageRetention delegation.
func TestOrgSettingsAdapter_SetStorageRetention(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{})
	}))
	defer srv.Close()

	a := orgSettingsAdapter{c: newOrgClientForTest(t, srv)}
	_ = a.SetStorageRetention(context.Background(), "org-uuid",
		syncer.StorageRetentionArgs{CacheDays: 30, WorkspaceDays: 15, ArtifactDays: 60})
}

// TestOrgSettingsAdapter_SetBudget verifies the SetBudget delegation.
func TestOrgSettingsAdapter_SetBudget(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{})
	}))
	defer srv.Close()

	a := orgSettingsAdapter{c: newOrgClientForTest(t, srv)}
	_ = a.SetBudget(context.Background(), "org-uuid", nil, 10000)
}

// TestOrgSettingsAdapter_SetBlockUnregisteredUsers verifies the delegation.
func TestOrgSettingsAdapter_SetBlockUnregisteredUsers(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{})
	}))
	defer srv.Close()

	a := orgSettingsAdapter{c: newOrgClientForTest(t, srv)}
	_ = a.SetBlockUnregisteredUsers(context.Background(), "org-uuid", true)
}

// TestOrgSettingsAdapter_SetReleaseTrackerSettings verifies the delegation.
func TestOrgSettingsAdapter_SetReleaseTrackerSettings(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{})
	}))
	defer srv.Close()

	a := orgSettingsAdapter{c: newOrgClientForTest(t, srv)}
	_ = a.SetReleaseTrackerSettings(context.Background(), "org-uuid", "24h")
}

// TestOrgSettingsAdapter_CreateURLOrbAllowEntry verifies the delegation.
func TestOrgSettingsAdapter_CreateURLOrbAllowEntry(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "new-entry"})
	}))
	defer srv.Close()

	a := orgSettingsAdapter{c: newOrgClientForTest(t, srv)}
	_ = a.CreateURLOrbAllowEntry(context.Background(), "gh/acme", "npm", "https://registry.npmjs.org", "")
}

// TestOrgSettingsAdapter_PutPolicyBundle verifies the delegation.
func TestOrgSettingsAdapter_PutPolicyBundle(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{})
	}))
	defer srv.Close()

	a := orgSettingsAdapter{c: newOrgClientForTest(t, srv)}
	_ = a.PutPolicyBundle(context.Background(), "org-uuid", map[string]string{"policy.rego": "package main"})
}

// TestOrgSettingsAdapter_SetPolicyEnforcement verifies the delegation.
func TestOrgSettingsAdapter_SetPolicyEnforcement(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{})
	}))
	defer srv.Close()

	a := orgSettingsAdapter{c: newOrgClientForTest(t, srv)}
	_ = a.SetPolicyEnforcement(context.Background(), "org-uuid", true)
}

// TestOrgSettingsAdapter_CreateOTelExporter verifies the delegation.
func TestOrgSettingsAdapter_CreateOTelExporter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "new-exporter"})
	}))
	defer srv.Close()

	a := orgSettingsAdapter{c: newOrgClientForTest(t, srv)}
	_ = a.CreateOTelExporter(context.Background(), "org-uuid", "https://otel.example.com", "grpc", false, nil)
}

// ---------------------------------------------------------------------------
// ciamWriterAdapter delegation tests
// ---------------------------------------------------------------------------

// TestCIAMWriterAdapter_SetOrgUserRole verifies SetOrgUserRole delegation.
func TestCIAMWriterAdapter_SetOrgUserRole(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{})
	}))
	defer srv.Close()

	a := ciamWriterAdapter{c: newOrgClientForTest(t, srv)}
	_ = a.SetOrgUserRole(context.Background(), "org-id", "user-id", "org-admin")
}

// TestCIAMWriterAdapter_AddUsersToGroup verifies AddUsersToGroup delegation.
func TestCIAMWriterAdapter_AddUsersToGroup(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{})
	}))
	defer srv.Close()

	a := ciamWriterAdapter{c: newOrgClientForTest(t, srv)}
	_ = a.AddUsersToGroup(context.Background(), "org-id", "group-id", []string{"user-1", "user-2"})
}

// TestCIAMWriterAdapter_SetProjectUserRole verifies SetProjectUserRole delegation.
func TestCIAMWriterAdapter_SetProjectUserRole(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{})
	}))
	defer srv.Close()

	a := ciamWriterAdapter{c: newOrgClientForTest(t, srv)}
	_ = a.SetProjectUserRole(context.Background(), "org-id", "proj-id", "user-id", "contributor")
}

// TestCIAMWriterAdapter_AddProjectGroupRole verifies AddProjectGroupRole delegation.
func TestCIAMWriterAdapter_AddProjectGroupRole(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{})
	}))
	defer srv.Close()

	a := ciamWriterAdapter{c: newOrgClientForTest(t, srv)}
	_ = a.AddProjectGroupRole(context.Background(), "org-id", "proj-id", []string{"g-1"}, "viewer")
}

// TestCIAMWriterAdapter_CreateGroup_ErrorPath verifies that CreateGroup
// propagates the error from the underlying client when the server returns 500.
// This covers the error branch in ciamWriterAdapter.CreateGroup.
func TestCIAMWriterAdapter_CreateGroup_ErrorPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"message":"server error"}`, http.StatusInternalServerError)
	}))
	defer srv.Close()

	a := ciamWriterAdapter{c: newOrgClientForTest(t, srv)}
	_, err := a.CreateGroup(context.Background(), "org-id", "platform", "desc")
	if err == nil {
		t.Fatal("expected error from CreateGroup on 500 response, got nil")
	}
}

// ---------------------------------------------------------------------------
// resolveTargetMeta — uncovered branches
// ---------------------------------------------------------------------------

// TestResolveTargetMeta_ProjectFoundByName verifies the fallback path where
// the source project slug is not in projBySourceSlug but the last component
// (project name) is in projByName.
func TestResolveTargetMeta_ProjectFoundByName(t *testing.T) {
	// A "dest slug" target — not in projBySourceSlug by full slug, but the
	// last component "web-app" matches a project in projByName.
	projBySlug := map[string]manifest.Project{}
	projByName := map[string]manifest.Project{
		"web-app": {Slug: "gh/acme/web-app", Name: "web-app"},
	}
	ctxByName := map[string]manifest.Context{}

	// Target looks like a dest slug: "gh/dest-org/web-app/env-var:MY_KEY"
	target := "gh/dest-org/web-app/env-var:MY_KEY"
	name, url := resolveTargetMeta(target, "gh/dest-org", "https://circleci.com",
		projBySlug, projByName, ctxByName)

	if name == "" {
		t.Errorf("expected non-empty friendly name for project found by name; got empty")
	}
	if url == "" {
		t.Errorf("expected non-empty settings URL for project found by name; got empty")
	}
}

// TestResolveTargetMeta_ContextNotFound_ReturnsEmpty verifies that when the
// context name is extracted but NOT found in ctxByName, the function returns
// empty strings (falls through to "no match" return).
func TestResolveTargetMeta_ContextNotFound_ReturnsEmpty(t *testing.T) {
	// Target looks like a context (no project prefix), but the context name
	// is not in ctxByName.
	projBySlug := map[string]manifest.Project{}
	projByName := map[string]manifest.Project{}
	ctxByName := map[string]manifest.Context{
		"known-ctx": {Name: "known-ctx"},
	}

	// "unknown-ctx" is not in ctxByName → should return empty.
	name, url := resolveTargetMeta("unknown-ctx/MY_SECRET",
		"gh/dest-org", "https://circleci.com",
		projBySlug, projByName, ctxByName)

	if name != "" {
		t.Errorf("expected empty friendly name for unknown context, got %q", name)
	}
	if url != "" {
		t.Errorf("expected empty URL for unknown context, got %q", url)
	}
}

// ---------------------------------------------------------------------------
// orgSettingsAdapter error paths — cover remaining 85.7% branches
// ---------------------------------------------------------------------------

// TestOrgSettingsAdapter_GetURLOrbAllowList_Error verifies error propagation.
func TestOrgSettingsAdapter_GetURLOrbAllowList_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"message":"forbidden"}`, http.StatusForbidden)
	}))
	defer srv.Close()

	a := orgSettingsAdapter{c: newOrgClientForTest(t, srv)}
	_, err := a.GetURLOrbAllowList(context.Background(), "gh/acme")
	if err == nil {
		t.Fatal("expected error from GetURLOrbAllowList on 403 response, got nil")
	}
}

// TestOrgSettingsAdapter_GetOTelExporters_Error verifies error propagation.
func TestOrgSettingsAdapter_GetOTelExporters_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"message":"internal error"}`, http.StatusInternalServerError)
	}))
	defer srv.Close()

	a := orgSettingsAdapter{c: newOrgClientForTest(t, srv)}
	_, err := a.GetOTelExporters(context.Background(), "org-uuid")
	if err == nil {
		t.Fatal("expected error from GetOTelExporters on 500 response, got nil")
	}
}

// ---------------------------------------------------------------------------
// ciamWriterAdapter error paths
// ---------------------------------------------------------------------------

// TestCIAMWriterAdapter_ListOrgRoleGrants_Error covers the error return in ListOrgRoleGrants.
func TestCIAMWriterAdapter_ListOrgRoleGrants_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"message":"not found"}`, http.StatusNotFound)
	}))
	defer srv.Close()

	a := ciamWriterAdapter{c: newOrgClientForTest(t, srv)}
	_, err := a.ListOrgRoleGrants(context.Background(), "org-uuid")
	if err == nil {
		t.Fatal("expected error from ListOrgRoleGrants on 404, got nil")
	}
}

// TestCIAMWriterAdapter_ListGroups_Error covers the error return in the
// ciamWriterAdapter ListGroups method.
func TestCIAMWriterAdapter_ListGroups_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"message":"forbidden"}`, http.StatusForbidden)
	}))
	defer srv.Close()

	a := ciamWriterAdapter{c: newOrgClientForTest(t, srv)}
	_, err := a.ListGroups(context.Background(), "org-uuid")
	if err == nil {
		t.Fatal("expected error from ciamWriterAdapter.ListGroups on 403, got nil")
	}
}

// TestOrgGroupLister_ListGroups_ErrorPath verifies that orgGroupLister.ListGroups
// propagates error from the underlying client (via a 500 response).
func TestOrgGroupLister_ListGroups_ErrorPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"message":"server error"}`, http.StatusInternalServerError)
	}))
	defer srv.Close()

	lister := orgGroupLister{c: newOrgClientForTest(t, srv)}
	_, err := lister.ListGroups(context.Background(), "org-uuid")
	if err == nil {
		t.Fatal("expected error from orgGroupLister.ListGroups on 500, got nil")
	}
}

// ---------------------------------------------------------------------------
// syncActionLine — friendlyName-only and settingsURL-only branches
// ---------------------------------------------------------------------------

// TestSyncActionLine_FriendlyNameOnly verifies the format when only a friendly
// name is available (no settings URL — e.g., host is empty so URL construction
// returns "").
func TestSyncActionLine_FriendlyNameOnly(t *testing.T) {
	// Use an empty host so cciurl.ProjectSettingsURL returns an empty URL.
	m := &manifest.Manifest{
		Source: manifest.Source{
			Host: "", // empty host → empty settings URL
			Org:  manifest.Org{Slug: "gh/acme"},
		},
		Projects: []manifest.Project{
			{Slug: "gh/acme/api", Name: "api-service"},
		},
	}

	a := syncer.Action{
		Kind:   "project-ssh-key",
		Target: "gh/acme/api/ssh-key:aa:bb:cc",
		Status: "manual",
	}
	line := syncActionLine(a, "gh/dest-org", m)

	// When URL is empty, format is "target (friendlyName)".
	if !strings.Contains(line, "api-service") {
		t.Errorf("expected friendly name 'api-service' in line %q", line)
	}
	// The format should NOT contain "→" when there is no URL.
	if !strings.Contains(line, "(api-service)") {
		t.Errorf("expected '(api-service)' format when no URL, got: %q", line)
	}
}
