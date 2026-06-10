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
        SECRETS_CMD["secrets extract / merge / capture"]
        SYNC[sync]
        MIGRATE[migrate]
        ORB_INLINE["orb inline"]
    end

    subgraph internal ["internal/"]
        EXPORTER["exporter<br/>Orchestrates API reads<br/>into a Manifest"]
        SYNCER["syncer<br/>Applies a Manifest + SecretBundle<br/>to the destination org"]
        SECRETS_PKG["secrets<br/>Reads live env vars<br/>into a SecretBundle"]
        CAPTURE_PKG["capture<br/>Generates inline config,<br/>triggers pipeline, polls,<br/>downloads artifact"]
        REPORT["report<br/>Formats terminal summary<br/>and audit Markdown"]
        MANIFEST["manifest<br/>Shared on-disk contract:<br/>Manifest - SecretBundle - Mapping"]
        GITHUB_PKG["github<br/>Resolves GitHub repo numeric ID<br/>for App pipeline definitions"]
        ORB_PKG["orb<br/>Fetches orb source via GraphQL<br/>and inlines into config YAML"]
    end

    subgraph api ["api/ (thin HTTP clients)"]
        ORG_API["org<br/>v2: GetOrg, ResolveOrgID<br/>v1.1: GetOrgSettings<br/>CIAM: groups, SSO"]
        CTX_API["context<br/>v2: List, Create, UpsertEnvVar<br/>ListRestrictions, CreateRestriction"]
        PROJ_API["project<br/>v2: GetProject, CreateProjectShell<br/>CreateAppProject, PipelineDefs, Triggers<br/>ListEnvVars, Settings, Follow<br/>v1.1: ListFollowedProjects<br/>private: ListOrgProjects"]
        PIPELINE_API["pipeline<br/>v2: TriggerWithConfig, GetStatus<br/>ListWorkflows, ListJobArtifacts"]
        REST["rest<br/>Shared HTTP client<br/>Circle-Token header<br/>JSON encode / decode"]
        GRAPHQL["graphql<br/>graphql-unstable endpoint<br/>OrbSource query"]
    end

    CIRCLE_API["CircleCI API<br/>circleci.com/api/v2<br/>circleci.com/api/v1.1<br/>circleci.com/api/private<br/>circleci.com/graphql-unstable<br/>app.circleci.com/private/ciam"]
    GITHUB_API["GitHub REST API<br/>api.github.com/repos"]

    ROOT --> EXPORT
    ROOT --> SECRETS_CMD
    ROOT --> SYNC
    ROOT --> MIGRATE
    ROOT --> ORB_INLINE

    MIGRATE --> EXPORTER
    MIGRATE --> SYNCER
    EXPORT --> EXPORTER
    EXPORT --> REPORT
    SECRETS_CMD --> SECRETS_PKG
    SECRETS_CMD --> CAPTURE_PKG
    SYNC --> SYNCER
    ORB_INLINE --> ORB_PKG

    EXPORTER --> ORG_API
    EXPORTER --> CTX_API
    EXPORTER --> PROJ_API
    EXPORTER --> MANIFEST

    SYNCER --> ORG_API
    SYNCER --> CTX_API
    SYNCER --> PROJ_API
    SYNCER --> MANIFEST
    SYNCER --> GITHUB_PKG

    CAPTURE_PKG --> MANIFEST
    CAPTURE_PKG --> PIPELINE_API

    SECRETS_PKG --> MANIFEST
    REPORT --> MANIFEST

    GITHUB_PKG --> GITHUB_API
    ORB_PKG --> GRAPHQL

    ORG_API --> REST
    CTX_API --> REST
    PROJ_API --> REST
    PIPELINE_API --> REST
    GRAPHQL --> REST

    REST --> CIRCLE_API
