package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	cctx "github.com/AwesomeCICD/circleci-org-migration-cli/api/context"
	apiOrb "github.com/AwesomeCICD/circleci-org-migration-cli/api/orb"
	"github.com/AwesomeCICD/circleci-org-migration-cli/api/org"
	"github.com/AwesomeCICD/circleci-org-migration-cli/api/project"
	"github.com/AwesomeCICD/circleci-org-migration-cli/api/runner"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/capture"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/exporter"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/report"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/syncer"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/transfer"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/ui"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/validate"
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
		skipOrb             bool
		skipPreflight       bool
		preflightOnly       bool
		output              string
		reportPath          string
		runnerNamespace     string
		destRunnerNamespace string
		orbNamespace        string
		destOrbNamespace    string
		jsonOutput          bool
		createProjectTokens bool
		includeDangerFlags  bool
		followAll           bool
		// in-pipeline secrets transfer (opt-in, mutually exclusive with --secrets)
		transferSecrets    bool
		destTokenContext   string
		includeProjectVars bool
		includeSSHKeys     bool
		transferHostProj   string
		removeRestrictions bool
		// post-apply parity validation
		skipValidate bool
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
				wt, wtErr := runMigrateWalkthrough(cmd, cfg, sourceOrg, destOrg, yes)
				if wtErr != nil {
					// Ctrl+C (SIGINT) cancels the context; surface a clean abort message.
					if errors.Is(wtErr, context.Canceled) || errors.Is(wtErr, context.DeadlineExceeded) {
						fmt.Fprintln(cmd.ErrOrStderr(), "\nAborted.")
						return wtErr
					}
					return wtErr
				}
				// Assign struct fields back to outer flag vars so that the
				// validation and execution logic below is unchanged.
				sourceOrg = wt.SourceOrg
				destOrg = wt.DestOrg
				secretsPath = wt.SecretsPath
				missing = wt.Missing
				// In guided mode, apply starts as false (dry-run first; we confirm later).
				apply = false
				yes = wt.Yes
				skipContexts = wt.SkipContexts
				skipProjects = wt.SkipProjects
				skipOrgSettings = wt.SkipOrgSettings
				skipExtras = wt.SkipExtras
				skipOrb = wt.SkipOrb
				skipRunner = wt.SkipRunner
				orbNamespace = wt.OrbNamespace
				destOrbNamespace = wt.DestOrbNamespace
				runnerNamespace = wt.RunnerNamespace
				destRunnerNamespace = wt.DestRunnerNamespace
				// In-pipeline transfer fields.
				transferSecrets = wt.TransferSecrets
				destTokenContext = wt.DestTokenContext
				includeProjectVars = wt.IncludeProjectVars
				includeSSHKeys = wt.IncludeSSHKeys
				removeRestrictions = wt.RemoveRestrictions
				transferHostProj = wt.HostProject
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

			// quietMode is true during the guided walkthrough: export and sync output
			// is condensed to short summaries so the user sees a calm overview
			// rather than a firehose. Full detail always goes to the report file.
			// Feature B: quiet output for guided runs.
			quietMode := wantsInteraction

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

			// Wire up the orb client for the source when needed (skipped when
			// --skip-orb is set).
			if !skipOrb && orbNamespace != "" {
				srcOrbClient, oerr := apiOrb.NewClient(cfg, srcToken)
				if oerr != nil {
					return fmt.Errorf("creating source orb client: %w", oerr)
				}
				ex.Orb = srcOrbClient
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
				OrbNamespace: func() string {
					if skipOrb {
						return ""
					}
					return orbNamespace
				}(),
			})
			if err != nil {
				return err
			}
			m.GeneratedAt = time.Now().UTC().Format(time.RFC3339)

			if !jsonOutput {
				if quietMode {
					// Feature B: one-line export summary for guided mode.
					cv, pv := countManifestVars(m)
					fmt.Fprintf(progressOut, "Exported: %d contexts (%d vars), %d projects (%d vars), %d orbs, %d runner classes, %d warnings\n",
						len(m.Contexts), cv, len(m.Projects), pv, len(m.Orbs), len(m.RunnerResourceClasses), len(m.Warnings))
				} else {
					fmt.Fprint(progressOut, report.Summary(m))
				}
			}

			// --- optional manifest/report saves (best-effort) -----------------
			// Feature B: in guided mode, always write the full report to
			// ./migration-report.md (or --report path) so detail is never lost.
			effectiveReportPath := reportPath
			if quietMode && effectiveReportPath == "" {
				effectiveReportPath = "migration-report.md"
			}

			if output != "" {
				if saveErr := m.Save(output); saveErr != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "warning: writing manifest: %v\n", saveErr)
				} else {
					fmt.Fprintf(progressOut, "Wrote manifest to      %s\n", output)
				}
			}
			if effectiveReportPath != "" {
				if saveErr := report.SaveMarkdown(m, effectiveReportPath); saveErr != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "warning: writing audit report: %v\n", saveErr)
				} else {
					if quietMode {
						fmt.Fprintf(progressOut, "Full detail: ./%s\n", effectiveReportPath)
					} else {
						fmt.Fprintf(progressOut, "Wrote audit report to  %s\n", effectiveReportPath)
					}
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

			// Wire up the runner/orb syncer clients once (reused for dry-run + apply).
			wireRunner := !skipRunner && (destRunnerNamespace != "" || len(m.RunnerResourceClasses) > 0)
			wireOrb := !skipOrb && (destOrbNamespace != "" || len(m.Orbs) > 0)
			sy, err := buildSyncer(cfg, dstToken, cmd.ErrOrStderr(), wireRunner, wireOrb)
			if err != nil {
				return err
			}

			// runSyncSections executes all enabled sync sections and returns the
			// accumulated section reports. It is called once for dry-run and
			// (in guided mode) once more for apply — reusing the same manifest
			// and syncer so export never runs twice.
			runSyncSections := func(applyNow bool) (map[string]*syncer.Report, error) {
				sOpts := syncer.Options{
					Apply:               applyNow,
					MissingSecrets:      missing,
					GitHubToken:         githubToken,
					DestGitHubOrg:       destGitHubOrg,
					DestRunnerNamespace: destRunnerNamespace,
					DestOrbNamespace:    destOrbNamespace,
					CreateProjectTokens: createProjectTokens,
					IncludeDangerFlags:  includeDangerFlags,
				}
				reps := make(map[string]*syncer.Report)

				if !skipOrgSettings {
					rep, syncErr := sy.SyncOrgSettings(ctx, m, mapping, sOpts)
					if syncErr != nil {
						return reps, syncErr
					}
					reps["Org Settings"] = rep
					if !jsonOutput && !quietMode {
						printSyncReport(cmd, "Org Settings", rep, m)
					}
				}
				if !skipContexts {
					rep, syncErr := sy.SyncContexts(ctx, m, bundle, mapping, sOpts)
					if syncErr != nil {
						return reps, syncErr
					}
					reps["Contexts"] = rep
					if !jsonOutput && !quietMode {
						printSyncReport(cmd, "Contexts", rep, m)
					}
				}
				if !skipProjects {
					rep, syncErr := sy.SyncProjects(ctx, m, bundle, mapping, sOpts)
					if syncErr != nil {
						return reps, syncErr
					}
					reps["Projects"] = rep
					if !jsonOutput && !quietMode {
						printSyncReport(cmd, "Projects", rep, m)
					}
					if enableErr := handleEnableBuilds(cmd, sy, rep, applyNow, yes, jsonOutput); enableErr != nil { //nolint:contextcheck // handleEnableBuilds predates ctx propagation; ctx not needed for sync-enable path
						return reps, enableErr
					}

					if !skipContexts {
						prDestSlug := destOrg
						if mapping != nil && mapping.Org.To != "" {
							prDestSlug = mapping.Org.To
						}
						prRep := sy.ApplyDeferredProjectRestrictions(ctx, &syncer.Report{DestOrgSlug: prDestSlug, Applied: sOpts.Apply}, sOpts)
						reps["Context Project Restrictions"] = prRep
						if !jsonOutput && !quietMode {
							printSyncReport(cmd, "Context Project Restrictions", prRep, m)
						}
					}
				}

				if !skipRunner && (len(m.RunnerResourceClasses) > 0 || destRunnerNamespace != "") {
					rep, syncErr := sy.SyncRunnerResourceClasses(ctx, m, sOpts)
					if syncErr != nil {
						return reps, syncErr
					}
					reps["Runner Resource Classes"] = rep
					if !jsonOutput && !quietMode {
						printSyncReport(cmd, "Runner Resource Classes", rep, m)
					}
				}

				if !skipCIAM && m.CIAM != nil {
					rep, syncErr := sy.SyncCIAM(ctx, m, mapping, sOpts)
					if syncErr != nil {
						return reps, syncErr
					}
					reps["CIAM"] = rep
					if !jsonOutput && !quietMode {
						printSyncReport(cmd, "CIAM", rep, m)
					}
				}

				if !skipOrb && (len(m.Orbs) > 0 || destOrbNamespace != "") {
					var orbFlagMgr syncer.OrbFlagManager
					if orgClient, oErr := org.NewClient(cfg, dstToken); oErr == nil {
						orbFlagMgr = &orgOrbFlagAdapter{c: orgClient}
					}
					destVCSType, destOrgName := "", ""
					destSlug := destOrg
					if mapping != nil && mapping.Org.To != "" {
						destSlug = mapping.Org.To
					}
					if parts := strings.SplitN(destSlug, "/", 2); len(parts) == 2 {
						destVCSType, destOrgName = parts[0], parts[1]
					}
					rep, syncErr := sy.SyncOrbs(ctx, m, sOpts, orbFlagMgr, destVCSType, destOrgName)
					if syncErr != nil {
						return reps, syncErr
					}
					reps["Orbs"] = rep
					if !jsonOutput && !quietMode {
						printSyncReport(cmd, "Orbs", rep, m)
					}
				}
				return reps, nil
			}

			// ── Sync pass ────────────────────────────────────────────────────
			// Interactive (guided) mode always dry-runs first, then asks to apply
			// below. Non-interactive mode honors --apply directly: a regression in
			// the guided-overhaul refactor hardcoded this to dry-run, so a scripted
			// `migrate --apply` silently applied nothing.
			repsBySection, err := runSyncSections(!wantsInteraction && apply)
			if err != nil {
				return err
			}

			if jsonOutput {
				// JSON output: emit a single combined result (non-interactive path only).
				exportSummary := buildExportSummary(m, output, effectiveReportPath)
				syncSummary := buildSyncSummary(apply, repsBySection)
				combined := migrateJSONOutput{
					DryRun: !apply,
					Export: exportSummary,
					Sync:   syncSummary,
				}
				return marshalJSON(cmd.OutOrStdout(), combined)
			}

			// Feature B: in quiet mode, show the consolidated end summary +
			// an actionable attention block in place of per-section reports.
			if quietMode {
				printQuietSyncSummary(progressOut, repsBySection, m)
			} else {
				// Non-interactive: print the consolidated end summary (sections
				// were already printed inside runSyncSections).
				printEndSummary(progressOut, repsBySection)
			}

			// ── Feature A: guided dry-run → confirm → apply ───────────────────
			if wantsInteraction && !apply {
				// Build a fresh prompter for the post-dry-run confirm.
				postPrompt := NewPrompterCtx(ctx, os.Stdin, cmd.ErrOrStderr())
				doApply, askErr := askApplyAfterDryRun(postPrompt, cmd.ErrOrStderr(), MigrateWalkthroughResult{
					SourceOrg: sourceOrg, DestOrg: destOrg,
					SkipContexts: skipContexts, SkipProjects: skipProjects,
					SkipOrgSettings: skipOrgSettings, SkipExtras: skipExtras,
					SkipOrb: skipOrb, SkipRunner: skipRunner,
					OrbNamespace: orbNamespace, DestOrbNamespace: destOrbNamespace,
					RunnerNamespace: runnerNamespace, DestRunnerNamespace: destRunnerNamespace,
					TransferSecrets: transferSecrets, DestTokenContext: destTokenContext,
					IncludeProjectVars: includeProjectVars, IncludeSSHKeys: includeSSHKeys,
					RemoveRestrictions: removeRestrictions, HostProject: transferHostProj,
					SecretsPath: secretsPath,
				})
				if askErr != nil {
					if errors.Is(askErr, context.Canceled) || errors.Is(askErr, context.DeadlineExceeded) {
						fmt.Fprintln(cmd.ErrOrStderr(), "\nAborted.")
						return askErr
					}
					return askErr
				}
				if doApply {
					apply = true
					fmt.Fprintln(cmd.ErrOrStderr(), "")
					fmt.Fprintln(cmd.ErrOrStderr(), "Applying changes to destination org...")
					applyReps, applyErr := runSyncSections(true /* apply */)
					if applyErr != nil {
						return applyErr
					}
					if quietMode {
						printQuietSyncSummary(progressOut, applyReps, m)
					} else {
						printEndSummary(progressOut, applyReps)
					}
					repsBySection = applyReps
				} else {
					fmt.Fprintln(cmd.ErrOrStderr(), "")
					fmt.Fprintln(cmd.ErrOrStderr(), "No changes applied. To apply later, re-run with:")
					fmt.Fprintf(cmd.ErrOrStderr(), "  circleci-migrate migrate --source-org %s --dest-org %s --apply\n", sourceOrg, destOrg)
					return nil
				}
			}

			// ── Step 3 (opt-in): in-pipeline secrets transfer ─────────────────
			// Only runs when --transfer-secrets is set AND apply is true
			// (dry-run does not trigger the transfer pipeline).
			if transferSecrets && apply {
				if err := runMigrateSecretsTransfer(cmd, cfg, m, srcToken, sourceOrg, destOrg, destTokenContext, transferHostProj, true, includeProjectVars, includeSSHKeys, removeRestrictions); err != nil {
					return err
				}
			} else if transferSecrets && !apply {
				// Non-interactive dry-run: skip actual transfer.
				_ = repsBySection // referenced to avoid unused-var lint
			}

			// ── Post-apply parity validation ──────────────────────────────────
			// Runs after a real apply completes (not dry-run), human output only
			// (skipped when --json or --skip-validate is set). Best-effort: any
			// export/compare failure is printed as a warning; migrate never exits
			// non-zero solely because of a parity gap.
			if apply && !jsonOutput && !skipValidate {
				runPostMigrateValidation(ctx, cmd, cfg, m, dstToken, destOrg,
					destRunnerNamespace, destOrbNamespace, mapping)
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
	f.StringVar(&orbNamespace, "orb-namespace", "",
		"Source orb namespace to capture published orbs from (e.g. 'acme'). "+
			"Both public and private orbs are captured along with every stable version and its raw YAML source. "+
			"The namespace must be supplied explicitly — there is no clean org→namespace lookup.")
	f.StringVar(&destOrbNamespace, "dest-orb-namespace", "",
		"Destination orb namespace to republish captured orb versions into (e.g. 'acme-new'). "+
			"Must be supplied explicitly — the syncer never guesses the destination namespace. "+
			"When omitted and the manifest contains orbs, each is flagged for manual recreation.")
	f.BoolVar(&skipOrb, "skip-orb", false, "Skip exporting and syncing orbs")
	f.BoolVar(&createProjectTokens, "create-project-tokens", false,
		"When set AND --apply, recreate each captured project API token on the destination project. "+
			"CAUTION: each recreated token mints a NEW one-time secret — every consumer of the old token "+
			"must be repointed to the new value. New plaintext values are printed to stderr once and cannot "+
			"be retrieved again. Default false: emit manual steps only.")
	f.BoolVar(&includeDangerFlags, "include-danger-flags", false,
		"Write the 'danger' feature flags (org: drop_all_build_requests, "+
			"require_context_group_restriction; project: drop_all_build_requests) to the destination. "+
			"Default false: these are skipped (and surfaced as a manual step only when the source value is "+
			"true) because enabling them on a freshly-migrated org/project can freeze or break pipelines. "+
			"Set this for a faithful migration once the destination is validated and ready.")
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
	f.BoolVar(&removeRestrictions, "remove-restrictions", false,
		"When --transfer-secrets is set, temporarily remove project/expression restrictions from "+
			"source contexts before the transfer pipeline runs, then restore them afterwards. "+
			"Use when a context has restrictions that prevent the host project from using it. "+
			"Group restrictions (including the default 'All members') are never removed.")
	f.BoolVar(&skipValidate, "skip-validate", false,
		"Skip the automatic post-apply parity check that runs after a successful --apply. "+
			"Validation is also skipped when --json is set (to keep JSON output clean). "+
			"Use --skip-validate in CI pipelines where you run 'validate' as a separate step "+
			"or when re-export of the destination org is not desirable immediately after apply.")

	return cmd
}

// migrateComponents is the ordered list of migration components shown during
// the interactive walkthrough.
var migrateComponents = []string{
	"contexts",
	"projects",
	"org settings",
	"extras (checkout keys, webhooks, schedules)",
	"orbs",
	"runners (self-hosted runner resource classes)",
}

// MigrateWalkthroughResult holds all values returned by the interactive guided
// walkthrough.  Using a struct instead of a long positional tuple makes the
// call site self-documenting and lets the walkthrough return new fields without
// breaking every caller.
type MigrateWalkthroughResult struct {
	SourceOrg string
	DestOrg   string

	// --- secrets (one of the three paths below is active) ---

	// SecretsPath is non-empty when the user chose the captured-bundle path.
	SecretsPath string
	// Missing controls how variables with no captured value are handled
	// (syncer.MissingSkip or syncer.MissingPlaceholder).  Always set, even for
	// the in-pipeline path (where it is forced to MissingSkip).
	Missing string

	// TransferSecrets is true when the user chose the in-pipeline transfer path.
	TransferSecrets bool
	// DestTokenContext is the source-org context holding CIRCLECI_DEST_TOKEN.
	// Required when TransferSecrets is true.
	DestTokenContext string
	// IncludeProjectVars controls whether project env-var values are also
	// transferred in-pipeline.
	IncludeProjectVars bool
	// IncludeSSHKeys controls whether additional project SSH keys are
	// transferred in-pipeline.
	IncludeSSHKeys bool
	// RemoveRestrictions controls whether context restrictions are temporarily
	// lifted on the source during the transfer pipeline.
	RemoveRestrictions bool
	// HostProject is the optional source project slug to host the transfer
	// pipeline.  Empty string means auto-pick.
	HostProject string

	// --- mode ---
	Apply           bool
	Yes             bool
	SkipContexts    bool
	SkipProjects    bool
	SkipOrgSettings bool
	SkipExtras      bool
	SkipOrb         bool
	SkipRunner      bool

	// --- orb / runner namespaces (set by guided mode when selected) ---
	OrbNamespace        string
	DestOrbNamespace    string
	RunnerNamespace     string
	DestRunnerNamespace string
}

// valueMethodInPipeline is the display label for the recommended in-pipeline
// transfer option in the Step 3a choice.
const valueMethodInPipeline = "in-pipeline transfer (RECOMMENDED)"

// valueMethodBundle is the display label for the captured-bundle option.
const valueMethodBundle = "captured secrets bundle (advanced)"

// valueMethodNone is the display label for the structure-only option.
const valueMethodNone = "none — migrate structure only; set values manually later"

// runMigrateWalkthrough conducts the interactive guided migration walkthrough.
// It writes prompts to cmd.ErrOrStderr() and reads answers from os.Stdin.
//
// The function delegates to RunMigrateWalkthroughWith so that tests can inject
// synthetic I/O via NewPrompterCtx without spawning a real TTY.
func runMigrateWalkthrough(
	cmd *cobra.Command,
	cfg *settings.Config,
	sourceOrg, destOrg string,
	yes bool,
) (MigrateWalkthroughResult, error) {
	// cmd.Context() returns nil for a cobra command that has not yet been
	// executed (e.g. in unit tests that call runMigrateWalkthrough directly
	// without going through Execute).  Substitute context.Background() so that
	// NewPrompterCtx always receives a non-nil context.
	cmdCtx := cmd.Context()
	if cmdCtx == nil {
		cmdCtx = context.Background()
	}
	return RunMigrateWalkthroughWith(
		NewPrompterCtx(cmdCtx, os.Stdin, cmd.ErrOrStderr()),
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
//
// srcNamespaceDefault and dstNamespaceDefault are the resolved registry
// namespaces to use as defaults for orb/runner namespace prompts (feature E).
// Pass "" to fall back to the org short-name heuristic.
func RunMigrateWalkthroughWith(
	p *Prompter,
	cmd *cobra.Command,
	cfg *settings.Config,
	sourceOrg, destOrg string,
	yes bool,
	namespaceDefaults ...string,
) (MigrateWalkthroughResult, error) {
	var srcNamespaceDefault, dstNamespaceDefault string
	if len(namespaceDefaults) >= 1 {
		srcNamespaceDefault = namespaceDefaults[0]
	}
	if len(namespaceDefaults) >= 2 {
		dstNamespaceDefault = namespaceDefaults[1]
	}
	out := p.out
	var result MigrateWalkthroughResult
	result.Yes = yes

	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "╔══════════════════════════════════════════════════╗")
	fmt.Fprintln(out, "║   CircleCI Organization Migration — guided mode  ║")
	fmt.Fprintln(out, "╚══════════════════════════════════════════════════╝")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "Tip: re-run with --source-org and --dest-org to skip these prompts.")

	// --- 1. Org slugs --------------------------------------------------------
	printStepHeader(out, 1, 3, "Source and destination organizations")
	fmt.Fprintln(out, "  Slug format: gh/<org>  or  circleci/<org-id>")

	if sourceOrg == "" {
		var err error
		sourceOrg, err = p.askRequired("Source org slug", "e.g. gh/acme")
		if err != nil {
			return result, err
		}
	} else {
		fmt.Fprintf(out, "  Source org:      %s  (from --source-org)\n", sourceOrg)
	}

	if destOrg == "" {
		var err error
		destOrg, err = p.askRequired("Destination org slug", "e.g. gh/acme-new")
		if err != nil {
			return result, err
		}
	} else {
		fmt.Fprintf(out, "  Destination org: %s  (from --dest-org)\n", destOrg)
	}
	result.SourceOrg = sourceOrg
	result.DestOrg = destOrg

	// --- 2. Tokens -----------------------------------------------------------
	printStepHeader(out, 2, 3, "API tokens")
	fmt.Fprintln(out, "  Token input is hidden when running on an interactive terminal.")

	srcToken := cfg.SourceTokenOrDefault()
	if srcToken == "" {
		var err error
		srcToken, err = p.askSecretRequired("Source API token (CIRCLECI_SOURCE_TOKEN)")
		if err != nil {
			return result, err
		}
		cfg.SourceToken = srcToken
	} else {
		fmt.Fprintln(out, "  Source token:      already set via flag or environment variable")
	}

	dstToken := cfg.DestTokenOrDefault()
	if dstToken == "" {
		var err error
		dstToken, err = p.askSecretRequired("Destination API token (CIRCLECI_DEST_TOKEN)")
		if err != nil {
			return result, err
		}
		cfg.DestToken = dstToken
	} else {
		fmt.Fprintln(out, "  Destination token: already set via flag or environment variable")
	}

	// --- Namespace auto-detect (Feature E) -----------------------------------
	// Best-effort: try to resolve the source and destination orb namespace from
	// the registered orbs.  This runs after the token step so that we have a
	// valid source token.  Failures (network, 2 s timeout, empty namespace) fall
	// back gracefully to the orgShortName heuristic used in the namespace prompts.
	// Only override the caller-supplied defaults when the auto-detect finds a value.
	if srcNamespaceDefault == "" && srcToken != "" {
		// Build a short-timeout context for the lookup so we never block the
		// walkthrough more than 2 seconds.
		nsCtx, nsCancel := context.WithTimeout(p.ctx, 2*time.Second)
		srcOrgClient, nsErr := org.NewClient(cfg, srcToken)
		if nsErr == nil {
			srcOrgID, idErr := srcOrgClient.ResolveOrgID(nsCtx, sourceOrg)
			if idErr == nil && srcOrgID != "" {
				srcNamespaceDefault = resolveOrgNamespace(nsCtx, cfg, srcToken, srcOrgID)
			}
		}
		nsCancel()
	}
	if dstNamespaceDefault == "" && dstToken != "" {
		nsCtx, nsCancel := context.WithTimeout(p.ctx, 2*time.Second)
		dstOrgClient, nsErr := org.NewClient(cfg, dstToken)
		if nsErr == nil {
			dstOrgID, idErr := dstOrgClient.ResolveOrgID(nsCtx, destOrg)
			if idErr == nil && dstOrgID != "" {
				dstNamespaceDefault = resolveOrgNamespace(nsCtx, cfg, dstToken, dstOrgID)
			}
		}
		nsCancel()
	}

	// --- 3. What to migrate --------------------------------------------------
	printStepHeader(out, 3, 3, "What to migrate")

	chosen, err := p.askMultiSelect(
		"Select components to migrate (default: all):",
		migrateComponents,
	)
	if err != nil {
		return result, err
	}

	// Map selection back to skip flags.  Start by skipping everything, then
	// un-skip whatever the user chose.
	result.SkipContexts = true
	result.SkipProjects = true
	result.SkipOrgSettings = true
	result.SkipExtras = true
	result.SkipOrb = true
	result.SkipRunner = true
	wantsOrbs := false
	wantsRunners := false
	for _, c := range chosen {
		switch c {
		case migrateComponents[0]: // contexts
			result.SkipContexts = false
		case migrateComponents[1]: // projects
			result.SkipProjects = false
		case migrateComponents[2]: // org settings
			result.SkipOrgSettings = false
		case migrateComponents[3]: // extras
			result.SkipExtras = false
		case migrateComponents[4]: // orbs
			result.SkipOrb = false
			wantsOrbs = true
		case migrateComponents[5]: // runners
			result.SkipRunner = false
			wantsRunners = true
		}
	}

	// --- Step 3 namespace prompts (orbs / runners) ---------------------------
	// When orbs are selected, prompt for source and destination namespaces.
	// Use the resolved registry namespace (feature E) when available; fall
	// back to the org short-name (e.g. "acme" from "gh/acme"); for
	// circleci/<uuid> orgs the fallback is empty so the user types the value.
	if wantsOrbs {
		fmt.Fprintln(out, "")
		fmt.Fprintln(out, "  Most orgs use their org name as the orb namespace.")
		fmt.Fprintln(out, "  Leave blank to skip orbs.")
		srcOrbDefault := srcNamespaceDefault
		if srcOrbDefault == "" {
			srcOrbDefault = orgShortName(sourceOrg)
		}
		result.OrbNamespace, err = p.askWithDefault("Source orb namespace", srcOrbDefault)
		if err != nil {
			return result, err
		}
		if result.OrbNamespace == "" {
			// User cleared the value — treat as skipped.
			result.SkipOrb = true
		} else {
			dstOrbDefault := dstNamespaceDefault
			if dstOrbDefault == "" {
				dstOrbDefault = orgShortName(destOrg)
			}
			result.DestOrbNamespace, err = p.askWithDefault("Destination orb namespace", dstOrbDefault)
			if err != nil {
				return result, err
			}
			if result.DestOrbNamespace == "" {
				result.SkipOrb = true
				result.OrbNamespace = ""
			}
		}
	}

	// When runners are selected, prompt for source and destination namespaces
	// with the same defaulting logic.
	if wantsRunners {
		fmt.Fprintln(out, "")
		fmt.Fprintln(out, "  Most orgs use their org name as the runner namespace.")
		fmt.Fprintln(out, "  Leave blank to skip runners.")
		srcRunnerDefault := srcNamespaceDefault
		if srcRunnerDefault == "" {
			srcRunnerDefault = orgShortName(sourceOrg)
		}
		result.RunnerNamespace, err = p.askWithDefault("Source runner namespace", srcRunnerDefault)
		if err != nil {
			return result, err
		}
		if result.RunnerNamespace == "" {
			result.SkipRunner = true
		} else {
			dstRunnerDefault := dstNamespaceDefault
			if dstRunnerDefault == "" {
				dstRunnerDefault = orgShortName(destOrg)
			}
			result.DestRunnerNamespace, err = p.askWithDefault("Destination runner namespace", dstRunnerDefault)
			if err != nil {
				return result, err
			}
			if result.DestRunnerNamespace == "" {
				result.SkipRunner = true
				result.RunnerNamespace = ""
			}
		}
	}

	// --- Step 3a. How to move secret values ----------------------------------
	printSubStepHeader(out, "3a", 3, "Secret values")
	fmt.Fprintln(out, "  How do you want to move secret VALUES to the destination?")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "  in-pipeline transfer (RECOMMENDED)")
	fmt.Fprintln(out, "    Runs a pipeline in the SOURCE org that pushes context and (optionally)")
	fmt.Fprintln(out, "    project env-var values and SSH keys directly to the destination.")
	fmt.Fprintln(out, "    No plaintext is written to disk. Requires a destination API token")
	fmt.Fprintln(out, "    stored in a source-org context (CIRCLECI_DEST_TOKEN).")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "  captured secrets bundle (advanced)")
	fmt.Fprintln(out, "    Supply a secrets.json produced by 'secrets capture'.")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "  none")
	fmt.Fprintln(out, "    Migrate structure only; set values manually later.")

	secretsMethod, err := p.askChoice(
		"Choose secrets migration method:",
		[]string{valueMethodInPipeline, valueMethodBundle, valueMethodNone},
	)
	if err != nil {
		return result, err
	}

	switch secretsMethod {
	case valueMethodInPipeline:
		// In-pipeline transfer path.
		result.TransferSecrets = true

		// Required: name of the source-org context holding CIRCLECI_DEST_TOKEN.
		// Feature C: explicit preamble rather than "(e.g. ...)" as a default.
		fmt.Fprintln(out, "")
		fmt.Fprintln(out, "  Create a context in the SOURCE org containing an API token with admin")
		fmt.Fprintln(out, "  access to the DESTINATION org, stored as the variable CIRCLECI_DEST_TOKEN.")
		fmt.Fprintln(out, "  Enter that context's name:")
		result.DestTokenContext, err = p.askRequired(
			"Name of a source-org context that holds a destination CircleCI API token",
			"",
		)
		if err != nil {
			return result, err
		}

		// Optional: also transfer project env-var values.
		result.IncludeProjectVars, err = p.askBool(
			"Also transfer project environment-variable values?", true,
		)
		if err != nil {
			return result, err
		}

		// Optional: also transfer additional project SSH keys.
		result.IncludeSSHKeys, err = p.askBool(
			"Also transfer additional project SSH keys?", true,
		)
		if err != nil {
			return result, err
		}

		// Optional: temporarily lift context restrictions during transfer.
		fmt.Fprintln(out, "")
		fmt.Fprintln(out, "  Note: contexts with project or expression restrictions block the transfer")
		fmt.Fprintln(out, "  pipeline unless the host project is in the allowed set.  Answering 'yes'")
		fmt.Fprintln(out, "  temporarily removes those restrictions and restores them after transfer.")
		result.RemoveRestrictions, err = p.askBool(
			"Temporarily lift context restrictions on the source during transfer (restored afterward)?",
			true,
		)
		if err != nil {
			return result, err
		}

		// Optional: host project override (empty = auto-pick).
		// Feature D: explain what the host project does instead of "(blank = auto-pick)".
		fmt.Fprintln(out, "")
		fmt.Fprintln(out, "  Pick a SOURCE project under which to run the secrets-extraction pipeline")
		fmt.Fprintln(out, "  (it triggers a short-lived pipeline there to read context/project values).")
		fmt.Fprintln(out, "  Leave blank to auto-pick an established project.")
		result.HostProject, err = p.askWithDefault(
			"Source project slug for the secrets pipeline (blank = auto-pick)", "",
		)
		if err != nil {
			return result, err
		}

		// Values flow through the pipeline — skip the missing-secrets step.
		result.SecretsPath = ""
		result.Missing = syncer.MissingSkip

	case valueMethodBundle:
		// Captured-bundle path — existing behaviour.
		result.SecretsPath, err = p.askWithDefault("Path to secrets bundle", "secrets.json")
		if err != nil {
			return result, err
		}

		// --- Step 3b. Missing secrets handling (bundle / none paths only) ----
		printSubStepHeader(out, "3b", 3, "Missing secret values")
		fmt.Fprintln(out, "  Variables not found in the bundle can be skipped or written as placeholders.")
		var missingChoice string
		missingChoice, err = p.askChoice(
			"How should missing secret values be handled?",
			[]string{syncer.MissingSkip, syncer.MissingPlaceholder},
		)
		if err != nil {
			return result, err
		}
		result.Missing = missingChoice

	default: // valueMethodNone
		// Structure-only path.
		result.SecretsPath = ""

		// --- Step 3b. Missing secrets handling (bundle / none paths only) ----
		printSubStepHeader(out, "3b", 3, "Missing secret values")
		fmt.Fprintln(out, "  Variables not found in the bundle can be skipped or written as placeholders.")
		var missingChoice string
		missingChoice, err = p.askChoice(
			"How should missing secret values be handled?",
			[]string{syncer.MissingSkip, syncer.MissingPlaceholder},
		)
		if err != nil {
			return result, err
		}
		result.Missing = missingChoice
	}

	// End-of-walkthrough pointer to advanced flags not covered by the prompts.
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "  Advanced options not covered above (set via flags, re-run with --help):")
	fmt.Fprintln(out, "    --dest-github-org   when repos moved to a new GitHub org (App orgs)")
	fmt.Fprintln(out, "    --mapping           per-project source->destination slug overrides")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "  Terraform alternative (IaC path):")
	fmt.Fprintln(out, "    To manage contexts, projects, webhooks, runners, and pipelines as Terraform")
	fmt.Fprintln(out, "    code, run 'terraform generate' after export and apply with terraform, then")
	fmt.Fprintln(out, "    re-run sync with --skip-terraform-managed to fill in CLI-only gaps:")
	fmt.Fprintln(out, "      circleci-migrate terraform generate --manifest manifest.json --dest-org-id <uuid> --out ./terraform/")
	fmt.Fprintln(out, "      cd ./terraform/ && terraform init && terraform plan && terraform apply")
	fmt.Fprintln(out, "      circleci-migrate sync --manifest manifest.json --dest-token $CIRCLECI_DEST_TOKEN --apply --skip-terraform-managed")

	// In guided mode, Apply is NOT set here — the RunE will run a dry-run first,
	// show a summary, and then ask "Apply now?" (feature A).
	// result.Apply stays false (zero value).

	return result, nil
}

