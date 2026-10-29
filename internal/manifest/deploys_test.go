package manifest_test

import (
	"path/filepath"
	"testing"

	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
)

func TestDeploys_RoundTripAndSort(t *testing.T) {
	m := &manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		Deploys: &manifest.Deploys{
			Environments: []manifest.DeployEnvironment{{Name: "staging"}, {Name: "production", Kind: "custom"}},
			Components: []manifest.DeployComponent{
				{Name: "web", ProjectSlug: "gh/a/web"}, {Name: "api", ProjectSlug: "gh/a/api"},
			},
			ProjectSettings: []manifest.DeployProjectSettings{
				{ProjectSlug: "gh/a/web", PipelineCount: 1}, {ProjectSlug: "gh/a/api", PipelineCount: 2},
			},
		},
	}
	m.SortStable()
	if m.Deploys.Environments[0].Name != "production" || m.Deploys.Components[0].Name != "api" ||
		m.Deploys.ProjectSettings[0].ProjectSlug != "gh/a/api" {
		t.Errorf("deploys not sorted: %+v", m.Deploys)
	}

	path := filepath.Join(t.TempDir(), "m.json")
	if err := m.Save(path); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := manifest.Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.Deploys == nil || len(got.Deploys.Environments) != 2 || got.Deploys.Environments[0].Kind != "custom" {
		t.Errorf("round trip lost deploys: %+v", got.Deploys)
	}

	// Omitted when absent (additive, schema version unchanged).
	empty := &manifest.Manifest{SchemaVersion: manifest.SchemaVersion}
	if err := empty.Save(path); err != nil {
		t.Fatalf("save: %v", err)
	}
	again, _ := manifest.Load(path)
	if again.Deploys != nil {
		t.Errorf("deploys should be nil when absent, got %+v", again.Deploys)
	}
}
