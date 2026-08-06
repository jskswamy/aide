## Unreleased

### Fix

#### aide sync no longer perpetually reinstalls hooks with explicit matchers

When `config.yaml` declares hooks with matchers such as `Grep|Glob`,
`compact`, `startup`, or `'*'`, `aide sync` generated a fresh
`+ install hook` operation on every run even after the hook was already
installed in `settings.json`.

Root cause: `WriteHooks` used `claudeMatcherMap[h.Matcher]` to translate
aide-internal matcher names to Claude Code's native names. The map only
contains `shell -> Bash`; every other matcher produced the zero-value `""`
and was written to `settings.json` without a matcher field. On the next
read, `ReadHooks` returned `matcher: ""`, causing a HookKey mismatch
against the desired set (which kept the original matcher) on every sync.
A related gap in `ComputePlan` meant the `'*'` wildcard read back from
`settings.json` was never normalized to the same "match all" sentinel
used for desired and managed hooks, so it also looked perpetually
out of sync.

Three fixes address this:

- `WriteHooks` now uses `toNativeMatcher`, which passes through any
  matcher not explicitly in `claudeMatcherMap` unchanged. Only aide-
  internal shorthands (currently only `shell`) are translated.
- `normalizeHookMatcher` converts the `'*'` wildcard to `''` consistently
  across desired, managed, *and* installed hook comparisons, so the
  "match all" sentinel always matches an entry with no matcher field.
- `WriteHooks` bucket grouping now uses a `bucketRefs` map instead of
  `buckets[len-1]`. When two hooks share the same event+matcher but
  differ in command path (e.g. both contexts declare a `compact` hook),
  the old code appended the second command to whichever bucket was last
  in the array rather than the correct one, silently misrouting commands
  into the wrong matcher bucket in settings.json.

The removal path in `WriteHooks` also accepts the empty-matcher form of a
hook when removing managed entries, so hooks written by older aide versions
(without a matcher field) are cleaned up during the next sync.

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

#### aide sync no longer reinstalls managed MCP servers every run

When a project-level `.mcp.json` declares an MCP server by the same name as
one aide manages at user scope, `claude mcp get <name>` returns the
project-scope entry. The User-scope filter then drops it, leaving the server
absent from the installed map, so every sync generated a fresh install op
even though aide had already installed the server.

The planner now skips the install if the server is already in managed state,
treating managed as the authoritative source when the get query is shadowed by
a higher-precedence scope.

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

#### Hook commands support {agent_dir} and ~/ path expansion

Hook commands in `config.yaml` can now reference `{agent_dir}` (the
agent's per-context config directory, e.g. Claude's `CLAUDE_CONFIG_DIR`)
and `~/` / `$HOME/` paths, resolved to absolute paths wherever hooks are
compared: sync, adopt, and drift checks.

- Drivers implement the new `AgentDirProvider` interface to advertise
  their config directory; `ResolveAgentDir` returns it if implemented,
  or `""` otherwise. The Claude driver returns `CLAUDE_CONFIG_DIR` when
  set (profile contexts) or `~/.claude` otherwise.
- `provision.ExpandPath` expands `{agent_dir}` and `~/`/`$HOME/` prefixes
  in hook commands. `ResolveDesired`, `ReadHooks`, and `DriftStatus` all
  apply it consistently, so desired, installed, and managed hook paths
  compare correctly and `aide status` / `aide which` no longer report
  false drift for `{agent_dir}` hooks.
- `aide adopt` rewrites hook commands prefixed with the agent directory
  to `{agent_dir}` tokens automatically, keeping adopted config portable
  across installations.
- `aide hook add`'s help text and interactive prompt list `{agent_dir}`
  alongside `{agent}` as an available template variable.

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
