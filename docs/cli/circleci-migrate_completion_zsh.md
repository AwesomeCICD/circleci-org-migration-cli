## circleci-migrate completion zsh

Generate the autocompletion script for zsh

### Synopsis

Generate the autocompletion script for the zsh shell.

If shell completion is not already enabled in your environment you will need
to enable it.  You can execute the following once:

	echo "autoload -U compinit; compinit" >> ~/.zshrc

To load completions in your current shell session:

	source <(circleci-migrate completion zsh)

To load completions for every new session, execute once:

#### Linux:

	circleci-migrate completion zsh > "${fpath[1]}/_circleci-migrate"

#### macOS:

	circleci-migrate completion zsh > $(brew --prefix)/share/zsh/site-functions/_circleci-migrate

You will need to start a new shell for this setup to take effect.


```
circleci-migrate completion zsh [flags]
```

### Options

```
  -h, --help              help for zsh
      --no-descriptions   disable completion descriptions
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

* [circleci-migrate completion](circleci-migrate_completion.md)	 - Generate the autocompletion script for the specified shell

