package manifest

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// BundleSSHKey holds the private-key material for one additional SSH key on
// a project, captured via an in-pipeline extraction job. This is the ONLY
// place in the tool where a private key value is ever stored — the manifest
// itself only captures the public metadata.
//
// SECURITY: PrivateKey is a plaintext PEM private key. Protect the bundle.
type BundleSSHKey struct {
	// Fingerprint is the SHA256 fingerprint (without the "SHA256:" prefix)
	// that uniquely identifies this key, matching the manifest ProjectSSHKey.
	Fingerprint string `json:"fingerprint"`
	// Hostname is the target host this key is scoped to (may be empty for
	// global additional SSH keys). Matched from the manifest catalog.
	Hostname string `json:"hostname,omitempty"`
	// PrivateKey is the plaintext PEM private-key material captured from the
	// in-pipeline extraction job.
	PrivateKey string `json:"private_key"`
}

// SecretBundle holds the plaintext environment-variable values that CircleCI
// will not return via API. It is produced only by the in-pipeline `secrets`
// command, which reads the values from the live job environment.
//
// SECURITY: this file contains plaintext secrets. It is written with 0600
// permissions and must never be committed or shared. Keep it separate from
// the manifest (which is safe to share) and delete it once the sync is done.
type SecretBundle struct {
	SchemaVersion string `json:"schema_version"`
	GeneratedAt   string `json:"generated_at,omitempty"`
	ToolVersion   string `json:"tool_version,omitempty"`

	// ContextSecrets maps context name -> variable name -> value.
	ContextSecrets map[string]map[string]string `json:"context_secrets,omitempty"`
	// ProjectSecrets maps project slug -> variable name -> value.
	ProjectSecrets map[string]map[string]string `json:"project_secrets,omitempty"`
	// SSHKeys maps project slug -> list of captured SSH private keys.
	// Keys are captured via an in-pipeline extraction job (add_ssh_keys step)
	// that materialises private-key files and reads them. Only keys whose
	// SHA256 fingerprint matches a cataloged ProjectSSHKey are captured.
	SSHKeys map[string][]BundleSSHKey `json:"ssh_keys,omitempty"`
}

// NewSecretBundle returns an empty, initialized SecretBundle.
func NewSecretBundle() *SecretBundle {
	return &SecretBundle{
		SchemaVersion:  SchemaVersion,
		ContextSecrets: map[string]map[string]string{},
		ProjectSecrets: map[string]map[string]string{},
		SSHKeys:        map[string][]BundleSSHKey{},
	}
}

// SetContextSecret records a context variable's value.
func (b *SecretBundle) SetContextSecret(context, name, value string) {
	if b.ContextSecrets == nil {
		b.ContextSecrets = map[string]map[string]string{}
	}
	if b.ContextSecrets[context] == nil {
		b.ContextSecrets[context] = map[string]string{}
	}
	b.ContextSecrets[context][name] = value
}

// SetProjectSecret records a project variable's value.
func (b *SecretBundle) SetProjectSecret(projectSlug, name, value string) {
	if b.ProjectSecrets == nil {
		b.ProjectSecrets = map[string]map[string]string{}
	}
	if b.ProjectSecrets[projectSlug] == nil {
		b.ProjectSecrets[projectSlug] = map[string]string{}
	}
	b.ProjectSecrets[projectSlug][name] = value
}

// AddSSHKey appends a captured SSH private key for projectSlug.
// Duplicate fingerprints (same project + fingerprint) are silently skipped so
// that re-running capture is idempotent.
//
// SECURITY: key.PrivateKey is plaintext. Protect the bundle file.
func (b *SecretBundle) AddSSHKey(projectSlug string, key BundleSSHKey) {
	if b.SSHKeys == nil {
		b.SSHKeys = map[string][]BundleSSHKey{}
	}
	// Idempotency: skip if a key with the same fingerprint already exists.
	for _, existing := range b.SSHKeys[projectSlug] {
		if existing.Fingerprint == key.Fingerprint {
			return
		}
	}
	b.SSHKeys[projectSlug] = append(b.SSHKeys[projectSlug], key)
}

// Merge copies all context, project secrets, and SSH keys from other into b.
// Later values win on key collisions for env vars. SSH keys are merged
// idempotently via AddSSHKey (duplicate fingerprints per project are skipped).
// Used to combine the per-context bundles produced by separate extraction jobs.
func (b *SecretBundle) Merge(other *SecretBundle) {
	if other == nil {
		return
	}
	for ctx, vars := range other.ContextSecrets {
		for name, val := range vars {
			b.SetContextSecret(ctx, name, val)
		}
	}
	for slug, vars := range other.ProjectSecrets {
		for name, val := range vars {
			b.SetProjectSecret(slug, name, val)
		}
	}
	for slug, keys := range other.SSHKeys {
		for _, k := range keys {
			b.AddSSHKey(slug, k)
		}
	}
}

// Save writes the bundle to path as indented JSON with 0600 permissions.
// It creates the parent directory (and any missing ancestors) if it does not
// already exist, so callers can safely write to paths like "captured/foo.json"
// without pre-creating the directory.
func (b *SecretBundle) Save(path string) error {
	if b.SchemaVersion == "" {
		b.SchemaVersion = SchemaVersion
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil { //nolint:gosec // G301: secret dirs must be owner-only (0700)
			return fmt.Errorf("creating output directory %s: %w", dir, err)
		}
	}
	return writeJSON(path, b, 0o600)
}

// LoadSecretBundle reads and validates a secret bundle from path.
func LoadSecretBundle(path string) (*SecretBundle, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var b SecretBundle
	if err := json.Unmarshal(data, &b); err != nil {
		return nil, fmt.Errorf("parsing secret bundle %s: %w", path, err)
	}
	if b.SchemaVersion != SchemaVersion {
		return nil, fmt.Errorf("secret bundle %s has unsupported schema version %q (this build supports %q)", path, b.SchemaVersion, SchemaVersion)
	}
	return &b, nil
}
