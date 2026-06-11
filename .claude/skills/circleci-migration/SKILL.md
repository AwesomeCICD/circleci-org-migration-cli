---
name: circleci-migration
description: >
  End-to-end guided walkthrough for migrating one CircleCI organization to
  another using circleci-migrate. Covers planning, export, secret capture,
  sync, validation, and troubleshooting with real commands.
---

# CircleCI Organization Migration — End-to-End Guide

This skill guides you through a complete org migration using `circleci-migrate`.
The tool moves contexts, project settings, environment variables, org-level
settings, and more — without ever exposing secret values over the API until
the precise moment they are needed.

---

## Step 1 — Plan: Identify org types and migration strategy

Before running any commands, determine the org types involved. The slug format
tells you which CircleCI integration the org uses:

| Org type | Slug format | Example |
|---|---|---|
| GitHub OAuth | `gh/<org-name>` | `gh/acme` |
| GitHub App or GitLab | `circleci/<org-uuid>` | `circleci/3ac9b843-7095-443e-bfb3-bd7037999ef5` |

**Identify your org slugs:**

- GitHub OAuth org: look at the URL when browsing `app.circleci.com` — it will
  contain `gh/your-org-name`.
- GitHub App org: the org UUID appears in the URL of your CircleCI org settings.
  It looks like `circleci/3ac9b843-7095-443e-bfb3-bd7037999ef5`.

**Choose the right migration path:**

| Source | Destination | Notes |
|---|---|---|
| OAuth (`gh/`) | OAuth (`gh/`) | Fully automated. |
| App (`circleci/`) | App (`circleci/`) | Fully automated. Provide `--github-token` if repos move GitHub orgs. |
| Mixed (org has both OAuth + App records) | Mixed | Run twice: once for the OAuth record, once for the App record. |
| OAuth → App (cross-type) | — | Planned; some settings are lost (fork PRs, OSS flag). |

**Repos moving GitHub orgs (EMU migration)?** You need a GitHub PAT and the
`--dest-github-org` flag so the tool can resolve the new repository IDs for
App pipeline definitions.

**API tokens to prepare:**

- Source org token (`CIRCLECI_SOURCE_TOKEN`): needs read access to the source org.
- Destination org token (`CIRCLECI_DEST_TOKEN`): needs admin/write access to the
  destination org.
- (Optional) GitHub PAT (`GITHUB_TOKEN`): required when repos move GitHub orgs.

---

## Step 2 — Export / Dry-run

The export phase is **read-only** — it never writes to CircleCI. Run it as many
times as you like. It produces:

- `manifest.json` — the machine-readable inventory (contexts, projects, settings,
  env-var names). Contains **no secret values**.
- `migration-report.md` — a human-readable audit: what will be automated, what
  requires manual steps, and any data-loss warnings.

### Option A — Interactive guided mode (recommended for first-time use)

Run `migrate` with no source/dest flags on an interactive terminal:

```bash
export CIRCLECI_SOURCE_TOKEN=your-source-token
export CIRCLECI_DEST_TOKEN=your-dest-token

circleci-migrate migrate
```

The walkthrough prompts for the source org, destination org, tokens (if not set
in the environment), which components to migrate, and whether to dry-run or apply.
It shows a confirmation summary before writing anything.

Pass `--no-input` to force non-interactive mode (errors immediately if required
values are missing, useful for CI pipelines).

### Option B — Non-interactive export (more control)

Export the source org to disk so you can review and optionally edit the manifest
before syncing:

```bash
export CIRCLECI_SOURCE_TOKEN=your-source-token

circleci-migrate export \
  --source-org gh/acme \
  -o manifest.json \
  --report migration-report.md
```

For a GitHub App org:

```bash
circleci-migrate export \
  --source-org circleci/3ac9b843-7095-443e-bfb3-bd7037999ef5 \
  -o manifest.json \
  --report migration-report.md
```

