# Agent Config Dir Resolver

**Date:** 2026-03-21
**Status:** Approved

## Problem

The sandbox's `extraWritablePaths` hardcodes Claude-specific config directories (`~/.claude`, `~/.config/claude`, `~/Library/Application Support/Claude`). When a user sets `CLAUDE_CONFIG_DIR` to a custom path, that path isn't in the sandbox's writable list, causing Claude to hang on launch.

More broadly, agent-specific config directory knowledge is baked into generic sandbox code. Different agents have different config locations and different env vars that override them.

## Design

### Agent Config Resolver Registry

A new file `internal/launcher/agentcfg.go` introduces:

```go
// AgentConfigResolver returns directories an agent needs write access to,
// given the process environment. Returns nil if no special dirs are needed.
type AgentConfigResolver func(env []string, homeDir string) []string
```

A package-level registry maps agent base names to resolvers:

```go
var agentConfigResolvers = map[string]AgentConfigResolver{
    "claude": claudeConfigDirs,
    "codex":  codexConfigDirs,
    "aider":  aiderConfigDirs,
    "goose":  gooseConfigDirs,
    "amp":    ampConfigDirs,
}
```

### Claude Resolver

**Env var:** `CLAUDE_CONFIG_DIR`
**Defaults:** `~/.claude`, `~/.config/claude`, `~/Library/Application Support/Claude`

```go
func claudeConfigDirs(env []string, homeDir string) []string {
    if dir := envLookup(env, "CLAUDE_CONFIG_DIR"); dir != "" {
        return []string{dir}
    }
    return defaultDirs(homeDir,
        filepath.Join(homeDir, ".claude"),
        filepath.Join(homeDir, ".config", "claude"),
        filepath.Join(homeDir, "Library", "Application Support", "Claude"),
    )
}
```

