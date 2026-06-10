// Package bundle provides age-based encryption and decryption of secret
// bundles. It intentionally uses the vetted filippo.io/age library — no
// custom crypto. Secret values and private key material are NEVER logged.
package bundle

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"

	"filippo.io/age"
	"filippo.io/age/agessh"

	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/clog"
)

// ParseRecipient parses an age recipient from one of three accepted forms:
//
//  1. An SSH public-key string beginning with "ssh-ed25519" or "ssh-rsa"
//     (passed via --recipient, e.g. from $HOME/.ssh/id_ed25519.pub content).
//  2. An age X25519 recipient string beginning with "age1".
//  3. The path to a file containing either of the above (--recipient-file).
//
// The function reads the file if input looks like a path (does not start with
// a known key prefix). The caller should prefer ParseRecipientFile for file
// paths to make intent explicit.
//
// SECURITY: the parsed recipient is a PUBLIC key — safe to log its type.
// We never log the raw key material.
func ParseRecipient(recipientStr string) (age.Recipient, error) {
	recipientStr = strings.TrimSpace(recipientStr)
	if strings.HasPrefix(recipientStr, "ssh-") {
		r, err := agessh.ParseRecipient(recipientStr)
		if err != nil {
			return nil, fmt.Errorf("parse SSH recipient: %w", err)
		}
		clog.Debugf("bundle: parsed SSH recipient type=%T", r)
		return r, nil
	}
	if strings.HasPrefix(recipientStr, "age1") {
		rs, err := age.ParseRecipients(strings.NewReader(recipientStr))
		if err != nil {
			return nil, fmt.Errorf("parse age X25519 recipient: %w", err)
		}
		if len(rs) == 0 {
			return nil, fmt.Errorf("no valid recipient found in string")
		}
		clog.Debugf("bundle: parsed age X25519 recipient")
		return rs[0], nil
	}
	return nil, fmt.Errorf("unrecognised recipient format (expected ssh-ed25519/ssh-rsa/age1...): %q", recipientStr)
}

// ParseRecipientFile reads the contents of path and delegates to ParseRecipient.
// Accepts an SSH public-key file (.pub) or an age recipients file.
func ParseRecipientFile(path string) (age.Recipient, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading recipient file %s: %w", path, err)
	}
	// Strip comment lines (lines starting with #) for age recipients files.
	var lines []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return nil, fmt.Errorf("recipient file %s is empty or contains only comments", path)
	}
	return ParseRecipient(lines[0])
}

// EncryptBundle encrypts the JSON payload with the given recipients and returns
// the binary age ciphertext. Binary (non-armored) format is used for compactness.
//
// SECURITY: plaintext is held only in memory; it is never written to disk.
func EncryptBundle(plaintext []byte, recipients ...age.Recipient) ([]byte, error) {
	if len(recipients) == 0 {
		return nil, fmt.Errorf("EncryptBundle: at least one recipient is required")
	}
	var buf bytes.Buffer
	w, err := age.Encrypt(&buf, recipients...)
	if err != nil {
		return nil, fmt.Errorf("EncryptBundle: age.Encrypt: %w", err)
	}
	if _, err := w.Write(plaintext); err != nil {
		return nil, fmt.Errorf("EncryptBundle: write plaintext: %w", err)
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("EncryptBundle: finalise ciphertext: %w", err)
	}
	clog.Debugf("bundle: encrypted %d plaintext bytes → %d ciphertext bytes", len(plaintext), buf.Len())
	return buf.Bytes(), nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Identities (decryption)
// ─────────────────────────────────────────────────────────────────────────────

// ParseIdentityFile reads one or more age/SSH identities from the file at path.
// Accepted formats:
//   - An SSH private key file (OpenSSH format, ed25519 or RSA) — parsed by
//     agessh.ParseIdentity. The function handles passphrase prompts by
//     returning an agessh.EncryptedSSHIdentity; callers that need passphrase
//     support should use ParseIdentityFileWithPassphrase.
//   - An age identity file (lines beginning with "AGE-SECRET-KEY-1") — parsed
//     by age.ParseIdentities.
//
// SECURITY: private key bytes are read into memory only for the duration of
// this call and are never logged.
func ParseIdentityFile(path string) ([]age.Identity, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading identity file %s: %w", path, err)
	}

	// Heuristic: if the file starts with "-----BEGIN OPENSSH PRIVATE KEY-----"
	// or is recognised as an SSH private key, use agessh.ParseIdentity.
	trimmed := strings.TrimSpace(string(data))
	if strings.HasPrefix(trimmed, "-----") {
		id, parseErr := agessh.ParseIdentity(data)
		if parseErr != nil {
			return nil, fmt.Errorf("parsing SSH identity from %s: %w", path, parseErr)
		}
		clog.Debugf("bundle: parsed SSH identity from %s", path)
		return []age.Identity{id}, nil
	}

	// Otherwise treat it as an age identity file.
	ids, parseErr := age.ParseIdentities(bytes.NewReader(data))
	if parseErr != nil {
		return nil, fmt.Errorf("parsing age identities from %s: %w", path, parseErr)
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("no identities found in %s", path)
	}
	clog.Debugf("bundle: parsed %d age identity/identities from %s", len(ids), path)
	return ids, nil
}

// DecryptBundle decrypts an age-encrypted ciphertext using the supplied
// identities and returns the plaintext bytes.
//
// SECURITY: the returned plaintext contains secret values — the caller must
// never log or print it. Private key material is never logged here.
func DecryptBundle(ciphertext []byte, identities ...age.Identity) ([]byte, error) {
	if len(identities) == 0 {
		return nil, fmt.Errorf("DecryptBundle: at least one identity is required")
	}
	r, err := age.Decrypt(bytes.NewReader(ciphertext), identities...)
	if err != nil {
		return nil, fmt.Errorf("DecryptBundle: age.Decrypt: %w", err)
	}
	plaintext, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("DecryptBundle: read plaintext: %w", err)
	}
	clog.Debugf("bundle: decrypted %d ciphertext bytes → %d plaintext bytes", len(ciphertext), len(plaintext))
	return plaintext, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Key generation
// ─────────────────────────────────────────────────────────────────────────────

// GenerateX25519Keypair generates an age X25519 keypair.
// Returns the identity (private) and recipient (public) objects, plus the
// string encodings suitable for writing to files.
//
// SECURITY: the identity string contains the private key — write it to a
// 0600 file and never log it.
func GenerateX25519Keypair() (identity *age.X25519Identity, identityStr, recipientStr string, err error) {
	id, err := age.GenerateX25519Identity()
	if err != nil {
		return nil, "", "", fmt.Errorf("generate X25519 keypair: %w", err)
	}
	return id, id.String(), id.Recipient().String(), nil
}
