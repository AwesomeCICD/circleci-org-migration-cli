# Architecture Decision Records

This directory records the significant architectural decisions for
`circleci-migrate`. Each ADR is a short, standalone document using the standard
Context / Decision / Consequences / Status format.

| ADR | Title | Status |
|-----|-------|--------|
| [0001](0001-thin-rest-clients-over-sdk.md) | Thin REST clients instead of a third-party SDK | Accepted |
| [0002](0002-inline-pipeline-config-for-secret-capture.md) | Inline (unversioned) pipeline config for secret-value capture | Accepted |
| [0003](0003-projects-created-paused-then-enabled.md) | Create projects paused, then enable them explicitly | Accepted |
| [0004](0004-two-client-design.md) | Two-host client design (`app.circleci.com` vs `circleci.com`) | Accepted |

## Adding a new ADR

Create `NNNN-title.md` with the next number, fill in Context / Decision /
Consequences / Status, and add a row to the table above.
