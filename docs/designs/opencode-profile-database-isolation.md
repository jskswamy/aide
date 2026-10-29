# OpenCode profile database isolation

*   **Author:** jskswamy
*   **Status:** Proposed
*   **Date:** 2026-10-28

--------------------------------------------------------------------------------

## Overview

A person who uses two aide profiles with OpenCode expects a login made under one profile to stay there. Aide already sends each profile that has selected a config dir to its own config tree. OpenCode does not keep accounts or the last-selected model in that tree. It keeps them in one shared SQLite file. This change makes a profile that has selected a config dir open its own database file inside that directory on launches that start a private server, and on `debug paths`. It adds `--standalone` after the leaf command whose own help lists that flag, so those commands do not attach to the other profile's server.

The person this is for already has named OpenCode profiles and will usually find those profiles logged out after the change. Aide does not copy accounts. A leftover `auth.json` in the shared data directory can still be imported by OpenCode the first time the new database is created, so those leftover accounts are not isolated until that file is removed. The old database file is not deleted. The break is written in the release notes, and `aide profile migrate` says the accounts were not copied. A normal launch does not print that note.

## The Problem

OpenCode profiles in aide isolate the config tree and not the identity. A saved account or a model last used under alpha is still visible under beta, because both launches open the same database. Passing `--standalone` by hand does not fix a launch that never sets a different database path: each private server can still open the shared file. A relative database path does not fix it either. OpenCode resolves a relative path under the shared data directory.

The failure shows up as the wrong account, or the wrong model, with no error. Config, plugins, and an injected API key can differ while the login does not. A saved account also takes precedence over an API key injected for that launch, so the injected key is added beside the shared login instead of being the one that is used.

Leaving the shared file in place for a profile that has not selected a config dir is required. That launch, including the usual default profile, already has the accounts people use today. Moving it would orphan those accounts. A missing `config_dir_name` stays the switch that keeps one profile on the harness global login. This change does not start inventing a config dir from the profile name.

## Goals

*   A profile that has selected an OpenCode config dir opens `<that-dir>/opencode.db`, an absolute path, and does not open the shared database for accounts or the selected model.
*   A profile that has not selected a config dir does not set, clear, or rewrite `OPENCODE_DB`. A path already in the process environment stays. The `--standalone` rule still applies to commands that accept it.
*   On OpenCode v2, aide adds `--standalone` after the leaf whose own help lists it: root, `run`, `mini`, `api`, `models`, `stats`, `reload`, and the `auth` and `session` leaves (`list`, `login`, `logout`, `export`, `import`, `auth switch`, `session delete`). The form is `auth list --standalone`, not `auth --standalone list`. An already-present `--standalone` or `--server` is left as passed.
*   Aide does not copy, truncate, or delete accounts or sessions. A new profile database is not seeded by aide. Isolation of leftover accounts in `auth.json` does not hold until the person removes that file.
*   The release notes state the break, retract the unreleased sentence that says the OpenCode database stays shared, and name the sources that are not migrated: a non-default data directory, a channel-specific database filename, and a leftover `auth.json`.
*   `aide profile migrate` tells the person that OpenCode accounts and sessions were not copied and were not deleted, including on `--adopt` and on a preview that writes nothing. A config that does not mention OpenCode does not get that note. Launch stays quiet.

## Non-Goals

*   Deriving a config dir from the profile name when `config_dir_name` is missing.
*   A new sandbox writable root, a file-level deny inside the shared data directory, or moving logs, state, or cache.
*   Denying, deleting, or moving a leftover `auth.json`. The release note is the only handling of that one-time import.
*   Adding `OPENCODE_DB` to the list of config-dir environment variables that are scrubbed for every harness.
*   A command that copies accounts in or back.
*   Changing `--server`. A server aide did not start is not pointed at this launch's database.
*   Adding `--standalone` to commands that do not accept it on OpenCode v2: `mcp`, `plugin`, `debug`, `service`, `pair`, `acp`, `serve`, `upgrade`, `update`, and `uninstall`. `debug paths` does not get the flag. It may still receive `OPENCODE_DB`, because it prints this process's path and does not start a server.
*   Isolating MCP or plugin files. Those may stay shared. `--standalone` does not isolate them.
*   A special case that keeps the shared database when a profile is pinned at OpenCode's global config dir. That pin gets `<that-dir>/opencode.db` too.
*   Making this rule apply to harnesses whose login already lives in the config dir. The database binding stays OpenCode-specific.
*   A note printed at launch.

