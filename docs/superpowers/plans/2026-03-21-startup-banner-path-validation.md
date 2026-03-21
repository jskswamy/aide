# Startup Banner & Path Validation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a configurable startup info banner (compact/boxed/clean styles, normal/detailed detail levels) and validate user-specified sandbox paths at launch time.

**Architecture:** Three layers — config schema (`Preferences` struct + merge), sandbox policy (path validation + warnings), and UI rendering (`internal/ui` package). The launcher wires them together. Each layer is independently testable.

**Tech Stack:** Go, cobra CLI, `fatih/color` (existing dep), `gopkg.in/yaml.v3`

**Spec:** `docs/superpowers/specs/2026-03-21-startup-banner-path-validation-design.md`

---

### Task 1: Preferences Config Schema

**Files:**
- Modify: `internal/config/schema.go`
- Test: `internal/config/schema_test.go`

- [ ] **Step 1: Add Preferences struct and ResolvePreferences to schema.go**

Add after the `ProjectOverride` struct:

```go
// Preferences holds global display/behavior settings.
type Preferences struct {
	ShowInfo   *bool  `yaml:"show_info,omitempty"`
	InfoStyle  string `yaml:"info_style,omitempty"`
	InfoDetail string `yaml:"info_detail,omitempty"`
}

// ResolvePreferences merges global and project preferences,
// applying defaults for unset fields.
func ResolvePreferences(global, project *Preferences) Preferences {
	t := true
	result := Preferences{
		ShowInfo:   &t,
		InfoStyle:  "compact",
		InfoDetail: "normal",
	}
	if global != nil {
		if global.ShowInfo != nil {
			result.ShowInfo = global.ShowInfo
		}
		if global.InfoStyle != "" {
			result.InfoStyle = global.InfoStyle
		}
		if global.InfoDetail != "" {
			result.InfoDetail = global.InfoDetail
		}
	}
	if project != nil {
		if project.ShowInfo != nil {
			result.ShowInfo = project.ShowInfo
		}
		if project.InfoStyle != "" {
			result.InfoStyle = project.InfoStyle
		}
		if project.InfoDetail != "" {
			result.InfoDetail = project.InfoDetail
		}
	}
	return result
}
```

Add `Preferences *Preferences` field to both `Config` and `ProjectOverride` structs.

- [ ] **Step 2: Write tests for Preferences**

Add to `schema_test.go`:
- `TestPreferences_Unmarshal` — YAML with `preferences:` section parses correctly
- `TestPreferences_Defaults` — `ResolvePreferences(nil, nil)` returns defaults
- `TestPreferences_GlobalOverride` — global sets `info_style: boxed`, result has boxed
- `TestPreferences_ProjectOverride` — global sets boxed, project sets clean, result has clean
- `TestPreferences_PartialProjectOverride` — project only sets `show_info: false`, inherits style/detail from global
- `TestPreferences_InvalidStyle` — `ResolvePreferences` with `info_style: "unknown"` keeps the value; callers (RenderBanner) fall back to compact

- [ ] **Step 3: Run tests**

Run: `go test ./internal/config/...`
Expected: all pass

- [ ] **Step 4: Commit**

```
Add Preferences config schema with merge logic
```

---

### Task 2: Preferences in Context Resolver

**Files:**
- Modify: `internal/context/resolver.go`
- Test: `internal/context/resolver_test.go`

- [ ] **Step 1: Add Preferences field to ResolvedContext**

```go
type ResolvedContext struct {
	Name        string
	MatchReason string
	Context     config.Context
	Preferences config.Preferences
}
```

- [ ] **Step 2: Initialize preferences in Resolve()**

After creating `rc` (both the minimal and full-format paths), add:

```go
rc.Preferences = config.ResolvePreferences(cfg.Preferences, nil)
```

- [ ] **Step 3: Merge project preferences in applyProjectOverride()**

Add after the env merge block:

```go
if po.Preferences != nil {
	rc.Preferences = config.ResolvePreferences(&rc.Preferences, po.Preferences)
}
```

- [ ] **Step 4: Write tests**

