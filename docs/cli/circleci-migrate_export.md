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

Examples:
  circleci-migrate export --source-org gh/acme --source-token $SRC_TOKEN
  circleci-migrate export --source-org gh/acme -o acme.json --report acme-audit.md
  circleci-migrate export --source-org gh/acme --project gh/acme/web --project gh/acme/api
  circleci-migrate export --source-org gh/acme --runner-namespace acme

```
circleci-migrate export --source-org <org-slug> [flags]
```

### Options

```
  -h, --help                      help for export
  -o, --output string             Path to write the JSON manifest (always written; use -o to change the path) (default "manifest.json")
      --project stringArray       Explicit project slug to export (repeat to export multiple: --project gh/acme/web --project gh/acme/api)
      --report string             Path to write the human-readable audit report (default "migration-report.md")
      --runner-namespace string   Source runner namespace to capture self-hosted runner resource classes from (e.g. 'acme'). The namespace must be supplied explicitly — there is no clean org→namespace lookup.
      --skip-contexts             Skip exporting contexts
      --skip-extras               Skip checkout keys, webhooks, and schedules
      --skip-projects             Skip exporting projects
      --source-org string         Source organization slug: gh/<org> or circleci/<org-id> (required)
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

