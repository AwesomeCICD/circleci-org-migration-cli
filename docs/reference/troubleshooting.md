# Troubleshooting

Find the error text or symptom in the table for the step you are on, then apply the fix. Add `--debug` to any command for verbose HTTP logging.

## Setup and authentication

| Error or symptom | Cause | Fix |
|---|---|---|
| `401` or `Unauthorized` | Token missing, wrong, or not an org admin of the org it acts on. | Set `CIRCLECI_SOURCE_TOKEN` and `CIRCLECI_DEST_TOKEN`. Each token's user must be an org admin of its org. Run `circleci-migrate doctor`. |
| Requests go to `circleci.com` but you run CircleCI Server | Default host. | Pass `--host https://circleci.example.com`, or set `CIRCLECI_CLI_HOST`. |
| Preflight fails: GitHub token required | An OAuth project with no captured App pipeline definition needs a repo ID, which only the GitHub API returns. | Set `GITHUB_TOKEN` (repo read) or pass `--github-token`, then re-run. Not needed with `--skip-projects`. |
| `--dest-org-id` rejected | You passed a slug. | Pass the destination org UUID from **Organization Settings > Overview**. |
| Prompts hang in CI | No terminal. | Add `--no-input`. It is implied when stdin is not a terminal. |

## Export

| Error or symptom | Cause | Fix |
|---|---|---|
| A repo is missing from the manifest | The repo was never set up as a CircleCI project. | For `gh/` orgs, add `--follow-all --github-token "$GITHUB_TOKEN"` to `export`. Archived repos are skipped. |
| Orbs or runner classes are missing | The namespace was not given. | Pass `--orb-namespace` and `--runner-namespace`. There is no automatic lookup. |

## Sync

| Error or symptom | Cause | Fix |
|---|---|---|
| `sync` hit the source org | No `org.to` in the mapping, so the destination defaults to the source. | Pass `--mapping` with `org.to` set. See [Mapping](../concepts/mapping.md). |
| Dry run shows everything as `manual` | No secrets bundle given. Expected on a first dry run. | Move values with `secrets transfer`, or pass `--secrets`. |
| `--apply` fails on project creation (App destination) | The repo is not connected to the destination CircleCI GitHub App. | Connect the repo in the destination org's GitHub App settings and re-run. The re-run creates the missing definition and trigger. |
| Pipeline definition `external_id` is wrong | Repos moved GitHub org, so the numeric repo IDs changed. | Add `--github-token` and `--dest-github-org`, or a `github_org` mapping entry. |
| Definition is `manual`: "already exists ... but differs" | A same-named definition has a different provider, repo ID or config path. | Align or delete it in the destination and re-run. `sync` never modifies it. |
| `sync` exits non-zero after a pipeline definition or trigger `[error]` | An API read or write failed for that project. The rest of the run still happened. | Fix the cause (access, a transient failure) and re-run `sync --apply`. Existing items report `exists`. `manual` items do not fail the exit code. |
| `sync --apply --yes` exits non-zero after "enabling builds" | At least one trigger failed to enable. | Fix it and re-run `sync --apply --yes --enable-existing-triggers`. |
| Builds do not start after apply (OAuth destination) | The project is not followed, or the webhook is not installed on GitHub. | Follow the project in the destination and confirm the GitHub webhook. |
| A project is missing from the plan | The destination cannot follow it yet. | Check GitHub access and onboarding for that repo. |
| `manual` on a project's env vars or context values | Secret values have not moved yet. | Run `secrets transfer`. Use `--missing-secrets placeholder` to create the names now. |
| CIAM roles or groups did not fully apply | Per-project grants and users not in the destination cannot be automated. | Check `migration-report.md`. Add the user to the destination org and re-run. `--skip-ciam` leaves roles alone. |
| Environment hierarchies are `manual` | They use a private API that a personal token may be refused. | Create environments under **Deploys > Environments** and hierarchies under **Deploys > Environment hierarchies**. |
| A user sees an empty Projects page in the new org | They have not authorized CircleCI with GitHub for the new org. | Have them click **Authorize** and then **Refresh permissions**. Confirm the repos are connected to the App installation. |

## Context restrictions

| Error or symptom | Cause | Fix |
|---|---|---|
| Project restriction is `manual` | No destination project of that name exists yet. | Run `sync --apply --skip-pipelines` to create the projects, add a `projects` mapping entry if the name differs, then re-run `sync --apply`. |
| Group restriction is `manual` | `sync` matches groups by name, and no group of that name exists in the destination. | Create the group, then re-run `sync --apply`. Or recreate the restriction in the destination UI. |

