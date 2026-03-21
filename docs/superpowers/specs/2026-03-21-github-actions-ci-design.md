# GitHub Actions CI Pipeline

**Date:** 2026-03-21
**Status:** Draft

## Problem

aide is about to go public. Before pushing, we need automated CI to enforce code quality, catch security issues, and prevent regressions. No CI exists today.

## Design

### General Notes

- All jobs run on `ubuntu-latest`.
- All jobs begin with `actions/checkout@v4`.
- Go version: `1.25.x` (matches `go.mod`'s `go 1.25.7`, picks up latest patch).
- All action references are pinned to major version tags for supply-chain safety.

### 1. Workflow: ci.yml

**Trigger:** PRs targeting `main` + push to `main`.

**Jobs (all parallel):**

#### lint
- Uses `golangci/golangci-lint-action@v7`
- Reads `.golangci.yml` from repo root
- Fails the build on any finding

#### test
- Runs `go test -race -coverprofile=coverage.out ./...`
- Note: CI intentionally adds `-race` and `-coverprofile` beyond what `make test` does. The Makefile target stays simple for local dev; CI adds thoroughness.
- Coverage profile uploaded as artifact

#### sast-gosec
- Runs `gosec` via `securego/gosec@v2` (verify exact action path during implementation — may be `securego/gosec/action@v2`)
- Scans with `gosec ./...`
- SARIF output uploaded to GitHub Security tab via `github/codeql-action/upload-sarif@v3`
- Fails on findings

#### build
- Runs `make build` (consistent with Makefile: `go build -o bin/aide ./cmd/aide`)

### 2. Workflow: security.yml

**Trigger:** PRs targeting `main` + push to `main` + weekly schedule (Sunday 00:00 UTC).

**Jobs:**

#### govulncheck
- `actions/setup-go@v5` with Go 1.25.x
- Installs `golang.org/x/vuln/cmd/govulncheck@latest`
- Runs `govulncheck ./...`
- Fails on known vulnerabilities

#### codeql
- Uses `github/codeql-action/init@v3` + `github/codeql-action/analyze@v3`
- Language: `go`
- SARIF results uploaded to GitHub Security tab

### 3. Linter Config: .golangci.yml

Extend the existing config. Current linters (errcheck, govet, staticcheck, unused) are kept. `staticcheck` already covers `gosimple` and `ineffassign`, so those are not added separately.

**New linters to add:**

- `misspell` — Catches common misspellings in comments/strings
- `revive` — Configurable Go linter (superset of golint)
- `gocritic` — Opinionated but catches real bugs
- `exhaustive` — Ensures switch statements on enums are exhaustive
- `nolintlint` — Enforces that `//nolint` directives have justifications

Preserve existing settings (errcheck exclusions, govet config). Add reasonable defaults for new linters.

### 4. File Structure

```
.github/
  workflows/
    ci.yml          — lint, test, gosec, build
    security.yml    — govulncheck, codeql (+ weekly schedule)
.golangci.yml       — extended with strict linters (already exists, modified)
```

### 5. Permissions

All workflows use minimal permissions:
- `contents: read` (default for most jobs)
- `security-events: write` (for SARIF upload to Security tab)
- `actions: read` (for CodeQL)

### 6. Caching

- Go module cache: `actions/setup-go@v5` handles this automatically with its built-in cache
- golangci-lint: the action handles its own cache

## What This Does NOT Include

- DAST — No running service to probe yet.
- Release workflows — Separate sub-project (GoReleaser).
- Changelog — Separate sub-project (git-cliff).
- Branch protection rules — Manual GitHub settings, not automated.
- Integration tests in CI — The `test-linux` target requires Docker-in-Docker and Linux-specific kernel features (Landlock). Deferred to a follow-up.
- Go version matrix — aide is distributed as binaries via GoReleaser; users don't build from source. Single version is sufficient.
