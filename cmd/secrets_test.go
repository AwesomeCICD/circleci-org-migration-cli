package cmd_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// writeManifest marshals m as indented JSON into dir/name and returns the path.
func writeManifest(t *testing.T, dir, name string, m *manifest.Manifest) string {
	t.Helper()
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	return path
}

// twoVarManifest returns a Manifest with a single context "demo" that has two
// env vars: FOO and BAR.
func twoVarManifest() *manifest.Manifest {
	return &manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		Contexts: []manifest.Context{
			{
				Name: "demo",
				EnvVars: []manifest.ContextEnvVar{
					{Name: "FOO"},
					{Name: "BAR"},
				},
			},
		},
	}
}

// ---------------------------------------------------------------------------
// Flag-validation errors (no --manifest)
// ---------------------------------------------------------------------------

// TestSecretsExtract_NoManifest verifies that running "secrets extract" with
// no --manifest flag returns an error mentioning "manifest".
func TestSecretsExtract_NoManifest(t *testing.T) {
	_, _, err := runCmd(t, "secrets", "extract")
	if err == nil {
		t.Fatal("expected error when --manifest is missing, got nil")
	}
	if !strings.Contains(err.Error(), "manifest") {
		t.Errorf("error %q does not mention 'manifest'", err.Error())
	}
}

// TestSecretsExtract_NeitherContextNorProject verifies that providing --manifest
// without specifying --context or --project returns an error.
func TestSecretsExtract_NeitherContextNorProject(t *testing.T) {
	dir := t.TempDir()
	mPath := writeManifest(t, dir, "manifest.json", twoVarManifest())

	_, _, err := runCmd(t, "secrets", "extract", "--manifest", mPath)
	if err == nil {
		t.Fatal("expected error when neither --context nor --project given, got nil")
	}
}

// TestSecretsExtract_BothContextAndProject verifies that providing both
// --context and --project returns an error (they are mutually exclusive).
func TestSecretsExtract_BothContextAndProject(t *testing.T) {
	dir := t.TempDir()
	mPath := writeManifest(t, dir, "manifest.json", twoVarManifest())

	_, _, err := runCmd(t, "secrets", "extract",
		"--manifest", mPath,
		"--context", "demo",
		"--project", "gh/acme/web",
	)
	if err == nil {
		t.Fatal("expected error when both --context and --project given, got nil")
	}
}

// ---------------------------------------------------------------------------
// Happy path
// ---------------------------------------------------------------------------

// TestSecretsExtract_HappyPath_ContextWithOneVar writes a manifest with a
// "demo" context (FOO, BAR), sets FOO in the environment, runs
// "secrets extract --context demo", and asserts:
//   - the output file exists and contains FOO's value
//   - the stderr warning about plaintext is printed
func TestSecretsExtract_HappyPath_ContextWithOneVar(t *testing.T) {
	dir := t.TempDir()
	mPath := writeManifest(t, dir, "manifest.json", twoVarManifest())
	outPath := filepath.Join(dir, "secrets.json")

	// Only FOO is present in the environment.
	t.Setenv("FOO", "super-secret")
	t.Setenv("BAR", "") // not set — unsetenv semantics via t.Setenv then unset below

	// BAR should be absent, not empty. Remove it to simulate a missing variable.
	os.Unsetenv("BAR")

	_, stderr, err := runCmd(t, "secrets", "extract",
		"--manifest", mPath,
		"--context", "demo",
		"-o", outPath,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The output file must exist.
	if _, statErr := os.Stat(outPath); statErr != nil {
		t.Fatalf("output file does not exist: %v", statErr)
	}

	// Load it back and verify FOO is captured.
	bundle, loadErr := manifest.LoadSecretBundle(outPath)
	if loadErr != nil {
		t.Fatalf("load secret bundle: %v", loadErr)
	}
	got, ok := bundle.ContextSecrets["demo"]["FOO"]
	if !ok {
		t.Fatal("bundle.ContextSecrets[\"demo\"][\"FOO\"] not set")
		return
	}
	if got != "super-secret" {
		t.Errorf("FOO = %q, want %q", got, "super-secret")
	}

	// The plaintext warning must appear on stderr.
	if !strings.Contains(stderr, "plaintext") {
		t.Errorf("stderr %q does not contain 'plaintext'", stderr)
	}
}

// ---------------------------------------------------------------------------
// --strict flag
// ---------------------------------------------------------------------------

// TestSecretsExtract_Strict_MissingVar verifies that --strict causes the
// command to return an error when a variable is absent from the environment.
func TestSecretsExtract_Strict_MissingVar(t *testing.T) {
	dir := t.TempDir()
	mPath := writeManifest(t, dir, "manifest.json", twoVarManifest())
	outPath := filepath.Join(dir, "secrets.json")

	// Neither FOO nor BAR is set.
	os.Unsetenv("FOO")
	os.Unsetenv("BAR")

	_, _, err := runCmd(t, "secrets", "extract",
		"--manifest", mPath,
		"--context", "demo",
		"-o", outPath,
		"--strict",
	)
	if err == nil {
		t.Fatal("expected error in --strict mode with missing vars, got nil")
	}
}

// ---------------------------------------------------------------------------
// Accumulation across two runs
// ---------------------------------------------------------------------------

// TestSecretsExtract_Accumulates verifies that running extract twice (for two
// different contexts) accumulates both into the same bundle without clobbering.
func TestSecretsExtract_Accumulates(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "secrets.json")

	// Manifest with two contexts.
	m := &manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		Contexts: []manifest.Context{
			{
				Name:    "ctx-alpha",
				EnvVars: []manifest.ContextEnvVar{{Name: "ALPHA_VAR"}},
			},
			{
				Name:    "ctx-beta",
				EnvVars: []manifest.ContextEnvVar{{Name: "BETA_VAR"}},
			},
		},
	}
	mPath := writeManifest(t, dir, "manifest.json", m)

	// First run — extract ctx-alpha.
	t.Setenv("ALPHA_VAR", "alpha-secret")
	os.Unsetenv("BETA_VAR")

	_, _, err := runCmd(t, "secrets", "extract",
		"--manifest", mPath,
		"--context", "ctx-alpha",
		"-o", outPath,
	)
	if err != nil {
		t.Fatalf("first extract error: %v", err)
	}

	// Second run — extract ctx-beta into the same file.
	os.Unsetenv("ALPHA_VAR")
	t.Setenv("BETA_VAR", "beta-secret")

	_, _, err = runCmd(t, "secrets", "extract",
		"--manifest", mPath,
		"--context", "ctx-beta",
		"-o", outPath,
	)
	if err != nil {
		t.Fatalf("second extract error: %v", err)
	}

	// Load the bundle and assert both contexts are present.
	bundle, loadErr := manifest.LoadSecretBundle(outPath)
	if loadErr != nil {
		t.Fatalf("load secret bundle: %v", loadErr)
	}

	if v, ok := bundle.ContextSecrets["ctx-alpha"]["ALPHA_VAR"]; !ok || v != "alpha-secret" {
		t.Errorf("ctx-alpha ALPHA_VAR = %q (ok=%v), want \"alpha-secret\" true", v, ok)
	}
	if v, ok := bundle.ContextSecrets["ctx-beta"]["BETA_VAR"]; !ok || v != "beta-secret" {
		t.Errorf("ctx-beta BETA_VAR = %q (ok=%v), want \"beta-secret\" true", v, ok)
	}
}
