<!--
  BADGE NOTES:
  - CircleCI build-status badge removed: "Status Badges" is not enabled for this project
    (dl.circleci.com/status-badge/... returns 404). To restore, enable
    Project Settings → Status Badges in the CircleCI UI for AwesomeCICD/circleci-org-migration-cli,
    then re-add:
      [![CircleCI build](https://dl.circleci.com/status-badge/img/gh/AwesomeCICD/circleci-org-migration-cli/tree/main.svg?style=shield)](https://dl.circleci.com/status-badge/redirect/gh/AwesomeCICD/circleci-org-migration-cli/tree/main)
  - Orb-registry badge removed: badges.circleci.com/orbs/awesomecicd/circleci-org-migration.svg
    returns 404 because the orb is PRIVATE. Re-add once the orb is public after the
    CircleCI-Labs namespace move:
      [![orb: cci-labs/circleci-org-migration](https://badges.circleci.com/orbs/cci-labs/circleci-org-migration.svg)](https://circleci.com/developer/orbs/orb/cci-labs/circleci-org-migration)
-->
[![GitHub release](https://img.shields.io/github/v/release/AwesomeCICD/circleci-org-migration-cli?logo=github)](https://github.com/AwesomeCICD/circleci-org-migration-cli/releases/latest)
[![Homebrew](https://img.shields.io/badge/homebrew-AwesomeCICD%2Ftap%2Fcircleci--migrate-orange?logo=homebrew)](https://github.com/AwesomeCICD/homebrew-tap)
[![Go 1.26](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![Go Report Card](https://goreportcard.com/badge/github.com/AwesomeCICD/circleci-org-migration-cli)](https://goreportcard.com/report/github.com/AwesomeCICD/circleci-org-migration-cli)
[![Conventional Commits](https://img.shields.io/badge/Conventional%20Commits-1.0.0-yellow.svg)](https://conventionalcommits.org)

# circleci-migrate

`circleci-migrate` moves configuration data from one CircleCI organization to another — contexts, project settings, environment variables, org-level settings, runner resource classes, and more — with a safe, auditable approach that never requires you to expose secrets in plain text until they are needed.

<p align="center">
  <img src="docs/demo.gif" alt="circleci-migrate demo" width="900">
</p>

---

## What it does

`circleci-migrate` exports everything it can read from your source org into a local `manifest.json`, then replays it into the destination. Because the CircleCI API never returns secret values, a separate **secrets capture** step runs a short-lived pipeline inside your source org to collect them — encrypted by default, never stored in plain text.

Items that cannot be migrated automatically (SSO/SAML, audit-log streaming, webhook HMAC secrets) are listed in a `migration-report.md` for manual follow-up.

| Category | What transfers |
|---|---|
| **Contexts** | Names, env-var names + values (via secrets capture), expression + group restrictions |
| **Projects** | Advanced settings, env-var names + values, webhooks, schedules, pipeline defs (App), triggers (App — created disabled) |
| **Org settings** | Feature flags, OIDC claims, URL-orb allow list, config policies, OTel exporters, contacts, storage retention, spend budgets, release-tracker, env hierarchy |
| **Runner resource classes** | Self-hosted runner classes (supply `--runner-namespace`) |

---

## Install

<!--
  NOTE: temp home is github.com/AwesomeCICD (orb: awesomecicd/circleci-org-migration);
  moves to CircleCI-Labs (repo + homebrew-tap, orb namespace cci-labs) on the Labs move.
-->

### Homebrew (recommended)

```bash
brew install AwesomeCICD/tap/circleci-migrate
```

> **Future namespace:** the tap and orb will move to `CircleCI-Labs` / `cci-labs` when the tool
> is republished under CircleCI Labs. The `AwesomeCICD` names are the current production location.

### `go install`

```bash
go install github.com/AwesomeCICD/circleci-org-migration-cli@latest
```

Requires Go 1.26+. The binary is placed in `$GOPATH/bin` (or `$HOME/go/bin`).

### Prebuilt binary

Prebuilt binaries for Linux and macOS are on
[GitHub Releases](https://github.com/AwesomeCICD/circleci-org-migration-cli/releases).
Archive naming: `circleci-migrate_<version>_<os>_<arch>.tar.gz`
(supported: `linux_amd64`, `linux_arm64`, `darwin_amd64`, `darwin_arm64`).

```bash
VERSION=v0.4.1
curl -sfL "https://github.com/AwesomeCICD/circleci-org-migration-cli/releases/download/${VERSION}/circleci-migrate_${VERSION#v}_linux_amd64.tar.gz" \
  | tar -xz
sudo install -m 0755 circleci-migrate /usr/local/bin/
```

---

## Quickstart

### Option A — Interactive guided walkthrough (recommended for first-time use)

Set tokens, then run:

```bash
export CIRCLECI_SOURCE_TOKEN="<source-org-personal-api-token>"
export CIRCLECI_DEST_TOKEN="<destination-org-personal-api-token>"

circleci-migrate migrate
```

`migrate` with no flags on a terminal launches a step-by-step walkthrough: it prompts for source org, destination org, which resources to migrate, and whether to apply. No changes are written until you explicitly confirm.

### Option B — Scripted export → capture → sync

Use this when you need to inspect or edit the manifest between steps, or are scripting a pipeline.

**Step 1 — Export the source org** (read-only, safe to re-run):

```bash
circleci-migrate export --source-org gh/acme
# Produces: manifest.json  migration-report.md
```

Review `migration-report.md` for warnings and manual follow-up items.

**Step 2 — Capture secret values** (runs a short-lived pipeline in the source org):

```bash
circleci-migrate secrets capture
```

Running with no flags launches the guided walkthrough. It will:
- Read the manifest to enumerate contexts and projects to capture.
- Offer to **encrypt** the artifact with [age](https://age-encryption.org/) (default: yes — plaintext secrets never persist in CircleCI storage).
- Offer to set artifact retention to 1 day before triggering the run (default: yes).
- Show a confirmation summary before proceeding.

The result is `secrets.json` on your local machine.

**Step 3 — Dry run, then apply** (destination org inferred from the manifest):

```bash
# Review the plan — nothing is written
circleci-migrate sync --manifest manifest.json --secrets secrets.json

# Apply when satisfied
circleci-migrate sync --manifest manifest.json --secrets secrets.json --apply
```

**Step 4 — Validate and rotate**

After sync completes: verify resources in the destination, enable builds when ready (the sync prompt or `--yes`), then rotate every captured secret value and delete `secrets.json`.

---

## Secrets

**The secret bundle (`secrets.json`) contains plaintext environment-variable values. Treat it like a password file.**

- **Encryption is on by default.** `secrets capture` encrypts the CircleCI artifact with [age](https://age-encryption.org/) so plaintext secrets never persist in CircleCI artifact storage. Only the decrypted local copy contains values.
- **1-day retention by default.** The guided walkthrough sets artifact retention to 1 day before triggering the extraction run.
- **Dry run before apply.** Always review the sync plan before passing `--apply`.
- **0600 permissions.** `secrets.json` is written with `0600` permissions. Do not commit it to version control.
- **Rotate after cutover.** Every captured secret should be rotated once the destination is confirmed healthy.

---

## Commands

| Command | What it does |
|---|---|
| `migrate` | All-in-one: export + sync in one step. Interactive walkthrough with no flags. |
| `export` | Read the source org and produce `manifest.json` + `migration-report.md`. Read-only. |
| `sync` | Replay a manifest into the destination org. Dry-run by default; add `--apply` to write. |
| `secrets capture` | CLI-orchestrated secret extraction (inline pipeline, no committed config). |
| `secrets extract` | Run inside a CircleCI job to capture values from the job environment (used by the orb). |
| `secrets merge` | Combine per-context/project bundles into one file. |
| `secrets decrypt` | Decrypt an age-encrypted bundle downloaded from an artifact. |
| `orb inline` | Inline private orbs into a config file. |
| `version` | Print version, commit SHA, and OS/arch. |

See the [generated CLI reference](docs/cli/README.md) for complete per-command flag documentation, and the [man pages](man/) for offline use.

### Key flags at a glance

**Global flags** (available on every command):

| Flag | Env var | Description |
|---|---|---|
| `--source-token` | `CIRCLECI_SOURCE_TOKEN` | API token for the source org |
| `--dest-token` | `CIRCLECI_DEST_TOKEN` | API token for the destination org |
| `--token` | `CIRCLECI_CLI_TOKEN` | Fallback token for both orgs |
| `--host` | `CIRCLECI_CLI_HOST` | CircleCI host URL (for Server installs) |
| `--debug` | | Verbose HTTP logging |

**`export` flags** (commonly used):

| Flag | Default | Description |
|---|---|---|
| `--source-org` | *(required)* | Source org slug: `gh/<org>` or `circleci/<org-id>` |
| `--output`, `-o` | `manifest.json` | Path to write the manifest |
| `--report` | `migration-report.md` | Path to write the audit report |
| `--project` | *(all followed)* | Explicit project slug(s) to export (repeat for multiple) |
| `--runner-namespace` | | Include self-hosted runner resource classes |

**`sync` flags** (commonly used):

| Flag | Default | Description |
|---|---|---|
| `--manifest` | *(required)* | Path to the export manifest |
| `--secrets` | `secrets.json` | Path to the secret bundle (skipped if absent) |
| `--apply` | `false` | Write changes (default: dry run) |
| `--yes`, `-y` | `false` | Auto-confirm enabling builds |
| `--missing-secrets` | `skip` | `skip` or `placeholder` for uncaptured variables |

**`migrate` flags** (commonly used):

| Flag | Default | Description |
|---|---|---|
| `--source-org` | *(prompted)* | Source org slug |
| `--dest-org` | *(prompted)* | Destination org slug |
| `--secrets` | `secrets.json` | Path to the secret bundle |
| `--apply` | `false` | Write changes |
| `--yes`, `-y` | `false` | Auto-confirm enabling builds |
| `--no-input` | `false` | Disable prompts; error on missing required values |

---

## Using `circleci run migrate` (plugin invocation)

`circleci-migrate` is also available as a plugin to the official
[circleci CLI](https://circleci.com/docs/local-cli/). When you run:

```bash
circleci run migrate export --source-org gh/acme
circleci run migrate sync   --manifest manifest.json --apply
circleci run migrate migrate
```

the `circleci` CLI looks up `circleci-migrate` on your `PATH` and execs it,
forwarding all arguments and injecting `CIRCLE_TOKEN` (your configured API token)
and `CIRCLE_URL` (your configured host). **No extra token or host flags are required**
when you are already authenticated with the `circleci` CLI.

Token and host precedence:

```
Token : --token/--source-token/--dest-token > CIRCLECI_CLI_TOKEN/CIRCLECI_SOURCE_TOKEN/CIRCLECI_DEST_TOKEN > CIRCLE_TOKEN
Host  : --host > CIRCLECI_CLI_HOST > CIRCLECI_HOST > CIRCLE_URL > https://circleci.com
```

Note: bare `circleci migrate` (without `run`) is **not** supported — it would require
the command to be merged into the upstream circleci-cli source tree.

---

## Org type notes

The org slug format (`gh/<org>` vs `circleci/<org-id>`) controls which APIs are used:

- **GitHub OAuth** (`gh/<org>`): full v1.1 and v2 API coverage. Projects are followed to install webhooks.
- **GitHub App** (`circleci/<org-id>`): v2 API only. Projects use pipeline definitions and triggers (created disabled).

**Same-type migrations** (OAuth→OAuth, App→App) are fully automated. For cross-type migrations or repos that have moved to a new GitHub org, see [docs/examples.md](docs/examples.md).

When `sync` creates projects with `--apply`, they are created **paused**. After sync you are prompted to enable builds, or pass `--yes` to skip the prompt.

---

## Further reading

- [Worked migration examples](docs/examples.md) — OAuth→OAuth, App→App, mixed, cross-type, repo-move, runners
- [Cutover runbook](docs/cutover-runbook.md) — operator checklist for production cutovers
- [Architecture and data flow](docs/architecture.md)
- [CLI reference](docs/cli/README.md) — auto-generated per-command flag reference
- [Man pages](man/) — auto-generated man pages for all commands
- [Contributing / developing](CONTRIBUTING.md)
