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
	// 4-7. namespace defaults for orbs and runners (accept with empty)
	// 8. secrets method → "3" (none — structure only)
	// 9. missing secrets choice → "1" (first option: skip)
	// 10. dry run first? → "y"
	lines := []string{
		"gh/acme",     // source org
		"gh/acme-new", // dest org
		"",            // components: accept default (all)
		"",            // source orb namespace: accept default (acme)
		"",            // dest orb namespace: accept default (acme-new)
		"",            // source runner namespace: accept default (acme)
		"",            // dest runner namespace: accept default (acme-new)
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
		"",       // source orb namespace: accept default (src)
		"",       // dest orb namespace: accept default (dst)
		"",       // source runner namespace: accept default (src)
		"",       // dest runner namespace: accept default (dst)
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
		"",       // source orb namespace: accept default (src)
		"",       // dest orb namespace: accept default (dst)
		"",       // source runner namespace: accept default (src)
		"",       // dest runner namespace: accept default (dst)
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
		"",                // source orb namespace: accept default (src)
		"",                // dest orb namespace: accept default (dst)
		"",                // source runner namespace: accept default (src)
		"",                // dest runner namespace: accept default (dst)
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
		"",       // source orb namespace: accept default (preset-src)
		"",       // dest orb namespace: accept default (dst)
		"",       // source runner namespace: accept default (preset-src)
		"",       // dest runner namespace: accept default (dst)
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
		"",            // source orb namespace: accept default (acme)
		"",            // dest orb namespace: accept default (acme-new)
		"",            // source runner namespace: accept default (acme)
		"",            // dest runner namespace: accept default (acme-new)
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
			"",       // source orb namespace: accept default (src)
			"",       // dest orb namespace: accept default (dst)
			"",       // source runner namespace: accept default (src)
			"",       // dest runner namespace: accept default (dst)
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
	// 4-7. namespace defaults for orbs and runners
	// 8. secrets method → "1" (in-pipeline transfer)
	// 9. dest token context name
	// 10. include project vars? → "y"
	// 11. include SSH keys? → "n"
	// 12. remove restrictions? → "y"
	// 13. host project (blank = auto)
	// 14. dry run? → "y"
	lines := []string{
		"gh/acme",           // source org
		"gh/acme-new",       // dest org
		"",                  // components: all
		"",                  // source orb namespace: accept default (acme)
		"",                  // dest orb namespace: accept default (acme-new)
		"",                  // source runner namespace: accept default (acme)
		"",                  // dest runner namespace: accept default (acme-new)
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
		"",                  // source orb namespace: accept default (acme)
		"",                  // dest orb namespace: accept default (acme-new)
		"",                  // source runner namespace: accept default (acme)
		"",                  // dest runner namespace: accept default (acme-new)
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
		"",       // source orb namespace: accept default (src)
		"",       // dest orb namespace: accept default (dst)
		"",       // source runner namespace: accept default (src)
		"",       // dest runner namespace: accept default (dst)
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
		"",            // source orb namespace: accept default (acme)
		"",            // dest orb namespace: accept default (acme-new)
		"",            // source runner namespace: accept default (acme)
		"",            // dest runner namespace: accept default (acme-new)
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
		"",            // source orb namespace: accept default (acme)
		"",            // dest orb namespace: accept default (acme-new)
		"",            // source runner namespace: accept default (acme)
		"",            // dest runner namespace: accept default (acme-new)
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

// ---------------------------------------------------------------------------
// Phase 2 — orbs and runners in guided mode
// ---------------------------------------------------------------------------

// TestRunMigrateWalkthroughWith_OrbsAndRunners_Selected verifies that selecting
// orbs and runners in the guided walkthrough prompts for namespaces and sets the
// result fields and skip flags correctly.
func TestRunMigrateWalkthroughWith_OrbsAndRunners_Selected(t *testing.T) {
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "fake-src-tok")
	t.Setenv("CIRCLECI_DEST_TOKEN", "fake-dst-tok")
	t.Setenv("CIRCLECI_CLI_TOKEN", "")

	// Select only orbs (5) and runners (6) from the component list.
	// Items 5 and 6 correspond to migrateComponents[4] and migrateComponents[5].
	lines := []string{
		"gh/acme",      // source org
		"gh/acme-new",  // dest org
		"5,6",          // components: orbs + runners only
		"acme-ns",      // source orb namespace (custom value)
		"acme-new-ns",  // dest orb namespace (custom value)
		"acme-run",     // source runner namespace (custom value)
		"acme-new-run", // dest runner namespace (custom value)
		"3",            // secrets method: none
		"1",            // missing-secrets: skip
		"y",            // dry run
	}

	res, err := runWalkthroughWithInput(t, lines)
	if err != nil {
		t.Fatalf("walkthrough error: %v", err)
	}

	// Orbs should be selected (not skipped), with namespaces captured.
	if res.SkipOrb {
		t.Error("expected SkipOrb=false (orbs were selected)")
	}
	if res.OrbNamespace != "acme-ns" {
		t.Errorf("OrbNamespace = %q, want %q", res.OrbNamespace, "acme-ns")
	}
	if res.DestOrbNamespace != "acme-new-ns" {
		t.Errorf("DestOrbNamespace = %q, want %q", res.DestOrbNamespace, "acme-new-ns")
	}

	// Runners should be selected (not skipped), with namespaces captured.
	if res.SkipRunner {
		t.Error("expected SkipRunner=false (runners were selected)")
	}
	if res.RunnerNamespace != "acme-run" {
		t.Errorf("RunnerNamespace = %q, want %q", res.RunnerNamespace, "acme-run")
	}
	if res.DestRunnerNamespace != "acme-new-run" {
		t.Errorf("DestRunnerNamespace = %q, want %q", res.DestRunnerNamespace, "acme-new-run")
	}

	// Other components should be skipped (only 5,6 were selected).
	if !res.SkipContexts {
		t.Error("expected SkipContexts=true (not selected)")
	}
	if !res.SkipProjects {
		t.Error("expected SkipProjects=true (not selected)")
	}
	if !res.SkipOrgSettings {
		t.Error("expected SkipOrgSettings=true (not selected)")
	}
	if !res.SkipExtras {
		t.Error("expected SkipExtras=true (not selected)")
	}
}

// TestRunMigrateWalkthroughWith_OrbsAndRunners_Deselected verifies that when
// orbs and runners are deselected (e.g. via a subset not including them),
// SkipOrb and SkipRunner are true and no namespace prompts appear.
func TestRunMigrateWalkthroughWith_OrbsAndRunners_Deselected(t *testing.T) {
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "fake-src-tok")
	t.Setenv("CIRCLECI_DEST_TOKEN", "fake-dst-tok")
	t.Setenv("CIRCLECI_CLI_TOKEN", "")

	// Select only contexts (1) and projects (2) — no orbs or runners.
	lines := []string{
		"gh/acme",     // source org
		"gh/acme-new", // dest org
		"1,2",         // components: contexts + projects only (no orbs/runners)
		"3",           // secrets method: none (no namespace prompts)
		"1",           // missing-secrets: skip
		"y",           // dry run
	}

	res, err := runWalkthroughWithInput(t, lines)
	if err != nil {
		t.Fatalf("walkthrough error: %v", err)
	}

	// Orbs and runners should be skipped.
	if !res.SkipOrb {
		t.Error("expected SkipOrb=true (not selected)")
	}
	if !res.SkipRunner {
		t.Error("expected SkipRunner=true (not selected)")
	}
	if res.OrbNamespace != "" {
		t.Errorf("expected OrbNamespace empty, got %q", res.OrbNamespace)
	}
	if res.DestOrbNamespace != "" {
		t.Errorf("expected DestOrbNamespace empty, got %q", res.DestOrbNamespace)
	}
	if res.RunnerNamespace != "" {
		t.Errorf("expected RunnerNamespace empty, got %q", res.RunnerNamespace)
	}
	if res.DestRunnerNamespace != "" {
		t.Errorf("expected DestRunnerNamespace empty, got %q", res.DestRunnerNamespace)
	}

	// The selected components should NOT be skipped.
	if res.SkipContexts {
		t.Error("expected SkipContexts=false (was selected)")
	}
	if res.SkipProjects {
		t.Error("expected SkipProjects=false (was selected)")
	}
}

