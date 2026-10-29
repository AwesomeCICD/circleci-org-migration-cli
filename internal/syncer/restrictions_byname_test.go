package syncer

import (
	"context"
	"errors"
	"strings"
	"testing"

	cctx "github.com/AwesomeCICD/circleci-org-migration-cli/api/context"
	"github.com/AwesomeCICD/circleci-org-migration-cli/api/org"
	"github.com/AwesomeCICD/circleci-org-migration-cli/api/project"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
)

// ---------------------------------------------------------------------------
// Context project restrictions: GitHub App destination, no mapping entry
// ---------------------------------------------------------------------------

// appOrgGetter reports a circleci-type (GitHub App) destination org.
func appOrgGetter(slug string) (*org.Organization, error) {
	return &org.Organization{ID: "dest-org-id", Slug: slug, VCSType: "circleci"}, nil
}

func appRestrictionSyncer(t *testing.T, listed []project.OrgProject, listErr error) (*Syncer, *fakeContextWriter, *fakeProjectWriter) {
	t.Helper()
	fw := &fakeContextWriter{
		listContexts: func(string, string) ([]cctx.Context, error) {
			return []cctx.Context{{ID: "ctx-1", Name: "prod"}}, nil
		},
		listRestrictions: func(string) ([]cctx.Restriction, error) { return nil, nil },
	}
	fp := &fakeProjectWriter{
		listOrgProjects: func(string) ([]project.OrgProject, error) { return listed, listErr },
		getProject: func(slug string) (*project.Project, error) {
			return nil, errors.New("GetProject must not be needed for a name match: " + slug)
		},
	}
	return &Syncer{Org: &fakeOrgResolver{}, Contexts: fw, Projects: fp}, fw, fp
}

func appOnlyOrgMapping() *manifest.Mapping {
	return &manifest.Mapping{Org: manifest.OrgMapping{From: "gh/src", To: "circleci/dest-org-uuid"}}
}

// The reported case: the mapping has only the org entry. The restriction is
// resolved by project name against the destination org and written with the
// destination project UUID.
func TestProjectRestriction_AppDest_MatchedByName(t *testing.T) {
	sy, fw, fp := appRestrictionSyncer(t, []project.OrgProject{
		{ID: "dest-proj-uuid", Slug: "circleci/dest-org-uuid/dest-proj-uuid", Name: "web"},
	}, nil)
	m := projectRestrictionManifest("src-proj-uuid", "gh/src/web", "web")
	opts := Options{Apply: true}
	if _, err := sy.SyncContexts(context.Background(), m, nil, appOnlyOrgMapping(), opts); err != nil {
		t.Fatal(err)
	}
	rep := sy.ApplyDeferredProjectRestrictions(context.Background(), nil, opts)

	if a := projectRestrictionAction(t, rep); a.Status != "set" {
		t.Fatalf("restriction status = %q (%s), want set", a.Status, a.Detail)
	}
	creates := fw.callsTo("CreateRestriction")
	if len(creates) != 1 || creates[0].args[1] != "project" || creates[0].args[2] != "dest-proj-uuid" {
		t.Fatalf("want one project restriction with the dest UUID, got %+v", creates)
	}
	if got := len(fp.callsTo("ListOrgProjects")); got != 1 {
		t.Errorf("destination projects listed %d times, want exactly 1", got)
	}
}

// The manifest project name (not the slug tail) is the match key.
func TestProjectRestriction_AppDest_UsesProjectName(t *testing.T) {
	sy, fw, _ := appRestrictionSyncer(t, []project.OrgProject{
		{ID: "hw-uuid", Slug: "circleci/dest-org-uuid/hw-uuid", Name: "helloworld"},
	}, nil)
	m := projectRestrictionManifest("src-proj-uuid", "gh/src/uipath-release-prototype", "helloworld")
	m.Projects[0].Name = "helloworld"
	opts := Options{Apply: true}
	if _, err := sy.SyncContexts(context.Background(), m, nil, appOnlyOrgMapping(), opts); err != nil {
		t.Fatal(err)
	}
	rep := sy.ApplyDeferredProjectRestrictions(context.Background(), nil, opts)
	if a := projectRestrictionAction(t, rep); a.Status != "set" {
		t.Fatalf("status = %q (%s)", a.Status, a.Detail)
	}
	if c := fw.callsTo("CreateRestriction"); len(c) != 1 || c[0].args[2] != "hw-uuid" {
		t.Errorf("want a restriction for hw-uuid, got %+v", c)
	}
}

