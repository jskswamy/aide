# Guard System Polish Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox syntax for tracking.

**Goal:** Fix 35 issues across 9 systemic patterns in the guard system by restructuring packages, unifying types, removing deprecated code, adding validation, typed helpers, description rewrites, smart config mutation, test coverage, and docs reconciliation.

**Architecture:** Guards move from `pkg/seatbelt/modules/` to `pkg/seatbelt/guards/` as a dedicated package. The `NetworkMode int` type in `pkg/seatbelt/module.go` is replaced with a plain `string` on `Context.Network`, eliminating the int-to-string mapping in `darwin.go`. A shared `ValidationResult` type in `pkg/seatbelt/validation.go` unifies all 5 validation call sites. Config mutation logic moves from CLI commands into `internal/sandbox/guard_config.go` so CLI commands become thin wrappers.

**Tech Stack:** Go, Apple Seatbelt (macOS sandbox)

---

## Task 1: Package Restructure (Fix 9)

Move all guard files and registry from `pkg/seatbelt/modules/` to `pkg/seatbelt/guards/`. Keep `claude.go` in `modules/`.

### Step 1.1: Create guards package directory

- [ ] Create `pkg/seatbelt/guards/` directory

```bash
mkdir -p pkg/seatbelt/guards
```

### Step 1.2: Move guard files with git mv

- [ ] Move all guard_*.go, registry.go, and their test files to `pkg/seatbelt/guards/`

```bash
cd /Users/subramk/source/github.com/jskswamy/aide

# Move guard implementation files
git mv pkg/seatbelt/modules/guard_base.go pkg/seatbelt/guards/guard_base.go
git mv pkg/seatbelt/modules/guard_system_runtime.go pkg/seatbelt/guards/guard_system_runtime.go
git mv pkg/seatbelt/modules/guard_network.go pkg/seatbelt/guards/guard_network.go
git mv pkg/seatbelt/modules/guard_filesystem.go pkg/seatbelt/guards/guard_filesystem.go
git mv pkg/seatbelt/modules/guard_keychain.go pkg/seatbelt/guards/guard_keychain.go
git mv pkg/seatbelt/modules/guard_node_toolchain.go pkg/seatbelt/guards/guard_node_toolchain.go
git mv pkg/seatbelt/modules/guard_nix_toolchain.go pkg/seatbelt/guards/guard_nix_toolchain.go
git mv pkg/seatbelt/modules/guard_git_integration.go pkg/seatbelt/guards/guard_git_integration.go
git mv pkg/seatbelt/modules/guard_ssh_keys.go pkg/seatbelt/guards/guard_ssh_keys.go
git mv pkg/seatbelt/modules/guard_cloud.go pkg/seatbelt/guards/guard_cloud.go
git mv pkg/seatbelt/modules/guard_kubernetes.go pkg/seatbelt/guards/guard_kubernetes.go
git mv pkg/seatbelt/modules/guard_terraform.go pkg/seatbelt/guards/guard_terraform.go
git mv pkg/seatbelt/modules/guard_vault.go pkg/seatbelt/guards/guard_vault.go
git mv pkg/seatbelt/modules/guard_browsers.go pkg/seatbelt/guards/guard_browsers.go
git mv pkg/seatbelt/modules/guard_password_managers.go pkg/seatbelt/guards/guard_password_managers.go
git mv pkg/seatbelt/modules/guard_aide_secrets.go pkg/seatbelt/guards/guard_aide_secrets.go
git mv pkg/seatbelt/modules/guard_sensitive.go pkg/seatbelt/guards/guard_sensitive.go
git mv pkg/seatbelt/modules/guard_custom.go pkg/seatbelt/guards/guard_custom.go
git mv pkg/seatbelt/modules/registry.go pkg/seatbelt/guards/registry.go

# Move test files
git mv pkg/seatbelt/modules/guard_ssh_keys_test.go pkg/seatbelt/guards/guard_ssh_keys_test.go
git mv pkg/seatbelt/modules/guard_cloud_test.go pkg/seatbelt/guards/guard_cloud_test.go
git mv pkg/seatbelt/modules/guard_browsers_test.go pkg/seatbelt/guards/guard_browsers_test.go
git mv pkg/seatbelt/modules/guard_password_managers_test.go pkg/seatbelt/guards/guard_password_managers_test.go
git mv pkg/seatbelt/modules/guard_sensitive_test.go pkg/seatbelt/guards/guard_sensitive_test.go
git mv pkg/seatbelt/modules/guard_custom_test.go pkg/seatbelt/guards/guard_custom_test.go
git mv pkg/seatbelt/modules/registry_test.go pkg/seatbelt/guards/registry_test.go
```

### Step 1.3: Update package declarations in all moved files

- [ ] Change `package modules` to `package guards` in every moved file

In every file under `pkg/seatbelt/guards/`, replace:

```go
// old
package modules
```

with:

```go
// new
package guards
```

Files to update (all in `/Users/subramk/source/github.com/jskswamy/aide/pkg/seatbelt/guards/`):
- `guard_base.go`
- `guard_system_runtime.go`
- `guard_network.go`
- `guard_filesystem.go`
- `guard_keychain.go`
- `guard_node_toolchain.go`
- `guard_nix_toolchain.go`
- `guard_git_integration.go`
- `guard_ssh_keys.go`
- `guard_cloud.go`
- `guard_kubernetes.go`
- `guard_terraform.go`
- `guard_vault.go`
- `guard_browsers.go`
- `guard_password_managers.go`
- `guard_sensitive.go`
- `guard_custom.go`
- `registry.go`

In every test file under `pkg/seatbelt/guards/`, replace:

```go
// old
package modules_test
```

with:

```go
// new
package guards_test
```

And update the import inside each test file from:

```go
"github.com/jskswamy/aide/pkg/seatbelt/modules"
```

to:

```go
"github.com/jskswamy/aide/pkg/seatbelt/guards"
```

And replace all `modules.` references with `guards.` in test files:
- `guard_ssh_keys_test.go`
- `guard_cloud_test.go`
- `guard_browsers_test.go`
- `guard_password_managers_test.go`
- `guard_sensitive_test.go`
- `guard_custom_test.go`
- `registry_test.go`

### Step 1.4: Update imports in consumer files

- [ ] Update all files that import guards from `modules` to import from `guards`

**File: `/Users/subramk/source/github.com/jskswamy/aide/internal/sandbox/darwin.go`**

Change:
```go
import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/jskswamy/aide/pkg/seatbelt"
	"github.com/jskswamy/aide/pkg/seatbelt/modules"
)
```

To:
```go
import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/jskswamy/aide/pkg/seatbelt"
	"github.com/jskswamy/aide/pkg/seatbelt/guards"
	"github.com/jskswamy/aide/pkg/seatbelt/modules"
)
```

And change `modules.ResolveActiveGuards` to `guards.ResolveActiveGuards` in the function body.

**File: `/Users/subramk/source/github.com/jskswamy/aide/internal/sandbox/sandbox.go`**

Change:
```go
import (
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/jskswamy/aide/pkg/seatbelt"
	"github.com/jskswamy/aide/pkg/seatbelt/modules"
)
```

To:
```go
import (
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/jskswamy/aide/pkg/seatbelt"
	"github.com/jskswamy/aide/pkg/seatbelt/guards"
)
```

And change `modules.DefaultGuardNames()` to `guards.DefaultGuardNames()`.

**File: `/Users/subramk/source/github.com/jskswamy/aide/internal/sandbox/policy.go`**

Change import from:
```go
"github.com/jskswamy/aide/pkg/seatbelt/modules"
```

To:
```go
"github.com/jskswamy/aide/pkg/seatbelt/guards"
```

And replace all `modules.` calls with `guards.`:
- `modules.ExpandGuardName` -> `guards.ExpandGuardName`
- `modules.DefaultGuardNames` -> `guards.DefaultGuardNames`
- `modules.AllGuards` -> `guards.AllGuards`
- `modules.GuardByName` -> `guards.GuardByName`

**File: `/Users/subramk/source/github.com/jskswamy/aide/cmd/aide/commands.go`**

Add import:
```go
"github.com/jskswamy/aide/pkg/seatbelt/guards"
```

And replace all guard-related `modules.` calls with `guards.`:
- `modules.AllGuards()` -> `guards.AllGuards()`
- `modules.DefaultGuardNames()` -> `guards.DefaultGuardNames()`
- `modules.GuardByName(name)` -> `guards.GuardByName(name)`

Keep `modules.ClaudeAgent()` import for agent module usage (it stays in `modules`).

**File: `/Users/subramk/source/github.com/jskswamy/aide/internal/sandbox/darwin_test.go`**

Change import:
```go
"github.com/jskswamy/aide/pkg/seatbelt/modules"
```

To:
```go
"github.com/jskswamy/aide/pkg/seatbelt/guards"
```

And replace `modules.DefaultGuardNames()` with `guards.DefaultGuardNames()`.

**File: `/Users/subramk/source/github.com/jskswamy/aide/internal/sandbox/policy_test.go`**

Change import:
```go
"github.com/jskswamy/aide/pkg/seatbelt/modules"
```

To:
```go
"github.com/jskswamy/aide/pkg/seatbelt/guards"
```

And replace all `modules.` references with `guards.`.

### Step 1.5: Remove old module-level test files that tested guard functionality

- [ ] Remove test files that remain in `modules/` but tested guard functionality

The following test files in `pkg/seatbelt/modules/` tested module-level compat wrappers and will be deleted in Task 3 (Fix 8). For now, check if they compile. If they reference moved types, update their imports. These files are:
- `base_test.go`
- `system_test.go`
- `network_test.go`
- `filesystem_test.go`
- `toolchain_test.go`

These test the deprecated compat wrappers (e.g. `Base()`, `Network()`, `Filesystem()`). They stay in `modules/` for now and will be deleted in Task 3.

### Step 1.6: Verify compilation and tests

- [ ] Run tests to confirm move succeeded

```bash
cd /Users/subramk/source/github.com/jskswamy/aide
go build ./...
go test ./pkg/seatbelt/guards/...
go test ./pkg/seatbelt/modules/...
go test ./internal/sandbox/...
```

### Step 1.7: Commit

- [ ] Commit the package restructure

---

## Task 2: Unify NetworkMode (Fix 5)

Remove `NetworkMode int` type and constants from `pkg/seatbelt/module.go`. Change `Context.Network` to `string`.

### Step 2.1: Write test for string-based network mode

- [ ] Create test verifying network guard works with string network modes

**File: `/Users/subramk/source/github.com/jskswamy/aide/pkg/seatbelt/guards/guard_network_string_test.go`**

```go
package guards_test

import (
	"strings"
	"testing"

	"github.com/jskswamy/aide/pkg/seatbelt"
	"github.com/jskswamy/aide/pkg/seatbelt/guards"
)

func TestNetworkGuard_StringModes(t *testing.T) {
	tests := []struct {
		name    string
		network string
		want    string // substring expected in rules output
		notWant string // substring NOT expected
	}{
		{"unrestricted", "unrestricted", "allow network*", ""},
		{"empty defaults to unrestricted", "", "allow network*", ""},
		{"outbound", "outbound", "allow network-outbound", ""},
		{"none", "none", "", "allow network"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &seatbelt.Context{
				HomeDir: "/tmp/test",
				Network: tt.network,
			}
			g := guards.NetworkGuard()
			rules := g.Rules(ctx)

			var sb strings.Builder
			for _, r := range rules {
				sb.WriteString(r.String())
				sb.WriteString("\n")
			}
			output := sb.String()

			if tt.want != "" && !strings.Contains(output, tt.want) {
				t.Errorf("expected output to contain %q, got:\n%s", tt.want, output)
			}
			if tt.notWant != "" && strings.Contains(output, tt.notWant) {
				t.Errorf("expected output NOT to contain %q, got:\n%s", tt.notWant, output)
			}
		})
	}
}
```

### Step 2.2: Update Context.Network type in module.go

- [ ] Remove `NetworkMode int` type and constants, change `Context.Network` to `string`

**File: `/Users/subramk/source/github.com/jskswamy/aide/pkg/seatbelt/module.go`**

Remove these lines entirely:

```go
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
```

Change in the `Context` struct:

```go
// old
Network     NetworkMode // consumed by network guard
```

```go
// new
Network     string      // "outbound", "none", "unrestricted", or "" (defaults to unrestricted)
```

### Step 2.3: Update network guard to switch on strings

- [ ] Rewrite networkGuard.Rules to use string values