**Read `migration-report.md` before continuing.** Items under "Warnings & manual
follow-ups" must be handled by hand (SSO, audit-log streaming, webhook signing
secrets, checkout keys). Note which resources will need manual attention.

### Option C — All-in-one dry run (migrate command)

If you want a combined export+sync dry run in one step:

```bash
circleci-migrate migrate \
  --source-org gh/acme \
  --dest-org gh/acme-new \
  --source-token "$CIRCLECI_SOURCE_TOKEN" \
  --dest-token "$CIRCLECI_DEST_TOKEN"
# (no --apply = dry run)
```

The manifest is held in memory. To also save it to disk:

```bash
circleci-migrate migrate \
  --source-org gh/acme \
  --dest-org gh/acme-new \
  -o manifest.json \
  --report migration-report.md
```

---

## Step 3 — Capture secret values

The CircleCI API **never returns environment-variable values** — it masks every
value in every API response. Secret values must be captured from inside a running
CircleCI job, where the platform injects the real values into the environment.

The manifest contains no secrets; it is safe to review, commit, and share.
The output of this step — `secrets.json` — contains **plaintext values**.
Treat it as a password file.

### Option A — CLI-orchestrated (`secrets capture`) — no committed config required

`secrets capture` submits an inline (unversioned) pipeline config to the source
org, waits for it to complete, and downloads the merged secret bundle. No
`.circleci/config.yml` changes are needed.

```bash
circleci-migrate secrets capture \
  --source-token "$CIRCLECI_SOURCE_TOKEN" \
  --manifest manifest.json \
  --output secrets.json
```

If some contexts have restrictions that would prevent the inline pipeline from
running, you have two options:

```bash
# Option: temporarily remove restrictions (restored after the run)
circleci-migrate secrets capture \
  --manifest manifest.json \
  --output secrets.json \
  --remove-restrictions

# Option: skip restricted contexts entirely
circleci-migrate secrets capture \
  --manifest manifest.json \
  --output secrets.json \
  --skip-restricted-contexts
```

If the project has no enabled pipeline trigger (e.g. an App org project with all
triggers disabled), pass `--enable-trigger` to temporarily enable one for the run.

Default poll timeout is 10 minutes; override with `--poll-timeout 20m` for large
orgs with many contexts.

### Option B — Orb-based (committed config, full control)

Commit `manifest.json` to a repository in the **source org** and add a workflow
using the `awesomecicd/circleci-org-migration` orb. The orb is currently
**private** — your source org must have access to it.

```yaml
# .circleci/config.yml  (in your SOURCE org's repository)
version: "2.1"
orbs:
  migrate: awesomecicd/circleci-org-migration@0.2.0

workflows:
  capture-secrets:
    jobs:
      # One job per context. Each job MUST reference exactly that context
      # under the workflow `context:` key — this is how CircleCI injects
      # the real secret values. Never mix two contexts in one job.
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
      # The merge job combines all per-context bundles into secrets.json
      # and uploads it as an artifact.
      - migrate/merge:
          name: merge-secrets
          requires:
            - extract-deploy-prod
            - extract-shared
```

For large numbers of contexts, use a matrix to fan out a single job stanza.
The matrix **must** declare an explicit `alias` so the `merge` job can depend
on the whole matrix by that alias:

```yaml
workflows:
  capture-secrets:
    jobs:
      - migrate/extract_context:
          name: extract-<< matrix.context_name >>
          context:
            - << matrix.context_name >>
          matrix:
            alias: extract_contexts   # required — merge depends on this alias
            parameters:
              context_name:
                - deploy-prod
                - shared
                - build
                - staging
      - migrate/merge:
          name: merge-secrets
          requires:
            - extract_contexts        # depends on the matrix alias
```

Download `secrets.json` from the `merge` job's artifacts in the CircleCI UI.

### Handling the orb namespace overlap window

