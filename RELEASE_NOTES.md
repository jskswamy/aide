## Unreleased

### 🧰 CLI

#### --agent now gets its own profile directory

In a context with `profile:`, `aide --agent pi` exported the context
agent's config directory (e.g. `CLAUDE_CONFIG_DIR=~/.claude-work`) and
gave Pi nothing, so Pi ran against its default `~/.pi`. The profile
directory now belongs to the agent that actually launches
(`PI_CODING_AGENT_DIR=~/.pi-work`).

- The profile is validated against that agent, so `env:` declaring its
  config-dir variable beside `profile:` is now rejected for `--agent`
  launches too.
- The original agent's config-dir variable is no longer passed to the
  agent you switched to.
- `aide which` and `aide status` accept `--agent` to show what such a
  launch would use.

#### remote_name in match rules now takes effect

A remote rule with `remote_name: upstream` was documented but ignored:
every remote rule was matched against `origin`. Each rule now matches
against the remote it names, so `remote_name: upstream` matches only
the `upstream` remote. Rules without `remote_name` still use `origin`.

#### Context matching is deterministic on ties

When two contexts scored equally for the same directory (for example,
identical remote rules), the winner could change from one run to the
next. The context whose name sorts first now always wins.

#### Remote matching normalizes host case, trailing slashes and ports

Remote URLs and remote rules are now compared in a normalized
`host[:port]/org/repo` form.

- Host names are case-insensitive: `GitHub.com/org/repo` matches a
  `github.com` remote, including in globs such as `GitHub.com/org/*`.
- A trailing `/` on a rule or a remote URL no longer prevents a match.
- **Behavior change:** a non-default port is now part of the identity.
  A rule `git.example.com/org/repo` no longer matches
  `ssh://git@git.example.com:2222/org/repo.git`; write the rule as
  `git.example.com:2222/org/repo` (or as the full URL). The same
  applies to globs: `git.example.com/*` becomes
  `git.example.com:2222/*`. A scheme's default port (`:22` for ssh,
  `:443` for https, `:80` for http, `:9418` for git) is dropped, so
  `ssh://git@github.com:22/org/repo.git` still matches
  `github.com/org/repo`.
- Match reasons in the launch banner, `aide which` and JSON
  `match_reason` no longer show credentials from a rule saved as a raw
  URL with a token; the rule is shown normalized.
- `aide context bind` saves remote rules in the normalized form
  (`github.com/org/repo`) instead of the raw URL, so tokens or
  passwords embedded in a remote URL are no longer copied into
  `config.yaml` or printed. A remote that is a local path (no host) is
  bound by folder path instead. Rules saved by earlier releases keep
  matching.

#### AIDE_PROFILE is exported alongside AIDE_CONTEXT

Launched agents and their child processes now see `AIDE_PROFILE` with
the resolved context name, the same value as `AIDE_CONTEXT`, which is
kept for existing scripts. `aide statusline` reads `AIDE_PROFILE` and
falls back to `AIDE_CONTEXT` for sessions started by older releases.

#### Clear error for profiles on agents without profile support

Selecting a context with `profile:` for an agent that cannot isolate a
profile now stops with `Profiles are not supported for harness
"opencode"`. Before, OpenCode and Cursor reported a driver-level
message, and Aider, Amp, Goose and unknown agents reported `no
provisioner registered for agent`.

- Profiles remain supported for Claude, Codex, Pi, Copilot and Gemini.

#### aide sync works for cursor-agent contexts

A context with `agent: cursor-agent` (the name `aide` discovers and
sandboxes) failed `aide sync` and the plugin/hook commands with `no
provisioner registered for agent "cursor-agent"`, because the Cursor
driver was registered as `cursor`. Both names now reach the Cursor
driver, so its hooks are synced, and the launch banner shows the
Cursor icon for `cursor-agent`. Existing `agent: cursor` contexts and
their sync state are unchanged.

### ⚙️ Configuration

#### config.yaml accepts profiles: alongside the legacy contexts:

`config.yaml` can now declare `profiles:` and `default_profile:`
instead of `contexts:` and `default_context:`. Both load the same way;
existing `contexts:` files keep working and are not rewritten. Commands
that save the config write back whichever form the file used.

- Inside a `profiles:` entry the native config-dir keys are
  `config_dir_name:` (was `profile:`) and `config_dir:` (was
  `profile_dir:`). The old names are rejected there with
  `profiles.<name>: profile: is a contexts: key; under profiles: use
  config_dir_name:`; the new names under `contexts:` are rejected the
  same way. `sandbox: {profile: ...}` (a named sandbox policy) is
  unchanged.
