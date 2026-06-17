package cmd_test

import (
	"strings"
	"testing"

	"github.com/AwesomeCICD/circleci-org-migration-cli/cmd"
	"github.com/AwesomeCICD/circleci-org-migration-cli/settings"
	"github.com/spf13/cobra"
)

// ---------------------------------------------------------------------------
// Interactive walkthrough — helpers
// ---------------------------------------------------------------------------

// skipResults captures which migration components were skipped after a
// walkthrough run.
type skipResults struct {
	contexts    bool
	projects    bool
	orgSettings bool
	extras      bool
	orb         bool
	runner      bool
}

// runWalkthrough drives the interactive guided walkthrough with scripted input
// and returns the skip flags, apply flag, and any error.  A fresh per-invocation
// settings.Config is passed in; token env vars are picked up by the config's
// *TokenOrDefault accessors, so t.Setenv changes are honoured.
func runWalkthrough(t *testing.T, input string) (skips skipResults, outApply bool, err error) {
	t.Helper()

	// Build a fresh command tree for an isolated invocation.
	root := cmd.MakeCommands()
	var migCmd *cobra.Command
	for _, sub := range root.Commands() {
		if strings.HasPrefix(sub.Use, "migrate") {
			migCmd = sub
			break
		}
	}
	if migCmd == nil {
		t.Fatal("migrate subcommand not found")
	}

	r := strings.NewReader(input)
	var promptBuf strings.Builder
	p := cmd.NewPrompter(r, &promptBuf)

	res, walkErr := cmd.RunMigrateWalkthroughWith(p, migCmd, &settings.Config{}, "", "", false)

	return skipResults{
		contexts:    res.SkipContexts,
		projects:    res.SkipProjects,
		orgSettings: res.SkipOrgSettings,
		extras:      res.SkipExtras,
		orb:         res.SkipOrb,
		runner:      res.SkipRunner,
	}, res.Apply, walkErr
}

// ---------------------------------------------------------------------------
// Prompt behaviour — askRequired (re-prompt on empty)
// ---------------------------------------------------------------------------