If the destination org cannot resolve the source org's private orb reference
(for example, during the `awesomecicd/` → `cci-labs/` namespace transfer), use
`orb inline` to embed the orb's current source directly into the config file:

```bash
circleci-migrate orb inline \
  --config .circleci/config.yml \
  --token "$CIRCLECI_CLI_TOKEN" \
  --output .circleci/config.yml
```

This replaces the orb stanza reference with the orb's actual YAML source so the
config works regardless of which namespace is currently active.

### Security precautions for secrets.json

- `secrets.json` is written with `0600` permissions (owner-read-only).
- Do **not** commit it to version control.
- Use a **private** CircleCI project for the capture pipeline.
- Set a **short artifact retention period** (1 day) on the capture pipeline.
- Delete the artifact and the local file once the destination sync is complete.
- **Rotate every captured secret value** after the migration is confirmed healthy.

---

## Step 4 — Sync / Apply to the destination org

Sync is **dry-run by default**. Always review the dry-run output before applying.
Sync is **idempotent**: re-running is safe; it reuses existing contexts and
projects by name rather than duplicating them.

### Dry run first

```bash
circleci-migrate sync \
  --manifest manifest.json \
  --secrets secrets.json \
  --dest-token "$CIRCLECI_DEST_TOKEN"
# (no --apply = dry run, prints the plan)
```

Review the output. Each resource shows one of: `created`, `exists`, `set`,
`manual`, or `error`. Items marked `manual` require human action (see the
migration report for details).

### Apply

```bash
circleci-migrate sync \
  --manifest manifest.json \
  --secrets secrets.json \
  --dest-token "$CIRCLECI_DEST_TOKEN" \
  --apply
```

After `--apply`, new projects are created in a **paused** state — no builds fire
until you explicitly enable them. You will see a prompt:

```
Enable builds for 3 project(s) now? [y/N]:
```

To auto-confirm and enable immediately, pass `--yes` (or `-y`):

```bash
circleci-migrate sync \
  --manifest manifest.json \
  --secrets secrets.json \
  --dest-token "$CIRCLECI_DEST_TOKEN" \
  --apply \
  --yes
```

### Project creation details by org type

**GitHub OAuth (`gh/`) destination:**
Projects are created as shells (no webhook installed). When you enable builds
(via `--yes` or the prompt), `sync` follows each project, which installs a
deploy key and webhook and may trigger an initial build.

**GitHub App (`circleci/`) destination:**
Projects are created with their pipeline definitions and triggers. Triggers are
created **disabled (`disabled: true`)** and only activated when you enable builds.
Repos must already be connected to the destination CircleCI GitHub App before
`sync --apply` runs, or project onboarding is skipped.

**Webhook and schedule triggers (App orgs):**
These are flagged as `manual` — webhook HMAC secrets cannot be migrated and
schedule-trigger creation via the Trigger API is a planned future addition. The
sync report lists them explicitly.

### Cross-org or GitHub repo move

If the destination org has a different name or uses a mapping file:

```bash
# Explicit dest-github-org when repos moved GitHub orgs
circleci-migrate sync \
  --manifest manifest.json \
  --secrets secrets.json \
  --dest-token "$CIRCLECI_DEST_TOKEN" \
  --github-token "$GITHUB_TOKEN" \
  --dest-github-org acme-new \
  --apply
```

Or using a mapping file to control per-project name changes:

```json
{
  "org": { "from": "gh/acme", "to": "gh/acme-new" },
  "projects": {
    "gh/acme/web": "gh/acme-new/web",
    "gh/acme/api": "gh/acme-new/api"
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

### Handling missing secrets

If a variable was not captured (or you run without a secret bundle):

```bash
# Default: skip variables with no captured value (they will need manual entry)
circleci-migrate sync --manifest manifest.json --apply --missing-secrets skip

