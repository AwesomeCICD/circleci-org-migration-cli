package manifest

import "testing"

// ---------------------------------------------------------------------------
// BundleSSHKey / AddSSHKey
// ---------------------------------------------------------------------------

// TestAddSSHKey_Append verifies that AddSSHKey appends a new key to the slice
// when no key with the same fingerprint already exists.
func TestAddSSHKey_Append(t *testing.T) {
	b := NewSecretBundle()
	k := BundleSSHKey{Fingerprint: "fp1=", Hostname: "github.com", PrivateKey: "pem1"}
	b.AddSSHKey("gh/acme/web", k)

	keys := b.SSHKeys["gh/acme/web"]
	if len(keys) != 1 {
		t.Fatalf("expected 1 key, got %d", len(keys))
	}
	if keys[0].Fingerprint != "fp1=" {
		t.Errorf("Fingerprint: got %q, want fp1=", keys[0].Fingerprint)
	}
	if keys[0].Hostname != "github.com" {
		t.Errorf("Hostname: got %q, want github.com", keys[0].Hostname)
	}
	if keys[0].PrivateKey != "pem1" {
		t.Errorf("PrivateKey: got %q, want pem1", keys[0].PrivateKey)
	}
}

// TestAddSSHKey_Upsert verifies that AddSSHKey replaces an existing entry when
// the same fingerprint is seen again (upsert semantics, no duplicates).
func TestAddSSHKey_Upsert(t *testing.T) {
	b := NewSecretBundle()
	b.AddSSHKey("gh/acme/web", BundleSSHKey{Fingerprint: "fp1=", Hostname: "github.com", PrivateKey: "old"})
	b.AddSSHKey("gh/acme/web", BundleSSHKey{Fingerprint: "fp1=", Hostname: "github.com", PrivateKey: "new"})

	keys := b.SSHKeys["gh/acme/web"]
	if len(keys) != 1 {
		t.Fatalf("expected 1 key (upsert), got %d", len(keys))
	}
	if keys[0].PrivateKey != "new" {
		t.Errorf("PrivateKey after upsert: got %q, want new", keys[0].PrivateKey)
	}
}

// TestAddSSHKey_MultipleKeys verifies that two keys with different fingerprints
// for the same project are stored independently.
func TestAddSSHKey_MultipleKeys(t *testing.T) {
	b := NewSecretBundle()
	b.AddSSHKey("gh/acme/web", BundleSSHKey{Fingerprint: "fp1=", Hostname: "github.com", PrivateKey: "pem1"})
	b.AddSSHKey("gh/acme/web", BundleSSHKey{Fingerprint: "fp2=", Hostname: "bitbucket.org", PrivateKey: "pem2"})

	keys := b.SSHKeys["gh/acme/web"]
	if len(keys) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(keys))
	}
}

// TestAddSSHKey_NilSSHKeysMap verifies that AddSSHKey initializes the SSHKeys
// map when the bundle's SSHKeys field is nil (e.g. loaded from an old bundle
// that predates this field).
func TestAddSSHKey_NilSSHKeysMap(t *testing.T) {
	b := &SecretBundle{SchemaVersion: SchemaVersion}
	// SSHKeys is intentionally nil here.
	b.AddSSHKey("gh/acme/web", BundleSSHKey{Fingerprint: "fp1=", PrivateKey: "pem"})

	if len(b.SSHKeys["gh/acme/web"]) != 1 {
		t.Errorf("expected 1 key, got %d", len(b.SSHKeys["gh/acme/web"]))
	}
}

// TestSecretBundleMerge_SSHKeys verifies that Merge copies SSH keys from the
// source bundle into the destination with upsert semantics.
func TestSecretBundleMerge_SSHKeys(t *testing.T) {
	a := NewSecretBundle()
	a.AddSSHKey("gh/acme/web", BundleSSHKey{Fingerprint: "fp1=", Hostname: "github.com", PrivateKey: "pem1"})

	other := NewSecretBundle()
	other.AddSSHKey("gh/acme/web", BundleSSHKey{Fingerprint: "fp2=", Hostname: "bitbucket.org", PrivateKey: "pem2"})
	other.AddSSHKey("gh/acme/api", BundleSSHKey{Fingerprint: "fp3=", Hostname: "", PrivateKey: "pem3"})

	a.Merge(other)

	webKeys := a.SSHKeys["gh/acme/web"]
	if len(webKeys) != 2 {
		t.Fatalf("expected 2 keys for gh/acme/web after merge, got %d", len(webKeys))
	}

	apiKeys := a.SSHKeys["gh/acme/api"]
	if len(apiKeys) != 1 {
		t.Fatalf("expected 1 key for gh/acme/api after merge, got %d", len(apiKeys))
	}

	// Upsert on collision: later value wins.
	collision := NewSecretBundle()
	collision.AddSSHKey("gh/acme/web", BundleSSHKey{Fingerprint: "fp1=", Hostname: "github.com", PrivateKey: "updated"})
	a.Merge(collision)

	webKeys = a.SSHKeys["gh/acme/web"]
	var fp1Key *BundleSSHKey
	for i := range webKeys {
		if webKeys[i].Fingerprint == "fp1=" {
			fp1Key = &webKeys[i]
			break
		}
	}
	if fp1Key == nil {
		t.Fatal("fp1= key not found after collision merge")
	}
	if fp1Key.PrivateKey != "updated" {
		t.Errorf("PrivateKey after upsert merge: got %q, want updated", fp1Key.PrivateKey)
	}
}

// TestSecretBundleMerge_NilPreservesSSHKeys verifies that Merge(nil) does not
// touch the SSH keys map.
func TestSecretBundleMerge_NilPreservesSSHKeys(t *testing.T) {
	b := NewSecretBundle()
	b.AddSSHKey("gh/acme/web", BundleSSHKey{Fingerprint: "fp1=", PrivateKey: "pem"})
	b.Merge(nil)

	if len(b.SSHKeys["gh/acme/web"]) != 1 {
		t.Error("Merge(nil) must not modify SSHKeys")
	}
}

// TestNewSecretBundle_SSHKeysInitialized verifies that NewSecretBundle returns
// a bundle with a non-nil SSHKeys map.
func TestNewSecretBundle_SSHKeysInitialized(t *testing.T) {
	b := NewSecretBundle()
	if b.SSHKeys == nil {
		t.Error("NewSecretBundle must initialize SSHKeys to a non-nil map")
	}
}