// askApplyAfterDryRun prompts the user (post dry-run) whether to apply changes.
// It prints a concise plan summary before asking.
// Returns true if the user confirms apply, false otherwise.
func askApplyAfterDryRun(p *Prompter, out io.Writer, wt MigrateWalkthroughResult) (bool, error) {
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "─────────────────────────────────────────────────────────────")
	fmt.Fprintln(out, "  Dry-run complete (see summary above).")
	fmt.Fprintln(out, "")
	fmt.Fprintf(out, "  Source:      %s\n", wt.SourceOrg)
	fmt.Fprintf(out, "  Destination: %s\n", wt.DestOrg)
	selected := componentsLabel(wt.SkipContexts, wt.SkipProjects, wt.SkipOrgSettings, wt.SkipExtras, wt.SkipOrb, wt.SkipRunner)
	fmt.Fprintf(out, "  Migrating:   %s\n", selected)
	if !wt.SkipOrb {
		fmt.Fprintf(out, "  Orbs:        %s → %s\n", wt.OrbNamespace, wt.DestOrbNamespace)
	}
	if !wt.SkipRunner {
		fmt.Fprintf(out, "  Runners:     %s → %s\n", wt.RunnerNamespace, wt.DestRunnerNamespace)
	}
	// Secrets path summary.
	switch {
	case wt.TransferSecrets:
		hostLabel := "auto"
		if wt.HostProject != "" {
			hostLabel = wt.HostProject
		}
		fmt.Fprintf(out,
			"  Secrets:     in-pipeline transfer via context %q (project-vars: %s, ssh-keys: %s, remove-restrictions: %s, host: %s)\n",
			wt.DestTokenContext,
			yesNo(wt.IncludeProjectVars),
			yesNo(wt.IncludeSSHKeys),
			yesNo(wt.RemoveRestrictions),
			hostLabel,
		)
	case wt.SecretsPath != "":
		fmt.Fprintf(out, "  Secrets:     bundle at %q\n", wt.SecretsPath)
	default:
		fmt.Fprintln(out, "  Secrets:     none (structure only)")
	}
	fmt.Fprintln(out, "─────────────────────────────────────────────────────────────")

	return p.askBool("Apply these changes to the destination now?", false)
}

