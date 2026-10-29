package syncer

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/AwesomeCICD/circleci-org-migration-cli/api/org"
	"github.com/AwesomeCICD/circleci-org-migration-cli/api/project"
	"github.com/AwesomeCICD/circleci-org-migration-cli/api/rest"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
)

// hierOrgResolver is an in-memory destination for environments and hierarchies.
type hierOrgResolver struct {
	nsOrgResolver

	envs                                   []org.BFFEnvironment
	hiers                                  []org.EnvHierarchySummary
	levels                                 map[string][]org.EnvHierarchyLevelRef
	assigned                               map[string]*org.EnvHierarchyDetail
	scope                                  string
	defaultID                              string
	defaultNm                              string
	createEnvErr, createHierErr, assignErr error

	createdEnvs   []string
	createdEnvTyp map[string]string
	createdHiers  []org.EnvHierarchyCreate
	assignCalls   []assignCall
}

type assignCall struct {
	hierID, entityType string
	ids                []string
}

func (f *hierOrgResolver) ListBFFEnvironments(context.Context, string) ([]org.BFFEnvironment, error) {
	return f.envs, nil
}
func (f *hierOrgResolver) CreateEnvironment(_ context.Context, _, name, typ, _ string) (string, error) {
	if f.createEnvErr != nil {
		return "", f.createEnvErr
	}
	f.createdEnvs = append(f.createdEnvs, name)
	if f.createdEnvTyp == nil {
		f.createdEnvTyp = map[string]string{}
	}
	f.createdEnvTyp[name] = typ
	return "env-" + name, nil
}
func (f *hierOrgResolver) ListEnvironmentHierarchies(context.Context, string) ([]org.EnvHierarchySummary, error) {
	return f.hiers, nil
}
func (f *hierOrgResolver) GetEnvironmentHierarchyDetail(_ context.Context, id string) (*org.EnvHierarchyDetail, error) {
	if d := f.assigned[id]; d != nil {
		return d, nil
	}
	return &org.EnvHierarchyDetail{}, nil
}
func (f *hierOrgResolver) ListEnvironmentHierarchyLevels(_ context.Context, id string) ([]org.EnvHierarchyLevelRef, error) {
	return f.levels[id], nil
}
func (f *hierOrgResolver) ResolveOrgEnvironmentHierarchy(context.Context, string) (string, string, string, error) {
	if f.scope == "" {
		return "NONE", "", "", nil
	}
	return f.scope, f.defaultID, f.defaultNm, nil
}
func (f *hierOrgResolver) CreateEnvironmentHierarchy(_ context.Context, _ string, in org.EnvHierarchyCreate) (string, error) {
	if f.createHierErr != nil {
		return "", f.createHierErr
	}
	f.createdHiers = append(f.createdHiers, in)
	return "hier-" + in.Name, nil
}
func (f *hierOrgResolver) UpsertEnvironmentHierarchyAssignment(_ context.Context, id, typ string, ids []string) error {
	if f.assignErr != nil {
		return f.assignErr
	}
	f.assignCalls = append(f.assignCalls, assignCall{id, typ, ids})
	return nil
}

func hierManifest() *manifest.Manifest {
	return &manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		Source:        manifest.Source{Org: manifest.Org{Slug: "gh/acme", ID: srcOrgUUID}},
		Deploys: &manifest.Deploys{Environments: []manifest.DeployEnvironment{
			{Name: "dev", Kind: "custom", Description: "dev env"},
			{Name: "prod", Kind: "kubernetes_cluster"},
		}},
		EnvironmentHierarchies: []manifest.EnvironmentHierarchyDef{{
			Name: "standard", Description: "dev to prod", IsDefault: true,
			Levels:   []manifest.EnvHierarchyLevel{{Position: 2, IntegrationName: "prod"}, {Position: 1, IntegrationName: "dev"}},
			Projects: []manifest.EnvHierarchyProjectRef{{Slug: "gh/acme/web", Name: "web"}, {Slug: "gh/acme/gone", Name: "gone"}},
			Components: []manifest.EnvHierarchyComponentRef{
				{Name: "web", ProjectSlug: "gh/acme/web", ProjectName: "web"},
				{Name: "late", ProjectSlug: "gh/acme/web", ProjectName: "web"},
			},
		}},
	}
}

