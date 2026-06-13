package project

import (
	"context"
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
	if err := c.CreateEnvVar(context.Background(), slug, varName, varValue); err != nil {
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
	if err := c.CreateEnvVar(context.Background(), slug, "V", "val"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCreateEnvVar_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusUnauthorized, map[string]string{"message": "unauthorized"})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	err := c.CreateEnvVar(context.Background(), "gh/acme/web", "VAR", "val")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestCreateEnvVar_EmptySlug(t *testing.T) {
	c := &Client{}
	if err := c.CreateEnvVar(context.Background(), "", "VAR", "val"); err == nil {
		t.Fatal("expected error for empty slug")
	}
}

func TestCreateEnvVar_EmptyName(t *testing.T) {
	c := &Client{}
	if err := c.CreateEnvVar(context.Background(), "gh/acme/web", "", "val"); err == nil {
		t.Fatal("expected error for empty name")
	}
}

// ---- UpdateSettings ---------------------------------------------------------

func TestUpdateSettings_HappyPath(t *testing.T) {
	// Set AutocancelBuilds=false (and OSS=true in the AdvancedSettings struct to
	// verify it is NOT forwarded to the wire — the API rejects it, see #247).
	// Assert body contains exactly AutocancelBuilds and that oss is absent.
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

		// AutocancelBuilds must be present.
		if v, exists := advanced["autocancel_builds"]; !exists || v != false {
			t.Errorf("advanced.autocancel_builds: got %v (exists=%v), want false", v, exists)
		}

		// "oss" must NEVER appear in the PATCH body — the API rejects it with
		// "Unexpected field 'advanced.oss'" for all project types. (#247)
		if _, exists := advanced["oss"]; exists {
			t.Errorf("advanced.oss must be absent from the PATCH body (rejected by API, see #247)")
		}

		// Other nil fields must NOT be present (nil *bool fields should be omitted).
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
				"autocancel_builds": false,
			},
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	s := &AdvancedSettings{
		OSS:              &ossTrue, // set in AdvancedSettings but must NOT appear on the wire
		AutocancelBuilds: &autocancelFalse,
	}
	if err := c.UpdateSettings(context.Background(), "gh", "acme", "web", s); err != nil {
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
	if err := c.UpdateSettings(context.Background(), "bitbucket", "my-org", "my-repo", s); err != nil {
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
	if err := c.UpdateSettings(context.Background(), "gh", "acme", "web", s); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestUpdateSettings_OSSNeverSentOnWire verifies that "oss" is never included
// in the PATCH body regardless of the value set on AdvancedSettings.
// Background: the CircleCI project-settings endpoint rejects "advanced.oss"
// with "Unexpected field 'advanced.oss'" for all project types (GitHub OAuth
// and GitHub App alike).  The Terraform provider has no "oss" attribute either
// (same root issue).  See issue #247.
func TestUpdateSettings_OSSNeverSentOnWire(t *testing.T) {
	ossTrue := true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		advanced, ok := body["advanced"].(map[string]interface{})
		if !ok {
			t.Fatalf("advanced field missing or not an object: %v", body["advanced"])
		}
		// "oss" must be absent — the API rejects it. (#247)
		if _, exists := advanced["oss"]; exists {
			t.Errorf("advanced.oss must be absent from the PATCH body (rejected by API, see #247); got body: %v", advanced)
		}
		respondJSON(w, http.StatusOK, map[string]interface{}{"advanced": map[string]interface{}{}})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	// Set OSS in AdvancedSettings — it must be recorded/read but never written.
	s := &AdvancedSettings{
		OSS:              &ossTrue,
		AutocancelBuilds: boolPtr(true),
		SetGithubStatus:  boolPtr(true),
	}
	if err := c.UpdateSettings(context.Background(), "gh", "acme", "web", s); err != nil {
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
	if err := c.UpdateSettings(context.Background(), "gh", "acme", "web", s); err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestUpdateSettings_NilSettings(t *testing.T) {
	c := &Client{}
	if err := c.UpdateSettings(context.Background(), "gh", "acme", "web", nil); err == nil {
		t.Fatal("expected error for nil settings")
	}
}

func TestUpdateSettings_EmptyProvider(t *testing.T) {
	c := &Client{}
	s := &AdvancedSettings{}
	if err := c.UpdateSettings(context.Background(), "", "acme", "web", s); err == nil {
		t.Fatal("expected error for empty provider")
	}
}

// ---- SetOSS -----------------------------------------------------------------

// TestSetOSS_Applied verifies that when the API responds with oss=true,
// SetOSS returns applied=true and nil error.
func TestSetOSS_Applied(t *testing.T) {
	ossTrue := true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("expected PATCH, got %s", r.Method)
		}
		wantPath := "/api/v2/project/gh/acme/web/settings"
		if r.URL.EscapedPath() != wantPath {
			t.Errorf("path = %q, want %q", r.URL.EscapedPath(), wantPath)
		}

		// Verify the body contains only {"advanced":{"oss":true}}.
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		adv, ok := body["advanced"].(map[string]interface{})
		if !ok {
			t.Fatalf("advanced not an object: %v", body["advanced"])
		}
		if v, exists := adv["oss"]; !exists || v != true {
			t.Errorf("advanced.oss: got %v (exists=%v), want true", v, exists)
		}
		// No other fields should be present.
		if len(adv) != 1 {
			t.Errorf("advanced should have exactly 1 key (oss), got %d: %v", len(adv), adv)
		}

		respondJSON(w, http.StatusOK, map[string]interface{}{
			"advanced": map[string]interface{}{"oss": true},
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	applied, err := c.SetOSS(context.Background(), "gh", "acme", "web")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !applied {
		t.Error("expected applied=true when API returns oss=true")
	}
	_ = ossTrue
}

// TestSetOSS_OSSFalseInResponse verifies that when the API responds with
// oss=false (private-repo no-op), SetOSS returns applied=false with nil error.
func TestSetOSS_OSSFalseInResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusOK, map[string]interface{}{
			"advanced": map[string]interface{}{"oss": false},
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	applied, err := c.SetOSS(context.Background(), "gh", "acme", "private-web")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if applied {
		t.Error("expected applied=false when API returns oss=false")
	}
}

// TestSetOSS_UnexpectedField_TreatedAsNotApplied verifies that the
// "Unexpected field 'advanced.oss'" 400 response is treated as not-applied
// (nil error) rather than a hard error, so App-project syncs keep running.
func TestSetOSS_UnexpectedField_TreatedAsNotApplied(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusBadRequest, map[string]string{
			"message": "Unexpected field 'advanced.oss'",
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	applied, err := c.SetOSS(context.Background(), "circleci", "org-id", "proj-id")
	if err != nil {
		t.Errorf("Unexpected field rejection must not be a hard error, got: %v", err)
	}
	if applied {
		t.Error("expected applied=false when API rejects the oss field")
	}
}

// TestSetOSS_OtherError_ReturnsError verifies that a non-field-rejection error
// (e.g. 403 Forbidden) is returned as a hard error.
func TestSetOSS_OtherError_ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusForbidden, map[string]string{"message": "forbidden"})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	_, err := c.SetOSS(context.Background(), "gh", "acme", "web")
	if err == nil {
		t.Fatal("expected error for 403 Forbidden, got nil")
	}
}

// TestSetOSS_EmptyArgs verifies that empty provider/org/proj returns an error
// without making an HTTP call.
func TestSetOSS_EmptyArgs(t *testing.T) {
	c := &Client{}
	if _, err := c.SetOSS(context.Background(), "", "acme", "web"); err == nil {
		t.Error("expected error for empty provider")
	}
	if _, err := c.SetOSS(context.Background(), "gh", "", "web"); err == nil {
		t.Error("expected error for empty org")
	}
	if _, err := c.SetOSS(context.Background(), "gh", "acme", ""); err == nil {
		t.Error("expected error for empty proj")
	}
}

// TestContainsUnexpectedOSSField verifies the helper detects the rejection
// message in both mixed and lower-case forms.
func TestContainsUnexpectedOSSField(t *testing.T) {
	cases := []struct {
		msg  string
		want bool
	}{
		{"Unexpected field 'advanced.oss'", true},
		{"unexpected field 'advanced.oss'", true},
		{"UNEXPECTED FIELD 'ADVANCED.OSS'", true},
		{"unexpected field: advanced.oss", true},
		{"some other error", false},
		{"unexpected field 'advanced.build_fork_prs'", false}, // oss missing
		{"oss something else entirely", false},                // unexpected missing
	}
	for _, tc := range cases {
		got := containsUnexpectedOSSField(tc.msg)
		if got != tc.want {
			t.Errorf("containsUnexpectedOSSField(%q) = %v, want %v", tc.msg, got, tc.want)
		}
	}
}
