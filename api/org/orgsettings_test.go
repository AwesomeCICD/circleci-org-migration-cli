package org

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// ─────────────────────────────────────────────────────────────────────────────
// snakeToKebab
// ─────────────────────────────────────────────────────────────────────────────

func TestSnakeToKebab(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"allow_certified_public_orbs", "allow-certified-public-orbs"},
		{"allow_uncertified_public_orbs", "allow-uncertified-public-orbs"},
		{"allow_private_orbs", "allow-private-orbs"},
		{"disable_user_checkout_keys", "disable-user-checkout-keys"},
		{"drop_all_build_requests", "drop-all-build-requests"},
		{"require_context_group_restriction", "require-context-group-restriction"},
		{"ai_error_summarization", "ai-error-summarization"},
		{"allow_ai_agents", "allow-ai-agents"},
		{"allow_api_trigger_with_config", "allow-api-trigger-with-config"},
		{"image_brownouts_enabled", "image-brownouts-enabled"},
		{"resource_class_brownouts_enabled", "resource-class-brownouts-enabled"},
		{"already-kebab", "already-kebab"},
		{"nounderscores", "nounderscores"},
		{"", ""},
	}
	for _, tc := range cases {
		got := snakeToKebab(tc.input)
		if got != tc.want {
			t.Errorf("snakeToKebab(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// GetFeatureFlags
// ─────────────────────────────────────────────────────────────────────────────

func TestGetFeatureFlags_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		wantPath := "/api/v1.1/organization/github/acme/settings"
		if r.URL.Path != wantPath {
			t.Errorf("expected path %q, got %q", wantPath, r.URL.Path)
		}
		respondJSON(w, http.StatusOK, map[string]any{
			"feature_flags": map[string]bool{
				"allow_certified_public_orbs":   true,
				"allow_uncertified_public_orbs": false,
				"drop_all_build_requests":       false,
			},
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	flags, err := c.GetFeatureFlags("github", "acme")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if flags["allow_certified_public_orbs"] != true {
		t.Errorf("allow_certified_public_orbs: got %v want true", flags["allow_certified_public_orbs"])
	}
	if flags["allow_uncertified_public_orbs"] != false {
		t.Errorf("allow_uncertified_public_orbs: got %v want false", flags["allow_uncertified_public_orbs"])
	}
}

func TestGetFeatureFlags_EmptyFlags(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusOK, map[string]any{"feature_flags": map[string]bool{}})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	flags, err := c.GetFeatureFlags("github", "acme")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(flags) != 0 {
		t.Errorf("expected empty map, got %v", flags)
	}
}

func TestGetFeatureFlags_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusForbidden, map[string]string{"message": "forbidden"})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	_, err := c.GetFeatureFlags("github", "acme")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// UpdateFeatureFlags
// ─────────────────────────────────────────────────────────────────────────────

func TestUpdateFeatureFlags_HappyPath_SnakeToKebabConversion(t *testing.T) {
	var receivedBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("expected PUT, got %s", r.Method)
		}
		wantPath := "/api/v1.1/organization/github/acme/settings"
		if r.URL.Path != wantPath {
			t.Errorf("expected path %q, got %q", wantPath, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&receivedBody); err != nil {
			t.Errorf("decode body: %v", err)
		}
		respondJSON(w, http.StatusOK, map[string]any{})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	flags := map[string]bool{
		"allow_certified_public_orbs":   true,
		"allow_uncertified_public_orbs": false,
		"drop_all_build_requests":       false,
	}
	if err := c.UpdateFeatureFlags("github", "acme", flags); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The body must have "feature_flags" as top-level key.
	ff, ok := receivedBody["feature_flags"].(map[string]any)
	if !ok {
		t.Fatalf("feature_flags not present or wrong type in body: %v", receivedBody)
	}
	// Keys must be kebab-case.
	if _, hasSnake := ff["allow_certified_public_orbs"]; hasSnake {
		t.Error("snake_case key should have been converted to kebab-case")
	}
	if v, ok := ff["allow-certified-public-orbs"]; !ok || v != true {
		t.Errorf("allow-certified-public-orbs: got %v (ok=%v), want true", v, ok)
	}
	if _, ok := ff["allow-uncertified-public-orbs"]; !ok {
		t.Error("allow-uncertified-public-orbs key missing after conversion")
	}
	if _, ok := ff["drop-all-build-requests"]; !ok {
		t.Error("drop-all-build-requests key missing after conversion")
	}
}

func TestUpdateFeatureFlags_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"message": "oops"})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	err := c.UpdateFeatureFlags("github", "acme", map[string]bool{"allow_private_orbs": true})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// GetOIDCClaims
// ─────────────────────────────────────────────────────────────────────────────

