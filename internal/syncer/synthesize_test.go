package syncer

import (
	"context"
	"errors"
	"testing"

	"github.com/AwesomeCICD/circleci-org-migration-cli/api/project"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/github"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
)

// ---------------------------------------------------------------------------
// translateEventPreset
// ---------------------------------------------------------------------------

func TestTranslateEventPreset(t *testing.T) {
	tests := []struct {
		name     string
		settings *manifest.AdvancedSettings
		want     string
	}{
		{"nil settings", nil, "all-pushes"},
		{"empty settings", &manifest.AdvancedSettings{}, "all-pushes"},
		{"build prs only false", &manifest.AdvancedSettings{BuildPRsOnly: boolPtr(false)}, "all-pushes"},
		{"build prs only true", &manifest.AdvancedSettings{BuildPRsOnly: boolPtr(true)}, "only-build-prs"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := translateEventPreset(tt.settings)
			if got != tt.want {
				t.Errorf("translateEventPreset: got %q want %q", got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// oauthSourceFullName
// ---------------------------------------------------------------------------

func TestOAuthSourceFullName(t *testing.T) {
	tests := []struct {
		slug     string
		wantName string
		wantOK   bool
	}{
		{"gh/acme/web", "acme/web", true},
		{"github/acme/web", "acme/web", true},
		{"GH/acme/web", "acme/web", true},
		{"circleci/org-id/proj-id", "", false},
		{"gitlab/acme/web", "", false},
		{"bad-slug", "", false},
	}
	for _, tt := range tests {
		got, ok := oauthSourceFullName(tt.slug)
		if got != tt.wantName || ok != tt.wantOK {
			t.Errorf("oauthSourceFullName(%q): got (%q,%v) want (%q,%v)", tt.slug, got, ok, tt.wantName, tt.wantOK)
		}
	}
}

// ---------------------------------------------------------------------------
// oauthSourceProject: an OAuth project with NO pipeline definitions.
// ---------------------------------------------------------------------------

// oauthSourceProject builds a manifest.Project for an OAuth source (gh/ slug,
// no pipeline definitions) with the given advanced settings.
func oauthSourceProject(name, slug string, settings *manifest.AdvancedSettings) manifest.Project {
	return manifest.Project{Slug: slug, Name: name, Settings: settings}
}

// ---------------------------------------------------------------------------
// syncAppProject: OAuth source → App dest synthesizes a pipeline-def + trigger
// ---------------------------------------------------------------------------

func TestSyncProjects_AppDest_OAuthSource_Synthesizes(t *testing.T) {
	stubResolveRepoID(t, "acme/web", "resolved-ext-id", nil)

	var gotDefSpec project.PipelineDefinitionSpec
	var gotTrigSpec project.TriggerSpec
	fp := &fakeProjectWriter{
		createPipelineDefinition: func(projectID string, spec project.PipelineDefinitionSpec) (string, error) {
			gotDefSpec = spec
			return "def-id", nil
		},
		createTrigger: func(projectID, defID string, spec project.TriggerSpec) (string, error) {
			gotTrigSpec = spec
			return "trig-id", nil
		},
	}
	sy := newSyncerAppProjects(fp)

	p := oauthSourceProject("web", "gh/acme/web", &manifest.AdvancedSettings{BuildPRsOnly: boolPtr(false)})
	m := projectManifest("gh/acme", p)

	rep, err := sy.SyncProjects(context.Background(), m, nil, nil, Options{Apply: true, GitHubToken: "gh-tok"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !fp.hasCalled("CreatePipelineDefinition") {
		t.Fatal("CreatePipelineDefinition must be called for an OAuth-source project")
	}
	if gotDefSpec.Name != "build-and-test" {
		t.Errorf("def Name: got %q want build-and-test", gotDefSpec.Name)
	}
	if gotDefSpec.ConfigFilePath != ".circleci/config.yml" {
		t.Errorf("def ConfigFilePath: got %q want .circleci/config.yml", gotDefSpec.ConfigFilePath)
	}
	if gotDefSpec.ConfigProvider != "github_app" || gotDefSpec.CheckoutProvider != "github_app" {
		t.Errorf("providers: got config=%q checkout=%q want github_app", gotDefSpec.ConfigProvider, gotDefSpec.CheckoutProvider)
	}
	if gotDefSpec.ConfigExternalID != "resolved-ext-id" || gotDefSpec.CheckoutExternalID != "resolved-ext-id" {
		t.Errorf("ext ids: got config=%q checkout=%q want resolved-ext-id", gotDefSpec.ConfigExternalID, gotDefSpec.CheckoutExternalID)
	}

	if !fp.hasCalled("CreateTrigger") {
		t.Fatal("CreateTrigger must be called for an OAuth-source project")
	}
	if !gotTrigSpec.Disabled {
		t.Error("synthesized trigger must be created disabled")
	}
	if gotTrigSpec.EventPreset != "all-pushes" {
		t.Errorf("trigger EventPreset: got %q want all-pushes", gotTrigSpec.EventPreset)
	}

	// The disabled trigger must be queued for the enable-builds step.
	if len(rep.PendingEnable) != 1 || rep.PendingEnable[0].Kind != "trigger" {
		t.Fatalf("PendingEnable: got %+v want one trigger target", rep.PendingEnable)
	}
}

// TestSyncProjects_AppDest_OAuthSource_PRsOnlyPreset verifies the prs-only
// translation reaches the created trigger.
func TestSyncProjects_AppDest_OAuthSource_PRsOnlyPreset(t *testing.T) {
	stubResolveRepoID(t, "acme/web", "resolved-ext-id", nil)

	var gotTrigSpec project.TriggerSpec
	fp := &fakeProjectWriter{
		createTrigger: func(projectID, defID string, spec project.TriggerSpec) (string, error) {
			gotTrigSpec = spec
			return "trig-id", nil
		},
	}
	sy := newSyncerAppProjects(fp)

	p := oauthSourceProject("web", "gh/acme/web", &manifest.AdvancedSettings{BuildPRsOnly: boolPtr(true)})
	m := projectManifest("gh/acme", p)

	if _, err := sy.SyncProjects(context.Background(), m, nil, nil, Options{Apply: true, GitHubToken: "gh-tok"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotTrigSpec.EventPreset != "only-build-prs" {
		t.Errorf("trigger EventPreset: got %q want only-build-prs", gotTrigSpec.EventPreset)
	}
}

// TestSyncProjects_AppDest_OAuthSource_DataLossWarnings verifies that fork, OSS,
// and pr_only_branch_override flags each emit a "manual" data-loss warning.
func TestSyncProjects_AppDest_OAuthSource_DataLossWarnings(t *testing.T) {
	stubResolveRepoID(t, "acme/web", "resolved-ext-id", nil)
	fp := &fakeProjectWriter{}
	sy := newSyncerAppProjects(fp)

	p := oauthSourceProject("web", "gh/acme/web", &manifest.AdvancedSettings{
		BuildForkPRs:          boolPtr(true),
		OSS:                   boolPtr(true),
		PROnlyBranchOverrides: []string{"main"},
	})
	m := projectManifest("gh/acme", p)

	rep, err := sy.SyncProjects(context.Background(), m, nil, nil, Options{Apply: true, GitHubToken: "gh-tok"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wants := []string{
		"never build forked PRs",
		"OSS/Free-and-Open-Source flag is OAuth-only",
		"pr_only_branch_overrides has no GitHub App equivalent",
	}
	for _, want := range wants {
		found := false
		for _, a := range rep.Actions {
			if a.Status == "manual" && containsStr(a.Detail, want) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected a manual data-loss action mentioning %q", want)
		}
	}
}

// TestSyncProjects_AppDest_OAuthSource_ForksReceiveSecrets verifies that the
// forks_receive_secret_env_vars flag alone triggers the fork-build warning.
func TestSyncProjects_AppDest_OAuthSource_ForksReceiveSecrets(t *testing.T) {
	stubResolveRepoID(t, "acme/web", "resolved-ext-id", nil)
	fp := &fakeProjectWriter{}
	sy := newSyncerAppProjects(fp)

	p := oauthSourceProject("web", "gh/acme/web", &manifest.AdvancedSettings{
		ForksReceiveSecretEnvVars: boolPtr(true),
	})
	m := projectManifest("gh/acme", p)

	rep, err := sy.SyncProjects(context.Background(), m, nil, nil, Options{Apply: true, GitHubToken: "gh-tok"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	found := false
	for _, a := range rep.Actions {
		if a.Status == "manual" && containsStr(a.Detail, "never build forked PRs") {
			found = true
		}
	}
	if !found {
		t.Error("expected a fork-build warning when forks_receive_secret_env_vars is set")
	}
}

// TestSyncProjects_AppDest_OAuthSource_RepoNotFound_ManualSkip verifies that a
// 404 from repo resolution records a manual action and skips creation.
func TestSyncProjects_AppDest_OAuthSource_RepoNotFound_ManualSkip(t *testing.T) {
	stubResolveRepoID(t, "acme/web", "", github.ErrRepoNotFound)
	fp := &fakeProjectWriter{}
	sy := newSyncerAppProjects(fp)

	p := oauthSourceProject("web", "gh/acme/web", nil)
	m := projectManifest("gh/acme", p)

	rep, err := sy.SyncProjects(context.Background(), m, nil, nil, Options{Apply: true, GitHubToken: "gh-tok"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fp.hasCalled("CreatePipelineDefinition") {
		t.Error("CreatePipelineDefinition must NOT be called when the repo is not found")
	}
	if fp.hasCalled("CreateTrigger") {
		t.Error("CreateTrigger must NOT be called when the repo is not found")
	}
	found := false
	for _, a := range rep.Actions {
		if a.Status == "manual" && containsStr(a.Detail, "not found in the destination GitHub org") {
			found = true
		}
	}
	if !found {
		t.Error("expected a manual 'repo not found' action")
	}
}

// TestSyncProjects_AppDest_OAuthSource_DryRun_Plans verifies that in dry-run
// mode the synthesis is planned but no writes occur.
func TestSyncProjects_AppDest_OAuthSource_DryRun_Plans(t *testing.T) {
	stubResolveRepoID(t, "acme/web", "resolved-ext-id", nil)
	fp := &fakeProjectWriter{}
	sy := newSyncerAppProjects(fp)

	p := oauthSourceProject("web", "gh/acme/web", nil)
	m := projectManifest("gh/acme", p)

	rep, err := sy.SyncProjects(context.Background(), m, nil, nil, Options{Apply: false, GitHubToken: "gh-tok"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fp.hasCalled("CreateAppProject") {
		t.Error("CreateAppProject must NOT be called in dry-run")
	}
	if fp.hasCalled("CreatePipelineDefinition") {
		t.Error("CreatePipelineDefinition must NOT be called in dry-run")
	}
	if fp.hasCalled("CreateTrigger") {
		t.Error("CreateTrigger must NOT be called in dry-run")
	}

	defPlanned, trigPlanned := false, false
	for _, a := range rep.Actions {
		if a.Kind == "project-pipeline-def" && a.Status == "created" && containsStr(a.Detail, "would synthesize") {
			defPlanned = true
		}
		if a.Kind == "project-trigger" && a.Status == "created" && containsStr(a.Detail, "would synthesize") {
			trigPlanned = true
		}
	}
	if !defPlanned {
		t.Error("expected a planned (would synthesize) pipeline-def action in dry-run")
	}
	if !trigPlanned {
		t.Error("expected a planned (would synthesize) trigger action in dry-run")
	}
}

// TestSyncProjects_AppDest_AppSource_NotSynthesized verifies that a project that
// HAS pipeline definitions (App source) follows the existing recreate path and
// does NOT synthesize a build-and-test definition.
func TestSyncProjects_AppDest_AppSource_NotSynthesized(t *testing.T) {
	var defNames []string
	fp := &fakeProjectWriter{
		createPipelineDefinition: func(projectID string, spec project.PipelineDefinitionSpec) (string, error) {
			defNames = append(defNames, spec.Name)
			return "def-id-" + spec.Name, nil
		},
	}
	sy := newSyncerAppProjects(fp)

	p := appManifestProject("web", "gh/acme/web", "captured-ext-id", "code_push")
	m := projectManifest("gh/acme", p)

	if _, err := sy.SyncProjects(context.Background(), m, nil, nil, Options{Apply: true}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(defNames) != 1 || defNames[0] != "default" {
		t.Fatalf("App-source project should recreate its captured definition only, got %v", defNames)
	}
	for _, n := range defNames {
		if n == "build-and-test" {
			t.Error("App-source project must NOT synthesize a build-and-test definition")
		}
	}
}

// TestSyncProjects_AppDest_OAuthSource_NonGitHubSlug_Manual verifies that a
// non-GitHub OAuth slug with no pipeline defs yields a manual action and no
// creation (cannot derive a GitHub repo).
func TestSyncProjects_AppDest_OAuthSource_NonGitHubSlug_Manual(t *testing.T) {
	fp := &fakeProjectWriter{}
	sy := newSyncerAppProjects(fp)

	// bb/ slug routed via explicit mapping into the App dest org.
	p := manifest.Project{Slug: "bb/acme/web", Name: "web"}
	m := projectManifest("bb/acme", p)
	mapping := &manifest.Mapping{
		Org: manifest.OrgMapping{From: "bb/acme", To: "circleci/dest-org-id"},
	}

	rep, err := sy.SyncProjects(context.Background(), m, nil, mapping, Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fp.hasCalled("CreatePipelineDefinition") {
		t.Error("CreatePipelineDefinition must NOT be called for a non-GitHub slug")
	}
	found := false
	for _, a := range rep.Actions {
		if a.Status == "manual" && containsStr(a.Detail, "cannot derive a GitHub repo") {
			found = true
		}
	}
	if !found {
		t.Error("expected a manual 'cannot derive a GitHub repo' action")
	}
}

// TestSyncProjects_AppDest_OAuthSource_PipelineDefRepoAccessError_Manual
// verifies that a repo-access error from CreatePipelineDefinition is recorded
// as a "manual" action (operator must connect the repo to the App), and the
// trigger is not created.
func TestSyncProjects_AppDest_OAuthSource_PipelineDefRepoAccessError_Manual(t *testing.T) {
	stubResolveRepoID(t, "acme/web", "resolved-ext-id", nil)
	fp := &fakeProjectWriter{
		createPipelineDefinition: func(projectID string, spec project.PipelineDefinitionSpec) (string, error) {
			return "", errors.New("Installation does not have access to repository")
		},
	}
	sy := newSyncerAppProjects(fp)

	p := oauthSourceProject("web", "gh/acme/web", nil)
	m := projectManifest("gh/acme", p)

	rep, err := sy.SyncProjects(context.Background(), m, nil, nil, Options{Apply: true, GitHubToken: "gh-tok"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fp.hasCalled("CreateTrigger") {
		t.Error("CreateTrigger must NOT be called when pipeline-def creation fails")
	}
	found := false
	for _, a := range rep.Actions {
		if a.Kind == "project-pipeline-def" && a.Status == "manual" && containsStr(a.Detail, "connected to the CircleCI GitHub App") {
			found = true
		}
	}
	if !found {
		t.Error("expected a manual repo-access action when CreatePipelineDefinition reports no access")
	}
}

// TestSyncProjects_AppDest_OAuthSource_PipelineDefOtherError verifies a non
// repo-access CreatePipelineDefinition failure is recorded as an "error" action.
func TestSyncProjects_AppDest_OAuthSource_PipelineDefOtherError(t *testing.T) {
	stubResolveRepoID(t, "acme/web", "resolved-ext-id", nil)
	fp := &fakeProjectWriter{
		createPipelineDefinition: func(projectID string, spec project.PipelineDefinitionSpec) (string, error) {
			return "", errors.New("internal server error")
		},
	}
	sy := newSyncerAppProjects(fp)

	p := oauthSourceProject("web", "gh/acme/web", nil)
	m := projectManifest("gh/acme", p)

	rep, err := sy.SyncProjects(context.Background(), m, nil, nil, Options{Apply: true, GitHubToken: "gh-tok"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	found := false
	for _, a := range rep.Actions {
		if a.Kind == "project-pipeline-def" && a.Status == "error" {
			found = true
		}
	}
	if !found {
		t.Error("expected an error pipeline-def action on a non-access failure")
	}
}

// TestSyncProjects_AppDest_OAuthSource_CreateTriggerError verifies that a
// CreateTrigger failure is recorded as an error action and does not panic.
func TestSyncProjects_AppDest_OAuthSource_CreateTriggerError(t *testing.T) {
	stubResolveRepoID(t, "acme/web", "resolved-ext-id", nil)
	fp := &fakeProjectWriter{
		createTrigger: func(projectID, defID string, spec project.TriggerSpec) (string, error) {
			return "", errors.New("trigger boom")
		},
	}
	sy := newSyncerAppProjects(fp)

	p := oauthSourceProject("web", "gh/acme/web", nil)
	m := projectManifest("gh/acme", p)

	rep, err := sy.SyncProjects(context.Background(), m, nil, nil, Options{Apply: true, GitHubToken: "gh-tok"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	found := false
	for _, a := range rep.Actions {
		if a.Kind == "project-trigger" && a.Status == "error" {
			found = true
		}
	}
	if !found {
		t.Error("expected an error trigger action when CreateTrigger fails")
	}
	if len(rep.PendingEnable) != 0 {
		t.Error("PendingEnable must be empty when trigger creation fails")
	}
}
