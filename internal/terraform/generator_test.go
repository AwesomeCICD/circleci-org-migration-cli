package terraform_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/terraform"
)

const (
	fixtureManifest           = "testdata/fixture_manifest.json"
	fixtureManifestStandalone = "testdata/fixture_manifest_standalone.json"
	fixtureBundle             = "testdata/fixture_bundle.json"
	fixtureMapping            = "testdata/fixture_mapping.json"
	destOrgID                 = "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
)

func loadManifest(t *testing.T, path string) *manifest.Manifest {
	t.Helper()
	m, err := manifest.Load(path)
	if err != nil {
		t.Fatalf("loading manifest: %v", err)
	}
	return m
}

func loadBundle(t *testing.T, path string) *manifest.SecretBundle {
	t.Helper()
	b, err := manifest.LoadSecretBundle(path)
	if err != nil {
		t.Fatalf("loading bundle: %v", err)
	}
	return b
}

func loadMapping(t *testing.T, path string) *manifest.Mapping {
	t.Helper()
	mp, err := manifest.LoadMapping(path)
	if err != nil {
		t.Fatalf("loading mapping: %v", err)
	}
	return mp
}

// generateTo runs terraform.Generate into a temp dir and returns the dir path.
func generateTo(t *testing.T, m *manifest.Manifest, opts terraform.Options) string {
	t.Helper()
	dir := t.TempDir()
	opts.OutDir = dir
	if err := terraform.Generate(m, opts); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	return dir
}

// readFile reads a file from dir, failing the test if it does not exist.
func readFile(t *testing.T, dir, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("reading %s: %v", name, err)
	}
	return string(data)
}

// fileExists returns true if the named file exists under dir.
func fileExists(t *testing.T, dir, name string) bool {
	t.Helper()
	_, err := os.Stat(filepath.Join(dir, name))
	return err == nil
}

// assertContains fails if s does not contain substr.
func assertContains(t *testing.T, file, s, substr string) {
	t.Helper()
	if !strings.Contains(s, substr) {
		t.Errorf("%s: expected to contain %q\n---\n%s", file, substr, s)
	}
}

// assertNotContains fails if s contains substr.
func assertNotContains(t *testing.T, file, s, substr string) {
	t.Helper()
	if strings.Contains(s, substr) {
		t.Errorf("%s: expected NOT to contain %q\n---\n%s", file, substr, s)
	}
}

// TestGenerate_StaticFiles verifies that versions.tf, providers.tf, contexts.tf,
// and projects.tf are generated with the expected static content.
// The fixture manifest has a "gh/" source slug so org type is inferred as oauth.
func TestGenerate_StaticFiles(t *testing.T) {
	m := loadManifest(t, fixtureManifest)
	dir := generateTo(t, m, terraform.Options{
		DestOrgID:   destOrgID,
		Host:        "https://circleci.com",
		DestOrgType: terraform.OrgTypeOAuth, // explicit to avoid inference notice in test output
	})

	t.Run("versions.tf", func(t *testing.T) {
		s := readFile(t, dir, "versions.tf")
		assertContains(t, "versions.tf", s, `CircleCI-Public/circleci`)
		assertContains(t, "versions.tf", s, `~> 0.3`)
		assertContains(t, "versions.tf", s, `required_version`)
	})

	t.Run("providers.tf", func(t *testing.T) {
		s := readFile(t, dir, "providers.tf")
		assertContains(t, "providers.tf", s, `provider "circleci"`)
		// org_id is in the locals block of providers.tf.
		assertContains(t, "providers.tf", s, destOrgID)
		assertContains(t, "providers.tf", s, `https://circleci.com`)
		// The provider uses 'key' (reads from CIRCLECI_TOKEN env var).
		assertContains(t, "providers.tf", s, `CIRCLECI_TOKEN`)
		// locals block with org_id must be present.
		assertContains(t, "providers.tf", s, `org_id`)
	})

	t.Run("contexts.tf", func(t *testing.T) {
		s := readFile(t, dir, "contexts.tf")
		assertContains(t, "contexts.tf", s, `resource "circleci_context"`)
		assertContains(t, "contexts.tf", s, `resource "circleci_context_environment_variable"`)
		assertContains(t, "contexts.tf", s, `for_each`)
		assertContains(t, "contexts.tf", s, `var.context_secrets`)
		assertContains(t, "contexts.tf", s, `context_id`)
	})

	t.Run("projects.tf_oauth", func(t *testing.T) {
		s := readFile(t, dir, "projects.tf")
		assertContains(t, "projects.tf", s, `resource "circleci_project"`)
		assertContains(t, "projects.tf", s, `resource "circleci_project_environment_variable"`)
		assertContains(t, "projects.tf", s, `for_each`)
		assertContains(t, "projects.tf", s, `var.project_secrets`)
		// OAuth: project_slug references the resource slug output (not a hard-coded slug).
		assertContains(t, "projects.tf", s, `circleci_project.projects[each.value.repo_name].slug`)
		// OAuth: advanced settings HCL assignments must NOT be present.
		// Check for assignment patterns (not just the name, which may appear in comments).
		assertNotContains(t, "projects.tf", s, `auto_cancel_builds            =`)
		assertNotContains(t, "projects.tf", s, `set_github_status             =`)
		assertNotContains(t, "projects.tf", s, `advanced_settings`)
	})
}

