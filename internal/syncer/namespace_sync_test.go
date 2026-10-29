package syncer

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/AwesomeCICD/circleci-org-migration-cli/api/org"
	apirunner "github.com/AwesomeCICD/circleci-org-migration-cli/api/runner"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
)

// ---------------------------------------------------------------------------
// Fakes: an OrgResolver that also lists org-owned orbs (namespace ownership),
// and a RunnerWriter that also lists classes attributed to an org.
// ---------------------------------------------------------------------------

const (
	srcOrgUUID  = "src-org-uuid"
	destOrgUUID = "dest-org-uuid"
	destSlugApp = "circleci/dest-short"
)

type nsOrgResolver struct {
	fakeOrgResolver
	// orbsByOrg maps org UUID → orbs owned by that org's namespaces.
	orbsByOrg map[string][]org.OrgOrb
	orbsErr   error

	deployEnvs  []org.DeployEnvironment
	deployComps []org.DeployComponent
}

func (f *nsOrgResolver) ResolveOrgID(_ context.Context, slug string) (string, error) {
	if slug == destSlugApp {
		return destOrgUUID, nil
	}
	return "org-uuid-" + slug, nil
}

func (f *nsOrgResolver) GetOrgOrbs(_ context.Context, orgUUID string) ([]org.OrgOrb, error) {
	if f.orbsErr != nil {
		return nil, f.orbsErr
	}
	return f.orbsByOrg[orgUUID], nil
}

func (f *nsOrgResolver) ListDeployEnvironments(_ context.Context, _ string) ([]org.DeployEnvironment, error) {
	return f.deployEnvs, nil
}

func (f *nsOrgResolver) ListDeployComponents(_ context.Context, _ string) ([]org.DeployComponent, error) {
	return f.deployComps, nil
}

type nsRunnerWriter struct {
	fakeRunnerWriter
	// destClasses are the class names attributed to the destination org.
	destClasses []string
}

func (f *nsRunnerWriter) ListResourceClassesByOrg(_ context.Context, orgID string) ([]apirunner.OrgResourceClass, error) {
	var out []apirunner.OrgResourceClass
	if orgID == destOrgUUID {
		for _, c := range f.destClasses {
			out = append(out, apirunner.OrgResourceClass{ResourceClass: c, OrgID: orgID})
		}
	}
	return out, nil
}

func ownedBy(orgUUID, ns string) map[string][]org.OrgOrb {
	return map[string][]org.OrgOrb{orgUUID: {{OrbName: "o", NamespaceName: ns}}}
}

func sameNSManifest(ns string) *manifest.Manifest {
	return &manifest.Manifest{
		SchemaVersion:   manifest.SchemaVersion,
		Source:          manifest.Source{Org: manifest.Org{Slug: "gh/acme", ID: srcOrgUUID}},
		OrbNamespace:    ns,
		RunnerNamespace: ns,
		Orbs: []manifest.CapturedOrb{{
			Name:     ns + "/tool",
			Versions: []manifest.CapturedOrbVersion{{Version: "1.0.0", Source: "version: 2.1"}},
		}},
		RunnerResourceClasses: []manifest.RunnerResourceClass{
			{Name: ns + "/fast", Description: "fast"},
			{Name: ns + "/slow", Description: "slow"},
		},
	}
}

func wantCommand(ns string) string {
	return `change-namespace-owner! "` + ns + `" "` + destSlugApp + `"`
}

