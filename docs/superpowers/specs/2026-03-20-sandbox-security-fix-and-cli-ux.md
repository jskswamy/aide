# Sandbox Security Fix + CLI UX — Spec

**Date:** 2026-03-20
**Status:** Draft
**Depends on:** Epic 3 (Agent Sandboxing) — completed
**Parked:** Domain/URL proxy filtering (AIDE-wga) — future epic

---

## Problem

Two critical security bugs and missing CLI UX for sandbox management:

1. **Passthrough path** (`aide --yolo` without config) never applies the OS sandbox — the agent runs with full filesystem access despite the warning claiming otherwise.
2. **Launch path** (`aide --yolo` with config but no `sandbox:` block) skips sandbox because `rc.Context.Sandbox == nil` short-circuits the application, even though `PolicyFromConfig(nil, ...)` correctly returns `DefaultPolicy`.
3. **No CLI commands** exist for inspecting or modifying sandbox policy — users must hand-edit YAML.
4. **`aide which`** doesn't show the active sandbox policy.
5. **`aide init`/`aide setup`** never mention sandbox exists.
6. **Replace semantics** for denied/readable/writable are dangerous — specifying `denied:` in config replaces all defaults, silently removing SSH key and credential protections.

---

## Design Decisions

### DD-18: Sandbox always applied unless explicitly disabled

The OS sandbox applies in ALL code paths (passthrough and launch) with `DefaultPolicy` unless the user explicitly sets `sandbox: false` in their context config. This is the "sandboxed by default" promise from DESIGN.md.

**Passthrough mode (no config file):** Always sandboxed with `DefaultPolicy`. There is no opt-out — if you want to disable the sandbox, create a config file with `sandbox: false`. This is intentional: the zero-config path should be the safest path.

**Launch mode (with config):** Sandboxed with `DefaultPolicy` unless the context has `sandbox: false`.

### DD-19: Network config supports both string and structured form

Backward-compatible schema change. Old `network: outbound` (string) still works. New structured form adds port filtering:

```yaml
# String form (backward compatible)
sandbox:
  network: outbound

# Structured form (new)
sandbox:
  network:
    mode: outbound
    allow_ports: [443, 53, 22]
```

### DD-20: Extra fields append to defaults, override fields replace

New `*_extra` fields (`denied_extra`, `readable_extra`, `writable_extra`) append to the default policy lists. Existing `denied`, `readable`, `writable` fields continue to replace defaults entirely (power user escape hatch).

```yaml
# Safe: adds to defaults
sandbox:
  denied_extra:
    - ~/.kube
    - ~/.terraform.d

# Power user: replaces ALL defaults
sandbox:
  denied:
    - ~/.ssh/id_*
    - ~/.aws/credentials
```

---

## Changes

### 1. Always Apply Sandbox (Security Fix)

#### File: `internal/launcher/passthrough.go`

`execAgent()` currently just execs the agent raw. The `Passthrough` method must thread `cwd` through to `execAgent` (change signature to accept `cwd string`).

**New imports required in passthrough.go:**
- `"github.com/jskswamy/aide/internal/sandbox"`
- `aidectx "github.com/jskswamy/aide/internal/context"`
- `"os/exec"` (already imported)

```go
func (l *Launcher) execAgent(cwd, name, binary string, extraArgs []string) error {
    if l.Yolo {
        yoloArgs, err := YoloArgs(name)
        if err != nil {
            return err
        }
        extraArgs = append(yoloArgs, extraArgs...)
    }

    // Always apply default sandbox policy (DD-18).
    // Passthrough has no config file, so there's no opt-out here.
    // Users who need sandbox: false must create a config (aide init).
    homeDir, _ := os.UserHomeDir()
    tempDir := os.TempDir()
    projectRoot := aidectx.ProjectRoot(cwd)

    rtDir, err := NewRuntimeDir()
    if err != nil {
        return fmt.Errorf("creating runtime dir: %w", err)
    }
    // NOTE: cleanup is best-effort. syscall.Exec replaces this process,
    // so deferred cleanup never runs. The runtime dir (containing the
    // .sb profile) persists until CleanStale() sweeps it on next invocation.
    // The sandbox-exec process reads the profile at startup, so the file
    // must exist when exec happens — this is correct.
    _ = CleanStale()

    policy := sandbox.DefaultPolicy(projectRoot, rtDir.Path(), homeDir, tempDir)
    cmd := &exec.Cmd{
        Path: binary,
        Args: append([]string{binary}, extraArgs...),
        Env:  os.Environ(),
    }
    sb := sandbox.NewSandbox()
    if err := sb.Apply(cmd, policy, rtDir.Path()); err != nil {
        _ = rtDir.Cleanup()
        return fmt.Errorf("applying sandbox: %w", err)
    }

    return l.Execer.Exec(cmd.Path, cmd.Args, cmd.Env)
}
```

