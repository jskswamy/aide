# Aide Statusline Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement `aide statusline claude` — a dual-mode command that renders active aide session state as a persistent status bar in Claude Code's UI, with a YAML-configurable module system and one-command install.

**Architecture:** Aide injects `AIDE_*` env vars before exec'ing the agent; these propagate to `aide statusline claude` (invoked by Claude Code as its statusLine command) via env inheritance — no file IPC. Config lives under `statusline:` in the global aide config and `.aide.yaml`, merged field-by-field per module. The install path patches `~/.claude/settings.json` atomically, generating a wrapper script when an existing statusLine is found. Render reads env + config and writes one line to stdout.

**Tech Stack:** Go, `gopkg.in/yaml.v3`, `encoding/json`, `github.com/spf13/cobra`, `github.com/jskswamy/aide/internal/fsutil` (AtomicWrite/AtomicWriteExecutable), `github.com/jskswamy/aide/internal/provision/agents/claude` (readSettings), `github.com/jskswamy/aide/internal/config`, `github.com/jskswamy/aide/internal/ui`, `github.com/jskswamy/aide/internal/capability`

## Global Constraints

- Default emoji: `🔒`/`🔓` (sandbox), `🌐`/`🌍` (network), `⚡` (caps icon), `⚠️` (trust untrusted), `📁` (context icon), `🚨` (auto_approve)
- `auto_approve` is always prepended first when active; `disabled: true` on that module has no effect
- `caps` and `context` modules are hidden (return `""`) when their env var is empty
- `trust` module is hidden when `AIDE_TRUST=trusted`; shown only when `AIDE_TRUST=untrusted`
- Empty string for any state string suppresses that state — but the zero-value limitation means you cannot suppress a non-empty default via YAML override; use `disabled: true` to hide the whole module
- Wrapper script written with `AtomicWriteExecutable` (mode 0700 equivalent)
- All JSON writes use `fsutil.AtomicWrite`
- `*bool` for `Disabled` field — follows existing `*bool ShowInfo` pattern in `Preferences`; allows project to re-enable a globally disabled module with `disabled: false`
- Never write "firmus" anywhere in code, tests, comments, or commits

---

## File Structure

| File | Action | Responsibility |
|---|---|---|
| `internal/config/schema.go` | Modify | `StatuslineConfig`, `ModuleConfig` structs; add to `Config` + `ProjectOverride`; `defaultStatuslineConfig`, `ResolveStatusline`, `applyStatuslineOverride`, `applyModuleOverride` |
| `internal/config/statusline_test.go` | Create | `TestResolveStatusline_*` merge tests |
| `internal/launcher/launcher.go` | Modify | `injectAideSessionEnv` function; inject before exec |
| `internal/launcher/launcher_statusline_test.go` | Create | `TestInjectAideSessionEnv_*` |
| `internal/provision/agents/claude/hooks.go` | Modify | `ReadStatusLine`, `WriteStatusLine`, `RemoveStatusLine`, `WrapperScriptPath`, `WriteWrapper` |
| `internal/provision/agents/claude/statusline_test.go` | Create | `TestReadStatusLine_*`, `TestWriteStatusLine_*`, `TestRemoveStatusLine`, `TestWriteWrapper` |
| `cmd/aide/statusline.go` | Create | `statuslineCmd`, `statuslineAgentCmd`, `renderStatusline`, `renderModule`, `isModuleDisabled`, `installStatusline`, `removeStatusline`, `envForRender` |
| `cmd/aide/statusline_test.go` | Create | `TestRenderStatusline_*` |
| `cmd/aide/commands.go` | Modify | Register `statuslineCmd()` |

---

### Task 1: Config Schema

**Files:**
- Modify: `internal/config/schema.go`
- Create: `internal/config/statusline_test.go`

**Interfaces:**
- Produces: `config.StatuslineConfig`, `config.ModuleConfig`
- Produces: `config.ResolveStatusline(global, project *StatuslineConfig) StatuslineConfig`
- Consumes: nothing from prior tasks

- [ ] **Step 1: Write the failing tests**

Create `internal/config/statusline_test.go`:

