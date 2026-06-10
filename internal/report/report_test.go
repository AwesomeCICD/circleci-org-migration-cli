package report_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CircleCI-Public/circleci-org-migration-cli/internal/manifest"
	"github.com/CircleCI-Public/circleci-org-migration-cli/internal/report"
)

// ---------------------------------------------------------------------------
// Test fixtures
// ---------------------------------------------------------------------------

func boolPtr(b bool) *bool { return &b }

// buildManifest returns a fully-populated manifest for testing.
func buildManifest() *manifest.Manifest {
	m := &manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		GeneratedAt:   "2024-06-01T12:00:00Z",
		ToolVersion:   "circleci-migrate/1.0.0",
		Source: manifest.Source{
			Host: "https://circleci.com",
			Org: manifest.Org{
				Slug:    "gh/acme",
				ID:      "org-uuid-999",
				Name:    "acme",
				VCSType: "github",
				Settings: &manifest.OrgSettings{
					RequireContextGroupRestriction: boolPtr(true),
				},
			},
		},
		Contexts: []manifest.Context{
			{
				Name:      "deploy-prod",
				SourceID:  "ctx-uuid-1",
				CreatedAt: "2024-01-01T00:00:00Z",
				EnvVars: []manifest.ContextEnvVar{
					{Name: "AWS_KEY", CreatedAt: "2024-01-01T00:00:00Z"},
					{Name: "DB_PASS", CreatedAt: "2024-01-02T00:00:00Z"},
				},
				Restrictions: []manifest.Restriction{
					{Type: "project", Value: "proj-uuid-1", Name: "web"},
					{Type: "group", Value: "group-uuid-1", Name: "security-team"},
				},
				SecurityGroups: []manifest.Group{
					{ID: "sg-1", Name: "eng-team", GroupType: "TEAM"},
				},
			},
			{
				Name:     "staging",
				SourceID: "ctx-uuid-2",
				EnvVars: []manifest.ContextEnvVar{
					{Name: "STAGING_KEY"},
				},
			},
		},
		Projects: []manifest.Project{
			{
				Slug:     "gh/acme/web",
				SourceID: "proj-uuid-1",
				Name:     "web",
				VCS: manifest.ProjectVCS{
					Provider:      "GitHub",
					URL:           "https://github.com/acme/web",
					DefaultBranch: "main",
				},
				Settings: &manifest.AdvancedSettings{
					AutocancelBuilds: boolPtr(true),
					SetGitHubStatus:  boolPtr(false),
					BuildForkPRs:     boolPtr(true),
				},
				EnvVars: []manifest.ProjectEnvVar{
					{Name: "API_KEY", MaskedValue: "xxxx1234"},
					{Name: "SECRET", MaskedValue: "xxxx5678"},
				},
				CheckoutKeys: []manifest.CheckoutKey{
					{Type: "deploy-key", Fingerprint: "aa:bb:cc", Preferred: true},
				},
				Webhooks: []manifest.Webhook{
					{Name: "notify", URL: "https://hooks.example.com", Events: []string{"workflow-completed"}},
				},
				Schedules: []manifest.Schedule{
					{Name: "nightly", Description: "nightly build"},
				},
			},
			{
				Slug: "gh/acme/api",
				Name: "api",
				EnvVars: []manifest.ProjectEnvVar{
					{Name: "DB_URL"},
				},
			},
		},
		Warnings: []manifest.Warning{
			{Scope: "context:deploy-prod", Code: "context_values_excluded", Message: "2 context variable value(s) are not in the manifest"},
			{Scope: "context:deploy-prod", Code: "group_restriction_manual", Message: "group restriction must be recreated manually"},
			{Scope: "project:gh/acme/web", Code: "project_values_excluded", Message: "2 project variable value(s) are masked"},
			{Scope: "projects", Code: "project_discovery_followed_only", Message: "projects were discovered from followed list"},
		},
	}
	return m
}

// ---------------------------------------------------------------------------
// Summary tests
// ---------------------------------------------------------------------------

func TestSummary_ContainsOrgName(t *testing.T) {
	m := buildManifest()
	s := report.Summary(m)
	if !strings.Contains(s, "acme") {
		t.Errorf("Summary does not contain org name 'acme': %q", s)
	}
}

func TestSummary_ContainsOrgSlug(t *testing.T) {
	m := buildManifest()
	s := report.Summary(m)
	if !strings.Contains(s, "gh/acme") {
		t.Errorf("Summary does not contain org slug 'gh/acme': %q", s)
	}
}