**File: `/Users/subramk/source/github.com/jskswamy/aide/pkg/seatbelt/guards/guard_network.go`**

Replace the `Rules` method:

```go
func (g *networkGuard) Rules(ctx *seatbelt.Context) []seatbelt.Rule {
	if ctx == nil {
		return nil
	}
	switch ctx.Network {
	case "outbound":
		return outboundRules(ctx.AllowPorts, ctx.DenyPorts)
	case "none":
		return nil
	case "unrestricted", "":
		return []seatbelt.Rule{seatbelt.Allow("network*")}
	default:
		return nil
	}
}
```

Also remove the old `seatbelt.NetworkOpen`, `seatbelt.NetworkOutbound`, `seatbelt.NetworkNone` references from the `networkModule` compat wrapper (the compat wrapper itself will be deleted in Task 3, but it must compile until then). For now, update the compat wrapper's switch:

```go
func (m *networkModule) Rules(_ *seatbelt.Context) []seatbelt.Rule {
	switch m.mode {
	case seatbelt.NetworkOpen:
		return []seatbelt.Rule{seatbelt.Allow("network*")}
	case seatbelt.NetworkOutbound:
		return outboundRules(m.opts.AllowPorts, m.opts.DenyPorts)
	case seatbelt.NetworkNone:
		return nil
	default:
		return nil
	}
}
```

Wait -- `seatbelt.NetworkOpen` etc. no longer exist after Step 2.2. The compat types reference them. Since Task 3 deletes these compat wrappers, we need to handle the compilation gap. The simplest approach: delete the compat types/constants from `guard_network.go` NOW as part of this step (they reference the removed `seatbelt.NetworkMode`). The compat wrappers in `guard_network.go` that reference `seatbelt.NetworkMode` are:

- `type NetworkMode = seatbelt.NetworkMode` -- references removed type
- `NetworkModeOpen`, `NetworkModeOutbound`, `NetworkModeNone` constants -- reference removed constants
- `NetworkOpen`, `NetworkOutbound`, `NetworkNone` module-level constants
- `PortOpts` struct
- `networkModule` struct and its methods
- `Network()` function
- `NetworkWithPorts()` function

**Delete all of these from `/Users/subramk/source/github.com/jskswamy/aide/pkg/seatbelt/guards/guard_network.go`** now. The file should become:

```go
// Network guard for macOS Seatbelt profiles.
//
// Controls network access with three modes: unrestricted, outbound-only, and none.
// Supports port-level filtering for outbound connections.

package guards

import (
	"fmt"

	"github.com/jskswamy/aide/pkg/seatbelt"
)

// networkGuard reads network mode and port options from ctx fields.
type networkGuard struct{}

// NetworkGuard returns a Guard that reads ctx.Network, ctx.AllowPorts, ctx.DenyPorts.
func NetworkGuard() seatbelt.Guard { return &networkGuard{} }

func (g *networkGuard) Name() string        { return "network" }
func (g *networkGuard) Type() string        { return "always" }
func (g *networkGuard) Description() string { return "network access control (open/outbound/none)" }

func (g *networkGuard) Rules(ctx *seatbelt.Context) []seatbelt.Rule {
	if ctx == nil {
		return nil
	}
	switch ctx.Network {
	case "outbound":
		return outboundRules(ctx.AllowPorts, ctx.DenyPorts)
	case "none":
		return nil
	case "unrestricted", "":
		return []seatbelt.Rule{seatbelt.Allow("network*")}
	default:
		return nil
	}
}

func outboundRules(allowPorts, denyPorts []int) []seatbelt.Rule {
	if len(allowPorts) > 0 {
		return allowPortRules(allowPorts)
	}
	if len(denyPorts) > 0 {
		return denyPortRules(denyPorts)
	}
	return []seatbelt.Rule{seatbelt.Allow("network-outbound")}
}

func allowPortRules(ports []int) []seatbelt.Rule {
	rules := []seatbelt.Rule{
		seatbelt.Deny("network-outbound"),
	}
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

func denyPortRules(ports []int) []seatbelt.Rule {
	rules := []seatbelt.Rule{
		seatbelt.Allow("network-outbound"),
	}
	for _, port := range ports {
		rules = append(rules,
			seatbelt.Raw(fmt.Sprintf(`(deny network-outbound (remote tcp "*:%d"))`, port)),
		)
	}
	return rules
}
```

### Step 2.4: Update darwin.go to remove NetworkMode int mapping

- [ ] Simplify `generateSeatbeltProfile` to use string directly

**File: `/Users/subramk/source/github.com/jskswamy/aide/internal/sandbox/darwin.go`**

Remove the entire `netMode` mapping block:

```go
// old — DELETE THIS BLOCK
// Map sandbox.NetworkMode (string) to seatbelt.NetworkMode (int)
var netMode seatbelt.NetworkMode
switch policy.Network {
case NetworkNone:
    netMode = seatbelt.NetworkNone
case NetworkOutbound:
    netMode = seatbelt.NetworkOutbound
default:
    netMode = seatbelt.NetworkOpen
}
```

And in the `WithContext` callback, change:

```go
// old
ctx.Network = netMode
```

to:

```go
// new
ctx.Network = string(policy.Network)
```

### Step 2.5: Delete modules-level compat tests that reference old NetworkMode

- [ ] Delete or update test files in `modules/` that reference the removed compat wrappers

**File: `/Users/subramk/source/github.com/jskswamy/aide/pkg/seatbelt/modules/network_test.go`** — Delete this file (it tests `Network()` and `NetworkWithPorts()` compat wrappers that are now removed).

```bash
git rm pkg/seatbelt/modules/network_test.go
```

### Step 2.6: Verify compilation and tests

- [ ] Run tests

```bash
cd /Users/subramk/source/github.com/jskswamy/aide
go build ./...
go test ./pkg/seatbelt/...
go test ./internal/sandbox/...
```

### Step 2.7: Commit

- [ ] Commit the NetworkMode unification

---

## Task 3: Remove Deprecated Backward-Compat Wrappers (Fix 8)

Delete all deprecated aliases and compat types.

### Step 3.1: Remove deprecated functions and types from guard files

- [ ] Remove `Base()` from `guard_base.go`

**File: `/Users/subramk/source/github.com/jskswamy/aide/pkg/seatbelt/guards/guard_base.go`**

Remove:
```go
// Base returns a module that emits the Seatbelt version and default-deny policy.
// Deprecated: use BaseGuard instead.
func Base() seatbelt.Module { return &baseGuard{} }
```

- [ ] Remove `SystemRuntime()` from `guard_system_runtime.go`

**File: `/Users/subramk/source/github.com/jskswamy/aide/pkg/seatbelt/guards/guard_system_runtime.go`**

Remove:
```go
// SystemRuntime returns a module with macOS system runtime rules.
// Deprecated: use SystemRuntimeGuard instead.
func SystemRuntime() seatbelt.Module { return &systemRuntimeGuard{} }
```

- [ ] Remove `KeychainIntegration()` from `guard_keychain.go`

**File: `/Users/subramk/source/github.com/jskswamy/aide/pkg/seatbelt/guards/guard_keychain.go`**

Remove:
```go
// KeychainIntegration returns a module with macOS Keychain sandbox rules.
// Deprecated: use KeychainGuard instead.
func KeychainIntegration() seatbelt.Module { return &keychainGuard{} }
```

- [ ] Remove `NodeToolchain()` from `guard_node_toolchain.go`

**File: `/Users/subramk/source/github.com/jskswamy/aide/pkg/seatbelt/guards/guard_node_toolchain.go`**

Remove:
```go
// NodeToolchain returns a module with Node.js ecosystem sandbox rules.
// Deprecated: use NodeToolchainGuard instead.
func NodeToolchain() seatbelt.Module { return &nodeToolchainGuard{} }
```

- [ ] Remove `NixToolchain()` from `guard_nix_toolchain.go`

**File: `/Users/subramk/source/github.com/jskswamy/aide/pkg/seatbelt/guards/guard_nix_toolchain.go`**

Remove:
```go
// NixToolchain returns a module with Nix package manager sandbox rules.
// Deprecated: use NixToolchainGuard instead.
func NixToolchain() seatbelt.Module { return &nixToolchainGuard{} }
```

- [ ] Remove `GitIntegration()` from `guard_git_integration.go`

**File: `/Users/subramk/source/github.com/jskswamy/aide/pkg/seatbelt/guards/guard_git_integration.go`**

Remove:
```go
// GitIntegration returns a module with Git configuration read-only sandbox rules.
// Deprecated: use GitIntegrationGuard instead.
func GitIntegration() seatbelt.Module { return &gitIntegrationGuard{} }
```

- [ ] Remove `Filesystem()`, `filesystemModule`, and `FilesystemConfig` from `guard_filesystem.go`

**File: `/Users/subramk/source/github.com/jskswamy/aide/pkg/seatbelt/guards/guard_filesystem.go`**

Remove these blocks:

```go
// FilesystemConfig specifies filesystem access rules.
type FilesystemConfig struct {
	// Writable paths get read+write access.
	Writable []string
	// Readable paths get read-only access.
	Readable []string
	// Denied paths are blocked for both read and write.
	// Supports glob patterns.
	Denied []string
}
```

```go
// filesystemModule is the backward-compat wrapper.
type filesystemModule struct {
	cfg FilesystemConfig
}

// Filesystem returns a module that controls filesystem access.
func Filesystem(cfg FilesystemConfig) seatbelt.Module {
	return &filesystemModule{cfg: cfg}
}

func (m *filesystemModule) Name() string { return "Filesystem" }

func (m *filesystemModule) Rules(_ *seatbelt.Context) []seatbelt.Rule {
	return filesystemRules(m.cfg)
}
```

Also update `filesystemGuard.Rules` to inline the logic instead of delegating to `filesystemRules(cfg FilesystemConfig)`. The `filesystemRules` helper used `FilesystemConfig` which is now removed. Refactor:

```go
func (g *filesystemGuard) Rules(ctx *seatbelt.Context) []seatbelt.Rule {
	if ctx == nil {
		return nil
	}

	var writable, readable []string
	if ctx.ProjectRoot != "" {
		writable = append(writable, ctx.ProjectRoot)
	}
	if ctx.HomeDir != "" {
		readable = append(readable, ctx.HomeDir)
	}
	if ctx.RuntimeDir != "" {
		writable = append(writable, ctx.RuntimeDir)
	}
	if ctx.TempDir != "" {
		writable = append(writable, ctx.TempDir)
	}

	var rules []seatbelt.Rule

	if len(writable) > 0 {
		rules = append(rules, seatbelt.Raw(fmt.Sprintf("(allow file-read* file-write*\n    %s)", buildRequireAny(writable))))
	}
	if len(readable) > 0 {
		rules = append(rules, seatbelt.Raw(fmt.Sprintf("(allow file-read*\n    %s)", buildRequireAny(readable))))
	}
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
```

Remove the now-unused `filesystemRules` function.

### Step 3.2: Delete modules-level compat test files

- [ ] Delete test files that tested deprecated wrappers

```bash
cd /Users/subramk/source/github.com/jskswamy/aide
git rm pkg/seatbelt/modules/base_test.go
git rm pkg/seatbelt/modules/system_test.go
git rm pkg/seatbelt/modules/filesystem_test.go
git rm pkg/seatbelt/modules/toolchain_test.go
```

(Note: `network_test.go` was already deleted in Step 2.5)

### Step 3.3: Grep for remaining references to removed symbols

- [ ] Verify no remaining callers

```bash
cd /Users/subramk/source/github.com/jskswamy/aide
grep -rn "modules\.Base()\|guards\.Base()\|\.SystemRuntime()\|\.Network(mode\|\.NetworkWithPorts(\|\.Filesystem(\|\.KeychainIntegration()\|\.NodeToolchain()\|\.NixToolchain()\|\.GitIntegration()\|FilesystemConfig\|networkModuleCompat\|filesystemModule\|PortOpts\|NetworkModeOpen\|NetworkModeOutbound\|NetworkModeNone" --include="*.go" .
```

Fix any remaining references found.

### Step 3.4: Verify compilation and tests

- [ ] Run full test suite

```bash
cd /Users/subramk/source/github.com/jskswamy/aide
go build ./...
go test ./...
```

### Step 3.5: Commit

- [ ] Commit removal of deprecated wrappers

---

## Task 4: Context Validation + ValidationResult (Fix 1)

### Step 4.1: Write tests for ValidationResult

- [ ] Create validation tests

