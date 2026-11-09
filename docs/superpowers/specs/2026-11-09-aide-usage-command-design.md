# aide usage — ccusage-powered per-profile usage reporting

*   **Author:** subramk (shaped with OpenCode)
*   **Status:** Proposed
*   **Date:** 2026-11-09

--------------------------------------------------------------------------------

## Overview

A person running several aide profiles (personal, work, forge) across several
harnesses (claude, codex, pi, opencode) cannot answer "how much did this
profile burn today?" or "where is my 5-hour window?" without hand-building
environment variables. ccusage already parses every one of those harnesses'
local data and supports comma-separated directory lists per source — but it
knows nothing about aide profiles, and aide knows every profile's config dir
exactly (`Context.NativeConfigDir`). `aide usage` closes that gap: it resolves
the profile the aide way, builds the env vars ccusage reads, and execs it. A
companion setup mode writes the ccusage config that labels pi-format session
stores per profile, something only aide can derive correctly because it owns
the profile registry.

## The Problem

ccusage takes data locations through `CLAUDE_CONFIG_DIR`, `CODEX_HOME`,
`PI_AGENT_DIR`, `OPENCODE_DATA_DIR` and friends — the same variables aide
sets per profile at launch. Doing this by hand means remembering the mapping,
globbing `~/.claude-*`, and keeping a wrapper script in sync when profiles
are added or renamed. The failure mode is silent: a mistyped or unset
variable makes ccusage fall back to the default dirs, and you read the wrong
profile's usage with no error. For pi-format stores (pi, omp) ccusage offers
named stores in `ccusage.json`, but deriving those from aide profiles by hand
duplicates the registry aide already maintains.

## Goals

*   `aide usage [ccusage-args...]` resolves the profile from CWD (the same
    `aidectx.Resolve` a launch uses) and runs ccusage scoped to that
    profile's data dirs. `aide usage daily`, `aide usage blocks --active`,
    `aide usage session` all pass straight through.
*   `aide usage --all` merges every profile's dirs (comma-separated, existing
    dirs only) for a whole-machine view.
*   `aide usage --profile NAME` scopes to a named profile from anywhere.
*   `aide usage --setup` writes `~/.claude/ccusage.json` `pi.stores` derived
    from the profile registry (one named store per isolated profile's pi
    sessions dir), preserving other keys, with a TTY diff prompt and
    `--force` for scripts.
*   A missing `ccusage` binary fails with an install hint, never a stack of
    wrong data.

## Non-Goals

*   Re-implementing usage parsing, pricing, or reports. ccusage is the
    engine; aide only routes data dirs to it. ccusage is an external
    dependency resolved on `PATH`, not vendored.
*   Sandboxing the ccusage run. It reads the user's own agent data;
    `aide usage` runs unsandboxed like `aide status` or `aide secrets edit`.
*   Server-side quota APIs (Anthropic 5h/weekly percentages, Antigravity
    pools). ccusage blocks are log-derived estimates; the authoritative
    number stays the harness's own `/usage` inside the session.
*   Live monitoring, dashboards, tmux/statusline rendering.
*   Harnesses without profile support in aide (`SupportsProfiles() ==
    false`: aider, amp, cursor-agent, goose, hermes, zed, …). Antigravity is
    not an aide harness; ccusage's own default discovery covers it.

## Anti-Goals

*   Failure is acceptable; **wrong-profile data is not**. If a profile has no
    data dir for a harness, that source must report "no data", never fall
    back to another profile's (or the global) dir.
*   `--setup` must not silently clobber a hand-maintained `ccusage.json`.

## Constraints

*   Follow the existing cobra pattern: `usageCmd()` in `cmd/aide/usage.go`,
    one line in `registerCommands` (`cmd/aide/commands.go`).
*   Pure, table-testable logic goes in `internal/usage`; `cmd/aide/usage.go`
    stays a thin cobra shell, matching how `resolveContextForMutation` keeps
    `commands.go` thin.
*   Env construction must use `Context.NativeConfigDir(h, home)` and
    `harness.All()` — never re-derive `~/.<h>-<profile>` by string
    formatting, and never glob the filesystem for profile dirs.
*   Tests stay hermetic against the real home directory (temp-dir fixtures,
    matching existing `cmdenv` tests).

## Approaches

*   **A — exec-wrapper, CWD-scoped by default (picked).** `aide usage daily`
    in a work-matched repo shows only work's usage; `--all` merges. Most
    aide-idiomatic: it mirrors `aide which`. Risk: a user expecting the
    merged ccusage default is surprised — mitigated by `--all` and the help
    text.
*   **B — exec-wrapper, merged by default.** `aide usage` behaves like bare
    ccusage across all profiles; `--profile` scopes. Rejected: the whole
    point is aide's profile resolution; merging discards it and makes the
    common per-profile question take a flag.
