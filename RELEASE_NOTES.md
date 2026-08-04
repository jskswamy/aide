## v2.1.0 (2026-08-03)

### Feature

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
