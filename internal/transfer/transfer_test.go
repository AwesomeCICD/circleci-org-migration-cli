package transfer

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

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

	cfg := buildTransferConfigWithVersion(m, plan.Contexts, nil, &opts, "v0.9.0")

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

	cfg := buildTransferConfigWithVersion(m, plan.Contexts, nil, &opts, "v0.9.0")

	// The context should appear only once in the workflow context list.
	count := strings.Count(cfg, "- migration-secrets")
	if count != 1 {
		t.Errorf("expected migration-secrets to appear once in context list, got %d", count)
	}
}

func TestBuildTransferConfig_Version(t *testing.T) {
	m := baseManifest()
	opts := baseOpts()
	opts.DestTokenContext = "migration-secrets"

	plan, _ := BuildPlan(m, &opts)

	// With a pinned version, the config should embed that version.
	cfg := buildTransferConfigWithVersion(m, plan.Contexts, nil, &opts, "v0.9.0")
	if !strings.Contains(cfg, "v0.9.0") {
		t.Error("expected pinned version in install step")
	}

	// With dev/empty version, should use "latest".
	cfgDev := buildTransferConfigWithVersion(m, plan.Contexts, nil, &opts, "dev")
	if !strings.Contains(cfgDev, "releases/latest") {
		t.Error("dev build should fall back to 'latest' release")
	}
}

func TestBuildTransferConfig_DestHostEmbedded(t *testing.T) {
	m := baseManifest()
	opts := baseOpts()
	opts.DestTokenContext = "migration-secrets"
	opts.DestHost = "https://circleci.example.com"

	plan, _ := BuildPlan(m, &opts)
	cfg := buildTransferConfigWithVersion(m, plan.Contexts, nil, &opts, "v1.0.0")

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
	cfg := buildTransferConfigWithVersion(m, plan.Contexts, nil, &opts, "v0.9.0")

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
	cfg := buildTransferConfigWithVersion(m, plan.Contexts, nil, &opts, "v1.0.0")

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

	cfg := buildTransferConfigWithVersion(m, plan.Contexts, nil, &opts, "v1.0.0")

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

	cfg := buildTransferConfigWithVersion(m, plan.Contexts, plan.Projects, &opts, "v1.0.0")

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

	webCfg := buildSingleProjectTransferConfigWithVersion(webPlan, &opts, "v1.0.0")

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
	apiCfg := buildSingleProjectTransferConfigWithVersion(apiPlan, &opts, "v1.0.0")
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

	cfg := buildSingleProjectTransferConfigWithVersion(pp, &opts, "v1.0.0")

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

	cfg := buildSingleProjectTransferConfigWithVersion(pp, &opts, "v1.0.0")

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
