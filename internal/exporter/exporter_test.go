package exporter_test

import (
	"errors"
	"strings"
	"testing"

	cctx "github.com/CircleCI-Public/circleci-org-migration-cli/api/context"
	"github.com/CircleCI-Public/circleci-org-migration-cli/api/org"
	"github.com/CircleCI-Public/circleci-org-migration-cli/api/project"
	"github.com/CircleCI-Public/circleci-org-migration-cli/internal/exporter"
	"github.com/CircleCI-Public/circleci-org-migration-cli/internal/manifest"
)

// ---------------------------------------------------------------------------
// Fake implementations
// ---------------------------------------------------------------------------

type fakeOrgAPI struct {
	getOrganization    func(slugOrID string) (*org.Organization, error)
	getOrgSettings     func(vcsType, orgName string) (*org.OrgSettings, error)
	getFeatureFlags    func(vcsType, orgName string) (map[string]bool, error)
	getOIDCClaims      func(orgID string) ([]string, string, error)
	getURLOrbAllowList func(slugOrID string) ([]org.URLOrbAllowEntry, error)
	getPolicyBundle    func(ownerID string) (map[string]string, error)
	getPolicyEnf       func(ownerID string) (bool, error)
	getAuditLogConfigs func(orgID string) ([]org.AuditLogConfig, error)
	getSSOEnforced     func(orgID string) (bool, error)
	getSSOConnection   func(orgID string) (map[string]any, bool, error)
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

func TestExport_OrgSettingsSkipped_CircleCISlug(t *testing.T) {
	featureFlagsCalled := false
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
				featureFlagsCalled = true
				return nil, nil
			},
		},
		Contexts: &fakeContextAPI{},
		Projects: &fakeProjectAPI{},
	}

	_, err := ex.Export(exporter.Options{OrgSlug: "circleci/some-uuid"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if featureFlagsCalled {
		t.Error("GetFeatureFlags should NOT be called for circleci/ slug (no vcs/name form)")
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

func TestExport_Projects_DiscoveryFromFollowed(t *testing.T) {
	ex := &exporter.Exporter{
		Org: &fakeOrgAPI{
			getOrganization: func(string) (*org.Organization, error) { return defaultOrg(), nil },
		},
		Contexts: &fakeContextAPI{},
		Projects: &fakeProjectAPI{
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
	slugs := map[string]bool{}
	for _, p := range m.Projects {
		slugs[p.Slug] = true
	}
	if !slugs["gh/myorg/web"] || !slugs["gh/myorg/api"] {
		t.Errorf("unexpected project slugs: %v", slugs)
	}
}

func TestExport_Projects_DiscoveryFollowedOnly_Warning(t *testing.T) {
	ex := &exporter.Exporter{
		Org: &fakeOrgAPI{
			getOrganization: func(string) (*org.Organization, error) { return defaultOrg(), nil },
		},
		Contexts: &fakeContextAPI{},
		Projects: &fakeProjectAPI{
			followedProjectsForOrg: func(orgName string) ([]project.FollowedProject, error) {
				return []project.FollowedProject{{Reponame: "web", VCSType: "github", Username: "myorg"}}, nil
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
		t.Error("expected project_discovery_followed_only warning when no explicit slugs given")
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

func TestExport_Projects_ExplicitAndDiscovered_Deduplicated(t *testing.T) {
	ex := &exporter.Exporter{
		Org: &fakeOrgAPI{
			getOrganization: func(string) (*org.Organization, error) { return defaultOrg(), nil },
		},
		Contexts: &fakeContextAPI{},
		Projects: &fakeProjectAPI{
			followedProjectsForOrg: func(orgName string) ([]project.FollowedProject, error) {
				return []project.FollowedProject{
					{Reponame: "web", VCSType: "github", Username: "myorg"},
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
	// "gh/myorg/web" is explicit AND discovered; should appear only once.
	count := 0
	for _, p := range m.Projects {
		if p.Slug == "gh/myorg/web" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("gh/myorg/web appears %d times, expected exactly 1", count)
	}
	if len(m.Projects) != 2 {
		t.Errorf("expected 2 projects, got %d", len(m.Projects))
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
