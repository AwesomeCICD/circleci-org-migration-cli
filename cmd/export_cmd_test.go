package cmd_test

import (
	"strings"
	"testing"

	"github.com/AwesomeCICD/circleci-org-migration-cli/cmd"
	"github.com/spf13/cobra"
)

// ---------------------------------------------------------------------------
// export subcommand validation tests
// ---------------------------------------------------------------------------

// TestExportCommand_NoOrg_ReturnsError verifies that running "export" without
// the required --source-org flag returns an error mentioning "org".
func TestExportCommand_NoOrg_ReturnsError(t *testing.T) {
	// Clear all token env vars so the org check fires before any token check.
	t.Setenv("CIRCLECI_CLI_TOKEN", "")
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "")
	t.Setenv("CIRCLECI_DEST_TOKEN", "")

	root := cmd.MakeCommands()
	var outBuf, errBuf strings.Builder
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	root.SetArgs([]string{"export"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error when --source-org is missing, got nil")
	}
	if !strings.Contains(err.Error(), "org") {
		t.Errorf("error %q does not mention 'org'", err.Error())
	}
}

// TestExportCommand_NoToken_ReturnsError verifies that running
// "export --source-org gh/x" with no API token available returns an error
// mentioning "token".
func TestExportCommand_NoToken_ReturnsError(t *testing.T) {
	// Ensure no token env vars are set.
	t.Setenv("CIRCLECI_CLI_TOKEN", "")
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "")
	t.Setenv("CIRCLECI_DEST_TOKEN", "")

	root := cmd.MakeCommands()
	var outBuf, errBuf strings.Builder
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	// Use the deprecated --org alias to also verify back-compat here.
	root.SetArgs([]string{"export", "--org", "gh/x"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error when no token is available, got nil")
	}
	if !strings.Contains(err.Error(), "token") {
		t.Errorf("error %q does not mention 'token'", err.Error())
	}
}

// TestExportCommand_TokenFromEnv_Proceeds verifies that the token check passes
// when CIRCLECI_CLI_TOKEN is set (the command will then fail on the network
// call, which is expected and acceptable).
func TestExportCommand_TokenFromEnv_Proceeds(t *testing.T) {
	t.Setenv("CIRCLECI_CLI_TOKEN", "fake-token-for-test")
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "")
	t.Setenv("CIRCLECI_DEST_TOKEN", "")

	root := cmd.MakeCommands()
	var outBuf, errBuf strings.Builder
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	root.SetArgs([]string{"export", "--source-org", "gh/x"})

	err := root.Execute()
	// The token check itself should pass; we expect a network/client error,
	// NOT the "no source API token" error.
	if err != nil && strings.Contains(err.Error(), "no source API token") {
		t.Errorf("with CIRCLECI_CLI_TOKEN set, should not get token error; got: %v", err)
	}
}

// TestExportCommand_SourceTokenFromEnv_Proceeds verifies that
// CIRCLECI_SOURCE_TOKEN is also accepted as a token source.
func TestExportCommand_SourceTokenFromEnv_Proceeds(t *testing.T) {
	t.Setenv("CIRCLECI_CLI_TOKEN", "")
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "fake-source-token")
	t.Setenv("CIRCLECI_DEST_TOKEN", "")

	root := cmd.MakeCommands()
	var outBuf, errBuf strings.Builder
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	root.SetArgs([]string{"export", "--source-org", "gh/x"})

	err := root.Execute()
	if err != nil && strings.Contains(err.Error(), "no source API token") {
		t.Errorf("with CIRCLECI_SOURCE_TOKEN set, should not get token error; got: %v", err)
	}
}

// TestExportCommand_FlagsRegistered verifies that the export subcommand
// exposes the expected flags — including both canonical names and hidden aliases.
func TestExportCommand_FlagsRegistered(t *testing.T) {
	root := cmd.MakeCommands()
	var exportCmd *cobra.Command
	for _, sub := range root.Commands() {
		if strings.HasPrefix(sub.Use, "export") {
			exportCmd = sub
			break
		}
	}
	if exportCmd == nil {
		t.Fatal("export subcommand not found")
	}

	// Canonical flags (new names).
	wantCanonical := []string{
		"source-org",
		"output",
		"report",
		"project",
		"skip-contexts",
		"skip-projects",
		"skip-extras",
	}
	for _, name := range wantCanonical {
		if exportCmd.Flags().Lookup(name) == nil {
			t.Errorf("export canonical flag --%s not registered", name)
		}
	}

	// Hidden back-compat aliases must still be registered (so old invocations work).
	wantAliases := []string{"org", "projects"}
	for _, name := range wantAliases {
		f := exportCmd.Flags().Lookup(name)
		if f == nil {
			t.Errorf("export back-compat alias --%s not registered", name)
			continue
		}
		if !f.Hidden {
			t.Errorf("export back-compat alias --%s should be hidden", name)
		}
	}
}