// TestGenerate_StaticFiles_Standalone verifies projects.tf for standalone orgs
// includes advanced settings and uses the .slug reference.
func TestGenerate_StaticFiles_Standalone(t *testing.T) {
	m := loadManifest(t, fixtureManifestStandalone)
	dir := generateTo(t, m, terraform.Options{
		DestOrgID:   destOrgID,
		Host:        "https://circleci.com",
		DestOrgType: terraform.OrgTypeStandalone,
	})

	t.Run("projects.tf_standalone", func(t *testing.T) {
		s := readFile(t, dir, "projects.tf")
		assertContains(t, "projects.tf", s, `resource "circleci_project"`)
		assertContains(t, "projects.tf", s, `resource "circleci_project_environment_variable"`)
		assertContains(t, "projects.tf", s, `for_each`)
		assertContains(t, "projects.tf", s, `var.project_secrets`)
		// Standalone: advanced settings MUST be present.
		assertContains(t, "projects.tf", s, `advanced_settings`)
		assertContains(t, "projects.tf", s, `auto_cancel_builds`)
		assertContains(t, "projects.tf", s, `set_github_status`)
		// project_slug references the resource slug output.
		assertContains(t, "projects.tf", s, `circleci_project.projects[each.value.repo_name].slug`)
	})
}

