# Statusline Enhancements Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix the misleading "sandboxed" state when aide didn't launch the
session, let each statusline module render independently for ccstatusline
Custom Command widgets, auto-detect the coding agent instead of requiring it
explicitly, ship ccstatusline's sandbox capability as a builtin that
auto-includes itself, and make TTY invocation preview real output instead of
printing help text.

**Architecture:** All render-mode changes live in `cmd/aide/statusline.go`
(existing) plus a new `cmd/aide/statusline_agent.go` for agent-resolution
logic (stdin JSON sniffing, CWD-context lookup, the 5-step resolution
order). The config schema gains one new field (`Unmanaged` on
`ModuleConfig`). The ccstatusline capability is a new entry in
`internal/capability/builtin.go`, auto-included by a small helper in a new
`internal/launcher/ccstatusline.go`, called from `Launcher.Launch` right
where capability names are already merged.

**Tech Stack:** Go, cobra (CLI), existing `internal/config`,
`internal/capability`, `internal/provision`, `internal/launcher` packages.
No new dependencies.

**Spec:** `docs/superpowers/specs/2026-08-07-statusline-enhancements-design.md`

## Global Constraints

- `aide statusline claude` with no new flags must produce byte-identical
  output to today's v1 behavior — this is the explicit backward-compat
  requirement from the spec.
- New module state fields follow the existing "empty string hides it"
  convention already used by `On`/`Off`/`Outbound`/`Unrestricted`/etc.
- Repeatable flags use `StringSliceVar` (matches the existing convention in
  `cmd/aide/cap.go`), not `StringArrayVar`.
- Default icon for the new `Unmanaged` state (both `sandbox` and `network`
  modules) is `"❓"`.
- Every task ends with `go build ./...` and `go test ./...` (or at minimum
  the affected package) passing before commit.

---

### Task 1: Add `Unmanaged` state to the statusline config schema

**Files:**
- Modify: `internal/config/schema.go:842-939` (`StatuslineConfig`,
  `ModuleConfig`, `defaultStatuslineConfig`, `applyModuleOverride`)
- Test: `internal/config/statusline_test.go`

**Interfaces:**
- Produces: `config.ModuleConfig.Unmanaged string` (yaml tag
  `unmanaged,omitempty`), populated with default `"❓"` on `Sandbox` and
  `Network` by `defaultStatuslineConfig()`, merged field-by-field in
  `applyModuleOverride` like every other state field.

- [ ] **Step 1: Write the failing tests**

Add to `internal/config/statusline_test.go`:

```go
func TestResolveStatusline_SandboxUnmanagedDefault(t *testing.T) {
	got := ResolveStatusline(nil, nil)
	if got.Sandbox == nil || got.Sandbox.Unmanaged != "❓" {
		t.Errorf("Sandbox.Unmanaged = %q, want ❓", got.Sandbox.Unmanaged)
	}
}

func TestResolveStatusline_NetworkUnmanagedDefault(t *testing.T) {
	got := ResolveStatusline(nil, nil)
	if got.Network == nil || got.Network.Unmanaged != "❓" {
		t.Errorf("Network.Unmanaged = %q, want ❓", got.Network.Unmanaged)
	}
}

func TestResolveStatusline_ProjectOverridesUnmanaged(t *testing.T) {
	project := &StatuslineConfig{
		Sandbox: &ModuleConfig{Unmanaged: "🚫"},
	}
	got := ResolveStatusline(nil, project)
	if got.Sandbox.Unmanaged != "🚫" {
		t.Errorf("Sandbox.Unmanaged = %q, want 🚫", got.Sandbox.Unmanaged)
	}
	if got.Sandbox.On != "🔒" {
		t.Errorf("Sandbox.On = %q, want default 🔒 (unchanged)", got.Sandbox.On)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/config/... -run TestResolveStatusline_.*Unmanaged -v`
Expected: FAIL — `ModuleConfig` has no field `Unmanaged`, compile error.

- [ ] **Step 3: Add the field and wire it through**

In `internal/config/schema.go`, add `Unmanaged` to `ModuleConfig` (after
`Untrusted`, before `Value`):

```go
type ModuleConfig struct {
	Disabled     *bool  `yaml:"disabled,omitempty"`
	Icon         string `yaml:"icon,omitempty"`
	On           string `yaml:"on,omitempty"`
	Off          string `yaml:"off,omitempty"`
	Outbound     string `yaml:"outbound,omitempty"`
	Unrestricted string `yaml:"unrestricted,omitempty"`
	Untrusted    string `yaml:"untrusted,omitempty"`
	Unmanaged    string `yaml:"unmanaged,omitempty"`
	Value        string `yaml:"value,omitempty"`
}
```

Update `defaultStatuslineConfig()`:

```go
func defaultStatuslineConfig() StatuslineConfig {
	return StatuslineConfig{
		Order:       []string{"sandbox", "network", "caps", "trust", "context"},
		Sandbox:     &ModuleConfig{On: "🔒", Off: "🔓", Unmanaged: "❓"},
		Network:     &ModuleConfig{Outbound: "🌐", Unrestricted: "🌍", Unmanaged: "❓"},
		Caps:        &ModuleConfig{Icon: "⚡"},
		Trust:       &ModuleConfig{Untrusted: "⚠️"},
		Context:     &ModuleConfig{Icon: "📁"},
		AutoApprove: &ModuleConfig{Value: "🚨"},
	}
}
```

