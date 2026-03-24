# macOS Sandbox Fixes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix 7 macOS sandbox bugs: nix guard gaps, writable_extra silently dropped, allow_subprocess dead code, broken integration tests, missing parent-metadata helper, and no cross-guard conflict detection.

**Architecture:** Expand the seatbelt guard system by wiring config fields through Policy -> Context -> guards, adding a path helper to prevent recurring metadata bugs, and adding contract tests that verify config fields produce profile rules.

**Tech Stack:** Go, macOS Seatbelt (sandbox-exec), `pkg/seatbelt`, `internal/sandbox`

**Spec:** `docs/superpowers/specs/2026-03-24-nix-toolchain-guard-expansion-design.md`

---

### Task 1: Parent-Metadata Helper

Do this first — Task 2 (nix guard) will use it.

**Files:**
- Modify: `pkg/seatbelt/path.go`
- Test: `pkg/seatbelt/path_test.go`

- [ ] **Step 1: Write the failing test**

In `pkg/seatbelt/path_test.go`, add:

```go
func TestSubpathWithParentMetadata(t *testing.T) {
	rules := seatbelt.SubpathWithParentMetadata("/nix/store")
	if len(rules) != 2 {
		t.Fatalf("expected 2 rules, got %d", len(rules))
	}
	output := rules[0].String() + "\n" + rules[1].String()
	if !strings.Contains(output, `(subpath "/nix/store")`) {
		t.Error("expected subpath rule for /nix/store")
	}
	if !strings.Contains(output, `(literal "/nix")`) {
		t.Error("expected metadata rule for parent /nix")
	}
	if !strings.Contains(output, "file-read-metadata") {
		t.Error("expected file-read-metadata in parent rule")
	}
}

func TestSubpathWithParentMetadata_RootChild(t *testing.T) {
	rules := seatbelt.SubpathWithParentMetadata("/opt")
	if len(rules) != 2 {
		t.Fatalf("expected 2 rules, got %d", len(rules))
	}
	output := rules[1].String()
	if !strings.Contains(output, `(literal "/")`) {
		t.Error("expected metadata rule for / when parent is root")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/seatbelt/ -run TestSubpathWithParentMetadata -v`
Expected: FAIL — `SubpathWithParentMetadata` not defined

- [ ] **Step 3: Implement the helper**

In `pkg/seatbelt/path.go`, add after `HomePrefix`:

```go
// SubpathWithParentMetadata returns two rules: (allow file-read* (subpath ...))
// for the given path, and (allow file-read-metadata (literal ...)) for its
// parent directory. Seatbelt subpath rules don't grant lstat on the parent
// itself, which breaks filepath.EvalSymlinks traversal.
func SubpathWithParentMetadata(path string) []Rule {
	parent := filepath.Dir(path)
	return []Rule{
		AllowRule(fmt.Sprintf(`(allow file-read* (subpath "%s"))`, path)),
		AllowRule(fmt.Sprintf(`(allow file-read-metadata (literal "%s"))`, parent)),
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/seatbelt/ -run TestSubpathWithParentMetadata -v`
Expected: PASS

- [ ] **Step 5: Commit**

Stage: `git add pkg/seatbelt/path.go pkg/seatbelt/path_test.go`
Run: `/commit --style classic add SubpathWithParentMetadata helper for seatbelt path traversal`

---

### Task 2: Expand Nix Toolchain Guard

**Files:**
- Modify: `pkg/seatbelt/guards/guard_nix_toolchain.go`
- Modify: `pkg/seatbelt/guards/toolchain_test.go`

- [ ] **Step 1: Write the failing tests**

In `pkg/seatbelt/guards/toolchain_test.go`, replace `TestGuard_NixToolchain_Paths` (lines 62-81) with:

```go
func TestGuard_NixToolchain_DetectionGate(t *testing.T) {
	// When /nix/store doesn't exist, guard should skip.
	// We can't remove /nix/store in tests, so just verify the
	// skip message format by checking the guard's behavior.
	// On systems without nix, Rules() returns Skipped.
	// On systems with nix, Rules() returns rules.
	g := guards.NixToolchainGuard()
	ctx := &seatbelt.Context{HomeDir: "/Users/testuser"}
	result := g.Rules(ctx)

	if dirExists("/nix/store") {
		if len(result.Skipped) > 0 {
			t.Error("nix is installed but guard returned Skipped")
		}
		if len(result.Rules) == 0 {
			t.Error("nix is installed but guard returned no rules")
		}
	} else {
		if len(result.Rules) > 0 {
			t.Error("nix is not installed but guard returned rules")
		}
		if len(result.Skipped) == 0 {
			t.Error("nix is not installed but guard returned no Skipped messages")
		}
	}
}

func TestGuard_NixToolchain_Paths(t *testing.T) {
	if !dirExists("/nix/store") {
		t.Skip("nix not installed")
	}
	g := guards.NixToolchainGuard()
	ctx := &seatbelt.Context{HomeDir: "/Users/testuser"}
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	// Existing paths
	paths := []string{
		`"/nix/store"`,
		`"/nix/var"`,
		`"/run/current-system"`,
		`(subpath "/Users/testuser/.nix-profile")`,
		`(subpath "/Users/testuser/.local/state/nix")`,
		`(subpath "/Users/testuser/.cache/nix")`,
	}
	for _, p := range paths {
		if !strings.Contains(output, p) {
			t.Errorf("expected output to contain %q", p)
		}
	}

	// New: firmlink resolution
	if !strings.Contains(output, `"/private/var/run/current-system"`) {
		t.Error("expected /private/var/run/current-system subpath")
	}

	// New: parent metadata
	if !strings.Contains(output, `file-read-metadata`) {
		t.Error("expected file-read-metadata rules")
	}
	if !strings.Contains(output, `(literal "/nix")`) {
		t.Error("expected metadata for /nix parent")
	}
	if !strings.Contains(output, `(literal "/run")`) {
		t.Error("expected metadata for /run parent")
	}

	// New: daemon socket
	if !strings.Contains(output, `network-outbound`) {
		t.Error("expected network-outbound rule for daemon socket")
	}
	if !strings.Contains(output, `unix-socket`) {
		t.Error("expected unix-socket in daemon socket rule")
	}
	if !strings.Contains(output, `/nix/var/nix/daemon-socket/socket`) {
		t.Error("expected daemon socket path")
	}

	// New: user paths
	if !strings.Contains(output, `"/Users/testuser/.nix-defexpr"`) {
		t.Error("expected .nix-defexpr path")
	}
	if !strings.Contains(output, `"/Users/testuser/.config/nix"`) {
		t.Error("expected .config/nix path")
	}
}
```