```

`internal/manifest` is the shared data contract. Every command reads or
writes `Manifest`, `SecretBundle`, or `Mapping` structs from this package.
The API clients never depend on each other; they communicate only through
the manifest types.

---

## `migrate` command flow

`migrate` combines the export and sync steps into one command.
The manifest is held in memory; it is never written to disk unless
`--output` is passed.

```mermaid
flowchart TD
    START(["circleci-migrate migrate<br/>--source-org gh/acme --dest-org gh/acme-new --apply"])

    START --> SRC_CLIENTS["Build source API clients<br/>(source token)"]
    SRC_CLIENTS --> EXPORT_STEP["Export source org<br/>(same as Phase 1 — read-only)"]
    EXPORT_STEP --> IN_MEM["In-memory Manifest<br/>(no disk write unless --output set)"]
    IN_MEM --> OPTWRITE{"--output / --report<br/>set?"}
    OPTWRITE -->|Yes| SAVE["Save manifest.json<br/>and/or migration-report.md"]
    OPTWRITE -->|No| DST_CLIENTS
    SAVE --> DST_CLIENTS

    DST_CLIENTS["Build destination API clients<br/>(dest token)"]
    DST_CLIENTS --> SYNC_ORG["SyncOrgSettings<br/>(feature flags, OIDC, URL-orb, policies, OTel, contacts)"]
    SYNC_ORG --> SYNC_CTX["SyncContexts<br/>(create / reuse contexts, set vars, add restrictions)"]
    SYNC_CTX --> SYNC_PROJ["SyncProjects<br/>(create / configure projects)"]
    SYNC_PROJ --> ENABLE["handleEnableBuilds<br/>(follow OAuth / enable App triggers)"]
    ENABLE --> END([Done])
```

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

    User->>export: "circleci-migrate export --org gh/acme"

    export->>ex: "Export(Options)"

    ex->>orgAPI: "GetOrganization(gh/acme)"
    orgAPI->>CCI: "GET /api/v2/organization/gh%2Facme"
    CCI-->>orgAPI: "id, name, slug, vcs_type"
    orgAPI-->>ex: Organization

    ex->>orgAPI: "GetFeatureFlags(github, acme)"
    orgAPI->>CCI: "GET /api/v1.1/organization/github/acme/settings"
    CCI-->>orgAPI: feature_flags map
    orgAPI-->>ex: OrgSettings

    ex->>orgAPI: "GetOIDCClaims(orgID)"
    orgAPI->>CCI: "GET /api/v2/org/orgID/oidc-custom-claims"
    CCI-->>orgAPI: "audience, ttl"
    orgAPI-->>ex: OIDCClaims

    ex->>orgAPI: "GetURLOrbAllowList(orgSlug)"
    orgAPI->>CCI: "GET /api/v2/organization/orgSlug/url-orb-allow-list"
    CCI-->>orgAPI: items
    orgAPI-->>ex: URLOrbAllowList

    ex->>orgAPI: "GetPolicyBundle(ownerID)"
    orgAPI->>CCI: "GET /api/v2/owner/ownerID/context/config/policy-bundle"
    CCI-->>orgAPI: "policy name to Rego map"
    orgAPI-->>ex: ConfigPolicies

    ex->>orgAPI: "GetAuditLogConfigs(orgID)"
    orgAPI->>CCI: "GET /api/v2/organizations/orgID/audit-log/configs"
    CCI-->>orgAPI: audit-log config items
    orgAPI-->>ex: AuditLogConfigs

    ex->>orgAPI: "GetSSOEnforced(orgID)"
    orgAPI->>CCI: "GET app.circleci.com/private/ciam/orgs/orgID/sso/enforced"
    CCI-->>orgAPI: enforced bool
    orgAPI-->>ex: SSOEnforced

    loop For each context page
        ex->>ctxAPI: "ListContexts(ownerID)"
        ctxAPI->>CCI: "GET /api/v2/context?owner-id=id"
        CCI-->>ctxAPI: "items, next_page_token"
    end

    loop For each context
        ex->>ctxAPI: "ListEnvVars(contextID)"
        ctxAPI->>CCI: "GET /api/v2/context/id/environment-variable"
        CCI-->>ctxAPI: "items with names only (values masked)"

        ex->>ctxAPI: "ListRestrictions(contextID)"
        ctxAPI->>CCI: "GET /api/v2/context/id/restrictions"
        CCI-->>ctxAPI: "restriction_type, restriction_value, name"
    end

    ex->>projAPI: "DiscoverProjects(orgID, orgName)"
    projAPI->>CCI: "GET /api/v1.1/projects (OAuth)<br/>or GET /api/private/project?organization-id=id (App)"
    CCI-->>projAPI: project list

    loop For each project slug
        ex->>projAPI: "GetProject(slug)"
        projAPI->>CCI: "GET /api/v2/project/slug"
        CCI-->>projAPI: "id, name, slug, vcs_info"

        ex->>projAPI: "GetSettings(provider, org, proj)"
        projAPI->>CCI: "GET /api/v2/project/provider/org/proj/settings"
        CCI-->>projAPI: advanced settings object

        ex->>projAPI: "ListEnvVars(slug)"
        projAPI->>CCI: "GET /api/v2/project/slug/envvar"
        CCI-->>projAPI: "names with masked values"

        opt App org project
            ex->>projAPI: "ListPipelineDefinitions(projectID)"
            projAPI->>CCI: "GET /api/v2/projects/projectID/pipeline-definitions"
            CCI-->>projAPI: pipeline definition list

            loop For each pipeline definition
                ex->>projAPI: "ListTriggers(projectID, defID)"
                projAPI->>CCI: "GET /api/v2/projects/projectID/pipeline-definitions/defID/triggers"
                CCI-->>projAPI: trigger list
            end
        end

        opt "--skip-extras not set"
            ex->>projAPI: "ListCheckoutKeys, ListWebhooks, ListSchedules"
        end
    end

    ex-->>export: "Manifest (no secret values)"
    export->>export: "manifest.Save(manifest.json)"
    export->>export: "report.SaveMarkdown(m, migration-report.md)"
    export-->>User: "terminal summary + file paths"
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

## `secrets capture` — CLI-orchestrated flow

`secrets capture` achieves the same result as the orb-based Phase 2 without
requiring a committed `.circleci/config.yml`. The entire pipeline is generated
at runtime and submitted as an inline (unversioned) config via the v2 Pipelines
API.

```mermaid
flowchart TD
    START(["circleci-migrate secrets capture --org gh/acme --manifest manifest.json"])

    START --> LOAD["Load manifest.json<br/>Build list of contexts and projects to extract"]
    LOAD --> GEN["Generate inline pipeline config<br/>(one extract job per context, one merge job)"]
    GEN --> TRIGGER["POST /api/v2/project/slug/pipeline<br/>body: branch + inline config YAML<br/>Returns pipeline ID"]
    TRIGGER --> POLL

    subgraph POLL ["Poll until terminal state or --poll-timeout"]
        direction TB
        STATUS["GET /api/v2/pipeline/id<br/>Check state field"]
        STATUS -->|"running / pending"| WAIT["Wait 5 s, retry"]
        WAIT --> STATUS
        STATUS -->|success| FETCH["Fetch artifact"]
        STATUS -->|"failed / errored"| FAIL["Exit non-zero<br/>Print workflow/job state"]
    end

    FETCH --> JOBS["GET /api/v2/pipeline/id/workflow<br/>GET /api/v2/workflow/wf-id/job<br/>Locate merge job by name"]
    JOBS --> ARTIFACTS["GET /api/v2/project/slug/job-number/artifacts<br/>Locate secrets.json artifact URL"]
    ARTIFACTS --> DOWNLOAD["GET artifact URL (Circle-Token header)<br/>Write to --output path (mode 0600)"]
    DOWNLOAD --> DONE([Done — secrets.json ready for sync])
