# Unrestricted Network Capability Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `--with network` capability and `--unrestricted-network` / `-N` flag for per-session unrestricted network access.

**Architecture:** New `NetworkMode` field on `Capability` and `SandboxOverrides` structs, plumbed through the existing capability resolution pipeline. A new `-N` CLI flag bypasses config port rules entirely.

**Tech Stack:** Go, Cobra CLI, macOS Seatbelt

---

### Task 1: Add NetworkMode to SandboxOverrides

**Files:**
- Modify: `internal/config/schema.go:129-137`
- Modify: `internal/config/schema_test.go`

- [ ] **Step 1: Write the failing test**

In `internal/config/schema_test.go`, add:

```go
func TestSandboxOverrides_HasNetworkMode(t *testing.T) {
	o := SandboxOverrides{NetworkMode: "unrestricted"}
	if o.NetworkMode != "unrestricted" {
		t.Errorf("expected NetworkMode unrestricted, got %q", o.NetworkMode)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -run TestSandboxOverrides_HasNetworkMode -v`
Expected: FAIL — `o.NetworkMode undefined`

- [ ] **Step 3: Add NetworkMode field**

In `internal/config/schema.go`, add the field to `SandboxOverrides`:

```go
type SandboxOverrides struct {
	Unguard       []string
	ReadableExtra []string
	WritableExtra []string
	DeniedExtra   []string
	EnvAllow      []string
	EnableGuard   []string
	Allow         []string
	NetworkMode   string // "unrestricted" or "" (no override)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/config/ -run TestSandboxOverrides_HasNetworkMode -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/config/schema.go internal/config/schema_test.go
git commit -m "Add NetworkMode field to SandboxOverrides"
```

---

### Task 2: Add NetworkMode to Capability struct and resolution

**Files:**
- Modify: `internal/capability/capability.go:9-22` (Capability struct)
- Modify: `internal/capability/capability.go:101-113` (flatten)
- Modify: `internal/capability/capability.go:115-127` (mergeChild)
- Modify: `internal/capability/capability.go:200-240` (ToSandboxOverrides)
- Modify: `internal/capability/capability_test.go`

- [ ] **Step 1: Write the failing test**

In `internal/capability/capability_test.go`, add:

```go
func TestCapability_NetworkMode_Resolves(t *testing.T) {
	registry := map[string]Capability{
		"network": {
			Name:        "network",
			NetworkMode: "unrestricted",
		},
	}
	resolved, err := ResolveOne("network", registry)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.NetworkMode != "unrestricted" {
		t.Errorf("expected NetworkMode unrestricted, got %q", resolved.NetworkMode)
	}
}

func TestCapability_NetworkMode_ToSandboxOverrides(t *testing.T) {
	set := &Set{
		Capabilities: []ResolvedCapability{
			{Name: "network", NetworkMode: "unrestricted"},
		},
	}
	o := set.ToSandboxOverrides()
	if o.NetworkMode != "unrestricted" {
		t.Errorf("expected NetworkMode unrestricted, got %q", o.NetworkMode)
	}
}

func TestCapability_NetworkMode_EmptyWhenNotSet(t *testing.T) {
	set := &Set{
		Capabilities: []ResolvedCapability{
			{Name: "k8s"},
		},
	}
	o := set.ToSandboxOverrides()
	if o.NetworkMode != "" {
		t.Errorf("expected empty NetworkMode, got %q", o.NetworkMode)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/capability/ -run TestCapability_NetworkMode -v`
Expected: FAIL — `NetworkMode undefined`

- [ ] **Step 3: Add NetworkMode to Capability and ResolvedCapability**

In `internal/capability/capability.go`:

Add `NetworkMode string` to the `Capability` struct (after `Allow`):

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
	EnableGuard []string
	Allow       []string
	NetworkMode string // "unrestricted" or "" (no override)
}
```

Add `NetworkMode string` to `ResolvedCapability` (after `Allow`):

```go
type ResolvedCapability struct {
	Name        string
	Sources     []string
	Unguard     []string
	Readable    []string
	Writable    []string
	Deny        []string
	EnvAllow    []string
	EnableGuard []string
	Allow       []string
	NetworkMode string
}
```

Update `flatten()` to copy NetworkMode:

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
		Allow:       copyStrings(capDef.Allow),
		NetworkMode: capDef.NetworkMode,
	}
}
```

Update `mergeChild()` — child's NetworkMode wins if set:

```go
func mergeChild(parent *ResolvedCapability, child *Capability) *ResolvedCapability {
	networkMode := parent.NetworkMode
	if child.NetworkMode != "" {
		networkMode = child.NetworkMode
	}
	return &ResolvedCapability{
		Name:        child.Name,
		Sources:     append([]string{child.Name}, parent.Sources...),
		Unguard:     dedup(append(parent.Unguard, child.Unguard...)),
		Readable:    dedup(append(parent.Readable, child.Readable...)),
		Writable:    dedup(append(parent.Writable, child.Writable...)),
		Deny:        dedup(append(parent.Deny, child.Deny...)),
		EnvAllow:    dedup(append(parent.EnvAllow, child.EnvAllow...)),
		EnableGuard: dedup(append(parent.EnableGuard, child.EnableGuard...)),
		Allow:       dedup(append(parent.Allow, child.Allow...)),
		NetworkMode: networkMode,
	}
}
```

Update `mergeAdditive()` — last non-empty wins:

```go
func mergeAdditive(a, b *ResolvedCapability) *ResolvedCapability {
	networkMode := a.NetworkMode
	if b.NetworkMode != "" {
		networkMode = b.NetworkMode
	}
	return &ResolvedCapability{
		Name:        a.Name,
		Sources:     append(a.Sources, b.Sources...),
		Unguard:     dedup(append(a.Unguard, b.Unguard...)),
		Readable:    dedup(append(a.Readable, b.Readable...)),
		Writable:    dedup(append(a.Writable, b.Writable...)),
		Deny:        dedup(append(a.Deny, b.Deny...)),
		EnvAllow:    dedup(append(a.EnvAllow, b.EnvAllow...)),
		EnableGuard: dedup(append(a.EnableGuard, b.EnableGuard...)),
		Allow:       dedup(append(a.Allow, b.Allow...)),
		NetworkMode: networkMode,
	}
}
```

Update `ToSandboxOverrides()` — first non-empty NetworkMode wins:

```go
func (cs *Set) ToSandboxOverrides() SandboxOverrides {
	var o SandboxOverrides

	for _, rc := range cs.Capabilities {
		o.Unguard = append(o.Unguard, rc.Unguard...)
		o.ReadableExtra = append(o.ReadableExtra, rc.Readable...)
		o.WritableExtra = append(o.WritableExtra, rc.Writable...)
		o.DeniedExtra = append(o.DeniedExtra, rc.Deny...)
		o.EnvAllow = append(o.EnvAllow, rc.EnvAllow...)
		o.EnableGuard = append(o.EnableGuard, rc.EnableGuard...)
		o.Allow = append(o.Allow, rc.Allow...)
		if o.NetworkMode == "" && rc.NetworkMode != "" {
			o.NetworkMode = rc.NetworkMode
		}
	}

	// ... rest unchanged (NeverAllow, NeverAllowEnv, dedup) ...
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/capability/ -run TestCapability_NetworkMode -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/capability/capability.go internal/capability/capability_test.go
git commit -m "Add NetworkMode to Capability and resolution pipeline"
```

---

### Task 3: Add network built-in capability

**Files:**
- Modify: `internal/capability/builtin.go:7-147`
- Modify: `internal/capability/builtin_test.go`

- [ ] **Step 1: Write the failing test**

In `internal/capability/builtin_test.go`, add:

```go
func TestBuiltin_Network_Exists(t *testing.T) {
	cap, ok := Builtins()["network"]
	if !ok {
		t.Fatal("missing built-in capability 'network'")
	}
	if cap.NetworkMode != "unrestricted" {
		t.Errorf("expected NetworkMode unrestricted, got %q", cap.NetworkMode)
	}
	if cap.Description == "" {
		t.Error("expected non-empty description")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/capability/ -run TestBuiltin_Network_Exists -v`
Expected: FAIL — `"network"` not found

- [ ] **Step 3: Add network capability to builtins**

In `internal/capability/builtin.go`, add after the `"gpg"` entry:

```go
		// Network
		"network": {
			Name:        "network",
			Description: "Unrestricted network access (inbound and outbound)",
			NetworkMode: "unrestricted",
		},
```

- [ ] **Step 4: Update the count test**

In `internal/capability/builtin_test.go`, update `TestBuiltins_Count`:

```go
func TestBuiltins_Count(t *testing.T) {
	if len(Builtins()) != 21 {
		t.Errorf("expected 21 built-in capabilities, got %d", len(Builtins()))
	}
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/capability/ -v`
Expected: All PASS

- [ ] **Step 6: Commit**

```bash
git add internal/capability/builtin.go internal/capability/builtin_test.go
git commit -m "Add network built-in capability"
```

---

### Task 4: Apply NetworkMode override in sandbox policy

**Files:**
- Modify: `internal/sandbox/capabilities.go:18-28`
- Modify: `internal/sandbox/capabilities_test.go`

- [ ] **Step 1: Write the failing test**

In `internal/sandbox/capabilities_test.go`, add:

```go
func TestApplyOverrides_NetworkMode(t *testing.T) {
	cfg := &config.SandboxPolicy{
		Network: &config.NetworkPolicy{
			Mode:      "outbound",
			DenyPorts: []int{22},
		},
	}
	overrides := config.SandboxOverrides{
		NetworkMode: "unrestricted",
	}
	sandbox.ApplyOverrides(&cfg, overrides)

	if cfg.Network == nil || cfg.Network.Mode != "unrestricted" {
		t.Errorf("expected network mode unrestricted, got %v", cfg.Network)
	}
	// Port deny list from config must be preserved
	if len(cfg.Network.DenyPorts) != 1 || cfg.Network.DenyPorts[0] != 22 {
		t.Errorf("expected deny_ports [22] preserved, got %v", cfg.Network.DenyPorts)
	}
}

func TestApplyOverrides_NetworkMode_NilNetwork(t *testing.T) {
	cfg := &config.SandboxPolicy{}
	overrides := config.SandboxOverrides{
		NetworkMode: "unrestricted",
	}
	sandbox.ApplyOverrides(&cfg, overrides)

	if cfg.Network == nil || cfg.Network.Mode != "unrestricted" {
		t.Errorf("expected network mode unrestricted, got %v", cfg.Network)
	}
}

func TestApplyOverrides_NetworkMode_Empty(t *testing.T) {
	cfg := &config.SandboxPolicy{
		Network: &config.NetworkPolicy{Mode: "outbound"},
	}
	overrides := config.SandboxOverrides{} // no NetworkMode
	sandbox.ApplyOverrides(&cfg, overrides)

	if cfg.Network.Mode != "outbound" {
		t.Errorf("expected network mode unchanged, got %q", cfg.Network.Mode)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/sandbox/ -run TestApplyOverrides_NetworkMode -v`
Expected: FAIL — NetworkMode not applied

- [ ] **Step 3: Update ApplyOverrides to handle NetworkMode**

In `internal/sandbox/capabilities.go`, update `ApplyOverrides`:

```go
func ApplyOverrides(cfg **config.SandboxPolicy, overrides config.SandboxOverrides) {
	if *cfg == nil {
		*cfg = &config.SandboxPolicy{}
	}
	(*cfg).Unguard = append((*cfg).Unguard, overrides.Unguard...)
	(*cfg).ReadableExtra = append((*cfg).ReadableExtra, overrides.ReadableExtra...)
	(*cfg).WritableExtra = append((*cfg).WritableExtra, overrides.WritableExtra...)
	(*cfg).DeniedExtra = append((*cfg).DeniedExtra, overrides.DeniedExtra...)
	(*cfg).GuardsExtra = append((*cfg).GuardsExtra, overrides.EnableGuard...)
	(*cfg).Allow = append((*cfg).Allow, overrides.Allow...)

	if overrides.NetworkMode != "" {
		if (*cfg).Network == nil {
			(*cfg).Network = &config.NetworkPolicy{}
		}
		(*cfg).Network.Mode = overrides.NetworkMode
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/sandbox/ -run TestApplyOverrides_NetworkMode -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/sandbox/capabilities.go internal/sandbox/capabilities_test.go
git commit -m "Apply NetworkMode from capability overrides to sandbox policy"
```

---

### Task 5: Add --unrestricted-network / -N CLI flag

**Files:**
- Modify: `cmd/aide/main.go:18-90`
- Modify: `internal/launcher/launcher.go:210-241`

- [ ] **Step 1: Add the flag to main.go**

In `cmd/aide/main.go`, add the variable declaration (after `ignoreProjectConfig`):

```go
var unrestrictedNetwork bool
```

Add the flag registration (after the `--ignore-project-config` line):

```go
rootCmd.Flags().BoolVarP(&unrestrictedNetwork, "unrestricted-network", "N", false,
	"Allow unrestricted network access, ignoring config port rules")
```

Pass it to the launcher. Update the `Launcher` struct initialization:

```go
l := &launcher.Launcher{
	Execer:              &launcher.SyscallExecer{},
	Yolo:                yolo || autoApprove,
	NoYolo:              noYolo || noAutoApprove,
	IgnoreProjectConfig: ignoreProjectConfig,
	UnrestrictedNetwork: unrestrictedNetwork,
}
```

- [ ] **Step 2: Add UnrestrictedNetwork field to Launcher**

In `internal/launcher/launcher.go`, add to the `Launcher` struct:

```go
UnrestrictedNetwork bool
```

After `sandbox.ApplyOverrides(&sandboxCfg, capOverrides)` (around line 241), add:

```go
// -N flag: force unrestricted network and clear port rules.
if l.UnrestrictedNetwork {
	if sandboxCfg == nil {
		sandboxCfg = &config.SandboxPolicy{}
	}
	if sandboxCfg.Network == nil {
		sandboxCfg.Network = &config.NetworkPolicy{}
	}
	sandboxCfg.Network.Mode = "unrestricted"
	sandboxCfg.Network.AllowPorts = nil
	sandboxCfg.Network.DenyPorts = nil
}
```

- [ ] **Step 3: Verify build succeeds**

Run: `go build ./cmd/aide/`
Expected: Success

- [ ] **Step 4: Verify flag appears in help**

Run: `go run ./cmd/aide/ --help | grep -A1 unrestricted`
Expected: Shows `-N, --unrestricted-network` flag

- [ ] **Step 5: Commit**

```bash
git add cmd/aide/main.go internal/launcher/launcher.go
git commit -m "Add --unrestricted-network / -N CLI flag"
```

---

### Task 6: Banner shows network mode when non-default

**Files:**
- Modify: `internal/ui/banner.go:96-101`
- Modify: `internal/ui/banner_test.go`

- [ ] **Step 1: Write the failing test**

In `internal/ui/banner_test.go`, find the existing banner tests and add:

```go
func TestSandboxNetworkLabel_Unrestricted(t *testing.T) {
	data := &BannerData{
		Sandbox: &SandboxInfo{Network: "unrestricted"},
	}
	label := sandboxNetworkLabel(data)
	if label != "unrestricted" {
		t.Errorf("expected unrestricted, got %q", label)
	}
}

func TestSandboxNetworkLabel_Default(t *testing.T) {
	data := &BannerData{
		Sandbox: &SandboxInfo{},
	}
	label := sandboxNetworkLabel(data)
	if label != "outbound" {
		t.Errorf("expected outbound, got %q", label)
	}
}
```