Update all callers of `execAgent` to pass `cwd`:
- `Passthrough()` line 68: `return l.execAgent(cwd, agentOverride, binary, extraArgs)`
- `Passthrough()` line 89: `return l.execAgent(cwd, name, binary, extraArgs)`

#### File: `internal/launcher/launcher.go`

Remove the nil guard. Change line 162 from:

```go
if rc.Context.Sandbox != nil && !rc.Context.Sandbox.Disabled {
```

To always build the policy:

```go
// Always apply sandbox (DD-18). PolicyFromConfig handles nil -> defaults.
policy, err := sandbox.PolicyFromConfig(rc.Context.Sandbox, projectRoot, rtDir.Path(), homeDir, tempDir)
if err != nil {
    cleanup()
    return fmt.Errorf("building sandbox policy: %w", err)
}
if policy != nil { // nil only when sandbox: false
    cmd := &exec.Cmd{
        Path: binary,
        Args: append([]string{binary}, extraArgs...),
        Env:  env,
    }
    sb := sandbox.NewSandbox()
    if err := sb.Apply(cmd, *policy, rtDir.Path()); err != nil {
        cleanup()
        return fmt.Errorf("applying sandbox: %w", err)
    }
    binary = cmd.Path
    extraArgs = cmd.Args[1:]
    env = cmd.Env
}
```

#### File: `cmd/aide/main.go`

Fix yolo warning to be accurate — remove hardcoded policy description, replace with "Default sandbox policy will be applied."

### 2. Network Config Schema

#### File: `internal/config/schema.go`

Add `NetworkPolicy` struct with `UnmarshalYAML` that handles both string and map:

```go
type NetworkPolicy struct {
    Mode       string `yaml:"mode,omitempty"`       // "outbound", "none", "unrestricted"
    AllowPorts []int  `yaml:"allow_ports,omitempty"` // empty = all ports
    DenyPorts  []int  `yaml:"deny_ports,omitempty"`
}

func (n *NetworkPolicy) UnmarshalYAML(unmarshal func(interface{}) error) error {
    // Try string first (backward compat)
    var s string
    if err := unmarshal(&s); err == nil {
        n.Mode = s
        return nil
    }
    // Try map
    type alias NetworkPolicy
    return unmarshal((*alias)(n))
}
```

Update `SandboxPolicy`:

```go
type SandboxPolicy struct {
    Disabled        bool           `yaml:"-"`
    Writable        []string       `yaml:"writable,omitempty"`
    Readable        []string       `yaml:"readable,omitempty"`
    Denied          []string       `yaml:"denied,omitempty"`
    WritableExtra   []string       `yaml:"writable_extra,omitempty"`
    ReadableExtra   []string       `yaml:"readable_extra,omitempty"`
    DeniedExtra     []string       `yaml:"denied_extra,omitempty"`
    Network         *NetworkPolicy `yaml:"network,omitempty"`
    AllowSubprocess *bool          `yaml:"allow_subprocess,omitempty"`
    CleanEnv        *bool          `yaml:"clean_env,omitempty"`
}
```

#### File: `internal/sandbox/sandbox.go`

Add `AllowPorts` and `DenyPorts` to `Policy` struct:

```go
type Policy struct {
    Writable        []string
    Readable        []string
    Denied          []string
    Network         NetworkMode
    AllowPorts      []int  // empty = all ports allowed
    DenyPorts       []int
    AllowSubprocess bool
    CleanEnv        bool
}
```

