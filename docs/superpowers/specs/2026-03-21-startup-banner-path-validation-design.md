# Startup Banner & Path Validation

**Date:** 2026-03-21
**Status:** Approved

## Problem

1. **No startup feedback** — aide launches the agent silently. Users don't see which context matched, what sandbox policy is in effect, or whether their config is working as expected. The `aide which --resolve` command exists but is a separate step.

2. **No path validation** — user-specified sandbox paths in config (writable_extra, denied_extra, etc.) are included in the sandbox policy unconditionally. If a path doesn't exist, it's silently added — potentially masking typos or stale config.

## Design

### Config: Preferences

Add a `Preferences` struct to `Config` and `ProjectOverride`:

```go
type Preferences struct {
    ShowInfo   *bool  `yaml:"show_info,omitempty"`    // default true
    InfoStyle  string `yaml:"info_style,omitempty"`   // compact|boxed|clean, default "compact"
    InfoDetail string `yaml:"info_detail,omitempty"`  // normal|detailed, default "normal"
}
```

```yaml
# config.yaml — global
preferences:
  show_info: true
  info_style: compact
  info_detail: normal

# .aide.yaml — project override (field-by-field merge, project wins)
preferences:
  info_detail: detailed
```

Added to `Config`:

```go
type Config struct {
    // ... existing fields ...
    Preferences *Preferences `yaml:"preferences,omitempty"`
}
```

Added to `ProjectOverride`:

```go
type ProjectOverride struct {
    // ... existing fields ...
    Preferences *Preferences `yaml:"preferences,omitempty"`
}
```

**Defaults:** `show_info: true`, `info_style: "compact"`, `info_detail: "normal"`.

**Merge rule:** project preferences override global field-by-field. Unset project fields inherit from global. The resolver merges preferences the same way it merges other override fields.

**Validation:** Invalid `info_style` or `info_detail` values produce a validation warning (not an error) in `aide validate`. At runtime, unknown `info_style` falls back to compact; unknown `info_detail` falls back to normal.

### Preferences Flow

Preferences need to flow from config through the resolver to the launcher:

1. `Config.Preferences` holds global preferences
2. `ProjectOverride.Preferences` holds project-level overrides
3. `ResolvedContext` gets a new `Preferences` field:

```go
type ResolvedContext struct {
    Name        string
    MatchReason string
    Context     config.Context
    Preferences config.Preferences  // resolved (global + project merged)
}
```

4. `Resolve()` calls `ResolvePreferences` with global config preferences (and nil project) to populate `ResolvedContext.Preferences` with defaults
5. `applyProjectOverride` calls `ResolvePreferences` again with project preferences to merge on top
6. `ResolvePreferences` helper applies defaults for any unset fields:

```go
func ResolvePreferences(global, project *Preferences) Preferences {
    result := Preferences{
        ShowInfo:   boolPtr(true),
        InfoStyle:  "compact",
        InfoDetail: "normal",
    }
    // Apply global fields (if non-zero), then project on top
    return result
}
```

Called in `Resolve()` during initial context creation:

```go
rc.Preferences = config.ResolvePreferences(cfg.Preferences, nil)
```

And in `applyProjectOverride()` when project override exists:

```go
rc.Preferences = config.ResolvePreferences(&rc.Preferences, po.Preferences)
```

### CLI: --resolve flag

Both `aide` (launch) and `aide which` accept `--resolve`:

```
aide                     # banner uses config info_detail (default: normal)
aide --resolve           # banner forced to detailed, then launches agent
aide which               # uses config info_detail
aide which --resolve     # forced to detailed
```

The `--resolve` flag overrides both `info_detail` (forces detailed) and `show_info` (forces true). So `--resolve` always shows a detailed banner even if `show_info: false`.

**Propagation:** The `--resolve` flag is a **persistent** `bool` on the root cobra command (using `PersistentFlags()` so subcommands like `aide which` inherit it). `Launcher.Launch()` gains a new `resolve bool` parameter:

