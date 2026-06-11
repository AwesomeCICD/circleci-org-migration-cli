package exporter_test

// captured_fields_export_test.go contains focused unit tests for the new
// captured fields introduced by issue #131: StorageRetentionLimits, full
// V11FeatureFlags map, schedule actor login, and org namespace.

import (
	"context"
	"testing"

	"github.com/AwesomeCICD/circleci-org-migration-cli/api/org"
	"github.com/AwesomeCICD/circleci-org-migration-cli/api/project"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/exporter"
)

// ─────────────────────────────────────────────────────────────────────────────
// StorageRetentionLimits
// ─────────────────────────────────────────────────────────────────────────────

// TestStorageRetentionLimits_CapturedIntoManifest verifies that when the API
// returns limits with non-zero Max values they are stored in
// OrgSettings.StorageRetentionLimits.
func TestStorageRetentionLimits_CapturedIntoManifest(t *testing.T) {
	ex := storageRetentionExporter(func(orgUUID string) (*org.StorageRetention, error) {
		return &org.StorageRetention{
			Controls: org.StorageRetentionControls{
				CacheDays:     10,
				WorkspaceDays: 5,
				ArtifactDays:  1,
			},
			Limits: org.StorageRetentionLimits{
				Cache:     org.StorageRetentionBound{Min: 1, Max: 30},
				Workspace: org.StorageRetentionBound{Min: 1, Max: 15},
				Artifact:  org.StorageRetentionBound{Min: 1, Max: 730},
			},
		}, nil
	})

	m, err := ex.Export(context.Background(), retentionOpts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Source.Org.Settings == nil {
		t.Fatal("OrgSettings is nil")
	}
	lim := m.Source.Org.Settings.StorageRetentionLimits
	if lim == nil {
		t.Fatal("StorageRetentionLimits is nil in manifest")
	}
	if lim.Cache.Min != 1 || lim.Cache.Max != 30 {
		t.Errorf("Cache limits: got min=%d max=%d want min=1 max=30", lim.Cache.Min, lim.Cache.Max)
	}
	if lim.Workspace.Min != 1 || lim.Workspace.Max != 15 {
		t.Errorf("Workspace limits: got min=%d max=%d want min=1 max=15", lim.Workspace.Min, lim.Workspace.Max)
	}
	if lim.Artifact.Min != 1 || lim.Artifact.Max != 730 {
		t.Errorf("Artifact limits: got min=%d max=%d want min=1 max=730", lim.Artifact.Min, lim.Artifact.Max)
	}
}

// TestStorageRetentionLimits_NilWhenAllZero verifies that limits with all-zero
// Max values are NOT stored (avoids misleading zeroed-out limits in manifest).
func TestStorageRetentionLimits_NilWhenAllZero(t *testing.T) {
	ex := storageRetentionExporter(func(orgUUID string) (*org.StorageRetention, error) {
		return &org.StorageRetention{
			Controls: org.StorageRetentionControls{CacheDays: 3},
			// Limits all zero — not set by this server.
		}, nil
	})

	m, err := ex.Export(context.Background(), retentionOpts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Source.Org.Settings != nil && m.Source.Org.Settings.StorageRetentionLimits != nil {
		t.Error("StorageRetentionLimits should be nil when all limit Max values are 0")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Full V11FeatureFlags map
// ─────────────────────────────────────────────────────────────────────────────

// TestV11FeatureFlags_FullMapCaptured verifies that the full feature-flags map
// (not just the two well-known keys) is stored in V11FeatureFlags.
func TestV11FeatureFlags_FullMapCaptured(t *testing.T) {
	ex := &exporter.Exporter{
		Org: &fakeOrgAPI{
			getOrganization: func(string) (*org.Organization, error) { return defaultOrg(), nil },
		},
		Contexts: &fakeContextAPI{},
		Projects: &fakeProjectAPI{
			getProject: func(slug string) (*project.Project, error) {
				return &project.Project{ID: "proj-id", Name: "web", Slug: slug}, nil
			},
			getV11ProjectFeatureFlags: func(slug string) (map[string]bool, error) {
				return map[string]bool{
					"api-trigger-with-config": true,
					"drop-all-build-requests": false,
					"some-extra-flag":         true,
					"another-flag":            false,
				}, nil
			},
			listOrgProjects: func(orgID string) ([]project.OrgProject, error) {
				return []project.OrgProject{{Slug: "gh/myorg/web"}}, nil
			},
		},
	}

	m, err := ex.Export(context.Background(), exporter.Options{
		OrgSlug:         "gh/myorg",
		IncludeProjects: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m.Projects) == 0 {
		t.Fatal("no projects in manifest")
	}
	p := m.Projects[0]
	if p.Settings == nil {
		t.Fatal("Settings is nil")
	}
	if p.Settings.V11FeatureFlags == nil {
		t.Fatal("V11FeatureFlags is nil")
	}
	// Well-known keys.
	if v, ok := p.Settings.V11FeatureFlags["api-trigger-with-config"]; !ok || !v {
		t.Errorf("api-trigger-with-config: got %v (ok=%v), want true", v, ok)
	}
	if v, ok := p.Settings.V11FeatureFlags["drop-all-build-requests"]; !ok || v {
		t.Errorf("drop-all-build-requests: got %v (ok=%v), want false", v, ok)
	}
	// Extra flags must also be present.
	if v, ok := p.Settings.V11FeatureFlags["some-extra-flag"]; !ok || !v {
		t.Errorf("some-extra-flag: got %v (ok=%v), want true", v, ok)
	}
	if _, ok := p.Settings.V11FeatureFlags["another-flag"]; !ok {
		t.Errorf("another-flag should be present in V11FeatureFlags")
	}
	// Backward-compat explicit fields must still be set.
	if p.Settings.APITriggerWithConfig == nil || !*p.Settings.APITriggerWithConfig {
		t.Errorf("APITriggerWithConfig (explicit field) should be true")
	}
	if p.Settings.DropAllBuildRequests == nil || *p.Settings.DropAllBuildRequests {
		t.Errorf("DropAllBuildRequests (explicit field) should be false")
	}
}

// TestV11FeatureFlags_EmptyMapProducesNilField verifies that when all flags are
// absent (empty map from API), V11FeatureFlags is nil (omitempty behaviour).
func TestV11FeatureFlags_EmptyMapProducesNilField(t *testing.T) {
	ex := &exporter.Exporter{
		Org: &fakeOrgAPI{
			getOrganization: func(string) (*org.Organization, error) { return defaultOrg(), nil },
		},
		Contexts: &fakeContextAPI{},
		Projects: &fakeProjectAPI{
			getProject: func(slug string) (*project.Project, error) {
				return &project.Project{ID: "proj-id", Name: "web", Slug: slug}, nil
			},
			getV11ProjectFeatureFlags: func(slug string) (map[string]bool, error) {
				return map[string]bool{}, nil
			},
			listOrgProjects: func(orgID string) ([]project.OrgProject, error) {
				return []project.OrgProject{{Slug: "gh/myorg/web"}}, nil
			},
		},
	}

	m, err := ex.Export(context.Background(), exporter.Options{
		OrgSlug:         "gh/myorg",
		IncludeProjects: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m.Projects) == 0 {
		t.Fatal("no projects")
	}
	p := m.Projects[0]
	if p.Settings != nil && p.Settings.V11FeatureFlags != nil {
		t.Errorf("V11FeatureFlags should be nil for empty flag map, got %v", p.Settings.V11FeatureFlags)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Schedule actor login
// ─────────────────────────────────────────────────────────────────────────────

// TestScheduleActorLogin_CapturedIntoManifest verifies that the actor.login
// from a schedule API response is stored in the manifest Schedule.ActorLogin.
func TestScheduleActorLogin_CapturedIntoManifest(t *testing.T) {
	ex := &exporter.Exporter{
		Org: &fakeOrgAPI{
			getOrganization: func(string) (*org.Organization, error) { return defaultOrg(), nil },
		},
		Contexts: &fakeContextAPI{},
		Projects: &fakeProjectAPI{
			getProject: func(slug string) (*project.Project, error) {
				return &project.Project{ID: "proj-id", Name: "web", Slug: slug}, nil
			},
			listSchedules: func(slug string) ([]project.Schedule, error) {
				return []project.Schedule{
					{
						ID:          "sched-uuid-1",
						Name:        "nightly",
						Description: "Nightly build",
						Actor:       project.ScheduleActor{Login: "pipeline-bot"},
					},
				}, nil
			},
			listOrgProjects: func(orgID string) ([]project.OrgProject, error) {
				return []project.OrgProject{{Slug: "gh/myorg/web"}}, nil
			},
		},
	}

	m, err := ex.Export(context.Background(), exporter.Options{
		OrgSlug:         "gh/myorg",
		IncludeProjects: true,
		IncludeExtras:   true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m.Projects) == 0 {
		t.Fatal("no projects")
	}
	p := m.Projects[0]
	if len(p.Schedules) == 0 {
		t.Fatal("no schedules in project")
	}
	if p.Schedules[0].ActorLogin != "pipeline-bot" {
		t.Errorf("ActorLogin: got %q want %q", p.Schedules[0].ActorLogin, "pipeline-bot")
	}
}

// TestScheduleActorLogin_EmptyWhenAbsent verifies that when the actor login is
// absent from the API response, ActorLogin is empty in the manifest.
func TestScheduleActorLogin_EmptyWhenAbsent(t *testing.T) {
	ex := &exporter.Exporter{
		Org: &fakeOrgAPI{
			getOrganization: func(string) (*org.Organization, error) { return defaultOrg(), nil },
		},
		Contexts: &fakeContextAPI{},
		Projects: &fakeProjectAPI{
			getProject: func(slug string) (*project.Project, error) {
				return &project.Project{ID: "proj-id", Name: "web", Slug: slug}, nil
			},
			listSchedules: func(slug string) ([]project.Schedule, error) {
				return []project.Schedule{
					{ID: "sched-2", Name: "weekly", Actor: project.ScheduleActor{}},
				}, nil
			},
			listOrgProjects: func(orgID string) ([]project.OrgProject, error) {
				return []project.OrgProject{{Slug: "gh/myorg/web"}}, nil
			},
		},
	}

	m, err := ex.Export(context.Background(), exporter.Options{
		OrgSlug:         "gh/myorg",
		IncludeProjects: true,
		IncludeExtras:   true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m.Projects) == 0 || len(m.Projects[0].Schedules) == 0 {
		t.Fatal("no projects/schedules")
	}
	if m.Projects[0].Schedules[0].ActorLogin != "" {
		t.Errorf("ActorLogin should be empty when absent, got %q", m.Projects[0].Schedules[0].ActorLogin)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Org namespace
// ─────────────────────────────────────────────────────────────────────────────

// TestOrbNamespace_DerivedFromOrgName verifies that when the org has a Name,
// OrbNamespace is set to the org name.
func TestOrbNamespace_DerivedFromOrgName(t *testing.T) {
	ex := &exporter.Exporter{
		Org: &fakeOrgAPI{
			getOrganization: func(string) (*org.Organization, error) {
				return &org.Organization{
					ID:      "org-uuid-123",
					Name:    "acme-corp",
					Slug:    "gh/acme-corp",
					VCSType: "github",
				}, nil
			},
		},
		Contexts: &fakeContextAPI{},
		Projects: &fakeProjectAPI{},
	}

	m, err := ex.Export(context.Background(), exporter.Options{OrgSlug: "gh/acme-corp"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Source.Org.Settings == nil {
		t.Fatal("OrgSettings is nil")
	}
	if m.Source.Org.Settings.OrbNamespace != "acme-corp" {
		t.Errorf("OrbNamespace: got %q want %q", m.Source.Org.Settings.OrbNamespace, "acme-corp")
	}
}
