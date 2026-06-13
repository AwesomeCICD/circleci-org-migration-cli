package syncer

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/AwesomeCICD/circleci-org-migration-cli/api/org"
	"github.com/AwesomeCICD/circleci-org-migration-cli/api/project"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/github"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
)

// ---------------------------------------------------------------------------
// Fake ProjectWriter
// ---------------------------------------------------------------------------

// projectCall records one call to a ProjectWriter method.
type projectCall struct {
	method string
	args   []string
}

// fakeProjectWriter records all calls for later assertion.
type fakeProjectWriter struct {
	getProject                func(slug string) (*project.Project, error)
	createProjectShell        func(provider, org, name string) (*project.Project, error)
	followProject             func(vcsType, org, repo string) (*project.FollowResult, error)
	listEnvVars               func(slug string) ([]project.EnvVar, error)
	createEnvVar              func(slug, name, value string) error
	updateSettings            func(provider, org, proj string, s *project.AdvancedSettings) error
	listWebhooks              func(projectID string) ([]project.Webhook, error)
	createWebhook             func(destProjectID string, w project.Webhook) error
	listSchedules             func(slug string) ([]project.Schedule, error)
	createSchedule            func(destSlug, name, description, attributionActor string, timetable, parameters map[string]any) error
	getProjectOIDCClaims      func(orgID, projID string) ([]string, string, error)
	setProjectOIDCClaims      func(orgID, projID string, audience []string, ttl string) error
	getV11ProjectFeatureFlags func(slug string) (map[string]bool, error)
	setV11ProjectFeatureFlags func(slug string, flags map[string]bool) error
	listAdditionalSSHKeys     func(slug string) ([]project.SSHKeyMeta, error)
	addAdditionalSSHKey       func(slug, hostname, privateKey string) error
	listProjectTokens         func(slug string) ([]project.ProjectAPIToken, error)
	createProjectToken        func(slug, scope, label string) (string, error)

	// App-org methods
	createAppProject         func(orgID, name string) (*project.Project, error)
	createPipelineDefinition func(projectID string, spec project.PipelineDefinitionSpec) (string, error)
	createTrigger            func(projectID, defID string, spec project.TriggerSpec) (string, error)
	enableTrigger            func(projectID, triggerID string) error
	listOrgProjects          func(orgID string) ([]project.OrgProject, error)

	calls           []projectCall
	settingsUpdates []*project.AdvancedSettings // captures the settings arg each time UpdateSettings is called
}

func (f *fakeProjectWriter) GetProject(_ context.Context, slug string) (*project.Project, error) {
	f.calls = append(f.calls, projectCall{"GetProject", []string{slug}})
	if f.getProject != nil {
		return f.getProject(slug)
	}
	return &project.Project{Slug: slug, ID: "proj-id-" + slug, Name: slug}, nil
}

func (f *fakeProjectWriter) CreateProjectShell(_ context.Context, provider, org, name string) (*project.Project, error) {
	f.calls = append(f.calls, projectCall{"CreateProjectShell", []string{provider, org, name}})
	if f.createProjectShell != nil {
		return f.createProjectShell(provider, org, name)
	}
	slug := provider + "/" + org + "/" + name
	return &project.Project{Slug: slug, ID: "new-proj-id-" + name, Name: name}, nil
}

func (f *fakeProjectWriter) FollowProject(_ context.Context, vcsType, org, repo string) (*project.FollowResult, error) {
	f.calls = append(f.calls, projectCall{"FollowProject", []string{vcsType, org, repo}})
	if f.followProject != nil {
		return f.followProject(vcsType, org, repo)
	}
	return &project.FollowResult{Followed: true}, nil
}

func (f *fakeProjectWriter) ListEnvVars(_ context.Context, slug string) ([]project.EnvVar, error) {
	f.calls = append(f.calls, projectCall{"ListEnvVars", []string{slug}})
	if f.listEnvVars != nil {
		return f.listEnvVars(slug)
	}
	return nil, nil
}

func (f *fakeProjectWriter) CreateEnvVar(_ context.Context, slug, name, value string) error {
	f.calls = append(f.calls, projectCall{"CreateEnvVar", []string{slug, name, value}})
	if f.createEnvVar != nil {
		return f.createEnvVar(slug, name, value)
	}
	return nil
}

func (f *fakeProjectWriter) UpdateSettings(_ context.Context, provider, org, proj string, s *project.AdvancedSettings) error {
	f.calls = append(f.calls, projectCall{"UpdateSettings", []string{provider, org, proj}})
	f.settingsUpdates = append(f.settingsUpdates, s)
	if f.updateSettings != nil {
		return f.updateSettings(provider, org, proj, s)
	}
	return nil
}

func (f *fakeProjectWriter) ListWebhooks(_ context.Context, projectID string) ([]project.Webhook, error) {
	f.calls = append(f.calls, projectCall{"ListWebhooks", []string{projectID}})
	if f.listWebhooks != nil {
		return f.listWebhooks(projectID)
	}
	return nil, nil
}

func (f *fakeProjectWriter) CreateWebhook(_ context.Context, destProjectID string, w project.Webhook) error {
	f.calls = append(f.calls, projectCall{"CreateWebhook", []string{destProjectID, w.Name, w.URL}})
	if f.createWebhook != nil {
		return f.createWebhook(destProjectID, w)
	}
	return nil
}

func (f *fakeProjectWriter) ListSchedules(_ context.Context, slug string) ([]project.Schedule, error) {
	f.calls = append(f.calls, projectCall{"ListSchedules", []string{slug}})
	if f.listSchedules != nil {
		return f.listSchedules(slug)
	}
	return nil, nil
}

func (f *fakeProjectWriter) CreateSchedule(_ context.Context, destSlug, name, description, attributionActor string, timetable, parameters map[string]any) error {
	f.calls = append(f.calls, projectCall{"CreateSchedule", []string{destSlug, name, description, attributionActor}})
	if f.createSchedule != nil {
		return f.createSchedule(destSlug, name, description, attributionActor, timetable, parameters)
	}
	return nil
}

func (f *fakeProjectWriter) GetProjectOIDCClaims(_ context.Context, orgID, projID string) ([]string, string, error) {
	f.calls = append(f.calls, projectCall{"GetProjectOIDCClaims", []string{orgID, projID}})
	if f.getProjectOIDCClaims != nil {
		return f.getProjectOIDCClaims(orgID, projID)
	}
	return nil, "", nil
}

func (f *fakeProjectWriter) SetProjectOIDCClaims(_ context.Context, orgID, projID string, audience []string, ttl string) error {
	f.calls = append(f.calls, projectCall{"SetProjectOIDCClaims", []string{orgID, projID, ttl}})
	if f.setProjectOIDCClaims != nil {
		return f.setProjectOIDCClaims(orgID, projID, audience, ttl)
	}
	return nil
}

func (f *fakeProjectWriter) GetV11ProjectFeatureFlags(_ context.Context, slug string) (map[string]bool, error) {
	f.calls = append(f.calls, projectCall{"GetV11ProjectFeatureFlags", []string{slug}})
	if f.getV11ProjectFeatureFlags != nil {
		return f.getV11ProjectFeatureFlags(slug)
	}
	return nil, nil
}

func (f *fakeProjectWriter) SetV11ProjectFeatureFlags(_ context.Context, slug string, flags map[string]bool) error {
	keys := make([]string, 0, len(flags))
	for k := range flags {
		keys = append(keys, k)
	}
	f.calls = append(f.calls, projectCall{"SetV11ProjectFeatureFlags", append([]string{slug}, keys...)})
	if f.setV11ProjectFeatureFlags != nil {
		return f.setV11ProjectFeatureFlags(slug, flags)
	}
	return nil
}

func (f *fakeProjectWriter) ListAdditionalSSHKeys(_ context.Context, slug string) ([]project.SSHKeyMeta, error) {
	f.calls = append(f.calls, projectCall{"ListAdditionalSSHKeys", []string{slug}})
	if f.listAdditionalSSHKeys != nil {
		return f.listAdditionalSSHKeys(slug)
	}
	return nil, nil
}

func (f *fakeProjectWriter) AddAdditionalSSHKey(_ context.Context, slug, hostname, privateKey string) error {
	f.calls = append(f.calls, projectCall{"AddAdditionalSSHKey", []string{slug, hostname, privateKey}})
	if f.addAdditionalSSHKey != nil {
		return f.addAdditionalSSHKey(slug, hostname, privateKey)
	}
	return nil
}

func (f *fakeProjectWriter) ListProjectTokens(_ context.Context, slug string) ([]project.ProjectAPIToken, error) {
	f.calls = append(f.calls, projectCall{"ListProjectTokens", []string{slug}})
	if f.listProjectTokens != nil {
		return f.listProjectTokens(slug)
	}
	return nil, nil
}

func (f *fakeProjectWriter) CreateProjectToken(_ context.Context, slug, scope, label string) (string, error) {
	f.calls = append(f.calls, projectCall{"CreateProjectToken", []string{slug, scope, label}})
	if f.createProjectToken != nil {
		return f.createProjectToken(slug, scope, label)
	}
	return "ccipat_PLACEHOLDER_syncer_test_value", nil
}

func (f *fakeProjectWriter) CreateAppProject(_ context.Context, orgID, name string) (*project.Project, error) {
	f.calls = append(f.calls, projectCall{"CreateAppProject", []string{orgID, name}})
	if f.createAppProject != nil {
		return f.createAppProject(orgID, name)
	}
	slug := "circleci/" + orgID + "/new-proj-" + name
	return &project.Project{Slug: slug, ID: "app-proj-id-" + name, Name: name}, nil
}

func (f *fakeProjectWriter) CreatePipelineDefinition(_ context.Context, projectID string, spec project.PipelineDefinitionSpec) (string, error) {
	f.calls = append(f.calls, projectCall{"CreatePipelineDefinition", []string{projectID, spec.Name}})
	if f.createPipelineDefinition != nil {
		return f.createPipelineDefinition(projectID, spec)
	}
	return "def-id-" + spec.Name, nil
}

func (f *fakeProjectWriter) CreateTrigger(_ context.Context, projectID, defID string, spec project.TriggerSpec) (string, error) {
	f.calls = append(f.calls, projectCall{"CreateTrigger", []string{projectID, defID, spec.EventPreset}})
	if f.createTrigger != nil {
		return f.createTrigger(projectID, defID, spec)
	}
	return "trigger-id-" + spec.EventPreset, nil
}

func (f *fakeProjectWriter) EnableTrigger(_ context.Context, projectID, triggerID string) error {
	f.calls = append(f.calls, projectCall{"EnableTrigger", []string{projectID, triggerID}})
	if f.enableTrigger != nil {
		return f.enableTrigger(projectID, triggerID)
	}
	return nil
}

func (f *fakeProjectWriter) ListOrgProjects(_ context.Context, orgID string) ([]project.OrgProject, error) {
	f.calls = append(f.calls, projectCall{"ListOrgProjects", []string{orgID}})
	if f.listOrgProjects != nil {
		return f.listOrgProjects(orgID)
	}
	return nil, nil
}

// hasCalled reports whether any call with the given method name was recorded.
func (f *fakeProjectWriter) hasCalled(method string) bool {
	for _, c := range f.calls {
		if c.method == method {
			return true
		}
	}
	return false
}

