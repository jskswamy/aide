# Preflight Command Suite Design

**Date:** 2026-03-23
**Status:** Implemented
**Type:** Project-specific Claude Code commands

## Problem

Three related pain points in the push workflow:

1. CI failures are caught only after pushing. Running checks locally
   before pushing prevents wasted round-trips.
2. After pushing, checking CI status requires switching to GitHub or
   running `gh` commands manually.
3. Fixing check failures (local or remote) is a manual process of
   reading errors and applying fixes one by one.

Additionally, the existing `/ci-check` command has two issues:
- The name suggests checking CI status, not running local checks.
- Parallel Bash calls cancel when one fails, breaking the
  run-everything-and-report-all-at-once design.

## Solution

Three focused commands:

| Command | Purpose |
|---------|---------|
| `/preflight` | Run CI checks locally before pushing |
| `/ci-status` | Check GitHub Actions results after pushing |
| `/fix-checks` | Fix failures from preflight or CI |

## Command 1: `/preflight`

Renamed from `/ci-check`. Runs the same checks as GitHub Actions CI
locally: build gate, then lint/test/gosec/govulncheck in parallel.

### Parallel cancellation fix

The Bash tool cancels sibling calls when one exits non-zero. To
prevent this, wrap each check so it always exits 0 by capturing
the exit code into a variable immediately:

```bash
<env-prefix>; <command> 2>&1; ec=$?; echo "PREFLIGHT_EXIT:$ec"
```

The sentinel `PREFLIGHT_EXIT:` uses a unique prefix unlikely to
appear in test or tool output. The preflight command file instructs
Claude to parse `PREFLIGHT_EXIT:N` from the output to determine
pass (0) or fail (non-zero). Since every Bash call exits 0, all
four checks run to completion regardless of individual failures.

### Environment setup

Same as the current `/ci-check` command. Every Bash call includes:

```
export GOROOT="$(dirname "$(dirname "$(readlink -f "$(command -v go)")")")";
export GOCACHE=/tmp/go-build-cache;
export GOMODCACHE=/tmp/gomod-cache;
export GOLANGCI_LINT_CACHE=/tmp/golangci-lint-cache
```

### Execution flow

1. Check prerequisites (`go`, `golangci-lint`, `gosec`, `govulncheck`)
2. Build gate: `make build` (if fails, skip everything else)
3. Parallel checks (all four, wrapped to always exit 0):
   - `golangci-lint run ./...`
   - `go test -race ./...`
   - `gosec ./...`
   - `govulncheck ./...`
4. Parse `PREFLIGHT_EXIT:N` from each check's output
5. Report summary table with pass/FAIL/skip and durations
6. Show failure details for failed checks
7. Verdict: READY TO PUSH or NOT READY TO PUSH

### gosec behavior difference from CI

In CI, gosec runs with `-no-fail` and outputs SARIF for GitHub's
security tab. Findings are advisory only and never block a build.

Locally, `/preflight` runs gosec without `-no-fail`. This is
intentionally stricter than CI: surface security findings as
failures so they get fixed before they accumulate. The `/fix-checks`
command accounts for this by classifying gosec findings as
auto-fixable (code changes) or advisory (suggest `nolint` comments
for cosmetic issues like unhandled Fprintf errors).

### Report format

Same as the current `/ci-check` spec. See the existing spec at
`docs/superpowers/specs/2026-03-23-ci-check-design.md` for the
exact output format with examples.

## Command 2: `/ci-status`

Check GitHub Actions results for the current branch. Uses a variant
of the `/preflight` report format adapted for remote CI data.

### Execution flow

1. Get current branch: `git branch --show-current`
2. Check if a PR exists: `gh pr view --json number,url 2>/dev/null`
3. If PR exists: `gh pr checks` to get check statuses
4. If no PR: query both CI and Security workflows using
   `gh run list --branch <branch> --workflow ci.yml --limit 1` and
   `gh run list --branch <branch> --workflow security.yml --limit 1`,
   then `gh run view <id> --json jobs` for each
5. Format results as a summary table

### Report format

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

When no CI run exists:

```
ci-status: no CI run found for branch 'feature/my-branch'

Branch not pushed to remote. Push first:
  git push -u origin feature/my-branch
```

If the branch exists on the remote but no CI run is found:

```
ci-status: no CI run found for branch 'feature/my-branch'

Branch exists on remote but no workflow has run.
Create a PR to trigger CI:
  gh pr create
```

## Command 3: `/fix-checks`

Fix failures found by checks. Single command handles both local
and remote failures.

### Execution flow

1. Run the same checks as `/preflight` inline (replicate the Bash
   calls directly — Claude Code commands cannot invoke each other)
2. If local failures found, categorize and fix:

   | Check | Auto-fixable | Manual action |
   |-------|-------------|---------------|
   | lint | Unused params (rename to `_`), missing comments, dead code removal | Complex refactors flagged by lint |
   | test | Read failure output, fix the failing code | Flaky tests, environment issues |
   | gosec | Code-level fixes (error handling) | Cosmetic findings (suggest `//nolint:G104`) |
   | govulncheck (deps) | Run `go get -u <module>@<fixed-version>` | N/A |
   | govulncheck (stdlib) | N/A | Report Go version upgrade needed |

3. If no local failures found:
   - Fetch CI logs via `gh run view --log-failed`
   - Parse failure output and apply the same fix categories
4. After fixing, re-run only the checks that failed to verify fixes
   (one verification pass; if it still fails, report remaining
   issues and stop — do not retry in a loop)
5. Report what was fixed and what needs manual attention

### Output format

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

## File locations

All three commands live in `.claude/commands/`:

- `.claude/commands/preflight.md` (rename from `ci-check.md`)
- `.claude/commands/ci-status.md` (new)
- `.claude/commands/fix-checks.md` (new)

The old `.claude/commands/ci-check.md` is deleted.

## What these commands do NOT do

- No git hook integration (future work)
- No automatic pushing after preflight passes
- No automatic PR creation
- `/fix-checks` does not auto-commit fixes (user reviews first)

## Success criteria

- `/preflight` runs all four checks to completion even when some fail
- `/ci-status` shows current CI state without running anything locally
- `/fix-checks` handles both local and remote failures
- All three commands produce scannable, consistent output formats
