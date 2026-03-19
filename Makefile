.PHONY: all build test vet lint clean devcontainer-build test-linux test-integration test-all

all: build vet test

build:
	go build -o bin/aide ./cmd/aide

test:
	go test ./...

test-integration:
	go test -tags integration ./...

vet:
	go vet ./...

lint:
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run ./...; \
	else \
		echo "golangci-lint not installed, skipping lint"; \
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
		docker run --rm --security-opt seccomp=unconfined \
			-v $(PWD):/workspace -w /workspace \
			aide-devcontainer make all test-integration; \
	else \
		echo "Docker not available, skipping Linux tests"; \
	fi

# Run everything: native tests + Linux container tests
test-all: all test-linux
