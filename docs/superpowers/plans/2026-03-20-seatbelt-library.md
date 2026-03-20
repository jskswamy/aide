# pkg/seatbelt Library Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a composable macOS Seatbelt profile library at `pkg/seatbelt/` that generates working sandbox profiles for AI coding agents, using `(deny default)` with granular Mach service/IPC/PTY rules ported from agent-safehouse.

**Architecture:** Module interface pattern — each module contributes Seatbelt rules. A `Profile` builder composes modules and renders to `.sb` format. aide's `darwin.go` becomes a thin consumer.

**Tech Stack:** Go, macOS Seatbelt (sandbox-exec)

**Spec:** `docs/superpowers/specs/2026-03-20-seatbelt-library-design.md`
**Reference:** [agent-safehouse](https://github.com/eugene1g/agent-safehouse) profiles

---

## File Structure

```
pkg/seatbelt/
├── doc.go                    # Package doc with attribution to agent-safehouse
├── profile.go                # Profile builder: New(), Use(), WithContext(), Render()
├── profile_test.go           # Profile composition tests
├── module.go                 # Module interface, Rule type, Context, helpers
├── render.go                 # .sb format rendering
├── render_test.go            # Render formatting tests
├── path.go                   # seatbeltPath(), expandGlobs() (moved from darwin.go)
├── path_test.go              # Path helper tests
├── modules/
│   ├── base.go               # (version 1)(deny default)
│   ├── base_test.go
│   ├── system.go             # ~120 rules: process, Mach services, IPC, devices, temp
│   ├── system_test.go
│   ├── network.go            # NetworkOpen, NetworkOutbound, NetworkNone, port filtering
│   ├── network_test.go
│   ├── filesystem.go         # Writable paths, denied paths (deferred to end)
│   ├── filesystem_test.go
│   ├── node.go               # Node.js ecosystem (npm, yarn, pnpm caches)
│   ├── nix.go                # Nix store, profiles, symlink chain
│   ├── git.go                # .gitconfig, .ssh/config, known_hosts
│   ├── keychain.go           # macOS Keychain + SecurityServer Mach services
│   ├── claude.go             # Claude Code paths
│   └── toolchain_test.go     # Tests for node, nix, git, keychain, claude modules

internal/sandbox/
├── darwin.go                 # MODIFIED: thin consumer of pkg/seatbelt
├── darwin_test.go            # MODIFIED: updated tests
```

---

### Task 1: Core types — Module interface, Rule, Context, rendering

Create the foundation: the types that everything else builds on.

**Files:**
- Create: `pkg/seatbelt/doc.go`
- Create: `pkg/seatbelt/module.go`
- Create: `pkg/seatbelt/render.go`
- Create: `pkg/seatbelt/render_test.go`
- Create: `pkg/seatbelt/path.go`
- Create: `pkg/seatbelt/path_test.go`

- [ ] **Step 1: Write tests for Rule rendering**

Create `pkg/seatbelt/render_test.go`:

```go
package seatbelt

import (
    "strings"
    "testing"
)

func TestRenderRules_Comment(t *testing.T) {
    rules := []Rule{Comment("test section")}
    out := renderRules(rules)
    if !strings.Contains(out, ";; test section") {
        t.Errorf("expected comment, got %q", out)
    }
}

func TestRenderRules_Allow(t *testing.T) {
    rules := []Rule{Allow("process-exec")}
    out := renderRules(rules)
    if !strings.Contains(out, "(allow process-exec)") {
        t.Errorf("expected allow rule, got %q", out)
    }
}

func TestRenderRules_Raw(t *testing.T) {
    block := "(deny file-write*\n    (require-not\n        (require-any\n            (subpath \"/tmp\"))))"
    rules := []Rule{Raw(block)}
    out := renderRules(rules)
    if !strings.Contains(out, block) {
        t.Errorf("expected raw block, got %q", out)
    }
}
```

- [ ] **Step 2: Run tests — verify they fail**

Run: `go test ./pkg/seatbelt/ -run TestRenderRules -v`
Expected: FAIL (package doesn't exist yet)

- [ ] **Step 3: Create doc.go with package doc and attribution**

Create `pkg/seatbelt/doc.go`:

```go
// Package seatbelt provides composable macOS Seatbelt sandbox profiles.
//
// It generates .sb profile strings for use with sandbox-exec, composing
// modular rule sets (system runtime, network, filesystem, toolchains,
// agent-specific paths) into a complete profile.
//
// # Attribution
//
// The Seatbelt rules in this library — particularly the system runtime
// operations, Mach service lookups, toolchain paths, and integration
// profiles — are ported from agent-safehouse by Eugene Goldin:
// https://github.com/eugene1g/agent-safehouse
//
// Agent-safehouse provides composable Seatbelt policy profiles for AI
// coding agents and has validated profiles for 14 agents.
//
// # Usage
//
//	profile := seatbelt.New(homeDir).Use(
//	    modules.Base(),
//	    modules.SystemRuntime(),
//	    modules.Network(modules.NetworkOpen),
//	    modules.Filesystem(modules.FilesystemConfig{
//	        Writable: []string{projectRoot, tmpDir},
//	        Denied:   []string{"~/.ssh/id_*"},
//	    }),
//	    modules.ClaudeAgent(),
//	)
//	sbText, err := profile.Render()
package seatbelt
```

- [ ] **Step 4: Create module.go with core types**

Create `pkg/seatbelt/module.go`:

```go
package seatbelt

import "path/filepath"

// Module contributes Seatbelt rules to a profile.
type Module interface {
    // Name returns a human-readable name for section comments.
    Name() string
    // Rules returns the Seatbelt rules this module contributes.
    Rules(ctx *Context) []Rule
}

// Context provides runtime information to modules.
type Context struct {
    HomeDir     string
    ProjectRoot string
    TempDir     string
    RuntimeDir  string
}

// HomePath returns homeDir joined with a relative path.
func (c *Context) HomePath(rel string) string {
    return filepath.Join(c.HomeDir, rel)
}

// Rule represents a Seatbelt rule or comment block.
type Rule struct {
    comment string
    lines   string
}

// Allow creates an (allow <operation>) rule.
func Allow(operation string) Rule {
    return Rule{lines: "(allow " + operation + ")"}
}

// Deny creates a (deny <operation>) rule.
func Deny(operation string) Rule {
    return Rule{lines: "(deny " + operation + ")"}
}

// Comment creates a ;; comment line.
func Comment(text string) Rule {
    return Rule{comment: text}
}

// Section creates a ;; --- section header --- comment.
func Section(name string) Rule {
    return Rule{comment: "--- " + name + " ---"}
}

// Raw creates a rule from raw Seatbelt text (may be multi-line).
func Raw(text string) Rule {
    return Rule{lines: text}
}
```

- [ ] **Step 5: Create render.go**

Create `pkg/seatbelt/render.go`:

```go
package seatbelt

import (
    "fmt"
    "strings"
)

// renderRules converts a slice of Rules to Seatbelt profile text.
func renderRules(rules []Rule) string {
    var b strings.Builder
    for _, r := range rules {
        if r.comment != "" {
            fmt.Fprintf(&b, ";; %s\n", r.comment)
        }
        if r.lines != "" {
            b.WriteString(r.lines)
            b.WriteByte('\n')
        }
    }
    return b.String()
}

// renderModule renders a module with a section header.
func renderModule(m Module, ctx *Context) string {
    var b strings.Builder
    fmt.Fprintf(&b, "\n;; === %s ===\n", m.Name())
    b.WriteString(renderRules(m.Rules(ctx)))
    return b.String()
}
```

- [ ] **Step 6: Create path.go — move seatbeltPath and expandGlobs from sandbox.go**

Create `pkg/seatbelt/path.go`:

```go
package seatbelt

import (
    "fmt"
    "os"
    "path/filepath"
    "strings"
)

// SeatbeltPath returns the Seatbelt path expression for a filesystem path.
// Directories use (subpath ...), files use (literal ...).
func SeatbeltPath(p string) string {
    info, err := os.Stat(p)
    if err == nil && info.IsDir() {
        return fmt.Sprintf(`(subpath "%s")`, p)
    }
    return fmt.Sprintf(`(literal "%s")`, p)
}

// HomeSubpath returns (subpath "<home>/<rel>") for use in profile rules.
func HomeSubpath(home, rel string) string {
    return fmt.Sprintf(`(subpath "%s")`, filepath.Join(home, rel))
}

// HomeLiteral returns (literal "<home>/<rel>") for use in profile rules.
func HomeLiteral(home, rel string) string {
    return fmt.Sprintf(`(literal "%s")`, filepath.Join(home, rel))
}

// HomePrefix returns (prefix "<home>/<rel>") for use in profile rules.
func HomePrefix(home, rel string) string {
    return fmt.Sprintf(`(prefix "%s")`, filepath.Join(home, rel))
}

// ExpandGlobs expands glob patterns in a list of paths.
// Non-glob paths are passed through unchanged.
func ExpandGlobs(patterns []string) []string {
    var result []string
    for _, p := range patterns {
        if strings.ContainsAny(p, "*?[") {
            matches, _ := filepath.Glob(p)
            result = append(result, matches...)
        } else {
            result = append(result, p)
        }
    }
    return result
}
```

- [ ] **Step 7: Create path_test.go**

```go
package seatbelt

import (
    "os"
    "path/filepath"
    "testing"
)

func TestSeatbeltPath_Directory(t *testing.T) {
    dir := t.TempDir()
    got := SeatbeltPath(dir)
    want := `(subpath "` + dir + `")`
    if got != want {
        t.Errorf("SeatbeltPath(%q) = %q, want %q", dir, got, want)
    }
}

func TestSeatbeltPath_File(t *testing.T) {
    dir := t.TempDir()
    f := filepath.Join(dir, "test.txt")
    os.WriteFile(f, []byte("x"), 0644)
    got := SeatbeltPath(f)
    want := `(literal "` + f + `")`
    if got != want {
        t.Errorf("SeatbeltPath(%q) = %q, want %q", f, got, want)
    }
}

func TestExpandGlobs_NoGlob(t *testing.T) {
    got := ExpandGlobs([]string{"/tmp/foo"})
    if len(got) != 1 || got[0] != "/tmp/foo" {
        t.Errorf("expected [/tmp/foo], got %v", got)
    }
}
```

- [ ] **Step 8: Run all tests — verify pass**

Run: `go test ./pkg/seatbelt/ -v`

- [ ] **Step 9: Commit**

```bash
git add pkg/seatbelt/
```
Then `/commit`

---

### Task 2: Profile builder — New(), Use(), WithContext(), Render()

**Files:**
- Create: `pkg/seatbelt/profile.go`
- Create: `pkg/seatbelt/profile_test.go`

- [ ] **Step 1: Write tests for profile composition**

Create `pkg/seatbelt/profile_test.go`:

```go
package seatbelt

import (
    "strings"
    "testing"
)

// testModule is a simple module for testing.
type testModule struct {
    name  string
    rules []Rule
}

func (m *testModule) Name() string            { return m.name }
func (m *testModule) Rules(_ *Context) []Rule { return m.rules }

func TestProfile_Render_EmptyProfile(t *testing.T) {
    p := New("/home/user")
    out, err := p.Render()
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if out != "" {
        t.Errorf("empty profile should render empty string, got %q", out)
    }
}

func TestProfile_Render_SingleModule(t *testing.T) {
    p := New("/home/user").Use(&testModule{
        name:  "test",
        rules: []Rule{Allow("process-exec")},
    })
    out, err := p.Render()
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if !strings.Contains(out, "(allow process-exec)") {
        t.Errorf("expected allow rule in output, got %q", out)
    }
    if !strings.Contains(out, "=== test ===") {
        t.Errorf("expected module name header, got %q", out)
    }
}

func TestProfile_Render_ModuleOrder(t *testing.T) {
    p := New("/home/user").Use(
        &testModule{name: "first", rules: []Rule{Allow("process-exec")}},
        &testModule{name: "second", rules: []Rule{Allow("process-fork")}},
    )
    out, err := p.Render()
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    firstIdx := strings.Index(out, "first")
    secondIdx := strings.Index(out, "second")
    if firstIdx > secondIdx {
        t.Error("modules should render in Use() order")
    }
}

func TestProfile_WithContext(t *testing.T) {
    var captured Context
    captureModule := &contextCapture{captured: &captured}
    p := New("/home/user").
        WithContext(func(ctx *Context) {
            ctx.ProjectRoot = "/tmp/project"
            ctx.TempDir = "/tmp"
        }).
        Use(captureModule)
    p.Render()
    if captured.HomeDir != "/home/user" {
        t.Errorf("expected HomeDir=/home/user, got %q", captured.HomeDir)
    }
    if captured.ProjectRoot != "/tmp/project" {
        t.Errorf("expected ProjectRoot=/tmp/project, got %q", captured.ProjectRoot)
    }
}

type contextCapture struct {
    captured *Context
}

func (c *contextCapture) Name() string { return "capture" }
func (c *contextCapture) Rules(ctx *Context) []Rule {
    *c.captured = *ctx
    return nil
}
```

- [ ] **Step 2: Run tests — verify they fail**

Run: `go test ./pkg/seatbelt/ -run TestProfile -v`

- [ ] **Step 3: Implement profile.go**

Create `pkg/seatbelt/profile.go`:

```go
package seatbelt

import "strings"

// Profile composes Seatbelt modules into a complete .sb profile.
type Profile struct {
    modules []Module
    ctx     Context
}

// New creates a profile builder for the given home directory.
func New(homeDir string) *Profile {
    return &Profile{
        ctx: Context{HomeDir: homeDir},
    }
}

// Use adds modules to the profile. Modules render in the order added.
func (p *Profile) Use(modules ...Module) *Profile {
    p.modules = append(p.modules, modules...)
    return p
}

// WithContext sets additional context fields.
func (p *Profile) WithContext(fn func(*Context)) *Profile {
    fn(&p.ctx)
    return p
}

// Render generates the Seatbelt .sb profile string.
func (p *Profile) Render() (string, error) {
    if len(p.modules) == 0 {
        return "", nil
    }
    var b strings.Builder
    for _, m := range p.modules {
        b.WriteString(renderModule(m, &p.ctx))
    }
    return b.String(), nil
}
```

- [ ] **Step 4: Run tests — verify pass**

Run: `go test ./pkg/seatbelt/ -v`

- [ ] **Step 5: Commit**

```bash
git add pkg/seatbelt/profile.go pkg/seatbelt/profile_test.go
```
Then `/commit`

---

### Task 3: Base + SystemRuntime modules (the big one)

Port agent-safehouse's `00-base.sb` and `10-system-runtime.sb` (~120 rules).

**Files:**
- Create: `pkg/seatbelt/modules/base.go`
- Create: `pkg/seatbelt/modules/system.go`
- Create: `pkg/seatbelt/modules/base_test.go`
- Create: `pkg/seatbelt/modules/system_test.go`

- [ ] **Step 1: Write tests for Base module**

```go
package modules

import (
    "strings"
    "testing"

    "github.com/jskswamy/aide/pkg/seatbelt"
)

func TestBase_DenyDefault(t *testing.T) {
    m := Base()
    rules := m.Rules(&seatbelt.Context{HomeDir: "/home/user"})
    out := renderTestRules(rules)
    if !strings.Contains(out, "(version 1)") {
        t.Error("Base should emit (version 1)")
    }
    if !strings.Contains(out, "(deny default)") {
        t.Error("Base should emit (deny default)")
    }
}
```

- [ ] **Step 2: Write tests for SystemRuntime module**

```go
func TestSystemRuntime_MachServices(t *testing.T) {
    m := SystemRuntime()
    rules := m.Rules(&seatbelt.Context{HomeDir: "/home/user"})
    out := renderTestRules(rules)

    // Must contain specific Mach services
    services := []string{
        "com.apple.logd",
        "com.apple.trustd.agent",
        "com.apple.dnssd.service",
        "com.apple.SecurityServer",
    }
    for _, svc := range services {
        if !strings.Contains(out, svc) {
            t.Errorf("SystemRuntime should include mach-lookup for %s", svc)
        }
    }
}

func TestSystemRuntime_ProcessRules(t *testing.T) {
    m := SystemRuntime()
    rules := m.Rules(&seatbelt.Context{HomeDir: "/home/user"})
    out := renderTestRules(rules)
    if !strings.Contains(out, "(allow process-exec)") {
        t.Error("should allow process-exec")
    }
    if !strings.Contains(out, "(allow pseudo-tty)") {
        t.Error("should allow pseudo-tty")
    }
    if !strings.Contains(out, "(allow system-socket)") {
        t.Error("should allow system-socket")
    }
}

func TestSystemRuntime_TempDirs(t *testing.T) {
    m := SystemRuntime()
    rules := m.Rules(&seatbelt.Context{HomeDir: "/home/user"})
    out := renderTestRules(rules)
    if !strings.Contains(out, "/private/tmp") {
        t.Error("should include /private/tmp")
    }
    if !strings.Contains(out, "/private/var/folders") {
        t.Error("should include /private/var/folders")
    }
}

// Helper
func renderTestRules(rules []seatbelt.Rule) string {
    // Render rules to text for assertion
    var b strings.Builder
    for _, r := range rules {
        b.WriteString(r.String())
        b.WriteByte('\n')
    }
    return b.String()
}
```

- [ ] **Step 3: Implement base.go**

Port from agent-safehouse `00-base.sb`:

```go
package modules

import "github.com/jskswamy/aide/pkg/seatbelt"

type baseModule struct{}

func Base() seatbelt.Module { return &baseModule{} }

func (m *baseModule) Name() string { return "Base" }

func (m *baseModule) Rules(_ *seatbelt.Context) []seatbelt.Rule {
    return []seatbelt.Rule{
        seatbelt.Raw("(version 1)"),
        seatbelt.Raw("(deny default)"),
    }
}
```

- [ ] **Step 4: Implement system.go**

Port from agent-safehouse `10-system-runtime.sb`. This is the largest module (~120 rules). Use the exact rules from agent-safehouse with Go string formatting. Include the `file-read*` allows for system paths, process rules, Mach services, temp dirs, device nodes, file-ioctl, IPC, and user-preference-read.

The file header should credit agent-safehouse:

```go
// System runtime module for macOS Seatbelt profiles.
//
// Rules ported from agent-safehouse by Eugene Goldin:
// https://github.com/eugene1g/agent-safehouse
// Source: profiles/10-system-runtime.sb
package modules
```

- [ ] **Step 5: Run tests — verify pass**

Run: `go test ./pkg/seatbelt/modules/ -v`

- [ ] **Step 6: Commit**

```bash
git add pkg/seatbelt/modules/
```
Then `/commit`

---

### Task 4: Network + Filesystem modules

**Files:**
- Create: `pkg/seatbelt/modules/network.go`
- Create: `pkg/seatbelt/modules/network_test.go`
- Create: `pkg/seatbelt/modules/filesystem.go`
- Create: `pkg/seatbelt/modules/filesystem_test.go`

- [ ] **Step 1: Write network tests**

Test `NetworkOpen`, `NetworkOutbound`, `NetworkNone`, port filtering.

- [ ] **Step 2: Implement network.go**

Three modes: `NetworkOpen` (`allow network*`), `NetworkOutbound` (with optional port filtering), `NetworkNone` (no network rules — `deny default` covers it).

- [ ] **Step 3: Write filesystem tests**

Test writable paths emit `(allow file-read* file-write* ...)`, denied paths emit `(deny file-read-data ...)` and `(deny file-write* ...)`.

- [ ] **Step 4: Implement filesystem.go**

`Filesystem(FilesystemConfig)` — writable and readable paths emitted as allows; denied paths emitted as denies. Denied paths rendered separately (caller should ensure they are added last to the profile for precedence).

- [ ] **Step 5: Run tests, commit**

---

### Task 5: Toolchain + Integration + Agent modules

**Files:**
- Create: `pkg/seatbelt/modules/node.go`
- Create: `pkg/seatbelt/modules/nix.go`
- Create: `pkg/seatbelt/modules/git.go`
- Create: `pkg/seatbelt/modules/keychain.go`
- Create: `pkg/seatbelt/modules/claude.go`
- Create: `pkg/seatbelt/modules/toolchain_test.go`

- [ ] **Step 1: Write tests for each module**

Each module should emit specific paths. Test by rendering rules and asserting expected paths/operations are present.

- [ ] **Step 2: Implement node.go**

Port from agent-safehouse `30-toolchains/node.sb`: npm, yarn, pnpm, corepack cache dirs.

- [ ] **Step 3: Implement nix.go**

Our own: `/nix/store`, `/nix/var`, `/run/current-system`, `~/.nix-profile`, `~/.local/state/nix`.

- [ ] **Step 4: Implement git.go**

Port from agent-safehouse `50-integrations-core/git.sb`: `.gitconfig`, `.ssh/config`, `.ssh/known_hosts`.

- [ ] **Step 5: Implement keychain.go**

Port from agent-safehouse `55-integrations-optional/keychain.sb`: Keychain files + SecurityServer Mach services.

- [ ] **Step 6: Implement claude.go**

Port from agent-safehouse `60-agents/claude-code.sb`: `~/.claude`, `~/.local/share/claude`, `~/.config/claude`, `~/.mcp.json`, etc.

- [ ] **Step 7: Run tests, commit**

---

### Task 6: Wire aide's darwin.go to use pkg/seatbelt

Replace `generateSeatbeltProfile()` in `internal/sandbox/darwin.go` with a thin consumer of the library.

**Files:**
- Modify: `internal/sandbox/darwin.go`
- Modify: `internal/sandbox/darwin_test.go`

- [ ] **Step 1: Rewrite generateSeatbeltProfile to compose seatbelt modules**

```go
func generateSeatbeltProfile(policy Policy) (string, error) {
    homeDir, _ := os.UserHomeDir()

    p := seatbelt.New(homeDir).
        WithContext(func(ctx *seatbelt.Context) {
            // ProjectRoot, TempDir, RuntimeDir set by caller via Policy.Writable
        }).
        Use(
            modules.Base(),
            modules.SystemRuntime(),
            networkModule(policy),
            modules.Filesystem(modules.FilesystemConfig{
                Writable: policy.Writable,
                Denied:   policy.Denied,
            }),
            modules.NodeToolchain(),
            modules.NixToolchain(),
            modules.GitIntegration(),
            modules.KeychainIntegration(),
            modules.ClaudeAgent(),
        )

    return p.Render()
}

func networkModule(policy Policy) seatbelt.Module {
    switch policy.Network {
    case NetworkNone:
        return modules.Network(modules.NetworkNone)
    case NetworkOutbound:
        opts := modules.PortOpts{
            AllowPorts: policy.AllowPorts,
            DenyPorts:  policy.DenyPorts,
        }
        return modules.NetworkWithPorts(modules.NetworkOutbound, opts)
    default:
        return modules.Network(modules.NetworkOpen)
    }
}
```

- [ ] **Step 2: Remove `seatbeltPath()` from darwin.go** (now in `pkg/seatbelt/path.go`)

- [ ] **Step 3: Update tests**

Update `darwin_test.go` to assert the new profile structure:
- Contains `(deny default)` not `(allow default)`
- Contains specific Mach services
- Contains denied paths with `(deny file-read-data ...)`
- Contains writable paths with `(allow file-read* file-write* ...)`

- [ ] **Step 4: Run all tests**

Run: `go test ./...`

- [ ] **Step 5: Commit**

Then `/commit`

---

### Task 7: End-to-end verification

**Files:** None (verification only)

- [ ] **Step 1: Build aide**

Run: `go build -o /tmp/aide-test ./cmd/aide`

- [ ] **Step 2: Run all tests**

Run: `go test ./...`

- [ ] **Step 3: Inspect generated profile**

Run: `go run ./cmd/aide sandbox test 2>&1 | head -50`

Verify:
- Contains `(deny default)` (NOT `(allow default)`)
- Contains specific Mach services (`com.apple.logd`, `com.apple.trustd.agent`, etc.)
- Contains `(allow pseudo-tty)`, `(allow system-socket)`
- Contains denied paths with `(deny file-read-data ...)`
- Contains writable paths

- [ ] **Step 4: Test agent works non-interactively**

```bash
go run ./cmd/aide sandbox test > /tmp/aide-test.sb
sandbox-exec -f /tmp/aide-test.sb $(which claude) --version
```
Expected: Prints version

- [ ] **Step 5: Test agent works with prompt**

```bash
sandbox-exec -f /tmp/aide-test.sb $(which claude) -p "say hello" </dev/null
```
Expected: Prints response (NOT hang)

- [ ] **Step 6: Test write restriction**

```bash
sandbox-exec -f /tmp/aide-test.sb touch ~/test-sandbox-write.txt
```
Expected: `Operation not permitted`

- [ ] **Step 7: Test sensitive file denied**

```bash
sandbox-exec -f /tmp/aide-test.sb cat ~/.ssh/id_ed25519
```
Expected: `Operation not permitted` (if file exists)
