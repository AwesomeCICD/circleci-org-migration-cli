# Architecture and data flow

This document describes how `circleci-migrate` is structured and how data
moves through the system during each phase of a migration.

---

## Component diagram

```mermaid
graph TD
    subgraph cmd ["cmd/ (Cobra commands)"]
        ROOT[root]
        EXPORT[export]
        SECRETS_CMD["secrets extract / merge"]
        SYNC[sync]
        MIGRATE["migrate (future)"]
    end

    subgraph internal ["internal/"]
        EXPORTER["exporter<br/>Orchestrates API reads<br/>into a Manifest"]
        SYNCER["syncer<br/>Applies a Manifest + SecretBundle<br/>to the destination org"]
        SECRETS_PKG["secrets<br/>Reads live env vars<br/>into a SecretBundle"]
        REPORT["report<br/>Formats terminal summary<br/>and audit Markdown"]
        MANIFEST["manifest<br/>Shared on-disk contract:<br/>Manifest · SecretBundle · Mapping"]
    end

    subgraph api ["api/ (thin HTTP clients)"]
        ORG_API["org<br/>v2: GetOrg, ResolveOrgID<br/>v1.1: GetOrgSettings"]
        CTX_API["context<br/>v2: List, Create, UpsertEnvVar<br/>ListRestrictions, CreateRestriction"]
        PROJ_API["project<br/>v2: GetProject, GetSettings<br/>ListEnvVars, ListCheckoutKeys<br/>ListWebhooks, ListSchedules<br/>v1.1: ListFollowedProjects, FollowProject"]
        REST["rest<br/>Shared HTTP client<br/>Circle-Token header<br/>JSON encode / decode"]
    end

    CIRCLE_API["CircleCI API<br/>circleci.com/api/v2<br/>circleci.com/api/v1.1<br/>app.circleci.com/private/ciam"]

    ROOT --> EXPORT
    ROOT --> SECRETS_CMD
    ROOT --> SYNC
    ROOT --> MIGRATE

    EXPORT --> EXPORTER
    EXPORT --> REPORT
    SECRETS_CMD --> SECRETS_PKG
    SYNC --> SYNCER

    EXPORTER --> ORG_API
    EXPORTER --> CTX_API
    EXPORTER --> PROJ_API
    EXPORTER --> MANIFEST

    SYNCER --> ORG_API
    SYNCER --> CTX_API
    SYNCER --> MANIFEST

    SECRETS_PKG --> MANIFEST

    REPORT --> MANIFEST

    ORG_API --> REST
    CTX_API --> REST
    PROJ_API --> REST

    REST --> CIRCLE_API
```

`internal/manifest` is the shared data contract. Every command reads or
writes `Manifest`, `SecretBundle`, or `Mapping` structs from this package.
The API clients never depend on each other; they communicate only through
the manifest types.

---

## Phase 1 — Export flow

