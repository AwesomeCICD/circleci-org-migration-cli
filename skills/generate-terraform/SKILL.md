---
name: generate-terraform
description: >
  Generate Terraform HCL from a CircleCI migration manifest and guide the
  Terraform-managed migration path. Trigger phrases: "generate terraform",
  "terraform generate", "use terraform for migration", "manage with terraform",
  "IaC migration", "terraform manage my circleci org", "generate HCL from
  manifest". Covers the full terraform generate → plan → apply → sync gap-fill
  workflow.
---

# Generate Terraform — Skill

This skill generates Terraform HCL from a `circleci-migrate` manifest and walks
through the complete IaC-managed migration path: generate → plan → apply →
`sync --skip-terraform-managed` gap-fill.

The Terraform path leaves declarative resources (contexts, projects, webhooks,
runners, pipelines) in Terraform state for ongoing `plan`/`apply` cycles. Use it
when the destination org should be managed as code after migration. Use the
standard `sync --apply` path if you just need a one-shot imperative migration.

---

## Prerequisites gate

**STOP AND ASK if any of the following is missing — never fabricate org IDs:**

- [ ] `manifest.json` produced by `circleci-migrate export`
- [ ] Destination org UUID (`--dest-org-id`) — find it at
      **app.circleci.com → Org Settings → Overview**
- [ ] Terraform >= 1.5 installed and on PATH (`terraform version`)
- [ ] Destination org type: `oauth` (`gh/` slug) or `standalone` (`circleci/<uuid>`)
- [ ] Destination CircleCI API token (`CIRCLECI_DEST_TOKEN`) — for the gap-fill
      `sync` step

Optional (ask if unsure):
- Secrets bundle (`--secrets bundle.json`) or placeholder flag (`--placeholders`)
- Mapping file (`--mapping mapping.json`) if project slugs change
- Destination runner namespace (`--dest-runner-namespace`) if runner classes migrate
  to a different namespace
- Existing sync result (`--existing sync-result.json`) if resources were already
  created by `sync` and need to be imported into Terraform state

---

## Task-progress checklist

- [ ] Prerequisites confirmed (manifest, dest-org-id, terraform installed)
- [ ] Terraform files generated in output directory
- [ ] `terraform init` succeeded
- [ ] `terraform plan` reviewed — resource counts match expectations
- [ ] `terraform apply` completed (with user confirmation)
- [ ] GAPS.md reviewed — all remaining items noted
- [ ] `sync --skip-terraform-managed` run to fill CLI-only gaps
- [ ] Destination spot-checked in UI

---

## Guardrails

- **Never print secret values.** Do not `cat` secrets.json or output its contents.
- **Never run `terraform apply` without explicit user confirmation.** Show the plan
  output first; ask "Ready to apply?" before proceeding.
- **Never fabricate dest-org-id UUIDs.** If the user doesn't have the UUID, tell
  them where to find it (Org Settings → Overview in the CircleCI UI).
- **Pipeline resources are standalone-only.** If the destination is OAuth (`gh/`),
  `circleci_pipeline` and `circleci_trigger` are omitted and land in GAPS.md.
  Always check the generated GAPS.md before declaring the migration complete.
- **Context group restrictions are OAuth-only.** For a standalone destination,
  `type=group` restrictions are omitted. They land in GAPS.md.
- **Provider attribute names for pipelines are inferred.** The `circleci_pipeline`
  and `circleci_trigger` nested blocks (`config_source`, `event_source`) are
  labelled `[INFERRED]` in the generated `pipelines.tf`. If `terraform plan`
  fails with "unexpected argument" errors, check the GAPS.md note and raise a
  provider issue.
- **Stop and report** instead of auto-recovering from `terraform` errors. Surface
  the exact error output so the user can act on it.

---

## Step 1 — Generate Terraform files

```bash
circleci-migrate terraform generate \
  --manifest manifest.json \
  --dest-org-id bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb \
  --out ./terraform/
```

With explicit org type (always recommended for clarity):

```bash
# GitHub App / standalone destination
circleci-migrate terraform generate \
  --manifest manifest.json \
  --dest-org-id bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb \
  --dest-org-type standalone \
  --out ./terraform/

# GitHub OAuth destination
circleci-migrate terraform generate \
  --manifest manifest.json \
  --dest-org-id bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb \
  --dest-org-type oauth \
  --out ./terraform/
```

With secret values from a captured bundle:

```bash
circleci-migrate terraform generate \
  --manifest manifest.json \
  --dest-org-id bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb \
  --secrets bundle.json \
  --out ./terraform/
```

With placeholder values (fill-in workbook):

```bash
circleci-migrate terraform generate \
  --manifest manifest.json \
  --dest-org-id bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb \
  --placeholders \
  --out ./terraform/
```

With runner namespace remapping:

```bash
circleci-migrate terraform generate \
  --manifest manifest.json \
  --dest-org-id bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb \
  --dest-runner-namespace acme-new \
  --out ./terraform/
```

**After generation:** show the user a summary of generated files and review
the `GAPS.md` output together.

---

## Step 2 — Review generated files

Files generated in `./terraform/`:

| File | Description |
|------|-------------|
| `versions.tf` | Provider version constraint (`>= 1.5`) |
| `providers.tf` | Provider block with `host` + `organization` |
| `contexts.tf` | `circleci_context` + `circleci_context_environment_variable` |
| `restrictions.tf` | `circleci_context_restriction` (project+expression both orgs; group OAuth-only) |
| `projects.tf` | `circleci_project` + `circleci_project_environment_variable` |
| `webhooks.tf` | `circleci_webhook` (both org types) |
| `runners.tf` | `circleci_runner_resource_class` + `circleci_runner_token` |
| `pipelines.tf` | `circleci_pipeline` + `circleci_trigger` (standalone ONLY) |
| `migration.auto.tfvars.json` | Non-secret resource settings |
| `secrets.auto.tfvars.json` | Env-var values (`--secrets` or `--placeholders` only) |
| `imports.tf` | Terraform 1.5+ `import {}` blocks (`--import-existing` only) |
| `GAPS.md` | Checklist of what Terraform does not manage + exact CLI commands |

Ask the user to confirm the generated resource counts look correct before
running `terraform init`.

---

## Step 3 — Terraform init and plan

```bash
cd ./terraform/
terraform init
terraform plan
```

Review the plan output together:
- Confirm resource counts (contexts, projects, webhooks, runners, pipelines).
- Check for unexpected diffs or errors.
- If `pipelines.tf` has `[INFERRED]` attribute errors, note them and proceed
  with the OAuth path or file a provider issue.

**Ask the user to confirm the plan** before applying.

---

## Step 4 — Terraform apply

Only after explicit user confirmation:

```bash
cd ./terraform/
terraform apply
```

Or non-interactively (CI):

```bash
terraform apply -auto-approve
```

---

## Step 5 — Adopting existing resources (--import-existing)

If resources were previously created by `circleci-migrate sync`, import them
into Terraform state to avoid "already exists" conflicts:

```bash
# Capture existing resource IDs
circleci-migrate sync --manifest manifest.json \
  --dest-token "$CIRCLECI_DEST_TOKEN" \
  --json > sync-result.json

# Regenerate with import blocks
circleci-migrate terraform generate \
  --manifest manifest.json \
  --dest-org-id bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb \
  --import-existing --existing sync-result.json \
  --out ./terraform/

# Plan and apply (imports existing, creates missing)
cd ./terraform/
terraform init && terraform plan
```

The `--import-existing` flag emits `imports.tf` with Terraform 1.5+ `import {}`
blocks for all resources found in the sync result. Resources without a matching
ID are created fresh.

---

## Step 6 — CLI gap-fill after terraform apply

After `terraform apply`, run `sync` with `--skip-terraform-managed` to avoid
overwriting Terraform-owned resources while filling in the CLI-only sections
(org-settings, CIAM, extras):

```bash
circleci-migrate sync \
  --manifest manifest.json \
  --dest-token "$CIRCLECI_DEST_TOKEN" \
  --apply --skip-terraform-managed
```

Or use `--only` to specify exactly which sections to sync:

```bash
circleci-migrate sync \
  --manifest manifest.json \
  --dest-token "$CIRCLECI_DEST_TOKEN" \
  --apply --only org-settings,ciam,extras
```

The sections Terraform manages (and thus `--skip-terraform-managed` skips):
`contexts`, `projects`, `runner` (resource classes + tokens).

---

## Step 7 — Review GAPS.md

After all steps, open the generated `GAPS.md` and work through every remaining
item with the user. Common gaps:

- Secrets (always manual — env-var values must be set)
- Legacy v2 schedules → recreate in destination
- Checkout/deploy keys → re-link from GitHub
- Additional SSH keys → re-add manually
- Project API tokens → regenerate
- CIAM roles and groups → sync with `--only ciam`
- Org-level settings (OIDC, OTel, budgets, retention) → sync with `--only org-settings`
- Context group restrictions (standalone destination) → apply via `sync`
- Pipelines/triggers (OAuth destination) → apply via `sync` or UI

---

## Org-type quick reference

| Source slug | Destination slug | `--dest-org-type` | Notes |
|-------------|------------------|--------------------|-------|
| `gh/<org>` | `gh/<org-new>` | `oauth` | Group restrictions emitted; pipelines omitted |
| `circleci/<uuid>` | `circleci/<uuid>` | `standalone` | Pipelines emitted; group restrictions omitted |
| `gh/<org>` | `circleci/<uuid>` | `standalone` | Cross-type; pipelines emitted |
| `circleci/<uuid>` | `gh/<org>` | `oauth` | Cross-type; pipelines omitted |

When `--dest-org-type` is omitted, the type is inferred from the source org slug
in the manifest and a note is printed. Always pass it explicitly to avoid
surprises.

---

## See also

- [docs/guide.md](../../docs/guide.md) — full step-by-step walkthrough with all scenarios
- [docs/cutover-runbook.md](../../docs/cutover-runbook.md) — operator checklist
- [run-migration/SKILL.md](../run-migration/SKILL.md) — full export → sync orchestrator
- [capture-secrets/SKILL.md](../capture-secrets/SKILL.md) — secrets capture workflow
