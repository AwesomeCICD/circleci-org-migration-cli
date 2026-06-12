# Migration playbooks

Step-by-step runbooks for operators performing a real CircleCI organization
migration. Every phase ends with a task-list **checklist** and an explicit
**validation gate** before the next phase begins.

For the conceptual overview, token setup, and per-command flag tables, start
with the [migration guide](../guide.md). The playbooks here are operator-facing
runbooks that walk through every phase in order for a specific account/org-type
combination.

---

## Which playbook do I need?

### Step 1 — Identify your source and destination org types

Find your org slug in the CircleCI web UI: **Organization Settings → Overview**.
The slug appears in the URL and at the top of the page.

| Slug prefix | Org type | Notes |
|---|---|---|
| `gh/<name>` | **GitHub OAuth** | Legacy GitHub integration; "followed" projects |
| `circleci/<uuid>` | **GitHub App / standalone** | GitHub App or standalone; pipeline definitions + triggers |

For a GitHub App org, the UUID is a `xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx` value.
For a GitLab or standalone org the prefix is also `circleci/<uuid>`.

### Step 2 — Determine your migration scenario

```
Source org type ──► Destination org type
     │
     ├── gh/  ──► gh/   →  [SAME-TYPE]  oauth-to-oauth.md
     │
     ├── circleci/ ──► circleci/  →  [SAME-TYPE]  standalone-to-standalone.md
     │
     └── gh/  ──► circleci/  →  [CROSS-TYPE, LOSSY]  cross-type-oauth-to-app.md
```

### Step 3 — CLI-only vs Terraform-managed

Every playbook covers **both** management models inside Phase 3:

- **(A) CLI applies everything** — the default path; no Terraform required.
- **(B) Terraform-managed** — `terraform generate` + `terraform apply` + CLI
  gap-fill. Use this when the destination org should land in Terraform state for
  ongoing IaC management.

---

## Playbook index

| Playbook | When to use |
|---|---|
| [oauth-to-oauth.md](oauth-to-oauth.md) | GitHub OAuth (`gh/`) → GitHub OAuth. Most common scenario. Projects onboarded by following; group context restrictions supported. No pipeline-definition/trigger objects. |
| [standalone-to-standalone.md](standalone-to-standalone.md) | GitHub App / standalone (`circleci/`) → standalone. Full Terraform surface (circleci_pipeline, circleci_trigger). CIAM org roles. Self-hosted runner support. |
| [cross-type-oauth-to-app.md](cross-type-oauth-to-app.md) | GitHub OAuth → GitHub App (lossy). Covers what is dropped (fork-PR builds, OSS flag, pr_only_branch_overrides), the GitHub-App repo-connection prerequisite, and the required mapping file. |

---

## Common across all playbooks

- **Rollback is safe at any point before Phase 7.** Nothing in the source org is
  modified or destroyed. Projects in the destination are created paused and will
  not build until you explicitly enable them.
- **Token resolution:** pass `--source-token`/`--dest-token` on the CLI or set
  `CIRCLECI_SOURCE_TOKEN`/`CIRCLECI_DEST_TOKEN` in your environment. A single
  `--token` / `CIRCLECI_CLI_TOKEN` acts as a fallback for both sides.
- **Dry run by default:** `sync` and `secrets transfer` never write anything
  without `--apply`. Re-running the dry run after changes is always safe.
- **What does NOT transfer:** the canonical reference is the
  [cutover runbook — Section 4](../cutover-runbook.md#4-does-not-transfer--data-loss).
  Each playbook links there rather than duplicating the list.

---

## See also

- [Migration guide](../guide.md) — end-to-end conceptual walkthrough, org types,
  token setup.
- [mapping.json reference](../mapping.md) — when you need a mapping file and
  what each key does.
- [Cutover runbook](../cutover-runbook.md) — operator checklist and the full
  what-does-NOT-transfer reference.
- [Troubleshooting](../troubleshooting.md) — common errors and fixes.
- [CLI reference](../cli/README.md) — complete per-command flag tables.
