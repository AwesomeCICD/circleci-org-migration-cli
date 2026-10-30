---
title: "Migrating a CircleCI Organization (GitHub OAuth → GitHub App)"
subtitle: "A step-by-step cutover runbook with checklists and validation gates"
author: "CircleCI"
date: "2026"
---

# Migrating a CircleCI Organization

## GitHub OAuth → GitHub App (standalone)

**Use this guide when** you are moving a CircleCI organization that uses the
legacy **GitHub OAuth** integration (slug `gh/<name>`) to a **standalone
GitHub App** organization (slug `circleci/<uuid>`). The OAuth org may also have
the CircleCI GitHub App installed alongside OAuth — this guide covers both.

**Tooling:** the `circleci-migrate` CLI, **v0.19.0 or later**. This guide uses
the `cutover` command and the `--skip-pipelines`, `--skip-contexts` and
`--webhook-secrets-out` flags, which are new in v0.19.0.

Throughout, the **source** is `gh/acme` and the **destination** is
`circleci/22222222-2222-2222-2222-222222222222`. Substitute your own values.

> **This migration is lossy, and the order matters.** The two integrations are
> different, so a few OAuth-only settings have no GitHub App equivalent (see
> What does not transfer). And several steps cannot
> be done after a later one — most importantly, **uninstalling the CircleCI
> GitHub App from a GitHub organization deletes that CircleCI org's GitHub App
> pipelines and triggers.** Follow the cutover order
> exactly.

> **Safety first.** Nothing is ever deleted from the source org. The
> destination is created with every trigger **disabled**, so nothing builds
> there until you turn it on. The cutover is reversible
> (Undo).

---

## What transfers

| Transfers automatically | Requires a manual step |
|---|---|
| Contexts + context env vars (via `secrets transfer`) | **Group** context restrictions (no App equivalent — use expression or project restrictions) |
| **Project-type** and expression context restrictions (remapped by project name) | Webhook signing secrets (new ones are generated and written to a file — hand them to each receiver) |
| Project env vars and additional SSH keys (via `secrets transfer`) | Checkout / deploy key **private** keys (never exportable; App projects check out through the GitHub App) |
| Project settings and the v1.1 flags that still apply to App projects | Plan / billing (CircleCI Support) |
| Pipeline definitions and triggers (recreated; created **disabled**) | Same-name orb namespace (CircleCI Support) |
| Schedule and webhook triggers | SSO (SAML) |
| Org settings, org-level webhooks, runner resource classes | Self-hosted runner instances and tokens |
| Environment hierarchies (see Troubleshooting) | |

---

## Before you start

### One GitHub org connects to one CircleCI org

A GitHub organization can be connected to **only one CircleCI org** through the
CircleCI GitHub App. Moving to the new org therefore means moving the App: you
**uninstall** it from the OAuth org's connection and **install** it from the new
`circleci/<uuid>` org. Until you do, the new org cannot see your repositories.
The cutover order below places that move at exactly the right point.

### Checklist

- [ ] Source slug confirmed: `gh/________` (Org Settings → Overview)
- [ ] The destination org **already exists** in CircleCI, and you have its
      **Organization ID** (UUID) from **Organization Settings → Overview**
- [ ] You are an **organization admin** of both orgs, and you are also able to
      install and uninstall the CircleCI GitHub App in the GitHub organization
- [ ] **Two personal API tokens** (User Settings → Personal API Tokens): one for
      the source org and one for the destination org
