package project

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// ─────────────────────────────────────────────────────────────────────────────
// TriggerPipelineRun
// ─────────────────────────────────────────────────────────────────────────────

// triggerPipelineRunRequest is the JSON body for
// POST /api/v2/project/{provider}/{org}/{project}/pipeline/run.
//
// The config sub-object carries the inline config content that overrides the
// repository config when api-trigger-with-config is enabled for the project.
// definition_id selects the pipeline definition (UUID from
// ListPipelineDefinitions). checkout.branch controls what code is checked out.
//
// JSON shape confirmed from live API:
//
//	{"definition_id":"<uuid>","config":{"branch":"<branch>","content":"<yaml>"},
//	 "checkout":{"branch":"<branch>"},"parameters":{...}}
type triggerPipelineRunRequest struct {
	DefinitionID string            `json:"definition_id"`
	Config       triggerConfigBody `json:"config"`
	Checkout     triggerCheckout   `json:"checkout"`
	Parameters   map[string]any    `json:"parameters,omitempty"`
}

type triggerConfigBody struct {
	Branch  string `json:"branch"`
	Content string `json:"content"`
}

type triggerCheckout struct {
	Branch string `json:"branch"`
}

// triggerPipelineRunResponse is the 201 body returned by the trigger endpoint.
type triggerPipelineRunResponse struct {
	ID     string `json:"id"`
	Number int    `json:"number"`
}

// TriggerPipelineRun triggers an unversioned pipeline run for the project
// identified by its slug. The run uses an inline config (content) rather than
// the repo's committed config, which requires api-trigger-with-config to be
// enabled on the project.
//
// Endpoint: POST /api/v2/project/{provider}/{org}/{project}/pipeline/run
//
// Returns the pipeline UUID on HTTP 201. On HTTP 200 the API reports a skipped
// run; TriggerPipelineRun returns ("", nil) with an ErrPipelineSkipped sentinel.
// params may be nil.
func (c *Client) TriggerPipelineRun(slug, definitionID, branch, configYAML string, params map[string]any) (string, error) {
	u, err := slugSubresource(slug, "pipeline/run")
	if err != nil {
		return "", fmt.Errorf("TriggerPipelineRun: %w", err)
	}

	body := triggerPipelineRunRequest{
		DefinitionID: definitionID,
		Config: triggerConfigBody{
			Branch:  branch,
			Content: configYAML,
		},
		Checkout: triggerCheckout{Branch: branch},
	}
	if len(params) > 0 {
		body.Parameters = params
	}

	req, err := c.v2.NewRequest("POST", u, body)
	if err != nil {
		return "", fmt.Errorf("TriggerPipelineRun: build request: %w", err)
	}

	// The trigger endpoint returns 201 on success and 200 when the run is
	// skipped. DoRequest treats any status < 400 as success when resp is
	// non-nil, so we must distinguish them by the fields present.
	var created triggerPipelineRunResponse
	code, err := c.v2.DoRequest(req, &created)
	if err != nil {
		return "", fmt.Errorf("TriggerPipelineRun %q: %w", slug, err)
	}

	if code == http.StatusOK {
		// The run was skipped; no pipeline ID available.
		return "", ErrPipelineSkipped
	}

	if created.ID == "" {
		return "", fmt.Errorf("TriggerPipelineRun %q: response had no pipeline id (status %d)", slug, code)
	}
	return created.ID, nil
}

// ErrPipelineSkipped is returned by TriggerPipelineRun when the API responds
// with HTTP 200, indicating the run was accepted but not started (e.g. because
// the inline config produced no differences from a cached result). Callers
// should treat this as a non-fatal signal and decide whether to retry or abort.
var ErrPipelineSkipped = fmt.Errorf("pipeline run skipped by server (HTTP 200)")

// ─────────────────────────────────────────────────────────────────────────────
// GetPipelineWorkflows
// ─────────────────────────────────────────────────────────────────────────────

// Workflow holds the identifier and current status of a pipeline workflow as
// returned by GET /api/v2/pipeline/{pipeline-id}/workflow.
//
// JSON shape confirmed from live API:
//
//	{"items":[{"id":"<uuid>","name":"<name>","status":"<status>",...}],...}
type Workflow struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

type listWorkflowsResponse struct {
	Items         []Workflow `json:"items"`
	NextPageToken string     `json:"next_page_token"`
}

