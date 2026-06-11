package syncer

import (
	"strings"
	"testing"

	"github.com/AwesomeCICD/circleci-org-migration-cli/api/project"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// sshKeyManifest builds a manifest with one project containing the given
// additional SSH keys. The source org is always "gh/acme" so tests can use
// an explicit project mapping.
func sshKeyManifest(srcSlug string, keys ...manifest.ProjectSSHKey) *manifest.Manifest {
	return &manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		Source: manifest.Source{
			Org: manifest.Org{Slug: "gh/acme"},
		},
		Projects: []manifest.Project{
			{Slug: srcSlug, Name: srcSlug, SSHKeys: keys},
		},
	}
}

// sshKeyMapping builds a Mapping that routes srcSlug → srcSlug (identity) so
// that the OAuth project sync path (GetProject → existing project) is exercised.
// Using explicit project entries avoids the "no mapping → manual" guard in
// SyncProjects that would otherwise skip the project entirely.
func sshKeyMapping(srcSlug string) *manifest.Mapping {
	return &manifest.Mapping{
		Org:      manifest.OrgMapping{From: "gh/acme", To: "gh/acme"},
		Projects: map[string]string{srcSlug: srcSlug},
	}
}

// sshKeyBundle builds a SecretBundle with one or more BundleSSHKeys for the
// given project slug.
func sshKeyBundle(projectSlug string, keys ...manifest.BundleSSHKey) *manifest.SecretBundle {
	b := manifest.NewSecretBundle()
	for _, k := range keys {
		b.AddSSHKey(projectSlug, k)
	}
	return b
}

