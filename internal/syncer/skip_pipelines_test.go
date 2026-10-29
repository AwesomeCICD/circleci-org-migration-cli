package syncer

import (
	"context"
	"strings"
	"testing"

	"github.com/AwesomeCICD/circleci-org-migration-cli/api/project"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
)

// ---------------------------------------------------------------------------
// --skip-pipelines
// ---------------------------------------------------------------------------

// A new project is created and configured, but no definition or trigger is
// created, nothing is queued for enable, and every one is reported as skipped.
func TestSkipPipelines_NewProject_CreatedWithoutDefinitions(t *testing.T) {
	failOnGitHub(t)
	fp := &fakeProjectWriter{
		createAppProject: func(orgID, name string) (*project.Project, error) {
			return &project.Project{ID: "new-id", Slug: "circleci/dest-org-id/new-id", Name: name}, nil
		},
	}
	sy := newSyncerAppProjects(fp)
	proj := appManifestProject("web", "gh/acme/web", "12345", "all-pushes")
	proj.EnvVars = []manifest.ProjectEnvVar{{Name: "TOKEN"}}
	m := projectManifest("gh/acme", proj)

	rep := runApply(t, sy, m, Options{Apply: true, SkipPipelines: true})

	if !fp.hasCalled("CreateAppProject") {
		t.Fatal("the project itself must still be created")
	}
	for _, method := range []string{"CreatePipelineDefinition", "CreateTrigger", "ListPipelineDefinitions"} {
		if fp.hasCalled(method) {
			t.Errorf("%s must not be called with --skip-pipelines", method)
		}
	}
	if len(rep.PendingEnable) != 0 {
		t.Errorf("no trigger may be queued for enable, got %+v", rep.PendingEnable)
	}
	if statusCount(rep, "project-pipeline-def", "skipped") != 1 || statusCount(rep, "project-trigger", "skipped") != 1 {
		t.Errorf("want 1 skipped definition + 1 skipped trigger, got %+v", rep.Actions)
	}
	for _, a := range append(actionsOfKind(rep, "project-pipeline-def"), actionsOfKind(rep, "project-trigger")...) {
		if !strings.Contains(a.Detail, "--skip-pipelines") {
			t.Errorf("detail should name the flag: %+v", a)
		}
	}
	// Other project data is still synced (the point of the flag).
	if !fp.hasCalled("UpdateSettings") && !fp.hasCalled("ListEnvVars") {
		t.Error("project settings / env vars must still be synced")
	}
}

// An OAuth project with no captured definitions would get a synthesized one;
// --skip-pipelines reports it as skipped and needs no GitHub token.
func TestSkipPipelines_OAuthSourceNoDefinitions_ReportsSynthesizedAsSkipped(t *testing.T) {
	failOnGitHub(t)
	fp := &fakeProjectWriter{
		createAppProject: func(_, name string) (*project.Project, error) {
			return &project.Project{ID: "n", Slug: "circleci/dest-org-id/n", Name: name}, nil
		},
	}
	sy := newSyncerAppProjects(fp)
	m := projectManifest("gh/acme", manifest.Project{Slug: "gh/acme/web", Name: "web"})

	rep := runApply(t, sy, m, Options{Apply: true, SkipPipelines: true})

	defs := actionsOfKind(rep, "project-pipeline-def")
	if len(defs) != 1 || defs[0].Status != "skipped" || !strings.Contains(defs[0].Target, synthDefName) {
		t.Errorf("want the synthesized %q definition reported as skipped, got %+v", synthDefName, defs)
	}
	if fp.hasCalled("CreatePipelineDefinition") {
		t.Error("no definition may be created")
	}
}

// Dry run reports the same thing and says why there are no definitions.
func TestSkipPipelines_DryRun(t *testing.T) {
	fp := &fakeProjectWriter{}
	sy := newSyncerAppProjects(fp)
	m := projectManifest("gh/acme", appManifestProject("web", "gh/acme/web", "12345", "all-pushes"))

	rep, err := sy.SyncProjects(context.Background(), m, nil, nil, Options{SkipPipelines: true})
	if err != nil {
		t.Fatal(err)
	}
	p := actionsOfKind(rep, "project")
	if len(p) == 0 || !strings.Contains(p[0].Detail, "--skip-pipelines") {
		t.Errorf("project action should mention --skip-pipelines, got %+v", p)
	}
	if statusCount(rep, "project-pipeline-def", "skipped") != 1 {
		t.Errorf("expected the definition reported as skipped: %+v", rep.Actions)
	}
}

// The ordering the flag exists for: a first run with --skip-pipelines creates
// the project, a second run without it reconciles the (now existing) project
// and creates the definition + trigger through the normal path, queued for enable.
func TestSkipPipelines_ThenFullRun_CreatesDefinitionsOnExistingProject(t *testing.T) {
	failOnGitHub(t)
	d := &destPipelines{}
	fp := d.writer() // dest project "web" exists, no definitions yet
	sy := newSyncerAppProjects(fp)
	m := projectManifest("gh/acme", appManifestProject("web", "gh/acme/web", "12345", "all-pushes"))

	first := runApply(t, sy, m, Options{Apply: true, SkipPipelines: true})
	if len(d.defSpecs) != 0 || len(d.trigSpecs) != 0 || len(first.PendingEnable) != 0 {
		t.Fatalf("skip run must create nothing: defs=%d trigs=%d pending=%d", len(d.defSpecs), len(d.trigSpecs), len(first.PendingEnable))
	}

	second := runApply(t, sy, m, Options{Apply: true})
	if len(d.defSpecs) != 1 || len(d.trigSpecs) != 1 {
		t.Fatalf("follow-up run must create the definition and trigger, got %d / %d", len(d.defSpecs), len(d.trigSpecs))
	}
	if len(second.PendingEnable) != 1 || second.PendingEnable[0].Kind != "trigger" {
		t.Errorf("follow-up run must queue the new trigger for enable, got %+v", second.PendingEnable)
	}
	if statusCount(second, "project-pipeline-def", "created") != 1 {
		t.Errorf("definition should be reported created on the follow-up run: %+v", second.Actions)
	}
}
