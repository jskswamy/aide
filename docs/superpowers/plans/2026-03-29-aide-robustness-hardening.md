# Aide Robustness Hardening Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Harden all 12 guards against nil/empty/symlink issues, fix the auto-suggest pipeline to always show missing capabilities, and verify nix system path coverage — all test-first.

**Architecture:** Table-driven robustness tests in a single `guard_robustness_test.go` cover all guards uniformly. Shared helpers (`resolveSymlink`, `expandTilde`) move to `helpers.go`. Launcher auto-suggest removes the zero-caps gate and shows detected-but-not-enabled capabilities in the banner via a new `SuggestedCaps` field.

**Tech Stack:** Go, macOS Seatbelt, Go text/template

**Spec:** `docs/superpowers/specs/2026-03-29-guard-robustness-design.md`

---

### Task 1: Move shared helpers from gitconfig.go to helpers.go

**Files:**
- Modify: `pkg/seatbelt/guards/helpers.go`
- Modify: `pkg/seatbelt/guards/gitconfig.go`

These helpers are currently private to gitconfig.go but needed by all guards.

- [ ] **Step 1: Add resolveSymlink and expandTilde to helpers.go**

Append to `pkg/seatbelt/guards/helpers.go`:

```go
// resolveSymlink resolves symlinks in a path. Returns the original
// path if resolution fails (e.g., path doesn't exist yet).
func resolveSymlink(path string) string {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return path
	}
	return resolved
}

// expandTilde replaces a leading "~/" with homeDir. Uses string
// concatenation instead of filepath.Join to preserve trailing slashes,
// which gitdir: patterns rely on for prefix matching.
func expandTilde(path, homeDir string) string {
	if strings.HasPrefix(path, "~/") {
		return homeDir + "/" + path[2:]
	}
	if path == "~" {
		return homeDir
	}
	return path
}
```

Add `"path/filepath"` and `"strings"` to the helpers.go import block (they
may already be there — check first, only add missing ones).

- [ ] **Step 2: Remove resolveSymlink and expandTilde from gitconfig.go**

Delete the `resolveSymlink` function (around line 239-245) and the
`expandTilde` function (around line 230-237) from gitconfig.go. The
shared versions in helpers.go are identical.

- [ ] **Step 3: Verify compilation**

Run: `go build ./pkg/seatbelt/guards/`

Expected: no errors — gitconfig.go still references the functions,
now resolved from helpers.go (same package).

- [ ] **Step 4: Run tests**

Run: `go test ./pkg/seatbelt/guards/ -v`

Expected: all pass — behavior unchanged.

- [ ] **Step 5: Commit**

```
git add pkg/seatbelt/guards/helpers.go pkg/seatbelt/guards/gitconfig.go
```

Message: `refactor: move resolveSymlink and expandTilde to shared helpers`

---

### Task 2: Write guard_robustness_test.go (failing tests)

**Files:**
- Create: `pkg/seatbelt/guards/guard_robustness_test.go`

Write all table-driven robustness tests. These MUST fail initially (documenting the bugs).

- [ ] **Step 1: Create guard_robustness_test.go**

Write to `pkg/seatbelt/guards/guard_robustness_test.go`:

