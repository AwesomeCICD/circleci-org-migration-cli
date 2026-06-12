package terraform_test

// M2 golden-file tests for:
//   - circleci_context_restriction (project + expression: both org types; group: OAuth ONLY)
//   - circleci_pipeline + circleci_trigger (standalone ONLY; omitted for OAuth)
//   - circleci_webhook (both org types)
//   - circleci_runner_resource_class + circleci_runner_token (both org types)
//   - --import-existing (import block emission)
//   - migration.auto.tfvars.json includes M2 sections
//   - GAPS.md updated for M2

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/terraform"
)

const fixtureExisting = "testdata/fixture_existing.json"

// ----------------------------------------------------------------------------
// restrictions.tf — context restrictions
// ----------------------------------------------------------------------------

// TestGenerate_M2_Restrictions_OAuth verifies restrictions.tf for OAuth orgs:
//   - project restrictions: emitted
//   - expression restrictions: emitted
//   - group restrictions: emitted (OAuth supports group restrictions)
func TestGenerate_M2_Restrictions_OAuth(t *testing.T) {
	m := loadManifest(t, fixtureManifest) // source slug: gh/acme (OAuth)
	dir := generateTo(t, m, terraform.Options{
		DestOrgID:   destOrgID,
		DestOrgType: terraform.OrgTypeOAuth,
	})

	s := readFile(t, dir, "restrictions.tf")
	assertContains(t, "restrictions.tf", s, `resource "circleci_context_restriction"`)
	assertContains(t, "restrictions.tf", s, `type`)
	assertContains(t, "restrictions.tf", s, `value`)
	assertContains(t, "restrictions.tf", s, `context_id`)
	// Variable definition must be present.
	assertContains(t, "restrictions.tf", s, `variable "restrictions"`)
	// Project restriction reference via project resource.
	assertContains(t, "restrictions.tf", s, `circleci_project.projects`)

	// For OAuth: group restrictions ARE generated (OAuth supports them).
	// The tfvars should have entries including both project and expression types.
	tfvarsData := readFile(t, dir, "migration.auto.tfvars.json")
	var tfvars terraform.TFVarsFile
	if err := json.Unmarshal([]byte(tfvarsData), &tfvars); err != nil {
		t.Fatalf("parsing migration.auto.tfvars.json: %v", err)
	}
	if len(tfvars.Restrictions) == 0 {
		t.Error("expected restrictions in tfvars, got 0")
	}
	// Count restriction types.
	typeCount := make(map[string]int)
	for _, r := range tfvars.Restrictions {
		typeCount[r.Type]++
	}
	if typeCount["project"] == 0 {
		t.Error("expected at least one project restriction in tfvars")
	}
	if typeCount["expression"] == 0 {
		t.Error("expected at least one expression restriction in tfvars")
	}
	// OAuth: group restrictions must be included.
	if typeCount["group"] == 0 {
		t.Error("expected at least one group restriction for OAuth org in tfvars")
	}
}

// TestGenerate_M2_Restrictions_Standalone verifies restrictions.tf for standalone orgs:
//   - project restrictions: emitted
//   - expression restrictions: emitted
//   - group restrictions: OMITTED (provider rejects on standalone orgs)
func TestGenerate_M2_Restrictions_Standalone(t *testing.T) {
	m := loadManifest(t, fixtureManifestStandalone)
	dir := generateTo(t, m, terraform.Options{
		DestOrgID:   destOrgID,
		DestOrgType: terraform.OrgTypeStandalone,
	})

	s := readFile(t, dir, "restrictions.tf")
	assertContains(t, "restrictions.tf", s, `resource "circleci_context_restriction"`)
	assertContains(t, "restrictions.tf", s, `type`)

	// Tfvars: should have project and expression, but NO group.
	tfvarsData := readFile(t, dir, "migration.auto.tfvars.json")
	var tfvars terraform.TFVarsFile
	if err := json.Unmarshal([]byte(tfvarsData), &tfvars); err != nil {
		t.Fatalf("parsing migration.auto.tfvars.json: %v", err)
	}
	typeCount := make(map[string]int)
	for _, r := range tfvars.Restrictions {
		typeCount[r.Type]++
	}
	if typeCount["project"] == 0 {
		t.Error("expected at least one project restriction for standalone")
	}
	if typeCount["expression"] == 0 {
		t.Error("expected at least one expression restriction for standalone")
	}
	// Standalone: group restrictions must NOT be present.
	if typeCount["group"] != 0 {
		t.Errorf("group restrictions must be omitted for standalone orgs, got %d", typeCount["group"])
	}

	// GAPS.md must mention the group restrictions gap for standalone.
	gaps := readFile(t, dir, "GAPS.md")
	assertContains(t, "GAPS.md", gaps, "group restrictions")
	assertContains(t, "GAPS.md", gaps, "standalone")
}

