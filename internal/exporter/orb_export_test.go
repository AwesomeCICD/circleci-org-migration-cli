package exporter_test

import (
	"context"
	"errors"
	"testing"

	apiOrb "github.com/AwesomeCICD/circleci-org-migration-cli/api/orb"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/exporter"
)

// ---------------------------------------------------------------------------
// Fake orb API
// ---------------------------------------------------------------------------

type fakeOrbAPI struct {
	resolveNamespaceID func(name string) (string, error)
	listOrbs           func(namespaceID string) ([]apiOrb.OrbPackage, error)
	listStableVersions func(orbID string) ([]apiOrb.OrbVersion, error)
	getSource          func(versionID string) (string, error)
}

func (f *fakeOrbAPI) ResolveNamespaceID(_ context.Context, name string) (string, error) {
	if f.resolveNamespaceID != nil {
		return f.resolveNamespaceID(name)
	}
	return "ns-uuid", nil
}

func (f *fakeOrbAPI) ListOrbs(_ context.Context, namespaceID string) ([]apiOrb.OrbPackage, error) {
	if f.listOrbs != nil {
		return f.listOrbs(namespaceID)
	}
	return nil, nil
}

func (f *fakeOrbAPI) ListStableVersions(_ context.Context, orbID string) ([]apiOrb.OrbVersion, error) {
	if f.listStableVersions != nil {
		return f.listStableVersions(orbID)
	}
	return nil, nil
}

