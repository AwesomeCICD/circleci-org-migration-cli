package cmd

import (
	"fmt"
	"os"
	"strings"
	"time"

	cctx "github.com/CircleCI-Public/circleci-org-migration-cli/api/context"
	"github.com/CircleCI-Public/circleci-org-migration-cli/api/org"
	"github.com/CircleCI-Public/circleci-org-migration-cli/api/project"
	"github.com/CircleCI-Public/circleci-org-migration-cli/api/runner"
	"github.com/CircleCI-Public/circleci-org-migration-cli/internal/exporter"
	"github.com/CircleCI-Public/circleci-org-migration-cli/internal/manifest"
	"github.com/CircleCI-Public/circleci-org-migration-cli/internal/report"
	"github.com/CircleCI-Public/circleci-org-migration-cli/internal/syncer"
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
		output              string
		reportPath          string
		runnerNamespace     string
		destRunnerNamespace string
	)

	cmd := &cobra.Command{
		Use:   "migrate [--source-org <slug> --dest-org <slug>] [--apply]",
		Short: "All-in-one: export source org and sync into destination org.",
		Long: `migrate combines 'export' and 'sync' into a single command.

When run WITHOUT --source-org and --dest-org on an interactive terminal,
migrate launches a guided walkthrough that prompts for each required value and
lets you choose which parts of the org to migrate. This interactive mode is
designed for first-time use and manual one-off migrations.

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
    --apply -o manifest.json --report migration-report.md`,
		RunE: func(cmd *cobra.Command, _ []string) error {
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
				// Non-TTY (piped/CI) with missing required flags: fail clearly.
				if missingSourceOrg {
					return fmt.Errorf("--source-org is required in non-interactive mode " +
						"(e.g. --source-org gh/acme); run without flags on an interactive " +
						"terminal for a guided walkthrough")
				}
				return fmt.Errorf("--dest-org is required in non-interactive mode " +
					"(e.g. --dest-org gh/acme-new); run without flags on an interactive " +
					"terminal for a guided walkthrough")
			}

			if wantsInteraction {
				// Launch interactive walkthrough.
				var err error
				sourceOrg, destOrg, secretsPath, missing, apply, yes,
					skipContexts, skipProjects, skipOrgSettings, skipExtras,
					err = runMigrateWalkthrough(cmd, sourceOrg, destOrg, yes)
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

			srcToken := rootOptions.SourceTokenOrDefault()
			if srcToken == "" {
				return fmt.Errorf("no source API token: set --source-token, --token, CIRCLECI_SOURCE_TOKEN, or CIRCLECI_CLI_TOKEN")
			}
			dstToken := rootOptions.DestTokenOrDefault()
			if dstToken == "" {
				return fmt.Errorf("no destination API token: set --dest-token, --token, CIRCLECI_DEST_TOKEN, or CIRCLECI_CLI_TOKEN")
			}

			// --- step 1: export from source org -------------------------------
			srcOrgClient, err := org.NewClient(rootOptions, srcToken)
			if err != nil {
				return fmt.Errorf("creating source org client: %w", err)
			}
			srcCtxClient, err := cctx.NewClient(rootOptions, srcToken)
			if err != nil {
				return fmt.Errorf("creating source context client: %w", err)
			}
			srcProjClient, err := project.NewClient(rootOptions, srcToken)
			if err != nil {
				return fmt.Errorf("creating source project client: %w", err)
			}

			ex := &exporter.Exporter{
				Org:      srcOrgClient,
				Contexts: srcCtxClient,
				Projects: srcProjClient,
				Out:      cmd.ErrOrStderr(),
			}

			if runnerNamespace != "" {
				srcRunnerClient, rerr := runner.NewClient(rootOptions, srcToken)
				if rerr != nil {
					return fmt.Errorf("creating source runner client: %w", rerr)
				}
				ex.Runner = srcRunnerClient
			}

			m, err := ex.Export(exporter.Options{
				Host:            rootOptions.Host,
				OrgSlug:         sourceOrg,
				IncludeContexts: !skipContexts,
				IncludeProjects: !skipProjects,
				IncludeExtras:   !skipExtras,
				RunnerNamespace: runnerNamespace,
			})
			if err != nil {
				return err
			}
			m.GeneratedAt = time.Now().UTC().Format(time.RFC3339)

			fmt.Fprint(cmd.OutOrStdout(), report.Summary(m))

			// --- optional manifest/report saves (best-effort) -----------------
			if output != "" {
				if saveErr := m.Save(output); saveErr != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "warning: writing manifest: %v\n", saveErr)
				} else {
					fmt.Fprintf(cmd.OutOrStdout(), "Wrote manifest to      %s\n", output)
				}
			}
			if reportPath != "" {
				if saveErr := report.SaveMarkdown(m, reportPath); saveErr != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "warning: writing audit report: %v\n", saveErr)
				} else {
					fmt.Fprintf(cmd.OutOrStdout(), "Wrote audit report to  %s\n", reportPath)
				}
			}

			// --- step 2: sync into destination org ----------------------------
			dstOrgClient, err := org.NewClient(rootOptions, dstToken)
			if err != nil {
				return fmt.Errorf("creating destination org client: %w", err)
			}
			dstCtxClient, err := cctx.NewClient(rootOptions, dstToken)
			if err != nil {
				return fmt.Errorf("creating destination context client: %w", err)
			}
			dstProjClient, err := project.NewClient(rootOptions, dstToken)
			if err != nil {
				return fmt.Errorf("creating destination project client: %w", err)
			}

			mapping, err := BuildMigrateMapping(mappingPath, sourceOrg, destOrg)
			if err != nil {
				return err
			}

			bundle, err := loadBundleIfPresent(secretsPath)
			if err != nil {
				return err
			}

			sy := &syncer.Syncer{
				Org:         dstOrgClient,
				Contexts:    dstCtxClient,
				Projects:    dstProjClient,
				OrgSettings: dstOrgClient,
				Groups:      orgGroupLister{dstOrgClient},
				Out:         cmd.ErrOrStderr(),
			}
			opts := syncer.Options{
				Apply:               apply,
				MissingSecrets:      missing,
				GitHubToken:         githubToken,
				DestGitHubOrg:       destGitHubOrg,
				DestRunnerNamespace: destRunnerNamespace,
			}

			// Wire up the runner client for the destination when needed.
			if destRunnerNamespace != "" || len(m.RunnerResourceClasses) > 0 {
				dstRunnerClient, rerr := runner.NewClient(rootOptions, dstToken)
				if rerr != nil {
					return fmt.Errorf("creating destination runner client: %w", rerr)
				}
				sy.Runner = dstRunnerClient
			}

			if !skipOrgSettings {
				rep, syncErr := sy.SyncOrgSettings(m, mapping, opts)
				if syncErr != nil {
					return syncErr
				}
				printSyncReport(cmd, "Org Settings", rep)
			}
			if !skipContexts {
				rep, syncErr := sy.SyncContexts(m, bundle, mapping, opts)
				if syncErr != nil {
					return syncErr
				}
				printSyncReport(cmd, "Contexts", rep)
			}
			if !skipProjects {
				rep, syncErr := sy.SyncProjects(m, bundle, mapping, opts)
				if syncErr != nil {
					return syncErr
				}
				printSyncReport(cmd, "Projects", rep)
				if enableErr := handleEnableBuilds(cmd, sy, rep, apply, yes); enableErr != nil {
					return enableErr
				}
			}

			// Runner resource classes.
			if len(m.RunnerResourceClasses) > 0 || destRunnerNamespace != "" {
				rep, syncErr := sy.SyncRunnerResourceClasses(m, opts)
				if syncErr != nil {
					return syncErr
				}
				printSyncReport(cmd, "Runner Resource Classes", rep)
			}
			return nil
		},
	}

	f := cmd.Flags()
	f.StringVar(&sourceOrg, "source-org", "",
		"Source organization slug: gh/<org> or circleci/<org-id> (required, or prompted interactively)")
	f.StringVar(&destOrg, "dest-org", "",
		"Destination organization slug: gh/<org> or circleci/<org-id> (required, or prompted interactively)")
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
	f.StringVarP(&output, "output", "o", "",
		"If set, save the exported manifest to this path")
	f.StringVar(&reportPath, "report", "",
		"If set, save the human-readable audit report to this path")
	f.StringVar(&runnerNamespace, "runner-namespace", "",
		"Source runner namespace to capture self-hosted runner resource classes from (e.g. 'acme'). "+
			"The namespace must be supplied explicitly — there is no clean org→namespace lookup.")
	f.StringVar(&destRunnerNamespace, "dest-runner-namespace", "",
		"Destination runner namespace for recreating self-hosted runner resource classes (e.g. 'acme-new'). "+
			"Must be supplied explicitly — the syncer never guesses the destination namespace. "+
			"When omitted and the manifest contains runner classes, each is flagged for manual recreation.")

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
		sourceOrg, destOrg, yes,
	)
}

