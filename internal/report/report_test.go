package report_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/report"
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

	// The header now shows: "Name (`slug`)" when a human name is available,
	// or "`slug`" when no name is set.  Both projects in buildManifest() have
	// names ("web" and "api"), so expect the friendly-name format.
	if !strings.Contains(md, "web (`gh/acme/web`)") {
		t.Errorf("Markdown missing project entry for gh/acme/web; got:\n%s", md)
	}
	if !strings.Contains(md, "api (`gh/acme/api`)") {
		t.Errorf("Markdown missing project entry for gh/acme/api; got:\n%s", md)
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
	for _, want := range []string{"secret values", "Checkout & SSH keys"} {
		if !strings.Contains(md, want) {
			t.Errorf("Markdown manual steps missing baseline item %q", want)
		}
	}
}

func TestMarkdown_ManualSteps_WebhookSigningSecretOnlyWhenWebhooksPresent(t *testing.T) {
	// Without webhooks: no signing secret warning.
	plain := report.Markdown(&manifest.Manifest{
		Source: manifest.Source{Org: manifest.Org{Name: "o", Slug: "gh/o"}},
	})
	if strings.Contains(plain, "Webhook signing secrets") {
		t.Errorf("Markdown should not mention webhook signing secrets when no webhooks captured")
	}

	// With webhooks: must warn.
	withWebhooks := report.Markdown(&manifest.Manifest{
		Source: manifest.Source{Org: manifest.Org{Name: "o", Slug: "gh/o"}},
		Projects: []manifest.Project{
			{
				Slug: "gh/o/web",
				Webhooks: []manifest.Webhook{
					{Name: "notify", URL: "https://hooks.example.com"},
				},
			},
		},
	})
	if !strings.Contains(withWebhooks, "Webhook signing secrets") {
		t.Errorf("Markdown should mention webhook signing secrets when webhooks are captured")
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

func TestMarkdown_ManualSteps_GroupsOnlyWhenPresent(t *testing.T) {
	// Without groups: no CircleCI groups manual step.
	plain := report.Markdown(&manifest.Manifest{
		Source: manifest.Source{Org: manifest.Org{Name: "o", Slug: "gh/o"}},
	})
	if strings.Contains(plain, "**CircleCI groups**") {
		t.Errorf("Markdown should not list a CircleCI groups manual step when no groups captured")
	}

	// With groups: step appears, with count and names.
	withGroups := report.Markdown(&manifest.Manifest{
		Source: manifest.Source{Org: manifest.Org{
			Name: "o", Slug: "gh/o",
			Settings: &manifest.OrgSettings{
				Groups: []manifest.OrgGroup{
					{ID: "g1", Name: "security-team"},
					{ID: "g2", Name: "platform"},
				},
			},
		}},
	})
	for _, want := range []string{
		"**CircleCI groups**",
		"recreate 2 CircleCI group(s)",
		"security-team",
		"platform",
		"managed via your IdP/SSO and is not migrated",
	} {
		if !strings.Contains(withGroups, want) {
			t.Errorf("Markdown groups manual step missing %q", want)
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

// TestMarkdown_OrgSettings_AllSections exercises every branch of
// writeOrgSettings with a fully-populated OrgSettings so the rendered report
// includes each org-level section.
func TestMarkdown_OrgSettings_AllSections(t *testing.T) {
	pid := "proj-uuid"
	m := &manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		Source: manifest.Source{
			Host: "https://circleci.com",
			Org: manifest.Org{
				Name: "Acme", Slug: "circleci/abc", ID: "org-uuid",
				Settings: &manifest.OrgSettings{
					FeatureFlags:                   map[string]bool{"allow_private_orbs": true, "drop_all_build_requests": false},
					StorageRetention:               &manifest.StorageRetentionControls{CacheDays: 7, WorkspaceDays: 7, ArtifactDays: 15},
					Budgets:                        &manifest.OrgBudgets{OrgBudget: &manifest.BudgetEntry{Credits: 1000000, EnforcementType: "warn"}, ProjectBudgets: []manifest.BudgetEntry{{Credits: 500, ProjectID: &pid}}},
					BlockUnregisteredUsers:         boolPtr(true),
					PolicyEnforcementEnabled:       boolPtr(true),
					RequireContextGroupRestriction: boolPtr(false),
					OIDCAudience:                   []string{"https://aud.example"},
					OIDCTTL:                        "1h",
					URLOrbAllowList:                []manifest.URLOrbAllowEntry{{Name: "ffdf", Prefix: "https://www.test.com/", Auth: "none"}},
					ConfigPolicies:                 map[string]string{"policy.rego": "package org"},
					Contacts:                       &manifest.OrgContacts{Primary: []string{"tech@example.com"}, Security: []string{"sec@example.com"}},
					OTelExporters:                  []manifest.OTelExporter{{Endpoint: "https://otel.example", Protocol: "http/protobuf"}},
					ReleaseTracker:                 &manifest.ReleaseTrackerSettings{InconclusiveReleaseTTL: "1h"},
					EnvironmentHierarchy:           &manifest.EnvironmentHierarchy{Name: "envs", Levels: []manifest.EnvHierarchyLevel{{Position: 1, IntegrationName: "orbs-dev"}, {Position: 2, IntegrationName: "orbs-prod"}}},
					AuditLogConfigs:                []manifest.AuditLogConfig{{ID: "al1"}},
					Orbs:                           []manifest.OrgOrb{{OrbName: "acme/util", LatestVersionNumber: "1.2.3", IsPrivate: true}},
				},
			},
		},
	}
	out := report.Markdown(m)
	for _, want := range []string{
		"## Org settings", "Feature flags (2)", "Enabled:", "Disabled:",
		"Storage retention", "Artifacts: 15", "Spend budgets", "1000000 credits",
		"Per-project budgets: 1", "Prevent unregistered-user spend: `true`",
		"OIDC custom claims", "1h", "URL-orb allow list (1)", "ffdf",
		"Config policies (1)", "Contacts", "tech@example.com", "sec@example.com",
		"OpenTelemetry exporters (1)", "Release tracker", "Environment hierarchy",
		"orbs-dev", "Audit-log streaming (1)", "Namespaces & orbs", "acme/util", "private",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered report missing %q", want)
		}
	}
}

// TestMarkdown_OrgSettings_NoneCaptured covers the nil-settings branch.
func TestMarkdown_OrgSettings_NoneCaptured(t *testing.T) {
	m := &manifest.Manifest{Source: manifest.Source{Org: manifest.Org{Name: "X", Slug: "gh/x"}}}
	out := report.Markdown(m)
	if !strings.Contains(out, "## Org settings") || !strings.Contains(out, "_None captured._") {
		t.Errorf("expected an Org settings section with '_None captured._'; got:\n%s", out[:min(len(out), 400)])
	}
}

// ---------------------------------------------------------------------------
// Repository line rendering (Bug #95)
// ---------------------------------------------------------------------------

// TestMarkdown_RepositoryLine_CircleCINativeProject verifies that a project
// with provider=="circleci" does NOT emit a scheme-less "//..." URL and instead
// shows the friendly "CircleCI-native project" label.
func TestMarkdown_RepositoryLine_CircleCINativeProject(t *testing.T) {
	m := &manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		Source: manifest.Source{
			Org: manifest.Org{Name: "myorg", Slug: "circleci/8ee930d4-abc"},
		},
		Projects: []manifest.Project{
			{
				Slug: "circleci/8ee930d4-abc/08ef317c-proj",
				Name: "Jam-Test",
				VCS: manifest.ProjectVCS{
					Provider: "circleci",
					URL:      "//circleci.com/8ee930d4-abc/08ef317c-proj",
				},
			},
		},
	}
	md := report.Markdown(m)

	// Must not contain the malformed scheme-less URL.
	if strings.Contains(md, "//circleci.com/") {
		t.Errorf("Markdown should not emit the scheme-less //circleci.com URL; got:\n%s", md)
	}
	// Must not contain a bare "//" anywhere in a Repository line.
	for _, line := range strings.Split(md, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "- Repository:") && strings.Contains(line, "//") {
			t.Errorf("Repository line contains scheme-less '//': %q", line)
		}
	}
	// Must show the friendly label instead.
	if !strings.Contains(md, "CircleCI-native project") {
		t.Errorf("Markdown should show 'CircleCI-native project' for circleci-provider projects; got:\n%s", md)
	}
}

// TestMarkdown_RepositoryLine_CircleCINativeURL verifies that a project whose
// URL starts with "//circleci.com/" (but has an empty/missing provider) is also
// treated as CircleCI-native.
func TestMarkdown_RepositoryLine_CircleCINativeURL(t *testing.T) {
	m := &manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		Source: manifest.Source{
			Org: manifest.Org{Name: "myorg", Slug: "circleci/abc"},
		},
		Projects: []manifest.Project{
			{
				Slug: "circleci/abc/proj",
				Name: "NativeProj",
				VCS: manifest.ProjectVCS{
					Provider: "",
					URL:      "//circleci.com/abc/proj-uuid",
				},
			},
		},
	}
	md := report.Markdown(m)

	if strings.Contains(md, "//circleci.com/") {
		t.Errorf("Markdown should not emit scheme-less //circleci.com URL; got:\n%s", md)
	}
	if !strings.Contains(md, "CircleCI-native project") {
		t.Errorf("Markdown should show 'CircleCI-native project' for //circleci.com URL; got:\n%s", md)
	}
}

