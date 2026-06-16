package cmd

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/AwesomeCICD/circleci-org-migration-cli/api/org"
	"github.com/AwesomeCICD/circleci-org-migration-cli/api/project"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/capture"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/transfer"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// secretsTransferLong is the long help text for 'secrets transfer'.
// #nosec G101 -- long help text; mentions 'token'/'secret' but contains no credential
const secretsTransferLong = `transfer is a ZERO-DISK-WRITE mode for migrating context env-var VALUES directly
from the SOURCE org to the DESTINATION org without producing any bundle file.

Instead of writing values to a build artifact (as 'secrets capture' does),
'secrets transfer' triggers a single dynamic pipeline in the SOURCE org with
one job per context. Each job imports the source context (CircleCI unmasks
the values into the job environment) and PUTs each value directly into the
matching context in the DESTINATION org via the CircleCI API over TLS.

NO plaintext ever touches disk or build artifacts — strictly better security
than the encrypted-bundle-artifact flow for context variables.

CREATE-MISSING DESTINATION CONTEXTS:
  When a destination context does not exist, the in-pipeline job creates it
  automatically (POST /api/v2/context) before setting env-var values. You no
  longer need to run 'sync --apply' first if you only want to fill values.
  The destination org must already exist; creating contexts within it is safe.

PROJECT ENV-VAR TRANSFER (opt-in with --include-project-vars):
  Pass --include-project-vars to also transfer project-level env-var values.

  IMPORTANT: project env vars are strictly project-scoped — CircleCI only
  injects them when a pipeline runs under that exact project. Therefore,
  'secrets transfer' triggers ONE SEPARATE PIPELINE per source project, each
  under that project's own slug. This is the only correct approach: a single
  shared host-project pipeline would silently produce blank values for every
  project that is not the host project (the bug this design explicitly prevents).

  The destination project must already be onboarded/exist. Resolution of
  source project slug → destination project slug requires an explicit --mapping
  entry (keys containing "/" in the projects map). Projects without a
  resolvable destination slug are SKIPPED and flagged in the plan:

    SKIP project "gh/acme/api": dest project for "gh/acme/api" unknown
    — provide --mapping or onboard it first; skipped

TRIGGER FLAGS (REQUIRED FOR --apply):
  For the in-pipeline unversioned-config trigger to work, BOTH of the following
  flags must be enabled:
    1. Org-level:     allow_api_trigger_with_config  (CircleCI org settings)
    2. Project-level: api-trigger-with-config         (per-project settings)

  'secrets transfer' checks and enables both flags automatically:
    - In interactive mode: prompts before enabling each flag.
    - With --enable-trigger (non-interactive): enables both automatically.
    - Both flags are restored to their prior values after the transfer (defer).

WHEN TO USE:
  - You trust the source org's pipeline infrastructure and want the simplest,
    most secure migration path for context env-var values.
  - Your destination contexts already exist or you want them auto-created.
  - You do NOT need a local copy of the secret values.

WHEN TO USE 'secrets capture' INSTEAD:
  - You need a local bundle for review, backup, or air-gapped flows.
  - You are migrating SSH keys.
  - You want to inspect values before writing them to the destination.

PREREQUISITES:
  1. Run 'export' to produce manifest.json.
  2. Store the DESTINATION org API token in a source-org context, e.g.:
       context name: "migration-secrets"
       env var:       CIRCLECI_DEST_TOKEN = <dest-org-api-token>
     Pass that context name via --dest-token-context.
  3. (Optional) To transfer project env vars, prepare a mapping.json with
     entries for each source project slug → destination project slug.
  4. Run 'secrets transfer --apply' to execute the transfer pipeline.

DRY RUN (default — safe to run without --apply):
  Without --apply, transfer prints a plan: which contexts and variables would
  be transferred, whether each context would be created or updated, and (when
  --include-project-vars is set) per-project resolution status. No pipeline
  is triggered.

  circleci-migrate secrets transfer --manifest manifest.json \
    --dest-org gh/dest-org --dest-token-context migration-secrets

APPLY — execute the transfer:
  circleci-migrate secrets transfer --manifest manifest.json \
    --dest-org gh/dest-org --dest-token-context migration-secrets \
    --enable-trigger --apply

TRUST MODEL & SECURITY:
  The in-pipeline jobs need the destination API token. The CLI does NOT embed
  the token value in the generated config. Instead, you store the dest token
  in a source-org context, and the CLI embeds that context NAME. CircleCI
  injects the token into the job as an environment variable.

  Security implication: any source-org admin who can create pipelines or attach
  contexts to jobs has implicit access to the dest token (the same access they
  have to any other sensitive context in the source org). Mitigate by:
    - Using a scoped API token for the destination (write to contexts only).
    - Rotating the dest token after the transfer is complete.
    - Restricting the source context holding the dest token to the minimum
      projects/pipelines that need it.

  The dest token is referenced in the config ONLY as ${CIRCLECI_DEST_TOKEN}
  (or your custom --dest-token-env-var name). The literal value never appears
  in the generated YAML.

Examples:
  # Dry run — see what would be transferred (no pipeline triggered):
  circleci-migrate secrets transfer --manifest manifest.json \
    --dest-org gh/dest-org \
    --dest-token-context migration-secrets

  # Transfer all contexts with values (requires --apply):
  circleci-migrate secrets transfer --manifest manifest.json \
    --dest-org gh/dest-org \
    --dest-token-context migration-secrets \
    --enable-trigger --apply

  # Transfer contexts and project env vars:
  circleci-migrate secrets transfer --manifest manifest.json \
    --dest-org gh/dest-org \
    --dest-token-context migration-secrets \
    --mapping mapping.json \
    --include-project-vars \
    --apply

  # Transfer specific contexts only:
  circleci-migrate secrets transfer --manifest manifest.json \
    --dest-org gh/dest-org \
    --dest-token-context migration-secrets \
    --context deploy-prod --context shared \
    --apply

  # Custom dest token env-var name:
  circleci-migrate secrets transfer --manifest manifest.json \
    --dest-org gh/dest-org \
    --dest-token-context migration-secrets \
    --dest-token-env-var MY_DEST_API_TOKEN \
    --apply

  # Explicit dest org UUID (alternative to --dest-org slug lookup):
  circleci-migrate secrets transfer --manifest manifest.json \
    --dest-org-id <dest-org-uuid> \
    --dest-token-context migration-secrets \
    --apply

  # Custom dest host (CircleCI Server installations):
  circleci-migrate secrets transfer --manifest manifest.json \
    --dest-org gh/dest-org \
    --dest-token-context migration-secrets \
    --dest-host https://circleci.example.com \
    --apply`

