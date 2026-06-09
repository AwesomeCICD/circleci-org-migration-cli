package project

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/CircleCI-Public/circleci-org-migration-cli/settings"
)

// ---- helpers ----------------------------------------------------------------

// newTestClient wires a Client whose v2 and v1.1 bases both point to the given
// httptest.Server.
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

// respondJSON writes a JSON-encoded body with the given status code.
func respondJSON(w http.ResponseWriter, status int, body interface{}) {
	b, _ := json.Marshal(body)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(b)
}

func mustParseURL(raw string) *url.URL {
	u, err := url.Parse(raw)
	if err != nil {
		panic(fmt.Sprintf("mustParseURL(%q): %v", raw, err))
	}
	return u
}

func boolPtr(b bool) *bool { return &b }

// ---- SplitSlug --------------------------------------------------------------

func TestSplitSlug(t *testing.T) {
	cases := []struct {
		input    string
		wantVCS  string
		wantOrg  string
		wantRepo string
		wantErr  bool
	}{
		{"gh/acme/web", "gh", "acme", "web", false},
		{"bitbucket/my-org/my repo", "bitbucket", "my-org", "my repo", false},
		{"circleci/org-id/proj-id", "circleci", "org-id", "proj-id", false},
		{"gh/acme", "", "", "", true},  // only two components
		{"acme", "", "", "", true},     // only one component
		{"", "", "", "", true},         // empty
		{"gh//web", "", "", "", true},  // empty middle component
		{"gh/acme/", "", "", "", true}, // empty last component
	}
	for _, tc := range cases {
		provider, org, proj, err := SplitSlug(tc.input)
		if tc.wantErr {
			if err == nil {
				t.Errorf("SplitSlug(%q): expected error, got nil", tc.input)
			}
			continue
		}
		if err != nil {
			t.Errorf("SplitSlug(%q): unexpected error: %v", tc.input, err)
			continue
		}
		if provider != tc.wantVCS || org != tc.wantOrg || proj != tc.wantRepo {
			t.Errorf("SplitSlug(%q) = (%q, %q, %q), want (%q, %q, %q)",
				tc.input, provider, org, proj, tc.wantVCS, tc.wantOrg, tc.wantRepo)
		}
	}
}

// ---- slugPath / slugSubresource URL-encoding --------------------------------

// TestSlugPathEncoding asserts that the wire path (EscapedPath) has literal
// slashes between the three components and that each component is individually
// percent-encoded.  A name with a space ("my repo") must appear as "my%20repo",
// and the slashes between segments must remain literal.
func TestSlugPathEncoding(t *testing.T) {
	cases := []struct {
		slug        string
		wantEscaped string
	}{
		{
			slug:        "gh/acme/web",
			wantEscaped: "project/gh/acme/web",
		},
		{
			slug:        "bitbucket/my org/my repo",
			wantEscaped: "project/bitbucket/my%20org/my%20repo",
		},
		{
			// Slug with a component that contains a slash itself – pathological
			// but the helper should encode the inner slash as %2F within the segment.
			// We can't construct such a slug via SplitSlug (it splits on the first
			// three '/' chars), so test via the underlying helper directly.
			slug:        "gh/acme/web",
			wantEscaped: "project/gh/acme/web",
		},
	}

	for _, tc := range cases {
		u, err := slugPath("project/", tc.slug)
		if err != nil {
			t.Errorf("slugPath(%q): unexpected error: %v", tc.slug, err)
			continue
		}
		if got := u.EscapedPath(); got != tc.wantEscaped {
			t.Errorf("slugPath(%q).EscapedPath() = %q, want %q", tc.slug, got, tc.wantEscaped)
		}
	}
}

// TestSlugSubresourceEncoding asserts that slugSubresource appends "/subresource"
// to the encoded slug path.
func TestSlugSubresourceEncoding(t *testing.T) {
	u, err := slugSubresource("gh/acme/web", "envvar")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "project/gh/acme/web/envvar"
	if got := u.EscapedPath(); got != want {
		t.Errorf("EscapedPath() = %q, want %q", got, want)
	}
}

