# Sandbox Guards Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox syntax for tracking.

**Goal:** Replace the hardcoded deny list and module system with a guard-based architecture where every sandbox access decision flows through named, typed, configurable guard modules.

**Architecture:** Guards extend the existing `Module` interface with `Type()` and `Description()` metadata. A registry maps guard names to implementations. The profile builder resolves active guards from config, orders them by type, and composes the seatbelt profile. Existing modules are absorbed into guard files one-to-one.

**Tech Stack:** Go, Apple Seatbelt (macOS sandbox)

**Spec:** `docs/superpowers/specs/2026-03-22-sandbox-guards-design.md`

---

## File Structure

```
pkg/seatbelt/
├── module.go                          # MODIFIED: Add Guard interface, extend Context
├── modules/
│   ├── guard_base.go                  # NEW (absorbs base.go)
│   ├── guard_system_runtime.go        # NEW (absorbs system.go)
│   ├── guard_network.go               # NEW (absorbs network.go)
│   ├── guard_filesystem.go            # NEW (absorbs filesystem.go)
│   ├── guard_keychain.go              # NEW (absorbs keychain.go)
│   ├── guard_node_toolchain.go        # NEW (absorbs node.go)
│   ├── guard_nix_toolchain.go         # NEW (absorbs nix.go)
│   ├── guard_git_integration.go       # NEW (absorbs git.go)
│   ├── guard_ssh_keys.go              # NEW
│   ├── guard_cloud.go                 # NEW (cloud-aws, cloud-gcp, cloud-azure, cloud-digitalocean, cloud-oci)
│   ├── guard_kubernetes.go            # NEW
│   ├── guard_terraform.go             # NEW
│   ├── guard_vault.go                 # NEW
│   ├── guard_browsers.go              # NEW
│   ├── guard_password_managers.go     # NEW
│   ├── guard_aide_secrets.go          # NEW
│   ├── guard_sensitive.go             # NEW (docker, github-cli, npm, netrc, vercel)
│   ├── guard_custom.go                # NEW
│   ├── registry.go                    # NEW
│   ├── registry_test.go              # NEW
│   ├── claude.go                      # UNCHANGED (agent module, not a guard)
│   ├── base_test.go                   # MODIFIED
│   ├── system_test.go                 # MODIFIED
│   ├── network_test.go                # MODIFIED
│   ├── filesystem_test.go             # MODIFIED
│   ├── toolchain_test.go              # MODIFIED
│   ├── guard_ssh_keys_test.go         # NEW
│   ├── guard_cloud_test.go            # NEW
│   ├── guard_browsers_test.go         # NEW
│   ├── guard_password_managers_test.go # NEW
│   ├── guard_sensitive_test.go        # NEW
│   ├── guard_custom_test.go           # NEW
│   ├── base.go                        # DELETED
│   ├── system.go                      # DELETED
│   ├── network.go                     # DELETED
│   ├── filesystem.go                  # DELETED
│   ├── keychain.go                    # DELETED
│   ├── node.go                        # DELETED
│   ├── nix.go                         # DELETED
│   └── git.go                         # DELETED

internal/sandbox/
├── sandbox.go                         # MODIFIED: new Policy struct, new DefaultPolicy
├── sandbox_test.go                    # MODIFIED
├── darwin.go                          # MODIFIED: guard-based profile composition
├── darwin_test.go                     # MODIFIED
├── policy.go                          # MODIFIED: guard resolution
├── policy_test.go                     # MODIFIED

internal/config/
├── schema.go                          # MODIFIED: guard fields on SandboxPolicy

cmd/aide/
├── commands.go                        # MODIFIED: guards/guard/unguard/types subcommands

internal/launcher/
├── launcher.go                        # MODIFIED: new DefaultPolicy signature
├── passthrough.go                     # MODIFIED: new DefaultPolicy signature
```

---

### Task 1: Guard interface + Context extension

Add the `Guard` interface to `pkg/seatbelt/module.go` and extend `Context` with fields guards need for testability.

**Files:**
- Modify: `pkg/seatbelt/module.go`

- [ ] **Step 1: Write failing test for EnvLookup**

Create `pkg/seatbelt/context_test.go`:

```go
package seatbelt

import "testing"

func TestEnvLookup_Found(t *testing.T) {
	ctx := &Context{
		Env: []string{"HOME=/home/user", "AWS_CONFIG_FILE=/custom/aws"},
	}
	val, ok := ctx.EnvLookup("AWS_CONFIG_FILE")
	if !ok {
		t.Fatal("expected EnvLookup to find AWS_CONFIG_FILE")
	}
	if val != "/custom/aws" {
		t.Errorf("expected /custom/aws, got %q", val)
	}
}

func TestEnvLookup_NotFound(t *testing.T) {
	ctx := &Context{
		Env: []string{"HOME=/home/user"},
	}
	_, ok := ctx.EnvLookup("MISSING_KEY")
	if ok {
		t.Error("expected EnvLookup to return false for missing key")
	}
}

func TestEnvLookup_EmptyValue(t *testing.T) {
	ctx := &Context{
		Env: []string{"EMPTY_VAR="},
	}
	val, ok := ctx.EnvLookup("EMPTY_VAR")
	if !ok {
		t.Fatal("expected EnvLookup to find EMPTY_VAR")
	}
	if val != "" {
		t.Errorf("expected empty string, got %q", val)
	}
}

func TestEnvLookup_NilEnv(t *testing.T) {
	ctx := &Context{}
	_, ok := ctx.EnvLookup("ANY_KEY")
	if ok {
		t.Error("expected EnvLookup to return false with nil Env")
	}
}
```

Run: `go test ./pkg/seatbelt/ -run TestEnvLookup -v`

Expected: FAIL (EnvLookup does not exist yet)

- [ ] **Step 2: Implement Guard interface and Context extension**

Edit `pkg/seatbelt/module.go` — add Guard interface after Module, extend Context, add EnvLookup:

```go
package seatbelt

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Module contributes Seatbelt rules to a profile.
type Module interface {
	// Name returns a human-readable name for section comments.
	Name() string
	// Rules returns the Seatbelt rules this module contributes.
	Rules(ctx *Context) []Rule
}

// Guard is a Module with metadata for the guard system.
type Guard interface {
	Module
	// Type returns the guard type: "always", "default", or "opt-in".
	Type() string
	// Description returns a human-readable description shown in CLI output.
	Description() string
}

// NetworkMode controls the level of network access.
type NetworkMode int

const (
	// NetworkOpen allows all network traffic (inbound + outbound).
	NetworkOpen NetworkMode = iota
	// NetworkOutbound allows outbound connections only.
	NetworkOutbound
	// NetworkNone denies all network traffic (default-deny covers it).
	NetworkNone
)

// Context provides runtime information to modules.
type Context struct {
	HomeDir     string
	ProjectRoot string
	TempDir     string
	RuntimeDir  string
	Env         []string     // for env var overrides (AWS_CONFIG_FILE, KUBECONFIG, etc.)
	GOOS        string       // for OS-specific paths ("darwin", "linux")

	// Fields consumed by specific always-guards
	Network     NetworkMode  // consumed by network guard
	AllowPorts  []int        // consumed by network guard
	DenyPorts   []int        // consumed by network guard
	ExtraDenied []string     // consumed by filesystem guard (user-configured denied: paths)
}

// HomePath returns homeDir joined with a relative path.
func (c *Context) HomePath(rel string) string {
	return filepath.Join(c.HomeDir, rel)
}

// EnvLookup searches ctx.Env for a KEY=VALUE entry and returns the value.
// Returns ("", false) if not found. Guards use this instead of os.Getenv().
func (c *Context) EnvLookup(key string) (string, bool) {
	prefix := key + "="
	for _, e := range c.Env {
		if strings.HasPrefix(e, prefix) {
			return e[len(prefix):], true
		}
	}
	return "", false
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

// String returns the rendered text of a single rule.
func (r Rule) String() string {
	var b strings.Builder
	if r.comment != "" {
		fmt.Fprintf(&b, ";; %s\n", r.comment)
	}
	if r.lines != "" {
		b.WriteString(r.lines)
	}
	return b.String()
}
```

Run: `go test ./pkg/seatbelt/ -run TestEnvLookup -v`

Expected: PASS

- [ ] **Step 3: Commit**

```
/commit
```

---

### Task 2: Convert base module to guard

Create `pkg/seatbelt/modules/guard_base.go` absorbing `base.go`. The base guard is type `always`.

**Files:**
- Create: `pkg/seatbelt/modules/guard_base.go`
- Modify: `pkg/seatbelt/modules/base_test.go`
- Delete: `pkg/seatbelt/modules/base.go`

- [ ] **Step 1: Update test to use Guard interface**

Replace `pkg/seatbelt/modules/base_test.go`:

```go
package modules_test

import (
	"strings"
	"testing"

	"github.com/jskswamy/aide/pkg/seatbelt"
	"github.com/jskswamy/aide/pkg/seatbelt/modules"
)

func renderTestRules(rules []seatbelt.Rule) string {
	var b strings.Builder
	for _, r := range rules {
		b.WriteString(r.String())
		b.WriteByte('\n')
	}
	return b.String()
}

func TestGuard_Base(t *testing.T) {
	g := modules.BaseGuard()

	if g.Name() != "base" {
		t.Errorf("expected Name() = %q, got %q", "base", g.Name())
	}
	if g.Type() != "always" {
		t.Errorf("expected Type() = %q, got %q", "always", g.Type())
	}
	if g.Description() == "" {
		t.Error("expected non-empty Description()")
	}

	output := renderTestRules(g.Rules(nil))

	if !strings.Contains(output, "(version 1)") {
		t.Error("expected output to contain (version 1)")
	}
	if !strings.Contains(output, "(deny default)") {
		t.Error("expected output to contain (deny default)")
	}
}
```

Run: `go test ./pkg/seatbelt/modules/ -run TestGuard_Base -v`

Expected: FAIL (BaseGuard does not exist)

- [ ] **Step 2: Create guard_base.go and backward-compat wrapper**

Create `pkg/seatbelt/modules/guard_base.go`:

```go
// Package modules provides composable Seatbelt profile building blocks.
package modules

import "github.com/jskswamy/aide/pkg/seatbelt"

type baseGuard struct{}

// BaseGuard returns the base guard that emits Seatbelt version and default-deny.
func BaseGuard() seatbelt.Guard { return &baseGuard{} }

// Base returns the base module (backward-compatible alias).
func Base() seatbelt.Module { return &baseGuard{} }

func (m *baseGuard) Name() string        { return "base" }
func (m *baseGuard) Type() string        { return "always" }
func (m *baseGuard) Description() string { return "(version 1), (deny default)" }

func (m *baseGuard) Rules(_ *seatbelt.Context) []seatbelt.Rule {
	return []seatbelt.Rule{
		seatbelt.Raw("(version 1)"),
		seatbelt.Raw("(deny default)"),
	}
}
```

Run: `go test ./pkg/seatbelt/modules/ -run TestGuard_Base -v`

Expected: PASS

- [ ] **Step 3: Delete old base.go**

Delete `pkg/seatbelt/modules/base.go`.

Run: `go test ./pkg/seatbelt/modules/ -v`

Expected: PASS (all existing tests still pass since `Base()` is re-exported from guard_base.go)

- [ ] **Step 4: Commit**

```
/commit
```

---

### Task 3: Convert system-runtime to guard

**Files:**
- Create: `pkg/seatbelt/modules/guard_system_runtime.go`
- Modify: `pkg/seatbelt/modules/system_test.go`
- Delete: `pkg/seatbelt/modules/system.go`

- [ ] **Step 1: Update test to verify Guard interface**

Add to the top of `pkg/seatbelt/modules/system_test.go`, replacing the `systemRuntimeOutput` helper and adding a guard metadata test:

```go
package modules_test

import (
	"strings"
	"testing"

	"github.com/jskswamy/aide/pkg/seatbelt"
	"github.com/jskswamy/aide/pkg/seatbelt/modules"
)

func systemRuntimeOutput() string {
	ctx := &seatbelt.Context{
		HomeDir: "/Users/testuser",
	}
	g := modules.SystemRuntimeGuard()
	return renderTestRules(g.Rules(ctx))
}

func TestGuard_SystemRuntime_Metadata(t *testing.T) {
	g := modules.SystemRuntimeGuard()
	if g.Name() != "system-runtime" {
		t.Errorf("expected Name() = %q, got %q", "system-runtime", g.Name())
	}
	if g.Type() != "always" {
		t.Errorf("expected Type() = %q, got %q", "always", g.Type())
	}
	if g.Description() == "" {
		t.Error("expected non-empty Description()")
	}
}
```

The existing `TestSystemRuntime_*` tests remain unchanged (they call `systemRuntimeOutput()` which now uses the guard).

Run: `go test ./pkg/seatbelt/modules/ -run TestGuard_SystemRuntime_Metadata -v`

Expected: FAIL (SystemRuntimeGuard does not exist)

- [ ] **Step 2: Create guard_system_runtime.go**

Create `pkg/seatbelt/modules/guard_system_runtime.go`:

```go
// System runtime guard for macOS Seatbelt profiles.
//
// Rules ported from agent-safehouse by Eugene Goldin:
// https://github.com/eugene1g/agent-safehouse
// Source: profiles/10-system-runtime.sb

package modules

import "github.com/jskswamy/aide/pkg/seatbelt"

type systemRuntimeGuard struct{}

// SystemRuntimeGuard returns the system-runtime guard.
func SystemRuntimeGuard() seatbelt.Guard { return &systemRuntimeGuard{} }

// SystemRuntime returns the system-runtime module (backward-compatible alias).
func SystemRuntime() seatbelt.Module { return &systemRuntimeGuard{} }

func (m *systemRuntimeGuard) Name() string        { return "system-runtime" }
func (m *systemRuntimeGuard) Type() string        { return "always" }
func (m *systemRuntimeGuard) Description() string { return "OS binaries, devices, mach services, temp dirs, process rules" }

func (m *systemRuntimeGuard) Rules(ctx *seatbelt.Context) []seatbelt.Rule {
	home := ctx.HomeDir

	return []seatbelt.Rule{
		// 1. System binary paths
		seatbelt.Section("System binary paths"),
		seatbelt.Raw(`(allow file-read*
    (subpath "/usr")
    (subpath "/bin")
    (subpath "/sbin")
    (subpath "/opt")
    (subpath "/System/Library")
    (subpath "/System/Volumes/Preboot")
    (subpath "/Library/Apple")
    (subpath "/Library/Frameworks")
    (subpath "/Library/Fonts")
    (subpath "/Library/Filesystems/NetFSPlugins")
    (subpath "/Library/Preferences/Logging")
    (literal "/Library/Preferences/.GlobalPreferences.plist")
    (literal "/Library/Preferences/com.apple.networkd.plist")
    (literal "/dev")
)`),

		// 2. Root filesystem traversal
		seatbelt.Section("Root filesystem traversal"),
		seatbelt.Raw(`(allow file-read-data
    (literal "/")
)`),

		// 3. Metadata traversal
		seatbelt.Section("Metadata traversal"),
		seatbelt.Raw(`(allow file-read-metadata
    (literal "/")
    (literal "/Users")
    (subpath "/System")
    (subpath "/private/var/run")
)`),

		// 3. Private/etc paths
		seatbelt.Section("Private/etc paths"),
		seatbelt.Raw(`(allow file-read*
    (literal "/private")
    (literal "/private/var")
    (subpath "/private/var/db/timezone")
    (literal "/private/var/select/sh")
    (literal "/private/var/select/developer_dir")
    (literal "/var/select/developer_dir")
    (literal "/private/var/db/xcode_select_link")
    (literal "/var/db/xcode_select_link")
    (literal "/private/etc/hosts")
    (literal "/private/etc/resolv.conf")
    (literal "/private/etc/services")
    (literal "/private/etc/protocols")
    (literal "/private/etc/shells")
    (subpath "/private/etc/ssl")
    (literal "/private/etc/localtime")
    (literal "/etc")
    (literal "/var")
)`),

		// 4. Home metadata traversal
		seatbelt.Section("Home metadata traversal"),
		seatbelt.Raw(`(allow file-read-metadata
    (literal "/home")
    (literal "/private/etc")
    (subpath "/dev")
    ` + seatbelt.HomeLiteral(home, ".config") + `
    ` + seatbelt.HomeLiteral(home, ".cache") + `
    ` + seatbelt.HomeLiteral(home, ".local") + `
    ` + seatbelt.HomeLiteral(home, ".local/share") + `
)`),

		// 5. User preferences
		seatbelt.Section("User preferences"),
		seatbelt.Raw(`(allow file-read*
    ` + seatbelt.HomePrefix(home, "Library/Preferences/.GlobalPreferences") + `
    ` + seatbelt.HomePrefix(home, "Library/Preferences/com.apple.GlobalPreferences") + `
    ` + seatbelt.HomeSubpath(home, "Library/Preferences/ByHost") + `
    ` + seatbelt.HomeLiteral(home, ".CFUserTextEncoding") + `
    ` + seatbelt.HomeLiteral(home, ".config") + `
    ` + seatbelt.HomeLiteral(home, ".cache") + `
    ` + seatbelt.HomeLiteral(home, ".local/bin") + `
)`),

		// 6. Process rules
		seatbelt.Section("Process rules"),
		seatbelt.Allow("process-exec"),
		seatbelt.Allow("process-fork"),
		seatbelt.Allow("sysctl-read"),
		seatbelt.Raw("(allow process-info* (target same-sandbox))"),
		seatbelt.Raw("(allow signal (target same-sandbox))"),
		seatbelt.Raw("(allow mach-priv-task-port (target same-sandbox))"),
		seatbelt.Allow("pseudo-tty"),

		// 7. Temp dirs
		seatbelt.Section("Temp dirs"),
		seatbelt.Raw(`(allow file-read* file-write*
    (subpath "/tmp")
    (subpath "/private/tmp")
    (subpath "/var/folders")
    (subpath "/private/var/folders")
)`),

		// 8. Launchd listener deny
		seatbelt.Section("Launchd listener deny"),
		seatbelt.Raw(`(deny file-read* file-write*
    (regex #"^/private/tmp/com\.apple\.launchd\.[^/]+/Listeners$")
    (regex #"^/tmp/com\.apple\.launchd\.[^/]+/Listeners$")
)`),

		// 9. Device nodes (read-write)
		seatbelt.Section("Device nodes"),
		seatbelt.Raw(`(allow file-read* file-write*
    (subpath "/dev/fd")
    (literal "/dev/stdout")
    (literal "/dev/stderr")
    (literal "/dev/null")
    (literal "/dev/tty")
    (literal "/dev/ptmx")
    (literal "/dev/dtracehelper")
    (regex #"^/dev/tty")
    (regex #"^/dev/ttys")
    (regex #"^/dev/pty")
)`),

		// 10. Read-only devices
		seatbelt.Section("Read-only devices"),
		seatbelt.Raw(`(allow file-read*
    (literal "/dev/zero")
    (literal "/dev/autofs_nowait")
    (literal "/dev/dtracehelper")
    (literal "/dev/urandom")
    (literal "/dev/random")
)`),

		// 11. File ioctl
		seatbelt.Section("File ioctl"),
		seatbelt.Raw(`(allow file-ioctl
    (literal "/dev/dtracehelper")
    (literal "/dev/tty")
    (literal "/dev/ptmx")
    (regex #"^/dev/tty")
    (regex #"^/dev/ttys")
    (regex #"^/dev/pty")
)`),

		// 12. Mach services
		seatbelt.Section("Mach services"),
		seatbelt.Raw(`(allow mach-lookup
    (global-name "com.apple.system.notification_center")
    (global-name "com.apple.system.opendirectoryd.libinfo")
    (global-name "com.apple.logd")
    (global-name "com.apple.FSEvents")
    (global-name "com.apple.SystemConfiguration.configd")
    (global-name "com.apple.SystemConfiguration.DNSConfiguration")
    (global-name "com.apple.trustd.agent")
    (global-name "com.apple.diagnosticd")
    (global-name "com.apple.analyticsd")
    (global-name "com.apple.dnssd.service")
    (global-name "com.apple.CoreServices.coreservicesd")
    (global-name "com.apple.DiskArbitration.diskarbitrationd")
    (global-name "com.apple.analyticsd.messagetracer")
    (global-name "com.apple.system.logger")
    (global-name "com.apple.coreservices.launchservicesd")
)`),

		// 13. System socket
		seatbelt.Section("System socket"),
		seatbelt.Allow("system-socket"),

		// 14. IPC shared memory
		seatbelt.Section("IPC shared memory"),
		seatbelt.Raw(`(allow ipc-posix-shm-read-data
    (ipc-posix-name "apple.shm.notification_center")
)`),
	}
}
```

Run: `go test ./pkg/seatbelt/modules/ -run "TestGuard_SystemRuntime|TestSystemRuntime" -v`

Expected: PASS

- [ ] **Step 3: Delete old system.go**

Delete `pkg/seatbelt/modules/system.go`.

Run: `go test ./pkg/seatbelt/modules/ -v`

Expected: PASS

- [ ] **Step 4: Commit**

```
/commit
```

---

### Task 4: Convert network module to guard

The network guard reads from `ctx.Network`, `ctx.AllowPorts`, and `ctx.DenyPorts` instead of constructor parameters.

**Files:**
- Create: `pkg/seatbelt/modules/guard_network.go`
- Modify: `pkg/seatbelt/modules/network_test.go`
- Delete: `pkg/seatbelt/modules/network.go`

- [ ] **Step 1: Write guard-based network tests**

Replace `pkg/seatbelt/modules/network_test.go`:

```go
package modules_test

import (
	"strings"
	"testing"

	"github.com/jskswamy/aide/pkg/seatbelt"
	"github.com/jskswamy/aide/pkg/seatbelt/modules"
)

func TestGuard_Network_Metadata(t *testing.T) {
	g := modules.NetworkGuard()
	if g.Name() != "network" {
		t.Errorf("expected Name() = %q, got %q", "network", g.Name())
	}
	if g.Type() != "always" {
		t.Errorf("expected Type() = %q, got %q", "always", g.Type())
	}
}

func TestGuard_Network_Outbound(t *testing.T) {
	g := modules.NetworkGuard()
	ctx := &seatbelt.Context{Network: seatbelt.NetworkOutbound}
	output := renderTestRules(g.Rules(ctx))

	if !strings.Contains(output, "(allow network-outbound)") {
		t.Error("expected (allow network-outbound)")
	}
}

func TestGuard_Network_Open(t *testing.T) {
	g := modules.NetworkGuard()
	ctx := &seatbelt.Context{Network: seatbelt.NetworkOpen}
	output := renderTestRules(g.Rules(ctx))

	if !strings.Contains(output, "(allow network*)") {
		t.Error("expected (allow network*)")
	}
}

func TestGuard_Network_None(t *testing.T) {
	g := modules.NetworkGuard()
	ctx := &seatbelt.Context{Network: seatbelt.NetworkNone}
	rules := g.Rules(ctx)

	if len(rules) != 0 {
		t.Errorf("expected no rules for NetworkNone, got %d", len(rules))
	}
}

func TestGuard_Network_PortFiltering(t *testing.T) {
	g := modules.NetworkGuard()
	ctx := &seatbelt.Context{
		Network:    seatbelt.NetworkOutbound,
		AllowPorts: []int{443, 53, 80},
	}
	output := renderTestRules(g.Rules(ctx))

	if !strings.Contains(output, "(deny network-outbound)") {
		t.Error("expected deny network-outbound before allow rules")
	}
	if !strings.Contains(output, `(allow network-outbound (remote tcp "*:443"))`) {
		t.Error("expected allow for TCP port 443")
	}
	if !strings.Contains(output, `(allow network-outbound (remote udp "*:53"))`) {
		t.Error("expected allow for UDP port 53 (DNS)")
	}
}

func TestGuard_Network_DenyPorts(t *testing.T) {
	g := modules.NetworkGuard()
	ctx := &seatbelt.Context{
		Network:   seatbelt.NetworkOutbound,
		DenyPorts: []int{22, 25},
	}
	output := renderTestRules(g.Rules(ctx))

	if !strings.Contains(output, `(deny network-outbound (remote tcp "*:22"))`) {
		t.Error("expected deny for TCP port 22")
	}
	if !strings.Contains(output, "(allow network-outbound)") {
		t.Error("expected allow network-outbound with deny ports")
	}
}

func TestGuard_Network_AllowTakesPrecedence(t *testing.T) {
	g := modules.NetworkGuard()
	ctx := &seatbelt.Context{
		Network:    seatbelt.NetworkOutbound,
		AllowPorts: []int{443},
		DenyPorts:  []int{22},
	}
	output := renderTestRules(g.Rules(ctx))

	if !strings.Contains(output, `(allow network-outbound (remote tcp "*:443"))`) {
		t.Error("expected allow for TCP port 443")
	}
	if strings.Contains(output, `(deny network-outbound (remote tcp "*:22"))`) {
		t.Error("DenyPorts should be ignored when AllowPorts is set")
	}
}

// Backward compatibility: old constructors still work
func TestNetwork_BackwardCompat_Open(t *testing.T) {
	m := modules.Network(modules.NetworkModeOpen)
	output := renderTestRules(m.Rules(nil))
	if !strings.Contains(output, "(allow network*)") {
		t.Error("expected (allow network*)")
	}
}

func TestNetwork_BackwardCompat_Outbound(t *testing.T) {
	m := modules.Network(modules.NetworkModeOutbound)
	output := renderTestRules(m.Rules(nil))
	if !strings.Contains(output, "(allow network-outbound)") {
		t.Error("expected (allow network-outbound)")
	}
}

func TestNetwork_BackwardCompat_WithPorts(t *testing.T) {
	m := modules.NetworkWithPorts(modules.NetworkModeOutbound, modules.PortOpts{
		AllowPorts: []int{443},
	})
	output := renderTestRules(m.Rules(nil))
	if !strings.Contains(output, `(allow network-outbound (remote tcp "*:443"))`) {
		t.Error("expected allow for TCP port 443")
	}
}
```

Run: `go test ./pkg/seatbelt/modules/ -run TestGuard_Network -v`

Expected: FAIL (NetworkGuard does not exist)

- [ ] **Step 2: Create guard_network.go**

Create `pkg/seatbelt/modules/guard_network.go`:

```go
// Network guard for macOS Seatbelt profiles.
//
// Controls network access with three modes: open, outbound-only, and none.
// Reads from ctx.Network, ctx.AllowPorts, ctx.DenyPorts.

package modules

import (
	"fmt"

	"github.com/jskswamy/aide/pkg/seatbelt"
)

// NetworkMode controls the level of network access (backward-compatible type).
type NetworkMode = seatbelt.NetworkMode

// Backward-compatible constants.
const (
	NetworkModeOpen     = seatbelt.NetworkOpen
	NetworkModeOutbound = seatbelt.NetworkOutbound
	NetworkModeNone     = seatbelt.NetworkNone
)

// Backward-compatible aliases for old code.
var (
	NetworkOpen     = seatbelt.NetworkOpen
	NetworkOutbound = seatbelt.NetworkOutbound
	NetworkNone     = seatbelt.NetworkNone
)

// PortOpts configures port-level filtering for outbound connections.
type PortOpts struct {
	AllowPorts []int
	DenyPorts  []int
}

type networkGuard struct{}

// NetworkGuard returns the network guard (reads from ctx).
func NetworkGuard() seatbelt.Guard { return &networkGuard{} }

func (m *networkGuard) Name() string        { return "network" }
func (m *networkGuard) Type() string        { return "always" }
func (m *networkGuard) Description() string { return "Network access mode, port filtering" }

func (m *networkGuard) Rules(ctx *seatbelt.Context) []seatbelt.Rule {
	if ctx == nil {
		return nil
	}
	switch ctx.Network {
	case seatbelt.NetworkOpen:
		return []seatbelt.Rule{seatbelt.Allow("network*")}
	case seatbelt.NetworkOutbound:
		return networkOutboundRules(ctx.AllowPorts, ctx.DenyPorts)
	case seatbelt.NetworkNone:
		return nil
	default:
		return nil
	}
}

func networkOutboundRules(allowPorts, denyPorts []int) []seatbelt.Rule {
	if len(allowPorts) > 0 {
		return networkAllowPortRules(allowPorts)
	}
	if len(denyPorts) > 0 {
		return networkDenyPortRules(denyPorts)
	}
	return []seatbelt.Rule{seatbelt.Allow("network-outbound")}
}

func networkAllowPortRules(ports []int) []seatbelt.Rule {
	rules := []seatbelt.Rule{seatbelt.Deny("network-outbound")}
	for _, port := range ports {
		rules = append(rules,
			seatbelt.Raw(fmt.Sprintf(`(allow network-outbound (remote tcp "*:%d"))`, port)),
		)
		if port == 53 {
			rules = append(rules,
				seatbelt.Raw(fmt.Sprintf(`(allow network-outbound (remote udp "*:%d"))`, port)),
			)
		}
	}
	return rules
}

func networkDenyPortRules(ports []int) []seatbelt.Rule {
	rules := []seatbelt.Rule{seatbelt.Allow("network-outbound")}
	for _, port := range ports {
		rules = append(rules,
			seatbelt.Raw(fmt.Sprintf(`(deny network-outbound (remote tcp "*:%d"))`, port)),
		)
	}
	return rules
}

// --- Backward-compatible constructors for darwin.go ---

type networkModuleCompat struct {
	mode seatbelt.NetworkMode
	opts PortOpts
}

// Network returns a backward-compatible network module.
func Network(mode seatbelt.NetworkMode) seatbelt.Module {
	return &networkModuleCompat{mode: mode}
}

// NetworkWithPorts returns a backward-compatible network module with ports.
func NetworkWithPorts(mode seatbelt.NetworkMode, opts PortOpts) seatbelt.Module {
	return &networkModuleCompat{mode: mode, opts: opts}
}

func (m *networkModuleCompat) Name() string { return "Network" }

func (m *networkModuleCompat) Rules(_ *seatbelt.Context) []seatbelt.Rule {
	switch m.mode {
	case seatbelt.NetworkOpen:
		return []seatbelt.Rule{seatbelt.Allow("network*")}
	case seatbelt.NetworkOutbound:
		return networkOutboundRules(m.opts.AllowPorts, m.opts.DenyPorts)
	case seatbelt.NetworkNone:
		return nil
	default:
		return nil
	}
}
```

Run: `go test ./pkg/seatbelt/modules/ -run "TestGuard_Network|TestNetwork_Backward" -v`

Expected: PASS

- [ ] **Step 3: Delete old network.go**

Delete `pkg/seatbelt/modules/network.go`.

Run: `go test ./pkg/seatbelt/modules/ -v && go test ./internal/sandbox/ -v`

Expected: PASS (backward-compat constructors keep darwin.go working)

- [ ] **Step 4: Commit**

```
/commit
```

---

### Task 5: Convert filesystem module to guard

The filesystem guard reads from `ctx` for project root, home dir, and `ctx.ExtraDenied` for user-denied paths.

**Files:**
- Create: `pkg/seatbelt/modules/guard_filesystem.go`
- Modify: `pkg/seatbelt/modules/filesystem_test.go`
- Delete: `pkg/seatbelt/modules/filesystem.go`

- [ ] **Step 1: Write guard-based filesystem tests**

Add guard tests to `pkg/seatbelt/modules/filesystem_test.go`:

```go
package modules_test

import (
	"strings"
	"testing"

	"github.com/jskswamy/aide/pkg/seatbelt"
	"github.com/jskswamy/aide/pkg/seatbelt/modules"
)

func TestGuard_Filesystem_Metadata(t *testing.T) {
	g := modules.FilesystemGuard()
	if g.Name() != "filesystem" {
		t.Errorf("expected Name() = %q, got %q", "filesystem", g.Name())
	}
	if g.Type() != "always" {
		t.Errorf("expected Type() = %q, got %q", "always", g.Type())
	}
}

func TestGuard_Filesystem_WritableReadable(t *testing.T) {
	g := modules.FilesystemGuard()
	ctx := &seatbelt.Context{
		HomeDir:     "/Users/testuser",
		ProjectRoot: "/Users/testuser/projects/myapp",
		TempDir:     "/tmp",
		RuntimeDir:  "/tmp/aide-12345",
	}
	output := renderTestRules(g.Rules(ctx))

	// Project root should be writable
	if !strings.Contains(output, "(allow file-read* file-write*") {
		t.Error("expected writable block")
	}
	if !strings.Contains(output, `(subpath "/Users/testuser/projects/myapp")`) {
		t.Error("expected project root in writable paths")
	}

	// Home dir should be readable
	if !strings.Contains(output, `(subpath "/Users/testuser")`) {
		t.Error("expected home dir in readable paths")
	}
}

func TestGuard_Filesystem_UserDenied(t *testing.T) {
	g := modules.FilesystemGuard()
	ctx := &seatbelt.Context{
		HomeDir:     "/Users/testuser",
		ProjectRoot: "/Users/testuser/projects/myapp",
		TempDir:     "/tmp",
		RuntimeDir:  "/tmp/aide-12345",
		ExtraDenied: []string{"/custom/secret", "/another/secret"},
	}
	output := renderTestRules(g.Rules(ctx))

	if !strings.Contains(output, `(deny file-read-data`) {
		t.Error("expected deny file-read-data for user-denied paths")
	}
	if !strings.Contains(output, `/custom/secret`) {
		t.Error("expected /custom/secret in denied output")
	}
	if !strings.Contains(output, `/another/secret`) {
		t.Error("expected /another/secret in denied output")
	}
}
```

Run: `go test ./pkg/seatbelt/modules/ -run TestGuard_Filesystem -v`

Expected: FAIL (FilesystemGuard does not exist)

- [ ] **Step 2: Create guard_filesystem.go**

Create `pkg/seatbelt/modules/guard_filesystem.go`:

```go
// Filesystem guard for macOS Seatbelt profiles.
//
// Controls file system access: project root (rw), home dir (ro),
// runtime/temp dirs (rw), and user-configured denied paths.

package modules

import (
	"fmt"
	"strings"

	"github.com/jskswamy/aide/pkg/seatbelt"
)

// FilesystemConfig specifies filesystem access rules (backward-compatible).
type FilesystemConfig struct {
	Writable []string
	Readable []string
	Denied   []string
}

type filesystemGuard struct{}

// FilesystemGuard returns the filesystem guard (reads from ctx).
func FilesystemGuard() seatbelt.Guard { return &filesystemGuard{} }

func (m *filesystemGuard) Name() string        { return "filesystem" }
func (m *filesystemGuard) Type() string        { return "always" }
func (m *filesystemGuard) Description() string { return "Project root (rw), home dir (ro), user-configured denied paths" }

func (m *filesystemGuard) Rules(ctx *seatbelt.Context) []seatbelt.Rule {
	if ctx == nil {
		return nil
	}

	var rules []seatbelt.Rule

	// Writable: project root, runtime dir, temp dir
	writable := []string{ctx.ProjectRoot, ctx.RuntimeDir, ctx.TempDir}
	var validWritable []string
	for _, p := range writable {
		if p != "" {
			validWritable = append(validWritable, p)
		}
	}
	if len(validWritable) > 0 {
		rules = append(rules, seatbelt.Raw(fmt.Sprintf("(allow file-read* file-write*\n    %s)", buildRequireAny(validWritable))))
	}

	// Readable: home dir, project root
	readable := []string{ctx.HomeDir, ctx.ProjectRoot}
	var validReadable []string
	for _, p := range readable {
		if p != "" {
			validReadable = append(validReadable, p)
		}
	}
	if len(validReadable) > 0 {
		rules = append(rules, seatbelt.Raw(fmt.Sprintf("(allow file-read*\n    %s)", buildRequireAny(validReadable))))
	}

	// Denied: user-configured extra denied paths
	if len(ctx.ExtraDenied) > 0 {
		expanded := seatbelt.ExpandGlobs(ctx.ExtraDenied)
		for _, p := range expanded {
			expr := seatbelt.Path(p)
			rules = append(rules,
				seatbelt.Raw(fmt.Sprintf("(deny file-read-data %s)", expr)),
				seatbelt.Raw(fmt.Sprintf("(deny file-write* %s)", expr)),
			)
		}
	}

	return rules
}

// --- Backward-compatible constructor ---

type filesystemModuleCompat struct {
	cfg FilesystemConfig
}

// Filesystem returns a backward-compatible filesystem module.
func Filesystem(cfg FilesystemConfig) seatbelt.Module {
	return &filesystemModuleCompat{cfg: cfg}
}

func (m *filesystemModuleCompat) Name() string { return "Filesystem" }

func (m *filesystemModuleCompat) Rules(_ *seatbelt.Context) []seatbelt.Rule {
	var rules []seatbelt.Rule

	if len(m.cfg.Writable) > 0 {
		rules = append(rules, seatbelt.Raw(fmt.Sprintf("(allow file-read* file-write*\n    %s)", buildRequireAny(m.cfg.Writable))))
	}
	if len(m.cfg.Readable) > 0 {
		rules = append(rules, seatbelt.Raw(fmt.Sprintf("(allow file-read*\n    %s)", buildRequireAny(m.cfg.Readable))))
	}
	if len(m.cfg.Denied) > 0 {
		expanded := seatbelt.ExpandGlobs(m.cfg.Denied)
		for _, p := range expanded {
			expr := seatbelt.Path(p)
			rules = append(rules,
				seatbelt.Raw(fmt.Sprintf("(deny file-read-data %s)", expr)),
				seatbelt.Raw(fmt.Sprintf("(deny file-write* %s)", expr)),
			)
		}
	}

	return rules
}

func buildRequireAny(paths []string) string {
	if len(paths) == 1 {
		return seatbelt.Path(paths[0])
	}
	var exprs []string
	for _, p := range paths {
		exprs = append(exprs, "    "+seatbelt.Path(p))
	}
	return fmt.Sprintf("(require-any\n%s)", strings.Join(exprs, "\n"))
}
```

Run: `go test ./pkg/seatbelt/modules/ -run "TestGuard_Filesystem|TestFilesystem" -v`

Expected: PASS

- [ ] **Step 3: Delete old filesystem.go**

Delete `pkg/seatbelt/modules/filesystem.go`.

Run: `go test ./pkg/seatbelt/modules/ -v && go test ./internal/sandbox/ -v`

Expected: PASS

- [ ] **Step 4: Commit**

```
/commit
```

---

### Task 6: Convert keychain module to guard

**Files:**
- Create: `pkg/seatbelt/modules/guard_keychain.go`
- Modify: `pkg/seatbelt/modules/toolchain_test.go` (update `TestKeychainIntegration`)
- Delete: `pkg/seatbelt/modules/keychain.go`

- [ ] **Step 1: Add guard metadata test to toolchain_test.go**

Add before the existing `TestKeychainIntegration` test:

```go
func TestGuard_Keychain_Metadata(t *testing.T) {
	g := modules.KeychainGuard()
	if g.Name() != "keychain" {
		t.Errorf("expected Name() = %q, got %q", "keychain", g.Name())
	}
	if g.Type() != "always" {
		t.Errorf("expected Type() = %q, got %q", "always", g.Type())
	}
}

func TestGuard_Keychain_AllowRules(t *testing.T) {
	g := modules.KeychainGuard()
	ctx := &seatbelt.Context{HomeDir: "/Users/testuser"}
	output := renderTestRules(g.Rules(ctx))

	// Keychain should produce ALLOW rules (not deny)
	if !strings.Contains(output, `(subpath "/Users/testuser/Library/Keychains")`) {
		t.Error("expected allow for ~/Library/Keychains")
	}
	if !strings.Contains(output, "com.apple.SecurityServer") {
		t.Error("expected SecurityServer mach service")
	}
	if !strings.Contains(output, "com.apple.AppleDatabaseChanged") {
		t.Error("expected IPC shared memory")
	}
}
```

Run: `go test ./pkg/seatbelt/modules/ -run TestGuard_Keychain -v`

Expected: FAIL (KeychainGuard does not exist)

- [ ] **Step 2: Create guard_keychain.go**

Create `pkg/seatbelt/modules/guard_keychain.go`:

```go
// Keychain guard for macOS Seatbelt profiles.
//
// Rules ported from agent-safehouse by Eugene Goldin:
// https://github.com/eugene1g/agent-safehouse
// Source: profiles/55-integrations-optional/keychain.sb

package modules

import "github.com/jskswamy/aide/pkg/seatbelt"

type keychainGuard struct{}

// KeychainGuard returns the keychain guard.
func KeychainGuard() seatbelt.Guard { return &keychainGuard{} }

// KeychainIntegration returns the keychain module (backward-compatible alias).
func KeychainIntegration() seatbelt.Module { return &keychainGuard{} }

func (m *keychainGuard) Name() string        { return "keychain" }
func (m *keychainGuard) Type() string        { return "always" }
func (m *keychainGuard) Description() string { return "macOS Keychain access -- Security framework, mach services, IPC" }

func (m *keychainGuard) Rules(ctx *seatbelt.Context) []seatbelt.Rule {
	home := ctx.HomeDir

	return []seatbelt.Rule{
		seatbelt.Section("User keychain"),
		seatbelt.Raw(`(allow file-read* file-write*
    ` + seatbelt.HomeSubpath(home, "Library/Keychains") + `
    ` + seatbelt.HomeLiteral(home, "Library/Preferences/com.apple.security.plist") + `
)`),

		seatbelt.Section("System keychain"),
		seatbelt.Raw(`(allow file-read*
    (literal "/Library/Preferences/com.apple.security.plist")
    (literal "/Library/Keychains/System.keychain")
    (subpath "/private/var/db/mds")
)`),

		seatbelt.Section("Keychain metadata traversal"),
		seatbelt.Raw(`(allow file-read-metadata
    ` + seatbelt.HomeLiteral(home, "Library") + `
    ` + seatbelt.HomeLiteral(home, "Library/Keychains") + `
    (literal "/Library")
    (literal "/Library/Keychains")
)`),

		seatbelt.Section("Security Mach services"),
		seatbelt.Raw(`(allow mach-lookup
    (global-name "com.apple.SecurityServer")
    (global-name "com.apple.security.agent")
    (global-name "com.apple.securityd.xpc")
    (global-name "com.apple.security.authhost")
    (global-name "com.apple.secd")
    (global-name "com.apple.trustd")
)`),

		seatbelt.Section("Security IPC shared memory"),
		seatbelt.Raw(`(allow ipc-posix-shm-read-data ipc-posix-shm-write-create ipc-posix-shm-write-data
    (ipc-posix-name "com.apple.AppleDatabaseChanged")
)`),
	}
}
```

Run: `go test ./pkg/seatbelt/modules/ -run "TestGuard_Keychain|TestKeychainIntegration" -v`

Expected: PASS

- [ ] **Step 3: Delete old keychain.go**

Delete `pkg/seatbelt/modules/keychain.go`.

Run: `go test ./pkg/seatbelt/modules/ -v`

Expected: PASS

- [ ] **Step 4: Commit**

```
/commit
```

---

### Task 7: Convert node-toolchain to guard

**Files:**
- Create: `pkg/seatbelt/modules/guard_node_toolchain.go`
- Modify: `pkg/seatbelt/modules/toolchain_test.go`
- Delete: `pkg/seatbelt/modules/node.go`

- [ ] **Step 1: Add guard metadata test**

Add to `pkg/seatbelt/modules/toolchain_test.go`:

```go
func TestGuard_NodeToolchain_Metadata(t *testing.T) {
	g := modules.NodeToolchainGuard()
	if g.Name() != "node-toolchain" {
		t.Errorf("expected Name() = %q, got %q", "node-toolchain", g.Name())
	}
	if g.Type() != "always" {
		t.Errorf("expected Type() = %q, got %q", "always", g.Type())
	}
}

func TestGuard_NodeToolchain_Paths(t *testing.T) {
	g := modules.NodeToolchainGuard()
	ctx := &seatbelt.Context{HomeDir: "/Users/testuser"}
	output := renderTestRules(g.Rules(ctx))

	paths := []string{
		`(subpath "/Users/testuser/.npm")`,
		`(subpath "/Users/testuser/.yarn")`,
		`(subpath "/Users/testuser/.nvm")`,
		`(literal "/Users/testuser/.npmrc")`,
		`(literal "/Users/testuser/.yarnrc")`,
	}
	for _, p := range paths {
		if !strings.Contains(output, p) {
			t.Errorf("expected output to contain %q", p)
		}
	}
}
```

Run: `go test ./pkg/seatbelt/modules/ -run TestGuard_NodeToolchain -v`

Expected: FAIL

- [ ] **Step 2: Create guard_node_toolchain.go**

Create `pkg/seatbelt/modules/guard_node_toolchain.go`. Copy the Rules body from `node.go`, update struct name and add Guard methods:

```go
// Node toolchain guard for macOS Seatbelt profiles.
//
// Rules ported from agent-safehouse by Eugene Goldin.

package modules

import "github.com/jskswamy/aide/pkg/seatbelt"

type nodeToolchainGuard struct{}

// NodeToolchainGuard returns the node-toolchain guard.
func NodeToolchainGuard() seatbelt.Guard { return &nodeToolchainGuard{} }

// NodeToolchain returns the node toolchain module (backward-compatible alias).
func NodeToolchain() seatbelt.Module { return &nodeToolchainGuard{} }

func (m *nodeToolchainGuard) Name() string        { return "node-toolchain" }
func (m *nodeToolchainGuard) Type() string        { return "always" }
func (m *nodeToolchainGuard) Description() string { return "npm/yarn/pnpm/nvm/corepack/Prisma/Turborepo paths" }

func (m *nodeToolchainGuard) Rules(ctx *seatbelt.Context) []seatbelt.Rule {
	home := ctx.HomeDir

	return []seatbelt.Rule{
		seatbelt.Section("Node version managers"),
		seatbelt.Raw(`(allow file-read* file-write*
    ` + seatbelt.HomeSubpath(home, ".nvm") + `
    ` + seatbelt.HomeSubpath(home, ".fnm") + `
)`),

		seatbelt.Section("npm"),
		seatbelt.Raw(`(allow file-read* file-write*
    ` + seatbelt.HomeSubpath(home, ".npm") + `
    ` + seatbelt.HomeSubpath(home, ".config/npm") + `
    ` + seatbelt.HomeSubpath(home, ".cache/npm") + `
    ` + seatbelt.HomeSubpath(home, ".cache/node") + `
    ` + seatbelt.HomeSubpath(home, "Library/Caches/npm") + `
    ` + seatbelt.HomeLiteral(home, ".npmrc") + `
    ` + seatbelt.HomeSubpath(home, ".config/configstore") + `
    ` + seatbelt.HomeSubpath(home, ".node-gyp") + `
    ` + seatbelt.HomeSubpath(home, ".cache/node-gyp") + `
)`),

		seatbelt.Section("pnpm"),
		seatbelt.Raw(`(allow file-read* file-write*
    ` + seatbelt.HomeSubpath(home, ".config/pnpm") + `
    ` + seatbelt.HomeSubpath(home, ".pnpm-state") + `
    ` + seatbelt.HomeSubpath(home, ".pnpm-store") + `
    ` + seatbelt.HomeSubpath(home, ".local/share/pnpm") + `
    ` + seatbelt.HomeSubpath(home, ".local/state/pnpm") + `
    ` + seatbelt.HomeSubpath(home, "Library/pnpm") + `
    ` + seatbelt.HomeSubpath(home, "Library/Caches/pnpm") + `
    ` + seatbelt.HomeSubpath(home, "Library/Preferences/pnpm") + `
)`),

		seatbelt.Section("yarn"),
		seatbelt.Raw(`(allow file-read* file-write*
    ` + seatbelt.HomeSubpath(home, ".yarn") + `
    ` + seatbelt.HomeLiteral(home, ".yarnrc") + `
    ` + seatbelt.HomeLiteral(home, ".yarnrc.yml") + `
    ` + seatbelt.HomeSubpath(home, ".config/yarn") + `
    ` + seatbelt.HomeSubpath(home, ".cache/yarn") + `
    ` + seatbelt.HomeSubpath(home, "Library/Caches/Yarn") + `
)`),

		seatbelt.Section("corepack"),
		seatbelt.Raw(`(allow file-read* file-write*
    ` + seatbelt.HomeSubpath(home, ".cache/node/corepack") + `
    ` + seatbelt.HomeSubpath(home, "Library/Caches/node/corepack") + `
)`),

		seatbelt.Section("Browser testing and tools"),
		seatbelt.Raw(`(allow file-read* file-write*
    ` + seatbelt.HomeSubpath(home, "Library/Caches/ms-playwright") + `
    ` + seatbelt.HomeSubpath(home, "Library/Caches/Cypress") + `
    ` + seatbelt.HomeSubpath(home, ".cache/puppeteer") + `
    ` + seatbelt.HomeSubpath(home, "Library/Caches/typescript") + `
)`),

		seatbelt.Section("Prisma"),
		seatbelt.Raw(`(allow file-read* file-write*
    ` + seatbelt.HomeSubpath(home, ".cache/prisma") + `
    ` + seatbelt.HomeSubpath(home, "Library/Caches/prisma-nodejs") + `
    ` + seatbelt.HomeSubpath(home, "Library/Caches/checkpoint-nodejs") + `
    ` + seatbelt.HomeSubpath(home, "Library/Caches/claude-cli-nodejs") + `
)`),

		seatbelt.Section("Turborepo"),
		seatbelt.Raw(`(allow file-read* file-write*
    ` + seatbelt.HomeSubpath(home, ".cache/turbo") + `
    ` + seatbelt.HomeSubpath(home, "Library/Caches/turbo") + `
    ` + seatbelt.HomeSubpath(home, "Library/Application Support/turborepo") + `
)`),
	}
}
```

- [ ] **Step 3: Delete old node.go, run tests**

Delete `pkg/seatbelt/modules/node.go`.

Run: `go test ./pkg/seatbelt/modules/ -run "TestGuard_NodeToolchain|TestNodeToolchain" -v`

Expected: PASS

- [ ] **Step 4: Commit**

```
/commit
```

---

### Task 8: Convert nix-toolchain to guard

**Files:**
- Create: `pkg/seatbelt/modules/guard_nix_toolchain.go`
- Delete: `pkg/seatbelt/modules/nix.go`

- [ ] **Step 1: Add guard test to toolchain_test.go**

```go
func TestGuard_NixToolchain_Metadata(t *testing.T) {
	g := modules.NixToolchainGuard()
	if g.Name() != "nix-toolchain" {
		t.Errorf("expected Name() = %q, got %q", "nix-toolchain", g.Name())
	}
	if g.Type() != "always" {
		t.Errorf("expected Type() = %q, got %q", "always", g.Type())
	}
}

func TestGuard_NixToolchain_Paths(t *testing.T) {
	g := modules.NixToolchainGuard()
	ctx := &seatbelt.Context{HomeDir: "/Users/testuser"}
	output := renderTestRules(g.Rules(ctx))

	paths := []string{
		`"/nix/store"`,
		`"/run/current-system"`,
		`(subpath "/Users/testuser/.nix-profile")`,
	}
	for _, p := range paths {
		if !strings.Contains(output, p) {
			t.Errorf("expected output to contain %q", p)
		}
	}
}
```

- [ ] **Step 2: Create guard_nix_toolchain.go**

```go
// Nix toolchain guard for macOS Seatbelt profiles.

package modules

import "github.com/jskswamy/aide/pkg/seatbelt"

type nixToolchainGuard struct{}

// NixToolchainGuard returns the nix-toolchain guard.
func NixToolchainGuard() seatbelt.Guard { return &nixToolchainGuard{} }

// NixToolchain returns the nix toolchain module (backward-compatible alias).
func NixToolchain() seatbelt.Module { return &nixToolchainGuard{} }

func (m *nixToolchainGuard) Name() string        { return "nix-toolchain" }
func (m *nixToolchainGuard) Type() string        { return "always" }
func (m *nixToolchainGuard) Description() string { return "Nix store, nix profile paths" }

func (m *nixToolchainGuard) Rules(ctx *seatbelt.Context) []seatbelt.Rule {
	home := ctx.HomeDir

	return []seatbelt.Rule{
		seatbelt.Section("Nix store and system paths"),
		seatbelt.Raw(`(allow file-read*
    (subpath "/nix/store")
    (subpath "/nix/var")
    (subpath "/run/current-system")
)`),

		seatbelt.Section("Nix user paths"),
		seatbelt.Raw(`(allow file-read* file-write*
    ` + seatbelt.HomeSubpath(home, ".nix-profile") + `
    ` + seatbelt.HomeSubpath(home, ".local/state/nix") + `
)`),
	}
}
```

- [ ] **Step 3: Delete old nix.go, run tests**

Delete `pkg/seatbelt/modules/nix.go`.

Run: `go test ./pkg/seatbelt/modules/ -run "TestGuard_NixToolchain|TestNixToolchain" -v`

Expected: PASS

- [ ] **Step 4: Commit**

```
/commit
```

---

### Task 9: Convert git-integration to guard

**Files:**
- Create: `pkg/seatbelt/modules/guard_git_integration.go`
- Delete: `pkg/seatbelt/modules/git.go`

- [ ] **Step 1: Add guard test to toolchain_test.go**

```go
func TestGuard_GitIntegration_Metadata(t *testing.T) {
	g := modules.GitIntegrationGuard()
	if g.Name() != "git-integration" {
		t.Errorf("expected Name() = %q, got %q", "git-integration", g.Name())
	}
	if g.Type() != "always" {
		t.Errorf("expected Type() = %q, got %q", "always", g.Type())
	}
}

func TestGuard_GitIntegration_Paths(t *testing.T) {
	g := modules.GitIntegrationGuard()
	ctx := &seatbelt.Context{HomeDir: "/Users/testuser"}
	output := renderTestRules(g.Rules(ctx))

	paths := []string{
		`(prefix "/Users/testuser/.gitconfig")`,
		`(literal "/Users/testuser/.ssh/config")`,
		`(literal "/Users/testuser/.ssh/known_hosts")`,
	}
	for _, p := range paths {
		if !strings.Contains(output, p) {
			t.Errorf("expected output to contain %q", p)
		}
	}

	if strings.Contains(output, "file-write") {
		t.Error("expected git-integration to be read-only")
	}
}
```

- [ ] **Step 2: Create guard_git_integration.go**

```go
// Git integration guard for macOS Seatbelt profiles.
//
// Rules ported from agent-safehouse by Eugene Goldin.

package modules

import "github.com/jskswamy/aide/pkg/seatbelt"

type gitIntegrationGuard struct{}

// GitIntegrationGuard returns the git-integration guard.
func GitIntegrationGuard() seatbelt.Guard { return &gitIntegrationGuard{} }

// GitIntegration returns the git integration module (backward-compatible alias).
func GitIntegration() seatbelt.Module { return &gitIntegrationGuard{} }

func (m *gitIntegrationGuard) Name() string        { return "git-integration" }
func (m *gitIntegrationGuard) Type() string        { return "always" }
func (m *gitIntegrationGuard) Description() string { return "Git config (read-only), SSH config/known_hosts" }

func (m *gitIntegrationGuard) Rules(ctx *seatbelt.Context) []seatbelt.Rule {
	home := ctx.HomeDir

	return []seatbelt.Rule{
		seatbelt.Section("Git configuration (read-only)"),
		seatbelt.Raw(`(allow file-read*
    ` + seatbelt.HomePrefix(home, ".gitconfig") + `
    ` + seatbelt.HomePrefix(home, ".gitignore") + `
    ` + seatbelt.HomeSubpath(home, ".config/git") + `
    ` + seatbelt.HomeLiteral(home, ".gitattributes") + `
    ` + seatbelt.HomeLiteral(home, ".ssh") + `
    ` + seatbelt.HomeLiteral(home, ".ssh/config") + `
    ` + seatbelt.HomeLiteral(home, ".ssh/known_hosts") + `
)`),
	}
}
```

- [ ] **Step 3: Delete old git.go, run tests**

Delete `pkg/seatbelt/modules/git.go`.

Run: `go test ./pkg/seatbelt/modules/ -run "TestGuard_GitIntegration|TestGitIntegration" -v`

Expected: PASS

- [ ] **Step 4: Commit**

```
/commit
```

---

### Task 10: Create ssh-keys guard (NEW)

The ssh-keys guard denies `~/.ssh` (subpath) but allows `known_hosts` and `config` (literal). Literal beats subpath in seatbelt.

**Files:**
- Create: `pkg/seatbelt/modules/guard_ssh_keys.go`
- Create: `pkg/seatbelt/modules/guard_ssh_keys_test.go`

- [ ] **Step 1: Write failing test**

Create `pkg/seatbelt/modules/guard_ssh_keys_test.go`:

```go
package modules_test

import (
	"strings"
	"testing"

	"github.com/jskswamy/aide/pkg/seatbelt"
	"github.com/jskswamy/aide/pkg/seatbelt/modules"
)

func TestGuard_SSHKeys_Metadata(t *testing.T) {
	g := modules.SSHKeysGuard()
	if g.Name() != "ssh-keys" {
		t.Errorf("expected Name() = %q, got %q", "ssh-keys", g.Name())
	}
	if g.Type() != "default" {
		t.Errorf("expected Type() = %q, got %q", "default", g.Type())
	}
}

func TestGuard_SSHKeys_DenyAndAllow(t *testing.T) {
	g := modules.SSHKeysGuard()
	ctx := &seatbelt.Context{HomeDir: "/Users/testuser"}
	output := renderTestRules(g.Rules(ctx))

	// Should deny entire .ssh via subpath
	if !strings.Contains(output, `(deny file-read-data (subpath "/Users/testuser/.ssh"))`) {
		t.Error("expected deny file-read-data (subpath ~/.ssh)")
	}
	if !strings.Contains(output, `(deny file-write* (subpath "/Users/testuser/.ssh"))`) {
		t.Error("expected deny file-write* (subpath ~/.ssh)")
	}

	// Should allow known_hosts and config via literal (beats subpath)
	if !strings.Contains(output, `(allow file-read* (literal "/Users/testuser/.ssh/known_hosts"))`) {
		t.Error("expected allow file-read* (literal ~/.ssh/known_hosts)")
	}
	if !strings.Contains(output, `(allow file-read* (literal "/Users/testuser/.ssh/config"))`) {
		t.Error("expected allow file-read* (literal ~/.ssh/config)")
	}
	// Allow directory listing for metadata traversal
	if !strings.Contains(output, `(allow file-read-metadata (literal "/Users/testuser/.ssh"))`) {
		t.Error("expected allow file-read-metadata (literal ~/.ssh)")
	}
}
```

Run: `go test ./pkg/seatbelt/modules/ -run TestGuard_SSHKeys -v`

Expected: FAIL

- [ ] **Step 2: Implement guard_ssh_keys.go**

Create `pkg/seatbelt/modules/guard_ssh_keys.go`:

```go
// SSH keys guard for macOS Seatbelt profiles.
//
// Denies access to entire ~/.ssh directory, then allows specific files
// (known_hosts, config) via literal rules that beat subpath in seatbelt.

package modules

import (
	"fmt"

	"github.com/jskswamy/aide/pkg/seatbelt"
)

type sshKeysGuard struct{}

// SSHKeysGuard returns the ssh-keys guard.
func SSHKeysGuard() seatbelt.Guard { return &sshKeysGuard{} }

func (m *sshKeysGuard) Name() string        { return "ssh-keys" }
func (m *sshKeysGuard) Type() string        { return "default" }
func (m *sshKeysGuard) Description() string { return "~/.ssh (deny), known_hosts/config (allow)" }

func (m *sshKeysGuard) Rules(ctx *seatbelt.Context) []seatbelt.Rule {
	sshDir := ctx.HomePath(".ssh")

	return []seatbelt.Rule{
		seatbelt.Section("SSH keys deny"),
		seatbelt.Raw(fmt.Sprintf(`(deny file-read-data (subpath "%s"))`, sshDir)),
		seatbelt.Raw(fmt.Sprintf(`(deny file-write* (subpath "%s"))`, sshDir)),

		seatbelt.Section("SSH allowed files (literal beats subpath)"),
		seatbelt.Raw(fmt.Sprintf(`(allow file-read* (literal "%s/known_hosts"))`, sshDir)),
		seatbelt.Raw(fmt.Sprintf(`(allow file-read* (literal "%s/config"))`, sshDir)),
		seatbelt.Raw(fmt.Sprintf(`(allow file-read-metadata (literal "%s"))`, sshDir)),
	}
}
```

Run: `go test ./pkg/seatbelt/modules/ -run TestGuard_SSHKeys -v`

Expected: PASS

- [ ] **Step 3: Commit**

```
/commit
```

---

### Task 11: Create cloud guards (NEW)

Create guards for cloud providers, kubernetes, terraform, and vault. Each checks env var overrides via `ctx.EnvLookup()`.

**Files:**
- Create: `pkg/seatbelt/modules/guard_cloud.go`
- Create: `pkg/seatbelt/modules/guard_kubernetes.go`
- Create: `pkg/seatbelt/modules/guard_terraform.go`
- Create: `pkg/seatbelt/modules/guard_vault.go`
- Create: `pkg/seatbelt/modules/guard_cloud_test.go`

- [ ] **Step 1: Write failing tests**

Create `pkg/seatbelt/modules/guard_cloud_test.go`:

```go
package modules_test

import (
	"strings"
	"testing"

	"github.com/jskswamy/aide/pkg/seatbelt"
	"github.com/jskswamy/aide/pkg/seatbelt/modules"
)

func TestGuard_CloudAWS_Default(t *testing.T) {
	g := modules.CloudAWSGuard()
	if g.Name() != "cloud-aws" {
		t.Errorf("expected Name() = %q, got %q", "cloud-aws", g.Name())
	}
	if g.Type() != "default" {
		t.Errorf("expected Type() = %q, got %q", "default", g.Type())
	}

	ctx := &seatbelt.Context{HomeDir: "/Users/testuser"}
	output := renderTestRules(g.Rules(ctx))

	for _, path := range []string{
		"/Users/testuser/.aws/credentials",
		"/Users/testuser/.aws/config",
		"/Users/testuser/.aws/sso/cache",
		"/Users/testuser/.aws/cli/cache",
	} {
		if !strings.Contains(output, path) {
			t.Errorf("expected output to contain %q", path)
		}
	}
}

func TestGuard_CloudAWS_EnvOverride(t *testing.T) {
	g := modules.CloudAWSGuard()
	ctx := &seatbelt.Context{
		HomeDir: "/Users/testuser",
		Env:     []string{"AWS_SHARED_CREDENTIALS_FILE=/custom/creds"},
	}
	output := renderTestRules(g.Rules(ctx))

	// Custom path should replace default credentials path
	if !strings.Contains(output, "/custom/creds") {
		t.Error("expected /custom/creds from AWS_SHARED_CREDENTIALS_FILE override")
	}
	// Default credentials path should NOT be present
	if strings.Contains(output, "/Users/testuser/.aws/credentials") {
		t.Error("default credentials path should be replaced by env override")
	}
	// Other default paths should still be present
	if !strings.Contains(output, "/Users/testuser/.aws/config") {
		t.Error("expected ~/.aws/config to still be present")
	}
}

func TestGuard_GCPEnvOverride(t *testing.T) {
	g := modules.CloudGCPGuard()
	ctx := &seatbelt.Context{
		HomeDir: "/Users/testuser",
		Env:     []string{"CLOUDSDK_CONFIG=/custom/gcloud"},
	}
	output := renderTestRules(g.Rules(ctx))

	if !strings.Contains(output, "/custom/gcloud") {
		t.Error("expected /custom/gcloud from CLOUDSDK_CONFIG override")
	}
	if strings.Contains(output, "/Users/testuser/.config/gcloud") {
		t.Error("default gcloud path should be replaced")
	}
}

func TestGuard_CloudAzure_Default(t *testing.T) {
	g := modules.CloudAzureGuard()
	if g.Type() != "default" {
		t.Errorf("expected Type() = %q, got %q", "default", g.Type())
	}
	ctx := &seatbelt.Context{HomeDir: "/Users/testuser"}
	output := renderTestRules(g.Rules(ctx))
	if !strings.Contains(output, "/Users/testuser/.azure") {
		t.Error("expected ~/.azure in output")
	}
}

func TestGuard_CloudDigitalOcean_Default(t *testing.T) {
	g := modules.CloudDigitalOceanGuard()
	ctx := &seatbelt.Context{HomeDir: "/Users/testuser"}
	output := renderTestRules(g.Rules(ctx))
	if !strings.Contains(output, "/Users/testuser/.config/doctl") {
		t.Error("expected ~/.config/doctl in output")
	}
}

func TestGuard_CloudOCI_Default(t *testing.T) {
	g := modules.CloudOCIGuard()
	ctx := &seatbelt.Context{HomeDir: "/Users/testuser"}
	output := renderTestRules(g.Rules(ctx))
	if !strings.Contains(output, "/Users/testuser/.oci") {
		t.Error("expected ~/.oci in output")
	}
}

func TestGuard_Kubernetes_Default(t *testing.T) {
	g := modules.KubernetesGuard()
	if g.Name() != "kubernetes" {
		t.Errorf("expected Name() = %q, got %q", "kubernetes", g.Name())
	}
	ctx := &seatbelt.Context{HomeDir: "/Users/testuser"}
	output := renderTestRules(g.Rules(ctx))
	if !strings.Contains(output, "/Users/testuser/.kube/config") {
		t.Error("expected ~/.kube/config in output")
	}
}

func TestGuard_Kubernetes_ColonSeparatedKubeconfig(t *testing.T) {
	g := modules.KubernetesGuard()
	ctx := &seatbelt.Context{
		HomeDir: "/Users/testuser",
		Env:     []string{"KUBECONFIG=/path/a:/path/b:/path/c"},
	}
	output := renderTestRules(g.Rules(ctx))

	for _, p := range []string{"/path/a", "/path/b", "/path/c"} {
		if !strings.Contains(output, p) {
			t.Errorf("expected output to contain KUBECONFIG path %q", p)
		}
	}
	if strings.Contains(output, "/Users/testuser/.kube/config") {
		t.Error("default path should be replaced when KUBECONFIG is set")
	}
}

func TestGuard_Terraform_Default(t *testing.T) {
	g := modules.TerraformGuard()
	ctx := &seatbelt.Context{HomeDir: "/Users/testuser"}
	output := renderTestRules(g.Rules(ctx))
	if !strings.Contains(output, "/Users/testuser/.terraform.d/credentials.tfrc.json") {
		t.Error("expected terraform credentials path")
	}
	if !strings.Contains(output, "/Users/testuser/.terraformrc") {
		t.Error("expected .terraformrc path")
	}
}

func TestGuard_Vault_Default(t *testing.T) {
	g := modules.VaultGuard()
	ctx := &seatbelt.Context{HomeDir: "/Users/testuser"}
	output := renderTestRules(g.Rules(ctx))
	if !strings.Contains(output, "/Users/testuser/.vault-token") {
		t.Error("expected .vault-token path")
	}
}
```

Run: `go test ./pkg/seatbelt/modules/ -run "TestGuard_Cloud|TestGuard_Kubernetes|TestGuard_Terraform|TestGuard_Vault" -v`

Expected: FAIL

- [ ] **Step 2: Implement guard_cloud.go**

Create `pkg/seatbelt/modules/guard_cloud.go`:

```go
// Cloud provider guards for macOS Seatbelt profiles.

package modules

import (
	"fmt"
	"strings"

	"github.com/jskswamy/aide/pkg/seatbelt"
)

// denyPathRules produces deny rules for a list of paths.
func denyPathRules(paths []string) []seatbelt.Rule {
	var rules []seatbelt.Rule
	for _, p := range paths {
		rules = append(rules,
			seatbelt.Raw(fmt.Sprintf(`(deny file-read-data (subpath "%s"))`, p)),
			seatbelt.Raw(fmt.Sprintf(`(deny file-write* (subpath "%s"))`, p)),
		)
	}
	return rules
}

// denyLiteralRules produces deny rules using literal (for files, not dirs).
func denyLiteralRules(paths []string) []seatbelt.Rule {
	var rules []seatbelt.Rule
	for _, p := range paths {
		rules = append(rules,
			seatbelt.Raw(fmt.Sprintf(`(deny file-read-data (literal "%s"))`, p)),
			seatbelt.Raw(fmt.Sprintf(`(deny file-write* (literal "%s"))`, p)),
		)
	}
	return rules
}

// envOverridePath returns the env var value if set and non-empty,
// otherwise returns the default path.
func envOverridePath(ctx *seatbelt.Context, envKey, defaultPath string) string {
	if val, ok := ctx.EnvLookup(envKey); ok && val != "" {
		return val
	}
	return defaultPath
}

// --- cloud-aws ---

type cloudAWSGuard struct{}

func CloudAWSGuard() seatbelt.Guard { return &cloudAWSGuard{} }

func (m *cloudAWSGuard) Name() string        { return "cloud-aws" }
func (m *cloudAWSGuard) Type() string        { return "default" }
func (m *cloudAWSGuard) Description() string { return "AWS credentials and config" }

func (m *cloudAWSGuard) Rules(ctx *seatbelt.Context) []seatbelt.Rule {
	home := ctx.HomeDir
	credsPath := envOverridePath(ctx, "AWS_SHARED_CREDENTIALS_FILE", ctx.HomePath(".aws/credentials"))
	configPath := envOverridePath(ctx, "AWS_CONFIG_FILE", ctx.HomePath(".aws/config"))

	paths := []string{
		credsPath,
		configPath,
	}
	// sso/cache and cli/cache always use default location
	paths = append(paths,
		fmt.Sprintf("%s/.aws/sso/cache", home),
		fmt.Sprintf("%s/.aws/cli/cache", home),
	)

	return denyPathRules(paths)
}

// --- cloud-gcp ---

type cloudGCPGuard struct{}

func CloudGCPGuard() seatbelt.Guard { return &cloudGCPGuard{} }

func (m *cloudGCPGuard) Name() string        { return "cloud-gcp" }
func (m *cloudGCPGuard) Type() string        { return "default" }
func (m *cloudGCPGuard) Description() string { return "GCP credentials and config" }

func (m *cloudGCPGuard) Rules(ctx *seatbelt.Context) []seatbelt.Rule {
	gcloudPath := envOverridePath(ctx, "CLOUDSDK_CONFIG", ctx.HomePath(".config/gcloud"))

	var paths []string
	paths = append(paths, gcloudPath)

	// GOOGLE_APPLICATION_CREDENTIALS points to a single file
	if val, ok := ctx.EnvLookup("GOOGLE_APPLICATION_CREDENTIALS"); ok && val != "" {
		return append(denyPathRules(paths), denyLiteralRules([]string{val})...)
	}
	return denyPathRules(paths)
}

// --- cloud-azure ---

type cloudAzureGuard struct{}

func CloudAzureGuard() seatbelt.Guard { return &cloudAzureGuard{} }

func (m *cloudAzureGuard) Name() string        { return "cloud-azure" }
func (m *cloudAzureGuard) Type() string        { return "default" }
func (m *cloudAzureGuard) Description() string { return "Azure CLI config and credentials" }

func (m *cloudAzureGuard) Rules(ctx *seatbelt.Context) []seatbelt.Rule {
	azurePath := envOverridePath(ctx, "AZURE_CONFIG_DIR", ctx.HomePath(".azure"))
	return denyPathRules([]string{azurePath})
}

// --- cloud-digitalocean ---

type cloudDigitalOceanGuard struct{}

func CloudDigitalOceanGuard() seatbelt.Guard { return &cloudDigitalOceanGuard{} }

func (m *cloudDigitalOceanGuard) Name() string        { return "cloud-digitalocean" }
func (m *cloudDigitalOceanGuard) Type() string        { return "default" }
func (m *cloudDigitalOceanGuard) Description() string { return "DigitalOcean CLI config" }

func (m *cloudDigitalOceanGuard) Rules(ctx *seatbelt.Context) []seatbelt.Rule {
	return denyPathRules([]string{ctx.HomePath(".config/doctl")})
}

// --- cloud-oci ---

type cloudOCIGuard struct{}

func CloudOCIGuard() seatbelt.Guard { return &cloudOCIGuard{} }

func (m *cloudOCIGuard) Name() string        { return "cloud-oci" }
func (m *cloudOCIGuard) Type() string        { return "default" }
func (m *cloudOCIGuard) Description() string { return "Oracle Cloud CLI config" }

func (m *cloudOCIGuard) Rules(ctx *seatbelt.Context) []seatbelt.Rule {
	ociPath := envOverridePath(ctx, "OCI_CLI_CONFIG_FILE", ctx.HomePath(".oci"))
	return denyPathRules([]string{ociPath})
}

// --- meta-guard expansion helpers ---

// CloudGuardNames returns the names of all cloud guards (for "cloud" meta-guard).
func CloudGuardNames() []string {
	return []string{"cloud-aws", "cloud-gcp", "cloud-azure", "cloud-digitalocean", "cloud-oci"}
}

// splitColonPaths splits a colon-separated path string into individual paths.
func splitColonPaths(s string) []string {
	parts := strings.Split(s, ":")
	var result []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}
```

- [ ] **Step 3: Implement guard_kubernetes.go**

Create `pkg/seatbelt/modules/guard_kubernetes.go`:

```go
// Kubernetes guard for macOS Seatbelt profiles.

package modules

import "github.com/jskswamy/aide/pkg/seatbelt"

type kubernetesGuard struct{}

func KubernetesGuard() seatbelt.Guard { return &kubernetesGuard{} }

func (m *kubernetesGuard) Name() string        { return "kubernetes" }
func (m *kubernetesGuard) Type() string        { return "default" }
func (m *kubernetesGuard) Description() string { return "Kubernetes config" }

func (m *kubernetesGuard) Rules(ctx *seatbelt.Context) []seatbelt.Rule {
	if val, ok := ctx.EnvLookup("KUBECONFIG"); ok && val != "" {
		paths := splitColonPaths(val)
		return denyLiteralRules(paths)
	}
	return denyLiteralRules([]string{ctx.HomePath(".kube/config")})
}
```

- [ ] **Step 4: Implement guard_terraform.go**

Create `pkg/seatbelt/modules/guard_terraform.go`:

```go
// Terraform guard for macOS Seatbelt profiles.

package modules

import "github.com/jskswamy/aide/pkg/seatbelt"

type terraformGuard struct{}

func TerraformGuard() seatbelt.Guard { return &terraformGuard{} }

func (m *terraformGuard) Name() string        { return "terraform" }
func (m *terraformGuard) Type() string        { return "default" }
func (m *terraformGuard) Description() string { return "Terraform credentials" }

func (m *terraformGuard) Rules(ctx *seatbelt.Context) []seatbelt.Rule {
	tfConfig := envOverridePath(ctx, "TF_CLI_CONFIG_FILE", ctx.HomePath(".terraform.d/credentials.tfrc.json"))
	paths := []string{tfConfig}
	// .terraformrc is always at default location
	paths = append(paths, ctx.HomePath(".terraformrc"))
	return denyLiteralRules(paths)
}
```

- [ ] **Step 5: Implement guard_vault.go**

Create `pkg/seatbelt/modules/guard_vault.go`:

```go
// Vault guard for macOS Seatbelt profiles.

package modules

import "github.com/jskswamy/aide/pkg/seatbelt"

type vaultGuard struct{}

func VaultGuard() seatbelt.Guard { return &vaultGuard{} }

func (m *vaultGuard) Name() string        { return "vault" }
func (m *vaultGuard) Type() string        { return "default" }
func (m *vaultGuard) Description() string { return "HashiCorp Vault token" }

func (m *vaultGuard) Rules(ctx *seatbelt.Context) []seatbelt.Rule {
	tokenPath := envOverridePath(ctx, "VAULT_TOKEN_FILE", ctx.HomePath(".vault-token"))
	return denyLiteralRules([]string{tokenPath})
}
```

Run: `go test ./pkg/seatbelt/modules/ -run "TestGuard_Cloud|TestGuard_Kubernetes|TestGuard_Terraform|TestGuard_Vault" -v`

Expected: PASS

- [ ] **Step 6: Commit**

```
/commit
```

---

### Task 12: Create browsers guard (NEW)

OS-aware browser path deny rules using `ctx.GOOS`.

**Files:**
- Create: `pkg/seatbelt/modules/guard_browsers.go`
- Create: `pkg/seatbelt/modules/guard_browsers_test.go`

- [ ] **Step 1: Write failing tests**

Create `pkg/seatbelt/modules/guard_browsers_test.go`:

```go
package modules_test

import (
	"strings"
	"testing"

	"github.com/jskswamy/aide/pkg/seatbelt"
	"github.com/jskswamy/aide/pkg/seatbelt/modules"
)

func TestGuard_Browsers_Metadata(t *testing.T) {
	g := modules.BrowsersGuard()
	if g.Name() != "browsers" {
		t.Errorf("expected Name() = %q, got %q", "browsers", g.Name())
	}
	if g.Type() != "default" {
		t.Errorf("expected Type() = %q, got %q", "default", g.Type())
	}
}

func TestGuard_Browsers_Darwin(t *testing.T) {
	g := modules.BrowsersGuard()
	ctx := &seatbelt.Context{
		HomeDir: "/Users/testuser",
		GOOS:    "darwin",
	}
	output := renderTestRules(g.Rules(ctx))

	darwinPaths := []string{
		"Library/Application Support/Google/Chrome",
		"Library/Application Support/Firefox",
		"Library/Safari",
		"Library/Application Support/BraveSoftware",
		"Library/Application Support/Microsoft Edge",
		"Library/Application Support/Arc",
		"Library/Application Support/Chromium",
	}
	for _, p := range darwinPaths {
		if !strings.Contains(output, p) {
			t.Errorf("expected darwin output to contain %q", p)
		}
	}

	// Should NOT contain linux paths
	if strings.Contains(output, ".config/google-chrome") {
		t.Error("darwin should not contain linux Chrome path")
	}
}

func TestGuard_Browsers_Linux(t *testing.T) {
	g := modules.BrowsersGuard()
	ctx := &seatbelt.Context{
		HomeDir: "/home/testuser",
		GOOS:    "linux",
	}
	output := renderTestRules(g.Rules(ctx))

	linuxPaths := []string{
		".config/google-chrome",
		".mozilla/firefox",
		".config/BraveSoftware",
		".config/microsoft-edge",
		".config/chromium",
		"snap/chromium",
	}
	for _, p := range linuxPaths {
		if !strings.Contains(output, p) {
			t.Errorf("expected linux output to contain %q", p)
		}
	}

	// Should NOT contain Safari (macOS only)
	if strings.Contains(output, "Library/Safari") {
		t.Error("linux should not contain Safari path")
	}
}
```

Run: `go test ./pkg/seatbelt/modules/ -run TestGuard_Browsers -v`

Expected: FAIL

- [ ] **Step 2: Implement guard_browsers.go**

Create `pkg/seatbelt/modules/guard_browsers.go`:

```go
// Browsers guard for macOS Seatbelt profiles.
//
// Denies browser data directories. OS-aware via ctx.GOOS.

package modules

import "github.com/jskswamy/aide/pkg/seatbelt"

type browsersGuard struct{}

// BrowsersGuard returns the browsers guard.
func BrowsersGuard() seatbelt.Guard { return &browsersGuard{} }

func (m *browsersGuard) Name() string        { return "browsers" }
func (m *browsersGuard) Type() string        { return "default" }
func (m *browsersGuard) Description() string { return "Browser data directories (Chrome, Firefox, Safari, Brave, Edge, Arc, Chromium)" }

func (m *browsersGuard) Rules(ctx *seatbelt.Context) []seatbelt.Rule {
	var paths []string

	switch ctx.GOOS {
	case "darwin":
		paths = []string{
			ctx.HomePath("Library/Application Support/Google/Chrome"),
			ctx.HomePath("Library/Application Support/Firefox"),
			ctx.HomePath("Library/Safari"),
			ctx.HomePath("Library/Application Support/BraveSoftware"),
			ctx.HomePath("Library/Application Support/Microsoft Edge"),
			ctx.HomePath("Library/Application Support/Arc"),
			ctx.HomePath("Library/Application Support/Chromium"),
		}
	case "linux":
		paths = []string{
			ctx.HomePath(".config/google-chrome"),
			ctx.HomePath(".mozilla/firefox"),
			ctx.HomePath(".config/BraveSoftware"),
			ctx.HomePath(".config/microsoft-edge"),
			ctx.HomePath(".config/chromium"),
			ctx.HomePath("snap/chromium"),
		}
	}

	return denyPathRules(paths)
}
```

Run: `go test ./pkg/seatbelt/modules/ -run TestGuard_Browsers -v`

Expected: PASS

- [ ] **Step 3: Commit**

```
/commit
```

---

### Task 13: Create password-managers + aide-secrets guards (NEW)

**Files:**
- Create: `pkg/seatbelt/modules/guard_password_managers.go`
- Create: `pkg/seatbelt/modules/guard_aide_secrets.go`
- Create: `pkg/seatbelt/modules/guard_password_managers_test.go`

- [ ] **Step 1: Write failing tests**

Create `pkg/seatbelt/modules/guard_password_managers_test.go`:

```go
package modules_test

import (
	"strings"
	"testing"

	"github.com/jskswamy/aide/pkg/seatbelt"
	"github.com/jskswamy/aide/pkg/seatbelt/modules"
)

func TestGuard_PasswordManagers_Metadata(t *testing.T) {
	g := modules.PasswordManagersGuard()
	if g.Name() != "password-managers" {
		t.Errorf("expected Name() = %q, got %q", "password-managers", g.Name())
	}
	if g.Type() != "default" {
		t.Errorf("expected Type() = %q, got %q", "default", g.Type())
	}
}

func TestGuard_PasswordManagers_Paths(t *testing.T) {
	g := modules.PasswordManagersGuard()
	ctx := &seatbelt.Context{HomeDir: "/Users/testuser"}
	output := renderTestRules(g.Rules(ctx))

	expectedPaths := []string{
		".config/op",
		".op",
		".config/Bitwarden CLI",
		".password-store",
		".config/gopass",
		".gnupg/private-keys-v1.d",
		".gnupg/secring.gpg",
	}
	for _, p := range expectedPaths {
		if !strings.Contains(output, p) {
			t.Errorf("expected output to contain %q", p)
		}
	}
}

func TestGuard_PasswordManagers_NoKeychains(t *testing.T) {
	g := modules.PasswordManagersGuard()
	ctx := &seatbelt.Context{HomeDir: "/Users/testuser"}
	output := renderTestRules(g.Rules(ctx))

	if strings.Contains(output, "Library/Keychains") {
		t.Error("password-managers guard must NOT include ~/Library/Keychains (managed by keychain guard)")
	}
}

func TestGuard_AideSecrets_Metadata(t *testing.T) {
	g := modules.AideSecretsGuard()
	if g.Name() != "aide-secrets" {
		t.Errorf("expected Name() = %q, got %q", "aide-secrets", g.Name())
	}
	if g.Type() != "default" {
		t.Errorf("expected Type() = %q, got %q", "default", g.Type())
	}
}

func TestGuard_AideSecrets_Paths(t *testing.T) {
	g := modules.AideSecretsGuard()
	ctx := &seatbelt.Context{HomeDir: "/Users/testuser"}
	output := renderTestRules(g.Rules(ctx))

	if !strings.Contains(output, ".config/aide/secrets") {
		t.Error("expected aide-secrets to deny ~/.config/aide/secrets")
	}
}
```

Run: `go test ./pkg/seatbelt/modules/ -run "TestGuard_PasswordManagers|TestGuard_AideSecrets" -v`

Expected: FAIL

- [ ] **Step 2: Implement guard_password_managers.go**

Create `pkg/seatbelt/modules/guard_password_managers.go`:

```go
// Password managers guard for macOS Seatbelt profiles.
//
// Denies CLI password store paths only.
// Does NOT include ~/Library/Keychains — that's managed by the keychain guard.

package modules

import "github.com/jskswamy/aide/pkg/seatbelt"

type passwordManagersGuard struct{}

// PasswordManagersGuard returns the password-managers guard.
func PasswordManagersGuard() seatbelt.Guard { return &passwordManagersGuard{} }

func (m *passwordManagersGuard) Name() string        { return "password-managers" }
func (m *passwordManagersGuard) Type() string        { return "default" }
func (m *passwordManagersGuard) Description() string { return "1Password CLI, Bitwarden CLI, pass, gopass, GPG private keys" }

func (m *passwordManagersGuard) Rules(ctx *seatbelt.Context) []seatbelt.Rule {
	paths := []string{
		ctx.HomePath(".config/op"),
		ctx.HomePath(".op"),
		ctx.HomePath(".config/Bitwarden CLI"),
		ctx.HomePath(".password-store"),
		ctx.HomePath(".config/gopass"),
		ctx.HomePath(".gnupg/private-keys-v1.d"),
	}
	rules := denyPathRules(paths)
	// secring.gpg is a file, use literal
	rules = append(rules, denyLiteralRules([]string{ctx.HomePath(".gnupg/secring.gpg")})...)
	return rules
}
```

- [ ] **Step 3: Implement guard_aide_secrets.go**

Create `pkg/seatbelt/modules/guard_aide_secrets.go`:

```go
// Aide secrets guard for macOS Seatbelt profiles.

package modules

import "github.com/jskswamy/aide/pkg/seatbelt"

type aideSecretsGuard struct{}

// AideSecretsGuard returns the aide-secrets guard.
func AideSecretsGuard() seatbelt.Guard { return &aideSecretsGuard{} }

func (m *aideSecretsGuard) Name() string        { return "aide-secrets" }
func (m *aideSecretsGuard) Type() string        { return "default" }
func (m *aideSecretsGuard) Description() string { return "aide encrypted secrets store" }

func (m *aideSecretsGuard) Rules(ctx *seatbelt.Context) []seatbelt.Rule {
	return denyPathRules([]string{ctx.HomePath(".config/aide/secrets")})
}
```

Run: `go test ./pkg/seatbelt/modules/ -run "TestGuard_PasswordManagers|TestGuard_AideSecrets" -v`

Expected: PASS

- [ ] **Step 4: Commit**

```
/commit
```

---

### Task 14: Create sensitive guards (NEW)

Opt-in guards for docker, github-cli, npm, netrc, vercel.

**Files:**
- Create: `pkg/seatbelt/modules/guard_sensitive.go`
- Create: `pkg/seatbelt/modules/guard_sensitive_test.go`

- [ ] **Step 1: Write failing tests**

Create `pkg/seatbelt/modules/guard_sensitive_test.go`:

```go
package modules_test

import (
	"strings"
	"testing"

	"github.com/jskswamy/aide/pkg/seatbelt"
	"github.com/jskswamy/aide/pkg/seatbelt/modules"
)

func TestGuard_Docker(t *testing.T) {
	g := modules.DockerGuard()
	if g.Name() != "docker" {
		t.Errorf("expected Name() = %q, got %q", "docker", g.Name())
	}
	if g.Type() != "opt-in" {
		t.Errorf("expected Type() = %q, got %q", "opt-in", g.Type())
	}
	ctx := &seatbelt.Context{HomeDir: "/Users/testuser"}
	output := renderTestRules(g.Rules(ctx))
	if !strings.Contains(output, ".docker/config.json") {
		t.Error("expected .docker/config.json")
	}
}

func TestGuard_Docker_EnvOverride(t *testing.T) {
	g := modules.DockerGuard()
	ctx := &seatbelt.Context{
		HomeDir: "/Users/testuser",
		Env:     []string{"DOCKER_CONFIG=/custom/docker"},
	}
	output := renderTestRules(g.Rules(ctx))
	if !strings.Contains(output, "/custom/docker") {
		t.Error("expected custom docker config path")
	}
}

func TestGuard_GithubCLI(t *testing.T) {
	g := modules.GithubCLIGuard()
	if g.Name() != "github-cli" {
		t.Errorf("expected Name() = %q, got %q", "github-cli", g.Name())
	}
	if g.Type() != "opt-in" {
		t.Errorf("expected Type() = %q, got %q", "opt-in", g.Type())
	}
	ctx := &seatbelt.Context{HomeDir: "/Users/testuser"}
	output := renderTestRules(g.Rules(ctx))
	if !strings.Contains(output, ".config/gh") {
		t.Error("expected .config/gh")
	}
}

func TestGuard_NPM(t *testing.T) {
	g := modules.NPMGuard()
	if g.Name() != "npm" {
		t.Errorf("expected Name() = %q, got %q", "npm", g.Name())
	}
	if g.Type() != "opt-in" {
		t.Errorf("expected Type() = %q, got %q", "opt-in", g.Type())
	}
	ctx := &seatbelt.Context{HomeDir: "/Users/testuser"}
	output := renderTestRules(g.Rules(ctx))
	if !strings.Contains(output, ".npmrc") {
		t.Error("expected .npmrc")
	}
	if !strings.Contains(output, ".yarnrc") {
		t.Error("expected .yarnrc")
	}
}

func TestGuard_Netrc(t *testing.T) {
	g := modules.NetrcGuard()
	if g.Type() != "opt-in" {
		t.Errorf("expected Type() = %q, got %q", "opt-in", g.Type())
	}
	ctx := &seatbelt.Context{HomeDir: "/Users/testuser"}
	output := renderTestRules(g.Rules(ctx))
	if !strings.Contains(output, ".netrc") {
		t.Error("expected .netrc")
	}
}

func TestGuard_Vercel(t *testing.T) {
	g := modules.VercelGuard()
	if g.Type() != "opt-in" {
		t.Errorf("expected Type() = %q, got %q", "opt-in", g.Type())
	}
	ctx := &seatbelt.Context{HomeDir: "/Users/testuser"}
	output := renderTestRules(g.Rules(ctx))
	if !strings.Contains(output, ".config/vercel") {
		t.Error("expected .config/vercel")
	}
}
```

Run: `go test ./pkg/seatbelt/modules/ -run "TestGuard_Docker|TestGuard_GithubCLI|TestGuard_NPM|TestGuard_Netrc|TestGuard_Vercel" -v`

Expected: FAIL

- [ ] **Step 2: Implement guard_sensitive.go**

Create `pkg/seatbelt/modules/guard_sensitive.go`:

```go
// Sensitive opt-in guards for macOS Seatbelt profiles.

package modules

import (
	"fmt"

	"github.com/jskswamy/aide/pkg/seatbelt"
)

// --- docker ---

type dockerGuard struct{}

func DockerGuard() seatbelt.Guard { return &dockerGuard{} }

func (m *dockerGuard) Name() string        { return "docker" }
func (m *dockerGuard) Type() string        { return "opt-in" }
func (m *dockerGuard) Description() string { return "Docker config (auth tokens)" }

func (m *dockerGuard) Rules(ctx *seatbelt.Context) []seatbelt.Rule {
	dockerDir := envOverridePath(ctx, "DOCKER_CONFIG", ctx.HomePath(".docker"))
	configFile := fmt.Sprintf("%s/config.json", dockerDir)
	return denyLiteralRules([]string{configFile})
}

// --- github-cli ---

type githubCLIGuard struct{}

func GithubCLIGuard() seatbelt.Guard { return &githubCLIGuard{} }

func (m *githubCLIGuard) Name() string        { return "github-cli" }
func (m *githubCLIGuard) Type() string        { return "opt-in" }
func (m *githubCLIGuard) Description() string { return "GitHub CLI credentials" }

func (m *githubCLIGuard) Rules(ctx *seatbelt.Context) []seatbelt.Rule {
	return denyPathRules([]string{ctx.HomePath(".config/gh")})
}

// --- npm ---

type npmGuard struct{}

func NPMGuard() seatbelt.Guard { return &npmGuard{} }

func (m *npmGuard) Name() string        { return "npm" }
func (m *npmGuard) Type() string        { return "opt-in" }
func (m *npmGuard) Description() string { return "npm/yarn auth tokens" }

func (m *npmGuard) Rules(ctx *seatbelt.Context) []seatbelt.Rule {
	return denyLiteralRules([]string{
		ctx.HomePath(".npmrc"),
		ctx.HomePath(".yarnrc"),
	})
}

// --- netrc ---

type netrcGuard struct{}

func NetrcGuard() seatbelt.Guard { return &netrcGuard{} }

func (m *netrcGuard) Name() string        { return "netrc" }
func (m *netrcGuard) Type() string        { return "opt-in" }
func (m *netrcGuard) Description() string { return "~/.netrc credentials" }

func (m *netrcGuard) Rules(ctx *seatbelt.Context) []seatbelt.Rule {
	return denyLiteralRules([]string{ctx.HomePath(".netrc")})
}

// --- vercel ---

type vercelGuard struct{}

func VercelGuard() seatbelt.Guard { return &vercelGuard{} }

func (m *vercelGuard) Name() string        { return "vercel" }
func (m *vercelGuard) Type() string        { return "opt-in" }
func (m *vercelGuard) Description() string { return "Vercel CLI credentials" }

func (m *vercelGuard) Rules(ctx *seatbelt.Context) []seatbelt.Rule {
	return denyPathRules([]string{ctx.HomePath(".config/vercel")})
}
```

Run: `go test ./pkg/seatbelt/modules/ -run "TestGuard_Docker|TestGuard_GithubCLI|TestGuard_NPM|TestGuard_Netrc|TestGuard_Vercel" -v`

Expected: PASS

- [ ] **Step 3: Commit**

```
/commit
```

---

### Task 15: Guard registry

Central registry that maps guard names to implementations, supports meta-guard expansion, and resolves active guard sets.

**Files:**
- Create: `pkg/seatbelt/modules/registry.go`
- Create: `pkg/seatbelt/modules/registry_test.go`

- [ ] **Step 1: Write failing tests**

Create `pkg/seatbelt/modules/registry_test.go`:

```go
package modules_test

import (
	"testing"

	"github.com/jskswamy/aide/pkg/seatbelt/modules"
)

func TestRegistry_AllGuards(t *testing.T) {
	guards := modules.AllGuards()
	if len(guards) == 0 {
		t.Fatal("expected at least one guard")
	}

	// Verify all built-in guards are registered
	expectedNames := []string{
		"base", "system-runtime", "network", "filesystem", "keychain",
		"node-toolchain", "nix-toolchain", "git-integration",
		"ssh-keys", "cloud-aws", "cloud-gcp", "cloud-azure",
		"cloud-digitalocean", "cloud-oci", "kubernetes", "terraform",
		"vault", "browsers", "password-managers", "aide-secrets",
		"docker", "github-cli", "npm", "netrc", "vercel",
	}
	guardMap := make(map[string]bool)
	for _, g := range guards {
		guardMap[g.Name()] = true
	}
	for _, name := range expectedNames {
		if !guardMap[name] {
			t.Errorf("expected guard %q in AllGuards()", name)
		}
	}
}

func TestRegistry_GuardByName(t *testing.T) {
	g, ok := modules.GuardByName("ssh-keys")
	if !ok {
		t.Fatal("expected to find ssh-keys guard")
	}
	if g.Name() != "ssh-keys" {
		t.Errorf("expected Name() = %q, got %q", "ssh-keys", g.Name())
	}

	_, ok = modules.GuardByName("nonexistent")
	if ok {
		t.Error("expected nonexistent guard to return false")
	}
}

func TestRegistry_GuardsByType(t *testing.T) {
	always := modules.GuardsByType("always")
	if len(always) == 0 {
		t.Error("expected at least one always guard")
	}
	for _, g := range always {
		if g.Type() != "always" {
			t.Errorf("GuardsByType(always) returned guard %q with type %q", g.Name(), g.Type())
		}
	}

	defaults := modules.GuardsByType("default")
	if len(defaults) == 0 {
		t.Error("expected at least one default guard")
	}

	optIn := modules.GuardsByType("opt-in")
	if len(optIn) == 0 {
		t.Error("expected at least one opt-in guard")
	}
}

func TestRegistry_ExpandGuardName_Cloud(t *testing.T) {
	names := modules.ExpandGuardName("cloud")
	expected := []string{"cloud-aws", "cloud-gcp", "cloud-azure", "cloud-digitalocean", "cloud-oci"}
	if len(names) != len(expected) {
		t.Fatalf("expected %d names, got %d", len(expected), len(names))
	}
	for i, n := range names {
		if n != expected[i] {
			t.Errorf("expected %q at index %d, got %q", expected[i], i, n)
		}
	}
}

func TestRegistry_ExpandGuardName_AllDefault(t *testing.T) {
	names := modules.ExpandGuardName("all-default")
	if len(names) == 0 {
		t.Fatal("expected all-default to expand to at least one guard")
	}
	for _, n := range names {
		g, ok := modules.GuardByName(n)
		if !ok {
			t.Errorf("expanded name %q not found in registry", n)
			continue
		}
		if g.Type() != "default" {
			t.Errorf("all-default expanded to %q which has type %q", n, g.Type())
		}
	}
}

func TestRegistry_ExpandGuardName_Regular(t *testing.T) {
	names := modules.ExpandGuardName("ssh-keys")
	if len(names) != 1 || names[0] != "ssh-keys" {
		t.Errorf("expected [ssh-keys], got %v", names)
	}
}

func TestRegistry_DefaultGuardNames(t *testing.T) {
	names := modules.DefaultGuardNames()
	if len(names) == 0 {
		t.Fatal("expected at least one default guard name")
	}

	// Should include all always guards
	nameSet := make(map[string]bool)
	for _, n := range names {
		nameSet[n] = true
	}
	if !nameSet["base"] {
		t.Error("DefaultGuardNames should include 'base' (always)")
	}
	if !nameSet["ssh-keys"] {
		t.Error("DefaultGuardNames should include 'ssh-keys' (default)")
	}
	// Should NOT include opt-in guards
	if nameSet["docker"] {
		t.Error("DefaultGuardNames should NOT include 'docker' (opt-in)")
	}
}

func TestRegistry_ResolveActiveGuards(t *testing.T) {
	guards := modules.ResolveActiveGuards([]string{"base", "ssh-keys", "docker"})
	if len(guards) != 3 {
		t.Fatalf("expected 3 guards, got %d", len(guards))
	}

	// Verify ordering: always first, then default, then opt-in
	if guards[0].Type() != "always" {
		t.Errorf("first guard should be always, got %q (%s)", guards[0].Name(), guards[0].Type())
	}
}

func TestRegistry_ResolveActiveGuards_SkipsUnknown(t *testing.T) {
	guards := modules.ResolveActiveGuards([]string{"base", "nonexistent"})
	if len(guards) != 1 {
		t.Errorf("expected 1 guard (unknown skipped), got %d", len(guards))
	}
}
```

Run: `go test ./pkg/seatbelt/modules/ -run TestRegistry -v`

Expected: FAIL

- [ ] **Step 2: Implement registry.go**

Create `pkg/seatbelt/modules/registry.go`:

```go
// Guard registry maps guard names to implementations.

package modules

import "github.com/jskswamy/aide/pkg/seatbelt"

// builtinGuards is the ordered list of all built-in guards.
// Order: always, default, opt-in. Within each type, order is registration order.
var builtinGuards []seatbelt.Guard

func init() {
	builtinGuards = []seatbelt.Guard{
		// always
		BaseGuard(),
		SystemRuntimeGuard(),
		NetworkGuard(),
		FilesystemGuard(),
		KeychainGuard(),
		NodeToolchainGuard(),
		NixToolchainGuard(),
		GitIntegrationGuard(),
		// default
		SSHKeysGuard(),
		CloudAWSGuard(),
		CloudGCPGuard(),
		CloudAzureGuard(),
		CloudDigitalOceanGuard(),
		CloudOCIGuard(),
		KubernetesGuard(),
		TerraformGuard(),
		VaultGuard(),
		BrowsersGuard(),
		PasswordManagersGuard(),
		AideSecretsGuard(),
		// opt-in
		DockerGuard(),
		GithubCLIGuard(),
		NPMGuard(),
		NetrcGuard(),
		VercelGuard(),
	}
}

// metaGuards maps meta-guard names to expansion functions.
var metaGuards = map[string]func() []string{
	"cloud": CloudGuardNames,
	"all-default": func() []string {
		var names []string
		for _, g := range builtinGuards {
			if g.Type() == "default" {
				names = append(names, g.Name())
			}
		}
		return names
	},
}

// AllGuards returns all built-in guards in registration order.
func AllGuards() []seatbelt.Guard {
	result := make([]seatbelt.Guard, len(builtinGuards))
	copy(result, builtinGuards)
	return result
}

// GuardByName looks up a guard by name. Returns (nil, false) if not found.
func GuardByName(name string) (seatbelt.Guard, bool) {
	for _, g := range builtinGuards {
		if g.Name() == name {
			return g, true
		}
	}
	return nil, false
}

// GuardsByType returns all guards matching the given type.
func GuardsByType(typ string) []seatbelt.Guard {
	var result []seatbelt.Guard
	for _, g := range builtinGuards {
		if g.Type() == typ {
			result = append(result, g)
		}
	}
	return result
}

// ExpandGuardName expands meta-guard names. Non-meta names return a
// single-element slice.
func ExpandGuardName(name string) []string {
	if fn, ok := metaGuards[name]; ok {
		return fn()
	}
	return []string{name}
}

// DefaultGuardNames returns names of all "always" + all "default" guards.
func DefaultGuardNames() []string {
	var names []string
	for _, g := range builtinGuards {
		if g.Type() == "always" || g.Type() == "default" {
			names = append(names, g.Name())
		}
	}
	return names
}

// typeOrder defines rendering order for guard types.
var typeOrder = map[string]int{
	"always":  0,
	"default": 1,
	"opt-in":  2,
}

// ResolveActiveGuards looks up guards by name and returns them ordered
// by type (always first, then default, then opt-in).
func ResolveActiveGuards(names []string) []seatbelt.Guard {
	var guards []seatbelt.Guard
	for _, name := range names {
		if g, ok := GuardByName(name); ok {
			guards = append(guards, g)
		}
	}

	// Stable sort by type order
	sortGuardsByType(guards)
	return guards
}

// sortGuardsByType sorts guards by type order using insertion sort (stable).
func sortGuardsByType(guards []seatbelt.Guard) {
	for i := 1; i < len(guards); i++ {
		key := guards[i]
		j := i - 1
		for j >= 0 && typeOrder[guards[j].Type()] > typeOrder[key.Type()] {
			guards[j+1] = guards[j]
			j--
		}
		guards[j+1] = key
	}
}
```

Run: `go test ./pkg/seatbelt/modules/ -run TestRegistry -v`

Expected: PASS

- [ ] **Step 3: Commit**

```
/commit
```

---

### Task 16: Custom guard support

Dynamic guards from config. Custom guards produce deny rules for paths and allow rules for allowed entries.

**Files:**
- Create: `pkg/seatbelt/modules/guard_custom.go`
- Create: `pkg/seatbelt/modules/guard_custom_test.go`

- [ ] **Step 1: Write failing tests**

Create `pkg/seatbelt/modules/guard_custom_test.go`:

```go
package modules_test

import (
	"strings"
	"testing"

	"github.com/jskswamy/aide/pkg/seatbelt"
	"github.com/jskswamy/aide/pkg/seatbelt/modules"
)

func TestGuard_Custom_Basic(t *testing.T) {
	g := modules.NewCustomGuard("audit-logs", modules.CustomGuardConfig{
		Type:        "default",
		Description: "Audit logs",
		Paths:       []string{"~/.config/audit"},
	})
	if g.Name() != "audit-logs" {
		t.Errorf("expected Name() = %q, got %q", "audit-logs", g.Name())
	}
	if g.Type() != "default" {
		t.Errorf("expected Type() = %q, got %q", "default", g.Type())
	}
	ctx := &seatbelt.Context{HomeDir: "/Users/testuser"}
	output := renderTestRules(g.Rules(ctx))
	if !strings.Contains(output, "/Users/testuser/.config/audit") {
		t.Error("expected audit path in output")
	}
}

func TestGuard_Custom_EnvOverride(t *testing.T) {
	g := modules.NewCustomGuard("company-tokens", modules.CustomGuardConfig{
		Type:        "opt-in",
		Description: "Company CLI credentials",
		Paths:       []string{"~/.config/company-cli"},
		EnvOverride: "COMPANY_CONFIG_DIR",
	})
	ctx := &seatbelt.Context{
		HomeDir: "/Users/testuser",
		Env:     []string{"COMPANY_CONFIG_DIR=/custom/company"},
	}
	output := renderTestRules(g.Rules(ctx))
	if !strings.Contains(output, "/custom/company") {
		t.Error("expected custom path from env override")
	}
	if strings.Contains(output, "/Users/testuser/.config/company-cli") {
		t.Error("default path should be replaced by env override")
	}
}

func TestGuard_Custom_AllowedPaths(t *testing.T) {
	g := modules.NewCustomGuard("internal-certs", modules.CustomGuardConfig{
		Type:        "default",
		Description: "Internal certificate store",
		Paths:       []string{"~/.internal/certs"},
		Allowed:     []string{"~/.internal/certs/ca.pem"},
	})
	ctx := &seatbelt.Context{HomeDir: "/Users/testuser"}
	output := renderTestRules(g.Rules(ctx))

	// Should deny the directory
	if !strings.Contains(output, "/Users/testuser/.internal/certs") {
		t.Error("expected deny for .internal/certs")
	}
	// Should allow the specific file
	if !strings.Contains(output, `(allow file-read* (literal "/Users/testuser/.internal/certs/ca.pem"))`) {
		t.Error("expected allow for ca.pem")
	}
}

func TestGuard_CustomValidation_EnvOverrideMultiPath(t *testing.T) {
	err := modules.ValidateCustomGuard("test", modules.CustomGuardConfig{
		Type:        "default",
		Paths:       []string{"~/.a", "~/.b"},
		EnvOverride: "TEST_VAR",
	})
	if err == nil {
		t.Error("expected validation error for env_override with multiple paths")
	}
}

func TestGuard_CustomValidation_AlwaysType(t *testing.T) {
	err := modules.ValidateCustomGuard("test", modules.CustomGuardConfig{
		Type:  "always",
		Paths: []string{"~/.test"},
	})
	if err == nil {
		t.Error("expected validation error for type: always on custom guard")
	}
}

func TestGuard_CustomValidation_BuiltinNameCollision(t *testing.T) {
	err := modules.ValidateCustomGuard("ssh-keys", modules.CustomGuardConfig{
		Type:  "default",
		Paths: []string{"~/.test"},
	})
	if err == nil {
		t.Error("expected validation error for built-in name collision")
	}
}
```

Run: `go test ./pkg/seatbelt/modules/ -run TestGuard_Custom -v`

Expected: FAIL

- [ ] **Step 2: Implement guard_custom.go**

Create `pkg/seatbelt/modules/guard_custom.go`:

```go
// Custom guard support for dynamic guards from config.

package modules

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/jskswamy/aide/pkg/seatbelt"
)

// CustomGuardConfig holds the config values for a custom guard.
type CustomGuardConfig struct {
	Type        string
	Description string
	Paths       []string
	EnvOverride string
	Allowed     []string
}

type customGuard struct {
	name   string
	cfg    CustomGuardConfig
}

// NewCustomGuard creates a guard from config values.
func NewCustomGuard(name string, cfg CustomGuardConfig) seatbelt.Guard {
	return &customGuard{name: name, cfg: cfg}
}

func (m *customGuard) Name() string        { return m.name }
func (m *customGuard) Type() string        { return m.cfg.Type }
func (m *customGuard) Description() string { return m.cfg.Description }

func (m *customGuard) Rules(ctx *seatbelt.Context) []seatbelt.Rule {
	paths := m.resolvePaths(ctx)

	var rules []seatbelt.Rule
	for _, p := range paths {
		rules = append(rules,
			seatbelt.Raw(fmt.Sprintf(`(deny file-read-data (subpath "%s"))`, p)),
			seatbelt.Raw(fmt.Sprintf(`(deny file-write* (subpath "%s"))`, p)),
		)
	}

	// Allowed paths use literal (more specific than subpath)
	for _, a := range m.cfg.Allowed {
		resolved := resolveHomePath(a, ctx.HomeDir)
		rules = append(rules,
			seatbelt.Raw(fmt.Sprintf(`(allow file-read* (literal "%s"))`, resolved)),
		)
	}

	return rules
}

func (m *customGuard) resolvePaths(ctx *seatbelt.Context) []string {
	// If env override is set and there's exactly one path, check env
	if m.cfg.EnvOverride != "" && len(m.cfg.Paths) == 1 {
		if val, ok := ctx.EnvLookup(m.cfg.EnvOverride); ok && val != "" {
			return []string{val}
		}
	}

	var resolved []string
	for _, p := range m.cfg.Paths {
		resolved = append(resolved, resolveHomePath(p, ctx.HomeDir))
	}
	return resolved
}

// resolveHomePath expands ~ to the actual home directory.
func resolveHomePath(path, homeDir string) string {
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(homeDir, path[2:])
	}
	return path
}

// ValidateCustomGuard validates a custom guard config.
func ValidateCustomGuard(name string, cfg CustomGuardConfig) error {
	// Cannot use type: always
	if cfg.Type == "always" {
		return fmt.Errorf("custom guard %q cannot use type \"always\"", name)
	}

	// Cannot collide with built-in name
	if _, ok := GuardByName(name); ok {
		return fmt.Errorf("custom guard %q conflicts with built-in guard", name)
	}

	// env_override requires exactly one path
	if cfg.EnvOverride != "" && len(cfg.Paths) != 1 {
		return fmt.Errorf("custom guard %q: env_override requires exactly one path, got %d", name, len(cfg.Paths))
	}

	if len(cfg.Paths) == 0 {
		return fmt.Errorf("custom guard %q: at least one path is required", name)
	}

	return nil
}
```

Run: `go test ./pkg/seatbelt/modules/ -run TestGuard_Custom -v`

Expected: PASS

- [ ] **Step 3: Commit**

```
/commit
```

---

### Task 17: Simplify Policy struct + DefaultPolicy

Replace the old Writable/Readable/Denied Policy with guard-based fields.

**Files:**
- Modify: `internal/sandbox/sandbox.go`
- Modify: `internal/sandbox/sandbox_test.go`

- [ ] **Step 1: Write updated tests**

Replace `internal/sandbox/sandbox_test.go`:

```go
package sandbox

import (
	"os/exec"
	"testing"

	"github.com/jskswamy/aide/pkg/seatbelt/modules"
)

func TestDefaultPolicy_Guards(t *testing.T) {
	policy := DefaultPolicy("/tmp/myproject", "/tmp/aide-12345", "/tmp", nil)

	if len(policy.Guards) == 0 {
		t.Fatal("expected non-empty Guards list")
	}

	// Should include all default guard names
	defaultNames := modules.DefaultGuardNames()
	guardSet := make(map[string]bool)
	for _, n := range policy.Guards {
		guardSet[n] = true
	}
	for _, n := range defaultNames {
		if !guardSet[n] {
			t.Errorf("expected guard %q in DefaultPolicy.Guards", n)
		}
	}
}

func TestDefaultPolicy_Fields(t *testing.T) {
	policy := DefaultPolicy("/tmp/proj", "/tmp/rt", "/tmp", []string{"PATH=/usr/bin"})

	if policy.ProjectRoot != "/tmp/proj" {
		t.Errorf("expected ProjectRoot=%q, got %q", "/tmp/proj", policy.ProjectRoot)
	}
	if policy.RuntimeDir != "/tmp/rt" {
		t.Errorf("expected RuntimeDir=%q, got %q", "/tmp/rt", policy.RuntimeDir)
	}
	if policy.Network != NetworkOutbound {
		t.Errorf("expected Network=%q, got %q", NetworkOutbound, policy.Network)
	}
	if !policy.AllowSubprocess {
		t.Error("expected AllowSubprocess=true")
	}
	if policy.CleanEnv {
		t.Error("expected CleanEnv=false")
	}
}

func TestNetworkModeConstants(t *testing.T) {
	if NetworkOutbound != "outbound" {
		t.Errorf("expected NetworkOutbound=%q, got %q", "outbound", NetworkOutbound)
	}
	if NetworkNone != "none" {
		t.Errorf("expected NetworkNone=%q, got %q", "none", NetworkNone)
	}
	if NetworkUnrestricted != "unrestricted" {
		t.Errorf("expected NetworkUnrestricted=%q, got %q", "unrestricted", NetworkUnrestricted)
	}
}

func TestNoopSandbox_Apply_ReturnsNil(t *testing.T) {
	s := &noopSandbox{}
	cmd := exec.Command("echo", "hello")
	policy := DefaultPolicy("/tmp/proj", "/tmp/rt", "/tmp", nil)
	err := s.Apply(cmd, policy, "/tmp/rt")
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
}

func TestNewSandbox_ReturnsNonNil(t *testing.T) {
	s := NewSandbox()
	if s == nil {
		t.Error("expected NewSandbox() to return non-nil Sandbox")
	}
}
```

Run: `go test ./internal/sandbox/ -run "TestDefaultPolicy|TestNetworkMode|TestNoopSandbox|TestNewSandbox" -v`

Expected: FAIL (new DefaultPolicy signature)

- [ ] **Step 2: Update sandbox.go**

Replace `internal/sandbox/sandbox.go`:

```go
package sandbox

import (
	"os/exec"
	"strings"

	"github.com/jskswamy/aide/pkg/seatbelt"
	"github.com/jskswamy/aide/pkg/seatbelt/modules"
)

// Sandbox applies a security policy to a command before execution.
type Sandbox interface {
	Apply(cmd *exec.Cmd, policy Policy, runtimeDir string) error
	GenerateProfile(policy Policy) (string, error)
}

// Policy describes the security boundary for an agent process.
type Policy struct {
	// Guard selection
	Guards      []string        // active guard names
	AgentModule seatbelt.Module // agent-specific module (Claude, etc.)

	// Runtime context (all fields passed to guards via Context)
	ProjectRoot string
	RuntimeDir  string
	TempDir     string
	Env         []string
	Network     NetworkMode
	AllowPorts  []int
	DenyPorts   []int
	ExtraDenied []string // user-configured denied:/denied_extra: paths

	// Process behavior
	AllowSubprocess bool
	CleanEnv        bool
}

// NetworkMode describes the network access policy for a sandboxed agent.
type NetworkMode string

const (
	NetworkOutbound     NetworkMode = "outbound"
	NetworkNone         NetworkMode = "none"
	NetworkUnrestricted NetworkMode = "unrestricted"
)

// DefaultPolicy returns the sandbox policy applied when no sandbox: block
// exists in the context config.
func DefaultPolicy(projectRoot, runtimeDir, tempDir string, env []string) Policy {
	return Policy{
		Guards:          modules.DefaultGuardNames(),
		ProjectRoot:     projectRoot,
		RuntimeDir:      runtimeDir,
		TempDir:         tempDir,
		Env:             env,
		Network:         NetworkOutbound,
		AllowSubprocess: true,
		CleanEnv:        false,
	}
}

// noopSandbox is a fallback Sandbox that does nothing.
type noopSandbox struct{}

func (n *noopSandbox) Apply(_ *exec.Cmd, _ Policy, _ string) error {
	return nil
}

func (n *noopSandbox) GenerateProfile(_ Policy) (string, error) {
	return "Sandbox not available on this platform (no-op sandbox)", nil
}

// expandGlobs expands glob patterns in a list of paths.
func expandGlobs(patterns []string) []string {
	return seatbelt.ExpandGlobs(patterns)
}

// filterEnv returns only essential env vars when CleanEnv is true.
func filterEnv(env []string) []string {
	essential := map[string]bool{
		"PATH": true, "HOME": true, "USER": true,
		"SHELL": true, "TERM": true, "LANG": true,
		"TMPDIR": true, "XDG_RUNTIME_DIR": true, "XDG_CONFIG_HOME": true,
	}
	var filtered []string
	for _, e := range env {
		k := strings.SplitN(e, "=", 2)[0]
		if essential[k] {
			filtered = append(filtered, e)
		}
	}
	return filtered
}
```

Run: `go test ./internal/sandbox/ -run "TestDefaultPolicy|TestNetworkMode|TestNoopSandbox|TestNewSandbox" -v`

Expected: PASS

- [ ] **Step 3: Commit**

```
/commit
```

---

### Task 18: Update darwin.go profile composition

Rewrite `generateSeatbeltProfile` to use the guard registry.

**Files:**
- Modify: `internal/sandbox/darwin.go`
- Modify: `internal/sandbox/darwin_test.go`

- [ ] **Step 1: Update darwin_test.go for new API**

Replace `internal/sandbox/darwin_test.go`:

```go
//go:build darwin

package sandbox

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestGenerateSeatbeltProfile_DenyDefault(t *testing.T) {
	policy := DefaultPolicy("/tmp/proj", "/tmp/rt", "/tmp", nil)
	policy.Network = NetworkNone
	profile, err := generateSeatbeltProfile(policy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(profile, "(deny default)") {
		t.Error("profile should contain (deny default)")
	}
}

func TestGenerateSeatbeltProfile_NetworkOutbound(t *testing.T) {
	policy := DefaultPolicy("/tmp/proj", "/tmp/rt", "/tmp", nil)
	policy.Network = NetworkOutbound
	profile, err := generateSeatbeltProfile(policy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(profile, "(allow network-outbound)") {
		t.Error("profile should contain (allow network-outbound)")
	}
}

func TestGenerateSeatbeltProfile_NetworkNone(t *testing.T) {
	policy := DefaultPolicy("/tmp/proj", "/tmp/rt", "/tmp", nil)
	policy.Network = NetworkNone
	profile, err := generateSeatbeltProfile(policy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// With deny-default, NetworkNone needs no network rules
	if strings.Contains(profile, "(allow network-outbound)") {
		t.Error("profile should NOT contain (allow network-outbound) with NetworkNone")
	}
}

func TestGenerateSeatbeltProfile_SystemEssentials(t *testing.T) {
	policy := DefaultPolicy("/tmp/proj", "/tmp/rt", "/tmp", nil)
	profile, err := generateSeatbeltProfile(policy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	essentials := []string{
		"(allow sysctl-read)",
		"(allow mach-lookup",
		"(allow pseudo-tty)",
		"(allow process-exec)",
		"(allow process-fork)",
	}
	for _, e := range essentials {
		if !strings.Contains(profile, e) {
			t.Errorf("profile should contain %q", e)
		}
	}
}

func TestGenerateSeatbeltProfile_PortFiltering(t *testing.T) {
	policy := DefaultPolicy("/tmp/proj", "/tmp/rt", "/tmp", nil)
	policy.AllowPorts = []int{443, 53}
	profile, err := generateSeatbeltProfile(policy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(profile, "(deny network-outbound)") {
		t.Error("profile should contain (deny network-outbound) when AllowPorts is set")
	}
	if !strings.Contains(profile, `(allow network-outbound (remote tcp "*:443"))`) {
		t.Error("profile should contain per-port TCP rule for 443")
	}
}

func TestDarwinSandbox_Apply_RewritesCmd(t *testing.T) {
	runtimeDir := t.TempDir()
	cmd := exec.Command("/usr/bin/echo", "hello", "world")
	policy := DefaultPolicy("/tmp/proj", runtimeDir, "/tmp", nil)

	s := &darwinSandbox{}
	err := s.Apply(cmd, policy, runtimeDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cmd.Path != "/usr/bin/sandbox-exec" {
		t.Errorf("expected cmd.Path=/usr/bin/sandbox-exec, got %q", cmd.Path)
	}
	if cmd.Args[0] != "sandbox-exec" {
		t.Errorf("expected Args[0]=sandbox-exec, got %q", cmd.Args[0])
	}
	if cmd.Args[1] != "-f" {
		t.Errorf("expected Args[1]=-f, got %q", cmd.Args[1])
	}

	profilePath := cmd.Args[2]
	content, err := os.ReadFile(profilePath)
	if err != nil {
		t.Fatalf("failed to read profile: %v", err)
	}
	if !strings.Contains(string(content), "(deny default)") {
		t.Error("profile file should contain (deny default)")
	}
}

func TestDarwinSandbox_Apply_CleanEnv(t *testing.T) {
	runtimeDir := t.TempDir()
	cmd := exec.Command("/usr/bin/echo", "hello")
	cmd.Env = []string{
		"PATH=/usr/bin",
		"HOME=/home/user",
		"SECRET_KEY=abc123",
		"TERM=xterm",
	}
	policy := DefaultPolicy("/tmp/proj", runtimeDir, "/tmp", nil)
	policy.CleanEnv = true

	s := &darwinSandbox{}
	err := s.Apply(cmd, policy, runtimeDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	envMap := make(map[string]string)
	for _, e := range cmd.Env {
		parts := strings.SplitN(e, "=", 2)
		if len(parts) == 2 {
			envMap[parts[0]] = parts[1]
		}
	}
	if _, ok := envMap["PATH"]; !ok {
		t.Error("PATH should be preserved")
	}
	if _, ok := envMap["SECRET_KEY"]; ok {
		t.Error("SECRET_KEY should be filtered out")
	}
}
```

Run: `go test ./internal/sandbox/ -run "TestGenerateSeatbeltProfile|TestDarwinSandbox" -v`

Expected: FAIL (darwin.go still uses old API)

- [ ] **Step 2: Rewrite darwin.go**

Replace `internal/sandbox/darwin.go`:

```go
//go:build darwin

// Package sandbox provides OS-native sandboxing for agent processes.
package sandbox

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/jskswamy/aide/pkg/seatbelt"
	"github.com/jskswamy/aide/pkg/seatbelt/modules"
)

// NewSandbox returns a darwinSandbox on macOS.
func NewSandbox() Sandbox {
	return &darwinSandbox{}
}

type darwinSandbox struct{}

func (d *darwinSandbox) Apply(cmd *exec.Cmd, policy Policy, runtimeDir string) error {
	profile, err := generateSeatbeltProfile(policy)
	if err != nil {
		return fmt.Errorf("generating seatbelt profile: %w", err)
	}

	profilePath := filepath.Join(runtimeDir, "sandbox.sb")
	if err := os.WriteFile(profilePath, []byte(profile), 0600); err != nil {
		return fmt.Errorf("writing seatbelt profile: %w", err)
	}

	originalArgs := cmd.Args
	cmd.Path = "/usr/bin/sandbox-exec"
	cmd.Args = append(
		[]string{"sandbox-exec", "-f", profilePath},
		originalArgs...,
	)

	if policy.CleanEnv {
		cmd.Env = filterEnv(cmd.Env)
	}

	return nil
}

func (d *darwinSandbox) GenerateProfile(policy Policy) (string, error) {
	return generateSeatbeltProfile(policy)
}

func generateSeatbeltProfile(policy Policy) (string, error) {
	homeDir, _ := os.UserHomeDir()

	// Map sandbox.NetworkMode to seatbelt.NetworkMode
	var networkMode seatbelt.NetworkMode
	switch policy.Network {
	case NetworkNone:
		networkMode = seatbelt.NetworkNone
	case NetworkOutbound:
		networkMode = seatbelt.NetworkOutbound
	default:
		networkMode = seatbelt.NetworkOpen
	}

	activeGuards := modules.ResolveActiveGuards(policy.Guards)

	p := seatbelt.New(homeDir).
		WithContext(func(c *seatbelt.Context) {
			c.ProjectRoot = policy.ProjectRoot
			c.TempDir = policy.TempDir
			c.RuntimeDir = policy.RuntimeDir
			c.Env = policy.Env
			c.GOOS = runtime.GOOS
			c.Network = networkMode
			c.AllowPorts = policy.AllowPorts
			c.DenyPorts = policy.DenyPorts
			c.ExtraDenied = policy.ExtraDenied
		})

	for _, g := range activeGuards {
		p.Use(g)
	}

	if policy.AgentModule != nil {
		p.Use(policy.AgentModule)
	}

	return p.Render()
}
```

Run: `go test ./internal/sandbox/ -run "TestGenerateSeatbeltProfile|TestDarwinSandbox" -v`

Expected: PASS

- [ ] **Step 3: Commit**

```
/commit
```

---

### Task 19: Update PolicyFromConfig for guard resolution

Rewrite `PolicyFromConfig` to resolve guards from config fields.

**Files:**
- Modify: `internal/sandbox/policy.go`
- Modify: `internal/sandbox/policy_test.go`

- [ ] **Step 1: Write guard resolution tests**

Add to `internal/sandbox/policy_test.go`:

```go
func TestPolicyFromConfig_GuardsOverridesDefault(t *testing.T) {
	cfg := &config.SandboxPolicy{
		Guards: []string{"ssh-keys", "cloud-aws"},
	}
	policy, warnings, err := PolicyFromConfig(cfg, "/proj", "/rt", "/home/user", "/tmp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = warnings

	// Should include always guards + specified guards
	guardSet := make(map[string]bool)
	for _, n := range policy.Guards {
		guardSet[n] = true
	}
	if !guardSet["base"] {
		t.Error("always guard 'base' should be in active set")
	}
	if !guardSet["ssh-keys"] {
		t.Error("specified guard 'ssh-keys' should be in active set")
	}
	if !guardSet["cloud-aws"] {
		t.Error("specified guard 'cloud-aws' should be in active set")
	}
	// Default guards NOT listed should be excluded
	if guardSet["browsers"] {
		t.Error("unspecified default guard 'browsers' should NOT be in active set")
	}
}

func TestPolicyFromConfig_GuardsExtraAdds(t *testing.T) {
	cfg := &config.SandboxPolicy{
		GuardsExtra: []string{"docker"},
	}
	policy, _, err := PolicyFromConfig(cfg, "/proj", "/rt", "/home/user", "/tmp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	guardSet := make(map[string]bool)
	for _, n := range policy.Guards {
		guardSet[n] = true
	}
	if !guardSet["docker"] {
		t.Error("guards_extra 'docker' should be in active set")
	}
	if !guardSet["ssh-keys"] {
		t.Error("default guard 'ssh-keys' should still be in active set")
	}
}

func TestPolicyFromConfig_UnguardRemoves(t *testing.T) {
	cfg := &config.SandboxPolicy{
		Unguard: []string{"browsers"},
	}
	policy, _, err := PolicyFromConfig(cfg, "/proj", "/rt", "/home/user", "/tmp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, n := range policy.Guards {
		if n == "browsers" {
			t.Error("unguarded 'browsers' should not be in active set")
		}
	}
}

func TestPolicyFromConfig_UnguardAlways_Error(t *testing.T) {
	cfg := &config.SandboxPolicy{
		Unguard: []string{"base"},
	}
	_, _, err := PolicyFromConfig(cfg, "/proj", "/rt", "/home/user", "/tmp")
	if err == nil {
		t.Error("expected error when unguarding always guard")
	}
}

func TestPolicyFromConfig_GuardsAndGuardsExtraWarns(t *testing.T) {
	cfg := &config.SandboxPolicy{
		Guards:      []string{"ssh-keys"},
		GuardsExtra: []string{"docker"},
	}
	_, warnings, err := PolicyFromConfig(cfg, "/proj", "/rt", "/home/user", "/tmp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(warnings) == 0 {
		t.Error("expected warning when both guards and guards_extra are set")
	}
}

func TestPolicyFromConfig_UnknownGuardName_Error(t *testing.T) {
	cfg := &config.SandboxPolicy{
		Guards: []string{"typo-guard"},
	}
	_, _, err := PolicyFromConfig(cfg, "/proj", "/rt", "/home/user", "/tmp")
	if err == nil {
		t.Error("expected error for unknown guard name")
	}
}

func TestPolicyFromConfig_DeniedAndDeniedExtra(t *testing.T) {
	cfg := &config.SandboxPolicy{
		Denied:      []string{"/secret"},
		DeniedExtra: []string{"/other"},
	}
	policy, warnings, err := PolicyFromConfig(cfg, "/proj", "/rt", "/home/user", "/tmp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// denied_extra should be ignored when denied is set
	if len(warnings) == 0 {
		t.Error("expected warning when both denied and denied_extra are set")
	}
	found := false
	for _, p := range policy.ExtraDenied {
		if p == "/secret" {
			found = true
		}
		if p == "/other" {
			t.Error("/other from denied_extra should be ignored")
		}
	}
	if !found {
		t.Error("expected /secret in ExtraDenied")
	}
}
```

Run: `go test ./internal/sandbox/ -run "TestPolicyFromConfig_Guards|TestPolicyFromConfig_Unguard|TestPolicyFromConfig_Denied" -v`

Expected: FAIL

- [ ] **Step 2: Rewrite policy.go**

This is a significant rewrite. The new `PolicyFromConfig` resolves guards instead of writable/readable/denied paths.

Replace the core function in `internal/sandbox/policy.go`. Keep the template resolution helpers, validation functions, and `ResolveSandboxRef`:

```go
package sandbox

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/jskswamy/aide/internal/config"
	"github.com/jskswamy/aide/pkg/seatbelt/modules"
)

// PolicyFromConfig builds a sandbox.Policy from a SandboxPolicy config.
func PolicyFromConfig(
	cfg *config.SandboxPolicy,
	projectRoot, runtimeDir, homeDir, tempDir string,
) (*Policy, []string, error) {
	defaults := DefaultPolicy(projectRoot, runtimeDir, tempDir, nil)

	if cfg == nil {
		return &defaults, nil, nil
	}

	var warnings []string
	policy := defaults

	// --- Guard resolution ---
	guardNames, gw, err := resolveGuardNames(cfg)
	if err != nil {
		return nil, nil, err
	}
	warnings = append(warnings, gw...)
	if guardNames != nil {
		policy.Guards = guardNames
	}

	// --- Denied paths (user-configured, fed to filesystem guard via ExtraDenied) ---
	if len(cfg.Denied) > 0 && len(cfg.DeniedExtra) > 0 {
		warnings = append(warnings,
			"sandbox: both denied and denied_extra are set; denied_extra is ignored when denied is specified",
		)
	}

	templateVars := map[string]string{
		"project_root": projectRoot,
		"runtime_dir":  runtimeDir,
		"home":         homeDir,
		"config_dir":   filepath.Join(homeDir, ".config", "aide"),
	}

	if len(cfg.Denied) > 0 {
		d, err := ResolvePaths(cfg.Denied, templateVars)
		if err != nil {
			return nil, nil, err
		}
		policy.ExtraDenied = d
	} else if len(cfg.DeniedExtra) > 0 {
		extra, err := ResolvePaths(cfg.DeniedExtra, templateVars)
		if err != nil {
			return nil, nil, err
		}
		policy.ExtraDenied = extra
	}

	// --- Network ---
	if cfg.Network != nil && cfg.Network.Mode != "" {
		policy.Network = NetworkMode(cfg.Network.Mode)
	}
	if cfg.Network != nil {
		policy.AllowPorts = cfg.Network.AllowPorts
		policy.DenyPorts = cfg.Network.DenyPorts
	}

	if cfg.AllowSubprocess != nil {
		policy.AllowSubprocess = *cfg.AllowSubprocess
	}
	if cfg.CleanEnv != nil {
		policy.CleanEnv = *cfg.CleanEnv
	}

	return &policy, warnings, nil
}

// resolveGuardNames resolves guard names from config fields.
func resolveGuardNames(cfg *config.SandboxPolicy) ([]string, []string, error) {
	var warnings []string

	// Both guards and guards_extra set: warn, ignore guards_extra
	if len(cfg.Guards) > 0 && len(cfg.GuardsExtra) > 0 {
		warnings = append(warnings,
			"sandbox: both guards and guards_extra are set; guards_extra is ignored when guards is specified",
		)
	}

	var active []string

	if len(cfg.Guards) > 0 {
		// guards: replaces default set, always guards are included automatically
		alwaysNames := alwaysGuardNames()
		alwaySet := make(map[string]bool)
		for _, n := range alwaysNames {
			alwaySet[n] = true
		}

		// Start with always guards
		active = append(active, alwaysNames...)
		seen := make(map[string]bool)
		for _, n := range alwaysNames {
			seen[n] = true
		}

		// Expand and add user-specified guards
		for _, name := range cfg.Guards {
			expanded := modules.ExpandGuardName(name)
			for _, en := range expanded {
				if seen[en] {
					continue // silently deduplicate always guards
				}
				if _, ok := modules.GuardByName(en); !ok {
					return nil, nil, fmt.Errorf("unknown guard %q", en)
				}
				seen[en] = true
				active = append(active, en)
			}
		}
	} else if len(cfg.GuardsExtra) > 0 {
		// guards_extra: extend default set
		active = modules.DefaultGuardNames()
		seen := make(map[string]bool)
		for _, n := range active {
			seen[n] = true
		}
		for _, name := range cfg.GuardsExtra {
			expanded := modules.ExpandGuardName(name)
			for _, en := range expanded {
				if seen[en] {
					continue
				}
				if _, ok := modules.GuardByName(en); !ok {
					return nil, nil, fmt.Errorf("unknown guard %q", en)
				}
				seen[en] = true
				active = append(active, en)
			}
		}
	} else {
		active = nil // use default from DefaultPolicy
	}

	// Apply unguard
	if len(cfg.Unguard) > 0 {
		if active == nil {
			active = modules.DefaultGuardNames()
		}
		removeSet := make(map[string]bool)
		for _, name := range cfg.Unguard {
			expanded := modules.ExpandGuardName(name)
			for _, en := range expanded {
				// Validate: cannot unguard always guards
				if g, ok := modules.GuardByName(en); ok && g.Type() == "always" {
					return nil, nil, fmt.Errorf("cannot unguard %q: type is always", en)
				}
				removeSet[en] = true
			}
		}
		var filtered []string
		for _, n := range active {
			if !removeSet[n] {
				filtered = append(filtered, n)
			}
		}
		active = filtered
	}

	return active, warnings, nil
}

// alwaysGuardNames returns names of all always-type guards.
func alwaysGuardNames() []string {
	var names []string
	for _, g := range modules.GuardsByType("always") {
		names = append(names, g.Name())
	}
	return names
}

// --- Keep existing template/path resolution functions below ---

// ResolvePaths resolves template variables and ~ in a list of path strings.
func ResolvePaths(paths []string, vars map[string]string) ([]string, error) {
	var resolved []string
	for _, p := range paths {
		r, err := resolvePathTemplate(p, vars)
		if err != nil {
			return nil, fmt.Errorf("resolving path %q: %w", p, err)
		}
		if strings.HasPrefix(r, "~/") {
			r = filepath.Join(vars["home"], r[2:])
		}
		resolved = append(resolved, r)
	}
	return resolved, nil
}

func resolvePathTemplate(tmplStr string, vars map[string]string) (string, error) {
	tmpl, err := template.New("path").Option("missingkey=error").Parse(tmplStr)
	if err != nil {
		return "", fmt.Errorf("template parse error: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, vars); err != nil {
		return "", fmt.Errorf("template execution error: %w", err)
	}
	return buf.String(), nil
}

func isGlobPattern(path string) bool {
	return strings.ContainsAny(path, "*?[{")
}

func validateAndFilterPaths(paths []string, warnings *[]string) []string {
	var filtered []string
	for _, p := range paths {
		if isGlobPattern(p) {
			filtered = append(filtered, p)
			continue
		}
		if _, err := os.Lstat(p); err != nil {
			*warnings = append(*warnings, fmt.Sprintf("skipped: %s (not found)", p))
			continue
		}
		filtered = append(filtered, p)
	}
	return filtered
}

// ValidationResult holds the results of a detailed sandbox config validation.
type ValidationResult struct {
	Errors   []string
	Warnings []string
}

// ValidateSandboxConfigDetailed validates a SandboxPolicy configuration.
func ValidateSandboxConfigDetailed(cfg *config.SandboxPolicy) ValidationResult {
	var result ValidationResult
	if cfg == nil {
		return result
	}

	if cfg.Network != nil {
		validNetworkModes := map[string]bool{
			"outbound": true, "none": true, "unrestricted": true, "": true,
		}
		if !validNetworkModes[cfg.Network.Mode] {
			result.Errors = append(result.Errors, fmt.Sprintf(
				"sandbox.network: invalid value %q, must be one of: outbound, none, unrestricted",
				cfg.Network.Mode,
			))
		}
		for _, port := range cfg.Network.AllowPorts {
			if port < 1 || port > 65535 {
				result.Errors = append(result.Errors, fmt.Sprintf(
					"sandbox.network.allow_ports: invalid port %d, must be 1-65535", port,
				))
			}
		}
		for _, port := range cfg.Network.DenyPorts {
			if port < 1 || port > 65535 {
				result.Errors = append(result.Errors, fmt.Sprintf(
					"sandbox.network.deny_ports: invalid port %d, must be 1-65535", port,
				))
			}
		}
	}

	if len(cfg.Denied) > 0 && len(cfg.DeniedExtra) > 0 {
		result.Warnings = append(result.Warnings,
			"sandbox: both denied and denied_extra are set; denied_extra is ignored when denied is specified",
		)
	}

	if len(cfg.Guards) > 0 && len(cfg.GuardsExtra) > 0 {
		result.Warnings = append(result.Warnings,
			"sandbox: both guards and guards_extra are set; guards_extra is ignored when guards is specified",
		)
	}

	// Validate guard names
	for _, name := range cfg.Guards {
		for _, en := range modules.ExpandGuardName(name) {
			if _, ok := modules.GuardByName(en); !ok {
				result.Errors = append(result.Errors, fmt.Sprintf("unknown guard %q", en))
			}
		}
	}
	for _, name := range cfg.GuardsExtra {
		for _, en := range modules.ExpandGuardName(name) {
			if _, ok := modules.GuardByName(en); !ok {
				result.Errors = append(result.Errors, fmt.Sprintf("unknown guard %q", en))
			}
		}
	}

	// Validate unguard: cannot unguard always guards
	for _, name := range cfg.Unguard {
		for _, en := range modules.ExpandGuardName(name) {
			if g, ok := modules.GuardByName(en); ok && g.Type() == "always" {
				result.Errors = append(result.Errors, fmt.Sprintf(
					"cannot unguard %q: type is always", en,
				))
			}
		}
	}

	return result
}

func ValidateSandboxConfig(cfg *config.SandboxPolicy) error {
	result := ValidateSandboxConfigDetailed(cfg)
	if len(result.Errors) > 0 {
		return fmt.Errorf("%s", result.Errors[0])
	}
	return nil
}

func ValidateSandboxRef(ref *config.SandboxRef, sandboxes map[string]config.SandboxPolicy) error {
	if ref == nil || ref.Disabled {
		return nil
	}
	if ref.Inline != nil {
		return ValidateSandboxConfig(ref.Inline)
	}
	if ref.ProfileName != "" {
		if ref.ProfileName == "default" || ref.ProfileName == "none" {
			return nil
		}
		if sandboxes == nil {
			return fmt.Errorf("sandbox profile %q not found (no sandboxes defined)", ref.ProfileName)
		}
		if _, ok := sandboxes[ref.ProfileName]; !ok {
			return fmt.Errorf("sandbox profile %q not found in sandboxes map", ref.ProfileName)
		}
		sp := sandboxes[ref.ProfileName]
		return ValidateSandboxConfig(&sp)
	}
	return nil
}

func ResolveSandboxRef(ref *config.SandboxRef, sandboxes map[string]config.SandboxPolicy) (*config.SandboxPolicy, bool, error) {
	if ref == nil {
		return nil, false, nil
	}
	if ref.Disabled {
		return nil, true, nil
	}
	if ref.Inline != nil {
		return ref.Inline, false, nil
	}
	if ref.ProfileName == "" || ref.ProfileName == "default" {
		return nil, false, nil
	}
	if ref.ProfileName == "none" {
		return nil, true, nil
	}
	if sandboxes == nil {
		return nil, false, fmt.Errorf("sandbox profile %q not found (no sandboxes defined)", ref.ProfileName)
	}
	sp, ok := sandboxes[ref.ProfileName]
	if !ok {
		return nil, false, fmt.Errorf("sandbox profile %q not found in sandboxes map", ref.ProfileName)
	}
	return &sp, false, nil
}

func isHomeDirPath(path string) bool {
	if strings.HasPrefix(path, "/home/") || strings.HasPrefix(path, "/Users/") {
		parts := strings.Split(strings.TrimRight(path, "/"), "/")
		return len(parts) == 3
	}
	return false
}
```

Run: `go test ./internal/sandbox/ -v`

Expected: PASS

- [ ] **Step 3: Commit**

```
/commit
```

---

### Task 20: Update config schema

Add guard fields to `SandboxPolicy` in `internal/config/schema.go`.

**Files:**
- Modify: `internal/config/schema.go`

- [ ] **Step 1: Add guard fields to SandboxPolicy**

Add the following fields to the `SandboxPolicy` struct in `internal/config/schema.go`:

```go
// In SandboxPolicy struct, add after the existing fields:
	Guards      []string `yaml:"guards,omitempty"`
	GuardsExtra []string `yaml:"guards_extra,omitempty"`
	Unguard     []string `yaml:"unguard,omitempty"`
```

Also add the `CustomGuard` and `GuardType` structs, and the fields on `Config`:

```go
// CustomGuard defines a user-configured guard.
type CustomGuard struct {
	Type        string   `yaml:"type,omitempty"`
	Description string   `yaml:"description,omitempty"`
	Paths       []string `yaml:"paths"`
	EnvOverride string   `yaml:"env_override,omitempty"`
	Allowed     []string `yaml:"allowed,omitempty"`
}

// GuardType defines a custom guard type with behavior mapping.
type GuardType struct {
	Behavior    string `yaml:"behavior"`
	Description string `yaml:"description"`
}
```

Add to `Config` struct:

```go
	CustomGuards map[string]CustomGuard `yaml:"custom_guards,omitempty"`
	GuardTypes   map[string]GuardType   `yaml:"guard_types,omitempty"`
```

Run: `go test ./internal/config/ -v && go test ./internal/sandbox/ -v`

Expected: PASS

- [ ] **Step 2: Commit**

```
/commit
```

---

### Task 21: CLI commands

Add `guards`, `guard`, `unguard`, `types` subcommands to the sandbox command group.

**Files:**
- Modify: `cmd/aide/commands.go`

- [ ] **Step 1: Add subcommands to sandboxCmd**

In the `sandboxCmd()` function, add:

```go
cmd.AddCommand(sandboxGuardsCmd())
cmd.AddCommand(sandboxGuardCmd())
cmd.AddCommand(sandboxUnguardCmd())
cmd.AddCommand(sandboxTypesCmd())
```

- [ ] **Step 2: Implement sandboxGuardsCmd**

```go
func sandboxGuardsCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "guards",
		Short:        "List all guards with type, status, and paths",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			guards := modules.AllGuards()

			// Determine active set (default for now)
			activeSet := make(map[string]bool)
			for _, n := range modules.DefaultGuardNames() {
				activeSet[n] = true
			}

			fmt.Fprintf(out, "%-20s %-12s %-10s %s\n", "GUARD", "TYPE", "STATUS", "DESCRIPTION")
			for _, g := range guards {
				status := "inactive"
				if activeSet[g.Name()] {
					status = "active"
				}
				fmt.Fprintf(out, "%-20s %-12s %-10s %s\n",
					g.Name(), g.Type(), status, g.Description())
			}
			return nil
		},
	}
}
```

- [ ] **Step 3: Implement sandboxGuardCmd**

```go
func sandboxGuardCmd() *cobra.Command {
	var contextName string
	cmd := &cobra.Command{
		Use:          "guard <name>",
		Short:        "Add a guard to guards_extra for the current context",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			guardName := args[0]

			// Validate guard name
			if _, ok := modules.GuardByName(guardName); !ok {
				return fmt.Errorf("unknown guard %q", guardName)
			}

			cfg, ctxName, ctx, err := resolveContextForMutation(contextName)
			if err != nil {
				return err
			}
			sp := ensureInlineSandbox(&ctx)
			// Add to guards_extra if not already present
			for _, g := range sp.GuardsExtra {
				if g == guardName {
					fmt.Fprintf(out, "Guard %q already in guards_extra for context %q\n", guardName, ctxName)
					return nil
				}
			}
			sp.GuardsExtra = append(sp.GuardsExtra, guardName)
			cfg.Contexts[ctxName] = ctx
			if err := config.WriteConfig(cfg); err != nil {
				return fmt.Errorf("writing config: %w", err)
			}
			fmt.Fprintf(out, "Added guard %q to guards_extra for context %q\n", guardName, ctxName)
			return nil
		},
	}
	cmd.Flags().StringVar(&contextName, "context", "", "target context name")
	return cmd
}
```

- [ ] **Step 4: Implement sandboxUnguardCmd**

```go
func sandboxUnguardCmd() *cobra.Command {
	var contextName string
	cmd := &cobra.Command{
		Use:          "unguard <name>",
		Short:        "Add a guard to the unguard list for the current context",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			guardName := args[0]

			// Validate: cannot unguard always guards
			if g, ok := modules.GuardByName(guardName); ok && g.Type() == "always" {
				return fmt.Errorf("cannot unguard %q: type is always", guardName)
			}

			cfg, ctxName, ctx, err := resolveContextForMutation(contextName)
			if err != nil {
				return err
			}
			sp := ensureInlineSandbox(&ctx)
			for _, u := range sp.Unguard {
				if u == guardName {
					fmt.Fprintf(out, "Guard %q already in unguard for context %q\n", guardName, ctxName)
					return nil
				}
			}
			sp.Unguard = append(sp.Unguard, guardName)
			cfg.Contexts[ctxName] = ctx
			if err := config.WriteConfig(cfg); err != nil {
				return fmt.Errorf("writing config: %w", err)
			}
			fmt.Fprintf(out, "Added %q to unguard for context %q\n", guardName, ctxName)
			return nil
		},
	}
	cmd.Flags().StringVar(&contextName, "context", "", "target context name")
	return cmd
}
```

- [ ] **Step 5: Implement sandboxTypesCmd**

```go
func sandboxTypesCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "types",
		Short:        "List all guard types",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "%-14s %-10s %s\n", "TYPE", "DEFAULT", "DESCRIPTION")
			fmt.Fprintf(out, "%-14s %-10s %s\n", "always", "active", "Agent needs this to function. Cannot be disabled.")
			fmt.Fprintf(out, "%-14s %-10s %s\n", "default", "active", "Protects important data. On by default, can be disabled.")
			fmt.Fprintf(out, "%-14s %-10s %s\n", "opt-in", "inactive", "Extra restriction. Off by default, user chooses to enable.")
			return nil
		},
	}
}
```

Note: Add `"github.com/jskswamy/aide/pkg/seatbelt/modules"` to the imports in commands.go.

Run: `go build ./cmd/aide/`

Expected: PASS (compiles successfully)

- [ ] **Step 6: Commit**

```
/commit
```

---

### Task 22: Update launcher callers

Update `launcher.go` and `passthrough.go` for the new `DefaultPolicy` signature (no `homeDir` parameter, takes `env` parameter).

**Files:**
- Modify: `internal/launcher/launcher.go`
- Modify: `internal/launcher/passthrough.go`

- [ ] **Step 1: Update passthrough.go**

In `passthrough.go`, update the `execAgent` function. Change:

```go
policy := sandbox.DefaultPolicy(projectRoot, rtDir.Path(), homeDir, tempDir)
```

to:

```go
policy := sandbox.DefaultPolicy(projectRoot, rtDir.Path(), tempDir, os.Environ())
```

Also update the `agentDirs` and banner data to work with the new Policy (no `policy.Writable`, `policy.Readable`, `policy.Denied`).

For banner, update to use guard count instead of old fields:

```go
bannerData := &ui.BannerData{
    AgentName: name,
    AgentPath: binary,
    Sandbox: &ui.SandboxInfo{
        Network:       string(policy.Network),
        Ports:         "all",
        WritableCount: 0, // guards manage paths now
        ReadableCount: 0,
    },
}
```

The `agentDirs` should be handled differently now - the agent module should be set on the policy:

```go
policy.AgentModule = resolveAgentModule(name, os.Environ(), homeDir)
```

For now, since `resolveAgentModule` doesn't exist yet and agent modules (like ClaudeAgent) are not guards, keep the existing approach by creating a simple agent filesystem module or defer to a later task. The simplest approach: just set `AgentModule` to the ClaudeAgent module when appropriate.

- [ ] **Step 2: Update launcher.go**

In `launcher.go`, update the `Launch` function. The `PolicyFromConfig` call no longer takes `homeDir` as it uses guards. Update:

```go
policy, pw, err := sandbox.PolicyFromConfig(sandboxCfg, projectRoot, rtDir.Path(), homeDir, tempDir)
```

The function signature still accepts `homeDir` for template resolution. The agent config dirs handling needs updating since `policy.Writable` no longer exists. Set the agent module on the policy instead.

Update the banner data construction to work with the new Policy struct.

Run: `go build ./cmd/aide/`

Expected: PASS

- [ ] **Step 3: Run full test suite**

Run: `go test ./... -count=1`

Expected: PASS

- [ ] **Step 4: Commit**

```
/commit
```

---

### Task 23: Integration test -- full profile no keychain conflict

Verify the complete generated profile has no keychain deny that would conflict with the keychain guard's allows.

**Files:**
- Modify: `internal/sandbox/darwin_test.go`

- [ ] **Step 1: Write integration tests**

Add to `internal/sandbox/darwin_test.go`:

```go
func TestProfile_NoKeychainConflict(t *testing.T) {
	policy := DefaultPolicy("/tmp/proj", "/tmp/rt", "/tmp", nil)
	profile, err := generateSeatbeltProfile(policy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Keychain guard produces allow rules for ~/Library/Keychains.
	// No guard should produce deny rules for that path.
	if strings.Contains(profile, `(deny file-read-data (subpath`) &&
		strings.Contains(profile, "Library/Keychains") {
		t.Error("profile should not have deny rules for Library/Keychains (managed by keychain guard)")
	}
}

func TestProfile_SSHAllowBeatsSubpathDeny(t *testing.T) {
	policy := DefaultPolicy("/tmp/proj", "/tmp/rt", "/tmp", nil)
	profile, err := generateSeatbeltProfile(policy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// ssh-keys guard: deny (subpath ~/.ssh) + allow (literal ~/.ssh/known_hosts)
	if !strings.Contains(profile, "(deny file-read-data (subpath") {
		t.Error("expected deny subpath for .ssh")
	}
	if !strings.Contains(profile, "(allow file-read* (literal") {
		t.Error("expected allow literal for known_hosts")
	}
}

func TestProfile_NpmGuardOverridesToolchain(t *testing.T) {
	// When npm opt-in guard is active alongside node-toolchain always guard,
	// npm guard's deny (literal .npmrc) beats node-toolchain's allow at same specificity
	policy := DefaultPolicy("/tmp/proj", "/tmp/rt", "/tmp", nil)
	policy.Guards = append(policy.Guards, "npm")
	profile, err := generateSeatbeltProfile(policy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Both should be present - seatbelt resolves deny-wins-at-same-specificity
	if !strings.Contains(profile, ".npmrc") {
		t.Error("expected .npmrc in profile (from both node-toolchain and npm guards)")
	}
}
```

Run: `go test ./internal/sandbox/ -run "TestProfile_NoKeychain|TestProfile_SSH|TestProfile_Npm" -v`

Expected: PASS

- [ ] **Step 2: Commit**

```
/commit
```

---

### Task 24: Remove old module files + cleanup

Delete the old module files that have been absorbed into guard files. Verify no remaining imports reference them.

**Files:**
- Delete: `pkg/seatbelt/modules/base.go` (already deleted in Task 2)
- Delete: `pkg/seatbelt/modules/system.go` (already deleted in Task 3)
- Delete: `pkg/seatbelt/modules/network.go` (already deleted in Task 4)
- Delete: `pkg/seatbelt/modules/filesystem.go` (already deleted in Task 5)
- Delete: `pkg/seatbelt/modules/keychain.go` (already deleted in Task 6)
- Delete: `pkg/seatbelt/modules/node.go` (already deleted in Task 7)
- Delete: `pkg/seatbelt/modules/nix.go` (already deleted in Task 8)
- Delete: `pkg/seatbelt/modules/git.go` (already deleted in Task 9)

Since deletions happened in individual tasks, this task is a verification pass.

- [ ] **Step 1: Verify no old files remain**

Check that these files do NOT exist:
- `pkg/seatbelt/modules/base.go`
- `pkg/seatbelt/modules/system.go`
- `pkg/seatbelt/modules/network.go`
- `pkg/seatbelt/modules/filesystem.go`
- `pkg/seatbelt/modules/keychain.go`
- `pkg/seatbelt/modules/node.go`
- `pkg/seatbelt/modules/nix.go`
- `pkg/seatbelt/modules/git.go`

- [ ] **Step 2: Run full test suite**

Run: `go test ./... -count=1`

Expected: PASS

- [ ] **Step 3: Verify build**

Run: `go build ./cmd/aide/`

Expected: PASS

- [ ] **Step 4: Final commit**

```
/commit
```
