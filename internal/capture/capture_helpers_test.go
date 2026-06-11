package capture

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	apicontext "github.com/AwesomeCICD/circleci-org-migration-cli/api/context"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
)

// ─────────────────────────────────────────────────────────────────────────────
// ParseOrgSlug
// ─────────────────────────────────────────────────────────────────────────────

func TestParseOrgSlug(t *testing.T) {
	cases := []struct {
		slug        string
		wantVCS     string
		wantOrgName string
		wantOK      bool
	}{
		{"gh/myorg", "github", "myorg", true},
		{"github/myorg", "github", "myorg", true},
		{"bb/myorg", "bitbucket", "myorg", true},
		{"bitbucket/myorg", "bitbucket", "myorg", true},
		{"circleci/some-uuid", "circleci", "some-uuid", true},
		{"other/myorg", "other", "myorg", true},
		{"", "", "", false},
		{"noprefix", "", "", false},
		{"/noname", "", "", false},
		{"prefix/", "", "", false},
	}
	for _, tc := range cases {
		vcs, orgName, ok := ParseOrgSlug(tc.slug)
		if ok != tc.wantOK {
			t.Errorf("ParseOrgSlug(%q) ok=%v want %v", tc.slug, ok, tc.wantOK)
			continue
		}
		if ok {
			if vcs != tc.wantVCS {
				t.Errorf("ParseOrgSlug(%q) vcsType=%q want %q", tc.slug, vcs, tc.wantVCS)
			}
			if orgName != tc.wantOrgName {
				t.Errorf("ParseOrgSlug(%q) orgName=%q want %q", tc.slug, orgName, tc.wantOrgName)
			}
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// realRestrictions
// ─────────────────────────────────────────────────────────────────────────────

func TestRealRestrictions_Empty(t *testing.T) {
	result := realRestrictions(nil, "org-uuid-123")
	if len(result) != 0 {
		t.Errorf("expected empty, got %v", result)
	}
}

func TestRealRestrictions_AllMembersOnly(t *testing.T) {
	restrictions := []manifest.Restriction{
		{Type: "group", Value: "org-uuid-123", Name: "All members"},
	}
	result := realRestrictions(restrictions, "org-uuid-123")
	if len(result) != 0 {
		t.Errorf("expected empty after filtering All-members restriction, got %v", result)
	}
}

func TestRealRestrictions_NonAllMembersGroupKept(t *testing.T) {
	restrictions := []manifest.Restriction{
		{Type: "group", Value: "team-uuid-456", Name: "engineering"},
	}
	result := realRestrictions(restrictions, "org-uuid-123")
	if len(result) != 1 {
		t.Fatalf("expected 1 restriction, got %d: %v", len(result), result)
	}
	if result[0].Value != "team-uuid-456" {
		t.Errorf("unexpected restriction: %+v", result[0])
	}
}

func TestRealRestrictions_MixedFiltersAllMembersOnly(t *testing.T) {
	restrictions := []manifest.Restriction{
		{Type: "group", Value: "org-uuid-123", Name: "All members"},
		{Type: "group", Value: "team-uuid-456", Name: "engineering"},
		{Type: "project", Value: "proj-uuid-789", Name: "web"},
	}
	result := realRestrictions(restrictions, "org-uuid-123")
	if len(result) != 2 {
		t.Fatalf("expected 2 real restrictions, got %d: %v", len(result), result)
	}
	for _, r := range result {
		if r.Type == "group" && r.Value == "org-uuid-123" {
			t.Error("All-members restriction should have been filtered out")
		}
	}
}

func TestRealRestrictions_ProjectAndExpressionKept(t *testing.T) {
	restrictions := []manifest.Restriction{
		{Type: "project", Value: "proj-uuid-1"},
		{Type: "expression", Value: `project.slug == "gh/acme/web"`},
	}
	result := realRestrictions(restrictions, "org-uuid-123")
	if len(result) != 2 {
		t.Errorf("expected 2 restrictions, got %d", len(result))
	}
}

func TestRealRestrictions_OrgIDEmpty(t *testing.T) {
	restrictions := []manifest.Restriction{
		{Type: "group", Value: "", Name: "All members"},
		{Type: "group", Value: "team-uuid-456", Name: "engineering"},
	}
	result := realRestrictions(restrictions, "")
	if len(result) != 1 || result[0].Value != "team-uuid-456" {
		t.Errorf("expected 1 non-empty-value restriction, got %v", result)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// MaybeEnableOrgTriggerFlag
// ─────────────────────────────────────────────────────────────────────────────

// fakeOrgFlagManager is a test double for OrgFlagManager.
type fakeOrgFlagManager struct {
	flags       map[string]bool
	updateCalls []map[string]bool
	getErr      error
	updateErr   error
}

func (f *fakeOrgFlagManager) GetFeatureFlags(_ context.Context, _, _ string) (map[string]bool, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	out := make(map[string]bool, len(f.flags))
	for k, v := range f.flags {
		out[k] = v
	}
	return out, nil
}

func (f *fakeOrgFlagManager) UpdateFeatureFlags(_ context.Context, _, _ string, flags map[string]bool) error {
	if f.updateErr != nil {
		return f.updateErr
	}
	f.updateCalls = append(f.updateCalls, flags)
	for k, v := range flags {
		f.flags[k] = v
	}
	return nil
}

func TestMaybeEnableOrgTriggerFlag_AlreadyEnabled_NoOp(t *testing.T) {
	mgr := &fakeOrgFlagManager{flags: map[string]bool{OrgAPITriggerKey: true}}
	var errBuf bytes.Buffer

	restore := MaybeEnableOrgTriggerFlag(context.Background(), &errBuf, mgr, "github", "myorg")
	restore()

	if len(mgr.updateCalls) != 0 {
		t.Errorf("expected 0 update calls (flag was already enabled), got %d: %v", len(mgr.updateCalls), mgr.updateCalls)
	}
}

func TestMaybeEnableOrgTriggerFlag_WasOff_EnablesAndRestores(t *testing.T) {
	mgr := &fakeOrgFlagManager{flags: map[string]bool{OrgAPITriggerKey: false}}
	var errBuf bytes.Buffer

	restore := MaybeEnableOrgTriggerFlag(context.Background(), &errBuf, mgr, "github", "myorg")

	if len(mgr.updateCalls) != 1 {
		t.Fatalf("expected 1 update call (enable), got %d", len(mgr.updateCalls))
	}
	if !mgr.updateCalls[0][OrgAPITriggerKey] {
		t.Error("first update call should enable the flag (true)")
	}

	restore()
	if len(mgr.updateCalls) != 2 {
		t.Fatalf("expected 2 update calls (enable + restore), got %d", len(mgr.updateCalls))
	}
	if mgr.updateCalls[1][OrgAPITriggerKey] {
		t.Error("restore call should set the flag to false")
	}
}

func TestMaybeEnableOrgTriggerFlag_GetError_WarnsAndNoOp(t *testing.T) {
	mgr := &fakeOrgFlagManager{getErr: fmt.Errorf("network timeout")}
	var errBuf bytes.Buffer

	restore := MaybeEnableOrgTriggerFlag(context.Background(), &errBuf, mgr, "github", "myorg")
	restore() // should be a no-op

	if len(mgr.updateCalls) != 0 {
		t.Errorf("expected 0 update calls on get error, got %d", len(mgr.updateCalls))
	}
	if !bytes.Contains(errBuf.Bytes(), []byte("WARNING")) {
		t.Errorf("expected WARNING in stderr, got %q", errBuf.String())
	}
}

func TestMaybeEnableOrgTriggerFlag_UpdateError_WarnsAndNoOp(t *testing.T) {
	mgr := &fakeOrgFlagManager{
		flags:     map[string]bool{OrgAPITriggerKey: false},
		updateErr: fmt.Errorf("permission denied"),
	}
	var errBuf bytes.Buffer

	restore := MaybeEnableOrgTriggerFlag(context.Background(), &errBuf, mgr, "github", "myorg")
	restore() // should be a no-op (enable failed, nothing to restore)

	if !bytes.Contains(errBuf.Bytes(), []byte("WARNING")) {
		t.Errorf("expected WARNING in stderr, got %q", errBuf.String())
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// orgTriggerAlreadyEnabled handles both key shapes
// ─────────────────────────────────────────────────────────────────────────────

func TestOrgTriggerAlreadyEnabled_OAuthKeyTrue(t *testing.T) {
	flags := map[string]bool{"allow_api_trigger_with_config": true}
	if !orgTriggerAlreadyEnabled(flags) {
		t.Error("expected true for standard OAuth key")
	}
}

func TestOrgTriggerAlreadyEnabled_OAuthKeyFalse(t *testing.T) {
	flags := map[string]bool{"allow_api_trigger_with_config": false}
	if orgTriggerAlreadyEnabled(flags) {
		t.Error("expected false when OAuth key is false")
	}
}

func TestOrgTriggerAlreadyEnabled_StandaloneKeyTrue(t *testing.T) {
	flags := map[string]bool{"allow_api_trigger_with_config_enabled": true}
	if !orgTriggerAlreadyEnabled(flags) {
		t.Error("expected true for standalone key shape")
	}
}

func TestOrgTriggerAlreadyEnabled_TrailingQuestionMark(t *testing.T) {
	flags := map[string]bool{"allow_api_trigger_with_config?": true}
	if !orgTriggerAlreadyEnabled(flags) {
		t.Error("expected true for key with trailing '?'")
	}
}

func TestOrgTriggerAlreadyEnabled_StandaloneKeyWithQuestionMark(t *testing.T) {
	flags := map[string]bool{"allow_api_trigger_with_config_enabled?": true}
	if !orgTriggerAlreadyEnabled(flags) {
		t.Error("expected true for standalone key with trailing '?'")
	}
}

func TestOrgTriggerAlreadyEnabled_EmptyFlags(t *testing.T) {
	if orgTriggerAlreadyEnabled(map[string]bool{}) {
		t.Error("expected false for empty flags map")
	}
}

func TestOrgTriggerAlreadyEnabled_UnrelatedKeys(t *testing.T) {
	flags := map[string]bool{"some_other_flag": true}
	if orgTriggerAlreadyEnabled(flags) {
		t.Error("expected false for unrelated keys")
	}
}

func TestMaybeEnableOrgTriggerFlag_StandaloneKeyAlreadyOn_NoUpdate(t *testing.T) {
	mgr := &fakeOrgFlagManager{
		flags: map[string]bool{"allow_api_trigger_with_config_enabled": true},
	}
	var errBuf bytes.Buffer

	restore := MaybeEnableOrgTriggerFlag(context.Background(), &errBuf, mgr, "circleci", "some-uuid")
	restore()

	if len(mgr.updateCalls) != 0 {
		t.Errorf("expected 0 update calls when standalone key is already enabled, got %d: %v",
			len(mgr.updateCalls), mgr.updateCalls)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// fakeRestrictionManager — test double for ContextRestrictionManager
// ─────────────────────────────────────────────────────────────────────────────

type fakeRestrictionManager struct {
	liveRestrictions    []apicontext.Restriction
	deletedIDs          []string
	createdRestrictions []struct{ rType, rValue string }

	listErr   error
	deleteErr error
	createErr error
}

func (f *fakeRestrictionManager) ListRestrictions(_ context.Context, _ string) ([]apicontext.Restriction, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.liveRestrictions, nil
}

func (f *fakeRestrictionManager) DeleteRestriction(_ context.Context, _, restrictionID string) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	f.deletedIDs = append(f.deletedIDs, restrictionID)
	return nil
}

func (f *fakeRestrictionManager) CreateRestriction(_ context.Context, _, rType, rValue string) error {
	if f.createErr != nil {
		return f.createErr
	}
	f.createdRestrictions = append(f.createdRestrictions, struct{ rType, rValue string }{rType, rValue})
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// prepareRestrictionRemoval
// ─────────────────────────────────────────────────────────────────────────────

func TestPrepareRestrictionRemoval_DeletesAndRestores(t *testing.T) {
	mgr := &fakeRestrictionManager{
		liveRestrictions: []apicontext.Restriction{
			{ID: "live-r-1", Type: "project", Value: "proj-uuid-1"},
			{ID: "live-r-2", Type: "expression", Value: `pipeline.git.branch == "main"`},
		},
	}

	mc := &manifest.Context{
		Name:     "my-ctx",
		SourceID: "ctx-source-uuid",
		Restrictions: []manifest.Restriction{
			{Type: "project", Value: "proj-uuid-1"},
			{Type: "expression", Value: `pipeline.git.branch == "main"`},
		},
	}

	var errBuf bytes.Buffer

	restore, err := prepareRestrictionRemoval(context.Background(), &errBuf, mgr, mc, "some-org-uuid")
	if err != nil {
		t.Fatalf("unexpected error from prepareRestrictionRemoval: %v", err)
	}

	if len(mgr.deletedIDs) != 2 {
		t.Errorf("expected 2 DELETE calls, got %d: %v", len(mgr.deletedIDs), mgr.deletedIDs)
	}
	wantDeleted := map[string]bool{"live-r-1": true, "live-r-2": true}
	for _, id := range mgr.deletedIDs {
		if !wantDeleted[id] {
			t.Errorf("unexpected deleted ID %q", id)
		}
	}

	if !bytes.Contains(errBuf.Bytes(), []byte("NOTICE")) {
		t.Errorf("expected NOTICE in stderr, got %q", errBuf.String())
	}

	if len(mgr.createdRestrictions) != 0 {
		t.Errorf("expected 0 CREATE calls before restore, got %d", len(mgr.createdRestrictions))
	}

	restore()

	if len(mgr.createdRestrictions) != 2 {
		t.Fatalf("expected 2 CREATE calls after restore, got %d: %v", len(mgr.createdRestrictions), mgr.createdRestrictions)
	}
	wantCreated := map[string]bool{"project/proj-uuid-1": true, `expression/pipeline.git.branch == "main"`: true}
	for _, c := range mgr.createdRestrictions {
		key := c.rType + "/" + c.rValue
		if !wantCreated[key] {
			t.Errorf("unexpected created restriction: type=%q value=%q", c.rType, c.rValue)
		}
	}
}

func TestPrepareRestrictionRemoval_ListError_ReturnsError(t *testing.T) {
	mgr := &fakeRestrictionManager{listErr: fmt.Errorf("network failure")}
	mc := &manifest.Context{Name: "ctx", SourceID: "ctx-uuid"}
	var errBuf bytes.Buffer

	_, err := prepareRestrictionRemoval(context.Background(), &errBuf, mgr, mc, "org-uuid")
	if err == nil {
		t.Fatal("expected error on ListRestrictions failure, got nil")
	}
	if len(mgr.deletedIDs) != 0 {
		t.Errorf("no deletes should occur if list fails, got %v", mgr.deletedIDs)
	}
}

func TestPrepareRestrictionRemoval_DeleteError_ReturnsError(t *testing.T) {
	mgr := &fakeRestrictionManager{
		liveRestrictions: []apicontext.Restriction{{ID: "r-1", Type: "project", Value: "p"}},
		deleteErr:        fmt.Errorf("forbidden"),
	}
	mc := &manifest.Context{Name: "ctx", SourceID: "ctx-uuid",
		Restrictions: []manifest.Restriction{{Type: "project", Value: "p"}},
	}
	var errBuf bytes.Buffer

	_, err := prepareRestrictionRemoval(context.Background(), &errBuf, mgr, mc, "org-uuid")
	if err == nil {
		t.Fatal("expected error on DeleteRestriction failure, got nil")
	}
}

func TestPrepareRestrictionRemoval_RestoreFailure_PrintsWarning(t *testing.T) {
	mgr := &fakeRestrictionManager{
		liveRestrictions: []apicontext.Restriction{{ID: "r-1", Type: "project", Value: "proj-uuid"}},
	}

	mc := &manifest.Context{
		Name:     "ctx",
		SourceID: "ctx-uuid",
		Restrictions: []manifest.Restriction{
			{Type: "project", Value: "proj-uuid"},
		},
	}

	var errBuf bytes.Buffer

	restore, err := prepareRestrictionRemoval(context.Background(), &errBuf, mgr, mc, "org-uuid")
	if err != nil {
		t.Fatalf("unexpected setup error: %v", err)
	}

	mgr.createErr = fmt.Errorf("create failed")
	restore()

	out := errBuf.String()
	if !bytes.Contains(errBuf.Bytes(), []byte("WARNING")) {
		t.Errorf("expected WARNING in stderr after restore failure, got %q", out)
	}
	if !bytes.Contains(errBuf.Bytes(), []byte("proj-uuid")) {
		t.Errorf("WARNING should name the restriction value, got %q", out)
	}
}

func TestPrepareRestrictionRemoval_UsesManifestStateForRestore(t *testing.T) {
	mgr := &fakeRestrictionManager{
		liveRestrictions: []apicontext.Restriction{
			{ID: "live-id-A", Type: "project", Value: "proj-X"},
		},
	}
	mc := &manifest.Context{
		Name:     "ctx",
		SourceID: "ctx-uuid",
		Restrictions: []manifest.Restriction{
			{Type: "project", Value: "proj-X", Name: "my-project"},
		},
	}

	var errBuf bytes.Buffer
	restore, err := prepareRestrictionRemoval(context.Background(), &errBuf, mgr, mc, "some-other-org-uuid")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	restore()

	if len(mgr.createdRestrictions) != 1 {
		t.Fatalf("expected 1 created restriction, got %d", len(mgr.createdRestrictions))
	}
	got := mgr.createdRestrictions[0]
	if got.rType != "project" || got.rValue != "proj-X" {
		t.Errorf("restore created wrong restriction: type=%q value=%q", got.rType, got.rValue)
	}
}

// Fix 3: prepareRestrictionRemoval must not touch the default "All members"
// group restriction (type=="group", value==orgID).

func TestPrepareRestrictionRemoval_DefaultGroupNotDeleted(t *testing.T) {
	const orgID = "acme-org-uuid"

	mgr := &fakeRestrictionManager{
		liveRestrictions: []apicontext.Restriction{
			{ID: "default-group-id", Type: "group", Value: orgID},
			{ID: "proj-restr-id", Type: "project", Value: "proj-uuid-X"},
		},
	}

	mc := &manifest.Context{
		Name:     "my-ctx",
		SourceID: "ctx-uuid",
		Restrictions: []manifest.Restriction{
			{Type: "group", Value: orgID, Name: "All members"},
			{Type: "project", Value: "proj-uuid-X", Name: "web"},
		},
	}

	var errBuf bytes.Buffer

	restore, err := prepareRestrictionRemoval(context.Background(), &errBuf, mgr, mc, orgID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(mgr.deletedIDs) != 1 {
		t.Fatalf("expected exactly 1 DELETE (project restr only), got %d: %v", len(mgr.deletedIDs), mgr.deletedIDs)
	}
	if mgr.deletedIDs[0] != "proj-restr-id" {
		t.Errorf("wrong restriction deleted: %q", mgr.deletedIDs[0])
	}

	restore()

	if len(mgr.createdRestrictions) != 1 {
		t.Fatalf("expected exactly 1 CREATE (project restr only) in restore, got %d: %v",
			len(mgr.createdRestrictions), mgr.createdRestrictions)
	}
	got := mgr.createdRestrictions[0]
	if got.rType != "project" || got.rValue != "proj-uuid-X" {
		t.Errorf("restore created wrong restriction: type=%q value=%q", got.rType, got.rValue)
	}

	for _, c := range mgr.createdRestrictions {
		if c.rType == "group" && c.rValue == orgID {
			t.Error("default group restriction must NEVER be re-created in restore")
		}
	}
}

func TestPrepareRestrictionRemoval_OnlyDefaultGroup_NoOp(t *testing.T) {
	const orgID = "acme-org-uuid"

	mgr := &fakeRestrictionManager{
		liveRestrictions: []apicontext.Restriction{
			{ID: "default-group-id", Type: "group", Value: orgID},
		},
	}

	mc := &manifest.Context{
		Name:     "all-members-ctx",
		SourceID: "ctx-uuid",
		Restrictions: []manifest.Restriction{
			{Type: "group", Value: orgID, Name: "All members"},
		},
	}

	var errBuf bytes.Buffer

	restore, err := prepareRestrictionRemoval(context.Background(), &errBuf, mgr, mc, orgID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(mgr.deletedIDs) != 0 {
		t.Errorf("expected 0 DELETEs for default-group-only context, got %d: %v",
			len(mgr.deletedIDs), mgr.deletedIDs)
	}

	restore()

	if len(mgr.createdRestrictions) != 0 {
		t.Errorf("expected 0 CREATEs (restore) for default-group-only context, got %d: %v",
			len(mgr.createdRestrictions), mgr.createdRestrictions)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// isGroupRestriction / isDefaultAllMembersGroup — org-type helpers
// ─────────────────────────────────────────────────────────────────────────────

func TestIsGroupRestriction_GroupType_True(t *testing.T) {
	r := manifest.Restriction{Type: "group", Value: "some-team-uuid"}
	if !isGroupRestriction(r) {
		t.Error("expected true for type=group")
	}
}

func TestIsGroupRestriction_ProjectType_False(t *testing.T) {
	r := manifest.Restriction{Type: "project", Value: "proj-uuid"}
	if isGroupRestriction(r) {
		t.Error("expected false for type=project")
	}
}

func TestIsGroupRestriction_ExpressionType_False(t *testing.T) {
	r := manifest.Restriction{Type: "expression", Value: `pipeline.git.branch == "main"`}
	if isGroupRestriction(r) {
		t.Error("expected false for type=expression")
	}
}

func TestIsDefaultAllMembersGroup_Matches(t *testing.T) {
	const orgID = "org-uuid-abc"
	r := manifest.Restriction{Type: "group", Value: orgID, Name: "All members"}
	if !isDefaultAllMembersGroup(r, orgID) {
		t.Error("expected true for group with value==orgID")
	}
}

func TestIsDefaultAllMembersGroup_DifferentValue_False(t *testing.T) {
	r := manifest.Restriction{Type: "group", Value: "team-uuid", Name: "engineering"}
	if isDefaultAllMembersGroup(r, "org-uuid-abc") {
		t.Error("expected false when group value != orgID")
	}
}

func TestIsDefaultAllMembersGroup_ProjectType_False(t *testing.T) {
	const orgID = "org-uuid-abc"
	r := manifest.Restriction{Type: "project", Value: orgID}
	if isDefaultAllMembersGroup(r, orgID) {
		t.Error("expected false for type=project even when value==orgID")
	}
}

// Issue #74: prepareRestrictionRemoval must NOT touch group restrictions
// (including non-default ones) because group restrictions are only supported
// on GitHub OAuth orgs and cannot be recreated via API on standalone/Bitbucket.

func TestPrepareRestrictionRemoval_NonDefaultGroupNotTouched(t *testing.T) {
	const orgID = "acme-org-uuid"
	const teamUUID = "engineering-team-uuid"

	mgr := &fakeRestrictionManager{
		liveRestrictions: []apicontext.Restriction{
			{ID: "group-restr-id", Type: "group", Value: teamUUID, Name: "engineering"},
			{ID: "proj-restr-id", Type: "project", Value: "proj-uuid-X"},
		},
	}

	mc := &manifest.Context{
		Name:     "secured-ctx",
		SourceID: "ctx-uuid",
		Restrictions: []manifest.Restriction{
			{Type: "group", Value: teamUUID, Name: "engineering"},
			{Type: "project", Value: "proj-uuid-X", Name: "web"},
		},
	}

	var errBuf bytes.Buffer

	restore, err := prepareRestrictionRemoval(context.Background(), &errBuf, mgr, mc, orgID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(mgr.deletedIDs) != 1 {
		t.Fatalf("expected exactly 1 DELETE (project only), got %d: %v", len(mgr.deletedIDs), mgr.deletedIDs)
	}
	if mgr.deletedIDs[0] != "proj-restr-id" {
		t.Errorf("wrong restriction deleted: got %q, want proj-restr-id", mgr.deletedIDs[0])
	}

	if !bytes.Contains(errBuf.Bytes(), []byte("group restriction")) {
		t.Errorf("expected NOTICE about group restriction being unmodified; stderr: %s", errBuf.String())
	}

	restore()

	if len(mgr.createdRestrictions) != 1 {
		t.Fatalf("expected exactly 1 CREATE (project only) in restore, got %d: %v",
			len(mgr.createdRestrictions), mgr.createdRestrictions)
	}
	got := mgr.createdRestrictions[0]
	if got.rType != "project" || got.rValue != "proj-uuid-X" {
		t.Errorf("restore created wrong restriction: type=%q value=%q", got.rType, got.rValue)
	}

	for _, c := range mgr.createdRestrictions {
		if c.rType == "group" {
			t.Errorf("group restriction must NEVER be created by restore: type=%q value=%q", c.rType, c.rValue)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// SelectProjects / FindProjectBySlug
// ─────────────────────────────────────────────────────────────────────────────

func TestSelectProjects_EmptySlugs_ReturnsProjectsWithValues(t *testing.T) {
	m := &manifest.Manifest{
		Projects: []manifest.Project{
			{Slug: "gh/acme/web", EnvVars: []manifest.ProjectEnvVar{{Name: "SECRET"}}},
			{Slug: "gh/acme/empty"},
			{Slug: "gh/acme/api", EnvVars: []manifest.ProjectEnvVar{{Name: "API_KEY"}}},
		},
	}
	got := SelectProjects(m, nil)
	if len(got) != 2 {
		t.Errorf("expected 2 projects with values, got %d", len(got))
	}
	for _, p := range got {
		if p.Slug == "gh/acme/empty" {
			t.Errorf("project with no env vars should NOT be included in default set")
		}
	}
}

func TestSelectProjects_EmptySlugs_AllEmpty_ReturnsNone(t *testing.T) {
	m := &manifest.Manifest{
		Projects: []manifest.Project{
			{Slug: "gh/acme/web"},
			{Slug: "gh/acme/api"},
		},
	}
	got := SelectProjects(m, nil)
	if len(got) != 0 {
		t.Errorf("expected 0 projects when all have no env vars, got %d", len(got))
	}
}

func TestSelectProjects_FiltersBySlugs(t *testing.T) {
	m := &manifest.Manifest{
		Projects: []manifest.Project{
			{Slug: "gh/acme/web"},
			{Slug: "gh/acme/api"},
			{Slug: "gh/acme/mobile"},
		},
	}
	got := SelectProjects(m, []string{"gh/acme/web", "gh/acme/mobile"})
	if len(got) != 2 {
		t.Fatalf("expected 2 projects, got %d", len(got))
	}
	slugs := map[string]bool{}
	for _, p := range got {
		slugs[p.Slug] = true
	}
	if !slugs["gh/acme/web"] || !slugs["gh/acme/mobile"] {
		t.Errorf("expected web and mobile, got: %v", slugs)
	}
}

func TestSelectProjects_UnknownSlug_Filtered(t *testing.T) {
	m := &manifest.Manifest{
		Projects: []manifest.Project{
			{Slug: "gh/acme/web"},
		},
	}
	got := SelectProjects(m, []string{"gh/acme/nonexistent"})
	if len(got) != 0 {
		t.Errorf("expected 0 projects for unknown slug, got %d", len(got))
	}
}

func TestSelectProjects_EmptyManifest(t *testing.T) {
	m := &manifest.Manifest{}
	got := SelectProjects(m, nil)
	if len(got) != 0 {
		t.Errorf("expected 0 projects, got %d", len(got))
	}
}

func TestFindProjectBySlug(t *testing.T) {
	m := &manifest.Manifest{
		Projects: []manifest.Project{
			{Slug: "gh/acme/web"},
			{Slug: "gh/acme/api"},
		},
	}
	if got := FindProjectBySlug(m, "gh/acme/api"); got == nil || got.Slug != "gh/acme/api" {
		t.Errorf("FindProjectBySlug(api) = %v, want gh/acme/api", got)
	}
	if got := FindProjectBySlug(m, "gh/acme/missing"); got != nil {
		t.Errorf("FindProjectBySlug(missing) = %v, want nil", got)
	}
}

func TestDedupe(t *testing.T) {
	got := dedupe([]string{"a", "b", "a", "c", "b"})
	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("dedupe length = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("dedupe[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
