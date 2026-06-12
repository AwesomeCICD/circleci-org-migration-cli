package cmd

import (
	"fmt"
	"os"

	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/terraform"
	"github.com/spf13/cobra"
)

// destOrgTypeHelp is a shared description of the --dest-org-type flag.
const destOrgTypeHelp = `Destination org authentication type: "oauth" (GitHub OAuth, "gh/" slug) or
"standalone" (GitHub App / GitLab / circleci-type, "circleci/" slug).
Aliases: oauth|gh|github → oauth; standalone|app|github_app → standalone.
When omitted, the type is inferred from the source org slug in the manifest
and a note is printed so you know which type was assumed.`

// newTerraformCommand returns the `terraform` command group.
func newTerraformCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "terraform",
		Short: "Generate Terraform configurations from an exported manifest.",
		Long: `terraform generates Terraform HCL and tfvars files from a circleci-migrate
manifest, targeting the official CircleCI Terraform provider
(CircleCI-Public/circleci, v0.3.x).

Terraform manages the declarative resources (contexts, context env-vars,
projects, project env-vars); the CLI remains the orchestrator for everything
Terraform cannot do: secrets capture, CIAM, org settings, schedules, checkout
keys, and SSH keys. The generated GAPS.md lists every remaining step with the
exact circleci-migrate command to complete it.

  circleci-migrate terraform generate \
    --manifest manifest.json \
    [--secrets bundle.json | --placeholders] \
    [--mapping mapping.json] \
    --dest-org-id <uuid> \
    [--dest-org-type oauth|standalone] \
    --out ./terraform/`,
	}

	cmd.AddCommand(newTerraformGenerateCommand())
	return cmd
}

