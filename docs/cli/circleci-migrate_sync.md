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
  • Projects — OAuth projects are recreated in a paused state. App projects
    are created with their captured pipeline definitions and triggers
    (github_oauth sources are translated to github_app); an OAuth project with
    no captured definition gets one synthesized "build-and-test" definition,
    which needs --github-token (preflight fails without it). Triggers are
    created disabled. Re-running reconciles existing App projects: missing
    definitions/triggers are created, everything present reports "exists", and
    triggers still disabled are queued so --apply --yes enables them. A failed
    enable makes sync exit non-zero.
  • Self-hosted runner resource classes — only when --dest-runner-namespace
    is provided or the manifest contains runner classes.

By default sync performs a DRY RUN and writes nothing — review the plan, then
re-run with --apply. Context restrictions are handled as follows:
  • expression — copied verbatim.
  • group — resolved by name to the destination group UUID (requires the group
    to exist in the destination; falls back to "manual" if not found).
  • project — remapped automatically: source project UUID → source slug →
    destination slug (via mapping) → destination project UUID. For a GitHub App
    destination, a project with no mapping entry is matched by NAME against
    the destination org's projects instead (no 'projects' entries needed).
    Requires that the destination project exists once the projects step has run;
    if it does not, the restriction falls back to "manual" naming the project
    and the fix. Re-run sync after the projects are created to resolve it.

When OAuth projects are missing in the destination, --apply creates them in a
paused state (no webhook, no builds). After creation you are prompted to enable
builds (follow the project, which installs the webhook and may trigger an
initial build). For a GitHub App destination the prompt instead offers to enable
the N triggers that were created disabled, across M projects. --yes / -y only
matters together with --apply: it auto-confirms enabling builds without the
interactive prompt (it has no effect in a dry run). Without a TTY, builds are
not enabled unless --yes is passed.

--skip-pipelines (GitHub App destinations) creates or reuses each project and
syncs its settings, env vars and other project data, but creates no pipeline
definitions or triggers (reported as "skipped: --skip-pipelines"). Run it first
to create the App projects BEFORE the GitHub App is moved off the source org, so
'secrets transfer --include-project-vars' can run while the source's App
pipeline definitions still exist; then run sync --apply again WITHOUT the flag
to add the definitions and triggers to the existing projects.

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
  circleci-migrate sync --manifest manifest.json --mapping mapping.json --apply --skip-pipelines

```
circleci-migrate sync --manifest <file> [--secrets <file>] [--apply] [flags]
```

### Options

