package extract

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/AwesomeCICD/circleci-org-migration-cli/api/project"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
	"gopkg.in/yaml.v3"
)

// workflowJobs0 parses cfg as YAML and returns workflows.extract.jobs[0].
func workflowJobs0(t *testing.T, cfg string) any {
	t.Helper()
	var doc map[string]any
	if err := yaml.Unmarshal([]byte(cfg), &doc); err != nil {
		t.Fatalf("generated config is not valid YAML: %v\n%s", err, cfg)
	}
	wf, ok := doc["workflows"].(map[string]any)
	if !ok {
		t.Fatalf("no workflows mapping in config:\n%s", cfg)
	}
	ex, ok := wf["extract"].(map[string]any)
	if !ok {
		t.Fatalf("no workflows.extract mapping:\n%s", cfg)
	}
	jobs, ok := ex["jobs"].([]any)
	if !ok || len(jobs) == 0 {
		t.Fatalf("workflows.extract.jobs missing/empty:\n%s", cfg)
	}
	return jobs[0]
}

// Regression: with NO contexts the job must be a bare STRING, not a `- job:`
// null mapping (which CircleCI rejects: "expected type: String, found:
// Mapping"). Found via live testing on a project with no contexts.
func TestBuildExtractConfig_NoContexts_JobIsString(t *testing.T) {
	job := workflowJobs0(t, buildExtractConfig([]string{"FOO"}, nil, nil))
	if _, ok := job.(string); !ok {
		t.Fatalf("with no contexts, jobs[0] must be a string; got %T (%v) — a null mapping fails CircleCI validation", job, job)
	}
}