// TestGenerate_MigrationTFVars verifies migration.auto.tfvars.json content
// for a standalone destination (advanced settings should be present in tfvars).
func TestGenerate_MigrationTFVars(t *testing.T) {
	m := loadManifest(t, fixtureManifest)
	dir := generateTo(t, m, terraform.Options{
		DestOrgID:   destOrgID,
		DestOrgType: terraform.OrgTypeStandalone, // explicit: test the standalone path
	})

	data := readFile(t, dir, "migration.auto.tfvars.json")

	// Parse and validate structure.
	var tfvars terraform.TFVarsFile
	if err := json.Unmarshal([]byte(data), &tfvars); err != nil {
		t.Fatalf("parsing migration.auto.tfvars.json: %v", err)
	}

	// Contexts must include both contexts, sorted by name.
	if len(tfvars.Contexts) != 2 {
		t.Errorf("expected 2 contexts, got %d", len(tfvars.Contexts))
	}
	if tfvars.Contexts[0].Name != "deploy-prod" {
		t.Errorf("expected first context name 'deploy-prod', got %q", tfvars.Contexts[0].Name)
	}
	if tfvars.Contexts[1].Name != "shared-ci" {
		t.Errorf("expected second context name 'shared-ci', got %q", tfvars.Contexts[1].Name)
	}
	// deploy-prod must list 3 env vars.
	if len(tfvars.Contexts[0].EnvVars) != 3 {
		t.Errorf("expected 3 env vars for deploy-prod, got %d", len(tfvars.Contexts[0].EnvVars))
	}

	// Projects.
	if len(tfvars.Projects) != 2 {
		t.Errorf("expected 2 projects, got %d", len(tfvars.Projects))
	}

	// The 'api' project must have advanced settings.
	var apiProj *terraform.TFVarProject
	for i := range tfvars.Projects {
		if tfvars.Projects[i].RepoName == "api" {
			apiProj = &tfvars.Projects[i]
		}
	}
	if apiProj == nil {
		t.Fatal("project 'api' not found in tfvars")
	}
	if v, ok := apiProj.AdvancedSettings["set_github_status"]; !ok || !v {
		t.Errorf("api project: expected set_github_status=true, got %v", apiProj.AdvancedSettings)
	}
	// write_settings_requires_admin IS a provider attribute in v0.3.x; it should
	// appear in the tfvars when set in the manifest.
	if v, ok := apiProj.AdvancedSettings["write_settings_requires_admin"]; !ok || !v {
		t.Errorf("api project: expected write_settings_requires_admin=true in tfvars (provider supports it), got %v", apiProj.AdvancedSettings)
	}
	// auto_cancel_builds (mapped from autocancel_builds) must appear with correct provider name.
	if v, ok := apiProj.AdvancedSettings["auto_cancel_builds"]; !ok || !v {
		t.Errorf("api project: expected auto_cancel_builds=true (mapped from autocancel_builds), got %v", apiProj.AdvancedSettings)
	}
	// autocancel_builds (manifest name) must NOT appear; only the provider name.
	if _, ok := apiProj.AdvancedSettings["autocancel_builds"]; ok {
		t.Errorf("api project: manifest name 'autocancel_builds' should not appear; use provider name 'auto_cancel_builds'")
	}

	// Values must NOT appear in migration.auto.tfvars.json.
	assertNotContains(t, "migration.auto.tfvars.json", data, "AKIAIOSFODNN7EXAMPLE")
	assertNotContains(t, "migration.auto.tfvars.json", data, "postgres://")
}

// TestGenerate_WithSecrets verifies secrets.auto.tfvars.json emitted when a
// bundle is provided.
func TestGenerate_WithSecrets(t *testing.T) {
	m := loadManifest(t, fixtureManifest)
	bundle := loadBundle(t, fixtureBundle)
	dir := generateTo(t, m, terraform.Options{
		DestOrgID:     destOrgID,
		SecretsBundle: bundle,
		DestOrgType:   terraform.OrgTypeOAuth, // fixture manifest has "gh/" source slug
	})

	// secrets.auto.tfvars.json must exist.
	if !fileExists(t, dir, "secrets.auto.tfvars.json") {
		t.Fatal("secrets.auto.tfvars.json was not generated")
	}

	data := readFile(t, dir, "secrets.auto.tfvars.json")

	var sv terraform.SecretsVarsFile
	if err := json.Unmarshal([]byte(data), &sv); err != nil {
		t.Fatalf("parsing secrets.auto.tfvars.json: %v", err)
	}

	// Context secrets must be present.
	deploySecrets := sv.ContextSecrets["deploy-prod"]
	if deploySecrets == nil {
		t.Fatal("deploy-prod secrets not found")
	}
	if deploySecrets["AWS_ACCESS_KEY_ID"] != "AKIAIOSFODNN7EXAMPLE" {
		t.Errorf("deploy-prod AWS_ACCESS_KEY_ID mismatch: %q", deploySecrets["AWS_ACCESS_KEY_ID"])
	}

	// Project secrets for 'api' must be present.
	apiSecrets := sv.ProjectSecrets["api"]
	if apiSecrets == nil {
		t.Fatal("api project secrets not found")
	}
	if !strings.Contains(apiSecrets["DATABASE_URL"], "postgres://") {
		t.Errorf("api DATABASE_URL mismatch: %q", apiSecrets["DATABASE_URL"])
	}

	// SECRETS_WORKBOOK.md must NOT be generated (that is only for --placeholders).
	if fileExists(t, dir, "SECRETS_WORKBOOK.md") {
		t.Error("SECRETS_WORKBOOK.md should not be generated when --secrets is given")
	}
}