func TestSummary_ContainsContextCount(t *testing.T) {
	m := buildManifest()
	s := report.Summary(m)
	if !strings.Contains(s, "2") {
		t.Errorf("Summary does not contain context count '2': %q", s)
	}
}

func TestSummary_ContainsProjectCount(t *testing.T) {
	m := buildManifest()
	s := report.Summary(m)
	if !strings.Contains(s, "2") {
		t.Errorf("Summary does not contain project count '2': %q", s)
	}
}

func TestSummary_ContainsWarningCodeGroupings(t *testing.T) {
	m := buildManifest()
	s := report.Summary(m)
	// Should list warning codes
	for _, code := range []string{"context_values_excluded", "group_restriction_manual", "project_values_excluded"} {
		if !strings.Contains(s, code) {
			t.Errorf("Summary does not contain warning code %q: %q", code, s)
		}
	}
}

func TestSummary_ContainsWarningCount(t *testing.T) {
	m := buildManifest()
	s := report.Summary(m)
	// 4 warnings
	if !strings.Contains(s, "4") {
		t.Errorf("Summary does not contain warning count '4': %q", s)
	}
}

func TestSummary_ContainsVariableCounts(t *testing.T) {
	m := buildManifest()
	s := report.Summary(m)
	// 3 total context vars (2+1), 3 total project vars (2+1)
	if !strings.Contains(s, "3") {
		t.Errorf("Summary does not contain variable count '3': %q", s)
	}
}

func TestSummary_EmptyOrg_ShowsDash(t *testing.T) {
	m := &manifest.Manifest{
		Source: manifest.Source{},
	}
	s := report.Summary(m)
	// orDash uses em-dash for empty
	if !strings.Contains(s, "—") {
		t.Errorf("Summary with empty org should show em-dash, got: %q", s)
	}
}

func TestSummary_NoWarnings(t *testing.T) {
	m := &manifest.Manifest{
		Source: manifest.Source{
			Org: manifest.Org{Name: "testorg", Slug: "gh/testorg"},
		},
	}
	s := report.Summary(m)
	if !strings.Contains(s, "Warnings   : 0") {
		t.Errorf("Summary with no warnings should show 0: %q", s)
	}
}

// ---------------------------------------------------------------------------
// Markdown tests
// ---------------------------------------------------------------------------

func TestMarkdown_ContainsSectionHeaders(t *testing.T) {
	m := buildManifest()
	md := report.Markdown(m)

	for _, header := range []string{
		"# CircleCI migration audit",
		"## Summary",
		"## Contexts",
		"## Projects",
		"## Warnings",
	} {
		if !strings.Contains(md, header) {
			t.Errorf("Markdown missing header %q", header)
		}
	}
}

func TestMarkdown_SSOSurfaced(t *testing.T) {
	m := &manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		Source: manifest.Source{
			Org: manifest.Org{
				Slug: "gh/acme",
				Name: "acme",
				Settings: &manifest.OrgSettings{
					SSO: &manifest.SSOSettings{Enforced: true, Realm: "acme-saml"},
				},
			},
		},
	}
	md := report.Markdown(m)
	for _, want := range []string{"SSO (SAML)", "acme-saml", "recreated manually"} {
		if !strings.Contains(md, want) {
			t.Errorf("Markdown missing %q", want)
		}
	}
}

func TestMarkdown_ContainsSummaryTable(t *testing.T) {
	m := buildManifest()
	md := report.Markdown(m)

	// The summary table header
	if !strings.Contains(md, "| Resource | Count |") {
		t.Errorf("Markdown missing summary table header: %q", md[:min(200, len(md))])
	}
	// Contexts row
	if !strings.Contains(md, "| Contexts |") {
		t.Errorf("Markdown missing Contexts row in summary table")
	}
	// Projects row
	if !strings.Contains(md, "| Projects |") {
		t.Errorf("Markdown missing Projects row in summary table")
	}
}

func TestMarkdown_ContainsContextEntry(t *testing.T) {
	m := buildManifest()
	md := report.Markdown(m)

	if !strings.Contains(md, "### `deploy-prod`") {
		t.Errorf("Markdown missing context entry for deploy-prod")
	}
	if !strings.Contains(md, "### `staging`") {
		t.Errorf("Markdown missing context entry for staging")
	}
}

func TestMarkdown_ContainsContextEnvVarNames(t *testing.T) {
	m := buildManifest()
	md := report.Markdown(m)

	if !strings.Contains(md, "AWS_KEY") {
		t.Errorf("Markdown missing env var AWS_KEY")
	}
	if !strings.Contains(md, "DB_PASS") {
		t.Errorf("Markdown missing env var DB_PASS")
	}
}

