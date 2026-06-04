## v1.15.0 — 2026-06-04

### ✨ Features

- **`aide explain` — configuration overview and how-to recipes.**
  A new `aide explain` command prints a redacted snapshot of the current
  aide configuration alongside topic-based how-to recipes. It never
  resolves or decrypts secrets (T1 threat model): literal credential
  values are replaced with `<redacted>` and secret-ref templates are
  shown symbolically (`{{ .secrets.github_token }}`). Non-credential
  literals and safe templates (e.g. `{{ .project_root }}/data`) are
  shown verbatim so the output is actually useful.

  Three output formats are supported:

  - `aide explain` — human-readable terminal overview (default)
  - `aide explain --format=agent` — consolidated markdown for injecting
    into an agent's context window
  - `aide explain --format=json` — machine-readable JSON for tooling

- **`aide explain <topic>` — print a single recipe.**
  Pass a topic name to print just that recipe body, making it easy to
  pipe a specific how-to into an agent context:

  ```sh
  aide explain add-mcp-server
  aide explain scope-a-sandbox
  aide explain configure-hooks
  ```

  Running `aide explain` without a topic lists all available topics.

- **Built-in recipes for MCP servers, sandbox scoping, and hooks.**
  Three recipes ship with this release:

  - `add-mcp-server` — add a top-level MCP server, scope it to one
    context via `extra:`, or exclude a server from specific contexts
  - `scope-a-sandbox` — add writable/readable/denied paths to a
    context sandbox, use a named sandbox profile, or disable the sandbox
  - `configure-hooks` — configure global hooks, add context-scoped
    extra hooks, or exclude inherited hooks per context

- **Hooks summarised in `aide explain` output.**
  Top-level hook counts and per-context hook overrides (exclude/extra)
  are now included in the explain output so the full configuration
  picture is visible at a glance.

- **Named hooks with per-context exclusion.**
  Hook entries now support an optional `name:` field. Per-context
  overrides can use `exclude_hooks:` (event-scoped map of hook names)
  to suppress specific hooks without dropping the entire event type.
  Unnamed hooks are never affected by `exclude_hooks`.

  ```yaml
  hooks:
    pre_tool:
      - command: global-guard
        name: guard
        matcher: shell
      - command: audit-log
        name: audit

  contexts:
    personal:
      hooks:
        exclude_hooks:
          pre_tool: [guard]   # audit-log still runs; guard does not
  ```

  `ValidateHooks` catches unknown names at config-load time with a
  descriptive error naming the context and event. See
  `aide explain configure-hooks` for the full syntax reference.

### Security

- **Secrets store is read-only during `configure` and `doctor` sandbox
  operations.** The secrets provider now opens the keystore in read-only
  mode when called from the explain/configure/doctor code path, preventing
  accidental writes during introspection.
