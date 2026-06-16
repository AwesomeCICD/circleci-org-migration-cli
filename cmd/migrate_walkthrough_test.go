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
func runWalkthroughWithInput(t *testing.T, inputLines []string) (cmd.MigrateWalkthroughResult, error) {
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
	// 4. secrets method → "3" (none — structure only)
	// 5. missing secrets choice → "1" (first option: skip)
	// 6. dry run first? → "y"
	lines := []string{
		"gh/acme",     // source org
		"gh/acme-new", // dest org
		"",            // components: accept default (all)
		"3",           // secrets method: none
		"1",           // missing-secrets: skip (first choice)
		"y",           // dry run (yes to "perform dry run")
	}

	res, err := runWalkthroughWithInput(t, lines)
	if err != nil {
		t.Fatalf("walkthrough error: %v", err)
	}
	if res.SourceOrg != "gh/acme" {
		t.Errorf("sourceOrg = %q, want %q", res.SourceOrg, "gh/acme")
	}
	if res.DestOrg != "gh/acme-new" {
		t.Errorf("destOrg = %q, want %q", res.DestOrg, "gh/acme-new")
	}
	if res.Apply {
		t.Error("expected apply=false for dry-run choice")
	}
	if res.Missing == "" {
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
		"3",      // secrets method: none
		"1",      // missing-secrets: skip
		"n",      // do NOT do dry run → apply=true
		"y",      // confirm apply
	}

	res, err := runWalkthroughWithInput(t, lines)
	if err != nil {
		t.Fatalf("walkthrough error: %v", err)
	}
	if !res.Apply {
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
		"3",      // secrets method: none
		"1",      // missing-secrets: skip
		"n",      // do NOT do dry run → apply=true
		"n",      // decline confirmation
	}

	_, err := runWalkthroughWithInput(t, lines)
	if err == nil {
		t.Error("expected cancellation error when user declines apply confirmation")
	}
	if !strings.Contains(err.Error(), "cancelled") {
		t.Errorf("expected 'cancelled' in error, got: %v", err)
	}
}

// TestRunMigrateWalkthroughWith_WithSecretsBundle verifies that when the user
// chooses the captured-bundle path, the prompter asks for the path.
func TestRunMigrateWalkthroughWith_WithSecretsBundle(t *testing.T) {
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "fake-src-tok")
	t.Setenv("CIRCLECI_DEST_TOKEN", "fake-dst-tok")
	t.Setenv("CIRCLECI_CLI_TOKEN", "")

	lines := []string{
		"gh/src",          // source org
		"gh/dst",          // dest org
		"",                // components: default
		"2",               // secrets method: bundle
		"my-secrets.json", // path to bundle
		"1",               // missing-secrets: skip
		"y",               // dry run
	}

	res, err := runWalkthroughWithInput(t, lines)
	if err != nil {
		t.Fatalf("walkthrough error: %v", err)
	}
	if res.SecretsPath != "my-secrets.json" {
		t.Errorf("secretsPath = %q, want %q", res.SecretsPath, "my-secrets.json")
	}
	if res.TransferSecrets {
		t.Error("expected TransferSecrets=false for bundle path")
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
		"3",      // secrets method: none
		"1",      // missing-secrets: skip
		"y",      // dry run
	}, "\n") + "\n"

	r := strings.NewReader(input)
	root := cmd.MakeCommands()
	var outBuf strings.Builder
	p := cmd.NewPrompter(r, &outBuf)

	res, err := cmd.RunMigrateWalkthroughWith(
		p, root, &settings.Config{}, "gh/preset-src", "", false,
	)
	if err != nil {
		t.Fatalf("walkthrough error: %v", err)
	}
	if res.SourceOrg != "gh/preset-src" {
		t.Errorf("sourceOrg = %q, want %q", res.SourceOrg, "gh/preset-src")
	}
	if res.DestOrg != "gh/dst" {
		t.Errorf("destOrg = %q, want %q", res.DestOrg, "gh/dst")
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

	_, err := cmd.RunMigrateWalkthroughWith(p, root, &settings.Config{}, "", "", false)
	return outBuf.String(), err
}