```

Key differences from the orb-based approach:

- No `.circleci/config.yml` is committed to the repository.
- The inline config is ephemeral — it exists only for the duration of the run.
- `--enable-trigger` and `--remove-restrictions` allow the command to temporarily
  unlock contexts or triggers that would otherwise block the pipeline from running,
  restoring them after the run completes.
- `--skip-restricted-contexts` is a safer alternative: skip any context with
  active restrictions rather than modifying them.

---

## Phase 3 — Sync flow

```mermaid
flowchart TD
    START(["circleci-migrate sync --manifest manifest.json --secrets secrets.json --apply"])

    START --> LOAD["Load manifest.json<br/>Load secrets.json (optional)<br/>Load mapping.json (optional)"]

    LOAD --> RESOLVE["Resolve destination org slug to UUID<br/>GET /api/v2/organization/slug"]

    RESOLVE --> ORG_SETTINGS["Sync org settings<br/>(feature flags, OIDC, URL-orb allow list,<br/>config policies, OTel exporters, contacts)<br/>Skipped with --skip-org-settings"]

    ORG_SETTINGS --> LIST_CTX["List existing contexts in destination org<br/>GET /api/v2/context?owner-id=dest-id"]

    LIST_CTX --> CTX_LOOP

    subgraph CTX_LOOP ["For each context in manifest"]
        ENSURE{"Context exists<br/>in destination?"}

        ENSURE -->|Yes| REUSE["Status: exists<br/>Reuse existing context ID"]
        ENSURE -->|"No — dry run"| WOULD_CREATE["Status: created (would create)<br/>No API call"]
        ENSURE -->|"No — --apply"| CREATE["POST /api/v2/context<br/>Status: created"]

        REUSE --> VARS
        WOULD_CREATE --> VARS
        CREATE --> VARS

        subgraph VARS ["For each env var in context"]
            HAS_VAL{"Value in<br/>secret bundle?"}
            HAS_VAL -->|"Yes — --apply"| UPSERT["PUT context/id/environment-variable/name<br/>Status: set"]
            HAS_VAL -->|"Yes — dry run"| DRY_SET["Status: set (would set)<br/>No API call"]
            HAS_VAL -->|"No — skip policy"| SKIP["Status: manual<br/>Skipped — set manually"]
            HAS_VAL -->|"No — placeholder policy"| PLACEHOLDER["PUT with REPLACE_ME value<br/>Status: set (placeholder)"]
        end

        VARS --> RESTRS

        subgraph RESTRS ["For each restriction in context"]
            RTYPE{"Restriction<br/>type?"}
            RTYPE -->|expression| EXPR_CHECK{"Already exists<br/>in destination?"}
            EXPR_CHECK -->|Yes| EXISTS["Status: exists"]
            EXPR_CHECK -->|"No — --apply"| CREATE_RESTR["POST context/id/restrictions<br/>Status: set"]
            EXPR_CHECK -->|"No — dry run"| WOULD_RESTR["Status: set (would add)"]
            RTYPE -->|group| GROUP_RESOLVE["Resolve dest group UUID by name<br/>via CIAM /groups endpoint"]
            GROUP_RESOLVE -->|"Found"| CREATE_GRP["POST context/id/restrictions<br/>Status: set"]
            GROUP_RESOLVE -->|"Not found"| MANUAL_G["Status: manual<br/>Create group in dest then re-run"]
            RTYPE -->|project| MANUAL_P["Status: manual<br/>Source-org project UUID does not transfer"]
        end
    end

    CTX_LOOP --> PROJ_LOOP

    subgraph PROJ_LOOP ["For each project in manifest"]
        DEST_TYPE{"Destination org<br/>type?"}

        DEST_TYPE -->|"OAuth (gh/)"| OAUTH_PATH["OAuth path:<br/>GET project by slug"]
        DEST_TYPE -->|"App (circleci/)"| APP_PATH["App path:<br/>GET /api/private/project?org-id=..."]

        OAUTH_PATH --> PROJ_EXISTS{"Project exists?"}
        PROJ_EXISTS -->|Yes| PROJ_CONFIG["Configure: settings, vars,<br/>webhooks, schedules, OIDC, v1.1 flags"]
        PROJ_EXISTS -->|"No — dry run"| WOULD_CREATE_PROJ["Status: created (would create)<br/>Queued for enable-builds"]
        PROJ_EXISTS -->|"No — --apply"| CREATE_SHELL["POST /api/v2/organization/provider/org/project<br/>Status: created (paused)"]
        CREATE_SHELL --> QUEUE_FOLLOW["Queue EnableTarget (kind: follow)"]
        QUEUE_FOLLOW --> PROJ_CONFIG

        APP_PATH --> APP_EXISTS{"Project exists<br/>by name?"}
        APP_EXISTS -->|Yes| APP_CONFIG["Configure: settings, vars,<br/>webhooks, schedules, OIDC, v1.1 flags"]
        APP_EXISTS -->|"No — dry run"| WOULD_CREATE_APP["Status: created (would create)<br/>Pipeline defs shown as planned"]
        APP_EXISTS -->|"No -- --apply"| CREATE_APP["POST /api/v2/organization/orgID/project<br/>Status: created"]
        CREATE_APP --> CREATE_DEFS["POST pipeline-definitions<br/>POST triggers (disabled=true)"]
        CREATE_DEFS --> QUEUE_TRIGGERS["Queue EnableTarget (kind: trigger)<br/>for each created trigger"]
        QUEUE_TRIGGERS --> APP_CONFIG
    end

    PROJ_LOOP --> ENABLE_STEP

    subgraph ENABLE_STEP ["Enable-builds step"]
        PENDING{"PendingEnable<br/>list empty?"}
        PENDING -->|Yes| DONE
        PENDING -->|"No — dry run"| DRY_ENABLE["Print: re-run with --apply to enable"]
        PENDING -->|"No -- --apply + --yes"| AUTO_ENABLE["Enable all automatically"]
        PENDING -->|"No -- --apply + TTY"| PROMPT["Prompt: Enable builds? y/N"]
        PROMPT -->|y| ENABLE_ALL["For each target:<br/>follow (OAuth) or EnableTrigger (App)"]
        PROMPT -->|N| SKIP_ENABLE["Print: re-run with --yes to enable later"]
        AUTO_ENABLE --> DONE
        ENABLE_ALL --> DONE
    end

    DONE([Done])

    ENABLE_STEP --> REPORT_OUT["Print sync report:<br/>DRY RUN or APPLIED<br/>created / exists / set / manual / error counts<br/>Needs-attention list"]

    REPORT_OUT --> END([Done])
