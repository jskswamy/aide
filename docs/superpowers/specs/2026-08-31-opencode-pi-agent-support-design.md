# Agent Support: OpenCode and Pi

## Problem

`aide` provisions plugins, MCP servers, hooks, and sandbox profiles for
several coding-agent CLIs (`claude`, `gemini`, `copilot`, `codex`,
`cursor`, `hermes`) through the `provision.Provisioner` interface. Two
harnesses the user actively uses day-to-day, OpenCode
(`anomalyco/opencode`, opencode.ai) and Pi (`earendil-works/pi`,
pi.dev), aren't supported yet — `aide` can't sandbox them, sync their
MCP servers, manage their plugins, or install hooks into them.

## Goals

- `aide` sandboxes both agents (seatbelt profile, same as every other
  supported agent).
- `aide` manages OpenCode's MCP servers declaratively, the same way it
  does for Gemini/Copilot.
- `aide` manages plugins (OpenCode) and extensions (Pi) declaratively.
- `aide` installs/removes hooks into both agents' native lifecycle-event
  mechanisms.

## Non-goals

- **Pi MCP.** Pi has no first-party MCP client; the only option is a
  third-party extension (`pi-mcp-extension`) with a config format
  `earendil-works` doesn't own and could change without notice. Skipped
  for this pass — revisit if/when Pi gets a first-party MCP story.
- **Marketplaces.** Neither agent has the concept. Both use bare
  npm/git/local-path refs per plugin entry (`ShapeURLDirect`), not
  named marketplaces (`ShapeMarketplace`). `AddMarketplace` /
  `RemoveMarketplace` / `InstalledMarketplaces` stay on `DriverBase`'s
  stub implementations for both drivers.
