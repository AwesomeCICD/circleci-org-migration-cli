## circleci-migrate validate

Compare source and destination orgs and report migration parity.

### Synopsis

validate exports BOTH the source and destination orgs (read-only), then
diffs them by name/structure and prints a per-section parity report.

It reports what matched, what is missing on the destination, and what needs
manual attention. Secret VALUES are never compared — they are masked by the
CircleCI API and intentionally absent from the comparison.

Sections checked:
  • Contexts        — each source context exists on destination; every env-var
                       NAME is present; restrictions are compared by type.
  • Projects        — each source project exists on destination (by mapped slug);
                       every env-var NAME is present; key advanced settings match;
                       additional SSH-key fingerprints and checkout keys are present.
  • Org Settings    — feature flags, OIDC claims, URL-orb allow list, config
                       policies, storage retention, release tracker, contacts,
                       OTel exporters. SSO is always reported as manual (DNS
                       verification + IdP setup required).
  • Runner Classes  — resource classes present in destination namespace. Requires
                       --dest-runner-namespace; skipped with a note when absent.
  • Orbs            — orbs and versions present in destination namespace. Requires
                       --dest-orb-namespace; skipped with a note when absent.
  • CIAM            — if source has CIAM data, a manual verification note is
                       emitted (role bindings must be confirmed by email).

EXIT CODE:
  0 — no missing items (✓ matched and ⚠ manual items only; manual items still
      require operator attention and are listed prominently).
  1 — one or more items are ✗ missing on the destination.

Manual items (⚠) indicate steps that require operator action but do not by
themselves cause a non-zero exit code. They are always listed in the
"NEEDS ATTENTION" block regardless of the exit code.

TOKEN SOURCES (same as migrate/sync):
  --source-token flag or CIRCLECI_SOURCE_TOKEN env var
  --dest-token flag or CIRCLECI_DEST_TOKEN env var
  --token flag or CIRCLECI_CLI_TOKEN env var (fallback for both)

MAPPING:
Pass --mapping to apply the same slug translations as 'sync'. Without a mapping,
source project slugs are compared against the destination using the org-level
slug derivation (gh/old/web ↔ gh/new/web when --source-org gh/old and
--dest-org gh/new).

Examples:
  # Compare two orgs non-interactively:
  circleci-migrate validate \
    --source-org gh/acme --dest-org gh/acme-new \
    --source-token $SRC_TOKEN --dest-token $DST_TOKEN

  # With a mapping file (same mapping used for sync):
  circleci-migrate validate \
    --source-org gh/acme --dest-org gh/acme-new \
    --mapping mapping.json

  # Include runner and orb comparison:
  circleci-migrate validate \
    --source-org gh/acme --dest-org gh/acme-new \
    --dest-runner-namespace acme-new \
    --dest-orb-namespace acme-new

  # Machine-readable JSON output:
  circleci-migrate validate \
    --source-org gh/acme --dest-org gh/acme-new \
    --json

```
circleci-migrate validate --source-org <slug> --dest-org <slug> [flags]
```

### Options

```
      --dest-orb-namespace string      Destination orb namespace to compare orbs against (e.g. 'acme-new'). When omitted the orb section is skipped with an explanatory note.
      --dest-org string                CircleCI organization slug for the destination org, e.g. gh/acme-new (shown in CircleCI → Organization Settings → Overview). (required)
      --dest-runner-namespace string   Destination runner namespace to compare runner resource classes against (e.g. 'acme-new'). When omitted the runner section is skipped with an explanatory note.
  -h, --help                           help for validate
      --json                           Print a machine-readable JSON result to stdout instead of the human-readable report. The JSON contains sections → items → status for tooling consumption.
      --mapping string                 Path to a source→destination mapping file (JSON, optional). Reuses the same format as 'sync --mapping' so you can use the same file for both commands.
      --no-input                       Disable all interactive prompts; error immediately if a required flag is missing. Implied when stdin is not a TTY (e.g. CI pipelines).
      --source-org string              CircleCI organization slug for the source org, e.g. gh/acme (shown in CircleCI → Organization Settings → Overview). (required)
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