- A file that mixes the two forms (e.g. `contexts:` with `profiles:`,
  or `default_context:` with `default_profile:`) stops with `config
  mixes profiles: and contexts: declarations`, naming the keys found
  and how to convert.
- **Behavior change:** two profiles (or contexts) whose agent config
  dirs would be the same directory now stop with `profiles share a
  native config dir`. This covers symlinks, a symlinked parent of a
  dir not created yet, and (for existing dirs) case-insensitive
  filesystems. Because `--agent` can launch any profile under any
  agent, every agent is checked: `work-claude` (agent claude) and
  `work-codex` (agent codex) both with `profile: work` collide on
  `~/.claude-work`. The error explains why and how to keep both
  logins: merge the two into one profile, or separate them with
  `config_dirs` (for example `profile: work-codex` plus
  `config_dirs: {codex: ~/.codex-work}` on `work-codex`, and
  `config_dirs: {codex: ~/.codex-work-claude}` on `work-claude`).
- New `config_dirs:` map on a profile (or context) sets the config dir
  for individual agents, e.g. `config_dirs: {claude: ~/.claude-work,
  codex: ~/.codex-acme}`. For each agent aide uses its `config_dirs`
  entry if present, else the single `config_dir:`/`profile_dir:` (which
  still applies to every agent), else `~/.<agent>-<config_dir_name>`.
  Keys must be agents with profile support (claude, codex, copilot,
  gemini, pi), paths must be under `$HOME`, and the profile must also
  set `config_dir_name:` (`profile:` under `contexts:`); otherwise
  loading stops with `invalid config_dirs`.
- `.aide.yaml` may name an existing profile with `profile: <name>`. A
  name your config does not define prints a warning (once per run)
  naming the file and the profile, and aide carries on. Selecting that profile from
  `.aide.yaml` is not wired in yet.
- `.aide.yaml` can never define a profile: `profiles:`, `contexts:`,
  `default_profile:`, `default_context:`, `config_dir_name:`,
  `config_dir:`, `config_dirs:`, `profile_dir:`, `match:`, `credentials:` and
  `default_credentials:` stop with `profile not allowed in project
  override: <key>: defines a profile`, whether or not the file is
  trusted.
- Commands that save the config (such as `aide agents add`) now refuse
  to write when the config failed to load, with `refusing to write
  config: it failed to load`. Before, some of them wrote an empty
  config over your `config.yaml`.

### 🔧 Internal

#### Multi-agent profile design

A draft design documents the proposed evolution from contexts to profiles,
profile-isolated agent authentication, selected API credentials, repository
preferences, worktree configuration inheritance, and Skillshare integration.

- Design only; no runtime behavior or configuration format has changed.
- See the 2026-10-16 profiles design spec in `docs/superpowers/specs/`.

## v2.3.0 (2026-10-15)

Pi and OpenCode join the supported agents, the Linux sandbox now gives
every agent its own config directory, plugin/hook/MCP sync is fixed
across agents and profiles, and the toolchain and dependencies are
patched for twelve reachable vulnerabilities.

### ✨ New agents

#### aide can now sandbox Pi

`aide` gains a `provision.Provisioner` driver for Pi
(`earendil-works/pi`, pi.dev), so `aide context create --agent pi` and
`aide launch` work the same way they do for the other supported agents.

- The sandbox grants `~/.pi`, or `$PI_CODING_AGENT_DIR` when set.
  `PI_CODING_AGENT_DIR` is also Pi's profile env key.
- MCP servers are synced into Pi's built-in `mcp.json`
  (`~/.pi/agent/mcp.json`, or under `$PI_CODING_AGENT_DIR`), which
  uses the same `mcpServers` shape as Gemini and Copilot.
- Plugins (Pi "packages") are synced through Pi's own `pi install`,
  `pi remove` and `pi list`, always at user scope.
- Hooks are installed as generated TypeScript extensions in
  `~/.pi/agent/extensions/` (global scope only), which Pi auto-loads.
  `pre_tool` maps to Pi's `tool_call` event and `post_tool` to
  `tool_result`.
- Project-local Pi config still needs Pi's one-time project-trust
  approval; `aide` does not grant it on your behalf.

#### aide can now sandbox OpenCode

`aide` gains a `provision.Provisioner` driver for OpenCode
(`anomalyco/opencode`, opencode.ai), so `aide context create --agent
opencode` and `aide launch` work the same way they do for the other
supported agents.

- MCP servers and plugins are synced by editing `opencode.jsonc`
  directly rather than shelling out to OpenCode's own CLI, which can't
  express non-npm plugin sources (git/local refs) and has no
  list/remove counterpart at all.
- Hooks are installed as generated JS plugin files dropped in
  `~/.config/opencode/plugin/` (global scope only; this driver does not
  write project-scope hooks), which OpenCode auto-loads.

