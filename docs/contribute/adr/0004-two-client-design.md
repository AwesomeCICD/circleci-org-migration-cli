# 4. Two-host client design (`app.circleci.com` vs `circleci.com`)

## Status

Accepted.

## Context

Different CircleCI capabilities live behind different hosts and API versions, and
they are not interchangeable:

- Most v2 work and the private project-list endpoint use the standard REST host.
- The private CIAM BFF (org groups, SSO) lives on **`app.circleci.com`**.
- Some org/project settings are only writable via **v1.1 on `circleci.com`** —
  notably feature-flag writes, which are a `PUT .../api/v1.1/.../settings`. The
  equivalent `POST` against `app.circleci.com` returns 404 (verified live).

Treating these as one base URL led to wrong-host calls failing.

## Decision

Model the destinations explicitly so each call is routed to the correct host and
API version. The REST client layer distinguishes the standard host from the
`app.circleci.com` host, and v1.1 feature-flag writes are sent as `PUT` to
`circleci.com` rather than to `app.circleci.com`. Each client method documents
which host/version it targets.

## Consequences

- Calls land on the host that actually serves them; the v1.1-PUT-on-circleci.com
  path for feature flags is encoded once and reused, avoiding the 404 trap.
- The code makes the host/version split visible at the call site, which helps
  future maintainers reason about CircleCI's heterogeneous API surface.
- Adding a new capability requires knowing (and recording) which host/version
  serves it; this is a deliberate, low cost given the alternative of silent
  wrong-host failures.
