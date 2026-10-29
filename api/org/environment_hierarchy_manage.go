package org

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/AwesomeCICD/circleci-org-migration-cli/api/rest"
)

// Private release-tracker BFF endpoints used to read and recreate environment
// hierarchies and the environments (deploy integrations) their levels reference.
//
// Confirmed from release-tracker internal/api/bff.go and
// internal/api/bff/environment_hierarchy_handler.go:
//
//	GET    /private/release-tracker/v1/environment?org-id=             list environments
//	POST   /private/release-tracker/v1/integration                      create an environment
//	GET    /private/release-tracker/v1/environment-hierarchy?org-id=    list hierarchies
//	POST   /private/release-tracker/v1/environment-hierarchy?org-id=    create (201)
//	GET    /private/release-tracker/v1/environment-hierarchy/{id}       detail + assigned ids
//	GET    /private/release-tracker/v1/environment-hierarchy/{id}/levels
//	PUT    /private/release-tracker/v1/environment-hierarchy-assignment (204)
//
// The BFF answers "permission denied" with 404, so callers treat 401/403/404 on
// a write as "this token cannot write" (see IsBFFWriteDenied).

const bffBase = "private/release-tracker/v1/"

// bffPageSize is the page size requested from the paginated BFF lists.
const bffPageSize = 100

// BFFEnvironment is an environment (deploy integration) as listed by the BFF.
// ID is the integration UUID that hierarchy levels reference.
type BFFEnvironment struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// EnvHierarchySummary is one entry of the hierarchy list.
type EnvHierarchySummary struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// EnvHierarchyLevelRef is a hierarchy level with its integration reference.
type EnvHierarchyLevelRef struct {
	Position        int    `json:"position"`
	IntegrationID   string `json:"integration_id"`
	IntegrationName string `json:"integration_name"`
}

// EnvHierarchyDetail is a hierarchy with the projects/components it is assigned to.
type EnvHierarchyDetail struct {
	ID                   string                 `json:"id"`
	Name                 string                 `json:"name"`
	Description          string                 `json:"description"`
	Levels               []EnvHierarchyLevelRef `json:"levels"`
	AssignedProjectIDs   []string               `json:"assigned_project_ids"`
	AssignedComponentIDs []string               `json:"assigned_component_ids"`
}

// EnvHierarchyCreate is the body of a hierarchy create.
type EnvHierarchyCreate struct {
	Name        string
	Description string
	Levels      []EnvHierarchyLevelRef // Position + IntegrationID are sent
	IsDefault   bool
}

// IsBFFWriteDenied reports whether err is the HTTP 401/403/404 the BFF returns
// when the caller may not write (404 is its "permission denied").
func IsBFFWriteDenied(err error) bool {
	var he *rest.HTTPError
	if !errors.As(err, &he) {
		return false
	}
	return he.Code == http.StatusUnauthorized || he.Code == http.StatusForbidden || he.Code == http.StatusNotFound
}

// IsBFFConflict reports whether err is an HTTP 409 (duplicate name/position/integration).
func IsBFFConflict(err error) bool {
	var he *rest.HTTPError
	return errors.As(err, &he) && he.Code == http.StatusConflict
}

func bffURL(path string, q url.Values) *url.URL {
	u := &url.URL{Path: bffBase + path}
	if len(q) > 0 {
		u.RawQuery = q.Encode()
	}
	return u
}

type bffPage[T any] struct {
	Items         []T    `json:"items"`
	NextPageToken string `json:"next_page_token"`
}

// bffList pages through a BFF list endpoint.
func bffList[T any](ctx context.Context, c *Client, path, orgUUID string) ([]T, error) {
	var out []T
	token := ""
	for {
		q := url.Values{"org-id": {orgUUID}, "page-size": {strconv.Itoa(bffPageSize)}}
		if token != "" {
			q.Set("page-token", token)
		}
		req, err := c.app.NewRequest(ctx, "GET", bffURL(path, q), nil)
		if err != nil {
			return nil, fmt.Errorf("build request: %w", err)
		}
		var page bffPage[T]
		if _, err := c.app.DoRequest(req, &page); err != nil {
			return nil, err
		}
		out = append(out, page.Items...)
		if page.NextPageToken == "" || len(page.Items) == 0 {
			return out, nil
		}
		token = page.NextPageToken
	}
}

// ListBFFEnvironments lists the org's environments (deploy integrations).
func (c *Client) ListBFFEnvironments(ctx context.Context, orgUUID string) ([]BFFEnvironment, error) {
	envs, err := bffList[BFFEnvironment](ctx, c, "environment", orgUUID)
	if err != nil {
		return nil, fmt.Errorf("ListBFFEnvironments %s: %w", orgUUID, err)
	}
	return envs, nil
}

// CreateEnvironment creates an environment (deploy integration) and returns its
// UUID. integrationType is one of KUBERNETES_CLUSTER, APPLE_DEVELOPER_ACCOUNT,
// GOOGLE_DEVELOPER_ACCOUNT or CUSTOM. The server discards settings (it stores
// "{}"), so none are sent.
func (c *Client) CreateEnvironment(ctx context.Context, orgUUID, name, integrationType, description string) (string, error) {
	body := map[string]any{
		"name":             name,
		"organization_id":  orgUUID,
		"integration_type": integrationType,
		"description":      description,
	}
	req, err := c.app.NewRequest(ctx, "POST", bffURL("integration", nil), body)
	if err != nil {
		return "", fmt.Errorf("CreateEnvironment: build request: %w", err)
	}
	var resp struct {
		ID string `json:"id"`
	}
	if _, err := c.app.DoRequest(req, &resp); err != nil {
		return "", fmt.Errorf("CreateEnvironment %q: %w", name, err)
	}
	return resp.ID, nil
}

