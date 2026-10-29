package syncer

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/AwesomeCICD/circleci-org-migration-cli/api/project"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
)

// ---------------------------------------------------------------------------
// Stateful fake destination: an App project whose pipeline definitions and
// triggers persist across sync runs, so re-runs can be exercised.
// ---------------------------------------------------------------------------

type destPipelines struct {
	defs        []project.PipelineDefinition
	triggers    map[string][]project.Trigger // by def id
	defSpecs    []project.PipelineDefinitionSpec
	trigSpecs   []project.TriggerSpec
	failDefOnce error // returned by the next CreatePipelineDefinition
	n           int
}

// writer returns a fakeProjectWriter whose single dest project "web" already
// exists, backed by d.
func (d *destPipelines) writer() *fakeProjectWriter {
	d.triggers = map[string][]project.Trigger{}
	return &fakeProjectWriter{
		listOrgProjects: func(string) ([]project.OrgProject, error) {
			return []project.OrgProject{{ID: "proj-1", Slug: "circleci/dest-org-id/proj-1", Name: "web"}}, nil
		},
		listPipelineDefinitions: func(string) ([]project.PipelineDefinition, error) {
			return append([]project.PipelineDefinition(nil), d.defs...), nil
		},
		listTriggers: func(_, defID string) ([]project.Trigger, error) {
			return append([]project.Trigger(nil), d.triggers[defID]...), nil
		},
		createPipelineDefinition: func(_ string, spec project.PipelineDefinitionSpec) (string, error) {
			if d.failDefOnce != nil {
				err := d.failDefOnce
				d.failDefOnce = nil
				return "", err
			}
			d.n++
			id := fmt.Sprintf("def-%d", d.n)
			d.defSpecs = append(d.defSpecs, spec)
			d.defs = append(d.defs, project.PipelineDefinition{ID: id, Name: spec.Name})
			return id, nil
		},
		createTrigger: func(_, defID string, spec project.TriggerSpec) (string, error) {
			d.n++
			id := fmt.Sprintf("trig-%d", d.n)
			d.trigSpecs = append(d.trigSpecs, spec)
			t := project.Trigger{
				ID: id, Name: "server-assigned-" + id, Disabled: spec.Disabled,
				EventPreset: spec.EventPreset, EventName: spec.EventName,
				CheckoutRef: spec.CheckoutRef, ConfigRef: spec.ConfigRef,
				EventSource: project.TriggerEventSource{Provider: spec.Provider},
			}
			t.EventSource.Webhook.Sender = spec.WebhookSender
			t.EventSource.Schedule.CronExpression = spec.CronExpression
			d.triggers[defID] = append(d.triggers[defID], t)
			return id, nil
		},
		enableTrigger: func(_, triggerID string) error {
			for def, ts := range d.triggers {
				for i := range ts {
					if ts[i].ID == triggerID {
						d.triggers[def][i].Disabled = false
					}
				}
			}
			return nil
		},
	}
}

// failOnGitHub fails the test if the GitHub API is consulted.
func failOnGitHub(t *testing.T) {
	t.Helper()
	orig := resolveRepoID
	t.Cleanup(func() { resolveRepoID = orig })
	resolveRepoID = func(_ context.Context, fullName, _, _ string) (string, error) {
		t.Errorf("GitHub API must not be called, but resolveRepoID(%q) was", fullName)
		return "", errors.New("unexpected GitHub call")
	}
}

func runApply(t *testing.T, sy *Syncer, m *manifest.Manifest, opts Options) *Report {
	t.Helper()
	rep, err := sy.SyncProjects(context.Background(), m, nil, nil, opts)
	if err != nil {
		t.Fatalf("SyncProjects: %v", err)
	}
	return rep
}

func statusCount(rep *Report, kind, status string) int {
	n := 0
	for _, a := range actionsOfKind(rep, kind) {
		if a.Status == status {
			n++
		}
	}
	return n
}

// enableAll runs the enable-builds step for everything the report queued.
func enableAll(t *testing.T, sy *Syncer, rep *Report) {
	t.Helper()
	for _, tgt := range rep.PendingEnable {
		if _, err := sy.EnableBuilds(context.Background(), tgt, true); err != nil {
			t.Fatalf("EnableBuilds: %v", err)
		}
	}
}

