# circleci-migrate

`circleci-migrate` is a command-line tool that moves configuration data from
one CircleCI organization to another. It handles contexts (including
environment-variable values and expression restrictions), project settings,
and project environment variables, and produces a human-readable audit report
so you can verify exactly what was captured before writing anything to the
destination.

> **Status: active development.**
> Context sync is fully implemented and tested. Project sync and the
> all-in-one `migrate` command are planned for a future milestone.

---

## How it works — the three-phase model

CircleCI's API masks every secret value it stores. You can read the *names* of
environment variables, but never their values. `circleci-migrate` handles this
constraint by splitting the migration into three phases:

```
Phase 1 — export       Read the source org. Produces manifest.json
                       (structure + variable names, no secret values)
                       and migration-report.md (human-readable audit).

Phase 2 — in-pipeline  Run a CircleCI workflow inside the source org.
          secret        Each job references one context; the orb captures
          extraction    the live variable values and writes secrets.json.

Phase 3 — sync         Read manifest.json + secrets.json and recreate
                       everything in the destination org. Dry run by
                       default; pass --apply to write.
```

Phases 1 and 3 run on your local machine (or any CI job with network access
to CircleCI). Phase 2 *must* run inside a CircleCI pipeline so that the
platform injects the real secret values into the job environment.

---

## Security

**The secret bundle (`secrets.json`) contains plaintext environment-variable
values. Treat it with the same care as a password file.**

- The file is written with `0600` permissions.
- Do not commit it to version control.
- Delete it once the sync is complete.
- `manifest.json` and `migration-report.md` contain no secret values and are
  safe to review, diff, and store.

---

## What is (and is not) migrated

| Resource | Captured | Synced | Notes |
|---|---|---|---|
| Context names | Yes | Yes | Created by name; destination assigns its own ID |
| Context env-var names | Yes | Yes | Names captured via API |
| Context env-var values | In-pipeline only | Yes (with secret bundle) | API never returns values; must use orb |
| Expression restrictions | Yes | Yes | Recreated on sync |
| Project restrictions | Yes (name recorded) | Manual | Source-org project IDs do not transfer; recreate manually |
| Group (security-group) restrictions | Yes (name recorded) | Manual | Group-restriction writes are not yet GA |
| Project settings (advanced) | Yes | In progress (M2) | Settings captured; sync not yet applied |
| Project env-var names | Yes | In progress (M2) | |
| Project env-var values | In-pipeline only | In progress (M2) | Same constraint as context values |
| Checkout key fingerprints | Yes (public metadata only) | Not yet | Private keys cannot be exported; regenerate on destination |
| Webhooks | Yes (metadata) | Not yet | |
| Scheduled pipelines | Yes (metadata) | Not yet | |
| Org-level settings | Partial (`require_context_group_restriction` only) | Manual | Most org settings are not exposed by any API |
| Additional SSH keys | No | No | Not available via API |

---

## Installation

Released binaries are not yet published. Build from source:

```bash
git clone https://github.com/CircleCI-Public/circleci-org-migration-cli.git
cd circleci-org-migration-cli
make build          # produces ./bin/circleci-migrate
```

Requirements: Go 1.26 or later, `make`.

