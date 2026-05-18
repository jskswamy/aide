## v1.12.0 — 2026-05-18

### ✨ New

- **`cursor-agent` added to known agents.** aide now recognises Cursor's CLI (`cursor-agent`) alongside the
  existing seven agents. The shorter `agent` symlink shipped by Cursor's installer is intentionally not registered - use
  `aide --agent cursor-agent`.

### 📦 Dependencies

- **Bump `github.com/go-git/go-git/v5` to v5.19.0.** Aligns object
  encoding with upstream, hardens commit and tag verification
  against malformed signatures, and pulls in newer `sha1cd` /
  `go-billy`. Replaces a Dependabot PR that previously failed CI
  on an unrelated coverage gate.

### 🛡 Security

- **Validate env-derived agent config-dir overrides.** The seatbelt
  module's per-agent config-dir resolver now refuses values pointing
  at sensitive home subdirs (`.ssh`, `.aws`, `.gnupg`, `.gpg`,
  `.config/gcloud`, `.azure`, `.kube`, `.docker`, `.netrc`,
  `.git-credentials`) and any path outside `$HOME`. Without this
  guard, `XDG_CONFIG_HOME=$HOME/.ssh` (or a hostile
  `*_CONFIG_DIR` pointing into `.aws`) would inject a writable
  subpath rule into the agent's sandbox. Env-derived overrides are
  also tilde-expanded to absolute paths before validation so
  Seatbelt subpath rules match the syscalls the agent actually
  makes.

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

### 🛠 Dev workflow

- **Greptile rules force threat-model exercise.** The previous
  rules read as a vocabulary — naming threat categories without
  requiring reviewers to trace a PR's inputs to its rule operands.
  Converted rules from labels to procedures: added a Confidence
  and Gate Coupling section that ties the reported confidence to
  the count of open high-severity findings, plus an explicit
  threat-modelling pass on env-controlled paths, symlinks, and
  per-peer-file consistency.
