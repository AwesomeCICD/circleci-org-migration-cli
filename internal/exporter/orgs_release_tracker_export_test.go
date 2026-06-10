package exporter_test

// orgs_release_tracker_export_test.go tests exportOrgSettings capture for
// org orbs, release-tracker settings, and environment hierarchy.

import (
	"errors"
	"testing"

	"github.com/AwesomeCICD/circleci-org-migration-cli/api/org"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/exporter"
)

var newExportOpts = exporter.Options{OrgSlug: "gh/myorg"}

// newFeatureExporter builds an Exporter with configurable callbacks for the
// three new capture paths.
func newFeatureExporter(
	getOrgOrbs func(string) ([]org.OrgOrb, error),
	getReleaseTrackerSettings func(string) (*org.ReleaseTrackerSettings, error),
	getEnvironmentHierarchy func(string) (*org.EnvHierarchyConfig, error),
) *exporter.Exporter {
	return &exporter.Exporter{
		Org: &fakeOrgAPI{
			getOrganization:           func(string) (*org.Organization, error) { return defaultOrg(), nil },
			getOrgOrbs:                getOrgOrbs,
			getReleaseTrackerSettings: getReleaseTrackerSettings,
			getEnvironmentHierarchy:   getEnvironmentHierarchy,
		},
		Contexts: &fakeContextAPI{},
		Projects: &fakeProjectAPI{},
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Org orbs — capture
// ─────────────────────────────────────────────────────────────────────────────

func TestOrgOrbs_Captured(t *testing.T) {
	ex := newFeatureExporter(
		func(orgUUID string) ([]org.OrgOrb, error) {
			return []org.OrgOrb{
				{
					OrbName:             "acme/my-orb",
					LatestVersionNumber: "0.3.0",
					OrbID:               "orb-uuid-1",
					IsPrivate:           true,
					Hidden:              false,
					Description:         "Custom orb",
				},
			}, nil
		},
		nil, nil,
	)

	m, err := ex.Export(newExportOpts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Source.Org.Settings == nil {
		t.Fatal("OrgSettings is nil")
	}
	orbs := m.Source.Org.Settings.Orbs
	if len(orbs) != 1 {
		t.Fatalf("expected 1 orb, got %d", len(orbs))
	}
	if orbs[0].OrbName != "acme/my-orb" {
		t.Errorf("OrbName: got %q want %q", orbs[0].OrbName, "acme/my-orb")
	}
	if orbs[0].LatestVersionNumber != "0.3.0" {
		t.Errorf("LatestVersionNumber: got %q want %q", orbs[0].LatestVersionNumber, "0.3.0")
	}
	if !orbs[0].IsPrivate {
		t.Error("IsPrivate should be true")
	}
}

func TestOrgOrbs_Empty_NotCaptured(t *testing.T) {
	ex := newFeatureExporter(
		func(orgUUID string) ([]org.OrgOrb, error) { return []org.OrgOrb{}, nil },
		nil, nil,
	)

	m, err := ex.Export(newExportOpts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Source.Org.Settings != nil && len(m.Source.Org.Settings.Orbs) != 0 {
		t.Errorf("expected Orbs to be empty, got %+v", m.Source.Org.Settings.Orbs)
	}
}

func TestOrgOrbs_APIError_Warning(t *testing.T) {
	ex := newFeatureExporter(
		func(orgUUID string) ([]org.OrgOrb, error) {
			return nil, errors.New("permission denied")
		},
		nil, nil,
	)

	m, err := ex.Export(newExportOpts)
	if err != nil {
		t.Fatalf("export must not fail on orbs API error, got: %v", err)
	}
	if m.Source.Org.Settings != nil && len(m.Source.Org.Settings.Orbs) != 0 {
		t.Error("Orbs should be nil/empty when API returns error")
	}
	found := false
	for _, w := range m.Warnings {
		if w.Code == "orbs_unreadable" && w.Scope == "org" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected warning 'orbs_unreadable', got: %+v", m.Warnings)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Release-tracker settings — capture
// ─────────────────────────────────────────────────────────────────────────────

func TestReleaseTrackerSettings_Captured(t *testing.T) {
	ex := newFeatureExporter(
		nil,
		func(orgUUID string) (*org.ReleaseTrackerSettings, error) {
			return &org.ReleaseTrackerSettings{InconclusiveReleaseTTL: "1h"}, nil
		},
		nil,
	)

	m, err := ex.Export(newExportOpts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Source.Org.Settings == nil {
		t.Fatal("OrgSettings is nil")
	}
	rt := m.Source.Org.Settings.ReleaseTracker
	if rt == nil {
		t.Fatal("ReleaseTracker is nil in manifest")
	}
	if rt.InconclusiveReleaseTTL != "1h" {
		t.Errorf("InconclusiveReleaseTTL: got %q want %q", rt.InconclusiveReleaseTTL, "1h")
	}
}

func TestReleaseTrackerSettings_NilResponse_NotCaptured(t *testing.T) {
	ex := newFeatureExporter(
		nil,
		func(orgUUID string) (*org.ReleaseTrackerSettings, error) { return nil, nil },
		nil,
	)

	m, err := ex.Export(newExportOpts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Source.Org.Settings != nil && m.Source.Org.Settings.ReleaseTracker != nil {
		t.Errorf("expected ReleaseTracker to be nil, got %+v", m.Source.Org.Settings.ReleaseTracker)
	}
}

func TestReleaseTrackerSettings_APIError_Warning(t *testing.T) {
	ex := newFeatureExporter(
		nil,
		func(orgUUID string) (*org.ReleaseTrackerSettings, error) {
			return nil, errors.New("not found")
		},
		nil,
	)

	m, err := ex.Export(newExportOpts)
	if err != nil {
		t.Fatalf("export must not fail on release-tracker API error, got: %v", err)
	}
	if m.Source.Org.Settings != nil && m.Source.Org.Settings.ReleaseTracker != nil {
		t.Error("ReleaseTracker should be nil when API returns error")
	}
	found := false
	for _, w := range m.Warnings {
		if w.Code == "release_tracker_unreadable" && w.Scope == "org" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected warning 'release_tracker_unreadable', got: %+v", m.Warnings)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Environment hierarchy — capture
// ─────────────────────────────────────────────────────────────────────────────

func TestEnvironmentHierarchy_Captured(t *testing.T) {
	ex := newFeatureExporter(
		nil, nil,
		func(orgUUID string) (*org.EnvHierarchyConfig, error) {
			return &org.EnvHierarchyConfig{
				Name:        "prod-hierarchy",
				Description: "desc",
				Levels: []org.EnvHierarchyLevel{
					{Position: 1, IntegrationName: "orbs-dev"},
					{Position: 2, IntegrationName: "prod-integration"},
				},
			}, nil
		},
	)

	m, err := ex.Export(newExportOpts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Source.Org.Settings == nil {
		t.Fatal("OrgSettings is nil")
	}
	h := m.Source.Org.Settings.EnvironmentHierarchy
	if h == nil {
		t.Fatal("EnvironmentHierarchy is nil in manifest")
	}
	if h.Name != "prod-hierarchy" {
		t.Errorf("Name: got %q want %q", h.Name, "prod-hierarchy")
	}
	if len(h.Levels) != 2 {
		t.Fatalf("Levels count = %d, want 2", len(h.Levels))
	}
	if h.Levels[0].IntegrationName != "orbs-dev" {
		t.Errorf("Levels[0].IntegrationName: got %q", h.Levels[0].IntegrationName)
	}
}

func TestEnvironmentHierarchy_NilResponse_NotCaptured(t *testing.T) {
	ex := newFeatureExporter(
		nil, nil,
		func(orgUUID string) (*org.EnvHierarchyConfig, error) { return nil, nil },
	)

	m, err := ex.Export(newExportOpts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Source.Org.Settings != nil && m.Source.Org.Settings.EnvironmentHierarchy != nil {
		t.Errorf("expected EnvironmentHierarchy to be nil, got %+v",
			m.Source.Org.Settings.EnvironmentHierarchy)
	}
}

func TestEnvironmentHierarchy_APIError_Warning(t *testing.T) {
	ex := newFeatureExporter(
		nil, nil,
		func(orgUUID string) (*org.EnvHierarchyConfig, error) {
			return nil, errors.New("not authorized")
		},
	)

	m, err := ex.Export(newExportOpts)
	if err != nil {
		t.Fatalf("export must not fail on environment-hierarchy API error, got: %v", err)
	}
	if m.Source.Org.Settings != nil && m.Source.Org.Settings.EnvironmentHierarchy != nil {
		t.Error("EnvironmentHierarchy should be nil when API returns error")
	}
	found := false
	for _, w := range m.Warnings {
		if w.Code == "environment_hierarchy_unreadable" && w.Scope == "org" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected warning 'environment_hierarchy_unreadable', got: %+v", m.Warnings)
	}
}
