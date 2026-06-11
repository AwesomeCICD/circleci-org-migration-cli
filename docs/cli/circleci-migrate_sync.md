## circleci-migrate sync

Apply a manifest to the destination org (contexts, projects, and org settings).

### Synopsis

sync recreates exported data in the destination CircleCI organization.

It reads the manifest (structure + variable names), an optional secret bundle
(the plaintext values captured by the in-pipeline 'secrets' step), and an
optional mapping file (source->destination org/project mapping; defaults to the
same names). It is idempotent: existing resources are reused by name where
possible.

Resources synced (in order):
  • Org settings — feature flags, OIDC claims, URL-orb allow list, config
    policies, OTel exporter, contacts, storage retention, budgets, release
    tracker, and block-unregistered-users.
  • Contexts — with environment variable values from the secret bundle.
  • Projects — OAuth projects are recreated in a paused state; App projects
    are created with a pipeline definition and trigger.
  • Self-hosted runner resource classes — only when --dest-runner-namespace
    is provided or the manifest contains runner classes.

By default sync performs a DRY RUN and writes nothing — review the plan, then
re-run with --apply. Group and project-type context restrictions are flagged
for manual recreation (group writes are not GA; project restriction values are
source-org IDs).

When OAuth projects are missing in the destination, --apply creates them in a
paused state (no webhook, no builds). After creation you are prompted to enable
builds (follow the project, which installs the webhook and may trigger an
initial build). Pass --yes / -y to auto-confirm without a prompt, or run without
a TTY and later re-run with --apply --yes to enable builds.

When the manifest contains self-hosted runner resource classes, pass
--dest-runner-namespace to recreate them in the destination namespace. If the
flag is omitted, runner classes are flagged for manual recreation — the syncer
never guesses the destination namespace.

Examples:
  circleci-migrate sync --manifest manifest.json --secrets secrets.json
  circleci-migrate sync --manifest manifest.json --secrets secrets.json --apply
  circleci-migrate sync --manifest manifest.json --mapping mapping.json --apply
  circleci-migrate sync --manifest manifest.json --apply --yes
  circleci-migrate sync --manifest manifest.json --dest-runner-namespace acme-new --apply

```
circleci-migrate sync --manifest <file> [--secrets <file>] [--apply] [flags]
```

### Options

```
      --apply                          Write changes to the destination (default: dry run)
      --dest-github-org string         Destination GitHub organization owner (e.g. 'acme-new'). Use when all repos have moved to a new GitHub org. Takes precedence over the source owner when resolving repo external IDs; overridden by an explicit github_org entry in the mapping file. Requires --github-token.
      --dest-runner-namespace string   Destination runner namespace for recreating self-hosted runner resource classes (e.g. 'acme-new'). Must be supplied explicitly — the syncer never guesses the destination namespace. When omitted and the manifest contains runner classes, each is flagged for manual recreation.
      --github-token string            GitHub personal access token used to resolve repository IDs when creating pipeline definitions in a GitHub App destination org. Falls back to $GITHUB_TOKEN. Required when repos have been moved to a new GitHub org (--dest-github-org or mapping github_org). When omitted, the captured external_id is reused (correct for same-org migrations).
  -h, --help                           help for sync
      --manifest string                Path to the export manifest (required)
      --mapping string                 Path to a source->destination mapping file (optional)
      --missing-secrets string         How to handle variables with no captured value: skip|placeholder (default "skip")
      --secrets string                 Path to the captured secret bundle (optional) (default "secrets.json")
      --skip-contexts                  Skip syncing contexts
      --skip-org-settings              Skip syncing org-level settings (feature flags, OIDC, URL-orb allow list, config policies)
      --skip-projects                  Skip syncing projects
  -y, --yes                            Auto-confirm enabling builds after project creation (skip the interactive prompt)
```

### Options inherited from parent commands

```
      --debug                 Enable debug logging
      --dest-token string     API token for the destination org (env: CIRCLECI_DEST_TOKEN)
      --host string           CircleCI host URL (env: CIRCLECI_HOST) (default "https://circleci.com")
      --source-token string   API token for the source org (env: CIRCLECI_SOURCE_TOKEN)
      --token string          Personal API token — fallback for both orgs (env: CIRCLECI_CLI_TOKEN)
```

### SEE ALSO

* [circleci-migrate](circleci-migrate.md)	 - Migrate data between CircleCI organisations.

