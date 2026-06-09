package org

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// ─────────────────────────────────────────────────────────────────────────────
// GetAuditLogConfigs
// ─────────────────────────────────────────────────────────────────────────────

func TestGetAuditLogConfigs_HappyPath(t *testing.T) {
	const orgID = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		wantPath := "/api/v2/organizations/" + orgID + "/audit-log/configs"
		if r.URL.Path != wantPath {
			t.Errorf("expected path %q, got %q", wantPath, r.URL.Path)
		}
		respondJSON(w, http.StatusOK, map[string]any{
			"items": []map[string]any{
				{
					"id":          "cfg-1",
					"org_id":      orgID,
					"purpose":     "security",
					"target_type": "s3",
					"is_disabled": false,
					"config": map[string]any{
						"arn":           "arn:aws:iam::123:role/audit",
						"region":        "us-east-1",
						"bucket_name":   "acme-audit",
						"bucket_prefix": "logs/",
						"endpoint":      "https://s3.amazonaws.com",
					},
				},
			},
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	configs, err := c.GetAuditLogConfigs(orgID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(configs) != 1 {
		t.Fatalf("expected 1 config, got %d", len(configs))
	}
	got := configs[0]
	if got.ID != "cfg-1" || got.Purpose != "security" || got.TargetType != "s3" {
		t.Errorf("unexpected config metadata: %+v", got)
	}
	if got.Config.ARN != "arn:aws:iam::123:role/audit" || got.Config.Region != "us-east-1" {
		t.Errorf("unexpected ARN/region: %+v", got.Config)
	}
	if got.Config.BucketName != "acme-audit" || got.Config.BucketPrefix != "logs/" {
		t.Errorf("unexpected bucket: %+v", got.Config)
	}
	if got.Config.Endpoint != "https://s3.amazonaws.com" {
		t.Errorf("unexpected endpoint: %q", got.Config.Endpoint)
	}
}

func TestGetAuditLogConfigs_Empty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusOK, map[string]any{"items": []any{}})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	configs, err := c.GetAuditLogConfigs("some-org")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(configs) != 0 {
		t.Errorf("expected empty, got %v", configs)
	}
}

func TestGetAuditLogConfigs_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusForbidden, map[string]string{"message": "forbidden"})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	_, err := c.GetAuditLogConfigs("some-org")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ListGroups
// ─────────────────────────────────────────────────────────────────────────────

// newTestClientWithAppServer builds a Client whose CIAM (app) client targets the
// given server, isolating the host/path assertions for the private CIAM endpoint.
func newTestClientWithAppServer(t *testing.T, srv *httptest.Server) *Client {
	t.Helper()
	serverURL, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}
	v2Base := serverURL.ResolveReference(&url.URL{Path: "/api/v2/"})
	v11Base := serverURL.ResolveReference(&url.URL{Path: "/api/v1.1/"})
	appBase := serverURL.ResolveReference(&url.URL{Path: "/"})
	privateBase := serverURL.ResolveReference(&url.URL{Path: "/api/private/"})
	return newClientFromAllBases(v2Base, v11Base, appBase, privateBase, "test-token", srv.Client())
}

// newTestClientWithPrivateServer builds a Client whose private API client
// targets the given server, isolating path assertions for /api/private/ endpoints.
func newTestClientWithPrivateServer(t *testing.T, srv *httptest.Server) *Client {
	t.Helper()
	serverURL, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}
	v2Base := serverURL.ResolveReference(&url.URL{Path: "/api/v2/"})
	v11Base := serverURL.ResolveReference(&url.URL{Path: "/api/v1.1/"})
	appBase := serverURL.ResolveReference(&url.URL{Path: "/"})
	privateBase := serverURL.ResolveReference(&url.URL{Path: "/api/private/"})
	return newClientFromAllBases(v2Base, v11Base, appBase, privateBase, "test-token", srv.Client())
}

func TestListGroups_HappyPath(t *testing.T) {
	const orgID = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		wantPath := "/private/ciam/orgs/" + orgID + "/groups"
		if r.URL.Path != wantPath {
			t.Errorf("expected path %q, got %q", wantPath, r.URL.Path)
		}
		if got := r.Header.Get("Circle-Token"); got != "test-token" {
			t.Errorf("Circle-Token header: got %q want %q", got, "test-token")
		}
		respondJSON(w, http.StatusOK, map[string]any{
			"items": []map[string]any{
				{"id": "grp-1", "name": "security", "org_id": orgID},
				{"id": "grp-2", "name": "platform", "org_id": orgID},
			},
		})
	}))
	defer srv.Close()

	c := newTestClientWithAppServer(t, srv)
	groups, err := c.ListGroups(orgID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(groups))
	}
	if groups[0].ID != "grp-1" || groups[0].Name != "security" {
		t.Errorf("unexpected group[0]: %+v", groups[0])
	}
	if groups[1].ID != "grp-2" || groups[1].Name != "platform" {
		t.Errorf("unexpected group[1]: %+v", groups[1])
	}
}

func TestListGroups_Empty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusOK, map[string]any{"items": []any{}})
	}))
	defer srv.Close()

	c := newTestClientWithAppServer(t, srv)
	groups, err := c.ListGroups("some-org")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(groups) != 0 {
		t.Errorf("expected empty, got %v", groups)
	}
}

func TestListGroups_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusNotFound, map[string]string{"message": "not found"})
	}))
	defer srv.Close()

	c := newTestClientWithAppServer(t, srv)
	_, err := c.ListGroups("some-org")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// TestAppBaseURL_RewritesCircleCIHost verifies the circleci.com → app.circleci.com
// host rewrite for the CIAM client.
func TestAppBaseURL_RewritesCircleCIHost(t *testing.T) {
	cases := []struct {
		in       string
		wantHost string
	}{
		{"https://circleci.com", "app.circleci.com"},
		{"https://app.circleci.com", "app.circleci.com"},
		{"https://circleci.example.com", "circleci.example.com"},
	}
	for _, tc := range cases {
		base, err := url.Parse(tc.in)
		if err != nil {
			t.Fatalf("parse %q: %v", tc.in, err)
		}
		got := appBaseURL(base)
		if got.Host != tc.wantHost {
			t.Errorf("appBaseURL(%q).Host = %q, want %q", tc.in, got.Host, tc.wantHost)
		}
		if got.Path != "/" {
			t.Errorf("appBaseURL(%q).Path = %q, want %q", tc.in, got.Path, "/")
		}
	}
}
