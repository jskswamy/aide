## Unreleased

### Fix

#### Managed state with tilde-form hook paths no longer triggers spurious ops

Hook entries in managed state written before path normalization was introduced
used tilde-form paths (`~/.claude/hooks/foo`). After upgrading, `aide sync`
would generate uninstall ops for those stale entries and no matching install
ops (because the installed hooks appeared as already present in absolute form).
Running sync would delete hooks from settings.json without re-adding them.

`ComputePlan` and `hasShortfall` now expand tilde paths in managed-state hook
commands before computing keys, so old tilde-form entries match the new
absolute-form desired and installed entries. No migration sync required.

#### DriftStatus no longer reports false drift for {agent_dir} hook commands

After `aide adopt` rewrites a hook command to use `{agent_dir}` and sync runs,
`aide status` / `aide which` previously showed perpetual "config changed since
last sync" because the drift check compared the unresolved template
(`{agent_dir}/hooks/foo`) against the managed resolved path
(`/Users/name/.claude/hooks/foo`).

`DriftStatus` now receives `agentDir` and `homeDir` from its call site and
passes them to `ResolveDesired`, producing a fully resolved desired set that
matches managed state correctly.

#### Plugin sync is now self-healing for project-scope and stale-index errors

Two error patterns previously blocked `aide sync` from cleaning up managed
state, causing the same errors to reappear on every run:

- A plugin managed at user scope but installed at project scope caused
  `claude plugin uninstall` to fail with "enabled at project scope".
  `UninstallPlugin` now tolerates this message; aide removes the entry
  from managed state and stops tracking the plugin at user scope.
- A marketplace plugin installed from a stale local index caused
  `claude plugin install` to fail with "local copy may be out of date".
  `InstallPlugin` now detects this, runs `claude plugin marketplace update
  <marketplace>` once (ignoring update errors), and retries the install.
  If the plugin name has no `@marketplace` suffix, the error is returned
  as-is without a retry.

### Feature

#### Test coverage: aide adopt hook agentDir prefix rewrite

Added `TestAdoptHookRewritesAgentDirPrefix` to verify that when a hook
command starts with the agent directory path, `aide adopt` rewrites it to
`{agent_dir}/...` in `config.yaml`. Extends `fakeProv` (the shared test
double) with `AgentDirProvider` and `HookInstaller` so the agentDir prefix
path can be exercised without a new registered agent name.

#### aide sync and adopt now wire real agentDir values for hook expansion

`aide sync` and `aide adopt` now pass the real `agentDir` and `homeDir` values
to `provision.ResolveDesired`, enabling hook command path expansion during
reconciliation. When adopting hooks, commands with paths beginning with the agent
directory are automatically rewritten to use `{agent_dir}` tokens in the config,
making the configuration portable across installations.

- `sync.go` calls `ResolveAgentDir` to get the driver's config directory and passes it to `ResolveDesired`
- `adopt.go` does the same, plus auto-replaces adopted hook commands prefixed with `agentDir` with `{agent_dir}` tokens
- Adopted hooks with `{agent_dir}` paths remain portable when later synced to other agent installations

#### Hook commands now support {agent_dir} and ~/ path expansion

`ResolveDesired` accepts two new parameters (`agentDir`, `homeDir`) that feed
into hook command substitution. When non-empty, `agentDir` replaces
`{agent_dir}` tokens in hook commands, and `homeDir` expands `~/` and
`$HOME/` prefixes to absolute paths. All existing callers pass `"", ""`
(no-op), preserving current behaviour until a later task wires in real values.

- Adds exported `provision.ExpandPath(s, homeDir string) string`
- Extends `provision.ResolveDesired` signature to accept `agentDir, homeDir string`
- Updates all six call sites in `cmd/aide/` and `internal/provision/drift.go`

#### AgentDirProvider interface enables drivers to advertise per-context config directory