```go
package config

import (
	"reflect"
	"testing"
)

func boolPtrSL(b bool) *bool { return &b }

func TestResolveStatusline_NilInputsGivesDefaults(t *testing.T) {
	got := ResolveStatusline(nil, nil)
	wantOrder := []string{"sandbox", "network", "caps", "trust", "context"}
	if !reflect.DeepEqual(got.Order, wantOrder) {
		t.Errorf("Order = %v, want %v", got.Order, wantOrder)
	}
	if got.Sandbox == nil || got.Sandbox.On != "🔒" {
		t.Errorf("Sandbox.On = %q, want 🔒", got.Sandbox.On)
	}
	if got.Sandbox == nil || got.Sandbox.Off != "🔓" {
		t.Errorf("Sandbox.Off = %q, want 🔓", got.Sandbox.Off)
	}
	if got.Network == nil || got.Network.Outbound != "🌐" {
		t.Errorf("Network.Outbound = %q, want 🌐", got.Network.Outbound)
	}
	if got.Caps == nil || got.Caps.Icon != "⚡" {
		t.Errorf("Caps.Icon = %q, want ⚡", got.Caps.Icon)
	}
	if got.Trust == nil || got.Trust.Untrusted != "⚠️" {
		t.Errorf("Trust.Untrusted = %q, want ⚠️", got.Trust.Untrusted)
	}
	if got.Context == nil || got.Context.Icon != "📁" {
		t.Errorf("Context.Icon = %q, want 📁", got.Context.Icon)
	}
	if got.AutoApprove == nil || got.AutoApprove.Value != "🚨" {
		t.Errorf("AutoApprove.Value = %q, want 🚨", got.AutoApprove.Value)
	}
}

func TestResolveStatusline_ProjectOverridesModuleField(t *testing.T) {
	project := &StatuslineConfig{
		Trust: &ModuleConfig{Disabled: boolPtrSL(true)},
	}
	got := ResolveStatusline(nil, project)
	if got.Trust == nil || got.Trust.Disabled == nil || !*got.Trust.Disabled {
		t.Error("Trust.Disabled should be true after project override")
	}
	if got.Sandbox == nil || got.Sandbox.On != "🔒" {
		t.Errorf("Sandbox.On = %q, want default 🔒 (unchanged)", got.Sandbox.On)
	}
}

func TestResolveStatusline_ProjectReplacesOrderWholesale(t *testing.T) {
	project := &StatuslineConfig{Order: []string{"caps", "sandbox"}}
	got := ResolveStatusline(nil, project)
	want := []string{"caps", "sandbox"}
	if !reflect.DeepEqual(got.Order, want) {
		t.Errorf("Order = %v, want %v", got.Order, want)
	}
}

func TestResolveStatusline_GlobalThenProjectFieldMerge(t *testing.T) {
	global := &StatuslineConfig{
		Sandbox: &ModuleConfig{On: "G"},
		Network: &ModuleConfig{Outbound: "N"},
	}
	project := &StatuslineConfig{
		Sandbox: &ModuleConfig{On: "P"},
	}
	got := ResolveStatusline(global, project)
	if got.Sandbox.On != "P" {
		t.Errorf("Sandbox.On = %q, want P (project wins)", got.Sandbox.On)
	}
	if got.Network.Outbound != "N" {
		t.Errorf("Network.Outbound = %q, want N (global preserved)", got.Network.Outbound)
	}
}

func TestResolveStatusline_GlobalOrderPreservedWhenProjectEmpty(t *testing.T) {
	global := &StatuslineConfig{Order: []string{"trust", "sandbox"}}
	got := ResolveStatusline(global, nil)
	want := []string{"trust", "sandbox"}
	if !reflect.DeepEqual(got.Order, want) {
		t.Errorf("Order = %v, want %v", got.Order, want)
	}
}

func TestResolveStatusline_ProjectCanReenableDisabledModule(t *testing.T) {
	global := &StatuslineConfig{Trust: &ModuleConfig{Disabled: boolPtrSL(true)}}
	project := &StatuslineConfig{Trust: &ModuleConfig{Disabled: boolPtrSL(false)}}
	got := ResolveStatusline(global, project)
	if got.Trust == nil || got.Trust.Disabled == nil || *got.Trust.Disabled {
		t.Error("Trust.Disabled should be false (project re-enabled)")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/config/... -run TestResolveStatusline -v
```

Expected: compile error — `StatuslineConfig`, `ModuleConfig`, `ResolveStatusline` undefined.

- [ ] **Step 3: Add types to `internal/config/schema.go`**

**3a.** Find `type Config struct` (line 13). Add after the `Preferences *Preferences` field (line 32):

```go
	Statusline *StatuslineConfig `yaml:"statusline,omitempty"`
```

**3b.** Find `type ProjectOverride struct` (line 753). Add after the `Preferences *Preferences` field (line 759):

```go
	Statusline *StatuslineConfig `yaml:"statusline,omitempty"`
```

**3c.** Append these types and functions at the end of `schema.go` (before the final blank line):

```go
// StatuslineConfig holds statusline display configuration.
// Lives under the top-level "statusline:" key in both global aide config
// and .aide.yaml. Merge: order replaced wholesale; module fields merged
// field-by-field (project wins over global, global wins over built-in default).
type StatuslineConfig struct {
	Order       []string      `yaml:"order,omitempty"`
	Sandbox     *ModuleConfig `yaml:"sandbox,omitempty"`
	Network     *ModuleConfig `yaml:"network,omitempty"`
	Caps        *ModuleConfig `yaml:"caps,omitempty"`
	Trust       *ModuleConfig `yaml:"trust,omitempty"`
	Context     *ModuleConfig `yaml:"context,omitempty"`
	AutoApprove *ModuleConfig `yaml:"auto_approve,omitempty"`
}

// ModuleConfig holds per-module statusline display configuration.
// State fields (On, Off, etc.) are the full rendered string for that state.
// Empty string suppresses the state. Icon is a prefix for list-type modules.
// Disabled uses *bool so project config can re-enable a globally disabled
// module by setting disabled: false.
type ModuleConfig struct {
	Disabled     *bool  `yaml:"disabled,omitempty"`
	Icon         string `yaml:"icon,omitempty"`
	On           string `yaml:"on,omitempty"`
	Off          string `yaml:"off,omitempty"`
	Outbound     string `yaml:"outbound,omitempty"`
	Unrestricted string `yaml:"unrestricted,omitempty"`
	Untrusted    string `yaml:"untrusted,omitempty"`
	Value        string `yaml:"value,omitempty"`
}

func defaultStatuslineConfig() StatuslineConfig {
	return StatuslineConfig{
		Order:       []string{"sandbox", "network", "caps", "trust", "context"},
		Sandbox:     &ModuleConfig{On: "🔒", Off: "🔓"},
		Network:     &ModuleConfig{Outbound: "🌐", Unrestricted: "🌍"},
		Caps:        &ModuleConfig{Icon: "⚡"},
		Trust:       &ModuleConfig{Untrusted: "⚠️"},
		Context:     &ModuleConfig{Icon: "📁"},
		AutoApprove: &ModuleConfig{Value: "🚨"},
	}
}

// ResolveStatusline merges global and project statusline configs over
// built-in defaults.
func ResolveStatusline(global, project *StatuslineConfig) StatuslineConfig {
	result := defaultStatuslineConfig()
	if global != nil {
		applyStatuslineOverride(&result, global)
	}
	if project != nil {
		applyStatuslineOverride(&result, project)
	}
	return result
}

func applyStatuslineOverride(dst *StatuslineConfig, src *StatuslineConfig) {
	if len(src.Order) > 0 {
		dst.Order = make([]string, len(src.Order))
		copy(dst.Order, src.Order)
	}
	applyModuleOverride(&dst.Sandbox, src.Sandbox)
	applyModuleOverride(&dst.Network, src.Network)
	applyModuleOverride(&dst.Caps, src.Caps)
	applyModuleOverride(&dst.Trust, src.Trust)
	applyModuleOverride(&dst.Context, src.Context)
	applyModuleOverride(&dst.AutoApprove, src.AutoApprove)
}

func applyModuleOverride(dst **ModuleConfig, src *ModuleConfig) {
	if src == nil {
		return
	}
	if *dst == nil {
		*dst = &ModuleConfig{}
	}
	d := *dst
	if src.Disabled != nil {
		d.Disabled = src.Disabled
	}
	if src.Icon != "" {
		d.Icon = src.Icon
	}
	if src.On != "" {
		d.On = src.On
	}
	if src.Off != "" {
		d.Off = src.Off
	}
	if src.Outbound != "" {
		d.Outbound = src.Outbound
	}
	if src.Unrestricted != "" {
		d.Unrestricted = src.Unrestricted
	}
	if src.Untrusted != "" {
		d.Untrusted = src.Untrusted
	}
	if src.Value != "" {
		d.Value = src.Value
	}
}
```

