# skills/

Task-scoped AI skills for `circleci-migrate`. These are a **supported,
versioned interface** — they ship with the CLI, are updated alongside it, and
are checked by CI to ensure every command they reference exists in the generated
CLI reference (`docs/cli/`).

---

## Layout

```
skills/
  run-migration/SKILL.md          Orchestrator: phases mapped 1:1 to docs/guide.md.
                                  Delegates to the other skills. TTY-aware (prefers
                                  the guided walkthrough when interactive; flags when
                                  scripted).
  run-migration/reference.md      Design notes: org types, slug remapping, dry-run-first
                                  rationale, fail-safe defaults.

  capture-secrets/SKILL.md        Secrets capture/extract decision tree + guardrails
                                  (never echo values, encrypted bundle by default,
                                  rotation step after cutover).

  apply-inline-orbs/SKILL.md      Wraps `orb inline`: per-repo PR loop for the
                                  namespace overlap window, post-cutover revert.

  cutover/SKILL.md                Wraps docs/cutover-runbook.md: validation,
                                  enable-builds sequencing, secret rotation,
                                  decommission.

  extend-cli/SKILL.md             Contributor-facing: the manifest + exporter +
                                  syncer + report four-file recipe for adding a
                                  new resource type.
```

---

## Conventions (all skills)

Every skill follows the conventions from the Labs migrator reference implementation:

1. **`description` frontmatter** — written for trigger-phrase matching by AI
   assistants. Lists the phrases that should activate the skill.

2. **Prerequisites gate** — explicit checklist that the agent must verify
   before running any command. If anything is missing, the agent stops and asks.
   The agent never fabricates org IDs, slugs, or UUIDs.

3. **Task-progress checklist** — the agent maintains this checklist as the task
   progresses, checking off items and reporting state.

4. **Guardrails section** — minimum per-skill safety rules:
   - Never print secret values
   - Never run `--apply` without explicit user confirmation
   - Never fabricate org IDs/slugs/UUIDs
   - Route all writes through dry-run first

5. **See also** — each skill ends by pointing at the matching documentation
   (`docs/guide.md`, `docs/cutover-runbook.md`, `docs/cli/` reference).

---

## CI staleness check

Every `circleci-migrate` command referenced in `skills/**/*.md` is checked
by `scripts/check-skill-commands.sh` against the generated CLI reference in
`docs/cli/`. The check fails if a referenced subcommand or flag is absent from
the reference docs — the same pattern that protects `docs/guide.md` via
`scripts/check-doc-flags.sh`.

Run locally:

```bash
./scripts/check-skill-commands.sh
```

---

## Deferred skills

`generate-terraform` — deferred to the Terraform M2 issue. The `terraform
generate` command does not yet exist in the CLI; the skill will be added when
the command ships.

---

## Using with Claude Code (local)

These skills are symlinked (or copied) from `skills/` into `.claude/skills/`
so Claude Code's skill loader can find them. The canonical source is `skills/`;
do not edit `.claude/skills/` directly.

To load a skill in a Claude Code session, reference it by name:
`/run-migration`, `/capture-secrets`, `/apply-inline-orbs`, `/cutover`,
`/extend-cli`.

---

## Future packaging

When `circleci-migrate` merges into the official `circleci` CLI, this
`skills/` directory will become a Claude Code **plugin**
(`circleci-migration` plugin), bundling the skills alongside the MCP/CLI
invocation surface. That is the natural packaging for "AI-guided migration"
as a product capability. Until then, skills are shipped in-repo and loaded
directly.
