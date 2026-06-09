[![CircleCI](https://dl.circleci.com/status-badge/img/gh/AwesomeCICD/circleci-org-migration-cli/tree/main.svg?style=svg)](https://dl.circleci.com/status-badge/redirect/gh/AwesomeCICD/circleci-org-migration-cli/tree/main) ![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white) [![Conventional Commits](https://img.shields.io/badge/Conventional%20Commits-1.0.0-yellow.svg)](https://conventionalcommits.org)

# circleci-migrate

`circleci-migrate` moves configuration data from one CircleCI organization to another — contexts, project settings, environment variables, and org-level settings — with a safe, auditable three-phase approach that never requires you to expose secrets in plain text until they are needed.

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

---

## Install

Prebuilt binaries are coming. For now, build from source:

```bash
git clone https://github.com/CircleCI-Public/circleci-org-migration-cli.git
cd circleci-org-migration-cli
make build            # produces ./bin/circleci-migrate
# or without make:
go build -o bin/circleci-migrate .
```

**Requirements:** Go 1.26 or later.

Once releases are published, the orb's `install` command will fetch them automatically — see [Phase 2](#phase-2-capture-secrets-inside-a-pipeline) below.

---

## Quick start

### Phase 1 — Export the source org

```bash
circleci-migrate export \
  --org gh/acme \
  --source-token "$SRC_TOKEN"
# Produces: manifest.json  migration-report.md
```

Review `migration-report.md`. Items requiring manual follow-up (group restrictions, project restrictions, SSO, audit-log) are listed under "Warnings & manual follow-ups".

The `--org` slug format:
- `gh/<org>` for GitHub OAuth organizations
- `circleci/<org-id>` for GitHub App or GitLab organizations

### Phase 2 — Capture secrets inside a pipeline

Because the API never returns secret values, capturing them requires running inside a CircleCI job. Commit `manifest.json` to your source org's repository (it contains no secrets), then add a workflow:

```yaml
# .circleci/config.yml in your SOURCE org
version: "2.1"
orbs:
  migrate: circleci-public/circleci-org-migration@1.0.0

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

## What gets migrated

| Resource | Captured | Synced | Notes |
|---|---|---|---|
| Context names | Yes | Yes | Created by name; destination assigns its own ID |
| Context env-var names | Yes | Yes | Names captured via API |
| Context env-var values | In-pipeline only | Yes (with secret bundle) | API never returns values; must use orb |
| Expression restrictions | Yes | Yes | Recreated on sync |
| Project restrictions | Yes (name recorded) | Manual | Source-org project IDs do not transfer; recreate manually |
| Group (security-group) restrictions | Yes (name recorded) | Manual | Group-restriction writes are not yet GA |
| Project advanced settings | Yes | Yes | `autocancel_builds`, `build_fork_prs`, etc. |
| Project env-var names | Yes | Yes | Names captured via API |
| Project env-var values | In-pipeline only | Yes (with secret bundle) | Same constraint as context values |
| Org feature flags | Yes | Yes | Full flag map via v1.1 API; safe/relevant flags written back |
| OIDC custom claims | Yes | Yes | Audience list and TTL via v2 API |
| URL-orb allow list | Yes | Yes | GitHub App / circleci-type orgs only |
| Config policies (Rego) | Yes | Yes | Scale plan only; enforcement toggle included |
| Audit-log streaming configs | Yes (captured) | Manual | AWS ARN/bucket is source-specific; recreate manually |
| SSO (SAML) configuration | Yes (captured) | Manual | Requires DNS verification + IdP setup; never auto-written |
| Checkout key fingerprints | Yes (public metadata only) | Not yet | Private keys cannot be exported; regenerate on destination |
| Webhooks | Yes (metadata) | Not yet | |
| Scheduled pipelines | Yes (metadata) | Not yet | |
| Additional SSH keys | No | No | Not available via API |

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

> **Project discovery:** export discovers projects through the followed-projects list (v1.1 API). If the source token's user does not follow every repository, pass an explicit `--projects` list for complete coverage.

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
| `--secrets` | `secrets.json` | Path to the secret bundle (optional) |
| `--mapping` | | Path to a source→destination mapping file (optional) |
| `--apply` | `false` | Write changes to destination (default: dry run) |
| `--missing-secrets` | `skip` | How to handle variables with no captured value: `skip` or `placeholder` |
| `--skip-contexts` | `false` | Skip syncing contexts |
| `--skip-projects` | `false` | Skip syncing projects |
| `--skip-org-settings` | `false` | Skip syncing org-level settings |

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

---

## Security

**The secret bundle (`secrets.json`) contains plaintext environment-variable values. Treat it with the same care as a password file.**

- The file is written with `0600` permissions.
- Do **not** commit it to version control.
- Delete it once the sync is complete.
- Use a private CircleCI project for Phase 2 and set a short artifact retention period.
- `manifest.json` and `migration-report.md` contain no secret values and are safe to review, diff, and store.

---

## GitHub OAuth vs GitHub App

The org slug format affects which APIs are available:

- **GitHub OAuth** (`gh/<org>`): full v1.1 and v2 API coverage, including project discovery via followed projects.
- **GitHub App / GitLab** (`circleci/<org-id>`): v2 API only. Project slugs use UUIDs; you must supply an explicit `--projects` list for export and a `--mapping` file for sync.

---

## Further reading

- [Architecture and data flow](docs/architecture.md)
- [CircleCI API usage](docs/api-usage.md)
- [Contributing](CONTRIBUTING.md)
