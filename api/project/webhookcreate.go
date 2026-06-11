package project

import (
	"context"
	"fmt"
	"net/url"
)

// createWebhookRequest is the wire format for POST /api/v2/webhook.
//
// JSON shape confirmed from:
//   - https://circleci.com/docs/api/v2/index.html (createWebhook request body)
//
// The scope object identifies the project by UUID ("id") and type "project".
// signing-secret is REQUIRED by the API but the source value is never
// readable via the API, so we always send an empty string.
type createWebhookRequest struct {
	Name          string             `json:"name"`
	Events        []string           `json:"events"`
	URL           string             `json:"url"`
	VerifyTLS     bool               `json:"verify-tls"`
	SigningSecret string             `json:"signing-secret"`
	Scope         webhookScopeObject `json:"scope"`
}

// webhookScopeObject is the project scope for webhook creation.
type webhookScopeObject struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}

// CreateWebhook creates an outbound webhook scoped to the given destination
// project UUID.
//
// Endpoint: POST /api/v2/webhook
// Request body:
//
//	{"name":"...","events":[...],"url":"...","verify-tls":bool,
//	 "signing-secret":"","scope":{"id":"<destProjectID>","type":"project"}}
//
// signing-secret is REQUIRED by the API but is never readable from the source
// project, so we always send an empty string.  The operator must set the real
// HMAC secret on the destination webhook manually.
func (c *Client) CreateWebhook(ctx context.Context, destProjectID string, w Webhook) error {
	if destProjectID == "" {
		return fmt.Errorf("project: CreateWebhook requires destProjectID")
	}

	verifyTLS := false
	if w.VerifyTLS != nil {
		verifyTLS = *w.VerifyTLS
	}

	body := createWebhookRequest{
		Name:          w.Name,
		Events:        w.Events,
		URL:           w.URL,
		VerifyTLS:     verifyTLS,
		SigningSecret: "",
		Scope: webhookScopeObject{
			ID:   destProjectID,
			Type: "project",
		},
	}

	u := &url.URL{Path: "webhook"}
	req, err := c.v2.NewRequest(ctx, "POST", u, &body)
	if err != nil {
		return fmt.Errorf("project: CreateWebhook: build request: %w", err)
	}

	var ignored map[string]any
	if _, err := c.v2.DoRequest(req, &ignored); err != nil {
		return fmt.Errorf("project: CreateWebhook %q: %w", w.Name, err)
	}
	return nil
}
