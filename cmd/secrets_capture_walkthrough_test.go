package cmd_test

// secrets_capture_walkthrough_test.go exercises the interactive guided
// walkthrough for 'secrets capture' (RunCaptureWalkthroughWith) using
// scripted I/O — no real TTY or API calls required.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AwesomeCICD/circleci-org-migration-cli/cmd"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// walkthroughManifest returns a manifest with two contexts and two projects.
func walkthroughManifest() *manifest.Manifest {
	return &manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		Source: manifest.Source{
			Org: manifest.Org{Slug: "gh/acme", ID: "org-uuid-1"},
		},
		Contexts: []manifest.Context{
			{Name: "deploy-prod", EnvVars: []manifest.ContextEnvVar{{Name: "PROD_TOKEN"}}},
			{Name: "deploy-staging", EnvVars: []manifest.ContextEnvVar{{Name: "STAGING_TOKEN"}}},
		},
		Projects: []manifest.Project{
			{Slug: "gh/acme/web", SourceID: "proj-web-uuid", EnvVars: []manifest.ProjectEnvVar{{Name: "WEB_VAR"}}},
			{Slug: "gh/acme/api", SourceID: "proj-api-uuid", EnvVars: []manifest.ProjectEnvVar{{Name: "API_VAR"}}},
		},
	}
}

// singleProjectManifest returns a manifest with one context and one project.
func singleProjectManifest() *manifest.Manifest {
	return &manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		Source: manifest.Source{
			Org: manifest.Org{Slug: "gh/acme", ID: "org-uuid-1"},
		},
		Contexts: []manifest.Context{
			{Name: "prod-ctx", EnvVars: []manifest.ContextEnvVar{{Name: "SECRET_KEY"}}},
		},
		Projects: []manifest.Project{
			{Slug: "gh/acme/web", SourceID: "proj-web-uuid", EnvVars: []manifest.ProjectEnvVar{{Name: "WEB_VAR"}}},
		},
	}
}

// noContextManifest returns a manifest with only projects, no contexts.
func noContextManifest() *manifest.Manifest {
	return &manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		Source: manifest.Source{
			Org: manifest.Org{Slug: "gh/acme", ID: "org-uuid-1"},
		},
		Projects: []manifest.Project{
			{Slug: "gh/acme/web", SourceID: "proj-web-uuid", EnvVars: []manifest.ProjectEnvVar{{Name: "WEB_VAR"}}},
		},
	}
}

// driveWalkthrough drives RunCaptureWalkthroughWith with scripted input and
// returns the result. The manifest is written to a temp file; manifestPath
// in the initial result must be set by the caller.
func driveWalkthrough(
	t *testing.T,
	m *manifest.Manifest,
	inputLines []string,
) (cmd.CaptureWalkthroughResult, string, error) {
	t.Helper()

	dir := t.TempDir()

	// Write manifest to a temp file so the walkthrough can load it.
	mPath := filepath.Join(dir, "manifest.json")
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	if writeErr := os.WriteFile(mPath, data, 0o644); writeErr != nil {
		t.Fatalf("write manifest: %v", writeErr)
	}

	outPath := filepath.Join(dir, "secrets.json")

	input := strings.Join(inputLines, "\n") + "\n"
	r := strings.NewReader(input)

	root := cmd.MakeCommands()
	var promptBuf strings.Builder
	p := cmd.NewPrompter(r, &promptBuf)

	initial := cmd.CaptureWalkthroughResult{
		ManifestPath: mPath,
		Output:       outPath,
	}
	result, walkthroughErr := cmd.RunCaptureWalkthroughWith(p, root, initial)
	return result, promptBuf.String(), walkthroughErr
}

// ---------------------------------------------------------------------------
// Happy path — encrypt with generate-key, artifact storage, set retention
// ---------------------------------------------------------------------------