// An explicit mapping entry still wins over a same-named destination project.
func TestProjectRestriction_AppDest_MappingEntryWins(t *testing.T) {
	sy, fw, fp := appRestrictionSyncer(t, []project.OrgProject{
		{ID: "name-match-uuid", Slug: "circleci/dest-org-uuid/name-match-uuid", Name: "web"},
	}, nil)
	fp.getProject = func(slug string) (*project.Project, error) {
		if slug != "circleci/dest-org-uuid/explicit" {
			t.Errorf("GetProject(%q), want the mapped slug", slug)
		}
		return &project.Project{Slug: slug, ID: "explicit-uuid"}, nil
	}
	m := projectRestrictionManifest("src-proj-uuid", "gh/src/web", "web")
	mapping := appOnlyOrgMapping()
	mapping.Projects = map[string]string{"gh/src/web": "circleci/dest-org-uuid/explicit"}
	opts := Options{Apply: true}
	if _, err := sy.SyncContexts(context.Background(), m, nil, mapping, opts); err != nil {
		t.Fatal(err)
	}
	sy.ApplyDeferredProjectRestrictions(context.Background(), nil, opts)
	if c := fw.callsTo("CreateRestriction"); len(c) != 1 || c[0].args[2] != "explicit-uuid" {
		t.Errorf("mapping entry must win; got %+v", c)
	}
	if fp.hasCalled("ListOrgProjects") {
		t.Error("no listing is needed when the mapping resolves the project")
	}
}

// No project of that name: a clear manual action naming the project, no write.
func TestProjectRestriction_AppDest_UnmatchedIsReported(t *testing.T) {
	sy, fw, _ := appRestrictionSyncer(t, []project.OrgProject{{ID: "x", Slug: "circleci/o/x", Name: "other"}}, nil)
	m := projectRestrictionManifest("src-proj-uuid", "gh/src/web", "web")
	opts := Options{Apply: true}
	if _, err := sy.SyncContexts(context.Background(), m, nil, appOnlyOrgMapping(), opts); err != nil {
		t.Fatal(err)
	}
	rep := sy.ApplyDeferredProjectRestrictions(context.Background(), nil, opts)
	a := projectRestrictionAction(t, rep)
	if a.Status != "manual" || !strings.Contains(a.Detail, `"web"`) || !strings.Contains(a.Detail, "gh/src/web") {
		t.Errorf("want a manual action naming project web and its source slug, got %+v", a)
	}
	if fw.hasCalled("CreateRestriction") {
		t.Error("nothing must be written for an unmatched project")
	}
}

// A failed listing is surfaced as a manual action, not swallowed.
func TestProjectRestriction_AppDest_ListErrorIsReported(t *testing.T) {
	sy, _, _ := appRestrictionSyncer(t, nil, errors.New("boom"))
	m := projectRestrictionManifest("src-proj-uuid", "gh/src/web", "web")
	opts := Options{Apply: true}
	if _, err := sy.SyncContexts(context.Background(), m, nil, appOnlyOrgMapping(), opts); err != nil {
		t.Fatal(err)
	}
	a := projectRestrictionAction(t, sy.ApplyDeferredProjectRestrictions(context.Background(), nil, opts))
	if a.Status != "manual" || !strings.Contains(a.Detail, "boom") {
		t.Errorf("want a manual action carrying the list error, got %+v", a)
	}
}

// Dry run: a project that this same run would create previews as "would add",
// not as "not found".
func TestProjectRestriction_AppDest_DryRunPreviewsProjectCreatedInRun(t *testing.T) {
	sy, _, _ := appRestrictionSyncer(t, nil, nil)
	sy.Org = &fakeOrgResolver{getOrganization: appOrgGetter}
	m := projectRestrictionManifest("src-proj-uuid", "gh/src/web", "web")
	m.Projects[0].PipelineDefinitions = nil
	opts := Options{} // dry run
	mapping := appOnlyOrgMapping()
	if _, err := sy.SyncContexts(context.Background(), m, nil, mapping, opts); err != nil {
		t.Fatal(err)
	}
	if _, err := sy.SyncProjects(context.Background(), m, nil, mapping, opts); err != nil {
		t.Fatal(err)
	}
	a := projectRestrictionAction(t, sy.ApplyDeferredProjectRestrictions(context.Background(), nil, opts))
	if a.Status != "set" || !strings.Contains(a.Detail, "would add") {
		t.Errorf("dry run should preview the restriction, got %+v", a)
	}
}
