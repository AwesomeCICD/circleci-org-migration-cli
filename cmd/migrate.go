package cmd

import (
	"fmt"
	"os"
	"time"

	cctx "github.com/CircleCI-Public/circleci-org-migration-cli/api/context"
	"github.com/CircleCI-Public/circleci-org-migration-cli/api/org"
	"github.com/CircleCI-Public/circleci-org-migration-cli/api/project"
	"github.com/CircleCI-Public/circleci-org-migration-cli/internal/exporter"
	"github.com/CircleCI-Public/circleci-org-migration-cli/internal/manifest"
	"github.com/CircleCI-Public/circleci-org-migration-cli/internal/report"
	"github.com/CircleCI-Public/circleci-org-migration-cli/internal/syncer"
	"github.com/spf13/cobra"
)

func newMigrateCommand() *cobra.Command {
	var (
		sourceOrg       string
		destOrg         string
		secretsPath     string
		mappingPath     string
		apply           bool
		yes             bool
		missing         string
		githubToken     string
		destGitHubOrg   string
		skipContexts    bool
		skipProjects    bool
		skipOrgSettings bool
		skipExtras      bool
		output          string
		reportPath      string
	)

	cmd := &cobra.Command{
		Use:   "migrate --source-org <slug> --dest-org <slug> [--apply]",
		Short: "All-in-one: export source org and sync into destination org.",
		Long: `migrate combines 'export' and 'sync' into a single command.

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
  circleci-migrate migrate \
    --source-org gh/acme --dest-org gh/acme-new \
    --source-token $SRC_TOKEN --dest-token $DST_TOKEN

  circleci-migrate migrate \
    --source-org gh/acme --dest-org gh/acme-new \
    --secrets secrets.json --apply --yes

  circleci-migrate migrate \
    --source-org gh/acme --dest-org gh/acme-new \
    --apply -o manifest.json --report migration-report.md`,
		RunE: func(cmd *cobra.Command, _ []string) error {
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

			m, err := ex.Export(exporter.Options{
				Host:            rootOptions.Host,
				OrgSlug:         sourceOrg,
				IncludeContexts: !skipContexts,
				IncludeProjects: !skipProjects,
				IncludeExtras:   !skipExtras,
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
				Apply:          apply,
				MissingSecrets: missing,
				GitHubToken:    githubToken,
				DestGitHubOrg:  destGitHubOrg,
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
			return nil
		},
	}

	f := cmd.Flags()
	f.StringVar(&sourceOrg, "source-org", "", "Source organization slug: gh/<org> or circleci/<org-id> (required)")
	f.StringVar(&destOrg, "dest-org", "", "Destination organization slug: gh/<org> or circleci/<org-id> (required)")
	f.StringVar(&secretsPath, "secrets", "secrets.json", "Path to a captured secret bundle (optional)")
	f.StringVar(&mappingPath, "mapping", "", "Path to a source->destination mapping file (optional)")
	f.BoolVar(&apply, "apply", false, "Write changes to the destination (default: dry run)")
	f.BoolVarP(&yes, "yes", "y", false, "Auto-confirm enabling builds after project creation (skip the interactive prompt)")
	f.StringVar(&missing, "missing-secrets", syncer.MissingSkip, "How to handle variables with no captured value: skip|placeholder")
	f.StringVar(&githubToken, "github-token", os.Getenv("GITHUB_TOKEN"),
		"GitHub personal access token used to resolve repository IDs when creating pipeline definitions in a GitHub App destination org. Defaults to $GITHUB_TOKEN. Required when repos have been moved to a new GitHub org (--dest-github-org or mapping github_org).")
	f.StringVar(&destGitHubOrg, "dest-github-org", "",
		"Destination GitHub organization owner (e.g. 'acme-new'). Use when all repos have moved to a new GitHub org. Takes precedence over the source owner when resolving repo external IDs; overridden by an explicit github_org entry in the mapping file. Requires --github-token.")
	f.BoolVar(&skipContexts, "skip-contexts", false, "Skip exporting and syncing contexts")
	f.BoolVar(&skipProjects, "skip-projects", false, "Skip exporting and syncing projects")
	f.BoolVar(&skipOrgSettings, "skip-org-settings", false, "Skip syncing org-level settings (feature flags, OIDC, URL-orb allow list, config policies)")
	f.BoolVar(&skipExtras, "skip-extras", false, "Skip checkout keys, webhooks, and schedules")
	f.StringVarP(&output, "output", "o", "", "If set, save the exported manifest to this path")
	f.StringVar(&reportPath, "report", "", "If set, save the human-readable audit report to this path")

	return cmd
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
