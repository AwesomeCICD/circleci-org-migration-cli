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

`circleci-migrate` moves configuration data from one CircleCI organization to
another — contexts, project settings, environment variables, org-level settings,
runner resource classes, and more — with a safe, auditable approach that never
requires you to expose secrets in plain text until they are needed.

<p align="center">
  <img src="docs/demo.gif" alt="circleci-migrate demo" width="900">
</p>

## What & why

`export` reads everything it can from your source org into a non-secret
`manifest.json` (structure and names only — safe to review and diff). `sync`
replays that manifest into the destination — a **dry run by default**, `--apply`
to write. Because the CircleCI API never returns secret values, a separate
`secrets capture` step runs a short-lived pipeline in the source org to collect
them, encrypted by default. A few things cannot be migrated by any API and are
listed in each export's `migration-report.md` and in the
[cutover runbook](docs/cutover-runbook.md#4-does-not-transfer--data-loss).

## Install

```bash
# Homebrew (recommended)
brew install AwesomeCICD/tap/circleci-migrate

# go install (Go 1.26+)
go install github.com/AwesomeCICD/circleci-org-migration-cli@latest
```

Or grab a prebuilt binary (Linux/macOS, amd64/arm64) from
[GitHub Releases](https://github.com/AwesomeCICD/circleci-org-migration-cli/releases):

```bash
# Resolve the latest release tag (e.g. v0.8.0) from the GitHub API.
VERSION=$(curl -sfL https://api.github.com/repos/AwesomeCICD/circleci-org-migration-cli/releases/latest \
  | grep '"tag_name"' | head -1 | cut -d'"' -f4)
curl -sfL "https://github.com/AwesomeCICD/circleci-org-migration-cli/releases/download/${VERSION}/circleci-migrate_${VERSION#v}_linux_amd64.tar.gz" \
  | tar -xz
sudo install -m 0755 circleci-migrate /usr/local/bin/
```

> **Future namespace:** the tap and orb move to `CircleCI-Labs` / `cci-labs`
> when the tool is republished under CircleCI Labs. `AwesomeCICD` is the current
> production location.

## Quickstart

```bash
export CIRCLECI_SOURCE_TOKEN="<source-org-admin-token>"
export CIRCLECI_DEST_TOKEN="<destination-org-admin-token>"

# Interactive guided walkthrough (recommended for first-time use)
circleci-migrate migrate

# …or scripted: export → capture secrets → dry-run → apply
circleci-migrate export --source-org gh/acme
circleci-migrate secrets capture --manifest manifest.json --encrypt --generate-key --enable-trigger
circleci-migrate sync --manifest manifest.json --secrets secrets.json --mapping mapping.json          # dry run
circleci-migrate sync --manifest manifest.json --secrets secrets.json --mapping mapping.json --apply  # write
```

> **`secrets.json` holds plaintext values** — treat it like a password file
> (written `0600`, never commit it), and **rotate every captured value** after
> the destination is healthy. To target a *different* org you must pass
> `--mapping` with `org.to` (otherwise sync hits your source org).

## Documentation

Read in this order:

1. **[Migration guide](docs/guide.md)** — the single walkthrough: org types,
   prerequisites & token permissions, and export → capture → sync with
   per-org-type examples.
2. **[mapping.json reference](docs/mapping.md)** — when you need a mapping file
   and what the `org` / `projects` / `github_org` keys do.
3. **[Cutover runbook](docs/cutover-runbook.md)** — the operator checklist for a
   production cutover, including the full *what does NOT transfer* list.
4. **[Troubleshooting](docs/troubleshooting.md)** — common errors and fixes.
5. **[CLI reference](docs/cli/README.md)** — complete, auto-generated
   per-command flag tables. (Also available as [man pages](man/).)

Further reading: [architecture & data flow](docs/architecture.md),
[API usage](docs/api-usage.md), [contributing](CONTRIBUTING.md).

## Using the official `circleci` CLI

`circleci-migrate` is also a plugin to the official
[circleci CLI](https://circleci.com/docs/local-cli/). Invoke it as
`circleci run migrate <args>` and the `circleci` CLI execs it on your `PATH`,
injecting your configured token and host — no extra flags required:

```bash
circleci run migrate export --source-org gh/acme
circleci run migrate sync   --manifest manifest.json --apply
```

Bare `circleci migrate` (without `run`) is not supported.
