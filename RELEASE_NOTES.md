## Unreleased

### ✨ Features

- **Declarative hook management for five coding agents.** A new `hooks:`
  config block and `aide hook` CLI let you declare agent lifecycle hooks in
  `config.yaml` and reconcile them into each agent's config via `aide sync`
  — the same plan/apply model used for plugins and MCP servers.

  Declare once, apply everywhere:

  ```yaml
  hooks:
    pre_tool:
      - command: "rtk hook {agent}"   # {agent} expands per context
  ```

  The `{agent}` template variable is replaced with each context's resolved
  agent name, so a single top-level declaration covers all contexts without
  duplication. Per-context `hooks.extra` and `hooks.exclude` let individual
  contexts add or suppress events.

  **Agent support matrix:**

  | Agent | `pre_tool` | `post_tool` | `session_start` | `session_end` | Storage format |
  |-------|-----------|------------|----------------|--------------|---------------|
  | Claude | ✓ | ✓ | ✓ | ✓ | `~/.claude/settings.json` |
  | Gemini | ✓ | — | — | — | `~/.gemini/hooks/aide_*.sh` |
  | Cursor | ✓ | — | — | — | `~/.cursor/hooks.json` |
  | Copilot | ✓ | — | — | — | `~/.config/copilot/hooks/aide-*.json` |
  | Hermes | ✓ | — | — | — | `~/.hermes/plugins/aide_*/` |
  | Codex | — | — | — | — | not supported |

  The `shell` matcher narrows a hook to the Bash/shell tool; it maps to the
  agent's native name (`Bash` for Claude, `Shell` for Cursor).

  Aide uses `managed.json` as the sole ownership record for hooks it writes.
  `WriteHooks` receives the previously-managed set and the desired set: it
  removes only entries aide wrote last time and writes the new desired set,
  so user-added hooks in agent config files are never touched.

  Shell metacharacters (`;|&\`$(){}!<>\\\"'\n\r\t*?[]#~`) are rejected in
  command values to prevent injection.

  **CLI:**

  ```bash
  aide hook list [--context <name>]
  aide hook add  --event <event> --command <cmd> [--matcher <matcher>]
  aide hook remove --event <event> --command <cmd> [--matcher <matcher>]
  aide sync   # writes to agent config
  ```


### 🔒 Security

- **Bump `github.com/go-git/go-git/v5` from 5.19.0 to 5.19.1.**
  Closes two upstream advisories surfaced by Dependabot against the
  go-git transitively used by `aide`'s git-aware code paths:

  - **CVE-2026-45571 / GHSA-crhj-59gh-8x96 (medium, CVSS 5.4, CWE-22).**
    Path validation in go-git's checkout logic had drifted from
    canonical Git, letting a crafted repository payload modify files
    outside the intended worktree — including the repository's `.git`
    directory (and submodule `.git` dirs, since submodule dotgit
    materialization escapes the worktree filesystem isolation that
    otherwise contains the main repo). 5.19.1 restores the upstream
    checks.
  - **CVE-2026-45570 / GHSA-m7cr-m3pv-hgrp (low, CWE-116).** The SSH
    transport wrapped repository paths in single quotes without
    escaping embedded single quotes, diverging from canonical Git's
    `sq_quote_buf`. A path containing `'` could break out of the
    quoted region in the remote exec command. The vulnerable behavior
    is on the SSH *server* side (servers that re-evaluate
    `$SSH_ORIGINAL_COMMAND` through a shell); canonical `git-shell`
    setups are not affected. 5.19.1 ports `sq_quote_buf` so go-git's
    wire output is byte-identical to canonical Git's.

  Exploitation requires interacting with attacker-controlled
  repositories or shell-evaluating SSH servers — same threat model
  as cloning a hostile remote — but the upgrade is mechanical and
  the patched release is API-compatible.

### ✨ Architecture

