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

# ci-local runs the full local equivalent of the CircleCI pipeline.
.PHONY: ci-local
ci-local: verify cover security

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

.PHONY: clean
clean:
	rm -rf bin/ dist/ coverage.out coverage.html test-results/ *.sarif