Update `applyModuleOverride` to merge the new field (add after the
`Untrusted` block):

```go
		if src.Untrusted != "" {
			d.Untrusted = src.Untrusted
		}
		if src.Unmanaged != "" {
			d.Unmanaged = src.Unmanaged
		}
		if src.Value != "" {
			d.Value = src.Value
		}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/config/... -v`
Expected: PASS, all tests including the 3 new ones and the existing
`TestResolveStatuline_*` suite.

- [ ] **Step 5: Commit**

```bash
git add internal/config/schema.go internal/config/statusline_test.go
git commit -m "Add unmanaged state field to statusline module config"
```

---

### Task 2: Distinguish "env var absent" from "env var off" in sandbox/network rendering

**Files:**
- Modify: `cmd/aide/statusline.go` (`renderModule`, `envForRender`)
- Test: `cmd/aide/statusline_test.go`

**Interfaces:**
- Consumes: `config.ModuleConfig.Unmanaged` (Task 1).
- Produces: `renderModule` now returns `cfg.Sandbox.Unmanaged` /
  `cfg.Network.Unmanaged` when the corresponding env var key is absent from
  the map (not merely empty-valued). `envForRender()` now omits keys for
  unset env vars entirely instead of inserting `""`.

- [ ] **Step 1: Write the failing tests**

Add to `cmd/aide/statusline_test.go`:

```go
func TestRenderStatusline_SandboxUnmanagedWhenEnvAbsent(t *testing.T) {
	cfg := config.ResolveStatusline(nil, nil)
	env := map[string]string{
		"AIDE_NETWORK_MODE": "outbound",
		"AIDE_CAPS":         "",
		"AIDE_TRUST":        "trusted",
	}
	got := renderStatusline(cfg, env)
	want := "❓ | 🌐"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRenderStatusline_NetworkUnmanagedWhenEnvAbsent(t *testing.T) {
	cfg := config.ResolveStatusline(nil, nil)
	env := map[string]string{
		"AIDE_SANDBOX": "on",
		"AIDE_CAPS":    "",
		"AIDE_TRUST":   "trusted",
	}
	got := renderStatusline(cfg, env)
	want := "🔒 | ❓"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRenderStatusline_BothUnmanagedWhenNoAideEnvAtAll(t *testing.T) {
	cfg := config.ResolveStatusline(nil, nil)
	got := renderStatusline(cfg, map[string]string{})
	want := "❓ | ❓"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestEnvForRender_AbsentVarOmittedFromMap(t *testing.T) {
	t.Setenv("AIDE_SANDBOX", "on")
	if orig, ok := os.LookupEnv("AIDE_NETWORK_MODE"); ok {
		os.Unsetenv("AIDE_NETWORK_MODE")
		t.Cleanup(func() { os.Setenv("AIDE_NETWORK_MODE", orig) })
	}
	env := envForRender()
	if _, ok := env["AIDE_SANDBOX"]; !ok {
		t.Error("AIDE_SANDBOX should be present in map when set")
	}
	if _, ok := env["AIDE_NETWORK_MODE"]; ok {
		t.Error("AIDE_NETWORK_MODE should be absent from map when unset")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./cmd/aide/... -run 'TestRenderStatusline_.*Unmanaged|TestEnvForRender_AbsentVarOmittedFromMap' -v`
