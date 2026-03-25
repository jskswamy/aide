# Banner Template Engine & Extra Path Transparency — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace imperative `fmt.Fprintf` banner rendering with `text/template` + `embed.FS` and show extra sandbox paths (writable, readable, denied) that aren't from capabilities.

**Architecture:** Extract types to `types.go`, create a `FuncMap` in `funcmap.go` wrapping `fatih/color` and data helpers, write three `.tmpl` files (compact/boxed/clean), and replace the three render functions with a single template-based `RenderBanner`. In launcher, snapshot config paths before capability merge to populate new `BannerData` fields.

**Tech Stack:** Go `text/template`, `embed.FS`, `fatih/color` (existing dependency)

**Spec:** `docs/superpowers/specs/2026-03-25-banner-template-engine-design.md`

---

## File Map

| Action | File | Responsibility |
|--------|------|----------------|
| Create | `internal/ui/types.go` | All display types: `BannerData`, `SandboxInfo`, `CapabilityDisplay`, `GuardDisplay`, `GuardOverride` |
| Create | `internal/ui/funcmap.go` | `colorFuncMap()` returning `template.FuncMap` with color + data helpers |
| Create | `internal/ui/funcmap_test.go` | Unit tests for each FuncMap function |
| Create | `internal/ui/templates/compact.tmpl` | Compact banner template |
| Create | `internal/ui/templates/boxed.tmpl` | Boxed banner template |
| Create | `internal/ui/templates/clean.tmpl` | Clean banner template |
| Modify | `internal/ui/banner.go` | Strip types/render funcs, replace with `embed.FS` + `RenderBanner` using templates |
| Modify | `internal/ui/banner_test.go` | Update tests: call `RenderBanner(w, style, data)` instead of `RenderCompact`/etc., add extra-paths tests |
| Modify | `internal/launcher/launcher.go:225-300` | Snapshot config paths before cap merge; pass to `buildBannerData`; populate `ExtraWritable`/`ExtraReadable`/`ExtraDenied` |
| Modify | `internal/launcher/passthrough.go:189` | Handle `RenderBanner` returning `error` |
| Modify | `cmd/aide/commands.go:336` | Handle `RenderBanner` returning `error` |

---

### Task 1: Extract types to `types.go`

**Files:**
- Create: `internal/ui/types.go`
- Modify: `internal/ui/banner.go:22-75`

- [ ] **Step 1: Create `types.go` with all display types**

Move these types verbatim from `banner.go` into `types.go`, adding the three new fields to `BannerData`:

```go
// Package ui provides terminal rendering for aide's startup banner and status output.
package ui

// CapabilityDisplay holds per-capability information for banner rendering.
type CapabilityDisplay struct {
	Name     string
	Paths    []string // readable/writable paths granted
	EnvVars  []string // env vars passed through
	Source   string   // "context config", "--with", "--without"
	Disabled bool     // true if --without excluded this
}

// BannerData holds all information needed to render an aide banner.
type BannerData struct {
	ContextName string
	MatchReason string
	AgentName   string
	AgentPath   string
	SecretName  string
	SecretKeys  []string          // nil = normal (show count), populated = detailed (list names)
	Env         map[string]string // key → annotation (e.g. "← secrets.api_key" or "= literal")
	EnvResolved map[string]string // key → redacted value, nil in normal mode
	Sandbox       *SandboxInfo
	Yolo          bool
	Warnings      []string
	Capabilities  []CapabilityDisplay
	DisabledCaps  []CapabilityDisplay // --without caps
	NeverAllow    []string
	CredWarnings  []string // "AWS_SECRET_ACCESS_KEY (via aws)"
	CompWarnings  []string // composition warnings
	AutoApprove   bool     // replaces Yolo for new banner display

	// Extra sandbox paths from config (not from capabilities)
	ExtraWritable []string
	ExtraReadable []string
	ExtraDenied   []string
}

// SandboxInfo describes sandbox configuration for display.
type SandboxInfo struct {
	Disabled  bool
	Network   string           // "outbound only", "unrestricted", "none"
	Ports     string           // "all" or "443, 53"
	Active    []GuardDisplay
	Skipped   []GuardDisplay
	Available []string // opt-in guard names not enabled
}

// GuardDisplay holds per-guard information for banner rendering.
type GuardDisplay struct {
	Name      string
	Protected []string
	Allowed   []string
	Overrides []GuardOverride
	Reason    string // for skipped: "~/.kube not found"
}

// GuardOverride records an env var override for display.
type GuardOverride struct {
	EnvVar      string
	Value       string
	DefaultPath string
}
```

- [ ] **Step 2: Remove the type definitions from `banner.go`**

Delete lines 22-75 from `banner.go` (the `CapabilityDisplay`, `BannerData`, `SandboxInfo`, `GuardDisplay`, `GuardOverride` type blocks). Keep all functions.

- [ ] **Step 3: Verify it compiles**

Run: `cd /Users/subramk/source/github.com/jskswamy/aide && go build ./internal/ui/...`
Expected: clean build, types are now in `types.go` within the same package

- [ ] **Step 4: Run existing tests to verify no regressions**

