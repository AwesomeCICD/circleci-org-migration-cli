package context

import (
	"fmt"
)

// createContextRequest is the wire format for POST /context.
//
// JSON shape confirmed from:
//   - github.com/CircleCI-Public/circleci-cli api/context/rest.go (CreateContextWithRestParams)
//   - CircleCI API v2 docs: POST /context requires {"name", "owner": {"id", "type"}}
type createContextRequest struct {
	Name  string             `json:"name"`
	Owner createContextOwner `json:"owner"`
}

type createContextOwner struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}

// CreateContext creates a new context in the given organization.
//
// Endpoint: POST /api/v2/context
// Request body: {"name": "<name>", "owner": {"id": "<ownerID>", "type": "organization"}}
// Response: {id, name, created_at} — the same Context struct used by ListContexts.
func (c *Client) CreateContext(name, ownerID string) (*Context, error) {
	if name == "" {
		return nil, fmt.Errorf("context: CreateContext requires name")
	}
	if ownerID == "" {
		return nil, fmt.Errorf("context: CreateContext requires ownerID")
	}

	u, err := c.rest.BaseURL.Parse("context")
	if err != nil {
		return nil, fmt.Errorf("context: CreateContext: build URL: %w", err)
	}

	body := createContextRequest{
		Name: name,
		Owner: createContextOwner{
			ID:   ownerID,
			Type: "organization",
		},
	}

	req, err := c.rest.NewRequest("POST", u, &body)
	if err != nil {
		return nil, fmt.Errorf("context: CreateContext: build request: %w", err)
	}

	var ctx Context
	if _, err := c.rest.DoRequest(req, &ctx); err != nil {
		return nil, fmt.Errorf("context: CreateContext %q: %w", name, err)
	}
	return &ctx, nil
}

// upsertEnvVarRequest is the wire format for PUT /context/{id}/environment-variable/{name}.
//
// JSON shape confirmed from:
//   - github.com/CircleCI-Public/circleci-cli api/context/rest.go (CreateEnvVarWithRest)
//   - CircleCI API v2 docs: PUT body is {"value": "<value>"}
type upsertEnvVarRequest struct {
	Value string `json:"value"`
}

// UpsertEnvVar creates or updates an environment variable in a context.
// This is an idempotent upsert — existing variables are overwritten.
//
// Endpoint: PUT /api/v2/context/{id}/environment-variable/{name}
// Request body: {"value": "<value>"}
func (c *Client) UpsertEnvVar(contextID, name, value string) error {
	if contextID == "" {
		return fmt.Errorf("context: UpsertEnvVar requires contextID")
	}
	if name == "" {
		return fmt.Errorf("context: UpsertEnvVar requires name")
	}

	path := fmt.Sprintf("context/%s/environment-variable/%s", contextID, name)
	u, err := c.rest.BaseURL.Parse(path)
	if err != nil {
		return fmt.Errorf("context: UpsertEnvVar: build URL: %w", err)
	}

	body := upsertEnvVarRequest{Value: value}
	req, err := c.rest.NewRequest("PUT", u, &body)
	if err != nil {
		return fmt.Errorf("context: UpsertEnvVar: build request: %w", err)
	}

	var resp EnvVar
	if _, err := c.rest.DoRequest(req, &resp); err != nil {
		return fmt.Errorf("context: UpsertEnvVar %q/%q: %w", contextID, name, err)
	}
	return nil
}

// createRestrictionRequest is the wire format for POST /context/{id}/restrictions.
//
// JSON shape confirmed from CircleCI API v2 docs:
//
//	POST /context/{context_id}/restrictions
//	Body: {"restriction_type": "project"|"expression"|"group", "restriction_value": "<value>"}
//	Response: 201 Created with {id, name, restriction_type, restriction_value}
type createRestrictionRequest struct {
	Type  string `json:"restriction_type"`
	Value string `json:"restriction_value"`
}

// CreateRestriction creates a project, expression, or group restriction on a context.
//
// Endpoint: POST /api/v2/context/{id}/restrictions
// Request body: {"restriction_type": "<type>", "restriction_value": "<value>"}
// Response: 201 Created with the newly-created Restriction object.
//
// NOTE: Project and expression restrictions are GA (March 2025). Group-type
// restriction writes are NOT yet GA — calls with restriction_type="group" may
// return 4xx until the feature is generally available.
func (c *Client) CreateRestriction(contextID, restrictionType, restrictionValue string) error {
	if contextID == "" {
		return fmt.Errorf("context: CreateRestriction requires contextID")
	}
	if restrictionType == "" {
		return fmt.Errorf("context: CreateRestriction requires restrictionType")
	}

	path := fmt.Sprintf("context/%s/restrictions", contextID)
	u, err := c.rest.BaseURL.Parse(path)
	if err != nil {
		return fmt.Errorf("context: CreateRestriction: build URL: %w", err)
	}

	body := createRestrictionRequest{
		Type:  restrictionType,
		Value: restrictionValue,
	}
	req, err := c.rest.NewRequest("POST", u, &body)
	if err != nil {
		return fmt.Errorf("context: CreateRestriction: build request: %w", err)
	}

	var resp Restriction
	if _, err := c.rest.DoRequest(req, &resp); err != nil {
		return fmt.Errorf("context: CreateRestriction %q/%q: %w", contextID, restrictionType, err)
	}
	return nil
}
