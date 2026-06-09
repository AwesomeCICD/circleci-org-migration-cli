package secrets

import (
	"testing"

	"github.com/CircleCI-Public/circleci-org-migration-cli/internal/manifest"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// makeLookup returns a Lookup that reports the given map's entries.
func makeLookup(env map[string]string) Lookup {
	return func(name string) (string, bool) {
		v, ok := env[name]
		return v, ok
	}
}

// demoManifest builds a minimal Manifest with one context named "demo"
// (vars FOO and BAR) and one project slug "gh/acme/web" (vars KEY and TOKEN).
func demoManifest() *manifest.Manifest {
	return &manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		Contexts: []manifest.Context{
			{
				Name: "demo",
				EnvVars: []manifest.ContextEnvVar{
					{Name: "FOO"},
					{Name: "BAR"},
				},
			},
		},
		Projects: []manifest.Project{
			{
				Slug: "gh/acme/web",
				EnvVars: []manifest.ProjectEnvVar{
					{Name: "KEY"},
					{Name: "TOKEN"},
				},
			},
		},
	}
}

// ---------------------------------------------------------------------------
// ExtractContext
// ---------------------------------------------------------------------------

// TestExtractContext_FoundAndMissing verifies that when a context has vars FOO
// and BAR, and the lookup knows FOO but not BAR, Found contains FOO and Missing
// contains BAR. The bundle must record FOO's value.
func TestExtractContext_FoundAndMissing(t *testing.T) {
	m := demoManifest()
	bundle := manifest.NewSecretBundle()
	lookup := makeLookup(map[string]string{"FOO": "foo-value"})

	res, err := ExtractContext(m, bundle, "demo", lookup)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
		return
	}

	if len(res.Found) != 1 || res.Found[0] != "FOO" {
		t.Errorf("Found = %v, want [FOO]", res.Found)
	}
	if len(res.Missing) != 1 || res.Missing[0] != "BAR" {
		t.Errorf("Missing = %v, want [BAR]", res.Missing)
	}

	got, ok := bundle.ContextSecrets["demo"]["FOO"]
	if !ok {
		t.Fatal("bundle.ContextSecrets[\"demo\"][\"FOO\"] not set")
		return
	}
	if got != "foo-value" {
		t.Errorf("bundle value = %q, want %q", got, "foo-value")
	}
}

// TestExtractContext_EmptyValueIsCaptured verifies that a variable whose
// lookup returns ("", true) is treated as Found (captured), not Missing.
func TestExtractContext_EmptyValueIsCaptured(t *testing.T) {
	m := &manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		Contexts: []manifest.Context{
			{
				Name:    "demo",
				EnvVars: []manifest.ContextEnvVar{{Name: "EMPTY_VAR"}},
			},
		},
	}
	bundle := manifest.NewSecretBundle()
	// The variable is set but empty.
	lookup := makeLookup(map[string]string{"EMPTY_VAR": ""})

	res, err := ExtractContext(m, bundle, "demo", lookup)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
		return
	}

	if len(res.Found) != 1 || res.Found[0] != "EMPTY_VAR" {
		t.Errorf("Found = %v, want [EMPTY_VAR]", res.Found)
	}
	if len(res.Missing) != 0 {
		t.Errorf("Missing = %v, want []", res.Missing)
	}

	got, ok := bundle.ContextSecrets["demo"]["EMPTY_VAR"]
	if !ok {
		t.Fatal("bundle.ContextSecrets[\"demo\"][\"EMPTY_VAR\"] not set")
		return
	}
	if got != "" {
		t.Errorf("bundle value = %q, want empty string", got)
	}
}

// TestExtractContext_UnknownContext verifies that referencing a context name
// not present in the manifest returns an error.
func TestExtractContext_UnknownContext(t *testing.T) {
	m := demoManifest()
	bundle := manifest.NewSecretBundle()

	_, err := ExtractContext(m, bundle, "no-such-context", makeLookup(nil))
	if err == nil {
		t.Fatal("expected error for unknown context, got nil")
	}
}

// ---------------------------------------------------------------------------
// ExtractProject
// ---------------------------------------------------------------------------

