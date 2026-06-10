package manifest

import (
	"path/filepath"
	"testing"
)

func ptr[T any](v T) *T { return &v }

func TestManifestRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.json")

	in := &Manifest{
		Source: Source{
			Host: "https://circleci.com",
			Org:  Org{Slug: "gh/acme", ID: "org-uuid", Name: "acme", VCSType: "github"},
		},
		Contexts: []Context{{
			Name:    "deploy-prod",
			EnvVars: []ContextEnvVar{{Name: "AWS_SECRET"}},
			Restrictions: []Restriction{
				{Type: "group", Value: "group-uuid"},
			},
			SecurityGroups: []Group{{ID: "group-uuid", Name: "platform-team", GroupType: "TEAM"}},
		}},
		Projects: []Project{{
			Slug:     "gh/acme/web",
			Name:     "web",
			VCS:      ProjectVCS{Provider: "GitHub", DefaultBranch: "main"},
			Settings: &AdvancedSettings{OSS: ptr(true), AutocancelBuilds: ptr(false)},
			EnvVars:  []ProjectEnvVar{{Name: "FOO", MaskedValue: "xxxx1234"}},
		}},
	}
	in.AddWarning("context:deploy-prod", "context_value_unavailable", "values not readable")

	if err := in.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	out, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if out.SchemaVersion != SchemaVersion {
		t.Errorf("SchemaVersion = %q, want %q", out.SchemaVersion, SchemaVersion)
	}
	if out.Source.Org.Slug != "gh/acme" {
		t.Errorf("Org.Slug = %q", out.Source.Org.Slug)
	}
	if len(out.Contexts) != 1 || out.Contexts[0].SecurityGroups[0].Name != "platform-team" {
		t.Errorf("security group not round-tripped: %+v", out.Contexts)
	}
	if out.Projects[0].Settings.OSS == nil || !*out.Projects[0].Settings.OSS {
		t.Errorf("project OSS setting not round-tripped: %+v", out.Projects[0].Settings)
	}
	if len(out.Warnings) != 1 {
		t.Errorf("warnings = %d, want 1", len(out.Warnings))
	}
}

func TestRunnerResourceClassRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.json")

	in := &Manifest{
		Source: Source{
			Host: "https://circleci.com",
			Org:  Org{Slug: "gh/acme", Name: "acme"},
		},
		RunnerNamespace: "acme",
		RunnerResourceClasses: []RunnerResourceClass{
			{Name: "acme/fast-runner", Description: "speedy"},
			{Name: "acme/slow-runner"},
		},
	}

	if err := in.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	out, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if out.RunnerNamespace != "acme" {
		t.Errorf("RunnerNamespace = %q, want %q", out.RunnerNamespace, "acme")
	}
	if len(out.RunnerResourceClasses) != 2 {
		t.Fatalf("RunnerResourceClasses count = %d, want 2", len(out.RunnerResourceClasses))
	}
	if out.RunnerResourceClasses[0].Name != "acme/fast-runner" {
		t.Errorf("RunnerResourceClasses[0].Name = %q", out.RunnerResourceClasses[0].Name)
	}
	if out.RunnerResourceClasses[0].Description != "speedy" {
		t.Errorf("RunnerResourceClasses[0].Description = %q", out.RunnerResourceClasses[0].Description)
	}
	if out.RunnerResourceClasses[1].Name != "acme/slow-runner" {
		t.Errorf("RunnerResourceClasses[1].Name = %q", out.RunnerResourceClasses[1].Name)
	}
	if out.RunnerResourceClasses[1].Description != "" {
		t.Errorf("RunnerResourceClasses[1].Description should be empty, got %q", out.RunnerResourceClasses[1].Description)
	}
}

func TestManifestRoundTrip_WithoutRunner(t *testing.T) {
	// Verify that manifests without runner fields are still valid (backward compat).
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.json")

	in := &Manifest{
		Source: Source{
			Host: "https://circleci.com",
			Org:  Org{Slug: "gh/acme", Name: "acme"},
		},
		// No RunnerNamespace or RunnerResourceClasses.
	}

	if err := in.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	out, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if out.RunnerNamespace != "" {
		t.Errorf("RunnerNamespace should be empty, got %q", out.RunnerNamespace)
	}
	if len(out.RunnerResourceClasses) != 0 {
		t.Errorf("RunnerResourceClasses should be empty, got %d", len(out.RunnerResourceClasses))
	}
}

func TestLoadRejectsUnsupportedSchema(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.json")
	if err := writeJSON(path, map[string]any{"schema_version": "999"}, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected error for unsupported schema version, got nil")
	}
}

func TestResolveProjectSlug(t *testing.T) {
	tests := []struct {
		name     string
		mapping  *Mapping
		source   string
		wantSlug string
		wantOK   bool
	}{
		{
			name:     "explicit override wins",
			mapping:  &Mapping{Org: OrgMapping{From: "gh/acme", To: "gh/acme-new"}, Projects: map[string]string{"gh/acme/web": "circleci/o/p"}},
			source:   "gh/acme/web",
			wantSlug: "circleci/o/p",
			wantOK:   true,
		},
		{
			name:     "identity swap within same vcs",
			mapping:  &Mapping{Org: OrgMapping{From: "gh/acme", To: "gh/acme-new"}},
			source:   "gh/acme/web",
			wantSlug: "gh/acme-new/web",
			wantOK:   true,
		},
		{
			name:    "github app destination needs explicit mapping",
			mapping: &Mapping{Org: OrgMapping{From: "gh/acme", To: "circleci/org-uuid"}},
			source:  "gh/acme/web",
			wantOK:  false,
		},
		{
			name:     "github app identity mapping resolves to itself",
			mapping:  &Mapping{Org: OrgMapping{From: "circleci/o", To: "circleci/o"}},
			source:   "circleci/o/p",
			wantSlug: "circleci/o/p",
			wantOK:   true,
		},
		{
			name:    "non-matching prefix fails",
			mapping: &Mapping{Org: OrgMapping{From: "gh/acme", To: "gh/acme-new"}},
			source:  "gh/other/web",
			wantOK:  false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			slug, ok := tt.mapping.ResolveProjectSlug(tt.source)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if ok && slug != tt.wantSlug {
				t.Errorf("slug = %q, want %q", slug, tt.wantSlug)
			}
		})
	}
}

func TestSecretBundleRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secrets.json")

	b := NewSecretBundle()
	b.SetContextSecret("deploy-prod", "AWS_SECRET", "s3kr3t")
	b.SetProjectSecret("gh/acme/web", "FOO", "barbazqux")

	if err := b.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	out, err := LoadSecretBundle(path)
	if err != nil {
		t.Fatalf("LoadSecretBundle: %v", err)
	}
	if out.ContextSecrets["deploy-prod"]["AWS_SECRET"] != "s3kr3t" {
		t.Errorf("context secret not round-tripped: %+v", out.ContextSecrets)
	}
	if out.ProjectSecrets["gh/acme/web"]["FOO"] != "barbazqux" {
		t.Errorf("project secret not round-tripped: %+v", out.ProjectSecrets)
	}
}
