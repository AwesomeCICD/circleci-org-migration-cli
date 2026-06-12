---
name: cutover
description: >
  Execute the CircleCI org migration cutover: validate the destination, enable
  builds, rotate secrets, decommission the source. Trigger phrases: "cutover
  to new CircleCI org", "enable builds in destination org", "go live with
  migration", "finalize circleci migration", "rotate migrated secrets",
  "decommission source circleci org", "validate migrated org", "migration
  cutover runbook". Wraps docs/cutover-runbook.md phases 5–8.
---

# Cutover

This skill covers the production cutover after `sync --apply` has completed:
validation, enabling builds, secret rotation, and decommissioning the source.
It maps to [docs/cutover-runbook.md](../../docs/cutover-runbook.md) steps 5–8.

**Do not start cutover until `sync --apply` has completed and been verified.**

---

## Prerequisites gate

**STOP AND ASK if any of the following is missing:**

- [ ] `sync --apply` has completed successfully (no fatal errors in the output)
- [ ] `manifest.json` and `migration-report.md` are available for reference
- [ ] Destination org token (`CIRCLECI_DEST_TOKEN`) is available
- [ ] User has reviewed the `manual` items in the migration report
- [ ] User has confirmed they are ready to enable builds (this is irreversible until manually disabled)

---

## Task-progress checklist

- [ ] Step 5 — Destination validated (contexts, env-var names, project settings)
- [ ] Step 5 — Manual follow-up items worked through (see migration-report.md)
- [ ] Step 6 — Builds enabled (App triggers unpaused; OAuth projects followed)
- [ ] Step 6 — Test build triggered on destination; confirmed healthy
- [ ] Step 7 — Every captured secret value rotated
- [ ] Step 7 — `secrets.json` deleted from local machine
- [ ] Step 7 — CircleCI extraction artifacts expired or deleted
- [ ] Step 8 — External pins updated (Backstage, Slack, status badges, etc.)
- [ ] Decommission — Source org decommission plan confirmed with user

---

## Guardrails

- **Never enable builds without explicit user confirmation.** Enabling builds makes the destination org go live; this is not reversible without manual intervention.
- **Never rotate a secret without confirming the new value has been set in the destination.** Rotation order: set new value in destination → confirm destination builds pass → then remove/rotate source value.
- **Never delete `secrets.json` until the user confirms the destination is healthy and all rotations are complete.**
- **Never decommission the source org without explicit user confirmation.** Always present a summary of what "decommission" means (archiving projects, removing webhooks, etc.) before proceeding.
- **Never fabricate org IDs, slugs, or UUIDs.**

---

## Step 5 — Validate the destination

### Re-export and diff

```bash
circleci-migrate export \
  --source-org gh/acme-new \
  --source-token "$CIRCLECI_DEST_TOKEN" \
  --output manifest-dest.json \
  --report migration-report-dest.md

diff manifest.json manifest-dest.json
```

Differences in UUIDs (context IDs, project IDs) are expected — these are
reassigned by the destination org. Differences in names, counts, or settings
are worth investigating.

### UI spot-checks

Walk the user through verifying in the CircleCI UI:

- **Contexts:** names match; env-var *names* (not values) match; restrictions recreated
- **Projects:** advanced settings, env-var names, webhooks, scheduled pipelines
- **Org settings:** feature flags, OIDC claims, config policies, OTel exporters
- **Runner resource classes:** recreated under the destination namespace

### Manual follow-up items

Work through every `manual` item in `migration-report.md`. Common items:

| Item | Action |
|---|---|
| Webhook signing secrets | Regenerate in destination org; update receiving endpoints |
| SSO (SAML) | DNS TXT verification + IdP-side SAML app setup |
| Audit-log streaming | Recreate each stream against destination-owned AWS infrastructure |
| OTel exporter headers | Re-add header values manually (server-redacted by the API) |
| Context project restrictions | Recreate in destination org settings UI (source UUIDs don't transfer) |
| Per-project CIAM role grants | Recreate user/group grants on each destination project |
| Project API tokens | Recreate with `--create-project-tokens` or manually; repoint consumers |
| Checkout / deploy keys | Regenerate on destination; update VCS-side deploy key |
| Org orbs | Republish in destination namespace |

---

## Step 6 — Enable builds

**Ask the user explicitly: "Are you ready to enable builds? This will start
pipelines running in the destination org."**

### If builds were not enabled during `sync --apply`

Re-run sync with `--apply --yes` (idempotent — only enables, does not re-create):

```bash
circleci-migrate sync \
  --manifest manifest.json \
  --mapping mapping.json \
  --apply --yes
```

### What enabling does by org type

- **GitHub App:** unpauses pipeline triggers. Subsequent VCS pushes and
  schedule/webhook events will fire.
- **GitHub OAuth:** follows each project, installing a deploy key and webhook.
  The first follow may trigger an initial build.

### Trigger a test build

Ask the user to trigger a test pipeline on the destination and confirm:
- The pipeline runs to completion
- Secrets are injected (build passes where it uses env vars)
- No unexpected permissions errors

---

## Step 7 — Rotate secrets and clean up

**Secret rotation must happen AFTER the destination is confirmed healthy.**

For each captured secret value:

1. Generate a new value (or rotate it in the source system).
2. Set the new value in the destination org (context or project env var in the CircleCI UI, or via API).
3. Confirm the destination build still passes with the new value.
4. Remove or rotate the old value in the source org.

### Clean up after rotation

```bash
# Delete secrets.json (after all rotations confirmed)
rm -P secrets.json   # macOS secure delete; use shred on Linux

# Revoke the CircleCI extraction artifact (if within retention window)
# (No delete-artifact API exists; set artifact-retention-days 1 before capture
# to minimize this window. The artifact expires automatically.)
```

Also:
- Clear any terminal history that may contain values (`history -c` or equivalent)
- Delete any local logs from the capture pipeline that include env-var values
- Revoke the `migration-identity.age` keypair if `--generate-key` was used and
  the private key is no longer needed

---

## Step 8 — Update external pins

Update everything that references the old org:

- **Service catalogs / Backstage:** project slugs and org slugs in catalog entries
- **Slack and notification integrations:** CircleCI webhook URLs point at the source org
- **Dashboards and status badges:** badge URLs include the project slug or org
- **Branch-protection / required status checks:** CircleCI check names include the project
- **Documentation, READMEs, bookmarks:** any link to `app.circleci.com` with the old org

Ask the user to provide a list of known external integrations before searching.

---

## Decommission the source org (optional, ask first)

**Always confirm explicitly before decommissioning.** Decommissioning options
(from least to most destructive — confirm at each level):

1. **Pause builds:** disable all project triggers in the source org so nothing
   new builds. Do NOT delete — keep it for reference.
2. **Unfollow projects (OAuth):** stops webhook delivery to the source org.
3. **Remove webhooks from VCS:** prevents any further events reaching the source org.
4. **Archive the source org:** in the CircleCI UI, org settings → archive/delete.
   This is irreversible.

Present the options to the user; do NOT proceed past step 1 without explicit
confirmation for each subsequent step.

---

## Troubleshooting

**Builds firing in source org instead of destination:** check that the VCS
webhook points at the destination org's project (for App orgs, verify the
GitHub App is installed in the destination GitHub org and the repo is connected).

**`sync --apply --yes` not enabling triggers:** confirm the mapping file points
to the destination org. If the mapping is missing, sync targets the source org.

**Context restriction missing in destination:** project-type restrictions cannot
be migrated automatically (source project UUIDs do not transfer). Recreate them
in the destination org settings UI.

**OTel exporters created but headers are blank:** the CircleCI API redacts OTel
header values. Re-add them manually in the destination org settings.

---

## See also

- [docs/cutover-runbook.md](../../docs/cutover-runbook.md) — full operator checklist + canonical "what does not transfer" list
- [docs/guide.md § Step 4 — Validate, enable, rotate](../../docs/guide.md#8-step-4--validate-enable-rotate)
- [capture-secrets/SKILL.md](../capture-secrets/SKILL.md) — if secrets rotation reveals gaps
