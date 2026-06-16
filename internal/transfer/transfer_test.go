package transfer

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	apicontext "github.com/AwesomeCICD/circleci-org-migration-cli/api/context"
	"github.com/AwesomeCICD/circleci-org-migration-cli/api/project"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
)

// ─────────────────────────────────────────────────────────────────────────────
// sanitizeName
// ─────────────────────────────────────────────────────────────────────────────

func TestSanitizeName(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"deploy-prod", "deploy-prod"},
		{"Deploy Prod", "deploy-prod"},
		{"my_context", "my-context"},
		{"ctx.v2", "ctx-v2"},
		{"  spaces  ", "spaces"},
		{"123abc", "123abc"},
		{"---", "ctx"},
		{"", "ctx"},
	}
	for _, tc := range cases {
		got := sanitizeName(tc.input)
		if got != tc.want {
			t.Errorf("sanitizeName(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// BuildPlan
// ─────────────────────────────────────────────────────────────────────────────

func baseManifest() *manifest.Manifest {
	return &manifest.Manifest{
		Source: manifest.Source{Org: manifest.Org{ID: "src-org-uuid"}},
		Contexts: []manifest.Context{
			{
				Name: "deploy-prod",
				EnvVars: []manifest.ContextEnvVar{
					{Name: "AWS_KEY"},
					{Name: "AWS_SECRET"},
				},
			},
			{
				Name: "shared",
				EnvVars: []manifest.ContextEnvVar{
					{Name: "NPM_TOKEN"},
				},
			},
			{
				Name:    "empty-ctx",
				EnvVars: nil,
			},
		},
		Projects: []manifest.Project{
			{Slug: "gh/acme/web"},
		},
	}
}

func baseOpts() Options {
	return Options{
		DestOrgID:        "dest-org-uuid",
		DestTokenContext: "migration-secrets",
		DryRun:           true,
		Stdout:           &bytes.Buffer{},
		Stderr:           &bytes.Buffer{},
	}
}

func TestBuildPlan_HappyPath(t *testing.T) {
	m := baseManifest()
	opts := baseOpts()

	plan, err := BuildPlan(m, &opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// empty-ctx should be excluded (no env vars).
	if len(plan.Contexts) != 2 {
		t.Fatalf("expected 2 contexts, got %d: %v", len(plan.Contexts), plan.Contexts)
	}

	// Verify var names are sorted.
	deployCtx := plan.Contexts[0]
	if deployCtx.SourceName != "deploy-prod" {
		t.Errorf("expected deploy-prod first, got %q", deployCtx.SourceName)
	}
	if len(deployCtx.VarNames) != 2 {
		t.Fatalf("expected 2 vars for deploy-prod, got %d", len(deployCtx.VarNames))
	}
	if deployCtx.VarNames[0] != "AWS_KEY" || deployCtx.VarNames[1] != "AWS_SECRET" {
		t.Errorf("vars not sorted: %v", deployCtx.VarNames)
	}
	if plan.TotalVars() != 3 {
		t.Errorf("expected 3 total vars, got %d", plan.TotalVars())
	}
	if plan.DestTokenContext != "migration-secrets" {
		t.Errorf("dest token context = %q, want migration-secrets", plan.DestTokenContext)
	}
	if plan.DestTokenEnvVar != "CIRCLECI_DEST_TOKEN" {
		t.Errorf("dest token env var = %q, want CIRCLECI_DEST_TOKEN", plan.DestTokenEnvVar)
	}
}

func TestBuildPlan_DestOrgIDRequired(t *testing.T) {
	m := baseManifest()
	opts := baseOpts()
	opts.DestOrgID = ""

	_, err := BuildPlan(m, &opts)
	if err == nil {
		t.Fatal("expected error when DestOrgID is empty")
	}
	if !strings.Contains(err.Error(), "--dest-org-id") {
		t.Errorf("error should mention --dest-org-id, got: %v", err)
	}
}

func TestBuildPlan_DestTokenContextRequired(t *testing.T) {
	m := baseManifest()
	opts := baseOpts()
	opts.DestTokenContext = ""

	_, err := BuildPlan(m, &opts)
	if err == nil {
		t.Fatal("expected error when DestTokenContext is empty")
	}
	if !strings.Contains(err.Error(), "--dest-token-context") {
		t.Errorf("error should mention --dest-token-context, got: %v", err)
	}
}

func TestBuildPlan_NoContextsWithVars_Error(t *testing.T) {
	m := &manifest.Manifest{
		Contexts: []manifest.Context{
			{Name: "empty", EnvVars: nil},
		},
	}
	opts := baseOpts()

	_, err := BuildPlan(m, &opts)
	if err == nil {
		t.Fatal("expected error when no contexts have vars")
	}
}

func TestBuildPlan_SelectedContextNames(t *testing.T) {
	m := baseManifest()
	opts := baseOpts()
	opts.SelectedContextNames = map[string]bool{"deploy-prod": true}

	plan, err := BuildPlan(m, &opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(plan.Contexts) != 1 {
		t.Fatalf("expected 1 context (filtered), got %d", len(plan.Contexts))
	}
	if plan.Contexts[0].SourceName != "deploy-prod" {
		t.Errorf("expected deploy-prod, got %q", plan.Contexts[0].SourceName)
	}
}

func TestBuildPlan_Mapping(t *testing.T) {
	m := baseManifest()
	opts := baseOpts()
	opts.Mapping = map[string]string{
		"deploy-prod": "prod-deployment",
	}

	plan, err := BuildPlan(m, &opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, cp := range plan.Contexts {
		if cp.SourceName == "deploy-prod" {
			if cp.DestName != "prod-deployment" {
				t.Errorf("deploy-prod dest name = %q, want prod-deployment", cp.DestName)
			}
		}
		if cp.SourceName == "shared" {
			if cp.DestName != "shared" {
				t.Errorf("shared dest name = %q, want shared (identity)", cp.DestName)
			}
		}
	}
}

func TestBuildPlan_CustomDestTokenEnvVar(t *testing.T) {
	m := baseManifest()
	opts := baseOpts()
	opts.DestTokenEnvVar = "MY_DEST_TOKEN"

	plan, err := BuildPlan(m, &opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if plan.DestTokenEnvVar != "MY_DEST_TOKEN" {
		t.Errorf("dest token env var = %q, want MY_DEST_TOKEN", plan.DestTokenEnvVar)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// buildTransferConfigWithVersion
// ─────────────────────────────────────────────────────────────────────────────

func TestBuildTransferConfig_ContainsContextAndJob(t *testing.T) {
	m := baseManifest()
	opts := baseOpts()
	opts.DestTokenContext = "migration-secrets"

	plan, err := BuildPlan(m, &opts)
	if err != nil {
		t.Fatalf("plan error: %v", err)
	}

	cfg := buildTransferConfigWithVersion(m, plan.Contexts, nil, &opts)

	// Must contain the job names derived from context names.
	if !strings.Contains(cfg, "circleci-migrate-transfer-deploy-prod") {
		t.Error("expected job name for deploy-prod")
	}
	if !strings.Contains(cfg, "circleci-migrate-transfer-shared") {
		t.Error("expected job name for shared")
	}

	// Must reference both the source context and the dest-token context.
	if !strings.Contains(cfg, "- deploy-prod") {
		t.Error("expected source context 'deploy-prod' in workflow context list")
	}
	if !strings.Contains(cfg, "- migration-secrets") {
		t.Error("expected dest-token context 'migration-secrets' in workflow context list")
	}

	// Must reference the dest org ID and host.
	if !strings.Contains(cfg, "dest-org-uuid") {
		t.Error("expected dest org ID in config")
	}

	// Dest token value must NOT appear (it's referenced by env-var name only).
	if strings.Contains(cfg, "actual-secret-token") {
		t.Error("config must not contain the actual dest token value")
	}

	// Must contain the PUT endpoint pattern.
	if !strings.Contains(cfg, "/api/v2/context/") {
		t.Error("expected CircleCI context API endpoint in config")
	}

	// Must reference env var names (not values).
	if !strings.Contains(cfg, "AWS_KEY") {
		t.Error("expected AWS_KEY env var name in config")
	}
	if !strings.Contains(cfg, "AWS_SECRET") {
		t.Error("expected AWS_SECRET env var name in config")
	}
}

func TestBuildTransferConfig_NoDestTokenContextDuplicated(t *testing.T) {
	// When the dest-token context is the same as the source context, it should
	// only appear once in the workflow context list.
	m := &manifest.Manifest{
		Contexts: []manifest.Context{
			{
				Name:    "migration-secrets",
				EnvVars: []manifest.ContextEnvVar{{Name: "CIRCLECI_DEST_TOKEN"}},
			},
		},
	}
	opts := baseOpts()
	opts.DestTokenContext = "migration-secrets" // same as the only context

	plan, err := BuildPlan(m, &opts)
	if err != nil {
		t.Fatalf("plan error: %v", err)
	}

	cfg := buildTransferConfigWithVersion(m, plan.Contexts, nil, &opts)

	// The context should appear only once in the workflow context list.
	count := strings.Count(cfg, "- migration-secrets")
	if count != 1 {
		t.Errorf("expected migration-secrets to appear once in context list, got %d", count)
	}
}

func TestBuildTransferConfig_NoInstallStep(t *testing.T) {
	// Transfer jobs do NOT install circleci-migrate — all work is done via
	// curl + jq which are already available in cimg/base:current. The install
	// step caused a 404 when the embedded version lacked a 'v' prefix.
	m := baseManifest()
	opts := baseOpts()
	opts.DestTokenContext = "migration-secrets"

	plan, _ := BuildPlan(m, &opts)

	cfg := buildTransferConfigWithVersion(m, plan.Contexts, nil, &opts)
	if strings.Contains(cfg, "Install circleci-migrate") {
		t.Error("transfer config must NOT contain an install step (transfer uses curl+jq only)")
	}
	if strings.Contains(cfg, "circleci-migrate version") {
		t.Error("transfer config must NOT verify circleci-migrate binary (no install step)")
	}
}

func TestBuildTransferConfig_DestHostEmbedded(t *testing.T) {
	m := baseManifest()
	opts := baseOpts()
	opts.DestTokenContext = "migration-secrets"
	opts.DestHost = "https://circleci.example.com"

	plan, _ := BuildPlan(m, &opts)
	cfg := buildTransferConfigWithVersion(m, plan.Contexts, nil, &opts)

	if !strings.Contains(cfg, "circleci.example.com") {
		t.Error("expected custom dest host in config")
	}
}

func TestBuildTransferConfig_NoPLAINTEXTValues(t *testing.T) {
	// Paranoia: make sure the config does not contain any literal "secret" value.
	// The values come from the job environment, not from the generated config.
	m := baseManifest()
	opts := baseOpts()
	opts.DestTokenContext = "migration-secrets"

	plan, _ := BuildPlan(m, &opts)
	cfg := buildTransferConfigWithVersion(m, plan.Contexts, nil, &opts)

	// These strings must never appear in the generated config.
	forbidden := []string{
		"actual-secret-value",
		"s3cr3t",
		"password",
	}
	for _, s := range forbidden {
		if strings.Contains(cfg, s) {
			t.Errorf("config must not contain %q", s)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// fakeTransferDeps — test double for Deps
// ─────────────────────────────────────────────────────────────────────────────

type fakeTransferDeps struct {
	proj       *project.Project
	projErr    error
	defs       []project.PipelineDefinition
	defsErr    error
	triggerID  string
	triggerErr error
	workflows  [][]project.Workflow
	wfIdx      int
}

func (f *fakeTransferDeps) GetProject(context.Context, string) (*project.Project, error) {
	return f.proj, f.projErr
}

func (f *fakeTransferDeps) ListPipelineDefinitions(context.Context, string) ([]project.PipelineDefinition, error) {
	return f.defs, f.defsErr
}

func (f *fakeTransferDeps) TriggerPipelineRun(context.Context, string, string, string, string, map[string]any) (string, error) {
	return f.triggerID, f.triggerErr
}

func (f *fakeTransferDeps) GetPipelineWorkflows(context.Context, string) ([]project.Workflow, error) {
	if f.wfIdx >= len(f.workflows) {
		return nil, nil
	}
	wf := f.workflows[f.wfIdx]
	f.wfIdx++
	return wf, nil
}

func (f *fakeTransferDeps) GetPipeline(context.Context, string) (*project.Pipeline, error) {
	return &project.Pipeline{State: "pending"}, nil
}

func happyDeps() *fakeTransferDeps {
	return &fakeTransferDeps{
		proj:      &project.Project{Slug: "gh/acme/web", ID: "proj-uuid"},
		defs:      []project.PipelineDefinition{{ID: "def-1", Name: "build"}},
		triggerID: "pipe-1",
		workflows: [][]project.Workflow{
			{{ID: "wf-1", Name: "transfer", Status: "success"}},
		},
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Transfer — dry run
// ─────────────────────────────────────────────────────────────────────────────

func TestTransfer_DryRun_NoPipelineTrigger(t *testing.T) {
	m := baseManifest()
	deps := happyDeps()
	opts := baseOpts()
	opts.DryRun = true
	opts.HostProjectSlug = "gh/acme/web"

	var out, errOut bytes.Buffer
	opts.Stdout = &out
	opts.Stderr = &errOut

	if err := Transfer(context.Background(), deps, m, opts); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Dry-run must not trigger a pipeline.
	if deps.wfIdx != 0 {
		t.Errorf("dry run should not trigger any pipeline, wfIdx=%d", deps.wfIdx)
	}

	// Must print the plan.
	outStr := out.String()
	if !strings.Contains(outStr, "deploy-prod") {
		t.Errorf("expected deploy-prod in plan output, got: %s", outStr)
	}
	if !strings.Contains(outStr, "AWS_KEY") {
		t.Errorf("expected AWS_KEY in plan output, got: %s", outStr)
	}
	if !strings.Contains(outStr, "Dry-run") {
		t.Errorf("expected Dry-run notice in plan output, got: %s", outStr)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Transfer — apply (live run)
// ─────────────────────────────────────────────────────────────────────────────

func TestTransfer_Apply_HappyPath(t *testing.T) {
	m := baseManifest()
	deps := happyDeps()
	opts := baseOpts()
	opts.DryRun = false
	opts.HostProjectSlug = "gh/acme/web"

	var out, errOut bytes.Buffer
	opts.Stdout = &out
	opts.Stderr = &errOut

	if err := Transfer(context.Background(), deps, m, opts); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Pipeline should have been triggered.
	if deps.wfIdx != 1 {
		t.Errorf("expected 1 workflow poll, got %d", deps.wfIdx)
	}

	outStr := out.String()
	if !strings.Contains(outStr, "succeeded") {
		t.Errorf("expected 'succeeded' in output, got: %s", outStr)
	}
}

func TestTransfer_Apply_WorkflowFailed(t *testing.T) {
	m := baseManifest()
	deps := happyDeps()
	deps.workflows = [][]project.Workflow{
		{{ID: "wf-1", Name: "transfer", Status: "failed"}},
	}
	opts := baseOpts()
	opts.DryRun = false
	opts.HostProjectSlug = "gh/acme/web"

	var out, errOut bytes.Buffer
	opts.Stdout = &out
	opts.Stderr = &errOut

	err := Transfer(context.Background(), deps, m, opts)
	if err == nil {
		t.Fatal("expected error when workflow failed")
	}
	if !errors.Is(err, ErrWorkflowFailed) {
		t.Errorf("expected ErrWorkflowFailed, got: %v", err)
	}
}

func TestTransfer_Apply_NoDefinitions_Error(t *testing.T) {
	m := baseManifest()
	deps := happyDeps()
	deps.defs = nil // no pipeline definitions
	opts := baseOpts()
	opts.DryRun = false
	opts.HostProjectSlug = "gh/acme/web"

	var out, errOut bytes.Buffer
	opts.Stdout = &out
	opts.Stderr = &errOut

	err := Transfer(context.Background(), deps, m, opts)
	if err == nil {
		t.Fatal("expected error when no pipeline definitions")
	}
	if !strings.Contains(err.Error(), "no pipeline definitions") {
		t.Errorf("error should mention pipeline definitions, got: %v", err)
	}
}

func TestTransfer_Apply_GetProjectError(t *testing.T) {
	m := baseManifest()
	deps := happyDeps()
	deps.projErr = errors.New("not found")
	opts := baseOpts()
	opts.DryRun = false
	opts.HostProjectSlug = "gh/acme/web"

	var out, errOut bytes.Buffer
	opts.Stdout = &out
	opts.Stderr = &errOut

	err := Transfer(context.Background(), deps, m, opts)
	if err == nil {
		t.Fatal("expected error on GetProject failure")
	}
}

func TestTransfer_Apply_TriggerError(t *testing.T) {
	m := baseManifest()
	deps := happyDeps()
	deps.triggerErr = errors.New("trigger failed")
	opts := baseOpts()
	opts.DryRun = false
	opts.HostProjectSlug = "gh/acme/web"

	var out, errOut bytes.Buffer
	opts.Stdout = &out
	opts.Stderr = &errOut

	err := Transfer(context.Background(), deps, m, opts)
	if err == nil {
		t.Fatal("expected error on trigger failure")
	}
}

func TestTransfer_AutoPickHostProject(t *testing.T) {
	m := baseManifest()
	deps := happyDeps()
	opts := baseOpts()
	opts.DryRun = false
	opts.HostProjectSlug = "" // auto-pick from manifest

	var out, errOut bytes.Buffer
	opts.Stdout = &out
	opts.Stderr = &errOut

	if err := Transfer(context.Background(), deps, m, opts); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have auto-picked the first project.
	errStr := errOut.String()
	if !strings.Contains(errStr, "Auto-picked host project") {
		t.Errorf("expected auto-pick notice, got: %s", errStr)
	}
}

func TestTransfer_NoProjectsForAutoPick_Error(t *testing.T) {
	m := &manifest.Manifest{
		Contexts: []manifest.Context{
			{Name: "ctx", EnvVars: []manifest.ContextEnvVar{{Name: "X"}}},
		},
		// No projects.
	}
	deps := happyDeps()
	opts := baseOpts()
	opts.DryRun = false
	opts.HostProjectSlug = "" // auto-pick would fail

	var out, errOut bytes.Buffer
	opts.Stdout = &out
	opts.Stderr = &errOut

	err := Transfer(context.Background(), deps, m, opts)
	if err == nil {
		t.Fatal("expected error when no projects for auto-pick")
	}
	if !strings.Contains(err.Error(), "host project") {
		t.Errorf("error should mention host project, got: %v", err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Plan: TotalVars
// ─────────────────────────────────────────────────────────────────────────────

func TestPlan_TotalVars(t *testing.T) {
	p := Plan{
		Contexts: []ContextPlan{
			{SourceName: "a", VarNames: []string{"X", "Y"}},
			{SourceName: "b", VarNames: []string{"Z"}},
		},
	}
	if p.TotalVars() != 3 {
		t.Errorf("TotalVars = %d, want 3", p.TotalVars())
	}
}

func TestPlan_TotalVars_Empty(t *testing.T) {
	p := Plan{}
	if p.TotalVars() != 0 {
		t.Errorf("TotalVars of empty plan = %d, want 0", p.TotalVars())
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// SecurityNote: config must reference token by env-var name, not value
// ─────────────────────────────────────────────────────────────────────────────

func TestBuildTransferConfig_TokenReferencedByName(t *testing.T) {
	m := baseManifest()
	opts := baseOpts()
	opts.DestTokenContext = "migration-secrets"
	opts.DestTokenEnvVar = "CIRCLECI_DEST_TOKEN"

	plan, _ := BuildPlan(m, &opts)
	cfg := buildTransferConfigWithVersion(m, plan.Contexts, nil, &opts)

	// The config should reference CIRCLECI_DEST_TOKEN as a shell var, not as a literal value.
	if !strings.Contains(cfg, "${CIRCLECI_DEST_TOKEN") {
		t.Error("config should reference dest token by ${ENV_VAR} notation, not as a literal value")
	}
	// The config must NOT contain the literal string that would be a token value.
	// Tokens look like "ccpaa_..." and the config must not have that pattern.
	if strings.Contains(cfg, "ccpaa_") {
		t.Error("config must not contain a literal API token value")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// destContextName / Mapping
// ─────────────────────────────────────────────────────────────────────────────

func TestOptionsDestContextName_NoMapping(t *testing.T) {
	opts := Options{}
	if got := opts.destContextName("deploy-prod"); got != "deploy-prod" {
		t.Errorf("identity mapping: got %q, want deploy-prod", got)
	}
}

func TestOptionsDestContextName_WithMapping(t *testing.T) {
	opts := Options{
		Mapping: map[string]string{"deploy-prod": "prod-deploy"},
	}
	if got := opts.destContextName("deploy-prod"); got != "prod-deploy" {
		t.Errorf("mapping: got %q, want prod-deploy", got)
	}
	if got := opts.destContextName("shared"); got != "shared" {
		t.Errorf("unmapped context: got %q, want shared", got)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Options defaults
// ─────────────────────────────────────────────────────────────────────────────

func TestOptionsBranch_Default(t *testing.T) {
	opts := Options{}
	if got := opts.branch(); got != "main" {
		t.Errorf("default branch = %q, want main", got)
	}
}

func TestOptionsBranch_Override(t *testing.T) {
	opts := Options{Branch: "release"}
	if got := opts.branch(); got != "release" {
		t.Errorf("branch = %q, want release", got)
	}
}

func TestOptionsDestHost_Default(t *testing.T) {
	opts := Options{}
	if got := opts.destHost(); got != "https://circleci.com" {
		t.Errorf("default destHost = %q, want https://circleci.com", got)
	}
}

func TestOptionsDestTokenEnvVar_Default(t *testing.T) {
	opts := Options{}
	if got := opts.destTokenEnvVar(); got != "CIRCLECI_DEST_TOKEN" {
		t.Errorf("default destTokenEnvVar = %q, want CIRCLECI_DEST_TOKEN", got)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Create-missing-context path
// ─────────────────────────────────────────────────────────────────────────────

func TestBuildTransferConfig_CreateMissingContext(t *testing.T) {
	m := baseManifest()
	opts := baseOpts()
	opts.DestTokenContext = "migration-secrets"

	plan, err := BuildPlan(m, &opts)
	if err != nil {
		t.Fatalf("plan error: %v", err)
	}

	cfg := buildTransferConfigWithVersion(m, plan.Contexts, nil, &opts)

	// The generated config must contain the create-if-missing POST logic.
	if !strings.Contains(cfg, "/api/v2/context\"") {
		t.Error("expected POST to /api/v2/context in config")
	}
	if !strings.Contains(cfg, "\"type\": \"organization\"") {
		t.Error("expected organization type in context POST body")
	}
	// Must contain the create branch (not just the error-and-exit branch).
	if !strings.Contains(cfg, "Creating it in org") && !strings.Contains(cfg, "not found — creating it") {
		t.Error("expected create-if-missing message in config shell")
	}
	// Must still resolve by listing first (pagination loop).
	if !strings.Contains(cfg, "api/v2/context?owner-id=") {
		t.Error("expected context list endpoint in config")
	}
	// Must NOT contain the old error-and-exit-only path.
	if strings.Contains(cfg, "Run: circleci-migrate sync") {
		t.Error("config must not tell operator to run sync (create-if-missing replaces that)")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Project env-var mapping resolution (--include-project-vars)
// ─────────────────────────────────────────────────────────────────────────────

// manifestWithProjects returns a manifest with projects that have env vars.
func manifestWithProjects() *manifest.Manifest {
	return &manifest.Manifest{
		Source: manifest.Source{Org: manifest.Org{ID: "src-org-uuid"}},
		Contexts: []manifest.Context{
			{
				Name:    "deploy-prod",
				EnvVars: []manifest.ContextEnvVar{{Name: "AWS_KEY"}},
			},
		},
		Projects: []manifest.Project{
			{
				Slug: "gh/acme/web",
				EnvVars: []manifest.ProjectEnvVar{
					{Name: "APP_SECRET"},
					{Name: "DB_URL"},
				},
			},
			{
				Slug: "gh/acme/api",
				EnvVars: []manifest.ProjectEnvVar{
					{Name: "API_KEY"},
				},
			},
			{
				Slug: "gh/acme/no-vars",
				// No env vars — should be excluded from plan.
			},
		},
	}
}

func TestBuildPlan_ProjectVars_Mapped(t *testing.T) {
	m := manifestWithProjects()
	opts := baseOpts()
	opts.IncludeProjectVars = true
	opts.Mapping = map[string]string{
		"gh/acme/web": "gh/acme-new/web",
		// gh/acme/api is intentionally NOT mapped — should be skipped.
	}

	plan, err := BuildPlan(m, &opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(plan.Projects) != 2 {
		t.Fatalf("expected 2 project plans (1 mapped + 1 skipped), got %d", len(plan.Projects))
	}

	// Verify mapped project.
	var webPlan, apiPlan *ProjectVarPlan
	for i := range plan.Projects {
		switch plan.Projects[i].SourceSlug {
		case "gh/acme/web":
			webPlan = &plan.Projects[i]
		case "gh/acme/api":
			apiPlan = &plan.Projects[i]
		}
	}

	if webPlan == nil {
		t.Fatal("expected gh/acme/web in plan")
	}
	if webPlan.Skipped {
		t.Errorf("gh/acme/web should not be skipped (has mapping)")
	}
	if webPlan.DestSlug != "gh/acme-new/web" {
		t.Errorf("gh/acme/web dest slug = %q, want gh/acme-new/web", webPlan.DestSlug)
	}
	if len(webPlan.VarNames) != 2 {
		t.Errorf("gh/acme/web expected 2 vars, got %d: %v", len(webPlan.VarNames), webPlan.VarNames)
	}

	if apiPlan == nil {
		t.Fatal("expected gh/acme/api in plan (as skipped)")
	}
	if !apiPlan.Skipped {
		t.Errorf("gh/acme/api should be skipped (no mapping)")
	}
	if apiPlan.SkipReason == "" {
		t.Error("gh/acme/api skip reason must not be empty")
	}
	if !strings.Contains(apiPlan.SkipReason, "gh/acme/api") {
		t.Errorf("skip reason should mention source slug, got: %s", apiPlan.SkipReason)
	}
	if !strings.Contains(apiPlan.SkipReason, "--mapping") {
		t.Errorf("skip reason should mention --mapping, got: %s", apiPlan.SkipReason)
	}
}

func TestBuildPlan_ProjectVars_AllSkipped_NoPanic(t *testing.T) {
	m := manifestWithProjects()
	opts := baseOpts()
	opts.IncludeProjectVars = true
	// No mapping entries — all projects will be skipped.

	plan, err := BuildPlan(m, &opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// All projects skipped is a valid plan (operator sees the SKIP lines).
	for _, pp := range plan.Projects {
		if !pp.Skipped {
			t.Errorf("expected all projects skipped with no mapping, but %q is not skipped", pp.SourceSlug)
		}
	}
	if plan.TotalProjectVars() != 0 {
		t.Errorf("expected 0 active project vars when all skipped, got %d", plan.TotalProjectVars())
	}
}

func TestBuildPlan_ProjectVarsOff_NoProjectPlans(t *testing.T) {
	m := manifestWithProjects()
	opts := baseOpts()
	opts.IncludeProjectVars = false // default

	plan, err := BuildPlan(m, &opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(plan.Projects) != 0 {
		t.Errorf("expected no project plans when IncludeProjectVars=false, got %d", len(plan.Projects))
	}
}

func TestBuildTransferConfig_ProjectVarsIncluded(t *testing.T) {
	m := manifestWithProjects()
	opts := baseOpts()
	opts.IncludeProjectVars = true
	opts.DestTokenContext = "migration-secrets"
	opts.Mapping = map[string]string{
		"gh/acme/web": "gh/acme-new/web",
	}

	plan, err := BuildPlan(m, &opts)
	if err != nil {
		t.Fatalf("plan error: %v", err)
	}

	cfg := buildTransferConfigWithVersion(m, plan.Contexts, plan.Projects, &opts)

	// Must contain the project job.
	if !strings.Contains(cfg, "circleci-migrate-transfer-project") {
		t.Error("expected project transfer job in config")
	}
	// Must reference dest project slug.
	if !strings.Contains(cfg, "gh/acme-new/web") {
		t.Error("expected dest project slug in config")
	}
	// Must use POST to v1.1 envvar endpoint.
	if !strings.Contains(cfg, "/api/v1.1/project/") {
		t.Error("expected v1.1 project envvar endpoint in config")
	}
	if !strings.Contains(cfg, "/envvar") {
		t.Error("expected /envvar path in project job")
	}
	// Skipped project (gh/acme/api) must NOT appear.
	if strings.Contains(cfg, "gh/acme/api") {
		t.Error("skipped project gh/acme/api must not appear in config")
	}
	// No plaintext values.
	if strings.Contains(cfg, "actual-secret") {
		t.Error("config must not contain secret values")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Plan output (printPlan) — project SKIP lines
// ─────────────────────────────────────────────────────────────────────────────

func TestPrintPlan_ShowsProjectSkips(t *testing.T) {
	plan := &Plan{
		Contexts: []ContextPlan{
			{SourceName: "deploy-prod", DestName: "deploy-prod", VarNames: []string{"AWS_KEY"}},
		},
		Projects: []ProjectVarPlan{
			{SourceSlug: "gh/acme/web", DestSlug: "gh/acme-new/web", VarNames: []string{"APP_SECRET"}},
			{SourceSlug: "gh/acme/api", Skipped: true, SkipReason: `dest project for "gh/acme/api" unknown — provide --mapping or onboard it first; skipped`},
		},
		DestTokenContext: "migration-secrets",
		DestTokenEnvVar:  "CIRCLECI_DEST_TOKEN",
	}
	opts := baseOpts()

	var out, errOut bytes.Buffer
	printPlan(&out, &errOut, plan, &opts)

	outStr := out.String()

	// Mapped project must appear with dest slug.
	if !strings.Contains(outStr, "gh/acme/web") {
		t.Error("expected gh/acme/web in plan output")
	}
	if !strings.Contains(outStr, "gh/acme-new/web") {
		t.Error("expected dest slug gh/acme-new/web in plan output")
	}
	// Skipped project must be flagged.
	if !strings.Contains(outStr, "SKIP") {
		t.Error("expected SKIP marker for unresolvable project")
	}
	if !strings.Contains(outStr, "gh/acme/api") {
		t.Error("expected skipped project slug in plan output")
	}
}

func TestPrintPlan_ContextCreateVsUpdate(t *testing.T) {
	plan := &Plan{
		Contexts: []ContextPlan{
			{SourceName: "existing-ctx", DestName: "existing-ctx", VarNames: []string{"KEY"}, WillCreate: false},
			{SourceName: "new-ctx", DestName: "new-ctx", VarNames: []string{"SECRET"}, WillCreate: true},
		},
		DestTokenContext: "migration-secrets",
		DestTokenEnvVar:  "CIRCLECI_DEST_TOKEN",
	}
	opts := baseOpts()

	var out, errOut bytes.Buffer
	printPlan(&out, &errOut, plan, &opts)

	outStr := out.String()
	if !strings.Contains(outStr, "[update]") {
		t.Error("expected [update] label for existing context")
	}
	if !strings.Contains(outStr, "[create]") {
		t.Error("expected [create] label for new context")
	}
}

func TestPlan_TotalProjectVars(t *testing.T) {
	p := Plan{
		Projects: []ProjectVarPlan{
			{SourceSlug: "a", DestSlug: "a-new", VarNames: []string{"X", "Y"}},
			{SourceSlug: "b", Skipped: true}, // should not count
			{SourceSlug: "c", DestSlug: "c-new", VarNames: []string{"Z"}},
		},
	}
	if got := p.TotalProjectVars(); got != 3 {
		t.Errorf("TotalProjectVars = %d, want 3", got)
	}
}

func TestOptionsDestProjectSlug_WithMapping(t *testing.T) {
	opts := Options{
		Mapping: map[string]string{
			"gh/acme/web": "gh/acme-new/web",
		},
	}
	slug, ok := opts.destProjectSlug("gh/acme/web")
	if !ok {
		t.Fatal("expected ok=true for mapped project")
	}
	if slug != "gh/acme-new/web" {
		t.Errorf("expected gh/acme-new/web, got %q", slug)
	}
}

func TestOptionsDestProjectSlug_NoMapping(t *testing.T) {
	opts := Options{}
	_, ok := opts.destProjectSlug("gh/acme/web")
	if ok {
		t.Error("expected ok=false when no mapping is set")
	}
}

func TestOptionsDestProjectSlug_MissingEntry(t *testing.T) {
	opts := Options{
		Mapping: map[string]string{
			"gh/acme/other": "gh/acme-new/other",
		},
	}
	_, ok := opts.destProjectSlug("gh/acme/web")
	if ok {
		t.Error("expected ok=false for unmapped project")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// multiCallFakeTransferDeps — extended fake that records all TriggerPipelineRun
// calls with their slugs, supporting concurrent callers.
// ─────────────────────────────────────────────────────────────────────────────

// triggerCall records one invocation of TriggerPipelineRun.
type triggerCall struct {
	slug string
	yaml string
}

// multiCallFakeTransferDeps is a thread-safe Deps fake that records all
// TriggerPipelineRun calls and can simulate per-slug failures for
// ListPipelineDefinitions.
type multiCallFakeTransferDeps struct {
	mu sync.Mutex

	// projErr is returned by GetProject (nil means success with a synthetic project).
	projErr error

	// defs / defsErr are returned by ListPipelineDefinitions for most slugs.
	defs    []project.PipelineDefinition
	defsErr error

	// defsEmptyForSlugs is the set of slugs for which ListPipelineDefinitions
	// returns an empty slice (simulates a project with no pipeline definitions).
	defsEmptyForSlugs map[string]bool

	// triggerID is the pipeline UUID returned by each TriggerPipelineRun call.
	triggerID  string
	triggerErr error

	// triggerCalls records every TriggerPipelineRun invocation in order.
	triggerCalls []triggerCall

	// successWorkflow is returned for every GetPipelineWorkflows call.
	successWorkflow project.Workflow
}

func (f *multiCallFakeTransferDeps) GetProject(_ context.Context, slug string) (*project.Project, error) {
	if f.projErr != nil {
		return nil, f.projErr
	}
	// Return a project whose slug and ID match the requested slug so callers
	// can distinguish which project is being resolved.
	return &project.Project{Slug: slug, ID: "id-" + slug}, nil
}

func (f *multiCallFakeTransferDeps) ListPipelineDefinitions(_ context.Context, projectID string) ([]project.PipelineDefinition, error) {
	if f.defsErr != nil {
		return nil, f.defsErr
	}
	// Strip the "id-" prefix to recover the slug used in defsEmptyForSlugs.
	slug := strings.TrimPrefix(projectID, "id-")
	if f.defsEmptyForSlugs[slug] {
		return nil, nil
	}
	if len(f.defs) == 0 {
		return []project.PipelineDefinition{{ID: "def-multi", Name: "build"}}, nil
	}
	return f.defs, nil
}

func (f *multiCallFakeTransferDeps) TriggerPipelineRun(_ context.Context, slug, _, _, configYAML string, _ map[string]any) (string, error) {
	f.mu.Lock()
	f.triggerCalls = append(f.triggerCalls, triggerCall{slug: slug, yaml: configYAML})
	f.mu.Unlock()

	if f.triggerErr != nil {
		return "", f.triggerErr
	}
	return f.triggerID, nil
}

func (f *multiCallFakeTransferDeps) GetPipelineWorkflows(_ context.Context, _ string) ([]project.Workflow, error) {
	wf := f.successWorkflow
	if wf.Status == "" {
		wf = project.Workflow{ID: "wf-ok", Name: "transfer", Status: "success"}
	}
	return []project.Workflow{wf}, nil
}

func (f *multiCallFakeTransferDeps) GetPipeline(_ context.Context, _ string) (*project.Pipeline, error) {
	return &project.Pipeline{State: "pending"}, nil
}

// happyMultiDeps returns a multiCallFakeTransferDeps configured for happy-path
// tests with a pinned pipeline ID and success workflow.
func happyMultiDeps() *multiCallFakeTransferDeps {
	return &multiCallFakeTransferDeps{
		triggerID:       "pipe-multi",
		successWorkflow: project.Workflow{ID: "wf-ok", Name: "transfer", Status: "success"},
	}
}

// manifestWithTwoMappedProjects returns a manifest with two projects that have
// env vars and are both fully mapped.
func manifestWithTwoMappedProjects() *manifest.Manifest {
	return &manifest.Manifest{
		Source: manifest.Source{Org: manifest.Org{ID: "src-org-uuid"}},
		Contexts: []manifest.Context{
			{Name: "deploy-prod", EnvVars: []manifest.ContextEnvVar{{Name: "AWS_KEY"}}},
		},
		Projects: []manifest.Project{
			{Slug: "gh/acme/web", EnvVars: []manifest.ProjectEnvVar{{Name: "APP_SECRET"}}},
			{Slug: "gh/acme/api", EnvVars: []manifest.ProjectEnvVar{{Name: "API_KEY"}}},
		},
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Regression: per-project pipeline correctness (issue #263)
// ─────────────────────────────────────────────────────────────────────────────

// TestTransfer_ProjectVars_OnePerProject_Regression is the primary regression
// test for issue #263.  It verifies that when --include-project-vars is set
// and there are N mapped projects, Transfer triggers N+1 pipelines in total:
//   - 1 host-project pipeline for contexts
//   - 1 per-project pipeline per source project, each under THAT project's slug
//
// Previously, a single host-project pipeline was used for all projects, which
// meant only the host project's env vars were injected; all other projects'
// vars resolved to empty strings (silent corruption).
func TestTransfer_ProjectVars_OnePerProject_Regression(t *testing.T) {
	m := manifestWithTwoMappedProjects()
	deps := happyMultiDeps()
	opts := baseOpts()
	opts.DryRun = false
	opts.HostProjectSlug = "gh/acme/web"
	opts.IncludeProjectVars = true
	opts.Mapping = map[string]string{
		"gh/acme/web": "gh/acme-new/web",
		"gh/acme/api": "gh/acme-new/api",
	}

	var out, errOut bytes.Buffer
	opts.Stdout = &out
	opts.Stderr = &errOut

	if err := Transfer(context.Background(), deps, m, opts); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	deps.mu.Lock()
	calls := deps.triggerCalls
	deps.mu.Unlock()

	// Expect 3 total triggers: 1 context pipeline + 2 per-project pipelines.
	if len(calls) != 3 {
		t.Fatalf("expected 3 TriggerPipelineRun calls (1 ctx + 2 project), got %d: %v",
			len(calls), slugsFromCalls(calls))
	}

	// The host pipeline must use the HostProjectSlug.
	if calls[0].slug != "gh/acme/web" {
		t.Errorf("first call (context pipeline) expected slug %q, got %q", "gh/acme/web", calls[0].slug)
	}

	// The two per-project pipelines must each use their own source slug.
	projectSlugs := slugsFromCalls(calls[1:])
	if !containsSlug(projectSlugs, "gh/acme/web") {
		t.Errorf("expected per-project pipeline under gh/acme/web, got slugs: %v", projectSlugs)
	}
	if !containsSlug(projectSlugs, "gh/acme/api") {
		t.Errorf("expected per-project pipeline under gh/acme/api, got slugs: %v", projectSlugs)
	}

	// Critically: neither per-project call may be the HOST project for the
	// OTHER project — each must use its OWN slug.
	for _, c := range calls[1:] {
		if c.slug != "gh/acme/web" && c.slug != "gh/acme/api" {
			t.Errorf("per-project call used unexpected slug %q (expected one of the source project slugs)", c.slug)
		}
	}
}

// TestTransfer_ProjectVars_ContextOnlyOnHostSlug verifies that the CONTEXT
// pipeline is triggered under the HostProjectSlug, not under the per-project
// slug.  This confirms the two pipelines are kept separate.
func TestTransfer_ProjectVars_ContextOnlyOnHostSlug(t *testing.T) {
	m := manifestWithTwoMappedProjects()
	deps := happyMultiDeps()
	opts := baseOpts()
	opts.DryRun = false
	opts.HostProjectSlug = "gh/acme/host"
	opts.IncludeProjectVars = true
	opts.Mapping = map[string]string{
		"gh/acme/web": "gh/acme-new/web",
		"gh/acme/api": "gh/acme-new/api",
	}

	var out, errOut bytes.Buffer
	opts.Stdout = &out
	opts.Stderr = &errOut

	if err := Transfer(context.Background(), deps, m, opts); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	deps.mu.Lock()
	calls := deps.triggerCalls
	deps.mu.Unlock()

	// First call must be the context pipeline on the host.
	if len(calls) == 0 {
		t.Fatal("no TriggerPipelineRun calls recorded")
	}
	if calls[0].slug != "gh/acme/host" {
		t.Errorf("context pipeline slug = %q, want %q", calls[0].slug, "gh/acme/host")
	}

	// No per-project call should ever use the host slug.
	for _, c := range calls[1:] {
		if c.slug == "gh/acme/host" {
			t.Errorf("per-project pipeline incorrectly used host slug %q; each project must use its own slug", c.slug)
		}
	}
}

// TestBuildSingleProjectTransferConfig_ContainsOnlyThatProject verifies that
// buildSingleProjectTransferConfig generates a config that contains ONLY the
// var names for the given project and NOT any other project's vars.
func TestBuildSingleProjectTransferConfig_ContainsOnlyThatProject(t *testing.T) {
	opts := baseOpts()
	opts.DestTokenContext = "migration-secrets"
	opts.DestOrgID = "dest-org-uuid"

	webPlan := ProjectVarPlan{
		SourceSlug: "gh/acme/web",
		DestSlug:   "gh/acme-new/web",
		VarNames:   []string{"APP_SECRET", "DB_URL"},
	}
	apiPlan := ProjectVarPlan{
		SourceSlug: "gh/acme/api",
		DestSlug:   "gh/acme-new/api",
		VarNames:   []string{"API_KEY"},
	}

	webCfg := buildSingleProjectTransferConfigWithVersion(webPlan, &opts)

	// Must contain the web project's vars.
	if !strings.Contains(webCfg, "APP_SECRET") {
		t.Error("web config must contain APP_SECRET")
	}
	if !strings.Contains(webCfg, "DB_URL") {
		t.Error("web config must contain DB_URL")
	}

	// Must NOT contain the api project's var.
	if strings.Contains(webCfg, "API_KEY") {
		t.Error("web config must NOT contain API_KEY (that belongs to gh/acme/api)")
	}

	// Must reference the dest project slug.
	if !strings.Contains(webCfg, "gh/acme-new/web") {
		t.Error("web config must reference dest slug gh/acme-new/web")
	}
	if strings.Contains(webCfg, "gh/acme-new/api") {
		t.Error("web config must NOT reference api dest slug")
	}

	// API config must contain only API_KEY.
	apiCfg := buildSingleProjectTransferConfigWithVersion(apiPlan, &opts)
	if !strings.Contains(apiCfg, "API_KEY") {
		t.Error("api config must contain API_KEY")
	}
	if strings.Contains(apiCfg, "APP_SECRET") || strings.Contains(apiCfg, "DB_URL") {
		t.Error("api config must NOT contain web project vars")
	}
}

// TestTransfer_ProjectVars_NoDefsForOneProject verifies that when one project
// has no pipeline definitions, that project is reported as failed while other
// projects are still attempted, and the overall command returns a non-nil error.
func TestTransfer_ProjectVars_NoDefsForOneProject(t *testing.T) {
	m := manifestWithTwoMappedProjects()

	// Make gh/acme/api return empty pipeline definitions.
	deps := happyMultiDeps()
	deps.defsEmptyForSlugs = map[string]bool{
		"gh/acme/api": true,
	}

	opts := baseOpts()
	opts.DryRun = false
	opts.HostProjectSlug = "gh/acme/web"
	opts.IncludeProjectVars = true
	opts.Mapping = map[string]string{
		"gh/acme/web": "gh/acme-new/web",
		"gh/acme/api": "gh/acme-new/api",
	}

	var out, errOut bytes.Buffer
	opts.Stdout = &out
	opts.Stderr = &errOut

	err := Transfer(context.Background(), deps, m, opts)

	// Overall command must fail because one project has no definitions.
	if err == nil {
		t.Fatal("expected error when one project has no pipeline definitions")
	}
	if !strings.Contains(err.Error(), "gh/acme/api") {
		t.Errorf("error must mention the failing project, got: %v", err)
	}
	if !strings.Contains(err.Error(), "no pipeline definitions") {
		t.Errorf("error must mention 'no pipeline definitions', got: %v", err)
	}

	// The OTHER project (gh/acme/web) must still have been attempted — its
	// pipeline trigger call must appear in the recorded calls.
	deps.mu.Lock()
	calls := deps.triggerCalls
	deps.mu.Unlock()

	webAttempted := false
	for _, c := range calls {
		if c.slug == "gh/acme/web" {
			webAttempted = true
			break
		}
	}
	if !webAttempted {
		t.Error("gh/acme/web should still have been attempted despite gh/acme/api failing")
	}
}

// TestTransfer_ProjectVarsOnly_NoContexts verifies that when the plan has no
// contexts but does have project vars, the context pipeline is NOT triggered
// (no wasted pipeline run) and project pipelines still execute.
func TestTransfer_ProjectVarsOnly_NoContexts(t *testing.T) {
	m := &manifest.Manifest{
		Source: manifest.Source{Org: manifest.Org{ID: "src-org-uuid"}},
		// No contexts with vars.
		Projects: []manifest.Project{
			{Slug: "gh/acme/web", EnvVars: []manifest.ProjectEnvVar{{Name: "APP_SECRET"}}},
		},
	}
	deps := happyMultiDeps()
	opts := baseOpts()
	opts.DryRun = false
	opts.HostProjectSlug = "gh/acme/web"
	opts.IncludeProjectVars = true
	opts.Mapping = map[string]string{
		"gh/acme/web": "gh/acme-new/web",
	}

	var out, errOut bytes.Buffer
	opts.Stdout = &out
	opts.Stderr = &errOut

	if err := Transfer(context.Background(), deps, m, opts); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	deps.mu.Lock()
	calls := deps.triggerCalls
	deps.mu.Unlock()

	// Only 1 trigger call expected: the per-project pipeline.
	// The context pipeline must NOT fire when there are no contexts.
	if len(calls) != 1 {
		t.Fatalf("expected 1 TriggerPipelineRun call (project only), got %d: %v", len(calls), slugsFromCalls(calls))
	}
	if calls[0].slug != "gh/acme/web" {
		t.Errorf("project pipeline slug = %q, want %q", calls[0].slug, "gh/acme/web")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// buildSingleProjectTransferConfig: security properties
// ─────────────────────────────────────────────────────────────────────────────

func TestBuildSingleProjectTransferConfig_DestTokenByName(t *testing.T) {
	opts := baseOpts()
	opts.DestTokenContext = "migration-secrets"
	opts.DestTokenEnvVar = "CIRCLECI_DEST_TOKEN"

	pp := ProjectVarPlan{
		SourceSlug: "gh/acme/web",
		DestSlug:   "gh/acme-new/web",
		VarNames:   []string{"APP_SECRET"},
	}

	cfg := buildSingleProjectTransferConfigWithVersion(pp, &opts)

	// Token referenced by env-var name, not literal value.
	if !strings.Contains(cfg, "${CIRCLECI_DEST_TOKEN") {
		t.Error("config must reference dest token by ${ENV_VAR} notation")
	}
	if strings.Contains(cfg, "ccpaa_") {
		t.Error("config must not contain a literal API token value")
	}
	// Dest-token context must be attached at the workflow level.
	if !strings.Contains(cfg, "- migration-secrets") {
		t.Error("config must include dest-token context in workflow job context list")
	}
}

func TestBuildSingleProjectTransferConfig_UsesV11Endpoint(t *testing.T) {
	opts := baseOpts()
	opts.DestTokenContext = "migration-secrets"

	pp := ProjectVarPlan{
		SourceSlug: "gh/acme/web",
		DestSlug:   "gh/acme-new/web",
		VarNames:   []string{"MY_VAR"},
	}

	cfg := buildSingleProjectTransferConfigWithVersion(pp, &opts)

	if !strings.Contains(cfg, "/api/v1.1/project/") {
		t.Error("single-project config must use v1.1 envvar endpoint")
	}
	if !strings.Contains(cfg, "/envvar") {
		t.Error("single-project config must reference /envvar path")
	}
	if !strings.Contains(cfg, "gh/acme-new/web") {
		t.Error("single-project config must reference dest project slug")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Issue #274 — unauthorized/not_run are terminal; unauthorized auto-retries
// ─────────────────────────────────────────────────────────────────────────────

// TestTerminalStatuses_UnauthorizedAndNotRunAreTerminal verifies that both
// "unauthorized" and "not_run" are included in terminalStatuses so the poller
// stops instead of hanging to poll-timeout.
func TestTerminalStatuses_UnauthorizedAndNotRunAreTerminal(t *testing.T) {
	for _, s := range []string{"unauthorized", "not_run"} {
		if !terminalStatuses[s] {
			t.Errorf("terminalStatuses[%q] = false; want true", s)
		}
	}
	// Existing terminal statuses must still be present.
	for _, s := range []string{"success", "failed", "error", "canceled"} {
		if !terminalStatuses[s] {
			t.Errorf("terminalStatuses[%q] = false; regression — was previously terminal", s)
		}
	}
}

// retryFakeTransferDeps is a Deps fake that returns "unauthorized" for the first
// N pipeline polls, then "success" for the (N+1)th.  It counts how many times
// TriggerPipelineRun is called.
type retryFakeTransferDeps struct {
	mu           sync.Mutex
	triggerCount int
	failCount    int // number of times to return "unauthorized" before success
	proj         *project.Project
	defs         []project.PipelineDefinition
}

func (f *retryFakeTransferDeps) GetProject(_ context.Context, slug string) (*project.Project, error) {
	if f.proj != nil {
		return f.proj, nil
	}
	return &project.Project{Slug: slug, ID: "id-" + slug}, nil
}

func (f *retryFakeTransferDeps) ListPipelineDefinitions(_ context.Context, _ string) ([]project.PipelineDefinition, error) {
	if len(f.defs) > 0 {
		return f.defs, nil
	}
	return []project.PipelineDefinition{{ID: "def-1", Name: "build"}}, nil
}

func (f *retryFakeTransferDeps) TriggerPipelineRun(_ context.Context, _, _, _, _ string, _ map[string]any) (string, error) {
	f.mu.Lock()
	f.triggerCount++
	f.mu.Unlock()
	return "pipe-retry", nil
}

func (f *retryFakeTransferDeps) GetPipelineWorkflows(_ context.Context, _ string) ([]project.Workflow, error) {
	f.mu.Lock()
	remaining := f.failCount
	if remaining > 0 {
		f.failCount--
	}
	f.mu.Unlock()

	if remaining > 0 {
		return []project.Workflow{{ID: "wf-1", Name: "transfer", Status: "unauthorized"}}, nil
	}
	return []project.Workflow{{ID: "wf-1", Name: "transfer", Status: "success"}}, nil
}

func (f *retryFakeTransferDeps) GetPipeline(_ context.Context, _ string) (*project.Pipeline, error) {
	return &project.Pipeline{State: "pending"}, nil
}

// TestTriggerAndPollProjectPipeline_UnauthorizedThenSuccess verifies that when
// the workflow returns "unauthorized" followed by "success", the pipeline trigger
// is retried and ultimately succeeds without an error.
func TestTriggerAndPollProjectPipeline_UnauthorizedThenSuccess(t *testing.T) {
	deps := &retryFakeTransferDeps{failCount: 1} // unauthorized once, then success
	pp := ProjectVarPlan{
		SourceSlug: "gh/acme/web",
		DestSlug:   "gh/acme-new/web",
		VarNames:   []string{"MY_VAR"},
	}
	opts := &Options{
		DestOrgID:        "dest-org-uuid",
		DestTokenContext: "migration-secrets",
		// Use a very short poll interval so the test doesn't wait 30 s.
		PollInterval: 1,
		Stdout:       &bytes.Buffer{},
		Stderr:       &bytes.Buffer{},
	}

	var errOut bytes.Buffer
	err := triggerAndPollProjectPipeline(context.Background(), deps, pp, opts, &errOut)
	if err != nil {
		t.Fatalf("expected success after retry, got: %v", err)
	}

	// TriggerPipelineRun should have been called twice: initial + 1 retry.
	deps.mu.Lock()
	count := deps.triggerCount
	deps.mu.Unlock()
	if count != 2 {
		t.Errorf("expected 2 TriggerPipelineRun calls (initial + 1 retry), got %d", count)
	}

	// The retry message should appear in the progress output.
	if !strings.Contains(errOut.String(), "unauthorized") {
		t.Errorf("expected 'unauthorized' in progress output, got: %s", errOut.String())
	}
}

// TestRunContextPipeline_UnauthorizedThenSuccess verifies the context pipeline
// path also retries on an "unauthorized" workflow (e.g. a just-followed host
// project whose context authorization hasn't propagated) and then succeeds.
func TestRunContextPipeline_UnauthorizedThenSuccess(t *testing.T) {
	deps := &retryFakeTransferDeps{failCount: 1} // unauthorized once, then success
	m := &manifest.Manifest{}
	plan := &Plan{
		Contexts: []ContextPlan{{SourceName: "deploy-prod", DestName: "deploy-prod", VarNames: []string{"K"}}},
	}
	opts := &Options{
		HostProjectSlug:  "gh/acme/web",
		DestOrgID:        "dest-org-uuid",
		DestTokenContext: "migration-secrets",
		PollInterval:     1, // short, so we don't sleep 30s
		Stdout:           &bytes.Buffer{},
		Stderr:           &bytes.Buffer{},
	}

	var errOut bytes.Buffer
	opts.Stderr = &errOut
	if err := runContextPipeline(context.Background(), deps, m, plan, opts); err != nil {
		t.Fatalf("expected success after retry, got: %v", err)
	}
	deps.mu.Lock()
	count := deps.triggerCount
	deps.mu.Unlock()
	if count != 2 {
		t.Errorf("expected 2 TriggerPipelineRun calls (initial + 1 retry), got %d", count)
	}
	if !strings.Contains(errOut.String(), "unauthorized") {
		t.Errorf("expected 'unauthorized' retry message, got: %s", errOut.String())
	}
}

// TestTriggerAndPollProjectPipeline_UnauthorizedAllRetries verifies that when
// all retries are exhausted the function returns a wrapped ErrWorkflowFailed
// with an actionable message.
func TestTriggerAndPollProjectPipeline_UnauthorizedAllRetries(t *testing.T) {
	// Always unauthorized (more fails than retries).
	deps := &retryFakeTransferDeps{failCount: unauthorizedRetryMax + 5}
	pp := ProjectVarPlan{
		SourceSlug: "gh/acme/web",
		DestSlug:   "gh/acme-new/web",
		VarNames:   []string{"MY_VAR"},
	}
	opts := &Options{
		DestOrgID:        "dest-org-uuid",
		DestTokenContext: "migration-secrets",
		PollInterval:     1,
		Stdout:           &bytes.Buffer{},
		Stderr:           &bytes.Buffer{},
	}

	err := triggerAndPollProjectPipeline(context.Background(), deps, pp, opts, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected error when all retries exhausted")
	}
	if !errors.Is(err, ErrWorkflowFailed) {
		t.Errorf("expected ErrWorkflowFailed, got: %v", err)
	}
	if !strings.Contains(err.Error(), "unauthorized") {
		t.Errorf("error should mention 'unauthorized', got: %v", err)
	}
	if !strings.Contains(err.Error(), "retries") {
		t.Errorf("error should mention 'retries', got: %v", err)
	}

	// Should have been triggered unauthorizedRetryMax+1 times.
	deps.mu.Lock()
	count := deps.triggerCount
	deps.mu.Unlock()
	if count != unauthorizedRetryMax+1 {
		t.Errorf("expected %d TriggerPipelineRun calls, got %d", unauthorizedRetryMax+1, count)
	}
}

// TestPollWorkflow_NotRunIsTerminal verifies that a "not_run" workflow status
// is recognized as terminal (poll returns immediately).
func TestPollWorkflow_NotRunIsTerminal(t *testing.T) {
	deps := &fakeTransferDeps{
		proj:      &project.Project{Slug: "gh/acme/web", ID: "proj-uuid"},
		defs:      []project.PipelineDefinition{{ID: "def-1", Name: "build"}},
		triggerID: "pipe-1",
		workflows: [][]project.Workflow{
			{{ID: "wf-1", Name: "transfer", Status: "not_run"}},
		},
	}

	wf, err := pollWorkflow(context.Background(), deps, "pipe-1", 1, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("pollWorkflow returned error: %v", err)
	}
	if wf.Status != "not_run" {
		t.Errorf("expected status not_run, got %q", wf.Status)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Helpers for per-project pipeline tests
// ─────────────────────────────────────────────────────────────────────────────

func slugsFromCalls(calls []triggerCall) []string {
	out := make([]string, len(calls))
	for i, c := range calls {
		out[i] = c.slug
	}
	return out
}

func containsSlug(slugs []string, target string) bool {
	for _, s := range slugs {
		if s == target {
			return true
		}
	}
	return false
}

// ─────────────────────────────────────────────────────────────────────────────
// SSH key transfer (--include-ssh-keys)
// ─────────────────────────────────────────────────────────────────────────────

// manifestWithSSHKeys returns a manifest with two projects: one has SSH keys,
// one has env vars, one has both.
func manifestWithSSHKeys() *manifest.Manifest {
	return &manifest.Manifest{
		Source: manifest.Source{Org: manifest.Org{ID: "src-org-uuid"}},
		Contexts: []manifest.Context{
			{
				Name:    "deploy-prod",
				EnvVars: []manifest.ContextEnvVar{{Name: "AWS_KEY"}},
			},
		},
		Projects: []manifest.Project{
			{
				Slug: "gh/acme/web",
				EnvVars: []manifest.ProjectEnvVar{
					{Name: "APP_SECRET"},
				},
				SSHKeys: []manifest.ProjectSSHKey{
					{Fingerprint: "abc123=", Hostname: "github.com"},
					{Fingerprint: "def456=", Hostname: ""},
				},
			},
			{
				Slug: "gh/acme/api",
				// Only SSH keys, no env vars.
				SSHKeys: []manifest.ProjectSSHKey{
					{Fingerprint: "ghi789=", Hostname: "gitlab.com"},
				},
			},
			{
				Slug: "gh/acme/cli",
				// Only env vars, no SSH keys.
				EnvVars: []manifest.ProjectEnvVar{{Name: "CLI_SECRET"}},
			},
		},
	}
}

// TestBuildPlan_IncludeSSHKeys_PopulatesSSHKeyPlans verifies that when
// IncludeSSHKeys is set, SSH keys are included in the project plans.
func TestBuildPlan_IncludeSSHKeys_PopulatesSSHKeyPlans(t *testing.T) {
	m := manifestWithSSHKeys()
	opts := baseOpts()
	opts.IncludeSSHKeys = true
	opts.Mapping = map[string]string{
		"gh/acme/web": "gh/acme-new/web",
		"gh/acme/api": "gh/acme-new/api",
		// gh/acme/cli intentionally not mapped to test skipping
	}

	plan, err := BuildPlan(m, &opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have plans for web (ssh keys mapped) and api (ssh keys mapped);
	// cli has no SSH keys so it won't appear.
	var webPlan, apiPlan *ProjectVarPlan
	for i := range plan.Projects {
		switch plan.Projects[i].SourceSlug {
		case "gh/acme/web":
			webPlan = &plan.Projects[i]
		case "gh/acme/api":
			apiPlan = &plan.Projects[i]
		}
	}

	if webPlan == nil {
		t.Fatal("expected gh/acme/web in plan (has SSH keys)")
	}
	if webPlan.Skipped {
		t.Errorf("gh/acme/web should not be skipped")
	}
	if len(webPlan.SSHKeys) != 2 {
		t.Errorf("gh/acme/web expected 2 SSH keys, got %d", len(webPlan.SSHKeys))
	}
	// Fingerprints should be present.
	fps := map[string]bool{}
	for _, k := range webPlan.SSHKeys {
		fps[k.Fingerprint] = true
	}
	if !fps["abc123="] || !fps["def456="] {
		t.Errorf("expected fingerprints abc123= and def456=, got: %+v", webPlan.SSHKeys)
	}
	// Hostname check.
	for _, k := range webPlan.SSHKeys {
		if k.Fingerprint == "abc123=" && k.Hostname != "github.com" {
			t.Errorf("abc123= hostname = %q, want github.com", k.Hostname)
		}
	}

	if apiPlan == nil {
		t.Fatal("expected gh/acme/api in plan (has SSH keys)")
	}
	if apiPlan.Skipped {
		t.Errorf("gh/acme/api should not be skipped")
	}
	if len(apiPlan.SSHKeys) != 1 {
		t.Errorf("gh/acme/api expected 1 SSH key, got %d", len(apiPlan.SSHKeys))
	}
	if apiPlan.SSHKeys[0].Fingerprint != "ghi789=" {
		t.Errorf("gh/acme/api fingerprint = %q, want ghi789=", apiPlan.SSHKeys[0].Fingerprint)
	}

	// TotalSSHKeys should be 3 (2 from web + 1 from api).
	if n := plan.TotalSSHKeys(); n != 3 {
		t.Errorf("TotalSSHKeys = %d, want 3", n)
	}
}

// TestBuildPlan_IncludeSSHKeys_ProjectWithOnlySSHKeys verifies that a project
// with only SSH keys (no env vars) gets a plan entry when IncludeSSHKeys is true
// even when IncludeProjectVars is false.
func TestBuildPlan_IncludeSSHKeys_ProjectWithOnlySSHKeys(t *testing.T) {
	m := manifestWithSSHKeys()
	opts := baseOpts()
	opts.IncludeSSHKeys = true
	opts.IncludeProjectVars = false // env vars off
	opts.Mapping = map[string]string{
		"gh/acme/api": "gh/acme-new/api",
	}

	plan, err := BuildPlan(m, &opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var apiPlan *ProjectVarPlan
	for i := range plan.Projects {
		if plan.Projects[i].SourceSlug == "gh/acme/api" {
			apiPlan = &plan.Projects[i]
		}
	}
	if apiPlan == nil {
		t.Fatal("expected gh/acme/api in plan (SSH keys, no env vars, IncludeSSHKeys=true)")
	}
	if apiPlan.Skipped {
		t.Errorf("gh/acme/api should not be skipped")
	}
	if len(apiPlan.SSHKeys) != 1 {
		t.Errorf("gh/acme/api expected 1 SSH key, got %d", len(apiPlan.SSHKeys))
	}
	// Env vars should be empty (IncludeProjectVars is off).
	if len(apiPlan.VarNames) != 0 {
		t.Errorf("gh/acme/api should have no env vars when IncludeProjectVars=false, got: %v", apiPlan.VarNames)
	}
}

// TestBuildPlan_IncludeSSHKeys_SkippedWhenNoMapping verifies that a project with
// SSH keys but no mapping entry is included as skipped.
func TestBuildPlan_IncludeSSHKeys_SkippedWhenNoMapping(t *testing.T) {
	m := manifestWithSSHKeys()
	opts := baseOpts()
	opts.IncludeSSHKeys = true
	// No mapping — all projects should be skipped.

	plan, err := BuildPlan(m, &opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, pp := range plan.Projects {
		if !pp.Skipped {
			t.Errorf("project %q should be skipped (no mapping), got Skipped=false", pp.SourceSlug)
		}
	}
}

// TestBuildPlan_MergeProjectVarsAndSSHKeys verifies that when both
// IncludeProjectVars and IncludeSSHKeys are set, a project with both env vars
// and SSH keys gets a single plan entry containing both.
func TestBuildPlan_MergeProjectVarsAndSSHKeys(t *testing.T) {
	m := manifestWithSSHKeys()
	opts := baseOpts()
	opts.IncludeProjectVars = true
	opts.IncludeSSHKeys = true
	opts.Mapping = map[string]string{
		"gh/acme/web": "gh/acme-new/web",
		"gh/acme/api": "gh/acme-new/api",
		"gh/acme/cli": "gh/acme-new/cli",
	}

	plan, err := BuildPlan(m, &opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var webPlan *ProjectVarPlan
	for i := range plan.Projects {
		if plan.Projects[i].SourceSlug == "gh/acme/web" {
			webPlan = &plan.Projects[i]
		}
	}
	if webPlan == nil {
		t.Fatal("expected gh/acme/web in plan")
	}
	if webPlan.Skipped {
		t.Errorf("gh/acme/web should not be skipped")
	}
	// gh/acme/web has 1 env var (APP_SECRET) and 2 SSH keys.
	if len(webPlan.VarNames) != 1 || webPlan.VarNames[0] != "APP_SECRET" {
		t.Errorf("gh/acme/web VarNames = %v, want [APP_SECRET]", webPlan.VarNames)
	}
	if len(webPlan.SSHKeys) != 2 {
		t.Errorf("gh/acme/web expected 2 SSH keys, got %d", len(webPlan.SSHKeys))
	}
}

// TestBuildSingleProjectTransferConfig_SSHKeysAddStep verifies that when a
// ProjectVarPlan has SSH keys, the generated config includes:
//   - an add_ssh_keys step with the fingerprints
//   - the SSH-key transfer run step with the correct POST endpoint
//   - jq --rawfile usage (no echo of key material)
func TestBuildSingleProjectTransferConfig_SSHKeysAddStep(t *testing.T) {
	opts := baseOpts()
	opts.DestTokenContext = "migration-secrets"

	pp := ProjectVarPlan{
		SourceSlug: "gh/acme/web",
		DestSlug:   "gh/acme-new/web",
		VarNames:   []string{"APP_SECRET"},
		SSHKeys: []SSHKeyPlan{
			{Fingerprint: "abc123=", Hostname: "github.com"},
			{Fingerprint: "def456=", Hostname: ""},
		},
	}

	cfg := buildSingleProjectTransferConfigWithVersion(pp, &opts)

	// Must contain add_ssh_keys with the correct fingerprints.
	if !strings.Contains(cfg, "add_ssh_keys:") {
		t.Error("expected add_ssh_keys step in config")
	}
	if !strings.Contains(cfg, "SHA256:abc123=") {
		t.Error("expected SHA256:abc123= fingerprint in add_ssh_keys")
	}
	if !strings.Contains(cfg, "SHA256:def456=") {
		t.Error("expected SHA256:def456= fingerprint in add_ssh_keys")
	}

	// Must contain the SSH-key transfer run step.
	if !strings.Contains(cfg, "Transfer additional SSH keys") {
		t.Error("expected SSH key transfer step name in config")
	}

	// Must POST to /api/v1.1/project/{slug}/ssh-key.
	if !strings.Contains(cfg, "/api/v1.1/project/") {
		t.Error("expected v1.1 project endpoint in ssh-key transfer step")
	}
	if !strings.Contains(cfg, "/ssh-key") {
		t.Error("expected /ssh-key path in config")
	}

	// Must use jq --rawfile (no echo of key material).
	if !strings.Contains(cfg, "jq --rawfile") || !strings.Contains(cfg, "--rawfile pk") {
		t.Error("expected jq --rawfile usage for safe private key capture")
	}

	// Must use ssh-keygen to match fingerprints.
	if !strings.Contains(cfg, "ssh-keygen -lf") {
		t.Error("expected ssh-keygen -lf for fingerprint matching")
	}

	// Must build JSON body with hostname and private_key fields.
	if !strings.Contains(cfg, `"hostname"`) {
		t.Error("expected hostname field in POST body")
	}
	if !strings.Contains(cfg, `"private_key"`) {
		t.Error("expected private_key field in POST body")
	}

	// Must reference the dest project slug.
	if !strings.Contains(cfg, "gh/acme-new/web") {
		t.Error("expected dest project slug in ssh-key transfer step")
	}

	// Env-var transfer step should also be present.
	if !strings.Contains(cfg, "APP_SECRET") {
		t.Error("expected APP_SECRET var in config (env-var transfer step)")
	}
}

// TestBuildSingleProjectTransferConfig_SSHKeysOnly verifies that a plan with
// SSH keys but NO env vars still generates a valid config (no env-var step,
// but add_ssh_keys and SSH-key transfer steps are present).
func TestBuildSingleProjectTransferConfig_SSHKeysOnly(t *testing.T) {
	opts := baseOpts()
	opts.DestTokenContext = "migration-secrets"

	pp := ProjectVarPlan{
		SourceSlug: "gh/acme/api",
		DestSlug:   "gh/acme-new/api",
		// No VarNames.
		SSHKeys: []SSHKeyPlan{
			{Fingerprint: "ghi789=", Hostname: "gitlab.com"},
		},
	}

	cfg := buildSingleProjectTransferConfigWithVersion(pp, &opts)

	// Must contain add_ssh_keys.
	if !strings.Contains(cfg, "add_ssh_keys:") {
		t.Error("expected add_ssh_keys step in config")
	}
	if !strings.Contains(cfg, "SHA256:ghi789=") {
		t.Error("expected SHA256:ghi789= fingerprint")
	}

	// Must contain the SSH key transfer step.
	if !strings.Contains(cfg, "Transfer additional SSH keys") {
		t.Error("expected SSH key transfer step")
	}

	// Must NOT contain an env-var transfer step (no env vars).
	if strings.Contains(cfg, "Transfer project env vars") {
		t.Error("config must NOT contain env-var transfer step when VarNames is empty")
	}

	// Must NOT contain /envvar (no env-var transfer).
	if strings.Contains(cfg, "/envvar") {
		t.Error("config must NOT contain /envvar endpoint when there are no env vars")
	}

	// Must be syntactically valid YAML (starts with version: 2.1).
	if !strings.HasPrefix(cfg, "version: 2.1") {
		t.Error("config must start with 'version: 2.1'")
	}

	// Dest-token context must appear in the workflow.
	if !strings.Contains(cfg, "- migration-secrets") {
		t.Error("expected dest-token context in workflow job context list")
	}
}

// TestBuildSingleProjectTransferConfig_SSHKeysNeverEchoed verifies that the
// generated config never contains an echo or cat of a private key variable.
func TestBuildSingleProjectTransferConfig_SSHKeysNeverEchoed(t *testing.T) {
	opts := baseOpts()
	opts.DestTokenContext = "migration-secrets"

	pp := ProjectVarPlan{
		SourceSlug: "gh/acme/web",
		DestSlug:   "gh/acme-new/web",
		SSHKeys: []SSHKeyPlan{
			{Fingerprint: "abc123=", Hostname: "github.com"},
		},
	}

	cfg := buildSingleProjectTransferConfigWithVersion(pp, &opts)

	// Forbidden patterns that would expose key material.
	forbidden := []string{
		"echo $pk",
		"echo \"$pk\"",
		"cat $pk",
		"cat \"$pk\"",
	}
	for _, pattern := range forbidden {
		if strings.Contains(cfg, pattern) {
			t.Errorf("config must not contain %q (key material exposure)", pattern)
		}
	}
}

// TestBuildPlan_IncludeSSHKeys_OffByDefault verifies that when IncludeSSHKeys
// is false (default), SSH keys are NOT included in project plans.
func TestBuildPlan_IncludeSSHKeys_OffByDefault(t *testing.T) {
	m := manifestWithSSHKeys()
	opts := baseOpts()
	// IncludeSSHKeys is false by default; IncludeProjectVars also false.
	// The plan should have no project plans (no contexts with SSH keys are included).
	// With contexts only there should be a valid plan (there is one context with a var).

	plan, err := BuildPlan(m, &opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(plan.Projects) != 0 {
		t.Errorf("expected no project plans when IncludeSSHKeys=false, got %d", len(plan.Projects))
	}
	if plan.TotalSSHKeys() != 0 {
		t.Errorf("expected TotalSSHKeys=0 when IncludeSSHKeys=false, got %d", plan.TotalSSHKeys())
	}
}

// TestTransfer_SSHKeysOnly_TriggersPerProjectPipeline verifies that a project
// with only SSH keys (no env vars) triggers a per-project pipeline when
// IncludeSSHKeys is true.
func TestTransfer_SSHKeysOnly_TriggersPerProjectPipeline(t *testing.T) {
	// Manifest: no context vars, one project with only SSH keys.
	m := &manifest.Manifest{
		Source: manifest.Source{Org: manifest.Org{ID: "src-org-uuid"}},
		Contexts: []manifest.Context{
			{Name: "deploy-prod", EnvVars: []manifest.ContextEnvVar{{Name: "AWS_KEY"}}},
		},
		Projects: []manifest.Project{
			{
				Slug: "gh/acme/api",
				// No env vars — only SSH keys.
				SSHKeys: []manifest.ProjectSSHKey{
					{Fingerprint: "ghi789=", Hostname: "gitlab.com"},
				},
			},
		},
	}

	deps := happyMultiDeps()
	opts := baseOpts()
	opts.DryRun = false
	opts.HostProjectSlug = "gh/acme/api"
	opts.IncludeSSHKeys = true
	opts.Mapping = map[string]string{
		"gh/acme/api": "gh/acme-new/api",
	}

	var out, errOut bytes.Buffer
	opts.Stdout = &out
	opts.Stderr = &errOut

	if err := Transfer(context.Background(), deps, m, opts); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	deps.mu.Lock()
	calls := deps.triggerCalls
	deps.mu.Unlock()

	// 2 calls: 1 context pipeline + 1 per-project pipeline for SSH keys.
	if len(calls) != 2 {
		t.Fatalf("expected 2 TriggerPipelineRun calls (1 ctx + 1 ssh-key project), got %d: %v",
			len(calls), slugsFromCalls(calls))
	}

	// The per-project pipeline must use gh/acme/api slug.
	if !containsSlug(slugsFromCalls(calls), "gh/acme/api") {
		t.Errorf("expected per-project pipeline under gh/acme/api")
	}

	// The per-project pipeline config (last call for gh/acme/api) must contain add_ssh_keys.
	// The first call is the context pipeline (no add_ssh_keys); the second is the
	// per-project pipeline (has add_ssh_keys + ssh-key POST).
	// Find the per-project pipeline by looking for the one that has add_ssh_keys.
	var perProjectYAML string
	for _, c := range calls {
		if c.slug == "gh/acme/api" && strings.Contains(c.yaml, "add_ssh_keys") {
			perProjectYAML = c.yaml
			break
		}
	}
	if perProjectYAML == "" {
		t.Fatal("no per-project pipeline config found with add_ssh_keys for gh/acme/api")
	}
	if !strings.Contains(perProjectYAML, "SHA256:ghi789=") {
		t.Error("per-project pipeline config must contain the SSH key fingerprint SHA256:ghi789=")
	}
	if !strings.Contains(perProjectYAML, "/ssh-key") {
		t.Error("per-project pipeline config must reference the /ssh-key endpoint")
	}
}

// TestPrintPlan_ShowsSSHKeyCount verifies that the plan output includes SSH key
// counts when present.
func TestPrintPlan_ShowsSSHKeyCount(t *testing.T) {
	plan := &Plan{
		Contexts: []ContextPlan{
			{SourceName: "deploy-prod", DestName: "deploy-prod", VarNames: []string{"AWS_KEY"}},
		},
		Projects: []ProjectVarPlan{
			{
				SourceSlug: "gh/acme/web",
				DestSlug:   "gh/acme-new/web",
				VarNames:   []string{"APP_SECRET"},
				SSHKeys: []SSHKeyPlan{
					{Fingerprint: "abc123=", Hostname: "github.com"},
				},
			},
		},
		DestTokenContext: "migration-secrets",
		DestTokenEnvVar:  "CIRCLECI_DEST_TOKEN",
	}
	opts := baseOpts()

	var out, errOut bytes.Buffer
	printPlan(&out, &errOut, plan, &opts)

	outStr := out.String()

	// Should mention SSH key count.
	if !strings.Contains(outStr, "ssh key") {
		t.Error("expected 'ssh key' in plan output when SSH keys are present")
	}
	// Should show the fingerprint.
	if !strings.Contains(outStr, "abc123=") {
		t.Error("expected fingerprint abc123= in plan output")
	}
	// Should show the hostname.
	if !strings.Contains(outStr, "github.com") {
		t.Error("expected hostname github.com in plan output")
	}
}

// TestPlan_TotalSSHKeys verifies the TotalSSHKeys helper.
func TestPlan_TotalSSHKeys(t *testing.T) {
	p := Plan{
		Projects: []ProjectVarPlan{
			{SourceSlug: "a", DestSlug: "a-new", SSHKeys: []SSHKeyPlan{{Fingerprint: "fp1", Hostname: "h1"}, {Fingerprint: "fp2", Hostname: ""}}},
			{SourceSlug: "b", Skipped: true}, // skipped — should not count
			{SourceSlug: "c", DestSlug: "c-new", SSHKeys: []SSHKeyPlan{{Fingerprint: "fp3", Hostname: "h3"}}},
		},
	}
	if got := p.TotalSSHKeys(); got != 3 {
		t.Errorf("TotalSSHKeys = %d, want 3", got)
	}
}

// TestPlan_TotalSSHKeys_Empty verifies that an empty plan returns 0.
func TestPlan_TotalSSHKeys_Empty(t *testing.T) {
	p := Plan{}
	if n := p.TotalSSHKeys(); n != 0 {
		t.Errorf("TotalSSHKeys of empty plan = %d, want 0", n)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// BuildPlan — blocking restriction detection
// ─────────────────────────────────────────────────────────────────────────────

// manifestWithRestrictions returns a Manifest with one context that has the
// given restrictions set on it.
func manifestWithRestrictions(restr []manifest.Restriction) *manifest.Manifest {
	return &manifest.Manifest{
		Source: manifest.Source{Org: manifest.Org{ID: "src-org"}},
		Contexts: []manifest.Context{
			{
				Name:         "restricted-ctx",
				SourceID:     "ctx-uuid-1",
				Restrictions: restr,
				EnvVars:      []manifest.ContextEnvVar{{Name: "SECRET_A"}},
			},
			{
				Name:    "open-ctx",
				EnvVars: []manifest.ContextEnvVar{{Name: "SECRET_B"}},
			},
		},
	}
}

// TestBuildPlan_BlockingRestrictionsProjectType verifies that project-type
// restrictions are captured in BlockingRestrictions.
func TestBuildPlan_BlockingRestrictionsProjectType(t *testing.T) {
	restr := []manifest.Restriction{
		{Type: "project", Value: "proj-uuid-a"},
	}
	m := manifestWithRestrictions(restr)
	opts := baseOpts()

	plan, err := BuildPlan(m, &opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var restrictedCtx *ContextPlan
	for i := range plan.Contexts {
		if plan.Contexts[i].SourceName == "restricted-ctx" {
			restrictedCtx = &plan.Contexts[i]
		}
	}
	if restrictedCtx == nil {
		t.Fatal("expected restricted-ctx in plan")
	}
	if len(restrictedCtx.BlockingRestrictions) != 1 {
		t.Fatalf("expected 1 blocking restriction, got %d: %v", len(restrictedCtx.BlockingRestrictions), restrictedCtx.BlockingRestrictions)
	}
	if restrictedCtx.BlockingRestrictions[0].Type != "project" {
		t.Errorf("expected type=project, got %q", restrictedCtx.BlockingRestrictions[0].Type)
	}
}

// TestBuildPlan_BlockingRestrictionsExpressionType verifies that expression
// restrictions are also captured.
func TestBuildPlan_BlockingRestrictionsExpressionType(t *testing.T) {
	restr := []manifest.Restriction{
		{Type: "expression", Value: "project.id = 'proj-abc'"},
	}
	m := manifestWithRestrictions(restr)
	opts := baseOpts()

	plan, err := BuildPlan(m, &opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, cp := range plan.Contexts {
		if cp.SourceName == "restricted-ctx" {
			if len(cp.BlockingRestrictions) != 1 || cp.BlockingRestrictions[0].Type != "expression" {
				t.Errorf("expected 1 expression blocking restriction, got: %v", cp.BlockingRestrictions)
			}
			return
		}
	}
	t.Fatal("restricted-ctx not found in plan")
}

// TestBuildPlan_GroupRestrictionsNotBlocking verifies that group-type
// restrictions (including the default "All members" group) are never
// treated as blocking.
func TestBuildPlan_GroupRestrictionsNotBlocking(t *testing.T) {
	restr := []manifest.Restriction{
		{Type: "group", Value: "src-org"},               // default "All members" restriction
		{Type: "group", Value: "some-other-group-uuid"}, // non-default group restriction
	}
	m := manifestWithRestrictions(restr)
	opts := baseOpts()

	plan, err := BuildPlan(m, &opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, cp := range plan.Contexts {
		if cp.SourceName == "restricted-ctx" {
			if len(cp.BlockingRestrictions) != 0 {
				t.Errorf("group restrictions must NOT be blocking, got: %v", cp.BlockingRestrictions)
			}
			return
		}
	}
	t.Fatal("restricted-ctx not found in plan")
}

// TestBuildPlan_MixedRestrictionsOnlyProjectBlocking verifies that when a
// context has both group and project restrictions, only the project restriction
// ends up in BlockingRestrictions.
func TestBuildPlan_MixedRestrictionsOnlyProjectBlocking(t *testing.T) {
	restr := []manifest.Restriction{
		{Type: "group", Value: "src-org"},
		{Type: "project", Value: "proj-uuid-x"},
	}
	m := manifestWithRestrictions(restr)
	opts := baseOpts()

	plan, err := BuildPlan(m, &opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, cp := range plan.Contexts {
		if cp.SourceName == "restricted-ctx" {
			if len(cp.BlockingRestrictions) != 1 {
				t.Fatalf("expected exactly 1 blocking restriction (the project type), got: %v", cp.BlockingRestrictions)
			}
			if cp.BlockingRestrictions[0].Type != "project" {
				t.Errorf("expected type=project, got %q", cp.BlockingRestrictions[0].Type)
			}
			return
		}
	}
	t.Fatal("restricted-ctx not found in plan")
}

// ─────────────────────────────────────────────────────────────────────────────
// handleContextRestrictions — fail-fast path
// ─────────────────────────────────────────────────────────────────────────────

// TestHandleContextRestrictions_NoBlocking verifies that handleContextRestrictions
// returns nil when no contexts have blocking restrictions.
func TestHandleContextRestrictions_NoBlocking(t *testing.T) {
	m := baseManifest()
	opts := baseOpts()
	plan, err := BuildPlan(m, &opts)
	if err != nil {
		t.Fatalf("build plan: %v", err)
	}

	if err := handleContextRestrictions(context.Background(), m, &plan, &opts); err != nil {
		t.Errorf("expected nil, got: %v", err)
	}
}

// TestHandleContextRestrictions_FailFastWithoutFlag verifies that
// handleContextRestrictions returns an actionable error (mentioning
// --remove-restrictions) when blocking restrictions exist and the flag is
// not set.
func TestHandleContextRestrictions_FailFastWithoutFlag(t *testing.T) {
	restr := []manifest.Restriction{
		{Type: "project", Value: "proj-uuid-x"},
	}
	m := manifestWithRestrictions(restr)
	opts := baseOpts()
	// RemoveRestrictions is false (the default)

	plan, err := BuildPlan(m, &opts)
	if err != nil {
		t.Fatalf("build plan: %v", err)
	}

	gotErr := handleContextRestrictions(context.Background(), m, &plan, &opts)
	if gotErr == nil {
		t.Fatal("expected an error, got nil")
	}
	errMsg := gotErr.Error()
	if !strings.Contains(errMsg, "--remove-restrictions") {
		t.Errorf("error should mention --remove-restrictions, got: %q", errMsg)
	}
	if !strings.Contains(errMsg, "restricted-ctx") {
		t.Errorf("error should name the blocking context, got: %q", errMsg)
	}
	if !strings.Contains(errMsg, "project") {
		t.Errorf("error should mention restriction type 'project', got: %q", errMsg)
	}
}

// TestHandleContextRestrictions_FailFastMentionsHostProject verifies that the
// error message includes the host project slug.
func TestHandleContextRestrictions_FailFastMentionsHostProject(t *testing.T) {
	restr := []manifest.Restriction{
		{Type: "expression", Value: "project.id = 'p'"},
	}
	m := manifestWithRestrictions(restr)
	opts := baseOpts()
	opts.HostProjectSlug = "gh/acme/web"

	plan, err := BuildPlan(m, &opts)
	if err != nil {
		t.Fatalf("build plan: %v", err)
	}

	gotErr := handleContextRestrictions(context.Background(), m, &plan, &opts)
	if gotErr == nil {
		t.Fatal("expected an error, got nil")
	}
	if !strings.Contains(gotErr.Error(), "gh/acme/web") {
		t.Errorf("error should include host project slug, got: %q", gotErr.Error())
	}
}

// TestHandleContextRestrictions_NoClientError verifies that requesting
// --remove-restrictions without a ContextClient is an error.
func TestHandleContextRestrictions_NoClientError(t *testing.T) {
	restr := []manifest.Restriction{
		{Type: "project", Value: "proj-uuid-y"},
	}
	m := manifestWithRestrictions(restr)
	opts := baseOpts()
	opts.RemoveRestrictions = true
	opts.ContextClient = nil // no client

	plan, err := BuildPlan(m, &opts)
	if err != nil {
		t.Fatalf("build plan: %v", err)
	}

	gotErr := handleContextRestrictions(context.Background(), m, &plan, &opts)
	if gotErr == nil {
		t.Fatal("expected an error when ContextClient is nil, got nil")
	}
	if !strings.Contains(gotErr.Error(), "ContextClient") {
		t.Errorf("error should mention ContextClient, got: %q", gotErr.Error())
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// fakeContextClient — test double for ContextRestrictionManager
// ─────────────────────────────────────────────────────────────────────────────

// fakeContextClient is a simple in-memory fake that records calls.
type fakeContextClient struct {
	mu              sync.Mutex
	liveByContextID map[string][]apicontext.Restriction
	deleteCalls     []string // restriction IDs deleted
	createCalls     []struct{ contextID, typ, value string }
	listErr         error
	deleteErr       error
	createErr       error
}

func (f *fakeContextClient) ListRestrictions(_ context.Context, contextID string) ([]apicontext.Restriction, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.liveByContextID[contextID], nil
}

func (f *fakeContextClient) DeleteRestriction(_ context.Context, _ string, restrictionID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.deleteErr != nil {
		return f.deleteErr
	}
	f.deleteCalls = append(f.deleteCalls, restrictionID)
	return nil
}

func (f *fakeContextClient) CreateRestriction(_ context.Context, contextID, restrictionType, restrictionValue string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.createErr != nil {
		return f.createErr
	}
	f.createCalls = append(f.createCalls, struct{ contextID, typ, value string }{contextID, restrictionType, restrictionValue})
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// handleContextRestrictions — remove+restore path
// ─────────────────────────────────────────────────────────────────────────────

// TestHandleContextRestrictions_RemoveAndRestore verifies that when
// --remove-restrictions is set, handleContextRestrictions deletes the live
// project/expression restrictions and the returned restore closure recreates
// them.
func TestHandleContextRestrictions_RemoveAndRestore(t *testing.T) {
	// Manifest context has a project restriction.
	restr := []manifest.Restriction{
		{Type: "project", Value: "proj-uuid-abc"},
	}
	m := manifestWithRestrictions(restr)

	// Fake live restrictions (what the API would return for the context).
	fake := &fakeContextClient{
		liveByContextID: map[string][]apicontext.Restriction{
			"ctx-uuid-1": {
				{ID: "live-restr-1", Type: "project", Value: "proj-uuid-abc"},
			},
		},
	}

	opts := baseOpts()
	opts.RemoveRestrictions = true
	opts.ContextClient = fake

	plan, err := BuildPlan(m, &opts)
	if err != nil {
		t.Fatalf("build plan: %v", err)
	}

	// handleContextRestrictions registers deferred restores — we call it here
	// without defer, then manually invoke a "restore" trigger via a helper that
	// mirrors what Transfer() does at function exit.
	//
	// Because the restore closures are registered with `defer restore()` inside
	// handleContextRestrictions itself, they execute when handleContextRestrictions
	// returns — this test can only verify the DELETE calls that happened before
	// the function returned.
	gotErr := handleContextRestrictions(context.Background(), m, &plan, &opts)
	if gotErr != nil {
		t.Fatalf("expected nil error, got: %v", gotErr)
	}

	// The project restriction should have been deleted.
	fake.mu.Lock()
	deleteCalls := append([]string(nil), fake.deleteCalls...)
	fake.mu.Unlock()

	if len(deleteCalls) != 1 {
		t.Fatalf("expected 1 delete call, got %d: %v", len(deleteCalls), deleteCalls)
	}
	if deleteCalls[0] != "live-restr-1" {
		t.Errorf("expected delete of 'live-restr-1', got %q", deleteCalls[0])
	}
}

// TestHandleContextRestrictions_GroupRestrictionsNotDeleted verifies that
// group restrictions are NEVER deleted even when --remove-restrictions is set.
func TestHandleContextRestrictions_GroupRestrictionsNotDeleted(t *testing.T) {
	// Context has only a group restriction (should never be blocking).
	restr := []manifest.Restriction{
		{Type: "group", Value: "src-org"},
	}
	m := manifestWithRestrictions(restr)

	fake := &fakeContextClient{
		liveByContextID: map[string][]apicontext.Restriction{
			"ctx-uuid-1": {
				{ID: "group-restr-id", Type: "group", Value: "src-org"},
			},
		},
	}

	opts := baseOpts()
	opts.RemoveRestrictions = true
	opts.ContextClient = fake

	plan, err := BuildPlan(m, &opts)
	if err != nil {
		t.Fatalf("build plan: %v", err)
	}

	// No blocking restrictions (all group), so handleContextRestrictions is a no-op.
	gotErr := handleContextRestrictions(context.Background(), m, &plan, &opts)
	if gotErr != nil {
		t.Fatalf("expected nil, got: %v", gotErr)
	}

	fake.mu.Lock()
	deleteCalls := fake.deleteCalls
	fake.mu.Unlock()

	if len(deleteCalls) != 0 {
		t.Errorf("expected NO delete calls for group-only restrictions, got: %v", deleteCalls)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// prepareTransferRestrictionRemoval
// ─────────────────────────────────────────────────────────────────────────────

// TestPrepareTransferRestrictionRemoval_DeletesProjectRestrictions verifies
// that only project/expression live restrictions are deleted (not group ones).
func TestPrepareTransferRestrictionRemoval_DeletesProjectRestrictions(t *testing.T) {
	mc := &manifest.Context{
		Name:     "ctx-a",
		SourceID: "ctx-uuid-a",
		Restrictions: []manifest.Restriction{
			{Type: "project", Value: "proj-1"},
			{Type: "expression", Value: "expr-1"},
		},
	}

	fake := &fakeContextClient{
		liveByContextID: map[string][]apicontext.Restriction{
			"ctx-uuid-a": {
				{ID: "live-proj", Type: "project", Value: "proj-1"},
				{ID: "live-expr", Type: "expression", Value: "expr-1"},
				{ID: "live-grp", Type: "group", Value: "org-uuid"},
			},
		},
	}

	var errBuf bytes.Buffer
	restore, err := prepareTransferRestrictionRemoval(context.Background(), &errBuf, fake, mc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Two project/expression restrictions deleted; group must NOT be deleted.
	fake.mu.Lock()
	deleteCalls := append([]string(nil), fake.deleteCalls...)
	fake.mu.Unlock()

	if len(deleteCalls) != 2 {
		t.Fatalf("expected 2 deletes, got %d: %v", len(deleteCalls), deleteCalls)
	}
	for _, id := range deleteCalls {
		if id == "live-grp" {
			t.Errorf("group restriction must NOT be deleted, but it was")
		}
	}

	// Invoke restore — should recreate the project and expression restrictions.
	restore()

	fake.mu.Lock()
	createCalls := append([]struct{ contextID, typ, value string }(nil), fake.createCalls...)
	fake.mu.Unlock()

	if len(createCalls) != 2 {
		t.Fatalf("expected 2 create calls after restore, got %d: %v", len(createCalls), createCalls)
	}
	types := map[string]bool{}
	for _, c := range createCalls {
		types[c.typ] = true
		if c.contextID != "ctx-uuid-a" {
			t.Errorf("expected contextID ctx-uuid-a, got %q", c.contextID)
		}
	}
	if !types["project"] {
		t.Error("expected project restriction to be restored")
	}
	if !types["expression"] {
		t.Error("expected expression restriction to be restored")
	}
}

// TestPrepareTransferRestrictionRemoval_ListError verifies that a ListRestrictions
// failure is propagated as an error.
func TestPrepareTransferRestrictionRemoval_ListError(t *testing.T) {
	mc := &manifest.Context{
		Name:     "ctx-b",
		SourceID: "ctx-uuid-b",
		Restrictions: []manifest.Restriction{
			{Type: "project", Value: "p"},
		},
	}

	fake := &fakeContextClient{
		listErr: errors.New("API unavailable"),
	}

	var errBuf bytes.Buffer
	_, err := prepareTransferRestrictionRemoval(context.Background(), &errBuf, fake, mc)
	if err == nil {
		t.Fatal("expected error when ListRestrictions fails")
	}
	if !strings.Contains(err.Error(), "listing live restrictions") {
		t.Errorf("error should mention listing, got: %q", err.Error())
	}
}

// TestPrepareTransferRestrictionRemoval_DeleteError propagates delete failures.
func TestPrepareTransferRestrictionRemoval_DeleteError(t *testing.T) {
	mc := &manifest.Context{
		Name:     "ctx-c",
		SourceID: "ctx-uuid-c",
		Restrictions: []manifest.Restriction{
			{Type: "project", Value: "p"},
		},
	}

	fake := &fakeContextClient{
		liveByContextID: map[string][]apicontext.Restriction{
			"ctx-uuid-c": {
				{ID: "rid-1", Type: "project", Value: "p"},
			},
		},
		deleteErr: errors.New("forbidden"),
	}

	var errBuf bytes.Buffer
	_, err := prepareTransferRestrictionRemoval(context.Background(), &errBuf, fake, mc)
	if err == nil {
		t.Fatal("expected error when DeleteRestriction fails")
	}
	if !strings.Contains(err.Error(), "deleting restriction") {
		t.Errorf("error should mention deleting, got: %q", err.Error())
	}
}

// TestPrepareTransferRestrictionRemoval_RestoreCreateError verifies that a
// create failure during restore is reported as a warning (not panic), and the
// restore func does not return an error (best-effort).
func TestPrepareTransferRestrictionRemoval_RestoreCreateError(t *testing.T) {
	mc := &manifest.Context{
		Name:     "ctx-d",
		SourceID: "ctx-uuid-d",
		Restrictions: []manifest.Restriction{
			{Type: "project", Value: "proj-q"},
		},
	}

	fake := &fakeContextClient{
		liveByContextID: map[string][]apicontext.Restriction{
			"ctx-uuid-d": {
				{ID: "rid-2", Type: "project", Value: "proj-q"},
			},
		},
	}

	var errBuf bytes.Buffer
	restore, err := prepareTransferRestrictionRemoval(context.Background(), &errBuf, fake, mc)
	if err != nil {
		t.Fatalf("unexpected error from prepare: %v", err)
	}

	// Now simulate CreateRestriction failing during restore.
	fake.mu.Lock()
	fake.createErr = errors.New("create forbidden")
	fake.mu.Unlock()

	// Restore must not panic; failure is reported as WARNING in stderr.
	restore()

	warnOutput := errBuf.String()
	if !strings.Contains(warnOutput, "WARNING") {
		t.Errorf("expected WARNING in stderr when restore fails, got: %q", warnOutput)
	}
	if !strings.Contains(warnOutput, "manually") {
		t.Errorf("expected 'manually' guidance in warning, got: %q", warnOutput)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Unauthorized retry message content
// ─────────────────────────────────────────────────────────────────────────────

// TestUnauthorizedRetryMessage_ContainsRestrictedContextGuidance verifies that
// the exhausted-retry error message from triggerAndPollProjectPipeline mentions
// restricted contexts and --remove-restrictions (not just "authorization propagation").
func TestUnauthorizedRetryMessage_ContainsRestrictedContextGuidance(t *testing.T) {
	// Build a fake deps that always returns "unauthorized" workflow status.
	// unauthorizedRetryMax+1 workflows so every attempt gets "unauthorized".
	unauthorizedWFs := make([][]project.Workflow, unauthorizedRetryMax+2)
	for i := range unauthorizedWFs {
		unauthorizedWFs[i] = []project.Workflow{{ID: "wf-1", Name: "transfer", Status: "unauthorized"}}
	}

	fake := &fakeTransferDeps{
		proj:      &project.Project{ID: "proj-id", Slug: "gh/acme/web"},
		defs:      []project.PipelineDefinition{{ID: "def-1"}},
		triggerID: "pipeline-uuid-1",
		workflows: unauthorizedWFs,
	}

	pp := ProjectVarPlan{
		SourceSlug: "gh/acme/web",
		DestSlug:   "gh/acme-new/web",
		VarNames:   []string{"MY_VAR"},
	}
	opts := baseOpts()
	opts.DryRun = false
	opts.PollInterval = 1 // 1ns; avoids real sleeps in tests

	var errBuf bytes.Buffer
	err := triggerAndPollProjectPipeline(context.Background(), fake, pp, &opts, &errBuf)
	if err == nil {
		t.Fatal("expected error when workflow always unauthorized")
	}
	errMsg := err.Error()
	if !strings.Contains(errMsg, "restricted context") {
		t.Errorf("exhausted-retry message should mention 'restricted context', got: %q", errMsg)
	}
	if !strings.Contains(errMsg, "--remove-restrictions") {
		t.Errorf("exhausted-retry message should mention '--remove-restrictions', got: %q", errMsg)
	}
}
