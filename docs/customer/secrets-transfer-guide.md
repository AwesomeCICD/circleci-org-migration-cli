---
title: "Transferring Secrets Between CircleCI Organizations"
subtitle: "A guide to moving context and project environment variables org-to-org — without writing secrets to disk"
author: "CircleCI"
date: "2026"
---

# Transferring Secrets Between CircleCI Organizations

**Audience:** CircleCI organization administrators moving environment variables
from one CircleCI organization to another (for example, during a GitHub
organization rename or consolidation).

**Scope:** This guide covers **secrets movement only** — context environment
variables, project-level environment variables, and additional project SSH
keys. It does not cover migrating projects, pipelines, or org settings. (For a
full migration, see the companion *GitHub OAuth → OAuth Migration Guide*, or the
*GitHub OAuth → GitHub App Migration Guide* when the destination is a GitHub App
org.)

**Tooling:** the `circleci-migrate` CLI **v0.19.0 or later**, `secrets transfer`
command. The destination can be a GitHub OAuth org (`gh/<name>`) or a GitHub App
org (`circleci/<uuid>`); the differences are called out where they matter.

> **Why a CLI and not copy/paste?** CircleCI never returns a secret **value**
> through its API — values are write-only by design. The only place a value can
> be read is *inside a running pipeline* of the project or organization that
> owns it. `secrets transfer` uses this fact to move values securely, in-flight,
> without ever writing them to a file, an artifact, or external storage.

> **Prerequisite — destination projects must already exist.** To transfer
> **project** environment variables or SSH keys, the matching destination
> projects must already be **onboarded/created** in the destination org.
> (Destination **contexts** are created automatically; **projects** are not.) A
> source project with no destination counterpart is skipped, shown as `SKIP` in
> the plan, and the command **exits non-zero naming it** — it is never skipped
> silently. Context-only transfers have no such requirement.
>
> - **GitHub OAuth destination:** run your org / project / context sync first —
>   or follow the *GitHub OAuth → OAuth Migration Guide*.
> - **GitHub App destination:** create the projects with
>   `circleci-migrate sync --apply --skip-pipelines` (it creates projects with
>   no pipeline definitions or triggers, so the GitHub App can stay installed on
>   the source until the transfer has run). See §6.

---

## 1. How it works (the security model)

`secrets transfer` runs entirely **inside your source organization's own CI**:

1. You run the CLI from your laptop. It does **not** read any secret values
   locally.
2. The CLI submits a short-lived, inline pipeline to your source org.
3. Inside that pipeline, CircleCI injects the real values (contexts are
   attached to the job; project variables are injected into that project's own
   pipeline).
4. The job sends each value **directly** to the destination organization's API
   over TLS, then exits.

**No secret value ever touches disk, a build artifact, or external storage.**

### The destination token is never embedded

The in-pipeline job needs an API token for the **destination** org. You never
put that token in any file or config. Instead:

1. You store the destination token in a **context in your source org**
   (for example, a context named `migration-secrets` containing a variable
   `CIRCLECI_DEST_TOKEN`).
2. You tell the CLI the **name** of that context.
3. CircleCI injects the token into the job at runtime; the generated pipeline
   config references it only as `${CIRCLECI_DEST_TOKEN}` — the literal value
   never appears anywhere.

> **Trust note:** anyone who can create pipelines in the source org can already
> read its contexts. Use a destination token scoped to the minimum needed
> (context/project write), and **rotate it after the transfer completes**.

---

## 2. What transfers, and what doesn't

| Item | Transfers with `secrets transfer`? | Notes |
|---|---|---|
| **Context environment variables** | ✅ Yes | One pipeline carries all contexts. Missing destination contexts are **created automatically**. |
| **Project environment variables** | ✅ Yes (opt-in: `--include-project-vars`) | One pipeline **per project** (see §5). Destination project must already exist. |
| **Additional project SSH keys** | ✅ Yes (opt-in: `--include-ssh-keys`) | Same in-pipeline zero-disk path as project env vars. Projects are resolved the same way (a `--mapping` entry, or by name for a GitHub App destination). Checkout / deploy keys are *not* moved: for a GitHub OAuth destination they are re-created when you follow the project; a GitHub App destination needs none. |
| **Restricted contexts** | ⚠️ Restricted contexts block the run unless you pass `--remove-restrictions` | See §7. |

