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

```
circleci-migrate migrate [--source-org <slug> --dest-org <slug>] [--apply] [flags]
```

### Options

```
      --apply                          Write changes to the destination (default: dry run)
      --dest-github-org string         Destination GitHub organization owner (e.g. 'acme-new'). Use when all repos have moved to a new GitHub org. Takes precedence over the source owner when resolving repo external IDs; overridden by an explicit github_org entry in the mapping file. Requires --github-token.
      --dest-org string                Destination organization slug: gh/<org> or circleci/<org-id> (required, or prompted interactively)
      --dest-runner-namespace string   Destination runner namespace for recreating self-hosted runner resource classes (e.g. 'acme-new'). Must be supplied explicitly — the syncer never guesses the destination namespace. When omitted and the manifest contains runner classes, each is flagged for manual recreation.
      --github-token string            GitHub personal access token used to resolve repository IDs when creating pipeline definitions in a GitHub App destination org. Falls back to $GITHUB_TOKEN. Required when repos have been moved to a new GitHub org (--dest-github-org or mapping github_org).
  -h, --help                           help for migrate
      --mapping string                 Path to a source->destination mapping file (optional)
      --missing-secrets string         How to handle variables with no captured value: skip|placeholder (default "skip")
      --no-input                       Disable all interactive prompts; error if a required value is missing (implied when stdin is not a TTY)
  -o, --output string                  Optional: save the exported manifest to this path (omit to keep migration entirely in-memory)
      --report string                  Optional: save the human-readable audit report to this path (omit to skip writing the report)
      --runner-namespace string        Source runner namespace to capture self-hosted runner resource classes from (e.g. 'acme'). The namespace must be supplied explicitly — there is no clean org→namespace lookup.
      --secrets string                 Path to a captured secret bundle (optional) (default "secrets.json")
      --skip-contexts                  Skip exporting and syncing contexts
      --skip-extras                    Skip checkout keys, webhooks, and schedules
      --skip-org-settings              Skip syncing org-level settings (feature flags, OIDC, URL-orb allow list, config policies)
      --skip-projects                  Skip exporting and syncing projects
      --source-org string              Source organization slug: gh/<org> or circleci/<org-id> (required, or prompted interactively)
  -y, --yes                            Auto-confirm enabling builds after project creation (skip the interactive prompt)
```

### Options inherited from parent commands

```
      --debug                 Enable debug logging
      --dest-token string     API token for the destination org (env: CIRCLECI_DEST_TOKEN)
      --host string           CircleCI host URL (env: CIRCLECI_CLI_HOST or CIRCLECI_HOST) (default "https://circleci.com")
      --source-token string   API token for the source org (env: CIRCLECI_SOURCE_TOKEN)
      --token string          Personal API token — fallback for both orgs (env: CIRCLECI_CLI_TOKEN)
```

### SEE ALSO

* [circleci-migrate](circleci-migrate.md)	 - Migrate data between CircleCI organisations.

