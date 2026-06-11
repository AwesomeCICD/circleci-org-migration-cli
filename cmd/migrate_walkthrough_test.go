package cmd_test

import (
	"strings"
	"testing"

	"github.com/AwesomeCICD/circleci-org-migration-cli/cmd"
	"github.com/AwesomeCICD/circleci-org-migration-cli/settings"
)

// ---------------------------------------------------------------------------
// componentsLabel — exercised via RunMigrateWalkthroughWith
// ---------------------------------------------------------------------------

// TestMigrateCmd_RunnerNamespaceFlagRegistered verifies both runner namespace
// flags exist on migrate.
func TestMigrateCmd_RunnerNamespaceFlagRegistered(t *testing.T) {
	migSub := findMigrateCmd(t)

	for _, name := range []string{"runner-namespace", "dest-runner-namespace"} {
		if migSub.Flags().Lookup(name) == nil {
			t.Errorf("migrate flag --%s not registered", name)
		}
	}
}

// TestMigrateCmd_NoInputFlagRegistered verifies --no-input is registered.
func TestMigrateCmd_NoInputFlagRegistered(t *testing.T) {
	migSub := findMigrateCmd(t)
	if migSub.Flags().Lookup("no-input") == nil {
		t.Error("migrate flag --no-input not registered")
	}
}

// ---------------------------------------------------------------------------
// RunMigrateWalkthroughWith — synthetic I/O tests
// ---------------------------------------------------------------------------

// runWalkthroughWithInput calls RunMigrateWalkthroughWith using a synthetic
// stdin reader built from inputLines.
func runWalkthroughWithInput(t *testing.T, inputLines []string) (
	srcOrg, dstOrg, secretsPath, missing string,
	apply, yes, skipCtx, skipProj, skipOrgSettings, skipExtras bool,
	err error,
) {
	t.Helper()

	input := strings.Join(inputLines, "\n") + "\n"
	r := strings.NewReader(input)

	root := cmd.MakeCommands()
	var outBuf strings.Builder
	root.SetOut(&outBuf)
	root.SetErr(&outBuf)

	p := cmd.NewPrompter(r, &outBuf)

	return cmd.RunMigrateWalkthroughWith(p, root, &settings.Config{}, "", "", false)
}

// TestRunMigrateWalkthroughWith_DryRunAllComponents exercises the walkthrough
// with all components selected and a dry-run choice.
func TestRunMigrateWalkthroughWith_DryRunAllComponents(t *testing.T) {
	// Set tokens so the walkthrough doesn't prompt for them.
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "fake-src-tok")
	t.Setenv("CIRCLECI_DEST_TOKEN", "fake-dst-tok")
	t.Setenv("CIRCLECI_CLI_TOKEN", "")

	// Input lines correspond to each prompt in order:
	// 1. source org slug
	// 2. dest org slug
	// 3. multiselect components (empty = all = default)
	// 4. use bundle? → "n"
	// 5. missing secrets choice → "1" (first option: skip)
	// 6. dry run first? → "y"
	lines := []string{
		"gh/acme",     // source org
		"gh/acme-new", // dest org
		"",            // components: accept default (all)
		"n",           // no secrets bundle
		"1",           // missing-secrets: skip (first choice)
		"y",           // dry run (yes to "perform dry run")
	}

	srcOrg, dstOrg, _, missing, apply, _, _, _, _, _, err := runWalkthroughWithInput(t, lines)
	if err != nil {
		t.Fatalf("walkthrough error: %v", err)
	}
	if srcOrg != "gh/acme" {
		t.Errorf("sourceOrg = %q, want %q", srcOrg, "gh/acme")
	}
	if dstOrg != "gh/acme-new" {
		t.Errorf("destOrg = %q, want %q", dstOrg, "gh/acme-new")
	}
	if apply {
		t.Error("expected apply=false for dry-run choice")
	}
	if missing == "" {
		t.Error("missing-secrets should not be empty")
	}
}