**File: `/Users/subramk/source/github.com/jskswamy/aide/pkg/seatbelt/validation_test.go`**

```go
package seatbelt_test

import (
	"testing"

	"github.com/jskswamy/aide/pkg/seatbelt"
)

func TestValidationResult_Empty(t *testing.T) {
	r := &seatbelt.ValidationResult{}
	if !r.OK() {
		t.Error("empty ValidationResult should be OK")
	}
	if r.Err() != nil {
		t.Error("empty ValidationResult should have nil Err()")
	}
}

func TestValidationResult_AddError(t *testing.T) {
	r := &seatbelt.ValidationResult{}
	r.AddError("field %q is required", "HomeDir")
	if r.OK() {
		t.Error("ValidationResult with error should not be OK")
	}
	if r.Err() == nil {
		t.Error("ValidationResult with error should have non-nil Err()")
	}
	if r.Err().Error() != `field "HomeDir" is required` {
		t.Errorf("unexpected error message: %s", r.Err())
	}
}

func TestValidationResult_AddWarning(t *testing.T) {
	r := &seatbelt.ValidationResult{}
	r.AddWarning("field %q is deprecated", "writable")
	if !r.OK() {
		t.Error("ValidationResult with only warnings should be OK")
	}
	if len(r.Warnings) != 1 {
		t.Errorf("expected 1 warning, got %d", len(r.Warnings))
	}
}

func TestValidationResult_Merge(t *testing.T) {
	r1 := &seatbelt.ValidationResult{}
	r1.AddError("error1")
	r1.AddWarning("warning1")

	r2 := seatbelt.ValidationResult{}
	r2.AddError("error2")
	r2.AddWarning("warning2")

	r1.Merge(r2)
	if len(r1.Errors) != 2 {
		t.Errorf("expected 2 errors after merge, got %d", len(r1.Errors))
	}
	if len(r1.Warnings) != 2 {
		t.Errorf("expected 2 warnings after merge, got %d", len(r1.Warnings))
	}
}

func TestContext_Validate_EmptyHomeDir(t *testing.T) {
	ctx := &seatbelt.Context{GOOS: "darwin"}
	r := ctx.Validate()
	if r.OK() {
		t.Error("expected validation error for empty HomeDir")
	}
}

func TestContext_Validate_EmptyGOOS(t *testing.T) {
	ctx := &seatbelt.Context{HomeDir: "/home/test"}
	r := ctx.Validate()
	if r.OK() {
		t.Error("expected validation error for empty GOOS")
	}
}

func TestContext_Validate_Valid(t *testing.T) {
	ctx := &seatbelt.Context{HomeDir: "/home/test", GOOS: "darwin"}
	r := ctx.Validate()
	if !r.OK() {
		t.Errorf("expected validation to pass, got errors: %v", r.Errors)
	}
}
```

### Step 4.2: Create ValidationResult type

- [ ] Create `/Users/subramk/source/github.com/jskswamy/aide/pkg/seatbelt/validation.go`

```go
package seatbelt

import "fmt"

// ValidationResult holds errors and warnings from validation.
// Used across all validation sites in the guard system.
type ValidationResult struct {
	Errors   []string
	Warnings []string
}

// AddError adds a formatted error message.
func (r *ValidationResult) AddError(format string, args ...interface{}) {
	r.Errors = append(r.Errors, fmt.Sprintf(format, args...))
}

// AddWarning adds a formatted warning message.
func (r *ValidationResult) AddWarning(format string, args ...interface{}) {
	r.Warnings = append(r.Warnings, fmt.Sprintf(format, args...))
}

// Err returns the first error as an error value, or nil if no errors.
func (r *ValidationResult) Err() error {
	if len(r.Errors) == 0 {
		return nil
	}
	return fmt.Errorf("%s", r.Errors[0])
}

// OK returns true if there are no errors.
func (r *ValidationResult) OK() bool {
	return len(r.Errors) == 0
}

// Merge incorporates errors and warnings from another ValidationResult.
func (r *ValidationResult) Merge(other ValidationResult) {
	r.Errors = append(r.Errors, other.Errors...)
	r.Warnings = append(r.Warnings, other.Warnings...)
}
```

### Step 4.3: Add Context.Validate() method

- [ ] Add Validate method to Context in module.go

**File: `/Users/subramk/source/github.com/jskswamy/aide/pkg/seatbelt/module.go`**

Add after the `EnvLookup` method:

```go
// Validate checks that required Context fields are set.
// Returns a ValidationResult with errors for missing fields.
func (c *Context) Validate() *ValidationResult {
	r := &ValidationResult{}
	if c.HomeDir == "" {
		r.AddError("context: HomeDir is required for guard path resolution")
	}
	if c.GOOS == "" {
		r.AddError("context: GOOS is required for OS-aware guards")
	}
	return r
}
```

### Step 4.4: Update generateSeatbeltProfile to set GOOS and validate context

- [ ] Add GOOS assignment and context validation in darwin.go

**File: `/Users/subramk/source/github.com/jskswamy/aide/internal/sandbox/darwin.go`**

Add `"runtime"` to imports.

Update `generateSeatbeltProfile` to:
1. Check that "base" is in the Guards list
2. Set GOOS on the context
3. Validate the context

Replace the function body:

```go
func generateSeatbeltProfile(policy Policy) (string, error) {
	// Validate base guard is present
	hasBase := false
	for _, name := range policy.Guards {
		if name == "base" {
			hasBase = true
			break
		}
	}
	if !hasBase {
		return "", fmt.Errorf("guard 'base' is required but not in Guards list")
	}

	homeDir, _ := os.UserHomeDir()

	// Resolve active guards from names
	guardModules := guards.ResolveActiveGuards(policy.Guards)

	// Create profile with context
	p := seatbelt.New(homeDir).
		WithContext(func(ctx *seatbelt.Context) {
			ctx.ProjectRoot = policy.ProjectRoot
			ctx.TempDir = policy.TempDir
			ctx.RuntimeDir = policy.RuntimeDir
			ctx.Env = policy.Env
			ctx.Network = string(policy.Network)
			ctx.AllowPorts = policy.AllowPorts
			ctx.DenyPorts = policy.DenyPorts
			ctx.ExtraDenied = policy.ExtraDenied
			ctx.GOOS = runtime.GOOS
		})

	// Use each guard module
	for _, g := range guardModules {
		p.Use(g)
	}

	// Use agent module if set
	if policy.AgentModule != nil {
		p.Use(policy.AgentModule)
	}

	return p.Render()
}
```

### Step 4.5: Update ValidateCustomGuard to use ValidationResult

- [ ] Update ValidateCustomGuard in guard_custom.go

**File: `/Users/subramk/source/github.com/jskswamy/aide/pkg/seatbelt/guards/guard_custom.go`**

Replace the `ValidateCustomGuard` function:

```go
// ValidateCustomGuard checks the configuration for common mistakes.
// Returns a ValidationResult with any errors found.
func ValidateCustomGuard(name string, cfg CustomGuardConfig) *seatbelt.ValidationResult {
	r := &seatbelt.ValidationResult{}

	if cfg.Type == "always" {
		r.AddError("custom guard %q: type \"always\" is not allowed for custom guards", name)
	}

	if _, builtin := GuardByName(name); builtin {
		r.AddError("custom guard %q: name collides with a built-in guard", name)
	}

	if cfg.EnvOverride != "" && len(cfg.Paths) > 1 {
		r.AddError("custom guard %q: EnvOverride cannot be used with multiple paths", name)
	}

	if len(cfg.Paths) == 0 {
		r.AddError("custom guard %q: at least one path is required", name)
	}

	return r
}
```

### Step 4.6: Update callers of ValidateCustomGuard

- [ ] Find and update all callers of ValidateCustomGuard

Search for `ValidateCustomGuard` callers and update them. Previous callers checked `err != nil`; now they check `!result.OK()`.

```bash
cd /Users/subramk/source/github.com/jskswamy/aide
grep -rn "ValidateCustomGuard" --include="*.go" .
```

Update each caller from:
```go
if err := modules.ValidateCustomGuard(name, cfg); err != nil {
    return err
}
```
to:
```go
if result := guards.ValidateCustomGuard(name, cfg); !result.OK() {
    return result.Err()
}
```

### Step 4.7: Migrate sandbox.ValidationResult to use seatbelt.ValidationResult

- [ ] Update `internal/sandbox/policy.go` to use `seatbelt.ValidationResult`

**File: `/Users/subramk/source/github.com/jskswamy/aide/internal/sandbox/policy.go`**

Remove the local `ValidationResult` type:

```go
// DELETE:
// ValidationResult holds the results of a detailed sandbox config validation.
type ValidationResult struct {
	Errors   []string
	Warnings []string
}
```

Add `"github.com/jskswamy/aide/pkg/seatbelt"` to imports.

Change `ValidateSandboxConfigDetailed` return type:

```go
// old
func ValidateSandboxConfigDetailed(cfg *config.SandboxPolicy) ValidationResult {
```

```go
// new
func ValidateSandboxConfigDetailed(cfg *config.SandboxPolicy) seatbelt.ValidationResult {
```

Update all `result.Errors = append(result.Errors, ...)` to `result.AddError(...)` and `result.Warnings = append(result.Warnings, ...)` to `result.AddWarning(...)`.

Full rewrite of `ValidateSandboxConfigDetailed`:

```go
func ValidateSandboxConfigDetailed(cfg *config.SandboxPolicy) seatbelt.ValidationResult {
	var result seatbelt.ValidationResult
	if cfg == nil {
		return result
	}

	// Validate network mode
	if cfg.Network != nil {
		validNetworkModes := map[string]bool{
			"outbound": true, "none": true, "unrestricted": true, "": true,
		}
		if !validNetworkModes[cfg.Network.Mode] {
			result.AddError(
				"sandbox.network: invalid value %q, must be one of: outbound, none, unrestricted",
				cfg.Network.Mode,
			)
		}

		for _, port := range cfg.Network.AllowPorts {
			if port < 1 || port > 65535 {
				result.AddError("sandbox.network.allow_ports: invalid port %d, must be 1-65535", port)
			}
		}

		for _, port := range cfg.Network.DenyPorts {
			if port < 1 || port > 65535 {
				result.AddError("sandbox.network.deny_ports: invalid port %d, must be 1-65535", port)
			}
		}
	}

	if len(cfg.Denied) > 0 && len(cfg.DeniedExtra) > 0 {
		result.AddWarning("sandbox: both denied and denied_extra are set; denied_extra is ignored when denied is specified")
	}

	if len(cfg.Writable) > 0 && len(cfg.WritableExtra) > 0 {
		result.AddWarning("sandbox: both writable and writable_extra are set; writable_extra is ignored when writable is specified")
	}

	if len(cfg.Readable) > 0 && len(cfg.ReadableExtra) > 0 {
		result.AddWarning("sandbox: both readable and readable_extra are set; readable_extra is ignored when readable is specified")
	}

	if len(cfg.Guards) > 0 && len(cfg.GuardsExtra) > 0 {
		result.AddWarning("sandbox: both guards and guards_extra are set; guards_extra is ignored when guards is specified")
	}

	for _, w := range cfg.Writable {
		if w == "~" || w == "~/" || isHomeDirPath(w) {
			result.AddWarning("sandbox.writable: %q includes the entire home directory, which is very broad", w)
		}
	}

	for _, name := range cfg.Guards {
		expanded := guards.ExpandGuardName(name)
		for _, n := range expanded {
			if _, ok := guards.GuardByName(n); !ok {
				result.AddError("sandbox.guards: unknown guard name %q", n)
			}
		}
	}

	for _, name := range cfg.GuardsExtra {
		expanded := guards.ExpandGuardName(name)
		for _, n := range expanded {
			if _, ok := guards.GuardByName(n); !ok {
				result.AddError("sandbox.guards_extra: unknown guard name %q", n)
			}
		}
	}

	for _, name := range cfg.Unguard {
		expanded := guards.ExpandGuardName(name)
		for _, n := range expanded {
			g, ok := guards.GuardByName(n)
			if !ok {
				result.AddError("sandbox.unguard: unknown guard name %q", n)
				continue
			}
			if g.Type() == "always" {
				result.AddError("sandbox.unguard: cannot unguard %q (type %q is always-on)", n, g.Type())
			}
		}
	}

	return result
}
```

Update `ValidateSandboxConfig` to match:

```go
func ValidateSandboxConfig(cfg *config.SandboxPolicy) error {
	result := ValidateSandboxConfigDetailed(cfg)
	return result.Err()
}
```

