# Playbook: GitHub App / standalone → standalone migration

**Source:** `circleci/<src-uuid>` — GitHub App or standalone org  
**Destination:** `circleci/<dst-uuid>` — GitHub App or standalone org  
**Typical use:** GitHub App org consolidation, standalone org migration, EMU
GitHub-org moves (add `--dest-github-org` when repos change GitHub org).

> Throughout, `SRC_UUID=11111111-1111-1111-1111-111111111111` and
> `DST_UUID=22222222-2222-2222-2222-222222222222` are used as placeholders.
> Substitute your own org UUIDs.

---

## Rollback / abort

Nothing in the source org is ever modified or destroyed. Projects in the
destination are created with triggers **disabled** — they will not build until
you explicitly enable them in Phase 7. To abort at any point before Phase 7:
do nothing. The source org keeps running normally. To clean up a partial
destination, delete contexts and projects in the destination org settings UI.

---

## Phase 0 — Prerequisites and identification

### 0.1 Confirm org slugs and types

Both orgs use the `circleci/<uuid>` slug format. Find the UUID at:

```
https://app.circleci.com/settings/organization/circleci/<uuid>/overview
```

or in the URL when you navigate to the org in the CircleCI web UI.

```bash
# Source slug (example):  circleci/11111111-1111-1111-1111-111111111111
# Destination slug (example): circleci/22222222-2222-2222-2222-222222222222

SRC_UUID="11111111-1111-1111-1111-111111111111"
DST_UUID="22222222-2222-2222-2222-222222222222"
```

### 0.2 API tokens

Create a **personal API token** for each side at
**User Settings → Personal API Tokens**. The token's user must be an
**organization admin** of the org it acts on.

```bash
export CIRCLECI_SOURCE_TOKEN="<source-org-admin-token>"
export CIRCLECI_DEST_TOKEN="<destination-org-admin-token>"
```

Token resolution order: `--source-token` / `--dest-token` flags override the
env vars; `--token` / `CIRCLECI_CLI_TOKEN` / `CIRCLE_TOKEN` is the fallback.

### 0.3 GitHub App repo connections (required before Phase 5)

For GitHub App destinations, every repository must be connected to the
destination CircleCI GitHub App **before** `sync --apply` attempts to create
projects. Repos not yet connected are skipped with a warning at apply time.

Verify in your destination org's GitHub App installation settings that all
expected repositories are connected.

### 0.4 GitHub token (only for cross-GitHub-org repo moves)

If repositories have moved to a **different GitHub organization** (e.g. an EMU
migration), supply a GitHub PAT with `repo` read so the tool resolves the new
numeric repository IDs:

```bash
export GITHUB_TOKEN="<github-pat-with-repo-read>"
```

This is not needed for same-GitHub-org migrations.

### 0.5 CIAM (for standalone orgs with org roles)

Standalone `circleci/`-type orgs carry **CIAM org roles and groups**. These sync
automatically. User role grants are applied only for users who already exist in
the destination org (matched by email) — add missing users to the destination
org first if you need their roles migrated.

### 0.6 Prepare the mapping file

Create `mapping.json` with the destination org UUID:

```json
{
  "org": {
    "from": "circleci/11111111-1111-1111-1111-111111111111",
    "to": "circleci/22222222-2222-2222-2222-222222222222"
  }
}
```

For repos that moved to a new GitHub org, add `github_org`:

```json
{
  "org": {
    "from": "circleci/11111111-1111-1111-1111-111111111111",
    "to": "circleci/22222222-2222-2222-2222-222222222222"
  },
  "github_org": { "from": "acme", "to": "acme-new" }
}
```

See [mapping.md](../mapping.md) for the full schema.

### 0.7 Self-hosted runners (optional)

If the source org uses self-hosted runner resource classes, identify the runner
namespace. There is no API lookup — check with your runners administrator or
look in pipeline configs.

### 0.8 Install the CLI (v0.10.0+)

```bash
brew install AwesomeCICD/tap/circleci-migrate
circleci-migrate version
```

---

### Phase 0 checklist

