# Preflight Command Suite Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Create three Claude Code commands (`/preflight`, `/ci-status`, `/fix-checks`) for local CI verification, remote CI status, and automated fix application.

**Architecture:** Three independent markdown prompt files in `.claude/commands/`. Each is a self-contained instruction set for Claude. The existing `/ci-check` command is replaced by `/preflight` with a parallel-cancellation fix. No Go code changes — all three are prompt-only files.

**Tech Stack:** Claude Code commands (markdown prompts), Bash tool, `gh` CLI, Go toolchain

---

## File Structure

| File | Action | Responsibility |
|------|--------|----------------|
| `.claude/commands/preflight.md` | Create | Run CI checks locally with parallel-safe wrapping |
| `.claude/commands/ci-status.md` | Create | Query GitHub Actions results via `gh` CLI |
| `.claude/commands/fix-checks.md` | Create | Detect and fix check failures (local or remote) |
| `.claude/commands/ci-check.md` | Delete | Replaced by `preflight.md` |

---

### Task 1: Create `/preflight` command

**Files:**
- Create: `.claude/commands/preflight.md`
- Delete: `.claude/commands/ci-check.md`

**Context:**
- This replaces the existing `/ci-check` command
- Read `.claude/commands/ci-check.md` first — the new file keeps the same environment setup and report format but adds the `PREFLIGHT_EXIT` wrapping pattern
- Read `.claude/commands/audit-docs.md` for reference on command tone and structure
- The spec is at `docs/superpowers/specs/2026-03-23-preflight-suite-design.md`, sections "Command 1: /preflight"

- [ ] **Step 1: Read existing commands for reference**

Read `.claude/commands/ci-check.md` and `.claude/commands/audit-docs.md`.

- [ ] **Step 2: Create the preflight command file**

Create `.claude/commands/preflight.md` with this exact content (note: uses `~~~` for inner code fences since the file is markdown):

~~~
# Preflight

Run the same checks as GitHub Actions CI locally before pushing.

## Environment Setup

Claude Code's sandbox may not inherit the full nix devshell environment.
Before running ANY Go command, set up the Go environment. Since shell
state does not persist between Bash tool calls, EVERY Bash command must
include this exact env prefix:

```
ENV_PREFIX='export GOROOT="$(dirname "$(dirname "$(readlink -f "$(command -v go)")")")"; export GOCACHE=/tmp/go-build-cache; export GOMODCACHE=/tmp/gomod-cache; export GOLANGCI_LINT_CACHE=/tmp/golangci-lint-cache'
```

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

```
export GOROOT="$(dirname "$(dirname "$(readlink -f "$(command -v go)")")")"; export GOCACHE=/tmp/go-build-cache; export GOMODCACHE=/tmp/gomod-cache; export GOLANGCI_LINT_CACHE=/tmp/golangci-lint-cache; make build
```

If the build fails (non-zero exit code), skip all remaining checks and
go directly to the report with build as FAIL and all others as "skip".

## Phase 2: Parallel Checks

If build passes, run ALL FOUR checks in parallel using four simultaneous
Bash tool calls in a single message.

**CRITICAL:** Each command must be wrapped so it ALWAYS exits 0, preventing
the Bash tool from cancelling sibling calls. Use this exact pattern — the
env prefix, then the command with stderr merged to stdout, then capture the
exit code into a variable and echo a sentinel:

```
export GOROOT="$(dirname "$(dirname "$(readlink -f "$(command -v go)")")")"; export GOCACHE=/tmp/go-build-cache; export GOMODCACHE=/tmp/gomod-cache; export GOLANGCI_LINT_CACHE=/tmp/golangci-lint-cache; golangci-lint run ./... 2>&1; ec=$?; echo "PREFLIGHT_EXIT:$ec"
```

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

```
preflight results
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
```

When build fails, skip all other checks:

```
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
```

Show failure details only for checks that failed — include the full
output from the failed check (everything before the PREFLIGHT_EXIT line).
Replace the duration placeholders with actual measured durations.
~~~

- [ ] **Step 3: Delete the old ci-check command**

```bash
git rm .claude/commands/ci-check.md
```

If `git rm` fails because the file is gitignored, use:

```bash
rm .claude/commands/ci-check.md
```

- [ ] **Step 4: Verify the new command exists**

```bash
ls .claude/commands/preflight.md
```

- [ ] **Step 5: Commit**

```bash
git add -f .claude/commands/preflight.md
git rm -f .claude/commands/ci-check.md 2>/dev/null || true
git -c commit.gpgsign=false commit -m "Replace /ci-check with /preflight command

Renamed for clarity: runs checks locally, does not check CI status.
Adds PREFLIGHT_EXIT wrapping to prevent parallel Bash call cancellation."
```

