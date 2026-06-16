package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	cctx "github.com/AwesomeCICD/circleci-org-migration-cli/api/context"
	"github.com/AwesomeCICD/circleci-org-migration-cli/api/org"
	"github.com/AwesomeCICD/circleci-org-migration-cli/api/project"
	"github.com/AwesomeCICD/circleci-org-migration-cli/api/runner"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/capture"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/exporter"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/report"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/syncer"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/transfer"
	"github.com/AwesomeCICD/circleci-org-migration-cli/settings"
	"github.com/spf13/cobra"
)

func newMigrateCommand() *cobra.Command {
	var (
		sourceOrg           string
		destOrg             string
		secretsPath         string
		mappingPath         string
		apply               bool
		yes                 bool
		noInput             bool
		missing             string
		githubToken         string
		destGitHubOrg       string
		skipContexts        bool
		skipProjects        bool
		skipOrgSettings     bool
		skipExtras          bool
		skipRunner          bool
		skipCIAM            bool
		skipPreflight       bool
		preflightOnly       bool
		output              string
		reportPath          string
		runnerNamespace     string
		destRunnerNamespace string
		jsonOutput          bool
		createProjectTokens bool
		followAll           bool
		// in-pipeline secrets transfer (opt-in, mutually exclusive with --secrets)
		transferSecrets    bool
		destTokenContext   string
		includeProjectVars bool
		includeSSHKeys     bool
		transferHostProj   string
	)

	cmd := &cobra.Command{
		Use:   "migrate [--source-org <slug> --dest-org <slug>] [--apply]",
		Short: "All-in-one: export source org and sync into destination org.",
		Long: `migrate combines 'export' and 'sync' into a single command.

When run WITHOUT --source-org and --dest-org on an interactive terminal,
migrate launches a guided walkthrough that prompts for each required value and
lets you choose which parts of the org to migrate. This interactive mode is
designed for first-time use and manual one-off migrations.

NOTE: interactive prompts are written to stderr; if you pipe stdout while
relying on the guided prompts, use a TTY for stdin — piping stdin triggers
non-TTY mode and skips all prompts (use --no-input to make this explicit).

When --source-org and --dest-org are provided, migrate runs non-interactively
using only the supplied flags — suitable for scripting and CI pipelines. Pass
--no-input (or run with stdin redirected / piped) to make the command error
immediately if any required value is missing, instead of blocking on a prompt.

It reads data from the source CircleCI organisation (using the source token),
builds an in-memory manifest, and immediately applies it to the destination
organisation (using the dest token) — without requiring a manifest file on
disk.

Secret VALUES are never exported via the API. If you have a captured secret
bundle (produced by the in-pipeline 'secrets' step), pass it with --secrets.
Without a bundle, all variable values are reported as needing manual entry
(or use --missing-secrets=placeholder to write placeholder values).

IN-PIPELINE SECRETS TRANSFER (opt-in):
  Pass --transfer-secrets together with --dest-token-context to run the
  in-pipeline transfer step after sync completes. This transfers context
  env-var values directly from the source org to the destination org without
  writing any bundle to disk. Mutually exclusive with --secrets.

  The project slug mapping is derived automatically from --source-org and
  --dest-org: for gh/ and bb/ dest orgs the dest slug is
  <provider>/<dest-org-name>/<repo>; this is the same derivation used by
  'mapping generate'. Pass --include-project-vars to also transfer project
  env-var values. Pass --include-ssh-keys to also transfer additional project
  SSH keys in-pipeline (zero-disk; private key material is never echoed to logs).

  Requires:
    --dest-token-context <name>   source-org context that holds CIRCLECI_DEST_TOKEN
    --transfer-secrets            opt-in flag to activate this step

  See 'secrets transfer --help' for full documentation of the in-pipeline flow.

By default migrate performs a DRY RUN and writes nothing to the destination.
Review the output, then re-run with --apply to write changes. Pass --yes / -y
to auto-confirm enabling builds for newly-created projects without a prompt.

Use --output / -o to save the exported manifest to disk, and --report to save
a human-readable audit document. Both flags are optional; omitting them keeps
the migration entirely in-memory.

For more control — e.g. to inspect or edit the manifest between steps — run
'export' and 'sync' separately.

Examples:
  # Interactive guided walkthrough (no flags required):
  circleci-migrate migrate

  # Non-interactive (flags bypass all prompts):
  circleci-migrate migrate \
    --source-org gh/acme --dest-org gh/acme-new \
    --source-token $SRC_TOKEN --dest-token $DST_TOKEN

  # CI pipeline (non-interactive, apply immediately):
  circleci-migrate migrate \
    --source-org gh/acme --dest-org gh/acme-new \
    --secrets secrets.json --apply --yes --no-input

  # Save manifest and audit report:
  circleci-migrate migrate \
    --source-org gh/acme --dest-org gh/acme-new \
    --apply -o manifest.json --report migration-report.md

  # In-pipeline secrets transfer (no bundle file):
  circleci-migrate migrate \
    --source-org gh/acme --dest-org gh/acme-new \
    --transfer-secrets --dest-token-context migration-secrets \
    --apply

  # In-pipeline transfer including project env vars:
  circleci-migrate migrate \
    --source-org gh/acme --dest-org gh/acme-new \
    --transfer-secrets --dest-token-context migration-secrets \
    --include-project-vars --apply

  # In-pipeline transfer including project env vars AND SSH keys:
  circleci-migrate migrate \
    --source-org gh/acme --dest-org gh/acme-new \
    --transfer-secrets --dest-token-context migration-secrets \
    --include-project-vars --include-ssh-keys --apply`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			cfg := configFromContext(ctx)
			// Resolve the GitHub token from the env after parsing so the flag
			// default never leaks $GITHUB_TOKEN into --help output.
			if githubToken == "" {
				githubToken = os.Getenv("GITHUB_TOKEN")
			}

			// Determine whether interactive mode is needed.
			// Interactive mode fires when BOTH required org flags are absent AND
			// stdin is an interactive TTY AND --no-input is not set.
			missingSourceOrg := sourceOrg == ""
			missingDestOrg := destOrg == ""
			wantsInteraction := (missingSourceOrg || missingDestOrg) && !noInput

			if wantsInteraction && !isInteractiveTTY() {
				// Non-TTY (piped/CI) with missing required flags: fail fast with a
				// clear, actionable message BEFORE any banner or prompt output is
				// written.  This is the primary gate for the CI/redirect case where
				// stdin is not a terminal (e.g. stdin=/dev/null, pipe, or CI runner).
				return fmt.Errorf(
					"interactive walkthrough requires a TTY; " +
						"pass --source-org and --dest-org to run non-interactively " +
						"(e.g. --source-org gh/acme --dest-org gh/acme-new). " +
						"See docs/guide.md")
			}

			if wantsInteraction {
				// Launch interactive walkthrough.
				var err error
				sourceOrg, destOrg, secretsPath, missing, apply, yes,
					skipContexts, skipProjects, skipOrgSettings, skipExtras,
					err = runMigrateWalkthrough(cmd, cfg, sourceOrg, destOrg, yes)
				if err != nil {
					return err
				}
			}

			// --- validation ---------------------------------------------------
			if sourceOrg == "" {
				return fmt.Errorf("--source-org is required (e.g. --source-org gh/acme)")
			}
			if destOrg == "" {
				return fmt.Errorf("--dest-org is required (e.g. --dest-org gh/acme-new)")
			}
			if missing != syncer.MissingSkip && missing != syncer.MissingPlaceholder {
				return fmt.Errorf("--missing-secrets must be %q or %q", syncer.MissingSkip, syncer.MissingPlaceholder)
			}
			// --follow-all requires --github-token.
			if followAll && githubToken == "" {
				return fmt.Errorf("--follow-all requires --github-token (or $GITHUB_TOKEN) to list GitHub repositories")
			}
			// --transfer-secrets and --secrets are mutually exclusive.
			if transferSecrets && cmd.Flags().Changed("secrets") {
				return fmt.Errorf("--transfer-secrets and --secrets are mutually exclusive: choose one secrets migration path")
			}
			// --transfer-secrets requires --dest-token-context.
			if transferSecrets && destTokenContext == "" {
				return fmt.Errorf("--transfer-secrets requires --dest-token-context (name of source-org context holding the destination API token)")
			}

			srcToken := cfg.SourceTokenOrDefault()
			if srcToken == "" {
				return noSourceTokenError()
			}
			dstToken := cfg.DestTokenOrDefault()
			if dstToken == "" {
				return fmt.Errorf("no destination API token: set --dest-token, --token, CIRCLECI_DEST_TOKEN, or CIRCLECI_CLI_TOKEN")
			}

			// When --json is set, suppress all human/progress output on stdout;
			// route any progress to stderr instead.
			progressOut := cmd.OutOrStdout()
			if jsonOutput {
				progressOut = cmd.ErrOrStderr()
			}

			// --- preflight checks -------------------------------------------
			// Run after token resolution so token checks are meaningful, but
			// before any export/sync work begins. Build lightweight clients
			// just for preflight (the full export clients are constructed below).
			if !skipPreflight {
				pfSrcOrgClient, pfErr := org.NewClient(cfg, srcToken)
				pfDstOrgClient, pfErr2 := org.NewClient(cfg, dstToken)
				pfProjClient, pfErr3 := project.NewClient(cfg, srcToken)

				// Preflight client build failures are best-effort: log and continue.
				var pfClients preflightClients
				if pfErr == nil {
					pfClients.srcOrg = pfSrcOrgClient
					pfClients.srcFlags = pfSrcOrgClient
					pfClients.srcOrgMgr = pfSrcOrgClient
				}
				if pfErr2 == nil {
					pfClients.dstOrg = pfDstOrgClient
				}
				if pfErr3 == nil {
					pfClients.srcProjects = pfProjClient
					// Wire up the follow-all offer in preflight when a GitHub token is
					// available.  The actual follow only runs if the user opts in interactively
					// or the caller has set --follow-all (which runs it after preflight).
					if githubToken != "" && pfProjClient != nil {
						pfClients.followAllRunner = func(fCtx context.Context) error {
							return runFollowAll(fCtx, sourceOrg, githubToken, pfProjClient, cmd.ErrOrStderr())
						}
					}
				}
				if pfErr != nil || pfErr2 != nil || pfErr3 != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "warning: preflight client init partial: src=%v dst=%v proj=%v\n",
						pfErr, pfErr2, pfErr3)
				}

				pfDeps := preflightDeps{
					cfg:           cfg,
					srcToken:      srcToken,
					dstToken:      dstToken,
					sourceOrg:     sourceOrg,
					destOrg:       destOrg,
					githubToken:   githubToken,
					destGitHubOrg: destGitHubOrg,
				}
				if pfRunErr := runMigratePreflight(ctx, pfDeps, pfClients, cmd.ErrOrStderr()); pfRunErr != nil {
					return pfRunErr
				}
			}

			// --preflight-only: exit after printing the preflight summary.
			if preflightOnly {
				return nil
			}

			// ── Optional: follow GitHub repos not yet set up in CircleCI ─────────
			// When --follow-all is set, list every GitHub repo in the source org and
			// follow any that are not already CircleCI projects.  This must run
			// BEFORE the export so that newly-followed projects are discovered.
			if followAll {
				// Build a temporary project client for the follow-all step.
				faClient, faErr := project.NewClient(cfg, srcToken)
				if faErr != nil {
					return fmt.Errorf("creating project client for follow-all: %w", faErr)
				}
				if faErr := runFollowAll(ctx, sourceOrg, githubToken, faClient, cmd.ErrOrStderr()); faErr != nil {
					return faErr
				}
			}

			// --- step 1: export from source org -------------------------------
			srcOrgClient, err := org.NewClient(cfg, srcToken)
			if err != nil {
				return fmt.Errorf("creating source org client: %w", err)
			}
			srcCtxClient, err := cctx.NewClient(cfg, srcToken)
			if err != nil {
				return fmt.Errorf("creating source context client: %w", err)
			}
			srcProjClient, err := project.NewClient(cfg, srcToken)
			if err != nil {
				return fmt.Errorf("creating source project client: %w", err)
			}

			ex := &exporter.Exporter{
				Org:      srcOrgClient,
				Contexts: srcCtxClient,
				Projects: srcProjClient,
				Out:      cmd.ErrOrStderr(),
			}

			// Wire up the runner client for the source when needed (skipped when
			// --skip-runner is set).
			if !skipRunner && runnerNamespace != "" {
				srcRunnerClient, rerr := runner.NewClient(cfg, srcToken)
				if rerr != nil {
					return fmt.Errorf("creating source runner client: %w", rerr)
				}
				ex.Runner = srcRunnerClient
			}

			m, err := ex.Export(ctx, exporter.Options{
				Host:            cfg.Host,
				OrgSlug:         sourceOrg,
				IncludeContexts: !skipContexts,
				IncludeProjects: !skipProjects,
				IncludeExtras:   !skipExtras,
				RunnerNamespace: func() string {
					if skipRunner {
						return ""
					}
					return runnerNamespace
				}(),
			})
			if err != nil {
				return err
			}
			m.GeneratedAt = time.Now().UTC().Format(time.RFC3339)

			if !jsonOutput {
				fmt.Fprint(progressOut, report.Summary(m))
			}

			// --- optional manifest/report saves (best-effort) -----------------
			if output != "" {
				if saveErr := m.Save(output); saveErr != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "warning: writing manifest: %v\n", saveErr)
				} else {
					fmt.Fprintf(progressOut, "Wrote manifest to      %s\n", output)
				}
			}
			if reportPath != "" {
				if saveErr := report.SaveMarkdown(m, reportPath); saveErr != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "warning: writing audit report: %v\n", saveErr)
				} else {
					fmt.Fprintf(progressOut, "Wrote audit report to  %s\n", reportPath)
				}
			}

			// --- step 2: sync into destination org ----------------------------
			mapping, err := BuildMigrateMapping(mappingPath, sourceOrg, destOrg)
			if err != nil {
				return err
			}

			bundle, err := loadBundleWithFeedback(secretsPath, !cmd.Flags().Changed("secrets"), cmd.ErrOrStderr())
			if err != nil {
				return err
			}

			opts := syncer.Options{
				Apply:               apply,
				MissingSecrets:      missing,
				GitHubToken:         githubToken,
				DestGitHubOrg:       destGitHubOrg,
				DestRunnerNamespace: destRunnerNamespace,
				CreateProjectTokens: createProjectTokens,
			}

			// Wire up the runner client for the destination when needed and not
			// skipped.
			wireRunner := !skipRunner && (destRunnerNamespace != "" || len(m.RunnerResourceClasses) > 0)
			sy, err := buildSyncer(cfg, dstToken, cmd.ErrOrStderr(), wireRunner)
			if err != nil {
				return err
			}

			// Accumulate section reports for --json output.
			repsBySection := make(map[string]*syncer.Report)

			if !skipOrgSettings {
				rep, syncErr := sy.SyncOrgSettings(ctx, m, mapping, opts)
				if syncErr != nil {
					return syncErr
				}
				repsBySection["Org Settings"] = rep
				if !jsonOutput {
					printSyncReport(cmd, "Org Settings", rep, m)
				}
			}
			if !skipContexts {
				rep, syncErr := sy.SyncContexts(ctx, m, bundle, mapping, opts)
				if syncErr != nil {
					return syncErr
				}
				repsBySection["Contexts"] = rep
				if !jsonOutput {
					printSyncReport(cmd, "Contexts", rep, m)
				}
			}
			if !skipProjects {
				rep, syncErr := sy.SyncProjects(ctx, m, bundle, mapping, opts)
				if syncErr != nil {
					return syncErr
				}
				repsBySection["Projects"] = rep
				if !jsonOutput {
					printSyncReport(cmd, "Projects", rep, m)
				}
				if enableErr := handleEnableBuilds(cmd, sy, rep, apply, yes, jsonOutput); enableErr != nil {
					return enableErr
				}
			}

			// Runner resource classes (skipped when --skip-runner is set).
			if !skipRunner && (len(m.RunnerResourceClasses) > 0 || destRunnerNamespace != "") {
				rep, syncErr := sy.SyncRunnerResourceClasses(ctx, m, opts)
				if syncErr != nil {
					return syncErr
				}
				repsBySection["Runner Resource Classes"] = rep
				if !jsonOutput {
					printSyncReport(cmd, "Runner Resource Classes", rep, m)
				}
			}

			// CIAM roles and groups (standalone circleci-type orgs only; self-gated).
			if !skipCIAM && m.CIAM != nil {
				rep, syncErr := sy.SyncCIAM(ctx, m, mapping, opts)
				if syncErr != nil {
					return syncErr
				}
				repsBySection["CIAM"] = rep
				if !jsonOutput {
					printSyncReport(cmd, "CIAM", rep, m)
				}
			}

			if jsonOutput {
				exportSummary := buildExportSummary(m, output, reportPath)
				syncSummary := buildSyncSummary(apply, repsBySection)
				combined := migrateJSONOutput{
					DryRun: !apply,
					Export: exportSummary,
					Sync:   syncSummary,
				}
				return marshalJSON(cmd.OutOrStdout(), combined)
			}

			// ── Step 3 (opt-in): in-pipeline secrets transfer ─────────────────
			// Only runs when --transfer-secrets is set. The project slug mapping
			// is derived in-memory from --source-org / --dest-org so the user
			// does not need to produce a mapping.json separately.
			if transferSecrets {
				if err := runMigrateSecretsTransfer(cmd, cfg, m, srcToken, sourceOrg, destOrg, destTokenContext, transferHostProj, apply, includeProjectVars, includeSSHKeys); err != nil {
					return err
				}
			}

			return nil
		},
	}

	f := cmd.Flags()
	f.StringVar(&sourceOrg, "source-org", "",
		"CircleCI organization slug for the source org, e.g. gh/my-org "+
			"(shown in CircleCI → Organization Settings → Overview). "+
			"This is the CircleCI org identifier, not a GitHub repository URL. (required, or prompted interactively)")
	f.StringVar(&destOrg, "dest-org", "",
		"CircleCI organization slug for the destination org, e.g. gh/my-new-org "+
			"(shown in CircleCI → Organization Settings → Overview). "+
			"This is the CircleCI org identifier, not a GitHub repository URL. (required, or prompted interactively)")
	f.StringVar(&secretsPath, "secrets", "secrets.json",
		"Path to a captured secret bundle (optional)")
	f.StringVar(&mappingPath, "mapping", "",
		"Path to a source->destination mapping file (optional)")
	f.BoolVar(&apply, "apply", false,
		"Write changes to the destination (default: dry run)")
	f.BoolVarP(&yes, "yes", "y", false,
		"Auto-confirm enabling builds after project creation (skip the interactive prompt)")
	f.BoolVar(&noInput, "no-input", false,
		"Disable all interactive prompts; error if a required value is missing (implied when stdin is not a TTY)")
	f.StringVar(&missing, "missing-secrets", syncer.MissingSkip,
		"How to handle variables with no captured value: skip|placeholder")
	f.StringVar(&githubToken, "github-token", "",
		"GitHub personal access token used to resolve repository IDs when creating pipeline definitions "+
			"in a GitHub App destination org. Falls back to $GITHUB_TOKEN. Required when repos have been "+
			"moved to a new GitHub org (--dest-github-org or mapping github_org).")
	f.StringVar(&destGitHubOrg, "dest-github-org", "",
		"Destination GitHub organization owner (e.g. 'acme-new'). Use when all repos have moved to a new "+
			"GitHub org. Takes precedence over the source owner when resolving repo external IDs; overridden "+
			"by an explicit github_org entry in the mapping file. Requires --github-token.")
	f.BoolVar(&skipContexts, "skip-contexts", false,
		"Skip exporting and syncing contexts")
	f.BoolVar(&skipProjects, "skip-projects", false,
		"Skip exporting and syncing projects")
	f.BoolVar(&skipOrgSettings, "skip-org-settings", false,
		"Skip syncing org-level settings (feature flags, OIDC, URL-orb allow list, config policies)")
	f.BoolVar(&skipExtras, "skip-extras", false,
		"Skip checkout keys, webhooks, and schedules")
	f.BoolVar(&skipRunner, "skip-runner", false,
		"Skip exporting and syncing self-hosted runner resource classes")
	f.BoolVar(&skipCIAM, "skip-ciam", false,
		"Skip syncing CIAM roles and groups (standalone circleci-type orgs only)")
	f.BoolVar(&jsonOutput, "json", false,
		"Print a machine-readable JSON summary to stdout instead of the human-readable output; progress is written to stderr")
	f.StringVarP(&output, "output", "o", "",
		"Optional: save the exported manifest to this path (omit to keep migration entirely in-memory)")
	f.StringVar(&reportPath, "report", "",
		"Optional: save the human-readable audit report to this path (omit to skip writing the report)")
	f.StringVar(&runnerNamespace, "runner-namespace", "",
		"Source runner namespace to capture self-hosted runner resource classes from (e.g. 'acme'). "+
			"The namespace must be supplied explicitly — there is no clean org→namespace lookup.")
	f.StringVar(&destRunnerNamespace, "dest-runner-namespace", "",
		"Destination runner namespace for recreating self-hosted runner resource classes (e.g. 'acme-new'). "+
			"Must be supplied explicitly — the syncer never guesses the destination namespace. "+
			"When omitted and the manifest contains runner classes, each is flagged for manual recreation.")
	f.BoolVar(&createProjectTokens, "create-project-tokens", false,
		"When set AND --apply, recreate each captured project API token on the destination project. "+
			"CAUTION: each recreated token mints a NEW one-time secret — every consumer of the old token "+
			"must be repointed to the new value. New plaintext values are printed to stderr once and cannot "+
			"be retrieved again. Default false: emit manual steps only.")
	f.BoolVar(&skipPreflight, "skip-preflight", false,
		"Skip the startup preflight checks (token validation, org reachability, cross-type warning, "+
			"api-trigger flag, project discovery). Preflight runs by default before export/sync; use "+
			"--skip-preflight in CI pipelines or when checks have already been verified manually.")
	f.BoolVar(&preflightOnly, "preflight-only", false,
		"Run the preflight checks and print the summary, then exit without performing export or sync. "+
			"Exits non-zero if any check is a hard failure; exits 0 on warnings (unless --skip-preflight is also set). "+
			"Use this to validate configuration before committing to a migration run.")
	f.BoolVar(&followAll, "follow-all", false,
		"(GitHub OAuth orgs only) Before exporting, list all GitHub repos in the source org and follow any "+
			"not yet set up as CircleCI projects, making them visible to subsequent discovery. "+
			"Requires --github-token. Archived repos are skipped. "+
			"Webhook-validation errors on brand-new repos are warned and skipped, not fatal. "+
			"Not applicable to circleci/ (App/standalone) orgs — a note is printed and this flag is ignored.")
	// In-pipeline secrets transfer (opt-in, mutually exclusive with --secrets).
	f.BoolVar(&transferSecrets, "transfer-secrets", false,
		"After sync, run the in-pipeline secrets transfer to copy context env-var values directly "+
			"from source to destination without writing a bundle file. "+
			"Requires --dest-token-context. Mutually exclusive with --secrets.")
	f.StringVar(&destTokenContext, "dest-token-context", "",
		"Name of the source-org context that holds the destination API token "+
			"(env var: CIRCLECI_DEST_TOKEN). Required when --transfer-secrets is set.")
	f.BoolVar(&includeProjectVars, "include-project-vars", false,
		"When --transfer-secrets is set, also transfer project-level env-var values to the "+
			"corresponding destination projects. "+
			"Destination project slugs are derived from --dest-org (gh/ and bb/ orgs only). "+
			"Projects without a derivable destination slug are skipped.")
	f.BoolVar(&includeSSHKeys, "include-ssh-keys", false, //nolint:gosec // flag name, not a credential
		"When --transfer-secrets is set, also transfer additional project SSH keys to the "+
			"destination projects via the in-pipeline zero-disk path. "+
			"Private key material is read with jq --rawfile and never echoed to logs. "+
			"Destination project slugs are derived from --dest-org (gh/ and bb/ orgs only). "+
			"Projects without a derivable destination slug are skipped.")
	f.StringVar(&transferHostProj, "host-project", "",
		"When --transfer-secrets is set, the source-org project slug whose pipeline runs the "+
			"context transfer (e.g. gh/acme/web). Defaults to the first project. Prefer an "+
			"ESTABLISHED (long-followed) project — a just-followed project's context "+
			"authorization may not have propagated yet.")

	return cmd
}

