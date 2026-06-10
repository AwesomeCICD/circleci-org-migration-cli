package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	apicontext "github.com/AwesomeCICD/circleci-org-migration-cli/api/context"
	"github.com/AwesomeCICD/circleci-org-migration-cli/api/org"
	"github.com/AwesomeCICD/circleci-org-migration-cli/api/project"
	bundlepkg "github.com/AwesomeCICD/circleci-org-migration-cli/internal/bundle"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/clog"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/extract"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
	"github.com/AwesomeCICD/circleci-org-migration-cli/settings"
	"github.com/AwesomeCICD/circleci-org-migration-cli/version"
	"github.com/spf13/cobra"
)

// flagReaderWriter is the minimal interface the capture command needs to read
// and restore the api-trigger-with-config project feature flag. Injected by
// tests; production uses a real *project.Client.
type flagReaderWriter interface {
	GetV11ProjectFeatureFlags(slug string) (map[string]bool, error)
	SetV11ProjectFeatureFlags(slug string, flags map[string]bool) error
}

// storageRetentionManager reads and writes org-level storage-retention controls
// before the extraction pipeline runs. Injected by tests; production uses a
// real *org.Client which satisfies this interface directly.
type storageRetentionManager interface {
	GetStorageRetention(orgUUID string) (*org.StorageRetention, error)
	SetStorageRetention(orgUUID string, controls org.StorageRetentionControls) error
}

// orgFlagManager reads and writes org-level feature flags via the v1.1
// organization settings endpoint.  Injected by tests; production uses a real
// *org.Client.
type orgFlagManager interface {
	GetFeatureFlags(vcsType, orgName string) (map[string]bool, error)
	UpdateFeatureFlags(vcsType, orgName string, flags map[string]bool) error
}

// pipelineDefLister lists pipeline definitions for a project so the command
// can resolve the first definition's UUID automatically.
type pipelineDefLister interface {
	ListPipelineDefinitions(projectID string) ([]project.PipelineDefinition, error)
}

// projectGetter retrieves project metadata (used to get the project UUID).
type projectGetter interface {
	GetProject(slug string) (*project.Project, error)
}

// contextRestrictionManager manages context restrictions during capture: it can
// list the live restrictions (to get their IDs for deletion) and create or
// delete individual restrictions.  Injected by tests; production uses a real
// *apicontext.Client.
type contextRestrictionManager interface {
	ListRestrictions(contextID string) ([]apicontext.Restriction, error)
	CreateRestriction(contextID, restrictionType, restrictionValue string) error
	DeleteRestriction(contextID, restrictionID string) error
}

// captureClient combines all interfaces the capture command exercises.
type captureClient interface {
	flagReaderWriter
	pipelineDefLister
	projectGetter
	contextRestrictionManager
	extract.Deps
}

const orgApiTriggerKey = "allow_api_trigger_with_config"