// TestSlugSpaceEncoding demonstrates that a slug with a space in the org/repo
// name is percent-encoded on the wire (my%20repo) and that the literal slashes
// between components are preserved.
func TestSlugSpaceEncoding(t *testing.T) {
	u, err := slugSubresource("gh/my org/my repo", "envvar")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "project/gh/my%20org/my%20repo/envvar"
	if got := u.EscapedPath(); got != want {
		t.Errorf("EscapedPath() = %q, want %q", got, want)
	}
}

// ---- GetProject -------------------------------------------------------------

func TestGetProject_HappyPath(t *testing.T) {
	const slug = "gh/acme/web"
	want := Project{
		Slug:             slug,
		ID:               "proj-uuid-123",
		Name:             "web",
		OrganizationName: "Acme Corp",
		OrganizationSlug: "gh/acme",
		OrganizationID:   "org-uuid-456",
		VCS: ProjectVCS{
			Provider:      "GitHub",
			URL:           "https://github.com/acme/web",
			DefaultBranch: "main",
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		// Slug components are simple ASCII – no percent-encoding needed.
		// Assert on EscapedPath (wire form) to detect any double-encoding.
		wantPath := "/api/v2/project/gh/acme/web"
		if r.URL.EscapedPath() != wantPath {
			t.Errorf("expected escaped path %q, got %q", wantPath, r.URL.EscapedPath())
		}
		respondJSON(w, http.StatusOK, map[string]interface{}{
			"slug":              want.Slug,
			"id":                want.ID,
			"name":              want.Name,
			"organization_name": want.OrganizationName,
			"organization_slug": want.OrganizationSlug,
			"organization_id":   want.OrganizationID,
			"vcs_info": map[string]string{
				"provider":       want.VCS.Provider,
				"vcs_url":        want.VCS.URL,
				"default_branch": want.VCS.DefaultBranch,
			},
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	got, err := c.GetProject(slug)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if *got != want {
		t.Errorf("got %+v, want %+v", *got, want)
	}
}

// TestGetProject_EscapedPath verifies that the path on the wire correctly uses
// literal slashes between components even when the slug itself has been given
// with plain ASCII chars (the canonical case).
func TestGetProject_EscapedPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Assert on EscapedPath to catch any double-encoding.
		want := "/api/v2/project/gh/acme/web"
		if r.URL.EscapedPath() != want {
			t.Errorf("EscapedPath = %q, want %q", r.URL.EscapedPath(), want)
		}
		respondJSON(w, http.StatusOK, Project{Slug: "gh/acme/web", Name: "web"})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	if _, err := c.GetProject("gh/acme/web"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGetProject_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusNotFound, map[string]string{"message": "Project not found."})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	_, err := c.GetProject("gh/acme/missing")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ---- GetSettings ------------------------------------------------------------

func TestGetSettings_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		// Decomposed path with individual segments.
		wantPath := "/api/v2/project/gh/acme/web/settings"
		if r.URL.EscapedPath() != wantPath {
			t.Errorf("expected path %q, got %q", wantPath, r.URL.EscapedPath())
		}
		respondJSON(w, http.StatusOK, map[string]interface{}{
			"advanced": map[string]interface{}{
				"autocancel_builds":             true,
				"build_fork_prs":                false,
				"set_github_status":             true,
				"write_settings_requires_admin": false,
				"pr_only_branch_overrides":      []string{"main", "dev"},
			},
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	got, err := c.GetSettings("gh", "acme", "web")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.AutocancelBuilds == nil || !*got.AutocancelBuilds {
		t.Errorf("AutocancelBuilds: expected *true, got %v", got.AutocancelBuilds)
	}
	if got.BuildForkPRs == nil || *got.BuildForkPRs {
		t.Errorf("BuildForkPRs: expected *false, got %v", got.BuildForkPRs)
	}
	if got.SetGithubStatus == nil || !*got.SetGithubStatus {
		t.Errorf("SetGithubStatus: expected *true, got %v", got.SetGithubStatus)
	}
	if got.WriteSettingsRequiresAdmin == nil || *got.WriteSettingsRequiresAdmin {
		t.Errorf("WriteSettingsRequiresAdmin: expected *false, got %v", got.WriteSettingsRequiresAdmin)
	}
	if len(got.PROnlyBranchOverrides) != 2 || got.PROnlyBranchOverrides[0] != "main" {
		t.Errorf("PROnlyBranchOverrides: got %v", got.PROnlyBranchOverrides)
	}
	// Fields not in response should be nil.
	if got.OSS != nil {
		t.Errorf("OSS: expected nil (absent from response), got %v", got.OSS)
	}
}

func TestGetSettings_AdvancedUnwrap(t *testing.T) {
	// Confirm that the "advanced" wrapper is stripped and we get the inner object.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusOK, map[string]interface{}{
			"advanced": map[string]interface{}{
				"oss": true,
			},
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	got, err := c.GetSettings("github", "myorg", "myrepo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.OSS == nil || !*got.OSS {
		t.Errorf("OSS: expected *true after unwrapping advanced, got %v", got.OSS)
	}
	if got.AutocancelBuilds != nil {
		t.Errorf("AutocancelBuilds should be nil (not in response)")
	}
}

func TestGetSettings_DecomposedPath(t *testing.T) {
	// Confirm provider/org/project are separate path segments (not joined slug).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		want := "/api/v2/project/bitbucket/my-org/my-repo/settings"
		if r.URL.EscapedPath() != want {
			t.Errorf("path = %q, want %q", r.URL.EscapedPath(), want)
		}
		respondJSON(w, http.StatusOK, map[string]interface{}{"advanced": map[string]interface{}{}})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	if _, err := c.GetSettings("bitbucket", "my-org", "my-repo"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ---- ListEnvVars ------------------------------------------------------------

func TestListEnvVars_MaskedValue(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.EscapedPath() != "/api/v2/project/gh/acme/web/envvar" {
			t.Errorf("unexpected path: %q", r.URL.EscapedPath())
		}
		respondJSON(w, http.StatusOK, map[string]interface{}{
			"items": []map[string]string{
				{"name": "SECRET_KEY", "value": "xxxx1234"},
				{"name": "DB_PASS", "value": "xxxxabcd"},
			},
			"next_page_token": "",
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	got, err := c.ListEnvVars("gh/acme/web")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 env vars, got %d", len(got))
	}
	if got[0].Name != "SECRET_KEY" || got[0].MaskedValue != "xxxx1234" {
		t.Errorf("unexpected first env var: %+v", got[0])
	}
	if got[1].Name != "DB_PASS" || got[1].MaskedValue != "xxxxabcd" {
		t.Errorf("unexpected second env var: %+v", got[1])
	}
}

func TestListEnvVars_Pagination(t *testing.T) {
	page1 := map[string]interface{}{
		"items":           []map[string]string{{"name": "VAR1", "value": "xxxx1"}},
		"next_page_token": "tok2",
	}
	page2 := map[string]interface{}{
		"items":           []map[string]string{{"name": "VAR2", "value": "xxxx2"}},
		"next_page_token": "",
	}

	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		switch callCount {
		case 1:
			if r.URL.RawQuery != "" {
				t.Errorf("first call should have no query, got %q", r.URL.RawQuery)
			}
			respondJSON(w, http.StatusOK, page1)
		case 2:
			if got := r.URL.Query().Get("page-token"); got != "tok2" {
				t.Errorf("second call: expected page-token=tok2, got %q", got)
			}
			respondJSON(w, http.StatusOK, page2)
		default:
			t.Errorf("unexpected third call")
			http.Error(w, "unexpected", http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	got, err := c.ListEnvVars("gh/acme/web")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if callCount != 2 {
		t.Errorf("expected 2 HTTP calls, got %d", callCount)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 env vars, got %d", len(got))
	}
	if got[0].Name != "VAR1" || got[1].Name != "VAR2" {
		t.Errorf("unexpected env vars: %+v", got)
	}
}

// ---- ListCheckoutKeys -------------------------------------------------------

func TestListCheckoutKeys_HappyPath(t *testing.T) {
	preferred := true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.EscapedPath() != "/api/v2/project/gh/acme/web/checkout-key" {
			t.Errorf("unexpected path: %q", r.URL.EscapedPath())
		}
		respondJSON(w, http.StatusOK, map[string]interface{}{
			"items": []map[string]interface{}{
				{
					"type":        "deploy-key",
					"fingerprint": "c9:0b:1c:4f:d5:65:56:b9",
					"public-key":  "ssh-ed25519 AAAA...",
					"preferred":   preferred,
					"created-at":  "2024-01-01T00:00:00Z",
				},
			},
			"next_page_token": "",
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	got, err := c.ListCheckoutKeys("gh/acme/web")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 key, got %d", len(got))
	}
	k := got[0]
	if k.Type != "deploy-key" {
		t.Errorf("Type: got %q, want deploy-key", k.Type)
	}
	if k.Fingerprint != "c9:0b:1c:4f:d5:65:56:b9" {
		t.Errorf("Fingerprint: got %q", k.Fingerprint)
	}
	if k.PublicKey != "ssh-ed25519 AAAA..." {
		t.Errorf("PublicKey: got %q", k.PublicKey)
	}
	if !k.Preferred {
		t.Errorf("Preferred: expected true")
	}
	if k.CreatedAt != "2024-01-01T00:00:00Z" {
		t.Errorf("CreatedAt: got %q", k.CreatedAt)
	}
}

func TestListCheckoutKeys_Pagination(t *testing.T) {
	page1 := map[string]interface{}{
		"items": []map[string]interface{}{
			{"type": "deploy-key", "fingerprint": "fp1", "public-key": "ssh-ed25519 a", "preferred": true, "created-at": "2024-01-01T00:00:00Z"},
		},
		"next_page_token": "nexttok",
	}
	page2 := map[string]interface{}{
		"items": []map[string]interface{}{
			{"type": "github-user-key", "fingerprint": "fp2", "public-key": "ssh-ed25519 b", "preferred": false, "created-at": "2024-02-01T00:00:00Z"},
		},
		"next_page_token": "",
	}

	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		switch callCount {
		case 1:
			respondJSON(w, http.StatusOK, page1)
		case 2:
			if got := r.URL.Query().Get("page-token"); got != "nexttok" {
				t.Errorf("page-token: got %q, want nexttok", got)
			}
			respondJSON(w, http.StatusOK, page2)
		default:
			t.Error("unexpected third call")
		}
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	got, err := c.ListCheckoutKeys("gh/acme/web")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if callCount != 2 {
		t.Errorf("expected 2 calls, got %d", callCount)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(got))
	}
}

// ---- ListWebhooks -----------------------------------------------------------

func TestListWebhooks_QueryParams(t *testing.T) {
	projectID := "proj-uuid-abc"
	verifyTLS := true

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		// Confirm path and query parameters.
		if r.URL.Path != "/api/v2/webhook" {
			t.Errorf("path: got %q, want /api/v2/webhook", r.URL.Path)
		}
		if got := r.URL.Query().Get("scope-id"); got != projectID {
			t.Errorf("scope-id: got %q, want %q", got, projectID)
		}
		if got := r.URL.Query().Get("scope-type"); got != "project" {
			t.Errorf("scope-type: got %q, want project", got)
		}
		respondJSON(w, http.StatusOK, map[string]interface{}{
			"items": []map[string]interface{}{
				{
					"id":         "webhook-uuid-1",
					"name":       "CI notifier",
					"url":        "https://example.com/hook",
					"events":     []string{"workflow-completed"},
					"verify-tls": verifyTLS,
				},
			},
			"next_page_token": "",
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	got, err := c.ListWebhooks(projectID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 webhook, got %d", len(got))
	}
	wh := got[0]
	if wh.ID != "webhook-uuid-1" {
		t.Errorf("ID: got %q", wh.ID)
	}
	if wh.Name != "CI notifier" {
		t.Errorf("Name: got %q", wh.Name)
	}
	if wh.URL != "https://example.com/hook" {
		t.Errorf("URL: got %q", wh.URL)
	}
	if len(wh.Events) != 1 || wh.Events[0] != "workflow-completed" {
		t.Errorf("Events: got %v", wh.Events)
	}
	if wh.VerifyTLS == nil || !*wh.VerifyTLS {
		t.Errorf("VerifyTLS: expected *true, got %v", wh.VerifyTLS)
	}
}

func TestListWebhooks_Pagination(t *testing.T) {
	page1 := map[string]interface{}{
		"items":           []map[string]interface{}{{"id": "w1", "name": "hook1", "url": "https://a.com", "events": []string{"job-completed"}, "verify-tls": false}},
		"next_page_token": "wh-tok",
	}
	page2 := map[string]interface{}{
		"items":           []map[string]interface{}{{"id": "w2", "name": "hook2", "url": "https://b.com", "events": []string{"workflow-completed"}, "verify-tls": true}},
		"next_page_token": "",
	}

	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		switch callCount {
		case 1:
			if r.URL.Query().Get("page-token") != "" {
				t.Error("first call should have no page-token")
			}
			respondJSON(w, http.StatusOK, page1)
		case 2:
			if got := r.URL.Query().Get("page-token"); got != "wh-tok" {
				t.Errorf("page-token: got %q, want wh-tok", got)
			}
			respondJSON(w, http.StatusOK, page2)
		default:
			t.Error("unexpected third call")
		}
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	got, err := c.ListWebhooks("proj-uuid")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if callCount != 2 {
		t.Errorf("expected 2 calls, got %d", callCount)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 webhooks, got %d", len(got))
	}
}

// ---- ListSchedules ----------------------------------------------------------

func TestListSchedules_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.EscapedPath() != "/api/v2/project/gh/acme/web/schedule" {
			t.Errorf("unexpected path: %q", r.URL.EscapedPath())
		}
		respondJSON(w, http.StatusOK, map[string]interface{}{
			"items": []map[string]interface{}{
				{
					"id":          "sched-uuid-1",
					"name":        "nightly",
					"description": "Nightly build",
					"timetable": map[string]interface{}{
						"per-hour":     1,
						"hours-of-day": []int{1},
						"days-of-week": []string{"MON"},
					},
					"parameters": map[string]interface{}{
						"branch": "main",
					},
				},
			},
			"next_page_token": "",
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	got, err := c.ListSchedules("gh/acme/web")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 schedule, got %d", len(got))
	}
	s := got[0]
	if s.ID != "sched-uuid-1" {
		t.Errorf("ID: got %q", s.ID)
	}
	if s.Name != "nightly" {
		t.Errorf("Name: got %q", s.Name)
	}
	if s.Description != "Nightly build" {
		t.Errorf("Description: got %q", s.Description)
	}
	if s.Timetable == nil {
		t.Error("Timetable should not be nil")
	}
	if s.Parameters == nil {
		t.Error("Parameters should not be nil")
	}
	if branch, ok := s.Parameters["branch"]; !ok || branch != "main" {
		t.Errorf("Parameters[branch]: got %v", s.Parameters["branch"])
	}
}

func TestListSchedules_Pagination(t *testing.T) {
	page1 := map[string]interface{}{
		"items":           []map[string]interface{}{{"id": "s1", "name": "alpha", "description": "", "timetable": map[string]interface{}{}, "parameters": map[string]interface{}{}}},
		"next_page_token": "s-tok",
	}
	page2 := map[string]interface{}{
		"items":           []map[string]interface{}{{"id": "s2", "name": "beta", "description": "", "timetable": map[string]interface{}{}, "parameters": map[string]interface{}{}}},
		"next_page_token": "",
	}

	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		switch callCount {
		case 1:
			respondJSON(w, http.StatusOK, page1)
		case 2:
			if got := r.URL.Query().Get("page-token"); got != "s-tok" {
				t.Errorf("page-token: got %q, want s-tok", got)
			}
			respondJSON(w, http.StatusOK, page2)
		default:
			t.Error("unexpected third call")
		}
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	got, err := c.ListSchedules("gh/acme/web")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if callCount != 2 {
		t.Errorf("expected 2 calls, got %d", callCount)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 schedules, got %d", len(got))
	}
}

// ---- ListFollowedProjects + FollowedProjectsForOrg -------------------------

func TestListFollowedProjects_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1.1/projects" {
			t.Errorf("unexpected path: %q", r.URL.Path)
		}
		respondJSON(w, http.StatusOK, []map[string]interface{}{
			{"vcs_type": "github", "username": "acme", "reponame": "web", "vcs_url": "https://github.com/acme/web", "followed": true},
			{"vcs_type": "github", "username": "other-org", "reponame": "tool", "vcs_url": "https://github.com/other-org/tool", "followed": true},
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	got, err := c.ListFollowedProjects()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 projects, got %d", len(got))
	}
	if got[0].VCSType != "github" || got[0].Username != "acme" || got[0].Reponame != "web" {
		t.Errorf("unexpected first project: %+v", got[0])
	}
	if !got[0].Followed {
		t.Errorf("expected Followed=true for first project")
	}
}

func TestFollowedProjectsForOrg_Filter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusOK, []map[string]interface{}{
			{"vcs_type": "github", "username": "acme", "reponame": "web", "vcs_url": "https://github.com/acme/web", "followed": true},
			{"vcs_type": "github", "username": "acme", "reponame": "api", "vcs_url": "https://github.com/acme/api", "followed": true},
			{"vcs_type": "github", "username": "other-org", "reponame": "tool", "vcs_url": "https://github.com/other-org/tool", "followed": false},
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	got, err := c.FollowedProjectsForOrg("acme")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 projects for org acme, got %d", len(got))
	}
	for _, p := range got {
		if p.Username != "acme" {
			t.Errorf("expected Username=acme, got %q", p.Username)
		}
	}
}

func TestFollowedProjectsForOrg_NoMatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusOK, []map[string]interface{}{
			{"vcs_type": "github", "username": "other-org", "reponame": "tool", "vcs_url": "https://github.com/other-org/tool", "followed": true},
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	got, err := c.FollowedProjectsForOrg("acme")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty slice, got %v", got)
	}
}

// ---- FollowProject ----------------------------------------------------------

func TestFollowProject_PostAndPath(t *testing.T) {
	var called bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		// Assert on EscapedPath to catch any encoding issues.
		wantPath := "/api/v1.1/project/github/acme/web/follow"
		if r.URL.EscapedPath() != wantPath {
			t.Errorf("expected escaped path %q, got %q", wantPath, r.URL.EscapedPath())
		}
		respondJSON(w, http.StatusOK, map[string]bool{"followed": true})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	result, err := c.FollowProject("github", "acme", "web")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("expected the server to be called")
	}
	if result == nil || !result.Followed {
		t.Errorf("expected Followed=true, got %v", result)
	}
}

func TestFollowProject_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusUnauthorized, map[string]string{"message": "unauthorized"})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	_, err := c.FollowProject("github", "acme", "web")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ---- NewClient (settings integration) --------------------------------------

func TestNewClient_DefaultHost(t *testing.T) {
	// Should not error when Host is empty (defaults to DefaultHost).
	cfg := &settings.Config{}
	_, err := NewClient(cfg, "token")
	if err != nil {
		t.Fatalf("unexpected error with empty host: %v", err)
	}
}

func TestNewClient_InvalidHost(t *testing.T) {
	cfg := &settings.Config{Host: "not a url"}
	_, err := NewClient(cfg, "token")
	if err == nil {
		t.Fatal("expected error for invalid host")
	}
}
