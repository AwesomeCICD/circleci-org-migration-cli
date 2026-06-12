# Playbook: GitHub OAuth → GitHub OAuth migration

**Source:** `gh/<source-org>` — GitHub OAuth integration  
**Destination:** `gh/<dest-org>` — GitHub OAuth integration  
**Typical use:** org rename, GitHub org move within OAuth, or consolidation of
two GitHub OAuth CircleCI orgs.

> Throughout, `gh/acme` is the source and `gh/acme-new` is the destination.
> Substitute your own slugs.

---

## Rollback / abort

Nothing in the source org is ever modified or destroyed. Projects in the
destination are created **paused** — they will not build until you explicitly
enable them in Phase 7. To abort at any point before Phase 7: do nothing. The
source org keeps running normally. To clean up a partial destination, delete
contexts and unfollow projects in the destination org settings UI.

---

## Phase 0 — Prerequisites and identification

### 0.1 Confirm org slugs and types

Both orgs use the `gh/<name>` slug format. Confirm at
**app.circleci.com/settings/organization**.

```bash
# Source slug (example):  gh/acme
# Destination slug (example): gh/acme-new
```

The destination org must already exist as a CircleCI org (it is created
automatically when someone first logs in with the corresponding GitHub account).

### 0.2 API tokens

Create a **personal API token** for each side at
**User Settings → Personal API Tokens**. The token's user must be an
**organization admin** of the org it acts on.

```bash
export CIRCLECI_SOURCE_TOKEN="<source-org-admin-token>"
export CIRCLECI_DEST_TOKEN="<destination-org-admin-token>"
```

Token resolution order: `--source-token` / `--dest-token` flags override the
env vars; `--token` / `CIRCLECI_CLI_TOKEN` / `CIRCLE_TOKEN` is the fallback for
both sides when the specific vars are unset.

### 0.3 GitHub permissions

No GitHub token is required for same-GitHub-org OAuth migrations. If repositories
have moved to a **different GitHub organization**, you will also need a GitHub
PAT with `repo` read:

```bash
export GITHUB_TOKEN="<github-pat-with-repo-read>"
```

### 0.4 Install the CLI (v0.10.0+)

```bash
brew install AwesomeCICD/tap/circleci-migrate
circleci-migrate version
```

### 0.5 Prepare the mapping file

For any migration where the destination org name differs from the source, create
`mapping.json`. Without `org.to` the sync will target the source org itself (a
warning is printed):

```json
{
  "org": { "from": "gh/acme", "to": "gh/acme-new" }
}
```

If individual repos have also moved to a new GitHub org, add `github_org`:

```json
{
  "org": { "from": "gh/acme", "to": "gh/acme-new" },
  "github_org": { "from": "acme", "to": "acme-new" }
}
```

See [mapping.md](../mapping.md) for the full schema.

### 0.6 Self-hosted runners (optional)

If the source org uses self-hosted runner resource classes, identify the runner
namespace (there is no API lookup — check with your runners administrator or
look in the pipeline configs). You will need it in Phase 1.

---

### Phase 0 checklist

- [ ] Source slug confirmed: `gh/_______________`
- [ ] Destination slug confirmed: `gh/_______________`
- [ ] Source token set in `CIRCLECI_SOURCE_TOKEN`; user is org admin
- [ ] Destination token set in `CIRCLECI_DEST_TOKEN`; user is org admin
- [ ] Destination org already exists in CircleCI
- [ ] `mapping.json` created with correct `org.to`
- [ ] Runner namespace(s) noted (if applicable): `_______________`
- [ ] `circleci-migrate version` shows v0.10.0 or later

### ✅ Confirm ALL of the above before continuing to Phase 1

---

## Phase 1 — Export and review

Export is **read-only**. It never writes to CircleCI and is safe to re-run.

```bash
circleci-migrate export \
  --source-org gh/acme \
  --output manifest.json \
  --report migration-report.md
```

With self-hosted runner resource classes:

```bash
circleci-migrate export \
  --source-org gh/acme \
  --runner-namespace acme-runners \
  --output manifest.json \
  --report migration-report.md
```