### Step 4.8: Verify compilation and tests

- [ ] Run tests

```bash
cd /Users/subramk/source/github.com/jskswamy/aide
go build ./...
go test ./pkg/seatbelt/...
go test ./internal/sandbox/...
```

### Step 4.9: Commit

- [ ] Commit validation and ValidationResult

---

## Task 5: Typed Deny Helpers (Fix 3)

### Step 5.1: Write tests for DenyDir, DenyFile, AllowReadFile helpers

- [ ] Create helper tests

**File: `/Users/subramk/source/github.com/jskswamy/aide/pkg/seatbelt/guards/helpers_test.go`**

```go
package guards_test

import (
	"strings"
	"testing"

	"github.com/jskswamy/aide/pkg/seatbelt/guards"
)

func TestDenyDir(t *testing.T) {
	rules := guards.DenyDir("/Users/test/.ssh")
	if len(rules) != 2 {
		t.Fatalf("expected 2 rules, got %d", len(rules))
	}
	r0 := rules[0].String()
	r1 := rules[1].String()
	if !strings.Contains(r0, `(deny file-read-data (subpath "/Users/test/.ssh"))`) {
		t.Errorf("unexpected rule[0]: %s", r0)
	}
	if !strings.Contains(r1, `(deny file-write* (subpath "/Users/test/.ssh"))`) {
		t.Errorf("unexpected rule[1]: %s", r1)
	}
}

func TestDenyFile(t *testing.T) {
	rules := guards.DenyFile("/Users/test/.aws/credentials")
	if len(rules) != 2 {
		t.Fatalf("expected 2 rules, got %d", len(rules))
	}
	r0 := rules[0].String()
	r1 := rules[1].String()
	if !strings.Contains(r0, `(deny file-read-data (literal "/Users/test/.aws/credentials"))`) {
		t.Errorf("unexpected rule[0]: %s", r0)
	}
	if !strings.Contains(r1, `(deny file-write* (literal "/Users/test/.aws/credentials"))`) {
		t.Errorf("unexpected rule[1]: %s", r1)
	}
}

func TestAllowReadFile(t *testing.T) {
	rule := guards.AllowReadFile("/Users/test/.ssh/known_hosts")
	s := rule.String()
	if !strings.Contains(s, `(allow file-read* (literal "/Users/test/.ssh/known_hosts"))`) {
		t.Errorf("unexpected rule: %s", s)
	}
}

func TestSplitColonPaths(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"/a:/b:/c", 3},
		{"/a::/b", 2},   // empty segment skipped
		{"", 0},
		{"/single", 1},
	}
	for _, tt := range tests {
		got := guards.SplitColonPaths(tt.input)
		if len(got) != tt.want {
			t.Errorf("SplitColonPaths(%q): expected %d parts, got %d: %v", tt.input, tt.want, len(got), got)
		}
	}
}
```

### Step 5.2: Create helpers.go

- [ ] Create `/Users/subramk/source/github.com/jskswamy/aide/pkg/seatbelt/guards/helpers.go`

```go
// Typed deny helpers for guard implementations.
//
// DenyDir uses (subpath ...) for directory trees.
// DenyFile uses (literal ...) for single files.
// This distinction prevents the OCI guard bug (C3) where a file path was
// incorrectly denied with subpath.

package guards

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/jskswamy/aide/pkg/seatbelt"
)

// DenyDir denies read+write to a directory tree using (subpath ...).
func DenyDir(path string) []seatbelt.Rule {
	return []seatbelt.Rule{
		seatbelt.Raw(fmt.Sprintf(`(deny file-read-data (subpath "%s"))`, path)),
		seatbelt.Raw(fmt.Sprintf(`(deny file-write* (subpath "%s"))`, path)),
	}
}

// DenyFile denies read+write to a single file using (literal ...).
func DenyFile(path string) []seatbelt.Rule {
	clean := filepath.Clean(path)
	return []seatbelt.Rule{
		seatbelt.Raw(fmt.Sprintf(`(deny file-read-data (literal "%s"))`, clean)),
		seatbelt.Raw(fmt.Sprintf(`(deny file-write* (literal "%s"))`, clean)),
	}
}

// AllowReadFile allows reading a single file using (literal ...).
func AllowReadFile(path string) seatbelt.Rule {
	return seatbelt.Raw(fmt.Sprintf(`(allow file-read* (literal "%s"))`, filepath.Clean(path)))
}

// EnvOverridePath returns env var value if set, otherwise joins homeDir with defaultRel.
func EnvOverridePath(ctx *seatbelt.Context, envKey, defaultRel string) string {
	if v, ok := ctx.EnvLookup(envKey); ok && v != "" {
		return v
	}
	return ctx.HomePath(defaultRel)
}

// SplitColonPaths splits colon-separated paths, skipping empty segments.
func SplitColonPaths(s string) []string {
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

### Step 5.3: Remove dead code from guard_cloud.go

- [ ] Remove `denyPathRules`, `denyLiteralRules` and the old per-path helpers, replace with DenyDir/DenyFile

**File: `/Users/subramk/source/github.com/jskswamy/aide/pkg/seatbelt/guards/guard_cloud.go`**

Remove these functions:
- `denyPathRules` (dead code, broken relative paths)
- `denyLiteralRules` (dead code, broken relative paths)
- `envOverridePath` (moved to helpers.go as `EnvOverridePath`)
- `splitColonPaths` (moved to helpers.go as `SplitColonPaths`)
- `denySubpathRuleForPath` (replaced by `DenyDir`)
- `denyLiteralRuleForPath` (replaced by `DenyFile`)

Remove the `"path/filepath"` and `"strings"` imports if no longer needed (they won't be after removing these helpers).

### Step 5.4: Update all guards to use DenyDir/DenyFile/EnvOverridePath

- [ ] Update `guard_cloud.go` guards

**File: `/Users/subramk/source/github.com/jskswamy/aide/pkg/seatbelt/guards/guard_cloud.go`**

Rewrite to:

```go
// Cloud provider credential guards for macOS Seatbelt profiles.
//
// Protects credentials for AWS, GCP, Azure, DigitalOcean, and OCI.

package guards

import "github.com/jskswamy/aide/pkg/seatbelt"

// CloudGuardNames returns all guard names provided by this file and related guards.
func CloudGuardNames() []string {
	return []string{
		"cloud-aws", "cloud-gcp", "cloud-azure", "cloud-digitalocean", "cloud-oci",
		"kubernetes", "terraform", "vault",
	}
}

// --- cloud-aws ---

type cloudAWSGuard struct{}

func CloudAWSGuard() seatbelt.Guard { return &cloudAWSGuard{} }

func (g *cloudAWSGuard) Name() string        { return "cloud-aws" }
func (g *cloudAWSGuard) Type() string        { return "default" }
func (g *cloudAWSGuard) Description() string { return "AWS credentials and config files" }

func (g *cloudAWSGuard) Rules(ctx *seatbelt.Context) []seatbelt.Rule {
	credsPath := EnvOverridePath(ctx, "AWS_SHARED_CREDENTIALS_FILE", ".aws/credentials")
	configPath := EnvOverridePath(ctx, "AWS_CONFIG_FILE", ".aws/config")

	var rules []seatbelt.Rule
	rules = append(rules, seatbelt.Section("AWS credentials"))
	rules = append(rules, DenyFile(credsPath)...)
	rules = append(rules, DenyFile(configPath)...)
	rules = append(rules, DenyDir(ctx.HomePath(".aws/sso/cache"))...)
	rules = append(rules, DenyDir(ctx.HomePath(".aws/cli/cache"))...)
	return rules
}

// --- cloud-gcp ---

type cloudGCPGuard struct{}

func CloudGCPGuard() seatbelt.Guard { return &cloudGCPGuard{} }

func (g *cloudGCPGuard) Name() string        { return "cloud-gcp" }
func (g *cloudGCPGuard) Type() string        { return "default" }
func (g *cloudGCPGuard) Description() string { return "GCP gcloud config directory and application credentials" }

func (g *cloudGCPGuard) Rules(ctx *seatbelt.Context) []seatbelt.Rule {
	gcloudPath := EnvOverridePath(ctx, "CLOUDSDK_CONFIG", ".config/gcloud")

	var rules []seatbelt.Rule
	rules = append(rules, seatbelt.Section("GCP credentials"))
	rules = append(rules, DenyDir(gcloudPath)...)

	if saPath, ok := ctx.EnvLookup("GOOGLE_APPLICATION_CREDENTIALS"); ok && saPath != "" {
		rules = append(rules, DenyFile(saPath)...)
	}
	return rules
}

// --- cloud-azure ---

type cloudAzureGuard struct{}

func CloudAzureGuard() seatbelt.Guard { return &cloudAzureGuard{} }

func (g *cloudAzureGuard) Name() string        { return "cloud-azure" }
func (g *cloudAzureGuard) Type() string        { return "default" }
func (g *cloudAzureGuard) Description() string { return "Azure CLI config directory" }

func (g *cloudAzureGuard) Rules(ctx *seatbelt.Context) []seatbelt.Rule {
	azurePath := EnvOverridePath(ctx, "AZURE_CONFIG_DIR", ".azure")

	var rules []seatbelt.Rule
	rules = append(rules, seatbelt.Section("Azure credentials"))
	rules = append(rules, DenyDir(azurePath)...)
	return rules
}

// --- cloud-digitalocean ---

type cloudDigitalOceanGuard struct{}

func CloudDigitalOceanGuard() seatbelt.Guard { return &cloudDigitalOceanGuard{} }

func (g *cloudDigitalOceanGuard) Name() string        { return "cloud-digitalocean" }
func (g *cloudDigitalOceanGuard) Type() string        { return "default" }
func (g *cloudDigitalOceanGuard) Description() string { return "DigitalOcean doctl config directory" }

func (g *cloudDigitalOceanGuard) Rules(ctx *seatbelt.Context) []seatbelt.Rule {
	var rules []seatbelt.Rule
	rules = append(rules, seatbelt.Section("DigitalOcean credentials"))
	rules = append(rules, DenyDir(ctx.HomePath(".config/doctl"))...)
	return rules
}

// --- cloud-oci ---

type cloudOCIGuard struct{}

func CloudOCIGuard() seatbelt.Guard { return &cloudOCIGuard{} }

func (g *cloudOCIGuard) Name() string        { return "cloud-oci" }
func (g *cloudOCIGuard) Type() string        { return "default" }
func (g *cloudOCIGuard) Description() string { return "Oracle Cloud CLI config directory" }

func (g *cloudOCIGuard) Rules(ctx *seatbelt.Context) []seatbelt.Rule {
	var rules []seatbelt.Rule
	rules = append(rules, seatbelt.Section("OCI credentials"))

	// OCI_CLI_CONFIG_FILE points to a single file; default ~/.oci is a directory
	if ociPath, ok := ctx.EnvLookup("OCI_CLI_CONFIG_FILE"); ok && ociPath != "" {
		rules = append(rules, DenyFile(ociPath)...)
	} else {
		rules = append(rules, DenyDir(ctx.HomePath(".oci"))...)
	}
	return rules
}
```

- [ ] Update `guard_kubernetes.go`

**File: `/Users/subramk/source/github.com/jskswamy/aide/pkg/seatbelt/guards/guard_kubernetes.go`**

```go
package guards

import "github.com/jskswamy/aide/pkg/seatbelt"

type kubernetesGuard struct{}

func KubernetesGuard() seatbelt.Guard { return &kubernetesGuard{} }

func (g *kubernetesGuard) Name() string        { return "kubernetes" }
func (g *kubernetesGuard) Type() string        { return "default" }
func (g *kubernetesGuard) Description() string { return "Kubernetes kubeconfig files" }

func (g *kubernetesGuard) Rules(ctx *seatbelt.Context) []seatbelt.Rule {
	var rules []seatbelt.Rule
	rules = append(rules, seatbelt.Section("Kubernetes credentials"))

	if kubeconfig, ok := ctx.EnvLookup("KUBECONFIG"); ok && kubeconfig != "" {
		for _, p := range SplitColonPaths(kubeconfig) {
			rules = append(rules, DenyFile(p)...)
		}
		return rules
	}

	rules = append(rules, DenyFile(ctx.HomePath(".kube/config"))...)
	return rules
}
```

- [ ] Update `guard_terraform.go`

**File: `/Users/subramk/source/github.com/jskswamy/aide/pkg/seatbelt/guards/guard_terraform.go`**

```go
package guards

import "github.com/jskswamy/aide/pkg/seatbelt"

