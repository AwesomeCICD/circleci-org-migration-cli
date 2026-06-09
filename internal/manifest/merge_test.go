package manifest

import "testing"

func TestSecretBundleMerge(t *testing.T) {
	a := NewSecretBundle()
	a.SetContextSecret("c1", "K", "v1")

	b := NewSecretBundle()
	b.SetContextSecret("c2", "K", "v2")
	b.SetProjectSecret("gh/o/p", "X", "y")

	a.Merge(b)
	if a.ContextSecrets["c1"]["K"] != "v1" || a.ContextSecrets["c2"]["K"] != "v2" {
		t.Fatalf("context secrets not merged: %+v", a.ContextSecrets)
	}
	if a.ProjectSecrets["gh/o/p"]["X"] != "y" {
		t.Fatalf("project secrets not merged: %+v", a.ProjectSecrets)
	}

	// Later value wins on collision.
	c := NewSecretBundle()
	c.SetContextSecret("c1", "K", "new")
	a.Merge(c)
	if got := a.ContextSecrets["c1"]["K"]; got != "new" {
		t.Errorf("collision: got %q, want %q", got, "new")
	}

	// Merging nil is a no-op.
	a.Merge(nil)
	if a.ContextSecrets["c1"]["K"] != "new" {
		t.Error("Merge(nil) mutated the bundle")
	}
}
