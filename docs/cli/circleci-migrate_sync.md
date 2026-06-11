## circleci-migrate sync

Apply a manifest to the destination org (contexts, projects, and org settings).

### Synopsis

sync recreates exported data in the destination CircleCI organization.

It reads the manifest (structure + variable names), an optional secret bundle
(the plaintext values captured by the in-pipeline 'secrets' step), and an
optional mapping file (source->destination org/project mapping; defaults to the
same names). It is idempotent: existing resources are reused by name where
possible.

The destination org defaults to the SOURCE org from the manifest. To target a
DIFFERENT org you MUST pass --mapping with org.to set — otherwise sync runs
against your own source org (a prominent warning is printed in that case).

--mapping file schema (JSON):
  {
    "org": { "from": "gh/acme", "to": "gh/acme-new" },
    "projects": { "gh/acme/web": "gh/acme-new/web" },
    "github_org": { "from": "acme", "to": "acme-new" }
  }
Only "org.to" is required to retarget the destination org. "projects" remaps
individual project slugs (needed for GitHub App destinations whose slug is
"circleci/<org-id>/<project-id>"); "github_org" rewrites repo owners when repos
moved to a new GitHub org.

Secrets: env-var VALUES come from the captured secret bundle (--secrets). With
--apply but NO bundle, contexts/projects are created with EMPTY env-var values
that you must fill in manually — run 'circleci-migrate secrets capture' first to
capture the plaintext values, then pass --secrets <bundle>.

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
initial build). --yes / -y only matters together with --apply: it auto-confirms
enabling builds without the interactive prompt (it has no effect in a dry run).
Without a TTY, builds are not enabled unless --yes is passed.

When the manifest contains self-hosted runner resource classes, pass
--dest-runner-namespace to recreate them in the destination namespace. If the
flag is omitted, runner classes are flagged for manual recreation — the syncer
never guesses the destination namespace.

Examples:
  circleci-migrate sync --manifest manifest.json --secrets secrets.json
  circleci-migrate sync --manifest manifest.json --secrets secrets.json --apply
  circleci-migrate sync --manifest manifest.json --mapping mapping.json --apply
  circleci-migrate sync --manifest manifest.json --mapping mapping.json --secrets secrets.json --apply --yes
  circleci-migrate sync --manifest manifest.json --mapping mapping.json --dest-token $DST_TOKEN --apply
  circleci-migrate sync --manifest manifest.json --dest-runner-namespace acme-new --apply

```
circleci-migrate sync --manifest <file> [--secrets <file>] [--apply] [flags]
```

### Options

```
      --apply                          Write changes to the destination (default: dry run)
      --create-project-tokens          When set AND --apply, recreate each captured project API token on the destination project. CAUTION: each recreated token mints a NEW one-time secret — every consumer of the old token must be repointed to the new value. New plaintext values are printed to stderr once and cannot be retrieved again. Default false: emit manual steps only.
      --dest-github-org string         Destination GitHub organization owner (e.g. 'acme-new'). Use when all repos have moved to a new GitHub org. Takes precedence over the source owner when resolving repo external IDs; overridden by an explicit github_org entry in the mapping file. Requires --github-token.
      --dest-runner-namespace string   Destination runner namespace for recreating self-hosted runner resource classes (e.g. 'acme-new'). Must be supplied explicitly — the syncer never guesses the destination namespace. When omitted and the manifest contains runner classes, each is flagged for manual recreation.
      --github-token string            GitHub personal access token used to resolve repository IDs when creating pipeline definitions in a GitHub App destination org. Falls back to $GITHUB_TOKEN. Required when repos have been moved to a new GitHub org (--dest-github-org or mapping github_org). When omitted, the captured external_id is reused (correct for same-org migrations).
  -h, --help                           help for sync
      --json                           Print a machine-readable JSON summary to stdout instead of the human-readable per-section reports
      --manifest string                Path to the export manifest (required)
      --mapping string                 Path to a source->destination mapping file (JSON). REQUIRED to change the destination org name; without it sync targets the SOURCE org. Schema: { "org": {"from":"gh/acme","to":"gh/acme-new"}, "projects": {"gh/acme/web":"gh/acme-new/web"}, "github_org": {"from":"acme","to":"acme-new"} }. Only org.to is required to retarget; projects/github_org are optional.
      --missing-secrets string         How to handle variables with no captured value: 'skip' omits the variable entirely; 'placeholder' creates the variable with a placeholder value. Use 'placeholder' for restricted contexts whose values cannot be captured, so the variable name exists and can be filled in manually later. (default "skip")
      --secrets string                 Path to the captured secret bundle holding plaintext env-var values (optional). Without it, --apply creates resources with EMPTY env-var values; run 'secrets capture' first to populate them. (default "secrets.json")
      --skip-ciam                      Skip syncing CIAM roles and groups (standalone circleci-type orgs only)
      --skip-contexts                  Skip syncing contexts
      --skip-extras                    Skip syncing project checkout keys, additional SSH keys, webhooks, and schedules
      --skip-org-settings              Skip syncing org-level settings (feature flags, OIDC, URL-orb allow list, config policies)
      --skip-projects                  Skip syncing projects
      --skip-runner                    Skip syncing self-hosted runner resource classes
  -y, --yes                            Only with --apply: auto-confirm enabling builds after project creation (skip the interactive prompt). No effect in a dry run.
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