```go
package guards_test

import (
	"strings"
	"testing"

	"github.com/jskswamy/aide/pkg/seatbelt"
	"github.com/jskswamy/aide/pkg/seatbelt/guards"
)

// TestGuardRobustness_NilContext verifies every guard handles nil context
// without panicking. Table-driven across all registered guards.
func TestGuardRobustness_NilContext(t *testing.T) {
	for _, g := range guards.AllGuards() {
		t.Run(g.Name(), func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("guard %q panicked on nil context: %v", g.Name(), r)
				}
			}()
			result := g.Rules(nil)
			// Should return without panic — empty or valid result
			_ = result
		})
	}
}

// TestGuardRobustness_EmptyHomeDir verifies every guard handles empty
// HomeDir without producing relative paths or panicking.
func TestGuardRobustness_EmptyHomeDir(t *testing.T) {
	for _, g := range guards.AllGuards() {
		t.Run(g.Name(), func(t *testing.T) {
			ctx := &seatbelt.Context{
				HomeDir:     "",
				ProjectRoot: "/project",
				GOOS:        "darwin",
			}
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("guard %q panicked on empty HomeDir: %v", g.Name(), r)
				}
			}()
			result := g.Rules(ctx)
			output := renderTestRules(result.Rules)

			// No rule should contain a relative path (no leading /)
			// Skip guards that don't emit file paths (base, network)
			if g.Name() == "base" || g.Name() == "network" {
				return
			}

			for _, line := range strings.Split(output, "\n") {
				line = strings.TrimSpace(line)
				// Look for path expressions that are relative
				if strings.Contains(line, `"`) {
					// Extract quoted paths
					for _, part := range strings.Split(line, `"`) {
						if len(part) > 0 && !strings.HasPrefix(part, "/") &&
							!strings.HasPrefix(part, "(") &&
							!strings.HasPrefix(part, ")") &&
							!strings.HasPrefix(part, "*") &&
							!strings.Contains(part, "apple") &&
							!strings.Contains(part, "com.") &&
							part != "" {
							// Check if this looks like a relative file path
							if strings.Contains(part, ".") &&
								!strings.Contains(part, " ") &&
								len(part) > 2 {
								t.Errorf("guard %q emitted relative path with empty HomeDir: %q in rule: %s",
									g.Name(), part, strings.TrimSpace(line))
							}
						}
					}
				}
			}
		})
	}
}
```

- [ ] **Step 2: Run tests to verify they FAIL**

Run: `go test ./pkg/seatbelt/guards/ -run TestGuardRobustness_NilContext -v`

Expected: FAIL — multiple guards panic on nil context.

Run: `go test ./pkg/seatbelt/guards/ -run TestGuardRobustness_EmptyHomeDir -v`

Expected: FAIL — guards emit relative paths.

- [ ] **Step 3: Commit (failing tests)**

```
git add pkg/seatbelt/guards/guard_robustness_test.go
```

Message: `test: add guard robustness tests for nil context and empty HomeDir`

---

### Task 3: Write guard_aide_secrets_test.go (failing tests)

**Files:**
- Create: `pkg/seatbelt/guards/guard_aide_secrets_test.go`

- [ ] **Step 1: Create guard_aide_secrets_test.go**

Write to `pkg/seatbelt/guards/guard_aide_secrets_test.go`:

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

func TestAideSecrets_Metadata(t *testing.T) {
	g := guards.AideSecretsGuard()
	if g.Name() != "aide-secrets" {
		t.Errorf("expected Name() = %q, got %q", "aide-secrets", g.Name())
	}
	if g.Type() != "default" {
		t.Errorf("expected Type() = %q, got %q", "default", g.Type())
	}
	if g.Description() == "" {
		t.Error("expected non-empty Description()")
	}
}

func TestAideSecrets_NilContext(t *testing.T) {
	g := guards.AideSecretsGuard()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked on nil context: %v", r)
		}
	}()
	result := g.Rules(nil)
	if len(result.Rules) != 0 {
		t.Error("expected no rules for nil context")
	}
}

func TestAideSecrets_EmptyHomeDir(t *testing.T) {
	g := guards.AideSecretsGuard()
	ctx := &seatbelt.Context{HomeDir: ""}
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked on empty HomeDir: %v", r)
		}
	}()
	result := g.Rules(ctx)
	if len(result.Rules) != 0 {
		t.Error("expected no rules for empty HomeDir")
	}
}

func TestAideSecrets_SecretsExist(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	secretsDir := filepath.Join(home, ".config", "aide", "secrets")
	if err := os.MkdirAll(secretsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	g := guards.AideSecretsGuard()
	ctx := &seatbelt.Context{HomeDir: home}
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	if !strings.Contains(output, "deny") {
		t.Error("expected deny rules when secrets directory exists")
	}
	if len(result.Protected) == 0 {
		t.Error("expected Protected to list secrets directory")
	}
}

func TestAideSecrets_SecretsMissing(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}

	g := guards.AideSecretsGuard()
	ctx := &seatbelt.Context{HomeDir: home}
	result := g.Rules(ctx)

	if len(result.Rules) != 0 {
		t.Error("expected no rules when secrets directory doesn't exist")
	}
	if len(result.Skipped) == 0 {
		t.Error("expected Skipped message when secrets directory doesn't exist")
	}
}
```

- [ ] **Step 2: Run tests to verify NilContext and EmptyHomeDir FAIL**

Run: `go test ./pkg/seatbelt/guards/ -run TestAideSecrets -v`

Expected: `TestAideSecrets_NilContext` and `TestAideSecrets_EmptyHomeDir` FAIL
(panic). `TestAideSecrets_SecretsExist` and `TestAideSecrets_SecretsMissing`
should PASS (existing logic works when context is valid).

- [ ] **Step 3: Commit (failing tests)**

```
git add pkg/seatbelt/guards/guard_aide_secrets_test.go
```

Message: `test: add aide-secrets guard tests with nil/empty coverage`

---

### Task 4: Fix all guard nil/empty checks