```mermaid
sequenceDiagram
    participant User
    participant export as "cmd/export"
    participant ex as "internal/exporter"
    participant orgAPI as "api/org"
    participant ctxAPI as "api/context"
    participant projAPI as "api/project"
    participant CCI as "CircleCI API"

    User->>export: circleci-migrate export --org gh/acme

    export->>ex: Export(Options)

    ex->>orgAPI: GetOrganization("gh/acme")
    orgAPI->>CCI: GET /api/v2/organization/gh%2Facme
    CCI-->>orgAPI: id, name, slug, vcs_type
    orgAPI-->>ex: Organization

    ex->>orgAPI: GetFeatureFlags("github", "acme")
    orgAPI->>CCI: GET /api/v1.1/organization/github/acme/settings
    CCI-->>orgAPI: feature_flags map
    orgAPI-->>ex: OrgSettings

    ex->>orgAPI: GetOIDCClaims(orgID)
    orgAPI->>CCI: GET /api/v2/org/orgID/oidc-custom-claims
    CCI-->>orgAPI: audience, ttl
    orgAPI-->>ex: OIDCClaims

    ex->>orgAPI: GetURLOrbAllowList(orgSlug)
    orgAPI->>CCI: GET /api/v2/organization/orgSlug/url-orb-allow-list
    CCI-->>orgAPI: items
    orgAPI-->>ex: URLOrbAllowList

    ex->>orgAPI: GetPolicyBundle(ownerID)
    orgAPI->>CCI: GET /api/v2/owner/ownerID/context/config/policy-bundle
    CCI-->>orgAPI: policy name to Rego map
    orgAPI-->>ex: ConfigPolicies

    ex->>orgAPI: GetAuditLogConfigs(orgID)
    orgAPI->>CCI: GET /api/v2/organizations/orgID/audit-log/configs
    CCI-->>orgAPI: audit-log config items
    orgAPI-->>ex: AuditLogConfigs

    ex->>orgAPI: GetSSOEnforced(orgID)
    orgAPI->>CCI: GET app.circleci.com/private/ciam/orgs/orgID/sso/enforced
    CCI-->>orgAPI: enforced bool
    orgAPI-->>ex: SSOEnforced

    loop For each context page
        ex->>ctxAPI: ListContexts(ownerID)
        ctxAPI->>CCI: GET /api/v2/context?owner-id=id
        CCI-->>ctxAPI: items, next_page_token
    end

    loop For each context
        ex->>ctxAPI: ListEnvVars(contextID)
        ctxAPI->>CCI: GET /api/v2/context/id/environment-variable
        CCI-->>ctxAPI: items with names only (values masked)

        ex->>ctxAPI: ListRestrictions(contextID)
        ctxAPI->>CCI: GET /api/v2/context/id/restrictions
        CCI-->>ctxAPI: restriction_type, restriction_value, name
    end

    ex->>projAPI: FollowedProjectsForOrg("acme")
    projAPI->>CCI: GET /api/v1.1/projects
    CCI-->>projAPI: flat array of project objects

    loop For each project slug
        ex->>projAPI: GetProject(slug)
        projAPI->>CCI: GET /api/v2/project/slug
        CCI-->>projAPI: id, name, slug, vcs_info

        ex->>projAPI: GetSettings(provider, org, proj)
        projAPI->>CCI: GET /api/v2/project/provider/org/proj/settings
        CCI-->>projAPI: advanced settings object

        ex->>projAPI: ListEnvVars(slug)
        projAPI->>CCI: GET /api/v2/project/slug/envvar
        CCI-->>projAPI: names with masked values

        opt --skip-extras not set
            ex->>projAPI: ListCheckoutKeys, ListWebhooks, ListSchedules
        end
    end

    ex-->>export: Manifest (no secret values)
    export->>export: manifest.Save("manifest.json")
    export->>export: report.SaveMarkdown(m, "migration-report.md")
    export-->>User: terminal summary + file paths
```

Key properties of the export phase:

- **Read-only.** No writes to CircleCI. Safe to run multiple times.
- **No secret values.** The API masks all values; the manifest records only
  names and metadata.
- **Best-effort per resource.** An error on one context or project produces a
  warning in the manifest and audit report, not a fatal failure.
- **Stable output.** Contexts, projects, and their variable lists are sorted
  by name so repeated exports of unchanged data produce identical files.

---

## Phase 2 — In-pipeline secret extraction

```mermaid
flowchart TD
    START(["User commits manifest.json to source-org repo"])

    START --> TRIGGER["Push triggers CircleCI pipeline in source org"]

    TRIGGER --> PARALLEL

    subgraph PARALLEL ["Parallel extract jobs — one per context"]
        J1["Job: extract-deploy-prod<br/>context: deploy-prod<br/><br/>1. checkout<br/>2. orb: install circleci-migrate<br/>3. secrets extract --context deploy-prod<br/>4. persist bundle to workspace"]

        J2["Job: extract-shared<br/>context: shared<br/><br/>1. checkout<br/>2. orb: install circleci-migrate<br/>3. secrets extract --context shared<br/>4. persist bundle to workspace"]

        JN["... one job per context ..."]
    end

    PARALLEL --> MERGE

    MERGE["Job: merge<br/>(requires all extract jobs)<br/><br/>1. attach_workspace<br/>2. circleci-migrate secrets merge -o secrets.json<br/>3. store_artifacts: secrets.json"]

    MERGE --> DOWNLOAD["User downloads secrets.json artifact from CircleCI UI"]

    DOWNLOAD --> NOTE["secrets.json contains plaintext values<br/>Protect it and do not commit it"]
```

