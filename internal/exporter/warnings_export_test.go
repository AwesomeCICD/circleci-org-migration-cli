package exporter_test

// warnings_export_test.go contains focused unit tests for the non-migratable-item
// warnings introduced by issue #130: webhook signing secrets, runner agent tokens,
// org orbs (republish), and budget enforcement=block.

import (
	"testing"

	"github.com/AwesomeCICD/circleci-org-migration-cli/api/org"
	"github.com/AwesomeCICD/circleci-org-migration-cli/api/project"
	apirunner "github.com/AwesomeCICD/circleci-org-migration-cli/api/runner"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/exporter"
)

// ─────────────────────────────────────────────────────────────────────────────
// Webhook signing secret warning
// ─────────────────────────────────────────────────────────────────────────────

// TestWebhookSigningSecretWarning_EmittedWhenWebhooksPresent verifies that
// when webhooks are captured a "webhook_signing_secret_excluded" warning is
// added per project with webhooks.
func TestWebhookSigningSecretWarning_EmittedWhenWebhooksPresent(t *testing.T) {
	t.Helper()
	ex := &exporter.Exporter{
		Org: &fakeOrgAPI{
			getOrganization: func(string) (*org.Organization, error) { return defaultOrg(), nil },
		},
		Contexts: &fakeContextAPI{},
		Projects: &fakeProjectAPI{
			getProject: func(slug string) (*project.Project, error) {
				return &project.Project{ID: "proj-id", Name: "web", Slug: slug}, nil
			},
			listWebhooks: func(projectID string) ([]project.Webhook, error) {
				tls := true
				return []project.Webhook{
					{ID: "wh-1", Name: "deploy-notify", URL: "https://hooks.example.com", Events: []string{"workflow-completed"}, VerifyTLS: &tls},
				}, nil
			},
			listOrgProjects: func(orgID string) ([]project.OrgProject, error) {
				return []project.OrgProject{{Slug: "gh/myorg/web"}}, nil
			},
		},
	}

	m, err := ex.Export(exporter.Options{
		OrgSlug:         "gh/myorg",
		IncludeProjects: true,
		IncludeExtras:   true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	found := false
	for _, w := range m.Warnings {
		if w.Code == "webhook_signing_secret_excluded" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected warning with code 'webhook_signing_secret_excluded', got warnings: %+v", m.Warnings)
	}
}

// TestWebhookSigningSecretWarning_NotEmittedWhenNoWebhooks verifies that when
// no webhooks are captured, no webhook signing secret warning is added.
func TestWebhookSigningSecretWarning_NotEmittedWhenNoWebhooks(t *testing.T) {
	ex := &exporter.Exporter{
		Org: &fakeOrgAPI{
			getOrganization: func(string) (*org.Organization, error) { return defaultOrg(), nil },
		},
		Contexts: &fakeContextAPI{},
		Projects: &fakeProjectAPI{
			getProject: func(slug string) (*project.Project, error) {
				return &project.Project{ID: "proj-id", Name: "web", Slug: slug}, nil
			},
			listWebhooks: func(projectID string) ([]project.Webhook, error) {
				return nil, nil // no webhooks
			},
			listOrgProjects: func(orgID string) ([]project.OrgProject, error) {
				return []project.OrgProject{{Slug: "gh/myorg/web"}}, nil
			},
		},
	}

	m, err := ex.Export(exporter.Options{
		OrgSlug:         "gh/myorg",
		IncludeProjects: true,
		IncludeExtras:   true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, w := range m.Warnings {
		if w.Code == "webhook_signing_secret_excluded" {
			t.Errorf("unexpected webhook_signing_secret_excluded warning when no webhooks present: %+v", w)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Runner agent token warning
// ─────────────────────────────────────────────────────────────────────────────

// TestRunnerAgentTokenWarning_EmittedPerResourceClass verifies that each
// captured runner resource class gets a "runner_agent_token_excluded" warning.
func TestRunnerAgentTokenWarning_EmittedPerResourceClass(t *testing.T) {
	ex := minimalExporter()
	ex.Runner = &fakeRunnerAPI{
		getResourceClasses: func(namespace string) ([]apirunner.ResourceClass, error) {
			return []apirunner.ResourceClass{
				{ResourceClass: "acme/runner-a", Description: "first"},
				{ResourceClass: "acme/runner-b", Description: "second"},
			}, nil
		},
	}

	m, err := ex.Export(exporter.Options{
		OrgSlug:         "gh/myorg",
		RunnerNamespace: "acme",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	warningCount := 0
	for _, w := range m.Warnings {
		if w.Code == "runner_agent_token_excluded" {
			warningCount++
		}
	}
	if warningCount != 2 {
		t.Errorf("expected 2 runner_agent_token_excluded warnings (one per class), got %d: %+v", warningCount, m.Warnings)
	}
}

// TestRunnerAgentTokenWarning_NotEmittedWhenNoRunnerClasses verifies that when
// no runner resource classes are captured, no agent token warning is added.
func TestRunnerAgentTokenWarning_NotEmittedWhenNoRunnerClasses(t *testing.T) {
	ex := minimalExporter()
	ex.Runner = &fakeRunnerAPI{
		getResourceClasses: func(namespace string) ([]apirunner.ResourceClass, error) {
			return nil, nil // no classes
		},
	}

	m, err := ex.Export(exporter.Options{
		OrgSlug:         "gh/myorg",
		RunnerNamespace: "acme",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, w := range m.Warnings {
		if w.Code == "runner_agent_token_excluded" {
			t.Errorf("unexpected runner_agent_token_excluded warning when no classes present: %+v", w)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Org orbs republish warning
// ─────────────────────────────────────────────────────────────────────────────

// TestOrgOrbsWarning_EmittedWhenOrbsPresent verifies that when org orbs are
// captured a single "orbs_require_republish" warning is emitted.
func TestOrgOrbsWarning_EmittedWhenOrbsPresent(t *testing.T) {
	ex := &exporter.Exporter{
		Org: &fakeOrgAPI{
			getOrganization: func(string) (*org.Organization, error) { return defaultOrg(), nil },
			getOrgOrbs: func(orgUUID string) ([]org.OrgOrb, error) {
				return []org.OrgOrb{
					{OrbName: "acme/my-orb", LatestVersionNumber: "1.0.0", IsPrivate: false},
					{OrbName: "acme/other-orb", LatestVersionNumber: "0.2.1", IsPrivate: true},
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

	found := false
	for _, w := range m.Warnings {
		if w.Code == "orbs_require_republish" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected warning with code 'orbs_require_republish', got warnings: %+v", m.Warnings)
	}
}

// TestOrgOrbsWarning_NotEmittedWhenNoOrbs verifies that when no orbs are
// captured, no orbs_require_republish warning is added.
func TestOrgOrbsWarning_NotEmittedWhenNoOrbs(t *testing.T) {
	ex := &exporter.Exporter{
		Org: &fakeOrgAPI{
			getOrganization: func(string) (*org.Organization, error) { return defaultOrg(), nil },
			getOrgOrbs: func(orgUUID string) ([]org.OrgOrb, error) {
				return nil, nil // no orbs
			},
		},
		Contexts: &fakeContextAPI{},
		Projects: &fakeProjectAPI{},
	}

	m, err := ex.Export(exporter.Options{OrgSlug: "gh/myorg"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, w := range m.Warnings {
		if w.Code == "orbs_require_republish" {
			t.Errorf("unexpected orbs_require_republish warning when no orbs present: %+v", w)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Budget enforcement=block warning
// ─────────────────────────────────────────────────────────────────────────────

// TestBudgetEnforcementBlockWarning_EmittedWhenBlockPresent verifies that
// when a budget with enforcement_type=block is captured, a
// "budget_enforcement_block_not_transferred" warning is emitted.
func TestBudgetEnforcementBlockWarning_EmittedWhenBlockPresent(t *testing.T) {
	ex := &exporter.Exporter{
		Org: &fakeOrgAPI{
			getOrganization: func(string) (*org.Organization, error) { return defaultOrg(), nil },
			getBudgets: func(orgUUID string) ([]org.Budget, error) {
				return []org.Budget{
					{
						Credits:         500000,
						BudgetID:        "budget-1",
						EnforcementType: "block",
						ProjectID:       nil,
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

	found := false
	for _, w := range m.Warnings {
		if w.Code == "budget_enforcement_block_not_transferred" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected warning with code 'budget_enforcement_block_not_transferred', got warnings: %+v", m.Warnings)
	}
}

// TestBudgetEnforcementBlockWarning_NotEmittedForWarn verifies that budgets
// with enforcement_type=warn do NOT trigger the block-enforcement warning.
func TestBudgetEnforcementBlockWarning_NotEmittedForWarn(t *testing.T) {
	ex := &exporter.Exporter{
		Org: &fakeOrgAPI{
			getOrganization: func(string) (*org.Organization, error) { return defaultOrg(), nil },
			getBudgets: func(orgUUID string) ([]org.Budget, error) {
				return []org.Budget{
					{
						Credits:         1000000,
						BudgetID:        "budget-warn",
						EnforcementType: "warn",
						ProjectID:       nil,
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

	for _, w := range m.Warnings {
		if w.Code == "budget_enforcement_block_not_transferred" {
			t.Errorf("unexpected budget_enforcement_block_not_transferred warning for warn enforcement: %+v", w)
		}
	}
}

// TestBudgetEnforcementBlockWarning_PerProjectBudget verifies that the block
// warning is also emitted for per-project budgets with enforcement=block.
func TestBudgetEnforcementBlockWarning_PerProjectBudget(t *testing.T) {
	projID := "proj-uuid-99"
	ex := &exporter.Exporter{
		Org: &fakeOrgAPI{
			getOrganization: func(string) (*org.Organization, error) { return defaultOrg(), nil },
			getBudgets: func(orgUUID string) ([]org.Budget, error) {
				return []org.Budget{
					{
						Credits:         200000,
						BudgetID:        "budget-proj",
						EnforcementType: "block",
						ProjectID:       &projID,
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

	found := false
	for _, w := range m.Warnings {
		if w.Code == "budget_enforcement_block_not_transferred" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected budget_enforcement_block_not_transferred warning for project budget with block enforcement, got: %+v", m.Warnings)
	}
}
