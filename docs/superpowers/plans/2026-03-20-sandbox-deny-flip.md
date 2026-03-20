# Sandbox (allow default) + Deny-List Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix the macOS sandbox so it works with Claude's interactive TUI while still blocking secret reads and restricting writes, by switching from `(deny default)` to `(allow default)`.

**Architecture:** Replace the Seatbelt profile from `(deny default)` + enumerate-every-allow to `(allow default)` + `(deny file-write* (require-not ...))` + `(deny file-read-data ...)` for secrets. Simplify `DefaultPolicy` in `sandbox.go` — delete `extraReadablePaths()`.

**Tech Stack:** Go, macOS Seatbelt (sandbox-exec)

**Spec:** `docs/superpowers/specs/2026-03-20-sandbox-default-policy-deny-flip.md`
**Findings:** `docs/sandbox-findings.md`

---

### Task 1: Rewrite Seatbelt profile to (allow default) + deny-list

The current profile uses `(deny default)` which breaks Claude's interactive TUI. Switch to `(allow default)` with targeted denies for writes and secret reads.

**Files:**
- Modify: `internal/sandbox/darwin.go:60-155` (generateSeatbeltProfile)
- Test: `internal/sandbox/darwin_test.go`

- [ ] **Step 1: Write failing tests for the new profile structure**

In `internal/sandbox/darwin_test.go`, update existing tests:

**A.** Replace `TestGenerateSeatbeltProfile_DenyDefault` (line 13) — should now assert `(allow default)`:

```go
func TestGenerateSeatbeltProfile_AllowDefault(t *testing.T) {
	policy := Policy{
		Network:         NetworkNone,
		AllowSubprocess: false,
	}

	profile, err := generateSeatbeltProfile(policy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(profile, "(allow default)") {
		t.Error("profile should start with (allow default)")
	}
	if strings.Contains(profile, "(deny default)") {
		t.Error("profile should NOT contain (deny default)")
	}
}
```

**B.** Replace `TestGenerateSeatbeltProfile_ReadGlobal` (line 54) — no longer needed, replace with write restriction test:

```go
func TestGenerateSeatbeltProfile_WriteRestriction(t *testing.T) {
	dir := t.TempDir()
	policy := Policy{
		Writable:        []string{dir},
		Network:         NetworkNone,
		AllowSubprocess: false,
	}

	profile, err := generateSeatbeltProfile(policy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Write access should be denied by default, allowed only for listed paths
	if !strings.Contains(profile, "(deny file-write*") {
		t.Error("profile should contain (deny file-write* to restrict writes")
	}
	if !strings.Contains(profile, "(require-not") {
		t.Error("profile should use (require-not for write exceptions")
	}
	if !strings.Contains(profile, dir) {
		t.Errorf("profile should list writable path %q as exception", dir)
	}
}
```

**C.** Replace `TestGenerateSeatbeltProfile_DeniedBeforeAllows` (line 92) — deny should use `file-read-data`:

```go
func TestGenerateSeatbeltProfile_DeniedPaths_UseFileReadData(t *testing.T) {
	dir := t.TempDir()
	denied := filepath.Join(dir, "denied")
	os.MkdirAll(denied, 0755)

	policy := Policy{
		Denied:          []string{denied},
		Network:         NetworkNone,
		AllowSubprocess: false,
	}

	profile, err := generateSeatbeltProfile(policy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Deny must use file-read-data (not file-read*) to override (allow default)
	if !strings.Contains(profile, "(deny file-read-data") {
		t.Error("denied paths should use (deny file-read-data, not (deny file-read*")
	}
	// file-write deny for defense-in-depth
	if !strings.Contains(profile, "(deny file-write*") {
		t.Error("denied paths should include (deny file-write* for defense-in-depth")
	}
}
```

**D.** Update `TestGenerateSeatbeltProfile_DeniedPaths` (line 71) — change assertion:

```go
	if !strings.Contains(profile, "(deny file-read-data") {
		t.Error("profile should contain (deny file-read-data for denied paths")
	}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/sandbox/ -run 'TestGenerateSeatbeltProfile_(AllowDefault|WriteRestriction|DeniedPaths)' -v`
Expected: FAIL — current code uses `(deny default)`, `file-read*`, and has no `require-not`.

- [ ] **Step 3: Rewrite generateSeatbeltProfile**

Replace the entire `generateSeatbeltProfile` function in `internal/sandbox/darwin.go` with:

```go
func generateSeatbeltProfile(policy Policy) (string, error) {
	var b strings.Builder

	// Allow everything by default. We restrict with targeted denies below.
	// (deny default) breaks Claude's interactive TUI because macOS Seatbelt
	// operations for terminal rendering are undocumented and unmaintainable.
	// See docs/sandbox-findings.md for the full investigation.
	b.WriteString("(version 1)\n")
	b.WriteString("(allow default)\n")

	// --- Write restriction ---
	// Deny all writes except to approved paths.
	// This is the primary security boundary on macOS.
	if len(policy.Writable) > 0 {
		b.WriteString("\n;; --- Write restriction (deny all except approved paths) ---\n")
		b.WriteString("(deny file-write*\n")
		b.WriteString("    (require-not\n")
		b.WriteString("        (require-any\n")
		for _, p := range policy.Writable {
			expr := seatbeltPath(p)
			b.WriteString(fmt.Sprintf("            %s\n", expr))
		}
		// Always allow writing to device nodes for terminal I/O
		b.WriteString("            (literal \"/dev/null\")\n")
		b.WriteString("            (literal \"/dev/tty\")\n")
		b.WriteString("            (literal \"/dev/dtracehelper\")\n")
		b.WriteString("            (regex #\"^/dev/ttys[0-9]+$\")\n")
		b.WriteString("            (regex #\"^/dev/pty.+$\")\n")
		b.WriteString("        )\n")
		b.WriteString("    )\n")
		b.WriteString(")\n")
	}

	// --- Network restriction ---
	switch policy.Network {
	case NetworkNone:
		b.WriteString("\n;; --- Network: deny all ---\n")
		b.WriteString("(deny network*)\n")
	case NetworkOutbound:
		if len(policy.DenyPorts) > 0 {
			b.WriteString("\n;; --- Network: deny specific ports ---\n")
			for _, port := range policy.DenyPorts {
				b.WriteString(fmt.Sprintf("(deny network-outbound (remote tcp \"*:%d\"))\n", port))
			}
		}
		if len(policy.AllowPorts) > 0 {
			// With (allow default), outbound is already allowed.
			// To restrict to specific ports: deny all outbound, then allow specific.
			b.WriteString("\n;; --- Network: allow only specific ports ---\n")
			b.WriteString("(deny network-outbound)\n")
			for _, port := range policy.AllowPorts {
				b.WriteString(fmt.Sprintf("(allow network-outbound (remote tcp \"*:%d\"))\n", port))
				if port == 53 {
					b.WriteString(fmt.Sprintf("(allow network-outbound (remote udp \"*:%d\"))\n", port))
				}
			}
		}
		// No DenyPorts and no AllowPorts: outbound is allowed by (allow default)
	case NetworkUnrestricted:
		// (allow default) covers everything
	}

	// --- Denied paths (secrets) ---
	// Must come last: (allow default) is a blanket allow, but an explicit
	// (deny file-read-data (literal "...")) with a path filter is more
	// specific and overrides it.
	deniedPaths := expandGlobs(policy.Denied)
	if len(deniedPaths) > 0 {
		b.WriteString("\n;; --- Denied paths (secrets) ---\n")
		for _, p := range deniedPaths {
			expr := seatbeltPath(p)
			b.WriteString(fmt.Sprintf("(deny file-read-data %s)\n", expr))
			b.WriteString(fmt.Sprintf("(deny file-write* %s)\n", expr))
		}
	}

	return b.String(), nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/sandbox/ -v`
Expected: All pass. Some existing tests may need adjustment:
- `TestGenerateSeatbeltProfile_WritablePaths` — now checks for write restriction deny, not `(allow file-write*`
- `TestGenerateSeatbeltProfile_SystemEssentials` — system essentials are no longer individually listed (covered by `allow default`); update assertions
- `TestGenerateSeatbeltProfile_NetworkNone` — should now assert `(deny network*)` instead of checking for absence of allow

Fix any remaining test failures by updating assertions to match the new profile structure.

- [ ] **Step 5: Run all tests**

Run: `go test ./...`
Expected: All pass.

- [ ] **Step 6: Commit**

Stage and use `/commit`:
```bash
git add internal/sandbox/darwin.go internal/sandbox/darwin_test.go
```

---

### Task 2: Simplify DefaultPolicy and delete extraReadablePaths

**Files:**
- Modify: `internal/sandbox/sandbox.go:82-192`
- Test: `internal/sandbox/sandbox_test.go`

- [ ] **Step 1: Update failing test — DefaultPolicy should have homeDir + projectRoot in Readable**

In `internal/sandbox/sandbox_test.go`, replace `TestDefaultPolicy_Paths` (line 10):