// TestGenerate_WithPlaceholders verifies secrets.auto.tfvars.json uses REPLACE_ME
// and that SECRETS_WORKBOOK.md is generated.
func TestGenerate_WithPlaceholders(t *testing.T) {
	m := loadManifest(t, fixtureManifest)
	dir := generateTo(t, m, terraform.Options{
		DestOrgID:    destOrgID,
		Placeholders: true,
		DestOrgType:  terraform.OrgTypeOAuth, // fixture manifest has "gh/" source slug
	})

	// secrets.auto.tfvars.json must exist.
	if !fileExists(t, dir, "secrets.auto.tfvars.json") {
		t.Fatal("secrets.auto.tfvars.json was not generated")
	}

	data := readFile(t, dir, "secrets.auto.tfvars.json")
	assertContains(t, "secrets.auto.tfvars.json", data, "REPLACE_ME")
	assertNotContains(t, "secrets.auto.tfvars.json", data, "AKIAIOSFODNN7EXAMPLE")

	// SECRETS_WORKBOOK.md must be generated.
	if !fileExists(t, dir, "SECRETS_WORKBOOK.md") {
		t.Fatal("SECRETS_WORKBOOK.md was not generated")
	}
	wb := readFile(t, dir, "SECRETS_WORKBOOK.md")
	assertContains(t, "SECRETS_WORKBOOK.md", wb, "deploy-prod")
	assertContains(t, "SECRETS_WORKBOOK.md", wb, "AWS_ACCESS_KEY_ID")
}

// TestGenerate_WithMapping verifies that project slugs are remapped per the
// mapping file and that the remapped repo names appear in the tfvars.
func TestGenerate_WithMapping(t *testing.T) {
	m := loadManifest(t, fixtureManifest)
	mp := loadMapping(t, fixtureMapping)

	dir := generateTo(t, m, terraform.Options{
		DestOrgID:   destOrgID,
		Mapping:     mp,
		DestOrgType: terraform.OrgTypeOAuth, // fixture manifest has "gh/" source slug
	})

	data := readFile(t, dir, "migration.auto.tfvars.json")

	// The mapping says from=gh/acme to=gh/acme-new — project slugs are remapped.
	// The repo name (last segment) stays the same, so "api" and "frontend" still
	// appear as repo names in the tfvars.
	assertContains(t, "migration.auto.tfvars.json", data, `"api"`)
	assertContains(t, "migration.auto.tfvars.json", data, `"frontend"`)
}

// TestGenerate_GAPSmd verifies GAPS.md is generated and contains the expected
// gap categories based on the fixture manifest (standalone dest — no proj-settings gap).
func TestGenerate_GAPSmd(t *testing.T) {
	m := loadManifest(t, fixtureManifest)
	dir := generateTo(t, m, terraform.Options{
		DestOrgID:   destOrgID,
		DestOrgType: terraform.OrgTypeStandalone,
	})

	if !fileExists(t, dir, "GAPS.md") {
		t.Fatal("GAPS.md was not generated")
	}

	gaps := readFile(t, dir, "GAPS.md")

	// The fixture has org settings → should mention org settings gap.
	assertContains(t, "GAPS.md", gaps, "Org-level settings")
	// The fixture has schedules → should mention schedules gap.
	assertContains(t, "GAPS.md", gaps, "Legacy v2 schedules")
	// The fixture has checkout keys → should mention checkout keys gap.
	assertContains(t, "GAPS.md", gaps, "Checkout / deploy keys")
	// The fixture has API tokens → should mention project API tokens gap.
	assertContains(t, "GAPS.md", gaps, "Project API tokens")
	// Context restrictions → M2 gap.
	assertContains(t, "GAPS.md", gaps, "Context restrictions")
	// Commands should reference circleci-migrate sync.
	assertContains(t, "GAPS.md", gaps, "circleci-migrate sync")
}