// ---------------------------------------------------------------------------
// A. Reconcile existing projects
// ---------------------------------------------------------------------------

// TestReconcile_ExistingProject_CreatesMissingAndEnables: the destination
// project exists but has no pipeline definitions; sync creates the definition
// and trigger and queues the trigger for enable-builds — without a GitHub token.
func TestReconcile_ExistingProject_CreatesMissingAndEnables(t *testing.T) {
	failOnGitHub(t)
	d := &destPipelines{}
	fp := d.writer()
	sy := newSyncerAppProjects(fp)
	m := projectManifest("gh/acme", appManifestProject("web", "gh/acme/web", "12345", "all-pushes"))

	rep := runApply(t, sy, m, Options{Apply: true})

	if len(d.defSpecs) != 1 || len(d.trigSpecs) != 1 {
		t.Fatalf("want 1 def + 1 trigger created, got %d / %d", len(d.defSpecs), len(d.trigSpecs))
	}
	if fp.hasCalled("CreateAppProject") {
		t.Error("existing project must not be re-created")
	}
	if len(rep.PendingEnable) != 1 || rep.PendingEnable[0].Kind != "trigger" || rep.PendingEnable[0].ProjectID != "proj-1" {
		t.Fatalf("expected the new trigger queued for enable, got %+v", rep.PendingEnable)
	}
	enableAll(t, sy, rep)
	if !fp.hasCalled("EnableTrigger") {
		t.Error("EnableTrigger must be called on --yes")
	}
}

// TestReconcile_SecondRun_IsNoOp: after a full run (create + enable) a second
// run changes nothing — every def/trigger reports "exists" and nothing is queued.
func TestReconcile_SecondRun_IsNoOp(t *testing.T) {
	failOnGitHub(t)
	d := &destPipelines{}
	fp := d.writer()
	sy := newSyncerAppProjects(fp)
	m := projectManifest("gh/acme", appManifestProject("web", "gh/acme/web", "12345", "all-pushes"))

	enableAll(t, sy, runApply(t, sy, m, Options{Apply: true}))
	before := len(fp.callsTo("CreatePipelineDefinition")) + len(fp.callsTo("CreateTrigger"))

	rep2 := runApply(t, sy, m, Options{Apply: true})

	after := len(fp.callsTo("CreatePipelineDefinition")) + len(fp.callsTo("CreateTrigger"))
	if after != before {
		t.Errorf("second run created %d more def/trigger(s); want 0", after-before)
	}
	if len(rep2.PendingEnable) != 0 {
		t.Errorf("second run must queue nothing, got %+v", rep2.PendingEnable)
	}
	if got := statusCount(rep2, "project-pipeline-def", "exists"); got != 1 {
		t.Errorf("want 1 exists def action, got %d", got)
	}
	if got := statusCount(rep2, "project-trigger", "exists"); got != 1 {
		t.Errorf("want 1 exists trigger action, got %d", got)
	}
	if statusCount(rep2, "project-pipeline-def", "created")+statusCount(rep2, "project-trigger", "created") != 0 {
		t.Error("second run must report nothing as created")
	}
}

// TestReconcile_RerunEnablesExistingDisabledTrigger: a previous run created the
// trigger disabled but never reached the enable step; a re-run queues it so
// `sync --apply --yes` enables builds.
func TestReconcile_RerunEnablesExistingDisabledTrigger(t *testing.T) {
	failOnGitHub(t)
	d := &destPipelines{}
	fp := d.writer()
	sy := newSyncerAppProjects(fp)
	m := projectManifest("gh/acme", appManifestProject("web", "gh/acme/web", "12345", "all-pushes"))

	runApply(t, sy, m, Options{Apply: true}) // creates disabled; enable step skipped

	rep2 := runApply(t, sy, m, Options{Apply: true})
	if len(rep2.PendingEnable) != 1 || rep2.PendingEnable[0].TriggerID != "trig-2" {
		t.Fatalf("re-run must queue the existing disabled trigger, got %+v", rep2.PendingEnable)
	}
	if len(d.trigSpecs) != 1 {
		t.Errorf("re-run must not create a duplicate trigger, got %d", len(d.trigSpecs))
	}
	enableAll(t, sy, rep2)
	if d.triggers["def-1"][0].Disabled {
		t.Error("trigger should be enabled after the re-run's enable step")
	}
	// ...and a third run is again a no-op.
	if rep3 := runApply(t, sy, m, Options{Apply: true}); len(rep3.PendingEnable) != 0 {
		t.Errorf("third run must queue nothing, got %+v", rep3.PendingEnable)
	}
}