## Open Questions

*   None. Decisions that were open in the concepts brief are closed in the Key Decisions section of `concepts_brief.md`.

## Technical Plan (Architecture)

Two changes, both on the OpenCode launch path. Neither reads or writes the database file.

1. When `HarnessOf(agent)` is `opencode`, the profile has selected a config dir, and the launch starts a private server or is `debug paths`, the environment `Exec` receives sets `OPENCODE_DB` to an absolute path inside that directory. `service`, `serve`, `mcp`, `plugin`, `acp`, `pair`, `upgrade`, and `uninstall` do not get the variable, so they cannot start the shared server with a profile database.
2. When the launched command's own help lists `--standalone`, aide adds that flag after the leaf unless the person already passed `--standalone` or `--server`.

A profile that has not selected a config dir does not set, clear, or rewrite `OPENCODE_DB`. The `--standalone` rule still applies. Passthrough, which has no profile, does not set `OPENCODE_DB`. It does get the `--standalone` rule.

```text
profile selected?
        |
        +-- no config dir --> leave OPENCODE_DB as it arrived
        |                     (unset, or the process environment)
        |
        +-- config dir and private server, or debug paths
                              |
                              v
                    HarnessOf(agent) == opencode
                    env Exec receives, after sandbox.Apply:
                    OPENCODE_DB=<that-dir>/opencode.db
                    if never_allow_env lists OPENCODE_DB: fail
                              |
                              v
                    OpenCode opens that file
                    (aide does not create it)

        +-- config dir but service/serve/mcp/plugin/...
                    do not set OPENCODE_DB

leaf help lists --standalone?
        |
        +-- no, or --server / --standalone already present --> leave args
        |
        +-- yes --> insert after that leaf
                    auth list --standalone
                    private server, this process's database
```

### Database path

The path is the absolute directory already chosen for `OPENCODE_CONFIG_DIR`, joined with the filename `opencode.db`. That directory is `config_dirs.opencode` if set, otherwise `config_dir`, otherwise `~/.<harness>-<config_dir_name>`. It is not derived again from the aide profile name. A pin of OpenCode's global config dir, including what `aide profile migrate --adopt` writes, uses that same rule. There is no branch that skips the global config dir.

The check is `HarnessOf(agent) == "opencode"`, not the alias name. The assignment happens on the environment slice `Exec` receives, after `sandbox.Apply`, so `clean_env` cannot strip it. A value that arrived earlier, from the shell, direnv, profile `env`, a project file, or a credential binding, is overwritten on the launches that set the variable. If `never_allow_env` lists `OPENCODE_DB` on one of those launches, the launch fails before exec. The key is not written back. If `OPENCODE_CONFIG_DIR` is missing or the joined path is not absolute, the launch fails before exec with `opencode profile database path is not absolute: %s`. A relative value is not joined onto the config dir. An existing empty config dir is still a successful aide launch. A missing parent directory is OpenCode's error. Aide does not create it.

`InjectProfileEnv` does not set `OPENCODE_DB`. That function also runs for provision, Skillshare target resolution, and preview. Setting the variable there would make those paths look like launches, and a later merge can put a caller-supplied value back on top. The launch is the only writer.

Aide does not create the database file, its parent directory, or a zero-byte placeholder. OpenCode creates the schema on first open. If a file is already at the target path, it is left as it is. "Not seeded" means aide does not copy rows into it. It does not mean aide truncates a file that is already there.

`OPENCODE_DB` is not added to `ConfigEnvKeys`. That list is scrubbed for every harness once a config dir is selected, and its values are treated as directories. A file path on that list would be the wrong kind of value, and it would be stripped on Claude and Codex launches. An existing `env: OPENCODE_DB` still loads. It is ignored only on an OpenCode launch that has selected a config dir.