Key behavior: when the env var is set (even to a path that doesn't exist yet), return **only** that path. The env var is an explicit override.

### Codex Resolver

**Env var:** `CODEX_HOME`
**Default:** `~/.codex`

```go
func codexConfigDirs(env []string, homeDir string) []string {
    if dir := envLookup(env, "CODEX_HOME"); dir != "" {
        return []string{dir}
    }
    return defaultDirs(homeDir, filepath.Join(homeDir, ".codex"))
}
```

### Aider Resolver

**Env var:** None (aider uses `AIDER_*` per-option vars, no single config dir override)
**Default:** `~/.aider`

```go
func aiderConfigDirs(env []string, homeDir string) []string {
    return defaultDirs(homeDir, filepath.Join(homeDir, ".aider"))
}
```

### Goose Resolver

**Env var:** `GOOSE_PATH_ROOT` (creates `config/`, `data/`, `state/` subdirs)
**Defaults:** `~/.config/goose`, `~/.local/share/goose`, `~/.local/state/goose`

```go
func gooseConfigDirs(env []string, homeDir string) []string {
    if dir := envLookup(env, "GOOSE_PATH_ROOT"); dir != "" {
        return []string{dir}
    }
    return defaultDirs(homeDir,
        filepath.Join(homeDir, ".config", "goose"),
        filepath.Join(homeDir, ".local", "share", "goose"),
        filepath.Join(homeDir, ".local", "state", "goose"),
    )
}
```

### Amp Resolver

**Env var:** `AMP_HOME`
**Defaults:** `~/.amp`, `~/.config/amp`

```go
func ampConfigDirs(env []string, homeDir string) []string {
    if dir := envLookup(env, "AMP_HOME"); dir != "" {
        return []string{dir}
    }
    return defaultDirs(homeDir,
        filepath.Join(homeDir, ".amp"),
        filepath.Join(homeDir, ".config", "amp"),
    )
}
```

### Helpers

```go
// envLookup finds a key in a KEY=VALUE slice.
// Returns "" for missing keys. Treats explicitly empty (KEY=) as unset.
func envLookup(env []string, key string) string {
    prefix := key + "="
    for _, e := range env {
        if strings.HasPrefix(e, prefix) {
            val := e[len(prefix):]
            if val == "" {
                return "" // treat KEY= as unset
            }
            return val
        }
    }
    return ""
}

// defaultDirs returns candidates that exist on disk, plus any that
// don't exist yet but are under homeDir (agents create these on
// first run, so they must be writable from the start).
func defaultDirs(homeDir string, candidates ...string) []string {
    var dirs []string
    for _, p := range candidates {
        if _, err := os.Lstat(p); err == nil {
            dirs = append(dirs, p)
        } else if strings.HasPrefix(p, homeDir) {
            // Include non-existent dirs under home — agents create
            // these on first run and need write access from the start.
            dirs = append(dirs, p)
        }
    }
    return dirs
}
```

### Public API

```go
// ResolveAgentConfigDirs returns directories the named agent needs
// write access to, based on the process environment.
// Returns nil for unknown agents.
func ResolveAgentConfigDirs(agentName string, env []string, homeDir string) []string {
    base := filepath.Base(agentName)
    if resolver, ok := agentConfigResolvers[base]; ok {
        return resolver(env, homeDir)
    }
    return nil
}
```

### Integration Points

**`Launcher.Launch()`** (config-based path):

The resolver must be called with the **final merged env** (after step 10 in Launch), so it sees any `CLAUDE_CONFIG_DIR` set via aide's config env vars. The `homeDir` variable must be hoisted above the sandbox block to be in scope. The call goes after env is built but before `sb.Apply()`:

```go
homeDir, _ := os.UserHomeDir()  // hoisted above sandbox block

// ... build env (step 10) ...

// 11b. Resolve agent-specific writable dirs
agentDirs := ResolveAgentConfigDirs(agentName, env, homeDir)

// 12. Apply sandbox
if !sbDisabled {
    policy.Writable = append(policy.Writable, agentDirs...)
    // ... existing sandbox apply logic ...
}
```

**`Launcher.execAgent()`** (passthrough path):

In passthrough, `cmd.Env` is `os.Environ()` — the raw process env. This is correct since there's no config-based env merging in passthrough mode.

```go
policy := sandbox.DefaultPolicy(projectRoot, rtDir.Path(), homeDir, tempDir)
agentDirs := ResolveAgentConfigDirs(name, cmd.Env, homeDir)
policy.Writable = append(policy.Writable, agentDirs...)
```

### Remove `extraWritablePaths`

The existing `extraWritablePaths` function in `sandbox.go` is removed. Its logic moves into the agent resolvers. `DefaultPolicy` no longer calls it — the launcher is responsible for adding agent-specific paths.

The `DefaultPolicy` function signature (`projectRoot, runtimeDir, homeDir, tempDir string`) stays unchanged since `homeDir` is still needed for the `Denied` paths list.

### CleanEnv Interaction

`CLAUDE_CONFIG_DIR` (and similar agent env vars) is not in the `filterEssentialEnv` allowlist. When `CleanEnv: true`, these vars are stripped from the process env. However, the resolver reads from the **final merged env** (which includes config-defined env vars). If a user sets `CLAUDE_CONFIG_DIR` in their aide config's `env:` block, it survives cleanEnv. If it's only in their shell env, it does not — but this is the expected behavior of cleanEnv.

The resolver call happens before sandbox apply but reads from the already-built `env` slice, so it correctly reflects whatever survived the cleanEnv filter + config merge.

## Testing

| Test | Description |
|------|-------------|
| `TestClaudeConfigDirs_EnvOverride` | Set `CLAUDE_CONFIG_DIR=/custom/path` in env slice. Assert returns `["/custom/path"]` only. |
| `TestClaudeConfigDirs_DefaultFallback` | No env var, create `~/.claude` in temp dir. Assert returned. |
| `TestClaudeConfigDirs_NoExistingDirs` | No env var, no dirs exist. Assert default dirs still returned (first-run support). |
| `TestClaudeConfigDirs_EmptyEnvVar` | Set `CLAUDE_CONFIG_DIR=` (empty). Assert falls through to defaults. |
| `TestCodexConfigDirs_EnvOverride` | Set `CODEX_HOME=/custom`. Assert `["/custom"]`. |
| `TestCodexConfigDirs_Default` | No env var. Assert `~/.codex` returned. |
| `TestGooseConfigDirs_EnvOverride` | Set `GOOSE_PATH_ROOT=/custom`. Assert `["/custom"]`. |
| `TestGooseConfigDirs_Defaults` | No env var. Assert three XDG dirs returned. |
| `TestAmpConfigDirs_EnvOverride` | Set `AMP_HOME=/custom`. Assert `["/custom"]`. |
| `TestAiderConfigDirs` | No env var support. Assert `~/.aider` returned. |
| `TestResolveAgentConfigDirs_UnknownAgent` | Pass `"vim"`. Assert nil. |
| `TestResolveAgentConfigDirs_PathBasename` | Pass `"/usr/local/bin/claude"`. Assert resolver found by basename. |
| `TestEnvLookup` | Basic key lookup, missing key, empty value. |
| `TestDefaultDirs_NonExistentUnderHome` | Non-existent path under homeDir is still included. |
| `TestDefaultDirs_NonExistentOutsideHome` | Non-existent path outside homeDir is excluded. |
| `TestLauncher_CustomClaudeConfigDir` | Integration: `CLAUDE_CONFIG_DIR` in env, verify sandbox writable list includes the custom dir. |
| `TestLauncher_CleanEnvStripsShellConfigDir` | Integration: `CLAUDE_CONFIG_DIR` set in shell env only, `CleanEnv: true`. Verify resolver falls back to defaults since cleanEnv strips the var. |
| `TestLauncher_CleanEnvPreservesConfigEnvVar` | Integration: `CLAUDE_CONFIG_DIR` set via aide config `env:` block, `CleanEnv: true`. Verify custom dir is in writable list since config env survives cleanEnv. |

## Extensibility

Adding a new agent requires:
1. One resolver function (5-10 lines)
2. One entry in `agentConfigResolvers` map

No changes to sandbox, policy, or launcher integration code. The resolver pattern handles the full range of conventions: single env var override (Claude, Codex, Amp), root-based override (Goose), and no override (Aider).

## Files Changed

| File | Change |
|------|--------|
| `internal/launcher/agentcfg.go` | New: registry, all resolvers, helpers, `ResolveAgentConfigDirs` |
| `internal/launcher/agentcfg_test.go` | New: unit tests for all resolvers |
| `internal/launcher/launcher.go` | Hoist `homeDir`, call `ResolveAgentConfigDirs` before sandbox apply |
| `internal/launcher/passthrough.go` | Call `ResolveAgentConfigDirs` in `execAgent` |
| `internal/sandbox/sandbox.go` | Remove `extraWritablePaths`, remove its call from `DefaultPolicy` |
| `internal/launcher/launcher_test.go` | Integration test for custom config dir |