// TestCaptureWalkthrough_HappyPath_EncryptGenerateKey verifies the complete
// guided flow: contexts (all), projects (all), auto-pick host project,
// encryption with generated key, artifact storage, set retention to 1 day,
// default branch, enable trigger, confirm.
func TestCaptureWalkthrough_HappyPath_EncryptGenerateKey(t *testing.T) {
	m := singleProjectManifest()

	// Input for single-project, single-context manifest:
	// Step 2: contexts (empty = all), projects (empty = all)
	// Step 3: host project — auto-pick (only 1 project → auto-selected, no prompt)
	// Step 4: encrypt? y → key choice: 1 (generate)
	// Step 5: storage → 1 (artifact, default)
	// Step 6: retention? y
	// Branch: (empty = main)
	// Enable trigger? y
	// Confirm: y
	lines := []string{
		"", // contexts: all (default)
		"", // projects: all (default)
		// host project auto-picked (only 1 project)
		"y", // encrypt? yes
		"1", // key choice: generate
		"1", // storage: artifact (default)
		"y", // set retention to 1 day
		"",  // branch: main (default)
		"y", // enable trigger
		"y", // confirm
	}

	result, _, err := driveWalkthrough(t, m, lines)
	if err != nil {
		t.Fatalf("walkthrough error: %v", err)
	}

	if !result.EncOpts.EncryptEnabled() {
		t.Error("expected EncOpts.encrypt=true")
	}
	if !result.EncOpts.GenerateKey() {
		t.Error("expected EncOpts.generateKey=true")
	}
	if result.ArtifactRetentionDays != 1 {
		t.Errorf("expected ArtifactRetentionDays=1, got %d", result.ArtifactRetentionDays)
	}
	if result.Branch != "main" {
		t.Errorf("expected Branch=main, got %q", result.Branch)
	}
	if !result.EnableTrigger {
		t.Error("expected EnableTrigger=true")
	}
	// Single project → auto-picked as host.
	if result.HostProjectSlug != "gh/acme/web" {
		t.Errorf("expected HostProjectSlug=gh/acme/web (auto-picked), got %q", result.HostProjectSlug)
	}
}

// ---------------------------------------------------------------------------
// Encryption — use existing SSH key
// ---------------------------------------------------------------------------

// TestCaptureWalkthrough_EncryptExistingKey verifies the path where the user
// provides an existing SSH public key path.
func TestCaptureWalkthrough_EncryptExistingKey(t *testing.T) {
	m := noContextManifest() // no contexts → no host project prompt

	lines := []string{
		"", // projects: all (no contexts in manifest)
		// no context or host project prompt
		"y",                     // encrypt? yes
		"2",                     // key choice: existing SSH key
		"~/.ssh/id_ed25519.pub", // pub key path
		"~/.ssh/id_ed25519",     // priv key path (default accepted)
		"1",                     // storage: artifact
		"n",                     // retention: no
		"",                      // branch: main
		"y",                     // enable trigger
		"y",                     // confirm
	}

	result, _, err := driveWalkthrough(t, m, lines)
	if err != nil {
		t.Fatalf("walkthrough error: %v", err)
	}

	if !result.EncOpts.EncryptEnabled() {
		t.Error("expected encrypt=true")
	}
	if result.EncOpts.GenerateKey() {
		t.Error("expected generateKey=false when using existing key")
	}
	if result.EncOpts.SSHPublicKey() != "~/.ssh/id_ed25519.pub" {
		t.Errorf("sshPublicKey=%q, want ~/.ssh/id_ed25519.pub", result.EncOpts.SSHPublicKey())
	}
	if result.ArtifactRetentionDays != 0 {
		t.Errorf("expected ArtifactRetentionDays=0 (user said no), got %d", result.ArtifactRetentionDays)
	}
}

// ---------------------------------------------------------------------------
// No encryption (plaintext)
// ---------------------------------------------------------------------------

