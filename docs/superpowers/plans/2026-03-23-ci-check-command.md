# CI Check Command Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Create a `/ci-check` Claude Code command that runs the same checks as GitHub Actions CI locally before pushing.

**Architecture:** A single markdown prompt file at `.claude/commands/ci-check.md` instructs Claude to run build as a gate, then lint/test/gosec/govulncheck in parallel, and report a summary table with pass/fail verdicts. The nix flake already provides all required tools.

**Tech Stack:** Claude Code commands (markdown prompt), Bash tool calls, nix devshell

---

### Task 1: Create the `/ci-check` command

**Files:**
- Create: `.claude/commands/ci-check.md`

**Context:**
- This is a Claude Code command — a markdown file that becomes available as `/ci-check` in the project
- Read `.claude/commands/audit-docs.md` first for reference on command structure and level of detail
- The spec is at `docs/superpowers/specs/2026-03-23-ci-check-design.md`

- [ ] **Step 1: Read the existing command for reference**

Read `.claude/commands/audit-docs.md` to understand the tone, structure, and level of detail used in existing commands.

- [ ] **Step 2: Write the command file**

Create `.claude/commands/ci-check.md` with the following content:

```markdown
# CI Check

Run the same checks as GitHub Actions CI locally before pushing.

## Prerequisites Check

Before running any checks, verify these tools are available by running
`command -v` for each one. All tools come from the nix devshell (`nix develop`):

- `go`
- `golangci-lint`
- `gosec`
- `govulncheck`

If any tool is missing, report which ones are missing and remind the user
to enter the devshell with `nix develop`. Do not proceed with checks.

## Phase 1: Build Gate

Run `make build` using the Bash tool. Record the exit code, output, and
wall-clock duration (note the time before and after the call).

If the build fails (non-zero exit code), skip all remaining checks and
go directly to the report with build as FAIL and all others as "skip".

## Phase 2: Parallel Checks

If build passes, run ALL FOUR of these checks in parallel using four
simultaneous Bash tool calls in a single message:

1. `golangci-lint run ./...`
2. `go test -race ./...`
3. `gosec ./...`
4. `govulncheck ./...`

IMPORTANT: Launch all four Bash calls in a single response so they run
concurrently. Do not wait for one to finish before starting the next.

Collect the exit code, full stdout+stderr, and wall-clock duration from each.

A check passes if its exit code is 0. Any non-zero exit code is a failure.

## Phase 3: Report

After all checks complete, output a summary table followed by failure
details. Use this exact format.

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

When some checks fail, show the summary table then failure details:

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

Show failure details only for checks that failed — include the full
stdout+stderr output from the failed check. Replace the duration
placeholders with actual measured durations.
```

- [ ] **Step 3: Verify the command is discoverable**

Run: `ls .claude/commands/ci-check.md`
Expected: File exists

- [ ] **Step 4: Commit the command**

```bash
git add .claude/commands/ci-check.md
git commit -m "Add /ci-check command for local CI verification"
```

### Task 2: Commit nix flake updates

The flake has been modified to add `pkgs.gosec` to buildInputs and a
govulncheck install in the shell hook. These changes may already be
staged or committed — check `git status` and `git diff flake.nix` first.

**Files:**
- Modified: `flake.nix`
- Possibly modified: `flake.lock` (if nix resolved new packages)

- [ ] **Step 1: Check if flake changes need committing**

Run `git status` and `git diff flake.nix`. If the file is already
committed (no changes shown), skip to Task 3.

- [ ] **Step 2: Verify the flake changes are correct**

Read `flake.nix` and confirm:
- `pkgs.gosec` is in `buildInputs`
- The `shellHook` includes the `govulncheck` install block

- [ ] **Step 3: Commit the flake update**

```bash
git add flake.nix flake.lock
git commit -m "Add gosec and govulncheck to nix devshell"
```

If `flake.lock` has no changes, only stage `flake.nix`.

### Task 3: Verification and finalize

- [ ] **Step 1: Run `/ci-check` in the project**

Invoke the `/ci-check` command and verify:
- It checks for tool availability
- Build runs first
- The four parallel checks run concurrently
- Summary table is displayed with pass/fail and durations for each
- Verdict line appears at the end

- [ ] **Step 2: Update spec status**

Use the Edit tool to change `**Status:** Draft` to `**Status:** Implemented`
in `docs/superpowers/specs/2026-03-23-ci-check-design.md`.

- [ ] **Step 3: Final commit**

```bash
git add docs/superpowers/specs/2026-03-23-ci-check-design.md
git commit -m "Mark ci-check spec as implemented"
```