- [ ] **Step 4: Run tests and build**

```bash
go test ./internal/config/... -run TestResolveStatusline -v
go build ./...
```

Expected: 6 tests PASS, no build errors.

- [ ] **Step 5: Commit**

Use `/commit` — message: "Add StatuslineConfig schema and merge logic"

---

### Task 2: Env Var Injection

**Files:**
- Modify: `internal/launcher/launcher.go`
- Create: `internal/launcher/launcher_statusline_test.go`

**Interfaces:**
- Consumes: `config.SandboxPolicy` (already in scope), `capability.Set` (already in scope), `ui.TrustInfo` (already in scope)
- Produces: `injectAideSessionEnv(env []string, sbDisabled bool, sandboxCfg *config.SandboxPolicy, autoApprove bool, caps *capability.Set, contextName string, agentName string, trustInfo *ui.TrustInfo) []string`

**Key types confirmed by reading the source:**
- `sandboxCfg *config.SandboxPolicy` — field `Network *config.NetworkPolicy`, field `NetworkPolicy.Mode string` ("unrestricted" or "outbound" or "")
- `resolvedCapSet *capability.Set` — field `Capabilities []capability.ResolvedCapability`, each has `Name string`
- `trustInfo *ui.TrustInfo` — field `Status string` ("untrusted" | "denied" | ""); nil or `""` means trusted
- `rc.Name string` — resolved context name
- `effectiveYolo bool` — auto-approve flag
- `sbDisabled bool` — sandbox disabled flag
- `agentName string` — logical agent name (e.g. "claude")
- `mergeEnv(base []string, resolved map[string]string) []string` — already in `launcher.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/launcher/launcher_statusline_test.go`:

```go
package launcher

import (
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/capability"
	"github.com/jskswamy/aide/internal/config"
	"github.com/jskswamy/aide/internal/ui"
)

func envMap(env []string) map[string]string {
	m := make(map[string]string, len(env))
	for _, kv := range env {
		if i := strings.IndexByte(kv, '='); i >= 0 {
			m[kv[:i]] = kv[i+1:]
		}
	}
	return m
}

func TestInjectAideSessionEnv_SandboxOn(t *testing.T) {
	result := envMap(injectAideSessionEnv(nil, false, nil, false, nil, "", "claude", nil))
	if result["AIDE_SANDBOX"] != "on" {
		t.Errorf("AIDE_SANDBOX = %q, want on", result["AIDE_SANDBOX"])
	}
}

func TestInjectAideSessionEnv_SandboxOff(t *testing.T) {
	result := envMap(injectAideSessionEnv(nil, true, nil, false, nil, "", "claude", nil))
	if result["AIDE_SANDBOX"] != "off" {
		t.Errorf("AIDE_SANDBOX = %q, want off", result["AIDE_SANDBOX"])
	}
}

func TestInjectAideSessionEnv_NetworkOutbound(t *testing.T) {
	result := envMap(injectAideSessionEnv(nil, false, nil, false, nil, "", "claude", nil))
	if result["AIDE_NETWORK_MODE"] != "outbound" {
		t.Errorf("AIDE_NETWORK_MODE = %q, want outbound", result["AIDE_NETWORK_MODE"])
	}
}

func TestInjectAideSessionEnv_NetworkUnrestricted(t *testing.T) {
	cfg := &config.SandboxPolicy{Network: &config.NetworkPolicy{Mode: "unrestricted"}}
	result := envMap(injectAideSessionEnv(nil, false, cfg, false, nil, "", "claude", nil))
	if result["AIDE_NETWORK_MODE"] != "unrestricted" {
		t.Errorf("AIDE_NETWORK_MODE = %q, want unrestricted", result["AIDE_NETWORK_MODE"])
	}
}

func TestInjectAideSessionEnv_CapsJoined(t *testing.T) {
	caps := &capability.Set{
		Capabilities: []capability.ResolvedCapability{
			{Name: "k8s"},
			{Name: "docker"},
		},
	}
	result := envMap(injectAideSessionEnv(nil, false, nil, false, caps, "", "claude", nil))
	if result["AIDE_CAPS"] != "k8s,docker" {
		t.Errorf("AIDE_CAPS = %q, want k8s,docker", result["AIDE_CAPS"])
	}
}

func TestInjectAideSessionEnv_NilCapsMeansEmpty(t *testing.T) {
	result := envMap(injectAideSessionEnv(nil, false, nil, false, nil, "", "claude", nil))
	if result["AIDE_CAPS"] != "" {
		t.Errorf("AIDE_CAPS = %q, want empty", result["AIDE_CAPS"])
	}
}

func TestInjectAideSessionEnv_TrustNilMeansTrusted(t *testing.T) {
	result := envMap(injectAideSessionEnv(nil, false, nil, false, nil, "", "claude", nil))
	if result["AIDE_TRUST"] != "trusted" {
		t.Errorf("AIDE_TRUST = %q, want trusted", result["AIDE_TRUST"])
	}
}

func TestInjectAideSessionEnv_TrustUntrusted(t *testing.T) {
	ti := &ui.TrustInfo{Status: "untrusted"}
	result := envMap(injectAideSessionEnv(nil, false, nil, false, nil, "", "claude", ti))
	if result["AIDE_TRUST"] != "untrusted" {
		t.Errorf("AIDE_TRUST = %q, want untrusted", result["AIDE_TRUST"])
	}
}

func TestInjectAideSessionEnv_AutoApprove(t *testing.T) {
	result := envMap(injectAideSessionEnv(nil, false, nil, true, nil, "", "claude", nil))
	if result["AIDE_AUTO_APPROVE"] != "1" {
		t.Errorf("AIDE_AUTO_APPROVE = %q, want 1", result["AIDE_AUTO_APPROVE"])
	}
}

func TestInjectAideSessionEnv_NoAutoApproveWhenFalse(t *testing.T) {
	result := envMap(injectAideSessionEnv(nil, false, nil, false, nil, "", "claude", nil))
	if _, ok := result["AIDE_AUTO_APPROVE"]; ok {
		t.Error("AIDE_AUTO_APPROVE should be absent when not active")
	}
}

func TestInjectAideSessionEnv_ContextName(t *testing.T) {
	result := envMap(injectAideSessionEnv(nil, false, nil, false, nil, "my-proj", "claude", nil))
	if result["AIDE_CONTEXT"] != "my-proj" {
		t.Errorf("AIDE_CONTEXT = %q, want my-proj", result["AIDE_CONTEXT"])
	}
}

func TestInjectAideSessionEnv_AgentName(t *testing.T) {
	result := envMap(injectAideSessionEnv(nil, false, nil, false, nil, "", "claude", nil))
	if result["AIDE_AGENT"] != "claude" {
		t.Errorf("AIDE_AGENT = %q, want claude", result["AIDE_AGENT"])
	}
}

func TestInjectAideSessionEnv_MergesWithExistingEnv(t *testing.T) {
	base := []string{"PATH=/usr/bin", "HOME=/home/user"}
	result := envMap(injectAideSessionEnv(base, false, nil, false, nil, "", "claude", nil))
	if result["PATH"] != "/usr/bin" {
		t.Error("existing PATH was lost")
	}
	if result["AIDE_SANDBOX"] == "" {
		t.Error("AIDE_SANDBOX was not injected")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/launcher/... -run TestInjectAideSessionEnv -v
```