// sshKeyActions returns all actions with kind "project-ssh-key".
func sshKeyActions(rep *Report) []Action {
	return actionsOfKind(rep, "project-ssh-key")
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestSyncProjects_SSHKey_AddedFromBundle verifies that an SSH key present in
// the manifest and in the bundle is added to the destination when Apply=true.
func TestSyncProjects_SSHKey_AddedFromBundle(t *testing.T) {
	const srcSlug = "gh/acme/web"
	const fp = "SHA256:abc123"
	const hostname = "github.com"
	const privKey = "-----BEGIN OPENSSH PRIVATE KEY-----\ntest\n-----END OPENSSH PRIVATE KEY-----"

	manifestKey := manifest.ProjectSSHKey{Fingerprint: fp, Hostname: hostname}
	bundleKey := manifest.BundleSSHKey{Fingerprint: fp, Hostname: hostname, PrivateKey: privKey}

	fw := &fakeProjectWriter{}
	sy := newSyncerProjects(fw)

	m := sshKeyManifest(srcSlug, manifestKey)
	bundle := sshKeyBundle(srcSlug, bundleKey)

	rep, err := sy.SyncProjects(m, bundle, sshKeyMapping(srcSlug), Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !fw.hasCalled("AddAdditionalSSHKey") {
		t.Error("AddAdditionalSSHKey must be called when Apply=true and key is in the bundle")
	}

	calls := fw.callsTo("AddAdditionalSSHKey")
	if len(calls) == 0 {
		t.Fatal("expected at least 1 AddAdditionalSSHKey call, got 0")
	}
	// args: [slug, hostname, privateKey]
	if calls[0].args[0] != srcSlug {
		t.Errorf("AddAdditionalSSHKey slug: got %q, want %q", calls[0].args[0], srcSlug)
	}
	if calls[0].args[1] != hostname {
		t.Errorf("AddAdditionalSSHKey hostname: got %q, want %q", calls[0].args[1], hostname)
	}
	if calls[0].args[2] != privKey {
		t.Errorf("AddAdditionalSSHKey privateKey: got %q, want %q", calls[0].args[2], privKey)
	}

	acts := sshKeyActions(rep)
	if len(acts) != 1 {
		t.Fatalf("expected 1 ssh-key action, got %d", len(acts))
	}
	if acts[0].Status != "set" {
		t.Errorf("action status: got %q, want %q", acts[0].Status, "set")
	}
}

// TestSyncProjects_SSHKey_DryRun_NoAdd verifies that in dry-run mode
// AddAdditionalSSHKey is NOT called but a "set" (would add) action is recorded.
func TestSyncProjects_SSHKey_DryRun_NoAdd(t *testing.T) {
	const srcSlug = "gh/acme/web"
	const fp = "SHA256:abc123"

	key := manifest.ProjectSSHKey{Fingerprint: fp, Hostname: "github.com"}
	bk := manifest.BundleSSHKey{Fingerprint: fp, Hostname: "github.com", PrivateKey: "priv-key"}

	fw := &fakeProjectWriter{}
	sy := newSyncerProjects(fw)

	m := sshKeyManifest(srcSlug, key)
	bundle := sshKeyBundle(srcSlug, bk)

	rep, err := sy.SyncProjects(m, bundle, sshKeyMapping(srcSlug), Options{Apply: false})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fw.hasCalled("AddAdditionalSSHKey") {
		t.Error("AddAdditionalSSHKey must NOT be called in dry-run mode")
	}

	acts := sshKeyActions(rep)
	if len(acts) != 1 {
		t.Fatalf("expected 1 ssh-key action, got %d", len(acts))
	}
	if acts[0].Status != "set" {
		t.Errorf("dry-run action status: got %q, want %q", acts[0].Status, "set")
	}
	if !strings.Contains(acts[0].Detail, "would add") {
		t.Errorf("dry-run detail %q should contain 'would add'", acts[0].Detail)
	}
}

// TestSyncProjects_SSHKey_Idempotent_SkipWhenFingerprintExists verifies that
// if a key with the same fingerprint already exists on the destination,
// AddAdditionalSSHKey is not called and the action has status "exists".
func TestSyncProjects_SSHKey_Idempotent_SkipWhenFingerprintExists(t *testing.T) {
	const srcSlug = "gh/acme/web"
	const fp = "SHA256:abc123"

	key := manifest.ProjectSSHKey{Fingerprint: fp, Hostname: "github.com"}
	bk := manifest.BundleSSHKey{Fingerprint: fp, Hostname: "github.com", PrivateKey: "priv-key"}

	fw := &fakeProjectWriter{
		listAdditionalSSHKeys: func(slug string) ([]project.SSHKey, error) {
			return []project.SSHKey{
				{Fingerprint: fp, Hostname: "github.com"},
			}, nil
		},
	}
	sy := newSyncerProjects(fw)

	m := sshKeyManifest(srcSlug, key)
	bundle := sshKeyBundle(srcSlug, bk)

	rep, err := sy.SyncProjects(m, bundle, sshKeyMapping(srcSlug), Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fw.hasCalled("AddAdditionalSSHKey") {
		t.Error("AddAdditionalSSHKey must NOT be called when key already exists on destination")
	}

	acts := sshKeyActions(rep)
	if len(acts) != 1 {
		t.Fatalf("expected 1 ssh-key action, got %d", len(acts))
	}
	if acts[0].Status != "exists" {
		t.Errorf("action status: got %q, want %q", acts[0].Status, "exists")
	}
}

// TestSyncProjects_SSHKey_ManualWarning_WhenPrivateKeyMissing verifies that
// if the manifest has an SSH key but the bundle has no corresponding private
// key, a "manual" action is emitted and no API call is made.
func TestSyncProjects_SSHKey_ManualWarning_WhenPrivateKeyMissing(t *testing.T) {
	const srcSlug = "gh/acme/web"
	const fp = "SHA256:abc123"

	key := manifest.ProjectSSHKey{Fingerprint: fp, Hostname: "github.com"}

	fw := &fakeProjectWriter{}
	sy := newSyncerProjects(fw)

	m := sshKeyManifest(srcSlug, key)
	// Bundle has no SSH keys for this project.
	bundle := manifest.NewSecretBundle()

	rep, err := sy.SyncProjects(m, bundle, sshKeyMapping(srcSlug), Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fw.hasCalled("AddAdditionalSSHKey") {
		t.Error("AddAdditionalSSHKey must NOT be called when private key is not in the bundle")
	}

	acts := sshKeyActions(rep)
	if len(acts) != 1 {
		t.Fatalf("expected 1 ssh-key action, got %d", len(acts))
	}
	if acts[0].Status != "manual" {
		t.Errorf("action status: got %q, want %q", acts[0].Status, "manual")
	}
	if !strings.Contains(acts[0].Detail, "private key not captured") {
		t.Errorf("detail %q should mention 'private key not captured'", acts[0].Detail)
	}
}

// TestSyncProjects_SSHKey_ManualWarning_NilBundle verifies that when no bundle
// is provided at all, an SSH key in the manifest results in a "manual" action.
func TestSyncProjects_SSHKey_ManualWarning_NilBundle(t *testing.T) {
	const srcSlug = "gh/acme/web"
	key := manifest.ProjectSSHKey{Fingerprint: "SHA256:xyz", Hostname: "gitlab.com"}

	fw := &fakeProjectWriter{}
	sy := newSyncerProjects(fw)

	m := sshKeyManifest(srcSlug, key)

	rep, err := sy.SyncProjects(m, nil, sshKeyMapping(srcSlug), Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fw.hasCalled("AddAdditionalSSHKey") {
		t.Error("AddAdditionalSSHKey must NOT be called when bundle is nil")
	}

	acts := sshKeyActions(rep)
	if len(acts) != 1 {
		t.Fatalf("expected 1 ssh-key action, got %d", len(acts))
	}
	if acts[0].Status != "manual" {
		t.Errorf("action status: got %q, want %q", acts[0].Status, "manual")
	}
}

// TestSyncProjects_SSHKey_NoKeys_NoActions verifies that a project with no
// SSH keys in the manifest produces no ssh-key actions at all.
func TestSyncProjects_SSHKey_NoKeys_NoActions(t *testing.T) {
	const srcSlug = "gh/acme/web"

	fw := &fakeProjectWriter{}
	sy := newSyncerProjects(fw)

	m := &manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		Source:        manifest.Source{Org: manifest.Org{Slug: "gh/acme"}},
		Projects: []manifest.Project{
			{Slug: srcSlug, Name: srcSlug}, // no SSHKeys
		},
	}

	rep, err := sy.SyncProjects(m, nil, sshKeyMapping(srcSlug), Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	acts := sshKeyActions(rep)
	if len(acts) != 0 {
		t.Errorf("expected no ssh-key actions, got %d: %v", len(acts), acts)
	}
	if fw.hasCalled("AddAdditionalSSHKey") {
		t.Error("AddAdditionalSSHKey must NOT be called when project has no SSH keys")
	}
}

// TestSyncProjects_SSHKey_ErrorFromAdd_IsErrorAction verifies that an
// AddAdditionalSSHKey API failure is recorded as an "error" action and does
// not propagate as a top-level error.
func TestSyncProjects_SSHKey_ErrorFromAdd_IsErrorAction(t *testing.T) {
	const srcSlug = "gh/acme/web"
	const fp = "SHA256:abc123"

	key := manifest.ProjectSSHKey{Fingerprint: fp, Hostname: "github.com"}
	bk := manifest.BundleSSHKey{Fingerprint: fp, Hostname: "github.com", PrivateKey: "priv"}

	fw := &fakeProjectWriter{
		addAdditionalSSHKey: func(slug, hostname, privateKey string) error {
			return errFake("add ssh key failed")
		},
	}
	sy := newSyncerProjects(fw)

	m := sshKeyManifest(srcSlug, key)
	bundle := sshKeyBundle(srcSlug, bk)

	rep, err := sy.SyncProjects(m, bundle, sshKeyMapping(srcSlug), Options{Apply: true})
	if err != nil {
		t.Fatalf("AddAdditionalSSHKey error must not propagate, got: %v", err)
	}

	acts := sshKeyActions(rep)
	if len(acts) != 1 {
		t.Fatalf("expected 1 ssh-key action, got %d", len(acts))
	}
	if acts[0].Status != "error" {
		t.Errorf("action status: got %q, want %q", acts[0].Status, "error")
	}
}

// TestSyncProjects_SSHKey_MultipleKeys verifies that multiple SSH keys on a
// project are each processed independently:
//   - fp1 already exists on the destination → "exists" (no API call).
//   - fp2 is new and in the bundle → added ("set").
//   - fp1 is NOT in the bundle at all, but that doesn't matter because it is
//     detected as existing on the destination first.
func TestSyncProjects_SSHKey_MultipleKeys(t *testing.T) {
	const srcSlug = "gh/acme/web"
	const fp1 = "SHA256:aaa111"
	const fp2 = "SHA256:bbb222"

	keys := []manifest.ProjectSSHKey{
		{Fingerprint: fp1, Hostname: "github.com"},
		{Fingerprint: fp2, Hostname: "gitlab.com"},
	}
	bk2 := manifest.BundleSSHKey{Fingerprint: fp2, Hostname: "gitlab.com", PrivateKey: "priv2"}

	fw := &fakeProjectWriter{
		listAdditionalSSHKeys: func(slug string) ([]project.SSHKey, error) {
			return []project.SSHKey{
				{Fingerprint: fp1, Hostname: "github.com"}, // already present
			}, nil
		},
	}
	sy := newSyncerProjects(fw)

	m := sshKeyManifest(srcSlug, keys...)
	// Bundle only has bk2; fp1 has no bundle entry but is already on dest.
	bundle := sshKeyBundle(srcSlug, bk2)

	rep, err := sy.SyncProjects(m, bundle, sshKeyMapping(srcSlug), Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	acts := sshKeyActions(rep)
	if len(acts) != 2 {
		t.Fatalf("expected 2 ssh-key actions, got %d", len(acts))
	}

	existsCount, setCount := 0, 0
	for _, a := range acts {
		switch a.Status {
		case "exists":
			existsCount++
		case "set":
			setCount++
		}
	}
	if existsCount != 1 {
		t.Errorf("expected 1 'exists' action, got %d", existsCount)
	}
	if setCount != 1 {
		t.Errorf("expected 1 'set' action, got %d", setCount)
	}

	// AddAdditionalSSHKey called once (only for fp2).
	addCalls := fw.callsTo("AddAdditionalSSHKey")
	if len(addCalls) != 1 {
		t.Fatalf("expected 1 AddAdditionalSSHKey call, got %d", len(addCalls))
	}
	if addCalls[0].args[0] != srcSlug {
		t.Errorf("AddAdditionalSSHKey slug: got %q, want %q", addCalls[0].args[0], srcSlug)
	}
}

// errFake is a minimal error type used in tests to avoid named-type clashes.
type errFakeType string

func (e errFakeType) Error() string { return string(e) }

func errFake(msg string) error { return errFakeType(msg) }
