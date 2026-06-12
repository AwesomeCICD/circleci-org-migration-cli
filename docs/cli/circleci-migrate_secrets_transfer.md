## circleci-migrate secrets transfer

Transfer context env-var values directly source→dest via an in-pipeline PUT (no bundle file).

### Synopsis

transfer is a ZERO-DISK-WRITE mode for migrating context env-var VALUES directly
from the SOURCE org to the DESTINATION org without producing any bundle file.

Instead of writing values to a build artifact (as 'secrets capture' does),
'secrets transfer' triggers a single dynamic pipeline in the SOURCE org with
one job per context. Each job imports the source context (CircleCI unmasks
the values into the job environment) and PUTs each value directly into the
matching context in the DESTINATION org via the CircleCI API over TLS.

NO plaintext ever touches disk or build artifacts — strictly better security
than the encrypted-bundle-artifact flow for context variables.

WHEN TO USE:
  - You trust the source org's pipeline infrastructure and want the simplest,
    most secure migration path for context env-var values.
  - Your destination contexts already exist (created by 'sync --apply') and
    you only need to fill in the values.
  - You do NOT need a local copy of the secret values.

WHEN TO USE 'secrets capture' INSTEAD:
  - You need a local bundle for review, backup, or air-gapped flows.
  - You are migrating SSH keys or project env vars (transfer is context-only).
  - You want to inspect values before writing them to the destination.

PREREQUISITES:
  1. Run 'export' to produce manifest.json.
  2. Run 'sync --apply' to create the destination contexts (empty shells).
  3. Store the DESTINATION org API token in a source-org context, e.g.:
       context name: "migration-secrets"
       env var:       CIRCLECI_DEST_TOKEN = <dest-org-api-token>
     Pass that context name via --dest-token-context.
  4. Run 'secrets transfer --apply' to execute the transfer pipeline.

DRY RUN (default — safe to run without --apply):
  Without --apply, transfer prints a plan: which contexts and variables would
  be transferred, and where the dest token is expected. No pipeline is
  triggered.

  circleci-migrate secrets transfer --manifest manifest.json \
    --dest-org-id <uuid> --dest-token-context migration-secrets

APPLY — execute the transfer:
  circleci-migrate secrets transfer --manifest manifest.json \
    --dest-org-id <uuid> --dest-token-context migration-secrets --apply

TRUST MODEL & SECURITY:
  The in-pipeline jobs need the destination API token. The CLI does NOT embed
  the token value in the generated config. Instead, you store the dest token
  in a source-org context, and the CLI embeds that context NAME. CircleCI
  injects the token into the job as an environment variable.

  Security implication: any source-org admin who can create pipelines or attach
  contexts to jobs has implicit access to the dest token (the same access they
  have to any other sensitive context in the source org). Mitigate by:
    - Using a scoped API token for the destination (write to contexts only).
    - Rotating the dest token after the transfer is complete.
    - Restricting the source context holding the dest token to the minimum
      projects/pipelines that need it.

  The dest token is referenced in the config ONLY as ${CIRCLECI_DEST_TOKEN}
  (or your custom --dest-token-env-var name). The literal value never appears
  in the generated YAML.

SCOPE DISCIPLINE:
  transfer handles CONTEXT ENV-VAR VALUES ONLY. For SSH keys, project env
  vars, and air-gapped flows, use 'secrets capture' with an encrypted bundle.

Examples:
  # Dry run — see what would be transferred (no pipeline triggered):
  circleci-migrate secrets transfer --manifest manifest.json \
    --dest-org-id <dest-org-uuid> \
    --dest-token-context migration-secrets

  # Transfer all contexts with values (requires --apply):
  circleci-migrate secrets transfer --manifest manifest.json \
    --dest-org-id <dest-org-uuid> \
    --dest-token-context migration-secrets \
    --enable-trigger --apply

  # Transfer specific contexts only:
  circleci-migrate secrets transfer --manifest manifest.json \
    --dest-org-id <dest-org-uuid> \
    --dest-token-context migration-secrets \
    --context deploy-prod --context shared \
    --apply

  # Custom dest token env-var name:
  circleci-migrate secrets transfer --manifest manifest.json \
    --dest-org-id <dest-org-uuid> \
    --dest-token-context migration-secrets \
    --dest-token-env-var MY_DEST_API_TOKEN \
    --apply

  # Custom dest host (CircleCI Server installations):
  circleci-migrate secrets transfer --manifest manifest.json \
    --dest-org-id <dest-org-uuid> \
    --dest-token-context migration-secrets \
    --dest-host https://circleci.example.com \
    --apply

```
circleci-migrate secrets transfer [--manifest <file>] --dest-org-id <uuid> --dest-token-context <ctx> [flags]
```

### Options

```
      --apply                       Execute the transfer pipeline (default: dry-run — prints the plan but triggers no pipeline). Pass --apply to actually write values to the destination org.
      --branch string               Branch to check out for the transfer pipeline run (default "main")
      --context stringArray         Context name(s) to transfer (default: all contexts with at least one env var in the manifest)
      --dest-host string            Destination CircleCI host URL (default: https://circleci.com; override for Server installs)
      --dest-org-id string          Destination org UUID (required). Find it in your manifest ('source.org.id') or the CircleCI org settings page. The in-pipeline job lists destination contexts by owner ID.
      --dest-token-context string   Name of the SOURCE-org context that holds the destination API token (the env var within that context is set by --dest-token-env-var). SECURITY: source-org admins with access to this context can read the token. Use a scoped token and rotate it after transfer.
      --dest-token-env-var string   Name of the env var inside --dest-token-context that holds the destination API token (default: CIRCLECI_DEST_TOKEN) (default "CIRCLECI_DEST_TOKEN")
      --enable-trigger              Enable api-trigger-with-config at the org level if not already on, and restore after transfer (the project-level flag must be enabled separately or already be on)
  -h, --help                        help for transfer
      --host-project string         Source-org project slug under which the transfer pipeline runs. Any project with api-trigger-with-config enabled works. Auto-picked from the manifest when omitted.
      --manifest string             Path to the export manifest (required)
      --mapping string              Path to mapping.json for context name overrides (optional). Entries in the 'projects' map whose keys do not contain '/' are treated as context name → destination name mappings.
      --poll-timeout duration       Maximum time to wait for the transfer pipeline to complete (0 = no timeout) (default 30m0s)
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

* [circleci-migrate secrets](circleci-migrate_secrets.md)	 - Capture secret values that the API cannot expose (RECOMMENDED: use 'secrets capture').