// TestExportCommand_OrgAlias_WorksLikeSourceOrg verifies that the deprecated
// --org flag still populates the org slug and proceeds past the org-required
// check (to the token check), proving back-compat.
func TestExportCommand_OrgAlias_WorksLikeSourceOrg(t *testing.T) {
	t.Setenv("CIRCLECI_CLI_TOKEN", "")
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "")
	t.Setenv("CIRCLECI_DEST_TOKEN", "")

	root := cmd.MakeCommands()
	var outBuf, errBuf strings.Builder
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	root.SetArgs([]string{"export", "--org", "gh/acme"})

	err := root.Execute()
	// Should NOT get "--source-org is required" — the alias populated the org.
	if err != nil && strings.Contains(err.Error(), "source-org is required") {
		t.Errorf("--org alias should satisfy the org requirement; got: %v", err)
	}
	// Should get the token error (proving we passed the org check).
	if err == nil {
		t.Fatal("expected token error, got nil")
	}
	if !strings.Contains(err.Error(), "token") {
		t.Errorf("expected a token error after org check passes; got: %v", err)
	}
}

// TestExportCommand_ProjectsAliasAcceptsComma verifies that the deprecated
// --projects flag still accepts comma-separated slugs and merges them into
// the project list (reaches the token check, not a project-parse error).
func TestExportCommand_ProjectsAliasAcceptsComma(t *testing.T) {
	t.Setenv("CIRCLECI_CLI_TOKEN", "")
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "")
	t.Setenv("CIRCLECI_DEST_TOKEN", "")

	root := cmd.MakeCommands()
	var outBuf, errBuf strings.Builder
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	root.SetArgs([]string{
		"export",
		"--org", "gh/acme",
		"--projects", "gh/acme/web,gh/acme/api",
	})

	err := root.Execute()
	// Should fail on token, not on flag parsing.
	if err != nil && !strings.Contains(err.Error(), "token") {
		t.Errorf("expected token error; got: %v", err)
	}
}

// TestExportCommand_ProjectFlagCanRepeat verifies that the canonical --project
// flag can be passed multiple times (StringArray semantics).
func TestExportCommand_ProjectFlagCanRepeat(t *testing.T) {
	t.Setenv("CIRCLECI_CLI_TOKEN", "")
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "")
	t.Setenv("CIRCLECI_DEST_TOKEN", "")

	root := cmd.MakeCommands()
	var outBuf, errBuf strings.Builder
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	root.SetArgs([]string{
		"export",
		"--source-org", "gh/acme",
		"--project", "gh/acme/web",
		"--project", "gh/acme/api",
	})

	err := root.Execute()
	// Should fail on token, not on flag parsing.
	if err != nil && !strings.Contains(err.Error(), "token") {
		t.Errorf("expected token error; got: %v", err)
	}
}

// TestExportCommand_SourceOrgAndOrgAreEquivalent checks that --source-org and
// --org both reach the token-error stage (i.e. both satisfy the required-org
// check), confirming they bind to the same underlying variable.
func TestExportCommand_SourceOrgAndOrgAreEquivalent(t *testing.T) {
	t.Setenv("CIRCLECI_CLI_TOKEN", "")
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "")
	t.Setenv("CIRCLECI_DEST_TOKEN", "")

	for _, flagName := range []string{"--source-org", "--org"} {
		t.Run(flagName, func(t *testing.T) {
			root := cmd.MakeCommands()
			var outBuf, errBuf strings.Builder
			root.SetOut(&outBuf)
			root.SetErr(&errBuf)
			root.SetArgs([]string{"export", flagName, "gh/acme"})

			err := root.Execute()
			if err == nil {
				t.Fatal("expected token error, got nil")
			}
			if !strings.Contains(err.Error(), "token") {
				t.Errorf("flag %s: expected token error; got: %v", flagName, err.Error())
			}
			if strings.Contains(err.Error(), "source-org is required") {
				t.Errorf("flag %s should satisfy org requirement; got: %v", flagName, err.Error())
			}
		})
	}
}
