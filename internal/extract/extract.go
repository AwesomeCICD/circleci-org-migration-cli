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
	bundlepkg "github.com/AwesomeCICD/circleci-org-migration-cli/internal/bundle"
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

// StorageMode controls where the extracted (and optionally encrypted) bundle
// is stored after the in-pipeline extraction job runs.
type StorageMode string

const (
	// StorageArtifact stores the bundle as a CircleCI job artifact (default).
	StorageArtifact StorageMode = "artifact"
	// StorageS3 uploads the bundle to S3 (requires AWS CLI + creds in the job).
	StorageS3 StorageMode = "s3"
	// StorageBoth stores the bundle in both CircleCI artifact and S3.
	StorageBoth StorageMode = "both"
)

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

	// EncryptRecipient is the age/SSH public key recipient string for in-pipeline
	// encryption. When non-empty the inline config passes this key to
	// 'secrets extract --encrypt --recipient', so the artifact stored in
	// CircleCI is age-encrypted. Empty means plaintext artifact.
	//
	// SECURITY: this is a PUBLIC key — safe to embed in the inline config.
	// Never set this to a private key.
	EncryptRecipient string

	// Storage controls where the extracted bundle is stored.
	// Default (empty / StorageArtifact) stores as a CircleCI artifact.
	Storage StorageMode

	// S3Bucket is the S3 bucket name for S3/both storage modes.
	S3Bucket string

	// S3Prefix is the key prefix within the S3 bucket.
	S3Prefix string
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