**Note:** Use `guards.TestDirExists` (exposed via `export_test.go`) instead of duplicating `dirExists`. Replace all `dirExists(` calls in the test with `guards.TestDirExists(`.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./pkg/seatbelt/guards/ -run TestGuard_NixToolchain -v`
Expected: FAIL — missing new paths in output

- [ ] **Step 3: Implement the expanded guard**

Replace the entire `Rules` method in `pkg/seatbelt/guards/guard_nix_toolchain.go`.

**Important:** Use `seatbelt.SubpathWithParentMetadata` from Task 1 for `/nix/store` and `/nix/var`. For `/run/current-system` use raw rules since we need special handling (firmlink + resolved path).

```go
func (g *nixToolchainGuard) Rules(ctx *seatbelt.Context) seatbelt.GuardResult {
	if !dirExists("/nix/store") {
		return seatbelt.GuardResult{
			Skipped: []string{"/nix/store not found — nix not installed"},
		}
	}

	home := ctx.HomeDir

	rules := []seatbelt.Rule{
		// Parent directory metadata for symlink traversal (/run firmlink)
		seatbelt.SectionAllow("Nix parent directory metadata"),
		seatbelt.AllowRule(`(allow file-read-metadata
    (literal "/run")
)`),
	}

	// Nix store and var with parent metadata (uses helper to ensure /nix lstat works)
	rules = append(rules, seatbelt.SectionAllow("Nix store and system paths"))
	rules = append(rules, seatbelt.SubpathWithParentMetadata("/nix/store")...)
	rules = append(rules, seatbelt.AllowRule(`(allow file-read*
    (subpath "/nix/var")
    (subpath "/run/current-system")
    (subpath "/private/var/run/current-system")
)`))

	rules = append(rules,

		// Nix daemon socket
		seatbelt.SectionAllow("Nix daemon socket"),
		seatbelt.AllowRule(`(allow network-outbound
    (remote unix-socket (path-literal "/nix/var/nix/daemon-socket/socket"))
)`),

		// Nix user paths (read-write)
		seatbelt.SectionAllow("Nix user paths"),
		seatbelt.AllowRule(`(allow file-read* file-write*
    ` + seatbelt.HomeSubpath(home, ".nix-profile") + `
    ` + seatbelt.HomeSubpath(home, ".local/state/nix") + `
    ` + seatbelt.HomeSubpath(home, ".cache/nix") + `
)`),

		// Nix channel definitions and user config (read-only)
		seatbelt.SectionAllow("Nix channel definitions and user config"),
		seatbelt.AllowRule(`(allow file-read*
    ` + seatbelt.HomeSubpath(home, ".nix-defexpr") + `
    ` + seatbelt.HomeSubpath(home, ".config/nix") + `
)`),
	)

	return seatbelt.GuardResult{Rules: rules}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./pkg/seatbelt/guards/ -run TestGuard_NixToolchain -v`
Expected: PASS

- [ ] **Step 5: Run full test suite**

Run: `go test ./pkg/seatbelt/guards/ -v`
Expected: All pass

- [ ] **Step 6: Commit**

Stage: `git add pkg/seatbelt/guards/guard_nix_toolchain.go pkg/seatbelt/guards/toolchain_test.go`
Run: `/commit --style classic expand nix-toolchain guard with parent metadata, firmlink resolution, daemon socket, and user paths`

---

### Task 3: Wire `ExtraWritable` / `ExtraReadable` Through the Pipeline

**Files:**
- Modify: `internal/sandbox/sandbox.go:37-74` (Policy struct)
- Modify: `pkg/seatbelt/module.go:44-57` (Context struct)
- Modify: `internal/sandbox/policy.go:32-96` (PolicyFromConfig)
- Modify: `internal/sandbox/darwin.go:83-93` (generateSeatbeltProfile context)
- Modify: `pkg/seatbelt/guards/guard_filesystem.go:28-48` (Rules method)

- [ ] **Step 1: Write the failing test — filesystem guard consumes ExtraWritable**

In `pkg/seatbelt/guards/filesystem_test.go`, add:

```go
func TestFilesystemGuard_ExtraWritable(t *testing.T) {
	g := guards.FilesystemGuard()
	ctx := &seatbelt.Context{
		HomeDir:       "/Users/testuser",
		ProjectRoot:   "/project",
		ExtraWritable: []string{"/custom/writable"},
	}
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	if !strings.Contains(output, `"/custom/writable"`) {
		t.Error("expected /custom/writable in filesystem guard output")
	}
	if !strings.Contains(output, "file-write*") {
		t.Error("expected file-write* rule for writable path")
	}
}

func TestFilesystemGuard_ExtraReadable(t *testing.T) {
	g := guards.FilesystemGuard()
	ctx := &seatbelt.Context{
		HomeDir:       "/Users/testuser",
		ProjectRoot:   "/project",
		ExtraReadable: []string{"/custom/readable"},
	}
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	if !strings.Contains(output, `"/custom/readable"`) {
		t.Error("expected /custom/readable in filesystem guard output")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/seatbelt/guards/ -run TestFilesystemGuard_Extra -v`
Expected: FAIL — `ExtraWritable` not a field on Context

- [ ] **Step 3: Add fields to Context**

In `pkg/seatbelt/module.go`, add after `ExtraDenied` (line 56):

```go
	ExtraWritable []string // consumed by filesystem guard (user-configured writable paths)
	ExtraReadable []string // consumed by filesystem guard (user-configured readable paths)
```

- [ ] **Step 4: Add fields to Policy**

In `internal/sandbox/sandbox.go`, add after `ExtraDenied` (line 66):

```go
	// ExtraWritable holds user-configured extra writable paths from config.
	ExtraWritable []string

	// ExtraReadable holds user-configured extra readable paths from config.
	ExtraReadable []string
```

- [ ] **Step 5: Consume in filesystem guard**

In `pkg/seatbelt/guards/guard_filesystem.go`, add before the return statement (before line 47):

```go
	writable = append(writable, ctx.ExtraWritable...)
	readable = append(readable, ctx.ExtraReadable...)
```

- [ ] **Step 6: Run guard test to verify it passes**

Run: `go test ./pkg/seatbelt/guards/ -run TestFilesystemGuard_Extra -v`
Expected: PASS

- [ ] **Step 7: Wire through PolicyFromConfig**

In `internal/sandbox/policy.go`, add after the `ExtraDenied` block (after line 76), before the `Network` block:

```go
	// --- ExtraWritable from writable/writable_extra ---
	if len(cfg.Writable) > 0 {
		w, err := ResolvePaths(cfg.Writable, templateVars)
		if err != nil {
			return nil, nil, err
		}
		policy.ExtraWritable = validateAndFilterPaths(w, &warnings)
	} else if len(cfg.WritableExtra) > 0 {
		extra, err := ResolvePaths(cfg.WritableExtra, templateVars)
		if err != nil {
			return nil, nil, err
		}
		policy.ExtraWritable = validateAndFilterPaths(extra, &warnings)
	}

	// --- ExtraReadable from readable/readable_extra ---
	if len(cfg.Readable) > 0 {
		r, err := ResolvePaths(cfg.Readable, templateVars)
		if err != nil {
			return nil, nil, err
		}
		policy.ExtraReadable = validateAndFilterPaths(r, &warnings)
	} else if len(cfg.ReadableExtra) > 0 {
		extra, err := ResolvePaths(cfg.ReadableExtra, templateVars)
		if err != nil {
			return nil, nil, err
		}
		policy.ExtraReadable = validateAndFilterPaths(extra, &warnings)
	}
```

- [ ] **Step 8: Pass through in generateSeatbeltProfile**

In `internal/sandbox/darwin.go`, add inside the `WithContext` callback (after line 93):

```go
			c.ExtraWritable = policy.ExtraWritable
			c.ExtraReadable = policy.ExtraReadable
```

- [ ] **Step 9: Update EvaluateGuards to pass new fields**

In `internal/sandbox/sandbox.go`, inside `EvaluateGuards` (lines 155-166), add the new fields to the `seatbelt.Context` construction:

```go
	ctx := &seatbelt.Context{
		HomeDir:         homeDir,
		ProjectRoot:     policy.ProjectRoot,
		TempDir:         policy.TempDir,
		RuntimeDir:      policy.RuntimeDir,
		Env:             policy.Env,
		GOOS:            runtime.GOOS,
		Network:         string(policy.Network),
		AllowPorts:      policy.AllowPorts,
		DenyPorts:       policy.DenyPorts,
		ExtraDenied:     policy.ExtraDenied,
		ExtraWritable:   policy.ExtraWritable,
		ExtraReadable:   policy.ExtraReadable,
		AllowSubprocess: policy.AllowSubprocess,
	}
```

This ensures banner diagnostics show correct guard results.

- [ ] **Step 10: Run full test suite**

Run: `go test ./internal/sandbox/ ./pkg/seatbelt/... -v`
Expected: All pass

- [ ] **Step 11: Commit**

Stage: `git add internal/sandbox/sandbox.go internal/sandbox/policy.go internal/sandbox/darwin.go pkg/seatbelt/module.go pkg/seatbelt/guards/guard_filesystem.go pkg/seatbelt/guards/filesystem_test.go`
Run: `/commit --style classic wire writable_extra and readable_extra config fields through Policy, Context, and filesystem guard to seatbelt profile`

---

### Task 4: Wire `AllowSubprocess` to System Runtime Guard

**Files:**
- Modify: `pkg/seatbelt/module.go:44-57` (Context struct)
- Modify: `internal/sandbox/darwin.go:83-93` (context passthrough)
- Modify: `pkg/seatbelt/guards/guard_system_runtime.go:109-117` (process rules)
- Test: `pkg/seatbelt/guards/system_runtime_test.go` (or existing test file)

- [ ] **Step 1: Write the failing test**

In the appropriate test file for system-runtime guard, add:

```go
func TestSystemRuntime_AllowSubprocess_True(t *testing.T) {
	g := guards.SystemRuntimeGuard()
	ctx := &seatbelt.Context{
		HomeDir:         "/Users/testuser",
		AllowSubprocess: true,
	}
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	if !strings.Contains(output, "(allow process-exec)") {
		t.Error("expected process-exec when AllowSubprocess=true")
	}
	if !strings.Contains(output, "(allow process-fork)") {
		t.Error("expected process-fork when AllowSubprocess=true")
	}
}

func TestSystemRuntime_AllowSubprocess_False(t *testing.T) {
	g := guards.SystemRuntimeGuard()
	ctx := &seatbelt.Context{
		HomeDir:         "/Users/testuser",
		AllowSubprocess: false,
	}
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	if !strings.Contains(output, "(allow process-exec)") {
		t.Error("expected process-exec even when AllowSubprocess=false")
	}
	if !strings.Contains(output, "(deny process-fork)") {
		t.Error("expected deny process-fork when AllowSubprocess=false")
	}
	if strings.Contains(output, "(allow process-fork)") {
		t.Error("should NOT have allow process-fork when AllowSubprocess=false")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/seatbelt/guards/ -run TestSystemRuntime_AllowSubprocess -v`
Expected: FAIL — `AllowSubprocess` not on Context, or test for `false` fails

- [ ] **Step 3: Add `AllowSubprocess` to Context**

In `pkg/seatbelt/module.go`, add after `ExtraReadable`:

```go
	AllowSubprocess bool // consumed by system-runtime guard
```

- [ ] **Step 4: Make process rules conditional in system-runtime guard**

In `pkg/seatbelt/guards/guard_system_runtime.go`, the current `Rules` method returns a struct literal with all rules inline. Refactor it to:

1. Change `return seatbelt.GuardResult{Rules: []seatbelt.Rule{` to `rules := []seatbelt.Rule{`
2. Change the closing `}}` to `}`
3. Remove `(allow process-fork)` from the literal (line 112)
4. Add the conditional fork logic after the slice, before the return:

```go
	// Conditional process-fork based on AllowSubprocess
	if ctx.AllowSubprocess {
		rules = append(rules, seatbelt.AllowRule("(allow process-fork)"))
	} else {
		rules = append(rules, seatbelt.DenyRule("(deny process-fork)"))
	}

	return seatbelt.GuardResult{Rules: rules}
```

- [ ] **Step 4b: Update existing test helpers**

Check `pkg/seatbelt/guards/system_test.go` for a `systemRuntimeOutput()` helper or similar. Any test that creates a `seatbelt.Context{}` without `AllowSubprocess: true` will now get `(deny process-fork)` instead of `(allow process-fork)`. Update those contexts to set `AllowSubprocess: true` to preserve existing behavior.

Also check `internal/sandbox/darwin_test.go` for any tests that render full profiles — those will also need `AllowSubprocess: true` on their Policy (already set by `DefaultPolicy`).

- [ ] **Step 5: Pass through in generateSeatbeltProfile**

In `internal/sandbox/darwin.go`, add inside `WithContext` callback:

```go
			c.AllowSubprocess = policy.AllowSubprocess
```

- [ ] **Step 6: Run tests**

Run: `go test ./pkg/seatbelt/guards/ -run TestSystemRuntime -v`
Expected: PASS

- [ ] **Step 7: Run full test suite to check for regressions**

Run: `go test ./pkg/seatbelt/... ./internal/sandbox/ -v`
Expected: All pass. Existing tests that check for `(allow process-fork)` will need their Context to set `AllowSubprocess: true`, or `DefaultPolicy` needs to default to `true`. Check `DefaultPolicy` in `sandbox.go:97-108` — it sets `AllowSubprocess: true`.

- [ ] **Step 8: Commit**

Stage: `git add pkg/seatbelt/module.go pkg/seatbelt/guards/guard_system_runtime.go internal/sandbox/darwin.go` (and any modified test files)
Run: `/commit --style classic wire AllowSubprocess through Context to system-runtime guard with conditional process-fork`

---

### Task 5: Fix Broken Integration Tests

**Files:**
- Modify: `internal/sandbox/integration_test.go`

- [ ] **Step 1: Read the current broken tests**

Read `internal/sandbox/integration_test.go` (already read — lines 31-171). These tests use `Policy.Denied`, `Policy.Writable`, `Policy.Readable` which don't exist.

- [ ] **Step 2: Rewrite all four tests**

Replace the entire file content (after the build tag and imports) with tests using the guard-based `Policy`:

```go
//go:build darwin && integration

package sandbox

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func realPath(t *testing.T, p string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatalf("EvalSymlinks(%q): %v", p, err)
	}
	return resolved
}

func TestSandbox_DeniedPathBlocked(t *testing.T) {
	runtimeDir := realPath(t, t.TempDir())
	deniedDir := realPath(t, t.TempDir())

	secretFile := filepath.Join(deniedDir, "id_rsa")
	if err := os.WriteFile(secretFile, []byte("TOP SECRET KEY"), 0600); err != nil {
		t.Fatalf("failed to create secret file: %v", err)
	}

	policy := DefaultPolicy(deniedDir, runtimeDir, os.TempDir(), os.Environ())
	policy.ExtraDenied = []string{secretFile}

	cmd := exec.Command("/bin/cat", secretFile)
	cmd.Env = os.Environ()

	s := NewSandbox()
	if err := s.Apply(cmd, policy, runtimeDir); err != nil {
		t.Fatalf("Apply failed: %v", err)
	}

	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected cat to fail on denied path, but it succeeded with output: %s", output)
	}
	t.Logf("sandbox correctly blocked read of denied path; exit error: %v, output: %s", err, output)
}

func TestSandbox_AllowedPathReadable(t *testing.T) {
	runtimeDir := realPath(t, t.TempDir())
	projectDir := realPath(t, t.TempDir())

	readableFile := filepath.Join(projectDir, "hello.txt")
	content := "hello from sandbox test"
	if err := os.WriteFile(readableFile, []byte(content), 0644); err != nil {
		t.Fatalf("failed to create readable file: %v", err)
	}

	policy := DefaultPolicy(projectDir, runtimeDir, os.TempDir(), os.Environ())

	cmd := exec.Command("/bin/cat", readableFile)
	cmd.Env = os.Environ()

	s := NewSandbox()
	if err := s.Apply(cmd, policy, runtimeDir); err != nil {
		t.Fatalf("Apply failed: %v", err)
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected cat to succeed on project path, but it failed: %v, output: %s", err, output)
	}
	if !strings.Contains(string(output), content) {
		t.Errorf("expected output to contain %q, got %q", content, string(output))
	}
}

func TestSandbox_WritablePath(t *testing.T) {
	runtimeDir := realPath(t, t.TempDir())
	projectDir := realPath(t, t.TempDir())

	policy := DefaultPolicy(projectDir, runtimeDir, os.TempDir(), os.Environ())

	targetFile := filepath.Join(projectDir, "test.txt")
	cmd := exec.Command("/usr/bin/touch", targetFile)
	cmd.Env = os.Environ()

	s := NewSandbox()
	if err := s.Apply(cmd, policy, runtimeDir); err != nil {
		t.Fatalf("Apply failed: %v", err)
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected touch to succeed on project path, but it failed: %v, output: %s", err, output)
	}
	if _, err := os.Stat(targetFile); os.IsNotExist(err) {
		t.Error("expected file to exist after touch in project dir, but it does not")
	}
}

func TestSandbox_ExtraWritablePath(t *testing.T) {
	runtimeDir := realPath(t, t.TempDir())
	projectDir := realPath(t, t.TempDir())
	extraDir := realPath(t, t.TempDir())

	policy := DefaultPolicy(projectDir, runtimeDir, os.TempDir(), os.Environ())
	policy.ExtraWritable = []string{extraDir}

	targetFile := filepath.Join(extraDir, "extra.txt")
	cmd := exec.Command("/usr/bin/touch", targetFile)
	cmd.Env = os.Environ()

	s := NewSandbox()
	if err := s.Apply(cmd, policy, runtimeDir); err != nil {
		t.Fatalf("Apply failed: %v", err)
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected touch to succeed on extra writable path, but it failed: %v, output: %s", err, output)
	}
	if _, err := os.Stat(targetFile); os.IsNotExist(err) {
		t.Error("expected file to exist after touch in extra writable dir")
	}
}

func TestSandbox_WriteToReadOnlyBlocked(t *testing.T) {
	runtimeDir := realPath(t, t.TempDir())
	projectDir := realPath(t, t.TempDir())
	readOnlyDir := realPath(t, t.TempDir())

	policy := DefaultPolicy(projectDir, runtimeDir, os.TempDir(), os.Environ())
	policy.ExtraReadable = []string{readOnlyDir}

	targetFile := filepath.Join(readOnlyDir, "test.txt")
	cmd := exec.Command("/usr/bin/touch", targetFile)
	cmd.Env = os.Environ()

	s := NewSandbox()
	if err := s.Apply(cmd, policy, runtimeDir); err != nil {
		t.Fatalf("Apply failed: %v", err)
	}

	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected touch to fail on read-only path, but it succeeded with output: %s", output)
	}
	t.Logf("sandbox correctly blocked write to read-only path; exit error: %v, output: %s", err, output)
}
```

- [ ] **Step 3: Verify tests compile**

Run: `go build -tags "darwin,integration" ./internal/sandbox/`
Expected: No errors

- [ ] **Step 4: Commit**

Stage: `git add internal/sandbox/integration_test.go`
Run: `/commit --style classic rewrite broken darwin integration tests to use guard-based Policy with ExtraDenied, ExtraWritable, ExtraReadable`

---

### Task 6: Config-to-Profile Contract Tests

**Files:**
- Create: `internal/sandbox/policy_contract_test.go`

- [ ] **Step 1: Write the contract tests**

Create `internal/sandbox/policy_contract_test.go`:

```go
//go:build darwin

package sandbox

import (
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/config"
)

// Contract tests verify that config fields actually produce rules in the
// rendered seatbelt profile. Catches "parsed but dropped" bugs.

func renderProfileFromConfig(t *testing.T, cfg *config.SandboxPolicy) string {
	t.Helper()
	policy, _, err := PolicyFromConfig(cfg, "/project", "/runtime", "/Users/testuser", "/tmp")
	if err != nil {
		t.Fatalf("PolicyFromConfig failed: %v", err)
	}
	sb := &darwinSandbox{}
	profile, err := sb.GenerateProfile(*policy)
	if err != nil {
		t.Fatalf("GenerateProfile failed: %v", err)
	}
	return profile
}

func TestContract_WritableExtraProducesRule(t *testing.T) {
	profile := renderProfileFromConfig(t, &config.SandboxPolicy{
		WritableExtra: []string{"/custom/writable"},
	})
	if !strings.Contains(profile, "/custom/writable") {
		t.Error("writable_extra path not found in rendered profile")
	}
	if !strings.Contains(profile, "file-write*") {
		t.Error("expected file-write* rule for writable_extra")
	}
}

func TestContract_ReadableExtraProducesRule(t *testing.T) {
	profile := renderProfileFromConfig(t, &config.SandboxPolicy{
		ReadableExtra: []string{"/custom/readable"},
	})
	if !strings.Contains(profile, "/custom/readable") {
		t.Error("readable_extra path not found in rendered profile")
	}
}

func TestContract_DeniedExtraProducesRule(t *testing.T) {
	profile := renderProfileFromConfig(t, &config.SandboxPolicy{
		DeniedExtra: []string{"/tmp/secret"},
	})
	if !strings.Contains(profile, "/tmp/secret") {
		t.Error("denied_extra path not found in rendered profile")
	}
	if !strings.Contains(profile, "deny file-read-data") {
		t.Error("expected deny file-read-data for denied path")
	}
	if !strings.Contains(profile, "deny file-write*") {
		t.Error("expected deny file-write* for denied path")
	}
}

func TestContract_AllowSubprocessFalseProducesDenyFork(t *testing.T) {
	f := false
	profile := renderProfileFromConfig(t, &config.SandboxPolicy{
		AllowSubprocess: &f,
	})
	if !strings.Contains(profile, "(deny process-fork)") {
		t.Error("allow_subprocess: false should produce deny process-fork")
	}
	if strings.Contains(profile, "(allow process-fork)") {
		t.Error("allow_subprocess: false should NOT produce allow process-fork")
	}
}

func TestContract_AllowSubprocessTrueDefault(t *testing.T) {
	profile := renderProfileFromConfig(t, &config.SandboxPolicy{})
	if !strings.Contains(profile, "(allow process-fork)") {
		t.Error("default policy should have allow process-fork")
	}
}

func TestContract_NetworkModeApplied(t *testing.T) {
	profile := renderProfileFromConfig(t, &config.SandboxPolicy{
		Network: &config.NetworkPolicy{Mode: "none"},
	})
	if strings.Contains(profile, "(allow network-outbound)") {
		t.Error("network: none should NOT have allow network-outbound")
	}
}
```

- [ ] **Step 2: Run the contract tests**

Run: `go test ./internal/sandbox/ -run TestContract -v`
Expected: All PASS (since we've already wired the fields in Tasks 3-4)

- [ ] **Step 3: Commit**

Stage: `git add internal/sandbox/policy_contract_test.go`
Run: `/commit --style classic add config-to-profile contract tests verifying writable_extra, readable_extra, denied_extra, AllowSubprocess, and network mode produce expected seatbelt rules`

---

### Task 7: Cross-Guard Conflict Detection

**Files:**
- Modify: `internal/sandbox/sandbox.go:146-180` (EvaluateGuards)
- Test: `internal/sandbox/sandbox_test.go`

- [ ] **Step 1: Write the failing test**

In `internal/sandbox/sandbox_test.go`, add:

```go
func TestDetectGuardConflicts(t *testing.T) {
	results := []seatbelt.GuardResult{
		{
			Name: "node-toolchain",
			Rules: []seatbelt.Rule{
				seatbelt.AllowRule(`(allow file-read* (literal "/Users/test/.npmrc"))`),
			},
		},
		{
			Name: "npm",
			Rules: []seatbelt.Rule{
				seatbelt.DenyRule(`(deny file-read-data (literal "/Users/test/.npmrc"))`),
			},
		},
	}

	warnings := DetectGuardConflicts(results)
	if len(warnings) == 0 {
		t.Error("expected conflict warning for .npmrc")
	}

	found := false
	for _, w := range warnings {
		if strings.Contains(w, ".npmrc") && strings.Contains(w, "npm") && strings.Contains(w, "node-toolchain") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected warning mentioning .npmrc conflict between npm and node-toolchain, got: %v", warnings)
	}
}

func TestDetectGuardConflicts_NoConflict(t *testing.T) {
	results := []seatbelt.GuardResult{
		{
			Name: "filesystem",
			Rules: []seatbelt.Rule{
				seatbelt.AllowRule(`(allow file-read* (subpath "/project"))`),
			},
		},
	}

	warnings := DetectGuardConflicts(results)
	if len(warnings) != 0 {
		t.Errorf("expected no warnings, got: %v", warnings)
	}
}
```

Add `"strings"` to imports if not already present.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/sandbox/ -run TestDetectGuardConflicts -v`
Expected: FAIL — `DetectGuardConflicts` not defined

- [ ] **Step 3: Implement conflict detection**

In `internal/sandbox/sandbox.go`, add after `EvaluateGuards`:

```go
// DetectGuardConflicts scans guard results for cases where a deny rule
// from one guard covers a path that another guard explicitly allows.
// Returns human-readable warning strings for the banner.
func DetectGuardConflicts(results []seatbelt.GuardResult) []string {
	type pathEntry struct {
		guard string
		path  string
	}

	var denied []pathEntry
	var allowed []pathEntry

	pathRe := regexp.MustCompile(`"([^"]+)"`)

	for _, r := range results {
		for _, rule := range r.Rules {
			text := rule.String()
			matches := pathRe.FindAllStringSubmatch(text, -1)
			for _, m := range matches {
				p := m[1]
				if strings.Contains(text, "deny ") {
					denied = append(denied, pathEntry{guard: r.Name, path: p})
				} else if strings.Contains(text, "allow ") {
					allowed = append(allowed, pathEntry{guard: r.Name, path: p})
				}
			}
		}
	}

	var warnings []string
	for _, d := range denied {
		for _, a := range allowed {
			if d.guard != a.guard && d.path == a.path {
				warnings = append(warnings,
					fmt.Sprintf("guard %q denies %s which guard %q allows (deny wins in seatbelt)",
						d.guard, d.path, a.guard))
			}
		}
	}
	return warnings
}
```

Add `"regexp"` and `"fmt"` to imports.

**Known limitation:** This only catches exact path matches, not subpath-covers-literal overlaps (e.g., `deny subpath "/tmp"` vs `allow literal "/tmp/foo"`). Document this in the code comment. Exact-match detection covers the most common real-world case (npm `.npmrc` conflict).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/sandbox/ -run TestDetectGuardConflicts -v`
Expected: PASS