// TestReconcile_HalfCreatedProject_Repaired: the first run created the project
// but the definition failed (repo not yet connected to the GitHub App); once the
// repo is connected a re-run creates the definition + trigger.
func TestReconcile_HalfCreatedProject_Repaired(t *testing.T) {
	failOnGitHub(t)
	d := &destPipelines{failDefOnce: errors.New("Installation does not have access to repository")}
	fp := d.writer()
	sy := newSyncerAppProjects(fp)
	m := projectManifest("gh/acme", appManifestProject("web", "gh/acme/web", "12345", "all-pushes"))

	rep1 := runApply(t, sy, m, Options{Apply: true})
	if statusCount(rep1, "project-pipeline-def", "manual") != 1 || len(d.defs) != 0 {
		t.Fatalf("first run should leave the def as manual: %+v", rep1.Actions)
	}
	if len(rep1.PendingEnable) != 0 {
		t.Fatalf("nothing to enable after a failed def, got %+v", rep1.PendingEnable)
	}

	rep2 := runApply(t, sy, m, Options{Apply: true})
	if len(d.defs) != 1 || len(d.trigSpecs) != 1 {
		t.Fatalf("re-run must create the def and trigger, got %d / %d", len(d.defs), len(d.trigSpecs))
	}
	if len(rep2.PendingEnable) != 1 {
		t.Errorf("re-run must queue the new trigger for enable, got %+v", rep2.PendingEnable)
	}
}

// TestReconcile_DefExistsTriggerMissing: only the missing trigger is created.
func TestReconcile_DefExistsTriggerMissing(t *testing.T) {
	failOnGitHub(t)
	d := &destPipelines{}
	d.defs = []project.PipelineDefinition{{ID: "def-x", Name: "default"}}
	fp := d.writer()
	sy := newSyncerAppProjects(fp)
	m := projectManifest("gh/acme", appManifestProject("web", "gh/acme/web", "12345", "all-pushes"))

	rep := runApply(t, sy, m, Options{Apply: true})

	if fp.hasCalled("CreatePipelineDefinition") {
		t.Error("existing definition must not be re-created")
	}
	if len(d.trigSpecs) != 1 {
		t.Errorf("want the missing trigger created, got %d", len(d.trigSpecs))
	}
	if len(rep.PendingEnable) != 1 || rep.PendingEnable[0].TriggerID == "" {
		t.Errorf("created trigger must be queued, got %+v", rep.PendingEnable)
	}
}

// TestReconcile_TriggerMatchedByFieldsWhenNameDiffers: CircleCI assigns trigger
// names, so a destination trigger with a different name but the same defining
// fields counts as existing (no duplicate).
func TestReconcile_TriggerMatchedByFieldsWhenNameDiffers(t *testing.T) {
	failOnGitHub(t)
	d := &destPipelines{}
	d.defs = []project.PipelineDefinition{{ID: "def-x", Name: "default"}}
	fp := d.writer()
	d.triggers["def-x"] = []project.Trigger{{
		ID: "t-1", Name: "something-else", Disabled: false, EventPreset: "all-pushes",
		EventSource: project.TriggerEventSource{Provider: "github_app"},
	}}
	sy := newSyncerAppProjects(fp)
	m := projectManifest("gh/acme", appManifestProject("web", "gh/acme/web", "12345", "all-pushes"))

	rep := runApply(t, sy, m, Options{Apply: true})
	if fp.hasCalled("CreateTrigger") {
		t.Error("equivalent trigger already exists; must not create another")
	}
	if len(rep.PendingEnable) != 0 {
		t.Errorf("already-enabled trigger must not be queued, got %+v", rep.PendingEnable)
	}
}

