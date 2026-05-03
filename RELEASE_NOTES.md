## v1.8.0

Two CLI surfaces redesigned for clearer mental model and a guided
empty-state experience when no context matches the current folder.

### 💥 Breaking changes

- `aide env set --from-secret KEY` is **removed**. Use one of:
  - `aide env set FOO --secret-key KEY --global`
  - `aide env set FOO --secret-store NAME --secret-key KEY --global`
  - `aide env set FOO --pick --global`
  - `--global` is now required for any secret reference.
- `aide context add` and `aide context add-match` are **removed**.
  Use `aide context create [name]` and `aide context bind <name>`.

### ✨ New

- `aide` now offers an interactive prompt when no context matches
  the current folder. Non-TTY mode prints concrete remediation
  commands.
- `aide context bind <name>` auto-detects the match rule (git
  remote when available, else folder path). `--path` and `--remote`
  override.
- `aide context create [name]` auto-detects the agent when exactly
  one supported agent is on PATH; prompts to bind cwd in TTY mode.

### 🔧 Internal

- New `examples_test.go` guard ensures every `aide ...` line in
  command help text actually parses against the real flag set.
- Cobra-level parsing tests for `env set` and `context bind/create`
  flag matrices.
