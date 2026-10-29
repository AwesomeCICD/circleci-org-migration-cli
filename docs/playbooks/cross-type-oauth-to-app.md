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
| **Pipeline definitions of a plain OAuth project** | A plain OAuth project has one implicit pipeline, so `sync` **synthesizes one** `build-and-test` definition (config `.circleci/config.yml`) with a single push trigger. It cannot know about any other config path. | Review projects that rely on multiple config paths; create the extra definitions in the destination |
| **Pipeline definitions already captured** | If the OAuth org already has GitHub App pipelines, the export captured them and `sync` **recreates every one** (name, description, custom `file_path`, triggers) instead of synthesizing — see [What is recreated](#what-is-recreated-on-the-app-destination). | Nothing to do; verify in Phase 6 |

**Group context restrictions:** group restrictions are an OAuth-only feature.
When the destination is a standalone/App org, group restrictions in the source
will be reported as `manual` and must be recreated using expression restrictions
or project restrictions in the destination. See
[context-restriction docs](https://circleci.com/docs/contexts/#restricting-a-context).

**Project slug shape changes:** because the org type changes, every project
slug changes shape: `gh/acme/web` → `circleci/<org-uuid>/<project-uuid>`. The
project UUID only exists once the project does, so the tool cannot derive
App-style project slugs; `sync` therefore finds-or-creates App projects **by
name** and does not need a `projects` mapping to create them. The `projects`
entries are needed afterwards, by the steps that address a destination project
by slug (project-type context restrictions and `secrets transfer
--include-project-vars`) — generate them with `mapping generate` once the
projects exist (see 0.7).

### 0.4 GitHub App repo connections (required before Phase 5)

Every repository should be connected to the destination CircleCI GitHub App
**before** `sync --apply` creates projects. For a repo that is not yet connected
the project is still created, but its pipeline definition fails with a `manual`
action ("connect the repo to the GitHub App"); connect the repo and **re-run
`sync --apply`** — the re-run creates the missing definition and trigger (see
[Re-running sync](#re-running-sync)). Verify in your destination org's GitHub App
installation settings that all expected repositories are connected.

If repositories have also moved to a **new GitHub organization** (e.g. an EMU
migration), you will also need `--dest-github-org` and `--github-token` — see
Phase 5 for details.

### 0.5 API tokens

```bash
export CIRCLECI_SOURCE_TOKEN="<source-org-admin-token>"
export CIRCLECI_DEST_TOKEN="<destination-org-admin-token>"
```

### 0.6 GitHub token (required for OAuth projects and for moved repos)

```bash
export GITHUB_TOKEN="<github-pat-with-repo-read>"
```

A GitHub token (`--github-token`, or the `$GITHUB_TOKEN` fallback) is needed
whenever `sync` has to look up a repository's numeric GitHub ID:

- **OAuth projects with no captured GitHub App pipeline definition** — i.e.
  almost every project in an OAuth org. `sync` synthesizes a definition for
  each, and the OAuth export has no repo ID to reuse, so the ID comes from the
  GitHub API. **Preflight (`doctor`, `sync`, `migrate`) fails without a token
  in this case**; with `--skip-preflight` every such project would end up
  `manual: no external_id available`.
- **Repos moved to a new GitHub org** (`--dest-github-org` or mapping
  `github_org`) — their IDs change and are re-resolved through the API.

It is **not** needed when the GitHub org is unchanged *and* the manifest already
captured the repo IDs (every project had GitHub App pipelines): those IDs are
reused as-is.

### 0.7 Build the mapping file

Start with just the destination org — that is all `sync` needs to create the
App projects (it finds-or-creates them by name):

```json
{
  "org": {
    "from": "gh/acme",
    "to": "circleci/22222222-2222-2222-2222-222222222222"
  }
}
```

An App project's slug is `circleci/<org-uuid>/<project-uuid>`, **not**
`circleci/<org-uuid>/<repo-name>` — the project UUID is assigned when the
project is created, so you cannot write these entries by hand before Phase 5.
After `sync --apply` has created the projects, generate the `projects` section:

```bash
circleci-migrate mapping generate \
  --manifest manifest.json \
  --dest-org circleci/22222222-2222-2222-2222-222222222222 \
  --dest-token $CIRCLECI_DEST_TOKEN \
  -o mapping.json
```

which yields entries of the form:

```json
{
  "org": {
    "from": "gh/acme",
    "to": "circleci/22222222-2222-2222-2222-222222222222"
  },
  "projects": {
    "gh/acme/web": "circleci/22222222-2222-2222-2222-222222222222/3f1c0b52-7d0e-4a57-9a53-0a6d4f1e9b10",
    "gh/acme/api": "circleci/22222222-2222-2222-2222-222222222222/9b8a6c14-2e7f-4d3b-8c61-5e2a7d0f4c33"
  }
}
```

Use that mapping for the steps that address a destination project by slug
(project-type context restrictions, `secrets transfer --include-project-vars`)
and for re-runs of `sync`.

If repos moved to a new GitHub org, add `github_org` (`mapping generate` writes
only `org` and `projects`, so re-add it to the generated file):

```json
{
  "org": {
    "from": "gh/acme",
    "to": "circleci/22222222-2222-2222-2222-222222222222"
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
- [ ] `mapping.json` created with correct `org.to` (`projects` entries are generated after Phase 5)
- [ ] `GITHUB_TOKEN` set (needed for OAuth projects without captured App pipelines, and for moved repos)
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
- Total projects, and which already have GitHub App pipeline definitions (these are recreated as captured; the rest get a synthesized one and need a GitHub token)

### 1.2 Check your mapping file

`mapping.json` needs only `org.to` for `sync` to create every project in the
report in the destination — a project with no `projects` entry is **not**
skipped (the App destination path finds-or-creates projects by name). The
`projects` entries (generated after Phase 5, see 0.7) are only needed for the
slug-addressed follow-up steps. Confirm `org.to` is the destination
`circleci/<uuid>` slug and, if repos moved GitHub orgs, that `github_org` is set.

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
- [ ] `mapping.json` has the correct `org.to` (and `github_org` if repos moved)
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

# Execute (context env vars only):
circleci-migrate secrets transfer \
  --manifest manifest.json \
  --dest-org-id "$DST_UUID" \
  --dest-token-context migration-secrets \
  --enable-trigger \
  --apply
```

SSH keys still require Option A.

> **Project env vars are post-sync.** `secrets transfer --include-project-vars`
> writes values directly to destination *projects*, which do not exist until
> `sync --apply` (Phase 5) creates them — so run that step **after Phase 5**, not
> here. For a GitHub App destination the mapping must contain explicit
> per-project entries with the destination `circleci/<org-id>/<project-id>`
> slugs (the standalone transfer routes project vars only via the mapping's
> `projects` map, never derived from `org.from`/`org.to`).

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

- Repository not yet connected to destination GitHub App — connect it (the
  project is still created; re-run to create the definition)
- External repo ID resolution failed — check `--github-token` / `--dest-github-org`
- No GitHub token for an OAuth project that needs a synthesized definition —
  preflight blocks this up front; set `--github-token` / `$GITHUB_TOKEN`

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

The GitHub API is called only to look up a repository's numeric `external_id`
when the manifest does not already hold a usable one:

- **Synthesized definitions** (plain OAuth projects): the repo ID is looked up
  from the repo's `owner/name` with `--github-token` / `$GITHUB_TOKEN`.
- **Repos moved to a new GitHub org** (`--dest-github-org` or mapping
  `github_org`): the new numeric ID is looked up in the new org.

When the GitHub org is unchanged and the ID was captured (projects that already
had GitHub App pipelines), the captured ID is reused with no GitHub call, token
or not.

### What is recreated on the App destination

For every pipeline definition captured in the manifest, `sync` recreates:

| Field | Behaviour |
|---|---|
| Name, description | Preserved. |
| Config source `file_path` | Preserved (e.g. `.circleci/release.yml`); `.circleci/config.yml` only if none was captured. |
| Config / checkout / trigger repo provider | `github_oauth` → `github_app`; `github_app` and `github_server` kept as-is. Any other provider is reported `manual`. |
| Trigger `event_preset`, `checkout_ref`, `config_ref` | Preserved. |
| Webhook triggers | Recreated (sender, `event_name`, refs). The webhook URL **and secret are new** — a `manual` action reminds you to redistribute them to the sender. |
| Schedule triggers | Recreated (cron expression, `event_name`, refs, pipeline `parameters`). Attribution is set to `system` unless the source recorded `current`/`system`. |
| Trigger enabled state | Created **disabled**; triggers that were enabled in the source are enabled at the enable-builds step (Phase 7), triggers disabled in the source stay disabled. |

> **Not preserved:** the trigger's `name` and `description`. The CircleCI API
> assigns a trigger's name itself and accepts neither field when creating a
> trigger, so recreated triggers carry CircleCI-assigned names. Re-runs match
> existing triggers by their defining fields (provider, preset, refs, event name,
> cron, sender) when the names differ.

A plain OAuth project (no captured definitions) gets one synthesized
`build-and-test` definition + push trigger whose `event_preset` follows the
project's `build_prs_only` flag (`only-build-prs` or `all-pushes`).

### Re-running sync

`sync` is safe to re-run and **reconciles existing App projects**: for a project
that already exists in the destination it lists the destination's pipeline
definitions and triggers and then

- creates any definition missing by name, and any trigger missing from an
  existing definition (this repairs a project whose definition failed earlier
  because the repo was not yet connected to the GitHub App);
- reports everything already present as `exists` — a second run changes nothing;
- queues for enable-builds every trigger that was enabled in the source but is
  still disabled in the destination, so `sync --apply --yes` enables builds even
  when the run that created the trigger never reached the enable step.

A dry run (`sync` without `--apply`) shows exactly this: `would create` for what
is missing and `exists` for what is present. If you deliberately disabled a
trigger in the destination after migration, a later `sync --apply --yes` will
re-enable it (the prompt/`--yes` is the gate) — drop `--yes` and answer **N**, or
use `--skip-projects`, when re-running for other sections.

### 5.2 Projects with a `manual` pipeline definition

A project whose repo is not yet connected to the GitHub App is still created, but
its pipeline definition is recorded as `manual`. Skipped definitions do not
block other resources. After connecting the repo, re-run `sync --apply`; the
re-run creates the missing definition and trigger.

---

### Phase 5 checklist

- [ ] `sync --apply` completed without errors
- [ ] Context count in destination matches Phase 1 notes
- [ ] Project count in destination matches Phase 1 notes (accounting for skips)
- [ ] Pipeline definitions and triggers present in destination
- [ ] `manual` pipeline definitions noted; repos connected and `sync --apply` re-run
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

- Pipeline definition(s) present, pointing to the correct config path (the
  captured `file_path`, or `.circleci/config.yml` for a synthesized definition)
- Trigger(s) present (in disabled state); webhook triggers have a new URL/secret

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
- [ ] `manual` pipeline definitions from Phase 5 resolved (repos connected + sync re-run)
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

This works on a **re-run** too: triggers created by an earlier `sync --apply`
and still disabled are re-queued and enabled, so you do not need to re-create
anything to cut over. If any enable fails, every result is still printed and
`sync` exits **non-zero**; fix the cause and run the command again.

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
- Pipeline definitions a plain OAuth project never had — `sync` synthesizes one
  per project; additional definitions must be created manually (definitions
  already captured from GitHub App pipelines are recreated)
- Trigger names/descriptions — assigned by CircleCI on creation, not copied

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