```go
func (l *Launcher) Launch(cwd string, agentOverride string,
    extraArgs []string, cleanEnv bool, resolve bool) error
```

The launcher uses `resolve` to override the resolved preferences before building `BannerData`.

### Banner Renderer (`internal/ui/banner.go`)

A new package `internal/ui` with a pure renderer. No config loading or disk access — callers build the data struct and pass it in.

#### Data structs

```go
type BannerData struct {
    ContextName  string
    MatchReason  string
    AgentName    string
    AgentPath    string            // resolved binary path (always populated)
    SecretName   string
    SecretKeys   []string          // nil in normal mode, listed in detailed
    Env          map[string]string // env annotations (see below)
    EnvResolved  map[string]string // partially masked values, nil in normal mode
    Sandbox      *SandboxInfo
    Warnings     []string          // path validation warnings
}

type SandboxInfo struct {
    Disabled      bool
    Network       string
    Ports         string           // "all" or "443, 53"
    WritableCount int
    ReadableCount int
    Denied        []string         // always listed by name
    Writable      []string         // populated in detailed mode only
    Readable      []string         // populated in detailed mode only
}
```

**Env annotations:** `BannerData.Env` contains pre-parsed annotations, not raw template strings. The caller (launcher/whichCmd) parses `{{ .secrets.api_key }}` → `← secrets.api_key`, `literal-value` → `= literal-value`, `{{ .project_root }}` → `← project_root`. This reuses the existing `classifyEnvSource` logic from `commands.go`. The renderer just displays these annotations as-is.

**Redaction format:** `EnvResolved` values use partial masking — show the first 8 characters followed by `***`. Values 8 characters or shorter show the full value followed by `***`. This reuses the existing `redactValue` function from `commands.go`.

#### Render functions

```go
func RenderBanner(w io.Writer, style string, data *BannerData)
func RenderCompact(w io.Writer, data *BannerData)
func RenderBoxed(w io.Writer, data *BannerData)
func RenderClean(w io.Writer, data *BannerData)
```

`RenderBanner` dispatches to the style-specific renderer. Falls back to compact for unknown styles.

#### Visual styles

**Compact** (default):

```
🔧 aide · work (claude → /usr/local/bin/claude)
   📁 path glob match: ~/work/*
   🔐 secret: work (3 keys: api_key, org_id, token)
   📦 env: ANTHROPIC_API_KEY ← secrets.api_key
          ORG_ID = acme
   🛡️  sandbox: outbound
      denied: ~/.ssh/id_*, ~/.aws/credentials, ~/.config/gcloud
      writable: 3 paths · readable: 2 paths
      ⚠ skipped: ~/.kube (not found)
```

**Boxed**:

```
┌─ aide ──────────────────────────────────────────
│ 🎯 Context   work
│ 📁 Matched   path glob match: ~/work/*
│ 🤖 Agent     claude → /usr/local/bin/claude
│ 🔐 Secret    work (3 keys: api_key, org_id, token)
│ 📦 Env       ANTHROPIC_API_KEY ← secrets.api_key
│              ORG_ID = acme
│ 🛡️  Sandbox   outbound
│              denied: ~/.ssh/id_*, ~/.aws/credentials,
│                      ~/.config/gcloud
│              writable: 3 paths · readable: 2 paths
│              ⚠ skipped: ~/.kube (not found)
└──────────────────────────────────────────────────
```

**Clean**:

```
aide · context: work
  Agent     claude → /usr/local/bin/claude
  Matched   ~/work/* (path glob)
  Secret    work (api_key, org_id, token)
  Env       ANTHROPIC_API_KEY ← secrets.api_key
            ORG_ID = acme
  Sandbox   outbound
            denied: ~/.ssh/id_*, ~/.aws/credentials,
                    ~/.config/gcloud
            writable: 3 paths · readable: 2 paths
            ⚠ skipped: ~/.kube (not found)
```

#### ANSI colors

