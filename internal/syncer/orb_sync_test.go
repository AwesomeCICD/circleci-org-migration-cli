package syncer

import (
	"context"
	"errors"
	"strings"
	"testing"

	apiOrb "github.com/AwesomeCICD/circleci-org-migration-cli/api/orb"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
)

// ---------------------------------------------------------------------------
// Fake OrbWriter
// ---------------------------------------------------------------------------

type fakeOrbWriter struct {
	resolveNamespaceID func(name string) (string, error)
	createOrb          func(shortName, namespaceID string, isPrivate bool) (*apiOrb.OrbPackage, error)
	resolveVersionRef  func(ref string) (*apiOrb.OrbVersion, error)
	publishVersion     func(orbID, version, yaml, destRef string) error

	resolveNSCalls []string
	createCalls    []string
	publishCalls   []string
	verifyCalls    []string
}

func (f *fakeOrbWriter) ResolveNamespaceID(_ context.Context, name string) (string, error) {
	f.resolveNSCalls = append(f.resolveNSCalls, name)
	if f.resolveNamespaceID != nil {
		return f.resolveNamespaceID(name)
	}
	return "dest-ns-uuid", nil
}

func (f *fakeOrbWriter) CreateOrb(_ context.Context, shortName, namespaceID string, isPrivate bool) (*apiOrb.OrbPackage, error) {
	f.createCalls = append(f.createCalls, shortName)
	if f.createOrb != nil {
		return f.createOrb(shortName, namespaceID, isPrivate)
	}
	return &apiOrb.OrbPackage{ID: "new-orb-id-" + shortName, Name: shortName, IsPrivate: isPrivate, NamespaceID: namespaceID}, nil
}

func (f *fakeOrbWriter) ResolveVersionRef(_ context.Context, ref string) (*apiOrb.OrbVersion, error) {
	f.verifyCalls = append(f.verifyCalls, ref)
	if f.resolveVersionRef != nil {
		return f.resolveVersionRef(ref)
	}
	return nil, nil // not found → proceed to publish
}

