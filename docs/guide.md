# Migration guide

This is the single end-to-end walkthrough for migrating one CircleCI
organization to another with `circleci-migrate`. It covers the org types you
can migrate, the prerequisites and token permissions you need, and the core
flow: **export → secrets capture → sync**. Per-org-type variations are called
out as sections within each step.

If you just want the operator checklist for a production cutover, use the
[cutover runbook](cutover-runbook.md). For full per-command flag tables, see the
[generated CLI reference](cli/README.md). For problems, see
[troubleshooting](troubleshooting.md).

> Throughout, examples use the fictional orgs `gh/acme` (source) and
> `gh/acme-new` (destination). Substitute your own slugs.

---

## Installing behind a restricted network / proxy

Enterprise networks commonly allowlist outbound traffic by domain. The required
domains differ by install method.

### Prebuilt binary (recommended)

The README curl snippet and any browser download of a GitHub release asset
contact two hosts:

| Domain | Purpose |
|---|---|
| `api.github.com` | Resolve the latest release tag (`/releases/latest`) |
| `github.com` | Download the release tarball |
| `release-assets.githubusercontent.com` | GitHub redirects archive fetches here |

Homebrew (`brew install`) additionally contacts `formulae.brew.sh` and
`raw.githubusercontent.com` for the tap formula.

### `go install github.com/AwesomeCICD/circleci-org-migration-cli@<version>`

Go resolves module downloads through its module proxy and checksum database,
plus several vanity-import domains used by the module's dependencies (confirmed
against `go.mod`):

| Domain | Purpose |
|---|---|
| `github.com` | Source repository and all `github.com/*` dependencies |
| `proxy.golang.org` | Go module proxy (download cache) |
| `sum.golang.org` | Go checksum database |
| `filippo.io` | `filippo.io/age`, `filippo.io/edwards25519`, `filippo.io/hpke` |
| `golang.org` | `golang.org/x/crypto`, `golang.org/x/term`, `golang.org/x/sys` |
| `gopkg.in` | `gopkg.in/yaml.v3` |
| `go.yaml.in` | `go.yaml.in/yaml/v3` |

To bypass the module proxy entirely (for example, if only `github.com` is
reachable), set:

```bash
GOPROXY=off go install github.com/AwesomeCICD/circleci-org-migration-cli@v0.8.1
```

This instructs Go to fetch directly from VCS instead of the proxy. You still
need the vanity-import hosts above because Go fetches their `go-import` metadata
via HTTPS even in `GOPROXY=off` mode.

To route through a corporate proxy, set the standard `HTTPS_PROXY` (or
`GOPROXY=https://your-proxy`) before running `go install`.

### Build from source (git clone + `go build`)

Clone access requires only `github.com`. The `go build` step resolves the same
vanity-import hosts listed above unless the tree is vendored:

```bash
# Vendored build — only github.com needed (for the clone itself)
git clone https://github.com/AwesomeCICD/circleci-org-migration-cli.git
cd circleci-org-migration-cli
GOFLAGS=-mod=vendor go build -o circleci-migrate .
```

This requires a `vendor/` directory in the cloned tree. The repository does not
currently ship a vendored tree; a future release may attach a vendored source
tarball (`go mod vendor` snapshot) to each GitHub release so that
`go build -mod=vendor` works with `github.com` access only. Until then, the
vanity-import hosts above must be reachable for an unvendored build.

---

## 1. The model

`circleci-migrate` works in two halves:

1. **`export`** reads everything it can from the source org and writes a
   non-secret `manifest.json` plus a human-readable `migration-report.md`. The
   manifest *is* the exported source data: structure and names only, never
   secret values. It is safe to review, diff, and store.
2. **`sync`** replays a manifest into the destination org. It is a **dry run by
   default**; add `--apply` to write.

Because the CircleCI API never returns secret values, a separate **`secrets
capture`** step runs a short-lived pipeline inside the source org to collect
them — encrypted by default, never stored in plain text.

`migrate` is the all-in-one command that runs export → sync in one step (with an
interactive walkthrough when run with no flags).

#### Interactive walkthrough — what gets migrated (Step 3)

When you run `migrate` with no flags on a TTY, Step 3 of the guided walkthrough
presents a multi-select menu of components to migrate.  All components are
selected by default:

- **contexts** — org-level contexts and their environment variables
- **projects** — followed projects, env vars, checkout keys, and pipeline definitions
- **org settings** — feature flags, OIDC providers, URL-orb allow list, config policies
- **extras** — checkout keys, webhooks, and schedules
- **orbs** — published orb versions under your namespace (new in v0.13.0)
- **runners** — self-hosted runner resource classes (new in v0.13.0)

When you select **orbs** or **runners**, the walkthrough immediately asks for
the source and destination namespaces, defaulting to the org's short name (e.g.
`acme` from `gh/acme`).  For `circleci/<uuid>` (App) orgs, the default is
empty and you type the namespace yourself.  Leaving a namespace blank skips that
component.  There is no API to auto-resolve the namespace from an org name, so
you must confirm or type the value.

To migrate orbs/runners non-interactively, pass the explicit namespace flags:
`--orb-namespace` / `--dest-orb-namespace` and `--runner-namespace` /
`--dest-runner-namespace`.

#### Interactive walkthrough — secrets step (Step 3a)

When you run `migrate` with no flags on a TTY, the guided walkthrough asks how
you want to move secret **values** to the destination.  Step 3a presents three
choices (in-pipeline transfer is the recommended default):

1. **in-pipeline transfer (RECOMMENDED)** — runs a pipeline in the SOURCE org
   that pushes context and (optionally) project env-var values and SSH keys
   directly to the destination.  No plaintext is written to disk.  Requires a
   destination API token stored in a source-org context (`CIRCLECI_DEST_TOKEN`).
   Follow-up prompts collect: the context name, whether to include project vars,
   SSH keys, whether to temporarily lift context restrictions, and an optional
   host-project override (blank = auto-pick).
2. **captured secrets bundle (advanced)** — supply a `secrets.json` produced by
   `secrets capture`.  Follow-up prompts ask for the bundle path and how to
   handle missing values (`skip` or `placeholder`).
3. **none** — migrate structure only; set values manually later.  The
   missing-values step still asks how to handle empty variables.

### One-command migration with in-pipeline secret transfer

The in-pipeline transfer is also available non-interactively.  Pass
`--transfer-secrets` to move context and project env-var values **in-pipeline**
(zero-disk) as part of the `migrate` command — no bundle file required:

```bash
circleci-migrate migrate \
  --source-org github/acme --dest-org github/acme-new \
  --transfer-secrets --dest-token-context migration-secrets \
  --include-project-vars --apply --yes
```

