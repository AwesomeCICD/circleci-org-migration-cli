## circleci-migrate orb

Manage CircleCI orb references in pipeline configs.

### Synopsis

The orb sub-commands help you work with orb references in
.circleci/config.yml files during an organisation migration.

During a migration overlap window a private orb's namespace lives in only one
organisation, so repos that have moved to the destination org cannot resolve
orbs from the source org's namespace.  The "inline" sub-command works around
this by fetching the orb's published source and embedding it directly inside
the consuming config, removing the namespace dependency.  After the namespace
is transferred the change should be reverted.

### Options

```
  -h, --help   help for orb
```

### Options inherited from parent commands

```
      --debug                 Enable debug logging
      --dest-token string     API token for the destination org (env: CIRCLECI_DEST_TOKEN)
      --host string           CircleCI host URL (env: CIRCLECI_CLI_HOST or CIRCLECI_HOST) (default "https://circleci.com")
      --source-token string   API token for the source org (env: CIRCLECI_SOURCE_TOKEN)
      --token string          Personal API token — fallback for both orgs (env: CIRCLECI_CLI_TOKEN)
```

### SEE ALSO

* [circleci-migrate](circleci-migrate.md)	 - Migrate data between CircleCI organisations.
* [circleci-migrate orb inline](circleci-migrate_orb_inline.md)	 - Inline private-orb references into a CircleCI config file.