### 🐧 Linux sandbox

#### Agents can write their own config under the Linux sandbox

On Linux, Landlock only grants paths an agent module lists as writable,
and the shared module behind most agents listed none. Pi, OpenCode,
Codex, Gemini, Aider, Amp and Goose ran with no write access to their
own config directory. They now get it, matching macOS.

- Symlinked config dirs resolve to their real targets before being
  granted, as they already did for Claude and cursor-agent.
- macOS sandbox profiles are unchanged.

#### aide sync on Linux now provisions the Claude config dir the sandbox uses

On Linux, a sandboxed Claude launch points `CLAUDE_CONFIG_DIR` at
`~/.config/aide/claude`, but `aide sync` and the Claude permission grant
behind `aide sandbox allow --global` kept writing Claude's default
`~/.claude.json` and `~/.claude/settings.json`. MCP servers, hooks and
directory grants that aide synced were therefore invisible to the
sandboxed agent.

- Sync and grants now apply the same launch-time redirect, so they write
  where the sandboxed Claude reads. A `profile:` or a `CLAUDE_CONFIG_DIR`
  you set yourself still wins, and contexts with `sandbox: false` keep
  using Claude's defaults.
- Run `aide sync` once after upgrading so already-declared MCP servers
  and hooks land in `~/.config/aide/claude`. macOS is unaffected.
- Sync now records which agent config dir it provisioned into. When that
  dir changes (this fix, or switching a context's `profile:`), managed
  MCP servers are reinstalled into the new dir instead of being assumed
  present. Expect each one to be reinstalled once on the first sync
  after upgrading.

#### aide statusline --install targets the sandboxed Claude config on Linux

On Linux, a sandboxed Claude reads its config from
`~/.config/aide/claude`, but `aide statusline claude --install` wrote
the status line into `~/.claude/settings.json`, so it never appeared.
It now writes to the same directory `aide sync` uses. Unsandboxed
contexts and macOS still use `~/.claude`.

#### Launch banner names the agent again under the Linux sandbox

On Linux the startup banner showed `claude → /path/to/aide` because it
was rendered after the sandbox rewrote the command into aide's own
re-exec wrapper. It now shows the agent binary, as on macOS.

### 🔌 Plugin, hook & MCP sync

#### aide sync no longer requires the age key for routine no-op runs

`aide sync` previously decrypted a context's age-encrypted secrets file on
every run, even when nothing about the declared config or the secrets
themselves had changed. That coupled routine sync runs (e.g. as part of a
home-manager activation) to age-key availability and plaintext-secret
handling for no reason.

- Sync now skips decryption entirely when neither `config.yaml` nor the
  encrypted secrets file changed since the last successful sync,
  substituting the already-installed MCP env values instead of
  re-resolving `{{ .secrets.X }}` templates.
- A new `--force-secrets` flag bypasses this gate on demand, forcing a
  real decrypt + re-resolve, the remedy if an installed secret value is
  manually edited or corrupted, since that drift is no longer auto-healed
  on every sync the way it used to be.

#### URL-direct plugins no longer reinstall on every sync

Agents that take plugins as bare install refs (OpenCode, and now Pi)
report installed plugins by that ref (`npm:@acme/hello`), never by the
readable key you chose in `config.yaml`. `aide sync` matched on the key,
so every run reinstalled each such plugin and listed its ref as
unmanaged, and removing an entry from config tried to uninstall the key,
which the agent didn't recognise, leaving the plugin installed.

- Sync and `aide plugin list` now credit a reported install ref to its
  config key, and uninstall by the ref aide recorded when installing it.
- Plugins synced before this fix are matched on the next sync, which
  also records their ref for later removal.

#### aide sync follows an exported agent config dir

If you export `CLAUDE_CONFIG_DIR` (or another agent's config-dir
variable such as `PI_CODING_AGENT_DIR`, `CODEX_HOME`, `GEMINI_HOME` or
`COPILOT_HOME`) in your shell rather than through `profile:`, the agent
reads that directory at launch, but `aide sync` still wrote hooks into
the default one. Sync and `aide statusline --install` now target the
exported directory. A value set in the context's `env:` or `profile:`
still takes precedence.

#### Codex, Copilot and Gemini profiles get their own MCP config and hooks

`aide sync` now writes Codex's `config.toml`, Copilot's
`mcp-config.json` and Gemini's hook scripts under `CODEX_HOME`,
`COPILOT_HOME` and `GEMINI_HOME` when the context's `profile:`, `env:`
or your shell sets them. Before, these files always went to `~/.codex`,
`~/.copilot` and `~/.gemini`, so an agent running under a profile never
saw the MCP servers or hooks aide synced for it.

#### Claude marketplaces keyed by a URL or local path now install their plugins

A Claude marketplace declared by URL or by local path, e.g.
`/abs/path/to/marketplace: [hello]`, was added but its plugins failed to
install with `Plugin "hello" not found in marketplace`, and every sync
tried to add the marketplace again. aide now matches the URL or path to
the name Claude registered the marketplace under and installs
`hello@<marketplace name>`.

#### Hook matchers now apply to OpenCode and Pi

A hook scoped with `matcher:` (e.g. `matcher: shell`) fired on every
tool call under OpenCode and Pi, because neither agent has a matcher
field and the generated hook scripts ignored it. The generated plugin
or extension now checks the tool name and only runs the command for the
matched tool (`shell` maps to `bash` in both). The same command can be
declared with and without a matcher, or under several matchers, each
getting its own file. Hooks without a matcher are unchanged.

#### session_start and session_end hooks now work for OpenCode and Pi

`session_start` and `session_end` hooks were silently skipped for
OpenCode and Pi. They are now installed:

- OpenCode: `session_start` runs when a top-level session is created
  (subagent sessions are skipped); `session_end` runs when OpenCode
  exits normally, not when it is killed.
- Pi: `session_start` and `session_end` map to Pi's `session_start`
  and `session_shutdown`, skipping extension reloads.
- As with Claude, both also fire when you switch to a new or resumed
  session.

#### aide plugin list labels URL-direct sources by type

`aide plugin list` showed `marketplace npm:@x/y` or
`marketplace file:///...` for plugins declared with a URL-direct
source, because every unrecognised source fell back to `marketplace`.
The DECLARED column now names the source type: `npm`, `file`, `url`
(http/https), `git`, `github` (including bare `owner/repo`) or
`local`. Plugins installed from a marketplace still show `marketplace`.

### 🧰 CLI

#### aide commands support --format json output

Read/query commands (`aide which`, `aide cap list`, `aide status`, etc.)
now support `--format json` for machine-readable output, alongside the
existing human-readable default.

- Every read/query command accepts `--format json` (default: `human`).
- The `$AIDE_FORMAT` environment variable sets the default when
  `--format` isn't passed explicitly (flag > env var > `human`).
- `aide explain`'s existing `--format human|agent|json` flag is
  unchanged.
- Errors under `--format json` are emitted as a `{"error": "..."}` JSON
  object instead of plain text.

#### aide which reports the real sandbox tier

`aide which` always printed `sandbox: disabled`, even for contexts
that launch under Landlock or Seatbelt, because only the launch path
evaluated the isolation tier. It now shows the same tier the launch
banner does, e.g. `primary (Landlock ABI 4)`, and `--format json`
includes `isolation_tier`.

#### aide sandbox allow/deny now sync Claude's own permission store

`aide sandbox allow <dir>` and `aide sandbox deny <dir>` previously only
touched aide's own OS-level sandbox policy (`readable_extra`/
`writable_extra`/`denied_extra`), leaving the same edit to be made by hand in
Claude Code's `settings.local.json`/`settings.json`
`permissions.additionalDirectories`. That manual step now happens
automatically as part of the same command.