// TestMarkdown_RepositoryLine_GitHubProject verifies that a real GitHub-backed
// project emits a proper https:// URL (never a scheme-less "//" URL).
func TestMarkdown_RepositoryLine_GitHubProject(t *testing.T) {
	m := &manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		Source: manifest.Source{
			Org: manifest.Org{Name: "acme", Slug: "gh/acme"},
		},
		Projects: []manifest.Project{
			{
				Slug: "gh/acme/web",
				Name: "web",
				VCS: manifest.ProjectVCS{
					Provider:      "GitHub",
					URL:           "https://github.com/acme/web",
					DefaultBranch: "main",
				},
			},
		},
	}
	md := report.Markdown(m)

	if !strings.Contains(md, "https://github.com/acme/web") {
		t.Errorf("Markdown should contain the full https GitHub URL; got:\n%s", md)
	}
	// Ensure there is no scheme-less // URL in any Repository line.
	for _, line := range strings.Split(md, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "- Repository:") && strings.Contains(line, "//") {
			// The only "//" should be inside "https://" — a bare "//" is a bug.
			if !strings.Contains(line, "https://") {
				t.Errorf("Repository line contains scheme-less '//': %q", line)
			}
		}
	}
}

// TestMarkdown_RepositoryLine_SchemelessGitHubURL verifies that a scheme-less
// "//github.com/..." URL (as sometimes returned by the API) is normalised to
// "https://github.com/..." in the rendered report.
func TestMarkdown_RepositoryLine_SchemelessGitHubURL(t *testing.T) {
	m := &manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		Source: manifest.Source{
			Org: manifest.Org{Name: "acme", Slug: "gh/acme"},
		},
		Projects: []manifest.Project{
			{
				Slug: "gh/acme/api",
				Name: "api",
				VCS: manifest.ProjectVCS{
					Provider: "GitHub",
					URL:      "//github.com/acme/api",
				},
			},
		},
	}
	md := report.Markdown(m)

	// The rendered URL must be prefixed with https://.
	if !strings.Contains(md, "https://github.com/acme/api") {
		t.Errorf("Markdown should normalise //github.com URL to https://; got:\n%s", md)
	}
	// And the raw scheme-less form must not appear as a Repository value.
	if strings.Contains(md, "- Repository: GitHub — `//github.com") {
		t.Errorf("Markdown must not emit scheme-less //github.com in Repository line; got:\n%s", md)
	}
}

