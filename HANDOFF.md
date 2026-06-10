# Project Handoff — `circleci-migrate` (CircleCI org-migration CLI)

> **Purpose of this file:** give a new engineer or AI agent everything needed to
> continue this project with zero prior context. It records what's built, what's
> proven live, every decision (including chat-only ones), open questions, and the
> exact next steps. Last updated 2026-06-10.

---

## ⭐ SESSION UPDATE — v0.3.0 SHIPPED (2026-06-10)

**v0.3.0 released** end-to-end (tag pipeline all green: setup ✓, GoReleaser ✓,
orb-publish-prod ✓): binaries (4 platforms + checksums) on GitHub Releases,
**orb `awesomecicd/circleci-org-migration@0.3.0`** (now snake_case), and the
Homebrew formula in `AwesomeCICD/homebrew-tap` all at 0.3.0. PRs #35–#47.

**Features added:** leveled `--debug` logging (`internal/clog`) + actionable API
errors (endpoint/status/request-id + "file an issue" hint); **interactive
`migrate`** guided walkthrough (TTY-gated; `--no-input` for CI; flags fully
bypass); **self-hosted runner resource-class capture/sync** (`export
--runner-namespace`, `sync --dest-runner-namespace`, `migrate` both; new
`api/runner`; manifest `runner_resource_classes`); see the `runner-resource-class-api`
memory.

**Orb hardening (closes the v0.2.0 "remaining RC010" item):** components/params
renamed kebab→snake_case (RC010) — **breaking orb API** (`extract_context`,
`extract_project`, `project_slug`, `context_name`, …); long run-commands moved to
`<<include>>` scripts (RC009); examples → `@volatile` (RC011); `source_url` fixed;
**orb-review re-enabled** in CI and passing.

**Bugs caught ONLY via live e2e, then fixed + re-validated live:**
1. Orb **matrix** example didn't compile — a custom-`name` matrix needs an explicit
   `matrix.alias` for `merge`'s `requires`. Fixed.
2. Orb **`extract_project`** crashed for every real slug (slashes in the output
   filename). Fixed: both extract jobs write to a `captured/` dir with sanitized
   names; merge globs `captured/*.json`; CLI `SecretBundle.Save` `MkdirAll`s.

**Security/hardening:** token flags (`--token`/`--source-token`/`--dest-token`/
`--github-token`) leaked their env values into `--help` (flag defaults) → now
default `""`, env fallback in `settings.*OrDefault()` + root PersistentPreRunE
(regression test). **SSO IdP secrets** (client_secret/x509_cert/idp_metadata_xml)
were stored plaintext in the manifest → now **redacted** (key kept, value
placeholdered, `sso_secret_redacted` warning). Added `.gitleaks.toml` (allowlists
the SSO fixture file); `.claude/worktrees/` gitignored.

**Live-validated this session (DUMMY data only, in `cci-cli-test-1/-2` + the
`awesomecicd` runner namespace):** orb in a REAL pipeline via `orb inline`
(the orb is private, so cross-org use = inline) — single-context capture ✓,
3-context **matrix** ✓, **project** env-var capture ✓; context migration **with
values** App→App (`sync --apply --secrets`, verified in dest) ✓; **runner
capture** (11 real resource classes) ✓. See the `live-test-resources` memory for
the leftover dummy artifacts to clean up.

**Docs:** README badges + `docs/examples.md` (OAuth→OAuth, App→App, mixed two-leg,
cross-type, EMU repo-move, secrets-capture, runner); fleshed-out CONTRIBUTING; new
AI skill `.claude/skills/circleci-migration/SKILL.md`. **Coverage gate 75%→85%**
(total 87.5%).

**Still deferred (unchanged):** the **CircleCI-Labs / `cci-labs`** move — repo,
public orb namespace, module path, orb install URL, goreleaser owner, public brew
tap. The orb RC010 snake_case rename is now DONE (no longer pending for the move).

---

## ⭐ SESSION UPDATE — v0.2.0 SHIPPED (2026-06-10)

The CLI is **released and the full distribution pipeline is proven end-to-end live.**

