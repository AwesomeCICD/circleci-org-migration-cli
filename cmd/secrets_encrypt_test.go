package cmd_test

// Tests for the new encryption / S3 / decrypt flags and commands added in
// feat(secrets): age/SSH encryption of the secret bundle + S3 storage option.
//
// These tests exercise:
//  1. secrets extract --encrypt writes a .age file and NO plaintext file.
//  2. secrets decrypt recovers the plaintext bundle from a .age file.
//  3. Round-trip: extract --encrypt → decrypt → load bundle.
//  4. secrets capture --encrypt and --storage flags are registered.
//  5. --generate-key flag is registered on capture.
//  6. --storage validation rejects unknown values and requires --s3-bucket.
//  7. bundle-encrypt hidden command works end-to-end.

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filippo.io/age"
	"golang.org/x/crypto/ssh"

	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
)

// ─────────────────────────────────────────────────────────────────────────────
// helpers
// ─────────────────────────────────────────────────────────────────────────────

// genSSHKeypairFiles generates an ed25519 SSH keypair and writes the private
// key to dir/id_ed25519 (0600) and the public key to dir/id_ed25519.pub.
// Returns the paths.
func genSSHKeypairFiles(t *testing.T, dir string) (privPath, pubPath string) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate ed25519: %v", err)
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatalf("ssh pub key: %v", err)
	}
	privPEMBlock, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		t.Fatalf("marshal private key: %v", err)
	}
	privPEM := pem.EncodeToMemory(privPEMBlock)
	pubStr := strings.TrimSuffix(string(ssh.MarshalAuthorizedKey(sshPub)), "\n")

	privPath = filepath.Join(dir, "id_ed25519")
	pubPath = filepath.Join(dir, "id_ed25519.pub")
	if err := os.WriteFile(privPath, privPEM, 0o600); err != nil {
		t.Fatalf("write priv key: %v", err)
	}
	if err := os.WriteFile(pubPath, []byte(pubStr+"\n"), 0o644); err != nil {
		t.Fatalf("write pub key: %v", err)
	}
	return privPath, pubPath
}

// genAgeKeypairFiles generates an age X25519 keypair and writes the identity
// to dir/identity.age (0600) and the recipient to dir/recipient.txt.
func genAgeKeypairFiles(t *testing.T, dir string) (idPath, recipPath string) {
	t.Helper()
	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("generate X25519 identity: %v", err)
	}
	idPath = filepath.Join(dir, "identity.age")
	recipPath = filepath.Join(dir, "recipient.txt")
	if err := os.WriteFile(idPath, []byte(id.String()+"\n"), 0o600); err != nil {
		t.Fatalf("write identity: %v", err)
	}
	if err := os.WriteFile(recipPath, []byte(id.Recipient().String()+"\n"), 0o644); err != nil {
		t.Fatalf("write recipient: %v", err)
	}
	return idPath, recipPath
}

// ─────────────────────────────────────────────────────────────────────────────
// secrets extract --encrypt
// ─────────────────────────────────────────────────────────────────────────────

func TestSecretsExtract_Encrypt_WritesAgeFile_NoPlaintext(t *testing.T) {
	dir := t.TempDir()
	mPath := writeManifest(t, dir, "manifest.json", twoVarManifest())
	_, pubPath := genSSHKeypairFiles(t, dir)

	outBase := filepath.Join(dir, "secrets.json")
	outAge := outBase + ".age"

	t.Setenv("FOO", "fake-secret-alpha")
	t.Setenv("BAR", "fake-secret-beta")

	stdout, stderr, err := runCmd(t, "secrets", "extract",
		"--manifest", mPath,
		"--context", "demo",
		"-o", outBase,
		"--encrypt",
		"--recipient-file", pubPath,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}

	// .age file must exist.
	if _, statErr := os.Stat(outAge); statErr != nil {
		t.Fatalf(".age file not created: %v", statErr)
	}
	// Plaintext .json must NOT exist.
	if _, statErr := os.Stat(outBase); statErr == nil {
		t.Fatal("plaintext .json file must not exist when --encrypt is set")
	}

	// .age file must not be parseable as JSON (it's binary age ciphertext).
	raw, _ := os.ReadFile(outAge)
	var dummy map[string]any
	if jsonErr := jsonUnmarshal(raw, &dummy); jsonErr == nil {
		t.Fatal(".age file should not be valid JSON")
	}

	// Stderr must NOT warn "plaintext" (the encrypted path has a different msg).
	if strings.Contains(stderr, "WARNING: "+outBase) {
		t.Errorf("stderr should not emit the plaintext warning for the JSON path: %s", stderr)
	}
}

