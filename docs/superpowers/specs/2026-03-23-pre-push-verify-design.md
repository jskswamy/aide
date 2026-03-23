# Pre-Push Verification Plugin

**Date:** 2026-03-23
**Status:** Approved

## Problem

aide needs pre-push verification to catch build failures, test regressions, and release config issues before changes reach the remote. This should work both inside Claude Code sessions and for manual pushes from the terminal.

## Design

### Dual Implementation

**1. Claude Code Plugin Hook** — Intercepts `git push` commands issued via Claude Code's Bash tool. A PreToolUse hook on Bash that pattern-matches `git push`, runs verification, and blocks on failure.

**2. Pre-commit Framework Hook** — A `local` hook in `.pre-commit-config.yaml` at the `pre-push` stage. Both implementations call the same verification script so there's one source of truth.

### Plugin Structure

```
.claude/plugins/pre-push-verify/
├── plugin.json           — Plugin manifest
└── hooks/
    └── pre-push.sh       — Verification script (shared by both hook systems)
```

### Verification Steps

Executed in order, fails fast on first failure:

1. **`make build`** — Catches compile errors
2. **`make test`** — Catches test regressions
3. **`goreleaser release --snapshot --clean`** — Catches release config issues (archive naming, ldflags, etc.)

### Error Handling

- All failures block the push unconditionally (no branch exceptions, no skip mechanism)
- Each step's stdout/stderr is shown for diagnosis
- If `goreleaser` is not installed, the snapshot step fails intentionally

### Claude Code Hook Details

- **Event:** PreToolUse on Bash
- **Match:** Commands containing `git push`
- **Behavior:** Runs `hooks/pre-push.sh` from plugin root. Exit code 0 allows push, non-zero blocks it.

### Pre-commit Integration

Added as a `local` repo entry in `.pre-commit-config.yaml`:

```yaml
- repo: local
  hooks:
    - id: pre-push-verify
      name: Pre-push build, test, and release verification
      entry: .claude/plugins/pre-push-verify/hooks/pre-push.sh
      language: script
      stages: [pre-push]
      pass_filenames: false
```

## What This Does NOT Include

- No lint step (pre-commit already runs golangci-lint)
- No branch-aware behavior (all pushes are gated)
- No skip/override mechanism
- No notification or reporting beyond stdout/stderr
