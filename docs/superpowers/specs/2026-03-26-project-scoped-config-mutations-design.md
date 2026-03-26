# Project-Scoped Config Mutations

**Goal:** Config mutation commands (`cap enable`, `sandbox deny`, `env set`, etc.) should default to project-level (`.aide.yaml`) instead of user-level (`~/.config/aide/config.yaml`), with a `--global` flag to opt into user-level when needed.

---

## Problem

All mutation commands today write to the global config at `~/.config/aide/config.yaml`. When a user runs `aide cap enable k8s` inside a terraform project, the capability is added to the resolved context in global config — affecting that context everywhere it matches, not just this project.

Users expect `cap enable k8s` to mean "enable k8s for *this project*", the same way `git config user.name "X"` sets the name for *this repo* and `git config --global` sets it system-wide.

---

## Design

### Scope split

Commands fall into two categories:

**Project-scoped by default** — commands that mutate fields on a resolved context:

| Command | Field mutated on ProjectOverride |
|---------|----------------------------------|
| `cap enable` | `Capabilities` (append) |
| `cap disable` | `Capabilities` (remove) or `DisabledCapabilities` (append, see below) |
| `sandbox deny` | `Sandbox.DeniedExtra` (append) |
| `sandbox allow --read` | `Sandbox.ReadableExtra` (append) |
| `sandbox allow --write` | `Sandbox.WritableExtra` (append) |
| `sandbox reset` | `Sandbox` (set to nil, removing all project sandbox overrides) |
| `sandbox network` | `Sandbox.Network` (replace) |
| `sandbox ports` | `Sandbox.Network.AllowPorts` (replace, preserves `Network.Mode`) |
| `sandbox guard` | `Sandbox.GuardsExtra` (append) |
| `sandbox unguard` | `Sandbox.Unguard` (append) |
| `env set` | `Env` (set key) |
| `env remove` | `Env` (remove key) |

All accept `--global` to target user-level config instead.

**Always global** — commands that define reusable registries or top-level settings:

- `agents add/remove/edit` — agent definitions
- `sandbox create/edit/remove` — named sandbox profiles
- `cap create/edit` — capability definitions
- `cap never-allow` — global deny list
- `context add/rename/remove/set-default` — context management

These do not get a `--global` flag; they always write to `~/.config/aide/config.yaml`.

### ProjectOverride schema change

Add a `DisabledCapabilities` field to `ProjectOverride`:

```go
type ProjectOverride struct {
    // ... existing fields ...
    Capabilities         []string `yaml:"capabilities,omitempty"`
    DisabledCapabilities []string `yaml:"disabled_capabilities,omitempty"`
}
```

This allows the project to negate capabilities provided by the global context. See "Capabilities merge semantics" below for resolution logic.

### Capabilities merge semantics

Change `applyProjectOverride()` in `internal/context/resolver.go`:

```go
// Before (replace):
if len(po.Capabilities) > 0 {
    rc.Context.Capabilities = po.Capabilities
}

// After (additive merge, then subtract disabled):
if len(po.Capabilities) > 0 || len(po.DisabledCapabilities) > 0 {
    merged := dedup(append(rc.Context.Capabilities, po.Capabilities...))
    rc.Context.Capabilities = subtract(merged, po.DisabledCapabilities)
}
```

Resolution: `union(context.Capabilities, project.Capabilities) - project.DisabledCapabilities`

`cap disable` at project level:
- If the capability is in `ProjectOverride.Capabilities`, remove it from there
- If the capability is in the global `Context.Capabilities` (not in the project list), add it to `ProjectOverride.DisabledCapabilities`
- This way `aide cap disable docker` works regardless of which level defined the capability

### Sandbox merge semantics

Change `applyProjectOverride()` to merge sandbox fields individually instead of replacing the entire `Sandbox`:

**Additive fields** (append + dedup):
- `DeniedExtra`, `ReadableExtra`, `WritableExtra` — path lists
- `GuardsExtra`, `Unguard` — guard lists

**Replace-if-set fields:**
- `Writable`, `Readable`, `Denied`, `Guards` — base lists (override replaces)
- `Network` — network policy (override replaces)
- `AllowSubprocess`, `CleanEnv` — booleans (override replaces)
- `Disabled` — sandbox disable flag (override replaces)

```go
// Before (wholesale replace):
if po.Sandbox != nil {
    rc.Context.Sandbox = config.SandboxPolicyToRef(po.Sandbox)
}

// After (field-level merge):
if po.Sandbox != nil {
    ensureInlineSandboxOnContext(rc)
    inline := rc.Context.Sandbox.Inline
    // Additive fields
    inline.DeniedExtra = dedup(append(inline.DeniedExtra, po.Sandbox.DeniedExtra...))
    inline.ReadableExtra = dedup(append(inline.ReadableExtra, po.Sandbox.ReadableExtra...))
    inline.WritableExtra = dedup(append(inline.WritableExtra, po.Sandbox.WritableExtra...))
    inline.GuardsExtra = dedup(append(inline.GuardsExtra, po.Sandbox.GuardsExtra...))
    inline.Unguard = dedup(append(inline.Unguard, po.Sandbox.Unguard...))
    // Replace-if-set fields
    if len(po.Sandbox.Writable) > 0 { inline.Writable = po.Sandbox.Writable }
    if len(po.Sandbox.Readable) > 0 { inline.Readable = po.Sandbox.Readable }
    // ... etc for other replace fields
}
```

