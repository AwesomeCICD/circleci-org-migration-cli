package bundle_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filippo.io/age"
	"filippo.io/age/agessh"
	"golang.org/x/crypto/ssh"

	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/bundle"
)

// ─────────────────────────────────────────────────────────────────────────────
// helpers
// ─────────────────────────────────────────────────────────────────────────────

// generateSSHEd25519Keypair generates a fresh ed25519 SSH keypair and returns
// the private key PEM bytes and the authorized_keys-format public key string.
func generateSSHEd25519Keypair(t *testing.T) (privPEM []byte, pubKeyStr string) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate ed25519 keypair: %v", err)
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatalf("create ssh public key: %v", err)
	}
	pubKeyStr = strings.TrimSuffix(string(ssh.MarshalAuthorizedKey(sshPub)), "\n")

	privSSH, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatalf("create ssh signer: %v", err)
	}
	privMarshal, err := ssh.MarshalPrivateKey(privSSH.PublicKey(), "")
	if err != nil {
		// MarshalPrivateKey requires the crypto.Signer (ed25519.PrivateKey), not the public key.
		// Use the raw private key directly.
		_ = privSSH
		privPEMBlock, pemErr := ssh.MarshalPrivateKey(priv, "")
		if pemErr != nil {
			t.Fatalf("marshal private key: %v", pemErr)
		}
		return pem.EncodeToMemory(privPEMBlock), pubKeyStr
	}
	_ = privMarshal
	privPEMBlock, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		t.Fatalf("marshal private key pem: %v", err)
	}
	return pem.EncodeToMemory(privPEMBlock), pubKeyStr
}

// ─────────────────────────────────────────────────────────────────────────────
// ParseRecipient
// ─────────────────────────────────────────────────────────────────────────────

func TestParseRecipient_SSHEd25519(t *testing.T) {
	_, pubKeyStr := generateSSHEd25519Keypair(t)
	r, err := bundle.ParseRecipient(pubKeyStr)
	if err != nil {
		t.Fatalf("ParseRecipient(ssh-ed25519): %v", err)
	}
	if _, ok := r.(*agessh.Ed25519Recipient); !ok {
		t.Errorf("expected *agessh.Ed25519Recipient, got %T", r)
	}
}

func TestParseRecipient_AgeX25519(t *testing.T) {
	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("generate X25519 identity: %v", err)
	}
	recipientStr := id.Recipient().String()

	r, err := bundle.ParseRecipient(recipientStr)
	if err != nil {
		t.Fatalf("ParseRecipient(age1...): %v", err)
	}
	if _, ok := r.(*age.X25519Recipient); !ok {
		t.Errorf("expected *age.X25519Recipient, got %T", r)
	}
}

