# Flags

Every flag of every command, one line each. `--help` works on every command. The generated [CLI reference](../cli/README.md) has the full help text and examples. `-o` is short for `--output` and `-y` is short for `--yes`.

## Global flags

These work on every command.

- `--debug` — print every HTTP request and response (off).
- `--host` — CircleCI host URL for CircleCI Server; also `CIRCLECI_CLI_HOST`, `CIRCLECI_HOST`, `CIRCLE_URL` (`https://circleci.com`).
- `--source-token` — API token for the source org; also `CIRCLECI_SOURCE_TOKEN`.
- `--dest-token` — API token for the destination org; also `CIRCLECI_DEST_TOKEN`.
- `--token` — fallback token for both orgs; also `CIRCLECI_CLI_TOKEN` or `CIRCLE_TOKEN`.

## doctor

Read-only preflight checks. Give `--source-org`, `--dest-org`, or both.

- `--source-org` — run source-side checks: token, reachability, API-trigger flag, project discovery.
- `--dest-org` — run destination-side checks: token, reachability, cross-type warning, GitHub token.
- `--github-token` — GitHub token (repo read) to check; falls back to `$GITHUB_TOKEN`.
- `--dest-github-org` — destination GitHub owner when repos moved; triggers the GitHub-token check.

## export

Reads the source org into `manifest.json` and `migration-report.md`. Read-only.

- `--source-org` — source org slug, such as `gh/acme` (required).
- `--dest-org` — destination slug; changes nothing in the export but adds the cross-type checklist to the report for `gh/` to `circleci/`.
- `--output` — manifest path (`manifest.json`).
- `--report` — audit report path (`migration-report.md`).
- `--project` — export only this project slug; repeat for several (all projects).
- `--skip-contexts` — do not export contexts (off).
- `--skip-projects` — do not export projects (off).
- `--skip-extras` — do not export checkout keys, webhooks or schedules (off).
- `--orb-namespace` — capture every published orb and stable version in this namespace (off).
- `--runner-namespace` — capture runner resource classes from this namespace (off).
- `--follow-all` — follow every GitHub repo not yet set up as a project; `gh/` orgs only; needs a GitHub token (off).
- `--github-token` — GitHub token for `--follow-all`; falls back to `$GITHUB_TOKEN`.
- `--include-usage` — also download a usage report CSV as a local record; it does not transfer (off).
- `--usage-start` — usage window start, RFC 3339 (30 days ago).
- `--usage-end` — usage window end, RFC 3339 (now); the window is at most 31 days.
- `--usage-timeout` — how long to wait for the usage export job (`10m`).
- `--skip-preflight` — skip the startup checks (off).
- `--json` — print a JSON summary to stdout (off).

## mapping generate

Writes `mapping.json` from a manifest.

- `--manifest` — manifest path (required).
- `--dest-org` — destination org slug (required).
- `--output` — mapping file path (`mapping.json` next to the manifest).

## secrets transfer

Moves secret values from source to destination inside a source-org pipeline. Dry run unless `--apply`.

