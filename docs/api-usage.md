# CircleCI API usage

`circleci-migrate` is v2-first. The vast majority of operations use the
CircleCI API v2 (`/api/v2/`). A small number of capabilities are only
available in the legacy v1.1 API, and two org-level resources require the
private CIAM endpoints served by `app.circleci.com`; those are called out
explicitly below.

All requests carry a `Circle-Token` header and a `User-Agent` header in the
form `circleci-migrate/<version>`. The host is configurable via `--host` or
`CIRCLECI_HOST` (default: `https://circleci.com`). The private CIAM endpoints
always target `app.circleci.com` (or the equivalent for Server installs) and
use the same token.

Secret values are **never returned by any API version**. The CircleCI API
masks every environment-variable value everywhere it appears. Capturing real
values requires running inside a CircleCI job (see
[Phase 2 in the README](../README.md#phase-2-capture-secrets-inside-a-pipeline)).

---

## Organization

### v2 endpoints

| Method | Endpoint | Used for |
|---|---|---|
| `GET` | `/api/v2/organization/{org-slug-or-id}` | Resolve org slug to name and UUID. The slug is percent-encoded (`gh%2Facme`) because it contains a `/`. |
| `GET` | `/api/v2/me/collaborations` | List all orgs the authenticated user collaborates with (defined; not called in current export/sync paths). |

### v1.1 org settings (feature flags)

| Method | Endpoint | Used for |
|---|---|---|
| `GET` | `/api/v1.1/organization/{vcsType}/{orgName}/settings` | Read the full org-level `feature_flags` map, including `require_context_group_restriction` and others. |
| `PUT` | `/api/v1.1/organization/{vcsType}/{orgName}/settings` | Write feature flags to the destination org. Keys are converted from `snake_case` to `kebab-case` for the write path. |

**Why v1.1?** The `feature_flags` object is not exposed by the v2 API. This
call is best-effort: if it fails (for example because the org is a GitHub App
org with no v1.1 slug), a warning is recorded in the manifest and export
continues.

### v2 OIDC custom claims

| Method | Endpoint | Used for |
|---|---|---|
| `GET` | `/api/v2/org/{orgID}/oidc-custom-claims` | Read the org's OIDC audience list and token TTL. |
| `PATCH` | `/api/v2/org/{orgID}/oidc-custom-claims` | Write audience and TTL to the destination org. |

### v2 URL-orb allow list

| Method | Endpoint | Used for |
|---|---|---|
| `GET` | `/api/v2/organization/{slug-or-id}/url-orb-allow-list` | Read the org's URL-orb allow list. Available on GitHub App (`circleci/`) orgs. |
| `POST` | `/api/v2/organization/{slug-or-id}/url-orb-allow-list` | Add an entry to the destination org's URL-orb allow list. |

### v2 config policies (Scale plan)

| Method | Endpoint | Used for |
|---|---|---|
| `GET` | `/api/v2/owner/{ownerID}/context/config/policy-bundle` | Read all Rego policies in the org's config policy bundle. Returns an empty map if the org has no bundle or is not on Scale. |
| `POST` | `/api/v2/owner/{ownerID}/context/config/policy-bundle` | Replace the destination org's policy bundle. Body: `{"policies": {name: rego}}`. |
| `GET` | `/api/v2/owner/{ownerID}/context/config/decision/settings` | Read whether config-policy enforcement is enabled. |
| `PATCH` | `/api/v2/owner/{ownerID}/context/config/decision/settings` | Enable or disable config-policy enforcement on the destination. |

### v2 audit-log streaming configs (capture only)

| Method | Endpoint | Used for |
|---|---|---|
| `GET` | `/api/v2/organizations/{orgID}/audit-log/configs` | Read the org's audit-log streaming configurations (S3 ARN, region, bucket, etc.). |

**Why capture-only?** Audit-log configs contain AWS ARN, region, and bucket
values that are specific to the source org's AWS account. `sync` surfaces them
as manual actions in the report; they are never automatically written to the
destination. Recreate them in the destination org's settings UI.

### Private CIAM endpoints (groups and SSO)

These endpoints are served by `app.circleci.com` (not `circleci.com`). The
`api/org` client maintains a separate HTTP client (`app`) that targets this
host. The `Circle-Token` header is used identically.

| Method | Endpoint | Used for |
|---|---|---|
| `GET` | `https://app.circleci.com/private/ciam/orgs/{orgID}/groups` | List the org's CIAM groups (named sets of members used for context group restrictions). |
| `GET` | `https://app.circleci.com/private/ciam/orgs/{orgID}/sso/enforced` | Read whether SSO login is enforced for the org. |
| `GET` | `https://app.circleci.com/private/ciam/orgs/{orgID}/sso/connection` | Read the org's SAML SSO connection body (realm, IdP fields). Returns HTTP 404 when no SSO is configured — treated as "not an error". |

**Why SSO is capture-only:** Recreating SSO on a destination org requires DNS
TXT domain verification and IdP-side SAML app / iframe-origin setup. This
cannot be automated via API. `sync` surfaces SSO state as a manual action in
the report and never writes it. See your IdP and CircleCI SSO documentation
for recreation steps.

---

## Contexts

### Read (export phase)

| Method | Endpoint | Used for |
|---|---|---|
| `GET` | `/api/v2/context?owner-id={id}` | List all contexts for an org. Paginated (`page-token`). |
| `GET` | `/api/v2/context/{id}/environment-variable` | List env-var **names** for a context. Values are always masked. Paginated. |
| `GET` | `/api/v2/context/{id}/restrictions` | List project, expression, and group restrictions. Returns `restriction_type`, `restriction_value`, and `name`. Paginated. |

### Write (sync phase)

| Method | Endpoint | Used for |
|---|---|---|
| `POST` | `/api/v2/context` | Create a context in the destination org. Body: `{"name": "<name>", "owner": {"id": "<org-id>", "type": "organization"}}`. |
| `PUT` | `/api/v2/context/{id}/environment-variable/{name}` | Create or update an environment variable in a context. Body: `{"value": "<value>"}`. Idempotent upsert. |
| `POST` | `/api/v2/context/{id}/restrictions` | Create an expression restriction on a context. Body: `{"restriction_type": "<type>", "restriction_value": "<value>"}`. |

**Note on restriction writes:** expression-type restriction writes are
generally available. Project-type restriction writes are attempted but the
source-org project UUID does not transfer to the destination, so these are
flagged as manual. Group-type restriction writes (`restriction_type: "group"`)
are **not yet GA** — `sync` marks them as `manual` and never attempts to
write them.

---

## Projects

### Read (export phase)

| Method | Endpoint | Used for |
|---|---|---|
| `GET` | `/api/v2/project/{project-slug}` | Fetch project metadata (name, ID, VCS info). Slug components are individually percent-encoded. |
| `GET` | `/api/v2/project/{provider}/{org}/{project}/settings` | Read advanced project settings (`autocancel_builds`, `build_fork_prs`, etc.). Response is wrapped in an `"advanced"` key. |
| `GET` | `/api/v2/project/{project-slug}/envvar` | List env-var names. The API returns a masked value (`"xxxx1234"`) for each variable; stored as `masked_value` in the manifest. Paginated. |
| `GET` | `/api/v2/project/{project-slug}/checkout-key` | List checkout/deploy key metadata (type, fingerprint, public key). Private key material is never returned. Paginated. |
| `GET` | `/api/v2/webhook?scope-id={project-id}&scope-type=project` | List outbound webhooks scoped to a project. Requires the project UUID, not the slug. Paginated. |
| `GET` | `/api/v2/project/{project-slug}/schedule` | List pipeline schedules. Paginated. |

### Non-v2 calls: project discovery

| Method | Endpoint | Used for |
|---|---|---|
| `GET` | `/api/v1.1/projects` | List all projects the authenticated user follows. Used during export to discover projects in the source org. The response is a flat JSON array (not paginated). |
| `POST` | `/api/v1.1/project/{vcsType}/{org}/{repo}/follow` | Follow a project. Write operation — installs a deploy key and webhook on the VCS repository. Reserved for a future milestone; not called by current sync. |

**Why v1.1 for project discovery?** The v2 API does not provide a
"list all projects in an org" endpoint that works without already knowing
each project's slug or UUID. The v1.1 `GET /projects` returns all projects
the authenticated user follows, filtered to the target org name. For orgs
where the source token's user does not follow every repository, pass an
explicit `--projects` list to `export`.

### Write (sync phase)

| Method | Endpoint | Used for |
|---|---|---|
| `POST` | `/api/v2/project/{project-slug}/envvar` | Create or replace a project environment variable. Body: `{"name": "<name>", "value": "<value>"}`. |
| `PATCH` | `/api/v2/project/{provider}/{org}/{project}/settings` | Apply a partial update to project advanced settings. Body: `{"advanced": { <non-nil fields only> }}`. |

---

## Runner (resource classes)

The runner v3 API base (`/api/v3/runner/...`) is reserved for a future
milestone covering self-hosted runner resource-class migration. It is not
called in the current release.

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
