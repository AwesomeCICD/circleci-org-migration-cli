<!--
  Badge notes: the CircleCI build-status badge needs Project Settings > Status Badges enabled;
  the orb-registry badge needs the orb to be public (badges.circleci.com/orbs/<namespace>/circleci-org-migration.svg).
  Re-add each when its prerequisite is met.
-->
[![GitHub release](https://img.shields.io/github/v/release/AwesomeCICD/circleci-org-migration-cli?logo=github)](https://github.com/AwesomeCICD/circleci-org-migration-cli/releases/latest)
[![Homebrew](https://img.shields.io/badge/homebrew-AwesomeCICD%2Ftap%2Fcircleci--migrate-orange?logo=homebrew)](https://github.com/AwesomeCICD/homebrew-tap)
[![Go 1.26](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![Go Report Card](https://goreportcard.com/badge/github.com/AwesomeCICD/circleci-org-migration-cli)](https://goreportcard.com/report/github.com/AwesomeCICD/circleci-org-migration-cli)
[![Conventional Commits](https://img.shields.io/badge/Conventional%20Commits-1.0.0-yellow.svg)](https://conventionalcommits.org)

# circleci-migrate

Move a CircleCI organization to another one without losing contexts, secrets, projects or settings. `circleci-migrate` exports your org, copies it to the destination, switches builds over, and checks that the two match. Every step is a dry run until you pass `--apply`.

<p align="center">
  <img src="docs/demo.gif" alt="circleci-migrate demo" width="900">
</p>

## Install

macOS and Linux:

```bash
brew install AwesomeCICD/tap/circleci-migrate
```

Windows (PowerShell):

```powershell
$v = (irm https://api.github.com/repos/AwesomeCICD/circleci-org-migration-cli/releases/latest).tag_name
$a = if ($env:PROCESSOR_ARCHITECTURE -eq 'ARM64') { 'arm64' } else { 'amd64' }
iwr "https://github.com/AwesomeCICD/circleci-org-migration-cli/releases/download/$v/circleci-migrate_$($v.TrimStart('v'))_windows_$a.zip" -OutFile cm.zip
Expand-Archive cm.zip -DestinationPath "$env:LOCALAPPDATA\circleci-migrate" -Force
```

Other options, signature checks and proxy notes are in [install](docs/get-started/install.md).

## Pick your migration

Start with the docs site: **https://awesomecicd.github.io/circleci-org-migration-cli/**. It has a plan builder that fills in the commands for your own orgs. Everything is also here on GitHub.

| You are moving | Runbook |
|---|---|
| GitHub OAuth org (`gh/…`) to a GitHub App org (`circleci/…`) | [OAuth to GitHub App](docs/migrations/oauth-to-app.md) |
| OAuth org to another OAuth org, including a new GitHub org | [OAuth to OAuth](docs/migrations/oauth-to-oauth.md) |
| GitHub App or standalone org to another | [App to App](docs/migrations/app-to-app.md) |

Not sure which one? Read [choose your migration](docs/get-started/choose.md). To see what carries over, read [what moves](docs/get-started/what-moves.md).

## Quickstart

```bash
export CIRCLECI_SOURCE_TOKEN="<source-org-admin-token>"
export CIRCLECI_DEST_TOKEN="<destination-org-admin-token>"

circleci-migrate doctor --source-org gh/acme --dest-org gh/acme-new   # preflight, read-only
circleci-migrate export --source-org gh/acme                          # writes manifest.json, migration-report.md
```

Then follow your runbook. For a guided, prompt-driven run of a simple OAuth to OAuth move, run `circleci-migrate migrate`.

## Documentation

- [Concepts](docs/concepts/how-it-works.md): how it works, [secrets](docs/concepts/secrets.md), [mapping](docs/concepts/mapping.md), [cutover](docs/concepts/cutover.md).
- Reference: [troubleshooting](docs/reference/troubleshooting.md), [flags](docs/reference/flags.md), [Terraform](docs/reference/terraform.md), [CLI reference](docs/cli/README.md).
- Contribute: [architecture](docs/contribute/architecture.md), [testing](docs/contribute/testing.md), [CONTRIBUTING](CONTRIBUTING.md).

Prefer the official [circleci CLI](https://circleci.com/docs/local-cli/)? Run `circleci run migrate <args>`.
