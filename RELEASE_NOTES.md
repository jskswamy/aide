## Unreleased

<!--
Add release notes here as specs ship. Group under the headings
below. When tagging, replace `## Unreleased` with `## vX.Y.Z`,
commit, then tag.

Headings (use only what applies; drop empty sections):
  ### 💥 Breaking changes
  ### ✨ New
  ### 🔧 Internal

After tagging, reset this file to this template.
-->

### ✨ New

#### Custom model gateway management (`aide gateway`)

aide now manages custom-hosted LLM gateways and model catalogues across Claude Code, Pi, OpenCode, and Codex via `aide gateway add`, `list`, `use`, `edit`, `rename`, `sync`, and `remove`.

- Stores gateway bearer tokens in `aide`'s encrypted `age` secret store (via interactive hidden prompt, `--token-stdin`, or `--token-env <VAR>`) and records only non-secret gateway settings under `profiles.<profile>.gateways.<name>` in `config.yaml`.
- Supports renaming gateways, rotating bearer tokens, and updating gateway URLs, TTLs, and target agents in place via `aide gateway edit [<name>]` and `aide gateway rename <old-name> [<new-name>]`, automatically migrating encrypted secret keys (`<old>_token` → `<new>_token`), cached model catalogues, `default_credentials`, repository preferences, and agent config files.
- Queries `<base_url>/v1/models`, caches the normalized model catalogue under `~/.local/state/aide/gateways/<profile>/<gateway>.json`, and materializes per-agent model configuration files (`gateway-models.json` for Claude Code, `models.json` for Pi, `opencode.jsonc` for OpenCode, and `config.toml` for Codex) without writing plaintext tokens to disk.
- Automatically refreshes expired gateway model catalogues at launch (default `refresh_ttl: 24h`) or on demand with `aide gateway sync`.
- Resolves default credentials per agent (`DefaultCredentialsForHarness`) and switches a profile's active default between custom gateways and built-in provider credentials via `aide gateway use [<name>]` (or `--default` on `aide gateway add`, or per launch with `aide --credential <name>`). `aide gateway list`, `aide which`, and the startup banner show active and standby gateways, target endpoints, and agent config directories.
- `base_url` must be `https://` except for `localhost`, `127.0.0.1` and `::1`, and `.aide.yaml` cannot declare `gateways:`.
- Codex reaches the gateway over the OpenAI protocol, so the gateway must also serve `/v1/chat/completions`.

#### Support for multiple profiles in OpenCode

aide now supports multiple profiles for OpenCode via `OPENCODE_CONFIG_DIR`.

- Automatically routes profile configuration (`opencode.jsonc`, plugins, hooks, skills) to isolated profile directories (`~/.opencode-<profile>` or custom `config_dirs`).
- Seatbelt sandbox restricts write access to the active profile's directory.
- OpenCode provision driver manages MCP servers and hooks per profile.
- The sandbox keeps OpenCode's data, state and cache dirs (`~/.local/share/opencode`, etc.) writable when `OPENCODE_CONFIG_DIR` is set; previously OpenCode crashed at startup with `EPERM` opening `opencode.log`.

#### OpenCode launches use a private server

aide now starts OpenCode with `--standalone` (root, `run` and `mini`).
OpenCode v2 otherwise reuses one per-user background service that keeps the
environment of whichever launch started it, so a profile's credentials
(e.g. `ANTHROPIC_API_KEY`) were ignored and the server ran outside the
sandbox. Pass `--server <url>` to opt out.

The sandbox also lets OpenCode open the directories and config entries it
resolves at startup: the home dir, each ancestor of the project, and any
existing `.claude`, `.agents`, `.opencode`, `opencode.json` or
`opencode.jsonc` in them. OpenCode does this whatever the profile, and its
server answers 500 if opening one is denied. Only the entries themselves are
granted (names are visible), never what is inside them.

### 🔧 Internal

- Fixed OpenCode yolo argument placement for subcommands. OpenCode v2 strictly
  requires flags to appear after subcommands (e.g. `opencode run --auto`),
  rejecting `--auto` when prepended before subcommands. aide now inserts `--auto`
  after `run` and omits it for subcommands that do not accept it.