- [ ] Source slug confirmed: `circleci/_______________`
- [ ] Destination slug confirmed: `circleci/_______________`
- [ ] Source token set in `CIRCLECI_SOURCE_TOKEN`; user is org admin
- [ ] Destination token set in `CIRCLECI_DEST_TOKEN`; user is org admin
- [ ] Destination org already exists in CircleCI
- [ ] GitHub App repo connections verified in destination org
- [ ] `mapping.json` created with correct `org.to` (destination UUID)
- [ ] CIAM: any users who need org roles already exist in the destination org
- [ ] Runner namespace(s) noted (if applicable): `_______________`
- [ ] `circleci-migrate version` shows v0.10.0 or later

### ✅ Confirm ALL of the above before continuing to Phase 1

---

## Phase 1 — Export and review

Export is **read-only**. It never writes to CircleCI and is safe to re-run.

```bash
circleci-migrate export \
  --source-org "circleci/$SRC_UUID" \
  --output manifest.json \
  --report migration-report.md
```

With self-hosted runner resource classes:

```bash
circleci-migrate export \
  --source-org "circleci/$SRC_UUID" \
  --runner-namespace acme-runners \
  --output manifest.json \
  --report migration-report.md
```

With an optional usage snapshot (local baseline only — does NOT transfer):

```bash
circleci-migrate export \
  --source-org "circleci/$SRC_UUID" \
  --include-usage \
  --output manifest.json \
  --report migration-report.md
```

### 1.1 Review the audit report

Open `migration-report.md` and check:

- Total contexts, total env vars per context
- Total projects and their pipeline definitions / trigger counts
- CIAM groups and org roles present
- Runner resource classes (if applicable)
- Warnings (restricted contexts, manual follow-ups)

### 1.2 Expected counts

| Item | Count |
|---|---|
| Contexts | |
| Context env vars (total) | |
| Projects | |
| Project env vars (total) | |
| Pipeline definitions | |
| Triggers | |
| CIAM groups | |
| Runner resource classes | |
| Webhooks | |

---

### Phase 1 checklist

- [ ] `manifest.json` written without errors
- [ ] `migration-report.md` reviewed in full
- [ ] Counts noted in the table above
- [ ] All warnings in the report understood and a plan made for each

### ✅ Report reviewed; counts match expectations — continue to Phase 2

---

## Phase 2 — Secrets capture

CircleCI never returns env-var **values** over its API. `secrets capture` submits
an inline (unversioned) pipeline config — no config is committed to your repo.

### Option A — `secrets capture` (recommended: local bundle)

**Interactive guided walkthrough (first-time use):**

```bash
circleci-migrate secrets capture
```

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

For context extraction, specify the host project explicitly (any project in the
source org):

```bash
circleci-migrate secrets capture \
  --manifest manifest.json \
  --host-project "circleci/$SRC_UUID/web" \
  --encrypt \
  --generate-key \
  --artifact-retention-days 1 \
  --enable-trigger \
  --output secrets.json
```

SSH key extraction is on by default. Pass `--no-ssh-keys` to skip it (env-var
capture only):

```bash
circleci-migrate secrets capture \
  --manifest manifest.json \
  --no-ssh-keys \
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
  --dest-org-id "$DST_UUID" \
  --dest-token-context migration-secrets

# Execute the transfer:
circleci-migrate secrets transfer \
  --manifest manifest.json \
  --dest-org-id "$DST_UUID" \
  --dest-token-context migration-secrets \
  --enable-trigger \
  --apply

# Also transfer project env vars (destination projects must already exist):
circleci-migrate secrets transfer \
  --manifest manifest.json \
  --dest-org-id "$DST_UUID" \
  --dest-token-context migration-secrets \
  --mapping mapping.json \
  --include-project-vars \
  --apply
```

No plaintext ever touches disk or build artifacts with this option. SSH keys
still require Option A.

### Restricted contexts

Contexts with restrictions are skipped by default. Options:
- `--remove-restrictions` — temporarily lift restrictions for the capture run
- Accept the gap; use `sync --missing-secrets placeholder` so the variable name
  exists in the destination for manual fill-in

### Security reminders