// TestCaptureWalkthrough_NoEncrypt verifies the flow when the user declines
// encryption.
func TestCaptureWalkthrough_NoEncrypt(t *testing.T) {
	m := noContextManifest()

	lines := []string{
		"",  // projects: all
		"n", // encrypt? no
		"1", // storage: artifact
		"y", // retention
		"",  // branch: main
		"y", // enable trigger
		"y", // confirm
	}

	result, _, err := driveWalkthrough(t, m, lines)
	if err != nil {
		t.Fatalf("walkthrough error: %v", err)
	}
	if result.EncOpts.EncryptEnabled() {
		t.Error("expected encrypt=false")
	}
}

// ---------------------------------------------------------------------------
// S3 storage
// ---------------------------------------------------------------------------

// TestCaptureWalkthrough_S3Storage verifies the S3 storage branch prompts for
// bucket and prefix.
func TestCaptureWalkthrough_S3Storage(t *testing.T) {
	m := noContextManifest()

	lines := []string{
		"",                    // projects: all
		"n",                   // encrypt? no
		"2",                   // storage: s3
		"my-migration-bucket", // s3 bucket
		"migration/",          // s3 prefix
		"y",                   // retention
		"",                    // branch: main
		"y",                   // enable trigger
		"y",                   // confirm
	}

	result, _, err := driveWalkthrough(t, m, lines)
	if err != nil {
		t.Fatalf("walkthrough error: %v", err)
	}
	if result.EncOpts.Storage() != "s3" {
		t.Errorf("expected storage=s3, got %q", result.EncOpts.Storage())
	}
	if result.EncOpts.S3Bucket() != "my-migration-bucket" {
		t.Errorf("s3Bucket=%q, want my-migration-bucket", result.EncOpts.S3Bucket())
	}
	if result.EncOpts.S3Prefix() != "migration/" {
		t.Errorf("s3Prefix=%q, want migration/", result.EncOpts.S3Prefix())
	}
}

// ---------------------------------------------------------------------------
// Host project selection — multi-project manifest
// ---------------------------------------------------------------------------

// TestCaptureWalkthrough_HostProjectSelection verifies that when multiple
// projects are in the manifest, the user is prompted to choose the host project
// for context extraction.
func TestCaptureWalkthrough_HostProjectSelection(t *testing.T) {
	m := walkthroughManifest() // 2 contexts, 2 projects

	// Step 2: contexts all, projects all
	// Step 3: host project — choose option 3 (gh/acme/api, index 2 in augmented list)
	//   option 1: auto-pick first (gh/acme/web)
	//   option 2: gh/acme/web
	//   option 3: gh/acme/api
	lines := []string{
		"",  // contexts: all
		"",  // projects: all
		"3", // host project: gh/acme/api (3rd option in list)
		"n", // encrypt? no
		"1", // storage: artifact
		"y", // retention
		"",  // branch: main
		"y", // enable trigger
		"y", // confirm
	}

	result, _, err := driveWalkthrough(t, m, lines)
	if err != nil {
		t.Fatalf("walkthrough error: %v", err)
	}
	if result.HostProjectSlug != "gh/acme/api" {
		t.Errorf("HostProjectSlug=%q, want gh/acme/api", result.HostProjectSlug)
	}
}

// TestCaptureWalkthrough_HostProjectAutoPickFirst verifies that choosing option
// 1 (auto-pick) selects the first project from the manifest.
func TestCaptureWalkthrough_HostProjectAutoPickFirst(t *testing.T) {
	m := walkthroughManifest()

	lines := []string{
		"",  // contexts: all
		"",  // projects: all
		"1", // host project: auto-pick first (gh/acme/web)
		"n", // encrypt? no
		"1", // storage: artifact
		"n", // retention: no
		"",  // branch: main
		"n", // enable trigger: no
		"y", // confirm
	}

	result, _, err := driveWalkthrough(t, m, lines)
	if err != nil {
		t.Fatalf("walkthrough error: %v", err)
	}
	if result.HostProjectSlug != "gh/acme/web" {
		t.Errorf("HostProjectSlug=%q, want gh/acme/web (auto-pick first)", result.HostProjectSlug)
	}
	if result.EnableTrigger {
		t.Error("expected EnableTrigger=false when user said no")
	}
}