Add to `resolver_test.go`:
- `TestResolve_PreferencesFromGlobal` — config has `Preferences: &Preferences{InfoStyle: "boxed"}`, resolved context has boxed
- `TestResolve_PreferencesProjectOverride` — global has boxed, project override has clean, result is clean
- `TestResolve_PreferencesDefaults` — no preferences in config, resolved context has defaults

- [ ] **Step 5: Run tests**

Run: `go test ./internal/context/...`
Expected: all pass

- [ ] **Step 6: Commit**

```
Wire preferences through context resolver
```

---

### Task 3: Path Validation in PolicyFromConfig

**Files:**
- Modify: `internal/sandbox/policy.go`
- Test: `internal/sandbox/policy_test.go`

- [ ] **Step 1: Add isGlobPattern helper**

```go
func isGlobPattern(path string) bool {
	return strings.ContainsAny(path, "*?[{")
}
```

- [ ] **Step 2: Add validateAndFilterPaths helper**

```go
// validateAndFilterPaths checks each resolved path. Non-glob paths that
// don't exist on disk are skipped and a warning is added.
func validateAndFilterPaths(paths []string, warnings *[]string) []string {
	var filtered []string
	for _, p := range paths {
		if isGlobPattern(p) {
			filtered = append(filtered, p)
			continue
		}
		if _, err := os.Lstat(p); err != nil {
			*warnings = append(*warnings, fmt.Sprintf("skipped: %s (not found)", p))
			continue
		}
		filtered = append(filtered, p)
	}
	return filtered
}
```

Add `"os"` to imports.

- [ ] **Step 3: Update PolicyFromConfig signature**

Change return from `(*Policy, error)` to `(*Policy, []string, error)`.

For the `cfg == nil` case, return `&defaults, nil, nil`.

- [ ] **Step 4: Add path validation calls**

After each `ResolvePaths` call in PolicyFromConfig, pass the result through `validateAndFilterPaths`:

```go
// In the writable override branch:
w, err := ResolvePaths(cfg.Writable, templateVars)
if err != nil {
    return nil, nil, err
}
policy.Writable = validateAndFilterPaths(w, &warnings)

// Same pattern for writable_extra, readable, readable_extra, denied, denied_extra
```

Initialize `var warnings []string` at the top of the function. Return `&policy, warnings, nil` at the end.

- [ ] **Step 5: Update all callers of PolicyFromConfig**

These callers need the extra return value (search for `PolicyFromConfig(` in each file):

- `internal/launcher/launcher.go` — the `PolicyFromConfig` call in `Launch()`. Capture warnings for banner.
- `cmd/aide/commands.go` — three calls: in `whichCmd` (sandbox section), `sandboxShowCmd`, and `sandboxTestCmd`. Ignore warnings with `_` for now.

- [ ] **Step 6: Update all existing PolicyFromConfig tests**

Every test that calls `PolicyFromConfig` needs the extra `warnings` return value. Add `_, ` before `err` in the return capture. There are ~20 tests in `policy_test.go`.

- [ ] **Step 7: Write new path validation tests**

Add to `policy_test.go`:

- `TestPolicyFromConfig_SkipsNonExistentPaths` — `writable_extra: ["/nonexistent/path"]`, assert path not in policy, warning returned
- `TestPolicyFromConfig_GlobsNotValidated` — `denied: ["~/.ssh/id_*", "~/.config/{foo}"]`, assert both pass through, no warnings
- `TestPolicyFromConfig_ExistingPathsIncluded` — create a temp dir, add to `writable_extra`, assert included, no warnings
- `TestPolicyFromConfig_MixedPaths` — mix of existing, non-existing, glob paths, verify correct filtering + warnings
- `TestPolicyFromConfig_OnlyActiveBranch` — set both `writable` and `writable_extra`, verify only `writable` is validated (extra is ignored per if/else logic)

- [ ] **Step 8: Run tests**

Run: `go test ./internal/sandbox/... ./internal/launcher/... ./cmd/aide/...`
Expected: all pass (build may fail until commands.go callers are fixed — fix those first)

- [ ] **Step 9: Commit**