// migrateComponents is the ordered list of migration components shown during
// the interactive walkthrough.
var migrateComponents = []string{
	"contexts",
	"projects",
	"org settings",
	"extras (checkout keys, webhooks, schedules)",
}

// runMigrateWalkthrough conducts the interactive guided migration walkthrough.
// It writes prompts to cmd.ErrOrStderr() and reads answers from os.Stdin.
//
// The function delegates to RunMigrateWalkthroughWith so that tests can inject
// synthetic I/O via NewPrompter without spawning a real TTY.
func runMigrateWalkthrough(
	cmd *cobra.Command,
	cfg *settings.Config,
	sourceOrg, destOrg string,
	yes bool,
) (
	outSourceOrg, outDestOrg, outSecretsPath, outMissing string,
	outApply, outYes, outSkipContexts, outSkipProjects, outSkipOrgSettings, outSkipExtras bool,
	err error,
) {
	return RunMigrateWalkthroughWith(
		NewPrompter(os.Stdin, cmd.ErrOrStderr()),
		cmd,
		cfg,
		sourceOrg, destOrg, yes,
	)
}

// RunMigrateWalkthroughWith is the injectable interactive walkthrough used by
// both the command (via runMigrateWalkthrough) and external test files.
// p supplies the I/O streams; cmd is used for printing the apply summary; cfg
// is the per-invocation config the walkthrough fills in (e.g. tokens prompted
// interactively) in place of the former package-level rootOptions global.
func RunMigrateWalkthroughWith(
	p *Prompter,
	cmd *cobra.Command,
	cfg *settings.Config,
	sourceOrg, destOrg string,
	yes bool,
) (
	outSourceOrg, outDestOrg, outSecretsPath, outMissing string,
	outApply, outYes, outSkipContexts, outSkipProjects, outSkipOrgSettings, outSkipExtras bool,
	err error,
) {
	out := p.out

	// Values gathered interactively. The walkthrough only runs when the
	// corresponding flags are absent, so these start empty and are filled by
	// the prompts below.
	var (
		secretsPath, missing                                           string
		apply, skipContexts, skipProjects, skipOrgSettings, skipExtras bool
	)

	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "╔══════════════════════════════════════════════════╗")
	fmt.Fprintln(out, "║   CircleCI Organization Migration — guided mode  ║")
	fmt.Fprintln(out, "╚══════════════════════════════════════════════════╝")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "Tip: re-run with --source-org and --dest-org to skip these prompts.")

	// --- 1. Org slugs --------------------------------------------------------
	printStepHeader(out, 1, 4, "Source and destination organizations")
	fmt.Fprintln(out, "  Slug format: gh/<org>  or  circleci/<org-id>")

	if sourceOrg == "" {
		sourceOrg, err = p.askRequired("Source org slug", "e.g. gh/acme")
		if err != nil {
			return
		}
	} else {
		fmt.Fprintf(out, "  Source org:      %s  (from --source-org)\n", sourceOrg)
	}

	if destOrg == "" {
		destOrg, err = p.askRequired("Destination org slug", "e.g. gh/acme-new")
		if err != nil {
			return
		}
	} else {
		fmt.Fprintf(out, "  Destination org: %s  (from --dest-org)\n", destOrg)
	}

	// --- 2. Tokens -----------------------------------------------------------
	printStepHeader(out, 2, 4, "API tokens")
	fmt.Fprintln(out, "  Token input is hidden when running on an interactive terminal.")

	srcToken := cfg.SourceTokenOrDefault()
	if srcToken == "" {
		srcToken, err = p.askSecretRequired("Source API token (CIRCLECI_SOURCE_TOKEN)")
		if err != nil {
			return
		}
		cfg.SourceToken = srcToken
	} else {
		fmt.Fprintln(out, "  Source token:      already set via flag or environment variable")
	}

	dstToken := cfg.DestTokenOrDefault()
	if dstToken == "" {
		dstToken, err = p.askSecretRequired("Destination API token (CIRCLECI_DEST_TOKEN)")
		if err != nil {
			return
		}
		cfg.DestToken = dstToken
	} else {
		fmt.Fprintln(out, "  Destination token: already set via flag or environment variable")
	}

	// --- 3. What to migrate --------------------------------------------------
	printStepHeader(out, 3, 4, "What to migrate")

	var chosen []string
	chosen, err = p.askMultiSelect(
		"Select components to migrate (default: all):",
		migrateComponents,
	)
	if err != nil {
		return
	}

	// Map selection back to skip flags.  Start by skipping everything, then
	// un-skip whatever the user chose.
	skipContexts = true
	skipProjects = true
	skipOrgSettings = true
	skipExtras = true
	for _, c := range chosen {
		switch c {
		case migrateComponents[0]: // contexts
			skipContexts = false
		case migrateComponents[1]: // projects
			skipProjects = false
		case migrateComponents[2]: // org settings
			skipOrgSettings = false
		case migrateComponents[3]: // extras
			skipExtras = false
		}
	}

	// --- Step 3a. Secrets bundle ---------------------------------------------
	printSubStepHeader(out, "3a", 4, "Secrets bundle")
	fmt.Fprintln(out, "  A captured secrets bundle supplies plaintext env-var values during sync.")
	fmt.Fprintln(out, "  Produce one with 'secrets capture' before running migrate.")
	fmt.Fprintln(out, "  Answering 'no' proceeds with NO secret values: the walkthrough will not")
	fmt.Fprintln(out, "  auto-load secrets.json, and variable values are handled per step 3b below.")
	var useBundle bool
	useBundle, err = p.askBool("Do you have a captured secrets bundle to provide?", false)
	if err != nil {
		return
	}
	if useBundle {
		secretsPath, err = p.askWithDefault("Path to secrets bundle", "secrets.json")
		if err != nil {
			return
		}
	} else {
		secretsPath = "" // no bundle
	}

	// --- Step 3b. Missing secrets handling -----------------------------------
	printSubStepHeader(out, "3b", 4, "Missing secret values")
	fmt.Fprintln(out, "  Variables not found in the bundle can be skipped or written as placeholders.")
	var missingChoice string
	missingChoice, err = p.askChoice(
		"How should missing secret values be handled?",
		[]string{syncer.MissingSkip, syncer.MissingPlaceholder},
	)
	if err != nil {
		return
	}
	missing = missingChoice

	// --- 4. Dry run vs apply -------------------------------------------------
	printStepHeader(out, 4, 4, "Dry run or apply")
	fmt.Fprintln(out, "  A dry run previews changes without writing anything to the destination.")

	var doApply bool
	doApply, err = p.askBool("Perform a dry run first (recommended)?", true)
	if err != nil {
		return
	}
	apply = !doApply // "yes to dry run" → apply=false

	if apply {
		// Show a summary and require an explicit "yes" before proceeding.
		fmt.Fprintln(out, "")
		fmt.Fprintln(out, "  !! APPLY MODE — changes WILL be written to the destination org !!")
		fmt.Fprintln(out, "")
		fmt.Fprintf(out, "  Source:      %s\n", sourceOrg)
		fmt.Fprintf(out, "  Destination: %s\n", destOrg)
		selected := componentsLabel(skipContexts, skipProjects, skipOrgSettings, skipExtras)
		fmt.Fprintf(out, "  Migrating:   %s\n", selected)
		fmt.Fprintln(out, "")

		var confirmed bool
		confirmed, err = p.askBool("Confirm — proceed with APPLY?", false)
		if err != nil {
			return
		}
		if !confirmed {
			err = fmt.Errorf("migration cancelled by user")
			return
		}
	}

	// End-of-walkthrough pointer to advanced flags not covered by the prompts.
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "  Advanced options not covered above (set via flags, re-run with --help):")
	fmt.Fprintln(out, "    --runner-namespace / --dest-runner-namespace  migrate self-hosted runner resource classes")
	fmt.Fprintln(out, "    --dest-github-org                             when repos moved to a new GitHub org (App orgs)")
	fmt.Fprintln(out, "    --mapping                                     per-project source->destination slug overrides")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "  Terraform alternative (IaC path):")
	fmt.Fprintln(out, "    To manage contexts, projects, webhooks, runners, and pipelines as Terraform")
	fmt.Fprintln(out, "    code, run 'terraform generate' after export and apply with terraform, then")
	fmt.Fprintln(out, "    re-run sync with --skip-terraform-managed to fill in CLI-only gaps:")
	fmt.Fprintln(out, "      circleci-migrate terraform generate --manifest manifest.json --dest-org-id <uuid> --out ./terraform/")
	fmt.Fprintln(out, "      cd ./terraform/ && terraform init && terraform plan && terraform apply")
	fmt.Fprintln(out, "      circleci-migrate sync --manifest manifest.json --dest-token $CIRCLECI_DEST_TOKEN --apply --skip-terraform-managed")

	outSourceOrg = sourceOrg
	outDestOrg = destOrg
	outSecretsPath = secretsPath
	outMissing = missing
	outApply = apply
	outYes = yes
	outSkipContexts = skipContexts
	outSkipProjects = skipProjects
	outSkipOrgSettings = skipOrgSettings
	outSkipExtras = skipExtras
	return
}

