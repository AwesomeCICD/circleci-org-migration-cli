package cmd

import (
	"fmt"
	"time"

	cctx "github.com/AwesomeCICD/circleci-org-migration-cli/api/context"
	"github.com/AwesomeCICD/circleci-org-migration-cli/api/org"
	"github.com/AwesomeCICD/circleci-org-migration-cli/api/project"
	"github.com/AwesomeCICD/circleci-org-migration-cli/api/runner"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/exporter"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/report"
	"github.com/spf13/cobra"
)

// ExportJSONSummary is the machine-readable result of an export command when
// --json is set. Only counts and paths are included — no secret values.
type ExportJSONSummary struct {
	// SourceOrgSlug is the slug of the source organization (e.g. "gh/acme").
	SourceOrgSlug string `json:"source_org_slug"`
	// SourceOrgID is the UUID of the source organization, when available.
	SourceOrgID string `json:"source_org_id,omitempty"`
	// Host is the CircleCI host that was queried.
	Host string `json:"host"`
	// GeneratedAt is the RFC 3339 timestamp of the export.
	GeneratedAt string `json:"generated_at"`
	// ContextCount is the number of contexts exported.
	ContextCount int `json:"context_count"`
	// ContextVarCount is the total number of context variable names exported
	// (values are never included).
	ContextVarCount int `json:"context_var_count"`
	// ProjectCount is the number of projects exported.
	ProjectCount int `json:"project_count"`
	// ProjectVarCount is the total number of project variable names exported
	// (values are never included).
	ProjectVarCount int `json:"project_var_count"`
	// WarningCount is the number of warnings recorded during export.
	WarningCount int `json:"warning_count"`
	// Warnings lists the warning codes and scopes (no secrets).
	Warnings []exportWarning `json:"warnings,omitempty"`
	// ManifestPath is the path the manifest was written to.
	ManifestPath string `json:"manifest_path"`
	// ReportPath is the path the audit report was written to.
	ReportPath string `json:"report_path"`
}

// exportWarning is a safe, secret-free representation of a manifest warning for
// JSON output.
type exportWarning struct {
	Scope   string `json:"scope"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// buildExportSummary constructs an ExportJSONSummary from a manifest and paths.
// It never includes secret values — only names and counts.
func buildExportSummary(m *manifest.Manifest, manifestPath, reportPath string) ExportJSONSummary {
	ctxVars := 0
	for _, c := range m.Contexts {
		ctxVars += len(c.EnvVars)
	}
	projVars := 0
	for _, p := range m.Projects {
		projVars += len(p.EnvVars)
	}
	warnings := make([]exportWarning, 0, len(m.Warnings))
	for _, w := range m.Warnings {
		warnings = append(warnings, exportWarning{Scope: w.Scope, Code: w.Code, Message: w.Message})
	}
	return ExportJSONSummary{
		SourceOrgSlug:   m.Source.Org.Slug,
		SourceOrgID:     m.Source.Org.ID,
		Host:            m.Source.Host,
		GeneratedAt:     m.GeneratedAt,
		ContextCount:    len(m.Contexts),
		ContextVarCount: ctxVars,
		ProjectCount:    len(m.Projects),
		ProjectVarCount: projVars,
		WarningCount:    len(m.Warnings),
		Warnings:        warnings,
		ManifestPath:    manifestPath,
		ReportPath:      reportPath,
	}
}

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
		jsonOutput      bool
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
			if jsonOutput {
				summary := buildExportSummary(m, output, reportPath)
				return marshalJSON(out, summary)
			}
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
	f.BoolVar(&jsonOutput, "json", false,
		"Print a machine-readable JSON summary to stdout instead of the human-readable summary (manifest and report files are still written)")
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