// GetPipelineWorkflows returns all workflows for a given pipeline UUID.
//
// Endpoint: GET /api/v2/pipeline/{pipeline-id}/workflow
//
// Terminal statuses are: success | failed | error | canceled.
// The poll loop in internal/extract uses those to detect completion.
func (c *Client) GetPipelineWorkflows(pipelineID string) ([]Workflow, error) {
	var all []Workflow
	pageToken := ""

	for {
		path := "pipeline/" + url.PathEscape(pipelineID) + "/workflow"
		u, err := url.Parse(path)
		if err != nil {
			return nil, fmt.Errorf("GetPipelineWorkflows: build URL: %w", err)
		}
		if pageToken != "" {
			q := url.Values{}
			q.Set("page-token", pageToken)
			u.RawQuery = q.Encode()
		}

		req, err := c.v2.NewRequest("GET", u, nil)
		if err != nil {
			return nil, fmt.Errorf("GetPipelineWorkflows: build request: %w", err)
		}

		var resp listWorkflowsResponse
		if _, err := c.v2.DoRequest(req, &resp); err != nil {
			return nil, fmt.Errorf("GetPipelineWorkflows %q: %w", pipelineID, err)
		}

		all = append(all, resp.Items...)

		if resp.NextPageToken == "" {
			break
		}
		pageToken = resp.NextPageToken
	}

	return all, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// GetWorkflowJobs
// ─────────────────────────────────────────────────────────────────────────────

// Job holds minimal metadata about a workflow job as returned by
// GET /api/v2/workflow/{workflow-id}/job.
//
// JSON shape confirmed from live API:
//
//	{"items":[{"name":"<name>","job_number":<int>,"status":"<status>",...}],...}
type Job struct {
	Name      string `json:"name"`
	JobNumber int    `json:"job_number"`
	Status    string `json:"status"`
}

type listJobsResponse struct {
	Items         []Job  `json:"items"`
	NextPageToken string `json:"next_page_token"`
}

// GetWorkflowJobs returns all jobs for a given workflow UUID.
//
// Endpoint: GET /api/v2/workflow/{workflow-id}/job
func (c *Client) GetWorkflowJobs(workflowID string) ([]Job, error) {
	var all []Job
	pageToken := ""

	for {
		path := "workflow/" + url.PathEscape(workflowID) + "/job"
		u, err := url.Parse(path)
		if err != nil {
			return nil, fmt.Errorf("GetWorkflowJobs: build URL: %w", err)
		}
		if pageToken != "" {
			q := url.Values{}
			q.Set("page-token", pageToken)
			u.RawQuery = q.Encode()
		}

		req, err := c.v2.NewRequest("GET", u, nil)
		if err != nil {
			return nil, fmt.Errorf("GetWorkflowJobs: build request: %w", err)
		}

		var resp listJobsResponse
		if _, err := c.v2.DoRequest(req, &resp); err != nil {
			return nil, fmt.Errorf("GetWorkflowJobs %q: %w", workflowID, err)
		}

		all = append(all, resp.Items...)

		if resp.NextPageToken == "" {
			break
		}
		pageToken = resp.NextPageToken
	}

	return all, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// ListJobArtifacts / DownloadArtifact
// ─────────────────────────────────────────────────────────────────────────────

// Artifact represents one artifact as returned by
// GET /api/v2/project/{project-slug}/{job-number}/artifacts.
//
// JSON shape confirmed from live API:
//
//	{"items":[{"path":"<path>","node_index":<int>,"url":"<url>"}],...}
type Artifact struct {
	Path      string `json:"path"`
	NodeIndex int    `json:"node_index"`
	URL       string `json:"url"`
}

type listArtifactsResponse struct {
	Items         []Artifact `json:"items"`
	NextPageToken string     `json:"next_page_token"`
}

// ListJobArtifacts returns all artifacts for a job identified by project slug
// and job number, fetching all pages automatically.
//
// Endpoint: GET /api/v2/project/{project-slug}/{job-number}/artifacts
func (c *Client) ListJobArtifacts(slug string, jobNumber int) ([]Artifact, error) {
	var all []Artifact
	pageToken := ""

	for {
		u, err := slugSubresource(slug, fmt.Sprintf("%d/artifacts", jobNumber))
		if err != nil {
			return nil, fmt.Errorf("ListJobArtifacts: %w", err)
		}
		if pageToken != "" {
			q := url.Values{}
			q.Set("page-token", pageToken)
			u.RawQuery = q.Encode()
		}

		req, err := c.v2.NewRequest("GET", u, nil)
		if err != nil {
			return nil, fmt.Errorf("ListJobArtifacts: build request: %w", err)
		}

		var resp listArtifactsResponse
		if _, err := c.v2.DoRequest(req, &resp); err != nil {
			return nil, fmt.Errorf("ListJobArtifacts %q #%d: %w", slug, jobNumber, err)
		}

		all = append(all, resp.Items...)

		if resp.NextPageToken == "" {
			break
		}
		pageToken = resp.NextPageToken
	}

	return all, nil
}

// DownloadArtifact fetches the artifact at artifactURL, passing the client's
// Circle-Token in the request header so that private artifacts on
// circle-artifacts.com are accessible. It follows redirects automatically
// (the http.Client used by the rest client does so by default).
//
// The returned bytes are the raw artifact body (e.g. JSON).
func (c *Client) DownloadArtifact(artifactURL string) ([]byte, error) {
	req, err := http.NewRequest("GET", artifactURL, nil) //nolint:noctx
	if err != nil {
		return nil, fmt.Errorf("DownloadArtifact: build request: %w", err)
	}
	// Authenticate so circle-artifacts.com serves private artifacts.
	c.v2.EnrichDownloadRequest(req)

	resp, err := c.v2.RawDo(req)
	if err != nil {
		return nil, fmt.Errorf("DownloadArtifact: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("DownloadArtifact: HTTP %d: %s", resp.StatusCode, string(body))
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("DownloadArtifact: read body: %w", err)
	}
	return data, nil
}
