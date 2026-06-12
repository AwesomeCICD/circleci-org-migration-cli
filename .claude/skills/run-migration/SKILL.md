---
name: run-migration
description: >
  Migrate a CircleCI organization to another org. Trigger phrases: "migrate
  my CircleCI org", "move CircleCI org", "migrate CircleCI organization",
  "help me migrate to a new CircleCI org", "run the circleci migration",
  "set up circleci-migrate". Orchestrates the full export → secrets capture →
  sync flow, phase by phase, and delegates to the other skills as needed.
---

# Run Migration — Orchestrator Skill

This skill guides an end-to-end CircleCI org migration using `circleci-migrate`.
It maps 1:1 to the phases in [docs/guide.md](../../docs/guide.md) and delegates
to the task-scoped skills for secrets capture, orb inlining, and cutover.

---

## Prerequisites gate

**STOP AND ASK if any of the following is missing — never fabricate org IDs or slugs:**

- [ ] Source org slug (e.g. `gh/acme` or `circleci/<uuid>`)
- [ ] Destination org slug (e.g. `gh/acme-new` or `circleci/<uuid>`)
- [ ] Source org CircleCI API token (`CIRCLECI_SOURCE_TOKEN`) — must be an org admin
- [ ] Destination org CircleCI API token (`CIRCLECI_DEST_TOKEN`) — must be an org admin
- [ ] `circleci-migrate` installed and on PATH (`circleci-migrate version`)
- [ ] Confirmation of org type: GitHub OAuth (`gh/`), GitHub App (`circleci/<uuid>`), or mixed

Optional (ask if unsure):
- GitHub PAT (`GITHUB_TOKEN`) — only if repos are moving to a different GitHub org
- Runner namespace — only if self-hosted runners are in scope
- Mapping file — only if org name changes or projects are being renamed

---

## Task-progress checklist

The agent maintains this checklist as the migration progresses.

- [ ] Phase 0 — Prerequisites confirmed (slugs, tokens, org types)
- [ ] Phase 1 — Export: `circleci-migrate export` completed; `manifest.json` reviewed
- [ ] Phase 2 — Secrets: `circleci-migrate secrets capture` completed; bundle on disk
- [ ] Phase 3 — Dry run: `circleci-migrate sync` (no `--apply`) reviewed
- [ ] Phase 4 — Apply: `circleci-migrate sync --apply` completed
- [ ] Phase 5 — Validate: destination spot-checked; manual items noted
- [ ] Phase 6 — Cutover: builds enabled, secrets rotated, external pins updated

---

## Guardrails

- **Never print secret values.** Do not `cat` secrets.json or output its contents.
- **Never run `--apply` without explicit user confirmation.** Show the dry-run output first; ask "Ready to apply?" before adding `--apply`.
- **Never fabricate org IDs or slugs.** If you don't have them, ask. OAuth slugs are `gh/<org-name>`; App/GitLab slugs are `circleci/<uuid>` (UUID comes from the CircleCI org settings URL).
- **Route all writes through dry-run first.** Always run `sync` without `--apply` and show the user the plan.
- **TTY awareness:** if the terminal is interactive, prefer `circleci-migrate migrate` (guided walkthrough) for first-time users. For scripted/CI use, pass all flags explicitly plus `--no-input`.
- **Stop and report** instead of auto-recovering from unexpected API errors. Surface the endpoint, status code, and request-id.

---

## Phase 0 — Plan

Determine org types from the slug format. See [reference.md](reference.md) for
the full org-type decision table and slug-remapping rules.

Key questions to resolve before any command:

1. Source slug format: `gh/<org>` (OAuth) or `circleci/<uuid>` (App/standalone/GitLab)?
2. Destination slug format: same type or cross-type?
3. Are repos moving to a different GitHub org? (→ need `GITHUB_TOKEN` + `--dest-github-org`)
4. Is this a mixed org (both `gh/` and `circleci/` records)? (→ run two legs)
5. Are self-hosted runner resource classes in scope? (→ need source/dest runner namespaces)

Set up environment variables now so tokens are never on the command line:

```bash
export CIRCLECI_SOURCE_TOKEN="<source-org-admin-token>"
export CIRCLECI_DEST_TOKEN="<destination-org-admin-token>"
```

---

## Phase 1 — Export the source org

`export` is read-only and safe to re-run.