func TestSecretsExtract_Encrypt_WithAgeRecipientFlag(t *testing.T) {
	dir := t.TempDir()
	mPath := writeManifest(t, dir, "manifest.json", twoVarManifest())
	idPath, recipPath := genAgeKeypairFiles(t, dir)
	_ = idPath

	outBase := filepath.Join(dir, "secrets.json")
	outAge := outBase + ".age"

	t.Setenv("FOO", "fake-val-1")
	t.Setenv("BAR", "fake-val-2")

	// Read recipient string to pass inline.
	recipStr, err := os.ReadFile(recipPath)
	if err != nil {
		t.Fatalf("read recipient: %v", err)
	}

	_, _, execErr := runCmd(t, "secrets", "extract",
		"--manifest", mPath,
		"--context", "demo",
		"-o", outBase,
		"--encrypt",
		"--recipient", strings.TrimSpace(string(recipStr)),
	)
	if execErr != nil {
		t.Fatalf("unexpected error: %v", execErr)
	}
	if _, statErr := os.Stat(outAge); statErr != nil {
		t.Fatalf(".age file not created: %v", statErr)
	}
}

func TestSecretsExtract_Encrypt_MissingRecipient_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	mPath := writeManifest(t, dir, "manifest.json", twoVarManifest())
	outBase := filepath.Join(dir, "secrets.json")

	t.Setenv("FOO", "fake-val")

	_, _, err := runCmd(t, "secrets", "extract",
		"--manifest", mPath,
		"--context", "demo",
		"-o", outBase,
		"--encrypt",
		// no --recipient or --recipient-file
	)
	if err == nil {
		t.Fatal("expected error when --encrypt set but no recipient provided, got nil")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "recipient") {
		t.Errorf("error %q should mention 'recipient'", err.Error())
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// secrets decrypt
// ─────────────────────────────────────────────────────────────────────────────

func TestSecretsDecrypt_SSHEd25519_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	mPath := writeManifest(t, dir, "manifest.json", twoVarManifest())
	privPath, pubPath := genSSHKeypairFiles(t, dir)

	outBase := filepath.Join(dir, "secrets.json")
	outAge := outBase + ".age"
	decrypted := filepath.Join(dir, "recovered.json")

	t.Setenv("FOO", "fake-secret-abc")
	t.Setenv("BAR", "fake-secret-xyz")

	// Extract with encryption.
	_, _, err := runCmd(t, "secrets", "extract",
		"--manifest", mPath,
		"--context", "demo",
		"-o", outBase,
		"--encrypt",
		"--recipient-file", pubPath,
	)
	if err != nil {
		t.Fatalf("extract --encrypt error: %v", err)
	}

	// Decrypt.
	stdout, stderr, err := runCmd(t, "secrets", "decrypt",
		"--identity-file", privPath,
		"-o", decrypted,
		outAge,
	)
	if err != nil {
		t.Fatalf("secrets decrypt error: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}

	// Load the decrypted bundle.
	bundle, loadErr := manifest.LoadSecretBundle(decrypted)
	if loadErr != nil {
		t.Fatalf("load recovered bundle: %v", loadErr)
	}

	if v, ok := bundle.ContextSecrets["demo"]["FOO"]; !ok || v != "fake-secret-abc" {
		t.Errorf("FOO = %q (ok=%v), want fake-secret-abc", v, ok)
	}
	if v, ok := bundle.ContextSecrets["demo"]["BAR"]; !ok || v != "fake-secret-xyz" {
		t.Errorf("BAR = %q (ok=%v), want fake-secret-xyz", v, ok)
	}
}

func TestSecretsDecrypt_AgeX25519_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	mPath := writeManifest(t, dir, "manifest.json", twoVarManifest())
	idPath, recipPath := genAgeKeypairFiles(t, dir)

	outBase := filepath.Join(dir, "secrets.json")
	outAge := outBase + ".age"
	decrypted := filepath.Join(dir, "recovered.json")

	t.Setenv("FOO", "fake-secret-age-1")
	t.Setenv("BAR", "fake-secret-age-2")

	// Extract with age X25519 encryption.
	_, _, err := runCmd(t, "secrets", "extract",
		"--manifest", mPath,
		"--context", "demo",
		"-o", outBase,
		"--encrypt",
		"--recipient-file", recipPath,
	)
	if err != nil {
		t.Fatalf("extract --encrypt error: %v", err)
	}

	// Decrypt.
	_, _, err = runCmd(t, "secrets", "decrypt",
		"--identity-file", idPath,
		"-o", decrypted,
		outAge,
	)
	if err != nil {
		t.Fatalf("secrets decrypt error: %v", err)
	}

	bundle, loadErr := manifest.LoadSecretBundle(decrypted)
	if loadErr != nil {
		t.Fatalf("load recovered bundle: %v", loadErr)
	}
	if v, ok := bundle.ContextSecrets["demo"]["FOO"]; !ok || v != "fake-secret-age-1" {
		t.Errorf("FOO = %q (ok=%v)", v, ok)
	}
}

func TestSecretsDecrypt_NoIdentityFile_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	_, _, err := runCmd(t, "secrets", "decrypt", filepath.Join(dir, "bundle.age"))
	if err == nil {
		t.Fatal("expected error when --identity-file is missing, got nil")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "identity") {
		t.Errorf("error %q should mention 'identity'", err.Error())
	}
}

