default: build

BINARY     := circleci-migrate
GOOS       := $(shell go env GOOS)
GOARCH     := $(shell go env GOARCH)
OUTPUT     := bin/$(BINARY)

.PHONY: build
build:
	go build -o $(OUTPUT) .

.PHONY: test
test:
	go test ./...

.PHONY: vet
vet:
	go vet ./...

.PHONY: fmt
fmt:
	gofmt -w .

.PHONY: tidy
tidy:
	go mod tidy

.PHONY: lint
lint:
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run; \
	else \
		echo "golangci-lint not found — skipping (install from https://golangci-lint.run/usage/install/)"; \
	fi

.PHONY: snapshot
snapshot:
	@if command -v goreleaser >/dev/null 2>&1; then \
		goreleaser release --snapshot --clean; \
	else \
		echo "goreleaser not found — skipping (install from https://goreleaser.com/install/)"; \
	fi

.PHONY: clean
clean:
	rm -rf bin/ dist/
