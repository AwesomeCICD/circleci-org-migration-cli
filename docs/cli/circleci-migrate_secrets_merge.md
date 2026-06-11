## circleci-migrate secrets merge

Merge multiple secret bundles into one.

### Synopsis

merge combines several secret bundles (for example, the per-context
bundles produced by separate extraction jobs) into a single bundle.

When --encrypt is set the merged bundle is written as an age-encrypted file
(<output>.age) instead of plaintext. Provide the recipient's public key via
--recipient (inline) or --recipient-file (path to a .pub or age recipients
file). The corresponding private key is required to decrypt with
'secrets decrypt'.

Example:
  circleci-migrate secrets merge -o secrets.json artifacts/*/secrets.json
  circleci-migrate secrets merge --encrypt --recipient-file ~/.ssh/id_ed25519.pub \
    -o secrets.json artifacts/*/secrets.json

```
circleci-migrate secrets merge -o <out> <bundle.json>... [flags]
```

### Options

```
      --encrypt                 Encrypt the output bundle with age (writes <output>.age; requires --recipient or --recipient-file)
  -h, --help                    help for merge
  -o, --output string           Path to write the merged bundle (default "secrets.json")
      --recipient string        age or SSH public key recipient string (ssh-ed25519/ssh-rsa/age1...)
      --recipient-file string   Path to an SSH public key (.pub) or age recipients file
```

### Options inherited from parent commands

```
      --debug                 Enable debug logging
      --dest-token string     API token for the destination org (env: CIRCLECI_DEST_TOKEN)
      --host string           CircleCI host URL (env: CIRCLECI_HOST) (default "https://circleci.com")
      --source-token string   API token for the source org (env: CIRCLECI_SOURCE_TOKEN)
      --token string          Personal API token — fallback for both orgs (env: CIRCLECI_CLI_TOKEN)
```

### SEE ALSO

* [circleci-migrate secrets](circleci-migrate_secrets.md)	 - Capture secret values that the API cannot expose (RECOMMENDED: use 'secrets capture').

