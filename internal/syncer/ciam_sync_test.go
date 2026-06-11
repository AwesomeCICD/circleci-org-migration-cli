package syncer

import (
	"fmt"
	"testing"

	"github.com/AwesomeCICD/circleci-org-migration-cli/api/org"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
)

// ─────────────────────────────────────────────────────────────────────────────
// Fake CIAMWriter
// ─────────────────────────────────────────────────────────────────────────────

type fakeCIAMWriter struct {
	listOrgRoleGrants   func(orgID string) ([]CIAMRoleGrant, error)
	setOrgUserRole      func(orgID, userID, role string) error
	listGroups          func(orgID string) ([]CIAMGroupInfo, error)
	createGroup         func(orgID, name, description string) (string, error)
	addUsersToGroup     func(orgID, groupID string, userIDs []string) error
	setProjectUserRole  func(orgID, projectID, userID, role string) error
	addProjectGroupRole func(orgID, projectID string, groupIDs []string, role string) error

	// Track calls for assertions.
	orgRolesCalled    bool
	groupsCreated     []string
	usersAdded        map[string][]string // groupID → []userID
	projectRolesSet   []string            // "orgID/projID/userID/role"
	projectGroupRoles []string            // "orgID/projID/groupID/role"
}

func (f *fakeCIAMWriter) ListOrgRoleGrants(orgID string) ([]CIAMRoleGrant, error) {
	f.orgRolesCalled = true
	if f.listOrgRoleGrants != nil {
		return f.listOrgRoleGrants(orgID)
	}
	return nil, nil
}

func (f *fakeCIAMWriter) SetOrgUserRole(orgID, userID, role string) error {
	if f.setOrgUserRole != nil {
		return f.setOrgUserRole(orgID, userID, role)
	}
	return nil
}

func (f *fakeCIAMWriter) ListGroups(orgID string) ([]CIAMGroupInfo, error) {
	if f.listGroups != nil {
		return f.listGroups(orgID)
	}
	return nil, nil
}

func (f *fakeCIAMWriter) CreateGroup(orgID, name, description string) (string, error) {
	f.groupsCreated = append(f.groupsCreated, name)
	if f.createGroup != nil {
		return f.createGroup(orgID, name, description)
	}
	return "new-group-id-" + name, nil
}

func (f *fakeCIAMWriter) AddUsersToGroup(orgID, groupID string, userIDs []string) error {
	if f.usersAdded == nil {
		f.usersAdded = map[string][]string{}
	}
	f.usersAdded[groupID] = append(f.usersAdded[groupID], userIDs...)
	if f.addUsersToGroup != nil {
		return f.addUsersToGroup(orgID, groupID, userIDs)
	}
	return nil
}

func (f *fakeCIAMWriter) SetProjectUserRole(orgID, projectID, userID, role string) error {
	f.projectRolesSet = append(f.projectRolesSet, fmt.Sprintf("%s/%s/%s/%s", orgID, projectID, userID, role))
	if f.setProjectUserRole != nil {
		return f.setProjectUserRole(orgID, projectID, userID, role)
	}
	return nil
}