func TestMarkdown_ContainsContextRestrictions(t *testing.T) {
	m := buildManifest()
	md := report.Markdown(m)

	// Restrictions section for deploy-prod
	if !strings.Contains(md, "Restrictions:") {
		t.Errorf("Markdown missing Restrictions section")
	}
	if !strings.Contains(md, "project") {
		t.Errorf("Markdown missing project restriction type")
	}
	if !strings.Contains(md, "group") {
		t.Errorf("Markdown missing group restriction type")
	}
}

func TestMarkdown_ContainsSecurityGroups(t *testing.T) {
	m := buildManifest()
	md := report.Markdown(m)

	if !strings.Contains(md, "Security groups") {
		t.Errorf("Markdown missing security groups section")
	}
	if !strings.Contains(md, "eng-team") {
		t.Errorf("Markdown missing security group name eng-team")
	}
}

func TestMarkdown_ContainsProjectEntry(t *testing.T) {
	m := buildManifest()
	md := report.Markdown(m)

	if !strings.Contains(md, "### `gh/acme/web`") {
		t.Errorf("Markdown missing project entry for gh/acme/web")
	}
	if !strings.Contains(md, "### `gh/acme/api`") {
		t.Errorf("Markdown missing project entry for gh/acme/api")
	}
}

func TestMarkdown_ContainsProjectDefaultBranch(t *testing.T) {
	m := buildManifest()
	md := report.Markdown(m)

	if !strings.Contains(md, "Default branch") {
		t.Errorf("Markdown missing Default branch line")
	}
	if !strings.Contains(md, "main") {
		t.Errorf("Markdown missing branch name 'main'")
	}
}

func TestMarkdown_ContainsProjectSettings(t *testing.T) {
	m := buildManifest()
	md := report.Markdown(m)

	if !strings.Contains(md, "Advanced settings") {
		t.Errorf("Markdown missing Advanced settings line")
	}
	if !strings.Contains(md, "autocancel_builds=true") {
		t.Errorf("Markdown missing autocancel_builds=true")
	}
	if !strings.Contains(md, "set_github_status=false") {
		t.Errorf("Markdown missing set_github_status=false")
	}
}

func TestMarkdown_ContainsProjectExtras(t *testing.T) {
	m := buildManifest()
	md := report.Markdown(m)

	if !strings.Contains(md, "Checkout keys: 1") {
		t.Errorf("Markdown missing checkout keys count")
	}
	if !strings.Contains(md, "Webhooks: 1") {
		t.Errorf("Markdown missing webhooks count")
	}
	if !strings.Contains(md, "Schedules: 1") {
		t.Errorf("Markdown missing schedules count")
	}
}

func TestMarkdown_ContainsWarningsTable(t *testing.T) {
	m := buildManifest()
	md := report.Markdown(m)

	if !strings.Contains(md, "| Scope | Code | Detail |") {
		t.Errorf("Markdown missing warnings table header")
	}
	if !strings.Contains(md, "context_values_excluded") {
		t.Errorf("Markdown missing context_values_excluded warning row")
	}
	if !strings.Contains(md, "project_values_excluded") {
		t.Errorf("Markdown missing project_values_excluded warning row")
	}
}

func TestMarkdown_PipeEscaping_InWarningMessage(t *testing.T) {
	m := &manifest.Manifest{
		Source: manifest.Source{
			Org: manifest.Org{Name: "testorg", Slug: "gh/testorg"},
		},
		Warnings: []manifest.Warning{
			{
				Scope:   "org",
				Code:    "test_code",
				Message: "value is foo|bar in the pipeline",
			},
		},
	}
	md := report.Markdown(m)

	// Pipes in warning message should be escaped
	if strings.Contains(md, "foo|bar") {
		t.Error("Markdown should escape pipe characters in warning messages; found unescaped 'foo|bar'")
	}
	if !strings.Contains(md, `foo\|bar`) {
		t.Errorf("Markdown should contain 'foo\\|bar' (escaped pipe): %q", md)
	}
}

func TestMarkdown_NoContexts_ShowsNone(t *testing.T) {
	m := &manifest.Manifest{
		Source: manifest.Source{
			Org: manifest.Org{Name: "testorg", Slug: "gh/testorg"},
		},
	}
	md := report.Markdown(m)

	if !strings.Contains(md, "_None._") {
		t.Errorf("Markdown should show _None._ when no contexts, got: %q", md)
	}
}

func TestMarkdown_OrgID_Included(t *testing.T) {
	m := buildManifest()
	md := report.Markdown(m)

	if !strings.Contains(md, "org-uuid-999") {
		t.Errorf("Markdown missing org ID 'org-uuid-999'")
	}
}