// TestMigrateWalkthrough_Step3SubStepLabels verifies that "Step 3a of 4" and
// "Step 3b of 4" appear in the walkthrough output when a non-in-pipeline path
// is chosen, preserving the original sub-step labelling from issue #76.
func TestMigrateWalkthrough_Step3SubStepLabels(t *testing.T) {
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "fake-src-tok")
	t.Setenv("CIRCLECI_DEST_TOKEN", "fake-dst-tok")
	t.Setenv("CIRCLECI_CLI_TOKEN", "")

	lines := []string{
		"gh/acme",     // source org
		"gh/acme-new", // dest org
		"",            // components: all
		"3",           // secrets method: none (Step 3a choice)
		"1",           // missing-secrets: skip (Step 3b)
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
	if !strings.Contains(output, "Secret values") {
		t.Errorf("expected 'Secret values' label in Step 3a; got:\n%s", output)
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
			"3",      // secrets method: none
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
		_, err := cmd.RunMigrateWalkthroughWith(p, root, cfg, "", "", false)
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

// ---------------------------------------------------------------------------
// Step 3a — new secrets-method choice (in-pipeline / bundle / none)
// ---------------------------------------------------------------------------

// TestRunMigrateWalkthroughWith_InPipelineTransfer exercises the recommended
// in-pipeline transfer path introduced by the new Step 3a choice.  It asserts
// that TransferSecrets is set, DestTokenContext is captured, the optional flags
// are set per user answers, and SecretsPath is empty (no bundle written).
func TestRunMigrateWalkthroughWith_InPipelineTransfer(t *testing.T) {
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "fake-src-tok")
	t.Setenv("CIRCLECI_DEST_TOKEN", "fake-dst-tok")
	t.Setenv("CIRCLECI_CLI_TOKEN", "")

	// Input lines for the in-pipeline path:
	// 1. source org
	// 2. dest org
	// 3. components: default (all)
	// 4. secrets method → "1" (in-pipeline transfer)
	// 5. dest token context name
	// 6. include project vars? → "y"
	// 7. include SSH keys? → "n"
	// 8. remove restrictions? → "y"
	// 9. host project (blank = auto)
	// 10. dry run? → "y"
	lines := []string{
		"gh/acme",           // source org
		"gh/acme-new",       // dest org
		"",                  // components: all
		"1",                 // secrets method: in-pipeline (RECOMMENDED)
		"migration-secrets", // dest token context name
		"y",                 // include project vars
		"n",                 // do NOT include SSH keys
		"y",                 // remove restrictions
		"",                  // host project: auto-pick
		"y",                 // dry run
	}

	res, err := runWalkthroughWithInput(t, lines)
	if err != nil {
		t.Fatalf("walkthrough error: %v", err)
	}

	if !res.TransferSecrets {
		t.Error("expected TransferSecrets=true for in-pipeline path")
	}
	if res.DestTokenContext != "migration-secrets" {
		t.Errorf("DestTokenContext = %q, want %q", res.DestTokenContext, "migration-secrets")
	}
	if !res.IncludeProjectVars {
		t.Error("expected IncludeProjectVars=true")
	}
	if res.IncludeSSHKeys {
		t.Error("expected IncludeSSHKeys=false (user answered 'n')")
	}
	if !res.RemoveRestrictions {
		t.Error("expected RemoveRestrictions=true")
	}
	if res.HostProject != "" {
		t.Errorf("expected HostProject empty for auto-pick, got %q", res.HostProject)
	}
	if res.SecretsPath != "" {
		t.Errorf("expected SecretsPath empty for in-pipeline path, got %q", res.SecretsPath)
	}
	if res.Apply {
		t.Error("expected apply=false (dry run chosen)")
	}
}

// TestRunMigrateWalkthroughWith_InPipelineTransfer_WithHostProject exercises
// the in-pipeline path when the user supplies a specific host project slug.
func TestRunMigrateWalkthroughWith_InPipelineTransfer_WithHostProject(t *testing.T) {
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "fake-src-tok")
	t.Setenv("CIRCLECI_DEST_TOKEN", "fake-dst-tok")
	t.Setenv("CIRCLECI_CLI_TOKEN", "")

	lines := []string{
		"gh/acme",           // source org
		"gh/acme-new",       // dest org
		"",                  // components: all
		"1",                 // secrets method: in-pipeline
		"migration-secrets", // dest token context name
		"y",                 // include project vars
		"y",                 // include SSH keys
		"n",                 // do NOT remove restrictions
		"gh/acme/web",       // host project
		"y",                 // dry run
	}

	res, err := runWalkthroughWithInput(t, lines)
	if err != nil {
		t.Fatalf("walkthrough error: %v", err)
	}
	if res.HostProject != "gh/acme/web" {
		t.Errorf("HostProject = %q, want %q", res.HostProject, "gh/acme/web")
	}
	if !res.IncludeSSHKeys {
		t.Error("expected IncludeSSHKeys=true")
	}
	if res.RemoveRestrictions {
		t.Error("expected RemoveRestrictions=false (user answered 'n')")
	}
}