Expected: FAIL — sandbox/network still default to on/outbound when the key
is absent (map lookup returns `""`, which today's code treats as "not
off"/"not unrestricted").

- [ ] **Step 3: Implement the presence-aware logic**

In `cmd/aide/statusline.go`, replace the `"sandbox"` and `"network"` cases
inside `renderModule`:

```go
	case "sandbox":
		if isModuleDisabled(cfg.Sandbox) || cfg.Sandbox == nil {
			return ""
		}
		v, ok := env["AIDE_SANDBOX"]
		switch {
		case !ok:
			return cfg.Sandbox.Unmanaged
		case v == "off":
			return cfg.Sandbox.Off
		default:
			return cfg.Sandbox.On
		}

	case "network":
		if isModuleDisabled(cfg.Network) || cfg.Network == nil {
			return ""
		}
		v, ok := env["AIDE_NETWORK_MODE"]
		switch {
		case !ok:
			return cfg.Network.Unmanaged
		case v == "unrestricted":
			return cfg.Network.Unrestricted
		default:
			return cfg.Network.Outbound
		}
```

Replace `envForRender()`:

```go
// envForRender extracts AIDE_* vars from the process environment for the
// render path. Keys are omitted entirely when the underlying env var is
// unset, distinguishing "absent" (aide didn't launch this session) from
// "present but empty" — renderModule relies on this distinction for the
// sandbox/network unmanaged state.
func envForRender() map[string]string {
	env := map[string]string{}
	for _, k := range []string{
		"AIDE_SANDBOX", "AIDE_NETWORK_MODE", "AIDE_CAPS",
		"AIDE_TRUST", "AIDE_AUTO_APPROVE", "AIDE_CONTEXT",
	} {
		if v, ok := os.LookupEnv(k); ok {
			env[k] = v
		}
	}
	return env
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./cmd/aide/... -v`
Expected: PASS — new tests pass, and all pre-existing
`TestRenderStatusline_*` tests still pass unchanged (they always set
`AIDE_SANDBOX`/`AIDE_NETWORK_MODE` explicitly, so `ok` is always `true` for
them).

- [ ] **Step 5: Commit**

```bash
git add cmd/aide/statusline.go cmd/aide/statusline_test.go
git commit -m "Render unmanaged state when aide env vars are absent"
```

---

### Task 3: Add `renderStatuslineModules` for filtered per-module output

**Files:**
- Modify: `cmd/aide/statusline.go`
- Test: `cmd/aide/statusline_test.go`

**Interfaces:**
- Consumes: `renderStatusline(cfg, env)`, `renderModule(name, cfg, env)`
  (existing).
- Produces: `renderStatuslineModules(cfg config.StatuslineConfig, env
  map[string]string, modules []string) string` — empty `modules` behaves
  exactly like `renderStatusline`; non-empty `modules` filters `cfg.Order`
  to the requested subset (preserving configured order, not flag order),
  joined by `" | "`. `auto_approve` only renders when explicitly requested
  in `modules`.

- [ ] **Step 1: Write the failing tests**

Add to `cmd/aide/statusline_test.go`:

```go
func TestRenderStatuslineModules_SingleModuleBareOutput(t *testing.T) {
	cfg := config.ResolveStatusline(nil, nil)
	env := map[string]string{
		"AIDE_SANDBOX":      "on",
		"AIDE_NETWORK_MODE": "outbound",
		"AIDE_CAPS":         "k8s,docker",
		"AIDE_TRUST":        "trusted",
	}
	got := renderStatuslineModules(cfg, env, []string{"caps"})
	want := "⚡ k8s,docker"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRenderStatuslineModules_MultipleModulesPreserveConfiguredOrder(t *testing.T) {
	cfg := config.ResolveStatusline(nil, nil)
	env := map[string]string{
		"AIDE_SANDBOX":      "on",
		"AIDE_NETWORK_MODE": "outbound",
		"AIDE_CAPS":         "",
		"AIDE_TRUST":        "trusted",
	}
	// Requested as network,sandbox but cfg.Order is sandbox,network,...
	got := renderStatuslineModules(cfg, env, []string{"network", "sandbox"})
	want := "🔒 | 🌐"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRenderStatuslineModules_EmptyModulesIsFullCombinedOutput(t *testing.T) {
	cfg := config.ResolveStatusline(nil, nil)
	env := map[string]string{
		"AIDE_SANDBOX":      "on",
		"AIDE_NETWORK_MODE": "outbound",
		"AIDE_CAPS":         "k8s",
		"AIDE_TRUST":        "trusted",
	}
	got := renderStatuslineModules(cfg, env, nil)
	want := renderStatusline(cfg, env)
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRenderStatuslineModules_AutoApproveOnlyWhenRequested(t *testing.T) {
	cfg := config.ResolveStatusline(nil, nil)
	env := map[string]string{
		"AIDE_SANDBOX":      "on",
		"AIDE_NETWORK_MODE": "outbound",
		"AIDE_AUTO_APPROVE": "1",
	}
	got := renderStatuslineModules(cfg, env, []string{"auto_approve"})
	want := "🚨"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	gotSandboxOnly := renderStatuslineModules(cfg, env, []string{"sandbox"})
	wantSandboxOnly := "🔒"
	if gotSandboxOnly != wantSandboxOnly {
		t.Errorf("got %q, want %q (auto_approve must not leak in when not requested)", gotSandboxOnly, wantSandboxOnly)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./cmd/aide/... -run TestRenderStatuslineModules -v`
Expected: FAIL — `renderStatuslineModules` undefined, compile error.

- [ ] **Step 3: Implement `renderStatuslineModules`**

Add to `cmd/aide/statusline.go`, after `renderStatusline`:

```go
// renderStatuslineModules renders only the requested modules, in cfg.Order
// (not the order modules were requested in), joined by " | ". Empty
// modules renders the full combined output, identical to renderStatusline.
func renderStatuslineModules(cfg config.StatuslineConfig, env map[string]string, modules []string) string {
	if len(modules) == 0 {
		return renderStatusline(cfg, env)
	}
	want := make(map[string]bool, len(modules))
	for _, m := range modules {
		want[m] = true
	}
	var parts []string
	if want["auto_approve"] && env["AIDE_AUTO_APPROVE"] == "1" && cfg.AutoApprove != nil {
		v := cfg.AutoApprove.Value
		if v == "" {
			v = "🚨"
		}
		parts = append(parts, v)
	}
	for _, mod := range cfg.Order {
		if !want[mod] {
			continue
		}
		if s := renderModule(mod, cfg, env); s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, " | ")
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./cmd/aide/... -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/aide/statusline.go cmd/aide/statusline_test.go
git commit -m "Add renderStatuslineModules for filtered module output"
```

---

### Task 4: Stdin JSON sniff helper for Claude Code detection

**Files:**
- Create: `cmd/aide/statusline_agent.go`
- Test: `cmd/aide/statusline_agent_test.go`

**Interfaces:**
- Produces: `looksLikeClaudeStatuslineJSON(data []byte) bool`.

- [ ] **Step 1: Write the failing tests**

Create `cmd/aide/statusline_agent_test.go`:

```go
package main

import "testing"

func TestLooksLikeClaudeStatuslineJSON_MatchesRealShape(t *testing.T) {
	data := []byte(`{"session_id":"abc123","model":{"id":"claude-3","display_name":"Claude"},"workspace":{"current_dir":"/tmp"}}`)
	if !looksLikeClaudeStatuslineJSON(data) {
		t.Error("expected match for Claude Code statusline JSON shape")
	}
}

func TestLooksLikeClaudeStatuslineJSON_MatchesSessionIDOnly(t *testing.T) {
	data := []byte(`{"session_id":"abc123"}`)
	if !looksLikeClaudeStatuslineJSON(data) {
		t.Error("expected match on session_id alone")
	}
}

func TestLooksLikeClaudeStatuslineJSON_RejectsUnrelatedJSON(t *testing.T) {
	data := []byte(`{"foo":"bar"}`)
	if looksLikeClaudeStatuslineJSON(data) {
		t.Error("expected no match for unrelated JSON")
	}
}

func TestLooksLikeClaudeStatuslineJSON_RejectsEmptyOrInvalid(t *testing.T) {
	if looksLikeClaudeStatuslineJSON(nil) {
		t.Error("expected no match for nil input")
	}
	if looksLikeClaudeStatuslineJSON([]byte("not json")) {
		t.Error("expected no match for invalid JSON")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./cmd/aide/... -run TestLooksLikeClaudeStatuslineJSON -v`
Expected: FAIL — `looksLikeClaudeStatuslineJSON` undefined, compile error.

- [ ] **Step 3: Implement the helper**

Create `cmd/aide/statusline_agent.go`:

```go
package main

import "encoding/json"

// claudeStatuslineProbe matches enough of the JSON Claude Code sends to
// statusLine.command on stdin to identify the caller as Claude Code,
// without committing to its full schema.
type claudeStatuslineProbe struct {
	SessionID string          `json:"session_id"`
	Model     json.RawMessage `json:"model"`
	Workspace json.RawMessage `json:"workspace"`
}

// looksLikeClaudeStatuslineJSON reports whether data matches Claude Code's
// statusline JSON shape closely enough to identify the caller as claude.
// Used when AIDE_AGENT is unset (statusline invoked outside an
// aide-launched session) and stdin is piped.
func looksLikeClaudeStatuslineJSON(data []byte) bool {
	var probe claudeStatuslineProbe
	if err := json.Unmarshal(data, &probe); err != nil {
		return false
	}
	return probe.SessionID != "" || len(probe.Model) > 0 || len(probe.Workspace) > 0
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./cmd/aide/... -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/aide/statusline_agent.go cmd/aide/statusline_agent_test.go
git commit -m "Add stdin JSON sniff for Claude Code detection"
```

---

### Task 5: CWD-context agent lookup helper (TTY fallback)

**Files:**
- Modify: `cmd/aide/statusline_agent.go`
- Test: `cmd/aide/statusline_agent_test.go`

**Interfaces:**
- Consumes: `resolveContextForMutation(contextName string) (*config.Config,
  string, config.Context, error)` (existing, `cmd/aide/commands.go:129`).
- Produces: `contextAgentForCWD() string` — returns `""` on any resolution
  failure.

- [ ] **Step 1: Write the failing tests**

Add to `cmd/aide/statusline_agent_test.go`:

```go
func writeContextMatchingCWD(t *testing.T, dir, contextName, agent string) {
	t.Helper()
	var b strings.Builder
	b.WriteString("contexts:\n  ")
	b.WriteString(contextName)
	b.WriteString(":\n    agent: ")
	b.WriteString(agent)
	b.WriteString("\n    match:\n      - path: ")
	b.WriteString(dir)
	b.WriteString("\n")
	path := filepath.Join(dir, "xdg", "aide", "config.yaml")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestContextAgentForCWD_ResolvesFromMatchingContext(t *testing.T) {
	dir := isolatedConfigDir(t)
	writeContextMatchingCWD(t, dir, "work", "gemini")
	got := contextAgentForCWD()
	if got != "gemini" {
		t.Errorf("got %q, want gemini", got)
	}
}

func TestContextAgentForCWD_NoConfigReturnsEmpty(t *testing.T) {
	isolatedConfigDir(t) // no config.yaml written
	got := contextAgentForCWD()
	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
}
```

Add imports to `cmd/aide/statusline_agent_test.go`: `os`, `path/filepath`,
`strings`, `testing` (all already used by other test files in this
package — `isolatedConfigDir` is defined in `context_bind_test.go`, same
package `main`, no import needed for it).

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./cmd/aide/... -run TestContextAgentForCWD -v`
Expected: FAIL — `contextAgentForCWD` undefined, compile error.

- [ ] **Step 3: Implement the helper**

Add to `cmd/aide/statusline_agent.go`:

```go
// contextAgentForCWD resolves the agent configured for the aide context
// matching the current working directory. Used as a TTY-preview fallback
// when no explicit agent, AIDE_AGENT, or stdin JSON signal is available.
// Returns "" on any resolution failure (no config, no matching context) so
// callers can fall back further.
func contextAgentForCWD() string {
	_, _, ctx, err := resolveContextForMutation("")
	if err != nil {
		return ""
	}
	return ctx.Agent
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./cmd/aide/... -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/aide/statusline_agent.go cmd/aide/statusline_agent_test.go
git commit -m "Add CWD-context agent lookup for statusline TTY preview"
```

---

### Task 6: `resolveStatuslineAgent` — the 5-step resolution order

**Files:**
- Modify: `cmd/aide/statusline_agent.go`
- Test: `cmd/aide/statusline_agent_test.go`

**Interfaces:**
- Consumes: `looksLikeClaudeStatuslineJSON` (Task 4), `contextAgentForCWD`
  (Task 5).
- Produces: `resolveStatuslineAgent(explicitAgent string, isTTY bool,
  stdinData []byte) string` — always returns a non-empty agent name.

- [ ] **Step 1: Write the failing tests**

Add to `cmd/aide/statusline_agent_test.go`:

```go
func unsetAideAgent(t *testing.T) {
	t.Helper()
	if orig, ok := os.LookupEnv("AIDE_AGENT"); ok {
		os.Unsetenv("AIDE_AGENT")
		t.Cleanup(func() { os.Setenv("AIDE_AGENT", orig) })
	}
}

func TestResolveStatuslineAgent_ExplicitWinsOverEverything(t *testing.T) {
	t.Setenv("AIDE_AGENT", "gemini")
	got := resolveStatuslineAgent("claude", false, []byte(`{"session_id":"x"}`))
	if got != "claude" {
		t.Errorf("got %q, want claude", got)
	}
}

func TestResolveStatuslineAgent_EnvWinsOverSniffAndDefault(t *testing.T) {
	t.Setenv("AIDE_AGENT", "gemini")
	got := resolveStatuslineAgent("", false, []byte(`{"session_id":"x"}`))
	if got != "gemini" {
		t.Errorf("got %q, want gemini", got)
	}
}

func TestResolveStatuslineAgent_SniffOnlyWhenPiped(t *testing.T) {
	unsetAideAgent(t)
	got := resolveStatuslineAgent("", false, []byte(`{"session_id":"x"}`))
	if got != "claude" {
		t.Errorf("got %q, want claude (sniffed from piped JSON)", got)
	}
}

func TestResolveStatuslineAgent_SniffIgnoredOnTTY(t *testing.T) {
	unsetAideAgent(t)
	isolatedConfigDir(t) // no matching context either
	got := resolveStatuslineAgent("", true, []byte(`{"session_id":"x"}`))
	if got != "claude" {
		t.Errorf("got %q, want claude (default, not from stdin sniff on TTY)", got)
	}
}

func TestResolveStatuslineAgent_TTYUsesContextWhenAvailable(t *testing.T) {
	unsetAideAgent(t)
	dir := isolatedConfigDir(t)
	writeContextMatchingCWD(t, dir, "work", "gemini")
	got := resolveStatuslineAgent("", true, nil)
	if got != "gemini" {
		t.Errorf("got %q, want gemini", got)
	}
}

func TestResolveStatuslineAgent_DefaultsToClaudeWhenNothingResolves(t *testing.T) {
	unsetAideAgent(t)
	isolatedConfigDir(t)
	got := resolveStatuslineAgent("", true, nil)
	if got != "claude" {
		t.Errorf("got %q, want claude default", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./cmd/aide/... -run TestResolveStatuslineAgent -v`
Expected: FAIL — `resolveStatuslineAgent` undefined, compile error.

- [ ] **Step 3: Implement the resolution order**

Add to `cmd/aide/statusline_agent.go`:

```go
import "os"
```

(merge into the existing `import "encoding/json"` block as a group)

```go
// resolveStatuslineAgent picks the coding agent for statusline rendering.
// Order: explicit flag/positional, AIDE_AGENT env (aide-launched session),
// stdin JSON shape (piped mode only, identifies Claude Code), CWD-matched
// aide context (TTY preview only), then "claude" as the final default —
// the only agent with statusline rendering support today.
func resolveStatuslineAgent(explicitAgent string, isTTY bool, stdinData []byte) string {
	if explicitAgent != "" {
		return explicitAgent
	}
	if v := os.Getenv("AIDE_AGENT"); v != "" {
		return v
	}
	if !isTTY && looksLikeClaudeStatuslineJSON(stdinData) {
		return "claude"
	}
	if isTTY {
		if a := contextAgentForCWD(); a != "" {
			return a
		}
	}
	return "claude"
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./cmd/aide/... -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/aide/statusline_agent.go cmd/aide/statusline_agent_test.go
git commit -m "Add 5-step agent resolution order for statusline rendering"
```

---

### Task 7: Wire `--module`/`--agent` flags, TTY preview, and bare `aide statusline` into the CLI

**Files:**
- Modify: `cmd/aide/statusline.go` (`statuslineCmd`, `statuslineAgentCmd`)
- Test: `cmd/aide/statusline_test.go`

**Interfaces:**
- Consumes: `renderStatuslineModules` (Task 3), `resolveStatuslineAgent`
  (Task 6), `installStatusline`/`removeStatusline` (existing, unchanged).
- Produces: `aide statusline` (bare, auto-detects agent), `aide statusline
  --agent <name>`, `aide statusline --module <name>` (repeatable, works on
  both the bare command and `aide statusline claude`), TTY invocation now
  renders a preview instead of printing help text.

- [ ] **Step 1: Write the failing tests**

Add to `cmd/aide/statusline_test.go`:

```go
// withPipedStdin replaces os.Stdin with a pipe pre-loaded with data,
// restoring the original on test cleanup. Simulates Claude Code piping
// statusline JSON to aide statusline's render mode.
func withPipedStdin(t *testing.T, data string) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = orig })
	go func() {
		w.WriteString(data)
		w.Close()
	}()
}

func TestRunStatusline_BareCommandAutoDetectsAndRenders(t *testing.T) {
	t.Setenv("AIDE_SANDBOX", "on")
	t.Setenv("AIDE_NETWORK_MODE", "outbound")
	withPipedStdin(t, `{"session_id":"abc"}`)

	cmd := statuslineCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v\n%s", err, buf.String())
	}
	got := strings.TrimSpace(buf.String())
	want := "🔒 | 🌐"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRunStatusline_ModuleFlagRepeatable(t *testing.T) {
	t.Setenv("AIDE_SANDBOX", "on")
	t.Setenv("AIDE_NETWORK_MODE", "outbound")
	withPipedStdin(t, `{"session_id":"abc"}`)

	cmd := statuslineCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--module", "sandbox", "--module", "network"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v\n%s", err, buf.String())
	}
	got := strings.TrimSpace(buf.String())
	want := "🔒 | 🌐"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRunStatusline_UnsupportedAgentErrors(t *testing.T) {
	withPipedStdin(t, `{"session_id":"abc"}`)
	cmd := statuslineCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--agent", "gemini"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for unsupported agent")
	}
	if !strings.Contains(err.Error(), "gemini") {
		t.Errorf("error should mention gemini, got: %v", err)
	}
}

