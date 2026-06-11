package cmd_test

// parity_test.go — tests for the four circleci-cli parity items (issue #77):
//   1. CIRCLECI_CLI_HOST alias (preferred over CIRCLECI_HOST)
//   2. Circleci-Cli-Command request header on outbound API calls
//   3. SetFlagErrorFunc — consistent flag-error message
//   4. version format aligns with circleci-cli style

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"runtime"
	"strings"
	"testing"

	"github.com/AwesomeCICD/circleci-org-migration-cli/api/rest"
	"github.com/AwesomeCICD/circleci-org-migration-cli/cmd"
	"github.com/AwesomeCICD/circleci-org-migration-cli/version"
)

// ---------------------------------------------------------------------------
// 1. CIRCLECI_CLI_HOST alias
// ---------------------------------------------------------------------------

// TestCircleCLIHostAlias_TakesPrecedenceOverLegacy verifies that when both
// CIRCLECI_CLI_HOST and CIRCLECI_HOST are set, CIRCLECI_CLI_HOST wins.
func TestCircleCLIHostAlias_TakesPrecedenceOverLegacy(t *testing.T) {
	t.Setenv("CIRCLECI_CLI_HOST", "https://cli-host.example.com")
	t.Setenv("CIRCLECI_HOST", "https://legacy-host.example.com")

	root := cmd.MakeCommands()
	hostFlag := root.PersistentFlags().Lookup("host")
	if hostFlag == nil {
		t.Fatal("--host flag not registered")
	}
	if got := hostFlag.DefValue; got != "https://cli-host.example.com" {
		t.Errorf("--host default = %q; want CIRCLECI_CLI_HOST value %q",
			got, "https://cli-host.example.com")
	}
}

// TestCircleCLIHostAlias_FallsBackToLegacy verifies that CIRCLECI_HOST is
// still honoured when CIRCLECI_CLI_HOST is absent.
func TestCircleCLIHostAlias_FallsBackToLegacy(t *testing.T) {
	t.Setenv("CIRCLECI_CLI_HOST", "")
	t.Setenv("CIRCLECI_HOST", "https://legacy-host.example.com")

	root := cmd.MakeCommands()
	hostFlag := root.PersistentFlags().Lookup("host")
	if hostFlag == nil {
		t.Fatal("--host flag not registered")
	}
	if got := hostFlag.DefValue; got != "https://legacy-host.example.com" {
		t.Errorf("--host default = %q; want CIRCLECI_HOST value %q",
			got, "https://legacy-host.example.com")
	}
}

// TestCircleCLIHostAlias_NeitherSet uses the compiled-in default when neither
// env var is present.
func TestCircleCLIHostAlias_NeitherSet(t *testing.T) {
	t.Setenv("CIRCLECI_CLI_HOST", "")
	t.Setenv("CIRCLECI_HOST", "")

	root := cmd.MakeCommands()
	hostFlag := root.PersistentFlags().Lookup("host")
	if hostFlag == nil {
		t.Fatal("--host flag not registered")
	}
	// The default must be a non-empty URL (the compiled-in default).
	if hostFlag.DefValue == "" {
		t.Error("--host default must not be empty when no env var is set")
	}
}

// TestHostFlagHelpMentionsBothEnvVars verifies that the --host flag usage
// text mentions both CIRCLECI_CLI_HOST and CIRCLECI_HOST.
func TestHostFlagHelpMentionsBothEnvVars(t *testing.T) {
	root := cmd.MakeCommands()
	hostFlag := root.PersistentFlags().Lookup("host")
	if hostFlag == nil {
		t.Fatal("--host flag not registered")
	}
	usage := hostFlag.Usage
	if !strings.Contains(usage, "CIRCLECI_CLI_HOST") {
		t.Errorf("--host usage %q does not mention CIRCLECI_CLI_HOST", usage)
	}
	if !strings.Contains(usage, "CIRCLECI_HOST") {
		t.Errorf("--host usage %q does not mention CIRCLECI_HOST", usage)
	}
}

// ---------------------------------------------------------------------------
// 2. Circleci-Cli-Command request header
// ---------------------------------------------------------------------------