// transferFlags holds the bound flag values for 'secrets transfer'.
type transferFlags struct {
	manifestPath       string
	destOrgSlug        string // --dest-org: slug resolved to UUID at run time
	destOrgID          string // --dest-org-id: explicit UUID override
	destHost           string
	destTokenContext   string
	destTokenEnvVar    string
	contextNames       []string
	mappingPath        string
	hostProjectSlug    string
	branch             string
	apply              bool
	enableTrigger      bool
	includeProjectVars bool
	pollTimeout        time.Duration
}

// newSecretsTransferCommand builds the "secrets transfer" subcommand.
func newSecretsTransferCommand() *cobra.Command {
	tf := &transferFlags{}

	cmd := &cobra.Command{
		Use:   "transfer [--manifest <file>] (--dest-org <slug> | --dest-org-id <uuid>) --dest-token-context <ctx>",
		Short: "Transfer context env-var values directly source→dest via an in-pipeline PUT (no bundle file).",
		Long:  secretsTransferLong,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return tf.run(cmd)
		},
	}
	tf.bind(cmd.Flags())
	return cmd
}

// run executes the transfer orchestration for the bound flags.
func (tf *transferFlags) run(cmd *cobra.Command) error {
	if tf.manifestPath == "" {
		return errors.New("--manifest is required")
	}
	// Exactly one of --dest-org or --dest-org-id must be provided.
	if tf.destOrgSlug == "" && tf.destOrgID == "" {
		return errors.New(
			"--dest-org (CircleCI org slug, e.g. gh/my-org) or --dest-org-id (UUID) is required; " +
				"find the org slug in CircleCI → Organization Settings → Overview")
	}
	if tf.destTokenContext == "" {
		return errors.New("--dest-token-context is required (name of the source-org context that holds the destination API token)")
	}

	m, err := manifest.Load(tf.manifestPath)
	if err != nil {
		return err
	}

	// Resolve the combined mapping from the mapping file (optional).
	// - Context name overrides: keys in Projects WITHOUT "/" are treated as
	//   context name → destination context name.
	// - Project slug overrides: keys in Projects WITH "/" are treated as
	//   source project slug → destination project slug.
	// Both live in the same map[string]string passed to transfer.Options.Mapping.
	var combinedMapping map[string]string
	if tf.mappingPath != "" {
		mapping, merr := manifest.LoadMapping(tf.mappingPath)
		if merr != nil {
			return fmt.Errorf("loading mapping: %w", merr)
		}
		combinedMapping = make(map[string]string, len(mapping.Projects))
		for src, dst := range mapping.Projects {
			combinedMapping[src] = dst
		}
	}

	selectedCtxNames := make(map[string]bool, len(tf.contextNames))
	for _, n := range tf.contextNames {
		selectedCtxNames[n] = true
	}

	cfg := configFromContext(cmd.Context())
	srcToken := cfg.SourceTokenOrDefault()
	if srcToken == "" {
		return noSourceTokenError()
	}

	// Resolve --dest-org slug to UUID when --dest-org-id is not provided.
	destOrgID := tf.destOrgID
	if destOrgID == "" {
		orgClient, oerr := org.NewClient(cfg, cfg.DestTokenOrDefault())
		if oerr != nil {
			// Fall back to source token for the org lookup (user may not have set dest token yet).
			orgClient, oerr = org.NewClient(cfg, srcToken)
			if oerr != nil {
				return fmt.Errorf("creating org client to resolve --dest-org: %w", oerr)
			}
		}
		resolved, rerr := orgClient.ResolveOrgID(cmd.Context(), tf.destOrgSlug)
		if rerr != nil {
			return fmt.Errorf("resolving destination org %q: %w\n"+
				"tip: pass --dest-org-id <uuid> to skip the lookup, or check the slug in CircleCI → Organization Settings → Overview",
				tf.destOrgSlug, rerr)
		}
		destOrgID = resolved
	}

	projClient, err := project.NewClient(cfg, srcToken)
	if err != nil {
		return fmt.Errorf("creating project client: %w", err)
	}

	// ── Org-level trigger flag ───────────────────────────────────────────────
	// Enable allow_api_trigger_with_config on the source org when requested.
	// Restore the prior value after the transfer (best-effort, via defer).
	orgFlagEnabled := false
	if vcsType, orgName, ok := capture.ParseOrgSlug(m.Source.Org.Slug); ok {
		orgClient, oerr := newOrgClientForCapture(cfg, srcToken)
		if oerr != nil {
			fmt.Fprintf(cmd.ErrOrStderr(),
				"WARNING: could not create org client to check org-level trigger flag: %v\n", oerr)
		} else {
			enabled, restoreOrgFn, enErr := maybeEnableOrgTrigger(cmd, orgClient, vcsType, orgName, tf.enableTrigger)
			if enErr != nil {
				return enErr
			}
			if restoreOrgFn != nil {
				defer restoreOrgFn()
			}
			orgFlagEnabled = enabled
		}
	}

	// ── Project-level trigger flags ──────────────────────────────────────────
	// Collect the set of source project slugs that will actually run pipelines:
	//   1. The host project (carries the context-transfer pipeline).
	//   2. Each non-skipped source project (per-project env-var pipelines).
	//
	// We resolve the host project here using the same logic as transfer.Transfer
	// (auto-pick first manifest project when --host-project is unset).
	hostSlug := tf.hostProjectSlug
	if hostSlug == "" && len(m.Projects) > 0 {
		hostSlug = m.Projects[0].Slug
	}

	// Collect project slugs that will need the project-level flag.
	slugsNeedingFlag := collectTransferProjectSlugs(hostSlug, m, combinedMapping, tf.includeProjectVars)

	// Enable the project-level api-trigger-with-config flag for each slug.
	// Deferred restores are collected and run in reverse order at function exit.
	for _, slug := range slugsNeedingFlag {
		restore, pErr := maybeEnableProjectTrigger(cmd, projClient, slug, tf.enableTrigger, orgFlagEnabled)
		if pErr != nil {
			return pErr
		}
		if restore != nil {
			defer restore() //nolint:revive // correct: closure captures slug independently via local copy
		}
	}

	opts := transfer.Options{
		HostProjectSlug:      tf.hostProjectSlug,
		Branch:               tf.branch,
		DestHost:             tf.destHost,
		DestOrgID:            destOrgID,
		DestTokenContext:     tf.destTokenContext,
		DestTokenEnvVar:      tf.destTokenEnvVar,
		SelectedContextNames: selectedCtxNames,
		Mapping:              combinedMapping,
		IncludeProjectVars:   tf.includeProjectVars,
		DryRun:               !tf.apply,
		PollTimeout:          tf.pollTimeout,
		Stdout:               cmd.OutOrStdout(),
		Stderr:               cmd.ErrOrStderr(),
	}

	return transfer.Transfer(cmd.Context(), projClient, m, opts)
}

