# `aide context` UX Redesign

**Status:** Draft
**Date:** 2026-05-01

## Problem

`aide context *` has three concrete UX failures:

1. **`add-match` is jargon.** "Match rule" is internal vocabulary. Users
   asking "how do I add this folder to my context?" don't reach for
   `add-match`; they reach for `add`.
2. **`add` overloads "add" with two meanings.** `aide context add`
   creates a *new* context; `aide context add-match` attaches a folder to
   an *existing* one. Reading them side by side, `add` is ambiguous —
   which is exactly why users land on the wrong one and accidentally
   create a second context.
3. **Launcher refuses on empty state.** When CWD doesn't match any
   context and `default_context` is unset, `aide` errors with "no
   context matched". This is the user's most painful failure mode (new
   project, fresh checkout, worktree in `/tmp`) and it's a dead end.

## Goals

- Single mental model for "make this folder resolve to a context":
  one verb to attach to existing, one verb to create new.
- The launcher's empty-state guides the user through a successful first
  run instead of refusing.
- Out-of-scope: secret-store internals, agent definitions, project
  overrides. The `set-secret` / `remove-secret` / `set-default`
  subcommands are already referenced by shipped error hints; they stay
  unchanged.

## Command surface — old → new

| Old | New | Notes |
| --- | --- | ----- |
| `aide context add` | `aide context create [name]` | Create new context; can also bind cwd at the end. |
| `aide context add-match` | `aide context bind <name>` | Attach cwd to existing context. Removed; no alias. |
| `aide context list` | (unchanged) | |
| `aide context rename <old> <new>` | (unchanged) | |
| `aide context remove <name>` | (unchanged) | |
| `aide context set-secret <name>` | (unchanged) | Used in shipped error hints. |
| `aide context remove-secret` | (unchanged) | |
| `aide context set-default [name]` | (unchanged) | Used in shipped error hints. |

Clean break — no aliases for `add` / `add-match`. Project has no active
user base, so a deprecation cycle adds maintenance burden with no
benefit.

## `aide context bind`

Attach the current folder to an existing context.

```
aide context bind <name>             # auto-detect match (git remote if repo, else folder path)
aide context bind <name> --path      # force exact folder path match
aide context bind <name> --remote    # force git remote match (error if not a git repo)
aide context bind                    # interactive picker over existing contexts
```

### Match rule resolution (auto-detect)

1. If cwd is inside a git repo AND `git remote get-url origin` succeeds,
   match by the remote URL.
2. Otherwise match by the exact folder path.

The git-remote default is durable: the same context resolves correctly
for any worktree or fresh checkout of the same repo, including
short-lived ones in `/tmp`. Folder-path matches only work for the exact
path that was bound.

`--path` and `--remote` override the default. `--remote` errors if the
folder is not a git repo with an `origin` remote.

### Strictness when the named context doesn't exist (hybrid TTY-aware)

- **TTY:** prompt `Context "work" doesn't exist. Create it now? [y/N]`.
  On `y`, fall through to the same flow as `aide context create work`.
- **Non-TTY:** hard error
  `context "work" not found. Run: aide context create work`.

### Output

```
Bound this folder to context "work" (matched by remote git@github.com:foo/bar.git)
```

## `aide context create`

Create a new context. Can bind cwd at the end.

### Interactive form

```
aide context create                  # walks: name → agent → secret? → bind here?
aide context create work             # name pre-filled
```

In TTY mode the wizard asks (in order):

1. **Name** — pre-filled if a positional was given.
2. **Agent** — autodetect: if exactly one supported agent is on PATH, use
   it without prompting and print `Using agent: claude (auto-detected)`.
   If zero or multiple, prompt with the list.
3. **Secret store** — optional, free-text or pick from existing stores.
4. **Bind this folder?** — `[Y/n]`, default Yes. If No, the context is
   created with no match rules.

### Scripted form

```
aide context create work --agent claude --secret-store firmus --here
aide context create work --no-here
```

| Flag | Effect |
| --- | --- |
| `--agent <name>` | Set the agent without prompting. |
| `--secret-store <name>` | Bind a secret store at create time. |
| `--here` | Bind cwd as a match rule (auto-detect like `bind`). |
| `--no-here` | Skip cwd binding entirely. |

Any flag provided suppresses its prompt. In non-TTY mode, missing
required information (name, agent when none can be auto-detected) is a
hard error pointing at the relevant flag.

### Output

```
Created context "work" (agent: claude).
Bound this folder (matched by remote git@github.com:foo/bar.git).
```

## Launcher empty-state