Run: `cd /Users/subramk/source/github.com/jskswamy/aide && go test ./internal/ui/... -count=1`
Expected: all tests pass (same package, same types)

- [ ] **Step 5: Commit**

```bash
git add internal/ui/types.go internal/ui/banner.go
git commit -m "refactor(ui): extract banner display types to types.go

Preparation for template engine migration. All display types
(BannerData, SandboxInfo, etc.) moved to types.go. Adds three
new fields: ExtraWritable, ExtraReadable, ExtraDenied for
non-capability sandbox paths."
```

---

### Task 2: Create FuncMap with color and data helpers

**Files:**
- Create: `internal/ui/funcmap.go`
- Create: `internal/ui/funcmap_test.go`

- [ ] **Step 1: Write failing tests for FuncMap helpers**

```go
package ui

import (
	"strings"
	"testing"

	"github.com/fatih/color"
)

func init() {
	color.NoColor = true
}

func TestColorFuncMap_HasAllKeys(t *testing.T) {
	fm := colorFuncMap()
	required := []string{
		"bold", "green", "boldGreen", "yellow", "dim", "red", "cyan",
		"agentDisplay", "secretDisplay", "envLines", "networkLabel",
		"truncate", "join", "hasItems", "slice",
		"sandboxDisabled", "sandboxPorts", "hasCapOrExtra",
	}
	for _, key := range required {
		if _, ok := fm[key]; !ok {
			t.Errorf("colorFuncMap missing key %q", key)
		}
	}
}

func TestColorFunc_NoColor(t *testing.T) {
	fm := colorFuncMap()
	// With NoColor=true, color funcs should return bare string
	bold := fm["bold"].(func(string) string)
	if got := bold("test"); got != "test" {
		t.Errorf("bold(\"test\") with NoColor = %q, want \"test\"", got)
	}
}

func TestSandboxDisabled_NilSandbox(t *testing.T) {
	fm := colorFuncMap()
	fn := fm["sandboxDisabled"].(func(*BannerData) bool)
	data := &BannerData{}
	if fn(data) {
		t.Error("sandboxDisabled should be false when Sandbox is nil")
	}
}

func TestSandboxDisabled_True(t *testing.T) {
	fm := colorFuncMap()
	fn := fm["sandboxDisabled"].(func(*BannerData) bool)
	data := &BannerData{Sandbox: &SandboxInfo{Disabled: true}}
	if !fn(data) {
		t.Error("sandboxDisabled should be true when Sandbox.Disabled is true")
	}
}

func TestSandboxPorts_NilSandbox(t *testing.T) {
	fm := colorFuncMap()
	fn := fm["sandboxPorts"].(func(*BannerData) string)
	if got := fn(&BannerData{}); got != "" {
		t.Errorf("sandboxPorts with nil sandbox = %q, want empty", got)
	}
}

func TestSandboxPorts_All(t *testing.T) {
	fm := colorFuncMap()
	fn := fm["sandboxPorts"].(func(*BannerData) string)
	data := &BannerData{Sandbox: &SandboxInfo{Ports: "all"}}
	if got := fn(data); got != "" {
		t.Errorf("sandboxPorts with 'all' = %q, want empty", got)
	}
}

func TestSandboxPorts_Specific(t *testing.T) {
	fm := colorFuncMap()
	fn := fm["sandboxPorts"].(func(*BannerData) string)
	data := &BannerData{Sandbox: &SandboxInfo{Ports: "443, 53"}}
	if got := fn(data); got != "443, 53" {
		t.Errorf("sandboxPorts = %q, want \"443, 53\"", got)
	}
}

func TestHasCapOrExtra_Caps(t *testing.T) {
	fm := colorFuncMap()
	fn := fm["hasCapOrExtra"].(func(*BannerData) bool)
	data := &BannerData{
		Capabilities: []CapabilityDisplay{{Name: "docker"}},
	}
	if !fn(data) {
		t.Error("hasCapOrExtra should be true with capabilities")
	}
}

func TestHasCapOrExtra_ExtraWritable(t *testing.T) {
	fm := colorFuncMap()
	fn := fm["hasCapOrExtra"].(func(*BannerData) bool)
	data := &BannerData{
		ExtraWritable: []string{"/some/path"},
	}
	if !fn(data) {
		t.Error("hasCapOrExtra should be true with ExtraWritable")
	}
}

func TestHasCapOrExtra_Empty(t *testing.T) {
	fm := colorFuncMap()
	fn := fm["hasCapOrExtra"].(func(*BannerData) bool)
	data := &BannerData{}
	if fn(data) {
		t.Error("hasCapOrExtra should be false with no caps or extra paths")
	}
}

func TestSlice(t *testing.T) {
	fm := colorFuncMap()
	fn := fm["slice"].(func([]string, int) []string)
	got := fn([]string{"a", "b", "c"}, 1)
	if len(got) != 2 || got[0] != "b" || got[1] != "c" {
		t.Errorf("slice([a,b,c], 1) = %v, want [b,c]", got)
	}
}

func TestJoin(t *testing.T) {
	fm := colorFuncMap()
	fn := fm["join"].(func([]string, string) string)
	got := fn([]string{"a", "b"}, ", ")
	if got != "a, b" {
		t.Errorf("join = %q, want \"a, b\"", got)
	}
}

func TestHasItems(t *testing.T) {
	fm := colorFuncMap()
	fn := fm["hasItems"].(func([]string) bool)
	if fn(nil) {
		t.Error("hasItems(nil) should be false")
	}
	if !fn([]string{"x"}) {
		t.Error("hasItems([x]) should be true")
	}
}

func TestNetworkLabel_NilSandbox(t *testing.T) {
	fm := colorFuncMap()
	fn := fm["networkLabel"].(func(*BannerData) string)
	got := fn(&BannerData{})
	if got != "outbound" {
		t.Errorf("networkLabel with nil sandbox = %q, want \"outbound\"", got)
	}
}

func TestAgentDisplay_DifferentPath(t *testing.T) {
	fm := colorFuncMap()
	fn := fm["agentDisplay"].(func(*BannerData) string)
	data := &BannerData{AgentName: "claude", AgentPath: "/usr/bin/claude"}
	got := fn(data)
	if !strings.Contains(got, "claude") || !strings.Contains(got, "/usr/bin/claude") {
		t.Errorf("agentDisplay = %q, expected both name and path", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/subramk/source/github.com/jskswamy/aide && go test ./internal/ui/... -run TestColorFunc -count=1`