---

## 3. Prerequisites

Before you begin, confirm each of the following.

- [ ] **Both organizations exist** in CircleCI. The source uses the GitHub OAuth
      integration (slug `gh/your-org`); the destination is either OAuth
      (`gh/your-org-new`) or a GitHub App org (`circleci/<uuid>`).
- [ ] You are an **organization admin** of *both* the source and destination
      orgs.
- [ ] **Two personal API tokens** (User Settings → Personal API Tokens), one
      whose user admins the source org and one for the destination org.
- [ ] The source org has **"unversioned config" enabled** (Organization
      Settings → Advanced → *Allow API-triggered pipelines to use unversioned
      config*). The CLI will detect this and offer to enable it if it is off.
- [ ] The destination organization's **Organization ID** (a UUID). Find it in
      the destination org's **Organization Settings → Overview**.
- [ ] **(Project env vars and SSH keys only)** The destination **projects are
      already onboarded/created** in the destination org. The transfer
      auto-creates destination *contexts* but **not** projects, and skips (and
      exits non-zero on) any project with no destination counterpart. Run the
      org/project/context sync (or the *GitHub OAuth → OAuth Migration Guide*)
      first; for a GitHub App destination use `sync --apply --skip-pipelines`.
      Context-only transfers do not need this.
- [ ] **(GitHub OAuth `gh/` or Bitbucket `bb/` source)** Each source project
      that will run a transfer pipeline has a **GitHub App pipeline definition**
      *or* a **checkout key** — see §6.
- [ ] **The source org is not paused.** Run `secrets transfer` *before*
      `cutover --pause-source` (see §6).
- [ ] The `circleci-migrate` CLI installed:

```bash
brew install AwesomeCICD/tap/circleci-migrate
circleci-migrate version    # confirm v0.19.0 or later
```

Set your tokens once for the session:

```bash
export CIRCLECI_SOURCE_TOKEN="<source-org-admin-token>"
export CIRCLECI_DEST_TOKEN="<destination-org-admin-token>"
```

---

## 4. Transferring context environment variables

This is the simplest path and the most common need.

### Step 4.1 — Export the source org

`export` is **read-only**. It records the *names* of your contexts, projects,
and variables (never the values) into a manifest the transfer command reads.

```bash
circleci-migrate export \
  --source-org gh/acme \
  --output manifest.json
```

### Step 4.2 — Store the destination token in a source-org context

In the **source** org's UI (Organization Settings → Contexts), create a context
named `migration-secrets` and add one variable:

| Variable | Value |
|---|---|
| `CIRCLECI_DEST_TOKEN` | *the destination org's API token* |

### Step 4.3 — Dry run (no changes)

Always preview first. Without `--apply`, nothing is triggered or written — the
command prints exactly what it *would* do.

```bash
circleci-migrate secrets transfer \
  --manifest manifest.json \
  --dest-org-id <destination-org-uuid> \
  --dest-token-context migration-secrets
```

Review the plan: each context, how many variables, and whether the destination
context would be **created** or **updated**.

### Step 4.4 — Apply

```bash
circleci-migrate secrets transfer \
  --manifest manifest.json \
  --dest-org-id <destination-org-uuid> \
  --dest-token-context migration-secrets \
  --enable-trigger \
  --apply
```

This triggers one pipeline in the source org that reads each context's values
and writes them to the matching destination context (creating it if needed).

> `--enable-trigger` lets the CLI enable the "unversioned config" org setting if
> it is currently off. Omit it if you have already enabled it manually.

To transfer **only specific contexts**, repeat `--context`:

```bash
circleci-migrate secrets transfer ... --context deploy-prod --context shared --apply
```

---

## 5. Transferring project environment variables

Project variables need one extra concept and one extra flag.

### Why it's different

CircleCI **project** environment variables are *project-scoped*: their values
are only readable inside **that project's own** pipeline. So the CLI runs **one
pipeline per source project**, each under its own project, so every project's
values are injected correctly. These run in parallel.

