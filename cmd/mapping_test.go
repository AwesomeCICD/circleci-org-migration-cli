package cmd_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AwesomeCICD/circleci-org-migration-cli/cmd"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
)

// ---------------------------------------------------------------------------
// Pure-function unit tests for matchProjects
// ---------------------------------------------------------------------------

// TestMatchProjects_AllMatched verifies that when every source slug has a
// corresponding dest project by repo name, all pairs appear in matched and
// both unmatchedSrc and destOnly are empty.
func TestMatchProjects_AllMatched(t *testing.T) {
	srcSlugs := []string{"gh/old-org/web", "gh/old-org/api"}
	destProjects := []mappingOrgProject{
		{ID: "p1", Slug: "gh/new-org/web", Name: "web"},
		{ID: "p2", Slug: "gh/new-org/api", Name: "api"},
	}

	matched, unmatchedSrc, destOnly := matchProjectsHelper(srcSlugs, destProjects)

	if len(matched) != 2 {
		t.Errorf("matched len = %d; want 2", len(matched))
	}
	if matched["gh/old-org/web"] != "gh/new-org/web" {
		t.Errorf("matched[web] = %q; want gh/new-org/web", matched["gh/old-org/web"])
	}
	if matched["gh/old-org/api"] != "gh/new-org/api" {
		t.Errorf("matched[api] = %q; want gh/new-org/api", matched["gh/old-org/api"])
	}
	if len(unmatchedSrc) != 0 {
		t.Errorf("unmatchedSrc = %v; want empty", unmatchedSrc)
	}
	if len(destOnly) != 0 {
		t.Errorf("destOnly = %v; want empty", destOnly)
	}
}

// TestMatchProjects_UnmatchedSource verifies that source slugs with no
// matching dest project are reported in unmatchedSrc.
func TestMatchProjects_UnmatchedSource(t *testing.T) {
	srcSlugs := []string{"gh/old-org/web", "gh/old-org/missing"}
	destProjects := []mappingOrgProject{
		{ID: "p1", Slug: "gh/new-org/web", Name: "web"},
	}

	matched, unmatchedSrc, destOnly := matchProjectsHelper(srcSlugs, destProjects)

	if len(matched) != 1 {
		t.Errorf("matched len = %d; want 1", len(matched))
	}
	if len(unmatchedSrc) != 1 || unmatchedSrc[0] != "gh/old-org/missing" {
		t.Errorf("unmatchedSrc = %v; want [gh/old-org/missing]", unmatchedSrc)
	}
	if len(destOnly) != 0 {
		t.Errorf("destOnly = %v; want empty", destOnly)
	}
}

// TestMatchProjects_DestOnly verifies that dest projects with no source
// counterpart are reported in destOnly.
func TestMatchProjects_DestOnly(t *testing.T) {
	srcSlugs := []string{"gh/old-org/web"}
	destProjects := []mappingOrgProject{
		{ID: "p1", Slug: "gh/new-org/web", Name: "web"},
		{ID: "p2", Slug: "gh/new-org/extras", Name: "extras"},
	}

	matched, unmatchedSrc, destOnly := matchProjectsHelper(srcSlugs, destProjects)

	if len(matched) != 1 {
		t.Errorf("matched len = %d; want 1", len(matched))
	}
	if len(unmatchedSrc) != 0 {
		t.Errorf("unmatchedSrc = %v; want empty", unmatchedSrc)
	}
	if len(destOnly) != 1 || destOnly[0] != "gh/new-org/extras" {
		t.Errorf("destOnly = %v; want [gh/new-org/extras]", destOnly)
	}
}

// TestMatchProjects_Mixed verifies the combined case: some matched, some
// unmatched source, and some dest-only.
func TestMatchProjects_Mixed(t *testing.T) {
	srcSlugs := []string{
		"gh/old-org/web",
		"gh/old-org/api",
		"gh/old-org/missing",
	}
	destProjects := []mappingOrgProject{
		{ID: "p1", Slug: "gh/new-org/web", Name: "web"},
		{ID: "p2", Slug: "gh/new-org/api", Name: "api"},
		{ID: "p3", Slug: "gh/new-org/bonus", Name: "bonus"},
	}

	matched, unmatchedSrc, destOnly := matchProjectsHelper(srcSlugs, destProjects)

	if len(matched) != 2 {
		t.Errorf("matched len = %d; want 2", len(matched))
	}
	if len(unmatchedSrc) != 1 || unmatchedSrc[0] != "gh/old-org/missing" {
		t.Errorf("unmatchedSrc = %v; want [gh/old-org/missing]", unmatchedSrc)
	}
	if len(destOnly) != 1 || destOnly[0] != "gh/new-org/bonus" {
		t.Errorf("destOnly = %v; want [gh/new-org/bonus]", destOnly)
	}
}

