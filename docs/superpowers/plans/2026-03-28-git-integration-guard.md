# Git Integration Guard & Git Remote Guard Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a self-contained git-integration guard (always) for local git config reading, and a git-remote guard (opt-in) for remote operations, with the EnableGuard capability plumbing to activate opt-in guards.

**Architecture:** Two new guards in `pkg/seatbelt/guards/`. The git-integration guard uses `go-git/v5/plumbing/format/config` to parse gitconfig and discover all referenced files (includes, excludesFile, attributesFile). The git-remote guard enables SSH key reading and network outbound for git push/pull, activated via a new `EnableGuard` field on the Capability struct that flows into `GuardsExtra`.

**Tech Stack:** Go, `go-git/v5/plumbing/format/config`, macOS Seatbelt profiles

**Spec:** `docs/superpowers/specs/2026-03-28-git-integration-guard-design.md`

---

### Task 1: Add `go-git` dependency

**Files:**
- Modify: `go.mod`
- Modify: `go.sum`

- [ ] **Step 1: Add go-git config parser dependency**

```bash
go get github.com/go-git/go-git/v5@latest
```

- [ ] **Step 2: Tidy modules**

```bash
go mod tidy
```

- [ ] **Step 3: Verify dependency is available**

```bash
go list github.com/go-git/go-git/v5/plumbing/format/config
```

Expected: prints the package path without error.

- [ ] **Step 4: Commit**

```bash
git add go.mod go.sum
git commit -m "deps: add go-git/v5 for gitconfig parsing"
```

---

### Task 2: Git config parser library

**Files:**
- Create: `pkg/seatbelt/guards/gitconfig.go`
- Create: `pkg/seatbelt/guards/gitconfig_test.go`

This is a standalone library that parses git configuration and returns all
file paths that git reads. It does NOT generate seatbelt rules — that's the
guard's job. This separation makes it testable without seatbelt dependencies.

- [ ] **Step 1: Write test for basic gitconfig parsing**

In `pkg/seatbelt/guards/gitconfig_test.go`:

```go
package guards_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jskswamy/aide/pkg/seatbelt/guards"
)

func TestParseGitConfig_BasicConfig(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}

	gitconfig := filepath.Join(home, ".gitconfig")
	if err := os.WriteFile(gitconfig, []byte(`[user]
	name = Test User
	email = test@example.com
`), 0o644); err != nil {
		t.Fatal(err)
	}

	result := guards.ParseGitConfig(home, "", nil)
	if result.Err != nil {
		t.Fatalf("unexpected error: %v", result.Err)
	}

	// Should always include well-known paths
	assertContains(t, result.ConfigFiles, gitconfig)
}

func assertContains(t *testing.T, slice []string, item string) {
	t.Helper()
	for _, s := range slice {
		if s == item {
			return
		}
	}
	t.Errorf("expected %v to contain %q", slice, item)
}

func assertNotContains(t *testing.T, slice []string, item string) {
	t.Helper()
	for _, s := range slice {
		if s == item {
			t.Errorf("expected %v to NOT contain %q", slice, item)
			return
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./pkg/seatbelt/guards/ -run TestParseGitConfig_BasicConfig -v
```

Expected: FAIL — `ParseGitConfig` not defined.

- [ ] **Step 3: Implement ParseGitConfig with basic config parsing**

In `pkg/seatbelt/guards/gitconfig.go`:

```go
package guards

import (
	"os"
	"path/filepath"
	"strings"

	gitconfig "github.com/go-git/go-git/v5/plumbing/format/config"
)

// GitConfigResult holds all file paths discovered from git configuration.
type GitConfigResult struct {
	// ConfigFiles are gitconfig files (global, system, includes).
	ConfigFiles []string
	// ExcludesFile is core.excludesFile (resolved).
	ExcludesFile string
	// AttributesFile is core.attributesFile (resolved).
	AttributesFile string
	// Err is set if parsing failed (caller should fall back to defaults).
	Err error
	// Warnings for non-fatal issues.
	Warnings []string
}

// AllPaths returns all discovered paths deduplicated.
func (r *GitConfigResult) AllPaths() []string {
	seen := make(map[string]bool)
	var out []string
	add := func(p string) {
		if p != "" && !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	for _, f := range r.ConfigFiles {
		add(f)
	}
	add(r.ExcludesFile)
	add(r.AttributesFile)
	return out
}

const maxIncludeDepth = 10

// ParseGitConfig parses git configuration starting from the global config
// and resolves includes, excludesFile, and attributesFile.
// homeDir is the user's home directory. projectRoot is used for includeIf
// gitdir: condition evaluation (may be empty). envLookup is used for
// XDG_CONFIG_HOME resolution (may be nil for tests).
func ParseGitConfig(homeDir, projectRoot string, envLookup func(string) (string, bool)) *GitConfigResult {
	result := &GitConfigResult{}

	// Determine config file locations
	globalConfig := filepath.Join(homeDir, ".gitconfig")
	xdgConfig := xdgGitConfigPath(homeDir, envLookup)
	systemConfig := "/etc/gitconfig"

	// Well-known paths are always included (even if they don't exist yet)
	wellKnown := []string{
		globalConfig,
		xdgConfig,
		systemConfig,
		filepath.Join(homeDir, ".config", "git", "ignore"),
		filepath.Join(homeDir, ".config", "git", "attributes"),
	}
	for _, p := range wellKnown {
		result.ConfigFiles = append(result.ConfigFiles, p)
	}

	// Set defaults for excludes/attributes
	result.ExcludesFile = filepath.Join(homeDir, ".gitignore")
	result.AttributesFile = filepath.Join(homeDir, ".config", "git", "attributes")

	// Parse global config
	parsed := parseConfigFile(globalConfig)
	if parsed == nil {
		// Try XDG location
		parsed = parseConfigFile(xdgConfig)
	}
	if parsed == nil {
		result.Warnings = append(result.Warnings, "no global gitconfig found, using defaults")
		return result
	}

	// Extract core.excludesFile
	if val := configValue(parsed, "core", "", "excludesFile"); val != "" {
		result.ExcludesFile = expandTilde(val, homeDir)
	}

	// Extract core.attributesFile
	if val := configValue(parsed, "core", "", "attributesFile"); val != "" {
		result.AttributesFile = expandTilde(val, homeDir)
	}

	// Resolve includes
	resolveIncludes(parsed, homeDir, projectRoot, result, 0)

	return result
}

// ParseGitConfigWithEnv is like ParseGitConfig but checks for
// GIT_CONFIG_GLOBAL and GIT_CONFIG_SYSTEM env overrides.
// When overrides are set, they REPLACE the default paths (not append).
func ParseGitConfigWithEnv(homeDir, projectRoot string, envLookup func(string) (string, bool)) *GitConfigResult {
	result := &GitConfigResult{}

	globalConfig := filepath.Join(homeDir, ".gitconfig")
	xdgConfig := xdgGitConfigPath(homeDir, envLookup)
	systemConfig := "/etc/gitconfig"

	// Check for env overrides — replace defaults when set
	if val, ok := envLookup("GIT_CONFIG_GLOBAL"); ok && val != "" {
		globalConfig = expandTilde(val, homeDir)
	}
	if val, ok := envLookup("GIT_CONFIG_SYSTEM"); ok && val != "" {
		systemConfig = val
	}

	// Well-known paths (with overrides applied)
	result.ConfigFiles = []string{
		globalConfig,
		xdgConfig,
		systemConfig,
		filepath.Join(homeDir, ".config", "git", "ignore"),
		filepath.Join(homeDir, ".config", "git", "attributes"),
	}

	// Defaults
	result.ExcludesFile = filepath.Join(homeDir, ".gitignore")
	result.AttributesFile = filepath.Join(homeDir, ".config", "git", "attributes")

	// Parse global config (which may be the overridden path)
	parsed := parseConfigFile(globalConfig)
	if parsed == nil {
		parsed = parseConfigFile(xdgConfig)
	}
	if parsed == nil {
		result.Warnings = append(result.Warnings, "no global gitconfig found, using defaults")
		return result
	}

	if val := configValue(parsed, "core", "", "excludesFile"); val != "" {
		result.ExcludesFile = expandTilde(val, homeDir)
	}
	if val := configValue(parsed, "core", "", "attributesFile"); val != "" {
		result.AttributesFile = expandTilde(val, homeDir)
	}

	resolveIncludes(parsed, homeDir, projectRoot, result, 0)

	return result
}

func parseConfigFile(path string) *gitconfig.Config {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	cfg := gitconfig.New()
	if err := cfg.Unmarshal(data); err != nil {
		return nil
	}
	return cfg
}

func configValue(cfg *gitconfig.Config, section, subsection, key string) string {
	s := cfg.Section(section)
	if s == nil {
		return ""
	}
	if subsection != "" {
		ss := s.Subsection(subsection)
		if ss == nil {
			return ""
		}
		return ss.Option(key)
	}
	return s.Option(key)
}

func resolveIncludes(cfg *gitconfig.Config, homeDir, projectRoot string, result *GitConfigResult, depth int) {
	if depth >= maxIncludeDepth {
		result.Warnings = append(result.Warnings, "max include depth reached")
		return
	}

	// Process [include] sections
	includeSection := cfg.Section("include")
	if includeSection != nil {
		for _, opt := range includeSection.Options {
			if opt.Key == "path" {
				path := expandTilde(opt.Value, homeDir)
				if !filepath.IsAbs(path) {
					// Relative includes are relative to the config file
					// For simplicity, resolve relative to home
					path = filepath.Join(homeDir, path)
				}
				resolved := resolveSymlink(path)
				result.ConfigFiles = append(result.ConfigFiles, resolved)
				// Recursively parse included file
				if parsed := parseConfigFile(resolved); parsed != nil {
					resolveIncludes(parsed, homeDir, projectRoot, result, depth+1)
				}
			}
		}
	}

	// Process [includeIf] sections
	for _, ss := range cfg.Section("includeIf").Subsections {
		if !evaluateIncludeCondition(ss.Name, projectRoot, homeDir) {
			continue
		}
		path := ss.Option("path")
		if path == "" {
			continue
		}
		path = expandTilde(path, homeDir)
		if !filepath.IsAbs(path) {
			path = filepath.Join(homeDir, path)
		}
		resolved := resolveSymlink(path)
		result.ConfigFiles = append(result.ConfigFiles, resolved)
		if parsed := parseConfigFile(resolved); parsed != nil {
			resolveIncludes(parsed, homeDir, projectRoot, result, depth+1)
		}
	}
}

func evaluateIncludeCondition(condition, projectRoot, homeDir string) bool {
	if projectRoot == "" {
		return false
	}

	// Handle gitdir: and gitdir/i: conditions
	if strings.HasPrefix(condition, "gitdir:") {
		pattern := strings.TrimPrefix(condition, "gitdir:")
		return matchGitDir(pattern, projectRoot, homeDir, false)
	}
	if strings.HasPrefix(condition, "gitdir/i:") {
		pattern := strings.TrimPrefix(condition, "gitdir/i:")
		return matchGitDir(pattern, projectRoot, homeDir, true)
	}

	return false
}

func matchGitDir(pattern, projectRoot, homeDir string, caseInsensitive bool) bool {
	gitDir := filepath.Join(projectRoot, ".git")

	// Expand ~ in pattern (common: gitdir:~/work/)
	pattern = expandTilde(pattern, homeDir)

	// Trailing / means match prefix
	if strings.HasSuffix(pattern, "/") {
		prefix := pattern
		target := gitDir + "/"
		if caseInsensitive {
			prefix = strings.ToLower(prefix)
			target = strings.ToLower(target)
		}
		return strings.HasPrefix(target, prefix)
	}

	// Exact match
	if caseInsensitive {
		return strings.EqualFold(gitDir, pattern)
	}
	return gitDir == pattern
}

func expandTilde(path, homeDir string) string {
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(homeDir, path[2:])
	}
	if path == "~" {
		return homeDir
	}
	return path
}

func resolveSymlink(path string) string {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return path // return original if resolution fails
	}
	return resolved
}

// xdgGitConfigPath returns the XDG git config path. Uses envLookup to
// check XDG_CONFIG_HOME (guards must not call os.Getenv directly).
func xdgGitConfigPath(homeDir string, envLookup func(string) (string, bool)) string {
	if envLookup != nil {
		if xdg, ok := envLookup("XDG_CONFIG_HOME"); ok && xdg != "" {
			return filepath.Join(xdg, "git", "config")
		}
	}
	return filepath.Join(homeDir, ".config", "git", "config")
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./pkg/seatbelt/guards/ -run TestParseGitConfig_BasicConfig -v
```

Expected: PASS

- [ ] **Step 5: Write test for custom excludesFile and attributesFile**

Append to `pkg/seatbelt/guards/gitconfig_test.go`:

```go
func TestParseGitConfig_CustomExcludesAndAttributes(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}

	gitconfig := filepath.Join(home, ".gitconfig")
	if err := os.WriteFile(gitconfig, []byte(`[core]
	excludesFile = ~/my-ignores
	attributesFile = ~/my-attributes
`), 0o644); err != nil {
		t.Fatal(err)
	}

	result := guards.ParseGitConfig(home, "", nil)
	if result.Err != nil {
		t.Fatalf("unexpected error: %v", result.Err)
	}

	expectedExcludes := filepath.Join(home, "my-ignores")
	if result.ExcludesFile != expectedExcludes {
		t.Errorf("expected excludesFile %q, got %q", expectedExcludes, result.ExcludesFile)
	}

	expectedAttributes := filepath.Join(home, "my-attributes")
	if result.AttributesFile != expectedAttributes {
		t.Errorf("expected attributesFile %q, got %q", expectedAttributes, result.AttributesFile)
	}
}
```

- [ ] **Step 6: Run test**

```bash
go test ./pkg/seatbelt/guards/ -run TestParseGitConfig_CustomExcludes -v
```

Expected: PASS

- [ ] **Step 7: Write test for include resolution**

Append to `pkg/seatbelt/guards/gitconfig_test.go`:

```go
func TestParseGitConfig_IncludeResolution(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}

	// Create an included config file
	workConfig := filepath.Join(home, ".gitconfig-work")
	if err := os.WriteFile(workConfig, []byte(`[user]
	email = work@company.com