**Files:**
- Modify: `pkg/seatbelt/guards/guard_aide_secrets.go`
- Modify: `pkg/seatbelt/guards/guard_keychain.go`
- Modify: `pkg/seatbelt/guards/guard_node_toolchain.go`
- Modify: `pkg/seatbelt/guards/guard_nix_toolchain.go`
- Modify: `pkg/seatbelt/guards/guard_system_runtime.go`
- Modify: `pkg/seatbelt/guards/guard_dev_credentials.go`
- Modify: `pkg/seatbelt/guards/guard_project_secrets.go`
- Modify: `pkg/seatbelt/guards/guard_custom.go`

- [ ] **Step 1: Fix guard_aide_secrets.go**

Add nil/empty check at the start of `Rules()` (line 22):

```go
func (g *aideSecretsGuard) Rules(ctx *seatbelt.Context) seatbelt.GuardResult {
	if ctx == nil || ctx.HomeDir == "" {
		return seatbelt.GuardResult{}
	}
	result := seatbelt.GuardResult{}
	// ... rest unchanged ...
```

- [ ] **Step 2: Fix guard_keychain.go**

Add nil/empty check at the start of `Rules()` (line 26):

```go
func (g *keychainGuard) Rules(ctx *seatbelt.Context) seatbelt.GuardResult {
	if ctx == nil || ctx.HomeDir == "" {
		return seatbelt.GuardResult{}
	}
	home := ctx.HomeDir
	// ... rest unchanged ...
```

- [ ] **Step 3: Fix guard_node_toolchain.go**

Add nil/empty check at the start of `Rules()` (line 22):

```go
func (g *nodeToolchainGuard) Rules(ctx *seatbelt.Context) seatbelt.GuardResult {
	if ctx == nil || ctx.HomeDir == "" {
		return seatbelt.GuardResult{}
	}
	home := ctx.HomeDir
	// ... rest unchanged ...
```

- [ ] **Step 4: Fix guard_nix_toolchain.go**

Add nil check BEFORE the dirExists check (line 22):

```go
func (g *nixToolchainGuard) Rules(ctx *seatbelt.Context) seatbelt.GuardResult {
	if ctx == nil || ctx.HomeDir == "" {
		return seatbelt.GuardResult{}
	}
	if !dirExists("/nix/store") {
		// ... rest unchanged ...
```

- [ ] **Step 5: Fix guard_system_runtime.go**

Add nil check at the start of `Rules()`. The system-runtime guard is
special — it has a large static rule block before accessing `ctx`. Add
the nil check before line 145 where `ctx.AllowSubprocess` is accessed:

```go
func (g *systemRuntimeGuard) Rules(ctx *seatbelt.Context) seatbelt.GuardResult {
	if ctx == nil {
		return seatbelt.GuardResult{}
	}
	// ... rest of existing static rules ...
```

- [ ] **Step 6: Fix guard_dev_credentials.go**

Add nil/empty check at the start of `Rules()` (line 38):

```go
func (g *devCredentialsGuard) Rules(ctx *seatbelt.Context) seatbelt.GuardResult {
	if ctx == nil || ctx.HomeDir == "" {
		return seatbelt.GuardResult{}
	}
	result := seatbelt.GuardResult{}
	// ... rest unchanged ...
```

- [ ] **Step 7: Fix guard_project_secrets.go**

Add nil check before the ProjectRoot check (line 36):

```go
func (g *projectSecretsGuard) Rules(ctx *seatbelt.Context) seatbelt.GuardResult {
	if ctx == nil {
		return seatbelt.GuardResult{}
	}
	result := seatbelt.GuardResult{}
	if ctx.ProjectRoot == "" {
		// ... rest unchanged ...
```

- [ ] **Step 8: Fix guard_custom.go**

Add nil/empty check at the start of `Rules()` (line 47). Also replace
`expandHome` with the shared `expandTilde`:

```go
func (g *customGuard) Rules(ctx *seatbelt.Context) seatbelt.GuardResult {
	if ctx == nil || ctx.HomeDir == "" {
		return seatbelt.GuardResult{}
	}
	result := seatbelt.GuardResult{}
	// ... rest unchanged ...
```

Then replace `expandHome(ctx, ...)` calls with `expandTilde(..., ctx.HomeDir)`
throughout the file:
- Line 57: `expandHome(ctx, g.cfg.Paths[0])` → `expandTilde(g.cfg.Paths[0], ctx.HomeDir)`
- Line 82: `expandHome(ctx, a)` → `expandTilde(a, ctx.HomeDir)`
- Line 105: `expandHome(ctx, p)` → `expandTilde(p, ctx.HomeDir)`

