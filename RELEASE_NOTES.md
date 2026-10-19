## Unreleased

### ✨ New

#### A built-in ponytail capability

When ponytail is set up (`~/.config/ponytail` exists, or Skillshare has
it installed), aide now turns on a `ponytail` capability by itself.
Before this, the ponytail band could not record each task and showed
`ponytail band: could not write the task ledger`, because the sandbox
did not let it write `~/.config/ponytail/band`.

- Read/write access to `~/.config/ponytail/band` only.
- Turn it off with `--without ponytail`, or add it by hand with
  `--with ponytail`.
- Default XDG location only; a custom `XDG_CONFIG_HOME` is not followed.
