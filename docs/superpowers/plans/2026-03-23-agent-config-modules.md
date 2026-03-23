# Agent Config Dir Modules Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make agent config directories flow through the seatbelt module system so env var overrides (like `CLAUDE_CONFIG_DIR`) are respected by the sandbox.

**Architecture:** Each agent gets a seatbelt module that uses `ctx.EnvLookup()` to check for config dir env var overrides, falling back to hardcoded defaults. Modules are the single source of truth — the standalone resolver registry in `agentcfg.go` is removed. The launcher propagates the merged env to `policy.Env` so modules can see config env vars.

**Tech Stack:** Go, macOS Seatbelt sandbox, aide's `pkg/seatbelt` module system

**Test command:** `nix develop --command go test ./...` (or `go test ./...` if Go is on PATH with GOROOT set)

---

## File Structure

| File | Responsibility |
|------|---------------|
| `pkg/seatbelt/path_helpers.go` | **New.** `ExistsOrUnderHome` — filesystem existence check for sandbox path decisions |
| `pkg/seatbelt/path_helpers_test.go` | **New.** Tests for `ExistsOrUnderHome` |
| `pkg/seatbelt/modules/helpers.go` | **New.** `resolveConfigDirs`, `configDirRules` — shared logic for all agent modules |
| `pkg/seatbelt/modules/helpers_test.go` | **New.** Tests for shared helpers |
| `pkg/seatbelt/modules/claude.go` | **Modify.** Add `CLAUDE_CONFIG_DIR` env var support |
| `pkg/seatbelt/modules/claude_test.go` | **New.** Tests for Claude module with/without env override |
| `pkg/seatbelt/modules/codex.go` | **New.** Codex agent module |
| `pkg/seatbelt/modules/aider.go` | **New.** Aider agent module |
| `pkg/seatbelt/modules/goose.go` | **New.** Goose agent module |
| `pkg/seatbelt/modules/amp.go` | **New.** Amp agent module |
| `pkg/seatbelt/modules/gemini.go` | **New.** Gemini agent module |
| `pkg/seatbelt/modules/agents_test.go` | **New.** Table-driven tests for all 5 new agent modules |
| `internal/launcher/agentcfg.go` | **Modify.** Delete resolver registry, expand `agentModuleResolvers` |
| `internal/launcher/agentcfg_test.go` | **Modify.** Delete resolver tests (coverage moves to module tests) |
| `internal/launcher/launcher.go` | **Modify.** Set `policy.Env = env` before sandbox apply |
| `internal/launcher/passthrough.go` | **Modify.** Remove dead `ResolveAgentConfigDirs` call |
| `internal/sandbox/darwin_test.go` | **Modify.** Add integration test for `CLAUDE_CONFIG_DIR` in profile |

---

### Task 1: Add `ExistsOrUnderHome` helper

**Files:**
- Create: `pkg/seatbelt/path_helpers.go`
- Create: `pkg/seatbelt/path_helpers_test.go`

- [ ] **Step 1: Write the failing tests**

```go
// pkg/seatbelt/path_helpers_test.go
package seatbelt

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExistsOrUnderHome_ExistingPath(t *testing.T) {
	home := t.TempDir()
	existing := filepath.Join(home, ".config")
	if err := os.Mkdir(existing, 0o755); err != nil {
		t.Fatal(err)
	}
	if !ExistsOrUnderHome(home, existing) {
		t.Error("expected true for existing path")
	}
}

func TestExistsOrUnderHome_NonExistentUnderHome(t *testing.T) {
	home := t.TempDir()
	nonExistent := filepath.Join(home, ".agent-config")
	if !ExistsOrUnderHome(home, nonExistent) {
		t.Error("expected true for non-existent path under home")
	}
}

func TestExistsOrUnderHome_NonExistentOutsideHome(t *testing.T) {
	home := t.TempDir()
	if ExistsOrUnderHome(home, "/opt/agent-config") {
		t.Error("expected false for non-existent path outside home")
	}
}

func TestExistsOrUnderHome_ExistingOutsideHome(t *testing.T) {
	home := t.TempDir()
	outside := t.TempDir() // exists but not under home
	if !ExistsOrUnderHome(home, outside) {
		t.Error("expected true for existing path even outside home")
	}
}

func TestExistsOrUnderHome_HomePrefixBoundary(t *testing.T) {
	// /tmp/home vs /tmp/homefoo — must not match
	home := t.TempDir()
	sibling := home + "foo"
	if ExistsOrUnderHome(home, sibling) {
		t.Errorf("should not match sibling dir %q when home is %q", sibling, home)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `nix develop --command go test ./pkg/seatbelt/ -run TestExistsOrUnderHome -v`
Expected: FAIL — `ExistsOrUnderHome` not defined

- [ ] **Step 3: Write the implementation**

```go
// pkg/seatbelt/path_helpers.go
package seatbelt

