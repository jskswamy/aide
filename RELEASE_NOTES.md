## v2.0.0 — 2026-06-22

This release brings first-class Linux support: aide now enforces OS-level
isolation on Linux, not just macOS. Everything since v1.14.1 ships here,
including the `aide explain` command and named hooks.

### 🐧 Linux sandbox

#### Landlock LSM with a bubblewrap fallback

aide now enforces the same deny-default, guard-based isolation on Linux
that it has long provided on macOS via Seatbelt. Filesystem access,
subprocess execution, and outbound network are all restricted at the
kernel layer — no container or VM required. Isolation is reported as one
of three tiers, so you always know exactly what is being enforced.

- **primary** — full filesystem rules plus per-port TCP enforcement
  (Landlock ABI ≥ 4, kernel ≥ 6.7)
- **degraded** — filesystem isolation only, no per-port TCP control
  (older Landlock with port rules configured, or the bubblewrap fallback)
- **unavailable** — no Landlock and no `bwrap`; the agent launches with a
  stderr warning and no OS-level isolation

| Platform | Mechanism | Tier |
| --- | --- | --- |
| Linux, kernel ≥ 6.7 | Landlock (ABI ≥ 4) | primary |
| Linux, kernel 5.13–6.6 | Landlock (ABI 1–3) | primary / degraded |
| Linux, no Landlock | bubblewrap (`bwrap`) | degraded |
| Linux, no Landlock or bwrap | — | unavailable |

Check the active backend and tier at any time:

```sh
aide sandbox show   # active backend + isolation tier
aide status         # tier alongside the rest of the run context
```

See [docs/sandbox.md](docs/sandbox.md) for the full platform table,
minimum system requirements, and Linux/macOS differences.

#### Subprocess and network confinement

Subprocess execution and outbound network are confined on Linux using the
same model aide already applies on macOS.

- Subprocess execution is gated with a seccomp-bpf filter and a PID
  namespace (`AllowSubprocess`).
- Outbound network follows the same mode model as macOS (`none` /
  `outbound` / `unrestricted`).
- Per-port TCP allow/deny lists require Landlock ABI ≥ 4; on earlier
  kernels the network mode applies to all ports.

#### aide-managed agent config directory

On Linux the agent's configuration lives in an aide-managed directory so
it fits the Landlock writable allow-list.

- The agent's config is redirected to `~/.config/aide/claude`, the only
  path in the Landlock writable allow-list.
- Point `CLAUDE_CONFIG_DIR` elsewhere by adding that path to
  `writable_extra`.

### 🔍 `aide explain`

#### Configuration overview and how-to recipes

A new `aide explain` command prints a redacted snapshot of the current
aide configuration alongside topic-based how-to recipes. It never resolves
or decrypts secrets (T1 threat model): literal credential values are
replaced with `<redacted>` and secret-ref templates are shown symbolically
(`{{ .secrets.github_token }}`). Non-credential literals and safe
templates (e.g. `{{ .project_root }}/data`) are shown verbatim so the
output is actually useful.

- `aide explain` — human-readable terminal overview (default)
- `aide explain --format=agent` — consolidated markdown for injecting into
  an agent's context window
- `aide explain --format=json` — machine-readable JSON for tooling

#### Single-recipe output with `aide explain <topic>`

Pass a topic name to print just that recipe body, making it easy to pipe a
specific how-to into an agent context. Running `aide explain` without a
topic lists all available topics.

```sh
aide explain add-mcp-server
aide explain scope-a-sandbox
aide explain configure-hooks
```

#### Built-in recipes for MCP servers, sandbox scoping, and hooks

Three how-to recipes ship with this release.

- `add-mcp-server` — add a top-level MCP server, scope it to one context
  via `extra:`, or exclude a server from specific contexts
- `scope-a-sandbox` — add writable/readable/denied paths to a context
  sandbox, use a named sandbox profile, or disable the sandbox
- `configure-hooks` — configure global hooks, add context-scoped extra
  hooks, or exclude inherited hooks per context

#### Hook summary in the output

The explain output now includes hook information so the full configuration
picture is visible at a glance.

- Top-level hook counts are shown.
- Per-context hook overrides (exclude/extra) are listed.

### 🪝 Hooks

#### Named hooks with per-context exclusion

Hook entries now support an optional `name:` field, and per-context
overrides can suppress specific hooks by name without dropping the entire
event type.

- Use `exclude_hooks:` (an event-scoped map of hook names) to suppress
  named hooks per context. Unnamed hooks are never affected.
- `ValidateHooks` catches unknown names at config-load time with a
  descriptive error naming the context and event.

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

See `aide explain configure-hooks` for the full syntax reference.

### 🔒 Security

#### Read-only secrets store during introspection

The secrets store is opened read-only during `configure` and `doctor`
sandbox operations.

- The secrets provider opens the keystore in read-only mode when called
  from the explain/configure/doctor code path, preventing accidental
  writes during introspection.

### Contributors

Thanks to everyone who contributed to this release:

- Selvakumar Natesan ([@selvakn](https://github.com/selvakn)) — Linux Landlock/seccomp sandbox