// TestGenerate_M2_Restrictions_ProjectRef verifies that project restrictions
// carry a project_repo_name field so the HCL can reference the project resource.
func TestGenerate_M2_Restrictions_ProjectRef(t *testing.T) {
	m := loadManifest(t, fixtureManifest)
	dir := generateTo(t, m, terraform.Options{
		DestOrgID:   destOrgID,
		DestOrgType: terraform.OrgTypeOAuth,
	})

	tfvarsData := readFile(t, dir, "migration.auto.tfvars.json")
	var tfvars terraform.TFVarsFile
	if err := json.Unmarshal([]byte(tfvarsData), &tfvars); err != nil {
		t.Fatalf("parsing migration.auto.tfvars.json: %v", err)
	}
	for _, r := range tfvars.Restrictions {
		if r.Type == "project" {
			if r.ProjectRepoName == "" {
				t.Errorf("project restriction %q missing project_repo_name", r.Value)
			}
		}
	}
}

// ----------------------------------------------------------------------------
// webhooks.tf
// ----------------------------------------------------------------------------

// TestGenerate_M2_Webhooks_OAuth verifies webhooks.tf for OAuth orgs.
func TestGenerate_M2_Webhooks_OAuth(t *testing.T) {
	m := loadManifest(t, fixtureManifest)
	dir := generateTo(t, m, terraform.Options{
		DestOrgID:   destOrgID,
		DestOrgType: terraform.OrgTypeOAuth,
	})

	s := readFile(t, dir, "webhooks.tf")
	assertContains(t, "webhooks.tf", s, `resource "circleci_webhook"`)
	assertContains(t, "webhooks.tf", s, `scope_id`)
	assertContains(t, "webhooks.tf", s, `circleci_project.projects`)
	assertContains(t, "webhooks.tf", s, `events`)
	assertContains(t, "webhooks.tf", s, `verify_tls`)
	assertContains(t, "webhooks.tf", s, `variable "webhooks"`)

	// Tfvars should have webhook entries.
	tfvarsData := readFile(t, dir, "migration.auto.tfvars.json")
	var tfvars terraform.TFVarsFile
	if err := json.Unmarshal([]byte(tfvarsData), &tfvars); err != nil {
		t.Fatalf("parsing migration.auto.tfvars.json: %v", err)
	}
	if len(tfvars.Webhooks) == 0 {
		t.Error("expected webhook entries in tfvars, got 0")
	}
	// Check that the notify-slack webhook from api project is included.
	found := false
	for _, w := range tfvars.Webhooks {
		if w.Name == "notify-slack" && w.ProjectRepoName == "api" {
			found = true
			if w.URL == "" {
				t.Error("webhook URL should not be empty")
			}
			if len(w.Events) == 0 {
				t.Error("webhook events should not be empty")
			}
		}
	}
	if !found {
		t.Error("expected to find notify-slack webhook for api project in tfvars")
	}
}

// TestGenerate_M2_Webhooks_Standalone verifies webhooks.tf is also generated for standalone orgs.
func TestGenerate_M2_Webhooks_Standalone(t *testing.T) {
	m := loadManifest(t, fixtureManifestStandalone)
	dir := generateTo(t, m, terraform.Options{
		DestOrgID:   destOrgID,
		DestOrgType: terraform.OrgTypeStandalone,
	})

	s := readFile(t, dir, "webhooks.tf")
	assertContains(t, "webhooks.tf", s, `resource "circleci_webhook"`)

	tfvarsData := readFile(t, dir, "migration.auto.tfvars.json")
	var tfvars terraform.TFVarsFile
	if err := json.Unmarshal([]byte(tfvarsData), &tfvars); err != nil {
		t.Fatalf("parsing migration.auto.tfvars.json: %v", err)
	}
	if len(tfvars.Webhooks) == 0 {
		t.Error("expected webhook entries for standalone in tfvars, got 0")
	}
}