Expected: FAIL — `colorFuncMap` undefined

- [ ] **Step 3: Implement `funcmap.go`**

```go
package ui

import (
	"strings"
	"text/template"

	"github.com/fatih/color"
)

// colorFuncMap returns the template.FuncMap for banner templates.
// Color helpers return plain strings (ANSI codes applied by fatih/color).
// Data helpers expose existing logic to templates declaratively.
func colorFuncMap() template.FuncMap {
	return template.FuncMap{
		// Color helpers
		"bold":      func(s string) string { return color.New(color.Bold).Sprint(s) },
		"green":     func(s string) string { return color.New(color.FgGreen).Sprint(s) },
		"boldGreen": func(s string) string { return color.New(color.FgGreen, color.Bold).Sprint(s) },
		"yellow":    func(s string) string { return color.New(color.FgYellow).Sprint(s) },
		"dim":       func(s string) string { return color.New(color.Faint).Sprint(s) },
		"red":       func(s string) string { return color.New(color.FgRed, color.Bold).Sprint(s) },
		"cyan":      func(s string) string { return color.New(color.FgCyan).Sprint(s) },

		// Data helpers (wrapping existing functions)
		"agentDisplay":  agentDisplay,
		"secretDisplay": secretDisplay,
		"envLines":      envLines,
		"networkLabel":  sandboxNetworkLabel,
		"truncate":      truncateList,

		// Utility helpers
		"join":     strings.Join,
		"hasItems": func(s []string) bool { return len(s) > 0 },
		"slice": func(s []string, i int) []string {
			if i >= len(s) {
				return nil
			}
			return s[i:]
		},

		// Banner logic helpers (nil-safe)
		// IMPORTANT: Go text/template `and` does NOT short-circuit argument evaluation.
		// `{{if and .Sandbox .Sandbox.Ports}}` panics when .Sandbox is nil.
		// Use these nil-safe helpers instead.
		"sandboxDisabled": func(d *BannerData) bool {
			return d.Sandbox != nil && d.Sandbox.Disabled
		},
		"sandboxPorts": func(d *BannerData) string {
			if d.Sandbox == nil {
				return ""
			}
			if d.Sandbox.Ports == "all" {
				return ""
			}
			return d.Sandbox.Ports
		},
		"hasCapOrExtra": func(d *BannerData) bool {
			return len(d.Capabilities) > 0 ||
				len(d.DisabledCaps) > 0 ||
				len(d.ExtraWritable) > 0 ||
				len(d.ExtraReadable) > 0 ||
				len(d.ExtraDenied) > 0
		},
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/subramk/source/github.com/jskswamy/aide && go test ./internal/ui/... -count=1`
Expected: all tests pass

- [ ] **Step 5: Commit**

```bash
git add internal/ui/funcmap.go internal/ui/funcmap_test.go
git commit -m "feat(ui): add template FuncMap with color and data helpers

Color helpers wrap fatih/color Sprint for use in text/template.
Data helpers expose existing agentDisplay, secretDisplay, envLines,
networkLabel, truncateList. New helpers: sandboxDisabled (nil-safe),
hasCapOrExtra, slice, join, hasItems."
```

---

### Task 3: Write compact template

**Files:**
- Create: `internal/ui/templates/compact.tmpl`

- [ ] **Step 1: Create templates directory**

Run: `mkdir -p /Users/subramk/source/github.com/jskswamy/aide/internal/ui/templates`

- [ ] **Step 2: Write `compact.tmpl`**