# Alternative: write REPLACE_ME so the variable exists and can be updated later
circleci-migrate sync --manifest manifest.json --apply --missing-secrets placeholder
```

### All-in-one apply (migrate command)

For migrations that do not need to inspect the manifest between phases:

```bash
circleci-migrate migrate \
  --source-org gh/acme \
  --dest-org gh/acme-new \
  --secrets secrets.json \
  --apply \
  --yes \
  -o manifest.json \
  --report migration-report.md
```

---

## Step 5 — Validate

After applying, verify the destination org matches the source:

1. **Re-export the destination org** and diff against the source manifest:

   ```bash
   circleci-migrate export \
     --source-org gh/acme-new \
     --source-token "$CIRCLECI_DEST_TOKEN" \
     -o manifest-dest.json \
     --report migration-report-dest.md

   diff manifest.json manifest-dest.json
   ```

2. **Spot-check in the CircleCI UI:**
   - Contexts: names, env-var names, and restrictions match.
   - Projects: advanced settings, env-var names, webhooks, and schedules match.
   - Org settings: feature flags, OIDC claims, config policies.

3. **Confirm builds are enabled:**
   - OAuth: check that projects are followed and webhooks are installed.
   - App: check that triggers are enabled (not paused).

4. **Trigger a test build** on the destination to confirm the pipeline runs and
   secrets are injected correctly.

5. **Manual items from the migration report** — work through each `manual` item:
   - Rotate captured secrets and set the fresh values.
   - Recreate webhook HMAC signing secrets and update receiver endpoints.
   - Regenerate checkout/deploy keys (private key material is never exported).
   - Recreate SSO (SAML) via DNS verification + IdP setup.
   - Recreate audit-log streaming configs against destination-owned infrastructure.
   - Re-add OpenTelemetry exporter header values (redacted by the API).

6. **Update external pins** that reference the old org:
   - Service catalogs / Backstage entries.
   - Slack and other notification integrations.
   - Dashboards, status badges, and Insights links.
   - Branch-protection / required status-check integrations on the VCS side.
   - Documentation, READMEs, and bookmarks.

---

## Step 6 — Troubleshoot

### Enable debug logging

Re-run any command with `--debug` to see full HTTP request/response detail:

```bash
circleci-migrate export --source-org gh/acme --debug
circleci-migrate sync --manifest manifest.json --debug
```

The debug output includes the endpoint, HTTP method, status code, request ID
(`X-Request-Id` header), and the response body for every API call.

### Reading errors

Every `4xx`/`5xx` error surfaces as:

```
Error: POST https://circleci.com/api/v2/context: 403 Forbidden
  message: "Insufficient permissions"
  request-id: abc123
```

Take the endpoint, status, and request ID when filing an issue at:
https://github.com/AwesomeCICD/circleci-org-migration-cli/issues

### Common issues

**"no source API token"**
One of `--source-token`, `--token`, `CIRCLECI_SOURCE_TOKEN`, or `CIRCLECI_CLI_TOKEN`
must be set. Verify with:
```bash
echo $CIRCLECI_SOURCE_TOKEN
```

**"no destination API token"**
Same as above but for the destination. Set `CIRCLECI_DEST_TOKEN` or use
`--dest-token`.

**Org slug format wrong**
OAuth orgs use `gh/<name>` (e.g. `gh/acme`). GitHub App and GitLab orgs use
`circleci/<uuid>` (e.g. `circleci/3ac9b843-7095-443e-bfb3-bd7037999ef5`).
The UUID comes from your CircleCI org settings URL.

**GitHub App vs OAuth confusion**
A GitHub organization that uses the CircleCI GitHub App integration registers as
two separate CircleCI org records — one OAuth record (`gh/`) and one App record
(`circleci/`). If you are migrating both sides, run `export`/`sync` twice: once
per record.

**Matrix missing `alias`**
When using a matrix for `extract_context`, the `merge` job must depend on the
matrix alias, not on individual job names. Add `alias: extract_contexts` (or any
name) under `matrix:` and use that alias in `merge`'s `requires:` list.
Without `alias`, the `requires:` entry will not match the dynamically-named jobs.

**Secrets appear masked in the destination**
Secret values captured by the orb will be present only if the context was
referenced under the workflow `context:` key of the extract job. If a context
was missed, re-run `secrets capture` or add the missing context to the orb config
and re-run the pipeline.

**Context restrictions blocking secrets capture**
If a context has group or expression restrictions the inline pipeline may be
rejected. Use `--remove-restrictions` (temporarily removes and restores them)
or `--skip-restricted-contexts` (skips those contexts entirely).

**Project not created — App destination**
Repositories must be connected to the destination org's CircleCI GitHub App
before `sync --apply`. Check that the GitHub App is installed and the repos are
visible in the destination org's project list.

**"webhook trigger: manual" in sync report**
Webhook HMAC secrets cannot be migrated. After sync, recreate the webhook signing
secret in the destination org and update the receiving endpoint's verification
configuration.

**Org settings not migrated (App destination)**
Feature flags via the v1.1 API are not available for GitHub App (`circleci/`) orgs.
This produces a warning in the manifest and is expected behaviour — not an error.

---

## Quick-reference command cheatsheet

```bash
# Interactive guided walkthrough (prompts for everything)
circleci-migrate migrate

