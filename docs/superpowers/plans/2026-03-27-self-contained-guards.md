# Self-Contained Guards Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix "not logged in" bug by restoring read access in keychain and nix-toolchain guards that was incorrectly removed.

**Architecture:** Two "always" guards lost their `file-read*` rules when they were simplified to rely on the filesystem guard for reads. The filesystem guard was later narrowed (correctly), breaking the assumption. Fix: make each guard self-contained with minimal correct permissions.

**Tech Stack:** Go, macOS Seatbelt sandbox profiles

**Spec:** `docs/superpowers/specs/2026-03-27-self-contained-guards-design.md`

---

### Task 1: Fix keychain guard — restore read, remove write

**Files:**
- Modify: `pkg/seatbelt/guards/guard_keychain.go:22-32`
- Test: `pkg/seatbelt/guards/toolchain_test.go:146-174`

- [ ] **Step 1: Update the failing test first**

In `pkg/seatbelt/guards/toolchain_test.go`, update `TestGuard_Keychain_Rules` (line 146). Change the comment and add assertions that `file-read*` is present and `file-write*` is absent for keychain paths:

```go
func TestGuard_Keychain_Rules(t *testing.T) {
	g := guards.KeychainGuard()
	ctx := &seatbelt.Context{HomeDir: "/Users/testuser"}
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	// User keychain read paths
	if !strings.Contains(output, `(subpath "/Users/testuser/Library/Keychains")`) {
		t.Error("expected user Library/Keychains path")
	}
	if !strings.Contains(output, `(literal "/Users/testuser/Library/Preferences/com.apple.security.plist")`) {
		t.Error("expected security plist path")
	}

	// Should be read-only, not read-write
	if !strings.Contains(output, "file-read*") {
		t.Error("expected file-read* for keychain paths")
	}
	if strings.Contains(output, "file-write*") {
		t.Error("keychain paths should be read-only, not writable")
	}

	// System keychain reads are now covered by system-runtime broad reads
	if strings.Contains(output, `(literal "/Library/Keychains/System.keychain")`) {
		t.Error("system keychain read should be removed (covered by system-runtime)")
	}

	// Mach services and IPC should still be present
	machServices := []string{
		"com.apple.SecurityServer",
		"com.apple.secd",
		"com.apple.trustd",
		"com.apple.AppleDatabaseChanged",
	}
	for _, svc := range machServices {
		if !strings.Contains(output, svc) {
			t.Errorf("expected output to contain %q", svc)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `nix develop --command go test ./pkg/seatbelt/guards/ -run TestGuard_Keychain_Rules -v`
Expected: FAIL — `file-write*` is present, `file-read*` is absent

- [ ] **Step 3: Fix the keychain guard**

In `pkg/seatbelt/guards/guard_keychain.go`, replace lines 25-32. Add `"fmt"` to imports. Change `file-write*` to `file-read*` and remove the stale comment:

```go
func (g *keychainGuard) Rules(ctx *seatbelt.Context) seatbelt.GuardResult {
	home := ctx.HomeDir

	return seatbelt.GuardResult{Rules: []seatbelt.Rule{
		seatbelt.SectionAllow("User keychain (read-only)"),
		seatbelt.AllowRule(fmt.Sprintf(`(allow file-read*
    %s
    %s
)`, seatbelt.HomeSubpath(home, "Library/Keychains"),
			seatbelt.HomeLiteral(home, "Library/Preferences/com.apple.security.plist"))),

		// System keychain reads and metadata traversal are now covered
		// by the system-runtime guard's broad /Library and /private reads.

		// Security Mach services
		seatbelt.SectionAllow("Security Mach services"),
		seatbelt.AllowRule(`(allow mach-lookup
    (global-name "com.apple.SecurityServer")
    (global-name "com.apple.security.agent")
    (global-name "com.apple.securityd.xpc")
    (global-name "com.apple.security.authhost")
    (global-name "com.apple.secd")
    (global-name "com.apple.trustd")
)`),

		// Security IPC shared memory
		seatbelt.SectionAllow("Security IPC shared memory"),
		seatbelt.AllowRule(`(allow ipc-posix-shm-read-data ipc-posix-shm-write-create ipc-posix-shm-write-data
    (ipc-posix-name "com.apple.AppleDatabaseChanged")
)`),
	}}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `nix develop --command go test ./pkg/seatbelt/guards/ -run TestGuard_Keychain -v`
Expected: PASS

- [ ] **Step 5: Commit**

```
git add pkg/seatbelt/guards/guard_keychain.go pkg/seatbelt/guards/toolchain_test.go
git commit -m "fix: restore keychain read access, remove unnecessary write

Keychain guard had file-write* but no file-read* after ab339c5 removed
reads assuming filesystem guard covered them. The filesystem guard was
later narrowed (022173b), breaking the assumption. Agents need to read
keychain tokens to authenticate; they should not write to the keychain
(Security framework uses Mach services for that)."
```

---

### Task 2: Fix nix-toolchain guard — restore read+write, add back dropped paths

**Files:**
- Modify: `pkg/seatbelt/guards/guard_nix_toolchain.go:18-42`
- Test: `pkg/seatbelt/guards/toolchain_test.go:83-130`

- [ ] **Step 1: Update the failing test first**

In `pkg/seatbelt/guards/toolchain_test.go`, replace `TestGuard_NixToolchain_Paths` (line 83). Reverse the `file-read*` negative assertion to positive. Move `~/.nix-defexpr` and `~/.config/nix` from "should NOT contain" to positive assertions. Keep system-runtime paths (`/nix/store`, `/nix/var`, `/run/current-system`) as negative:

```go
func TestGuard_NixToolchain_Paths(t *testing.T) {
	if !guards.TestDirExists("/nix/store") {
		t.Skip("nix not installed")
	}
	g := guards.NixToolchainGuard()
	ctx := &seatbelt.Context{HomeDir: "/Users/testuser"}
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	// Daemon socket (still owned by nix-toolchain)
	if !strings.Contains(output, `network-outbound`) {
		t.Error("expected network-outbound rule for daemon socket")
	}
	if !strings.Contains(output, `/nix/var/nix/daemon-socket/socket`) {
		t.Error("expected daemon socket path")
	}

	// Nix user paths (read+write, self-contained)
	userPaths := []string{
		`(subpath "/Users/testuser/.nix-profile")`,
		`(subpath "/Users/testuser/.local/state/nix")`,
		`(subpath "/Users/testuser/.cache/nix")`,
	}
	for _, p := range userPaths {
		if !strings.Contains(output, p) {
			t.Errorf("expected user path %q", p)
		}
	}

	// Must have file-read* (self-contained, not relying on filesystem guard)
	if !strings.Contains(output, "file-read*") {
		t.Error("nix user paths must include file-read* (guard must be self-contained)")
	}

	// Nix channel definitions and user config (read-only)
	readPaths := []string{
		`(subpath "/Users/testuser/.nix-defexpr")`,
		`(subpath "/Users/testuser/.config/nix")`,
	}
	for _, p := range readPaths {
		if !strings.Contains(output, p) {
			t.Errorf("expected read path %q", p)
		}
	}

	// System paths should NOT be in nix-toolchain (owned by system-runtime)
	systemPaths := []string{
		`"/nix/store"`,
		`"/nix/var"`,
		`"/run/current-system"`,
	}
	for _, p := range systemPaths {
		if strings.Contains(output, p) {
			t.Errorf("should NOT contain system path %q (owned by system-runtime)", p)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `nix develop --command go test ./pkg/seatbelt/guards/ -run TestGuard_NixToolchain_Paths -v`
Expected: FAIL — `file-read*` absent, `~/.nix-defexpr` absent

- [ ] **Step 3: Fix the nix-toolchain guard**

In `pkg/seatbelt/guards/guard_nix_toolchain.go`, replace lines 27-41. Add `"fmt"` to imports. Change `file-write*` to `file-read* file-write*`, remove stale comment, add back `~/.nix-defexpr` and `~/.config/nix`:

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

		// Nix user paths (read+write, self-contained)
		seatbelt.SectionAllow("Nix user paths"),
		seatbelt.AllowRule(fmt.Sprintf(`(allow file-read* file-write*
    %s
    %s
    %s
)`, seatbelt.HomeSubpath(home, ".nix-profile"),
			seatbelt.HomeSubpath(home, ".local/state/nix"),
			seatbelt.HomeSubpath(home, ".cache/nix"))),

		// Nix channel definitions and user config (read-only)
		seatbelt.SectionAllow("Nix channel definitions and user config"),
		seatbelt.AllowRule(fmt.Sprintf(`(allow file-read*
    %s
    %s
)`, seatbelt.HomeSubpath(home, ".nix-defexpr"),
			seatbelt.HomeSubpath(home, ".config/nix"))),
	}}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `nix develop --command go test ./pkg/seatbelt/guards/ -run TestGuard_NixToolchain -v`
Expected: PASS

- [ ] **Step 5: Commit**

```
git add pkg/seatbelt/guards/guard_nix_toolchain.go pkg/seatbelt/guards/toolchain_test.go
git commit -m "fix: restore nix-toolchain read access and dropped paths

Nix guard had file-write* but no file-read* after ab339c5 removed reads
assuming filesystem guard covered them. Also restores ~/.nix-defexpr and
~/.config/nix read rules that were completely dropped. Guards must be
self-contained — no implicit cross-guard dependencies."
```

---

### Task 3: Run full test suite and verify no regressions

- [ ] **Step 1: Run all guard tests**

Run: `nix develop --command go test ./pkg/seatbelt/guards/ -v`
Expected: All tests PASS

- [ ] **Step 2: Run sandbox integration tests**

Run: `nix develop --command go test ./internal/sandbox/ -v`
Expected: All tests PASS

- [ ] **Step 3: Run full test suite**

Run: `nix develop --command go test ./... 2>&1 | tail -30`
Expected: No new failures