// TestPrompt_AskRequired_RepromptOnEmpty verifies that the walkthrough
// re-prompts when the user enters an empty source-org slug.
func TestPrompt_AskRequired_RepromptOnEmpty(t *testing.T) {
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "pre-src")
	t.Setenv("CIRCLECI_DEST_TOKEN", "pre-dst")

	// First source-org line is empty → re-prompt; second provides a valid slug.
	// "3" → secrets method: none; "1" → missing-secrets: skip; "y" → dry run.
	// Four empty lines accept namespace defaults for orbs and runners.
	input := "\ngh/acme\ngh/acme-new\nall\n\n\n\n\n3\n1\ny\n"
	_, _, err := runWalkthrough(t, input)
	if err != nil {
		t.Fatalf("unexpected error after re-prompt: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Prompt behaviour — walkthrough always returns Apply=false (Feature A)
// ---------------------------------------------------------------------------

// TestPrompt_Walkthrough_AlwaysReturnsDryRun verifies that the guided
// walkthrough always returns Apply=false regardless of user input, because
// the dry-run → confirm → apply loop was moved to RunE (Feature A).
// RunE runs the dry-run, prints the summary, and then asks "Apply now?"
// outside of the walkthrough.
func TestPrompt_Walkthrough_AlwaysReturnsDryRun(t *testing.T) {
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "pre-src")
	t.Setenv("CIRCLECI_DEST_TOKEN", "pre-dst")

	// "3" → secrets method: none; "1" → missing-secrets: skip.
	// Four empty lines accept namespace defaults for orbs and runners.
	input := "gh/acme\ngh/acme-new\nall\n\n\n\n\n3\n1\n"
	_, outApply, err := runWalkthrough(t, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outApply {
		t.Error("expected apply=false: the walkthrough never sets Apply=true; " +
			"the apply confirm happens in RunE after the dry-run summary")
	}
}

// ---------------------------------------------------------------------------
// Prompt behaviour — component selection (askMultiSelect)
// ---------------------------------------------------------------------------

// TestPrompt_MultiSelect_All verifies that entering "all" selects every
// migration component (no components skipped).
func TestPrompt_MultiSelect_All(t *testing.T) {
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "pre-src")
	t.Setenv("CIRCLECI_DEST_TOKEN", "pre-dst")

	// Four empty lines accept namespace defaults for orbs and runners.
	input := "gh/acme\ngh/acme-new\nall\n\n\n\n\n3\n1\ny\n"
	skips, _, err := runWalkthrough(t, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if skips.contexts || skips.projects || skips.orgSettings || skips.extras || skips.orb || skips.runner {
		t.Errorf("expected no skips with 'all', got: contexts=%v projects=%v orgSettings=%v extras=%v orb=%v runner=%v",
			skips.contexts, skips.projects, skips.orgSettings, skips.extras, skips.orb, skips.runner)
	}
}

// TestPrompt_MultiSelect_EmptyLine selects all components via the empty-line
// default.
func TestPrompt_MultiSelect_EmptyLine(t *testing.T) {
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "pre-src")
	t.Setenv("CIRCLECI_DEST_TOKEN", "pre-dst")

	// Empty line on multi-select prompt → "all" default.
	// Four more empty lines accept namespace defaults for orbs and runners.
	input := "gh/acme\ngh/acme-new\n\n\n\n\n\n3\n1\ny\n"
	skips, _, err := runWalkthrough(t, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if skips.contexts || skips.projects || skips.orgSettings || skips.extras || skips.orb || skips.runner {
		t.Errorf("expected all selected with empty line, got: %+v", skips)
	}
}

// TestPrompt_MultiSelect_None verifies that "none" skips all components.
func TestPrompt_MultiSelect_None(t *testing.T) {
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "pre-src")
	t.Setenv("CIRCLECI_DEST_TOKEN", "pre-dst")

	input := "gh/acme\ngh/acme-new\nnone\n3\n1\ny\n"
	skips, _, err := runWalkthrough(t, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !skips.contexts || !skips.projects || !skips.orgSettings || !skips.extras || !skips.orb || !skips.runner {
		t.Errorf("expected all skips with 'none', got: contexts=%v projects=%v orgSettings=%v extras=%v orb=%v runner=%v",
			skips.contexts, skips.projects, skips.orgSettings, skips.extras, skips.orb, skips.runner)
	}
}

// TestPrompt_MultiSelect_Subset selects only contexts (1) and projects (2).
func TestPrompt_MultiSelect_Subset(t *testing.T) {
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "pre-src")
	t.Setenv("CIRCLECI_DEST_TOKEN", "pre-dst")

	// Items 1,2 = contexts + projects.
	input := "gh/acme\ngh/acme-new\n1,2\n3\n1\ny\n"
	skips, _, err := runWalkthrough(t, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if skips.contexts {
		t.Error("contexts should NOT be skipped (item 1 was selected)")
	}
	if skips.projects {
		t.Error("projects should NOT be skipped (item 2 was selected)")
	}
	if !skips.orgSettings {
		t.Error("org settings should be skipped (not selected)")
	}
	if !skips.extras {
		t.Error("extras should be skipped (not selected)")
	}
}

// TestPrompt_MultiSelect_InvalidThenValid verifies that an out-of-range number
// is rejected and the user is re-prompted, then "all" is accepted.
func TestPrompt_MultiSelect_InvalidThenValid(t *testing.T) {
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "pre-src")
	t.Setenv("CIRCLECI_DEST_TOKEN", "pre-dst")

	// "99" is out of range → reprompt; "all" is accepted on the second attempt.
	// Four empty lines accept namespace defaults for orbs and runners.
	input := "gh/acme\ngh/acme-new\n99\nall\n\n\n\n\n3\n1\ny\n"
	skips, _, err := runWalkthrough(t, input)
	if err != nil {
		t.Fatalf("unexpected error after re-prompt: %v", err)
	}
	if skips.contexts || skips.projects || skips.orgSettings || skips.extras || skips.orb || skips.runner {
		t.Errorf("expected no skips after 'all'; got: %+v", skips)
	}
}

// ---------------------------------------------------------------------------
// Prompt behaviour — missing-secrets choice
// ---------------------------------------------------------------------------

// TestPrompt_MissingSecrets_Placeholder verifies that choosing placeholder
// (item 2 in the missing-secrets step) is accepted without error.
func TestPrompt_MissingSecrets_Placeholder(t *testing.T) {
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "pre-src")
	t.Setenv("CIRCLECI_DEST_TOKEN", "pre-dst")

	// "3" → secrets method: none; "2" → missing-secrets: placeholder; "y" → dry run.
	// Four empty lines accept namespace defaults for orbs and runners.
	input := "gh/acme\ngh/acme-new\nall\n\n\n\n\n3\n2\ny\n"
	_, _, err := runWalkthrough(t, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Prompt behaviour — secrets bundle
// ---------------------------------------------------------------------------

// TestPrompt_SecretsBundle_Provided verifies that choosing the bundle method
// prompts for a path and accepts the provided value.
func TestPrompt_SecretsBundle_Provided(t *testing.T) {
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "pre-src")
	t.Setenv("CIRCLECI_DEST_TOKEN", "pre-dst")

	// "2" → secrets method: bundle; "my-secrets.json" → path;
	// "1" → missing-secrets: skip; "y" → dry run.
	// Four empty lines accept namespace defaults for orbs and runners.
	input := "gh/acme\ngh/acme-new\nall\n\n\n\n\n2\nmy-secrets.json\n1\ny\n"
	_, _, err := runWalkthrough(t, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Prompt behaviour — tokens already set in env
// ---------------------------------------------------------------------------

// TestPrompt_TokensAlreadySet_SkipsTokenPrompts verifies that when tokens are
// already configured via environment variables the walkthrough does not block
// waiting for token input.
func TestPrompt_TokensAlreadySet_SkipsTokenPrompts(t *testing.T) {
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "pre-set-src")
	t.Setenv("CIRCLECI_DEST_TOKEN", "pre-set-dst")

	// No token lines in input — the walkthrough must not block on them.
	// Four empty lines accept namespace defaults for orbs and runners.
	input := "gh/acme\ngh/acme-new\nall\n\n\n\n\n3\n1\ny\n"
	_, _, err := runWalkthrough(t, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Prompt behaviour — token prompts (askSecret / askSecretRequired)
// ---------------------------------------------------------------------------

// TestPrompt_TokenPrompts_WhenNotSetInEnv verifies that the walkthrough
// prompts for both source and destination tokens when they are not available
// via flags or environment variables.
func TestPrompt_TokenPrompts_WhenNotSetInEnv(t *testing.T) {
	t.Setenv("CIRCLECI_CLI_TOKEN", "")
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "")
	t.Setenv("CIRCLECI_DEST_TOKEN", "")
	t.Setenv("CIRCLE_TOKEN", "")

	// Provide token values inline in the scripted input.
	// Four empty lines accept namespace defaults for orbs and runners.
	input := "gh/acme\ngh/acme-new\nmy-src-token\nmy-dst-token\nall\n\n\n\n\n3\n1\ny\n"
	_, _, err := runWalkthrough(t, input)
	if err != nil {
		t.Fatalf("unexpected error when providing tokens interactively: %v", err)
	}
}

// TestPrompt_TokenPrompts_EmptyThenValid verifies that an empty secret token
// is rejected and the user is re-prompted until a non-empty value is entered.
func TestPrompt_TokenPrompts_EmptyThenValid(t *testing.T) {
	t.Setenv("CIRCLECI_CLI_TOKEN", "")
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "")
	t.Setenv("CIRCLECI_DEST_TOKEN", "")
	t.Setenv("CIRCLE_TOKEN", "")

	// Empty source token → re-prompt; second entry is valid.
	// Four empty lines accept namespace defaults for orbs and runners.
	input := "gh/acme\ngh/acme-new\n\nmy-src-token\nmy-dst-token\nall\n\n\n\n\n3\n1\ny\n"
	_, _, err := runWalkthrough(t, input)
	if err != nil {
		t.Fatalf("unexpected error after re-prompt for token: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Non-TTY safety (--no-input flag)
// ---------------------------------------------------------------------------

// TestMigrateCmd_NoInputFlag_Registered verifies that --no-input is a known
// flag on the migrate subcommand.
func TestMigrateCmd_NoInputFlag_Registered(t *testing.T) {
	migSub := findMigrateCmd(t)
	if migSub.Flags().Lookup("no-input") == nil {
		t.Error("migrate flag --no-input not registered")
	}
}

// TestMigrateCmd_NoInputFlag_MissingOrg_Errors verifies that --no-input with
// missing --source-org errors immediately without prompting.
func TestMigrateCmd_NoInputFlag_MissingOrg_Errors(t *testing.T) {
	t.Setenv("CIRCLECI_CLI_TOKEN", "")
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "")
	t.Setenv("CIRCLECI_DEST_TOKEN", "")
	t.Setenv("CIRCLE_TOKEN", "")

	_, _, err := runMigrateCmd(t, "--no-input")
	if err == nil {
		t.Fatal("expected error with --no-input and missing --source-org")
	}
	if !strings.Contains(err.Error(), "source-org") {
		t.Errorf("error %q does not mention 'source-org'", err.Error())
	}
}

// TestMigrateCmd_NoInputFlag_MissingDestOrg_Errors verifies that --no-input
// with --source-org provided but --dest-org missing errors clearly.
func TestMigrateCmd_NoInputFlag_MissingDestOrg_Errors(t *testing.T) {
	t.Setenv("CIRCLECI_CLI_TOKEN", "")
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "")
	t.Setenv("CIRCLECI_DEST_TOKEN", "")
	t.Setenv("CIRCLE_TOKEN", "")

	_, _, err := runMigrateCmd(t, "--no-input", "--source-org", "gh/acme")
	if err == nil {
		t.Fatal("expected error with --no-input and missing --dest-org")
	}
	if !strings.Contains(err.Error(), "dest-org") {
		t.Errorf("error %q does not mention 'dest-org'", err.Error())
	}
}

// ---------------------------------------------------------------------------
// Issue #157 — separator/header before each question
// ---------------------------------------------------------------------------

// TestPromptSeparator_BlankLineBeforeMultiSelect verifies that a blank line
// appears in the output immediately before the multi-select option list.  This
// is the must-fix for issue #157: consecutive questions must NOT stack.
func TestPromptSeparator_BlankLineBeforeMultiSelect(t *testing.T) {
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "pre-src")
	t.Setenv("CIRCLECI_DEST_TOKEN", "pre-dst")

	lines := []string{
		"gh/acme",     // source org
		"gh/acme-new", // dest org
		"",            // components: all
		"",            // source orb namespace: accept default
		"",            // dest orb namespace: accept default
		"",            // source runner namespace: accept default
		"",            // dest runner namespace: accept default
		"3",           // secrets method: none
		"1",           // missing-secrets: skip
		"y",           // dry run
	}

	output, err := runWalkthroughCaptureOutput(t, lines)
	if err != nil {
		t.Fatalf("walkthrough error: %v", err)
	}

	// The output must contain at least one blank line followed by the
	// multi-select label.  A blank line is "\n\n" (two consecutive newlines).
	if !strings.Contains(output, "\n\n") {
		t.Errorf("expected at least one blank separator line in output; got:\n%s", output)
	}
}

// TestPromptSeparator_StepHeaderContainsStepNumber verifies that styled step
// headers (e.g. "Step 1 of 4") appear in the migrate walkthrough output so
// that steps are clearly delimited.
func TestPromptSeparator_StepHeaderContainsStepNumber(t *testing.T) {
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "pre-src")
	t.Setenv("CIRCLECI_DEST_TOKEN", "pre-dst")

	lines := []string{
		"gh/acme",     // source org
		"gh/acme-new", // dest org
		"",            // components: all
		"",            // source orb namespace: accept default
		"",            // dest orb namespace: accept default
		"",            // source runner namespace: accept default
		"",            // dest runner namespace: accept default
		"3",           // secrets method: none
		"1",           // missing-secrets: skip
		"y",           // dry run
	}

	output, err := runWalkthroughCaptureOutput(t, lines)
	if err != nil {
		t.Fatalf("walkthrough error: %v", err)
	}

	// The walkthrough now has 3 top-level steps (Step 4 was the "dry run or
	// apply" question, which is now handled by RunE after the dry-run pass).
	for _, want := range []string{"Step 1 of 3", "Step 2 of 3", "Step 3 of 3"} {
		if !strings.Contains(output, want) {
			t.Errorf("expected %q in migrate walkthrough output; got:\n%s", want, output)
		}
	}
	if strings.Contains(output, "Step 4 of") {
		t.Errorf("walkthrough should not contain 'Step 4 of' (dry-run step moved to RunE); got:\n%s", output)
	}
}

// TestPromptSeparator_BlankLineBeforeBoolQuestion verifies that a blank line
// is emitted before yes/no (bool) questions, providing visual separation.
// The in-pipeline secrets path has several bool prompts (include-project-vars,
// include-ssh-keys, remove-restrictions) that exercise this property.
func TestPromptSeparator_BlankLineBeforeBoolQuestion(t *testing.T) {
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "pre-src")
	t.Setenv("CIRCLECI_DEST_TOKEN", "pre-dst")

	// Drive the in-pipeline path which contains bool prompts inside the walkthrough.
	lines := []string{
		"gh/acme",           // source org
		"gh/acme-new",       // dest org
		"",                  // components: all
		"",                  // source orb namespace: accept default
		"",                  // dest orb namespace: accept default
		"",                  // source runner namespace: accept default
		"",                  // dest runner namespace: accept default
		"1",                 // secrets method: in-pipeline (has bool prompts)
		"migration-secrets", // dest token context name
		"y",                 // include project vars [Y/n]
		"n",                 // include SSH keys [Y/n]
		"y",                 // remove restrictions [Y/n]
		"",                  // host project: auto
	}

	output, err := runWalkthroughCaptureOutput(t, lines)
	if err != nil {
		t.Fatalf("walkthrough error: %v", err)
	}

	// The include-project-vars bool prompt contains "[Y/n]"; it must be preceded
	// by a blank line.  The output must contain at least one blank line overall.
	if !strings.Contains(output, "\n\n") {
		t.Errorf("expected at least one blank separator line in output; got:\n%s", output)
	}
	// The in-pipeline bool prompt "[Y/n]" must appear in the output.
	idx := strings.Index(output, "[Y/n]")
	if idx < 0 {
		t.Fatalf("expected '[Y/n]' in output for in-pipeline bool prompts; got:\n%s", output)
	}
	before := output[:idx]
	if !strings.Contains(before, "\n\n") {
		t.Errorf("expected blank line before [Y/n] prompt; output before:\n%s", before)
	}
}

