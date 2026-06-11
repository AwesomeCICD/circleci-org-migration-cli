## circleci-migrate secrets capture

Capture secret values by running an unversioned pipeline inside CircleCI (RECOMMENDED).

### Synopsis

capture is the RECOMMENDED way to extract secret values from CircleCI.

It extracts plaintext environment-variable values WITHOUT committing any config
to the target repository. The CLI builds an inline (unversioned) pipeline config,
triggers a run inside CircleCI, and downloads the captured values automatically.

  RECOMMENDED: run 'secrets capture' on an interactive terminal without flags to
  launch the guided walkthrough. It prompts for each option with sensible defaults
  and explicit guidance on host-project selection for context extraction.

  For the orb-based alternative (committed config), see:
    circleci-migrate orb inline --help
    circleci-migrate secrets extract --help

HOW IT WORKS:
  1. Reads variable names from the manifest for the selected project(s) and
     context(s).
  2. Ensures api-trigger-with-config is enabled for each project (either it
     must already be on, or --enable-trigger must be set).
  3. Triggers an unversioned pipeline run with an inline config that dumps the
     variable values to a build artifact.
  4. Polls until the pipeline completes, then downloads and parses the artifact.
  5. Writes the captured values into the secret bundle (--output).
  6. Restores the api-trigger-with-config flag to its original value (even on
     failure).

HOST PROJECT FOR CONTEXT EXTRACTION:
  Context env vars are injected into a job that references the context.
  The pipeline must run under some project — this is the "host project".
  Any project works; build history is irrelevant (only extraction matters).
  Use --host-project to specify it; the guided mode prompts you to choose.
  Project env vars are always captured under each project's own pipeline.

ENCRYPTION (default: ON — use --no-encrypt to opt out):
  By default, the in-pipeline extraction job encrypts the artifact with age so
  that plaintext secrets NEVER persist in CircleCI artifact storage. Encryption
  requires a public key: supply --ssh-public-key or --generate-key. When neither
  is given, capture auto-generates a fresh keypair (--generate-key behaviour).

  After the run, capture downloads the .age artifact and decrypts it locally
  with --ssh-private-key (or the generated key) to build the in-memory bundle.

  Use --no-encrypt to disable encryption and accept a PLAINTEXT artifact. This
  is strongly discouraged for production secrets — build artifacts are retained
  for at least 1 day and there is no delete-artifact API.

  Use --generate-key to have capture create a fresh age X25519 keypair
  automatically, print the file paths, and use it for this run.

STORAGE (--storage):
  artifact (default) — store the bundle as a CircleCI job artifact.
  s3                 — upload to S3 only (requires aws CLI + AWS creds in job).
  both               — store in both artifact and S3.

  For S3 storage provide --s3-bucket and (optionally) --s3-prefix.
  The job executor must have AWS credentials via a context or project env vars.

SECURITY NOTES:
  - Without --encrypt: the secret bundle contains plaintext secrets. Protect it.
  - Build artifacts are retained for at least 1 day; there is no delete API.
    With --encrypt the artifact is age-encrypted so plaintext never hits disk.
  - Rotate any captured secrets after migration.

Examples:
  # Interactive guided walkthrough (recommended for first-time use):
  circleci-migrate secrets capture

  # Non-interactive with encryption (default; auto-generates a keypair):
  circleci-migrate secrets capture --manifest manifest.json --source-token $TOKEN
  circleci-migrate secrets capture --manifest manifest.json --project gh/acme/web \
    --enable-trigger --branch main -o secrets.json
  # Encrypted capture with auto-generated key (explicit):
  circleci-migrate secrets capture --manifest manifest.json --generate-key
  # Encrypted capture with existing SSH key:
  circleci-migrate secrets capture --manifest manifest.json \
    --ssh-public-key ~/.ssh/id_ed25519.pub --ssh-private-key ~/.ssh/id_ed25519
  # Opt out of encryption (PLAINTEXT artifact — NOT recommended):
  circleci-migrate secrets capture --manifest manifest.json --no-encrypt
  # Context capture specifying host project explicitly:
  circleci-migrate secrets capture --manifest manifest.json \
    --context deploy-prod --host-project gh/acme/web --enable-trigger
  # Upload encrypted bundle to S3 instead of artifact:
  circleci-migrate secrets capture --manifest manifest.json --generate-key \
    --storage s3 --s3-bucket my-migration-bucket --s3-prefix migration/

```
circleci-migrate secrets capture [--manifest <file>] [flags]
```

### Options

```
      --artifact-retention-days int   Set the org's artifact-retention to this many days BEFORE triggering the extraction pipeline. Recommended value: 1 (the minimum). Default 0 = leave unchanged. The prior value is logged so you can restore it manually. This control is NOT auto-restored after capture — keeping retention low is the safe default when secrets may land in artifacts.
      --branch string                 Branch to check out for the extraction run (default "main")
      --context stringArray           Context name(s) to capture (default: all in manifest)
      --enable-trigger                Enable api-trigger-with-config if not already on, and restore after capture
      --encrypt                       Encrypt the in-pipeline artifact with age so plaintext secrets never persist in CircleCI (default: true). Supply --ssh-public-key or --generate-key; if neither is given a fresh keypair is auto-generated. Use --no-encrypt to opt out. (default true)
      --generate-key                  Generate a fresh age X25519 keypair for this run. Writes the identity to ./migration-identity.age and the recipient to ./migration-recipient.txt. Use --generate-key instead of --ssh-public-key when you do not have an existing key. Auto-enabled when --encrypt is in effect and no key is supplied.
  -h, --help                          help for capture
      --host-project string           Project slug to use when running the CONTEXT extraction pipeline. Any project works — build history is irrelevant; only the extraction matters. Prompted interactively when contexts are selected and this flag is absent.
      --manifest string               Path to the export manifest (prompted interactively when omitted on a TTY)
      --no-encrypt                    Disable artifact encryption and produce a PLAINTEXT secrets artifact. NOT recommended for production secrets — build artifacts are retained for at least 1 day and there is no delete-artifact API.
      --no-input                      Disable all interactive prompts; error if a required value is missing (implied when stdin is not a TTY)
  -o, --output string                 Path to the secret bundle to write/append (default "secrets.json")
      --poll-timeout duration         Maximum time to wait for each pipeline to complete (0 = no timeout) (default 10m0s)
      --project stringArray           Project slug(s) to capture (default: all in manifest)
      --remove-restrictions           Temporarily remove real context restrictions before extraction and restore them afterwards (requires explicit opt-in)
      --s3-bucket string              S3 bucket name for --storage s3|both (required when --storage s3 or both)
      --s3-prefix string              S3 key prefix for --storage s3|both (optional; e.g. 'migration/')
      --skip-restricted-contexts      Skip contexts that have project/expression/group restrictions (attach warning instead of attempting) (default true)
      --ssh-private-key string        Path to an SSH private key or age identity file used to decrypt the artifact locally. Defaults to ~/.ssh/id_ed25519 if present and --ssh-public-key points to the matching .pub.
      --ssh-public-key string         Path to an SSH public key (.pub) or age recipients file used as the encryption recipient. The public key is safe to embed in the pipeline config.
      --storage string                Where to store the (optionally encrypted) bundle after extraction.
                                      artifact (default) — store as a CircleCI job artifact.
                                      s3                 — upload to S3 via the aws CLI (requires AWS creds in job).
                                      both               — store in both artifact and S3. (default "artifact")
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

* [circleci-migrate secrets](circleci-migrate_secrets.md)	 - Capture secret values that the API cannot expose (RECOMMENDED: use 'secrets capture').