```
{{- boldGreen "🔧 aide" -}}
{{- if .ContextName}} · {{.ContextName}}{{end}} ({{agentDisplay .}})
{{- if .MatchReason}}
   📁 {{.MatchReason}}
{{- end}}
{{- if .SecretName}}
   🔐 secret: {{secretDisplay .}}
{{- end}}
{{- $lines := envLines . -}}
{{- if $lines}}
   📦 env: {{index $lines 0}}
{{- range slice $lines 1}}
          {{.}}
{{- end}}
{{- end}}
{{- if sandboxDisabled .}}
   🛡 sandbox: disabled
{{- else if hasCapOrExtra .}}
   🛡 sandbox: network {{networkLabel .}}
{{- with sandboxPorts .}}
   🛡 ports: {{.}}
{{- end}}
{{- range .Capabilities}}
     {{boldGreen "✓"}} {{printf "%-10s" .Name}} {{truncate .Paths 3}}
{{- if and .Source (ne .Source "context config")}}  {{dim (printf "← %s" .Source)}}{{end}}
{{- end}}
{{- if .ExtraWritable}}
     {{yellow "⊕"}} writable:  {{truncate .ExtraWritable 3}}
{{- end}}
{{- if .ExtraReadable}}
     {{yellow "⊕"}} readable:  {{truncate .ExtraReadable 3}}
{{- end}}
{{- if .ExtraDenied}}
     {{red "⊘"}} denied:    {{truncate .ExtraDenied 3}}
{{- end}}
{{- range .DisabledCaps}}
     {{dim "○"}} {{printf "%-10s" .Name}} disabled for this session  ← --without
{{- end}}
{{- range .NeverAllow}}
     {{red "✗"}} denied    {{.}} (never-allow)
{{- end}}
{{- if .CredWarnings}}
     {{yellow "⚠"}} credentials exposed: {{join .CredWarnings ", "}}
{{- end}}
{{- range .CompWarnings}}
     {{yellow "⚠"}} {{.}}
{{- end}}
{{- else}}
   🛡 sandbox: network {{networkLabel .}}, code-only
{{- with sandboxPorts .}}
   🛡 ports: {{.}}
{{- end}}
{{- end}}
{{- range .Warnings}}
     {{yellow "⚠"}} {{.}}
{{- end}}
{{- if .AutoApprove}}
   {{red "⚡ AUTO-APPROVE — all agent actions execute without confirmation"}}
{{- end}}
```

- [ ] **Step 3: Commit**

```bash
git add internal/ui/templates/compact.tmpl
git commit -m "feat(ui): add compact banner template

Replaces RenderCompact imperative code. Includes capability display,
extra writable/readable/denied paths, disabled caps, never-allow,
credential warnings, and auto-approve sections."
```

---

### Task 4: Write boxed and clean templates

**Files:**
- Create: `internal/ui/templates/boxed.tmpl`
- Create: `internal/ui/templates/clean.tmpl`

- [ ] **Step 1: Write `boxed.tmpl`**

```
{{- boldGreen "┌─ aide ───────────────────────────────────────"}}
{{- if .ContextName}}
│ 🎯 {{cyan "Context   "}}{{boldGreen .ContextName}}
{{- end}}
{{- if .MatchReason}}
│ 📁 {{cyan "Matched   "}}{{.MatchReason}}
{{- end}}
│ 🤖 {{cyan "Agent     "}}{{agentDisplay .}}
{{- if .SecretName}}
│ 🔐 {{cyan "Secret    "}}{{secretDisplay .}}
{{- end}}
{{- $lines := envLines . -}}
{{- if $lines}}
│ 📦 {{cyan "Env       "}}{{index $lines 0}}
{{- range slice $lines 1}}
│              {{.}}
{{- end}}
{{- end}}
{{- if sandboxDisabled .}}
│ 🛡 sandbox: disabled
{{- else if hasCapOrExtra .}}
│ 🛡 sandbox: network {{networkLabel .}}
{{- with sandboxPorts .}}
│ 🛡 ports: {{.}}
{{- end}}
{{- range .Capabilities}}
│    {{boldGreen "✓"}} {{printf "%-10s" .Name}} {{truncate .Paths 3}}
{{- if and .Source (ne .Source "context config")}}  {{dim (printf "← %s" .Source)}}{{end}}
{{- end}}
{{- if .ExtraWritable}}
│    {{yellow "⊕"}} writable:  {{truncate .ExtraWritable 3}}
{{- end}}
{{- if .ExtraReadable}}
│    {{yellow "⊕"}} readable:  {{truncate .ExtraReadable 3}}
{{- end}}
{{- if .ExtraDenied}}
│    {{red "⊘"}} denied:    {{truncate .ExtraDenied 3}}
{{- end}}
{{- range .DisabledCaps}}
│    {{dim "○"}} {{printf "%-10s" .Name}} disabled for this session  ← --without
{{- end}}
{{- range .NeverAllow}}
│    {{red "✗"}} denied    {{.}} (never-allow)
{{- end}}
{{- if .CredWarnings}}
│    {{yellow "⚠"}} credentials exposed: {{join .CredWarnings ", "}}
{{- end}}
{{- range .CompWarnings}}
│    {{yellow "⚠"}} {{.}}
{{- end}}
{{- else}}
│ 🛡 sandbox: network {{networkLabel .}}, code-only
{{- with sandboxPorts .}}
│ 🛡 ports: {{.}}
{{- end}}
{{- end}}
{{- range .Warnings}}
│ {{yellow "⚠"}} {{.}}
{{- end}}
{{- if .AutoApprove}}
│ {{red "⚡ AUTO-APPROVE — all agent actions execute without confirmation"}}
{{- end}}
└──────────────────────────────────────────────────
```