import (
	"os"
	"path/filepath"
	"strings"
)

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

- [ ] **Step 4: Run tests to verify they pass**

Run: `nix develop --command go test ./pkg/seatbelt/ -run TestExistsOrUnderHome -v`
Expected: PASS (all 5 tests)

- [ ] **Step 5: Commit**

```bash
git add pkg/seatbelt/path_helpers.go pkg/seatbelt/path_helpers_test.go
git commit -m "feat(seatbelt): add ExistsOrUnderHome path helper"
```

---

### Task 2: Add shared module helpers (`resolveConfigDirs`, `configDirRules`)

**Files:**
- Create: `pkg/seatbelt/modules/helpers.go`
- Create: `pkg/seatbelt/modules/helpers_test.go`

- [ ] **Step 1: Write the failing tests**

```go
// pkg/seatbelt/modules/helpers_test.go
package modules

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/jskswamy/aide/pkg/seatbelt"
)

// --- resolveConfigDirs ---

func TestResolveConfigDirs_EnvOverride(t *testing.T) {
	ctx := &seatbelt.Context{
		HomeDir: "/home/user",
		Env:     []string{"AGENT_HOME=/custom/path"},
	}
	dirs := resolveConfigDirs(ctx, "AGENT_HOME", []string{
		filepath.Join(ctx.HomeDir, ".agent"),
	})
	if len(dirs) != 1 || dirs[0] != "/custom/path" {
		t.Errorf("expected [/custom/path], got %v", dirs)
	}
}

func TestResolveConfigDirs_EmptyEnvFallsThrough(t *testing.T) {
	home := t.TempDir()
	ctx := &seatbelt.Context{
		HomeDir: home,
		Env:     []string{"AGENT_HOME="},
	}
	dirs := resolveConfigDirs(ctx, "AGENT_HOME", []string{
		filepath.Join(home, ".agent"),
	})
	// Empty env var treated as unset — should return defaults
	if len(dirs) != 1 || dirs[0] != filepath.Join(home, ".agent") {
		t.Errorf("expected default dir, got %v", dirs)
	}
}

func TestResolveConfigDirs_NoEnvKey(t *testing.T) {
	home := t.TempDir()
	ctx := &seatbelt.Context{HomeDir: home}
	dirs := resolveConfigDirs(ctx, "", []string{
		filepath.Join(home, ".agent"),
	})
	if len(dirs) != 1 {
		t.Errorf("expected 1 default dir, got %v", dirs)
	}
}

func TestResolveConfigDirs_NonExistentOutsideHome(t *testing.T) {
	home := t.TempDir()
	ctx := &seatbelt.Context{HomeDir: home}
	dirs := resolveConfigDirs(ctx, "", []string{"/opt/nonexistent"})
	if len(dirs) != 0 {
		t.Errorf("expected empty dirs for non-existent outside home, got %v", dirs)
	}
}

// --- configDirRules ---

func TestConfigDirRules_Empty(t *testing.T) {
	rules := configDirRules("Test", nil)
	if rules != nil {
		t.Errorf("expected nil rules for empty dirs, got %v", rules)
	}
}

func TestConfigDirRules_SingleDir(t *testing.T) {
	rules := configDirRules("Test", []string{"/home/user/.agent"})
	if len(rules) != 2 { // section header + 1 rule
		t.Errorf("expected 2 rules, got %d", len(rules))
	}
	ruleStr := rules[1].String()
	if !strings.Contains(ruleStr, `(subpath "/home/user/.agent")`) {
		t.Errorf("expected subpath rule, got %q", ruleStr)
	}
	if !strings.Contains(ruleStr, "file-read*") || !strings.Contains(ruleStr, "file-write*") {
		t.Errorf("expected file-read* file-write*, got %q", ruleStr)
	}
}

func TestConfigDirRules_MultipleDirs(t *testing.T) {
	rules := configDirRules("Test", []string{"/a", "/b", "/c"})
	if len(rules) != 4 { // section header + 3 rules
		t.Errorf("expected 4 rules, got %d", len(rules))
	}
}

func TestConfigDirRules_SectionName(t *testing.T) {
	rules := configDirRules("Claude", []string{"/a"})
	header := rules[0].String()
	if !strings.Contains(header, "Claude config") {
		t.Errorf("expected section header with 'Claude config', got %q", header)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `nix develop --command go test ./pkg/seatbelt/modules/ -run "TestResolveConfigDirs|TestConfigDirRules" -v`
Expected: FAIL — functions not defined

- [ ] **Step 3: Write the implementation**

```go
// pkg/seatbelt/modules/helpers.go
package modules