```
      --apply                                    Write changes to the destination (default: dry run)
      --create-project-tokens                    When set AND --apply, recreate each captured project API token on the destination project. CAUTION: each recreated token mints a NEW one-time secret — every consumer of the old token must be repointed to the new value. New plaintext values are printed to stderr once and cannot be retrieved again. Default false: emit manual steps only.
      --dest-github-org string                   Destination GitHub organization owner (e.g. 'acme-new'). Use when all repos have moved to a new GitHub org. Takes precedence over the source owner when resolving repo external IDs; overridden by an explicit github_org entry in the mapping file. Requires --github-token.
      --dest-orb-namespace string                Destination orb namespace to republish captured orb versions into (e.g. 'acme-new'). Must be supplied explicitly — the syncer never guesses the destination namespace. When omitted and the manifest contains orbs, each is flagged for manual recreation.
      --dest-runner-namespace string             Destination runner namespace for recreating self-hosted runner resource classes (e.g. 'acme-new'). Must be supplied explicitly — the syncer never guesses the destination namespace. When omitted and the manifest contains runner classes, each is flagged for manual recreation.
      --enable-existing-triggers                 Also enable triggers that ALREADY exist in the destination and are disabled while their source trigger was enabled (GitHub App destinations). Default: only triggers created in this run are enabled, so a re-run never re-enables a trigger you disabled on purpose. Use it on a re-run after answering N at the enable prompt. Needs --yes (or an interactive yes) to take effect.
      --github-token string                      GitHub personal access token (repo read) used to look up repository IDs when creating pipeline definitions in a GitHub App destination org. Falls back to $GITHUB_TOKEN. Required when repos have moved to a new GitHub org (--dest-github-org or mapping github_org), and for every OAuth project that has no captured GitHub App pipeline definition (a definition is synthesized for it and its repo ID is only available from GitHub) — preflight fails without it in that case, unless every such project already exists in the destination (or --skip-projects is set). Not needed when the GitHub org is unchanged and the manifest captured the repo IDs: those are reused.
  -h, --help                                     help for sync
      --include-danger-flags                     Write the 'danger' feature flags (org: drop_all_build_requests, require_context_group_restriction; project: drop_all_build_requests) to the destination. Default false: these are skipped (and surfaced as a manual step only when the source value is true) because enabling them on a freshly-migrated org/project can freeze or break pipelines. Set this for a faithful migration once the destination is validated and ready.
      --json                                     Print a machine-readable JSON summary to stdout instead of the human-readable per-section reports
      --manifest string                          Path to the export manifest (required)
      --mapping string                           Path to a source->destination mapping file (JSON). REQUIRED to change the destination org name; without it sync targets the SOURCE org. Schema: { "org": {"from":"gh/acme","to":"gh/acme-new"}, "projects": {"gh/acme/web":"gh/acme-new/web"}, "github_org": {"from":"acme","to":"acme-new"} }. Only org.to is required to retarget; projects/github_org are optional.
      --missing-secrets string                   How to handle variables with no captured value: 'skip' omits the variable entirely; 'placeholder' creates the variable with a placeholder value. Use 'placeholder' for restricted contexts whose values cannot be captured, so the variable name exists and can be filled in manually later. (default "skip")
      --only string                              Comma-separated list of sections to sync, skipping all others. Accepted values: org-settings, contexts, projects, runner, ciam, extras, orb. Example: --only org-settings,ciam,extras (to sync only CLI-only sections after terraform apply). Mutually exclusive with --skip-terraform-managed.
      --secrets string                           Path to the captured secret bundle holding plaintext env-var values (optional). Without it, --apply creates resources with EMPTY env-var values; run 'secrets capture' first to populate them. (default "secrets.json")
      --skip-ciam                                Skip syncing CIAM roles and groups (standalone circleci-type orgs only)
      --skip-contexts                            Skip syncing contexts
      --skip-extras                              Skip syncing project checkout keys, additional SSH keys, webhooks, and schedules
      --skip-orb                                 Skip syncing orbs
      --skip-org-settings                        Skip syncing org-level settings (feature flags, OIDC, URL-orb allow list, config policies)
      --skip-pipelines                           GitHub App destinations: create or reuse each project and sync its settings, env vars and other project data, but create NO pipeline definitions or triggers (reported as 'skipped: --skip-pipelines'). Use it to create the App projects BEFORE the GitHub App is moved off the source (OAuth) org, so 'secrets transfer --include-project-vars' can run while the source's App pipeline definitions still exist; a later run without the flag adds the definitions and triggers to the existing projects. No GitHub token is needed.
      --skip-preflight                           Skip the startup destination-side preflight checks (token validation, destination org reachability, cross-type warning, GitHub token check). Preflight runs by default before sync; use --skip-preflight in CI pipelines or when checks have been verified manually.
      --skip-projects                            Skip syncing projects
      --skip-runner                              Skip syncing self-hosted runner resource classes
      --skip-terraform-managed terraform apply   Skip syncing sections that Terraform manages (contexts, projects, runner resource classes). Use this for the CLI gap-fill step after terraform apply to avoid overwriting resources Terraform already owns. Syncs org-settings, CIAM, and extras (checkout-keys, ssh-keys, schedules). Mutually exclusive with --only.
      --webhook-secrets-out string               With --apply, write the NEW secrets of recreated webhooks to this file (JSON, mode 0600): a freshly generated signing secret for each outbound webhook created in this run (sent on create) and the new URL of each recreated inbound custom-webhook trigger. The source secrets are unreadable, so every receiver/sender must be given the new values. Secrets are never printed. Without the flag, webhooks are created with an empty signing secret and the manual steps say where to set it. Only affects webhooks created in this run.
  -y, --yes                                      Only with --apply: auto-confirm enabling builds after project creation (skip the interactive prompt). No effect in a dry run.
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

