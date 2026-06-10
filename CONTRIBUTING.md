# Contributing to circleci-migrate

Thank you for helping improve `circleci-migrate`. This guide covers local
development setup, the test and coverage requirements, the security checks that
run in CI, orb development, commit conventions, PR norms, and the automated
release process.

---

## Prerequisites

| Tool | Version | Notes |
|---|---|---|
| Go | 1.26+ | The module's minimum version; match the `go` directive in `go.mod`. |
| `make` | any | All common tasks are wrapped in `Makefile` targets. |
| CircleCI CLI | latest | Required for orb development (`orb-pack`, `orb-validate`, `orb-publish-dev`). Install via https://circleci.com/docs/local-cli/ |
| Developer tools | see below | Install once with `make tools`. |

Install the developer tooling that lint and security targets depend on:

```bash
make tools
```

This installs the following at the versions pinned in the `Makefile`:

- **golangci-lint** (currently `v2.12.2`) — meta-linter for Go.
- **gosec** (currently `v2.27.1`) — Go SAST (hardcoded creds, weak TLS, etc.).
- **govulncheck** (latest) — call-graph-aware Go vulnerability scanner.
- **gitleaks** (currently `v8.30.1`) — committed-secret detector.

Versions in the `Makefile` are kept in sync with `.circleci/continue_config.yml`
via Renovate. When Renovate bumps a version, update both files together.

---

## Local development

### Building

```bash
make build          # produces ./bin/circleci-migrate
```

Or without `make`:

```bash
go build -o bin/circleci-migrate .
```

### Running the full local validation suite

```bash
make verify         # go vet + gofmt check + tests + build (fast, run before every push)
make ci-local       # everything CI runs: verify + cover + security + config-validate
```

### All make targets

| Target | What it does |
|---|---|
| `make build` | Compile `./bin/circleci-migrate` for the current OS/arch. |
| `make test` | Run the full test suite with the race detector (`go test -race ./...`). |
| `make vet` | Run `go vet ./...`. |
| `make fmt` | Auto-format all Go source files with `gofmt`. |
| `make fmt-check` | Check gofmt without writing (used by CI lint job). |
| `make tidy` | Run `go mod tidy`. |
| `make cover` | Tests + coverage profile + HTML report + threshold gate. |
| `make lint` | Run `golangci-lint run --timeout 5m` (install first: `make tools`). |
| `make verify` | `vet` + `fmt-check` + `test` + `build` — the fast inner-loop gate. |
| `make security` | `govulncheck` + `gosec` + `gitleaks` — mirrors the CI security stage. |
| `make config-validate` | Validate `.circleci/config.yml` with the CircleCI CLI. |
| `make trivy` | Filesystem vuln/secret/misconfig scan (install a pinned trivy first). |
| `make ci-local` | `verify` + `cover` + `security` + `config-validate` — full CI equivalent. |
| `make tools` | Install `golangci-lint`, `gosec`, `govulncheck`, `gitleaks`. |
| `make snapshot` | Local GoReleaser snapshot build (all platforms, no publish). |
| `make release-snapshot` | Same as snapshot but explicitly skips publish. |
| `make release-check` | Validate `.goreleaser.yml` against the GoReleaser v2 schema. |
| `make orb-pack` | Pack `orb/src/` into `orb/orb.yml` (`circleci orb pack`). |
| `make orb-validate` | Pack + validate the orb against the private namespace. |
| `make orb-shellcheck` | Run `shellcheck` against `orb/src/scripts/*.sh`. |
| `make orb-test` | `orb-validate` + `orb-shellcheck` — full local orb regression. |
| `make orb-publish-dev` | Pack, validate, and publish a dev-labelled orb for manual testing. |
| `make clean` | Remove `bin/`, `dist/`, `coverage.out`, `coverage.html`, test results. |

---

## Testing and coverage

### Test approach

- Tests use the standard library `testing` package — no third-party test frameworks.
- API client packages (`api/context`, `api/org`, `api/project`, `api/pipeline`)
  mock the CircleCI API using `net/http/httptest.NewServer`. Each test asserts on
  the HTTP method, `EscapedPath()`, query parameters, and `Circle-Token` header.
- The `internal/exporter` and `internal/syncer` packages test end-to-end
  behaviour using fake API servers that replicate real CircleCI responses.
- Run with the race detector enabled: `go test -race ./...`.

