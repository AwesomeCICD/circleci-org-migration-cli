## circleci-migrate mapping generate

Auto-generate a project slug mapping from a manifest and a destination org.

### Synopsis

generate lists the projects already onboarded in the destination org and
matches them against the projects captured in the export manifest by repo
name (the last '/'-separated segment of the slug, e.g. "web" from
"gh/acme/web").

Output:
  • A mapping.json file ready for use with 'sync --mapping' and
    'secrets transfer --mapping'.
  • A human-readable report printed to stdout with three sections:
      matched       — source slug → dest slug (written to mapping.json)
      unmatched     — source projects with no matching dest project
      dest-only     — dest projects with no source counterpart (info only)

Exit code is 0 even when there are unmatched entries — the report is the
deliverable; unmatched entries mean the user must onboard those projects
in the destination org first (or add manual entries to the mapping file).

Examples:
  circleci-migrate mapping generate \
    --manifest manifest.json \
    --dest-org gh/new-org \
    -o mapping.json

  circleci-migrate mapping generate \
    --manifest manifest.json \
    --dest-org circleci/aaaabbbb-cccc-dddd-eeee-ffffgggghhhh \
    --dest-token $CIRCLECI_DEST_TOKEN \
    -o mapping.json

```
circleci-migrate mapping generate --manifest <file> --dest-org <slug> -o <mapping.json> [flags]
```

### Options

```
      --dest-org string   CircleCI organization slug for the destination org, e.g. gh/new-org (shown in CircleCI → Organization Settings → Overview). This is the CircleCI org identifier, not a GitHub repository URL. (required)
  -h, --help              help for generate
      --manifest string   Path to the export manifest (required)
  -o, --output string     Path to write the mapping file (default: mapping.json next to the manifest)
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

* [circleci-migrate mapping](circleci-migrate_mapping.md)	 - Generate and manage project slug mapping files.

