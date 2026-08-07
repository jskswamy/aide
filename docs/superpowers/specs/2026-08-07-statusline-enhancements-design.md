# Aide Statusline Enhancements

**Status:** Approved
**Date:** 2026-08-07
**Builds on:** [2026-08-03-statusline-design.md](2026-08-03-statusline-design.md)

---

## Problem

The v1 statusline (`aide statusline claude`) has four gaps found in daily use:

1. **Misleading "sandboxed" state.** `sandbox` and `network` modules treat
   any `AIDE_SANDBOX`/`AIDE_NETWORK_MODE` value other than the explicit
   `off`/`unrestricted` as "on". When the agent wasn't launched through aide
   at all — the env vars are completely absent, not explicitly `off` — the
   statusline still renders the sandboxed-on icon. That's wrong: it's not
   that sandboxing is on, it's that aide isn't managing this session.
2. **One combined string, no composability.** `aide statusline claude`
   always prints every enabled module joined by `" | "`. Tools like
   [ccstatusline](https://github.com/sirmalloc/ccstatusline) let users
   compose a statusline from independent "Custom Command" widgets (each
   invoked separately, on its own line or position) — there's no way to
   plug a single aide module in as one widget.
3. **Agent must be named explicitly.** `aide statusline claude` requires
   the caller to know and pass the agent name, even when it's already
   knowable from context (an aide-launched session already carries
   `AIDE_AGENT`).
4. **ccstatusline capability is hand-rolled per user.** Making aide's
   sandboxed Claude process able to read ccstatusline's own config
   (`~/.config/ccstatusline/settings.json`) currently requires each user to
   define a custom capability and opt into it per context.
5. **TTY invocation is confusing.** Running `aide statusline claude`
   directly in a terminal (not piped from Claude Code) prints a wall of
   usage text instead of showing what the statusline would actually look
   like — there's no quick way to preview it.

---

## Design Overview

Four changes, one spec:

- **Unmanaged state** — `sandbox` and `network` modules gain a third,
  user-configurable state rendered when their env var is completely absent
  (vs. explicitly off/outbound).
- **`--module` flag** — render a single module's output instead of the
  joined string, so each module can back one ccstatusline Custom Command
  widget.
- **Agent auto-detection** — `aide statusline` (no positional agent) infers
  the agent from context; `--agent`/positional `claude` remain valid
  explicit overrides.
- **`ccstatusline` builtin capability** — ship the capability definition in
  aide's builtin registry, auto-included in a context's effective
  capability set when aide detects the tool is installed.
- **TTY preview** — invoking the render path directly in a terminal renders
  the same output a real invocation would, instead of printing help text.

---

## CLI Surface

```bash
aide statusline                       # render, auto-detect agent
aide statusline claude                # render, explicit agent (unchanged)
aide statusline --agent claude        # render, explicit agent (new form)
aide statusline --module sandbox      # render just the sandbox module
aide statusline --module sandbox --module network  # render just these two, joined
aide statusline claude --module network  # explicit agent + single module
aide statusline claude --install      # unchanged
aide statusline claude --remove       # unchanged
```

`--module` is repeatable and accepts any name from `order`: `sandbox`,
`network`, `caps`, `trust`, `context`, `auto_approve`. When one or more are
given, `renderStatusline` filters `order` down to the requested subset
(still honoring their disabled/empty-hides-output rules), preserving
configured `order` rather than flag-passed order, and joins the results
with `" | "` — one flag prints one module's bare string with no join
overhead; multiple flags behave like a scoped combined output. Omitting
`--module` entirely is fully backward compatible with v1's combined
output (all modules, same join).

`--install`/`--remove` are unaffected: they already resolve a
`provision.Context` (which carries `.Agent`) via
`resolveContextForMutation` + `provision.ResolveContext`, and continue to
require a resolvable agent (positional or `--agent`).

---

## Agent Resolution (render mode)

Resolution order, first match wins:

1. **Explicit** — `--agent <name>` flag, or the legacy positional
   (`aide statusline claude`).
2. **`AIDE_AGENT` env var** — set by the launcher whenever aide launched
   this session (see `internal/launcher/launcher.go`); the common case.
3. **Stdin JSON sniff** (piped mode only, `AIDE_AGENT` absent) — attempt to
   parse stdin as Claude Code's status-line JSON payload (has
   `session_id`/`model`/`workspace`-shaped fields). A match resolves to
   `claude`. This is the only agent wired to statusline rendering today;
   sniffing other agents' shapes is added when their statusline support
   lands.
4. **Context lookup** (TTY mode only, nothing above resolved) — resolve the
   aide context for the current working directory the same way
   `--install`/`--remove` already do (`resolveContextForMutation` →
   `provision.ResolveContext`), and use `context.Agent`.
5. **Default** — if no context matches either (no aide config, or CWD
   matches nothing), fall back to `"claude"`.

If resolution fails to produce *any* agent (should only happen if steps 1-5
somehow all fail), print an error rather than guessing further.

