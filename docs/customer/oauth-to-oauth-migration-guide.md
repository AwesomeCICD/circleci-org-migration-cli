---
title: "Migrating a CircleCI Organization (GitHub OAuth → GitHub OAuth)"
subtitle: "A step-by-step runbook with checklists and validation gates"
author: "CircleCI"
date: "2026"
---

# Migrating a CircleCI Organization

## GitHub OAuth → GitHub OAuth

**Use this guide when** you are moving CircleCI configuration from one GitHub
OAuth organization to another — for example, a GitHub org rename, moving to a
new GitHub org, or consolidating two CircleCI orgs. Both organizations use the
`gh/<name>` slug format.

**Tooling:** the `circleci-migrate` CLI.

Throughout, the **source** is `gh/acme` and the **destination** is
`gh/acme-new`. Substitute your own slugs.

> **Safety first.** Nothing in the source org is ever modified. Destination
> projects are created **paused** — they install no webhook and run no builds
> until you explicitly enable them in Phase 6. You can abort any time before
> then by simply stopping; the source keeps running normally.

---

## What transfers

| Transfers automatically | Requires a manual step |
|---|---|
| Contexts + context env vars | Context restrictions (recreate in UI) |
| Project env vars | SSH / checkout keys (re-add or use capture) |
| Project settings | Webhook signing secrets (regenerate) |
| Org-level webhooks | Org orbs (republish in destination) |
| Scheduled pipelines | Per-project access grants |
| Self-hosted runner resource classes | |

A complete "does not transfer" list is printed in your migration report
(Phase 1).

---

## Phase 0 — Prerequisites

- [ ] Source slug confirmed: `gh/________` (Org Settings → Overview)
- [ ] Destination slug confirmed: `gh/________`, and the destination org
      **already exists** in CircleCI
- [ ] **Personal API tokens** for both orgs; each token's user is an
      **org admin** of that org
- [ ] CLI installed and current:

```bash
brew install AwesomeCICD/tap/circleci-migrate
circleci-migrate version    # v0.12.0 or later
```

Set tokens for the session:

```bash
export CIRCLECI_SOURCE_TOKEN="<source-org-admin-token>"
export CIRCLECI_DEST_TOKEN="<destination-org-admin-token>"
```

If repositories also moved to a **different GitHub org**, also set a GitHub PAT
with `repo` read: `export GITHUB_TOKEN="<pat>"`.

**Preflight check** (validates tokens, reachability, and common issues before
you start):

```bash
circleci-migrate doctor --source-org gh/acme --dest-org gh/acme-new
```

### ✅ Gate: all Phase 0 boxes checked, `doctor` reports no blockers.

---

## Phase 1 — Export and review (read-only)

Export never writes to CircleCI. It produces a manifest and a human-readable
audit report.

```bash
circleci-migrate export \
  --source-org gh/acme \
  --output manifest.json \
  --report migration-report.md
```

If the source uses self-hosted runners, add `--runner-namespace <namespace>`.

Open `migration-report.md` and record your baseline counts:

| Item | Count |
|---|---|
| Contexts | |
| Context env vars (total) | |
| Projects | |
| Project env vars (total) | |
| Webhooks | |
| Scheduled pipelines | |

> **Tip — follow all projects:** if some source projects were never set up in
> CircleCI, they won't appear. The export will flag this and you can run
> `export --follow-all` (with a GitHub token) to onboard every repo first.

### ✅ Gate: report reviewed in full; counts recorded; every warning understood.

---

## Phase 2 — Move secrets

CircleCI never returns secret **values** via API, so values are moved from
*inside* a pipeline. Choose one approach:

### Recommended: direct transfer (no file on disk)

Best for most OAuth → OAuth moves. Values go straight from source to
destination over TLS; nothing is written to disk. **See the companion
*Transferring Secrets Between CircleCI Organizations* guide** for the full
walkthrough. In brief:

```bash
# Store the destination token in a SOURCE-org context named "migration-secrets"
# (variable: CIRCLECI_DEST_TOKEN), then:

# Generate the project mapping (matches projects by repo name):
circleci-migrate mapping generate --manifest manifest.json --dest-org gh/acme-new -o mapping.json

# Dry-run, then apply:
circleci-migrate secrets transfer --manifest manifest.json \
  --dest-org-id <dest-org-uuid> --dest-token-context migration-secrets \
  --mapping mapping.json --include-project-vars
circleci-migrate secrets transfer --manifest manifest.json \
  --dest-org-id <dest-org-uuid> --dest-token-context migration-secrets \
  --mapping mapping.json --include-project-vars --enable-trigger --apply
```

### Alternative: capture to an encrypted bundle

Use this if you need a reviewable local copy, are migrating SSH keys, or want to
inspect values first. Produces an encrypted `secrets.json` you then feed to
`sync`.

```bash
circleci-migrate secrets capture \
  --manifest manifest.json \
  --encrypt --generate-key \
  --artifact-retention-days 1 \
  --enable-trigger \
  --output secrets.json
```

