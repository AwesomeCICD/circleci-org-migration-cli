package syncer

import (
	"errors"
	"testing"

	"github.com/CircleCI-Public/circleci-org-migration-cli/api/project"
	"github.com/CircleCI-Public/circleci-org-migration-cli/internal/manifest"
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

	calls           []projectCall
	settingsUpdates []*project.AdvancedSettings // captures the settings arg each time UpdateSettings is called
}

func (f *fakeProjectWriter) GetProject(slug string) (*project.Project, error) {
	f.calls = append(f.calls, projectCall{"GetProject", []string{slug}})
	if f.getProject != nil {
		return f.getProject(slug)
	}
	return &project.Project{Slug: slug, ID: "proj-id-" + slug, Name: slug}, nil
}

func (f *fakeProjectWriter) CreateProjectShell(provider, org, name string) (*project.Project, error) {
	f.calls = append(f.calls, projectCall{"CreateProjectShell", []string{provider, org, name}})
	if f.createProjectShell != nil {
		return f.createProjectShell(provider, org, name)
	}
	slug := provider + "/" + org + "/" + name
	return &project.Project{Slug: slug, ID: "new-proj-id-" + name, Name: name}, nil
}

func (f *fakeProjectWriter) FollowProject(vcsType, org, repo string) (*project.FollowResult, error) {
	f.calls = append(f.calls, projectCall{"FollowProject", []string{vcsType, org, repo}})
	if f.followProject != nil {
		return f.followProject(vcsType, org, repo)
	}
	return &project.FollowResult{Followed: true}, nil
}

func (f *fakeProjectWriter) ListEnvVars(slug string) ([]project.EnvVar, error) {
	f.calls = append(f.calls, projectCall{"ListEnvVars", []string{slug}})
	if f.listEnvVars != nil {
		return f.listEnvVars(slug)
	}
	return nil, nil
}

func (f *fakeProjectWriter) CreateEnvVar(slug, name, value string) error {
	f.calls = append(f.calls, projectCall{"CreateEnvVar", []string{slug, name, value}})
	if f.createEnvVar != nil {
		return f.createEnvVar(slug, name, value)
	}
	return nil
}

func (f *fakeProjectWriter) UpdateSettings(provider, org, proj string, s *project.AdvancedSettings) error {
	f.calls = append(f.calls, projectCall{"UpdateSettings", []string{provider, org, proj}})
	f.settingsUpdates = append(f.settingsUpdates, s)
	if f.updateSettings != nil {
		return f.updateSettings(provider, org, proj, s)
	}
	return nil
}

func (f *fakeProjectWriter) ListWebhooks(projectID string) ([]project.Webhook, error) {
	f.calls = append(f.calls, projectCall{"ListWebhooks", []string{projectID}})
	if f.listWebhooks != nil {
		return f.listWebhooks(projectID)
	}
	return nil, nil
}

func (f *fakeProjectWriter) CreateWebhook(destProjectID string, w project.Webhook) error {
	f.calls = append(f.calls, projectCall{"CreateWebhook", []string{destProjectID, w.Name, w.URL}})
	if f.createWebhook != nil {
		return f.createWebhook(destProjectID, w)
	}
	return nil
}

func (f *fakeProjectWriter) ListSchedules(slug string) ([]project.Schedule, error) {
	f.calls = append(f.calls, projectCall{"ListSchedules", []string{slug}})
	if f.listSchedules != nil {
		return f.listSchedules(slug)
	}
	return nil, nil
}

func (f *fakeProjectWriter) CreateSchedule(destSlug, name, description, attributionActor string, timetable, parameters map[string]any) error {
	f.calls = append(f.calls, projectCall{"CreateSchedule", []string{destSlug, name, description, attributionActor}})
	if f.createSchedule != nil {
		return f.createSchedule(destSlug, name, description, attributionActor, timetable, parameters)
	}
	return nil
}

func (f *fakeProjectWriter) GetProjectOIDCClaims(orgID, projID string) ([]string, string, error) {
	f.calls = append(f.calls, projectCall{"GetProjectOIDCClaims", []string{orgID, projID}})
	if f.getProjectOIDCClaims != nil {
		return f.getProjectOIDCClaims(orgID, projID)
	}
	return nil, "", nil
}

func (f *fakeProjectWriter) SetProjectOIDCClaims(orgID, projID string, audience []string, ttl string) error {
	f.calls = append(f.calls, projectCall{"SetProjectOIDCClaims", []string{orgID, projID, ttl}})
	if f.setProjectOIDCClaims != nil {
		return f.setProjectOIDCClaims(orgID, projID, audience, ttl)
	}
	return nil
}

func (f *fakeProjectWriter) GetV11ProjectFeatureFlags(slug string) (map[string]bool, error) {
	f.calls = append(f.calls, projectCall{"GetV11ProjectFeatureFlags", []string{slug}})
	if f.getV11ProjectFeatureFlags != nil {
		return f.getV11ProjectFeatureFlags(slug)
	}
	return nil, nil
}