```
Add path validation to PolicyFromConfig with warnings
```

---

### Task 4: Banner Renderer Package

**Files:**
- Create: `internal/ui/banner.go`
- Create: `internal/ui/banner_test.go`

- [ ] **Step 1: Create internal/ui directory and data structs**

Create `internal/ui/banner.go` with:

```go
package ui

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/fatih/color"
)

type BannerData struct {
	ContextName string
	MatchReason string
	AgentName   string
	AgentPath   string
	SecretName  string
	SecretKeys  []string
	Env         map[string]string
	EnvResolved map[string]string
	Sandbox     *SandboxInfo
	Warnings    []string
}

type SandboxInfo struct {
	Disabled      bool
	Network       string
	Ports         string
	WritableCount int
	ReadableCount int
	Denied        []string
	Writable      []string
	Readable      []string
}
```

- [ ] **Step 2: Implement helper functions**

```go
var (
	boldGreen = color.New(color.FgGreen, color.Bold)
	cyan      = color.New(color.FgCyan)
	yellow    = color.New(color.FgYellow)
	dim       = color.New(color.Faint)
)

func RenderBanner(w io.Writer, style string, data *BannerData) {
	switch style {
	case "boxed":
		RenderBoxed(w, data)
	case "clean":
		RenderClean(w, data)
	default:
		RenderCompact(w, data)
	}
}
```

Add helper functions for formatting:
- `formatAgent(data)` — returns agent string based on whether AgentPath is populated
- `formatSecret(data)` — returns secret string with key count or key list
- `formatEnv(data)` — returns env lines with annotations or resolved values
- `formatSandbox(data)` — returns sandbox lines with denied list + counts or full paths
- `formatWarnings(data)` — returns warning lines

- [ ] **Step 3: Implement RenderCompact**

Renders the compact style with emoji prefixes:
```
🔧 aide · work (claude → /usr/local/bin/claude)
   📁 path glob match: ~/work/*
   🔐 secret: work (3 keys: api_key, org_id, token)
   📦 env: ANTHROPIC_API_KEY ← secrets.api_key
   🛡️  sandbox: outbound
      ...
```

- [ ] **Step 4: Implement RenderBoxed**

Renders with box-drawing characters:
```
┌─ aide ──────────────────────
│ 🎯 Context   work
│ ...
└──────────────────────────────
```

- [ ] **Step 5: Implement RenderClean**

Renders with simple indentation:
```
aide · context: work
  Agent     claude → /usr/local/bin/claude
  ...
```

- [ ] **Step 6: Write tests**

Add `internal/ui/banner_test.go` with:
- `TestRenderCompact` — full BannerData, verify output contains key elements
- `TestRenderBoxed` — verify box-drawing chars present
- `TestRenderClean` — verify clean format
- `TestRenderBanner_UnknownStyle` — unknown style falls back to compact output
- `TestRenderBanner_WithWarnings` — warnings appear with ⚠
- `TestRenderBanner_DetailedMode` — full paths in sandbox, key names in secret
- `TestRenderBanner_NormalMode` — counts only
- `TestRenderBanner_NoSandbox` — sandbox disabled shown
- `TestRenderBanner_NoSecret` — secret section omitted
- `TestRenderBanner_NoEnv` — env section omitted

Note: Use `color.NoColor = true` in tests to disable ANSI codes for predictable output matching.

- [ ] **Step 7: Run tests**

Run: `go test ./internal/ui/...`
Expected: all pass

- [ ] **Step 8: Commit**

```
Add banner renderer with compact, boxed, and clean styles
```

---

### Task 5: Wire --resolve Flag and Launch Banner

**Files:**
- Modify: `cmd/aide/main.go`
- Modify: `internal/launcher/launcher.go`
- Modify: `internal/launcher/passthrough.go`
- Modify: `cmd/aide/commands.go`

- [ ] **Step 1: Add --resolve persistent flag to root command**

In `main.go`, add:

```go
var resolve bool
```

Add to flags:

```go
rootCmd.PersistentFlags().BoolVar(&resolve, "resolve", false,
    "Show detailed startup info (forces show_info and detailed mode)")
```