// ---------------------------------------------------------------------------
// Issue #74: group-restriction manual note in report
// ---------------------------------------------------------------------------

// TestMarkdown_GroupRestriction_ManualNote_PresentWhenNonDefault verifies that
// the report includes the group-restriction manual note when at least one
// context has a non-default group restriction (type=="group", value!=orgID).
func TestMarkdown_GroupRestriction_ManualNote_PresentWhenNonDefault(t *testing.T) {
	const orgID = "acme-org-uuid"
	m := &manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		Source: manifest.Source{
			Org: manifest.Org{Name: "acme", Slug: "gh/acme", ID: orgID},
		},
		Contexts: []manifest.Context{
			{
				Name:     "secured-ctx",
				SourceID: "ctx-uuid-1",
				EnvVars:  []manifest.ContextEnvVar{{Name: "DEPLOY_KEY"}},
				Restrictions: []manifest.Restriction{
					// Default All-members group — should NOT trigger the note alone.
					{Type: "group", Value: orgID, Name: "All members"},
					// Non-default group restriction — SHOULD trigger the note.
					{Type: "group", Value: "engineering-team-uuid", Name: "engineering"},
				},
			},
		},
	}

	md := report.Markdown(m)

	for _, want := range []string{
		"Context group restrictions (manual)",
		"GitHub OAuth",
		"standalone",
		"Re-apply group restrictions manually",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("Markdown group-restriction note missing %q", want)
		}
	}
}