---

## TTY Preview

Today, invoking the render path with stdin attached to a terminal prints
static usage text. This changes to: run the same render logic as piped
mode (resolving the agent per the order above, reading `AIDE_*` env vars,
applying `--module` if set) and print the result, so a user can run
`aide statusline claude` (or bare `aide statusline`) directly to see
exactly what would show up in their statusline.

Stdin draining (`io.Copy(io.Discard, os.Stdin)`) only applies to piped
mode — there's nothing to drain from a TTY, and step 3 (JSON sniff) only
applies when stdin is a pipe in the first place, since a TTY has no JSON to
sniff.

Cobra's built-in `--help`/`-h` handling is unaffected and continues to
print full flag usage; this change only replaces the previous ad hoc
TTY-detection branch.

---

## Config Schema: Unmanaged State

`sandbox` and `network` modules each gain a third state field, following
the existing "empty string hides it" convention:

```yaml
statusline:
  sandbox:
    on: "🔒"
    off: "🔓"
    unmanaged: "❓"    # new: shown when AIDE_SANDBOX is entirely absent
  network:
    outbound: "🌐"
    unrestricted: "🌍"
    unmanaged: "❓"    # new: shown when AIDE_NETWORK_MODE is entirely absent
```

Rendering must distinguish "env var absent" from "env var present with any
other value" — today's `map[string]string` extraction in `envForRender()`
collapses both to `""`. The render path needs to preserve presence (e.g.
`os.LookupEnv` at each check, or a presence-aware map) so the three cases
(`on`/`off`/`unmanaged` and `outbound`/`unrestricted`/`unmanaged`) are each
reachable.

```go
case "sandbox":
    v, ok := lookupEnv("AIDE_SANDBOX")
    switch {
    case !ok:
        return cfg.Sandbox.Unmanaged
    case v == "off":
        return cfg.Sandbox.Off
    default:
        return cfg.Sandbox.On
    }
```

Same shape for `network`. Other modules (`caps`, `trust`, `context`,
`auto_approve`) already hide correctly when their env var is absent — no
change needed there.

---

## ccstatusline Builtin Capability

Add to `internal/capability/builtin.go`, alongside the other builtins:

```go
{
    Name:        "ccstatusline",
    Description: "Beautiful highly customizable statusline for Claude Code CLI with powerline support, themes, and more.",
    Readable:    []string{"~/.config/ccstatusline/settings.json"},
}
```

**Auto-inclusion:** at context resolution time, if
`~/.config/ccstatusline/settings.json` exists on disk, add `ccstatusline`
to the context's effective capability set even when its `capabilities:`
list doesn't mention it — same mechanism class as other filesystem-presence
checks aide already does, not a new "default-on for everyone" concept.
Users without the file installed see no behavior change; users who already
list `ccstatusline` explicitly are unaffected (idempotent add).

---

## Testing

- Unit tests for the presence-vs-value distinction on `sandbox`/`network`
  rendering (absent / off / on; absent / unrestricted / outbound).
- Unit tests for `--module` output matching the corresponding slice of the
  combined-mode output, for each module name individually and for multiple
  `--module` flags combined (verifying configured-order, not flag-order,
  joining).
- Unit tests for the agent-resolution order: explicit flag wins over
  `AIDE_AGENT`; `AIDE_AGENT` wins over stdin sniff; stdin sniff only
  attempted when piped; context lookup and `claude` default only attempted
  in TTY mode.
- Unit test confirming TTY invocation renders output instead of the old
  help text, and that `--help` still shows flag usage.
- Unit test for `ccstatusline` capability auto-inclusion: present when the
  settings file exists, absent (and not silently duplicated) when it
  doesn't or when already explicitly listed.

---

## Implementation Locations

| Component | Location |
|---|---|
| `--module` flag, agent resolution, TTY preview | `cmd/aide/statusline.go` |
| Stdin JSON sniff for Claude Code | `cmd/aide/statusline.go` (new helper) |
| Context-based agent fallback | reuse `resolveContextForMutation` / `provision.ResolveContext` (already in this file) |
| `Unmanaged` schema fields | `internal/config/schema.go` |
| Presence-aware env lookup | `cmd/aide/statusline.go` (`renderModule`, `envForRender` or replacement) |
| `ccstatusline` builtin + auto-inclusion | `internal/capability/builtin.go` (definition), capability resolution path (auto-include check) |

---

## Out of Scope

- Sniffing stdin JSON shapes for agents other than Claude Code — added
  when those agents get statusline rendering support.
- ccstatusline package installation/management by aide — this spec only
  grants sandbox read access to its config; installing/configuring
  ccstatusline itself remains the user's responsibility.
- A general "default-on capability" mechanism — the ccstatusline
  auto-inclusion is a targeted, presence-based check, not a new opt-out
  model for all builtin capabilities.
- `agent` module rendering — still out of scope per the original spec.