Note on types: `ProjectOverride.Sandbox` is `*SandboxPolicy` (not `*SandboxRef`). Project-path mutation commands operate directly on the `*SandboxPolicy` fields — they do not need `ensureInlineSandbox()` which is only for the `SandboxRef` wrapper used in `Context`.

Note on merge precondition: when the resolved context uses a profile name reference (e.g. `sandbox: strict`), `applyProjectOverride` must expand the profile into an inline `SandboxPolicy` before merging additive fields. Additive fields cannot be appended to a profile name string. This expansion is handled by a helper (`ensureInlineSandboxOnContext`) that resolves the profile from `Config.Sandboxes[name]` into `Context.Sandbox.Inline`.

Note on `*bool` fields: `AllowSubprocess` and `CleanEnv` are `*bool` — guarded with `!= nil`, no zero-value ambiguity. `Disabled` is a bare `bool` with `yaml:"-"` set via custom `UnmarshalYAML` using the `sandbox: false` form — not a direct struct field, so no ambiguity in practice.

### Project override write support

New function in `internal/config/`:

```go
func WriteProjectOverride(path string, po *ProjectOverride) error
```

- If `.aide.yaml` does not exist at `path`, creates it (the starting state is an empty `ProjectOverride{}`)
- Marshals the updated `ProjectOverride` struct to YAML
- Writes atomically (temp file + rename, same pattern as `WriteConfig`)

### Project root discovery for writes

When `--global` is not set, the command needs to find where to write `.aide.yaml`.

The existing `findProjectConfig()` does a single walk up from cwd: at each directory it checks for `.aide.yaml`, then checks for `.git` and stops if found. The write-path discovery reuses this same walk but adds a creation fallback:

1. Single walk up from cwd: at each level, check for `.aide.yaml` (return if found), then check for `.git` (stop walking if found)
2. If walk stopped at a `.git` directory without finding `.aide.yaml`, create `.aide.yaml` in that git root
3. If walk reached filesystem root without finding either, create `.aide.yaml` in cwd

### Mutation path

Two distinct code paths depending on scope:

| Scope | Read from | Mutate | Write to |
|-------|-----------|--------|----------|
| project (default) | `.aide.yaml` | `ProjectOverride` struct | `.aide.yaml` |
| `--global` | `config.yaml` | `Context` struct | `config.yaml` |

The structs differ (`ProjectOverride` vs `Context`) but the overlapping fields are compatible. The `--global` path is the existing code path; the project path is new.

### Resolve function changes

The existing `resolveContextForMutation(contextName string)` in `cmd/aide/commands.go` returns `(*config.Config, string, config.Context, error)`. Introduce a parallel function in the same file for the project path:

```go
func resolveProjectOverrideForMutation() (*config.Config, *ProjectOverride, string, error)
```

Returns:
- `*config.Config` — the loaded global config (needed for capability validation against the registry)
- `*ProjectOverride` — the loaded project override (empty `ProjectOverride{}` if `.aide.yaml` doesn't exist)
- `string` — the file path to write `.aide.yaml` back to
- `error`

Each affected command checks the `--global` flag:
- `--global`: call `resolveContextForMutation(contextName)` + `WriteConfig()` (existing path)
- default: call `resolveProjectOverrideForMutation()` + `WriteProjectOverride()` (new path)

### Flag interactions

**`--context` without `--global`:** Error with message "the --context flag requires --global" and exit. Project-level mutations don't target a named context — they apply to `.aide.yaml` which overlays on top of whichever context resolves for this project.

**`--global` without `--context`:** Valid. Uses `resolveContextForMutation("")` which resolves the best-matching context for the current directory, same as today.

### CLI surface

```
aide cap enable k8s,terraform             # → .aide.yaml
aide cap enable --global k8s,terraform    # → ~/.config/aide/config.yaml

aide sandbox deny /etc/secrets            # → .aide.yaml
aide sandbox deny --global /etc/secrets   # → ~/.config/aide/config.yaml

aide env set FOO=bar                      # → .aide.yaml
aide env set --global FOO=bar             # → ~/.config/aide/config.yaml
```

Help text for each affected command updated to indicate project-level default:

```
Usage:
  aide cap enable <capability>[,capability...] [flags]

Flags:
      --global    Apply to user-level config instead of project
      --context   Target context name (requires --global)
```

### Example `.aide.yaml` after mutations

```yaml
capabilities:
  - k8s
  - terraform
disabled_capabilities:
  - docker
env:
  KUBECONFIG: /path/to/kubeconfig
sandbox:
  guards_extra:
    - ssh-keys
  denied_extra:
    - /etc/passwd
```

---

### Layering precedence

Project override always wins over global context at resolution time. If a user runs `aide cap enable --global k8s` but the project's `.aide.yaml` has `disabled_capabilities: [k8s]`, the capability is suppressed for that project. This is intentional — the project-level config is the closest scope and takes precedence, matching the `git config` mental model where local overrides global.

---

## Out of scope

- Adding `--global` to always-global commands
- Project-level context creation (contexts live in global config)
- Merge semantics for single-value override fields (agent, secret, yolo, mcp_servers remain replace-if-set)