## Secrets transfer

| Error or symptom | Cause | Fix |
|---|---|---|
| `the project has no SSH key` | A `gh/` or `bb/` source project has neither a GitHub App pipeline definition nor a checkout key. | Add a deploy key or user key under **Project Settings > SSH Keys > Checkout SSH keys**, or pass `--host-project` with a project that qualifies. Run the transfer before uninstalling the App. |
| Deploy keys are disabled by a GitHub policy | An org or enterprise rule blocks deploy keys. | Add a user key instead, or transfer while the GitHub App is still installed. |
| `N source project(s) are paused (drop-all-build-requests=true)` | The source was paused by `cutover --pause-source`. | Run the transfer first, or restore with `cutover --undo --apply` and re-run. |
| `no destination project could be resolved` | A source project has no same-named destination project and no mapping entry. | Create it with `sync --apply --skip-pipelines`, or add a `projects` entry, then re-run. Add `--skip-contexts` if contexts already went through. |
| `SKIP ... dest project unknown` in the plan | Same cause. | Same fix. |
| `unauthorized`, or `WARN: blocking restrictions` | A project or expression restriction on a context blocks the host project. | Re-run with `--remove-restrictions`. |
| `Permission denied`, or the pipeline is skipped | `allow_api_trigger_with_config` is off in the source org. | Add `--enable-trigger`, or enable it in **Organization Settings > Advanced**. |
| Project variables arrived empty | An older CLI version. | Upgrade `circleci-migrate` and re-run. |
| Destination context not created | The destination token is not an org admin token, or the org does not exist. | Use an admin token for the destination. |

## Secrets capture

| Error or symptom | Cause | Fix |
|---|---|---|
| `api-trigger-with-config disabled` | The org setting is off. | Add `--enable-trigger`. It is restored afterwards. |
| Capture stops without triggering anything | A non-interactive capture of everything fails closed. | Scope it with `--context` or `--project`, or pass `--yes` to allow it. |
| Values missing for restricted contexts | Restricted contexts are skipped by default. | Add `--remove-restrictions`, or use `sync --missing-secrets placeholder`. |

## Cutover

| Error or symptom | Cause | Fix |
|---|---|---|
| `--enable-dest` reports `manual` for a project | The trigger match is ambiguous, or the manifest shows the project paused or not followed. | Enable that trigger by hand in **Project Settings > Triggers**. Run `sync --apply` first so triggers exist. |
| `--undo` fails: state file missing or for another org | Wrong `--state` path or the file was deleted. | Point `--state` at the right file. Without it there is nothing to restore. |
| `--undo` only restores a flag | The GitHub App was already uninstalled from the source. | Expected. Pause the source before you uninstall next time. |
| Both orgs build | `sync --yes` enabled the destination while the source was live. | Pause the source with `cutover --pause-source --apply`. |

## Validate

| Error or symptom | Cause | Fix |
|---|---|---|
| `migration-secrets` shows as missing | The context exists only to run the transfer. | Add `--dest-token-context migration-secrets`, or `--ignore-context <name>`. |
| Runner or orb section is skipped | No destination namespace given. | Pass `--dest-runner-namespace` and `--dest-orb-namespace`. |
| Exit 1 with `missing` items | The destination lacks something the source has. | Fix each item and re-run. `manual` items do not fail the exit code. |

## Orbs and runners

| Error or symptom | Cause | Fix |
|---|---|---|
| Orbs are `manual` and `sync` does not republish | The destination namespace has the same name as the source. A namespace cannot exist in two orgs. | Ask CircleCI support to move the namespace to the destination org. Orbs move with it. |
| Runner class returns `409` or is `manual`: "attributed to another org" | A class keeps the org it was created under, even after a namespace move. | Delete the source copies, re-run `sync`, create new tokens and re-register agents. |
| Configs reference old orb names | A different destination namespace was used. | Rewrite each `old/orb` to `new/orb` from the notice that `sync` printed. |

## Terraform

| Error or symptom | Cause | Fix |
|---|---|---|
| `terraform apply` fails on project settings | Advanced settings are not supported for OAuth (`gh/`) orgs. | Regenerate with `--dest-org-type oauth`. See [Terraform](terraform.md). |
| `sync` overwrites Terraform-managed resources | Both tools manage them. | Run `sync --skip-terraform-managed`, or `--only org-settings,ciam,extras`. |
