package syncer

import (
	"errors"
	"strings"
	"testing"

	apirunner "github.com/CircleCI-Public/circleci-org-migration-cli/api/runner"
	"github.com/CircleCI-Public/circleci-org-migration-cli/internal/manifest"
)

// ---------------------------------------------------------------------------
// Fake runner writer
// ---------------------------------------------------------------------------

type fakeRunnerWriter struct {
	getResourceClasses  func(namespace string) ([]apirunner.ResourceClass, error)
	createResourceClass func(resourceClass, description string) (*apirunner.ResourceClass, error)
	getCalls            []string
	createCalls         []string
}

func (f *fakeRunnerWriter) GetResourceClassesByNamespace(namespace string) ([]apirunner.ResourceClass, error) {
	f.getCalls = append(f.getCalls, namespace)
	if f.getResourceClasses != nil {
		return f.getResourceClasses(namespace)
	}
	return nil, nil
}

func (f *fakeRunnerWriter) CreateResourceClass(resourceClass, description string) (*apirunner.ResourceClass, error) {
	f.createCalls = append(f.createCalls, resourceClass)
	if f.createResourceClass != nil {
		return f.createResourceClass(resourceClass, description)
	}
	return &apirunner.ResourceClass{ID: "new-id", ResourceClass: resourceClass, Description: description}, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func runnerManifestWith(ns string, classes ...manifest.RunnerResourceClass) *manifest.Manifest {
	return &manifest.Manifest{
		SchemaVersion:         manifest.SchemaVersion,
		RunnerNamespace:       ns,
		RunnerResourceClasses: classes,
	}
}

func minimalSyncer() *Syncer {
	return &Syncer{
		Org: &fakeOrgResolver{},
	}
}

// ---------------------------------------------------------------------------
// SyncRunnerResourceClasses tests
// ---------------------------------------------------------------------------

// TestSyncRunner_NoClassesInManifest verifies that an empty manifest produces
// an empty report with no API calls.
func TestSyncRunner_NoClassesInManifest(t *testing.T) {
	rw := &fakeRunnerWriter{}
	sy := minimalSyncer()
	sy.Runner = rw

	m := &manifest.Manifest{SchemaVersion: manifest.SchemaVersion}
	rep, err := sy.SyncRunnerResourceClasses(m, Options{
		Apply:               true,
		DestRunnerNamespace: "acme-new",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rep.Actions) != 0 {
		t.Errorf("expected 0 actions, got %d: %+v", len(rep.Actions), rep.Actions)
	}
	if len(rw.createCalls) != 0 {
		t.Errorf("expected 0 CreateResourceClass calls, got %d", len(rw.createCalls))
	}
}

// TestSyncRunner_DryRun_ReportsCreated verifies that a dry run reports
// "created" for each class without making API calls.
func TestSyncRunner_DryRun_ReportsCreated(t *testing.T) {
	rw := &fakeRunnerWriter{}
	sy := minimalSyncer()
	sy.Runner = rw

	m := runnerManifestWith("acme",
		manifest.RunnerResourceClass{Name: "acme/fast", Description: "fast runner"},
		manifest.RunnerResourceClass{Name: "acme/slow", Description: "slow runner"},
	)
	rep, err := sy.SyncRunnerResourceClasses(m, Options{
		Apply:               false, // dry run
		DestRunnerNamespace: "acme-new",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	counts := rep.Counts()
	if counts["created"] != 2 {
		t.Errorf("expected 2 created, got %+v", counts)
	}
	// No actual API calls in dry run.
	if len(rw.createCalls) != 0 {
		t.Errorf("dry run should not call CreateResourceClass, got %d calls", len(rw.createCalls))
	}
}

// TestSyncRunner_Apply_Creates verifies that in apply mode, each class is
// created with the translated name.
func TestSyncRunner_Apply_Creates(t *testing.T) {
	rw := &fakeRunnerWriter{
		// Return empty existing list so nothing is pre-existing.
		getResourceClasses: func(namespace string) ([]apirunner.ResourceClass, error) {
			return nil, nil
		},
	}
	sy := minimalSyncer()
	sy.Runner = rw

	m := runnerManifestWith("acme",
		manifest.RunnerResourceClass{Name: "acme/fast", Description: "speedy"},
	)
	rep, err := sy.SyncRunnerResourceClasses(m, Options{
		Apply:               true,
		DestRunnerNamespace: "acme-new",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	counts := rep.Counts()
	if counts["created"] != 1 {
		t.Errorf("expected 1 created, got %+v", counts)
	}
	if len(rw.createCalls) != 1 || rw.createCalls[0] != "acme-new/fast" {
		t.Errorf("CreateResourceClass called with %v, want [acme-new/fast]", rw.createCalls)
	}
}

// TestSyncRunner_Apply_AlreadyExists_Idempotent verifies that a class already
// present in the destination is reported as "exists" without being re-created.
func TestSyncRunner_Apply_AlreadyExists_Idempotent(t *testing.T) {
	rw := &fakeRunnerWriter{
		getResourceClasses: func(namespace string) ([]apirunner.ResourceClass, error) {
			return []apirunner.ResourceClass{
				{ID: "existing-id", ResourceClass: "acme-new/fast", Description: "existing"},
			}, nil
		},
	}
	sy := minimalSyncer()
	sy.Runner = rw

	m := runnerManifestWith("acme",
		manifest.RunnerResourceClass{Name: "acme/fast", Description: "speedy"},
	)
	rep, err := sy.SyncRunnerResourceClasses(m, Options{
		Apply:               true,
		DestRunnerNamespace: "acme-new",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	counts := rep.Counts()
	if counts["exists"] != 1 {
		t.Errorf("expected 1 exists, got %+v", counts)
	}
	if len(rw.createCalls) != 0 {
		t.Errorf("expected no CreateResourceClass calls for existing class, got %d", len(rw.createCalls))
	}
}

// TestSyncRunner_Apply_Conflict_Idempotent verifies that a conflict (409) from
// CreateResourceClass is treated as idempotent ("exists").
func TestSyncRunner_Apply_Conflict_Idempotent(t *testing.T) {
	rw := &fakeRunnerWriter{
		getResourceClasses: func(namespace string) ([]apirunner.ResourceClass, error) {
			return nil, nil // nothing pre-existing
		},
		createResourceClass: func(resourceClass, description string) (*apirunner.ResourceClass, error) {
			return nil, errors.New("resource class already exists (conflict)")
		},
	}
	sy := minimalSyncer()
	sy.Runner = rw

	m := runnerManifestWith("acme",
		manifest.RunnerResourceClass{Name: "acme/fast", Description: "speedy"},
	)
	rep, err := sy.SyncRunnerResourceClasses(m, Options{
		Apply:               true,
		DestRunnerNamespace: "acme-new",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	counts := rep.Counts()
	if counts["exists"] != 1 {
		t.Errorf("expected 1 exists on conflict, got %+v", counts)
	}
}

// TestSyncRunner_NoDestNamespace_Manual verifies that without a destination
// namespace, all classes are flagged as manual.
func TestSyncRunner_NoDestNamespace_Manual(t *testing.T) {
	rw := &fakeRunnerWriter{}
	sy := minimalSyncer()
	sy.Runner = rw

	m := runnerManifestWith("acme",
		manifest.RunnerResourceClass{Name: "acme/fast"},
		manifest.RunnerResourceClass{Name: "acme/slow"},
	)
	rep, err := sy.SyncRunnerResourceClasses(m, Options{
		Apply:               true,
		DestRunnerNamespace: "", // no destination namespace
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	counts := rep.Counts()
	if counts["manual"] != 2 {
		t.Errorf("expected 2 manual, got %+v", counts)
	}
	if len(rw.createCalls) != 0 {
		t.Errorf("expected no CreateResourceClass calls, got %d", len(rw.createCalls))
	}
}

// TestSyncRunner_NameTranslation verifies the src→dest namespace translation.
func TestSyncRunner_NameTranslation(t *testing.T) {
	tests := []struct {
		name      string
		srcNs     string
		destNs    string
		className string
		want      string
	}{
		{"same ns prefix", "acme", "acme-new", "acme/my-runner", "acme-new/my-runner"},
		{"different prefix", "src", "dst", "src/runner-x", "dst/runner-x"},
		{"no prefix in name", "", "dst", "some/runner", "dst/runner"},
		{"no slash in name", "", "dst", "runner", "dst/runner"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := translateResourceClass(tt.className, tt.srcNs, tt.destNs)
			if got != tt.want {
				t.Errorf("translateResourceClass(%q, %q, %q) = %q, want %q",
					tt.className, tt.srcNs, tt.destNs, got, tt.want)
			}
		})
	}
}

// TestSyncRunner_CreateError_Reported verifies that a non-conflict API error
// is reported as "error" in the report.
func TestSyncRunner_CreateError_Reported(t *testing.T) {
	rw := &fakeRunnerWriter{
		getResourceClasses: func(namespace string) ([]apirunner.ResourceClass, error) {
			return nil, nil
		},
		createResourceClass: func(resourceClass, description string) (*apirunner.ResourceClass, error) {
			return nil, errors.New("internal server error")
		},
	}
	sy := minimalSyncer()
	sy.Runner = rw

	m := runnerManifestWith("acme",
		manifest.RunnerResourceClass{Name: "acme/fast"},
	)
	rep, err := sy.SyncRunnerResourceClasses(m, Options{
		Apply:               true,
		DestRunnerNamespace: "acme-new",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	counts := rep.Counts()
	if counts["error"] != 1 {
		t.Errorf("expected 1 error action, got %+v", counts)
	}
}

// TestTranslateResourceClass_IsAlreadyExists tests the isAlreadyExists helper.
func TestIsAlreadyExists(t *testing.T) {
	tests := []struct {
		err  error
		want bool
	}{
		{nil, false},
		{errors.New("already exists"), true},
		{errors.New("resource class already exists (conflict)"), true},
		{errors.New("conflict on create"), true},
		{errors.New("not found"), false},
		{errors.New("internal server error"), false},
	}
	for _, tt := range tests {
		got := isAlreadyExists(tt.err)
		if got != tt.want {
			errStr := "<nil>"
			if tt.err != nil {
				errStr = tt.err.Error()
			}
			t.Errorf("isAlreadyExists(%q) = %v, want %v", errStr, got, tt.want)
		}
	}
}

// TestSyncRunner_NilRunnerClient_Manual verifies that when the Runner client
// is nil but DestRunnerNamespace is set, classes are flagged as manual.
func TestSyncRunner_NilRunnerClient_Manual(t *testing.T) {
	sy := minimalSyncer()
	// sy.Runner is nil

	m := runnerManifestWith("acme",
		manifest.RunnerResourceClass{Name: "acme/fast"},
	)
	rep, err := sy.SyncRunnerResourceClasses(m, Options{
		Apply:               true,
		DestRunnerNamespace: "acme-new",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, a := range rep.Actions {
		if !strings.Contains(a.Detail, "no runner client configured") {
			t.Errorf("expected 'no runner client configured' in detail, got: %q", a.Detail)
		}
	}
}