With an optional usage snapshot (local baseline only — does NOT transfer):

```bash
circleci-migrate export \
  --source-org gh/acme \
  --include-usage \
  --output manifest.json \
  --report migration-report.md
```

### 1.1 Review the audit report

Open `migration-report.md` and check:

- Total contexts, total env vars per context
- Total projects and env vars per project
- Warnings (restricted contexts, manual follow-ups specific to your org)
- Any items pre-flagged as `manual` (these cannot be automated)

### 1.2 Expected counts

Note these for cross-checking after sync:

| Item | Count |
|---|---|
| Contexts | |
| Context env vars (total) | |
| Projects | |
| Project env vars (total) | |
| Webhooks | |
| Scheduled pipelines | |
| Runner resource classes | |

---

### Phase 1 checklist

- [ ] `manifest.json` written without errors
- [ ] `migration-report.md` reviewed in full
- [ ] Counts noted in the table above
- [ ] All warnings in the report understood and a plan made for each

### ✅ Report reviewed; counts match expectations — continue to Phase 2

---

## Phase 2 — Secrets capture

CircleCI never returns env-var **values** over its API, so capturing them
requires running inside a CircleCI pipeline. No config is committed to your
repository — `secrets capture` submits an inline (unversioned) pipeline config.

### Option A — `secrets capture` (recommended: local bundle)

**Interactive guided walkthrough (first-time use):**

```bash
circleci-migrate secrets capture
```

The guided flow prompts for: manifest path, which contexts/projects to capture,
host project for context extraction (any project works), encryption (strongly
recommended), storage, and artifact retention.

**Non-interactive (CI-safe):**

```bash
circleci-migrate secrets capture \
  --manifest manifest.json \
  --encrypt \
  --generate-key \
  --artifact-retention-days 1 \
  --enable-trigger \
  --output secrets.json
```

Encryption is on by default; `--generate-key` creates a fresh age keypair
automatically. The plaintext bundle `secrets.json` is written locally (mode
`0600`). Do not commit it.

Context extraction runs under a "host project" that you specify with
`--host-project`. Any project in the source org works:

```bash
circleci-migrate secrets capture \
  --manifest manifest.json \
  --host-project gh/acme/web \
  --encrypt \
  --generate-key \
  --artifact-retention-days 1 \
  --enable-trigger \
  --output secrets.json
```

### Option B — `secrets transfer` (zero-disk-write, context vars only)

Store the destination API token in a source-org context (e.g. `migration-secrets`)
with env var `CIRCLECI_DEST_TOKEN`, then:

```bash
# Dry run — safe, prints plan only:
circleci-migrate secrets transfer \
  --manifest manifest.json \
  --dest-org-id <dest-org-uuid> \
  --dest-token-context migration-secrets

# Execute (transfers directly; no bundle written to disk):
circleci-migrate secrets transfer \
  --manifest manifest.json \
  --dest-org-id <dest-org-uuid> \
  --dest-token-context migration-secrets \
  --enable-trigger \
  --apply
```

Destination contexts are auto-created if they do not yet exist. SSH keys still
require Option A (`secrets capture`).

### Restricted contexts

Contexts with restrictions are skipped by default
(`--skip-restricted-contexts`, on by default). To capture them either:
- `--remove-restrictions` — temporarily lift restrictions and restore after run
- Accept the gap; use `sync --missing-secrets placeholder` so the variable name
  exists in the destination and can be filled in manually after sync

### Security reminders

- `secrets.json` contains **plaintext** env-var values — protect it accordingly.
- Use `--artifact-retention-days 1` to minimise the in-CircleCI exposure window.
- **Rotate every captured value** after the destination is confirmed healthy
  (Phase 8).
- Use a private project for the capture pipeline.

---

### Phase 2 checklist