- [ ] **Step 2: Write `clean.tmpl`**

```
{{- boldGreen "aide" -}}
{{- if .ContextName}} · context: {{boldGreen .ContextName}}{{end}}
  {{cyan "Agent     "}}{{agentDisplay .}}
{{- if .MatchReason}}
  {{cyan "Matched   "}}{{.MatchReason}}
{{- end}}
{{- if .SecretName}}
  {{cyan "Secret    "}}{{secretDisplay .}}
{{- end}}
{{- $lines := envLines . -}}
{{- if $lines}}
  {{cyan "Env       "}}{{index $lines 0}}
{{- range slice $lines 1}}
            {{.}}
{{- end}}
{{- end}}
{{- if sandboxDisabled .}}
  {{cyan "sandbox:  "}}disabled
{{- else if hasCapOrExtra .}}
  {{cyan "sandbox:  "}}network {{networkLabel .}}
{{- with sandboxPorts .}}
  {{cyan "ports:    "}}{{.}}
{{- end}}
{{- range .Capabilities}}
    {{boldGreen "✓"}} {{printf "%-10s" .Name}} {{truncate .Paths 3}}
{{- if and .Source (ne .Source "context config")}}  {{dim (printf "← %s" .Source)}}{{end}}
{{- end}}
{{- if .ExtraWritable}}
    {{yellow "⊕"}} writable:  {{truncate .ExtraWritable 3}}
{{- end}}
{{- if .ExtraReadable}}
    {{yellow "⊕"}} readable:  {{truncate .ExtraReadable 3}}
{{- end}}
{{- if .ExtraDenied}}
    {{red "⊘"}} denied:    {{truncate .ExtraDenied 3}}
{{- end}}
{{- range .DisabledCaps}}
    {{dim "○"}} {{printf "%-10s" .Name}} disabled for this session  ← --without
{{- end}}
{{- range .NeverAllow}}
    {{red "✗"}} denied    {{.}} (never-allow)
{{- end}}
{{- if .CredWarnings}}
    {{yellow "⚠"}} credentials exposed: {{join .CredWarnings ", "}}
{{- end}}
{{- range .CompWarnings}}
    {{yellow "⚠"}} {{.}}
{{- end}}
{{- else}}
  {{cyan "sandbox:  "}}network {{networkLabel .}}, code-only
{{- with sandboxPorts .}}
  {{cyan "ports:    "}}{{.}}
{{- end}}
{{- end}}
{{- range .Warnings}}
  {{yellow "⚠"}} {{.}}
{{- end}}
{{- if .AutoApprove}}
  {{red "⚡ AUTO-APPROVE — all agent actions execute without confirmation"}}
{{- end}}
```

- [ ] **Step 3: Commit**

```bash
git add internal/ui/templates/boxed.tmpl internal/ui/templates/clean.tmpl
git commit -m "feat(ui): add boxed and clean banner templates

Boxed uses box-drawing borders and labeled rows. Clean uses
no emoji with cyan labels. Both include sandbox-disabled check
(fixing bug in old boxed code) and extra path sections."
```

---

### Task 5: Rewrite `banner.go` with template engine

**Files:**
- Modify: `internal/ui/banner.go`

- [ ] **Step 1: Rewrite `banner.go`**

Replace the entire file with:

```go
// Package ui provides terminal rendering for aide's startup banner and status output.
package ui

import (
	"embed"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/template"

	"github.com/fatih/color"
)

//go:embed templates/*.tmpl
var templateFS embed.FS

// RenderBanner renders the banner using the given style. Valid styles are
// "compact" (default), "boxed", and "clean".
func RenderBanner(w io.Writer, style string, data *BannerData) error {
	tmpl, err := template.New("").Funcs(colorFuncMap()).ParseFS(templateFS, "templates/*.tmpl")
	if err != nil {
		return fmt.Errorf("parsing banner templates: %w", err)
	}
	name := style + ".tmpl"
	// Fall back to compact for unknown styles
	if t := tmpl.Lookup(name); t == nil {
		name = "compact.tmpl"
	}
	if err := tmpl.ExecuteTemplate(w, name, data); err != nil {
		return fmt.Errorf("rendering banner style %q: %w", style, err)
	}
	return nil
}

// --- Data helper functions (used by FuncMap and retained for direct use) ---

// agentDisplay returns the agent display string, including path when it differs
// from the name.
func agentDisplay(data *BannerData) string {
	if data.AgentPath != "" && data.AgentPath != data.AgentName {
		return fmt.Sprintf("%s → %s", data.AgentName, data.AgentPath)
	}
	return data.AgentName
}

// secretDisplay returns the secret display string.
func secretDisplay(data *BannerData) string {
	if data.SecretName == "" {
		return ""
	}
	if len(data.SecretKeys) > 0 {
		return fmt.Sprintf("%s (%d keys: %s)", data.SecretName, len(data.SecretKeys), strings.Join(data.SecretKeys, ", "))
	}
	return data.SecretName
}

// envLines returns formatted env variable lines sorted by key.
func envLines(data *BannerData) []string {
	if len(data.Env) == 0 {
		return nil
	}
	keys := make([]string, 0, len(data.Env))
	for k := range data.Env {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var lines []string
	for _, k := range keys {
		annotation := data.Env[k]
		if data.EnvResolved != nil {
			if rv, ok := data.EnvResolved[k]; ok {
				lines = append(lines, fmt.Sprintf("%s %s (%s)", k, annotation, rv))
				continue
			}
		}
		lines = append(lines, fmt.Sprintf("%s %s", k, annotation))
	}
	return lines
}

// truncateList caps a list at maxItems and appends "(+N more)" if truncated.
func truncateList(items []string, maxItems int) string {
	if len(items) == 0 {
		return ""
	}
	if len(items) <= maxItems {
		return strings.Join(items, ", ")
	}
	shown := strings.Join(items[:maxItems], ", ")
	return fmt.Sprintf("%s (+%d more)", shown, len(items)-maxItems)
}

// sandboxNetworkLabel returns the network mode for display.
func sandboxNetworkLabel(data *BannerData) string {
	if data.Sandbox != nil && data.Sandbox.Network != "" {
		return data.Sandbox.Network
	}
	return "outbound"
}

// renderGuardSection is available for aide sandbox commands but no longer
// used in the banner. Guard details are internal — the banner shows
// capabilities only. Keeping the types (SandboxInfo, GuardDisplay) for
// the aide sandbox guards CLI command.
//
//nolint:unused // retained for aide sandbox guards command
func renderGuardSection(w io.Writer, info *SandboxInfo, prefix string) {
	boldGreenC := color.New(color.FgGreen, color.Bold)
	yellowC := color.New(color.FgYellow)
	dimC := color.New(color.Faint)

	for _, g := range info.Active {
		boldGreenC.Fprintf(w, "%s✓ %s\n", prefix, g.Name)
		if len(g.Protected) > 0 {
			fmt.Fprintf(w, "%s    denied:  %s\n", prefix, truncateList(g.Protected, 3))
		}
		if len(g.Allowed) > 0 {
			fmt.Fprintf(w, "%s    allowed: %s\n", prefix, truncateList(g.Allowed, 3))
		}
		for _, o := range g.Overrides {
			fmt.Fprintf(w, "%s    override: %s → %s (default: %s)\n",
				prefix, o.EnvVar, o.Value, o.DefaultPath)
		}
	}
	if len(info.Active) > 0 && (len(info.Skipped) > 0 || len(info.Available) > 0) {
		fmt.Fprintln(w)
	}
	for _, g := range info.Skipped {
		yellowC.Fprintf(w, "%s⊘ %s", prefix, g.Name)
		fmt.Fprintf(w, " — %s\n", g.Reason)
	}
	if len(info.Skipped) > 0 && len(info.Available) > 0 {
		fmt.Fprintln(w)
	}
	if len(info.Available) > 0 {
		dimC.Fprintf(w, "%s○ %s — available (opt-in)\n",
			prefix, strings.Join(info.Available, ", "))
	}
	needsHint := len(info.Skipped) > 0 || len(info.Available) > 0
	for _, g := range info.Active {
		if len(g.Protected) > 3 || len(g.Allowed) > 3 {
			needsHint = true
		}
	}
	if needsHint {
		fmt.Fprintln(w)
		dimC.Fprintf(w, "%srun `aide sandbox` for full details\n", prefix)
	}
}
```

- [ ] **Step 2: Verify it compiles**

Run: `cd /Users/subramk/source/github.com/jskswamy/aide && go build ./internal/ui/...`
Expected: clean build

- [ ] **Step 3: Commit**

```bash
git add internal/ui/banner.go
git commit -m "feat(ui): replace imperative rendering with text/template engine

RenderBanner now uses embed.FS templates instead of switch + Fprintf.
Removes RenderCompact, RenderBoxed, RenderClean, renderCapabilitySection,
renderAutoApprove, hasCapabilities, and package-level color vars.
Retains renderGuardSection for aide sandbox CLI.
Returns error for template parse/exec failures."
```

---

### Task 6: Update callers to handle `RenderBanner` returning `error`

**Files:**
- Modify: `internal/launcher/launcher.go:297`
- Modify: `internal/launcher/passthrough.go:189`
- Modify: `cmd/aide/commands.go:336`

- [ ] **Step 1: Update `launcher.go` line 297**

In `internal/launcher/launcher.go`, change:

```go
		ui.RenderBanner(l.stderr(), prefs.InfoStyle, bannerData)
```

to:

```go
		if err := ui.RenderBanner(l.stderr(), prefs.InfoStyle, bannerData); err != nil {
			fmt.Fprintf(l.stderr(), "warning: banner render failed: %v\n", err)
		}
```

- [ ] **Step 2: Update `passthrough.go` line 189**

In `internal/launcher/passthrough.go`, change:

```go
	ui.RenderBanner(l.stderr(), "compact", bannerData)
```

to:

```go
	if err := ui.RenderBanner(l.stderr(), "compact", bannerData); err != nil {
		fmt.Fprintf(l.stderr(), "warning: banner render failed: %v\n", err)
	}
```