func TestMarkdown_RequireContextGroupRestriction_Included(t *testing.T) {
	m := buildManifest()
	md := report.Markdown(m)

	if !strings.Contains(md, "Require context group restriction") {
		t.Errorf("Markdown missing require context group restriction line")
	}
}

func TestMarkdown_NoWarnings_ShowsNone(t *testing.T) {
	m := &manifest.Manifest{
		Source: manifest.Source{
			Org: manifest.Org{Name: "testorg", Slug: "gh/testorg"},
		},
	}
	md := report.Markdown(m)

	// Should show none for warnings
	if !strings.Contains(md, "_None._") {
		t.Errorf("Markdown should show _None._ when no warnings, got: %q", md)
	}
}

// ---------------------------------------------------------------------------
// Cutover runbook tests
// ---------------------------------------------------------------------------

func TestMarkdown_ContainsRunbookSectionHeaders(t *testing.T) {
	m := buildManifest()
	md := report.Markdown(m)

	for _, header := range []string{
		"## Cutover runbook",
		"### 1. Recommended cutover order",
		"### 2. Automated by `sync --apply`",
		"### 3. Manual steps required",
		"### 4. Does not transfer / data loss",
		"### 5. Update external pins",
	} {
		if !strings.Contains(md, header) {
			t.Errorf("Markdown missing runbook header %q", header)
		}
	}
}

func TestMarkdown_RunbookOrderMentionsPausedCreation(t *testing.T) {
	m := buildManifest()
	md := report.Markdown(m)
	for _, want := range []string{"sync --apply", "paused", "Capture secret values", "Rotate the captured secrets"} {
		if !strings.Contains(md, want) {
			t.Errorf("Markdown runbook order missing %q", want)
		}
	}
}

func TestMarkdown_ManualStepsBaselineAlwaysPresent(t *testing.T) {
	// Minimal manifest with no org settings: baseline manual items must still appear.
	m := &manifest.Manifest{
		Source: manifest.Source{Org: manifest.Org{Name: "o", Slug: "gh/o"}},
	}
	md := report.Markdown(m)
	for _, want := range []string{"secret values", "Checkout & SSH keys", "Webhook signing secrets"} {
		if !strings.Contains(md, want) {
			t.Errorf("Markdown manual steps missing baseline item %q", want)
		}
	}
}

func TestMarkdown_ManualSteps_ContextValuesNotePullsWarning(t *testing.T) {
	m := buildManifest() // has context_values_excluded warning
	md := report.Markdown(m)
	if !strings.Contains(md, "export note:") {
		t.Errorf("Markdown manual steps should quote the recorded warning via 'export note:'")
	}
}

func TestMarkdown_ManualSteps_SSOOnlyWhenPresent(t *testing.T) {
	// Without SSO.
	plain := report.Markdown(&manifest.Manifest{
		Source: manifest.Source{Org: manifest.Org{Name: "o", Slug: "gh/o"}},
	})
	if strings.Contains(plain, "**SSO (SAML)**") {
		t.Errorf("Markdown should not list an SSO manual step when no SSO is configured")
	}

	// With SSO.
	withSSO := report.Markdown(&manifest.Manifest{
		Source: manifest.Source{Org: manifest.Org{
			Name: "o", Slug: "gh/o",
			Settings: &manifest.OrgSettings{SSO: &manifest.SSOSettings{Enforced: true, Realm: "r"}},
		}},
	})
	if !strings.Contains(withSSO, "**SSO (SAML)**") {
		t.Errorf("Markdown should list an SSO manual step when SSO is configured")
	}
}

func TestMarkdown_ManualSteps_AuditLogOnlyWhenPresent(t *testing.T) {
	plain := report.Markdown(&manifest.Manifest{
		Source: manifest.Source{Org: manifest.Org{Name: "o", Slug: "gh/o"}},
	})
	if strings.Contains(plain, "Audit-log streaming") {
		t.Errorf("Markdown should not mention audit-log streaming when no configs present")
	}

	withAudit := report.Markdown(&manifest.Manifest{
		Source: manifest.Source{Org: manifest.Org{
			Name: "o", Slug: "gh/o",
			Settings: &manifest.OrgSettings{
				AuditLogConfigs: []manifest.AuditLogConfig{{ID: "a1"}},
			},
		}},
	})
	if !strings.Contains(withAudit, "Audit-log streaming") {
		t.Errorf("Markdown should mention audit-log streaming when configs present")
	}
}