// componentsLabel builds a short human-readable list of selected migration
// components, used in the apply confirmation summary.
func componentsLabel(skipContexts, skipProjects, skipOrgSettings, skipExtras bool) string {
	var parts []string
	if !skipContexts {
		parts = append(parts, "contexts")
	}
	if !skipProjects {
		parts = append(parts, "projects")
	}
	if !skipOrgSettings {
		parts = append(parts, "org settings")
	}
	if !skipExtras {
		parts = append(parts, "extras")
	}
	if len(parts) == 0 {
		return "(none)"
	}
	return strings.Join(parts, ", ")
}

// migrateJSONOutput is the combined machine-readable result of a migrate
// command when --json is set. It contains a top-level dry_run flag plus
// the export and sync summaries, reusing the same types as the standalone
// export/sync commands.
type migrateJSONOutput struct {
	// DryRun is true when --apply was not set (no changes were written).
	DryRun bool `json:"dry_run"`
	// Export contains the export phase summary.
	Export ExportJSONSummary `json:"export"`
	// Sync contains the sync phase summary.
	Sync SyncJSONSummary `json:"sync"`
}

// BuildMigrateMapping returns the manifest.Mapping to use during sync.
//
// When mappingPath is non-empty the mapping is loaded from disk. Otherwise a
// simple source→destination org mapping is constructed from srcOrg and dstOrg.
func BuildMigrateMapping(mappingPath, srcOrg, dstOrg string) (*manifest.Mapping, error) {
	if mappingPath != "" {
		return manifest.LoadMapping(mappingPath)
	}
	return &manifest.Mapping{
		Org: manifest.OrgMapping{From: srcOrg, To: dstOrg},
	}, nil
}