A launch that has not selected a config dir does not set, clear, or rewrite `OPENCODE_DB`. `--clean-env` already drops an ambient value on that path. It is not added to the essential-environment list to preserve it. An `env: OPENCODE_CONFIG_DIR` without a selected config dir does not cause `OPENCODE_DB` to be set. A Claude launch does not set it. `--agent opencode` uses the OpenCode directory for that profile, not the other agent's directory.

The filename is exactly `opencode.db`. OpenCode's channel-specific names (`opencode-<channel>.db`) are not opened and are not migrated. Aide does not add a channel suffix and does not read `XDG_DATA_HOME` to find the old file. The old file stays wherever OpenCode was already opening it. The sandbox grants stay hardcoded at `~/.local/share/opencode`, `~/.local/state/opencode`, and `~/.cache/opencode`. The module comment stops saying the database lives in the data directory. The grants do not change. Isolation is "this launch opens this file," not "the other file is unreadable."

### Private server

`--standalone` is a global OpenCode flag on the commands that declare it. It means "start a private server with this process's environment" instead of attaching to the per-user background service. Without it, `auth list` and `session` read the database of whichever launch started that service, so `OPENCODE_DB` on the child is ignored.

Aide already adds the flag for a root launch, `run`, and `mini`. On OpenCode v2 the flag is inserted after the leaf whose own help lists it:

*   root TUI (prepend), `run`, `mini`
*   `api`, `models`, `stats`, `reload`
*   `auth list`, `auth login`, `auth logout`, `auth export`, `auth import`, `auth switch`
*   `session list`, `session delete`, `session export`, `session import`

The accepted form is `auth list --standalone`. `auth --standalone list` is rejected. The flag is not added for `mcp`, `plugin`, `debug`, `service`, `pair`, `acp`, `serve`, `upgrade`, `update`, or `uninstall`. `debug paths` prints the database path from this process's environment and does not start a server, so it may receive `OPENCODE_DB` and must not receive the flag. Names that aide still treats as subcommands but that are not in this OpenCode tree (`attach`, `completion`, `db`, `github`, `import`, `pr`, `web`, `export`) are left unchanged. An explicit `--server` or an already-present `--standalone` is left as the person passed it. OpenCode rejects the two flags together. Both the flag and the database variable use `HarnessOf(agent) == "opencode"`, so an `agents:` alias whose binary is OpenCode is included.

`opencode debug paths db` is the path check. `auth list --standalone` is the account check. MCP and plugin files may still be shared. The flag does not isolate them.

### What the person is told

`RELEASE_NOTES.md` gains a breaking-change entry. It says a profile that has selected an OpenCode config dir now opens `<config-dir>/opencode.db`, that accounts and sessions are not copied and are not deleted, and that the old file remains wherever OpenCode was already opening it. It names `XDG_DATA_HOME`, a channel-specific filename, and a leftover `auth.json` as sources that are not migrated. A leftover `auth.json` can be imported by OpenCode into a new database the first time that database is created. Aide does not deny, delete, or move that file. The unreleased sentence that says the OpenCode database stays shared is removed or rewritten so the two notes do not contradict.

`aide profile migrate` prints: `OpenCode accounts and sessions were not copied and were not deleted. They remain in the database OpenCode was already opening, which is not always under the data directory.` The note is printed on preview, `--write`, and `--adopt`, including when the config already uses `profiles:` and nothing else changed. The trigger is `HarnessOf` is `opencode`, a `config_dirs` key is `opencode`, or a global-login row is OpenCode. A Claude-only config does not get the note. Launch does not print it.

Rollback is to stop setting `OPENCODE_DB` and to stop adding the extra `--standalone` flags. Accounts created only under the profile path stay on disk and disappear from view until the variable is set again. There is no copy-back command.

## Alternatives Considered

### Alternative A: Stop admitting OpenCode profiles

*   **Pros**: Matches the written rule that a harness is admitted only when its own directory isolates login. No second environment variable to keep in sync with OpenCode.
*   **Cons**: People who already use OpenCode profiles lose the config-dir split they have today, or keep a split that does not isolate accounts. The launch would have to stop, or the release note would have to say the profiles are not isolated.
*   **Reason for Rejection**: OpenCode already has a knob for the database file. Refusing the launch does not use that knob, and it does not fix the shared file for people who keep launching.