// TestMatchProjects_EmptySource verifies that an empty source list produces
// no matched/unmatchedSrc entries, and all dest projects go to destOnly.
func TestMatchProjects_EmptySource(t *testing.T) {
	var srcSlugs []string
	destProjects := []mappingOrgProject{
		{ID: "p1", Slug: "gh/new-org/web", Name: "web"},
	}

	matched, unmatchedSrc, destOnly := matchProjectsHelper(srcSlugs, destProjects)

	if len(matched) != 0 {
		t.Errorf("matched = %v; want empty", matched)
	}
	if len(unmatchedSrc) != 0 {
		t.Errorf("unmatchedSrc = %v; want empty", unmatchedSrc)
	}
	if len(destOnly) != 1 {
		t.Errorf("destOnly len = %d; want 1", len(destOnly))
	}
}

// TestMatchProjects_EmptyDest verifies that when dest has no projects all
// source slugs land in unmatchedSrc and destOnly is empty.
func TestMatchProjects_EmptyDest(t *testing.T) {
	srcSlugs := []string{"gh/old-org/web"}
	var destProjects []mappingOrgProject

	matched, unmatchedSrc, destOnly := matchProjectsHelper(srcSlugs, destProjects)

	if len(matched) != 0 {
		t.Errorf("matched = %v; want empty", matched)
	}
	if len(unmatchedSrc) != 1 || unmatchedSrc[0] != "gh/old-org/web" {
		t.Errorf("unmatchedSrc = %v; want [gh/old-org/web]", unmatchedSrc)
	}
	if len(destOnly) != 0 {
		t.Errorf("destOnly = %v; want empty", destOnly)
	}
}

// TestMatchProjects_AppSlugs verifies that GitHub App slugs
// (circleci/<uuid>/<uuid>) are matched by name correctly.
func TestMatchProjects_AppSlugs(t *testing.T) {
	srcSlugs := []string{"gh/old-org/web"}
	destProjects := []mappingOrgProject{
		{ID: "proj-uuid", Slug: "circleci/org-uuid/proj-uuid", Name: "web"},
	}

	matched, unmatchedSrc, destOnly := matchProjectsHelper(srcSlugs, destProjects)

	if len(matched) != 1 {
		t.Errorf("matched len = %d; want 1", len(matched))
	}
	if matched["gh/old-org/web"] != "circleci/org-uuid/proj-uuid" {
		t.Errorf("matched[gh/old-org/web] = %q; want circleci/org-uuid/proj-uuid", matched["gh/old-org/web"])
	}
	if len(unmatchedSrc) != 0 {
		t.Errorf("unmatchedSrc = %v; want empty", unmatchedSrc)
	}
	if len(destOnly) != 0 {
		t.Errorf("destOnly = %v; want empty", destOnly)
	}
}

// ---------------------------------------------------------------------------
// Command-layer tests (httptest-backed)
// ---------------------------------------------------------------------------

// TestMappingGenerateCommand_MissingManifest verifies that omitting --manifest
// returns an error mentioning "manifest".
func TestMappingGenerateCommand_MissingManifest(t *testing.T) {
	t.Setenv("CIRCLECI_DEST_TOKEN", "fake-token")
	_, _, err := runCmd(t, "mapping", "generate", "--dest-org", "gh/new-org")
	if err == nil {
		t.Fatal("expected error when --manifest is missing, got nil")
	}
	if !strings.Contains(err.Error(), "manifest") {
		t.Errorf("error %q does not mention 'manifest'", err.Error())
	}
}

// TestMappingGenerateCommand_MissingDestOrg verifies that omitting --dest-org
// returns an error mentioning "dest-org".
func TestMappingGenerateCommand_MissingDestOrg(t *testing.T) {
	t.Setenv("CIRCLECI_DEST_TOKEN", "fake-token")
	_, _, err := runCmd(t, "mapping", "generate", "--manifest", "manifest.json")
	if err == nil {
		t.Fatal("expected error when --dest-org is missing, got nil")
	}
	if !strings.Contains(err.Error(), "dest-org") {
		t.Errorf("error %q does not mention 'dest-org'", err.Error())
	}
}