`), 0o644); err != nil {
		t.Fatal(err)
	}

	gitconfig := filepath.Join(home, ".gitconfig")
	if err := os.WriteFile(gitconfig, []byte(`[include]
	path = ~/.gitconfig-work
`), 0o644); err != nil {
		t.Fatal(err)
	}

	result := guards.ParseGitConfig(home, "", nil)
	if result.Err != nil {
		t.Fatalf("unexpected error: %v", result.Err)
	}

	assertContains(t, result.ConfigFiles, workConfig)
}

func TestParseGitConfig_IncludeIfGitdir(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	projectRoot := filepath.Join(tmp, "work", "myproject")
	if err := os.MkdirAll(filepath.Join(projectRoot, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}

	workConfig := filepath.Join(home, ".gitconfig-work")
	if err := os.WriteFile(workConfig, []byte(`[user]
	email = work@company.com
`), 0o644); err != nil {
		t.Fatal(err)
	}

	gitconfig := filepath.Join(home, ".gitconfig")
	content := `[includeIf "gitdir:` + filepath.Join(tmp, "work") + `/"]
	path = ~/.gitconfig-work
`
	if err := os.WriteFile(gitconfig, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	// With matching project root
	result := guards.ParseGitConfig(home, projectRoot, nil)
	assertContains(t, result.ConfigFiles, workConfig)

	// With non-matching project root
	otherProject := filepath.Join(tmp, "personal", "myproject")
	if err := os.MkdirAll(filepath.Join(otherProject, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	result2 := guards.ParseGitConfig(home, otherProject, nil)
	assertNotContains(t, result2.ConfigFiles, workConfig)
}
```

- [ ] **Step 8: Run tests**

```bash
go test ./pkg/seatbelt/guards/ -run TestParseGitConfig -v
```

Expected: all PASS

- [ ] **Step 9: Write test for symlink resolution**

Append to `pkg/seatbelt/guards/gitconfig_test.go`:

```go
func TestParseGitConfig_SymlinkResolution(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	real := filepath.Join(tmp, "real")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}

	// Create real config file
	realConfig := filepath.Join(real, "gitconfig-work")
	if err := os.WriteFile(realConfig, []byte(`[user]
	email = work@company.com
`), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create symlink in home
	symlink := filepath.Join(home, ".gitconfig-work")
	if err := os.Symlink(realConfig, symlink); err != nil {
		t.Fatal(err)
	}

	// Main config includes the symlink
	if err := os.WriteFile(filepath.Join(home, ".gitconfig"), []byte(`[include]
	path = ~/.gitconfig-work
`), 0o644); err != nil {
		t.Fatal(err)
	}

	result := guards.ParseGitConfig(home, "", nil)

	// Should contain the RESOLVED (real) path, not the symlink
	assertContains(t, result.ConfigFiles, realConfig)
}
```

- [ ] **Step 10: Run symlink test**

```bash
go test ./pkg/seatbelt/guards/ -run TestParseGitConfig_SymlinkResolution -v
```

Expected: PASS

- [ ] **Step 11: Write test for ParseGitConfigWithEnv**

Append to `pkg/seatbelt/guards/gitconfig_test.go`:

```go
func TestParseGitConfigWithEnv_GlobalOverride(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}

	customConfig := filepath.Join(tmp, "custom-gitconfig")
	if err := os.WriteFile(customConfig, []byte(`[core]
	excludesFile = ~/custom-ignores
`), 0o644); err != nil {
		t.Fatal(err)
	}

	envLookup := func(key string) (string, bool) {
		if key == "GIT_CONFIG_GLOBAL" {
			return customConfig, true
		}
		return "", false
	}

	result := guards.ParseGitConfigWithEnv(home, "", envLookup)

	// Should contain the override path instead of ~/.gitconfig
	assertContains(t, result.ConfigFiles, customConfig)

	// Should pick up core.excludesFile from the override config
	expectedExcludes := filepath.Join(home, "custom-ignores")
	if result.ExcludesFile != expectedExcludes {
		t.Errorf("expected excludesFile %q, got %q", expectedExcludes, result.ExcludesFile)
	}
}

func TestParseGitConfigWithEnv_SystemOverride(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}

	customSystem := filepath.Join(tmp, "system-gitconfig")
	envLookup := func(key string) (string, bool) {
		if key == "GIT_CONFIG_SYSTEM" {
			return customSystem, true
		}
		return "", false
	}

	result := guards.ParseGitConfigWithEnv(home, "", envLookup)
	assertContains(t, result.ConfigFiles, customSystem)
}
```

- [ ] **Step 12: Run env tests**

```bash
go test ./pkg/seatbelt/guards/ -run TestParseGitConfigWithEnv -v
```

Expected: all PASS

- [ ] **Step 13: Write test for fallback on missing config**

Append to `pkg/seatbelt/guards/gitconfig_test.go`:

```go
func TestParseGitConfig_MissingConfig(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "empty-home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}

	result := guards.ParseGitConfig(home, "", nil)
	// Should NOT error — graceful degradation
	if result.Err != nil {
		t.Fatalf("should not error on missing config: %v", result.Err)
	}

	// Should still have well-known defaults
	if result.ExcludesFile != filepath.Join(home, ".gitignore") {
		t.Errorf("expected default excludesFile, got %q", result.ExcludesFile)
	}

	// Should have warning
	if len(result.Warnings) == 0 {
		t.Error("expected warning about missing config")
	}
}

func TestParseGitConfig_MaxIncludeDepth(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}

	// Create a self-referencing include (circular)
	gitconfig := filepath.Join(home, ".gitconfig")
	if err := os.WriteFile(gitconfig, []byte(`[include]
	path = ~/.gitconfig
`), 0o644); err != nil {
		t.Fatal(err)
	}

	result := guards.ParseGitConfig(home, "", nil)
	// Should not hang or panic — depth limiting should kick in
	if result.Err != nil {
		t.Fatalf("should not error: %v", result.Err)
	}

	// Should have warning about max depth
	hasDepthWarning := false
	for _, w := range result.Warnings {
		if strings.Contains(w, "max include depth") {
			hasDepthWarning = true
			break
		}
	}
	if !hasDepthWarning {
		t.Error("expected max include depth warning")
	}
}
```

- [ ] **Step 14: Run all gitconfig tests**

```bash
go test ./pkg/seatbelt/guards/ -run "TestParseGitConfig|TestParseGitConfigWithEnv" -v
```

Expected: all PASS

- [ ] **Step 15: Commit**

```bash
git add pkg/seatbelt/guards/gitconfig.go pkg/seatbelt/guards/gitconfig_test.go
git commit -m "feat: add git config parser library with include resolution"
```

---

### Task 3: Git integration guard

**Files:**
- Create: `pkg/seatbelt/guards/guard_git_integration.go`
- Create: `pkg/seatbelt/guards/guard_git_integration_test.go`
- Modify: `pkg/seatbelt/guards/registry.go:14-24`

- [ ] **Step 1: Write test for guard metadata**

In `pkg/seatbelt/guards/guard_git_integration_test.go`:

```go
package guards_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jskswamy/aide/pkg/seatbelt"
	"github.com/jskswamy/aide/pkg/seatbelt/guards"
)

func TestGuard_GitIntegration_Metadata(t *testing.T) {
	g := guards.GitIntegrationGuard()

	if g.Name() != "git-integration" {
		t.Errorf("expected Name() = %q, got %q", "git-integration", g.Name())
	}
	if g.Type() != "always" {
		t.Errorf("expected Type() = %q, got %q", "always", g.Type())
	}
	if g.Description() == "" {
		t.Error("expected non-empty Description()")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./pkg/seatbelt/guards/ -run TestGuard_GitIntegration_Metadata -v
```

Expected: FAIL — `GitIntegrationGuard` not defined.

- [ ] **Step 3: Implement the guard**

In `pkg/seatbelt/guards/guard_git_integration.go`:

```go
// Git integration guard for macOS Seatbelt profiles.
//
// Parses git configuration to discover all referenced files and emits
// read-only allow rules. Uses go-git for gitconfig parsing with custom
// include resolution.

package guards

import (
	"fmt"

	"github.com/jskswamy/aide/pkg/seatbelt"
)

type gitIntegrationGuard struct{}

// GitIntegrationGuard returns a Guard that allows read access to all
// git configuration files discovered through gitconfig parsing.
func GitIntegrationGuard() seatbelt.Guard { return &gitIntegrationGuard{} }

func (g *gitIntegrationGuard) Name() string { return "git-integration" }
func (g *gitIntegrationGuard) Type() string { return "always" }
func (g *gitIntegrationGuard) Description() string {
	return "Git configuration files (read-only, parsed from gitconfig)"
}

func (g *gitIntegrationGuard) Rules(ctx *seatbelt.Context) seatbelt.GuardResult {
	if ctx == nil {
		return seatbelt.GuardResult{}
	}

	home := ctx.HomeDir
	if home == "" {
		return seatbelt.GuardResult{}
	}

	// Parse git config with env overrides
	gcResult := ParseGitConfigWithEnv(home, ctx.ProjectRoot, ctx.EnvLookup)

	var result seatbelt.GuardResult

	// Report env overrides
	if val, ok := ctx.EnvLookup("GIT_CONFIG_GLOBAL"); ok && val != "" {
		result.Overrides = append(result.Overrides, seatbelt.Override{
			EnvVar:      "GIT_CONFIG_GLOBAL",
			Value:       val,
			DefaultPath: home + "/.gitconfig",
		})
	}
	if val, ok := ctx.EnvLookup("GIT_CONFIG_SYSTEM"); ok && val != "" {
		result.Overrides = append(result.Overrides, seatbelt.Override{
			EnvVar:      "GIT_CONFIG_SYSTEM",
			Value:       val,
			DefaultPath: "/etc/gitconfig",
		})
	}

	// Collect all paths
	allPaths := gcResult.AllPaths()
	if len(allPaths) == 0 {
		return result
	}

	// Build allow rules
	var pathExprs []string
	for _, p := range allPaths {
		pathExprs = append(pathExprs, fmt.Sprintf(`    (literal "%s")`, p))
	}

	result.Rules = append(result.Rules,
		seatbelt.SectionAllow("Git configuration (read-only)"),
		seatbelt.AllowRule(fmt.Sprintf("(allow file-read*\n%s)", joinLines(pathExprs))),
	)

	return result
}

func joinLines(lines []string) string {
	result := ""
	for i, l := range lines {
		if i > 0 {
			result += "\n"
		}
		result += l
	}
	return result
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./pkg/seatbelt/guards/ -run TestGuard_GitIntegration_Metadata -v
```

Expected: PASS

- [ ] **Step 5: Write test for guard rules output**

Append to `pkg/seatbelt/guards/guard_git_integration_test.go`:

```go
func TestGuard_GitIntegration_WellKnownPaths(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}

	// Create a minimal gitconfig
	if err := os.WriteFile(filepath.Join(home, ".gitconfig"), []byte(`[user]
	name = Test
`), 0o644); err != nil {
		t.Fatal(err)
	}

	g := guards.GitIntegrationGuard()
	ctx := &seatbelt.Context{
		HomeDir:     home,
		ProjectRoot: "/project",
	}
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	// Should have all well-known git config paths
	expectedPaths := []string{
		filepath.Join(home, ".gitconfig"),
		filepath.Join(home, ".config", "git", "config"),
		filepath.Join(home, ".config", "git", "ignore"),
		filepath.Join(home, ".config", "git", "attributes"),
		filepath.Join(home, ".gitignore"), // default excludesFile
	}
	for _, p := range expectedPaths {
		if !strings.Contains(output, `"`+p+`"`) {
			t.Errorf("expected output to contain path %q", p)
		}
	}

	// Should be read-only
	if !strings.Contains(output, "file-read*") {
		t.Error("expected file-read* rule")
	}
	if strings.Contains(output, "file-write*") {
		t.Error("git config paths should be read-only")
	}
}

func TestGuard_GitIntegration_CustomExcludes(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(home, ".gitconfig"), []byte(`[core]
	excludesFile = ~/custom-ignores
`), 0o644); err != nil {
		t.Fatal(err)
	}

	g := guards.GitIntegrationGuard()
	ctx := &seatbelt.Context{HomeDir: home}
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	customPath := filepath.Join(home, "custom-ignores")
	if !strings.Contains(output, `"`+customPath+`"`) {
		t.Errorf("expected custom excludesFile path %q in output", customPath)
	}
}

func TestGuard_GitIntegration_EnvOverride(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	customConfig := filepath.Join(tmp, "custom-gitconfig")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(customConfig, []byte(`[user]
	name = Custom
`), 0o644); err != nil {
		t.Fatal(err)
	}

	g := guards.GitIntegrationGuard()
	ctx := &seatbelt.Context{
		HomeDir: home,
		Env:     []string{"GIT_CONFIG_GLOBAL=" + customConfig},
	}
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	if !strings.Contains(output, `"`+customConfig+`"`) {
		t.Errorf("expected GIT_CONFIG_GLOBAL path %q in output", customConfig)
	}

	// Should report override
	if len(result.Overrides) == 0 {
		t.Error("expected override for GIT_CONFIG_GLOBAL")
	}
}

func TestGuard_GitIntegration_NilContext(t *testing.T) {
	g := guards.GitIntegrationGuard()
	result := g.Rules(nil)
	if len(result.Rules) != 0 {
		t.Error("expected no rules for nil context")
	}
}
```

- [ ] **Step 6: Run tests**

```bash
go test ./pkg/seatbelt/guards/ -run TestGuard_GitIntegration -v
```

Expected: all PASS

- [ ] **Step 7: Register the guard in registry.go**

Modify `pkg/seatbelt/guards/registry.go` — add `GitIntegrationGuard()` after
`FilesystemGuard()` and before `KeychainGuard()` in the always guards block:

```go
func init() {
	// always guards first
	builtinGuards = append(builtinGuards,
		BaseGuard(),
		SystemRuntimeGuard(),
		NetworkGuard(),
		FilesystemGuard(),
		GitIntegrationGuard(), // git config parsing (read-only)
		KeychainGuard(),
		NodeToolchainGuard(),
		NixToolchainGuard(),
	)
	// ... default guards unchanged ...
```

