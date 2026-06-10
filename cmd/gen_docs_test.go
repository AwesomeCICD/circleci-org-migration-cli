package cmd_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestGenDocsCommand_GeneratesFiles invokes gen-docs into a temp dir and
// verifies that the expected man pages and markdown files are produced.
func TestGenDocsCommand_GeneratesFiles(t *testing.T) {
	tmp := t.TempDir()
	manDir := filepath.Join(tmp, "man")
	mdDir := filepath.Join(tmp, "docs", "cli")

	_, _, err := runCmd(t, "gen-docs",
		"--man-dir", manDir,
		"--md-dir", mdDir,
	)
	if err != nil {
		t.Fatalf("gen-docs returned error: %v", err)
	}

	// Man pages
	wantMan := []string{
		"circleci-migrate.1",
		"circleci-migrate-export.1",
		"circleci-migrate-sync.1",
		"circleci-migrate-migrate.1",
		"circleci-migrate-version.1",
	}
	for _, f := range wantMan {
		path := filepath.Join(manDir, f)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("expected man page %s not found: %v", f, err)
		}
	}

	// Markdown files
	wantMD := []string{
		"circleci-migrate.md",
		"circleci-migrate_export.md",
		"circleci-migrate_sync.md",
		"circleci-migrate_migrate.md",
		"circleci-migrate_version.md",
	}
	for _, f := range wantMD {
		path := filepath.Join(mdDir, f)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("expected markdown file %s not found: %v", f, err)
		}
	}
}

// TestGenDocsCommand_OutputMentionsDirs verifies that gen-docs writes the
// destination directories to stdout.
func TestGenDocsCommand_OutputMentionsDirs(t *testing.T) {
	tmp := t.TempDir()
	manDir := filepath.Join(tmp, "man")
	mdDir := filepath.Join(tmp, "md")

	stdout, _, err := runCmd(t, "gen-docs",
		"--man-dir", manDir,
		"--md-dir", mdDir,
	)
	if err != nil {
		t.Fatalf("gen-docs returned error: %v", err)
	}

	if !strings.Contains(stdout, manDir) {
		t.Errorf("stdout %q does not mention man-dir %q", stdout, manDir)
	}
	if !strings.Contains(stdout, mdDir) {
		t.Errorf("stdout %q does not mention md-dir %q", stdout, mdDir)
	}
}

// TestGenDocsCommand_IsHidden verifies that gen-docs does not appear in the
// root --help output (it is a developer-only hidden command).
func TestGenDocsCommand_IsHidden(t *testing.T) {
	stdout, _, err := runCmd(t, "--help")
	if err != nil {
		t.Fatalf("--help returned error: %v", err)
	}
	if strings.Contains(stdout, "gen-docs") {
		t.Errorf("gen-docs should be hidden from --help but appeared in: %q", stdout)
	}
}

// TestGenDocsCommand_Idempotent verifies that running gen-docs twice produces
// no diff (deterministic output).
func TestGenDocsCommand_Idempotent(t *testing.T) {
	tmp := t.TempDir()
	manDir := filepath.Join(tmp, "man")
	mdDir := filepath.Join(tmp, "md")

	// First run.
	if _, _, err := runCmd(t, "gen-docs", "--man-dir", manDir, "--md-dir", mdDir); err != nil {
		t.Fatalf("first gen-docs run: %v", err)
	}

	// Snapshot first-run output.
	snap1 := dirSnapshot(t, manDir)
	snap2 := dirSnapshot(t, mdDir)

	// Second run.
	if _, _, err := runCmd(t, "gen-docs", "--man-dir", manDir, "--md-dir", mdDir); err != nil {
		t.Fatalf("second gen-docs run: %v", err)
	}

	// Compare.
	for f, content := range snap1 {
		current, err := os.ReadFile(filepath.Join(manDir, f))
		if err != nil {
			t.Errorf("reading %s after second run: %v", f, err)
			continue
		}
		if string(current) != content {
			t.Errorf("man page %s changed between runs (not idempotent)", f)
		}
	}
	for f, content := range snap2 {
		current, err := os.ReadFile(filepath.Join(mdDir, f))
		if err != nil {
			t.Errorf("reading %s after second run: %v", f, err)
			continue
		}
		if string(current) != content {
			t.Errorf("markdown %s changed between runs (not idempotent)", f)
		}
	}
}

// dirSnapshot returns a map[filename → content] for every file directly in dir.
func dirSnapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading dir %s: %v", dir, err)
	}
	snap := make(map[string]string, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatalf("reading %s: %v", e.Name(), err)
		}
		snap[e.Name()] = string(data)
	}
	return snap
}
