## circleci-migrate terraform generate

Generate Terraform HCL and tfvars from an exported manifest.

### Synopsis

generate reads an exported manifest and writes a complete set of
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

to complete the items listed in GAPS.md without double-writing Terraform-managed resources.

```
circleci-migrate terraform generate [flags]
```

### Examples

```
  # Basic generation (no secrets in output); org type inferred from manifest
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
    --out ./terraform/
```

### Options

```
      --dest-org-id string             UUID of the destination CircleCI organization (required)
      --dest-org-type string           Destination org authentication type: "oauth" (GitHub OAuth, "gh/" slug) or
                                       "standalone" (GitHub App / GitLab / circleci-type, "circleci/" slug).
                                       Aliases: oauth|gh|github → oauth; standalone|app|github_app → standalone.
                                       When omitted, the type is inferred from the source org slug in the manifest
                                       and a note is printed so you know which type was assumed.
      --dest-runner-namespace string   Destination runner namespace for self-hosted runner resource classes (e.g. 'acme-new'). When omitted, the source namespace from the manifest is used.
      --existing sync --json           Path to a sync --json output file containing resource_ids of already-existing destination resources. Required with --import-existing.
  -h, --help                           help for generate
      --import-existing sync           Emit Terraform 1.5+ import {} blocks for resources that already exist in the destination. Destination IDs are read from --existing <sync-json>. Use this for the adoption path when resources were previously created by sync.
      --manifest string                Path to the exported manifest.json (default "manifest.json")
      --mapping string                 Path to a mapping.json for org slug / project ID remapping
      --out string                     Output directory for generated Terraform files (default "./terraform")
      --placeholders                   Emit secrets.auto.tfvars.json with REPLACE_ME placeholders and a fill-in workbook
      --secrets string                 Path to a secrets bundle.json — writes plaintext values to secrets.auto.tfvars.json
```

### Options inherited from parent commands

```
      --debug                 Enable debug logging
      --dest-token string     API token for the destination org (env: CIRCLECI_DEST_TOKEN)
      --host string           CircleCI host URL (env: CIRCLECI_CLI_HOST, CIRCLECI_HOST, or CIRCLE_URL) (default "https://circleci.com")
      --source-token string   API token for the source org (env: CIRCLECI_SOURCE_TOKEN)
      --token string          Personal API token — fallback for both orgs (env: CIRCLECI_CLI_TOKEN or CIRCLE_TOKEN)
```

### SEE ALSO

* [circleci-migrate terraform](circleci-migrate_terraform.md)	 - Generate Terraform configurations from an exported manifest.

