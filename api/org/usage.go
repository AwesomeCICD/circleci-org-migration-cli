package org

import (
	"context"
	"fmt"
	"net/url"
)

// ─────────────────────────────────────────────────────────────────────────────
// Usage export job
// ─────────────────────────────────────────────────────────────────────────────

// usageExportJobCreateRequest is the request body for POST
// /api/v2/organizations/{orgID}/usage_export_job.
type usageExportJobCreateRequest struct {
	Start        string   `json:"start"`
	End          string   `json:"end"`
	SharedOrgIDs []string `json:"shared_org_ids,omitempty"`
}

// usageExportJobResponse is the common shape returned by both the create and
// status endpoints:
//
//	POST /api/v2/organizations/{orgID}/usage_export_job
//	GET  /api/v2/organizations/{orgID}/usage_export_job/{jobID}
type usageExportJobResponse struct {
	UsageExportJobID string   `json:"usage_export_job_id"`
	State            string   `json:"state"`
	DownloadURLs     []string `json:"download_urls,omitempty"`
}

// usageExportJobPath returns the URL for the create endpoint.
func usageExportJobPath(orgID string) (*url.URL, error) {
	return url.Parse("organizations/" + url.PathEscape(orgID) + "/usage_export_job")
}

// usageExportJobStatusPath returns the URL for the status/download endpoint.
func usageExportJobStatusPath(orgID, jobID string) (*url.URL, error) {
	return url.Parse("organizations/" + url.PathEscape(orgID) + "/usage_export_job/" + url.PathEscape(jobID))
}

// CreateUsageExportJob submits an async usage-export job for the given org and
// time window.  start and end must be RFC 3339 timestamps; the window may not
// exceed 31 days (enforced by the API).
//
// Endpoint: POST https://circleci.com/api/v2/organizations/{orgID}/usage_export_job
func (c *Client) CreateUsageExportJob(ctx context.Context, orgID, start, end string) (jobID string, err error) {
	u, err := usageExportJobPath(orgID)
	if err != nil {
		return "", fmt.Errorf("CreateUsageExportJob: build URL: %w", err)
	}

	body := usageExportJobCreateRequest{Start: start, End: end}
	req, err := c.v2.NewRequest(ctx, "POST", u, body)
	if err != nil {
		return "", fmt.Errorf("CreateUsageExportJob: build request: %w", err)
	}

	var resp usageExportJobResponse
	if _, err := c.v2.DoRequest(req, &resp); err != nil {
		return "", fmt.Errorf("CreateUsageExportJob %s: %w", orgID, err)
	}
	return resp.UsageExportJobID, nil
}

// GetUsageExportJob returns the current state and, when completed, the
// pre-signed download URLs for the given export job.
//
// Endpoint: GET https://circleci.com/api/v2/organizations/{orgID}/usage_export_job/{jobID}
func (c *Client) GetUsageExportJob(ctx context.Context, orgID, jobID string) (state string, downloadURLs []string, err error) {
	u, err := usageExportJobStatusPath(orgID, jobID)
	if err != nil {
		return "", nil, fmt.Errorf("GetUsageExportJob: build URL: %w", err)
	}

	req, err := c.v2.NewRequest(ctx, "GET", u, nil)
	if err != nil {
		return "", nil, fmt.Errorf("GetUsageExportJob: build request: %w", err)
	}

	var resp usageExportJobResponse
	if _, err := c.v2.DoRequest(req, &resp); err != nil {
		return "", nil, fmt.Errorf("GetUsageExportJob %s/%s: %w", orgID, jobID, err)
	}
	return resp.State, resp.DownloadURLs, nil
}