type terraformGuard struct{}

func TerraformGuard() seatbelt.Guard { return &terraformGuard{} }

func (g *terraformGuard) Name() string        { return "terraform" }
func (g *terraformGuard) Type() string        { return "default" }
func (g *terraformGuard) Description() string { return "Terraform credential and config files" }

func (g *terraformGuard) Rules(ctx *seatbelt.Context) []seatbelt.Rule {
	var rules []seatbelt.Rule
	rules = append(rules, seatbelt.Section("Terraform credentials"))

	if cliConfig, ok := ctx.EnvLookup("TF_CLI_CONFIG_FILE"); ok && cliConfig != "" {
		rules = append(rules, DenyFile(cliConfig)...)
		return rules
	}

	rules = append(rules, DenyFile(ctx.HomePath(".terraform.d/credentials.tfrc.json"))...)
	rules = append(rules, DenyFile(ctx.HomePath(".terraformrc"))...)
	return rules
}
```

- [ ] Update `guard_vault.go`

**File: `/Users/subramk/source/github.com/jskswamy/aide/pkg/seatbelt/guards/guard_vault.go`**

```go
package guards

import "github.com/jskswamy/aide/pkg/seatbelt"

type vaultGuard struct{}

func VaultGuard() seatbelt.Guard { return &vaultGuard{} }

func (g *vaultGuard) Name() string        { return "vault" }
func (g *vaultGuard) Type() string        { return "default" }
func (g *vaultGuard) Description() string { return "HashiCorp Vault token file" }

func (g *vaultGuard) Rules(ctx *seatbelt.Context) []seatbelt.Rule {
	tokenPath := EnvOverridePath(ctx, "VAULT_TOKEN_FILE", ".vault-token")

	var rules []seatbelt.Rule
	rules = append(rules, seatbelt.Section("Vault credentials"))
	rules = append(rules, DenyFile(tokenPath)...)
	return rules
}
```

- [ ] Update `guard_sensitive.go` (docker, github-cli, npm, netrc, vercel)

**File: `/Users/subramk/source/github.com/jskswamy/aide/pkg/seatbelt/guards/guard_sensitive.go`**

```go
package guards

import (
	"path/filepath"

	"github.com/jskswamy/aide/pkg/seatbelt"
)

// --- docker ---

type dockerGuard struct{}

func DockerGuard() seatbelt.Guard { return &dockerGuard{} }

func (g *dockerGuard) Name() string        { return "docker" }
func (g *dockerGuard) Type() string        { return "opt-in" }
func (g *dockerGuard) Description() string { return "Docker config.json (registry credentials)" }

func (g *dockerGuard) Rules(ctx *seatbelt.Context) []seatbelt.Rule {
	var configDir string
	if v, ok := ctx.EnvLookup("DOCKER_CONFIG"); ok && v != "" {
		configDir = v
	} else {
		configDir = ctx.HomePath(".docker")
	}
	configFile := filepath.Join(configDir, "config.json")

	var rules []seatbelt.Rule
	rules = append(rules, seatbelt.Section("Docker credentials"))
	rules = append(rules, DenyFile(configFile)...)
	return rules
}

// --- github-cli ---

type githubCLIGuard struct{}

func GithubCLIGuard() seatbelt.Guard { return &githubCLIGuard{} }

func (g *githubCLIGuard) Name() string        { return "github-cli" }
func (g *githubCLIGuard) Type() string        { return "opt-in" }
func (g *githubCLIGuard) Description() string { return "GitHub CLI auth tokens (~/.config/gh)" }

func (g *githubCLIGuard) Rules(ctx *seatbelt.Context) []seatbelt.Rule {
	var rules []seatbelt.Rule
	rules = append(rules, seatbelt.Section("GitHub CLI credentials"))
	rules = append(rules, DenyDir(ctx.HomePath(".config/gh"))...)
	return rules
}

// --- npm ---

type npmGuard struct{}

func NPMGuard() seatbelt.Guard { return &npmGuard{} }

func (g *npmGuard) Name() string        { return "npm" }
func (g *npmGuard) Type() string        { return "opt-in" }
func (g *npmGuard) Description() string { return "npm and yarn registry credentials (~/.npmrc, ~/.yarnrc)" }

func (g *npmGuard) Rules(ctx *seatbelt.Context) []seatbelt.Rule {
	var rules []seatbelt.Rule
	rules = append(rules, seatbelt.Section("npm/yarn credentials"))
	rules = append(rules, DenyFile(ctx.HomePath(".npmrc"))...)
	rules = append(rules, DenyFile(ctx.HomePath(".yarnrc"))...)
	return rules
}

// --- netrc ---

type netrcGuard struct{}

func NetrcGuard() seatbelt.Guard { return &netrcGuard{} }

func (g *netrcGuard) Name() string        { return "netrc" }
func (g *netrcGuard) Type() string        { return "opt-in" }
func (g *netrcGuard) Description() string { return "netrc credentials file (~/.netrc)" }

func (g *netrcGuard) Rules(ctx *seatbelt.Context) []seatbelt.Rule {
	var rules []seatbelt.Rule
	rules = append(rules, seatbelt.Section("netrc credentials"))
	rules = append(rules, DenyFile(ctx.HomePath(".netrc"))...)
	return rules
}

// --- vercel ---

type vercelGuard struct{}

func VercelGuard() seatbelt.Guard { return &vercelGuard{} }

func (g *vercelGuard) Name() string        { return "vercel" }
func (g *vercelGuard) Type() string        { return "opt-in" }
func (g *vercelGuard) Description() string { return "Vercel CLI credentials (~/.config/vercel)" }

func (g *vercelGuard) Rules(ctx *seatbelt.Context) []seatbelt.Rule {
	var rules []seatbelt.Rule
	rules = append(rules, seatbelt.Section("Vercel credentials"))
	rules = append(rules, DenyDir(ctx.HomePath(".config/vercel"))...)
	return rules
}
```

- [ ] Update `guard_password_managers.go`

**File: `/Users/subramk/source/github.com/jskswamy/aide/pkg/seatbelt/guards/guard_password_managers.go`**

Replace `denySubpathRuleForPath` calls with `DenyDir`:

```go
package guards

import "github.com/jskswamy/aide/pkg/seatbelt"

type passwordManagersGuard struct{}

func PasswordManagersGuard() seatbelt.Guard { return &passwordManagersGuard{} }

func (g *passwordManagersGuard) Name() string        { return "password-managers" }
func (g *passwordManagersGuard) Type() string        { return "default" }
func (g *passwordManagersGuard) Description() string { return "1Password, Bitwarden, pass, gopass, and GPG private keys" }

func (g *passwordManagersGuard) Rules(ctx *seatbelt.Context) []seatbelt.Rule {
	var rules []seatbelt.Rule

	rules = append(rules, seatbelt.Section("1Password CLI"))
	rules = append(rules, DenyDir(ctx.HomePath(".config/op"))...)
	rules = append(rules, DenyDir(ctx.HomePath(".op"))...)

	rules = append(rules, seatbelt.Section("Bitwarden CLI"))
	rules = append(rules, DenyDir(ctx.HomePath(".config/Bitwarden CLI"))...)

	rules = append(rules, seatbelt.Section("pass"))
	rules = append(rules, DenyDir(ctx.HomePath(".password-store"))...)

	rules = append(rules, seatbelt.Section("gopass"))
	rules = append(rules, DenyDir(ctx.HomePath(".local/share/gopass"))...)

	rules = append(rules, seatbelt.Section("GPG private keys"))
	rules = append(rules, DenyDir(ctx.HomePath(".gnupg"))...)

	return rules
}

// --- aide-secrets ---

type aideSecretsGuard struct{}

func AideSecretsGuard() seatbelt.Guard { return &aideSecretsGuard{} }

func (g *aideSecretsGuard) Name() string        { return "aide-secrets" }
func (g *aideSecretsGuard) Type() string        { return "default" }
func (g *aideSecretsGuard) Description() string { return "aide secrets store (~/.config/aide/secrets)" }

func (g *aideSecretsGuard) Rules(ctx *seatbelt.Context) []seatbelt.Rule {
	var rules []seatbelt.Rule
	rules = append(rules, seatbelt.Section("aide secrets"))
	rules = append(rules, DenyDir(ctx.HomePath(".config/aide/secrets"))...)
	return rules
}
```

- [ ] Update `guard_browsers.go`

**File: `/Users/subramk/source/github.com/jskswamy/aide/pkg/seatbelt/guards/guard_browsers.go`**

Replace inline deny rule construction with `DenyDir`:

```go
package guards

import "github.com/jskswamy/aide/pkg/seatbelt"

type browsersGuard struct{}

func BrowsersGuard() seatbelt.Guard { return &browsersGuard{} }

func (g *browsersGuard) Name() string        { return "browsers" }
func (g *browsersGuard) Type() string        { return "default" }
func (g *browsersGuard) Description() string { return "Browser profile directories (cookies, passwords, history)" }

func (g *browsersGuard) Rules(ctx *seatbelt.Context) []seatbelt.Rule {
	var rules []seatbelt.Rule
	rules = append(rules, seatbelt.Section("Browser profiles"))

	switch ctx.GOOS {
	case "linux":
		rules = append(rules, g.linuxRules(ctx)...)
	default:
		rules = append(rules, g.darwinRules(ctx)...)
	}
	return rules
}

func (g *browsersGuard) darwinRules(ctx *seatbelt.Context) []seatbelt.Rule {
	appSupport := ctx.HomePath("Library/Application Support")

	browsers := []string{
		"Google/Chrome",
		"Google/Chrome Canary",
		"Firefox",
		"Safari",
		"BraveSoftware/Brave-Browser",
		"Microsoft Edge",
		"Arc",
		"Chromium",
	}

	var rules []seatbelt.Rule
	for _, b := range browsers {
		rules = append(rules, DenyDir(appSupport+"/"+b)...)
	}
	return rules
}