### Coverage gate

The `cover` target runs the full test suite, generates an HTML report, and
enforces a minimum total coverage threshold:

```bash
make cover
```

The current threshold is **85%**. The gate is enforced by
`scripts/check-coverage.sh`:

```
scripts/check-coverage.sh [coverage-profile] [threshold-percent]
```

Threshold resolution order: CLI argument → `$COVERAGE_THRESHOLD` env var →
default (85). Override locally for testing:

```bash
COVERAGE_THRESHOLD=85 make cover
```

When adding new code, include tests that keep total coverage above the threshold.
CI will fail the `coverage` job if it drops below the gate.

---

## Linting

CI runs three linting steps (all in the `lint` job):

1. **`go vet`** — standard Go tool; flags suspicious constructs. Blocking.
2. **`gofmt`** — code formatting. Files must be `gofmt`-clean before merge.
   Auto-fix with `make fmt`; check only with `make fmt-check`.
3. **`golangci-lint`** — meta-linter. Configuration lives in `.golangci.yml`.
   Run with `make lint` (after `make tools` installs the pinned version).

Run all three at once:

```bash
make verify
```

The `golangci-lint` version is pinned in the `Makefile` and in
`.circleci/continue_config.yml`. When updating, change both files in the same
commit.

---

## Security scanning

CI runs four scanners in the security stage. Reproduce them locally:

```bash
make security       # govulncheck + gosec + gitleaks
make trivy          # filesystem scan (separate — install trivy first)
```

| Scanner | What it checks | Blocking in CI? |
|---|---|---|
| **govulncheck** | Go vulnerability database, call-graph-aware (only flags reachable vulns). | Yes |
| **gosec** | Go SAST: hardcoded credentials, weak TLS, command injection, path traversal, etc. Runs at `high` severity. | Yes (high severity) |
| **gitleaks** | Committed-secret detection across the full git history. | Yes |
| **trivy** | Filesystem scan: vulnerabilities, secrets, misconfigurations (HIGH/CRITICAL). | Warn-only for now |

### gitleaks allowlist policy

The `.gitleaks.toml` file extends the upstream default ruleset and adds a small
allowlist for test fixture files. The allowlist is for files that contain
**deliberately fake, non-secret** data that triggers entropy or keyword heuristics
(for example, SSO test fixtures with synthetic SAML metadata field names).

When adding a new allowlist entry:

1. Justify it in the `description` field — explain exactly why the path is safe.
2. Use a regex anchored to the specific test file (e.g. `internal/exporter/sso_export_test\.go`).
3. Keep the allowlist minimal. Do not add broad directory globs.
4. Never add an entry to suppress a real secret — fix the secret instead.

---

## Fast inner-loop validation with chunk