// TestGenerate_NoSecretsWithoutFlag verifies that secrets.auto.tfvars.json is
// NOT written unless --secrets or --placeholders is given.
func TestGenerate_NoSecretsWithoutFlag(t *testing.T) {
	m := loadManifest(t, fixtureManifest)
	dir := generateTo(t, m, terraform.Options{
		DestOrgID:   destOrgID,
		DestOrgType: terraform.OrgTypeOAuth,
	})

	if fileExists(t, dir, "secrets.auto.tfvars.json") {
		t.Error("secrets.auto.tfvars.json must NOT be generated without --secrets or --placeholders")
	}
}

// TestGenerate_CustomHost verifies that --host is reflected in providers.tf.
func TestGenerate_CustomHost(t *testing.T) {
	m := loadManifest(t, fixtureManifest)
	dir := generateTo(t, m, terraform.Options{
		DestOrgID:   destOrgID,
		Host:        "https://circleci.example.com",
		DestOrgType: terraform.OrgTypeOAuth,
	})

	s := readFile(t, dir, "providers.tf")
	assertContains(t, "providers.tf", s, "https://circleci.example.com")
}

// TestGenerate_OrgTypeOAuth verifies OAuth output: no advanced settings in
// projects.tf or migration.auto.tfvars.json; project_slug uses .slug reference;
// GAPS.md includes the advanced-settings gap.
func TestGenerate_OrgTypeOAuth(t *testing.T) {
	m := loadManifest(t, fixtureManifest) // source slug: gh/acme (OAuth)
	dir := generateTo(t, m, terraform.Options{
		DestOrgID:   destOrgID,
		DestOrgType: terraform.OrgTypeOAuth,
	})

	t.Run("projects.tf_no_advanced_settings", func(t *testing.T) {
		s := readFile(t, dir, "projects.tf")
		assertContains(t, "projects.tf", s, `resource "circleci_project"`)
		assertContains(t, "projects.tf", s, `resource "circleci_project_environment_variable"`)
		// project_slug must use the .slug computed attribute, not a hard-coded value.
		assertContains(t, "projects.tf", s, `circleci_project.projects[each.value.repo_name].slug`)
		// Advanced settings HCL assignments must be absent for OAuth.
		// Check for = assignment patterns to avoid false positives from comments.
		assertNotContains(t, "projects.tf", s, `auto_cancel_builds            =`)
		assertNotContains(t, "projects.tf", s, `build_fork_prs                =`)
		assertNotContains(t, "projects.tf", s, `disable_ssh                   =`)
		assertNotContains(t, "projects.tf", s, `forks_receive_secret_env_vars =`)
		assertNotContains(t, "projects.tf", s, `set_github_status             =`)
		assertNotContains(t, "projects.tf", s, `setup_workflows               =`)
		assertNotContains(t, "projects.tf", s, `write_settings_requires_admin =`)
	})

	t.Run("tfvars_no_advanced_settings", func(t *testing.T) {
		data := readFile(t, dir, "migration.auto.tfvars.json")
		// advanced_settings must be absent (empty map → omitempty drops it).
		assertNotContains(t, "migration.auto.tfvars.json", data, `"advanced_settings"`)
	})

	t.Run("GAPS.md_has_project_settings_gap", func(t *testing.T) {
		gaps := readFile(t, dir, "GAPS.md")
		// OAuth must surface the advanced-settings gap.
		assertContains(t, "GAPS.md", gaps, "Project advanced settings")
		assertContains(t, "GAPS.md", gaps, "OAuth")
		assertContains(t, "GAPS.md", gaps, "circleci-migrate sync")
	})
}

