package syncer

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/AwesomeCICD/circleci-org-migration-cli/api/org"
	"github.com/AwesomeCICD/circleci-org-migration-cli/api/project"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
)

func deploysManifest() *manifest.Manifest {
	return &manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		Source:        manifest.Source{Org: manifest.Org{Slug: "gh/acme", ID: srcOrgUUID}},
		Deploys: &manifest.Deploys{
			Environments: []manifest.DeployEnvironment{
				{Name: "production", Kind: "custom", Description: "prod"},
				{Name: "staging", Kind: "custom"},
			},
			Components: []manifest.DeployComponent{
				{Name: "web", ProjectSlug: "gh/acme/web", ProjectName: "web"},
				{Name: "api", ProjectSlug: "gh/acme/api", ProjectName: "api"},
				{Name: "old", ProjectSlug: "gh/acme/web", ProjectName: "web", Archived: true},
			},
			ProjectSettings: []manifest.DeployProjectSettings{
				{ProjectSlug: "gh/acme/web", ProjectName: "web", PipelineCount: 2},
			},
		},
	}
}

func TestSyncDeploys_NoDeploys_NoOp(t *testing.T) {
	sy := &Syncer{Org: &nsOrgResolver{}}
	rep, err := sy.SyncDeploys(context.Background(), &manifest.Manifest{Source: manifest.Source{Org: manifest.Org{Slug: "gh/acme"}}}, nil, Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rep.Actions) != 0 {
		t.Errorf("expected no actions, got %+v", rep.Actions)
	}
}

// TestSyncDeploys_ManualListNoCreateEndpoint verifies that, because the Deploys
// API is read-only, missing environments/components/settings are reported
// manual (with their names) and nothing is written — in dry-run and apply alike.
func TestSyncDeploys_ManualListNoCreateEndpoint(t *testing.T) {
	for _, apply := range []bool{false, true} {
		or := &nsOrgResolver{
			deployEnvs: []org.DeployEnvironment{{Name: "staging"}}, // already in dest
		}
		fp := &fakeProjectWriter{}
		fp.listOrgProjects = func(string) ([]project.OrgProject, error) {
			return []project.OrgProject{{ID: "dest-web-id", Name: "web"}}, nil
		}
		sy := &Syncer{Org: or, Projects: fp}
		mp := &manifest.Mapping{Org: manifest.OrgMapping{From: "gh/acme", To: destSlugApp}}

		rep, err := sy.SyncDeploys(context.Background(), deploysManifest(), mp, Options{Apply: apply})
		if err != nil {
			t.Fatalf("apply=%v: unexpected error: %v", apply, err)
		}
		byTarget := map[string]Action{}
		for _, a := range rep.Actions {
			byTarget[a.Kind+":"+a.Target] = a
		}
		if a := byTarget["deploy-environment:production"]; a.Status != "manual" ||
			!strings.Contains(a.Detail, "circleci run release plan --environment-name") {
			t.Errorf("apply=%v production env: want manual w/ creation hint, got %+v", apply, a)
		}
		if a := byTarget["deploy-environment:staging"]; a.Status != "exists" {
			t.Errorf("apply=%v staging env: want exists, got %+v", apply, a)
		}
		if a := byTarget["deploy-component:web"]; a.Status != "manual" || !strings.Contains(a.Detail, `its project "web" exists in the destination`) {
			t.Errorf("apply=%v web component: want manual mentioning mapped project, got %+v", apply, a)
		}
		if a := byTarget["deploy-component:api"]; a.Status != "manual" || !strings.Contains(a.Detail, `its project "api" was not found in the destination org`) {
			t.Errorf("apply=%v api component: want manual noting missing project, got %+v", apply, a)
		}
		if a := byTarget["deploy-component:old"]; a.Status != "skipped" {
			t.Errorf("apply=%v archived component: want skipped, got %+v", apply, a)
		}
		if a := byTarget["deploy-settings:web"]; a.Status != "manual" || !strings.Contains(a.Detail, "2 deploy/rollback pipeline") {
			t.Errorf("apply=%v deploy settings: want manual, got %+v", apply, a)
		}
		if rep.Counts()["created"] != 0 || rep.Counts()["set"] != 0 {
			t.Errorf("apply=%v: nothing may be created/set (no create endpoint): %v", apply, rep.Counts())
		}
	}
}

func TestSyncDeploys_ExistingComponentMatchedByProject(t *testing.T) {
	or := &nsOrgResolver{
		deployComps: []org.DeployComponent{{ID: "c1", Name: "web", ProjectID: "dest-web-id"}},
	}
	fp := &fakeProjectWriter{}
	fp.listOrgProjects = func(string) ([]project.OrgProject, error) {
		return []project.OrgProject{{ID: "dest-web-id", Name: "web"}}, nil
	}
	sy := &Syncer{Org: or, Projects: fp}
	mp := &manifest.Mapping{Org: manifest.OrgMapping{From: "gh/acme", To: destSlugApp}}
	rep, _ := sy.SyncDeploys(context.Background(), deploysManifest(), mp, Options{Apply: true})
	for _, a := range rep.Actions {
		if a.Kind == "deploy-component" && a.Target == "web" && a.Status != "exists" {
			t.Errorf("web component present in dest project should be 'exists', got %+v", a)
		}
	}
}

// TestSyncDeploys_MissingProject_NotMatchedToProjectlessComponent: a component
// whose destination project is missing is reported manual, never matched against
// a same-named component with no project (an empty project key).
func TestSyncDeploys_MissingProject_NotMatchedToProjectlessComponent(t *testing.T) {
	or := &nsOrgResolver{
		deployComps: []org.DeployComponent{{ID: "c1", Name: "api", ProjectID: ""}},
	}
	fp := &fakeProjectWriter{}
	fp.listOrgProjects = func(string) ([]project.OrgProject, error) {
		return []project.OrgProject{{ID: "dest-web-id", Name: "web"}}, nil
	}
	sy := &Syncer{Org: or, Projects: fp}
	mp := &manifest.Mapping{Org: manifest.OrgMapping{From: "gh/acme", To: destSlugApp}}
	rep, err := sy.SyncDeploys(context.Background(), deploysManifest(), mp, Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, a := range rep.Actions {
		if a.Kind == "deploy-component" && a.Target == "api" &&
			(a.Status != "manual" || !strings.Contains(a.Detail, "was not found in the destination org")) {
			t.Errorf("api component with a missing project must be manual (project not found), got %+v", a)
		}
	}
}

// TestSyncDeploys_ResolveOrgError_IsErrorActionNotAbort: an unresolvable
// destination org becomes an error action in the Deploys report; sync is not
// aborted.
func TestSyncDeploys_ResolveOrgError_IsErrorActionNotAbort(t *testing.T) {
	or := &fakeOrgResolver{resolveOrgID: func(string) (string, error) { return "", errors.New("boom") }}
	sy := &Syncer{Org: or}
	mp := &manifest.Mapping{Org: manifest.OrgMapping{From: "gh/acme", To: destSlugApp}}
	rep, err := sy.SyncDeploys(context.Background(), deploysManifest(), mp, Options{Apply: true})
	if err != nil {
		t.Fatalf("a ResolveOrgID failure must not abort sync, got error %v", err)
	}
	if rep == nil || rep.Counts()["error"] != 1 || !strings.Contains(rep.Actions[0].Detail, "boom") {
		t.Errorf("want one error action carrying the cause, got %+v", rep)
	}
}
