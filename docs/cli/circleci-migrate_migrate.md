## circleci-migrate migrate

All-in-one: export source org and sync into destination org.

### Synopsis

migrate combines 'export' and 'sync' into a single command.

When run WITHOUT --source-org and --dest-org on an interactive terminal,
migrate launches a guided walkthrough that prompts for each required value and
lets you choose which parts of the org to migrate. This interactive mode is
designed for first-time use and manual one-off migrations.

NOTE: interactive prompts are written to stderr; if you pipe stdout while
relying on the guided prompts, use a TTY for stdin — piping stdin triggers
non-TTY mode and skips all prompts (use --no-input to make this explicit).

When --source-org and --dest-org are provided, migrate runs non-interactively
using only the supplied flags — suitable for scripting and CI pipelines. Pass
--no-input (or run with stdin redirected / piped) to make the command error
immediately if any required value is missing, instead of blocking on a prompt.

It reads data from the source CircleCI organisation (using the source token),
builds an in-memory manifest, and immediately applies it to the destination
organisation (using the dest token) — without requiring a manifest file on
disk.

Secret VALUES are never exported via the API. If you have a captured secret
bundle (produced by the in-pipeline 'secrets' step), pass it with --secrets.
Without a bundle, all variable values are reported as needing manual entry
(or use --missing-secrets=placeholder to write placeholder values).

IN-PIPELINE SECRETS TRANSFER (opt-in):
  Pass --transfer-secrets together with --dest-token-context to run the
  in-pipeline transfer step after sync completes. This transfers context
  env-var values directly from the source org to the destination org without
  writing any bundle to disk. Mutually exclusive with --secrets.

  The project slug mapping is derived automatically from --source-org and
  --dest-org: for gh/ and bb/ dest orgs the dest slug is
  <provider>/<dest-org-name>/<repo>; this is the same derivation used by
  'mapping generate'. Pass --include-project-vars to also transfer project
  env-var values. Pass --include-ssh-keys to also transfer additional project
  SSH keys in-pipeline (zero-disk; private key material is never echoed to logs).

  Requires:
    --dest-token-context <name>   source-org context that holds CIRCLECI_DEST_TOKEN
    --transfer-secrets            opt-in flag to activate this step

  See 'secrets transfer --help' for full documentation of the in-pipeline flow.

By default migrate performs a DRY RUN and writes nothing to the destination.
Review the output, then re-run with --apply to write changes. Pass --yes / -y
to auto-confirm enabling builds for newly-created projects without a prompt.

Use --output / -o to save the exported manifest to disk, and --report to save
a human-readable audit document. Both flags are optional; omitting them keeps
the migration entirely in-memory.

For more control — e.g. to inspect or edit the manifest between steps — run
'export' and 'sync' separately.

Examples:
  # Interactive guided walkthrough (no flags required):
  circleci-migrate migrate

  # Non-interactive (flags bypass all prompts):
  circleci-migrate migrate \
    --source-org gh/acme --dest-org gh/acme-new \
    --source-token $SRC_TOKEN --dest-token $DST_TOKEN

  # CI pipeline (non-interactive, apply immediately):
  circleci-migrate migrate \
    --source-org gh/acme --dest-org gh/acme-new \
    --secrets secrets.json --apply --yes --no-input

  # Save manifest and audit report:
  circleci-migrate migrate \
    --source-org gh/acme --dest-org gh/acme-new \
    --apply -o manifest.json --report migration-report.md

  # In-pipeline secrets transfer (no bundle file):
  circleci-migrate migrate \
    --source-org gh/acme --dest-org gh/acme-new \
    --transfer-secrets --dest-token-context migration-secrets \
    --apply

  # In-pipeline transfer including project env vars:
  circleci-migrate migrate \
    --source-org gh/acme --dest-org gh/acme-new \
    --transfer-secrets --dest-token-context migration-secrets \
    --include-project-vars --apply

  # In-pipeline transfer including project env vars AND SSH keys:
  circleci-migrate migrate \
    --source-org gh/acme --dest-org gh/acme-new \
    --transfer-secrets --dest-token-context migration-secrets \
    --include-project-vars --include-ssh-keys --apply

```
circleci-migrate migrate [--source-org <slug> --dest-org <slug>] [--apply] [flags]
```

### Options

