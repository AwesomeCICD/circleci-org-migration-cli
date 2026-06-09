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
        SECRETS_CMD[secrets extract / merge]
        SYNC[sync]
        MIGRATE[migrate *future*]
    end

    subgraph internal ["internal/"]
        EXPORTER[exporter\nOrchestrates API reads\ninto a Manifest]
        SYNCER[syncer\nApplies a Manifest + SecretBundle\nto the destination org]
        SECRETS_PKG[secrets\nReads live env vars\ninto a SecretBundle]
        REPORT[report\nFormats terminal summary\nand audit Markdown]
        MANIFEST[manifest\nShared on-disk contract:\nManifest · SecretBundle · Mapping]
    end

    subgraph api ["api/ (thin HTTP clients)"]
        ORG_API[org\nv2: GetOrg, ResolveOrgID\nv1.1: GetOrgSettings]
        CTX_API[context\nv2: List, Create, UpsertEnvVar\nListRestrictions, CreateRestriction]
        PROJ_API[project\nv2: GetProject, GetSettings,\nListEnvVars, ListCheckoutKeys,\nListWebhooks, ListSchedules\nv1.1: ListFollowedProjects, FollowProject]
        REST[rest\nShared HTTP client\nCircle-Token header\nJSON encode / decode]
    end

    CIRCLE_API["CircleCI API\ncircleci.com/api/v2\ncircleci.com/api/v1.1"]

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
    participant export as cmd/export
    participant ex as internal/exporter
    participant orgAPI as api/org
    participant ctxAPI as api/context
    participant projAPI as api/project
    participant CCI as CircleCI API

    User->>export: circleci-migrate export --org gh/acme

    export->>ex: Export(Options{OrgSlug: "gh/acme", ...})

    ex->>orgAPI: GetOrganization("gh/acme")
    orgAPI->>CCI: GET /api/v2/organization/gh%2Facme
    CCI-->>orgAPI: {id, name, slug, vcs_type}
    orgAPI-->>ex: Organization

    ex->>orgAPI: GetOrgSettings("github", "acme")
    orgAPI->>CCI: GET /api/v1.1/organization/github/acme/settings
    CCI-->>orgAPI: {feature_flags: {require_context_group_restriction}}
    orgAPI-->>ex: OrgSettings (best-effort; warning added on error)

    loop For each context page
        ex->>ctxAPI: ListContexts(ownerID)
        ctxAPI->>CCI: GET /api/v2/context?owner-id=<id>
        CCI-->>ctxAPI: {items: [...], next_page_token}
    end

    loop For each context
        ex->>ctxAPI: ListEnvVars(contextID)
        ctxAPI->>CCI: GET /api/v2/context/<id>/environment-variable
        CCI-->>ctxAPI: {items: [{variable, created_at}]} — values masked

        ex->>ctxAPI: ListRestrictions(contextID)
        ctxAPI->>CCI: GET /api/v2/context/<id>/restrictions
        CCI-->>ctxAPI: {items: [{restriction_type, restriction_value, name}]}

        Note over ex: Group restrictions → warning added\n(writes not yet GA)
    end

    ex->>projAPI: FollowedProjectsForOrg("acme")
    projAPI->>CCI: GET /api/v1.1/projects
    CCI-->>projAPI: [{username, reponame, ...}] — flat array

    loop For each project slug
        ex->>projAPI: GetProject(slug)
        projAPI->>CCI: GET /api/v2/project/<slug>
        CCI-->>projAPI: {id, name, slug, vcs_info}

        ex->>projAPI: GetSettings(provider, org, proj)
        projAPI->>CCI: GET /api/v2/project/<provider>/<org>/<proj>/settings
        CCI-->>projAPI: {advanced: {...}}

        ex->>projAPI: ListEnvVars(slug)
        projAPI->>CCI: GET /api/v2/project/<slug>/envvar
        CCI-->>projAPI: {items: [{name, value: "xxxx1234"}]} — values masked

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
    START([User commits manifest.json\nto source-org repo])

    START --> TRIGGER[Push triggers CircleCI pipeline\nin source org]

    TRIGGER --> PARALLEL

    subgraph PARALLEL ["Parallel extract jobs — one per context"]
        J1["Job: extract-deploy-prod\ncontext: [deploy-prod]\n\n1. checkout\n2. orb: install circleci-migrate\n3. secrets extract\n   --context deploy-prod\n4. persist secrets-context-deploy-prod.json\n   to workspace + artifacts"]

        J2["Job: extract-shared\ncontext: [shared]\n\n1. checkout\n2. orb: install circleci-migrate\n3. secrets extract\n   --context shared\n4. persist secrets-context-shared.json\n   to workspace + artifacts"]

        JN["... one job per context ..."]
    end

    PARALLEL --> MERGE

    MERGE["Job: merge\n(requires all extract jobs)\n\n1. attach_workspace\n2. circleci-migrate secrets merge\n   -o secrets.json secrets-*.json\n3. store_artifacts: secrets.json"]

    MERGE --> DOWNLOAD[User downloads secrets.json\nartifact from CircleCI UI]

    DOWNLOAD --> NOTE["secrets.json contains plaintext values\nProtect it — do not commit it"]
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
    START([User runs: circleci-migrate sync\n--manifest manifest.json\n--secrets secrets.json])

    START --> LOAD["Load manifest.json\nLoad secrets.json (optional)\nLoad mapping.json (optional)"]

    LOAD --> RESOLVE["Resolve destination org slug → UUID\nGET /api/v2/organization/<slug>"]

    RESOLVE --> LIST_CTX["List existing contexts in destination org\nGET /api/v2/context?owner-id=<dest-id>"]

    LIST_CTX --> LOOP

    subgraph LOOP ["For each context in manifest"]
        ENSURE{"Context exists\nin destination?"}

        ENSURE -->|Yes| REUSE["Status: exists\nReuse existing context ID"]
        ENSURE -->|No, dry run| WOULD_CREATE["Status: created (would create)\nNo API call"]
        ENSURE -->|No, --apply| CREATE["POST /api/v2/context\nStatus: created"]

        REUSE --> VARS
        WOULD_CREATE --> VARS
        CREATE --> VARS

        subgraph VARS ["For each env var in context"]
            HAS_VAL{"Value in\nsecret bundle?"}
            HAS_VAL -->|Yes, --apply| UPSERT["PUT /api/v2/context/<id>/environment-variable/<name>\nStatus: set"]
            HAS_VAL -->|Yes, dry run| DRY_SET["Status: set (would set)\nNo API call"]
            HAS_VAL -->|No, skip policy| SKIP["Status: manual\nSkipped — set manually"]
            HAS_VAL -->|No, placeholder policy| PLACEHOLDER["PUT with REPLACE_ME value\nStatus: set (placeholder)"]
        end

        VARS --> RESTRS

        subgraph RESTRS ["For each restriction in context"]
            RTYPE{"Restriction\ntype?"}
            RTYPE -->|expression| EXPR_CHECK{"Already exists\nin destination?"}
            EXPR_CHECK -->|Yes| EXISTS["Status: exists"]
            EXPR_CHECK -->|No, --apply| CREATE_RESTR["POST /api/v2/context/<id>/restrictions\nStatus: set"]
            EXPR_CHECK -->|No, dry run| WOULD_RESTR["Status: set (would add)"]
            RTYPE -->|project| MANUAL_P["Status: manual\nSource-org project UUID\ndoes not transfer"]
            RTYPE -->|group| MANUAL_G["Status: manual\nGroup-restriction writes\nnot yet GA"]
        end
    end

    LOOP --> REPORT["Print sync report:\nDRY RUN / APPLIED\ncreated / exists / set / manual / error counts\nNeeds-attention list"]

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