```

---

## Project creation and enable-builds detail

The create-then-enable model ensures that builds never fire on a destination
project until you explicitly say so. The flow differs between org types:

```mermaid
flowchart LR
    subgraph OAUTH ["OAuth org (gh/)"]
        O1["CreateProjectShell<br/>POST /organization/provider/org/project<br/>No webhook installed<br/>No builds fire"] --> O2["Queue EnableTarget<br/>kind: follow"]
        O2 --> O3["EnableBuilds: FollowProject<br/>POST /api/v1.1/project/vcs/org/repo/follow<br/>Installs deploy key + webhook<br/>May trigger initial build"]
    end

    subgraph APP ["GitHub App org (circleci/)"]
        A1["CreateAppProject<br/>POST /organization/orgID/project<br/>Project shell created"] --> A2["CreatePipelineDefinition<br/>POST /projects/projID/pipeline-definitions"]
        A2 --> A3["CreateTrigger (disabled=true)<br/>POST /projects/projID/pipeline-definitions/defID/triggers<br/>Trigger exists but is paused — no builds fire"]
        A3 --> A4["Queue EnableTarget<br/>kind: trigger"]
        A4 --> A5["EnableBuilds: EnableTrigger<br/>PATCH /projects/projID/triggers/triggerID<br/>Sets disabled=false<br/>Builds can now fire"]
    end