// ----------------------------------------------------------------------------
// runners.tf
// ----------------------------------------------------------------------------

// TestGenerate_M2_Runners_OAuth verifies runners.tf for OAuth orgs.
func TestGenerate_M2_Runners_OAuth(t *testing.T) {
	m := loadManifest(t, fixtureManifest)
	dir := generateTo(t, m, terraform.Options{
		DestOrgID:   destOrgID,
		DestOrgType: terraform.OrgTypeOAuth,
	})

	s := readFile(t, dir, "runners.tf")
	assertContains(t, "runners.tf", s, `resource "circleci_runner_resource_class"`)
	assertContains(t, "runners.tf", s, `resource "circleci_runner_token"`)
	assertContains(t, "runners.tf", s, `resource_class`)
	assertContains(t, "runners.tf", s, `variable "runner_classes"`)

	// Tfvars should have runner class entries.
	tfvarsData := readFile(t, dir, "migration.auto.tfvars.json")
	var tfvars terraform.TFVarsFile
	if err := json.Unmarshal([]byte(tfvarsData), &tfvars); err != nil {
		t.Fatalf("parsing migration.auto.tfvars.json: %v", err)
	}
	if len(tfvars.RunnerClasses) == 0 {
		t.Error("expected runner_classes in tfvars, got 0")
	}
	// Should have acme/linux-medium and acme/linux-xlarge.
	found := make(map[string]bool)
	for _, rc := range tfvars.RunnerClasses {
		found[rc.ResourceClass] = true
	}
	for _, want := range []string{"acme/linux-medium", "acme/linux-xlarge"} {
		if !found[want] {
			t.Errorf("expected runner class %q in tfvars", want)
		}
	}
}

// TestGenerate_M2_Runners_Standalone verifies runners.tf for standalone orgs.
func TestGenerate_M2_Runners_Standalone(t *testing.T) {
	m := loadManifest(t, fixtureManifestStandalone)
	dir := generateTo(t, m, terraform.Options{
		DestOrgID:   destOrgID,
		DestOrgType: terraform.OrgTypeStandalone,
	})

	s := readFile(t, dir, "runners.tf")
	assertContains(t, "runners.tf", s, `resource "circleci_runner_resource_class"`)
	assertContains(t, "runners.tf", s, `resource "circleci_runner_token"`)

	tfvarsData := readFile(t, dir, "migration.auto.tfvars.json")
	var tfvars terraform.TFVarsFile
	if err := json.Unmarshal([]byte(tfvarsData), &tfvars); err != nil {
		t.Fatalf("parsing migration.auto.tfvars.json: %v", err)
	}
	if len(tfvars.RunnerClasses) == 0 {
		t.Error("expected runner_classes for standalone in tfvars, got 0")
	}
	if tfvars.RunnerClasses[0].ResourceClass != "acme-app/linux-medium" {
		t.Errorf("expected acme-app/linux-medium, got %q", tfvars.RunnerClasses[0].ResourceClass)
	}
}

// TestGenerate_M2_Runners_DestNamespace verifies that --dest-runner-namespace
// causes the resource class names to use the destination namespace.
func TestGenerate_M2_Runners_DestNamespace(t *testing.T) {
	m := loadManifest(t, fixtureManifest) // source namespace: acme
	dir := generateTo(t, m, terraform.Options{
		DestOrgID:           destOrgID,
		DestOrgType:         terraform.OrgTypeOAuth,
		DestRunnerNamespace: "acme-new",
	})

	tfvarsData := readFile(t, dir, "migration.auto.tfvars.json")
	var tfvars terraform.TFVarsFile
	if err := json.Unmarshal([]byte(tfvarsData), &tfvars); err != nil {
		t.Fatalf("parsing migration.auto.tfvars.json: %v", err)
	}
	for _, rc := range tfvars.RunnerClasses {
		if !startsWith(rc.ResourceClass, "acme-new/") {
			t.Errorf("expected runner class to use dest namespace 'acme-new/', got %q", rc.ResourceClass)
		}
	}
}

// ----------------------------------------------------------------------------
// pipelines.tf
// ----------------------------------------------------------------------------

