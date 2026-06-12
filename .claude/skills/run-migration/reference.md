# Run Migration — Reference Notes

Design context for the `run-migration` skill. This file is for skill authors
and contributors; it explains the decisions embedded in the skill.

---

## Org types

CircleCI has three integration types that affect how the CLI works:

| Slug format | Org type | Notes |
|---|---|---|
| `gh/<org-name>` | GitHub OAuth | Legacy integration. v1.1 + v2 API. Projects are *followed* (webhook + deploy key). OAuth-only build flags apply (`oss`, `build_fork_prs`, etc.). |
| `circleci/<uuid>` | GitHub App | v2 API only. Projects use pipeline definitions + triggers (created **disabled**). Repos identified by numeric GitHub `external_id`. |
| `circleci/<uuid>` | CircleCI standalone | Same slug format as GitHub App. Supports CIAM roles and groups — synced unless `--skip-ciam` is passed. |
| `circleci/<uuid>` | GitLab | Same slug format as GitHub App. |

Find the slug in the org's CircleCI URL (`app.circleci.com`). For OAuth orgs
the URL contains `gh/<org-name>`. For App/standalone/GitLab orgs the UUID
appears in the org settings URL.

**Mixed orgs:** a GitHub org that has BOTH the OAuth and the GitHub App
integration active has TWO separate CircleCI org records — one `gh/<org>` and
one `circleci/<uuid>`. Migrate each leg separately (run the full
export→capture→sync flow twice).

---

## Slug remapping

`sync` defaults to the source org. To target a different destination org, supply
a mapping file with `org.to`:

```json
{
  "org": { "from": "gh/acme", "to": "gh/acme-new" },
  "projects": {
    "gh/acme/web": "gh/acme-new/web"
  }
}
```

Only `org.to` is required to retarget. The `projects` map is optional and only
needed if individual project names change. Without a mapping file, `sync` targets
the **source** org (a prominent warning is printed) — which is safe for
reviewing/testing but wrong for production cutover.

For cross-type migrations (OAuth → App), project slugs change format:
```json
{
  "org": { "from": "gh/acme", "to": "circleci/22222222-2222-2222-2222-222222222222" },
  "projects": {
    "gh/acme/web": "circleci/22222222-2222-2222-2222-222222222222/web"
  }
}
```

---

## Why dry-run first

`sync` is **dry-run by default** (no `--apply`). This is intentional:

1. The dry run shows the full plan before any writes: every `created`, `set`,
   and `manual` action is listed. The user can abort without consequences.
2. Missing secrets, wrong mappings, and permission gaps surface in the dry run
   before they cause partially-applied state.
3. Idempotency means re-running `sync --apply` after fixing a problem is safe —
   it reuses existing contexts and projects by name rather than duplicating them.

The skill enforces dry-run first by always running `sync` without `--apply`
and asking for explicit confirmation before applying.

---

## GitHub repo move (EMU / cross-GitHub-org)

When repos move to a different GitHub org, CircleCI's GitHub App identifies each
repo by its numeric GitHub `external_id`, which changes on move. Supply:

- `--github-token` (falls back to `$GITHUB_TOKEN`) — GitHub PAT with repo read
- `--dest-github-org acme-new` — the destination GitHub org name

The CLI resolves the new IDs automatically. Repos not found in the destination
GitHub org are flagged as `manual` and skipped. This flag is NOT needed for
same-GitHub-org migrations.

---

## App project trigger state

GitHub App projects are created with their triggers **disabled**
(`disabled: true`). This means no builds fire until the user explicitly enables
them (via the `--yes` flag or by answering Y at the prompt). This is by design —
it lets the operator validate the destination before going live.

For OAuth orgs, projects are created unfollowed (no webhook) and only start
building when followed (via `--yes` or the prompt).

---

## Fail-safe defaults

- Sync without `--apply`: dry run only. No writes.
- Builds paused at creation: no unintended CI runs on the destination.
- Secrets bundle optional: if absent, env vars are created with empty values.
- `--missing-secrets skip` (default): variables with no captured value are
  omitted, not written as empty strings.
- `--missing-secrets placeholder`: creates the variable name with a placeholder
  so it exists for manual fill-in.

---

## See also

- [docs/guide.md](../../docs/guide.md) — full walkthrough with all scenarios
- [docs/mapping.md](../../docs/mapping.md) — mapping file schema reference
- [docs/architecture.md](../../docs/architecture.md) — how export/sync work internally
