package org

import (
	"context"
	"fmt"
	"net/url"
)

// ─────────────────────────────────────────────────────────────────────────────
// Org technical / security contacts
// ─────────────────────────────────────────────────────────────────────────────

// contactsResponse mirrors GET /api/private/organization/{orgID}/contacts.
//
// Response shape: {"primary":[emails...],"security":[emails...]}.
// Each list may hold up to 5 email addresses.
type contactsResponse struct {
	Primary  []string `json:"primary"`
	Security []string `json:"security"`
}

// GetContacts returns the org's technical (primary) and security contact
// email lists.
//
// Endpoint: GET /api/private/organization/{orgID}/contacts
//
// Uses the private client (circleci.com/api/private/).
// Response: {"primary":[emails],"security":[emails]}. Max 5 per list.
func (c *Client) GetContacts(ctx context.Context, orgID string) (primary, security []string, err error) {
	u, err := url.Parse("organization/" + url.PathEscape(orgID) + "/contacts")
	if err != nil {
		return nil, nil, fmt.Errorf("GetContacts: build URL: %w", err)
	}

	req, err := c.private.NewRequest(ctx, "GET", u, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("GetContacts: build request: %w", err)
	}

	var raw contactsResponse
	if _, err := c.private.DoRequest(req, &raw); err != nil {
		return nil, nil, fmt.Errorf("GetContacts %s: %w", orgID, err)
	}
	return raw.Primary, raw.Security, nil
}

// SetContacts overwrites the org's technical (primary) and security contact
// email lists.
//
// Endpoint: PUT /api/private/organization/{orgID}/contacts
//
// Body: {"primary":[...],"security":[...]}. PUT semantics — the full lists are
// replaced. Max 5 addresses per list.
func (c *Client) SetContacts(ctx context.Context, orgID string, primary, security []string) error {
	u, err := url.Parse("organization/" + url.PathEscape(orgID) + "/contacts")
	if err != nil {
		return fmt.Errorf("SetContacts: build URL: %w", err)
	}

	body := contactsResponse{Primary: primary, Security: security}
	req, err := c.private.NewRequest(ctx, "PUT", u, body)
	if err != nil {
		return fmt.Errorf("SetContacts: build request: %w", err)
	}

	var ignored map[string]any
	if _, err := c.private.DoRequest(req, &ignored); err != nil {
		return fmt.Errorf("SetContacts %s: %w", orgID, err)
	}
	return nil
}