func TestSecretsDecrypt_FlagsRegistered(t *testing.T) {
	root := MakeTestCommands()
	sub := findSubcommand(root, "secrets", "decrypt")
	if sub == nil {
		t.Fatal("'secrets decrypt' subcommand not found")
	}
	for _, name := range []string{"identity-file", "output"} {
		if sub.Flags().Lookup(name) == nil {
			t.Errorf("flag --%s not registered on 'secrets decrypt'", name)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// secrets capture — new encryption/storage flags registered
// ─────────────────────────────────────────────────────────────────────────────

func TestSecretsCapture_EncryptFlagsRegistered(t *testing.T) {
	root := MakeTestCommands()
	sub := findSubcommand(root, "secrets", "capture")
	if sub == nil {
		t.Fatal("'secrets capture' subcommand not found")
	}
	required := []string{
		"encrypt", "ssh-public-key", "ssh-private-key", "generate-key",
		"storage", "s3-bucket", "s3-prefix",
	}
	for _, name := range required {
		if sub.Flags().Lookup(name) == nil {
			t.Errorf("flag --%s not registered on 'secrets capture'", name)
		}
	}
}

func TestSecretsCapture_StorageValidation_UnknownValue(t *testing.T) {
	dir := t.TempDir()
	m := captureTestManifest()
	mPath := writeManifest(t, dir, "manifest.json", m)

	t.Setenv("CIRCLECI_CLI_TOKEN", "fake-token")

	_, _, err := runCmd(t, "secrets", "capture",
		"--manifest", mPath,
		"--storage", "invalid-storage",
	)
	if err == nil {
		t.Fatal("expected error for unknown --storage value, got nil")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "storage") {
		t.Errorf("error %q should mention 'storage'", err.Error())
	}
}

func TestSecretsCapture_StorageS3_RequiresBucket(t *testing.T) {
	dir := t.TempDir()
	m := captureTestManifest()
	mPath := writeManifest(t, dir, "manifest.json", m)

	t.Setenv("CIRCLECI_CLI_TOKEN", "fake-token")

	_, _, err := runCmd(t, "secrets", "capture",
		"--manifest", mPath,
		"--storage", "s3",
		// no --s3-bucket
	)
	if err == nil {
		t.Fatal("expected error when --storage s3 but no --s3-bucket, got nil")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "s3-bucket") {
		t.Errorf("error %q should mention 's3-bucket'", err.Error())
	}
}

func TestSecretsCapture_EncryptRequiresPublicKeyOrGenerate(t *testing.T) {
	dir := t.TempDir()
	m := captureTestManifest()
	mPath := writeManifest(t, dir, "manifest.json", m)

	t.Setenv("CIRCLECI_CLI_TOKEN", "fake-token")

	_, _, err := runCmd(t, "secrets", "capture",
		"--manifest", mPath,
		"--encrypt",
		// no --ssh-public-key or --generate-key
	)
	if err == nil {
		t.Fatal("expected error when --encrypt without key, got nil")
	}
}

func TestSecretsCapture_EncryptAndGenerate_MutuallyExclusive(t *testing.T) {
	dir := t.TempDir()
	m := captureTestManifest()
	mPath := writeManifest(t, dir, "manifest.json", m)
	_, pubPath := genSSHKeypairFiles(t, dir)

	t.Setenv("CIRCLECI_CLI_TOKEN", "fake-token")

	_, _, err := runCmd(t, "secrets", "capture",
		"--manifest", mPath,
		"--encrypt",
		"--ssh-public-key", pubPath,
		"--generate-key",
	)
	if err == nil {
		t.Fatal("expected error when --generate-key and --ssh-public-key both set, got nil")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// bundle-encrypt hidden command
// ─────────────────────────────────────────────────────────────────────────────

func TestBundleEncrypt_WritesAgeFile(t *testing.T) {
	dir := t.TempDir()
	idPath, recipPath := genAgeKeypairFiles(t, dir)

	// Read recipient string.
	recipBytes, err := os.ReadFile(recipPath)
	if err != nil {
		t.Fatalf("read recipient: %v", err)
	}
	recipStr := strings.TrimSpace(string(recipBytes))

	inputFile := filepath.Join(dir, "bundle.json")
	outputFile := filepath.Join(dir, "bundle.json.age")

	// Write a fake bundle JSON.
	if err := os.WriteFile(inputFile, []byte(`{"schema_version":"1","context_secrets":{"ctx":{"VAR":"fake-val"}}}`), 0o600); err != nil {
		t.Fatalf("write input: %v", err)
	}

	_, _, execErr := runCmd(t, "bundle-encrypt",
		"--recipient", recipStr,
		"--input", inputFile,
		"--output", outputFile,
	)
	if execErr != nil {
		t.Fatalf("bundle-encrypt error: %v", execErr)
	}

	// Output must exist and be a valid age ciphertext.
	raw, readErr := os.ReadFile(outputFile)
	if readErr != nil {
		t.Fatalf("output file not created: %v", readErr)
	}
	if len(raw) == 0 {
		t.Fatal("encrypted output is empty")
	}

	// Decrypt and verify.
	idBytes, _ := os.ReadFile(idPath)
	_ = idBytes

	// Use secrets decrypt to verify the round-trip.
	decOutput := filepath.Join(dir, "recovered.json")
	_, _, decErr := runCmd(t, "secrets", "decrypt",
		"--identity-file", idPath,
		"-o", decOutput,
		outputFile,
	)
	if decErr != nil {
		t.Fatalf("secrets decrypt after bundle-encrypt: %v", decErr)
	}
	bndl, loadErr := manifest.LoadSecretBundle(decOutput)
	if loadErr != nil {
		t.Fatalf("load recovered: %v", loadErr)
	}
	if v := bndl.ContextSecrets["ctx"]["VAR"]; v != "fake-val" {
		t.Errorf("VAR = %q, want fake-val", v)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// secrets merge --encrypt
// ─────────────────────────────────────────────────────────────────────────────

// jsonUnmarshal is a thin wrapper used in tests to check whether bytes are
// valid JSON without importing encoding/json directly at each call site.
func jsonUnmarshal(data []byte, v any) error {
	return json.Unmarshal(data, v)
}

func TestSecretsMerge_Encrypt_WritesAgeFile(t *testing.T) {
	dir := t.TempDir()
	_, pubPath := genSSHKeypairFiles(t, dir)
	privPath := filepath.Join(dir, "id_ed25519")

	// Create two small bundles to merge.
	b1 := manifest.NewSecretBundle()
	b1.SetContextSecret("ctx1", "VAR1", "fake-value-1")
	if err := b1.Save(filepath.Join(dir, "b1.json")); err != nil {
		t.Fatalf("save b1: %v", err)
	}
	b2 := manifest.NewSecretBundle()
	b2.SetContextSecret("ctx2", "VAR2", "fake-value-2")
	if err := b2.Save(filepath.Join(dir, "b2.json")); err != nil {
		t.Fatalf("save b2: %v", err)
	}

	outBase := filepath.Join(dir, "merged.json")
	outAge := outBase + ".age"

	_, _, err := runCmd(t, "secrets", "merge",
		"-o", outBase,
		"--encrypt",
		"--recipient-file", pubPath,
		filepath.Join(dir, "b1.json"),
		filepath.Join(dir, "b2.json"),
	)
	if err != nil {
		t.Fatalf("secrets merge --encrypt error: %v", err)
	}

	if _, statErr := os.Stat(outAge); statErr != nil {
		t.Fatalf(".age output not found: %v", statErr)
	}
	if _, statErr := os.Stat(outBase); statErr == nil {
		t.Fatal("plaintext .json should not exist when --encrypt is set")
	}

	// Decrypt and verify both contexts are present.
	recovered := filepath.Join(dir, "recovered.json")
	_, _, decErr := runCmd(t, "secrets", "decrypt",
		"--identity-file", privPath,
		"-o", recovered,
		outAge,
	)
	if decErr != nil {
		t.Fatalf("decrypt merged: %v", decErr)
	}
	bndl, loadErr := manifest.LoadSecretBundle(recovered)
	if loadErr != nil {
		t.Fatalf("load merged: %v", loadErr)
	}
	if bndl.ContextSecrets["ctx1"]["VAR1"] != "fake-value-1" {
		t.Error("ctx1/VAR1 missing from merged bundle")
	}
	if bndl.ContextSecrets["ctx2"]["VAR2"] != "fake-value-2" {
		t.Error("ctx2/VAR2 missing from merged bundle")
	}
}
