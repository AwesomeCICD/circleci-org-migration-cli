# What moves

Names, structure and settings move on their own. Secret values move with one extra command. A short list has to be done by hand.

Read each row as: does it move automatically, does it need a step in your runbook, or do you do it yourself. Nothing is ever deleted from the source org.

| Item | Moves automatically | Moves with a step | Do by hand |
|---|---|---|---|
| Contexts and variable names | `sync` | | |
| Context secret values | | `secrets transfer` | Treat every value as exposed once moved: rotate it |
| Context restrictions: project and expression | `sync` remaps projects by name | | |
| Context restrictions: group | | Automatic only if a group of the same name exists in the destination | Otherwise recreate in the destination UI |
| Project settings | `sync` | | OAuth-only settings with no App equivalent are dropped: fork-PR builds, fork-PR secrets, `pr_only_branch_overrides` |
| OSS flag | | | App projects detect it from repo visibility. For an OAuth destination with a public repo, set "Free and Open Source" in Advanced settings |
| Project env vars | Names via `sync` | Values via `secrets transfer --include-project-vars` | |
| Additional SSH keys | | `secrets transfer --include-ssh-keys` | |
| Checkout and deploy keys | Created when an OAuth project is followed | | Private keys are never exportable. App destinations need none. Re-add manually uploaded deploy keys on OAuth |
| Pipeline definitions and triggers (App destinations) | `sync` creates them, disabled | `cutover --enable-dest` turns them on | A plain OAuth project gets one synthesized `build-and-test` definition. Add others by hand. Trigger names are assigned by CircleCI |
| Schedules | `sync` | | |
| Webhooks | `sync` recreates them | `--webhook-secrets-out` writes the new outbound signing secrets and inbound URLs | Give every sender and receiver the new value. Without the flag, secrets are empty |
| Environments and hierarchies | `sync`, by name | | If the API rejects the write, create them under Deploys. Components appear on the first `circleci run release plan`. Deploy history stays in the source |
| Orbs and namespace | A different destination namespace is republished by `sync` | | A same-name namespace is moved by CircleCI support. Rewrite orb references if you changed namespace |
| Runner resource classes | `sync --dest-runner-namespace` | | After a same-name namespace move the classes stay attributed to the source: delete the copies, re-run `sync`, issue new tokens |
| Runner instances and tokens | | | Never move. Register agents against the destination classes |
| Org roles and groups | App and standalone orgs, for users already in the destination (`--skip-ciam` leaves them alone) | | Add users to the destination first, then re-run `sync` |
| Per-project role grants | | | Recreate on each destination project |
| SSO (SAML) | | | DNS verification and IdP setup. New SSO users default to Viewer: set Contributor for people who trigger pipelines |
| Org settings | `sync`: feature flags, OIDC claims, URL-orb allow list, config policies, contacts, retention, budgets, release tracker, block-unregistered-users | `--include-danger-flags` after you validate | |
| OpenTelemetry exporter headers, audit-log streaming | Exporter config via `sync` | | Re-add header values. Recreate streams against destination-owned infrastructure |
| Cloud OIDC trust policies, IP allowlists | | | Update them for the new org ID and ranges |
| Plan and billing | | | Ask CircleCI support to move the plan. The destination should be on the Free plan and not share another org's plan |
| Project API tokens | | `sync --create-project-tokens` mints new values | Repoint every consumer of the old token |
| Personal API tokens | | | Each user creates their own for the new org |
| Insights, build history, usage data | | | Do not move. `export --include-usage` saves a local usage snapshot only |
| IDs and slugs | | | New project, context and pipeline IDs are assigned, and `gh/acme/web` becomes `circleci/<org>/<project>` for App destinations. Update anything that hard-codes the old ones |

Secret values are never readable through the API. `secrets transfer` copies them from inside a pipeline, straight to the destination, and never writes them to disk. See [secrets](../concepts/secrets.md).

Moving from OAuth to a GitHub App loses a few settings that have no equivalent. The [OAuth to GitHub App runbook](../migrations/oauth-to-app.md) lists them.
