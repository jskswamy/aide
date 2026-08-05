## Unreleased

### Feature

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