#### File: `internal/sandbox/policy.go`

Update `PolicyFromConfig` to:
- Handle new `NetworkPolicy` struct (extract mode, ports)
- Merge `*_extra` fields by appending to defaults
- Existing override fields still replace

**Call sites that must be updated** when `Network` changes from `string` to `*NetworkPolicy`:
- `PolicyFromConfig()` in `internal/sandbox/policy.go` line 70: `if cfg.Network != ""` → check `cfg.Network != nil` and extract `.Mode`
- `ValidateSandboxConfig()` in `internal/sandbox/policy.go` line 123: validate `cfg.Network.Mode` instead of `cfg.Network`

```go
// IMPORTANT: Override blocks must come BEFORE extra blocks in the code,
// so we can skip extra processing when an override is present.
// This avoids wasted work and makes the intent clear.

// Override fields replace entirely (power user escape hatch)
if len(cfg.Denied) > 0 {
    d, err := ResolvePaths(cfg.Denied, templateVars)
    if err != nil {
        return nil, err
    }
    policy.Denied = d
} else if len(cfg.DeniedExtra) > 0 {
    // Extra fields append to defaults (safe default path)
    extra, err := ResolvePaths(cfg.DeniedExtra, templateVars)
    if err != nil {
        return nil, err
    }
    policy.Denied = append(policy.Denied, extra...)
}

// Same pattern for Readable and Writable:
if len(cfg.Readable) > 0 {
    // ... replace
} else if len(cfg.ReadableExtra) > 0 {
    // ... append
}

if len(cfg.Writable) > 0 {
    // ... replace
} else if len(cfg.WritableExtra) > 0 {
    // ... append
}

// Network policy extraction
if cfg.Network != nil {
    if cfg.Network.Mode != "" {
        policy.Network = NetworkMode(cfg.Network.Mode)
    }
    policy.AllowPorts = cfg.Network.AllowPorts
    policy.DenyPorts = cfg.Network.DenyPorts
}
```

### 3. Platform Port Filtering

#### File: `internal/sandbox/darwin.go`

When `AllowPorts` is non-empty, generate per-port Seatbelt rules instead of blanket `(allow network-outbound)`:

```scheme
;; Port-filtered outbound
(allow network-outbound (remote tcp "*:443"))
(allow network-outbound (remote tcp "*:53"))
(allow network-outbound (remote udp "*:53"))
(allow network-outbound (remote tcp "*:22"))
```

When `DenyPorts` is non-empty, add deny rules for those specific ports before the allow:

```scheme
(deny network-outbound (remote tcp "*:8080"))
```

**Implementation note:** The exact Seatbelt syntax for port-level filtering (`(remote tcp "*:PORT")`) varies across macOS versions and is sparsely documented by Apple. The implementation must:
1. Test the syntax against target macOS versions (minimum macOS 13+)
2. Include a **fallback path**: if per-port profile compilation fails (detected via `sandbox-exec -c` dry-run or profile parse error), fall back to blanket `(allow network-outbound)` and log a warning: "Port-level network filtering not supported on this macOS version; using blanket outbound."

#### File: `internal/sandbox/linux.go`

Landlock v4+ (kernel 6.7+) supports per-port TCP restrictions. The existing code uses `landlock.V5.BestEffort()` which includes V4 capabilities. For `AllowPorts`:

```go
for _, port := range policy.AllowPorts {
    rules = append(rules, landlock.NetPort(landlock.AccessNetConnectTCP, uint16(port)))
}
```

**Implementation note:** The exact go-landlock API function name must be verified against the pinned library version (`go get github.com/landlock-lsm/go-landlock@latest`). The function may be `landlock.NetPort`, `landlock.TCPPort`, or similar depending on version. `BestEffort()` will gracefully degrade if the kernel doesn't support network restrictions.

For bwrap fallback: port filtering is not supported. Log a warning and fall back to mode-only enforcement.

### 4. Sandbox CLI Commands

#### File: `cmd/aide/commands.go`

New `sandboxCmd()` with subcommands:

**`aide sandbox show [--context NAME]`**
- Resolves context for CWD (or named context)
- Builds effective policy via `PolicyFromConfig`
- Prints human-readable table:

```
Sandbox Policy (context: personal)

  Writable:
    /Users/subramk/source/github.com/jskswamy/aide
    /tmp
    /var/folders/.../aide-12345

  Readable:
    /usr/bin, /usr/local/bin, /bin, /usr/lib, /usr/share
    ~/.gitconfig, ~/.config/git, ~/.ssh/known_hosts

  Denied:
    ~/.ssh/id_ed25519, ~/.ssh/id_rsa_nvidia, ~/.ssh/id_rsa_yubikey
    ~/.aws/credentials, ~/.azure, ~/.config/gcloud
    ~/.config/aide/secrets
    ~/Library/Application Support/Google/Chrome, ~/.mozilla

  Network:    outbound (all ports)
  Subprocess: allowed
  Clean env:  no
```

**`aide sandbox deny <path> [--context NAME]`**
- Adds path to `denied_extra` in the context's sandbox config
- Creates sandbox block if absent
- Runs `aide validate` after

**`aide sandbox allow <path> [--read|--write] [--context NAME]`**
- Adds path to `readable_extra` (default) or `writable_extra` (with `--write`)
- Creates sandbox block if absent

**`aide sandbox reset [--context NAME]`**
- Removes the entire `sandbox:` block from the context config (reverts to defaults)
- Asks for confirmation

**`aide sandbox ports <port1> <port2> ... [--context NAME]`**
- Sets `network.allow_ports` on the context's sandbox config

**`aide sandbox test [--context NAME]`**
- Generates the platform-specific profile (Seatbelt .sb or Landlock rules)
- Prints it to stdout without launching the agent
- Useful for debugging
- On unsupported platforms: print "Sandbox not available on this platform (no-op sandbox)."
- Without config (passthrough): show `DefaultPolicy` profile
- Shows glob expansions so user can see which actual files are denied

### 5. `aide which` Shows Sandbox

#### File: `cmd/aide/commands.go`

Add sandbox section to `whichCmd()` output when `--resolve` is set:

```go
// After env vars section
homeDir, _ := os.UserHomeDir()
tempDir := os.TempDir()
sbPolicy, _ := sandbox.PolicyFromConfig(resolved.Context.Sandbox, projectRoot, rtDir, homeDir, tempDir)
if sbPolicy != nil {
    fmt.Fprintln(out, "Sandbox:")
    fmt.Fprintf(out, "  Writable    %s\n", summarizePaths(sbPolicy.Writable))
    fmt.Fprintf(out, "  Readable    %s\n", summarizePaths(sbPolicy.Readable))
    fmt.Fprintf(out, "  Denied      %s\n", summarizePaths(sbPolicy.Denied))
    portInfo := "all"
    if len(sbPolicy.AllowPorts) > 0 {
        portInfo = fmt.Sprintf("%v", sbPolicy.AllowPorts)
    }
    fmt.Fprintf(out, "  Network     %s (ports: %s)\n", sbPolicy.Network, portInfo)
} else {
    fmt.Fprintln(out, "Sandbox:  disabled")
}
```

### 6. `aide init` / `aide setup` Include Sandbox

#### File: `cmd/aide/commands.go`

Add sandbox step to `setupCreateNew()` after env wiring:

```
Step N: Sandbox
  Default policy protects SSH keys, cloud credentials, and browser profiles.
  [1] Use defaults (recommended)
  [2] Add extra denied paths
  [3] Disable sandbox (not recommended)
  Select [1]:
```

Option 2 prompts for paths to add to `denied_extra`. Option 3 sets `sandbox: false` with a warning.

### 7. Enhanced Validation

#### File: `internal/sandbox/policy.go`

Expand `ValidateSandboxConfig`:
- Validate network mode via `cfg.Network.Mode` (type changed from `string` to `*NetworkPolicy`)
- Validate port numbers (1-65535) in `AllowPorts` and `DenyPorts`
- Warn if `denied` is set without including default sensitive paths (SSH keys, cloud creds)
- Warn if `writable` includes `~` (home dir — too broad)
- Warn if both `denied` and `denied_extra` are set (confusing — extra is ignored when override is present)

