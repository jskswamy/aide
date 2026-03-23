# Preflight

Run the same checks as GitHub Actions CI locally before pushing.

## Environment Setup

Claude Code's sandbox may not inherit the full nix devshell environment.
Before running ANY Go command, set up the Go environment. Since shell
state does not persist between Bash tool calls, EVERY Bash command must
include this exact env prefix:

~~~
ENV_PREFIX='export GOROOT="$(dirname "$(dirname "$(readlink -f "$(command -v go)")")")"; export GOCACHE=/tmp/go-build-cache; export GOMODCACHE=/tmp/gomod-cache; export GOLANGCI_LINT_CACHE=/tmp/golangci-lint-cache'
~~~

## Prerequisites Check

Verify these tools are available by running `command -v` for each one.
All tools come from the nix devshell (`nix develop`):

- `go`
- `golangci-lint`
- `gosec`
- `govulncheck`

If any tool is missing, report which ones are missing and remind the user
to enter the devshell with `nix develop`. Do not proceed with checks.

## Phase 1: Build Gate

Run the build with the env prefix:

~~~
export GOROOT="$(dirname "$(dirname "$(readlink -f "$(command -v go)")")")"; export GOCACHE=/tmp/go-build-cache; export GOMODCACHE=/tmp/gomod-cache; export GOLANGCI_LINT_CACHE=/tmp/golangci-lint-cache; make build
~~~

If the build fails (non-zero exit code), skip all remaining checks and
go directly to the report with build as FAIL and all others as "skip".

## Phase 2: Parallel Checks

If build passes, run ALL FOUR checks in parallel using four simultaneous
Bash tool calls in a single message.

**CRITICAL:** Each command must be wrapped so it ALWAYS exits 0, preventing
the Bash tool from cancelling sibling calls. Use this exact pattern — the
env prefix, then the command with stderr merged to stdout, then capture the
exit code into a variable and echo a sentinel:

~~~
export GOROOT="$(dirname "$(dirname "$(readlink -f "$(command -v go)")")")"; export GOCACHE=/tmp/go-build-cache; export GOMODCACHE=/tmp/gomod-cache; export GOLANGCI_LINT_CACHE=/tmp/golangci-lint-cache; golangci-lint run ./... 2>&1; ec=$?; echo "PREFLIGHT_EXIT:$ec"
~~~

The four commands (each wrapped with the pattern above):

1. `golangci-lint run ./...`
2. `go test -race ./...`
3. `gosec ./...`
4. `govulncheck ./...`

Launch all four Bash calls in a single response. Do not wait for one to
finish before starting the next.

After all four complete, parse `PREFLIGHT_EXIT:N` from each command's
output. N=0 means pass, N>0 means fail.

## Phase 3: Report

Output a summary table followed by failure details. Use this exact format.

When all checks pass:

~~~
preflight results
────────────────────────────────────────
  build         pass   (1.2s)
  lint          pass   (3.4s)
  test          pass   (5.1s)
  gosec         pass   (2.8s)
  govulncheck   pass   (1.5s)
────────────────────────────────────────
RESULT: READY TO PUSH
~~~

When some checks fail, show the summary table then failure details:

~~~
preflight results
────────────────────────────────────────
  build         pass   (1.2s)
  lint          FAIL   (3.4s)
  test          pass   (5.1s)
  gosec         pass   (2.8s)
  govulncheck   pass   (1.5s)
────────────────────────────────────────

--- lint (FAIL) ---
internal/foo/bar.go:42:6: exported: func name will be used as
    foo.FooBar by other packages, and that stutters (revive)

────────────────────────────────────────
RESULT: NOT READY TO PUSH (1 check failed)
~~~

When build fails, skip all other checks:

~~~
preflight results
────────────────────────────────────────
  build         FAIL   (0.8s)
  lint          skip
  test          skip
  gosec         skip
  govulncheck   skip
────────────────────────────────────────

--- build (FAIL) ---
cmd/aide/main.go:15:2: undefined: nonexistent

────────────────────────────────────────
RESULT: NOT READY TO PUSH (build failed)
~~~

Show failure details only for checks that failed — include the full
output from the failed check (everything before the PREFLIGHT_EXIT line).
Replace the duration placeholders with actual measured durations.