// TestRunMigrateWalkthroughWith_OrbsAndRunners_DefaultNamespace verifies that
// when orbs+runners are selected with empty input (accepting defaults), the org
// short name is used as the default namespace for a gh/ org.
func TestRunMigrateWalkthroughWith_OrbsAndRunners_DefaultNamespace(t *testing.T) {
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "fake-src-tok")
	t.Setenv("CIRCLECI_DEST_TOKEN", "fake-dst-tok")
	t.Setenv("CIRCLECI_CLI_TOKEN", "")

	lines := []string{
		"gh/acme",     // source org → short name = acme
		"gh/acme-new", // dest org → short name = acme-new
		"5,6",         // components: orbs + runners
		"",            // source orb namespace: accept default (acme)
		"",            // dest orb namespace: accept default (acme-new)
		"",            // source runner namespace: accept default (acme)
		"",            // dest runner namespace: accept default (acme-new)
		"3",           // secrets method: none
		"1",           // missing-secrets: skip
		"y",           // dry run
	}

	res, err := runWalkthroughWithInput(t, lines)
	if err != nil {
		t.Fatalf("walkthrough error: %v", err)
	}

	if res.OrbNamespace != "acme" {
		t.Errorf("OrbNamespace = %q, want %q (derived from gh/acme)", res.OrbNamespace, "acme")
	}
	if res.DestOrbNamespace != "acme-new" {
		t.Errorf("DestOrbNamespace = %q, want %q (derived from gh/acme-new)", res.DestOrbNamespace, "acme-new")
	}
	if res.RunnerNamespace != "acme" {
		t.Errorf("RunnerNamespace = %q, want %q (derived from gh/acme)", res.RunnerNamespace, "acme")
	}
	if res.DestRunnerNamespace != "acme-new" {
		t.Errorf("DestRunnerNamespace = %q, want %q (derived from gh/acme-new)", res.DestRunnerNamespace, "acme-new")
	}
	if res.SkipOrb {
		t.Error("expected SkipOrb=false when namespaces were accepted")
	}
	if res.SkipRunner {
		t.Error("expected SkipRunner=false when namespaces were accepted")
	}
}