// ---------------------------------------------------------------------------
// Retention prompt — decline
// ---------------------------------------------------------------------------

// TestCaptureWalkthrough_RetentionDeclined verifies that saying "n" to
// retention leaves ArtifactRetentionDays=0.
func TestCaptureWalkthrough_RetentionDeclined(t *testing.T) {
	m := noContextManifest()

	lines := []string{
		"",  // projects: all
		"n", // encrypt
		"1", // storage: artifact
		"n", // retention: no
		"",  // branch: main
		"y", // enable trigger
		"y", // confirm
	}

	result, _, err := driveWalkthrough(t, m, lines)
	if err != nil {
		t.Fatalf("walkthrough error: %v", err)
	}
	if result.ArtifactRetentionDays != 0 {
		t.Errorf("expected ArtifactRetentionDays=0, got %d", result.ArtifactRetentionDays)
	}
}

// ---------------------------------------------------------------------------
// Cancellation
// ---------------------------------------------------------------------------

// TestCaptureWalkthrough_Cancelled verifies that declining the final
// confirmation returns an error.
func TestCaptureWalkthrough_Cancelled(t *testing.T) {
	m := noContextManifest()

	lines := []string{
		"",  // projects: all
		"n", // encrypt
		"1", // storage: artifact
		"n", // retention
		"",  // branch: main
		"y", // enable trigger
		"n", // CANCEL
	}

	_, _, err := driveWalkthrough(t, m, lines)
	if err == nil {
		t.Fatal("expected cancellation error, got nil")
	}
	if !strings.Contains(err.Error(), "cancelled") {
		t.Errorf("error %q does not mention 'cancelled'", err.Error())
	}
}

// ---------------------------------------------------------------------------
// --no-input flag bypasses walkthrough on non-TTY
// ---------------------------------------------------------------------------

// TestCaptureCmd_NoInput_NoManifest_ReturnsError verifies that --no-input with
// missing --manifest errors immediately without prompting.
func TestCaptureCmd_NoInput_NoManifest_ReturnsError(t *testing.T) {
	t.Setenv("CIRCLECI_CLI_TOKEN", "fake-token")

	_, _, err := runCmd(t, "secrets", "capture", "--no-input")
	if err == nil {
		t.Fatal("expected error with --no-input and missing --manifest")
	}
	if !strings.Contains(err.Error(), "manifest") {
		t.Errorf("error %q does not mention 'manifest'", err.Error())
	}
}

// TestCaptureCmd_NoInputFlag_Registered verifies that --no-input is registered
// on the capture subcommand.
func TestCaptureCmd_NoInputFlag_Registered(t *testing.T) {
	root := MakeTestCommands()
	sub := findSubcommand(root, "secrets", "capture")
	if sub == nil {
		t.Fatal("'secrets capture' subcommand not found")
	}
	if sub.Flags().Lookup("no-input") == nil {
		t.Error("flag --no-input not registered on 'secrets capture'")
	}
}

// TestCaptureCmd_HostProjectFlag_Registered verifies that --host-project is
// registered on the capture subcommand.
func TestCaptureCmd_HostProjectFlag_Registered(t *testing.T) {
	root := MakeTestCommands()
	sub := findSubcommand(root, "secrets", "capture")
	if sub == nil {
		t.Fatal("'secrets capture' subcommand not found")
	}
	if sub.Flags().Lookup("host-project") == nil {
		t.Error("flag --host-project not registered on 'secrets capture'")
	}
}

// ---------------------------------------------------------------------------
// Subset selection
// ---------------------------------------------------------------------------