func TestRunStatusline_ClaudeSubcommandStillWorksUnmodified(t *testing.T) {
	t.Setenv("AIDE_SANDBOX", "off")
	t.Setenv("AIDE_NETWORK_MODE", "unrestricted")
	withPipedStdin(t, `{}`)

	cmd := statuslineAgentCmd("claude")
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v\n%s", err, buf.String())
	}
	got := strings.TrimSpace(buf.String())
	want := "🔓 | 🌍"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRunStatusline_TTYRendersPreviewInsteadOfHelpText(t *testing.T) {
	t.Setenv("AIDE_SANDBOX", "on")
	t.Setenv("AIDE_NETWORK_MODE", "outbound")
	// os.Stdin defaults to the test process's real stdin, which go test
	// runs with a non-TTY (piped/redirected) stdin — simulate the TTY
	// path explicitly by pointing os.Stdin at a closed pipe end that
	// still reports as a char device is impractical in-process, so this
	// test instead verifies the behavioral contract directly:
	// resolveStatuslineAgent + renderStatuslineModules compose correctly
	// for the TTY branch (isTTY=true), which is exercised in Task 6's
	// unit tests. This test only asserts the old help-text branch is gone.
	cmd := statuslineAgentCmd("claude")
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{})
	withPipedStdin(t, `{}`)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v\n%s", err, buf.String())
	}
	if strings.Contains(buf.String(), "Render aide session state as a statusline") {
		t.Error("old help text should no longer be printed")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./cmd/aide/... -run 'TestRunStatusline_' -v`
Expected: FAIL — `--module`/`--agent` flags don't exist on `statuslineCmd`,
bare command has no RunE, "gemini" unsupported-agent error doesn't exist.

- [ ] **Step 3: Rewrite `statuslineCmd` and `statuslineAgentCmd`**

Replace the top of `cmd/aide/statusline.go` (from `func statuslineCmd()`
through the end of `removeStatusline`) with:

```go
func statuslineCmd() *cobra.Command {
	var agent string
	var modules []string
	var install, remove bool
	var contextName string
	cmd := &cobra.Command{
		Use:          "statusline [agent]",
		Short:        "Render or install the aide statusline for a coding agent",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if install || remove {
				if agent == "" {
					return fmt.Errorf(`--agent is required with --install/--remove (or use "aide statusline <agent> --install")`)
				}
				return runStatuslineInstallRemove(cmd, agent, install, remove, contextName)
			}
			return runStatuslineRender(cmd, agent, modules)
		},
	}
	cmd.Flags().StringVar(&agent, "agent", "", "Coding agent (default: auto-detected)")
	cmd.Flags().StringSliceVar(&modules, "module", nil, "Render only these modules (repeatable)")
	cmd.Flags().BoolVar(&install, "install", false, "Install aide statusline")
	cmd.Flags().BoolVar(&remove, "remove", false, "Remove aide statusline")
	cmd.Flags().StringVar(&contextName, "context", "", "Context name (default: matched by CWD)")
	cmd.AddCommand(statuslineAgentCmd("claude"))
	return cmd
}