- [ ] **Step 3: Update `cmd/aide/commands.go` line 336**

Change:

```go
			ui.RenderBanner(out, prefs.InfoStyle, data)
```

to:

```go
			if err := ui.RenderBanner(out, prefs.InfoStyle, data); err != nil {
				return fmt.Errorf("rendering banner: %w", err)
			}
```

- [ ] **Step 4: Verify it compiles**

Run: `cd /Users/subramk/source/github.com/jskswamy/aide && go build ./...`
Expected: clean build

- [ ] **Step 5: Commit**

```bash
git add internal/launcher/launcher.go internal/launcher/passthrough.go cmd/aide/commands.go
git commit -m "fix: handle RenderBanner error return at all call sites

Launcher and passthrough log warnings on failure (non-fatal).
aide which command returns the error."
```

---

### Task 7: Update tests for template-based rendering

**Files:**
- Modify: `internal/ui/banner_test.go`

- [ ] **Step 1: Rewrite `banner_test.go`**

All tests should call `RenderBanner(w, style, data)` instead of `RenderCompact`/`RenderBoxed`/`RenderClean`. Use structural assertions (contains/not-contains). Add extra-path tests.

Key changes:
- Replace `RenderCompact(&buf, data)` → `RenderBanner(&buf, "compact", data)`
- Replace `RenderBoxed(&buf, data)` → `RenderBanner(&buf, "boxed", data)`
- Replace `RenderClean(&buf, data)` → `RenderBanner(&buf, "clean", data)`
- Check `err` return from `RenderBanner`
- Add `TestRenderBanner_UnknownStyle` to verify fallback (returns nil error, outputs compact)
- Add tests for extra paths

New test additions:

```go
func TestRenderCompact_ExtraWritable(t *testing.T) {
	data := &BannerData{
		AgentName:     "claude",
		Sandbox:       &SandboxInfo{Network: "unrestricted"},
		ExtraWritable: []string{"~/.config/gcloud", "~/.kube/"},
	}
	var buf bytes.Buffer
	if err := RenderBanner(&buf, "compact", data); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "writable:") {
		t.Errorf("expected writable section:\n%s", out)
	}
	if !strings.Contains(out, "~/.config/gcloud") {
		t.Errorf("expected gcloud path:\n%s", out)
	}
	if strings.Contains(out, "code-only") {
		t.Errorf("should not show code-only when extra paths present:\n%s", out)
	}
}

func TestRenderCompact_ExtraReadable(t *testing.T) {
	data := &BannerData{
		AgentName:     "claude",
		Sandbox:       &SandboxInfo{Network: "outbound only"},
		ExtraReadable: []string{"~/.docker"},
	}
	var buf bytes.Buffer
	if err := RenderBanner(&buf, "compact", data); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "readable:") {
		t.Errorf("expected readable section:\n%s", out)
	}
	if !strings.Contains(out, "~/.docker") {
		t.Errorf("expected docker path:\n%s", out)
	}
}

func TestRenderCompact_ExtraDenied(t *testing.T) {
	data := &BannerData{
		AgentName:   "claude",
		Sandbox:     &SandboxInfo{Network: "outbound only"},
		ExtraDenied: []string{"/etc/shadow"},
	}
	var buf bytes.Buffer
	if err := RenderBanner(&buf, "compact", data); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "denied:") {
		t.Errorf("expected denied section:\n%s", out)
	}
}

func TestRenderCompact_NoExtraPaths(t *testing.T) {
	data := &BannerData{
		AgentName: "claude",
		Sandbox:   &SandboxInfo{Network: "outbound only"},
	}
	var buf bytes.Buffer
	if err := RenderBanner(&buf, "compact", data); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if strings.Contains(out, "writable:") {
		t.Errorf("should not show writable when empty:\n%s", out)
	}
	if strings.Contains(out, "readable:") {
		t.Errorf("should not show readable when empty:\n%s", out)
	}
	if !strings.Contains(out, "code-only") {
		t.Errorf("should show code-only when no caps or extra paths:\n%s", out)
	}
}

func TestRenderBoxed_ExtraWritable(t *testing.T) {
	data := &BannerData{
		AgentName:     "claude",
		Sandbox:       &SandboxInfo{Network: "outbound only"},
		ExtraWritable: []string{"~/.azure/"},
	}
	var buf bytes.Buffer
	if err := RenderBanner(&buf, "boxed", data); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "writable:") {
		t.Errorf("boxed expected writable section:\n%s", out)
	}
}

func TestRenderClean_ExtraWritable(t *testing.T) {
	data := &BannerData{
		AgentName:     "claude",
		Sandbox:       &SandboxInfo{Network: "outbound only"},
		ExtraWritable: []string{"~/.azure/"},
	}
	var buf bytes.Buffer
	if err := RenderBanner(&buf, "clean", data); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "writable:") {
		t.Errorf("clean expected writable section:\n%s", out)
	}
}

func TestRenderBanner_ErrorOnBadTemplate(t *testing.T) {
	// This tests the fallback — unknown style should not error
	var buf bytes.Buffer
	err := RenderBanner(&buf, "nonexistent", &BannerData{AgentName: "claude"})
	if err != nil {
		t.Errorf("unknown style should fall back to compact, not error: %v", err)
	}
	if !strings.Contains(buf.String(), "aide") {
		t.Error("fallback should render compact")
	}
}
```