import (
	"fmt"
	"path/filepath"

	"github.com/jskswamy/aide/pkg/seatbelt"
)

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
			`(allow file-read* file-write* (subpath %q))`,
			filepath.Clean(dir),
		)))
	}
	return rules
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `nix develop --command go test ./pkg/seatbelt/modules/ -run "TestResolveConfigDirs|TestConfigDirRules" -v`
Expected: PASS (all 8 tests)

- [ ] **Step 5: Commit**

```bash
git add pkg/seatbelt/modules/helpers.go pkg/seatbelt/modules/helpers_test.go
git commit -m "feat(modules): add resolveConfigDirs and configDirRules helpers"
```

---

### Task 3: Update Claude module to support `CLAUDE_CONFIG_DIR`

**Files:**
- Modify: `pkg/seatbelt/modules/claude.go`
- Create: `pkg/seatbelt/modules/claude_test.go`

- [ ] **Step 1: Write the failing tests**

```go
// pkg/seatbelt/modules/claude_test.go
package modules

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/jskswamy/aide/pkg/seatbelt"
)

func TestClaudeModule_EnvOverride(t *testing.T) {
	home := t.TempDir()
	ctx := &seatbelt.Context{
		HomeDir: home,
		Env:     []string{"CLAUDE_CONFIG_DIR=/custom/claude-work"},
		GOOS:    "darwin",
	}
	m := ClaudeAgent()
	rules := m.Rules(ctx)

	profile := rulesToString(rules)
	if !strings.Contains(profile, `(subpath "/custom/claude-work")`) {
		t.Error("expected custom config dir in rules")
	}
	// Default .claude should NOT be present as a config dir subpath
	// (it may still appear in the user data section via HomeSubpath)
}

func TestClaudeModule_DefaultConfigDirs(t *testing.T) {
	home := t.TempDir()
	ctx := &seatbelt.Context{
		HomeDir: home,
		GOOS:    "darwin",
	}
	m := ClaudeAgent()
	rules := m.Rules(ctx)

	profile := rulesToString(rules)
	// Should have default config dirs
	if !strings.Contains(profile, filepath.Join(home, ".claude")) {
		t.Error("expected ~/.claude in default config dirs")
	}
}

func TestClaudeModule_NonConfigPathsAlwaysPresent(t *testing.T) {
	home := t.TempDir()
	ctx := &seatbelt.Context{
		HomeDir: home,
		Env:     []string{"CLAUDE_CONFIG_DIR=/custom/path"},
		GOOS:    "darwin",
	}
	m := ClaudeAgent()
	rules := m.Rules(ctx)

	profile := rulesToString(rules)
	// These runtime paths should always be present regardless of CLAUDE_CONFIG_DIR
	for _, expected := range []string{".cache/claude", ".local/state/claude", ".mcp.json"} {
		if !strings.Contains(profile, expected) {
			t.Errorf("expected %q in rules regardless of env override", expected)
		}
	}
}

func TestClaudeModule_Name(t *testing.T) {
	m := ClaudeAgent()
	if m.Name() != "Claude Agent" {
		t.Errorf("expected 'Claude Agent', got %q", m.Name())
	}
}

// rulesToString concatenates all rule strings for assertion matching.
func rulesToString(rules []seatbelt.Rule) string {
	var b strings.Builder
	for _, r := range rules {
		b.WriteString(r.String())
		b.WriteByte('\n')
	}
	return b.String()
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `nix develop --command go test ./pkg/seatbelt/modules/ -run TestClaudeModule -v`
Expected: FAIL — current claude module doesn't check env var, `TestClaudeModule_EnvOverride` fails

- [ ] **Step 3: Update the Claude module**

Replace the contents of `pkg/seatbelt/modules/claude.go` with:

```go
// Package modules provides composable Seatbelt profile building blocks.
//
// Claude agent module rules ported from agent-safehouse by Eugene Goldin:
// https://github.com/eugene1g/agent-safehouse
// Source: profiles/60-agents/claude-code.sb
package modules