func (g *browsersGuard) linuxRules(ctx *seatbelt.Context) []seatbelt.Rule {
	configDir := ctx.HomePath(".config")
	mozillaDir := ctx.HomePath(".mozilla")
	snapDir := ctx.HomePath("snap")

	browsers := []struct {
		base string
		name string
	}{
		{configDir, "google-chrome"},
		{configDir, "google-chrome-beta"},
		{mozillaDir, "firefox"},
		{configDir, "BraveSoftware/Brave-Browser"},
		{configDir, "microsoft-edge"},
		{configDir, "chromium"},
		{snapDir, "chromium"},
	}

	var rules []seatbelt.Rule
	for _, b := range browsers {
		rules = append(rules, DenyDir(b.base+"/"+b.name)...)
	}
	return rules
}
```

- [ ] Update `guard_ssh_keys.go`

**File: `/Users/subramk/source/github.com/jskswamy/aide/pkg/seatbelt/guards/guard_ssh_keys.go`**

The SSH keys guard uses a more complex pattern (deny dir + allow specific files). It uses `seatbelt.HomeSubpath` and `seatbelt.HomeLiteral` directly. Leave this guard as-is since it has a non-standard deny+allow pattern that doesn't map cleanly to `DenyDir`/`DenyFile` alone. The existing implementation is correct.

### Step 5.5: Verify compilation and tests

- [ ] Run tests

```bash
cd /Users/subramk/source/github.com/jskswamy/aide
go build ./...
go test ./pkg/seatbelt/guards/...
go test ./internal/sandbox/...
```

### Step 5.6: Commit

- [ ] Commit typed deny helpers

---

## Task 6: Description Rewrites + CLI Messages + Banner (Fix 4)

### Step 6.1: Rewrite all guard Description() methods

- [ ] Update descriptions in every guard file

Apply these changes across all guard files in `/Users/subramk/source/github.com/jskswamy/aide/pkg/seatbelt/guards/`:

| File | Guard | Old Description | New Description |
|------|-------|----------------|-----------------|
| `guard_base.go` | `base` | `(version 1), (deny default)` | `Sandbox foundation -- blocks all access unless explicitly allowed` |
| `guard_system_runtime.go` | `system-runtime` | `macOS system runtime paths, devices, and Mach services` | `System binaries, devices, and OS services for agent operation` |
| `guard_network.go` | `network` | `network access control (open/outbound/none)` | `Network access for agent operation` |
| `guard_filesystem.go` | `filesystem` | `project and home filesystem access` | `Project directory (read-write) and home directory (read-only) access` |
| `guard_keychain.go` | `keychain` | `macOS Keychain read/write and security Mach services` | `macOS Keychain access for authentication and certificates` |
| `guard_node_toolchain.go` | `node-toolchain` | `Node.js, npm, yarn, pnpm, and browser testing tool paths` | `Node.js package managers and build tool access` |
| `guard_nix_toolchain.go` | `nix-toolchain` | `Nix store, system paths, and user profile` | `Nix store and profile access` |
| `guard_git_integration.go` | `git-integration` | `Git config and SSH keys (read-only)` | `Git config and SSH host verification (read-only)` |
| `guard_ssh_keys.go` | `ssh-keys` | `SSH keys directory (deny) with known_hosts+config allow` | `Blocks access to SSH private keys; allows known_hosts and config` |
| `guard_cloud.go` | `cloud-aws` | `AWS credentials and config files` | `Blocks access to AWS credentials and config` |
| `guard_cloud.go` | `cloud-gcp` | `GCP gcloud config directory and application credentials` | `Blocks access to GCP credentials and config` |
| `guard_cloud.go` | `cloud-azure` | `Azure CLI config directory` | `Blocks access to Azure CLI credentials` |
| `guard_cloud.go` | `cloud-digitalocean` | `DigitalOcean doctl config directory` | `Blocks access to DigitalOcean CLI credentials` |
| `guard_cloud.go` | `cloud-oci` | `Oracle Cloud CLI config directory` | `Blocks access to Oracle Cloud CLI credentials` |
| `guard_kubernetes.go` | `kubernetes` | `Kubernetes kubeconfig files` | `Blocks access to Kubernetes config` |
| `guard_terraform.go` | `terraform` | `Terraform credential and config files` | `Blocks access to Terraform credentials` |
| `guard_vault.go` | `vault` | `HashiCorp Vault token file` | `Blocks access to Vault token` |
| `guard_browsers.go` | `browsers` | `Browser profile directories (cookies, passwords, history)` | `Blocks access to browser data (cookies, passwords, history)` |
| `guard_password_managers.go` | `password-managers` | `1Password, Bitwarden, pass, gopass, and GPG private keys` | `Blocks access to password manager data and GPG private keys` |
| `guard_password_managers.go` | `aide-secrets` | `aide secrets store (~/.config/aide/secrets)` | `Blocks access to aide's encrypted secrets` |
| `guard_sensitive.go` | `docker` | `Docker config.json (registry credentials)` | `Blocks access to Docker registry credentials` |
| `guard_sensitive.go` | `github-cli` | `GitHub CLI auth tokens (~/.config/gh)` | `Blocks access to GitHub CLI credentials` |
| `guard_sensitive.go` | `npm` | `npm and yarn registry credentials (~/.npmrc, ~/.yarnrc)` | `Blocks access to npm and yarn auth tokens` |
| `guard_sensitive.go` | `netrc` | `netrc credentials file (~/.netrc)` | `Blocks access to netrc credentials` |
| `guard_sensitive.go` | `vercel` | `Vercel CLI credentials (~/.config/vercel)` | `Blocks access to Vercel CLI credentials` |

### Step 6.2: Rewrite CLI messages in commands.go

- [ ] Update guard/unguard command messages

**File: `/Users/subramk/source/github.com/jskswamy/aide/cmd/aide/commands.go`**

In `sandboxGuardCmd()`:

Change Short from:
```go
Short: "Add a guard to guards_extra for a context's sandbox",
```
to:
```go
Short: "Enable a guard for a context's sandbox",
```

Change the idempotent message from:
```go
fmt.Fprintf(cmd.OutOrStdout(), "Guard %q is already in guards_extra for context %q\n", name, ctxName)
```
to:
```go
fmt.Fprintf(cmd.OutOrStdout(), "Guard %q is already enabled for context %q\n", name, ctxName)
```

Change the success message from:
```go
fmt.Fprintf(cmd.OutOrStdout(), "Added guard %q to guards_extra for context %q\n", name, ctxName)
```
to:
```go
fmt.Fprintf(cmd.OutOrStdout(), "Guard %q enabled for context %q\n", name, ctxName)
```

In `sandboxUnguardCmd()`:

Change the always error from:
```go
return fmt.Errorf("guard %q is an always-active guard and cannot be unguarded", name)
```
to:
```go
return fmt.Errorf("guard %q is an always-active guard and cannot be disabled", name)
```

Change the idempotent message from:
```go
fmt.Fprintf(cmd.OutOrStdout(), "Guard %q is already in unguard list for context %q\n", name, ctxName)
```
to:
```go
fmt.Fprintf(cmd.OutOrStdout(), "Guard %q is already disabled for context %q\n", name, ctxName)
```

Change the success message from:
```go
fmt.Fprintf(cmd.OutOrStdout(), "Added guard %q to unguard list for context %q\n", name, ctxName)
```
to:
```go
fmt.Fprintf(cmd.OutOrStdout(), "Guard %q disabled for context %q\n", name, ctxName)
```

In `sandboxGuardsCmd()`:

Change Short from:
```go
Short: "List all guards with type, status, and paths",
```
to:
```go
Short: "List all guards with type, status, and description",
```

In `sandboxTypesCmd()`:

Change the `DEFAULT` column header:
```go
// old
fmt.Fprintf(out, "%-12s %-10s %s\n", "TYPE", "DEFAULT", "DESCRIPTION")
```
to:
```go
// new
fmt.Fprintf(out, "%-12s %-10s %s\n", "TYPE", "STATE", "DESCRIPTION")
```

### Step 6.3: Update banner rendering

- [ ] Add `Protecting` field to `SandboxInfo` and `sandboxProtectingLine`

**File: `/Users/subramk/source/github.com/jskswamy/aide/internal/ui/banner.go`**

Add `Protecting` field to `SandboxInfo`:

```go
type SandboxInfo struct {
	Disabled   bool
	Network    string
	Ports      string
	GuardCount int
	Denied     []string
	Guards     []string
	Protecting []string // human-readable categories: "SSH keys", "cloud credentials", ...
}
```

Add the `sandboxProtectingLine` function:

```go
// sandboxProtectingLine returns a summary of what the sandbox protects.
func sandboxProtectingLine(info *SandboxInfo) string {
	if info == nil || len(info.Protecting) == 0 {
		return ""
	}
	return "protecting: " + strings.Join(info.Protecting, ", ")
}
```

In `RenderCompact`, `RenderBoxed`, and `RenderClean`, add the protecting line after the sandbox summary, before the counts line. In each render function, add:

```go
if pl := sandboxProtectingLine(data.Sandbox); pl != "" {
    fmt.Fprintf(w, "      %s\n", pl)  // adjust prefix per render style
}
```

Fix shield emoji from `\U0001F6E1\uFE0F` (with variation selector) to plain `\U0001F6E1`:

In all three render functions, change:
```go
"🛡️  sandbox"
```
to:
```go
"🛡 sandbox"
```

### Step 6.4: Verify compilation and tests

- [ ] Run tests

```bash
cd /Users/subramk/source/github.com/jskswamy/aide
go build ./...
go test ./pkg/seatbelt/guards/...
go test ./internal/ui/...
```

### Step 6.5: Commit

- [ ] Commit description rewrites and CLI message fixes

---

## Task 7: Smart Config Mutation (Fix 2)

### Step 7.1: Write tests for EffectiveGuards, EnableGuard, DisableGuard

- [ ] Create guard_config_test.go

**File: `/Users/subramk/source/github.com/jskswamy/aide/internal/sandbox/guard_config_test.go`**

```go
package sandbox

import (
	"testing"

	"github.com/jskswamy/aide/internal/config"
	"github.com/jskswamy/aide/pkg/seatbelt/guards"
)

func TestEffectiveGuards_NilConfig(t *testing.T) {
	result := EffectiveGuards(nil)
	expected := guards.DefaultGuardNames()
	assertSliceEqual(t, result, expected, "EffectiveGuards(nil)")
}

func TestEffectiveGuards_GuardsExtra(t *testing.T) {
	cfg := &config.SandboxPolicy{
		GuardsExtra: []string{"docker"},
	}
	result := EffectiveGuards(cfg)
	found := false
	for _, n := range result {
		if n == "docker" {
			found = true
		}
	}
	if !found {
		t.Error("expected docker in effective guards")
	}
}

func TestEffectiveGuards_Unguard(t *testing.T) {
	cfg := &config.SandboxPolicy{
		Unguard: []string{"ssh-keys"},
	}
	result := EffectiveGuards(cfg)
	for _, n := range result {
		if n == "ssh-keys" {
			t.Error("ssh-keys should be removed by unguard")
		}
	}
}

func TestEnableGuard_MetaGuardError(t *testing.T) {
	cfg := &config.SandboxPolicy{}
	result := EnableGuard(cfg, "cloud")
	if result.OK() {
		t.Error("expected error for meta-guard name")
	}
}

func TestEnableGuard_AppendsToGuardsExtra(t *testing.T) {
	cfg := &config.SandboxPolicy{}
	result := EnableGuard(cfg, "docker")
	if !result.OK() {
		t.Fatalf("unexpected error: %v", result.Errors)
	}
	found := false
	for _, n := range cfg.GuardsExtra {
		if n == "docker" {
			found = true
		}
	}
	if !found {
		t.Error("expected docker in guards_extra")
	}
}

func TestEnableGuard_AppendsToGuards_WhenGuardsSet(t *testing.T) {
	cfg := &config.SandboxPolicy{
		Guards: []string{"ssh-keys"},
	}
	result := EnableGuard(cfg, "docker")
	if !result.OK() {
		t.Fatalf("unexpected error: %v", result.Errors)
	}
	found := false
	for _, n := range cfg.Guards {
		if n == "docker" {
			found = true
		}
	}
	if !found {
		t.Error("expected docker in guards (when guards: is set)")
	}
}

func TestDisableGuard_MetaGuardError(t *testing.T) {
	cfg := &config.SandboxPolicy{}
	result := DisableGuard(cfg, "cloud")
	if result.OK() {
		t.Error("expected error for meta-guard name")
	}
}

func TestDisableGuard_AlwaysGuardError(t *testing.T) {
	cfg := &config.SandboxPolicy{}
	result := DisableGuard(cfg, "base")
	if result.OK() {
		t.Error("expected error for always guard")
	}
}

func TestDisableGuard_AddsToUnguard(t *testing.T) {
	cfg := &config.SandboxPolicy{}
	result := DisableGuard(cfg, "ssh-keys")
	if !result.OK() {
		t.Fatalf("unexpected error: %v", result.Errors)
	}
	found := false
	for _, n := range cfg.Unguard {
		if n == "ssh-keys" {
			found = true
		}
	}
	if !found {
		t.Error("expected ssh-keys in unguard")
	}
}

func TestDisableGuard_RemovesFromGuards(t *testing.T) {
	cfg := &config.SandboxPolicy{
		Guards: []string{"ssh-keys", "browsers"},
	}
	result := DisableGuard(cfg, "ssh-keys")
	if !result.OK() {
		t.Fatalf("unexpected error: %v", result.Errors)
	}
	for _, n := range cfg.Guards {
		if n == "ssh-keys" {
			t.Error("ssh-keys should have been removed from guards")
		}
	}
}

func TestDisableGuard_RemovesFromGuardsExtra(t *testing.T) {
	cfg := &config.SandboxPolicy{
		GuardsExtra: []string{"docker"},
	}
	result := DisableGuard(cfg, "docker")
	if !result.OK() {
		t.Fatalf("unexpected error: %v", result.Errors)
	}
	for _, n := range cfg.GuardsExtra {
		if n == "docker" {
			t.Error("docker should have been removed from guards_extra")
		}
	}
}
```

### Step 7.2: Create guard_config.go

- [ ] Create `/Users/subramk/source/github.com/jskswamy/aide/internal/sandbox/guard_config.go`