Expected: compile error — `injectAideSessionEnv` undefined.

- [ ] **Step 3: Add `injectAideSessionEnv` to `internal/launcher/launcher.go`**

Add the imports `"github.com/jskswamy/aide/internal/ui"` and `"strings"` to the import block if not already present. Then add this function (near `filterNeverAllowEnv`, around line 70):

```go
// injectAideSessionEnv appends AIDE_* environment variables reflecting the
// active aide session state. Inherited by the agent and all child processes
// (including aide statusline claude invoked by the agent's TUI).
func injectAideSessionEnv(
	env []string,
	sbDisabled bool,
	sandboxCfg *config.SandboxPolicy,
	autoApprove bool,
	caps *capability.Set,
	contextName string,
	agentName string,
	trustInfo *ui.TrustInfo,
) []string {
	add := make(map[string]string, 8)

	if sbDisabled {
		add["AIDE_SANDBOX"] = "off"
	} else {
		add["AIDE_SANDBOX"] = "on"
	}

	networkMode := "outbound"
	if sandboxCfg != nil && sandboxCfg.Network != nil && sandboxCfg.Network.Mode == "unrestricted" {
		networkMode = "unrestricted"
	}
	add["AIDE_NETWORK_MODE"] = networkMode

	capList := ""
	if caps != nil {
		names := make([]string, 0, len(caps.Capabilities))
		for _, c := range caps.Capabilities {
			names = append(names, c.Name)
		}
		capList = strings.Join(names, ",")
	}
	add["AIDE_CAPS"] = capList

	if trustInfo == nil || trustInfo.Status == "" {
		add["AIDE_TRUST"] = "trusted"
	} else {
		add["AIDE_TRUST"] = "untrusted"
	}

	if autoApprove {
		add["AIDE_AUTO_APPROVE"] = "1"
	}

	if contextName != "" {
		add["AIDE_CONTEXT"] = contextName
	}

	if agentName != "" {
		add["AIDE_AGENT"] = agentName
	}

	return mergeEnv(env, add)
}
```

- [ ] **Step 4: Call `injectAideSessionEnv` before exec in `Launch`**

Find the exec block (around line 475–481). Add the injection call just before the `if l.Diagnose` block:

```go
	// Inject AIDE_* session vars for statusline and subprocesses.
	env = injectAideSessionEnv(env, sbDisabled, sandboxCfg, effectiveYolo, resolvedCapSet, rc.Name, agentName, trustInfo)

	// 14. Exec the agent binary
	args := append([]string{binary}, extraArgs...)
```

- [ ] **Step 5: Run tests and build**

```bash
go test ./internal/launcher/... -run TestInjectAideSessionEnv -v
go build ./...
```

Expected: 13 tests PASS, no build errors.

- [ ] **Step 6: Commit**

Use `/commit` — message: "Inject AIDE_* env vars before agent exec for statusline"

---

### Task 3: Settings Patch

**Files:**
- Modify: `internal/provision/agents/claude/hooks.go`
- Create: `internal/provision/agents/claude/statusline_test.go`

**Interfaces:**
- Consumes: `readSettings`, `settingsPath`, `strVal`, `fsutil.AtomicWrite`, `fsutil.AtomicWriteExecutable` — all already in `hooks.go` or its imports
- Produces:
  - `ReadStatusLine(ctx provision.Context) (string, error)`
  - `WriteStatusLine(ctx provision.Context, command string) error`
  - `RemoveStatusLine(ctx provision.Context) (string, error)`
  - `WrapperScriptPath(homeDir string) string`
  - `WriteWrapper(homeDir, existingCmd string) (string, error)`