// TestMarkdown_GroupRestriction_ManualNote_AbsentWhenOnlyDefaultAllMembers
// verifies that the group-restriction note is NOT emitted when the only group
// restriction is the default "All members" (type=="group", value==orgID).
func TestMarkdown_GroupRestriction_ManualNote_AbsentWhenOnlyDefaultAllMembers(t *testing.T) {
	const orgID = "acme-org-uuid"
	m := &manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		Source: manifest.Source{
			Org: manifest.Org{Name: "acme", Slug: "gh/acme", ID: orgID},
		},
		Contexts: []manifest.Context{
			{
				Name:     "all-members-ctx",
				SourceID: "ctx-uuid-1",
				EnvVars:  []manifest.ContextEnvVar{{Name: "VAR"}},
				Restrictions: []manifest.Restriction{
					// Only the All-members default — should NOT produce the note.
					{Type: "group", Value: orgID, Name: "All members"},
				},
			},
		},
	}

	md := report.Markdown(m)

	if strings.Contains(md, "Context group restrictions (manual)") {
		t.Error("Markdown should NOT emit group-restriction note when only the default All-members restriction is present")
	}
}

// TestMarkdown_GroupRestriction_ManualNote_AbsentWhenNoGroupRestrictions
// verifies that the note is absent when no contexts have group restrictions.
func TestMarkdown_GroupRestriction_ManualNote_AbsentWhenNoGroupRestrictions(t *testing.T) {
	m := &manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		Source: manifest.Source{
			Org: manifest.Org{Name: "acme", Slug: "gh/acme", ID: "acme-org-uuid"},
		},
		Contexts: []manifest.Context{
			{
				Name:     "ctx",
				SourceID: "ctx-uuid-1",
				EnvVars:  []manifest.ContextEnvVar{{Name: "VAR"}},
				Restrictions: []manifest.Restriction{
					{Type: "project", Value: "proj-uuid"},
					{Type: "expression", Value: `project.slug == "gh/acme/web"`},
				},
			},
		},
	}

	md := report.Markdown(m)

	if strings.Contains(md, "Context group restrictions (manual)") {
		t.Error("Markdown should NOT emit group-restriction note when no group restrictions exist")
	}
}

