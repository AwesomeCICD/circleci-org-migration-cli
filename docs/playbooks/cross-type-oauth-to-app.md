# Playbook: GitHub OAuth → GitHub App (cross-type, lossy)

**Source:** `gh/<source-org>` — GitHub OAuth integration  
**Destination:** `circleci/<dst-uuid>` — GitHub App (standalone) org  
**Typical use:** migrating a legacy GitHub OAuth CircleCI org to a GitHub App
org after GitHub EMU migration, consolidation, or re-onboarding.

> This is a **lossy** migration. Several OAuth-specific settings and build
> behaviours have no equivalent in the GitHub App integration and will be
> silently dropped. Read [Section 0.3](#03-data-loss-review-before-you-start)
> in full before proceeding.

> Throughout, `gh/acme` is the source and
> `DST_UUID=22222222-2222-2222-2222-222222222222` is the destination UUID.
> Substitute your own values.

---

## Rollback / abort

Nothing in the source org is ever modified or destroyed. Projects in the
destination are created with triggers **disabled** — they will not build until
you explicitly enable them in Phase 7. To abort at any point before Phase 7:
do nothing. The source org keeps running normally. To clean up a partial
destination, delete contexts and projects in the destination org settings UI.

---

## Phase 0 — Prerequisites and identification

### 0.1 Confirm source org slug and type

The source org uses the `gh/<name>` slug format (GitHub OAuth).

```bash
# Source slug: gh/acme
```

### 0.2 Confirm destination org slug and type

The destination uses the `circleci/<uuid>` slug format (GitHub App).

```bash
DST_UUID="22222222-2222-2222-2222-222222222222"
# Destination slug: circleci/22222222-2222-2222-2222-222222222222
```

Find the destination UUID at:
`https://app.circleci.com/settings/organization/circleci/<uuid>/overview`

### 0.3 Data-loss review: what is DROPPED in this migration

**Read this section in full before continuing.**

The following OAuth settings have no GitHub App equivalent and will be
reported as `manual` or silently omitted:

| OAuth setting | Status in App org | Action |
|---|---|---|
| `build_fork_prs` (build PRs from forks) | **No App equivalent** — the GitHub App never builds fork PRs in the same way | Remove or accept the drop; inform contributors |
| `forks_receive_secret_env_vars` (fork PRs get env vars) | **No App equivalent** | Accept the drop; the App org never injects secrets into fork-PR builds |
| `oss` flag (open-source free tier) | **No App equivalent** | Accept the drop; contact CircleCI support if OSS access is needed |
| `pr_only_branch_overrides` (trigger only on PR branches) | **No App equivalent** | Accept the drop; configure trigger filters in the pipeline definition instead |
| **Multiple pipeline definitions per project** | Each OAuth project maps to **one** pipeline definition using the default config path (`.circleci/config.yml`). OAuth projects have a single config; App orgs can have multiple pipeline defs — but `sync` creates only one. | Review projects that rely on multiple config paths or alternative config sources |

**Group context restrictions:** group restrictions are an OAuth-only feature.
When the destination is a standalone/App org, group restrictions in the source
will be reported as `manual` and must be recreated using expression restrictions
or project restrictions in the destination. See
[context-restriction docs](https://circleci.com/docs/contexts/#restricting-a-context).

**Project slug shape changes:** because the org type changes, every project
slug changes shape: `gh/acme/web` → `circleci/<uuid>/web`. You must supply an
explicit `projects` mapping for each project — the tool cannot derive App-style
project slugs automatically.

### 0.4 GitHub App repo connections (required before Phase 5)

Every repository must be connected to the destination CircleCI GitHub App
**before** `sync --apply` creates projects. Repos not yet connected are skipped
with a warning. Verify in your destination org's GitHub App installation settings
that all expected repositories are connected.

If repositories have also moved to a **new GitHub organization** (e.g. an EMU
migration), you will also need `--dest-github-org` and `--github-token` — see
Phase 5 for details.

### 0.5 API tokens

```bash
export CIRCLECI_SOURCE_TOKEN="<source-org-admin-token>"
export CIRCLECI_DEST_TOKEN="<destination-org-admin-token>"
```

### 0.6 GitHub token (required if repos moved to a new GitHub org)

```bash
export GITHUB_TOKEN="<github-pat-with-repo-read>"
```

Supply `--github-token $GITHUB_TOKEN` (or rely on the `$GITHUB_TOKEN` fallback)
together with `--dest-github-org` when repositories have moved GitHub orgs and
their numeric IDs have changed. Not needed if repos remain in the same GitHub org.

### 0.7 Build the mapping file

Because the org type changes, project slugs change shape. You MUST provide an
explicit `projects` entry for every project that has a different slug structure:

```json
{
  "org": {
    "from": "gh/acme",
    "to": "circleci/22222222-2222-2222-2222-222222222222"
  },
  "projects": {
    "gh/acme/web": "circleci/22222222-2222-2222-2222-222222222222/web",
    "gh/acme/api": "circleci/22222222-2222-2222-2222-222222222222/api",
    "gh/acme/frontend": "circleci/22222222-2222-2222-2222-222222222222/frontend"
  }
}
```

If repos moved to a new GitHub org, add `github_org`:

```json
{
  "org": {
    "from": "gh/acme",
    "to": "circleci/22222222-2222-2222-2222-222222222222"
  },
  "projects": {
    "gh/acme/web": "circleci/22222222-2222-2222-2222-222222222222/web",
    "gh/acme/api": "circleci/22222222-2222-2222-2222-222222222222/api"
  },
  "github_org": { "from": "acme", "to": "acme-new" }
}
```

See [mapping.md](../mapping.md) for the full schema.

### 0.8 Self-hosted runners (optional)

If the source org uses self-hosted runner resource classes, identify the runner
namespace. You will need `--runner-namespace` on export and
`--dest-runner-namespace` on sync.

### 0.9 Install the CLI (v0.10.0+)

```bash
brew install AwesomeCICD/tap/circleci-migrate
circleci-migrate version
```

---

### Phase 0 checklist

- [ ] Source OAuth slug confirmed: `gh/_______________`
- [ ] Destination App slug confirmed: `circleci/_______________`
- [ ] Data-loss review completed (Section 0.3); stakeholders informed of dropped settings
- [ ] GitHub App repo connections verified for all repos in destination org
- [ ] Source token set in `CIRCLECI_SOURCE_TOKEN`; user is org admin
- [ ] Destination token set in `CIRCLECI_DEST_TOKEN`; user is org admin
- [ ] `mapping.json` created with correct `org.to` and all `projects` entries
- [ ] Runner namespace(s) noted (if applicable): `_______________`
- [ ] `circleci-migrate version` shows v0.10.0 or later

### ✅ Confirm ALL of the above before continuing to Phase 1

---

## Phase 1 — Export and review

Export is **read-only** and safe to re-run.

```bash
circleci-migrate export \
  --source-org gh/acme \
  --output manifest.json \
  --report migration-report.md
```

With runner resource classes:

```bash
circleci-migrate export \
  --source-org gh/acme \
  --runner-namespace acme-runners \
  --output manifest.json \
  --report migration-report.md
```

### 1.1 Review the audit report

Pay special attention to:

- OAuth-specific flags per project: `build_fork_prs`, `forks_receive_secret_env_vars`,
  `oss`, `pr_only_branch_overrides` — these will all be `manual` in the
  cross-type sync report
- Context restrictions: group restrictions will be flagged as `manual`
- Contexts and their env-var counts
- Total projects — you must have a `projects` mapping entry for each

### 1.2 Verify your mapping file covers all projects

Every project in `migration-report.md` must have a corresponding entry in the
`projects` section of `mapping.json`. Missing projects will be silently skipped
at sync time.

### 1.3 Expected counts

| Item | Count |
|---|---|
| Contexts | |
| Context env vars (total) | |
| Projects | |
| Project env vars (total) | |
| OAuth flags to drop (`build_fork_prs`, `oss`, etc.) | |
| Group context restrictions (will be manual) | |

---

### Phase 1 checklist

- [ ] `manifest.json` written without errors
- [ ] `migration-report.md` reviewed in full
- [ ] All projects in the report have a matching entry in `mapping.json`
- [ ] OAuth-specific settings reviewed; drop decisions documented
- [ ] Group restriction plan agreed (drop or recreate as expression restrictions)

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

**Non-interactive:**

```bash
circleci-migrate secrets capture \
  --manifest manifest.json \
  --encrypt \
  --generate-key \
  --artifact-retention-days 1 \
  --enable-trigger \
  --output secrets.json
```

For context extraction, specify the host project:

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

**Important:** capture runs in the **source (OAuth) org**. The destination org
does not need to exist for this step.

### Option B — `secrets transfer` (zero-disk-write, context vars only)

Store the destination API token in a source-org context (e.g. `migration-secrets`)
with env var `CIRCLECI_DEST_TOKEN`, then:

```bash
# Dry run:
circleci-migrate secrets transfer \
  --manifest manifest.json \
  --dest-org-id "$DST_UUID" \
  --dest-token-context migration-secrets

# Execute:
circleci-migrate secrets transfer \
  --manifest manifest.json \
  --dest-org-id "$DST_UUID" \
  --dest-token-context migration-secrets \
  --enable-trigger \
  --apply
```

SSH keys still require Option A.

### Restricted contexts

Contexts with restrictions are skipped by default. Options:
- `--remove-restrictions` — temporarily lift restrictions for the capture run
- Accept the gap; use `sync --missing-secrets placeholder` so the variable name
  exists for manual fill-in

### Security reminders

- `secrets.json` contains **plaintext** values — protect it accordingly.
- **Rotate every captured value** after the destination is confirmed healthy (Phase 8).

---

### Phase 2 checklist

- [ ] Decision made: Option A or Option B
- [ ] If Option A: `secrets.json` written; encryption confirmed
- [ ] If Option B: dry-run plan reviewed; destination token in source context
- [ ] Restricted-context gap plan agreed
- [ ] `secrets.json` not committed to source control

### ✅ Secrets captured or transfer planned — continue to Phase 3

---

## Phase 3 — Choose management model

### Option A — CLI applies everything (default)

Skip ahead to Phase 4. No extra setup required.

### Option B — Terraform-managed destination

```bash
circleci-migrate terraform generate \
  --manifest manifest.json \
  --secrets secrets.json \
  --mapping mapping.json \
  --dest-org-id "$DST_UUID" \
  --dest-org-type standalone \
  --out ./terraform/
```

**Note:** `--dest-org-type standalone` is required here even though the **source**
is an OAuth org, because the **destination** is a GitHub App (standalone) org.
This ensures `circleci_pipeline` and `circleci_trigger` resources are generated
and OAuth-only advanced settings are included in the output.

```bash
cd ./terraform/
export TF_VAR_circleci_api_token="<dest-org-api-token>"
terraform init && terraform plan && terraform apply
```

Review `GAPS.md` and run the CLI gap-fill:

```bash
circleci-migrate sync \
  --manifest manifest.json \
  --secrets secrets.json \
  --mapping mapping.json \
  --dest-token $CIRCLECI_DEST_TOKEN \
  --apply \
  --skip-terraform-managed
```

---

### Phase 3 checklist

- [ ] Management model decided: (A) CLI-only  OR  (B) Terraform-managed
- [ ] If B: `terraform generate` run with `--dest-org-type standalone`; GAPS.md reviewed
- [ ] If B: `terraform apply` succeeded; `sync --skip-terraform-managed --apply` run

### ✅ Management model established — continue to Phase 4

---

## Phase 4 — Dry-run sync

```bash
circleci-migrate sync \
  --manifest manifest.json \
  --secrets secrets.json \
  --mapping mapping.json
```

With runner resource classes:

```bash
circleci-migrate sync \
  --manifest manifest.json \
  --secrets secrets.json \
  --mapping mapping.json \
  --dest-runner-namespace acme-new-runners
```

With repos moved to a new GitHub org:

```bash
circleci-migrate sync \
  --manifest manifest.json \
  --secrets secrets.json \
  --mapping mapping.json \
  --github-token $GITHUB_TOKEN \
  --dest-github-org acme-new
```

### 4.1 Expected `manual` items in the dry-run

The following will appear as `manual` — this is expected for a cross-type
migration:

- `build_fork_prs`, `forks_receive_secret_env_vars`, `oss`, `pr_only_branch_overrides`
  — no App equivalent; must be handled outside CircleCI or via trigger filters
- Group context restrictions — recreate as expression restrictions or project
  restrictions in the destination org UI
- Project-type context restrictions — recreate in the destination org UI
  (source-org project UUIDs do not transfer)
- Per-project CIAM grants (if destination is a standalone org with CIAM)

### 4.2 Review `error` lines

`error` lines indicate a problem that will prevent resource creation at apply
time. Common causes:

- Project missing from `mapping.json` — add the entry
- Repository not yet connected to destination GitHub App — connect it
- External repo ID resolution failed — check `--github-token` / `--dest-github-org`

---

### Phase 4 checklist

- [ ] Dry-run completed
- [ ] All OAuth-flag `manual` items reviewed and drop/replacement plan confirmed
- [ ] Group restriction `manual` items reviewed; expression-restriction plan agreed
- [ ] No unexpected `error` lines (all errors understood and fixable)
- [ ] All projects appear in the plan (check vs Phase 1 count)

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

With runner resource classes:

```bash
circleci-migrate sync \
  --manifest manifest.json \
  --secrets secrets.json \
  --mapping mapping.json \
  --dest-runner-namespace acme-new-runners \
  --apply
```

With cross-GitHub-org repo move (`--dest-github-org` resolves numeric repo IDs):

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

### 5.1 What `--github-token` and `--dest-github-org` do

When repositories have moved to a new GitHub org, their numeric `external_id`
in GitHub changes. The `--dest-github-org` flag tells the tool to look up the
new numeric IDs using the GitHub API, so pipeline definitions are created with
the correct `external_id`. `--github-token` (or `$GITHUB_TOKEN`) provides the
PAT with `repo` read access for this lookup.

Without these flags (same-GitHub-org scenario), the `external_id` captured in
the manifest is reused as-is.

### 5.2 Projects skipped

Projects that have no entry in `mapping.json`, or whose repo is not yet
connected to the GitHub App, will be **skipped** with a warning. Skipped
projects do not block other resources from being created. Re-run `sync --apply`
after connecting the repo or adding the mapping entry.

---

### Phase 5 checklist

- [ ] `sync --apply` completed without errors
- [ ] Context count in destination matches Phase 1 notes
- [ ] Project count in destination matches Phase 1 notes (accounting for skips)
- [ ] Pipeline definitions and triggers present in destination
- [ ] Skipped projects noted and planned for follow-up
- [ ] Builds NOT yet enabled (triggers disabled)

### ✅ Resources created; builds not yet enabled — continue to Phase 6

---

## Phase 6 — Validate destination

### 6.1 Contexts and env vars

In the CircleCI UI for the destination org (**Organization Settings → Contexts**):

- All contexts present with correct env-var counts
- Expression and project context restrictions applied correctly
- Group restrictions verified as manually recreated (if applicable)

### 6.2 Project pipeline definitions and triggers

For each project in the destination (**Project Settings → Pipelines/Triggers**):

- Pipeline definition present, pointing to the correct config path (`.circleci/config.yml`)
- Trigger present (in disabled state)

### 6.3 OAuth settings dropped

Confirm with stakeholders that the dropped settings are acceptable:
- `build_fork_prs` — fork contributors are informed builds will not trigger automatically
- `oss` — OSS contributors aware of any change in free access
- `pr_only_branch_overrides` — trigger filter configuration reviewed

### 6.4 Fresh export of destination

```bash
circleci-migrate export \
  --source-org "circleci/$DST_UUID" \
  --source-token $CIRCLECI_DEST_TOKEN \
  --output manifest-dest.json \
  --report report-dest.md
```

Compare context and project counts between `manifest.json` and
`manifest-dest.json`.

### 6.5 Manual items

Work through `manual` items from Phase 4 and `migration-report.md`:

- Group context restrictions — recreate as expression restrictions in destination UI
- Project-type context restrictions — recreate in destination org UI
- OAuth-only settings — document decision for each dropped item
- Webhook signing secrets — regenerate and update receivers
- OTel exporter headers — re-add in org settings
- See: [cutover-runbook.md — Section 4](../cutover-runbook.md#4-does-not-transfer--data-loss)

---

### Phase 6 checklist

- [ ] All contexts present with correct env-var counts
- [ ] Pipeline definitions and triggers present per project
- [ ] Context restrictions validated (expression/project types; group types manually recreated)
- [ ] Dropped OAuth settings documented and communicated to stakeholders
- [ ] Skipped projects from Phase 5 resolved (repos connected + sync re-run)
- [ ] All `manual` items from Phase 4 addressed

### ✅ Destination validated — continue to Phase 7

---

## Phase 7 — Enable builds / cutover

When satisfied the destination is correct, enable builds.

```bash
circleci-migrate sync \
  --manifest manifest.json \
  --secrets secrets.json \
  --mapping mapping.json \
  --apply \
  --yes
```

`--yes` auto-confirms the enable prompt. For GitHub App projects, this unpauses
the pipeline triggers — future pushes to configured branches will trigger pipelines.

### 7.1 Verify triggers are active

In the destination org (**Project Settings → Triggers**): triggers show as active.

### 7.2 Smoke pipeline

Push a commit or trigger a pipeline manually:

- Confirm pipeline starts and progresses through jobs
- Confirm context env vars are injected correctly
- Confirm any expression-based context restrictions work as expected
- Confirm no fork-PR builds fire (if the drop of `build_fork_prs` was intentional)

---

### Phase 7 checklist

- [ ] `sync --apply --yes` run (or triggers enabled via UI)
- [ ] Pipeline triggers show as active in destination
- [ ] At least one pipeline ran successfully in the destination
- [ ] Context env vars accessible within a job
- [ ] OAuth-dropped settings confirmed absent (e.g. fork-PR builds do not trigger)
- [ ] No unexpected build failures

### ✅ A real pipeline ran green on the destination — continue to Phase 8

---

## Phase 8 — Post-cutover

### 8.1 Rotate all captured secrets

Rotate every env-var value captured during Phase 2:

1. Generate new values in upstream systems
2. Update values in destination contexts and project env vars
3. Confirm destination builds still pass

### 8.2 Re-register runners (if applicable)

Runner instances and registration tokens do not transfer. Re-register each agent
against the destination resource classes.

### 8.3 Clean up capture artifacts

- Delete `secrets.json` from your local machine
- Delete `migration-identity.age` and `migration-recipient.txt` if generated
- Verify capture pipeline artifacts expired

### 8.4 Update external pins

The destination project slugs are now `circleci/<uuid>/<project>` not
`gh/acme/<project>`. Update all external references:

- Service catalogs / Backstage (slugs and UUIDs have changed)
- Slack and notification integrations
- Dashboards, status badges, Insights links
- Branch-protection / required-status-check integrations on GitHub (check
  App-vs-OAuth integration name difference)
- Any tooling that hard-codes CircleCI project or context UUIDs

See: [cutover-runbook.md — Section 5](../cutover-runbook.md#5-update-external-pins)

### 8.5 Decommission source OAuth org

When the destination is confirmed healthy:

1. Unfollow projects in the source org (removes webhooks from GitHub)
2. Delete or archive contexts in the source org
3. Communicate the migration to your team and to any external contributors
   (especially if `build_fork_prs` was in use)

### 8.6 What does NOT transfer

For the canonical reference, see:
[cutover-runbook.md — Section 4](../cutover-runbook.md#4-does-not-transfer--data-loss).

Key cross-type items to note:

- Fork-PR builds, the OSS flag, and `pr_only_branch_overrides` — no App equivalent
- Group context restrictions — no direct App equivalent (use expression restrictions)
- Multiple pipeline definitions per project — `sync` creates one per project;
  additional definitions must be created manually

---

### Phase 8 checklist

- [ ] Every captured secret value rotated
- [ ] `secrets.json` deleted from local machine
- [ ] Service catalog / Backstage entries updated (new `circleci/<uuid>/<project>` slugs)
- [ ] GitHub branch-protection / status check integrations updated
- [ ] Slack / notification integrations updated
- [ ] Status badges updated
- [ ] Fork-PR contributors informed of the change in behaviour
- [ ] Source OAuth org projects unfollowed / decommissioned when ready
- [ ] Team notified of the new org

---

## See also

- [Migration guide — cross-type OAuth → GitHub App scenario](../guide.md#7e-cross-type-oauth--github-app)
- [Cutover runbook](../cutover-runbook.md)
- [mapping.json reference](../mapping.md)
- [Troubleshooting](../troubleshooting.md)
- [CLI reference](../cli/README.md)
