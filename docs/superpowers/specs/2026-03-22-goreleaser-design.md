# GoReleaser Release Pipeline

**Date:** 2026-03-22
**Status:** Draft

## Problem

aide needs a release pipeline before going public. Users need downloadable binaries with version info, and the release process should be automated from a git tag push.

## Design

### 1. GoReleaser Config: .goreleaser.yml

Uses GoReleaser **v2** config format (`version: 2` at top of file).

**Builds:**
- Single binary `aide` from `./cmd/aide`
- Targets: `darwin/arm64`, `darwin/amd64` (macOS only for now)
- ldflags inject version, commit, and date:
  ```
  -s -w -X main.version={{.Version}} -X main.commit={{.ShortCommit}} -X main.date={{.Date}}
  ```

**Archives:**
- `.tar.gz` format
- Includes: binary, `LICENSE`, `README.md` (both exist in repo root)
- Name template: `aide_{{ .Version }}_{{ .Os }}_{{ .Arch }}`

**Checksum:** `sha256sums.txt`

**Changelog:**
- Auto-generated from commits since last tag
- Sort by `asc`
- No commit grouping in v1 — keep it simple. Add conventional commit grouping later if needed.

**Snapshot:** For non-tag builds, use `version_template: "{{ .Version }}-next"` (GoReleaser v2 field name).

### 2. Version Info in Binary

**Add** two new variables to `cmd/aide/main.go` (currently only `version` exists):

```go
var (
    version = "dev"
    commit  = "none"
    date    = "unknown"
)
```

Set Cobra's `SetVersionTemplate` on the root command to produce the desired format:

```go
rootCmd.SetVersionTemplate(`aide {{.Version}} (commit: ` + commit + `, built: ` + date + ")\n")
rootCmd.Version = version
```

`aide --version` output:

```
aide v0.1.0 (commit: abc1234, built: 2026-03-22T10:00:00Z)
```

During development (without ldflags):

```
aide dev (commit: none, built: unknown)
```

### 3. Release Workflow: .github/workflows/release.yml

**Trigger:** Push tag matching `v*`.

**Steps:**
1. `actions/checkout@v4` with `fetch-depth: 0` (GoReleaser needs full history for changelog)
2. `actions/setup-go@v5` with Go 1.25.x
3. Run tests: `go test -race ./...` (gate: don't release broken code; includes `-race` for parity with CI)
4. `goreleaser/goreleaser-action@v6` with `args: release --clean`
5. Uses `GITHUB_TOKEN` for creating the release

**Permissions:** `contents: write` (to create releases and upload assets)

### 4. Install Script: install.sh

A standalone shell script at the repo root for one-liner installation:

```bash
curl -sSfL https://raw.githubusercontent.com/jskswamy/aide/main/install.sh | sh
```

**Behavior:**
- Detects OS (`uname -s`) — only macOS supported, errors on others with a clear message.
- Detects architecture (`uname -m`) — maps `arm64`/`aarch64` → `arm64`, `x86_64` → `amd64`.
- Fetches the latest release tag from GitHub API (`/repos/jskswamy/aide/releases/latest`).
- Downloads the matching archive from GitHub Releases.
- Verifies checksum against `sha256sums.txt`.
- Extracts the binary to `./bin/` by default, or `INSTALL_DIR` if set.
- No root required by default (installs to local dir). User can `INSTALL_DIR=/usr/local/bin sudo sh` for system-wide install.

**Usage examples:**
```bash
# Install to ./bin/
curl -sSfL https://raw.githubusercontent.com/jskswamy/aide/main/install.sh | sh

# Install to /usr/local/bin
curl -sSfL https://raw.githubusercontent.com/jskswamy/aide/main/install.sh | INSTALL_DIR=/usr/local/bin sudo sh

# Install specific version
curl -sSfL https://raw.githubusercontent.com/jskswamy/aide/main/install.sh | VERSION=v0.2.0 sh
```

### 5. File Structure

```
.goreleaser.yml                     — GoReleaser config (v2 format)
.github/workflows/release.yml      — Release workflow (tag-triggered)
cmd/aide/main.go                    — Add commit/date vars + SetVersionTemplate
install.sh                          — Curl-friendly install script
```

## What This Does NOT Include

- **Homebrew tap** — Deferred. Add when there's demand.
- **Nix package** — Deferred. Add when there's demand.
- **Linux/Windows builds** — macOS only until testing is complete for other platforms.
- **Docker images** — Not applicable (CLI tool, not a service).
- **Signing** — Can be added later with cosign or GPG.
- **Changelog grouping** — Simple list for now. Add conventional commit grouping later if needed.
- **Makefile snapshot target** — Can add `make snapshot` later for local testing.