// TestCaptureWalkthrough_ContextSubset verifies that selecting a subset of
// contexts (item 1 only) results in ContextNames containing only that context.
func TestCaptureWalkthrough_ContextSubset(t *testing.T) {
	m := walkthroughManifest()

	lines := []string{
		"1", // contexts: select item 1 (deploy-prod)
		"1", // projects: select item 1 (gh/acme/web)
		"1", // host project: auto-pick first
		"n", // encrypt
		"1", // storage: artifact
		"n", // retention
		"",  // branch: main
		"y", // enable trigger
		"y", // confirm
	}

	result, _, err := driveWalkthrough(t, m, lines)
	if err != nil {
		t.Fatalf("walkthrough error: %v", err)
	}
	if len(result.ContextNames) != 1 || result.ContextNames[0] != "deploy-prod" {
		t.Errorf("ContextNames=%v, want [deploy-prod]", result.ContextNames)
	}
	if len(result.ProjectSlugs) != 1 || result.ProjectSlugs[0] != "gh/acme/web" {
		t.Errorf("ProjectSlugs=%v, want [gh/acme/web]", result.ProjectSlugs)
	}
}

// ---------------------------------------------------------------------------
// No contexts in manifest — host project step is skipped
// ---------------------------------------------------------------------------

// TestCaptureWalkthrough_NoContextsInManifest verifies that when the manifest
// has no contexts, the host-project step is silently skipped.
func TestCaptureWalkthrough_NoContextsInManifest(t *testing.T) {
	m := noContextManifest()

	// Only project selection + encryption + storage + retention + branch + trigger + confirm.
	lines := []string{
		"", // projects: all
		// no context or host project prompt
		"n", // encrypt
		"1", // storage: artifact
		"n", // retention
		"",  // branch: main
		"y", // enable trigger
		"y", // confirm
	}

	result, _, err := driveWalkthrough(t, m, lines)
	if err != nil {
		t.Fatalf("walkthrough error: %v", err)
	}
	if result.HostProjectSlug != "" {
		t.Errorf("HostProjectSlug should be empty when no contexts; got %q", result.HostProjectSlug)
	}
}

// ---------------------------------------------------------------------------
// Both storage mode
// ---------------------------------------------------------------------------

// TestCaptureWalkthrough_BothStorage verifies the "both" (artifact + S3)
// storage path.
func TestCaptureWalkthrough_BothStorage(t *testing.T) {
	m := noContextManifest()

	lines := []string{
		"",            // projects: all
		"n",           // encrypt
		"3",           // storage: both
		"both-bucket", // s3 bucket
		"both/",       // s3 prefix
		"n",           // retention: no
		"",            // branch: main
		"y",           // enable trigger
		"y",           // confirm
	}

	result, _, err := driveWalkthrough(t, m, lines)
	if err != nil {
		t.Fatalf("walkthrough error: %v", err)
	}
	if result.EncOpts.Storage() != "both" {
		t.Errorf("storage=%q, want both", result.EncOpts.Storage())
	}
	if result.EncOpts.S3Bucket() != "both-bucket" {
		t.Errorf("s3Bucket=%q, want both-bucket", result.EncOpts.S3Bucket())
	}
}

// ---------------------------------------------------------------------------
// Custom branch
// ---------------------------------------------------------------------------

// TestCaptureWalkthrough_CustomBranch verifies that a non-default branch value
// is captured correctly.
func TestCaptureWalkthrough_CustomBranch(t *testing.T) {
	m := noContextManifest()

	lines := []string{
		"",        // projects: all
		"n",       // encrypt
		"1",       // storage: artifact
		"n",       // retention
		"develop", // branch: develop
		"y",       // enable trigger
		"y",       // confirm
	}

	result, _, err := driveWalkthrough(t, m, lines)
	if err != nil {
		t.Fatalf("walkthrough error: %v", err)
	}
	if result.Branch != "develop" {
		t.Errorf("Branch=%q, want develop", result.Branch)
	}
}
