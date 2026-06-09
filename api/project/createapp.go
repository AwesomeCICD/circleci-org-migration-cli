package project

import (
	"fmt"
	"net/url"
)

// createAppProjectRequest is the wire format for
// POST /api/v2/organization/{orgID}/project (GitHub App / circleci-type orgs).
//
// JSON shape confirmed from live API:
//
//	POST /api/v2/organization/{orgUUID}/project
//	Body: {"name": "<name>"}
//	Response 200: {"id","slug":"circleci/<orgUUID>/<projUUID>","organization_id",...}
//
// Note: unlike the OAuth path (POST /organization/{provider}/{org}/project which
// has TWO path segments before /project), the App path takes a BARE org UUID as a
// single path segment.
type createAppProjectRequest struct {
	Name string `json:"name"`
}

// CreateAppProject creates a GitHub App project in the destination org by name.
// orgID must be the bare org UUID (not a slug).
//
// Endpoint: POST /api/v2/organization/{orgID}/project
// Request body: {"name": "<name>"}
// Response 200: Project JSON (id, name, slug:"circleci/<orgUUID>/<projUUID>", organization_id, ...)
func (c *Client) CreateAppProject(orgID, name string) (*Project, error) {
	if orgID == "" || name == "" {
		return nil, fmt.Errorf("project: CreateAppProject requires orgID and name")
	}

	path := "organization/" + url.PathEscape(orgID) + "/project"
	u, err := url.Parse(path)
	if err != nil {
		return nil, fmt.Errorf("project: CreateAppProject: build URL: %w", err)
	}

	body := createAppProjectRequest{Name: name}
	req, err := c.v2.NewRequest("POST", u, &body)
	if err != nil {
		return nil, fmt.Errorf("project: CreateAppProject: build request: %w", err)
	}

	var p Project
	if _, err := c.v2.DoRequest(req, &p); err != nil {
		return nil, fmt.Errorf("project: CreateAppProject %s/%s: %w", orgID, name, err)
	}
	return &p, nil
}

// PipelineDefinitionSpec holds the parameters required to create a new pipeline
// definition on a GitHub App project.
//
// JSON shape confirmed from live API:
//
//	POST /api/v2/projects/{projUUID}/pipeline-definitions
//	Body: {
//	  "name": "...",
//	  "description": "..." (omitempty),
//	  "config_source": {
//	    "provider": "github_app",
//	    "repo": {"external_id": "<id>"},
//	    "file_path": ".circleci/config.yml"
//	  },
//	  "checkout_source": {
//	    "provider": "github_app",
//	    "repo": {"external_id": "<id>"}
//	  }
//	}
//	Response 200: {"id": "<defUUID>", ...}
type PipelineDefinitionSpec struct {
	Name               string
	Description        string
	ConfigProvider     string
	ConfigExternalID   string
	ConfigFilePath     string
	CheckoutProvider   string
	CheckoutExternalID string
}

type createPipelineDefinitionRequest struct {
	Name           string                   `json:"name"`
	Description    string                   `json:"description,omitempty"`
	ConfigSource   createPipelineSourceBody `json:"config_source"`
	CheckoutSource createCheckoutSourceBody `json:"checkout_source"`
}

type createPipelineSourceBody struct {
	Provider string                   `json:"provider"`
	Repo     createPipelineSourceRepo `json:"repo"`
	FilePath string                   `json:"file_path,omitempty"`
}

type createCheckoutSourceBody struct {
	Provider string                   `json:"provider"`
	Repo     createPipelineSourceRepo `json:"repo"`
}

type createPipelineSourceRepo struct {
	ExternalID string `json:"external_id"`
}

type createPipelineDefinitionResponse struct {
	ID string `json:"id"`
}

// CreatePipelineDefinition creates a pipeline definition on the given project.
// projectID must be the project UUID. Returns the new definition's UUID.
//
// Endpoint: POST /api/v2/projects/{projUUID}/pipeline-definitions
// Request body: PipelineDefinitionSpec fields mapped to the API wire shape.
// Response 200: {"id": "<defUUID>", ...}
func (c *Client) CreatePipelineDefinition(projectID string, spec PipelineDefinitionSpec) (string, error) {
	if projectID == "" {
		return "", fmt.Errorf("project: CreatePipelineDefinition requires projectID")
	}
	if spec.Name == "" {
		return "", fmt.Errorf("project: CreatePipelineDefinition requires spec.Name")
	}

	path := "projects/" + url.PathEscape(projectID) + "/pipeline-definitions"
	u, err := url.Parse(path)
	if err != nil {
		return "", fmt.Errorf("project: CreatePipelineDefinition: build URL: %w", err)
	}

	body := createPipelineDefinitionRequest{
		Name:        spec.Name,
		Description: spec.Description,
		ConfigSource: createPipelineSourceBody{
			Provider: spec.ConfigProvider,
			Repo:     createPipelineSourceRepo{ExternalID: spec.ConfigExternalID},
			FilePath: spec.ConfigFilePath,
		},
		CheckoutSource: createCheckoutSourceBody{
			Provider: spec.CheckoutProvider,
			Repo:     createPipelineSourceRepo{ExternalID: spec.CheckoutExternalID},
		},
	}

	req, err := c.v2.NewRequest("POST", u, &body)
	if err != nil {
		return "", fmt.Errorf("project: CreatePipelineDefinition: build request: %w", err)
	}

	var resp createPipelineDefinitionResponse
	if _, err := c.v2.DoRequest(req, &resp); err != nil {
		return "", fmt.Errorf("project: CreatePipelineDefinition %s/%s: %w", projectID, spec.Name, err)
	}
	return resp.ID, nil
}