// yesNo returns "yes" or "no" for a boolean, used in the apply summary.
func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// componentsLabel builds a short human-readable list of selected migration
// components, used in the apply confirmation summary.
func componentsLabel(skipContexts, skipProjects, skipOrgSettings, skipExtras, skipOrb, skipRunner bool) string {
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
	if !skipOrb {
		parts = append(parts, "orbs")
	}
	if !skipRunner {
		parts = append(parts, "runners")
	}
	if len(parts) == 0 {
		return "(none)"
	}
	return strings.Join(parts, ", ")
}

// orgShortName extracts the short org name from a slug like "gh/acme" → "acme".
// For "circleci/<uuid>" orgs (App/standalone), the UUID is not a usable
// namespace name, so we return empty string to let the user type the value.
func orgShortName(slug string) string {
	parts := strings.SplitN(slug, "/", 2)
	if len(parts) != 2 || parts[1] == "" {
		return ""
	}
	// circleci/<uuid> orgs: the second segment is a UUID — not a namespace name.
	if parts[0] == "circleci" {
		return ""
	}
	return parts[1]
}

// countManifestVars returns (contextVarCount, projectVarCount) from a manifest.
// Used for the quiet one-line export summary (feature B).
func countManifestVars(m *manifest.Manifest) (int, int) {
	cv := 0
	for _, c := range m.Contexts {
		cv += len(c.EnvVars)
	}
	pv := 0
	for _, p := range m.Projects {
		pv += len(p.EnvVars)
	}
	return cv, pv
}

