## Unreleased

### 🧹 Refactor

- **`internal/fsutil.AtomicWrite`.** Four packages each rolled their
  own "marshal-then-tmp-rename" helper with subtly different error
  messages, permissions, and tmp-naming. Durability semantics
  (0o600 file, 0o750 parent MkdirAll, cleanup on rename failure) are
  now owned in one place and consumed by `approvalstore`,
  `config.WriteConfigTo`, `config.WriteProjectOverride`,
  `secrets.Manager.EditFromContent`, and `secrets.Rotate`.