*   **C — aide also owns the ccusage pi-store config (picked, with A).**
    `--setup` derives `pi.stores` from the profile registry. Rejected as
    *the whole feature* (a generated file can't scope a run by CWD), picked
    as the complement: it gives per-profile *labels* in merged pi reports,
    which env vars cannot express.
*   **D — embed parsing in aide.** Rejected: duplicates ccusage's per-source
    parsers and their maintenance treadmill (19 sources and counting).

## Design

### Command surface

```
aide usage [flags] [--] [ccusage args...]

Flags:
  --all              merge all profiles' data dirs
  --profile NAME     scope to a named profile (overrides CWD resolution)
  --setup            write/refresh pi.stores in the ccusage config, then exit
  --force            with --setup: write without the interactive diff prompt
```

`--all` and `--profile` are mutually exclusive. Bare `aide usage` (no ccusage
args) runs `ccusage daily`.

### Scope → environment mapping

For each in-scope profile and each descriptor from `harness.All()` with
`ProfileEnvKey != ""`, compute `dir = ctx.NativeConfigDir(h, home)` and map:

| aide harness | ccusage env var | value | list-capable |
|---|---|---|---|
| claude | `CLAUDE_CONFIG_DIR` | `dir` | yes |
| codex | `CODEX_HOME` | `dir` | yes |
| copilot | `COPILOT_HOME` | `dir` | **no — single root** |
| pi | `PI_AGENT_DIR` | `dir` + `/sessions` | yes, but see the pi rule |
| opencode | `OPENCODE_DATA_DIR` | `dir` (holds the profile's `opencode.db`) | yes |
| gemini | `GEMINI_DATA_DIR` | `dir` + `/tmp` | yes |

**The pi rule (load-bearing):** ccusage derives its *default* pi store from
`PI_AGENT_DIR` and rejects any named store in `pi.stores` that overlaps it
(`Invalid ccusage config: ... paths overlap default pi store`). The two
mechanisms are therefore mutually exclusive: env vars scope a run, named
stores label a merged run. `PI_AGENT_DIR` is set **only in profile scope**;
`--all` never sets it — pi coverage in the merged view comes from the
default store plus the `pi.stores` written by `--setup` (and isolated pi
profiles are simply absent from `--all` until `--setup` has run; the help
text says so).

**Single-root rule:** in `--all`, list-capable vars merge comma-separated;
single-root vars (`COPILOT_HOME`) take the first existing dir and print a
warning naming the profiles whose dirs were left out.

This table and both rules are not hardcoded policy — they are the
`ccusageBackend` entry's `HarnessEnv`, `ListCapable`, and `AllExclude`
data (see *Backend seam* below).

Semantics per scope:

*   **Profile scope (default / `--profile`):** set each var to the computed
    path **even when the dir does not exist**. Unified ccusage reports skip
    missing dirs; focused reports error visibly. This is the no-wrong-data
    guarantee: ccusage's built-in fallbacks never get a chance to read
    another profile's (or the global) dir.
*   **`--all`:** collect dirs across all profiles, keep existing dirs only,
    dedupe preserving order, join with `,` — but only for list-capable vars,
    and never `PI_AGENT_DIR` (the pi rule above). Profiles without
    `SelectsConfigDir()` contribute the harness default dir via
    `NativeConfigDir`, so global data is included naturally. A var with zero
    existing dirs is left unset (ccusage defaults cover the global dirs).
*   **Scrub first:** start from `os.Environ()` minus every var in the table
    above (plus `PI_CODING_AGENT_DIR`, `PI_CODING_AGENT_SESSION_DIR`,
    `OPENCODE_CONFIG_DIR`, `OPENCODE_CONFIG`, `GEMINI_HOME`,
    `XDG_DATA_HOME`), so a stale shell or outer-session export cannot leak
    across scopes.

### Exec

The command is always registered and its help/completion always render —
visibility never depends on PATH state. The `exec.LookPath("ccusage")`
check runs **only on the exec path** (never in `PreRun`): on failure,
print

```
ccusage not found on PATH — install it: brew install ccusage · nixpkgs: pkgs.ccusage
```

and exit 1. `--setup` performs no exec, so it works with ccusage
uninstalled — the config can be bootstrapped first. Otherwise run with the
computed env, stdio attached, and pass through ccusage's exit code. Inside
an aide-launched session the session's own `CLAUDE_CONFIG_DIR`/`CODEX_HOME`
is scrubbed and recomputed; CWD resolution lands on the same profile, so
behavior is consistent.

### Backend seam (designed for a second tool)

v1 ships ccusage as the only backend, but behind a deliberately-shaped,
data-driven seam so that adding a tool is a one-entry change in one file —
not a redesign, and not a plugin system built before it is needed:

```go
// internal/usage
type Backend struct {
    Name        string              // "ccusage"
    Binary      string              // looked up on PATH (exec path only)
    InstallHint string              // printed when Binary is missing
    HarnessEnv  map[string]string   // aide harness -> backend env var
    ListCapable map[string]bool     // env var accepts comma lists (--all)
    AllExclude  []string            // vars --all must never set (the pi rule)
    ScrubExtra  []string            // backend-specific vars to scrub
    Setup       func(...) error     // writes backend config (pi stores); nil = none
}
```

*   v1 defines exactly one entry, `ccusageBackend`. The pi rule and the
    single-root rule live here as *data* (`AllExclude: ["PI_AGENT_DIR"]`,
    `ListCapable: {..., "COPILOT_HOME": false}`) because they are
    ccusage-semantics, not aide-semantics — a second backend brings its own.
*   `--setup` dispatches through `Backend.Setup`; a backend with `Setup ==
    nil` answers "backend X has no setup step".
*   Resolution in v1 is `backends[0]`. When a second backend lands, the
    extension path is: pick the first found on PATH, override with
    `--backend NAME` and an optional `usage.backend` config key — the flag
    and key are added *then*, at the command's existing flag site, not now.
*   Out of scope regardless: dynamic loading, out-of-tree backends, a
    registry package. "Easy to add" means: one `Backend` literal, its
    `Setup` func if any, and tests — in `internal/usage` and nowhere else.

Why not more: toktrack's persistent cache merges profiles (wrong data by
design for us) and openusage.sh reads fixed single dirs, so there is no
second tool worth serving today; the seam exists so that changes with one
entry instead of a refactor.

### `--setup`: pi named stores

Derive stores from the registry, not the filesystem:

1. For each profile with `SelectsConfigDir()` whose pi dir
   (`NativeConfigDir("pi", home)`) differs from the harness default: emit
   `{name: <profile>, path: <dir>/sessions}`. Profile names are sanitized to
   ccusage's `^[a-z][a-z0-9_-]{0,31}$` (lowercase, invalid runs → `-`);
   collisions after sanitizing are an error, not a silent rename.
2. The default store (`~/.pi/agent/sessions`) is never listed — ccusage
   reads it unless `PI_AGENT_DIR` is set, and overlap with a named store is
   rejected by ccusage.
3. If `~/.omp/agent/sessions` exists and no profile's pi dir covers it, add
   `{name: "omp", path: "~/.omp/agent/sessions"}` (matches ccusage's own
    documented example). *[Author's call — drop if it surprises.]*
4. Validate the full set for path overlap before writing; on overlap, print
   the offenders and exit 1 without touching the file.
5. Write target: `~/.claude/ccusage.json`. ccusage's config search path is
   `./.ccusage/ccusage.json` plus `ccusage.json` in each resolved claude
   dir — so this file is found whenever `~/.claude` is among them (`--all`,
   default-profile scope, bare ccusage), and *not* found in a non-default
   profile scope (`~/.claude-forge`); that is fine and intended, because
   profile scope carries pi data via `PI_AGENT_DIR` instead of stores.
   Preserve all other top-level keys via a JSON round-trip; only `pi.stores`
   is replaced. On a TTY, show the merged file as a diff and require
   confirmation; `--force` writes without asking. A pre-existing
   hand-written `pi` key is shown in the diff like any other change — the
   prompt is the guard.

### Files touched

*   `cmd/aide/usage.go` (new), `cmd/aide/commands.go` (one line),
    `cmd/aide/usage_test.go` (new)
*   `internal/usage/usage.go`, `internal/usage/usage_test.go` (new):
    scope resolution, env construction, scrub list, store derivation
*   `docs/cli-reference.md`: `aide usage` section
*   Shell completion: profile names for `--profile`, mirroring the existing
    completion helper pattern

### Testing

*   `internal/usage`: table-driven env construction (profile scope sets
    missing paths; `--all` filters to existing + dedupes; scrub removes
    leaked vars); store derivation incl. sanitize, collision, overlap.
*   `cmd/aide/usage_test.go`: a fake `ccusage` shell script on a temp PATH
    captures argv and env; assert scope behavior, `--` passthrough, exit-code
    passthrough, and the not-found message.
*   `--setup`: golden JSON round-trip tests (other keys preserved), TTY
    prompt bypassed under `--force`.

### Acceptance

*   `cd ~/source/github.com/forge-platform && aide usage daily` reports
    forge claude data only (diff against
    `CLAUDE_CONFIG_DIR=~/.claude-forge ccusage claude daily`).
*   `aide usage --all daily` covers `~/.claude`, `~/.claude-forge`,
    `~/.claude-work` (and codex/opencode equivalents that exist), leaves
    `PI_AGENT_DIR` unset, and — after `--setup` — shows pi usage labeled per
    store with no `paths overlap default pi store` error.
*   `aide usage --setup` produces a `~/.claude/ccusage.json` whose
    `pi.stores` labels `pi-forge`, `pi-work`, `omp`; a subsequent
    `ccusage daily` shows those agents labeled.
*   `PATH` without ccusage → the install-hint error, exit 1.

### Builder-verification points

*   ccusage's opencode source reads `<OPENCODE_DATA_DIR>/opencode.db` (v2
    sqlite) and/or `storage/message` JSON — confirm the profile config dir
    layout aide produces (`OPENCODE_DB=<dir>/opencode.db`) is one of the
    shapes ccusage discovers; if not, note the gap in cli-reference and leave
    opencode out of the env table.
*   `aide usage` in a repo whose resolved profile has no `SelectsConfigDir()`
    must equal bare ccusage for that harness (global dirs).

## Open Questions

None that change what gets built. If `--setup`'s prompt proves annoying in
practice, a `[aide]` marker section in the JSON can be added later without
changing the command surface.