// TestMappingGenerateCommand_MissingToken verifies that when no token is
// available the error mentions "token".
func TestMappingGenerateCommand_MissingToken(t *testing.T) {
	t.Setenv("CIRCLECI_CLI_TOKEN", "")
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "")
	t.Setenv("CIRCLECI_DEST_TOKEN", "")
	t.Setenv("CIRCLE_TOKEN", "")

	_, _, err := runCmd(t, "mapping", "generate",
		"--manifest", "manifest.json",
		"--dest-org", "gh/new-org",
	)
	if err == nil {
		t.Fatal("expected error when no token is available, got nil")
	}
	if !strings.Contains(err.Error(), "token") {
		t.Errorf("error %q does not mention 'token'", err.Error())
	}
}

// TestMappingGenerateCommand_WritesMapping is an integration-style test that
// wires httptest fakes for ResolveOrgID (GET /api/v2/organization/...) and
// ListOrgProjects (GET /api/private/project), then asserts that:
//   - the mapping.json output is valid and loadable by manifest.LoadMapping
//   - the stdout report contains the expected sections
func TestMappingGenerateCommand_WritesMapping(t *testing.T) {
	// Fake API server that handles both endpoints.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/v2/organization/"):
			// ResolveOrgID → GetOrganization
			respondJSONHelper(w, http.StatusOK, map[string]interface{}{
				"id":       "dest-org-uuid",
				"name":     "new-org",
				"slug":     "gh/new-org",
				"vcs_type": "github",
			})
		case r.URL.Path == "/api/private/project":
			respondJSONHelper(w, http.StatusOK, map[string]interface{}{
				"items": []map[string]interface{}{
					{"id": "p1", "slug": "gh/new-org/web", "name": "web"},
					{"id": "p2", "slug": "gh/new-org/extra", "name": "extra"},
				},
				"next_page_token": "",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	// Write a minimal manifest.
	tmpDir := t.TempDir()
	manifestPath := filepath.Join(tmpDir, "manifest.json")
	writeTestManifest(t, manifestPath, []string{"gh/old-org/web", "gh/old-org/missing"})

	outputPath := filepath.Join(tmpDir, "mapping.json")

	// Run the command against the fake server.
	root := cmd.MakeCommands()
	root.PersistentFlags().Set("host", srv.URL) //nolint:errcheck
	var outBuf, errBuf strings.Builder
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	root.SetArgs([]string{
		"mapping", "generate",
		"--manifest", manifestPath,
		"--dest-org", "gh/new-org",
		"-o", outputPath,
		"--dest-token", "fake-test-token",
	})

	if err := root.Execute(); err != nil {
		t.Fatalf("mapping generate: %v\nstdout: %s\nstderr: %s", err, outBuf.String(), errBuf.String())
	}

	// --- Verify mapping file ------------------------------------------------
	mp, err := manifest.LoadMapping(outputPath)
	if err != nil {
		t.Fatalf("LoadMapping: %v", err)
	}
	if mp.Projects["gh/old-org/web"] != "gh/new-org/web" {
		t.Errorf("mapping[gh/old-org/web] = %q; want gh/new-org/web", mp.Projects["gh/old-org/web"])
	}
	// gh/old-org/missing has no onboarded dest project but the dest org is
	// gh/ so the slug is DERIVED as gh/new-org/missing and written to the mapping.
	if mp.Projects["gh/old-org/missing"] != "gh/new-org/missing" {
		t.Errorf("mapping[gh/old-org/missing] = %q; want gh/new-org/missing (derived)", mp.Projects["gh/old-org/missing"])
	}
	if mp.Org.From != "gh/old-org" {
		t.Errorf("org.from = %q; want gh/old-org", mp.Org.From)
	}
	if mp.Org.To != "gh/new-org" {
		t.Errorf("org.to = %q; want gh/new-org", mp.Org.To)
	}

	// --- Verify stdout report -----------------------------------------------
	out := outBuf.String()
	if !strings.Contains(out, "Matched") {
		t.Errorf("stdout missing 'Matched' section:\n%s", out)
	}
	// gh/old-org/missing should appear in the "derived" section, not "Unmatched source"
	if !strings.Contains(out, "derived") {
		t.Errorf("stdout missing 'derived' section:\n%s", out)
	}
	if !strings.Contains(out, "gh/old-org/web") {
		t.Errorf("stdout missing matched project gh/old-org/web:\n%s", out)
	}
	if !strings.Contains(out, "gh/old-org/missing") {
		t.Errorf("stdout missing derived project gh/old-org/missing:\n%s", out)
	}
	// dest-only: extra has no source counterpart
	if !strings.Contains(out, "gh/new-org/extra") {
		t.Errorf("stdout missing dest-only project gh/new-org/extra:\n%s", out)
	}
}

// TestMappingGenerateCommand_HelpWorks verifies that 'mapping generate --help'
// exits 0 and contains the expected key phrases.
func TestMappingGenerateCommand_HelpWorks(t *testing.T) {
	out, _, err := runCmd(t, "mapping", "generate", "--help")
	if err != nil {
		t.Fatalf("mapping generate --help: %v", err)
	}
	for _, phrase := range []string{"--manifest", "--dest-org", "--output"} {
		if !strings.Contains(out, phrase) {
			t.Errorf("help output missing %q:\n%s", phrase, out)
		}
	}
}

// TestMappingCommand_HelpWorks verifies that 'mapping --help' exits 0 and
// lists the generate subcommand.
func TestMappingCommand_HelpWorks(t *testing.T) {
	out, _, err := runCmd(t, "mapping", "--help")
	if err != nil {
		t.Fatalf("mapping --help: %v", err)
	}
	if !strings.Contains(out, "generate") {
		t.Errorf("mapping --help missing 'generate' subcommand:\n%s", out)
	}
}

// ---------------------------------------------------------------------------
// normalizeVCSPrefix unit tests
// ---------------------------------------------------------------------------

// normalizeVCSPrefixHelper is a self-contained mirror of cmd.normalizeVCSPrefix
// for use in package cmd_test (which cannot call unexported functions directly).
func normalizeVCSPrefixHelper(slug string) string {
	if strings.HasPrefix(slug, "github/") {
		return "gh/" + strings.TrimPrefix(slug, "github/")
	}
	if strings.HasPrefix(slug, "bitbucket/") {
		return "bb/" + strings.TrimPrefix(slug, "bitbucket/")
	}
	return slug
}

// TestNormalizeVCSPrefix verifies that VCS provider prefix normalization produces
// the expected canonical short forms.
func TestNormalizeVCSPrefix(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"github/my-org", "gh/my-org"},
		{"github/my-org/my-repo", "gh/my-org/my-repo"},
		{"bitbucket/my-org", "bb/my-org"},
		{"bitbucket/my-org/my-repo", "bb/my-org/my-repo"},
		{"gh/my-org", "gh/my-org"},
		{"gh/my-org/my-repo", "gh/my-org/my-repo"},
		{"bb/my-org", "bb/my-org"},
		{"bb/my-org/my-repo", "bb/my-org/my-repo"},
		{"circleci/uuid-org/uuid-proj", "circleci/uuid-org/uuid-proj"},
		{"", ""},
		{"just-a-name", "just-a-name"},
		// "githubfoo/" is NOT a github/ prefix — should be returned unchanged.
		{"githubfoo/org", "githubfoo/org"},
	}
	for _, tc := range cases {
		got := normalizeVCSPrefixHelper(tc.input)
		if got != tc.want {
			t.Errorf("normalizeVCSPrefix(%q) = %q; want %q", tc.input, got, tc.want)
		}
	}
}

