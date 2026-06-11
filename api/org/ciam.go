package org

import (
	"fmt"
	"net/url"
)

// OrgRoleGrant is one org-level CIAM role assignment.
// The portable identity is Email (keyed) and Username (display); UserID is the
// server-assigned UUID required for write operations.
type OrgRoleGrant struct {
	UserID   string `json:"userId"`
	Email    string `json:"email"`
	Username string `json:"username"`
	Role     string `json:"role"` // org-admin | org-contributor | org-viewer
}

// orgRoleGrantsResponse mirrors GET /private/ciam/orgs/{orgID}/role-grants.
type orgRoleGrantsResponse struct {
	Items []OrgRoleGrant `json:"items"`
}

// ListOrgRoleGrants returns all org-level CIAM role grants.
//
// Endpoint: GET https://app.circleci.com/private/ciam/orgs/{orgID}/role-grants
func (c *Client) ListOrgRoleGrants(orgID string) ([]OrgRoleGrant, error) {
	u, err := url.Parse("private/ciam/orgs/" + url.PathEscape(orgID) + "/role-grants")
	if err != nil {
		return nil, fmt.Errorf("ListOrgRoleGrants: build URL: %w", err)
	}

	req, err := c.app.NewRequest("GET", u, nil)
	if err != nil {
		return nil, fmt.Errorf("ListOrgRoleGrants: build request: %w", err)
	}

	var raw orgRoleGrantsResponse
	if _, err := c.app.DoRequest(req, &raw); err != nil {
		return nil, fmt.Errorf("ListOrgRoleGrants %s: %w", orgID, err)
	}
	return raw.Items, nil
}

// SetOrgUserRole assigns a CIAM role to a user in the org.
//
// Endpoint: PUT https://app.circleci.com/private/ciam/orgs/{orgID}/role-grants/user/{userID}
// Body: {"role": "<role>"}
func (c *Client) SetOrgUserRole(orgID, userID, role string) error {
	u, err := url.Parse("private/ciam/orgs/" + url.PathEscape(orgID) + "/role-grants/user/" + url.PathEscape(userID))
	if err != nil {
		return fmt.Errorf("SetOrgUserRole: build URL: %w", err)
	}

	body := map[string]string{"role": role}
	req, err := c.app.NewRequest("PUT", u, body)
	if err != nil {
		return fmt.Errorf("SetOrgUserRole: build request: %w", err)
	}

	var ignored map[string]any
	if _, err := c.app.DoRequest(req, &ignored); err != nil {
		return fmt.Errorf("SetOrgUserRole %s/%s: %w", orgID, userID, err)
	}
	return nil
}

// CreateGroup creates a new CIAM group in the org.
//
// Endpoint: POST https://app.circleci.com/private/ciam/orgs/{orgID}/groups
// Body: {"orgId": "...", "name": "...", "description": "..."}
func (c *Client) CreateGroup(orgID, name, description string) (*Group, error) {
	u, err := url.Parse("private/ciam/orgs/" + url.PathEscape(orgID) + "/groups")
	if err != nil {
		return nil, fmt.Errorf("CreateGroup: build URL: %w", err)
	}

	body := map[string]string{
		"orgId":       orgID,
		"name":        name,
		"description": description,
	}
	req, err := c.app.NewRequest("POST", u, body)
	if err != nil {
		return nil, fmt.Errorf("CreateGroup: build request: %w", err)
	}

	var g Group
	if _, err := c.app.DoRequest(req, &g); err != nil {
		return nil, fmt.Errorf("CreateGroup %s/%s: %w", orgID, name, err)
	}
	return &g, nil
}

// AddUsersToGroup adds users (by userID) to a CIAM group.
//
// Endpoint: POST https://app.circleci.com/private/ciam/orgs/{orgID}/groups/{groupID}/add-users
// Body: {"user_ids": [...]}
func (c *Client) AddUsersToGroup(orgID, groupID string, userIDs []string) error {
	u, err := url.Parse("private/ciam/orgs/" + url.PathEscape(orgID) + "/groups/" + url.PathEscape(groupID) + "/add-users")
	if err != nil {
		return fmt.Errorf("AddUsersToGroup: build URL: %w", err)
	}

	body := map[string]any{"user_ids": userIDs}
	req, err := c.app.NewRequest("POST", u, body)
	if err != nil {
		return fmt.Errorf("AddUsersToGroup: build request: %w", err)
	}

	var ignored map[string]any
	if _, err := c.app.DoRequest(req, &ignored); err != nil {
		return fmt.Errorf("AddUsersToGroup %s/%s: %w", orgID, groupID, err)
	}
	return nil
}

// ProjectUserRoleGrant is one project-level CIAM role grant for a user.
type ProjectUserRoleGrant struct {
	UserID   string `json:"user_id"`
	Email    string `json:"email"`
	Username string `json:"username"`
	Role     string `json:"role"` // project-admin | project-contributor | project-viewer
}

