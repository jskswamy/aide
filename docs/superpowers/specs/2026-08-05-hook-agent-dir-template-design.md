# Hook `{agent_dir}` Template and Sync Resilience

**Date:** 2026-08-05
**Status:** Approved

## Problem

Three related problems surface when running `aide sync` across multiple Claude
profiles (e.g. default, a work profile, a client profile):

**Hook path mismatch.** Config declares hooks with literal `~/.claude/hooks/…`
paths. For profile-based contexts, `CLAUDE_CONFIG_DIR` is set to a
profile-specific directory (e.g. `~/.claude-work/`). The desired path
(`~/.claude/hooks/foo`) doesn't match the actually-installed path
(`/Users/name/.claude-work/hooks/foo`), so every sync generates spurious
install ops and shows the already-installed hooks as unmanaged.

**Path representation inconsistency.** Hook commands reach aide from two
sources — config.yaml (user-written, may use `~/` or `$HOME/`) and
`settings.json` (may be absolute or tilde-form depending on how the hook was
added). Without normalization, identical paths compare as unequal.

**Sync failures block state cleanup.** Two error patterns prevent
`updateStateAfterSync` from running, leaving stale managed-state entries that
trigger the same errors on the next sync:
- A plugin managed at user scope but installed at project scope causes
  `claude plugin uninstall` to fail with "enabled at project scope".
- A marketplace plugin installed from a stale local index causes
  `claude plugin install` to fail with "local copy may be out of date".

## Goals

- Support `{agent_dir}` as a template variable in hook commands, resolving to
  the agent's config directory per context.
- Normalize `~/` and `$HOME/` in hook commands to absolute paths on both the
  desired and installed sides so comparison is form-independent.
- `aide adopt` rewrites adopted hook commands using `{agent_dir}` when the
  path is under the current agent's config directory.
- Project-scope plugin uninstall no longer hard-fails sync.
- Stale marketplace triggers an auto-update-and-retry instead of a hard fail.

## Non-Goals

- Managing or copying hook scripts between profile directories — aide manages
  config, not script deployment.
- Expanding other shell variables (`$USER`, `$XDG_*`, etc.) — only `~/` and
  `$HOME/` are normalized.
- Auto-migrating existing literal `~/.claude/…` paths in config.yaml —
  migration is manual or via `aide adopt`.

---

## Design

### 1. Path normalization (`ExpandPath`)

Add `ExpandPath(s, homeDir string) string` to `internal/provision`:

```go
// ExpandPath converts ~/... and $HOME/... to absolute paths using homeDir.
// All other paths are returned unchanged.
func ExpandPath(s, homeDir string) string {
    switch {
    case strings.HasPrefix(s, "~/"):
        return filepath.Join(homeDir, s[2:])
    case strings.HasPrefix(s, "$HOME/"):
        return filepath.Join(homeDir, s[6:])
    }
    return s
}
```

Applied on the **desired side** inside `substituteHookVars` (first step, before
variable substitution) and on the **installed side** in `claude.ReadHooks`
(on each returned `cmd` using `ctx.HomeDir`).

After normalization, all hook command strings inside aide are absolute paths,
regardless of how they were written.

### 2. `{agent_dir}` template variable

#### Optional interface

```go
// AgentDirProvider is implemented by drivers that have a per-context
// config directory distinct from the default agent home.
type AgentDirProvider interface {
    AgentDir(ctx Context) string
}

// ResolveAgentDir returns the agent's config directory for ctx,
// or "" if the driver does not implement AgentDirProvider.
func ResolveAgentDir(prov Provisioner, ctx Context) string {
    if p, ok := prov.(AgentDirProvider); ok {
        return p.AgentDir(ctx)
    }
    return ""
}
```

#### Claude Driver implementation

```go
func (d *Driver) AgentDir(ctx provision.Context) string {
    if dir, ok := ctx.Env["CLAUDE_CONFIG_DIR"]; ok && dir != "" {
        return dir
    }
    return filepath.Join(ctx.HomeDir, ".claude")
}
```

`CLAUDE_CONFIG_DIR` is set by `InjectProfileEnv` for profile-based contexts
and is always an absolute path. The fallback covers the default context.

#### `substituteHookVars` signature change

```go
func substituteHookVars(cmd, agentName, agentDir, homeDir string) string {
    cmd = ExpandPath(cmd, homeDir)          // normalize ~/  and $HOME/
    cmd = strings.ReplaceAll(cmd, "{agent}", agentName)
    if agentDir != "" {
        cmd = strings.ReplaceAll(cmd, "{agent_dir}", agentDir)
    }
    return cmd
}
```

#### `ResolveDesired` signature change

```go
func ResolveDesired(cfg *config.Config, contextName, agentDir, homeDir string) (Desired, error)
```

Callers compute both values before calling:

```go
agentDir := provision.ResolveAgentDir(env.prov, env.provCtx)
homeDir  := env.provCtx.HomeDir
desired, err := provision.ResolveDesired(env.cfg, env.contextName, agentDir, homeDir)
```

Both `runSync` and `runAdopt` are updated. Test callers pass `""` for both new params
(no expansion, acceptable for unit tests that don't exercise profile paths).

### 3. `aide adopt` auto-replace

When the user adopts a hook, adopt checks whether the resolved (tilde-expanded)
command path starts with `agentDir + "/"`. If so, the prefix is replaced with
`{agent_dir}` before writing to config.yaml.

```go
// in the hook adoption loop of runAdopt:
cmd := h.Command  // already tilde-expanded by ReadHooks
if agentDir != "" && strings.HasPrefix(cmd, agentDir+"/") {
    cmd = "{agent_dir}" + cmd[len(agentDir):]
}
env.cfg.Hooks[h.Event] = append(existing, config.HookEntry{
    Name:    hookCommandBasename(h.Command),
    Matcher: h.Matcher,
    Command: cmd,
    Timeout: h.Timeout,
})
```

Commands that don't start with the agent dir (e.g. `/usr/local/bin/my-hook`,
`rtk hook claude`) are written literally, unchanged.