// TestMarkdown_GroupRestriction_ManualNote_PullsWarning verifies that when a
// "group_restriction" warning is in the manifest, the note includes the
// export warning text via warningSuffix.
func TestMarkdown_GroupRestriction_ManualNote_PullsWarning(t *testing.T) {
	const orgID = "org-uuid"
	m := &manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		Source: manifest.Source{
			Org: manifest.Org{Name: "o", Slug: "gh/o", ID: orgID},
		},
		Contexts: []manifest.Context{
			{
				Name:     "ctx",
				SourceID: "ctx-uuid",
				EnvVars:  []manifest.ContextEnvVar{{Name: "VAR"}},
				Restrictions: []manifest.Restriction{
					{Type: "group", Value: "team-uuid", Name: "eng"},
				},
			},
		},
		Warnings: []manifest.Warning{
			{
				Scope:   "context:ctx",
				Code:    "group_restriction_manual",
				Message: "group restriction must be recreated manually",
			},
		},
	}

	md := report.Markdown(m)

	if !strings.Contains(md, "export note:") {
		t.Errorf("Markdown group-restriction note should include 'export note:'; got:\n%s", md)
	}
	if !strings.Contains(md, "group restriction must be recreated manually") {
		t.Errorf("Markdown should include the warning message text; got:\n%s", md)
	}
}

// ---------------------------------------------------------------------------
// Additional SSH key rendering
// ---------------------------------------------------------------------------

// TestMarkdown_SSHKeys_RenderedPerProject verifies that when a project has
// additional SSH keys, the report includes a section with hostname,
// fingerprint, and public-key preview for each key.
func TestMarkdown_SSHKeys_RenderedPerProject(t *testing.T) {
	m := &manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		Source: manifest.Source{
			Org: manifest.Org{Name: "acme", Slug: "gh/acme"},
		},
		Projects: []manifest.Project{
			{
				Slug: "gh/acme/web",
				Name: "web",
				SSHKeys: []manifest.ProjectSSHKey{
					{
						Hostname:    "github.com",
						Fingerprint: "Cv1BbZPFHMZzCPx+1CsJqO0kRBIlOm7DEqR/jPbHnBg=",
						PublicKey:   "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQC user@host",
					},
				},
			},
		},
	}

	md := report.Markdown(m)

	for _, want := range []string{
		"Additional SSH keys",
		"github.com",
		"Cv1BbZPFHMZzCPx+1CsJqO0kRBIlOm7DEqR/jPbHnBg=",
		"ssh-rsa AAAAB3NzaC1yc2EAAAA", // preview truncated
	} {
		if !strings.Contains(md, want) {
			t.Errorf("Markdown missing %q in SSH keys section; got:\n%s", want, md[:min(500, len(md))])
		}
	}
}

// TestMarkdown_SSHKeys_GlobalHostFallback verifies that an SSH key with an
// empty hostname is rendered as "(global)" in the report.
func TestMarkdown_SSHKeys_GlobalHostFallback(t *testing.T) {
	m := &manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		Source: manifest.Source{
			Org: manifest.Org{Name: "acme", Slug: "gh/acme"},
		},
		Projects: []manifest.Project{
			{
				Slug: "gh/acme/web",
				Name: "web",
				SSHKeys: []manifest.ProjectSSHKey{
					{Hostname: "", Fingerprint: "fp=", PublicKey: "ssh-rsa AAAA..."},
				},
			},
		},
	}

	md := report.Markdown(m)
	if !strings.Contains(md, "(global)") {
		t.Errorf("Markdown should render empty hostname as (global); got:\n%s", md[:min(500, len(md))])
	}
}

// TestMarkdown_SSHKeys_NoSectionWhenAbsent verifies that projects without
// additional SSH keys do not render the SSH keys section.
func TestMarkdown_SSHKeys_NoSectionWhenAbsent(t *testing.T) {
	m := &manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		Source: manifest.Source{
			Org: manifest.Org{Name: "acme", Slug: "gh/acme"},
		},
		Projects: []manifest.Project{
			{Slug: "gh/acme/web", Name: "web", SSHKeys: nil},
		},
	}

	md := report.Markdown(m)
	if strings.Contains(md, "Additional SSH keys") {
		t.Errorf("Markdown should NOT render SSH keys section when no keys present; got:\n%s", md[:min(500, len(md))])
	}
}

