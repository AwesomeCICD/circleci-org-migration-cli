package manifest

// Tests for manifest/mapping/secret-bundle load+save round-trips, warning
// accumulation, and repo-owner remapping. Existing tests live in
// manifest_test.go; this file only adds NEW test names.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// AddWarning
// ---------------------------------------------------------------------------

func TestAddWarning_AppendsFields(t *testing.T) {
	m := &Manifest{}
	m.AddWarning("context:deploy", "value_unavailable", "no secret values")
	if len(m.Warnings) != 1 {
		t.Fatalf("len(Warnings) = %d; want 1", len(m.Warnings))
	}
	w := m.Warnings[0]
	if w.Scope != "context:deploy" {
		t.Errorf("Scope = %q; want %q", w.Scope, "context:deploy")
	}
	if w.Code != "value_unavailable" {
		t.Errorf("Code = %q; want %q", w.Code, "value_unavailable")
	}
	if w.Message != "no secret values" {
		t.Errorf("Message = %q; want %q", w.Message, "no secret values")
	}
}

func TestAddWarning_MultipleWarnings(t *testing.T) {
	m := &Manifest{}
	m.AddWarning("org", "code1", "msg1")
	m.AddWarning("project:web", "code2", "msg2")
	m.AddWarning("context:ci", "code3", "msg3")
	if len(m.Warnings) != 3 {
		t.Fatalf("len(Warnings) = %d; want 3", len(m.Warnings))
	}
}

// ---------------------------------------------------------------------------
// Mapping.Save + LoadMapping round-trip
// ---------------------------------------------------------------------------

func TestMappingSaveAndLoad_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mapping.json")

	in := &Mapping{
		Org: OrgMapping{From: "gh/acme", To: "gh/acme-new"},
		Projects: map[string]string{
			"gh/acme/web": "gh/acme-new/web",
		},
	}
	if err := in.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	out, err := LoadMapping(path)
	if err != nil {
		t.Fatalf("LoadMapping: %v", err)
	}
	if out.SchemaVersion != SchemaVersion {
		t.Errorf("SchemaVersion = %q; want %q", out.SchemaVersion, SchemaVersion)
	}
	if out.Org.From != "gh/acme" || out.Org.To != "gh/acme-new" {
		t.Errorf("Org = %+v; want {gh/acme gh/acme-new}", out.Org)
	}
	if got := out.Projects["gh/acme/web"]; got != "gh/acme-new/web" {
		t.Errorf("Projects[gh/acme/web] = %q; want %q", got, "gh/acme-new/web")
	}
}

func TestMappingSave_SetsSchemaVersionWhenEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mapping.json")

	m := &Mapping{Org: OrgMapping{From: "gh/src", To: "gh/dst"}}
	// SchemaVersion is intentionally left empty.
	if m.SchemaVersion != "" {
		t.Skip("precondition: SchemaVersion must start empty")
	}
	if err := m.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	out, err := LoadMapping(path)
	if err != nil {
		t.Fatalf("LoadMapping: %v", err)
	}
	if out.SchemaVersion != SchemaVersion {
		t.Errorf("SchemaVersion = %q after round-trip; want %q", out.SchemaVersion, SchemaVersion)
	}
}

// ---------------------------------------------------------------------------
// LoadMapping error paths
// ---------------------------------------------------------------------------