// TestReconcile_DryRun_ReportsWouldCreateAndExists: a dry run on an existing
// project writes nothing and accurately distinguishes "would create" from
// "exists".
func TestReconcile_DryRun_ReportsWouldCreateAndExists(t *testing.T) {
	failOnGitHub(t)
	d := &destPipelines{}
	d.defs = []project.PipelineDefinition{{ID: "def-x", Name: "default"}}
	fp := d.writer()
	sy := newSyncerAppProjects(fp)

	p := appManifestProject("web", "gh/acme/web", "12345", "all-pushes")
	p.PipelineDefinitions = append(p.PipelineDefinitions, manifest.PipelineDefinition{
		Name:           "release",
		ConfigSource:   manifest.PipelineSource{Provider: "github_app", RepoFullName: "acme/web", RepoExternalID: "12345", FilePath: ".circleci/release.yml"},
		CheckoutSource: manifest.PipelineSource{Provider: "github_app", RepoFullName: "acme/web", RepoExternalID: "12345"},
	})
	m := projectManifest("gh/acme", p)

	rep := runApply(t, sy, m, Options{Apply: false})

	for _, c := range []string{"CreatePipelineDefinition", "CreateTrigger", "EnableTrigger"} {
		if fp.hasCalled(c) {
			t.Errorf("dry run must not call %s", c)
		}
	}
	var sawExists, sawWould bool
	for _, a := range actionsOfKind(rep, "project-pipeline-def") {
		if strings.HasSuffix(a.Target, "def:default") && a.Status == "exists" {
			sawExists = true
		}
		if strings.HasSuffix(a.Target, "def:release") && a.Status == "created" && strings.Contains(a.Detail, "would create") {
			sawWould = true
		}
	}
	if !sawExists || !sawWould {
		t.Errorf("dry run should show default=exists and release=would create: %+v", rep.Actions)
	}
	if len(rep.PendingEnable) != 0 {
		t.Errorf("dry run must not queue enables, got %+v", rep.PendingEnable)
	}
}

// TestReconcile_DisabledInSource_StaysDisabled: a trigger disabled in the source
// is created disabled and is not queued for enable.
func TestReconcile_DisabledInSource_StaysDisabled(t *testing.T) {
	failOnGitHub(t)
	d := &destPipelines{}
	fp := d.writer()
	sy := newSyncerAppProjects(fp)
	p := appManifestProject("web", "gh/acme/web", "12345", "all-pushes")
	p.PipelineDefinitions[0].Triggers[0].Disabled = true
	m := projectManifest("gh/acme", p)

	rep := runApply(t, sy, m, Options{Apply: true})
	if len(d.trigSpecs) != 1 || !d.trigSpecs[0].Disabled {
		t.Fatalf("trigger must be created disabled: %+v", d.trigSpecs)
	}
	if len(rep.PendingEnable) != 0 {
		t.Errorf("source-disabled trigger must not be queued, got %+v", rep.PendingEnable)
	}
	// Re-run: dest disabled + source disabled → still not queued.
	if rep2 := runApply(t, sy, m, Options{Apply: true}); len(rep2.PendingEnable) != 0 {
		t.Errorf("re-run must not queue a source-disabled trigger, got %+v", rep2.PendingEnable)
	}
}

// TestReconcile_ListDefsError_RecordedAndSkipped: a failure listing destination
// definitions is surfaced and nothing is created blindly.
func TestReconcile_ListDefsError_RecordedAndSkipped(t *testing.T) {
	d := &destPipelines{}
	fp := d.writer()
	fp.listPipelineDefinitions = func(string) ([]project.PipelineDefinition, error) { return nil, errors.New("boom") }
	sy := newSyncerAppProjects(fp)
	m := projectManifest("gh/acme", appManifestProject("web", "gh/acme/web", "12345", "all-pushes"))

	rep := runApply(t, sy, m, Options{Apply: true})
	if statusCount(rep, "project-pipeline-def", "error") != 1 {
		t.Errorf("want an error action: %+v", rep.Actions)
	}
	if fp.hasCalled("CreatePipelineDefinition") {
		t.Error("must not create when the destination state is unknown")
	}
}

// ---------------------------------------------------------------------------
// B. Faithful recreation
// ---------------------------------------------------------------------------