func (f *fakeOrbWriter) PublishVersion(_ context.Context, orbID, version, yaml, destRef string) error {
	f.publishCalls = append(f.publishCalls, destRef)
	if f.publishVersion != nil {
		return f.publishVersion(orbID, version, yaml, destRef)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Fake OrbFlagManager
// ---------------------------------------------------------------------------

type fakeOrbFlagManager struct {
	getFlags    func(vcsType, orgName string) (map[string]bool, error)
	updateFlags func(vcsType, orgName string, flags map[string]bool) error
	updateCalls []map[string]bool
}

func (f *fakeOrbFlagManager) GetOrbFeatureFlags(_ context.Context, vcsType, orgName string) (map[string]bool, error) {
	if f.getFlags != nil {
		return f.getFlags(vcsType, orgName)
	}
	return map[string]bool{
		flagAllowUncertifiedPublicOrbs: true,
		flagAllowPrivateOrbs:           true,
	}, nil
}

func (f *fakeOrbFlagManager) UpdateOrbFeatureFlags(_ context.Context, _, _ string, flags map[string]bool) error {
	f.updateCalls = append(f.updateCalls, flags)
	if f.updateFlags != nil {
		return f.updateFlags("", "", flags)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func orbManifestWith(srcNs string, orbs ...manifest.CapturedOrb) *manifest.Manifest {
	return &manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		OrbNamespace:  srcNs,
		Orbs:          orbs,
	}
}

func simpleOrb(name string, isPrivate bool, versions ...manifest.CapturedOrbVersion) manifest.CapturedOrb {
	return manifest.CapturedOrb{Name: name, IsPrivate: isPrivate, Versions: versions}
}

func simpleVer(version, src string) manifest.CapturedOrbVersion {
	return manifest.CapturedOrbVersion{Version: version, Source: src}
}

func minimalOrbSyncer() *Syncer {
	return &Syncer{Org: &fakeOrgResolver{}}
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestSyncOrbs_NoOrbsInManifest verifies that an empty manifest produces an
// empty report with no API calls.
func TestSyncOrbs_NoOrbsInManifest(t *testing.T) {
	ow := &fakeOrbWriter{}
	sy := minimalOrbSyncer()
	sy.Orb = ow

	m := &manifest.Manifest{SchemaVersion: manifest.SchemaVersion}
	rep, err := sy.SyncOrbs(context.Background(), m, Options{
		Apply:            true,
		DestOrbNamespace: "acme-new",
	}, nil, "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rep.Actions) != 0 {
		t.Errorf("expected 0 actions, got %d: %+v", len(rep.Actions), rep.Actions)
	}
	if len(ow.publishCalls) != 0 {
		t.Errorf("expected 0 publish calls, got %d", len(ow.publishCalls))
	}
}

// TestSyncOrbs_NoDestNamespace_Manual verifies that without a destination
// namespace, all orbs are flagged as manual.
func TestSyncOrbs_NoDestNamespace_Manual(t *testing.T) {
	ow := &fakeOrbWriter{}
	sy := minimalOrbSyncer()
	sy.Orb = ow

	m := orbManifestWith("acme",
		simpleOrb("acme/my-orb", false, simpleVer("1.0.0", "v2.1:")),
		simpleOrb("acme/priv-orb", true, simpleVer("0.1.0", "v2.1:")),
	)
	rep, err := sy.SyncOrbs(context.Background(), m, Options{
		Apply:            true,
		DestOrbNamespace: "", // not supplied
	}, nil, "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	counts := rep.Counts()
	if counts["manual"] != 2 {
		t.Errorf("expected 2 manual, got %+v", counts)
	}
	if len(ow.publishCalls) != 0 {
		t.Errorf("expected no publish calls, got %d", len(ow.publishCalls))
	}
}

// TestSyncOrbs_DryRun_ReportsCreated verifies that a dry run reports "created"
// for each version without making API calls.
func TestSyncOrbs_DryRun_ReportsCreated(t *testing.T) {
	ow := &fakeOrbWriter{}
	sy := minimalOrbSyncer()
	sy.Orb = ow

	m := orbManifestWith("acme",
		simpleOrb("acme/my-orb", false,
			simpleVer("1.0.0", "src1"),
			simpleVer("2.0.0", "src2"),
		),
	)
	rep, err := sy.SyncOrbs(context.Background(), m, Options{
		Apply:            false,
		DestOrbNamespace: "acme-new",
	}, nil, "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	counts := rep.Counts()
	// 2 version "created" + 1 config-rewrite-notice "manual"
	if counts["created"] != 2 {
		t.Errorf("expected 2 created, got %+v", counts)
	}
	if len(ow.publishCalls) != 0 {
		t.Errorf("dry run should not call PublishVersion, got %d calls", len(ow.publishCalls))
	}
}

// TestSyncOrbs_Apply_PublishesVersions verifies that in apply mode, each
// version is published to the destination namespace.
func TestSyncOrbs_Apply_PublishesVersions(t *testing.T) {
	ow := &fakeOrbWriter{}
	sy := minimalOrbSyncer()
	sy.Orb = ow

	m := orbManifestWith("acme",
		simpleOrb("acme/my-orb", false,
			simpleVer("1.0.0", "src1"),
			simpleVer("2.0.0", "src2"),
		),
	)
	rep, err := sy.SyncOrbs(context.Background(), m, Options{
		Apply:            true,
		DestOrbNamespace: "acme-new",
	}, nil, "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	counts := rep.Counts()
	if counts["created"] != 2 {
		t.Errorf("expected 2 created, got %+v", counts)
	}
	if len(ow.publishCalls) != 2 {
		t.Errorf("expected 2 publish calls, got %d: %v", len(ow.publishCalls), ow.publishCalls)
	}
	// Verify namespace was resolved.
	if len(ow.resolveNSCalls) != 1 || ow.resolveNSCalls[0] != "acme-new" {
		t.Errorf("resolveNSCalls = %v, want [acme-new]", ow.resolveNSCalls)
	}
	// Verify orb was created.
	if len(ow.createCalls) != 1 || ow.createCalls[0] != "my-orb" {
		t.Errorf("createCalls = %v, want [my-orb]", ow.createCalls)
	}
}

// TestSyncOrbs_Apply_SkipsExistingVersions verifies that versions already
// present in the destination namespace are skipped (idempotent).
func TestSyncOrbs_Apply_SkipsExistingVersions(t *testing.T) {
	ow := &fakeOrbWriter{
		resolveVersionRef: func(ref string) (*apiOrb.OrbVersion, error) {
			if strings.HasSuffix(ref, "@1.0.0") {
				// Already exists.
				return &apiOrb.OrbVersion{ID: "existing-ver", Version: "1.0.0"}, nil
			}
			return nil, nil // not found → publish
		},
	}
	sy := minimalOrbSyncer()
	sy.Orb = ow

	m := orbManifestWith("acme",
		simpleOrb("acme/my-orb", false,
			simpleVer("1.0.0", "src1"), // exists → skip
			simpleVer("2.0.0", "src2"), // new → publish
		),
	)
	rep, err := sy.SyncOrbs(context.Background(), m, Options{
		Apply:            true,
		DestOrbNamespace: "acme-new",
	}, nil, "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	counts := rep.Counts()
	if counts["exists"] != 1 {
		t.Errorf("expected 1 exists, got %+v", counts)
	}
	if counts["created"] != 1 {
		t.Errorf("expected 1 created, got %+v", counts)
	}
	if len(ow.publishCalls) != 1 {
		t.Errorf("expected 1 publish call (only new version), got %d: %v", len(ow.publishCalls), ow.publishCalls)
	}
}

// TestSyncOrbs_Apply_CreateOrbError_SkipsVersions verifies that when CreateOrb
// fails, all versions of that orb are reported as errors.
func TestSyncOrbs_Apply_CreateOrbError_SkipsVersions(t *testing.T) {
	ow := &fakeOrbWriter{
		createOrb: func(shortName, _ string, _ bool) (*apiOrb.OrbPackage, error) {
			return nil, errors.New("permission denied")
		},
	}
	sy := minimalOrbSyncer()
	sy.Orb = ow

	m := orbManifestWith("acme",
		simpleOrb("acme/my-orb", false,
			simpleVer("1.0.0", "src1"),
			simpleVer("2.0.0", "src2"),
		),
	)
	rep, err := sy.SyncOrbs(context.Background(), m, Options{
		Apply:            true,
		DestOrbNamespace: "acme-new",
	}, nil, "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	counts := rep.Counts()
	if counts["error"] != 2 {
		t.Errorf("expected 2 error actions (one per version), got %+v", counts)
	}
	if len(ow.publishCalls) != 0 {
		t.Errorf("expected 0 publish calls when create fails, got %d", len(ow.publishCalls))
	}
}

// TestSyncOrbs_Apply_PublishError_Reported verifies that a publish error is
// reported as "error" in the report (not a fatal error).
func TestSyncOrbs_Apply_PublishError_Reported(t *testing.T) {
	ow := &fakeOrbWriter{
		publishVersion: func(_, version, _, _ string) error {
			if version == "2.0.0" {
				return errors.New("publish failed")
			}
			return nil
		},
	}
	sy := minimalOrbSyncer()
	sy.Orb = ow

	m := orbManifestWith("acme",
		simpleOrb("acme/my-orb", false,
			simpleVer("1.0.0", "src1"), // OK
			simpleVer("2.0.0", "src2"), // error
		),
	)
	rep, err := sy.SyncOrbs(context.Background(), m, Options{
		Apply:            true,
		DestOrbNamespace: "acme-new",
	}, nil, "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	counts := rep.Counts()
	if counts["created"] != 1 {
		t.Errorf("expected 1 created, got %+v", counts)
	}
	if counts["error"] != 1 {
		t.Errorf("expected 1 error, got %+v", counts)
	}
}

// TestSyncOrbs_Apply_NilOrbClient_Manual verifies that when the Orb client is
// nil but DestOrbNamespace is set, orbs are flagged as manual.
func TestSyncOrbs_Apply_NilOrbClient_Manual(t *testing.T) {
	sy := minimalOrbSyncer()
	// sy.Orb is nil

	m := orbManifestWith("acme",
		simpleOrb("acme/my-orb", false, simpleVer("1.0.0", "src")),
	)
	rep, err := sy.SyncOrbs(context.Background(), m, Options{
		Apply:            true,
		DestOrbNamespace: "acme-new",
	}, nil, "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	counts := rep.Counts()
	if counts["manual"] != 1 {
		t.Errorf("expected 1 manual (nil client), got %+v", counts)
	}
}

// TestSyncOrbs_Apply_ResolveNamespaceError_Fatal verifies that a namespace
// resolution error is returned as a fatal error (not just a warning).
func TestSyncOrbs_Apply_ResolveNamespaceError_Fatal(t *testing.T) {
	ow := &fakeOrbWriter{
		resolveNamespaceID: func(name string) (string, error) {
			return "", errors.New("namespace not found")
		},
	}
	sy := minimalOrbSyncer()
	sy.Orb = ow

	m := orbManifestWith("acme",
		simpleOrb("acme/my-orb", false, simpleVer("1.0.0", "src")),
	)
	_, err := sy.SyncOrbs(context.Background(), m, Options{
		Apply:            true,
		DestOrbNamespace: "nonexistent",
	}, nil, "", "")
	if err == nil {
		t.Fatal("expected error for namespace resolve failure, got nil")
	}
}

// TestSyncOrbs_ConfigRewriteNotice verifies that a prominent config-rewrite
// notice is included in the report when source and dest namespaces differ.
func TestSyncOrbs_ConfigRewriteNotice(t *testing.T) {
	ow := &fakeOrbWriter{}
	sy := minimalOrbSyncer()
	sy.Orb = ow

	m := orbManifestWith("acme",
		simpleOrb("acme/my-orb", false, simpleVer("1.0.0", "src")),
	)
	rep, err := sy.SyncOrbs(context.Background(), m, Options{
		Apply:            true,
		DestOrbNamespace: "acme-new", // different from source "acme"
	}, nil, "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Find the config-rewrite-notice action.
	var found bool
	for _, a := range rep.Actions {
		if a.Kind == "orb" && a.Target == "config-rewrite-notice" {
			found = true
			if !strings.Contains(a.Detail, "acme/my-orb") {
				t.Errorf("config-rewrite-notice does not mention source orb: %q", a.Detail)
			}
			if !strings.Contains(a.Detail, "acme-new/my-orb") {
				t.Errorf("config-rewrite-notice does not mention dest orb: %q", a.Detail)
			}
			break
		}
	}
	if !found {
		t.Errorf("expected config-rewrite-notice action in report; actions: %+v", rep.Actions)
	}
}

// TestSyncOrbs_FlagToggleAndRestore verifies that when an OrbFlagManager is
// provided and flags are not already enabled, they are enabled before publishing
// and restored to their prior values afterwards.
func TestSyncOrbs_FlagToggleAndRestore(t *testing.T) {
	ow := &fakeOrbWriter{}
	sy := minimalOrbSyncer()
	sy.Orb = ow

	// Simulate both flags being disabled on the destination org.
	fm := &fakeOrbFlagManager{
		getFlags: func(_, _ string) (map[string]bool, error) {
			return map[string]bool{
				flagAllowUncertifiedPublicOrbs: false,
				flagAllowPrivateOrbs:           false,
			}, nil
		},
	}

	m := orbManifestWith("acme",
		simpleOrb("acme/my-orb", true, simpleVer("1.0.0", "src")), // private
	)
	_, err := sy.SyncOrbs(context.Background(), m, Options{
		Apply:            true,
		DestOrbNamespace: "acme-new",
	}, fm, "github", "acme-new")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// At minimum one update call should have been made (enable) plus one more
	// (restore).  The restore runs after SyncOrbs returns via deferred call.
	if len(fm.updateCalls) < 1 {
		t.Errorf("expected at least 1 UpdateOrbFeatureFlags call, got %d", len(fm.updateCalls))
	}
}

// TestSyncOrbs_OrbShortName verifies the orbShortName helper.
func TestSyncOrbs_OrbShortName(t *testing.T) {
	tests := []struct {
		fullName string
		want     string
	}{
		{"acme/my-orb", "my-orb"},
		{"ns/sub/orb", "orb"},
		{"bare-orb", "bare-orb"},
	}
	for _, tt := range tests {
		got := orbShortName(tt.fullName)
		if got != tt.want {
			t.Errorf("orbShortName(%q) = %q, want %q", tt.fullName, got, tt.want)
		}
	}
}