// printQuietSyncSummary prints the consolidated end-of-run totals and an
// actionable attention block listing only items that need manual action.
// This is the quiet-mode equivalent of per-section printSyncReport calls.
// Feature B: condensed sync output for guided runs.
func printQuietSyncSummary(out io.Writer, repsBySection map[string]*syncer.Report, m *manifest.Manifest) {
	if len(repsBySection) == 0 {
		return
	}
	ren := ui.New(out)
	var tc ui.TotalCounts
	var attention []struct {
		section string
		action  syncer.Action
	}

	sectionOrder := []string{"Org Settings", "Contexts", "Projects", "Context Project Restrictions", "Runner Resource Classes", "CIAM", "Orbs"}
	for _, section := range sectionOrder {
		rep, ok := repsBySection[section]
		if !ok || rep == nil {
			continue
		}
		tc.Add(ui.Counts(rep.Counts()))
		for _, a := range rep.Actions {
			if a.Status == "manual" || a.Status == "error" {
				attention = append(attention, struct {
					section string
					action  syncer.Action
				}{section, a})
			}
		}
	}

	ren.EndSummary(tc)

	if len(attention) > 0 {
		items := make([]ui.AttentionItem, 0, len(attention))
		for _, aa := range attention {
			line := syncActionLine(aa.action, "", m)
			if line == "" {
				line = aa.action.Target
			}
			items = append(items, ui.AttentionItem{
				Status: aa.action.Status,
				Label:  "[" + aa.section + "] " + line,
				Detail: aa.action.Detail,
			})
		}
		ren.AttentionBlock(items)
	}
}

