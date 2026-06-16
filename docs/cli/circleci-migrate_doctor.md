## circleci-migrate doctor

Run migration preflight checks without migrating.

### Synopsis

doctor runs the same preflight checks as 'migrate' but exits immediately
after printing the summary — it does not export or sync any data.

Use it to validate your tokens, org slugs, and configuration before running a
migration. It is safe to run as many times as needed; it is entirely read-only.

If --dest-org is omitted, only source-side checks are run (token present, source
org reachable, api-trigger flag state, and project discovery). If --source-org is
omitted, only destination-side checks are run (token present, destination org
reachable, cross-type warning, GitHub token for repo resolution).

Exit codes:
  0 — all checks passed (OK or warnings only)
  1 — one or more hard failures (missing required token or unreachable org)

Examples:
  # Check both source and destination:
  circleci-migrate doctor --source-org gh/acme --dest-org gh/acme-new

  # Source-side only (validate before export):
  circleci-migrate doctor --source-org gh/acme

  # Destination-side only (validate before sync):
  circleci-migrate doctor --dest-org gh/acme-new

```
circleci-migrate doctor [--source-org <slug>] [--dest-org <slug>] [flags]
```

### Options

```
      --dest-github-org string   Destination GitHub organization owner (e.g. 'acme-new'). Use when repos have moved to a new GitHub org. Triggers the GitHub-token check.
      --dest-org string          CircleCI organization slug for the destination org, e.g. gh/my-new-org (shown in CircleCI → Organization Settings → Overview). This is the CircleCI org identifier, not a GitHub repository URL. When provided, destination-side checks are run (token, reachability, cross-type warning, GitHub token). May be combined with --source-org to run both sides.
      --github-token string      GitHub personal access token used to resolve repository IDs when creating pipeline definitions in a GitHub App destination org. Falls back to $GITHUB_TOKEN.
  -h, --help                     help for doctor
      --source-org string        CircleCI organization slug for the source org, e.g. gh/my-org (shown in CircleCI → Organization Settings → Overview). This is the CircleCI org identifier, not a GitHub repository URL. When provided, source-side checks are run (token, reachability, api-trigger flag, project discovery). May be combined with --dest-org to run both sides.
```

### Options inherited from parent commands

```
      --debug                 Enable debug logging
      --dest-token string     API token for the destination org (env: CIRCLECI_DEST_TOKEN)
      --host string           CircleCI host URL (env: CIRCLECI_CLI_HOST, CIRCLECI_HOST, or CIRCLE_URL) (default "https://circleci.com")
      --source-token string   API token for the source org (env: CIRCLECI_SOURCE_TOKEN)
      --token string          Personal API token — fallback for both orgs (env: CIRCLECI_CLI_TOKEN or CIRCLE_TOKEN)
```

### SEE ALSO

* [circleci-migrate](circleci-migrate.md)	 - Migrate data between CircleCI organisations.