- [ ] A **GitHub personal access token** with `repo` read — needed for every
      OAuth project that has no GitHub App pipeline definition (the tool
      synthesizes one and must look up the repository's numeric ID), and when
      repos moved to a new GitHub org
- [ ] A **`migration-secrets` context** in the *source* org (below)
- [ ] CLI installed and current:

```bash
brew install AwesomeCICD/tap/circleci-migrate
circleci-migrate version    # v0.19.0 or later
```

Set the tokens and the destination UUID for the session:

```bash
export CIRCLECI_SOURCE_TOKEN="<source-org-admin-token>"
export CIRCLECI_DEST_TOKEN="<destination-org-admin-token>"
export GITHUB_TOKEN="<github-pat-with-repo-read>"
export DST_UUID="22222222-2222-2222-2222-222222222222"
```

### The `migration-secrets` context

Secret values can only be read inside a running pipeline, so `secrets transfer`
runs a pipeline in your **source** org and writes the values straight to the
destination. It needs the destination token for that, and gets it from a
context — it is never written into any file.

In the **source** org (Organization Settings → Contexts), create a context named
`migration-secrets` with one variable:

| Variable | Value |
|---|---|
| `CIRCLECI_DEST_TOKEN` | *the destination org's API token* |

You will delete this context (and rotate the token) at the end.

### `mapping.json` — only the `org` line

Create `mapping.json` with just the destination org. That is all the tool needs:
it finds or creates each App project **by name**, and later steps match source
projects to destination projects by name too. You do **not** need
`mapping generate`, and you cannot write per-project entries by hand anyway,
because an App project's slug contains a UUID that does not exist until the
project does.

```json
{
  "org": {
    "from": "gh/acme",
    "to": "circleci/22222222-2222-2222-2222-222222222222"
  }
}
```

If the repositories also moved to a **different GitHub org**, add a
`github_org` entry and pass `--dest-github-org acme-new` to `sync`:

```json
{
  "org": {
    "from": "gh/acme",
    "to": "circleci/22222222-2222-2222-2222-222222222222"
  },
  "github_org": { "from": "acme", "to": "acme-new" }
}
```

### Preflight

```bash
circleci-migrate doctor \
  --source-org gh/acme \
  --dest-org "circleci/$DST_UUID"
```

`doctor` is read-only. It checks both tokens, that both orgs are reachable, and
that a GitHub token is present for the OAuth → App case.

### ✅ Gate: every box above checked; `doctor` reports no blockers.

---

## Step 1 — Export the source (read-only)

Export while the GitHub App is **still installed** on the source GitHub org, so
the manifest captures any GitHub App pipeline definitions the source already has.

```bash
circleci-migrate export \
  --source-org gh/acme \
  --dest-org "circleci/$DST_UUID" \
  --output manifest.json \
  --report migration-report.md
```

Passing `--dest-org` adds the ordered cross-type checklist to the report. Open
`migration-report.md`, read it in full, and record your baseline counts
(contexts, projects, project env vars, webhooks, schedules).

### ✅ Gate: report reviewed in full; counts recorded; every warning understood.

---

## Step 2 — Dry runs

Every command that changes something is a **dry run unless you pass `--apply`**.
Run each of these once now, and read the output, before you start the cutover.

```bash
# 2a. What would sync create? (projects only, no pipeline definitions yet)
circleci-migrate sync --manifest manifest.json --mapping mapping.json \
  --skip-pipelines

# 2b. What would the secrets transfer do, and can it run?
circleci-migrate secrets transfer --manifest manifest.json --mapping mapping.json \
  --dest-org-id "$DST_UUID" --dest-token-context migration-secrets \
  --include-project-vars --include-ssh-keys --remove-restrictions

# 2c. What would pausing the source change?
circleci-migrate cutover --manifest manifest.json --mapping mapping.json \
  --pause-source
```

What to look for:

- **`sync`**: `error` lines need a plan before you apply. `manual` lines for
  secret *values* are expected — values move in Step 4.3, not by `sync`.
- **`secrets transfer`**: the plan lists every source → destination project pair
  (marked `matched by name` when it came from the name match). **Before
  Step 4.2 every project shows as `SKIP`** (and the run would exit non-zero),
  because the destination projects do not exist yet — that is expected; run this
  dry run again right after Step 4.2. Any `SKIP` that remains after that has no
  way to check out its code — see Troubleshooting. The dry
  run ends with a `WARNING: --apply would fail` block when something would stop
  the real run (a paused source project, a project with no checkout key).
- **`cutover`**: lists each source project and trigger it would pause. Nothing is
  written without `--apply`.

You cannot preview everything now: the destination triggers do not exist until
Step 4.6, so a `cutover --enable-dest` preview only becomes meaningful after the
second `sync`.

### ✅ Gate: no unexplained `error` lines; you know why each `SKIP` appears.

---

## Step 3 — Tell the team

- Announce a **merge freeze** window and who is responsible for it.
- Tell users they will be asked to **re-authorize CircleCI with GitHub** after
  the App moves (Step 4.5).
- Decide who from CircleCI Support you will ask for the plan transfer and any
  namespace move (Step 5).

---

## Step 4 — The cutover

> Steps 4.1 to 4.8 must run **in this order**. The reason for each position is
> given next to the step.

Keep these two files safe until the cutover is confirmed:
`cutover-state.json` (what `--undo` restores from) and `webhook-secrets.json`
(the new webhook signing secrets — treat it like a credential).

### 4.1 Freeze merges

Freeze merges in the source repositories (branch protection, a merge freeze, or
an announcement) so nothing lands between the secrets transfer and the switch.

### 4.2 Create the destination projects — no pipelines yet

```bash
circleci-migrate sync --manifest manifest.json --mapping mapping.json \
  --skip-pipelines --apply
```

This creates (or reuses) each App project and syncs contexts, org settings and
project settings, but **creates no pipeline definitions or triggers** (each is
reported `skipped: --skip-pipelines`) and needs no GitHub token. The projects
must exist now because the next step writes secrets into them — and that step
must run while the GitHub App is still installed on the source.

### 4.3 Transfer the secrets while the source can still build

```bash
circleci-migrate secrets transfer --manifest manifest.json --mapping mapping.json \
  --dest-org-id "$DST_UUID" --dest-token-context migration-secrets \
  --include-project-vars --include-ssh-keys --remove-restrictions \
  --enable-trigger --apply
```

This runs one pipeline per source project, inside CircleCI, and writes each
value straight to the destination over TLS. **No secret value is written to
disk.** Why it must come **before** pausing the source: the transfer runs a
pipeline in each source project, and a project paused with
`drop-all-build-requests` drops it. `secrets transfer` fails fast and names any
paused project; if you ever hit that, run `cutover --undo --apply` first.

- `--remove-restrictions` temporarily lifts project/expression restrictions on
  source contexts and restores them afterwards (group restrictions are never
  touched).
- `--enable-trigger` turns on the "unversioned config" setting for the run and
  restores it afterwards.

To check this step: the log reports how many context and project pipelines
succeeded, and which GitHub App or OAuth definition each one ran.

### 4.4 Pause the source

```bash
circleci-migrate cutover --manifest manifest.json --mapping mapping.json \
  --pause-source            # preview
circleci-migrate cutover --manifest manifest.json --mapping mapping.json \
  --pause-source --apply
```

This pauses each source project **reversibly**: it sets
`drop-all-build-requests=true` (recording the previous value) and disables the
source's enabled App, webhook and schedule triggers. Projects are never
unfollowed. Add `--pause-org` to also set the flag on the whole source org.
Every change is written to `cutover-state.json` *before* it is made.

> **Pause the source before you uninstall the GitHub App.** After the
> uninstall the source's App triggers no longer exist, so they can be neither
> disabled nor re-enabled; the pause then relies on the flag alone, and `--undo`
> can only restore that flag.

### 4.5 Move the GitHub App connection

1. **Uninstall** the CircleCI GitHub App from the OAuth org's connection (in
   the GitHub organization's installed apps).
2. **Install** it from the new `circleci/<uuid>` org, and connect every
   repository you are migrating.
3. Have **each user** open the new org in the CircleCI web app and **Authorize**
   the CircleCI GitHub App with GitHub when prompted. If they see an empty
   **Projects** page, they use **Refresh permissions** so CircleCI re-reads
   which repositories they can access.

Repositories that are not connected still get a project, but their pipeline
definition fails with a `manual` note ("connect the repo to the GitHub App").
Connect the repo and re-run the next step.

### 4.6 Add the pipeline definitions and triggers

```bash
circleci-migrate sync --manifest manifest.json --mapping mapping.json \
  --apply --webhook-secrets-out webhook-secrets.json
```

Now that the App is connected, this adds a pipeline definition and the triggers
to each existing project. Triggers are created **disabled**. Answer **N** at the
"Enable N trigger(s) across M project(s)…?" prompt — you enable them in the next
step, under `cutover`.

- OAuth projects with no GitHub App definition get one synthesized
  `build-and-test` definition (config `.circleci/config.yml`) with a single push
  trigger. Review any project that relies on more than one config path.
- `--webhook-secrets-out` writes the **new** signing secret of every recreated
  outbound webhook, and the new URL of every recreated inbound webhook trigger,
  to `webhook-secrets.json` (mode 0600, never printed). Hand each receiver and
  sender its new value, then delete the file. Re-runs merge into the file rather
  than overwriting it.
- `sync` is safe to re-run. It creates only what is missing and reports the rest
  as `exists`.

### 4.7 Enable the destination

```bash
circleci-migrate cutover --manifest manifest.json --mapping mapping.json \
  --enable-dest             # preview
circleci-migrate cutover --manifest manifest.json --mapping mapping.json \
  --enable-dest --apply
```

This enables the destination triggers whose source counterpart was enabled. A
trigger that was disabled in the source stays disabled. An ambiguous match, or a
project the manifest shows as already paused or not followed, is reported
`manual` and **not** enabled.

> **Do not run `sync --apply --yes` instead of this step while the source is
> still building** — it enables destination triggers without pausing the source,
> so both orgs build (and are billed).

### 4.8 Validate

```bash
circleci-migrate validate \
  --source-org gh/acme \
  --dest-org "circleci/$DST_UUID" \
  --mapping mapping.json \
  --dest-token-context migration-secrets
```

`validate` exports both orgs read-only and prints a per-section parity report,
including a **Pipeline Definitions & Triggers** section for App destinations. It
exits non-zero if anything is **missing**. `--dest-token-context` leaves the
`migration-secrets` context out of the comparison, because it only exists to run
the migration. Secret *values* are never compared — the API masks them.

Expected `⚠` manual items: group context restrictions, SSO, CIAM role bindings.

Then push a commit (or trigger a pipeline) and confirm a real pipeline starts
and runs green on the destination, and that a job can read its context
variables.

### ✅ Gate: `validate` exits 0; every `⚠` item has a plan; a real pipeline ran green on the destination.

---

## Step 5 — What CircleCI Support must run

Two things cannot be done through the API and are run by **CircleCI Support**.
Open a support request with the details below. You can raise it before the
cutover so it is ready when you need it.

**Plan / billing transfer.** The destination org must be on the **Free** plan
and must not be a plan-share rider.

```text
transfer-plan "<from>" :github "<to-short-id>" :circleci
```

`<from>` is the source org and `<to-short-id>` is the destination org's short
ID — give Support the destination slug `circleci/<DST_UUID>` and they will
confirm it.

**Orb namespace (only if you publish orbs).** A namespace belongs to exactly one
org, so it is **moved**, not recreated. The orbs move with it and no config
changes are needed.

```text
change-namespace-owner! "<namespace>" "<dest-org-slug>"
```

The owner slug accepts `gh/<name>`, `github/<name>` or `circleci/<short-id>`.
Ask for `change-namespace-owner!` — **never** `rename-namespace`, which does not
free the old name.

After the move, runner resource classes keep the org they were created under, so
creating the same class name in the destination returns a 409. Delete the source
copies (or ask Support), re-run `sync --apply --dest-runner-namespace <ns>`,
issue new tokens with `circleci runner token create <class> "<nickname>"`, and
re-register your agents.

**Do not build in both orgs** once the destination is live — you would pay twice.

---

## Undoing the cutover

If something is wrong after the cutover, put the source back from the state
file:

```bash
circleci-migrate cutover --undo --apply
```

`--undo` restores everything recorded in `cutover-state.json`: the source flags
go back to their previous values, the source triggers it disabled are
re-enabled, and the destination triggers it enabled are disabled again. It needs
only the state file (and `--apply` to act), is idempotent, and fails if the file
does not exist. Pass `--manifest` and `--mapping` as well if you want the state's
orgs checked against them. Keep the file until the cutover is confirmed.

Two limits to know about:

- If the GitHub App has already been **uninstalled** from the source, its
  triggers no longer exist, so `--undo` can only restore the
  `drop-all-build-requests` flag. That is the reason the source is paused
  *before* the uninstall.
- `--undo` does not move the GitHub App connection. A GitHub org connects to one
  CircleCI org at a time, so to return builds to the source, uninstall the App
  from the new org and reinstall it from the old one yourself.

---

## What does not transfer

| Item | What to do |
|---|---|
| **Fork-PR builds** (`build_fork_prs`), **fork-PR secrets** (`forks_receive_secret_env_vars`), the **OSS flag**, **`pr_only_branch_overrides`** — no GitHub App equivalent | Accept the drop, tell your contributors, and configure trigger filters in the pipeline definition. GitHub App projects detect open-source status from repository visibility. |
| **Group context restrictions** | Recreate with expression or project restrictions in the destination. |
| **Extra pipeline definitions** a plain OAuth project never had | `sync` synthesizes one `build-and-test` definition per project. Create any additional definitions by hand. Definitions already captured from GitHub App pipelines *are* recreated. |
| **Trigger names and descriptions** | Assigned by CircleCI when a trigger is created; recreated triggers carry new names. |
| **Webhook signing secrets and inbound webhook URLs** | New values are generated — see `webhook-secrets.json`. Update every sender and receiver. |
| **Project API tokens** | Recreate with `sync --create-project-tokens` (each mints a **new** value) or by hand, and repoint every consumer. |
| **Identifiers** (project / context / pipeline UUIDs) and **project slugs** | Reassigned. `gh/acme/web` becomes `circleci/<org-uuid>/<project-uuid>`. Update service catalogs, status badges, dashboards, Slack/notification integrations, and GitHub required-status-check names. |
| **Plan / billing**, **same-name orb namespace** | CircleCI Support (Step 5). |
| **SSO (SAML)** | Recreate manually (DNS verification plus IdP setup). |
| **Audit-log streaming, OpenTelemetry exporter headers** | Recreate against destination-owned infrastructure; re-add the headers. |
| **Self-hosted runner instances and registration tokens** | Re-register agents against the destination resource classes. |
| **Deploys history** | Stays in the source. Environments and hierarchies are recreated where the API allows. |
| **Secret values** | Moved by `secrets transfer`, but treat every value as exposed once moved. **Rotate them** after cutover. |

---

## After the cutover

- [ ] Hand each receiver and sender its new webhook value from
      `webhook-secrets.json`, then **delete** the file
- [ ] **Rotate** every secret value that was transferred
- [ ] **Rotate the destination token** stored in `migration-secrets`, and delete
      the `migration-secrets` context from the source org
- [ ] Delete `cutover-state.json` once you are sure you will not undo
- [ ] Repoint external references to the new project slugs
- [ ] Decommission the source org when confident: unfollow its projects, archive
      contexts, and notify your team

---

## Troubleshooting

| Symptom | Cause and fix |
|---|---|
| `secrets transfer` fails: **"the project has no SSH key"** | CircleCI will not start a pipeline that checks out over SSH on a `gh/` or `bb/` project with no **checkout key**, even with inline config. A project needs *either* a **GitHub App pipeline definition** (checks out with the app token — no key needed; `secrets transfer` prefers it automatically) *or* a key. The check runs before anything is changed; an affected project shows as `SKIP` in the plan, is left out on `--apply`, and the command exits non-zero listing it. Fix: **Project Settings → SSH Keys → Checkout SSH keys → Add Deploy Key** (or add a user key) on each listed project, then re-run. **This is why the order matters:** once the GitHub App is uninstalled, the source's App definitions are gone and every project needs a key. Run `secrets transfer` before Step 4.5. |
| **Deploy keys are disabled** in the GitHub organization, so a key cannot be added | A GitHub org or enterprise policy can turn off deploy keys. Add a **user key** instead (Project Settings → SSH Keys → Add User Key), or avoid the need for a key by running `secrets transfer` while the GitHub App is still installed (Step 4.3 before 4.5). A project with a GitHub App definition needs no key at all. |
| `secrets transfer` fails: **"source project(s) are paused"** | A source project is paused with `drop-all-build-requests`, so CircleCI would drop the transfer pipeline. Run the transfer **before** `cutover --pause-source`, or restore the source with `cutover --undo --apply` and re-run. |
| `secrets transfer`: **"no destination project could be resolved"** | A source project has no destination project of the same name and no `projects` entry. Create the missing projects with `sync --apply --skip-pipelines` (before the App moves), or add a `projects` entry to `mapping.json`, then re-run. Add `--skip-contexts` if the contexts already went through — it runs only the per-project pipelines. |
| **Empty Projects page** in the new org for a user | The user has not authorized CircleCI with GitHub for the new org yet. Have them **Authorize** when prompted, then use **Refresh permissions**. Also confirm the repositories are connected to the GitHub App installation in the new org. |
| A project's pipeline definition is `manual` — "connect the repo to the GitHub App" | The repository is not connected to the new App installation. Connect it, then re-run `sync --apply`; the re-run creates the missing definition and trigger. |
| Environment hierarchies are reported **`manual`** | Environment hierarchies are written through a private API that a personal token may not be allowed to use; the first rejected write turns the rest into `manual`. Create the environments by hand under **Deploys → Environments** and the hierarchies under **Deploys → Environment hierarchies**, then assign them. A hierarchy that already exists with different levels is never modified — align it by hand and re-run `sync`. A component that does not exist yet is created by the first `circleci run release plan`. |
| `cutover --enable-dest` reports **`manual`** for a project | An ambiguous trigger match, or a project the manifest shows as paused or not followed, is deliberately left alone. Enable that trigger by hand in **Project Settings → Triggers**. Run `sync --apply` first so the triggers exist. |
| `sync` reports **`manual`** for a context project restriction | The destination project was not found by name. Once the project exists, re-run `sync --apply`. |
| `validate` reports the `migration-secrets` context as missing | Pass `--dest-token-context migration-secrets` (or `--ignore-context <name>`). |
| `Permission denied` / pipeline skipped during `secrets transfer` | The "unversioned config" setting is off in the source org. Re-run with `--enable-trigger`. |

---

## Command quick reference

```bash
# 0. Preflight, export, review
circleci-migrate doctor --source-org gh/acme --dest-org "circleci/$DST_UUID"
circleci-migrate export --source-org gh/acme --dest-org "circleci/$DST_UUID" \
  --output manifest.json --report migration-report.md

# 4.1 Freeze merges
# 4.2 Create destination projects (no pipelines yet)
circleci-migrate sync --manifest manifest.json --mapping mapping.json --skip-pipelines --apply

# 4.3 Transfer secrets (source must still be able to build)
circleci-migrate secrets transfer --manifest manifest.json --mapping mapping.json \
  --dest-org-id "$DST_UUID" --dest-token-context migration-secrets \
  --include-project-vars --include-ssh-keys --remove-restrictions --enable-trigger --apply

# 4.4 Pause the source
circleci-migrate cutover --manifest manifest.json --mapping mapping.json --pause-source --apply

# 4.5 Uninstall the GitHub App from the OAuth org, install from the new org,
#     users Authorize / Refresh permissions

# 4.6 Add pipeline definitions and triggers (answer N at the enable prompt)
circleci-migrate sync --manifest manifest.json --mapping mapping.json \
  --apply --webhook-secrets-out webhook-secrets.json

# 4.7 Enable the destination
circleci-migrate cutover --manifest manifest.json --mapping mapping.json --enable-dest --apply

# 4.8 Validate
circleci-migrate validate --source-org gh/acme --dest-org "circleci/$DST_UUID" \
  --mapping mapping.json --dest-token-context migration-secrets

# Undo
circleci-migrate cutover --undo --apply
```

---

*© CircleCI. Generated for customer use. Commands assume `circleci-migrate`
v0.19.0 or later. Nothing in this runbook deletes anything from the source
organization.*