import (
	"path/filepath"

	"github.com/jskswamy/aide/pkg/seatbelt"
)

type claudeAgentModule struct{}

// ClaudeAgent returns a module with Claude Code agent sandbox rules.
func ClaudeAgent() seatbelt.Module { return &claudeAgentModule{} }

func (m *claudeAgentModule) Name() string { return "Claude Agent" }

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

- [ ] **Step 4: Run tests to verify they pass**

Run: `nix develop --command go test ./pkg/seatbelt/modules/ -run TestClaudeModule -v`
Expected: PASS (all 4 tests)

- [ ] **Step 5: Commit**

```bash
git add pkg/seatbelt/modules/claude.go pkg/seatbelt/modules/claude_test.go
git commit -m "feat(modules): add CLAUDE_CONFIG_DIR env var support to Claude module"
```

---

### Task 4: Create agent modules for Codex, Aider, Goose, Amp, Gemini

**Files:**
- Create: `pkg/seatbelt/modules/codex.go`
- Create: `pkg/seatbelt/modules/aider.go`
- Create: `pkg/seatbelt/modules/goose.go`
- Create: `pkg/seatbelt/modules/amp.go`
- Create: `pkg/seatbelt/modules/gemini.go`
- Create: `pkg/seatbelt/modules/agents_test.go`

- [ ] **Step 1: Write the failing tests (table-driven)**

```go
// pkg/seatbelt/modules/agents_test.go
package modules

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/jskswamy/aide/pkg/seatbelt"
)

func TestAgentModules(t *testing.T) {
	tests := []struct {
		name          string
		factory       func() seatbelt.Module
		envKey        string
		envVal        string
		expectedName  string
		defaultCount  int   // number of default dirs (when no env override)
		defaultSuffix []string // suffixes to find in default config rules
	}{
		{
			name:          "Codex/env-override",
			factory:       CodexAgent,
			envKey:        "CODEX_HOME",
			envVal:        "/custom/codex",
			expectedName:  "Codex Agent",
			defaultCount:  1,
			defaultSuffix: []string{".codex"},
		},
		{
			name:          "Aider/defaults",
			factory:       AiderAgent,
			envKey:        "",
			envVal:        "",
			expectedName:  "Aider Agent",
			defaultCount:  1,
			defaultSuffix: []string{".aider"},
		},
		{
			name:          "Goose/env-override",
			factory:       GooseAgent,
			envKey:        "GOOSE_PATH_ROOT",
			envVal:        "/custom/goose",
			expectedName:  "Goose Agent",
			defaultCount:  3,
			defaultSuffix: []string{".config/goose", ".local/share/goose", ".local/state/goose"},
		},
		{
			name:          "Amp/env-override",
			factory:       AmpAgent,
			envKey:        "AMP_HOME",
			envVal:        "/custom/amp",
			expectedName:  "Amp Agent",
			defaultCount:  2,
			defaultSuffix: []string{".amp", ".config/amp"},
		},
		{
			name:          "Gemini/env-override",
			factory:       GeminiAgent,
			envKey:        "GEMINI_HOME",
			envVal:        "/custom/gemini",
			expectedName:  "Gemini Agent",
			defaultCount:  1,
			defaultSuffix: []string{".gemini"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name+"/name", func(t *testing.T) {
			m := tt.factory()
			if m.Name() != tt.expectedName {
				t.Errorf("Name() = %q, want %q", m.Name(), tt.expectedName)
			}
		})

		// Test env override (if agent supports it)
		if tt.envKey != "" {
			t.Run(tt.name+"/env-override", func(t *testing.T) {
				home := t.TempDir()
				ctx := &seatbelt.Context{
					HomeDir: home,
					Env:     []string{tt.envKey + "=" + tt.envVal},
					GOOS:    "darwin",
				}
				m := tt.factory()
				rules := m.Rules(ctx)
				profile := rulesToString(rules)
				if !strings.Contains(profile, `(subpath "`+tt.envVal+`")`) {
					t.Errorf("expected env override path %q in rules, got:\n%s", tt.envVal, profile)
				}
			})
		}

		// Test defaults
		t.Run(tt.name+"/defaults", func(t *testing.T) {
			home := t.TempDir()
			ctx := &seatbelt.Context{
				HomeDir: home,
				GOOS:    "darwin",
			}
			m := tt.factory()
			rules := m.Rules(ctx)
			profile := rulesToString(rules)
			for _, suffix := range tt.defaultSuffix {
				expected := filepath.Join(home, suffix)
				if !strings.Contains(profile, expected) {
					t.Errorf("expected default dir %q in rules, got:\n%s", expected, profile)
				}
			}
		})
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `nix develop --command go test ./pkg/seatbelt/modules/ -run TestAgentModules -v`
Expected: FAIL — `CodexAgent`, `AiderAgent`, etc. not defined

- [ ] **Step 3: Create all 5 agent modules**

Create `pkg/seatbelt/modules/codex.go`:
```go
package modules