Update the `Launch` call to pass `resolve`:

```go
return l.Launch(cwd, agentFlag, args, cleanEnv, resolve)
```

- [ ] **Step 2: Update Launcher.Launch() signature and add banner**

Add `resolve bool` parameter. Add `Stderr io.Writer` field to Launcher (default `os.Stderr`).

After building the env (step 10) and before sandbox apply (step 12), add banner rendering:

```go
// 11b. Render startup banner
prefs := rc.Preferences
if resolve {
    t := true
    prefs.ShowInfo = &t
    prefs.InfoDetail = "detailed"
}
if prefs.ShowInfo != nil && *prefs.ShowInfo {
    bannerData := buildBannerData(rc, agentName, binary, env, policy, pathWarnings, &prefs)
    stderr := l.stderr()
    ui.RenderBanner(stderr, prefs.InfoStyle, bannerData)
}
```

Add a `buildBannerData` helper function and a `stderr()` method on Launcher.

- [ ] **Step 3: Add banner to passthrough path**

In `execAgent()`, add minimal banner after building the policy:

```go
bannerData := &ui.BannerData{
    AgentName: name,
    AgentPath: binary,
    Sandbox: &ui.SandboxInfo{
        Network:       string(policy.Network),
        Ports:         "all",
        WritableCount: len(policy.Writable),
        ReadableCount: len(policy.Readable),
        Denied:        policy.Denied,
    },
}
ui.RenderBanner(os.Stderr, "compact", bannerData)
```

- [ ] **Step 4: Refactor whichCmd to use banner renderer**

Replace the existing manual output in `whichCmd` with:
1. Build `BannerData` from resolved context (same logic as launcher)
2. Call `ui.RenderBanner(out, prefs.InfoStyle, data)` where `out` is `cmd.OutOrStdout()`
3. When `--resolve` is set, populate detailed fields (secret keys, resolved env, full sandbox paths)
4. `aide which` always renders regardless of `show_info`

**Important:** `whichCmd` currently has its own local `--resolve` flag. Remove it and rely on the persistent `--resolve` from the root command. Read the flag via `cmd.Flags().GetBool("resolve")` (persistent flags are inherited). This avoids a duplicate-flag registration error.

Move `classifyEnvSource` and `redactValue` to `internal/ui/helpers.go` (or keep them in commands.go and call them when building BannerData — simpler).

- [ ] **Step 5: Write integration tests**

Add to `internal/launcher/launcher_test.go`:

- `TestLaunch_BannerPrintsToStderr` — Launch with a mock Execer and a `Stderr` buffer. Verify banner output written to the buffer.
- `TestLaunch_ShowInfoFalse` — Config with `preferences: {show_info: false}`. Verify stderr buffer is empty.
- `TestLaunch_ResolveFlag` — Launch with `resolve: true`. Verify detailed info appears (full paths, key names).
- `TestLaunch_ResolveFlagOverridesShowInfoFalse` — Config has `show_info: false`, but `resolve: true`. Verify banner still prints.
- `TestPassthrough_MinimalBanner` — Passthrough launch. Verify banner shows agent name and sandbox defaults.

These tests use the existing `mockExecer` pattern from `launcher_test.go`. Set `l.Stderr` to a `bytes.Buffer` to capture output.

- [ ] **Step 6: Build and fix compilation**

Run: `go build ./...`
Fix any compilation errors from the signature change.

- [ ] **Step 7: Run full test suite**

Run: `go test ./...`
Expected: all pass

- [ ] **Step 8: Commit**

```
Wire startup banner into launch and which commands
```

---

### Task 6: Final Integration and Docs

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Update README with preferences config**

Add a section showing the `preferences:` config block and what each option does.

- [ ] **Step 2: Run full test suite**

Run: `go test ./...`
Expected: all pass

- [ ] **Step 3: Build binary and manual test**

Run: `go build ./cmd/aide`
Test: `./aide which` — should show the new banner
Test: `./aide which --resolve` — should show detailed banner

- [ ] **Step 4: Commit**

```
Update README with preferences documentation
```