// With contexts the job is a mapping carrying the context list.
func TestBuildExtractConfig_WithContexts_JobIsMapping(t *testing.T) {
	job := workflowJobs0(t, buildExtractConfig([]string{"FOO"}, []string{"ctx1"}, nil))
	m, ok := job.(map[string]any)
	if !ok {
		t.Fatalf("with contexts, jobs[0] must be a mapping; got %T (%v)", job, job)
	}
	if _, ok := m[dumpJobName]; !ok {
		t.Fatalf("job mapping missing key %q: %v", dumpJobName, m)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// buildExtractConfig unit tests
// ─────────────────────────────────────────────────────────────────────────────

func TestBuildExtractConfig_ContainsJobName(t *testing.T) {
	cfg := buildExtractConfig([]string{"SECRET_KEY"}, nil, nil)
	if !strings.Contains(cfg, dumpJobName) {
		t.Errorf("config does not contain job name %q:\n%s", dumpJobName, cfg)
	}
}

func TestBuildExtractConfig_ContainsImage(t *testing.T) {
	cfg := buildExtractConfig([]string{"FOO"}, nil, nil)
	if !strings.Contains(cfg, "cimg/base:current") {
		t.Errorf("config does not contain expected image:\n%s", cfg)
	}
}

func TestBuildExtractConfig_ContainsResourceClassSmall(t *testing.T) {
	cfg := buildExtractConfig([]string{"FOO"}, nil, nil)
	if !strings.Contains(cfg, "resource_class: small") {
		t.Errorf("config does not contain resource_class: small:\n%s", cfg)
	}
}

func TestBuildExtractConfig_ContainsStoreArtifacts(t *testing.T) {
	cfg := buildExtractConfig([]string{"FOO"}, nil, nil)
	if !strings.Contains(cfg, "store_artifacts") {
		t.Errorf("config missing store_artifacts step:\n%s", cfg)
	}
	if !strings.Contains(cfg, artifactPath) {
		t.Errorf("config missing artifact path %q:\n%s", artifactPath, cfg)
	}
}

func TestBuildExtractConfig_ContainsEnvVarNames(t *testing.T) {
	vars := []string{"SECRET_KEY", "DB_PASS", "API_TOKEN"}
	cfg := buildExtractConfig(vars, nil, nil)
	for _, v := range vars {
		if !strings.Contains(cfg, v) {
			t.Errorf("config missing env var %q:\n%s", v, cfg)
		}
	}
}

func TestBuildExtractConfig_ContainsContextNames(t *testing.T) {
	ctxs := []string{"deploy-prod", "staging-creds"}
	cfg := buildExtractConfig([]string{"FOO"}, ctxs, nil)
	for _, c := range ctxs {
		if !strings.Contains(cfg, c) {
			t.Errorf("config missing context %q:\n%s", c, cfg)
		}
	}
	if !strings.Contains(cfg, "context:") {
		t.Errorf("config missing 'context:' block:\n%s", cfg)
	}
}

func TestBuildExtractConfig_NoContextNames_NoContextBlock(t *testing.T) {
	cfg := buildExtractConfig([]string{"FOO"}, nil, nil)
	if strings.Contains(cfg, "context:") {
		t.Errorf("config should not have 'context:' when no contexts provided:\n%s", cfg)
	}
}

func TestBuildExtractConfig_IsValidYAMLish(t *testing.T) {
	cfg := buildExtractConfig([]string{"A", "B"}, []string{"ctx-1"}, nil)
	// Must start with version line.
	if !strings.HasPrefix(cfg, "version: 2.1") {
		t.Errorf("config must start with 'version: 2.1':\n%s", cfg)
	}
	// Must have jobs and workflows sections.
	if !strings.Contains(cfg, "jobs:") {
		t.Errorf("config missing 'jobs:' section:\n%s", cfg)
	}
	if !strings.Contains(cfg, "workflows:") {
		t.Errorf("config missing 'workflows:' section:\n%s", cfg)
	}
}

func TestBuildExtractConfig_EmptyVarList(t *testing.T) {
	// Should not panic; produces a valid (if useless) config.
	cfg := buildExtractConfig(nil, nil, nil)
	if !strings.Contains(cfg, dumpJobName) {
		t.Errorf("config does not contain job name with empty var list:\n%s", cfg)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// buildExtractConfig — encryption options
// ─────────────────────────────────────────────────────────────────────────────

func TestBuildExtractConfig_EncryptRecipient_EmbeddedInConfig(t *testing.T) {
	opts := &Options{EncryptRecipient: "age1fakepublickeystring"}
	cfg := buildExtractConfig([]string{"FOO"}, nil, opts)

	// Must embed the public key.
	if !strings.Contains(cfg, "age1fakepublickeystring") {
		t.Errorf("config missing embedded recipient key:\n%s", cfg)
	}
	// Must have a bundle-encrypt step.
	if !strings.Contains(cfg, "bundle-encrypt") {
		t.Errorf("config missing bundle-encrypt step:\n%s", cfg)
	}
	// Must reference the .age output path.
	if !strings.Contains(cfg, artifactPathAge) {
		t.Errorf("config missing .age artifact path:\n%s", cfg)
	}
	// Must use a trap to ensure plaintext is removed even on encrypt failure.
	wantTrap := "trap 'rm -f " + artifactPath + "' EXIT"
	if !strings.Contains(cfg, wantTrap) {
		t.Errorf("config missing trap for plaintext cleanup:\n%s", cfg)
	}
	// The trap must appear BEFORE bundle-encrypt so cleanup is registered first.
	trapIdx := strings.Index(cfg, "trap 'rm -f")
	encryptIdx := strings.Index(cfg, "bundle-encrypt")
	if trapIdx < 0 || encryptIdx < 0 || trapIdx > encryptIdx {
		t.Errorf("trap must appear before bundle-encrypt; trapIdx=%d encryptIdx=%d", trapIdx, encryptIdx)
	}
}

// TestBuildExtractConfig_Encrypt_NoRmFAfterEncrypt verifies that the old
// unconditional "rm -f <plaintext>" line no longer appears after bundle-encrypt
// (cleanup is now done via trap, not a bare rm -f that would be skipped on
// encrypt failure under set -euo pipefail).
func TestBuildExtractConfig_Encrypt_NoRmFAfterEncrypt(t *testing.T) {
	opts := &Options{EncryptRecipient: "age1fake"}
	cfg := buildExtractConfig([]string{"FOO"}, nil, opts)

	encryptIdx := strings.Index(cfg, "bundle-encrypt")
	if encryptIdx < 0 {
		t.Fatalf("bundle-encrypt not found in config:\n%s", cfg)
	}
	// The substring after bundle-encrypt must not contain a bare "rm -f <plaintext>".
	afterEncrypt := cfg[encryptIdx:]
	bare := "rm -f " + artifactPath + "\n"
	if strings.Contains(afterEncrypt, bare) {
		t.Errorf("found bare 'rm -f <plaintext>' after bundle-encrypt; cleanup should be via trap instead:\n%s", afterEncrypt)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Bug 1 — install step always present in generated config
// ─────────────────────────────────────────────────────────────────────────────

func TestBuildExtractConfig_InstallStepAlwaysPresent(t *testing.T) {
	cfg := buildExtractConfigWithVersion([]string{"FOO"}, nil, nil, "v0.4.0")

	// Must have an install step before the dump step.
	if !strings.Contains(cfg, "Install circleci-migrate") {
		t.Errorf("config missing Install circleci-migrate step:\n%s", cfg)
	}
	if !strings.Contains(cfg, "circleci-migrate version") {
		t.Errorf("config missing 'circleci-migrate version' verification:\n%s", cfg)
	}
	// The install step must appear BEFORE the dump step.
	installIdx := strings.Index(cfg, "Install circleci-migrate")
	dumpIdx := strings.Index(cfg, "Dump env vars to artifact")
	if installIdx < 0 || dumpIdx < 0 || installIdx > dumpIdx {
		t.Errorf("install step must appear before dump step; installIdx=%d dumpIdx=%d", installIdx, dumpIdx)
	}
}

func TestBuildExtractConfig_InstallStep_PinnedVersion(t *testing.T) {
	cfg := buildExtractConfigWithVersion([]string{"FOO"}, nil, nil, "v0.4.0")

	// Must pin the exact version from the binary.
	if !strings.Contains(cfg, "ver=v0.4.0") {
		t.Errorf("config should pin version v0.4.0; config:\n%s", cfg)
	}
	// Must not fall back to latest when a version is given.
	if strings.Contains(cfg, "releases/latest") {
		t.Errorf("config should not use 'latest' when a version is given:\n%s", cfg)
	}
}

func TestBuildExtractConfig_InstallStep_FallsBackToLatest_ForDevVersion(t *testing.T) {
	for _, devVer := range []string{"dev", "", "unknown"} {
		cfg := buildExtractConfigWithVersion([]string{"FOO"}, nil, nil, devVer)
		if !strings.Contains(cfg, "releases/latest") {
			t.Errorf("version=%q: expected 'latest' fallback in install step:\n%s", devVer, cfg)
		}
	}
}

func TestBuildExtractConfig_InstallStep_WithEncrypt(t *testing.T) {
	// Encryption requires circleci-migrate (for bundle-encrypt); the install
	// step must be present when encryption is requested.
	opts := &Options{EncryptRecipient: "age1fake"}
	cfg := buildExtractConfigWithVersion([]string{"FOO"}, nil, opts, "v0.4.0")

	if !strings.Contains(cfg, "Install circleci-migrate") {
		t.Errorf("install step missing when encryption is requested:\n%s", cfg)
	}
	if !strings.Contains(cfg, "bundle-encrypt") {
		t.Errorf("bundle-encrypt step missing when encryption is requested:\n%s", cfg)
	}
}

func TestBuildExtractConfig_S3Storage_HasS3UploadStep(t *testing.T) {
	opts := &Options{
		Storage:  StorageS3,
		S3Bucket: "my-bucket",
		S3Prefix: "migration/",
	}
	cfg := buildExtractConfig([]string{"FOO"}, nil, opts)

	if !strings.Contains(cfg, "aws s3 cp") {
		t.Errorf("config missing aws s3 cp step:\n%s", cfg)
	}
	if !strings.Contains(cfg, "my-bucket") {
		t.Errorf("config missing S3 bucket:\n%s", cfg)
	}
	// S3-only: no store_artifacts.
	if strings.Contains(cfg, "store_artifacts") {
		t.Errorf("config should not have store_artifacts for s3-only storage:\n%s", cfg)
	}
}

func TestBuildExtractConfig_BothStorage_HasBothSteps(t *testing.T) {
	opts := &Options{
		Storage:  StorageBoth,
		S3Bucket: "my-bucket",
		S3Prefix: "migration/",
	}
	cfg := buildExtractConfig([]string{"FOO"}, nil, opts)

	if !strings.Contains(cfg, "aws s3 cp") {
		t.Errorf("config missing aws s3 cp step:\n%s", cfg)
	}
	if !strings.Contains(cfg, "store_artifacts") {
		t.Errorf("config missing store_artifacts for both storage:\n%s", cfg)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Fake Deps for Capture tests
// ─────────────────────────────────────────────────────────────────────────────

type fakeDeps struct {
	// TriggerPipelineRun behaviour
	triggerPipelineID string
	triggerErr        error

	// GetPipelineWorkflows behaviour: list of responses returned sequentially
	workflowResponses [][]project.Workflow
	workflowErrs      []error
	workflowCallIdx   int

	// GetWorkflowJobs behaviour
	jobsResult []project.Job
	jobsErr    error

	// ListJobArtifacts behaviour
	artifactsResult []project.Artifact
	artifactsErr    error

	// DownloadArtifact behaviour
	downloadData []byte
	downloadErr  error
}

func (f *fakeDeps) TriggerPipelineRun(_ context.Context, _, _, _, _ string, _ map[string]any) (string, error) {
	return f.triggerPipelineID, f.triggerErr
}

func (f *fakeDeps) GetPipelineWorkflows(_ context.Context, _ string) ([]project.Workflow, error) {
	if f.workflowCallIdx >= len(f.workflowResponses) {
		return nil, nil
	}
	idx := f.workflowCallIdx
	f.workflowCallIdx++
	var err error
	if idx < len(f.workflowErrs) {
		err = f.workflowErrs[idx]
	}
	return f.workflowResponses[idx], err
}

func (f *fakeDeps) GetWorkflowJobs(_ context.Context, _ string) ([]project.Job, error) {
	return f.jobsResult, f.jobsErr
}

func (f *fakeDeps) ListJobArtifacts(_ context.Context, _ string, _ int) ([]project.Artifact, error) {
	return f.artifactsResult, f.artifactsErr
}

func (f *fakeDeps) DownloadArtifact(_ context.Context, _ string) ([]byte, error) {
	return f.downloadData, f.downloadErr
}

// ─────────────────────────────────────────────────────────────────────────────
// Capture happy-path tests
// ─────────────────────────────────────────────────────────────────────────────

// secretPayload builds the JSON artifact body.
func secretPayload(t *testing.T, vals map[string]string) []byte {
	t.Helper()
	data, err := json.Marshal(vals)
	if err != nil {
		t.Fatalf("marshal secret payload: %v", err)
	}
	return data
}

func TestCapture_HappyPath(t *testing.T) {
	want := map[string]string{
		"SECRET_KEY": "s3cret",
		"DB_PASS":    "hunter2",
	}

	deps := &fakeDeps{
		triggerPipelineID: "pipe-1",
		workflowResponses: [][]project.Workflow{
			// First poll: still running
			{{ID: "wf-1", Name: "extract", Status: "running"}},
			// Second poll: success
			{{ID: "wf-1", Name: "extract", Status: "success"}},
		},
		jobsResult: []project.Job{
			{Name: dumpJobName, JobNumber: 42, Status: "success"},
		},
		artifactsResult: []project.Artifact{
			{Path: artifactPath, NodeIndex: 0, URL: "https://circle-artifacts.com/0/circleci-migrate-secrets.json"},
		},
		downloadData: secretPayload(t, want),
	}

	got, err := Capture(
		context.Background(),
		deps,
		"gh/acme/web",
		[]string{"SECRET_KEY", "DB_PASS"},
		nil,
		Options{DefinitionID: "def-1", Branch: "main", PollInterval: time.Millisecond},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("key %q: got %q want %q", k, got[k], v)
		}
	}
}

func TestCapture_MissingDefinitionID(t *testing.T) {
	_, err := Capture(context.Background(), &fakeDeps{}, "gh/acme/web", nil, nil, Options{})
	if err == nil {
		t.Fatal("expected error when DefinitionID is empty, got nil")
	}
	if !strings.Contains(err.Error(), "DefinitionID") {
		t.Errorf("error %q does not mention DefinitionID", err.Error())
	}
}

func TestCapture_TriggerError(t *testing.T) {
	deps := &fakeDeps{
		triggerErr: fmt.Errorf("network error"),
	}
	_, err := Capture(context.Background(), deps, "gh/acme/web", nil, nil, Options{DefinitionID: "def-1"})
	if err == nil {
		t.Fatal("expected trigger error, got nil")
	}
}

func TestCapture_PipelineSkipped(t *testing.T) {
	deps := &fakeDeps{
		triggerErr: project.ErrPipelineSkipped,
	}
	_, err := Capture(context.Background(), deps, "gh/acme/web", nil, nil, Options{DefinitionID: "def-1"})
	if err == nil {
		t.Fatal("expected error on skipped pipeline, got nil")
	}
}

func TestCapture_WorkflowFailed(t *testing.T) {
	deps := &fakeDeps{
		triggerPipelineID: "pipe-1",
		workflowResponses: [][]project.Workflow{
			{{ID: "wf-1", Name: "extract", Status: "failed"}},
		},
	}
	_, err := Capture(
		context.Background(), deps, "gh/acme/web", nil, nil,
		Options{DefinitionID: "def-1", PollInterval: time.Millisecond},
	)
	if err == nil {
		t.Fatal("expected error on failed workflow, got nil")
	}
	if !strings.Contains(err.Error(), "failed") {
		t.Errorf("error %q does not mention 'failed'", err.Error())
	}
}

func TestCapture_NoArtifact(t *testing.T) {
	deps := &fakeDeps{
		triggerPipelineID: "pipe-1",
		workflowResponses: [][]project.Workflow{
			{{ID: "wf-1", Name: "extract", Status: "success"}},
		},
		jobsResult:      []project.Job{{Name: dumpJobName, JobNumber: 1, Status: "success"}},
		artifactsResult: []project.Artifact{}, // empty — no artifact
	}
	_, err := Capture(
		context.Background(), deps, "gh/acme/web", nil, nil,
		Options{DefinitionID: "def-1", PollInterval: time.Millisecond},
	)
	if !strings.Contains(err.Error(), "no secrets artifact") {
		t.Errorf("expected ErrNoArtifact, got: %v", err)
	}
}

func TestCapture_PollTimeout(t *testing.T) {
	// Every workflow call returns "running"; the timeout should fire.
	deps := &fakeDeps{
		triggerPipelineID: "pipe-1",
		// Return "running" indefinitely by making the slice very long.
		workflowResponses: func() [][]project.Workflow {
			s := make([][]project.Workflow, 100)
			for i := range s {
				s[i] = []project.Workflow{{ID: "wf-1", Name: "extract", Status: "running"}}
			}
			return s
		}(),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := Capture(ctx, deps, "gh/acme/web", nil, nil,
		Options{DefinitionID: "def-1", PollInterval: 5 * time.Millisecond})
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
}

func TestCapture_JobNotFound(t *testing.T) {
	deps := &fakeDeps{
		triggerPipelineID: "pipe-1",
		workflowResponses: [][]project.Workflow{
			{{ID: "wf-1", Name: "extract", Status: "success"}},
		},
		jobsResult: []project.Job{
			{Name: "some-other-job", JobNumber: 1, Status: "success"},
		},
	}
	_, err := Capture(
		context.Background(), deps, "gh/acme/web", nil, nil,
		Options{DefinitionID: "def-1", PollInterval: time.Millisecond},
	)
	if err == nil {
		t.Fatal("expected error when dump job not found, got nil")
	}
}

func TestCapture_ArtifactDownloadError(t *testing.T) {
	deps := &fakeDeps{
		triggerPipelineID: "pipe-1",
		workflowResponses: [][]project.Workflow{
			{{ID: "wf-1", Name: "extract", Status: "success"}},
		},
		jobsResult: []project.Job{{Name: dumpJobName, JobNumber: 1, Status: "success"}},
		artifactsResult: []project.Artifact{
			{Path: artifactPath, URL: "https://circle-artifacts.com/0/circleci-migrate-secrets.json"},
		},
		downloadErr: fmt.Errorf("download failed"),
	}
	_, err := Capture(
		context.Background(), deps, "gh/acme/web", nil, nil,
		Options{DefinitionID: "def-1", PollInterval: time.Millisecond},
	)
	if err == nil {
		t.Fatal("expected download error, got nil")
	}
}

func TestCapture_BadArtifactJSON(t *testing.T) {
	deps := &fakeDeps{
		triggerPipelineID: "pipe-1",
		workflowResponses: [][]project.Workflow{
			{{ID: "wf-1", Name: "extract", Status: "success"}},
		},
		jobsResult: []project.Job{{Name: dumpJobName, JobNumber: 1, Status: "success"}},
		artifactsResult: []project.Artifact{
			{Path: artifactPath, URL: "https://circle-artifacts.com/0/circleci-migrate-secrets.json"},
		},
		downloadData: []byte("NOT_JSON"),
	}
	_, err := Capture(
		context.Background(), deps, "gh/acme/web", nil, nil,
		Options{DefinitionID: "def-1", PollInterval: time.Millisecond},
	)
	if err == nil {
		t.Fatal("expected JSON parse error, got nil")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// buildSSHKeyExtractConfig unit tests
// ─────────────────────────────────────────────────────────────────────────────

func TestBuildSSHKeyExtractConfig_ContainsJobName(t *testing.T) {
	keys := []SSHKeyInput{{Fingerprint: "abc123=", Hostname: "github.com"}}
	cfg := buildSSHKeyExtractConfig(keys, nil)
	if !strings.Contains(cfg, sshKeyDumpJobName) {
		t.Errorf("config does not contain SSH key job name %q:\n%s", sshKeyDumpJobName, cfg)
	}
}

func TestBuildSSHKeyExtractConfig_ContainsDockerImage(t *testing.T) {
	keys := []SSHKeyInput{{Fingerprint: "abc123=", Hostname: "github.com"}}
	cfg := buildSSHKeyExtractConfig(keys, nil)
	if !strings.Contains(cfg, "cimg/base:current") {
		t.Errorf("config does not contain expected docker image:\n%s", cfg)
	}
}

func TestBuildSSHKeyExtractConfig_NoCheckoutStep(t *testing.T) {
	keys := []SSHKeyInput{{Fingerprint: "abc123=", Hostname: "github.com"}}
	cfg := buildSSHKeyExtractConfig(keys, nil)
	// The checkout step must never appear — it would materialise the deploy key.
	if strings.Contains(cfg, "- checkout") {
		t.Errorf("config must NOT contain a checkout step (would materialise deploy key):\n%s", cfg)
	}
}

func TestBuildSSHKeyExtractConfig_AddSSHKeys_ExplicitFingerprints(t *testing.T) {
	keys := []SSHKeyInput{
		{Fingerprint: "Cv1BbZPFHMZzCPx+1CsJqO0kRBIlOm7DEqR/jPbHnBg=", Hostname: "github.com"},
		{Fingerprint: "XYZabc123def456=", Hostname: "bitbucket.org"},
	}
	cfg := buildSSHKeyExtractConfig(keys, nil)

	// Must have add_ssh_keys step.
	if !strings.Contains(cfg, "add_ssh_keys:") {
		t.Errorf("config missing add_ssh_keys step:\n%s", cfg)
	}
	// Each fingerprint must appear with "SHA256:" prefix.
	for _, k := range keys {
		want := `"SHA256:` + k.Fingerprint + `"`
		if !strings.Contains(cfg, want) {
			t.Errorf("config missing fingerprint %q:\n%s", want, cfg)
		}
	}
}

func TestBuildSSHKeyExtractConfig_MatchByRecomputedSHA256(t *testing.T) {
	keys := []SSHKeyInput{{Fingerprint: "abc123=", Hostname: "example.com"}}
	cfg := buildSSHKeyExtractConfig(keys, nil)

	// Script must compute SHA256 fingerprint via ssh-keygen -E sha256.
	if !strings.Contains(cfg, "ssh-keygen") {
		t.Errorf("config missing ssh-keygen command for fingerprint computation:\n%s", cfg)
	}
	if !strings.Contains(cfg, "sha256") {
		t.Errorf("config missing sha256 in ssh-keygen invocation:\n%s", cfg)
	}
	// Match step: the script must look up the fingerprint in a catalog.
	if !strings.Contains(cfg, "FP_TO_HOST") {
		t.Errorf("config missing FP_TO_HOST lookup map:\n%s", cfg)
	}
}

func TestBuildSSHKeyExtractConfig_HostnameEmbedded(t *testing.T) {
	keys := []SSHKeyInput{{Fingerprint: "abc123=", Hostname: "deploy.example.com"}}
	cfg := buildSSHKeyExtractConfig(keys, nil)
	// The hostname must appear in the FP_TO_HOST initialisation.
	if !strings.Contains(cfg, "deploy.example.com") {
		t.Errorf("config missing hostname 'deploy.example.com':\n%s", cfg)
	}
}

func TestBuildSSHKeyExtractConfig_StoreArtifact(t *testing.T) {
	keys := []SSHKeyInput{{Fingerprint: "abc123=", Hostname: "github.com"}}
	cfg := buildSSHKeyExtractConfig(keys, nil)
	if !strings.Contains(cfg, "store_artifacts") {
		t.Errorf("config missing store_artifacts step:\n%s", cfg)
	}
	if !strings.Contains(cfg, sshKeyArtifactPath) {
		t.Errorf("config missing artifact path %q:\n%s", sshKeyArtifactPath, cfg)
	}
}

func TestBuildSSHKeyExtractConfig_IsValidYAMLish(t *testing.T) {
	keys := []SSHKeyInput{{Fingerprint: "abc123=", Hostname: "github.com"}}
	cfg := buildSSHKeyExtractConfig(keys, nil)
	if !strings.HasPrefix(cfg, "version: 2.1") {
		t.Errorf("config must start with 'version: 2.1':\n%s", cfg)
	}
	if !strings.Contains(cfg, "jobs:") {
		t.Errorf("config missing 'jobs:' section:\n%s", cfg)
	}
	if !strings.Contains(cfg, "workflows:") {
		t.Errorf("config missing 'workflows:' section:\n%s", cfg)
	}
}

func TestBuildSSHKeyExtractConfig_WithEncryption(t *testing.T) {
	keys := []SSHKeyInput{{Fingerprint: "abc123=", Hostname: "github.com"}}
	opts := &Options{EncryptRecipient: "age1fakepublickey"}
	cfg := buildSSHKeyExtractConfig(keys, opts)

	// Must embed the public key.
	if !strings.Contains(cfg, "age1fakepublickey") {
		t.Errorf("config missing embedded recipient key:\n%s", cfg)
	}
	// Must have bundle-encrypt step.
	if !strings.Contains(cfg, "bundle-encrypt") {
		t.Errorf("config missing bundle-encrypt step:\n%s", cfg)
	}
	// Must store the .age artifact.
	if !strings.Contains(cfg, sshKeyArtifactPathAge) {
		t.Errorf("config missing .age artifact path:\n%s", cfg)
	}
	// Must have trap for plaintext cleanup.
	wantTrap := "trap 'rm -f " + sshKeyArtifactPath + "' EXIT"
	if !strings.Contains(cfg, wantTrap) {
		t.Errorf("config missing trap for plaintext cleanup:\n%s", cfg)
	}
	// Trap must appear before bundle-encrypt.
	trapIdx := strings.Index(cfg, "trap 'rm -f")
	encryptIdx := strings.Index(cfg, "bundle-encrypt")
	if trapIdx < 0 || encryptIdx < 0 || trapIdx > encryptIdx {
		t.Errorf("trap must appear before bundle-encrypt; trapIdx=%d encryptIdx=%d", trapIdx, encryptIdx)
	}
}

func TestBuildSSHKeyExtractConfig_WithEncryption_ArtifactIsAge(t *testing.T) {
	keys := []SSHKeyInput{{Fingerprint: "abc123=", Hostname: "github.com"}}
	opts := &Options{EncryptRecipient: "age1fake"}
	cfg := buildSSHKeyExtractConfig(keys, opts)

	// The store_artifacts path must be the .age file, not the plaintext.
	if !strings.Contains(cfg, "circleci-migrate-sshkeys.json.age") {
		t.Errorf("store_artifacts must use .age path when encryption is on:\n%s", cfg)
	}
}

func TestBuildSSHKeyExtractConfig_InstallStepPresent(t *testing.T) {
	keys := []SSHKeyInput{{Fingerprint: "abc123=", Hostname: "github.com"}}
	cfg := buildSSHKeyExtractConfigWithVersion(keys, nil, "v0.4.0")

	// Install step must be present (needed for bundle-encrypt even when
	// encryption is not requested, in case it is added later).
	if !strings.Contains(cfg, "Install circleci-migrate") {
		t.Errorf("config missing Install circleci-migrate step:\n%s", cfg)
	}
	// Install step must appear before the collect step.
	installIdx := strings.Index(cfg, "Install circleci-migrate")
	collectIdx := strings.Index(cfg, "Collect SSH private keys")
	if installIdx < 0 || collectIdx < 0 || installIdx > collectIdx {
		t.Errorf("install step must appear before collect step; installIdx=%d collectIdx=%d", installIdx, collectIdx)
	}
}

func TestBuildSSHKeyExtractConfig_MultipleKeys(t *testing.T) {
	keys := []SSHKeyInput{
		{Fingerprint: "fp1aaa=", Hostname: "github.com"},
		{Fingerprint: "fp2bbb=", Hostname: "bitbucket.org"},
		{Fingerprint: "fp3ccc=", Hostname: ""},
	}
	cfg := buildSSHKeyExtractConfig(keys, nil)

	for _, k := range keys {
		want := `"SHA256:` + k.Fingerprint + `"`
		if !strings.Contains(cfg, want) {
			t.Errorf("config missing fingerprint %q:\n%s", want, cfg)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// CaptureSSHKeys happy-path and error tests
// ─────────────────────────────────────────────────────────────────────────────

// sshKeyPayload builds the JSON artifact body for SSH key capture tests.
func sshKeyPayload(t *testing.T, keys []manifest.BundleSSHKey) []byte {
	t.Helper()
	data, err := json.Marshal(keys)
	if err != nil {
		t.Fatalf("marshal ssh key payload: %v", err)
	}
	return data
}

func TestCaptureSSHKeys_HappyPath(t *testing.T) {
	want := []manifest.BundleSSHKey{
		{
			Fingerprint: "Cv1BbZPFHMZzCPx+1CsJqO0kRBIlOm7DEqR/jPbHnBg=",
			Hostname:    "github.com",
			PrivateKey:  "PLACEHOLDER-PRIVATE-KEY-NOT-REAL",
		},
	}

	deps := &fakeDeps{
		triggerPipelineID: "pipe-ssh-1",
		workflowResponses: [][]project.Workflow{
			{{ID: "wf-ssh-1", Name: "extract-ssh", Status: "running"}},
			{{ID: "wf-ssh-1", Name: "extract-ssh", Status: "success"}},
		},
		jobsResult: []project.Job{
			{Name: sshKeyDumpJobName, JobNumber: 7, Status: "success"},
		},
		artifactsResult: []project.Artifact{
			{
				Path: sshKeyArtifactPath,
				URL:  "https://circle-artifacts.com/0/circleci-migrate-sshkeys.json",
			},
		},
		downloadData: sshKeyPayload(t, want),
	}

	keys := []SSHKeyInput{
		{
			Fingerprint: "Cv1BbZPFHMZzCPx+1CsJqO0kRBIlOm7DEqR/jPbHnBg=",
			Hostname:    "github.com",
		},
	}

	got, err := CaptureSSHKeys(
		context.Background(),
		deps,
		"gh/acme/web",
		keys,
		Options{DefinitionID: "def-ssh-1", Branch: "main", PollInterval: time.Millisecond},
		"",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 SSH key, got %d", len(got))
	}
	if got[0].Fingerprint != want[0].Fingerprint {
		t.Errorf("Fingerprint: got %q want %q", got[0].Fingerprint, want[0].Fingerprint)
	}
	if got[0].Hostname != want[0].Hostname {
		t.Errorf("Hostname: got %q want %q", got[0].Hostname, want[0].Hostname)
	}
	if got[0].PrivateKey != want[0].PrivateKey {
		t.Errorf("PrivateKey: got %q want %q", got[0].PrivateKey, want[0].PrivateKey)
	}
}

func TestCaptureSSHKeys_EmptyKeys_ReturnsNil(t *testing.T) {
	// When no keys are requested, CaptureSSHKeys returns (nil, nil) without
	// triggering any pipeline.
	deps := &fakeDeps{}
	got, err := CaptureSSHKeys(
		context.Background(),
		deps,
		"gh/acme/web",
		nil, // no keys
		Options{DefinitionID: "def-1"},
		"",
	)
	if err != nil {
		t.Fatalf("unexpected error for empty keys: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for empty key list, got %v", got)
	}
}

func TestCaptureSSHKeys_MissingDefinitionID_Error(t *testing.T) {
	keys := []SSHKeyInput{{Fingerprint: "fp1=", Hostname: "github.com"}}
	_, err := CaptureSSHKeys(context.Background(), &fakeDeps{}, "gh/acme/web", keys, Options{}, "")
	if err == nil {
		t.Fatal("expected error when DefinitionID is empty, got nil")
	}
	if !strings.Contains(err.Error(), "DefinitionID") {
		t.Errorf("error %q does not mention DefinitionID", err.Error())
	}
}

func TestCaptureSSHKeys_WorkflowFailed(t *testing.T) {
	deps := &fakeDeps{
		triggerPipelineID: "pipe-1",
		workflowResponses: [][]project.Workflow{
			{{ID: "wf-1", Name: "extract-ssh", Status: "failed"}},
		},
	}
	keys := []SSHKeyInput{{Fingerprint: "fp1=", Hostname: "github.com"}}
	_, err := CaptureSSHKeys(
		context.Background(), deps, "gh/acme/web", keys,
		Options{DefinitionID: "def-1", PollInterval: time.Millisecond},
		"",
	)
	if err == nil {
		t.Fatal("expected error on failed workflow, got nil")
	}
}

func TestCaptureSSHKeys_NoArtifact_Error(t *testing.T) {
	deps := &fakeDeps{
		triggerPipelineID: "pipe-1",
		workflowResponses: [][]project.Workflow{
			{{ID: "wf-1", Name: "extract-ssh", Status: "success"}},
		},
		jobsResult:      []project.Job{{Name: sshKeyDumpJobName, JobNumber: 1, Status: "success"}},
		artifactsResult: []project.Artifact{},
	}
	keys := []SSHKeyInput{{Fingerprint: "fp1=", Hostname: "github.com"}}
	_, err := CaptureSSHKeys(
		context.Background(), deps, "gh/acme/web", keys,
		Options{DefinitionID: "def-1", PollInterval: time.Millisecond},
		"",
	)
	if err == nil {
		t.Fatal("expected ErrNoArtifact, got nil")
	}
}

func TestCaptureSSHKeys_BadArtifactJSON(t *testing.T) {
	deps := &fakeDeps{
		triggerPipelineID: "pipe-1",
		workflowResponses: [][]project.Workflow{
			{{ID: "wf-1", Name: "extract-ssh", Status: "success"}},
		},
		jobsResult: []project.Job{{Name: sshKeyDumpJobName, JobNumber: 1, Status: "success"}},
		artifactsResult: []project.Artifact{
			{Path: sshKeyArtifactPath, URL: "https://circle-artifacts.com/0/circleci-migrate-sshkeys.json"},
		},
		downloadData: []byte("NOT_JSON"),
	}
	keys := []SSHKeyInput{{Fingerprint: "fp1=", Hostname: "github.com"}}
	_, err := CaptureSSHKeys(
		context.Background(), deps, "gh/acme/web", keys,
		Options{DefinitionID: "def-1", PollInterval: time.Millisecond},
		"",
	)
	if err == nil {
		t.Fatal("expected JSON parse error, got nil")
	}
}

func TestCaptureSSHKeys_PipelineSkipped(t *testing.T) {
	deps := &fakeDeps{
		triggerErr: project.ErrPipelineSkipped,
	}
	keys := []SSHKeyInput{{Fingerprint: "fp1=", Hostname: "github.com"}}
	_, err := CaptureSSHKeys(
		context.Background(), deps, "gh/acme/web", keys,
		Options{DefinitionID: "def-1"},
		"",
	)
	if err == nil {
		t.Fatal("expected error on skipped pipeline, got nil")
	}
}

func TestCaptureSSHKeys_TriggerError(t *testing.T) {
	deps := &fakeDeps{
		triggerErr: fmt.Errorf("network error"),
	}
	keys := []SSHKeyInput{{Fingerprint: "fp1=", Hostname: "github.com"}}
	_, err := CaptureSSHKeys(
		context.Background(), deps, "gh/acme/web", keys,
		Options{DefinitionID: "def-1"},
		"",
	)
	if err == nil {
		t.Fatal("expected trigger error, got nil")
	}
}

func TestCaptureSSHKeys_JobNotFound(t *testing.T) {
	deps := &fakeDeps{
		triggerPipelineID: "pipe-1",
		workflowResponses: [][]project.Workflow{
			{{ID: "wf-1", Name: "extract-ssh", Status: "success"}},
		},
		jobsResult: []project.Job{
			{Name: "some-other-job", JobNumber: 1, Status: "success"},
		},
	}
	keys := []SSHKeyInput{{Fingerprint: "fp1=", Hostname: "github.com"}}
	_, err := CaptureSSHKeys(
		context.Background(), deps, "gh/acme/web", keys,
		Options{DefinitionID: "def-1", PollInterval: time.Millisecond},
		"",
	)
	if err == nil {
		t.Fatal("expected error when SSH key dump job not found, got nil")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// manifest.SecretBundle SSH-key helpers
// ─────────────────────────────────────────────────────────────────────────────

func TestBundleAddSSHKey_Idempotent(t *testing.T) {
	b := manifest.NewSecretBundle()
	key := manifest.BundleSSHKey{
		Fingerprint: "fp1=",
		Hostname:    "github.com",
		PrivateKey:  "PLACEHOLDER-KEY-MATERIAL",
	}

	b.AddSSHKey("gh/acme/web", key)
	b.AddSSHKey("gh/acme/web", key) // duplicate — should be skipped

	if len(b.SSHKeys["gh/acme/web"]) != 1 {
		t.Errorf("expected 1 key after duplicate add, got %d", len(b.SSHKeys["gh/acme/web"]))
	}
}

func TestBundleAddSSHKey_MultipleKeys(t *testing.T) {
	b := manifest.NewSecretBundle()
	b.AddSSHKey("gh/acme/web", manifest.BundleSSHKey{Fingerprint: "fp1=", Hostname: "github.com", PrivateKey: "PLACEHOLDER-KEY-1"})
	b.AddSSHKey("gh/acme/web", manifest.BundleSSHKey{Fingerprint: "fp2=", Hostname: "bitbucket.org", PrivateKey: "PLACEHOLDER-KEY-2"})

	if len(b.SSHKeys["gh/acme/web"]) != 2 {
		t.Errorf("expected 2 keys, got %d", len(b.SSHKeys["gh/acme/web"]))
	}
}

func TestBundleAddSSHKey_MultipleProjects(t *testing.T) {
	b := manifest.NewSecretBundle()
	b.AddSSHKey("gh/acme/web", manifest.BundleSSHKey{Fingerprint: "fp1=", Hostname: "github.com", PrivateKey: "PLACEHOLDER-KEY-1"})
	b.AddSSHKey("gh/acme/api", manifest.BundleSSHKey{Fingerprint: "fp2=", Hostname: "github.com", PrivateKey: "PLACEHOLDER-KEY-2"})

	if len(b.SSHKeys["gh/acme/web"]) != 1 {
		t.Errorf("expected 1 key for web, got %d", len(b.SSHKeys["gh/acme/web"]))
	}
	if len(b.SSHKeys["gh/acme/api"]) != 1 {
		t.Errorf("expected 1 key for api, got %d", len(b.SSHKeys["gh/acme/api"]))
	}
}

func TestBundleMerge_MergesSSHKeys(t *testing.T) {
	a := manifest.NewSecretBundle()
	a.AddSSHKey("gh/acme/web", manifest.BundleSSHKey{Fingerprint: "fp1=", Hostname: "github.com", PrivateKey: "PLACEHOLDER-KEY-1"})

	b := manifest.NewSecretBundle()
	b.AddSSHKey("gh/acme/web", manifest.BundleSSHKey{Fingerprint: "fp2=", Hostname: "bitbucket.org", PrivateKey: "PLACEHOLDER-KEY-2"})
	b.AddSSHKey("gh/acme/api", manifest.BundleSSHKey{Fingerprint: "fp3=", Hostname: "gitlab.com", PrivateKey: "PLACEHOLDER-KEY-3"})

	a.Merge(b)

	if len(a.SSHKeys["gh/acme/web"]) != 2 {
		t.Errorf("expected 2 keys for web after merge, got %d", len(a.SSHKeys["gh/acme/web"]))
	}
	if len(a.SSHKeys["gh/acme/api"]) != 1 {
		t.Errorf("expected 1 key for api after merge, got %d", len(a.SSHKeys["gh/acme/api"]))
	}
}

func TestBundleMerge_SSHKeyDuplicatesDeduped(t *testing.T) {
	a := manifest.NewSecretBundle()
	a.AddSSHKey("gh/acme/web", manifest.BundleSSHKey{Fingerprint: "fp1=", Hostname: "github.com", PrivateKey: "PLACEHOLDER-KEY-1"})

	b := manifest.NewSecretBundle()
	// Same fingerprint as in a — must be deduplicated on merge.
	b.AddSSHKey("gh/acme/web", manifest.BundleSSHKey{Fingerprint: "fp1=", Hostname: "github.com", PrivateKey: "PLACEHOLDER-KEY-1"})

	a.Merge(b)

	if len(a.SSHKeys["gh/acme/web"]) != 1 {
		t.Errorf("expected 1 key after merging duplicate, got %d", len(a.SSHKeys["gh/acme/web"]))
	}
}
