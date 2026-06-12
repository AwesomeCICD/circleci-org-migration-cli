---
name: extend-cli
description: >
  Add a new resource type to circleci-migrate. Contributor-facing skill.
  Trigger phrases: "add a new resource to circleci-migrate", "extend the
  migration CLI", "add a manifest section", "implement a new exporter",
  "add a syncer for a new CircleCI resource", "contribute to circleci-migrate",
  "how do I add support for a new CircleCI resource type". The four-file recipe:
  manifest section + exporter + syncer + report entry.
---

# Extend the CLI — Contributor Recipe

This skill guides contributors through adding support for a new CircleCI
resource type to `circleci-migrate`. It documents the four-file recipe that
all resource types follow: manifest section, exporter, syncer, and report.

For full development setup, see [CONTRIBUTING.md](../../CONTRIBUTING.md).

---

## Prerequisites gate

**STOP AND ASK if any of the following is missing:**

- [ ] Name of the CircleCI resource to add (e.g. "project webhook", "runner token")
- [ ] API endpoint(s) that read the resource (export side)
- [ ] API endpoint(s) that write or create the resource (sync side)
- [ ] Confirmation of whether the resource has secret values (→ affects manifest design)
- [ ] Confirmation of whether it is org-scoped or project-scoped
- [ ] The Go module is cloned and `make build` passes locally

---

## Task-progress checklist

- [ ] API endpoints identified and documented (see `docs/api-usage.md` for the pattern)
- [ ] Manifest struct added (`internal/manifest/`)
- [ ] Exporter implemented (`internal/export/`) + unit tests
- [ ] Syncer implemented (`internal/sync/`) + unit tests
- [ ] Report entry added (`internal/report/`)
- [ ] `make build` passes
- [ ] `make lint` passes (0 issues — run before committing)
- [ ] `make cover` passes (coverage gate, currently 85%)
- [ ] `make docs` regenerated (`docs/cli/*.md` and `man/`)
- [ ] `make verify` passes
- [ ] PR opened referencing the relevant issue

---

## Guardrails

- **Never store secret values in the manifest.** The manifest is non-secret and safe to commit and share. Secret values (env-var values, token values) go in the secrets bundle only.
- **Never add API calls to `internal/manifest/` packages.** Manifest structs are pure data; API calls belong in the exporter.
- **Run `make lint` before every commit.** The v0.5–v0.7 audit found that skipping lint let stale patterns accumulate. The CI gate catches it, but catching it locally is faster.
- **Never fabricate API endpoint paths or response shapes.** Check against the live API reference or `docs/api-usage.md` first.
- **Dry-run by default.** All syncers must check the dry-run flag and print `(would create)` / `(would set)` instead of writing. Use the existing syncers as a template.

---

## The four-file recipe

Every resource type in `circleci-migrate` follows this pattern:

```
1. internal/manifest/<resource>.go     — struct(s) for the manifest section
2. internal/export/<resource>.go       — API read → populate manifest struct
3. internal/sync/<resource>.go         — manifest struct → API write (dry-run-aware)
4. internal/report/<resource>.go       — human-readable report section
```

---

## Step 1 — Manifest struct

Add the resource's data shape to `internal/manifest/`. The manifest must:

- Contain only **names, IDs, and non-secret settings** — never values.
- Be JSON-serializable (all exported fields, `json:"..."` tags).
- Include a `SchemaVersion` guard if the shape is likely to change.

Example pattern:

```go
// internal/manifest/webhook.go
package manifest

// Webhook captures an outbound webhook configuration for a project.
// Values (HMAC signing secrets) are never captured — they must be
// regenerated after sync.
type Webhook struct {
    Name   string `json:"name"`
    URL    string `json:"url"`
    Events []string `json:"events"`
    // SigningSecret is intentionally omitted — not returned by the API.
}
```

Embed the new struct in the top-level `Manifest` struct in
`internal/manifest/manifest.go`.

---

## Step 2 — Exporter

Add the export logic in `internal/export/`. The exporter:

- Makes read-only API calls against the **source** org.
- Populates the manifest struct.
- Must work with `--skip-extras` / `--skip-projects` / `--skip-contexts` flags
  (check the existing exporters to see how they honor skips).
- Must handle pagination (use the existing pagination helper or pattern).
- Must surface API errors without panicking.

Unit-test with a recorded HTTP fixture (see existing `*_test.go` files for the
`httptest` pattern).

---

## Step 3 — Syncer

Add the sync logic in `internal/sync/`. The syncer:

- Reads from the manifest struct.
- Is **dry-run by default**: checks `opts.DryRun` and prints
  `resource: name (would create)` / `(would set)` instead of writing.
- Is **idempotent**: on `--apply`, it checks if the resource already exists
  and skips creation (or updates in-place if the resource supports it).
- Reports each action with a status word: `created`, `exists`, `set`, `manual`,
  or `error`.
- Uses `--dest-token` / `CIRCLECI_DEST_TOKEN` for authentication.
- Emits `manual` for anything that cannot be automated (regenerated secrets,
  resources requiring manual UI steps).

---

## Step 4 — Report entry

Add a section to `internal/report/` (or extend an existing section) so the
generated `migration-report.md` lists:

- How many of the resource were found / synced.
- Any `manual` items with clear instructions.
- Any data-loss warnings (things that cannot transfer).

Update the canonical "does not transfer" table in `docs/cutover-runbook.md` if
the new resource has items that cannot be migrated automatically.

---

## Step 5 — Wire it up

1. Call the new exporter from `cmd/export.go` (or the appropriate entry point).
2. Call the new syncer from `cmd/sync.go` / `cmd/sync_wiring.go`.
3. Add `--skip-<resource>` flag if the resource should be skippable (follow the
   existing `--skip-contexts`, `--skip-projects`, `--skip-extras` pattern).
4. Add the flag to `cmd/migrate.go` if `migrate` should also pass it through.

---

## Step 6 — Regenerate docs and verify

```bash
make build     # must pass
make docs      # regenerates docs/cli/*.md and man/
make lint      # 0 issues — run BEFORE committing
make verify    # parity checks
make cover     # 85% gate
```

If `make docs` changes any file, commit the regenerated docs alongside the
code change. The CI `docs-check` gate will fail if generated docs are stale.

---

## Testing checklist

- [ ] Unit tests for the exporter (recorded HTTP fixtures)
- [ ] Unit tests for the syncer (dry-run + apply paths)
- [ ] Unit tests for the manifest struct (marshaling round-trip)
- [ ] `TestParityExportSync` passes (parity test in `cmd/parity_test.go` — checks
  that every manifest field has a corresponding sync handler)
- [ ] E2E smoke passes (`make e2e` or the CI e2e job)

---

## Where to look for patterns

| Resource | Exporter | Syncer |
|---|---|---|
| Contexts | `internal/export/contexts.go` | `internal/sync/contexts.go` |
| Projects | `internal/export/projects.go` | `internal/sync/projects.go` |
| Org settings | `internal/export/orgsettings.go` | `internal/sync/orgsettings.go` |
| Runner resource classes | `internal/export/runner.go` | `internal/sync/runner.go` |

---

## See also

- [CONTRIBUTING.md](../../CONTRIBUTING.md) — development setup, local validation loop
- [docs/architecture.md](../../docs/architecture.md) — how export and sync are structured
- [docs/api-usage.md](../../docs/api-usage.md) — API endpoints the CLI uses
- [docs/adr/](../../docs/adr/) — architecture decision records (why certain patterns exist)
