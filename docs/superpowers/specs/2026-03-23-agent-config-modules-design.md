# Agent Config Dir Modules

**Date:** 2026-03-23
**Status:** Draft
**Supersedes:** 2026-03-21-agent-config-dir-resolver-design.md (resolver approach)

## Problem

The sandbox blocks writes to agent config directories when env var overrides are used. Specifically, setting `CLAUDE_CONFIG_DIR=/Users/subramk/.claude-work` in aide's config `env:` block causes Claude to hang because the sandbox doesn't grant write access to that path.

Root causes:
1. `PolicyFromConfig` passes `nil` for env — `policy.Env` is never populated in the config-based launcher path, so modules and guards can't see env vars like `CLAUDE_CONFIG_DIR`
2. The Claude agent module hardcodes paths (`~/.claude`, `~/.config/claude`, etc.) and never checks `CLAUDE_CONFIG_DIR`
3. The `ResolveAgentConfigDirs` resolver was built but never wired into the sandbox — its result is discarded in passthrough (`_ = ResolveAgentConfigDirs(...)`) and never called in the config-based path
4. Only Claude has an agent module — other agents (Codex, Goose, Amp, Aider, Gemini) have no module at all

The previous spec (2026-03-21) proposed adding resolver results to `policy.Writable`, but the codebase evolved to a guard-based system where guards and modules control all path access. `policy.Writable` doesn't exist on the runtime `Policy` struct and config-level `SandboxPolicy.Writable` is explicitly not used in guard-based profile generation.

## Decision

Agent config dir knowledge flows through the guard/module system. Modules are the single source of truth for agent config directories, env var overrides, and default paths. The standalone resolver registry is removed.

**Why:** The module system already has the right abstraction (`ctx.EnvLookup`, per-agent registration, seatbelt rule generation). A separate resolver duplicates knowledge and creates a maintenance burden. Modules can generate precise seatbelt rules (file-read*, file-write*) rather than broad writable grants.

## Design

### 1. Fix `policy.Env` propagation

In `internal/launcher/launcher.go`, `PolicyFromConfig` (line 197) internally calls `DefaultPolicy(..., nil)`, so `policy.Env` is always nil. After `PolicyFromConfig` returns and before `policy.AgentModule` is set (~line 203), add:

```go
// launcher.go, inside the !sbDisabled block, after PolicyFromConfig (line 197):
policy, pw, err := sandbox.PolicyFromConfig(sandboxCfg, projectRoot, rtDir.Path(), homeDir, tempDir)
// ... error handling ...
if policy != nil {
    policy.Env = env  // propagate merged env so modules see CLAUDE_CONFIG_DIR etc.
    // 12b. Set agent module for sandbox profile
    policy.AgentModule = ResolveAgentModule(agentName)
    // ... rest of sandbox apply ...
}
```

This ensures modules see env vars from both the shell environment and aide's config `env:` block.

Note: the passthrough path (`passthrough.go` line 135) already passes `os.Environ()` to `DefaultPolicy`, so `policy.Env` is correctly populated there. No change needed.

### 2. Shared helper: `ExistsOrUnderHome`

New function in `pkg/seatbelt/path_helpers.go`:

```go
// ExistsOrUnderHome returns true if path exists on disk, or if it's
// under homeDir. Agents create config dirs on first run, so paths
// under home must be writable even before they exist.
//
// Note: this is stricter than the previous defaultDirs helper which used
// strings.HasPrefix(p, homeDir) — that would incorrectly match
// /Users/subramkfoo when homeDir is /Users/subramk. The trailing
// separator check is an intentional correctness fix.
func ExistsOrUnderHome(homeDir, path string) bool {
    if _, err := os.Lstat(path); err == nil {
        return true
    }
    return strings.HasPrefix(path, homeDir+string(filepath.Separator))
}
```

### 3. Shared helper: `configDirRules`

New function in `pkg/seatbelt/modules/helpers.go`:

```go
// configDirRules generates file-read* file-write* Grant rules for
// agent config directories. Each dir gets a subpath rule.
func configDirRules(sectionName string, dirs []string) []seatbelt.Rule {
    if len(dirs) == 0 {
        return nil
    }
    rules := []seatbelt.Rule{
        seatbelt.SectionGrant(sectionName + " config"),
    }
    for _, dir := range dirs {
        rules = append(rules, seatbelt.GrantRule(fmt.Sprintf(
            `(allow file-read* file-write* (subpath %q))`, dir,
        )))
    }
    return rules
}
```

### 4. Resolve config dirs from context

New function in `pkg/seatbelt/modules/helpers.go`:

```go
// resolveConfigDirs returns directories for an agent given an env var
// override key and a list of default candidates. When the env var is
// set, only that path is returned (explicit override). Otherwise,
// candidates that exist or are under homeDir are returned.
//
// Empty env var semantics: ctx.EnvLookup returns ("", true) for KEY=,
// but we treat empty as unset (fall through to defaults). This matches
// the previous resolver behavior where KEY= was treated as unset.
func resolveConfigDirs(ctx *seatbelt.Context, envKey string, candidates []string) []string {
    if envKey != "" {
        if dir, ok := ctx.EnvLookup(envKey); ok && dir != "" {
            return []string{dir}
        }
    }
    var dirs []string
    for _, p := range candidates {
        if seatbelt.ExistsOrUnderHome(ctx.HomeDir, p) {
            dirs = append(dirs, p)
        }
    }
    return dirs
}
```

### 5. Update Claude module

`pkg/seatbelt/modules/claude.go` — add `CLAUDE_CONFIG_DIR` support:

```go
func (m *claudeAgentModule) Rules(ctx *seatbelt.Context) []seatbelt.Rule {
    home := ctx.HomeDir

    // Resolve config dirs (env override or defaults)
    configDirs := resolveConfigDirs(ctx, "CLAUDE_CONFIG_DIR", []string{
        filepath.Join(home, ".claude"),
        filepath.Join(home, ".config", "claude"),
        filepath.Join(home, "Library", "Application Support", "Claude"),
    })

    rules := configDirRules("Claude", configDirs)

    // Additional Claude-specific paths (not affected by CLAUDE_CONFIG_DIR)
    rules = append(rules,
        seatbelt.SectionGrant("Claude user data"),
        seatbelt.GrantRule(`(allow file-read* file-write*
    `+seatbelt.HomePrefix(home, ".local/bin/claude")+`
    `+seatbelt.HomeSubpath(home, ".cache/claude")+`
    `+seatbelt.HomePrefix(home, ".claude.json")+`
    `+seatbelt.HomeLiteral(home, ".claude.lock")+`
    `+seatbelt.HomeSubpath(home, ".local/state/claude")+`
    `+seatbelt.HomeSubpath(home, ".local/share/claude")+`
    `+seatbelt.HomeLiteral(home, ".mcp.json")+`
)`),

        // Claude managed configuration (read-only)
        seatbelt.SectionGrant("Claude managed configuration"),
        seatbelt.GrantRule(`(allow file-read*
    `+seatbelt.HomePrefix(home, ".claude.json.")+`
    `+seatbelt.HomeLiteral(home, "Library/Application Support/Claude/claude_desktop_config.json")+`
    (subpath "/Library/Application Support/ClaudeCode/.claude")
    (literal "/Library/Application Support/ClaudeCode/managed-settings.json")
    (literal "/Library/Application Support/ClaudeCode/managed-mcp.json")
    (literal "/Library/Application Support/ClaudeCode/CLAUDE.md")
)`),
    )

    return rules
}
```

Note: when `CLAUDE_CONFIG_DIR` is set, the config dir rules grant access to only that path. The "Claude user data" section still grants access to `.cache/claude`, `.local/state/claude`, etc. — these are runtime data paths, not config dirs, and aren't affected by the override.

### 6. New agent modules

Each follows the same pattern using `resolveConfigDirs` + `configDirRules`:

**Codex** (`pkg/seatbelt/modules/codex.go`):
- Env: `CODEX_HOME`
- Defaults: `~/.codex`

