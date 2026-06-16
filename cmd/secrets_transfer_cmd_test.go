package cmd_test

// secrets_transfer_cmd_test.go — black-box tests for 'secrets transfer' CLI
// flag validation (Fix 1: --dest-org / --dest-org-id mutual exclusion).
//
// These tests exercise the command through runCmd (which invokes MakeCommands())
// and do NOT require network access.

import (
	"os"
	"strings"
	"testing"
)

// minimalTransferManifestJSON is a small but valid manifest for flag-validation
// tests that need a real --manifest file.
var minimalTransferManifestJSON = []byte(`{
  "schema_version": "1",
  "source": {
    "host": "https://circleci.com",
    "org": {"slug": "gh/old-org", "name": "old-org"}
  },
  "contexts": [],
  "projects": []
}`)

// writeMinimalManifest creates a minimal manifest at a temp path and returns
// the path.
func writeMinimalManifest(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	path := tmp + "/manifest.json"
	if err := os.WriteFile(path, minimalTransferManifestJSON, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	return path
}

// TestSecretsTransfer_MissingManifest verifies that omitting --manifest
// returns an error mentioning "manifest".
func TestSecretsTransfer_MissingManifest(t *testing.T) {
	t.Setenv("CIRCLECI_CLI_TOKEN", "")
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "")
	t.Setenv("CIRCLE_TOKEN", "")
	_, _, err := runCmd(t,
		"secrets", "transfer",
		"--dest-org", "gh/dest-org",
		"--dest-token-context", "migration-secrets",
	)
	if err == nil {
		t.Fatal("expected error when --manifest is missing")
	}
	if !strings.Contains(err.Error(), "manifest") {
		t.Errorf("error should mention 'manifest'; got: %v", err)
	}
}

// TestSecretsTransfer_NeitherDestOrgNorDestOrgID verifies that omitting both
// --dest-org and --dest-org-id returns a clear error.
func TestSecretsTransfer_NeitherDestOrgNorDestOrgID(t *testing.T) {
	t.Setenv("CIRCLECI_CLI_TOKEN", "")
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "")
	t.Setenv("CIRCLE_TOKEN", "")
	mPath := writeMinimalManifest(t)

	_, _, err := runCmd(t,
		"secrets", "transfer",
		"--manifest", mPath,
		"--dest-token-context", "migration-secrets",
	)
	if err == nil {
		t.Fatal("expected error when neither --dest-org nor --dest-org-id is provided")
	}
	if !strings.Contains(err.Error(), "dest-org") {
		t.Errorf("error should mention 'dest-org'; got: %v", err)
	}
}

// TestSecretsTransfer_MissingDestTokenContext verifies that omitting
// --dest-token-context returns a clear error.
func TestSecretsTransfer_MissingDestTokenContext(t *testing.T) {
	t.Setenv("CIRCLECI_CLI_TOKEN", "")
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "")
	t.Setenv("CIRCLE_TOKEN", "")
	mPath := writeMinimalManifest(t)

	_, _, err := runCmd(t,
		"secrets", "transfer",
		"--manifest", mPath,
		"--dest-org", "gh/dest-org",
	)
	if err == nil {
		t.Fatal("expected error when --dest-token-context is missing")
	}
	if !strings.Contains(err.Error(), "dest-token-context") {
		t.Errorf("error should mention 'dest-token-context'; got: %v", err)
	}
}

// TestSecretsTransfer_HelpWorks verifies that 'secrets transfer --help' exits
// 0 and mentions both --dest-org and --dest-org-id flags.
func TestSecretsTransfer_HelpWorks(t *testing.T) {
	out, _, err := runCmd(t, "secrets", "transfer", "--help")
	if err != nil {
		t.Fatalf("secrets transfer --help: %v", err)
	}
	for _, phrase := range []string{"--dest-org", "--dest-org-id", "--dest-token-context", "--enable-trigger"} {
		if !strings.Contains(out, phrase) {
			t.Errorf("help output missing %q:\n%s", phrase, out)
		}
	}
}
