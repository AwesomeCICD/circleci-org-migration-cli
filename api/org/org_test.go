package org

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// newTestClient builds a Client pointed at the given httptest.Server for both
// API versions.  Using explicit base URLs lets tests avoid the settings layer.
func newTestClient(t *testing.T, srv *httptest.Server) *Client {
	t.Helper()
	serverURL, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}
	v2Base := serverURL.ResolveReference(&url.URL{Path: "/api/v2/"})
	v11Base := serverURL.ResolveReference(&url.URL{Path: "/api/v1.1/"})
	return newClientFromBases(v2Base, v11Base, "test-token", srv.Client())
}

// respondJSON writes a JSON body with the given status code.
func respondJSON(w http.ResponseWriter, status int, body interface{}) {
	b, _ := json.Marshal(body)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(b)
}

// --------------------------------------------------------------------------
// GetOrganization
// --------------------------------------------------------------------------

func TestGetOrganization_HappyPath(t *testing.T) {
	const slug = "gh/acme"
	want := Organization{
		ID:      "org-uuid-123",
		Name:    "Acme Corp",
		Slug:    slug,
		VCSType: "github",
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		// Slug with '/' must be percent-encoded exactly once on the wire →
		// gh%2Facme. Assert on EscapedPath (the wire form); r.URL.Path is the
		// decoded form and would not reveal a double-escaping bug.
		wantPath := "/api/v2/organization/gh%2Facme"
		if r.URL.EscapedPath() != wantPath {
			t.Errorf("expected escaped path %q, got %q", wantPath, r.URL.EscapedPath())
		}
		respondJSON(w, http.StatusOK, want)
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	got, err := c.GetOrganization(context.Background(), slug)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if *got != want {
		t.Errorf("got %+v, want %+v", *got, want)
	}
}

func TestGetOrganization_ByUUID(t *testing.T) {
	const uuid = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	want := Organization{
		ID:      uuid,
		Name:    "Some Org",
		Slug:    "circleci/" + uuid,
		VCSType: "circleci",
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// UUID has no slash, so path segment is unmodified.
		wantPath := "/api/v2/organization/" + uuid
		if r.URL.Path != wantPath {
			t.Errorf("expected path %q, got %q", wantPath, r.URL.Path)
		}
		respondJSON(w, http.StatusOK, want)
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	got, err := c.GetOrganization(context.Background(), uuid)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if *got != want {
		t.Errorf("got %+v, want %+v", *got, want)
	}
}

func TestGetOrganization_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusNotFound, map[string]string{"message": "not found"})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	_, err := c.GetOrganization(context.Background(), "gh/missing")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// --------------------------------------------------------------------------
// ListCollaborations
// --------------------------------------------------------------------------

func TestListCollaborations_HappyPath(t *testing.T) {
	rawResp := []map[string]string{
		{
			"id":         "uuid-1",
			"name":       "Org One",
			"slug":       "gh/org-one",
			"vcs_type":   "github",
			"avatar_url": "http://example.com/avatar1.png",
		},
		{
			"id":         "uuid-2",
			"name":       "Org Two",
			"slug":       "bb/org-two",
			"vcs_type":   "bitbucket",
			"avatar_url": "http://example.com/avatar2.png",
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/api/v2/me/collaborations" {
			t.Errorf("unexpected path: %q", r.URL.Path)
		}
		respondJSON(w, http.StatusOK, rawResp)
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	got, err := c.ListCollaborations(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 orgs, got %d", len(got))
	}
	if got[0].ID != "uuid-1" || got[0].Slug != "gh/org-one" || got[0].VCSType != "github" {
		t.Errorf("unexpected first org: %+v", got[0])
	}
	if got[1].ID != "uuid-2" || got[1].Slug != "bb/org-two" || got[1].VCSType != "bitbucket" {
		t.Errorf("unexpected second org: %+v", got[1])
	}
}

func TestListCollaborations_Empty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusOK, []interface{}{})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	got, err := c.ListCollaborations(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty slice, got %v", got)
	}
}

