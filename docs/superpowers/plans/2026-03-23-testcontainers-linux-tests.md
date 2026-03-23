# Testcontainers Linux Integration Tests — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Run linux sandbox tests (bwrap + Landlock) from macOS via testcontainers-go, replacing devcontainer.

**Architecture:** A new `linux_container_test.go` (build-tagged `!linux`) uses testcontainers-go to start a linux container with bwrap, bind-mounts the project source, and runs `go test` inside it targeting the existing linux test files. Existing tests are unchanged.

**Tech Stack:** `testcontainers-go`, Docker, `golang:1.25-bookworm` base image

**Spec:** `docs/superpowers/specs/2026-03-23-testcontainers-linux-integration-design.md`

---

### Task 1: Add testcontainers-go dependency

**Files:**
- Modify: `go.mod`
- Modify: `go.sum`

- [ ] **Step 1: Add the dependency**

```bash
cd /Users/subramk/source/github.com/jskswamy/aide/.worktrees/feat-testcontainers
go get github.com/testcontainers/testcontainers-go
go mod tidy
```

- [ ] **Step 2: Verify it resolves**

Run: `go mod verify`
Expected: `all modules verified`

- [ ] **Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "Add testcontainers-go dependency"
```

---

### Task 2: Create test container Dockerfile

**Files:**
- Create: `internal/sandbox/testdata/Dockerfile`

- [ ] **Step 1: Create the Dockerfile**

```dockerfile
FROM golang:1.25-bookworm

RUN apt-get update && apt-get install -y --no-install-recommends \
    bubblewrap \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /workspace
```

- [ ] **Step 2: Verify it builds**

Run: `docker build -t aide-sandbox-test internal/sandbox/testdata/`
Expected: Image builds successfully

- [ ] **Step 3: Verify bwrap works inside container**

Run: `docker run --rm --privileged aide-sandbox-test bwrap --version`
Expected: Prints bwrap version

- [ ] **Step 4: Commit**

```bash
git add internal/sandbox/testdata/Dockerfile
git commit -m "Add Dockerfile for linux sandbox test container"
```

---

### Task 3: Write the container test launcher

**Files:**
- Create: `internal/sandbox/linux_container_test.go`

- [ ] **Step 1: Write the test file**

This file is build-tagged `!linux` so it only runs on macOS (or
other non-linux platforms). It uses testcontainers-go to:

1. Build the container from `testdata/Dockerfile`
2. Bind-mount the project root into `/workspace`
3. Run `go test -v -tags integration ./internal/sandbox/...` inside
4. Stream output to `t.Log`
5. Fail if exit code != 0

```go
//go:build !linux

package sandbox

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func projectRoot(t *testing.T) string {
	t.Helper()
	// internal/sandbox -> project root is ../../
	dir, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve project root: %v", err)
	}
	return dir
}

func TestLinuxSandbox_ViaContainer(t *testing.T) {
	if os.Getenv("SKIP_CONTAINER_TESTS") != "" {
		t.Skip("SKIP_CONTAINER_TESTS set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	root := projectRoot(t)

	req := testcontainers.ContainerRequest{
		FromDockerfile: testcontainers.FromDockerfile{
			Context:    filepath.Join(root, "internal", "sandbox", "testdata"),
			Dockerfile: "Dockerfile",
		},
		Mounts: testcontainers.Mounts(
			testcontainers.BindMount(root, "/workspace"),
		),
		Cmd: []string{
			"go", "test", "-v", "-count=1",
			"-tags", "integration",
			"./internal/sandbox/...",
		},
		Privileged: true, // needed for bwrap namespace operations
		WaitingFor: wait.ForExit(),
	}

	container, err := testcontainers.GenericContainer(ctx,
		testcontainers.GenericContainerRequest{
			ContainerRequest: req,
			Started:          true,
		})
	if err != nil {
		t.Fatalf("start container: %v", err)
	}
	defer func() {
		_ = container.Terminate(ctx)
	}()

	// Read logs
	logs, err := container.Logs(ctx)
	if err != nil {
		t.Fatalf("read container logs: %v", err)
	}
	defer logs.Close()

	buf := make([]byte, 64*1024)
	for {
		n, readErr := logs.Read(buf)
		if n > 0 {
			t.Log(string(buf[:n]))
		}
		if readErr != nil {
			break
		}
	}

	// Check exit code
	state, err := container.State(ctx)
	if err != nil {
		t.Fatalf("get container state: %v", err)
	}
	if state.ExitCode != 0 {
		t.Fatalf("linux tests failed with exit code %d", state.ExitCode)
	}
}
```

- [ ] **Step 2: Verify it compiles on macOS**

Run: `go vet ./internal/sandbox/...`
Expected: No errors (file compiles on darwin via `!linux` tag)

- [ ] **Step 3: Run the container test**

Run: `go test -v -run TestLinuxSandbox_ViaContainer ./internal/sandbox/... -timeout 5m`
Expected: Container starts, linux tests run inside, output streamed, PASS

- [ ] **Step 4: Verify skip behavior**

Run: `SKIP_CONTAINER_TESTS=1 go test -v -run TestLinuxSandbox_ViaContainer ./internal/sandbox/...`
Expected: Test skipped with message

- [ ] **Step 5: Commit**

```bash
git add internal/sandbox/linux_container_test.go
git commit -m "Add testcontainers-based linux sandbox test launcher"
```

---

### Task 4: Remove devcontainer

**Files:**
- Delete: `.devcontainer/Dockerfile`
- Delete: `.devcontainer/devcontainer.json`

- [ ] **Step 1: Remove the devcontainer directory**

```bash
rm -rf .devcontainer
```

- [ ] **Step 2: Verify no references remain**

Search for "devcontainer" in the codebase. Remove or update any
references found (docs, CI, etc.).

- [ ] **Step 3: Commit**

```bash
git rm -r .devcontainer
git commit -m "Remove devcontainer in favor of testcontainers"
```

---

### Task 5: Update preflight command

**Files:**
- Modify: `.claude/commands/preflight.md`

- [ ] **Step 1: Update the test description**

The `go test` step now implicitly includes linux tests via
testcontainers when Docker is available. Update the preflight
documentation to note this — no new check line needed, but the
description of the test check should mention that linux sandbox
tests run via container on non-linux platforms.

- [ ] **Step 2: Remove the cross-platform lint/gosec workaround note**

The cross-platform lint and gosec checks remain useful (they're
faster than container tests for catching static issues). But
update the comment about testcontainers being a "future
improvement" since it's now implemented.

- [ ] **Step 3: Commit**

```bash
git add -f .claude/commands/preflight.md
git commit -m "Update preflight docs for testcontainers integration"
```

---

### Task 6: End-to-end verification

- [ ] **Step 1: Run full preflight locally**

Run `/preflight` and verify:
- Native build passes
- Cross-platform build passes
- Lint (native + linux) passes
- Tests pass (including container-based linux tests)
- Gosec (native + linux) passes
- Govulncheck passes

- [ ] **Step 2: Simulate the expandGlobs deletion scenario**

Temporarily delete `expandGlobs` from `sandbox.go` and run
`go test ./internal/sandbox/...`. Verify the container test
catches the breakage. Then restore the function.

- [ ] **Step 3: Verify CI compatibility**

Check that CI (which runs on native linux) is unaffected — the
`linux_container_test.go` file is build-tagged `!linux` so it
won't run on CI.