**Why one job per context?**

Each job can reference only the contexts listed under its `context:` key in
the workflow. If two contexts define a variable with the same name, combining
them in one job would cause one value to overwrite the other. Running one job
per context guarantees isolation.

The `secrets extract` command reads variable names from `manifest.json`, looks
each up in `os.LookupEnv`, and records found values in a `SecretBundle` JSON
file. Variables not present in the environment are listed under "Not found" in
the output (and cause a non-zero exit if `--strict` is passed).

After all extract jobs complete, the `merge` job combines the per-context
bundles into a single `secrets.json` using `secrets merge`.

---

## Phase 3 — Sync flow

```mermaid
flowchart TD
    START(["User runs: circleci-migrate sync --manifest manifest.json --secrets secrets.json"])

    START --> LOAD["Load manifest.json<br/>Load secrets.json (optional)<br/>Load mapping.json (optional)"]

    LOAD --> RESOLVE["Resolve destination org slug to UUID<br/>GET /api/v2/organization/slug"]

    RESOLVE --> ORG_SETTINGS["Sync org settings<br/>(feature flags, OIDC, URL-orb allow list, config policies)<br/>Skipped with --skip-org-settings"]

    ORG_SETTINGS --> LIST_CTX["List existing contexts in destination org<br/>GET /api/v2/context?owner-id=dest-id"]

    LIST_CTX --> LOOP

    subgraph LOOP ["For each context in manifest"]
        ENSURE{"Context exists<br/>in destination?"}

        ENSURE -->|Yes| REUSE["Status: exists<br/>Reuse existing context ID"]
        ENSURE -->|"No, dry run"| WOULD_CREATE["Status: created (would create)<br/>No API call"]
        ENSURE -->|"No, --apply"| CREATE["POST /api/v2/context<br/>Status: created"]

        REUSE --> VARS
        WOULD_CREATE --> VARS
        CREATE --> VARS

        subgraph VARS ["For each env var in context"]
            HAS_VAL{"Value in<br/>secret bundle?"}
            HAS_VAL -->|"Yes, --apply"| UPSERT["PUT /api/v2/context/id/environment-variable/name<br/>Status: set"]
            HAS_VAL -->|"Yes, dry run"| DRY_SET["Status: set (would set)<br/>No API call"]
            HAS_VAL -->|"No, skip policy"| SKIP["Status: manual<br/>Skipped — set manually"]
            HAS_VAL -->|"No, placeholder policy"| PLACEHOLDER["PUT with REPLACE_ME value<br/>Status: set (placeholder)"]
        end

        VARS --> RESTRS

        subgraph RESTRS ["For each restriction in context"]
            RTYPE{"Restriction<br/>type?"}
            RTYPE -->|expression| EXPR_CHECK{"Already exists<br/>in destination?"}
            EXPR_CHECK -->|Yes| EXISTS["Status: exists"]
            EXPR_CHECK -->|"No, --apply"| CREATE_RESTR["POST /api/v2/context/id/restrictions<br/>Status: set"]
            EXPR_CHECK -->|"No, dry run"| WOULD_RESTR["Status: set (would add)"]
            RTYPE -->|project| MANUAL_P["Status: manual<br/>Source-org project UUID does not transfer"]
            RTYPE -->|group| MANUAL_G["Status: manual<br/>Group-restriction writes not yet GA"]
        end
    end

    LOOP --> REPORT["Print sync report:<br/>DRY RUN or APPLIED<br/>created / exists / set / manual / error counts<br/>Needs-attention list"]

    REPORT --> END([Done])
```

Key properties of the sync phase:

- **Dry run by default.** No writes to CircleCI unless `--apply` is passed.
  Reviewing the dry-run output before applying is strongly recommended.
- **Idempotent.** Existing contexts are reused by name. Re-running sync with
  `--apply` is safe — it will not duplicate contexts or overwrite restrictions
  that already exist.
- **Transparent report.** Every action (created, exists, set, manual, error)
  is recorded and printed. Items requiring manual follow-up are surfaced
  explicitly.
