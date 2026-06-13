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
- [API usage reference (every endpoint the CLI calls)](api-usage.md)
- [Contributing and development guide](../CONTRIBUTING.md)

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
- Project additional SSH keys — re-uploaded by fingerprint match (skipped with `--skip-extras`). The private key material must be supplied; see manual steps.
- Project- and org-level OIDC custom claims (audience / TTL).
- Project pipeline definitions and triggers (GitHub App orgs; triggers created disabled).
- **CIAM org-level roles and groups** (standalone `circleci/`-type orgs only): groups are recreated and org roles are assigned for users who already exist in the destination org (matched by email). Skipped with `--skip-ciam`. Per-project CIAM grants are **manual** (see [#179](https://github.com/AwesomeCICD/circleci-org-migration-cli/issues/179) and Section 3).
- Org settings:
  - v1.1 feature flags
  - URL-orb allow list
  - Config policies (Rego, Scale plan only)
  - OTel exporter configurations (header values redacted — see manual steps)
  - Technical and security contacts
  - Storage / artifact retention
  - Spend budgets
  - Block-unregistered-users flag
  - Release-tracker settings
- Self-hosted runner **resource classes** (when `--runner-namespace` on export and `--dest-runner-namespace` on sync are supplied). Definitions only — runner instances and registration tokens do not transfer.
- Project creation: OAuth orgs are onboarded by following the project; App orgs get their pipeline definitions and triggers recreated.
- Project **API tokens** — only when `--create-project-tokens` is set; each recreated token mints a NEW one-time secret, so every consumer must be repointed. Off by default (manual steps emitted instead).

---

## 3. Manual steps required

Which of these apply depends on your source org; the per-export report lists only
the relevant ones.

- **Context & project secret values** — never exported automatically. Capture
  them in the source via `secrets capture` (recommended) or the orb, supply the
  bundle to `sync`, then rotate after cutover.
- **Checkout keys & additional SSH keys** — private key material is never
  exported by any API. Checkout/deploy keys must be regenerated on the
  destination (and updated VCS-side). Additional SSH keys are re-uploaded only
  if you supply the private key material to `sync`; otherwise regenerate them.
- **Per-project CIAM role grants** — user and group grants on individual
  projects are reported as `manual` because the destination project UUID is not
  reliably mappable from the source. Recreate them on the destination project
  ([#179](https://github.com/AwesomeCICD/circleci-org-migration-cli/issues/179)).
  Org-level CIAM roles/groups *are* automated (standalone orgs).
- **Unmatched CIAM users** — org-role grants are only applied for users who
  already exist in the destination org. Add the user to the destination org
  first, then re-run `sync --apply`.
- **Project API tokens** — recreated only with `--create-project-tokens`; by
  default they are reported as manual. Recreating a token mints a NEW value, so
  repoint every consumer of the old token.
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

## 4. What does NOT transfer (canonical list)

This is the single canonical "does not transfer / data loss" reference for
`circleci-migrate`. Other docs link here rather than duplicating it.

| Item | Why it does not transfer | What to do |
|---|---|---|
| **Identifiers (project / context / pipeline UUIDs)** | Reassigned by the destination org. | Update anything that hard-codes a source UUID. |
| **Secret values** | Masked by the API; only captured plaintext (via `secrets capture`/orb) can be synced. | Capture, sync, then **rotate** — treat every captured value as exposed once written to a file or artifact, even if encrypted. |
| **Checkout / deploy key private material** | Never exportable via any API. | Regenerate deploy keys on the destination and update the VCS side. |
| **Additional SSH key private material** | Public key + fingerprint export, but private keys are never returned. | Re-upload by supplying the private key to `sync`, or regenerate. |
| **Webhook signing (HMAC) secrets** | Not exported. | Regenerate and update receivers after sync creates the webhooks. |
| **Project API token values** | Returned only once at creation. | Recreate with `--create-project-tokens` (mints NEW values) and repoint consumers, or recreate manually. |
| **Per-project CIAM role grants** | Destination project UUID not reliably mappable ([#179](https://github.com/AwesomeCICD/circleci-org-migration-cli/issues/179)). | Recreate user/group grants on the destination project. |
| **CIAM org roles for users not yet in the dest org** | Roles can only be assigned to existing destination members. | Add the user first, then re-run `sync --apply`. |
| **SSO (SAML) configuration** | Requires DNS TXT verification + IdP-side setup; not automatable. | Recreate manually. |
| **Audit-log streaming, OTel exporter headers** | Point at source-owned infra / server-redacted. | Recreate against destination infra; re-add OTel headers manually. |
| **Usage data** | Local baseline only (`--include-usage`). | Not synced — retained as a source-org record. |
| **Runner instances & registration tokens** | Only resource-class *definitions* transfer. | Re-register runners against the destination resource classes. |
| **Cross-type settings (OAuth → App)** | No App equivalent for fork-PR builds, the OSS flag, or `pr_only_branch_overrides`; multiple App pipeline definitions cannot collapse into one OAuth config. | Reconfigure manually where an equivalent exists. |
| **OSS ("Free and Open Source") flag** | GitHub App projects auto-detect OSS from repo visibility (the PATCH endpoint rejects the `oss` field); OAuth private repos silently no-op. The sync makes a best-effort attempt and records a `manual` note when it cannot confirm the flag. | For GitHub App destinations: no action needed — OSS status is auto-detected from the public/private visibility of the GitHub repo. For OAuth destinations with a public repo: enable "Free and Open Source" in the destination project's **Advanced settings**. |

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
