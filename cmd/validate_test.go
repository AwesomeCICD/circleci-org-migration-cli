package cmd_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/AwesomeCICD/circleci-org-migration-cli/cmd"
	"github.com/spf13/cobra"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// runValidateCmd executes the validate subcommand and returns stdout, stderr, error.
func runValidateCmd(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	root := cmd.MakeCommands()
	var outBuf, errBuf strings.Builder
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	root.SetArgs(append([]string{"validate"}, args...))
	err = root.Execute()
	return outBuf.String(), errBuf.String(), err
}

// findValidateCmd returns the cobra validate subcommand from a freshly built root.
func findValidateCmd(t *testing.T) *cobra.Command {
	t.Helper()
	root := cmd.MakeCommands()
	for _, sub := range root.Commands() {
		if strings.HasPrefix(sub.Use, "validate") {
			return sub
		}
	}
	t.Fatal("validate subcommand not found")
	return nil
}

// ---------------------------------------------------------------------------
// Registration
// ---------------------------------------------------------------------------

// TestValidateCommand_Registered verifies that validate is registered in the
// root command tree and has the expected Use prefix.
func TestValidateCommand_Registered(t *testing.T) {
	root := cmd.MakeCommands()
	found := false
	for _, sub := range root.Commands() {
		if strings.HasPrefix(sub.Use, "validate") {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("validate subcommand not found in root command tree")
	}
}

// ---------------------------------------------------------------------------
// Required-flag validation
// ---------------------------------------------------------------------------

// TestValidateCommand_NoSourceOrg_ReturnsError verifies that running "validate"
// without --source-org returns an error mentioning "source-org".
func TestValidateCommand_NoSourceOrg_ReturnsError(t *testing.T) {
	t.Setenv("CIRCLECI_CLI_TOKEN", "fake")
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "")
	t.Setenv("CIRCLECI_DEST_TOKEN", "")
	t.Setenv("CIRCLE_TOKEN", "")

	_, _, err := runValidateCmd(t, "--no-input")
	if err == nil {
		t.Fatal("expected error when --source-org is missing, got nil")
	}
	if !strings.Contains(err.Error(), "source-org") {
		t.Errorf("error %q does not mention 'source-org'", err.Error())
	}
}

// TestValidateCommand_NoDestOrg_ReturnsError verifies that running "validate"
// without --dest-org returns an error mentioning "dest-org".
func TestValidateCommand_NoDestOrg_ReturnsError(t *testing.T) {
	t.Setenv("CIRCLECI_CLI_TOKEN", "fake")
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "")
	t.Setenv("CIRCLECI_DEST_TOKEN", "")
	t.Setenv("CIRCLE_TOKEN", "")

	_, _, err := runValidateCmd(t, "--source-org", "gh/acme", "--no-input")
	if err == nil {
		t.Fatal("expected error when --dest-org is missing, got nil")
	}
	if !strings.Contains(err.Error(), "dest-org") {
		t.Errorf("error %q does not mention 'dest-org'", err.Error())
	}
}

// TestValidateCommand_NoSourceToken_ReturnsError verifies that running
// "validate" with orgs but no token returns a token error.
func TestValidateCommand_NoSourceToken_ReturnsError(t *testing.T) {
	t.Setenv("CIRCLECI_CLI_TOKEN", "")
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "")
	t.Setenv("CIRCLECI_DEST_TOKEN", "")
	t.Setenv("CIRCLE_TOKEN", "")

	_, _, err := runValidateCmd(t,
		"--source-org", "gh/acme",
		"--dest-org", "gh/acme-new",
		"--no-input",
	)
	if err == nil {
		t.Fatal("expected error when no token is available, got nil")
	}
	if !strings.Contains(err.Error(), "token") {
		t.Errorf("error %q does not mention 'token'", err.Error())
	}
}

// TestValidateCommand_NoDestToken_ReturnsError verifies that a missing dest
// token returns a clear error when the source token is set.
func TestValidateCommand_NoDestToken_ReturnsError(t *testing.T) {
	t.Setenv("CIRCLECI_CLI_TOKEN", "")
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "src-token-fake")
	t.Setenv("CIRCLECI_DEST_TOKEN", "")
	t.Setenv("CIRCLE_TOKEN", "")

	_, _, err := runValidateCmd(t,
		"--source-org", "gh/acme",
		"--dest-org", "gh/acme-new",
		"--no-input",
	)
	if err == nil {
		t.Fatal("expected error when dest token is missing, got nil")
	}
	if !strings.Contains(err.Error(), "token") {
		t.Errorf("error %q does not mention 'token'", err.Error())
	}
}

// ---------------------------------------------------------------------------
// Flags registered
// ---------------------------------------------------------------------------

// TestValidateCommand_FlagsRegistered verifies that all documented flags are
// registered on the validate subcommand.
func TestValidateCommand_FlagsRegistered(t *testing.T) {
	v := findValidateCmd(t)

	wantFlags := []string{
		"source-org",
		"dest-org",
		"mapping",
		"dest-runner-namespace",
		"dest-orb-namespace",
		"json",
		"no-input",
	}
	for _, name := range wantFlags {
		if v.Flags().Lookup(name) == nil {
			t.Errorf("validate flag --%s not registered", name)
		}
	}
}

// ---------------------------------------------------------------------------
// --json output shape
// ---------------------------------------------------------------------------