func TestLoadMapping_MissingFile(t *testing.T) {
	_, err := LoadMapping("/no/such/file/mapping.json")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestLoadMapping_BadJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(path, []byte("{not valid json"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	_, err := LoadMapping(path)
	if err == nil {
		t.Fatal("expected error for bad JSON, got nil")
	}
}

func TestLoadMapping_SchemaMismatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mapping.json")
	if err := writeJSON(path, map[string]any{"schema_version": "999", "org": map[string]any{"from": "a", "to": "b"}}, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadMapping(path)
	if err == nil {
		t.Fatal("expected schema-version mismatch error, got nil")
	}
}

// LoadMapping accepts a mapping whose schema_version field is absent (empty
// string), treating it as compatible.
func TestLoadMapping_EmptySchemaVersionAccepted(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mapping.json")
	// No schema_version key at all.
	if err := writeJSON(path, map[string]any{"org": map[string]any{"from": "gh/a", "to": "gh/b"}}, 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := LoadMapping(path)
	if err != nil {
		t.Fatalf("LoadMapping: %v", err)
	}
	if out.Org.From != "gh/a" {
		t.Errorf("Org.From = %q; want %q", out.Org.From, "gh/a")
	}
}

// ---------------------------------------------------------------------------
// LoadSecretBundle error paths
// ---------------------------------------------------------------------------

func TestLoadSecretBundle_MissingFile(t *testing.T) {
	_, err := LoadSecretBundle("/no/such/file/secrets.json")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestLoadSecretBundle_BadJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(path, []byte("{not valid json"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	_, err := LoadSecretBundle(path)
	if err == nil {
		t.Fatal("expected error for bad JSON, got nil")
	}
}

func TestLoadSecretBundle_SchemaMismatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secrets.json")
	if err := writeJSON(path, map[string]any{"schema_version": "999"}, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadSecretBundle(path)
	if err == nil {
		t.Fatal("expected schema-version mismatch error, got nil")
	}
}

// TestLoadSecretBundle_UnknownField verifies that a bundle JSON with an
// unrecognised top-level field (e.g. the misspelling "contexts" instead of
// the correct "context_secrets") is rejected with a clear error that names
// the offending field, rather than silently producing an empty bundle.
func TestLoadSecretBundle_UnknownField(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad-field.json")
	// "contexts" is the wrong field name — the correct name is "context_secrets".
	raw := `{"schema_version":"1","contexts":{"myctx":{"KEY":"val"}}}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	_, err := LoadSecretBundle(path)
	if err == nil {
		t.Fatal("expected error for unknown field 'contexts', got nil")
	}
	// The error must mention the offending field name so the user knows what to fix.
	if !strings.Contains(err.Error(), "contexts") {
		t.Errorf("error %q does not mention the offending field 'contexts'", err.Error())
	}
}

// TestLoadSecretBundle_ValidBundle_RoundTrip verifies that a correctly-shaped
// bundle (using the proper field names) loads without error and with the
// expected values intact.
func TestLoadSecretBundle_ValidBundle_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secrets.json")
	b := NewSecretBundle()
	b.SetContextSecret("ctx-a", "KEY", "val")
	b.SetProjectSecret("gh/o/p", "ENV", "secret")
	if err := b.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := LoadSecretBundle(path)
	if err != nil {
		t.Fatalf("LoadSecretBundle: %v", err)
	}
	if loaded.ContextSecrets["ctx-a"]["KEY"] != "val" {
		t.Errorf("ContextSecrets[ctx-a][KEY] = %q; want %q", loaded.ContextSecrets["ctx-a"]["KEY"], "val")
	}
	if loaded.ProjectSecrets["gh/o/p"]["ENV"] != "secret" {
		t.Errorf("ProjectSecrets[gh/o/p][ENV] = %q; want %q", loaded.ProjectSecrets["gh/o/p"]["ENV"], "secret")
	}
}

// ---------------------------------------------------------------------------
// IdentityMapping
// ---------------------------------------------------------------------------

func TestIdentityMapping_FromEqualsTo(t *testing.T) {
	m := IdentityMapping("gh/myorg")
	if m.Org.From != "gh/myorg" {
		t.Errorf("Org.From = %q; want %q", m.Org.From, "gh/myorg")
	}
	if m.Org.To != "gh/myorg" {
		t.Errorf("Org.To = %q; want %q", m.Org.To, "gh/myorg")
	}
}

func TestIdentityMapping_ResolveProjectSlug_SameOrg(t *testing.T) {
	m := IdentityMapping("gh/myorg")
	slug, ok := m.ResolveProjectSlug("gh/myorg/repo")
	if !ok {
		t.Fatal("expected ok=true for identity mapping")
	}
	if slug != "gh/myorg/repo" {
		t.Errorf("slug = %q; want %q", slug, "gh/myorg/repo")
	}
}

// ---------------------------------------------------------------------------
// Manifest.Save sets SchemaVersion when empty
// ---------------------------------------------------------------------------

func TestManifestSave_SetsSchemaVersionWhenEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.json")

	m := &Manifest{
		Source: Source{Host: "https://circleci.com", Org: Org{Slug: "gh/test", Name: "test"}},
	}
	// SchemaVersion starts empty.
	if err := m.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	out, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if out.SchemaVersion != SchemaVersion {
		t.Errorf("SchemaVersion = %q; want %q", out.SchemaVersion, SchemaVersion)
	}
}

// ---------------------------------------------------------------------------
// Load error path: bad JSON
// ---------------------------------------------------------------------------

func TestLoad_BadJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(path, []byte("{bad json}"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for bad JSON, got nil")
	}
}

// ---------------------------------------------------------------------------
// SecretBundle helpers
// ---------------------------------------------------------------------------

func TestNewSecretBundle_Initialized(t *testing.T) {
	b := NewSecretBundle()
	if b.SchemaVersion != SchemaVersion {
		t.Errorf("SchemaVersion = %q; want %q", b.SchemaVersion, SchemaVersion)
	}
	if b.ContextSecrets == nil {
		t.Error("ContextSecrets must not be nil")
	}
	if b.ProjectSecrets == nil {
		t.Error("ProjectSecrets must not be nil")
	}
}

func TestSecretBundle_SetContextSecret(t *testing.T) {
	b := NewSecretBundle()
	b.SetContextSecret("ctx", "KEY", "val")
	if b.ContextSecrets["ctx"]["KEY"] != "val" {
		t.Errorf("ContextSecrets[ctx][KEY] = %q; want %q", b.ContextSecrets["ctx"]["KEY"], "val")
	}
}

func TestSecretBundle_SetProjectSecret(t *testing.T) {
	b := NewSecretBundle()
	b.SetProjectSecret("gh/org/proj", "ENV", "secret")
	if b.ProjectSecrets["gh/org/proj"]["ENV"] != "secret" {
		t.Errorf("ProjectSecrets[gh/org/proj][ENV] = %q; want %q", b.ProjectSecrets["gh/org/proj"]["ENV"], "secret")
	}
}

// SetContextSecret on a bundle with nil maps (edge case: zero-value struct).
func TestSecretBundle_SetContextSecret_NilMap(t *testing.T) {
	b := &SecretBundle{} // nil maps
	b.SetContextSecret("ctx", "KEY", "val")
	if b.ContextSecrets["ctx"]["KEY"] != "val" {
		t.Errorf("ContextSecrets[ctx][KEY] = %q; want %q", b.ContextSecrets["ctx"]["KEY"], "val")
	}
}

func TestSecretBundle_SetProjectSecret_NilMap(t *testing.T) {
	b := &SecretBundle{} // nil maps
	b.SetProjectSecret("gh/o/p", "K", "v")
	if b.ProjectSecrets["gh/o/p"]["K"] != "v" {
		t.Errorf("ProjectSecrets[gh/o/p][K] = %q; want %q", b.ProjectSecrets["gh/o/p"]["K"], "v")
	}
}

// ---------------------------------------------------------------------------
// SecretBundle.Save — nested directory creation
// ---------------------------------------------------------------------------

// TestSecretBundleSave_CreatesParentDir verifies that SecretBundle.Save
// automatically creates the parent directory when it does not exist. This is
// the fix for the live bug where slugs like "gh/org/repo" caused "no such
// file or directory" because the path was written verbatim as a nested path.
func TestSecretBundleSave_CreatesParentDir(t *testing.T) {
	base := t.TempDir()
	// Path with three levels of directories that do not yet exist.
	path := filepath.Join(base, "subdir", "that", "does", "not", "exist", "secrets.json")

	b := NewSecretBundle()
	b.SetContextSecret("ctx", "KEY", "val")

	if err := b.Save(path); err != nil {
		t.Fatalf("Save to nested path: %v", err)
	}

	// Read it back to confirm the file is valid.
	loaded, err := LoadSecretBundle(path)
	if err != nil {
		t.Fatalf("LoadSecretBundle: %v", err)
	}
	if loaded.ContextSecrets["ctx"]["KEY"] != "val" {
		t.Errorf("ContextSecrets[ctx][KEY] = %q; want %q", loaded.ContextSecrets["ctx"]["KEY"], "val")
	}
}

// TestSecretBundleSave_ParentDirPerms verifies that SecretBundle.Save creates
// the parent directory with 0700 permissions (owner-only) so that secret
// material is not readable by other users on the same system.
func TestSecretBundleSave_ParentDirPerms(t *testing.T) {
	base := t.TempDir()
	// Use a sub-directory that does not yet exist so Save must create it.
	path := filepath.Join(base, "secret-dir", "bundle.json")

	b := NewSecretBundle()
	b.SetContextSecret("ctx", "KEY", "val")

	if err := b.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	info, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("Stat parent dir: %v", err)
	}
	// Parent directory must be 0700 (owner read/write/execute only).
	if info.Mode().Perm() != 0o700 {
		t.Errorf("parent dir mode = %04o, want 0700", info.Mode().Perm())
	}
}

// ---------------------------------------------------------------------------
// Mapping.MapRepoFullName
// ---------------------------------------------------------------------------

// TestMapRepoFullName_MappedOwner verifies that when GitHubOrg is set and the
// source full-name matches the From owner, the owner is replaced with To.
func TestMapRepoFullName_MappedOwner(t *testing.T) {
	m := &Mapping{
		Org:       OrgMapping{From: "circleci/src-id", To: "circleci/dst-id"},
		GitHubOrg: &OrgMapping{From: "acme", To: "acme-new"},
	}
	cases := []struct {
		source string
		want   string
	}{
		{"acme/web", "acme-new/web"},
		{"acme/api", "acme-new/api"},
		{"acme/some-repo", "acme-new/some-repo"},
	}
	for _, tc := range cases {
		got := m.MapRepoFullName(tc.source)
		if got != tc.want {
			t.Errorf("MapRepoFullName(%q) = %q; want %q", tc.source, got, tc.want)
		}
	}
}

// TestMapRepoFullName_UnmappedOwner verifies that when the source full-name
// does not match the From owner, it is returned unchanged.
func TestMapRepoFullName_UnmappedOwner(t *testing.T) {
	m := &Mapping{
		GitHubOrg: &OrgMapping{From: "acme", To: "acme-new"},
	}
	cases := []string{
		"other-org/web",
		"acme-extra/repo", // prefix match must be exact (owner + slash)
		"",
	}
	for _, tc := range cases {
		got := m.MapRepoFullName(tc)
		if got != tc {
			t.Errorf("MapRepoFullName(%q) = %q; want unchanged %q", tc, got, tc)
		}
	}
}

// TestMapRepoFullName_NoGitHubOrg verifies that when GitHubOrg is nil,
// MapRepoFullName returns the source full-name unchanged.
func TestMapRepoFullName_NoGitHubOrg(t *testing.T) {
	m := &Mapping{
		Org: OrgMapping{From: "circleci/src", To: "circleci/dst"},
	}
	got := m.MapRepoFullName("acme/web")
	if got != "acme/web" {
		t.Errorf("MapRepoFullName with nil GitHubOrg: got %q, want %q", got, "acme/web")
	}
}

// TestMapRepoFullName_NilMapping verifies that calling MapRepoFullName on a
// nil Mapping returns the source full-name unchanged (nil-safe).
func TestMapRepoFullName_NilMapping(t *testing.T) {
	var m *Mapping
	got := m.MapRepoFullName("acme/web")
	if got != "acme/web" {
		t.Errorf("MapRepoFullName on nil Mapping: got %q, want %q", got, "acme/web")
	}
}

// TestMappingGitHubOrg_RoundTrip verifies that the GitHubOrg field is
// persisted and reloaded correctly via Save/LoadMapping.
func TestMappingGitHubOrg_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/mapping.json"

	in := &Mapping{
		Org:       OrgMapping{From: "circleci/src-id", To: "circleci/dst-id"},
		GitHubOrg: &OrgMapping{From: "acme", To: "acme-new"},
	}
	if err := in.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	out, err := LoadMapping(path)
	if err != nil {
		t.Fatalf("LoadMapping: %v", err)
	}
	if out.GitHubOrg == nil {
		t.Fatal("GitHubOrg must not be nil after round-trip")
	}
	if out.GitHubOrg.From != "acme" || out.GitHubOrg.To != "acme-new" {
		t.Errorf("GitHubOrg = %+v; want {From:acme To:acme-new}", out.GitHubOrg)
	}
}
