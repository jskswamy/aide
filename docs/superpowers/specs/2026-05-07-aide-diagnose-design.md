# `aide --diagnose` — surfacing child agent failures

**Status:** Design
**Date:** 2026-05-07

## Problem

When the child agent dies with a generic message (e.g. `error: An unknown error occurred (Unexpected)`), aide passes the message through and exits. The user is left with no thread to pull. Aide already knows everything that would help — sandbox profile, env wiring, paths, exit info — but never surfaces it.

A real-world report (sarojdongol, 2026-05-07): closed a Claude session, reopened, got the message above. Aide's banner showed config and secrets resolved correctly; the failure was downstream and aide gave no diagnostic context.

## Goal

A single explicit knob that turns aide into a diagnosing wrapper around the child, producing a redacted, GitHub-pasteable report. No automatic post-mortems on the happy path.

## Non-goals

- Auto-detection of cryptic failures and auto-dumping. Rejected: adds noise on every non-zero exit (Ctrl-C, agent self-exit, etc.).
- Linux trace-mode parity in v1.
- Uploading reports anywhere. Local file only.
- A `--debug` alias. Cobra suggests on typo.
- Re-running the failed invocation automatically. `--diagnose` wraps the *current* invocation.

## User-facing surface

Two flags, both off by default:

- `--diagnose` — wraps the current child run; on exit, prints a terminal summary and writes a full markdown report. No behavior change for the child.
- `--diagnose-trace` — implies `--diagnose`, additionally runs the child under a permissive *log-not-deny* sandbox profile and captures macOS sandbox denial events for the child PID. Heavier; opt-in because the child runs under a different profile.

**Always-on signpost (no auto-dump):** when the child exits abnormally and `--diagnose` was *not* set, append one line to stderr:

```
hint: re-run with 'aide --diagnose' to capture a diagnostic report.
```

"Abnormal" deliberately *excludes* user-initiated and clean shutdowns. The hint must not fire when:

- Child was killed by `SIGINT` (exit 130) — user pressed Ctrl-C.
- Child was killed by `SIGTERM` (exit 143) — supervisor stop.
- Child was killed by `SIGHUP` (exit 129) — terminal closed.
- Child exited 0 — clean exit, including agent slash-commands like `/exit` and `/quit` that return 0.

The hint *does* fire for any other non-zero exit, including non-zero exits from slash-commands if the agent surfaces them that way (rare; if a real-world agent exits non-zero on `/exit`, treat it as a bug in our trigger and add it to the suppression list rather than train users to ignore the hint).

This is the only behavior change on the happy path.

**Tweakable via env (documented in `--help` and README):**

- `AIDE_DIAGNOSE_STDERR_LINES` (default `200`)
- `AIDE_DIAGNOSE_STDERR_BYTES` (default `65536`)

Whichever limit hits first wins. No CLI flag, no config-file key — env var only, since these are throwaway tuning knobs, not durable preferences.

## Architecture

New package `internal/diag` with three components, separated for testability:

1. **`collector`** — gathers static facts (versions, cwd, profile, env keys, secret sources) before exec, dynamic facts (exit code, signal, runtime, stderr tail) after exec. Knows nothing about formatting or files. Single chokepoint for redaction.
2. **`renderer`** — turns a `Report` struct into markdown for the file and a compact summary for the terminal. Pure function; golden-file tested.
3. **`writer`** — picks the path (`~/.cache/aide/diagnose/<RFC3339-timestamp>-<short-hash>.md`, where `<short-hash>` is the first 8 chars of a SHA-256 over `cwd|argv|pid` to disambiguate concurrent runs), writes the file, falls back to stderr on any I/O error so the report is never lost.

`internal/launcher` gains one integration point: when `--diagnose` is set, it switches the exec strategy. Today `launcher` uses `syscall.Exec` (process replacement) — after that call aide ceases to exist and no post-mortem is possible. Diagnose mode instead uses `exec.Cmd` (fork+exec), keeping aide alive as a parent to:

- tee the child's stderr into a bounded ring buffer (limits from env vars above);
- observe `Wait()` to learn exit code, signal, runtime;
- hand collected facts to `diag` after the child returns.

The fork+exec path must:

- forward `SIGINT`, `SIGTERM`, `SIGHUP`, `SIGQUIT`, `SIGWINCH` to the child;
- set the child's stdin/stdout to aide's (passthrough TTY);
- run the child in its own process group so `Ctrl-C` reaches it normally;
- *not* tee stdout (so the agent's TUI is unaffected) — only stderr is captured.

The default execution path (no `--diagnose`) keeps `syscall.Exec` unchanged. This is important: changing the default exec strategy is out of scope and would alter signal/TTY semantics for every user.

`--diagnose-trace` adds:

4. **`tracer`** — builds a permissive variant of the resolved profile, runs the child under it, then parses `log show --last 5m --predicate 'sender == "Sandbox" AND processIdentifier == <child_pid>'` and attaches denials to the report.

### Open question on trace mode

macOS `sandbox-exec` may not provide a true "log-not-deny" mode. The seatbelt-deny-wins behavior (recorded in project memory: deny always wins over allow) suggests this is constrained. Two fallbacks if no log-not-deny mode exists:

- **Fallback A:** run child without `sandbox-exec` and gather denials from the *original* failed run via `log show` (read backward over the last few minutes). Different semantics: traces what already happened, not a re-run.
- **Fallback B:** widen the profile maximally (`(version 1) (allow default)`) and capture syscalls under `sandbox-exec -t` if the trace flag exists.

Pick during implementation after a quick spike. Document the chosen path in code comments.

## Data flow

```
aide --diagnose <args>
        │
        ▼
launcher.Run ── if diagnose ──▶ collector.Pre()  (snapshot config/profile/env-keys)
        │
        ▼
exec.Cmd ── stderr ──▶ ringbuf (bounded by env vars)
        │              │
        │              └─▶ stderr (passthrough, unchanged)
        ▼
exit ──▶ collector.Post(exit, runtime, ringbuf)
        │
        ▼
renderer.Markdown() ──▶ writer.WriteFile() ──▶ ~/.cache/aide/diagnose/<id>.md
        │                      │
        ▼                      └─ on error ─▶ stderr (full report)
renderer.Summary() ──▶ terminal
```

Trace mode inserts `tracer.WrapProfile()` before exec and `tracer.CollectDenials(pid)` after.

## Report contents

Markdown sections, in order:

1. **TL;DR** — one line: exit code, runtime, classification (`fast-fail (<500ms)` / `crashed mid-run` / `exited cleanly with non-zero`).
2. **Environment** — aide version/commit/date, OS, arch, shell, locale.
3. **Invocation** — cwd, resolved config path, agent binary, full argv. Env values redacted in argv too.
4. **Secrets wiring** — env keys injected (names + lengths), secret source paths, age key source. **Never values.**
5. **Sandbox** — variants enabled, guards active (names + types). Rendered `.sb` content in file only, not terminal summary.
6. **Child output** — last N lines of stderr (paths under `$HOME` rewritten to `~`).
7. **Sandbox denials** *(trace mode only)* — table of operation, path, pid.
8. **Reproduction** — one-liner the user can paste back: `cd <cwd> && aide --diagnose -- <argv>`.

## Redaction rules

Single chokepoint in `collector`:

- Env *values* never enter the `Report` struct. Only key names + `len(value)`.
- Secret values from sops never enter the struct. Enforce structurally: `Report` has no field that could hold a secret value; the type system prevents leaks.
- `$HOME` → `~` rewrite applied in renderer for user-shareable strings.
- Hostname → omitted (machine-identifying).
- Username → left as-is (already in `~` paths the user knows; over-redacting hurts the user paste-it-into-an-issue use case more than it helps).

## Error handling

- File-write failure → full report to stderr, exit code preserved from child.
- Trace-mode log capture failure → report still written without denials section, with a note explaining why (e.g., `log show` permissions).
- Stderr ring-buffer overrun → truncation marker `[…stderr truncated, N bytes dropped…]` at the boundary.
- `--diagnose-trace` on Linux → fail fast with clear message: *"trace mode is macOS-only in v1; --diagnose still works."*

## Testing

- **Unit (`collector`)** — fake env, fake profile, fake exit; assert no secret value appears in the rendered report (table-driven, includes red-team tests with secret-looking values planted in env).
- **Unit (`renderer`)** — golden-file tests for markdown and terminal summary.
- **Unit (`writer`)** — temp dir + simulated `ENOSPC`; assert stderr fallback fires and full content is preserved.
- **Unit (`tracer`)** — profile-wrapping is a pure transform; golden test on `.sb` output. Denial parsing fed canned `log show` output.
- **Integration** — fixture child binary that exits 1 fast, exits 1 slow, exits 0. `aide --diagnose` against each; assert file presence, redaction, summary content.
- **Manual** — re-run sarojdongol's repro with `--diagnose` and confirm the report identifies the failing layer.

## Documentation

- `aide --help`: document `--diagnose`, `--diagnose-trace`, the two env vars, and the failure-path hint.
- `README.md`: add a *"Diagnosing a failed run"* section with example output and a paste-ready bug-report flow.
- `CONTRIBUTING.md` (if applicable): tell maintainers to ask for the diagnose file in issue templates.

## Out of scope (explicit YAGNI)

Restated for emphasis:

- Auto-dump on cryptic failure.
- Linux trace mode.
- Network upload.
- `--debug` alias.
- Config-file key for buffer sizes.
- Auto-retry of failed invocation.