// TestGenerate_M2_Pipelines_Standalone verifies pipelines.tf is generated for standalone orgs.
func TestGenerate_M2_Pipelines_Standalone(t *testing.T) {
	m := loadManifest(t, fixtureManifestStandalone)
	dir := generateTo(t, m, terraform.Options{
		DestOrgID:   destOrgID,
		DestOrgType: terraform.OrgTypeStandalone,
	})

	// pipelines.tf must exist for standalone.
	if !fileExists(t, dir, "pipelines.tf") {
		t.Fatal("pipelines.tf should be generated for standalone orgs")
	}
	s := readFile(t, dir, "pipelines.tf")
	assertContains(t, "pipelines.tf", s, `resource "circleci_pipeline"`)
	assertContains(t, "pipelines.tf", s, `resource "circleci_trigger"`)
	assertContains(t, "pipelines.tf", s, `project_id`)
	assertContains(t, "pipelines.tf", s, `pipeline_id`)
	assertContains(t, "pipelines.tf", s, `config_source_provider`)
	assertContains(t, "pipelines.tf", s, `checkout_source_provider`)
	assertContains(t, "pipelines.tf", s, `event_source_provider`)
	assertContains(t, "pipelines.tf", s, `variable "pipelines"`)

	// Tfvars should have pipeline entries.
	tfvarsData := readFile(t, dir, "migration.auto.tfvars.json")
	var tfvars terraform.TFVarsFile
	if err := json.Unmarshal([]byte(tfvarsData), &tfvars); err != nil {
		t.Fatalf("parsing migration.auto.tfvars.json: %v", err)
	}
	if len(tfvars.Pipelines) == 0 {
		t.Error("expected pipeline entries for standalone in tfvars, got 0")
	}
	pd := tfvars.Pipelines[0]
	if pd.ProjectRepoName == "" {
		t.Error("pipeline project_repo_name should not be empty")
	}
	if pd.ConfigSourceProvider != "github_app" {
		t.Errorf("expected config_provider github_app, got %q", pd.ConfigSourceProvider)
	}
	if pd.ConfigSourceRepoExternalID == "" {
		t.Error("pipeline config_repo_external_id should not be empty")
	}
	if len(pd.Triggers) == 0 {
		t.Error("expected at least one trigger in pipeline tfvars entry")
	}
}

// TestGenerate_M2_Pipelines_OAuth_Omitted verifies that pipelines.tf is NOT
// generated for OAuth orgs (provider schema rejects github_oauth).
func TestGenerate_M2_Pipelines_OAuth_Omitted(t *testing.T) {
	m := loadManifest(t, fixtureManifest) // source slug: gh/acme (OAuth)
	dir := generateTo(t, m, terraform.Options{
		DestOrgID:   destOrgID,
		DestOrgType: terraform.OrgTypeOAuth,
	})

	// pipelines.tf must NOT exist for OAuth.
	if fileExists(t, dir, "pipelines.tf") {
		t.Error("pipelines.tf should NOT be generated for OAuth orgs")
	}

	// Tfvars must not have pipeline entries for OAuth.
	tfvarsData := readFile(t, dir, "migration.auto.tfvars.json")
	var tfvars terraform.TFVarsFile
	if err := json.Unmarshal([]byte(tfvarsData), &tfvars); err != nil {
		t.Fatalf("parsing migration.auto.tfvars.json: %v", err)
	}
	if len(tfvars.Pipelines) != 0 {
		t.Errorf("pipelines must be absent from tfvars for OAuth, got %d", len(tfvars.Pipelines))
	}

	// GAPS.md must have the OAuth pipeline/trigger gap.
	gaps := readFile(t, dir, "GAPS.md")
	assertContains(t, "GAPS.md", gaps, "Pipeline definitions and triggers")
	assertContains(t, "GAPS.md", gaps, "OAuth")
	assertContains(t, "GAPS.md", gaps, "provider rejects")
}

// TestGenerate_M2_Pipelines_OAuth_InTFVars verifies that pipeline entries
// from an OAuth manifest are NOT in tfvars (pipelines are omitted for OAuth).
func TestGenerate_M2_Pipelines_GAPS_OAuth(t *testing.T) {
	m := loadManifest(t, fixtureManifest)
	dir := generateTo(t, m, terraform.Options{
		DestOrgID:   destOrgID,
		DestOrgType: terraform.OrgTypeOAuth,
	})
	gaps := readFile(t, dir, "GAPS.md")
	assertContains(t, "GAPS.md", gaps, "--skip-terraform-managed")
}

