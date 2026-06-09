package project

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func boolPtr(b bool) *bool { return &b }

// ---- CreateEnvVar -----------------------------------------------------------

func TestCreateEnvVar_HappyPath(t *testing.T) {
	const slug = "gh/acme/web"
	const varName = "MY_VAR"
	const varValue = "my-secret"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		// EscapedPath should use individual per-component encoding.
		wantPath := "/api/v2/project/gh/acme/web/envvar"
		if r.URL.EscapedPath() != wantPath {
			t.Errorf("expected escaped path %q, got %q", wantPath, r.URL.EscapedPath())
		}

		// Assert request body: {"name":"MY_VAR","value":"my-secret"}.
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if body["name"] != varName {
			t.Errorf("name: got %v, want %s", body["name"], varName)
		}
		if body["value"] != varValue {
			t.Errorf("value: got %v, want %s", body["value"], varValue)
		}
		if len(body) != 2 {
			t.Errorf("body should have exactly 2 keys (name, value), got %d: %v", len(body), body)
		}

		respondJSON(w, http.StatusCreated, map[string]string{
			"name":  varName,
			"value": "xxxxcret",
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	if err := c.CreateEnvVar(slug, varName, varValue); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCreateEnvVar_EncodedSlug(t *testing.T) {
	// Verify that spaces in org/repo names are percent-encoded on the wire.
	const slug = "gh/my org/my repo"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wantPath := "/api/v2/project/gh/my%20org/my%20repo/envvar"
		if r.URL.EscapedPath() != wantPath {
			t.Errorf("EscapedPath = %q, want %q", r.URL.EscapedPath(), wantPath)
		}
		respondJSON(w, http.StatusCreated, map[string]string{"name": "V", "value": "xxxx"})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	if err := c.CreateEnvVar(slug, "V", "val"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCreateEnvVar_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusUnauthorized, map[string]string{"message": "unauthorized"})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	err := c.CreateEnvVar("gh/acme/web", "VAR", "val")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestCreateEnvVar_EmptySlug(t *testing.T) {
	c := &Client{}
	if err := c.CreateEnvVar("", "VAR", "val"); err == nil {
		t.Fatal("expected error for empty slug")
	}
}

func TestCreateEnvVar_EmptyName(t *testing.T) {
	c := &Client{}
	if err := c.CreateEnvVar("gh/acme/web", "", "val"); err == nil {
		t.Fatal("expected error for empty name")
	}
}

// ---- UpdateSettings ---------------------------------------------------------

func TestUpdateSettings_HappyPath(t *testing.T) {
	// Set OSS=true and AutocancelBuilds=false; all other fields nil.
	// Assert body contains exactly those two fields under "advanced".
	ossTrue := true
	autocancelFalse := false

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("expected PATCH, got %s", r.Method)
		}
		// Path must be the decomposed form (provider/org/project), not a pre-joined slug.
		wantPath := "/api/v2/project/gh/acme/web/settings"
		if r.URL.EscapedPath() != wantPath {
			t.Errorf("expected path %q, got %q", wantPath, r.URL.EscapedPath())
		}

		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}

		advanced, ok := body["advanced"].(map[string]interface{})
		if !ok {
			t.Fatalf("advanced field missing or not an object: %v", body["advanced"])
		}

		// OSS and AutocancelBuilds must be present.
		if v, exists := advanced["oss"]; !exists || v != true {
			t.Errorf("advanced.oss: got %v (exists=%v), want true", v, exists)
		}
		if v, exists := advanced["autocancel_builds"]; !exists || v != false {
			t.Errorf("advanced.autocancel_builds: got %v (exists=%v), want false", v, exists)
		}

		// Other fields must NOT be present (nil *bool fields should be omitted).
		for _, absent := range []string{
			"build_fork_prs", "build_prs_only", "disable_ssh",
			"forks_receive_secret_env_vars", "set_github_status",
			"setup_workflows", "write_settings_requires_admin",
			"pr_only_branch_overrides",
		} {
			if _, exists := advanced[absent]; exists {
				t.Errorf("field %q should be absent from the body but was present", absent)
			}
		}

		respondJSON(w, http.StatusOK, map[string]interface{}{
			"advanced": map[string]interface{}{
				"oss":               true,
				"autocancel_builds": false,
			},
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	s := &AdvancedSettings{
		OSS:              &ossTrue,
		AutocancelBuilds: &autocancelFalse,
	}
	if err := c.UpdateSettings("gh", "acme", "web", s); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestUpdateSettings_DecomposedPath(t *testing.T) {
	// Confirm that provider/org/project are individual path segments, not a pre-joined slug.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		want := "/api/v2/project/bitbucket/my-org/my-repo/settings"
		if r.URL.EscapedPath() != want {
			t.Errorf("path = %q, want %q", r.URL.EscapedPath(), want)
		}
		respondJSON(w, http.StatusOK, map[string]interface{}{"advanced": map[string]interface{}{}})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	s := &AdvancedSettings{OSS: boolPtr(true)}
	if err := c.UpdateSettings("bitbucket", "my-org", "my-repo", s); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestUpdateSettings_PROnlyBranchOverridesIncluded(t *testing.T) {
	// When PROnlyBranchOverrides is non-empty it must appear in the body.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		advanced, ok := body["advanced"].(map[string]interface{})
		if !ok {
			t.Fatalf("advanced field missing: %v", body)
		}
		overrides, exists := advanced["pr_only_branch_overrides"]
		if !exists {
			t.Error("pr_only_branch_overrides should be present when non-empty")
		}
		arr, ok := overrides.([]interface{})
		if !ok || len(arr) != 2 {
			t.Errorf("pr_only_branch_overrides: got %v, want slice of 2", overrides)
		}
		respondJSON(w, http.StatusOK, map[string]interface{}{"advanced": map[string]interface{}{}})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	s := &AdvancedSettings{
		PROnlyBranchOverrides: []string{"main", "develop"},
	}
	if err := c.UpdateSettings("gh", "acme", "web", s); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestUpdateSettings_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusForbidden, map[string]string{"message": "forbidden"})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	s := &AdvancedSettings{OSS: boolPtr(true)}
	if err := c.UpdateSettings("gh", "acme", "web", s); err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestUpdateSettings_NilSettings(t *testing.T) {
	c := &Client{}
	if err := c.UpdateSettings("gh", "acme", "web", nil); err == nil {
		t.Fatal("expected error for nil settings")
	}
}

func TestUpdateSettings_EmptyProvider(t *testing.T) {
	c := &Client{}
	s := &AdvancedSettings{}
	if err := c.UpdateSettings("", "acme", "web", s); err == nil {
		t.Fatal("expected error for empty provider")
	}
}
