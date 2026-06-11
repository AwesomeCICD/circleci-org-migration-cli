## circleci-migrate export

Export source-org data to a local manifest file.

### Synopsis

export reads configuration from the source CircleCI organization and
writes a non-secret JSON manifest plus a human-readable audit report.

The manifest captures contexts (and their variable names, restrictions, and
security groups), projects (settings, variable names, and metadata), and
org-level settings. It is read-only: it never writes to CircleCI, and it never
contains secret values — those are masked by the API and must be captured with
the in-pipeline secrets step.

The org slug is "gh/<org>" for GitHub OAuth organizations or
"circleci/<org-id>" for GitHub App / GitLab organizations.

Self-hosted runner resource classes live under a namespace on runner.circleci.com.
Pass --runner-namespace to capture them. The namespace must be supplied explicitly
because there is no clean org→namespace lookup in the CircleCI API.

USAGE DATA SNAPSHOT (opt-in):

Pass --include-usage to also request a historical usage report from the CircleCI
Usage API. The report is downloaded as gzip-compressed CSV files to a "usage/"
sub-directory next to the manifest. The window defaults to the last 30 days; use
--usage-start / --usage-end (RFC 3339) to override. The maximum window is 31 days
(enforced by the API).

IMPORTANT: usage data is a local baseline/record only. It does NOT transfer to
the destination organisation during sync or migrate. If the usage export fails or
times out, the main export succeeds and a warning is printed.

Examples:
  circleci-migrate export --source-org gh/acme --source-token $SRC_TOKEN
  circleci-migrate export --source-org gh/acme -o acme.json --report acme-audit.md
  circleci-migrate export --source-org gh/acme --project gh/acme/web --project gh/acme/api
  circleci-migrate export --source-org gh/acme --runner-namespace acme
  circleci-migrate export --source-org gh/acme --include-usage
  circleci-migrate export --source-org gh/acme --include-usage --usage-start 2026-01-01T00:00:00Z --usage-end 2026-01-31T23:59:59Z

```
circleci-migrate export --source-org <org-slug> [flags]
```

### Options

```
  -h, --help                      help for export
      --include-usage             (Opt-in) Request a historical usage report from the CircleCI Usage API and download the CSV files to a 'usage/' sub-directory next to the manifest. This data is a local baseline/record only — it does NOT transfer to the destination org.
      --json                      Print a machine-readable JSON summary to stdout instead of the human-readable summary (manifest and report files are still written)
  -o, --output string             Path to write the JSON manifest (always written; use -o to change the path) (default "manifest.json")
      --project stringArray       Explicit project slug to export (repeat to export multiple: --project gh/acme/web --project gh/acme/api)
      --report string             Path to write the human-readable audit report (default "migration-report.md")
      --runner-namespace string   Source runner namespace to capture self-hosted runner resource classes from (e.g. 'acme'). The namespace must be supplied explicitly — there is no clean org→namespace lookup.
      --skip-contexts             Skip exporting contexts
      --skip-extras               Skip checkout keys, webhooks, and schedules
      --skip-projects             Skip exporting projects
      --source-org string         Source organization slug: gh/<org> or circleci/<org-id> (required)
      --usage-end string          End of the usage report window in RFC 3339 format (default: now). Only used when --include-usage is set.
      --usage-start string        Start of the usage report window in RFC 3339 format (default: 30 days ago). The window may not exceed 31 days. Only used when --include-usage is set.
      --usage-timeout duration    Maximum time to wait for the usage export job to complete before giving up. Only used when --include-usage is set. (default 10m0s)
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