// TestRecreate_OAuthProviderTranslatedAndFieldsPreserved: a github_oauth
// definition/trigger becomes github_app and every field the API accepts is sent.
func TestRecreate_OAuthProviderTranslatedAndFieldsPreserved(t *testing.T) {
	failOnGitHub(t)
	d := &destPipelines{}
	sy := newSyncerAppProjects(d.writer())

	p := manifest.Project{
		Slug: "gh/acme/web", Name: "web",
		PipelineDefinitions: []manifest.PipelineDefinition{{
			Name:           "release",
			Description:    "Runs on tags",
			ConfigSource:   manifest.PipelineSource{Provider: "github_oauth", RepoFullName: "acme/web", RepoExternalID: "777", FilePath: ".circleci/release.yml"},
			CheckoutSource: manifest.PipelineSource{Provider: "github_oauth", RepoFullName: "acme/web", RepoExternalID: "777"},
			Triggers: []manifest.Trigger{{
				Name:        "tags",
				Description: "only tags",
				EventPreset: "only-tags",
				CheckoutRef: "refs/heads/main",
				ConfigRef:   "refs/heads/main",
				EventSource: manifest.TriggerEventSource{Provider: "github_oauth", RepoFullName: "acme/web", RepoExternalID: "777"},
			}},
		}},
	}
	rep := runApply(t, sy, projectManifest("gh/acme", p), Options{Apply: true})

	if len(d.defSpecs) != 1 {
		t.Fatalf("want 1 def, got %d: %+v", len(d.defSpecs), rep.Actions)
	}
	ds := d.defSpecs[0]
	if ds.ConfigProvider != "github_app" || ds.CheckoutProvider != "github_app" {
		t.Errorf("providers must be translated to github_app, got config=%q checkout=%q", ds.ConfigProvider, ds.CheckoutProvider)
	}
	if ds.ConfigExternalID != "777" || ds.CheckoutExternalID != "777" {
		t.Errorf("captured external ids must be reused, got %q / %q", ds.ConfigExternalID, ds.CheckoutExternalID)
	}
	if ds.ConfigFilePath != ".circleci/release.yml" {
		t.Errorf("custom file_path must be preserved, got %q", ds.ConfigFilePath)
	}
	if ds.Description != "Runs on tags" || ds.Name != "release" {
		t.Errorf("name/description must be preserved: %+v", ds)
	}
	if len(d.trigSpecs) != 1 {
		t.Fatalf("want 1 trigger, got %d", len(d.trigSpecs))
	}
	ts := d.trigSpecs[0]
	want := project.TriggerSpec{
		Provider: "github_app", ExternalID: "777", EventPreset: "only-tags",
		CheckoutRef: "refs/heads/main", ConfigRef: "refs/heads/main", Disabled: true,
	}
	if fmt.Sprintf("%+v", ts) != fmt.Sprintf("%+v", want) {
		t.Errorf("trigger spec:\n got  %+v\n want %+v", ts, want)
	}
}

// TestRecreate_GitHubServerProviderKept: github_server is not rewritten.
func TestRecreate_GitHubServerProviderKept(t *testing.T) {
	failOnGitHub(t)
	d := &destPipelines{}
	sy := newSyncerAppProjects(d.writer())
	p := appManifestProject("web", "circleci/o/p", "42", "all-pushes")
	p.PipelineDefinitions[0].ConfigSource.Provider = "github_server"
	p.PipelineDefinitions[0].CheckoutSource.Provider = "github_server"
	p.PipelineDefinitions[0].Triggers[0].EventSource.Provider = "github_server"
	runApply(t, sy, projectManifest("circleci/o", p), Options{Apply: true})
	if d.defSpecs[0].ConfigProvider != "github_server" || d.trigSpecs[0].Provider != "github_server" {
		t.Errorf("github_server must be preserved: %+v / %+v", d.defSpecs[0], d.trigSpecs[0])
	}
}

// TestRecreate_UnsupportedConfigProvider_Manual: an unknown provider is reported
// rather than guessed at.
func TestRecreate_UnsupportedConfigProvider_Manual(t *testing.T) {
	failOnGitHub(t)
	d := &destPipelines{}
	fp := d.writer()
	sy := newSyncerAppProjects(fp)
	p := appManifestProject("web", "circleci/o/p", "42", "all-pushes")
	p.PipelineDefinitions[0].ConfigSource.Provider = "bitbucket_dc"
	rep := runApply(t, sy, projectManifest("circleci/o", p), Options{Apply: true})
	if statusCount(rep, "project-pipeline-def", "manual") != 1 || fp.hasCalled("CreatePipelineDefinition") {
		t.Errorf("unsupported provider must be manual and not created: %+v", rep.Actions)
	}
}

