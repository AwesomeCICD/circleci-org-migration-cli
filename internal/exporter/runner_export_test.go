package exporter_test

import (
	"errors"
	"testing"

	apirunner "github.com/CircleCI-Public/circleci-org-migration-cli/api/runner"
	"github.com/CircleCI-Public/circleci-org-migration-cli/internal/exporter"
)

// ---------------------------------------------------------------------------
// Fake runner API
// ---------------------------------------------------------------------------

type fakeRunnerAPI struct {
	getResourceClasses func(namespace string) ([]apirunner.ResourceClass, error)
}

func (f *fakeRunnerAPI) GetResourceClassesByNamespace(namespace string) ([]apirunner.ResourceClass, error) {
	if f.getResourceClasses != nil {
		return f.getResourceClasses(namespace)
	}
	return nil, nil
}

// ---------------------------------------------------------------------------
// Runner resource class capture tests
// ---------------------------------------------------------------------------

// TestExport_Runner_NamespaceSet_Captured verifies that when RunnerNamespace
// is set and the runner client is wired, resource classes appear in the manifest.
func TestExport_Runner_NamespaceSet_Captured(t *testing.T) {
	ex := minimalExporter()
	ex.Runner = &fakeRunnerAPI{
		getResourceClasses: func(namespace string) ([]apirunner.ResourceClass, error) {
			if namespace != "acme" {
				t.Errorf("GetResourceClassesByNamespace called with %q, want %q", namespace, "acme")
			}
			return []apirunner.ResourceClass{
				{ID: "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee", ResourceClass: "acme/my-runner", Description: "fast runner"},
				{ID: "11111111-2222-3333-4444-555555555555", ResourceClass: "acme/slow-runner", Description: ""},
			}, nil
		},
	}

	m, err := ex.Export(exporter.Options{
		OrgSlug:         "gh/acme",
		RunnerNamespace: "acme",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.RunnerNamespace != "acme" {
		t.Errorf("RunnerNamespace = %q, want %q", m.RunnerNamespace, "acme")
	}
	if len(m.RunnerResourceClasses) != 2 {
		t.Fatalf("RunnerResourceClasses count = %d, want 2", len(m.RunnerResourceClasses))
	}
	if m.RunnerResourceClasses[0].Name != "acme/my-runner" {
		t.Errorf("RunnerResourceClasses[0].Name = %q, want %q", m.RunnerResourceClasses[0].Name, "acme/my-runner")
	}
	if m.RunnerResourceClasses[0].Description != "fast runner" {
		t.Errorf("RunnerResourceClasses[0].Description = %q, want %q", m.RunnerResourceClasses[0].Description, "fast runner")
	}
	if m.RunnerResourceClasses[1].Name != "acme/slow-runner" {
		t.Errorf("RunnerResourceClasses[1].Name = %q", m.RunnerResourceClasses[1].Name)
	}
}

// TestExport_Runner_EmptyNamespace_Skipped verifies that when RunnerNamespace
// is empty, runner capture is silently skipped.
func TestExport_Runner_EmptyNamespace_Skipped(t *testing.T) {
	called := false
	ex := minimalExporter()
	ex.Runner = &fakeRunnerAPI{
		getResourceClasses: func(namespace string) ([]apirunner.ResourceClass, error) {
			called = true
			return nil, nil
		},
	}

	m, err := ex.Export(exporter.Options{
		OrgSlug:         "gh/acme",
		RunnerNamespace: "", // empty → skip
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if called {
		t.Error("GetResourceClassesByNamespace should not be called when RunnerNamespace is empty")
	}
	if len(m.RunnerResourceClasses) != 0 {
		t.Errorf("expected 0 runner classes, got %d", len(m.RunnerResourceClasses))
	}
}

// TestExport_Runner_NoRunnerClient_Skipped verifies that when Runner is nil,
// runner capture is silently skipped even if RunnerNamespace is set.
func TestExport_Runner_NoRunnerClient_Skipped(t *testing.T) {
	ex := minimalExporter()
	// Runner is nil (default)

	m, err := ex.Export(exporter.Options{
		OrgSlug:         "gh/acme",
		RunnerNamespace: "acme",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m.RunnerResourceClasses) != 0 {
		t.Errorf("expected 0 runner classes (no client), got %d", len(m.RunnerResourceClasses))
	}
}

// TestExport_Runner_APIError_Warning verifies that a runner API error results
// in a warning and does not fail the export.
func TestExport_Runner_APIError_Warning(t *testing.T) {
	ex := minimalExporter()
	ex.Runner = &fakeRunnerAPI{
		getResourceClasses: func(namespace string) ([]apirunner.ResourceClass, error) {
			return nil, errors.New("runner API unavailable")
		},
	}

	m, err := ex.Export(exporter.Options{
		OrgSlug:         "gh/acme",
		RunnerNamespace: "acme",
	})
	if err != nil {
		t.Fatalf("export should not fail on runner API error: %v", err)
	}
	if len(m.RunnerResourceClasses) != 0 {
		t.Errorf("expected 0 runner classes on error, got %d", len(m.RunnerResourceClasses))
	}
	// A warning should have been recorded.
	var found bool
	for _, w := range m.Warnings {
		if w.Code == "runner_unreadable" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected runner_unreadable warning, got: %+v", m.Warnings)
	}
}