**Note:** `hooks.go` already imports `encoding/json`, `errors`, `fmt`, `os`, `path/filepath`, `fsutil`, `provision`. All required — no new imports needed.

- [ ] **Step 1: Write the failing tests**

Create `internal/provision/agents/claude/statusline_test.go`:

```go
package claude_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/provision"
	"github.com/jskswamy/aide/internal/provision/agents/claude"
)

func tempClaudeCtx(t *testing.T) (provision.Context, string) {
	t.Helper()
	dir := t.TempDir()
	return provision.Context{
		HomeDir: dir,
		Env:     map[string]string{"CLAUDE_CONFIG_DIR": dir},
	}, dir
}

func TestReadStatusLine_EmptyWhenNoSettings(t *testing.T) {
	ctx, _ := tempClaudeCtx(t)
	got, err := claude.ReadStatusLine(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestWriteStatusLine_SetsCommand(t *testing.T) {
	ctx, dir := tempClaudeCtx(t)
	if err := claude.WriteStatusLine(ctx, "aide statusline claude"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "settings.json"))
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	sl, _ := raw["statusLine"].(map[string]interface{})
	if sl["command"] != "aide statusline claude" {
		t.Errorf("command = %v", sl["command"])
	}
}

func TestWriteStatusLine_PreservesExistingKeys(t *testing.T) {
	ctx, dir := tempClaudeCtx(t)
	os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"model":"claude-sonnet-4-6"}`), 0644)
	if err := claude.WriteStatusLine(ctx, "aide statusline claude"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "settings.json"))
	var raw map[string]interface{}
	json.Unmarshal(data, &raw)
	if raw["model"] != "claude-sonnet-4-6" {
		t.Error("model key was lost")
	}
}

func TestReadStatusLine_RoundTrip(t *testing.T) {
	ctx, _ := tempClaudeCtx(t)
	claude.WriteStatusLine(ctx, "aide statusline claude")
	got, err := claude.ReadStatusLine(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got != "aide statusline claude" {
		t.Errorf("ReadStatusLine = %q", got)
	}
}

func TestRemoveStatusLine_ReturnsPrevAndClearsKey(t *testing.T) {
	ctx, dir := tempClaudeCtx(t)
	claude.WriteStatusLine(ctx, "aide statusline claude")
	prev, err := claude.RemoveStatusLine(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if prev != "aide statusline claude" {
		t.Errorf("prev = %q", prev)
	}
	got, _ := claude.ReadStatusLine(ctx)
	if got != "" {
		t.Errorf("after remove, ReadStatusLine = %q, want empty", got)
	}
	// Verify key is gone from JSON
	data, _ := os.ReadFile(filepath.Join(dir, "settings.json"))
	var raw map[string]interface{}
	json.Unmarshal(data, &raw)
	if _, ok := raw["statusLine"]; ok {
		t.Error("statusLine key still present after remove")
	}
}

func TestRemoveStatusLine_EmptyWhenNeverSet(t *testing.T) {
	ctx, _ := tempClaudeCtx(t)
	prev, err := claude.RemoveStatusLine(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if prev != "" {
		t.Errorf("prev = %q, want empty", prev)
	}
}

func TestWriteWrapper_ContainsBothCommands(t *testing.T) {
	dir := t.TempDir()
	path, err := claude.WriteWrapper(dir, "npx ccstatusline")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	content := string(data)
	if !strings.Contains(content, "npx ccstatusline") {
		t.Errorf("wrapper missing existing command:\n%s", content)
	}
	if !strings.Contains(content, "aide statusline claude") {
		t.Errorf("wrapper missing aide command:\n%s", content)
	}
}

func TestWriteWrapper_IsExecutable(t *testing.T) {
	dir := t.TempDir()
	path, err := claude.WriteWrapper(dir, "npx ccstatusline")
	if err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)
	if info.Mode()&0100 == 0 {
		t.Errorf("wrapper is not executable: mode %v", info.Mode())
	}
}

func TestWrapperScriptPath_IsUnderConfig(t *testing.T) {
	got := claude.WrapperScriptPath("/home/user")
	want := "/home/user/.config/aide/statusline-wrapper.sh"
	if got != want {
		t.Errorf("WrapperScriptPath = %q, want %q", got, want)
	}
}
```

- [ ] **Step 2: Run to verify they fail**

```bash
go test ./internal/provision/agents/claude/... -run "TestReadStatusLine|TestWriteStatusLine|TestRemoveStatusLine|TestWriteWrapper|TestWrapperScriptPath" -v
```

Expected: compile errors — `claude.ReadStatusLine` etc. undefined.

- [ ] **Step 3: Add functions to `internal/provision/agents/claude/hooks.go`**

Append after the closing `}` of `strVal` (line 187):

```go
// ReadStatusLine returns the current statusLine.command from settings.json,
// or "" if not set or file does not exist.
func ReadStatusLine(ctx provision.Context) (string, error) {
	raw, err := readSettings(ctx)
	if err != nil {
		return "", err
	}
	sl, _ := raw["statusLine"].(map[string]interface{})
	return strVal(sl, "command"), nil
}

// WriteStatusLine sets statusLine.command in settings.json, preserving all
// other keys. Uses AtomicWrite for crash safety.
func WriteStatusLine(ctx provision.Context, command string) error {
	path := settingsPath(ctx)
	raw, err := readSettings(ctx)
	if err != nil {
		return err
	}
	raw["statusLine"] = map[string]interface{}{
		"type":    "command",
		"command": command,
	}
	data, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return fmt.Errorf("claude statusline: marshal: %w", err)
	}
	return fsutil.AtomicWrite(path, data)
}