**Aider** (`pkg/seatbelt/modules/aider.go`):
- Env: none
- Defaults: `~/.aider`

**Goose** (`pkg/seatbelt/modules/goose.go`):
- Env: `GOOSE_PATH_ROOT`
- Defaults: `~/.config/goose`, `~/.local/share/goose`, `~/.local/state/goose`

**Amp** (`pkg/seatbelt/modules/amp.go`):
- Env: `AMP_HOME`
- Defaults: `~/.amp`, `~/.config/amp`

**Gemini** (`pkg/seatbelt/modules/gemini.go`):
- Env: `GEMINI_HOME`
- Defaults: `~/.gemini`

### 7. Register modules

In `internal/launcher/agentcfg.go`, expand `agentModuleResolvers`:

```go
var agentModuleResolvers = map[string]func() seatbelt.Module{
    "claude": modules.ClaudeAgent,
    "codex":  modules.CodexAgent,
    "aider":  modules.AiderAgent,
    "goose":  modules.GooseAgent,
    "amp":    modules.AmpAgent,
    "gemini": modules.GeminiAgent,
}
```

### 8. Delete resolver registry

Remove from `internal/launcher/agentcfg.go`:
- `AgentConfigResolver` type
- `agentConfigResolvers` map
- `ResolveAgentConfigDirs` function
- All per-agent resolver functions (`claudeConfigDirs`, `codexConfigDirs`, `aiderConfigDirs`, `gooseConfigDirs`, `ampConfigDirs`, `geminiConfigDirs`)
- `envLookup` helper (replaced by `ctx.EnvLookup`)
- `defaultDirs` helper (replaced by `resolveConfigDirs` + `ExistsOrUnderHome`)

### 9. Clean up passthrough.go

Remove the dead code at line 137-140:

```go
// DELETE:
// Agent config dirs are now handled by the agent module in the seatbelt profile.
// For completeness, resolve them but they are encoded in the module itself.
homeDir, _ := os.UserHomeDir()
_ = ResolveAgentConfigDirs(name, os.Environ(), homeDir)
```

The module already handles this via `policy.AgentModule = ResolveAgentModule(name)` at line 136.

### 10. Passthrough env propagation

In `passthrough.go`, `DefaultPolicy` already receives `os.Environ()` as the env parameter (line 135), so `policy.Env` is correctly populated in the passthrough path. No change needed here.

## Testing

### Unit Tests

| Test | File | Description |
|------|------|-------------|
| `TestExistsOrUnderHome_Exists` | `pkg/seatbelt/path_helpers_test.go` | Existing path returns true |
| `TestExistsOrUnderHome_UnderHome` | `pkg/seatbelt/path_helpers_test.go` | Non-existent path under home returns true |
| `TestExistsOrUnderHome_OutsideHome` | `pkg/seatbelt/path_helpers_test.go` | Non-existent path outside home returns false |
| `TestResolveConfigDirs_EnvOverride` | `pkg/seatbelt/modules/helpers_test.go` | Env var set → only that path returned |
| `TestResolveConfigDirs_EmptyEnv` | `pkg/seatbelt/modules/helpers_test.go` | Env var empty → falls through to defaults |
| `TestResolveConfigDirs_NoEnvKey` | `pkg/seatbelt/modules/helpers_test.go` | Empty envKey → defaults only |
| `TestConfigDirRules_Empty` | `pkg/seatbelt/modules/helpers_test.go` | No dirs → nil rules |
| `TestConfigDirRules_Multiple` | `pkg/seatbelt/modules/helpers_test.go` | Multiple dirs → section + rules |
| `TestClaudeModule_EnvOverride` | `pkg/seatbelt/modules/claude_test.go` | `CLAUDE_CONFIG_DIR` set → custom path in rules |
| `TestClaudeModule_Defaults` | `pkg/seatbelt/modules/claude_test.go` | No env → default paths in rules |
| `TestClaudeModule_NonConfigPaths` | `pkg/seatbelt/modules/claude_test.go` | `.cache/claude`, `.local/state/claude` always present regardless of env |
| `TestCodexModule_EnvOverride` | `pkg/seatbelt/modules/codex_test.go` | `CODEX_HOME` override |
| `TestCodexModule_Defaults` | `pkg/seatbelt/modules/codex_test.go` | Default `~/.codex` |
| `TestGooseModule_EnvOverride` | `pkg/seatbelt/modules/goose_test.go` | `GOOSE_PATH_ROOT` override |
| `TestGooseModule_Defaults` | `pkg/seatbelt/modules/goose_test.go` | Three XDG dirs |
| `TestAmpModule_EnvOverride` | `pkg/seatbelt/modules/amp_test.go` | `AMP_HOME` override |
| `TestAiderModule_Defaults` | `pkg/seatbelt/modules/aider_test.go` | No env var, just `~/.aider` |
| `TestGeminiModule_EnvOverride` | `pkg/seatbelt/modules/gemini_test.go` | `GEMINI_HOME` override |