The destination **project must already exist** (be onboarded) in the
destination org. And the CLI needs to know which source project maps to which
destination project:

- **GitHub App destination (`circleci/<uuid>`):** a source project with no
  mapping entry is matched **by name** against the destination org's projects
  and written to its real `circleci/<org-id>/<project-id>` slug. **A `mapping.json`
  containing only the `org` line is enough**; `mapping generate` is optional (use
  it to review or override a match). The dry-run plan prints every
  `source → destination` pair it will write to, marked `matched by name` when it
  was not in the mapping.
- **GitHub OAuth destination (`gh/<name>`):** generate the mapping as below, or
  rely on the `org` line plus matching repository names.

A mapping entry always wins over the name match. A project that matches neither
is shown as `SKIP`, counted as unresolved, and the run exits non-zero naming it.

### Step 5.1 — Generate the project mapping (optional for a GitHub App destination)

The `mapping generate` command matches your source projects to the destination
org's projects **by repository name** and writes a `mapping.json`, plus a report
of anything that didn't match.

```bash
circleci-migrate mapping generate \
  --manifest manifest.json \
  --dest-org gh/acme-new \
  -o mapping.json
```

The report has three sections:

- **Matched** — written to `mapping.json` (e.g. `gh/acme/web → gh/acme-new/web`).
- **Unmatched source** — a source project with no destination counterpart.
  Onboard that repo in the destination org first, or add a manual entry to
  `mapping.json`.
- **Destination-only** — destination projects with no source match (just
  informational).

Review `mapping.json` and the report before continuing. For a GitHub App
destination you can skip this step: with just
`{ "org": { "from": "gh/acme", "to": "circleci/<uuid>" } }` in `mapping.json`,
projects are matched by name when you run the transfer.

### Step 5.2 — Dry run with project vars

```bash
circleci-migrate secrets transfer \
  --manifest manifest.json \
  --dest-org-id <destination-org-uuid> \
  --dest-token-context migration-secrets \
  --mapping mapping.json \
  --include-project-vars
```

The plan now also lists, per project, which variables would transfer and which
projects are **skipped** (`SKIP`: no mapping entry and no destination project of
that name → create it first). The dry run ends with a `WARNING: --apply would
fail` block if anything would stop the real run.

### Step 5.3 — Apply

```bash
circleci-migrate secrets transfer \
  --manifest manifest.json \
  --dest-org-id <destination-org-uuid> \
  --dest-token-context migration-secrets \
  --mapping mapping.json \
  --include-project-vars \
  --enable-trigger \
  --apply
```

The summary reports how many context and project pipelines succeeded.

### Contexts already transferred? Use `--skip-contexts`

If the contexts already went through (for example in an earlier run), re-run
with `--skip-contexts` to run **only** the per-project pipelines:

```bash
circleci-migrate secrets transfer \
  --manifest manifest.json \
  --dest-org-id <destination-org-uuid> \
  --dest-token-context migration-secrets \
  --mapping mapping.json \
  --skip-contexts --include-project-vars --include-ssh-keys \
  --enable-trigger --apply
```

`--skip-contexts` needs `--include-project-vars` and/or `--include-ssh-keys`
(there would be nothing to transfer otherwise) and cannot be combined with
`--context`. No host project is resolved, no context restriction is touched, and
no context pipeline runs.

---

## 6. Checkout keys and the host project

*Applies to GitHub OAuth (`gh/`) and Bitbucket (`bb/`) **source** projects.
`circleci/` projects are not affected.*

CircleCI refuses to start a pipeline that checks out over SSH on a `gh/` or
`bb/` project that has no **checkout key**, even when the config is inline
("the project has no SSH key"). `secrets transfer` runs one pipeline under a
**host project** (for contexts) and one under **each source project** (for
`--include-project-vars` / `--include-ssh-keys`), so each of those projects
needs one of:

1. **A GitHub App pipeline definition.** In a GitHub OAuth org that also has the
   CircleCI GitHub App installed, every project has GitHub App pipeline
   definitions in addition to its implicit OAuth one. They check out with the
   app token and **need no key**. `secrets transfer` **prefers a GitHub App
   definition automatically** and logs which definition each run used. Disabled
   OAuth triggers do not matter for API-triggered runs.