// RunMigrateWalkthroughWith is the injectable interactive walkthrough used by
// both the command (via runMigrateWalkthrough) and external test files.
// p supplies the I/O streams; cmd is used for printing the apply summary.
func RunMigrateWalkthroughWith(
	p *Prompter,
	cmd *cobra.Command,
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
	fmt.Fprintln(out, "")

	// --- 1. Org slugs --------------------------------------------------------
	fmt.Fprintln(out, "Step 1 of 4 — Source and destination organizations")
	fmt.Fprintln(out, "  Slug format: gh/<org>  or  circleci/<org-id>")
	fmt.Fprintln(out, "")

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
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "Step 2 of 4 — API tokens")
	fmt.Fprintln(out, "  Tokens are read without echo and are never written to logs.")
	fmt.Fprintln(out, "")

	srcToken := rootOptions.SourceTokenOrDefault()
	if srcToken == "" {
		srcToken, err = p.askSecretRequired("Source API token (CIRCLECI_SOURCE_TOKEN)")
		if err != nil {
			return
		}
		rootOptions.SourceToken = srcToken
	} else {
		fmt.Fprintln(out, "  Source token:      already set via flag or environment variable")
	}

	dstToken := rootOptions.DestTokenOrDefault()
	if dstToken == "" {
		dstToken, err = p.askSecretRequired("Destination API token (CIRCLECI_DEST_TOKEN)")
		if err != nil {
			return
		}
		rootOptions.DestToken = dstToken
	} else {
		fmt.Fprintln(out, "  Destination token: already set via flag or environment variable")
	}

	// --- 3. What to migrate --------------------------------------------------
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "Step 3 of 4 — What to migrate")
	fmt.Fprintln(out, "")

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

	// --- 3b. Secrets bundle --------------------------------------------------
	fmt.Fprintln(out, "")
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

	// --- 3c. Missing secrets handling ----------------------------------------
	fmt.Fprintln(out, "")
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
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "Step 4 of 4 — Dry run or apply")
	fmt.Fprintln(out, "  A dry run previews changes without writing anything to the destination.")
	fmt.Fprintln(out, "")

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