// TestRunMigrateWalkthroughWith_NoneMethod verifies the "none" path: no bundle,
// no transfer; the missing-secrets step still runs.
func TestRunMigrateWalkthroughWith_NoneMethod(t *testing.T) {
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "fake-src-tok")
	t.Setenv("CIRCLECI_DEST_TOKEN", "fake-dst-tok")
	t.Setenv("CIRCLECI_CLI_TOKEN", "")

	lines := []string{
		"gh/src", // source org
		"gh/dst", // dest org
		"",       // components: default
		"3",      // secrets method: none
		"1",      // missing-secrets: skip
		"y",      // dry run
	}

	res, err := runWalkthroughWithInput(t, lines)
	if err != nil {
		t.Fatalf("walkthrough error: %v", err)
	}
	if res.TransferSecrets {
		t.Error("expected TransferSecrets=false for none path")
	}
	if res.SecretsPath != "" {
		t.Errorf("expected SecretsPath empty for none path, got %q", res.SecretsPath)
	}
	if res.Missing == "" {
		t.Error("expected Missing to be set for none path")
	}
}

// TestMigrateWalkthrough_Step3aLeadsWithInPipeline verifies that the Step 3a
// output text presents the in-pipeline transfer as the first / recommended
// option, and that "Step 3a of 4" still appears in the output.
func TestMigrateWalkthrough_Step3aLeadsWithInPipeline(t *testing.T) {
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "fake-src-tok")
	t.Setenv("CIRCLECI_DEST_TOKEN", "fake-dst-tok")
	t.Setenv("CIRCLECI_CLI_TOKEN", "")

	// Drive the none path (fewest follow-up prompts) so the test is simple.
	lines := []string{
		"gh/acme",     // source org
		"gh/acme-new", // dest org
		"",            // components: all
		"3",           // secrets method: none
		"1",           // missing-secrets: skip
		"y",           // dry run
	}

	output, err := runWalkthroughCaptureOutput(t, lines)
	if err != nil {
		t.Fatalf("walkthrough error: %v", err)
	}

	// Step 3a header must still be present.
	if !strings.Contains(output, "Step 3a of 4") {
		t.Errorf("expected 'Step 3a of 4' in output; got:\n%s", output)
	}

	// The recommended in-pipeline option must appear first in the list.
	inPipelineIdx := strings.Index(output, "in-pipeline transfer (RECOMMENDED)")
	bundleIdx := strings.Index(output, "captured secrets bundle (advanced)")
	noneIdx := strings.Index(output, "none")
	if inPipelineIdx < 0 {
		t.Error("expected 'in-pipeline transfer (RECOMMENDED)' in Step 3a output")
	}
	if bundleIdx < 0 {
		t.Error("expected 'captured secrets bundle (advanced)' in Step 3a output")
	}
	if noneIdx < 0 {
		t.Error("expected 'none' option in Step 3a output")
	}
	if inPipelineIdx > bundleIdx {
		t.Error("expected in-pipeline option to appear before bundle option in Step 3a")
	}
}

// TestMigrateWalkthrough_InPipelineApplySummary exercises the apply
// confirmation summary for the in-pipeline transfer path, verifying that the
// "Secrets: in-pipeline transfer via context" line appears.
func TestMigrateWalkthrough_InPipelineApplySummary(t *testing.T) {
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "fake-src-tok")
	t.Setenv("CIRCLECI_DEST_TOKEN", "fake-dst-tok")
	t.Setenv("CIRCLECI_CLI_TOKEN", "")

	lines := []string{
		"gh/acme",     // source org
		"gh/acme-new", // dest org
		"",            // components: all
		"1",           // secrets method: in-pipeline
		"my-ctx",      // dest token context
		"y",           // include project vars
		"y",           // include SSH keys
		"y",           // remove restrictions
		"",            // host project: auto
		"n",           // do NOT dry run → apply=true
		"y",           // confirm apply
	}

	output, err := runWalkthroughCaptureOutput(t, lines)
	if err != nil {
		t.Fatalf("walkthrough error: %v", err)
	}

	if !strings.Contains(output, `in-pipeline transfer via context "my-ctx"`) {
		t.Errorf("expected apply summary to mention in-pipeline transfer via context; got:\n%s", output)
	}
}
