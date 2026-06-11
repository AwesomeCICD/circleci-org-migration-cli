package syncer

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
)

// ─────────────────────────────────────────────────────────────────────────────
// syncBudgets — org-level budget
// ─────────────────────────────────────────────────────────────────────────────

func TestSyncBudgets_OrgBudget_DryRunNoWrites(t *testing.T) {
	fw := &fakeOrgSettingsWriter{}
	sy := newOrgSettingsSyncer(fw)

	m := orgSettingsManifest(&manifest.OrgSettings{
		Budgets: &manifest.OrgBudgets{
			OrgBudget: &manifest.BudgetEntry{
				Credits:         1000000,
				BudgetID:        "budget-uuid-1",
				EnforcementType: "warn",
			},
		},
	})

	rep, err := sy.SyncOrgSettings(context.Background(), m, mappingTo("gh/dest"), Options{Apply: false})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fw.hasCalled("SetBudget") {
		t.Error("SetBudget must NOT be called in dry-run mode")
	}

	setActions := actionsOfStatus(rep, "set")
	if len(setActions) == 0 {
		t.Error("expected a set action for org budget in dry-run")
	}
}

func TestSyncBudgets_OrgBudget_ApplyTrue(t *testing.T) {
	var gotOrgUUID string
	var gotProjectID *string
	var gotCredits int

	fw := &fakeOrgSettingsWriter{
		setBudget: func(orgUUID string, projectID *string, credits int) error {
			gotOrgUUID = orgUUID
			gotProjectID = projectID
			gotCredits = credits
			return nil
		},
	}
	sy := newOrgSettingsSyncer(fw)

	m := orgSettingsManifest(&manifest.OrgSettings{
		Budgets: &manifest.OrgBudgets{
			OrgBudget: &manifest.BudgetEntry{
				Credits:         2000000,
				BudgetID:        "budget-uuid-1",
				EnforcementType: "warn",
			},
		},
	})

	rep, err := sy.SyncOrgSettings(context.Background(), m, mappingTo("gh/dest"), Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fw.budgetSets != 1 {
		t.Errorf("expected 1 SetBudget call, got %d", fw.budgetSets)
	}
	if gotOrgUUID == "" {
		t.Error("orgUUID must be non-empty")
	}
	if gotProjectID != nil {
		t.Errorf("projectID must be nil for org-level budget, got %v", gotProjectID)
	}
	if gotCredits != 2000000 {
		t.Errorf("credits: got %d want 2000000", gotCredits)
	}

	setActions := actionsOfStatus(rep, "set")
	if len(setActions) == 0 {
		t.Error("expected set action for org budget")
	}
}

func TestSyncBudgets_OrgBudget_WriteError_IsErrorAction(t *testing.T) {
	fw := &fakeOrgSettingsWriter{
		setBudget: func(orgUUID string, projectID *string, credits int) error {
			return errors.New("budget write failed")
		},
	}
	sy := newOrgSettingsSyncer(fw)

	m := orgSettingsManifest(&manifest.OrgSettings{
		Budgets: &manifest.OrgBudgets{
			OrgBudget: &manifest.BudgetEntry{Credits: 1000000},
		},
	})

	rep, err := sy.SyncOrgSettings(context.Background(), m, mappingTo("gh/dest"), Options{Apply: true})
	if err != nil {
		t.Fatalf("budget write error must not propagate, got: %v", err)
	}

	errActions := actionsOfStatus(rep, "error")
	if len(errActions) == 0 {
		t.Error("expected an error action when SetBudget fails")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// syncBudgets — per-project budget
// ─────────────────────────────────────────────────────────────────────────────

func TestSyncBudgets_ProjectBudget_NoMapping_ManualAction(t *testing.T) {
	// Without an explicit project mapping, per-project budgets cannot be transferred.
	fw := &fakeOrgSettingsWriter{}
	sy := newOrgSettingsSyncer(fw)

	srcProjID := "src-proj-uuid-1"
	m := orgSettingsManifest(&manifest.OrgSettings{
		Budgets: &manifest.OrgBudgets{
			ProjectBudgets: []manifest.BudgetEntry{
				{Credits: 50000, BudgetID: "bud-2", EnforcementType: "block", ProjectID: &srcProjID},
			},
		},
	})

	rep, err := sy.SyncOrgSettings(context.Background(), m, mappingTo("gh/dest"), Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// SetBudget must NOT be called for unmapped per-project budgets.
	if fw.hasCalled("SetBudget") {
		t.Error("SetBudget must NOT be called for unmapped per-project budget")
	}

	// A manual action must be emitted.
	manual := actionsOfStatus(rep, "manual")
	if len(manual) == 0 {
		t.Fatal("expected a manual action for unmapped per-project budget")
	}
	found := false
	for _, a := range manual {
		if strings.Contains(a.Detail, srcProjID) {
			found = true
		}
	}
	if !found {
		t.Errorf("manual action detail should mention source project UUID %q; got %+v", srcProjID, manual)
	}
}

func TestSyncBudgets_ProjectBudget_WithMapping_ApplyTrue(t *testing.T) {
	var gotProjectID *string
	fw := &fakeOrgSettingsWriter{
		setBudget: func(orgUUID string, projectID *string, credits int) error {
			gotProjectID = projectID
			return nil
		},
	}
	sy := newOrgSettingsSyncer(fw)

	srcProjID := "src-proj-uuid-1"
	destProjID := "dest-proj-uuid-mapped"
	m := orgSettingsManifest(&manifest.OrgSettings{
		Budgets: &manifest.OrgBudgets{
			ProjectBudgets: []manifest.BudgetEntry{
				{Credits: 75000, ProjectID: &srcProjID},
			},
		},
	})

	// Provide a mapping from source project UUID to destination project UUID.
	mapping := &manifest.Mapping{
		Org:      manifest.OrgMapping{From: "gh/src", To: "gh/dest"},
		Projects: map[string]string{srcProjID: destProjID},
	}

	rep, err := sy.SyncOrgSettings(context.Background(), m, mapping, Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fw.budgetSets != 1 {
		t.Errorf("expected 1 SetBudget call, got %d", fw.budgetSets)
	}
	if gotProjectID == nil || *gotProjectID != destProjID {
		t.Errorf("SetBudget projectID: got %v want %q", gotProjectID, destProjID)
	}

	setActions := actionsOfStatus(rep, "set")
	if len(setActions) == 0 {
		t.Error("expected set action for mapped per-project budget")
	}
}

func TestSyncBudgets_NilBudgets_NoWrite(t *testing.T) {
	fw := &fakeOrgSettingsWriter{}
	sy := newOrgSettingsSyncer(fw)

	m := orgSettingsManifest(&manifest.OrgSettings{
		FeatureFlags: map[string]bool{},
		// No Budgets.
	})

	_, err := sy.SyncOrgSettings(context.Background(), m, mappingTo("gh/dest"), Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fw.hasCalled("SetBudget") {
		t.Error("SetBudget must NOT be called when Budgets is nil")
	}
}

func TestSyncBudgets_DryRun_EnforcementTypeNoted(t *testing.T) {
	// When enforcement_type is not the default, a note must be present in the detail.
	fw := &fakeOrgSettingsWriter{}
	sy := newOrgSettingsSyncer(fw)

	m := orgSettingsManifest(&manifest.OrgSettings{
		Budgets: &manifest.OrgBudgets{
			OrgBudget: &manifest.BudgetEntry{
				Credits:         1000000,
				EnforcementType: "warn",
			},
		},
	})

	rep, err := sy.SyncOrgSettings(context.Background(), m, mappingTo("gh/dest"), Options{Apply: false})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, a := range rep.Actions {
		if a.Target == "budget:org" {
			// Detail should mention enforcement_type.
			if !strings.Contains(a.Detail, "warn") {
				t.Errorf("detail should mention enforcement_type=warn; got %q", a.Detail)
			}
			return
		}
	}
	t.Error("budget:org action not found")
}

// ─────────────────────────────────────────────────────────────────────────────
// syncBlockUnregisteredUsers
// ─────────────────────────────────────────────────────────────────────────────

func TestSyncBlockUnregisteredUsers_DryRunNoWrites(t *testing.T) {
	fw := &fakeOrgSettingsWriter{}
	sy := newOrgSettingsSyncer(fw)

	enabled := true
	m := orgSettingsManifest(&manifest.OrgSettings{
		BlockUnregisteredUsers: &enabled,
	})

	rep, err := sy.SyncOrgSettings(context.Background(), m, mappingTo("gh/dest"), Options{Apply: false})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fw.hasCalled("SetBlockUnregisteredUsers") {
		t.Error("SetBlockUnregisteredUsers must NOT be called in dry-run mode")
	}

	setActions := actionsOfStatus(rep, "set")
	if len(setActions) == 0 {
		t.Error("expected a set action for block_unregistered_users in dry-run")
	}
}

func TestSyncBlockUnregisteredUsers_ApplyTrue_Enable(t *testing.T) {
	var gotEnabled bool
	fw := &fakeOrgSettingsWriter{
		setBlockUnregisteredUsers: func(orgUUID string, enabled bool) error {
			gotEnabled = enabled
			return nil
		},
	}
	sy := newOrgSettingsSyncer(fw)

	enabled := true
	m := orgSettingsManifest(&manifest.OrgSettings{
		BlockUnregisteredUsers: &enabled,
	})

	rep, err := sy.SyncOrgSettings(context.Background(), m, mappingTo("gh/dest"), Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fw.blockUnregisteredUsersSets != 1 {
		t.Errorf("expected 1 SetBlockUnregisteredUsers call, got %d", fw.blockUnregisteredUsersSets)
	}
	if !gotEnabled {
		t.Error("expected enabled=true")
	}

	setActions := actionsOfStatus(rep, "set")
	if len(setActions) == 0 {
		t.Error("expected a set action for block_unregistered_users")
	}
}

func TestSyncBlockUnregisteredUsers_ApplyTrue_Disable(t *testing.T) {
	var gotEnabled bool
	fw := &fakeOrgSettingsWriter{
		setBlockUnregisteredUsers: func(orgUUID string, enabled bool) error {
			gotEnabled = enabled
			return nil
		},
	}
	sy := newOrgSettingsSyncer(fw)

	disabled := false
	m := orgSettingsManifest(&manifest.OrgSettings{
		BlockUnregisteredUsers: &disabled,
	})

	_, err := sy.SyncOrgSettings(context.Background(), m, mappingTo("gh/dest"), Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fw.blockUnregisteredUsersSets != 1 {
		t.Errorf("expected 1 SetBlockUnregisteredUsers call, got %d", fw.blockUnregisteredUsersSets)
	}
	if gotEnabled {
		t.Error("expected enabled=false")
	}
}

func TestSyncBlockUnregisteredUsers_Nil_NoWrite(t *testing.T) {
	fw := &fakeOrgSettingsWriter{}
	sy := newOrgSettingsSyncer(fw)

	m := orgSettingsManifest(&manifest.OrgSettings{
		FeatureFlags: map[string]bool{},
		// No BlockUnregisteredUsers
	})

	_, err := sy.SyncOrgSettings(context.Background(), m, mappingTo("gh/dest"), Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fw.hasCalled("SetBlockUnregisteredUsers") {
		t.Error("SetBlockUnregisteredUsers must NOT be called when BlockUnregisteredUsers is nil")
	}
}

func TestSyncBlockUnregisteredUsers_WriteError_IsErrorAction(t *testing.T) {
	fw := &fakeOrgSettingsWriter{
		setBlockUnregisteredUsers: func(orgUUID string, enabled bool) error {
			return errors.New("feature write failed")
		},
	}
	sy := newOrgSettingsSyncer(fw)

	enabled := true
	m := orgSettingsManifest(&manifest.OrgSettings{
		BlockUnregisteredUsers: &enabled,
	})

	rep, err := sy.SyncOrgSettings(context.Background(), m, mappingTo("gh/dest"), Options{Apply: true})
	if err != nil {
		t.Fatalf("block_unregistered_users write error must not propagate, got: %v", err)
	}

	errActions := actionsOfStatus(rep, "error")
	if len(errActions) == 0 {
		t.Error("expected an error action when SetBlockUnregisteredUsers fails")
	}
}