// TestRunMigrateWalkthroughWith_ApplyWithConfirmation exercises the apply
// branch (user selects apply and then confirms).
func TestRunMigrateWalkthroughWith_ApplyWithConfirmation(t *testing.T) {
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "fake-src-tok")
	t.Setenv("CIRCLECI_DEST_TOKEN", "fake-dst-tok")
	t.Setenv("CIRCLECI_CLI_TOKEN", "")

	// "n" to dry-run question → apply=true; then "y" to confirm.
	lines := []string{
		"gh/src", // source org
		"gh/dst", // dest org
		"",       // components: default (all)
		"n",      // no secrets bundle
		"1",      // missing-secrets: skip
		"n",      // do NOT do dry run → apply=true
		"y",      // confirm apply
	}

	_, _, _, _, apply, _, _, _, _, _, err := runWalkthroughWithInput(t, lines)
	if err != nil {
		t.Fatalf("walkthrough error: %v", err)
	}
	if !apply {
		t.Error("expected apply=true when user confirmed apply")
	}
}

// TestRunMigrateWalkthroughWith_ApplyCancelled exercises the case where the
// user declines the apply confirmation.
func TestRunMigrateWalkthroughWith_ApplyCancelled(t *testing.T) {
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "fake-src-tok")
	t.Setenv("CIRCLECI_DEST_TOKEN", "fake-dst-tok")
	t.Setenv("CIRCLECI_CLI_TOKEN", "")

	// "n" to dry-run → apply=true, then "n" to decline confirmation.
	lines := []string{
		"gh/src", // source org
		"gh/dst", // dest org
		"",       // components: default
		"n",      // no secrets bundle
		"1",      // missing-secrets: skip
		"n",      // do NOT do dry run → apply=true
		"n",      // decline confirmation
	}

	_, _, _, _, _, _, _, _, _, _, err := runWalkthroughWithInput(t, lines)
	if err == nil {
		t.Error("expected cancellation error when user declines apply confirmation")
	}
	if !strings.Contains(err.Error(), "cancelled") {
		t.Errorf("expected 'cancelled' in error, got: %v", err)
	}
}

// TestRunMigrateWalkthroughWith_WithSecretsBundle verifies that when the user
// says they have a secrets bundle, the prompter asks for the path.
func TestRunMigrateWalkthroughWith_WithSecretsBundle(t *testing.T) {
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "fake-src-tok")
	t.Setenv("CIRCLECI_DEST_TOKEN", "fake-dst-tok")
	t.Setenv("CIRCLECI_CLI_TOKEN", "")

	lines := []string{
		"gh/src",          // source org
		"gh/dst",          // dest org
		"",                // components: default
		"y",               // yes, have secrets bundle
		"my-secrets.json", // path to bundle
		"1",               // missing-secrets: skip
		"y",               // dry run
	}

	_, _, secretsPath, _, _, _, _, _, _, _, err := runWalkthroughWithInput(t, lines)
	if err != nil {
		t.Fatalf("walkthrough error: %v", err)
	}
	if secretsPath != "my-secrets.json" {
		t.Errorf("secretsPath = %q, want %q", secretsPath, "my-secrets.json")
	}
}