// TestMappingGenerateCommand_NormalizesVCSPrefixes verifies that when the user
// passes --dest-org with a "github/" prefix, the written mapping.json uses the
// canonical "gh/" prefix for both org.to and project slugs.
func TestMappingGenerateCommand_NormalizesVCSPrefixes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/v2/organization/"):
			respondJSONHelper(w, http.StatusOK, map[string]interface{}{
				"id": "dest-org-uuid", "name": "new-org",
				"slug": "gh/new-org", "vcs_type": "github",
			})
		case r.URL.Path == "/api/private/project":
			respondJSONHelper(w, http.StatusOK, map[string]interface{}{
				"items": []map[string]interface{}{
					{"id": "p1", "slug": "gh/new-org/web", "name": "web"},
				},
				"next_page_token": "",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	tmpDir := t.TempDir()
	// The source manifest uses a "github/" prefix (as a user might export).
	manifestPath := filepath.Join(tmpDir, "manifest.json")
	writeTestManifestWithOrgSlug(t, manifestPath, "github/old-org", []string{"github/old-org/web"})
	outputPath := filepath.Join(tmpDir, "mapping.json")

	root := cmd.MakeCommands()
	root.PersistentFlags().Set("host", srv.URL) //nolint:errcheck
	var outBuf, errBuf strings.Builder
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	root.SetArgs([]string{
		"mapping", "generate",
		"--manifest", manifestPath,
		"--dest-org", "github/new-org", // user passes long form
		"-o", outputPath,
		"--dest-token", "fake-test-token",
	})

	if err := root.Execute(); err != nil {
		t.Fatalf("mapping generate: %v\nstdout: %s\nstderr: %s", err, outBuf.String(), errBuf.String())
	}

	mp, err := manifest.LoadMapping(outputPath)
	if err != nil {
		t.Fatalf("LoadMapping: %v", err)
	}
	if mp.Org.From != "gh/old-org" {
		t.Errorf("org.from = %q; want gh/old-org (normalized from github/old-org)", mp.Org.From)
	}
	if mp.Org.To != "gh/new-org" {
		t.Errorf("org.to = %q; want gh/new-org (normalized from github/new-org)", mp.Org.To)
	}
	// project slug keys must also be normalized
	if _, ok := mp.Projects["github/old-org/web"]; ok {
		t.Error("mapping still contains un-normalized key github/old-org/web; expected gh/old-org/web")
	}
	if _, ok := mp.Projects["gh/old-org/web"]; !ok {
		t.Errorf("mapping missing normalized key gh/old-org/web; projects = %v", mp.Projects)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// mappingOrgProject mirrors project.OrgProject for use in the pure-function
// helper below so we can test matchProjects without importing the api/project
// package from a _test package (which is allowed — we just use a local type
// and convert).
type mappingOrgProject struct {
	ID   string
	Slug string
	Name string
}

// matchProjectsHelper is a self-contained mirror of cmd.matchProjects for use
// in package cmd_test (which cannot call unexported functions directly).
//
// It indexes dest projects by Name (matching the production logic) so that
// both OAuth slugs ("gh/org/web" → Name "web") and App slugs
// ("circleci/uuid/uuid" → Name "web") are handled correctly.
func matchProjectsHelper(
	srcSlugs []string,
	destProjects []mappingOrgProject,
) (matched map[string]string, unmatchedSrc []string, destOnly []string) {
	// Index by Name, mirroring the production matchProjects logic.
	destByName := make(map[string]mappingOrgProject)
	for _, dp := range destProjects {
		if dp.Name == "" {
			continue
		}
		if _, exists := destByName[dp.Name]; !exists {
			destByName[dp.Name] = dp
		}
	}

	matched = make(map[string]string)
	usedDestSlugs := make(map[string]bool)

	for _, src := range srcSlugs {
		name := repoNameHelper(src)
		if dp, ok := destByName[name]; ok {
			matched[src] = dp.Slug
			usedDestSlugs[dp.Slug] = true
		} else {
			unmatchedSrc = append(unmatchedSrc, src)
		}
	}

	for _, dp := range destProjects {
		if !usedDestSlugs[dp.Slug] {
			destOnly = append(destOnly, dp.Slug)
		}
	}
	return matched, unmatchedSrc, destOnly
}

// repoNameHelper mirrors cmd.repoName for the pure-function test helpers.
func repoNameHelper(slug string) string {
	if idx := strings.LastIndex(slug, "/"); idx >= 0 {
		return slug[idx+1:]
	}
	return slug
}

// respondJSONHelper writes v as JSON with the given status code.
func respondJSONHelper(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeTestManifest writes a minimal manifest JSON with the given project slugs.
// The org slug in source is always "gh/old-org".
func writeTestManifest(t *testing.T, path string, slugs []string) {
	t.Helper()
	writeTestManifestWithOrgSlug(t, path, "gh/old-org", slugs)
}

// writeTestManifestWithOrgSlug writes a minimal manifest JSON using an explicit
// source org slug (to test normalization with non-canonical prefixes such as
// "github/old-org").
func writeTestManifestWithOrgSlug(t *testing.T, path string, orgSlug string, slugs []string) {
	t.Helper()
	orgName := orgSlug
	if idx := strings.LastIndex(orgSlug, "/"); idx >= 0 {
		orgName = orgSlug[idx+1:]
	}
	projects := make([]map[string]interface{}, 0, len(slugs))
	for _, s := range slugs {
		projects = append(projects, map[string]interface{}{
			"slug":                  s,
			"name":                  s[strings.LastIndex(s, "/")+1:],
			"vcs":                   map[string]interface{}{},
			"environment_variables": []interface{}{},
		})
	}
	m := map[string]interface{}{
		"schema_version": "1",
		"source": map[string]interface{}{
			"host": "https://circleci.com",
			"org": map[string]interface{}{
				"slug": orgSlug,
				"name": orgName,
			},
		},
		"contexts": []interface{}{},
		"projects": projects,
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
}

// TestMappingGenerateCommand_DefaultOutputPath verifies that when -o is omitted
// the mapping file is written as "mapping.json" next to the manifest.
func TestMappingGenerateCommand_DefaultOutputPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/v2/organization/"):
			respondJSONHelper(w, http.StatusOK, map[string]interface{}{
				"id": "dest-org-uuid", "name": "new-org",
				"slug": "gh/new-org", "vcs_type": "github",
			})
		case r.URL.Path == "/api/private/project":
			respondJSONHelper(w, http.StatusOK, map[string]interface{}{
				"items":           []interface{}{},
				"next_page_token": "",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	tmpDir := t.TempDir()
	manifestPath := filepath.Join(tmpDir, "manifest.json")
	writeTestManifest(t, manifestPath, []string{"gh/old-org/web"})

	root := cmd.MakeCommands()
	root.PersistentFlags().Set("host", srv.URL) //nolint:errcheck
	var outBuf, errBuf strings.Builder
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	root.SetArgs([]string{
		"mapping", "generate",
		"--manifest", manifestPath,
		"--dest-org", "gh/new-org",
		"--dest-token", "fake-token",
		// no -o flag
	})

	if err := root.Execute(); err != nil {
		t.Fatalf("mapping generate: %v\nstdout: %s\nstderr: %s", err, outBuf.String(), errBuf.String())
	}

	// Default path = mapping.json next to the manifest.
	defaultOut := filepath.Join(tmpDir, "mapping.json")
	if _, statErr := os.Stat(defaultOut); statErr != nil {
		t.Errorf("expected default output file %s to exist: %v", defaultOut, statErr)
	}
}

// ---------------------------------------------------------------------------
// deriveDestSlug unit tests (Issue #272)
// ---------------------------------------------------------------------------

// deriveDestSlugHelper mirrors the logic of cmd.deriveDestSlug for use in
// package cmd_test without requiring the function to be exported.
func deriveDestSlugHelper(srcSlug, normalizedDestOrgSlug string) (string, bool) {
	provider := ""
	switch {
	case strings.HasPrefix(normalizedDestOrgSlug, "gh/"):
		provider = "gh"
	case strings.HasPrefix(normalizedDestOrgSlug, "bb/"):
		provider = "bb"
	default:
		return "", false
	}
	destOrgName := strings.TrimPrefix(normalizedDestOrgSlug, provider+"/")
	if destOrgName == "" || strings.Contains(destOrgName, "/") {
		return "", false
	}
	repo := repoNameHelper(srcSlug)
	if repo == "" || repo == srcSlug {
		return "", false
	}
	return provider + "/" + destOrgName + "/" + repo, true
}

// TestDeriveDestSlug_GHProvider verifies that gh/ source slugs produce derived
// dest slugs with the correct provider, dest org, and repo.
func TestDeriveDestSlug_GHProvider(t *testing.T) {
	cases := []struct {
		srcSlug     string
		destOrgSlug string
		wantDst     string
		wantOK      bool
	}{
		{"gh/old-org/web", "gh/new-org", "gh/new-org/web", true},
		{"gh/old-org/api", "gh/new-org", "gh/new-org/api", true},
		{"github/old-org/web", "gh/new-org", "gh/new-org/web", true}, // non-normalized src
	}
	for _, tc := range cases {
		got, ok := deriveDestSlugHelper(tc.srcSlug, tc.destOrgSlug)
		if ok != tc.wantOK || got != tc.wantDst {
			t.Errorf("deriveDestSlug(%q, %q) = (%q, %v); want (%q, %v)",
				tc.srcSlug, tc.destOrgSlug, got, ok, tc.wantDst, tc.wantOK)
		}
	}
}

// TestDeriveDestSlug_BBProvider verifies that bb/ dest org slugs work.
func TestDeriveDestSlug_BBProvider(t *testing.T) {
	got, ok := deriveDestSlugHelper("bb/old-org/service", "bb/new-org")
	if !ok || got != "bb/new-org/service" {
		t.Errorf("deriveDestSlug bb: got (%q, %v); want (bb/new-org/service, true)", got, ok)
	}
}

// TestDeriveDestSlug_CircleCIProviderNotDerived verifies that circleci/ dest
// org slugs return ("", false) because those slugs contain UUIDs.
func TestDeriveDestSlug_CircleCIProviderNotDerived(t *testing.T) {
	got, ok := deriveDestSlugHelper("gh/old-org/web", "circleci/aaaabbbb-cccc-dddd-eeee-ffffgggghhhh")
	if ok || got != "" {
		t.Errorf("deriveDestSlug circleci/: got (%q, %v); want (\"\", false)", got, ok)
	}
}

// TestDeriveDestSlug_NoSlashInSrc verifies that a srcSlug with no slash
// returns ("", false) because there is no extractable repo name.
func TestDeriveDestSlug_NoSlashInSrc(t *testing.T) {
	got, ok := deriveDestSlugHelper("somerepo", "gh/new-org")
	if ok || got != "" {
		t.Errorf("deriveDestSlug no-slash src: got (%q, %v); want (\"\", false)", got, ok)
	}
}

// TestDeriveDestSlug_MalformedDestOrgSlug verifies that a dest org slug with
// multiple slashes (invalid form) returns ("", false).
func TestDeriveDestSlug_MalformedDestOrgSlug(t *testing.T) {
	// "gh/new-org/sub" is not a valid org slug (extra segment) — should not derive.
	got, ok := deriveDestSlugHelper("gh/old-org/web", "gh/new-org/sub")
	if ok || got != "" {
		t.Errorf("deriveDestSlug malformed dest org: got (%q, %v); want (\"\", false)", got, ok)
	}
}

// TestDeriveDestSlug_EmptyDestOrgName verifies that "gh/" (empty org name)
// returns ("", false).
func TestDeriveDestSlug_EmptyDestOrgName(t *testing.T) {
	got, ok := deriveDestSlugHelper("gh/old-org/web", "gh/")
	if ok || got != "" {
		t.Errorf("deriveDestSlug empty org name: got (%q, %v); want (\"\", false)", got, ok)
	}
}

// TestMappingGenerateCommand_DerivedSlugs verifies that when a source project
// has no onboarded dest project, generate writes the derived slug to
// mapping.json and reports it in the "derived" section.
func TestMappingGenerateCommand_DerivedSlugs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/v2/organization/"):
			respondJSONHelper(w, http.StatusOK, map[string]interface{}{
				"id": "dest-org-uuid", "name": "new-org",
				"slug": "gh/new-org", "vcs_type": "github",
			})
		case r.URL.Path == "/api/private/project":
			// Only "web" is onboarded; "worker" is not yet onboarded.
			respondJSONHelper(w, http.StatusOK, map[string]interface{}{
				"items": []map[string]interface{}{
					{"id": "p1", "slug": "gh/new-org/web", "name": "web"},
				},
				"next_page_token": "",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	tmpDir := t.TempDir()
	// Source org has "web" (will match) and "worker" (will be derived).
	manifestPath := filepath.Join(tmpDir, "manifest.json")
	writeTestManifest(t, manifestPath, []string{"gh/old-org/web", "gh/old-org/worker"})
	outputPath := filepath.Join(tmpDir, "mapping.json")

	root := cmd.MakeCommands()
	root.PersistentFlags().Set("host", srv.URL) //nolint:errcheck
	var outBuf, errBuf strings.Builder
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	root.SetArgs([]string{
		"mapping", "generate",
		"--manifest", manifestPath,
		"--dest-org", "gh/new-org",
		"-o", outputPath,
		"--dest-token", "fake-test-token",
	})

	if err := root.Execute(); err != nil {
		t.Fatalf("mapping generate: %v\nstdout: %s\nstderr: %s", err, outBuf.String(), errBuf.String())
	}

	// Verify the mapping file.
	mp, err := manifest.LoadMapping(outputPath)
	if err != nil {
		t.Fatalf("LoadMapping: %v", err)
	}
	// "web" is onboarded → matched directly.
	if mp.Projects["gh/old-org/web"] != "gh/new-org/web" {
		t.Errorf("matched entry: mapping[gh/old-org/web] = %q; want gh/new-org/web", mp.Projects["gh/old-org/web"])
	}
	// "worker" is not onboarded → derived.
	if mp.Projects["gh/old-org/worker"] != "gh/new-org/worker" {
		t.Errorf("derived entry: mapping[gh/old-org/worker] = %q; want gh/new-org/worker", mp.Projects["gh/old-org/worker"])
	}

	// Verify the stdout report.
	out := outBuf.String()
	if !strings.Contains(out, "derived") {
		t.Errorf("stdout missing 'derived' section:\n%s", out)
	}
	if !strings.Contains(out, "gh/old-org/worker") {
		t.Errorf("stdout missing derived slug gh/old-org/worker:\n%s", out)
	}
	// "Unmatched source projects" section should be empty (count 0).
	if !strings.Contains(out, "Unmatched source projects (0)") {
		t.Errorf("expected zero unmatched source projects in output:\n%s", out)
	}
	// worker should NOT appear in "Unmatched source" section.
	unmatchedIdx := strings.Index(out, "Unmatched source")
	derivedIdx := strings.Index(out, "derived")
	if unmatchedIdx >= 0 && derivedIdx >= 0 {
		// worker entry should be in derived section, i.e. appear before Unmatched section.
		workerIdx := strings.Index(out, "gh/old-org/worker")
		if workerIdx > unmatchedIdx {
			t.Errorf("gh/old-org/worker appears after 'Unmatched source' — should be in 'derived' section:\n%s", out)
		}
	}
}

// TestMappingGenerateCommand_CircleCIOrgNoDerivation verifies that when the
// dest org is a circleci/ (App/standalone) org, unmatched source projects
// are NOT derived and remain in the "Unmatched source" section.
func TestMappingGenerateCommand_CircleCIOrgNoDerivation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/v2/organization/"):
			respondJSONHelper(w, http.StatusOK, map[string]interface{}{
				"id":   "aaaabbbb-cccc-dddd-eeee-ffffgggghhhh",
				"name": "app-org",
				"slug": "circleci/aaaabbbb-cccc-dddd-eeee-ffffgggghhhh",
			})
		case r.URL.Path == "/api/private/project":
			// No onboarded projects.
			respondJSONHelper(w, http.StatusOK, map[string]interface{}{
				"items":           []interface{}{},
				"next_page_token": "",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	tmpDir := t.TempDir()
	manifestPath := filepath.Join(tmpDir, "manifest.json")
	writeTestManifest(t, manifestPath, []string{"gh/old-org/web"})
	outputPath := filepath.Join(tmpDir, "mapping.json")

	root := cmd.MakeCommands()
	root.PersistentFlags().Set("host", srv.URL) //nolint:errcheck
	var outBuf, errBuf strings.Builder
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	root.SetArgs([]string{
		"mapping", "generate",
		"--manifest", manifestPath,
		"--dest-org", "circleci/aaaabbbb-cccc-dddd-eeee-ffffgggghhhh",
		"-o", outputPath,
		"--dest-token", "fake-test-token",
	})

	if err := root.Execute(); err != nil {
		t.Fatalf("mapping generate: %v\nstdout: %s\nstderr: %s", err, outBuf.String(), errBuf.String())
	}

	mp, err := manifest.LoadMapping(outputPath)
	if err != nil {
		t.Fatalf("LoadMapping: %v", err)
	}
	// No derivation for circleci/ org: mapping should be empty.
	if len(mp.Projects) != 0 {
		t.Errorf("expected empty projects mapping for circleci/ dest org, got: %v", mp.Projects)
	}

	out := outBuf.String()
	// The project should appear in Unmatched source, not derived.
	if !strings.Contains(out, "Unmatched source") {
		t.Errorf("stdout missing 'Unmatched source' section:\n%s", out)
	}
	if !strings.Contains(out, "gh/old-org/web") {
		t.Errorf("stdout missing unmatched project gh/old-org/web:\n%s", out)
	}
	// derived section should show (none).
	if !strings.Contains(out, "derived — dest project not yet onboarded) (0)") {
		t.Errorf("stdout should show 0 derived entries for circleci/ org:\n%s", out)
	}
}

// TestMappingGenerateCommand_ExitsZeroWithUnmatched confirms that the command
// exits 0 (returns nil error) even when some source projects are unmatched,
// because the report is the deliverable, not an error condition.
func TestMappingGenerateCommand_ExitsZeroWithUnmatched(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/v2/organization/"):
			respondJSONHelper(w, http.StatusOK, map[string]interface{}{
				"id": "dest-org-uuid", "name": "new-org",
				"slug": "gh/new-org", "vcs_type": "github",
			})
		case r.URL.Path == "/api/private/project":
			// Intentionally empty — all source projects will be unmatched.
			respondJSONHelper(w, http.StatusOK, map[string]interface{}{
				"items":           []interface{}{},
				"next_page_token": "",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	tmpDir := t.TempDir()
	manifestPath := filepath.Join(tmpDir, "manifest.json")
	writeTestManifest(t, manifestPath, []string{"gh/old-org/web", "gh/old-org/api"})
	outputPath := filepath.Join(tmpDir, "mapping.json")

	root := cmd.MakeCommands()
	root.PersistentFlags().Set("host", srv.URL) //nolint:errcheck
	var outBuf, errBuf strings.Builder
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	root.SetArgs([]string{
		"mapping", "generate",
		"--manifest", manifestPath,
		"--dest-org", "gh/new-org",
		"-o", outputPath,
		"--dest-token", "fake-token",
	})

	if err := root.Execute(); err != nil {
		t.Fatalf("expected exit 0 when projects are unmatched, got error: %v", err)
	}

	// The output should mention the unmatched projects.
	out := outBuf.String()
	if !strings.Contains(out, "gh/old-org/web") {
		t.Errorf("stdout missing unmatched project gh/old-org/web:\n%s", out)
	}
}
