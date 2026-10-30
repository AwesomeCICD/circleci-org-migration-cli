# Test plan: GitHub OAuth → GitHub App (standalone) migration

This is the acceptance test for `circleci-migrate` moving a GitHub OAuth org
(`gh/<org>`) to a standalone GitHub App org (`circleci/<org-id>`). It covers
what the CLI migrates on its own and the steps that need CircleCI support.

Unit tests (`make verify`) cover the code paths. This plan is the **live**
check: a full run between throwaway orgs before running it for a customer.

## Fixture orgs

| Role | Org | Notes |
| --- | --- | --- |
| Source | `gh/gh-oauth-cci-1` (OAuth) | Throwaway. The CircleCI GitHub App must be installed on its GitHub org **linked to this OAuth org**, so it has GitHub App pipelines the way BCBSM does |
| Destination | New throwaway standalone org | Created for the test. Gets the GitHub App only at step 4, after it is uninstalled from the source |
| Namespace | A throwaway orb/runner namespace owned by the source | Needed to test the support-run `change-namespace-owner!` |

A GitHub org links to only **one** CircleCI org through the GitHub App, so
source and destination cannot both hold the app at the same time. The test
has to go through the uninstall → reinstall handoff, just like a customer.

## Seed the source

- **Contexts:** one unrestricted; one with a project restriction; one with an
  expression restriction; one with a group restriction; one whose values
  contain quotes, `$`, newlines and unicode (RBI's "bad characters" broke its
  scripts).
- **Project A:** a single implicit OAuth pipeline on `.circleci/config.yml`,
  with project env vars, an additional SSH key, a webhook and a schedule.
- **Project B:** three GitHub App pipeline definitions: the default path, a
  custom path (`.circleci/java.yml`) and a deploy definition. Triggers should
  include a push with a branch filter, a disabled trigger and a schedule
  trigger. Also give it one Deploys environment and component.
- **Org:** an OIDC custom claim, a config policy, a URL-orb allow list, a
  private orb with two versions in the test namespace, and a runner resource
  class in the test namespace.

## Run and expected results

| # | Step | Expected |
| --- | --- | --- |
| 1 | `doctor --source-org … --dest-org circleci/<id>` | Passes. If any project needs a synthesized definition and there's no GitHub token, it fails with an actionable error |
| 2 | `export --orb-namespace <ns> --runner-namespace <ns>` | The manifest has every definition and trigger with all fields, plus deploy environments and components. The report includes the cross-type checklist |
| 3 | `sync` dry run with `--mapping` | Context and org lines say `create`. Project lines error only because the repos aren't connected yet. Orbs and runners say `manual` with the exact `change-namespace-owner!` command, because the source still owns the namespace |
| 4 | Uninstall the GitHub App from the source; install it from the destination | The source's GitHub App pipelines are gone. The destination's VCS Connections shows today's install date |
| 5 | `sync --apply`, answering **N** at enable | Projects are created with **every** captured definition: same names, file paths, descriptions and all trigger fields, disabled. Contexts have values and restrictions. The exit code is 0 |
| 6 | Run `sync --apply` again | Everything says `exists`; nothing is duplicated |
| 7 | Delete one definition in the destination, then re-run `sync --apply` | The missing definition and its triggers are recreated |
| 8 | Re-run `sync --apply --yes` | Nothing is enabled: the triggers already exist and a re-run only enables triggers it creates. Each says "left disabled; pass --enable-existing-triggers" |
| 8b | Re-run `sync --apply --yes --enable-existing-triggers` | The triggers that were enabled in the source are enabled now (each prints its trigger ID and definition name), and source-disabled triggers stay disabled |
| 9 | Force an enable failure (for example, revoke the token mid-run) | Non-zero exit and a per-project error line |
| 10 | Support runs `change-namespace-owner! "<ns>" "circleci/<short-id>"`, then you re-run `sync --apply` | Orbs need no action. Runner classes are recreated in the destination, or reported `manual` with the reason |
| 11 | Push to Project B | The right definitions run; the orb resolves; contexts inject; OIDC tokens carry the new org ID |
| 12 | `validate --source-org … --dest-org … --mapping mapping.json` | Parity passes, apart from the documented OAuth-only differences |

## Support-only steps (stage with a throwaway first)

- `transfer-plan "<from>" :github "<to-short-id>" :circleci`, on a throwaway
  paid plan or in staging. The destination must be Free and not a share rider.
- `change-namespace-owner!` on a throwaway namespace (step 10). Vijay did the
  same before RBI's cutover.

## Not covered by the CLI (check by hand)

- SSO, users joining and role defaults (new SSO members are Viewers).
- AWS/GCP OIDC trust policies.
- IP ranges entitlement.
- Build history, Insights, artifacts and caches; none of these transfer.
