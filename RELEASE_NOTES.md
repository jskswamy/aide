## Unreleased

### 🧹 Refactor

- **`internal/fsutil.AtomicWrite`.** Four packages each rolled their
  own "marshal-then-tmp-rename" helper with subtly different error
  messages, permissions, and tmp-naming. Durability semantics
  (0o600 file, 0o750 parent MkdirAll, cleanup on rename failure) are
  now owned in one place and consumed by `approvalstore`,
  `config.WriteConfigTo`, `config.WriteProjectOverride`,
  `secrets.Manager.EditFromContent`, and `secrets.Rotate`.
- **`internal/homepath` for `~`/`$HOME` expansion.** Five packages
  each carried their own tilde helper with subtly divergent
  semantics (lone `~` accepted in some, ignored in others; trailing
  slashes preserved only by seatbelt's concat-based variant; inverse
  collapse direction in `internal/diag`). One `homepath.Expand` plus
  `homepath.Collapse` now own those rules; the seatbelt
  `gitdir:`-trailing-slash regression test still passes against the
  shared implementation.
- **`internal/sliceutil.Dedup` plus stdlib generics.** Order-
  preserving string Dedup was reimplemented three times with a
  fourth int variant. `containsString`, `removeString`,
  `removeFromSlice`, `copyStrings/Ints`, `copyVisited`, and `joinCSV`
  reimplemented things `slices`/`maps`/`strings` already provide.
  All now route through `sliceutil.Dedup` or stdlib equivalents.