- **Subagents.** Not a feature `aide` provisions for any existing
  agent (it's not part of the `Provisioner` interface at all) — out of
  scope here too, regardless of what either CLI supports natively.
- **Auto-trusting Pi projects.** Pi gates project-local config behind a
  one-time interactive "project trust" decision. `aide` does not write
  to `~/.pi/agent/trust.json` on the user's behalf — see Known
  Limitations.
- **CLI-driven OpenCode plugins/MCP.** Both are file-edit only. See
  Research Findings for why the CLI paths were rejected.

## Research Findings

Verified directly against the real binaries (`nix-shell -p opencode`,
`nix-shell -p pi-coding-agent`; the latter's `meta.homepage` confirms
`https://pi.dev/`, matching the harness in question), not just docs —
several things the docs implied turned out to work differently in
practice:

**OpenCode MCP is not CLI-scriptable.** `opencode mcp add` only accepts
`--url`/`--env`/`--header` — there's no flag for a local/stdio server's
command, so anything but a remote HTTP/SSE server can't be added this
way at all. `opencode mcp list` doesn't just list: it live-connects to
every configured server and prints ANSI/box-drawing health-check
output (confirmed — it attempted to reach a dummy `https://example.com/mcp`
URL and reported the connection failure), with no `--json` flag. Both
are a bad fit for `aide`'s read-diff-write reconcile loop. Confirmed
file shape instead, written by `opencode mcp add` itself to
`~/.config/opencode/opencode.jsonc`:

```jsonc
{
  "mcp": {
    "testsrv": { "type": "remote", "url": "https://example.com/mcp" }
    // stdio form (from docs, not independently re-verified): "type": "local", "command": [...], "environment": {...}
  }
}
```

**OpenCode's plugin CLI only covers npm sources.** `opencode plugin
<module>` help text: positional is literally "npm module name" — no
git-URL or local-path support, and no list/remove subcommand exists at
all. Since `aide`'s plugin entries can be npm/git/local refs
uniformly, and list/remove need a file-edit path regardless, the CLI
adds a second code path for a narrow subset of sources. Decision: skip
it, file-edit only, one code path.

**Pi's plugin CLI is a full, clean triad.** Confirmed by installing a
real local extension against an isolated `$HOME`:

```
$ pi install ./local/path
Installing ./local/path...
Installed ./local/path
$ cat ~/.pi/agent/settings.json
{ "packages": ["../../../local/path"] }
$ pi list
User packages:
  ../../../local/path
    /resolved/absolute/path
$ pi remove npm:@nonexistent/pkg
Removing npm:@nonexistent/pkg...
No matching package found for npm:@nonexistent/pkg   # exit 1
```

Two implementation notes from this: (1) `pi list`'s output is **two
lines per entry** (declared source, then resolved absolute path) —
`provision.ParsePluginList` assumes one entry per line and can't be
reused as-is; the Pi driver needs its own parser. (2) the "not
installed" error message is `"No matching package found for X"`, which
does not match any substring in `provision.DefaultTolerateStderr`
(`"not installed"`, `"not found"`, `"not configured"`) — `UninstallPlugin`
needs `"No matching package found"` appended to the tolerated set for
rollback safety, the same way Claude's driver appends `"project
scope"`.

**Confirmed sandbox-relevant home directories.** OpenCode touches
`~/.config/opencode` (config), `~/.local/share/opencode` (data/repos/
logs), `~/.local/state/opencode` (locks), `~/.cache/opencode` (Bun
package cache) — all four observed populated after a single `opencode
mcp add` invocation. Pi touches `~/.pi/agent` (confirmed via
`PI_CODING_AGENT_DIR`'s documented default and an observed write
attempt to `~/.pi/agent/settings.json.lock` on startup) — home-relative
default is `.pi` (the parent of `.pi/agent`, in case other `.pi/*`
subdirectories exist that the env var doesn't cover).

**OpenCode's config file allows comments (JSONC), which
`encoding/json` can't parse.** Every existing MCP/plugin handler in
this codebase uses plain `encoding/json`. Since OpenCode's config file
is `opencode.jsonc` and users may hand-add comments, a plain
`encoding/json.Unmarshal` would error on a real user's file. Decision:
add `github.com/tailscale/hujson` (small, single-purpose, MIT) and run
`hujson.Standardize` on the raw bytes before unmarshaling in `Read`.
`Write` is unaffected — it already rewrites the full file structurally
via `json.MarshalIndent`, same as every other file-edit handler, so
original comments/formatting aren't preserved across a write either
way (consistent with existing behavior for every other agent).

## Design

### `internal/provision/agents/opencode/`

```go
type Driver struct {
    provision.DriverBase
}

func New() *Driver {
    return &Driver{DriverBase: provision.DriverBase{Caps: provision.Capabilities{
        AgentName:     "opencode",
        SupportsMCP:     true,
        SupportsPlugins: true,
        SupportsHooks:   true,
        SourceShapes:    []provision.SourceShape{provision.ShapeURLDirect},
    }}}
}
```

- **`MCPConfigPath`** returns `~/.config/opencode/opencode.jsonc`
  (project scope: `<project>/opencode.jsonc`, matching how project vs
  user config is resolved elsewhere in `opencode.json`'s merge order).
- **`MCPHandler`** returns a new `internal/provision/mcp/opencodejson.go`
  handler (same package as `claudejson.go`/`jsonflat.go`, reusing the
  existing unexported `reconcile` helper). Field mapping differs from
  `jsonFlat`: OpenCode's `"command"` is a JSON array
  (`[cmd, arg1, arg2, ...]`), not a separate `command`/`args` pair —
  the handler joins/splits `provision.MCPServer.Command` +
  `.Args` across that array on read/write. `Env` maps to `"environment"`,
  not `"env"`. The `"type"` discriminator (`"local"` vs `"remote"`) is
  inferred on write from whether `Command` or `URL` is set, and ignored
  on read (whichever of `Command`/`URL` is populated already implies
  it) — no new field needed on `provision.MCPServer`.
- **Plugins**: `InstalledPlugins`/`InstallPlugin`/`UninstallPlugin` all
  read-modify-write the `"plugin"` array in `opencode.jsonc` directly
  (parallel structure to the MCP handler, but simpler — a bare string
  array, no per-entry object). No CLI is shelled out to.
- **Hooks**: `SupportsHooks: true`, `ReadHooks`/`WriteHooks` implement
  `provision.HookInstaller` by generating/removing files in
  `.opencode/plugin/` (project) or `~/.config/opencode/plugin/`
  (global) — **not** a JSON edit, since OpenCode has no declarative
  hook config. A new `OpenCodeHookArtifact = HookArtifact{Prefix:
  "aide-", Ext: ".js"}` names the generated files (parallel to
  `GeminiHookArtifact`/`HermesHookArtifact`). Each generated file is a
  minimal plugin module exporting the OpenCode plugin shape, mapping
  aide's normalized events to OpenCode's native ones:

  | aide event | OpenCode hook |
  |---|---|
  | `pre_tool` | `tool.execute.before` |
  | `post_tool` | `tool.execute.after` |
  | `session_start` | `event` (filtered to session-start payloads) |
  | `session_end` | `event` (filtered to session-end payloads) |

  matcher (tool name) is applied inside the generated JS as an `if`
  check on the hook's `tool` argument, since OpenCode's hook API isn't
  matcher-scoped the way Claude's JSON config is. The generated file
  shells out to `h.Command` via Bun's `$` (already available in the
  plugin context per OpenCode's plugin API) exactly the way Claude's
  hook command already is a shell command string.
- **`AgentDir`**: no confirmed env var redirects OpenCode's whole home
  dir (only `OPENCODE_CONFIG`, which points at a single file, not a
  directory) — `AgentDirProvider` is not implemented; profile support
  (`ProfileEnvKey`) is left unset, same as agents without a documented
  home-redirect var.

### `internal/provision/agents/pi/`

```go
type Driver struct {
    provision.DriverBase
    runner provision.Runner
}

func New(r provision.Runner) *Driver {
    return &Driver{
        DriverBase: provision.DriverBase{Caps: provision.Capabilities{
            AgentName:       "pi",
            SupportsPlugins: true,
            SupportsHooks:   true,
            SourceShapes:    []provision.SourceShape{provision.ShapeURLDirect},
            ProfileEnvKey:   "PI_CODING_AGENT_DIR",
        }},
        runner: r,
    }
}
```

- **No MCP** — `SupportsMCP` stays `false`; `MCPConfigPath`/`MCPHandler`
  fall through to `DriverBase`'s stubs.
- **Plugins**: CLI-driven, mirroring the `claude` driver's shape:
  - `InstallPlugin` → `pi install <source>` (`-l` appended for
    project-scope contexts).
  - `UninstallPlugin` → `pi remove <source>` (`-l` for project scope),
    via `provision.RunCLI` with `DefaultTolerateStderr` plus
    `"No matching package found"` appended (see Research Findings).
  - `InstalledPlugins` → `pi list`, parsed with a small
    driver-local parser (not `provision.ParsePluginList`) that groups
    the two-line-per-entry output (`"  <source>"` then
    `"    <resolved-path>"`), taking the source line as `Plugin.Key`/
    `Plugin.Name`.
  - Binary-missing (`exec.LookPath` failure) returns `(nil, nil)` from
    `InstalledPlugins`, matching the `claude`/`gemini` convention of
    treating "not installed" as "nothing installed" rather than an
    error.
- **Hooks**: `SupportsHooks: true`, `ReadHooks`/`WriteHooks` generate/
  remove files in `.pi/extensions/` (project) or
  `~/.pi/agent/extensions/` (global). New `PiHookArtifact =
  HookArtifact{Prefix: "aide-", Ext: ".ts"}`. Generated file registers
  a `pi.on("tool_call", ...)` (pre-tool) or equivalent post-tool/
  session event handler shelling out to `h.Command`, with the matcher
  applied as an `if` check on the tool-call payload, same reasoning as
  OpenCode's generated hooks.
- **`AgentDir`**: `PI_CODING_AGENT_DIR` is a confirmed, documented env
  var for the whole config directory (default `~/.pi/agent`) — unlike
  OpenCode, Pi *does* get `ProfileEnvKey` and an `AgentDirProvider`
  implementation, following the exact pattern of Claude's
  `CLAUDE_CONFIG_DIR`.

### Sandbox modules

```go
// pkg/seatbelt/modules/opencode.go
func OpenCodeAgent() seatbelt.Module {
    return NewSimpleAgent(AgentSpec{
        DisplayName:     "OpenCode Agent",
        SectionName:     "OpenCode",
        HomeRelDefaults: []string{
            ".config/opencode", ".local/share/opencode",
            ".local/state/opencode", ".cache/opencode",
        },
    })
}

// pkg/seatbelt/modules/pi.go
func PiAgent() seatbelt.Module {
    return NewSimpleAgent(AgentSpec{
        DisplayName:     "Pi Agent",
        SectionName:     "Pi",
        EnvKey:          "PI_CODING_AGENT_DIR",
        HomeRelDefaults: []string{".pi"},
    })
}
```

`EnvKey` is left empty for OpenCode (no confirmed home-redirect var,
same as several existing `AgentSpec`-based modules); Pi's spec uses
`PI_CODING_AGENT_DIR` as the override key, matching `AgentDirProvider`
above.

### Wiring

- `internal/launcher/agentcfg.go`: add `"opencode": modules.OpenCodeAgent`
  and `"pi": modules.PiAgent` to `agentModuleResolvers`.
- `cmd/aide/provision_drivers.go`: add blank imports for both new
  agent packages.
- `internal/display/display.go`: add icon entries for `"opencode"` and
  `"pi"`.

## Known Limitations

**Pi project trust.** Project-local `.pi/settings.json` (and therefore
project-scope hooks/plugins `aide` writes there) only loads after a
one-time interactive trust decision, recorded in
`~/.pi/agent/trust.json`. `aide` does not write to that file or set
`defaultProjectTrust` on the user's behalf — this stays a manual,
one-time step the user performs (`pi --approve` once, or configuring
`defaultProjectTrust` themselves), documented in the driver's package
comment rather than automated.

Both items flagged in an earlier draft of this spec as
docs-only/unconfirmed have since been verified directly: dropping
`probe.js` into `.opencode/plugin/` and running `opencode debug
config` from that project showed the plugin auto-loading (its
`console.error` output appeared, and it showed up in the resolved
`"plugin"` array with `plugin_origins[].scope: "local"`); and
hand-writing a `"type": "local", "command": [...], "environment":
{...}` MCP entry and re-running `opencode debug config` round-tripped
it byte-for-byte with no errors, confirming the stdio shape the
handler needs to produce.

## Testing

- `internal/provision/mcp/opencodejson_test.go`: round-trip
  read/write for `"mcp"` key, command-array ↔ Command/Args mapping,
  `environment` ↔ `Env` mapping, `_aide_managed` preserved, user-added
  entries untouched.
- `internal/provision/agents/opencode/opencode_test.go`: plugin
  array read/add/remove (dedup on install, no-op uninstall of absent
  entry), table tests parallel to `gemini`'s plugin tests.
- `internal/provision/agents/opencode/hooks_test.go`: generated file
  content for each aide-event → OpenCode-hook mapping, artifact
  add/remove/rename-on-desired-change, parallel to
  `hermes_test.go`/`claude/hooks_test.go`.
- `internal/provision/agents/pi/pi_test.go`: `InstallPlugin`/
  `UninstallPlugin` CLI invocation shape (including `-l` for project
  scope) via a fake `Runner`, `InstalledPlugins`' two-line-output
  parser against real captured `pi list` output (including the
  empty-state `"No packages installed."` case), tolerated-stderr
  behavior for `"No matching package found"`.
- `internal/provision/agents/pi/hooks_test.go`: generated `.ts`
  extension content and artifact lifecycle, parallel to the OpenCode
  hooks tests.
- `pkg/seatbelt/modules/opencode_test.go`, `pi_test.go`: `Rules()`
  output against `HomeRelDefaults`, parallel to existing
  `gemini_test.go`/`aider_test.go`.