func statuslineAgentCmd(agent string) *cobra.Command {
	var install, remove bool
	var contextName string
	var modules []string
	cmd := &cobra.Command{
		Use:          agent,
		Short:        fmt.Sprintf("Render or install aide statusline for %s", agent),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if install || remove {
				return runStatuslineInstallRemove(cmd, agent, install, remove, contextName)
			}
			return runStatuslineRender(cmd, agent, modules)
		},
	}
	cmd.Flags().BoolVar(&install, "install", false, fmt.Sprintf("Install aide statusline for %s", agent))
	cmd.Flags().BoolVar(&remove, "remove", false, "Remove aide statusline")
	cmd.Flags().StringVar(&contextName, "context", "", "Context name (default: matched by CWD)")
	cmd.Flags().StringSliceVar(&modules, "module", nil, "Render only these modules (repeatable)")
	return cmd
}

// runStatuslineInstallRemove resolves the target agent's context and
// dispatches to installStatusline/removeStatusline. Shared by the bare
// `aide statusline --agent X --install` form and the explicit
// `aide statusline X --install` subcommand form.
func runStatuslineInstallRemove(cmd *cobra.Command, agent string, install, remove bool, contextName string) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("home dir: %w", err)
	}
	cfg, name, cfgCtx, err := resolveContextForMutation(contextName)
	_ = cfg
	if err != nil {
		return err
	}
	cwd, _ := os.Getwd()
	pCtx, err := provision.ResolveContext(name, cfgCtx, homeDir, cwd, resolveContextEnv(cfgCtx, homeDir))
	if err != nil {
		return err
	}
	if install {
		return installStatusline(cmd, pCtx, homeDir, agent)
	}
	return removeStatusline(cmd, pCtx, homeDir)
}

