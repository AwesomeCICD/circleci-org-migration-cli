package org

import (
	"context"
	"fmt"
	"net/url"
)

// ─────────────────────────────────────────────────────────────────────────────
// OTel exporters
// ─────────────────────────────────────────────────────────────────────────────

// OTelExporter is one OpenTelemetry exporter configuration on an org, as
// returned by GET /api/v2/otel/exporters.
//
// Header values come back redacted as "xxxx" (encrypted at rest) and are
// captured for reference only — they cannot be replayed to a destination org.
// Up to 5 exporters are supported per org.
type OTelExporter struct {
	ID       string            `json:"id"`
	Endpoint string            `json:"endpoint"`
	Protocol string            `json:"protocol"`
	Insecure bool              `json:"insecure"`
	Headers  map[string]string `json:"headers,omitempty"`
}

// GetOTelExporters returns the org's OTel exporter configurations.
//
// Endpoint: GET /api/v2/otel/exporters?org-id={orgID}
//
// Response shape: a JSON array of {id, endpoint, protocol, insecure, headers}.
// Header values are redacted ("xxxx") by the server.
// Each org may have at most 5 exporters.
func (c *Client) GetOTelExporters(ctx context.Context, orgID string) ([]OTelExporter, error) {
	u := &url.URL{
		Path:     "otel/exporters",
		RawQuery: "org-id=" + url.QueryEscape(orgID),
	}

	req, err := c.v2.NewRequest(ctx, "GET", u, nil)
	if err != nil {
		return nil, fmt.Errorf("GetOTelExporters: build request: %w", err)
	}

	var raw []OTelExporter
	if _, err := c.v2.DoRequest(req, &raw); err != nil {
		return nil, fmt.Errorf("GetOTelExporters %s: %w", orgID, err)
	}
	return raw, nil
}

// otelExporterCreateBody is the POST body for CreateOTelExporter.
// Confirmed by the OTel exporters service API: body fields org_id, endpoint,
// protocol, insecure, headers.
type otelExporterCreateBody struct {
	OrgID    string            `json:"org_id"`
	Endpoint string            `json:"endpoint"`
	Protocol string            `json:"protocol"`
	Insecure bool              `json:"insecure"`
	Headers  map[string]string `json:"headers,omitempty"`
}

// CreateOTelExporter adds a new OTel exporter to the org.
//
// Endpoint: POST /api/v2/otel/exporters
//
// Body: {"org_id":"<orgID>","endpoint":"...","protocol":"...","insecure":bool,
// "headers":{...}}.  Each org may have at most 5 exporters.
func (c *Client) CreateOTelExporter(ctx context.Context, orgID, endpoint, protocol string, insecure bool, headers map[string]string) error {
	u := &url.URL{Path: "otel/exporters"}

	body := otelExporterCreateBody{
		OrgID:    orgID,
		Endpoint: endpoint,
		Protocol: protocol,
		Insecure: insecure,
		Headers:  headers,
	}
	req, err := c.v2.NewRequest(ctx, "POST", u, body)
	if err != nil {
		return fmt.Errorf("CreateOTelExporter: build request: %w", err)
	}

	var ignored map[string]any
	if _, err := c.v2.DoRequest(req, &ignored); err != nil {
		return fmt.Errorf("CreateOTelExporter %s: %w", orgID, err)
	}
	return nil
}