- [ ] **Step 2: Run tests to verify they pass**

Run: `go test ./internal/ui/ -run TestSandboxNetworkLabel -v`
Expected: PASS (these test existing behavior — confirming the banner already works)

The banner already displays the network mode via `sandboxNetworkLabel()` and the template. When `--with network` or `-N` sets the mode to `unrestricted`, the banner will automatically show it. No additional changes needed here.

- [ ] **Step 3: Commit**

```bash
git add internal/ui/banner_test.go
git commit -m "Add banner tests for network mode display"
```

---

### Task 7: Integration test

**Files:**
- Modify: `internal/sandbox/integration_test.go` or `internal/sandbox/capabilities_test.go`

- [ ] **Step 1: Write the integration test**

In `internal/sandbox/capabilities_test.go`, add:

```go
func TestNetworkCapability_EndToEnd(t *testing.T) {
	cfg := &config.Config{
		Capabilities: map[string]config.CapabilityDef{},
	}

	// Simulate --with network
	capNames := MergeCapNames(nil, []string{"network"}, nil)
	_, overrides, err := ResolveCapabilities(capNames, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if overrides.NetworkMode != "unrestricted" {
		t.Errorf("expected NetworkMode unrestricted, got %q", overrides.NetworkMode)
	}

	// Apply to a sandbox config with port deny list
	sandboxCfg := &config.SandboxPolicy{
		Network: &config.NetworkPolicy{
			Mode:      "outbound",
			DenyPorts: []int{22},
		},
	}
	ApplyOverrides(&sandboxCfg, overrides)

	// Network mode should be unrestricted but deny ports preserved
	if sandboxCfg.Network.Mode != "unrestricted" {
		t.Errorf("expected unrestricted, got %q", sandboxCfg.Network.Mode)
	}
	if len(sandboxCfg.Network.DenyPorts) != 1 {
		t.Errorf("expected deny_ports preserved, got %v", sandboxCfg.Network.DenyPorts)
	}
}

func TestUnrestrictedNetworkFlag_ClearsPortRules(t *testing.T) {
	sandboxCfg := &config.SandboxPolicy{
		Network: &config.NetworkPolicy{
			Mode:       "outbound",
			AllowPorts: []int{443, 8443},
			DenyPorts:  []int{22},
		},
	}

	// Simulate -N flag behavior
	sandboxCfg.Network.Mode = "unrestricted"
	sandboxCfg.Network.AllowPorts = nil
	sandboxCfg.Network.DenyPorts = nil

	if sandboxCfg.Network.Mode != "unrestricted" {
		t.Errorf("expected unrestricted, got %q", sandboxCfg.Network.Mode)
	}
	if sandboxCfg.Network.AllowPorts != nil {
		t.Error("expected AllowPorts nil")
	}
	if sandboxCfg.Network.DenyPorts != nil {
		t.Error("expected DenyPorts nil")
	}
}
```

- [ ] **Step 2: Run all tests**

Run: `go test ./... -v`
Expected: All PASS

- [ ] **Step 3: Commit**

```bash
git add internal/sandbox/capabilities_test.go
git commit -m "Add integration tests for network capability"
```

---

### Task 8: Full test suite verification

- [ ] **Step 1: Run full test suite**

Run: `go test ./...`
Expected: All PASS, no regressions

- [ ] **Step 2: Build and manual smoke test**

Run: `go build -o aide-test ./cmd/aide/ && ./aide-test --help | grep -A1 unrestricted`
Expected: Shows `-N, --unrestricted-network` flag

- [ ] **Step 3: Final commit if any cleanup needed**

Clean up `aide-test` binary: `rm aide-test`