func TestMarkdown_ManualSteps_OTelAndContactsAndPipelineDefs(t *testing.T) {
	m := &manifest.Manifest{
		Source: manifest.Source{Org: manifest.Org{
			Name: "o", Slug: "circleci/abc",
			Settings: &manifest.OrgSettings{
				OTelExporters: []manifest.OTelExporter{{Endpoint: "https://otel"}},
				Contacts:      &manifest.OrgContacts{Primary: []string{"a@b.com"}},
			},
		}},
		Projects: []manifest.Project{
			{
				Slug: "circleci/abc/p1",
				PipelineDefinitions: []manifest.PipelineDefinition{
					{Name: "default"},
				},
			},
		},
	}
	md := report.Markdown(m)
	for _, want := range []string{
		"OpenTelemetry exporter headers",
		"technical & security contacts",
		"Repository connections (App destinations)",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("Markdown manual steps missing %q", want)
		}
	}
}

func TestMarkdown_ManualSteps_DangerFlags(t *testing.T) {
	m := &manifest.Manifest{
		Source: manifest.Source{Org: manifest.Org{
			Name: "o", Slug: "gh/o",
			Settings: &manifest.OrgSettings{RequireContextGroupRestriction: boolPtr(true)},
		}},
		Projects: []manifest.Project{
			{Slug: "gh/o/p", Settings: &manifest.AdvancedSettings{DropAllBuildRequests: boolPtr(true)}},
		},
	}
	md := report.Markdown(m)
	for _, want := range []string{"require_context_group_restriction", "drop_all_build_requests", "danger flag"} {
		if !strings.Contains(md, want) {
			t.Errorf("Markdown manual steps missing danger flag %q", want)
		}
	}
}

func TestMarkdown_DataLoss_PresentAndUUIDsMentioned(t *testing.T) {
	m := buildManifest()
	md := report.Markdown(m)
	if !strings.Contains(md, "### 4. Does not transfer / data loss") {
		t.Errorf("Markdown missing data-loss section")
	}
	for _, want := range []string{"UUID", "rotate", "pr_only_branch_overrides"} {
		if !strings.Contains(md, want) {
			t.Errorf("Markdown data-loss section missing %q", want)
		}
	}
}

func TestMarkdown_DataLoss_OAuthSourceCallsOutCrossType(t *testing.T) {
	// gh/ slug → OAuth source.
	oauth := report.Markdown(buildManifest())
	if !strings.Contains(oauth, "OAuth → App") {
		t.Errorf("OAuth-source data-loss should call out OAuth → App explicitly")
	}
	// circleci/ slug → App source.
	app := report.Markdown(&manifest.Manifest{
		Source: manifest.Source{Org: manifest.Org{Name: "o", Slug: "circleci/abc"}},
	})
	if !strings.Contains(app, "Cross-type moves lose settings") {
		t.Errorf("App-source data-loss should use the generic cross-type wording")
	}
}

func TestMarkdown_ExternalPinsSection(t *testing.T) {
	m := buildManifest()
	md := report.Markdown(m)
	for _, want := range []string{"### 5. Update external pins", "Backstage", "Slack", "status-check", "badges"} {
		if !strings.Contains(md, want) {
			t.Errorf("Markdown external-pins section missing %q", want)
		}
	}
}

// ---------------------------------------------------------------------------
// SaveMarkdown tests
// ---------------------------------------------------------------------------

func TestSaveMarkdown_WritesFileMatchingMarkdown(t *testing.T) {
	m := buildManifest()
	dir := t.TempDir()
	path := filepath.Join(dir, "report.md")

	if err := report.SaveMarkdown(m, path); err != nil {
		t.Fatalf("SaveMarkdown error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile error: %v", err)
	}

	want := report.Markdown(m)
	if string(data) != want {
		t.Errorf("file contents do not match Markdown(m):\ngot:  %q\nwant: %q", string(data[:min(200, len(data))]), want[:min(200, len(want))])
	}
}

func TestSaveMarkdown_InvalidPath_ReturnsError(t *testing.T) {
	m := buildManifest()
	err := report.SaveMarkdown(m, "/nonexistent/deep/path/report.md")
	if err == nil {
		t.Error("expected error for invalid path, got nil")
	}
}

func TestSaveMarkdown_FileHasExpectedContent(t *testing.T) {
	m := buildManifest()
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.md")

	if err := report.SaveMarkdown(m, path); err != nil {
		t.Fatalf("SaveMarkdown error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile error: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "# CircleCI migration audit") {
		t.Error("saved file does not contain expected title")
	}
	if !strings.Contains(content, "acme") {
		t.Error("saved file does not contain org name")
	}
}

// min returns the smaller of a and b.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
