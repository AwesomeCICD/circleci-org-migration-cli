# Plan builder tests

Run from the repository root with Node 24 and no installs:

```bash
node --test site/tests/*.test.mjs
```

On Node 24 a directory argument is resolved as a module instead of being expanded, so CI and this page use the glob form:
`node --test site/tests/*.test.mjs`.

## What the tests check

- **Validation.** Slug shapes per migration type, org ID format, source differs from destination, a fix
  message for a type and slug mismatch, namespaces, server URLs, and the warnings for risky combinations.
- **Runbook parity.** The step order of each scenario is pinned to its runbook in `docs/migrations/`, every step
  links to a heading that exists there, and every `circleci-migrate` command in a runbook is emitted by the plan.
- **Option toggles.** Each checkbox adds or removes the right flags and steps.
- **Shells.** PowerShell commands are single lines with no backslash or backtick.
- **Public-site hygiene.** Tokens appear only as `<paste token>`, and no internal admin command, customer
  name or internal URL appears in any generated text.
- **Flags exist.** The suite parses `docs/cli/circleci-migrate_*.md` (generated, so authoritative), builds a
  table of every subcommand and its flags, then builds a plan for every scenario, every combination of the
  ten options, both shells and cloud and server hosts. It asserts that every `--flag` of every
  `circleci-migrate` command exists for that command. Regenerate `docs/cli` (`make docs`) when a CLI flag
  changes, then run the tests: a renamed or removed flag fails here before it reaches a reader.

## Files

| File | Role |
|---|---|
| `site/assets/js/plan/plan.mjs` | Validation, `buildPlan`, run sheet, shareable state. No DOM. |
| `site/assets/js/plan/catalog.mjs` | Every step, and the ordered step ids of each scenario. |
| `site/assets/js/plan/ui.mjs` | The form and the rendered plan. Bundled by Hugo's `js.Build`. |
| `site/layouts/_shortcodes/plan-builder.html` | Loads the CSS and the bundled JS. |
