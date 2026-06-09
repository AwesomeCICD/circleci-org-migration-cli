package manifest

import (
	"encoding/json"
	"fmt"
	"os"
)

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
}

// NewSecretBundle returns an empty, initialized SecretBundle.
func NewSecretBundle() *SecretBundle {
	return &SecretBundle{
		SchemaVersion:  SchemaVersion,
		ContextSecrets: map[string]map[string]string{},
		ProjectSecrets: map[string]map[string]string{},
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

// Merge copies all context and project secrets from other into b. Later values
// win on key collisions. Used to combine the per-context bundles produced by
// separate extraction jobs into one.
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
}

// Save writes the bundle to path as indented JSON with 0600 permissions.
func (b *SecretBundle) Save(path string) error {
	if b.SchemaVersion == "" {
		b.SchemaVersion = SchemaVersion
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
