package exporter_test

import (
	"context"
	"testing"

	"github.com/AwesomeCICD/circleci-org-migration-cli/api/org"
	"github.com/AwesomeCICD/circleci-org-migration-cli/api/project"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/exporter"
)

// newCIAMExporter returns an Exporter wired with the provided fakeOrgAPI.
// The project API is configured to return a single project with a known ID
// so per-project CIAM calls have a real project UUID to work with.
func newCIAMExporter(orgAPI *fakeOrgAPI) *exporter.Exporter {
	projAPI := &fakeProjectAPI{
		listOrgProjects: func(orgID string) ([]project.OrgProject, error) {
			return []project.OrgProject{
				{ID: "proj-uuid-1", Slug: "circleci/org-uuid/proj-uuid-1", Name: "my-project"},
			}, nil
		},
		getProject: func(slug string) (*project.Project, error) {
			return &project.Project{
				ID:   "proj-uuid-1",
				Name: "my-project",
				Slug: slug,
			}, nil
		},
	}
	return &exporter.Exporter{
		Org:      orgAPI,
		Contexts: &fakeContextAPI{},
		Projects: projAPI,
	}
}

const ciamOrgSlug = "circleci/aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
const ciamOrgID = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"

// baseCIAMOrgAPI returns a fakeOrgAPI for a circleci-type org with all
// optional endpoints returning empty/nil so tests don't need to stub everything.
func baseCIAMOrgAPI() *fakeOrgAPI {
	return &fakeOrgAPI{
		getOrganization: func(slugOrID string) (*org.Organization, error) {
			return &org.Organization{
				ID:      ciamOrgID,
				Name:    "Acme",
				Slug:    ciamOrgSlug,
				VCSType: "circleci",
			}, nil
		},
		listGroups: func(orgID string) ([]org.Group, error) {
			return nil, nil
		},
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Gate: non-circleci-type org produces no CIAM data
// ─────────────────────────────────────────────────────────────────────────────

func TestExportCIAM_SkippedForGitHubOAuthOrg(t *testing.T) {
	f := &fakeOrgAPI{
		getOrganization: func(_ string) (*org.Organization, error) {
			return &org.Organization{
				ID: "org-gh", Slug: "gh/acme", Name: "Acme", VCSType: "github",
			}, nil
		},
	}
	e := newCIAMExporter(f)
	m, err := e.Export(context.Background(), exporter.Options{
		OrgSlug:         "gh/acme",
		IncludeProjects: false,
		IncludeContexts: false,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.CIAM != nil {
		t.Errorf("expected CIAM to be nil for GitHub OAuth org, got %+v", m.CIAM)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Happy path: org roles captured
// ─────────────────────────────────────────────────────────────────────────────

func TestExportCIAM_OrgRolesCaptured(t *testing.T) {
	f := baseCIAMOrgAPI()
	f.listOrgRoleGrants = func(orgID string) ([]org.OrgRoleGrant, error) {
		return []org.OrgRoleGrant{
			{UserID: "uid-1", Email: "alice@example.com", Username: "alice", Role: "org-admin"},
			{UserID: "uid-2", Email: "bob@example.com", Username: "bob", Role: "org-viewer"},
		}, nil
	}

	e := newCIAMExporter(f)
	m, err := e.Export(context.Background(), exporter.Options{
		OrgSlug:         ciamOrgSlug,
		IncludeProjects: false,
		IncludeContexts: false,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.CIAM == nil {
		t.Fatal("expected CIAM to be non-nil")
	}
	if len(m.CIAM.OrgRoles) != 2 {
		t.Fatalf("expected 2 org roles, got %d", len(m.CIAM.OrgRoles))
	}
	if m.CIAM.OrgRoles[0].Email != "alice@example.com" || m.CIAM.OrgRoles[0].Role != "org-admin" {
		t.Errorf("unexpected first org role: %+v", m.CIAM.OrgRoles[0])
	}
	if m.CIAM.OrgRoles[1].Email != "bob@example.com" || m.CIAM.OrgRoles[1].Role != "org-viewer" {
		t.Errorf("unexpected second org role: %+v", m.CIAM.OrgRoles[1])
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Groups captured (not the default "All members" group)
// ─────────────────────────────────────────────────────────────────────────────

func TestExportCIAM_GroupsCaptured_SkipsDefaultAllMembersGroup(t *testing.T) {
	f := baseCIAMOrgAPI()
	f.listOrgRoleGrants = func(_ string) ([]org.OrgRoleGrant, error) {
		return []org.OrgRoleGrant{
			{UserID: "uid-1", Email: "alice@example.com", Role: "org-admin"},
		}, nil
	}
	f.listGroups = func(orgID string) ([]org.Group, error) {
		return []org.Group{
			// Default "All members" group — ID == orgID, must be skipped.
			{ID: ciamOrgID, Name: "All members"},
			{ID: "grp-security", Name: "security-team"},
			{ID: "grp-platform", Name: "platform-team"},
		}, nil
	}

	e := newCIAMExporter(f)
	m, err := e.Export(context.Background(), exporter.Options{
		OrgSlug:         ciamOrgSlug,
		IncludeProjects: false,
		IncludeContexts: false,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.CIAM == nil {
		t.Fatal("expected CIAM to be non-nil")
	}
	if len(m.CIAM.Groups) != 2 {
		t.Fatalf("expected 2 groups (default all-members skipped), got %d: %+v", len(m.CIAM.Groups), m.CIAM.Groups)
	}
	for _, g := range m.CIAM.Groups {
		if g.Name == "All members" {
			t.Error("default 'All members' group should be excluded from CIAM.Groups")
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Per-project user grants captured
// ─────────────────────────────────────────────────────────────────────────────

func TestExportCIAM_ProjectUserGrantsCaptured(t *testing.T) {
	f := baseCIAMOrgAPI()
	f.listOrgRoleGrants = func(_ string) ([]org.OrgRoleGrant, error) {
		return []org.OrgRoleGrant{
			{UserID: "uid-1", Email: "alice@example.com", Role: "org-admin"},
		}, nil
	}
	f.listProjectUserRoleGrants = func(orgID, projectID string) ([]org.ProjectUserRoleGrant, error) {
		if projectID != "proj-uuid-1" {
			return nil, nil
		}
		return []org.ProjectUserRoleGrant{
			{UserID: "uid-1", Email: "alice@example.com", Username: "alice", Role: "project-admin"},
		}, nil
	}

	e := newCIAMExporter(f)
	m, err := e.Export(context.Background(), exporter.Options{
		OrgSlug:         ciamOrgSlug,
		IncludeProjects: true,
		IncludeContexts: false,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.CIAM == nil {
		t.Fatal("expected CIAM to be non-nil")
	}
	if len(m.CIAM.ProjectUserGrants) != 1 {
		t.Fatalf("expected 1 project user grant, got %d", len(m.CIAM.ProjectUserGrants))
	}
	g := m.CIAM.ProjectUserGrants[0]
	if g.Email != "alice@example.com" || g.Role != "project-admin" {
		t.Errorf("unexpected project user grant: %+v", g)
	}
	if g.ProjectName != "my-project" {
		t.Errorf("expected project name %q, got %q", "my-project", g.ProjectName)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Read error adds warning, export continues
// ─────────────────────────────────────────────────────────────────────────────

func TestExportCIAM_OrgRoleGrantsReadError_AddsWarning(t *testing.T) {
	f := baseCIAMOrgAPI()
	f.listOrgRoleGrants = func(_ string) ([]org.OrgRoleGrant, error) {
		return nil, errFake("simulated API error")
	}

	e := newCIAMExporter(f)
	m, err := e.Export(context.Background(), exporter.Options{
		OrgSlug:         ciamOrgSlug,
		IncludeProjects: false,
		IncludeContexts: false,
	})
	if err != nil {
		t.Fatalf("Export should not fail on non-fatal CIAM error; got: %v", err)
	}
	// CIAM may be nil (no data captured) but the export itself completes.
	// A warning with code "org_role_grants_unreadable" must be present.
	found := false
	for _, w := range m.Warnings {
		if w.Code == "org_role_grants_unreadable" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected warning with code %q; got warnings: %+v", "org_role_grants_unreadable", m.Warnings)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// No CIAM data → CIAM field stays nil
// ─────────────────────────────────────────────────────────────────────────────

func TestExportCIAM_NoCIAMData_FieldIsNil(t *testing.T) {
	f := baseCIAMOrgAPI()
	// No role grants, no groups, no project grants.
	f.listOrgRoleGrants = func(_ string) ([]org.OrgRoleGrant, error) {
		return nil, nil
	}

	e := newCIAMExporter(f)
	m, err := e.Export(context.Background(), exporter.Options{
		OrgSlug:         ciamOrgSlug,
		IncludeProjects: false,
		IncludeContexts: false,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.CIAM != nil {
		t.Errorf("expected CIAM to be nil when no data; got %+v", m.CIAM)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Per-project group grants captured and group name resolved from ID
// ─────────────────────────────────────────────────────────────────────────────

func TestExportCIAM_ProjectGroupGrantsCapturedGroupNameResolved(t *testing.T) {
	f := baseCIAMOrgAPI()
	f.listOrgRoleGrants = func(_ string) ([]org.OrgRoleGrant, error) {
		return []org.OrgRoleGrant{
			{UserID: "uid-1", Email: "alice@example.com", Role: "org-admin"},
		}, nil
	}
	f.listGroups = func(_ string) ([]org.Group, error) {
		return []org.Group{
			{ID: "grp-sec-id", Name: "security-team"},
		}, nil
	}
	f.listProjectGroupRoleGrants = func(orgID, projectID string) ([]org.ProjectGroupRoleGrant, error) {
		if projectID != "proj-uuid-1" {
			return nil, nil
		}
		return []org.ProjectGroupRoleGrant{
			{GroupID: "grp-sec-id", Role: "project-contributor"},
		}, nil
	}

	e := newCIAMExporter(f)
	m, err := e.Export(context.Background(), exporter.Options{
		OrgSlug:         ciamOrgSlug,
		IncludeProjects: true,
		IncludeContexts: false,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.CIAM == nil {
		t.Fatal("expected CIAM to be non-nil")
	}
	if len(m.CIAM.ProjectGroupGrants) != 1 {
		t.Fatalf("expected 1 project group grant, got %d", len(m.CIAM.ProjectGroupGrants))
	}
	g := m.CIAM.ProjectGroupGrants[0]
	if g.GroupName != "security-team" {
		t.Errorf("expected group name %q resolved from ID, got %q", "security-team", g.GroupName)
	}
	if g.Role != "project-contributor" {
		t.Errorf("expected role %q, got %q", "project-contributor", g.Role)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Per-project group grants — group ID not in lookup → falls back to ID
// ─────────────────────────────────────────────────────────────────────────────

func TestExportCIAM_ProjectGroupGrantsUnknownGroupIDFallsback(t *testing.T) {
	f := baseCIAMOrgAPI()
	f.listOrgRoleGrants = func(_ string) ([]org.OrgRoleGrant, error) {
		return []org.OrgRoleGrant{
			{UserID: "uid-1", Email: "alice@example.com", Role: "org-admin"},
		}, nil
	}
	f.listGroups = func(_ string) ([]org.Group, error) {
		// No groups in list — group ID will not resolve.
		return nil, nil
	}
	f.listProjectGroupRoleGrants = func(orgID, projectID string) ([]org.ProjectGroupRoleGrant, error) {
		return []org.ProjectGroupRoleGrant{
			{GroupID: "unknown-grp-id", Role: "project-viewer"},
		}, nil
	}

	e := newCIAMExporter(f)
	m, err := e.Export(context.Background(), exporter.Options{
		OrgSlug:         ciamOrgSlug,
		IncludeProjects: true,
		IncludeContexts: false,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.CIAM == nil {
		t.Fatal("expected CIAM to be non-nil")
	}
	if len(m.CIAM.ProjectGroupGrants) != 1 {
		t.Fatalf("expected 1 project group grant, got %d", len(m.CIAM.ProjectGroupGrants))
	}
	// Should fall back to the raw group ID when name is unknown.
	g := m.CIAM.ProjectGroupGrants[0]
	if g.GroupName != "unknown-grp-id" {
		t.Errorf("expected fallback group name %q, got %q", "unknown-grp-id", g.GroupName)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Project user role grants read error adds warning, export continues
// ─────────────────────────────────────────────────────────────────────────────

func TestExportCIAM_ProjectUserGrantsReadError_AddsWarning(t *testing.T) {
	f := baseCIAMOrgAPI()
	f.listOrgRoleGrants = func(_ string) ([]org.OrgRoleGrant, error) {
		return []org.OrgRoleGrant{
			{UserID: "uid-1", Email: "alice@example.com", Role: "org-admin"},
		}, nil
	}
	f.listProjectUserRoleGrants = func(_, _ string) ([]org.ProjectUserRoleGrant, error) {
		return nil, errFake("project role grants API error")
	}

	e := newCIAMExporter(f)
	m, err := e.Export(context.Background(), exporter.Options{
		OrgSlug:         ciamOrgSlug,
		IncludeProjects: true,
		IncludeContexts: false,
	})
	if err != nil {
		t.Fatalf("Export should not fail on non-fatal CIAM error; got: %v", err)
	}

	found := false
	for _, w := range m.Warnings {
		if w.Code == "project_user_role_grants_unreadable" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected warning with code %q; got warnings: %+v", "project_user_role_grants_unreadable", m.Warnings)
	}
}

// errFake is a simple error for test stubs.
type errFake string

func (e errFake) Error() string { return string(e) }