// runStatuslineRender resolves the agent and renders the requested modules
// (or the full combined output when modules is empty) to stdout. Runs
// identically whether stdin is piped (Claude Code invoking it on every
// update) or a TTY (a human previewing the statusline directly) — the only
// difference is how the agent gets resolved (see resolveStatuslineAgent).
func runStatuslineRender(cmd *cobra.Command, explicitAgent string, modules []string) error {
	fi, statErr := os.Stdin.Stat()
	isTTY := statErr != nil || (fi.Mode()&os.ModeCharDevice) != 0

	var stdinData []byte
	if !isTTY {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("reading stdin: %w", err)
		}
		stdinData = data
	}

	agent := resolveStatuslineAgent(explicitAgent, isTTY, stdinData)
	if agent != "claude" {
		return fmt.Errorf("statusline rendering not yet supported for agent %q (only claude is supported)", agent)
	}

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
	out := renderStatuslineModules(resolved, envForRender(), modules)
	if out != "" {
		fmt.Fprintln(cmd.OutOrStdout(), out)
	}
	return nil
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
```

Leave `renderStatusline`, `renderStatuslineModules`, `isModuleDisabled`,
`renderModule`, and `envForRender` exactly as they are after Tasks 2 and 3
— only the command-construction and dispatch functions above change.

`installStatusline`/`removeStatusline` are reproduced unchanged (they
already existed) — this step is a straight cut-and-paste for them; the
actual behavior change is confined to `statuslineCmd`,
`statuslineAgentCmd`, and the two new `runStatusline*` functions.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./cmd/aide/... -v`
Expected: PASS — all new tests, plus every pre-existing
`TestStatuslineInstall_*`/`TestStatuslineRemove_*` test (they call
`statuslineAgentCmd("claude")` directly with `--install`/`--remove`, whose
behavior is unchanged).