// projectUserRoleGrantsResponse mirrors GET /private/ciam/orgs/{orgID}/projects/{projectID}/role-grants.
type projectUserRoleGrantsResponse struct {
	Items []ProjectUserRoleGrant `json:"items"`
}

// ListProjectUserRoleGrants returns all user-level CIAM role grants for a project.
//
// Endpoint: GET https://app.circleci.com/private/ciam/orgs/{orgID}/projects/{projectID}/role-grants
func (c *Client) ListProjectUserRoleGrants(orgID, projectID string) ([]ProjectUserRoleGrant, error) {
	u, err := url.Parse("private/ciam/orgs/" + url.PathEscape(orgID) + "/projects/" + url.PathEscape(projectID) + "/role-grants")
	if err != nil {
		return nil, fmt.Errorf("ListProjectUserRoleGrants: build URL: %w", err)
	}

	req, err := c.app.NewRequest("GET", u, nil)
	if err != nil {
		return nil, fmt.Errorf("ListProjectUserRoleGrants: build request: %w", err)
	}

	var raw projectUserRoleGrantsResponse
	if _, err := c.app.DoRequest(req, &raw); err != nil {
		return nil, fmt.Errorf("ListProjectUserRoleGrants %s/%s: %w", orgID, projectID, err)
	}
	return raw.Items, nil
}

// SetProjectUserRole assigns a CIAM role to a user on a project.
//
// Endpoint: PUT https://app.circleci.com/private/ciam/orgs/{orgID}/projects/{projectID}/role-grants/user/{userID}
// Body: {"role": "<role>"}
func (c *Client) SetProjectUserRole(orgID, projectID, userID, role string) error {
	u, err := url.Parse("private/ciam/orgs/" + url.PathEscape(orgID) + "/projects/" + url.PathEscape(projectID) + "/role-grants/user/" + url.PathEscape(userID))
	if err != nil {
		return fmt.Errorf("SetProjectUserRole: build URL: %w", err)
	}

	body := map[string]string{"role": role}
	req, err := c.app.NewRequest("PUT", u, body)
	if err != nil {
		return fmt.Errorf("SetProjectUserRole: build request: %w", err)
	}

	var ignored map[string]any
	if _, err := c.app.DoRequest(req, &ignored); err != nil {
		return fmt.Errorf("SetProjectUserRole %s/%s/%s: %w", orgID, projectID, userID, err)
	}
	return nil
}

// ProjectGroupRoleGrant is one project-level CIAM role grant for a group.
type ProjectGroupRoleGrant struct {
	GroupID string `json:"group_id"`
	Role    string `json:"role"` // project-admin | project-contributor | project-viewer
}

// projectGroupRoleGrantsResponse mirrors GET /private/ciam/orgs/{orgID}/projects/{projectID}/groups.
type projectGroupRoleGrantsResponse struct {
	Items []ProjectGroupRoleGrant `json:"items"`
}

// ListProjectGroupRoleGrants returns all group-level CIAM role grants for a project.
//
// Endpoint: GET https://app.circleci.com/private/ciam/orgs/{orgID}/projects/{projectID}/groups
func (c *Client) ListProjectGroupRoleGrants(orgID, projectID string) ([]ProjectGroupRoleGrant, error) {
	u, err := url.Parse("private/ciam/orgs/" + url.PathEscape(orgID) + "/projects/" + url.PathEscape(projectID) + "/groups")
	if err != nil {
		return nil, fmt.Errorf("ListProjectGroupRoleGrants: build URL: %w", err)
	}

	req, err := c.app.NewRequest("GET", u, nil)
	if err != nil {
		return nil, fmt.Errorf("ListProjectGroupRoleGrants: build request: %w", err)
	}

	var raw projectGroupRoleGrantsResponse
	if _, err := c.app.DoRequest(req, &raw); err != nil {
		return nil, fmt.Errorf("ListProjectGroupRoleGrants %s/%s: %w", orgID, projectID, err)
	}
	return raw.Items, nil
}

// AddProjectGroupRole grants a role to a set of groups on a project.
//
// Endpoint: POST https://app.circleci.com/private/ciam/orgs/{orgID}/projects/{projectID}/groups
// Body: {"group_ids": [...], "role": "<role>"}
func (c *Client) AddProjectGroupRole(orgID, projectID string, groupIDs []string, role string) error {
	u, err := url.Parse("private/ciam/orgs/" + url.PathEscape(orgID) + "/projects/" + url.PathEscape(projectID) + "/groups")
	if err != nil {
		return fmt.Errorf("AddProjectGroupRole: build URL: %w", err)
	}

	body := map[string]any{"group_ids": groupIDs, "role": role}
	req, err := c.app.NewRequest("POST", u, body)
	if err != nil {
		return fmt.Errorf("AddProjectGroupRole: build request: %w", err)
	}

	var ignored map[string]any
	if _, err := c.app.DoRequest(req, &ignored); err != nil {
		return fmt.Errorf("AddProjectGroupRole %s/%s: %w", orgID, projectID, err)
	}
	return nil
}
