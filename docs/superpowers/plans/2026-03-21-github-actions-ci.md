# GitHub Actions CI Pipeline Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add CI workflows for linting, testing, SAST, and security scanning before the repo goes public.

**Architecture:** Two GitHub Actions workflows — `ci.yml` (fast, every PR) and `security.yml` (includes CodeQL + weekly govulncheck). Extends the existing `.golangci.yml` with strict linters.

**Tech Stack:** GitHub Actions, golangci-lint, gosec, govulncheck, CodeQL

**Spec:** `docs/superpowers/specs/2026-03-21-github-actions-ci-design.md`

---

### Task 1: Extend .golangci.yml with Strict Linters

**Files:**
- Modify: `.golangci.yml`

- [ ] **Step 1: Update .golangci.yml**

Add the new linters to the existing config. Preserve all existing settings (errcheck exclusions, govet config).

```yaml
version: "2"

linters:
  enable:
    - errcheck
    - govet
    - staticcheck
    - unused
    - misspell
    - revive
    - gocritic
    - exhaustive
    - nolintlint
  settings:
    errcheck:
      check-type-assertions: true
      exclude-functions:
        - fmt.Fprint
        - fmt.Fprintf
        - fmt.Fprintln
        - (*github.com/fatih/color.Color).Fprintf
        - (*github.com/fatih/color.Color).Fprintln
    govet:
      enable-all: true
      disable:
        - fieldalignment
        - shadow
    exhaustive:
      default-signifies-exhaustive: true
    nolintlint:
      require-explanation: true
      require-specific: true

issues:
  max-issues-per-linter: 0
  max-same-issues: 0
```

Notes on new linter settings:
- `exhaustive.default-signifies-exhaustive: true` — a `default` case in a switch counts as exhaustive, reducing false positives.
- `nolintlint.require-explanation: true` — forces `//nolint:lintername // reason` format so suppressions are documented.
- `nolintlint.require-specific: true` — disallows bare `//nolint` without specifying which linter.

- [ ] **Step 2: Run the linter locally to see current findings**

Run: `cd /Users/subramk/source/github.com/jskswamy/aide && golangci-lint run ./... 2>&1 | head -50`

Expected: May report new findings from the added linters. Review the output:
- If findings are legitimate issues: fix them in a follow-up or note them.
- If findings are false positives: add targeted `//nolint:lintername // reason` suppressions or adjust linter settings.

Do NOT fix all lint issues in this task. The goal is to get the config right. Fixing lint issues is a separate effort.

- [ ] **Step 3: Commit**

```
git add .golangci.yml
```
Message: `Extend golangci-lint config with strict linters`

---

### Task 2: Create ci.yml Workflow

**Files:**
- Create: `.github/workflows/ci.yml`

- [ ] **Step 1: Create the .github/workflows directory**

Run: `mkdir -p /Users/subramk/source/github.com/jskswamy/aide/.github/workflows`

- [ ] **Step 2: Write ci.yml**

```yaml
name: CI

on:
  push:
    branches: [main]
  pull_request:
    branches: [main]

permissions:
  contents: read

jobs:
  lint:
    name: Lint
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-go@v5
        with:
          go-version: "1.25.x"

      - uses: golangci/golangci-lint-action@v7
        with:
          version: latest

  test:
    name: Test
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-go@v5
        with:
          go-version: "1.25.x"

      - name: Run tests
        run: go test -race -coverprofile=coverage.out ./...

      - name: Upload coverage
        uses: actions/upload-artifact@v4
        with:
          name: coverage
          path: coverage.out
          retention-days: 7

  sast-gosec:
    name: Security Scan (gosec)
    runs-on: ubuntu-latest
    permissions:
      contents: read
      security-events: write
    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-go@v5
        with:
          go-version: "1.25.x"

      - name: Install gosec
        run: go install github.com/securego/gosec/v2/cmd/gosec@latest

      - name: Run gosec
        run: gosec -fmt sarif -out gosec-results.sarif ./...

      - name: Upload SARIF
        uses: github/codeql-action/upload-sarif@v3
        if: always()
        with:
          sarif_file: gosec-results.sarif

  build:
    name: Build
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-go@v5
        with:
          go-version: "1.25.x"

      - name: Build
        run: make build
```