func hierSyncer(or *hierOrgResolver) *Syncer {
	fp := &fakeProjectWriter{}
	fp.listOrgProjects = func(string) ([]project.OrgProject, error) {
		return []project.OrgProject{{ID: "dest-web", Name: "web"}}, nil
	}
	return &Syncer{Org: or, Projects: fp}
}

func hierMapping() *manifest.Mapping {
	return &manifest.Mapping{Org: manifest.OrgMapping{From: "gh/acme", To: destSlugApp}}
}

func byTarget(rep *Report) map[string]Action {
	out := map[string]Action{}
	for _, a := range rep.Actions {
		out[a.Kind+":"+a.Target] = a
	}
	return out
}

func TestSyncEnvironmentHierarchies_NoneIsNoOp(t *testing.T) {
	sy := hierSyncer(&hierOrgResolver{})
	rep, err := sy.SyncEnvironmentHierarchies(context.Background(),
		&manifest.Manifest{Source: manifest.Source{Org: manifest.Org{Slug: "gh/acme"}}}, nil, Options{Apply: true})
	if err != nil || len(rep.Actions) != 0 {
		t.Fatalf("err=%v actions=%+v", err, rep.Actions)
	}
}

func TestSyncEnvironmentHierarchies_Apply_CreatesEnvsHierarchyAndAssignments(t *testing.T) {
	or := &hierOrgResolver{
		envs: []org.BFFEnvironment{}, // dev/prod both missing
		// the component "web" already exists in the destination project
	}
	or.deployComps = []org.DeployComponent{{ID: "comp-web", Name: "web", ProjectID: "dest-web"}}
	rep, err := hierSyncer(or).SyncEnvironmentHierarchies(context.Background(), hierManifest(), hierMapping(), Options{Apply: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(or.createdEnvs, ","); got != "dev,prod" {
		t.Errorf("environments created = %q", got)
	}
	if or.createdEnvTyp["dev"] != "CUSTOM" || or.createdEnvTyp["prod"] != "KUBERNETES_CLUSTER" {
		t.Errorf("integration types = %v", or.createdEnvTyp)
	}
	if len(or.createdHiers) != 1 {
		t.Fatalf("hierarchies created = %+v", or.createdHiers)
	}
	h := or.createdHiers[0]
	if !h.IsDefault || len(h.Levels) != 2 || h.Levels[0].Position != 1 || h.Levels[0].IntegrationID != "env-dev" || h.Levels[1].IntegrationID != "env-prod" {
		t.Errorf("hierarchy body (levels mapped by env name, in position order) = %+v", h)
	}
	if len(or.assignCalls) != 2 {
		t.Fatalf("assign calls = %+v", or.assignCalls)
	}
	if c := or.assignCalls[0]; c.entityType != "PROJECT" || len(c.ids) != 1 || c.ids[0] != "dest-web" || c.hierID != "hier-standard" {
		t.Errorf("project assignment = %+v", c)
	}
	if c := or.assignCalls[1]; c.entityType != "COMPONENT" || c.ids[0] != "comp-web" {
		t.Errorf("component assignment = %+v", c)
	}
	acts := byTarget(rep)
	if a := acts["env-hierarchy:standard/project:gone"]; a.Status != "manual" {
		t.Errorf("missing project should be manual: %+v", a)
	}
	if a := acts["env-hierarchy:standard/component:web/late"]; a.Status != "manual" || !strings.Contains(a.Detail, "first release creates the component") {
		t.Errorf("missing component should be manual with re-run hint: %+v", a)
	}
	if a := acts["env-hierarchy-environment:prod"]; a.Status != "created" || !strings.Contains(a.Detail, "reconnect") {
		t.Errorf("non-CUSTOM environment should note settings are not copied: %+v", a)
	}
}

func TestSyncEnvironmentHierarchies_DryRunWritesNothing(t *testing.T) {
	or := &hierOrgResolver{}
	rep, err := hierSyncer(or).SyncEnvironmentHierarchies(context.Background(), hierManifest(), hierMapping(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(or.createdEnvs)+len(or.createdHiers)+len(or.assignCalls) != 0 {
		t.Errorf("dry run wrote: envs=%v hiers=%v assigns=%v", or.createdEnvs, or.createdHiers, or.assignCalls)
	}
	acts := byTarget(rep)
	if a := acts["env-hierarchy:standard"]; a.Status != "created" || !strings.Contains(a.Detail, "would create") || !strings.Contains(a.Detail, "org default") {
		t.Errorf("dry-run hierarchy action: %+v", a)
	}
	if a := acts["env-hierarchy:standard/projects"]; a.Status != "set" || !strings.Contains(a.Detail, "would assign") {
		t.Errorf("dry-run project assignment: %+v", a)
	}
	if a := acts["env-hierarchy-environment:dev"]; !strings.Contains(a.Detail, "would create") {
		t.Errorf("dry-run environment: %+v", a)
	}
}

func TestSyncEnvironmentHierarchies_Idempotent_SameLevelsExists(t *testing.T) {
	or := &hierOrgResolver{
		envs:  []org.BFFEnvironment{{ID: "i1", Name: "dev"}, {ID: "i2", Name: "prod"}},
		hiers: []org.EnvHierarchySummary{{ID: "h1", Name: "standard"}},
		levels: map[string][]org.EnvHierarchyLevelRef{"h1": {
			{Position: 1, IntegrationID: "i1", IntegrationName: "dev"}, {Position: 2, IntegrationID: "i2", IntegrationName: "prod"}}},
		assigned: map[string]*org.EnvHierarchyDetail{"h1": {AssignedProjectIDs: []string{"dest-web"}}},
		scope:    "ORGANIZATION", defaultID: "h1", defaultNm: "standard",
	}
	rep, err := hierSyncer(or).SyncEnvironmentHierarchies(context.Background(), hierManifest(), hierMapping(), Options{Apply: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(or.createdEnvs)+len(or.createdHiers) != 0 {
		t.Errorf("re-run must not create anything: %v %v", or.createdEnvs, or.createdHiers)
	}
	for _, c := range or.assignCalls {
		if c.entityType == "PROJECT" {
			t.Errorf("already-assigned project must not be re-assigned: %+v", c)
		}
	}
	acts := byTarget(rep)
	if acts["env-hierarchy:standard"].Status != "exists" || acts["env-hierarchy:standard/project:web"].Status != "exists" ||
		acts["env-hierarchy:standard/default"].Status != "exists" {
		t.Errorf("expected exists for hierarchy, project and default: %+v", rep.Actions)
	}
}

func TestSyncEnvironmentHierarchies_ExistingWithDifferentLevelsIsManualAndUntouched(t *testing.T) {
	or := &hierOrgResolver{
		envs:  []org.BFFEnvironment{{ID: "i1", Name: "dev"}, {ID: "i2", Name: "prod"}},
		hiers: []org.EnvHierarchySummary{{ID: "h1", Name: "standard"}},
		levels: map[string][]org.EnvHierarchyLevelRef{"h1": {
			{Position: 1, IntegrationID: "i2", IntegrationName: "prod"}, {Position: 2, IntegrationID: "i1", IntegrationName: "dev"}}},
	}
	rep, _ := hierSyncer(or).SyncEnvironmentHierarchies(context.Background(), hierManifest(), hierMapping(), Options{Apply: true})
	a := byTarget(rep)["env-hierarchy:standard"]
	if a.Status != "manual" || !strings.Contains(a.Detail, "NOT modified") {
		t.Errorf("want manual/untouched, got %+v", a)
	}
	if len(or.createdHiers)+len(or.assignCalls) != 0 {
		t.Error("a mismatching hierarchy must not be recreated or assigned")
	}
}

func TestSyncEnvironmentHierarchies_DoesNotOverrideExistingOrgDefault(t *testing.T) {
	or := &hierOrgResolver{
		envs:  []org.BFFEnvironment{{ID: "i1", Name: "dev"}, {ID: "i2", Name: "prod"}},
		scope: "ORGANIZATION", defaultID: "other", defaultNm: "legacy",
	}
	rep, _ := hierSyncer(or).SyncEnvironmentHierarchies(context.Background(), hierManifest(), hierMapping(), Options{Apply: true})
	if len(or.createdHiers) != 1 || or.createdHiers[0].IsDefault {
		t.Errorf("must create with is_default=false when dest has a default: %+v", or.createdHiers)
	}
	if a := byTarget(rep)["env-hierarchy:standard/default"]; a.Status != "manual" || !strings.Contains(a.Detail, "legacy") {
		t.Errorf("want manual naming the existing default: %+v", a)
	}
}

func TestSyncEnvironmentHierarchies_WriteDeniedDegradesToManual(t *testing.T) {
	or := &hierOrgResolver{createEnvErr: &rest.HTTPError{Code: 403, Message: "forbidden"}}
	rep, err := hierSyncer(or).SyncEnvironmentHierarchies(context.Background(), hierManifest(), hierMapping(), Options{Apply: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(or.createdHiers) != 0 {
		t.Error("nothing may be created after the token is rejected")
	}
	acts := byTarget(rep)
	for _, k := range []string{"env-hierarchy-environment:dev", "env-hierarchy-environment:prod", "env-hierarchy:standard"} {
		if acts[k].Status != "manual" {
			t.Errorf("%s: want manual, got %+v", k, acts[k])
		}
	}
	if !strings.Contains(acts["env-hierarchy-environment:dev"].Detail, "personal API tokens") {
		t.Errorf("manual line should explain the rejection: %q", acts["env-hierarchy-environment:dev"].Detail)
	}
	for _, a := range rep.Actions {
		if a.Status == "error" {
			t.Errorf("a denied write is manual, not error: %+v", a)
		}
	}
}

func TestSyncEnvironmentHierarchies_HierarchyConflictAndUnexpectedError(t *testing.T) {
	or := &hierOrgResolver{
		envs:          []org.BFFEnvironment{{ID: "i1", Name: "dev"}, {ID: "i2", Name: "prod"}},
		createHierErr: &rest.HTTPError{Code: 409, Message: "duplicate"},
	}
	rep, _ := hierSyncer(or).SyncEnvironmentHierarchies(context.Background(), hierManifest(), hierMapping(), Options{Apply: true})
	if a := byTarget(rep)["env-hierarchy:standard"]; a.Status != "manual" {
		t.Errorf("409 should be manual: %+v", a)
	}
	or.createHierErr = errors.New("boom")
	rep, _ = hierSyncer(or).SyncEnvironmentHierarchies(context.Background(), hierManifest(), hierMapping(), Options{Apply: true})
	if a := byTarget(rep)["env-hierarchy:standard"]; a.Status != "error" {
		t.Errorf("other failures should be error: %+v", a)
	}
}

func TestSyncEnvironmentHierarchies_LegacyOrgDefaultOnlyManifest(t *testing.T) {
	m := &manifest.Manifest{Source: manifest.Source{Org: manifest.Org{Slug: "gh/acme", Settings: &manifest.OrgSettings{
		EnvironmentHierarchy: &manifest.EnvironmentHierarchy{Name: "old", Levels: []manifest.EnvHierarchyLevel{{Position: 1, IntegrationName: "dev"}}},
	}}}}
	if !HasEnvironmentHierarchies(m) {
		t.Fatal("legacy single hierarchy should count")
	}
	or := &hierOrgResolver{envs: []org.BFFEnvironment{{ID: "i1", Name: "dev"}}}
	_, err := hierSyncer(or).SyncEnvironmentHierarchies(context.Background(), m, hierMapping(), Options{Apply: true})
	if err != nil || len(or.createdHiers) != 1 || !or.createdHiers[0].IsDefault {
		t.Fatalf("err=%v created=%+v", err, or.createdHiers)
	}
}

func TestSyncEnvironmentHierarchies_ClientWithoutSupportIsManual(t *testing.T) {
	sy := &Syncer{Org: &nsOrgResolver{}}
	rep, err := sy.SyncEnvironmentHierarchies(context.Background(), hierManifest(), hierMapping(), Options{Apply: true})
	if err != nil {
		t.Fatal(err)
	}
	if a := byTarget(rep)["env-hierarchy:standard"]; a.Status != "manual" {
		t.Errorf("want manual, got %+v", a)
	}
}
