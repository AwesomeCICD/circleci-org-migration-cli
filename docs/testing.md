# Testing guide

This document explains how the end-to-end test fixtures are structured and how
to run a manual dry-run or apply migration against a dummy org.

---

## e2e-fixtures.yaml

`e2e-fixtures.yaml` at the repo root describes the CircleCI orgs, GitHub orgs,
and repositories used for live and manual testing. It has two sections:

### `fixtures` — dedicated dummy orgs

These are throwaway orgs provisioned specifically for repeatable e2e testing.
Each scenario defines a `source` (populated with synthetic test data) and a
`dest` (empty, intended to receive the migration).

| Scenario | Description |
|---|---|
| `oauth_to_oauth` | GitHub OAuth org to GitHub OAuth org (same-type). Seed data: two contexts and two project env vars. |
| `app_to_app` | GitHub App org to GitHub App org (same-type). Seed data: one context and at least one repo with a `.circleci/config.yml` to exercise pipeline-definition capture. |

Slugs and org IDs are marked `TBD` until the dummy orgs are provisioned. Repos
listed in `repos:` should exist in both the source and destination GitHub orgs
and should use synthetic (non-secret) config. The seed data under `seed:` is
created in the source org before a test run.

### `known_dev_resources` — real-ish accounts for ad-hoc testing

These entries describe real or semi-real CircleCI orgs used during development:

| Name | Role | Notes |
|---|---|---|
| `Dummy-Test` | `dest-writable-dummy` | Safe for write tests (sync --apply, project create/delete). No connected repo yet, so pipeline-definition creation is not live-testable here. |
| `james-crowley` | `oauth-source-readonly` | Real OAuth org (~105 projects). Export/read tests only. |
| `CircleCI-Labs-Standalone` | `app-source-readonly` | Real GitHub App org with pipeline definitions and triggers (~50 projects). Export/read tests only. |
| `AwesomeCICD` | `ci-host` | Hosts this repo's CI and the `awesomecicd` orb namespace. Do not run destructive tests here. |

---

## No secrets in this file

`e2e-fixtures.yaml` contains **no API tokens or secret values**. All
credentials come from environment variables:

| Env var | Purpose |
|---|---|
| `CIRCLECI_SOURCE_TOKEN` | API token for the source org (fallback: `CIRCLECI_CLI_TOKEN`) |
| `CIRCLECI_DEST_TOKEN` | API token for the destination org (fallback: `CIRCLECI_CLI_TOKEN`) |
| `GITHUB_TOKEN` | GitHub PAT for resolving repo external IDs (App pipeline definitions) |

Set these in your shell before running any migration command. Never commit
token values to this file or any other file in the repository.

---

## Running a manual dry-run

A dry run reads the source org and prints what *would* happen in the
destination — no writes are made. This is safe to run against any org.

```bash
export CIRCLECI_SOURCE_TOKEN=your-source-token
export CIRCLECI_DEST_TOKEN=your-dest-token

# Using separate export + sync steps
circleci-migrate export --source-org gh/james-crowley
circleci-migrate sync --manifest manifest.json --dest-token "$CIRCLECI_DEST_TOKEN"

# Or using the all-in-one migrate command
circleci-migrate migrate \
  --source-org gh/james-crowley \
  --dest-org circleci/3ac9b843-7095-443e-bfb3-bd7037999ef5
```

Review the output for `manual` items and the `migration-report.md` for a
full audit of what was captured.

---

## Running a manual apply migration against the dummy org

The `Dummy-Test` org (`circleci/3ac9b843-7095-443e-bfb3-bd7037999ef5`) is
designated as a writable sandbox. It is safe to create and delete contexts,
env vars, and project shells here.

```bash
export CIRCLECI_SOURCE_TOKEN=your-source-token
export CIRCLECI_DEST_TOKEN=your-dest-token

# Export from source
circleci-migrate export \
  --source-org gh/james-crowley \
  -o manifest.json \
  --report migration-report.md

# Apply to the dummy dest org (--yes skips the enable-builds prompt)
circleci-migrate sync \
  --manifest manifest.json \
  --dest-token "$CIRCLECI_DEST_TOKEN" \
  --mapping mapping.json \
  --apply \
  --yes
```

Or in one shot using `migrate`:

```bash
circleci-migrate migrate \
  --source-org gh/james-crowley \
  --dest-org circleci/3ac9b843-7095-443e-bfb3-bd7037999ef5 \
  --apply --yes \
  -o manifest.json --report migration-report.md
```

**Tip:** Pass `--skip-org-settings` if you do not want to overwrite the dummy
org's feature flags and OIDC settings during a test run.

---

## Using the e2e fixtures (when provisioned)

Once the fixture orgs are provisioned and their slugs are filled in, a
typical e2e test flow for `oauth_to_oauth` would be:

1. Seed the source org: create the contexts and project env vars listed under
   `fixtures.oauth_to_oauth.seed`.
2. Run the migration in dry-run mode and verify the plan output.
3. Run with `--apply --yes` and verify the destination org matches the source.
4. Re-run with `--apply` to verify idempotency (no duplicate resources).
5. Clean up: delete the created resources from the destination org.

For `app_to_app`, additionally verify that pipeline definitions and triggers
are created with `disabled=true`, then that `--yes` enables them correctly.