func TestGetOIDCClaims_HappyPath(t *testing.T) {
	const orgID = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		wantPath := "/api/v2/org/" + orgID + "/oidc-custom-claims"
		if r.URL.Path != wantPath {
			t.Errorf("expected path %q, got %q", wantPath, r.URL.Path)
		}
		respondJSON(w, http.StatusOK, map[string]any{
			"org_id":   orgID,
			"audience": []string{"https://example.com", "https://other.example.com"},
			"ttl":      "1h",
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	audience, ttl, err := c.GetOIDCClaims(orgID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ttl != "1h" {
		t.Errorf("ttl: got %q want %q", ttl, "1h")
	}
	if len(audience) != 2 {
		t.Fatalf("audience: got %d items want 2", len(audience))
	}
	if audience[0] != "https://example.com" {
		t.Errorf("audience[0]: got %q want %q", audience[0], "https://example.com")
	}
}

func TestGetOIDCClaims_Empty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusOK, map[string]any{"org_id": "some-id"})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	audience, ttl, err := c.GetOIDCClaims("some-id")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(audience) != 0 {
		t.Errorf("expected empty audience, got %v", audience)
	}
	if ttl != "" {
		t.Errorf("expected empty ttl, got %q", ttl)
	}
}

func TestGetOIDCClaims_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusNotFound, map[string]string{"message": "not found"})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	_, _, err := c.GetOIDCClaims("missing-org")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// SetOIDCClaims
// ─────────────────────────────────────────────────────────────────────────────

func TestSetOIDCClaims_HappyPath(t *testing.T) {
	const orgID = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	var receivedBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("expected PATCH, got %s", r.Method)
		}
		wantPath := "/api/v2/org/" + orgID + "/oidc-custom-claims"
		if r.URL.Path != wantPath {
			t.Errorf("expected path %q, got %q", wantPath, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&receivedBody); err != nil {
			t.Errorf("decode body: %v", err)
		}
		respondJSON(w, http.StatusOK, map[string]any{})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	if err := c.SetOIDCClaims(orgID, []string{"https://example.com"}, "2h"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if ttl, ok := receivedBody["ttl"]; !ok || ttl != "2h" {
		t.Errorf("ttl in body: got %v (ok=%v), want %q", ttl, ok, "2h")
	}
	aud, ok := receivedBody["audience"].([]any)
	if !ok || len(aud) != 1 {
		t.Errorf("audience in body: got %v", receivedBody["audience"])
	}
}

func TestSetOIDCClaims_NoOp_EmptyAudienceAndTTL(t *testing.T) {
	// Neither audience nor ttl — must make no HTTP call.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("unexpected HTTP call when audience and ttl are both empty")
		respondJSON(w, http.StatusOK, map[string]any{})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	if err := c.SetOIDCClaims("some-id", nil, ""); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSetOIDCClaims_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusUnprocessableEntity, map[string]string{"message": "invalid"})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	err := c.SetOIDCClaims("org-id", []string{"aud"}, "1h")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// GetURLOrbAllowList
// ─────────────────────────────────────────────────────────────────────────────

func TestGetURLOrbAllowList_HappyPath(t *testing.T) {
	const slugOrID = "gh/acme"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		wantEscaped := "/api/v2/organization/gh%2Facme/url-orb-allow-list"
		if r.URL.EscapedPath() != wantEscaped {
			t.Errorf("expected escaped path %q, got %q", wantEscaped, r.URL.EscapedPath())
		}
		respondJSON(w, http.StatusOK, map[string]any{
			"items": []map[string]string{
				{"id": "id-1", "name": "github-raw", "prefix": "https://raw.githubusercontent.com/", "auth": "none"},
				{"id": "id-2", "name": "s3-bucket", "prefix": "https://bucket.s3.amazonaws.com/", "auth": "aws"},
			},
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	entries, err := c.GetURLOrbAllowList(slugOrID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].Name != "github-raw" || entries[0].Prefix != "https://raw.githubusercontent.com/" {
		t.Errorf("unexpected entry[0]: %+v", entries[0])
	}
}

func TestGetURLOrbAllowList_Empty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusOK, map[string]any{"items": []any{}})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	entries, err := c.GetURLOrbAllowList("gh/acme")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected empty, got %v", entries)
	}
}