// ListEnvironmentHierarchies lists every hierarchy in the org.
func (c *Client) ListEnvironmentHierarchies(ctx context.Context, orgUUID string) ([]EnvHierarchySummary, error) {
	hs, err := bffList[EnvHierarchySummary](ctx, c, "environment-hierarchy", orgUUID)
	if err != nil {
		return nil, fmt.Errorf("ListEnvironmentHierarchies %s: %w", orgUUID, err)
	}
	return hs, nil
}

// GetEnvironmentHierarchyDetail returns one hierarchy including the project and
// component ids it is assigned to.
func (c *Client) GetEnvironmentHierarchyDetail(ctx context.Context, id string) (*EnvHierarchyDetail, error) {
	req, err := c.app.NewRequest(ctx, "GET", bffURL("environment-hierarchy/"+url.PathEscape(id), nil), nil)
	if err != nil {
		return nil, fmt.Errorf("GetEnvironmentHierarchyDetail: build request: %w", err)
	}
	var d EnvHierarchyDetail
	if _, err := c.app.DoRequest(req, &d); err != nil {
		return nil, fmt.Errorf("GetEnvironmentHierarchyDetail %s: %w", id, err)
	}
	return &d, nil
}

// ListEnvironmentHierarchyLevels returns the ordered levels of a hierarchy, with
// integration ids and names.
func (c *Client) ListEnvironmentHierarchyLevels(ctx context.Context, id string) ([]EnvHierarchyLevelRef, error) {
	req, err := c.app.NewRequest(ctx, "GET", bffURL("environment-hierarchy/"+url.PathEscape(id)+"/levels", nil), nil)
	if err != nil {
		return nil, fmt.Errorf("ListEnvironmentHierarchyLevels: build request: %w", err)
	}
	var resp struct {
		Items []EnvHierarchyLevelRef `json:"items"`
	}
	if _, err := c.app.DoRequest(req, &resp); err != nil {
		return nil, fmt.Errorf("ListEnvironmentHierarchyLevels %s: %w", id, err)
	}
	return resp.Items, nil
}

// ResolveOrgEnvironmentHierarchy resolves the ORGANIZATION-level default
// hierarchy (no project/component given). It returns the resolved scope
// ("ORGANIZATION" or "NONE") and the hierarchy id and name when one is set.
func (c *Client) ResolveOrgEnvironmentHierarchy(ctx context.Context, orgUUID string) (scope, id, name string, err error) {
	q := url.Values{"org-id": {orgUUID}}
	req, rerr := c.app.NewRequest(ctx, "GET", bffURL("environment-hierarchy/resolve", q), nil)
	if rerr != nil {
		return "", "", "", fmt.Errorf("ResolveOrgEnvironmentHierarchy: build request: %w", rerr)
	}
	var resp struct {
		ResolvedScope string `json:"resolved_scope"`
		Hierarchy     *struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"hierarchy"`
	}
	if _, derr := c.app.DoRequest(req, &resp); derr != nil {
		return "", "", "", fmt.Errorf("ResolveOrgEnvironmentHierarchy %s: %w", orgUUID, derr)
	}
	if resp.Hierarchy == nil {
		return resp.ResolvedScope, "", "", nil
	}
	return resp.ResolvedScope, resp.Hierarchy.ID, resp.Hierarchy.Name, nil
}

// CreateEnvironmentHierarchy creates a hierarchy and returns its id (HTTP 201).
// A duplicate name, position or integration is HTTP 409; an unknown integration
// is HTTP 400.
func (c *Client) CreateEnvironmentHierarchy(ctx context.Context, orgUUID string, in EnvHierarchyCreate) (string, error) {
	type level struct {
		Position      int    `json:"position"`
		IntegrationID string `json:"integration_id"`
	}
	levels := make([]level, 0, len(in.Levels))
	for _, l := range in.Levels {
		levels = append(levels, level{Position: l.Position, IntegrationID: l.IntegrationID})
	}
	body := map[string]any{
		"name":        in.Name,
		"description": in.Description,
		"levels":      levels,
		"is_default":  in.IsDefault,
	}
	req, err := c.app.NewRequest(ctx, "POST", bffURL("environment-hierarchy", url.Values{"org-id": {orgUUID}}), body)
	if err != nil {
		return "", fmt.Errorf("CreateEnvironmentHierarchy: build request: %w", err)
	}
	var resp struct {
		ID string `json:"id"`
	}
	if _, err := c.app.DoRequest(req, &resp); err != nil {
		return "", fmt.Errorf("CreateEnvironmentHierarchy %q: %w", in.Name, err)
	}
	return resp.ID, nil
}

// UpsertEnvironmentHierarchyAssignment assigns a hierarchy to entities of one
// type: "ORGANIZATION" (exactly one id), "PROJECT" or "COMPONENT". An entity
// that already has an assignment is re-pointed at this hierarchy.
func (c *Client) UpsertEnvironmentHierarchyAssignment(ctx context.Context, hierarchyID, entityType string, entityIDs []string) error {
	body := map[string]any{
		"hierarchy_id": hierarchyID,
		"entity_type":  entityType,
		"entity_ids":   entityIDs,
	}
	req, err := c.app.NewRequest(ctx, "PUT", bffURL("environment-hierarchy-assignment", nil), body)
	if err != nil {
		return fmt.Errorf("UpsertEnvironmentHierarchyAssignment: build request: %w", err)
	}
	if _, err := c.app.DoRequest(req, nil); err != nil {
		return fmt.Errorf("UpsertEnvironmentHierarchyAssignment %s/%s: %w", hierarchyID, entityType, err)
	}
	return nil
}
