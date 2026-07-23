## v2.0.1 — 2026-07-23

This is a security patch release. No new features or breaking changes.

### 🔒 Security

#### Dependency vulnerabilities patched

Two upstream vulnerabilities addressed by upgrading affected packages.

- `golang.org/x/text` v0.37.0 → v0.39.0: fixes GO-2026-5970, an infinite
  loop triggered by malformed Unicode input.
- Go toolchain go1.26.4 → go1.26.5: fixes GO-2026-5856, a privacy leak in
  `crypto/tls` Encrypted Client Hello (ECH).

#### Hook script permissions tightened to 0o700

Generated hook scripts (Gemini bash wrapper, Hermes `__init__.py`) were
written world-executable (0o755). They now use 0o700 (owner-execute only).

- Resolves gosec G302 alerts #111 and #113.
- `fsutil.AtomicWriteExecutable` is now the single place that pins the
  correct permission; callers no longer call `os.Chmod` separately, closing
  a race window between the write and the chmod.
