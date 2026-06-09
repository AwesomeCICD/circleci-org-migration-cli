# Contributing

Thanks for helping improve `circleci-migrate`. This guide covers local
development, testing, and the checks that run in CI.

## Prerequisites

- Go 1.26+
- `make`
- (optional) the developer tools used by lint/security: `make tools`

## Common tasks

```bash
make build      # build ./bin/circleci-migrate
make test       # run unit tests with the race detector
make cover      # tests + coverage report (coverage.html) + threshold gate
make lint       # golangci-lint (run `make tools` first to install it)
make fmt        # gofmt the tree
make verify     # vet + gofmt check + test + build  (mirrors the CI lint/test/build stages)
make security   # govulncheck + gosec + gitleaks    (mirrors the CI security stage)
make ci-local   # everything CI runs, locally
make tools      # install golangci-lint, gosec, govulncheck, gitleaks
```

Run `make verify` before every push; run `make ci-local` for the full pipeline
equivalent.

## Testing & coverage

- Tests use the standard library `testing` package with `net/http/httptest`
  for API clients — no external test frameworks.
- API client packages mock the CircleCI API with `httptest.Server` and assert
  on the request method, path (`EscapedPath`), query, and headers.
- Coverage is gated in CI by `scripts/check-coverage.sh`. Override the floor
  with `COVERAGE_THRESHOLD=NN make cover`.

## Security scanning

CI runs three account-free scanners; reproduce them locally with `make security`:

- **govulncheck** — Go vulnerability DB, call-graph aware (only flags reachable
  vulnerabilities). Blocking.
- **gosec** — Go SAST (hardcoded creds, weak TLS, command/path injection, …).
  Blocking on high severity.
- **gitleaks** — committed-secret detection across git history. Blocking.

- **trivy** — filesystem scan (vuln + secret + misconfig) via the
  `cci-labs/trivy@1.0.0` orb in CI, pinned to trivy `v0.56.2`. Warn-only for
  now (flip the scan's `exit-code` to `1` to make it blocking). Run locally
  with `make trivy` (install a pinned trivy first).

## Fast inner-loop validation with chunk

[`chunk`](https://github.com/CircleCI-Public/chunk-cli) runs the same checks in
the inner loop (and can wire them into agent/editor hooks). This repo ships a
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

## Continuous integration

`.circleci/config.yml` runs on every push:

- **lint** — `go vet`, `gofmt` check, `golangci-lint`
- **test** — `gotestsum` (JUnit results) + coverage artifact + threshold gate
- **security** — `govulncheck`, `gosec`, `gitleaks`
- **build** — a GOOS×GOARCH matrix (linux/darwin × amd64/arm64), gated on
  lint + test, publishing binaries as artifacts

## Commits

Use small, atomic [Conventional Commits](https://www.conventionalcommits.org/)
(`feat:`, `fix:`, `chore:`, `docs:`, …). Keep customer-facing language clear and
professional — this is a customer-run tool.
