# Configure hooks

Hooks run shell commands at agent lifecycle events. Supported events:
`pre_tool`, `post_tool`, `session_start`, `session_end`, `stop`.

Top-level hooks apply to every context. Per-context overrides can add
extras or exclude inherited events.

## Add a hook to all contexts

    hooks:
      pre_tool:
        - command: my-guard-script
          matcher: shell

## Add a hook to one context only

    contexts:
      work:
        hooks:
          extra:
            pre_tool:
              - command: work-audit-hook
                matcher: shell

## Exclude a top-level hook in one context

    hooks:
      pre_tool:
        - command: global-guard

    contexts:
      personal:
        hooks:
          exclude: [pre_tool]

## Exclude a specific hook by name

Add a `name:` field to individual hooks, then use `exclude_hooks:` in a
context to suppress only those hooks by name. Unnamed hooks are never
affected by `exclude_hooks`.

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
            pre_tool: [guard]

The `personal` context inherits `audit-log` but not `global-guard`.
The existing `exclude: [event]` form still drops an entire event type.