### 4. HookKey and managed state

`HookKey(event, matcher, command)` is unchanged. Because both sides are now
normalized to absolute paths before any HookKey is computed, comparisons are
correct without modifying the key function.

Managed state records the **resolved** command (absolute, with `{agent_dir}`
substituted). This means managed state is per-context and profile-specific,
which is correct — the default context manages `/Users/name/.claude/hooks/foo`
and the work context independently manages `/Users/name/.claude-work/hooks/foo`.

`updateStateAfterSync` (hooks section) writes `desired.Hooks` (already resolved)
to `cs.Hooks` — no change needed.

### 5. Sync resilience

#### 5a. Project-scope plugin uninstall

`claude.UninstallPlugin` adds `"project scope"` to the tolerate list:

```go
func (d *Driver) UninstallPlugin(pctx provision.Context, name string) error {
    return provision.RunCLI(context.Background(), d.runner, pctx.Env,
        "claude plugin uninstall "+name,
        "claude", []string{"plugin", "uninstall", name},
        append(provision.DefaultTolerateStderr, "project scope")...)
}
```

When `claude plugin uninstall` fails because the plugin is project-scoped,
`RunCLI` returns nil. `provision.Apply` continues, `updateStateAfterSync` runs,
and the entry is removed from managed state. Future syncs are clean.

The plugin remains in the agent at project scope — aide simply stops tracking it
at user scope.

#### 5b. Stale marketplace auto-update

`claude.InstallPlugin` detects the "out of date" condition and retries:

```go
func (d *Driver) InstallPlugin(pctx provision.Context, p provision.Plugin) error {
    err := provision.RunCLI(context.Background(), d.runner, pctx.Env,
        "claude plugin install "+p.Name,
        "claude", []string{"plugin", "install", p.Name})
    if err == nil {
        return nil
    }
    // If the marketplace index is stale, update it and retry once.
    if strings.Contains(err.Error(), "may be out of date") {
        marketplace := marketplaceFromPluginName(p.Name)
        if marketplace != "" {
            // Tolerate all errors from the update (marketplace may not exist).
            _, _, _, _ = d.runner.Run(context.Background(), pctx.Env,
                "claude", "plugin", "marketplace", "update", marketplace)
            return provision.RunCLI(context.Background(), d.runner, pctx.Env,
                "claude plugin install "+p.Name,
                "claude", []string{"plugin", "install", p.Name})
        }
    }
    return err
}

// marketplaceFromPluginName extracts the marketplace from "plugin@marketplace".
func marketplaceFromPluginName(name string) string {
    if i := strings.LastIndexByte(name, '@'); i > 0 {
        return name[i+1:]
    }
    return ""
}
```

---

## Migration story

### Existing configs with literal `~/.claude/…` paths

**No immediate breakage.** After normalization, `~/.claude/hooks/foo` expands
to `/Users/name/.claude/hooks/foo` on the desired side. For the default
context, the installed path is also `/Users/name/.claude/hooks/foo` — they
match. No ops generated. ✅

**Profile contexts** still get install ops for the tilde-literal form because
the desired path points to the wrong profile dir. The fix for profile contexts
is to migrate to `{agent_dir}`.

### Migration paths

**Option A — via `aide adopt`:**
1. Run `aide adopt --context <profile>` for each profile context.
2. Adopt discovers the correctly-installed hooks (already in the profile's
   settings.json) and writes them as `{agent_dir}/hooks/…` in config.yaml.
3. Re-run `aide adopt` for the default context to mark those as managed too.
4. Future syncs are clean for all contexts.

**Option B — manual edit:**
1. In config.yaml, replace `~/.claude/hooks/` with `{agent_dir}/hooks/` in
   each hook command.
2. Run `aide sync` for each context. For the default context: desired resolves
   to `/Users/name/.claude/hooks/foo`, installed is the same → no-op. For
   profile contexts: desired resolves to the profile dir → install op if not
   already installed.

### Profile-specific hooks (not shared across contexts)

Hooks that only make sense in one profile should be declared under
`contexts.<name>.hooks.extra` (already supported), not in the top-level
`hooks:` map. This prevents aide from trying to install them in contexts where
the backing script doesn't exist.

---

## Affected files

| File | Change |
|------|--------|
| `internal/provision/desired.go` | Add `ExpandPath`; change `substituteHookVars` and `ResolveDesired` signatures |
| `internal/provision/provisioner.go` | Add `AgentDirProvider` interface and `ResolveAgentDir` helper |
| `internal/provision/agents/claude/claude.go` | `AgentDir` method; `UninstallPlugin` tolerate "project scope"; `InstallPlugin` auto-update retry |
| `internal/provision/agents/claude/hooks.go` | `ReadHooks` applies `ExpandPath` on each returned command |
| `cmd/aide/sync.go` | Compute `agentDir`, `homeDir`; pass to `ResolveDesired` |
| `cmd/aide/adopt.go` | Compute `agentDir`; auto-replace in adopted hook commands |
| `internal/provision/desired_test.go` | Tests for `ExpandPath`, `{agent_dir}` substitution, cross-profile resolution |
| `internal/provision/agents/claude/claude_test.go` | Tests for project-scope toleration, marketplace retry |