// TestRunMigrateWalkthroughWith_SourceOrgPreset verifies that when sourceOrg
// is already set (via flag), the walkthrough skips prompting for it.
func TestRunMigrateWalkthroughWith_SourceOrgPreset(t *testing.T) {
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "fake-src-tok")
	t.Setenv("CIRCLECI_DEST_TOKEN", "fake-dst-tok")
	t.Setenv("CIRCLECI_CLI_TOKEN", "")

	input := strings.Join([]string{
		"gh/dst", // dest org (source already given)
		"",       // components: default
		"n",      // no secrets bundle
		"1",      // missing-secrets: skip
		"y",      // dry run
	}, "\n") + "\n"

	r := strings.NewReader(input)
	root := cmd.MakeCommands()
	var outBuf strings.Builder
	p := cmd.NewPrompter(r, &outBuf)

	srcOrg, dstOrg, _, _, _, _, _, _, _, _, err := cmd.RunMigrateWalkthroughWith(
		p, root, &settings.Config{}, "gh/preset-src", "", false,
	)
	if err != nil {
		t.Fatalf("walkthrough error: %v", err)
	}
	if srcOrg != "gh/preset-src" {
		t.Errorf("sourceOrg = %q, want %q", srcOrg, "gh/preset-src")
	}
	if dstOrg != "gh/dst" {
		t.Errorf("destOrg = %q, want %q", dstOrg, "gh/dst")
	}
}

// ---------------------------------------------------------------------------
// migrate --no-input non-interactive mode
// ---------------------------------------------------------------------------

// TestMigrateCmd_NoInput_BothOrgsProvided verifies that --no-input with both
// orgs provided advances past the org-validation check.
func TestMigrateCmd_NoInput_BothOrgsProvided(t *testing.T) {
	t.Setenv("CIRCLECI_CLI_TOKEN", "")
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "")
	t.Setenv("CIRCLECI_DEST_TOKEN", "")
	t.Setenv("CIRCLE_TOKEN", "")

	_, _, err := runMigrateCmd(t,
		"--no-input",
		"--source-org", "gh/acme",
		"--dest-org", "gh/acme-new",
	)
	// Should fail on token check, NOT on org-slug check.
	if err == nil {
		t.Fatal("expected error (no token)")
	}
	if strings.Contains(err.Error(), "source-org") || strings.Contains(err.Error(), "dest-org") {
		t.Errorf("should not get org-slug error when both orgs provided; got: %v", err)
	}
	if !strings.Contains(err.Error(), "token") {
		t.Errorf("expected token error, got: %v", err)
	}
}