// TestRecreate_CircleCIConfigSource_NoRepo: the repo-less circleci config source
// needs no external id.
func TestRecreate_CircleCIConfigSource_NoRepo(t *testing.T) {
	failOnGitHub(t)
	d := &destPipelines{}
	sy := newSyncerAppProjects(d.writer())
	p := appManifestProject("web", "circleci/o/p", "42", "all-pushes")
	p.PipelineDefinitions[0].ConfigSource = manifest.PipelineSource{Provider: "circleci", FilePath: ".circleci/config.yml"}
	runApply(t, sy, projectManifest("circleci/o", p), Options{Apply: true})
	if len(d.defSpecs) != 1 || d.defSpecs[0].ConfigProvider != "circleci" || d.defSpecs[0].ConfigExternalID != "" {
		t.Errorf("circleci config source must be created without a repo: %+v", d.defSpecs)
	}
}

// TestRecreate_WebhookTrigger: recreated with sender/event_name/refs, plus a
// manual note that the URL/secret changed.
func TestRecreate_WebhookTrigger(t *testing.T) {
	failOnGitHub(t)
	d := &destPipelines{}
	sy := newSyncerAppProjects(d.writer())
	p := appManifestProject("web", "circleci/o/p", "42", "all-pushes")
	p.PipelineDefinitions[0].Triggers = []manifest.Trigger{{
		Name: "datadog", EventName: "monitor-alert", CheckoutRef: "main", ConfigRef: "main",
		EventSource: manifest.TriggerEventSource{Provider: "webhook", WebhookSender: "datadog"},
	}}
	rep := runApply(t, sy, projectManifest("circleci/o", p), Options{Apply: true})

	want := project.TriggerSpec{Provider: "webhook", WebhookSender: "datadog", EventName: "monitor-alert", CheckoutRef: "main", ConfigRef: "main", Disabled: true}
	if len(d.trigSpecs) != 1 || fmt.Sprintf("%+v", d.trigSpecs[0]) != fmt.Sprintf("%+v", want) {
		t.Fatalf("webhook spec: got %+v want %+v", d.trigSpecs, want)
	}
	var manual bool
	for _, a := range actionsOfKind(rep, "project-trigger") {
		if a.Status == "manual" && strings.Contains(a.Detail, "NEW URL and secret") {
			manual = true
		}
	}
	if !manual {
		t.Errorf("expected a manual note about the new webhook secret: %+v", rep.Actions)
	}
}

// TestRecreate_WebhookMissingRequired_Manual: required fields not captured → manual.
func TestRecreate_WebhookMissingRequired_Manual(t *testing.T) {
	failOnGitHub(t)
	d := &destPipelines{}
	fp := d.writer()
	sy := newSyncerAppProjects(fp)
	p := appManifestProject("web", "circleci/o/p", "42", "all-pushes")
	p.PipelineDefinitions[0].Triggers = []manifest.Trigger{{
		Name: "hook", EventSource: manifest.TriggerEventSource{Provider: "webhook"},
	}}
	rep := runApply(t, sy, projectManifest("circleci/o", p), Options{Apply: true})
	if fp.hasCalled("CreateTrigger") || statusCount(rep, "project-trigger", "manual") != 1 {
		t.Errorf("incomplete webhook trigger must be manual: %+v", rep.Actions)
	}
}

