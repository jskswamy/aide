# Split-Read Sandbox Model Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the whitelisted-read sandbox model with a split-read model: broad system reads outside `$HOME`, scoped development reads inside `$HOME`, discovery-based deny guards for secrets.

**Architecture:** System-runtime guard gets broad reads for system paths. Filesystem guard replaces the `$HOME` full-read with a curated list of development directories. New deny guards protect `.env` files, shell history, mounted volumes, and dev credentials. Five guards promoted from opt-in to default.

**Tech Stack:** Go, macOS Seatbelt (`sandbox-exec`), `pkg/seatbelt`, `internal/sandbox`

**Spec:** `docs/superpowers/specs/2026-03-24-broad-read-allow-design.md`

---

### Task 1: Rewrite System-Runtime Read Rules

Replace the whitelisted system path reads with broad top-level directory allows.

**Files:**
- Modify: `pkg/seatbelt/guards/guard_system_runtime.go:25-107`
- Modify: `pkg/seatbelt/guards/system_test.go`

- [ ] **Step 1: Write the failing test**

In the system-runtime test file, add a test that checks for broad system reads:

```go
func TestSystemRuntime_BroadSystemReads(t *testing.T) {
	g := guards.SystemRuntimeGuard()
	ctx := &seatbelt.Context{HomeDir: "/Users/testuser", AllowSubprocess: true}
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	// Should have broad system directory allows
	broadPaths := []string{
		`(subpath "/usr")`, `(subpath "/bin")`, `(subpath "/sbin")`,
		`(subpath "/opt")`, `(subpath "/System")`, `(subpath "/Library")`,
		`(subpath "/nix")`, `(subpath "/private")`, `(subpath "/Applications")`,
		`(subpath "/run")`, `(subpath "/dev")`, `(subpath "/tmp")`, `(subpath "/var")`,
	}
	for _, p := range broadPaths {
		if !strings.Contains(output, p) {
			t.Errorf("expected broad system read for %s", p)
		}
	}

	// Should NOT have the old granular paths
	oldPaths := []string{
		`(subpath "/System/Library")`,     // was specific, now just /System
		`(subpath "/Library/Apple")`,       // was specific, now just /Library
		`(subpath "/Library/Frameworks")`,  // was specific, now just /Library
	}
	for _, p := range oldPaths {
		if strings.Contains(output, p) {
			t.Errorf("should not have old granular path %s (replaced by broad /System, /Library)", p)
		}
	}

	// Non-read rules should still be present
	if !strings.Contains(output, "(allow process-exec)") {
		t.Error("expected process-exec rule")
	}
	if !strings.Contains(output, "(allow mach-lookup") {
		t.Error("expected mach-lookup rules")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/seatbelt/guards/ -run TestSystemRuntime_BroadSystemReads -v`
Expected: FAIL — old granular paths still present

- [ ] **Step 3: Rewrite the system-runtime read rules**

In `pkg/seatbelt/guards/guard_system_runtime.go`, replace sections 1-5 (lines 25-107) of the rules slice with:

```go
	rules := []seatbelt.Rule{
		// 1. Broad system reads — all top-level system directories
		seatbelt.SectionAllow("Broad system reads"),
		seatbelt.AllowRule(`(allow file-read*
    (subpath "/usr")
    (subpath "/bin")
    (subpath "/sbin")
    (subpath "/opt")
    (subpath "/System")
    (subpath "/Library")
    (subpath "/nix")
    (subpath "/private")
    (subpath "/Applications")
    (subpath "/run")
    (subpath "/dev")
    (subpath "/tmp")
    (subpath "/var")
)`),

		// 2. Root-level traversal
		seatbelt.SectionAllow("Root-level traversal"),
		seatbelt.AllowRule(`(allow file-read-metadata
    (literal "/")
    (literal "/Users")
)`),
		seatbelt.AllowRule(`(allow file-read-data
    (literal "/")
)`),
```

Keep everything from section 6 onward (process rules, temp dirs, device nodes, etc.) unchanged. The `home`-based rules in sections 4-5 (Home metadata traversal, User preferences) are removed — the filesystem guard handles `$HOME` reads now.