// TestRunMigrateWalkthroughWith_OrbsAndRunners_CircleCIOrg verifies that for a
// circleci/<uuid> org (App/standalone), the default namespace is empty and the
// user is expected to type one; if left blank, orbs/runners are skipped.
func TestRunMigrateWalkthroughWith_OrbsAndRunners_CircleCIOrg(t *testing.T) {
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "fake-src-tok")
	t.Setenv("CIRCLECI_DEST_TOKEN", "fake-dst-tok")
	t.Setenv("CIRCLECI_CLI_TOKEN", "")

	lines := []string{
		"circleci/abc-uuid-123", // source org (App/standalone — no short name)
		"circleci/def-uuid-456", // dest org
		"5,6",                   // components: orbs + runners
		"",                      // source orb namespace: blank → skip
		// No dest orb namespace prompt because source was blank → SkipOrb=true
		"", // source runner namespace: blank → skip
		// No dest runner namespace prompt because source was blank → SkipRunner=true
		"3", // secrets method: none
		"1", // missing-secrets: skip
		"y", // dry run
	}

	res, err := runWalkthroughWithInput(t, lines)
	if err != nil {
		t.Fatalf("walkthrough error: %v", err)
	}

	// With circleci/ orgs the default is empty, and user entered empty → skip.
	if !res.SkipOrb {
		t.Error("expected SkipOrb=true when user left orb namespace blank for circleci/ org")
	}
	if !res.SkipRunner {
		t.Error("expected SkipRunner=true when user left runner namespace blank for circleci/ org")
	}
	if res.OrbNamespace != "" {
		t.Errorf("expected OrbNamespace empty, got %q", res.OrbNamespace)
	}
	if res.RunnerNamespace != "" {
		t.Errorf("expected RunnerNamespace empty, got %q", res.RunnerNamespace)
	}
}

// TestRunMigrateWalkthroughWith_OrbsAndRunners_ApplySummary verifies that when
// apply mode is chosen with orbs and runners selected, the apply summary
// includes the namespace lines.
func TestRunMigrateWalkthroughWith_OrbsAndRunners_ApplySummary(t *testing.T) {
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "fake-src-tok")
	t.Setenv("CIRCLECI_DEST_TOKEN", "fake-dst-tok")
	t.Setenv("CIRCLECI_CLI_TOKEN", "")

	lines := []string{
		"gh/acme",     // source org
		"gh/acme-new", // dest org
		"5,6",         // components: orbs + runners
		"acme",        // source orb namespace
		"acme-new",    // dest orb namespace
		"acme",        // source runner namespace
		"acme-new",    // dest runner namespace
		"3",           // secrets method: none
		"1",           // missing-secrets: skip
		"n",           // do NOT dry run → apply=true
		"y",           // confirm apply
	}

	output, err := runWalkthroughCaptureOutput(t, lines)
	if err != nil {
		t.Fatalf("walkthrough error: %v", err)
	}

	if !strings.Contains(output, "Orbs:") {
		t.Errorf("expected 'Orbs:' line in apply summary; got:\n%s", output)
	}
	if !strings.Contains(output, "acme → acme-new") {
		t.Errorf("expected 'acme → acme-new' namespace mapping in apply summary; got:\n%s", output)
	}
	if !strings.Contains(output, "Runners:") {
		t.Errorf("expected 'Runners:' line in apply summary; got:\n%s", output)
	}
}
