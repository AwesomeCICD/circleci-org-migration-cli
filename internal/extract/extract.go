// Package extract orchestrates secret extraction via an unversioned CircleCI
// pipeline run. It builds an inline config that dumps environment-variable
// values to a job artifact, triggers the run, polls until completion, then
// downloads and parses the artifact into a name→value map.
//
// No secret values are ever logged or written to stdout.
package extract

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/AwesomeCICD/circleci-org-migration-cli/api/project"
)

// ─────────────────────────────────────────────────────────────────────────────
// Dependency interfaces (injected so tests can use fakes)
// ─────────────────────────────────────────────────────────────────────────────

// PipelineRunner triggers an unversioned pipeline run and returns its UUID.
type PipelineRunner interface {
	TriggerPipelineRun(slug, definitionID, branch, configYAML string, params map[string]any) (string, error)
}

// WorkflowPoller returns the current workflows for a pipeline.
type WorkflowPoller interface {
	GetPipelineWorkflows(pipelineID string) ([]project.Workflow, error)
}

// JobLister returns the jobs in a workflow.
type JobLister interface {
	GetWorkflowJobs(workflowID string) ([]project.Job, error)
}

// ArtifactFetcher lists and downloads job artifacts.
type ArtifactFetcher interface {
	ListJobArtifacts(slug string, jobNumber int) ([]project.Artifact, error)
	DownloadArtifact(url string) ([]byte, error)
}

// Deps bundles all API dependencies so callers can pass a single concrete
// *project.Client or a fake in tests.
type Deps interface {
	PipelineRunner
	WorkflowPoller
	JobLister
	ArtifactFetcher
}

// ─────────────────────────────────────────────────────────────────────────────
// Options
// ─────────────────────────────────────────────────────────────────────────────

// Options controls the behaviour of Capture.
type Options struct {
	// DefinitionID is the pipeline-definition UUID to use (required).
	// Obtain via project.Client.ListPipelineDefinitions.
	DefinitionID string

	// Branch is checked out for the extraction run (default "main").
	Branch string

	// PollInterval is the delay between workflow-status polls (default 5s).
	PollInterval time.Duration

	// PollTimeout is the maximum time to wait for the pipeline to finish.
	// Zero means no timeout (the caller's context deadline applies instead).
	PollTimeout time.Duration
}

func (o *Options) branch() string {
	if o.Branch != "" {
		return o.Branch
	}
	return "main"
}

func (o *Options) pollInterval() time.Duration {
	if o.PollInterval > 0 {
		return o.PollInterval
	}
	return 5 * time.Second
}

// ─────────────────────────────────────────────────────────────────────────────
// Inline-config builder
// ─────────────────────────────────────────────────────────────────────────────

// dumpJobName is the name given to the extraction job in the inline config.
const dumpJobName = "circleci-migrate-extract"

// artifactPath is the path written by the extraction job and then uploaded as
// an artifact. It must start with /tmp/ so store_artifacts can resolve it.
const artifactPath = "/tmp/circleci-migrate-secrets.json"

// buildExtractConfig constructs the minimal CircleCI YAML config that:
//   - attaches each context in contextNames
//   - runs a single job on cimg/base:current (resource_class: small)
//   - echos the given envNames as JSON to /tmp/circleci-migrate-secrets.json
//   - uploads that file as a build artifact via store_artifacts
//
// The output is a pure string transformation with no I/O so it is easy to
// unit-test independently of the HTTP layer.
//
// Shell command design: each value is written as a JSON string using printf
// %s quoting inside a jq-assembled object, avoiding eval or subshell exposure
// of the values in process-list metadata.
func buildExtractConfig(envNames []string, contextNames []string) string {
	var sb strings.Builder

	sb.WriteString("version: 2.1\n")
	sb.WriteString("jobs:\n")
	sb.WriteString("  " + dumpJobName + ":\n")
	sb.WriteString("    docker:\n")
	sb.WriteString("      - image: cimg/base:current\n")
	sb.WriteString("    resource_class: small\n")
	sb.WriteString("    steps:\n")
	sb.WriteString("      - run:\n")
	sb.WriteString("          name: Dump env vars to artifact\n")
	sb.WriteString("          command: |\n")

	// Build a shell snippet that writes a JSON object of name→value pairs.
	// We use printf and jq to avoid any shell-injection risk from var names.
	// The jq filter builds the object incrementally from a stream of
	// ["NAME","$VALUE"] two-element arrays.
	sb.WriteString("            set -euo pipefail\n")
	sb.WriteString("            (\n")

	for _, name := range envNames {
		// Each line: printf '%s\t%s\n' NAME "$NAME"
		// Using tab as delimiter; jq splits on \t.
		safeName := strings.ReplaceAll(name, "'", "'\\''")
		sb.WriteString(fmt.Sprintf(
			"              printf '%%s\\t%%s\\n' '%s' \"${%s:-}\"\n",
			safeName, safeName,
		))
	}

	// Pipe through jq to build the JSON object.
	sb.WriteString("            ) | jq -Rn '[inputs | split(\"\\t\")] | map({(.[0]): .[1]}) | add // {}' \\\n")
	sb.WriteString(fmt.Sprintf("              > %s\n", artifactPath))

	sb.WriteString("      - store_artifacts:\n")
	sb.WriteString(fmt.Sprintf("          path: %s\n", artifactPath))
	sb.WriteString("          destination: circleci-migrate-secrets.json\n")

	sb.WriteString("workflows:\n")
	sb.WriteString("  extract:\n")
	sb.WriteString("    jobs:\n")
	sb.WriteString("      - " + dumpJobName + ":\n")

	if len(contextNames) > 0 {
		sb.WriteString("          context:\n")
		for _, ctx := range contextNames {
			sb.WriteString(fmt.Sprintf("            - %s\n", ctx))
		}
	}

	return sb.String()
}

