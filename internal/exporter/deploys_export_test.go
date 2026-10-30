package exporter_test

import (
	"context"
	"errors"
	"testing"

	"github.com/AwesomeCICD/circleci-org-migration-cli/api/org"
	"github.com/AwesomeCICD/circleci-org-migration-cli/api/project"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/exporter"
)

type fakeDeployAPI struct {
	envs      []org.DeployEnvironment
	envsErr   error
	envDetail map[string]*org.DeployEnvironment
	comps     []org.DeployComponent
	settings  map[string]*org.DeploySettings
}

func (f *fakeDeployAPI) ListDeployEnvironments(context.Context, string) ([]org.DeployEnvironment, error) {
	return f.envs, f.envsErr
}

func (f *fakeDeployAPI) GetDeployEnvironment(_ context.Context, id string) (*org.DeployEnvironment, error) {
	if d, ok := f.envDetail[id]; ok {
		return d, nil
	}
	return nil, errors.New("not found")
}

func (f *fakeDeployAPI) ListDeployComponents(context.Context, string) ([]org.DeployComponent, error) {
	return f.comps, nil
}

func (f *fakeDeployAPI) GetDeploySettings(_ context.Context, projectID string) (*org.DeploySettings, error) {
	if s, ok := f.settings[projectID]; ok {
		return s, nil
	}
	return &org.DeploySettings{ProjectID: projectID}, nil
}

func deployExporter(d *fakeDeployAPI) *exporter.Exporter {
	ex := minimalExporter()
	ex.Projects = &fakeProjectAPI{
		getProject: func(slug string) (*project.Project, error) {
			return &project.Project{Slug: slug, ID: "proj-web-uuid", Name: "web"}, nil
		},
	}
	ex.Deploy = d
	return ex
}

func TestExport_Deploys_CapturesEnvsComponentsSettings(t *testing.T) {
	d := &fakeDeployAPI{
		envs: []org.DeployEnvironment{{ID: "e1", Name: "production"}, {ID: "e2", Name: "staging"}},
		envDetail: map[string]*org.DeployEnvironment{
			"e1": {ID: "e1", Name: "production", Kind: "custom", Description: "prod env"},
		},
		comps: []org.DeployComponent{
			{ID: "c1", Name: "web", ProjectID: "proj-web-uuid"},
			{ID: "c2", Name: "orphan", ProjectID: "unknown-project"},
			{ID: "c3", Name: "no-project"},
		},
		settings: map[string]*org.DeploySettings{
			"proj-web-uuid": {ProjectID: "proj-web-uuid", PipelineIDs: []string{"p1", "p2"}},
		},
	}
	m, err := deployExporter(d).Export(context.Background(), exporter.Options{
		Host: "https://circleci.com", OrgSlug: "gh/myorg",
		IncludeProjects: true, ProjectSlugs: []string{"gh/myorg/web"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Deploys == nil {
		t.Fatal("expected m.Deploys to be captured")
	}
	if len(m.Deploys.Environments) != 2 {
		t.Fatalf("environments = %+v", m.Deploys.Environments)
	}
	if e := m.Deploys.Environments[0]; e.Name != "production" || e.Kind != "custom" || e.Description != "prod env" {
		t.Errorf("production env detail not merged: %+v", e)
	}
	byName := map[string]string{}
	for _, c := range m.Deploys.Components {
		byName[c.Name] = c.ProjectSlug
	}
	if byName["web"] != "gh/myorg/web" {
		t.Errorf("component web should resolve to project slug gh/myorg/web, got %q", byName["web"])
	}
	if byName["orphan"] != "" || byName["no-project"] != "" {
		t.Errorf("unresolvable components must have no project slug: %v", byName)
	}
	if len(m.Deploys.ProjectSettings) != 1 || m.Deploys.ProjectSettings[0].PipelineCount != 2 ||
		m.Deploys.ProjectSettings[0].ProjectSlug != "gh/myorg/web" {
		t.Errorf("project settings = %+v", m.Deploys.ProjectSettings)
	}
	var warned bool
	for _, w := range m.Warnings {
		if w.Code == "deploy_component_project_unresolved" {
			warned = true
		}
	}
	if !warned {
		t.Error("expected a deploy_component_project_unresolved warning for the orphan component")
	}
}

func TestExport_Deploys_NilClientOrEmptyIsSilent(t *testing.T) {
	// nil Deploy client: skipped.
	m, err := minimalExporter().Export(context.Background(), exporter.Options{Host: "h", OrgSlug: "gh/myorg"})
	if err != nil || m.Deploys != nil {
		t.Errorf("nil client: err=%v deploys=%+v", err, m.Deploys)
	}
	// Empty org: nothing recorded.
	m, err = deployExporter(&fakeDeployAPI{}).Export(context.Background(), exporter.Options{Host: "h", OrgSlug: "gh/myorg"})
	if err != nil || m.Deploys != nil {
		t.Errorf("empty: err=%v deploys=%+v", err, m.Deploys)
	}
}

func TestExport_Deploys_ReadFailureIsWarningNotError(t *testing.T) {
	d := &fakeDeployAPI{envsErr: errors.New("503 service unavailable")}
	m, err := deployExporter(d).Export(context.Background(), exporter.Options{Host: "h", OrgSlug: "gh/myorg"})
	if err != nil {
		t.Fatalf("a deploy read failure must not fail the export: %v", err)
	}
	var found bool
	for _, w := range m.Warnings {
		if w.Code == "deploy_environments_unreadable" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected deploy_environments_unreadable warning, got %+v", m.Warnings)
	}
}