### Alternative B: Move the whole data directory

*   **Pros**: One variable, `XDG_DATA_HOME`, would move the database, the log, state, and cache together. A relative `OPENCODE_DB` would then land in the profile directory too.
*   **Cons**: The sandbox would need a new writable root. The log file would no longer open at the path the current grants allow. Logs, state, and cache are not the account store, and they were left shared on purpose.
*   **Reason for Rejection**: The account store is the database file. Moving everything else is a larger isolation project, and it needs a directory grant this change is not adding.

### Alternative C: Copy accounts into the new file

*   **Pros**: A person who already logged in under a named profile would not have to log in again. The break would be invisible.
*   **Cons**: The shared file is not always `~/.local/share/opencode/opencode.db`. `XDG_DATA_HOME` and the release channel move it. A copy without the `-wal` file is a stale or corrupt snapshot. A successful copy puts one profile's login into another profile's file, which is the bug. Aide would have to open a credential database it does not own.
*   **Reason for Rejection**: This is a bugfix, not a migration. The old file stays where it is. The person logs in again under the profile. The release notes and `aide profile migrate` say the accounts were not copied.

### Alternative D: Deny the shared credential files in the sandbox

*   **Pros**: A leftover `auth.json`, or a process that ignores `OPENCODE_DB` and opens the shared database by path, could not read the other profile's accounts. The log directory could stay writable if the deny is a single file and deny wins over allow.
*   **Cons**: The import of `auth.json` is OpenCode's one-time migration, and it runs only when that file is still present. Denying the file is new sandbox policy for a case the person can remove by deleting the file. A deny of `opencode.db` inside a directory that must stay writable is a rule no other profile redirect uses.
*   **Reason for Rejection**: The release note is enough. No code is added to deny, delete, or move `auth.json`. The shared data directory stays writable so the log can open.

### Alternative E: Treat a missing `config_dir_name` as the profile name

*   **Pros**: Every named profile would get its own directory without an extra field. The "no config dir" case would disappear.
*   **Cons**: A missing `config_dir_name` is how one profile keeps the harness global directory and the login already stored there. Using the profile name would move that profile off `~/.claude`, `~/.codex`, `~/.pi/agent`, and `~/.config/opencode` onto empty directories. That logs them out of every harness, not only OpenCode. A launch must not rewrite the config to record the move.
*   **Reason for Rejection**: Issue #21 does not need that rule. A missing `config_dir_name` still means keep the global login. Changing the default belongs in a separate change, and that change has to pin the profile that currently owns the global login.

### Alternative F: Add `--standalone` to every OpenCode command

*   **Pros**: One rule, no list of commands that accept the flag. `auth list` would not be a special case.
*   **Cons**: `debug paths`, `service`, `mcp add`, `upgrade`, and `serve` do not accept the flag, or they exist to manage the shared background service. Injecting the flag there makes OpenCode reject the command, or stops `service stop` from reaching the service it is supposed to stop.
*   **Reason for Rejection**: The flag is added only where OpenCode declares it. Commands that reject it, and `service`, are left alone. An explicit `--server` still wins.

## Non-Functional Requirements

*   **Security**: The database file and its `-wal` and `-shm` sidecars now sit in the profile config dir. Anything that syncs or copies that directory copies accounts and sessions. The release note says so. Aide does not open the file, so it does not log its contents.
*   **Rollback**: Stop setting `OPENCODE_DB` and stop adding the extra `--standalone` flags. No data migration runs in either direction. Accounts created only under the profile path remain on disk and drop out of view until the variable is set again.
*   **Performance**: One environment assignment and one flag insertion per launch. A private server is started only for commands that accept `--standalone`. No database read.

## Detailed Implementation Plan

No new file. No file is deleted. Aide does not create `opencode.db`. Tests inspect the environment and the argument list. They do not launch OpenCode and they do not write a database file.

### Target Files to Create/Modify

#### 1. `internal/launcher/launcher.go` (Modify)