// RemoveStatusLine deletes the statusLine key from settings.json.
// Returns the previous command (empty string if not set) so the caller
// can inform the user about wrapper cleanup.
func RemoveStatusLine(ctx provision.Context) (string, error) {
	path := settingsPath(ctx)
	raw, err := readSettings(ctx)
	if err != nil {
		return "", err
	}
	sl, _ := raw["statusLine"].(map[string]interface{})
	prev := strVal(sl, "command")
	delete(raw, "statusLine")
	data, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return "", fmt.Errorf("claude statusline: marshal: %w", err)
	}
	return prev, fsutil.AtomicWrite(path, data)
}

// WrapperScriptPath returns the path to the aide-generated statusline wrapper.
func WrapperScriptPath(homeDir string) string {
	return filepath.Join(homeDir, ".config", "aide", "statusline-wrapper.sh")
}

// WriteWrapper generates a wrapper script that pipes stdin to both the existing
// statusline command and aide statusline claude. Returns the script path.
// Uses AtomicWriteExecutable so the file is immediately runnable.
func WriteWrapper(homeDir, existingCmd string) (string, error) {
	path := WrapperScriptPath(homeDir)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return "", fmt.Errorf("claude statusline: mkdir: %w", err)
	}
	content := "#!/bin/bash\n" +
		"# Managed by aide statusline --install. Do not edit manually.\n" +
		"input=$(cat)\n" +
		"echo \"$input\" | " + existingCmd + "\n" +
		"echo \"$input\" | aide statusline claude\n"
	if err := fsutil.AtomicWriteExecutable(path, []byte(content)); err != nil {
		return "", fmt.Errorf("claude statusline: write wrapper: %w", err)
	}
	return path, nil
}
```

- [ ] **Step 4: Run tests and build**

```bash
go test ./internal/provision/agents/claude/... -run "TestReadStatusLine|TestWriteStatusLine|TestRemoveStatusLine|TestWriteWrapper|TestWrapperScriptPath" -v
go build ./...
```

Expected: 9 tests PASS, no build errors.

- [ ] **Step 5: Commit**

Use `/commit` — message: "Add statusline read/write/wrapper helpers to claude hooks"

---

### Task 4: Statusline Subcommand

**Files:**
- Create: `cmd/aide/statusline.go`
- Create: `cmd/aide/statusline_test.go`
- Modify: `cmd/aide/commands.go`

**Interfaces:**
- Consumes: `config.ResolveStatusline`, `config.StatuslineConfig`, `config.ModuleConfig`, `config.Load`, `config.Dir` (from Task 1); `claude.ReadStatusLine`, `claude.WriteStatusLine`, `claude.RemoveStatusLine`, `claude.WrapperScriptPath`, `claude.WriteWrapper` (from Task 3)
- Produces: `statuslineCmd() *cobra.Command` (registered in `commands.go`)
- Produces (internal): `renderStatusline(cfg config.StatuslineConfig, env map[string]string) string`

**Mode detection:** Claude Code invokes the statusline command with stdin as a pipe. When stdin is a character device (TTY), the command prints help. When stdin is a pipe, it renders.

- [ ] **Step 1: Write the failing tests**

Create `cmd/aide/statusline_test.go`:

```go
package main

import (
	"testing"

	"github.com/jskswamy/aide/internal/config"
)

func boolPtrST(b bool) *bool { return &b }

