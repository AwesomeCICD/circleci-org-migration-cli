## circleci-migrate completion fish

Generate the autocompletion script for fish

### Synopsis

Generate the autocompletion script for the fish shell.

To load completions in your current shell session:

	circleci-migrate completion fish | source

To load completions for every new session, execute once:

	circleci-migrate completion fish > ~/.config/fish/completions/circleci-migrate.fish

You will need to start a new shell for this setup to take effect.


```
circleci-migrate completion fish [flags]
```

### Options

```
  -h, --help              help for fish
      --no-descriptions   disable completion descriptions
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

* [circleci-migrate completion](circleci-migrate_completion.md)	 - Generate the autocompletion script for the specified shell