- `--transfer-secrets` — after sync creates+follows the destination projects,
  run the in-pipeline transfer for context env vars (and project env vars with
  `--include-project-vars`). Mutually exclusive with `--secrets`.
- `--dest-token-context <name>` — the source-org context holding the destination
  API token (`CIRCLECI_DEST_TOKEN`). Required with `--transfer-secrets`.
- `--include-project-vars` — also transfer project-level env-var values
  (destination project slugs are derived from `--dest-org`).
- `--include-ssh-keys` — also transfer additional project SSH keys in-pipeline
  (zero-disk; private key material is read with `jq --rawfile` and never echoed
  to logs). Destination project slugs are derived from `--dest-org`. A project
  with SSH keys but no env vars still triggers a per-project pipeline.
- `--host-project <slug>` — the source project whose pipeline runs the context
  transfer. Defaults to the first project; prefer an **established** (long-
  followed) project, since a just-followed project's context authorization may
  not have propagated yet.

### Preflight checks

Before work begins, each command runs **preflight checks** that detect common
configuration issues and print a ✅/⚠️/❌ summary:

| Command | Checks run | Hard fail conditions |
|---|---|---|
| `migrate` | All of the below | Missing token; dest org unreachable |
| `export` | Source token, source org reachable, api-trigger flag, project discovery | Missing source token |
| `sync` | Dest token, dest org reachable, cross-type warning, GitHub token | Missing dest token; dest org unreachable |
| `doctor` | Source- and/or dest-side checks (see below) | Same as respective command |

Full check table for `migrate` (and `doctor --source-org … --dest-org …`):

| Check | Hard fail? |
|---|---|
| Source + destination tokens set | Yes (migration cannot proceed) |
| Destination org reachable | Yes (migration cannot proceed) |
| Source org reachable | No (warn + continue) |
| Cross-type migration (e.g. OAuth → standalone) | No (warn; see [playbooks](playbooks/cross-type-oauth-to-app.md)) |
| `allow_api_trigger_with_config` on source org | No (warn; enabled automatically during secrets capture) |
| Project discovery count + path | No (warn if private API unavailable) |
| GitHub token for repo resolution | No (warn if cross-type and `--github-token` absent) |

On an interactive TTY, warnings prompt "Continue? [Y/n]" before proceeding. On a
non-TTY (CI), warnings are printed and the command proceeds automatically.

#### `--preflight-only` (migrate)

Run the full preflight and exit without doing export or sync. Useful for
validating configuration before committing to a migration:

```bash
circleci-migrate migrate \
  --source-org gh/acme --dest-org gh/acme-new \
  --preflight-only
# Exit 0 on OK/warnings; exit 1 on hard failures.
```

#### `--skip-preflight`

Available on `migrate`, `export`, and `sync`. Skips all preflight checks for
the respective command. Use in CI pipelines where checks have been validated in a
prior step (e.g. via `doctor`), or to speed up repeated runs:

```bash
# Skip preflight on migrate (already validated via doctor):
circleci-migrate migrate \
  --source-org gh/acme --dest-org gh/acme-new \
  --skip-preflight --apply --yes

# Skip preflight on export:
circleci-migrate export --source-org gh/acme --skip-preflight

# Skip preflight on sync:
circleci-migrate sync --manifest manifest.json --apply --skip-preflight
```

#### `doctor` — standalone preflight command

`doctor` runs preflight checks without migrating. It is entirely **read-only**
and safe to run as many times as needed:

```bash
# Check both source and destination before a full migration:
circleci-migrate doctor --source-org gh/acme --dest-org gh/acme-new

# Source-side only (validate before export):
circleci-migrate doctor --source-org gh/acme

# Destination-side only (validate before sync):
circleci-migrate doctor --dest-org gh/acme-new
```

Exit codes: `0` = all checks OK or warnings only; `1` = hard failure (missing
required token or unreachable org).