func (f *fakeOrbAPI) GetSource(_ context.Context, versionID string) (string, error) {
	if f.getSource != nil {
		return f.getSource(versionID)
	}
	return "version: 2.1\n", nil
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestExport_Orb_NamespaceSet_Captured verifies that when OrbNamespace is set
// and the orb client is wired, orbs appear in the manifest with versions.
func TestExport_Orb_NamespaceSet_Captured(t *testing.T) {
	ex := minimalExporter()
	ex.Orb = &fakeOrbAPI{
		resolveNamespaceID: func(name string) (string, error) {
			if name != "acme" {
				t.Errorf("ResolveNamespaceID called with %q, want %q", name, "acme")
			}
			return "ns-uuid-acme", nil
		},
		listOrbs: func(namespaceID string) ([]apiOrb.OrbPackage, error) {
			if namespaceID != "ns-uuid-acme" {
				t.Errorf("ListOrbs called with %q, want %q", namespaceID, "ns-uuid-acme")
			}
			return []apiOrb.OrbPackage{
				{ID: "orb-id-1", Name: "my-orb", IsPrivate: false, NamespaceID: namespaceID},
				{ID: "orb-id-2", Name: "priv-orb", IsPrivate: true, NamespaceID: namespaceID},
			}, nil
		},
		listStableVersions: func(orbID string) ([]apiOrb.OrbVersion, error) {
			switch orbID {
			case "orb-id-1":
				return []apiOrb.OrbVersion{
					{ID: "ver-1", Version: "1.0.0"},
					{ID: "ver-2", Version: "2.0.0"},
				}, nil
			case "orb-id-2":
				return []apiOrb.OrbVersion{
					{ID: "ver-3", Version: "0.1.0"},
				}, nil
			}
			return nil, nil
		},
		getSource: func(versionID string) (string, error) {
			return "version: 2.1\n# source for " + versionID, nil
		},
	}

	m, err := ex.Export(context.Background(), exporter.Options{
		OrgSlug:      "gh/acme",
		OrbNamespace: "acme",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if m.OrbNamespace != "acme" {
		t.Errorf("OrbNamespace = %q, want %q", m.OrbNamespace, "acme")
	}
	if len(m.Orbs) != 2 {
		t.Fatalf("Orbs count = %d, want 2", len(m.Orbs))
	}

	// Find each orb by name.
	orbByName := make(map[string]int)
	for i, o := range m.Orbs {
		orbByName[o.Name] = i
	}

	idx, ok := orbByName["acme/my-orb"]
	if !ok {
		t.Fatal("expected acme/my-orb in manifest")
	}
	myOrb := m.Orbs[idx]
	if myOrb.IsPrivate {
		t.Error("acme/my-orb should not be private")
	}
	if len(myOrb.Versions) != 2 {
		t.Errorf("acme/my-orb: expected 2 versions, got %d", len(myOrb.Versions))
	}
	// Versions must be semver-ascending.
	if myOrb.Versions[0].Version != "1.0.0" || myOrb.Versions[1].Version != "2.0.0" {
		t.Errorf("versions not in ascending order: %v", myOrb.Versions)
	}

	idx2, ok2 := orbByName["acme/priv-orb"]
	if !ok2 {
		t.Fatal("expected acme/priv-orb in manifest")
	}
	privOrb := m.Orbs[idx2]
	if !privOrb.IsPrivate {
		t.Error("acme/priv-orb should be private")
	}
}

// TestExport_Orb_EmptyNamespace_Skipped verifies that when OrbNamespace is
// empty, orb capture is silently skipped.
func TestExport_Orb_EmptyNamespace_Skipped(t *testing.T) {
	called := false
	ex := minimalExporter()
	ex.Orb = &fakeOrbAPI{
		resolveNamespaceID: func(name string) (string, error) {
			called = true
			return "ns-uuid", nil
		},
	}

	m, err := ex.Export(context.Background(), exporter.Options{
		OrgSlug:      "gh/acme",
		OrbNamespace: "", // empty → skip
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if called {
		t.Error("ResolveNamespaceID should not be called when OrbNamespace is empty")
	}
	if len(m.Orbs) != 0 {
		t.Errorf("expected 0 orbs, got %d", len(m.Orbs))
	}
}

// TestExport_Orb_NoOrbClient_Skipped verifies that when Orb is nil, orb
// capture is silently skipped even if OrbNamespace is set.
func TestExport_Orb_NoOrbClient_Skipped(t *testing.T) {
	ex := minimalExporter()
	// Orb is nil (default)

	m, err := ex.Export(context.Background(), exporter.Options{
		OrgSlug:      "gh/acme",
		OrbNamespace: "acme",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m.Orbs) != 0 {
		t.Errorf("expected 0 orbs (no client), got %d", len(m.Orbs))
	}
}

// TestExport_Orb_NamespaceResolveError_Warning verifies that a namespace
// resolve error results in a warning and does not fail the export.
func TestExport_Orb_NamespaceResolveError_Warning(t *testing.T) {
	ex := minimalExporter()
	ex.Orb = &fakeOrbAPI{
		resolveNamespaceID: func(name string) (string, error) {
			return "", errors.New("namespace not found")
		},
	}

	m, err := ex.Export(context.Background(), exporter.Options{
		OrgSlug:      "gh/acme",
		OrbNamespace: "nonexistent",
	})
	if err != nil {
		t.Fatalf("export should not fail on namespace error: %v", err)
	}
	if len(m.Orbs) != 0 {
		t.Errorf("expected 0 orbs on namespace error, got %d", len(m.Orbs))
	}

	var found bool
	for _, w := range m.Warnings {
		if w.Code == "orb_namespace_unresolvable" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected orb_namespace_unresolvable warning, got: %+v", m.Warnings)
	}
}

// TestExport_Orb_ListError_Warning verifies that a ListOrbs API error results
// in a warning and does not fail the export.
func TestExport_Orb_ListError_Warning(t *testing.T) {
	ex := minimalExporter()
	ex.Orb = &fakeOrbAPI{
		listOrbs: func(string) ([]apiOrb.OrbPackage, error) {
			return nil, errors.New("API unavailable")
		},
	}

	m, err := ex.Export(context.Background(), exporter.Options{
		OrgSlug:      "gh/acme",
		OrbNamespace: "acme",
	})
	if err != nil {
		t.Fatalf("export should not fail on list error: %v", err)
	}

	var found bool
	for _, w := range m.Warnings {
		if w.Code == "orb_list_unreadable" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected orb_list_unreadable warning, got: %+v", m.Warnings)
	}
}

// TestExport_Orb_VersionFetchError_Warning verifies that a version-fetch error
// on one orb records a warning for that orb but continues with others.
func TestExport_Orb_VersionFetchError_Warning(t *testing.T) {
	ex := minimalExporter()
	ex.Orb = &fakeOrbAPI{
		listOrbs: func(string) ([]apiOrb.OrbPackage, error) {
			return []apiOrb.OrbPackage{
				{ID: "orb-bad", Name: "broken-orb"},
				{ID: "orb-good", Name: "good-orb"},
			}, nil
		},
		listStableVersions: func(orbID string) ([]apiOrb.OrbVersion, error) {
			if orbID == "orb-bad" {
				return nil, errors.New("versions API error")
			}
			return []apiOrb.OrbVersion{{ID: "ver-1", Version: "1.0.0"}}, nil
		},
	}

	m, err := ex.Export(context.Background(), exporter.Options{
		OrgSlug:      "gh/acme",
		OrbNamespace: "acme",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Only the good orb should be in the manifest.
	if len(m.Orbs) != 1 {
		t.Errorf("expected 1 orb (broken one skipped), got %d: %v", len(m.Orbs), m.Orbs)
	}

	// A warning should have been recorded for the broken orb.
	var found bool
	for _, w := range m.Warnings {
		if w.Code == "orb_versions_unreadable" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected orb_versions_unreadable warning, got: %+v", m.Warnings)
	}
}

// TestExport_Orb_VersionsSortedSemver verifies that captured versions are
// sorted in semver-ascending order regardless of the API return order.
func TestExport_Orb_VersionsSortedSemver(t *testing.T) {
	ex := minimalExporter()
	ex.Orb = &fakeOrbAPI{
		listOrbs: func(string) ([]apiOrb.OrbPackage, error) {
			return []apiOrb.OrbPackage{{ID: "orb-1", Name: "my-orb"}}, nil
		},
		listStableVersions: func(string) ([]apiOrb.OrbVersion, error) {
			// Return out of order.
			return []apiOrb.OrbVersion{
				{ID: "v3", Version: "2.0.0"},
				{ID: "v1", Version: "0.1.0"},
				{ID: "v2", Version: "1.0.0"},
				{ID: "v4", Version: "1.10.0"},
				{ID: "v5", Version: "1.9.0"},
			}, nil
		},
	}

	m, err := ex.Export(context.Background(), exporter.Options{
		OrgSlug:      "gh/acme",
		OrbNamespace: "acme",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m.Orbs) != 1 {
		t.Fatalf("expected 1 orb, got %d", len(m.Orbs))
	}

	want := []string{"0.1.0", "1.0.0", "1.9.0", "1.10.0", "2.0.0"}
	got := make([]string, len(m.Orbs[0].Versions))
	for i, v := range m.Orbs[0].Versions {
		got[i] = v.Version
	}
	for i, w := range want {
		if i >= len(got) || got[i] != w {
			t.Errorf("versions[%d] = %q, want %q (full: %v)", i, func() string {
				if i < len(got) {
					return got[i]
				}
				return "<missing>"
			}(), w, got)
		}
	}
}