Once releases are published, the orb's `install` command will fetch them
automatically — see [Phase 2](#phase-2-in-pipeline-secret-extraction) below.

---

## Global flags

These flags are available to every sub-command. Environment variables are
read before flag parsing, so they act as defaults that flags can override.

| Flag | Environment variable | Default | Description |
|---|---|---|---|
| `--host` | `CIRCLECI_HOST` | `https://circleci.com` | CircleCI host URL (useful for Server installs) |
| `--token` | `CIRCLECI_CLI_TOKEN` | | Personal API token — fallback for both orgs |
| `--source-token` | `CIRCLECI_SOURCE_TOKEN` | | API token for the source org (read operations) |
| `--dest-token` | `CIRCLECI_DEST_TOKEN` | | API token for the destination org (write operations) |
| `--debug` | | `false` | Enable debug logging |

---

## Quickstart

### Phase 1 — Export

Export reads the source org and produces two files:

- `manifest.json` — the structured export (safe to review and store)
- `migration-report.md` — a human-readable audit of everything captured

```bash
circleci-migrate export \
  --org gh/acme \
  --source-token "$SRC_TOKEN"
```

The `--org` flag takes a *slug*:

- `gh/<org>` for GitHub OAuth organizations
- `circleci/<org-id>` for GitHub App or GitLab organizations

Additional export flags:

| Flag | Default | Description |
|---|---|---|
| `--org` | *(required)* | Source organization slug |
| `--output`, `-o` | `manifest.json` | Path to write the JSON manifest |
| `--report` | `migration-report.md` | Path to write the audit report |
| `--projects` | *(all followed)* | Explicit project slugs to export, comma-separated |
| `--skip-contexts` | `false` | Skip exporting contexts |
| `--skip-projects` | `false` | Skip exporting projects |
| `--skip-extras` | `false` | Skip checkout keys, webhooks, and schedules |

> **Project discovery:** by default, export discovers projects through the
> followed-projects list (API v1.1). If the source token's user does not
> follow every repository, pass an explicit `--projects` list to ensure
> complete coverage.

After export, review `migration-report.md`. Any item that requires manual
follow-up (group restrictions, project restrictions) is listed under
"Warnings & manual follow-ups".

### Phase 2 — In-pipeline secret extraction

Because the CircleCI API never returns environment-variable values,
`circleci-migrate` provides a [CircleCI orb](orb/orb.yml) that runs inside
your source org's pipeline. Each job references exactly one context so the
platform injects its variables; the `secrets extract` command reads them from
the live environment and writes a bundle file.

**Step 1.** Commit `manifest.json` to your repository (it contains no secrets).

**Step 2.** Add a workflow to `.circleci/config.yml` in your source org:

```yaml
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

**Step 3.** Download the `secrets.json` artifact from the `merge` job. This
file contains plaintext values — see the [Security](#security) section above.

**Manual extraction (without the orb):**

```bash
# Inside a job that references the target context:
circleci-migrate secrets extract \
  --manifest manifest.json \
  --context deploy-prod \
  --output secrets-deploy-prod.json

# Merge multiple bundles locally:
circleci-migrate secrets merge \
  --output secrets.json \
  secrets-deploy-prod.json secrets-shared.json
```

`secrets extract` flags:

| Flag | Default | Description |
|---|---|---|
| `--manifest` | *(required)* | Path to the export manifest |
| `--context` | | Context name to capture (mutually exclusive with `--project`) |
| `--project` | | Project slug to capture (mutually exclusive with `--context`) |
| `--output`, `-o` | `secrets.json` | Path to write or append the secret bundle |
| `--strict` | `false` | Fail if any expected variable is missing from the environment |

### Phase 3 — Sync

Sync reads the manifest (and optionally the secret bundle) and recreates
everything in the destination org. By default it performs a **dry run** —
it prints a plan of what would happen but writes nothing. Review the output,
then re-run with `--apply`.

```bash
# Dry run — review the plan first
circleci-migrate sync \
  --manifest manifest.json \
  --secrets secrets.json \
  --dest-token "$DST_TOKEN"

# Apply the changes
circleci-migrate sync \
  --manifest manifest.json \
  --secrets secrets.json \
  --dest-token "$DST_TOKEN" \
  --apply
```

Sync is **idempotent**: if a context already exists in the destination org it
is reused (not recreated). Re-running sync is safe.

For cross-org renames or GitHub App destinations (where the slug is
`circleci/<org-id>` and cannot be derived from the repo name), supply a
mapping file:

```bash
# mapping.json
{
  "org": { "from": "gh/acme", "to": "gh/acme-new" },
  "projects": {
    "gh/acme/web": "gh/acme-new/web"
  }
}

circleci-migrate sync \
  --manifest manifest.json \
  --secrets secrets.json \
  --mapping mapping.json \
  --apply
```

`sync` flags:

| Flag | Default | Description |
|---|---|---|
| `--manifest` | *(required)* | Path to the export manifest |
| `--secrets` | `secrets.json` | Path to the captured secret bundle (optional) |
| `--mapping` | | Path to a source→destination mapping file (optional) |
| `--apply` | `false` | Write changes to the destination (default: dry run) |
| `--missing-secrets` | `skip` | How to handle variables with no captured value: `skip` or `placeholder` |

**Missing secrets:** if you run sync without a secret bundle (or a variable
was not captured), the default behavior (`--missing-secrets skip`) leaves
that variable out of the destination context. Pass `--missing-secrets placeholder`
to write the placeholder value `REPLACE_ME` instead, so the variable exists
in the destination and you can update it later.

---

## GitHub OAuth vs GitHub App

The org slug format you use affects which APIs are available:

- **GitHub OAuth** (`gh/<org>`): full v1.1 and v2 API coverage, including
  project discovery via followed projects and project follow.
- **GitHub App / GitLab** (`circleci/<org-id>`): v2 API only. Project
  slugs use UUIDs and cannot be derived from repo names; you must supply
  an explicit `--projects` list for export and a `--mapping` file for sync.

---

## Further reading

- [Architecture and data flow](docs/architecture.md)
- [CircleCI API usage](docs/api-usage.md)
- [Contributing](CONTRIBUTING.md)
