package cmd

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
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

// usagePollInterval is the time between status polls for the async usage export
// job.  It is a package-level variable so tests can override it.
var usagePollInterval = 10 * time.Second

// downloadUsageFile fetches a single pre-signed URL and saves it to outDir,
// using the last path segment of the URL (before the query string) as the
// filename.  Returns the written path on success.
func downloadUsageFile(ctx context.Context, rawURL, outDir string) (string, error) {
	// Derive the local filename from the URL path component.  Pre-signed S3 URLs
	// look like "…/usage-123.csv.gz?X-Amz-…".  We parse the URL properly to
	// extract just the path, then take its base (last segment).
	base := usageFileBase(rawURL)
	dest := filepath.Join(outDir, base)

	// #nosec G107 -- URL comes from the CircleCI API; not user-controlled
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", fmt.Errorf("download %s: build request: %w", rawURL, err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("download %s: %w", rawURL, err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("download %s: HTTP %d", rawURL, resp.StatusCode)
	}

	f, err := os.Create(dest) // #nosec G304 -- dest is constructed from outDir + API filename
	if err != nil {
		return "", fmt.Errorf("create %s: %w", dest, err)
	}
	defer f.Close() //nolint:errcheck

	if _, err := io.Copy(f, resp.Body); err != nil {
		return "", fmt.Errorf("write %s: %w", dest, err)
	}
	return dest, nil
}

// usageFileBase returns a safe local filename for a pre-signed download URL.
// It parses the URL to extract the last path segment.  If no usable segment can
// be found (root path, unparseable URL, etc.) it falls back to "usage.csv.gz".
func usageFileBase(rawURL string) string {
	// Prefer proper URL parsing so we correctly handle any scheme/host/path.
	if parsed, err := url.Parse(rawURL); err == nil {
		if seg := filepath.Base(parsed.Path); seg != "" && seg != "." && seg != "/" {
			return seg
		}
	}
	return "usage.csv.gz"
}

