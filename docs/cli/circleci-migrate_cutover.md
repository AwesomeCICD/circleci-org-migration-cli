## circleci-migrate cutover

Switch builds from the source org to the destination (pause source, enable destination, undo).

### Synopsis

cutover performs the final switch of a migration: it stops builds in the SOURCE
org and turns builds on in the DESTINATION org, recording every change so it can
be reversed. It is a DRY RUN unless --apply is given.

  --pause-source   For each source project, pause it reversibly: set the v1.1
                   feature flag drop-all-build-requests=true on OAuth/VCS
                   projects (the prior value is recorded), and disable the
                   source triggers that are enabled and are not github_oauth
                   triggers (the ids are recorded). github_oauth triggers cannot
                   be disabled through the API, so OAuth projects rely on the
                   flag. Projects are NEVER unfollowed (that is destructive).
  --pause-org      Also set drop-all-build-requests=true on the whole source org
                   (the org-level flag overrides every project). Use with
                   --pause-source.
  --enable-dest    Enable the destination triggers whose source counterpart was
                   enabled (triggers are matched exactly as sync matches them).
                   A trigger that was disabled in the source stays disabled.
                   Run sync --apply first so the destination triggers exist.
  --undo           Restore everything recorded in the state file: source flags
                   back to their prior values, the source triggers it disabled
                   re-enabled, and the destination triggers it enabled
                   disabled again. Needs only --state (and --apply to act).

Every change is written to the state file (--state, default cutover-state.json, mode 0600)
BEFORE it is made, so --undo works even after a partial failure. Re-running is
idempotent: items already in the wanted state report "exists". Each item is
printed with its outcome and the command exits non-zero if any item failed.

Order for a clean cutover: sync --apply (destination created paused), validate,
cutover --pause-source --apply, cutover --enable-dest --apply (the two can be
given together: the source is paused first). If something is wrong, cutover
--undo --apply returns the source to its previous state.

CAUTION: once the GitHub App is UNINSTALLED from the source GitHub org, the
source's GitHub App triggers no longer exist, so they can neither be disabled
nor re-enabled; pausing then relies on drop-all-build-requests alone, and --undo
can only restore that flag. Pause the source BEFORE uninstalling the App.

Source actions use the source token (--source-token / CIRCLECI_SOURCE_TOKEN,
else --token); destination actions use the destination token (--dest-token /
CIRCLECI_DEST_TOKEN, else --token).

Examples:
  circleci-migrate cutover --manifest manifest.json --pause-source
  circleci-migrate cutover --manifest manifest.json --mapping mapping.json --pause-source --enable-dest --apply
  circleci-migrate cutover --manifest manifest.json --pause-source --pause-org --apply
  circleci-migrate cutover --undo --apply

```
circleci-migrate cutover [--pause-source] [--pause-org] [--enable-dest] [--undo] --manifest <file> [--apply] [flags]
```

### Options

```
      --apply             Make the changes (default: dry run)
      --enable-dest       Enable destination triggers whose source counterpart was enabled
  -h, --help              help for cutover
      --manifest string   Path to the export manifest (required unless --undo)
      --mapping string    Path to the source->destination mapping file (JSON); org.to is required with --enable-dest
      --pause-org         Also set drop-all-build-requests on the whole source org (with --pause-source)
      --pause-source      Pause the source: drop-all-build-requests on OAuth projects and disable enabled source App triggers
      --state string      State file recording every change (mode 0600); read by --undo (default "cutover-state.json")
      --undo              Restore everything recorded in the state file
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

