# Testcontainers Linux Integration Tests

**Date:** 2026-03-23
**Status:** Draft
**Type:** Testing infrastructure

## Problem

We develop on macOS but CI runs on Linux. The linux sandbox uses
bwrap (namespace isolation) + Landlock (file access control + port
filtering) as a complementary pair. These are invisible to the
macOS compiler and toolchain:

- `golangci-lint` on macOS never sees `linux.go` — functions shared
  across platforms appear "unused" and get deleted
- `go test` on macOS never runs `linux_test.go` or
  `linux_integration_test.go` — regressions go undetected until CI
- `gosec` on macOS misses security findings in `linux.go`

The current workaround (cross-compile lint/gosec with
`GOOS=linux`) catches static analysis gaps but cannot run tests.

## Solution

Use `testcontainers-go` to run the full linux sandbox test suite
from macOS during local development and preflight checks.

## Design

### Container Image

A minimal Dockerfile purpose-built for running linux sandbox tests.
Located at `internal/sandbox/testdata/Dockerfile`:

```dockerfile
FROM golang:1.25-bookworm

RUN apt-get update && apt-get install -y --no-install-recommends \
    bubblewrap \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /workspace
```

This provides:
- **Go toolchain** — compile and run tests inside the container
- **bubblewrap** — namespace isolation (network, mount, PID)
- **Landlock** — provided by Docker Desktop's VM kernel (6.x)

No other tools needed. This is a test runner, not a dev environment.

### Test Helper

A shared helper in `internal/sandbox/linuxtest_helper_test.go`
(build-tagged `!linux`) that:

1. Starts a container from the Dockerfile
2. Bind-mounts the project source into `/workspace`
3. Runs `go test` inside the container targeting the linux test files
4. Streams output back to the host test runner
5. Fails the host test if container tests fail

```go
//go:build !linux

package sandbox

// RunLinuxTests starts a testcontainer and runs the linux sandbox
// tests inside it. Called from darwin/other platform tests so that
// linux tests execute as part of the normal test suite.
func RunLinuxTests(t *testing.T, args ...string) {
    // 1. Build container from testdata/Dockerfile
    // 2. Bind-mount project root
    // 3. Run: go test -tags linux,integration ./internal/sandbox/...
    // 4. Stream stdout/stderr to t.Log
    // 5. Fail t if exit code != 0
}
```

### Test File Structure

```
internal/sandbox/
├── sandbox.go                      # shared types + helpers
├── darwin.go                       # macOS: Seatbelt
├── linux.go                        # Linux: bwrap + Landlock
├── sandbox_other.go                # no-op fallback
├── darwin_test.go                  # macOS unit tests
├── linux_test.go                   # Linux unit tests (run in container)
├── linux_integration_test.go       # Linux integration tests (run in container)
├── linux_container_test.go         # NEW: host-side test that launches container
└── testdata/
    └── Dockerfile                  # NEW: test container image
```

`linux_container_test.go` (build-tagged `!linux`):
- Contains `TestLinuxSandbox_ViaContainer(t *testing.T)`
- Uses `RunLinuxTests` helper to run all linux tests in a container
- Skips if Docker is not available (`testcontainers.SkipIfProviderIsNotHealthy`)
- Runs as part of `go test ./internal/sandbox/...` on macOS

### What Runs Inside the Container

All existing linux tests, unmodified:

**Unit tests (`linux_test.go`):**
- Landlock availability detection
- bwrap argument construction (bind, ro-bind, network, PID)
- Environment filtering with CleanEnv
- Port filtering warnings
- Graceful fallback when neither available

**Integration tests (`linux_integration_test.go`):**
- Denied path blocking (ExtraDenied paths inaccessible)
- Writable path access (ProjectRoot is writable)
- Command execution under sandbox
- Sandbox reference resolution

### Preflight Integration

Update `/preflight` to recognize that `go test -race ./...` now
includes linux tests via testcontainers. No special flags needed —
the container test is a regular Go test that runs when Docker is
available.

The preflight report gains implicit linux test coverage without
a separate check line. If Docker is unavailable, the test skips
gracefully with a logged message.

### Drop devcontainer

Remove `.devcontainer/` entirely. It served as a linux dev
environment but is superseded by testcontainers for testing and
nix devshell for development.

## Constraints

- **Docker Desktop required** — tests skip if Docker unavailable
- **Kernel version** — Docker Desktop's VM kernel must be 5.13+
  for Landlock (all current versions satisfy this). Kernel 6.7+
  needed for port filtering tests (V4); tests should skip
  gracefully on older kernels via existing `BestEffort()` pattern.
- **First run is slow** — container image build is cached after
  first run. Subsequent runs reuse the image.
- **CI unchanged** — CI runs on native linux and does not use
  testcontainers. The container tests only activate on non-linux
  platforms.

## What This Does NOT Change

- Linux sandbox implementation (bwrap + Landlock) — unchanged
- macOS sandbox implementation (Seatbelt) — unchanged
- CI workflow — unchanged (runs native linux)
- Existing test files — unchanged (just run inside a container now)

## Success Criteria

- `go test ./internal/sandbox/...` on macOS runs linux tests via
  testcontainers and reports pass/fail
- Deleting `expandGlobs` on macOS would be caught by local tests
  (container build fails)
- Preflight catches linux regressions before push
- No devcontainer directory in the repo
