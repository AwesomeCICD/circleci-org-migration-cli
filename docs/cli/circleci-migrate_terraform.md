## circleci-migrate terraform

Generate Terraform configurations from an exported manifest.

### Synopsis

terraform generates Terraform HCL and tfvars files from a circleci-migrate
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
    --out ./terraform/

### Options

```
  -h, --help   help for terraform
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

* [circleci-migrate](circleci-migrate.md)	 - Migrate data between CircleCI organisations.
* [circleci-migrate terraform generate](circleci-migrate_terraform_generate.md)	 - Generate Terraform HCL and tfvars from an exported manifest.