### Integration Tests

| Test | File | Description |
|------|------|-------------|
| `TestLauncher_CustomClaudeConfigDir` | `internal/launcher/launcher_test.go` | `CLAUDE_CONFIG_DIR` in config env → sandbox profile contains custom path |
| `TestLauncher_PolicyEnvPropagation` | `internal/launcher/launcher_test.go` | Merged env reaches `policy.Env` |
| `TestPassthrough_NoDeadResolverCall` | `internal/launcher/passthrough_test.go` | Verify clean compilation after removing dead code |
| `TestSeatbeltProfile_CustomConfigDir` | `internal/sandbox/darwin_test.go` | Full profile render with `CLAUDE_CONFIG_DIR` in `policy.Env` → custom path appears as `(subpath ...)` in `.sb` output |

## Files Changed

| File | Change |
|------|--------|
| `pkg/seatbelt/path_helpers.go` | **New**: `ExistsOrUnderHome` helper |
| `pkg/seatbelt/path_helpers_test.go` | **New**: tests for `ExistsOrUnderHome` |
| `pkg/seatbelt/modules/helpers.go` | **New**: `configDirRules`, `resolveConfigDirs` |
| `pkg/seatbelt/modules/helpers_test.go` | **New**: tests for shared helpers |
| `pkg/seatbelt/modules/claude.go` | **Modified**: add `CLAUDE_CONFIG_DIR` env var support via `resolveConfigDirs` |
| `pkg/seatbelt/modules/claude_test.go` | **Modified**: add env override tests |
| `pkg/seatbelt/modules/codex.go` | **New**: Codex agent module |
| `pkg/seatbelt/modules/codex_test.go` | **New**: tests |
| `pkg/seatbelt/modules/aider.go` | **New**: Aider agent module |
| `pkg/seatbelt/modules/aider_test.go` | **New**: tests |
| `pkg/seatbelt/modules/goose.go` | **New**: Goose agent module |
| `pkg/seatbelt/modules/goose_test.go` | **New**: tests |
| `pkg/seatbelt/modules/amp.go` | **New**: Amp agent module |
| `pkg/seatbelt/modules/amp_test.go` | **New**: tests |
| `pkg/seatbelt/modules/gemini.go` | **New**: Gemini agent module |
| `pkg/seatbelt/modules/gemini_test.go` | **New**: tests |
| `internal/launcher/agentcfg.go` | **Modified**: delete resolver registry, expand `agentModuleResolvers` |
| `internal/launcher/agentcfg_test.go` | **Modified**: rewrite tests to target modules |
| `internal/launcher/launcher.go` | **Modified**: set `policy.Env = env` before sandbox apply |
| `internal/launcher/passthrough.go` | **Modified**: remove dead `ResolveAgentConfigDirs` call |
| `internal/launcher/launcher_test.go` | **Modified**: add integration test for env propagation |

## Extensibility

Adding a new agent requires:
1. One module file (~25 lines) in `pkg/seatbelt/modules/`
2. One entry in `agentModuleResolvers` map in `internal/launcher/agentcfg.go`

No changes to sandbox, policy, guard, or launcher integration code.