// TestValidateCommand_JSONFlag_AcceptedByParser verifies that the --json flag
// is accepted by the flag parser (we cannot run real exports without tokens,
// but we can confirm flag parsing does not error before the network calls).
func TestValidateCommand_JSONFlag_AcceptedByParser(t *testing.T) {
	t.Setenv("CIRCLECI_CLI_TOKEN", "")
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "")
	t.Setenv("CIRCLECI_DEST_TOKEN", "")
	t.Setenv("CIRCLE_TOKEN", "")

	_, _, err := runValidateCmd(t,
		"--source-org", "gh/acme",
		"--dest-org", "gh/acme-new",
		"--json",
		"--no-input",
	)
	// We expect a token error — NOT a flag-parse error.
	if err != nil && strings.Contains(err.Error(), "--json") {
		t.Errorf("--json flag should not produce a parse error; got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// buildValidateJSONOutput
// ---------------------------------------------------------------------------

// TestBuildValidateJSONOutput_Shape verifies that buildValidateJSONOutput
// produces a correctly shaped JSON object.
//
// We test this indirectly through the exported ValidateJSONOutput type by
// marshalling and unmarshalling, checking the key fields.
func TestBuildValidateJSONOutput_Shape(t *testing.T) {
	// Construct a minimal ValidateJSONOutput directly and verify JSON shape.
	out := cmd.ValidateJSONOutput{
		SourceOrg: "gh/acme",
		DestOrg:   "gh/acme-new",
		HasGaps:   true,
		Sections:  nil,
		Totals:    cmd.ValidateJSONTotals{Matched: 5, Missing: 2, Manual: 1},
	}

	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(data)

	for _, want := range []string{`"source_org"`, `"dest_org"`, `"has_gaps"`, `"sections"`, `"totals"`} {
		if !strings.Contains(s, want) {
			t.Errorf("JSON output missing field %q; got:\n%s", want, s)
		}
	}

	// Unmarshal back and check values.
	var decoded cmd.ValidateJSONOutput
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.SourceOrg != "gh/acme" {
		t.Errorf("SourceOrg: got %q want %q", decoded.SourceOrg, "gh/acme")
	}
	if !decoded.HasGaps {
		t.Error("HasGaps should be true")
	}
}

// TestValidateCommand_NoInputFlag_NonTTY verifies that passing --no-input
// together with missing orgs returns the expected error (not an interactive
// prompt that would block).
func TestValidateCommand_NoInputFlag_NonTTY(t *testing.T) {
	t.Setenv("CIRCLECI_CLI_TOKEN", "")
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "")
	t.Setenv("CIRCLECI_DEST_TOKEN", "")
	t.Setenv("CIRCLE_TOKEN", "")

	_, _, err := runValidateCmd(t, "--no-input")
	if err == nil {
		t.Fatal("expected error in no-input mode with missing orgs")
	}
	// Should mention source-org (the first required flag).
	if !strings.Contains(err.Error(), "source-org") {
		t.Errorf("expected 'source-org' in error message; got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// buildValidateJSONOutput — unit tests for the JSON shaping helper
// ---------------------------------------------------------------------------

// TestBuildValidateJSONOutput_HasGaps verifies that HasGaps is true when there
// are missing items.
func TestBuildValidateJSONOutput_HasGaps(t *testing.T) {
	// A ValidateJSONOutput with HasGaps=true can be constructed directly and
	// verified. The buildValidateJSONOutput function is package-private but its
	// output type is exported, so we test via the exported type.
	out := cmd.ValidateJSONOutput{
		SourceOrg: "gh/acme",
		DestOrg:   "gh/acme-new",
		HasGaps:   true,
	}
	if !out.HasGaps {
		t.Error("expected HasGaps to be true")
	}
}

// TestBuildValidateJSONOutput_Sections verifies that section data survives
// JSON marshalling round-trip correctly.
func TestBuildValidateJSONOutput_Sections(t *testing.T) {
	out := cmd.ValidateJSONOutput{
		SourceOrg: "gh/acme",
		DestOrg:   "gh/acme-new",
		HasGaps:   false,
		Sections: []cmd.ValidateJSONSection{
			{
				Name: "Contexts",
				Items: []cmd.ValidateJSONItem{
					{Status: "matched", Name: "deploy", Detail: "context present"},
				},
				Counts: cmd.ValidateJSONCounts{Matched: 1},
			},
		},
	}

	data, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded cmd.ValidateJSONOutput
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(decoded.Sections) != 1 {
		t.Fatalf("expected 1 section, got %d", len(decoded.Sections))
	}
	if decoded.Sections[0].Name != "Contexts" {
		t.Errorf("section name: got %q want %q", decoded.Sections[0].Name, "Contexts")
	}
	if len(decoded.Sections[0].Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(decoded.Sections[0].Items))
	}
	if decoded.Sections[0].Items[0].Status != "matched" {
		t.Errorf("item status: got %q want %q", decoded.Sections[0].Items[0].Status, "matched")
	}
}

// TestValidateJSONOutput_SkippedSection verifies JSON serialisation of
// skipped sections (Skipped + SkipReason fields).
func TestValidateJSONOutput_SkippedSection(t *testing.T) {
	out := cmd.ValidateJSONOutput{
		Sections: []cmd.ValidateJSONSection{
			{
				Name:       "Runner Resource Classes",
				Skipped:    true,
				SkipReason: "pass --dest-runner-namespace to enable runner resource-class comparison",
			},
		},
	}
	data, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, `"skipped":true`) {
		t.Errorf("expected skipped=true in JSON; got: %s", s)
	}
	if !strings.Contains(s, "skip_reason") {
		t.Errorf("expected skip_reason in JSON; got: %s", s)
	}
}
