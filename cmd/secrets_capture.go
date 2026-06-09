package cmd

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/CircleCI-Public/circleci-org-migration-cli/api/project"
	"github.com/CircleCI-Public/circleci-org-migration-cli/internal/extract"
	"github.com/CircleCI-Public/circleci-org-migration-cli/internal/manifest"
	"github.com/CircleCI-Public/circleci-org-migration-cli/version"
	"github.com/spf13/cobra"
)

// flagReaderWriter is the minimal interface the capture command needs to read
// and restore the api-trigger-with-config project feature flag. Injected by
// tests; production uses a real *project.Client.
type flagReaderWriter interface {
	GetV11ProjectFeatureFlags(slug string) (map[string]bool, error)
	SetV11ProjectFeatureFlags(slug string, flags map[string]bool) error
}

// pipelineDefLister lists pipeline definitions for a project so the command
// can resolve the first definition's UUID automatically.
type pipelineDefLister interface {
	ListPipelineDefinitions(projectID string) ([]project.PipelineDefinition, error)
}

// projectGetter retrieves project metadata (used to get the project UUID).
type projectGetter interface {
	GetProject(slug string) (*project.Project, error)
}

// captureClient combines all interfaces the capture command exercises.
type captureClient interface {
	flagReaderWriter
	pipelineDefLister
	projectGetter
	extract.Deps
}

const apiTriggerKey = "api-trigger-with-config"

// newSecretsCaptureCommand builds the "secrets capture" subcommand.
func newSecretsCaptureCommand() *cobra.Command {
	var (
		manifestPath       string
		output             string
		projectSlugs       []string
		contextNames       []string
		branch             string
		enableTrigger      bool
		skipRestrictedCtxs bool
		pollTimeout        time.Duration
	)

	cmd := &cobra.Command{
		Use:   "capture --manifest <file>",
		Short: "Capture secret values by running an unversioned pipeline inside CircleCI.",
		Long: `capture extracts plaintext environment-variable values WITHOUT committing
any config to the target project. It:

  1. Reads variable names from the manifest for the selected project(s) and
     context(s).
  2. Ensures api-trigger-with-config is enabled for each project (either it
     must already be on, or --enable-trigger must be set).
  3. Triggers an unversioned pipeline run with an inline config that dumps the
     variable values to a build artifact.
  4. Polls until the pipeline completes, then downloads and parses the artifact.
  5. Writes the captured values into the secret bundle (--output).
  6. Restores the api-trigger-with-config flag to its original value (even on
     failure).

SECURITY NOTES:
  - The secret bundle contains plaintext secrets. Protect it, do not commit it.
  - Build artifacts are retained for at least 1 day with no delete-artifact API.
    Rotate any captured secrets and treat the artifact as sensitive.

Examples:
  circleci-migrate secrets capture --manifest manifest.json --source-token $TOKEN
  circleci-migrate secrets capture --manifest manifest.json --project gh/acme/web \
    --enable-trigger --branch main -o secrets.json`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if manifestPath == "" {
				return errors.New("--manifest is required")
			}

			token := rootOptions.SourceTokenOrDefault()
			if token == "" {
				return fmt.Errorf("no API token: set --source-token, --token, CIRCLECI_SOURCE_TOKEN, or CIRCLECI_CLI_TOKEN")
			}

			m, err := manifest.Load(manifestPath)
			if err != nil {
				return err
			}

			bundle, err := loadOrNewBundle(output)
			if err != nil {
				return err
			}

			projClient, err := project.NewClient(rootOptions, token)
			if err != nil {
				return fmt.Errorf("creating project client: %w", err)
			}

			// Resolve the set of projects to process.
			projects := selectProjects(m, projectSlugs)
			if len(projects) == 0 {
				return fmt.Errorf("no projects matched the given selectors (manifest has %d projects)", len(m.Projects))
			}

			// Pre-resolve the set of context names the caller wants to include
			// (empty slice means: include all contexts attached to each project).
			selectedCtxNames := make(map[string]bool, len(contextNames))
			for _, n := range contextNames {
				selectedCtxNames[n] = true
			}

			var captureErr error
			for i := range projects {
				p := &projects[i]
				if err := captureProject(
					cmd.Context(),
					cmd,
					projClient,
					m, bundle,
					p,
					selectedCtxNames,
					branch, output,
					enableTrigger, skipRestrictedCtxs,
					pollTimeout,
				); err != nil {
					// Continue processing other projects; record the first error.
					fmt.Fprintf(cmd.ErrOrStderr(), "ERROR capturing project %s: %v\n", p.Slug, err)
					if captureErr == nil {
						captureErr = err
					}
				}
			}

			bundle.GeneratedAt = time.Now().UTC().Format(time.RFC3339)
			bundle.ToolVersion = version.UserAgent()
			if err := bundle.Save(output); err != nil {
				return fmt.Errorf("writing secret bundle: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Secret bundle written to %s\n", output)
			fmt.Fprintln(cmd.ErrOrStderr(), `
WARNING: The secret bundle contains PLAINTEXT secrets.
  - Protect the file; do not commit it to version control.
  - Build artifacts from the extraction run are retained for at least 1 day.
    There is NO delete-artifact API. Rotate captured secrets and treat the
    artifact as sensitive until it expires.`)

			return captureErr
		},
	}

	f := cmd.Flags()
	f.StringVar(&manifestPath, "manifest", "", "Path to the export manifest (required)")
	f.StringVarP(&output, "output", "o", "secrets.json", "Path to the secret bundle to write/append")
	f.StringArrayVar(&projectSlugs, "project", nil, "Project slug(s) to capture (default: all in manifest)")
	f.StringArrayVar(&contextNames, "context", nil, "Context name(s) to capture for each project (default: all attached)")
	f.StringVar(&branch, "branch", "main", "Branch to check out for the extraction run")
	f.BoolVar(&enableTrigger, "enable-trigger", false,
		"Enable api-trigger-with-config if not already on, and restore after capture")
	f.BoolVar(&skipRestrictedCtxs, "skip-restricted-contexts", true,
		"Skip contexts that have project/expression/group restrictions (attach warning instead of attempting)")
	f.DurationVar(&pollTimeout, "poll-timeout", 10*time.Minute,
		"Maximum time to wait for each pipeline to complete (0 = no timeout)")

	return cmd
}

