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
