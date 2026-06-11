## circleci-migrate completion bash

Generate the autocompletion script for bash

### Synopsis

Generate the autocompletion script for the bash shell.

This script depends on the 'bash-completion' package.
If it is not installed already, you can install it via your OS's package manager.

To load completions in your current shell session:

	source <(circleci-migrate completion bash)

To load completions for every new session, execute once:

#### Linux:

	circleci-migrate completion bash > /etc/bash_completion.d/circleci-migrate

#### macOS:

	circleci-migrate completion bash > $(brew --prefix)/etc/bash_completion.d/circleci-migrate

You will need to start a new shell for this setup to take effect.


```
circleci-migrate completion bash
```

### Options

```
  -h, --help              help for bash
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