For the authoritative list of **what does NOT transfer** and requires manual
follow-up, see the cutover runbook:
[Manual steps required](cutover-runbook.md#3-manual-steps-required) and
[Does not transfer / data loss](cutover-runbook.md#4-does-not-transfer--data-loss).

---

## 2. Org types

The **org slug format** controls which APIs the tool uses and which features
apply. Find your slug in the org's CircleCI URL.

| Org type | Slug format | Example | Notes |
|---|---|---|---|
| **GitHub OAuth** | `gh/<org>` | `gh/acme` | Legacy integration. Full v1.1 + v2 API coverage. Projects are *followed* to install webhooks. OAuth-only build flags (`oss`, `build_fork_prs`, `forks_receive_secret_env_vars`, `pr_only_branch_overrides`) apply. |
| **GitHub App** | `circleci/<org-id>` | `circleci/22222222-2222-2222-2222-222222222222` | v2 API only. Projects use pipeline definitions + triggers (created **disabled**). Repos identified by numeric GitHub `external_id`. |
| **CircleCI standalone** | `circleci/<org-id>` | `circleci/<uuid>` | Standalone (non-VCS-OAuth) orgs. Supports **CIAM** roles and groups — synced unless you pass `--skip-ciam`. |
| **GitLab (App)** | `circleci/<org-id>` | `circleci/<uuid>` | Uses the `circleci/<org-id>` slug like GitHub App. |

**Same-type migrations** (OAuth→OAuth, App→App, standalone→standalone) are fully
automated. Cross-type (OAuth→App) and repo-move scenarios are covered in
[§7 Scenarios](#7-scenarios-by-org-type).

> **Mixed orgs:** when one GitHub org has *both* the OAuth and the GitHub App
> integration, CircleCI registers them as **two separate org records** (one
> `gh/<org>`, one `circleci/<uuid>`). Migrate each leg separately — see
> [§7](#7-scenarios-by-org-type).

---

## 3. Prerequisites & token permissions

### CircleCI API tokens

You need a **personal API token** for each side:

| Token | Env var | Used for |
|---|---|---|
| Source token | `CIRCLECI_SOURCE_TOKEN` | Reading the source org (export, capture) |
| Destination token | `CIRCLECI_DEST_TOKEN` | Writing the destination org (sync) |
| Fallback token | `CIRCLECI_CLI_TOKEN` / `CIRCLE_TOKEN` | Used for both when the specific tokens are unset |

Create tokens at **User Settings → Personal API Tokens**. The token's user must
be an **organization admin** of the org it acts on:

- **Source:** admin/read access to contexts, projects, and org settings; ability
  to trigger pipelines (for `secrets capture`).
- **Destination:** admin to create contexts, projects, pipeline definitions,
  triggers, and to write org settings.

Set them as environment variables so you never pass tokens on the command line:

```bash
export CIRCLECI_SOURCE_TOKEN="<source-org-admin-token>"
export CIRCLECI_DEST_TOKEN="<destination-org-admin-token>"
```

### GitHub token (only for repo moves)

If repos have moved to a **different GitHub org** (e.g. an EMU migration), CircleCI's
GitHub App identifies each repo by its numeric GitHub ID, which changes when the
repo moves. Supply a GitHub PAT with **repo read** so the tool can resolve the
new IDs:

```bash
export GITHUB_TOKEN="<github-pat-with-repo-read>"
```

This is **not** needed for same-GitHub-org migrations.

### CircleCI Server / self-hosted (`--host`)

For CircleCI Server (or any non-cloud host), point every command at your install
with `--host` (or `CIRCLECI_CLI_HOST` / `CIRCLECI_HOST` / `CIRCLE_URL`):

```bash
circleci-migrate export --source-org gh/acme --host https://circleci.example.com
```

The default is `https://circleci.com`.

### Using the official `circleci` CLI

`circleci-migrate` is also a plugin to the official
[circleci CLI](https://circleci.com/docs/local-cli/). When invoked as
`circleci run migrate <args>`, the `circleci` CLI execs `circleci-migrate` on
your `PATH` and injects `CIRCLE_TOKEN` and `CIRCLE_URL` — no extra token/host
flags needed:

```bash
circleci run migrate export --source-org gh/acme
circleci run migrate sync   --manifest manifest.json --apply
```

Bare `circleci migrate` (without `run`) is **not** supported.

---

## 4. Step 1 — Export the source org

`export` is read-only and safe to re-run.

```bash
circleci-migrate export --source-org gh/acme
# Produces: manifest.json  migration-report.md
```

Common options:

```bash
circleci-migrate export \
  --source-org gh/acme \
  --output manifest.json \
  --report migration-report.md
```

Then **review `migration-report.md`** — it lists everything captured and the
manual follow-ups that apply to *your* org.

### Scoping what is exported

- `--project gh/acme/web --project gh/acme/api` — export only specific projects
  (repeat the flag). Default is all discovered projects.
- The `--skip-*` family limits what is read:
  - `--skip-contexts` — skip contexts.
  - `--skip-projects` — skip projects.
  - `--skip-extras` — skip checkout keys, webhooks, and schedules.

### Follow all GitHub repos (opt-in, GitHub OAuth orgs only)

The exporter discovers projects via CircleCI's private org-projects API, which
covers all onboarded projects. The remaining gap: GitHub repos that were **never
set up in CircleCI** won't appear. `--follow-all` closes that gap for GitHub
OAuth (`gh/`) orgs by following every un-onboarded repo before the export runs.

```bash
circleci-migrate export --source-org gh/acme \
  --follow-all --github-token $GITHUB_TOKEN
```

Rules:
- Requires `--github-token` (or `$GITHUB_TOKEN`). Returns an error if absent.
- Archived GitHub repos are skipped.
- Brand-new repos with no branch may trigger a webhook-validation warning —
  these are warned and skipped, not fatal; all other repos are still followed.
- Not applicable to `circleci/` (App/standalone) orgs — a note is printed and
  the flag is ignored.

The preflight step (which runs by default before export) also offers to run
follow-all interactively when `--github-token` is set, or prints a suggestion
to re-run with `--follow-all --github-token` in non-interactive mode.

### Self-hosted runner resource classes

There is no clean org→namespace lookup, so you must name the runner namespace
explicitly:

```bash
circleci-migrate export --source-org gh/acme --runner-namespace acme-runners
```

### Orb namespace capture (opt-in)

Use `--orb-namespace` to capture all published orbs (public and private) under a
namespace, along with every stable version and its raw YAML source. The namespace
must be supplied explicitly — there is no clean org→namespace lookup.

```bash
circleci-migrate export --source-org gh/acme --orb-namespace acme
```

After export, captured orbs appear in the manifest. During sync, supply
`--dest-orb-namespace` to republish them into the destination namespace.

### Usage data snapshot (opt-in)

`--include-usage` also downloads a historical usage report (gzip CSV) from the
CircleCI Usage API into a `usage/` directory next to the manifest. **This is a
local baseline/record only — it does NOT transfer to the destination.**

```bash
circleci-migrate export --source-org gh/acme --include-usage \
  --usage-start 2026-01-01T00:00:00Z --usage-end 2026-01-31T23:59:59Z
```

The default window is the last 30 days; the max window is 31 days (API limit).
If the usage export fails, the main export still succeeds with a warning.

### Machine-readable output

Add `--json` to print a JSON summary to stdout instead of the human-readable
summary (the manifest and report files are still written). Useful in CI.

---

## 5. Step 2 — Move secret values

Env-var and context **values** are masked by the API. Two approaches are
available:

- **`secrets transfer`** (recommended) — triggers an inline pipeline in the
  source org that reads each value and PUTs it directly to the destination org
  over TLS. No plaintext ever touches disk or artifacts.
- **`secrets capture`** (alternative) — captures values into a local encrypted
  bundle (`secrets.json`) that `sync` then reads. Use this when you need a
  reviewable local copy, want to migrate SSH keys via the bundle path, or cannot
  use the in-pipeline transfer.

### Recommended: `secrets transfer` (zero-disk-write)

`secrets transfer` triggers a single dynamic pipeline in the SOURCE org with one
job per context. Each job imports the context (CircleCI unmasks the values into
the job environment) and PUTs each value directly into the matching context in the
DESTINATION org via the CircleCI API over TLS.

**No plaintext ever touches disk or artifacts** — strictly better security for
context variables than the bundle-artifact flow.

**Create-missing destination contexts:** if a destination context does not yet
exist, the in-pipeline job creates it automatically before setting values.
Running `sync --apply` first is no longer required for contexts.

**Trust model:** The CLI embeds the context NAME (not the token value) in the
generated pipeline config. CircleCI injects the token as an env var inside the
job. Source-org admins with access to the token context have implicit access to
the destination token — use a scoped token and rotate it after transfer.

**Dry-run by default** (like `sync`). Pass `--apply` to execute. The dry-run
plan shows each context with `[create]` or `[update]` based on intent.

```bash
# 1. Store dest token in a source-org context, then dry-run the plan:
circleci-migrate secrets transfer --manifest manifest.json \
  --dest-org-id <dest-org-uuid> \
  --dest-token-context migration-secrets

# 2. Execute the transfer:
circleci-migrate secrets transfer --manifest manifest.json \
  --dest-org-id <dest-org-uuid> \
  --dest-token-context migration-secrets \
  --enable-trigger --apply

# 3. Also transfer project env vars (requires mapping.json with project entries):
circleci-migrate secrets transfer --manifest manifest.json \
  --dest-org-id <dest-org-uuid> \
  --dest-token-context migration-secrets \
  --mapping mapping.json \
  --include-project-vars \
  --apply
```

Key flags:
- `--dest-org-id` — destination org UUID (find it in `manifest.json` under
  `source.org.id`, or in the CircleCI org settings page).
- `--dest-token-context` — name of the source-org context holding the dest token.
- `--dest-token-env-var` — env-var name inside that context (default:
  `CIRCLECI_DEST_TOKEN`).
- `--dest-host` — override for CircleCI Server installations (default:
  `https://circleci.com`).
- `--apply` — execute the pipeline (omit for dry-run).
- `--context` — limit to specific context names; default is all contexts with
  values.
- `--include-project-vars` — also transfer project env-var values (default:
  off). Each source project must be resolvable to a destination project slug
  via `--mapping`; projects without a mapping entry are **skipped** and clearly
  flagged in the plan. The destination project must already be onboarded.
- `--include-ssh-keys` — also transfer additional project SSH keys via the
  in-pipeline zero-disk path (default: off). The in-pipeline job uses
  `add_ssh_keys` to materialize each key, matches by SHA256 fingerprint, reads
  the private key with `jq --rawfile` (never echoed to logs), and POSTs it to
  the destination project. A project with SSH keys but no env vars still triggers
  a per-project pipeline. Requires a `--mapping` entry for each project.
- `--mapping` — optional path to `mapping.json`. Entries in `projects` whose
  keys contain `/` are project slug overrides (source → dest project slug);
  entries whose keys have no `/` are context name → destination context name
  overrides.
- `--remove-restrictions` — contexts with **project or expression restrictions**
  block the transfer pipeline by default (the host project must be in the allowed
  set). With this flag the CLI temporarily removes those restrictions before
  triggering the pipeline and restores them afterwards (best-effort). The default
  "All members" group restriction is never touched. Re-run with this flag when
  the dry-run plan shows `WARN: blocking restrictions` for a context.

**Scope:** context env-var values by default; add `--include-project-vars` for
project env vars and `--include-ssh-keys` for additional project SSH keys.

### Alternative: `secrets capture` bundle flow

`secrets capture` runs a short-lived pipeline inside the **source** org that
dumps the values to an artifact, downloads it, and writes a local `secrets.json`.
It commits **no** config to your repo (it submits an inline/unversioned config).

#### Interactive (recommended for first-time use)

Run on a TTY with no flags to launch the guided walkthrough:

```bash
circleci-migrate secrets capture
```

It prompts for the manifest, which contexts/projects to capture, the host
project for context extraction, encryption, storage, and artifact retention.

#### Non-interactive (CI-safe)

Once `--manifest` is supplied (or stdin is not a TTY), capture runs
non-interactively. **Fail-closed guard:** if neither `--context` nor `--project`
is set and you have not passed `--yes` (or `--no-input`), an unattended
capture-all errors out instead of sweeping every context/project. Scope it, or
acknowledge with `--yes`:

```bash
# Encrypted with an auto-generated key + 1-day retention (recommended)
circleci-migrate secrets capture \
  --manifest manifest.json \
  --encrypt --generate-key \
  --artifact-retention-days 1 \
  --enable-trigger \
  --output secrets.json

# Scope to specific contexts / projects
circleci-migrate secrets capture --manifest manifest.json \
  --context deploy-prod --host-project gh/acme/web --enable-trigger
```

#### Encryption (on by default)

Encryption is **on by default** so plaintext secrets never persist in CircleCI
artifact storage. Supply a recipient with `--generate-key` (creates a fresh age
keypair) or `--ssh-public-key`/`--ssh-private-key` (use an existing SSH key).
Use `--no-encrypt` to opt out (a plaintext artifact — strongly discouraged).

```bash
# Existing SSH key
circleci-migrate secrets capture --manifest manifest.json --encrypt \
  --ssh-public-key ~/.ssh/id_ed25519.pub --ssh-private-key ~/.ssh/id_ed25519 \
  --artifact-retention-days 1 --enable-trigger --output secrets.json
```

#### SSH keys (on by default)

`secrets capture` also extracts **additional project SSH private keys** that are
cataloged in the manifest, via a separate in-pipeline job that uses
`add_ssh_keys` with the explicit fingerprints (the checkout/deploy key is never
materialised). This is **on by default**; pass `--ssh-keys=false` to skip it (for
example, an env-var-only capture).

#### Storage (`--storage`)

- `artifact` (default) — store the bundle as a CircleCI job artifact.
- `s3` — upload to S3 only (requires the `aws` CLI + AWS creds in the job;
  provide `--s3-bucket` and optionally `--s3-prefix`).
- `both` — store in both.

```bash
circleci-migrate secrets capture --manifest manifest.json --generate-key \
  --storage s3 --s3-bucket my-migration-bucket --s3-prefix migration/
```

#### Restricted contexts (capture path)

If a context has restrictions that block the inline pipeline:

- `--skip-restricted-contexts` (default: true) — skip them and attach a warning.
- `--remove-restrictions` — temporarily lift real restrictions and restore them
  after the run (explicit opt-in).

For uncaptured values, `sync --missing-secrets placeholder` still creates the
variable name so it can be filled in manually later.

#### Orb-based alternative (committed config)

For large numbers of contexts or full pipeline control, commit `manifest.json`
to a repo in your source org and use the `awesomecicd/circleci-org-migration`
orb. Each job must reference **exactly one context** (mixing contexts lets
same-named variables overwrite each other):

```yaml
# .circleci/config.yml in your SOURCE org
version: "2.1"
orbs:
  migrate: awesomecicd/circleci-org-migration@0.8.0
workflows:
  capture-secrets:
    jobs:
      - migrate/extract_context:
          name: extract-deploy-prod
          context_name: deploy-prod
          context: [deploy-prod]
      - migrate/merge:
          name: merge-secrets
          requires: [extract-deploy-prod]
```

For many contexts, use a matrix with an explicit `alias` so `merge` can depend
on the whole matrix:

```yaml
version: "2.1"
orbs:
  migrate: awesomecicd/circleci-org-migration@0.8.0
workflows:
  capture-secrets:
    jobs:
      - migrate/extract_context:
          name: extract-<< matrix.context_name >>
          context: [<< matrix.context_name >>]
          matrix:
            alias: extract_contexts
            parameters:
              context_name: [deploy-prod, shared, build, staging]
      - migrate/merge:
          name: merge-secrets
          requires: [extract_contexts]
```

Download `secrets.json` from the `merge` job's **Artifacts** tab. If the bundle
is age-encrypted, decrypt it locally with `secrets decrypt`.

#### Protecting `secrets.json`

`secrets.json` contains plaintext values — treat it like a password file.

- Encryption is on by default; keep it on for production secrets.
- `--artifact-retention-days 1` minimises the in-CircleCI exposure window.
- The local file is written with `0600` permissions. Do **not** commit it.
- Use a **private** project for the capture pipeline.
- **Rotate every captured value** after the destination is confirmed healthy.

---

## 6. Step 3 — Sync into the destination

`sync` is a **dry run by default**.

### Destination resolution

The destination org **defaults to the source org from the manifest**. To target
a *different* org you MUST pass `--mapping` with `org.to` set — otherwise sync
runs against your own source org (a prominent warning is printed). See
[mapping.md](mapping.md) for the full schema.

```bash
# Dry run — nothing is written; review the plan
circleci-migrate sync --manifest manifest.json --secrets secrets.json \
  --mapping mapping.json

# Apply when satisfied
circleci-migrate sync --manifest manifest.json --secrets secrets.json \
  --mapping mapping.json --apply
```

The dry-run plan shows each action as `created (would create)`, `set (would
set)`, or `manual`.

### Secrets

Env-var values come from `--secrets` (default `secrets.json`, skipped if
absent). With `--apply` but **no** bundle, resources are created with **empty**
env-var values you must fill in manually. `--missing-secrets`:

- `skip` (default) — omit variables with no captured value.
- `placeholder` — create the variable with a placeholder value (useful for
  restricted contexts) so the name exists for manual fill-in.

### Enabling builds

When `--apply` creates projects, they are created **paused** (no webhook, no
builds). You are then prompted to enable builds; `--yes` / `-y` auto-confirms
(only meaningful with `--apply`; no effect in a dry run). Without a TTY, builds
are not enabled unless `--yes` is passed.

### Context restrictions

Context restrictions are transferred automatically when sync has all the
information it needs:

| Restriction type | Behaviour |
|---|---|
| `expression` | Copied verbatim to the destination context. |
| `group` | Resolved by name to the destination group UUID (group must exist in the destination org; falls back to `manual` if not found). |
| `project` | **Auto-remapped**: source project UUID → source slug (from manifest) → destination slug (via mapping) → destination project UUID. Falls back to `manual` if any step fails (see note below). |

**Project restriction ordering note:** `sync` processes contexts _before_
projects. On the first run, destination projects may not exist yet, causing
project restrictions to fall back to `manual` with a re-run instruction.
Simply re-run `sync --apply` after projects have been created to resolve them.
The re-run is idempotent — existing restrictions are skipped.

### Project API tokens

`--create-project-tokens` (with `--apply`) recreates each captured project API
token on the destination. **Caution:** each recreated token mints a **new**
one-time secret printed once to stderr — every consumer of the old token must be
repointed. Default is off (the report emits manual steps only).

### The `--skip-*` family

| Flag | Skips |
|---|---|
| `--skip-org-settings` | Org-level settings (feature flags, OIDC, URL-orb allow list, config policies, etc.) |
| `--skip-contexts` | Contexts |
| `--skip-projects` | Projects |
| `--skip-extras` | Checkout keys, additional SSH keys, webhooks, schedules |
| `--skip-runner` | Self-hosted runner resource classes |
| `--skip-ciam` | CIAM roles and groups (standalone `circleci`-type orgs only) |
| `--skip-orb` | Orbs (captured orb versions) |

### Runner resource classes

Supply `--dest-runner-namespace` to recreate runner classes in the destination
(the namespace must already exist; the syncer never guesses it). When omitted,
runner classes are flagged for manual recreation.

```bash
circleci-migrate sync --manifest manifest.json --secrets secrets.json \
  --mapping mapping.json --dest-runner-namespace acme-new-runners --apply --yes
```

### Orb namespace sync

Supply `--dest-orb-namespace` to republish captured orb versions into the
destination namespace. The syncer publishes each stable version idempotently
(existing versions are skipped) and enables the required orb-publishing feature
flags on the destination org for the duration of the publish, then restores them.

After publishing, a **CONFIG REWRITE REQUIRED** notice lists every source→destination
orb mapping that must be updated in `.circleci/config.yml` before cutover.

```bash
circleci-migrate sync --manifest manifest.json \
  --dest-orb-namespace acme-new --apply
```

When `--dest-orb-namespace` is omitted and the manifest contains orbs, each orb
is flagged as `manual` with instructions to re-run with `--dest-orb-namespace`
or submit a support ticket to CircleCI for a direct namespace transfer.

### Machine-readable output

`--json` prints a JSON summary to stdout instead of the per-section reports;
progress goes to stderr.

---

## 7. Scenarios by org type

All scenarios share the export → capture → sync flow above. The differences are
the slugs, whether you need a mapping file, and a few flags.

### 7a. OAuth → OAuth

Both orgs use the GitHub OAuth integration. If the org name changes, supply a
[mapping](mapping.md) with `org.to`:

```bash
circleci-migrate export --source-org gh/acme -o manifest.json
circleci-migrate secrets capture --manifest manifest.json \
  --encrypt --generate-key --enable-trigger -o secrets.json
circleci-migrate sync --manifest manifest.json --secrets secrets.json \
  --mapping mapping.json --apply
```

Or the all-in-one:

```bash
circleci-migrate migrate \
  --source-org gh/acme --dest-org gh/acme-new \
  --secrets secrets.json --apply --yes
```

### 7b. GitHub App → GitHub App

App orgs use UUID slugs (`circleci/<uuid>`). Find them at
`https://app.circleci.com/settings/organization/circleci/<uuid>/overview`.

```bash
SRC_UUID="11111111-1111-1111-1111-111111111111"
DST_UUID="22222222-2222-2222-2222-222222222222"

circleci-migrate export --source-org "circleci/$SRC_UUID" -o manifest.json
# Capture via the orb (large orgs) or `secrets capture`
circleci-migrate sync --manifest manifest.json --secrets secrets.json \
  --mapping mapping.json --apply --yes
```

App projects are created with triggers **disabled**; `--yes` enables them after
creation. Omit `--yes` (answer N at the prompt) to keep them paused until you're
ready, then re-run `--apply --yes`. Repos must already be connected to the
destination GitHub App.

### 7c. CircleCI standalone → standalone

Standalone `circleci`-type orgs additionally carry **CIAM roles and groups**.
These sync by default; pass `--skip-ciam` to leave them alone. (CIAM
provisioning is reported as a manual follow-up where the API cannot fully
automate it — check the report.)

```bash
circleci-migrate sync --manifest manifest.json --secrets secrets.json \
  --mapping mapping.json --apply        # CIAM included
circleci-migrate sync --manifest manifest.json --secrets secrets.json \
  --mapping mapping.json --skip-ciam --apply   # CIAM left untouched
```

### 7d. Mixed org (OAuth + App) — two legs

Run the whole flow twice, once per org record:

```bash
# Leg 1 — OAuth record
circleci-migrate export --source-org gh/acme -o manifest-oauth.json --report report-oauth.md
circleci-migrate secrets capture --manifest manifest-oauth.json -o secrets-oauth.json
circleci-migrate sync --manifest manifest-oauth.json --secrets secrets-oauth.json \
  --mapping mapping-oauth.json --apply --yes

# Leg 2 — App record (capture via orb; download secrets-app.json)
circleci-migrate export --source-org "circleci/$SRC_UUID" -o manifest-app.json --report report-app.md
circleci-migrate sync --manifest manifest-app.json --secrets secrets-app.json \
  --mapping mapping-app.json --apply --yes
```

Contexts and org settings may overlap between the two records — review both
reports, and consider `--skip-org-settings` on the second leg to avoid
double-applying org flags.

### 7e. Cross-type: OAuth → GitHub App

A follow-on migration, typically after an OAuth org has moved. **Data-loss
caveats** (recorded as `manual` in the report):

- `build_fork_prs` — the GitHub App never builds fork PRs; cannot be replicated.
- The OSS flag and `pr_only_branch_overrides` have no App equivalent.
- Multiple App pipeline definitions can't be collapsed from one OAuth config —
  the tool creates one pipeline definition per project using the default config
  path (`.circleci/config.yml`).

Because the slug type changes, you must supply a [mapping](mapping.md) that maps
project slugs:

```json
{
  "org": { "from": "gh/acme", "to": "circleci/22222222-2222-2222-2222-222222222222" },
  "projects": {
    "gh/acme/web": "circleci/22222222-2222-2222-2222-222222222222/web",
    "gh/acme/api": "circleci/22222222-2222-2222-2222-222222222222/api"
  }
}
```

```bash
circleci-migrate sync --manifest manifest.json --secrets secrets.json \
  --mapping mapping.json --apply --yes
```

### 7f. Repo-move / EMU (repos moved to a new GitHub org)

When repos move between GitHub orgs, their numeric `external_id` changes. Supply
`--github-token` + `--dest-github-org` so the tool resolves the new IDs (found →
onboard, missing → flagged manual + skipped):

```bash
export GITHUB_TOKEN="<github-pat-with-repo-read>"

circleci-migrate migrate \
  --source-org "circleci/$SRC_UUID" --dest-org "circleci/$DST_UUID" \
  --secrets secrets.json --dest-github-org acme-new --apply --yes
# --github-token falls back to $GITHUB_TOKEN
```

For a **partial** move (only some repos changed org), use the `github_org` key
or per-project `projects` entries in the [mapping file](mapping.md) instead of
`--dest-github-org`.

### 7g. Runner resource classes

```bash
circleci-migrate export --source-org gh/acme --runner-namespace acme-runners -o manifest.json
circleci-migrate sync --manifest manifest.json --secrets secrets.json \
  --mapping mapping.json --dest-runner-namespace acme-new-runners --apply --yes
```

The destination namespace must already exist. Resource-class tokens are treated
as secrets — supply a bundle or use `--missing-secrets placeholder`.

### 7h. Orb namespace transfer

Capture all published orbs and republish them into a destination namespace:

```bash
# Export with orb capture
circleci-migrate export --source-org gh/acme --orb-namespace acme -o manifest.json

# Sync — republishes each version idempotently
circleci-migrate sync --manifest manifest.json \
  --dest-orb-namespace acme-new --apply
```

After publishing you will see a **CONFIG REWRITE REQUIRED** section listing every
`acme/<orb>` → `acme-new/<orb>` mapping that operators must update in project
`.circleci/config.yml` files before cutover.

Use `--skip-orb` to exclude orb sync when running `migrate` or `sync` without
orb capture.

---

## 8. Terraform generation (optional)

`terraform generate` converts an exported manifest into a set of Terraform HCL
files targeting the official **CircleCI-Public/circleci** provider (v0.3.x).
Use this when you want the migrated org to land in Terraform state rather than
being created imperatively by `sync`.

> **Terraform vs CLI split:** Terraform manages the declarative resource
> *shells* (contexts, projects, env-var names + values). The CLI remains the
> orchestrator for everything the Terraform provider cannot do: secrets capture,
> CIAM roles and groups, org-level settings, legacy schedules, checkout/deploy
> keys, SSH keys, and project API tokens. The generated **GAPS.md** lists every
> remaining step with the exact `circleci-migrate` command to complete it.

### OAuth vs standalone destination orgs

The CircleCI Terraform provider's advanced project-settings attributes
(`auto_cancel_builds`, `build_fork_prs`, `disable_ssh`,
`forks_receive_secret_env_vars`, `set_github_status`, `setup_workflows`,
`write_settings_requires_admin`) are **only supported for standalone (GitHub
App / GitLab / `circleci/`-type) orgs**. For GitHub OAuth (`gh/`-type) orgs
the provider's `GetSettings`/`UpdateSettings` APIs are not available and
including those attributes would cause `terraform apply` to fail.

Use `--dest-org-type` to tell the generator which kind of destination org you
are targeting:

| Value | Aliases | When to use |
|---|---|---|
| `oauth` | `gh`, `github` | Destination is a GitHub OAuth org (`gh/<org>` slug) |
| `standalone` | `app`, `github_app` | Destination is a GitHub App / GitLab / standalone org (`circleci/<uuid>` slug) |

When `--dest-org-type` is **omitted**, the type is **inferred from the source
org slug** in the manifest (`gh/` → oauth; `circleci/` → standalone) and a
note is printed explaining which type was assumed and how to override it.

For **OAuth destinations**, `projects.tf` is generated **without** advanced
settings. The generated `GAPS.md` lists project advanced settings as a gap
with a `circleci-migrate sync` command to apply them. For **standalone
destinations**, all advanced settings are included (current behavior).

### Basic usage

```bash
# Org type inferred from manifest source slug (notice printed to stderr)
circleci-migrate terraform generate \
  --manifest manifest.json \
  --dest-org-id <destination-org-uuid> \
  --out ./terraform/

# Explicit: OAuth destination (no advanced project settings in output)
circleci-migrate terraform generate \
  --manifest manifest.json \
  --dest-org-id <destination-org-uuid> \
  --dest-org-type oauth \
  --out ./terraform/

# Explicit: standalone destination (advanced project settings included)
circleci-migrate terraform generate \
  --manifest manifest.json \
  --dest-org-id <destination-org-uuid> \
  --dest-org-type standalone \
  --out ./terraform/
```

This writes the following files into `--out`:

| File | Contents |
|---|---|
| `versions.tf` | Provider version constraint (`~> 0.3`) |
| `providers.tf` | Provider block — `host` and `organization` from `--host`/`--dest-org-id` |
| `contexts.tf` | `circleci_context` + `circleci_context_environment_variable` resources |
| `projects.tf` | `circleci_project` + `circleci_project_environment_variable` resources (advanced settings only for standalone) |
| `migration.auto.tfvars.json` | Non-secret values (context names, project settings where applicable) |
| `GAPS.md` | Everything Terraform does not manage + CLI commands to finish the job |

### Providing secret values

Env-var values are never included in the manifest (the CircleCI API masks them).
Supply them one of two ways:

```bash
# From a captured bundle (values written to secrets.auto.tfvars.json — PLAINTEXT)
circleci-migrate terraform generate \
  --manifest manifest.json \
  --secrets bundle.json \
  --dest-org-id <uuid> --out ./terraform/

# Placeholder mode (REPLACE_ME values + SECRETS_WORKBOOK.md fill-in guide)
circleci-migrate terraform generate \
  --manifest manifest.json \
  --placeholders \
  --dest-org-id <uuid> --out ./terraform/
```

`--secrets bundle.json` writes plaintext values to `secrets.auto.tfvars.json`.
A warning is printed to stderr — treat that file like a password file and delete
it after `terraform apply`.

`--placeholders` emits `REPLACE_ME` values and a `SECRETS_WORKBOOK.md` table for
manual fill-in before applying.

### Org slug / project ID remapping

```bash
circleci-migrate terraform generate \
  --manifest manifest.json \
  --mapping mapping.json \
  --dest-org-id <uuid> --out ./terraform/
```

The mapping file is the same one used by `sync` (see [mapping.md](mapping.md)).

### Apply the generated configuration

```bash
cd ./terraform/

# Set the API token for the destination org
export TF_VAR_circleci_api_token="<dest-org-api-token>"

terraform init
terraform plan
terraform apply
```

Then run the CLI to fill the gaps listed in `GAPS.md`:

```bash
circleci-migrate sync --manifest manifest.json \
  --secrets bundle.json \
  --dest-token $CIRCLECI_DEST_TOKEN \
  --apply
```

### CircleCI Server (`--host`)

The generated `providers.tf` sets `host` from `--host`. For CircleCI Server:

```bash
circleci-migrate terraform generate \
  --manifest manifest.json \
  --dest-org-id <uuid> \
  --host https://circleci.example.com \
  --out ./terraform/
```

### M2 resources (context restrictions, webhooks, runners, pipelines/triggers)

`terraform generate` (M2) now covers these additional resources:

| Resource | Both org types | Standalone only | OAuth only |
|---|---|---|---|
| `circleci_context_restriction` type=project | ✓ | | |
| `circleci_context_restriction` type=expression | ✓ | | |
| `circleci_context_restriction` type=group | | | ✓ |
| `circleci_webhook` | ✓ | | |
| `circleci_runner_resource_class` + `circleci_runner_token` | ✓ | | |
| `circleci_pipeline` + `circleci_trigger` | | ✓ | skipped |

For **OAuth destinations**: `circleci_pipeline` and `circleci_trigger` are
**omitted** (provider schema rejects `github_oauth`). Pipeline definitions in
the manifest land in GAPS.md and must be recreated via `sync`.

### Self-hosted runner namespace

Pass `--dest-runner-namespace` to map runner classes to a different destination namespace:

```bash
circleci-migrate terraform generate \
  --manifest manifest.json \
  --dest-org-id <uuid> \
  --dest-runner-namespace acme-new \
  --out ./terraform/
```

When omitted, the source namespace from the manifest is used as-is.

### Adopting existing resources (--import-existing)

If resources were previously created by `circleci-migrate sync`, you can import
them into Terraform state with the `--import-existing` flag. Pass the output of
`sync --json` via `--existing`:

```bash
# Step 1: run sync --json to capture existing resource IDs
circleci-migrate sync --manifest manifest.json --dest-token $CIRCLECI_DEST_TOKEN \
  --json > sync-result.json

# Step 2: generate with import blocks
circleci-migrate terraform generate \
  --manifest manifest.json \
  --dest-org-id <uuid> \
  --import-existing --existing sync-result.json \
  --out ./terraform/

# Step 3: terraform plan (imports existing, creates missing)
cd ./terraform/ && terraform init && terraform plan
```

This emits Terraform 1.5+ `import {}` blocks in `imports.tf` for contexts,
projects, webhooks, and runner resource classes that already exist.

### CLI gap-fill after terraform apply

After `terraform apply`, use `--skip-terraform-managed` on `sync` to avoid
overwriting resources Terraform now owns. This syncs only the CLI-only sections
(org-settings, CIAM, extras):

```bash
circleci-migrate sync --manifest manifest.json \
  --dest-token $CIRCLECI_DEST_TOKEN \
  --apply --skip-terraform-managed
```

Alternatively, use `--only` to sync only specific sections:

```bash
# Sync only org-settings, CIAM, and extras after terraform apply
circleci-migrate sync --manifest manifest.json \
  --dest-token $CIRCLECI_DEST_TOKEN \
  --apply --only org-settings,ciam,extras
```

### How should the destination be managed?

When running `migrate` interactively, you can choose how the destination is
managed:

1. **CLI applies everything (default)** — run `sync --apply` to recreate all
   resources imperatively. This is the standard path.
2. **Generate Terraform + CLI gap-fill** — run `terraform generate` first, then
   `terraform apply`, then `sync --skip-terraform-managed` (or `--only`) to fill
   in what the provider cannot manage. Use this when the destination org should
   land in Terraform state for ongoing IaC management.

The Terraform path requires an extra setup step (Terraform installed, remote
state configured) but leaves all declarative resources in state for future
`plan`/`apply` cycles. The GAPS.md lists every remaining CLI step with the exact
command to complete it.

### What Terraform does NOT manage (M1/M2 scope)

The following always require the CLI `sync` command (they are listed in GAPS.md):
legacy v2 schedules, checkout/deploy keys, additional SSH keys, project API
tokens, CIAM roles and groups, org-level settings (feature flags, OIDC, OTel,
contacts, retention, budgets, orb allowlist, SSO, release tracker), and private
orb inlining.

For **OAuth (`gh/`) destination orgs**, project advanced settings
(`auto_cancel_builds`, `build_fork_prs`, `disable_ssh`, etc.) and pipeline/trigger
resources are also in GAPS.md. Apply them via:

```bash
circleci-migrate sync --manifest manifest.json --dest-token $CIRCLECI_DEST_TOKEN --apply
```

---

## 9. Step 4 — Validate, enable, rotate

### `validate` — automated gap analysis

The `validate` command compares the source org against the destination org
in-memory (no files written) and reports which items are present, missing, or
require manual attention:

```bash
# Human-readable report (exit 0 = no gaps, exit 1 = gaps found)
circleci-migrate validate \
  --source-org gh/acme --dest-org gh/acme-new

# With a mapping file (same format as 'sync --mapping')
circleci-migrate validate \
  --source-org gh/acme --dest-org gh/acme-new \
  --mapping mapping.json

# Include runner and orb comparisons
circleci-migrate validate \
  --source-org gh/acme --dest-org gh/acme-new \
  --dest-runner-namespace acme-new \
  --dest-orb-namespace acme-new

# Machine-readable JSON (for CI pipelines or tooling)
circleci-migrate validate \
  --source-org gh/acme --dest-org gh/acme-new \
  --json
```

**What `validate` checks:**

| Section | Status types |
|---|---|
| Contexts (env-var names, restrictions) | ✓ matched / ✗ missing |
| Projects (env-var names, settings, SSH keys, checkout keys) | ✓ matched / ✗ missing |
| Org Settings (feature flags, OIDC, URL-orb allow list, config policies, retention, release tracker, contacts, OTel) | ✓ matched / ⚠ manual |
| Runner Resource Classes (`--dest-runner-namespace` required) | ✓ matched / ✗ missing / skipped |
| Orbs (`--dest-orb-namespace` required) | ✓ matched / ✗ missing / skipped |
| SSO | ⚠ always manual (DNS + IdP reconfiguration required) |
| CIAM roles | ⚠ always manual (UUIDs differ between orgs) |

Exit codes: `0` = no missing items; `1` = gaps found. Items marked `⚠ manual`
do **not** cause a non-zero exit but appear in a prominent NEEDS ATTENTION block.

**Flags (`validate`-specific):**

| Flag | Description |
|---|---|
| `--source-org` | Source org slug (e.g. `gh/acme`). **Required.** |
| `--dest-org` | Destination org slug (e.g. `gh/acme-new`). **Required.** |
| `--mapping` | Path to a `mapping.json` file (same format as `sync --mapping`). Optional. |
| `--dest-runner-namespace` | Destination runner namespace. When omitted the Runner section is skipped. |
| `--dest-orb-namespace` | Destination orb namespace. When omitted the Orb section is skipped. |
| `--json` | Emit machine-readable JSON instead of the human report. |
| `--no-input` | Disable prompts; fail immediately on missing required flags. |

After sync (and any `terraform apply`) completes, follow the
[cutover runbook](cutover-runbook.md):

1. Run `validate` to confirm no gaps remain — fix any `✗ missing` items before
   cutover.
2. Verify project settings, webhooks, and schedules.
3. Enable builds when ready (the sync prompt or `--yes`).
4. Recreate items that don't transfer — see
   [Manual steps required](cutover-runbook.md#3-manual-steps-required).
5. Update external pins (Backstage, Slack, status badges, branch-protection).
6. **Rotate every captured secret** and delete `secrets.json` (and
   `secrets.auto.tfvars.json` if Terraform was used).

---

## 10. Verifying release artifacts

Every release binary and archive is signed with
[Sigstore](https://www.sigstore.dev/) keyless cosign (no long-lived key stored
in CI). The signing identity is a CircleCI OIDC certificate minted at release
time and recorded on the public Sigstore transparency log (Rekor). No secrets
are needed to verify.

### What is published

Each GitHub Release asset has an accompanying `.bundle` file (the Sigstore
bundle: signature + certificate + Rekor log entry in one JSON file). For
example:

```
circleci-migrate_1.2.3_linux_amd64.tar.gz
circleci-migrate_1.2.3_linux_amd64.tar.gz.bundle
```

### How to verify

Download the binary/archive and its `.bundle` sidecar, then run:

```bash
# Install cosign v3 if not already present
go install github.com/sigstore/cosign/v3/cmd/cosign@v3.1.1

cosign verify-blob \
  circleci-migrate_1.2.3_linux_amd64.tar.gz \
  --bundle circleci-migrate_1.2.3_linux_amd64.tar.gz.bundle \
  --certificate-oidc-issuer https://oidc.circleci.com \
  --certificate-identity-regexp 'https://circleci\.com/api/v2/projects/.*'
```

A successful verification prints:

```
Verified OK
```

### Pinning to a specific project (optional)

The certificate's Subject Alternative Name (SAN) encodes the exact CircleCI
pipeline-definition URL that produced the signature:

```
https://circleci.com/api/v2/projects/<CIRCLE_PROJECT_ID>/pipeline-definitions/<def-id>
```

You can pin to the exact project by replacing `--certificate-identity-regexp`
with `--certificate-identity` and the full URL from the bundle:

```bash
# Inspect the identity in the bundle
cat circleci-migrate_1.2.3_linux_amd64.tar.gz.bundle \
  | jq -r '.verificationMaterial.certificate.rawBytes' \
  | base64 -d \
  | openssl x509 -inform DER -noout -text \
  | grep URI

# Then pin:
cosign verify-blob \
  circleci-migrate_1.2.3_linux_amd64.tar.gz \
  --bundle circleci-migrate_1.2.3_linux_amd64.tar.gz.bundle \
  --certificate-oidc-issuer https://oidc.circleci.com \
  --certificate-identity "https://circleci.com/api/v2/projects/<CIRCLE_PROJECT_ID>/pipeline-definitions/<def-id>"
```

---

## See also

- [Playbooks](playbooks/README.md) — step-by-step operator runbooks with
  per-phase checklists and validation gates, one per account/org-type:
  [OAuth → OAuth](playbooks/oauth-to-oauth.md),
  [standalone → standalone](playbooks/standalone-to-standalone.md),
  [OAuth → App (cross-type, lossy)](playbooks/cross-type-oauth-to-app.md).
- [Cutover runbook](cutover-runbook.md) — operator checklist + the full
  what-does-NOT-transfer list.
- [mapping.json reference](mapping.md) — when you need a mapping file and what
  each key does.
- [Troubleshooting](troubleshooting.md) — common errors and fixes.
- [CLI reference](cli/README.md) — complete per-command flag tables.
- [Architecture](architecture.md) — how the tool reads and writes data.