// TestSetCommandPath_HeaderPresentOnRequest verifies that after calling
// SetCommandPath the Circleci-Cli-Command header appears on outbound requests.
func TestSetCommandPath_HeaderPresentOnRequest(t *testing.T) {
	var captured string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r.Header.Get("Circleci-Cli-Command")
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{}`)
	}))
	defer srv.Close()

	base, err := url.Parse(srv.URL + "/api/v2/")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	c := rest.New(base, "tok", srv.Client())
	c.SetCommandPath("circleci-migrate export")

	req, err := c.NewRequest(context.Background(), http.MethodGet, &url.URL{Path: "me"}, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	_, _ = c.DoRequest(req, nil)

	if captured != "circleci-migrate export" {
		t.Errorf("Circleci-Cli-Command = %q; want %q", captured, "circleci-migrate export")
	}
}

// TestSetCommandPath_HeaderAbsentWhenEmpty verifies that the header is omitted
// when no command path has been set (default zero value).
func TestSetCommandPath_HeaderAbsentWhenEmpty(t *testing.T) {
	var captured string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r.Header.Get("Circleci-Cli-Command")
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{}`)
	}))
	defer srv.Close()

	base, err := url.Parse(srv.URL + "/api/v2/")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	// No per-client SetCommandPath call. Clear any process-wide default that a
	// prior test (e.g. one that ran a command through PersistentPreRunE) may
	// have left set via rest.SetDefaultCommandPath, so commandPath is empty.
	rest.SetDefaultCommandPath("")
	c := rest.New(base, "tok", srv.Client())

	req, err := c.NewRequest(context.Background(), http.MethodGet, &url.URL{Path: "me"}, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	_, _ = c.DoRequest(req, nil)

	if captured != "" {
		t.Errorf("Circleci-Cli-Command = %q; want empty (not set)", captured)
	}
}

// TestRootPersistentPreRun_SetsDefaultCommandPath verifies that running a
// command through the tree wires the active command path into rest via
// SetDefaultCommandPath (C4), so that every REST client built afterwards
// forwards the Circleci-Cli-Command header without each call site opting in.
func TestRootPersistentPreRun_SetsDefaultCommandPath(t *testing.T) {
	rest.SetDefaultCommandPath("") // reset any leftover state from other tests.

	// `version` exercises PersistentPreRunE without making API calls.
	if _, _, err := runCmd(t, "version"); err != nil {
		t.Fatalf("running version: %v", err)
	}

	var captured string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r.Header.Get("Circleci-Cli-Command")
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{}`)
	}))
	defer srv.Close()

	base, err := url.Parse(srv.URL + "/api/v2/")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	// A client built AFTER the command ran must inherit the default path.
	c := rest.New(base, "tok", srv.Client())
	req, err := c.NewRequest(context.Background(), http.MethodGet, &url.URL{Path: "me"}, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	_, _ = c.DoRequest(req, nil)

	if captured != "circleci-migrate version" {
		t.Errorf("Circleci-Cli-Command = %q; want %q", captured, "circleci-migrate version")
	}
}

// ---------------------------------------------------------------------------
// 3. SetFlagErrorFunc
// ---------------------------------------------------------------------------

// TestSetFlagErrorFunc_UnknownFlag verifies that passing an unknown flag
// produces an error message that includes a usage hint ("--help").
func TestSetFlagErrorFunc_UnknownFlag(t *testing.T) {
	_, _, err := runCmd(t, "--unknown-flag-xyz")
	if err == nil {
		t.Fatal("expected error for unknown flag, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "--help") {
		t.Errorf("flag error %q does not contain '--help' usage hint", msg)
	}
}

// ---------------------------------------------------------------------------
// 4. version format
// ---------------------------------------------------------------------------

// TestVersionFormat_CircleCLIStyle verifies the version output matches the
// circleci-cli style: "circleci-migrate v<version> (<commit>) <os>/<arch>".
func TestVersionFormat_CircleCLIStyle(t *testing.T) {
	out, _, err := runCmd(t, "version")
	if err != nil {
		t.Fatalf("version command error: %v", err)
	}

	// Must contain the version string prefixed with "v".
	wantVersion := "v" + version.Version
	if !strings.Contains(out, wantVersion) {
		t.Errorf("output %q does not contain %q", out, wantVersion)
	}

	// Must contain the commit in parentheses, not "commit: <sha>".
	wantCommitFmt := "(" + version.Commit + ")"
	if !strings.Contains(out, wantCommitFmt) {
		t.Errorf("output %q does not contain commit in parentheses %q", out, wantCommitFmt)
	}
	if strings.Contains(out, "commit:") {
		t.Errorf("output %q should NOT use 'commit:' label (old format)", out)
	}

	// Must contain OS/arch on the same line.
	wantOSArch := runtime.GOOS + "/" + runtime.GOARCH
	if !strings.Contains(out, wantOSArch) {
		t.Errorf("output %q does not contain OS/arch %q", out, wantOSArch)
	}

	// Must be a single line (trailing newline allowed).
	trimmed := strings.TrimRight(out, "\n")
	if strings.Contains(trimmed, "\n") {
		t.Errorf("version output has more than one line: %q", out)
	}
}
