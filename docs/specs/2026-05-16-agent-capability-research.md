# Agent Capability Research — Provisioning Feature

Date: 2026-05-16
Status: research artifact, feeds the capability matrix in
`2026-05-15-declarative-agent-provisioning-design.md`.

Each agent was investigated by a dedicated subagent against official
docs. Sources cited inline. "Couldn't confirm from docs" means exactly
that — not an assumption.

## Summary Matrix

| Agent       | Plugins | MCP | Plugin install path        | MCP config path                                 | MCP format | Cleanly scriptable? |
| ----------- | ------- | --- | -------------------------- | ----------------------------------------------- | ---------- | ------------------- |
| Gemini CLI  | ✓       | ✓   | `gemini extensions ...`    | `~/.gemini/settings.json` (`mcpServers`)        | JSON       | YES (cleanest)      |
| Copilot CLI | ✓       | ✓   | `copilot plugin ...`       | `~/.copilot/mcp-config.json` (`mcpServers`)     | JSON       | YES                 |
| Claude Code | ✓       | ✓   | `claude plugin install`    | `~/.claude.json` (user) + `.mcp.json` (project) | JSON*      | MCP yes, plugins TUI-leaning |
| Codex       | ✓       | ✓   | TUI / `npx codex-marketplace` | `~/.codex/config.toml`                       | **TOML**   | MCP yes, plugins not |
| Goose       | ✓†      | ✓†  | file edit                  | `~/.config/goose/config.yaml`                   | **YAML**   | File-edit only      |
| Amp         | ⚠️      | ✓   | file-drop `.ts`            | `~/.config/amp/settings.json` (`amp.mcpServers`)| JSON‡      | MCP yes, plugins are code |
| Aider       | ✗       | ✗   | N/A                        | N/A                                              | N/A        | Skip from feature   |

\* Claude user-scope MCP is nested under `projects.<path>.mcpServers`, not a flat `mcpServers` top-level key — needs path-aware reader.

† Goose unifies plugins and MCP under a single `extensions:` map.

‡ Amp uses `amp.mcpServers` (a dotted key inside flat JSON), not `mcpServers` at the root.

## Per-Agent Fact Sheets

### Claude Code

**Binary:** `claude`

**Plugins:** YES
- Install:    `claude plugin install <name>@<marketplace>`
- Uninstall:  `claude plugin uninstall <name>@<marketplace>`
- List:       `claude plugin` (interactive UI — no `--json` flag documented)
- Source types: marketplace name, Git URL (https/ssh `.git`), local directory or `marketplace.json`, remote HTTP URL
- Unattended: install path documented only via interactive UI; no `--yes` flag confirmed. SDK has `--plugin-dir`/`--plugin-url` for per-session use, not persistent install.

**MCP servers:** YES
- Config files:
  - User scope: `~/.claude.json` → entries under `projects.<path>.mcpServers`
  - Project scope: `.mcp.json` at repo root → top-level `mcpServers`
- Per-server keys: `type` (`http`/`sse`/`stdio`), `command`, `args`, `env`, `url`, `headers`, `oauth`, `headersHelper`, `alwaysLoad`
- Full CLI: `claude mcp add|remove|list|get|add-json`
- Reload: HTTP/SSE auto-reconnect; stdio servers per-session; `/reload-plugins` for plugin-bundled MCP

**Sources:** code.claude.com/docs (mcp.md, cli-reference.md, discover-plugins.md, settings.md, claude-directory.md)

### Gemini CLI

**Binary:** `gemini`

**Plugins (called "extensions"):** YES — clean CLI
- Install:    `gemini extensions install <github-url>` or `gemini extensions install --path <local-dir>`
- Uninstall:  `gemini extensions uninstall <name>`
- List:       `gemini extensions list`
- Enable/disable: `gemini extensions enable|disable <name>`
- Source types: GitHub URL, local directory (must contain `gemini-extension.json`)
- Installed to: `~/.gemini/extensions/<name>/`
- Restart required for changes to take effect

**MCP servers:** YES
- Config files:
  - User: `~/.gemini/settings.json`
  - Project: `<project>/.gemini/settings.json`
  - Related state: `~/.gemini/mcp-oauth-tokens.json`, `~/.gemini/mcp-server-enablement.json`