- The agent-side edit is best-effort: it never fails the aide-side
  mutation, printing a warning instead if it can't be applied.
- Both commands gained a `--no-agent-grant` flag to opt out and keep the
  old aide-only behavior.
- The new `provision.DirectoryGranter` interface is the extension point;
  other agent drivers can implement it later to gain the same behavior.
- `sandbox deny` correctly revokes nested grants (e.g. denying `/repo`
  after allowing `/repo/sub`), not just exact path matches, without
  touching unrelated parent grants.

#### Auto-approve works with Pi and OpenCode

With `yolo: true` in your preferences or a context, or with
`--auto-approve`, launching Pi or OpenCode failed with `--yolo not
supported for agent`.

- OpenCode now gets its `--auto` flag.
- Pi never asks for permission, so it launches unchanged. Its
  `--approve` flag trusts project-local files, a separate decision
  aide leaves to you.

### 🔒 Security

#### Go 1.26.9 and patched dependencies resolve twelve vulnerabilities

govulncheck reported twelve vulnerabilities reachable from aide's code
in the Go standard library and in dependencies, failing the scheduled
security check. Release binaries and the Nix flake now build with
patched versions.

- Go toolchain 1.26.6 → 1.26.9 (`crypto/tls`, `net/http`,
  `html/template`, `net/textproto`). The Nix flake pins Go 1.26.9
  until nixpkgs ships it.
- `golang.org/x/net` v0.56.0 → v0.60.0,
  `go.opentelemetry.io/otel/sdk` v1.44.0 → v1.45.0 and
  `google.golang.org/grpc` v1.83.1 → v1.83.2.