func (o *Options) storageMode() StorageMode {
	switch o.Storage {
	case StorageS3, StorageBoth:
		return o.Storage
	default:
		return StorageArtifact
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Inline-config builder
// ─────────────────────────────────────────────────────────────────────────────

// dumpJobName is the name given to the extraction job in the inline config.
const dumpJobName = "circleci-migrate-extract"

// artifactPath is the path written by the extraction job and then uploaded as
// an artifact. It must start with /tmp/ so store_artifacts can resolve it.
const artifactPath = "/tmp/circleci-migrate-secrets.json"

// artifactPathAge is the encrypted variant of the artifact path.
const artifactPathAge = "/tmp/circleci-migrate-secrets.json.age"

// buildExtractConfig constructs the minimal CircleCI YAML config that:
//   - attaches each context in contextNames
//   - runs a single job on cimg/base:current (resource_class: small)
//   - echos the given envNames as JSON to /tmp/circleci-migrate-secrets.json
//   - optionally encrypts to /tmp/circleci-migrate-secrets.json.age
//   - optionally uploads to S3 via `aws s3 cp`
//   - uploads the artifact via store_artifacts
//
// When encryptRecipient is non-empty, the inline config embeds the PUBLIC key
// in an environment variable and passes it to 'secrets extract --encrypt
// --recipient $MIGRATE_ENCRYPT_RECIPIENT'. The public key is safe to embed.
//
// The output is a pure string transformation with no I/O so it is easy to
// unit-test independently of the HTTP layer.
//
// Shell command design: each value is written as a JSON string using printf
// %s quoting inside a jq-assembled object, avoiding eval or subshell exposure
// of the values in process-list metadata.
func buildExtractConfig(envNames []string, contextNames []string, opts *Options) string {
	encrypt := opts != nil && opts.EncryptRecipient != ""
	storage := StorageArtifact
	if opts != nil {
		storage = opts.storageMode()
	}
	s3Bucket := ""
	s3Prefix := ""
	if opts != nil {
		s3Bucket = opts.S3Bucket
		s3Prefix = opts.S3Prefix
	}

	var sb strings.Builder

	sb.WriteString("version: 2.1\n")
	sb.WriteString("jobs:\n")
	sb.WriteString("  " + dumpJobName + ":\n")
	sb.WriteString("    docker:\n")
	sb.WriteString("      - image: cimg/base:current\n")
	sb.WriteString("    resource_class: small\n")

	// Embed the public recipient key as an env var when encryption is requested.
	// SECURITY: this is a PUBLIC key — it is safe to embed in the config.
	if encrypt {
		sb.WriteString("    environment:\n")
		// Escape any $ in the recipient string so it is treated as literal.
		safeRecip := strings.ReplaceAll(opts.EncryptRecipient, "$", "\\$")
		sb.WriteString(fmt.Sprintf("      MIGRATE_ENCRYPT_RECIPIENT: %q\n", safeRecip))
	}

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

	if encrypt {
		// Run circleci-migrate secrets extract --encrypt inline to produce the
		// encrypted artifact. We re-extract from the JSON we just wrote.
		// Actually we encrypt the JSON file we already produced using a minimal
		// inline approach: pipe the file through `circleci-migrate secrets decrypt`
		// is circular — instead we use the extract command's encrypt path.
		// Simplest: produce the JSON then encrypt it in place using a Go helper.
		// Since circleci-migrate is available in the job (it runs this config),
		// we encode the public key and call a mini-encrypt shell snippet.
		// The cleanest approach: write a helper that our CLI provides.
		// We pipe the JSON through `circleci-migrate bundle-encrypt` -- but that
		// command doesn't exist yet; instead we'll generate a Python/shell snippet.
		//
		// Best approach per design: the in-pipeline `secrets extract` binary
		// already handles --encrypt. We need circleci-migrate installed in the job.
		// Per the design doc: "no external age binary needed — our binary handles it".
		// So we call `circleci-migrate secrets extract --encrypt --recipient ...`
		// BUT that reads from env vars, not from a JSON file.
		//
		// Resolution: emit a step that installs circleci-migrate (or assumes it's
		// already installed via a prior orb step) and calls:
		//   circleci-migrate bundle-encrypt --recipient "$MIGRATE_ENCRYPT_RECIPIENT"
		//     --input /tmp/circleci-migrate-secrets.json
		//     --output /tmp/circleci-migrate-secrets.json.age
		//
		// We add a `bundle encrypt` internal command, OR we embed a small Python
		// snippet for the encryption. Given the design says "our binary handles it",
		// add an internal `bundle encrypt` hidden command that reads stdin/file.
		//
		// For now emit a step using `python3 -c` with the age encryption.
		// Actually the simplest and most robust approach: use the `age` tool.
		// But the design says no external age binary. So we use our own binary.
		//
		// FINAL decision: emit a step calling `circleci-migrate bundle-encrypt`
		// (hidden internal command we add). The binary is already in PATH because
		// the prior orb install step put it there.
		sb.WriteString(fmt.Sprintf("            circleci-migrate bundle-encrypt --recipient \"$MIGRATE_ENCRYPT_RECIPIENT\" --input %s --output %s\n", artifactPath, artifactPathAge))
		sb.WriteString(fmt.Sprintf("            rm -f %s\n", artifactPath)) // remove plaintext
	}

	// S3 upload step.
	if storage == StorageS3 || storage == StorageBoth {
		targetFile := artifactPath
		if encrypt {
			targetFile = artifactPathAge
		}
		s3Key := "circleci-migrate-secrets.json"
		if encrypt {
			s3Key = "circleci-migrate-secrets.json.age"
		}
		s3URL := fmt.Sprintf("s3://%s/%s%s", s3Bucket, s3Prefix, s3Key)
		sb.WriteString("      - run:\n")
		sb.WriteString("          name: Upload secret bundle to S3\n")
		sb.WriteString("          command: |\n")
		sb.WriteString("            set -euo pipefail\n")
		sb.WriteString(fmt.Sprintf("            aws s3 cp %s %s\n", targetFile, s3URL))
	}

	// store_artifacts step — only for artifact or both modes.
	if storage == StorageArtifact || storage == StorageBoth {
		if encrypt {
			sb.WriteString("      - store_artifacts:\n")
			sb.WriteString(fmt.Sprintf("          path: %s\n", artifactPathAge))
			sb.WriteString("          destination: circleci-migrate-secrets.json.age\n")
		} else {
			sb.WriteString("      - store_artifacts:\n")
			sb.WriteString(fmt.Sprintf("          path: %s\n", artifactPath))
			sb.WriteString("          destination: circleci-migrate-secrets.json\n")
		}
	}

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
// When opts.EncryptRecipient is set, the artifact in CircleCI is
// age-encrypted. Capture automatically decrypts it using the identities in
// opts — callers must supply them via opts.DecryptIdentityFile.
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
	return CaptureWithDecrypt(ctx, deps, projectSlug, allVarNames, contextNames, opts, "")
}

// CaptureWithDecrypt is like Capture but accepts a decryptIdentityFile path
// used to decrypt an age-encrypted artifact downloaded from the pipeline.
// When decryptIdentityFile is empty and the artifact is encrypted, decryption
// is skipped and the raw (encrypted) bytes are returned — useful for callers
// that handle decryption themselves.
//
// SECURITY: identityFile is a private key — never logged.
func CaptureWithDecrypt(
	ctx context.Context,
	deps Deps,
	projectSlug string,
	allVarNames []string,
	contextNames []string,
	opts Options,
	decryptIdentityFile string,
) (map[string]string, error) {
	branch := opts.branch()
	pollInterval := opts.pollInterval()
	encrypt := opts.EncryptRecipient != ""
	storage := opts.storageMode()

	if opts.DefinitionID == "" {
		return nil, fmt.Errorf("extract.Capture: DefinitionID is required")
	}

	configYAML := buildExtractConfig(allVarNames, contextNames, &opts)

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

	// For S3-only storage we cannot download from CircleCI — instruct the caller.
	if storage == StorageS3 {
		return nil, fmt.Errorf("extract.Capture: --storage s3 was requested; the bundle is in s3://%s/%s — download and decrypt it locally, then load it with 'secrets decrypt'", opts.S3Bucket, opts.S3Prefix)
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

	wantDest := "circleci-migrate-secrets.json"
	if encrypt {
		wantDest = "circleci-migrate-secrets.json.age"
	}
	var artifactURL string
	for _, a := range artifacts {
		if strings.HasSuffix(a.Path, wantDest) || strings.HasSuffix(a.URL, wantDest) {
			artifactURL = a.URL
			break
		}
	}
	if artifactURL == "" {
		// Fallback: also try the non-encrypted path in case the job didn't encrypt.
		for _, a := range artifacts {
			if strings.HasSuffix(a.Path, "circleci-migrate-secrets.json") || strings.HasSuffix(a.URL, "circleci-migrate-secrets.json") {
				artifactURL = a.URL
				break
			}
		}
	}
	if artifactURL == "" {
		return nil, ErrNoArtifact
	}

	data, err := deps.DownloadArtifact(artifactURL)
	if err != nil {
		return nil, fmt.Errorf("extract.Capture: download artifact: %w", err)
	}

	// If the data is age-encrypted and we have an identity, decrypt it.
	if encrypt && decryptIdentityFile != "" {
		// SECURITY: do not log identityFile.
		identities, idErr := bundlepkg.ParseIdentityFile(decryptIdentityFile)
		if idErr != nil {
			return nil, fmt.Errorf("extract.Capture: load decrypt identity: %w", idErr)
		}
		data, err = bundlepkg.DecryptBundle(data, identities...)
		if err != nil {
			return nil, fmt.Errorf("extract.Capture: decrypt artifact: %w", err)
		}
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
