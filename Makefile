.PHONY: all build install install-dev vet generate lint clean devcontainer-build help \
	test/unit test/race test/coverage test/sandbox test/integration test/linux test/all \
	security/gosec security/vuln

# Version metadata for `make build` — mirrors .goreleaser.yml so dev binaries
# report a meaningful version. `make install` instead delegates to goreleaser
# so the installed binary matches release artifacts exactly.
VERSION    ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT     ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
BUILD_DATE ?= $(shell git log -1 --format=%cI 2>/dev/null || echo unknown)
LDFLAGS    := -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(BUILD_DATE)

GOPATH_BIN := $(shell go env GOPATH)/bin
GOBIN      := $(shell pwd)/.gobin

# gosec rules excluded per .gosec.yaml (single source of truth)
GOSEC_EXCLUDE := $(shell yq -r '.exclude | keys | join(",")' .gosec.yaml 2>/dev/null)
GOSEC_SARIF   := $(if $(filter 1,$(SARIF)),-fmt sarif -out gosec-results.sarif)

help: ## Show this help
	@awk 'BEGIN {FS = ":.*?## "}; \
		/^[a-zA-Z_\/%-]+:.*?## / {printf "  \033[36m%-22s\033[0m %s\n", $$1, $$2}' \
		$(MAKEFILE_LIST)

# ── Build ─────────────────────────────────────────────────────────────────────

all: build vet test/unit  ## Build, vet, and test

build:       ## Build the aide binary to bin/aide
	go build -ldflags "$(LDFLAGS)" -o bin/aide ./cmd/aide

# Install via goreleaser so the binary matches release artifacts. goreleaser
# refuses to run on a dirty tree without --snapshot, which gives us the
# dirty-commit guard for free.
install:     ## Install via goreleaser (matches release binary exactly)
	@if ! command -v goreleaser >/dev/null 2>&1; then \
		echo "error: goreleaser not installed; see https://goreleaser.com/install/"; \
		exit 1; \
	fi
	goreleaser build --single-target --clean --output bin/aide
	install -m 0755 bin/aide $(GOPATH_BIN)/aide
	@echo "installed: $(GOPATH_BIN)/aide"

install-dev: ## Install quickly without goreleaser (dev iteration)
	go build -ldflags "$(LDFLAGS)" -o bin/aide ./cmd/aide
	install -m 0755 bin/aide $(GOPATH_BIN)/aide
	@echo "installed (dev): $(GOPATH_BIN)/aide ($(VERSION))"

# ── Test ──────────────────────────────────────────────────────────────────────

test/unit:        ## Run unit tests with race detector
	go test -race ./...

test/race:        ## Run tests with race detector (alias for test/unit)
	go test -race ./...

test/coverage:    ## Run tests with coverage and enforce thresholds
	go test -race -coverprofile=coverage.out ./...
	@GOBIN=$(GOBIN) go install github.com/vladopajic/go-test-coverage/v2@latest
	$(GOBIN)/go-test-coverage --config .testcoverage.yml

test/sandbox:     ## Run Linux sandbox unit tests (no integration tag)
	go test -race ./internal/sandbox/ -v \
		-run "TestDetect|TestCompute|TestPlatform|TestDeriveGrantedPathSet|TestDerivePortPolicy"

test/integration: ## Run integration tests (requires -tags integration)
	go test -tags integration ./...

devcontainer-build: ## Build the Linux devcontainer image
	docker build -t aide-devcontainer -f .devcontainer/Dockerfile .

test/linux: devcontainer-build  ## Run full suite inside Linux devcontainer
	@if command -v docker >/dev/null 2>&1 && docker info >/dev/null 2>&1; then \
		docker run --rm --privileged \
			-v $(PWD):/workspace -w /workspace \
			aide-devcontainer make all test/integration; \
	else \
		echo "Docker not available, skipping Linux tests"; \
	fi

test/all: all test/linux  ## Run everything: native tests + Linux container tests

# ── Quality ───────────────────────────────────────────────────────────────────

vet:      ## Run go vet
	go vet ./...

generate: ## Regenerate mocks and other generated code
	go generate ./...

lint:     ## Run golangci-lint
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run ./...; \
	else \
		echo "golangci-lint not installed, skipping lint"; \
	fi

# ── Security ──────────────────────────────────────────────────────────────────

security/gosec: ## Run gosec scan (SARIF=1 for CI output)
	@if ! command -v gosec >/dev/null 2>&1; then \
		echo "gosec not installed, skipping security scan"; \
	elif [ -z "$(GOSEC_EXCLUDE)" ]; then \
		echo "warning: could not read .gosec.yaml (is yq installed?), running gosec without exclusions"; \
		gosec $(GOSEC_SARIF) ./...; \
	else \
		gosec -exclude=$(GOSEC_EXCLUDE) $(GOSEC_SARIF) ./...; \
	fi

security/vuln: ## Run govulncheck for known CVEs
	@GOBIN=$(GOBIN) go install golang.org/x/vuln/cmd/govulncheck@latest
	$(GOBIN)/govulncheck ./...

# ── Utility ───────────────────────────────────────────────────────────────────

clean: ## Remove build artifacts and coverage output
	rm -rf bin/ coverage.out