```bash
circleci-migrate export \
  --source-org gh/acme \
  --output manifest.json \
  --report migration-report.md
```

For a GitHub App org:
```bash
circleci-migrate export \
  --source-org "circleci/11111111-1111-1111-1111-111111111111" \
  --output manifest.json \
  --report migration-report.md
```

With runner namespace:
```bash
circleci-migrate export \
  --source-org gh/acme \
  --runner-namespace acme-runners \
  --output manifest.json \
  --report migration-report.md
```

**After export:** show the user a summary of what was found (context count, project
count, key manual items from the report). Ask them to confirm before proceeding.

If the org has private orbs that reference `awesomecicd/` (or another namespace
that may not resolve in the destination), delegate to
[apply-inline-orbs](../apply-inline-orbs/SKILL.md).

---

## Phase 2 — Capture secrets

Delegate to [capture-secrets](../capture-secrets/SKILL.md).

The recommended flow (interactive, guided):

```bash
circleci-migrate secrets capture
```

Non-interactive (CI-safe):
```bash
circleci-migrate secrets capture \
  --manifest manifest.json \
  --encrypt --generate-key \
  --artifact-retention-days 1 \
  --enable-trigger \
  --output secrets.json
```

Do NOT proceed to Phase 3 without a `secrets.json` (or explicit user confirmation
that they want to sync without secrets, which creates empty env vars).

---

## Phase 3 — Dry run (review the plan)

```bash
circleci-migrate sync \
  --manifest manifest.json \
  --secrets secrets.json \
  --mapping mapping.json
```

Without a mapping file (same-name org migration):
```bash
circleci-migrate sync \
  --manifest manifest.json \
  --secrets secrets.json
```

Show the dry-run output. Every resource shows `created (would create)`, `set (would
set)`, or `manual`. Items marked `manual` require follow-up — note them in the
checklist.

**Ask the user to confirm** before proceeding to Phase 4.

---

## Phase 4 — Apply

Only run `--apply` after explicit user confirmation in Phase 3.

```bash
circleci-migrate sync \
  --manifest manifest.json \
  --secrets secrets.json \
  --mapping mapping.json \
  --apply
```

New projects are created **paused** — no builds fire until you explicitly enable
them. The `--yes` flag auto-enables builds at creation time; **ask the user
whether to pass `--yes`** (do not auto-decide).

For missing secret values use `--missing-secrets placeholder` if the user wants
variable names to exist for manual fill-in.

---

## Phase 5 — Validate

Ask the user to spot-check the destination:

1. Re-export the destination and diff:
   ```bash
   circleci-migrate export \
     --source-org gh/acme-new \
     --source-token "$CIRCLECI_DEST_TOKEN" \
     --output manifest-dest.json
   diff manifest.json manifest-dest.json
   ```

2. Check contexts, env-var names, project settings, webhooks in the CircleCI UI.
3. Work through every `manual` item in `migration-report.md`.

Delegate to [cutover](../cutover/SKILL.md) for the full validation and
enable-builds sequencing.

---

## Phase 6 — Cutover

Delegate to [cutover](../cutover/SKILL.md).

Key steps:
- Enable builds for destination projects
- Rotate every captured secret value
- Delete `secrets.json` and CircleCI artifacts
- Update external pins (Backstage, Slack, status badges, branch-protection)

---

## Interactive mode (first-time use)

On an interactive TTY with no flags, the CLI's built-in walkthrough covers phases
1–4 in one guided session:

```bash
circleci-migrate migrate
```

Pass all flags to skip prompts (CI/scripted use):
```bash
circleci-migrate migrate \
  --source-org gh/acme \
  --dest-org gh/acme-new \
  --secrets secrets.json \
  --apply --yes --no-input \
  --output manifest.json \
  --report migration-report.md
```

---

## See also

- [docs/guide.md](../../docs/guide.md) — full step-by-step walkthrough with all org-type scenarios
- [docs/cutover-runbook.md](../../docs/cutover-runbook.md) — operator checklist + what-does-not-transfer
- [reference.md](reference.md) — org types, slug remapping, dry-run-first rationale
- [capture-secrets/SKILL.md](../capture-secrets/SKILL.md)
- [apply-inline-orbs/SKILL.md](../apply-inline-orbs/SKILL.md)
- [cutover/SKILL.md](../cutover/SKILL.md)
