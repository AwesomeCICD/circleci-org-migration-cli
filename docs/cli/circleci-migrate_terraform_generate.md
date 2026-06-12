## circleci-migrate terraform generate

Generate Terraform HCL and tfvars from an exported manifest.

### Synopsis

generate reads an exported manifest and writes a complete set of
Terraform files into --out:

  versions.tf            — provider version constraint (~> 0.3)
  providers.tf           — provider block with host + org from flags
  contexts.tf            — circleci_context + circleci_context_environment_variable resources
  projects.tf            — circleci_project + circleci_project_environment_variable resources
  migration.auto.tfvars.json — non-secret values (context/project names, advanced settings)
  secrets.auto.tfvars.json   — env-var values (only with --secrets or --placeholders)
  GAPS.md                — checklist of what Terraform does not manage + CLI commands to fill gaps

The generated HCL uses for_each over variables so that regenerating after a new
export changes only the tfvars, not the modules.

Apply the generated configuration:

  cd ./terraform/
  terraform init
  terraform plan
  terraform apply

Then run:

  circleci-migrate sync --manifest manifest.json --dest-token $CIRCLECI_DEST_TOKEN --apply

to complete the items listed in GAPS.md.

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
```

### Options

```
      --dest-org-id string     UUID of the destination CircleCI organization (required)
      --dest-org-type string   Destination org authentication type: "oauth" (GitHub OAuth, "gh/" slug) or
                               "standalone" (GitHub App / GitLab / circleci-type, "circleci/" slug).
                               Aliases: oauth|gh|github → oauth; standalone|app|github_app → standalone.
                               When omitted, the type is inferred from the source org slug in the manifest
                               and a note is printed so you know which type was assumed.
  -h, --help                   help for generate
      --manifest string        Path to the exported manifest.json (default "manifest.json")
      --mapping string         Path to a mapping.json for org slug / project ID remapping
      --out string             Output directory for generated Terraform files (default "./terraform")
      --placeholders           Emit secrets.auto.tfvars.json with REPLACE_ME placeholders and a fill-in workbook
      --secrets string         Path to a secrets bundle.json — writes plaintext values to secrets.auto.tfvars.json
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

