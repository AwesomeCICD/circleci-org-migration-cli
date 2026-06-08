# circleci-migrate

A command-line tool for migrating data between CircleCI organisations.

> **Status: under active development.**
> The tool skeleton is in place and the API surface is stable, but the
> migration commands are not yet implemented.  Watch this space.

---

## What it does

`circleci-migrate` moves the following resources from a *source* CircleCI
organisation to a *destination* organisation:

- **Contexts** and their environment variables
- **Project-level environment variables**
- **Project settings and metadata**
- **VCS integration configuration**

The three-step model is:

| Step | Command | Description |
|------|---------|-------------|
| 1 | `export` | Read the source org and write a manifest file to disk |
| 2 | *(review)* | Inspect or edit the manifest before applying it |
| 3 | `sync` | Apply the manifest to the destination org |

If you do not need to inspect the manifest, the `migrate` command runs both
steps in one shot.

---

## Installation

*Release artifacts are not yet published.  To build from source:*

```bash
git clone https://github.com/CircleCI-Public/circleci-org-migration-cli.git
cd circleci-org-migration-cli
make build          # produces bin/circleci-migrate
```

---

## Usage

```text
# Export your source org to a manifest
circleci-migrate export \
  --source-token "$SRC_TOKEN" \
  --org           myorg       \
  > manifest.json

# Review manifest.json, then sync to the destination org
circleci-migrate sync \
  --dest-token "$DST_TOKEN" \
  --org         neworg      \
  manifest.json

# -- or -- migrate in a single step
circleci-migrate migrate \
  --source-token "$SRC_TOKEN" --source-org myorg \
  --dest-token   "$DST_TOKEN" --dest-org   neworg
```

Global flags available to every command:

| Flag | Env variable | Default | Description |
|------|-------------|---------|-------------|
| `--host` | `CIRCLECI_HOST` | `https://circleci.com` | CircleCI host URL |
| `--token` | `CIRCLECI_CLI_TOKEN` | | Fallback API token for both orgs |
| `--source-token` | `CIRCLECI_SOURCE_TOKEN` | | Read token for the source org |
| `--dest-token` | `CIRCLECI_DEST_TOKEN` | | Write token for the destination org |
| `--debug` | | `false` | Enable debug logging |

Run `circleci-migrate --help` or `circleci-migrate <command> --help` for
full flag documentation.

---

## Contributing

This project follows the conventions of
[circleci-cli](https://github.com/CircleCI-Public/circleci-cli) and is
designed to merge into it in the future.  Please open an issue before
submitting a large pull request.
