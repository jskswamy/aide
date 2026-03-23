# Fix Checks

Detect and fix failures from local checks or CI. Does not auto-commit
fixes — the user reviews changes before committing.

## Environment Setup

Same env prefix as /preflight. Every Bash command must include:

~~~
export GOROOT="$(dirname "$(dirname "$(readlink -f "$(command -v go)")")")"; export GOCACHE=/tmp/go-build-cache; export GOMODCACHE=/tmp/gomod-cache; export GOLANGCI_LINT_CACHE=/tmp/golangci-lint-cache
~~~

## Step 1: Detect failures

Run the same checks as /preflight to detect local failures. Use the
PREFLIGHT_EXIT wrapping pattern so all checks complete:

1. Build gate: `make build` (if fails, fix build errors first)
2. If build passes, run four parallel checks (wrapped to always exit 0):

~~~
<env-prefix>; golangci-lint run ./... 2>&1; ec=$?; echo "PREFLIGHT_EXIT:$ec"
<env-prefix>; go test -race ./... 2>&1; ec=$?; echo "PREFLIGHT_EXIT:$ec"
<env-prefix>; gosec ./... 2>&1; ec=$?; echo "PREFLIGHT_EXIT:$ec"
<env-prefix>; govulncheck ./... 2>&1; ec=$?; echo "PREFLIGHT_EXIT:$ec"
~~~

Parse PREFLIGHT_EXIT:N from each to determine pass/fail.

If ALL checks pass locally, fall back to remote CI failures (Step 1b).

## Step 1b: Fall back to remote CI failures

If no local failures, fetch CI logs:

~~~bash
gh run list --branch $(git branch --show-current) --workflow ci.yml --limit 1 --json databaseId,conclusion
~~~

If the latest run failed:

~~~bash
gh run view <id> --log-failed
~~~

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

~~~
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
~~~

If everything was fixed:

~~~
fix-checks results
────────────────────────────────────────
Fixed:
  - lint: removed unused parameter 'i' in darwin_test.go (2 locations)
  - lint: added comment for exported const Setup
  - lint: removed unused function expandGlobs

Re-verified: all checks pass
────────────────────────────────────────
~~~

Do NOT commit the fixes. The user reviews changes and commits manually.