// parseOrgSlug converts a manifest org slug into the (vcsType, orgName) pair
// expected by the v1.1 org-settings endpoint.
//
//   - "gh/<org>"        → ("github", "<org>")
//   - "bb/<org>"        → ("bitbucket", "<org>")
//   - "circleci/<uuid>" → ("circleci", "<uuid>")
//
// ok is false when the slug is empty or malformed.
func parseOrgSlug(slug string) (vcsType, orgName string, ok bool) {
	parts := strings.SplitN(slug, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	switch parts[0] {
	case "gh", "github":
		return "github", parts[1], true
	case "bb", "bitbucket":
		return "bitbucket", parts[1], true
	case "circleci":
		return "circleci", parts[1], true
	default:
		return parts[0], parts[1], true
	}
}

// realRestrictions filters out the default "All members" group restriction
// (type=="group" with value==orgID) from the supplied list.  Every App-org
// context has this restriction by default; it is NOT a real access restriction
// — it simply means "all org members".  A context is considered genuinely
// restricted only when at least one non-All-members restriction remains.
func realRestrictions(restrictions []manifest.Restriction, orgID string) []manifest.Restriction {
	out := make([]manifest.Restriction, 0, len(restrictions))
	for _, r := range restrictions {
		if r.Type == "group" && r.Value == orgID {
			// Default "All members" restriction — skip it.
			continue
		}
		out = append(out, r)
	}
	return out
}

const apiTriggerKey = "api-trigger-with-config"

// captureEncryptOpts groups the encryption-related flags for 'secrets capture'.
// They are resolved from flags by newSecretsCaptureCommand and threaded through
// to captureProject / CaptureWithDecrypt.
type captureEncryptOpts struct {
	encrypt       bool
	sshPublicKey  string // path to SSH public key file (recipient)
	sshPrivateKey string // path to SSH private key file (identity for local decrypt)
	generateKey   bool   // generate a fresh X25519 keypair and use it
	recipientStr  string // resolved public key string (after reading file / generating)
	identityFile  string // resolved private key/identity file path
	storage       string // "artifact" | "s3" | "both"
	s3Bucket      string
	s3Prefix      string
}

// newSecretsCaptureCommand builds the "secrets capture" subcommand.
func newSecretsCaptureCommand() *cobra.Command {
	var (
		manifestPath          string
		output                string
		projectSlugs          []string
		contextNames          []string
		branch                string
		enableTrigger         bool
		skipRestrictedCtxs    bool
		removeRestrictions    bool
		pollTimeout           time.Duration
		artifactRetentionDays int
		encOpts               captureEncryptOpts
	)

	cmd := &cobra.Command{
		Use:   "capture --manifest <file>",
		Short: "Capture secret values by running an unversioned pipeline inside CircleCI.",
		Long: `capture extracts plaintext environment-variable values WITHOUT committing
any config to the target project. It:

  1. Reads variable names from the manifest for the selected project(s) and
     context(s).
  2. Ensures api-trigger-with-config is enabled for each project (either it
     must already be on, or --enable-trigger must be set).
  3. Triggers an unversioned pipeline run with an inline config that dumps the
     variable values to a build artifact.
  4. Polls until the pipeline completes, then downloads and parses the artifact.
  5. Writes the captured values into the secret bundle (--output).
  6. Restores the api-trigger-with-config flag to its original value (even on
     failure).

ENCRYPTION (--encrypt):
  When --encrypt is set the in-pipeline extraction job encrypts the artifact
  with age using a public key you supply (--ssh-public-key or an age key via
  the recipient field). The CircleCI artifact is then encrypted — plaintext
  secrets NEVER persist in CircleCI storage.

  After the run, capture downloads the .age artifact and decrypts it locally
  with --ssh-private-key (or the generated key) to build the in-memory bundle.

  Use --generate-key to have capture create a fresh age X25519 keypair
  automatically, print the file paths, and use it for this run.

STORAGE (--storage):
  artifact (default) — store the bundle as a CircleCI job artifact.
  s3                 — upload to S3 only (requires aws CLI + AWS creds in job).
  both               — store in both artifact and S3.

  For S3 storage provide --s3-bucket and (optionally) --s3-prefix.
  The job executor must have AWS credentials via a context or project env vars.

SECURITY NOTES:
  - Without --encrypt: the secret bundle contains plaintext secrets. Protect it.
  - Build artifacts are retained for at least 1 day; there is no delete API.
    With --encrypt the artifact is age-encrypted so plaintext never hits disk.
  - Rotate any captured secrets after migration.

Examples:
  circleci-migrate secrets capture --manifest manifest.json --source-token $TOKEN
  circleci-migrate secrets capture --manifest manifest.json --project gh/acme/web \
    --enable-trigger --branch main -o secrets.json
  # Encrypted capture with auto-generated key:
  circleci-migrate secrets capture --manifest manifest.json --encrypt --generate-key
  # Encrypted capture with existing SSH key:
  circleci-migrate secrets capture --manifest manifest.json --encrypt \
    --ssh-public-key ~/.ssh/id_ed25519.pub --ssh-private-key ~/.ssh/id_ed25519
  # Upload encrypted bundle to S3 instead of artifact:
  circleci-migrate secrets capture --manifest manifest.json --encrypt --generate-key \
    --storage s3 --s3-bucket my-migration-bucket --s3-prefix migration/`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if manifestPath == "" {
				return errors.New("--manifest is required")
			}

			token := rootOptions.SourceTokenOrDefault()
			if token == "" {
				return fmt.Errorf("no API token: set --source-token, --token, CIRCLECI_SOURCE_TOKEN, or CIRCLECI_CLI_TOKEN")
			}

			// ── Resolve encryption options ────────────────────────────────────
			if encOpts.encrypt {
				if err := resolveEncryptOpts(cmd, &encOpts); err != nil {
					return err
				}
			}

			// ── Validate storage flags ────────────────────────────────────────
			if err := validateStorageFlags(&encOpts); err != nil {
				return err
			}

			m, err := manifest.Load(manifestPath)
			if err != nil {
				return err
			}

			bndl, err := loadOrNewBundle(output)
			if err != nil {
				return err
			}

			projClient, err := project.NewClient(rootOptions, token)
			if err != nil {
				return fmt.Errorf("creating project client: %w", err)
			}

			ctxClient, err := newContextClientForCapture(rootOptions, token)
			if err != nil {
				return fmt.Errorf("creating context client: %w", err)
			}

			capClient := &combinedCaptureClient{
				flagReaderWriter:          projClient,
				pipelineDefLister:         projClient,
				projectGetter:             projClient,
				contextRestrictionManager: ctxClient,
				Deps:                      projClient,
			}

			// Resolve the set of projects to process.
			projects := selectProjects(m, projectSlugs)
			if len(projects) == 0 {
				return fmt.Errorf("no projects matched the given selectors (manifest has %d projects)", len(m.Projects))
			}

			// Pre-resolve the set of context names the caller wants to include
			// (empty slice means: include all contexts attached to each project).
			selectedCtxNames := make(map[string]bool, len(contextNames))
			for _, n := range contextNames {
				selectedCtxNames[n] = true
			}

			// ── Org-level allow_api_trigger_with_config ───────────────────────
			// The pipeline/run endpoint also requires the ORG-level flag to be on.
			// We read-and-restore it ONCE before iterating projects (not per-project).
			// On any error we warn and continue — the per-project flag is the primary
			// gate and callers can fix the org flag manually.
			if enableTrigger {
				if vcsType, orgName, ok := parseOrgSlug(m.Source.Org.Slug); ok {
					orgClient, oerr := newOrgClientForCapture(rootOptions, token)
					if oerr != nil {
						fmt.Fprintf(cmd.ErrOrStderr(),
							"WARNING: could not create org client to check org-level flag: %v\n", oerr)
					} else {
						restoreOrgFlag := maybeEnableOrgTriggerFlag(cmd, orgClient, vcsType, orgName)
						defer restoreOrgFlag()
					}
				}
			}

			// ── Artifact-retention safety control ─────────────────────────────
			// When --artifact-retention-days is set, lower the artifact-retention
			// BEFORE triggering the extraction pipeline so that any secrets that
			// land in the build artifact expire quickly.  We deliberately do NOT
			// auto-restore: keeping artifact retention low is the safe default
			// when secrets may be present in artifacts.
			if artifactRetentionDays > 0 && m.Source.Org.ID != "" {
				orgRetentionClient, oerr := newOrgClientForCapture(rootOptions, token)
				if oerr != nil {
					fmt.Fprintf(cmd.ErrOrStderr(),
						"WARNING: could not create org client for artifact-retention control: %v\n", oerr)
				} else {
					applyArtifactRetentionControl(cmd, orgRetentionClient, m.Source.Org.ID, artifactRetentionDays)
				}
			}

			var captureErr error
			for i := range projects {
				p := &projects[i]
				if err := captureProject(
					cmd.Context(),
					cmd,
					capClient,
					m, bndl,
					p,
					selectedCtxNames,
					branch, output,
					enableTrigger, skipRestrictedCtxs, removeRestrictions,
					pollTimeout,
					encOpts,
				); err != nil {
					// Continue processing other projects; record the first error.
					fmt.Fprintf(cmd.ErrOrStderr(), "ERROR capturing project %s: %v\n", p.Slug, err)
					if captureErr == nil {
						captureErr = err
					}
				}
			}

			bndl.GeneratedAt = time.Now().UTC().Format(time.RFC3339)
			bndl.ToolVersion = version.UserAgent()
			if err := bndl.Save(output); err != nil {
				return fmt.Errorf("writing secret bundle: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Secret bundle written to %s\n", output)

			if encOpts.encrypt {
				fmt.Fprintln(cmd.ErrOrStderr(), `
NOTE: --encrypt was set. The CircleCI artifact was age-encrypted.
  Plaintext secrets never persisted in CircleCI artifact storage.
  The local bundle at `+output+` contains plaintext. Protect it.`)
			} else {
				fmt.Fprintln(cmd.ErrOrStderr(), `
WARNING: The secret bundle contains PLAINTEXT secrets.
  - Protect the file; do not commit it to version control.
  - Build artifacts from the extraction run are retained for at least 1 day.
    There is NO delete-artifact API. Rotate captured secrets and treat the
    artifact as sensitive until it expires.`)
			}

			return captureErr
		},
	}

	f := cmd.Flags()
	f.StringVar(&manifestPath, "manifest", "", "Path to the export manifest (required)")
	f.StringVarP(&output, "output", "o", "secrets.json", "Path to the secret bundle to write/append")
	f.StringArrayVar(&projectSlugs, "project", nil, "Project slug(s) to capture (default: all in manifest)")
	f.StringArrayVar(&contextNames, "context", nil, "Context name(s) to capture for each project (default: all attached)")
	f.StringVar(&branch, "branch", "main", "Branch to check out for the extraction run")
	f.BoolVar(&enableTrigger, "enable-trigger", false,
		"Enable api-trigger-with-config if not already on, and restore after capture")
	f.BoolVar(&skipRestrictedCtxs, "skip-restricted-contexts", true,
		"Skip contexts that have project/expression/group restrictions (attach warning instead of attempting)")
	f.BoolVar(&removeRestrictions, "remove-restrictions", false,
		"Temporarily remove real context restrictions before extraction and restore them afterwards (requires explicit opt-in)")
	f.DurationVar(&pollTimeout, "poll-timeout", 10*time.Minute,
		"Maximum time to wait for each pipeline to complete (0 = no timeout)")
	f.IntVar(&artifactRetentionDays, "artifact-retention-days", 0,
		"Set the org's artifact-retention to this many days BEFORE triggering the extraction pipeline. "+
			"Recommended value: 1 (the minimum). Default 0 = leave unchanged. "+
			"The prior value is logged so you can restore it manually. "+
			"This control is NOT auto-restored after capture — keeping retention low is the safe default "+
			"when secrets may land in artifacts.")

	// ── Encryption flags ──────────────────────────────────────────────────────
	f.BoolVar(&encOpts.encrypt, "encrypt", false,
		"Encrypt the in-pipeline artifact with age so plaintext secrets never persist in CircleCI. "+
			"Requires --ssh-public-key or --generate-key.")
	f.StringVar(&encOpts.sshPublicKey, "ssh-public-key", "",
		"Path to an SSH public key (.pub) or age recipients file used as the encryption recipient. "+
			"The public key is safe to embed in the pipeline config.")
	f.StringVar(&encOpts.sshPrivateKey, "ssh-private-key", "",
		"Path to an SSH private key or age identity file used to decrypt the artifact locally. "+
			"Defaults to ~/.ssh/id_ed25519 if present and --ssh-public-key points to the matching .pub.")
	f.BoolVar(&encOpts.generateKey, "generate-key", false,
		"Generate a fresh age X25519 keypair for this run. Writes the identity to "+
			"./migration-identity.age and the recipient to ./migration-recipient.txt. "+
			"Use --generate-key instead of --ssh-public-key when you do not have an existing key.")

	// ── Storage flags ─────────────────────────────────────────────────────────
	f.StringVar(&encOpts.storage, "storage", "artifact",
		`Where to store the (optionally encrypted) bundle after extraction.
artifact (default) — store as a CircleCI job artifact.
s3                 — upload to S3 via the aws CLI (requires AWS creds in job).
both               — store in both artifact and S3.`)
	f.StringVar(&encOpts.s3Bucket, "s3-bucket", "",
		"S3 bucket name for --storage s3|both (required when --storage s3 or both)")
	f.StringVar(&encOpts.s3Prefix, "s3-prefix", "",
		"S3 key prefix for --storage s3|both (optional; e.g. 'migration/')")

	return cmd
}

// resolveEncryptOpts resolves the encryption recipient string from flags:
//   - --generate-key:      generate a fresh age X25519 keypair, write files.
//   - --ssh-public-key:    read the public key from the file.
//
// On success encOpts.recipientStr and encOpts.identityFile are populated.
// SECURITY: private key material is written to file only; never logged.
func resolveEncryptOpts(cmd *cobra.Command, encOpts *captureEncryptOpts) error {
	if encOpts.generateKey && encOpts.sshPublicKey != "" {
		return errors.New("--generate-key and --ssh-public-key are mutually exclusive")
	}
	if !encOpts.generateKey && encOpts.sshPublicKey == "" {
		return errors.New("--encrypt requires --ssh-public-key <path> or --generate-key")
	}

	if encOpts.generateKey {
		// Generate a fresh age X25519 keypair.
		_, idStr, recipStr, err := bundlepkg.GenerateX25519Keypair()
		if err != nil {
			return fmt.Errorf("generating keypair: %w", err)
		}
		idFile := "migration-identity.age"
		recipFile := "migration-recipient.txt"
		// SECURITY: write identity (private key) to 0600 file; do not log.
		if err := writeSecretFile(idFile, idStr+"\n"); err != nil {
			return fmt.Errorf("writing identity file: %w", err)
		}
		if err := writeFile(recipFile, recipStr+"\n"); err != nil {
			return fmt.Errorf("writing recipient file: %w", err)
		}
		fmt.Fprintf(cmd.OutOrStdout(),
			"Generated age X25519 keypair:\n"+
				"  Identity (private key): %s  ← keep secret, needed for decrypt\n"+
				"  Recipient (public key): %s  ← safe to share\n",
			idFile, recipFile)
		encOpts.recipientStr = recipStr
		encOpts.identityFile = idFile
		return nil
	}

	// Read from --ssh-public-key file.
	_, parseErr := bundlepkg.ParseRecipientFile(encOpts.sshPublicKey)
	if parseErr != nil {
		return fmt.Errorf("parsing --ssh-public-key %s: %w", encOpts.sshPublicKey, parseErr)
	}
	// Read raw key string for embedding in config.
	data, err := readFirstKeyLine(encOpts.sshPublicKey)
	if err != nil {
		return fmt.Errorf("reading --ssh-public-key %s: %w", encOpts.sshPublicKey, err)
	}
	encOpts.recipientStr = data

	// Determine identity file for local decryption.
	if encOpts.sshPrivateKey != "" {
		encOpts.identityFile = encOpts.sshPrivateKey
	} else {
		// Default to ~/.ssh/id_ed25519 if it exists.
		home, herr := os.UserHomeDir()
		if herr == nil {
			candidate := home + "/.ssh/id_ed25519"
			if _, serr := os.Stat(candidate); serr == nil {
				encOpts.identityFile = candidate
				clog.Infof("capture: using default SSH identity %s for local decryption", candidate)
			}
		}
	}
	return nil
}

// validateStorageFlags returns an error if the storage-related flags are
// inconsistent (e.g. s3 storage requested but no bucket given).
func validateStorageFlags(encOpts *captureEncryptOpts) error {
	switch encOpts.storage {
	case "artifact", "":
		return nil
	case "s3", "both":
		if encOpts.s3Bucket == "" {
			return fmt.Errorf("--storage %s requires --s3-bucket", encOpts.storage)
		}
		return nil
	default:
		return fmt.Errorf("--storage must be one of: artifact, s3, both (got %q)", encOpts.storage)
	}
}

// writeSecretFile writes data to path with 0600 permissions (private key).
func writeSecretFile(path, data string) error {
	return os.WriteFile(path, []byte(data), 0o600)
}

// writeFile writes data to path with 0644 permissions (public key / recipient).
func writeFile(path, data string) error {
	return os.WriteFile(path, []byte(data), 0o644)
}

// readFirstKeyLine returns the first non-empty, non-comment line from path —
// suitable for extracting the key string from a .pub or age recipients file.
func readFirstKeyLine(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		return line, nil
	}
	return "", fmt.Errorf("no key found in %s", path)
}