When `aidectx.Resolve` returns "no context matched" AND
`cfg.DefaultContext == ""`, the launcher invokes a new empty-state flow
instead of returning the error verbatim.

### TTY mode

```
aide: no context matches this folder.

What do you want to do?
  [1] Bind this folder to an existing context
  [2] Create a new context for this folder
  [3] Launch once with an existing context (don't save)
  [c] Cancel

Choose [1]:
```

- `[1]` — invoke the same flow as `aide context bind` (interactive
  picker over existing contexts). On success, re-resolve and continue
  the launch with the now-bound context.
- `[2]` — invoke the `aide context create` flow (cwd-binding default Yes
  applies). On success, continue the launch.
- `[3]` — list contexts, user picks one. Launch with that context for
  this invocation only; no config write. Equivalent to
  `aide use <name> -- <agent-args>`.
- `[c]` — exit cleanly with nonzero status.

The default option is `[1]` because users almost always have an existing
context they meant to use; first-time-ever launches are rare compared to
"I'm in a folder that doesn't match my work context yet".

### Non-TTY mode

Hard error with all four options listed as concrete commands:

```
aide: no context matches this folder, and no default_context is configured.

To proceed, run one of:
  aide context bind <name>            # attach this folder to existing context
  aide context create [name]          # create a new context for this folder
  aide use <name> -- <agent-args>     # launch once without persisting
  aide context set-default <name>     # use a fallback for unmatched folders
```

No env-var escape hatch — CI should fail loudly. Pipelines that need a
default can set `default_context` in the global config or pass
`aide use`.

## Implementation notes

- Delete `contextAddCmd` and `contextAddMatchCmd` from
  `cmd/aide/context.go`. Add `contextBindCmd` and `contextCreateCmd`.
- The interactive wizard logic shared by `create` and the empty-state
  `[2]` path lives in a single helper (e.g. `runCreateWizard`) so both
  surfaces produce identical prompts.
- The empty-state flow itself lives in a small new file (e.g.
  `internal/launcher/empty_state.go`) so `launcher.Launch` stays
  focused. `Launch` calls into it only when `Resolve` errored AND
  `DefaultContext == ""`.
- TTY detection: `term.IsTerminal(int(os.Stdin.Fd()))`, mirroring how
  the existing banner detects TTY.
- Match-rule auto-detect uses `aidectx.DetectRemote(cwd, "origin")`,
  already imported by neighboring code.
- The create wizard's secret-store step is free-text (matching the
  current `add` wizard's behavior). The store picker helper
  (`selectSecret`) was removed during the recent env-set redesign and
  does not need to come back — typing a name is one keystroke per
  character and the existing `set-secret` flow validates the name
  against disk afterwards.

## Testing

- Cobra-level parsing tests for `bind` and `create`, mirroring the
  `env_set_test.go` pattern. Cover:
  - `bind <name>` with TTY-isolated tempdir (uses XDG/HOME isolation
    helper from `projectTempDir`)
  - `bind <name>` with `--path`, `--remote`, and `--remote` outside a
    git repo (error)
  - `bind <missing>` non-TTY → "context not found"
  - `create work --agent X --no-here` happy path
  - `create work --agent X` in non-TTY without `--no-here` → no binding
  - Mutually-exclusive flag matrix where applicable
- Empty-state launcher: unit-test the new helper with a fake
  reader/writer. One TTY path that picks `[3]` and verifies the
  one-shot launch override; one non-TTY path that asserts the exact
  hint string. Use a stub Launcher to avoid spawning a real agent.
- Examples-as-tests guard (already shipped) catches any `Long:`
  drift introduced by the new help text.

## Migration / docs

- `docs/cli-reference.md` — new rows for `bind` and `create`; remove
  `add` and `add-match` rows.
- `docs/environment.md` — add a brief "binding a folder to a context"
  paragraph cross-linking to the new `bind` doc.
- `docs/getting-started.md` (new) — walk a first-time user through the
  empty-state flow; document both the TTY interactive path and the
  non-TTY scripted path.
- Sweep all README/docs for residual references to `add-match` or
  `aide context add`.

## Open questions

- Whether `bind` should also accept a path argument like
  `bind <name> /some/other/path` for binding folders other than cwd.
  Current draft: cwd only; out of scope. A future flag (`--at <path>`)
  could be added without breaking the design.
- Whether the empty-state `[3]` flow should print a one-line tip after
  the launch finishes ("This was a one-time override; run
  `aide context bind <name>` to make it permanent."). Probably yes;
  small enough to fold into implementation without re-spec'ing.