---

### Task 2: Create `/ci-status` command

**Files:**
- Create: `.claude/commands/ci-status.md`

**Context:**
- This is a new command that queries GitHub Actions via the `gh` CLI
- It does NOT run any checks locally — read-only
- The spec is at `docs/superpowers/specs/2026-03-23-preflight-suite-design.md`, section "Command 2: /ci-status"

- [ ] **Step 1: Create the ci-status command file**

Create `.claude/commands/ci-status.md` with this exact content:

~~~
# CI Status

Check GitHub Actions results for the current branch. Read-only — does
not run any checks locally.

## Step 1: Get current branch

Run:
```bash
git branch --show-current
```

## Step 2: Check for a PR

Run:
```bash
gh pr view --json number,url,headRefName 2>/dev/null
```

If this succeeds (exit 0), a PR exists. Extract the PR number and URL.
If it fails, there is no PR for this branch.

## Step 3: Get check results

**If a PR exists:**

Run:
```bash
gh pr checks
```

This lists all checks with their status, duration, and URL.

**If no PR exists:**

Query both CI workflows:
```bash
gh run list --branch <branch> --workflow ci.yml --limit 1 --json databaseId,status,conclusion
gh run list --branch <branch> --workflow security.yml --limit 1 --json databaseId,status,conclusion
```

If either returns results, get job details:
```bash
gh run view <id> --json jobs
```

If neither returns results, check if the branch exists on the remote:
```bash
git ls-remote --heads origin <branch>
```

## Step 4: Report

Format results as a summary table. Use this exact format.

When checks are available (PR path):

```
ci-status results (PR #42)
────────────────────────────────────────
  Lint                pass   (1m 2s)
  Test                pass   (1m 28s)
  Build               pass   (1m 1s)
  Security Scan       pass   (3m 6s)
  Vulnerability Check pass   (21s)
  CodeQL Analysis     pass   (2m 27s)
────────────────────────────────────────
RESULT: ALL CHECKS PASSED

URL: https://github.com/jskswamy/aide/pull/42
```

When checks are failing or pending:

```
ci-status results (PR #42)
────────────────────────────────────────
  Lint                FAIL   (1m 2s)
  Test                pass   (1m 28s)
  Build               pass   (1m 1s)
  Security Scan       pending
  Vulnerability Check pass   (21s)
  CodeQL Analysis     pending
────────────────────────────────────────
RESULT: 1 FAILED, 2 PENDING

URL: https://github.com/jskswamy/aide/pull/42
```

When branch is not pushed:

```
ci-status: no CI run found for branch 'feature/my-branch'

Branch not pushed to remote. Push first:
  git push -u origin feature/my-branch
```

When branch exists on remote but no CI run found:

```
ci-status: no CI run found for branch 'feature/my-branch'

Branch exists on remote but no workflow has run.
Create a PR to trigger CI:
  gh pr create
```

Map `gh pr checks` status values: "pass" → pass, "fail" → FAIL,
"pending" → pending, "skipping" → skip.
~~~

- [ ] **Step 2: Verify the file exists**

```bash
ls .claude/commands/ci-status.md
```

- [ ] **Step 3: Commit**

```bash
git add -f .claude/commands/ci-status.md
git -c commit.gpgsign=false commit -m "Add /ci-status command for GitHub Actions results"
```

---

### Task 3: Create `/fix-checks` command

**Files:**
- Create: `.claude/commands/fix-checks.md`

**Context:**
- This command detects failures and applies fixes
- It replicates the `/preflight` check logic inline (commands cannot invoke each other)
- The spec is at `docs/superpowers/specs/2026-03-23-preflight-suite-design.md`, section "Command 3: /fix-checks"
- Key detail: one verification pass after fixes, no retry loop

- [ ] **Step 1: Create the fix-checks command file**

Create `.claude/commands/fix-checks.md` with this exact content:

~~~
# Fix Checks

Detect and fix failures from local checks or CI. Does not auto-commit
fixes — the user reviews changes before committing.

## Environment Setup

Same env prefix as /preflight. Every Bash command must include:

```
export GOROOT="$(dirname "$(dirname "$(readlink -f "$(command -v go)")")")"; export GOCACHE=/tmp/go-build-cache; export GOMODCACHE=/tmp/gomod-cache; export GOLANGCI_LINT_CACHE=/tmp/golangci-lint-cache
```

## Step 1: Detect failures

Run the same checks as /preflight to detect local failures. Use the
PREFLIGHT_EXIT wrapping pattern so all checks complete:

1. Build gate: `make build` (if fails, fix build errors first)
2. If build passes, run four parallel checks (wrapped to always exit 0):

