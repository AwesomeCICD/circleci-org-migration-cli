package exporter_test

// api_tokens_export_test.go contains unit tests for project API token capture
// (issue #132). The token VALUES are never available via list; only label+scope
// are stored in the manifest.

import (
	"errors"
	"testing"

	"github.com/AwesomeCICD/circleci-org-migration-cli/api/org"
	"github.com/AwesomeCICD/circleci-org-migration-cli/api/project"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/exporter"
)

// defaultOrgForTokens returns a standard GitHub OAuth org fixture.
func defaultOrgForTokens() *org.Organization {
	return &org.Organization{
		ID:      "org-uuid-tokens",
		Name:    "tokenorg",
		Slug:    "gh/tokenorg",
		VCSType: "github",
	}
}

// tokenExporter builds a minimal Exporter with the given listProjectTokens impl.
func tokenExporter(listTokens func(slug string) ([]project.ProjectAPIToken, error)) *exporter.Exporter {
	return &exporter.Exporter{
		Org: &fakeOrgAPI{
			getOrganization: func(string) (*org.Organization, error) { return defaultOrgForTokens(), nil },
		},
		Contexts: &fakeContextAPI{},
		Projects: &fakeProjectAPI{
			getProject: func(slug string) (*project.Project, error) {
				return &project.Project{ID: "proj-id-tok", Name: "web", Slug: slug}, nil
			},
			listOrgProjects: func(orgID string) ([]project.OrgProject, error) {
				return []project.OrgProject{{Slug: "gh/tokenorg/web"}}, nil
			},
			listProjectTokens: listTokens,
		},
	}
}

var tokenOpts = exporter.Options{
	OrgSlug:         "gh/tokenorg",
	IncludeProjects: true,
	IncludeExtras:   true,
}

// TestAPITokens_CapturedIntoManifest verifies that when the API returns tokens
// the exporter maps label+scope into the manifest (no value field).
func TestAPITokens_CapturedIntoManifest(t *testing.T) {
	ex := tokenExporter(func(slug string) ([]project.ProjectAPIToken, error) {
		return []project.ProjectAPIToken{
			{ID: "tok-id-1", Label: "deploy-bot", Scope: "all"},
			{ID: "tok-id-2", Label: "status-check", Scope: "status"},
		}, nil
	})

	m, err := ex.Export(tokenOpts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m.Projects) == 0 {
		t.Fatal("no projects in manifest")
	}
	p := m.Projects[0]
	if len(p.APITokens) != 2 {
		t.Fatalf("expected 2 APITokens, got %d", len(p.APITokens))
	}

	// First token.
	if p.APITokens[0].Label != "deploy-bot" {
		t.Errorf("Label[0]: got %q, want deploy-bot", p.APITokens[0].Label)
	}
	if p.APITokens[0].Scope != "all" {
		t.Errorf("Scope[0]: got %q, want all", p.APITokens[0].Scope)
	}

	// Second token.
	if p.APITokens[1].Label != "status-check" {
		t.Errorf("Label[1]: got %q, want status-check", p.APITokens[1].Label)
	}
	if p.APITokens[1].Scope != "status" {
		t.Errorf("Scope[1]: got %q, want status", p.APITokens[1].Scope)
	}

	// A warning must be emitted (values excluded).
	foundWarning := false
	for _, w := range m.Warnings {
		if w.Code == "api_tokens_values_excluded" {
			foundWarning = true
		}
	}
	if !foundWarning {
		t.Error("expected api_tokens_values_excluded warning, none found")
	}
}

// TestAPITokens_EmptyList verifies that when there are no tokens the manifest
// field is nil/absent and no warning is emitted for it.
func TestAPITokens_EmptyList(t *testing.T) {
	ex := tokenExporter(func(slug string) ([]project.ProjectAPIToken, error) {
		return nil, nil
	})

	m, err := ex.Export(tokenOpts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m.Projects) == 0 {
		t.Fatal("no projects in manifest")
	}
	p := m.Projects[0]
	if len(p.APITokens) != 0 {
		t.Errorf("expected 0 APITokens, got %d", len(p.APITokens))
	}

	// No api_tokens_values_excluded warning should be present.
	for _, w := range m.Warnings {
		if w.Code == "api_tokens_values_excluded" {
			t.Errorf("unexpected api_tokens_values_excluded warning when no tokens present")
		}
	}
}

// TestAPITokens_ReadError verifies that a read error produces a non-fatal warning
// and the export continues (no fatal error returned).
func TestAPITokens_ReadError(t *testing.T) {
	ex := tokenExporter(func(slug string) ([]project.ProjectAPIToken, error) {
		return nil, errors.New("forbidden")
	})

	m, err := ex.Export(tokenOpts)
	if err != nil {
		t.Fatalf("export should not fail on token read error: %v", err)
	}
	if len(m.Projects) == 0 {
		t.Fatal("no projects in manifest")
	}

	// The project should have no tokens (error occurred).
	if len(m.Projects[0].APITokens) != 0 {
		t.Errorf("expected no APITokens on read error, got %d", len(m.Projects[0].APITokens))
	}

	// A warning with code "api_tokens_unreadable" must be recorded.
	foundWarning := false
	for _, w := range m.Warnings {
		if w.Code == "api_tokens_unreadable" {
			foundWarning = true
		}
	}
	if !foundWarning {
		t.Error("expected api_tokens_unreadable warning, none found")
	}
}
