.PHONY: all build install install-dev test vet generate lint gosec coverage clean devcontainer-build test-linux test-integration test-all

all: build vet test

# Version metadata for `make build` — mirrors .goreleaser.yml so dev binaries
# report a meaningful version. `make install` instead delegates to goreleaser
# so the installed binary matches release artifacts exactly.
VERSION   ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT    ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
BUILD_DATE ?= $(shell git log -1 --format=%cI 2>/dev/null || echo unknown)
LDFLAGS := -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(BUILD_DATE)

GOPATH_BIN := $(shell go env GOPATH)/bin

build:
	go build -ldflags "$(LDFLAGS)" -o bin/aide ./cmd/aide

# Install via goreleaser so the binary matches release artifacts. goreleaser
# refuses to run on a dirty tree without --snapshot, which gives us the
# dirty-commit guard for free.
install:
	@if ! command -v goreleaser >/dev/null 2>&1; then \
		echo "error: goreleaser not installed; see https://goreleaser.com/install/"; \
		exit 1; \
	fi
	goreleaser build --single-target --clean --output bin/aide
	install -m 0755 bin/aide $(GOPATH_BIN)/aide
	@echo "installed: $(GOPATH_BIN)/aide"

# Fast local install for iteration. Skips goreleaser (and its dirty-tree
# guard), uses the same ldflags as `make build`. Version string carries the
# `-dirty` suffix from `git describe --dirty` when the tree is dirty, so the
# installed binary still reports its provenance.
install-dev:
	go build -ldflags "$(LDFLAGS)" -o bin/aide ./cmd/aide
	install -m 0755 bin/aide $(GOPATH_BIN)/aide
	@echo "installed (dev): $(GOPATH_BIN)/aide ($(VERSION))"

test:
	go test ./...

test-integration:
	go test -tags integration ./...

vet:
	go vet ./...

generate:
	go generate ./...

lint:
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run ./...; \
	else \
		echo "golangci-lint not installed, skipping lint"; \
	fi

# gosec rules excluded per .gosec.yaml (single source of truth)
GOSEC_EXCLUDE := $(shell yq -r '.exclude | keys | join(",")' .gosec.yaml 2>/dev/null)

gosec:
	@if ! command -v gosec >/dev/null 2>&1; then \
		echo "gosec not installed, skipping security scan"; \
	elif [ -z "$(GOSEC_EXCLUDE)" ]; then \
		echo "warning: could not read .gosec.yaml (is yq installed?), running gosec without exclusions"; \
		gosec ./...; \
	else \
		gosec -exclude=$(GOSEC_EXCLUDE) ./...; \
	fi

# Run tests with coverage and enforce thresholds from .testcoverage.yml
GOBIN := $(shell pwd)/.gobin
coverage:
	go test -race -coverprofile=coverage.out ./...
	@GOBIN=$(GOBIN) go install github.com/vladopajic/go-test-coverage/v2@latest
	$(GOBIN)/go-test-coverage --config .testcoverage.yml

clean:
	rm -rf bin/ coverage.out

# Devcontainer for Linux testing (not the aide application image)
devcontainer-build:
	docker build -t aide-devcontainer -f .devcontainer/Dockerfile .

# Run the full test suite inside the Linux devcontainer
# This is needed for Linux-specific code (Landlock, bwrap) that can't run on macOS
test-linux: devcontainer-build
	@if command -v docker >/dev/null 2>&1 && docker info >/dev/null 2>&1; then \
		docker run --rm --privileged \
			-v $(PWD):/workspace -w /workspace \
			aide-devcontainer make all test-integration; \
	else \
		echo "Docker not available, skipping Linux tests"; \
	fi

# Run everything: native tests + Linux container tests
test-all: all test-linux
