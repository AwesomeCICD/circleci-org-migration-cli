// Internal (white-box) tests for unexported helpers in secrets_capture.go.
// Must use package cmd (not cmd_test) to access unexported symbols.
package cmd

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	apicontext "github.com/AwesomeCICD/circleci-org-migration-cli/api/context"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
	"github.com/spf13/cobra"
)

// ─────────────────────────────────────────────────────────────────────────────
// parseOrgSlug
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
		vcs, orgName, ok := parseOrgSlug(tc.slug)
		if ok != tc.wantOK {
			t.Errorf("parseOrgSlug(%q) ok=%v want %v", tc.slug, ok, tc.wantOK)
			continue
		}
		if ok {
			if vcs != tc.wantVCS {
				t.Errorf("parseOrgSlug(%q) vcsType=%q want %q", tc.slug, vcs, tc.wantVCS)
			}
			if orgName != tc.wantOrgName {
				t.Errorf("parseOrgSlug(%q) orgName=%q want %q", tc.slug, orgName, tc.wantOrgName)
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
	// A single "All members" group restriction (value == orgID) → empty result.
	restrictions := []manifest.Restriction{
		{Type: "group", Value: "org-uuid-123", Name: "All members"},
	}
	result := realRestrictions(restrictions, "org-uuid-123")
	if len(result) != 0 {
		t.Errorf("expected empty after filtering All-members restriction, got %v", result)
	}
}

func TestRealRestrictions_NonAllMembersGroupKept(t *testing.T) {
	// A group restriction with value != orgID is a real restriction → kept.
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
	// Mix: one All-members group + one real team group + one project restriction.
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
	// Project and expression restrictions are always real.
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
	// When orgID is empty, no group restriction is treated as All-members.
	restrictions := []manifest.Restriction{
		{Type: "group", Value: "", Name: "All members"},
		{Type: "group", Value: "team-uuid-456", Name: "engineering"},
	}
	result := realRestrictions(restrictions, "")
	// With empty orgID, only group with value=="" matches → filtered out.
	if len(result) != 1 || result[0].Value != "team-uuid-456" {
		t.Errorf("expected 1 non-empty-value restriction, got %v", result)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// maybeEnableOrgTriggerFlag
// ─────────────────────────────────────────────────────────────────────────────

// fakeOrgFlagManager is a test double for orgFlagManager.
type fakeOrgFlagManager struct {
	flags       map[string]bool
	updateCalls []map[string]bool
	getErr      error
	updateErr   error
}

func (f *fakeOrgFlagManager) GetFeatureFlags(vcsType, orgName string) (map[string]bool, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	out := make(map[string]bool, len(f.flags))
	for k, v := range f.flags {
		out[k] = v
	}
	return out, nil
}

func (f *fakeOrgFlagManager) UpdateFeatureFlags(vcsType, orgName string, flags map[string]bool) error {
	if f.updateErr != nil {
		return f.updateErr
	}
	f.updateCalls = append(f.updateCalls, flags)
	// Apply updates to internal state so subsequent gets see the change.
	for k, v := range flags {
		f.flags[k] = v
	}
	return nil
}

// newTestCobraCmd returns a minimal cobra.Command backed by buffer writers.
func newTestCobraCmd() (*cobra.Command, *bytes.Buffer) {
	errBuf := &bytes.Buffer{}
	c := &cobra.Command{Use: "test"}
	c.SetErr(errBuf)
	return c, errBuf
}

func TestMaybeEnableOrgTriggerFlag_AlreadyEnabled_NoOp(t *testing.T) {
	mgr := &fakeOrgFlagManager{flags: map[string]bool{orgApiTriggerKey: true}}
	cmd, _ := newTestCobraCmd()

	restore := maybeEnableOrgTriggerFlag(cmd, mgr, "github", "myorg")
	restore()

	// No UpdateFeatureFlags calls because the flag was already on.
	if len(mgr.updateCalls) != 0 {
		t.Errorf("expected 0 update calls (flag was already enabled), got %d: %v", len(mgr.updateCalls), mgr.updateCalls)
	}
}

func TestMaybeEnableOrgTriggerFlag_WasOff_EnablesAndRestores(t *testing.T) {
	mgr := &fakeOrgFlagManager{flags: map[string]bool{orgApiTriggerKey: false}}
	cmd, _ := newTestCobraCmd()

	restore := maybeEnableOrgTriggerFlag(cmd, mgr, "github", "myorg")

	// After the call, the flag should be enabled.
	if len(mgr.updateCalls) != 1 {
		t.Fatalf("expected 1 update call (enable), got %d", len(mgr.updateCalls))
	}
	if !mgr.updateCalls[0][orgApiTriggerKey] {
		t.Error("first update call should enable the flag (true)")
	}

	// Calling restore should disable it again.
	restore()
	if len(mgr.updateCalls) != 2 {
		t.Fatalf("expected 2 update calls (enable + restore), got %d", len(mgr.updateCalls))
	}
	if mgr.updateCalls[1][orgApiTriggerKey] {
		t.Error("restore call should set the flag to false")
	}
}

func TestMaybeEnableOrgTriggerFlag_GetError_WarnsAndNoOp(t *testing.T) {
	mgr := &fakeOrgFlagManager{getErr: fmt.Errorf("network timeout")}
	cmd, errBuf := newTestCobraCmd()

	restore := maybeEnableOrgTriggerFlag(cmd, mgr, "github", "myorg")
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
		flags:     map[string]bool{orgApiTriggerKey: false},
		updateErr: fmt.Errorf("permission denied"),
	}
	cmd, errBuf := newTestCobraCmd()

	restore := maybeEnableOrgTriggerFlag(cmd, mgr, "github", "myorg")
	restore() // should be a no-op (enable failed, nothing to restore)

	if !bytes.Contains(errBuf.Bytes(), []byte("WARNING")) {
		t.Errorf("expected WARNING in stderr, got %q", errBuf.String())
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Bug 6 — orgTriggerAlreadyEnabled handles both key shapes
// ─────────────────────────────────────────────────────────────────────────────

func TestOrgTriggerAlreadyEnabled_OAuthKeyTrue(t *testing.T) {
	// Standard OAuth key shape.
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
	// Standalone / GitHub-App org key shape.
	flags := map[string]bool{"allow_api_trigger_with_config_enabled": true}
	if !orgTriggerAlreadyEnabled(flags) {
		t.Error("expected true for standalone key shape")
	}
}

func TestOrgTriggerAlreadyEnabled_TrailingQuestionMark(t *testing.T) {
	// Some API responses append a "?" — must be stripped and still match.
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
	// When the standalone key shape is present and true, no update should happen.
	mgr := &fakeOrgFlagManager{
		flags: map[string]bool{"allow_api_trigger_with_config_enabled": true},
	}
	cmd, _ := newTestCobraCmd()

	restore := maybeEnableOrgTriggerFlag(cmd, mgr, "circleci", "some-uuid")
	restore()

	if len(mgr.updateCalls) != 0 {
		t.Errorf("expected 0 update calls when standalone key is already enabled, got %d: %v",
			len(mgr.updateCalls), mgr.updateCalls)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// fakeRestrictionManager — test double for contextRestrictionManager
// ─────────────────────────────────────────────────────────────────────────────

type fakeRestrictionManager struct {
	// liveRestrictions is returned by ListRestrictions.
	liveRestrictions []apicontext.Restriction
	// deletedIDs records restriction IDs passed to DeleteRestriction.
	deletedIDs []string
	// createdRestrictions records (type, value) pairs passed to CreateRestriction.
	createdRestrictions []struct{ rType, rValue string }

	listErr   error
	deleteErr error
	createErr error
}

func (f *fakeRestrictionManager) ListRestrictions(_ string) ([]apicontext.Restriction, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.liveRestrictions, nil
}

func (f *fakeRestrictionManager) DeleteRestriction(_, restrictionID string) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	f.deletedIDs = append(f.deletedIDs, restrictionID)
	return nil
}

func (f *fakeRestrictionManager) CreateRestriction(_, rType, rValue string) error {
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

	cmd, errBuf := newTestCobraCmd()

	restore, err := prepareRestrictionRemoval(cmd, mgr, mc, "some-org-uuid")
	if err != nil {
		t.Fatalf("unexpected error from prepareRestrictionRemoval: %v", err)
	}

	// Both live restrictions should have been deleted.
	if len(mgr.deletedIDs) != 2 {
		t.Errorf("expected 2 DELETE calls, got %d: %v", len(mgr.deletedIDs), mgr.deletedIDs)
	}
	wantDeleted := map[string]bool{"live-r-1": true, "live-r-2": true}
	for _, id := range mgr.deletedIDs {
		if !wantDeleted[id] {
			t.Errorf("unexpected deleted ID %q", id)
		}
	}

	// Notice should have been printed.
	if !bytes.Contains(errBuf.Bytes(), []byte("NOTICE")) {
		t.Errorf("expected NOTICE in stderr, got %q", errBuf.String())
	}

	// No creates yet.
	if len(mgr.createdRestrictions) != 0 {
		t.Errorf("expected 0 CREATE calls before restore, got %d", len(mgr.createdRestrictions))
	}

	// Call restore — should re-create from the manifest state.
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
	cmd, _ := newTestCobraCmd()

	_, err := prepareRestrictionRemoval(cmd, mgr, mc, "org-uuid")
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
	cmd, _ := newTestCobraCmd()

	_, err := prepareRestrictionRemoval(cmd, mgr, mc, "org-uuid")
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

	cmd, errBuf := newTestCobraCmd()

	restore, err := prepareRestrictionRemoval(cmd, mgr, mc, "org-uuid")
	if err != nil {
		t.Fatalf("unexpected setup error: %v", err)
	}

	// Inject create error before calling restore.
	mgr.createErr = fmt.Errorf("create failed")
	restore()

	// A WARNING must be printed naming the restriction to re-add manually.
	out := errBuf.String()
	if !bytes.Contains(errBuf.Bytes(), []byte("WARNING")) {
		t.Errorf("expected WARNING in stderr after restore failure, got %q", out)
	}
	if !bytes.Contains(errBuf.Bytes(), []byte("proj-uuid")) {
		t.Errorf("WARNING should name the restriction value, got %q", out)
	}
}

func TestPrepareRestrictionRemoval_UsesManifestStateForRestore(t *testing.T) {
	// Live restrictions have different IDs from manifest, but restore must use
	// the manifest's type+value pairs (not the live IDs).
	mgr := &fakeRestrictionManager{
		liveRestrictions: []apicontext.Restriction{
			{ID: "live-id-A", Type: "project", Value: "proj-X"},
		},
	}
	mc := &manifest.Context{
		Name:     "ctx",
		SourceID: "ctx-uuid",
		Restrictions: []manifest.Restriction{
			// Manifest records the same logical restriction.
			{Type: "project", Value: "proj-X", Name: "my-project"},
		},
	}

	cmd, _ := newTestCobraCmd()
	restore, err := prepareRestrictionRemoval(cmd, mgr, mc, "some-other-org-uuid")
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

// ─────────────────────────────────────────────────────────────────────────────
// Fix 3: prepareRestrictionRemoval must not touch the default "All members"
// group restriction (type=="group", value==orgID).
// ─────────────────────────────────────────────────────────────────────────────

// TestPrepareRestrictionRemoval_DefaultGroupNotDeleted verifies that when the
// live restrictions include the default "All members" group (type=="group",
// value==orgID), it is NOT deleted and NOT re-created in the restore.
func TestPrepareRestrictionRemoval_DefaultGroupNotDeleted(t *testing.T) {
	const orgID = "acme-org-uuid"

	mgr := &fakeRestrictionManager{
		liveRestrictions: []apicontext.Restriction{
			// Default "All members" group — must be left untouched.
			{ID: "default-group-id", Type: "group", Value: orgID},
			// A real project restriction — must be deleted and restored.
			{ID: "proj-restr-id", Type: "project", Value: "proj-uuid-X"},
		},
	}

	mc := &manifest.Context{
		Name:     "my-ctx",
		SourceID: "ctx-uuid",
		Restrictions: []manifest.Restriction{
			{Type: "group", Value: orgID, Name: "All members"}, // default group in manifest
			{Type: "project", Value: "proj-uuid-X", Name: "web"},
		},
	}

	cmd, _ := newTestCobraCmd()

	restore, err := prepareRestrictionRemoval(cmd, mgr, mc, orgID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Only the project restriction should have been deleted, NOT the default group.
	if len(mgr.deletedIDs) != 1 {
		t.Fatalf("expected exactly 1 DELETE (project restr only), got %d: %v", len(mgr.deletedIDs), mgr.deletedIDs)
	}
	if mgr.deletedIDs[0] != "proj-restr-id" {
		t.Errorf("wrong restriction deleted: %q", mgr.deletedIDs[0])
	}
	if mgr.deletedIDs[0] == "default-group-id" {
		t.Error("default group restriction must NEVER be deleted")
	}

	// Call restore.
	restore()

	// Only the project restriction should be re-created, NOT the default group.
	if len(mgr.createdRestrictions) != 1 {
		t.Fatalf("expected exactly 1 CREATE (project restr only) in restore, got %d: %v",
			len(mgr.createdRestrictions), mgr.createdRestrictions)
	}
	got := mgr.createdRestrictions[0]
	if got.rType != "project" || got.rValue != "proj-uuid-X" {
		t.Errorf("restore created wrong restriction: type=%q value=%q", got.rType, got.rValue)
	}

	// Ensure the group restriction was never re-created.
	for _, c := range mgr.createdRestrictions {
		if c.rType == "group" && c.rValue == orgID {
			t.Error("default group restriction must NEVER be re-created in restore")
		}
	}
}

// TestPrepareRestrictionRemoval_OnlyDefaultGroup_NoOp verifies that when the
// ONLY live restriction is the default "All members" group, no DELETE or CREATE
// calls are made.
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

	cmd, _ := newTestCobraCmd()

	restore, err := prepareRestrictionRemoval(cmd, mgr, mc, orgID)
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
// readFirstKeyLine / writeFile / writeSecretFile
// ─────────────────────────────────────────────────────────────────────────────

func TestReadFirstKeyLine_ReturnsFirstNonComment(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keys.txt")
	content := "# comment line\n\nssh-ed25519 AAAAC3... user@host\nage1somekey\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	got, err := readFirstKeyLine(path)
	if err != nil {
		t.Fatalf("readFirstKeyLine: %v", err)
	}
	if !strings.HasPrefix(got, "ssh-ed25519") {
		t.Errorf("expected ssh-ed25519 line, got %q", got)
	}
}

func TestReadFirstKeyLine_EmptyFile_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.txt")
	if err := os.WriteFile(path, []byte("# only comments\n"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	_, err := readFirstKeyLine(path)
	if err == nil {
		t.Fatal("expected error for empty/comments-only file, got nil")
	}
}

func TestReadFirstKeyLine_MissingFile_ReturnsError(t *testing.T) {
	_, err := readFirstKeyLine("/nonexistent/path.pub")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestWriteSecretFile_CreatesFileWith0600(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secret.txt")
	if err := writeSecretFile(path, "AGE-SECRET-KEY-1fake\n"); err != nil {
		t.Fatalf("writeSecretFile: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("expected 0600, got %o", perm)
	}
}

func TestWriteFile_CreatesFileWith0644(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "recipient.txt")
	if err := writeFile(path, "age1fakepubkey\n"); err != nil {
		t.Fatalf("writeFile: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o644 {
		t.Errorf("expected 0644, got %o", perm)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// validateStorageFlags
// ─────────────────────────────────────────────────────────────────────────────

func TestValidateStorageFlags_Artifact_OK(t *testing.T) {
	if err := validateStorageFlags(&captureEncryptOpts{storage: "artifact"}); err != nil {
		t.Errorf("unexpected error for 'artifact': %v", err)
	}
}

func TestValidateStorageFlags_Empty_OK(t *testing.T) {
	if err := validateStorageFlags(&captureEncryptOpts{storage: ""}); err != nil {
		t.Errorf("unexpected error for empty storage: %v", err)
	}
}

func TestValidateStorageFlags_S3_WithBucket_OK(t *testing.T) {
	if err := validateStorageFlags(&captureEncryptOpts{storage: "s3", s3Bucket: "my-bucket"}); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateStorageFlags_S3_NoBucket_Error(t *testing.T) {
	err := validateStorageFlags(&captureEncryptOpts{storage: "s3"})
	if err == nil {
		t.Fatal("expected error for s3 without bucket, got nil")
	}
}

func TestValidateStorageFlags_Both_WithBucket_OK(t *testing.T) {
	if err := validateStorageFlags(&captureEncryptOpts{storage: "both", s3Bucket: "my-bucket"}); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateStorageFlags_Unknown_Error(t *testing.T) {
	err := validateStorageFlags(&captureEncryptOpts{storage: "invalid"})
	if err == nil {
		t.Fatal("expected error for unknown storage value, got nil")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Issue #74: isGroupRestriction / isDefaultAllMembersGroup — org-type helpers
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

// ─────────────────────────────────────────────────────────────────────────────
// Issue #74: prepareRestrictionRemoval must NOT touch group restrictions
// (including non-default ones) because group restrictions are only supported
// on GitHub OAuth orgs and cannot be recreated via API on standalone/Bitbucket.
// ─────────────────────────────────────────────────────────────────────────────

// TestPrepareRestrictionRemoval_NonDefaultGroupNotTouched verifies that a
// non-default group restriction (type=="group", value!=orgID) is NEVER deleted
// or recreated by prepareRestrictionRemoval, even though it is a real
// restriction returned by realRestrictions.
func TestPrepareRestrictionRemoval_NonDefaultGroupNotTouched(t *testing.T) {
	const orgID = "acme-org-uuid"
	const teamUUID = "engineering-team-uuid"

	mgr := &fakeRestrictionManager{
		liveRestrictions: []apicontext.Restriction{
			// A real non-default group restriction (type=group, value=teamUUID != orgID).
			{ID: "group-restr-id", Type: "group", Value: teamUUID, Name: "engineering"},
			// A project restriction that SHOULD be touched.
			{ID: "proj-restr-id", Type: "project", Value: "proj-uuid-X"},
		},
	}

	mc := &manifest.Context{
		Name:     "secured-ctx",
		SourceID: "ctx-uuid",
		Restrictions: []manifest.Restriction{
			{Type: "group", Value: teamUUID, Name: "engineering"}, // non-default group
			{Type: "project", Value: "proj-uuid-X", Name: "web"},
		},
	}

	cmd, errBuf := newTestCobraCmd()

	restore, err := prepareRestrictionRemoval(cmd, mgr, mc, orgID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Only the project restriction should have been deleted.
	if len(mgr.deletedIDs) != 1 {
		t.Fatalf("expected exactly 1 DELETE (project only), got %d: %v", len(mgr.deletedIDs), mgr.deletedIDs)
	}
	if mgr.deletedIDs[0] != "proj-restr-id" {
		t.Errorf("wrong restriction deleted: got %q, want proj-restr-id", mgr.deletedIDs[0])
	}

	// A NOTICE about the group restriction being unmodified should appear.
	if !bytes.Contains(errBuf.Bytes(), []byte("group restriction")) {
		t.Errorf("expected NOTICE about group restriction being unmodified; stderr: %s", errBuf.String())
	}

	restore()

	// Only the project restriction should have been re-created.
	if len(mgr.createdRestrictions) != 1 {
		t.Fatalf("expected exactly 1 CREATE (project only) in restore, got %d: %v",
			len(mgr.createdRestrictions), mgr.createdRestrictions)
	}
	got := mgr.createdRestrictions[0]
	if got.rType != "project" || got.rValue != "proj-uuid-X" {
		t.Errorf("restore created wrong restriction: type=%q value=%q", got.rType, got.rValue)
	}

	// The group restriction must never have been re-created.
	for _, c := range mgr.createdRestrictions {
		if c.rType == "group" {
			t.Errorf("group restriction must NEVER be created by restore: type=%q value=%q", c.rType, c.rValue)
		}
	}
}