func actionFor(rep *Report, kind string) *Action {
	for i := range rep.Actions {
		if rep.Actions[i].Kind == kind {
			return &rep.Actions[i]
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Orbs
// ---------------------------------------------------------------------------

func TestSyncOrbs_SameNamespace_SourceOwned_ManualNoPublish(t *testing.T) {
	ow := &fakeOrbWriter{}
	sy := &Syncer{
		Org: &nsOrgResolver{orbsByOrg: ownedBy(srcOrgUUID, "bcbsm")},
		Orb: ow,
	}
	m := sameNSManifest("bcbsm")

	rep, err := sy.SyncOrbs(context.Background(), m, Options{Apply: true, DestOrbNamespace: "bcbsm", DestOrgSlug: destSlugApp}, nil, "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ow.createCalls) != 0 || len(ow.publishCalls) != 0 || len(ow.resolveNSCalls) != 0 {
		t.Errorf("must not touch the orb API: create=%v publish=%v resolve=%v", ow.createCalls, ow.publishCalls, ow.resolveNSCalls)
	}
	a := actionFor(rep, "orb-namespace")
	if a == nil || a.Status != "manual" {
		t.Fatalf("expected a manual orb-namespace action, got %+v", rep.Actions)
	}
	if !strings.Contains(a.Detail, wantCommand("bcbsm")) {
		t.Errorf("manual detail missing exact command %q: %s", wantCommand("bcbsm"), a.Detail)
	}
	if !strings.Contains(a.Detail, "no change") {
		t.Errorf("manual detail should say config references need no change: %s", a.Detail)
	}
	if n := rep.Counts()["created"]; n != 0 {
		t.Errorf("no orb may be reported created, got %d", n)
	}
}

func TestSyncOrbs_SameNamespace_DestOwned_Skipped(t *testing.T) {
	ow := &fakeOrbWriter{}
	sy := &Syncer{
		Org: &nsOrgResolver{orbsByOrg: ownedBy(destOrgUUID, "bcbsm")},
		Orb: ow,
	}
	rep, err := sy.SyncOrbs(context.Background(), sameNSManifest("bcbsm"),
		Options{Apply: true, DestOrbNamespace: "bcbsm", DestOrgSlug: destSlugApp}, nil, "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ow.createCalls)+len(ow.publishCalls) != 0 {
		t.Errorf("orbs moved with the namespace; nothing may be published: %v %v", ow.createCalls, ow.publishCalls)
	}
	if n := rep.Counts()["manual"]; n != 0 {
		t.Errorf("dest-owned namespace needs no manual action, got %d: %+v", n, rep.Actions)
	}
	if a := actionFor(rep, "orb"); a == nil || a.Status != "skipped" {
		t.Errorf("expected orb skipped, got %+v", rep.Actions)
	}
}

func TestSyncOrbs_SameNamespace_UnknownOwner_ManualNoPublish(t *testing.T) {
	ow := &fakeOrbWriter{}
	sy := &Syncer{Org: &nsOrgResolver{orbsErr: errors.New("boom")}, Orb: ow}
	rep, err := sy.SyncOrbs(context.Background(), sameNSManifest("bcbsm"),
		Options{Apply: true, DestOrbNamespace: "bcbsm", DestOrgSlug: destSlugApp}, nil, "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ow.createCalls)+len(ow.publishCalls) != 0 {
		t.Errorf("an undetermined same-name namespace must never be republished")
	}
	a := actionFor(rep, "orb-namespace")
	if a == nil || a.Status != "manual" || !strings.Contains(a.Detail, wantCommand("bcbsm")) {
		t.Errorf("expected manual action carrying the command, got %+v", rep.Actions)
	}
}

func TestSyncOrbs_SameNamespace_DryRunSourceOwned(t *testing.T) {
	ow := &fakeOrbWriter{}
	sy := &Syncer{Org: &nsOrgResolver{orbsByOrg: ownedBy(srcOrgUUID, "bcbsm")}, Orb: ow}
	rep, _ := sy.SyncOrbs(context.Background(), sameNSManifest("bcbsm"),
		Options{Apply: false, DestOrbNamespace: "bcbsm", DestOrgSlug: destSlugApp}, nil, "", "")
	if a := actionFor(rep, "orb-namespace"); a == nil || a.Status != "manual" {
		t.Errorf("dry run must still report the handoff, got %+v", rep.Actions)
	}
	if rep.Counts()["created"] != 0 {
		t.Errorf("dry run must not plan orb creation for a same-name namespace")
	}
}

func TestSyncOrbs_DifferentNamespace_PublishesAndRewriteNotice(t *testing.T) {
	ow := &fakeOrbWriter{}
	sy := &Syncer{Org: &nsOrgResolver{orbsByOrg: ownedBy(srcOrgUUID, "bcbsm")}, Orb: ow}
	rep, err := sy.SyncOrbs(context.Background(), sameNSManifest("bcbsm"),
		Options{Apply: true, DestOrbNamespace: "bcbsm-new", DestOrgSlug: destSlugApp}, nil, "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ow.publishCalls) != 1 || ow.publishCalls[0] != "bcbsm-new/tool@1.0.0" {
		t.Errorf("expected publish into the new namespace, got %v", ow.publishCalls)
	}
	var notice *Action
	for i := range rep.Actions {
		if rep.Actions[i].Target == "config-rewrite-notice" {
			notice = &rep.Actions[i]
		}
	}
	if notice == nil || !strings.Contains(notice.Detail, "bcbsm/tool") || !strings.Contains(notice.Detail, "bcbsm-new/tool") {
		t.Errorf("expected a rewrite notice mapping bcbsm/tool → bcbsm-new/tool, got %+v", notice)
	}
}

// ---------------------------------------------------------------------------
// Runner resource classes
// ---------------------------------------------------------------------------

func TestSyncRunner_SameNamespace_SourceOwned_ManualNoCreate(t *testing.T) {
	rw := &nsRunnerWriter{}
	sy := &Syncer{Org: &nsOrgResolver{orbsByOrg: ownedBy(srcOrgUUID, "bcbsm")}, Runner: rw}
	rep, err := sy.SyncRunnerResourceClasses(context.Background(), sameNSManifest("bcbsm"),
		Options{Apply: true, DestRunnerNamespace: "bcbsm", DestOrgSlug: destSlugApp})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rw.createCalls) != 0 {
		t.Errorf("must not create classes while the source owns the namespace: %v", rw.createCalls)
	}
	a := actionFor(rep, "runner-namespace")
	if a == nil || a.Status != "manual" || !strings.Contains(a.Detail, wantCommand("bcbsm")) {
		t.Fatalf("expected manual action with the exact command, got %+v", rep.Actions)
	}
	if !strings.Contains(a.Detail, "attributed to the source org") {
		t.Errorf("detail should explain class attribution: %s", a.Detail)
	}
}

func TestSyncRunner_SameNamespace_DestOwned_CreatesMissingClasses(t *testing.T) {
	rw := &nsRunnerWriter{destClasses: []string{"bcbsm/fast"}}
	rw.getResourceClasses = func(string) ([]apirunner.ResourceClass, error) {
		return []apirunner.ResourceClass{{ResourceClass: "bcbsm/fast"}}, nil
	}
	sy := &Syncer{Org: &nsOrgResolver{orbsByOrg: ownedBy(destOrgUUID, "bcbsm")}, Runner: rw}
	rep, err := sy.SyncRunnerResourceClasses(context.Background(), sameNSManifest("bcbsm"),
		Options{Apply: true, DestRunnerNamespace: "bcbsm", DestOrgSlug: destSlugApp})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rw.createCalls) != 1 || rw.createCalls[0] != "bcbsm/slow" {
		t.Errorf("expected only bcbsm/slow created (same name, no rewrite), got %v", rw.createCalls)
	}
	c := rep.Counts()
	if c["exists"] != 1 || c["created"] != 1 || c["manual"] != 0 {
		t.Errorf("counts = %v, want exists=1 created=1 manual=0 (%+v)", c, rep.Actions)
	}
}

func TestSyncRunner_SameNamespace_ClassAttributedToSource_Manual(t *testing.T) {
	// Namespace is dest-owned, "bcbsm/fast" is in the namespace but attributed to
	// the source org (classes do not follow a namespace transfer).
	rw := &nsRunnerWriter{} // dest org owns no classes
	rw.getResourceClasses = func(string) ([]apirunner.ResourceClass, error) {
		return []apirunner.ResourceClass{{ResourceClass: "bcbsm/fast"}}, nil
	}
	sy := &Syncer{Org: &nsOrgResolver{orbsByOrg: ownedBy(destOrgUUID, "bcbsm")}, Runner: rw}
	rep, _ := sy.SyncRunnerResourceClasses(context.Background(), sameNSManifest("bcbsm"),
		Options{Apply: true, DestRunnerNamespace: "bcbsm", DestOrgSlug: destSlugApp})

	var fast *Action
	for i := range rep.Actions {
		if rep.Actions[i].Target == "bcbsm/fast" {
			fast = &rep.Actions[i]
		}
	}
	if fast == nil || fast.Status != "manual" || !strings.Contains(fast.Detail, "attributed to another org") {
		t.Fatalf("expected manual 'attributed to another org' for bcbsm/fast, got %+v", fast)
	}
	for _, c := range rw.createCalls {
		if c == "bcbsm/fast" {
			t.Errorf("must not attempt to create a class that exists under the source org")
		}
	}
	if len(rw.createCalls) != 1 || rw.createCalls[0] != "bcbsm/slow" {
		t.Errorf("bcbsm/slow should still be created, got %v", rw.createCalls)
	}
}

func TestSyncRunner_SameNamespace_Create409_Manual(t *testing.T) {
	rw := &nsRunnerWriter{}
	rw.createResourceClass = func(rc, _ string) (*apirunner.ResourceClass, error) {
		return nil, errors.New("runner: CreateResourceClass: HTTP 409: resource class already exists")
	}
	sy := &Syncer{Org: &nsOrgResolver{orbsByOrg: ownedBy(destOrgUUID, "bcbsm")}, Runner: rw}
	rep, _ := sy.SyncRunnerResourceClasses(context.Background(), sameNSManifest("bcbsm"),
		Options{Apply: true, DestRunnerNamespace: "bcbsm", DestOrgSlug: destSlugApp})
	if n := rep.Counts()["manual"]; n != 2 {
		t.Fatalf("409 on a same-name class must be manual (2 classes), got %d: %+v", n, rep.Actions)
	}
	if n := rep.Counts()["exists"]; n != 0 {
		t.Errorf("a 409 must not be reported as 'exists' for a same-name namespace, got %d", n)
	}
	for _, a := range rep.Actions {
		if !strings.Contains(a.Detail, "attributed to another org") {
			t.Errorf("manual detail should explain attribution: %s", a.Detail)
		}
	}
}

func TestSyncRunner_SameNamespace_UnknownOwner_NamespaceInaccessible_Manual(t *testing.T) {
	rw := &nsRunnerWriter{}
	rw.getResourceClasses = func(string) ([]apirunner.ResourceClass, error) {
		return nil, errors.New("runner: GetResourceClassesByNamespace: HTTP 403: forbidden")
	}
	sy := &Syncer{Org: &nsOrgResolver{}, Runner: rw}
	rep, _ := sy.SyncRunnerResourceClasses(context.Background(), sameNSManifest("bcbsm"),
		Options{Apply: true, DestRunnerNamespace: "bcbsm", DestOrgSlug: destSlugApp})
	if len(rw.createCalls) != 0 {
		t.Errorf("must not create when the namespace is inaccessible: %v", rw.createCalls)
	}
	a := actionFor(rep, "runner-namespace")
	if a == nil || a.Status != "manual" || !strings.Contains(a.Detail, wantCommand("bcbsm")) {
		t.Errorf("expected manual action with the command, got %+v", rep.Actions)
	}
}

func TestSyncRunner_DifferentNamespace_RewriteNotice(t *testing.T) {
	rw := &fakeRunnerWriter{}
	sy := &Syncer{Org: &fakeOrgResolver{}, Runner: rw}
	rep, _ := sy.SyncRunnerResourceClasses(context.Background(), sameNSManifest("bcbsm"),
		Options{Apply: true, DestRunnerNamespace: "bcbsm-new"})
	if len(rw.createCalls) != 2 || rw.createCalls[0] != "bcbsm-new/fast" {
		t.Errorf("classes should be created under the new namespace, got %v", rw.createCalls)
	}
	var notice *Action
	for i := range rep.Actions {
		if rep.Actions[i].Target == "config-rewrite-notice" {
			notice = &rep.Actions[i]
		}
	}
	if notice == nil || notice.Status != "manual" ||
		!strings.Contains(notice.Detail, "resource_class:") ||
		!strings.Contains(notice.Detail, "bcbsm/fast  →  bcbsm-new/fast") {
		t.Errorf("expected a resource_class rewrite notice listing the mapping, got %+v", notice)
	}
}

func TestSyncRunner_SameNamespace_NoNoticeWhenUnchanged(t *testing.T) {
	// With an identical namespace there is nothing to rewrite.
	rw := &nsRunnerWriter{}
	sy := &Syncer{Org: &nsOrgResolver{orbsByOrg: ownedBy(destOrgUUID, "bcbsm")}, Runner: rw}
	rep, _ := sy.SyncRunnerResourceClasses(context.Background(), sameNSManifest("bcbsm"),
		Options{Apply: false, DestRunnerNamespace: "bcbsm", DestOrgSlug: destSlugApp})
	for _, a := range rep.Actions {
		if a.Target == "config-rewrite-notice" {
			t.Errorf("no rewrite notice expected for an unchanged namespace: %+v", a)
		}
	}
	if len(rw.createCalls) != 0 {
		t.Errorf("dry run must not create: %v", rw.createCalls)
	}
}

func TestFlagOrgCoords(t *testing.T) {
	cases := []struct{ in, vcs, name string }{
		{"circleci/abc-123", "circleci", "abc-123"},
		{"gh/acme", "github", "acme"},
		{"circleci/", "", ""},
		{"", "", ""},
	}
	for _, tc := range cases {
		v, n := flagOrgCoords(tc.in)
		if v != tc.vcs || n != tc.name {
			t.Errorf("flagOrgCoords(%q) = (%q,%q), want (%q,%q)", tc.in, v, n, tc.vcs, tc.name)
		}
	}
}
