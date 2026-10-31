# OpenCode profile data and state dirs

*   **Author:** nithyanatarajan
*   **Status:** Proposed
*   **Date:** 2026-10-31

--------------------------------------------------------------------------------

## Overview

A person who keeps work and personal OpenCode profiles apart expects one profile's project files and prompts to stay out of the other. A profile that selects an OpenCode config dir now keeps OpenCode's data and state directories there too, next to the database that [OpenCode profile database isolation](opencode-profile-database-isolation.md) already moved.

## The Problem

OpenCode writes snapshots of each project to `<data-dir>/opencode/snapshot/` so it can undo edits, and prompt history to `<state-dir>/opencode/prompt-history.jsonl`. Neither follows `OPENCODE_CONFIG_DIR` or `OPENCODE_DB`, so every profile writes to `~/.local/share/opencode` and `~/.local/state/opencode`.

The OpenCode sandbox module grants those directories to every OpenCode launch, so one profile can read what another wrote. Observed on OpenCode 2.2.3 under a work profile:

*   A snapshot of a repository with 38,650 tracked paths left all of those paths in the shared `index`.
*   A snapshot of a repository with an untracked file listed that file and stored new git objects in the snapshot directory, so uncommitted content is stored there and not only file names.

## Goals

*   A launch that receives `OPENCODE_DB` also receives `XDG_DATA_HOME=<config-dir>/data` and `XDG_STATE_HOME=<config-dir>/state`, replacing any ambient or configured value.
*   A profile that has not selected a config dir does not set, clear, or rewrite either variable.
*   Commands that can start the shared background server do not receive either variable, for the same reason they do not receive `OPENCODE_DB`.
*   A `never_allow_env` entry that would strip either variable is an error before exec, not a silent return to the shared directories.

## Non-Goals

*   Moving `~/.cache/opencode`. On the observed install it holds `bin` and `npm`, which are tooling and not project content.
*   Copying or deleting anything already in the shared directories. Aide does not own those files.
*   A new sandbox writable root. Both directories sit inside the config dir, which the launch can already write.
*   Stopping OpenCode's child processes from inheriting the variables. OpenCode has no setting for this.

## Technical Plan

`openCodeProfileEnv` in `internal/launcher/opencode_db.go` wraps `openCodeProfileDB` and returns the three variables. The launcher calls it on the existing OpenCode path, under the same conditions, and merges the result after the sandbox so `clean_env` cannot drop it.

OpenCode has no variable for its snapshot path, and the one that moves it is the XDG data directory. OpenCode appends its own name, so snapshots land in `<config-dir>/data/opencode/snapshot/`.

## Trade-offs

**Child processes inherit `XDG_DATA_HOME`.** Commands that OpenCode's shell tool runs see the profile's directory. A tool that keeps data there looks empty inside OpenCode. Measured on the observed machine by setting the variable directly: `direnv status` on a repository already allowed reports it as not allowed, and `direnv exec` stops with "is blocked", and `uv tool list` reports no tools where it otherwise lists three. A person can run `direnv allow` again per repository, reinstall what a profile needs, or set the tool's own variable (`UV_TOOL_DIR`, `UV_PYTHON_INSTALL_DIR`) in the profile's `env`. None of these remedies was tried. This is the main cost of the change.

**History in the shared directories stays behind.** Logs and prompt history start fresh. Sessions from before the change remain in the profile database while their snapshots remain in the old directory, so undoing an edit from such a session may fail.

**The config dir now holds project content.** Anything that copies or syncs the config dir copies snapshots too.

## Alternatives Considered

### Alternative A: Turn snapshots off for the profile

*   **Pros**: No child process sees a changed variable, and no project content is stored.
*   **Cons**: The person loses undo. It leaves prompt history shared. A per-profile setting for it on OpenCode 2.2.3 was not confirmed.
*   **Reason for Rejection**: It removes a feature to fix a storage location.

### Alternative B: Set the variables in the person's profile `env`

*   **Pros**: No code change.
*   **Cons**: Profile `env` applies to every harness in the profile, so Claude Code and Pi would also see them. Each person would have to find this gap.
*   **Reason for Rejection**: Separation is what a config dir already promises for OpenCode, so it belongs in the launcher and only on OpenCode launches.

### Alternative C: Deny the other profiles' directories in the sandbox

*   **Pros**: Files stay where OpenCode puts them.
*   **Cons**: All profiles share one path, so a deny blocks the profile's own snapshots too.
*   **Reason for Rejection**: There is no path to deny until the directories are split.

## Relation to the database isolation design

That design listed moving logs and state as a non-goal and rejected moving the data directory, because its goal was accounts and the account store is the database file. Its objection that a new writable root would be needed does not apply here, since these directories sit inside the config dir. Snapshots and prompt history are the new information, and they make this the "larger isolation project" that design set aside. It also expected `XDG_DATA_HOME` to move the cache, which OpenCode 2.2.3 does not do.

## Verification

Done on macOS, OpenCode 2.2.3, with a built binary and a real interactive session under a work profile, driven through `tmux`:

*   `debug paths` reports data, log and state inside the config dir, and the database at `<config-dir>/opencode.db`.
*   A session's snapshot landed in `<config-dir>/data/opencode/snapshot/`, and the shared snapshot directory gained no entry.
*   Prompt history landed in `<config-dir>/state/opencode/prompt-history.jsonl`, and the shared file gained no line.
*   `/undo` removed a file the session had created.
*   The shell tool sees `XDG_DATA_HOME` and `XDG_STATE_HOME` set to the config dir. One earlier session did not show `XDG_STATE_HOME`, and the cause is not known.
*   MCP stays connected.

Not done:

*   A real build or test in a work repository, and running installed `uv` tools rather than listing them.
*   Other tools that keep data under `XDG_DATA_HOME`.
*   Linux.