// TestGenerate_M2_Pipelines_Standalone_ScheduleTrigger verifies that schedule
// triggers are captured in the tfvars for standalone orgs.
func TestGenerate_M2_Pipelines_Standalone_ScheduleTrigger(t *testing.T) {
	m := loadManifest(t, fixtureManifest) // has a schedule trigger
	dir := generateTo(t, m, terraform.Options{
		DestOrgID:   destOrgID,
		DestOrgType: terraform.OrgTypeStandalone,
	})

	tfvarsData := readFile(t, dir, "migration.auto.tfvars.json")
	var tfvars terraform.TFVarsFile
	if err := json.Unmarshal([]byte(tfvarsData), &tfvars); err != nil {
		t.Fatalf("parsing migration.auto.tfvars.json: %v", err)
	}
	// Find a schedule trigger in the tfvars.
	found := false
	for _, pd := range tfvars.Pipelines {
		for _, trig := range pd.Triggers {
			if trig.EventSourceProvider == "schedule" {
				found = true
				if trig.EventSourceScheduleCronExpression == "" {
					t.Error("schedule trigger cron should not be empty")
				}
			}
		}
	}
	if !found {
		t.Error("expected to find a schedule trigger in standalone tfvars")
	}
}

// ----------------------------------------------------------------------------
// --import-existing
// ----------------------------------------------------------------------------

// TestGenerate_M2_ImportExisting_WithIDs verifies that imports.tf is generated
// when --import-existing and --existing are provided.
func TestGenerate_M2_ImportExisting_WithIDs(t *testing.T) {
	m := loadManifest(t, fixtureManifest)
	existingIDs, err := terraform.LoadExistingIDs(fixtureExisting)
	if err != nil {
		t.Fatalf("LoadExistingIDs: %v", err)
	}
	if existingIDs == nil {
		t.Fatal("expected existing IDs from fixture, got nil")
	}

	dir := generateTo(t, m, terraform.Options{
		DestOrgID:      destOrgID,
		DestOrgType:    terraform.OrgTypeOAuth,
		ImportExisting: true,
		ExistingIDs:    existingIDs,
	})

	if !fileExists(t, dir, "imports.tf") {
		t.Fatal("imports.tf should be generated when --import-existing is set")
	}
	s := readFile(t, dir, "imports.tf")
	assertContains(t, "imports.tf", s, `import {`)
	// Context imports.
	assertContains(t, "imports.tf", s, `circleci_context.contexts`)
	assertContains(t, "imports.tf", s, `deploy-prod`)
	// Project imports.
	assertContains(t, "imports.tf", s, `circleci_project.projects`)
	assertContains(t, "imports.tf", s, `api`)
	// Webhook imports.
	assertContains(t, "imports.tf", s, `circleci_webhook.webhooks`)
	assertContains(t, "imports.tf", s, `notify-slack`)
	// Runner resource class imports.
	assertContains(t, "imports.tf", s, `circleci_runner_resource_class.runners`)
	assertContains(t, "imports.tf", s, `linux-medium`)
}

// TestGenerate_M2_ImportExisting_Disabled verifies imports.tf is NOT generated
// when --import-existing is not set.
func TestGenerate_M2_ImportExisting_Disabled(t *testing.T) {
	m := loadManifest(t, fixtureManifest)
	dir := generateTo(t, m, terraform.Options{
		DestOrgID:   destOrgID,
		DestOrgType: terraform.OrgTypeOAuth,
		// ImportExisting not set (default false)
	})

	if fileExists(t, dir, "imports.tf") {
		t.Error("imports.tf should NOT be generated without --import-existing")
	}
}

// TestGenerate_M2_ImportExisting_NilIDs verifies that when ImportExisting is
// true but ExistingIDs is nil, imports.tf is NOT generated.
func TestGenerate_M2_ImportExisting_NilIDs(t *testing.T) {
	m := loadManifest(t, fixtureManifest)
	dir := generateTo(t, m, terraform.Options{
		DestOrgID:      destOrgID,
		DestOrgType:    terraform.OrgTypeOAuth,
		ImportExisting: true,
		ExistingIDs:    nil, // no IDs provided
	})

	if fileExists(t, dir, "imports.tf") {
		t.Error("imports.tf should NOT be generated when ExistingIDs is nil")
	}
}