- [ ] **Step 5: Commit**

```bash
git add cmd/aide/statusline.go cmd/aide/statusline_test.go
git commit -m "Add auto-detected agent, --module flag, and TTY preview to statusline"
```

---

### Task 8: Ship `ccstatusline` as a builtin capability

**Files:**
- Modify: `internal/capability/builtin.go`
- Test: `internal/capability/builtin_test.go`

**Interfaces:**
- Produces: `Builtins()["ccstatusline"]` with `Readable:
  []string{"~/.config/ccstatusline/settings.json"}`.

- [ ] **Step 1: Write the failing tests**

Add to `internal/capability/builtin_test.go`:

```go
func TestBuiltin_Ccstatusline_Exists(t *testing.T) {
	c, ok := Builtins()["ccstatusline"]
	if !ok {
		t.Fatal("missing built-in capability 'ccstatusline'")
	}
	if c.Description == "" {
		t.Error("expected non-empty description")
	}
	wantReadable := []string{"~/.config/ccstatusline/settings.json"}
	if !reflect.DeepEqual(c.Readable, wantReadable) {
		t.Errorf("Readable = %v, want %v", c.Readable, wantReadable)
	}
}
```

Update the existing count assertion in the same file:

```go
func TestBuiltins_Count(t *testing.T) {
	if len(Builtins()) != 23 {
		t.Errorf("expected 23 built-in capabilities, got %d", len(Builtins()))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/capability/... -run 'TestBuiltin_Ccstatusline_Exists|TestBuiltins_Count' -v`
Expected: FAIL — capability doesn't exist yet; count is 22.

- [ ] **Step 3: Add the builtin capability**

In `internal/capability/builtin.go`, add a new entry inside the `init()`
map, right before the `"network"` entry (after `"clipboard"`):

```go
		"ccstatusline": {
			Name:        "ccstatusline",
			Description: "Beautiful highly customizable statusline for Claude Code CLI with powerline support, themes, and more.",
			Readable:    []string{"~/.config/ccstatusline/settings.json"},
		},
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/capability/... -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/capability/builtin.go internal/capability/builtin_test.go
git commit -m "Add ccstatusline builtin capability"
```

---

### Task 9: Auto-include `ccstatusline` when the tool is installed

**Files:**
- Create: `internal/launcher/ccstatusline.go`
- Test: `internal/launcher/ccstatusline_test.go`
- Modify: `internal/launcher/launcher.go:371`

**Interfaces:**
- Consumes: `homepath.Expand(path, home string) string` (existing,
  `internal/homepath/homepath.go:17`).
- Produces: `autoIncludeCcstatusline(capNames, withoutCaps []string,
  homeDir string) []string`, called from `Launcher.Launch` right after
  `sandbox.MergeCapNames`.

