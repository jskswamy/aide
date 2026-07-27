## v2.0.2 — 2026-07-27

This is a bug fix release. No new features or breaking changes.

### Bug Fixes

#### Detect duplicate context binds across URL schemes

`aide context bind` would silently duplicate a match rule if the same
repository was already bound to a different context, including when the
remote URL differed only in scheme (https vs ssh vs git shorthand).

- Same-context rebind exits early with an "already bound" message (no-op)
- Cross-context collision in TTY mode prompts the user to abort or move
  the binding to the new context
- Cross-context collision in non-TTY mode returns a clear error with
  instructions to re-run interactively
- URL normalization reuses the existing `ParseRemoteHost` function so
  `https://github.com/org/repo` and `git@github.com:org/repo.git` are
  correctly identified as the same repository