**Integration note:** `config.WriteConfig` must be tested for round-trip fidelity with the new `*_extra` fields and `*NetworkPolicy` struct before CLI commands can be built. Add a `TestConfigRoundTrip_SandboxExtraFields` test.

---

## Tests

### Security fix tests

| Test | Description |
|------|-------------|
| `TestPassthrough_AppliesSandbox` | Verify sandbox-exec wraps the agent in passthrough mode |
| `TestPassthrough_SandboxDeniesSSHKeys` | Integration: cat ~/.ssh/id_* fails in passthrough sandbox |
| `TestPassthrough_NoOptOut_AlwaysSandboxed` | Verify passthrough mode has no way to disable sandbox (must create config) |
| `TestPassthrough_ExecAgent_UsesCwd` | Verify `execAgent` uses the `cwd` parameter for `ProjectRoot`, not `"."` |
| `TestLaunch_NoSandboxBlock_AppliesDefaults` | Verify sandbox applies when config has no sandbox: block |
| `TestLaunch_SandboxFalse_NoSandbox` | Verify sandbox: false still disables it |

### Network config tests

| Test | Description |
|------|-------------|
| `TestNetworkPolicy_UnmarshalString` | `network: outbound` parses as string mode |
| `TestNetworkPolicy_UnmarshalMap` | `network: {mode: outbound, allow_ports: [443]}` parses correctly |
| `TestSeatbeltProfile_PortFiltering` | Seatbelt profile contains per-port rules when allow_ports set |
| `TestSeatbeltProfile_NoPortFiltering` | Blanket network-outbound when no ports specified |

### Extra fields tests

| Test | Description |
|------|-------------|
| `TestPolicyFromConfig_DeniedExtra_AppendsToDefaults` | denied_extra adds to default denied list |
| `TestPolicyFromConfig_DeniedOverride_ReplacesDefaults` | denied replaces defaults (existing behavior preserved) |
| `TestPolicyFromConfig_BothDeniedAndExtra_OverrideWins` | If both set, denied replaces, extra entries absent from final policy |
| `TestConfigRoundTrip_SandboxExtraFields` | WriteConfig + Load round-trips *_extra fields and NetworkPolicy correctly |

### CLI tests

| Test | Description |
|------|-------------|
| `TestSandboxShow_DefaultPolicy` | aide sandbox show prints correct default policy |
| `TestSandboxDeny_AddsPath` | aide sandbox deny adds to denied_extra in config |
| `TestSandboxAllow_Read` | aide sandbox allow --read adds to readable_extra |
| `TestSandboxReset_RemovesBlock` | aide sandbox reset removes sandbox: from config |
| `TestSandboxPorts_SetsAllowPorts` | aide sandbox ports sets network.allow_ports |
| `TestSandboxTest_GeneratesProfile` | aide sandbox test prints Seatbelt profile |
| `TestSandboxTest_UnsupportedPlatform` | aide sandbox test on unsupported platform prints informative message |
| `TestSandboxTest_NoConfig` | aide sandbox test without config shows DefaultPolicy profile |

---

## Implementation Order

```
Task A: Security fix (passthrough + launch paths)     ← P0, do first
    │
    ├── Task B: Network schema + port filtering        ← can parallel with C
    │     ├── B1: Config schema (NetworkPolicy)
    │     ├── B2: darwin.go port rules
    │     └── B3: linux.go port rules
    │
    ├── Task C: Extra fields merge semantics           ← can parallel with B
    │     ├── C1: Schema + policy merge
    │     └── C2: Validation warnings
    │
    └── Task D: CLI commands                           ← depends on B, C
          ├── D1: aide sandbox show
          ├── D2: aide sandbox deny/allow/reset/ports
          ├── D3: aide sandbox test
          ├── D4: aide which --resolve sandbox section
          └── D5: aide init/setup sandbox step
```

---

## Out of Scope

- **Domain/URL filtering** — parked as AIDE-wga, requires local proxy (future epic)
- **Per-process sandboxing** — current sandbox wraps the entire agent process tree
- **Remote config sync** — no git remote configured
