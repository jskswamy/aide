.PHONY: all build install test vet lint clean test-integration

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

lint:
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run ./...; \
	else \
		echo "golangci-lint not installed, skipping lint"; \
	fi

clean:
	rm -rf bin/