// runUsageExport orchestrates the full async usage-export flow: submit the job,
// poll until completed (or timeout), then download all CSVs to outDir.
// Errors are non-fatal: they are written to errOut and the function returns.
func runUsageExport(ctx context.Context, orgClient *org.Client, orgID, start, end, outDir string, timeout time.Duration, errOut io.Writer) {
	fmt.Fprintf(errOut, "\nNote: usage data is a local baseline snapshot — it does NOT transfer to the destination org.\n")

	jobID, err := orgClient.CreateUsageExportJob(ctx, orgID, start, end)
	if err != nil {
		fmt.Fprintf(errOut, "Warning: usage export job creation failed: %v\n", err)
		return
	}
	fmt.Fprintf(errOut, "Usage export job created: %s (polling for completion...)\n", jobID)

	deadline := time.Now().Add(timeout)
	var downloadURLs []string
	for completed := false; !completed; {
		state, urls, pollErr := orgClient.GetUsageExportJob(ctx, orgID, jobID)
		if pollErr != nil {
			fmt.Fprintf(errOut, "Warning: usage export poll failed: %v\n", pollErr)
			return
		}
		switch state {
		case "completed":
			downloadURLs = urls
			completed = true
		case "failed", "error":
			fmt.Fprintf(errOut, "Warning: usage export job %s ended with state %q — skipping download.\n", jobID, state)
			return
		default:
			if time.Now().After(deadline) {
				fmt.Fprintf(errOut, "Warning: usage export job %s did not complete within %s — skipping download.\n", jobID, timeout)
				return
			}
			time.Sleep(usagePollInterval)
		}
	}

	if len(downloadURLs) == 0 {
		fmt.Fprintf(errOut, "Warning: usage export job completed but returned no download URLs.\n")
		return
	}

	if mkErr := os.MkdirAll(outDir, 0o750); mkErr != nil {
		fmt.Fprintf(errOut, "Warning: could not create usage output dir %s: %v\n", outDir, mkErr)
		return
	}

	for _, u := range downloadURLs {
		path, dlErr := downloadUsageFile(ctx, u, outDir)
		if dlErr != nil {
			fmt.Fprintf(errOut, "Warning: usage download failed: %v\n", dlErr)
			continue
		}
		fmt.Fprintf(errOut, "Usage data saved: %s\n", path)
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
		// Usage export flags (opt-in; data stays local, never transferred to dest).
		includeUsage bool
		usageStart   string
		usageEnd     string
		usageTimeout time.Duration
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

USAGE DATA SNAPSHOT (opt-in):

Pass --include-usage to also request a historical usage report from the CircleCI
Usage API. The report is downloaded as gzip-compressed CSV files to a "usage/"
sub-directory next to the manifest. The window defaults to the last 30 days; use
--usage-start / --usage-end (RFC 3339) to override. The maximum window is 31 days
(enforced by the API).

IMPORTANT: usage data is a local baseline/record only. It does NOT transfer to
the destination organisation during sync or migrate. If the usage export fails or
times out, the main export succeeds and a warning is printed.

Examples:
  circleci-migrate export --source-org gh/acme --source-token $SRC_TOKEN
  circleci-migrate export --source-org gh/acme -o acme.json --report acme-audit.md
  circleci-migrate export --source-org gh/acme --project gh/acme/web --project gh/acme/api
  circleci-migrate export --source-org gh/acme --runner-namespace acme
  circleci-migrate export --source-org gh/acme --include-usage
  circleci-migrate export --source-org gh/acme --include-usage --usage-start 2026-01-01T00:00:00Z --usage-end 2026-01-31T23:59:59Z`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			cfg := configFromContext(ctx)
			// Merge values from hidden alias --projects (StringSlice, comma-or-repeat)
			// into the canonical --project list.
			projectSlugs = append(projectSlugs, projectsAlias...)

			if orgSlug == "" {
				return fmt.Errorf("--source-org is required (e.g. --source-org gh/acme)")
			}
			token := cfg.SourceTokenOrDefault()
			if token == "" {
				return noSourceTokenError()
			}

			orgClient, err := org.NewClient(cfg, token)
			if err != nil {
				return fmt.Errorf("creating org client: %w", err)
			}
			ctxClient, err := cctx.NewClient(cfg, token)
			if err != nil {
				return fmt.Errorf("creating context client: %w", err)
			}
			projClient, err := project.NewClient(cfg, token)
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
				runnerClient, rerr := runner.NewClient(cfg, token)
				if rerr != nil {
					return fmt.Errorf("creating runner client: %w", rerr)
				}
				ex.Runner = runnerClient
			}

			m, err := ex.Export(ctx, exporter.Options{
				Host:            cfg.Host,
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

			// ── Optional usage export (opt-in; does NOT transfer to dest) ────────
			if includeUsage {
				// Resolve the org ID needed by the usage export API.
				orgID, idErr := orgClient.ResolveOrgID(ctx, orgSlug)
				if idErr != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "Warning: could not resolve org ID for usage export: %v\n", idErr)
				} else {
					// Default window: last 30 days.
					end := time.Now().UTC()
					start := end.Add(-30 * 24 * time.Hour)

					if usageStart != "" {
						if t, parseErr := time.Parse(time.RFC3339, usageStart); parseErr == nil {
							start = t
						} else {
							fmt.Fprintf(cmd.ErrOrStderr(), "Warning: --usage-start %q is not RFC3339; using default.\n", usageStart)
						}
					}
					if usageEnd != "" {
						if t, parseErr := time.Parse(time.RFC3339, usageEnd); parseErr == nil {
							end = t
						} else {
							fmt.Fprintf(cmd.ErrOrStderr(), "Warning: --usage-end %q is not RFC3339; using default.\n", usageEnd)
						}
					}

					usageDir := filepath.Join(filepath.Dir(output), "usage")
					runUsageExport(
						ctx,
						orgClient,
						orgID,
						start.Format(time.RFC3339),
						end.Format(time.RFC3339),
						usageDir,
						usageTimeout,
						cmd.ErrOrStderr(),
					)
				}
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

	// Usage export flags (opt-in; data is local-only, never transferred to dest).
	f.BoolVar(&includeUsage, "include-usage", false,
		"(Opt-in) Request a historical usage report from the CircleCI Usage API and download the CSV files "+
			"to a 'usage/' sub-directory next to the manifest. "+
			"This data is a local baseline/record only — it does NOT transfer to the destination org.")
	f.StringVar(&usageStart, "usage-start", "",
		"Start of the usage report window in RFC 3339 format (default: 30 days ago). "+
			"The window may not exceed 31 days. Only used when --include-usage is set.")
	f.StringVar(&usageEnd, "usage-end", "",
		"End of the usage report window in RFC 3339 format (default: now). "+
			"Only used when --include-usage is set.")
	f.DurationVar(&usageTimeout, "usage-timeout", 10*time.Minute,
		"Maximum time to wait for the usage export job to complete before giving up. "+
			"Only used when --include-usage is set.")

	// Hidden back-compat aliases — old invocations must still work.
	f.StringVar(&orgSlug, "org", "",
		"Deprecated: use --source-org. Source organization slug: gh/<org> or circleci/<org-id>")
	_ = f.MarkHidden("org")

	f.StringSliceVar(&projectsAlias, "projects", nil,
		"Deprecated: use --project. Comma-separated project slugs to export")
	_ = f.MarkHidden("projects")

	return cmd
}
