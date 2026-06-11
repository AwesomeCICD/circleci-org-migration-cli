package exporter_test

import (
	"errors"
	"strings"
	"testing"

	cctx "github.com/AwesomeCICD/circleci-org-migration-cli/api/context"
	"github.com/AwesomeCICD/circleci-org-migration-cli/api/org"
	"github.com/AwesomeCICD/circleci-org-migration-cli/api/project"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/exporter"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
)

// ---------------------------------------------------------------------------
// Fake implementations
// ---------------------------------------------------------------------------

type fakeOrgAPI struct {
	getOrganization           func(slugOrID string) (*org.Organization, error)
	getOrgSettings            func(vcsType, orgName string) (*org.OrgSettings, error)
	getFeatureFlags           func(vcsType, orgName string) (map[string]bool, error)
	getOIDCClaims             func(orgID string) ([]string, string, error)
	getURLOrbAllowList        func(slugOrID string) ([]org.URLOrbAllowEntry, error)
	getPolicyBundle           func(ownerID string) (map[string]string, error)
	getPolicyEnf              func(ownerID string) (bool, error)
	getAuditLogConfigs        func(orgID string) ([]org.AuditLogConfig, error)
	getSSOEnforced            func(orgID string) (bool, error)
	getSSOConnection          func(orgID string) (map[string]any, bool, error)
	getOTelExporters          func(orgID string) ([]org.OTelExporter, error)
	getContacts               func(orgID string) ([]string, []string, error)
	listGroups                func(orgID string) ([]org.Group, error)
	getStorageRetention       func(orgUUID string) (*org.StorageRetention, error)
	getBudgets                func(orgUUID string) ([]org.Budget, error)
	getBlockUnregisteredUsers func(orgUUID string) (bool, error)
	getOrgOrbs                func(orgUUID string) ([]org.OrgOrb, error)
	getReleaseTrackerSettings func(orgUUID string) (*org.ReleaseTrackerSettings, error)
	getEnvironmentHierarchy   func(orgUUID string) (*org.EnvHierarchyConfig, error)
}

func (f *fakeOrgAPI) GetOrganization(slugOrID string) (*org.Organization, error) {
	if f.getOrganization != nil {
		return f.getOrganization(slugOrID)
	}
	return nil, errors.New("fakeOrgAPI: GetOrganization not configured")
}

func (f *fakeOrgAPI) GetOrgSettings(vcsType, orgName string) (*org.OrgSettings, error) {
	if f.getOrgSettings != nil {
		return f.getOrgSettings(vcsType, orgName)
	}
	return nil, nil
}

func (f *fakeOrgAPI) GetFeatureFlags(vcsType, orgName string) (map[string]bool, error) {
	if f.getFeatureFlags != nil {
		return f.getFeatureFlags(vcsType, orgName)
	}
	return nil, nil
}

func (f *fakeOrgAPI) GetOIDCClaims(orgID string) ([]string, string, error) {
	if f.getOIDCClaims != nil {
		return f.getOIDCClaims(orgID)
	}
	return nil, "", nil
}

func (f *fakeOrgAPI) GetURLOrbAllowList(slugOrID string) ([]org.URLOrbAllowEntry, error) {
	if f.getURLOrbAllowList != nil {
		return f.getURLOrbAllowList(slugOrID)
	}
	return nil, nil
}

func (f *fakeOrgAPI) GetPolicyBundle(ownerID string) (map[string]string, error) {
	if f.getPolicyBundle != nil {
		return f.getPolicyBundle(ownerID)
	}
	return nil, nil
}

func (f *fakeOrgAPI) GetPolicyEnforcement(ownerID string) (bool, error) {
	if f.getPolicyEnf != nil {
		return f.getPolicyEnf(ownerID)
	}
	return false, nil
}

func (f *fakeOrgAPI) GetAuditLogConfigs(orgID string) ([]org.AuditLogConfig, error) {
	if f.getAuditLogConfigs != nil {
		return f.getAuditLogConfigs(orgID)
	}
	return nil, nil
}

func (f *fakeOrgAPI) GetSSOEnforced(orgID string) (bool, error) {
	if f.getSSOEnforced != nil {
		return f.getSSOEnforced(orgID)
	}
	return false, nil
}

func (f *fakeOrgAPI) GetSSOConnection(orgID string) (map[string]any, bool, error) {
	if f.getSSOConnection != nil {
		return f.getSSOConnection(orgID)
	}
	return nil, false, nil
}

func (f *fakeOrgAPI) GetOTelExporters(orgID string) ([]org.OTelExporter, error) {
	if f.getOTelExporters != nil {
		return f.getOTelExporters(orgID)
	}
	return nil, nil
}

func (f *fakeOrgAPI) GetContacts(orgID string) ([]string, []string, error) {
	if f.getContacts != nil {
		return f.getContacts(orgID)
	}
	return nil, nil, nil
}

func (f *fakeOrgAPI) ListGroups(orgID string) ([]org.Group, error) {
	if f.listGroups != nil {
		return f.listGroups(orgID)
	}
	return nil, nil
}

func (f *fakeOrgAPI) GetStorageRetention(orgUUID string) (*org.StorageRetention, error) {
	if f.getStorageRetention != nil {
		return f.getStorageRetention(orgUUID)
	}
	return nil, nil
}

func (f *fakeOrgAPI) GetBudgets(orgUUID string) ([]org.Budget, error) {
	if f.getBudgets != nil {
		return f.getBudgets(orgUUID)
	}
	return nil, nil
}

func (f *fakeOrgAPI) GetBlockUnregisteredUsers(orgUUID string) (bool, error) {
	if f.getBlockUnregisteredUsers != nil {
		return f.getBlockUnregisteredUsers(orgUUID)
	}
	return false, nil
}

func (f *fakeOrgAPI) GetOrgOrbs(orgUUID string) ([]org.OrgOrb, error) {
	if f.getOrgOrbs != nil {
		return f.getOrgOrbs(orgUUID)
	}
	return nil, nil
}

func (f *fakeOrgAPI) GetReleaseTrackerSettings(orgUUID string) (*org.ReleaseTrackerSettings, error) {
	if f.getReleaseTrackerSettings != nil {
		return f.getReleaseTrackerSettings(orgUUID)
	}
	return nil, nil
}

func (f *fakeOrgAPI) GetEnvironmentHierarchy(orgUUID string) (*org.EnvHierarchyConfig, error) {
	if f.getEnvironmentHierarchy != nil {
		return f.getEnvironmentHierarchy(orgUUID)
	}
	return nil, nil
}

type fakeContextAPI struct {
	listContexts     func(ownerID, ownerSlug string) ([]cctx.Context, error)
	listEnvVars      func(contextID string) ([]cctx.EnvVar, error)
	listRestrictions func(contextID string) ([]cctx.Restriction, error)
}

func (f *fakeContextAPI) ListContexts(ownerID, ownerSlug string) ([]cctx.Context, error) {
	if f.listContexts != nil {
		return f.listContexts(ownerID, ownerSlug)
	}
	return nil, nil
}

func (f *fakeContextAPI) ListEnvVars(contextID string) ([]cctx.EnvVar, error) {
	if f.listEnvVars != nil {
		return f.listEnvVars(contextID)
	}
	return nil, nil
}

func (f *fakeContextAPI) ListRestrictions(contextID string) ([]cctx.Restriction, error) {
	if f.listRestrictions != nil {
		return f.listRestrictions(contextID)
	}
	return nil, nil
}

type fakeProjectAPI struct {
	getProject                func(slug string) (*project.Project, error)
	getSettings               func(provider, orgName, proj string) (*project.AdvancedSettings, error)
	listEnvVars               func(slug string) ([]project.EnvVar, error)
	listCheckoutKeys          func(slug string) ([]project.CheckoutKey, error)
	listWebhooks              func(projectID string) ([]project.Webhook, error)
	listSchedules             func(slug string) ([]project.Schedule, error)
	followedProjectsForOrg    func(orgName string) ([]project.FollowedProject, error)
	getProjectOIDCClaims      func(orgID, projID string) ([]string, string, error)
	getV11ProjectFeatureFlags func(slug string) (map[string]bool, error)
	listOrgProjects           func(orgID string) ([]project.OrgProject, error)
	listPipelineDefinitions   func(projectID string) ([]project.PipelineDefinition, error)
	listTriggers              func(projectID, defID string) ([]project.Trigger, error)
}

func (f *fakeProjectAPI) GetProject(slug string) (*project.Project, error) {
	if f.getProject != nil {
		return f.getProject(slug)
	}
	return &project.Project{Slug: slug, ID: "proj-id", Name: slug}, nil
}

func (f *fakeProjectAPI) GetSettings(provider, orgName, proj string) (*project.AdvancedSettings, error) {
	if f.getSettings != nil {
		return f.getSettings(provider, orgName, proj)
	}
	return nil, nil
}

func (f *fakeProjectAPI) ListEnvVars(slug string) ([]project.EnvVar, error) {
	if f.listEnvVars != nil {
		return f.listEnvVars(slug)
	}
	return nil, nil
}

func (f *fakeProjectAPI) ListCheckoutKeys(slug string) ([]project.CheckoutKey, error) {
	if f.listCheckoutKeys != nil {
		return f.listCheckoutKeys(slug)
	}
	return nil, nil
}

func (f *fakeProjectAPI) ListWebhooks(projectID string) ([]project.Webhook, error) {
	if f.listWebhooks != nil {
		return f.listWebhooks(projectID)
	}
	return nil, nil
}

func (f *fakeProjectAPI) ListSchedules(slug string) ([]project.Schedule, error) {
	if f.listSchedules != nil {
		return f.listSchedules(slug)
	}
	return nil, nil
}

func (f *fakeProjectAPI) FollowedProjectsForOrg(orgName string) ([]project.FollowedProject, error) {
	if f.followedProjectsForOrg != nil {
		return f.followedProjectsForOrg(orgName)
	}
	return nil, nil
}

func (f *fakeProjectAPI) GetProjectOIDCClaims(orgID, projID string) ([]string, string, error) {
	if f.getProjectOIDCClaims != nil {
		return f.getProjectOIDCClaims(orgID, projID)
	}
	return nil, "", nil
}

func (f *fakeProjectAPI) GetV11ProjectFeatureFlags(slug string) (map[string]bool, error) {
	if f.getV11ProjectFeatureFlags != nil {
		return f.getV11ProjectFeatureFlags(slug)
	}
	return nil, nil
}

func (f *fakeProjectAPI) ListOrgProjects(orgID string) ([]project.OrgProject, error) {
	if f.listOrgProjects != nil {
		return f.listOrgProjects(orgID)
	}
	// Default: return an empty list (no projects discovered via private API).
	return nil, nil
}

func (f *fakeProjectAPI) ListPipelineDefinitions(projectID string) ([]project.PipelineDefinition, error) {
	if f.listPipelineDefinitions != nil {
		return f.listPipelineDefinitions(projectID)
	}
	return nil, nil
}

