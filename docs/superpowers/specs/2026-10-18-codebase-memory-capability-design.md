# Make codebase-memory-mcp work in the macOS sandbox

Status: draft for review. macOS only; Linux is untested and out of scope
until a Linux VM is available.

An earlier draft of this spec proposed a built-in `codebase-memory`
capability and a new `traverse` field. Review (a builder check and a
ponytail pass) showed the baseline policy already grants almost
everything the tool needs, and an experiment confirmed one missing rule
is enough. This version replaces the earlier design.

## Intent

`codebase-memory-mcp` fails inside an aide sandbox on macOS:

```text
codebase-memory-mcp: secure CLI coordination could not be created
(cache-private): /Users/<user>/.cache/codebase-memory-mcp: ancestry
component validation failed (errno 1)
```

Before it uses its cache directory the tool walks the path from `/`
down, opening each component with
`openat(O_RDONLY|O_DIRECTORY|O_NOFOLLOW)` (`src/daemon/ipc.c`,
`private_directory_tree_open`). Opening a directory for reading needs
`file-read-data` on that directory, not just `file-read-metadata`.

## What the baseline already grants

From `pkg/seatbelt/guards`:

- `/`: `file-read-data` (`guard_system_runtime.go`, "Root-level
  traversal").
- `/Users`: `file-read-metadata` only. This is the gap.
- `$HOME`: `file-read-data` as a literal, and `file-read-metadata` on the
  whole subtree (`guard_filesystem.go`, "Home directory traversal").
- `~/.cache`: `file-read* file-write*` as a subtree ("Build caches"),
  which already covers `~/.cache/codebase-memory-mcp`.
- `/private/tmp` and `/tmp`: readable by the system subpath list, which
  covers the daemon's socket directory for reads. Write access there is
  unverified.

## Measured result (2026-10-18, default profile, real CLI)

`aide sandbox test` policy plus the extra rule, running
`codebase-memory-mcp cli --json list_projects` under `sandbox-exec`:

| Extra rule | Result |
|---|---|
| none | fails with the error above |
| read/write on the cache subtree only | still fails |
| `(allow file-read-data (literal "/Users"))` only | passes |
| `/Users` + cache read/write | passes |
| literals for `/Users`, `~`, `~/.cache` | passes |

So one literal on the parent directory of `$HOME` fixes the default
case. Cache grants are unnecessary because the baseline already allows
`~/.cache`.

## Goals

- The MCP server and the CLI work under aide's default sandbox on macOS
  with no user configuration.
- The grant is a single literal directory: the process can open and list
  `/Users` (names only). No subtree grant is added anywhere.

## Non-goals

- A `codebase-memory` capability, a `traverse` field, a launch-time
  resolver or auto-include rules. None are needed for the default
  location.
- Supporting `CBM_CACHE_DIR` outside `~/.cache`. Such a cache needs the
  user's own `readable_extra`/`writable_extra` (and the tool then also
  checks that path's ancestors). Document this; do not special-case it.
- Linux, bivouac, ponytail and episodic-memory capabilities (separate
  specs).
- MCP registration (Skillshare owns it).

## Design

In `guard_system_runtime.go`, extend the "Root-level traversal"
`file-read-data` literal list from `/` to also include `/Users`. Add
the parent directory of `$HOME` as a literal as well when the guard has
the home directory and it is not `/Users` (homes outside `/Users`, for
example on an external volume). Literals only; never a subpath.

Update the guard's tests: the rendered policy contains the new literal,
and does not contain any new subpath.

## Security analysis

The rule is `file-read-data` on `(literal "/Users")` and, for a home
outside `/Users`, on the literal parent of `$HOME`.

- What it allows: opening and listing that one directory, so the process
  sees entry names (other local accounts, `Shared`).
- What it does not allow: reading anything inside any entry. A `literal`
  matches exactly that path. The sandbox's access to another account's
  home stays what it was (no metadata, no data).
- Deny rules still win. Credential and sensitive-directory denies
  (`~/.ssh`, `~/.aws`, ...) are subtrees below `$HOME` and do not overlap
  the literal. The existing deny-ordering test in
  `internal/sandbox/darwin_test.go` already pins that denies follow
  allows, so no new ordering test is needed.
- Already accepted: the baseline grants `file-read-data` on `/` and on
  `$HOME`, and `file-read-metadata` on `/Users`. The new capability is
  listing one more directory's names.
- Residual risk: the agent learns that other local accounts exist. This
  is an information leak on a shared machine, not an access path.
- Bounds on the parent literal: one directory level only; never emitted
  when the parent is `/` or `/Users` (already granted); never a subpath.
- Tests pin the shape: the guard test asserts the literals and that
  neither `/Users` nor an external-volume parent appears as a subpath.

A full threat model is not warranted for this change. Linux needs one
before any equivalent grant, because Landlock rules are recursive.

## Verification (macOS)

Done, 2026-10-18, on the default profile:

- Real CLI under the rebuilt baseline policy, no extra rules:
  `codebase-memory-mcp cli --json list_projects` exits 0.
- Claude through aide, asked to call the MCP `list_projects` tool:
  returns the project count. The same prompt through the installed aide
  with the old policy fails with `CONNECTION_CLOSED`.
- Pi through aide, same prompt: returns the project count.
- Guard and sandbox test suites pass.

Not done:

- Cold start (no cache directory, no daemon). It needs the account's
  daemon stopped first; an attempt with a different `CBM_CACHE_DIR`
  collided with the running daemon and proved nothing.
- A profile with a restrictive custom `sandbox.readable`, and a home
  path containing a space (the guard test covers a space in the
  parent literal only).
- Whether the daemon socket directory under `/private/tmp` is writable
  under the baseline. The warm runs above worked, which suggests yes.

## Left to the builder

- Where the home-parent literal comes from (guard context) and its test
  seam.
- The exact wording of the docs note about `CBM_CACHE_DIR`.

## Rollout

Add a note under Internal or Sandbox in `RELEASE_NOTES.md`: the default
macOS policy now lets the process list `/Users`, so codebase-memory-mcp
starts. Users can delete any hand-written grants for this tool after
upgrading; that is their step.