import (
	"path/filepath"

	"github.com/jskswamy/aide/pkg/seatbelt"
)

type codexAgentModule struct{}

// CodexAgent returns a module with Codex agent sandbox rules.
func CodexAgent() seatbelt.Module { return &codexAgentModule{} }

func (m *codexAgentModule) Name() string { return "Codex Agent" }

func (m *codexAgentModule) Rules(ctx *seatbelt.Context) []seatbelt.Rule {
	dirs := resolveConfigDirs(ctx, "CODEX_HOME", []string{
		filepath.Join(ctx.HomeDir, ".codex"),
	})
	return configDirRules("Codex", dirs)
}
```

Create `pkg/seatbelt/modules/aider.go`:
```go
package modules

import (
	"path/filepath"

	"github.com/jskswamy/aide/pkg/seatbelt"
)

type aiderAgentModule struct{}

// AiderAgent returns a module with Aider agent sandbox rules.
func AiderAgent() seatbelt.Module { return &aiderAgentModule{} }

func (m *aiderAgentModule) Name() string { return "Aider Agent" }

func (m *aiderAgentModule) Rules(ctx *seatbelt.Context) []seatbelt.Rule {
	// Aider has no single env var override (uses per-option AIDER_* vars).
	dirs := resolveConfigDirs(ctx, "", []string{
		filepath.Join(ctx.HomeDir, ".aider"),
	})
	return configDirRules("Aider", dirs)
}
```

Create `pkg/seatbelt/modules/goose.go`:
```go
package modules

import (
	"path/filepath"

	"github.com/jskswamy/aide/pkg/seatbelt"
)

type gooseAgentModule struct{}

// GooseAgent returns a module with Goose agent sandbox rules.
func GooseAgent() seatbelt.Module { return &gooseAgentModule{} }

func (m *gooseAgentModule) Name() string { return "Goose Agent" }

func (m *gooseAgentModule) Rules(ctx *seatbelt.Context) []seatbelt.Rule {
	dirs := resolveConfigDirs(ctx, "GOOSE_PATH_ROOT", []string{
		filepath.Join(ctx.HomeDir, ".config", "goose"),
		filepath.Join(ctx.HomeDir, ".local", "share", "goose"),
		filepath.Join(ctx.HomeDir, ".local", "state", "goose"),
	})
	return configDirRules("Goose", dirs)
}
```

Create `pkg/seatbelt/modules/amp.go`:
```go
package modules

import (
	"path/filepath"

	"github.com/jskswamy/aide/pkg/seatbelt"
)

type ampAgentModule struct{}

// AmpAgent returns a module with Amp agent sandbox rules.
func AmpAgent() seatbelt.Module { return &ampAgentModule{} }

