package cmd

// Internal (white-box) tests for resolveEncryptOpts and the SSHPrivateKey
// accessor in secrets_capture_encrypt.go.

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filippo.io/age"
	"github.com/spf13/cobra"
)

// newDiscardCmd returns a cobra.Command whose stdout/stderr are discarded.
func newDiscardCmd() *cobra.Command {
	c := &cobra.Command{}
	c.SetOut(io.Discard)
	c.SetErr(io.Discard)
	return c
}

// TestResolveEncryptOpts_MutuallyExclusive covers the generate-key + ssh-key
// guard.
func TestResolveEncryptOpts_MutuallyExclusive(t *testing.T) {
	opts := &captureEncryptOpts{generateKey: true, sshPublicKey: "/tmp/k.pub"}
	if err := resolveEncryptOpts(newDiscardCmd(), opts); err == nil {
		t.Error("expected mutually-exclusive error")
	}
}

// TestResolveEncryptOpts_NeitherSet covers the "requires a key" guard.
func TestResolveEncryptOpts_NeitherSet(t *testing.T) {
	opts := &captureEncryptOpts{}
	if err := resolveEncryptOpts(newDiscardCmd(), opts); err == nil {
		t.Error("expected error when neither generate-key nor ssh-public-key set")
	}
}

// TestResolveEncryptOpts_GenerateKey covers the generate-key path, which writes
// the identity and recipient files into the working directory.
func TestResolveEncryptOpts_GenerateKey(t *testing.T) {
	dir := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if chErr := os.Chdir(dir); chErr != nil {
		t.Fatalf("chdir: %v", chErr)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	opts := &captureEncryptOpts{generateKey: true}
	if err := resolveEncryptOpts(newDiscardCmd(), opts); err != nil {
		t.Fatalf("resolveEncryptOpts: %v", err)
	}
	if opts.recipientStr == "" {
		t.Error("expected recipientStr to be populated")
	}
	if opts.identityFile != "migration-identity.age" {
		t.Errorf("identityFile = %q, want migration-identity.age", opts.identityFile)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "migration-identity.age")); statErr != nil {
		t.Errorf("identity file not written: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "migration-recipient.txt")); statErr != nil {
		t.Errorf("recipient file not written: %v", statErr)
	}
}

// TestResolveEncryptOpts_SSHPublicKey covers the read-from-public-key path with
// an explicit private key path (so the identityFile is set directly).
func TestResolveEncryptOpts_SSHPublicKey(t *testing.T) {
	dir := t.TempDir()
	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("generate identity: %v", err)
	}
	pubPath := filepath.Join(dir, "recipient.txt")
	if wErr := os.WriteFile(pubPath, []byte(id.Recipient().String()+"\n"), 0o644); wErr != nil {
		t.Fatalf("write recipient: %v", wErr)
	}
	privPath := filepath.Join(dir, "identity.age")
	if wErr := os.WriteFile(privPath, []byte(id.String()+"\n"), 0o600); wErr != nil {
		t.Fatalf("write identity: %v", wErr)
	}

	opts := &captureEncryptOpts{sshPublicKey: pubPath, sshPrivateKey: privPath}
	if err := resolveEncryptOpts(newDiscardCmd(), opts); err != nil {
		t.Fatalf("resolveEncryptOpts: %v", err)
	}
	if !strings.HasPrefix(opts.recipientStr, "age1") {
		t.Errorf("recipientStr = %q, want age1... prefix", opts.recipientStr)
	}
	if opts.identityFile != privPath {
		t.Errorf("identityFile = %q, want %q", opts.identityFile, privPath)
	}
	// Exercise the SSHPrivateKey accessor (0% before).
	if opts.SSHPrivateKey() != privPath {
		t.Errorf("SSHPrivateKey() = %q, want %q", opts.SSHPrivateKey(), privPath)
	}
}

// TestResolveEncryptOpts_SSHPublicKey_ParseError covers the parse-error branch
// when the public key file content is not a valid recipient.
func TestResolveEncryptOpts_SSHPublicKey_ParseError(t *testing.T) {
	dir := t.TempDir()
	pubPath := filepath.Join(dir, "bad.pub")
	if wErr := os.WriteFile(pubPath, []byte("not-a-valid-recipient\n"), 0o644); wErr != nil {
		t.Fatalf("write: %v", wErr)
	}
	opts := &captureEncryptOpts{sshPublicKey: pubPath}
	if err := resolveEncryptOpts(newDiscardCmd(), opts); err == nil {
		t.Error("expected parse error for invalid recipient file")
	}
}