```

**Webhook and schedule triggers** (App orgs) are flagged as `manual` — the
webhook HMAC secret cannot be migrated and schedule-trigger creation via the
Trigger API is a planned future addition.

---

Key properties of the sync phase:

- **Dry run by default.** No writes to CircleCI unless `--apply` is passed.
  Reviewing the dry-run output before applying is strongly recommended.
- **Idempotent.** Existing contexts and projects are reused by name. Re-running
  sync with `--apply` is safe — it will not duplicate resources or overwrite
  restrictions that already exist.
- **Transparent report.** Every action (created, exists, set, manual, error)
  is recorded and printed. Items requiring manual follow-up are surfaced
  explicitly.

---

## `orb inline` — GraphQL orb-source flow

The `orb inline` command rewrites a CircleCI config file, replacing each private
orb reference in the `orbs:` stanza with the orb's inlined YAML source. This is
used during the namespace-transfer overlap window (e.g. while `awesomecicd/`
content is being moved to `cci-labs/`) to produce a config that works regardless
of which namespace is active.

```mermaid
flowchart TD
    START(["circleci-migrate orb inline --config .circleci/config.yml"])

    START --> PARSE["Parse config YAML<br/>Extract orbs: stanza"]
    PARSE --> LOOP

    subgraph LOOP ["For each orb reference"]
        direction TB
        QUERY["POST circleci.com/graphql-unstable<br/>OrbSource query with orbVersionRef"]
        QUERY -->|"source returned"| INLINE["Replace orb reference with inline source block"]
        QUERY -->|"null source (public or not found)"| PASS["Pass through unchanged"]
    end

    LOOP --> WRITE["Write merged config to --output (or stdout)"]
    WRITE --> DONE([Done])
```

The GraphQL query (`OrbSource`) is sent to the `graphql-unstable` endpoint with
a `Circle-Token` header. The `source` field in the response is the raw orb YAML
string. Public orbs return a source but are left as references (they are
resolvable without a token). Private orbs with a `null` source are left as-is
with a warning — they require a token with access to that namespace.
