package cmd

// secretsCaptureLong is the long help text for the 'secrets capture' command,
// split out of secrets_capture.go to keep the command wiring compact.
const secretsCaptureLong = `capture is the RECOMMENDED way to extract secret values from CircleCI.

It extracts plaintext environment-variable values WITHOUT committing any config
to the target repository. The CLI builds an inline (unversioned) pipeline config,
triggers a run inside CircleCI, and downloads the captured values automatically.

  RECOMMENDED: run 'secrets capture' on an interactive terminal without flags to
  launch the guided walkthrough. It prompts for each option with sensible defaults
  and explicit guidance on host-project selection for context extraction.

NON-INTERACTIVE MODE & THE CAPTURE-ALL GUARD:
  Only --manifest is strictly required. Once --manifest is supplied (or stdin is
  not a TTY) capture runs non-interactively and does NOT launch the guided
  walkthrough. By default, when neither --context nor --project is given, capture
  would select EVERY context and project that has at least one env var in the
  manifest and trigger a REAL CircleCI extraction pipeline for each one.

  To prevent accidental org-wide extraction (e.g. from a piped or recorded
  session), an unattended capture-all is FAIL-CLOSED: if the run is
  non-interactive, neither --context nor --project is set, and you have NOT
  passed --yes (or --no-input), capture errors out instead of triggering
  pipelines. You then have three options:
    1. Pass --context and/or --project to scope exactly what is captured.
    2. Pass --yes (or --no-input) to acknowledge an unattended capture-all.
    3. Run on an interactive TTY (no --manifest) for the guided walkthrough.

  Interactive prompts are written to stderr; if you pipe stdout while relying on
  the guided prompts, use a TTY for stdin — piping stdin triggers non-TTY mode.

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
    --storage s3 --s3-bucket my-migration-bucket --s3-prefix migration/`