- **v0.2.0 published** via release-please → tag → CircleCI GoReleaser:
  - **Binaries** (4 platforms + checksums) on GitHub Releases — public, anonymous
    download verified, binary runs with the injected version.
  - **Orb** `awesomecicd/circleci-org-migration@0.2.0` published (private).
  - **Homebrew** formula in `AwesomeCICD/homebrew-tap`
    (`brew tap AwesomeCICD/homebrew-tap && brew install circleci-migrate`).
  - Versioning automated: Conventional Commit → release-please PR → merge → tag →
    GoReleaser (binaries+brew) + orb-publish-prod. (`release-as` used once for 0.1.0, then removed.)
- **Repo is PUBLIC**; orb install scripts point at `AwesomeCICD` (anonymous download).
- **Live-validated this session**: App→App migrate (apply, context+value); OAuth→App
  and OAuth→OAuth (plans); secret capture; full App project-creation chain
  (create→pipeline-def→trigger→enable→delete); repo-move verification; `--projects` (bug found+fixed live).
- **Features added**: cross-type OAuth→App synthesis, `orb inline` (GraphQL orb-source +
  config inlining), org-group capture, cutover runbook, orb test suite (chunk + CI lint/
  validate/shellcheck gating publish), context-restriction auto-toggle, OTel/contacts/
  project-OIDC/v1.1-flags capture+sync.
- **Hardened live** (the CI project + contexts): `build_fork_prs`=false,
  `forks_receive_secret_env_vars`=false; `goreleaser` (GITHUB_TOKEN) + `orb-publishing`
  contexts, both restricted to the project.
- **Resolved**: public repo ✅, brew ✅, release-please ✅, inline-orb-swap ✅ (GraphQL is
  the only orb-source path — minimal POST, no SDK), first version 0.1.0→**0.2.0**. PRs #21–#33.

**Remaining (deferred/gated — NOT blocked code):**
- **CircleCI-Labs republish** (the move): repo → `CircleCI-Labs`, orb namespace → `cci-labs`
  (public orb), module path + orb install URL + goreleaser owner → CircleCI-Labs, public
  brew tap. At that point also fix orb-review RC010 (rename orb components kebab→snake_case —
  breaking, so do on the fresh republish) + RC011 (example version refs). See §7/§8.
- **SSO sub-field modeling** + **runner resource-class capture**: need an org WITH SSO /
  WITH self-hosted runners to verify live (test orgs have neither).
- **Functional orb test in a real pipeline**: binary install proven manually; a
  self-referential CI job was judged not worth the coupling — optional.

---

## 0. How to continue / provenance

- **Built across Claude Code sessions.** Current session ID:
  **`b916acbe-d474-41e8-a9ac-60268ff4a542`**. Full transcript:
  `~/.claude/projects/-Users-jim-git-circleci-org-migration-cli/b916acbe-d474-41e8-a9ac-60268ff4a542.jsonl`
  This session continued from earlier ones (other `*.jsonl` files in that dir,
  e.g. `d6bd3ff7-…`, `6541c5a4-…`, `2a1c0c4d-…`); the durable decisions from all
  of them are distilled into the `memory/` dir, so you don't need to replay the
  transcripts. To resume in Claude Code: `claude --resume b916acbe-d474-41e8-a9ac-60268ff4a542`.
- **Durable memory** (decisions, API references, live-test facts) lives in
  `~/.claude/projects/-Users-jim-git-circleci-org-migration-cli/memory/` with an
  index at `MEMORY.md`. Key memories: `migration-cli-goal`, `project-creation-api-reference`,
  `unversioned-config-extraction`, `org-settings-sync-candidates`,
  `release-and-orb-publish-setup`, `live-test-resources`, `sync-destination-write-reference`.
- **Source of truth = the code on `main` + this file + the memory dir.** When in
  doubt, read the code; it's small and heavily commented (every API method cites
  its endpoint + JSON shape).
