# CI Check Command Design

**Date:** 2026-03-23
**Status:** Superseded by preflight-suite-design
**Type:** Project-specific Claude Code command

## Problem

CI failures are caught only after pushing to GitHub. The feedback
loop is slow — push, wait for Actions, read logs, fix, push again.
We just experienced this: Landlock test failures, a vulnerable
dependency, gosec exit code issues, and lint errors all required
multiple round-trips to fix.

## Solution

A `/ci-check` command that runs the same checks as GitHub Actions
CI locally, with a fast gate check followed by parallel execution
of the remaining checks.

## Scope

Project-specific command for the aide repository. Lives at
`.claude/commands/ci-check.md`. Not a standalone plugin — that
can be built later once the command is proven.

## Execution Flow

### Phase 1: Gate (sequential)

Run `make build` first. If it fails, stop immediately and report
the error. No point running lint or tests if the code does not
compile.

### Phase 2: Parallel checks

If build passes, run all four checks concurrently:

| Check | Command | Purpose |
|-------|---------|---------|
| Lint | `golangci-lint run ./...` | Style and correctness |
| Test | `go test -race ./...` | Logic and race conditions |
| Security | `gosec ./...` | Static security analysis |
| Vulnerabilities | `govulncheck ./...` | Dependency vulnerability scan |

Collect all results before reporting. Do not fail-fast within
this phase — run everything and report all issues together.

### Phase 3: Report

Output a summary table followed by failure details:

```
ci-check results
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
```

When all checks pass:

```
ci-check results
────────────────────────────────────────
  build         pass   (1.2s)
  lint          pass   (3.4s)
  test          pass   (5.1s)
  gosec         pass   (2.8s)
  govulncheck   pass   (1.5s)
────────────────────────────────────────
RESULT: READY TO PUSH
```

### Phase 4: Build gate failure

When build fails, skip all other checks:

```
ci-check results
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
```

## Prerequisites

All required tools are provided by the nix devshell (`nix develop`):

- `go`, `golangci-lint`, `gosec` — from nixpkgs
- `govulncheck` — installed via `go install` in the shell hook

The command prompt must verify tool availability before running
and report any missing tools with a reminder to enter the devshell.

## Command Implementation

The command is a markdown prompt file at
`.claude/commands/ci-check.md` that instructs Claude to:

1. Verify all required tools are available
2. Run `make build` via Bash tool
3. If build succeeds, run four parallel Bash calls
4. Collect exit codes and stdout/stderr from each
5. Format the summary table with pass/FAIL and duration
6. Append full output for each failed check
7. End with a verdict line

The prompt must instruct Claude to use parallel Bash tool calls
for the four concurrent checks. Each Bash call captures both
stdout and stderr. Timing is derived from the Bash tool execution.

## Checks Parity with CI

| CI Job | CI Command | Local Command |
|--------|-----------|---------------|
| Build | `make build` | `make build` |
| Lint | `golangci-lint-action@v7` | `golangci-lint run ./...` |
| Test | `go test -race -coverprofile=coverage.out ./...` | `go test -race ./...` |
| Security Scan | `gosec -no-fail -fmt sarif ./...` | `gosec ./...` |
| Vuln Check | `govulncheck ./...` | `govulncheck ./...` |

Differences from CI:
- No coverage profile (not needed locally)
- gosec uses text output instead of SARIF (more readable)
- gosec runs without `-no-fail` — intentionally stricter than CI.
  In CI, gosec findings are advisory (uploaded to GitHub security
  tab via SARIF). Locally, we want to surface them as failures so
  they get fixed before they accumulate.
- `go vet` is not a separate check. It runs as part of
  `golangci-lint` which enables the `govet` linter by default.
  The project's `.golangci.yml` config is picked up automatically.
- No CodeQL (requires GitHub infrastructure)

## What This Does Not Do

- No git hook integration (future work, separate design)
- No auto-fix suggestions (just reports issues)
- No SARIF or structured output (text-only for readability)
- No configuration file (commands match CI exactly)
- Not a standalone plugin (project-specific for now)

## Success Criteria

- Running `/ci-check` catches the same issues that CI catches
- Build failure stops execution immediately
- Four parallel checks complete faster than running sequentially
- Output is scannable — pass/fail visible at a glance
- Failure details are sufficient to fix without re-running
