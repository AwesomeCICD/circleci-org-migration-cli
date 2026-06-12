## circleci-migrate secrets

Capture secret values that the API cannot expose (RECOMMENDED: use 'secrets capture').

### Synopsis

secrets handles the one thing the CircleCI API cannot: secret VALUES.

CircleCI masks environment-variable values everywhere in its API, so 'export'
captures only their names. The 'secrets' subcommands recover those values by
running a pipeline inside CircleCI and reading the variables from the job env.

NOTE: 'secrets extract' is designed to run INSIDE a CircleCI job (not locally).
For the recommended local workflow, use 'secrets capture' instead.

RECOMMENDED PATH — 'secrets capture' (CLI-orchestrated, no committed config):

  circleci-migrate secrets capture

  Run on an interactive terminal to launch the guided walkthrough. The CLI:
    • Loads your manifest to list available contexts and projects.
    • Lets you pick which contexts and projects to extract.
    • Prompts for the HOST PROJECT under which context extraction runs
      (any project works; build history is irrelevant).
    • Recommends encryption (age) so plaintext never persists in artifacts.
    • Builds an inline unversioned pipeline config and triggers the run.
    • Polls until completion, then downloads and decrypts the artifact.
    • Writes the captured values to a local secret bundle.

  All flags bypass prompts for CI/scripted use — see 'secrets capture --help'.

ALTERNATIVE PATH — orb / 'secrets extract' (committed config):

  Use 'circleci-migrate orb inline' or the awesomecicd/circleci-org-migration
  orb to add an extraction job to an existing pipeline config. The in-job
  'secrets extract' command reads values from the job environment.

  This path requires committing a config change but gives you full control
  over when and how the extraction job runs.

Subcommands:
  capture   CLI-orchestrated extraction via unversioned pipeline (RECOMMENDED)
  transfer  Direct source→dest transfer via in-pipeline PUT (no bundle file; context values only)
  extract   In-job extraction from the current environment (orb path)
  decrypt   Decrypt an age-encrypted secret bundle locally
  merge     Merge multiple secret bundles into one

### Options

```
  -h, --help   help for secrets
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
* [circleci-migrate secrets capture](circleci-migrate_secrets_capture.md)	 - Capture secret values by running an unversioned pipeline inside CircleCI (RECOMMENDED).
* [circleci-migrate secrets decrypt](circleci-migrate_secrets_decrypt.md)	 - Decrypt an age-encrypted secret bundle.
* [circleci-migrate secrets extract](circleci-migrate_secrets_extract.md)	 - Extract secret values from the current job environment (for use in orb/pipeline jobs).
* [circleci-migrate secrets merge](circleci-migrate_secrets_merge.md)	 - Merge multiple secret bundles into one.
* [circleci-migrate secrets transfer](circleci-migrate_secrets_transfer.md)	 - Transfer context env-var values directly source→dest via an in-pipeline PUT (no bundle file).