- [ ] **Step 2: Run all tests**

Run: `cd /Users/subramk/source/github.com/jskswamy/aide && go test ./internal/ui/... -count=1 -v`
Expected: all tests pass

- [ ] **Step 3: Commit**

```bash
git add internal/ui/banner_test.go
git commit -m "test(ui): update banner tests for template engine

Replace direct RenderCompact/Boxed/Clean calls with RenderBanner.
Add tests for extra writable/readable/denied path display.
Add error handling assertions."
```

---

### Task 8: Populate extra path fields in launcher

**Files:**
- Modify: `internal/launcher/launcher.go:225-300,418-546`

- [ ] **Step 1: Snapshot config paths before capability merge in `Launch()`**

In `internal/launcher/launcher.go`, after line 229 (after `ResolveSandboxRef`) and before line 234 (the capability merge block), add:

```go
	// Snapshot original config paths before capability overrides merge them.
	var configWritableExtra, configReadableExtra, configDeniedExtra []string
	if sandboxCfg != nil {
		configWritableExtra = append([]string{}, sandboxCfg.WritableExtra...)
		configReadableExtra = append([]string{}, sandboxCfg.ReadableExtra...)
		configDeniedExtra = append([]string{}, sandboxCfg.DeniedExtra...)
	}
```

- [ ] **Step 2: Pass config path snapshots to `buildBannerData`**

Update the `buildBannerData` call on line 294 to pass the three new slices. Add parameters to the function signature:

```go
func (l *Launcher) buildBannerData(
	rc *aidectx.ResolvedContext,
	agentName, binary string,
	resolvedEnv map[string]string,
	pathWarnings []string,
	sbDisabled bool,
	sandboxCfg *config.SandboxPolicy,
	projectRoot, rtDirPath, homeDir string,
	prefs *config.Preferences,
	resolvedCapSet *capability.Set,
	capOverrides capability.SandboxOverrides,
	contextCapSet map[string]bool,
	withoutCaps []string,
	cfg *config.Config,
	configWritableExtra, configReadableExtra, configDeniedExtra []string,
) *ui.BannerData {
```

- [ ] **Step 3: Populate ExtraWritable/ExtraReadable/ExtraDenied**

At the end of `buildBannerData`, before `return data`, add the set-difference logic:

```go
	// Populate extra paths that are from config (not capabilities)
	data.ExtraWritable = stringSetDiff(configWritableExtra, capOverrides.WritableExtra)
	data.ExtraReadable = stringSetDiff(configReadableExtra, capOverrides.ReadableExtra)
	data.ExtraDenied = stringSetDiff(configDeniedExtra, capOverrides.DeniedExtra)
```

Add the helper function:

```go
// stringSetDiff returns elements in a that are not in b.
func stringSetDiff(a, b []string) []string {
	if len(a) == 0 {
		return nil
	}
	bSet := make(map[string]bool, len(b))
	for _, s := range b {
		bSet[s] = true
	}
	var diff []string
	for _, s := range a {
		if !bSet[s] {
			diff = append(diff, s)
		}
	}
	return diff
}
```

- [ ] **Step 4: Update the `buildBannerData` call site**

In `Launch()`, update line 294:

```go
		bannerData := l.buildBannerData(rc, agentName, binary, resolvedEnv, pathWarnings, sbDisabled, sandboxCfg, projectRoot, rtDir.Path(), homeDir, &prefs, resolvedCapSet, capOverrides, contextCapSet, withoutCaps, cfg, configWritableExtra, configReadableExtra, configDeniedExtra)
```

- [ ] **Step 5: Verify it compiles and tests pass**

Run: `cd /Users/subramk/source/github.com/jskswamy/aide && go build ./... && go test ./internal/launcher/... -count=1`
Expected: clean build, tests pass

- [ ] **Step 6: Commit**

```bash
git add internal/launcher/launcher.go
git commit -m "feat(launcher): populate extra sandbox paths in banner data

Snapshots config writable_extra/readable_extra/denied_extra before
capability overrides are merged. Set-difference produces paths that
came from config but not from capabilities, displayed in the banner
for transparency."
```

---

### Task 9: End-to-end verification

- [ ] **Step 1: Run full test suite**

Run: `cd /Users/subramk/source/github.com/jskswamy/aide && go test ./... -count=1`
Expected: all tests pass across all packages

- [ ] **Step 2: Build and run manual verification**

Run: `cd /Users/subramk/source/github.com/jskswamy/aide && go build -o aide ./cmd/aide && ./aide --resolve`
Expected: banner renders with the template engine, shows sandbox info. If in a context with `writable_extra`/`readable_extra`, those paths appear with `⊕`/`⊘` markers.

- [ ] **Step 3: Verify each style**

Run with each style to visually confirm:
```bash
# Edit config temporarily or use env override to test styles
./aide --resolve  # compact (default)
```

- [ ] **Step 4: Commit (if any tweaks needed)**

Only if template whitespace or alignment needs adjustment after visual inspection.