// TestPromptOptionList_NumbersHaveIndentation verifies that the numbered option
// list produced by askChoice / askMultiSelect is indented (starts with spaces),
// so options are visually grouped and separated from surrounding text.
func TestPromptOptionList_NumbersHaveIndentation(t *testing.T) {
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "pre-src")
	t.Setenv("CIRCLECI_DEST_TOKEN", "pre-dst")

	lines := []string{
		"gh/acme",     // source org
		"gh/acme-new", // dest org
		"",            // components: all (multi-select list printed here)
		"",            // source orb namespace: accept default
		"",            // dest orb namespace: accept default
		"",            // source runner namespace: accept default
		"",            // dest runner namespace: accept default
		"3",           // secrets method: none
		"1",           // missing-secrets: skip (choice list printed here)
		"y",           // dry run
	}

	output, err := runWalkthroughCaptureOutput(t, lines)
	if err != nil {
		t.Fatalf("walkthrough error: %v", err)
	}

	// The option list items must be indented (start with whitespace + number).
	// Check for "  1)" or "  1)" — at least two leading spaces before the number.
	if !strings.Contains(output, "  1)") {
		t.Errorf("expected indented option '  1)' in output; got:\n%s", output)
	}
}

// ---------------------------------------------------------------------------
// Issue #158 — completion command is hidden
// ---------------------------------------------------------------------------