// newTerraformGenerateCommand returns the `terraform generate` subcommand.
func newTerraformGenerateCommand() *cobra.Command {
	var (
		manifestPath        string
		secretsPath         string
		placeholders        bool
		mappingPath         string
		destOrgID           string
		outDir              string
		destOrgTypeRaw      string
		destRunnerNamespace string
		existingPath        string
		importExisting      bool
	)

	cmd := &cobra.Command{
		Use:   "generate",
		Short: "Generate Terraform HCL and tfvars from an exported manifest.",
		Long: `generate reads an exported manifest and writes a complete set of
Terraform files into --out:

  versions.tf            — provider version constraint (~> 0.3)
  providers.tf           — provider block with host + org from flags
  contexts.tf            — circleci_context + circleci_context_environment_variable resources
  restrictions.tf        — circleci_context_restriction resources (project+expression both orgs; group OAuth-only)
  projects.tf            — circleci_project + circleci_project_environment_variable resources
  webhooks.tf            — circleci_webhook resources (both org types)
  runners.tf             — circleci_runner_resource_class + circleci_runner_token resources
  pipelines.tf           — circleci_pipeline + circleci_trigger resources (standalone ONLY)
  migration.auto.tfvars.json — non-secret values (context/project/webhook/pipeline/runner settings)
  secrets.auto.tfvars.json   — env-var values (only with --secrets or --placeholders)
  imports.tf             — Terraform 1.5+ import blocks (only with --import-existing)
  GAPS.md                — checklist of what Terraform does not manage + CLI commands to fill gaps

The generated HCL uses for_each over variables so that regenerating after a new
export changes only the tfvars, not the modules.

Apply the generated configuration:

  cd ./terraform/
  terraform init
  terraform plan
  terraform apply

Then run:

  circleci-migrate sync --manifest manifest.json --dest-token $CIRCLECI_DEST_TOKEN \
    --apply --skip-terraform-managed

to complete the items listed in GAPS.md without double-writing Terraform-managed resources.`,
		Example: `  # Basic generation (no secrets in output); org type inferred from manifest
  circleci-migrate terraform generate \
    --manifest manifest.json \
    --dest-org-id bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb \
    --out ./terraform/

  # Explicit OAuth destination (omits advanced project settings)
  circleci-migrate terraform generate \
    --manifest manifest.json \
    --dest-org-id bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb \
    --dest-org-type oauth \
    --out ./terraform/

  # Explicit standalone destination (includes advanced project settings)
  circleci-migrate terraform generate \
    --manifest manifest.json \
    --dest-org-id bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb \
    --dest-org-type standalone \
    --out ./terraform/

  # With secret values from a captured bundle
  circleci-migrate terraform generate \
    --manifest manifest.json \
    --secrets bundle.json \
    --dest-org-id bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb \
    --out ./terraform/

  # With placeholder values and a workbook to fill in
  circleci-migrate terraform generate \
    --manifest manifest.json \
    --placeholders \
    --dest-org-id bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb \
    --out ./terraform/

  # With org slug remapping
  circleci-migrate terraform generate \
    --manifest manifest.json \
    --mapping mapping.json \
    --dest-org-id bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb \
    --out ./terraform/

  # Adopt already-existing resources (--import-existing)
  circleci-migrate terraform generate \
    --manifest manifest.json \
    --dest-org-id bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb \
    --import-existing --existing sync-result.json \
    --out ./terraform/

  # Custom runner namespace mapping
  circleci-migrate terraform generate \
    --manifest manifest.json \
    --dest-org-id bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb \
    --dest-runner-namespace acme-new \
    --out ./terraform/`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := configFromContext(cmd.Context())

			// Validate mutual exclusivity.
			if secretsPath != "" && placeholders {
				return fmt.Errorf("--secrets and --placeholders are mutually exclusive; use one or the other")
			}

			// Parse optional --dest-org-type.
			destOrgType := terraform.OrgTypeUnknown
			if destOrgTypeRaw != "" {
				var err error
				destOrgType, err = terraform.ParseOrgType(destOrgTypeRaw)
				if err != nil {
					return err
				}
			}

			// Load manifest.
			m, err := manifest.Load(manifestPath)
			if err != nil {
				return fmt.Errorf("loading manifest: %w", err)
			}

			// Load optional mapping.
			var mp *manifest.Mapping
			if mappingPath != "" {
				mp, err = manifest.LoadMapping(mappingPath)
				if err != nil {
					return fmt.Errorf("loading mapping: %w", err)
				}
			}

			// Load optional secrets bundle.
			var bundle *manifest.SecretBundle
			if secretsPath != "" {
				bundle, err = manifest.LoadSecretBundle(secretsPath)
				if err != nil {
					return fmt.Errorf("loading secrets bundle: %w", err)
				}
			}

			// Print plaintext warning before writing secrets.
			if bundle != nil {
				fmt.Fprintln(cmd.ErrOrStderr(), terraform.PlaintextWarning)
			}

			// Load optional existing IDs for --import-existing.
			var existingIDs *terraform.ExistingIDs
			if importExisting && existingPath != "" {
				var loadErr error
				existingIDs, loadErr = terraform.LoadExistingIDs(existingPath)
				if loadErr != nil {
					return loadErr
				}
			}

			opts := terraform.Options{
				DestOrgID:           destOrgID,
				Host:                cfg.Host,
				Mapping:             mp,
				SecretsBundle:       bundle,
				Placeholders:        placeholders,
				OutDir:              outDir,
				DestOrgType:         destOrgType,
				DestRunnerNamespace: destRunnerNamespace,
				ExistingIDs:         existingIDs,
				ImportExisting:      importExisting,
			}

			if err := terraform.Generate(m, opts); err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Terraform files written to %s\n", outDir)
			fmt.Fprintln(cmd.OutOrStdout())
			fmt.Fprintln(cmd.OutOrStdout(), "Next steps:")
			fmt.Fprintf(cmd.OutOrStdout(), "  1. cd %s && terraform init && terraform plan\n", outDir)
			fmt.Fprintln(cmd.OutOrStdout(), "  2. Review the plan, then: terraform apply")
			fmt.Fprintln(cmd.OutOrStdout(), "  3. Review GAPS.md for resources Terraform does not manage.")
			fmt.Fprintln(cmd.OutOrStdout(), "     Run the listed circleci-migrate sync commands to complete the migration.")
			if bundle != nil {
				fmt.Fprintln(cmd.OutOrStdout())
				fmt.Fprintln(cmd.ErrOrStderr(), terraform.PlaintextWarning)
			}

			// If GAPS.md was generated, print a summary to stderr.
			gapsPath := outDir + "/GAPS.md"
			if _, err := os.Stat(gapsPath); err == nil {
				fmt.Fprintf(cmd.OutOrStdout(), "\nSee %s for the complete list of gaps.\n", gapsPath)
			}

			return nil
		},
	}

	f := cmd.Flags()
	f.StringVar(&manifestPath, "manifest", "manifest.json",
		"Path to the exported manifest.json")
	f.StringVar(&secretsPath, "secrets", "",
		"Path to a secrets bundle.json — writes plaintext values to secrets.auto.tfvars.json")
	f.BoolVar(&placeholders, "placeholders", false,
		"Emit secrets.auto.tfvars.json with REPLACE_ME placeholders and a fill-in workbook")
	f.StringVar(&mappingPath, "mapping", "",
		"Path to a mapping.json for org slug / project ID remapping")
	f.StringVar(&destOrgID, "dest-org-id", "",
		"UUID of the destination CircleCI organization (required)")
	f.StringVar(&outDir, "out", "./terraform",
		"Output directory for generated Terraform files")
	f.StringVar(&destOrgTypeRaw, "dest-org-type", "",
		destOrgTypeHelp)
	f.StringVar(&destRunnerNamespace, "dest-runner-namespace", "",
		"Destination runner namespace for self-hosted runner resource classes "+
			"(e.g. 'acme-new'). When omitted, the source namespace from the manifest is used.")
	f.BoolVar(&importExisting, "import-existing", false,
		"Emit Terraform 1.5+ import {} blocks for resources that already exist in the "+
			"destination. Destination IDs are read from --existing <sync-json>. "+
			"Use this for the adoption path when resources were previously created by `sync`.")
	f.StringVar(&existingPath, "existing", "",
		"Path to a `sync --json` output file containing resource_ids of already-existing "+
			"destination resources. Required with --import-existing.")

	_ = cmd.MarkFlagRequired("dest-org-id")

	return cmd
}
