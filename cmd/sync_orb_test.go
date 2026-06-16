package cmd_test

// sync_orb_test.go tests orb-related paths in the sync command that are not
// covered by the existing test suite. It uses the same helpers (runSyncCmd,
// writeTinyManifest) defined in sync_test.go.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// writeOrbManifest writes a manifest with the given captured orbs to dir and
// returns its path. orbNamespace is the source orb namespace; orbs is a list
// of simple captured orb entries (name only, one version each).
func writeOrbManifest(t *testing.T, dir, orbNamespace string, orbNames []string) string {
	t.Helper()

	orbsData := make([]any, 0, len(orbNames))
	for _, name := range orbNames {
		orbsData = append(orbsData, map[string]any{
			"name":       orbNamespace + "/" + name,
			"is_private": false,
			"versions": []any{
				map[string]any{
					"version": "1.0.0",
					"source":  "version: 2.1\n",
				},
			},
		})
	}

	m := map[string]any{
		"schema_version": "1",
		"source": map[string]any{
			"host": "https://circleci.com",
			"org":  map[string]any{"slug": "gh/testorg", "name": "testorg"},
		},
		"contexts":      []any{},
		"projects":      []any{},
		"orb_namespace": orbNamespace,
		"orbs":          orbsData,
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		t.Fatalf("marshal orb manifest: %v", err)
	}
	path := filepath.Join(dir, "manifest.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write orb manifest: %v", err)
	}
	return path
}

// ---------------------------------------------------------------------------
// --skip-orb flag
// ---------------------------------------------------------------------------

// TestSyncCmd_SkipOrbFlagRegistered verifies that --skip-orb is registered on
// the sync subcommand.
func TestSyncCmd_SkipOrbFlagRegistered(t *testing.T) {
	syncSub := findSyncCmd(t)
	if syncSub.Flags().Lookup("skip-orb") == nil {
		t.Error("sync flag --skip-orb not registered")
	}
}

// TestSyncCmd_DestOrbNamespaceFlagRegistered verifies that --dest-orb-namespace
// is registered on the sync subcommand.
func TestSyncCmd_DestOrbNamespaceFlagRegistered(t *testing.T) {
	syncSub := findSyncCmd(t)
	if syncSub.Flags().Lookup("dest-orb-namespace") == nil {
		t.Error("sync flag --dest-orb-namespace not registered")
	}
}

// ---------------------------------------------------------------------------
// dry-run with orbs in manifest + no --dest-orb-namespace → "manual" actions
// ---------------------------------------------------------------------------

// TestSyncCmd_OrbsInManifest_NoDestNamespace_ReportsManual verifies that when
// the manifest contains captured orbs but --dest-orb-namespace is NOT provided,
// the sync command (dry-run) reports the orbs as requiring manual action.
//
// This exercises the SyncOrbs "no destination namespace" path wired through the
// cmd layer: wireOrb is true (manifest has orbs), SyncOrbs is called, every orb
// is recorded as "manual", and printSyncReport emits a "Needs attention" block.
func TestSyncCmd_OrbsInManifest_NoDestNamespace_ReportsManual(t *testing.T) {
	t.Setenv("CIRCLECI_CLI_TOKEN", "fake-token-orb-manual")
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "")
	t.Setenv("CIRCLECI_DEST_TOKEN", "")

	dir := t.TempDir()
	mPath := writeOrbManifest(t, dir, "acme", []string{"my-orb", "other-orb"})

	stdout, _, err := runSyncCmd(t,
		"--manifest", mPath,
		"--skip-contexts",
		"--skip-projects",
		"--skip-org-settings",
		"--skip-runner",
		"--skip-preflight",
		// NO --dest-orb-namespace → orbs must be flagged as manual
	)
	if err != nil {
		t.Fatalf("expected success (dry-run), got: %v", err)
	}

	// The sync report must show the "Orbs" section with manual items.
	if !strings.Contains(stdout, "Orbs") {
		t.Errorf("expected 'Orbs' section in output; got:\n%s", stdout)
	}
	// "manual" or "Needs attention" must appear because both orbs require it.
	if !strings.Contains(stdout, "manual") && !strings.Contains(stdout, "Needs attention") {
		t.Errorf("expected 'manual' or 'Needs attention' in output when no --dest-orb-namespace; got:\n%s", stdout)
	}
	// Both orb names should be mentioned.
	for _, name := range []string{"acme/my-orb", "acme/other-orb"} {
		if !strings.Contains(stdout, name) {
			t.Errorf("expected orb %q in output; got:\n%s", name, stdout)
		}
	}
}