# Export source org to disk
circleci-migrate export --source-org gh/acme -o manifest.json --report migration-report.md

# CLI-orchestrated secret capture (no config commit needed)
circleci-migrate secrets capture \
  --manifest manifest.json --output secrets.json

# Dry run sync (no writes)
circleci-migrate sync --manifest manifest.json --secrets secrets.json

# Apply sync (writes to destination)
circleci-migrate sync --manifest manifest.json --secrets secrets.json --apply --yes

# All-in-one migrate (dry run)
circleci-migrate migrate --source-org gh/acme --dest-org gh/acme-new

# All-in-one migrate (apply, save files)
circleci-migrate migrate \
  --source-org gh/acme --dest-org gh/acme-new \
  --secrets secrets.json --apply --yes \
  -o manifest.json --report migration-report.md

# Re-export destination for diff-based validation
circleci-migrate export --source-org gh/acme-new --source-token "$CIRCLECI_DEST_TOKEN" \
  -o manifest-dest.json

# Inline private orb source (namespace overlap window)
circleci-migrate orb inline \
  --config .circleci/config.yml --token "$CCI_TOKEN" \
  --output .circleci/config.yml

# Print version
circleci-migrate version

# Debug any command
circleci-migrate <command> --debug ...
```

---

## Environment variable reference

| Variable | Used by |
|---|---|
| `CIRCLECI_SOURCE_TOKEN` | Source org API token (read operations) |
| `CIRCLECI_DEST_TOKEN` | Destination org API token (write operations) |
| `CIRCLECI_CLI_TOKEN` | Fallback token for both orgs |
| `CIRCLECI_HOST` | CircleCI host (default `https://circleci.com`; set for Server installs) |
| `GITHUB_TOKEN` | GitHub PAT for resolving repo IDs when repos move GitHub orgs |

---

## What does NOT transfer automatically (always manual)

| Resource | Why |
|---|---|
| Secret values | API never returns them; captured separately via pipeline |
| Checkout / SSH keys | Private key material is not exportable; regenerate on destination |
| Webhook HMAC signing secrets | Not returned by API; regenerate and update receivers |
| SSO (SAML) | DNS verification + IdP setup required; not automatable |
| Audit-log streaming | AWS ARN/bucket is source-specific; recreate against dest infrastructure |
| OTel exporter header values | Redacted by the API; re-add manually after sync |
| Project-type context restrictions | Source project UUIDs do not transfer; recreate manually |
| App webhook / schedule triggers | Webhook HMAC secret cannot be migrated; schedule Trigger API is planned |
| Additional SSH keys | Not available via API |
