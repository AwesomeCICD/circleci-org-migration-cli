# CircleCI API usage

`circleci-migrate` is v2-first. The vast majority of operations use the
CircleCI API v2 (`/api/v2/`). A small number of capabilities are only
available in the legacy v1.1 API; those are called out explicitly below.

All requests carry a `Circle-Token` header and a `User-Agent` header in the
form `circleci-migrate/<version>`. The host is configurable via `--host` or
`CIRCLECI_HOST` (default: `https://circleci.com`).

Secret values are **never returned by any API version**. The CircleCI API
masks every environment-variable value everywhere it appears. Capturing real
values requires running inside a CircleCI job (see
[Phase 2 in the README](../README.md#phase-2-in-pipeline-secret-extraction)).

---

## Organization

| Method | Endpoint | API | Used for |
|---|---|---|---|
| `GET` | `/api/v2/organization/{org-slug-or-id}` | v2 | Resolve org slug to name and UUID. The slug is percent-encoded (`gh%2Facme`) because it contains a `/`. |
| `GET` | `/api/v2/me/collaborations` | v2 | List all orgs the authenticated user collaborates with (defined but not called in current export/sync paths). |

### Non-v2 call: org settings

| Method | Endpoint | API | Used for |
|---|---|---|---|
| `GET` | `/api/v1.1/organization/{vcsType}/{orgName}/settings` | **v1.1** | Read the org-level `require_context_group_restriction` feature flag. |

**Why v1.1?** This feature flag is not exposed by the v2 API. The v1.1
endpoint returns a `feature_flags` object that includes
`require_context_group_restriction`. This call is best-effort: if it fails
(for example, because the org is a GitHub App org with no v1.1 slug), a
warning is recorded in the manifest and export continues.

---

## Contexts

### Read (export phase)

| Method | Endpoint | API | Used for |
|---|---|---|---|
| `GET` | `/api/v2/context?owner-id={id}` | v2 | List all contexts for an org. Paginated (`page-token`). |
| `GET` | `/api/v2/context/{id}/environment-variable` | v2 | List env-var **names** for a context. Values are always masked. Paginated. |
| `GET` | `/api/v2/context/{id}/restrictions` | v2 | List project, expression, and group restrictions. Returns `restriction_type`, `restriction_value`, and `name`. Paginated. |

### Write (sync phase)

| Method | Endpoint | API | Used for |
|---|---|---|---|
| `POST` | `/api/v2/context` | v2 | Create a context in the destination org. Body: `{"name": "<name>", "owner": {"id": "<org-id>", "type": "organization"}}`. |
| `PUT` | `/api/v2/context/{id}/environment-variable/{name}` | v2 | Create or update an environment variable in a context. Body: `{"value": "<value>"}`. Idempotent upsert. |
| `POST` | `/api/v2/context/{id}/restrictions` | v2 | Create a restriction on a context. Body: `{"restriction_type": "<type>", "restriction_value": "<value>"}`. |

**Note on restriction writes:** project-type and expression-type restriction
writes are generally available as of March 2025. Group-type restriction writes
(`restriction_type: "group"`) are **not yet GA** — calls with this type may
return a 4xx error. `circleci-migrate sync` therefore marks group restrictions
as `manual` and does not attempt to write them.

---

## Projects

### Read (export phase)

| Method | Endpoint | API | Used for |
|---|---|---|---|
| `GET` | `/api/v2/project/{project-slug}` | v2 | Fetch project metadata (name, ID, VCS info). The slug components are individually percent-encoded. |
| `GET` | `/api/v2/project/{provider}/{org}/{project}/settings` | v2 | Read advanced project settings (`autocancel_builds`, `build_fork_prs`, etc.). Response is wrapped in an `"advanced"` key. |
| `GET` | `/api/v2/project/{project-slug}/envvar` | v2 | List env-var names. The API returns a masked value (`"xxxx1234"`) for each variable; `circleci-migrate` stores this as `masked_value` in the manifest. Paginated. |
| `GET` | `/api/v2/project/{project-slug}/checkout-key` | v2 | List checkout/deploy key metadata (type, fingerprint, public key). Private key material is never returned. Paginated. |
| `GET` | `/api/v2/webhook?scope-id={project-id}&scope-type=project` | v2 | List outbound webhooks scoped to a project. Requires the project UUID, not the slug. Paginated. |
| `GET` | `/api/v2/project/{project-slug}/schedule` | v2 | List pipeline schedules. Paginated. |

### Non-v2 calls: project discovery and follow

| Method | Endpoint | API | Used for |
|---|---|---|---|
| `GET` | `/api/v1.1/projects` | **v1.1** | List all projects the authenticated user follows. Used during export to discover projects in the source org. The response is a flat JSON array (not paginated). |
| `POST` | `/api/v1.1/project/{vcsType}/{org}/{repo}/follow` | **v1.1** | Follow a project. **Write operation** — installs a deploy key and webhook on the VCS repository. Not called by the current sync implementation; reserved for a future milestone. |

**Why v1.1 for project discovery?** The v2 API does not provide a
"list all projects in an org" endpoint that works without already knowing
each project's slug or UUID. The v1.1 `GET /projects` returns all projects
the authenticated user follows, filtered to the target org name, which is
sufficient for most migrations. For orgs where the source token's user does
not follow every repository, pass an explicit `--projects` list to `export`.

**Why v1.1 for project follow?** The v2 API has no equivalent endpoint. The
follow operation requires caution — it triggers webhook installation and may
start a build — so it is gated behind an explicit opt-in in the command layer.

### Write (sync phase — future milestone)

| Method | Endpoint | API | Used for |
|---|---|---|---|
| `POST` | `/api/v2/project/{project-slug}/envvar` | v2 | Create or replace a project environment variable. Body: `{"name": "<name>", "value": "<value>"}`. |
| `PATCH` | `/api/v2/project/{provider}/{org}/{project}/settings` | v2 | Apply a partial update to project advanced settings. Body: `{"advanced": { <non-nil fields only> }}`. |

---

## Pagination

All paginated v2 endpoints follow the same pattern: the response includes a
`next_page_token` field. When non-empty, the next request includes
`?page-token=<token>`. `circleci-migrate` fetches all pages automatically.

---

## Error handling

The HTTP client (`api/rest`) returns an `HTTPError` for any `4xx` or `5xx`
response. The tool extracts the `message` field from JSON error bodies when
present. Per-resource errors during export are recorded as warnings in the
manifest rather than aborting the entire export. During sync, per-context
errors are recorded in the report under `error` status and the remaining
contexts continue to be processed.
