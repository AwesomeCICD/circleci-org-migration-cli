package manifest

// extra_test.go adds tests for previously uncovered functions in the manifest
// package to raise coverage above 80%.

import (
	"os"
	"path/filepath"
	"testing"
)

// ---------------------------------------------------------------------------
// SortStable
// ---------------------------------------------------------------------------

func TestSortStable_ContextsAndProjects(t *testing.T) {
	m := &Manifest{
		Contexts: []Context{
			{Name: "zzz-context", EnvVars: []ContextEnvVar{{Name: "Z_VAR"}, {Name: "A_VAR"}}},
			{Name: "aaa-context", EnvVars: []ContextEnvVar{{Name: "B_VAR"}, {Name: "A_VAR"}}},
		},
		Projects: []Project{
			{Slug: "gh/acme/zzz", EnvVars: []ProjectEnvVar{{Name: "Z"}, {Name: "A"}}},
			{Slug: "gh/acme/aaa", EnvVars: []ProjectEnvVar{{Name: "B"}, {Name: "A"}}},
		},
	}

	m.SortStable()

	if m.Contexts[0].Name != "aaa-context" {
		t.Errorf("contexts[0] = %q, want aaa-context", m.Contexts[0].Name)
	}
	if m.Contexts[1].Name != "zzz-context" {
		t.Errorf("contexts[1] = %q, want zzz-context", m.Contexts[1].Name)
	}
	// Env vars within aaa-context should also be sorted.
	if m.Contexts[0].EnvVars[0].Name != "A_VAR" {
		t.Errorf("contexts[0].EnvVars[0] = %q, want A_VAR", m.Contexts[0].EnvVars[0].Name)
	}

	if m.Projects[0].Slug != "gh/acme/aaa" {
		t.Errorf("projects[0].Slug = %q, want gh/acme/aaa", m.Projects[0].Slug)
	}
	if m.Projects[1].Slug != "gh/acme/zzz" {
		t.Errorf("projects[1].Slug = %q, want gh/acme/zzz", m.Projects[1].Slug)
	}
	// Env vars within aaa project should be sorted.
	if m.Projects[0].EnvVars[0].Name != "A" {
		t.Errorf("projects[0].EnvVars[0] = %q, want A", m.Projects[0].EnvVars[0].Name)
	}
}

func TestSortStable_PipelineDefinitionsAndTriggers(t *testing.T) {
	m := &Manifest{
		Projects: []Project{
			{
				Slug: "gh/acme/web",
				PipelineDefinitions: []PipelineDefinition{
					{
						Name: "zzz-pipeline",
						Triggers: []Trigger{
							{Name: "z-trigger"},
							{Name: "a-trigger"},
						},
					},
					{
						Name: "aaa-pipeline",
						Triggers: []Trigger{
							{Name: "m-trigger"},
							{Name: "b-trigger"},
						},
					},
				},
			},
		},
	}

	m.SortStable()

	defs := m.Projects[0].PipelineDefinitions
	if defs[0].Name != "aaa-pipeline" {
		t.Errorf("defs[0].Name = %q, want aaa-pipeline", defs[0].Name)
	}
	if defs[1].Name != "zzz-pipeline" {
		t.Errorf("defs[1].Name = %q, want zzz-pipeline", defs[1].Name)
	}
	// Triggers within aaa-pipeline should be sorted.
	if defs[0].Triggers[0].Name != "b-trigger" {
		t.Errorf("defs[0].Triggers[0] = %q, want b-trigger", defs[0].Triggers[0].Name)
	}
	if defs[0].Triggers[1].Name != "m-trigger" {
		t.Errorf("defs[0].Triggers[1] = %q, want m-trigger", defs[0].Triggers[1].Name)
	}
}

func TestSortStable_AlreadySorted_NoChange(t *testing.T) {
	m := &Manifest{
		Contexts: []Context{
			{Name: "alpha"},
			{Name: "beta"},
			{Name: "gamma"},
		},
	}
	m.SortStable()
	if m.Contexts[0].Name != "alpha" || m.Contexts[2].Name != "gamma" {
		t.Errorf("already-sorted manifest was altered: %v", m.Contexts)
	}
}

func TestSortStable_EmptyManifest_NoPanic(t *testing.T) {
	m := &Manifest{}
	// Should not panic.
	m.SortStable()
}

// ---------------------------------------------------------------------------
// Load — missing file
// ---------------------------------------------------------------------------

func TestLoad_MissingFile(t *testing.T) {
	_, err := Load("/no/such/file/manifest.json")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

// ---------------------------------------------------------------------------
// writeJSON — error paths (unserializable type)
// ---------------------------------------------------------------------------

func TestWriteJSON_UnwritablePath(t *testing.T) {
	// Writing to a path under a non-existent directory should fail.
	err := writeJSON("/no/such/directory/file.json", map[string]any{"k": "v"}, 0o644)
	if err == nil {
		t.Fatal("expected error writing to non-existent directory, got nil")
	}
}

// ---------------------------------------------------------------------------
// SecretBundle.Save — permissions check
// ---------------------------------------------------------------------------

func TestSecretBundleSave_WritesWithRestrictedPerms(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secrets.json")

	b := NewSecretBundle()
	b.SetContextSecret("ctx", "KEY", "value")
	if err := b.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	// File mode should be 0600 (owner read/write only).
	if info.Mode().Perm() != 0o600 {
		t.Errorf("file mode = %04o, want 0600", info.Mode().Perm())
	}
}

// ---------------------------------------------------------------------------
// Manifest round-trip with SortStable
// ---------------------------------------------------------------------------

func TestManifestSortStable_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sorted.json")

	m := &Manifest{
		Source: Source{
			Host: "https://circleci.com",
			Org:  Org{Slug: "gh/acme", Name: "acme"},
		},
		Contexts: []Context{
			{Name: "z-ctx", EnvVars: []ContextEnvVar{{Name: "Z"}, {Name: "A"}}},
			{Name: "a-ctx", EnvVars: []ContextEnvVar{{Name: "B"}}},
		},
		Projects: []Project{
			{Slug: "gh/acme/z-proj"},
			{Slug: "gh/acme/a-proj"},
		},
	}
	m.SortStable()
	if err := m.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	out, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if out.Contexts[0].Name != "a-ctx" {
		t.Errorf("contexts[0].Name = %q, want a-ctx", out.Contexts[0].Name)
	}
	if out.Projects[0].Slug != "gh/acme/a-proj" {
		t.Errorf("projects[0].Slug = %q, want gh/acme/a-proj", out.Projects[0].Slug)
	}
}
