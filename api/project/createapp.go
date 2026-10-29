package project

import (
	"context"
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
func (c *Client) CreateAppProject(ctx context.Context, orgID, name string) (*Project, error) {
	if orgID == "" || name == "" {
		return nil, fmt.Errorf("project: CreateAppProject requires orgID and name")
	}

	path := "organization/" + url.PathEscape(orgID) + "/project"
	u, err := url.Parse(path)
	if err != nil {
		return nil, fmt.Errorf("project: CreateAppProject: build URL: %w", err)
	}

	body := createAppProjectRequest{Name: name}
	req, err := c.v2.NewRequest(ctx, "POST", u, &body)
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
//
// Providers: the v2 create endpoint accepts github_app and github_server for
// both sources, plus "circleci" for the config source (no repo). When
// ConfigExternalID / CheckoutExternalID is empty the repo object is omitted,
// which is only valid for the "circleci" config provider.
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
	Provider string                    `json:"provider"`
	Repo     *createPipelineSourceRepo `json:"repo,omitempty"`
	FilePath string                    `json:"file_path,omitempty"`
}

type createCheckoutSourceBody struct {
	Provider string                    `json:"provider"`
	Repo     *createPipelineSourceRepo `json:"repo,omitempty"`
}

// sourceRepo returns the repo body for an external ID, or nil when the ID is
// empty (so the "repo" key is omitted, as the "circleci" config provider needs).
func sourceRepo(externalID string) *createPipelineSourceRepo {
	if externalID == "" {
		return nil
	}
	return &createPipelineSourceRepo{ExternalID: externalID}
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
func (c *Client) CreatePipelineDefinition(ctx context.Context, projectID string, spec PipelineDefinitionSpec) (string, error) {
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
			Repo:     sourceRepo(spec.ConfigExternalID),
			FilePath: spec.ConfigFilePath,
		},
		CheckoutSource: createCheckoutSourceBody{
			Provider: spec.CheckoutProvider,
			Repo:     sourceRepo(spec.CheckoutExternalID),
		},
	}

	req, err := c.v2.NewRequest(ctx, "POST", u, &body)
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
// JSON shape per the public API v2 createTrigger schema (additionalProperties
// is false, so any other key is rejected with a 400):
//
//	POST /api/v2/projects/{projUUID}/pipeline-definitions/{defUUID}/triggers
//	Body: {
//	  "event_source": {
//	    "provider": "github_app|github_server|github_oauth|webhook|schedule",
//	    "repo":     {"external_id": "<id>"},                       // github_*
//	    "webhook":  {"sender": "<sender>"},                        // webhook
//	    "schedule": {"cron_expression": "...", "attribution_actor": "current|system"}
//	  },
//	  "event_preset": "<preset>",   // github_*; omitted for webhook/schedule
//	  "checkout_ref": "<ref>",      // required for webhook/schedule
//	  "config_ref":   "<ref>",      // required for webhook/schedule
//	  "event_name":   "<name>",     // required for webhook/schedule only
//	  "disabled": true,
//	  "parameters": {...}           // schedule only
//	}
//	Response 200: {"id": "<triggerUUID>", ...}
//
// The API has no create-time "name" or "description" for a trigger; the name is
// assigned by CircleCI, so those captured fields cannot be recreated.
type TriggerSpec struct {
	Provider    string
	ExternalID  string
	EventPreset string
	Disabled    bool

	// EventName, CheckoutRef and ConfigRef are sent when non-empty. They are
	// required for webhook and schedule triggers.
	EventName   string
	CheckoutRef string
	ConfigRef   string

	// WebhookSender is the optional sender label for a webhook trigger.
	WebhookSender string

	// CronExpression and AttributionActor describe a schedule trigger.
	// AttributionActor must be "current" or "system" (or empty to omit).
	CronExpression   string
	AttributionActor string

	// Parameters are pipeline parameters (schedule triggers only).
	Parameters map[string]any
}

type createTriggerRequest struct {
	EventSource createTriggerEventSource `json:"event_source"`
	EventPreset string                   `json:"event_preset,omitempty"`
	CheckoutRef string                   `json:"checkout_ref,omitempty"`
	ConfigRef   string                   `json:"config_ref,omitempty"`
	EventName   string                   `json:"event_name,omitempty"`
	Disabled    bool                     `json:"disabled"`
	Parameters  map[string]any           `json:"parameters,omitempty"`
}

type createTriggerEventSource struct {
	Provider string                    `json:"provider"`
	Repo     *createPipelineSourceRepo `json:"repo,omitempty"`
	Webhook  *createTriggerWebhook     `json:"webhook,omitempty"`
	Schedule *createTriggerSchedule    `json:"schedule,omitempty"`
}

type createTriggerWebhook struct {
	Sender string `json:"sender,omitempty"`
}

type createTriggerSchedule struct {
	CronExpression   string `json:"cron_expression,omitempty"`
	AttributionActor string `json:"attribution_actor,omitempty"`
}

type createTriggerResponse struct {
	ID          string `json:"id"`
	EventSource struct {
		Webhook struct {
			URL    string `json:"url"`
			Sender string `json:"sender"`
		} `json:"webhook"`
	} `json:"event_source"`
}

// CreatedTrigger is the result of CreateTriggerDetailed.
type CreatedTrigger struct {
	ID string
	// WebhookURL and WebhookSender are set for a provider "webhook" trigger.
	// WebhookURL embeds the server-generated secret (?secret=...); CircleCI
	// returns it unmasked only in the create response — later reads redact it —
	// so it must be treated as a secret and is never logged.
	WebhookURL    string
	WebhookSender string
}

// CreateTrigger creates a trigger on the given pipeline definition.
// projectID and defID must be UUIDs. Returns the new trigger's UUID.
//
// Endpoint: POST /api/v2/projects/{projUUID}/pipeline-definitions/{defUUID}/triggers
// Request body: TriggerSpec fields mapped to the API wire shape.
// Response 200: {"id": "<triggerUUID>", ...}
func (c *Client) CreateTrigger(ctx context.Context, projectID, defID string, spec TriggerSpec) (string, error) {
	t, err := c.CreateTriggerDetailed(ctx, projectID, defID, spec)
	if err != nil {
		return "", err
	}
	return t.ID, nil
}

// CreateTriggerDetailed is CreateTrigger that also returns the webhook URL and
// sender CircleCI generates for a provider "webhook" trigger.
func (c *Client) CreateTriggerDetailed(ctx context.Context, projectID, defID string, spec TriggerSpec) (*CreatedTrigger, error) {
	if projectID == "" || defID == "" {
		return nil, fmt.Errorf("project: CreateTrigger requires projectID and defID")
	}

	path := "projects/" + url.PathEscape(projectID) +
		"/pipeline-definitions/" + url.PathEscape(defID) +
		"/triggers"
	u, err := url.Parse(path)
	if err != nil {
		return nil, fmt.Errorf("project: CreateTrigger: build URL: %w", err)
	}

	src := createTriggerEventSource{Provider: spec.Provider, Repo: sourceRepo(spec.ExternalID)}
	switch spec.Provider {
	case "webhook":
		src.Webhook = &createTriggerWebhook{Sender: spec.WebhookSender}
	case "schedule":
		src.Schedule = &createTriggerSchedule{
			CronExpression:   spec.CronExpression,
			AttributionActor: spec.AttributionActor,
		}
	}
	body := createTriggerRequest{
		EventSource: src,
		EventPreset: spec.EventPreset,
		CheckoutRef: spec.CheckoutRef,
		ConfigRef:   spec.ConfigRef,
		EventName:   spec.EventName,
		Disabled:    spec.Disabled,
		Parameters:  spec.Parameters,
	}

	req, err := c.v2.NewRequest(ctx, "POST", u, &body)
	if err != nil {
		return nil, fmt.Errorf("project: CreateTrigger: build request: %w", err)
	}

	var resp createTriggerResponse
	if _, err := c.v2.DoRequest(req, &resp); err != nil {
		return nil, fmt.Errorf("project: CreateTrigger %s/%s: %w", projectID, defID, err)
	}
	return &CreatedTrigger{
		ID:            resp.ID,
		WebhookURL:    resp.EventSource.Webhook.URL,
		WebhookSender: resp.EventSource.Webhook.Sender,
	}, nil
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
func (c *Client) EnableTrigger(ctx context.Context, projectID, triggerID string) error {
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
	req, err := c.v2.NewRequest(ctx, "PATCH", u, &body)
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
func (c *Client) DeleteProject(ctx context.Context, slug string) error {
	if slug == "" {
		return fmt.Errorf("project: DeleteProject requires slug")
	}

	u, err := slugPath("project/", slug)
	if err != nil {
		return fmt.Errorf("project: DeleteProject: build URL: %w", err)
	}

	req, err := c.v2.NewRequest(ctx, "DELETE", u, nil)
	if err != nil {
		return fmt.Errorf("project: DeleteProject: build request: %w", err)
	}

	if _, err := c.v2.DoRequest(req, nil); err != nil {
		return fmt.Errorf("project: DeleteProject %s: %w", slug, err)
	}
	return nil
}