- `secrets.json` contains **plaintext** values — protect it accordingly.
- Use `--artifact-retention-days 1` to minimise exposure.
- **Rotate every captured value** after the destination is confirmed healthy
  (Phase 8).

---

### Phase 2 checklist

- [ ] Decision made: Option A (bundle) or Option B (transfer)
- [ ] If Option A: `secrets.json` written; encryption confirmed
- [ ] If Option B: dry-run plan reviewed; destination token in source context
- [ ] Restricted-context gap plan agreed
- [ ] SSH keys plan made (capture via Option A, or manual re-upload after sync)
- [ ] `secrets.json` not committed to source control

### ✅ Secrets captured or transfer planned — continue to Phase 3

---

## Phase 3 — Choose management model

### Option A — CLI applies everything (default)

Skip ahead to Phase 4. No extra setup required.

### Option B — Terraform-managed destination

Use this when the destination org should be managed as Terraform state. The
standalone (`circleci/`) org type supports the full Terraform surface including
`circleci_pipeline` and `circleci_trigger` resources.

**3B.1 — Generate Terraform configuration**

```bash
circleci-migrate terraform generate \
  --manifest manifest.json \
  --secrets secrets.json \
  --mapping mapping.json \
  --dest-org-id "$DST_UUID" \
  --dest-org-type standalone \
  --out ./terraform/
```

With self-hosted runner namespace:

```bash
circleci-migrate terraform generate \
  --manifest manifest.json \
  --secrets secrets.json \
  --mapping mapping.json \
  --dest-org-id "$DST_UUID" \
  --dest-org-type standalone \
  --dest-runner-namespace acme-new-runners \
  --out ./terraform/
```

Use `--placeholders` if you do not yet have a secrets bundle:

```bash
circleci-migrate terraform generate \
  --manifest manifest.json \
  --placeholders \
  --mapping mapping.json \
  --dest-org-id "$DST_UUID" \
  --dest-org-type standalone \
  --out ./terraform/
```

The generator emits:

| File | Contents |
|---|---|
| `versions.tf` | Provider version constraint (`~> 0.3`) |
| `providers.tf` | Provider block |
| `contexts.tf` | `circleci_context` + env-var resources |
| `restrictions.tf` | `circleci_context_restriction` resources |
| `projects.tf` | `circleci_project` + env-var resources (with advanced settings) |
| `webhooks.tf` | `circleci_webhook` resources |
| `runners.tf` | `circleci_runner_resource_class` + `circleci_runner_token` |
| `pipelines.tf` | `circleci_pipeline` + `circleci_trigger` (standalone only) |
| `GAPS.md` | CLI commands for items Terraform does not manage |

**3B.2 — Review and apply**

```bash
cd ./terraform/
export TF_VAR_circleci_api_token="<dest-org-api-token>"
terraform init
terraform plan
terraform apply
```

**3B.3 — Review GAPS.md**

`GAPS.md` lists org settings, CIAM, legacy schedules, checkout keys, SSH keys,
project API tokens, and any other gaps with the exact CLI command for each.

**3B.4 — CLI gap-fill**

```bash
circleci-migrate sync \
  --manifest manifest.json \
  --secrets secrets.json \
  --mapping mapping.json \
  --dest-token $CIRCLECI_DEST_TOKEN \
  --apply \
  --skip-terraform-managed
```

Or use `--only` to sync specific sections:

```bash
circleci-migrate sync \
  --manifest manifest.json \
  --dest-token $CIRCLECI_DEST_TOKEN \
  --apply \
  --only org-settings,ciam,extras
```

---

### Phase 3 checklist

- [ ] Management model decided: (A) CLI-only  OR  (B) Terraform-managed
- [ ] If B: `terraform generate` run with `--dest-org-type standalone`
- [ ] If B: `GAPS.md` reviewed; `terraform apply` succeeded
- [ ] If B: `sync --skip-terraform-managed --apply` run successfully

### ✅ Management model established — continue to Phase 4

---

## Phase 4 — Dry-run sync

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

| Status | Meaning |
|---|---|
| `created (would create)` | Will be created on `--apply` |
| `set (would set)` | Value will be written |
| `exists` | Already exists; will be reused |
| `manual` | Cannot be automated (project-type restrictions, per-project CIAM grants, etc.) |
| `error` | Problem — investigate before applying |