```go
package sandbox

import (
	"github.com/jskswamy/aide/internal/config"
	"github.com/jskswamy/aide/pkg/seatbelt"
	"github.com/jskswamy/aide/pkg/seatbelt/guards"
)

// EffectiveGuards resolves the active guard set for a sandbox config.
// Applies: defaults -> guards (override) or guards_extra (extend) -> unguard (remove).
// Returns default guard names if cfg is nil.
func EffectiveGuards(cfg *config.SandboxPolicy) []string {
	if cfg == nil {
		return guards.DefaultGuardNames()
	}

	resolved, _, _ := resolveGuards(cfg)
	if resolved == nil {
		return guards.DefaultGuardNames()
	}
	return resolved
}

// isMetaGuard returns true if name is a meta-guard (cloud, all-default).
func isMetaGuard(name string) bool {
	return name == "cloud" || name == "all-default"
}

// EnableGuard adds a guard to the config, handling state correctly.
func EnableGuard(cfg *config.SandboxPolicy, name string) *seatbelt.ValidationResult {
	r := &seatbelt.ValidationResult{}

	if isMetaGuard(name) {
		r.AddError("cannot enable meta-guard %q; use concrete guard names (e.g. cloud-aws, cloud-gcp)", name)
		return r
	}

	if _, ok := guards.GuardByName(name); !ok {
		r.AddError("unknown guard %q", name)
		return r
	}

	// Check if already active
	effective := EffectiveGuards(cfg)
	for _, n := range effective {
		if n == name {
			r.AddWarning("guard %q is already enabled", name)
			return r
		}
	}

	// Add to the correct field based on config state
	if len(cfg.Guards) > 0 {
		// guards: is set -> append to guards:
		cfg.Guards = append(cfg.Guards, name)
	} else {
		// guards: not set -> append to guards_extra:
		cfg.GuardsExtra = append(cfg.GuardsExtra, name)
	}

	// If the guard is in unguard, remove it
	cfg.Unguard = removeString(cfg.Unguard, name)

	return r
}

// DisableGuard removes a guard from the config.
func DisableGuard(cfg *config.SandboxPolicy, name string) *seatbelt.ValidationResult {
	r := &seatbelt.ValidationResult{}

	if isMetaGuard(name) {
		r.AddError("cannot disable meta-guard %q; use concrete guard names (e.g. cloud-aws, cloud-gcp)", name)
		return r
	}

	g, ok := guards.GuardByName(name)
	if !ok {
		r.AddError("unknown guard %q", name)
		return r
	}

	if g.Type() == "always" {
		r.AddError("guard %q is an always-active guard and cannot be disabled", name)
		return r
	}

	// Check if already inactive
	effective := EffectiveGuards(cfg)
	isActive := false
	for _, n := range effective {
		if n == name {
			isActive = true
			break
		}
	}
	if !isActive {
		r.AddWarning("guard %q is already disabled", name)
		return r
	}

	// Remove from guards: if present
	found := false
	for _, n := range cfg.Guards {
		if n == name {
			found = true
			break
		}
	}
	if found {
		cfg.Guards = removeString(cfg.Guards, name)
		return r
	}

	// Remove from guards_extra: if present
	for _, n := range cfg.GuardsExtra {
		if n == name {
			found = true
			break
		}
	}
	if found {
		cfg.GuardsExtra = removeString(cfg.GuardsExtra, name)
		return r
	}

	// Otherwise add to unguard:
	cfg.Unguard = append(cfg.Unguard, name)
	return r
}
```

### Step 7.3: Add SandboxRef.MarshalYAML for flat serialization

- [ ] Add MarshalYAML to SandboxRef

**File: `/Users/subramk/source/github.com/jskswamy/aide/internal/config/schema.go`**

Add after the `SandboxRef.UnmarshalYAML` method:

```go
// MarshalYAML serializes a SandboxRef to clean YAML.
// When Inline is set and ProfileName is empty, the inline policy is written
// directly (flat) without an "inline:" wrapper.
func (r SandboxRef) MarshalYAML() (interface{}, error) {
	if r.Disabled {
		return false, nil
	}
	if r.ProfileName != "" {
		return map[string]string{"profile": r.ProfileName}, nil
	}
	if r.Inline != nil {
		// Marshal the SandboxPolicy directly (flat), not wrapped in "inline:"
		return r.Inline, nil
	}
	return nil, nil
}
```

### Step 7.4: Rewrite CLI guard/unguard commands as thin wrappers

- [ ] Update sandboxGuardCmd to use EnableGuard

**File: `/Users/subramk/source/github.com/jskswamy/aide/cmd/aide/commands.go`**

Replace `sandboxGuardCmd` RunE body:

```go
RunE: func(cmd *cobra.Command, args []string) error {
    name := args[0]
    if _, ok := guards.GuardByName(name); !ok {
        return fmt.Errorf("unknown guard %q (run 'aide sandbox guards' to list available guards)", name)
    }
    cfg, ctxName, ctx, err := resolveContextForMutation(contextName)
    if err != nil {
        return err
    }
    sp := ensureInlineSandbox(&ctx)
    result := sandbox.EnableGuard(sp, name)
    for _, w := range result.Warnings {
        fmt.Fprintf(cmd.OutOrStdout(), "%s\n", w)
    }
    if !result.OK() {
        return result.Err()
    }
    cfg.Contexts[ctxName] = ctx
    if err := config.WriteConfig(cfg); err != nil {
        return fmt.Errorf("writing config: %w", err)
    }
    fmt.Fprintf(cmd.OutOrStdout(), "Guard %q enabled for context %q\n", name, ctxName)
    return nil
},
```

Replace `sandboxUnguardCmd` RunE body:

```go
RunE: func(cmd *cobra.Command, args []string) error {
    name := args[0]
    if _, ok := guards.GuardByName(name); !ok {
        return fmt.Errorf("unknown guard %q (run 'aide sandbox guards' to list available guards)", name)
    }
    cfg, ctxName, ctx, err := resolveContextForMutation(contextName)
    if err != nil {
        return err
    }
    sp := ensureInlineSandbox(&ctx)
    result := sandbox.DisableGuard(sp, name)
    for _, w := range result.Warnings {
        fmt.Fprintf(cmd.OutOrStdout(), "%s\n", w)
    }
    if !result.OK() {
        return result.Err()
    }
    cfg.Contexts[ctxName] = ctx
    if err := config.WriteConfig(cfg); err != nil {
        return fmt.Errorf("writing config: %w", err)
    }
    fmt.Fprintf(cmd.OutOrStdout(), "Guard %q disabled for context %q\n", name, ctxName)
    return nil
},
```

Add `"github.com/jskswamy/aide/internal/sandbox"` import to commands.go if not already present.

### Step 7.5: Update guards-list STATUS to be context-aware

- [ ] Update sandboxGuardsCmd to use EffectiveGuards

**File: `/Users/subramk/source/github.com/jskswamy/aide/cmd/aide/commands.go`**

In `sandboxGuardsCmd`, replace the active set computation:

```go
// old
activeSet := make(map[string]bool)
for _, n := range modules.DefaultGuardNames() {
    activeSet[n] = true
}
```

with:

```go
// new — resolve active guards from context config if available
activeNames := guards.DefaultGuardNames()
// TODO: if context is resolvable, use sandbox.EffectiveGuards(resolved sandbox config)
activeSet := make(map[string]bool, len(activeNames))
for _, n := range activeNames {
    activeSet[n] = true
}
```

(Full context-aware resolution requires resolving the current context, which may fail. For now, use defaults as fallback. The EffectiveGuards function is available for when context resolution is wired in.)

### Step 7.6: Verify compilation and tests

- [ ] Run tests

```bash
cd /Users/subramk/source/github.com/jskswamy/aide
go build ./...
go test ./internal/sandbox/...
go test ./cmd/aide/...
```

### Step 7.7: Commit

- [ ] Commit smart config mutation

---

## Task 8: Test Coverage (Fix 6)

### Step 8.1: Safety tests — empty guards and empty HomeDir

- [ ] Add safety tests to darwin_test.go

**File: `/Users/subramk/source/github.com/jskswamy/aide/internal/sandbox/darwin_test.go`**

Add:

```go
func TestGenerateSeatbeltProfile_EmptyGuards_ReturnsError(t *testing.T) {
	policy := Policy{
		Guards:  []string{},
		Network: NetworkNone,
	}
	_, err := generateSeatbeltProfile(policy)
	if err == nil {
		t.Fatal("expected error for empty guards list")
	}
	if !strings.Contains(err.Error(), "base") {
		t.Errorf("expected error about missing base guard, got: %v", err)
	}
}

func TestGenerateSeatbeltProfile_NoBase_ReturnsError(t *testing.T) {
	policy := Policy{
		Guards:  []string{"ssh-keys", "cloud-aws"},
		Network: NetworkNone,
	}
	_, err := generateSeatbeltProfile(policy)
	if err == nil {
		t.Fatal("expected error when base guard is missing")
	}
}
```

### Step 8.2: Context validation tests

- [ ] Tests already in Step 4.1 (validation_test.go)

### Step 8.3: Meta-guard expansion tests in policy_test.go

- [ ] Add meta-guard tests

**File: `/Users/subramk/source/github.com/jskswamy/aide/internal/sandbox/policy_test.go`**

Add:

```go
func TestPolicyFromConfig_GuardsExtraCloud(t *testing.T) {
	cfg := &config.SandboxPolicy{
		GuardsExtra: []string{"cloud"},
	}
	policy, _, err := PolicyFromConfig(cfg, "/tmp/proj", "/tmp/rt", "/home/test", "/tmp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	cloudGuards := map[string]bool{
		"cloud-aws": false, "cloud-gcp": false, "cloud-azure": false,
		"cloud-digitalocean": false, "cloud-oci": false,
	}
	for _, name := range policy.Guards {
		if _, ok := cloudGuards[name]; ok {
			cloudGuards[name] = true
		}
	}
	for name, found := range cloudGuards {
		if !found {
			t.Errorf("expected cloud guard %q in resolved guards", name)
		}
	}
}

func TestPolicyFromConfig_UnguardCloud(t *testing.T) {
	cfg := &config.SandboxPolicy{
		Unguard: []string{"cloud"},
	}
	policy, _, err := PolicyFromConfig(cfg, "/tmp/proj", "/tmp/rt", "/home/test", "/tmp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	cloudGuards := []string{"cloud-aws", "cloud-gcp", "cloud-azure", "cloud-digitalocean", "cloud-oci"}
	for _, name := range policy.Guards {
		for _, cg := range cloudGuards {
			if name == cg {
				t.Errorf("cloud guard %q should have been removed by unguard", name)
			}
		}
	}
}
```

### Step 8.4: Deduplication tests

- [ ] Add dedup tests to registry_test.go

**File: `/Users/subramk/source/github.com/jskswamy/aide/pkg/seatbelt/guards/registry_test.go`**

Add:

```go
func TestRegistry_ResolveActiveGuards_Dedup(t *testing.T) {
	names := []string{"ssh-keys", "base", "ssh-keys", "base"}
	result := guards.ResolveActiveGuards(names)

	// Should deduplicate
	seen := make(map[string]int)
	for _, g := range result {
		seen[g.Name()]++
	}
	for name, count := range seen {
		if count > 1 {
			t.Errorf("guard %q appears %d times, expected 1", name, count)
		}
	}
}
```

### Step 8.5: KUBECONFIG edge cases

- [ ] Add KUBECONFIG test with empty segments

**File: `/Users/subramk/source/github.com/jskswamy/aide/pkg/seatbelt/guards/guard_cloud_test.go`**

Add (or create the file if it only contains existing tests):

```go
func TestKubernetesGuard_ColonSeparated_EmptySegment(t *testing.T) {
	ctx := &seatbelt.Context{
		HomeDir: "/home/test",
		Env:     []string{"KUBECONFIG=/a::/b"},
	}
	g := guards.KubernetesGuard()
	rules := g.Rules(ctx)

	var output strings.Builder
	for _, r := range rules {
		output.WriteString(r.String())
		output.WriteString("\n")
	}
	s := output.String()

	if !strings.Contains(s, `"/a"`) {
		t.Error("expected deny for /a")
	}
	if !strings.Contains(s, `"/b"`) {
		t.Error("expected deny for /b")
	}
	// Should NOT contain empty path
	if strings.Contains(s, `""`) {
		t.Error("should not contain empty path deny")
	}
}
```

### Step 8.6: EnvLookup duplicate keys test

- [ ] Add EnvLookup test

**File: `/Users/subramk/source/github.com/jskswamy/aide/pkg/seatbelt/context_test.go`**

```go
package seatbelt_test

import (
	"testing"

	"github.com/jskswamy/aide/pkg/seatbelt"
)

func TestContext_EnvLookup_FirstWins(t *testing.T) {
	ctx := &seatbelt.Context{
		Env: []string{"FOO=first", "FOO=second"},
	}
	val, ok := ctx.EnvLookup("FOO")
	if !ok {
		t.Fatal("expected to find FOO")
	}
	if val != "first" {
		t.Errorf("expected first-wins semantics, got %q", val)
	}
}

func TestContext_EnvLookup_NotFound(t *testing.T) {
	ctx := &seatbelt.Context{
		Env: []string{"BAR=value"},
	}
	_, ok := ctx.EnvLookup("FOO")
	if ok {
		t.Error("expected not found for FOO")
	}
}

func TestContext_EnvLookup_EmptyValue(t *testing.T) {
	ctx := &seatbelt.Context{
		Env: []string{"FOO="},
	}
	val, ok := ctx.EnvLookup("FOO")
	if !ok {
		t.Fatal("expected to find FOO")
	}
	if val != "" {
		t.Errorf("expected empty value, got %q", val)
	}
}
```