> **Security:** `secrets.json` holds plaintext values — keep it `0600`, never
> commit it, and delete it after cutover. Keep artifact retention at 1 day.

### ✅ Gate: secrets transferred (direct) **or** `secrets.json` captured; restricted-context and SSH-key gaps noted.

---

## Phase 3 — Prepare the mapping

If you used direct transfer, you already created `mapping.json` in Phase 2. If
not, create it now so `sync` targets the right destination:

```json
{
  "org": { "from": "gh/acme", "to": "gh/acme-new" }
}
```

If repos also moved GitHub orgs, add:

```json
{
  "org": { "from": "gh/acme", "to": "gh/acme-new" },
  "github_org": { "from": "acme", "to": "acme-new" }
}
```

### ✅ Gate: `mapping.json` has the correct destination org.

---

## Phase 4 — Dry-run sync

Preview every change. Without `--apply`, nothing is written.

```bash
circleci-migrate sync \
  --manifest manifest.json \
  --secrets secrets.json \
  --mapping mapping.json
```

(Omit `--secrets secrets.json` if you used direct transfer in Phase 2 — the
values are already in the destination.)

Each line shows a status:

| Status | Meaning |
|---|---|
| `would create` | Will be created on apply |
| `would set` | Value will be written |
| `exists` | Already present; reused |
| `manual` | Cannot be automated — handle in the UI |
| `error` | Investigate before applying |

### ✅ Gate: no unexpected `error` lines; every `manual` item has a plan; counts match Phase 1.

---

## Phase 5 — Apply (projects created paused)

```bash
circleci-migrate sync \
  --manifest manifest.json \
  --secrets secrets.json \
  --mapping mapping.json \
  --apply
```

When prompted **"Enable builds for N projects now?"**, answer **N** — you'll
enable them in Phase 6 after validation.

Re-run the dry-run command to confirm previously-`would create` lines now show
`exists`.

### ✅ Gate: apply completed without errors; destination counts match; builds NOT yet enabled.

---

## Phase 6 — Validate, then enable builds

### 6.1 Validate (before enabling anything)

In the destination org UI, confirm:

- **Contexts** — all present, correct variable counts, restrictions correct.
- **Project env vars** — names present for a sample of projects.
- **Webhooks / Triggers** — present as expected.
- Work through every `manual` item from Phase 4 and the migration report
  (e.g. recreate context restrictions, regenerate webhook signing secrets).

Optional cross-check:

```bash
circleci-migrate export --source-org gh/acme-new \
  --source-token "$CIRCLECI_DEST_TOKEN" --output manifest-dest.json
```

### 6.2 Enable builds (cutover)

```bash
circleci-migrate sync \
  --manifest manifest.json \
  --secrets secrets.json \
  --mapping mapping.json \
  --apply --yes
```

`--yes` confirms enabling builds. For OAuth projects this **follows** each
project — installing the GitHub webhook and possibly triggering an initial
build.

Then verify in the destination: projects show as *following*, at least one
pipeline runs green, and a job can read its context variables.

### ✅ Gate: a real pipeline ran green on the destination; context vars accessible from a job.

---

## Phase 7 — Post-cutover

- [ ] **Rotate** every secret value that was captured to a file (Phase 2,
      capture path). Direct-transfer values were never on disk, but rotate the
      destination API token used for the transfer.
- [ ] Delete any local `secrets.json` and generated key files.
- [ ] Repoint external references to the new org: status badges, Slack/
      notifications, dashboards, Backstage/service catalog, and **GitHub
      branch-protection required status checks**.
- [ ] Decommission the source org when confident: unfollow its projects (removes
      webhooks), archive, and notify your team.

### ✅ Gate: secrets rotated; external pins updated; team notified.

---

## Troubleshooting

| Symptom | Fix |
|---|---|
| `manual` on context restrictions | Source-org project UUIDs don't transfer; recreate the restriction in the destination UI. |
| `manual` on env vars | Supply `--secrets secrets.json`, or use `--missing-secrets placeholder` to create the names for manual fill-in. |
| Project missing from the plan | Destination can't follow it yet — check GitHub access / onboarding. |
| Pipeline skipped during secrets transfer | Enable "unversioned config" in the source org (or pass `--enable-trigger`). |
| Builds didn't start after enabling | Confirm the project is *following* and the webhook installed on GitHub. |

---

## Command quick reference

```bash
circleci-migrate doctor   --source-org gh/acme --dest-org gh/acme-new
circleci-migrate export   --source-org gh/acme --output manifest.json --report migration-report.md
circleci-migrate mapping generate --manifest manifest.json --dest-org gh/acme-new -o mapping.json
# secrets: see the Secrets Transfer guide
circleci-migrate sync --manifest manifest.json --mapping mapping.json                 # dry run
circleci-migrate sync --manifest manifest.json --mapping mapping.json --apply         # create (paused)
circleci-migrate sync --manifest manifest.json --mapping mapping.json --apply --yes   # enable builds
```

---

*© CircleCI. Generated for customer use. Commands assume `circleci-migrate`
v0.12.0 or later. Nothing in this runbook modifies the source organization.*
