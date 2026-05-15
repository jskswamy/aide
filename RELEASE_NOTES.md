## Unreleased

### 🐛 Bug fixes

- **SSH commit signing keys auto-granted by `git-integration`
  guard.** When git is configured with `gpg.format = ssh`, the
  always-on `git-integration` guard now resolves `user.signingkey`
  from the user's global gitconfig (and `[includeIf]`-included
  configs) and emits a narrow `file-read*` allow for the resolved
  key and its `.pub` sibling, plus a read+write rule on
  `~/.ssh/known_hosts` for TOFU on first push. Previously, agents
  inside the sandbox had to be told `readable_extra: [~/.ssh/
  id_ed25519_signing, …]` per context, and `git commit` failed with
  a misleading `Couldn't load public key … No such file or directory`
  from `ssh-keygen -Y sign`. The grant is scoped: symlinks are
  resolved, paths escaping `$HOME` are refused, literal `ssh-…`
  pubkey strings produce no rule, and the parser only ever reads
  global/included configs — never `<project>/.git/config` — so a
  hostile repo cannot inject a path into the allow set (regression-
  tested).

### 🔒 Security

- **Trust store hash format hardened against newline-in-path
  collisions.** `trust.FileHash` and `trust.PathHash` previously
  encoded `path + "\n" + contents` without length prefixing, leaving
  a path containing a newline able to impersonate a different
  `(path, contents)` pair. Both functions now build keys through a
  shared `internal/hashutil` Builder that uses length-prefixed,
  version-tagged encoding (`trust-v1-file` and `trust-v1-path`),
  matching the existing safer encoding already used by `consent`.

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
- **CLI scope guard centralized.** Fourteen cobra `RunE` bodies
  across `aide sandbox`, `aide env`, and `aide cap` opened with the
  same `if !global && contextName != ""` guard, and three of them
  reimplemented the `{outbound, none, unrestricted}` validation
  inline. New `validateContextScope` and `runScopedMutation` helpers
  in `cmd/aide`, plus `config.ValidNetworkModes` and
  `config.ValidateNetworkMode`, collapse the guards onto one helper
  and the network-mode literal onto one declaration.
- **`internal/hashutil` shared digest builder.** Trust, consent, and
  evidence digests now build keys through a single length-prefixed
  `Builder` with explicit version tags. Adds
  `approvalstore.Store.Sub` so sibling aggregates wire their
  sub-namespaces in one place, plus `consent.Status.String` to match
  `trust.Status`'s contract.
- **Seatbelt agent module skeleton collapsed.** Five of the six
  bundled agent modules (aider, amp, codex, gemini, goose) now
  declare themselves through a data-only `modules.AgentSpec` instead
  of carrying an identical struct + constructor + Name + Rules
  skeleton each. The unused `seatbelt.Section` alias is removed
  (`SectionAllow` was always identical), and the three home-path
  rule constructors share a private `homeExpr` helper.
- **`sandbox.Paths` parameter object and `Policy.ToSeatbeltContext`.**
  `PolicyFromConfig` and `DefaultPolicy` previously took 3–4
  positional path strings — `homeDir` and `tempDir` could be swapped
  at a call site without a compile error. They now take a named
  `sandbox.Paths` struct. `EvaluateGuards` and the darwin profile
  generator no longer carry parallel inline `Policy → Context`
  copies; both go through a new `Policy.ToSeatbeltContext`. As a side
  effect, the `SSHPorts` field that had drifted out of the darwin
  copy is now consistent across both paths.

### ⚠️ Breaking

- **Existing `trust`/`deny` records become invalid on upgrade.** The
  hash format change above is intentional — the legacy `path + "\n"
  + contents` encoding had no migration path that preserved the
  collision-safety invariant. Users will be prompted to re-trust
  `.aide.yaml` files on first interaction after the upgrade. No
  silent acceptance of stale records.
