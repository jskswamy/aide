# Epic: Linux Sandbox Guard System Parity

**Date:** 2026-03-24
**Status:** Planned
**Priority:** P2
**Depends on:** macOS sandbox fixes (2026-03-24-nix-toolchain-guard-expansion-design.md)

## Problem

The Linux sandbox backend completely ignores the guard system. SSH keys, cloud
credentials, browser profiles, password managers — all unprotected on Linux.
The guard system is Darwin-only.

## Issues

### 1. Linux backend ignores `Policy.Guards` entirely

**Severity: Critical**

`linuxWritable`, `linuxReadable`, `linuxDenied` (`linux.go:57-87`) derive paths
from `Policy.ProjectRoot`, `Policy.RuntimeDir`, `Policy.TempDir`, and
`Policy.ExtraDenied` directly. `Policy.Guards` is never read. All credential
guards (ssh-keys, cloud-*, browsers, password-managers, etc.) are inert on Linux.

### 2. `deny_ports` without `allow_ports` blocks ALL TCP on Landlock

**Severity: Important**

Landlock is default-deny. When only `DenyPorts` is set (no `AllowPorts`), the
code adds NO `ConnectTCP` rules, which blocks all TCP. The user intended to block
specific ports, not all networking.

### 3. bwrap doesn't bind `/nix` or non-standard system paths

**Severity: Important**

bwrap setup (`linux.go:234-265`) binds `/usr`, `/etc/resolv.conf`, `/proc`,
`/dev`, `/lib`, `/lib64` but NOT `/nix`, `/opt/homebrew`, or other non-standard
paths. NixOS systems are completely broken under bwrap.

### 4. Linux hardcodes `os.UserHomeDir()`

**Severity: Important**

`linuxReadable` calls `os.UserHomeDir()` directly instead of using the homeDir
from Policy/Context. Inconsistent with Darwin and breaks in test scenarios.

### 5. Linux integration tests don't test guard-based policies

**Severity: Important**

All Linux integration tests use raw `Policy` structs with just
`ProjectRoot`/`RuntimeDir`/`ExtraDenied`. None test with `Guards` populated.

### 6. Guard-to-Landlock rule mapping needed

**Severity: Important**

Need a translation layer from guard rules (which emit seatbelt syntax) to
Landlock restrictions. This is the core architectural work — guards currently
only produce seatbelt rules, and Linux needs a parallel output format.

## Approach Options

**A. Guard → abstract rules → platform backend** (recommended)
Guards emit abstract rules (AllowRead, AllowWrite, DenyRead, etc.) that each
platform backend translates to its native format.

**B. Dual-emit guards**
Each guard emits both seatbelt rules and Landlock rules. More duplication but
simpler to implement incrementally.

**C. Seatbelt-to-Landlock translator**
Parse the generated seatbelt profile and translate to Landlock. Fragile.
