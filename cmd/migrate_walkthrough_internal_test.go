package cmd

// Internal (white-box) tests for unexported helpers in migrate.go that the
// external cmd_test package cannot reach: the runMigrateWalkthrough thin
// wrapper (which reads from os.Stdin) and the yesNo formatter.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
	"github.com/AwesomeCICD/circleci-org-migration-cli/settings"
)

// withStdin temporarily replaces os.Stdin with a pipe whose read end yields
// the supplied input, restoring the original on cleanup.  It lets us drive the
// runMigrateWalkthrough wrapper (which hard-codes os.Stdin) without a TTY.
func withStdin(t *testing.T, input string) {
	t.Helper()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	if _, err := w.WriteString(input); err != nil {
		t.Fatalf("write to pipe: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close pipe writer: %v", err)
	}

	orig := os.Stdin
	os.Stdin = r
	t.Cleanup(func() {
		os.Stdin = orig
		_ = r.Close()
	})
}

// TestRunMigrateWalkthrough_Wrapper drives the unexported runMigrateWalkthrough
// wrapper end-to-end with a scripted stdin, covering the thin delegation to
// RunMigrateWalkthroughWith (none-method, dry-run path).
func TestRunMigrateWalkthrough_Wrapper(t *testing.T) {
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "fake-src-tok")
	t.Setenv("CIRCLECI_DEST_TOKEN", "fake-dst-tok")
	t.Setenv("CIRCLECI_CLI_TOKEN", "")

	lines := []string{
		"",  // components: default (all)
		"",  // source orb namespace: accept default (src)
		"",  // dest orb namespace: accept default (dst)
		"",  // source runner namespace: accept default (src)
		"",  // dest runner namespace: accept default (dst)
		"3", // secrets method: none
		"1", // missing-secrets: skip
		"y", // dry run
	}
	withStdin(t, strings.Join(lines, "\n")+"\n")

	root := MakeCommands()
	var outBuf strings.Builder
	root.SetOut(&outBuf)
	root.SetErr(&outBuf)

	res, err := runMigrateWalkthrough(root, &settings.Config{}, "gh/src", "gh/dst", false)
	if err != nil {
		t.Fatalf("runMigrateWalkthrough error: %v", err)
	}
	if res.SourceOrg != "gh/src" || res.DestOrg != "gh/dst" {
		t.Errorf("orgs = (%q, %q), want (gh/src, gh/dst)", res.SourceOrg, res.DestOrg)
	}
	if res.Apply {
		t.Error("expected apply=false for dry-run choice")
	}
}

// TestRunMigrateWalkthrough_Wrapper_PropagatesError verifies the wrapper
// surfaces an error from the underlying walkthrough (apply confirmation
// declined → "cancelled" error).
func TestRunMigrateWalkthrough_Wrapper_PropagatesError(t *testing.T) {
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "fake-src-tok")
	t.Setenv("CIRCLECI_DEST_TOKEN", "fake-dst-tok")
	t.Setenv("CIRCLECI_CLI_TOKEN", "")

	lines := []string{
		"",  // components: default
		"",  // source orb namespace: accept default (src)
		"",  // dest orb namespace: accept default (dst)
		"",  // source runner namespace: accept default (src)
		"",  // dest runner namespace: accept default (dst)
		"3", // secrets method: none
		"1", // missing-secrets: skip
		"n", // do NOT dry run → apply=true
		"n", // decline confirmation
	}
	withStdin(t, strings.Join(lines, "\n")+"\n")

	root := MakeCommands()
	var outBuf strings.Builder
	root.SetOut(&outBuf)
	root.SetErr(&outBuf)

	_, err := runMigrateWalkthrough(root, &settings.Config{}, "gh/src", "gh/dst", false)
	if err == nil {
		t.Fatal("expected cancellation error from wrapper")
	}
	if !strings.Contains(err.Error(), "cancelled") {
		t.Errorf("expected 'cancelled' error, got: %v", err)
	}
}

