# CircleCI org migration — cutover runbook

This is the generic operator runbook for migrating a CircleCI organization with
`circleci-migrate`. Every export also embeds a copy of this runbook in its
`migration-report.md`, tailored to the resources that export actually contains.
Use this document to understand the overall flow; use the per-export report for
the manual steps and data-loss notes that apply to your org.

**Quick links:**
- [README and interactive-first getting-started guide](../README.md)
- [Worked examples for every migration type](examples.md)
- [CLI reference (auto-generated, all flags)](cli/README.md)
- [Man pages](../man/)
- [Contributing and development guide](../CONTRIBUTING.md)
- [`.claude/skills/circleci-migration`](../.claude/skills/) — Claude Code skill for AI-assisted migrations

---

## 1. Recommended cutover order

### Step 1 — Export the source org

```bash
circleci-migrate export \
  --source-org gh/acme \
  --source-token "$SRC_TOKEN" \
  --output manifest.json \
  --report migration-report.md
```

Produces `manifest.json` (structure + names, no secret values) and
`migration-report.md` (human-readable audit). The manifest is non-secret and
safe to review, diff, and store. **Read the report before continuing** — it
lists every warning and manual follow-up required for your specific org.

### Step 2 — Capture secret values (recommended: guided `secrets capture`)

CircleCI never returns env-var values over the API, so capturing them requires
running inside a CircleCI pipeline. The recommended way is the interactive
`secrets capture` walkthrough:

```bash
circleci-migrate secrets capture
```

This guided flow (6 steps) will:

1. Load the manifest and enumerate contexts and projects.
2. Let you select what to capture.
3. Ask you to choose a **host project** for context extraction (any project works).
4. Offer **age encryption** of the in-pipeline artifact (strongly recommended — plaintext never lands in CircleCI artifact storage).
5. Let you choose storage: CircleCI artifact, S3, or both.
6. Offer to set **artifact retention to 1 day** before the run (strongly recommended).

For non-interactive / CI use, supply all flags:

```bash
circleci-migrate secrets capture \
  --manifest manifest.json \
  --encrypt --generate-key \
  --artifact-retention-days 1 \
  --enable-trigger \
  --output secrets.json
```

The result is a plaintext `secrets.json` on your local machine — treat it as
sensitive. Delete it after the sync is confirmed successful.

