package cmd

import (
	"errors"
	"fmt"
	"time"

	"github.com/AwesomeCICD/circleci-org-migration-cli/api/project"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/capture"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/transfer"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// secretsTransferLong is the long help text for 'secrets transfer'.
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
  Each source project's env vars are available in the job environment (CircleCI
  injects them), and are POSTed to the matching destination project via the
  v1.1 envvar API.

  IMPORTANT: the destination project must already be onboarded/exist.
  Resolution of source project slug → destination project slug requires an
  explicit --mapping entry (keys containing "/" in the projects map). Projects
  without a resolvable destination slug are SKIPPED and flagged in the plan:

    SKIP project "gh/acme/api": dest project for "gh/acme/api" unknown
    — provide --mapping or onboard it first; skipped

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
    --dest-org-id <uuid> --dest-token-context migration-secrets

APPLY — execute the transfer:
  circleci-migrate secrets transfer --manifest manifest.json \
    --dest-org-id <uuid> --dest-token-context migration-secrets --apply

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
    --dest-org-id <dest-org-uuid> \
    --dest-token-context migration-secrets

  # Transfer all contexts with values (requires --apply):
  circleci-migrate secrets transfer --manifest manifest.json \
    --dest-org-id <dest-org-uuid> \
    --dest-token-context migration-secrets \
    --enable-trigger --apply

  # Transfer contexts and project env vars:
  circleci-migrate secrets transfer --manifest manifest.json \
    --dest-org-id <dest-org-uuid> \
    --dest-token-context migration-secrets \
    --mapping mapping.json \
    --include-project-vars \
    --apply

  # Transfer specific contexts only:
  circleci-migrate secrets transfer --manifest manifest.json \
    --dest-org-id <dest-org-uuid> \
    --dest-token-context migration-secrets \
    --context deploy-prod --context shared \
    --apply

  # Custom dest token env-var name:
  circleci-migrate secrets transfer --manifest manifest.json \
    --dest-org-id <dest-org-uuid> \
    --dest-token-context migration-secrets \
    --dest-token-env-var MY_DEST_API_TOKEN \
    --apply

  # Custom dest host (CircleCI Server installations):
  circleci-migrate secrets transfer --manifest manifest.json \
    --dest-org-id <dest-org-uuid> \
    --dest-token-context migration-secrets \
    --dest-host https://circleci.example.com \
    --apply`

// transferFlags holds the bound flag values for 'secrets transfer'.
type transferFlags struct {
	manifestPath       string
	destOrgID          string
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
		Use:   "transfer [--manifest <file>] --dest-org-id <uuid> --dest-token-context <ctx>",
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
	if tf.destOrgID == "" {
		return errors.New("--dest-org-id is required (the destination org UUID; find it in your manifest or the CircleCI org settings page)")
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

	projClient, err := project.NewClient(cfg, srcToken)
	if err != nil {
		return fmt.Errorf("creating project client: %w", err)
	}

	// Enable org-level api-trigger-with-config if requested.
	if tf.enableTrigger {
		if vcsType, orgName, ok := capture.ParseOrgSlug(m.Source.Org.Slug); ok {
			orgClient, oerr := newOrgClientForCapture(cfg, srcToken)
			if oerr != nil {
				fmt.Fprintf(cmd.ErrOrStderr(),
					"WARNING: could not create org client to check org-level trigger flag: %v\n", oerr)
			} else {
				restoreOrgFlag := capture.MaybeEnableOrgTriggerFlag(cmd.Context(), cmd.ErrOrStderr(), orgClient, vcsType, orgName)
				defer restoreOrgFlag()
			}
		}
	}

	opts := transfer.Options{
		HostProjectSlug:      tf.hostProjectSlug,
		Branch:               tf.branch,
		DestHost:             tf.destHost,
		DestOrgID:            tf.destOrgID,
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

// bind registers the transfer flags on f and stores their values in tf.
func (tf *transferFlags) bind(f *pflag.FlagSet) {
	f.StringVar(&tf.manifestPath, "manifest", "", "Path to the export manifest (required)")
	f.StringVar(&tf.destOrgID, "dest-org-id", "",
		"Destination org UUID (required). Find it in your manifest ('source.org.id') or the "+
			"CircleCI org settings page. The in-pipeline job lists destination contexts by owner ID.")
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
		"Source-org project slug under which the transfer pipeline runs. "+
			"Any project with api-trigger-with-config enabled works. "+
			"Auto-picked from the manifest when omitted.")
	f.StringVar(&tf.branch, "branch", "main",
		"Branch to check out for the transfer pipeline run")
	f.BoolVar(&tf.apply, "apply", false,
		"Execute the transfer pipeline (default: dry-run — prints the plan but triggers no pipeline). "+
			"Pass --apply to actually write values to the destination org.")
	f.BoolVar(&tf.enableTrigger, "enable-trigger", false,
		"Enable api-trigger-with-config at the org level if not already on, and restore after transfer "+
			"(the project-level flag must be enabled separately or already be on)")
	f.BoolVar(&tf.includeProjectVars, "include-project-vars", false,
		"Also transfer project env-var values to the destination projects (default: off, context-only). "+
			"Requires each source project to be resolvable to a destination project slug via --mapping; "+
			"projects without a mapping entry are skipped with a warning. "+
			"Destination project must already be onboarded/exist in the destination org.")
	f.DurationVar(&tf.pollTimeout, "poll-timeout", 30*time.Minute,
		"Maximum time to wait for the transfer pipeline to complete (0 = no timeout)")
}