Then delete the `expandHome` function (lines 111-116).

- [ ] **Step 9: Run robustness tests**

Run: `go test ./pkg/seatbelt/guards/ -run "TestGuardRobustness|TestAideSecrets" -v`

Expected: all PASS

- [ ] **Step 10: Run full test suite**

Run: `go test ./pkg/seatbelt/guards/ -v`

Expected: all PASS (no regressions)

- [ ] **Step 11: Commit**

```
git add pkg/seatbelt/guards/guard_aide_secrets.go \
       pkg/seatbelt/guards/guard_keychain.go \
       pkg/seatbelt/guards/guard_node_toolchain.go \
       pkg/seatbelt/guards/guard_nix_toolchain.go \
       pkg/seatbelt/guards/guard_system_runtime.go \
       pkg/seatbelt/guards/guard_dev_credentials.go \
       pkg/seatbelt/guards/guard_project_secrets.go \
       pkg/seatbelt/guards/guard_custom.go
```

Message: `fix: add nil context and empty HomeDir checks to all guards`

---

### Task 5: Verify nix /etc/nix coverage

**Files:**
- Modify: `pkg/seatbelt/guards/toolchain_test.go`
- Conditionally modify: `pkg/seatbelt/guards/guard_nix_toolchain.go`

- [ ] **Step 1: Write test to verify /etc/nix coverage**

Append to `pkg/seatbelt/guards/toolchain_test.go`:

```go
func TestGuard_NixToolchain_EtcNixCoverage(t *testing.T) {
	if !guards.TestDirExists("/nix/store") {
		t.Skip("nix not installed")
	}

	// The system-runtime guard should cover /etc/nix via (subpath "/private")
	// since on macOS /etc -> /private/etc. Verify by checking the combined
	// profile output.
	sysGuard := guards.SystemRuntimeGuard()
	ctx := &seatbelt.Context{HomeDir: "/Users/testuser", GOOS: "darwin"}
	sysResult := sysGuard.Rules(ctx)
	sysOutput := renderTestRules(sysResult.Rules)

	// /private subpath should cover /etc/nix (via /etc -> /private/etc symlink)
	if !strings.Contains(sysOutput, `"/private"`) &&
		!strings.Contains(sysOutput, `"/etc"`) {
		t.Error("expected system-runtime to cover /etc (via /private subpath)")
	}
}
```

- [ ] **Step 2: Run test**

Run: `go test ./pkg/seatbelt/guards/ -run TestGuard_NixToolchain_EtcNixCoverage -v`

Expected: PASS if system-runtime covers it, FAIL if not.

- [ ] **Step 3: If test FAILS, add /etc/nix rules to nix guard**

Only if the test failed, add to `guard_nix_toolchain.go` after the
channel definitions block:

```go
		// System nix configuration (read-only)
		seatbelt.SectionAllow("System nix configuration"),
		seatbelt.AllowRule(`(allow file-read*
    (subpath "/etc/nix")
)`),
```

- [ ] **Step 4: Run full suite**

Run: `go test ./pkg/seatbelt/guards/ -v`

Expected: all PASS

- [ ] **Step 5: Commit**

```
git add pkg/seatbelt/guards/toolchain_test.go
```

If nix guard was modified:
```
git add pkg/seatbelt/guards/guard_nix_toolchain.go
```

Message: `test: verify nix /etc/nix coverage via system-runtime guard`
or: `fix: add /etc/nix read access to nix toolchain guard`

---

### Task 6: Add SuggestedCaps to BannerData

**Files:**
- Modify: `internal/ui/types.go`

- [ ] **Step 1: Add Suggested field and SuggestedCaps slice**

In `internal/ui/types.go`, add `Suggested bool` to `CapabilityDisplay`:

```go
type CapabilityDisplay struct {
	Name      string
	Paths     []string
	EnvVars   []string
	Source    string
	Disabled  bool
	Suggested bool     // true if detected but not enabled
}
```

Add `SuggestedCaps` to `BannerData` (after `DisabledCaps`):

```go
	DisabledCaps   []CapabilityDisplay
	SuggestedCaps  []CapabilityDisplay // detected but not enabled
```

- [ ] **Step 2: Verify compilation**

Run: `go build ./internal/ui/...`

Expected: compiles (new fields are zero-valued by default).

- [ ] **Step 3: Commit**

```
git add internal/ui/types.go
```

Message: `feat: add SuggestedCaps field to BannerData for auto-suggest`

---

### Task 7: Fix launcher auto-suggest pipeline

**Files:**
- Modify: `internal/launcher/launcher.go:479-486`