// TestMarkdown_ManualSteps_SSHKeysNoteWhenPresent verifies that the manual
// steps section includes a note about private SSH keys not being exported
// when at least one project has additional SSH keys.
func TestMarkdown_ManualSteps_SSHKeysNoteWhenPresent(t *testing.T) {
	m := &manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		Source: manifest.Source{
			Org: manifest.Org{Name: "acme", Slug: "gh/acme"},
		},
		Projects: []manifest.Project{
			{
				Slug: "gh/acme/web",
				Name: "web",
				SSHKeys: []manifest.ProjectSSHKey{
					{Hostname: "github.com", Fingerprint: "fp=", PublicKey: "ssh-rsa AAAA..."},
				},
			},
		},
		Warnings: []manifest.Warning{
			{
				Scope:   "project:gh/acme/web",
				Code:    "ssh_keys_private_excluded",
				Message: "1 additional SSH key(s) captured (public metadata only)",
			},
		},
	}

	md := report.Markdown(m)

	for _, want := range []string{
		"Additional SSH keys (private keys not exported)",
		"PRIVATE keys are never returned",
		"SSH-key extraction step",
		"export note:",
		"1 additional SSH key(s) captured (public metadata only)",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("Markdown manual-steps missing %q; got:\n%s", want, md[:min(800, len(md))])
		}
	}
}

// TestMarkdown_ManualSteps_SSHKeysNoteAbsentWhenNoKeys verifies that the
// additional-SSH-keys manual-steps note is NOT emitted when no project has
// additional SSH keys.
func TestMarkdown_ManualSteps_SSHKeysNoteAbsentWhenNoKeys(t *testing.T) {
	m := &manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		Source: manifest.Source{
			Org: manifest.Org{Name: "acme", Slug: "gh/acme"},
		},
		Projects: []manifest.Project{
			{Slug: "gh/acme/web", Name: "web"},
		},
	}

	md := report.Markdown(m)
	if strings.Contains(md, "Additional SSH keys (private keys not exported)") {
		t.Errorf("Manual-steps SSH-key note should be absent when no SSH keys in manifest; got note in:\n%s", md[:min(800, len(md))])
	}
}

// TestMarkdown_SSHKeys_PublicKeyPreviewTruncated verifies that a long public
// key is truncated to a preview in the rendered output.
func TestMarkdown_SSHKeys_PublicKeyPreviewTruncated(t *testing.T) {
	longKey := "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQC7e8+longkeygoeshere user@host.example.com"
	m := &manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		Source: manifest.Source{
			Org: manifest.Org{Name: "acme", Slug: "gh/acme"},
		},
		Projects: []manifest.Project{
			{
				Slug: "gh/acme/web",
				Name: "web",
				SSHKeys: []manifest.ProjectSSHKey{
					{Hostname: "test.host", Fingerprint: "fp=", PublicKey: longKey},
				},
			},
		},
	}

	md := report.Markdown(m)

	// The full key must NOT appear verbatim (it is longer than the preview limit).
	if strings.Contains(md, longKey) {
		t.Errorf("Full public key should not appear in report (should be truncated); found in:\n%s", md[:min(800, len(md))])
	}
	// The truncation marker must be present.
	if !strings.Contains(md, "...") {
		t.Errorf("Truncation marker '...' missing from SSH key preview in report")
	}
}

// ---------------------------------------------------------------------------
// Issue #130: new warnings in manual steps
// ---------------------------------------------------------------------------

