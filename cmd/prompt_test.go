package cmd_test

import (
	"strings"
	"testing"

	"github.com/AwesomeCICD/circleci-org-migration-cli/cmd"
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
}

// runWalkthrough drives the interactive guided walkthrough with scripted input
// and returns the skip flags, apply flag, and any error.  rootOptions are
// re-initialised via MakeCommands so that t.Setenv changes are picked up.
func runWalkthrough(t *testing.T, input string) (skips skipResults, outApply bool, err error) {
	t.Helper()

	// Build a fresh command tree so rootOptions is re-seeded from env vars.
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

	_, _, _, _, ap, _, skipCtx, skipProj, skipOrg, skipExt, walkErr :=
		cmd.RunMigrateWalkthroughWith(p, migCmd, "", "", false)

	return skipResults{
		contexts:    skipCtx,
		projects:    skipProj,
		orgSettings: skipOrg,
		extras:      skipExt,
	}, ap, walkErr
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
	input := "\ngh/acme\ngh/acme-new\nall\nn\nskip\ny\n"
	_, _, err := runWalkthrough(t, input)
	if err != nil {
		t.Fatalf("unexpected error after re-prompt: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Prompt behaviour — dry-run / apply (askBool)
// ---------------------------------------------------------------------------

// TestPrompt_DryRun_DefaultYes_EmptyInput verifies that pressing Enter on the
// dry-run prompt (default=yes) results in apply=false.
func TestPrompt_DryRun_DefaultYes_EmptyInput(t *testing.T) {
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "pre-src")
	t.Setenv("CIRCLECI_DEST_TOKEN", "pre-dst")

	// Empty line on dry-run prompt → accept default (dry run, not apply).
	input := "gh/acme\ngh/acme-new\nall\nn\nskip\n\n"
	_, outApply, err := runWalkthrough(t, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outApply {
		t.Error("expected apply=false (dry run) when user presses Enter on dry-run prompt")
	}
}

// TestPrompt_Apply_Confirmed verifies that declining dry run and confirming
// apply results in apply=true.
func TestPrompt_Apply_Confirmed(t *testing.T) {
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "pre-src")
	t.Setenv("CIRCLECI_DEST_TOKEN", "pre-dst")

	// "n" → skip dry run (wants apply); "y" → confirm apply.
	input := "gh/acme\ngh/acme-new\nall\nn\nskip\nn\ny\n"
	_, outApply, err := runWalkthrough(t, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !outApply {
		t.Error("expected apply=true after user confirms apply")
	}
}

// TestPrompt_Apply_Cancelled verifies that declining the apply confirmation
// returns an error.
func TestPrompt_Apply_Cancelled(t *testing.T) {
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "pre-src")
	t.Setenv("CIRCLECI_DEST_TOKEN", "pre-dst")

	// "n" → skip dry run (wants apply); "n" → decline apply confirmation.
	input := "gh/acme\ngh/acme-new\nall\nn\nskip\nn\nn\n"
	_, _, err := runWalkthrough(t, input)
	if err == nil {
		t.Fatal("expected error when user cancels apply confirmation")
	}
	if !strings.Contains(err.Error(), "cancelled") {
		t.Errorf("error %q does not mention 'cancelled'", err.Error())
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

	input := "gh/acme\ngh/acme-new\nall\nn\nskip\ny\n"
	skips, _, err := runWalkthrough(t, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if skips.contexts || skips.projects || skips.orgSettings || skips.extras {
		t.Errorf("expected no skips with 'all', got: contexts=%v projects=%v orgSettings=%v extras=%v",
			skips.contexts, skips.projects, skips.orgSettings, skips.extras)
	}
}

// TestPrompt_MultiSelect_EmptyLine selects all components via the empty-line
// default.
func TestPrompt_MultiSelect_EmptyLine(t *testing.T) {
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "pre-src")
	t.Setenv("CIRCLECI_DEST_TOKEN", "pre-dst")

	// Empty line on multi-select prompt → "all" default.
	input := "gh/acme\ngh/acme-new\n\nn\nskip\ny\n"
	skips, _, err := runWalkthrough(t, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if skips.contexts || skips.projects || skips.orgSettings || skips.extras {
		t.Errorf("expected all selected with empty line, got: %+v", skips)
	}
}

// TestPrompt_MultiSelect_None verifies that "none" skips all components.
func TestPrompt_MultiSelect_None(t *testing.T) {
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "pre-src")
	t.Setenv("CIRCLECI_DEST_TOKEN", "pre-dst")

	input := "gh/acme\ngh/acme-new\nnone\nn\nskip\ny\n"
	skips, _, err := runWalkthrough(t, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !skips.contexts || !skips.projects || !skips.orgSettings || !skips.extras {
		t.Errorf("expected all skips with 'none', got: contexts=%v projects=%v orgSettings=%v extras=%v",
			skips.contexts, skips.projects, skips.orgSettings, skips.extras)
	}
}

// TestPrompt_MultiSelect_Subset selects only contexts (1) and projects (2).
func TestPrompt_MultiSelect_Subset(t *testing.T) {
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "pre-src")
	t.Setenv("CIRCLECI_DEST_TOKEN", "pre-dst")

	// Items 1,2 = contexts + projects.
	input := "gh/acme\ngh/acme-new\n1,2\nn\nskip\ny\n"
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
	input := "gh/acme\ngh/acme-new\n99\nall\nn\nskip\ny\n"
	skips, _, err := runWalkthrough(t, input)
	if err != nil {
		t.Fatalf("unexpected error after re-prompt: %v", err)
	}
	if skips.contexts || skips.projects || skips.orgSettings || skips.extras {
		t.Errorf("expected no skips after 'all'; got: %+v", skips)
	}
}

// ---------------------------------------------------------------------------
// Prompt behaviour — missing-secrets choice
// ---------------------------------------------------------------------------

// TestPrompt_MissingSecrets_Placeholder verifies that choosing item 2
// (placeholder) is accepted without error.
func TestPrompt_MissingSecrets_Placeholder(t *testing.T) {
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "pre-src")
	t.Setenv("CIRCLECI_DEST_TOKEN", "pre-dst")

	// Item 2 = placeholder.
	input := "gh/acme\ngh/acme-new\nall\nn\n2\ny\n"
	_, _, err := runWalkthrough(t, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Prompt behaviour — secrets bundle
// ---------------------------------------------------------------------------

// TestPrompt_SecretsBundle_Provided verifies that answering "y" to the bundle
// question prompts for a path and accepts the provided value.
func TestPrompt_SecretsBundle_Provided(t *testing.T) {
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "pre-src")
	t.Setenv("CIRCLECI_DEST_TOKEN", "pre-dst")

	// "y" → use bundle; "my-secrets.json" → path; then "skip" → dry run.
	input := "gh/acme\ngh/acme-new\nall\ny\nmy-secrets.json\nskip\ny\n"
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
	input := "gh/acme\ngh/acme-new\nall\nn\nskip\ny\n"
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
	input := "gh/acme\ngh/acme-new\nmy-src-token\nmy-dst-token\nall\nn\nskip\ny\n"
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
	input := "gh/acme\ngh/acme-new\n\nmy-src-token\nmy-dst-token\nall\nn\nskip\ny\n"
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