// TestGenerate_OrgTypeStandalone verifies standalone output: advanced settings
// ARE included in projects.tf and migration.auto.tfvars.json; no project-settings gap.
func TestGenerate_OrgTypeStandalone(t *testing.T) {
	m := loadManifest(t, fixtureManifestStandalone) // source slug: circleci/... (standalone)
	dir := generateTo(t, m, terraform.Options{
		DestOrgID:   destOrgID,
		DestOrgType: terraform.OrgTypeStandalone,
	})

	t.Run("projects.tf_has_advanced_settings", func(t *testing.T) {
		s := readFile(t, dir, "projects.tf")
		assertContains(t, "projects.tf", s, `resource "circleci_project"`)
		// Standalone: advanced settings must be present.
		assertContains(t, "projects.tf", s, `advanced_settings`)
		assertContains(t, "projects.tf", s, `auto_cancel_builds`)
		assertContains(t, "projects.tf", s, `set_github_status`)
		// project_slug uses .slug computed attribute.
		assertContains(t, "projects.tf", s, `circleci_project.projects[each.value.repo_name].slug`)
	})

	t.Run("tfvars_has_advanced_settings", func(t *testing.T) {
		data := readFile(t, dir, "migration.auto.tfvars.json")
		// Advanced settings must appear for the 'api' project (has settings in fixture).
		assertContains(t, "migration.auto.tfvars.json", data, `"advanced_settings"`)
		assertContains(t, "migration.auto.tfvars.json", data, `"auto_cancel_builds"`)
		assertContains(t, "migration.auto.tfvars.json", data, `"set_github_status"`)
	})

	t.Run("GAPS.md_no_project_settings_gap", func(t *testing.T) {
		gaps := readFile(t, dir, "GAPS.md")
		// Standalone must NOT have the OAuth project-settings gap.
		assertNotContains(t, "GAPS.md", gaps, "Project advanced settings (OAuth org")
	})
}

// TestGenerate_OrgTypeInference verifies that the org type is correctly inferred
// from the source slug when --dest-org-type is not specified.
func TestGenerate_OrgTypeInference(t *testing.T) {
	t.Run("gh_slug_infers_oauth", func(t *testing.T) {
		m := loadManifest(t, fixtureManifest) // slug: gh/acme
		got := terraform.InferOrgType(m)
		if got != terraform.OrgTypeOAuth {
			t.Errorf("InferOrgType(gh/acme) = %v, want OrgTypeOAuth", got)
		}
	})

	t.Run("circleci_slug_infers_standalone", func(t *testing.T) {
		m := loadManifest(t, fixtureManifestStandalone) // slug: circleci/...
		got := terraform.InferOrgType(m)
		if got != terraform.OrgTypeStandalone {
			t.Errorf("InferOrgType(circleci/...) = %v, want OrgTypeStandalone", got)
		}
	})
}