// TestExtractProject_FoundAndMissing verifies analogous behaviour for projects:
// KEY is found, TOKEN is missing, and the bundle records KEY's value.
func TestExtractProject_FoundAndMissing(t *testing.T) {
	m := demoManifest()
	bundle := manifest.NewSecretBundle()
	lookup := makeLookup(map[string]string{"KEY": "key-value"})

	res, err := ExtractProject(m, bundle, "gh/acme/web", lookup)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
		return
	}

	if len(res.Found) != 1 || res.Found[0] != "KEY" {
		t.Errorf("Found = %v, want [KEY]", res.Found)
	}
	if len(res.Missing) != 1 || res.Missing[0] != "TOKEN" {
		t.Errorf("Missing = %v, want [TOKEN]", res.Missing)
	}

	got, ok := bundle.ProjectSecrets["gh/acme/web"]["KEY"]
	if !ok {
		t.Fatal("bundle.ProjectSecrets[\"gh/acme/web\"][\"KEY\"] not set")
		return
	}
	if got != "key-value" {
		t.Errorf("bundle value = %q, want %q", got, "key-value")
	}
}

// TestExtractProject_UnknownSlug verifies that referencing a project slug not
// present in the manifest returns an error.
func TestExtractProject_UnknownSlug(t *testing.T) {
	m := demoManifest()
	bundle := manifest.NewSecretBundle()

	_, err := ExtractProject(m, bundle, "gh/acme/unknown", makeLookup(nil))
	if err == nil {
		t.Fatal("expected error for unknown project slug, got nil")
	}
}

// ---------------------------------------------------------------------------
// Accumulation (no clobbering)
// ---------------------------------------------------------------------------

// TestExtractContext_AccumulatesIntoBundleWithoutClobbering verifies that
// extracting a second context into a bundle that already has entries keeps
// the original entries intact.
func TestExtractContext_AccumulatesIntoBundleWithoutClobbering(t *testing.T) {
	m := &manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		Contexts: []manifest.Context{
			{
				Name:    "ctx-a",
				EnvVars: []manifest.ContextEnvVar{{Name: "A_VAR"}},
			},
			{
				Name:    "ctx-b",
				EnvVars: []manifest.ContextEnvVar{{Name: "B_VAR"}},
			},
		},
	}
	bundle := manifest.NewSecretBundle()

	// First extraction into the bundle.
	_, err := ExtractContext(m, bundle, "ctx-a", makeLookup(map[string]string{"A_VAR": "aaa"}))
	if err != nil {
		t.Fatalf("first extraction error: %v", err)
		return
	}

	// Second extraction should accumulate, not clobber.
	_, err = ExtractContext(m, bundle, "ctx-b", makeLookup(map[string]string{"B_VAR": "bbb"}))
	if err != nil {
		t.Fatalf("second extraction error: %v", err)
		return
	}

	if v, ok := bundle.ContextSecrets["ctx-a"]["A_VAR"]; !ok || v != "aaa" {
		t.Errorf("ctx-a A_VAR = %q (ok=%v), want \"aaa\" true", v, ok)
	}
	if v, ok := bundle.ContextSecrets["ctx-b"]["B_VAR"]; !ok || v != "bbb" {
		t.Errorf("ctx-b B_VAR = %q (ok=%v), want \"bbb\" true", v, ok)
	}
}

// TestExtractProject_AccumulatesIntoBundleWithoutClobbering verifies the same
// accumulation property for projects.
func TestExtractProject_AccumulatesIntoBundleWithoutClobbering(t *testing.T) {
	m := &manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		Projects: []manifest.Project{
			{
				Slug:    "gh/acme/alpha",
				EnvVars: []manifest.ProjectEnvVar{{Name: "ALPHA"}},
			},
			{
				Slug:    "gh/acme/beta",
				EnvVars: []manifest.ProjectEnvVar{{Name: "BETA"}},
			},
		},
	}
	bundle := manifest.NewSecretBundle()

	_, err := ExtractProject(m, bundle, "gh/acme/alpha", makeLookup(map[string]string{"ALPHA": "alpha-val"}))
	if err != nil {
		t.Fatalf("first extraction error: %v", err)
		return
	}

	_, err = ExtractProject(m, bundle, "gh/acme/beta", makeLookup(map[string]string{"BETA": "beta-val"}))
	if err != nil {
		t.Fatalf("second extraction error: %v", err)
		return
	}

	if v, ok := bundle.ProjectSecrets["gh/acme/alpha"]["ALPHA"]; !ok || v != "alpha-val" {
		t.Errorf("gh/acme/alpha ALPHA = %q (ok=%v), want \"alpha-val\" true", v, ok)
	}
	if v, ok := bundle.ProjectSecrets["gh/acme/beta"]["BETA"]; !ok || v != "beta-val" {
		t.Errorf("gh/acme/beta BETA = %q (ok=%v), want \"beta-val\" true", v, ok)
	}
}