- [ ] **Step 5: Commit**

Stage: `git add internal/sandbox/sandbox.go internal/sandbox/sandbox_test.go`
Run: `/commit --style classic add cross-guard conflict detection diagnostic for banner warnings when deny rules override allow rules from other guards`
```

---

### Task 8: Toolchain Integration Smoke Tests

**Files:**
- Create: `internal/sandbox/toolchain_integration_test.go`

- [ ] **Step 1: Write the integration tests**

Create `internal/sandbox/toolchain_integration_test.go`:

```go
//go:build darwin && integration

package sandbox

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func dirExistsInteg(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func TestNixGuard_StatNix(t *testing.T) {
	if !dirExistsInteg("/nix/store") {
		t.Skip("nix not installed")
	}

	runtimeDir := realPath(t, t.TempDir())
	projectDir := realPath(t, t.TempDir())
	policy := DefaultPolicy(projectDir, runtimeDir, os.TempDir(), os.Environ())

	cmd := exec.Command("/usr/bin/stat", "/nix")
	cmd.Env = os.Environ()

	s := NewSandbox()
	if err := s.Apply(cmd, policy, runtimeDir); err != nil {
		t.Fatalf("Apply failed: %v", err)
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("stat /nix failed inside sandbox: %v\noutput: %s", err, output)
	}
}

func TestNixGuard_StatRunCurrentSystem(t *testing.T) {
	if !dirExistsInteg("/nix/store") {
		t.Skip("nix not installed")
	}

	runtimeDir := realPath(t, t.TempDir())
	projectDir := realPath(t, t.TempDir())
	policy := DefaultPolicy(projectDir, runtimeDir, os.TempDir(), os.Environ())

	// Use /private/var/run/current-system since /run is a firmlink
	cmd := exec.Command("/usr/bin/stat", "/private/var/run/current-system")
	cmd.Env = os.Environ()

	s := NewSandbox()
	if err := s.Apply(cmd, policy, runtimeDir); err != nil {
		t.Fatalf("Apply failed: %v", err)
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("stat /private/var/run/current-system failed inside sandbox: %v\noutput: %s", err, output)
	}
}

func TestNixGuard_GoToolchain(t *testing.T) {
	if !dirExistsInteg("/nix/store") {
		t.Skip("nix not installed")
	}

	// Find go binary through nix profile
	goPath := filepath.Join(os.Getenv("HOME"), ".nix-profile", "bin", "go")
	if _, err := os.Stat(goPath); os.IsNotExist(err) {
		t.Skipf("nix go not found at %s", goPath)
	}

	runtimeDir := realPath(t, t.TempDir())
	projectDir := realPath(t, t.TempDir())
	policy := DefaultPolicy(projectDir, runtimeDir, os.TempDir(), os.Environ())

	cmd := exec.Command(goPath, "env", "GOROOT")
	cmd.Env = os.Environ()

	s := NewSandbox()
	if err := s.Apply(cmd, policy, runtimeDir); err != nil {
		t.Fatalf("Apply failed: %v", err)
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go env GOROOT failed inside sandbox: %v\noutput: %s", err, output)
	}

	if !strings.Contains(string(output), "/nix/store") {
		t.Errorf("expected GOROOT in /nix/store, got: %s", output)
	}
}
```

- [ ] **Step 2: Verify tests compile**

Run: `go build -tags "darwin,integration" ./internal/sandbox/`
Expected: No errors

- [ ] **Step 3: Commit**

Stage: `git add internal/sandbox/toolchain_integration_test.go`
Run: `/commit --style classic add nix toolchain integration smoke tests for stat /nix, /run/current-system, and go env GOROOT inside sandbox`

---

### Task 9: Final Verification

- [ ] **Step 1: Run full test suite**

Run: `go test ./...`
Expected: All pass

- [ ] **Step 2: Run vet and lint**

Run: `go vet ./...`
Expected: No issues

- [ ] **Step 3: Verify the nix guard fix manually (if inside aide sandbox)**

Run: `stat /nix` — should succeed after the fix is applied to a new aide session.

- [ ] **Step 4: Final commit if any fixups needed**

Review `git diff` for any uncommitted changes, commit if needed.