```go
func TestDefaultPolicy_Paths(t *testing.T) {
	projectRoot := "/tmp/myproject"
	runtimeDir := "/tmp/aide-12345"
	homeDir := "/home/testuser"
	tempDir := "/tmp"

	policy := DefaultPolicy(projectRoot, runtimeDir, homeDir, tempDir)

	// Writable
	assertContains(t, policy.Writable, projectRoot, "Writable should contain projectRoot")
	assertContains(t, policy.Writable, runtimeDir, "Writable should contain runtimeDir")
	assertContains(t, policy.Writable, tempDir, "Writable should contain tempDir")

	// Readable — deny-list model: homeDir + projectRoot (for Linux Landlock)
	assertContains(t, policy.Readable, homeDir, "Readable should contain homeDir")
	assertContains(t, policy.Readable, projectRoot, "Readable should contain projectRoot")

	// Denied
	assertContains(t, policy.Denied, filepath.Join(homeDir, ".ssh/id_*"), "Denied should contain SSH key glob")
	assertContains(t, policy.Denied, filepath.Join(homeDir, ".aws/credentials"), "Denied should contain AWS credentials")
	assertContains(t, policy.Denied, filepath.Join(homeDir, ".config/aide/secrets"), "Denied should contain aide secrets")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/sandbox/ -run TestDefaultPolicy_Paths -v`
Expected: FAIL — `homeDir` not in Readable.

- [ ] **Step 3: Simplify DefaultPolicy and delete extraReadablePaths**

In `internal/sandbox/sandbox.go`, replace `DefaultPolicy()`:

```go
func DefaultPolicy(projectRoot, runtimeDir, homeDir, tempDir string) Policy {
	writable := []string{
		projectRoot,
		runtimeDir,
		tempDir,
	}
	for _, dir := range extraWritablePaths(homeDir) {
		writable = append(writable, dir)
	}

	return Policy{
		Writable: writable,
		Readable: []string{
			homeDir,
			projectRoot,
		},
		Denied: []string{
			filepath.Join(homeDir, ".ssh/id_*"),
			filepath.Join(homeDir, ".aws/credentials"),
			filepath.Join(homeDir, ".azure"),
			filepath.Join(homeDir, ".config/gcloud"),
			filepath.Join(homeDir, ".config/aide/secrets"),
			filepath.Join(homeDir, "Library/Application Support/Google/Chrome"),
			filepath.Join(homeDir, ".mozilla"),
			filepath.Join(homeDir, "snap/chromium"),
		},
		Network:         NetworkOutbound,
		AllowSubprocess: true,
		CleanEnv:        false,
	}
}
```

Delete the entire `extraReadablePaths()` function (lines 148-192). Keep `extraWritablePaths()` (lines 194-208) and the `"os"` import (still used by `extraWritablePaths` and `expandGlobs`).

- [ ] **Step 4: Run tests to verify pass**

Run: `go test ./...`
Expected: All pass. `policy_test.go` tests use `DefaultPolicy()` dynamically and will auto-adapt.

- [ ] **Step 5: Commit**

Stage and use `/commit`:
```bash
git add internal/sandbox/sandbox.go internal/sandbox/sandbox_test.go
```

---

### Task 3: End-to-end verification

**Files:** None (verification only, no commit)

- [ ] **Step 1: Build aide**

Run: `go build -o /tmp/aide-test ./cmd/aide`

- [ ] **Step 2: Run all tests**

Run: `go test ./...`

- [ ] **Step 3: Inspect generated profile**

Run: `go run ./cmd/aide sandbox test 2>&1 | head -30`

Verify:
- Contains `(allow default)` (NOT `(deny default)`)
- Contains `(deny file-write* (require-not (require-any ...)))` with writable exceptions
- Contains `(deny file-read-data ...)` for SSH keys/credentials
- Does NOT contain `(allow file-read-data (subpath "/"))` (no longer needed)
- Does NOT contain `(allow process-exec)` etc. (covered by allow default)

- [ ] **Step 4: Test agent works (non-interactive)**

```bash
go run ./cmd/aide sandbox test > /tmp/aide-test.sb
sandbox-exec -f /tmp/aide-test.sb $(which claude) --version
```
Expected: Prints version.

- [ ] **Step 5: Test sensitive file denied**

If `~/.ssh/id_ed25519` exists:
```bash
sandbox-exec -f /tmp/aide-test.sb cat ~/.ssh/id_ed25519
```
Expected: `Operation not permitted`

- [ ] **Step 6: Test write restriction**

```bash
sandbox-exec -f /tmp/aide-test.sb touch ~/test-sandbox-write.txt
```
Expected: `Operation not permitted` (home dir is not in writable list)