// collectTransferProjectSlugs returns the deduplicated set of source project
// slugs that will run pipelines during transfer. The host project always runs
// (for the context-transfer pipeline). When includeProjectVars is true, every
// source project that has a resolved destination slug also runs its own
// per-project pipeline.
func collectTransferProjectSlugs(hostSlug string, m *manifest.Manifest, combinedMapping map[string]string, includeProjectVars bool) []string {
	seen := make(map[string]bool)
	var out []string

	add := func(s string) {
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}

	add(hostSlug)

	if includeProjectVars {
		for _, mp := range m.Projects {
			if len(mp.EnvVars) == 0 {
				continue
			}
			// Only add if the project can be resolved to a destination slug.
			if combinedMapping != nil {
				if _, ok := combinedMapping[mp.Slug]; ok {
					add(mp.Slug)
				}
			}
		}
	}
	return out
}

// maybeEnableOrgTrigger checks the org-level allow_api_trigger_with_config flag
// and enables it if needed. It returns (flagWasAlreadyEnabled, restoreFunc, error).
// In interactive mode it prompts before enabling; with enableTrigger=true it
// enables automatically. On refusal it returns an actionable error.
func maybeEnableOrgTrigger(cmd *cobra.Command, mgr capture.OrgFlagManager, vcsType, orgName string, enableTrigger bool) (bool, func(), error) {
	ctx := cmd.Context()
	stderr := cmd.ErrOrStderr()

	flags, err := mgr.GetFeatureFlags(ctx, vcsType, orgName)
	if err != nil {
		fmt.Fprintf(stderr, "WARNING: could not read org-level feature flags for %s/%s: %v — proceeding\n", vcsType, orgName, err)
		return false, nil, nil
	}

	if capture.OrgTriggerAlreadyEnabled(flags) {
		return true, nil, nil // already on; nothing to do
	}

	// Flag is off — decide whether to enable.
	switch {
	case enableTrigger:
		// Non-interactive: auto-enable.
		fmt.Fprintf(stderr, "Enabling org-level allow_api_trigger_with_config for %s/%s…\n", vcsType, orgName)
	case isInteractiveTTY():
		// Interactive: prompt the user.
		p := NewPrompter(os.Stdin, stderr)
		ok, perr := p.askBool(
			fmt.Sprintf("Enable org-level allow_api_trigger_with_config for %s/%s now?", vcsType, orgName),
			true,
		)
		if perr != nil {
			return false, nil, fmt.Errorf("reading prompt: %w", perr)
		}
		if !ok {
			return false, nil, fmt.Errorf(
				"org-level allow_api_trigger_with_config is OFF for %s/%s — cannot trigger pipelines.\n"+
					"Enable it in CircleCI → Organization Settings → Advanced, or re-run with --enable-trigger",
				vcsType, orgName)
		}
		fmt.Fprintf(stderr, "Enabling org-level allow_api_trigger_with_config for %s/%s…\n", vcsType, orgName)
	default:
		// Non-interactive, no --enable-trigger: fail fast.
		return false, nil, fmt.Errorf(
			"org-level allow_api_trigger_with_config is OFF for %s/%s.\n"+
				"Pass --enable-trigger to enable it automatically, or enable it manually in "+
				"CircleCI → Organization Settings → Advanced",
			vcsType, orgName)
	}

	if uerr := mgr.UpdateFeatureFlags(ctx, vcsType, orgName, map[string]bool{capture.OrgAPITriggerKey: true}); uerr != nil {
		fmt.Fprintf(stderr, "WARNING: could not enable org-level flag for %s/%s: %v — proceeding\n", vcsType, orgName, uerr)
		return false, nil, nil
	}

	restore := func() {
		fmt.Fprintf(stderr, "Restoring org-level allow_api_trigger_with_config=false for %s/%s…\n", vcsType, orgName)
		if rerr := mgr.UpdateFeatureFlags(ctx, vcsType, orgName, map[string]bool{capture.OrgAPITriggerKey: false}); rerr != nil {
			fmt.Fprintf(stderr, "WARNING: failed to restore org-level flag for %s/%s: %v\n", vcsType, orgName, rerr)
		}
	}
	return false, restore, nil
}