- **Bold green**: `aide` header, context name
- **Cyan**: labels (Agent, Matched, Secret, Env, Sandbox)
- **White**: values
- **Yellow**: warnings (⚠ lines)
- **Dim**: secondary info (path counts, arrows, annotations)

Uses `fatih/color` (existing dependency). Colors auto-disabled when output is not a terminal.

#### Detail levels

| Field | normal | detailed |
|-------|--------|----------|
| Agent binary path | name only | full resolved path |
| Secret keys | count | listed by name |
| Env values | source annotation | redacted resolved values |
| Sandbox writable/readable | counts | full path list |
| Sandbox denied | listed | listed |
| Path warnings | shown | shown |

### Path Validation

Validate user-specified sandbox paths at policy build time in `PolicyFromConfig`.

#### Scope

- **Validated:** user-specified paths from config — whichever list is actually used in each category (writable OR writable_extra, not both, since the if/else logic only processes one). Validation runs on the active branch.
- **Not validated:** default policy paths (computed, not user config)
- **Not validated:** glob patterns — these are deny patterns that intentionally may not match

#### Glob detection

A path is a glob pattern if it contains `*`, `?`, `[`, or `{`. This matches the character set used by `globBase()` in `resolver.go` for consistency across the codebase. Extract as a shared `isGlobPattern(path string) bool` helper if useful.

#### Signature change

```go
func PolicyFromConfig(
    cfg *config.SandboxPolicy,
    projectRoot, runtimeDir, homeDir, tempDir string,
) (*Policy, []string, error)
//          ^^^^^^^^ path warnings (e.g. "~/.kube: not found")
```

All callers of `PolicyFromConfig` must be updated. The launcher uses the warnings for the banner. Other callers (e.g. `sandboxShowCmd`, `sandboxTestCmd`) can ignore warnings with `_`.

#### Behavior

For each user-specified literal (non-glob) path in the active branch:
1. Resolve templates (`{{ .home }}`, etc.)
2. Check `os.Lstat(path)`
3. If not found: skip from policy, add to warnings list
4. If found: include in policy as before

Warnings are returned to the caller and flow into `BannerData.Warnings` for display.

**Relationship to existing validation:** `ValidateSandboxConfigDetailed` in `policy.go` returns `ValidationResult.Warnings` for config-level issues (e.g. writable + writable_extra both set). The new path warnings from `PolicyFromConfig` are a separate concern (runtime path existence). Both flow into `BannerData.Warnings` as a combined list with clear prefixes: existing warnings as-is, path warnings as `"skipped: <path> (not found)"`.

### Integration Points

#### Output destination

- **Launch banner:** prints to **stderr** so it doesn't interfere with agent stdout
- **`aide which`:** prints to **stdout** (preserving existing behavior — `aide which` is diagnostic output, not agent output)

#### `Launcher.Launch()`

Updated signature:

```go
func (l *Launcher) Launch(cwd string, agentOverride string,
    extraArgs []string, cleanEnv bool, resolve bool) error
```

After resolving context and building the sandbox policy, before exec:

1. Collect path warnings from `PolicyFromConfig`
2. Get resolved preferences from `ResolvedContext.Preferences`
3. If `resolve` flag: override `ShowInfo=true`, `InfoDetail="detailed"`
4. If `ShowInfo` is true:
   - Build `BannerData` from resolved context, policy, warnings
   - If detailed: populate secret keys, resolved env, full path lists
   - Call `ui.RenderBanner(os.Stderr, style, data)`
5. Exec the agent

#### `Launcher.execAgent()` (passthrough path)

The passthrough path has limited data: no `ResolvedContext`, no `SandboxPolicy` config, no secrets. The banner is simpler:

- **Available:** agent name, agent binary path, project root, sandbox policy (default), agent config dirs
- **Not available:** context name/match reason, secrets, env vars, user sandbox config, preferences

For passthrough, show a minimal banner with what's available (agent, sandbox defaults). No path validation warnings since there's no user config. Preferences default to `show_info: true`, `info_style: compact`, `info_detail: normal` since there's no config file to read.

#### `whichCmd()`

