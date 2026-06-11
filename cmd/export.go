package cmd

import (
	"fmt"
	"time"

	cctx "github.com/AwesomeCICD/circleci-org-migration-cli/api/context"
	"github.com/AwesomeCICD/circleci-org-migration-cli/api/org"
	"github.com/AwesomeCICD/circleci-org-migration-cli/api/project"
	"github.com/AwesomeCICD/circleci-org-migration-cli/api/runner"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/exporter"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/report"
	"github.com/spf13/cobra"
)

func newExportCommand() *cobra.Command {
	var (
		orgSlug         string
		output          string
		reportPath      string
		projectSlugs    []string
		projectsAlias   []string // hidden alias --projects (StringSlice back-compat)
		skipContexts    bool
		skipProjects    bool
		skipExtras      bool
		runnerNamespace string
	)

	cmd := &cobra.Command{
		Use:   "export --source-org <org-slug>",
		Short: "Export source-org data to a local manifest file.",
		Long: `export reads configuration from the source CircleCI organization and
writes a non-secret JSON manifest plus a human-readable audit report.

The manifest captures contexts (and their variable names, restrictions, and
security groups), projects (settings, variable names, and metadata), and
org-level settings. It is read-only: it never writes to CircleCI, and it never
contains secret values — those are masked by the API and must be captured with
the in-pipeline secrets step.

The org slug is "gh/<org>" for GitHub OAuth organizations or
"circleci/<org-id>" for GitHub App / GitLab organizations.

Self-hosted runner resource classes live under a namespace on runner.circleci.com.
Pass --runner-namespace to capture them. The namespace must be supplied explicitly
because there is no clean org→namespace lookup in the CircleCI API.

Examples:
  circleci-migrate export --source-org gh/acme --source-token $SRC_TOKEN
  circleci-migrate export --source-org gh/acme -o acme.json --report acme-audit.md
  circleci-migrate export --source-org gh/acme --project gh/acme/web --project gh/acme/api
  circleci-migrate export --source-org gh/acme --runner-namespace acme`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Merge values from hidden alias --projects (StringSlice, comma-or-repeat)
			// into the canonical --project list.
			projectSlugs = append(projectSlugs, projectsAlias...)

			if orgSlug == "" {
				return fmt.Errorf("--source-org is required (e.g. --source-org gh/acme)")
			}
			token := rootOptions.SourceTokenOrDefault()
			if token == "" {
				return fmt.Errorf("no source API token: set --source-token, --token, CIRCLECI_SOURCE_TOKEN, or CIRCLECI_CLI_TOKEN")
			}

			orgClient, err := org.NewClient(rootOptions, token)
			if err != nil {
				return fmt.Errorf("creating org client: %w", err)
			}
			ctxClient, err := cctx.NewClient(rootOptions, token)
			if err != nil {
				return fmt.Errorf("creating context client: %w", err)
			}
			projClient, err := project.NewClient(rootOptions, token)
			if err != nil {
				return fmt.Errorf("creating project client: %w", err)
			}

			ex := &exporter.Exporter{
				Org:      orgClient,
				Contexts: ctxClient,
				Projects: projClient,
				Out:      cmd.ErrOrStderr(),
			}

			if runnerNamespace != "" {
				runnerClient, rerr := runner.NewClient(rootOptions, token)
				if rerr != nil {
					return fmt.Errorf("creating runner client: %w", rerr)
				}
				ex.Runner = runnerClient
			}

			m, err := ex.Export(exporter.Options{
				Host:            rootOptions.Host,
				OrgSlug:         orgSlug,
				ProjectSlugs:    projectSlugs,
				IncludeContexts: !skipContexts,
				IncludeProjects: !skipProjects,
				IncludeExtras:   !skipExtras,
				RunnerNamespace: runnerNamespace,
			})
			if err != nil {
				return err
			}
			m.GeneratedAt = time.Now().UTC().Format(time.RFC3339)

			if err := m.Save(output); err != nil {
				return fmt.Errorf("writing manifest: %w", err)
			}
			if err := report.SaveMarkdown(m, reportPath); err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			fmt.Fprint(out, report.Summary(m))
			fmt.Fprintf(out, "\nWrote manifest to      %s\n", output)
			fmt.Fprintf(out, "Wrote audit report to  %s\n", reportPath)
			return nil
		},
	}

	f := cmd.Flags()

	// Canonical flags (new names).
	f.StringVar(&orgSlug, "source-org", "",
		"Source organization slug: gh/<org> or circleci/<org-id> (required)")
	f.StringVarP(&output, "output", "o", "manifest.json",
		"Path to write the JSON manifest (always written; use -o to change the path)")
	f.StringVar(&reportPath, "report", "migration-report.md",
		"Path to write the human-readable audit report")
	f.StringArrayVar(&projectSlugs, "project", nil,
		"Explicit project slug to export (repeat to export multiple: --project gh/acme/web --project gh/acme/api)")
	f.BoolVar(&skipContexts, "skip-contexts", false, "Skip exporting contexts")
	f.BoolVar(&skipProjects, "skip-projects", false, "Skip exporting projects")
	f.BoolVar(&skipExtras, "skip-extras", false, "Skip checkout keys, webhooks, and schedules")
	f.StringVar(&runnerNamespace, "runner-namespace", "",
		"Source runner namespace to capture self-hosted runner resource classes from (e.g. 'acme'). "+
			"The namespace must be supplied explicitly — there is no clean org→namespace lookup.")

	// Hidden back-compat aliases — old invocations must still work.
	f.StringVar(&orgSlug, "org", "",
		"Deprecated: use --source-org. Source organization slug: gh/<org> or circleci/<org-id>")
	_ = f.MarkHidden("org")

	f.StringSliceVar(&projectsAlias, "projects", nil,
		"Deprecated: use --project. Comma-separated project slugs to export")
	_ = f.MarkHidden("projects")

	return cmd
}
