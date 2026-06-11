package manifest

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// BundleSSHKey holds the private key material for one additional SSH key on a
// project. It is stored in the SecretBundle and consumed by the syncer when
// re-adding additional SSH keys to the destination project.
//
// SECURITY: PrivateKey is the plaintext PEM-encoded private key. The bundle
// that contains this struct must be treated with the same care as any other
// plaintext secret — written with 0600 permissions, never committed or shared,
// and deleted once the sync is complete.
type BundleSSHKey struct {
	// Fingerprint is the SHA256 fingerprint of the key (e.g. "SHA256:abc123…").
	// It is used to match this BundleSSHKey to the corresponding ProjectSSHKey
	// entry in the manifest and to detect idempotent re-adds on the destination.
	Fingerprint string `json:"fingerprint"`
	// Hostname is the hostname this key is scoped to (e.g. "github.com").
	// May be empty when the key is not scoped to a specific host.
	Hostname string `json:"hostname,omitempty"`
	// PrivateKey is the plaintext PEM-encoded private SSH key. This field is the
	// only reason the bundle must be protected — the manifest stores only the
	// public key and fingerprint.
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
	// SSHKeys maps project slug -> list of BundleSSHKey (one per additional SSH
	// key on that project). Keyed by the source project slug (same as
	// ProjectSecrets). This section is produced by a future Part 3 extraction
	// step; the syncer consumes it to re-add additional SSH keys at the
	// destination. Nil/empty bundles produced before Part 3 remain valid — the
	// section is omitempty and its absence is handled gracefully by the syncer.
	SSHKeys map[string][]BundleSSHKey `json:"ssh_keys,omitempty"`
}

// NewSecretBundle returns an empty, initialized SecretBundle.
func NewSecretBundle() *SecretBundle {
	return &SecretBundle{
		SchemaVersion:  SchemaVersion,
		ContextSecrets: map[string]map[string]string{},
		ProjectSecrets: map[string]map[string]string{},
	}
}

// AddSSHKey records an additional SSH key's private material for the given
// project slug. If a key with the same Fingerprint already exists for the slug,
// it is replaced (idempotent upsert).
func (b *SecretBundle) AddSSHKey(projectSlug string, key BundleSSHKey) {
	if b.SSHKeys == nil {
		b.SSHKeys = map[string][]BundleSSHKey{}
	}
	keys := b.SSHKeys[projectSlug]
	for i, k := range keys {
		if k.Fingerprint == key.Fingerprint {
			keys[i] = key
			b.SSHKeys[projectSlug] = keys
			return
		}
	}
	b.SSHKeys[projectSlug] = append(keys, key)
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

// Merge copies all context secrets, project secrets, and SSH keys from other
// into b. Later values win on key collisions (for env vars: last writer wins;
// for SSH keys: keys with the same fingerprint are replaced). Used to combine
// the per-context bundles produced by separate extraction jobs into one.
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