// TestCompletion_CommandIsHiddenOrAbsent verifies that the auto-generated
// completion command is not advertised in --help.  When HiddenDefaultCmd=true,
// cobra either omits the command entirely or marks it hidden; either way it
// must not be returned by IsAvailableCommand.
func TestCompletion_CommandIsHiddenOrAbsent(t *testing.T) {
	root := cmd.MakeCommands()

	for _, sub := range root.Commands() {
		if sub.Name() == "completion" {
			// Found: it must NOT be available (i.e. must be hidden).
			if sub.IsAvailableCommand() {
				t.Error("completion command should not be available (HiddenDefaultCmd=true)")
			}
			return
		}
	}
	// Not found at all — that is also acceptable (cobra suppressed it entirely).
}

// TestCompletion_NotInHelpOutput verifies that the completion command does not
// appear in the root --help output when HiddenDefaultCmd is set.
func TestCompletion_NotInHelpOutput(t *testing.T) {
	out, _, err := runCmd(t, "--help")
	if err != nil {
		t.Fatalf("--help returned error: %v", err)
	}
	// The word "completion" must not appear as a listed subcommand.
	// (It may legitimately appear inside the Long description as a word, but
	// cobra lists sub-commands using their exact name on its own line.)
	lines := strings.Split(out, "\n")
	for _, line := range lines {
		// Look for lines where "completion" appears as the first non-space token
		// (i.e. listed as a subcommand name).
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "completion") {
			t.Errorf("completion appears as a listed subcommand in --help output:\n  %q", line)
		}
	}
}