// resolveOrgNamespace attempts to detect the org's registered orb namespace by:
//  1. GET /api/private/orb?org-id=<orgID> to find the first orb UUID
//  2. Use the orb client's ListOrbs path to find the namespace name
//
// This is a best-effort, time-bounded operation: if anything fails or takes
// longer than maxWait, it returns "" so the caller falls back to orgShortName.
// Feature E: org → namespace auto-detect.
func resolveOrgNamespace(ctx context.Context, cfg *settings.Config, token, orgID string) string {
	if orgID == "" {
		return ""
	}

	type privOrbItem struct {
		OrbID string `json:"orb_id"` //nolint:tagliatelle
	}
	type privOrbResp struct {
		Orbs []privOrbItem `json:"orbs"`
	}

	// Build a short-timeout context so we never block the prompt more than ~2s.
	tCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	// Resolve the host (default: circleci.com).
	host := cfg.Host
	if host == "" {
		host = "https://circleci.com"
	}

	privURL := host + "/api/private/orb?org-id=" + orgID

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 2 * time.Second}
	}

	req, err := http.NewRequestWithContext(tCtx, http.MethodGet, privURL, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("Circle-Token", token)

	resp, err := httpClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return ""
	}

	var privResp privOrbResp
	if decErr := json.NewDecoder(resp.Body).Decode(&privResp); decErr != nil {
		return ""
	}
	if len(privResp.Orbs) == 0 {
		return ""
	}
	orbID := privResp.Orbs[0].OrbID
	if orbID == "" {
		return ""
	}

	// Resolve the namespace via the orb client: list orbs for the namespace
	// that owns this orb. We use ListOrbs with the namespace ID, but we first
	// need the namespace ID. Since we have an orb ID, we can call ListOrbs
	// using the namespaceID from the orb package record.
	// Rather than a full graph traversal, use the simpler approach:
	// list orbs for the org's namespace by querying the orb packages endpoint
	// with the orb ID directly via the existing orb client.
	orbClient, oErr := apiOrb.NewClient(cfg, token)
	if oErr != nil {
		return ""
	}

	// We need to find the namespace from the orb ID. The orb v3 API returns
	// namespace ID in the package item, but we need to match by orb ID.
	// Use a lightweight approach: list orbs for namespace IDs by resolving the
	// orb package list scoped to the known orb ID via filter[orb_id].
	// Since ListOrbs requires a namespaceID, we instead query the private orb
	// list endpoint to get the namespace name directly.
	type privOrbDetail struct {
		Namespace struct {
			Name string `json:"name"`
		} `json:"namespace"`
	}
	type privOrbDetailResp struct {
		Orb privOrbDetail `json:"orb"`
	}

	detailURL := host + "/api/private/orb/" + orbID
	req2, err := http.NewRequestWithContext(tCtx, http.MethodGet, detailURL, nil)
	if err != nil {
		return ""
	}
	req2.Header.Set("Circle-Token", token)

	resp2, err := httpClient.Do(req2)
	if err != nil || resp2.StatusCode != http.StatusOK {
		if resp2 != nil {
			resp2.Body.Close() //nolint:errcheck
		}
		// Fall back: use the orb client to list all orbs and find the namespace.
		// We need the namespace UUID from the orb package. Since we cannot get it
		// without another API call, just return "" and fall back to orgShortName.
		_ = orbClient
		return ""
	}
	defer resp2.Body.Close() //nolint:errcheck

	var detailResp privOrbDetailResp
	if decErr := json.NewDecoder(resp2.Body).Decode(&detailResp); decErr != nil {
		return ""
	}
	return detailResp.Orb.Namespace.Name
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
//
// The org slugs are normalized to their canonical VCS prefix (github/→gh/,
// bitbucket/→bb/) so the derived mapping matches the canonical project slugs in
// the manifest (always "gh/…"/"bb/…"). Without this, passing
// "--source-org github/acme" produced an Org.From of "github/acme" that failed
// the prefix check in ResolveProjectSlug, silently breaking per-project slug
// remapping (e.g. project-type context restrictions fell back to "manual").
func BuildMigrateMapping(mappingPath, srcOrg, dstOrg string) (*manifest.Mapping, error) {
	if mappingPath != "" {
		return manifest.LoadMapping(mappingPath)
	}
	return &manifest.Mapping{
		Org: manifest.OrgMapping{From: normalizeVCSPrefix(srcOrg), To: normalizeVCSPrefix(dstOrg)},
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
	apply, includeProjectVars, includeSSHKeys, removeRestrictions bool,
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

	// Build the context client for restriction management when --remove-restrictions is set.
	var ctxClient transfer.ContextRestrictionManager
	if removeRestrictions {
		cc, ccErr := cctx.NewClient(cfg, srcToken)
		if ccErr != nil {
			return fmt.Errorf("creating context client for --remove-restrictions: %w", ccErr)
		}
		ctxClient = cc
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
		RemoveRestrictions: removeRestrictions,
		ContextClient:      ctxClient,
		DryRun:             !apply,
		PollTimeout:        30 * time.Minute,
		Stdout:             cmd.OutOrStdout(),
		Stderr:             stderr,
	}

	return transfer.Transfer(cmd.Context(), projClient, m, opts)
}

// runPostMigrateValidation exports the destination org and runs a parity check
// against the source manifest, printing the human-readable report to cmd's
// stdout.  It is best-effort: any error is printed as a warning and the
// function returns nil so that migration success is never masked.
//
// This function is only called when:
//   - a real apply was performed (not a dry-run)
//   - --json is NOT set (validation output would corrupt the JSON stream)
//   - --skip-validate is NOT set
func runPostMigrateValidation(
	ctx context.Context,
	cmd *cobra.Command,
	cfg *settings.Config,
	srcManifest *manifest.Manifest,
	dstToken, destOrg, destRunnerNamespace, destOrbNamespace string,
	mapping *manifest.Mapping,
) {
	stderr := cmd.ErrOrStderr()
	stdout := cmd.OutOrStdout()

	fmt.Fprintln(stderr, "")
	fmt.Fprintln(stderr, "── Post-migration validation ─────────────────────────────")
	fmt.Fprintf(stderr, "Exporting destination org %s...\n", destOrg)

	dstManifest, err := exportDestManifest(ctx, cfg, dstToken, destOrg, destRunnerNamespace, destOrbNamespace, stderr)
	if err != nil {
		fmt.Fprintf(stderr, "post-migration validation skipped: %v\n", err)
		return
	}

	result := validate.Compare(srcManifest, dstManifest, mapping, validate.Options{
		DestRunnerNamespace: destRunnerNamespace,
		DestOrbNamespace:    destOrbNamespace,
	})

	var b strings.Builder
	printValidateReport(result, &b)
	fmt.Fprint(stdout, b.String())

	_, missing, manual := validateTotals(result)
	total := missing + manual
	if total > 0 {
		fmt.Fprintf(stdout, "Post-migration validation found %d item(s) needing attention — see above.\n", total)
	}
}