- `--manifest` — manifest path (required).
- `--dest-org-id` — destination org UUID.
- `--dest-org` — destination org slug; resolved to the UUID for you.
- `--dest-token-context` — source-org context that holds the destination token.
- `--dest-token-env-var` — variable name inside that context (`CIRCLECI_DEST_TOKEN`).
- `--dest-host` — destination host URL for Server installs (`https://circleci.com`).
- `--mapping` — `mapping.json` for project and context name overrides.
- `--context` — transfer only this context; repeatable (all contexts with variables).
- `--include-project-vars` — also move project env vars (off).
- `--include-ssh-keys` — also move additional project SSH keys (off).
- `--skip-contexts` — run only the per-project pipelines; needs one of the two flags above (off).
- `--remove-restrictions` — lift project and expression restrictions for the run, then restore them (off).
- `--enable-trigger` — turn on `api-trigger-with-config` for the run and restore it afterwards (off).
- `--host-project` — source project that runs the context pipeline (picked for you).
- `--branch` — branch to check out (each project's default branch, else `main`).
- `--poll-timeout` — how long to wait for the pipeline (`30m`).
- `--apply` — run the transfer (dry run).

## secrets capture

Captures secret values into a local bundle. Interactive on a terminal when `--manifest` is omitted.

- `--manifest` — manifest path (prompted when omitted).
- `--output` — bundle path (`secrets.json`).
- `--context` — capture only this context; repeatable (all in the manifest).
- `--project` — capture only this project; repeatable (all in the manifest).
- `--host-project` — project that runs the context extraction (prompted).
- `--branch` — branch to check out (`main`).
- `--encrypt` — encrypt the artifact with age (on).
- `--no-encrypt` — produce a plaintext artifact; not recommended (off).
- `--generate-key` — create an age keypair for this run (on when encrypting with no key given).
- `--ssh-public-key` — public key or age recipients file to encrypt to.
- `--ssh-private-key` — private key or age identity used to decrypt locally (`~/.ssh/id_ed25519` if present).
- `--artifact-retention-days` — set artifact retention before the run; 1 is the minimum and is not restored (leave unchanged).
- `--storage` — where the bundle goes: `artifact`, `s3` or `both` (`artifact`).
- `--s3-bucket` — S3 bucket; required for `s3` and `both`.
- `--s3-prefix` — S3 key prefix (none).
- `--ssh-keys` — also capture additional SSH keys; `--ssh-keys=false` skips them (on).
- `--skip-restricted-contexts` — skip contexts with restrictions and warn (on).
- `--remove-restrictions` — lift context restrictions for the run, then restore them (off).
- `--enable-trigger` — turn on `allow_api_trigger_with_config` for the run and restore it (auto-detected).
- `--yes` — allow an unattended capture of everything (off).
- `--no-input` — disable prompts; error if a value is missing (on without a terminal).
- `--poll-timeout` — how long to wait for each pipeline (`10m`; `0` waits forever).

## secrets decrypt

Decrypts an age-encrypted bundle.

- `--identity-file` — SSH private key or age identity to decrypt with (required).
- `--output` — decrypted bundle path (`secrets.json`).

## secrets extract

Used inside the capture pipeline; you rarely run it by hand.

- `--manifest` — manifest path (required).
- `--context` — context to extract; not with `--project`.
- `--project` — project to extract; not with `--context`.
- `--output` — bundle path to write or append (`secrets.json`).
- `--encrypt` — write `<output>.age`; needs a recipient (off).
- `--recipient` — age or SSH public key string.
- `--recipient-file` — SSH public key or age recipients file.
- `--strict` — fail if an expected variable is missing (off).

## secrets merge

Merges partial bundles from several extract jobs.

- `--output` — merged bundle path (`secrets.json`).
- `--encrypt` — write `<output>.age`; needs a recipient (off).
- `--recipient` — age or SSH public key string.
- `--recipient-file` — SSH public key or age recipients file.

## sync

Replays a manifest into the destination org. Dry run unless `--apply`.

- `--manifest` — manifest path (required).
- `--mapping` — `mapping.json`; `org.to` is required to leave the source org.
- `--secrets` — secrets bundle path (`secrets.json`, skipped if absent).
- `--missing-secrets` — variables with no value: `skip` or `placeholder` (`skip`).
- `--apply` — write changes (dry run).
- `--yes` — with `--apply`, enable builds without asking (off).
- `--enable-existing-triggers` — also enable triggers that already exist disabled (off).
- `--skip-org-settings` — skip org-level settings (off).
- `--skip-contexts` — skip contexts (off).
- `--skip-projects` — skip projects (off).
- `--skip-pipelines` — create App projects with no definitions or triggers (off).
- `--skip-extras` — skip checkout keys, SSH keys, webhooks and schedules (off).
- `--skip-runner` — skip runner resource classes (off).
- `--skip-ciam` — skip roles and groups; standalone orgs only (off).
- `--skip-orb` — skip orbs (off).
- `--skip-preflight` — skip the startup checks (off).
- `--skip-terraform-managed` — skip sections Terraform manages: contexts, projects, runner classes (off).
- `--only` — sync only these sections: `org-settings`, `contexts`, `projects`, `runner`, `ciam`, `extras`, `orb`.
- `--dest-orb-namespace` — republish orbs into this namespace.
- `--dest-runner-namespace` — recreate runner classes in this namespace.
- `--dest-github-org` — destination GitHub owner when repos moved; needs a GitHub token.
- `--github-token` — GitHub token (repo read) for repo IDs; falls back to `$GITHUB_TOKEN`.
- `--webhook-secrets-out` — with `--apply`, write new webhook secrets and URLs here, mode 0600.
- `--create-project-tokens` — with `--apply`, recreate project API tokens as new values (off).
- `--include-danger-flags` — also write `drop_all_build_requests` and `require_context_group_restriction` (off).
- `--json` — print a JSON summary to stdout (off).

## cutover

Switches builds from source to destination. Dry run unless `--apply`.

- `--manifest` — manifest path (required unless `--undo`).
- `--mapping` — mapping file; `org.to` is required with `--enable-dest`.
- `--pause-source` — pause the source: set `drop-all-build-requests` and disable enabled App triggers (off).
- `--pause-org` — also pause the whole source org; use with `--pause-source` (off).
- `--enable-dest` — enable destination triggers whose source trigger was enabled (off).
- `--undo` — restore everything recorded in the state file (off).
- `--state` — state file path, mode 0600 (`cutover-state.json`).
- `--apply` — make the changes (dry run).

## validate

Compares source and destination. Exit 1 means something is missing.

- `--source-org` — source org slug (required).
- `--dest-org` — destination org slug (required).
- `--mapping` — the same `mapping.json` you gave `sync`.
- `--dest-token-context` — leave this transfer-token context out of the comparison.
- `--ignore-context` — leave this context out; repeatable.
- `--dest-runner-namespace` — compare runner classes in this namespace (section skipped).
- `--dest-orb-namespace` — compare orbs in this namespace (section skipped).
- `--no-input` — fail on a missing flag instead of prompting (on without a terminal).
- `--json` — print a JSON result to stdout (off).

## migrate

Export, sync and validate in one command. Run with no flags on a terminal for the guided walkthrough.

It takes these flags with the same meaning as above: from `export`, `--orb-namespace`, `--runner-namespace`, `--follow-all`, `--github-token`, `--skip-contexts`, `--skip-projects`, `--skip-extras`, `--skip-preflight` and `--json`. From `sync`, `--mapping`, `--secrets`, `--missing-secrets`, `--apply`, `--yes`, `--enable-existing-triggers`, `--skip-org-settings`, `--skip-pipelines`, `--skip-runner`, `--skip-ciam`, `--skip-orb`, `--dest-orb-namespace`, `--dest-runner-namespace`, `--dest-github-org`, `--webhook-secrets-out`, `--create-project-tokens` and `--include-danger-flags`. These are specific to `migrate`:

- `--source-org` — source org slug (prompted).
- `--dest-org` — destination org slug (prompted).
- `--output` — save the manifest here (kept in memory).
- `--report` — save the audit report here (not written).
- `--transfer-secrets` — after sync, run the in-pipeline secrets transfer; not with `--secrets` (off).
- `--dest-token-context` — context holding the destination token; required with `--transfer-secrets`.
- `--include-project-vars` — with `--transfer-secrets`, also move project env vars (off).
- `--include-ssh-keys` — with `--transfer-secrets`, also move additional SSH keys (off).
- `--remove-restrictions` — with `--transfer-secrets`, lift context restrictions for the run (off).
- `--host-project` — with `--transfer-secrets`, project that runs the context pipeline (first project).
- `--preflight-only` — run the checks and exit (off).
- `--skip-validate` — skip the parity check that runs after a successful `--apply` (off).
- `--no-input` — disable prompts (on without a terminal).

## terraform generate

Writes Terraform files from a manifest. See [Terraform output](terraform.md).

- `--manifest` — manifest path (`manifest.json`).
- `--dest-org-id` — destination org UUID (required).
- `--dest-org-type` — `oauth` or `standalone` (inferred from the source slug).
- `--out` — output directory (`./terraform`).
- `--mapping` — `mapping.json` for slug and project ID remapping.
- `--secrets` — secrets bundle; writes plaintext to `secrets.auto.tfvars.json`.
- `--placeholders` — write `REPLACE_ME` values and a fill-in workbook (off).
- `--dest-runner-namespace` — destination runner namespace (the source namespace).
- `--import-existing` — emit `import` blocks for resources that already exist (off).
- `--existing` — `sync --json` output file that supplies the IDs; required with `--import-existing`.

## orb inline

Rewrites a config so its orbs are inlined.

- `--config` — `.circleci/config.yml` to rewrite (required).
- `--namespace` — inline only orbs in this namespace (all).
- `--output` — output path (stdout).