- [ ] **Step 1: Replace the gated detection with always-detect logic**

Replace lines 479-486 in `launcher.go`:

```go
	// Project detection: if no capabilities active, suggest based on project files
	if len(data.Capabilities) == 0 && len(data.DisabledCaps) == 0 {
		suggestions := capability.DetectProject(projectRoot)
		if len(suggestions) > 0 {
			data.Warnings = append(data.Warnings,
				fmt.Sprintf("Detected project tools. Suggested: aide --with %s", strings.Join(suggestions, " ")))
		}
	}
```

With:

```go
	// Project detection: always detect and show missing capabilities
	suggestions := capability.DetectProject(projectRoot)
	if len(suggestions) > 0 {
		// Build set of already-enabled capability names
		enabled := make(map[string]bool, len(data.Capabilities))
		for _, c := range data.Capabilities {
			enabled[c.Name] = true
		}
		// Filter to only unresolved suggestions
		for _, name := range suggestions {
			if enabled[name] {
				continue
			}
			// Look up capability definition for display info
			registry := capability.MergedRegistry(nil)
			if cap, ok := registry[name]; ok {
				paths := append([]string{}, cap.Readable...)
				paths = append(paths, cap.Writable...)
				data.SuggestedCaps = append(data.SuggestedCaps, ui.CapabilityDisplay{
					Name:      name,
					Paths:     paths,
					Suggested: true,
					Source:    "detected",
				})
			}
		}
	}
```

Note: `capability.MergedRegistry(nil)` returns builtins only (nil means
no user-defined caps to merge). Import `capability` package if not
already imported.

- [ ] **Step 2: Verify compilation**

Run: `go build ./internal/launcher/...`

Expected: compiles.

- [ ] **Step 3: Commit**

```
git add internal/launcher/launcher.go
```

Message: `fix: always detect capabilities regardless of enabled count`

---

### Task 8: Update banner templates for suggested caps

**Files:**
- Modify: `internal/ui/templates/compact.tmpl`
- Modify: `internal/ui/templates/boxed.tmpl`
- Modify: `internal/ui/templates/clean.tmpl`

- [ ] **Step 1: Update compact.tmpl**

After the `.DisabledCaps` range block (line 38), add:

```
{{- range .SuggestedCaps}}
     {{dim "○"}} {{printf "%-10s" .Name}} {{truncate .Paths 3}}  {{dim "(detected — enable with --with)"}}
{{- end}}
```

- [ ] **Step 2: Update boxed.tmpl and clean.tmpl**

Add the same block to the equivalent position in each template. Read
each template first to find the correct insertion point (after the
disabled caps range block).

- [ ] **Step 3: Run full test suite**

Run: `go test ./... 2>&1 | tail -20`

Expected: all PASS

- [ ] **Step 4: Commit**

```
git add internal/ui/templates/compact.tmpl \
       internal/ui/templates/boxed.tmpl \
       internal/ui/templates/clean.tmpl
```

Message: `feat: show detected-but-not-enabled capabilities in banner`

---

### Task 9: Add launcher suggestion tests

**Files:**
- Modify: `internal/launcher/launcher_test.go`

- [ ] **Step 1: Write test for suggestions with partial caps**

Read `internal/launcher/launcher_test.go` first to understand the
existing test patterns and helpers. Then add a test that:

1. Creates a project with `go.mod` and `.github/workflows/` and
   `.git/config` with remotes
2. Calls the detection logic with `github` already enabled
3. Verifies `go` and `git-remote` appear in `SuggestedCaps`
4. Verifies `github` does NOT appear in `SuggestedCaps`

The exact test structure depends on the existing test helpers in the
file. If the launcher tests use integration-style setup, follow that
pattern. If they use unit-style mocking, follow that.

- [ ] **Step 2: Write test for no suggestions when all detected are enabled**

Similar setup but with all detected caps enabled. Verify `SuggestedCaps`
is empty.

- [ ] **Step 3: Run tests**

Run: `go test ./internal/launcher/ -v`

Expected: all PASS

- [ ] **Step 4: Commit**

```
git add internal/launcher/launcher_test.go
```

Message: `test: add launcher capability suggestion tests`

---

### Task 10: Full verification

**Files:** None (verification only)

- [ ] **Step 1: Run full test suite**

Run: `go test ./...`

Expected: all PASS

- [ ] **Step 2: Run go vet**

Run: `go vet ./...`

Expected: no issues

- [ ] **Step 3: Verify banner output**

If aide binary is buildable, test with:

```bash
go run ./cmd/aide sandbox show 2>&1 | head -20
```

Verify git-integration guard appears in the output.
