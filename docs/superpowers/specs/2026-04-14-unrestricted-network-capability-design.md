# Unrestricted Network Capability

**Status:** Approved
**Date:** 2026-04-14

## Problem

Aide's sandbox defaults to outbound-only network access with optional
port filtering. Some workflows require inbound listening -- notably,
Claude Code's `/login` command starts a local daemon that listens for
an OAuth callback. Without inbound network access, OAuth login fails
inside the sandbox.

No CLI mechanism exists to grant unrestricted network access for a
session.

## Design

Two new opt-in mechanisms, neither persistent:

### 1. `--with network` capability

A new built-in capability named `network`. When activated via
`aide --with network`, it sets the network mode to `unrestricted`,
which emits `(allow network*)` in the seatbelt profile. Port
allow/deny rules from `.aide.yaml` still apply.

This capability uses a new `NetworkMode` field on the `Capability`
struct. The field flows through `SandboxOverrides` to the policy
layer, where it overrides the default `outbound` mode.

### 2. `--unrestricted-network` / `-N` flag

A dedicated CLI flag that forces fully unrestricted network access
and clears all port allow/deny lists from config. This flag is a
superset of `--with network`.

Use `-N` when config-level port restrictions must be bypassed,
such as when the OAuth callback port is unpredictable.

### Interaction model

| Flag | Network mode | Config port rules | Use case |
|------|-------------|-------------------|----------|
| *(none)* | outbound | Applied | Normal coding |
| `--with network` | unrestricted | Applied | All ports, respect deny list |
| `-N` | unrestricted | Ignored | OAuth login, dev servers |

`-N` implicitly includes `--with network`. Specifying both is
redundant but harmless.

## Data Flow

```
CLI: aide --with network claude
  -> MergeCapNames adds "network" to list
  -> ResolveCapabilities finds "network" capability
  -> SandboxOverrides.NetworkMode = "unrestricted"
  -> ApplyOverrides sets sandboxCfg.Network = "unrestricted"
  -> PolicyFromConfig keeps AllowPorts/DenyPorts from .aide.yaml
  -> NetworkGuard emits (allow network*) + port deny rules

CLI: aide -N claude
  -> Flag parsed in main.go
  -> sandboxCfg.Network = "unrestricted"
  -> sandboxCfg.AllowPorts = nil, sandboxCfg.DenyPorts = nil
  -> NetworkGuard emits (allow network*) only
```

## Components Changed

| File | Change |
|------|--------|
| `internal/capability/builtin.go` | Add `network` capability with `NetworkMode` field |
| `internal/capability/capability.go` | Add `NetworkMode` to `Capability` and `SandboxOverrides` |
| `cmd/aide/main.go` | Add `--unrestricted-network` / `-N` flag |
| `internal/sandbox/policy.go` | Apply `NetworkMode` from overrides; `-N` clears port lists |
| `internal/ui/banner.go` | Show network mode when non-default |

The network guard, seatbelt profile generation, and sandbox execution
remain unchanged.

## Banner Display

When network mode is non-default, the banner includes it:

```
aide v1.4.4 | Claude Code | Network: unrestricted
```

Default outbound mode shows no indicator.

## Threat Model

### Attack surface

Adding unrestricted network access opens two categories of risk:
inbound listeners and port-deny bypass.

| Threat | Severity | Likelihood | Residual risk |
|--------|----------|------------|---------------|
| Data exfiltration via outbound | Medium | Low (needs compromise + opt-in) | Low |
| Inbound listeners (reverse shell, C2) | Medium | Low (needs compromise + opt-in) | Medium |
| Port deny bypass via `-N` | Low | Low (explicit flag required) | Low |
| Persistent misconfiguration | Low | Low (per-invocation only) | Low |
| Local service access (Docker, DBs) | Medium | Low (needs compromise + opt-in) | Medium |

### Mitigations

1. **Banner indicator** -- session banner shows the active network
   mode so the user always sees what is enabled.
2. **No persistence** -- neither flag writes to config. Each session
   starts with the default outbound mode.
3. **Two-tier design** -- `--with network` respects config port
   rules; `-N` overrides them. Users choose the level of openness
   they need.
4. **Filesystem sandbox unchanged** -- even with full network access,
   the process sandbox still constrains filesystem, process, and IPC
   operations.

### Verdict

The risks are acceptable. Both flags require explicit per-session
opt-in. The two-tier design gives users a safe middle ground. The
filesystem and process sandbox still constrains what a compromised
process can do even with full network access.

## Testing

- Unit: `network` capability resolves to `NetworkMode: "unrestricted"`
- Unit: `-N` flag clears port allow/deny lists
- Unit: `--with network` preserves port deny lists from config
- Unit: banner shows network mode when non-default
- Integration: seatbelt profile contains `(allow network*)` with
  either flag