// TestSyncCmd_OrbsInManifest_NoDestNamespace_JSON_IncludesOrbWarnings verifies
// that the JSON output includes the "Orbs" section with manual-count > 0 when
// orbs are present but no destination namespace is provided.
func TestSyncCmd_OrbsInManifest_NoDestNamespace_JSON_IncludesOrbWarnings(t *testing.T) {
	t.Setenv("CIRCLECI_CLI_TOKEN", "fake-token-orb-manual-json")
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "")
	t.Setenv("CIRCLECI_DEST_TOKEN", "")

	dir := t.TempDir()
	mPath := writeOrbManifest(t, dir, "acme", []string{"my-orb"})

	stdout, _, err := runSyncCmd(t,
		"--manifest", mPath,
		"--skip-contexts",
		"--skip-projects",
		"--skip-org-settings",
		"--skip-runner",
		"--skip-preflight",
		"--json",
	)
	if err != nil {
		t.Fatalf("expected success (dry-run+json), got: %v", err)
	}

	// Must be valid JSON.
	var result map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &result); err != nil {
		t.Fatalf("--json stdout is not valid JSON: %v\noutput: %q", err, stdout)
	}

	// The sections array must contain an "Orbs" entry.
	sections, _ := result["sections"].([]any)
	var foundOrbs bool
	for _, s := range sections {
		sec, _ := s.(map[string]any)
		if sec["section"] == "Orbs" {
			foundOrbs = true
			// manual count must be >= 1.
			manual, _ := sec["manual"].(float64)
			if manual < 1 {
				t.Errorf("expected manual >= 1 in Orbs section, got %v", sec)
			}
		}
	}
	if !foundOrbs {
		t.Errorf("expected 'Orbs' section in JSON sections; got: %v", sections)
	}

	// warnings must include the manual orb.
	warnings, _ := result["warnings"].([]any)
	if len(warnings) == 0 {
		t.Errorf("expected at least one warning for manual orb; got none")
	}
}

// TestSyncCmd_SkipOrb_ManifestWithOrbs_NoOrbSection verifies that --skip-orb
// suppresses the orb sync section even when the manifest has orbs.
func TestSyncCmd_SkipOrb_ManifestWithOrbs_NoOrbSection(t *testing.T) {
	t.Setenv("CIRCLECI_CLI_TOKEN", "fake-token-skip-orb")
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "")
	t.Setenv("CIRCLECI_DEST_TOKEN", "")

	dir := t.TempDir()
	mPath := writeOrbManifest(t, dir, "acme", []string{"my-orb"})

	stdout, _, err := runSyncCmd(t,
		"--manifest", mPath,
		"--skip-contexts",
		"--skip-projects",
		"--skip-org-settings",
		"--skip-runner",
		"--skip-orb",
		"--skip-preflight",
	)
	if err != nil {
		t.Fatalf("expected success with --skip-orb, got: %v", err)
	}

	// With --skip-orb, the Orbs section must NOT appear in the output.
	if strings.Contains(stdout, "Orbs") {
		t.Errorf("--skip-orb should suppress the Orbs section; stdout:\n%s", stdout)
	}
}

// TestSyncCmd_OnlyOrb_ReachesOrbSection verifies that --only orb skips all
// other sections and runs only the orb section (which flags items as manual
// when no --dest-orb-namespace is set).
func TestSyncCmd_OnlyOrb_ReachesOrbSection(t *testing.T) {
	t.Setenv("CIRCLECI_CLI_TOKEN", "fake-token-only-orb")
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "")
	t.Setenv("CIRCLECI_DEST_TOKEN", "")

	dir := t.TempDir()
	mPath := writeOrbManifest(t, dir, "acme", []string{"my-orb"})

	stdout, _, err := runSyncCmd(t,
		"--manifest", mPath,
		"--only", "orb",
		"--skip-preflight",
	)
	if err != nil {
		t.Fatalf("expected success with --only orb, got: %v", err)
	}

	// The Orbs section should appear in the output.
	if !strings.Contains(stdout, "Orbs") {
		t.Errorf("expected 'Orbs' section with --only orb; stdout:\n%s", stdout)
	}
}

// ---------------------------------------------------------------------------
// buildSyncSummary: orbs section inclusion
// ---------------------------------------------------------------------------

// TestSyncCmd_JSON_OrbSection_AppearsInSections verifies that when the sync
// runs with orbs in the manifest (and no dest namespace), the JSON output
// includes the Orbs section before other sections are skipped.
func TestSyncCmd_JSON_OrbSection_AppearsInSections(t *testing.T) {
	t.Setenv("CIRCLECI_CLI_TOKEN", "fake-token-json-orb-section")
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "")
	t.Setenv("CIRCLECI_DEST_TOKEN", "")

	dir := t.TempDir()
	mPath := writeOrbManifest(t, dir, "acme", []string{"my-orb"})

	stdout, _, err := runSyncCmd(t,
		"--manifest", mPath,
		"--skip-contexts",
		"--skip-projects",
		"--skip-org-settings",
		"--skip-runner",
		"--skip-preflight",
		"--json",
	)
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &result); err != nil {
		t.Fatalf("invalid JSON: %v\noutput: %q", err, stdout)
	}

	// dry_run must be true (no --apply).
	if dr, _ := result["dry_run"].(bool); !dr {
		t.Errorf("expected dry_run=true, got %v", result["dry_run"])
	}

	sections, _ := result["sections"].([]any)
	var sectionNames []string
	for _, s := range sections {
		sec, _ := s.(map[string]any)
		sectionNames = append(sectionNames, sec["section"].(string))
	}

	found := false
	for _, name := range sectionNames {
		if name == "Orbs" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'Orbs' in sections %v", sectionNames)
	}
}