### 4.2 Common "Needs attention" items for standalone orgs

- **`manual` on context project restrictions:** source-org project UUIDs do not
  transfer. Recreate in the destination org UI after sync.
- **`manual` on per-project CIAM grants:** destination project UUID not reliably
  mappable — recreate user/group grants on the destination project manually.
- **`error` on project creation:** repository not yet connected to the destination
  GitHub App. Connect the repo, then re-run.
- **CIAM users not yet in destination:** add the user to the destination org,
  then re-run `sync --apply`. User role grants are applied only for existing members.

---

### Phase 4 checklist

- [ ] Dry-run completed without unexpected `error` lines
- [ ] All `manual` items reviewed; plan made for each
- [ ] Context + project counts match Phase 1 notes
- [ ] Pipeline definitions and triggers present in the plan
- [ ] CIAM groups and org roles visible in the plan

### ✅ Dry-run plan reviewed; no unexpected errors — continue to Phase 5

---

## Phase 5 — Apply

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

With cross-GitHub-org repo move (`--github-token` + `--dest-github-org`):

```bash
circleci-migrate sync \
  --manifest manifest.json \
  --secrets secrets.json \
  --mapping mapping.json \
  --github-token $GITHUB_TOKEN \
  --dest-github-org acme-new \
  --apply
```

App projects are created with pipeline triggers **disabled**. Answer **N** at
the "Enable builds?" prompt — enable builds in Phase 7 after validation.

### 5.1 CIAM output

The sync report will show CIAM groups recreated and org-role grants applied.
Users not yet in the destination are listed as requiring manual follow-up.

### 5.2 Verify resource counts

After apply, confirm in the CircleCI UI or run a second dry-run:

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
- [ ] Pipeline definitions and triggers present in destination
- [ ] CIAM groups and org roles applied (check report for unmatched users)
- [ ] Runner resource classes present (if applicable)
- [ ] Builds NOT yet enabled (triggers disabled)

### ✅ Resources created; builds not yet enabled — continue to Phase 6

---

## Phase 6 — Validate destination

### 6.1 Contexts and env vars

In the CircleCI UI for the destination org (**Organization Settings → Contexts**):

- All contexts present with correct env-var counts
- Context restrictions (expression restrictions) applied correctly
- Group restrictions applied (if any CIAM groups exist)

### 6.2 Project pipeline definitions and triggers

For a sample of projects in the destination org (**Project Settings → Pipelines**
or **Triggers**):

- Pipeline definition present
- Trigger present (in disabled state)
- Advanced settings (auto-cancel, setup workflows, etc.) match source

### 6.3 CIAM roles and groups (standalone orgs)

In the CircleCI UI (**Organization Settings → Groups** or **Members**):

- CIAM groups recreated with correct membership
- Org-level role grants applied for existing users
- Note any unmatched users from the sync report

### 6.4 Runner resource classes (if applicable)

In the CircleCI UI or runner dashboard, confirm resource classes exist in the
destination namespace.

### 6.5 Cross-check via fresh export of destination

```bash
circleci-migrate export \
  --source-org "circleci/$DST_UUID" \
  --source-token $CIRCLECI_DEST_TOKEN \
  --output manifest-dest.json \
  --report report-dest.md
```

Compare context names, project counts, and pipeline definitions between
`manifest.json` and `manifest-dest.json`.

### 6.6 Manual items

Work through the `manual` items from the dry-run and `migration-report.md`:

- Project-type context restrictions — recreate in destination org UI
- Per-project CIAM grants — recreate on destination projects
- Webhook signing secrets — regenerate and update receivers
- OTel exporter headers — re-add in org settings
- Org orbs — republish in destination namespace
- See: [cutover-runbook.md — Section 4](../cutover-runbook.md#4-does-not-transfer--data-loss)

---

### Phase 6 checklist

- [ ] All contexts present with correct env-var counts
- [ ] Pipeline definitions and triggers present per project
- [ ] CIAM groups and org roles verified in UI
- [ ] Advanced project settings match source
- [ ] Runner resource classes verified (if applicable)
- [ ] All `manual` items from Phase 4 addressed
- [ ] Fresh destination export reviewed (optional but recommended)

### ✅ Destination validated — continue to Phase 7

---

## Phase 7 — Enable builds / cutover

When satisfied the destination is correct, enable builds.

### 7.1 Enable builds via sync

```bash
circleci-migrate sync \
  --manifest manifest.json \
  --secrets secrets.json \
  --mapping mapping.json \
  --apply \
  --yes
```

`--yes` auto-confirms the "Enable builds?" prompt. For GitHub App orgs, this
**unpauses the pipeline triggers** — future pushes to the configured branches
will trigger pipelines.

### 7.2 Verify triggers are active

In the CircleCI UI for the destination org:
- **Project Settings → Triggers**: triggers show as enabled/active
- Confirm a pipeline is triggered on the next push or by manual trigger

### 7.3 Smoke pipeline

Trigger a pipeline manually or push a commit:

```bash
# Or use the CircleCI UI: Project → Trigger Pipeline
```

Confirm:
- Pipeline starts and progresses through jobs
- Context env vars are injected into jobs that reference contexts
- No unexpected permission or authentication failures

---

### Phase 7 checklist

- [ ] `sync --apply --yes` run (or triggers enabled via UI)
- [ ] Pipeline triggers show as active in destination
- [ ] At least one pipeline ran successfully in the destination
- [ ] Context env vars accessible from within a job
- [ ] CIAM-restricted contexts accessible by users in the correct groups
- [ ] No unexpected build failures

### ✅ A real pipeline ran green on the destination — continue to Phase 8

---

## Phase 8 — Post-cutover

### 8.1 Rotate all captured secrets

Rotate every env-var value captured during Phase 2:

1. Generate new values in upstream systems
2. Update values in destination contexts and project env vars
3. Confirm destination builds still pass

### 8.2 Re-register runners

Self-hosted **runner instances** and their **registration tokens** do not
transfer — only the resource-class definitions were migrated. Re-register each
runner agent against the destination resource classes:

```bash
# On each runner host, update the CircleCI runner config to point at the
# destination org's resource class and use a new registration token.
# Generate a new token in the destination org:
#   Organization Settings → Self-hosted Runners → <resource-class> → Add Token
```

### 8.3 Clean up capture artifacts

- Delete `secrets.json` locally
- Delete `migration-identity.age` and `migration-recipient.txt` if generated
- Verify capture pipeline artifacts have expired (or manually clean up if
  retention was not set to 1 day)

### 8.4 Update external pins

See the full list at
[cutover-runbook.md — Section 5](../cutover-runbook.md#5-update-external-pins):

- Service catalogs / Backstage (new project UUIDs)
- Slack and notification integrations
- Dashboards, status badges, Insights links
- Branch-protection / required-status checks on GitHub
- Any tooling that hard-codes CircleCI project or context UUIDs

### 8.5 Decommission source org

When the destination is fully healthy:

1. Disable pipeline triggers in the source org
2. Communicate the migration to your team
3. Monitor the destination for any missed configurations

### 8.6 What does NOT transfer

For the canonical reference, see:
[cutover-runbook.md — Section 4](../cutover-runbook.md#4-does-not-transfer--data-loss).

---

### Phase 8 checklist

- [ ] Every captured secret value rotated
- [ ] `secrets.json` deleted from local machine
- [ ] Runner instances re-registered against destination resource classes
- [ ] Service catalog / Backstage entries updated (new UUIDs)
- [ ] GitHub branch-protection status checks updated
- [ ] Slack / notification integrations updated
- [ ] Status badges updated
- [ ] Source org triggers disabled when ready
- [ ] Team notified of the new org

---

## See also

- [Migration guide — GitHub App → GitHub App scenario](../guide.md#7b-github-app--github-app)
- [Migration guide — standalone → standalone scenario](../guide.md#7c-circleci-standalone--standalone)
- [Cutover runbook](../cutover-runbook.md)
- [mapping.json reference](../mapping.md)
- [Troubleshooting](../troubleshooting.md)
- [CLI reference](../cli/README.md)