// TestRecreate_ScheduleTrigger: cron, event name, refs, parameters sent; a
// captured user-id actor falls back to "system".
func TestRecreate_ScheduleTrigger(t *testing.T) {
	failOnGitHub(t)
	d := &destPipelines{}
	sy := newSyncerAppProjects(d.writer())
	p := appManifestProject("web", "circleci/o/p", "42", "all-pushes")
	p.PipelineDefinitions[0].Triggers = []manifest.Trigger{{
		Name: "nightly", EventName: "nightly-build", CheckoutRef: "main", ConfigRef: "main",
		Parameters: map[string]any{"deploy_env": "staging"},
		EventSource: manifest.TriggerEventSource{
			Provider: "schedule", ScheduleCron: "0 9 * * 1-5",
			ScheduleActor: "8a3f0000-0000-0000-0000-000000000001",
		},
	}}
	runApply(t, sy, projectManifest("circleci/o", p), Options{Apply: true})

	if len(d.trigSpecs) != 1 {
		t.Fatalf("want 1 schedule trigger, got %d", len(d.trigSpecs))
	}
	ts := d.trigSpecs[0]
	if ts.Provider != "schedule" || ts.CronExpression != "0 9 * * 1-5" || ts.EventName != "nightly-build" ||
		ts.CheckoutRef != "main" || ts.ConfigRef != "main" || ts.AttributionActor != "system" ||
		ts.Parameters["deploy_env"] != "staging" || !ts.Disabled {
		t.Errorf("schedule spec: %+v", ts)
	}
}

// TestRecreate_UnknownTriggerProvider_Manual.
func TestRecreate_UnknownTriggerProvider_Manual(t *testing.T) {
	failOnGitHub(t)
	d := &destPipelines{}
	fp := d.writer()
	sy := newSyncerAppProjects(fp)
	p := appManifestProject("web", "circleci/o/p", "42", "all-pushes")
	p.PipelineDefinitions[0].Triggers[0].EventSource.Provider = "gitlab"
	rep := runApply(t, sy, projectManifest("circleci/o", p), Options{Apply: true})
	if fp.hasCalled("CreateTrigger") || statusCount(rep, "project-trigger", "manual") != 1 {
		t.Errorf("unknown provider must be manual: %+v", rep.Actions)
	}
}

// TestRecreate_NewProject_SameFlow: the new-project path uses the same code and
// reuses the captured id with no token.
func TestRecreate_NewProject_CapturedIDNoToken(t *testing.T) {
	failOnGitHub(t)
	fp := &fakeProjectWriter{}
	var spec project.PipelineDefinitionSpec
	fp.createPipelineDefinition = func(_ string, s project.PipelineDefinitionSpec) (string, error) { spec = s; return "d", nil }
	sy := newSyncerAppProjects(fp)
	runApply(t, sy, projectManifest("gh/acme", appManifestProject("web", "gh/acme/web", "999", "all-pushes")), Options{Apply: true})
	if spec.ConfigExternalID != "999" || spec.CheckoutExternalID != "999" {
		t.Errorf("captured id must be reused without a token: %+v", spec)
	}
}

// TestRecreate_TokenPresentSameOrg_StillNoGitHubCall: even with a token, an
// unchanged GitHub org reuses the captured id (no API call).
func TestRecreate_TokenPresentSameOrg_StillNoGitHubCall(t *testing.T) {
	failOnGitHub(t)
	d := &destPipelines{}
	sy := newSyncerAppProjects(d.writer())
	runApply(t, sy, projectManifest("gh/acme", appManifestProject("web", "gh/acme/web", "999", "all-pushes")),
		Options{Apply: true, GitHubToken: "tok"})
	if len(d.defSpecs) != 1 || d.defSpecs[0].ConfigExternalID != "999" {
		t.Errorf("want captured id 999 reused: %+v", d.defSpecs)
	}
}

// TestRecreate_OrgChanged_ResolvesViaGitHub: org change → API lookup of the new id.
func TestRecreate_OrgChanged_ResolvesViaGitHub(t *testing.T) {
	stubResolveRepoID(t, "acme-new/web", "5555", nil)
	d := &destPipelines{}
	sy := newSyncerAppProjects(d.writer())
	runApply(t, sy, projectManifest("gh/acme", appManifestProject("web", "gh/acme/web", "999", "all-pushes")),
		Options{Apply: true, GitHubToken: "tok", DestGitHubOrg: "acme-new"})
	if d.defSpecs[0].ConfigExternalID != "5555" || d.trigSpecs[0].ExternalID != "5555" {
		t.Errorf("want resolved id 5555: %+v / %+v", d.defSpecs[0], d.trigSpecs[0])
	}
}