func (m *ampAgentModule) Name() string { return "Amp Agent" }

func (m *ampAgentModule) Rules(ctx *seatbelt.Context) []seatbelt.Rule {
	dirs := resolveConfigDirs(ctx, "AMP_HOME", []string{
		filepath.Join(ctx.HomeDir, ".amp"),
		filepath.Join(ctx.HomeDir, ".config", "amp"),
	})
	return configDirRules("Amp", dirs)
}
```

Create `pkg/seatbelt/modules/gemini.go`:
```go
package modules

import (
	"path/filepath"

	"github.com/jskswamy/aide/pkg/seatbelt"
)

type geminiAgentModule struct{}

// GeminiAgent returns a module with Gemini CLI agent sandbox rules.
func GeminiAgent() seatbelt.Module { return &geminiAgentModule{} }

func (m *geminiAgentModule) Name() string { return "Gemini Agent" }

func (m *geminiAgentModule) Rules(ctx *seatbelt.Context) []seatbelt.Rule {
	dirs := resolveConfigDirs(ctx, "GEMINI_HOME", []string{
		filepath.Join(ctx.HomeDir, ".gemini"),
	})
	return configDirRules("Gemini", dirs)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `nix develop --command go test ./pkg/seatbelt/modules/ -v`
Expected: PASS (all module tests — helpers, claude, agents)

- [ ] **Step 5: Commit**

```bash
git add pkg/seatbelt/modules/codex.go pkg/seatbelt/modules/aider.go pkg/seatbelt/modules/goose.go pkg/seatbelt/modules/amp.go pkg/seatbelt/modules/gemini.go pkg/seatbelt/modules/agents_test.go
git commit -m "feat(modules): add agent modules for Codex, Aider, Goose, Amp, Gemini"
```

---

### Task 5: Delete resolver registry, register modules, and clean up passthrough

**Files:**
- Modify: `internal/launcher/agentcfg.go`
- Modify: `internal/launcher/agentcfg_test.go`
- Modify: `internal/launcher/passthrough.go:137-140`

**Important:** The resolver deletion and passthrough cleanup MUST happen atomically — deleting `ResolveAgentConfigDirs` from `agentcfg.go` without removing its call in `passthrough.go` causes a build break.

- [ ] **Step 1: Rewrite `agentcfg.go`**

Replace the entire contents of `internal/launcher/agentcfg.go` with:

```go
// Package launcher orchestrates agent discovery, sandbox setup, and process execution.
package launcher

import (
	"path/filepath"

	"github.com/jskswamy/aide/pkg/seatbelt"
	"github.com/jskswamy/aide/pkg/seatbelt/modules"
)

// agentModuleResolvers maps agent base names to their seatbelt module factory.
var agentModuleResolvers = map[string]func() seatbelt.Module{
	"claude": modules.ClaudeAgent,
	"codex":  modules.CodexAgent,
	"aider":  modules.AiderAgent,
	"goose":  modules.GooseAgent,
	"amp":    modules.AmpAgent,
	"gemini": modules.GeminiAgent,
}

// ResolveAgentModule returns the seatbelt module for the named agent, or nil.
func ResolveAgentModule(agentName string) seatbelt.Module {
	base := filepath.Base(agentName)
	if factory, ok := agentModuleResolvers[base]; ok {
		return factory()
	}
	return nil
}
```

- [ ] **Step 2: Remove dead code from `passthrough.go`**

In `internal/launcher/passthrough.go`, delete lines 137-140:

```go
	// DELETE these 4 lines:
	// Agent config dirs are now handled by the agent module in the seatbelt profile.
	// For completeness, resolve them but they are encoded in the module itself.
	homeDir, _ := os.UserHomeDir()
	_ = ResolveAgentConfigDirs(name, os.Environ(), homeDir)
```

Note: `os` is still used elsewhere in `passthrough.go` — do NOT remove the import.

- [ ] **Step 3: Rewrite `agentcfg_test.go`**

Replace the entire contents of `internal/launcher/agentcfg_test.go` with:

```go
package launcher

import (
	"testing"
)

func TestResolveAgentModule_KnownAgents(t *testing.T) {
	agents := []string{"claude", "codex", "aider", "goose", "amp", "gemini"}
	for _, name := range agents {
		m := ResolveAgentModule(name)
		if m == nil {
			t.Errorf("ResolveAgentModule(%q) returned nil, expected module", name)
		}
	}
}

func TestResolveAgentModule_UnknownAgent(t *testing.T) {
	m := ResolveAgentModule("vim")
	if m != nil {
		t.Errorf("ResolveAgentModule(vim) should return nil, got %v", m)
	}
}

func TestResolveAgentModule_PathBasename(t *testing.T) {
	m := ResolveAgentModule("/usr/local/bin/claude")
	if m == nil {
		t.Error("ResolveAgentModule with full path should resolve by basename")
	}
	if m.Name() != "Claude Agent" {
		t.Errorf("expected 'Claude Agent', got %q", m.Name())
	}
}
```

- [ ] **Step 4: Verify compilation and tests pass**

Run: `nix develop --command go build ./...`
Expected: SUCCESS (no dangling references to deleted resolver)

Run: `nix develop --command go test ./internal/launcher/ -run TestResolveAgentModule -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/launcher/agentcfg.go internal/launcher/agentcfg_test.go internal/launcher/passthrough.go
git commit -m "refactor(launcher): delete resolver registry, register all agent modules, clean up passthrough"
```

---

### Task 6: Fix `policy.Env` propagation in launcher.go

**Files:**
- Modify: `internal/launcher/launcher.go:203`

- [ ] **Step 1: Add `policy.Env = env` in the sandbox block**

In `internal/launcher/launcher.go`, inside the `if policy != nil` block (line 203), add `policy.Env = env` before the agent module assignment. The block should read:

```go
		if policy != nil {
			// Propagate merged env so modules see CLAUDE_CONFIG_DIR etc.
			policy.Env = env
			// 12b. Set agent module for sandbox profile
			policy.AgentModule = ResolveAgentModule(agentName)

			cmd := &exec.Cmd{
```

This is a one-line addition at line 204 (between the `if policy != nil {` and the agent module line).

- [ ] **Step 2: Verify compilation**

Run: `nix develop --command go build ./...`
Expected: SUCCESS

- [ ] **Step 3: Commit**

```bash
git add internal/launcher/launcher.go
git commit -m "fix(launcher): propagate merged env to policy.Env for module env lookups"
```

---

### Task 7: Integration test — full profile with `CLAUDE_CONFIG_DIR`

**Files:**
- Modify: `internal/sandbox/darwin_test.go`

- [ ] **Step 1: Write the integration test**

Add to `internal/sandbox/darwin_test.go`:

```go
func TestSeatbeltProfile_CustomClaudeConfigDir(t *testing.T) {
	customDir := "/Users/testuser/.claude-work"
	policy := DefaultPolicy("/tmp/proj", "/tmp/rt", "/tmp",
		[]string{"CLAUDE_CONFIG_DIR=" + customDir},
	)
	policy.AgentModule = modules.ClaudeAgent()

	profile, err := generateSeatbeltProfile(policy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Custom config dir should appear as a subpath grant
	if !strings.Contains(profile, `(subpath "`+customDir+`")`) {
		t.Errorf("profile should contain custom config dir %q", customDir)
	}

	// Default .claude should NOT appear as a standalone config dir subpath
	// (it may still appear in the user data section via HomeSubpath, which is fine)

	// Runtime paths should always be present
	if !strings.Contains(profile, ".cache/claude") {
		t.Error("profile should still contain .cache/claude regardless of env override")
	}
}
```

- [ ] **Step 2: Run the test**

Run: `nix develop --command go test ./internal/sandbox/ -run TestSeatbeltProfile_CustomClaudeConfigDir -v`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/sandbox/darwin_test.go
git commit -m "test(sandbox): add integration test for CLAUDE_CONFIG_DIR in seatbelt profile"
```

---

### Task 8: Run full test suite

- [ ] **Step 1: Run all tests**

Run: `nix develop --command go test ./...`
Expected: ALL PASS

- [ ] **Step 2: Run vet and lint**

Run: `nix develop --command go vet ./...`
Expected: No issues

- [ ] **Step 3: Final commit if any fixups needed**

If any test failures or lint issues were found and fixed, commit those fixes.
