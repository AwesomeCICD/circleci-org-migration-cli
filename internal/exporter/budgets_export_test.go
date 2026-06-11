package exporter_test

// budgets_export_test.go contains focused unit tests for the spend-budget and
// block-unregistered-users capture code paths in exportOrgSettings.

import (
	"context"
	"errors"
	"testing"

	"github.com/AwesomeCICD/circleci-org-migration-cli/api/org"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/exporter"
)

// budgetExporter builds an Exporter whose GetBudgets and
// GetBlockUnregisteredUsers fakes are controlled by the caller.
func budgetExporter(
	getBudgets func(orgUUID string) ([]org.Budget, error),
	getBlockUnregisteredUsers func(orgUUID string) (bool, error),
) *exporter.Exporter {
	return &exporter.Exporter{
		Org: &fakeOrgAPI{
			getOrganization:           func(string) (*org.Organization, error) { return defaultOrg(), nil },
			getBudgets:                getBudgets,
			getBlockUnregisteredUsers: getBlockUnregisteredUsers,
		},
		Contexts: &fakeContextAPI{},
		Projects: &fakeProjectAPI{},
	}
}

var budgetOpts = exporter.Options{OrgSlug: "gh/myorg"}

// ─────────────────────────────────────────────────────────────────────────────
// Spend budgets — capture
// ─────────────────────────────────────────────────────────────────────────────

func TestBudgets_OrgBudgetCaptured(t *testing.T) {
	ex := budgetExporter(func(orgUUID string) ([]org.Budget, error) {
		return []org.Budget{
			{
				Credits:         1000000,
				BudgetID:        "budget-uuid-1",
				EnforcementType: "warn",
				ProjectID:       nil,
			},
		}, nil
	}, nil)

	m, err := ex.Export(context.Background(), budgetOpts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Source.Org.Settings == nil {
		t.Fatal("OrgSettings is nil")
	}
	b := m.Source.Org.Settings.Budgets
	if b == nil {
		t.Fatal("Budgets is nil in manifest")
	}
	if b.OrgBudget == nil {
		t.Fatal("OrgBudget is nil")
	}
	if b.OrgBudget.Credits != 1000000 {
		t.Errorf("OrgBudget.Credits: got %d want 1000000", b.OrgBudget.Credits)
	}
	if b.OrgBudget.EnforcementType != "warn" {
		t.Errorf("OrgBudget.EnforcementType: got %q want %q", b.OrgBudget.EnforcementType, "warn")
	}
	if b.OrgBudget.ProjectID != nil {
		t.Errorf("OrgBudget.ProjectID: got %v want nil", b.OrgBudget.ProjectID)
	}
	if len(b.ProjectBudgets) != 0 {
		t.Errorf("ProjectBudgets: expected empty, got %v", b.ProjectBudgets)
	}
}

func TestBudgets_ProjectBudgetCaptured(t *testing.T) {
	projID := "proj-uuid-1"
	ex := budgetExporter(func(orgUUID string) ([]org.Budget, error) {
		return []org.Budget{
			{
				Credits:         50000,
				BudgetID:        "budget-proj-uuid",
				EnforcementType: "block",
				ProjectID:       &projID,
			},
		}, nil
	}, nil)

	m, err := ex.Export(context.Background(), budgetOpts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Source.Org.Settings == nil {
		t.Fatal("OrgSettings is nil")
	}
	b := m.Source.Org.Settings.Budgets
	if b == nil {
		t.Fatal("Budgets is nil")
	}
	if b.OrgBudget != nil {
		t.Error("OrgBudget should be nil when only project budget is returned")
	}
	if len(b.ProjectBudgets) != 1 {
		t.Fatalf("ProjectBudgets: expected 1, got %d", len(b.ProjectBudgets))
	}
	pb := b.ProjectBudgets[0]
	if pb.Credits != 50000 {
		t.Errorf("ProjectBudgets[0].Credits: got %d want 50000", pb.Credits)
	}
	if pb.ProjectID == nil || *pb.ProjectID != projID {
		t.Errorf("ProjectBudgets[0].ProjectID: got %v want %q", pb.ProjectID, projID)
	}
}

func TestBudgets_APIError_Warning(t *testing.T) {
	ex := budgetExporter(func(orgUUID string) ([]org.Budget, error) {
		return nil, errors.New("permission denied")
	}, nil)

	m, err := ex.Export(context.Background(), budgetOpts)
	if err != nil {
		t.Fatalf("export must not fail on budget API error, got: %v", err)
	}

	if m.Source.Org.Settings != nil && m.Source.Org.Settings.Budgets != nil {
		t.Error("Budgets should be nil when API returns error")
	}

	found := false
	for _, w := range m.Warnings {
		if w.Code == "budgets_unreadable" && w.Scope == "org" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected warning with code 'budgets_unreadable', got warnings: %+v", m.Warnings)
	}
}

func TestBudgets_EmptyResponse_NilBudgets(t *testing.T) {
	ex := budgetExporter(func(orgUUID string) ([]org.Budget, error) {
		return []org.Budget{}, nil
	}, nil)

	m, err := ex.Export(context.Background(), budgetOpts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Source.Org.Settings != nil && m.Source.Org.Settings.Budgets != nil {
		t.Error("expected Budgets to be nil for empty API response")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Block unregistered users — capture
// ─────────────────────────────────────────────────────────────────────────────

func TestBlockUnregisteredUsers_TrueCaptured(t *testing.T) {
	ex := budgetExporter(nil, func(orgUUID string) (bool, error) {
		return true, nil
	})

	m, err := ex.Export(context.Background(), budgetOpts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Source.Org.Settings == nil {
		t.Fatal("OrgSettings is nil")
	}
	v := m.Source.Org.Settings.BlockUnregisteredUsers
	if v == nil {
		t.Fatal("BlockUnregisteredUsers is nil")
	}
	if !*v {
		t.Errorf("BlockUnregisteredUsers: got false want true")
	}
}

func TestBlockUnregisteredUsers_FalseCaptured(t *testing.T) {
	ex := budgetExporter(nil, func(orgUUID string) (bool, error) {
		return false, nil
	})

	m, err := ex.Export(context.Background(), budgetOpts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Source.Org.Settings == nil {
		t.Fatal("OrgSettings is nil")
	}
	v := m.Source.Org.Settings.BlockUnregisteredUsers
	if v == nil {
		t.Fatal("BlockUnregisteredUsers is nil")
	}
	if *v {
		t.Errorf("BlockUnregisteredUsers: got true want false")
	}
}

func TestBlockUnregisteredUsers_APIError_Warning(t *testing.T) {
	ex := budgetExporter(nil, func(orgUUID string) (bool, error) {
		return false, errors.New("feature unavailable")
	})

	m, err := ex.Export(context.Background(), budgetOpts)
	if err != nil {
		t.Fatalf("export must not fail on block-unregistered-users API error, got: %v", err)
	}

	// Setting must not be captured.
	if m.Source.Org.Settings != nil && m.Source.Org.Settings.BlockUnregisteredUsers != nil {
		t.Error("BlockUnregisteredUsers should be nil when API returns error")
	}

	found := false
	for _, w := range m.Warnings {
		if w.Code == "block_unregistered_users_unreadable" && w.Scope == "org" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected warning with code 'block_unregistered_users_unreadable', got warnings: %+v", m.Warnings)
	}
}