func (f *fakeProjectWriter) SetV11ProjectFeatureFlags(slug string, flags map[string]bool) error {
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
func newSyncerProjects(fp *fakeProjectWriter) *Syncer {
	return &Syncer{
		Org:      &fakeOrgResolver{},
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

	rep, err := sy.SyncProjects(m, bundle, nil, Options{Apply: true})
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

	rep, err := sy.SyncProjects(m, nil, nil, Options{Apply: false})
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

	rep, err := sy.SyncProjects(m, nil, mapping, Options{Apply: true})
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

	rep, err := sy.SyncProjects(m, nil, mapping, Options{Apply: true})
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

	rep, err := sy.SyncProjects(m, nil, nil, Options{Apply: true})
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

	rep, err := sy.SyncProjects(m, nil, nil, Options{Apply: false})
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

	rep, err := sy.SyncProjects(m, bundle, mapping, Options{Apply: true})
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

	_, err := sy.SyncProjects(m, bundle, nil, Options{Apply: false})
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

	rep, err := sy.SyncProjects(m, bundle, nil, Options{Apply: true})
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

	rep, err := sy.SyncProjects(m, bundle, nil, Options{Apply: true, MissingSecrets: MissingSkip})
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

	rep, err := sy.SyncProjects(m, bundle, nil, Options{Apply: true, MissingSecrets: MissingPlaceholder})
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

	_, err := sy.SyncProjects(m, bundle, nil, Options{Apply: false, MissingSecrets: MissingPlaceholder})
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

	rep, err := sy.SyncProjects(m, bundle, nil, Options{Apply: true})
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

	rep, err := sy.SyncProjects(m, nil, nil, Options{Apply: true})
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

	rep, err := sy.SyncProjects(m, nil, mapping, Options{Apply: false})
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

	rep, err := sy.SyncProjects(m, nil, nil, Options{Apply: false})
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

	rep, err := sy.SyncProjects(m, bundle, nil, Options{Apply: true})
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

	_, err := sy.SyncProjects(m, nil, nil, Options{Apply: true})
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

	_, err := sy.SyncProjects(m, bundle, mapping, Options{Apply: true})
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

	rep, err := sy.SyncProjects(m, nil, nil, Options{Apply: true, MissingSecrets: MissingSkip})
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

	rep, err := sy.SyncProjects(m, bundle, nil, Options{Apply: true, MissingSecrets: MissingPlaceholder})
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

	repDry, err := sy.SyncProjects(m, nil, nil, Options{Apply: false})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repDry.Applied {
		t.Error("Report.Applied should be false when Apply=false")
	}

	repApply, err := sy.SyncProjects(m, nil, nil, Options{Apply: true})
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

	rep, err := sy.SyncProjects(m, nil, mapping, Options{Apply: true})
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

	rep, err := sy.SyncProjects(m, nil, nil, Options{Apply: true})
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

	rep, err := sy.SyncProjects(m, nil, nil, Options{Apply: true})
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

	rep, err := sy.SyncProjects(m, nil, nil, Options{Apply: false})
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

	rep, err := sy.SyncProjects(m, nil, nil, Options{Apply: true})
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

	rep, err := sy.SyncProjects(m, nil, nil, Options{Apply: true})
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

	rep, err := sy.SyncProjects(m, nil, mapping, Options{Apply: true})
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

	rep, err := sy.SyncProjects(m, nil, nil, Options{Apply: true})
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

	rep, err := sy.SyncProjects(m, nil, nil, Options{Apply: false})
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

	rep, err := sy.SyncProjects(m, nil, nil, Options{Apply: true})
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

	rep, err := sy.SyncProjects(m, nil, nil, Options{Apply: true})
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

// TestSyncProjects_DropAllBuildRequests_Skipped verifies that drop_all_build_requests
// is NEVER applied and always produces a "manual" warning action.
func TestSyncProjects_DropAllBuildRequests_Skipped(t *testing.T) {
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

	rep, err := sy.SyncProjects(m, nil, nil, Options{Apply: true})
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
		t.Error("expected a 'manual' project-flag action for drop_all_build_requests")
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

	rep, err := sy.SyncProjects(m, nil, nil, Options{Apply: false})
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
// apply=true calls FollowProject and returns a "set" Action.
func TestEnableBuilds_Apply_CallsFollowProject(t *testing.T) {
	fp := &fakeProjectWriter{}
	sy := newSyncerProjects(fp)

	target := EnableTarget{Slug: "gh/acme/web", VCSType: "gh", Org: "acme", Repo: "web"}
	action, err := sy.EnableBuilds(target, true)
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
// apply=false does not call FollowProject and returns a "manual" Action.
func TestEnableBuilds_DryRun_NoFollowProject(t *testing.T) {
	fp := &fakeProjectWriter{}
	sy := newSyncerProjects(fp)

	target := EnableTarget{Slug: "gh/acme/web", VCSType: "gh", Org: "acme", Repo: "web"}
	action, err := sy.EnableBuilds(target, false)
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

	target := EnableTarget{Slug: "gh/acme/web", VCSType: "gh", Org: "acme", Repo: "web"}
	action, err := sy.EnableBuilds(target, true)
	if err == nil {
		t.Fatal("expected error from FollowProject failure, got nil")
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

	rep, err := sy.SyncProjects(m, nil, nil, Options{Apply: true})
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
