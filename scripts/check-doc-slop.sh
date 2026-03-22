#!/usr/bin/env bash
# Pre-commit hook: check documentation files for slop words and patterns.
# Fast regex-based check. For full audit, use: /audit-docs in Claude Code.

set -euo pipefail

BANNED_WORDS=(
  "seamlessly" "effortlessly" "robust" "powerful" "leverage"
  "utilize" "comprehensive" "cutting-edge" "innovative"
  "game-changing" "world-class" "best-in-class" "next-generation"
  "harness" "unlock" "empower" "elevate" "streamline"
  "delve" "unpack"
)

BANNED_PATTERNS=(
  "It's worth noting"
  "Let me explain"
  "In today's world"
  "As we know"
  "Ever wondered"
  "What if you could"
  "One might say"
  "It could be argued"
  "It's important to note"
  "as you can see"
  "keep in mind"
)

# Only check staged markdown files
FILES=$(git diff --cached --name-only --diff-filter=ACM -- '*.md' | grep -v 'docs/superpowers/' || true)

if [ -z "$FILES" ]; then
  exit 0
fi

ERRORS=0

for word in "${BANNED_WORDS[@]}"; do
  for file in $FILES; do
    matches=$(grep -in "\b${word}\b" "$file" 2>/dev/null || true)
    if [ -n "$matches" ]; then
      echo "SLOP: '$word' found in $file:"
      echo "$matches" | sed 's/^/  /'
      ERRORS=$((ERRORS + 1))
    fi
  done
done

for pattern in "${BANNED_PATTERNS[@]}"; do
  for file in $FILES; do
    matches=$(grep -in "$pattern" "$file" 2>/dev/null || true)
    if [ -n "$matches" ]; then
      echo "SLOP: pattern '$pattern' found in $file:"
      echo "$matches" | sed 's/^/  /'
      ERRORS=$((ERRORS + 1))
    fi
  done
done

# Check for em dashes
for file in $FILES; do
  matches=$(grep -n "—" "$file" 2>/dev/null || true)
  if [ -n "$matches" ]; then
    echo "SLOP: em dash (—) found in $file:"
    echo "$matches" | sed 's/^/  /'
    ERRORS=$((ERRORS + 1))
  fi
done

if [ $ERRORS -gt 0 ]; then
  echo ""
  echo "$ERRORS slop issue(s) found. Fix before committing."
  echo "For full audit: /audit-docs in Claude Code"
  exit 1
fi
