## circleci-migrate orb inline

Inline private-orb references into a CircleCI config file.

### Synopsis

inline fetches the published YAML source of each orb referenced in the
supplied config file and replaces the external reference with an inline orb
definition, removing the dependency on the orb namespace.

This is intended as a temporary workaround during the migration overlap window
when the source org's private orbs are not yet resolvable in the destination
org.  REVERT the change once the namespace has been transferred.

WARNING: YAML comments and anchors are NOT preserved by the round-trip
serialisation.  For comment-preserving rewrites, apply the change manually.

Example:
  circleci-migrate orb inline \
    --config .circleci/config.yml \
    --namespace myns \
    --output .circleci/config.inlined.yml

```
circleci-migrate orb inline [flags]
```

### Options

```
      --config string      Path to the .circleci/config.yml file to rewrite (required)
  -h, --help               help for inline
      --namespace string   Only inline orbs whose namespace matches this value (empty = inline all)
  -o, --output string      Output path (default: stdout)
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

* [circleci-migrate orb](circleci-migrate_orb.md)	 - Manage CircleCI orb references in pipeline configs.

