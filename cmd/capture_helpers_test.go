// Internal (white-box) tests for unexported helpers in secrets_capture.go.
// Must use package cmd (not cmd_test) to access unexported symbols.
package cmd

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/CircleCI-Public/circleci-org-migration-cli/internal/manifest"
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