// TestMarkdown_ManualSteps_RunnerTokensOnlyWhenPresent verifies that the
// runner agent-token manual step is only emitted when runner classes exist.
func TestMarkdown_ManualSteps_RunnerTokensOnlyWhenPresent(t *testing.T) {
	// Without runner classes: no runner agent-token step.
	plain := report.Markdown(&manifest.Manifest{
		Source: manifest.Source{Org: manifest.Org{Name: "o", Slug: "gh/o"}},
	})
	if strings.Contains(plain, "Runner agent tokens") {
		t.Errorf("Markdown should not mention runner agent tokens when no runner classes captured")
	}

	// With runner classes: step must appear.
	withRunners := report.Markdown(&manifest.Manifest{
		Source:          manifest.Source{Org: manifest.Org{Name: "o", Slug: "gh/o"}},
		RunnerNamespace: "my-ns",
		RunnerResourceClasses: []manifest.RunnerResourceClass{
			{Name: "my-ns/runner-a"},
			{Name: "my-ns/runner-b"},
		},
	})
	for _, want := range []string{"Runner agent tokens", "my-ns", "2 resource class"} {
		if !strings.Contains(withRunners, want) {
			t.Errorf("Markdown runner-agent-token step missing %q", want)
		}
	}
}

// TestMarkdown_ManualSteps_OrgOrbsOnlyWhenPresent verifies that the org-orbs
// republish step is only emitted when orbs are captured.
func TestMarkdown_ManualSteps_OrgOrbsOnlyWhenPresent(t *testing.T) {
	// Without orbs: no orbs step.
	plain := report.Markdown(&manifest.Manifest{
		Source: manifest.Source{Org: manifest.Org{Name: "o", Slug: "gh/o"}},
	})
	if strings.Contains(plain, "Org orbs (") {
		t.Errorf("Markdown should not mention org orbs republish when no orbs captured")
	}

	// With orbs: step must appear with count and namespace.
	withOrbs := report.Markdown(&manifest.Manifest{
		Source: manifest.Source{Org: manifest.Org{
			Name: "o", Slug: "gh/o",
			Settings: &manifest.OrgSettings{
				OrbNamespace: "acme-ns",
				Orbs: []manifest.OrgOrb{
					{OrbName: "acme-ns/my-orb", LatestVersionNumber: "1.0.0"},
				},
			},
		}},
	})
	for _, want := range []string{"Org orbs (1", "acme-ns", "republish"} {
		if !strings.Contains(withOrbs, want) {
			t.Errorf("Markdown org-orbs republish step missing %q", want)
		}
	}
}

// TestMarkdown_ManualSteps_BudgetBlockOnlyWhenBlockEnforcement verifies that
// the budget enforcement=block step is only emitted when applicable.
func TestMarkdown_ManualSteps_BudgetBlockOnlyWhenBlockEnforcement(t *testing.T) {
	// Warn enforcement: no block warning.
	withWarn := report.Markdown(&manifest.Manifest{
		Source: manifest.Source{Org: manifest.Org{
			Name: "o", Slug: "gh/o",
			Settings: &manifest.OrgSettings{
				Budgets: &manifest.OrgBudgets{
					OrgBudget: &manifest.BudgetEntry{Credits: 500000, EnforcementType: "warn"},
				},
			},
		}},
	})
	if strings.Contains(withWarn, "Budget enforcement mode") {
		t.Errorf("Markdown should not mention budget enforcement block for warn enforcement")
	}

	// Block enforcement: warning must appear.
	withBlock := report.Markdown(&manifest.Manifest{
		Source: manifest.Source{Org: manifest.Org{
			Name: "o", Slug: "gh/o",
			Settings: &manifest.OrgSettings{
				Budgets: &manifest.OrgBudgets{
					OrgBudget: &manifest.BudgetEntry{Credits: 500000, EnforcementType: "block"},
				},
			},
		}},
	})
	for _, want := range []string{"Budget enforcement mode", "block", "manually"} {
		if !strings.Contains(withBlock, want) {
			t.Errorf("Markdown budget-enforcement-block step missing %q", want)
		}
	}
}

// ---------------------------------------------------------------------------
// Issue #131: new captured fields in report
// ---------------------------------------------------------------------------