- [ ] Decision made: Option A (bundle) or Option B (transfer)
- [ ] If Option A: `secrets.json` written; encryption confirmed (or risk of plaintext artifact accepted)
- [ ] If Option B: destination token stored in source-org context; dry-run plan reviewed
- [ ] Restricted-context gap plan agreed (capture with `--remove-restrictions`, or `--missing-secrets placeholder` at sync)
- [ ] SSH keys plan made (capture via Option A, or manual re-upload after sync)
- [ ] `secrets.json` not committed to source control

### ✅ Secrets captured or transfer planned — continue to Phase 3

---

## Phase 3 — Choose management model

### Option A — CLI applies everything (default)

Skip ahead to Phase 4. No extra setup required.

### Option B — Terraform-managed destination

Use this path when the destination org should be managed as Terraform state for
ongoing IaC lifecycle.

**3B.1 — Generate Terraform configuration**

```bash
circleci-migrate terraform generate \
  --manifest manifest.json \
  --secrets secrets.json \
  --mapping mapping.json \
  --dest-org-id <dest-org-uuid> \
  --dest-org-type oauth \
  --out ./terraform/
```

Use `--placeholders` instead of `--secrets` to emit `REPLACE_ME` placeholder
values and a fill-in workbook (`SECRETS_WORKBOOK.md`):

```bash
circleci-migrate terraform generate \
  --manifest manifest.json \
  --placeholders \
  --mapping mapping.json \
  --dest-org-id <dest-org-uuid> \
  --dest-org-type oauth \
  --out ./terraform/
```

**3B.2 — Review and apply**

```bash
cd ./terraform/
export TF_VAR_circleci_api_token="<dest-org-api-token>"
terraform init
terraform plan    # review what will be created
terraform apply
```

**3B.3 — Review GAPS.md**

`./terraform/GAPS.md` lists every item Terraform does NOT manage (org settings,
CIAM, extras) with the exact `circleci-migrate` command to fill each gap.

**3B.4 — CLI gap-fill**

After `terraform apply`, use `--skip-terraform-managed` so the CLI does not
overwrite Terraform-owned resources:

```bash
circleci-migrate sync \
  --manifest manifest.json \
  --secrets secrets.json \
  --mapping mapping.json \
  --dest-token $CIRCLECI_DEST_TOKEN \
  --apply \
  --skip-terraform-managed
```

This syncs only org-settings, extras (checkout keys, SSH keys, schedules), and
anything else Terraform does not manage. Then continue to Phase 6 (skip Phase
4 and 5 for the sections Terraform already handled).

---

### Phase 3 checklist

- [ ] Management model decided: (A) CLI-only  OR  (B) Terraform-managed
- [ ] If B: `terraform generate` run; GAPS.md reviewed; `terraform apply` succeeded
- [ ] If B: `sync --skip-terraform-managed --apply` run successfully

### ✅ Management model established — continue to Phase 4

---

## Phase 4 — Dry-run sync

Review the plan before writing anything. No `--apply` means nothing is created.

```bash
circleci-migrate sync \
  --manifest manifest.json \
  --secrets secrets.json \
  --mapping mapping.json
```

With self-hosted runners:

```bash
circleci-migrate sync \
  --manifest manifest.json \
  --secrets secrets.json \
  --mapping mapping.json \
  --dest-runner-namespace acme-new-runners
```

### 4.1 Reading the plan output

Each resource line shows one of:

| Status | Meaning |
|---|---|
| `created (would create)` | Does not exist in destination; will be created on `--apply` |
| `set (would set)` | Value will be written |
| `exists` | Already exists with the same name; will be reused |
| `manual` | Cannot be automated (e.g. project-type context restrictions, secret values without a bundle) |
| `error` | Problem resolving this resource — investigate before applying |

### 4.2 Common "Needs attention" items