// maybeEnableProjectTrigger checks the project-level api-trigger-with-config
// flag for slug and enables it if needed. It returns a restore function (or nil
// if no change was made) and any hard error.
//
// enableTrigger: when true, enable automatically (non-interactive).
// orgFlagAlreadyOn: informational — used in actionable error messages.
func maybeEnableProjectTrigger(cmd *cobra.Command, client capture.FlagReaderWriter, slug string, enableTrigger, orgFlagAlreadyOn bool) (func(), error) {
	const apiTriggerKey = "api-trigger-with-config"

	ctx := cmd.Context()
	stderr := cmd.ErrOrStderr()

	flags, err := client.GetV11ProjectFeatureFlags(ctx, slug)
	if err != nil {
		// Non-fatal: warn and continue. The pipeline trigger will fail clearly.
		fmt.Fprintf(stderr, "WARNING: could not read project feature flags for %s: %v — skipping flag check\n", slug, err)
		return nil, nil
	}

	if flags[apiTriggerKey] {
		return nil, nil // already on; nothing to do or restore
	}

	// Flag is off — decide whether to enable.
	switch {
	case enableTrigger:
		fmt.Fprintf(stderr, "Enabling api-trigger-with-config for project %s…\n", slug)
	case isInteractiveTTY():
		p := NewPrompter(os.Stdin, stderr)
		ok, perr := p.askBool(
			fmt.Sprintf("Enable api-trigger-with-config for project %s now?", slug),
			true,
		)
		if perr != nil {
			return nil, fmt.Errorf("reading prompt: %w", perr)
		}
		if !ok {
			return nil, fmt.Errorf(
				"api-trigger-with-config is OFF for project %s — cannot trigger the transfer pipeline.\n"+
					"Enable it in CircleCI → Project Settings → Advanced, or re-run with --enable-trigger",
				slug)
		}
		fmt.Fprintf(stderr, "Enabling api-trigger-with-config for project %s…\n", slug)
	default:
		orgHint := ""
		if !orgFlagAlreadyOn {
			orgHint = " (also ensure org-level allow_api_trigger_with_config is enabled)"
		}
		return nil, fmt.Errorf(
			"api-trigger-with-config is OFF for project %s%s.\n"+
				"Pass --enable-trigger to enable it automatically, or enable it manually in "+
				"CircleCI → Project Settings → Advanced",
			slug, orgHint)
	}

	if serr := client.SetV11ProjectFeatureFlags(ctx, slug, map[string]bool{apiTriggerKey: true}); serr != nil {
		return nil, fmt.Errorf("enabling api-trigger-with-config for project %s: %w", slug, serr)
	}

	restore := func() {
		fmt.Fprintf(stderr, "Restoring api-trigger-with-config=false for project %s…\n", slug)
		if rerr := client.SetV11ProjectFeatureFlags(ctx, slug, map[string]bool{apiTriggerKey: false}); rerr != nil {
			fmt.Fprintf(stderr, "WARNING: failed to restore api-trigger-with-config for project %s: %v\n", slug, rerr)
		}
	}
	return restore, nil
}

