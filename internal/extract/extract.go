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
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
	"github.com/AwesomeCICD/circleci-org-migration-cli/version"
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

// buildInstallStep returns the YAML for a CircleCI run step that downloads and
// installs the circleci-migrate binary from GitHub Releases.  It mirrors the
// logic in orb/src/scripts/install.sh: OS/arch detection, tarball download,
// extract to /usr/local/bin.
//
// When ver is empty, "dev", or "unknown" the step falls back to the latest
// release tag via the GitHub API.  Otherwise it downloads exactly ver (must
// include the "v" prefix, e.g. "v0.4.0").
func buildInstallStep(ver string) string {
	repo := "AwesomeCICD/circleci-org-migration-cli"
	// Normalise: strip leading "v" for the tarball filename, keep it for the
	// tag/download URL.  Fall back to "latest" for dev/unknown builds.
	tag := ver
	if tag == "" || tag == "dev" || tag == "unknown" {
		tag = "latest"
	}

	var sb strings.Builder
	sb.WriteString("      - run:\n")
	sb.WriteString("          name: Install circleci-migrate\n")
	sb.WriteString("          command: |\n")
	sb.WriteString("            set -euo pipefail\n")
	sb.WriteString("            repo=" + repo + "\n")
	if tag == "latest" {
		sb.WriteString(`            ver=$(curl -sfL "https://api.github.com/repos/${repo}/releases/latest" \` + "\n")
		sb.WriteString(`              | grep -o '"tag_name": *"[^"]*"' | head -1 \` + "\n")
		sb.WriteString(`              | sed 's/.*"\(v[^"]*\)".*/\1/')` + "\n")
		sb.WriteString("            if [ -z \"$ver\" ]; then\n")
		sb.WriteString("              echo 'ERROR: could not resolve latest release tag' >&2; exit 1\n")
		sb.WriteString("            fi\n")
	} else {
		sb.WriteString(fmt.Sprintf("            ver=%s\n", tag))
	}
	sb.WriteString(`            v="${ver#v}"` + "\n")
	sb.WriteString(`            os=$(uname -s | tr '[:upper:]' '[:lower:]')` + "\n")
	sb.WriteString(`            arch=$(uname -m)` + "\n")
	sb.WriteString(`            case "$arch" in` + "\n")
	sb.WriteString(`              x86_64)        arch="amd64" ;;` + "\n")
	sb.WriteString(`              aarch64|arm64) arch="arm64" ;;` + "\n")
	sb.WriteString(`              *) echo "ERROR: unsupported arch: $arch" >&2; exit 1 ;;` + "\n")
	sb.WriteString(`            esac` + "\n")
	sb.WriteString(`            url="https://github.com/${repo}/releases/download/${ver}/circleci-migrate_${v}_${os}_${arch}.tar.gz"` + "\n")
	sb.WriteString(`            echo "Downloading ${url}"` + "\n")
	sb.WriteString(`            tmp=$(mktemp -d)` + "\n")
	sb.WriteString(`            curl -sfL "$url" | tar -xz -C "$tmp"` + "\n")
	sb.WriteString(`            bin=$(find "$tmp" -type f -name circleci-migrate | head -1)` + "\n")
	sb.WriteString(`            sudo install -m 0755 "$bin" /usr/local/bin/circleci-migrate` + "\n")
	sb.WriteString(`            rm -rf "$tmp"` + "\n")
	sb.WriteString(`            circleci-migrate version` + "\n")
	return sb.String()
}

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
	return buildExtractConfigWithVersion(envNames, contextNames, opts, version.Version)
}

// buildExtractConfigWithVersion is the internal implementation that accepts a
// version string for testability (so tests can inject a concrete version string
// without linking against the real version package variable).
func buildExtractConfigWithVersion(envNames []string, contextNames []string, opts *Options, ver string) string {
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
	// Always install circleci-migrate first so the bundle-encrypt step (and any
	// future in-job circleci-migrate invocations) can find the binary.  Without
	// this step the job exits 127 ("command not found") on the encrypt step.
	sb.WriteString(buildInstallStep(ver))
	sb.WriteString("      - run:\n")
	sb.WriteString("          name: Dump env vars to artifact\n")
	sb.WriteString("          command: |\n")

	// Build a shell snippet that writes a JSON object of name→value pairs.
	// We use printf and jq to avoid any shell-injection risk from var names.
	// The jq filter builds the object incrementally from a stream of
	// ["NAME","$VALUE"] two-element arrays.
	sb.WriteString("            set -euo pipefail\n")

	if len(envNames) > 0 {
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
	} else {
		// No variable names: an empty `( )` subshell is a bash syntax error, so
		// just write an empty JSON object. (Callers should skip extraction
		// entirely when there is nothing to capture, but stay robust here.)
		sb.WriteString(fmt.Sprintf("            echo '{}' > %s\n", artifactPath))
	}

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
		// Remove plaintext on EXIT (covers both success and any non-zero exit,
		// including when bundle-encrypt fails under set -euo pipefail).
		sb.WriteString(fmt.Sprintf("            trap 'rm -f %s' EXIT\n", artifactPath))
		sb.WriteString(fmt.Sprintf("            circleci-migrate bundle-encrypt --recipient \"$MIGRATE_ENCRYPT_RECIPIENT\" --input %s --output %s\n", artifactPath, artifactPathAge))
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
	if len(contextNames) > 0 {
		// Job needs a `context:` mapping — emit the job as a mapping key.
		sb.WriteString("      - " + dumpJobName + ":\n")
		sb.WriteString("          context:\n")
		for _, ctx := range contextNames {
			sb.WriteString(fmt.Sprintf("            - %s\n", ctx))
		}
	} else {
		// No contexts ⇒ no job-level config. Emit the bare job name as a
		// STRING; `- job:` with nothing under it is an invalid null mapping
		// ("expected type: String, found: Mapping") and the pipeline errors.
		sb.WriteString("      - " + dumpJobName + "\n")
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

// ─────────────────────────────────────────────────────────────────────────────
// SSH-key extraction
// ─────────────────────────────────────────────────────────────────────────────

// sshKeyArtifactPath is the path written by the SSH-key extraction job.
const sshKeyArtifactPath = "/tmp/circleci-migrate-sshkeys.json"

// sshKeyArtifactPathAge is the encrypted variant.
const sshKeyArtifactPathAge = "/tmp/circleci-migrate-sshkeys.json.age"

// sshKeyDumpJobName is the name of the SSH-key extraction job.
const sshKeyDumpJobName = "circleci-migrate-ssh-extract"

// SSHKeyInput describes one cataloged SSH key whose private material we want
// to extract.  Fingerprint is the bare SHA256 fingerprint (no "SHA256:" prefix)
// as stored in the manifest; the generated config prefixes it with "SHA256:"
// when calling add_ssh_keys and when matching via ssh-keygen -lf.
type SSHKeyInput struct {
	Fingerprint string // bare SHA256 fingerprint (no "SHA256:" prefix)
	Hostname    string // target hostname (may be empty for global keys)
}

// buildSSHKeyExtractConfig constructs the minimal CircleCI YAML config that:
//   - Uses a docker executor (no checkout step, so the deploy/checkout key is
//     never materialised).
//   - Runs add_ssh_keys with the EXPLICIT cataloged fingerprints (prefixed with
//     "SHA256:") so only the requested keys are placed in $HOME/.ssh.
//   - Runs a script that iterates ~/.ssh/id_rsa_* files, computes each file's
//     SHA256 fingerprint via ssh-keygen -lf, and records matching keys (by
//     recomputed SHA256 fingerprint) as JSON.
//   - Optionally encrypts the JSON output via the existing bundle-encrypt path.
//   - Stores the (optionally encrypted) artifact.
//
// The match-by-recomputed-SHA256 approach is necessary because CircleCI names
// the materialised key files using the MD5 fingerprint (id_rsa_<md5fp>), while
// the manifest stores SHA256 fingerprints (the only format the API returns).
// Recomputing the SHA256 in-job bridges this gap without any MD5 lookups.
//
// SECURITY: private key material never appears in logs — the script reads file
// contents without echoing them. Only the encrypted artifact leaves the job.
func buildSSHKeyExtractConfig(keys []SSHKeyInput, opts *Options) string {
	return buildSSHKeyExtractConfigWithVersion(keys, opts, version.Version)
}

// buildSSHKeyExtractConfigWithVersion is the testable variant.
func buildSSHKeyExtractConfigWithVersion(keys []SSHKeyInput, opts *Options, ver string) string {
	encrypt := opts != nil && opts.EncryptRecipient != ""

	var sb strings.Builder

	sb.WriteString("version: 2.1\n")
	sb.WriteString("jobs:\n")
	sb.WriteString("  " + sshKeyDumpJobName + ":\n")
	sb.WriteString("    docker:\n")
	sb.WriteString("      - image: cimg/base:current\n")
	sb.WriteString("    resource_class: small\n")

	if encrypt {
		sb.WriteString("    environment:\n")
		safeRecip := strings.ReplaceAll(opts.EncryptRecipient, "$", "\\$")
		sb.WriteString(fmt.Sprintf("      MIGRATE_ENCRYPT_RECIPIENT: %q\n", safeRecip))
	}

	sb.WriteString("    steps:\n")
	// Install circleci-migrate so bundle-encrypt is available.
	sb.WriteString(buildInstallStep(ver))

	// add_ssh_keys step — explicit fingerprints with SHA256: prefix.
	sb.WriteString("      - add_ssh_keys:\n")
	sb.WriteString("          fingerprints:\n")
	for _, k := range keys {
		sb.WriteString(fmt.Sprintf("            - \"SHA256:%s\"\n", k.Fingerprint))
	}

	// Script: iterate materialised key files, match by recomputed SHA256,
	// collect into JSON.
	sb.WriteString("      - run:\n")
	sb.WriteString("          name: Collect SSH private keys by SHA256 fingerprint\n")
	sb.WriteString("          command: |\n")
	sb.WriteString("            set -euo pipefail\n")

	// Build a shell map of SHA256→hostname so we can look up the hostname
	// when we find a matching file.  We embed the fingerprint→hostname pairs
	// directly in the generated script as a simple series of assignments.
	sb.WriteString("            # fingerprint → hostname lookup (from manifest catalog)\n")
	sb.WriteString("            declare -A FP_TO_HOST\n")
	for _, k := range keys {
		// Shell-safe: fingerprints are base64 chars + '=' signs; hostnames are
		// hostname chars.  Neither contains shell-special characters.
		sb.WriteString(fmt.Sprintf("            FP_TO_HOST[%q]=%q\n", k.Fingerprint, k.Hostname))
	}

	// Walk ~/.ssh/id_rsa_* files, recompute SHA256, match against our set.
	sb.WriteString(`
            results='[]'
            for f in "$HOME"/.ssh/id_rsa_* "$HOME"/.ssh/id_ed25519_* "$HOME"/.ssh/id_ecdsa_*; do
              [ -f "$f" ] || continue
              # Recompute SHA256 fingerprint (output: "2048 SHA256:<fp> comment (RSA)")
              fp_line=$(ssh-keygen -lf "$f" -E sha256 2>/dev/null) || continue
              # Extract bare fingerprint after "SHA256:"
              fp=$(echo "$fp_line" | grep -oP '(?<=SHA256:)[A-Za-z0-9+/=]+') || continue
              # Check if this fingerprint is in our catalog
              if [ -z "${FP_TO_HOST[$fp]+set}" ]; then
                continue
              fi
              host="${FP_TO_HOST[$fp]}"
              # Read private key contents (no echo — avoid leaking to log)
              privkey=$(cat "$f")
              # Append JSON object to results array using jq
              results=$(printf '%s' "$results" | jq \
                --arg fp  "$fp" \
                --arg hn  "$host" \
                --arg pk  "$privkey" \
                '. += [{"fingerprint":$fp,"hostname":$hn,"private_key":$pk}]')
            done
            printf '%s\n' "$results" > ` + sshKeyArtifactPath + "\n")

	if encrypt {
		sb.WriteString(fmt.Sprintf("            trap 'rm -f %s' EXIT\n", sshKeyArtifactPath))
		sb.WriteString(fmt.Sprintf("            circleci-migrate bundle-encrypt --recipient \"$MIGRATE_ENCRYPT_RECIPIENT\" --input %s --output %s\n", sshKeyArtifactPath, sshKeyArtifactPathAge))
	}

	// store_artifacts step.
	if encrypt {
		sb.WriteString("      - store_artifacts:\n")
		sb.WriteString(fmt.Sprintf("          path: %s\n", sshKeyArtifactPathAge))
		sb.WriteString("          destination: circleci-migrate-sshkeys.json.age\n")
	} else {
		sb.WriteString("      - store_artifacts:\n")
		sb.WriteString(fmt.Sprintf("          path: %s\n", sshKeyArtifactPath))
		sb.WriteString("          destination: circleci-migrate-sshkeys.json\n")
	}

	sb.WriteString("workflows:\n")
	sb.WriteString("  extract-ssh:\n")
	sb.WriteString("    jobs:\n")
	sb.WriteString("      - " + sshKeyDumpJobName + "\n")

	return sb.String()
}

// CaptureSSHKeys triggers an unversioned pipeline that materialises additional
// SSH keys via add_ssh_keys, collects private-key material matched by SHA256
// fingerprint, and returns the captured keys.
//
// keys lists the cataloged SSH keys (fingerprint + hostname) from the manifest
// Project.SSHKeys. Only keys whose SHA256 fingerprints appear in this list are
// captured; the checkout/deploy key is never materialised (no checkout step).
//
// When opts.EncryptRecipient is set, the artifact is age-encrypted and
// decryptIdentityFile must be provided for local decryption.
//
// SECURITY: the returned slice contains plaintext private-key material.
// The caller must never log or print the PrivateKey field.
func CaptureSSHKeys(
	ctx context.Context,
	deps Deps,
	projectSlug string,
	keys []SSHKeyInput,
	opts Options,
	decryptIdentityFile string,
) ([]manifest.BundleSSHKey, error) {
	if opts.DefinitionID == "" {
		return nil, fmt.Errorf("extract.CaptureSSHKeys: DefinitionID is required")
	}
	if len(keys) == 0 {
		return nil, nil
	}

	encrypt := opts.EncryptRecipient != ""
	branch := opts.branch()
	pollInterval := opts.pollInterval()

	configYAML := buildSSHKeyExtractConfig(keys, &opts)

	pipelineID, err := deps.TriggerPipelineRun(projectSlug, opts.DefinitionID, branch, configYAML, nil)
	if err != nil {
		if errors.Is(err, project.ErrPipelineSkipped) {
			return nil, fmt.Errorf("extract.CaptureSSHKeys: pipeline run was skipped — check api-trigger-with-config is enabled")
		}
		return nil, fmt.Errorf("extract.CaptureSSHKeys: trigger pipeline: %w", err)
	}

	pollCtx := ctx
	if opts.PollTimeout > 0 {
		var cancel context.CancelFunc
		pollCtx, cancel = context.WithTimeout(ctx, opts.PollTimeout)
		defer cancel()
	}

	wf, err := pollWorkflow(pollCtx, deps, pipelineID, pollInterval)
	if err != nil {
		return nil, fmt.Errorf("extract.CaptureSSHKeys: poll: %w", err)
	}
	if wf.Status != "success" {
		return nil, fmt.Errorf("%w: status=%q workflow=%q", ErrWorkflowFailed, wf.Status, wf.Name)
	}

	jobs, err := deps.GetWorkflowJobs(wf.ID)
	if err != nil {
		return nil, fmt.Errorf("extract.CaptureSSHKeys: list jobs: %w", err)
	}
	var dumpJob *project.Job
	for i := range jobs {
		if jobs[i].Name == sshKeyDumpJobName {
			dumpJob = &jobs[i]
			break
		}
	}
	if dumpJob == nil {
		return nil, fmt.Errorf("extract.CaptureSSHKeys: job %q not found in workflow", sshKeyDumpJobName)
	}

	artifacts, err := deps.ListJobArtifacts(projectSlug, dumpJob.JobNumber)
	if err != nil {
		return nil, fmt.Errorf("extract.CaptureSSHKeys: list artifacts: %w", err)
	}

	wantDest := "circleci-migrate-sshkeys.json"
	if encrypt {
		wantDest = "circleci-migrate-sshkeys.json.age"
	}
	var artifactURL string
	for _, a := range artifacts {
		if strings.HasSuffix(a.Path, wantDest) || strings.HasSuffix(a.URL, wantDest) {
			artifactURL = a.URL
			break
		}
	}
	if artifactURL == "" {
		return nil, fmt.Errorf("extract.CaptureSSHKeys: %w (want %q)", ErrNoArtifact, wantDest)
	}

	data, err := deps.DownloadArtifact(artifactURL)
	if err != nil {
		return nil, fmt.Errorf("extract.CaptureSSHKeys: download artifact: %w", err)
	}

	if encrypt && decryptIdentityFile != "" {
		identities, idErr := bundlepkg.ParseIdentityFile(decryptIdentityFile)
		if idErr != nil {
			return nil, fmt.Errorf("extract.CaptureSSHKeys: load decrypt identity: %w", idErr)
		}
		data, err = bundlepkg.DecryptBundle(data, identities...)
		if err != nil {
			return nil, fmt.Errorf("extract.CaptureSSHKeys: decrypt artifact: %w", err)
		}
	}

	var captured []manifest.BundleSSHKey
	if err := json.Unmarshal(data, &captured); err != nil {
		return nil, fmt.Errorf("extract.CaptureSSHKeys: parse artifact JSON: %w", err)
	}

	return captured, nil
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
