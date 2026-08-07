# `aide cap list` Status Column Design

**Status:** Approved
**Date:** 2026-08-06

---

## Problem

`aide cap list` shows every available capability (built-in and user-defined)
with its name, source, and description, but gives no indication of whether
any of them are actually active for the current project. A user has to
separately run `aide cap audit` (or reason about their config by hand) to
find out what's enabled right now.

Investigating `aide cap audit` while designing this surfaced a real, separate
bug: when no `--context` flag is given, `capAuditCmd` resolves the applicable
context's *name* via `aidectx.Resolve` (which correctly merges any
`.aide.yaml` project override on top of the matched context — see
`internal/context/resolver.go`'s `applyProjectOverride`), then discards that
merged result and re-fetches the raw, unmerged context by name via
`resolveContextForMutation`. Project-level capability changes (`aide cap
enable X` at project scope, or `disabled_capabilities` in `.aide.yaml`) are
silently ignored by `cap audit`'s default output.

`resolveContextForMutation` itself is not wrong — its callers
(`capEnableCmd`/`capDisableCmd` in `--global` mode) need the raw, unmerged
context because they write back to it, and persisting an already
project-merged value into the global config would be incorrect. The bug is
that `capAuditCmd`, a read-only command, reuses a mutation-oriented helper
and inherits semantics that don't fit its purpose.

---

## Design Overview

### Shared resolution helper

Add `resolveEffectiveCapabilities(cwd, contextName string) (name string, caps []string, err error)` in `cmd/aide/commands.go`, alongside the existing `resolveContextForMutation`:

- **`contextName == ""`** (the common case): resolve via `aidectx.Resolve(cfg, cwd, remoteURL)` and return `rc.Name`, `rc.Context.Capabilities` — the fully project-override-merged list. This answers "what's actually active here."
- **`contextName != ""`**: look up `cfg.Contexts[contextName]` directly, no project-override merge — matching the existing, established precedent in `sandboxTestCmd`'s `--context` handling. Project overrides are tied to *this directory*, not to an arbitrary named context being inspected; applying them to a manually-named context the user is peeking at (which may not even be the context this directory would normally resolve to) would be misleading, not more correct.
- Unknown `contextName`: return an error, matching `cap show --context`/`cap audit --context`'s existing behavior.

Both `capListCmd` and `capAuditCmd` route through this helper. `capAuditCmd`'s default (no `--context`) path switches from `resolveContextForMutation` to this helper, fixing the merge bug. Its `--context <name>` path is unchanged (it was already doing the raw lookup, which was already correct for that case).

### `cap list` STATUS column

Add a `--context <name>` flag to `capListCmd`, matching `cap show`/`cap audit`.

Add a `STATUS` column, positioned right after `NAME` (before `SOURCE`):

```
NAME                 STATUS       SOURCE       DESCRIPTION
aws                  -            built-in     AWS CLI and credentials
clipboard            enabled      built-in     Read/write access to the system clipboard (pasteboard)
go                   suggested    built-in     Go toolchain
ssh                  disabled     built-in     SSH keys, agent, and outbound SSH transport (port 22 + custom). Required for: git over SSH, ssh login, scp/rsync.
```

Rationale for placement: `STATUS` is the dynamic, per-context fact the user is scanning for; keeping it adjacent to `NAME` means reading down that column doesn't require first skipping past `SOURCE`.

Four possible values, computed per capability name against the full registry `capListCmd` already iterates:

1. **`enabled`** — the name appears in the effective capabilities list from `resolveEffectiveCapabilities`.
2. **`disabled`** — the name appears in `cfg.ProjectOverride.DisabledCapabilities`. Only possible when a `.aide.yaml` exists for `cwd` and no `--context` was given (project overrides don't apply to an explicitly-named context per the resolution rule above, so this state never shows in that mode).
3. **`suggested`** — the name is *not* enabled, but its markers match this project, via `capability.DetectProject(os.DirFS(cwd))` — the same function `internal/launcher/launcher.go` already calls to decide whether to prompt for detection consent at launch. This only covers built-in capabilities (matching `DetectProject`'s existing scope — it iterates `Builtins()`, not the merged registry); user-defined capabilities never show `suggested`, unchanged from today's detection behavior.
4. **`-`** — none of the above; the common case.

Precedence: `enabled` > `disabled` > `suggested`. In practice these never actually collide (a name can't be both enabled and disabled given how the merge works, and `suggested` is only computed for names that aren't enabled), but `enabled` wins if some future edge case produced overlap.

### Error handling

- No resolvable context at all (empty config, no matching rule, no `default_context`): `enabled`/`disabled` can't be determined for any row. `cap list` must keep working exactly as it does today with zero config (per the existing "Allow check to work even without config" pattern in `capCheckCmd`/`capSuggestForPathCmd`) — every row shows `suggested` or `-`, no error, no crash.
- `--context <name>` naming a context that doesn't exist: error out, matching `cap show --context`/`cap audit --context`.
- `DetectProject` only needs the filesystem (`os.DirFS(cwd)`), so `suggested` detection still runs even when context resolution fails entirely.

---

## Changes

1. **`cmd/aide/commands.go`**: add `resolveEffectiveCapabilities(cwd, contextName string) (string, []string, error)`.
2. **`cmd/aide/cap.go`**:
   - `capListCmd`: add `--context` flag; add `STATUS` column computation and rendering.
   - `capAuditCmd`: switch its default (no `--context`) path to `resolveEffectiveCapabilities`.
3. **Tests**:
   - `cmd/aide/commands_test.go` (or new `commands_capabilities_test.go`): unit tests for `resolveEffectiveCapabilities` — no override, project override adding/removing capabilities, explicit `--context`, unknown context name.
   - `cmd/aide/cap_test.go`: table-driven tests for per-row state computation (`enabled`/`disabled`/`suggested`/`-`); `capListCmd` output test asserting column order and values against a temp dir with a marker file and/or `.aide.yaml`; `capAuditCmd` regression test proving a project-level `disabled_capabilities` entry is now reflected (would fail before this change).

---

## Testing

- Unit tests for `resolveEffectiveCapabilities` covering all four branches above.
- Unit tests for state computation, table-driven against a fake registry and fake effective-capabilities list — no real filesystem or config needed for this part.
- `capListCmd` integration-style test: temp dir with a marker file (e.g. `go.mod`) and a minimal `.aide.yaml`, asserting the rendered `STATUS` column.
- `capAuditCmd` regression test for the merge-bug fix.
- Existing `cap list`/`cap audit` tests continue to pass, with only the expected new-column diff for `cap list`'s golden output (if any exists).

---

## Out of Scope

- Extending `DetectProject`/`suggested` state to user-defined capabilities (would require markers on user-defined entries, which `MergedRegistry` doesn't currently detect-scan).
- Any change to `aide cap show`'s per-capability detail view — it already shows full resolved detail for one capability and isn't context-aware in the same sense `list`/`audit` are.
- Any change to the consent/detection prompt flow at launch time (`internal/launcher/launcher.go`) — `suggested` in `cap list` is read-only information, not a trigger for the interactive consent prompt.
- Making `cap list`'s column layout configurable or machine-parseable (e.g. `--json`) — out of scope for this change; `suggest-for-path` remains the only command explicitly designed for machine consumption.
