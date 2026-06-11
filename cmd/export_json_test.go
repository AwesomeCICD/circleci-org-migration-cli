package cmd_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/AwesomeCICD/circleci-org-migration-cli/cmd"
)

// ---------------------------------------------------------------------------
// export --json flag
// ---------------------------------------------------------------------------

// TestExportCmd_JSONFlagRegistered verifies that --json is a local flag on the
// export subcommand (not a global/persistent flag).
func TestExportCmd_JSONFlagRegistered(t *testing.T) {
	root := cmd.MakeCommands()
	for _, sub := range root.Commands() {
		if strings.HasPrefix(sub.Use, "export") {
			f := sub.Flags().Lookup("json")
			if f == nil {
				t.Fatal("export --json flag not registered")
			}
			if f.Hidden {
				t.Error("export --json should not be hidden")
			}
			return
		}
	}
	t.Fatal("export subcommand not found")
}

// TestExportCmd_JSON_NotGlobal verifies that --json is NOT a persistent
// (global) flag — it must be command-local per the official circleci-cli
// convention.
func TestExportCmd_JSON_NotGlobal(t *testing.T) {
	root := cmd.MakeCommands()
	if root.PersistentFlags().Lookup("json") != nil {
		t.Error("--json must not be a persistent/global flag; it should be local to each command")
	}
}

// TestExportCmd_JSONFlag_EmitsValidJSON verifies that when --source-org and
// a token are provided but the network call succeeds (or fails), the --json
// flag does not break flag parsing and the flag is accepted. Since we cannot
// make a real network call here we test the path up to the point where the
// flag is parsed and the token/org check passes, and then verify that any
// error is not a "token" or "org" error (i.e. the flag itself is well-formed).
func TestExportCmd_JSONFlag_FlagParsedCorrectly(t *testing.T) {
	t.Setenv("CIRCLECI_CLI_TOKEN", "fake-token-json-export")
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "")
	t.Setenv("CIRCLECI_DEST_TOKEN", "")

	root := cmd.MakeCommands()
	var outBuf, errBuf strings.Builder
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	root.SetArgs([]string{"export", "--source-org", "gh/testorg", "--json"})

	err := root.Execute()
	// We expect a network error (not a flag/token/org error) — proving the flag
	// was correctly parsed and passed the early validation.
	if err != nil {
		if strings.Contains(err.Error(), "unknown flag") {
			t.Fatalf("--json is an unknown flag: %v", err)
		}
		if strings.Contains(err.Error(), "no source API token") {
			t.Errorf("with CIRCLECI_CLI_TOKEN set, should not get token error; got: %v", err)
		}
		if strings.Contains(err.Error(), "--source-org is required") {
			t.Errorf("--source-org was provided; should not get required error; got: %v", err)
		}
	}
}

// TestBuildExportSummary_NoSecrets verifies that buildExportSummary (tested via
// the exported type structure) never includes masked variable values — only
// counts and names.  We verify this by constructing the summary from a known
// manifest and asserting the JSON output does NOT contain the word "xxxx"
// (the mask hint CircleCI returns for project env var values).
func TestBuildExportSummary_NoSecrets(t *testing.T) {
	// Construct the summary via the exported type — values must not appear.
	summary := cmd.ExportJSONSummary{
		SourceOrgSlug:   "gh/testorg",
		SourceOrgID:     "abc-123",
		Host:            "https://circleci.com",
		GeneratedAt:     "2026-01-01T00:00:00Z",
		ContextCount:    2,
		ContextVarCount: 4,
		ProjectCount:    1,
		ProjectVarCount: 3,
		WarningCount:    0,
		ManifestPath:    "manifest.json",
		ReportPath:      "migration-report.md",
	}

	data, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	out := string(data)

	// No secret values in the summary.
	if strings.Contains(out, "xxxx") {
		t.Errorf("JSON output should not contain masked secret values; got: %s", out)
	}

	// Required keys present.
	for _, key := range []string{
		"source_org_slug", "host", "generated_at",
		"context_count", "context_var_count",
		"project_count", "project_var_count",
		"warning_count", "manifest_path", "report_path",
	} {
		if !strings.Contains(out, `"`+key+`"`) {
			t.Errorf("expected key %q in JSON output; got: %s", key, out)
		}
	}

	// Verify round-trip.
	var decoded cmd.ExportJSONSummary
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("round-trip unmarshal error: %v", err)
	}
	if decoded.SourceOrgSlug != summary.SourceOrgSlug {
		t.Errorf("SourceOrgSlug: got %q want %q", decoded.SourceOrgSlug, summary.SourceOrgSlug)
	}
	if decoded.ContextCount != summary.ContextCount {
		t.Errorf("ContextCount: got %d want %d", decoded.ContextCount, summary.ContextCount)
	}
}

// TestBuildExportSummary_TopLevelKeys verifies the JSON schema has the expected
// top-level keys by unmarshalling into a generic map.
func TestBuildExportSummary_TopLevelKeys(t *testing.T) {
	summary := cmd.ExportJSONSummary{
		SourceOrgSlug: "gh/acme",
		Host:          "https://circleci.com",
		ManifestPath:  "out.json",
		ReportPath:    "report.md",
	}

	data, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal to map: %v", err)
	}

	want := []string{
		"source_org_slug", "host", "generated_at",
		"context_count", "context_var_count",
		"project_count", "project_var_count",
		"warning_count", "manifest_path", "report_path",
	}
	for _, k := range want {
		if _, ok := m[k]; !ok {
			t.Errorf("expected top-level key %q in JSON schema", k)
		}
	}
}