// TestRecreate_OrgChangedNoToken_Manual.
func TestRecreate_OrgChangedNoToken_Manual(t *testing.T) {
	failOnGitHub(t)
	d := &destPipelines{}
	fp := d.writer()
	sy := newSyncerAppProjects(fp)
	rep := runApply(t, sy, projectManifest("gh/acme", appManifestProject("web", "gh/acme/web", "999", "all-pushes")),
		Options{Apply: true, DestGitHubOrg: "acme-new"})
	if fp.hasCalled("CreatePipelineDefinition") || statusCount(rep, "project-ext-id", "manual") == 0 {
		t.Errorf("org change without token must be manual: %+v", rep.Actions)
	}
}

// ---------------------------------------------------------------------------
// Synthesis (OAuth source with no captured definitions)
// ---------------------------------------------------------------------------

func oauthOnlyProject() manifest.Project {
	return manifest.Project{Slug: "gh/acme/web", Name: "web"}
}

// TestSynth_NoToken_ManualWithTokenHint: nothing captured and no token → manual
// naming the token as the fix; nothing created.
func TestSynth_NoToken_ManualWithTokenHint(t *testing.T) {
	failOnGitHub(t)
	d := &destPipelines{}
	fp := d.writer()
	sy := newSyncerAppProjects(fp)
	rep := runApply(t, sy, projectManifest("gh/acme", oauthOnlyProject()), Options{Apply: true})

	if fp.hasCalled("CreatePipelineDefinition") {
		t.Error("must not create a definition without a repo id")
	}
	var hint bool
	for _, a := range actionsOfKind(rep, "project-ext-id") {
		if a.Status == "manual" && strings.Contains(a.Detail, "--github-token") {
			hint = true
		}
	}
	if !hint {
		t.Errorf("manual action must name --github-token: %+v", rep.Actions)
	}
}

// TestSynth_WithToken_ExistingProject_CreatedOnceThenNoOp.
func TestSynth_WithToken_ExistingProject_CreatedOnceThenNoOp(t *testing.T) {
	stubResolveRepoID(t, "acme/web", "321", nil)
	d := &destPipelines{}
	fp := d.writer()
	sy := newSyncerAppProjects(fp)
	m := projectManifest("gh/acme", oauthOnlyProject())

	rep := runApply(t, sy, m, Options{Apply: true, GitHubToken: "tok"})
	if len(d.defSpecs) != 1 || d.defSpecs[0].Name != synthDefName || d.defSpecs[0].ConfigExternalID != "321" {
		t.Fatalf("synthesized def: %+v", d.defSpecs)
	}
	if len(rep.PendingEnable) != 1 {
		t.Fatalf("synthesized trigger must be queued: %+v", rep.PendingEnable)
	}
	enableAll(t, sy, rep)

	rep2 := runApply(t, sy, m, Options{Apply: true, GitHubToken: "tok"})
	if len(d.defSpecs) != 1 || len(d.trigSpecs) != 1 || len(rep2.PendingEnable) != 0 {
		t.Errorf("second synth run must be a no-op: defs=%d trigs=%d pending=%d", len(d.defSpecs), len(d.trigSpecs), len(rep2.PendingEnable))
	}
}

// TestSynth_ExistingProjectWithOwnDefinitions_Untouched.
func TestSynth_ExistingProjectWithOwnDefinitions_Untouched(t *testing.T) {
	failOnGitHub(t)
	d := &destPipelines{}
	d.defs = []project.PipelineDefinition{{ID: "mine", Name: "custom"}}
	fp := d.writer()
	sy := newSyncerAppProjects(fp)
	rep := runApply(t, sy, projectManifest("gh/acme", oauthOnlyProject()), Options{Apply: true, GitHubToken: "tok"})
	if fp.hasCalled("CreatePipelineDefinition") {
		t.Error("must not stack a synthesized definition on a project with its own")
	}
	if statusCount(rep, "project-pipeline-def", "exists") != 1 {
		t.Errorf("want an exists action: %+v", rep.Actions)
	}
}

func TestCountSynthesizedProjects(t *testing.T) {
	m := projectManifest("gh/acme",
		oauthOnlyProject(),
		appManifestProject("app", "gh/acme/app", "1", "all-pushes"), // has captured defs
		manifest.Project{Slug: "circleci/o/p", Name: "p"},           // App source, not synthesized
		manifest.Project{Slug: "github/acme/other", Name: "other"},
	)
	if got := CountSynthesizedProjects(m); got != 2 {
		t.Errorf("CountSynthesizedProjects = %d, want 2", got)
	}
}
