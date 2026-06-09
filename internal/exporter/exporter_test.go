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
	getOrganization func(slugOrID string) (*org.Organization, error)
	getOrgSettings  func(vcsType, orgName string) (*org.OrgSettings, error)
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

type fakeContextAPI struct {
	listContexts      func(ownerID, ownerSlug string) ([]cctx.Context, error)
	listEnvVars       func(contextID string) ([]cctx.EnvVar, error)
	listRestrictions  func(contextID string) ([]cctx.Restriction, error)
	listOrgGroups     func(orgID string) ([]cctx.Group, error)
	listContextGroups func(contextID string) ([]cctx.Group, error)
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

func (f *fakeContextAPI) ListOrgGroups(orgID string) ([]cctx.Group, error) {
	if f.listOrgGroups != nil {
		return f.listOrgGroups(orgID)
	}
	return nil, nil
}

func (f *fakeContextAPI) ListContextGroups(contextID string) ([]cctx.Group, error) {
	if f.listContextGroups != nil {
		return f.listContextGroups(contextID)
	}
	return nil, nil
}

type fakeProjectAPI struct {
	getProject             func(slug string) (*project.Project, error)
	getSettings            func(provider, orgName, proj string) (*project.AdvancedSettings, error)
	listEnvVars            func(slug string) ([]project.EnvVar, error)
	listCheckoutKeys       func(slug string) ([]project.CheckoutKey, error)
	listWebhooks           func(projectID string) ([]project.Webhook, error)
	listSchedules          func(slug string) ([]project.Schedule, error)
	followedProjectsForOrg func(orgName string) ([]project.FollowedProject, error)
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
	settingsCalled := false
	trueVal := true
	ex := &exporter.Exporter{
		Org: &fakeOrgAPI{
			getOrganization: func(string) (*org.Organization, error) { return defaultOrg(), nil },
			getOrgSettings: func(vcsType, orgName string) (*org.OrgSettings, error) {
				settingsCalled = true
				if vcsType != "github" {
					t.Errorf("GetOrgSettings vcsType: got %q want %q", vcsType, "github")
				}
				if orgName != "myorg" {
					t.Errorf("GetOrgSettings orgName: got %q want %q", orgName, "myorg")
				}
				return &org.OrgSettings{RequireContextGroupRestriction: &trueVal}, nil
			},
		},
		Contexts: &fakeContextAPI{},
		Projects: &fakeProjectAPI{},
	}

	m, err := ex.Export(exporter.Options{OrgSlug: "gh/myorg"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !settingsCalled {
		t.Error("GetOrgSettings was not called for gh/ slug")
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
}

func TestExport_OrgSettingsSkipped_CircleCISlug(t *testing.T) {
	settingsCalled := false
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
			getOrgSettings: func(vcsType, orgName string) (*org.OrgSettings, error) {
				settingsCalled = true
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
	if settingsCalled {
		t.Error("GetOrgSettings should NOT be called for circleci/ slug")
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
			listOrgGroups: func(orgID string) ([]cctx.Group, error) {
				return []cctx.Group{
					{ID: "group-uuid-1", Name: "security-team", GroupType: "TEAM"},
				}, nil
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
	if len(m.Contexts) != 1 || len(m.Contexts[0].Restrictions) != 1 {
		t.Fatalf("unexpected contexts/restrictions: %+v", m.Contexts)
	}
	r := m.Contexts[0].Restrictions[0]
	if r.Type != "group" {
		t.Errorf("restriction type: got %q want %q", r.Type, "group")
	}
	if r.Name != "security-team" {
		t.Errorf("restriction name: got %q want %q (should resolve group UUID)", r.Name, "security-team")
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
			listContextGroups: func(contextID string) ([]cctx.Group, error) {
				return []cctx.Group{
					{ID: "sg-1", Name: "eng-team", GroupType: "TEAM"},
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
	if sg.ID != "sg-1" || sg.Name != "eng-team" || sg.GroupType != "TEAM" {
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

func TestExport_Projects_OrgSettingsError_IsWarning(t *testing.T) {
	ex := &exporter.Exporter{
		Org: &fakeOrgAPI{
			getOrganization: func(string) (*org.Organization, error) { return defaultOrg(), nil },
			getOrgSettings: func(vcsType, orgName string) (*org.OrgSettings, error) {
				return nil, errors.New("settings unavailable")
			},
		},
		Contexts: &fakeContextAPI{},
		Projects: &fakeProjectAPI{},
	}

	m, err := ex.Export(exporter.Options{OrgSlug: "gh/myorg"})
	if err != nil {
		t.Fatalf("settings error should not fail Export, got: %v", err)
	}
	found := false
	for _, w := range m.Warnings {
		if w.Code == "org_settings_unreadable" {
			found = true
		}
	}
	if !found {
		t.Error("expected org_settings_unreadable warning, not found")
	}
}
