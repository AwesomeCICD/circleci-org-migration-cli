package exporter_test

import (
	"context"
	"errors"
	"testing"

	"github.com/AwesomeCICD/circleci-org-migration-cli/api/org"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/exporter"
)

// hierOrg adds the environment-hierarchy reads to the shared fake org client.
type hierOrg struct {
	*fakeOrgAPI
	list      []org.EnvHierarchySummary
	listErr   error
	levels    map[string][]org.EnvHierarchyLevelRef
	detail    map[string]*org.EnvHierarchyDetail
	defaultID string
}

func (h *hierOrg) ListEnvironmentHierarchies(context.Context, string) ([]org.EnvHierarchySummary, error) {
	return h.list, h.listErr
}
func (h *hierOrg) GetEnvironmentHierarchyDetail(_ context.Context, id string) (*org.EnvHierarchyDetail, error) {
	if d, ok := h.detail[id]; ok {
		return d, nil
	}
	return &org.EnvHierarchyDetail{}, nil
}
func (h *hierOrg) ListEnvironmentHierarchyLevels(_ context.Context, id string) ([]org.EnvHierarchyLevelRef, error) {
	return h.levels[id], nil
}
func (h *hierOrg) ResolveOrgEnvironmentHierarchy(context.Context, string) (string, string, string, error) {
	if h.defaultID == "" {
		return "NONE", "", "", nil
	}
	return "ORGANIZATION", h.defaultID, "", nil
}

func hierExporter(h *hierOrg, d *fakeDeployAPI) *exporter.Exporter {
	ex := deployExporter(d)
	h.fakeOrgAPI = ex.Org.(*fakeOrgAPI)
	ex.Org = h
	return ex
}

func TestExport_EnvironmentHierarchies_CapturesLevelsAndAssignments(t *testing.T) {
	h := &hierOrg{
		list: []org.EnvHierarchySummary{{ID: "h1", Name: "standard", Description: "dev to prod"}, {ID: "h2", Name: "mobile"}},
		levels: map[string][]org.EnvHierarchyLevelRef{
			"h1": {{Position: 2, IntegrationID: "i2", IntegrationName: "prod"}, {Position: 1, IntegrationID: "i1", IntegrationName: "dev"}},
			"h2": {{Position: 1, IntegrationID: "i3", IntegrationName: "ios"}},
		},
		detail: map[string]*org.EnvHierarchyDetail{
			"h2": {AssignedProjectIDs: []string{"proj-web-uuid", "gone"}, AssignedComponentIDs: []string{"c1"}},
		},
		defaultID: "h1",
	}
	d := &fakeDeployAPI{comps: []org.DeployComponent{{ID: "c1", Name: "web", ProjectID: "proj-web-uuid"}}}
	m, err := hierExporter(h, d).Export(context.Background(), exporter.Options{
		Host: "https://circleci.com", OrgSlug: "gh/myorg",
		IncludeProjects: true, ProjectSlugs: []string{"gh/myorg/web"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(m.EnvironmentHierarchies) != 2 {
		t.Fatalf("hierarchies = %+v", m.EnvironmentHierarchies)
	}
	// SortStable orders by name: mobile, standard.
	mobile, std := m.EnvironmentHierarchies[0], m.EnvironmentHierarchies[1]
	if std.Name != "standard" || !std.IsDefault || mobile.IsDefault {
		t.Errorf("default flag wrong: %+v / %+v", std, mobile)
	}
	if len(std.Levels) != 2 || std.Levels[0].Position != 1 || std.Levels[0].IntegrationName != "dev" {
		t.Errorf("levels not captured in position order: %+v", std.Levels)
	}
	if len(mobile.Projects) != 1 || mobile.Projects[0].Name != "web" || mobile.Projects[0].Slug != "gh/myorg/web" {
		t.Errorf("project assignment: %+v", mobile.Projects)
	}
	if len(mobile.Components) != 1 || mobile.Components[0].Name != "web" || mobile.Components[0].ProjectSlug != "gh/myorg/web" {
		t.Errorf("component assignment: %+v", mobile.Components)
	}
	var unresolved bool
	for _, w := range m.Warnings {
		if w.Code == "environment_hierarchy_project_unresolved" {
			unresolved = true
		}
	}
	if !unresolved {
		t.Error("expected a warning for the project assignment that is not in the export")
	}
}

func TestExport_EnvironmentHierarchies_ReadErrorWarnsAndNilClientIsSilent(t *testing.T) {
	h := &hierOrg{listErr: errors.New("403 forbidden")}
	m, err := hierExporter(h, &fakeDeployAPI{}).Export(context.Background(), exporter.Options{Host: "h", OrgSlug: "gh/myorg"})
	if err != nil {
		t.Fatal(err)
	}
	if len(m.EnvironmentHierarchies) != 0 {
		t.Errorf("no hierarchies expected: %+v", m.EnvironmentHierarchies)
	}
	var warned bool
	for _, w := range m.Warnings {
		if w.Code == "environment_hierarchies_unreadable" {
			warned = true
		}
	}
	if !warned {
		t.Error("expected environment_hierarchies_unreadable warning")
	}

	// An Org client without the optional methods is skipped silently.
	m, err = minimalExporter().Export(context.Background(), exporter.Options{Host: "h", OrgSlug: "gh/myorg"})
	if err != nil || len(m.EnvironmentHierarchies) != 0 {
		t.Errorf("plain client: err=%v hierarchies=%+v", err, m.EnvironmentHierarchies)
	}
}