// TestMigrateCmd_InvalidMissingSecrets_NoInput verifies that passing an
// invalid --missing-secrets value with --no-input errors out properly.
func TestMigrateCmd_InvalidMissingSecrets_NoInput(t *testing.T) {
	t.Setenv("CIRCLECI_CLI_TOKEN", "")
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "")
	t.Setenv("CIRCLECI_DEST_TOKEN", "")
	t.Setenv("CIRCLE_TOKEN", "")

	_, _, err := runMigrateCmd(t,
		"--no-input",
		"--source-org", "gh/acme",
		"--dest-org", "gh/acme-new",
		"--missing-secrets", "invalid",
	)
	if err == nil {
		t.Fatal("expected error for invalid --missing-secrets")
	}
	if !strings.Contains(err.Error(), "missing-secrets") {
		t.Errorf("error should mention 'missing-secrets', got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Issue #76 — migrate walkthrough Step 3a/3b sub-step labels
// ---------------------------------------------------------------------------

// runWalkthroughCaptureOutput calls RunMigrateWalkthroughWith and captures the
// full prompt/output text so we can assert on step labels.
func runWalkthroughCaptureOutput(t *testing.T, inputLines []string) (string, error) {
	t.Helper()

	input := strings.Join(inputLines, "\n") + "\n"
	r := strings.NewReader(input)

	root := cmd.MakeCommands()
	var outBuf strings.Builder
	root.SetOut(&outBuf)
	root.SetErr(&outBuf)

	p := cmd.NewPrompter(r, &outBuf)

	_, _, _, _, _, _, _, _, _, _, err := cmd.RunMigrateWalkthroughWith(p, root, &settings.Config{}, "", "", false)
	return outBuf.String(), err
}

// TestMigrateWalkthrough_Step3SubStepLabels verifies that "Step 3a of 4" and
// "Step 3b of 4" appear in the walkthrough output, replacing the unlabelled
// sub-prompts that existed before issue #76 was fixed.
func TestMigrateWalkthrough_Step3SubStepLabels(t *testing.T) {
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "fake-src-tok")
	t.Setenv("CIRCLECI_DEST_TOKEN", "fake-dst-tok")
	t.Setenv("CIRCLECI_CLI_TOKEN", "")

	lines := []string{
		"gh/acme",     // source org
		"gh/acme-new", // dest org
		"",            // components: all
		"n",           // no secrets bundle
		"1",           // missing-secrets: skip
		"y",           // dry run
	}

	output, err := runWalkthroughCaptureOutput(t, lines)
	if err != nil {
		t.Fatalf("walkthrough error: %v", err)
	}
	if !strings.Contains(output, "Step 3a of 4") {
		t.Errorf("expected 'Step 3a of 4' in migrate walkthrough output; got:\n%s", output)
	}
	if !strings.Contains(output, "Step 3b of 4") {
		t.Errorf("expected 'Step 3b of 4' in migrate walkthrough output; got:\n%s", output)
	}
	if !strings.Contains(output, "Secrets bundle") {
		t.Errorf("expected 'Secrets bundle' label in Step 3a; got:\n%s", output)
	}
	if !strings.Contains(output, "Missing secret values") {
		t.Errorf("expected 'Missing secret values' label in Step 3b; got:\n%s", output)
	}
}

// TestRunMigrateWalkthroughWith_ConfigIsolation is the regression test for the
// rootOptions-global removal (#190). It drives the walkthrough twice in the SAME
// process, each time with its OWN *settings.Config and a token entered at the
// interactive prompt. With the former package-level global, the token captured
// by the first run would leak into the second; with per-invocation config the
// tokens land only in their own config and never cross-contaminate.
func TestRunMigrateWalkthroughWith_ConfigIsolation(t *testing.T) {
	// No token env vars set: the walkthrough must prompt for tokens, and those
	// prompted values are written into the passed-in config.
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "")
	t.Setenv("CIRCLECI_DEST_TOKEN", "")
	t.Setenv("CIRCLECI_CLI_TOKEN", "")
	t.Setenv("CIRCLE_TOKEN", "")

	drive := func(srcTok, dstTok string) *settings.Config {
		t.Helper()
		lines := []string{
			"gh/src", // source org
			"gh/dst", // dest org
			srcTok,   // source token (prompted because env is empty)
			dstTok,   // dest token (prompted because env is empty)
			"",       // components: default (all)
			"n",      // no secrets bundle
			"1",      // missing-secrets: skip
			"y",      // dry run
		}
		r := strings.NewReader(strings.Join(lines, "\n") + "\n")
		var outBuf strings.Builder
		root := cmd.MakeCommands()
		root.SetOut(&outBuf)
		root.SetErr(&outBuf)
		p := cmd.NewPrompter(r, &outBuf)

		cfg := &settings.Config{}
		_, _, _, _, _, _, _, _, _, _, err := cmd.RunMigrateWalkthroughWith(p, root, cfg, "", "", false)
		if err != nil {
			t.Fatalf("walkthrough error: %v", err)
		}
		return cfg
	}

	first := drive("first-src-token", "first-dst-token")
	second := drive("second-src-token", "second-dst-token")

	// The first invocation's config must retain ONLY its own tokens.
	if first.SourceToken != "first-src-token" || first.DestToken != "first-dst-token" {
		t.Errorf("first config tokens = (%q, %q), want (first-src-token, first-dst-token)",
			first.SourceToken, first.DestToken)
	}
	// The second invocation's config must retain ONLY its own tokens — proving
	// the first run did not leak into a shared global.
	if second.SourceToken != "second-src-token" || second.DestToken != "second-dst-token" {
		t.Errorf("second config tokens = (%q, %q), want (second-src-token, second-dst-token)",
			second.SourceToken, second.DestToken)
	}
	// And the two configs are distinct instances (no aliasing).
	if first == second {
		t.Error("expected two distinct *settings.Config instances across invocations")
	}
}
