// Encryption, storage, and key-file helpers for 'secrets capture'. Split out
// of secrets_capture.go to keep the command file focused on wiring; this is
// cmd-layer flag resolution (reads/writes key files, parses recipients).
package cmd

import (
	"errors"
	"fmt"
	"os"
	"strings"

	bundlepkg "github.com/AwesomeCICD/circleci-org-migration-cli/internal/bundle"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/capture"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/clog"
	"github.com/spf13/cobra"
)

// captureEncryptOpts groups the encryption-related flags for 'secrets capture'.
// They are resolved from flags by newSecretsCaptureCommand and threaded through
// to captureProject / CaptureWithDecrypt.
//
// NOTE: fields that need to be inspectable by CaptureWalkthroughResult (which
// is exported for tests) are also exported here.
type captureEncryptOpts struct {
	encrypt       bool   // Encrypt (exported via EncryptEnabled) — encrypt the artifact
	sshPublicKey  string // path to SSH public key file (recipient)
	sshPrivateKey string // path to SSH private key file (identity for local decrypt)
	generateKey   bool   // generate a fresh X25519 keypair and use it
	recipientStr  string // resolved public key string (after reading file / generating)
	identityFile  string // resolved private key/identity file path
	storage       string // "artifact" | "s3" | "both"
	s3Bucket      string
	s3Prefix      string
}

// toCaptureEncrypt converts the resolved encryption flags into the
// internal/capture EncryptOptions consumed by CaptureProject. Only the values
// needed to run an in-pipeline extraction are passed through; the cmd layer
// owns flag parsing and key-file resolution.
func (o captureEncryptOpts) toCaptureEncrypt() capture.EncryptOptions {
	return capture.EncryptOptions{
		Recipient:    o.recipientStr,
		IdentityFile: o.identityFile,
		Storage:      o.storage,
		S3Bucket:     o.s3Bucket,
		S3Prefix:     o.s3Prefix,
	}
}

// EncryptEnabled reports whether encryption is enabled.
func (o captureEncryptOpts) EncryptEnabled() bool { return o.encrypt }

// GenerateKey reports whether a fresh keypair should be generated.
func (o captureEncryptOpts) GenerateKey() bool { return o.generateKey }

// SSHPublicKey returns the path to the SSH public key file.
func (o captureEncryptOpts) SSHPublicKey() string { return o.sshPublicKey }

// SSHPrivateKey returns the path to the SSH private key file.
func (o captureEncryptOpts) SSHPrivateKey() string { return o.sshPrivateKey }

// Storage returns the storage mode string.
func (o captureEncryptOpts) Storage() string { return o.storage }

// S3Bucket returns the S3 bucket name.
func (o captureEncryptOpts) S3Bucket() string { return o.s3Bucket }

// S3Prefix returns the S3 key prefix.
func (o captureEncryptOpts) S3Prefix() string { return o.s3Prefix }

// resolveEncryptOpts resolves the encryption recipient string from flags:
//   - --generate-key:      generate a fresh age X25519 keypair, write files.
//   - --ssh-public-key:    read the public key from the file.
//
// On success encOpts.recipientStr and encOpts.identityFile are populated.
// SECURITY: private key material is written to file only; never logged.
func resolveEncryptOpts(cmd *cobra.Command, encOpts *captureEncryptOpts) error {
	if encOpts.generateKey && encOpts.sshPublicKey != "" {
		return errors.New("--generate-key and --ssh-public-key are mutually exclusive")
	}
	if !encOpts.generateKey && encOpts.sshPublicKey == "" {
		return errors.New("--encrypt requires --ssh-public-key <path> or --generate-key")
	}

	if encOpts.generateKey {
		// Generate a fresh age X25519 keypair.
		_, idStr, recipStr, err := bundlepkg.GenerateX25519Keypair()
		if err != nil {
			return fmt.Errorf("generating keypair: %w", err)
		}
		idFile := "migration-identity.age"
		recipFile := "migration-recipient.txt"
		// SECURITY: write identity (private key) to 0600 file; do not log.
		if err := writeSecretFile(idFile, idStr+"\n"); err != nil {
			return fmt.Errorf("writing identity file: %w", err)
		}
		if err := writeFile(recipFile, recipStr+"\n"); err != nil {
			return fmt.Errorf("writing recipient file: %w", err)
		}
		fmt.Fprintf(cmd.OutOrStdout(),
			"Generated age X25519 keypair:\n"+
				"  Identity (private key): %s  ← keep secret, needed for decrypt\n"+
				"  Recipient (public key): %s  ← safe to share\n",
			idFile, recipFile)
		encOpts.recipientStr = recipStr
		encOpts.identityFile = idFile
		return nil
	}

	// Read from --ssh-public-key file.
	_, parseErr := bundlepkg.ParseRecipientFile(encOpts.sshPublicKey)
	if parseErr != nil {
		return fmt.Errorf("parsing --ssh-public-key %s: %w", encOpts.sshPublicKey, parseErr)
	}
	// Read raw key string for embedding in config.
	data, err := readFirstKeyLine(encOpts.sshPublicKey)
	if err != nil {
		return fmt.Errorf("reading --ssh-public-key %s: %w", encOpts.sshPublicKey, err)
	}
	encOpts.recipientStr = data

	// Determine identity file for local decryption.
	if encOpts.sshPrivateKey != "" {
		encOpts.identityFile = encOpts.sshPrivateKey
	} else {
		// Default to ~/.ssh/id_ed25519 if it exists.
		home, herr := os.UserHomeDir()
		if herr == nil {
			candidate := home + "/.ssh/id_ed25519"
			if _, serr := os.Stat(candidate); serr == nil {
				encOpts.identityFile = candidate
				clog.Infof("capture: using default SSH identity %s for local decryption", candidate)
			}
		}
	}
	return nil
}

// validateStorageFlags returns an error if the storage-related flags are
// inconsistent (e.g. s3 storage requested but no bucket given).
func validateStorageFlags(encOpts *captureEncryptOpts) error {
	switch encOpts.storage {
	case "artifact", "":
		return nil
	case "s3", "both":
		if encOpts.s3Bucket == "" {
			return fmt.Errorf("--storage %s requires --s3-bucket", encOpts.storage)
		}
		return nil
	default:
		return fmt.Errorf("--storage must be one of: artifact, s3, both (got %q)", encOpts.storage)
	}
}

// writeSecretFile writes data to path with 0600 permissions (private key).
func writeSecretFile(path, data string) error {
	return os.WriteFile(path, []byte(data), 0o600)
}

// writeFile writes data to path with 0644 permissions (public key / recipient).
func writeFile(path, data string) error {
	// #nosec G306 -- recipient file is a PUBLIC age key; world-readable (0644) is intentional
	return os.WriteFile(path, []byte(data), 0o644)
}

// readFirstKeyLine returns the first non-empty, non-comment line from path —
// suitable for extracting the key string from a .pub or age recipients file.
func readFirstKeyLine(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		return line, nil
	}
	return "", fmt.Errorf("no key found in %s", path)
}

// newOrgClientForCapture creates an *org.Client for reading and writing org
