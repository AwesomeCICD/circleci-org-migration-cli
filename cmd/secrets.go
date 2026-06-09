package cmd

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/CircleCI-Public/circleci-org-migration-cli/internal/manifest"
	"github.com/CircleCI-Public/circleci-org-migration-cli/internal/secrets"
	"github.com/CircleCI-Public/circleci-org-migration-cli/version"
	"github.com/spf13/cobra"
)

func newSecretsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "secrets",
		Short: "Capture secret values from inside a CircleCI job.",
		Long: `secrets handles the one thing the API cannot: secret VALUES.

CircleCI masks environment-variable values everywhere in its API, so export
captures only their names. Inside a running job, however, a context's (or
project's) variables are injected as ordinary environment variables. The
'extract' subcommand reads those values from the job environment — using the
names recorded in the export manifest — and writes them to a secret bundle.

This command is meant to run INSIDE a CircleCI job (see the generated workflow
and the orb). The resulting bundle contains plaintext secrets: protect it,
keep it out of version control, and delete it once the sync is complete.`,
	}
	cmd.AddCommand(newSecretsExtractCommand())
	return cmd
}

func newSecretsExtractCommand() *cobra.Command {
	var (
		manifestPath string
		output       string
		contextName  string
		projectSlug  string
		strict       bool
	)

	cmd := &cobra.Command{
		Use:   "extract --manifest <file> (--context <name> | --project <slug>)",
		Short: "Capture a context's or project's secret values from the job environment.",
		Long: `extract reads the variable names for one context or project from the
manifest, looks each value up in the current environment, and records the
found values in a secret bundle (merging into an existing bundle if present).

Run this inside a CircleCI job that injects the target's variables:
  - For a context, the job must reference exactly that context.
  - For project variables, the job must run within that project.

Examples:
  circleci-migrate secrets extract --manifest manifest.json --context deploy-prod
  circleci-migrate secrets extract --manifest manifest.json --project gh/acme/web -o secrets.json`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if manifestPath == "" {
				return errors.New("--manifest is required")
			}
			if (contextName == "") == (projectSlug == "") {
				return errors.New("exactly one of --context or --project is required")
			}

			m, err := manifest.Load(manifestPath)
			if err != nil {
				return err
			}
			bundle, err := loadOrNewBundle(output)
			if err != nil {
				return err
			}

			var (
				res    *secrets.Result
				target string
			)
			if contextName != "" {
				target = "context " + contextName
				res, err = secrets.ExtractContext(m, bundle, contextName, os.LookupEnv)
			} else {
				target = "project " + projectSlug
				res, err = secrets.ExtractProject(m, bundle, projectSlug, os.LookupEnv)
			}
			if err != nil {
				return err
			}

			bundle.GeneratedAt = time.Now().UTC().Format(time.RFC3339)
			bundle.ToolVersion = version.UserAgent()
			if err := bundle.Save(output); err != nil {
				return fmt.Errorf("writing secret bundle: %w", err)
			}

			out := cmd.OutOrStdout()
			total := len(res.Found) + len(res.Missing)
			fmt.Fprintf(out, "Captured %d/%d variable(s) for %s into %s\n", len(res.Found), total, target, output)
			if len(res.Missing) > 0 {
				fmt.Fprintf(out, "Not found in this job's environment: %v\n", res.Missing)
			}
			fmt.Fprintln(cmd.ErrOrStderr(), "WARNING: "+output+" contains plaintext secrets — protect it and do not commit it.")

			if strict && len(res.Missing) > 0 {
				return fmt.Errorf("%d variable(s) were missing from the environment", len(res.Missing))
			}
			return nil
		},
	}

	f := cmd.Flags()
	f.StringVar(&manifestPath, "manifest", "", "Path to the export manifest (required)")
	f.StringVarP(&output, "output", "o", "secrets.json", "Path to the secret bundle to write/append")
	f.StringVar(&contextName, "context", "", "Context name to capture (mutually exclusive with --project)")
	f.StringVar(&projectSlug, "project", "", "Project slug to capture (mutually exclusive with --context)")
	f.BoolVar(&strict, "strict", false, "Fail if any expected variable is missing from the environment")

	return cmd
}

// loadOrNewBundle returns the existing bundle at path, or a fresh one if the
// file does not exist, so repeated extract calls accumulate into one bundle.
func loadOrNewBundle(path string) (*manifest.SecretBundle, error) {
	if _, err := os.Stat(path); err == nil {
		return manifest.LoadSecretBundle(path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return manifest.NewSecretBundle(), nil
}
