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

### 🔧 Internal

- Fixed OpenCode yolo argument placement for subcommands. OpenCode v2 strictly
  requires flags to appear after subcommands (e.g. `opencode run --auto`),
  rejecting `--auto` when prepended before subcommands. aide now inserts `--auto`
  after `run` and omits it for subcommands that do not accept it.
