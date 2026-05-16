## Unreleased

### ✨ New

- **`cursor-agent` added to known agents.** aide now recognises Cursor's CLI (`cursor-agent`) alongside the
  existing seven agents. The shorter `agent` symlink shipped by Cursor's installer is intentionally not registered - use
  `aide --agent cursor-agent`.

### 🐛 Bug fixes

- **`internal/fsutil` coverage above CI gate.** `AtomicWrite`'s
  error branches (parent-dir creation failure, temp-file creation
  failure, rename-onto-non-empty-dir) had no tests, leaving the
  package at 45.5% — under the 60% per-package threshold ci.yml
  enforces. Added three error-path tests using POSIX-reliable
  traps (file-where-dir-expected, chmod 0500 on parent, rename
  onto non-empty directory) plus a leftover-temp-file assertion
  on the rename-failure cleanup path. Coverage now 63.6%. The
  residual 8 statements are `Chmod`/`Write`/`Close` failure
  cleanups on an open `*os.File` — provoking those reliably
  needs a fs-fault-injection seam in production code, deferred
  as a separate concern.
