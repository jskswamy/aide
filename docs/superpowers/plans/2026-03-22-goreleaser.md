# GoReleaser Release Pipeline Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add tag-triggered release pipeline with GoReleaser, version info injection, and curl-friendly install script.

**Architecture:** GoReleaser builds macOS binaries from git tags, GitHub Actions automates the release, ldflags inject version metadata, install.sh provides one-liner installation.

**Tech Stack:** GoReleaser v2, GitHub Actions, shell script

**Spec:** `docs/superpowers/specs/2026-03-22-goreleaser-design.md`

---

### Task 1: Version Info in Binary

**Files:**
- Modify: `cmd/aide/main.go`

- [ ] **Step 1: Update version variables**

Replace the single `var version = "dev"` with a var block:

```go
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)
```

- [ ] **Step 2: Add SetVersionTemplate to root command**

After the `rootCmd` declaration (after line 56), before `rootCmd.Flags()`, add:

```go
rootCmd.SetVersionTemplate("aide " + version + " (commit: " + commit + ", built: " + date + ")\n")
```

Remove `Version: version,` from the `cobra.Command` struct literal since we set it via the var block and the template handles formatting. Actually, keep `Version: version` — Cobra needs it set for `--version` to work. The template just controls the output format.

- [ ] **Step 3: Verify it compiles**

Run: `cd /tmp/aide-goreleaser && go build ./cmd/aide`
Expected: Compiles successfully

- [ ] **Step 4: Test version output**

Run: `cd /tmp/aide-goreleaser && ./aide --version`
Expected: `aide dev (commit: none, built: unknown)`

- [ ] **Step 5: Commit**

```
git add cmd/aide/main.go
```
Message: `Add commit and date to version output`

---

### Task 2: GoReleaser Config

**Files:**
- Create: `.goreleaser.yml`

- [ ] **Step 1: Create .goreleaser.yml**

```yaml
version: 2

builds:
  - binary: aide
    main: ./cmd/aide
    goos:
      - darwin
    goarch:
      - amd64
      - arm64
    ldflags:
      - -s -w
      - -X main.version={{.Version}}
      - -X main.commit={{.ShortCommit}}
      - -X main.date={{.Date}}

archives:
  - format: tar.gz
    name_template: "aide_{{ .Version }}_{{ .Os }}_{{ .Arch }}"
    files:
      - LICENSE
      - README.md

checksum:
  name_template: "sha256sums.txt"

changelog:
  sort: asc

snapshot:
  version_template: "{{ .Version }}-next"

release:
  github:
    owner: jskswamy
    name: aide
```

- [ ] **Step 2: Validate YAML**

Run: `cd /tmp/aide-goreleaser && cat .goreleaser.yml | head -5`
Expected: Shows `version: 2` at top

- [ ] **Step 3: Commit**

```
git add .goreleaser.yml
```
Message: `Add GoReleaser config for macOS releases`

---

### Task 3: Release Workflow

**Files:**
- Create: `.github/workflows/release.yml`

- [ ] **Step 1: Create release.yml**

```yaml
name: Release

on:
  push:
    tags:
      - "v*"

permissions:
  contents: write

jobs:
  release:
    name: Release
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0

      - uses: actions/setup-go@v5
        with:
          go-version: "1.25.x"

      - name: Run tests
        run: go test -race ./...

      - name: Run GoReleaser
        uses: goreleaser/goreleaser-action@v6
        with:
          version: latest
          args: release --clean
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
```

- [ ] **Step 2: Commit**

```
git add .github/workflows/release.yml
```
Message: `Add release workflow for tag-triggered GoReleaser builds`

---

### Task 4: Install Script

**Files:**
- Create: `install.sh`

- [ ] **Step 1: Create install.sh**