2. **A checkout key** — a deploy key or a user key (Project Settings → SSH Keys →
   Checkout SSH keys). This is what a project with *only* the OAuth definition
   needs. Keys are read from the manifest, or looked up live if the manifest was
   exported with `--skip-extras`.

**Host project.** Unless you pass `--host-project`, the CLI auto-picks the first
manifest project with a GitHub App definition, else the first with a pipeline
definition and a checkout key, and prints which one it chose and why. An
explicit `--host-project` with neither is rejected. All of this is checked
**before** any context restriction is removed or trigger flag enabled, so a
doomed run changes nothing. A per-project transfer with neither is shown as
`SKIP`, left out on `--apply`, and the command exits non-zero listing it; the
other projects still run.

**Order matters when the destination is a GitHub App org.** Once the CircleCI
GitHub App is uninstalled from the OAuth org, its App definitions are gone, so a
project-var or SSH-key transfer after that point needs a checkout key on every
project (contexts transferred *before* the uninstall do not). Run
`secrets transfer` **first**.

**A paused source fails fast.** If you are using `circleci-migrate cutover`, the
transfer must run **before** `cutover --pause-source`: a project paused with
`drop-all-build-requests` drops the transfer pipeline. `secrets transfer`
checks this up front, names every paused project, and stops before changing
anything. Run the transfer first, or restore the source with
`cutover --undo --apply` and re-run.

---

## 7. Restricted contexts and SSH keys

### Restricted contexts

**Project-type and expression restrictions** block the transfer pipeline by
default when the host project is not in the allowed set — the workflow returns
"unauthorized."

> **Context restrictions and `sync`:** when you run the full migration (`sync
> --apply`), **project-type restrictions are remapped and recreated
> automatically** on the destination via the project mapping (for a GitHub App
> destination, by project name). **Group**
> restrictions require manual recreation in the destination UI — group IDs
> differ between orgs and cannot be remapped automatically.

**Default behavior (no flag):** the CLI detects blocking restrictions from the
manifest at plan time and fails fast with an actionable error listing the affected
contexts and instructions to re-run with `--remove-restrictions`.

**With `--remove-restrictions`:** the CLI temporarily removes project/expression
restrictions from the source context, triggers the transfer pipeline, then
restores them afterwards (best-effort). Group restrictions (including the default
"All members") are **never** touched.

```bash
circleci-migrate secrets transfer \
  --manifest manifest.json \
  --dest-org-id <destination-org-uuid> \
  --dest-token-context migration-secrets \
  --remove-restrictions \
  --enable-trigger \
  --apply
```

If restore fails (e.g. due to a transient error), the CLI prints a `WARNING`
with the exact restriction to re-add manually — no restriction is silently lost.

### SSH keys

`--include-ssh-keys` transfers **additional project SSH keys** via the same
in-pipeline zero-disk path: the job uses `add_ssh_keys` to materialize each key,
reads the private key with `jq --rawfile` (never echoed to logs), and POSTs it
to the destination project. Requires a `--mapping` entry for each project.

**Checkout / deploy keys** are *not* moved by `secrets transfer`. For a GitHub
OAuth destination they are re-created automatically when you follow the project
in the destination org (re-add any manually-uploaded deploy keys in the
destination project settings); a GitHub App destination checks out through the
GitHub App and needs none. (Do not confuse these with the *source-side* checkout
key requirement in §6.)

---

## 8. Verify

In the **destination** org UI:

- **Organization Settings → Contexts** — every expected context is present with
  the expected number of variables.
- **Project Settings → Environment Variables** — for a sample of projects,
  variable **names** are present (values are masked, as always).

For a structured parity check, use the **`validate`** command — it exports both
orgs read-only and prints a per-section report (Contexts, Projects, Org
Settings, Runners, Orbs, CIAM) with ✓ matched / ✗ missing / ⚠ manual items,
a NEEDS ATTENTION summary, and an exit code of 1 if anything is missing:

```bash
circleci-migrate validate \
  --source-org gh/acme \
  --dest-org gh/acme-new \
  --mapping mapping.json \
  --dest-token-context migration-secrets
```

