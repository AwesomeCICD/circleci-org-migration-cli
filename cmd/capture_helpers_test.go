// Internal (white-box) tests for unexported helpers in secrets_capture.go.
// Must use package cmd (not cmd_test) to access unexported symbols.
//
// NOTE: the capture orchestration helpers (parseOrgSlug, realRestrictions,
// prepareRestrictionRemoval, MaybeEnableOrgTriggerFlag, etc.) moved to
// internal/capture; their tests now live in that package
// (internal/capture/*_test.go). This file retains tests for the cmd-resident
// flag/file helpers only.
package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ─────────────────────────────────────────────────────────────────────────────
// readFirstKeyLine / writeFile / writeSecretFile
// ─────────────────────────────────────────────────────────────────────────────

func TestReadFirstKeyLine_ReturnsFirstNonComment(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keys.txt")
	content := "# comment line\n\nssh-ed25519 AAAAC3... user@host\nage1somekey\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	got, err := readFirstKeyLine(path)
	if err != nil {
		t.Fatalf("readFirstKeyLine: %v", err)
	}
	if !strings.HasPrefix(got, "ssh-ed25519") {
		t.Errorf("expected ssh-ed25519 line, got %q", got)
	}
}

func TestReadFirstKeyLine_EmptyFile_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.txt")
	if err := os.WriteFile(path, []byte("# only comments\n"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	_, err := readFirstKeyLine(path)
	if err == nil {
		t.Fatal("expected error for empty/comments-only file, got nil")
	}
}

func TestReadFirstKeyLine_MissingFile_ReturnsError(t *testing.T) {
	_, err := readFirstKeyLine("/nonexistent/path.pub")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestWriteSecretFile_CreatesFileWith0600(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secret.txt")
	if err := writeSecretFile(path, "AGE-SECRET-KEY-1fake\n"); err != nil {
		t.Fatalf("writeSecretFile: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("expected 0600, got %o", perm)
	}
}

func TestWriteFile_CreatesFileWith0644(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "recipient.txt")
	if err := writeFile(path, "age1fakepubkey\n"); err != nil {
		t.Fatalf("writeFile: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o644 {
		t.Errorf("expected 0644, got %o", perm)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// validateStorageFlags
// ─────────────────────────────────────────────────────────────────────────────

func TestValidateStorageFlags_Artifact_OK(t *testing.T) {
	if err := validateStorageFlags(&captureEncryptOpts{storage: "artifact"}); err != nil {
		t.Errorf("unexpected error for 'artifact': %v", err)
	}
}

func TestValidateStorageFlags_Empty_OK(t *testing.T) {
	if err := validateStorageFlags(&captureEncryptOpts{storage: ""}); err != nil {
		t.Errorf("unexpected error for empty storage: %v", err)
	}
}

func TestValidateStorageFlags_S3_WithBucket_OK(t *testing.T) {
	if err := validateStorageFlags(&captureEncryptOpts{storage: "s3", s3Bucket: "my-bucket"}); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateStorageFlags_S3_NoBucket_Error(t *testing.T) {
	err := validateStorageFlags(&captureEncryptOpts{storage: "s3"})
	if err == nil {
		t.Fatal("expected error for s3 without bucket, got nil")
	}
}

func TestValidateStorageFlags_Both_WithBucket_OK(t *testing.T) {
	if err := validateStorageFlags(&captureEncryptOpts{storage: "both", s3Bucket: "my-bucket"}); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateStorageFlags_Unknown_Error(t *testing.T) {
	err := validateStorageFlags(&captureEncryptOpts{storage: "invalid"})
	if err == nil {
		t.Fatal("expected error for unknown storage value, got nil")
	}
}