- Top-level key: `mcpServers`
- Per-server keys: `command`, `args`, `env`, `cwd`, `url` (SSE), `httpUrl` (HTTP), `headers`, `timeout`, `trust`, `includeTools`, `excludeTools`, `authProviderType`
- CLI: `gemini mcp add|list|remove|enable|disable|auth`
- Restart required to pick up changes

**Scripted invocation:** `-p`/`--prompt` (one-shot), `--non-interactive`, `--yolo`, `--output-format json`

**Sources:** github.com/google-gemini/gemini-cli docs, geminicli.com/docs

### Copilot CLI

**Binary:** `copilot` (npm `@github/copilot` — NOT `gh copilot`)

**Plugins:** YES — clean CLI
- Install:    `copilot plugin install <name>@<marketplace>`
- Uninstall:  `copilot plugin uninstall <name>`
- List:       `copilot plugin list`
- Update:     `copilot plugin update <name>`
- Marketplace: `copilot plugin marketplace add|remove|list|browse`
- Source types: GitHub `OWNER/REPO`, local path, non-GitHub Git URL
- Default marketplaces: `copilot-plugins`, `awesome-copilot`
- Plugin = directory with `plugin.json` manifest (can bundle agents, skills, hooks, MCP)

**MCP servers:** YES
- Config file: `~/.copilot/mcp-config.json` (workspace-local discovered walking from cwd up to git root)
- Top-level: `mcpServers` (Claude-style flat format)
- Per-server: `type` (`local`/`stdio`/`http`/`sse`), `command`, `args`, `env`, `url`, `headers`, `tools` (`*` or csv allowlist)
- CLI commands: interactive REPL only (`/mcp add|show|edit|delete|enable|disable`). **File-edit is fully supported and the canonical scriptable path.**
- Hot reload — picked up on save without restart

**Scripted invocation:** `copilot -p "<prompt>"` (one-shot), `--allow-all-tools`. `--headless --stdio` was removed early 2026 — do not use.

**Sources:** docs.github.com/en/copilot, github.blog/changelog/2026-01-14 (Enhanced Agents)

### Codex (OpenAI)

**Binary:** `codex`

**Plugins:** YES — but install path is TUI-leaning
- Primary install: `/plugins` slash command in TUI (interactive); `/plugin marketplace add <source>` then `/plugin install <name>`; `/reload-plugins` to activate.
- Marketplace subcommand: `codex plugin marketplace`
- External non-interactive helper: `npx codex-marketplace add <owner>/<repo>/plugins/<name>` — writes metadata into `~/.codex/plugins/cache` and toggles `[plugins."name@source"] enabled = true` in `config.toml`.
- Disable without uninstall: set `enabled = false` in `config.toml` and restart.
- Source types: Git marketplace, local directory, OpenAI-curated registry.
- **Fully non-interactive `codex plugin install` subcommand: NOT confirmed.**