```bash
#!/bin/sh
set -e

# aide installer
# Usage:
#   curl -sSfL https://raw.githubusercontent.com/jskswamy/aide/main/install.sh | sh
#   curl -sSfL https://raw.githubusercontent.com/jskswamy/aide/main/install.sh | INSTALL_DIR=/usr/local/bin sudo sh
#   curl -sSfL https://raw.githubusercontent.com/jskswamy/aide/main/install.sh | VERSION=v0.2.0 sh

REPO="jskswamy/aide"
BINARY="aide"
INSTALL_DIR="${INSTALL_DIR:-./bin}"

log() { echo "aide-installer: $*"; }
fail() { echo "aide-installer: ERROR: $*" >&2; exit 1; }

# Detect OS
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$OS" in
    darwin) ;;
    *) fail "unsupported OS: $OS (only macOS is supported)" ;;
esac

# Detect architecture
ARCH=$(uname -m)
case "$ARCH" in
    x86_64)       ARCH="amd64" ;;
    arm64|aarch64) ARCH="arm64" ;;
    *) fail "unsupported architecture: $ARCH" ;;
esac

# Determine version
if [ -z "$VERSION" ]; then
    log "fetching latest version..."
    VERSION=$(curl -sSfL "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name"' | sed -E 's/.*"([^"]+)".*/\1/')
    if [ -z "$VERSION" ]; then
        fail "could not determine latest version"
    fi
fi
log "version: $VERSION"

# Download
ARCHIVE="aide_${VERSION#v}_${OS}_${ARCH}.tar.gz"
URL="https://github.com/${REPO}/releases/download/${VERSION}/${ARCHIVE}"
CHECKSUM_URL="https://github.com/${REPO}/releases/download/${VERSION}/sha256sums.txt"

TMPDIR=$(mktemp -d)
trap 'rm -rf "$TMPDIR"' EXIT

log "downloading $ARCHIVE..."
curl -sSfL -o "${TMPDIR}/${ARCHIVE}" "$URL" || fail "download failed: $URL"

# Verify checksum
log "verifying checksum..."
curl -sSfL -o "${TMPDIR}/sha256sums.txt" "$CHECKSUM_URL" || fail "checksum download failed"
EXPECTED=$(grep "$ARCHIVE" "${TMPDIR}/sha256sums.txt" | awk '{print $1}')
if [ -z "$EXPECTED" ]; then
    fail "archive not found in sha256sums.txt"
fi
ACTUAL=$(shasum -a 256 "${TMPDIR}/${ARCHIVE}" | awk '{print $1}')
if [ "$EXPECTED" != "$ACTUAL" ]; then
    fail "checksum mismatch: expected $EXPECTED, got $ACTUAL"
fi
log "checksum verified"

# Extract and install
mkdir -p "$INSTALL_DIR"
tar -xzf "${TMPDIR}/${ARCHIVE}" -C "$TMPDIR"
cp "${TMPDIR}/${BINARY}" "${INSTALL_DIR}/${BINARY}"
chmod +x "${INSTALL_DIR}/${BINARY}"

log "installed $BINARY $VERSION to ${INSTALL_DIR}/${BINARY}"

# Hint if not in PATH
case ":$PATH:" in
    *":${INSTALL_DIR}:"*) ;;
    *) log "NOTE: add ${INSTALL_DIR} to your PATH" ;;
esac
```

- [ ] **Step 2: Make executable**

Run: `chmod +x /tmp/aide-goreleaser/install.sh`

- [ ] **Step 3: Commit**

```
git add install.sh
```
Message: `Add curl-friendly install script for macOS`

---

### Task 5: Verify Everything

- [ ] **Step 1: Check file structure**

Run: `find /tmp/aide-goreleaser -name ".goreleaser.yml" -o -name "release.yml" -o -name "install.sh" | sort`

Expected:
```
/tmp/aide-goreleaser/.github/workflows/release.yml
/tmp/aide-goreleaser/.goreleaser.yml
/tmp/aide-goreleaser/install.sh
```

- [ ] **Step 2: Verify version output**

Run: `cd /tmp/aide-goreleaser && go build ./cmd/aide && ./aide --version`
Expected: `aide dev (commit: none, built: unknown)`

- [ ] **Step 3: Shellcheck install script (if available)**

Run: `shellcheck /tmp/aide-goreleaser/install.sh 2>&1 || echo "shellcheck not available"`
