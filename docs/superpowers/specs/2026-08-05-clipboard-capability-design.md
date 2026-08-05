# Clipboard Capability Design

**Status:** Approved
**Date:** 2026-08-05

---

## Problem

Commit `6cff2ea4` ("Fix Claude Code image paste in aide sandbox") added a
single Seatbelt rule — `(allow mach-lookup (global-name
"com.apple.pasteboard.1"))` — directly inside `claudeAgentModule.Rules()` to
fix Ctrl+V image paste. This introduced a regression: copying text back out
of the sandbox stopped working.

Root cause, confirmed empirically against aide's own generated sandbox
profile (`aide sandbox test`):

- `pbcopy` run under the profile **exits 0** (reports success) but the
  write never reaches the real host pasteboard — a silent no-op, not a
  denial.
- Before `6cff2ea4`, `com.apple.pasteboard.1` had no rule at all, so
  `pbcopy` failed outright (permission denied). Well-behaved clipboard
  code that tries a native write and falls back to a safe alternative
  (e.g. a terminal OSC-52 escape sequence) correctly fell back — so copy
  worked, just not through the pasteboard.
- After `6cff2ea4`, `pbcopy`'s false-success exit code defeats that
  fallback logic, so text copy silently goes nowhere while paste (a pure
  read) keeps working.

The actual fix isn't a Mach-IPC subtlety. Aide's Claude module rules are
ported from [eugene1g/agent-safehouse](https://github.com/eugene1g/agent-safehouse)
(see the file header in `claude.go`), which ships a **dedicated clipboard
integration** (`profiles/55-integrations-optional/clipboard.sb`) that grants
three `mach-lookup` global-names, not one:

```
(allow mach-lookup
    (global-name "com.apple.pasteboard.1")   ;; Pasteboard service used by pbcopy/pbpaste.
    (global-name "com.apple.lsd.mapdb")      ;; Launch Services type lookups required by pbcopy.
    (global-name "com.apple.lsd.modifydb")
)
```

`com.apple.lsd.mapdb`/`com.apple.lsd.modifydb` are Launch Services lookups
`pbcopy` needs for UTI/type resolution when writing a pasteboard flavor;
without them the write silently no-ops instead of erroring. This exact
ruleset was verified against aide's real generated profile: both `pbcopy`
(write) and `pbpaste` (read) round-trip correctly to the real host
clipboard. The original commit ported only one of the three required
rules from upstream — an incomplete port, not an OS-level limitation.

Separately, clipboard access today is wired into the `claude` agent
module specifically (`pkg/seatbelt/modules/claude.go`), not the capability
system (`internal/capability`). Only Claude sessions get it, unconditionally,
with no way to grant it to other agents or disable it independently of the
whole Claude module.

---

## Design Overview

Add `clipboard` as a new built-in capability in `internal/capability/builtin.go`,
using the corrected upstream ruleset, and remove the ad hoc rule from
`claude.go` entirely. Clipboard access becomes an explicit, opt-in,
agent-agnostic capability like `aws`, `docker`, etc., rather than something
bundled into one agent's module.

```go
"clipboard": {
    Name:        "clipboard",
    Description: "Read/write access to the system clipboard (pasteboard)",
    Allow: []string{
        `mach-lookup (global-name "com.apple.pasteboard.1")`,
        `mach-lookup (global-name "com.apple.lsd.mapdb")`,
        `mach-lookup (global-name "com.apple.lsd.modifydb")`,
    },
},
```

`Capability.Allow` entries are operation expressions, not full rule text: the
filesystem guard substitutes each entry into `(allow %s)`
(`pkg/seatbelt/guards/guard_filesystem.go:119-125`, exercised by
`TestFilesystem_ExtraAllow`). Three separate `mach-lookup (global-name
"...")` entries render as three independent `(allow mach-lookup
(global-name "..."))` rules — functionally identical to upstream's single
rule with three grouped global-names, since SBPL evaluates each `(allow op
filter)` independently regardless of grouping.

Key decisions:

- **Single capability, both directions.** No `clipboard-read`/`clipboard-write`
  split and no `Direction` sub-mode field. The OS pasteboard is one resource
  reached through one Mach service; splitting it would be new capability
  surface with no real precedent (`network`'s "inbound/outbound" is
  descriptive text only — `NetworkMode` has no actual directional code path).
- **Opt-in, not default-enabled.** Matches the deny-by-default philosophy —
  clipboard is a real exfiltration/injection vector. No `Markers` for
  auto-detection; it's a runtime preference, not a project characteristic.
- **macOS only.** `Allow` rules feed into `SandboxPolicy.ExtraAllow`, which
  the Darwin/Seatbelt builder consumes but the Linux Landlock/seccomp
  builder never reads (confirmed in `internal/sandbox/linux.go`) — so this
  capability is naturally inert on Linux with zero special-casing needed.

---

## Changes

1. **`internal/capability/builtin.go`**: add the `clipboard` capability
   above.
2. **`pkg/seatbelt/modules/claude.go`**: delete the "osascript clipboard
   access" `SectionAllow`/`AllowRule` block added in `6cff2ea4`.
3. **`pkg/seatbelt/modules/claude_test.go`**: delete
   `TestClaudeAgent_ClipboardMachService` (clipboard no longer lives in the
   Claude module).
4. **`internal/capability/builtin_test.go`**: add a test asserting the
   `clipboard` capability's `Allow` rules contain all three `global-name`
   entries — the previous test only checked for `com.apple.pasteboard.1`,
   which is exactly the gap that let an incomplete port ship unnoticed.

---

## Migration / Behavior Change

Clipboard access moves from "automatic for Claude sessions" to "opt-in for
any agent." This is a breaking change for existing Claude users relying on
image paste: they must add `clipboard` to their context's `capabilities:`
list (or pass `--with clipboard`) after this ships.

`RELEASE_NOTES.md` gets an entry framing this as a deliberate least-privilege
change (clipboard is now visible and auditable in a user's capability list,
and available to every agent, not just Claude) alongside the copy-out bug
fix, not just a bug fix.

---

## Testing

- Unit test on the new `clipboard` capability's `Allow` rules (all three
  `global-name` entries present).
- Remove the now-obsolete Claude-module clipboard test.
- Manual verification (recorded as a plan step, not automated): run
  `pbcopy`/`pbpaste` under the profile generated by
  `aide sandbox test --with clipboard` to confirm real host round-trip
  before/after — this class of bug (false-success exit code) doesn't
  surface in unit tests that only check rule text.

---

## Out of Scope

- GUI app launching capability (separate spec; upstream's
  `55-integrations-optional/macos-gui.sb` is a useful reference — it
  depends on this `clipboard` capability plus window-server/accessibility/TCC
  Mach services).
- ccstatusline default-capability support (separate spec).
- Linux clipboard (X11/xclip, Wayland/wl-clipboard) — materially different
  mechanism, not requested.