*   **Type / Scope**: Profile launch environment.
*   **Proposed Changes**: After `sandbox.Apply`, when `HarnessOf(agent) == "opencode"`, `SelectsConfigDir()` is true, and the launch is a private-server command or `debug paths`, set `OPENCODE_DB` on the environment slice `Exec` receives to `filepath.Join` of the absolute `OPENCODE_CONFIG_DIR` already in that environment and the filename `opencode.db`. Overwrite any earlier value. Do not set it on `service`, `serve`, `mcp`, `plugin`, `acp`, `pair`, `upgrade`, or `uninstall`. If `never_allow_env` lists `OPENCODE_DB` on a launch that would set it, return `opencode profile database path is blocked by never_allow_env` before exec and do not write the key back. If `OPENCODE_CONFIG_DIR` is missing or the joined path is not absolute, return `opencode profile database path is not absolute: %s` before exec. Do not call this from `InjectProfileEnv`. Do not create the file or its parent. A launch that has not selected a config dir does not set, clear, or rewrite `OPENCODE_DB`. Pass the harness name into `ApplyLaunchArgs`.

#### 2. `internal/harness/harness.go` (Modify)

*   **Type / Scope**: OpenCode argument injection.
*   **Proposed Changes**: `ApplyLaunchArgs` still returns the arguments unchanged when `--standalone` or `--server` is already present. It still prepends `--standalone` for a root launch, and still inserts it immediately after `run` and `mini`. On OpenCode v2, also insert it after the leaf whose own help lists it: `api`, `models`, `stats`, `reload`, `auth list|login|logout|export|import|switch`, and `session list|delete|export|import`. The form is `auth list --standalone`. Do not add the flag for `mcp`, `plugin`, `debug`, `service`, `pair`, `acp`, `serve`, `upgrade`, `update`, or `uninstall`. Leave `attach`, `completion`, `db`, `github`, `import`, `pr`, `web`, and `export` unchanged. The caller passes `HarnessOf(agent)`, not the alias name. Do not add `OPENCODE_DB` to `ConfigEnvKeys`.

#### 3. `internal/harness/harness_test.go` (Modify)

*   **Type / Scope**: Argument-injection tests.
*   **Proposed Changes**: Extend `TestApplyLaunchArgs` so each accepting leaf gains `--standalone` after that leaf. `auth list` must become `auth list --standalone`, not `auth --standalone list`. `mcp list`, `plugin list`, `debug agents`, `debug paths`, and `service stop` stay unchanged. An existing `--standalone` or `--server` is left alone.

#### 4. `internal/launcher/launcher_test.go` (Modify)

*   **Type / Scope**: Launch environment tests.
*   **Proposed Changes**: Add tests that capture the environment `Exec` receives, after `sandbox.Apply`. A profile with `config_dir_name` launching OpenCode `run` or the TUI gets an absolute `OPENCODE_DB` under that profile's OpenCode config dir. `config_dirs.opencode` wins over the derived directory, including when that path is the global config dir `~/.config/opencode`. `debug paths` gets the variable and does not get the flag. `service stop` and `mcp list` do not get the variable. A profile with no config dir leaves an ambient `OPENCODE_DB` in place, including a relative one. A selecting private-server launch overwrites an ambient relative value and a profile `env: OPENCODE_DB`. A `never_allow_env` entry for `OPENCODE_DB` fails the launch and does not exec. A missing or relative `OPENCODE_CONFIG_DIR` returns `opencode profile database path is not absolute: %s` and does not exec. A Claude launch does not set `OPENCODE_DB`. An `agents:` alias whose binary is OpenCode, and `--agent opencode` on a Claude profile, use the OpenCode directory and get both the variable and the flag. `clean_env: true` still leaves `OPENCODE_DB` on the exec environment. The test home must not gain an `opencode.db` file from the launch setup.

#### 5. `internal/launcher/passthrough_test.go` (Modify)

*   **Type / Scope**: No-profile launch.
*   **Proposed Changes**: A passthrough `auth list` becomes `auth list --standalone` and does not set `OPENCODE_DB`. A passthrough `debug paths` gains neither the flag nor `OPENCODE_DB`. The existing `run` expectation stays `run --standalone`.