// TestRunCaptureWalkthrough_Wrapper drives the unexported
// runCaptureWalkthrough wrapper end-to-end with a scripted stdin and a
// temp-file manifest, covering its thin delegation to
// RunCaptureWalkthroughWith.
func TestRunCaptureWalkthrough_Wrapper(t *testing.T) {
	dir := t.TempDir()
	mPath := filepath.Join(dir, "manifest.json")
	m := &manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		Source: manifest.Source{
			Org: manifest.Org{Slug: "gh/acme", ID: "org-uuid-1"},
		},
		Contexts: []manifest.Context{
			{Name: "prod-ctx", EnvVars: []manifest.ContextEnvVar{{Name: "SECRET_KEY"}}},
		},
		Projects: []manifest.Project{
			{Slug: "gh/acme/web", SourceID: "proj-web-uuid", EnvVars: []manifest.ProjectEnvVar{{Name: "WEB_VAR"}}},
		},
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	if writeErr := os.WriteFile(mPath, data, 0o644); writeErr != nil {
		t.Fatalf("write manifest: %v", writeErr)
	}

	// Manifest + output preset (no prompts for those). Single project + single
	// context → host project auto-picked, so the scripted answers are:
	lines := []string{
		"",  // contexts: all (default)
		"",  // projects: all (default)
		"y", // encrypt? yes
		"1", // key choice: generate
		"1", // storage: artifact (default)
		"y", // set retention to 1 day
		"",  // branch: main (default)
		"y", // enable trigger
		"y", // confirm
	}
	withStdin(t, strings.Join(lines, "\n")+"\n")

	root := MakeCommands()
	var outBuf strings.Builder
	root.SetOut(&outBuf)
	root.SetErr(&outBuf)

	res, err := runCaptureWalkthrough(root, CaptureWalkthroughResult{
		ManifestPath: mPath,
		Output:       filepath.Join(dir, "secrets.json"),
	})
	if err != nil {
		t.Fatalf("runCaptureWalkthrough error: %v", err)
	}
	if res.HostProjectSlug != "gh/acme/web" {
		t.Errorf("HostProjectSlug = %q, want gh/acme/web (auto-picked)", res.HostProjectSlug)
	}
	if !res.EnableTrigger {
		t.Error("expected EnableTrigger=true")
	}
}

// overrideStdinTerminal forces isInteractiveTTY() to report tty, restoring the
// original on cleanup.  Mirrors overrideInteractiveTTY in doctor_test.go but is
// defined here to keep this file self-contained (different name avoids a
// redeclaration clash).
func overrideStdinTerminal(t *testing.T, tty bool) {
	t.Helper()
	orig := stdinIsTerminal
	stdinIsTerminal = func() bool { return tty }
	t.Cleanup(func() { stdinIsTerminal = orig })
}

// runMigrateCmdInteractive runs the migrate subcommand end-to-end through the
// interactive walkthrough by forcing the TTY check true and feeding scripted
// answers via os.Stdin.  This drives the RunE walkthrough branch (the field
// assignment from MigrateWalkthroughResult back to the flag vars) plus
// downstream validation that the external cmd_test package cannot reach.
func runMigrateCmdInteractive(t *testing.T, stdin string, args ...string) (string, error) {
	t.Helper()
	overrideStdinTerminal(t, true)
	withStdin(t, stdin)

	root := MakeCommands()
	var outBuf strings.Builder
	root.SetOut(&outBuf)
	root.SetErr(&outBuf)
	root.SetArgs(append([]string{"migrate"}, args...))

	err := root.Execute()
	return outBuf.String(), err
}

// TestMigrateCmd_Interactive_WalkthroughToValidation drives the migrate command
// through the interactive walkthrough with tokens entered at the prompt, then
// lets validation/export proceed.  The export step hits the network and fails,
// which is expected — the point is to cover the RunE walkthrough-assignment
// block, the token prompt branches, and the post-walkthrough validation that
// only run when the guided path is taken.
func TestMigrateCmd_Interactive_WalkthroughToValidation(t *testing.T) {
	// No tokens in the environment → the walkthrough prompts for them, which
	// exercises the token-prompt branches and flows the values into cfg.
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "")
	t.Setenv("CIRCLECI_DEST_TOKEN", "")
	t.Setenv("CIRCLECI_CLI_TOKEN", "")
	t.Setenv("CIRCLE_TOKEN", "")

	// Scripted answers (no org flags → both prompted):
	lines := []string{
		"gh/acme",      // source org
		"gh/acme-new",  // dest org
		"fake-src-tok", // source token (prompted)
		"fake-dst-tok", // dest token (prompted)
		"",             // components: default (all)
		"",             // source orb namespace: accept default (acme)
		"",             // dest orb namespace: accept default (acme-new)
		"",             // source runner namespace: accept default (acme)
		"",             // dest runner namespace: accept default (acme-new)
		"3",            // secrets method: none
		"1",            // missing-secrets: skip
		"y",            // dry run (apply=false)
	}
	stdin := strings.Join(lines, "\n") + "\n"

	// --skip-preflight so we go straight to export (which then fails on the
	// network — acceptable; we only need the walkthrough+validation coverage).
	out, err := runMigrateCmdInteractive(t, stdin, "--skip-preflight")

	// The banner proves the interactive walkthrough actually ran.
	if !strings.Contains(out, "guided mode") {
		t.Errorf("expected guided-mode banner in output; got:\n%s", out)
	}
	// An error is expected (export hits the network with a fake token); we just
	// assert it is NOT a validation/usage error about missing orgs or tokens,
	// proving the walkthrough-supplied values passed validation.
	if err != nil {
		msg := err.Error()
		if strings.Contains(msg, "--source-org is required") ||
			strings.Contains(msg, "--dest-org is required") ||
			strings.Contains(msg, "no source API token") ||
			strings.Contains(msg, "no destination API token") {
			t.Errorf("walkthrough values should have passed validation; got: %v", err)
		}
	}
}

