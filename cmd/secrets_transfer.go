package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	apicontext "github.com/AwesomeCICD/circleci-org-migration-cli/api/context"
	"github.com/AwesomeCICD/circleci-org-migration-cli/api/org"
	"github.com/AwesomeCICD/circleci-org-migration-cli/api/project"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/capture"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/transfer"
	"github.com/AwesomeCICD/circleci-org-migration-cli/settings"
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

ADDITIONAL SSH KEY TRANSFER (opt-in with --include-ssh-keys):
  Pass --include-ssh-keys to transfer additional project SSH keys in-pipeline
  without writing private key material to disk or artifacts.  The in-pipeline
  job uses add_ssh_keys to materialize each key, matches it by SHA256 fingerprint,
  reads the private key VERBATIM with jq --rawfile (never echoed), and POSTs it
  to the destination project via POST /api/v1.1/project/{slug}/ssh-key.
  A project with SSH keys but no env vars still triggers a per-project pipeline.

  IMPORTANT: project env vars are strictly project-scoped — CircleCI only
  injects them when a pipeline runs under that exact project. Therefore,
  'secrets transfer' triggers ONE SEPARATE PIPELINE per source project, each
  under that project's own slug. This is the only correct approach: a single
  shared host-project pipeline would silently produce blank values for every
  project that is not the host project (the bug this design explicitly prevents).

  The destination project must already exist. Source project slug →
  destination project slug is resolved from an explicit --mapping entry (keys
  containing "/" in the projects map) first. For a GitHub App ("circleci/")
  destination, a project with no mapping entry is matched BY NAME against the
  destination org's projects (the real circleci/<org-uuid>/<project-uuid> slug
  is used), so 'mapping generate' is optional; the plan prints each source →
  destination pair. Create the destination projects first with
  'sync --apply --skip-pipelines' (it makes projects without pipeline
  definitions, so the GitHub App can stay installed on the source until the
  transfer has run). A project with no mapping entry and no match is SKIPPED,
  flagged in the plan, and the run exits non-zero naming it:

    SKIP project "gh/acme/api": no destination project named "api" in the
    destination org and no mapping entry for "gh/acme/api" — create it first …

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
  3. (Optional) To transfer project env vars into a gh/ or bb/ destination,
     prepare a mapping.json with an entry per source project slug → destination
     project slug. A GitHub App destination matches projects by name instead.
  4. GitHub OAuth (gh/) and Bitbucket (bb/) source projects: CircleCI refuses to
     start a pipeline that checks out over SSH on a project with no CHECKOUT KEY
     ("the project has no SSH key"), even with inline config. In a GitHub OAuth org
     that also has the CircleCI GitHub App installed, each project also has GitHub
     App pipeline definitions, which check out with the app token and need no key;
     transfer prefers those automatically (the log says which definition ran).
     So a project needs a key only if it has NO GitHub App pipeline definition:
     add a deploy key under Project Settings → SSH Keys → Checkout SSH keys, or
     give it an App definition. OAuth triggers may stay disabled. circleci/
     projects are not affected. This is checked BEFORE any context restriction is
     removed or trigger flag enabled: the auto-picked host is the first manifest
     project with a GitHub App definition, else one with a pipeline definition and
     a checkout key; an explicit --host-project with neither is rejected; a
     per-project transfer with neither is shown as SKIP, skipped on --apply, and
     the command exits non-zero listing it. Keys come from the manifest's
     checkout_keys, or are looked up live when the manifest was exported with
     --skip-extras. NOTE: once the GitHub App is uninstalled from the org its
     App definitions are gone, so project-var / SSH-key transfers then need
     checkout keys; contexts transferred before the uninstall do not.
  5. Contexts already transferred? Pass --skip-contexts with --include-project-vars
     and/or --include-ssh-keys to run only the per-project pipelines (no host
     project, no restriction removal, no context pipeline).
  6. Run 'secrets transfer --apply' to execute the transfer pipeline.

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

  # Contexts already transferred — project vars and SSH keys only:
  circleci-migrate secrets transfer --manifest manifest.json \
    --dest-org gh/dest-org \
    --dest-token-context migration-secrets \
    --mapping mapping.json \
    --skip-contexts --include-project-vars --include-ssh-keys \
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
	includeSSHKeys     bool
	skipContexts       bool
	removeRestrictions bool
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

	if tf.skipContexts {
		if len(tf.contextNames) > 0 {
			return errors.New("--skip-contexts cannot be combined with --context (it drops every context)")
		}
		if !tf.includeProjectVars && !tf.includeSSHKeys {
			return errors.New("--skip-contexts leaves nothing to transfer: also pass --include-project-vars and/or --include-ssh-keys")
		}
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
	orgClient, oerr := org.NewClient(cfg, cfg.DestTokenOrDefault())
	if oerr != nil {
		// Fall back to source token for the org lookup (user may not have set dest token yet).
		orgClient, oerr = org.NewClient(cfg, srcToken)
		if oerr != nil {
			return fmt.Errorf("creating org client to resolve the destination org: %w", oerr)
		}
	}
	if destOrgID == "" {
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

	// Build the context client for restriction management when --remove-restrictions is set.
	var ctxClient transfer.ContextRestrictionManager
	if tf.removeRestrictions && !tf.skipContexts { // no contexts → no restrictions to lift
		cc, ccErr := apicontext.NewClient(cfg, srcToken)
		if ccErr != nil {
			return fmt.Errorf("creating context client for --remove-restrictions: %w", ccErr)
		}
		ctxClient = cc
	}

	// GitHub App destinations: projects with no mapping entry are resolved by
	// name from the destination org's project list (mapping generate is optional).
	var destProjects transfer.DestProjectLister
	if (tf.includeProjectVars || tf.includeSSHKeys) && isAppDestOrg(cmd.Context(), orgClient, tf.destOrgSlug, destOrgID, cmd.ErrOrStderr()) {
		dstClient, derr := project.NewClient(cfg, destClientToken(cfg, srcToken))
		if derr != nil {
			return fmt.Errorf("creating destination project client: %w", derr)
		}
		destProjects = dstClient
	}

	opts := transfer.Options{
		DestProjects:         destProjects,
		HostProjectSlug:      tf.hostProjectSlug,
		Branch:               tf.branch,
		DestHost:             tf.destHost,
		DestOrgID:            destOrgID,
		DestTokenContext:     tf.destTokenContext,
		DestTokenEnvVar:      tf.destTokenEnvVar,
		SelectedContextNames: selectedCtxNames,
		Mapping:              combinedMapping,
		IncludeProjectVars:   tf.includeProjectVars,
		IncludeSSHKeys:       tf.includeSSHKeys,
		SkipContexts:         tf.skipContexts,
		RemoveRestrictions:   tf.removeRestrictions,
		ContextClient:        ctxClient,
		BeforeMutate:         newTriggerFlagHook(cmd, cfg, srcToken, m.Source.Org.Slug, projClient, tf.enableTrigger),
		DryRun:               !tf.apply,
		PollTimeout:          tf.pollTimeout,
		Stdout:               cmd.OutOrStdout(),
		Stderr:               cmd.ErrOrStderr(),
	}

	return transfer.Transfer(cmd.Context(), projClient, m, opts)
}

// destClientToken is the token used to read the destination org's project list:
// the destination token, else the source token (the same fallback the org
// lookup uses when no destination token is configured).
func destClientToken(cfg *settings.Config, srcToken string) string {
	if t := cfg.DestTokenOrDefault(); t != "" {
		return t
	}
	return srcToken
}

// isAppDestOrg reports whether the destination is a GitHub App / standalone
// ("circleci") org, whose project slugs carry UUIDs and so cannot be derived
// from a repo name.  A "circleci/..." slug says so directly and a gh/ or bb/
// slug rules it out; with only a bare UUID the org record's vcs_type decides.
// A failed lookup is warned about and treated as "not App" (mapping-only).
func isAppDestOrg(ctx context.Context, g orgGetter, destSlug, destOrgID string, errW io.Writer) bool {
	switch {
	case strings.HasPrefix(destSlug, "circleci/"):
		return true
	case strings.Contains(destSlug, "/"):
		return false
	}
	o, err := g.GetOrganization(ctx, destOrgID)
	if err != nil {
		fmt.Fprintf(errW, "WARNING: could not read destination org %s to decide whether projects can be matched by name: %v\n", destOrgID, err)
		return false
	}
	return strings.EqualFold(o.VCSType, "circleci")
}

// newTriggerFlagHook returns the transfer.Options.BeforeMutate hook that
// enables the org-level allow_api_trigger_with_config flag and the project-level
// api-trigger-with-config flag on every project that will run a pipeline.
//
// transfer.Transfer calls it only on --apply and only after its read-only
// pre-flight checks (host project, checkout keys) have passed, so a doomed run
// never flips a flag.  The returned cleanup restores the prior values in
// reverse order (project flags first, then the org flag).
func newTriggerFlagHook(cmd *cobra.Command, cfg *settings.Config, srcToken, orgSlug string, projClient capture.FlagReaderWriter, enableTrigger bool) func([]string) (func(), error) {
	var orgMgr capture.OrgFlagManager
	if _, _, ok := capture.ParseOrgSlug(orgSlug); ok {
		oc, oerr := newOrgClientForCapture(cfg, srcToken)
		if oerr != nil {
			fmt.Fprintf(cmd.ErrOrStderr(),
				"WARNING: could not create org client to check org-level trigger flag: %v\n", oerr)
		} else {
			orgMgr = oc
		}
	}
	return triggerFlagHook(cmd, orgMgr, orgSlug, projClient, enableTrigger)
}

// triggerFlagHook is newTriggerFlagHook with the org-flag manager injected
// (nil skips the org-level flag).
func triggerFlagHook(cmd *cobra.Command, orgMgr capture.OrgFlagManager, orgSlug string, projClient capture.FlagReaderWriter, enableTrigger bool) func([]string) (func(), error) {
	return func(slugs []string) (func(), error) {
		var restores []func()
		cleanup := func() {
			for i := len(restores) - 1; i >= 0; i-- {
				restores[i]()
			}
		}

		// Org-level flag: enable when requested; restore afterwards.
		orgFlagEnabled := false
		if vcsType, orgName, ok := capture.ParseOrgSlug(orgSlug); ok && orgMgr != nil {
			enabled, restoreOrg, err := maybeEnableOrgTrigger(cmd, orgMgr, vcsType, orgName, enableTrigger)
			if err != nil {
				return nil, err
			}
			if restoreOrg != nil {
				restores = append(restores, restoreOrg)
			}
			orgFlagEnabled = enabled
		}

		// Project-level flag for each project that will run a pipeline.
		for _, slug := range slugs {
			restore, err := maybeEnableProjectTrigger(cmd, projClient, slug, enableTrigger, orgFlagEnabled)
			if err != nil {
				cleanup() // undo whatever was already enabled
				return nil, err
			}
			if restore != nil {
				restores = append(restores, restore)
			}
		}
		return cleanup, nil
	}
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
		"Path to mapping.json (optional). Entries in the 'projects' map whose keys do not contain '/' are "+
			"context name → destination name mappings; keys containing '/' are source project slug → destination "+
			"project slug entries, which win over the by-name match used for GitHub App destinations.")
	f.StringVar(&tf.hostProjectSlug, "host-project", "",
		"Source-org project slug under which the context-transfer pipeline runs. "+
			"Any project with api-trigger-with-config enabled works, but a GitHub OAuth (gh/) or "+
			"Bitbucket (bb/) project must have a GitHub App pipeline definition or a checkout key "+
			"(deploy key or user key — Project Settings → SSH Keys), or CircleCI refuses to run the pipeline; "+
			"a project with neither is rejected before anything is changed. "+
			"When omitted, the first manifest project with a GitHub App pipeline definition is picked, "+
			"else the first with a pipeline definition and a checkout key.")
	f.StringVar(&tf.branch, "branch", "",
		"Branch to check out for the transfer pipeline run. "+
			"Default: each project's default branch from the manifest, falling back to main. "+
			"Set this to force a single branch for all projects.")
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
			"Each source project is resolved to a destination project via --mapping, or, for a GitHub App destination, by name; "+
			"a project that cannot be resolved is skipped and the run exits non-zero naming it. "+
			"Destination project must already be onboarded/exist in the destination org (sync --skip-pipelines creates them).")
	f.BoolVar(&tf.includeSSHKeys, "include-ssh-keys", false, //nolint:gosec // flag name, not a credential
		"Also transfer additional project SSH keys to the destination projects via the in-pipeline "+
			"zero-disk path (add_ssh_keys materializes each key; private key is read verbatim with "+
			"jq --rawfile and POSTed to the destination — never echoed to logs). "+
			"Projects are resolved like --include-project-vars (mapping entry, else name match for a GitHub App destination); "+
			"unresolvable projects are skipped and make the run exit non-zero. "+
			"A project with SSH keys but no env vars still triggers a per-project pipeline. "+
			"Destination project must already be onboarded/exist in the destination org.")
	f.BoolVar(&tf.skipContexts, "skip-contexts", false,
		"Drop every context from the plan so only the per-project pipelines run — for when contexts "+
			"were already transferred. Requires --include-project-vars and/or --include-ssh-keys; cannot be "+
			"combined with --context. No host project is resolved and no context restriction is touched.")
	f.BoolVar(&tf.removeRestrictions, "remove-restrictions", false,
		"Temporarily remove project/expression restrictions from source contexts before the transfer "+
			"pipeline runs, then restore them afterwards (best-effort). "+
			"A context with a project or expression restriction causes the transfer pipeline to come back "+
			"'unauthorized' when the host project is not in the allowed set. "+
			"The default 'All members' group restriction and all other group restrictions are NEVER "+
			"removed (they are org-type specific and cannot always be recreated via API). "+
			"Without this flag, Transfer fails fast with an actionable error when blocking restrictions "+
			"are detected instead of triggering a pipeline that will be unauthorized.")
	f.DurationVar(&tf.pollTimeout, "poll-timeout", 30*time.Minute,
		"Maximum time to wait for the transfer pipeline to complete (0 = no timeout)")
}