// bind registers the transfer flags on f and stores their values in tf.
func (tf *transferFlags) bind(f *pflag.FlagSet) {
	f.StringVar(&tf.manifestPath, "manifest", "", "Path to the export manifest (required)")
	f.StringVar(&tf.destOrgSlug, "dest-org", "",
		"CircleCI organization slug for the destination org, e.g. gh/my-org "+
			"(shown in CircleCI → Organization Settings → Overview). "+
			"This is the CircleCI org identifier, not a GitHub repository URL. "+
			"The CLI resolves it to the org UUID automatically. "+
			"Use --dest-org-id to supply the UUID directly.")
	f.StringVar(&tf.destOrgID, "dest-org-id", "",
		"Destination org UUID (explicit override; alternative to --dest-org). "+
			"Find it in your manifest ('source.org.id') or the CircleCI org settings page.")
	f.StringVar(&tf.destHost, "dest-host", "",
		"Destination CircleCI host URL (default: https://circleci.com; override for Server installs)")
	f.StringVar(&tf.destTokenContext, "dest-token-context", "",
		"Name of the SOURCE-org context that holds the destination API token "+
			"(the env var within that context is set by --dest-token-env-var). "+
			"SECURITY: source-org admins with access to this context can read the token. "+
			"Use a scoped token and rotate it after transfer.")
	f.StringVar(&tf.destTokenEnvVar, "dest-token-env-var", "CIRCLECI_DEST_TOKEN",
		"Name of the env var inside --dest-token-context that holds the destination API token "+
			"(default: CIRCLECI_DEST_TOKEN)")
	f.StringArrayVar(&tf.contextNames, "context", nil,
		"Context name(s) to transfer (default: all contexts with at least one env var in the manifest)")
	f.StringVar(&tf.mappingPath, "mapping", "",
		"Path to mapping.json for context name overrides (optional). Entries in the 'projects' map "+
			"whose keys do not contain '/' are treated as context name → destination name mappings.")
	f.StringVar(&tf.hostProjectSlug, "host-project", "",
		"Source-org project slug under which the context-transfer pipeline runs. "+
			"Any project with api-trigger-with-config enabled works. "+
			"Auto-picked from the manifest when omitted.")
	f.StringVar(&tf.branch, "branch", "main",
		"Branch to check out for the transfer pipeline run")
	f.BoolVar(&tf.apply, "apply", false,
		"Execute the transfer pipeline (default: dry-run — prints the plan but triggers no pipeline). "+
			"Pass --apply to actually write values to the destination org.")
	f.BoolVar(&tf.enableTrigger, "enable-trigger", false,
		"Automatically enable api-trigger-with-config (both org-level and per-project) if not already on, "+
			"and restore the prior values after the transfer completes. "+
			"In interactive mode you are prompted for each flag; in non-interactive mode this flag "+
			"is required when any trigger flag is off.")
	f.BoolVar(&tf.includeProjectVars, "include-project-vars", false,
		"Also transfer project env-var values to the destination projects (default: off, context-only). "+
			"Requires each source project to be resolvable to a destination project slug via --mapping; "+
			"projects without a mapping entry are skipped with a warning. "+
			"Destination project must already be onboarded/exist in the destination org.")
	f.DurationVar(&tf.pollTimeout, "poll-timeout", 30*time.Minute,
		"Maximum time to wait for the transfer pipeline to complete (0 = no timeout)")
}
