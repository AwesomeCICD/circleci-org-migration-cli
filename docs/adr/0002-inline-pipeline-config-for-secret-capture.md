# 2. Inline (unversioned) pipeline config for secret-value capture

## Status

Accepted.

## Context

Context and project environment-variable *values* are masked by the CircleCI
API on read — only the variable names are returned. Migrating an org faithfully
requires transferring the actual secret values, so we needed a way to obtain
them without asking the operator to re-enter every secret by hand, and without
committing any helper configuration into the source org's repositories.

A pipeline running inside the source org can read the values it has access to.
The question was how to run such a pipeline without modifying the customer's
committed `.circleci/config.yml`.

## Decision

`secrets capture` runs an **inline (unversioned) pipeline config**: it
temporarily enables `api-trigger-with-config` (at org and project scope),
triggers a pipeline via `POST .../pipeline/run` with an inline `config.content`
that dumps the exported variable names and attached contexts to an artifact,
polls for completion, downloads the artifact, parses the values, and aggregates
them client-side. The `api-trigger-with-config` setting is restored to its
prior state afterward.

An orb (`extract_context` / `extract_project` jobs) provides the same capability
as an in-pipeline alternative for teams that prefer to drive it from their own
config.

## Consequences

- Secret values can be captured with **no committed configuration** in the
  customer's repositories — nothing is left behind in source control.
- Requires the ability to toggle `api-trigger-with-config`; the tool restores the
  original value to avoid leaving the org in a changed state.
- The capture pipeline must run on a project; contexts can be captured via any
  project, while project env-vars run under their own project.
- Values transit a pipeline artifact; this is handled with storage-access
  controls and (optionally) encryption rather than persisting plaintext longer
  than necessary.
