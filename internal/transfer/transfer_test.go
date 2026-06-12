package transfer

import (
	"bytes"
	"context"
	"errors"
	"strings"
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