func (f *fakeProjectAPI) ListTriggers(projectID, defID string) ([]project.Trigger, error) {
	if f.listTriggers != nil {
		return f.listTriggers(projectID, defID)
	}
	return nil, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// defaultOrg returns a standard GitHub OAuth org for testing.
func defaultOrg() *org.Organization {
	return &org.Organization{
		ID:      "org-uuid-123",
		Name:    "myorg",
		Slug:    "gh/myorg",
		VCSType: "github",
	}
}

// minimalExporter builds an exporter with a fixed org and empty API fakes.
func minimalExporter() *exporter.Exporter {
	return &exporter.Exporter{
		Org: &fakeOrgAPI{
			getOrganization: func(string) (*org.Organization, error) { return defaultOrg(), nil },
		},
		Contexts: &fakeContextAPI{},
		Projects: &fakeProjectAPI{},
	}
}

// ---------------------------------------------------------------------------
// Org resolution
// ---------------------------------------------------------------------------

func TestExport_OrgResolution(t *testing.T) {
	ex := minimalExporter()
	m, err := ex.Export(exporter.Options{
		Host:    "https://circleci.com",
		OrgSlug: "gh/myorg",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := manifest.Org{
		Slug:    "gh/myorg",
		ID:      "org-uuid-123",
		Name:    "myorg",
		VCSType: "github",
	}
	got := m.Source.Org
	if got.Slug != want.Slug {
		t.Errorf("Slug: got %q want %q", got.Slug, want.Slug)
	}
	if got.ID != want.ID {
		t.Errorf("ID: got %q want %q", got.ID, want.ID)
	}
	if got.Name != want.Name {
		t.Errorf("Name: got %q want %q", got.Name, want.Name)
	}
	if got.VCSType != want.VCSType {
		t.Errorf("VCSType: got %q want %q", got.VCSType, want.VCSType)
	}
}

func TestExport_SourceHost(t *testing.T) {
	ex := minimalExporter()
	m, err := ex.Export(exporter.Options{
		Host:    "https://custom.example.com",
		OrgSlug: "gh/myorg",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Source.Host != "https://custom.example.com" {
		t.Errorf("Source.Host: got %q want %q", m.Source.Host, "https://custom.example.com")
	}
}

func TestExport_OrgSettingsCaptured_GHSlug(t *testing.T) {
	flagsCalled := false
	ex := &exporter.Exporter{
		Org: &fakeOrgAPI{
			getOrganization: func(string) (*org.Organization, error) { return defaultOrg(), nil },
			getFeatureFlags: func(vcsType, orgName string) (map[string]bool, error) {
				flagsCalled = true
				if vcsType != "github" {
					t.Errorf("GetFeatureFlags vcsType: got %q want %q", vcsType, "github")
				}
				if orgName != "myorg" {
					t.Errorf("GetFeatureFlags orgName: got %q want %q", orgName, "myorg")
				}
				return map[string]bool{
					"require_context_group_restriction": true,
					"allow_certified_public_orbs":       true,
				}, nil
			},
		},
		Contexts: &fakeContextAPI{},
		Projects: &fakeProjectAPI{},
	}

	m, err := ex.Export(exporter.Options{OrgSlug: "gh/myorg"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !flagsCalled {
		t.Error("GetFeatureFlags was not called for gh/ slug")
	}
	if m.Source.Org.Settings == nil {
		t.Fatal("Settings is nil")
	}
	if m.Source.Org.Settings.RequireContextGroupRestriction == nil {
		t.Fatal("RequireContextGroupRestriction is nil")
	}
	if !*m.Source.Org.Settings.RequireContextGroupRestriction {
		t.Error("RequireContextGroupRestriction should be true")
	}
	if m.Source.Org.Settings.FeatureFlags["allow_certified_public_orbs"] != true {
		t.Error("FeatureFlags should contain allow_certified_public_orbs=true")
	}
}

// circleci/<uuid> (GitHub-App / standalone) orgs DO capture v1.1 feature flags:
// the settings endpoint accepts vcs="circleci", name=<uuid>.
func TestExport_OrgFeatureFlags_CircleCISlug(t *testing.T) {
	var gotVCS, gotName string
	ex := &exporter.Exporter{
		Org: &fakeOrgAPI{
			getOrganization: func(string) (*org.Organization, error) {
				return &org.Organization{
					ID:      "some-uuid",
					Name:    "myorg",
					Slug:    "circleci/some-uuid",
					VCSType: "circleci",
				}, nil
			},
			getFeatureFlags: func(vcsType, orgName string) (map[string]bool, error) {
				gotVCS, gotName = vcsType, orgName
				return map[string]bool{"allow_api_trigger_with_config": true}, nil
			},
		},
		Contexts: &fakeContextAPI{},
		Projects: &fakeProjectAPI{},
	}

	m, err := ex.Export(exporter.Options{OrgSlug: "circleci/some-uuid"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotVCS != "circleci" || gotName != "some-uuid" {
		t.Errorf("GetFeatureFlags called with (%q,%q), want (circleci,some-uuid)", gotVCS, gotName)
	}
	if m.Source.Org.Settings == nil || !m.Source.Org.Settings.FeatureFlags["allow_api_trigger_with_config"] {
		t.Error("expected captured feature flags for circleci/ slug org")
	}
}

func TestExport_GetOrganizationError_ReturnsError(t *testing.T) {
	ex := &exporter.Exporter{
		Org: &fakeOrgAPI{
			getOrganization: func(string) (*org.Organization, error) {
				return nil, errors.New("network failure")
			},
		},
		Contexts: &fakeContextAPI{},
		Projects: &fakeProjectAPI{},
	}

	_, err := ex.Export(exporter.Options{OrgSlug: "gh/myorg"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "resolving organization") {
		t.Errorf("error %q does not mention 'resolving organization'", err.Error())
	}
}

// ---------------------------------------------------------------------------
// Contexts
// ---------------------------------------------------------------------------

func TestExport_Contexts_EnvVarNames(t *testing.T) {
	ex := &exporter.Exporter{
		Org: &fakeOrgAPI{
			getOrganization: func(string) (*org.Organization, error) { return defaultOrg(), nil },
		},
		Contexts: &fakeContextAPI{
			listContexts: func(ownerID, ownerSlug string) ([]cctx.Context, error) {
				return []cctx.Context{
					{ID: "ctx-1", Name: "deploy-prod", CreatedAt: "2024-01-01T00:00:00Z"},
				}, nil
			},
			listEnvVars: func(contextID string) ([]cctx.EnvVar, error) {
				return []cctx.EnvVar{
					{Name: "AWS_SECRET", CreatedAt: "2024-01-02T00:00:00Z"},
					{Name: "DB_PASSWORD", CreatedAt: "2024-01-03T00:00:00Z"},
				}, nil
			},
		},
		Projects: &fakeProjectAPI{},
	}

	m, err := ex.Export(exporter.Options{
		OrgSlug:         "gh/myorg",
		IncludeContexts: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m.Contexts) != 1 {
		t.Fatalf("expected 1 context, got %d", len(m.Contexts))
	}
	ctx := m.Contexts[0]
	if ctx.Name != "deploy-prod" {
		t.Errorf("context name: got %q want %q", ctx.Name, "deploy-prod")
	}
	if len(ctx.EnvVars) != 2 {
		t.Fatalf("expected 2 env vars, got %d", len(ctx.EnvVars))
	}
	names := map[string]bool{}
	for _, v := range ctx.EnvVars {
		names[v.Name] = true
	}
	if !names["AWS_SECRET"] || !names["DB_PASSWORD"] {
		t.Errorf("env var names not as expected: %v", names)
	}
}

func TestExport_Contexts_ContextValuesExcludedWarning(t *testing.T) {
	ex := &exporter.Exporter{
		Org: &fakeOrgAPI{
			getOrganization: func(string) (*org.Organization, error) { return defaultOrg(), nil },
		},
		Contexts: &fakeContextAPI{
			listContexts: func(ownerID, ownerSlug string) ([]cctx.Context, error) {
				return []cctx.Context{{ID: "ctx-1", Name: "prod"}}, nil
			},
			listEnvVars: func(contextID string) ([]cctx.EnvVar, error) {
				return []cctx.EnvVar{{Name: "SECRET"}}, nil
			},
		},
		Projects: &fakeProjectAPI{},
	}

	m, err := ex.Export(exporter.Options{OrgSlug: "gh/myorg", IncludeContexts: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	found := false
	for _, w := range m.Warnings {
		if w.Code == "context_values_excluded" {
			found = true
		}
	}
	if !found {
		t.Error("expected context_values_excluded warning, not found")
	}
}

func TestExport_Contexts_Restrictions(t *testing.T) {
	ex := &exporter.Exporter{
		Org: &fakeOrgAPI{
			getOrganization: func(string) (*org.Organization, error) { return defaultOrg(), nil },
		},
		Contexts: &fakeContextAPI{
			listContexts: func(ownerID, ownerSlug string) ([]cctx.Context, error) {
				return []cctx.Context{{ID: "ctx-1", Name: "prod"}}, nil
			},
			listRestrictions: func(contextID string) ([]cctx.Restriction, error) {
				return []cctx.Restriction{
					{ID: "r1", Type: "project", Value: "proj-uuid-1", Name: "web"},
					{ID: "r2", Type: "expression", Value: "project.slug == \"gh/myorg/api\"", Name: ""},
				}, nil
			},
		},
		Projects: &fakeProjectAPI{},
	}

	m, err := ex.Export(exporter.Options{OrgSlug: "gh/myorg", IncludeContexts: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m.Contexts) != 1 {
		t.Fatalf("expected 1 context, got %d", len(m.Contexts))
	}
	if len(m.Contexts[0].Restrictions) != 2 {
		t.Fatalf("expected 2 restrictions, got %d", len(m.Contexts[0].Restrictions))
	}
}

func TestExport_Contexts_GroupRestriction_ResolvedName(t *testing.T) {
	ex := &exporter.Exporter{
		Org: &fakeOrgAPI{
			getOrganization: func(string) (*org.Organization, error) { return defaultOrg(), nil },
		},
		Contexts: &fakeContextAPI{
			listContexts: func(ownerID, ownerSlug string) ([]cctx.Context, error) {
				return []cctx.Context{{ID: "ctx-1", Name: "prod"}}, nil
			},
			listRestrictions: func(contextID string) ([]cctx.Restriction, error) {
				// v2 returns the group name directly in the restriction.
				return []cctx.Restriction{
					{ID: "r1", Type: "group", Value: "group-uuid-1", Name: "security-team"},
				}, nil
			},
		},
		Projects: &fakeProjectAPI{},
	}

	m, err := ex.Export(exporter.Options{OrgSlug: "gh/myorg", IncludeContexts: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m.Contexts) != 1 || len(m.Contexts[0].Restrictions) != 1 {
		t.Fatalf("unexpected contexts/restrictions: %+v", m.Contexts)
	}
	r := m.Contexts[0].Restrictions[0]
	if r.Type != "group" || r.Name != "security-team" {
		t.Errorf("restriction: got %+v, want type=group name=security-team", r)
	}
	// Security groups are derived from the group-type restriction.
	if len(m.Contexts[0].SecurityGroups) != 1 || m.Contexts[0].SecurityGroups[0].Name != "security-team" {
		t.Errorf("security groups: %+v", m.Contexts[0].SecurityGroups)
	}
}

func TestExport_Contexts_GroupRestriction_ManualWarning(t *testing.T) {
	ex := &exporter.Exporter{
		Org: &fakeOrgAPI{
			getOrganization: func(string) (*org.Organization, error) { return defaultOrg(), nil },
		},
		Contexts: &fakeContextAPI{
			listContexts: func(ownerID, ownerSlug string) ([]cctx.Context, error) {
				return []cctx.Context{{ID: "ctx-1", Name: "prod"}}, nil
			},
			listRestrictions: func(contextID string) ([]cctx.Restriction, error) {
				return []cctx.Restriction{
					{ID: "r1", Type: "group", Value: "group-uuid-1", Name: ""},
				}, nil
			},
		},
		Projects: &fakeProjectAPI{},
	}

	m, err := ex.Export(exporter.Options{OrgSlug: "gh/myorg", IncludeContexts: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	found := false
	for _, w := range m.Warnings {
		if w.Code == "group_restriction_manual" {
			found = true
		}
	}
	if !found {
		t.Error("expected group_restriction_manual warning, not found")
	}
}

func TestExport_Contexts_GroupRestriction_AllMembersNoWarning(t *testing.T) {
	// When the group restriction value == org ID (the default "All members"
	// restriction), group_restriction_manual must NOT be emitted.
	orgID := "org-uuid-123" // matches defaultOrg().ID
	ex := &exporter.Exporter{
		Org: &fakeOrgAPI{
			getOrganization: func(string) (*org.Organization, error) { return defaultOrg(), nil },
		},
		Contexts: &fakeContextAPI{
			listContexts: func(ownerID, ownerSlug string) ([]cctx.Context, error) {
				return []cctx.Context{{ID: "ctx-1", Name: "prod"}}, nil
			},
			listRestrictions: func(contextID string) ([]cctx.Restriction, error) {
				return []cctx.Restriction{
					// All-members default restriction: value == org ID.
					{ID: "r1", Type: "group", Value: orgID, Name: "All members"},
				}, nil
			},
		},
		Projects: &fakeProjectAPI{},
	}

	m, err := ex.Export(exporter.Options{OrgSlug: "gh/myorg", IncludeContexts: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, w := range m.Warnings {
		if w.Code == "group_restriction_manual" {
			t.Errorf("group_restriction_manual must NOT be emitted for the All-members default restriction")
		}
	}
	// The restriction must still be recorded in the manifest.
	if len(m.Contexts) != 1 || len(m.Contexts[0].Restrictions) != 1 {
		t.Fatalf("expected context with 1 restriction recorded, got: %+v", m.Contexts)
	}
}

func TestExport_Contexts_GroupRestriction_NonAllMembersWarning(t *testing.T) {
	// For a non-All-members group restriction, group_restriction_manual IS emitted.
	ex := &exporter.Exporter{
		Org: &fakeOrgAPI{
			getOrganization: func(string) (*org.Organization, error) { return defaultOrg(), nil },
		},
		Contexts: &fakeContextAPI{
			listContexts: func(ownerID, ownerSlug string) ([]cctx.Context, error) {
				return []cctx.Context{{ID: "ctx-1", Name: "prod"}}, nil
			},
			listRestrictions: func(contextID string) ([]cctx.Restriction, error) {
				return []cctx.Restriction{
					// Real team restriction — different from org ID.
					{ID: "r2", Type: "group", Value: "team-uuid-456", Name: "engineering"},
				}, nil
			},
		},
		Projects: &fakeProjectAPI{},
	}

	m, err := ex.Export(exporter.Options{OrgSlug: "gh/myorg", IncludeContexts: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	found := false
	for _, w := range m.Warnings {
		if w.Code == "group_restriction_manual" {
			found = true
		}
	}
	if !found {
		t.Error("expected group_restriction_manual warning for non-All-members group restriction")
	}
}

func TestExport_Contexts_SecurityGroups(t *testing.T) {
	ex := &exporter.Exporter{
		Org: &fakeOrgAPI{
			getOrganization: func(string) (*org.Organization, error) { return defaultOrg(), nil },
		},
		Contexts: &fakeContextAPI{
			listContexts: func(ownerID, ownerSlug string) ([]cctx.Context, error) {
				return []cctx.Context{{ID: "ctx-1", Name: "prod"}}, nil
			},
			listRestrictions: func(contextID string) ([]cctx.Restriction, error) {
				return []cctx.Restriction{
					{ID: "r1", Type: "group", Value: "sg-1", Name: "eng-team"},
				}, nil
			},
		},
		Projects: &fakeProjectAPI{},
	}

	m, err := ex.Export(exporter.Options{OrgSlug: "gh/myorg", IncludeContexts: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m.Contexts) != 1 {
		t.Fatalf("expected 1 context, got %d", len(m.Contexts))
	}
	if len(m.Contexts[0].SecurityGroups) != 1 {
		t.Fatalf("expected 1 security group, got %d", len(m.Contexts[0].SecurityGroups))
	}
	sg := m.Contexts[0].SecurityGroups[0]
	if sg.ID != "sg-1" || sg.Name != "eng-team" {
		t.Errorf("security group: %+v", sg)
	}
}

func TestExport_Contexts_EnvVarError_IsWarning(t *testing.T) {
	ex := &exporter.Exporter{
		Org: &fakeOrgAPI{
			getOrganization: func(string) (*org.Organization, error) { return defaultOrg(), nil },
		},
		Contexts: &fakeContextAPI{
			listContexts: func(ownerID, ownerSlug string) ([]cctx.Context, error) {
				return []cctx.Context{{ID: "ctx-1", Name: "prod"}}, nil
			},
			listEnvVars: func(contextID string) ([]cctx.EnvVar, error) {
				return nil, errors.New("permission denied")
			},
		},
		Projects: &fakeProjectAPI{},
	}

	m, err := ex.Export(exporter.Options{OrgSlug: "gh/myorg", IncludeContexts: true})
	if err != nil {
		t.Fatalf("per-resource error should not fail Export, got: %v", err)
	}
	found := false
	for _, w := range m.Warnings {
		if w.Code == "env_vars_unreadable" {
			found = true
		}
	}
	if !found {
		t.Error("expected env_vars_unreadable warning, not found")
	}
}

func TestExport_Contexts_ListContextsError_IsWarning(t *testing.T) {
	ex := &exporter.Exporter{
		Org: &fakeOrgAPI{
			getOrganization: func(string) (*org.Organization, error) { return defaultOrg(), nil },
		},
		Contexts: &fakeContextAPI{
			listContexts: func(ownerID, ownerSlug string) ([]cctx.Context, error) {
				return nil, errors.New("context API unavailable")
			},
		},
		Projects: &fakeProjectAPI{},
	}

	m, err := ex.Export(exporter.Options{OrgSlug: "gh/myorg", IncludeContexts: true})
	if err != nil {
		t.Fatalf("contexts list error should not fail Export, got: %v", err)
	}
	found := false
	for _, w := range m.Warnings {
		if w.Code == "contexts_unreadable" {
			found = true
		}
	}
	if !found {
		t.Error("expected contexts_unreadable warning, not found")
	}
}

func TestExport_Contexts_Sorted(t *testing.T) {
	ex := &exporter.Exporter{
		Org: &fakeOrgAPI{
			getOrganization: func(string) (*org.Organization, error) { return defaultOrg(), nil },
		},
		Contexts: &fakeContextAPI{
			listContexts: func(ownerID, ownerSlug string) ([]cctx.Context, error) {
				return []cctx.Context{
					{ID: "c3", Name: "zebra"},
					{ID: "c1", Name: "alpha"},
					{ID: "c2", Name: "mango"},
				}, nil
			},
		},
		Projects: &fakeProjectAPI{},
	}

	m, err := ex.Export(exporter.Options{OrgSlug: "gh/myorg", IncludeContexts: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m.Contexts) != 3 {
		t.Fatalf("expected 3 contexts, got %d", len(m.Contexts))
	}
	if m.Contexts[0].Name != "alpha" || m.Contexts[1].Name != "mango" || m.Contexts[2].Name != "zebra" {
		t.Errorf("contexts not sorted: %v", m.Contexts)
	}
}

func TestExport_IncludeContextsFalse_SkipsContexts(t *testing.T) {
	contextCalled := false
	ex := &exporter.Exporter{
		Org: &fakeOrgAPI{
			getOrganization: func(string) (*org.Organization, error) { return defaultOrg(), nil },
		},
		Contexts: &fakeContextAPI{
			listContexts: func(ownerID, ownerSlug string) ([]cctx.Context, error) {
				contextCalled = true
				return []cctx.Context{{ID: "c1", Name: "prod"}}, nil
			},
		},
		Projects: &fakeProjectAPI{},
	}

	m, err := ex.Export(exporter.Options{OrgSlug: "gh/myorg", IncludeContexts: false})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if contextCalled {
		t.Error("ListContexts should NOT be called when IncludeContexts=false")
	}
	if len(m.Contexts) != 0 {
		t.Errorf("expected 0 contexts, got %d", len(m.Contexts))
	}
}

// ---------------------------------------------------------------------------
// Projects
// ---------------------------------------------------------------------------

func TestExport_Projects_DiscoveryFromOrgProjectsList(t *testing.T) {
	// Primary discovery path: ListOrgProjects returns both GitHub OAuth and App
	// org slugs; they are all added to the export set.
	ex := &exporter.Exporter{
		Org: &fakeOrgAPI{
			getOrganization: func(string) (*org.Organization, error) { return defaultOrg(), nil },
		},
		Contexts: &fakeContextAPI{},
		Projects: &fakeProjectAPI{
			listOrgProjects: func(orgID string) ([]project.OrgProject, error) {
				if orgID != "org-uuid-123" {
					t.Errorf("ListOrgProjects orgID: got %q want org-uuid-123", orgID)
				}
				return []project.OrgProject{
					{ID: "pid-1", Slug: "gh/myorg/web", Name: "web"},
					{ID: "pid-2", Slug: "gh/myorg/api", Name: "api"},
				}, nil
			},
			getProject: func(slug string) (*project.Project, error) {
				return &project.Project{Slug: slug, ID: "pid", Name: slug}, nil
			},
		},
	}

	m, err := ex.Export(exporter.Options{OrgSlug: "gh/myorg", IncludeProjects: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m.Projects) != 2 {
		t.Fatalf("expected 2 projects, got %d", len(m.Projects))
	}
	slugs := map[string]bool{}
	for _, p := range m.Projects {
		slugs[p.Slug] = true
	}
	if !slugs["gh/myorg/web"] || !slugs["gh/myorg/api"] {
		t.Errorf("unexpected project slugs: %v", slugs)
	}
}

func TestExport_Projects_DiscoveryFallbackToFollowed(t *testing.T) {
	// When ListOrgProjects fails, discovery falls back to FollowedProjectsForOrg
	// and a fallback warning is emitted.
	ex := &exporter.Exporter{
		Org: &fakeOrgAPI{
			getOrganization: func(string) (*org.Organization, error) { return defaultOrg(), nil },
		},
		Contexts: &fakeContextAPI{},
		Projects: &fakeProjectAPI{
			listOrgProjects: func(orgID string) ([]project.OrgProject, error) {
				return nil, errors.New("private API unavailable")
			},
			followedProjectsForOrg: func(orgName string) ([]project.FollowedProject, error) {
				return []project.FollowedProject{
					{Reponame: "web", VCSType: "github", Username: "myorg"},
					{Reponame: "api", VCSType: "github", Username: "myorg"},
				}, nil
			},
			getProject: func(slug string) (*project.Project, error) {
				return &project.Project{Slug: slug, ID: "pid", Name: slug}, nil
			},
		},
	}

	m, err := ex.Export(exporter.Options{OrgSlug: "gh/myorg", IncludeProjects: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m.Projects) != 2 {
		t.Fatalf("expected 2 projects, got %d", len(m.Projects))
	}
	found := false
	for _, w := range m.Warnings {
		if w.Code == "discovery_fallback" {
			found = true
		}
	}
	if !found {
		t.Error("expected discovery_fallback warning when ListOrgProjects fails")
	}
}

func TestExport_Projects_DiscoveryFollowedOnly_Warning_NotOnPrivateAPISuccess(t *testing.T) {
	// project_discovery_followed_only must NOT fire when ListOrgProjects succeeds —
	// the private API returns a complete project set so the warning is misleading.
	ex := &exporter.Exporter{
		Org: &fakeOrgAPI{
			getOrganization: func(string) (*org.Organization, error) { return defaultOrg(), nil },
		},
		Contexts: &fakeContextAPI{},
		Projects: &fakeProjectAPI{
			listOrgProjects: func(orgID string) ([]project.OrgProject, error) {
				return []project.OrgProject{{ID: "pid", Slug: "gh/myorg/web", Name: "web"}}, nil
			},
		},
	}

	m, err := ex.Export(exporter.Options{OrgSlug: "gh/myorg", IncludeProjects: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, w := range m.Warnings {
		if w.Code == "project_discovery_followed_only" {
			t.Error("project_discovery_followed_only must NOT be emitted when ListOrgProjects succeeded")
		}
	}
}

func TestExport_Projects_DiscoveryFollowedOnly_Warning_OnFallback(t *testing.T) {
	// project_discovery_followed_only IS emitted when no explicit slugs are given
	// AND discovery fell back to the v1.1 followed-projects list.
	ex := &exporter.Exporter{
		Org: &fakeOrgAPI{
			getOrganization: func(string) (*org.Organization, error) { return defaultOrg(), nil },
		},
		Contexts: &fakeContextAPI{},
		Projects: &fakeProjectAPI{
			listOrgProjects: func(orgID string) ([]project.OrgProject, error) {
				return nil, errors.New("private API unavailable")
			},
			followedProjectsForOrg: func(orgName string) ([]project.FollowedProject, error) {
				return []project.FollowedProject{
					{Reponame: "web", VCSType: "github", Username: "myorg"},
				}, nil
			},
		},
	}

	m, err := ex.Export(exporter.Options{OrgSlug: "gh/myorg", IncludeProjects: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	found := false
	for _, w := range m.Warnings {
		if w.Code == "project_discovery_followed_only" {
			found = true
		}
	}
	if !found {
		t.Error("expected project_discovery_followed_only warning when discovery fell back to followed list")
	}
}

func TestExport_Projects_DiscoveryFollowedOnly_Warning_NotWhenExplicitSlugs(t *testing.T) {
	// project_discovery_followed_only must NOT fire when the caller provided
	// explicit slugs, even if discovery fell back.
	ex := &exporter.Exporter{
		Org: &fakeOrgAPI{
			getOrganization: func(string) (*org.Organization, error) { return defaultOrg(), nil },
		},
		Contexts: &fakeContextAPI{},
		Projects: &fakeProjectAPI{
			listOrgProjects: func(orgID string) ([]project.OrgProject, error) {
				return nil, errors.New("private API unavailable")
			},
			followedProjectsForOrg: func(orgName string) ([]project.FollowedProject, error) {
				return nil, nil
			},
		},
	}

	m, err := ex.Export(exporter.Options{
		OrgSlug:         "gh/myorg",
		IncludeProjects: true,
		ProjectSlugs:    []string{"gh/myorg/web"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, w := range m.Warnings {
		if w.Code == "project_discovery_followed_only" {
			t.Error("project_discovery_followed_only must NOT fire when explicit slugs were given")
		}
	}
}

func TestExport_Projects_ExplicitSlugs_NoDiscoveryOnlyWarning(t *testing.T) {
	ex := &exporter.Exporter{
		Org: &fakeOrgAPI{
			getOrganization: func(string) (*org.Organization, error) { return defaultOrg(), nil },
		},
		Contexts: &fakeContextAPI{},
		Projects: &fakeProjectAPI{},
	}

	m, err := ex.Export(exporter.Options{
		OrgSlug:         "gh/myorg",
		IncludeProjects: true,
		ProjectSlugs:    []string{"gh/myorg/web"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, w := range m.Warnings {
		if w.Code == "project_discovery_followed_only" {
			t.Error("should NOT have project_discovery_followed_only warning when explicit slugs given")
		}
	}
}

func TestExport_Projects_ExplicitSlugs_SkipOrgWideDiscovery(t *testing.T) {
	// When explicit --projects are given, ONLY those are exported — org-wide
	// discovery (ListOrgProjects) must NOT run, so projects returned by it that
	// are not in the explicit set must not appear in the manifest.
	listOrgCalled := false
	ex := &exporter.Exporter{
		Org: &fakeOrgAPI{
			getOrganization: func(string) (*org.Organization, error) { return defaultOrg(), nil },
		},
		Contexts: &fakeContextAPI{},
		Projects: &fakeProjectAPI{
			listOrgProjects: func(orgID string) ([]project.OrgProject, error) {
				listOrgCalled = true
				return []project.OrgProject{
					{ID: "pid-1", Slug: "gh/myorg/web", Name: "web"},
					{ID: "pid-2", Slug: "gh/myorg/should-not-appear", Name: "should-not-appear"},
				}, nil
			},
		},
	}

	m, err := ex.Export(exporter.Options{
		OrgSlug:         "gh/myorg",
		IncludeProjects: true,
		ProjectSlugs:    []string{"gh/myorg/web", "gh/myorg/api"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if listOrgCalled {
		t.Error("ListOrgProjects should NOT be called when explicit --projects are given")
	}
	got := map[string]bool{}
	for _, p := range m.Projects {
		got[p.Slug] = true
	}
	if len(m.Projects) != 2 || !got["gh/myorg/web"] || !got["gh/myorg/api"] {
		t.Errorf("expected exactly the 2 explicit projects {web, api}, got %d: %v", len(m.Projects), got)
	}
	if got["gh/myorg/should-not-appear"] {
		t.Error("a discovered project leaked into the manifest despite explicit --projects")
	}
}

func TestExport_Projects_Sorted(t *testing.T) {
	ex := &exporter.Exporter{
		Org: &fakeOrgAPI{
			getOrganization: func(string) (*org.Organization, error) { return defaultOrg(), nil },
		},
		Contexts: &fakeContextAPI{},
		Projects: &fakeProjectAPI{},
	}

	m, err := ex.Export(exporter.Options{
		OrgSlug:         "gh/myorg",
		IncludeProjects: true,
		ProjectSlugs:    []string{"gh/myorg/zebra", "gh/myorg/alpha", "gh/myorg/mango"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m.Projects) != 3 {
		t.Fatalf("expected 3 projects, got %d", len(m.Projects))
	}
	if m.Projects[0].Slug != "gh/myorg/alpha" ||
		m.Projects[1].Slug != "gh/myorg/mango" ||
		m.Projects[2].Slug != "gh/myorg/zebra" {
		t.Errorf("projects not sorted: %v", m.Projects)
	}
}

func TestExport_Projects_Settings_MappedCorrectly(t *testing.T) {
	trueVal := true
	falseVal := false

	ex := &exporter.Exporter{
		Org: &fakeOrgAPI{
			getOrganization: func(string) (*org.Organization, error) { return defaultOrg(), nil },
		},
		Contexts: &fakeContextAPI{},
		Projects: &fakeProjectAPI{
			getProject: func(slug string) (*project.Project, error) {
				return &project.Project{Slug: slug, ID: "pid", Name: "web"}, nil
			},
			getSettings: func(provider, orgName, proj string) (*project.AdvancedSettings, error) {
				return &project.AdvancedSettings{
					AutocancelBuilds: &trueVal,
					SetGithubStatus:  &falseVal,
					BuildForkPRs:     &trueVal,
				}, nil
			},
		},
	}

	m, err := ex.Export(exporter.Options{
		OrgSlug:         "gh/myorg",
		IncludeProjects: true,
		ProjectSlugs:    []string{"gh/myorg/web"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m.Projects) != 1 {
		t.Fatalf("expected 1 project, got %d", len(m.Projects))
	}
	s := m.Projects[0].Settings
	if s == nil {
		t.Fatal("Settings is nil")
		return
	}
	if s.AutocancelBuilds == nil || !*s.AutocancelBuilds {
		t.Error("AutocancelBuilds should be true")
	}
	// SetGithubStatus in project API maps to SetGitHubStatus in manifest
	if s.SetGitHubStatus == nil || *s.SetGitHubStatus {
		t.Error("SetGitHubStatus should be false (mapped from SetGithubStatus)")
	}
	if s.BuildForkPRs == nil || !*s.BuildForkPRs {
		t.Error("BuildForkPRs should be true")
	}
}

func TestExport_Projects_EnvVars(t *testing.T) {
	ex := &exporter.Exporter{
		Org: &fakeOrgAPI{
			getOrganization: func(string) (*org.Organization, error) { return defaultOrg(), nil },
		},
		Contexts: &fakeContextAPI{},
		Projects: &fakeProjectAPI{
			getProject: func(slug string) (*project.Project, error) {
				return &project.Project{Slug: slug, ID: "pid", Name: "web"}, nil
			},
			listEnvVars: func(slug string) ([]project.EnvVar, error) {
				return []project.EnvVar{
					{Name: "AWS_KEY", MaskedValue: "xxxx1234"},
					{Name: "DB_PASS", MaskedValue: "xxxx5678"},
				}, nil
			},
		},
	}

	m, err := ex.Export(exporter.Options{
		OrgSlug:         "gh/myorg",
		IncludeProjects: true,
		ProjectSlugs:    []string{"gh/myorg/web"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m.Projects) != 1 {
		t.Fatalf("expected 1 project, got %d", len(m.Projects))
	}
	evs := m.Projects[0].EnvVars
	if len(evs) != 2 {
		t.Fatalf("expected 2 env vars, got %d", len(evs))
	}
	found := map[string]string{}
	for _, v := range evs {
		found[v.Name] = v.MaskedValue
	}
	if found["AWS_KEY"] != "xxxx1234" {
		t.Errorf("AWS_KEY masked value: got %q want %q", found["AWS_KEY"], "xxxx1234")
	}
}

func TestExport_Projects_EnvVarError_IsWarning(t *testing.T) {
	ex := &exporter.Exporter{
		Org: &fakeOrgAPI{
			getOrganization: func(string) (*org.Organization, error) { return defaultOrg(), nil },
		},
		Contexts: &fakeContextAPI{},
		Projects: &fakeProjectAPI{
			getProject: func(slug string) (*project.Project, error) {
				return &project.Project{Slug: slug, ID: "pid", Name: "web"}, nil
			},
			listEnvVars: func(slug string) ([]project.EnvVar, error) {
				return nil, errors.New("env var API error")
			},
		},
	}

	m, err := ex.Export(exporter.Options{
		OrgSlug:         "gh/myorg",
		IncludeProjects: true,
		ProjectSlugs:    []string{"gh/myorg/web"},
	})
	if err != nil {
		t.Fatalf("per-resource error should not fail Export, got: %v", err)
	}
	found := false
	for _, w := range m.Warnings {
		if w.Code == "env_vars_unreadable" {
			found = true
		}
	}
	if !found {
		t.Error("expected env_vars_unreadable warning, not found")
	}
}

func TestExport_Projects_GetProjectError_IsWarning(t *testing.T) {
	ex := &exporter.Exporter{
		Org: &fakeOrgAPI{
			getOrganization: func(string) (*org.Organization, error) { return defaultOrg(), nil },
		},
		Contexts: &fakeContextAPI{},
		Projects: &fakeProjectAPI{
			getProject: func(slug string) (*project.Project, error) {
				return nil, errors.New("project not found")
			},
		},
	}

	m, err := ex.Export(exporter.Options{
		OrgSlug:         "gh/myorg",
		IncludeProjects: true,
		ProjectSlugs:    []string{"gh/myorg/web"},
	})
	if err != nil {
		t.Fatalf("per-resource error should not fail Export, got: %v", err)
	}
	// Project should still appear in manifest (partial)
	if len(m.Projects) != 1 {
		t.Fatalf("expected 1 project entry (partial), got %d", len(m.Projects))
	}
	found := false
	for _, w := range m.Warnings {
		if w.Code == "project_unreadable" {
			found = true
		}
	}
	if !found {
		t.Error("expected project_unreadable warning, not found")
	}
}

func TestExport_Projects_IncludeExtras_CheckoutKeys(t *testing.T) {
	ex := &exporter.Exporter{
		Org: &fakeOrgAPI{
			getOrganization: func(string) (*org.Organization, error) { return defaultOrg(), nil },
		},
		Contexts: &fakeContextAPI{},
		Projects: &fakeProjectAPI{
			getProject: func(slug string) (*project.Project, error) {
				return &project.Project{Slug: slug, ID: "pid", Name: "web"}, nil
			},
			listCheckoutKeys: func(slug string) ([]project.CheckoutKey, error) {
				return []project.CheckoutKey{
					{Type: "deploy-key", Fingerprint: "aa:bb:cc", Preferred: true},
				}, nil
			},
		},
	}

	m, err := ex.Export(exporter.Options{
		OrgSlug:         "gh/myorg",
		IncludeProjects: true,
		IncludeExtras:   true,
		ProjectSlugs:    []string{"gh/myorg/web"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m.Projects) != 1 {
		t.Fatalf("expected 1 project, got %d", len(m.Projects))
	}
	if len(m.Projects[0].CheckoutKeys) != 1 {
		t.Fatalf("expected 1 checkout key, got %d", len(m.Projects[0].CheckoutKeys))
	}
	if m.Projects[0].CheckoutKeys[0].Type != "deploy-key" {
		t.Errorf("checkout key type: got %q want %q", m.Projects[0].CheckoutKeys[0].Type, "deploy-key")
	}
}

func TestExport_Projects_IncludeExtras_Webhooks(t *testing.T) {
	trueVal := true
	ex := &exporter.Exporter{
		Org: &fakeOrgAPI{
			getOrganization: func(string) (*org.Organization, error) { return defaultOrg(), nil },
		},
		Contexts: &fakeContextAPI{},
		Projects: &fakeProjectAPI{
			getProject: func(slug string) (*project.Project, error) {
				return &project.Project{Slug: slug, ID: "proj-id-abc", Name: "web"}, nil
			},
			listWebhooks: func(projectID string) ([]project.Webhook, error) {
				return []project.Webhook{
					{ID: "wh-1", Name: "notify", URL: "https://hooks.example.com", Events: []string{"workflow-completed"}, VerifyTLS: &trueVal},
				}, nil
			},
		},
	}

	m, err := ex.Export(exporter.Options{
		OrgSlug:         "gh/myorg",
		IncludeProjects: true,
		IncludeExtras:   true,
		ProjectSlugs:    []string{"gh/myorg/web"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m.Projects) != 1 || len(m.Projects[0].Webhooks) != 1 {
		t.Fatalf("expected 1 project with 1 webhook, got projects=%d", len(m.Projects))
	}
	wh := m.Projects[0].Webhooks[0]
	if wh.Name != "notify" || wh.URL != "https://hooks.example.com" {
		t.Errorf("webhook: %+v", wh)
	}
}

func TestExport_Projects_IncludeExtras_Schedules(t *testing.T) {
	ex := &exporter.Exporter{
		Org: &fakeOrgAPI{
			getOrganization: func(string) (*org.Organization, error) { return defaultOrg(), nil },
		},
		Contexts: &fakeContextAPI{},
		Projects: &fakeProjectAPI{
			getProject: func(slug string) (*project.Project, error) {
				return &project.Project{Slug: slug, ID: "pid", Name: "web"}, nil
			},
			listSchedules: func(slug string) ([]project.Schedule, error) {
				return []project.Schedule{
					{Name: "nightly", Description: "nightly build", Timetable: map[string]any{"hours": []int{2}}},
				}, nil
			},
		},
	}

	m, err := ex.Export(exporter.Options{
		OrgSlug:         "gh/myorg",
		IncludeProjects: true,
		IncludeExtras:   true,
		ProjectSlugs:    []string{"gh/myorg/web"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m.Projects) != 1 || len(m.Projects[0].Schedules) != 1 {
		t.Fatalf("unexpected: %+v", m.Projects)
	}
	if m.Projects[0].Schedules[0].Name != "nightly" {
		t.Errorf("schedule name: got %q", m.Projects[0].Schedules[0].Name)
	}
}

func TestExport_Projects_IncludeExtrasFalse_NoExtras(t *testing.T) {
	keysCalled := false
	ex := &exporter.Exporter{
		Org: &fakeOrgAPI{
			getOrganization: func(string) (*org.Organization, error) { return defaultOrg(), nil },
		},
		Contexts: &fakeContextAPI{},
		Projects: &fakeProjectAPI{
			getProject: func(slug string) (*project.Project, error) {
				return &project.Project{Slug: slug, ID: "pid", Name: "web"}, nil
			},
			listCheckoutKeys: func(slug string) ([]project.CheckoutKey, error) {
				keysCalled = true
				return nil, nil
			},
		},
	}

	_, err := ex.Export(exporter.Options{
		OrgSlug:         "gh/myorg",
		IncludeProjects: true,
		IncludeExtras:   false,
		ProjectSlugs:    []string{"gh/myorg/web"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if keysCalled {
		t.Error("ListCheckoutKeys should NOT be called when IncludeExtras=false")
	}
}

func TestExport_IncludeProjectsFalse_SkipsProjects(t *testing.T) {
	followCalled := false
	ex := &exporter.Exporter{
		Org: &fakeOrgAPI{
			getOrganization: func(string) (*org.Organization, error) { return defaultOrg(), nil },
		},
		Contexts: &fakeContextAPI{},
		Projects: &fakeProjectAPI{
			followedProjectsForOrg: func(orgName string) ([]project.FollowedProject, error) {
				followCalled = true
				return nil, nil
			},
		},
	}

	m, err := ex.Export(exporter.Options{OrgSlug: "gh/myorg", IncludeProjects: false})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if followCalled {
		t.Error("FollowedProjectsForOrg should NOT be called when IncludeProjects=false")
	}
	if len(m.Projects) != 0 {
		t.Errorf("expected 0 projects, got %d", len(m.Projects))
	}
}

func TestExport_OutputWriter_ReceivesProgress(t *testing.T) {
	var buf strings.Builder
	ex := &exporter.Exporter{
		Org: &fakeOrgAPI{
			getOrganization: func(string) (*org.Organization, error) { return defaultOrg(), nil },
		},
		Contexts: &fakeContextAPI{},
		Projects: &fakeProjectAPI{},
		Out:      &buf,
	}

	_, err := ex.Export(exporter.Options{OrgSlug: "gh/myorg"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if buf.Len() == 0 {
		t.Error("expected progress output to Out, got empty")
	}
}

func TestExport_SchemaVersion(t *testing.T) {
	m, err := minimalExporter().Export(exporter.Options{OrgSlug: "gh/myorg"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.SchemaVersion != manifest.SchemaVersion {
		t.Errorf("SchemaVersion: got %q want %q", m.SchemaVersion, manifest.SchemaVersion)
	}
}

func TestExport_Projects_VCSFieldsMapped(t *testing.T) {
	ex := &exporter.Exporter{
		Org: &fakeOrgAPI{
			getOrganization: func(string) (*org.Organization, error) { return defaultOrg(), nil },
		},
		Contexts: &fakeContextAPI{},
		Projects: &fakeProjectAPI{
			getProject: func(slug string) (*project.Project, error) {
				return &project.Project{
					Slug: slug,
					ID:   "pid",
					Name: "web",
					VCS: project.ProjectVCS{
						Provider:      "GitHub",
						URL:           "https://github.com/myorg/web",
						DefaultBranch: "main",
					},
				}, nil
			},
		},
	}

	m, err := ex.Export(exporter.Options{
		OrgSlug:         "gh/myorg",
		IncludeProjects: true,
		ProjectSlugs:    []string{"gh/myorg/web"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m.Projects) != 1 {
		t.Fatalf("expected 1 project, got %d", len(m.Projects))
	}
	vcs := m.Projects[0].VCS
	if vcs.Provider != "GitHub" {
		t.Errorf("VCS.Provider: got %q want %q", vcs.Provider, "GitHub")
	}
	if vcs.DefaultBranch != "main" {
		t.Errorf("VCS.DefaultBranch: got %q want %q", vcs.DefaultBranch, "main")
	}
	if vcs.URL != "https://github.com/myorg/web" {
		t.Errorf("VCS.URL: got %q want %q", vcs.URL, "https://github.com/myorg/web")
	}
}

func TestExport_OrgSettings_AuditLogConfigsCaptured(t *testing.T) {
	ex := &exporter.Exporter{
		Org: &fakeOrgAPI{
			getOrganization: func(string) (*org.Organization, error) { return defaultOrg(), nil },
			getAuditLogConfigs: func(orgID string) ([]org.AuditLogConfig, error) {
				if orgID != "org-uuid-123" {
					t.Errorf("GetAuditLogConfigs orgID: got %q want %q", orgID, "org-uuid-123")
				}
				return []org.AuditLogConfig{
					{
						ID:         "cfg-1",
						Purpose:    "security",
						TargetType: "s3",
						IsDisabled: false,
						Config: org.AuditLogTarget{
							ARN:          "arn:aws:iam::123:role/audit",
							Region:       "us-east-1",
							BucketName:   "acme-audit",
							BucketPrefix: "logs/",
							Endpoint:     "https://s3.amazonaws.com",
						},
					},
				}, nil
			},
		},
		Contexts: &fakeContextAPI{},
		Projects: &fakeProjectAPI{},
	}

	m, err := ex.Export(exporter.Options{OrgSlug: "gh/myorg"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Source.Org.Settings == nil {
		t.Fatal("Settings is nil")
	}
	configs := m.Source.Org.Settings.AuditLogConfigs
	if len(configs) != 1 {
		t.Fatalf("expected 1 audit-log config, got %d", len(configs))
	}
	got := configs[0]
	if got.ID != "cfg-1" || got.Purpose != "security" || got.TargetType != "s3" {
		t.Errorf("unexpected config metadata: %+v", got)
	}
	if got.Config.ARN != "arn:aws:iam::123:role/audit" || got.Config.BucketName != "acme-audit" {
		t.Errorf("unexpected config target: %+v", got.Config)
	}
	if got.Config.Region != "us-east-1" || got.Config.BucketPrefix != "logs/" || got.Config.Endpoint != "https://s3.amazonaws.com" {
		t.Errorf("unexpected config target: %+v", got.Config)
	}
}

func TestExport_OrgSettings_AuditLogConfigsError_IsWarning(t *testing.T) {
	ex := &exporter.Exporter{
		Org: &fakeOrgAPI{
			getOrganization: func(string) (*org.Organization, error) { return defaultOrg(), nil },
			getAuditLogConfigs: func(orgID string) ([]org.AuditLogConfig, error) {
				return nil, errors.New("audit-log API down")
			},
		},
		Contexts: &fakeContextAPI{},
		Projects: &fakeProjectAPI{},
	}

	m, err := ex.Export(exporter.Options{OrgSlug: "gh/myorg"})
	if err != nil {
		t.Fatalf("audit-log error should not fail Export, got: %v", err)
	}
	found := false
	for _, w := range m.Warnings {
		if w.Code == "audit_log_configs_unreadable" {
			found = true
		}
	}
	if !found {
		t.Error("expected audit_log_configs_unreadable warning, not found")
	}
}

func TestExport_OrgSettings_FeatureFlagsError_IsWarning(t *testing.T) {
	ex := &exporter.Exporter{
		Org: &fakeOrgAPI{
			getOrganization: func(string) (*org.Organization, error) { return defaultOrg(), nil },
			getFeatureFlags: func(vcsType, orgName string) (map[string]bool, error) {
				return nil, errors.New("feature flags unavailable")
			},
		},
		Contexts: &fakeContextAPI{},
		Projects: &fakeProjectAPI{},
	}

	m, err := ex.Export(exporter.Options{OrgSlug: "gh/myorg"})
	if err != nil {
		t.Fatalf("feature flags error should not fail Export, got: %v", err)
	}
	found := false
	for _, w := range m.Warnings {
		if w.Code == "feature_flags_unreadable" {
			found = true
		}
	}
	if !found {
		t.Error("expected feature_flags_unreadable warning, not found")
	}
}

func TestExport_OrgSettings_OIDCError_IsWarning(t *testing.T) {
	ex := &exporter.Exporter{
		Org: &fakeOrgAPI{
			getOrganization: func(string) (*org.Organization, error) { return defaultOrg(), nil },
			getOIDCClaims: func(orgID string) ([]string, string, error) {
				return nil, "", errors.New("oidc API down")
			},
		},
		Contexts: &fakeContextAPI{},
		Projects: &fakeProjectAPI{},
	}

	m, err := ex.Export(exporter.Options{OrgSlug: "gh/myorg"})
	if err != nil {
		t.Fatalf("OIDC error should not fail Export, got: %v", err)
	}
	found := false
	for _, w := range m.Warnings {
		if w.Code == "oidc_claims_unreadable" {
			found = true
		}
	}
	if !found {
		t.Error("expected oidc_claims_unreadable warning, not found")
	}
}

func TestExport_OrgSettings_SSOCaptured(t *testing.T) {
	ex := &exporter.Exporter{
		Org: &fakeOrgAPI{
			getOrganization: func(string) (*org.Organization, error) { return defaultOrg(), nil },
			getSSOEnforced: func(orgID string) (bool, error) {
				if orgID != "org-uuid-123" {
					t.Errorf("GetSSOEnforced orgID: got %q want %q", orgID, "org-uuid-123")
				}
				return true, nil
			},
			getSSOConnection: func(orgID string) (map[string]any, bool, error) {
				return map[string]any{
					"realm":         "acme-saml",
					"idp_entity_id": "https://idp.example.com/entity",
				}, true, nil
			},
		},
		Contexts: &fakeContextAPI{},
		Projects: &fakeProjectAPI{},
	}

	m, err := ex.Export(exporter.Options{OrgSlug: "gh/myorg"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Source.Org.Settings == nil {
		t.Fatal("Settings is nil")
		return
	}
	sso := m.Source.Org.Settings.SSO
	if sso == nil {
		t.Fatal("SSO is nil")
		return
	}
	if !sso.Enforced {
		t.Error("SSO.Enforced should be true")
	}
	if sso.Realm != "acme-saml" {
		t.Errorf("SSO.Realm: got %q want %q", sso.Realm, "acme-saml")
	}
	if sso.Connection["idp_entity_id"] != "https://idp.example.com/entity" {
		t.Errorf("SSO.Connection not captured: %v", sso.Connection)
	}
}

func TestExport_OrgSettings_SSOEnforcedOnlyCaptured(t *testing.T) {
	// Enforced=true but no connection (404) → SSO still recorded, no realm.
	ex := &exporter.Exporter{
		Org: &fakeOrgAPI{
			getOrganization:  func(string) (*org.Organization, error) { return defaultOrg(), nil },
			getSSOEnforced:   func(orgID string) (bool, error) { return true, nil },
			getSSOConnection: func(orgID string) (map[string]any, bool, error) { return nil, false, nil },
		},
		Contexts: &fakeContextAPI{},
		Projects: &fakeProjectAPI{},
	}

	m, err := ex.Export(exporter.Options{OrgSlug: "gh/myorg"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Source.Org.Settings == nil || m.Source.Org.Settings.SSO == nil {
		t.Fatal("expected SSO to be captured when enforced=true")
		return
	}
	if !m.Source.Org.Settings.SSO.Enforced {
		t.Error("SSO.Enforced should be true")
	}
	if m.Source.Org.Settings.SSO.Connection != nil {
		t.Errorf("SSO.Connection should be nil when no connection found, got %v", m.Source.Org.Settings.SSO.Connection)
	}
}

func TestExport_OrgSettings_NoSSO_NilWhenOffAndNoConnection(t *testing.T) {
	// Enforced=false + 404 connection → OrgSettings.SSO must be nil.
	ex := &exporter.Exporter{
		Org: &fakeOrgAPI{
			getOrganization:  func(string) (*org.Organization, error) { return defaultOrg(), nil },
			getSSOEnforced:   func(orgID string) (bool, error) { return false, nil },
			getSSOConnection: func(orgID string) (map[string]any, bool, error) { return nil, false, nil },
		},
		Contexts: &fakeContextAPI{},
		Projects: &fakeProjectAPI{},
	}

	m, err := ex.Export(exporter.Options{OrgSlug: "gh/myorg"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Source.Org.Settings != nil && m.Source.Org.Settings.SSO != nil {
		t.Errorf("SSO should be nil when not enforced and no connection, got %+v", m.Source.Org.Settings.SSO)
	}
}

func TestExport_OrgSettings_SSOError_IsWarning(t *testing.T) {
	ex := &exporter.Exporter{
		Org: &fakeOrgAPI{
			getOrganization: func(string) (*org.Organization, error) { return defaultOrg(), nil },
			getSSOEnforced: func(orgID string) (bool, error) {
				return false, errors.New("sso enforced API down")
			},
			getSSOConnection: func(orgID string) (map[string]any, bool, error) {
				return nil, false, errors.New("sso connection API down")
			},
		},
		Contexts: &fakeContextAPI{},
		Projects: &fakeProjectAPI{},
	}

	m, err := ex.Export(exporter.Options{OrgSlug: "gh/myorg"})
	if err != nil {
		t.Fatalf("SSO error should not fail Export, got: %v", err)
	}
	found := false
	for _, w := range m.Warnings {
		if w.Code == "sso_unreadable" {
			found = true
		}
	}
	if !found {
		t.Error("expected sso_unreadable warning, not found")
	}
}

func TestExport_OrgSettings_FullCapture(t *testing.T) {
	// Verifies that all org settings sub-reads are called and populated.
	trueVal := true
	ex := &exporter.Exporter{
		Org: &fakeOrgAPI{
			getOrganization: func(string) (*org.Organization, error) { return defaultOrg(), nil },
			getFeatureFlags: func(vcsType, orgName string) (map[string]bool, error) {
				return map[string]bool{
					"allow_certified_public_orbs": true,
					"drop_all_build_requests":     false,
				}, nil
			},
			getOIDCClaims: func(orgID string) ([]string, string, error) {
				return []string{"https://example.com"}, "1h", nil
			},
			getURLOrbAllowList: func(slugOrID string) ([]org.URLOrbAllowEntry, error) {
				return []org.URLOrbAllowEntry{
					{ID: "e1", Name: "github-raw", Prefix: "https://raw.githubusercontent.com/", Auth: "none"},
				}, nil
			},
			getPolicyBundle: func(ownerID string) (map[string]string, error) {
				return map[string]string{"my_policy": "package org\ndefault allow = false"}, nil
			},
			getPolicyEnf: func(ownerID string) (bool, error) {
				return true, nil
			},
		},
		Contexts: &fakeContextAPI{},
		Projects: &fakeProjectAPI{},
	}

	m, err := ex.Export(exporter.Options{OrgSlug: "gh/myorg"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s := m.Source.Org.Settings
	if s == nil {
		t.Fatal("Settings is nil")
		return
	}
	if s.FeatureFlags["allow_certified_public_orbs"] != true {
		t.Error("FeatureFlags not populated")
	}
	if len(s.OIDCAudience) != 1 || s.OIDCAudience[0] != "https://example.com" {
		t.Errorf("OIDCAudience: got %v", s.OIDCAudience)
	}
	if s.OIDCTTL != "1h" {
		t.Errorf("OIDCTTL: got %q want %q", s.OIDCTTL, "1h")
	}
	if len(s.URLOrbAllowList) != 1 || s.URLOrbAllowList[0].Name != "github-raw" {
		t.Errorf("URLOrbAllowList: got %v", s.URLOrbAllowList)
	}
	if s.ConfigPolicies["my_policy"] == "" {
		t.Error("ConfigPolicies not populated")
	}
	if s.PolicyEnforcementEnabled == nil || !*s.PolicyEnforcementEnabled {
		t.Error("PolicyEnforcementEnabled should be true")
	}
	_ = trueVal
}

// ---------------------------------------------------------------------------
// Project-level OIDC capture
// ---------------------------------------------------------------------------

func TestExport_Projects_OIDCCaptured(t *testing.T) {
	ex := &exporter.Exporter{
		Org: &fakeOrgAPI{
			getOrganization: func(string) (*org.Organization, error) { return defaultOrg(), nil },
		},
		Contexts: &fakeContextAPI{},
		Projects: &fakeProjectAPI{
			getProject: func(slug string) (*project.Project, error) {
				return &project.Project{Slug: slug, ID: "proj-uuid-999", Name: "web"}, nil
			},
			getProjectOIDCClaims: func(orgID, projID string) ([]string, string, error) {
				if orgID != "org-uuid-123" {
					t.Errorf("GetProjectOIDCClaims orgID: got %q want org-uuid-123", orgID)
				}
				if projID != "proj-uuid-999" {
					t.Errorf("GetProjectOIDCClaims projID: got %q want proj-uuid-999", projID)
				}
				return []string{"https://oidc.example.com"}, "8h", nil
			},
		},
	}

	m, err := ex.Export(exporter.Options{
		OrgSlug:         "gh/myorg",
		IncludeProjects: true,
		ProjectSlugs:    []string{"gh/myorg/web"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m.Projects) != 1 {
		t.Fatalf("expected 1 project, got %d", len(m.Projects))
	}
	p := m.Projects[0]
	if len(p.OIDCAudience) != 1 || p.OIDCAudience[0] != "https://oidc.example.com" {
		t.Errorf("OIDCAudience: got %v", p.OIDCAudience)
	}
	if p.OIDCTTL != "8h" {
		t.Errorf("OIDCTTL: got %q want 8h", p.OIDCTTL)
	}
}

func TestExport_Projects_OIDCError_IsWarning(t *testing.T) {
	ex := &exporter.Exporter{
		Org: &fakeOrgAPI{
			getOrganization: func(string) (*org.Organization, error) { return defaultOrg(), nil },
		},
		Contexts: &fakeContextAPI{},
		Projects: &fakeProjectAPI{
			getProject: func(slug string) (*project.Project, error) {
				return &project.Project{Slug: slug, ID: "proj-uuid-err", Name: "web"}, nil
			},
			getProjectOIDCClaims: func(orgID, projID string) ([]string, string, error) {
				return nil, "", errors.New("oidc API down")
			},
		},
	}

	m, err := ex.Export(exporter.Options{
		OrgSlug:         "gh/myorg",
		IncludeProjects: true,
		ProjectSlugs:    []string{"gh/myorg/web"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	found := false
	for _, w := range m.Warnings {
		if w.Code == "oidc_claims_unreadable" {
			found = true
		}
	}
	if !found {
		t.Error("expected oidc_claims_unreadable warning for project, not found")
	}
}

func TestExport_Projects_OIDCEmpty_NotSet(t *testing.T) {
	// When OIDC returns empty audience and empty TTL, fields must remain unset.
	ex := &exporter.Exporter{
		Org: &fakeOrgAPI{
			getOrganization: func(string) (*org.Organization, error) { return defaultOrg(), nil },
		},
		Contexts: &fakeContextAPI{},
		Projects: &fakeProjectAPI{
			getProject: func(slug string) (*project.Project, error) {
				return &project.Project{Slug: slug, ID: "proj-uuid-empty", Name: "web"}, nil
			},
			getProjectOIDCClaims: func(orgID, projID string) ([]string, string, error) {
				return nil, "", nil // empty
			},
		},
	}

	m, err := ex.Export(exporter.Options{
		OrgSlug:         "gh/myorg",
		IncludeProjects: true,
		ProjectSlugs:    []string{"gh/myorg/web"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m.Projects) != 1 {
		t.Fatalf("expected 1 project, got %d", len(m.Projects))
	}
	p := m.Projects[0]
	if len(p.OIDCAudience) != 0 {
		t.Errorf("OIDCAudience should be empty when OIDC returns nothing, got %v", p.OIDCAudience)
	}
	if p.OIDCTTL != "" {
		t.Errorf("OIDCTTL should be empty when OIDC returns nothing, got %q", p.OIDCTTL)
	}
}

// ---------------------------------------------------------------------------
// Project-level v1.1 feature flag capture
// ---------------------------------------------------------------------------

func TestExport_Projects_V11FlagsCaptured(t *testing.T) {
	ex := &exporter.Exporter{
		Org: &fakeOrgAPI{
			getOrganization: func(string) (*org.Organization, error) { return defaultOrg(), nil },
		},
		Contexts: &fakeContextAPI{},
		Projects: &fakeProjectAPI{
			getProject: func(slug string) (*project.Project, error) {
				return &project.Project{Slug: slug, ID: "pid", Name: "web"}, nil
			},
			getV11ProjectFeatureFlags: func(slug string) (map[string]bool, error) {
				return map[string]bool{
					"api-trigger-with-config": true,
					"drop-all-build-requests": false,
				}, nil
			},
		},
	}

	m, err := ex.Export(exporter.Options{
		OrgSlug:         "gh/myorg",
		IncludeProjects: true,
		ProjectSlugs:    []string{"gh/myorg/web"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m.Projects) != 1 {
		t.Fatalf("expected 1 project, got %d", len(m.Projects))
	}
	s := m.Projects[0].Settings
	if s == nil {
		t.Fatal("Settings should not be nil when v1.1 flags are captured")
		return
	}
	if s.APITriggerWithConfig == nil || !*s.APITriggerWithConfig {
		t.Errorf("APITriggerWithConfig: expected *true, got %v", s.APITriggerWithConfig)
	}
	if s.DropAllBuildRequests == nil || *s.DropAllBuildRequests {
		t.Errorf("DropAllBuildRequests: expected *false, got %v", s.DropAllBuildRequests)
	}
}

func TestExport_Projects_V11FlagsError_IsWarning(t *testing.T) {
	ex := &exporter.Exporter{
		Org: &fakeOrgAPI{
			getOrganization: func(string) (*org.Organization, error) { return defaultOrg(), nil },
		},
		Contexts: &fakeContextAPI{},
		Projects: &fakeProjectAPI{
			getProject: func(slug string) (*project.Project, error) {
				return &project.Project{Slug: slug, ID: "pid", Name: "web"}, nil
			},
			getV11ProjectFeatureFlags: func(slug string) (map[string]bool, error) {
				return nil, errors.New("v1.1 flags API down")
			},
		},
	}

	m, err := ex.Export(exporter.Options{
		OrgSlug:         "gh/myorg",
		IncludeProjects: true,
		ProjectSlugs:    []string{"gh/myorg/web"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	found := false
	for _, w := range m.Warnings {
		if w.Code == "v11_feature_flags_unreadable" {
			found = true
		}
	}
	if !found {
		t.Error("expected v11_feature_flags_unreadable warning, not found")
	}
}

func TestExport_Projects_V11Flags_DoNotClobberV2Settings(t *testing.T) {
	// v1.1 flags must not overwrite existing v2 advanced settings fields.
	trueVal := true

	ex := &exporter.Exporter{
		Org: &fakeOrgAPI{
			getOrganization: func(string) (*org.Organization, error) { return defaultOrg(), nil },
		},
		Contexts: &fakeContextAPI{},
		Projects: &fakeProjectAPI{
			getProject: func(slug string) (*project.Project, error) {
				return &project.Project{Slug: slug, ID: "pid", Name: "web"}, nil
			},
			getSettings: func(provider, orgName, proj string) (*project.AdvancedSettings, error) {
				return &project.AdvancedSettings{AutocancelBuilds: &trueVal}, nil
			},
			getV11ProjectFeatureFlags: func(slug string) (map[string]bool, error) {
				return map[string]bool{"api-trigger-with-config": true}, nil
			},
		},
	}

	m, err := ex.Export(exporter.Options{
		OrgSlug:         "gh/myorg",
		IncludeProjects: true,
		ProjectSlugs:    []string{"gh/myorg/web"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m.Projects) != 1 {
		t.Fatalf("expected 1 project, got %d", len(m.Projects))
	}
	s := m.Projects[0].Settings
	if s == nil {
		t.Fatal("Settings should not be nil")
		return
	}
	// v2 setting preserved.
	if s.AutocancelBuilds == nil || !*s.AutocancelBuilds {
		t.Error("AutocancelBuilds should still be true (v2 setting not clobbered)")
	}
	// v1.1 flag also set.
	if s.APITriggerWithConfig == nil || !*s.APITriggerWithConfig {
		t.Error("APITriggerWithConfig should be true (from v1.1)")
	}
}

// ---------------------------------------------------------------------------
// OTel exporters capture
// ---------------------------------------------------------------------------

func TestExport_OrgSettings_OTelExportersCaptured(t *testing.T) {
	ex := &exporter.Exporter{
		Org: &fakeOrgAPI{
			getOrganization: func(string) (*org.Organization, error) { return defaultOrg(), nil },
			getOTelExporters: func(orgID string) ([]org.OTelExporter, error) {
				if orgID != "org-uuid-123" {
					t.Errorf("GetOTelExporters orgID: got %q want org-uuid-123", orgID)
				}
				return []org.OTelExporter{
					{
						ID:       "exp-1",
						Endpoint: "https://otel.example.com:4318",
						Protocol: "http/protobuf",
						Insecure: false,
						Headers:  map[string]string{"Authorization": "xxxx"},
					},
					{
						ID:       "exp-2",
						Endpoint: "grpc.example.com:4317",
						Protocol: "grpc",
						Insecure: true,
					},
				}, nil
			},
		},
		Contexts: &fakeContextAPI{},
		Projects: &fakeProjectAPI{},
	}

	m, err := ex.Export(exporter.Options{OrgSlug: "gh/myorg"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Source.Org.Settings == nil {
		t.Fatal("Settings is nil")
	}
	exporters := m.Source.Org.Settings.OTelExporters
	if len(exporters) != 2 {
		t.Fatalf("expected 2 OTel exporters, got %d", len(exporters))
	}
	if exporters[0].Endpoint != "https://otel.example.com:4318" {
		t.Errorf("exporters[0].Endpoint: got %q", exporters[0].Endpoint)
	}
	if exporters[0].Protocol != "http/protobuf" {
		t.Errorf("exporters[0].Protocol: got %q", exporters[0].Protocol)
	}
	if exporters[0].Insecure {
		t.Error("exporters[0].Insecure should be false")
	}
	// Header values are redacted client-side (even the server-side "xxxx" value
	// is replaced by the redaction placeholder to be conservative).
	if _, ok := exporters[0].Headers["Authorization"]; !ok {
		t.Error("exporters[0].Headers: Authorization key must be preserved")
	}
	if v := exporters[0].Headers["Authorization"]; !strings.Contains(v, "redacted") {
		t.Errorf("exporters[0].Headers[Authorization]: expected redaction placeholder, got %q", v)
	}
	if exporters[1].Insecure != true {
		t.Error("exporters[1].Insecure should be true")
	}
}

func TestExport_OrgSettings_OTelExportersEmpty_NotSet(t *testing.T) {
	// When GetOTelExporters returns an empty slice, OTelExporters must be nil in the manifest.
	ex := &exporter.Exporter{
		Org: &fakeOrgAPI{
			getOrganization: func(string) (*org.Organization, error) { return defaultOrg(), nil },
			getOTelExporters: func(orgID string) ([]org.OTelExporter, error) {
				return []org.OTelExporter{}, nil
			},
		},
		Contexts: &fakeContextAPI{},
		Projects: &fakeProjectAPI{},
	}

	m, err := ex.Export(exporter.Options{OrgSlug: "gh/myorg"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Source.Org.Settings != nil && len(m.Source.Org.Settings.OTelExporters) != 0 {
		t.Errorf("OTelExporters should be empty when API returns [], got %v", m.Source.Org.Settings.OTelExporters)
	}
}

func TestExport_OrgSettings_OTelExportersError_IsWarning(t *testing.T) {
	ex := &exporter.Exporter{
		Org: &fakeOrgAPI{
			getOrganization: func(string) (*org.Organization, error) { return defaultOrg(), nil },
			getOTelExporters: func(orgID string) ([]org.OTelExporter, error) {
				return nil, errors.New("otel API down")
			},
		},
		Contexts: &fakeContextAPI{},
		Projects: &fakeProjectAPI{},
	}

	m, err := ex.Export(exporter.Options{OrgSlug: "gh/myorg"})
	if err != nil {
		t.Fatalf("OTel error should not fail Export, got: %v", err)
	}
	found := false
	for _, w := range m.Warnings {
		if w.Code == "otel_exporters_unreadable" {
			found = true
		}
	}
	if !found {
		t.Error("expected otel_exporters_unreadable warning, not found")
	}
}

// ---------------------------------------------------------------------------
// Contacts capture
// ---------------------------------------------------------------------------

func TestExport_OrgSettings_ContactsCaptured(t *testing.T) {
	ex := &exporter.Exporter{
		Org: &fakeOrgAPI{
			getOrganization: func(string) (*org.Organization, error) { return defaultOrg(), nil },
			getContacts: func(orgID string) ([]string, []string, error) {
				if orgID != "org-uuid-123" {
					t.Errorf("GetContacts orgID: got %q want org-uuid-123", orgID)
				}
				return []string{"alice@example.com"}, []string{"sec@example.com"}, nil
			},
		},
		Contexts: &fakeContextAPI{},
		Projects: &fakeProjectAPI{},
	}

	m, err := ex.Export(exporter.Options{OrgSlug: "gh/myorg"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Source.Org.Settings == nil {
		t.Fatal("Settings is nil")
	}
	c := m.Source.Org.Settings.Contacts
	if c == nil {
		t.Fatal("Contacts is nil")
		return
	}
	if len(c.Primary) != 1 || c.Primary[0] != "alice@example.com" {
		t.Errorf("Contacts.Primary: got %v", c.Primary)
	}
	if len(c.Security) != 1 || c.Security[0] != "sec@example.com" {
		t.Errorf("Contacts.Security: got %v", c.Security)
	}
}

func TestExport_OrgSettings_ContactsEmpty_NilWhenBothEmpty(t *testing.T) {
	// Both primary and security empty → Contacts must be nil.
	ex := &exporter.Exporter{
		Org: &fakeOrgAPI{
			getOrganization: func(string) (*org.Organization, error) { return defaultOrg(), nil },
			getContacts: func(orgID string) ([]string, []string, error) {
				return []string{}, []string{}, nil
			},
		},
		Contexts: &fakeContextAPI{},
		Projects: &fakeProjectAPI{},
	}

	m, err := ex.Export(exporter.Options{OrgSlug: "gh/myorg"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Source.Org.Settings != nil && m.Source.Org.Settings.Contacts != nil {
		t.Errorf("Contacts should be nil when both lists are empty, got %+v", m.Source.Org.Settings.Contacts)
	}
}

func TestExport_OrgSettings_ContactsError_IsWarning(t *testing.T) {
	ex := &exporter.Exporter{
		Org: &fakeOrgAPI{
			getOrganization: func(string) (*org.Organization, error) { return defaultOrg(), nil },
			getContacts: func(orgID string) ([]string, []string, error) {
				return nil, nil, errors.New("contacts API down")
			},
		},
		Contexts: &fakeContextAPI{},
		Projects: &fakeProjectAPI{},
	}

	m, err := ex.Export(exporter.Options{OrgSlug: "gh/myorg"})
	if err != nil {
		t.Fatalf("contacts error should not fail Export, got: %v", err)
	}
	found := false
	for _, w := range m.Warnings {
		if w.Code == "contacts_unreadable" {
			found = true
		}
	}
	if !found {
		t.Error("expected contacts_unreadable warning, not found")
	}
}

// ---------------------------------------------------------------------------
// Group definitions capture
// ---------------------------------------------------------------------------

func TestExport_OrgSettings_GroupsCaptured_ExcludesAllMembers(t *testing.T) {
	// ListGroups returns the default "All members" group (ID == org ID) plus two
	// real groups. Only the real groups must be captured.
	orgID := "org-uuid-123" // matches defaultOrg().ID
	ex := &exporter.Exporter{
		Org: &fakeOrgAPI{
			getOrganization: func(string) (*org.Organization, error) { return defaultOrg(), nil },
			listGroups: func(gotOrgID string) ([]org.Group, error) {
				if gotOrgID != orgID {
					t.Errorf("ListGroups orgID: got %q want %q", gotOrgID, orgID)
				}
				return []org.Group{
					{ID: orgID, Name: "All members"}, // default — must be excluded
					{ID: "grp-1", Name: "security-team"},
					{ID: "grp-2", Name: "platform"},
				}, nil
			},
		},
		Contexts: &fakeContextAPI{},
		Projects: &fakeProjectAPI{},
	}

	m, err := ex.Export(exporter.Options{OrgSlug: "gh/myorg"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Source.Org.Settings == nil {
		t.Fatal("Settings is nil")
		return
	}
	got := m.Source.Org.Settings.Groups
	if len(got) != 2 {
		t.Fatalf("expected 2 groups (All-members excluded), got %d: %+v", len(got), got)
	}
	names := map[string]string{}
	for _, g := range got {
		names[g.Name] = g.ID
		if g.ID == orgID {
			t.Errorf("All-members group (id==orgID) must be excluded, found %+v", g)
		}
	}
	if names["security-team"] != "grp-1" || names["platform"] != "grp-2" {
		t.Errorf("captured groups not as expected: %+v", got)
	}
}

func TestExport_OrgSettings_GroupsNil_WhenOnlyAllMembers(t *testing.T) {
	// When the only group is the default "All members" group, Groups must be nil.
	orgID := "org-uuid-123"
	ex := &exporter.Exporter{
		Org: &fakeOrgAPI{
			getOrganization: func(string) (*org.Organization, error) { return defaultOrg(), nil },
			listGroups: func(string) ([]org.Group, error) {
				return []org.Group{{ID: orgID, Name: "All members"}}, nil
			},
		},
		Contexts: &fakeContextAPI{},
		Projects: &fakeProjectAPI{},
	}

	m, err := ex.Export(exporter.Options{OrgSlug: "gh/myorg"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Source.Org.Settings != nil && len(m.Source.Org.Settings.Groups) != 0 {
		t.Errorf("Groups should be nil/empty when only All-members present, got %+v", m.Source.Org.Settings.Groups)
	}
}

func TestExport_OrgSettings_GroupsNil_WhenEmpty(t *testing.T) {
	// No groups at all → Groups must be nil.
	ex := &exporter.Exporter{
		Org: &fakeOrgAPI{
			getOrganization: func(string) (*org.Organization, error) { return defaultOrg(), nil },
			listGroups: func(string) ([]org.Group, error) {
				return nil, nil
			},
		},
		Contexts: &fakeContextAPI{},
		Projects: &fakeProjectAPI{},
	}

	m, err := ex.Export(exporter.Options{OrgSlug: "gh/myorg"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Source.Org.Settings != nil && len(m.Source.Org.Settings.Groups) != 0 {
		t.Errorf("Groups should be nil/empty when no groups present, got %+v", m.Source.Org.Settings.Groups)
	}
}

func TestExport_OrgSettings_GroupsError_IsWarning(t *testing.T) {
	ex := &exporter.Exporter{
		Org: &fakeOrgAPI{
			getOrganization: func(string) (*org.Organization, error) { return defaultOrg(), nil },
			listGroups: func(string) ([]org.Group, error) {
				return nil, errors.New("groups API down")
			},
		},
		Contexts: &fakeContextAPI{},
		Projects: &fakeProjectAPI{},
	}

	m, err := ex.Export(exporter.Options{OrgSlug: "gh/myorg"})
	if err != nil {
		t.Fatalf("groups error should not fail Export, got: %v", err)
	}
	found := false
	for _, w := range m.Warnings {
		if w.Code == "groups_unreadable" {
			found = true
		}
	}
	if !found {
		t.Error("expected groups_unreadable warning, not found")
	}
}

// ---------------------------------------------------------------------------
// ListOrgProjects discovery
// ---------------------------------------------------------------------------

func TestExport_Projects_ListOrgProjectsCalled_WithOrgID(t *testing.T) {
	// ListOrgProjects must be called with the org UUID (not the slug).
	calledWith := ""
	ex := &exporter.Exporter{
		Org: &fakeOrgAPI{
			getOrganization: func(string) (*org.Organization, error) { return defaultOrg(), nil },
		},
		Contexts: &fakeContextAPI{},
		Projects: &fakeProjectAPI{
			listOrgProjects: func(orgID string) ([]project.OrgProject, error) {
				calledWith = orgID
				return nil, nil
			},
		},
	}

	_, err := ex.Export(exporter.Options{OrgSlug: "gh/myorg", IncludeProjects: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calledWith != "org-uuid-123" {
		t.Errorf("ListOrgProjects called with %q, want org-uuid-123", calledWith)
	}
}

func TestExport_Projects_ListOrgProjects_AppOrgSlug(t *testing.T) {
	// GitHub App org (circleci/<uuid>) — ListOrgProjects returns App slugs.
	appOrg := &org.Organization{
		ID:      "app-org-uuid",
		Name:    "myorg",
		Slug:    "circleci/app-org-uuid",
		VCSType: "circleci",
	}
	ex := &exporter.Exporter{
		Org: &fakeOrgAPI{
			getOrganization: func(string) (*org.Organization, error) { return appOrg, nil },
		},
		Contexts: &fakeContextAPI{},
		Projects: &fakeProjectAPI{
			listOrgProjects: func(orgID string) ([]project.OrgProject, error) {
				if orgID != "app-org-uuid" {
					t.Errorf("ListOrgProjects orgID: got %q want app-org-uuid", orgID)
				}
				return []project.OrgProject{
					{ID: "proj-app-1", Slug: "circleci/app-org-uuid/proj-uuid-1", Name: "myapp"},
				}, nil
			},
			getProject: func(slug string) (*project.Project, error) {
				return &project.Project{Slug: slug, ID: "proj-app-1", Name: "myapp"}, nil
			},
		},
	}

	m, err := ex.Export(exporter.Options{OrgSlug: "circleci/app-org-uuid", IncludeProjects: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m.Projects) != 1 {
		t.Fatalf("expected 1 project, got %d", len(m.Projects))
	}
	if m.Projects[0].Slug != "circleci/app-org-uuid/proj-uuid-1" {
		t.Errorf("unexpected slug: %q", m.Projects[0].Slug)
	}
}

func TestExport_Projects_FollowedFlag_SetForFollowedProject(t *testing.T) {
	// Projects in the followed list have Followed=true; others have Followed=false.
	ex := &exporter.Exporter{
		Org: &fakeOrgAPI{
			getOrganization: func(string) (*org.Organization, error) { return defaultOrg(), nil },
		},
		Contexts: &fakeContextAPI{},
		Projects: &fakeProjectAPI{
			listOrgProjects: func(orgID string) ([]project.OrgProject, error) {
				return []project.OrgProject{
					{ID: "pid-web", Slug: "gh/myorg/web", Name: "web"},
					{ID: "pid-api", Slug: "gh/myorg/api", Name: "api"},
				}, nil
			},
			followedProjectsForOrg: func(orgName string) ([]project.FollowedProject, error) {
				// Only "web" is followed.
				return []project.FollowedProject{
					{Reponame: "web", VCSType: "github", Username: "myorg"},
				}, nil
			},
			getProject: func(slug string) (*project.Project, error) {
				return &project.Project{Slug: slug, ID: "pid", Name: slug}, nil
			},
		},
	}

	m, err := ex.Export(exporter.Options{OrgSlug: "gh/myorg", IncludeProjects: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m.Projects) != 2 {
		t.Fatalf("expected 2 projects, got %d", len(m.Projects))
	}
	bySlug := map[string]manifest.Project{}
	for _, p := range m.Projects {
		bySlug[p.Slug] = p
	}
	web := bySlug["gh/myorg/web"]
	if web.Followed == nil || !*web.Followed {
		t.Errorf("gh/myorg/web: Followed should be true, got %v", web.Followed)
	}
	api := bySlug["gh/myorg/api"]
	if api.Followed == nil || *api.Followed {
		t.Errorf("gh/myorg/api: Followed should be false, got %v", api.Followed)
	}
}

func TestExport_Projects_FollowedFlag_NilForAppOrg(t *testing.T) {
	// GitHub App orgs have no v1.1 slug form; Followed must be nil.
	appOrg := &org.Organization{
		ID:      "app-org-uuid",
		Name:    "myorg",
		Slug:    "circleci/app-org-uuid",
		VCSType: "circleci",
	}
	ex := &exporter.Exporter{
		Org: &fakeOrgAPI{
			getOrganization: func(string) (*org.Organization, error) { return appOrg, nil },
		},
		Contexts: &fakeContextAPI{},
		Projects: &fakeProjectAPI{
			listOrgProjects: func(orgID string) ([]project.OrgProject, error) {
				return []project.OrgProject{
					{ID: "pid-app", Slug: "circleci/app-org-uuid/proj-1", Name: "proj"},
				}, nil
			},
			getProject: func(slug string) (*project.Project, error) {
				return &project.Project{Slug: slug, ID: "pid-app", Name: "proj"}, nil
			},
		},
	}

	m, err := ex.Export(exporter.Options{OrgSlug: "circleci/app-org-uuid", IncludeProjects: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m.Projects) != 1 {
		t.Fatalf("expected 1 project, got %d", len(m.Projects))
	}
	if m.Projects[0].Followed != nil {
		t.Errorf("Followed should be nil for App org, got %v", m.Projects[0].Followed)
	}
}

func TestExport_Projects_FollowedListError_IsWarning(t *testing.T) {
	// If FollowedProjectsForOrg fails when building the cross-reference, a
	// warning is emitted and projects are still exported (Followed stays nil).
	ex := &exporter.Exporter{
		Org: &fakeOrgAPI{
			getOrganization: func(string) (*org.Organization, error) { return defaultOrg(), nil },
		},
		Contexts: &fakeContextAPI{},
		Projects: &fakeProjectAPI{
			listOrgProjects: func(orgID string) ([]project.OrgProject, error) {
				return []project.OrgProject{
					{ID: "pid", Slug: "gh/myorg/web", Name: "web"},
				}, nil
			},
			followedProjectsForOrg: func(orgName string) ([]project.FollowedProject, error) {
				return nil, errors.New("followed API down")
			},
			getProject: func(slug string) (*project.Project, error) {
				return &project.Project{Slug: slug, ID: "pid", Name: slug}, nil
			},
		},
	}

	m, err := ex.Export(exporter.Options{OrgSlug: "gh/myorg", IncludeProjects: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m.Projects) != 1 {
		t.Fatalf("expected 1 project, got %d", len(m.Projects))
	}
	found := false
	for _, w := range m.Warnings {
		if w.Code == "followed_list_unreadable" {
			found = true
		}
	}
	if !found {
		t.Error("expected followed_list_unreadable warning, not found")
	}
}

// ---------------------------------------------------------------------------
// Pipeline definitions and triggers capture
// ---------------------------------------------------------------------------

func TestExport_Projects_PipelineDefinitions_Captured(t *testing.T) {
	ex := &exporter.Exporter{
		Org: &fakeOrgAPI{
			getOrganization: func(string) (*org.Organization, error) { return defaultOrg(), nil },
		},
		Contexts: &fakeContextAPI{},
		Projects: &fakeProjectAPI{
			getProject: func(slug string) (*project.Project, error) {
				return &project.Project{Slug: slug, ID: "proj-uuid-extras", Name: "web"}, nil
			},
			listPipelineDefinitions: func(projectID string) ([]project.PipelineDefinition, error) {
				if projectID != "proj-uuid-extras" {
					t.Errorf("ListPipelineDefinitions projectID: got %q want proj-uuid-extras", projectID)
				}
				return []project.PipelineDefinition{
					{
						ID:          "def-1",
						Name:        "main",
						Description: "main pipeline",
						ConfigSource: project.PipelineSource{
							Provider: "github_app",
							Repo:     project.PipelineSourceRepo{FullName: "acme/web", ExternalID: "ext-1"},
							FilePath: ".circleci/config.yml",
						},
						CheckoutSource: project.PipelineSource{
							Provider: "github_app",
							Repo:     project.PipelineSourceRepo{FullName: "acme/web", ExternalID: "ext-1"},
						},
					},
				}, nil
			},
			listTriggers: func(projectID, defID string) ([]project.Trigger, error) {
				return nil, nil
			},
		},
	}

	m, err := ex.Export(exporter.Options{
		OrgSlug:         "gh/myorg",
		IncludeProjects: true,
		IncludeExtras:   true,
		ProjectSlugs:    []string{"gh/myorg/web"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m.Projects) != 1 {
		t.Fatalf("expected 1 project, got %d", len(m.Projects))
	}
	defs := m.Projects[0].PipelineDefinitions
	if len(defs) != 1 {
		t.Fatalf("expected 1 pipeline definition, got %d", len(defs))
	}
	d := defs[0]
	if d.Name != "main" {
		t.Errorf("definition Name: got %q want main", d.Name)
	}
	if d.Description != "main pipeline" {
		t.Errorf("definition Description: got %q want main pipeline", d.Description)
	}
	if d.ConfigSource.Provider != "github_app" {
		t.Errorf("ConfigSource.Provider: got %q want github_app", d.ConfigSource.Provider)
	}
	if d.ConfigSource.RepoFullName != "acme/web" {
		t.Errorf("ConfigSource.RepoFullName: got %q want acme/web", d.ConfigSource.RepoFullName)
	}
	if d.ConfigSource.RepoExternalID != "ext-1" {
		t.Errorf("ConfigSource.RepoExternalID: got %q want ext-1", d.ConfigSource.RepoExternalID)
	}
	if d.ConfigSource.FilePath != ".circleci/config.yml" {
		t.Errorf("ConfigSource.FilePath: got %q", d.ConfigSource.FilePath)
	}
	if d.CheckoutSource.Provider != "github_app" {
		t.Errorf("CheckoutSource.Provider: got %q want github_app", d.CheckoutSource.Provider)
	}
}

func TestExport_Projects_PipelineDefinitions_Triggers_GithubApp(t *testing.T) {
	// Triggers with github_app event_source are mapped to the manifest's
	// flattened TriggerEventSource.
	ex := &exporter.Exporter{
		Org: &fakeOrgAPI{
			getOrganization: func(string) (*org.Organization, error) { return defaultOrg(), nil },
		},
		Contexts: &fakeContextAPI{},
		Projects: &fakeProjectAPI{
			getProject: func(slug string) (*project.Project, error) {
				return &project.Project{Slug: slug, ID: "proj-trig-test", Name: "web"}, nil
			},
			listPipelineDefinitions: func(projectID string) ([]project.PipelineDefinition, error) {
				return []project.PipelineDefinition{
					{ID: "def-a", Name: "main",
						ConfigSource:   project.PipelineSource{Provider: "github_app"},
						CheckoutSource: project.PipelineSource{Provider: "github_app"},
					},
				}, nil
			},
			listTriggers: func(projectID, defID string) ([]project.Trigger, error) {
				return []project.Trigger{
					{
						ID: "t1", Name: "push", EventName: "push", Disabled: false,
						EventSource: project.TriggerEventSource{
							Provider: "github_app",
							Repo:     project.TriggerEventSourceRepo{FullName: "acme/web", ExternalID: "ext-42"},
						},
					},
				}, nil
			},
		},
	}

	m, err := ex.Export(exporter.Options{
		OrgSlug:         "gh/myorg",
		IncludeProjects: true,
		IncludeExtras:   true,
		ProjectSlugs:    []string{"gh/myorg/web"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m.Projects) != 1 || len(m.Projects[0].PipelineDefinitions) != 1 {
		t.Fatalf("expected 1 def, got %+v", m.Projects)
	}
	trigs := m.Projects[0].PipelineDefinitions[0].Triggers
	if len(trigs) != 1 {
		t.Fatalf("expected 1 trigger, got %d", len(trigs))
	}
	trig := trigs[0]
	if trig.Name != "push" {
		t.Errorf("trigger Name: got %q want push", trig.Name)
	}
	if trig.EventSource.Provider != "github_app" {
		t.Errorf("EventSource.Provider: got %q want github_app", trig.EventSource.Provider)
	}
	if trig.EventSource.RepoFullName != "acme/web" {
		t.Errorf("EventSource.RepoFullName: got %q want acme/web", trig.EventSource.RepoFullName)
	}
	if trig.EventSource.RepoExternalID != "ext-42" {
		t.Errorf("EventSource.RepoExternalID: got %q want ext-42", trig.EventSource.RepoExternalID)
	}
	// Webhook and schedule fields must be empty for github_app.
	if trig.EventSource.WebhookSender != "" {
		t.Errorf("WebhookSender should be empty for github_app, got %q", trig.EventSource.WebhookSender)
	}
}

func TestExport_Projects_PipelineDefinitions_Triggers_Webhook(t *testing.T) {
	// Webhook triggers: WebhookSender captured; URL must NOT be stored.
	ex := &exporter.Exporter{
		Org: &fakeOrgAPI{
			getOrganization: func(string) (*org.Organization, error) { return defaultOrg(), nil },
		},
		Contexts: &fakeContextAPI{},
		Projects: &fakeProjectAPI{
			getProject: func(slug string) (*project.Project, error) {
				return &project.Project{Slug: slug, ID: "proj-wh-test", Name: "web"}, nil
			},
			listPipelineDefinitions: func(projectID string) ([]project.PipelineDefinition, error) {
				return []project.PipelineDefinition{
					{ID: "def-b", Name: "main",
						ConfigSource:   project.PipelineSource{Provider: "github_app"},
						CheckoutSource: project.PipelineSource{Provider: "github_app"},
					},
				}, nil
			},
			listTriggers: func(projectID, defID string) ([]project.Trigger, error) {
				return []project.Trigger{
					{
						ID: "t-wh", Name: "ext-hook", Disabled: false,
						EventSource: project.TriggerEventSource{
							Provider: "webhook",
							Webhook: project.TriggerEventSourceWebhook{
								URL:    "https://circleci.com/hooks/123?secret=**REDACTED**",
								Sender: "deploy-bot",
							},
						},
					},
				}, nil
			},
		},
	}

	m, err := ex.Export(exporter.Options{
		OrgSlug:         "gh/myorg",
		IncludeProjects: true,
		IncludeExtras:   true,
		ProjectSlugs:    []string{"gh/myorg/web"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m.Projects[0].PipelineDefinitions[0].Triggers) != 1 {
		t.Fatalf("expected 1 trigger")
	}
	trig := m.Projects[0].PipelineDefinitions[0].Triggers[0]
	if trig.EventSource.Provider != "webhook" {
		t.Errorf("Provider: got %q want webhook", trig.EventSource.Provider)
	}
	if trig.EventSource.WebhookSender != "deploy-bot" {
		t.Errorf("WebhookSender: got %q want deploy-bot", trig.EventSource.WebhookSender)
	}
	// RepoFullName must NOT be populated for webhook provider.
	if trig.EventSource.RepoFullName != "" {
		t.Errorf("RepoFullName should be empty for webhook, got %q", trig.EventSource.RepoFullName)
	}
}

func TestExport_Projects_PipelineDefinitions_Triggers_Schedule(t *testing.T) {
	// Schedule triggers: ScheduleCron and ScheduleActor captured.
	ex := &exporter.Exporter{
		Org: &fakeOrgAPI{
			getOrganization: func(string) (*org.Organization, error) { return defaultOrg(), nil },
		},
		Contexts: &fakeContextAPI{},
		Projects: &fakeProjectAPI{
			getProject: func(slug string) (*project.Project, error) {
				return &project.Project{Slug: slug, ID: "proj-sched-test", Name: "web"}, nil
			},
			listPipelineDefinitions: func(projectID string) ([]project.PipelineDefinition, error) {
				return []project.PipelineDefinition{
					{ID: "def-c", Name: "main",
						ConfigSource:   project.PipelineSource{Provider: "github_app"},
						CheckoutSource: project.PipelineSource{Provider: "github_app"},
					},
				}, nil
			},
			listTriggers: func(projectID, defID string) ([]project.Trigger, error) {
				return []project.Trigger{
					{
						ID: "t-sched", Name: "nightly", Disabled: false,
						EventSource: project.TriggerEventSource{
							Provider: "schedule",
							Schedule: project.TriggerEventSourceSchedule{
								CronExpression:   "0 3 * * *",
								AttributionActor: "system",
							},
						},
					},
				}, nil
			},
		},
	}

	m, err := ex.Export(exporter.Options{
		OrgSlug:         "gh/myorg",
		IncludeProjects: true,
		IncludeExtras:   true,
		ProjectSlugs:    []string{"gh/myorg/web"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	trig := m.Projects[0].PipelineDefinitions[0].Triggers[0]
	if trig.EventSource.Provider != "schedule" {
		t.Errorf("Provider: got %q want schedule", trig.EventSource.Provider)
	}
	if trig.EventSource.ScheduleCron != "0 3 * * *" {
		t.Errorf("ScheduleCron: got %q want '0 3 * * *'", trig.EventSource.ScheduleCron)
	}
	if trig.EventSource.ScheduleActor != "system" {
		t.Errorf("ScheduleActor: got %q want system", trig.EventSource.ScheduleActor)
	}
}

func TestExport_Projects_PipelineDefinitions_Unreadable_IsWarning(t *testing.T) {
	// ListPipelineDefinitions error → warning, export continues.
	ex := &exporter.Exporter{
		Org: &fakeOrgAPI{
			getOrganization: func(string) (*org.Organization, error) { return defaultOrg(), nil },
		},
		Contexts: &fakeContextAPI{},
		Projects: &fakeProjectAPI{
			getProject: func(slug string) (*project.Project, error) {
				return &project.Project{Slug: slug, ID: "proj-err-test", Name: "web"}, nil
			},
			listPipelineDefinitions: func(projectID string) ([]project.PipelineDefinition, error) {
				return nil, errors.New("pipeline definitions API unavailable")
			},
		},
	}

	m, err := ex.Export(exporter.Options{
		OrgSlug:         "gh/myorg",
		IncludeProjects: true,
		IncludeExtras:   true,
		ProjectSlugs:    []string{"gh/myorg/web"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m.Projects) != 1 {
		t.Fatalf("expected 1 project, got %d", len(m.Projects))
	}
	if len(m.Projects[0].PipelineDefinitions) != 0 {
		t.Errorf("expected 0 defs when unreadable, got %d", len(m.Projects[0].PipelineDefinitions))
	}
	found := false
	for _, w := range m.Warnings {
		if w.Code == "pipeline_definitions_unreadable" {
			found = true
		}
	}
	if !found {
		t.Error("expected pipeline_definitions_unreadable warning, not found")
	}
}

func TestExport_Projects_Triggers_Unreadable_IsWarning(t *testing.T) {
	// ListTriggers error → warning per definition; other defs still captured.
	ex := &exporter.Exporter{
		Org: &fakeOrgAPI{
			getOrganization: func(string) (*org.Organization, error) { return defaultOrg(), nil },
		},
		Contexts: &fakeContextAPI{},
		Projects: &fakeProjectAPI{
			getProject: func(slug string) (*project.Project, error) {
				return &project.Project{Slug: slug, ID: "proj-trig-err", Name: "web"}, nil
			},
			listPipelineDefinitions: func(projectID string) ([]project.PipelineDefinition, error) {
				return []project.PipelineDefinition{
					{ID: "def-fail", Name: "main",
						ConfigSource:   project.PipelineSource{Provider: "github_app"},
						CheckoutSource: project.PipelineSource{Provider: "github_app"},
					},
				}, nil
			},
			listTriggers: func(projectID, defID string) ([]project.Trigger, error) {
				return nil, errors.New("triggers API unavailable")
			},
		},
	}

	m, err := ex.Export(exporter.Options{
		OrgSlug:         "gh/myorg",
		IncludeProjects: true,
		IncludeExtras:   true,
		ProjectSlugs:    []string{"gh/myorg/web"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m.Projects) != 1 {
		t.Fatalf("expected 1 project, got %d", len(m.Projects))
	}
	// Definition is still captured, triggers are empty.
	if len(m.Projects[0].PipelineDefinitions) != 1 {
		t.Fatalf("expected 1 definition even on trigger error, got %d", len(m.Projects[0].PipelineDefinitions))
	}
	if len(m.Projects[0].PipelineDefinitions[0].Triggers) != 0 {
		t.Errorf("expected 0 triggers on error, got %d", len(m.Projects[0].PipelineDefinitions[0].Triggers))
	}
	found := false
	for _, w := range m.Warnings {
		if w.Code == "triggers_unreadable" {
			found = true
		}
	}
	if !found {
		t.Error("expected triggers_unreadable warning, not found")
	}
}

func TestExport_Projects_PipelineDefinitions_SkippedWhenNoExtras(t *testing.T) {
	// Pipeline definitions must NOT be captured when IncludeExtras=false.
	defsCalled := false
	ex := &exporter.Exporter{
		Org: &fakeOrgAPI{
			getOrganization: func(string) (*org.Organization, error) { return defaultOrg(), nil },
		},
		Contexts: &fakeContextAPI{},
		Projects: &fakeProjectAPI{
			getProject: func(slug string) (*project.Project, error) {
				return &project.Project{Slug: slug, ID: "pid", Name: "web"}, nil
			},
			listPipelineDefinitions: func(projectID string) ([]project.PipelineDefinition, error) {
				defsCalled = true
				return nil, nil
			},
		},
	}

	_, err := ex.Export(exporter.Options{
		OrgSlug:         "gh/myorg",
		IncludeProjects: true,
		IncludeExtras:   false,
		ProjectSlugs:    []string{"gh/myorg/web"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if defsCalled {
		t.Error("ListPipelineDefinitions should NOT be called when IncludeExtras=false")
	}
}

func TestExport_Projects_PipelineDefinitions_SortStable(t *testing.T) {
	// Pipeline definitions and triggers are sorted by name.
	ex := &exporter.Exporter{
		Org: &fakeOrgAPI{
			getOrganization: func(string) (*org.Organization, error) { return defaultOrg(), nil },
		},
		Contexts: &fakeContextAPI{},
		Projects: &fakeProjectAPI{
			getProject: func(slug string) (*project.Project, error) {
				return &project.Project{Slug: slug, ID: "pid-sort", Name: "web"}, nil
			},
			listPipelineDefinitions: func(projectID string) ([]project.PipelineDefinition, error) {
				return []project.PipelineDefinition{
					{ID: "d-z", Name: "zebra",
						ConfigSource:   project.PipelineSource{Provider: "github_app"},
						CheckoutSource: project.PipelineSource{Provider: "github_app"},
					},
					{ID: "d-a", Name: "alpha",
						ConfigSource:   project.PipelineSource{Provider: "github_app"},
						CheckoutSource: project.PipelineSource{Provider: "github_app"},
					},
				}, nil
			},
			listTriggers: func(projectID, defID string) ([]project.Trigger, error) {
				return []project.Trigger{
					{ID: "t-z", Name: "zzz", Disabled: false, EventSource: project.TriggerEventSource{Provider: "github_app"}},
					{ID: "t-a", Name: "aaa", Disabled: false, EventSource: project.TriggerEventSource{Provider: "github_app"}},
				}, nil
			},
		},
	}

	m, err := ex.Export(exporter.Options{
		OrgSlug:         "gh/myorg",
		IncludeProjects: true,
		IncludeExtras:   true,
		ProjectSlugs:    []string{"gh/myorg/web"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defs := m.Projects[0].PipelineDefinitions
	if len(defs) != 2 {
		t.Fatalf("expected 2 defs, got %d", len(defs))
	}
	if defs[0].Name != "alpha" || defs[1].Name != "zebra" {
		t.Errorf("definitions not sorted: %v %v", defs[0].Name, defs[1].Name)
	}
	trigs := defs[0].Triggers
	if len(trigs) != 2 {
		t.Fatalf("expected 2 triggers, got %d", len(trigs))
	}
	if trigs[0].Name != "aaa" || trigs[1].Name != "zzz" {
		t.Errorf("triggers not sorted: %v %v", trigs[0].Name, trigs[1].Name)
	}
}
