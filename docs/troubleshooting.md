# Troubleshooting

Common problems when running `circleci-migrate`, and how to fix them. For the
full walkthrough see the [migration guide](guide.md); for the mapping file see
[mapping.md](mapping.md).

---

## Dry run shows everything as `manual`

You ran without a secrets bundle. That is expected for a first dry run — the
plan still shows contexts and projects as `created (would create)`. Run
[`secrets capture`](guide.md#5-step-2--capture-secret-values) (or use the orb)
to produce `secrets.json`, then pass it with `--secrets`.

## `sync` targeted the wrong org (it hit my source org)

`sync` defaults the destination to the **source org from the manifest**. To
write to a different org you must pass `--mapping` with `org.to` set; otherwise
sync runs against your own source org and prints a warning. See
[mapping.md](mapping.md).

## `--apply` fails on project creation (App org)

Repos must be connected to the destination CircleCI GitHub App **before**
`sync --apply`. Check the GitHub App installation in the destination org's
settings, then re-run.

## Pipeline definitions show the wrong `external_id`

If repos have moved GitHub orgs, their numeric IDs changed. Add `--github-token`
and `--dest-github-org` (or a `github_org` mapping entry) so the tool resolves
the new IDs. See the
[repo-move scenario](guide.md#7f-repo-move--emu-repos-moved-to-a-new-github-org).

## Context restriction shows `manual`

Project-type context restrictions cannot be migrated automatically — source-org
project UUIDs do not transfer. Recreate them in the destination org settings UI
after sync. (Expression and group restrictions transfer where the API allows.)

## `secrets capture` fails with `api-trigger-with-config disabled`

Add `--enable-trigger` to the capture command. This enables the flag temporarily
for the extraction run and restores it afterwards.

## `secrets capture` errors out without triggering anything

In a non-interactive run, if neither `--context` nor `--project` is given, an
unattended **capture-all is fail-closed** to prevent accidental org-wide
extraction. Either:

1. Pass `--context` and/or `--project` to scope exactly what is captured.
2. Pass `--yes` (or `--no-input`) to acknowledge an unattended capture-all.
3. Run on an interactive TTY (no `--manifest`) for the guided walkthrough.

## Captured values are missing for restricted contexts

Restricted contexts are skipped by default (`--skip-restricted-contexts`). To
capture them, either `--remove-restrictions` (temporarily lift and restore), or
accept the gap and use `sync --missing-secrets placeholder` so the variable name
exists for manual fill-in later.

## CircleCI Server: requests go to circleci.com

Pass `--host https://circleci.example.com` (or set `CIRCLECI_CLI_HOST` /
`CIRCLECI_HOST` / `CIRCLE_URL`). The default host is `https://circleci.com`.

## Authentication / 401 errors

Set `CIRCLECI_SOURCE_TOKEN` and `CIRCLECI_DEST_TOKEN` (or the fallback
`CIRCLECI_CLI_TOKEN` / `CIRCLE_TOKEN`). The token's user must be an
**organization admin** of the org it acts on. See
[prerequisites](guide.md#3-prerequisites--token-permissions).

## CIAM roles/groups did not fully apply

CIAM provisioning is reported as a manual follow-up where the API cannot fully
automate it — check `migration-report.md`. Use `--skip-ciam` to leave CIAM
untouched entirely (standalone `circleci`-type orgs only).

## Debug mode

Add `--debug` to any command for verbose HTTP request/response logging:

```bash
circleci-migrate sync --manifest manifest.json --debug
```

---

## See also

- [Migration guide](guide.md)
- [Cutover runbook](cutover-runbook.md) — operator checklist and the full
  what-does-NOT-transfer list.
- [CLI reference](cli/README.md)