[`chunk`](https://github.com/CircleCI-Public/chunk-cli) runs the same checks in
the inner loop and can wire them into agent or editor hooks. This repo ships a
`.chunk/config.json` mapping `chunk validate` to the `make` targets above,
split by role so the inner loop stays fast:

- **precheck** (fast, every change): `vet`, `build`, `test-changed` (only the
  changed packages), plus `format` as an autofix.
- **gate** (before push): `lint`, full `test`, `security`, `config-validate`.

Each command has a `timeout`; tune them in `.chunk/config.json`.

```bash
brew install CircleCI-Public/circleci/chunk
chunk validate           # run all configured checks locally
chunk validate test      # run a single check
chunk validate --list    # list configured checks
chunk init               # (optional) wire pre-commit / agent hooks
```

`chunk` runs the configured shell commands locally; it does not execute the
`.circleci/config.yml` jobs. For a faithful local run of the actual pipeline
jobs, `circleci local execute` is an option (Docker executor only, no caching).

---

## Orb development

The `awesomecicd/circleci-org-migration` orb lives in `orb/src/` (Orb
Development Kit layout). The packed single-file form (`orb/orb.yml`) is
`.gitignore`d — `orb/src/` is the source of truth.

### Layout

```
orb/src/
  @orb.yml                    # orb metadata (version, description, display)
  commands/                   # reusable commands (snake_case names)
  jobs/                       # reusable jobs (snake_case names)
    extract_context.yml
    extract_project.yml
    merge.yml
  executors/
    default.yml
  examples/                   # usage examples shown in the orb registry
    capture-context-secrets.yml
    capture-context-secrets-matrix.yml
  scripts/                    # shell scripts sourced by commands/jobs
    extract-context.sh
    extract-project.sh
    install.sh
    resolve-version.sh
```

### Naming conventions (RC010 rule)

All orb component names and parameter names **must use `snake_case`**. The
`orb-tools/review` RC010 rule enforces this and will fail the CI orb-test gate
if `camelCase` or `kebab-case` names are used. Examples:

- Job: `extract_context` (correct), not `extractContext` or `extract-context`
- Parameter: `context_name` (correct), not `contextName` or `context-name`
- Command: `cache_cli` (correct), not `cacheCli` or `cache-cli`

### Example versions (RC011 rule)

All examples in `orb/src/examples/` must reference a published orb version
(e.g. `awesomecicd/circleci-org-migration@0.2.0` or `@volatile`), not an
unpublished dev label. The `orb-tools/review` RC011 rule enforces this.

### Local orb development workflow

```bash
# 1. Pack and validate (mirrors the CI gate)
make orb-test

# 2. Publish a dev label for manual testing (requires CIRCLE_TOKEN in env)
make orb-publish-dev
# Publishes: awesomecicd/circleci-org-migration@dev:manual-<timestamp>

# 3. Pack only (inspect the packed YAML without validating)
make orb-pack
```

`--org-id efc130dc-284f-4533-964e-844f5c173860` is required for `orb validate`
because `awesomecicd/circleci-org-migration` is a private orb. The Makefile
`orb-validate` target passes this automatically.

### CI orb pipeline

The orb pipeline is triggered by the `circleci/path-filtering` setup orb when
any file under `orb/` changes. It runs:

1. **`orb-tools/lint`** — orb YAML structure and naming conventions.
2. **`orb-validate`** (our pack + validate job) — authoritative validation gate.
3. **`shellcheck/check`** — lints shell scripts in `orb/src/scripts/`.
4. **`orb-tools/review`** — RC010 (snake_case) and RC011 (example versions).
5. **`publish-orb` (dev)** — publishes a SHA-labelled dev version if all gates pass.

On a version tag push, the `orb-publish-prod` workflow fires and publishes a
semver release.

---

## Commit conventions

This repository uses [Conventional Commits](https://www.conventionalcommits.org/).
Commit messages feed `release-please`, which computes version bumps and generates
`CHANGELOG.md`.

### Format

```
<type>(<scope>): <short description>

[optional body]

[optional footer(s)]
```

### Types

| Type | Version bump | When to use |
|---|---|---|
| `feat` | minor | New user-facing feature or command. |
| `fix` | patch | Bug fix. |
| `chore` | none | Maintenance: dependency bumps, CI config, tooling. |
| `docs` | none | Documentation only. |
| `refactor` | none | Code restructuring without behaviour change. |
| `test` | none | Adding or updating tests. |
| `perf` | patch | Performance improvement. |
| `BREAKING CHANGE` (footer) | major | Breaking API or CLI change. |

### Examples

```
feat(sync): add --dest-github-org flag for cross-GitHub-org migrations

fix(secrets): handle context restriction 403 gracefully in capture mode

docs: add end-to-end migration AI skill + flesh out CONTRIBUTING

chore(deps): bump golangci-lint from v2.11.0 to v2.12.2
```

Keep commits small and atomic. Squash "WIP" commits before opening a PR.
Use clear, professional language — this is a customer-run tool and commit
messages appear in the generated changelog.

---

## Pull request norms

- **Small, focused PRs.** A PR should do one thing and do it completely. Large
  multi-concern PRs are harder to review and harder to revert.
- **All CI jobs must be green** before requesting review. Check the CircleCI
  status badge in the README.
- **No skipped tests.** If a test must be skipped temporarily, add a TODO with
  a linked issue and an expected resolution date.
- **Keep `go.mod` / `go.sum` tidy.** Run `make tidy` if you add or remove
  dependencies and commit both files together.
- **Update docs alongside code.** If a flag, command, or behaviour changes,
  update the README command reference and the relevant `docs/*.md` file in the
  same PR.
- **Request review from at least one maintainer** before merging.

---

## Continuous integration

`.circleci/config.yml` is a **setup pipeline** that uses the
`circleci/path-filtering` orb to decide which downstream workflows fire.
All jobs and workflows live in `.circleci/continue_config.yml`.

### Workflow stages (CI workflow — every branch push)

```
Stage 1 (parallel, no prerequisites):
  lint              go vet + gofmt check + golangci-lint
  security-vuln     govulncheck
  security-sast     gosec (JUnit + SARIF artifacts)
  security-secrets  gitleaks (SARIF artifact)
  security-trivy    trivy filesystem scan (warn-only)

Stage 2 (requires all of Stage 1):
  test              gotestsum with parallelism=2 (JUnit results + HTML report)
  coverage          full suite coverage + threshold gate + HTML artifact
  e2e               binary smoke tests (version, help, secrets extract fixture)

Stage 3 (requires test):
  build             matrix: linux/darwin × amd64/arm64 (binary artifacts)
```

### Orb-triggered workflows

When any file under `orb/` changes, `path-filtering` sets `run-orb-publish=true`,
which activates the `orb-publish-dev` workflow:

```
orb-lint-dev + orb-validate-dev + orb-shellcheck-dev + orb-review-dev
  → publish-orb-dev (dev label: SHA-prefixed)
```

On a version tag push (`v*`), the `orb-publish-prod` workflow fires:

```
orb-lint-prod + orb-validate-prod + orb-shellcheck-prod + orb-review-prod
  → publish-orb-prod (semver from tag)
```

---

## Release process

Releases are fully automated via [release-please](https://github.com/googleapis/release-please)
and [GoReleaser](https://goreleaser.com/), driven by Conventional Commits.

### Flow

```
1. Conventional Commits land on main
      ↓
2. release-please (CircleCI `release-please` workflow, main-branch only) opens
   or updates a "release PR"
   - Computes the SemVer bump from commit types
     (feat → minor, fix → patch, BREAKING CHANGE footer → major)
   - Updates CHANGELOG.md
   - Bumps the version in .release-please-manifest.json
      ↓
3. Maintainer reviews and merges the release PR
      ↓
4. release-please (CircleCI, triggered by the merge commit on main) creates the
   git tag and a bare GitHub Release (no binaries yet)
      ↓
5. CircleCI `release` workflow fires on the new tag (v* filter)
   - Installs GoReleaser at the pinned version (v2.9.0)
   - `goreleaser release --clean` builds cross-platform binaries:
       linux_amd64, linux_arm64, darwin_amd64, darwin_arm64
   - APPENDS archives + checksums to the GitHub Release (mode: append)
   - Publishes the Homebrew formula to AwesomeCICD/homebrew-tap → Formula/
      ↓
6. CircleCI `orb-publish-prod` workflow fires on the same tag
   - Packs, validates, and publishes the orb as a semver release:
       awesomecicd/circleci-org-migration@<version>
```

### Key notes

- **release-please owns version bumps and the changelog.** GoReleaser builds
  and appends binary artifacts only; it does not generate its own changelog
  (`changelog.disable: true` in `.goreleaser.yml`).
- **GoReleaser is in `append` mode** (`release.mode: append`). It adds binary
  archives to the release that release-please already created, rather than
  creating its own.
- **Homebrew formula** is published to `AwesomeCICD/homebrew-tap`
  (`Formula/circleci-migrate.rb`). This requires a push token for the tap repo
  configured in the `goreleaser` CircleCI context. If the token is absent,
  GoReleaser skips the formula upload gracefully (`skip_upload: auto`).
- **Archive naming** (`circleci-migrate_{version}_{os}_{arch}.tar.gz`) is fixed
  and must not be changed — the orb's install script constructs the download URL
  using this exact pattern.
- **Namespace future:** the repo, tap, and orb will move from `AwesomeCICD` /
  `awesomecicd` to `CircleCI-Labs` / `cci-labs` when the tool is published under
  CircleCI Labs. All naming is documented in the README.

### Required CI contexts

| Context name | Secret | Used by |
|---|---|---|
| `goreleaser` | `GITHUB_TOKEN` | release-please job (contents:write + pull-requests:write) **and** GoReleaser release job (repo + tap write access). |
| `orb-publishing` | `CIRCLE_TOKEN` | Orb dev and prod publish jobs. |

> **Token scopes for release-please:** the `GITHUB_TOKEN` in the `goreleaser`
> context must have **`contents:write`** (to push the version-bump commit and
> create the git tag) and **`pull-requests:write`** (to open / update the
> release PR).  A classic PAT with `repo` scope satisfies both.  A fine-grained
> PAT needs both permissions granted explicitly.
