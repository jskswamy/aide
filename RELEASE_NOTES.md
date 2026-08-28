## v2.2.0 (2026-08-28)

A statusline for your terminal, hooks that adopt themselves and travel
across contexts via `{agent_dir}`, more resilient plugin/MCP sync, and a
round of security patches to the toolchain and dependencies.

### 🪝 Hooks

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

#### aide sync hook plan shows correct per-matcher labels

Hook operations in the sync plan were missing the matcher segment in their
display name. Hooks that share event and command but differ by matcher (e.g.
`session_start startup` vs `session_start compact`) appeared identical in the
output, giving the impression of duplicates.

The `hookOpName` helper now includes the matcher when non-empty, producing
`session_start:compact:~/.claude/hooks/cbm-session-reminder` style labels.

### 🔌 Plugin & MCP sync

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

### 📊 aide statusline

#### aide statusline: live session state in your terminal

A new `aide statusline <agent>` subcommand renders the current aide session
state as a compact, emoji-based statusline string. Claude Code (and compatible
agents) can invoke it via the `statusLine.command` setting to display sandbox,
network, capability, trust, and context state directly in the terminal.

- `aide statusline claude` renders the statusline whether stdin is a pipe
  (Claude Code invocation path) or a TTY (human preview)
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

#### aide statusline: unmanaged state, module filtering, and agent auto-detection

Follow-up to the initial `aide statusline` release above, closing gaps found
during whole-branch review.

- `sandbox`/`network` modules now distinguish "aide didn't launch this
  session at all" from "aide launched it with the sandbox/network explicitly
  off": a new `unmanaged:` value (default `"💀"` for `sandbox`, `"🌫️"` for
  `network`) renders when the corresponding `AIDE_SANDBOX`/
  `AIDE_NETWORK_MODE` env var is absent entirely, rather than falling
  through to the `off`/`unrestricted` state.
- `aide statusline` (bare, no agent) now auto-detects the agent: `AIDE_AGENT`
  when set, otherwise the shape of piped stdin JSON identifies Claude Code,
  otherwise (TTY only) the CWD-matched aide context's configured agent,
  otherwise `claude`.
- New repeatable `--module <name>` flag renders only the requested modules
  (e.g. `aide statusline --module sandbox --module network`), for composing
  aide's output into a third-party statusline widget. An unrecognized module
  name is now a hard error listing the valid names, instead of silently
  producing empty output.
- An unsupported positional agent (e.g. `aide statusline gemini`, or
  `--agent gemini`) now errors instead of silently rendering claude's
  statusline; `claude` remains the only agent with rendering support.
- TTY mode now renders a real preview instead of printing usage help, and
  that preview genuinely simulates what a real `aide launch` would set
  (sandbox, network, capabilities, trust, auto-approve) for the CWD-matched
  context, instead of reading the real process env (which is empty outside
  an actual launch). `--context <name>` does the same for an explicitly
  named context, in both TTY and piped mode, and errors if the name doesn't
  exist.
- New `ccstatusline` capability grants read access to
  `~/.config/ccstatusline/settings.json`, for `aide statusline` invoked from
  a ccstatusline Custom Command widget. It's the one built-in capability
  that auto-enables itself: when its settings file exists on disk at
  context-resolution time, it's added to the effective capability set
  automatically, both at actual agent launch and in `aide cap
  list`/`aide cap audit` output, so what those commands report always
  matches what a real launch grants.

### 🔒 Security

#### Clipboard access is now its own opt-in capability (fixes copy-out regression)

The mach-lookup allow added to fix Claude Code's Ctrl+V image paste only
granted `com.apple.pasteboard.1`, one of three Mach services `pbcopy`
needs. Without the other two (`com.apple.lsd.mapdb` and
`com.apple.lsd.modifydb`, both Launch Services type lookups needed when
writing a pasteboard flavor), `pbcopy` exited 0 but silently failed to
write to the real host pasteboard, breaking any clipboard fallback logic
that trusted that exit code. Copying text out of the sandbox stopped
working while paste (a pure read) kept working.

Clipboard access is now the `clipboard` capability
(`internal/capability/builtin.go`), grants all three required Mach
services, and is available to every agent, not just Claude.

**Breaking change:** clipboard access is opt-in. If you relied on Claude
Code image paste working automatically, add `clipboard` to your context's
`capabilities:` list or pass `--with clipboard`.

#### Bump Go toolchain to 1.26.6, resolving seven stdlib CVEs

CI's govulncheck step was failing because the pinned toolchain (1.26.5)
shipped seven now-patched Go standard library vulnerabilities reachable
from aide's code, spanning `net/url`, `html/template`, `crypto/tls`,
`net/http`, `encoding/xml`, and `encoding/asn1`.

- `go.mod`'s `toolchain` directive is bumped to `go1.26.6`.
- The Nix flake's `nixpkgs` pin is updated so the dev shell also resolves
  a Go 1.26.6+ toolchain directly, instead of relying on
  `GOTOOLCHAIN=auto` to silently fetch it over the network.

#### Bump github.com/go-git/go-git/v5 to v5.19.2

Picks up upstream's `[SECURITY]` fixes: a path-traversal guard for git
reference names in dotgit storage, plus `golang.org/x/crypto`, `x/net`,
and `x/text` version bumps pulled in transitively.
