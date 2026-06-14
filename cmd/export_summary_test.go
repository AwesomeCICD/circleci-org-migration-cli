package cmd

// export_summary_test.go — white-box unit tests for buildExportSummary.
// Lives in package cmd so it can access the unexported function.

import (
	"testing"

	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
)

func TestBuildExportSummary_CountsAndPaths(t *testing.T) {
	m := &manifest.Manifest{
		GeneratedAt: "2026-01-01T00:00:00Z",
		Source: manifest.Source{
			Host: "https://circleci.com",
			Org:  manifest.Org{Slug: "gh/acme", ID: "org-uuid"},
		},
		Contexts: []manifest.Context{
			{EnvVars: []manifest.ContextEnvVar{{Name: "TOKEN"}, {Name: "SECRET"}}},
			{EnvVars: []manifest.ContextEnvVar{{Name: "KEY"}}},
		},
		Projects: []manifest.Project{
			{EnvVars: []manifest.ProjectEnvVar{{Name: "DB_URL"}}},
			{EnvVars: []manifest.ProjectEnvVar{{Name: "API_KEY"}, {Name: "WEBHOOK_SECRET"}}},
		},
		Warnings: []manifest.Warning{
			{Scope: "project:gh/acme/web", Code: "project_values_excluded", Message: "masked"},
			{Scope: "projects", Code: "discovery_fallback", Message: "fallback"},
		},
	}

	sum := buildExportSummary(m, "/tmp/manifest.json", "/tmp/report.md")

	if sum.SourceOrgSlug != "gh/acme" {
		t.Errorf("SourceOrgSlug: got %q, want gh/acme", sum.SourceOrgSlug)
	}
	if sum.SourceOrgID != "org-uuid" {
		t.Errorf("SourceOrgID: got %q, want org-uuid", sum.SourceOrgID)
	}
	if sum.Host != "https://circleci.com" {
		t.Errorf("Host: got %q, want https://circleci.com", sum.Host)
	}
	if sum.ContextCount != 2 {
		t.Errorf("ContextCount: got %d, want 2", sum.ContextCount)
	}
	if sum.ContextVarCount != 3 {
		t.Errorf("ContextVarCount: got %d, want 3 (2+1)", sum.ContextVarCount)
	}
	if sum.ProjectCount != 2 {
		t.Errorf("ProjectCount: got %d, want 2", sum.ProjectCount)
	}
	if sum.ProjectVarCount != 3 {
		t.Errorf("ProjectVarCount: got %d, want 3 (1+2)", sum.ProjectVarCount)
	}
	if sum.WarningCount != 2 {
		t.Errorf("WarningCount: got %d, want 2", sum.WarningCount)
	}
	if len(sum.Warnings) != 2 {
		t.Errorf("Warnings length: got %d, want 2", len(sum.Warnings))
	}
	if sum.ManifestPath != "/tmp/manifest.json" {
		t.Errorf("ManifestPath: got %q, want /tmp/manifest.json", sum.ManifestPath)
	}
	if sum.ReportPath != "/tmp/report.md" {
		t.Errorf("ReportPath: got %q, want /tmp/report.md", sum.ReportPath)
	}
}

func TestBuildExportSummary_EmptyManifest(t *testing.T) {
	m := &manifest.Manifest{}
	sum := buildExportSummary(m, "out.json", "report.md")
	if sum.ContextCount != 0 || sum.ProjectCount != 0 || sum.WarningCount != 0 {
		t.Errorf("empty manifest should produce all-zero counts; got %+v", sum)
	}
	// buildExportSummary uses make([]exportWarning, 0, ...) so Warnings is non-nil but empty.
	if len(sum.Warnings) != 0 {
		t.Errorf("empty warnings should produce empty Warnings slice; got %v", sum.Warnings)
	}
}