Notes:
- gosec is installed via `go install` rather than using the `securego/gosec` action (which has inconsistent versioning). This gives us predictable behavior.
- The `-fmt sarif` flag produces SARIF output for the Security tab. The `if: always()` on the upload step ensures SARIF is uploaded even if gosec finds issues (non-zero exit).
- `security-events: write` permission is scoped to the `sast-gosec` job only (minimal permissions).

- [ ] **Step 3: Validate YAML syntax**

Run: `cd /Users/subramk/source/github.com/jskswamy/aide && python3 -c "import yaml; yaml.safe_load(open('.github/workflows/ci.yml'))" && echo "Valid YAML"`

Expected: `Valid YAML`

- [ ] **Step 4: Commit**

```
git add .github/workflows/ci.yml
```
Message: `Add CI workflow for lint, test, gosec, and build`

---

### Task 3: Create security.yml Workflow

**Files:**
- Create: `.github/workflows/security.yml`

- [ ] **Step 1: Write security.yml**

```yaml
name: Security

on:
  push:
    branches: [main]
  pull_request:
    branches: [main]
  schedule:
    - cron: "0 0 * * 0"  # Sunday 00:00 UTC

permissions:
  contents: read
  security-events: write
  actions: read

jobs:
  govulncheck:
    name: Vulnerability Check
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-go@v5
        with:
          go-version: "1.25.x"

      - name: Install govulncheck
        run: go install golang.org/x/vuln/cmd/govulncheck@latest

      - name: Run govulncheck
        run: govulncheck ./...

  codeql:
    name: CodeQL Analysis
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-go@v5
        with:
          go-version: "1.25.x"

      - name: Initialize CodeQL
        uses: github/codeql-action/init@v3
        with:
          languages: go

      - name: Build
        run: make build

      - name: Perform CodeQL Analysis
        uses: github/codeql-action/analyze@v3
```

Note: CodeQL needs a build step between init and analyze so it can trace the compilation. We use `make build` for this.

- [ ] **Step 2: Validate YAML syntax**

Run: `cd /Users/subramk/source/github.com/jskswamy/aide && python3 -c "import yaml; yaml.safe_load(open('.github/workflows/security.yml'))" && echo "Valid YAML"`

Expected: `Valid YAML`

- [ ] **Step 3: Commit**

```
git add .github/workflows/security.yml
```
Message: `Add security workflow with govulncheck and CodeQL`

---

### Task 4: Verify Everything Together

- [ ] **Step 1: Verify file structure**

Run: `find /Users/subramk/source/github.com/jskswamy/aide/.github -type f`

Expected:
```
.github/workflows/ci.yml
.github/workflows/security.yml
```

- [ ] **Step 2: Validate both workflows**

Run: `cd /Users/subramk/source/github.com/jskswamy/aide && for f in .github/workflows/*.yml; do echo "--- $f ---"; python3 -c "import yaml; yaml.safe_load(open('$f')); print('Valid')"; done`

Expected: Both files report `Valid`

- [ ] **Step 3: Run lint locally to confirm config works**

Run: `cd /Users/subramk/source/github.com/jskswamy/aide && golangci-lint run ./... --timeout 5m 2>&1 | tail -10`

Expected: Either clean output or known findings from the new strict linters. No config parsing errors.

- [ ] **Step 4: Run tests locally to confirm they still pass**

Run: `cd /Users/subramk/source/github.com/jskswamy/aide && go test -race ./... 2>&1 | tail -10`

Expected: All tests pass.