func TestGetURLOrbAllowList_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusForbidden, map[string]string{"message": "forbidden"})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	_, err := c.GetURLOrbAllowList("gh/acme")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// CreateURLOrbAllowEntry
// ─────────────────────────────────────────────────────────────────────────────

func TestCreateURLOrbAllowEntry_HappyPath(t *testing.T) {
	const orgID = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	var receivedBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		wantPath := "/api/v2/organization/" + orgID + "/url-orb-allow-list"
		if r.URL.Path != wantPath {
			t.Errorf("expected path %q, got %q", wantPath, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&receivedBody); err != nil {
			t.Errorf("decode body: %v", err)
		}
		respondJSON(w, http.StatusCreated, map[string]any{"id": "new-entry-id"})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	if err := c.CreateURLOrbAllowEntry(orgID, "github-raw", "https://raw.githubusercontent.com/", "none"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if receivedBody["name"] != "github-raw" {
		t.Errorf("name: got %v want %q", receivedBody["name"], "github-raw")
	}
	if receivedBody["prefix"] != "https://raw.githubusercontent.com/" {
		t.Errorf("prefix: got %v", receivedBody["prefix"])
	}
	if receivedBody["auth"] != "none" {
		t.Errorf("auth: got %v want %q", receivedBody["auth"], "none")
	}
}

func TestCreateURLOrbAllowEntry_EscapedSlugPath(t *testing.T) {
	// Slug with slash must be percent-encoded exactly once.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wantEscaped := "/api/v2/organization/gh%2Facme/url-orb-allow-list"
		if r.URL.EscapedPath() != wantEscaped {
			t.Errorf("expected escaped path %q, got %q", wantEscaped, r.URL.EscapedPath())
		}
		respondJSON(w, http.StatusCreated, map[string]any{})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	if err := c.CreateURLOrbAllowEntry("gh/acme", "n", "p", "a"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCreateURLOrbAllowEntry_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusBadRequest, map[string]string{"message": "invalid input"})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	err := c.CreateURLOrbAllowEntry("gh/acme", "", "bad-prefix", "bad-auth")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// GetPolicyBundle
// ─────────────────────────────────────────────────────────────────────────────

func TestGetPolicyBundle_HappyPath(t *testing.T) {
	const ownerID = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		wantPath := "/api/v2/owner/" + ownerID + "/context/config/policy-bundle"
		if r.URL.Path != wantPath {
			t.Errorf("expected path %q, got %q", wantPath, r.URL.Path)
		}
		respondJSON(w, http.StatusOK, map[string]string{
			"my_policy":    "package org\ndefault allow = false",
			"other_policy": "package org\ndefault allow = true",
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	bundle, err := c.GetPolicyBundle(ownerID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(bundle) != 2 {
		t.Fatalf("expected 2 policies, got %d", len(bundle))
	}
	if bundle["my_policy"] == "" {
		t.Error("my_policy missing from bundle")
	}
}

func TestGetPolicyBundle_Empty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusOK, map[string]string{})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	bundle, err := c.GetPolicyBundle("some-owner")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if bundle == nil {
		t.Error("expected non-nil map, got nil")
	}
	if len(bundle) != 0 {
		t.Errorf("expected empty map, got %v", bundle)
	}
}

func TestGetPolicyBundle_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusForbidden, map[string]string{"message": "forbidden"})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	_, err := c.GetPolicyBundle("owner-id")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// PutPolicyBundle
// ─────────────────────────────────────────────────────────────────────────────

func TestPutPolicyBundle_HappyPath(t *testing.T) {
	const ownerID = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	var receivedBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		wantPath := "/api/v2/owner/" + ownerID + "/context/config/policy-bundle"
		if r.URL.Path != wantPath {
			t.Errorf("expected path %q, got %q", wantPath, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&receivedBody); err != nil {
			t.Errorf("decode body: %v", err)
		}
		respondJSON(w, http.StatusOK, map[string]any{})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	policies := map[string]string{
		"my_policy": "package org\ndefault allow = false",
	}
	if err := c.PutPolicyBundle(ownerID, policies); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	policiesInBody, ok := receivedBody["policies"].(map[string]any)
	if !ok {
		t.Fatalf("policies not present or wrong type in body: %v", receivedBody)
	}
	if _, ok := policiesInBody["my_policy"]; !ok {
		t.Error("my_policy missing from body")
	}
}

func TestPutPolicyBundle_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusForbidden, map[string]string{"message": "not on scale plan"})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	err := c.PutPolicyBundle("owner-id", map[string]string{"p": "rego"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// GetPolicyEnforcement
// ─────────────────────────────────────────────────────────────────────────────

func TestGetPolicyEnforcement_Enabled(t *testing.T) {
	const ownerID = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		wantPath := "/api/v2/owner/" + ownerID + "/context/config/decision/settings"
		if r.URL.Path != wantPath {
			t.Errorf("expected path %q, got %q", wantPath, r.URL.Path)
		}
		respondJSON(w, http.StatusOK, map[string]any{"enabled": true})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	enabled, err := c.GetPolicyEnforcement(ownerID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !enabled {
		t.Error("expected enabled=true, got false")
	}
}

func TestGetPolicyEnforcement_Disabled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusOK, map[string]any{"enabled": false})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	enabled, err := c.GetPolicyEnforcement("owner-id")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if enabled {
		t.Error("expected enabled=false, got true")
	}
}

func TestGetPolicyEnforcement_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusNotFound, map[string]string{"message": "not found"})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	_, err := c.GetPolicyEnforcement("owner-id")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// SetPolicyEnforcement
// ─────────────────────────────────────────────────────────────────────────────

func TestSetPolicyEnforcement_EnableTrue(t *testing.T) {
	const ownerID = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	var receivedBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("expected PATCH, got %s", r.Method)
		}
		wantPath := "/api/v2/owner/" + ownerID + "/context/config/decision/settings"
		if r.URL.Path != wantPath {
			t.Errorf("expected path %q, got %q", wantPath, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&receivedBody); err != nil {
			t.Errorf("decode body: %v", err)
		}
		respondJSON(w, http.StatusOK, map[string]any{"enabled": true})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	if err := c.SetPolicyEnforcement(ownerID, true); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if receivedBody["enabled"] != true {
		t.Errorf("enabled in body: got %v want true", receivedBody["enabled"])
	}
}

func TestSetPolicyEnforcement_DisableFalse(t *testing.T) {
	var receivedBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&receivedBody); err != nil {
			t.Errorf("decode body: %v", err)
		}
		respondJSON(w, http.StatusOK, map[string]any{"enabled": false})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	if err := c.SetPolicyEnforcement("owner-id", false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if receivedBody["enabled"] != false {
		t.Errorf("enabled in body: got %v want false", receivedBody["enabled"])
	}
}

func TestSetPolicyEnforcement_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusForbidden, map[string]string{"message": "forbidden"})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	err := c.SetPolicyEnforcement("owner-id", true)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}