**MCP servers:** YES
- Config file: `$CODEX_HOME/config.toml` (default `~/.codex/config.toml`); project scope at `.codex/config.toml` in trusted projects.
- **Format: TOML, not JSON.**
- Per-server table: `[mcp_servers.<name>]`. Stdio keys: `command`, `args`, `env`, `env_vars`, `cwd`, `experimental_environment`. HTTP keys: `url`, `bearer_token_env_var`, `http_headers`, `env_http_headers`. Common: `startup_timeout_sec`, `tool_timeout_sec`, `enabled`, `required`, `enabled_tools`, `disabled_tools`. Top-level: `mcp_oauth_callback_port`, `mcp_oauth_callback_url`.
- CLI: `codex mcp add [--env K=V] -- <cmd>` (stdio), `codex mcp add --url <url>` (HTTP), `codex mcp list [--json]`, `codex mcp remove <name>`, `codex mcp --help`. `codex mcp-server` runs Codex itself as MCP.
- Reload: restart required. Known race writing `config.toml` on concurrent removes (issue #16024).

**Scripted invocation:** `codex exec` / `codex e` for non-interactive runs. Flags: `--json` (NDJSON events), `--output-last-message <file>`, `--output-schema`, `--ephemeral`, `--ignore-user-config`, `--ignore-rules`.

**Sources:** developers.openai.com/codex/{mcp,cli/reference,config-reference,plugins,plugins/build}, github.com/openai/codex/blob/main/docs/config.md, deepwiki.com/openai/codex/6.3-mcp-cli-commands

### Goose (Block)

**Binary:** `goose`

**Plugins/Extensions:** YES — file-edit only
- Goose unifies plugins and MCP under "extensions". Any MCP server is added as an extension.
- Install:    `goose configure` → "Add Extension" (interactive); or edit `~/.config/goose/config.yaml`; `goose://extension` deep-link
- Uninstall:  `goose configure` → "Remove Extension" (interactive); or remove YAML entry
- List:       `goose configure` → "Toggle Extensions" (interactive); no `goose extensions list --json` confirmed
- Source types: `builtin`, `stdio` (MCP child), `streamable_http`, `sse`, `platform`, `frontend`, `inline_python`
- Unattended: no scripted CLI confirmed. **Direct YAML edit is the reliable path for aide.**

**MCP servers:** SAME as extensions. `stdio`/`sse`/`streamable_http` extension types ARE the MCP transports.
- Config file: `~/.config/goose/config.yaml` (macOS/Linux); `%APPDATA%\Block\goose\config\config.yaml` (Windows)
- Top-level key: `extensions:` (map keyed by extension name)
- Per-entry fields: `type`, `name`, `enabled`, `bundled`, `display_name`, `description`, `timeout`, `available_tools`, plus type-specific (`cmd`+`args`+`env_keys`+`envs` for stdio; `uri`+`headers` for streamable_http/sse; `code` for inline_python). Secrets via `env_keys` → OS keyring.
- Reload: not confirmed; config read on session start.

**Sources:** goose-docs.ai/docs/guides/{config-files,goose-cli-commands}, deepwiki.com/block/goose/{5.3,5.4}, block.github.io/goose/docs/guides/allowlist

### Amp (Sourcegraph)

**Binary:** `amp` (`npm i -g @sourcegraph/amp` or curl installer)

**Plugins:** YES, but file-drop semantics — no install CLI
- Plugins are TypeScript files: `~/.config/amp/plugins/*.ts` (user) or `<project>/.amp/plugins/*.ts` (workspace)
- Install = write `.ts` file; uninstall = delete file; list = directory scan
- Source: local `.ts` only (no npm/git package manager semantics)
- Reload: command-palette action `plugins: reload` (no documented unattended CLI reload)

**MCP servers:** YES
- Config file:
  - User: `~/.config/amp/settings.json` (NOT `~/.amp/` as our sandbox assumed — see "Sandbox path discrepancy" below)
  - Workspace: `<project>/.amp/settings.json`
- **Top-level key: `amp.mcpServers` (dotted key inside flat JSON, NOT nested object)**
- Per-server (stdio): `command`, `args`, `env` (supports `${VAR}` expansion)
- Per-server (remote): `url`, `headers`
- CLI commands (partial):
  - `amp mcp add <name> -- <cmd> [args]` (stdio)
  - `amp mcp add <name> <url>` (remote)
  - `amp mcp approve <name>` (workspace approval)
  - `amp mcp oauth login|logout <name>`
  - `amp mcp doctor` (status)
  - **`amp mcp remove` and `amp mcp list` are NOT documented as CLI commands** — edit `settings.json` directly
- One-shot: `amp -x ... --mcp-config <file>` passes MCP without modifying configs
- Reload: restart `amp` process to pick up edits

**Sources:** ampcode.com/manual, ampcode.com/manual/plugin-api, github.com/sourcegraph/amp-examples-and-guides

### Aider

**Binary:** `aider`

**Plugins:** NO. No plugin system; no `aider plugin install/list/uninstall`. Confirmed via aider.chat/docs.

**MCP servers:** NO native support. Community wrappers (`mcpm-aider`) exist as "experiments". Not official.

**State storage:** Aider uses dotfiles, NOT a `~/.aider/` directory:
- `~/.aider.conf.yml` (main config)
- `~/.aider.model.settings.yml`
- `~/.aider.model.metadata.json`
- `.aider.input.history`, `.aider.chat.history.md`, `.aider.llm.history` (typically in repo/cwd)

**Sources:** aider.chat/docs/{config,config/aider_conf,config/options,scripting,usage}, github.com/Aider-AI/aider

**Recommendation: skip Aider from the provisioning feature.** Driver would expose `SupportsPlugins() == false` and `SupportsMCP() == false`.

## Sandbox path discrepancies discovered during research

Two seatbelt module assumptions appear wrong against the verified user-config paths:

1. **Amp** — sandbox lists `~/.amp/` and `~/.config/amp/` as user defaults. Per Amp docs, `~/.amp/` is the *workspace* convention (`.amp/` inside a project), not a user-home location. User config lives at `~/.config/amp/`. The `~/.amp/` entry in `pkg/seatbelt/modules/amp.go` should be removed or scoped to workspace use only.

2. **Aider** — sandbox lists `~/.aider/` as a defaults dir, but Aider stores state in `~/.aider.*` dotfiles, not a directory. The sandbox should allow `~/.aider.conf.yml`, `~/.aider.model.settings.yml`, `~/.aider.model.metadata.json`, and `~/.aider.*.history` instead.

These are separate from the provisioning feature scope but worth filing as bug issues.

## Implications for the driver design

1. **The shared `internal/provision/mcpfile.go` (JSON + flat `mcpServers` key) only works directly for Gemini, Copilot, and Claude project-scope `.mcp.json`.** Other agents need:
   - Claude user-scope: path-aware reader for `projects.<repo>.mcpServers` nesting in `~/.claude.json`.
   - Amp: same JSON shape but key is `amp.mcpServers` (treat as one nested level).
   - Codex: TOML parser/writer (different file format).
   - Goose: YAML parser/writer with `extensions:` map (different format AND unified plugins/MCP).

2. **Plugin install/uninstall/list abstraction:**
   - Cleanest: Gemini (`gemini extensions ...`), Copilot (`copilot plugin ...`) — shell-out per Provisioner spec.
   - TUI-leaning: Claude, Codex — shell-out is risky unattended; consider falling back to config-file edits for the `enabled` toggle (Codex) or sentinel-file-based ownership tracking (Claude). At minimum, drivers should detect the interactive failure mode and surface a clear "install in a TTY" error.
   - File-edit only: Goose (YAML edit), Amp (write `.ts` file).
   - Not supported: Aider.

3. **Three driver implementation tiers proposed:**

   - **Tier 1 — well-supported, clean CLI:** Gemini, Copilot. Implement first. Provisioner Methods use shell-out to documented subcommands. MCP via shared JSON helper.
   - **Tier 2 — supported but quirky:** Claude (path-aware MCP reader for user-scope; plugin install requires TTY or fallback), Codex (TOML MCP handling; plugins via config flag toggle).
   - **Tier 3 — file-edit only, non-uniform shape:** Goose (YAML, unified extensions/MCP — may want a separate `Provisioner` shape that doesn't split plugins/MCP), Amp (TS plugin file-drop, nested MCP key).
   - **Skip:** Aider.

4. **Provisioner interface tweaks suggested:**
   - Add `RequiresTTY() bool` capability flag so `aide sync --yes` can short-circuit drivers that can't run unattended for plugins (Claude plugin, Codex plugin), with a clear error.
   - The shared `MCPFile` helper should be one of multiple implementations behind a per-driver `MCPHandler` interface, not the only path. Move to `internal/provision/mcp/` subpackage with `jsonflat.go`, `claudejson.go`, `codextoml.go`, `gooseyaml.go`, `amp.go`.

## Recommended next-step ordering (Tasks 11–17 revision)

| Order | Driver  | Effort | Notes                                                              |
| ----- | ------- | ------ | ------------------------------------------------------------------ |
| 1     | Gemini  | low    | Clean CLI for both. Test bed for the Provisioner contract.         |
| 2     | Copilot | low    | Plugin CLI clean; MCP via shared JSON helper.                      |
| 3     | Claude  | medium | Implement path-aware user-scope MCP reader. Plugin install: shell out, document TTY caveat. |
| 4     | Codex   | medium | TOML MCP handler. Plugin: `[plugins."x@y"] enabled` toggle only.   |
| 5     | Goose   | medium | YAML handler. Unified plugins/MCP — decide if `SupportsPlugins`/`SupportsMCP` both `true` or unified. |
| 6     | Amp     | medium | File-drop plugins (write/delete `.ts`). Nested-key MCP.            |
| —     | Aider   | skip   | Returns `false` for both.                                          |

The Provisioner interface and shared types from Tasks 1–10 still hold. The shared MCP helper needs to become pluggable to accommodate non-JSON formats; recommend a small refactor in a follow-up task before adding the Tier 2/3 drivers.