- [ ] **Step 8: Update registry test counts**

In `pkg/seatbelt/guards/registry_test.go`:

- Line 11: change `10` to `11` (total guards: was 10, now 11 with git-integration)
- Line 43: change `7` to `8` (always guards: was 7, now 8 with git-integration)

Note: git-remote (opt-in) is added in Task 7 and will bump these again.

- [ ] **Step 9: Run full guard test suite**

```bash
go test ./pkg/seatbelt/guards/ -v
```

Expected: all PASS (including new git-integration tests and updated registry counts).

- [ ] **Step 10: Commit**

```bash
git add pkg/seatbelt/guards/guard_git_integration.go pkg/seatbelt/guards/guard_git_integration_test.go pkg/seatbelt/guards/registry.go pkg/seatbelt/guards/registry_test.go
git commit -m "feat: add git-integration always guard with config parsing"
```

---

### Task 4: Remove git paths from filesystem guard

**Files:**
- Modify: `pkg/seatbelt/guards/guard_filesystem.go:57-63`
- Modify: `pkg/seatbelt/guards/filesystem_test.go`

- [ ] **Step 1: Remove git config section from filesystem guard**

In `pkg/seatbelt/guards/guard_filesystem.go`, remove lines 57-63 (the git
config allow block). Change:

```go
		rules = append(rules,
			// Git configuration (read-only)
			seatbelt.SectionAllow("Git configuration (read-only)"),
			seatbelt.AllowRule(`(allow file-read*
    `+seatbelt.HomeLiteral(home, ".gitconfig")+`
    `+seatbelt.HomeSubpath(home, ".config/git")+`
)`),
```