```
<env-prefix>; golangci-lint run ./... 2>&1; ec=$?; echo "PREFLIGHT_EXIT:$ec"
<env-prefix>; go test -race ./... 2>&1; ec=$?; echo "PREFLIGHT_EXIT:$ec"
<env-prefix>; gosec ./... 2>&1; ec=$?; echo "PREFLIGHT_EXIT:$ec"
<env-prefix>; govulncheck ./... 2>&1; ec=$?; echo "PREFLIGHT_EXIT:$ec"
```

Parse PREFLIGHT_EXIT:N from each to determine pass/fail.

If ALL checks pass locally, fall back to remote CI failures (Step 1b).

## Step 1b: Fall back to remote CI failures

If no local failures, fetch CI logs:

```bash
gh run list --branch $(git branch --show-current) --workflow ci.yml --limit 1 --json databaseId,conclusion
```

If the latest run failed:

```bash
gh run view <id> --log-failed
```

Parse the failure output and proceed to Step 2 with those failures.

If CI also passes, report "All checks pass — nothing to fix."

## Step 2: Categorize and fix

For each failure, apply fixes based on this classification:

**Lint failures (auto-fix):**
- Unused parameters: rename to `_` (e.g., `func foo(i int, ...)` → `func foo(_ int, ...)`)
- Missing exported comments: add a short doc comment
- Unused functions/variables: remove them
- Complex lint issues: read the code, understand the intent, apply the fix

**Test failures (auto-fix):**
- Read the test output to understand what failed
- Read the relevant source code
- Fix the code (not the test, unless the test itself is wrong)

**Gosec findings:**
- Code-level fixes: add error handling where appropriate
- Cosmetic findings (e.g., unhandled Fprintf errors in UI code): suggest
  adding `//nolint:G104` with a brief reason comment
- Classify each finding — do not blindly nolint everything

**Govulncheck (dependency vulns):**
- Run `go get -u <module>@<fixed-version>` then `go mod tidy`

**Govulncheck (stdlib vulns):**
- Cannot auto-fix. Report the Go version that contains the fix and
  tell the user to update the Go version in flake.nix.

## Step 3: Verify fixes

After applying fixes, re-run ONLY the checks that failed (not all five).
Use the same PREFLIGHT_EXIT wrapping pattern.

This is a single verification pass. If checks still fail after fixes,
report the remaining issues and stop. Do NOT retry in a loop.

## Step 4: Report

Output what was fixed and what needs manual action:

```
fix-checks results
────────────────────────────────────────
Fixed:
  - lint: removed unused parameter 'i' in darwin_test.go (2 locations)
  - lint: added comment for exported const Setup

Needs manual action:
  - govulncheck: Go 1.25.7 has 3 stdlib vulns, update to 1.25.8
  - gosec: 43 unhandled Fprintf errors in banner.go (cosmetic, consider nolint)

Re-verified: lint now passes
────────────────────────────────────────
```

If everything was fixed:

```
fix-checks results
────────────────────────────────────────
Fixed:
  - lint: removed unused parameter 'i' in darwin_test.go (2 locations)
  - lint: added comment for exported const Setup
  - lint: removed unused function expandGlobs

Re-verified: all checks pass
────────────────────────────────────────
```

Do NOT commit the fixes. The user reviews changes and commits manually.
~~~

- [ ] **Step 2: Verify the file exists**

```bash
ls .claude/commands/fix-checks.md
```

- [ ] **Step 3: Commit**

```bash
git add -f .claude/commands/fix-checks.md
git -c commit.gpgsign=false commit -m "Add /fix-checks command for automated failure repair"
```

---

### Task 4: Update spec and finalize

**Files:**
- Modify: `docs/superpowers/specs/2026-03-23-preflight-suite-design.md`
- Modify: `docs/superpowers/specs/2026-03-23-ci-check-design.md`

- [ ] **Step 1: Mark preflight suite spec as implemented**

Use the Edit tool to change `**Status:** Draft` to `**Status:** Implemented`
in `docs/superpowers/specs/2026-03-23-preflight-suite-design.md`.

- [ ] **Step 2: Mark old ci-check spec as superseded**

Use the Edit tool to change `**Status:** Implemented` to
`**Status:** Superseded by preflight-suite-design`
in `docs/superpowers/specs/2026-03-23-ci-check-design.md`.

- [ ] **Step 3: Commit**

```bash
git add docs/superpowers/specs/2026-03-23-preflight-suite-design.md docs/superpowers/specs/2026-03-23-ci-check-design.md
git -c commit.gpgsign=false commit -m "Mark preflight suite spec as implemented"
```
