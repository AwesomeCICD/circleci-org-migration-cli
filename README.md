[![CircleCI](https://dl.circleci.com/status-badge/img/gh/AwesomeCICD/circleci-org-migration-cli/tree/main.svg?style=svg)](https://dl.circleci.com/status-badge/redirect/gh/AwesomeCICD/circleci-org-migration-cli/tree/main) ![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white) [![Conventional Commits](https://img.shields.io/badge/Conventional%20Commits-1.0.0-yellow.svg)](https://conventionalcommits.org)

# circleci-migrate

`circleci-migrate` moves configuration data from one CircleCI organization to another — contexts, project settings, environment variables, org-level settings, and more — with a safe, auditable approach that never requires you to expose secrets in plain text until they are needed.

---

## What it does

CircleCI's API masks every secret value it stores. You can read the *names* of environment variables but never their values. `circleci-migrate` handles this constraint with three phases:

```
Phase 1 — export          Read-only. Exports source org to manifest.json
                          (structure + names, no secret values) and
                          migration-report.md (human-readable audit).

Phase 2 — secrets         Run inside a CircleCI pipeline in the source org.
          capture         The orb injects real secret values into the job
                          environment; secrets extract captures them into
                          a bundle. secrets merge combines per-context
                          bundles into a single secrets.json artifact.

Phase 3 — sync            Read manifest.json + secrets.json and recreate
                          everything in the destination org.
                          Dry-run by default; pass --apply to write.
```

Phases 1 and 3 run on your local machine (or any CI job). Phase 2 **must** run inside a CircleCI pipeline so the platform injects real secret values.

The `migrate` command wraps all three phases into one command for the common case where you do not need to inspect or edit the manifest in between.

---

## Quick start — all-in-one `migrate`

The fastest path when migrating from one org directly to another:

```bash
# Dry run first — review the plan, nothing is written
circleci-migrate migrate \
  --source-org gh/acme \
  --dest-org gh/acme-new \
  --source-token "$SRC_TOKEN" \
  --dest-token "$DST_TOKEN"

# Apply when you are satisfied with the plan
circleci-migrate migrate \
  --source-org gh/acme \
  --dest-org gh/acme-new \
  --source-token "$SRC_TOKEN" \
  --dest-token "$DST_TOKEN" \
  --apply
```

`migrate` runs `export` followed by `sync` in one step, keeping the manifest in memory. To also save the manifest and audit report to disk:

```bash
circleci-migrate migrate \
  --source-org gh/acme \
  --dest-org gh/acme-new \
  --apply \
  -o manifest.json \
  --report migration-report.md
```

For more control — for example to review or edit the manifest between phases — use `export` and `sync` as separate commands (see below).

---

## Install

### Prebuilt binaries (recommended)

Download the latest release from [GitHub Releases](https://github.com/CircleCI-Public/circleci-org-migration-cli/releases):

```bash
# Example for Linux amd64 — replace version, os, and arch as needed
VERSION=v1.0.0
curl -sfL "https://github.com/CircleCI-Public/circleci-org-migration-cli/releases/download/${VERSION}/circleci-migrate_${VERSION#v}_linux_amd64.tar.gz" \
  | tar -xz
sudo install -m 0755 circleci-migrate /usr/local/bin/
```

Archive naming: `circleci-migrate_{version}_{os}_{arch}.tar.gz`
Supported OS/arch combinations: `linux_amd64`, `linux_arm64`, `darwin_amd64`, `darwin_arm64`.

### Build from source

```bash
git clone https://github.com/CircleCI-Public/circleci-org-migration-cli.git
cd circleci-org-migration-cli
make build            # produces ./bin/circleci-migrate
# or without make:
go build -o bin/circleci-migrate .
```

**Requirements:** Go 1.26 or later.

---

## Three-phase walkthrough

### Phase 1 — Export the source org

```bash
circleci-migrate export \
  --org gh/acme \
  --source-token "$SRC_TOKEN"
# Produces: manifest.json  migration-report.md
```

Review `migration-report.md`. Items requiring manual follow-up (group restrictions, project restrictions, SSO, audit-log streaming) are listed under "Warnings & manual follow-ups".

The `--org` slug format:
- `gh/<org>` for GitHub OAuth organizations
- `circleci/<org-id>` for GitHub App or GitLab organizations

### Phase 2 — Capture secrets inside a pipeline

Because the API never returns secret values, capturing them requires running inside a CircleCI job. Commit `manifest.json` to your source org's repository (it contains no secrets), then add a workflow using the `awesomecicd/circleci-org-migration` orb:

```yaml
# .circleci/config.yml in your SOURCE org
version: "2.1"
orbs:
  migrate: awesomecicd/circleci-org-migration@1.0.0

workflows:
  capture-secrets:
    jobs:
      # One job per context. Each job must reference exactly that context
      # so its variables are injected — do not mix contexts in one job.
      - migrate/extract-context:
          name: extract-deploy-prod
          context-name: deploy-prod
          context:
            - deploy-prod
      - migrate/extract-context:
          name: extract-shared
          context-name: shared
          context:
            - shared
      # Merge all per-context bundles into a single secrets.json artifact.
      - migrate/merge:
          requires:
            - extract-deploy-prod
            - extract-shared
```

Download `secrets.json` from the `merge` job's artifacts. This file contains plaintext values — see [Security](#security) below.

The orb's `install` command fetches the prebuilt binary from GitHub Releases automatically. For large numbers of contexts, use a matrix to fan out a single job stanza instead of writing one stanza per context (see the orb's `capture-context-secrets-matrix` example).

### Phase 3 — Sync to the destination org

```bash
# Dry run first — review the plan, nothing is written
circleci-migrate sync \
  --manifest manifest.json \
  --secrets secrets.json \
  --dest-token "$DST_TOKEN"

# Apply when you are satisfied with the plan
circleci-migrate sync \
  --manifest manifest.json \
  --secrets secrets.json \
  --dest-token "$DST_TOKEN" \
  --apply
```

Sync is **idempotent**: existing contexts are reused by name; re-running is safe.

---

## Project creation and enabling builds

When `sync` (or `migrate`) creates projects in the destination org with `--apply`, they are created in a **paused** state — no webhook is installed and no builds fire until you explicitly enable them. This is intentional: it gives you time to review the new org before any pipeline runs.

**OAuth orgs:** a project is created as a shell; to enable builds the project must be "followed", which installs a deploy key and webhook.

**GitHub App orgs:** a project is created along with its pipeline definitions and triggers. Triggers are created **disabled (paused)** and must be explicitly enabled.

After `--apply` completes, you are prompted to enable builds:

```
Enable builds for 3 project(s) now? [y/N]:
```

To skip the prompt and enable automatically:

```bash
circleci-migrate sync --manifest manifest.json --apply --yes
# or with migrate:
circleci-migrate migrate --source-org gh/acme --dest-org gh/acme-new --apply --yes
```

To skip for now and enable later, just press Enter (or run without a TTY). You can re-run with `--apply --yes` at any time — it is safe to call again.

> **Note on GitHub App webhook/schedule triggers:** triggers of type `webhook` or `schedule` require manual recreation because the webhook HMAC secret cannot be migrated and schedule-trigger creation via the Trigger API is a planned future addition. The sync report will list these as `manual` actions.

---

## What gets migrated

| Resource | Captured | Synced | Notes |
|---|---|---|---|
| Context names | Yes | Yes | Created by name; destination assigns its own ID |
| Context env-var names | Yes | Yes | Names captured via API |
| Context env-var values | In-pipeline only | Yes (with secret bundle) | API never returns values; must use orb |
| Expression restrictions | Yes | Yes | Recreated on sync |
| Group restrictions | Yes (name recorded) | Yes (by name lookup) | Resolved to destination group UUID by name; "All members" maps to org ID |
| Project restrictions | Yes (name recorded) | Manual | Source-org project IDs do not transfer; recreate manually |
| Project creation | Yes (metadata) | Yes | Created paused; enable-builds step required (see above) |
| Project advanced settings | Yes | Yes | `autocancel_builds`, `build_fork_prs`, etc. |
| Project env-var names | Yes | Yes | Names captured via API |
| Project env-var values | In-pipeline only | Yes (with secret bundle) | Same constraint as context values |
| Pipeline definitions (App) | Yes | Yes | Created on new App projects; external_id reused or resolved via GitHub API |
| Pipeline triggers (App) | Yes | Yes (disabled) | Created disabled; enabled in enable-builds step. Webhook/schedule triggers: manual |
| Org feature flags | Yes | Yes | Full flag map via v1.1 API; safe/relevant flags written back |
| OIDC custom claims | Yes | Yes | Audience list and TTL via v2 API; captured at both org and project level |
| URL-orb allow list | Yes | Yes | GitHub App / circleci-type orgs only |
| Config policies (Rego) | Yes | Yes | Scale plan only; enforcement toggle included |
| OTel exporters | Yes | Yes (partial) | Exporter configs recreated; header values are redacted by the API and must be set manually |
| Org contacts | Yes | Yes | Primary and security contact email lists |
| Audit-log streaming configs | Yes (captured) | Manual | AWS ARN/bucket is source-specific; recreate manually |
| SSO (SAML) configuration | Yes (captured) | Manual | Requires DNS verification + IdP setup; never auto-written |
| Checkout key fingerprints | Yes (public metadata only) | Not yet | Private keys cannot be exported; regenerate on destination |
| Webhooks (OAuth projects) | Yes (metadata) | Yes | HMAC signing-secret must be set manually |
| Scheduled pipelines (OAuth) | Yes (metadata) | Yes | Recreated on OAuth destinations only |
| Webhooks (App projects) | Yes (metadata) | Yes | HMAC signing-secret must be set manually |
| Scheduled pipelines (App) | Yes (metadata) | Manual | App-org schedules require the Trigger API (planned) |
| Additional SSH keys | No | No | Not available via API |

---

## GitHub OAuth vs GitHub App

The org slug format affects which APIs are available and how projects are managed:

- **GitHub OAuth** (`gh/<org>`): full v1.1 and v2 API coverage, including project discovery via followed projects. Projects are followed to install webhooks.
- **GitHub App** (`circleci/<org-id>`): v2 API only. Project slugs use UUIDs; projects use pipeline definitions and triggers instead of webhooks. Project discovery uses the private `/api/private/project` endpoint.

### Same-type migrations (recommended first step)

The tool is designed primarily for **same-type** migrations:

- **OAuth → OAuth** (`gh/acme` → `gh/acme-new`): fully automated with a name mapping.
- **App → App** (`circleci/<src-uuid>` → `circleci/<dst-uuid>`): fully automated; the `--github-token` flag helps resolve repository external IDs when the destination is in a different GitHub org.

### Cross-type migrations

A **GitHub App** org that also has GitHub-connected repositories registers as two separate CircleCI organization records (one OAuth record, one App record). Migrating such a setup between two complete environments typically requires two separate runs — one for the OAuth side and one for the App side.

**OAuth → App** (pure cross-type) is a documented future direction. Key data-loss caveats to be aware of:

- GitHub App never builds fork PRs; if your source org has `build_fork_prs` enabled the setting cannot be replicated.
- Multiple pipeline definitions per App project cannot collapse to a single OAuth project config.

---

## Global flags

These flags are available on every sub-command. Environment variables are read before flag parsing, so they act as defaults that CLI flags can override.

| Flag | Environment variable | Default | Description |
|---|---|---|---|
| `--host` | `CIRCLECI_HOST` | `https://circleci.com` | CircleCI host URL (useful for Server installs) |
| `--token` | `CIRCLECI_CLI_TOKEN` | | Personal API token — fallback for both orgs |
| `--source-token` | `CIRCLECI_SOURCE_TOKEN` | | API token for the source org (read operations) |
| `--dest-token` | `CIRCLECI_DEST_TOKEN` | | API token for the destination org (write operations) |
| `--debug` | | `false` | Enable debug logging |

---

## Command reference

### `migrate`

All-in-one: exports the source org and syncs it into the destination in a single command. The manifest is kept in memory; use `-o` to save it to disk.

```bash
# Dry run
circleci-migrate migrate \
  --source-org gh/acme --dest-org gh/acme-new

# Apply with secret bundle
circleci-migrate migrate \
  --source-org gh/acme --dest-org gh/acme-new \
  --secrets secrets.json --apply

# Apply and auto-confirm enabling builds, save manifest + report
circleci-migrate migrate \
  --source-org gh/acme --dest-org gh/acme-new \
  --apply --yes \
  -o manifest.json --report migration-report.md
```

`migrate` uses the source token (`--source-token` / `CIRCLECI_SOURCE_TOKEN`) for the export step and the dest token (`--dest-token` / `CIRCLECI_DEST_TOKEN`) for the sync step.

| Flag | Default | Description |
|---|---|---|
| `--source-org` | *(required)* | Source organization slug (`gh/<org>` or `circleci/<org-id>`) |
| `--dest-org` | *(required)* | Destination organization slug |
| `--secrets` | `secrets.json` | Path to a captured secret bundle (optional; file is silently skipped if absent) |
| `--mapping` | | Path to a source→destination mapping file (optional) |
| `--apply` | `false` | Write changes to destination (default: dry run) |
| `--yes`, `-y` | `false` | Auto-confirm enabling builds after project creation (skip the interactive prompt) |
| `--missing-secrets` | `skip` | How to handle variables with no captured value: `skip` or `placeholder` |
| `--github-token` | `$GITHUB_TOKEN` | GitHub PAT used to resolve repository IDs for App pipeline definitions |
| `--skip-contexts` | `false` | Skip exporting and syncing contexts |
| `--skip-projects` | `false` | Skip exporting and syncing projects |
| `--skip-org-settings` | `false` | Skip syncing org-level settings |
| `--skip-extras` | `false` | Skip checkout keys, webhooks, and schedules |
| `--output`, `-o` | | If set, save the exported manifest to this path |
| `--report` | | If set, save the human-readable audit report to this path |

### `export`

Reads the source org and produces `manifest.json` and `migration-report.md`. Read-only — never writes to CircleCI.

```bash
circleci-migrate export --org gh/acme --source-token "$SRC_TOKEN"
circleci-migrate export --org gh/acme -o acme.json --report acme-audit.md
circleci-migrate export --org gh/acme --projects gh/acme/web,gh/acme/api
```

| Flag | Default | Description |
|---|---|---|
| `--org` | *(required)* | Source organization slug (`gh/<org>` or `circleci/<org-id>`) |
| `--output`, `-o` | `manifest.json` | Path to write the JSON manifest |
| `--report` | `migration-report.md` | Path to write the audit report |
| `--projects` | *(all followed)* | Explicit project slugs to export, comma-separated |
| `--skip-contexts` | `false` | Skip exporting contexts |
| `--skip-projects` | `false` | Skip exporting projects |
| `--skip-extras` | `false` | Skip checkout keys, webhooks, and schedules |

> **Project discovery:** `export` discovers projects through the followed-projects list (v1.1 API) for OAuth orgs and through the private project-list endpoint for App orgs. If the source token's user does not have access to every repository, pass an explicit `--projects` list for complete coverage.

### `secrets extract`

Run this **inside a CircleCI job** that references the target context. Reads variable names from the manifest, captures their live values from the job environment, and writes a secret bundle.

```bash
circleci-migrate secrets extract \
  --manifest manifest.json \
  --context deploy-prod \
  --output secrets-deploy-prod.json
```

| Flag | Default | Description |
|---|---|---|
| `--manifest` | *(required)* | Path to the export manifest |
| `--context` | | Context name to capture (mutually exclusive with `--project`) |
| `--project` | | Project slug to capture (mutually exclusive with `--context`) |
| `--output`, `-o` | `secrets.json` | Path to write or append the secret bundle |
| `--strict` | `false` | Fail if any expected variable is missing from the environment |

### `secrets merge`

Combines multiple per-context/project bundles into one file.

```bash
circleci-migrate secrets merge \
  --output secrets.json \
  secrets-deploy-prod.json secrets-shared.json
```

### `sync`

Recreates exported data in the destination org. **Dry-run by default** — review the plan, then re-run with `--apply`.

```bash
circleci-migrate sync \
  --manifest manifest.json \
  --secrets secrets.json \
  --dest-token "$DST_TOKEN" \
  --apply
```

| Flag | Default | Description |
|---|---|---|
| `--manifest` | *(required)* | Path to the export manifest |
| `--secrets` | `secrets.json` | Path to the secret bundle (optional; silently skipped if absent) |
| `--mapping` | | Path to a source→destination mapping file (optional) |
| `--apply` | `false` | Write changes to destination (default: dry run) |
| `--yes`, `-y` | `false` | Auto-confirm enabling builds after project creation |
| `--missing-secrets` | `skip` | How to handle variables with no captured value: `skip` or `placeholder` |
| `--github-token` | `$GITHUB_TOKEN` | GitHub PAT used to resolve repository IDs for App pipeline definitions. When omitted, the captured `external_id` from the source manifest is reused (correct for same-GitHub-org migrations). |
| `--skip-contexts` | `false` | Skip syncing contexts |
| `--skip-projects` | `false` | Skip syncing projects |
| `--skip-org-settings` | `false` | Skip syncing org-level settings (feature flags, OIDC, URL-orb allow list, config policies) |

**Missing secrets:** if a variable was not captured (or you run without a secret bundle), `--missing-secrets skip` (default) omits it from the destination. Pass `--missing-secrets placeholder` to write `REPLACE_ME` so the variable exists and can be updated later.

**Cross-org rename or GitHub App destination:** supply a mapping file:

```json
{
  "org": { "from": "gh/acme", "to": "gh/acme-new" },
  "projects": {
    "gh/acme/web": "gh/acme-new/web"
  }
}
```

```bash
circleci-migrate sync \
  --manifest manifest.json \
  --secrets secrets.json \
  --mapping mapping.json \
  --apply
```

### `version`

Prints the version number, git commit SHA, and OS/architecture.

```bash
circleci-migrate version
```

---

## Security

**The secret bundle (`secrets.json`) contains plaintext environment-variable values. Treat it with the same care as a password file.**

- The file is written with `0600` permissions.
- Do **not** commit it to version control.
- Delete it once the sync is complete.
- Use a private CircleCI project for Phase 2 and set a short artifact retention period.
- `manifest.json` and `migration-report.md` contain no secret values and are safe to review, diff, and store.

---

## Further reading

- [Architecture and data flow](docs/architecture.md)
- [CircleCI API usage](docs/api-usage.md)
- [Testing guide](docs/testing.md)
- [Contributing](CONTRIBUTING.md)