- **MCP management goes through each agent's own CLI, not direct
  config-file edits.** New `provision.MCPInstaller` interface lets
  drivers implement `InstalledMCPServers` / `InstallMCPServer` /
  `UninstallMCPServer` against the agent's native `mcp` subcommand,
  the same way plugin install/uninstall has always worked. The
  engine prefers `MCPInstaller` over the legacy file-handler
  (`MCPHandler`) when both are available, so callers transparently
  pick up the new path.

  Three drivers migrate to the CLI path in this release:

  - **claude** — uses `claude mcp add-json --scope user`, `claude mcp
    remove <name> -s user`, and per-name `claude mcp get <name>` to
    populate the installed set. Claude requires `"type": "http"`
    alongside HTTP URLs in `~/.claude.json` and silently drops
    entries that omit it; routing through `add-json` keeps aide on
    the same schema claude's own CLI uses, so version drift no
    longer breaks aide. `MCPConfigPath` and `MCPHandler` now return
    empty/nil — direct edits of `~/.claude.json` (or the previous
    project-scope `.mcp.json`) are gone.

  - **gemini** — uses `gemini mcp add --scope user --transport ...`
    and `gemini mcp remove <name> -s user`. `gemini mcp list`
    output is parsed for the installed set (gemini has no
    per-name `get` subcommand). Env vars are not exposed in
    list output, so stdio entries with env may show a benign
    re-install on each sync until upstream surfaces them.

  - **codex** — uses `codex mcp add <name> --url ...` (HTTP) or
    `codex mcp add <name> --env K=V -- <command> [args...]`
    (stdio), and per-name `codex mcp get <name> --json` for the
    installed set. Codex's `--json` schema was derived from its
    public reference, not exercised against a live binary in this
    session; verify if you're running codex sync in production.

  **copilot** stays on the file-handler path for now: GitHub's
  Copilot CLI documents an interactive `/mcp add` REPL command
  only, with no confirmed non-interactive subcommand at this
  release.

### 🐞 Bug Fixes

- **`aide sync` no longer hard-bails on unmanaged plugins/MCP
  servers.** Previously, a plain `aide sync` on a context whose
  agent already had any plugin or MCP server aide didn't know
  about would fail with `unmanaged plugins/MCP servers detected;
  run `aide adopt` first or rerun with --yes`. That forced an
  unrelated workflow (`aide adopt`) whenever installs touched a
  context with pre-existing tooling. The block didn't prevent
  any actual harm — unmanaged items resolve to `OpIgnore`, which
  the engine genuinely skips — so it only added friction. Sync
  now prints `Note: N unmanaged item(s) will be left alone. Run
  `aide adopt` to bring them under aide.` before the existing
  `[y/N]` prompt and proceeds normally. Behaviour with `--yes`
  is unchanged.

- **1mcp (and other URL-based MCP servers) no longer fail silently
  in Claude.** Previously, `aide sync` for the claude agent wrote
  `<project>/.mcp.json` (project scope), which requires per-project
  approval inside Claude before entries appear in `claude mcp list`
  or connect. Shared aggregators like `1mcp`, declared once at the
  top level of `config.yaml` and intended to be available
  everywhere, were unreachable without visiting every matched
  directory and accepting the prompt. With the CLI-driven refactor
  above, aide installs to user scope via `claude mcp add-json
  --scope user`, so a single `aide sync` suffices and `claude mcp
  list` shows the entry immediately. Existing per-project
  `.mcp.json` files written by aide remain on disk as orphans;
  delete them manually after the upgrade.

- **Deterministic order for minimal-format `mcp_servers`.** When
  `config.yaml` used the legacy list-form syntax
  (`mcp_servers: [git, context7]`) under a minimal/flat config,
  `normalizeMinimal` rebuilt the synthesised default context's
  `MCPServers` slice by iterating the parsed `MCPServerMap` — a Go
  map, so iteration order is randomized. The slice came out in a
  different order on every run, surfaced as a flaky
  `TestLoad_MinimalConfig` on CI (`expected mcp_servers [git, context7],
  got [context7 git]`). The slice is now sorted lexicographically
  before `normalizeMinimal` returns, so callers see a stable order
  regardless of map seed. The original YAML sequence order was already
  destroyed at parse time — `MCPServerMap.UnmarshalYAML`'s sequence
  branch stores names as keys in a map — so a sort-on-emit is the
  smallest deterministic fix; full YAML-order preservation would
  require a parallel `[]string` or AST-level round-trip and is left
  as a follow-up if it ever proves necessary.