// selectProjects returns the manifest projects matching slugs.  If slugs is
// empty all manifest projects are returned.
func selectProjects(m *manifest.Manifest, slugs []string) []manifest.Project {
	if len(slugs) == 0 {
		return m.Projects
	}
	want := make(map[string]bool, len(slugs))
	for _, s := range slugs {
		want[s] = true
	}
	var out []manifest.Project
	for _, p := range m.Projects {
		if want[p.Slug] {
			out = append(out, p)
		}
	}
	return out
}

// captureProject handles the full capture flow for a single manifest project.
// It restores the api-trigger-with-config flag even if capture fails.
func captureProject(
	ctx context.Context,
	cmd *cobra.Command,
	client captureClient,
	m *manifest.Manifest,
	bundle *manifest.SecretBundle,
	p *manifest.Project,
	selectedCtxNames map[string]bool,
	branch, output string,
	enableTrigger, skipRestricted bool,
	pollTimeout time.Duration,
) error {
	stderr := cmd.ErrOrStderr()
	stdout := cmd.OutOrStdout()

	// ── 1. Ensure api-trigger-with-config ────────────────────────────────────
	flags, err := client.GetV11ProjectFeatureFlags(p.Slug)
	if err != nil {
		return fmt.Errorf("read feature flags for %s: %w", p.Slug, err)
	}

	wasEnabled := flags[apiTriggerKey]

	if !wasEnabled {
		if !enableTrigger {
			return fmt.Errorf(
				"project %s has api-trigger-with-config disabled; "+
					"set --enable-trigger to enable it automatically",
				p.Slug,
			)
		}
		fmt.Fprintf(stderr, "Enabling api-trigger-with-config for %s…\n", p.Slug)
		if err := client.SetV11ProjectFeatureFlags(p.Slug, map[string]bool{"api-trigger-with-config": true}); err != nil {
			return fmt.Errorf("enable api-trigger-with-config for %s: %w", p.Slug, err)
		}
	}

	// Defer restoration so it runs even on error.
	defer func() {
		if !wasEnabled && enableTrigger {
			fmt.Fprintf(stderr, "Restoring api-trigger-with-config=false for %s…\n", p.Slug)
			if restoreErr := client.SetV11ProjectFeatureFlags(p.Slug, map[string]bool{"api-trigger-with-config": false}); restoreErr != nil {
				fmt.Fprintf(stderr, "WARNING: failed to restore api-trigger-with-config for %s: %v\n", p.Slug, restoreErr)
			}
		}
	}()

	// ── 2. Resolve pipeline definition ID ────────────────────────────────────
	proj, err := client.GetProject(p.Slug)
	if err != nil {
		return fmt.Errorf("get project %s: %w", p.Slug, err)
	}

	defs, err := client.ListPipelineDefinitions(proj.ID)
	if err != nil {
		return fmt.Errorf("list pipeline definitions for %s: %w", p.Slug, err)
	}
	if len(defs) == 0 {
		return fmt.Errorf("project %s has no pipeline definitions — is it a GitHub App project?", p.Slug)
	}
	defID := defs[0].ID

	// ── 3. Build var name list and context list ───────────────────────────────
	// Project env var names.
	var allVarNames []string
	for _, ev := range p.EnvVars {
		allVarNames = append(allVarNames, ev.Name)
	}

	// Contexts attached to this project (inferred by matching the manifest
	// contexts against the project — the manifest doesn't record explicit
	// project↔context links so we use the selectedCtxNames filter or all).
	var ctxNamesForRun []string

	for i := range m.Contexts {
		mc := &m.Contexts[i]

		// Apply context filter if the caller passed --context flags.
		if len(selectedCtxNames) > 0 && !selectedCtxNames[mc.Name] {
			continue
		}

		// Warn about and optionally skip restricted contexts.
		if len(mc.Restrictions) > 0 {
			fmt.Fprintf(stderr,
				"WARNING: context %q has restrictions (%d). The extraction job may not "+
					"have access to it. Auto-toggling restrictions is not supported; handle "+
					"manually if needed.\n",
				mc.Name, len(mc.Restrictions),
			)
			if skipRestricted {
				fmt.Fprintf(stderr, "Skipping restricted context %q (--skip-restricted-contexts=true).\n", mc.Name)
				continue
			}
		}

		ctxNamesForRun = append(ctxNamesForRun, mc.Name)
		for _, ev := range mc.EnvVars {
			allVarNames = append(allVarNames, ev.Name)
		}
	}

	// De-duplicate var names (a context var may shadow a project var with the
	// same name).
	allVarNames = dedupe(allVarNames)

	fmt.Fprintf(stdout, "Capturing %d variable(s) for project %s (contexts: %v)…\n",
		len(allVarNames), p.Slug, ctxNamesForRun)

	// ── 4. Run Capture ────────────────────────────────────────────────────────
	opts := extract.Options{
		DefinitionID: defID,
		Branch:       branch,
		PollTimeout:  pollTimeout,
	}

	values, err := extract.Capture(ctx, client, p.Slug, allVarNames, ctxNamesForRun, opts)
	if err != nil {
		return fmt.Errorf("capture for %s: %w", p.Slug, err)
	}

	// ── 5. Store in bundle ────────────────────────────────────────────────────
	// Project vars.
	for _, ev := range p.EnvVars {
		if v, ok := values[ev.Name]; ok {
			bundle.SetProjectSecret(p.Slug, ev.Name, v)
		}
	}
	// Context vars.
	for i := range m.Contexts {
		mc := &m.Contexts[i]
		// Only store contexts we actually attached.
		included := false
		for _, n := range ctxNamesForRun {
			if n == mc.Name {
				included = true
				break
			}
		}
		if !included {
			continue
		}
		for _, ev := range mc.EnvVars {
			if v, ok := values[ev.Name]; ok {
				bundle.SetContextSecret(mc.Name, ev.Name, v)
			}
		}
	}

	// Write incrementally so a mid-loop failure still saves what was captured.
	bundle.GeneratedAt = time.Now().UTC().Format(time.RFC3339)
	bundle.ToolVersion = version.UserAgent()
	if err := bundle.Save(output); err != nil {
		return fmt.Errorf("saving bundle after project %s: %w", p.Slug, err)
	}

	fmt.Fprintf(stdout, "Captured %d variable(s) for %s\n", len(values), p.Slug)
	return nil
}

// dedupe returns a copy of s with duplicate entries removed (first occurrence
// wins), preserving order.
func dedupe(s []string) []string {
	seen := make(map[string]bool, len(s))
	out := make([]string, 0, len(s))
	for _, v := range s {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}