Refactor to use the same `BannerData` + `RenderBanner` path. Always renders regardless of `show_info` (since the user explicitly asked). `--resolve` forces detailed mode. Output goes to stdout (existing behavior).

#### `applyProjectOverride()`

Add preferences merging alongside existing field merges:

```go
if po.Preferences != nil {
    rc.Preferences = ResolvePreferences(&rc.Preferences, po.Preferences)
}
```

## Testing

| Test | Description |
|------|-------------|
| **Config** | |
| `TestPreferences_Unmarshal` | Parse YAML with preferences section |
| `TestPreferences_Defaults` | Missing preferences → defaults applied |
| `TestPreferences_ProjectOverride` | Project overrides specific fields, inherits others |
| `TestPreferences_InvalidStyle` | Unknown info_style → validation warning, runtime fallback to compact |
| **Path validation** | |
| `TestPolicyFromConfig_SkipsNonExistentPaths` | User path doesn't exist → skipped + warning |
| `TestPolicyFromConfig_GlobsNotValidated` | Glob patterns (including `{`) pass through without validation |
| `TestPolicyFromConfig_ExistingPathsIncluded` | Existing user paths included normally |
| `TestPolicyFromConfig_MixedPaths` | Mix of existing, non-existing, glob → correct filtering + warnings |
| `TestPolicyFromConfig_OnlyActiveBranch` | writable set → writable_extra ignored, validation only runs on writable |
| **Banner renderer** | |
| `TestRenderCompact` | Snapshot test of compact output |
| `TestRenderBoxed` | Snapshot test of boxed output |
| `TestRenderClean` | Snapshot test of clean output |
| `TestRenderBanner_UnknownStyle` | Falls back to compact |
| `TestRenderBanner_WithWarnings` | Warnings appear in output |
| `TestRenderBanner_DetailedMode` | Full paths, key lists, redacted values shown |
| `TestRenderBanner_NormalMode` | Counts only, annotations only |
| `TestRenderBanner_NoSandbox` | Disabled sandbox shown correctly |
| `TestRenderBanner_NoSecret` | Secret section omitted |
| `TestRenderBanner_NoEnv` | Env section omitted |
| **Integration** | |
| `TestLaunch_BannerPrintsToStderr` | Banner output goes to stderr |
| `TestLaunch_ShowInfoFalse` | No banner when show_info: false |
| `TestLaunch_ResolveFlag` | --resolve forces detailed output |
| `TestLaunch_ResolveFlagOverridesShowInfoFalse` | --resolve shows banner even with show_info: false |
| `TestWhich_AlwaysShowsBanner` | aide which renders regardless of show_info |
| `TestPassthrough_MinimalBanner` | Passthrough shows agent + sandbox defaults only |

## Files Changed

| File | Change |
|------|--------|
| `internal/config/schema.go` | Add `Preferences` struct, add field to `Config` and `ProjectOverride` |
| `internal/config/schema_test.go` | Preferences unmarshal + validation tests |
| `internal/context/resolver.go` | Add `Preferences` to `ResolvedContext`, merge in `applyProjectOverride`, add `ResolvePreferences` helper |
| `internal/context/resolver_test.go` | Preferences merge tests |
| `internal/ui/banner.go` | New: `BannerData`, `SandboxInfo`, render functions |
| `internal/ui/banner_test.go` | New: snapshot tests for all styles + modes |
| `internal/sandbox/policy.go` | Return warnings from `PolicyFromConfig`, validate user paths, add `isGlobPattern` helper |
| `internal/sandbox/policy_test.go` | Path validation tests |
| `internal/launcher/launcher.go` | Add `resolve` param to `Launch`, build BannerData, call renderer |
| `internal/launcher/passthrough.go` | Minimal banner support for passthrough path |
| `cmd/aide/commands.go` | Refactor `whichCmd` to use shared renderer, add `--resolve` to root cmd, update `Launch` call sites |
| `cmd/aide/main.go` | Wire `--resolve` flag on root command |