**Important:** Remove the `home := ctx.HomeDir` line at the top of the method if no remaining rules use it. Check if any non-read rules reference `home` (mach services don't, but user preferences did — those are removed).

- [ ] **Step 4: Run tests**

Run: `go test ./pkg/seatbelt/guards/ -run TestSystemRuntime -v`
Expected: All pass. Some existing tests may need updating if they checked for old paths.

- [ ] **Step 5: Run full guard suite**

Run: `go test ./pkg/seatbelt/guards/ -v`
Expected: All pass

- [ ] **Step 6: Commit**

Stage: `git add pkg/seatbelt/guards/guard_system_runtime.go pkg/seatbelt/guards/system_test.go`
Run: `/commit --style classic rewrite system-runtime guard with broad system directory reads`

---

### Task 2: Rewrite Filesystem Guard for Scoped `$HOME` Reads

Replace the full `$HOME` read-only with specific development paths.

**Files:**
- Modify: `pkg/seatbelt/guards/guard_filesystem.go`
- Modify: `pkg/seatbelt/guards/filesystem_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestFilesystemGuard_ScopedHomeReads(t *testing.T) {
	g := guards.FilesystemGuard()
	ctx := &seatbelt.Context{
		HomeDir:     "/Users/testuser",
		ProjectRoot: "/project",
	}
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	// Should NOT have broad $HOME read
	if strings.Contains(output, `(subpath "/Users/testuser")`) &&
		!strings.Contains(output, `(subpath "/Users/testuser/`)`) {
		t.Error("should NOT have broad $HOME subpath read")
	}

	// Should have specific dev paths
	devPaths := []string{
		`"/Users/testuser/.config"`,
		`"/Users/testuser/.cache"`,
		`"/Users/testuser/.local"`,
		`"/Users/testuser/.ssh"`,
		`"/Users/testuser/.cargo"`,
		`"/Users/testuser/.rustup"`,
		`"/Users/testuser/go"`,
		`"/Users/testuser/Library/Keychains"`,
		`"/Users/testuser/Library/Caches"`,
		`"/Users/testuser/Library/Preferences"`,
	}
	for _, p := range devPaths {
		if !strings.Contains(output, p) {
			t.Errorf("expected dev path %s in output", p)
		}
	}

	// Should have home dotfile regex
	if !strings.Contains(output, "regex") {
		t.Error("expected regex rule for home dotfiles")
	}

	// Project root should still be writable
	if !strings.Contains(output, `"/project"`) {
		t.Error("expected project root in writable paths")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/seatbelt/guards/ -run TestFilesystemGuard_ScopedHome -v`

- [ ] **Step 3: Rewrite the filesystem guard**

Replace the `Rules` method in `guard_filesystem.go`:

```go
func (g *filesystemGuard) Rules(ctx *seatbelt.Context) seatbelt.GuardResult {
	if ctx == nil {
		return seatbelt.GuardResult{}
	}

	home := ctx.HomeDir
	var writable []string

	if ctx.ProjectRoot != "" {
		writable = append(writable, ctx.ProjectRoot)
	}
	if ctx.RuntimeDir != "" {
		writable = append(writable, ctx.RuntimeDir)
	}
	if ctx.TempDir != "" {
		writable = append(writable, ctx.TempDir)
	}
	writable = append(writable, ctx.ExtraWritable...)

	var rules []seatbelt.Rule

	// Writable paths
	if len(writable) > 0 {
		rules = append(rules, seatbelt.AllowRule(
			fmt.Sprintf("(allow file-read* file-write*\n    %s)", buildRequireAny(writable))))
	}

	// Scoped $HOME reads — development paths only
	if home != "" {
		rules = append(rules,
			seatbelt.SectionAllow("Home development paths (read-only)"),
			seatbelt.AllowRule(`(allow file-read*
    `+seatbelt.HomeSubpath(home, ".config")+`
    `+seatbelt.HomeSubpath(home, ".cache")+`
    `+seatbelt.HomeSubpath(home, ".local")+`
    `+seatbelt.HomeSubpath(home, ".nix-profile")+`
    `+seatbelt.HomeSubpath(home, ".nix-defexpr")+`
    `+seatbelt.HomeSubpath(home, ".ssh")+`
    `+seatbelt.HomeSubpath(home, ".cargo")+`
    `+seatbelt.HomeSubpath(home, ".rustup")+`
    `+seatbelt.HomeSubpath(home, "go")+`
    `+seatbelt.HomeSubpath(home, ".pyenv")+`
    `+seatbelt.HomeSubpath(home, ".rbenv")+`
    `+seatbelt.HomeSubpath(home, ".sdkman")+`
    `+seatbelt.HomeSubpath(home, ".gradle")+`
    `+seatbelt.HomeSubpath(home, ".m2")+`
    `+seatbelt.HomeSubpath(home, "Library/Keychains")+`
    `+seatbelt.HomeSubpath(home, "Library/Caches")+`
    `+seatbelt.HomeSubpath(home, "Library/Preferences")+`
)`),

			// Dotfiles directly in $HOME (e.g., .gitconfig, .npmrc)
			seatbelt.SectionAllow("Home dotfiles"),
			seatbelt.AllowRule(fmt.Sprintf(`(allow file-read*
    (regex #"^%s/\.[^/]+$")
)`, home)),

			// Home and Library metadata for traversal
			seatbelt.SectionAllow("Home metadata traversal"),
			seatbelt.AllowRule(`(allow file-read-metadata
    `+seatbelt.HomeLiteral(home, "")+`
    `+seatbelt.HomeLiteral(home, "Library")+`
)`),
		)

		// ExtraReadable — adds allow rules AND serves as deny opt-out
		if len(ctx.ExtraReadable) > 0 {
			for _, p := range ctx.ExtraReadable {
				rules = append(rules,
					seatbelt.AllowRule(fmt.Sprintf(`(allow file-read* %s)`, seatbelt.Path(p))))
			}
		}
	}

	// Denied paths
	if len(ctx.ExtraDenied) > 0 {
		expanded := seatbelt.ExpandGlobs(ctx.ExtraDenied)
		for _, p := range expanded {
			expr := seatbelt.Path(p)
			rules = append(rules,
				seatbelt.DenyRule(fmt.Sprintf("(deny file-read-data %s)", expr)),
				seatbelt.DenyRule(fmt.Sprintf("(deny file-write* %s)", expr)),
			)
		}
	}

	return seatbelt.GuardResult{Rules: rules}
}
```

- [ ] **Step 4: Update existing filesystem tests**

Update `TestFilesystemGuard_ExtraWritable` — it should still pass (writable handling unchanged). Remove or update `TestFilesystemGuard_ExtraReadable` since ExtraReadable now produces individual allow rules, not a batch readable entry.

- [ ] **Step 5: Run tests**

Run: `go test ./pkg/seatbelt/guards/ -run TestFilesystemGuard -v`

- [ ] **Step 6: Commit**

Stage: `git add pkg/seatbelt/guards/guard_filesystem.go pkg/seatbelt/guards/filesystem_test.go`
Run: `/commit --style classic rewrite filesystem guard with scoped home reads for development paths only`

---

### Task 3: New Guard — `mounted-volumes`

**Files:**
- Create: `pkg/seatbelt/guards/guard_mounted_volumes.go`
- Modify: `pkg/seatbelt/guards/registry.go`
- Create: test in appropriate test file

- [ ] **Step 1: Write the failing test**

```go
func TestMountedVolumes_DeniesVolumes(t *testing.T) {
	g := guards.MountedVolumesGuard()

	if g.Name() != "mounted-volumes" {
		t.Errorf("expected name mounted-volumes, got %s", g.Name())
	}
	if g.Type() != "default" {
		t.Errorf("expected type default, got %s", g.Type())
	}

	ctx := &seatbelt.Context{HomeDir: "/Users/testuser"}
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	if !strings.Contains(output, `deny file-read-data`) {
		t.Error("expected deny rule for /Volumes")
	}
	if !strings.Contains(output, `"/Volumes"`) {
		t.Error("expected /Volumes path in deny rule")
	}
}
```

- [ ] **Step 2: Implement the guard**

Create `pkg/seatbelt/guards/guard_mounted_volumes.go`:

```go
package guards

import "github.com/jskswamy/aide/pkg/seatbelt"

type mountedVolumesGuard struct{}

func MountedVolumesGuard() seatbelt.Guard { return &mountedVolumesGuard{} }

func (g *mountedVolumesGuard) Name() string        { return "mounted-volumes" }
func (g *mountedVolumesGuard) Type() string        { return "default" }
func (g *mountedVolumesGuard) Description() string {
	return "Blocks access to mounted volumes, Time Machine, and external storage"
}

func (g *mountedVolumesGuard) Rules(ctx *seatbelt.Context) seatbelt.GuardResult {
	if !dirExists("/Volumes") {
		return seatbelt.GuardResult{
			Skipped: []string{"/Volumes not found"},
		}
	}
	return seatbelt.GuardResult{
		Rules:     DenyDir("/Volumes"),
		Protected: []string{"/Volumes"},
	}
}
```

- [ ] **Step 3: Register in registry.go**

Add `MountedVolumesGuard()` to the default guards section in `registry.go` (after line 39).

- [ ] **Step 4: Run tests**

Run: `go test ./pkg/seatbelt/guards/ -run TestMountedVolumes -v`

- [ ] **Step 5: Commit**

Stage: `git add pkg/seatbelt/guards/guard_mounted_volumes.go pkg/seatbelt/guards/registry.go pkg/seatbelt/guards/*_test.go`
Run: `/commit --style classic add mounted-volumes guard to deny access to external storage and Time Machine`

---

### Task 4: New Guard — `shell-history`

**Files:**
- Create: `pkg/seatbelt/guards/guard_shell_history.go`
- Modify: `pkg/seatbelt/guards/registry.go`

- [ ] **Step 1: Write the failing test**

```go
func TestShellHistory_DeniesHistoryFiles(t *testing.T) {
	g := guards.ShellHistoryGuard()

	if g.Type() != "default" {
		t.Errorf("expected type default, got %s", g.Type())
	}

	ctx := &seatbelt.Context{HomeDir: "/Users/testuser"}
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	// Should deny known history files that exist
	// Since we can't guarantee which exist in test, just check structure
	if len(result.Rules) == 0 && len(result.Skipped) == 0 {
		t.Error("expected either rules or skipped entries")
	}
}
```

- [ ] **Step 2: Implement the guard**

Create `pkg/seatbelt/guards/guard_shell_history.go`:

```go
package guards

import (
	"fmt"
	"path/filepath"

	"github.com/jskswamy/aide/pkg/seatbelt"
)

type shellHistoryGuard struct{}

func ShellHistoryGuard() seatbelt.Guard { return &shellHistoryGuard{} }

func (g *shellHistoryGuard) Name() string        { return "shell-history" }
func (g *shellHistoryGuard) Type() string        { return "default" }
func (g *shellHistoryGuard) Description() string {
	return "Blocks access to shell and REPL history files containing inline secrets"
}

var historyFiles = []string{
	".bash_history",
	".zsh_history",
	".local/share/fish/fish_history",
	".python_history",
	".node_repl_history",
	".irb_history",
	".psql_history",
	".mysql_history",
	".sqlite_history",
	".lesshst",
}

func (g *shellHistoryGuard) Rules(ctx *seatbelt.Context) seatbelt.GuardResult {
	result := seatbelt.GuardResult{}

	// Build opt-out set from ExtraReadable
	optOut := make(map[string]bool)
	for _, p := range ctx.ExtraReadable {
		optOut[p] = true
	}

	for _, rel := range historyFiles {
		fullPath := filepath.Join(ctx.HomeDir, rel)
		if optOut[fullPath] {
			result.Allowed = append(result.Allowed, fullPath)
			continue
		}
		if pathExists(fullPath) {
			result.Rules = append(result.Rules, DenyFile(fullPath)...)
			result.Protected = append(result.Protected, fullPath)
		} else {
			result.Skipped = append(result.Skipped,
				fmt.Sprintf("%s not found", fullPath))
		}
	}

	return result
}
```

- [ ] **Step 3: Register in registry.go**

Add `ShellHistoryGuard()` to the default guards section.

- [ ] **Step 4: Run tests and commit**

Run: `go test ./pkg/seatbelt/guards/ -run TestShellHistory -v`

Stage: `git add pkg/seatbelt/guards/guard_shell_history.go pkg/seatbelt/guards/registry.go pkg/seatbelt/guards/*_test.go`
Run: `/commit --style classic add shell-history guard to deny access to shell and REPL history files`

---

### Task 5: New Guard — `dev-credentials`

**Files:**
- Create: `pkg/seatbelt/guards/guard_dev_credentials.go`
- Modify: `pkg/seatbelt/guards/registry.go`

- [ ] **Step 1: Write the failing test**

```go
func TestDevCredentials_DeniesKnownCredFiles(t *testing.T) {
	g := guards.DevCredentialsGuard()

	if g.Type() != "default" {
		t.Errorf("expected type default, got %s", g.Type())
	}

	ctx := &seatbelt.Context{HomeDir: "/Users/testuser"}
	result := g.Rules(ctx)

	// Should have some combination of rules and skipped
	if len(result.Rules) == 0 && len(result.Skipped) == 0 {
		t.Error("expected either rules or skipped entries")
	}

	// Check that known cred paths are attempted
	output := renderTestRules(result.Rules)
	skipped := fmt.Sprintf("%v", result.Skipped)
	combined := output + skipped

	credPaths := []string{
		".config/gh",
		".cargo/credentials",
		".gradle/gradle.properties",
		".m2/settings.xml",
	}
	for _, p := range credPaths {
		if !strings.Contains(combined, p) {
			t.Errorf("expected %s to be protected or skipped", p)
		}
	}
}
```

- [ ] **Step 2: Implement the guard**

Create `pkg/seatbelt/guards/guard_dev_credentials.go`:

```go
package guards

import (
	"fmt"
	"path/filepath"

	"github.com/jskswamy/aide/pkg/seatbelt"
)

type devCredentialsGuard struct{}

func DevCredentialsGuard() seatbelt.Guard { return &devCredentialsGuard{} }

func (g *devCredentialsGuard) Name() string        { return "dev-credentials" }
func (g *devCredentialsGuard) Type() string        { return "default" }
func (g *devCredentialsGuard) Description() string {
	return "Blocks access to development tool credential files within allowed directories"
}

// credentialPaths lists relative paths under $HOME that contain auth tokens.
// These live inside allowed directories (~/.config/, ~/.cargo/, etc.) so they
// need explicit deny rules.
var credentialPaths = []struct {
	rel   string
	isDir bool
}{
	{".config/gh", true},             // GitHub CLI OAuth tokens
	{".cargo/credentials.toml", false}, // crates.io publish token
	{".gradle/gradle.properties", false}, // Maven/Artifactory tokens
	{".m2/settings.xml", false},       // Maven repository credentials
	{".config/hub", false},            // Hub CLI GitHub token
	{".config/glab-cli", true},        // GitLab CLI token
	{".pypirc", false},                // PyPI upload credentials
	{".gem/credentials", false},       // RubyGems push token
}

func (g *devCredentialsGuard) Rules(ctx *seatbelt.Context) seatbelt.GuardResult {
	result := seatbelt.GuardResult{}

	// Build opt-out set from ExtraReadable
	optOut := make(map[string]bool)
	for _, p := range ctx.ExtraReadable {
		optOut[p] = true
	}

	for _, cred := range credentialPaths {
		fullPath := filepath.Join(ctx.HomeDir, cred.rel)

		if optOut[fullPath] {
			result.Allowed = append(result.Allowed, fullPath)
			continue
		}

		if cred.isDir {
			if !dirExists(fullPath) {
				result.Skipped = append(result.Skipped,
					fmt.Sprintf("%s not found", fullPath))
				continue
			}
			result.Rules = append(result.Rules, DenyDir(fullPath)...)
		} else {
			if !pathExists(fullPath) {
				result.Skipped = append(result.Skipped,
					fmt.Sprintf("%s not found", fullPath))
				continue
			}
			result.Rules = append(result.Rules, DenyFile(fullPath)...)
		}
		result.Protected = append(result.Protected, fullPath)
	}

	return result
}
```

- [ ] **Step 3: Register in registry.go**

Add `DevCredentialsGuard()` to the default guards section.

- [ ] **Step 4: Run tests and commit**

Run: `go test ./pkg/seatbelt/guards/ -run TestDevCredentials -v`

Stage: `git add pkg/seatbelt/guards/guard_dev_credentials.go pkg/seatbelt/guards/registry.go pkg/seatbelt/guards/*_test.go`
Run: `/commit --style classic add dev-credentials guard to deny auth tokens in allowed config directories`

---

### Task 6: New Guard — `project-secrets`

**Files:**
- Create: `pkg/seatbelt/guards/guard_project_secrets.go`
- Modify: `pkg/seatbelt/guards/registry.go`

- [ ] **Step 1: Write the failing test**

```go
func TestProjectSecrets_DeniesEnvFiles(t *testing.T) {
	g := guards.ProjectSecretsGuard()

	if g.Type() != "default" {
		t.Errorf("expected type default, got %s", g.Type())
	}

	// Create a temp project with .env files
	projectDir := t.TempDir()
	os.WriteFile(filepath.Join(projectDir, ".env"), []byte("SECRET=foo"), 0644)
	os.WriteFile(filepath.Join(projectDir, ".env.local"), []byte("DB=bar"), 0644)
	os.WriteFile(filepath.Join(projectDir, ".envrc"), []byte("export X=1"), 0644)
	os.WriteFile(filepath.Join(projectDir, "main.go"), []byte("package main"), 0644)

	ctx := &seatbelt.Context{
		HomeDir:     "/Users/testuser",
		ProjectRoot: projectDir,
	}
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	// Should deny .env, .env.local, .envrc but NOT main.go
	if !strings.Contains(output, ".env\"") {
		t.Error("expected .env to be denied")
	}
	if !strings.Contains(output, ".env.local") {
		t.Error("expected .env.local to be denied")
	}
	if !strings.Contains(output, ".envrc") {
		t.Error("expected .envrc to be denied")
	}
	if strings.Contains(output, "main.go") {
		t.Error("should NOT deny main.go")
	}
}

func TestProjectSecrets_DeniesGitHooksWrites(t *testing.T) {
	g := guards.ProjectSecretsGuard()

	projectDir := t.TempDir()
	gitHooksDir := filepath.Join(projectDir, ".git", "hooks")
	os.MkdirAll(gitHooksDir, 0755)

	ctx := &seatbelt.Context{
		HomeDir:     "/Users/testuser",
		ProjectRoot: projectDir,
	}
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	if !strings.Contains(output, "hooks") {
		t.Error("expected .git/hooks write deny")
	}
	if !strings.Contains(output, "deny file-write*") {
		t.Error("expected deny file-write* for hooks")
	}
}

func TestProjectSecrets_SkipsNodeModules(t *testing.T) {
	g := guards.ProjectSecretsGuard()

	projectDir := t.TempDir()
	nmDir := filepath.Join(projectDir, "node_modules", "pkg")
	os.MkdirAll(nmDir, 0755)
	os.WriteFile(filepath.Join(nmDir, ".env"), []byte("LEAKED=1"), 0644)

	ctx := &seatbelt.Context{
		HomeDir:     "/Users/testuser",
		ProjectRoot: projectDir,
	}
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	if strings.Contains(output, "node_modules") {
		t.Error("should NOT scan inside node_modules")
	}
}

func TestProjectSecrets_RespectsWritableExtra(t *testing.T) {
	g := guards.ProjectSecretsGuard()

	projectDir := t.TempDir()
	envFile := filepath.Join(projectDir, ".env")
	os.WriteFile(envFile, []byte("SECRET=foo"), 0644)

	ctx := &seatbelt.Context{
		HomeDir:       "/Users/testuser",
		ProjectRoot:   projectDir,
		ExtraWritable: []string{envFile},
	}
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	if strings.Contains(output, ".env") {
		t.Error("should NOT deny .env when in ExtraWritable")
	}
}
```

- [ ] **Step 3: Implement the guard**

Create `pkg/seatbelt/guards/guard_project_secrets.go`:

```go
package guards

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jskswamy/aide/pkg/seatbelt"
)

// Directories to always skip during project scanning.
var skipDirs = map[string]bool{
	".git": true, "node_modules": true, "vendor": true,
	"__pycache__": true, ".venv": true, "venv": true,
	".tox": true, ".eggs": true, "dist": true, "build": true,
}

type projectSecretsGuard struct{}

func ProjectSecretsGuard() seatbelt.Guard { return &projectSecretsGuard{} }

func (g *projectSecretsGuard) Name() string        { return "project-secrets" }
func (g *projectSecretsGuard) Type() string        { return "default" }
func (g *projectSecretsGuard) Description() string {
	return "Blocks access to .env files and denies writes to .git/hooks"
}

func (g *projectSecretsGuard) Rules(ctx *seatbelt.Context) seatbelt.GuardResult {
	result := seatbelt.GuardResult{}
	if ctx.ProjectRoot == "" {
		return result
	}

	// Build opt-out set from ExtraReadable + ExtraWritable
	optOut := make(map[string]bool)
	for _, p := range ctx.ExtraReadable {
		optOut[p] = true
	}
	for _, p := range ctx.ExtraWritable {
		optOut[p] = true
	}

	// Scan for .env* and .envrc files
	envFiles := scanEnvFiles(ctx.ProjectRoot)
	for _, f := range envFiles {
		if optOut[f] {
			result.Allowed = append(result.Allowed, f)
			continue
		}
		result.Rules = append(result.Rules, DenyFile(f)...)
		result.Protected = append(result.Protected, f)
	}

	// Deny writes to .git/hooks/
	hooksDir := filepath.Join(ctx.ProjectRoot, ".git", "hooks")
	if dirExists(hooksDir) {
		result.Rules = append(result.Rules,
			seatbelt.DenyRule(fmt.Sprintf(`(deny file-write* (subpath "%s"))`, hooksDir)))
		result.Protected = append(result.Protected, hooksDir)
	}

	return result
}

// scanEnvFiles walks the project root for .env* and .envrc files,
// skipping known non-project directories.
func scanEnvFiles(root string) []string {
	var found []string
	filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable
		}
		if d.IsDir() {
			if skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		name := d.Name()
		if isEnvFile(name) {
			found = append(found, path)
		}
		return nil
	})
	return found
}

func isEnvFile(name string) bool {
	if name == ".envrc" {
		return true
	}
	if name == ".env" || strings.HasPrefix(name, ".env.") {
		return true
	}
	return false
}
```

**Note:** This implementation uses `filepath.WalkDir` with hardcoded skip dirs instead of a gitignore library. This is simpler, covers the 90% case, and avoids adding a dependency. The gitignore library can be added later if needed.

- [ ] **Step 4: Register in registry.go**

Add `ProjectSecretsGuard()` to the default guards section.

- [ ] **Step 5: Run tests**

Run: `go test ./pkg/seatbelt/guards/ -run TestProjectSecrets -v`

- [ ] **Step 6: Commit**

Stage: `git add pkg/seatbelt/guards/guard_project_secrets.go pkg/seatbelt/guards/registry.go pkg/seatbelt/guards/*_test.go`
Run: `/commit --style classic add project-secrets guard to deny .env files and .git/hooks writes`

---

### Task 7: Promote Guards from opt-in to default

**Files:**
- Modify: `pkg/seatbelt/guards/guard_sensitive.go` (docker, github-cli, npm, netrc)
- Modify: `pkg/seatbelt/guards/guard_kubernetes.go`
- Modify: `pkg/seatbelt/guards/registry.go`

- [ ] **Step 1: Write the failing test**

```go
func TestGuardPromotions_DefaultType(t *testing.T) {
	promoted := []struct {
		name string
		guard seatbelt.Guard
	}{
		{"docker", guards.DockerGuard()},
		{"github-cli", guards.GithubCLIGuard()},
		{"npm", guards.NPMGuard()},
		{"netrc", guards.NetrcGuard()},
		{"kubernetes", guards.KubernetesGuard()},
	}
	for _, p := range promoted {
		if p.guard.Type() != "default" {
			t.Errorf("guard %s: expected type 'default', got %q", p.name, p.guard.Type())
		}
	}
}
```

- [ ] **Step 2: Change Type() returns**

In `pkg/seatbelt/guards/guard_sensitive.go`:
- `dockerGuard.Type()`: change `"opt-in"` to `"default"` (line 23)
- `githubCLIGuard.Type()`: change `"opt-in"` to `"default"` (line 62)
- `npmGuard.Type()`: change `"opt-in"` to `"default"` (line 88)
- `netrcGuard.Type()`: change `"opt-in"` to `"default"` (line 125)

In `pkg/seatbelt/guards/guard_kubernetes.go`:
- `kubernetesGuard.Type()`: change `"opt-in"` to `"default"` (line 19)

- [ ] **Step 3: Update registry.go**

Move `DockerGuard()`, `GithubCLIGuard()`, `NPMGuard()`, `NetrcGuard()` from the opt-in section to the default section. `KubernetesGuard()` is already in the default section (line 34).

The opt-in section should only contain `VercelGuard()` after this change.

- [ ] **Step 4: Run tests**

Run: `go test ./pkg/seatbelt/guards/ -run TestGuardPromotions -v`
Then: `go test ./pkg/seatbelt/guards/ -v` (full suite)

- [ ] **Step 5: Commit**

Stage: `git add pkg/seatbelt/guards/guard_sensitive.go pkg/seatbelt/guards/guard_kubernetes.go pkg/seatbelt/guards/registry.go pkg/seatbelt/guards/*_test.go`
Run: `/commit --style classic promote docker, github-cli, npm, netrc, and kubernetes guards to default`

---

### Task 8: Simplify Existing Guards

Remove redundant read rules from guards where system-runtime broad reads or filesystem scoped reads now cover them.

**Files:**
- Modify: `pkg/seatbelt/guards/guard_nix_toolchain.go`
- Modify: `pkg/seatbelt/guards/guard_git_integration.go`
- Modify: `pkg/seatbelt/guards/guard_keychain.go`
- Modify: `pkg/seatbelt/guards/guard_ssh_keys.go`
- Modify: `pkg/seatbelt/guards/toolchain_test.go`
- Modify: other test files as needed

- [ ] **Step 1: Simplify nix-toolchain guard**

Remove all read rules, keep detection gate + daemon socket + write paths. The Rules method becomes:

```go
func (g *nixToolchainGuard) Rules(ctx *seatbelt.Context) seatbelt.GuardResult {
	if !dirExists("/nix/store") {
		return seatbelt.GuardResult{
			Skipped: []string{"/nix/store not found — nix not installed"},
		}
	}

	home := ctx.HomeDir

	return seatbelt.GuardResult{Rules: []seatbelt.Rule{
		// Nix daemon socket
		seatbelt.SectionAllow("Nix daemon socket"),
		seatbelt.AllowRule(`(allow network-outbound
    (remote unix-socket (path-literal "/nix/var/nix/daemon-socket/socket"))
)`),

		// Nix user paths (write only — reads covered by filesystem guard)
		seatbelt.SectionAllow("Nix user paths (write)"),
		seatbelt.AllowRule(`(allow file-write*
    ` + seatbelt.HomeSubpath(home, ".nix-profile") + `
    ` + seatbelt.HomeSubpath(home, ".local/state/nix") + `
    ` + seatbelt.HomeSubpath(home, ".cache/nix") + `
)`),
	}}
}
```

**Important:** Change `file-read* file-write*` to just `file-write*` for the nix user paths since reads are covered by the filesystem guard's `~/.nix-profile`, `~/.cache`, `~/.local` allows.

- [ ] **Step 2: Empty git-integration guard**

Replace the Rules method to return empty:

```go
func (g *gitIntegrationGuard) Rules(ctx *seatbelt.Context) seatbelt.GuardResult {
	// All git config reads are now covered by the filesystem guard's
	// scoped $HOME reads (.gitconfig dotfile, ~/.config/git/, ~/.ssh/).
	return seatbelt.GuardResult{}
}
```

- [ ] **Step 3: Simplify keychain guard**

Remove "System keychain" and "Keychain metadata traversal" read rules. Keep "User keychain" write rules, "Security Mach services", and "Security IPC shared memory".

- [ ] **Step 4: Simplify ssh-keys guard**

Remove the allow rules for safe files (`known_hosts`, `config`, `.pub` files). These are now readable via the filesystem guard's `~/.ssh` subpath allow. Keep only the deny rules for private keys and the `~/.ssh` metadata rule.

- [ ] **Step 5: Update all affected tests**

Update tests in `toolchain_test.go`, `system_test.go`, and any other files that assert specific read paths in simplified guards. The nix test should check for daemon socket and write paths only. The git test should verify empty rules. Etc.

- [ ] **Step 6: Run full test suite**

Run: `go test ./pkg/seatbelt/guards/ -v`

- [ ] **Step 7: Commit**

Stage: `git add pkg/seatbelt/guards/guard_nix_toolchain.go pkg/seatbelt/guards/guard_git_integration.go pkg/seatbelt/guards/guard_keychain.go pkg/seatbelt/guards/guard_ssh_keys.go pkg/seatbelt/guards/*_test.go`
Run: `/commit --style classic simplify guards by removing read rules now covered by broad system and scoped home reads`

---

### Task 9: Update Contract Tests

**Files:**
- Modify: `internal/sandbox/policy_contract_test.go`

- [ ] **Step 1: Update contract tests**

- Update `TestContract_ReadableExtraProducesRule` — ExtraReadable should produce `file-read*` allow rules (individual paths, not batch)
- Add `TestContract_ReadableExtraOptOutsDeny` — verify a path in `readable_extra` causes project-secrets, shell-history, and dev-credentials guards to skip their deny for that path
- Add `TestContract_ScopedHomeReads` — verify default profile contains `.config`, `.cache`, `.ssh` reads but NOT `~/Documents`
- Add `TestContract_MountedVolumesDenied` — verify default profile denies `/Volumes`
- Add `TestContract_CrossGuardSafety_NodeToolchain` — verify node-toolchain write paths are not blocked by any new deny guard
- Add `TestContract_CrossGuardSafety_NixToolchain` — verify nix-toolchain write paths are not blocked by any new deny guard
- Update `TestContract_AllowSubprocessFalseProducesDenyFork` if the system-runtime changes broke it

- [ ] **Step 2: Run contract tests**

Run: `go test ./internal/sandbox/ -run TestContract -v`

- [ ] **Step 3: Commit**

Stage: `git add internal/sandbox/policy_contract_test.go`
Run: `/commit --style classic update contract tests for split-read model and new guards`

---

### Task 10: Update Integration Tests

**Files:**
- Modify: `internal/sandbox/integration_test.go`
- Modify: `internal/sandbox/toolchain_integration_test.go`

- [ ] **Step 1: Update integration tests**

- `TestSandbox_WriteToReadOnlyBlocked` — remove `ExtraReadable` setup (the path was in a temp dir, which is now readable via broad system reads). Test that writing to a non-writable system path is blocked instead.
- `TestSandbox_ExtraWritablePath` — should still work unchanged
- Add `TestSandbox_OtherProjectNotReadable` (integration, behind build tag) — create a temp dir simulating another project outside project root under `$HOME`, verify reads are blocked
- Add `TestSandbox_HomeDocumentsNotReadable` (integration, behind build tag) — verify `~/Documents/` is not readable inside the sandbox (scoped home reads exclude it)
- Update toolchain integration tests if needed

- [ ] **Step 2: Verify compilation**

Run: `go build -tags "darwin,integration" ./internal/sandbox/`

- [ ] **Step 3: Commit**

Stage: `git add internal/sandbox/integration_test.go internal/sandbox/toolchain_integration_test.go`
Run: `/commit --style classic update integration tests for split-read sandbox model`

---

### Task 11: Update Darwin Profile Tests

**Files:**
- Modify: `internal/sandbox/darwin_test.go`

- [ ] **Step 1: Update profile rendering tests**

Tests that render full profiles (`TestGenerateSeatbeltProfile_*`) need updating for:
- Broad system reads instead of specific path lists
- Scoped `$HOME` reads instead of full `$HOME` subpath
- New guards appearing in default profile (mounted-volumes, shell-history, dev-credentials, project-secrets)
- Promoted guards appearing in default profile

- [ ] **Step 2: Run full sandbox test suite**

Run: `go test ./internal/sandbox/ -v`

- [ ] **Step 3: Commit**

Stage: `git add internal/sandbox/darwin_test.go`
Run: `/commit --style classic update darwin profile tests for split-read model`

---

### Task 12: Final Verification

- [ ] **Step 1: Run full test suite**

Run: `go test ./...`
Expected: All pass

- [ ] **Step 2: Run vet**

Run: `go vet ./...`
Expected: No issues

- [ ] **Step 3: Verify key behaviors manually (if inside aide sandbox)**

- `stat /Library/Developer` — should succeed (broad system reads)
- `stat /nix` — should succeed
- `make --version` — should succeed (if gnumake in nix-profile)
- `cat ~/.zsh_history` — should fail (shell-history guard)

- [ ] **Step 4: Commit any fixups**

If any fixups needed, stage and commit.
Run: `/commit --style classic <description of fixups>`
