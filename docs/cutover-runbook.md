# CircleCI org migration — cutover runbook

This is the generic operator runbook for migrating a CircleCI organization with
`circleci-migrate`. Every export also embeds a copy of this runbook in its
`migration-report.md`, tailored to the resources that export actually contains.
Use this document to understand the overall flow; use the per-export report for
the manual steps and data-loss notes that apply to your org.

## 1. Recommended cutover order

1. **Export the source org** — produces `manifest.json` and `migration-report.md`.
   The manifest is non-secret and safe to review, diff, and store. Read the
   report before continuing.
2. **Capture secret values** — run the in-pipeline `secrets` orb/step (or
   `secrets capture`) in the source org. CircleCI never returns env-var values
   over the API, so this is the only way to migrate them. The result is a
   plaintext `secrets.json` — treat it as sensitive.
3. **`sync --apply`** — creates the destination resources. New projects are
   created **paused**: App triggers are disabled and OAuth onboarding is not
   followed, so nothing builds until you explicitly enable it.
4. **Validate the destination** — compare contexts, env-var names, project
   settings, webhooks, schedules, and group restrictions against the report.
5. **Enable builds** — bring the destination live (`sync --yes`, the interactive
   prompt, or re-enable triggers / follow projects).
6. **Rotate the captured secrets** — once builds are healthy, rotate every value
   captured in step 2 and delete the extraction artifacts (`secrets.json` and
   any logs that may contain values).
7. **Update external pins** — repoint everything that references the old org
   (see the last section).

## 2. Automated by `sync --apply`

- Contexts and their environment variables (names; values from the capture step).
- Project settings, environment variables, webhooks, and scheduled pipelines.
- Project- and org-level OIDC custom claims (audience / TTL).
- Org settings: feature flags, OIDC, URL-orb allow list, config policies, and
  technical/security contacts.
- Project creation: OAuth orgs are onboarded by following the project; App orgs
  get their pipeline definitions and triggers recreated.
- Context group restrictions, mapped onto destination CIAM groups.

## 3. Manual steps required

Which of these apply depends on your source org; the per-export report lists only
the relevant ones. The first three always apply.

- **Context & project secret values** — never exported. Capture them in the
  source, supply the bundle to `sync`, then rotate after cutover.
- **Checkout & SSH keys** — private key material is never exported. Regenerate
  deploy/checkout and user keys on the destination and update VCS-side deploy keys.
- **Webhook signing secrets** — not exported; regenerate and update receivers.
- **SSO (SAML)** — recreate manually (DNS TXT domain verification + IdP-side SAML
  app). Not automatable.
- **Audit-log streaming** — the S3 ARN/region/bucket/endpoint point at the source
  AWS account; recreate each stream against destination-owned infrastructure.
- **OpenTelemetry exporter headers** — header values are server-redacted and
  cannot be replayed; re-add them manually after `sync` creates the exporters.
- **Danger flags** (`require_context_group_restriction`, `drop_all_build_requests`)
  — enable only after validation, or they can silently break or drop builds.
- **Org technical & security contacts** — `sync` overwrites these; verify them.
- **Repository connections (App destinations)** — repos must exist and be
  connected to the destination CircleCI GitHub App before `sync --apply`, or
  project onboarding is skipped.

## 4. Does not transfer / data loss

- **Identifiers change.** Project, context, and pipeline UUIDs are reassigned by
  the destination; anything hard-coding a source UUID must be updated.
- **Captured secrets must be rotated.** Treat every captured value as exposed.
- **Cross-type moves lose settings.** OAuth→App drops fork-PR builds, the OSS
  flag, and `pr_only_branch_overrides` (no App equivalent). Multiple App pipeline
  definitions cannot collapse into a single OAuth config.

## 5. Update external pins

After cutover, update everything that points at the old org to the new org's
slugs/IDs:

- Service catalogs / Backstage entries referencing the old project slugs.
- Slack and other notification integrations.
- Dashboards, status badges, and Insights links.
- Branch-protection / required status-check integrations on the VCS side.
- Documentation, READMEs, and bookmarks linking to the old org.
