# Migration guide

This is the single end-to-end walkthrough for migrating one CircleCI
organization to another with `circleci-migrate`. It covers the org types you
can migrate, the prerequisites and token permissions you need, and the core
flow: **export → secrets capture → sync**. Per-org-type variations are called
out as sections within each step.

If you just want the operator checklist for a production cutover, use the
[cutover runbook](cutover-runbook.md). For full per-command flag tables, see the
[generated CLI reference](cli/README.md). For problems, see
[troubleshooting](troubleshooting.md).

> Throughout, examples use the fictional orgs `gh/acme` (source) and
> `gh/acme-new` (destination). Substitute your own slugs.

---

## 1. The model

`circleci-migrate` works in two halves:

1. **`export`** reads everything it can from the source org and writes a
   non-secret `manifest.json` plus a human-readable `migration-report.md`. The
   manifest *is* the exported source data: structure and names only, never
   secret values. It is safe to review, diff, and store.
2. **`sync`** replays a manifest into the destination org. It is a **dry run by
   default**; add `--apply` to write.

Because the CircleCI API never returns secret values, a separate **`secrets
capture`** step runs a short-lived pipeline inside the source org to collect
them — encrypted by default, never stored in plain text.

`migrate` is the all-in-one command that runs export → sync in one step (with an
interactive walkthrough when run with no flags).