// runMigrateSecretsTransfer executes the in-pipeline secrets transfer step
// inside migrate when --transfer-secrets is set.
//
// It derives the project slug mapping in-memory using the same logic as
// 'mapping generate': for gh/ and bb/ dest orgs the dest slug is
// <provider>/<dest-org-name>/<repo>. The mapping is passed to
// transfer.Transfer as the combinedMapping so project env-var pipelines are
// routed to the correct destination projects without requiring a mapping.json
// file on disk.
//
// Parameters:
//
//	cmd               — parent cobra command (for stdout/stderr/context)
//	cfg               — resolved CLI config (host, tokens)
//	m                 — in-memory manifest produced by the export step
//	srcToken          — source org API token
//	sourceOrg         — source org slug (e.g. "gh/acme")
//	destOrg           — destination org slug (e.g. "gh/acme-new")
//	destTokenContext  — source-org context name holding CIRCLECI_DEST_TOKEN
//	apply             — true → trigger the pipeline; false → dry run only
//	includeProjectVars — true → also transfer project env-var values
//	includeSSHKeys    — true → also transfer additional project SSH keys
func runMigrateSecretsTransfer(
	cmd *cobra.Command,
	cfg *settings.Config,
	m *manifest.Manifest,
	srcToken, sourceOrg, destOrg, destTokenContext, hostProjectOverride string,
	apply, includeProjectVars, includeSSHKeys bool,
) error {
	stderr := cmd.ErrOrStderr()

	fmt.Fprintln(stderr, "")
	fmt.Fprintln(stderr, "── In-pipeline secrets transfer ─────────────────────────────")

	// Build the combined mapping in-memory: derive dest project slugs for all
	// source projects that can be matched by repo name to the dest org.
	normalizedDest := normalizeVCSPrefix(destOrg)
	combinedMapping := make(map[string]string, len(m.Projects))
	for _, mp := range m.Projects {
		srcSlug := normalizeVCSPrefix(mp.Slug)
		if dst, ok := deriveDestSlug(srcSlug, normalizedDest); ok {
			combinedMapping[srcSlug] = dst
		}
	}

	// Resolve destination org ID from the slug.
	dstToken := cfg.DestTokenOrDefault()
	orgClient, err := org.NewClient(cfg, dstToken)
	if err != nil {
		return fmt.Errorf("creating org client for secrets transfer: %w", err)
	}
	destOrgID, err := orgClient.ResolveOrgID(cmd.Context(), destOrg)
	if err != nil {
		return fmt.Errorf("resolving destination org %q for secrets transfer: %w", destOrg, err)
	}

	projClient, err := project.NewClient(cfg, srcToken)
	if err != nil {
		return fmt.Errorf("creating project client for secrets transfer: %w", err)
	}

	// Enable the org-level trigger flag when needed (non-interactive: auto-enable
	// since the user opted in with --transfer-secrets; restore after).
	orgFlagEnabled := false
	if vcsType, orgName, ok := capture.ParseOrgSlug(m.Source.Org.Slug); ok {
		orgMgr, oerr := newOrgClientForCapture(cfg, srcToken)
		if oerr != nil {
			fmt.Fprintf(stderr, "warning: could not create org client to check org-level trigger flag: %v\n", oerr)
		} else {
			enabled, restoreOrg, enErr := maybeEnableOrgTrigger(cmd, orgMgr, vcsType, orgName, true /* autoEnable */)
			if enErr != nil {
				return enErr
			}
			if restoreOrg != nil {
				defer restoreOrg()
			}
			orgFlagEnabled = enabled
		}
	}

	// Enable the project-level trigger flag for each source project that will
	// run a pipeline (host project + per-project env-var pipelines).
	hostSlug := ""
	if hostProjectOverride != "" {
		hostSlug = normalizeVCSPrefix(hostProjectOverride)
	} else if len(m.Projects) > 0 {
		hostSlug = normalizeVCSPrefix(m.Projects[0].Slug)
	}
	slugsNeedingFlag := collectTransferProjectSlugs(hostSlug, m, combinedMapping, includeProjectVars, includeSSHKeys)
	for _, slug := range slugsNeedingFlag {
		restore, pErr := maybeEnableProjectTrigger(cmd, projClient, slug, true /* autoEnable */, orgFlagEnabled)
		if pErr != nil {
			return pErr
		}
		if restore != nil {
			defer restore() //nolint:revive
		}
	}

	// #nosec G101 -- DestTokenEnvVar is the NAME of an env var (not a secret
	// value); the token is injected at runtime from the source-org context.
	opts := transfer.Options{
		HostProjectSlug:    hostSlug,
		Branch:             "main",
		DestOrgID:          destOrgID,
		DestTokenContext:   destTokenContext,
		DestTokenEnvVar:    "CIRCLECI_DEST_TOKEN",
		Mapping:            combinedMapping,
		IncludeProjectVars: includeProjectVars,
		IncludeSSHKeys:     includeSSHKeys,
		DryRun:             !apply,
		PollTimeout:        30 * time.Minute,
		Stdout:             cmd.OutOrStdout(),
		Stderr:             stderr,
	}

	return transfer.Transfer(cmd.Context(), projClient, m, opts)
}
