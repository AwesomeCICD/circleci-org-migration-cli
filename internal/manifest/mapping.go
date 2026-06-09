package manifest

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// Mapping describes how source identities map onto the destination org during
// `sync`. It is optional input: when omitted, sync defaults to identity
// mapping (same names), which is correct for a straight org rename within the
// same VCS.
type Mapping struct {
	SchemaVersion string `json:"schema_version,omitempty"`

	Org OrgMapping `json:"org"`
	// Projects maps a source project slug to a destination project slug. Use
	// this for renames, or when the destination is a GitHub App org (where the
	// slug is "circleci/<org-id>/<project-id>" and cannot be derived from the
	// source repo name).
	Projects map[string]string `json:"projects,omitempty"`
}

// OrgMapping maps the source org slug to the destination org slug.
type OrgMapping struct {
	From string `json:"from"` // e.g. "gh/acme"
	To   string `json:"to"`   // e.g. "gh/acme-new" or "circleci/<org-id>"
}

// LoadMapping reads a mapping file from path.
func LoadMapping(path string) (*Mapping, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m Mapping
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parsing mapping %s: %w", path, err)
	}
	if m.SchemaVersion != "" && m.SchemaVersion != SchemaVersion {
		return nil, fmt.Errorf("mapping %s has unsupported schema version %q (this build supports %q)", path, m.SchemaVersion, SchemaVersion)
	}
	return &m, nil
}

// IdentityMapping returns a Mapping that maps an org onto itself, used when no
// mapping file is supplied.
func IdentityMapping(orgSlug string) *Mapping {
	return &Mapping{Org: OrgMapping{From: orgSlug, To: orgSlug}}
}

// ResolveProjectSlug returns the destination slug for a source project slug.
//
// An explicit entry in Projects always wins. Otherwise it attempts an identity
// transform by swapping the org portion of the slug (From -> To), which is
// valid only when the destination is a slug-style org (e.g. "gh/<org>"). When
// the destination is a GitHub App org ("circleci/<org-id>") the project ID
// cannot be derived, so resolution fails and ok is false — the caller should
// require an explicit mapping and warn.
func (m *Mapping) ResolveProjectSlug(sourceSlug string) (slug string, ok bool) {
	if dst, found := m.Projects[sourceSlug]; found {
		return dst, true
	}

	from, to := m.Org.From, m.Org.To
	if from == "" || to == "" {
		return "", false
	}

	prefix := from + "/"
	if !strings.HasPrefix(sourceSlug, prefix) {
		return "", false
	}
	// GitHub App destination slugs need a project ID we don't have here.
	if strings.HasPrefix(to, "circleci/") {
		return "", false
	}
	return to + "/" + strings.TrimPrefix(sourceSlug, prefix), true
}

// Save writes the mapping to path as indented JSON.
func (m *Mapping) Save(path string) error {
	if m.SchemaVersion == "" {
		m.SchemaVersion = SchemaVersion
	}
	return writeJSON(path, m, 0o644)
}
