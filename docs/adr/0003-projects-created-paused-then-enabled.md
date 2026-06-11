# 3. Create projects paused, then enable them explicitly

## Status

Accepted.

## Context

When syncing to a GitHub App destination org, project creation involves a chain:
create the project, create its pipeline-definitions, then create triggers. Once
triggers are live, the destination project can start building on incoming
events. During a migration the operator may not be ready for the destination
org to react to webhooks immediately — premature builds could fire before the
cutover is intended, or before the operator has verified the migrated
configuration.

## Decision

For GitHub App destinations, create each trigger **disabled (paused)** and enable
it as a distinct, explicit step: `PATCH .../triggers {disabled:false}`.
Enabling builds is gated behind an interactive confirmation prompt (or `--yes`
for non-interactive runs). This makes "the destination starts building" a
deliberate operator action rather than a side effect of project creation.

## Consequences

- Migration can create the full destination project structure without triggering
  unintended builds, supporting a controlled cutover.
- The operator gets a clear, auditable enable step and can verify the migrated
  setup before going live.
- Slightly more API work (create-then-PATCH) versus creating already-enabled
  triggers, in exchange for safety during cutover.