`--dest-token-context` leaves the `migration-secrets` context out of the
comparison — it exists only to run the transfer and is never recreated on the
destination. (Repeat `--ignore-context <name>` for any other migration-only
context.)

> **Note:** secret *values* are never compared — the API masks them. `validate`
> checks presence and structure (names, counts, settings, restrictions).

Alternatively, a quick manual cross-check:

```bash
circleci-migrate export \
  --source-org gh/acme-new \
  --source-token "$CIRCLECI_DEST_TOKEN" \
  --output manifest-dest.json
```

Compare context and project variable counts between `manifest.json` and
`manifest-dest.json`.

---

## 9. Security checklist (do this after transfer)

- [ ] Confirmed destination contexts/projects have the expected variables.
- [ ] **Rotated the destination API token** stored in `migration-secrets`.
- [ ] Deleted the `migration-secrets` context from the source org (or rotated
      its token) once the transfer is complete.
- [ ] Considered rotating any high-sensitivity values as a hygiene step.
- [ ] Used a **private** project as the host for the transfer pipeline.

> `secrets transfer` never writes values to disk or artifacts, so there is no
> local file to delete — one of its key advantages.

---

## 10. Troubleshooting

| Symptom | Cause / fix |
|---|---|
| `Permission denied` mid-run / pipeline skipped | "Unversioned config" is off in the source org. Re-run with `--enable-trigger`, or enable it in Org Settings → Advanced. |
| A project shows `SKIP … dest project unknown` | No mapping entry (and, for a GitHub App destination, no destination project of that name). Onboard the repo in the destination first if needed, then run `mapping generate` or add the entry to `mapping.json`. |
| `We did not run this pipeline because the project has no SSH key` | A `gh/`/`bb/` source project has neither a GitHub App pipeline definition nor a checkout key. Add a deploy key (or user key) under Project Settings → SSH Keys → Checkout SSH keys, or give it an App definition, then re-run. See §6. If the GitHub App was already uninstalled from the source org, its App definitions are gone and every project needs a key. |
| `N source project(s) are paused (drop-all-build-requests=true)` | The source was paused by `cutover --pause-source`. Run the transfer **before** the pause, or restore it with `cutover --undo --apply` and re-run. |
| `N project(s) skipped because no destination project could be resolved` | A source project has no destination project of the same name and no mapping entry. Create it first (`sync --apply --skip-pipelines` for a GitHub App destination), or add the entry to `mapping.json`, then re-run — with `--skip-contexts` if the contexts already went through. |
| Project variables transferred as **empty** | You are on an older CLI. Upgrade to **v0.19.0 or later** (empty values were fixed in v0.17.1 by running one pipeline per project so each project's values inject correctly). |
| `--dest-org-id` rejected | You passed a slug. This flag needs the destination **Organization ID (UUID)** from Org Settings → Overview. |
| Destination context not created | The destination org must exist and the token must be an org admin token. |

---

## Command quick reference

```bash
# 1. Export source (read-only)
circleci-migrate export --source-org gh/acme --output manifest.json

# 2. (project vars / SSH keys only) Generate the mapping.
#    Optional for a GitHub App destination: projects are matched by name.
circleci-migrate mapping generate --manifest manifest.json --dest-org gh/acme-new -o mapping.json

# 3. Dry run
circleci-migrate secrets transfer --manifest manifest.json \
  --dest-org-id <uuid> --dest-token-context migration-secrets \
  [--mapping mapping.json] [--include-project-vars] [--include-ssh-keys]

# 4. Apply
circleci-migrate secrets transfer --manifest manifest.json \
  --dest-org-id <uuid> --dest-token-context migration-secrets \
  [--mapping mapping.json] [--include-project-vars] [--include-ssh-keys] \
  [--skip-contexts] [--remove-restrictions] [--host-project gh/acme/<repo>] \
  --enable-trigger --apply

# 5. Verify
circleci-migrate validate --source-org gh/acme --dest-org gh/acme-new --mapping mapping.json \
  --dest-token-context migration-secrets
```

---

*© CircleCI. Generated for customer use. Commands assume `circleci-migrate`
v0.19.0 or later.*