See [Example 5 in docs/examples.md](examples.md#example-5--secrets-capture-in-detail)
for the full orb-based alternative (committed config, matrix capture).

### Step 3 — Dry run (review the plan)

```bash
circleci-migrate sync \
  --manifest manifest.json \
  --secrets secrets.json \
  --dest-token "$DST_TOKEN"
```

No `--apply` → dry run only. Every action is shown as `created (would create)`,
`set (would set)`, or `manual`. Nothing is written.

### Step 4 — `sync --apply`

```bash
circleci-migrate sync \
  --manifest manifest.json \
  --secrets secrets.json \
  --dest-token "$DST_TOKEN" \
  --apply
```

Creates the destination resources. New projects are created **paused**: App
triggers are disabled and OAuth projects are not followed, so nothing builds
until you explicitly enable it.

Or use the all-in-one `migrate` for steps 1 + 3 + 4 together:

```bash
circleci-migrate migrate \
  --source-org gh/acme \
  --dest-org gh/acme-cloud \
  --secrets secrets.json \
  --apply --yes \
  --output manifest.json \
  --report migration-report.md
```

### Step 5 — Validate the destination

Compare contexts, env-var names, project settings, webhooks, schedules, and
group restrictions against the audit report. Work through the manual-follow-up
items in [Section 3](#3-manual-steps-required) below.

### Step 6 — Enable builds

Bring the destination live:
- Answer **Y** at the `Enable builds for N project(s) now? [y/N]:` prompt, or
  re-run `sync --apply --yes` / `migrate --apply --yes`.
- For GitHub App orgs this enables (unpauses) the pipeline triggers.
- For GitHub OAuth orgs this follows the projects, installing deploy keys and webhooks.

### Step 7 — Rotate the captured secrets

Once builds are healthy, rotate every value captured in step 2 and delete:
- `secrets.json` (your local copy)
- The CircleCI build artifact(s) from the extraction run (if not already expired via the 1-day retention setting)
- Any local logs or shells that may contain captured values

### Step 8 — Update external pins

Repoint everything that references the old org (see [Section 5](#5-update-external-pins)).

---

## 2. Automated by `sync --apply`

- Contexts and their environment variables (names; values from the capture step).
- Context expression restrictions and group restrictions (mapped onto destination CIAM groups by name).
- Project settings, environment variables, webhooks, and scheduled pipelines.
- Project- and org-level OIDC custom claims (audience / TTL).
- Project pipeline definitions and triggers (GitHub App orgs; triggers created disabled).
- Org settings:
  - v1.1 feature flags
  - URL-orb allow list
  - Config policies (Rego, Scale plan only)
  - OTel exporter configurations (header values redacted — see manual steps)
  - Technical and security contacts
  - Spend budgets
  - Block-unregistered-users flag
  - Release-tracker settings
- Self-hosted runner resource classes (when `--runner-namespace` / `--dest-runner-namespace` are supplied).
- Project creation: OAuth orgs are onboarded by following the project; App orgs get their pipeline definitions and triggers recreated.

---

## 3. Manual steps required

Which of these apply depends on your source org; the per-export report lists only
the relevant ones.

- **Context & project secret values** — never exported automatically. Capture
  them in the source via `secrets capture` or the orb, supply the bundle to
  `sync`, then rotate after cutover.
- **Checkout & SSH keys** — private key material is never exported. Regenerate
  deploy/checkout and user keys on the destination and update VCS-side deploy keys.
- **Webhook signing secrets** — not exported; regenerate and update receivers.
- **SSO (SAML)** — recreate manually (DNS TXT domain verification + IdP-side SAML
  app). Not automatable.
- **Audit-log streaming** — the S3 ARN/region/bucket/endpoint point at the source
  AWS account; recreate each stream against destination-owned infrastructure.
- **OpenTelemetry exporter headers** — header values are server-redacted and
  cannot be replayed; re-add them manually after `sync` creates the exporters.
- **Context project restrictions** — source-org project UUIDs do not transfer;
  recreate project-type context restrictions in the destination org settings UI
  after sync.
- **Danger flags** (`require_context_group_restriction`, `drop_all_build_requests`)
  — enable only after validation, or they can silently break or drop builds.
- **Org orb list** — org-level orbs must be republished in the destination
  namespace. The report lists each orb name.
- **Environment hierarchy** — captured as reference data; apply manually if needed.
- **Repository connections (App destinations)** — repos must exist and be
  connected to the destination CircleCI GitHub App before `sync --apply`, or
  project onboarding is skipped with a warning.

---

## 4. Does not transfer / data loss

- **Identifiers change.** Project, context, and pipeline UUIDs are reassigned by
  the destination; anything hard-coding a source UUID must be updated.
- **Captured secrets must be rotated.** Treat every captured value as exposed
  once it has been written to a file or artifact, even if encrypted.
- **Cross-type moves lose settings.** OAuth → App drops fork-PR builds, the OSS
  flag, and `pr_only_branch_overrides` (no App equivalent). Multiple App pipeline
  definitions cannot collapse into a single OAuth config.
- **Checkout key private material** is never exportable via any API. Regenerate
  deploy keys on the destination.
- **Additional SSH keys** (project SSH keys added manually beyond checkout keys)
  are not available via the API and cannot be migrated.

---

## 5. Update external pins

After cutover, update everything that points at the old org to the new org's
slugs/IDs:

- Service catalogs / Backstage entries referencing the old project slugs.
- Slack and other notification integrations.
- Dashboards, status badges, and Insights links.
- Branch-protection / required status-check integrations on the VCS side.
- Documentation, READMEs, and bookmarks linking to the old org.
- Any tooling or scripts that hard-code CircleCI project or context UUIDs.

---

## Troubleshooting

**Dry run shows everything as `manual`:** you ran without a secrets bundle. That
is expected for a first dry run — the plan still shows contexts and projects as
`created (would create)`. Run `secrets capture` or use the orb to produce `secrets.json`.

**`--apply` fails on project creation (App org):** repos must be connected to
the destination CircleCI GitHub App before `sync --apply`. Check the GitHub App
installation in the destination org's settings.

**Pipeline definitions show wrong `external_id`:** if repos have moved GitHub
orgs, add `--github-token` and `--dest-github-org` (see
[Example 6 in docs/examples.md](examples.md#example-6--repo-move--emu-repos-moved-to-a-new-github-org)).

**Context restriction shows `manual`:** project-type restrictions cannot be
migrated automatically (source-org project UUIDs do not transfer). Recreate
them in the destination org settings UI after sync.

**`secrets capture` fails with `api-trigger-with-config disabled`:** add
`--enable-trigger` to the capture command. This enables the flag temporarily
for the extraction run and restores it afterwards.

**Debug mode:** add `--debug` to any command for verbose HTTP request/response
logging:

```bash
circleci-migrate sync --manifest manifest.json --debug
```