// ─────────────────────────────────────────────────────────────────────────────
// Capture orchestrator
// ─────────────────────────────────────────────────────────────────────────────

// terminalStatuses is the set of CircleCI workflow statuses that indicate the
// pipeline has finished (success, failure, or cancellation).
var terminalStatuses = map[string]bool{
	"success":  true,
	"failed":   true,
	"error":    true,
	"canceled": true,
}

// ErrWorkflowFailed is returned when the extraction workflow finishes in a
// non-success terminal state.
var ErrWorkflowFailed = errors.New("extraction workflow did not succeed")

// ErrNoArtifact is returned when the dump job completed but its artifact was
// not found in the artifact list.
var ErrNoArtifact = errors.New("extraction job completed but no secrets artifact found")

// Capture triggers an unversioned pipeline run for projectSlug, waits for it
// to complete, downloads the secrets artifact, and returns a map of
// variable-name → plaintext-value.
//
// envNames lists the project env var names to capture.
// contextNames are the context names to attach to the extraction job (their
// env vars will also be injected into the job and captured).
// allVarNames is the union of envNames and all context var names to capture.
//
// SECURITY: the returned map contains plaintext secrets. The caller must never
// log or print its values.
func Capture(
	ctx context.Context,
	deps Deps,
	projectSlug string,
	allVarNames []string,
	contextNames []string,
	opts Options,
) (map[string]string, error) {
	branch := opts.branch()
	pollInterval := opts.pollInterval()

	if opts.DefinitionID == "" {
		return nil, fmt.Errorf("extract.Capture: DefinitionID is required")
	}

	configYAML := buildExtractConfig(allVarNames, contextNames)

	pipelineID, err := deps.TriggerPipelineRun(projectSlug, opts.DefinitionID, branch, configYAML, nil)
	if err != nil {
		if errors.Is(err, project.ErrPipelineSkipped) {
			return nil, fmt.Errorf("extract.Capture: pipeline run was skipped by server — check api-trigger-with-config is enabled and the config is valid")
		}
		return nil, fmt.Errorf("extract.Capture: trigger pipeline: %w", err)
	}

	// Apply optional poll timeout as a child context deadline.
	pollCtx := ctx
	if opts.PollTimeout > 0 {
		var cancel context.CancelFunc
		pollCtx, cancel = context.WithTimeout(ctx, opts.PollTimeout)
		defer cancel()
	}

	// Poll until at least one terminal workflow is found.
	wf, err := pollWorkflow(pollCtx, deps, pipelineID, pollInterval)
	if err != nil {
		return nil, fmt.Errorf("extract.Capture: poll: %w", err)
	}

	if wf.Status != "success" {
		return nil, fmt.Errorf("%w: status=%q workflow=%q", ErrWorkflowFailed, wf.Status, wf.Name)
	}

	// Find the dump job in the workflow.
	jobs, err := deps.GetWorkflowJobs(wf.ID)
	if err != nil {
		return nil, fmt.Errorf("extract.Capture: list jobs: %w", err)
	}

	var dumpJob *project.Job
	for i := range jobs {
		if jobs[i].Name == dumpJobName {
			dumpJob = &jobs[i]
			break
		}
	}
	if dumpJob == nil {
		return nil, fmt.Errorf("extract.Capture: job %q not found in workflow", dumpJobName)
	}

	// List and download the secrets artifact.
	artifacts, err := deps.ListJobArtifacts(projectSlug, dumpJob.JobNumber)
	if err != nil {
		return nil, fmt.Errorf("extract.Capture: list artifacts: %w", err)
	}

	const wantDest = "circleci-migrate-secrets.json"
	var artifactURL string
	for _, a := range artifacts {
		if strings.HasSuffix(a.Path, "circleci-migrate-secrets.json") || strings.HasSuffix(a.URL, wantDest) {
			artifactURL = a.URL
			break
		}
	}
	if artifactURL == "" {
		return nil, ErrNoArtifact
	}

	data, err := deps.DownloadArtifact(artifactURL)
	if err != nil {
		return nil, fmt.Errorf("extract.Capture: download artifact: %w", err)
	}

	var values map[string]string
	if err := json.Unmarshal(data, &values); err != nil {
		return nil, fmt.Errorf("extract.Capture: parse artifact JSON: %w", err)
	}

	return values, nil
}

// pollWorkflow blocks until the pipeline has a terminal workflow, then returns
// it. It returns an error if ctx is cancelled or the pipeline has no workflows
// after several polls.
func pollWorkflow(ctx context.Context, poller WorkflowPoller, pipelineID string, interval time.Duration) (project.Workflow, error) {
	for {
		workflows, err := poller.GetPipelineWorkflows(pipelineID)
		if err != nil {
			return project.Workflow{}, fmt.Errorf("GetPipelineWorkflows: %w", err)
		}

		for _, wf := range workflows {
			if terminalStatuses[wf.Status] {
				return wf, nil
			}
		}

		select {
		case <-ctx.Done():
			return project.Workflow{}, fmt.Errorf("poll timed out waiting for pipeline %q: %w", pipelineID, ctx.Err())
		case <-time.After(interval):
			// continue polling
		}
	}
}