```
      --apply                          Write changes to the destination (default: dry run)
      --create-project-tokens          When set AND --apply, recreate each captured project API token on the destination project. CAUTION: each recreated token mints a NEW one-time secret — every consumer of the old token must be repointed to the new value. New plaintext values are printed to stderr once and cannot be retrieved again. Default false: emit manual steps only.
      --dest-github-org string         Destination GitHub organization owner (e.g. 'acme-new'). Use when all repos have moved to a new GitHub org. Takes precedence over the source owner when resolving repo external IDs; overridden by an explicit github_org entry in the mapping file. Requires --github-token.
      --dest-orb-namespace string      Destination orb namespace to republish captured orb versions into (e.g. 'acme-new'). Must be supplied explicitly — the syncer never guesses the destination namespace. When omitted and the manifest contains orbs, each is flagged for manual recreation.
      --dest-org string                CircleCI organization slug for the destination org, e.g. gh/my-new-org (shown in CircleCI → Organization Settings → Overview). This is the CircleCI org identifier, not a GitHub repository URL. (required, or prompted interactively)
      --dest-runner-namespace string   Destination runner namespace for recreating self-hosted runner resource classes (e.g. 'acme-new'). Must be supplied explicitly — the syncer never guesses the destination namespace. When omitted and the manifest contains runner classes, each is flagged for manual recreation.
      --dest-token-context string      Name of the source-org context that holds the destination API token (env var: CIRCLECI_DEST_TOKEN). Required when --transfer-secrets is set.
      --follow-all                     (GitHub OAuth orgs only) Before exporting, list all GitHub repos in the source org and follow any not yet set up as CircleCI projects, making them visible to subsequent discovery. Requires --github-token. Archived repos are skipped. Webhook-validation errors on brand-new repos are warned and skipped, not fatal. Not applicable to circleci/ (App/standalone) orgs — a note is printed and this flag is ignored.
      --github-token string            GitHub personal access token used to resolve repository IDs when creating pipeline definitions in a GitHub App destination org. Falls back to $GITHUB_TOKEN. Required when repos have been moved to a new GitHub org (--dest-github-org or mapping github_org).
  -h, --help                           help for migrate
      --host-project string            When --transfer-secrets is set, the source-org project slug whose pipeline runs the context transfer (e.g. gh/acme/web). Defaults to the first project. Prefer an ESTABLISHED (long-followed) project — a just-followed project's context authorization may not have propagated yet.
      --include-danger-flags           Write the 'danger' feature flags (org: drop_all_build_requests, require_context_group_restriction; project: drop_all_build_requests) to the destination. Default false: these are skipped (and surfaced as a manual step only when the source value is true) because enabling them on a freshly-migrated org/project can freeze or break pipelines. Set this for a faithful migration once the destination is validated and ready.
      --include-project-vars           When --transfer-secrets is set, also transfer project-level env-var values to the corresponding destination projects. Destination project slugs are derived from --dest-org (gh/ and bb/ orgs only). Projects without a derivable destination slug are skipped.
      --include-ssh-keys               When --transfer-secrets is set, also transfer additional project SSH keys to the destination projects via the in-pipeline zero-disk path. Private key material is read with jq --rawfile and never echoed to logs. Destination project slugs are derived from --dest-org (gh/ and bb/ orgs only). Projects without a derivable destination slug are skipped.
      --json                           Print a machine-readable JSON summary to stdout instead of the human-readable output; progress is written to stderr
      --mapping string                 Path to a source->destination mapping file (optional)
      --missing-secrets string         How to handle variables with no captured value: skip|placeholder (default "skip")
      --no-input                       Disable all interactive prompts; error if a required value is missing (implied when stdin is not a TTY)
      --orb-namespace string           Source orb namespace to capture published orbs from (e.g. 'acme'). Both public and private orbs are captured along with every stable version and its raw YAML source. The namespace must be supplied explicitly — there is no clean org→namespace lookup.
  -o, --output string                  Optional: save the exported manifest to this path (omit to keep migration entirely in-memory)
      --preflight-only                 Run the preflight checks and print the summary, then exit without performing export or sync. Exits non-zero if any check is a hard failure; exits 0 on warnings (unless --skip-preflight is also set). Use this to validate configuration before committing to a migration run.
      --remove-restrictions            When --transfer-secrets is set, temporarily remove project/expression restrictions from source contexts before the transfer pipeline runs, then restore them afterwards. Use when a context has restrictions that prevent the host project from using it. Group restrictions (including the default 'All members') are never removed.
      --report string                  Optional: save the human-readable audit report to this path (omit to skip writing the report)
      --runner-namespace string        Source runner namespace to capture self-hosted runner resource classes from (e.g. 'acme'). The namespace must be supplied explicitly — there is no clean org→namespace lookup.
      --secrets string                 Path to a captured secret bundle (optional) (default "secrets.json")
      --skip-ciam                      Skip syncing CIAM roles and groups (standalone circleci-type orgs only)
      --skip-contexts                  Skip exporting and syncing contexts
      --skip-extras                    Skip checkout keys, webhooks, and schedules
      --skip-orb                       Skip exporting and syncing orbs
      --skip-org-settings              Skip syncing org-level settings (feature flags, OIDC, URL-orb allow list, config policies)
      --skip-preflight                 Skip the startup preflight checks (token validation, org reachability, cross-type warning, api-trigger flag, project discovery). Preflight runs by default before export/sync; use --skip-preflight in CI pipelines or when checks have already been verified manually.
      --skip-projects                  Skip exporting and syncing projects
      --skip-runner                    Skip exporting and syncing self-hosted runner resource classes
      --skip-validate                  Skip the automatic post-apply parity check that runs after a successful --apply. Validation is also skipped when --json is set (to keep JSON output clean). Use --skip-validate in CI pipelines where you run 'validate' as a separate step or when re-export of the destination org is not desirable immediately after apply.
      --source-org string              CircleCI organization slug for the source org, e.g. gh/my-org (shown in CircleCI → Organization Settings → Overview). This is the CircleCI org identifier, not a GitHub repository URL. (required, or prompted interactively)
      --transfer-secrets               After sync, run the in-pipeline secrets transfer to copy context env-var values directly from source to destination without writing a bundle file. Requires --dest-token-context. Mutually exclusive with --secrets.
  -y, --yes                            Auto-confirm enabling builds after project creation (skip the interactive prompt)
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

