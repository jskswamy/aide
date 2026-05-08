## v1.9.0

### ✨ Features

- **`--diagnose` flag** for surfacing child-agent failures. When set,
  aide switches from `syscall.Exec` (process replacement) to fork+exec
  so it stays alive after the child exits, captures the last N lines /
  bytes of stderr, and writes a redacted markdown post-mortem to
  `~/.cache/aide/diagnose/<id>.md` (mode 0600). A compact terminal
  summary is also printed. Secret values are redacted (only env-var
  names and lengths recorded); paths and argv are not, so users are
  reminded to review the file before sharing.
- **`--diagnose-trace` flag** (macOS only). Post-hoc queries
  `log show --predicate 'sender == "Sandbox"'` and attaches matching
  sandbox-denial rows to the report.
- **Tunable capture limits** via `AIDE_DIAGNOSE_STDERR_LINES`
  (default 200) and `AIDE_DIAGNOSE_STDERR_BYTES` (default 65536).

### 🐛 Fixes

- `install.sh`: corrected `sudo` invocation example.

### 🔧 Internal

- New `internal/diag` package: typed redaction surface (`Report`,
  `EnvKey`), pre/post collector with sensitive-flag list (handles
  `--key=value` and `--key value`, with underscore→dash normalization),
  markdown renderer with golden-file tests, and a secret-aware writer
  with stderr fallback.
- `cmd/aide` now propagates the child exit code via `errors.As` on
  `interface{ ExitCode() int }` instead of forcing `exit=1`.
