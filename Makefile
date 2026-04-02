.PHONY: all build install test vet generate lint gosec clean devcontainer-build test-linux test-integration test-all

all: build vet test

build:
	go build -o bin/aide ./cmd/aide

install:
	go install ./cmd/aide

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

clean:
	rm -rf bin/

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
