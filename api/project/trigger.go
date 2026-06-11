package project

import (
	"context"
	"fmt"
	"net/url"
)

// TriggerEventSourceRepo holds the repository identity for a trigger event
// source whose provider is github_app, github_server, or github_oauth.
type TriggerEventSourceRepo struct {
	FullName   string `json:"full_name,omitempty"`
	ExternalID string `json:"external_id,omitempty"`
}

// TriggerEventSourceWebhook holds webhook metadata for a trigger event source
// whose provider is "webhook".  The URL field is intentionally omitted from
// capture (it contains a ?secret=**REDACTED** query parameter that could leak
// the redaction mask in logs); only the sender identity is recorded.
//
// JSON field names confirmed from live HTTP 200 response of:
//
//	GET /api/v2/projects/{projectID}/pipeline-definitions/{defID}/triggers
type TriggerEventSourceWebhook struct {
	URL    string `json:"url,omitempty"`    // NOT stored in manifest; see TriggerEventSource
	Sender string `json:"sender,omitempty"` // captured as WebhookSender
}

// TriggerEventSourceSchedule holds schedule metadata for a trigger event source
// whose provider is "schedule".
//
// JSON field names confirmed from live HTTP 200 response.
type TriggerEventSourceSchedule struct {
	CronExpression   string `json:"cron_expression,omitempty"`
	AttributionActor string `json:"attribution_actor,omitempty"`
}

// TriggerEventSource is the discriminated-union event-source for a trigger.
// provider is one of: github_app | github_server | github_oauth | webhook | schedule.
//
// JSON field names confirmed from live HTTP 200 response.
type TriggerEventSource struct {
	Provider string                     `json:"provider"`
	Repo     TriggerEventSourceRepo     `json:"repo,omitempty"`
	Webhook  TriggerEventSourceWebhook  `json:"webhook,omitempty"`
	Schedule TriggerEventSourceSchedule `json:"schedule,omitempty"`
}

// Trigger represents one pipeline trigger as returned by
// GET /api/v2/projects/{projectID}/pipeline-definitions/{defID}/triggers.
//
// JSON field names confirmed from live HTTP 200 response.
type Trigger struct {
	ID          string             `json:"id"`
	Name        string             `json:"name"`
	EventName   string             `json:"event_name,omitempty"`
	Description string             `json:"description,omitempty"`
	CreatedAt   string             `json:"created_at,omitempty"`
	CheckoutRef string             `json:"checkout_ref,omitempty"`
	ConfigRef   string             `json:"config_ref,omitempty"`
	EventPreset string             `json:"event_preset,omitempty"`
	Disabled    bool               `json:"disabled"`
	EventSource TriggerEventSource `json:"event_source"`
}

type listTriggersResponse struct {
	Items         []Trigger `json:"items"`
	NextPageToken string    `json:"next_page_token"`
}

// ListTriggers returns all triggers for the given pipeline definition, fetching
// all pages automatically.
//
// Endpoint: GET /api/v2/projects/{projectID}/pipeline-definitions/{defID}/triggers
//
// projectID and defID must be UUIDs.  The event_source field is a union
// discriminated by provider; callers should inspect EventSource.Provider to
// determine which sub-fields are populated.
func (c *Client) ListTriggers(ctx context.Context, projectID, defID string) ([]Trigger, error) {
	var all []Trigger
	pageToken := ""

	for {
		path := "projects/" + url.PathEscape(projectID) +
			"/pipeline-definitions/" + url.PathEscape(defID) +
			"/triggers"
		u, err := url.Parse(path)
		if err != nil {
			return nil, fmt.Errorf("ListTriggers: build URL: %w", err)
		}
		if pageToken != "" {
			q := url.Values{}
			q.Set("page-token", pageToken)
			u.RawQuery = q.Encode()
		}

		req, err := c.v2.NewRequest(ctx, "GET", u, nil)
		if err != nil {
			return nil, fmt.Errorf("ListTriggers: build request: %w", err)
		}

		var resp listTriggersResponse
		if _, err := c.v2.DoRequest(req, &resp); err != nil {
			return nil, fmt.Errorf("ListTriggers %q/%q: %w", projectID, defID, err)
		}

		all = append(all, resp.Items...)

		if resp.NextPageToken == "" {
			break
		}
		pageToken = resp.NextPageToken
	}

	return all, nil
}