func (f *fakeCIAMWriter) AddProjectGroupRole(orgID, projectID string, groupIDs []string, role string) error {
	for _, gid := range groupIDs {
		f.projectGroupRoles = append(f.projectGroupRoles, fmt.Sprintf("%s/%s/%s/%s", orgID, projectID, gid, role))
	}
	if f.addProjectGroupRole != nil {
		return f.addProjectGroupRole(orgID, projectID, groupIDs, role)
	}
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

func newCIAMTestSyncer(ciamWriter *fakeCIAMWriter, destVCSType string) *Syncer {
	return &Syncer{
		Org: &fakeOrgResolver{
			getOrganization: func(slug string) (*org.Organization, error) {
				return &org.Organization{
					ID:      "dest-org-uuid",
					Slug:    slug,
					VCSType: destVCSType,
				}, nil
			},
			resolveOrgID: func(_ string) (string, error) {
				return "dest-org-uuid", nil
			},
		},
		CIAM: ciamWriter,
	}
}

func ciamManifest() *manifest.Manifest {
	return &manifest.Manifest{
		Source: manifest.Source{
			Org: manifest.Org{
				ID:      "src-org-uuid",
				Slug:    "circleci/src-org-uuid",
				VCSType: "circleci",
			},
		},
		CIAM: &manifest.CIAMData{
			OrgRoles: []manifest.CIAMOrgRole{
				{Email: "alice@example.com", Role: "org-admin"},
				{Email: "unmatched@example.com", Role: "org-viewer"},
			},
			Groups: []manifest.CIAMGroup{
				{Name: "security-team", Description: "Security team"},
			},
			ProjectUserGrants: []manifest.CIAMProjectUserGrant{
				{
					ProjectName: "my-project",
					ProjectSlug: "circleci/src-org-uuid/proj-uuid-1",
					Email:       "alice@example.com",
					Role:        "project-admin",
				},
			},
			ProjectGroupGrants: []manifest.CIAMProjectGroupGrant{
				{
					ProjectName: "my-project",
					ProjectSlug: "circleci/src-org-uuid/proj-uuid-1",
					GroupName:   "security-team",
					Role:        "project-contributor",
				},
			},
		},
		Projects: []manifest.Project{
			{
				Slug:     "circleci/src-org-uuid/proj-uuid-1",
				Name:     "my-project",
				SourceID: "proj-uuid-1",
			},
		},
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test: nil CIAM field in manifest → no-op
// ─────────────────────────────────────────────────────────────────────────────

func TestSyncCIAM_NilManifestCIAM_NoOp(t *testing.T) {
	ciam := &fakeCIAMWriter{}
	s := newCIAMTestSyncer(ciam, "circleci")
	m := &manifest.Manifest{
		Source: manifest.Source{
			Org: manifest.Org{Slug: "circleci/dest-org-uuid", VCSType: "circleci"},
		},
		CIAM: nil,
	}
	report, err := s.SyncCIAM(m, nil, Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(report.Actions) != 0 {
		t.Errorf("expected no actions for nil CIAM, got %d", len(report.Actions))
	}
	if ciam.orgRolesCalled {
		t.Error("ListOrgRoleGrants should not be called when CIAM is nil")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test: non-circleci destination → manual action, no writes
// ─────────────────────────────────────────────────────────────────────────────

func TestSyncCIAM_NonCircleCIDestination_EmitsManual(t *testing.T) {
	ciam := &fakeCIAMWriter{}
	s := newCIAMTestSyncer(ciam, "github")
	m := ciamManifest()
	report, err := s.SyncCIAM(m, nil, Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should have one "manual" action indicating dest is not standalone.
	hasManual := false
	for _, a := range report.Actions {
		if a.Status == "manual" && a.Target == "destination_not_standalone" {
			hasManual = true
		}
	}
	if !hasManual {
		t.Errorf("expected manual action for non-circleci destination; got actions: %+v", report.Actions)
	}
	if len(ciam.groupsCreated) > 0 {
		t.Errorf("expected no group creates for non-circleci destination; got %v", ciam.groupsCreated)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test: dry-run — no writes, plans recorded
// ─────────────────────────────────────────────────────────────────────────────

func TestSyncCIAM_DryRun_NoWrites(t *testing.T) {
	ciam := &fakeCIAMWriter{
		listOrgRoleGrants: func(_ string) ([]CIAMRoleGrant, error) {
			return []CIAMRoleGrant{
				{UserID: "uid-alice", Email: "alice@example.com"},
			}, nil
		},
		listGroups: func(_ string) ([]CIAMGroupInfo, error) {
			return nil, nil // no existing groups
		},
	}
	var createGroupCalled bool
	ciam.createGroup = func(_, _, _ string) (string, error) {
		createGroupCalled = true
		return "new-id", nil
	}
	var setOrgRoleCalled bool
	ciam.setOrgUserRole = func(_, _, _ string) error {
		setOrgRoleCalled = true
		return nil
	}

	s := newCIAMTestSyncer(ciam, "circleci")
	m := ciamManifest()

	report, err := s.SyncCIAM(m, nil, Options{Apply: false})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if createGroupCalled {
		t.Error("CreateGroup should NOT be called in dry-run mode")
	}
	if setOrgRoleCalled {
		t.Error("SetOrgUserRole should NOT be called in dry-run mode")
	}

	// But plans should be recorded.
	var planCount int
	for _, a := range report.Actions {
		if a.Status == "created" || a.Status == "set" {
			planCount++
		}
	}
	if planCount == 0 {
		t.Errorf("expected plan actions in dry-run; got: %+v", report.Actions)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test: group idempotency — existing groups not recreated
// ─────────────────────────────────────────────────────────────────────────────

func TestSyncCIAM_GroupIdempotent_ExistingGroupNotRecreated(t *testing.T) {
	ciam := &fakeCIAMWriter{
		listOrgRoleGrants: func(_ string) ([]CIAMRoleGrant, error) { return nil, nil },
		listGroups: func(_ string) ([]CIAMGroupInfo, error) {
			return []CIAMGroupInfo{
				{ID: "existing-grp-id", Name: "security-team"},
			}, nil
		},
	}

	s := newCIAMTestSyncer(ciam, "circleci")
	m := ciamManifest()
	// Remove project grants to keep the test focused.
	m.CIAM.ProjectGroupGrants = nil
	m.CIAM.OrgRoles = nil

	report, err := s.SyncCIAM(m, nil, Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ciam.groupsCreated) > 0 {
		t.Errorf("expected no groups created (already exists); got %v", ciam.groupsCreated)
	}

	// Should have an "exists" action for the group.
	hasExists := false
	for _, a := range report.Actions {
		if a.Status == "exists" {
			hasExists = true
		}
	}
	if !hasExists {
		t.Errorf("expected 'exists' action for existing group; got: %+v", report.Actions)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test: email-matched users get roles; unmatched emit manual
// ─────────────────────────────────────────────────────────────────────────────

func TestSyncCIAM_EmailMatchedUsersGetRoles_UnmatchedEmitManual(t *testing.T) {
	ciam := &fakeCIAMWriter{
		listOrgRoleGrants: func(_ string) ([]CIAMRoleGrant, error) {
			// Only alice is in the dest org; unmatched@example.com is not.
			return []CIAMRoleGrant{
				{UserID: "uid-alice", Email: "alice@example.com"},
			}, nil
		},
		listGroups: func(_ string) ([]CIAMGroupInfo, error) { return nil, nil },
	}

	s := newCIAMTestSyncer(ciam, "circleci")
	m := ciamManifest()
	// Remove project/group grants for focused test.
	m.CIAM.Groups = nil
	m.CIAM.ProjectUserGrants = nil
	m.CIAM.ProjectGroupGrants = nil

	report, err := s.SyncCIAM(m, nil, Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var setActions, manualActions []Action
	for _, a := range report.Actions {
		switch a.Status {
		case "set":
			setActions = append(setActions, a)
		case "manual":
			manualActions = append(manualActions, a)
		}
	}

	if len(setActions) != 1 {
		t.Errorf("expected 1 'set' action for alice; got %d: %+v", len(setActions), setActions)
	}
	if len(manualActions) != 1 {
		t.Errorf("expected 1 'manual' action for unmatched user; got %d: %+v", len(manualActions), manualActions)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test: project user grants applied for matched users
// ─────────────────────────────────────────────────────────────────────────────

func TestSyncCIAM_ProjectUserGrantsApplied(t *testing.T) {
	ciam := &fakeCIAMWriter{
		listOrgRoleGrants: func(_ string) ([]CIAMRoleGrant, error) {
			return []CIAMRoleGrant{
				{UserID: "uid-alice", Email: "alice@example.com"},
			}, nil
		},
		listGroups: func(_ string) ([]CIAMGroupInfo, error) { return nil, nil },
	}

	s := newCIAMTestSyncer(ciam, "circleci")
	m := ciamManifest()
	m.CIAM.Groups = nil
	m.CIAM.OrgRoles = nil
	m.CIAM.ProjectGroupGrants = nil

	_, err := s.SyncCIAM(m, nil, Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(ciam.projectRolesSet) != 1 {
		t.Fatalf("expected 1 project role set; got %d: %v", len(ciam.projectRolesSet), ciam.projectRolesSet)
	}
	// The project ID used is the source SourceID (same-org migration).
	want := "dest-org-uuid/proj-uuid-1/uid-alice/project-admin"
	if ciam.projectRolesSet[0] != want {
		t.Errorf("got %q, want %q", ciam.projectRolesSet[0], want)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test: nil CIAMWriter → no-op
// ─────────────────────────────────────────────────────────────────────────────

func TestSyncCIAM_NilCIAMWriter_NoOp(t *testing.T) {
	s := &Syncer{
		Org: &fakeOrgResolver{
			resolveOrgID: func(_ string) (string, error) { return "dest-id", nil },
			getOrganization: func(slug string) (*org.Organization, error) {
				return &org.Organization{Slug: slug, VCSType: "circleci"}, nil
			},
		},
		CIAM: nil,
	}
	m := ciamManifest()
	report, err := s.SyncCIAM(m, nil, Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(report.Actions) != 0 {
		t.Errorf("expected no actions when CIAMWriter is nil; got %+v", report.Actions)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test: group creation error recorded, execution continues
// ─────────────────────────────────────────────────────────────────────────────

func TestSyncCIAM_GroupCreateError_RecordsErrorContinues(t *testing.T) {
	ciam := &fakeCIAMWriter{
		listOrgRoleGrants: func(_ string) ([]CIAMRoleGrant, error) { return nil, nil },
		listGroups:        func(_ string) ([]CIAMGroupInfo, error) { return nil, nil },
		createGroup: func(_, _, _ string) (string, error) {
			return "", fmt.Errorf("server error creating group")
		},
	}

	s := newCIAMTestSyncer(ciam, "circleci")
	m := ciamManifest()
	m.CIAM.OrgRoles = nil
	m.CIAM.ProjectGroupGrants = nil
	m.CIAM.ProjectUserGrants = nil

	report, err := s.SyncCIAM(m, nil, Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected fatal error: %v", err)
	}

	var errorActions []Action
	for _, a := range report.Actions {
		if a.Status == "error" {
			errorActions = append(errorActions, a)
		}
	}
	if len(errorActions) == 0 {
		t.Errorf("expected error action for group creation failure; got: %+v", report.Actions)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test: buildEmailToUserIDMap error — empty map returned, execution continues
// ─────────────────────────────────────────────────────────────────────────────

func TestSyncCIAM_ListOrgRoleGrantsError_ErrorRecorded(t *testing.T) {
	ciam := &fakeCIAMWriter{
		listOrgRoleGrants: func(_ string) ([]CIAMRoleGrant, error) {
			return nil, fmt.Errorf("network error")
		},
		listGroups: func(_ string) ([]CIAMGroupInfo, error) { return nil, nil },
	}

	s := newCIAMTestSyncer(ciam, "circleci")
	m := ciamManifest()

	report, err := s.SyncCIAM(m, nil, Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected fatal error: %v", err)
	}

	var errorActions []Action
	for _, a := range report.Actions {
		if a.Status == "error" {
			errorActions = append(errorActions, a)
		}
	}
	if len(errorActions) == 0 {
		t.Errorf("expected error action from ListOrgRoleGrants failure; got: %+v", report.Actions)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test: project group grants applied when group exists in destination
// ─────────────────────────────────────────────────────────────────────────────

func TestSyncCIAM_ProjectGroupGrantsApplied(t *testing.T) {
	// The group must be present in m.CIAM.Groups so syncCIAMGroups builds the
	// nameToID map — the listGroups stub returns it as an existing group so no
	// CreateGroup is called. The resulting nameToID map is then used to resolve
	// the project group grant.
	ciam := &fakeCIAMWriter{
		listOrgRoleGrants: func(_ string) ([]CIAMRoleGrant, error) { return nil, nil },
		listGroups: func(_ string) ([]CIAMGroupInfo, error) {
			return []CIAMGroupInfo{{ID: "grp-security-id", Name: "security-team"}}, nil
		},
	}

	s := newCIAMTestSyncer(ciam, "circleci")
	m := ciamManifest()
	m.CIAM.OrgRoles = nil
	m.CIAM.ProjectUserGrants = nil
	// Keep Groups with "security-team" so syncCIAMGroups records it as "exists"
	// and includes it in groupNameToID.

	_, err := s.SyncCIAM(m, nil, Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(ciam.projectGroupRoles) != 1 {
		t.Fatalf("expected 1 project group role set; got %d: %v", len(ciam.projectGroupRoles), ciam.projectGroupRoles)
	}
	// "dest-org-uuid/proj-uuid-1/grp-security-id/project-contributor"
	want := "dest-org-uuid/proj-uuid-1/grp-security-id/project-contributor"
	if ciam.projectGroupRoles[0] != want {
		t.Errorf("got %q, want %q", ciam.projectGroupRoles[0], want)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test: project user grant — project not found in dest, emits manual
// ─────────────────────────────────────────────────────────────────────────────

func TestSyncCIAM_ProjectUserGrant_ProjectNotFound_EmitsManual(t *testing.T) {
	ciam := &fakeCIAMWriter{
		listOrgRoleGrants: func(_ string) ([]CIAMRoleGrant, error) {
			return []CIAMRoleGrant{{UserID: "uid-alice", Email: "alice@example.com"}}, nil
		},
		listGroups: func(_ string) ([]CIAMGroupInfo, error) { return nil, nil },
	}

	s := newCIAMTestSyncer(ciam, "circleci")
	m := ciamManifest()
	m.CIAM.OrgRoles = nil
	m.CIAM.Groups = nil
	m.CIAM.ProjectGroupGrants = nil
	// Strip the project SourceID so there's no project UUID in the map.
	m.Projects = []manifest.Project{
		{Slug: "circleci/src-org-uuid/proj-uuid-1", Name: "my-project", SourceID: ""},
	}

	report, err := s.SyncCIAM(m, nil, Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var manualActions []Action
	for _, a := range report.Actions {
		if a.Status == "manual" {
			manualActions = append(manualActions, a)
		}
	}
	if len(manualActions) == 0 {
		t.Errorf("expected manual action for missing project; got: %+v", report.Actions)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test: project group grant — group not found in dest, emits manual
// ─────────────────────────────────────────────────────────────────────────────

func TestSyncCIAM_ProjectGroupGrant_GroupNotFound_EmitsManual(t *testing.T) {
	ciam := &fakeCIAMWriter{
		listOrgRoleGrants: func(_ string) ([]CIAMRoleGrant, error) { return nil, nil },
		listGroups:        func(_ string) ([]CIAMGroupInfo, error) { return nil, nil },
	}

	s := newCIAMTestSyncer(ciam, "circleci")
	m := ciamManifest()
	m.CIAM.OrgRoles = nil
	m.CIAM.Groups = nil // no groups in dest, no creates planned
	m.CIAM.ProjectUserGrants = nil

	report, err := s.SyncCIAM(m, nil, Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var manualActions []Action
	for _, a := range report.Actions {
		if a.Status == "manual" {
			manualActions = append(manualActions, a)
		}
	}
	if len(manualActions) == 0 {
		t.Errorf("expected manual action for missing group; got: %+v", report.Actions)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test: listGroups error — groups section records error and returns empty map
// ─────────────────────────────────────────────────────────────────────────────

func TestSyncCIAM_ListGroupsError_RecordsError(t *testing.T) {
	ciam := &fakeCIAMWriter{
		listOrgRoleGrants: func(_ string) ([]CIAMRoleGrant, error) { return nil, nil },
		listGroups: func(_ string) ([]CIAMGroupInfo, error) {
			return nil, fmt.Errorf("groups API error")
		},
	}

	s := newCIAMTestSyncer(ciam, "circleci")
	m := ciamManifest()
	m.CIAM.OrgRoles = nil
	m.CIAM.ProjectUserGrants = nil

	report, err := s.SyncCIAM(m, nil, Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected fatal error: %v", err)
	}

	var errorActions []Action
	for _, a := range report.Actions {
		if a.Status == "error" {
			errorActions = append(errorActions, a)
		}
	}
	if len(errorActions) == 0 {
		t.Errorf("expected error action from ListGroups failure; got: %+v", report.Actions)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test: AddUsersToGroup error — error action recorded, continues
// ─────────────────────────────────────────────────────────────────────────────

func TestSyncCIAM_AddUsersToGroupError_RecordsError(t *testing.T) {
	ciam := &fakeCIAMWriter{
		listOrgRoleGrants: func(_ string) ([]CIAMRoleGrant, error) {
			return []CIAMRoleGrant{
				{UserID: "uid-alice", Email: "alice@example.com"},
			}, nil
		},
		listGroups: func(_ string) ([]CIAMGroupInfo, error) { return nil, nil },
		addUsersToGroup: func(_, _ string, _ []string) error {
			return fmt.Errorf("members API error")
		},
	}

	s := newCIAMTestSyncer(ciam, "circleci")
	m := ciamManifest()
	m.CIAM.OrgRoles = nil
	m.CIAM.ProjectUserGrants = nil
	m.CIAM.ProjectGroupGrants = nil
	m.CIAM.Groups = []manifest.CIAMGroup{
		{
			Name:         "security-team",
			MemberEmails: []string{"alice@example.com"},
		},
	}

	report, err := s.SyncCIAM(m, nil, Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected fatal error: %v", err)
	}

	var errorActions []Action
	for _, a := range report.Actions {
		if a.Status == "error" {
			errorActions = append(errorActions, a)
		}
	}
	if len(errorActions) == 0 {
		t.Errorf("expected error action from AddUsersToGroup failure; got: %+v", report.Actions)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test: SetProjectUserRole error — error action recorded, continues
// ─────────────────────────────────────────────────────────────────────────────

func TestSyncCIAM_SetProjectUserRoleError_RecordsError(t *testing.T) {
	ciam := &fakeCIAMWriter{
		listOrgRoleGrants: func(_ string) ([]CIAMRoleGrant, error) {
			return []CIAMRoleGrant{
				{UserID: "uid-alice", Email: "alice@example.com"},
			}, nil
		},
		listGroups: func(_ string) ([]CIAMGroupInfo, error) { return nil, nil },
		setProjectUserRole: func(_, _, _, _ string) error {
			return fmt.Errorf("project role API error")
		},
	}

	s := newCIAMTestSyncer(ciam, "circleci")
	m := ciamManifest()
	m.CIAM.OrgRoles = nil
	m.CIAM.Groups = nil
	m.CIAM.ProjectGroupGrants = nil

	report, err := s.SyncCIAM(m, nil, Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected fatal error: %v", err)
	}

	var errorActions []Action
	for _, a := range report.Actions {
		if a.Status == "error" {
			errorActions = append(errorActions, a)
		}
	}
	if len(errorActions) == 0 {
		t.Errorf("expected error action from SetProjectUserRole failure; got: %+v", report.Actions)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test: AddProjectGroupRole error — error action recorded, continues
// ─────────────────────────────────────────────────────────────────────────────

func TestSyncCIAM_AddProjectGroupRoleError_RecordsError(t *testing.T) {
	ciam := &fakeCIAMWriter{
		listOrgRoleGrants: func(_ string) ([]CIAMRoleGrant, error) { return nil, nil },
		listGroups: func(_ string) ([]CIAMGroupInfo, error) {
			return []CIAMGroupInfo{{ID: "grp-security-id", Name: "security-team"}}, nil
		},
		addProjectGroupRole: func(_, _ string, _ []string, _ string) error {
			return fmt.Errorf("group role API error")
		},
	}

	s := newCIAMTestSyncer(ciam, "circleci")
	m := ciamManifest()
	m.CIAM.OrgRoles = nil
	m.CIAM.ProjectUserGrants = nil
	// security-team must be in Groups so syncCIAMGroups adds it to nameToID map.

	report, err := s.SyncCIAM(m, nil, Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected fatal error: %v", err)
	}

	var errorActions []Action
	for _, a := range report.Actions {
		if a.Status == "error" {
			errorActions = append(errorActions, a)
		}
	}
	if len(errorActions) == 0 {
		t.Errorf("expected error action from AddProjectGroupRole failure; got: %+v", report.Actions)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test: explicit project mapping overrides source slug
// ─────────────────────────────────────────────────────────────────────────────

func TestSyncCIAM_ExplicitProjectMappingUsed(t *testing.T) {
	ciam := &fakeCIAMWriter{
		listOrgRoleGrants: func(_ string) ([]CIAMRoleGrant, error) {
			return []CIAMRoleGrant{
				{UserID: "uid-alice", Email: "alice@example.com"},
			}, nil
		},
		listGroups: func(_ string) ([]CIAMGroupInfo, error) { return nil, nil },
	}

	s := newCIAMTestSyncer(ciam, "circleci")
	m := ciamManifest()
	m.CIAM.OrgRoles = nil
	m.CIAM.Groups = nil
	m.CIAM.ProjectGroupGrants = nil

	// Provide an explicit mapping that maps the source slug to a different dest project.
	mapping := &manifest.Mapping{
		Org: manifest.OrgMapping{To: "circleci/dest-org-uuid"},
		Projects: map[string]string{
			"circleci/src-org-uuid/proj-uuid-1": "circleci/dest-org-uuid/mapped-proj-uuid",
		},
	}

	_, err := s.SyncCIAM(m, mapping, Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The project role should use the mapped project ID, not the SourceID.
	if len(ciam.projectRolesSet) != 1 {
		t.Fatalf("expected 1 project role set; got %d", len(ciam.projectRolesSet))
	}
	want := "dest-org-uuid/circleci/dest-org-uuid/mapped-proj-uuid/uid-alice/project-admin"
	if ciam.projectRolesSet[0] != want {
		t.Errorf("got %q, want %q", ciam.projectRolesSet[0], want)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test: SetOrgUserRole error — error action recorded, continues
// ─────────────────────────────────────────────────────────────────────────────

func TestSyncCIAM_SetOrgUserRoleError_RecordsError(t *testing.T) {
	ciam := &fakeCIAMWriter{
		listOrgRoleGrants: func(_ string) ([]CIAMRoleGrant, error) {
			return []CIAMRoleGrant{
				{UserID: "uid-alice", Email: "alice@example.com"},
			}, nil
		},
		listGroups: func(_ string) ([]CIAMGroupInfo, error) { return nil, nil },
		setOrgUserRole: func(_, _, _ string) error {
			return fmt.Errorf("org role API error")
		},
	}

	s := newCIAMTestSyncer(ciam, "circleci")
	m := ciamManifest()
	m.CIAM.Groups = nil
	m.CIAM.ProjectUserGrants = nil
	m.CIAM.ProjectGroupGrants = nil
	// Keep only alice's org role
	m.CIAM.OrgRoles = []manifest.CIAMOrgRole{
		{Email: "alice@example.com", Role: "org-admin"},
	}

	report, err := s.SyncCIAM(m, nil, Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected fatal error: %v", err)
	}

	var errorActions []Action
	for _, a := range report.Actions {
		if a.Status == "error" {
			errorActions = append(errorActions, a)
		}
	}
	if len(errorActions) == 0 {
		t.Errorf("expected error action from SetOrgUserRole failure; got: %+v", report.Actions)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test: groups with members — matched/unmatched split correctly
// ─────────────────────────────────────────────────────────────────────────────

func TestSyncCIAM_GroupMembersMatchedAndUnmatched(t *testing.T) {
	ciam := &fakeCIAMWriter{
		listOrgRoleGrants: func(_ string) ([]CIAMRoleGrant, error) {
			return []CIAMRoleGrant{
				{UserID: "uid-alice", Email: "alice@example.com"},
			}, nil
		},
		listGroups: func(_ string) ([]CIAMGroupInfo, error) { return nil, nil },
	}

	s := newCIAMTestSyncer(ciam, "circleci")
	m := ciamManifest()
	m.CIAM.OrgRoles = nil
	m.CIAM.ProjectUserGrants = nil
	m.CIAM.ProjectGroupGrants = nil
	m.CIAM.Groups = []manifest.CIAMGroup{
		{
			Name:         "security-team",
			MemberEmails: []string{"alice@example.com", "notinorg@example.com"},
		},
	}

	report, err := s.SyncCIAM(m, nil, Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// alice matched → should be added; notinorg unmatched → manual.
	var manualActions []Action
	var setActions []Action
	for _, a := range report.Actions {
		if a.Status == "manual" {
			manualActions = append(manualActions, a)
		}
		if a.Status == "set" {
			setActions = append(setActions, a)
		}
	}
	if len(manualActions) == 0 {
		t.Errorf("expected manual action for unmatched member; got: %+v", report.Actions)
	}
	if len(setActions) == 0 {
		t.Errorf("expected set action for matched member; got: %+v", report.Actions)
	}
	// alice should have been added to the group.
	if len(ciam.usersAdded) == 0 {
		t.Errorf("expected AddUsersToGroup called; got empty usersAdded map")
	}
}