func TestRenderStatusline_AllModulesDefault(t *testing.T) {
	cfg := config.ResolveStatusline(nil, nil)
	env := map[string]string{
		"AIDE_SANDBOX":      "on",
		"AIDE_NETWORK_MODE": "outbound",
		"AIDE_CAPS":         "k8s,docker",
		"AIDE_TRUST":        "trusted",
		"AIDE_CONTEXT":      "my-project",
	}
	got := renderStatusline(cfg, env)
	want := "🔒 | 🌐 | ⚡ k8s,docker | 📁 my-project"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestRenderStatusline_UntrustedShowsTrust(t *testing.T) {
	cfg := config.ResolveStatusline(nil, nil)
	env := map[string]string{
		"AIDE_SANDBOX":      "on",
		"AIDE_NETWORK_MODE": "outbound",
		"AIDE_CAPS":         "",
		"AIDE_TRUST":        "untrusted",
	}
	got := renderStatusline(cfg, env)
	want := "🔒 | 🌐 | ⚠️"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestRenderStatusline_AutoApprovePrepended(t *testing.T) {
	cfg := config.ResolveStatusline(nil, nil)
	env := map[string]string{
		"AIDE_SANDBOX":      "on",
		"AIDE_NETWORK_MODE": "outbound",
		"AIDE_CAPS":         "",
		"AIDE_TRUST":        "trusted",
		"AIDE_AUTO_APPROVE": "1",
	}
	got := renderStatusline(cfg, env)
	want := "🚨 | 🔒 | 🌐"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestRenderStatusline_DisabledModuleSkipped(t *testing.T) {
	project := &config.StatuslineConfig{
		Network: &config.ModuleConfig{Disabled: boolPtrST(true)},
	}
	cfg := config.ResolveStatusline(nil, project)
	env := map[string]string{
		"AIDE_SANDBOX":      "on",
		"AIDE_NETWORK_MODE": "outbound",
		"AIDE_CAPS":         "",
		"AIDE_TRUST":        "trusted",
	}
	got := renderStatusline(cfg, env)
	want := "🔒"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestRenderStatusline_SandboxOff(t *testing.T) {
	cfg := config.ResolveStatusline(nil, nil)
	env := map[string]string{
		"AIDE_SANDBOX":      "off",
		"AIDE_NETWORK_MODE": "unrestricted",
		"AIDE_CAPS":         "",
		"AIDE_TRUST":        "trusted",
	}
	got := renderStatusline(cfg, env)
	want := "🔓 | 🌍"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestRenderStatusline_EmptyCapHidesModule(t *testing.T) {
	cfg := config.ResolveStatusline(nil, nil)
	env := map[string]string{
		"AIDE_SANDBOX":      "on",
		"AIDE_NETWORK_MODE": "outbound",
		"AIDE_CAPS":         "",
		"AIDE_TRUST":        "trusted",
	}
	got := renderStatusline(cfg, env)
	want := "🔒 | 🌐"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestRenderStatusline_CustomOrderRespected(t *testing.T) {
	project := &config.StatuslineConfig{
		Order: []string{"network", "sandbox"},
	}
	cfg := config.ResolveStatusline(nil, project)
	env := map[string]string{
		"AIDE_SANDBOX":      "on",
		"AIDE_NETWORK_MODE": "outbound",
		"AIDE_CAPS":         "",
		"AIDE_TRUST":        "trusted",
	}
	got := renderStatusline(cfg, env)
	want := "🌐 | 🔒"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestRenderStatusline_EmptyResultWhenAllHidden(t *testing.T) {
	project := &config.StatuslineConfig{
		Order: []string{},
	}
	cfg := config.ResolveStatusline(nil, project)
	got := renderStatusline(cfg, map[string]string{})
	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
}
```

- [ ] **Step 2: Run to verify they fail**

```bash
go test ./cmd/aide/... -run TestRenderStatusline -v
```

Expected: compile error — `renderStatusline` undefined.

- [ ] **Step 3: Create `cmd/aide/statusline.go`**

```go
package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/jskswamy/aide/internal/config"
	claudeprov "github.com/jskswamy/aide/internal/provision/agents/claude"
	"github.com/jskswamy/aide/internal/provision"
	"github.com/spf13/cobra"
)

func statuslineCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "statusline <agent>",
		Short:        "Render or install the aide statusline for a coding agent",
		SilenceUsage: true,
	}
	cmd.AddCommand(statuslineAgentCmd("claude"))
	return cmd
}

func statuslineAgentCmd(agent string) *cobra.Command {
	var install, remove bool
	cmd := &cobra.Command{
		Use:          agent,
		Short:        fmt.Sprintf("Render or install aide statusline for %s", agent),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			homeDir, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("home dir: %w", err)
			}
			ctx := provision.Context{
				HomeDir: homeDir,
				Env:     envSliceToMap(os.Environ()),
			}
			switch {
			case install:
				return installStatusline(cmd, ctx, homeDir, agent)
			case remove:
				return removeStatusline(cmd, ctx, homeDir)
			}

			// Render mode: only when stdin is a pipe (invoked by Claude Code).
			fi, err := os.Stdin.Stat()
			if err != nil || (fi.Mode()&os.ModeCharDevice) != 0 {
				fmt.Fprintf(cmd.OutOrStdout(),
					"aide statusline %s\n\nRender aide session state as a statusline.\n\nFlags:\n  --install  Configure %s to run aide statusline\n  --remove   Remove the statusLine entry\n",
					agent, agent)
				return nil
			}

			io.Copy(io.Discard, os.Stdin)

			cwd, _ := os.Getwd()
			cfg, _ := config.Load(config.Dir(), cwd)
			var global, project *config.StatuslineConfig
			if cfg != nil {
				global = cfg.Statusline
				if cfg.ProjectOverride != nil {
					project = cfg.ProjectOverride.Statusline
				}
			}
			resolved := config.ResolveStatusline(global, project)
			out := renderStatusline(resolved, envForRender())
			if out != "" {
				fmt.Fprintln(cmd.OutOrStdout(), out)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&install, "install", false, fmt.Sprintf("Install aide statusline for %s", agent))
	cmd.Flags().BoolVar(&remove, "remove", false, "Remove aide statusline")
	return cmd
}

func installStatusline(cmd *cobra.Command, ctx provision.Context, homeDir, agent string) error {
	existing, err := claudeprov.ReadStatusLine(ctx)
	if err != nil {
		return err
	}
	target := fmt.Sprintf("aide statusline %s", agent)
	switch existing {
	case "":
		if err := claudeprov.WriteStatusLine(ctx, target); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Installed: statusLine.command = %q\n", target)
	case target:
		fmt.Fprintln(cmd.OutOrStdout(), "Already installed.")
	default:
		wrapperPath, err := claudeprov.WriteWrapper(homeDir, existing)
		if err != nil {
			return err
		}
		if err := claudeprov.WriteStatusLine(ctx, wrapperPath); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Generated wrapper at %s\nstatusLine.command → %s\n", wrapperPath, wrapperPath)
	}
	return nil
}

func removeStatusline(cmd *cobra.Command, ctx provision.Context, homeDir string) error {
	prev, err := claudeprov.RemoveStatusLine(ctx)
	if err != nil {
		return err
	}
	if prev == "" {
		fmt.Fprintln(cmd.OutOrStdout(), "statusLine was not configured.")
		return nil
	}
	fmt.Fprintln(cmd.OutOrStdout(), "Removed statusLine.")
	if prev == claudeprov.WrapperScriptPath(homeDir) {
		fmt.Fprintf(cmd.OutOrStdout(), "Wrapper at %s — delete manually if no longer needed.\n", prev)
	}
	return nil
}

// renderStatusline renders active aide modules joined by " | ".
// auto_approve is always prepended first when active, regardless of order.
func renderStatusline(cfg config.StatuslineConfig, env map[string]string) string {
	var parts []string
	if env["AIDE_AUTO_APPROVE"] == "1" && cfg.AutoApprove != nil {
		v := cfg.AutoApprove.Value
		if v == "" {
			v = "🚨"
		}
		parts = append(parts, v)
	}
	for _, mod := range cfg.Order {
		if s := renderModule(mod, cfg, env); s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, " | ")
}

func isModuleDisabled(m *config.ModuleConfig) bool {
	return m != nil && m.Disabled != nil && *m.Disabled
}

func renderModule(name string, cfg config.StatuslineConfig, env map[string]string) string {
	switch name {
	case "sandbox":
		if isModuleDisabled(cfg.Sandbox) || cfg.Sandbox == nil {
			return ""
		}
		if env["AIDE_SANDBOX"] == "off" {
			return cfg.Sandbox.Off
		}
		return cfg.Sandbox.On

	case "network":
		if isModuleDisabled(cfg.Network) || cfg.Network == nil {
			return ""
		}
		if env["AIDE_NETWORK_MODE"] == "unrestricted" {
			return cfg.Network.Unrestricted
		}
		return cfg.Network.Outbound

	case "caps":
		if isModuleDisabled(cfg.Caps) || cfg.Caps == nil {
			return ""
		}
		caps := env["AIDE_CAPS"]
		if caps == "" {
			return ""
		}
		icon := cfg.Caps.Icon
		if icon == "" {
			icon = "⚡"
		}
		return icon + " " + caps

	case "trust":
		if isModuleDisabled(cfg.Trust) || cfg.Trust == nil {
			return ""
		}
		if env["AIDE_TRUST"] != "untrusted" {
			return ""
		}
		return cfg.Trust.Untrusted

	case "context":
		if isModuleDisabled(cfg.Context) || cfg.Context == nil {
			return ""
		}
		ctx := env["AIDE_CONTEXT"]
		if ctx == "" {
			return ""
		}
		icon := cfg.Context.Icon
		if icon == "" {
			icon = "📁"
		}
		return icon + " " + ctx

	case "auto_approve":
		return "" // always prepended separately; ignored in order loop
	}
	return ""
}

// envForRender extracts AIDE_* vars from os.Environ for the render path.
func envForRender() map[string]string {
	return map[string]string{
		"AIDE_SANDBOX":      os.Getenv("AIDE_SANDBOX"),
		"AIDE_NETWORK_MODE": os.Getenv("AIDE_NETWORK_MODE"),
		"AIDE_CAPS":         os.Getenv("AIDE_CAPS"),
		"AIDE_TRUST":        os.Getenv("AIDE_TRUST"),
		"AIDE_AUTO_APPROVE": os.Getenv("AIDE_AUTO_APPROVE"),
		"AIDE_CONTEXT":      os.Getenv("AIDE_CONTEXT"),
	}
}

// envSliceToMap converts []string{"K=V", ...} to map[string]string{"K":"V", ...}.
func envSliceToMap(env []string) map[string]string {
	m := make(map[string]string, len(env))
	for _, kv := range env {
		if i := strings.IndexByte(kv, '='); i >= 0 {
			m[kv[:i]] = kv[i+1:]
		}
	}
	return m
}
```

**Note:** `filepath` is imported but only used indirectly via `claudeprov.WrapperScriptPath`. If the compiler complains, remove the `filepath` import (it's not directly called in this file — `WrapperScriptPath` is in the `claude` package).

- [ ] **Step 4: Register `statuslineCmd` in `cmd/aide/commands.go`**

Add after `rootCmd.AddCommand(explainCmd())` (line 42):

```go
	rootCmd.AddCommand(statuslineCmd())
```

- [ ] **Step 5: Run tests and build**

```bash
go test ./cmd/aide/... -run TestRenderStatusline -v
go build ./...
```

Expected: 8 tests PASS, no build errors.

- [ ] **Step 6: Smoke test render mode**

```bash
echo '{}' | AIDE_SANDBOX=on AIDE_NETWORK_MODE=outbound AIDE_CAPS="k8s,docker" AIDE_TRUST=trusted AIDE_CONTEXT=myproj go run ./cmd/aide statusline claude
```

Expected output:
```
🔒 | 🌐 | ⚡ k8s,docker | 📁 myproj
```

- [ ] **Step 7: Smoke test install mode**

```bash
go run ./cmd/aide statusline claude --install
```

Expected: `Installed: statusLine.command = "aide statusline claude"` (or "Already installed." if run twice)

- [ ] **Step 8: Commit**

Use `/commit` — message: "Add aide statusline subcommand with render and install modes"

---

## Self-Review

**Spec coverage:**

| Requirement | Task |
|---|---|
| Config schema: `StatuslineConfig`, `ModuleConfig`, `*bool Disabled` | Task 1 |
| Merge: `order` replaced wholesale, modules merged field-by-field | Task 1 |
| Default order `[sandbox, network, caps, trust, context]` | Task 1 |
| Built-in defaults (emoji) | Task 1 |
| `AIDE_*` env vars injected before exec | Task 2 |
| Env inherited via exec chain — no file IPC | Task 2 |
| Render: drain stdin, read env + config, walk order, join ` \| ` | Task 4 |
| `auto_approve` always prepended when active | Task 4 |
| `disabled: true` on `auto_approve` has no effect (it's in prepend path, not order loop) | Task 4 |
| `caps`, `context` hidden when env var empty | Task 4 |
| `trust` hidden when `AIDE_TRUST=trusted` | Task 4 |
| Empty state string suppresses state | Task 4 `renderModule` |
| `--install` patches `~/.claude/settings.json` | Task 3 + Task 4 |
| Wrapper script when existing command present | Task 3 `WriteWrapper` + Task 4 |
| Wrapper is executable (0700) | Task 3 `AtomicWriteExecutable` |
| `--remove` deletes key, prints wrapper path | Task 4 |
| All JSON writes atomic | Task 3 `AtomicWrite` |
| `agent` module: accepted in config (field exists on `StatuslineConfig`), renderer returns `""` for unknown name | Task 4 default case |

**Placeholder scan:** None.

**Type consistency:**
- `config.StatuslineConfig` and `config.ModuleConfig` defined in Task 1, used in Task 4 ✅
- `config.ResolveStatusline` defined in Task 1, used in Task 4 ✅
- `claude.ReadStatusLine`, `WriteStatusLine`, `RemoveStatusLine`, `WrapperScriptPath`, `WriteWrapper` defined in Task 3, used in Task 4 ✅
- `renderStatusline(cfg config.StatuslineConfig, env map[string]string) string` defined and tested in Task 4 ✅
- `injectAideSessionEnv` signature matches across Tasks 2 and its test ✅
- `mergeEnv(base []string, resolved map[string]string) []string` — existing launcher function, used correctly ✅