// TestGenerate_InferencePaths exercises the Generate function's inference code
// paths (OrgTypeUnknown → infer from slug) so the stderr-printing branches are
// covered. The actual content correctness is verified in the org-type tests above.
func TestGenerate_InferencePaths(t *testing.T) {
	t.Run("infer_oauth_from_gh_slug", func(t *testing.T) {
		m := loadManifest(t, fixtureManifest) // slug: gh/acme → should infer OrgTypeOAuth
		// OrgTypeUnknown causes inference; we just verify generation succeeds.
		dir := generateTo(t, m, terraform.Options{
			DestOrgID:   destOrgID,
			DestOrgType: terraform.OrgTypeUnknown, // explicit: exercise inference path
		})
		s := readFile(t, dir, "projects.tf")
		// Result should be the OAuth template (no advanced settings assignments).
		assertNotContains(t, "projects.tf", s, `auto_cancel_builds            =`)
	})

	t.Run("infer_standalone_from_circleci_slug", func(t *testing.T) {
		m := loadManifest(t, fixtureManifestStandalone) // slug: circleci/... → standalone
		dir := generateTo(t, m, terraform.Options{
			DestOrgID:   destOrgID,
			DestOrgType: terraform.OrgTypeUnknown, // explicit: exercise inference path
		})
		s := readFile(t, dir, "projects.tf")
		// Result should be the standalone template (advanced settings present).
		assertContains(t, "projects.tf", s, `advanced_settings`)
	})
}

// TestParseOrgType verifies all accepted --dest-org-type values and aliases.
func TestParseOrgType(t *testing.T) {
	cases := []struct {
		input string
		want  terraform.OrgType
		errOK bool
	}{
		{"oauth", terraform.OrgTypeOAuth, false},
		{"gh", terraform.OrgTypeOAuth, false},
		{"github", terraform.OrgTypeOAuth, false},
		{"GH", terraform.OrgTypeOAuth, false},    // case-insensitive
		{"OAuth", terraform.OrgTypeOAuth, false}, // mixed case
		{"standalone", terraform.OrgTypeStandalone, false},
		{"app", terraform.OrgTypeStandalone, false},
		{"github_app", terraform.OrgTypeStandalone, false},
		{"Standalone", terraform.OrgTypeStandalone, false}, // mixed case
		{"unknown-value", terraform.OrgTypeUnknown, true},
		{"", terraform.OrgTypeUnknown, true},
	}
	for _, tc := range cases {
		got, err := terraform.ParseOrgType(tc.input)
		if tc.errOK {
			if err == nil {
				t.Errorf("ParseOrgType(%q): expected error, got nil (result %v)", tc.input, got)
			}
		} else {
			if err != nil {
				t.Errorf("ParseOrgType(%q): unexpected error: %v", tc.input, err)
			}
			if got != tc.want {
				t.Errorf("ParseOrgType(%q) = %v, want %v", tc.input, got, tc.want)
			}
		}
	}
}

// TestGenerate_SlugReference verifies that circleci_project_environment_variable
// uses the project resource's .slug computed attribute (not a hard-coded slug)
// for both org types, ensuring the correct format is used automatically.
func TestGenerate_SlugReference(t *testing.T) {
	for _, tc := range []struct {
		name    string
		fixture string
		orgType terraform.OrgType
	}{
		{"oauth", fixtureManifest, terraform.OrgTypeOAuth},
		{"standalone", fixtureManifestStandalone, terraform.OrgTypeStandalone},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := loadManifest(t, tc.fixture)
			dir := generateTo(t, m, terraform.Options{
				DestOrgID:   destOrgID,
				DestOrgType: tc.orgType,
			})
			s := readFile(t, dir, "projects.tf")
			// The env-var resource must use the computed .slug output, not dest_slug.
			assertContains(t, "projects.tf", s, `circleci_project.projects[each.value.repo_name].slug`)
			// Hard-coded dest_slug must not be used as the project_slug value.
			// (dest_slug may still appear in the variable definition, just not as the project_slug value)
			assertNotContains(t, "projects.tf", s, `project_slug = each.value.dest_slug`)
		})
	}
}

// TestGenerate_TFIdentifier validates edge cases in the identifier sanitiser.
func TestGenerate_TFIdentifier(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"my-context", "my_context"},
		{"123start", "r123start"},
		{"hello world", "hello_world"},
		{"", "resource"},
		{"a", "a"},
	}
	for _, tc := range cases {
		got := terraform.TFIdentifier(tc.input)
		if got != tc.want {
			t.Errorf("TFIdentifier(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}
