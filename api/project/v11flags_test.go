package project

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// ---- GetV11ProjectFeatureFlags ---------------------------------------------

func TestGetV11ProjectFeatureFlags_HappyPath(t *testing.T) {
	const slug = "gh/acme/web"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		// v1.1 endpoint uses the slug sub-resource "settings".
		wantPath := "/api/v1.1/project/gh/acme/web/settings"
		if r.URL.EscapedPath() != wantPath {
			t.Errorf("path: got %q want %q", r.URL.EscapedPath(), wantPath)
		}
		respondJSON(w, http.StatusOK, map[string]any{
			"feature_flags": map[string]any{
				"api-trigger-with-config": true,
				"drop-all-build-requests": false,
				"some-extra-bool-flag":    true,
				// Non-bool value that must be ignored.
				"some-array-flag": []string{"val1", "val2"},
			},
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	flags, err := c.GetV11ProjectFeatureFlags(slug)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v, ok := flags["api-trigger-with-config"]; !ok || !v {
		t.Errorf("api-trigger-with-config: got %v (ok=%v), want true", v, ok)
	}
	if v, ok := flags["drop-all-build-requests"]; !ok || v {
		t.Errorf("drop-all-build-requests: got %v (ok=%v), want false", v, ok)
	}
	// Extra bool flags must also be captured (full-map behavior).
	if v, ok := flags["some-extra-bool-flag"]; !ok || !v {
		t.Errorf("some-extra-bool-flag: got %v (ok=%v), want true", v, ok)
	}
	// Non-bool keys must not be in the result.
	if _, ok := flags["some-array-flag"]; ok {
		t.Error("non-bool flag should be ignored")
	}
}

func TestGetV11ProjectFeatureFlags_SlugEncoding(t *testing.T) {
	// Verify that spaces in org/repo names are percent-encoded.
	const slug = "gh/my org/my repo"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wantPath := "/api/v1.1/project/gh/my%20org/my%20repo/settings"
		if r.URL.EscapedPath() != wantPath {
			t.Errorf("path: got %q want %q", r.URL.EscapedPath(), wantPath)
		}
		respondJSON(w, http.StatusOK, map[string]any{
			"feature_flags": map[string]any{},
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	_, err := c.GetV11ProjectFeatureFlags(slug)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGetV11ProjectFeatureFlags_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusUnauthorized, map[string]string{"message": "unauthorized"})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	_, err := c.GetV11ProjectFeatureFlags("gh/acme/web")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ---- SetV11ProjectFeatureFlags ---------------------------------------------

func TestSetV11ProjectFeatureFlags_HappyPath(t *testing.T) {
	const slug = "gh/acme/web"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("expected PUT, got %s", r.Method)
		}
		wantPath := "/api/v1.1/project/gh/acme/web/settings"
		if r.URL.EscapedPath() != wantPath {
			t.Errorf("path: got %q want %q", r.URL.EscapedPath(), wantPath)
		}

		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		ff, ok := body["feature_flags"].(map[string]any)
		if !ok {
			t.Fatalf("feature_flags not present or not an object: %v", body)
		}
		// Snake-case key should have been converted to kebab-case.
		if v, exists := ff["api-trigger-with-config"]; !exists || v != true {
			t.Errorf("feature_flags[api-trigger-with-config]: got %v (exists=%v), want true", v, exists)
		}
		// snake_case key must NOT be present.
		if _, exists := ff["api_trigger_with_config"]; exists {
			t.Error("snake_case key must not be present in PUT body; use kebab-case")
		}

		respondJSON(w, http.StatusOK, map[string]any{})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	err := c.SetV11ProjectFeatureFlags(slug, map[string]bool{"api_trigger_with_config": true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSetV11ProjectFeatureFlags_DropAllBuildRequests_KebabKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		ff, _ := body["feature_flags"].(map[string]any)
		if v, ok := ff["drop-all-build-requests"]; !ok || v != false {
			t.Errorf("drop-all-build-requests: got %v (ok=%v), want false", v, ok)
		}
		respondJSON(w, http.StatusOK, map[string]any{})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	err := c.SetV11ProjectFeatureFlags("gh/acme/web", map[string]bool{"drop_all_build_requests": false})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSetV11ProjectFeatureFlags_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusForbidden, map[string]string{"message": "forbidden"})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	err := c.SetV11ProjectFeatureFlags("gh/acme/web", map[string]bool{"api_trigger_with_config": true})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// TestSetV11ProjectFeatureFlags_StringBody verifies that a 2xx response whose
// body is a plain JSON string (not a map) is handled without error.
// The live v1.1 PUT /settings endpoint may return a non-map body such as
// `"ok"`, which would previously fail with
// "cannot unmarshal string into Go value of type map[string]interface {}".
func TestSetV11ProjectFeatureFlags_StringBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("expected PUT, got %s", r.Method)
		}
		// Respond with a raw string — not an object — to exercise the
		// discard-body path introduced by the fix.
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`"ok"`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	err := c.SetV11ProjectFeatureFlags("gh/acme/web", map[string]bool{"api_trigger_with_config": true})
	if err != nil {
		t.Fatalf("unexpected error for string body: %v", err)
	}
}

// TestGetV11ProjectFeatureFlags_NonBoolFlagIgnored verifies that a feature_flags
// map containing array-valued or non-bool entries does not cause an error and
// that only the bool-valued known keys are returned.
// The live v1.1 project settings feature_flags can contain non-bool entries such
// as pr-only-branch-overrides:["main"] which must not crash the decode.
func TestGetV11ProjectFeatureFlags_NonBoolFlagIgnored(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusOK, map[string]any{
			"feature_flags": map[string]any{
				"api-trigger-with-config":  true,
				"another-bool-flag":        false,
				"pr-only-branch-overrides": []string{"main", "develop"},
				"some-numeric-flag":        42,
				"some-object-flag":         map[string]any{"k": "v"},
			},
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	flags, err := c.GetV11ProjectFeatureFlags("gh/acme/web")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v, ok := flags["api-trigger-with-config"]; !ok || !v {
		t.Errorf("api-trigger-with-config: got %v (ok=%v), want true", v, ok)
	}
	// Other bool flags must also be captured (full-map behavior).
	if _, ok := flags["another-bool-flag"]; !ok {
		t.Error("another-bool-flag should be present (bool value)")
	}
	// Non-bool keys must not appear in the result.
	for _, k := range []string{"pr-only-branch-overrides", "some-numeric-flag", "some-object-flag"} {
		if _, ok := flags[k]; ok {
			t.Errorf("key %q should be ignored (non-bool value), but was present", k)
		}
	}
	// drop-all-build-requests was absent, so it should not be in the result.
	if _, ok := flags["drop-all-build-requests"]; ok {
		t.Error("drop-all-build-requests should be absent when not in the response")
	}
}
