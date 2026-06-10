# circleci-migrate CLI Reference

Auto-generated reference documentation for every command in
`circleci-migrate`. Do not edit these files by hand — they are regenerated
from the live command tree via `make docs` and `make man`.

## Commands

| Command | Description |
|---------|-------------|
| [circleci-migrate](circleci-migrate.md) | Root command — global flags and overview |
| [export](circleci-migrate_export.md) | Export source-org data to a local manifest file |
| [sync](circleci-migrate_sync.md) | Sync a manifest into the destination org |
| [migrate](circleci-migrate_migrate.md) | All-in-one: export and sync in one step |
| [secrets](circleci-migrate_secrets.md) | Manage secret bundles |
| [secrets capture](circleci-migrate_secrets_capture.md) | Capture secret values from CI environment variables |
| [secrets extract](circleci-migrate_secrets_extract.md) | Extract secret values from a manifest |
| [secrets merge](circleci-migrate_secrets_merge.md) | Merge secret bundles |
| [orb](circleci-migrate_orb.md) | Orb-related utilities |
| [orb inline](circleci-migrate_orb_inline.md) | Inline an orb into a config |
| [version](circleci-migrate_version.md) | Display version and build information |

## Regenerating

Run `make docs` to regenerate the markdown reference into `docs/cli/` and
`make man` to regenerate the man pages into `man/`:

```bash
make docs   # regenerates docs/cli/*.md
make man    # regenerates man/*.1
```

Both targets build the binary first, then invoke the hidden `gen-docs`
command with the appropriate output directory.

## Viewing man pages locally

After running `make man`, view any man page with:

```bash
man ./man/circleci-migrate.1
man ./man/circleci-migrate-export.1
man ./man/circleci-migrate-sync.1
```