### Step 8.7: Only always-guards test

- [ ] Add test for profile with only always guards

**File: `/Users/subramk/source/github.com/jskswamy/aide/internal/sandbox/darwin_test.go`**

Add:

```go
func TestGenerateSeatbeltProfile_OnlyAlwaysGuards(t *testing.T) {
	// Use only the always guards (no default/opt-in)
	var alwaysNames []string
	for _, g := range guards.AllGuards() {
		if g.Type() == "always" {
			alwaysNames = append(alwaysNames, g.Name())
		}
	}

	policy := Policy{
		Guards:  alwaysNames,
		Network: NetworkOutbound,
	}
	profile, err := generateSeatbeltProfile(policy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(profile, "(deny default)") {
		t.Error("profile should contain (deny default)")
	}
	if !strings.Contains(profile, "network-outbound") {
		t.Error("profile should contain network rules")
	}
}
```

### Step 8.8: ValidateCustomGuard zero paths test

- [ ] Add test for zero paths

**File: `/Users/subramk/source/github.com/jskswamy/aide/pkg/seatbelt/guards/guard_custom_test.go`**

Add (check existing tests first and add if not present):

```go
func TestValidateCustomGuard_ZeroPaths(t *testing.T) {
	result := guards.ValidateCustomGuard("my-guard", guards.CustomGuardConfig{
		Type: "default",
	})
	if result.OK() {
		t.Error("expected error for zero paths")
	}
}
```

### Step 8.9: Round-trip tests

- [ ] Add config-to-profile round-trip tests

**File: `/Users/subramk/source/github.com/jskswamy/aide/internal/sandbox/darwin_test.go`**

Add:

```go
func TestRoundTrip_UnguardSSHKeys(t *testing.T) {
	cfg := &config.SandboxPolicy{
		Unguard: []string{"ssh-keys"},
	}
	policy, _, err := PolicyFromConfig(cfg, "/tmp/proj", "/tmp/rt", "/home/test", "/tmp")
	if err != nil {
		t.Fatalf("PolicyFromConfig error: %v", err)
	}
	profile, err := generateSeatbeltProfile(*policy)
	if err != nil {
		t.Fatalf("generateSeatbeltProfile error: %v", err)
	}
	if strings.Contains(profile, ".ssh") {
		t.Error("profile should NOT contain .ssh deny when ssh-keys is unguarded")
	}
}

func TestRoundTrip_GuardDocker(t *testing.T) {
	cfg := &config.SandboxPolicy{
		GuardsExtra: []string{"docker"},
	}
	policy, _, err := PolicyFromConfig(cfg, "/tmp/proj", "/tmp/rt", "/home/test", "/tmp")
	if err != nil {
		t.Fatalf("PolicyFromConfig error: %v", err)
	}
	profile, err := generateSeatbeltProfile(*policy)
	if err != nil {
		t.Fatalf("generateSeatbeltProfile error: %v", err)
	}
	if !strings.Contains(profile, ".docker") {
		t.Error("profile should contain .docker deny when docker guard is active")
	}
}
```

Add `"github.com/jskswamy/aide/internal/config"` to imports in darwin_test.go for the round-trip tests.

### Step 8.10: guard_config_test.go tests

- [ ] Tests already created in Step 7.1

### Step 8.11: Unguard unknown guard test

- [ ] Add test in policy_test.go

**File: `/Users/subramk/source/github.com/jskswamy/aide/internal/sandbox/policy_test.go`**

Add:

```go
func TestPolicyFromConfig_UnguardUnknown_ReturnsError(t *testing.T) {
	cfg := &config.SandboxPolicy{
		Unguard: []string{"nonexistent"},
	}
	_, _, err := PolicyFromConfig(cfg, "/tmp/proj", "/tmp/rt", "/home/test", "/tmp")
	if err == nil {
		t.Fatal("expected error for unknown guard in unguard")
	}
	if !strings.Contains(err.Error(), "unguard") {
		t.Errorf("error should mention 'unguard', got: %v", err)
	}
}
```

### Step 8.12: Verify all tests pass

- [ ] Run full test suite

```bash
cd /Users/subramk/source/github.com/jskswamy/aide
go test ./...
```

### Step 8.13: Commit

- [ ] Commit test coverage additions

---

## Task 9: Spec + Docs Reconciliation (Fix 7)

### Step 9.1: Update spec document

- [ ] Update `/Users/subramk/source/github.com/jskswamy/aide/docs/superpowers/specs/2026-03-22-sandbox-guards-design.md`

Changes to make:
1. Update Config YAML examples: `allow_ports`/`deny_ports` are nested under `network:`, not flat
2. Update CLI output: PATHS column should say DESCRIPTION
3. Remove `types show/add/remove` subcommands (not implemented)
4. Add note: `aide sandbox guard/unguard` accept concrete names only, not meta-guards
5. Update Context: `Network` is `string` not `NetworkMode int`
6. Update package path from `pkg/seatbelt/modules` to `pkg/seatbelt/guards`

### Step 9.2: Update docs/sandbox.md

- [ ] Update `/Users/subramk/source/github.com/jskswamy/aide/docs/sandbox.md` (if it exists)

Check if the file exists first:

```bash
ls /Users/subramk/source/github.com/jskswamy/aide/docs/sandbox.md
```

If it exists, update it to:
1. Replace Writable/Readable/Denied table with guard-based description
2. Lead with `guards:`/`guards_extra:`/`unguard:` config
3. Add guard CLI commands
4. Update library example to guard API
5. Update "Available modules" to reference guard constructors in `pkg/seatbelt/guards/`

### Step 9.3: Add code comments

- [ ] Add clarifying comments in key files

**File: `/Users/subramk/source/github.com/jskswamy/aide/pkg/seatbelt/guards/registry.go`**

Add comment to `ResolveActiveGuards`:

```go
// ResolveActiveGuards looks up guards by name and returns them ordered by type
// (always -> default -> opt-in). Unknown names are silently skipped;
// callers should validate guard names before calling this function.
```

**File: `/Users/subramk/source/github.com/jskswamy/aide/internal/sandbox/policy.go`**

Add comment to `ValidateSandboxConfigDetailed`:

```go
// ValidateSandboxConfigDetailed validates a SandboxPolicy configuration,
// returning both errors and warnings.
// Note: writable/readable fields are retained for backward compatibility
// but are not used by the guard system. Guards handle all deny/allow rules.
```

### Step 9.4: Commit

- [ ] Commit spec and docs reconciliation

---

## Summary of Files Changed

| File (after restructure) | Tasks |
|--------------------------|-------|
| `pkg/seatbelt/module.go` | 2 (remove NetworkMode int, change Context.Network to string), 4 (add Validate) |
| `pkg/seatbelt/validation.go` | 4 (new: ValidationResult) |
| `pkg/seatbelt/validation_test.go` | 4, 8 (new: ValidationResult + Context.Validate tests) |
| `pkg/seatbelt/context_test.go` | 8 (new: EnvLookup tests) |
| `pkg/seatbelt/guards/` (new package) | 1 (moved from modules/) |
| `pkg/seatbelt/guards/helpers.go` | 5 (new: DenyDir, DenyFile, AllowReadFile, EnvOverridePath, SplitColonPaths) |
| `pkg/seatbelt/guards/helpers_test.go` | 5 (new: helper tests) |
| `pkg/seatbelt/guards/guard_base.go` | 1, 3, 6 (move, remove Base(), rewrite Description) |
| `pkg/seatbelt/guards/guard_system_runtime.go` | 1, 3, 6 (move, remove SystemRuntime(), rewrite Description) |
| `pkg/seatbelt/guards/guard_network.go` | 1, 2, 3, 6 (move, string switch, remove compat, rewrite Description) |
| `pkg/seatbelt/guards/guard_network_string_test.go` | 2 (new: string mode test) |
| `pkg/seatbelt/guards/guard_filesystem.go` | 1, 3, 6 (move, remove compat, rewrite Description) |
| `pkg/seatbelt/guards/guard_keychain.go` | 1, 3, 6 (move, remove KeychainIntegration(), rewrite Description) |
| `pkg/seatbelt/guards/guard_node_toolchain.go` | 1, 3, 6 (move, remove NodeToolchain(), rewrite Description) |
| `pkg/seatbelt/guards/guard_nix_toolchain.go` | 1, 3, 6 (move, remove NixToolchain(), rewrite Description) |
| `pkg/seatbelt/guards/guard_git_integration.go` | 1, 3, 6 (move, remove GitIntegration(), rewrite Description) |
| `pkg/seatbelt/guards/guard_ssh_keys.go` | 1, 6 (move, rewrite Description) |
| `pkg/seatbelt/guards/guard_cloud.go` | 1, 5, 6 (move, typed helpers, fix OCI, rewrite Descriptions) |
| `pkg/seatbelt/guards/guard_kubernetes.go` | 1, 5, 6 (move, typed helpers, rewrite Description) |
| `pkg/seatbelt/guards/guard_terraform.go` | 1, 5, 6 (move, typed helpers, rewrite Description) |
| `pkg/seatbelt/guards/guard_vault.go` | 1, 5, 6 (move, typed helpers, rewrite Description) |
| `pkg/seatbelt/guards/guard_browsers.go` | 1, 5, 6 (move, typed helpers, rewrite Description) |
| `pkg/seatbelt/guards/guard_password_managers.go` | 1, 5, 6 (move, typed helpers, rewrite Descriptions) |
| `pkg/seatbelt/guards/guard_sensitive.go` | 1, 5, 6 (move, typed helpers, rewrite Descriptions) |
| `pkg/seatbelt/guards/guard_custom.go` | 1, 4 (move, use ValidationResult) |
| `pkg/seatbelt/guards/registry.go` | 1, 9 (move, comment update) |
| `pkg/seatbelt/guards/registry_test.go` | 1, 8 (move, add dedup test) |
| `pkg/seatbelt/guards/guard_cloud_test.go` | 1, 8 (move, add KUBECONFIG test) |
| `pkg/seatbelt/guards/guard_custom_test.go` | 1, 8 (move, add zero paths test) |
| `pkg/seatbelt/modules/claude.go` | 1 (stays, unchanged) |
| `internal/sandbox/sandbox.go` | 1 (import path change) |
| `internal/sandbox/darwin.go` | 1, 2, 4 (import, string network, GOOS fix, validation) |
| `internal/sandbox/darwin_test.go` | 1, 8 (import, safety tests, round-trip tests) |
| `internal/sandbox/policy.go` | 1, 4, 9 (import, ValidationResult migration, comment) |
| `internal/sandbox/policy_test.go` | 1, 8 (import, meta-guard + unknown guard tests) |
| `internal/sandbox/guard_config.go` | 7 (new: EffectiveGuards, EnableGuard, DisableGuard) |
| `internal/sandbox/guard_config_test.go` | 7, 8 (new: config mutation tests) |
| `internal/ui/banner.go` | 6 (Protecting field, protecting line, shield emoji) |
| `internal/config/schema.go` | 7 (SandboxRef.MarshalYAML) |
| `cmd/aide/commands.go` | 1, 6, 7 (import, messages, thin wrappers) |
| Deleted: `pkg/seatbelt/modules/base_test.go` | 3 |
| Deleted: `pkg/seatbelt/modules/system_test.go` | 3 |
| Deleted: `pkg/seatbelt/modules/network_test.go` | 2 |
| Deleted: `pkg/seatbelt/modules/filesystem_test.go` | 3 |
| Deleted: `pkg/seatbelt/modules/toolchain_test.go` | 3 |
| `docs/superpowers/specs/2026-03-22-sandbox-guards-design.md` | 9 |
| `docs/sandbox.md` | 9 (if exists) |

## Verification Commands

After all tasks, run:

```bash
cd /Users/subramk/source/github.com/jskswamy/aide
go build ./...
go vet ./...
go test ./...
```

Every test should pass. The guards package should be at `pkg/seatbelt/guards/`, the `modules/` package should only contain `claude.go`, and all 35 issues from the spec should be addressed.
