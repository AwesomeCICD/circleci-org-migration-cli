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
// the required --org flag returns an error mentioning "org".
func TestExportCommand_NoOrg_ReturnsError(t *testing.T) {
	// Clear all token env vars so the --org check fires before any token check.
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
		t.Fatal("expected error when --org is missing, got nil")
	}
	if !strings.Contains(err.Error(), "org") {
		t.Errorf("error %q does not mention 'org'", err.Error())
	}
}

// TestExportCommand_NoToken_ReturnsError verifies that running
// "export --org gh/x" with no API token available returns an error
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
	root.SetArgs([]string{"export", "--org", "gh/x"})

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
	root.SetArgs([]string{"export", "--org", "gh/x"})

	err := root.Execute()
	if err != nil && strings.Contains(err.Error(), "no source API token") {
		t.Errorf("with CIRCLECI_SOURCE_TOKEN set, should not get token error; got: %v", err)
	}
}

// TestExportCommand_FlagsRegistered verifies that the export subcommand
// exposes the expected flags.
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

	wantFlags := []string{"org", "output", "report", "projects", "skip-contexts", "skip-projects", "skip-extras"}
	for _, name := range wantFlags {
		if exportCmd.Flags().Lookup(name) == nil {
			t.Errorf("export flag --%s not registered", name)
		}
	}
}