// TestMarkdown_StorageRetentionLimits_Rendered verifies that when
// StorageRetentionLimits are captured they appear in the report.
func TestMarkdown_StorageRetentionLimits_Rendered(t *testing.T) {
	m := &manifest.Manifest{
		Source: manifest.Source{Org: manifest.Org{
			Name: "o", Slug: "gh/o",
			Settings: &manifest.OrgSettings{
				StorageRetention: &manifest.StorageRetentionControls{
					CacheDays: 7, WorkspaceDays: 7, ArtifactDays: 15,
				},
				StorageRetentionLimits: &manifest.StorageRetentionLimits{
					Cache:     manifest.StorageRetentionBound{Min: 1, Max: 30},
					Workspace: manifest.StorageRetentionBound{Min: 1, Max: 15},
					Artifact:  manifest.StorageRetentionBound{Min: 1, Max: 730},
				},
			},
		}},
	}
	md := report.Markdown(m)
	for _, want := range []string{"Plan limits", "30", "730"} {
		if !strings.Contains(md, want) {
			t.Errorf("Markdown storage retention limits missing %q; got:\n%s", want, md[:min(len(md), 400)])
		}
	}
}

// TestMarkdown_StorageRetentionLimits_NotRenderedWhenNil verifies that when no
// limits are captured, the limits line is not emitted.
func TestMarkdown_StorageRetentionLimits_NotRenderedWhenNil(t *testing.T) {
	m := &manifest.Manifest{
		Source: manifest.Source{Org: manifest.Org{
			Name: "o", Slug: "gh/o",
			Settings: &manifest.OrgSettings{
				StorageRetention: &manifest.StorageRetentionControls{
					CacheDays: 7,
				},
				// No StorageRetentionLimits
			},
		}},
	}
	md := report.Markdown(m)
	if strings.Contains(md, "Plan limits") {
		t.Errorf("Markdown should not emit Plan limits line when StorageRetentionLimits is nil")
	}
}

// TestMarkdown_OrbNamespace_Rendered verifies that OrbNamespace appears in the
// namespaces & orbs section.
func TestMarkdown_OrbNamespace_Rendered(t *testing.T) {
	m := &manifest.Manifest{
		Source: manifest.Source{Org: manifest.Org{
			Name: "o", Slug: "gh/o",
			Settings: &manifest.OrgSettings{
				OrbNamespace: "my-namespace",
			},
		}},
	}
	md := report.Markdown(m)
	if !strings.Contains(md, "my-namespace") {
		t.Errorf("Markdown should contain orb namespace 'my-namespace'; got:\n%s", md)
	}
}

// TestMarkdown_ScheduleActorLogin_Rendered verifies that when a schedule has an
// ActorLogin it appears in the report.
func TestMarkdown_ScheduleActorLogin_Rendered(t *testing.T) {
	m := &manifest.Manifest{
		Source: manifest.Source{Org: manifest.Org{Name: "o", Slug: "gh/o"}},
		Projects: []manifest.Project{
			{
				Slug: "gh/o/web",
				Name: "web",
				Schedules: []manifest.Schedule{
					{Name: "nightly", ActorLogin: "pipeline-bot"},
				},
			},
		},
	}
	md := report.Markdown(m)
	for _, want := range []string{"nightly", "pipeline-bot"} {
		if !strings.Contains(md, want) {
			t.Errorf("Markdown schedule actor login missing %q", want)
		}
	}
}

// TestMarkdown_V11FeatureFlags_ExtraFlagsRendered verifies that extra v1.1
// feature flags (beyond the two well-known ones) are shown in the report.
func TestMarkdown_V11FeatureFlags_ExtraFlagsRendered(t *testing.T) {
	m := &manifest.Manifest{
		Source: manifest.Source{Org: manifest.Org{Name: "o", Slug: "gh/o"}},
		Projects: []manifest.Project{
			{
				Slug: "gh/o/web",
				Name: "web",
				Settings: &manifest.AdvancedSettings{
					V11FeatureFlags: map[string]bool{
						"api-trigger-with-config": true,
						"extra-custom-flag":       false,
					},
				},
			},
		},
	}
	md := report.Markdown(m)
	if !strings.Contains(md, "extra-custom-flag") {
		t.Errorf("Markdown should show extra v1.1 feature flags; got:\n%s", md)
	}
}
