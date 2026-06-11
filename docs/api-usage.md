# CircleCI API usage

`circleci-migrate` is v2-first. The vast majority of operations use the
CircleCI API v2 (`/api/v2/`). A small number of capabilities are only
available in the legacy v1.1 API, a project-discovery capability uses the
private `/api/private/` endpoint, and two org-level resources require the
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
[Secrets in the README](../README.md#secrets)).

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

### v2 OpenTelemetry exporters

| Method | Endpoint | Used for |
|---|---|---|
| `GET` | `/api/v2/org/{orgID}/otel-exporter` | Read the org's OTel exporter configurations (up to 5 per org). Header values are redacted as `"xxxx"` by the server. |
| `POST` | `/api/v2/org/{orgID}/otel-exporter` | Create an OTel exporter on the destination org. Header values must be set manually after creation. |

### v2 org contacts

| Method | Endpoint | Used for |
|---|---|---|
| `GET` | `/api/v2/org/{orgID}/contacts` | Read the org's primary and security contact email lists. |
| `PUT` | `/api/v2/org/{orgID}/contacts` | Overwrite the destination org's contact lists. |

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
| `POST` | `/api/v2/context/{id}/restrictions` | Create an expression or group restriction on a context. Body: `{"restriction_type": "<type>", "restriction_value": "<value>"}`. |

**Note on restriction writes:** expression-type and group-type restriction
writes are generally available. Group restrictions are resolved by name from
the destination org's CIAM group list; the "All members" group resolves to the
destination org UUID. Project-type restrictions are flagged as manual because
source-org project UUIDs do not transfer to the destination.

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
| `GET` | `/api/v2/projects/{projectID}/pipeline-definitions` | List App-pipeline definitions for a project. Each definition includes its config source, checkout source, and file path. Paginated. |
| `GET` | `/api/v2/projects/{projectID}/pipeline-definitions/{defID}/triggers` | List the triggers attached to a pipeline definition. Returns `event_source`, `event_preset`, and `disabled` fields. Paginated. |
| `GET` | `/api/v2/org/{orgID}/project/{projectID}/oidc-custom-claims` | Read per-project OIDC audience and TTL. |

### Project discovery

| Method | Endpoint | Used for |
|---|---|---|
| `GET` | `/api/v1.1/projects` | List all projects the authenticated user follows. Used for OAuth org export; returns a flat JSON array (not paginated). |
| `GET` | `/api/private/project?organization-id={orgID}&page-size=50` | List all projects belonging to an org by UUID. Used for GitHub App org export. Covers both `gh/` and `circleci/` slugs. Paginated via `next_page_token`. Page size capped at 50 (server returns HTTP 500 for larger values). |

**Why two endpoints?** The v1.1 `GET /projects` endpoint covers OAuth orgs
but returns only followed projects, and does not work for GitHub App orgs
whose slugs use UUIDs. The private `/api/private/project` endpoint covers all
org types by UUID and is used for App org export. For OAuth orgs where the
source token's user does not follow every repository, pass an explicit
`--projects` list to `export`.

### Write (sync phase)

#### Common project operations

| Method | Endpoint | Used for |
|---|---|---|
| `POST` | `/api/v2/project/{project-slug}/envvar` | Create or replace a project environment variable. Body: `{"name": "<name>", "value": "<value>"}`. |
| `PATCH` | `/api/v2/project/{provider}/{org}/{project}/settings` | Apply a partial update to project advanced settings. Body: `{"advanced": { <non-nil fields only> }}`. |
| `POST` | `/api/v2/webhook` | Create a webhook on a project. Body includes `name`, `url`, `events`, `verify-tls`, and `scope` (project UUID). The HMAC signing secret cannot be migrated — it must be set manually. |
| `POST` | `/api/v2/project/{project-slug}/schedule` | Create a pipeline schedule on an OAuth project. Body includes `name`, `description`, `timetable`, `parameters`, and `attribution-actor`. |
| `PATCH` | `/api/v2/org/{orgID}/project/{projectID}/oidc-custom-claims` | Write per-project OIDC audience and TTL. |

#### v1.1 per-project feature flags

| Method | Endpoint | Used for |
|---|---|---|
| `GET` | `/api/v1.1/project/{slug}/settings` | Read the `feature_flags` blob for a project (e.g. `api_trigger_with_config`, `drop_all_build_requests`). |
| `PUT` | `/api/v1.1/project/{slug}/settings` | Write project feature flags. `drop_all_build_requests` is a danger flag that is skipped by default. |

#### OAuth project creation and enable-builds

| Method | Endpoint | Used for |
|---|---|---|
| `POST` | `/api/v2/organization/{provider}/{org}/project` | Create a project shell in an OAuth org. Body: `{"name": "<repo>"}`. The project is created without a webhook; no builds fire until the project is followed. |
| `POST` | `/api/v1.1/project/{vcsType}/{org}/{repo}/follow` | Follow a project. Installs a deploy key and webhook on the VCS repository. This is the "enable builds" action for OAuth projects — it may trigger an initial build. |

#### GitHub App project creation and enable-builds

| Method | Endpoint | Used for |
|---|---|---|
| `POST` | `/api/v2/organization/{orgID}/project` | Create a GitHub App project by org UUID and name. Body: `{"name": "<name>"}`. The `orgID` is the bare UUID (not a slug) for App-type orgs. Returns the new project's UUID. |
| `POST` | `/api/v2/projects/{projUUID}/pipeline-definitions` | Create a pipeline definition on an App project. Body includes `name`, `description`, `config_source` (provider, repo external_id, file_path), and `checkout_source`. |
| `POST` | `/api/v2/projects/{projUUID}/pipeline-definitions/{defUUID}/triggers` | Create a trigger on a pipeline definition. Created with `disabled: true` so no builds fire until explicitly enabled. Body includes `event_source` (provider, repo external_id) and `event_preset`. |
| `PATCH` | `/api/v2/projects/{projUUID}/triggers/{triggerUUID}` | Enable a trigger by setting `disabled: false`. This is the "enable builds" action for App projects — after this call, the trigger may fire on matching events. |
| `DELETE` | `/api/v2/project/{slug}` | Delete a project. Used for rollback and test cleanup only. |

**GitHub repository external_id:** App pipeline definitions and triggers
require a numeric GitHub repository ID (`external_id`). By default the ID
captured from the source manifest is reused directly, which is correct for
same-GitHub-org migrations. When `--github-token` is provided, the tool calls
the GitHub API to resolve the ID for the destination repository (useful when
the destination org is connected to a different GitHub organization).

---

## GitHub API

| Method | Endpoint | Used for |
|---|---|---|
| `GET` | `https://api.github.com/repos/{owner}/{repo}` | Resolve a GitHub repository's numeric ID (`id` field). Used when `--github-token` is set and a pipeline definition needs a fresh `external_id` for the destination repo. |

The request carries an `Authorization: Bearer {token}` header when a token is
supplied. A 404 response is treated as an error; the tool falls back to the
captured `external_id` if resolution fails.

---

## CircleCI GraphQL API (`orb inline`)

The `orb inline` command fetches private orb source via the CircleCI GraphQL API.
Unlike the REST endpoints above, this uses the **`graphql-unstable`** endpoint:

```
POST https://circleci.com/graphql-unstable
```

The request carries a `Circle-Token` header and a JSON body with a `query`
field. The query used to fetch orb source is:

```graphql
query OrbSource($orbVersionRef: String!) {
  orbVersion(orbVersionRef: $orbVersionRef) {
    source
  }
}
```

`orbVersionRef` is the fully qualified orb reference from the config's `orbs:`
stanza (e.g. `awesomecicd/circleci-org-migration@0.2.0`). Public orbs are
fetched without a token; private orbs require a token that belongs to a user or
machine-user with access to that orb's namespace. A `null` `source` in the
response is treated as "orb not found or not accessible."

**Why `graphql-unstable`?** Orb source is not exposed by the REST API v2. The
`graphql-unstable` endpoint is the canonical way to introspect orb content and
is used by the CircleCI CLI for the same purpose.

---

## Pipelines API (`secrets capture`)

The `secrets capture` command drives a CircleCI pipeline run from the CLI
without requiring a committed `.circleci/config.yml`. It uses the v2 Pipelines
API to submit an **inline (unversioned) config** and then polls for completion:

### Trigger a pipeline with inline config

| Method | Endpoint | Used for |
|---|---|---|
| `POST` | `/api/v2/project/{project-slug}/pipeline` | Trigger a new pipeline run. When the body includes a `config` field, the supplied YAML is used as the pipeline config rather than the repo's committed config. |

Request body:

```json
{
  "branch": "main",
  "config": "<inline YAML config string>"
}
```

The inline config is generated at runtime: it contains one extraction job per
context listed in the manifest (one context per job to guarantee variable
isolation) and a final merge job that uploads `secrets.json` as an artifact.

### Poll for pipeline status

| Method | Endpoint | Used for |
|---|---|---|
| `GET` | `/api/v2/pipeline/{pipeline-id}` | Fetch pipeline metadata including `state` (`created`, `errored`, `setup-pending`, `setup`, `pending`, `running`, `failing`, `failed`, `success`, `canceled`). |
| `GET` | `/api/v2/pipeline/{pipeline-id}/workflow` | List the workflows in a pipeline. Used to surface workflow-level errors to the user when a run fails. |

### Download the artifact

| Method | Endpoint | Used for |
|---|---|---|
| `GET` | `/api/v2/workflow/{workflow-id}/job` | List the jobs in a workflow. Used to identify the merge job by name. |
| `GET` | `/api/v2/project/{project-slug}/{job-number}/artifacts` | List artifacts for a job. Used to locate the `secrets.json` artifact produced by the merge job. |

The artifact download itself (`url` from the artifacts list) is a direct GET
with the `Circle-Token` header; no separate API client is required.

**Error handling:** if the pipeline run does not reach `success` within
`--poll-timeout` (default 10 minutes), `secrets capture` exits with a non-zero
status and prints the workflow and job state. The partially run pipeline is left
in place for inspection; it is not cancelled automatically.

---

## Runner (resource classes)

The runner v3 API base (`/api/v3/runner/...`) is used for self-hosted runner
resource-class migration (since v0.3.0).

---

## Pagination

All paginated v2 endpoints follow the same pattern: the response includes a
`next_page_token` field. When non-empty, the next request includes
`?page-token=<token>`. `circleci-migrate` fetches all pages automatically.

The private `/api/private/project` endpoint uses the same `next_page_token`
pattern but requires a `page-size` query parameter. The page size is capped at
50 to avoid server errors.

---

## Error handling

The HTTP client (`api/rest`) returns an `HTTPError` for any `4xx` or `5xx`
response. The tool extracts the `message` field from JSON error bodies when
present. Per-resource errors during export are recorded as warnings in the
manifest rather than aborting the entire export. During sync, per-context and
per-project errors are recorded in the report under `error` status and the
remaining resources continue to be processed.