// TestLoadExistingIDs_EmptyPath returns nil for empty path.
func TestLoadExistingIDs_EmptyPath(t *testing.T) {
	ids, err := terraform.LoadExistingIDs("")
	if err != nil {
		t.Fatalf("LoadExistingIDs empty path: unexpected error: %v", err)
	}
	if ids != nil {
		t.Error("LoadExistingIDs empty path: expected nil")
	}
}

// TestLoadExistingIDs_MissingSection returns nil (not error) for a file without resource_ids.
func TestLoadExistingIDs_MissingSection(t *testing.T) {
	// Write a minimal sync JSON without resource_ids.
	f, err := os.CreateTemp("", "sync-*.json")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	if _, err := f.WriteString(`{"dry_run":false,"dest_org_slug":"gh/acme","sections":[]}`); err != nil {
		t.Fatal(err)
	}
	f.Close()

	ids, err := terraform.LoadExistingIDs(f.Name())
	if err != nil {
		t.Fatalf("LoadExistingIDs missing section: unexpected error: %v", err)
	}
	if ids != nil {
		t.Errorf("LoadExistingIDs missing section: expected nil, got %+v", ids)
	}
}

// ----------------------------------------------------------------------------
// GAPS.md M2 updates
// ----------------------------------------------------------------------------

// TestGenerate_M2_GAPSmd_OAuth verifies GAPS.md M2 content for OAuth dest.
func TestGenerate_M2_GAPSmd_OAuth(t *testing.T) {
	m := loadManifest(t, fixtureManifest)
	dir := generateTo(t, m, terraform.Options{
		DestOrgID:   destOrgID,
		DestOrgType: terraform.OrgTypeOAuth,
	})

	gaps := readFile(t, dir, "GAPS.md")
	// M2 what-is-managed section.
	assertContains(t, "GAPS.md", gaps, "What Terraform manages (M2)")
	assertContains(t, "GAPS.md", gaps, "circleci_context_restriction")
	assertContains(t, "GAPS.md", gaps, "circleci_webhook")
	assertContains(t, "GAPS.md", gaps, "circleci_runner_resource_class")
	// OAuth: pipeline/trigger gap must be present.
	assertContains(t, "GAPS.md", gaps, "Pipeline definitions and triggers")
	assertContains(t, "GAPS.md", gaps, "OAuth")
	// CLI gap-fill hint.
	assertContains(t, "GAPS.md", gaps, "--skip-terraform-managed")
}

// TestGenerate_M2_GAPSmd_Standalone verifies GAPS.md M2 content for standalone dest.
func TestGenerate_M2_GAPSmd_Standalone(t *testing.T) {
	m := loadManifest(t, fixtureManifestStandalone)
	dir := generateTo(t, m, terraform.Options{
		DestOrgID:   destOrgID,
		DestOrgType: terraform.OrgTypeStandalone,
	})

	gaps := readFile(t, dir, "GAPS.md")
	// M2 what-is-managed section.
	assertContains(t, "GAPS.md", gaps, "What Terraform manages (M2)")
	assertContains(t, "GAPS.md", gaps, "circleci_pipeline")
	assertContains(t, "GAPS.md", gaps, "standalone/App only")
	// Group restriction gap for standalone.
	assertContains(t, "GAPS.md", gaps, "group")
	assertContains(t, "GAPS.md", gaps, "standalone")
	// CLI gap-fill hint.
	assertContains(t, "GAPS.md", gaps, "--skip-terraform-managed")
	// No pipeline/trigger gap for standalone (it's managed).
	assertNotContains(t, "GAPS.md", gaps, "Pipeline definitions and triggers (OAuth org")
}

// ----------------------------------------------------------------------------
// TFVarsFile M2 fields
// ----------------------------------------------------------------------------

