default: build

BINARY   := circleci-migrate
GOOS     := $(shell go env GOOS)
GOARCH   := $(shell go env GOARCH)
OUTPUT   := bin/$(BINARY)

# Pinned tool versions (kept in sync with .circleci/config.yml).
GOLANGCI_VERSION := v2.12.2
GOSEC_VERSION    := v2.27.1
GITLEAKS_VERSION := 8.30.1

GOBIN := $(shell go env GOPATH)/bin

.PHONY: build
build:
	go build -o $(OUTPUT) .

.PHONY: test
test:
	go test -race ./...

.PHONY: vet
vet:
	go vet ./...

.PHONY: fmt
fmt:
	gofmt -w .

.PHONY: fmt-check
fmt-check:
	@unformatted=$$(gofmt -l .); \
	if [ -n "$$unformatted" ]; then \
		echo "These files are not gofmt-clean:"; echo "$$unformatted"; exit 1; \
	fi

.PHONY: tidy
tidy:
	go mod tidy

.PHONY: cover
cover:
	go test -race -covermode=atomic -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	go tool cover -func=coverage.out | tail -1
	./scripts/check-coverage.sh coverage.out

.PHONY: lint
lint:
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run --timeout 5m; \
	else \
		echo "golangci-lint not found — run 'make tools' (or see https://golangci-lint.run/)"; exit 1; \
	fi

# verify mirrors the CircleCI lint+test+build stages for fast local validation.
.PHONY: verify
verify: vet fmt-check test build

# security mirrors the CircleCI security stage. Tools are installed on demand.
.PHONY: security
security:
	@echo "==> govulncheck"; \
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...
	@echo "==> gosec"; \
	go run github.com/securego/gosec/v2/cmd/gosec@$(GOSEC_VERSION) -severity high ./...
	@echo "==> gitleaks"; \
	if command -v gitleaks >/dev/null 2>&1; then \
		gitleaks detect --source=. --redact --exit-code=1; \
	else \
		echo "gitleaks not found — run 'make tools' to install it"; exit 1; \
	fi

.PHONY: config-validate
config-validate:
	@if command -v circleci >/dev/null 2>&1; then \
		circleci config validate .circleci/config.yml; \
	else \
		echo "circleci CLI not found — install from https://circleci.com/docs/local-cli/"; exit 1; \
	fi

# trivy runs the same filesystem scan as the CI security-trivy job (cci-labs
# Trivy orb pins trivy v0.56.2). Install a pinned trivy locally to match.
.PHONY: trivy
trivy:
	@if command -v trivy >/dev/null 2>&1; then \
		trivy fs --scanners vuln,secret,misconfig --severity HIGH,CRITICAL .; \
	else \
		echo "trivy not found — install a pinned version (CI uses trivy v0.56.2)"; exit 1; \
	fi

# ci-local runs the full local equivalent of the CircleCI pipeline.
.PHONY: ci-local
ci-local: verify cover security config-validate

# tools installs the developer tooling used by lint/security locally.
.PHONY: tools
tools:
	curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/HEAD/install.sh \
		| sh -s -- -b "$(GOBIN)" $(GOLANGCI_VERSION)
	go install github.com/securego/gosec/v2/cmd/gosec@$(GOSEC_VERSION)
	go install golang.org/x/vuln/cmd/govulncheck@latest
	curl -sfL "https://github.com/gitleaks/gitleaks/releases/download/v$(GITLEAKS_VERSION)/gitleaks_$(GITLEAKS_VERSION)_$(GOOS)_$(shell echo $(GOARCH) | sed 's/amd64/x64/')."* \
		| tar -xz -C "$(GOBIN)" gitleaks || echo "gitleaks install: adjust platform if this failed"

.PHONY: snapshot
snapshot:
	@if command -v goreleaser >/dev/null 2>&1; then \
		goreleaser release --snapshot --clean; \
	else \
		echo "goreleaser not found — see https://goreleaser.com/install/"; \
	fi

# release-snapshot builds all cross-platform archives locally without publishing.
# Useful for validating the GoReleaser config and archive naming before tagging.
.PHONY: release-snapshot
release-snapshot:
	goreleaser release --snapshot --clean --skip=publish

# release-check validates .goreleaser.yml against the GoReleaser v2 schema.
.PHONY: release-check
release-check:
	goreleaser check

# orb-validate runs the CircleCI CLI orb linter against the single-file orb.
# Mirrors the validate step in the CI orb-publish job.
.PHONY: orb-validate
orb-validate:
	@if command -v circleci >/dev/null 2>&1; then \
		circleci orb validate orb/orb.yml; \
	else \
		echo "circleci CLI not found — install from https://circleci.com/docs/local-cli/"; exit 1; \
	fi

# orb-publish-dev validates then publishes a dev-labelled version of the orb
# for manual / local testing. Requires CIRCLE_TOKEN to be set in your shell.
# The label includes a Unix timestamp so successive publishes don't collide.
.PHONY: orb-publish-dev
orb-publish-dev: orb-validate
	circleci orb publish orb/orb.yml \
		awesomecicd/circleci-org-migration@dev:manual-$$(date +%s) \
		--token "$$CIRCLE_TOKEN"

.PHONY: clean
clean:
	rm -rf bin/ dist/ coverage.out coverage.html test-results/ *.sarif