- [ ] **Step 1: Write the failing tests**

Create `internal/launcher/ccstatusline_test.go`:

```go
package launcher

import (
	"os"
	"path/filepath"
	"testing"
)

func writeCcstatuslineSettings(t *testing.T, homeDir string) {
	t.Helper()
	dir := filepath.Join(homeDir, ".config", "ccstatusline")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestAutoIncludeCcstatusline_AddsWhenSettingsFileExists(t *testing.T) {
	homeDir := t.TempDir()
	writeCcstatuslineSettings(t, homeDir)
	got := autoIncludeCcstatusline([]string{"k8s"}, nil, homeDir)
	if len(got) != 2 || got[0] != "k8s" || got[1] != "ccstatusline" {
		t.Errorf("got %v, want [k8s ccstatusline]", got)
	}
}

func TestAutoIncludeCcstatusline_NoOpWhenSettingsFileMissing(t *testing.T) {
	homeDir := t.TempDir()
	got := autoIncludeCcstatusline([]string{"k8s"}, nil, homeDir)
	if len(got) != 1 || got[0] != "k8s" {
		t.Errorf("got %v, want [k8s]", got)
	}
}

func TestAutoIncludeCcstatusline_NoOpWhenAlreadyPresent(t *testing.T) {
	homeDir := t.TempDir()
	writeCcstatuslineSettings(t, homeDir)
	got := autoIncludeCcstatusline([]string{"ccstatusline"}, nil, homeDir)
	if len(got) != 1 || got[0] != "ccstatusline" {
		t.Errorf("got %v, want [ccstatusline] (not duplicated)", got)
	}
}

func TestAutoIncludeCcstatusline_RespectsExplicitExclusion(t *testing.T) {
	homeDir := t.TempDir()
	writeCcstatuslineSettings(t, homeDir)
	got := autoIncludeCcstatusline([]string{"k8s"}, []string{"ccstatusline"}, homeDir)
	if len(got) != 1 || got[0] != "k8s" {
		t.Errorf("got %v, want [k8s] (explicit --without honored)", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/launcher/... -run TestAutoIncludeCcstatusline -v`
Expected: FAIL — `autoIncludeCcstatusline` undefined, compile error.

- [ ] **Step 3: Implement the helper and wire it into `Launch`**

Create `internal/launcher/ccstatusline.go`:

```go
package launcher

import (
	"os"

	"github.com/jskswamy/aide/internal/homepath"
)

const ccstatuslineSettingsPath = "~/.config/ccstatusline/settings.json"

// autoIncludeCcstatusline adds the "ccstatusline" capability to capNames
// when the tool's settings file exists on disk, so a sandboxed agent
// process invoking `aide statusline claude` from a ccstatusline Custom
// Command widget can read its config. No-ops when the capability is
// already present, was explicitly excluded via --without, or the settings
// file doesn't exist (ccstatusline isn't installed/configured).
func autoIncludeCcstatusline(capNames, withoutCaps []string, homeDir string) []string {
	for _, c := range capNames {
		if c == "ccstatusline" {
			return capNames
		}
	}
	for _, c := range withoutCaps {
		if c == "ccstatusline" {
			return capNames
		}
	}
	if _, err := os.Stat(homepath.Expand(ccstatuslineSettingsPath, homeDir)); err != nil {
		return capNames
	}
	return append(capNames, "ccstatusline")
}
```

In `internal/launcher/launcher.go`, change line 371 from:

```go
	capNames := sandbox.MergeCapNames(rc.Context.Capabilities, withCaps, withoutCaps)
```

to:

```go
	capNames := sandbox.MergeCapNames(rc.Context.Capabilities, withCaps, withoutCaps)
	capNames = autoIncludeCcstatusline(capNames, withoutCaps, homeDir)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/launcher/... -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/launcher/ccstatusline.go internal/launcher/ccstatusline_test.go internal/launcher/launcher.go
git commit -m "Auto-include ccstatusline capability when settings file exists"
```

---

### Task 10: Full-suite verification

**Files:** None (verification only).

- [ ] **Step 1: Run the full test suite**

Run: `go test ./... -v 2>&1 | tail -80`
Expected: All packages `ok`, no failures.

- [ ] **Step 2: Run go vet and build**

Run: `go vet ./... && go build ./...`
Expected: No errors.

- [ ] **Step 3: Manually verify backward compatibility**

```bash
AIDE_SANDBOX=on AIDE_NETWORK_MODE=outbound AIDE_CAPS=k8s,docker AIDE_TRUST=trusted \
  bash -c 'echo "{}" | go run ./cmd/aide statusline claude'
```
Expected output: `🔒 | 🌐 | ⚡ k8s,docker` (identical to v1 — no `📁`/`⚠️`
segments since `AIDE_CONTEXT`/untrusted aren't set).

```bash
bash -c 'echo "{}" | go run ./cmd/aide statusline claude'
```
(no AIDE_* env at all)
Expected output: `❓ | ❓` (unmanaged state, not the misleading `🔒 | 🌐`).

```bash
AIDE_SANDBOX=on AIDE_NETWORK_MODE=outbound bash -c 'echo "{}" | go run ./cmd/aide statusline claude --module sandbox'
```
Expected output: `🔒` (bare single-module output).

- [ ] **Step 4: No commit — this task is verification-only**