func TestListCollaborations_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"message": "internal error"})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	_, err := c.ListCollaborations(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// --------------------------------------------------------------------------
// ResolveOrgID
// --------------------------------------------------------------------------

func TestResolveOrgID_BareUUID(t *testing.T) {
	// A bare UUID must be returned as-is without any HTTP call.
	c := newClientFromBases(
		mustParseURL("http://unreachable.invalid/api/v2/"),
		mustParseURL("http://unreachable.invalid/api/v1.1/"),
		"tok", nil,
	)

	const uuid = "12345678-1234-1234-1234-123456789abc"
	got, err := c.ResolveOrgID(context.Background(), uuid)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != uuid {
		t.Errorf("got %q, want %q", got, uuid)
	}
}

func TestResolveOrgID_CircleCISlug(t *testing.T) {
	// A "circleci/<uuid>" slug must return the UUID portion without any HTTP call.
	c := newClientFromBases(
		mustParseURL("http://unreachable.invalid/api/v2/"),
		mustParseURL("http://unreachable.invalid/api/v1.1/"),
		"tok", nil,
	)

	const uuid = "aaaabbbb-cccc-dddd-eeee-ffffaaaabbbb"
	slug := "circleci/" + uuid
	got, err := c.ResolveOrgID(context.Background(), slug)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != uuid {
		t.Errorf("got %q, want %q", got, uuid)
	}
}

func TestResolveOrgID_OrgSlug_CallsGetOrganization(t *testing.T) {
	const orgID = "resolved-uuid-xyz"
	var called bool

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if r.URL.EscapedPath() != "/api/v2/organization/gh%2Facme" {
			t.Errorf("unexpected escaped path: %q", r.URL.EscapedPath())
		}
		respondJSON(w, http.StatusOK, Organization{
			ID:      orgID,
			Name:    "Acme",
			Slug:    "gh/acme",
			VCSType: "github",
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	got, err := c.ResolveOrgID(context.Background(), "gh/acme")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("expected GetOrganization to be called")
	}
	if got != orgID {
		t.Errorf("got %q, want %q", got, orgID)
	}
}

func TestResolveOrgID_NotCircleCISlugWithBadUUID(t *testing.T) {
	// "circleci/not-a-uuid" must fall through to GetOrganization.
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		respondJSON(w, http.StatusOK, Organization{
			ID:      "some-id",
			Name:    "Org",
			Slug:    "circleci/not-a-uuid",
			VCSType: "circleci",
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	got, err := c.ResolveOrgID(context.Background(), "circleci/not-a-uuid")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if callCount != 1 {
		t.Errorf("expected exactly 1 HTTP call, got %d", callCount)
	}
	if got != "some-id" {
		t.Errorf("got %q, want %q", got, "some-id")
	}
}

// --------------------------------------------------------------------------
// GetOrgSettings
// --------------------------------------------------------------------------

func TestGetOrgSettings_FlagTrue(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		wantPath := "/api/v1.1/organization/github/acme/settings"
		if r.URL.Path != wantPath {
			t.Errorf("expected path %q, got %q", wantPath, r.URL.Path)
		}
		respondJSON(w, http.StatusOK, map[string]interface{}{
			"feature_flags": map[string]interface{}{
				"require_context_group_restriction": true,
			},
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	got, err := c.GetOrgSettings(context.Background(), "github", "acme")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.RequireContextGroupRestriction == nil {
		t.Fatal("expected RequireContextGroupRestriction to be non-nil")
	}
	if *got.RequireContextGroupRestriction != true {
		t.Errorf("expected true, got %v", *got.RequireContextGroupRestriction)
	}
}

func TestGetOrgSettings_FlagFalse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusOK, map[string]interface{}{
			"feature_flags": map[string]interface{}{
				"require_context_group_restriction": false,
			},
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	got, err := c.GetOrgSettings(context.Background(), "github", "acme")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.RequireContextGroupRestriction == nil {
		t.Fatal("expected RequireContextGroupRestriction to be non-nil")
	}
	if *got.RequireContextGroupRestriction != false {
		t.Errorf("expected false, got %v", *got.RequireContextGroupRestriction)
	}
}

func TestGetOrgSettings_FlagAbsent(t *testing.T) {
	// feature_flags present but flag key is absent → RequireContextGroupRestriction nil.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusOK, map[string]interface{}{
			"feature_flags": map[string]interface{}{
				"some_other_flag": true,
			},
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	got, err := c.GetOrgSettings(context.Background(), "github", "acme")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.RequireContextGroupRestriction != nil {
		t.Errorf("expected nil, got %v", *got.RequireContextGroupRestriction)
	}
}

func TestGetOrgSettings_FeatureFlagsAbsent(t *testing.T) {
	// No feature_flags key at all.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusOK, map[string]interface{}{})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	got, err := c.GetOrgSettings(context.Background(), "github", "acme")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.RequireContextGroupRestriction != nil {
		t.Errorf("expected nil, got %v", *got.RequireContextGroupRestriction)
	}
}

func TestGetOrgSettings_PathConstruction(t *testing.T) {
	// Confirm the URL path uses the vcsType and orgName verbatim (no extra escaping
	// for ordinary ASCII names).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		want := "/api/v1.1/organization/bitbucket/my-org/settings"
		if r.URL.Path != want {
			t.Errorf("expected %q, got %q", want, r.URL.Path)
		}
		respondJSON(w, http.StatusOK, map[string]interface{}{})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	if _, err := c.GetOrgSettings(context.Background(), "bitbucket", "my-org"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGetOrgSettings_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusUnauthorized, map[string]string{"message": "unauthorized"})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	_, err := c.GetOrgSettings(context.Background(), "github", "acme")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// --------------------------------------------------------------------------
// isBareUUID / slugIsCIRCLECIUUID unit tests
// --------------------------------------------------------------------------

func TestIsBareUUID(t *testing.T) {
	cases := []struct {
		input string
		want  bool
	}{
		{"12345678-1234-1234-1234-123456789abc", true},
		{"aaaabbbb-cccc-dddd-eeee-ffffaaaabbbb", true},
		{"AAAABBBB-CCCC-DDDD-EEEE-FFFFAAAABBBB", true},
		{"gh/acme", false},
		{"circleci/12345678-1234-1234-1234-123456789abc", false},
		{"12345678-1234-1234-1234-123456789abX", false}, // invalid hex
		{"12345678-1234-1234-1234-123456789ab", false},  // too short
		{"", false},
	}
	for _, tc := range cases {
		got := isBareUUID(tc.input)
		if got != tc.want {
			t.Errorf("isBareUUID(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}

func TestSlugIsCIRCLECIUUID(t *testing.T) {
	const validUUID = "12345678-1234-1234-1234-123456789abc"
	cases := []struct {
		input  string
		wantID string
		wantOK bool
	}{
		{"circleci/" + validUUID, validUUID, true},
		{"circleci/not-a-uuid", "", false},
		{"gh/acme", "", false},
		{"github/" + validUUID, "", false},
		{validUUID, "", false},
	}
	for _, tc := range cases {
		gotID, gotOK := slugIsCIRCLECIUUID(tc.input)
		if gotOK != tc.wantOK || gotID != tc.wantID {
			t.Errorf("slugIsCIRCLECIUUID(%q) = (%q, %v), want (%q, %v)",
				tc.input, gotID, gotOK, tc.wantID, tc.wantOK)
		}
	}
}

// --------------------------------------------------------------------------
// helpers
// --------------------------------------------------------------------------

func mustParseURL(raw string) *url.URL {
	u, err := url.Parse(raw)
	if err != nil {
		panic(fmt.Sprintf("mustParseURL(%q): %v", raw, err))
	}
	return u
}
