# 1. Thin REST clients instead of a third-party SDK

## Status

Accepted.

## Context

The CLI talks to several CircleCI API surfaces (v2, v1.1, v3, the private CIAM
BFF, and a private project-list endpoint). It is modeled structurally on
`CircleCI-Public/circleci-cli` so that it could one day merge into the official
CLI. We needed to decide whether to depend on a generated or third-party Go SDK
or to own the HTTP layer directly.

No single SDK covers all of the surfaces we use — in particular the private
CIAM/groups endpoints and the private project-list endpoint. A heavy SDK
dependency would also complicate an eventual merge into the official CLI and
obscure exactly which endpoints and JSON shapes we depend on.

## Decision

Own thin REST clients under `api/` (`org`, `context`, `project`, plus a shared
`rest` client). No third-party CircleCI SDK. Each client method is small and
documents the exact endpoint, method, and request/response JSON shape it uses.

## Consequences

- Full control over every request (escaped paths, methods, headers, page sizes)
  and clear visibility into which API surface each call hits.
- We can mix v2, v1.1, v3, and private endpoints freely as the migration needs
  dictate, without waiting on an SDK to expose them.
- We carry the maintenance cost of the HTTP layer and must track API changes
  ourselves; this is mitigated by wire-level `httptest` tests that assert request
  details.
- Aligns with the official CLI's style, easing a potential future merge.
