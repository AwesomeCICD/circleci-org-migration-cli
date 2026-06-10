package org

import (
	"fmt"
	"net/url"
)

// ─────────────────────────────────────────────────────────────────────────────
// Spend budgets
// ─────────────────────────────────────────────────────────────────────────────

// Budget represents one spend-budget entry returned by the budgets API.
// ProjectID is nil for the org-level budget and a project UUID for per-project
// budgets. Consumption, Percentage, and ThresholdExceeded are runtime stats and
// are not used for capture/transfer.
type Budget struct {
	Credits           int     `json:"credits"`
	BudgetID          string  `json:"budget_id"`
	EnforcementType   string  `json:"enforcement_type"`
	ProjectID         *string `json:"project_id"`
	Consumption       int     `json:"consumption,omitempty"`
	Percentage        float64 `json:"percentage,omitempty"`
	ThresholdExceeded bool    `json:"threshold_exceeded,omitempty"`
}

// budgetsListResponse mirrors GET /private/orgs/{orgUUID}/budgets.
type budgetsListResponse struct {
	Budgets []Budget `json:"budgets"`
}

// budgetSetRequest is the body for PUT /private/orgs/{orgUUID}/budgets.
type budgetSetRequest struct {
	Credits   int     `json:"credits"`
	ProjectID *string `json:"project_id"`
}

// budgetsPath returns the relative URL path for the budgets endpoint.
func budgetsPath(orgUUID string) (*url.URL, error) {
	return url.Parse("private/orgs/" + url.PathEscape(orgUUID) + "/budgets")
}

// budgetPath returns the relative URL path for a single budget (by budget_id).
func budgetPath(orgUUID, budgetID string) (*url.URL, error) {
	return url.Parse("private/orgs/" + url.PathEscape(orgUUID) + "/budgets/" + url.PathEscape(budgetID))
}

// GetBudgets returns all spend-budget entries for the given org. The response
// includes the org-level budget (project_id == nil) and any per-project budgets.
//
// Endpoint: GET https://app.circleci.com/private/orgs/{orgUUID}/budgets
func (c *Client) GetBudgets(orgUUID string) ([]Budget, error) {
	u, err := budgetsPath(orgUUID)
	if err != nil {
		return nil, fmt.Errorf("GetBudgets: build URL: %w", err)
	}

	req, err := c.app.NewRequest("GET", u, nil)
	if err != nil {
		return nil, fmt.Errorf("GetBudgets: build request: %w", err)
	}

	var raw budgetsListResponse
	if _, err := c.app.DoRequest(req, &raw); err != nil {
		return nil, fmt.Errorf("GetBudgets %s: %w", orgUUID, err)
	}
	return raw.Budgets, nil
}

// SetBudget creates or updates a budget via PUT. Pass projectID == nil for the
// org-level budget; pass a non-nil project UUID for a per-project budget.
//
// Endpoint: PUT https://app.circleci.com/private/orgs/{orgUUID}/budgets
func (c *Client) SetBudget(orgUUID string, projectID *string, credits int) error {
	u, err := budgetsPath(orgUUID)
	if err != nil {
		return fmt.Errorf("SetBudget: build URL: %w", err)
	}

	body := budgetSetRequest{Credits: credits, ProjectID: projectID}
	req, err := c.app.NewRequest("PUT", u, body)
	if err != nil {
		return fmt.Errorf("SetBudget: build request: %w", err)
	}

	var ignored map[string]any
	if _, err := c.app.DoRequest(req, &ignored); err != nil {
		return fmt.Errorf("SetBudget %s: %w", orgUUID, err)
	}
	return nil
}

// DeleteBudget removes a budget entry identified by budgetID.
//
// Endpoint: DELETE https://app.circleci.com/private/orgs/{orgUUID}/budgets/{budgetID}
func (c *Client) DeleteBudget(orgUUID, budgetID string) error {
	u, err := budgetPath(orgUUID, budgetID)
	if err != nil {
		return fmt.Errorf("DeleteBudget: build URL: %w", err)
	}

	req, err := c.app.NewRequest("DELETE", u, nil)
	if err != nil {
		return fmt.Errorf("DeleteBudget: build request: %w", err)
	}

	var ignored map[string]any
	if _, err := c.app.DoRequest(req, &ignored); err != nil {
		return fmt.Errorf("DeleteBudget %s/%s: %w", orgUUID, budgetID, err)
	}
	return nil
}