func TestParseRecipient_UnknownFormat(t *testing.T) {
	_, err := bundle.ParseRecipient("not-a-key")
	if err == nil {
		t.Fatal("expected error for unknown format, got nil")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ParseRecipientFile
// ─────────────────────────────────────────────────────────────────────────────

func TestParseRecipientFile_SSHPubKeyFile(t *testing.T) {
	_, pubKeyStr := generateSSHEd25519Keypair(t)
	dir := t.TempDir()
	pubPath := filepath.Join(dir, "id_ed25519.pub")
	if err := os.WriteFile(pubPath, []byte(pubKeyStr+"\n"), 0o600); err != nil {
		t.Fatalf("write pub key file: %v", err)
	}

	r, err := bundle.ParseRecipientFile(pubPath)
	if err != nil {
		t.Fatalf("ParseRecipientFile: %v", err)
	}
	if _, ok := r.(*agessh.Ed25519Recipient); !ok {
		t.Errorf("expected *agessh.Ed25519Recipient, got %T", r)
	}
}

func TestParseRecipientFile_AgePubFile(t *testing.T) {
	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("generate X25519 identity: %v", err)
	}
	dir := t.TempDir()
	pubPath := filepath.Join(dir, "recipient.txt")
	if err := os.WriteFile(pubPath, []byte(id.Recipient().String()+"\n"), 0o600); err != nil {
		t.Fatalf("write age pub file: %v", err)
	}

	r, err := bundle.ParseRecipientFile(pubPath)
	if err != nil {
		t.Fatalf("ParseRecipientFile: %v", err)
	}
	if _, ok := r.(*age.X25519Recipient); !ok {
		t.Errorf("expected *age.X25519Recipient, got %T", r)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Encrypt→Decrypt round-trips
// ─────────────────────────────────────────────────────────────────────────────

func TestEncryptDecrypt_SSHEd25519RoundTrip(t *testing.T) {
	privPEM, pubKeyStr := generateSSHEd25519Keypair(t)
	dir := t.TempDir()

	privPath := filepath.Join(dir, "id_ed25519")
	if err := os.WriteFile(privPath, privPEM, 0o600); err != nil {
		t.Fatalf("write private key: %v", err)
	}

	// Parse recipient from public key string.
	recipient, err := bundle.ParseRecipient(pubKeyStr)
	if err != nil {
		t.Fatalf("ParseRecipient: %v", err)
	}

	// Encrypt.
	plaintext := []byte(`{"context_secrets":{"ctx":{"VAR":"fake-secret-value-123"}}}`)
	ciphertext, err := bundle.EncryptBundle(plaintext, recipient)
	if err != nil {
		t.Fatalf("EncryptBundle: %v", err)
	}
	if len(ciphertext) == 0 {
		t.Fatal("ciphertext is empty")
	}
	if string(ciphertext) == string(plaintext) {
		t.Fatal("ciphertext is identical to plaintext — encryption did nothing")
	}

	// Decrypt.
	identities, err := bundle.ParseIdentityFile(privPath)
	if err != nil {
		t.Fatalf("ParseIdentityFile: %v", err)
	}
	recovered, err := bundle.DecryptBundle(ciphertext, identities...)
	if err != nil {
		t.Fatalf("DecryptBundle: %v", err)
	}
	if string(recovered) != string(plaintext) {
		t.Errorf("decrypted = %q, want %q", recovered, plaintext)
	}
}

func TestEncryptDecrypt_AgeX25519RoundTrip(t *testing.T) {
	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("generate X25519 identity: %v", err)
	}
	dir := t.TempDir()
	idPath := filepath.Join(dir, "identity.age")
	if err := os.WriteFile(idPath, []byte(id.String()+"\n"), 0o600); err != nil {
		t.Fatalf("write identity: %v", err)
	}

	recipient := id.Recipient()
	plaintext := []byte(`{"project_secrets":{"gh/acme/repo":{"DB_PASS":"fake-password-xyz"}}}`)

	ciphertext, err := bundle.EncryptBundle(plaintext, recipient)
	if err != nil {
		t.Fatalf("EncryptBundle: %v", err)
	}

	identities, err := bundle.ParseIdentityFile(idPath)
	if err != nil {
		t.Fatalf("ParseIdentityFile: %v", err)
	}
	recovered, err := bundle.DecryptBundle(ciphertext, identities...)
	if err != nil {
		t.Fatalf("DecryptBundle: %v", err)
	}
	if string(recovered) != string(plaintext) {
		t.Errorf("decrypted = %q, want %q", recovered, plaintext)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// DecryptBundle — wrong key must fail
// ─────────────────────────────────────────────────────────────────────────────

func TestDecryptBundle_WrongKey(t *testing.T) {
	id1, _ := age.GenerateX25519Identity()
	id2, _ := age.GenerateX25519Identity()

	plaintext := []byte(`{"context_secrets":{}}`)
	ciphertext, err := bundle.EncryptBundle(plaintext, id1.Recipient())
	if err != nil {
		t.Fatalf("EncryptBundle: %v", err)
	}

	_, err = bundle.DecryptBundle(ciphertext, id2)
	if err == nil {
		t.Fatal("expected error decrypting with wrong key, got nil")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// GenerateX25519Keypair
// ─────────────────────────────────────────────────────────────────────────────

func TestGenerateX25519Keypair(t *testing.T) {
	id, idStr, recipStr, err := bundle.GenerateX25519Keypair()
	if err != nil {
		t.Fatalf("GenerateX25519Keypair: %v", err)
	}
	if id == nil {
		t.Fatal("identity is nil")
	}
	if !strings.HasPrefix(idStr, "AGE-SECRET-KEY-1") {
		t.Errorf("identity string should start with AGE-SECRET-KEY-1, got %q", idStr)
	}
	if !strings.HasPrefix(recipStr, "age1") {
		t.Errorf("recipient string should start with age1, got %q", recipStr)
	}

	// Confirm round-trip with generated keypair.
	plaintext := []byte("test payload")
	r, err := bundle.ParseRecipient(recipStr)
	if err != nil {
		t.Fatalf("ParseRecipient from generated: %v", err)
	}
	ct, err := bundle.EncryptBundle(plaintext, r)
	if err != nil {
		t.Fatalf("EncryptBundle: %v", err)
	}
	pt, err := bundle.DecryptBundle(ct, id)
	if err != nil {
		t.Fatalf("DecryptBundle: %v", err)
	}
	if string(pt) != string(plaintext) {
		t.Errorf("round-trip failed: got %q", pt)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// EncryptBundle edge cases
// ─────────────────────────────────────────────────────────────────────────────

func TestEncryptBundle_NoRecipients(t *testing.T) {
	_, err := bundle.EncryptBundle([]byte("data"))
	if err == nil {
		t.Fatal("expected error with zero recipients, got nil")
	}
}