To just remove these lines entirely. The next section (aide's own paths)
follows immediately after.

- [ ] **Step 2: Update filesystem tests — remove `.gitconfig` assertions**

In `pkg/seatbelt/guards/filesystem_test.go`, update the tests:

In `TestFilesystem_ScopedReadablePaths` (line 61-65): remove the assertions
for `.gitconfig` and `.config/git`.

In `TestFilesystem_MixedConfig` (line 152): remove the `.gitconfig` assertion.

In `TestGuard_Filesystem_CtxPaths` (line 296): remove the `.gitconfig`
assertion.

In `TestFilesystemGuard_ScopedHomeReads` (lines 213-215): remove `.gitconfig`
and `.config/git` from `narrowPaths` slice.

In `TestFilesystemGuard_NarrowBaseline` (lines 321-322): remove `.gitconfig`
and `.config/git` from `allowedPaths` slice.

- [ ] **Step 3: Run filesystem tests**

```bash
go test ./pkg/seatbelt/guards/ -run TestFilesystem -v
```

Expected: all PASS

- [ ] **Step 4: Run full guard test suite**

```bash
go test ./pkg/seatbelt/guards/ -v
```

Expected: all PASS

- [ ] **Step 5: Commit**

```bash
git add pkg/seatbelt/guards/guard_filesystem.go pkg/seatbelt/guards/filesystem_test.go
git commit -m "refactor: remove git config paths from filesystem guard

Git config paths are now owned by the git-integration guard."
```

---

### Task 5: Add `~/.git-credentials` to dev-credentials guard

**Files:**
- Modify: `pkg/seatbelt/guards/guard_dev_credentials.go:27-36`
- Modify: `pkg/seatbelt/guards/guard_dev_credentials_test.go`

- [ ] **Step 1: Write test for `.git-credentials` denial**

Append to `pkg/seatbelt/guards/guard_dev_credentials_test.go`:

```go
func TestDevCredentials_GitCredentials(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}

	// Create the credentials file
	credFile := filepath.Join(home, ".git-credentials")
	if err := os.WriteFile(credFile, []byte("https://user:token@github.com"), 0o600); err != nil {
		t.Fatal(err)
	}

	g := guards.DevCredentialsGuard()
	ctx := &seatbelt.Context{HomeDir: home}
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	if !strings.Contains(output, credFile) {
		t.Errorf("expected .git-credentials path %q in deny rules", credFile)
	}
	if !strings.Contains(output, "deny file-read-data") {
		t.Error("expected deny file-read-data rule for .git-credentials")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./pkg/seatbelt/guards/ -run TestDevCredentials_GitCredentials -v
```

Expected: FAIL — `.git-credentials` not in credentialPaths.

- [ ] **Step 3: Add `.git-credentials` to credentialPaths**

In `pkg/seatbelt/guards/guard_dev_credentials.go`, add to the
`credentialPaths` slice (after the `.gem/credentials` entry):

```go
	{".git-credentials", false},           // Git plaintext credential store
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./pkg/seatbelt/guards/ -run TestDevCredentials_GitCredentials -v
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add pkg/seatbelt/guards/guard_dev_credentials.go pkg/seatbelt/guards/guard_dev_credentials_test.go
git commit -m "fix: deny access to ~/.git-credentials plaintext credential store"
```

---

### Task 6: EnableGuard capability plumbing

**Files:**
- Modify: `internal/capability/capability.go:10-31,97-131,189-226`
- Modify: `internal/config/schema.go:128-134`
- Modify: `internal/sandbox/capabilities.go:18-26`
- Modify: `internal/capability/capability_test.go`
- Modify: `internal/sandbox/capabilities_test.go`

- [ ] **Step 1: Write test for EnableGuard on Capability**

Append to `internal/capability/capability_test.go`:

```go
func TestResolveOne_EnableGuard(t *testing.T) {
	registry := map[string]Capability{
		"git-remote": {
			Name:        "git-remote",
			EnableGuard: []string{"git-remote"},
			EnvAllow:    []string{"SSH_AUTH_SOCK"},
		},
	}

	resolved, err := ResolveOne("git-remote", registry)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resolved.EnableGuard) != 1 || resolved.EnableGuard[0] != "git-remote" {
		t.Errorf("expected EnableGuard [git-remote], got %v", resolved.EnableGuard)
	}
}

func TestResolveOne_EnableGuard_Inherits(t *testing.T) {
	registry := map[string]Capability{
		"base-remote": {
			Name:        "base-remote",
			EnableGuard: []string{"git-remote"},
		},
		"my-remote": {
			Name:        "my-remote",
			Extends:     "base-remote",
			EnvAllow:    []string{"SSH_AUTH_SOCK"},
		},
	}

	resolved, err := ResolveOne("my-remote", registry)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resolved.EnableGuard) != 1 || resolved.EnableGuard[0] != "git-remote" {
		t.Errorf("expected inherited EnableGuard [git-remote], got %v", resolved.EnableGuard)
	}
}

func TestToSandboxOverrides_EnableGuard(t *testing.T) {
	registry := map[string]Capability{
		"git-remote": {
			Name:        "git-remote",
			EnableGuard: []string{"git-remote"},
			EnvAllow:    []string{"SSH_AUTH_SOCK"},
		},
	}

	set, err := ResolveAll([]string{"git-remote"}, registry, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	overrides := set.ToSandboxOverrides()
	if len(overrides.EnableGuard) != 1 || overrides.EnableGuard[0] != "git-remote" {
		t.Errorf("expected EnableGuard [git-remote] in overrides, got %v", overrides.EnableGuard)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/capability/ -run TestResolveOne_EnableGuard -v
go test ./internal/capability/ -run TestToSandboxOverrides_EnableGuard -v
```

Expected: FAIL — `EnableGuard` field doesn't exist.

- [ ] **Step 3: Add EnableGuard to Capability and ResolvedCapability**

In `internal/capability/capability.go`, add `EnableGuard []string` to both
structs:

```go
type Capability struct {
	Name        string
	Description string
	Extends     string
	Combines    []string
	Unguard     []string
	Readable    []string
	Writable    []string
	Deny        []string
	EnvAllow    []string
	EnableGuard []string // guards to activate when capability is enabled
}

type ResolvedCapability struct {
	Name        string
	Sources     []string
	Unguard     []string
	Readable    []string
	Writable    []string
	Deny        []string
	EnvAllow    []string
	EnableGuard []string
}
```

Update `flatten()`:

```go
func flatten(capDef *Capability) *ResolvedCapability {
	return &ResolvedCapability{
		Name:        capDef.Name,
		Sources:     []string{capDef.Name},
		Unguard:     copyStrings(capDef.Unguard),
		Readable:    copyStrings(capDef.Readable),
		Writable:    copyStrings(capDef.Writable),
		Deny:        copyStrings(capDef.Deny),
		EnvAllow:    copyStrings(capDef.EnvAllow),
		EnableGuard: copyStrings(capDef.EnableGuard),
	}
}
```

Update `mergeChild()`:

```go
func mergeChild(parent *ResolvedCapability, child *Capability) *ResolvedCapability {
	return &ResolvedCapability{
		Name:        child.Name,
		Sources:     append([]string{child.Name}, parent.Sources...),
		Unguard:     dedup(append(parent.Unguard, child.Unguard...)),
		Readable:    dedup(append(parent.Readable, child.Readable...)),
		Writable:    dedup(append(parent.Writable, child.Writable...)),
		Deny:        dedup(append(parent.Deny, child.Deny...)),
		EnvAllow:    dedup(append(parent.EnvAllow, child.EnvAllow...)),
		EnableGuard: dedup(append(parent.EnableGuard, child.EnableGuard...)),
	}
}
```

Update `mergeAdditive()`:

```go
func mergeAdditive(a, b *ResolvedCapability) *ResolvedCapability {
	return &ResolvedCapability{
		Name:        a.Name,
		Sources:     append(a.Sources, b.Sources...),
		Unguard:     dedup(append(a.Unguard, b.Unguard...)),
		Readable:    dedup(append(a.Readable, b.Readable...)),
		Writable:    dedup(append(a.Writable, b.Writable...)),
		Deny:        dedup(append(a.Deny, b.Deny...)),
		EnvAllow:    dedup(append(a.EnvAllow, b.EnvAllow...)),
		EnableGuard: dedup(append(a.EnableGuard, b.EnableGuard...)),
	}
}
```

- [ ] **Step 4: Add EnableGuard to SandboxOverrides**

In `internal/config/schema.go`, add to `SandboxOverrides`:

```go
type SandboxOverrides struct {
	Unguard       []string
	ReadableExtra []string
	WritableExtra []string
	DeniedExtra   []string
	EnvAllow      []string
	EnableGuard   []string
}
```

- [ ] **Step 5: Update ToSandboxOverrides**

In `internal/capability/capability.go`, update `ToSandboxOverrides()` to
collect EnableGuard:

Add after line 198 (`o.EnvAllow = append(o.EnvAllow, rc.EnvAllow...)`):

```go
		o.EnableGuard = append(o.EnableGuard, rc.EnableGuard...)
```

Add after line 223 (`o.EnvAllow = dedup(o.EnvAllow)`):

```go
	o.EnableGuard = dedup(o.EnableGuard)
```

- [ ] **Step 6: Update ApplyOverrides to flow into GuardsExtra**

In `internal/sandbox/capabilities.go`, add after line 25:

```go
	(*cfg).GuardsExtra = append((*cfg).GuardsExtra, overrides.EnableGuard...)
```

- [ ] **Step 7: Run capability tests**

```bash
go test ./internal/capability/ -v
```

Expected: all PASS

- [ ] **Step 8: Write test for ApplyOverrides with EnableGuard**

Append to `internal/sandbox/capabilities_test.go`:

```go
func TestApplyOverrides_EnableGuard(t *testing.T) {
	cfg := &config.SandboxPolicy{}
	overrides := config.SandboxOverrides{
		EnableGuard: []string{"git-remote"},
	}
	ApplyOverrides(&cfg, overrides)

	if len(cfg.GuardsExtra) != 1 || cfg.GuardsExtra[0] != "git-remote" {
		t.Errorf("expected GuardsExtra [git-remote], got %v", cfg.GuardsExtra)
	}
}
```

- [ ] **Step 9: Run sandbox capabilities tests**

```bash
go test ./internal/sandbox/ -run TestApplyOverrides -v
```

Expected: all PASS

- [ ] **Step 10: Commit**

```bash
git add internal/capability/capability.go internal/config/schema.go internal/sandbox/capabilities.go internal/capability/capability_test.go internal/sandbox/capabilities_test.go
git commit -m "feat: add EnableGuard field to capability plumbing

Capabilities can now activate opt-in guards via EnableGuard.
The field flows through resolution into SandboxOverrides and
gets appended to GuardsExtra in ApplyOverrides."
```

---

### Task 7: Git remote guard

**Files:**
- Create: `pkg/seatbelt/guards/guard_git_remote.go`
- Create: `pkg/seatbelt/guards/guard_git_remote_test.go`
- Modify: `pkg/seatbelt/guards/registry.go:26-30`

- [ ] **Step 1: Write test for guard metadata**

In `pkg/seatbelt/guards/guard_git_remote_test.go`:

```go
package guards_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/jskswamy/aide/pkg/seatbelt"
	"github.com/jskswamy/aide/pkg/seatbelt/guards"
)

func TestGuard_GitRemote_Metadata(t *testing.T) {
	g := guards.GitRemoteGuard()

	if g.Name() != "git-remote" {
		t.Errorf("expected Name() = %q, got %q", "git-remote", g.Name())
	}
	if g.Type() != "opt-in" {
		t.Errorf("expected Type() = %q, got %q", "opt-in", g.Type())
	}
	if g.Description() == "" {
		t.Error("expected non-empty Description()")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./pkg/seatbelt/guards/ -run TestGuard_GitRemote_Metadata -v
```

Expected: FAIL — `GitRemoteGuard` not defined.

- [ ] **Step 3: Implement the guard**

In `pkg/seatbelt/guards/guard_git_remote.go`:

```go
// Git remote guard for macOS Seatbelt profiles.
//
// Enables git remote operations (push, fetch, pull) by allowing SSH key
// reading, SSH agent socket access, and network outbound on ports 22/443.
// This is an opt-in guard activated via the git-remote capability.

package guards

import (
	"fmt"
	"path/filepath"

	"github.com/jskswamy/aide/pkg/seatbelt"
)

type gitRemoteGuard struct{}

// GitRemoteGuard returns a Guard that enables git remote operations.
func GitRemoteGuard() seatbelt.Guard { return &gitRemoteGuard{} }

func (g *gitRemoteGuard) Name() string { return "git-remote" }
func (g *gitRemoteGuard) Type() string { return "opt-in" }
func (g *gitRemoteGuard) Description() string {
	return "Git remote operations — SSH keys, credentials, and network (ports 22/443)"
}

func (g *gitRemoteGuard) Rules(ctx *seatbelt.Context) seatbelt.GuardResult {
	if ctx == nil {
		return seatbelt.GuardResult{}
	}

	home := ctx.HomeDir
	if home == "" {
		return seatbelt.GuardResult{}
	}

	var result seatbelt.GuardResult

	// SSH key and config access (read-only)
	// HomeSubpath covers config, known_hosts, and key files
	result.Rules = append(result.Rules,
		seatbelt.SectionAllow("SSH keys and config for git transport (read-only)"),
		seatbelt.AllowRule(fmt.Sprintf(`(allow file-read*
    %s
)`, seatbelt.HomeSubpath(home, ".ssh"))),
	)

	// SSH agent socket
	if sock, ok := ctx.EnvLookup("SSH_AUTH_SOCK"); ok && sock != "" {
		result.Rules = append(result.Rules,
			seatbelt.SectionAllow("SSH agent socket"),
			seatbelt.AllowRule(fmt.Sprintf(`(allow network-outbound
    (remote unix-socket (path-literal "%s"))
)`, sock)),
		)
		result.Overrides = append(result.Overrides, seatbelt.Override{
			EnvVar: "SSH_AUTH_SOCK",
			Value:  sock,
		})
	} else {
		result.Skipped = append(result.Skipped,
			"SSH_AUTH_SOCK not set — SSH agent socket rule skipped")
	}

	// Git credential manager config (read-only, if present)
	gcmDir := filepath.Join(home, ".config", "git-credential-manager")
	if dirExists(gcmDir) {
		result.Rules = append(result.Rules,
			seatbelt.SectionAllow("Git Credential Manager config (read-only)"),
			seatbelt.AllowRule(fmt.Sprintf(`(allow file-read* %s)`,
				seatbelt.HomeSubpath(home, ".config/git-credential-manager"))),
		)
	}

	// Network outbound on git transport ports
	result.Rules = append(result.Rules,
		seatbelt.SectionAllow("Network outbound for git transport (ports 22, 443)"),
		seatbelt.AllowRule(`(allow network-outbound
    (remote tcp "*:22")
    (remote tcp "*:443")
)`),
	)

	// Defense-in-depth: explicitly deny ~/.git-credentials even if
	// dev-credentials guard is unguarded by another capability.
	gitCredentials := filepath.Join(home, ".git-credentials")
	result.Rules = append(result.Rules,
		seatbelt.SectionDeny("Plaintext git credentials (defense-in-depth)"),
		seatbelt.DenyRule(fmt.Sprintf(`(deny file-read-data (literal "%s"))`, gitCredentials)),
		seatbelt.DenyRule(fmt.Sprintf(`(deny file-write* (literal "%s"))`, gitCredentials)),
	)
	result.Protected = append(result.Protected, gitCredentials)

	return result
}
```

- [ ] **Step 4: Run test to verify metadata passes**

```bash
go test ./pkg/seatbelt/guards/ -run TestGuard_GitRemote_Metadata -v
```

Expected: PASS

- [ ] **Step 5: Write test for guard rules**

Append to `pkg/seatbelt/guards/guard_git_remote_test.go`:

```go
func TestGuard_GitRemote_Rules(t *testing.T) {
	g := guards.GitRemoteGuard()
	home := "/Users/testuser"
	ctx := &seatbelt.Context{
		HomeDir: home,
		Env:     []string{"SSH_AUTH_SOCK=/tmp/ssh-agent.sock"},
	}
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	// SSH paths (subpath covers all files under .ssh)
	if !strings.Contains(output, `"/Users/testuser/.ssh"`) {
		t.Error("expected SSH directory subpath")
	}

	// SSH agent socket
	if !strings.Contains(output, `/tmp/ssh-agent.sock`) {
		t.Error("expected SSH agent socket path")
	}
	if !strings.Contains(output, "network-outbound") {
		t.Error("expected network-outbound rule")
	}

	// Network ports
	if !strings.Contains(output, `"*:22"`) {
		t.Error("expected port 22 network rule")
	}
	if !strings.Contains(output, `"*:443"`) {
		t.Error("expected port 443 network rule")
	}

	// git-credentials deny (defense-in-depth)
	gitCreds := filepath.Join(home, ".git-credentials")
	if !strings.Contains(output, gitCreds) {
		t.Error("expected .git-credentials deny rule")
	}
	if !strings.Contains(output, "deny file-read-data") {
		t.Error("expected deny file-read-data for git-credentials")
	}
}

func TestGuard_GitRemote_NoSSHAgent(t *testing.T) {
	g := guards.GitRemoteGuard()
	ctx := &seatbelt.Context{
		HomeDir: "/Users/testuser",
		// No SSH_AUTH_SOCK in Env
	}
	result := g.Rules(ctx)

	if len(result.Skipped) == 0 {
		t.Error("expected skipped message for missing SSH_AUTH_SOCK")
	}

	output := renderTestRules(result.Rules)
	if strings.Contains(output, "unix-socket") {
		t.Error("should not have SSH agent socket rule when SSH_AUTH_SOCK is unset")
	}
}

func TestGuard_GitRemote_NilContext(t *testing.T) {
	g := guards.GitRemoteGuard()
	result := g.Rules(nil)
	if len(result.Rules) != 0 {
		t.Error("expected no rules for nil context")
	}
}

func TestGuard_GitRemote_ReadOnly(t *testing.T) {
	g := guards.GitRemoteGuard()
	ctx := &seatbelt.Context{HomeDir: "/Users/testuser"}
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	// SSH paths should be read-only (file-read*, not file-write*)
	// The only file-write* should be in the git-credentials deny rule
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if strings.Contains(line, "file-write*") && !strings.Contains(line, "deny") {
			t.Errorf("SSH paths should be read-only, found write rule: %s", line)
		}
	}
}
```

- [ ] **Step 6: Run tests**

```bash
go test ./pkg/seatbelt/guards/ -run TestGuard_GitRemote -v
```

Expected: all PASS

- [ ] **Step 7: Register the guard in registry.go**

In `pkg/seatbelt/guards/registry.go`, add `GitRemoteGuard()` as an opt-in
guard. Add a new section after the default guards block:

```go
func init() {
	// always guards first
	builtinGuards = append(builtinGuards,
		BaseGuard(),
		SystemRuntimeGuard(),
		NetworkGuard(),
		FilesystemGuard(),
		GitIntegrationGuard(), // git config parsing (read-only)
		KeychainGuard(),
		NodeToolchainGuard(),
		NixToolchainGuard(),
	)
	// default guards
	builtinGuards = append(builtinGuards,
		ProjectSecretsGuard(),
		DevCredentialsGuard(),
		AideSecretsGuard(),
	)
	// opt-in guards (activated via capability EnableGuard)
	builtinGuards = append(builtinGuards,
		GitRemoteGuard(),
	)
}
```

- [ ] **Step 8: Update registry test counts for git-remote**

In `pkg/seatbelt/guards/registry_test.go`:

- Line 11: change `11` to `12` (total guards: was 11 after Task 3, now 12)
- Line 63: change `0` to `1` (opt-in guards: was 0, now 1 with git-remote)

- [ ] **Step 9: Run full guard test suite**

```bash
go test ./pkg/seatbelt/guards/ -v
```

Expected: all PASS

- [ ] **Step 10: Commit**

```bash
git add pkg/seatbelt/guards/guard_git_remote.go pkg/seatbelt/guards/guard_git_remote_test.go pkg/seatbelt/guards/registry.go pkg/seatbelt/guards/registry_test.go
git commit -m "feat: add git-remote opt-in guard for remote operations

Enables SSH key reading, SSH agent socket, and network outbound
on ports 22/443. Activated via git-remote capability. Includes
defense-in-depth deny for ~/.git-credentials."
```

---

### Task 8: Add `git-remote` capability and detection

**Files:**
- Modify: `internal/capability/builtin.go:128-141`
- Modify: `internal/capability/detect.go:97-109`
- Modify: `internal/capability/detect_test.go`
- Modify: `internal/capability/builtin_test.go`

- [ ] **Step 1: Write detection test**

Append to `internal/capability/detect_test.go`:

```go
func TestDetectProject_GitRemote(t *testing.T) {
	tmp := t.TempDir()

	// Create .git/config with a remote
	gitDir := filepath.Join(tmp, ".git")
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	gitConfig := `[remote "origin"]
	url = git@github.com:user/repo.git
	fetch = +refs/heads/*:refs/remotes/origin/*
`
	if err := os.WriteFile(filepath.Join(gitDir, "config"), []byte(gitConfig), 0o644); err != nil {
		t.Fatal(err)
	}

	suggestions := DetectProject(tmp)
	found := false
	for _, s := range suggestions {
		if s == "git-remote" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected git-remote in suggestions, got %v", suggestions)
	}
}

func TestDetectProject_GitRemote_NoRemotes(t *testing.T) {
	tmp := t.TempDir()

	// Create .git/config without remotes
	gitDir := filepath.Join(tmp, ".git")
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "config"), []byte(`[core]
	bare = false
`), 0o644); err != nil {
		t.Fatal(err)
	}

	suggestions := DetectProject(tmp)
	for _, s := range suggestions {
		if s == "git-remote" {
			t.Error("should NOT suggest git-remote when no remotes configured")
		}
	}
}

func TestDetectProject_GitRemote_NoGitDir(t *testing.T) {
	tmp := t.TempDir()

	suggestions := DetectProject(tmp)
	for _, s := range suggestions {
		if s == "git-remote" {
			t.Error("should NOT suggest git-remote when no .git directory")
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/capability/ -run TestDetectProject_GitRemote -v
```

Expected: FAIL — no git-remote detection logic.

- [ ] **Step 3: Add git-remote detection to DetectProject**

In `internal/capability/detect.go`, add before the `return suggestions` line
(line 108):

```go
	// Git remote operations
	gitConfigPath := filepath.Join(projectRoot, ".git", "config")
	if containsInFileByPath(gitConfigPath, "[remote ") {
		suggestions = append(suggestions, "git-remote")
	}
```

- [ ] **Step 4: Run detection tests**

```bash
go test ./internal/capability/ -run TestDetectProject_GitRemote -v
```

Expected: all PASS

- [ ] **Step 5: Add git-remote capability to builtins**

In `internal/capability/builtin.go`, add after the `"github"` entry
(line 134):

```go
		"git-remote": {
			Name:        "git-remote",
			Description: "Git remote operations (push, fetch, pull) via SSH and HTTPS",
			EnableGuard: []string{"git-remote"},
			EnvAllow:    []string{"SSH_AUTH_SOCK"},
		},
```

- [ ] **Step 6: Run builtin tests**

```bash
go test ./internal/capability/ -v
```

Expected: all PASS

- [ ] **Step 7: Commit**

```bash
git add internal/capability/builtin.go internal/capability/detect.go internal/capability/detect_test.go
git commit -m "feat: add git-remote capability with auto-detection

Auto-detects when .git/config has remotes and suggests enabling
the git-remote capability. Uses EnableGuard to activate the
git-remote opt-in guard."
```

---

### Task 9: End-to-end integration test

**Files:**
- Modify: `internal/sandbox/capabilities_test.go`

- [ ] **Step 1: Write integration test**

Append to `internal/sandbox/capabilities_test.go`:

```go
func TestResolveCapabilities_GitRemote_EnableGuard(t *testing.T) {
	cfg := &config.Config{}
	capSet, overrides, err := ResolveCapabilities([]string{"git-remote"}, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capSet == nil {
		t.Fatal("expected non-nil capSet")
	}

	// EnableGuard should flow through to overrides
	if len(overrides.EnableGuard) != 1 || overrides.EnableGuard[0] != "git-remote" {
		t.Errorf("expected EnableGuard [git-remote], got %v", overrides.EnableGuard)
	}

	// EnvAllow should include SSH_AUTH_SOCK
	found := false
	for _, e := range overrides.EnvAllow {
		if e == "SSH_AUTH_SOCK" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected SSH_AUTH_SOCK in EnvAllow")
	}

	// ApplyOverrides should add to GuardsExtra
	var sandboxCfg *config.SandboxPolicy
	ApplyOverrides(&sandboxCfg, overrides)

	if len(sandboxCfg.GuardsExtra) != 1 || sandboxCfg.GuardsExtra[0] != "git-remote" {
		t.Errorf("expected GuardsExtra [git-remote], got %v", sandboxCfg.GuardsExtra)
	}
}
```

- [ ] **Step 2: Run test**

```bash
go test ./internal/sandbox/ -run TestResolveCapabilities_GitRemote -v
```

Expected: PASS

- [ ] **Step 3: Run full test suite**

```bash
go test ./... 2>&1 | tail -30
```

Expected: all packages PASS. If any fail, investigate and fix.

- [ ] **Step 4: Commit**

```bash
git add internal/sandbox/capabilities_test.go
git commit -m "test: add end-to-end integration test for git-remote capability

Verifies EnableGuard flows through capability resolution,
SandboxOverrides, and ApplyOverrides into GuardsExtra."
```

---

### Task 10: Run full test suite and lint

**Files:** None (verification only)

- [ ] **Step 1: Run all tests**

```bash
go test ./...
```

Expected: all PASS

- [ ] **Step 2: Run linter**

```bash
golangci-lint run ./... 2>&1 | head -50
```

Expected: no new lint errors. If there are issues, fix them.

- [ ] **Step 3: Run go vet**

```bash
go vet ./...
```

Expected: no issues.

- [ ] **Step 4: Verify sandbox profile output**

If available, run the sandbox show command to verify the git-integration guard
appears in the generated profile:

```bash
go run ./cmd/aide sandbox show 2>&1 | grep -A5 "git-integration"
```

Expected: shows git configuration read-only rules.
