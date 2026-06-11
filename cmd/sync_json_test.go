package cmd_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/AwesomeCICD/circleci-org-migration-cli/cmd"
)

// ---------------------------------------------------------------------------
// sync --json flag
// ---------------------------------------------------------------------------

// TestSyncCmd_JSONFlagRegistered verifies that --json is a local flag on the
// sync subcommand.
func TestSyncCmd_JSONFlagRegistered(t *testing.T) {
	syncSub := findSyncCmd(t)
	f := syncSub.Flags().Lookup("json")
	if f == nil {
		t.Fatal("sync --json flag not registered")
	}
	if f.Hidden {
		t.Error("sync --json should not be hidden")
	}
}

// TestSyncCmd_JSON_NotGlobal verifies that --json is NOT a persistent/global
// flag on the sync subcommand.
func TestSyncCmd_JSON_NotGlobal(t *testing.T) {
	root := cmd.MakeCommands()
	if root.PersistentFlags().Lookup("json") != nil {
		t.Error("--json must not be a persistent/global flag; it should be local to each command")
	}
}

// TestSyncCmd_JSONFlag_Accepted verifies that passing --json to sync does not
// cause a flag-parsing error.
func TestSyncCmd_JSONFlag_Accepted(t *testing.T) {
	t.Setenv("CIRCLECI_CLI_TOKEN", "")
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "")
	t.Setenv("CIRCLECI_DEST_TOKEN", "")

	dir := t.TempDir()
	mPath := writeTinyManifest(t, dir)

	_, _, err := runSyncCmd(t, "--manifest", mPath, "--json")
	if err != nil {
		if strings.Contains(err.Error(), "unknown flag") {
			t.Fatalf("--json is an unknown flag: %v", err)
		}
		// token error is acceptable — we just want to know the flag parsed fine.
	}
}

// TestSyncCmd_JSON_SkipAllSections_EmitsValidJSON verifies that when all
// sections are skipped and --json is set, the output is valid JSON with the
// expected top-level keys.
func TestSyncCmd_JSON_SkipAllSections_EmitsValidJSON(t *testing.T) {
	t.Setenv("CIRCLECI_CLI_TOKEN", "fake-token-sync-json")
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "")
	t.Setenv("CIRCLECI_DEST_TOKEN", "")

	dir := t.TempDir()
	mPath := writeTinyManifest(t, dir)

	stdout, _, err := runSyncCmd(t,
		"--manifest", mPath,
		"--skip-contexts",
		"--skip-projects",
		"--skip-org-settings",
		"--json",
	)
	if err != nil {
		t.Fatalf("expected success when skipping all sections, got: %v", err)
	}

	// Must be valid JSON.
	var result map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &result); err != nil {
		t.Fatalf("--json output is not valid JSON: %v\noutput: %q", err, stdout)
	}

	// Top-level keys.
	for _, key := range []string{"dry_run", "sections", "dest_org_slug"} {
		// dest_org_slug is omitempty so may be absent when dest is empty — skip
		// that one in the empty-manifest case.
		if key == "dest_org_slug" {
			continue
		}
		if _, ok := result[key]; !ok {
			t.Errorf("expected key %q in JSON output; got: %v", key, result)
		}
	}

	// dry_run should be true (no --apply).
	if dr, ok := result["dry_run"]; !ok || dr != true {
		t.Errorf("expected dry_run=true in JSON output; got: %v", result["dry_run"])
	}

	// sections should be an array (may be empty since all sections were skipped).
	if sections, ok := result["sections"]; !ok {
		t.Error("expected 'sections' key in JSON output")
	} else {
		if _, ok := sections.([]any); !ok {
			t.Errorf("expected 'sections' to be a JSON array; got %T", sections)
		}
	}
}

// TestSyncCmd_JSON_HumanSummaryAbsent verifies that when --json is set, the
// human-readable "== ... sync — DRY RUN ==" headers are NOT written to stdout.
func TestSyncCmd_JSON_HumanSummaryAbsent(t *testing.T) {
	t.Setenv("CIRCLECI_CLI_TOKEN", "fake-token-sync-json-nosummary")
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "")
	t.Setenv("CIRCLECI_DEST_TOKEN", "")

	dir := t.TempDir()
	mPath := writeTinyManifest(t, dir)

	stdout, _, err := runSyncCmd(t,
		"--manifest", mPath,
		"--skip-contexts",
		"--skip-projects",
		"--skip-org-settings",
		"--json",
	)
	if err != nil {
		t.Fatalf("expected success: %v", err)
	}
	if strings.Contains(stdout, "DRY RUN") {
		t.Error("with --json, human-readable 'DRY RUN' header must not appear in stdout")
	}
	if strings.Contains(stdout, "== Contexts sync") {
		t.Error("with --json, human-readable section header must not appear in stdout")
	}
}

// TestBuildSyncSummary_NoSecrets verifies that the SyncJSONSummary struct
// serialises to JSON without any secret values — only status counts and
// target names (which are context/project names, not secret values).
func TestBuildSyncSummary_NoSecrets(t *testing.T) {
	summary := cmd.SyncJSONSummary{
		DryRun:      true,
		DestOrgSlug: "gh/acme-new",
		Sections: []cmd.SyncSectionSummary{
			{Section: "Contexts", Created: 3, Exists: 1},
			{Section: "Projects", Created: 2, Manual: 1},
		},
		Warnings: nil,
	}

	data, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	out := string(data)

	// No secret values.
	if strings.Contains(out, "xxxx") || strings.Contains(out, "REPLACE_ME") {
		t.Errorf("JSON output should not contain secret placeholder values; got: %s", out)
	}

	// Top-level keys.
	for _, key := range []string{"dry_run", "dest_org_slug", "sections"} {
		if !strings.Contains(out, `"`+key+`"`) {
			t.Errorf("expected top-level key %q in JSON output; got: %s", key, out)
		}
	}

	// Section keys.
	for _, key := range []string{"section", "created", "exists", "set", "skipped", "manual", "error"} {
		if !strings.Contains(out, `"`+key+`"`) {
			t.Errorf("expected section key %q in JSON output; got: %s", key, out)
		}
	}

	// Round-trip.
	var decoded cmd.SyncJSONSummary
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("round-trip: %v", err)
	}
	if decoded.DryRun != true {
		t.Errorf("DryRun: got %v want true", decoded.DryRun)
	}
	if len(decoded.Sections) != 2 {
		t.Errorf("Sections: got %d want 2", len(decoded.Sections))
	}
}

// TestBuildSyncSummary_TopLevelKeys verifies the JSON schema by unmarshalling
// into a generic map and checking for expected top-level keys.
func TestBuildSyncSummary_TopLevelKeys(t *testing.T) {
	summary := cmd.SyncJSONSummary{
		DryRun:   false,
		Sections: []cmd.SyncSectionSummary{},
	}

	data, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	want := []string{"dry_run", "sections"}
	for _, k := range want {
		if _, ok := m[k]; !ok {
			t.Errorf("expected top-level key %q in SyncJSONSummary schema", k)
		}
	}
}