// selectProjects returns the manifest projects matching slugs.  If slugs is
// empty all manifest projects are returned.
func selectProjects(m *manifest.Manifest, slugs []string) []manifest.Project {
	if len(slugs) == 0 {
		return m.Projects
	}
	want := make(map[string]bool, len(slugs))
	for _, s := range slugs {
		want[s] = true
	}
	var out []manifest.Project
	for _, p := range m.Projects {
		if want[p.Slug] {
			out = append(out, p)
		}
	}
	return out
}

// captureProject handles the full capture flow for a single manifest project.
// It restores the api-trigger-with-config flag even if capture fails.
func captureProject(
	ctx context.Context,
	cmd *cobra.Command,
	client captureClient,
	m *manifest.Manifest,
	bundle *manifest.SecretBundle,
	p *manifest.Project,
	selectedCtxNames map[string]bool,
	branch, output string,
	enableTrigger, skipRestricted, removeRestrictions bool,
	pollTimeout time.Duration,
	encOpts captureEncryptOpts,
) error {
	stderr := cmd.ErrOrStderr()
	stdout := cmd.OutOrStdout()

	// ── 1. Ensure api-trigger-with-config ────────────────────────────────────
	flags, err := client.GetV11ProjectFeatureFlags(p.Slug)
	if err != nil {
		return fmt.Errorf("read feature flags for %s: %w", p.Slug, err)
	}

	wasEnabled := flags[apiTriggerKey]

	if !wasEnabled {
		if !enableTrigger {
			return fmt.Errorf(
				"project %s has api-trigger-with-config disabled; "+
					"set --enable-trigger to enable it automatically "+
					"(both the org-level allow_api_trigger_with_config AND the project-level "+
					"api-trigger-with-config flags must be on for unversioned-config pipelines)",
				p.Slug,
			)
		}
		fmt.Fprintf(stderr, "Enabling api-trigger-with-config for %s…\n", p.Slug)
		if err := client.SetV11ProjectFeatureFlags(p.Slug, map[string]bool{"api-trigger-with-config": true}); err != nil {
			return fmt.Errorf("enable api-trigger-with-config for %s: %w", p.Slug, err)
		}
	}

	// Defer restoration so it runs even on error.
	defer func() {
		if !wasEnabled && enableTrigger {
			fmt.Fprintf(stderr, "Restoring api-trigger-with-config=false for %s…\n", p.Slug)
			if restoreErr := client.SetV11ProjectFeatureFlags(p.Slug, map[string]bool{"api-trigger-with-config": false}); restoreErr != nil {
				fmt.Fprintf(stderr, "WARNING: failed to restore api-trigger-with-config for %s: %v\n", p.Slug, restoreErr)
			}
		}
	}()

	// ── 2. Resolve pipeline definition ID ────────────────────────────────────
	proj, err := client.GetProject(p.Slug)
	if err != nil {
		return fmt.Errorf("get project %s: %w", p.Slug, err)
	}

	defs, err := client.ListPipelineDefinitions(proj.ID)
	if err != nil {
		return fmt.Errorf("list pipeline definitions for %s: %w", p.Slug, err)
	}
	if len(defs) == 0 {
		return fmt.Errorf("project %s has no pipeline definitions — is it a GitHub App project?", p.Slug)
	}
	defID := defs[0].ID

	// ── 3. Build var name list and context list ───────────────────────────────
	// Project env var names.
	var allVarNames []string
	for _, ev := range p.EnvVars {
		allVarNames = append(allVarNames, ev.Name)
	}

	// Contexts attached to this project (inferred by matching the manifest
	// contexts against the project — the manifest doesn't record explicit
	// project↔context links so we use the selectedCtxNames filter or all).
	var ctxNamesForRun []string

	for i := range m.Contexts {
		mc := &m.Contexts[i]

		// Apply context filter if the caller passed --context flags.
		if len(selectedCtxNames) > 0 && !selectedCtxNames[mc.Name] {
			continue
		}

		// Warn about and optionally skip genuinely restricted contexts.
		// The default "All members" group restriction (type==group, value==orgID)
		// is not a real restriction — every App-org context has it automatically.
		real := realRestrictions(mc.Restrictions, m.Source.Org.ID)
		if len(real) > 0 {
			if removeRestrictions {
				// Temporarily remove restrictions so the extraction run can access
				// the context, then restore from the manifest (source of truth).
				// The restore func is deferred so it runs even on error or panic.
				restore, prepErr := prepareRestrictionRemoval(cmd, client, mc)
				if prepErr != nil {
					return prepErr
				}
				defer restore()
			} else {
				fmt.Fprintf(stderr,
					"WARNING: context %q has restrictions (%d). The extraction job may not "+
						"have access to it. Auto-toggling restrictions is not supported; handle "+
						"manually if needed.\n",
					mc.Name, len(real),
				)
				if skipRestricted {
					fmt.Fprintf(stderr, "Skipping restricted context %q (--skip-restricted-contexts=true).\n", mc.Name)
					continue
				}
			}
		}

		ctxNamesForRun = append(ctxNamesForRun, mc.Name)
		for _, ev := range mc.EnvVars {
			allVarNames = append(allVarNames, ev.Name)
		}
	}

	// De-duplicate var names (a context var may shadow a project var with the
	// same name).
	allVarNames = dedupe(allVarNames)

	fmt.Fprintf(stdout, "Capturing %d variable(s) for project %s (contexts: %v)…\n",
		len(allVarNames), p.Slug, ctxNamesForRun)

	// ── 4. Run Capture ────────────────────────────────────────────────────────
	opts := extract.Options{
		DefinitionID:     defID,
		Branch:           branch,
		PollTimeout:      pollTimeout,
		EncryptRecipient: encOpts.recipientStr,
		Storage:          extract.StorageMode(encOpts.storage),
		S3Bucket:         encOpts.s3Bucket,
		S3Prefix:         encOpts.s3Prefix,
	}

	// SECURITY: encOpts.identityFile is a private key path — do not log.
	values, err := extract.CaptureWithDecrypt(ctx, client, p.Slug, allVarNames, ctxNamesForRun, opts, encOpts.identityFile)
	if err != nil {
		return fmt.Errorf("capture for %s: %w", p.Slug, err)
	}

	// ── 5. Store in bundle ────────────────────────────────────────────────────
	// Project vars.
	for _, ev := range p.EnvVars {
		if v, ok := values[ev.Name]; ok {
			bundle.SetProjectSecret(p.Slug, ev.Name, v)
		}
	}
	// Context vars.
	for i := range m.Contexts {
		mc := &m.Contexts[i]
		// Only store contexts we actually attached.
		included := false
		for _, n := range ctxNamesForRun {
			if n == mc.Name {
				included = true
				break
			}
		}
		if !included {
			continue
		}
		for _, ev := range mc.EnvVars {
			if v, ok := values[ev.Name]; ok {
				bundle.SetContextSecret(mc.Name, ev.Name, v)
			}
		}
	}

	// Write incrementally so a mid-loop failure still saves what was captured.
	bundle.GeneratedAt = time.Now().UTC().Format(time.RFC3339)
	bundle.ToolVersion = version.UserAgent()
	if err := bundle.Save(output); err != nil {
		return fmt.Errorf("saving bundle after project %s: %w", p.Slug, err)
	}

	fmt.Fprintf(stdout, "Captured %d variable(s) for %s\n", len(values), p.Slug)
	return nil
}