- **`manual` on context restrictions:** project-type restrictions use source-org
  project UUIDs that do not transfer. Recreate them in the destination org UI
  after sync. See [troubleshooting](../troubleshooting.md#context-restriction-shows-manual).
- **`manual` on env vars:** no secrets bundle supplied, or restricted context
  skipped. Supply `--secrets secrets.json` or use `--missing-secrets placeholder`.
- **`error` on project creation:** repositories must be connected to the
  destination GitHub App (not applicable for OAuth→OAuth migrations).
- **Missing projects:** a project in the manifest is not present in the dry-run
  output if the destination org cannot yet follow it — check GitHub access.

---

### Phase 4 checklist

- [ ] Dry-run completed without unexpected `error` lines
- [ ] All `manual` items reviewed and a plan made for each
- [ ] Context counts in the plan match Phase 1 notes
- [ ] Project counts in the plan match Phase 1 notes
- [ ] No unexpected org-settings `error` lines

### ✅ Dry-run plan reviewed; no unexpected errors — continue to Phase 5

---

## Phase 5 — Apply

Apply creates resources in the destination. Projects are created **paused** —
no webhook is installed and no builds run until Phase 7.

```bash
circleci-migrate sync \
  --manifest manifest.json \
  --secrets secrets.json \
  --mapping mapping.json \
  --apply
```

With self-hosted runners:

```bash
circleci-migrate sync \
  --manifest manifest.json \
  --secrets secrets.json \
  --mapping mapping.json \
  --dest-runner-namespace acme-new-runners \
  --apply
```

With `--missing-secrets placeholder` (creates env var names with placeholder
values where no captured value exists):

```bash
circleci-migrate sync \
  --manifest manifest.json \
  --secrets secrets.json \
  --mapping mapping.json \
  --missing-secrets placeholder \
  --apply
```

When sync finishes it will prompt:

```
Enable builds for N project(s) now? [y/N]:
```

Answer **N** here — enable builds in Phase 7 after validation. To skip the
prompt entirely (e.g. in a non-TTY context), omit `--yes` so it auto-skips.

### 5.1 Verify resource counts

After apply, re-run a **dry run** against the destination to confirm what was
created:

```bash
circleci-migrate sync \
  --manifest manifest.json \
  --secrets secrets.json \
  --mapping mapping.json
```

All previously-`would create` lines should now show `exists`.

---

### Phase 5 checklist

- [ ] `sync --apply` completed without errors
- [ ] Context count in destination matches Phase 1 notes
- [ ] Project count in destination matches Phase 1 notes
- [ ] Builds NOT yet enabled (projects are paused)
- [ ] Second dry-run shows `exists` for created resources

### ✅ Resources created; builds not yet enabled — continue to Phase 6

---

## Phase 6 — Validate destination

Before enabling builds, verify the destination is correct.

### 6.1 Contexts and env vars

In the CircleCI UI for the destination org (**Organization Settings →
Contexts**), confirm:

- All expected contexts are present
- Each context shows the expected number of environment variables
- Context restrictions (expression/group restrictions) are correct

### 6.2 Project settings

For a sample of projects in the destination org, verify in the UI
(**Project Settings → Environment Variables**):

- Env var names are present
- Project settings (e.g. advanced settings) match expectations

### 6.3 Webhooks and schedules

- **Organization Settings → Webhooks**: org-level webhooks present
- **Project Settings → Webhooks**: project-level webhooks present
- **Project Settings → Triggers** (if using scheduled pipelines): schedule names present

### 6.4 Cross-check via fresh export of destination (recommended)

Export the destination and diff against the source manifest:

```bash
circleci-migrate export \
  --source-org gh/acme-new \
  --source-token $CIRCLECI_DEST_TOKEN \
  --output manifest-dest.json \
  --report report-dest.md
```

Compare context names and project names between `manifest.json` and
`manifest-dest.json`. Counts should match (minus any items that were intentionally
skipped or are manual).

### 6.5 Manual items

Work through the `manual` items from the dry-run and the `migration-report.md`:

- Project-type context restrictions — recreate in the destination org UI
- Webhook signing secrets — regenerate and update receivers
- OTel exporter headers — re-add in org settings
- Per-project CIAM grants — recreate on each destination project (n/a for OAuth)
- Org orbs — republish in the destination namespace (if applicable)
- See the canonical list: [cutover-runbook.md — Section 4](../cutover-runbook.md#4-does-not-transfer--data-loss)

---

### Phase 6 checklist

- [ ] All contexts present in destination with correct env-var counts
- [ ] Sample of project env vars validated in UI
- [ ] Org-level settings match expectations (OIDC claims, feature flags, retention)
- [ ] Webhooks present in destination
- [ ] Scheduled pipelines present (if any)
- [ ] All `manual` items from Phase 4 addressed
- [ ] Fresh destination export reviewed (optional but recommended)

### ✅ Destination validated — continue to Phase 7

---

## Phase 7 — Enable builds / cutover

When you are confident the destination is correct, enable builds.

### 7.1 Enable builds via sync

```bash
circleci-migrate sync \
  --manifest manifest.json \
  --secrets secrets.json \
  --mapping mapping.json \
  --apply \
  --yes
```

`--yes` auto-confirms the "Enable builds for N projects?" prompt. For GitHub
OAuth projects, this **follows** each project — which installs the deploy key
and webhook on GitHub and may trigger an initial build.

### 7.2 Verify initial builds

In the destination org (**Projects** page), confirm:

- Projects show as "following" (the webhook is installed)
- A pipeline run appears (or confirm a push triggers one)
- The pipeline succeeds (or any failures are understood and expected)

### 7.3 Smoke pipeline

Trigger a manual pipeline on at least one representative project to confirm
end-to-end functionality:

- Push a commit to a branch
- Confirm the webhook fires and a pipeline starts
- Confirm the pipeline can access context env vars (check a job that uses a
  context)

---

### Phase 7 checklist

- [ ] `sync --apply --yes` run (or builds enabled via UI)
- [ ] Projects show as followed in the destination org
- [ ] At least one pipeline ran successfully in the destination
- [ ] Context env vars accessible from within a job
- [ ] No unexpected build failures

### ✅ A real pipeline ran green on the destination — continue to Phase 8

---

## Phase 8 — Post-cutover

### 8.1 Rotate all captured secrets

Every env-var value captured during Phase 2 should be considered potentially
exposed (it was written to a file and possibly to a CircleCI artifact). Rotate
each value by:

1. Generating a new value in the upstream system (database password, API key, etc.)
2. Updating the value in the destination org context or project env var
3. Confirming destination builds still pass

### 8.2 Clean up capture artifacts

- Delete `secrets.json` from your local machine
- If using artifact storage: the retention period (recommended: 1 day) should
  have expired; verify at **Job → Artifacts** for the capture pipeline run
- Delete `migration-identity.age` and `migration-recipient.txt` if generated

### 8.3 Update external pins

Repoint everything that references the old org. See the full list in
[cutover-runbook.md — Section 5](../cutover-runbook.md#5-update-external-pins):

- Service catalogs (Backstage, internal portals)
- Slack and notification integrations
- Dashboards, status badges, Insights links
- Branch-protection / required-status-check integrations on GitHub
- READMEs and documentation

### 8.4 Decommission the source org

When you are confident the destination is fully healthy:

1. Disable or archive the source-org CircleCI projects (unfollow them to
   remove webhooks)
2. Communicate the migration to your team
3. Monitor the destination for any missed projects or configurations

### 8.5 What does NOT transfer

For the canonical reference of items that require manual follow-up, see:
[cutover-runbook.md — Section 4](../cutover-runbook.md#4-does-not-transfer--data-loss).

---

### Phase 8 checklist

- [ ] Every captured secret value rotated
- [ ] `secrets.json` deleted from local machine
- [ ] CircleCI capture artifacts expired or deleted
- [ ] Service catalog / Backstage entries updated
- [ ] GitHub branch-protection status checks updated to new org
- [ ] Slack / notification integrations updated
- [ ] Status badges updated
- [ ] Source org projects disabled / decommissioned when ready
- [ ] Team notified of the new org

---

## See also

- [Migration guide — OAuth → OAuth scenario](../guide.md#7a-oauth--oauth)
- [Cutover runbook](../cutover-runbook.md)
- [mapping.json reference](../mapping.md)
- [Troubleshooting](../troubleshooting.md)
- [CLI reference](../cli/README.md)
