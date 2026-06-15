## circleci-migrate mapping

Generate and manage project slug mapping files.

### Synopsis

mapping provides utilities for creating the mapping.json file that tells
'sync' and 'secrets transfer' how source project slugs correspond to
destination project slugs.

The most common use is 'mapping generate', which auto-matches projects by
repo name (the last segment of the slug) so you don't have to hand-write
mapping.json for a standard org rename.

### Options

```
  -h, --help   help for mapping
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
* [circleci-migrate mapping generate](circleci-migrate_mapping_generate.md)	 - Auto-generate a project slug mapping from a manifest and a destination org.

