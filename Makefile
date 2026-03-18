.PHONY: all build test vet lint clean

all: build vet test

build:
	go build -o bin/aide ./cmd/aide

test:
	go test ./...

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