// dedupe returns a copy of s with duplicate entries removed (first occurrence
// wins), preserving order.
func dedupe(s []string) []string {
	seen := make(map[string]bool, len(s))
	out := make([]string, 0, len(s))
	for _, v := range s {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

// newOrgClientForCapture creates an *org.Client for reading and writing org
// feature flags during the capture flow.
func newOrgClientForCapture(cfg *settings.Config, token string) (*org.Client, error) {
	c, err := org.NewClient(cfg, token)
	if err != nil {
		return nil, fmt.Errorf("creating org client: %w", err)
	}
	return c, nil
}

// newContextClientForCapture creates an *apicontext.Client for managing
// context restrictions during the capture flow.
func newContextClientForCapture(cfg *settings.Config, token string) (*apicontext.Client, error) {
	c, err := apicontext.NewClient(cfg, token)
	if err != nil {
		return nil, fmt.Errorf("creating context client: %w", err)
	}
	return c, nil
}

// combinedCaptureClient wires a project client (flagReaderWriter,
// pipelineDefLister, projectGetter, extract.Deps) together with a separate
// context client (contextRestrictionManager) into the single captureClient
// interface that captureProject expects.
type combinedCaptureClient struct {
	flagReaderWriter
	pipelineDefLister
	projectGetter
	contextRestrictionManager
	extract.Deps
}

// prepareRestrictionRemoval fetches live restriction IDs for mc and deletes
// every real restriction so the extraction run can access the context.  It
// returns a restore function that the caller MUST immediately defer — calling
// it re-creates the restrictions from mc.Restrictions (the exported manifest
// state, the source of truth).  The restore runs even if extraction later fails
// or panics.
//
// If any DELETE fails, the error is returned and no restore func is registered
// (nothing was removed yet, nothing needs restoring).
// If a RESTORE fails, a prominent WARNING is printed naming exactly which
// restriction (context, type, value) must be manually re-added.
func prepareRestrictionRemoval(cmd *cobra.Command, client contextRestrictionManager, mc *manifest.Context) (restoreFn func(), err error) {
	stderr := cmd.ErrOrStderr()

	// Fetch live restrictions to get their IDs for deletion.
	live, listErr := client.ListRestrictions(mc.SourceID)
	if listErr != nil {
		return func() {}, fmt.Errorf("listing live restrictions for context %q: %w", mc.Name, listErr)
	}

	// The manifest's recorded restrictions are our restore source of truth.
	// Even if the live state is unexpected we restore to the exported baseline.
	restoreFrom := make([]manifest.Restriction, len(mc.Restrictions))
	copy(restoreFrom, mc.Restrictions)

	fmt.Fprintf(stderr,
		"NOTICE: temporarily removing %d restriction(s) from context %q for extraction.\n",
		len(live), mc.Name,
	)
	for _, lr := range live {
		if delErr := client.DeleteRestriction(mc.SourceID, lr.ID); delErr != nil {
			return func() {}, fmt.Errorf("deleting restriction %q from context %q: %w", lr.ID, mc.Name, delErr)
		}
	}

	// Build the restore closure.  It re-creates every restriction recorded in
	// the manifest; the manifest state is the source of truth.
	restore := func() {
		fmt.Fprintf(stderr,
			"NOTICE: restoring %d restriction(s) on context %q.\n",
			len(restoreFrom), mc.Name,
		)
		for _, r := range restoreFrom {
			if createErr := client.CreateRestriction(mc.SourceID, r.Type, r.Value); createErr != nil {
				fmt.Fprintf(stderr,
					"WARNING: failed to restore restriction on context %q "+
						"(type=%q value=%q): %v — you must re-add this restriction manually.\n",
					mc.Name, r.Type, r.Value, createErr,
				)
			}
		}
	}
	return restore, nil
}

// maybeEnableOrgTriggerFlag reads the org-level allow_api_trigger_with_config
// flag.  If it is off, it enables it and returns a restore func that must be
// called (typically via defer) to set it back to false.  If it was already on,
// the restore func is a no-op.  On read failure the error is treated as
// best-effort: a WARNING is printed and a no-op restore func is returned so
// the caller can proceed; the per-project flag is the primary gate.
func maybeEnableOrgTriggerFlag(cmd *cobra.Command, mgr orgFlagManager, vcsType, orgName string) func() {
	stderr := cmd.ErrOrStderr()

	flags, err := mgr.GetFeatureFlags(vcsType, orgName)
	if err != nil {
		fmt.Fprintf(stderr,
			"WARNING: could not read org-level feature flags (%s/%s): %v — proceeding\n",
			vcsType, orgName, err)
		return func() {} // no-op restore
	}

	wasEnabled := flags[orgApiTriggerKey]
	if wasEnabled {
		return func() {} // already on, nothing to restore
	}

	fmt.Fprintf(stderr, "Enabling org-level allow_api_trigger_with_config for %s/%s…\n", vcsType, orgName)
	if uerr := mgr.UpdateFeatureFlags(vcsType, orgName, map[string]bool{orgApiTriggerKey: true}); uerr != nil {
		fmt.Fprintf(stderr,
			"WARNING: could not enable org-level allow_api_trigger_with_config for %s/%s: %v — proceeding\n",
			vcsType, orgName, uerr)
		return func() {} // failed to enable, nothing to restore
	}

	// Return a restore func that the caller must defer.
	return func() {
		fmt.Fprintf(stderr, "Restoring org-level allow_api_trigger_with_config=false for %s/%s…\n", vcsType, orgName)
		if rerr := mgr.UpdateFeatureFlags(vcsType, orgName, map[string]bool{orgApiTriggerKey: false}); rerr != nil {
			fmt.Fprintf(stderr,
				"WARNING: failed to restore org-level allow_api_trigger_with_config for %s/%s: %v\n",
				vcsType, orgName, rerr)
		}
	}
}

// applyArtifactRetentionControl reads the current org storage-retention controls,
// then sets artifact retention to targetDays (keeping cache/workspace unchanged).
//
// The prior artifact-retention value is logged via clog so the operator knows
// what value to restore if needed. This function deliberately does NOT
// auto-restore: keeping artifact retention low is the safe default when
// secrets may be present in pipeline artifacts (there is no delete-artifact API).
//
// A clear note is printed to stderr with the prior value and restore instructions.
func applyArtifactRetentionControl(cmd *cobra.Command, mgr storageRetentionManager, orgUUID string, targetDays int) {
	stderr := cmd.ErrOrStderr()

	current, err := mgr.GetStorageRetention(orgUUID)
	if err != nil {
		fmt.Fprintf(stderr,
			"WARNING: could not read current artifact-retention for org %s: %v — skipping retention control\n",
			orgUUID, err)
		clog.Warnf("applyArtifactRetentionControl: GetStorageRetention(%s): %v", orgUUID, err)
		return
	}

	priorDays := current.Controls.ArtifactDays
	clog.Infof("artifact-retention safety: current artifact_days=%d, setting to %d for org %s",
		priorDays, targetDays, orgUUID)

	newControls := org.StorageRetentionControls{
		CacheDays:     current.Controls.CacheDays,
		WorkspaceDays: current.Controls.WorkspaceDays,
		ArtifactDays:  targetDays,
	}
	if err := mgr.SetStorageRetention(orgUUID, newControls); err != nil {
		fmt.Fprintf(stderr,
			"WARNING: could not set artifact-retention to %d days for org %s: %v — skipping retention control\n",
			targetDays, orgUUID, err)
		clog.Warnf("applyArtifactRetentionControl: SetStorageRetention(%s): %v", orgUUID, err)
		return
	}

	clog.Infof("artifact-retention set to %d day(s) for org %s (was %d)", targetDays, orgUUID, priorDays)
	fmt.Fprintf(stderr,
		"NOTICE: artifact-retention set to %d day(s) for org %s (was %d day(s)).\n"+
			"  Secrets landing in build artifacts will expire sooner.\n"+
			"  This value is NOT auto-restored. To restore, run:\n"+
			"    POST https://app.circleci.com/private/orgs/%s/storage-retention-controls\n"+
			"    body: {\"retention_days_artifact\":%d,\"retention_days_cache\":%d,\"retention_days_workspace\":%d}\n",
		targetDays, orgUUID, priorDays,
		orgUUID, priorDays, current.Controls.CacheDays, current.Controls.WorkspaceDays,
	)
}