// callsTo returns all recorded calls to the named method.
func (f *fakeProjectWriter) callsTo(method string) []projectCall {
	var out []projectCall
	for _, c := range f.calls {
		if c.method == method {
			out = append(out, c)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// projectManifest builds a manifest with one or more projects.
// Each call to projectManifest accepts a source org slug and one or more
// manifest.Project values.
func projectManifest(srcOrgSlug string, projects ...manifest.Project) *manifest.Manifest {
	return &manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		Source: manifest.Source{
			Org: manifest.Org{Slug: srcOrgSlug},
		},
		Projects: projects,
	}
}

// simpleProject builds a minimal manifest.Project with the given slug and env var names.
func simpleProject(slug string, varNames ...string) manifest.Project {
	var vars []manifest.ProjectEnvVar
	for _, n := range varNames {
		vars = append(vars, manifest.ProjectEnvVar{Name: n})
	}
	return manifest.Project{Slug: slug, Name: slug, EnvVars: vars}
}

// projectBundleWith builds a SecretBundle with project secrets for the given slug.
func projectBundleWith(slug string, kvPairs ...string) *manifest.SecretBundle {
	b := manifest.NewSecretBundle()
	for i := 0; i+1 < len(kvPairs); i += 2 {
		b.SetProjectSecret(slug, kvPairs[i], kvPairs[i+1])
	}
	return b
}

// actionsOfKind filters report actions by kind.
func actionsOfKind(rep *Report, kind string) []Action {
	var out []Action
	for _, a := range rep.Actions {
		if a.Kind == kind {
			out = append(out, a)
		}
	}
	return out
}

// firstActionOfKind returns the first action with the given kind, or nil.
func firstActionOfKind(rep *Report, kind string) *Action {
	for i := range rep.Actions {
		if rep.Actions[i].Kind == kind {
			return &rep.Actions[i]
		}
	}
	return nil
}

// newSyncerProjects builds a Syncer with a stubbed Projects writer.
// The default OrgResolver returns vcs_type "github" (OAuth) so existing tests
// are unaffected.
func newSyncerProjects(fp *fakeProjectWriter) *Syncer {
	return &Syncer{
		Org:      &fakeOrgResolver{},
		Projects: fp,
	}
}

// newSyncerAppProjects builds a Syncer whose OrgResolver reports the dest org
// as a "circleci"-type (GitHub App) org.
func newSyncerAppProjects(fp *fakeProjectWriter) *Syncer {
	return &Syncer{
		Org: &fakeOrgResolver{
			getOrganization: func(slug string) (*org.Organization, error) {
				return &org.Organization{ID: "dest-org-id", Slug: slug, VCSType: "circleci"}, nil
			},
		},
		Projects: fp,
	}
}

// ---------------------------------------------------------------------------
// SyncProjects: project missing in destination
// ---------------------------------------------------------------------------

// TestSyncProjects_ProjectMissingInDest_OAuth verifies that when GetProject
// returns an error for an OAuth (gh/) slug, the project shell is created
// (apply=true), a "created" action is recorded, and settings/vars are still
// applied.  PendingEnable is also populated.
func TestSyncProjects_ProjectMissingInDest_OAuth(t *testing.T) {
	getCount := 0
	fp := &fakeProjectWriter{
		getProject: func(slug string) (*project.Project, error) {
			getCount++
			// First call (initial lookup) fails; second call (re-fetch after create) succeeds.
			if getCount == 1 {
				return nil, errors.New("project not found")
			}
			return &project.Project{Slug: slug, ID: "created-proj-id", Name: "web"}, nil
		},
	}
	sy := newSyncerProjects(fp)

	trueVal := true
	p := manifest.Project{
		Slug:    "gh/acme/web",
		Name:    "web",
		EnvVars: []manifest.ProjectEnvVar{{Name: "DB_URL"}},
		Settings: &manifest.AdvancedSettings{
			SetGitHubStatus: &trueVal,
		},
	}
	m := projectManifest("gh/acme", p)
	bundle := projectBundleWith("gh/acme/web", "DB_URL", "postgres://localhost")

	rep, err := sy.SyncProjects(context.Background(), m, bundle, nil, Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Project action must be "created".
	a := firstActionOfKind(rep, "project")
	if a == nil {
		t.Fatal("expected a project action, got none")
		return
	}
	if a.Status != "created" {
		t.Errorf("status: got %q want %q", a.Status, "created")
	}

	// CreateProjectShell must have been called.
	if !fp.hasCalled("CreateProjectShell") {
		t.Error("CreateProjectShell must be called for a missing OAuth project when Apply=true")
	}

	// After creation the settings and vars helpers should still run.
	if !fp.hasCalled("UpdateSettings") {
		t.Error("UpdateSettings must be called even for a freshly created project")
	}
	if !fp.hasCalled("CreateEnvVar") {
		t.Error("CreateEnvVar must be called for a freshly created project when Apply=true and value exists")
	}

	// PendingEnable must contain one entry for the created project.
	if len(rep.PendingEnable) != 1 {
		t.Fatalf("PendingEnable: got %d entries, want 1", len(rep.PendingEnable))
	}
	if rep.PendingEnable[0].Slug != "gh/acme/web" {
		t.Errorf("PendingEnable[0].Slug: got %q, want %q", rep.PendingEnable[0].Slug, "gh/acme/web")
	}
}

// TestSyncProjects_ProjectMissingInDest_OAuth_DryRun verifies that in dry-run
// mode the project is not created but a "created" plan action is recorded and
// PendingEnable is populated.
func TestSyncProjects_ProjectMissingInDest_OAuth_DryRun(t *testing.T) {
	fp := &fakeProjectWriter{
		getProject: func(slug string) (*project.Project, error) {
			return nil, errors.New("project not found")
		},
	}
	sy := newSyncerProjects(fp)

	p := simpleProject("gh/acme/web", "DB_URL")
	m := projectManifest("gh/acme", p)

	rep, err := sy.SyncProjects(context.Background(), m, nil, nil, Options{Apply: false})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	a := firstActionOfKind(rep, "project")
	if a == nil {
		t.Fatal("expected a project action, got none")
		return
	}
	if a.Status != "created" {
		t.Errorf("status: got %q want %q", a.Status, "created")
	}
	if fp.hasCalled("CreateProjectShell") {
		t.Error("CreateProjectShell must NOT be called in dry-run mode")
	}

	// PendingEnable should be populated even in dry-run.
	if len(rep.PendingEnable) != 1 {
		t.Fatalf("PendingEnable: got %d entries, want 1", len(rep.PendingEnable))
	}
}

// TestSyncProjects_ProjectMissingInDest_App_Manual verifies that when GetProject
// returns an error for a circleci/ slug, a "manual" action is recorded
// (App creation is a future milestone) and CreateProjectShell is not called.
func TestSyncProjects_ProjectMissingInDest_App_Manual(t *testing.T) {
	appDstSlug := "circleci/org-id-abc/proj-id-def"
	fp := &fakeProjectWriter{
		getProject: func(slug string) (*project.Project, error) {
			return nil, errors.New("project not found")
		},
	}
	sy := newSyncerProjects(fp)

	p := simpleProject("gh/acme/web", "DB_URL")
	m := projectManifest("gh/acme", p)
	mapping := &manifest.Mapping{
		Org:      manifest.OrgMapping{From: "gh/acme", To: "circleci/org-id-abc"},
		Projects: map[string]string{"gh/acme/web": appDstSlug},
	}

	rep, err := sy.SyncProjects(context.Background(), m, nil, mapping, Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	a := firstActionOfKind(rep, "project")
	if a == nil {
		t.Fatal("expected a project action, got none")
		return
	}
	if a.Status != "manual" {
		t.Errorf("status: got %q want %q", a.Status, "manual")
	}
	if fp.hasCalled("CreateProjectShell") {
		t.Error("CreateProjectShell must NOT be called for App-org (circleci/) slugs")
	}
	if len(rep.PendingEnable) != 0 {
		t.Errorf("PendingEnable must be empty for App-org slugs, got %d", len(rep.PendingEnable))
	}
}

// ---------------------------------------------------------------------------
// SyncProjects: mapping unresolved (circleci/ dest, no explicit project entry)
// ---------------------------------------------------------------------------

// TestSyncProjects_MappingUnresolved_Manual verifies that when the destination
// org is "circleci/<org-id>" and there is no explicit project entry in the
// mapping, ResolveProjectSlug returns ok=false, a "manual" action is recorded,
// and GetProject is never called.
func TestSyncProjects_MappingUnresolved_Manual(t *testing.T) {
	fp := &fakeProjectWriter{}
	sy := newSyncerProjects(fp)

	p := simpleProject("gh/acme/web")
	m := projectManifest("gh/acme", p)

	// Org.To is a circleci/ slug → no slug derivation possible without an
	// explicit project entry.
	mapping := &manifest.Mapping{
		Org: manifest.OrgMapping{From: "gh/acme", To: "circleci/org-id-abc"},
	}

	rep, err := sy.SyncProjects(context.Background(), m, nil, mapping, Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	a := firstActionOfKind(rep, "project")
	if a == nil {
		t.Fatal("expected a project action, got none")
		return
	}
	if a.Status != "manual" {
		t.Errorf("status: got %q want %q", a.Status, "manual")
	}
	if fp.hasCalled("GetProject") {
		t.Error("GetProject must NOT be called when project slug cannot be resolved")
	}
}

// ---------------------------------------------------------------------------
// SyncProjects: settings Apply=true
// ---------------------------------------------------------------------------

// TestSyncProjects_Settings_ApplyTrue_UpdateSettingsCalled verifies that when
// a project exists and settings are present, Apply=true causes UpdateSettings
// to be called with the correctly mapped AdvancedSettings.
func TestSyncProjects_Settings_ApplyTrue_UpdateSettingsCalled(t *testing.T) {
	fp := &fakeProjectWriter{}
	sy := newSyncerProjects(fp)

	trueVal := true
	p := manifest.Project{
		Slug:    "gh/acme/web",
		Name:    "web",
		EnvVars: nil,
		Settings: &manifest.AdvancedSettings{
			SetGitHubStatus: &trueVal,
			OSS:             &trueVal,
		},
	}
	m := projectManifest("gh/acme", p)

	rep, err := sy.SyncProjects(context.Background(), m, nil, nil, Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// UpdateSettings must have been called.
	if !fp.hasCalled("UpdateSettings") {
		t.Fatal("UpdateSettings must be called when Apply=true and settings are present")
	}

	// Check the action.
	a := firstActionOfKind(rep, "project-settings")
	if a == nil {
		t.Fatal("expected a project-settings action, got none")
		return
	}
	if a.Status != "set" {
		t.Errorf("status: got %q want %q", a.Status, "set")
	}

	// Verify the mapped settings were passed correctly.
	if len(fp.settingsUpdates) == 0 {
		t.Fatal("no settings updates recorded")
	}
	got := fp.settingsUpdates[0]
	if got.SetGithubStatus == nil || !*got.SetGithubStatus {
		t.Error("SetGithubStatus: expected true (mapped from manifest SetGitHubStatus)")
	}
	if got.OSS == nil || !*got.OSS {
		t.Error("OSS: expected true (preserved from manifest)")
	}
}

// TestSyncProjects_Settings_DryRun_UpdateSettingsNotCalled verifies that when
// Apply=false, UpdateSettings is NOT called but a "set" action ("would update")
// is still recorded.
func TestSyncProjects_Settings_DryRun_UpdateSettingsNotCalled(t *testing.T) {
	fp := &fakeProjectWriter{}
	sy := newSyncerProjects(fp)

	trueVal := true
	p := manifest.Project{
		Slug:    "gh/acme/web",
		Name:    "web",
		EnvVars: nil,
		Settings: &manifest.AdvancedSettings{
			SetGitHubStatus: &trueVal,
		},
	}
	m := projectManifest("gh/acme", p)

	rep, err := sy.SyncProjects(context.Background(), m, nil, nil, Options{Apply: false})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fp.hasCalled("UpdateSettings") {
		t.Error("UpdateSettings must NOT be called in dry-run mode")
	}

	a := firstActionOfKind(rep, "project-settings")
	if a == nil {
		t.Fatal("expected a project-settings action, got none")
		return
	}
	if a.Status != "set" {
		t.Errorf("dry-run settings status: got %q want %q", a.Status, "set")
	}
}

// ---------------------------------------------------------------------------
// SyncProjects: env var from bundle, source vs dest slug mapping
// ---------------------------------------------------------------------------

// TestSyncProjects_EnvVar_ApplyTrue_BundleLookedUpBySourceSlug verifies that
// project secrets in the bundle are keyed by the SOURCE slug and written to the
// DEST slug via CreateEnvVar.
func TestSyncProjects_EnvVar_ApplyTrue_BundleLookedUpBySourceSlug(t *testing.T) {
	fp := &fakeProjectWriter{}
	sy := newSyncerProjects(fp)

	srcSlug := "gh/acme/web"
	dstSlug := "gh/acme-new/web"
	p := simpleProject(srcSlug, "API_KEY")
	m := projectManifest("gh/acme", p)

	// Bundle is keyed by the SOURCE slug.
	bundle := projectBundleWith(srcSlug, "API_KEY", "s3cr3t")

	// Mapping: acme → acme-new (explicit project entry to avoid slug resolution).
	mapping := &manifest.Mapping{
		Org:      manifest.OrgMapping{From: "gh/acme", To: "gh/acme-new"},
		Projects: map[string]string{srcSlug: dstSlug},
	}

	rep, err := sy.SyncProjects(context.Background(), m, bundle, mapping, Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	creates := fp.callsTo("CreateEnvVar")
	if len(creates) == 0 {
		t.Fatal("CreateEnvVar must be called when Apply=true and bundle value exists")
	}
	// First arg is the dest slug.
	if creates[0].args[0] != dstSlug {
		t.Errorf("CreateEnvVar slug: got %q want %q", creates[0].args[0], dstSlug)
	}
	if creates[0].args[1] != "API_KEY" {
		t.Errorf("CreateEnvVar name: got %q want %q", creates[0].args[1], "API_KEY")
	}
	if creates[0].args[2] != "s3cr3t" {
		t.Errorf("CreateEnvVar value: got %q want %q", creates[0].args[2], "s3cr3t")
	}

	a := firstActionOfKind(rep, "project-var")
	if a == nil {
		t.Fatal("expected a project-var action, got none")
		return
	}
	if a.Status != "set" {
		t.Errorf("status: got %q want %q", a.Status, "set")
	}
}

// TestSyncProjects_EnvVar_DryRun_CreateEnvVarNotCalled verifies that in dry-run
// mode, CreateEnvVar is not called even though a bundle value exists.
func TestSyncProjects_EnvVar_DryRun_CreateEnvVarNotCalled(t *testing.T) {
	fp := &fakeProjectWriter{}
	sy := newSyncerProjects(fp)

	srcSlug := "gh/acme/web"
	p := simpleProject(srcSlug, "API_KEY")
	m := projectManifest("gh/acme", p)
	bundle := projectBundleWith(srcSlug, "API_KEY", "s3cr3t")

	_, err := sy.SyncProjects(context.Background(), m, bundle, nil, Options{Apply: false})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fp.hasCalled("CreateEnvVar") {
		t.Error("CreateEnvVar must NOT be called in dry-run mode")
	}
}

// ---------------------------------------------------------------------------
// SyncProjects: env var already exists in destination
// ---------------------------------------------------------------------------

// TestSyncProjects_EnvVar_AlreadyExists verifies that when ListEnvVars returns
// a variable already present, an "exists" action is recorded and CreateEnvVar
// is not called.
func TestSyncProjects_EnvVar_AlreadyExists(t *testing.T) {
	fp := &fakeProjectWriter{
		listEnvVars: func(slug string) ([]project.EnvVar, error) {
			return []project.EnvVar{{Name: "DB_PASS", MaskedValue: "xxxx1234"}}, nil
		},
	}
	sy := newSyncerProjects(fp)

	srcSlug := "gh/acme/web"
	p := simpleProject(srcSlug, "DB_PASS")
	m := projectManifest("gh/acme", p)
	bundle := projectBundleWith(srcSlug, "DB_PASS", "hunter2")

	rep, err := sy.SyncProjects(context.Background(), m, bundle, nil, Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fp.hasCalled("CreateEnvVar") {
		t.Error("CreateEnvVar must NOT be called when variable already exists")
	}

	a := firstActionOfKind(rep, "project-var")
	if a == nil {
		t.Fatal("expected a project-var action, got none")
		return
	}
	if a.Status != "exists" {
		t.Errorf("status: got %q want %q", a.Status, "exists")
	}
}

// ---------------------------------------------------------------------------
// SyncProjects: missing value + MissingSkip
// ---------------------------------------------------------------------------

// TestSyncProjects_MissingValue_Skip verifies that a variable absent from the
// bundle with the MissingSkip policy produces a "manual" action and no write.
func TestSyncProjects_MissingValue_Skip(t *testing.T) {
	fp := &fakeProjectWriter{}
	sy := newSyncerProjects(fp)

	srcSlug := "gh/acme/web"
	p := simpleProject(srcSlug, "MISSING_VAR")
	m := projectManifest("gh/acme", p)
	bundle := projectBundleWith(srcSlug) // no value for MISSING_VAR

	rep, err := sy.SyncProjects(context.Background(), m, bundle, nil, Options{Apply: true, MissingSecrets: MissingSkip})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fp.hasCalled("CreateEnvVar") {
		t.Error("CreateEnvVar must NOT be called when MissingSecrets=skip")
	}

	a := firstActionOfKind(rep, "project-var")
	if a == nil {
		t.Fatal("expected a project-var action, got none")
		return
	}
	if a.Status != "manual" {
		t.Errorf("status: got %q want %q", a.Status, "manual")
	}
}

// ---------------------------------------------------------------------------
// SyncProjects: missing value + MissingPlaceholder
// ---------------------------------------------------------------------------

// TestSyncProjects_MissingValue_Placeholder_ApplyTrue verifies that a variable
// absent from the bundle with MissingPlaceholder policy and Apply=true causes
// CreateEnvVar to be called with the placeholder value.
func TestSyncProjects_MissingValue_Placeholder_ApplyTrue(t *testing.T) {
	fp := &fakeProjectWriter{}
	sy := newSyncerProjects(fp)

	srcSlug := "gh/acme/web"
	p := simpleProject(srcSlug, "MISSING_VAR")
	m := projectManifest("gh/acme", p)
	bundle := projectBundleWith(srcSlug) // no value for MISSING_VAR

	rep, err := sy.SyncProjects(context.Background(), m, bundle, nil, Options{Apply: true, MissingSecrets: MissingPlaceholder})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	creates := fp.callsTo("CreateEnvVar")
	if len(creates) == 0 {
		t.Fatal("CreateEnvVar must be called when MissingPlaceholder is set and Apply=true")
	}
	if creates[0].args[2] != DefaultPlaceholder {
		t.Errorf("CreateEnvVar value: got %q want %q", creates[0].args[2], DefaultPlaceholder)
	}

	a := firstActionOfKind(rep, "project-var")
	if a == nil {
		t.Fatal("expected a project-var action, got none")
		return
	}
	if a.Status != "set" {
		t.Errorf("status: got %q want %q", a.Status, "set")
	}
}

// TestSyncProjects_MissingValue_Placeholder_DryRun_NoCreate verifies that with
// MissingPlaceholder and Apply=false, CreateEnvVar is not called.
func TestSyncProjects_MissingValue_Placeholder_DryRun_NoCreate(t *testing.T) {
	fp := &fakeProjectWriter{}
	sy := newSyncerProjects(fp)

	srcSlug := "gh/acme/web"
	p := simpleProject(srcSlug, "MISSING_VAR")
	m := projectManifest("gh/acme", p)
	bundle := projectBundleWith(srcSlug)

	_, err := sy.SyncProjects(context.Background(), m, bundle, nil, Options{Apply: false, MissingSecrets: MissingPlaceholder})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fp.hasCalled("CreateEnvVar") {
		t.Error("CreateEnvVar must NOT be called in dry-run mode even with MissingPlaceholder")
	}
}

// ---------------------------------------------------------------------------
// SyncProjects: CreateEnvVar error → "error" action, no panic, no top-level error
// ---------------------------------------------------------------------------

// TestSyncProjects_CreateEnvVar_Error_IsErrorAction verifies that a
// CreateEnvVar failure is recorded as an "error" action without panicking or
// returning a top-level error.
func TestSyncProjects_CreateEnvVar_Error_IsErrorAction(t *testing.T) {
	fp := &fakeProjectWriter{
		createEnvVar: func(slug, name, value string) error {
			return errors.New("create env var API down")
		},
	}
	sy := newSyncerProjects(fp)

	srcSlug := "gh/acme/web"
	p := simpleProject(srcSlug, "MY_VAR")
	m := projectManifest("gh/acme", p)
	bundle := projectBundleWith(srcSlug, "MY_VAR", "val")

	rep, err := sy.SyncProjects(context.Background(), m, bundle, nil, Options{Apply: true})
	if err != nil {
		t.Fatalf("CreateEnvVar error must not propagate, got: %v", err)
	}

	hasError := false
	for _, a := range rep.Actions {
		if a.Status == "error" {
			hasError = true
		}
	}
	if !hasError {
		t.Error("expected an 'error' action when CreateEnvVar fails, got none")
	}
}

// ---------------------------------------------------------------------------
// SyncProjects: UpdateSettings error → "error" action, no panic, no top-level error
// ---------------------------------------------------------------------------

// TestSyncProjects_UpdateSettings_Error_IsErrorAction verifies that an
// UpdateSettings failure is recorded as an "error" action without panicking or
// returning a top-level error.
func TestSyncProjects_UpdateSettings_Error_IsErrorAction(t *testing.T) {
	fp := &fakeProjectWriter{
		updateSettings: func(provider, org, proj string, s *project.AdvancedSettings) error {
			return errors.New("update settings API down")
		},
	}
	sy := newSyncerProjects(fp)

	trueVal := true
	p := manifest.Project{
		Slug:    "gh/acme/web",
		Name:    "web",
		EnvVars: nil,
		Settings: &manifest.AdvancedSettings{
			AutocancelBuilds: &trueVal,
		},
	}
	m := projectManifest("gh/acme", p)

	rep, err := sy.SyncProjects(context.Background(), m, nil, nil, Options{Apply: true})
	if err != nil {
		t.Fatalf("UpdateSettings error must not propagate, got: %v", err)
	}

	hasError := false
	for _, a := range rep.Actions {
		if a.Status == "error" {
			hasError = true
		}
	}
	if !hasError {
		t.Error("expected an 'error' action when UpdateSettings fails, got none")
	}
}

// ---------------------------------------------------------------------------
// SyncProjects: Report.DestOrgSlug
// ---------------------------------------------------------------------------

// TestSyncProjects_Report_DestOrgSlug_FromMapping verifies that Report.DestOrgSlug
// equals mapping.Org.To when a mapping is provided.
func TestSyncProjects_Report_DestOrgSlug_FromMapping(t *testing.T) {
	fp := &fakeProjectWriter{}
	sy := newSyncerProjects(fp)

	m := projectManifest("gh/acme")
	mapping := &manifest.Mapping{Org: manifest.OrgMapping{From: "gh/acme", To: "gh/acme-new"}}

	rep, err := sy.SyncProjects(context.Background(), m, nil, mapping, Options{Apply: false})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if rep.DestOrgSlug != "gh/acme-new" {
		t.Errorf("DestOrgSlug: got %q want %q", rep.DestOrgSlug, "gh/acme-new")
	}
}

// TestSyncProjects_Report_DestOrgSlug_NilMapping verifies that when no mapping
// is provided, Report.DestOrgSlug falls back to the manifest source org slug.
func TestSyncProjects_Report_DestOrgSlug_NilMapping(t *testing.T) {
	fp := &fakeProjectWriter{}
	sy := newSyncerProjects(fp)

	m := projectManifest("gh/source-org")

	rep, err := sy.SyncProjects(context.Background(), m, nil, nil, Options{Apply: false})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if rep.DestOrgSlug != "gh/source-org" {
		t.Errorf("DestOrgSlug: got %q want %q", rep.DestOrgSlug, "gh/source-org")
	}
}

// ---------------------------------------------------------------------------
// SyncProjects: two projects (one with settings, one with vars)
// ---------------------------------------------------------------------------

// TestSyncProjects_TwoProjects verifies that when the manifest has two projects,
// both are processed independently and produce the correct actions.
func TestSyncProjects_TwoProjects(t *testing.T) {
	fp := &fakeProjectWriter{}
	sy := newSyncerProjects(fp)

	trueVal := true
	p1 := manifest.Project{
		Slug:    "gh/acme/api",
		Name:    "api",
		EnvVars: []manifest.ProjectEnvVar{{Name: "API_KEY"}},
		Settings: &manifest.AdvancedSettings{
			SetGitHubStatus: &trueVal,
		},
	}
	p2 := manifest.Project{
		Slug:    "gh/acme/web",
		Name:    "web",
		EnvVars: []manifest.ProjectEnvVar{{Name: "WEB_SECRET"}},
	}
	m := projectManifest("gh/acme", p1, p2)

	bundle := manifest.NewSecretBundle()
	bundle.SetProjectSecret("gh/acme/api", "API_KEY", "key-val")
	bundle.SetProjectSecret("gh/acme/web", "WEB_SECRET", "web-val")

	rep, err := sy.SyncProjects(context.Background(), m, bundle, nil, Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Both projects should have been processed.
	settingsActions := actionsOfKind(rep, "project-settings")
	if len(settingsActions) != 1 {
		t.Errorf("expected 1 project-settings action, got %d", len(settingsActions))
	}
	varActions := actionsOfKind(rep, "project-var")
	if len(varActions) != 2 {
		t.Errorf("expected 2 project-var actions, got %d", len(varActions))
	}

	// Settings action for p1.
	if len(settingsActions) > 0 && settingsActions[0].Status != "set" {
		t.Errorf("settings action status: got %q want %q", settingsActions[0].Status, "set")
	}
	// Both var actions should be "set".
	for _, a := range varActions {
		if a.Status != "set" {
			t.Errorf("var action status: got %q want %q", a.Status, "set")
		}
	}
	// UpdateSettings called once for p1.
	if len(fp.callsTo("UpdateSettings")) != 1 {
		t.Errorf("expected 1 UpdateSettings call, got %d", len(fp.callsTo("UpdateSettings")))
	}
	// CreateEnvVar called twice (once per project).
	if len(fp.callsTo("CreateEnvVar")) != 2 {
		t.Errorf("expected 2 CreateEnvVar calls, got %d", len(fp.callsTo("CreateEnvVar")))
	}
}

// ---------------------------------------------------------------------------
// SyncProjects: no settings on project (nil Settings)
// ---------------------------------------------------------------------------

// TestSyncProjects_NoSettings_UpdateSettingsNotCalled verifies that when the
// manifest project has no Settings, UpdateSettings is never called.
func TestSyncProjects_NoSettings_UpdateSettingsNotCalled(t *testing.T) {
	fp := &fakeProjectWriter{}
	sy := newSyncerProjects(fp)

	p := simpleProject("gh/acme/web") // no settings
	m := projectManifest("gh/acme", p)

	_, err := sy.SyncProjects(context.Background(), m, nil, nil, Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fp.hasCalled("UpdateSettings") {
		t.Error("UpdateSettings must NOT be called when project has no settings")
	}
}

// ---------------------------------------------------------------------------
// SyncProjects: explicit project entry in mapping, different dest slug
// ---------------------------------------------------------------------------

// TestSyncProjects_ExplicitMapping_DestSlugUsed verifies that when there is an
// explicit project entry in the mapping, the dest slug from the mapping is
// passed to GetProject and CreateEnvVar, not the source slug.
func TestSyncProjects_ExplicitMapping_DestSlugUsed(t *testing.T) {
	fp := &fakeProjectWriter{}
	sy := newSyncerProjects(fp)

	srcSlug := "gh/acme/web"
	dstSlug := "gh/acme-new/webapp"
	p := simpleProject(srcSlug, "TOKEN")
	m := projectManifest("gh/acme", p)

	bundle := projectBundleWith(srcSlug, "TOKEN", "tok123")
	mapping := &manifest.Mapping{
		Org:      manifest.OrgMapping{From: "gh/acme", To: "gh/acme-new"},
		Projects: map[string]string{srcSlug: dstSlug},
	}

	_, err := sy.SyncProjects(context.Background(), m, bundle, mapping, Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	gets := fp.callsTo("GetProject")
	if len(gets) == 0 {
		t.Fatal("expected GetProject to be called")
	}
	if gets[0].args[0] != dstSlug {
		t.Errorf("GetProject slug: got %q want %q", gets[0].args[0], dstSlug)
	}

	creates := fp.callsTo("CreateEnvVar")
	if len(creates) == 0 {
		t.Fatal("expected CreateEnvVar to be called")
	}
	if creates[0].args[0] != dstSlug {
		t.Errorf("CreateEnvVar slug: got %q want %q", creates[0].args[0], dstSlug)
	}
	// Value still from SOURCE slug bundle.
	if creates[0].args[2] != "tok123" {
		t.Errorf("CreateEnvVar value: got %q want %q", creates[0].args[2], "tok123")
	}
}

// ---------------------------------------------------------------------------
// SyncProjects: nil bundle treats all vars as missing
// ---------------------------------------------------------------------------

// TestSyncProjects_NilBundle_AllVarsManual verifies that when no bundle is
// provided, all project env vars are treated as missing (MissingSkip policy).
func TestSyncProjects_NilBundle_AllVarsManual(t *testing.T) {
	fp := &fakeProjectWriter{}
	sy := newSyncerProjects(fp)

	p := simpleProject("gh/acme/web", "VAR1", "VAR2")
	m := projectManifest("gh/acme", p)

	rep, err := sy.SyncProjects(context.Background(), m, nil, nil, Options{Apply: true, MissingSecrets: MissingSkip})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fp.hasCalled("CreateEnvVar") {
		t.Error("CreateEnvVar must NOT be called with nil bundle + MissingSkip")
	}

	manualCount := 0
	for _, a := range rep.Actions {
		if a.Kind == "project-var" && a.Status == "manual" {
			manualCount++
		}
	}
	if manualCount != 2 {
		t.Errorf("expected 2 manual project-var actions, got %d", manualCount)
	}
}

// ---------------------------------------------------------------------------
// SyncProjects: toProjectSettings field mapping
// ---------------------------------------------------------------------------

// TestToProjectSettings_MapsAllFields verifies that toProjectSettings maps
// every manifest.AdvancedSettings field to the correct project.AdvancedSettings
// field, in particular that SetGitHubStatus maps to SetGithubStatus.
func TestToProjectSettings_MapsAllFields(t *testing.T) {
	trueVal := true
	falseVal := false
	overrides := []string{"main", "release"}
	src := &manifest.AdvancedSettings{
		AutocancelBuilds:           &trueVal,
		BuildForkPRs:               &falseVal,
		BuildPRsOnly:               &trueVal,
		DisableSSH:                 &falseVal,
		ForksReceiveSecretEnvVars:  &trueVal,
		OSS:                        &falseVal,
		SetGitHubStatus:            &trueVal,
		SetupWorkflows:             &falseVal,
		WriteSettingsRequiresAdmin: &trueVal,
		PROnlyBranchOverrides:      overrides,
	}

	got := toProjectSettings(src)

	check := func(field string, got, want *bool) {
		t.Helper()
		if got == nil {
			t.Errorf("%s: got nil, want non-nil", field)
			return
		}
		if *got != *want {
			t.Errorf("%s: got %v want %v", field, *got, *want)
		}
	}

	check("AutocancelBuilds", got.AutocancelBuilds, &trueVal)
	check("BuildForkPRs", got.BuildForkPRs, &falseVal)
	check("BuildPRsOnly", got.BuildPRsOnly, &trueVal)
	check("DisableSSH", got.DisableSSH, &falseVal)
	check("ForksReceiveSecretEnvVars", got.ForksReceiveSecretEnvVars, &trueVal)
	check("OSS", got.OSS, &falseVal)
	// Key mapping: manifest.SetGitHubStatus → project.SetGithubStatus
	check("SetGithubStatus", got.SetGithubStatus, &trueVal)
	check("SetupWorkflows", got.SetupWorkflows, &falseVal)
	check("WriteSettingsRequiresAdmin", got.WriteSettingsRequiresAdmin, &trueVal)

	if len(got.PROnlyBranchOverrides) != len(overrides) {
		t.Errorf("PROnlyBranchOverrides: got %v want %v", got.PROnlyBranchOverrides, overrides)
	}
}

// ---------------------------------------------------------------------------
// SyncProjects: MissingPlaceholder CreateEnvVar error
// ---------------------------------------------------------------------------

// TestSyncProjects_Placeholder_CreateEnvVarError_IsErrorAction verifies that a
// CreateEnvVar failure during placeholder write is recorded as an "error" action.
func TestSyncProjects_Placeholder_CreateEnvVarError_IsErrorAction(t *testing.T) {
	fp := &fakeProjectWriter{
		createEnvVar: func(slug, name, value string) error {
			return errors.New("placeholder write failed")
		},
	}
	sy := newSyncerProjects(fp)

	srcSlug := "gh/acme/web"
	p := simpleProject(srcSlug, "MISSING_VAR")
	m := projectManifest("gh/acme", p)
	bundle := projectBundleWith(srcSlug) // no value

	rep, err := sy.SyncProjects(context.Background(), m, bundle, nil, Options{Apply: true, MissingSecrets: MissingPlaceholder})
	if err != nil {
		t.Fatalf("placeholder write error must not propagate, got: %v", err)
	}

	hasError := false
	for _, a := range rep.Actions {
		if a.Kind == "project-var" && a.Status == "error" {
			hasError = true
		}
	}
	if !hasError {
		t.Error("expected an 'error' project-var action when placeholder CreateEnvVar fails")
	}
}

// ---------------------------------------------------------------------------
// SyncProjects: Applied field on Report
// ---------------------------------------------------------------------------

// TestSyncProjects_Report_AppliedField verifies that Report.Applied reflects
// the Options.Apply value.
func TestSyncProjects_Report_AppliedField(t *testing.T) {
	fp := &fakeProjectWriter{}
	sy := newSyncerProjects(fp)
	m := projectManifest("gh/acme")

	repDry, err := sy.SyncProjects(context.Background(), m, nil, nil, Options{Apply: false})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repDry.Applied {
		t.Error("Report.Applied should be false when Apply=false")
	}

	repApply, err := sy.SyncProjects(context.Background(), m, nil, nil, Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !repApply.Applied {
		t.Error("Report.Applied should be true when Apply=true")
	}
}

// ---------------------------------------------------------------------------
// SyncProjects: invalid dest slug (SplitSlug fails)
// ---------------------------------------------------------------------------

// TestSyncProjects_InvalidDestSlug_ErrorAction verifies that if the resolved
// destination slug is not a valid three-part slug, an "error" action is recorded
// for project-settings and no panic occurs.
func TestSyncProjects_InvalidDestSlug_ErrorAction(t *testing.T) {
	fp := &fakeProjectWriter{}
	sy := newSyncerProjects(fp)

	trueVal := true
	p := manifest.Project{
		Slug:    "gh/acme/web",
		Name:    "web",
		EnvVars: nil,
		Settings: &manifest.AdvancedSettings{
			AutocancelBuilds: &trueVal,
		},
	}
	m := projectManifest("gh/acme", p)

	// Mapping with an explicit bad dest slug (only two parts).
	mapping := &manifest.Mapping{
		Org:      manifest.OrgMapping{From: "gh/acme", To: "gh/acme-new"},
		Projects: map[string]string{"gh/acme/web": "gh/badslug"},
	}

	rep, err := sy.SyncProjects(context.Background(), m, nil, mapping, Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected top-level error: %v", err)
	}

	hasError := false
	for _, a := range rep.Actions {
		if a.Status == "error" {
			hasError = true
		}
	}
	if !hasError {
		t.Error("expected an 'error' action for invalid dest slug, got none")
	}
}

// ---------------------------------------------------------------------------
// SyncProjects: webhook sync
// ---------------------------------------------------------------------------

// TestSyncProjects_Webhook_Created verifies that a webhook not present in the
// destination is created (Apply=true) and a signing-secret manual note is emitted.
func TestSyncProjects_Webhook_Created(t *testing.T) {
	verifyTLS := true
	fp := &fakeProjectWriter{}
	sy := newSyncerProjects(fp)

	p := manifest.Project{
		Slug: "gh/acme/web",
		Name: "web",
		Webhooks: []manifest.Webhook{
			{Name: "ci-notify", URL: "https://hooks.example.com", Events: []string{"workflow-completed"}, VerifyTLS: &verifyTLS},
		},
	}
	m := projectManifest("gh/acme", p)

	rep, err := sy.SyncProjects(context.Background(), m, nil, nil, Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !fp.hasCalled("CreateWebhook") {
		t.Error("CreateWebhook must be called when webhook is not present and Apply=true")
	}

	// Expect a "set" action for creation and a "manual" action for signing-secret.
	setFound, manualFound := false, false
	for _, a := range actionsOfKind(rep, "project-webhook") {
		if a.Status == "set" {
			setFound = true
		}
		if a.Status == "manual" {
			manualFound = true
		}
	}
	if !setFound {
		t.Error("expected a 'set' project-webhook action")
	}
	if !manualFound {
		t.Error("expected a 'manual' project-webhook action (signing-secret note)")
	}
}

// TestSyncProjects_Webhook_Exists verifies that a webhook already present with
// the same name+url is not created and gets an "exists" status.
func TestSyncProjects_Webhook_Exists(t *testing.T) {
	fp := &fakeProjectWriter{
		listWebhooks: func(projectID string) ([]project.Webhook, error) {
			return []project.Webhook{
				{ID: "existing-wh", Name: "ci-notify", URL: "https://hooks.example.com"},
			}, nil
		},
	}
	sy := newSyncerProjects(fp)

	p := manifest.Project{
		Slug: "gh/acme/web",
		Name: "web",
		Webhooks: []manifest.Webhook{
			{Name: "ci-notify", URL: "https://hooks.example.com", Events: []string{"workflow-completed"}},
		},
	}
	m := projectManifest("gh/acme", p)

	rep, err := sy.SyncProjects(context.Background(), m, nil, nil, Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fp.hasCalled("CreateWebhook") {
		t.Error("CreateWebhook must NOT be called when webhook already exists")
	}

	found := false
	for _, a := range actionsOfKind(rep, "project-webhook") {
		if a.Status == "exists" {
			found = true
		}
	}
	if !found {
		t.Error("expected an 'exists' project-webhook action")
	}
}

// TestSyncProjects_Webhook_DryRun verifies that in dry-run mode CreateWebhook
// is not called but a "set" action is recorded.
func TestSyncProjects_Webhook_DryRun(t *testing.T) {
	fp := &fakeProjectWriter{}
	sy := newSyncerProjects(fp)

	p := manifest.Project{
		Slug: "gh/acme/web",
		Name: "web",
		Webhooks: []manifest.Webhook{
			{Name: "ci-notify", URL: "https://hooks.example.com", Events: []string{"workflow-completed"}},
		},
	}
	m := projectManifest("gh/acme", p)

	rep, err := sy.SyncProjects(context.Background(), m, nil, nil, Options{Apply: false})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fp.hasCalled("CreateWebhook") {
		t.Error("CreateWebhook must NOT be called in dry-run mode")
	}

	found := false
	for _, a := range actionsOfKind(rep, "project-webhook") {
		if a.Status == "set" {
			found = true
		}
	}
	if !found {
		t.Error("expected a 'set' (would create) project-webhook action in dry-run")
	}
}

// ---------------------------------------------------------------------------
// SyncProjects: schedule sync
// ---------------------------------------------------------------------------

// TestSyncProjects_Schedule_OAuth_Created verifies that a schedule is created
// on an OAuth (gh/) destination slug.
func TestSyncProjects_Schedule_OAuth_Created(t *testing.T) {
	fp := &fakeProjectWriter{}
	sy := newSyncerProjects(fp)

	p := manifest.Project{
		Slug: "gh/acme/web",
		Name: "web",
		Schedules: []manifest.Schedule{
			{Name: "nightly", Description: "Nightly build",
				Timetable: map[string]any{"per-hour": 1}, Parameters: map[string]any{"branch": "main"}},
		},
	}
	m := projectManifest("gh/acme", p)

	rep, err := sy.SyncProjects(context.Background(), m, nil, nil, Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !fp.hasCalled("CreateSchedule") {
		t.Error("CreateSchedule must be called for an OAuth destination when Apply=true")
	}

	found := false
	for _, a := range actionsOfKind(rep, "project-schedule") {
		if a.Status == "set" {
			found = true
		}
	}
	if !found {
		t.Error("expected a 'set' project-schedule action")
	}
}

// TestSyncProjects_Schedule_OAuth_Exists verifies that a schedule already
// present by name is not re-created.
func TestSyncProjects_Schedule_OAuth_Exists(t *testing.T) {
	fp := &fakeProjectWriter{
		listSchedules: func(slug string) ([]project.Schedule, error) {
			return []project.Schedule{{ID: "s1", Name: "nightly"}}, nil
		},
	}
	sy := newSyncerProjects(fp)

	p := manifest.Project{
		Slug:      "gh/acme/web",
		Name:      "web",
		Schedules: []manifest.Schedule{{Name: "nightly"}},
	}
	m := projectManifest("gh/acme", p)

	rep, err := sy.SyncProjects(context.Background(), m, nil, nil, Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fp.hasCalled("CreateSchedule") {
		t.Error("CreateSchedule must NOT be called when schedule already exists")
	}

	found := false
	for _, a := range actionsOfKind(rep, "project-schedule") {
		if a.Status == "exists" {
			found = true
		}
	}
	if !found {
		t.Error("expected an 'exists' project-schedule action")
	}
}

// TestSyncProjects_Schedule_AppSlug_Manual verifies that when the destination
// is a GitHub App ("circleci/") slug, schedules are NOT created and a "manual"
// action is recorded instead.
func TestSyncProjects_Schedule_AppSlug_Manual(t *testing.T) {
	fp := &fakeProjectWriter{
		getProject: func(slug string) (*project.Project, error) {
			return &project.Project{Slug: slug, ID: "proj-app-id", Name: "web"}, nil
		},
	}
	sy := newSyncerProjects(fp)

	srcSlug := "gh/acme/web"
	appDstSlug := "circleci/org-id-abc/proj-id-def"
	p := manifest.Project{
		Slug:      srcSlug,
		Name:      "web",
		Schedules: []manifest.Schedule{{Name: "nightly"}},
	}
	m := projectManifest("gh/acme", p)

	mapping := &manifest.Mapping{
		Org:      manifest.OrgMapping{From: "gh/acme", To: "circleci/org-id-abc"},
		Projects: map[string]string{srcSlug: appDstSlug},
	}

	rep, err := sy.SyncProjects(context.Background(), m, nil, mapping, Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fp.hasCalled("CreateSchedule") {
		t.Error("CreateSchedule must NOT be called for a GitHub App destination slug")
	}

	found := false
	for _, a := range actionsOfKind(rep, "project-schedule") {
		if a.Status == "manual" {
			found = true
		}
	}
	if !found {
		t.Error("expected a 'manual' project-schedule action for App slug")
	}
}

// ---------------------------------------------------------------------------
// SyncProjects: project OIDC sync
// ---------------------------------------------------------------------------

// TestSyncProjects_OIDCClaims_Set verifies that project OIDC claims are applied
// when present in the manifest and Apply=true.
func TestSyncProjects_OIDCClaims_Set(t *testing.T) {
	fp := &fakeProjectWriter{
		getProject: func(slug string) (*project.Project, error) {
			return &project.Project{Slug: slug, ID: "dest-proj-id", Name: "web"}, nil
		},
	}
	sy := newSyncerProjects(fp)

	p := manifest.Project{
		Slug:         "gh/acme/web",
		Name:         "web",
		OIDCAudience: []string{"https://oidc.example.com"},
		OIDCTTL:      "4h",
	}
	m := projectManifest("gh/acme", p)

	rep, err := sy.SyncProjects(context.Background(), m, nil, nil, Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !fp.hasCalled("SetProjectOIDCClaims") {
		t.Error("SetProjectOIDCClaims must be called when OIDCAudience/OIDCTTL are set and Apply=true")
	}

	found := false
	for _, a := range actionsOfKind(rep, "project-oidc") {
		if a.Status == "set" {
			found = true
		}
	}
	if !found {
		t.Error("expected a 'set' project-oidc action")
	}
}

// TestSyncProjects_OIDCClaims_DryRun verifies that in dry-run mode
// SetProjectOIDCClaims is not called.
func TestSyncProjects_OIDCClaims_DryRun(t *testing.T) {
	fp := &fakeProjectWriter{
		getProject: func(slug string) (*project.Project, error) {
			return &project.Project{Slug: slug, ID: "dest-proj-id", Name: "web"}, nil
		},
	}
	sy := newSyncerProjects(fp)

	p := manifest.Project{
		Slug:         "gh/acme/web",
		Name:         "web",
		OIDCAudience: []string{"https://oidc.example.com"},
		OIDCTTL:      "4h",
	}
	m := projectManifest("gh/acme", p)

	rep, err := sy.SyncProjects(context.Background(), m, nil, nil, Options{Apply: false})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fp.hasCalled("SetProjectOIDCClaims") {
		t.Error("SetProjectOIDCClaims must NOT be called in dry-run mode")
	}

	found := false
	for _, a := range actionsOfKind(rep, "project-oidc") {
		if a.Status == "set" {
			found = true
		}
	}
	if !found {
		t.Error("expected a 'set' (would set) project-oidc action in dry-run")
	}
}

// TestSyncProjects_OIDCClaims_Empty_NoAction verifies that when the manifest
// project has no OIDC fields, no OIDC action is emitted.
func TestSyncProjects_OIDCClaims_Empty_NoAction(t *testing.T) {
	fp := &fakeProjectWriter{}
	sy := newSyncerProjects(fp)

	p := simpleProject("gh/acme/web")
	m := projectManifest("gh/acme", p)

	rep, err := sy.SyncProjects(context.Background(), m, nil, nil, Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fp.hasCalled("SetProjectOIDCClaims") {
		t.Error("SetProjectOIDCClaims must NOT be called when OIDCAudience and OIDCTTL are empty")
	}
	if len(actionsOfKind(rep, "project-oidc")) != 0 {
		t.Error("expected no project-oidc actions when OIDC fields are empty")
	}
}

// ---------------------------------------------------------------------------
// SyncProjects: per-project v1.1 feature flags
// ---------------------------------------------------------------------------

// TestSyncProjects_APITriggerFlag_Set verifies that api_trigger_with_config is
// applied when present and Apply=true.
func TestSyncProjects_APITriggerFlag_Set(t *testing.T) {
	fp := &fakeProjectWriter{}
	sy := newSyncerProjects(fp)

	trueVal := true
	p := manifest.Project{
		Slug: "gh/acme/web",
		Name: "web",
		Settings: &manifest.AdvancedSettings{
			APITriggerWithConfig: &trueVal,
		},
	}
	m := projectManifest("gh/acme", p)

	rep, err := sy.SyncProjects(context.Background(), m, nil, nil, Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !fp.hasCalled("SetV11ProjectFeatureFlags") {
		t.Error("SetV11ProjectFeatureFlags must be called when api_trigger_with_config is set and Apply=true")
	}

	found := false
	for _, a := range actionsOfKind(rep, "project-flag") {
		if a.Status == "set" {
			found = true
		}
	}
	if !found {
		t.Error("expected a 'set' project-flag action for api_trigger_with_config")
	}
}

// TestSyncProjects_DropAllBuildRequests_TrueEmitsManual verifies that when
// drop_all_build_requests is true, a "manual" warning is emitted and the flag
// is never written to the destination.
func TestSyncProjects_DropAllBuildRequests_TrueEmitsManual(t *testing.T) {
	fp := &fakeProjectWriter{}
	sy := newSyncerProjects(fp)

	trueVal := true
	p := manifest.Project{
		Slug: "gh/acme/web",
		Name: "web",
		Settings: &manifest.AdvancedSettings{
			DropAllBuildRequests: &trueVal,
		},
	}
	m := projectManifest("gh/acme", p)

	rep, err := sy.SyncProjects(context.Background(), m, nil, nil, Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fp.hasCalled("SetV11ProjectFeatureFlags") {
		t.Error("SetV11ProjectFeatureFlags must NOT be called for drop_all_build_requests (danger flag)")
	}

	found := false
	for _, a := range actionsOfKind(rep, "project-flag") {
		if a.Status == "manual" {
			found = true
		}
	}
	if !found {
		t.Error("expected a 'manual' project-flag action when drop_all_build_requests=true")
	}
}

// TestSyncProjects_DropAllBuildRequests_FalseNoNoise verifies that when
// drop_all_build_requests is false (its default/non-set value), NO manual
// warning action is emitted — only non-danger flags should generate noise.
func TestSyncProjects_DropAllBuildRequests_FalseNoNoise(t *testing.T) {
	fp := &fakeProjectWriter{}
	sy := newSyncerProjects(fp)

	falseVal := false
	p := manifest.Project{
		Slug: "gh/acme/web",
		Name: "web",
		Settings: &manifest.AdvancedSettings{
			DropAllBuildRequests: &falseVal,
		},
	}
	m := projectManifest("gh/acme", p)

	rep, err := sy.SyncProjects(context.Background(), m, nil, nil, Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fp.hasCalled("SetV11ProjectFeatureFlags") {
		t.Error("SetV11ProjectFeatureFlags must NOT be called for drop_all_build_requests (danger flag, even when false)")
	}

	for _, a := range actionsOfKind(rep, "project-flag") {
		if a.Status == "manual" {
			t.Errorf("no manual action expected when drop_all_build_requests=false, got action: %+v", a)
		}
	}
}

// TestSyncProjects_APITriggerFlag_DryRun verifies that in dry-run mode
// SetV11ProjectFeatureFlags is not called.
func TestSyncProjects_APITriggerFlag_DryRun(t *testing.T) {
	fp := &fakeProjectWriter{}
	sy := newSyncerProjects(fp)

	trueVal := true
	p := manifest.Project{
		Slug: "gh/acme/web",
		Name: "web",
		Settings: &manifest.AdvancedSettings{
			APITriggerWithConfig: &trueVal,
		},
	}
	m := projectManifest("gh/acme", p)

	rep, err := sy.SyncProjects(context.Background(), m, nil, nil, Options{Apply: false})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fp.hasCalled("SetV11ProjectFeatureFlags") {
		t.Error("SetV11ProjectFeatureFlags must NOT be called in dry-run mode")
	}

	found := false
	for _, a := range actionsOfKind(rep, "project-flag") {
		if a.Status == "set" {
			found = true
		}
	}
	if !found {
		t.Error("expected a 'set' (would set) project-flag action in dry-run")
	}
}

// ---------------------------------------------------------------------------
// EnableBuilds
// ---------------------------------------------------------------------------

// TestEnableBuilds_Apply_CallsFollowProject verifies that EnableBuilds with
// Kind="follow" and apply=true calls FollowProject and returns a "set" Action.
func TestEnableBuilds_Apply_CallsFollowProject(t *testing.T) {
	fp := &fakeProjectWriter{}
	sy := newSyncerProjects(fp)

	target := EnableTarget{Kind: "follow", Slug: "gh/acme/web", VCSType: "gh", Org: "acme", Repo: "web"}
	action, err := sy.EnableBuilds(context.Background(), target, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !fp.hasCalled("FollowProject") {
		t.Error("FollowProject must be called when apply=true")
	}
	if action.Status != "set" {
		t.Errorf("action.Status: got %q, want %q", action.Status, "set")
	}
	if action.Target != "gh/acme/web" {
		t.Errorf("action.Target: got %q, want %q", action.Target, "gh/acme/web")
	}
}

// TestEnableBuilds_DryRun_NoFollowProject verifies that EnableBuilds with
// Kind="follow" and apply=false does not call FollowProject and returns a "manual" Action.
func TestEnableBuilds_DryRun_NoFollowProject(t *testing.T) {
	fp := &fakeProjectWriter{}
	sy := newSyncerProjects(fp)

	target := EnableTarget{Kind: "follow", Slug: "gh/acme/web", VCSType: "gh", Org: "acme", Repo: "web"}
	action, err := sy.EnableBuilds(context.Background(), target, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fp.hasCalled("FollowProject") {
		t.Error("FollowProject must NOT be called in dry-run mode")
	}
	if action.Status != "manual" {
		t.Errorf("action.Status: got %q, want %q", action.Status, "manual")
	}
}

// TestEnableBuilds_Apply_FollowError_ReturnsError verifies that a FollowProject
// error is returned and reflected in the Action status.
func TestEnableBuilds_Apply_FollowError_ReturnsError(t *testing.T) {
	fp := &fakeProjectWriter{
		followProject: func(vcsType, org, repo string) (*project.FollowResult, error) {
			return nil, errors.New("follow API down")
		},
	}
	sy := newSyncerProjects(fp)

	target := EnableTarget{Kind: "follow", Slug: "gh/acme/web", VCSType: "gh", Org: "acme", Repo: "web"}
	action, err := sy.EnableBuilds(context.Background(), target, true)
	if err == nil {
		t.Fatal("expected error from FollowProject failure, got nil")
	}
	if action.Status != "error" {
		t.Errorf("action.Status: got %q, want %q", action.Status, "error")
	}
}

// ---------------------------------------------------------------------------
// EnableBuilds: App trigger path
// ---------------------------------------------------------------------------

// TestEnableBuilds_Trigger_Apply_CallsEnableTrigger verifies that EnableBuilds
// with Kind="trigger" and apply=true calls EnableTrigger and returns a "set" Action.
func TestEnableBuilds_Trigger_Apply_CallsEnableTrigger(t *testing.T) {
	fp := &fakeProjectWriter{}
	sy := newSyncerProjects(fp)

	target := EnableTarget{Kind: "trigger", ProjectID: "proj-uuid", TriggerID: "trig-uuid"}
	action, err := sy.EnableBuilds(context.Background(), target, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !fp.hasCalled("EnableTrigger") {
		t.Error("EnableTrigger must be called when Kind=trigger and apply=true")
	}
	if action.Status != "set" {
		t.Errorf("action.Status: got %q, want %q", action.Status, "set")
	}
}

// TestEnableBuilds_Trigger_DryRun_NoEnableTrigger verifies that EnableBuilds
// with Kind="trigger" and apply=false does not call EnableTrigger.
func TestEnableBuilds_Trigger_DryRun_NoEnableTrigger(t *testing.T) {
	fp := &fakeProjectWriter{}
	sy := newSyncerProjects(fp)

	target := EnableTarget{Kind: "trigger", ProjectID: "proj-uuid", TriggerID: "trig-uuid"}
	action, err := sy.EnableBuilds(context.Background(), target, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fp.hasCalled("EnableTrigger") {
		t.Error("EnableTrigger must NOT be called in dry-run mode")
	}
	if action.Status != "manual" {
		t.Errorf("action.Status: got %q, want %q", action.Status, "manual")
	}
}

// TestEnableBuilds_Trigger_Error_ReturnsError verifies that an EnableTrigger
// error is returned and reflected in the Action status.
func TestEnableBuilds_Trigger_Error_ReturnsError(t *testing.T) {
	fp := &fakeProjectWriter{
		enableTrigger: func(projectID, triggerID string) error {
			return errors.New("enable trigger API down")
		},
	}
	sy := newSyncerProjects(fp)

	target := EnableTarget{Kind: "trigger", ProjectID: "proj-uuid", TriggerID: "trig-uuid"}
	action, err := sy.EnableBuilds(context.Background(), target, true)
	if err == nil {
		t.Fatal("expected error from EnableTrigger failure, got nil")
	}
	if action.Status != "error" {
		t.Errorf("action.Status: got %q, want %q", action.Status, "error")
	}
}

// TestSyncProjects_CreateProjectShell_Error_IsErrorAction verifies that a
// CreateProjectShell failure is recorded as an "error" action and the project
// is skipped (no settings/vars helpers called).
func TestSyncProjects_CreateProjectShell_Error_IsErrorAction(t *testing.T) {
	fp := &fakeProjectWriter{
		getProject: func(slug string) (*project.Project, error) {
			return nil, errors.New("project not found")
		},
		createProjectShell: func(provider, org, name string) (*project.Project, error) {
			return nil, errors.New("create shell API down")
		},
	}
	sy := newSyncerProjects(fp)

	p := simpleProject("gh/acme/web", "DB_URL")
	m := projectManifest("gh/acme", p)

	rep, err := sy.SyncProjects(context.Background(), m, nil, nil, Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	a := firstActionOfKind(rep, "project")
	if a == nil {
		t.Fatal("expected a project action, got none")
		return
	}
	if a.Status != "error" {
		t.Errorf("status: got %q want %q", a.Status, "error")
	}
	if fp.hasCalled("UpdateSettings") {
		t.Error("UpdateSettings must NOT be called when CreateProjectShell fails")
	}
	if fp.hasCalled("CreateEnvVar") {
		t.Error("CreateEnvVar must NOT be called when CreateProjectShell fails")
	}
}

// ---------------------------------------------------------------------------
// SyncProjects: GitHub App (circleci vcs_type) destination path
// ---------------------------------------------------------------------------

// appManifestProject builds a manifest.Project suitable for App-path tests
// with a github_app pipeline definition and one github_app trigger.
func appManifestProject(name, srcSlug, extID, preset string) manifest.Project {
	return manifest.Project{
		Slug: srcSlug,
		Name: name,
		PipelineDefinitions: []manifest.PipelineDefinition{
			{
				Name: "default",
				ConfigSource: manifest.PipelineSource{
					Provider:       "github_app",
					RepoFullName:   "acme/" + name,
					RepoExternalID: extID,
					FilePath:       ".circleci/config.yml",
				},
				CheckoutSource: manifest.PipelineSource{
					Provider:       "github_app",
					RepoFullName:   "acme/" + name,
					RepoExternalID: extID,
				},
				Triggers: []manifest.Trigger{
					{
						Name:        "push",
						EventPreset: preset,
						EventSource: manifest.TriggerEventSource{
							Provider:       "github_app",
							RepoFullName:   "acme/" + name,
							RepoExternalID: extID,
						},
					},
				},
			},
		},
	}
}

// TestSyncProjects_AppDest_CreateProject_Apply verifies that when the destination
// org is circleci-type, a missing project is created with CreateAppProject, a
// pipeline definition is created, and a disabled trigger is queued for enabling.
func TestSyncProjects_AppDest_CreateProject_Apply(t *testing.T) {
	fp := &fakeProjectWriter{}
	sy := newSyncerAppProjects(fp)

	p := appManifestProject("web", "gh/acme/web", "12345", "code_push")
	m := projectManifest("gh/acme", p)

	rep, err := sy.SyncProjects(context.Background(), m, nil, nil, Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !fp.hasCalled("CreateAppProject") {
		t.Error("CreateAppProject must be called for a missing App project when Apply=true")
	}
	if !fp.hasCalled("CreatePipelineDefinition") {
		t.Error("CreatePipelineDefinition must be called after App project creation")
	}
	if !fp.hasCalled("CreateTrigger") {
		t.Error("CreateTrigger must be called for github_app triggers")
	}

	// Project action should be "created".
	a := firstActionOfKind(rep, "project")
	if a == nil {
		t.Fatal("expected a project action, got none")
		return
	}
	if a.Status != "created" {
		t.Errorf("project action.Status: got %q, want created", a.Status)
	}

	// A trigger should be queued for enabling.
	if len(rep.PendingEnable) != 1 {
		t.Fatalf("PendingEnable: got %d, want 1", len(rep.PendingEnable))
	}
	if rep.PendingEnable[0].Kind != "trigger" {
		t.Errorf("PendingEnable[0].Kind: got %q, want trigger", rep.PendingEnable[0].Kind)
	}
}

// TestSyncProjects_AppDest_CreateProject_DryRun verifies that in dry-run mode
// CreateAppProject is not called but a "created" plan action is recorded.
func TestSyncProjects_AppDest_CreateProject_DryRun(t *testing.T) {
	fp := &fakeProjectWriter{}
	sy := newSyncerAppProjects(fp)

	p := appManifestProject("web", "gh/acme/web", "12345", "code_push")
	m := projectManifest("gh/acme", p)

	rep, err := sy.SyncProjects(context.Background(), m, nil, nil, Options{Apply: false})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fp.hasCalled("CreateAppProject") {
		t.Error("CreateAppProject must NOT be called in dry-run mode")
	}
	if fp.hasCalled("CreatePipelineDefinition") {
		t.Error("CreatePipelineDefinition must NOT be called in dry-run mode")
	}

	a := firstActionOfKind(rep, "project")
	if a == nil {
		t.Fatal("expected a project action in dry-run")
		return
	}
	if a.Status != "created" {
		t.Errorf("project action.Status: got %q, want created", a.Status)
	}
	if !containsStr(a.Detail, "would create App project") {
		t.Errorf("detail %q should mention 'would create App project'", a.Detail)
	}
}

// TestSyncProjects_AppDest_ExistingProject_Reused verifies that when a project
// with the same name exists in the dest org, it is reused and configured (no create).
func TestSyncProjects_AppDest_ExistingProject_Reused(t *testing.T) {
	fp := &fakeProjectWriter{
		listOrgProjects: func(orgID string) ([]project.OrgProject, error) {
			return []project.OrgProject{
				{ID: "existing-proj-id", Slug: "circleci/dest-org-id/existing-proj-id", Name: "web"},
			}, nil
		},
	}
	sy := newSyncerAppProjects(fp)

	p := appManifestProject("web", "gh/acme/web", "12345", "code_push")
	p.EnvVars = []manifest.ProjectEnvVar{{Name: "MY_VAR"}}
	m := projectManifest("gh/acme", p)
	bundle := projectBundleWith("gh/acme/web", "MY_VAR", "secretval")

	rep, err := sy.SyncProjects(context.Background(), m, bundle, nil, Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fp.hasCalled("CreateAppProject") {
		t.Error("CreateAppProject must NOT be called when project already exists")
	}

	// Env var should be synced to the existing project's slug.
	creates := fp.callsTo("CreateEnvVar")
	if len(creates) == 0 {
		t.Fatal("CreateEnvVar must be called for the reused project")
	}
	if creates[0].args[0] != "circleci/dest-org-id/existing-proj-id" {
		t.Errorf("CreateEnvVar slug: got %q, want circleci/dest-org-id/existing-proj-id", creates[0].args[0])
	}

	_ = rep
}

// TestSyncProjects_AppDest_WebhookTrigger_Manual verifies that webhook-provider
// triggers are recorded as "manual" actions (not created).
func TestSyncProjects_AppDest_WebhookTrigger_Manual(t *testing.T) {
	fp := &fakeProjectWriter{}
	sy := newSyncerAppProjects(fp)

	p := manifest.Project{
		Slug: "gh/acme/web",
		Name: "web",
		PipelineDefinitions: []manifest.PipelineDefinition{
			{
				Name: "default",
				ConfigSource: manifest.PipelineSource{
					Provider:       "github_app",
					RepoExternalID: "111",
					FilePath:       ".circleci/config.yml",
				},
				CheckoutSource: manifest.PipelineSource{
					Provider:       "github_app",
					RepoExternalID: "111",
				},
				Triggers: []manifest.Trigger{
					{
						Name: "my-webhook",
						EventSource: manifest.TriggerEventSource{
							Provider:      "webhook",
							WebhookSender: "someone",
						},
					},
				},
			},
		},
	}
	m := projectManifest("gh/acme", p)

	rep, err := sy.SyncProjects(context.Background(), m, nil, nil, Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fp.hasCalled("CreateTrigger") {
		t.Error("CreateTrigger must NOT be called for webhook triggers")
	}

	found := false
	for _, a := range actionsOfKind(rep, "project-trigger") {
		if a.Status == "manual" {
			found = true
		}
	}
	if !found {
		t.Error("expected a 'manual' project-trigger action for webhook trigger")
	}
}

// TestSyncProjects_AppDest_ScheduleTrigger_Manual verifies that schedule-provider
// triggers are recorded as "manual" actions.
func TestSyncProjects_AppDest_ScheduleTrigger_Manual(t *testing.T) {
	fp := &fakeProjectWriter{}
	sy := newSyncerAppProjects(fp)

	p := manifest.Project{
		Slug: "gh/acme/web",
		Name: "web",
		PipelineDefinitions: []manifest.PipelineDefinition{
			{
				Name: "default",
				ConfigSource: manifest.PipelineSource{
					Provider:       "github_app",
					RepoExternalID: "222",
					FilePath:       ".circleci/config.yml",
				},
				CheckoutSource: manifest.PipelineSource{
					Provider:       "github_app",
					RepoExternalID: "222",
				},
				Triggers: []manifest.Trigger{
					{
						Name: "nightly",
						EventSource: manifest.TriggerEventSource{
							Provider:     "schedule",
							ScheduleCron: "0 2 * * *",
						},
					},
				},
			},
		},
	}
	m := projectManifest("gh/acme", p)

	rep, err := sy.SyncProjects(context.Background(), m, nil, nil, Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fp.hasCalled("CreateTrigger") {
		t.Error("CreateTrigger must NOT be called for schedule triggers")
	}

	found := false
	for _, a := range actionsOfKind(rep, "project-trigger") {
		if a.Status == "manual" {
			found = true
		}
	}
	if !found {
		t.Error("expected a 'manual' project-trigger action for schedule trigger")
	}
}

// TestSyncProjects_AppDest_ExternalID_CapturedReused verifies that when no
// GitHub token is provided, the captured RepoExternalID is used directly.
func TestSyncProjects_AppDest_ExternalID_CapturedReused(t *testing.T) {
	var gotSpec project.PipelineDefinitionSpec
	fp := &fakeProjectWriter{
		createPipelineDefinition: func(projectID string, spec project.PipelineDefinitionSpec) (string, error) {
			gotSpec = spec
			return "def-id", nil
		},
	}
	sy := newSyncerAppProjects(fp)

	p := appManifestProject("web", "gh/acme/web", "captured-ext-id", "code_push")
	m := projectManifest("gh/acme", p)

	if _, err := sy.SyncProjects(context.Background(), m, nil, nil, Options{Apply: true}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if gotSpec.ConfigExternalID != "captured-ext-id" {
		t.Errorf("ConfigExternalID: got %q, want captured-ext-id", gotSpec.ConfigExternalID)
	}
}

// TestSyncProjects_AppDest_ExternalID_TokenResolved verifies that when a
// GitHubToken is set, the resolved ID is used instead of the captured one.
func TestSyncProjects_AppDest_ExternalID_TokenResolved(t *testing.T) {
	origResolve := resolveRepoID
	defer func() { resolveRepoID = origResolve }()
	resolveRepoID = func(_ context.Context, fullName, token, baseURL string) (string, error) {
		if fullName == "acme/web" && token == "gh-tok" {
			return "resolved-id-999", nil
		}
		return "", errors.New("unexpected call")
	}

	var gotSpec project.PipelineDefinitionSpec
	fp := &fakeProjectWriter{
		createPipelineDefinition: func(projectID string, spec project.PipelineDefinitionSpec) (string, error) {
			gotSpec = spec
			return "def-id", nil
		},
	}
	sy := newSyncerAppProjects(fp)

	p := appManifestProject("web", "gh/acme/web", "old-captured-id", "code_push")
	m := projectManifest("gh/acme", p)

	if _, err := sy.SyncProjects(context.Background(), m, nil, nil, Options{Apply: true, GitHubToken: "gh-tok"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if gotSpec.ConfigExternalID != "resolved-id-999" {
		t.Errorf("ConfigExternalID: got %q, want resolved-id-999 (token-resolved)", gotSpec.ConfigExternalID)
	}
}

// TestSyncProjects_AppDest_ExternalID_ResolveOtherError_Skips verifies that
// when token resolution fails with a non-404 error (e.g. network failure), an
// "error" action is emitted and CreatePipelineDefinition is NOT called — the
// project is skipped rather than onboarded with a potentially stale id.
func TestSyncProjects_AppDest_ExternalID_ResolveOtherError_Skips(t *testing.T) {
	origResolve := resolveRepoID
	defer func() { resolveRepoID = origResolve }()
	resolveRepoID = func(_ context.Context, fullName, token, baseURL string) (string, error) {
		return "", errors.New("GitHub API unreachable")
	}

	fp := &fakeProjectWriter{}
	sy := newSyncerAppProjects(fp)

	p := appManifestProject("web", "gh/acme/web", "captured-id", "code_push")
	m := projectManifest("gh/acme", p)

	rep, err := sy.SyncProjects(context.Background(), m, nil, nil, Options{Apply: true, GitHubToken: "bad-tok"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// CreatePipelineDefinition must NOT be called when resolution errors.
	if fp.hasCalled("CreatePipelineDefinition") {
		t.Error("CreatePipelineDefinition must NOT be called when repo ID resolution errors out")
	}

	// An "error" action should be emitted.
	found := false
	for _, a := range actionsOfKind(rep, "project-ext-id") {
		if a.Status == "error" {
			found = true
		}
	}
	if !found {
		t.Error("expected an 'error' project-ext-id action when resolution fails with non-404")
	}
}

// TestSyncProjects_AppDest_ExternalID_NoTokenNoCaptured_Manual verifies that
// when neither a token nor a captured id is available, a "manual" action is
// emitted and CreatePipelineDefinition is not called.
func TestSyncProjects_AppDest_ExternalID_NoTokenNoCaptured_Manual(t *testing.T) {
	fp := &fakeProjectWriter{}
	sy := newSyncerAppProjects(fp)

	p := manifest.Project{
		Slug: "gh/acme/web",
		Name: "web",
		PipelineDefinitions: []manifest.PipelineDefinition{
			{
				Name: "default",
				ConfigSource: manifest.PipelineSource{
					Provider: "github_app",
					// No RepoExternalID and no RepoFullName.
				},
				CheckoutSource: manifest.PipelineSource{
					Provider: "github_app",
				},
			},
		},
	}
	m := projectManifest("gh/acme", p)

	rep, err := sy.SyncProjects(context.Background(), m, nil, nil, Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fp.hasCalled("CreatePipelineDefinition") {
		t.Error("CreatePipelineDefinition must NOT be called when no external_id available")
	}

	found := false
	for _, a := range actionsOfKind(rep, "project-ext-id") {
		if a.Status == "manual" {
			found = true
		}
	}
	if !found {
		t.Error("expected a 'manual' project-ext-id action when no external_id available")
	}
}

// TestSyncProjects_AppDest_CreateAppProject_Error verifies that a CreateAppProject
// failure is recorded as an "error" action and configuration helpers are skipped.
func TestSyncProjects_AppDest_CreateAppProject_Error(t *testing.T) {
	fp := &fakeProjectWriter{
		createAppProject: func(orgID, name string) (*project.Project, error) {
			return nil, errors.New("create App project API down")
		},
	}
	sy := newSyncerAppProjects(fp)

	p := appManifestProject("web", "gh/acme/web", "12345", "code_push")
	m := projectManifest("gh/acme", p)

	rep, err := sy.SyncProjects(context.Background(), m, nil, nil, Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	a := firstActionOfKind(rep, "project")
	if a == nil {
		t.Fatal("expected a project action, got none")
		return
	}
	if a.Status != "error" {
		t.Errorf("project action.Status: got %q, want error", a.Status)
	}
	if fp.hasCalled("CreatePipelineDefinition") {
		t.Error("CreatePipelineDefinition must NOT be called when CreateAppProject fails")
	}
}

// ---------------------------------------------------------------------------
// FIX 1: App-dest advanced settings — OAuth-only fields stripped
// ---------------------------------------------------------------------------

// TestSyncProjects_AppDest_Settings_OAuthOnlyFieldsStripped verifies that when
// the destination project is a GitHub App (circleci/ provider), the four
// OAuth-only advanced-settings fields are NOT forwarded to UpdateSettings.
// Background: the live PATCH /settings endpoint for App projects returns
// "Unexpected field 'advanced.oss'" if any of these fields are sent.
func TestSyncProjects_AppDest_Settings_OAuthOnlyFieldsStripped(t *testing.T) {
	fp := &fakeProjectWriter{
		getProject: func(slug string) (*project.Project, error) {
			return &project.Project{Slug: slug, ID: "proj-app-id", Name: "web"}, nil
		},
	}
	sy := newSyncerProjects(fp)

	trueVal := true
	falseVal := false
	p := manifest.Project{
		Slug: "gh/acme/web",
		Name: "web",
		Settings: &manifest.AdvancedSettings{
			// OAuth-only fields — must be stripped for App dest.
			OSS:                       &trueVal,
			BuildForkPRs:              &falseVal,
			ForksReceiveSecretEnvVars: &trueVal,
			PROnlyBranchOverrides:     []string{"main"},
			// Valid App fields — must be preserved.
			AutocancelBuilds: &trueVal,
			SetGitHubStatus:  &trueVal,
			SetupWorkflows:   &falseVal,
		},
	}
	m := projectManifest("gh/acme", p)

	// Route via a mapping with a circleci/ destination slug so that the
	// OAuth path is taken (GetProject succeeds) with a circleci/ provider.
	appDstSlug := "circleci/org-id-abc/proj-id-def"
	mapping := &manifest.Mapping{
		Org:      manifest.OrgMapping{From: "gh/acme", To: "circleci/org-id-abc"},
		Projects: map[string]string{"gh/acme/web": appDstSlug},
	}

	rep, err := sy.SyncProjects(context.Background(), m, nil, mapping, Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = rep

	if !fp.hasCalled("UpdateSettings") {
		t.Fatal("UpdateSettings must be called when Apply=true and settings are present")
	}
	if len(fp.settingsUpdates) == 0 {
		t.Fatal("no settings updates recorded")
	}
	got := fp.settingsUpdates[0]

	// OAuth-only fields must be nil.
	if got.OSS != nil {
		t.Error("OSS must be nil (stripped) for a circleci/ destination")
	}
	if got.BuildForkPRs != nil {
		t.Error("BuildForkPRs must be nil (stripped) for a circleci/ destination")
	}
	if got.ForksReceiveSecretEnvVars != nil {
		t.Error("ForksReceiveSecretEnvVars must be nil (stripped) for a circleci/ destination")
	}
	if len(got.PROnlyBranchOverrides) != 0 {
		t.Errorf("PROnlyBranchOverrides must be empty (stripped) for a circleci/ destination, got %v", got.PROnlyBranchOverrides)
	}

	// Valid App fields must be preserved.
	if got.AutocancelBuilds == nil || !*got.AutocancelBuilds {
		t.Error("AutocancelBuilds must be preserved for a circleci/ destination")
	}
	if got.SetGithubStatus == nil || !*got.SetGithubStatus {
		t.Error("SetGithubStatus must be preserved for a circleci/ destination")
	}
	if got.SetupWorkflows == nil || *got.SetupWorkflows {
		t.Error("SetupWorkflows must be preserved (false) for a circleci/ destination")
	}
}

// TestSyncProjects_OAuthDest_Settings_OAuthFieldsPassedThrough verifies that for
// a non-App (OAuth) destination the syncer does NOT strip fork/PR fields from the
// AdvancedSettings struct it passes to UpdateSettings (unlike the App path which
// calls stripOAuthOnlySettings).  Note that "oss" is never serialised to the wire
// regardless — that is enforced at the project.Client.UpdateSettings layer (see
// advancedSettingsPatch in write.go and issue #247).
func TestSyncProjects_OAuthDest_Settings_OAuthFieldsPassedThrough(t *testing.T) {
	fp := &fakeProjectWriter{}
	sy := newSyncerProjects(fp)

	trueVal := true
	overrides := []string{"main", "develop"}
	p := manifest.Project{
		Slug: "gh/acme/web",
		Name: "web",
		Settings: &manifest.AdvancedSettings{
			OSS:                       &trueVal,
			BuildForkPRs:              &trueVal,
			ForksReceiveSecretEnvVars: &trueVal,
			PROnlyBranchOverrides:     overrides,
			AutocancelBuilds:          &trueVal,
		},
	}
	m := projectManifest("gh/acme", p)

	// gh/ destination — OAuth path.
	_, err := sy.SyncProjects(context.Background(), m, nil, nil, Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !fp.hasCalled("UpdateSettings") {
		t.Fatal("UpdateSettings must be called")
	}
	got := fp.settingsUpdates[0]

	// The syncer does not strip these fields from the struct for OAuth destinations
	// (stripping is a no-op at this layer; the write.go layer omits oss from the
	// wire body for all project types per #247).
	if got.OSS == nil || !*got.OSS {
		t.Error("OSS must be present in the AdvancedSettings struct for OAuth destination (write.go omits it from the wire body)")
	}
	if got.BuildForkPRs == nil || !*got.BuildForkPRs {
		t.Error("BuildForkPRs must be passed through for OAuth destination")
	}
	if got.ForksReceiveSecretEnvVars == nil || !*got.ForksReceiveSecretEnvVars {
		t.Error("ForksReceiveSecretEnvVars must be passed through for OAuth destination")
	}
	if len(got.PROnlyBranchOverrides) != 2 {
		t.Errorf("PROnlyBranchOverrides must be passed through for OAuth destination, got %v", got.PROnlyBranchOverrides)
	}
}

// TestStripOAuthOnlySettings_MutationSafety verifies that stripOAuthOnlySettings
// returns a new struct and does not mutate the original.
func TestStripOAuthOnlySettings_MutationSafety(t *testing.T) {
	trueVal := true
	orig := &project.AdvancedSettings{
		OSS:                       &trueVal,
		BuildForkPRs:              &trueVal,
		ForksReceiveSecretEnvVars: &trueVal,
		PROnlyBranchOverrides:     []string{"main"},
		AutocancelBuilds:          &trueVal,
	}

	stripped := stripOAuthOnlySettings(orig)

	// Stripped copy must have nil OAuth fields.
	if stripped.OSS != nil {
		t.Error("stripped.OSS must be nil")
	}
	if stripped.BuildForkPRs != nil {
		t.Error("stripped.BuildForkPRs must be nil")
	}
	if stripped.ForksReceiveSecretEnvVars != nil {
		t.Error("stripped.ForksReceiveSecretEnvVars must be nil")
	}
	if len(stripped.PROnlyBranchOverrides) != 0 {
		t.Errorf("stripped.PROnlyBranchOverrides must be empty, got %v", stripped.PROnlyBranchOverrides)
	}
	// Non-OAuth field preserved.
	if stripped.AutocancelBuilds == nil || !*stripped.AutocancelBuilds {
		t.Error("AutocancelBuilds must be preserved in stripped copy")
	}

	// Original must not be mutated.
	if orig.OSS == nil {
		t.Error("original.OSS must not be mutated (should still be non-nil)")
	}
	if orig.BuildForkPRs == nil {
		t.Error("original.BuildForkPRs must not be mutated")
	}
	if len(orig.PROnlyBranchOverrides) != 1 {
		t.Errorf("original.PROnlyBranchOverrides must not be mutated, got %v", orig.PROnlyBranchOverrides)
	}
}

// ---------------------------------------------------------------------------
// FIX 3: CreatePipelineDefinition repo-access error → "manual" status
// ---------------------------------------------------------------------------

// TestSyncProjects_AppDest_PipelineDef_RepoAccessError_Manual verifies that
// when CreatePipelineDefinition returns an error containing
// "does not have access to repository", the action status is "manual" (not
// "error") and the detail includes remediation guidance.
func TestSyncProjects_AppDest_PipelineDef_RepoAccessError_Manual(t *testing.T) {
	fp := &fakeProjectWriter{
		createPipelineDefinition: func(projectID string, spec project.PipelineDefinitionSpec) (string, error) {
			return "", errors.New("Installation does not have access to repository.")
		},
	}
	sy := newSyncerAppProjects(fp)

	p := appManifestProject("web", "gh/acme/web", "12345", "code_push")
	m := projectManifest("gh/acme", p)

	rep, err := sy.SyncProjects(context.Background(), m, nil, nil, Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	a := firstActionOfKind(rep, "project-pipeline-def")
	if a == nil {
		t.Fatal("expected a project-pipeline-def action, got none")
		return
	}
	if a.Status != "manual" {
		t.Errorf("status: got %q want %q (repo-access errors must be manual, not error)", a.Status, "manual")
	}
	if !containsStr(a.Detail, "GitHub App") {
		t.Errorf("detail should mention GitHub App remediation, got: %q", a.Detail)
	}
}

// TestSyncProjects_AppDest_PipelineDef_OtherError_IsErrorAction verifies that
// a non-access CreatePipelineDefinition error is still recorded as "error".
func TestSyncProjects_AppDest_PipelineDef_OtherError_IsErrorAction(t *testing.T) {
	fp := &fakeProjectWriter{
		createPipelineDefinition: func(projectID string, spec project.PipelineDefinitionSpec) (string, error) {
			return "", errors.New("internal server error")
		},
	}
	sy := newSyncerAppProjects(fp)

	p := appManifestProject("web", "gh/acme/web", "12345", "code_push")
	m := projectManifest("gh/acme", p)

	rep, err := sy.SyncProjects(context.Background(), m, nil, nil, Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	a := firstActionOfKind(rep, "project-pipeline-def")
	if a == nil {
		t.Fatal("expected a project-pipeline-def action, got none")
		return
	}
	if a.Status != "error" {
		t.Errorf("status: got %q want %q (non-access errors must be error)", a.Status, "error")
	}
}

// TestIsRepoAccessError verifies the substring-based access-error detector.
func TestIsRepoAccessError(t *testing.T) {
	cases := []struct {
		msg  string
		want bool
	}{
		{"Installation does not have access to repository.", true},
		{"installation does not have access to repository", true}, // case-insensitive
		{"does not have access to repository foo/bar", true},
		{"internal server error", false},
		{"project not found", false},
		{"", false},
	}
	for _, tc := range cases {
		var err error
		if tc.msg != "" {
			err = errors.New(tc.msg)
		}
		got := isRepoAccessError(err)
		if got != tc.want {
			t.Errorf("isRepoAccessError(%q) = %v, want %v", tc.msg, got, tc.want)
		}
	}
}

// containsStr is a test helper to check substring containment.
func containsStr(s, sub string) bool {
	if len(sub) == 0 {
		return true
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Repo-move scenario: resolveExternalID with GitHub-org mapping
// ---------------------------------------------------------------------------

// stubResolveRepoID replaces resolveRepoID for the duration of a test and
// restores it on cleanup.  It returns the provided id/error for the given
// full-name, and errors for any other name.
func stubResolveRepoID(t *testing.T, expectName, returnID string, returnErr error) {
	t.Helper()
	orig := resolveRepoID
	t.Cleanup(func() { resolveRepoID = orig })
	resolveRepoID = func(_ context.Context, fullName, token, baseURL string) (string, error) {
		if fullName == expectName {
			return returnID, returnErr
		}
		t.Errorf("resolveRepoID called with unexpected name %q (want %q)", fullName, expectName)
		return "", errors.New("unexpected call")
	}
}

// TestResolveExternalID_TokenAndRepoFound verifies that when a token is
// provided and the repo is found in the dest GH org, the resolved id is used
// and a "set" action is emitted (previously "resolved"; renamed to "set" so
// it appears in summary counts).
func TestResolveExternalID_TokenAndRepoFound(t *testing.T) {
	stubResolveRepoID(t, "acme-new/web", "999", nil)

	fp := &fakeProjectWriter{
		createPipelineDefinition: func(projectID string, spec project.PipelineDefinitionSpec) (string, error) {
			return "def-id", nil
		},
	}
	sy := newSyncerAppProjects(fp)

	p := appManifestProject("web", "gh/acme/web", "old-id", "code_push")
	// Manually override repo full-names to use the source GH owner "acme".
	p.PipelineDefinitions[0].ConfigSource.RepoFullName = "acme/web"
	p.PipelineDefinitions[0].CheckoutSource.RepoFullName = "acme/web"
	p.PipelineDefinitions[0].Triggers[0].EventSource.RepoFullName = "acme/web"
	m := projectManifest("gh/acme", p)

	mapping := &manifest.Mapping{
		Org:       manifest.OrgMapping{From: "circleci/src", To: "circleci/dst"},
		GitHubOrg: &manifest.OrgMapping{From: "acme", To: "acme-new"},
	}

	rep, err := sy.SyncProjects(context.Background(), m, nil, mapping, Options{Apply: true, GitHubToken: "gh-tok"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !fp.hasCalled("CreatePipelineDefinition") {
		t.Error("CreatePipelineDefinition must be called when repo is found in dest GH org")
	}

	// "set" actions must be present for config and checkout sources (repo found).
	setCount := 0
	for _, a := range actionsOfKind(rep, "project-ext-id") {
		if a.Status == "set" {
			setCount++
		}
	}
	if setCount < 2 {
		t.Errorf("expected at least 2 'set' ext-id actions (config+checkout), got %d", setCount)
	}
}

// TestResolveExternalID_TokenAndRepo404_ManualSkip verifies that when a token
// is provided but the repo returns 404, a "manual" action is emitted and
// CreatePipelineDefinition is NOT called.
func TestResolveExternalID_TokenAndRepo404_ManualSkip(t *testing.T) {
	// Return a 404-style error (wraps github.ErrRepoNotFound).
	notFoundErr := fmt.Errorf("github: ResolveRepoID %q: %w (HTTP 404)", "acme-new/web", github.ErrRepoNotFound)
	stubResolveRepoID(t, "acme-new/web", "", notFoundErr)

	fp := &fakeProjectWriter{}
	sy := newSyncerAppProjects(fp)

	p := appManifestProject("web", "gh/acme/web", "old-id", "code_push")
	p.PipelineDefinitions[0].ConfigSource.RepoFullName = "acme/web"
	p.PipelineDefinitions[0].CheckoutSource.RepoFullName = "acme/web"
	p.PipelineDefinitions[0].Triggers[0].EventSource.RepoFullName = "acme/web"
	m := projectManifest("gh/acme", p)

	mapping := &manifest.Mapping{
		Org:       manifest.OrgMapping{From: "circleci/src", To: "circleci/dst"},
		GitHubOrg: &manifest.OrgMapping{From: "acme", To: "acme-new"},
	}

	rep, err := sy.SyncProjects(context.Background(), m, nil, mapping, Options{Apply: true, GitHubToken: "gh-tok"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fp.hasCalled("CreatePipelineDefinition") {
		t.Error("CreatePipelineDefinition must NOT be called when repo is not found in dest GH org")
	}

	// A "manual" action must be emitted with remediation guidance.
	found := false
	for _, a := range actionsOfKind(rep, "project-ext-id") {
		if a.Status == "manual" && containsStr(a.Detail, "not found in the destination GitHub org") {
			found = true
		}
	}
	if !found {
		t.Error("expected a 'manual' project-ext-id action mentioning 'not found in the destination GitHub org'")
	}
}

// TestResolveExternalID_NoToken_OrgChanged_Manual verifies that when no token
// is provided but the GH org has changed (destFullName != sourceFullName), a
// "manual" action is emitted requiring the operator to provide a token.
func TestResolveExternalID_NoToken_OrgChanged_Manual(t *testing.T) {
	fp := &fakeProjectWriter{}
	sy := newSyncerAppProjects(fp)

	p := appManifestProject("web", "gh/acme/web", "old-id", "code_push")
	p.PipelineDefinitions[0].ConfigSource.RepoFullName = "acme/web"
	p.PipelineDefinitions[0].CheckoutSource.RepoFullName = "acme/web"
	p.PipelineDefinitions[0].Triggers[0].EventSource.RepoFullName = "acme/web"
	m := projectManifest("gh/acme", p)

	mapping := &manifest.Mapping{
		Org:       manifest.OrgMapping{From: "circleci/src", To: "circleci/dst"},
		GitHubOrg: &manifest.OrgMapping{From: "acme", To: "acme-new"},
	}

	// No GitHubToken.
	rep, err := sy.SyncProjects(context.Background(), m, nil, mapping, Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fp.hasCalled("CreatePipelineDefinition") {
		t.Error("CreatePipelineDefinition must NOT be called when org changed and no token provided")
	}

	found := false
	for _, a := range actionsOfKind(rep, "project-ext-id") {
		if a.Status == "manual" && containsStr(a.Detail, "--github-token") {
			found = true
		}
	}
	if !found {
		t.Error("expected a 'manual' project-ext-id action mentioning '--github-token'")
	}
}

// TestResolveExternalID_NoToken_OrgSame_ReusesCapturedID verifies that when no
// token is provided and the GH org has NOT changed, the captured id is reused
// silently (no ext-id action emitted).
func TestResolveExternalID_NoToken_OrgSame_ReusesCapturedID(t *testing.T) {
	var gotSpec project.PipelineDefinitionSpec
	fp := &fakeProjectWriter{
		createPipelineDefinition: func(projectID string, spec project.PipelineDefinitionSpec) (string, error) {
			gotSpec = spec
			return "def-id", nil
		},
	}
	sy := newSyncerAppProjects(fp)

	// Same GH org: fullName and destFullName are both "acme/web".
	p := appManifestProject("web", "gh/acme/web", "captured-123", "code_push")
	m := projectManifest("gh/acme", p)

	// No mapping and no DestGitHubOrg → org unchanged.
	rep, err := sy.SyncProjects(context.Background(), m, nil, nil, Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !fp.hasCalled("CreatePipelineDefinition") {
		t.Error("CreatePipelineDefinition must be called when captured id is available and org is unchanged")
	}
	if gotSpec.ConfigExternalID != "captured-123" {
		t.Errorf("ConfigExternalID: got %q, want captured-123", gotSpec.ConfigExternalID)
	}

	// No ext-id action should be emitted for same-org (silent reuse).
	extIDActions := actionsOfKind(rep, "project-ext-id")
	for _, a := range extIDActions {
		if a.Status == "manual" || a.Status == "error" {
			t.Errorf("unexpected ext-id action status %q for same-org captured id: %s", a.Status, a.Detail)
		}
	}
}

// TestResolveExternalID_DestGitHubOrg_Option verifies that Options.DestGitHubOrg
// is used to remap the owner when no Mapping.GitHubOrg is set.
func TestResolveExternalID_DestGitHubOrg_Option(t *testing.T) {
	stubResolveRepoID(t, "acme-dest/web", "777", nil)

	fp := &fakeProjectWriter{
		createPipelineDefinition: func(projectID string, spec project.PipelineDefinitionSpec) (string, error) {
			return "def-id", nil
		},
	}
	sy := newSyncerAppProjects(fp)

	p := appManifestProject("web", "gh/acme/web", "old-id", "code_push")
	p.PipelineDefinitions[0].ConfigSource.RepoFullName = "acme/web"
	p.PipelineDefinitions[0].CheckoutSource.RepoFullName = "acme/web"
	p.PipelineDefinitions[0].Triggers[0].EventSource.RepoFullName = "acme/web"
	m := projectManifest("gh/acme", p)

	// Use DestGitHubOrg option (no GitHubOrg in mapping).
	_, err := sy.SyncProjects(context.Background(), m, nil, nil, Options{
		Apply:         true,
		GitHubToken:   "gh-tok",
		DestGitHubOrg: "acme-dest",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !fp.hasCalled("CreatePipelineDefinition") {
		t.Error("CreatePipelineDefinition must be called when DestGitHubOrg resolves the repo")
	}
}

// TestResolveExternalID_MappingGitHubOrg_PrecedenceOverDestOption verifies that
// Mapping.GitHubOrg takes precedence over Options.DestGitHubOrg.
func TestResolveExternalID_MappingGitHubOrg_PrecedenceOverDestOption(t *testing.T) {
	// The mapping says "acme-mapping"; the option says "acme-option".
	// The stub should be called with "acme-mapping/web", not "acme-option/web".
	stubResolveRepoID(t, "acme-mapping/web", "888", nil)

	fp := &fakeProjectWriter{
		createPipelineDefinition: func(projectID string, spec project.PipelineDefinitionSpec) (string, error) {
			return "def-id", nil
		},
	}
	sy := newSyncerAppProjects(fp)

	p := appManifestProject("web", "gh/acme/web", "old-id", "code_push")
	p.PipelineDefinitions[0].ConfigSource.RepoFullName = "acme/web"
	p.PipelineDefinitions[0].CheckoutSource.RepoFullName = "acme/web"
	p.PipelineDefinitions[0].Triggers[0].EventSource.RepoFullName = "acme/web"
	m := projectManifest("gh/acme", p)

	mapping := &manifest.Mapping{
		GitHubOrg: &manifest.OrgMapping{From: "acme", To: "acme-mapping"},
	}

	_, err := sy.SyncProjects(context.Background(), m, nil, mapping, Options{
		Apply:         true,
		GitHubToken:   "gh-tok",
		DestGitHubOrg: "acme-option", // must be ignored
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !fp.hasCalled("CreatePipelineDefinition") {
		t.Error("CreatePipelineDefinition must be called when Mapping.GitHubOrg resolves the repo")
	}
}

// TestSyncProjects_AppDest_DryRun_ResolvePreview verifies that in dry-run
// mode, resolveExternalID is still called (read-only GitHub GET) so the
// preview accurately shows found/skipped status.
func TestSyncProjects_AppDest_DryRun_ResolvePreview(t *testing.T) {
	stubResolveRepoID(t, "acme-new/web", "555", nil)

	fp := &fakeProjectWriter{}
	sy := newSyncerAppProjects(fp)

	p := appManifestProject("web", "gh/acme/web", "old-id", "code_push")
	p.PipelineDefinitions[0].ConfigSource.RepoFullName = "acme/web"
	p.PipelineDefinitions[0].CheckoutSource.RepoFullName = "acme/web"
	p.PipelineDefinitions[0].Triggers[0].EventSource.RepoFullName = "acme/web"
	m := projectManifest("gh/acme", p)

	mapping := &manifest.Mapping{
		GitHubOrg: &manifest.OrgMapping{From: "acme", To: "acme-new"},
	}

	rep, err := sy.SyncProjects(context.Background(), m, nil, mapping, Options{Apply: false, GitHubToken: "gh-tok"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// No writes.
	if fp.hasCalled("CreateAppProject") {
		t.Error("CreateAppProject must NOT be called in dry-run mode")
	}
	if fp.hasCalled("CreatePipelineDefinition") {
		t.Error("CreatePipelineDefinition must NOT be called in dry-run mode")
	}

	// Resolve results must be visible in the report as "set" actions
	// (GitHub GET is read-only — still runs in dry-run preview).
	found := false
	for _, a := range actionsOfKind(rep, "project-ext-id") {
		if a.Status == "set" {
			found = true
		}
	}
	if !found {
		t.Error("dry-run must show 'set' ext-id actions (GitHub GET is read-only — run in dry-run)")
	}
}

// ---------------------------------------------------------------------------
// syncProjectSSHKeys tests
// ---------------------------------------------------------------------------

// bundleWithSSHKey builds a SecretBundle containing one SSH key for the given slug.
func bundleWithSSHKey(slug, fingerprint, hostname, privateKey string) *manifest.SecretBundle {
	b := manifest.NewSecretBundle()
	b.AddSSHKey(slug, manifest.BundleSSHKey{
		Fingerprint: fingerprint,
		Hostname:    hostname,
		PrivateKey:  privateKey,
	})
	return b
}

// projectWithSSHKey builds a manifest.Project with one SSHKeys entry.
func projectWithSSHKey(slug, fingerprint, hostname string) manifest.Project {
	return manifest.Project{
		Slug: slug,
		Name: slug,
		SSHKeys: []manifest.ProjectSSHKey{
			{Fingerprint: fingerprint, Hostname: hostname},
		},
	}
}

// TestSyncProjectSSHKeys_ReAddsFromBundle verifies that when a matching
// BundleSSHKey exists, AddAdditionalSSHKey is called with the correct
// arguments in apply mode.
func TestSyncProjectSSHKeys_ReAddsFromBundle(t *testing.T) {
	const slug = "gh/acme/web"
	const fp = "Cv1BbZPFHMZzCPx+1CsJqO0kRBIlOm7DEqR/jPbHnBg="
	const host = "github.com"
	const privKey = "-----BEGIN RSA PRIVATE KEY-----\nMIIEo...\n-----END RSA PRIVATE KEY-----\n"

	fp2 := &fakeProjectWriter{}
	sy := newSyncerProjects(fp2)

	p := projectWithSSHKey(slug, fp, host)
	m := projectManifest("gh/acme", p)
	bundle := bundleWithSSHKey(slug, fp, host, privKey)

	rep, err := sy.SyncProjects(context.Background(), m, bundle, nil, Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	adds := fp2.callsTo("AddAdditionalSSHKey")
	if len(adds) != 1 {
		t.Fatalf("expected 1 AddAdditionalSSHKey call, got %d", len(adds))
	}
	if adds[0].args[0] != slug {
		t.Errorf("AddAdditionalSSHKey slug: got %q want %q", adds[0].args[0], slug)
	}
	if adds[0].args[1] != host {
		t.Errorf("AddAdditionalSSHKey hostname: got %q want %q", adds[0].args[1], host)
	}
	if adds[0].args[2] != privKey {
		t.Errorf("AddAdditionalSSHKey privateKey: got %q want %q", adds[0].args[2], privKey)
	}

	sshActions := actionsOfKind(rep, "project-ssh-key")
	if len(sshActions) == 0 {
		t.Fatal("expected at least one project-ssh-key action")
	}
	if sshActions[0].Status != "set" {
		t.Errorf("action status: got %q want set", sshActions[0].Status)
	}
}

// TestSyncProjectSSHKeys_IdempotentSkipExisting verifies that a key whose
// fingerprint already exists on the destination is not added again.
func TestSyncProjectSSHKeys_IdempotentSkipExisting(t *testing.T) {
	const slug = "gh/acme/web"
	const fp = "existing-fingerprint="

	fakeWriter := &fakeProjectWriter{
		listAdditionalSSHKeys: func(s string) ([]project.SSHKeyMeta, error) {
			return []project.SSHKeyMeta{{Fingerprint: fp, Hostname: "github.com"}}, nil
		},
	}
	sy := newSyncerProjects(fakeWriter)

	p := projectWithSSHKey(slug, fp, "github.com")
	m := projectManifest("gh/acme", p)
	bundle := bundleWithSSHKey(slug, fp, "github.com", "-----BEGIN RSA PRIVATE KEY-----\nfoo\n-----END RSA PRIVATE KEY-----\n")

	rep, err := sy.SyncProjects(context.Background(), m, bundle, nil, Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fakeWriter.hasCalled("AddAdditionalSSHKey") {
		t.Error("AddAdditionalSSHKey must NOT be called when fingerprint already exists on destination")
	}

	sshActions := actionsOfKind(rep, "project-ssh-key")
	if len(sshActions) == 0 {
		t.Fatal("expected a project-ssh-key action")
	}
	if sshActions[0].Status != "exists" {
		t.Errorf("action status: got %q want exists", sshActions[0].Status)
	}
}

// TestSyncProjectSSHKeys_DryRunNoWrite verifies that in dry-run mode
// AddAdditionalSSHKey is not called but a "set" action is recorded.
func TestSyncProjectSSHKeys_DryRunNoWrite(t *testing.T) {
	const slug = "gh/acme/web"
	const fp = "dry-run-fingerprint="

	fakeWriter := &fakeProjectWriter{}
	sy := newSyncerProjects(fakeWriter)

	p := projectWithSSHKey(slug, fp, "github.com")
	m := projectManifest("gh/acme", p)
	bundle := bundleWithSSHKey(slug, fp, "github.com", "-----BEGIN RSA PRIVATE KEY-----\nfoo\n-----END RSA PRIVATE KEY-----\n")

	rep, err := sy.SyncProjects(context.Background(), m, bundle, nil, Options{Apply: false})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fakeWriter.hasCalled("AddAdditionalSSHKey") {
		t.Error("AddAdditionalSSHKey must NOT be called in dry-run mode")
	}
	if fakeWriter.hasCalled("ListAdditionalSSHKeys") {
		t.Error("ListAdditionalSSHKeys must NOT be called in dry-run mode (no writes, no idempotency check needed)")
	}

	sshActions := actionsOfKind(rep, "project-ssh-key")
	if len(sshActions) == 0 {
		t.Fatal("expected a project-ssh-key action in dry-run")
	}
	if sshActions[0].Status != "set" {
		t.Errorf("dry-run action status: got %q want set", sshActions[0].Status)
	}
}

// TestSyncProjectSSHKeys_ManualWhenKeyMissingFromBundle verifies that when a
// project has SSHKeys metadata but the bundle has no entry with that
// fingerprint, a "manual" warning is emitted.
func TestSyncProjectSSHKeys_ManualWhenKeyMissingFromBundle(t *testing.T) {
	const slug = "gh/acme/web"
	const fp = "missing-in-bundle="

	fakeWriter := &fakeProjectWriter{}
	sy := newSyncerProjects(fakeWriter)

	p := projectWithSSHKey(slug, fp, "github.com")
	m := projectManifest("gh/acme", p)
	// Bundle has no SSH keys for this project.
	bundle := manifest.NewSecretBundle()

	rep, err := sy.SyncProjects(context.Background(), m, bundle, nil, Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fakeWriter.hasCalled("AddAdditionalSSHKey") {
		t.Error("AddAdditionalSSHKey must NOT be called when private key is not in the bundle")
	}

	sshActions := actionsOfKind(rep, "project-ssh-key")
	if len(sshActions) == 0 {
		t.Fatal("expected a project-ssh-key action")
	}
	if sshActions[0].Status != "manual" {
		t.Errorf("action status: got %q want manual", sshActions[0].Status)
	}
}

// TestSyncProjectSSHKeys_ManualWhenNilBundle verifies that a nil bundle causes
// all SSH keys to be emitted as "manual" warnings.
func TestSyncProjectSSHKeys_ManualWhenNilBundle(t *testing.T) {
	const slug = "gh/acme/web"

	fakeWriter := &fakeProjectWriter{}
	sy := newSyncerProjects(fakeWriter)

	p := manifest.Project{
		Slug: slug,
		Name: slug,
		SSHKeys: []manifest.ProjectSSHKey{
			{Fingerprint: "fp1=", Hostname: "github.com"},
			{Fingerprint: "fp2=", Hostname: "bitbucket.org"},
		},
	}
	m := projectManifest("gh/acme", p)

	rep, err := sy.SyncProjects(context.Background(), m, nil, nil, Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fakeWriter.hasCalled("AddAdditionalSSHKey") {
		t.Error("AddAdditionalSSHKey must NOT be called with nil bundle")
	}

	manualCount := 0
	for _, a := range actionsOfKind(rep, "project-ssh-key") {
		if a.Status == "manual" {
			manualCount++
		}
	}
	if manualCount != 2 {
		t.Errorf("expected 2 manual ssh-key actions, got %d", manualCount)
	}
}

// TestSyncProjectSSHKeys_AddError verifies that an AddAdditionalSSHKey failure
// is recorded as an "error" action without propagating to the top-level error.
func TestSyncProjectSSHKeys_AddError(t *testing.T) {
	const slug = "gh/acme/web"
	const fp = "error-fingerprint="

	fakeWriter := &fakeProjectWriter{
		addAdditionalSSHKey: func(s, hostname, privateKey string) error {
			return errors.New("API unavailable")
		},
	}
	sy := newSyncerProjects(fakeWriter)

	p := projectWithSSHKey(slug, fp, "github.com")
	m := projectManifest("gh/acme", p)
	bundle := bundleWithSSHKey(slug, fp, "github.com", "-----BEGIN RSA PRIVATE KEY-----\nfoo\n-----END RSA PRIVATE KEY-----\n")

	rep, err := sy.SyncProjects(context.Background(), m, bundle, nil, Options{Apply: true})
	if err != nil {
		t.Fatalf("AddAdditionalSSHKey error must not propagate, got: %v", err)
	}

	hasError := false
	for _, a := range actionsOfKind(rep, "project-ssh-key") {
		if a.Status == "error" {
			hasError = true
		}
	}
	if !hasError {
		t.Error("expected an 'error' project-ssh-key action when AddAdditionalSSHKey fails")
	}
}

// TestSyncProjectSSHKeys_NoSSHKeysInManifest verifies that when a project has
// no SSHKeys, no SSH key actions are emitted and no API calls are made.
func TestSyncProjectSSHKeys_NoSSHKeysInManifest(t *testing.T) {
	fakeWriter := &fakeProjectWriter{}
	sy := newSyncerProjects(fakeWriter)

	p := simpleProject("gh/acme/web") // no SSHKeys
	m := projectManifest("gh/acme", p)

	rep, err := sy.SyncProjects(context.Background(), m, nil, nil, Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fakeWriter.hasCalled("ListAdditionalSSHKeys") {
		t.Error("ListAdditionalSSHKeys must NOT be called when project has no SSHKeys")
	}
	if fakeWriter.hasCalled("AddAdditionalSSHKey") {
		t.Error("AddAdditionalSSHKey must NOT be called when project has no SSHKeys")
	}
	if len(actionsOfKind(rep, "project-ssh-key")) != 0 {
		t.Error("expected no project-ssh-key actions when project has no SSHKeys")
	}
}

// ---------------------------------------------------------------------------
// Fix #4: Dry-run must NOT call ListEnvVars (spurious call guard)
// ---------------------------------------------------------------------------

// TestSyncProjects_DryRun_NoListEnvVars verifies that in dry-run mode
// ListEnvVars is NOT called. In dry-run mode the destination project slug
// may be a placeholder (e.g. "circleci/<org-id>/<new>") that would cause
// a doomed HTTP call, matching the pattern of syncProjectSSHKeys.
func TestSyncProjects_DryRun_NoListEnvVars_AppOrg(t *testing.T) {
	fp := &fakeProjectWriter{
		listOrgProjects: func(orgID string) ([]project.OrgProject, error) {
			return nil, nil // No existing projects → will create
		},
	}
	sy := newSyncerAppProjects(fp)

	p := simpleProject("gh/acme/web", "API_KEY")
	p.Name = "web"
	m := projectManifest("gh/acme", p)

	_, err := sy.SyncProjects(context.Background(), m, nil, nil, Options{Apply: false})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fp.hasCalled("ListEnvVars") {
		t.Error("ListEnvVars must NOT be called in dry-run mode (slug is a placeholder for new App projects)")
	}
}

// TestSyncProjects_Apply_CallsListEnvVars verifies that in apply mode
// ListEnvVars IS called (to detect already-existing vars).
func TestSyncProjects_Apply_CallsListEnvVars(t *testing.T) {
	fp := &fakeProjectWriter{}
	sy := newSyncerProjects(fp)

	srcSlug := "gh/acme/web"
	p := simpleProject(srcSlug, "API_KEY")
	m := projectManifest("gh/acme", p)
	bundle := projectBundleWith(srcSlug, "API_KEY", "val")

	_, err := sy.SyncProjects(context.Background(), m, bundle, nil, Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !fp.hasCalled("ListEnvVars") {
		t.Error("ListEnvVars must be called in apply mode for idempotency check")
	}
}

// ---------------------------------------------------------------------------
// Fix #5: Existing project emits "exists" action
// ---------------------------------------------------------------------------

// TestSyncProjects_ExistingProject_OAuth_EmitsExists verifies that when
// GetProject succeeds for an OAuth project (it already exists), a project
// action with status "exists" is recorded before the sub-resource sync.
func TestSyncProjects_ExistingProject_OAuth_EmitsExists(t *testing.T) {
	fp := &fakeProjectWriter{
		// GetProject succeeds → project already exists.
		getProject: func(slug string) (*project.Project, error) {
			return &project.Project{Slug: slug, ID: "existing-proj-id", Name: "web"}, nil
		},
	}
	sy := newSyncerProjects(fp)

	p := simpleProject("gh/acme/web", "DB_URL")
	m := projectManifest("gh/acme", p)

	rep, err := sy.SyncProjects(context.Background(), m, nil, nil, Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	a := firstActionOfKind(rep, "project")
	if a == nil {
		t.Fatal("expected a project action, got none")
	}
	if a.Status != "exists" {
		t.Errorf("project action status: got %q want %q", a.Status, "exists")
	}
}

// TestSyncProjects_ExistingProject_App_EmitsExists verifies that when
// ListOrgProjects finds a matching project, an "exists" project action is
// recorded before the sub-resource sync.
func TestSyncProjects_ExistingProject_App_EmitsExists(t *testing.T) {
	existingSlug := "circleci/dest-org-id/existing-proj"
	fp := &fakeProjectWriter{
		listOrgProjects: func(orgID string) ([]project.OrgProject, error) {
			return []project.OrgProject{
				{ID: "existing-proj-id", Slug: existingSlug, Name: "web"},
			}, nil
		},
	}
	sy := newSyncerAppProjects(fp)

	p := simpleProject("gh/acme/web")
	p.Name = "web"
	m := projectManifest("gh/acme", p)

	rep, err := sy.SyncProjects(context.Background(), m, nil, nil, Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	a := firstActionOfKind(rep, "project")
	if a == nil {
		t.Fatal("expected a project action, got none")
	}
	if a.Status != "exists" {
		t.Errorf("project action status: got %q want %q", a.Status, "exists")
	}
	if a.Target != existingSlug {
		t.Errorf("project action target: got %q want %q", a.Target, existingSlug)
	}
}

// ---------------------------------------------------------------------------
// Fix #6: resolveExternalID emits "set" (not "resolved") when repo is found
// ---------------------------------------------------------------------------

// TestResolveExternalID_ResolvedCountsInSummary verifies that a successful
// repo resolution emits status "set" (not "resolved") so it appears in the
// Counts() summary tally.
func TestResolveExternalID_ResolvedCountsInSummary(t *testing.T) {
	stubResolveRepoID(t, "acme/web", "id-123", nil)

	fp := &fakeProjectWriter{
		createPipelineDefinition: func(projectID string, spec project.PipelineDefinitionSpec) (string, error) {
			return "def-id", nil
		},
	}
	sy := newSyncerAppProjects(fp)

	p := appManifestProject("web", "gh/acme/web", "old-id", "code_push")
	m := projectManifest("gh/acme", p)

	rep, err := sy.SyncProjects(context.Background(), m, nil, nil, Options{Apply: true, GitHubToken: "gh-tok"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	counts := rep.Counts()
	if counts["set"] == 0 {
		t.Errorf("expected at least one 'set' count (resolved repo), got counts=%v", counts)
	}
	if counts["resolved"] != 0 {
		t.Errorf("expected zero 'resolved' count (status was renamed to 'set'), got %d", counts["resolved"])
	}
}

// ---------------------------------------------------------------------------
// Project API token sync tests (#132)
// ---------------------------------------------------------------------------

// projectManifestWithTokens builds a minimal project manifest with captured
// API tokens for testing.
func projectManifestWithTokens(tokens ...manifest.ProjectAPIToken) *manifest.Manifest {
	return &manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		Source: manifest.Source{
			Org: manifest.Org{Slug: "gh/src", ID: "org-src-uuid"},
		},
		Projects: []manifest.Project{
			{
				Slug:      "gh/src/web",
				Name:      "web",
				SourceID:  "proj-src-uuid",
				EnvVars:   []manifest.ProjectEnvVar{},
				APITokens: tokens,
			},
		},
	}
}

// TestSyncProjectAPITokens_FlagOff_DefaultManual verifies that when
// --create-project-tokens is false (default), each token emits a "manual" action
// and CreateProjectToken is NEVER called.
func TestSyncProjectAPITokens_FlagOff_DefaultManual(t *testing.T) {
	fp := &fakeProjectWriter{
		getProject: func(slug string) (*project.Project, error) {
			return &project.Project{Slug: slug, ID: "proj-dst-uuid", Name: "web"}, nil
		},
	}
	sy := &Syncer{
		Org:      &fakeOrgResolver{},
		Projects: fp,
	}

	m := projectManifestWithTokens(
		manifest.ProjectAPIToken{Label: "deploy-bot", Scope: "all"},
		manifest.ProjectAPIToken{Label: "status-check", Scope: "status"},
	)

	// Apply with CreateProjectTokens=false (default).
	rep, err := sy.SyncProjects(context.Background(), m, nil, mappingTo("gh/dst"), Options{Apply: true, CreateProjectTokens: false})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// CreateProjectToken must NOT have been called.
	if fp.hasCalled("CreateProjectToken") {
		t.Error("CreateProjectToken must NOT be called when --create-project-tokens is false")
	}

	// There must be exactly 2 "manual" actions for the tokens.
	manualCount := 0
	for _, a := range rep.Actions {
		if a.Kind == "project-api-token" && a.Status == "manual" {
			manualCount++
		}
	}
	if manualCount != 2 {
		t.Errorf("expected 2 manual token actions, got %d (actions: %v)", manualCount, rep.Actions)
	}
}

// TestSyncProjectAPITokens_FlagOn_Apply_Creates verifies that when
// --create-project-tokens=true AND --apply, a new token is created and the
// result is "created".
func TestSyncProjectAPITokens_FlagOn_Apply_Creates(t *testing.T) {
	var createCalls [][]string

	fp := &fakeProjectWriter{
		getProject: func(slug string) (*project.Project, error) {
			return &project.Project{Slug: slug, ID: "proj-dst-uuid", Name: "web"}, nil
		},
		listProjectTokens: func(slug string) ([]project.ProjectAPIToken, error) {
			// No existing tokens on destination.
			return nil, nil
		},
		createProjectToken: func(slug, scope, label string) (string, error) {
			createCalls = append(createCalls, []string{slug, scope, label})
			return "ccipat_PLACEHOLDER_created_value", nil
		},
	}
	sy := &Syncer{
		Org:      &fakeOrgResolver{},
		Projects: fp,
	}

	m := projectManifestWithTokens(
		manifest.ProjectAPIToken{Label: "deploy-bot", Scope: "all"},
	)

	rep, err := sy.SyncProjects(context.Background(), m, nil, mappingTo("gh/dst"), Options{Apply: true, CreateProjectTokens: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(createCalls) != 1 {
		t.Fatalf("expected 1 CreateProjectToken call, got %d", len(createCalls))
	}
	if createCalls[0][1] != "all" {
		t.Errorf("scope: got %q want all", createCalls[0][1])
	}
	if createCalls[0][2] != "deploy-bot" {
		t.Errorf("label: got %q want deploy-bot", createCalls[0][2])
	}

	// Action must be "created".
	createdCount := 0
	for _, a := range rep.Actions {
		if a.Kind == "project-api-token" && a.Status == "created" {
			createdCount++
		}
	}
	if createdCount != 1 {
		t.Errorf("expected 1 created token action, got %d", createdCount)
	}
}

// TestSyncProjectAPITokens_FlagOn_Apply_IdempotentSkip verifies that when a
// token with the same label+scope already exists on the destination,
// CreateProjectToken is NOT called (idempotent skip).
func TestSyncProjectAPITokens_FlagOn_Apply_IdempotentSkip(t *testing.T) {
	fp := &fakeProjectWriter{
		getProject: func(slug string) (*project.Project, error) {
			return &project.Project{Slug: slug, ID: "proj-dst-uuid", Name: "web"}, nil
		},
		listProjectTokens: func(slug string) ([]project.ProjectAPIToken, error) {
			// Token already exists on destination.
			return []project.ProjectAPIToken{
				{ID: "existing-tok-id", Label: "deploy-bot", Scope: "all"},
			}, nil
		},
	}
	sy := &Syncer{
		Org:      &fakeOrgResolver{},
		Projects: fp,
	}

	m := projectManifestWithTokens(
		manifest.ProjectAPIToken{Label: "deploy-bot", Scope: "all"},
	)

	rep, err := sy.SyncProjects(context.Background(), m, nil, mappingTo("gh/dst"), Options{Apply: true, CreateProjectTokens: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// CreateProjectToken must NOT have been called (idempotent skip).
	if fp.hasCalled("CreateProjectToken") {
		t.Error("CreateProjectToken must NOT be called when token already exists (idempotent skip)")
	}

	// Action must be "exists".
	existsCount := 0
	for _, a := range rep.Actions {
		if a.Kind == "project-api-token" && a.Status == "exists" {
			existsCount++
		}
	}
	if existsCount != 1 {
		t.Errorf("expected 1 exists token action, got %d (actions: %v)", existsCount, rep.Actions)
	}
}

// TestSyncProjectAPITokens_FlagOn_DryRun_NoCreate verifies that with
// --create-project-tokens=true but --apply=false (dry run), CreateProjectToken
// is NOT called and the action status is "created" (would create).
func TestSyncProjectAPITokens_FlagOn_DryRun_NoCreate(t *testing.T) {
	fp := &fakeProjectWriter{
		getProject: func(slug string) (*project.Project, error) {
			return &project.Project{Slug: slug, ID: "proj-dst-uuid", Name: "web"}, nil
		},
	}
	sy := &Syncer{
		Org:      &fakeOrgResolver{},
		Projects: fp,
	}

	m := projectManifestWithTokens(
		manifest.ProjectAPIToken{Label: "ci-token", Scope: "view-builds"},
	)

	rep, err := sy.SyncProjects(context.Background(), m, nil, mappingTo("gh/dst"), Options{Apply: false, CreateProjectTokens: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// No API calls should be made in dry-run.
	if fp.hasCalled("CreateProjectToken") {
		t.Error("CreateProjectToken must NOT be called in dry-run mode")
	}
	if fp.hasCalled("ListProjectTokens") {
		t.Error("ListProjectTokens must NOT be called in dry-run mode (no apply)")
	}

	// Action must be "created" (would create).
	createdCount := 0
	for _, a := range rep.Actions {
		if a.Kind == "project-api-token" && a.Status == "created" {
			createdCount++
		}
	}
	if createdCount != 1 {
		t.Errorf("expected 1 would-create token action, got %d", createdCount)
	}
}