Drivers can now implement `AgentDirProvider` to declare their agent's per-context
config directory (e.g. Claude's `CLAUDE_CONFIG_DIR`). The `ResolveAgentDir` helper
returns the directory if implemented, or "" otherwise, enabling hook commands to
reference `{agent_dir}` as an absolute path to the agent's profile.

- Adds `provision.AgentDirProvider` interface with `AgentDir(ctx Context) string` method
- Adds `provision.ResolveAgentDir(prov Provisioner, ctx Context) string` helper
- Registers `{agent_dir}` in `HookTemplateVars` for CLI help and interactive prompts

#### Claude driver implements AgentDirProvider; ReadHooks expands tilde paths

The Claude driver now satisfies `AgentDirProvider`: `AgentDir` returns
`CLAUDE_CONFIG_DIR` when set (profile contexts) or `~/.claude` otherwise.
`ReadHooks` now calls `ExpandPath` on every command it reads from
`settings.json`, converting `~/` and `$HOME/` prefixes to absolute paths
before returning them to the engine.

- Adds `(*Driver).AgentDir(ctx provision.Context) string` to `claude.go`
- Applies `provision.ExpandPath(cmd, ctx.HomeDir)` in `ReadHooks` inside `hooks.go`

### Fix

#### aide sync no longer reinstalls managed MCP servers every run

When a project-level `.mcp.json` declares an MCP server by the same name as
one aide manages at user scope, `claude mcp get <name>` returns the
project-scope entry. The User-scope filter then drops it, leaving the server
absent from the installed map, so every sync generated a fresh install op
even though aide had already installed the server.

The planner now skips the install if the server is already in managed state,
treating managed as the authoritative source when the get query is shadowed by
a higher-precedence scope.

- Fixes `+ install mcp <name>` reappearing after every `aide sync`
- A new `TestComputePlanMCPSkipsInstallWhenManaged` test covers the scenario

#### aide adopt no longer corrupts config with name-only marketplace keys

Adopting a marketplace whose key is a bare name (e.g. `rfctl-local` rather
than `owner/repo` or a URL) wrote an invalid entry into `config.yaml`. On
the next run, `ValidatePlugins` rejected the config with a shape error.

The fix skips name-only keys during marketplace adoption and prints a note
telling the user to add the entry manually with a proper repo path.

#### aide sync hook plan shows correct per-matcher labels

Hook operations in the sync plan were missing the matcher segment in their
display name. Hooks that share event and command but differ by matcher (e.g.
`session_start startup` vs `session_start compact`) appeared identical in the
output, giving the impression of duplicates.

The `hookOpName` helper now includes the matcher when non-empty, producing
`session_start:compact:~/.claude/hooks/cbm-session-reminder` style labels.

#### Claude Code image paste (Ctrl+V) now works inside aide sandbox

The macOS sandbox blocked `com.apple.pasteboard.1`, the per-user mach
service that `osascript` uses to read clipboard data. Claude Code uses
osascript to detect and read PNG image data on Ctrl+V, so pasting
screenshots silently failed with "clipboard empty" when running via `aide`.

The fix adds a mach-lookup allow for `com.apple.pasteboard.1` to the
Claude module rules. The entry is scoped to the Claude agent only; no
other sandboxed processes gain clipboard access.

### Feature

#### aide adopt now promotes unmanaged hooks into config

`aide adopt` previously handled plugins, MCP servers, and marketplaces but
left hooks untouched. Hooks installed directly in Claude's settings (e.g.
via `claude hooks add`) were visible in `aide sync` as unmanaged but had no
path to adoption.

`aide adopt` now discovers, prompts for, and records hooks. Adopted hooks
are written to the top-level `hooks:` map in `config.yaml` (so they apply
across contexts) and marked as managed in state so future syncs do not
surface them as unmanaged.

- Works for any agent that implements `provision.HookInstaller`
- Hooks are deduplicated by `event+matcher+command` key before writing
- `--yes` flag adopts all unmanaged hooks without prompting

#### aide statusline: live session state in your terminal

A new `aide statusline <agent>` subcommand renders the current aide session
state as a compact, emoji-based statusline string. Claude Code (and compatible
agents) can invoke it via the `statusLine.command` setting to display sandbox,
network, capability, trust, and context state directly in the terminal.

- `aide statusline claude` renders the statusline when stdin is a pipe (Claude
  Code invocation path); prints help when stdin is a TTY
- `aide statusline claude --install` patches the target context's
  `settings.json` to set `statusLine.command`; generates a wrapper script if
  another command is already configured
- `aide statusline claude --install --context <name>` installs into a specific
  aide context (profile-based `CLAUDE_CONFIG_DIR` is resolved automatically)
- `aide statusline claude --remove` clears the `statusLine` key from settings;
  also accepts `--context <name>` to target a non-default context
- Render order, per-module icons, and disabled state are all configurable via
  `statusline:` in `aide.yaml` or `.aide.yaml`
- `auto_approve` module is always prepended (before the order list) when
  `AIDE_AUTO_APPROVE=1` is set
- Modules with no content (`caps`, `context`) are silently hidden
- `trust` module only appears when `AIDE_TRUST=untrusted`