- Work was done as small PRs (#5–#19 merged). Conventional Commits throughout.

---

## 1. What this is

A customer-facing **Go CLI (`circleci-migrate`)** that migrates one CircleCI
organization to another: contexts (incl. secret *values*), projects (settings,
env vars, pipeline-definitions, triggers, schedules, webhooks), and org-level
settings. Modeled structurally on `CircleCI-Public/circleci-cli` so it could one
day merge into the official CLI. **Own thin REST clients, no third-party SDK.**

Repo is hosted (temporarily) at **`github.com/AwesomeCICD/circleci-org-migration-cli`**
(INTERNAL visibility). **Final home: GH org `CircleCI-Labs`, orb namespace `cci-labs`.**
⚠️ The Go module path and a couple of references still say `CircleCI-Public/...`
(orb-template/default leftovers) — see §7 (must become `CircleCI-Labs` on the move).

---

## 2. Architecture

- **Commands** (`cmd/`): `export`, `secrets` (`extract`/`merge` for the orb +
  `capture` CLI-orchestrated), `sync`, `migrate`, `version`.
- **API clients** (`api/`): `org`, `context`, `project`, `rest` (shared). Use
  CircleCI v2 (preferred), v1.1 (org/project feature_flags — **writes are PUT
  `circleci.com/api/v1.1/.../settings`**, NOT app.circleci.com/POST), v3 (runner,
  not yet used), and the private CIAM BFF on **app.circleci.com** (groups, SSO).
- **Internal** (`internal/`): `manifest` (Manifest/SecretBundle/Mapping JSON
  contracts), `exporter`, `syncer`, `secrets`, `extract` (unversioned-config
  orchestration), `report`, `github` (resolve repo `external_id`).
- **Orb** (`orb/src/`): Orb Development Kit layout (`@orb.yml`, `commands/`,
  `jobs/`, `executors/`, `examples/`, `scripts/`). Packed with `circleci orb pack
  orb/src`. Commands: `install` (with version+arch caching, act-orb pattern),
  `restore_cli`, `cache_cli`, `extract_context`, `extract_project`. Jobs:
  `extract_context`, `extract_project`, `merge`. Executor `default` = small docker.
- **CI** (`.circleci/`): `config.yml` is a **setup config** using
  `circleci/path-filtering@3.0.0`; it continues to `continue_config.yml` which
  holds the `ci`, `release` (tag-only, GoReleaser), and `orb-publish-dev`/
  `orb-publish-prod` workflows. Orb publish fires only when `orb/` changes
  (dev on branches) or on a `vX.Y.Z` tag (prod). Coverage gate 75% (currently ~84%).

---

## 3. What's built (all merged to `main`)

- **export**: contexts (env-var NAMES, restrictions [expression/group/project],
  security groups), projects (advanced settings, env-var names, checkout keys,
  webhooks, schedules, **pipeline-definitions + triggers**), org settings (v1.1
  feature flags, OIDC org+project, URL-orb allow-list, config policies, audit-log,
  SSO, OTel exporters, tech/security contacts). Discovers ALL projects via the
  **private project-list endpoint** `GET /api/private/project?organization-id=`
  (page-size 50 — 100 returns 500), which works for BOTH OAuth and App orgs (the
  old v1.1 followed-list missed App-org projects).
- **secret VALUES** (the masked-by-API problem) — two paths:
  - **Orb** (in-pipeline): run `extract_context`/`extract_project` jobs.
  - **`secrets capture`** (CLI-orchestrated, NO committed config): enables
    `api-trigger-with-config` (org + project) and RESTORES it; triggers a pipeline
    with an **inline/unversioned config** (`POST .../pipeline/run` with
    `config.content`) that dumps the exported var names + attached contexts to an
    artifact; polls; downloads the artifact; parses values; aggregates client-side.
    Treats the default "All members" group restriction (value==orgID) as
    unrestricted. `--remove-restrictions` temporarily lifts genuine restrictions
    and restores them from the exported manifest state.
- **sync** (dry-run default, `--apply`): contexts (+ values from the bundle +
  restrictions incl. group via CIAM groups), projects (settings, env vars,
  webhooks, schedules, project OIDC, v1.1 flags), org settings. **Project
  creation**: OAuth = create shell + deferred follow; **GitHub App** = create
  project → pipeline-definitions → triggers created **disabled** (paused) → enable
  via `PATCH .../triggers {disabled:false}`. Enable-builds is an interactive
  prompt or `--yes`. Danger flags (`drop_all_build_requests`) skipped+warned.
  OAuth-only advanced fields (`oss`, `build_fork_prs`, `forks_receive_secret_env_vars`,
  `pr_only_branch_overrides`) are stripped for App dests.
- **Cross-type + repo-move (EMU)**: targets SAME-TYPE first (OAuth→OAuth, App→App,
  mixed = two runs since GH-App-on-OAuth is two separate CircleCI org records).
  Repo-move (repos moved to a new GitHub org) handled via **`--github-token` +
  `--dest-github-org`** (or `github_org` in the mapping file): resolves each repo's
  NEW `external_id` in the dest GitHub org — found → onboard, 404 → flag missing +
  skip (no broken projects), pre-flight preview in dry-run.
- **migrate** = all-in-one export → sync → enable.
- **Release/dist**: GoReleaser (cross-platform GitHub Releases), path-filtered
  orb publish (dev/prod), Renovate (live; dashboard issue #15; all deps current).

---

## 4. Live-validated against REAL orgs (2026-06-09)

Test orgs (see `e2e-fixtures.yaml` + the `live-test-resources` memory):
- **cci-cli-test-1** `circleci/RgCaaCv4TcKVRtngt3L4Q7` (id c7d47878-…) — SOURCE.
  Project `test-repo-1` (repo `cci-cli-test/test-repo-1`), context `test` (var `boo`).
- **cci-cli-test-2** `circleci/7QDiMfvptupf5ojScxXzrQ` (id 33d4c5fa-…) — DEST.
  Connected repo `cci-cli-test-2/test` (external_id 1264462906), project `fdffddf`.
- **Dummy-Test** `circleci/8G3atTsVAntm74tjLjbpaG` — write-safe scratch (no connected repo).

Proven live:
- `secrets capture` captured context `test`/`boo` value via an unversioned-config run.
- App→App `migrate` (cci-cli-test-1 → cci-cli-test-2): **context + its secret value
  migrated**, project shell created, settings + v1.1 flag write succeeded; error
  count driven 3→0; cross-GH-org repo → "manual".
- **Full App project-creation chain (raw API)**: create project → pipeline-definition
  → trigger `disabled:true` → PATCH enable → delete. Pause→resume works.
- **Repo-move verification**: `test-repo-1` → `cci-cli-test-2/test-repo-1` (404) →
  flagged manual + skipped.
- Org feature-flag WRITE = **PUT circleci.com** (verified 200; POST/app.circleci.com 404).
- `external_id` == GitHub repo numeric id (reconfirmed via GitHub API).

NOT yet live: the orb actually RUNNING in a pipeline (`install` + `extract-*` jobs)
— **blocked on the first binary release** (the orb's install downloads a released
binary, and no release exists yet). OAuth→OAuth live (new OAuth orgs may be
uncreatable — App is the real scenario; OAuth paths are unit-tested).

---

## 5. Key decisions (incl. chat-only)

- **Secret bundle**: storage-access controls only, **no encryption**; never echo
  secret values; minimize plaintext artifacts.
- **Sync is dry-run by default**, `--apply` to write. Missing secret value →
  `--missing-secrets skip|placeholder` (default skip).
- **Same-type migrations first** (OAuth→OAuth, App→App, mixed→mixed); cross-type
  OAuth→App is a follow-on (data-loss caveats: App never builds fork PRs; multiple
  pipeline-defs can't collapse to one OAuth config).
- **GitHub token** for repo `external_id` resolution in the dest GH org (repo IDs
  change when a repo moves GH orgs). Found→onboard, missing→flag+skip.
- **Orb**: namespace `awesomecicd` (temp), **private**; semver via tags. Restructured
  to ODK `src/` layout. `merge` job kept as optional (CLI aggregates client-side).
- **Release**: GoReleaser → GitHub Releases only (no registry yet). `goreleaser`
  CircleCI context (with `GITHUB_TOKEN`) + a `vX.Y.Z` tag are needed for the first
  release (user action). Don't publish publicly until the Labs move.
- **Orchestration model**: Opus orchestrator + Sonnet subagents for sub-tasks.

---

## 6. Open questions / pending decisions (from chat)

- **Make the repo PUBLIC now?** Pros: enables anonymous orb `install`, `go install`,
  and **brew** testing (no token friction); the repo is just a dev tool. Cons: the
  URL still changes on the CircleCI-Labs move regardless (module path + orb URL +
  goreleaser owner) — a known one-time find/replace. Leaning: **make it public** to
  unblock brew/install testing now. (GitHub setting change = user action.)
- **Brew**: only works against PUBLIC release URLs → requires the repo public. Then
  add a GoReleaser `brews:`/`homebrew_casks:` block targeting a tap repo (e.g.
  `CircleCI-Labs/homebrew-tap`) → `brew install`.
- **Semantic-version release automation**: GoReleaser does NOT decide the bump — it
  releases whatever tag exists. Use **release-please** or **semantic-release** to
  read Conventional Commits → compute major/minor/patch → create the tag →
  GoReleaser fires on the tag. We already use Conventional Commits, so adding
  release-please is straightforward. (Decide which; release-please is low-friction.)
- **README badges**: contains some orb-specific/templated badges that may not all
  apply — review + clean up.

---

## 7. Repo-owner / namespace cleanup needed (before/at the Labs move)

Final home = **`github.com/CircleCI-Labs/circleci-org-migrator`**(? confirm repo name)
in org **CircleCI-Labs**, orb namespace **`cci-labs`**. Currently references say
`CircleCI-Public` (orb-template/Go-module defaults) and `awesomecicd` (temp orb ns).
To fix on the move:
- `go.mod` module path `github.com/CircleCI-Public/circleci-org-migration-cli` →
  `github.com/CircleCI-Labs/<repo>` (+ all import paths).
- `orb/src/scripts/install.sh` + `resolve-version.sh` `repo="CircleCI-Public/..."`
  → the real release repo. **NOTE: today this is WRONG even for temp** — releases
  publish to **AwesomeCICD** (git remote) but the script downloads from
  CircleCI-Public → would 404. For temp testing, point at AwesomeCICD (+ a token,
  since the repo is INTERNAL) OR make the repo public.
- `.goreleaser.yml` infers owner from the remote (AwesomeCICD now) — fine; comments
  mention CircleCI-Public.
- Orb namespace `awesomecicd` → `cci-labs`; publish a **public** orb.

---

## 8. Next steps / backlog

1. **Cross-type OAuth→App translation** (the 3rd of three directions; shares the
   syncer App path): map OAuth build flags → App trigger `event_preset`, synthesize
   an App pipeline-def from an OAuth source; surface data-loss callouts.
2. **Orb test suite** (asked for): chunk sidecars (config validate, `orb pack` +
   `validate`, **shellcheck** on `orb/src/scripts`) + a CI orb-test workflow (
   `orb-tools/lint`, `orb-tools/review`, `shellcheck/check`, pack, process the
   matrix example) gated on `orb/` changes — so orb regressions fail BEFORE publish.
   Model on the Orb Development Kit test-deploy pipeline.
3. **First binary release**: user creates the `goreleaser` context (`GITHUB_TOKEN`);
   push `v0.1.0` → GoReleaser publishes binaries + `orb-publish-prod` publishes the
   semver orb. Unblocks `orb install` + the **functional** orb test (orb running).
4. **Full repo-move migrate e2e**: once a matching repo exists in the dest GH org.
5. **Borrow from the competitor** (see §9): the **inline private-orb swap +
   post-cutover revert** idea, and the **operational hand-off artifacts**.
6. Make the repo public + add brew + release-please (per §6 decisions).
7. Lower-priority audit gaps: SSO sub-fields (timeout/group-mappings/idp-bypass),
   org-group definitions, runner resource classes.
8. **Orb review findings — address at the CircleCI-Labs republish** (2026-06-09):
   `orb-tools/review` was removed from both `orb-publish-dev` and `orb-publish-prod`
   because it is advisory-only but CircleCI has no per-job allow-failure, so its
   non-zero exit falsely coloured the v0.2.0 `orb-publish-prod` workflow red even
   though the orb published successfully.  The three findings to act on later:

   - **RC010 — DONE (PR #TBD).** Renamed all kebab-case component names and
     parameters to snake_case (`extract_context`, `restore_cli`, `cache_cli`,
     `extract_project`; params `context_name`, `project_slug`, `install_dir`,
     `cache_key_prefix`, `force_install`, `cache_cli`). This is a breaking orb API
     change; acceptable for a pre-1.0 orb. `orb-tools/review` re-enabled in CI.
   - **RC011 — DONE (PR #TBD).** Example files updated from stale `@1.0.0` to
     `@volatile` (orb-tools convention for examples).
   - **Consider moving long inline shell to `<<include(scripts/...)>>` files.**
     The `extract_context` and `extract_project` commands contain sizeable inline
     shell blocks; extracting them to `orb/src/scripts/` and referencing them via
     `<<include(...)>>` would improve readability and `shellcheck` coverage (scripts
     in `orb/src/scripts/` are already checked by the `shellcheck/check` gate).

---

## 9. Competitive comparison — `CircleCI-Labs/circleci-org-migrator`

Audited 2026-06-09 (cloned to `/tmp/circleci-org-migrator`). It is a **toolkit**
(Python collectors + the official `terraform-provider-circleci` + Python gap-fill
scripts + an in-CI secret-transfer pipeline + a Makefile run in a documented 9-step
sequence + agent `SKILL.md` files). **Maturity: early-stage** — single commit
(2026-04-22), no tests, no CI, no releases; excellent docs; no evidence of live
validation.

**What THEY have that we don't (worth borrowing):**
1. **Inline private-orb swap with post-cutover revert** (`apply_inline_orb_swap.py`)
   — a namespace lives in only one org, so during overlap private orbs aren't
   resolvable in the target; they fetch the orb source and inline it into each
   consuming repo's config, then revert after the namespace transfer. **Borrow this.**
2. **Terraform-backed provisioning + state** (declarative plan/destroy/rollback).
3. **AI-agent `SKILL.md` packaging**; operational hand-off artifacts
   (external-pins template, secret-tracking workbook, org-settings checklist).
4. **CircleCI Server support** via `API_URL` override.

**What WE have that they don't:** org settings actually migrated (theirs is a
manual checklist); **project env-var VALUES** transferred (theirs is manual-fill
only); checkout/SSH keys exported; complete project discovery (theirs uses a 90-day
Insights window that misses dormant projects); automated cross-type + EMU repo-move
with `external_id` verification; group/expression context restrictions (theirs is
project-only); a tested single binary with CI + live validation; `secrets capture`
needing no committed config.

**Recommendation:** **Ours supersedes it for production** (broader/deeper coverage,
automation, maturity, live validation), but **borrow two ideas**: the inline
private-orb swap (fills a real overlap-window gap) and the operational hand-off
artifacts. The competitor is risky as a customer's primary tool due to no
org-settings migration, no project env-var value transfer, dormant-project blind
spots, and its untested single-commit state.

---

## 10. Quick orientation commands

```
make build            # build the binary
make verify           # fmt + vet + lint + test + coverage gate
make orb-validate     # pack orb/src + validate (private orb, --org-id)
go run . export --org circleci/<uuid> -o manifest.json
go run . migrate --source-org <slug> --dest-org <slug> --secrets secrets.json --apply --yes
```
Auth: `CIRCLECI_SOURCE_TOKEN` / `CIRCLECI_DEST_TOKEN` (fallback `CIRCLECI_CLI_TOKEN`,
read from `~/.circleci/cli.yml`), `GITHUB_TOKEN` for repo `external_id` resolution.
</content>