For the authoritative list of **what does NOT transfer** and requires manual
follow-up, see the cutover runbook:
[Manual steps required](cutover-runbook.md#3-manual-steps-required) and
[Does not transfer / data loss](cutover-runbook.md#4-does-not-transfer--data-loss).

---

## 2. Org types

The **org slug format** controls which APIs the tool uses and which features
apply. Find your slug in the org's CircleCI URL.

| Org type | Slug format | Example | Notes |
|---|---|---|---|
| **GitHub OAuth** | `gh/<org>` | `gh/acme` | Legacy integration. Full v1.1 + v2 API coverage. Projects are *followed* to install webhooks. OAuth-only build flags (`oss`, `build_fork_prs`, `forks_receive_secret_env_vars`, `pr_only_branch_overrides`) apply. |
| **GitHub App** | `circleci/<org-id>` | `circleci/22222222-2222-2222-2222-222222222222` | v2 API only. Projects use pipeline definitions + triggers (created **disabled**). Repos identified by numeric GitHub `external_id`. |
| **CircleCI standalone** | `circleci/<org-id>` | `circleci/<uuid>` | Standalone (non-VCS-OAuth) orgs. Supports **CIAM** roles and groups — synced unless you pass `--skip-ciam`. |
| **GitLab (App)** | `circleci/<org-id>` | `circleci/<uuid>` | Uses the `circleci/<org-id>` slug like GitHub App. |

**Same-type migrations** (OAuth→OAuth, App→App, standalone→standalone) are fully
automated. Cross-type (OAuth→App) and repo-move scenarios are covered in
[§7 Scenarios](#7-scenarios-by-org-type).

> **Mixed orgs:** when one GitHub org has *both* the OAuth and the GitHub App
> integration, CircleCI registers them as **two separate org records** (one
> `gh/<org>`, one `circleci/<uuid>`). Migrate each leg separately — see
> [§7](#7-scenarios-by-org-type).

---

## 3. Prerequisites & token permissions

### CircleCI API tokens

You need a **personal API token** for each side:

| Token | Env var | Used for |
|---|---|---|
| Source token | `CIRCLECI_SOURCE_TOKEN` | Reading the source org (export, capture) |
| Destination token | `CIRCLECI_DEST_TOKEN` | Writing the destination org (sync) |
| Fallback token | `CIRCLECI_CLI_TOKEN` / `CIRCLE_TOKEN` | Used for both when the specific tokens are unset |

Create tokens at **User Settings → Personal API Tokens**. The token's user must
be an **organization admin** of the org it acts on:

- **Source:** admin/read access to contexts, projects, and org settings; ability
  to trigger pipelines (for `secrets capture`).
- **Destination:** admin to create contexts, projects, pipeline definitions,
  triggers, and to write org settings.

Set them as environment variables so you never pass tokens on the command line:

```bash
export CIRCLECI_SOURCE_TOKEN="<source-org-admin-token>"
export CIRCLECI_DEST_TOKEN="<destination-org-admin-token>"
```

### GitHub token (only for repo moves)

If repos have moved to a **different GitHub org** (e.g. an EMU migration), CircleCI's
GitHub App identifies each repo by its numeric GitHub ID, which changes when the
repo moves. Supply a GitHub PAT with **repo read** so the tool can resolve the
new IDs:

```bash
export GITHUB_TOKEN="<github-pat-with-repo-read>"
```

This is **not** needed for same-GitHub-org migrations.

### CircleCI Server / self-hosted (`--host`)

For CircleCI Server (or any non-cloud host), point every command at your install
with `--host` (or `CIRCLECI_CLI_HOST` / `CIRCLECI_HOST` / `CIRCLE_URL`):

```bash
circleci-migrate export --source-org gh/acme --host https://circleci.example.com
```

The default is `https://circleci.com`.

### Using the official `circleci` CLI

`circleci-migrate` is also a plugin to the official
[circleci CLI](https://circleci.com/docs/local-cli/). When invoked as
`circleci run migrate <args>`, the `circleci` CLI execs `circleci-migrate` on
your `PATH` and injects `CIRCLE_TOKEN` and `CIRCLE_URL` — no extra token/host
flags needed:

```bash
circleci run migrate export --source-org gh/acme
circleci run migrate sync   --manifest manifest.json --apply
```

Bare `circleci migrate` (without `run`) is **not** supported.

---

## 4. Step 1 — Export the source org

`export` is read-only and safe to re-run.

```bash
circleci-migrate export --source-org gh/acme
# Produces: manifest.json  migration-report.md
```

Common options:

```bash
circleci-migrate export \
  --source-org gh/acme \
  --output manifest.json \
  --report migration-report.md
```

Then **review `migration-report.md`** — it lists everything captured and the
manual follow-ups that apply to *your* org.

### Scoping what is exported

- `--project gh/acme/web --project gh/acme/api` — export only specific projects
  (repeat the flag). Default is all discovered projects.
- The `--skip-*` family limits what is read:
  - `--skip-contexts` — skip contexts.
  - `--skip-projects` — skip projects.
  - `--skip-extras` — skip checkout keys, webhooks, and schedules.

### Self-hosted runner resource classes

There is no clean org→namespace lookup, so you must name the runner namespace
explicitly:

```bash
circleci-migrate export --source-org gh/acme --runner-namespace acme-runners
```

### Usage data snapshot (opt-in)

`--include-usage` also downloads a historical usage report (gzip CSV) from the
CircleCI Usage API into a `usage/` directory next to the manifest. **This is a
local baseline/record only — it does NOT transfer to the destination.**

```bash
circleci-migrate export --source-org gh/acme --include-usage \
  --usage-start 2026-01-01T00:00:00Z --usage-end 2026-01-31T23:59:59Z
```

The default window is the last 30 days; the max window is 31 days (API limit).
If the usage export fails, the main export still succeeds with a warning.

### Machine-readable output

Add `--json` to print a JSON summary to stdout instead of the human-readable
summary (the manifest and report files are still written). Useful in CI.

---

## 5. Step 2 — Capture secret values

Env-var and context **values** are masked by the API. `secrets capture` runs a
short-lived pipeline inside the **source** org that dumps the values to an
artifact, downloads it, and writes a local `secrets.json`. It commits **no**
config to your repo (it submits an inline/unversioned config).

### Interactive (recommended for first-time use)

Run on a TTY with no flags to launch the guided walkthrough:

```bash
circleci-migrate secrets capture
```

It prompts for the manifest, which contexts/projects to capture, the host
project for context extraction, encryption, storage, and artifact retention.

### Non-interactive (CI-safe)

Once `--manifest` is supplied (or stdin is not a TTY), capture runs
non-interactively. **Fail-closed guard:** if neither `--context` nor `--project`
is set and you have not passed `--yes` (or `--no-input`), an unattended
capture-all errors out instead of sweeping every context/project. Scope it, or
acknowledge with `--yes`:

```bash
# Encrypted with an auto-generated key + 1-day retention (recommended)
circleci-migrate secrets capture \
  --manifest manifest.json \
  --encrypt --generate-key \
  --artifact-retention-days 1 \
  --enable-trigger \
  --output secrets.json

# Scope to specific contexts / projects
circleci-migrate secrets capture --manifest manifest.json \
  --context deploy-prod --host-project gh/acme/web --enable-trigger
```

### Encryption (on by default)

Encryption is **on by default** so plaintext secrets never persist in CircleCI
artifact storage. Supply a recipient with `--generate-key` (creates a fresh age
keypair) or `--ssh-public-key`/`--ssh-private-key` (use an existing SSH key).
Use `--no-encrypt` to opt out (a plaintext artifact — strongly discouraged).

```bash
# Existing SSH key
circleci-migrate secrets capture --manifest manifest.json --encrypt \
  --ssh-public-key ~/.ssh/id_ed25519.pub --ssh-private-key ~/.ssh/id_ed25519 \
  --artifact-retention-days 1 --enable-trigger --output secrets.json
```

### SSH keys (on by default)

`secrets capture` also extracts **additional project SSH private keys** that are
cataloged in the manifest, via a separate in-pipeline job that uses
`add_ssh_keys` with the explicit fingerprints (the checkout/deploy key is never
materialised). This is **on by default**; pass `--no-ssh-keys` to skip it (for
example, an env-var-only capture).

### Storage (`--storage`)

- `artifact` (default) — store the bundle as a CircleCI job artifact.
- `s3` — upload to S3 only (requires the `aws` CLI + AWS creds in the job;
  provide `--s3-bucket` and optionally `--s3-prefix`).
- `both` — store in both.

```bash
circleci-migrate secrets capture --manifest manifest.json --generate-key \
  --storage s3 --s3-bucket my-migration-bucket --s3-prefix migration/
```

### Restricted contexts

If a context has restrictions that block the inline pipeline:

- `--skip-restricted-contexts` (default: true) — skip them and attach a warning.
- `--remove-restrictions` — temporarily lift real restrictions and restore them
  after the run (explicit opt-in).

For uncaptured values, `sync --missing-secrets placeholder` still creates the
variable name so it can be filled in manually later.

### Orb-based alternative (committed config)

For large numbers of contexts or full pipeline control, commit `manifest.json`
to a repo in your source org and use the `awesomecicd/circleci-org-migration`
orb. Each job must reference **exactly one context** (mixing contexts lets
same-named variables overwrite each other):

```yaml
# .circleci/config.yml in your SOURCE org
version: "2.1"
orbs:
  migrate: awesomecicd/circleci-org-migration@0.8.0
workflows:
  capture-secrets:
    jobs:
      - migrate/extract_context:
          name: extract-deploy-prod
          context_name: deploy-prod
          context: [deploy-prod]
      - migrate/merge:
          name: merge-secrets
          requires: [extract-deploy-prod]
```

For many contexts, use a matrix with an explicit `alias` so `merge` can depend
on the whole matrix:

```yaml
version: "2.1"
orbs:
  migrate: awesomecicd/circleci-org-migration@0.8.0
workflows:
  capture-secrets:
    jobs:
      - migrate/extract_context:
          name: extract-<< matrix.context_name >>
          context: [<< matrix.context_name >>]
          matrix:
            alias: extract_contexts
            parameters:
              context_name: [deploy-prod, shared, build, staging]
      - migrate/merge:
          name: merge-secrets
          requires: [extract_contexts]
```

Download `secrets.json` from the `merge` job's **Artifacts** tab. If the bundle
is age-encrypted, decrypt it locally with `secrets decrypt`.

### Protecting `secrets.json`

`secrets.json` contains plaintext values — treat it like a password file.

- Encryption is on by default; keep it on for production secrets.
- `--artifact-retention-days 1` minimises the in-CircleCI exposure window.
- The local file is written with `0600` permissions. Do **not** commit it.
- Use a **private** project for the capture pipeline.
- **Rotate every captured value** after the destination is confirmed healthy.

---

## 6. Step 3 — Sync into the destination

`sync` is a **dry run by default**.

### Destination resolution

The destination org **defaults to the source org from the manifest**. To target
a *different* org you MUST pass `--mapping` with `org.to` set — otherwise sync
runs against your own source org (a prominent warning is printed). See
[mapping.md](mapping.md) for the full schema.

```bash
# Dry run — nothing is written; review the plan
circleci-migrate sync --manifest manifest.json --secrets secrets.json \
  --mapping mapping.json

# Apply when satisfied
circleci-migrate sync --manifest manifest.json --secrets secrets.json \
  --mapping mapping.json --apply
```

The dry-run plan shows each action as `created (would create)`, `set (would
set)`, or `manual`.

### Secrets

Env-var values come from `--secrets` (default `secrets.json`, skipped if
absent). With `--apply` but **no** bundle, resources are created with **empty**
env-var values you must fill in manually. `--missing-secrets`:

- `skip` (default) — omit variables with no captured value.
- `placeholder` — create the variable with a placeholder value (useful for
  restricted contexts) so the name exists for manual fill-in.

### Enabling builds

When `--apply` creates projects, they are created **paused** (no webhook, no
builds). You are then prompted to enable builds; `--yes` / `-y` auto-confirms
(only meaningful with `--apply`; no effect in a dry run). Without a TTY, builds
are not enabled unless `--yes` is passed.

### Project API tokens

`--create-project-tokens` (with `--apply`) recreates each captured project API
token on the destination. **Caution:** each recreated token mints a **new**
one-time secret printed once to stderr — every consumer of the old token must be
repointed. Default is off (the report emits manual steps only).

### The `--skip-*` family

| Flag | Skips |
|---|---|
| `--skip-org-settings` | Org-level settings (feature flags, OIDC, URL-orb allow list, config policies, etc.) |
| `--skip-contexts` | Contexts |
| `--skip-projects` | Projects |
| `--skip-extras` | Checkout keys, additional SSH keys, webhooks, schedules |
| `--skip-runner` | Self-hosted runner resource classes |
| `--skip-ciam` | CIAM roles and groups (standalone `circleci`-type orgs only) |

### Runner resource classes

Supply `--dest-runner-namespace` to recreate runner classes in the destination
(the namespace must already exist; the syncer never guesses it). When omitted,
runner classes are flagged for manual recreation.

```bash
circleci-migrate sync --manifest manifest.json --secrets secrets.json \
  --mapping mapping.json --dest-runner-namespace acme-new-runners --apply --yes
```

### Machine-readable output

`--json` prints a JSON summary to stdout instead of the per-section reports;
progress goes to stderr.

---

## 7. Scenarios by org type

All scenarios share the export → capture → sync flow above. The differences are
the slugs, whether you need a mapping file, and a few flags.

### 7a. OAuth → OAuth

Both orgs use the GitHub OAuth integration. If the org name changes, supply a
[mapping](mapping.md) with `org.to`:

```bash
circleci-migrate export --source-org gh/acme -o manifest.json
circleci-migrate secrets capture --manifest manifest.json \
  --encrypt --generate-key --enable-trigger -o secrets.json
circleci-migrate sync --manifest manifest.json --secrets secrets.json \
  --mapping mapping.json --apply
```

Or the all-in-one:

```bash
circleci-migrate migrate \
  --source-org gh/acme --dest-org gh/acme-new \
  --secrets secrets.json --apply --yes
```

### 7b. GitHub App → GitHub App

App orgs use UUID slugs (`circleci/<uuid>`). Find them at
`https://app.circleci.com/settings/organization/circleci/<uuid>/overview`.

```bash
SRC_UUID="11111111-1111-1111-1111-111111111111"
DST_UUID="22222222-2222-2222-2222-222222222222"

circleci-migrate export --source-org "circleci/$SRC_UUID" -o manifest.json
# Capture via the orb (large orgs) or `secrets capture`
circleci-migrate sync --manifest manifest.json --secrets secrets.json \
  --mapping mapping.json --apply --yes
```

App projects are created with triggers **disabled**; `--yes` enables them after
creation. Omit `--yes` (answer N at the prompt) to keep them paused until you're
ready, then re-run `--apply --yes`. Repos must already be connected to the
destination GitHub App.

### 7c. CircleCI standalone → standalone

Standalone `circleci`-type orgs additionally carry **CIAM roles and groups**.
These sync by default; pass `--skip-ciam` to leave them alone. (CIAM
provisioning is reported as a manual follow-up where the API cannot fully
automate it — check the report.)

```bash
circleci-migrate sync --manifest manifest.json --secrets secrets.json \
  --mapping mapping.json --apply        # CIAM included
circleci-migrate sync --manifest manifest.json --secrets secrets.json \
  --mapping mapping.json --skip-ciam --apply   # CIAM left untouched
```

### 7d. Mixed org (OAuth + App) — two legs

Run the whole flow twice, once per org record:

```bash
# Leg 1 — OAuth record
circleci-migrate export --source-org gh/acme -o manifest-oauth.json --report report-oauth.md
circleci-migrate secrets capture --manifest manifest-oauth.json -o secrets-oauth.json
circleci-migrate sync --manifest manifest-oauth.json --secrets secrets-oauth.json \
  --mapping mapping-oauth.json --apply --yes

# Leg 2 — App record (capture via orb; download secrets-app.json)
circleci-migrate export --source-org "circleci/$SRC_UUID" -o manifest-app.json --report report-app.md
circleci-migrate sync --manifest manifest-app.json --secrets secrets-app.json \
  --mapping mapping-app.json --apply --yes
```

Contexts and org settings may overlap between the two records — review both
reports, and consider `--skip-org-settings` on the second leg to avoid
double-applying org flags.

### 7e. Cross-type: OAuth → GitHub App

A follow-on migration, typically after an OAuth org has moved. **Data-loss
caveats** (recorded as `manual` in the report):

- `build_fork_prs` — the GitHub App never builds fork PRs; cannot be replicated.
- The OSS flag and `pr_only_branch_overrides` have no App equivalent.
- Multiple App pipeline definitions can't be collapsed from one OAuth config —
  the tool creates one pipeline definition per project using the default config
  path (`.circleci/config.yml`).

Because the slug type changes, you must supply a [mapping](mapping.md) that maps
project slugs:

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
circleci-migrate sync --manifest manifest.json --secrets secrets.json \
  --mapping mapping.json --apply --yes
```

### 7f. Repo-move / EMU (repos moved to a new GitHub org)

When repos move between GitHub orgs, their numeric `external_id` changes. Supply
`--github-token` + `--dest-github-org` so the tool resolves the new IDs (found →
onboard, missing → flagged manual + skipped):

```bash
export GITHUB_TOKEN="<github-pat-with-repo-read>"

circleci-migrate migrate \
  --source-org "circleci/$SRC_UUID" --dest-org "circleci/$DST_UUID" \
  --secrets secrets.json --dest-github-org acme-new --apply --yes
# --github-token falls back to $GITHUB_TOKEN
```

For a **partial** move (only some repos changed org), use the `github_org` key
or per-project `projects` entries in the [mapping file](mapping.md) instead of
`--dest-github-org`.

### 7g. Runner resource classes

```bash
circleci-migrate export --source-org gh/acme --runner-namespace acme-runners -o manifest.json
circleci-migrate sync --manifest manifest.json --secrets secrets.json \
  --mapping mapping.json --dest-runner-namespace acme-new-runners --apply --yes
```

The destination namespace must already exist. Resource-class tokens are treated
as secrets — supply a bundle or use `--missing-secrets placeholder`.

---

## 8. Step 4 — Validate, enable, rotate

After sync completes, follow the [cutover runbook](cutover-runbook.md):

1. Validate contexts and env-var names against the audit report.
2. Verify project settings, webhooks, and schedules.
3. Enable builds when ready (the sync prompt or `--yes`).
4. Recreate items that don't transfer — see
   [Manual steps required](cutover-runbook.md#3-manual-steps-required).
5. Update external pins (Backstage, Slack, status badges, branch-protection).
6. **Rotate every captured secret** and delete `secrets.json`.

---

## See also

- [Cutover runbook](cutover-runbook.md) — operator checklist + the full
  what-does-NOT-transfer list.
- [mapping.json reference](mapping.md) — when you need a mapping file and what
  each key does.
- [Troubleshooting](troubleshooting.md) — common errors and fixes.
- [CLI reference](cli/README.md) — complete per-command flag tables.
- [Architecture](architecture.md) — how the tool reads and writes data.