#### 6. `internal/provision/profile_harness_test.go` (Modify)

*   **Type / Scope**: Profile env injection.
*   **Proposed Changes**: `TestInjectProfileEnvForOpenCode` asserts `OPENCODE_DB` is absent. Injection still sets only `OPENCODE_CONFIG_DIR`.

#### 7. `cmd/aide/profile_migrate.go` (Modify)

*   **Type / Scope**: Migrate stdout.
*   **Proposed Changes**: Print this sentence from both the unchanged path and the summary path, on preview, `--write`, and `--adopt`: `OpenCode accounts and sessions were not copied and were not deleted. They remain in the database OpenCode was already opening, which is not always under the data directory.` Print it when `HarnessOf` is `opencode`, a `config_dirs` key is `opencode`, or a global-login row is OpenCode, including when the config already uses `profiles:` and nothing else changed. Do not name `~/.local/share/opencode/opencode.db`. Do not print it for a Claude-only config. Do not print it from launch.

#### 8. `cmd/aide/profile_migrate_test.go` (Modify)

*   **Type / Scope**: Migrate output tests.
*   **Proposed Changes**: Assert the quoted sentence for each trigger: `HarnessOf` is `opencode`, a `config_dirs` key is `opencode`, and an OpenCode global-login row. Assert it on an already-`profiles:` config, on preview, on `--write`, and on `--adopt`. A Claude-only config does not contain it. The test does not require a database file to exist.

#### 9. `RELEASE_NOTES.md` (Modify)

*   **Type / Scope**: User-facing break.
*   **Proposed Changes**: Under `### 💥 Breaking changes`, state that a profile which has selected an OpenCode config dir now opens `<config-dir>/opencode.db`. Accounts and sessions are not copied and are not deleted. The old file remains wherever OpenCode was already opening it, which is not always `~/.local/share/opencode/opencode.db` (`XDG_DATA_HOME` and a channel-specific filename move it). A leftover `auth.json` in the shared data directory can be imported into a new database the first time OpenCode creates it. Aide does not deny, delete, or move that file. The database file now sits in the config dir and will be copied by anything that syncs that directory. Rewrite the unreleased "OpenCode launches use a private server" bullet so it names the v2 leaves that get `--standalone` (`auth list --standalone`, not a flag before the subcommand), and still leaves `--server` and commands that reject the flag alone. Keep the sentence that the data, state, and cache directories stay writable. Remove or rewrite any sentence that says the database stays in the shared data directory.

#### 10. `pkg/seatbelt/modules/opencode.go` (Modify)

*   **Type / Scope**: Comment only.
*   **Proposed Changes**: The comment stops saying the database and auth live in the XDG data directory. It says the data directory stays granted for the log, state, and cache, and that a selected config dir also holds `opencode.db` when the launch sets `OPENCODE_DB`. The grants do not change. No new deny rule.

#### 11. `pkg/seatbelt/modules/profile_isolation_test.go` (Modify)

*   **Type / Scope**: Comment only.
*   **Proposed Changes**: The comment on `TestOpenCodeConfigDirOverrideKeepsDataDirs` stops saying the data directory holds the database and auth. The assertion stays: the config dir and `~/.local/share/opencode`, `~/.local/state/opencode`, and `~/.cache/opencode` remain granted.

### Verification Plan

*   **Unit tests**: `go test ./internal/harness ./internal/launcher ./internal/provision ./cmd/aide ./pkg/seatbelt/modules`
*   **What those tests prove**: two selecting profiles would be given different absolute `OPENCODE_DB` values, each under that profile's config dir, on the environment `Exec` receives. A relative ambient value is overwritten on a private-server launch and left alone when no config dir is selected. `service` and `mcp list` do not get the variable. `auth list` becomes `auth list --standalone`. `debug paths` does not get the flag. A blocked or non-absolute path fails before exec. No test creates a database file.
*   **What those tests do not prove**: a live `auth list` against two real OpenCode databases. That needs the OpenCode binary and is not added as a CI test.
*   **Static analysis**: `go test` is the check. No new linter config.

--------------------------------------------------------------------------------

*Drafted with EGM*