// TestGenerate_M2_TFVarsFile_M2Fields verifies all M2 fields in migration.auto.tfvars.json.
func TestGenerate_M2_TFVarsFile_M2Fields(t *testing.T) {
	m := loadManifest(t, fixtureManifest)
	dir := generateTo(t, m, terraform.Options{
		DestOrgID:   destOrgID,
		DestOrgType: terraform.OrgTypeOAuth,
	})

	tfvarsData := readFile(t, dir, "migration.auto.tfvars.json")
	// Restrictions must be present.
	assertContains(t, "migration.auto.tfvars.json", tfvarsData, `"restrictions"`)
	// Webhooks must be present.
	assertContains(t, "migration.auto.tfvars.json", tfvarsData, `"webhooks"`)
	// Runner classes must be present.
	assertContains(t, "migration.auto.tfvars.json", tfvarsData, `"runner_classes"`)
	// Pipelines must NOT be present for OAuth.
	assertNotContains(t, "migration.auto.tfvars.json", tfvarsData, `"pipelines"`)
}

// TestGenerate_M2_TFVarsFile_StandaloneHasPipelines verifies pipelines appear
// in migration.auto.tfvars.json for standalone orgs.
func TestGenerate_M2_TFVarsFile_StandaloneHasPipelines(t *testing.T) {
	m := loadManifest(t, fixtureManifestStandalone)
	dir := generateTo(t, m, terraform.Options{
		DestOrgID:   destOrgID,
		DestOrgType: terraform.OrgTypeStandalone,
	})

	tfvarsData := readFile(t, dir, "migration.auto.tfvars.json")
	assertContains(t, "migration.auto.tfvars.json", tfvarsData, `"pipelines"`)
	assertContains(t, "migration.auto.tfvars.json", tfvarsData, `"runner_classes"`)
	assertContains(t, "migration.auto.tfvars.json", tfvarsData, `"webhooks"`)
	assertContains(t, "migration.auto.tfvars.json", tfvarsData, `"restrictions"`)
}

// ----------------------------------------------------------------------------
// Files emitted for each org type
// ----------------------------------------------------------------------------

// TestGenerate_M2_AllFiles_Standalone verifies all expected files are generated for standalone.
func TestGenerate_M2_AllFiles_Standalone(t *testing.T) {
	m := loadManifest(t, fixtureManifestStandalone)
	dir := generateTo(t, m, terraform.Options{
		DestOrgID:   destOrgID,
		DestOrgType: terraform.OrgTypeStandalone,
	})

	for _, f := range []string{
		"versions.tf", "providers.tf", "contexts.tf", "projects.tf",
		"restrictions.tf", "webhooks.tf", "runners.tf", "pipelines.tf",
		"migration.auto.tfvars.json", "GAPS.md",
	} {
		if !fileExists(t, dir, f) {
			t.Errorf("expected %s to be generated for standalone, but it is missing", f)
		}
	}
}

// TestGenerate_M2_AllFiles_OAuth verifies all expected files are generated for OAuth,
// and that pipelines.tf is absent.
func TestGenerate_M2_AllFiles_OAuth(t *testing.T) {
	m := loadManifest(t, fixtureManifest)
	dir := generateTo(t, m, terraform.Options{
		DestOrgID:   destOrgID,
		DestOrgType: terraform.OrgTypeOAuth,
	})

	for _, f := range []string{
		"versions.tf", "providers.tf", "contexts.tf", "projects.tf",
		"restrictions.tf", "webhooks.tf", "runners.tf",
		"migration.auto.tfvars.json", "GAPS.md",
	} {
		if !fileExists(t, dir, f) {
			t.Errorf("expected %s to be generated for OAuth, but it is missing", f)
		}
	}

	// pipelines.tf must NOT exist for OAuth.
	if fileExists(t, dir, "pipelines.tf") {
		t.Error("pipelines.tf should NOT be generated for OAuth orgs")
	}
}

// TestGenerate_M2_VersionsTF_RequiresTF15 verifies that versions.tf requires >= 1.5
// (needed for import {} blocks introduced in M2).
func TestGenerate_M2_VersionsTF_RequiresTF15(t *testing.T) {
	m := loadManifest(t, fixtureManifest)
	dir := generateTo(t, m, terraform.Options{
		DestOrgID:   destOrgID,
		DestOrgType: terraform.OrgTypeOAuth,
	})

	s := readFile(t, dir, "versions.tf")
	assertContains(t, "versions.tf", s, ">= 1.5")
}

// ----------------------------------------------------------------------------
// helpers
// ----------------------------------------------------------------------------

func startsWith(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
