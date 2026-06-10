# Worked migration examples

This document contains complete, copy-pasteable examples for the most common
migration scenarios. All examples use the fictional organizations `gh/acme`
(source) and `gh/acme-cloud` (destination) — substitute your own slugs
throughout.

---

## Before you start

Set your tokens as environment variables so you don't pass them on the command
line:

```bash
export CIRCLECI_SOURCE_TOKEN="your-source-org-personal-api-token"
export CIRCLECI_DEST_TOKEN="your-destination-org-personal-api-token"
```

Install `circleci-migrate` (see [Install in the README](../README.md#install)).

---

## Example 1 — OAuth → OAuth (same integration type)

**Scenario:** move `gh/acme` to a new GitHub organization `gh/acme-cloud`. Both
organizations use the GitHub OAuth integration.

### Step 1 — Export the source org

```bash
circleci-migrate export \
  --org gh/acme \
  --output manifest.json \
  --report migration-report.md
```

Review `migration-report.md`. Items listed under "Warnings & manual follow-ups"
require action outside the tool (SSO, audit-log streaming, webhook signing
secrets, etc.).

### Step 2 — Capture secret values

Secret values are never returned by the CircleCI API. You must capture them
from inside a CircleCI pipeline. See the full [secrets capture
flow](#example-5--secrets-capture-in-detail) below — here is the quick version
using the CLI-orchestrated approach:

```bash
circleci-migrate secrets capture \
  --org gh/acme \
  --manifest manifest.json \
  --output secrets.json
```

> **Protect `secrets.json`** — it contains plaintext values. Set a short
> artifact retention period, do not commit it, and delete it after the sync.

### Step 3 — Dry run (review the plan)

```bash
circleci-migrate sync \
  --manifest manifest.json \
  --secrets secrets.json \
  --dest-token "$CIRCLECI_DEST_TOKEN"
# No --apply → dry run only. Review output before continuing.
```

The output shows every action as `created (would create)`, `set (would set)`,
or `manual` — nothing is written.

### Step 4 — Apply

```bash
circleci-migrate sync \
  --manifest manifest.json \
  --secrets secrets.json \
  --apply
# Prompts: "Enable builds for N project(s) now? [y/N]"
# Pass --yes to auto-confirm, or answer interactively.
```

Or skip straight to apply with the all-in-one `migrate` command:

```bash
circleci-migrate migrate \
  --source-org gh/acme \
  --dest-org gh/acme-cloud \
  --secrets secrets.json \
  --apply --yes \
  --output manifest.json \
  --report migration-report.md
```

### Step 5 — Rotate secrets

After confirming the destination builds are healthy, rotate every captured
value and delete `secrets.json`.

---

## Example 2 — GitHub App → GitHub App (same integration type)

**Scenario:** move `circleci/<src-uuid>` to `circleci/<dst-uuid>`. Both
organizations use the GitHub App integration.

GitHub App orgs use UUID-based slugs. Find them in your CircleCI org settings
URL: `https://app.circleci.com/settings/organization/circleci/<uuid>/overview`.

```bash
SRC_UUID="11111111-1111-1111-1111-111111111111"
DST_UUID="22222222-2222-2222-2222-222222222222"
```

### Step 1 — Export

```bash
circleci-migrate export \
  --org "circleci/$SRC_UUID" \
  --output manifest.json \
  --report migration-report.md
```

### Step 2 — Capture secrets (orb-based)

Commit `manifest.json` to a repository in your source GitHub App org and add a
capture workflow. See [Example 5](#example-5--secrets-capture-in-detail) for
the full orb config. After the pipeline completes, download `secrets.json` from
the `merge` job's artifacts.

### Step 3 — Dry run

```bash
circleci-migrate sync \
  --manifest manifest.json \
  --secrets secrets.json \
  --dest-token "$CIRCLECI_DEST_TOKEN"
```

Review the plan. For App orgs, the output will show pipeline definitions and
disabled triggers being created.

### Step 4 — Apply

```bash
circleci-migrate sync \
  --manifest manifest.json \
  --secrets secrets.json \
  --apply --yes
```

App org projects are created with triggers **disabled** (`disabled: true`).
`--yes` enables them automatically after creation. To defer enabling (no builds
fire until you are ready):

```bash
circleci-migrate sync \
  --manifest manifest.json \
  --secrets secrets.json \
  --apply
# Do NOT pass --yes. Answer N (or press Enter) at the enable-builds prompt.
# Re-run with --apply --yes later when you are ready to go live.
```

### Step 5 — Resolve GitHub repository IDs (if repos are in a different GitHub org)

If the destination CircleCI org is connected to a **different GitHub
organization** than the source, pass `--github-token` to let the tool resolve
the correct `external_id` for each repository:

```bash
export GITHUB_TOKEN="your-github-pat"

circleci-migrate sync \
  --manifest manifest.json \
  --secrets secrets.json \
  --dest-github-org acme-cloud \
  --apply --yes
# --github-token falls back to $GITHUB_TOKEN automatically
```

Or use the `migrate` all-in-one:

```bash
circleci-migrate migrate \
  --source-org "circleci/$SRC_UUID" \
  --dest-org   "circleci/$DST_UUID" \
  --secrets secrets.json \
  --dest-github-org acme-cloud \
  --apply --yes
```

---

## Example 3 — Mixed org (GitHub App + OAuth) → mixed org

**Background:** when a GitHub organization has both the legacy OAuth and the
newer GitHub App CircleCI integrations active simultaneously, CircleCI registers
them as **two separate organization records** — one with a `gh/<org>` slug and
one with a `circleci/<uuid>` slug. To fully migrate such an environment you run
`circleci-migrate` **twice**: once for the OAuth record and once for the App
record.

```
Source side:
  gh/acme              ← OAuth record
  circleci/<src-uuid>  ← App record

Destination side:
  gh/acme-cloud              ← OAuth record
  circleci/<dst-uuid>        ← App record
```

**Leg 1 — OAuth record:**

```bash
# Export and capture for the OAuth side
circleci-migrate export --org gh/acme -o manifest-oauth.json --report report-oauth.md
circleci-migrate secrets capture \
  --org gh/acme \
  --manifest manifest-oauth.json \
  --output secrets-oauth.json

# Sync OAuth → OAuth
circleci-migrate sync \
  --manifest manifest-oauth.json \
  --secrets secrets-oauth.json \
  --apply --yes
```

**Leg 2 — App record:**

```bash
SRC_UUID="11111111-1111-1111-1111-111111111111"
DST_UUID="22222222-2222-2222-2222-222222222222"

# Export and capture for the App side
circleci-migrate export \
  --org "circleci/$SRC_UUID" \
  -o manifest-app.json \
  --report report-app.md

# Capture via orb (see Example 5) — download secrets-app.json from artifacts

# Sync App → App
circleci-migrate sync \
  --manifest manifest-app.json \
  --secrets secrets-app.json \
  --apply --yes
```

> **Note:** contexts and org-level settings may overlap between the two records.
> Review both audit reports (`report-oauth.md`, `report-app.md`) before
> applying, and consider using `--skip-org-settings` on the second leg if you
> do not want to double-apply org-level flags.

---

## Example 4 — Cross-type: OAuth → GitHub App (follow-on migration)

**Scenario:** `gh/acme` (OAuth) migrating to `circleci/<dst-uuid>` (GitHub App).

This is a **follow-on** migration — typically done after an OAuth org has been
moved and you want to consolidate onto the GitHub App integration. Key
data-loss caveats:

- `build_fork_prs` (fork PR builds) — the GitHub App never builds fork PRs;
  this setting cannot be replicated and is recorded as `manual` in the report.
- The OSS flag and `pr_only_branch_overrides` have no App equivalent.
- Multiple App pipeline definitions cannot be created from a single OAuth
  project config — the tool creates one pipeline definition per project using
  the source project's default config path (`.circleci/config.yml`).

```bash
DST_UUID="22222222-2222-2222-2222-222222222222"

# Export the OAuth source
circleci-migrate export \
  --org gh/acme \
  --output manifest.json \
  --report migration-report.md

# Review migration-report.md — pay attention to fork-PR and OSS flags.

# Capture secrets
circleci-migrate secrets capture \
  --org gh/acme \
  --manifest manifest.json \
  --output secrets.json

# Dry run (cross-type: dest is App)
circleci-migrate sync \
  --manifest manifest.json \
  --secrets secrets.json \
  --dest-token "$CIRCLECI_DEST_TOKEN"

# Apply
circleci-migrate sync \
  --manifest manifest.json \
  --secrets secrets.json \
  --apply --yes
```

You must supply a mapping file when org names differ so the tool knows how to
map project slugs:

```json
{
  "org": { "from": "gh/acme", "to": "circleci/22222222-2222-2222-2222-222222222222" },
  "projects": {
    "gh/acme/web": "circleci/22222222-2222-2222-2222-222222222222/web",
    "gh/acme/api": "circleci/22222222-2222-2222-2222-222222222222/api"
  }
}
```

```bash
circleci-migrate sync \
  --manifest manifest.json \
  --secrets secrets.json \
  --mapping mapping.json \
  --apply --yes
```

---

## Example 5 — Secrets capture in detail

Secret values are **never returned by the CircleCI API**. The only way to
migrate them is to run inside a CircleCI pipeline, where a context's variables
are injected as real environment variables.

There are two approaches:

### Option A — CLI-orchestrated (`secrets capture`)

Best for: ad-hoc migrations where you do not want to commit a config.

`secrets capture` generates a pipeline config at runtime, submits it as an
inline (unversioned) config to the CircleCI Pipelines API, waits for the run,
and downloads the merged `secrets.json` — all without touching `.circleci/config.yml`.

```bash
circleci-migrate secrets capture \
  --org gh/acme \
  --manifest manifest.json \
  --output secrets.json
```

If the pipeline trigger is paused (common for App orgs), add
`--enable-trigger` to temporarily enable it for the run:

```bash
circleci-migrate secrets capture \
  --org gh/acme \
  --manifest manifest.json \
  --output secrets.json \
  --enable-trigger
```

If any contexts have restrictions that block the inline pipeline, use one of:

```bash
# Remove restrictions temporarily, restore after run
--remove-restrictions

# Skip contexts with restrictions entirely
--skip-restricted-contexts
```

### Option B — Orb-based (committed config)

Best for: large numbers of contexts, auditability, or when you want full
control over the pipeline.

Commit `manifest.json` to a repository in your source org (it contains no
secrets) and add a workflow using the `awesomecicd/circleci-org-migration` orb.

Each job must reference **exactly one context** — do not mix contexts in a
single job, or one context's variables will overwrite another's when they share
a name.

```yaml
# .circleci/config.yml in your SOURCE org
version: "2.1"
orbs:
  migrate: awesomecicd/circleci-org-migration@0.2.0

workflows:
  capture-secrets:
    jobs:
      - migrate/extract_context:
          name: extract-deploy-prod
          context_name: deploy-prod
          context:
            - deploy-prod
      - migrate/extract_context:
          name: extract-shared
          context_name: shared
          context:
            - shared
      - migrate/merge:
          name: merge-secrets
          requires:
            - extract-deploy-prod
            - extract-shared
```

Push this config, wait for the `merge-secrets` job to complete, then download
`secrets.json` from the `merge-secrets` job's **Artifacts** tab in the
CircleCI UI.

**Many contexts — use a matrix:**

When you have many contexts, a matrix expands a single job stanza into one job
per context name. The matrix **must** declare an explicit `alias` so the
`merge` job can depend on the whole matrix by that alias:

```yaml
version: "2.1"
orbs:
  migrate: awesomecicd/circleci-org-migration@0.2.0

workflows:
  capture-secrets:
    jobs:
      - migrate/extract_context:
          name: extract-<< matrix.context_name >>
          context:
            - << matrix.context_name >>
          matrix:
            alias: extract_contexts
            parameters:
              context_name:
                - deploy-prod
                - shared
                - build
                - staging
      - migrate/merge:
          name: merge-secrets
          requires:
            - extract_contexts
```

### Protecting `secrets.json`

- The file is written with `0600` permissions.
- Do **not** commit it to version control.
- Use a **private** CircleCI project for the capture pipeline.
- Set a **short artifact retention period** in the project settings.
- Delete the artifact and your local copy once the sync is complete.
- Rotate every captured secret value after the destination is confirmed healthy.

---

## Example 6 — Repo-move / EMU (repos moved to a new GitHub org)

**Scenario:** your GitHub repositories have been moved from `github.com/acme`
to `github.com/acme-cloud` (for example via a GitHub Enterprise Managed Users
migration). CircleCI's GitHub App integration records each repository by its
numeric GitHub ID (`external_id`). When repos move between GitHub orgs, those
IDs change; `--github-token` and `--dest-github-org` tell the tool to look up
the new IDs via the GitHub API.

```bash
export GITHUB_TOKEN="your-github-pat-with-repo-read"

# All-in-one
circleci-migrate migrate \
  --source-org "circleci/$SRC_UUID" \
  --dest-org   "circleci/$DST_UUID" \
  --secrets secrets.json \
  --dest-github-org acme-cloud \
  --apply --yes
# --github-token falls back to $GITHUB_TOKEN
```

Or using separate `export` / `sync` steps:

```bash
# Export (no GitHub token needed for export)
circleci-migrate export \
  --org "circleci/$SRC_UUID" \
  --output manifest.json

# Sync — supply the GitHub token so the tool can resolve new repo IDs
circleci-migrate sync \
  --manifest manifest.json \
  --secrets secrets.json \
  --dest-github-org acme-cloud \
  --apply --yes
```

When only some repositories moved (partial EMU), use a
[mapping file](../README.md#sync) to specify per-project overrides instead of
`--dest-github-org`.

---

## Example 7 — Runner resource-class capture and recreation

> **Note:** runner resource-class migration is being added in a parallel
> release. The flags below will be available when that release ships.

**Scenario:** your source org has self-hosted runner resource classes that you
want to recreate in the destination org. Because there is no org-to-namespace
lookup, you must supply the runner namespace name explicitly.

**Export with runner resource classes:**

```bash
circleci-migrate export \
  --org gh/acme \
  --runner-namespace acme-runners \
  --output manifest.json \
  --report migration-report.md
```

**Sync with runner resource class recreation:**

```bash
circleci-migrate sync \
  --manifest manifest.json \
  --secrets secrets.json \
  --dest-runner-namespace acme-cloud-runners \
  --apply --yes
```

**All-in-one with runner namespace flags:**

```bash
circleci-migrate migrate \
  --source-org gh/acme \
  --dest-org gh/acme-cloud \
  --runner-namespace acme-runners \
  --dest-runner-namespace acme-cloud-runners \
  --secrets secrets.json \
  --apply --yes
```

The runner namespace must already exist in the destination org. The tool
captures resource class names and tokens from the source namespace and
recreates the resource classes in the destination namespace. Runner tokens are
treated as secrets — supply a secret bundle or use `--missing-secrets
placeholder` to create placeholder tokens.

---

## Cutover checklist

After every migration, work through the items in the
[cutover runbook](cutover-runbook.md):

1. Validate contexts and env-var names match the audit report.
2. Verify project settings, webhooks, and schedules.
3. Enable builds when ready (triggers / follow-projects).
4. Set webhook HMAC secrets manually (not exported — regenerate and update
   receivers).
5. Recreate SSO, audit-log streaming, and OTel exporter headers manually.
6. Rotate every captured secret value.
7. Update external pins: Backstage entries, Slack integrations, status badges,
   branch-protection checks.

---

## Troubleshooting tips

**Dry run shows everything as `manual`:** you ran without a secrets bundle.
That is expected for a first dry run — the plan still shows contexts and
projects as `created (would create)`. Run `secrets capture` or use the orb to
produce `secrets.json`.

**`--apply` fails on project creation (App org):** repos must be connected to
the destination CircleCI GitHub App before `sync --apply`. Check the GitHub App
installation in the destination org's settings.

**Pipeline definitions show wrong `external_id`:** if repos have moved GitHub
orgs, add `--github-token` and `--dest-github-org` (see Example 6).

**Context restriction shows `manual`:** project-type restrictions cannot be
migrated automatically (source-org project UUIDs do not transfer). Recreate
them in the destination org settings UI after sync.

**Debug mode:** add `--debug` to any command for verbose HTTP request/response
logging:

```bash
circleci-migrate sync --manifest manifest.json --debug
```