// TriggerSpec holds the parameters required to create a new trigger on a
// pipeline definition.
//
// JSON shape confirmed from live API:
//
//	POST /api/v2/projects/{projUUID}/pipeline-definitions/{defUUID}/triggers
//	Body: {
//	  "event_source": {"provider": "github_app", "repo": {"external_id": "<id>"}},
//	  "event_preset": "<preset>",
//	  "disabled": true
//	}
//	Response 200: {"id": "<triggerUUID>", ...}
type TriggerSpec struct {
	Provider    string
	ExternalID  string
	EventPreset string
	Disabled    bool
}

type createTriggerRequest struct {
	EventSource createTriggerEventSource `json:"event_source"`
	EventPreset string                   `json:"event_preset"`
	Disabled    bool                     `json:"disabled"`
}

type createTriggerEventSource struct {
	Provider string                   `json:"provider"`
	Repo     createPipelineSourceRepo `json:"repo"`
}

type createTriggerResponse struct {
	ID string `json:"id"`
}

// CreateTrigger creates a trigger on the given pipeline definition.
// projectID and defID must be UUIDs. Returns the new trigger's UUID.
//
// Endpoint: POST /api/v2/projects/{projUUID}/pipeline-definitions/{defUUID}/triggers
// Request body: TriggerSpec fields mapped to the API wire shape.
// Response 200: {"id": "<triggerUUID>", ...}
func (c *Client) CreateTrigger(projectID, defID string, spec TriggerSpec) (string, error) {
	if projectID == "" || defID == "" {
		return "", fmt.Errorf("project: CreateTrigger requires projectID and defID")
	}

	path := "projects/" + url.PathEscape(projectID) +
		"/pipeline-definitions/" + url.PathEscape(defID) +
		"/triggers"
	u, err := url.Parse(path)
	if err != nil {
		return "", fmt.Errorf("project: CreateTrigger: build URL: %w", err)
	}

	body := createTriggerRequest{
		EventSource: createTriggerEventSource{
			Provider: spec.Provider,
			Repo:     createPipelineSourceRepo{ExternalID: spec.ExternalID},
		},
		EventPreset: spec.EventPreset,
		Disabled:    spec.Disabled,
	}

	req, err := c.v2.NewRequest("POST", u, &body)
	if err != nil {
		return "", fmt.Errorf("project: CreateTrigger: build request: %w", err)
	}

	var resp createTriggerResponse
	if _, err := c.v2.DoRequest(req, &resp); err != nil {
		return "", fmt.Errorf("project: CreateTrigger %s/%s: %w", projectID, defID, err)
	}
	return resp.ID, nil
}

// enableTriggerRequest is the wire format for
// PATCH /api/v2/projects/{projUUID}/triggers/{triggerUUID}.
//
// JSON shape confirmed from live API:
//
//	PATCH /api/v2/projects/{projUUID}/triggers/{triggerUUID}
//	Body: {"disabled": false}
//	Response 200: trigger JSON
type enableTriggerRequest struct {
	Disabled bool `json:"disabled"`
}

// EnableTrigger unpauses (enables) a previously-disabled trigger by setting
// disabled=false.
//
// Endpoint: PATCH /api/v2/projects/{projUUID}/triggers/{triggerUUID}
// Request body: {"disabled": false}
// Response 200: trigger JSON (ignored)
func (c *Client) EnableTrigger(projectID, triggerID string) error {
	if projectID == "" || triggerID == "" {
		return fmt.Errorf("project: EnableTrigger requires projectID and triggerID")
	}

	path := "projects/" + url.PathEscape(projectID) +
		"/triggers/" + url.PathEscape(triggerID)
	u, err := url.Parse(path)
	if err != nil {
		return fmt.Errorf("project: EnableTrigger: build URL: %w", err)
	}

	body := enableTriggerRequest{Disabled: false}
	req, err := c.v2.NewRequest("PATCH", u, &body)
	if err != nil {
		return fmt.Errorf("project: EnableTrigger: build request: %w", err)
	}

	if _, err := c.v2.DoRequest(req, nil); err != nil {
		return fmt.Errorf("project: EnableTrigger %s/%s: %w", projectID, triggerID, err)
	}
	return nil
}

// DeleteProject deletes the project identified by slug.
//
// Endpoint: DELETE /api/v2/project/{slug}
// Response 200: empty body
//
// This is primarily used for rollback / cleanup in tests and tooling.
func (c *Client) DeleteProject(slug string) error {
	if slug == "" {
		return fmt.Errorf("project: DeleteProject requires slug")
	}

	u, err := slugPath("project/", slug)
	if err != nil {
		return fmt.Errorf("project: DeleteProject: build URL: %w", err)
	}

	req, err := c.v2.NewRequest("DELETE", u, nil)
	if err != nil {
		return fmt.Errorf("project: DeleteProject: build request: %w", err)
	}

	if _, err := c.v2.DoRequest(req, nil); err != nil {
		return fmt.Errorf("project: DeleteProject %s: %w", slug, err)
	}
	return nil
}