// TestMigrateCmd_Interactive_NonTTYFailFast covers the non-TTY fail-fast guard:
// when interaction is wanted (no org flags) but stdin is not a terminal, the
// command must error before printing the banner.
func TestMigrateCmd_Interactive_NonTTYFailFast(t *testing.T) {
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "")
	t.Setenv("CIRCLECI_DEST_TOKEN", "")
	t.Setenv("CIRCLECI_CLI_TOKEN", "")
	t.Setenv("CIRCLE_TOKEN", "")

	overrideStdinTerminal(t, false)

	root := MakeCommands()
	var outBuf strings.Builder
	root.SetOut(&outBuf)
	root.SetErr(&outBuf)
	root.SetArgs([]string{"migrate"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected fail-fast error on non-TTY with no org flags")
	}
	if !strings.Contains(err.Error(), "requires a TTY") {
		t.Errorf("expected TTY fail-fast error, got: %v", err)
	}
}

// writeCaptureManifest writes a small manifest to a temp file and returns its
// path. Helper for the capture-walkthrough internal tests.
func writeCaptureManifest(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	mPath := filepath.Join(dir, "manifest.json")
	m := &manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		Source: manifest.Source{
			Org: manifest.Org{Slug: "gh/acme", ID: "org-uuid-1"},
		},
		Contexts: []manifest.Context{
			{Name: "prod-ctx", EnvVars: []manifest.ContextEnvVar{{Name: "SECRET_KEY"}}},
		},
		Projects: []manifest.Project{
			{Slug: "gh/acme/web", SourceID: "proj-web-uuid", EnvVars: []manifest.ProjectEnvVar{{Name: "WEB_VAR"}}},
		},
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	if writeErr := os.WriteFile(mPath, data, 0o644); writeErr != nil {
		t.Fatalf("write manifest: %v", writeErr)
	}
	return mPath
}

// TestRunCaptureWalkthroughWith_AllFieldsPreset drives RunCaptureWalkthroughWith
// with every value supplied up front (as if all flags were passed). This
// exercises the "(from --flag)" alternate branches in each step — the ones the
// prompt-driven tests never reach — leaving only the final confirmation prompt.
func TestRunCaptureWalkthroughWith_AllFieldsPreset(t *testing.T) {
	mPath := writeCaptureManifest(t)

	initial := CaptureWalkthroughResult{
		ManifestPath:          mPath,
		Output:                "out.json",
		ProjectSlugs:          []string{"gh/acme/web"},
		ContextNames:          []string{"prod-ctx"},
		HostProjectSlug:       "gh/acme/web",
		Branch:                "release",
		EnableTrigger:         true,
		ArtifactRetentionDays: 7,
		EncOpts: captureEncryptOpts{
			encrypt:      true,
			sshPublicKey: "/tmp/key.pub",
			storage:      "s3",
		},
	}

	// Only the final "Proceed with capture?" prompt remains.
	withInput := strings.NewReader("y\n")
	var outBuf strings.Builder
	root := MakeCommands()
	p := NewPrompter(withInput, &outBuf)

	res, err := RunCaptureWalkthroughWith(p, root, initial)
	if err != nil {
		t.Fatalf("walkthrough error: %v", err)
	}
	if res.Branch != "release" {
		t.Errorf("Branch = %q, want release", res.Branch)
	}
	if res.ArtifactRetentionDays != 7 {
		t.Errorf("ArtifactRetentionDays = %d, want 7", res.ArtifactRetentionDays)
	}
	out := outBuf.String()
	// Confirm the "(from --flag)" branches were taken.
	for _, want := range []string{"from --manifest", "from --context", "from --project", "from --host-project", "from --encrypt", "from --storage", "from --artifact-retention-days", "from --output", "from --branch", "from --enable-trigger"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected output to contain %q (preset branch); got:\n%s", want, out)
		}
	}
}

// TestRunCaptureWalkthroughWith_CancelledInternal covers the cancellation
// branch (final confirm = no) via the internal package.
func TestRunCaptureWalkthroughWith_CancelledInternal(t *testing.T) {
	mPath := writeCaptureManifest(t)
	initial := CaptureWalkthroughResult{
		ManifestPath:          mPath,
		Output:                "out.json",
		ProjectSlugs:          []string{"gh/acme/web"},
		ContextNames:          []string{"prod-ctx"},
		HostProjectSlug:       "gh/acme/web",
		Branch:                "main",
		EnableTrigger:         true,
		ArtifactRetentionDays: 1,
		EncOpts:               captureEncryptOpts{encrypt: true, generateKey: true, storage: "artifact"},
	}
	var outBuf strings.Builder
	root := MakeCommands()
	p := NewPrompter(strings.NewReader("n\n"), &outBuf)

	_, err := RunCaptureWalkthroughWith(p, root, initial)
	if err == nil || !strings.Contains(err.Error(), "cancelled") {
		t.Errorf("expected cancellation error, got: %v", err)
	}
}

// TestYesNo table-tests the yesNo formatter for both branches.
func TestYesNo(t *testing.T) {
	if got := yesNo(true); got != "yes" {
		t.Errorf("yesNo(true) = %q, want %q", got, "yes")
	}
	if got := yesNo(false); got != "no" {
		t.Errorf("yesNo(false) = %q, want %q", got, "no")
	}
}
