# Linux Sandbox Threat Model — Landlock + seccomp + bwrap + memfd

**Status:** Accepted (codifies threat work landed in v1.15.0)
**Date:** 2026-06-07
**Implements:** [2026-03-24-linux-sandbox-parity-epic.md](2026-03-24-linux-sandbox-parity-epic.md)
**Related:** DESIGN.md DD-14, DD-15, DD-26, DD-28, DD-33

## Problem

The Linux sandbox now layers Landlock LSM, seccomp BPF, PID namespaces,
and a bwrap fallback. The implementation landed across 27 commits, each
of which addressed a specific threat or invariant. That thinking is
load-bearing for future contributors — when someone proposes a "small
cleanup" that re-introduces one of these patterns, the reviewer needs a
single place to point at.

This doc consolidates the threats considered, the defenses chosen, the
threats accepted with rationale, and the threats explicitly out of
scope. It is the prose accompaniment to DD-33 in `DESIGN.md`.

## Architecture summary

Three layered mechanisms, selected per launch by
`internal/sandbox/tier_linux.go::ComputeIsolationTier`:

| Layer | Mechanism | Purpose |
|---|---|---|
| Filesystem | Landlock LSM (ABI v5+) | Deny-default; per-subtree writable/readable with `FS_REFER` for safe cross-dir renames |
| Filesystem (fallback) | bwrap path-mount namespaces | Used when kernel lacks Landlock ABI v5 |
| Subprocess containment | seccomp BPF + PID namespace | Block `clone`/`fork`/`vfork`/`clone3` when `AllowSubprocess=false` |
| Policy delivery | `memfd_create` + fd inheritance | Pass policy bytes to the re-exec child via inherited fd; never via filesystem |

## Trust boundaries

| Actor | Trust | Has |
|---|---|---|
| Developer | Trusted | Host machine, source, creds, project access |
| `aide` launcher process | Trusted | Builds policy from config + guards, writes memfd, re-execs into `__sandbox-apply` |
| `aide --sandbox-apply` child | Trusted to read inherited fds, then drops privilege via Landlock+seccomp before exec'ing the agent | Receives policy via memfd, applies kernel restrictions, exec-s the agent |
| Sandboxed agent | **Untrusted** | Coding agent (Claude / Cursor / …) bounded by Landlock + seccomp |
| Sandbox grandchild (via `forkExecInPIDNamespace`) | **Untrusted, also bounded** | Spawned only when `AllowSubprocess=true`; inherits agent's restrictions |
| Host kernel | Trusted | Enforces Landlock and BPF; aide trusts the LSM |
| Host filesystem (config, secrets) | Trusted | Aide reads `~/.config/aide/*` and `.aide.yaml` |

## Threats addressed

Numbered T1–T13 in roughly the order they were addressed during PR #14.
Each entry names the defense, the implementation site, and the commit
that introduced the work.

### T1 — Secret-bearing policy file left on disk after SIGKILL

**Attack.** Earlier code wrote `runtimeDir/landlock-policy.json`
containing `policy.Env` — which may carry decrypted SOPS secret values
like `ANTHROPIC_API_KEY` — so the re-exec child could read it. A
SIGKILL between write and `defer cleanup` strands the plaintext file.

**Defense.** Policy delivered via `memfd_create` (kernel-anonymous
memory). The kernel reclaims memory when the last fd refcount drops, so
process death = memfd freed. No filesystem touched.

**Where.** `internal/sandbox/policy.go::writePolicyToMemfd`,
`internal/sandbox/seccomp_linux.go::writeSeccompBpfToMemfd`. Commit
`Pass sandbox policy via memfd, not filesystem`.

### T2 — Memfd fd closed by Go GC mid-handoff (EBADF)

**Attack.** `os.NewFile` installs a runtime finalizer on the inner
`*file`. When the launcher drops its reference to `cmd.ExtraFiles[0]`
(the memfd `*os.File`) between `applyLandlock` returning and
`syscall.Exec`, GC can fire the finalizer in the window and close the
fd in the launcher process. The re-exec child wakes to EBADF on read.
Non-deterministic; surfaces 30–50% of runs under allocation pressure.

**Defense.** Pin the `*os.File` in a package-level append-only slice
(`pinnedSandboxMemfds`). As long as the slice keeps the reference
reachable, the inner `*file` stays reachable and the finalizer cannot
run. `syscall.Exec` replaces the process image, freeing the slice;
error paths exit the process and the slice goes with it.

`runtime.SetFinalizer(f, nil)` does **not** fix this: the finalizer is
attached to `f.file` (unexported inner pointer), not to `f` itself.

**Where.** `internal/sandbox/policy.go::pinnedSandboxMemfds` and the
matching helper for `noSubprocessSeccompMemfd`. Regression pin:
`TestPolicyFD_SurvivesSyscallExec_AfterGC` deterministically drops
references and forces multi-pass GC between memfd creation and exec.
Commit `Pin sandbox memfd against GC to fix EBADF`.

### T3 — `clone3()` subprocess escape

**Attack.** seccomp filter denies `clone`, `fork`, `vfork`. Modern
libcs (glibc ≥ 2.34) call `clone3()` instead, which the filter doesn't
recognise. Subprocess spawned, `AllowSubprocess=false` bypassed.

**Defense.** Filter returns `ENOSYS` for `clone3` rather than allowing
or killing. `ENOSYS` is the kernel-canonical "this syscall isn't
supported here" signal, so glibc's fallback chain picks up the legacy
`clone()` path — which the filter blocks. No quiet pass-through; the
spawn family is closed regardless of libc version.

**Where.** `internal/sandbox/seccomp_linux.go::noSubprocessBPF`. Commit
`Apply tree-based deny-wins; ENOSYS for clone3`.

### T4 — System paths over-grant via sysfs

**Attack.** Initial Landlock policy granted read on all of `/sys` so
container-aware tools wouldn't break. That subtree exposes hardware
fingerprinting (`/sys/class/dmi/*`), network info (`/sys/class/net/*`),
and kernel module enumeration (`/sys/module/*`) — useful for both
exfiltration profiling and exploit-tooling.

**Defense.** Narrow to `/sys/fs/cgroup` only. That single subtree is
what cgroup-aware code actually consults; everything else under `/sys`
stays unreadable.

**Where.** `internal/sandbox/linux.go` system-paths section. Commit
`Narrow sysfs Landlock grant to /sys/fs/cgroup`.

### T5 — Deny-wins bypass at system-path bootstrap

**Attack.** User configures `denied_extra: ["/etc"]`. System-path
bootstrap re-adds `/etc` as readable for `resolv.conf` etc. The
bootstrap ran AFTER user deny rules in the previous pipeline, so the
user's intent was silently violated.

**Defense.** Tree-based deny-wins evaluation in
`DeriveGrantedPathSet`: any deny rule at any specificity beats any
allow rule for the same subtree. Bootstrap re-adds are filtered against
the deny set before emission. See `feedback_seatbelt_deny_wins.md`
memory for the macOS-side history of this rule.

**Where.** `internal/sandbox/sandbox.go::DeriveGrantedPathSet`. Commit
`Fix deny-wins bypass in system-path bootstrap`.

### T6 — Dotfile-symlink exfiltration

**Attack.** User symlinks `~/.cursor/skills` to `~/dotfiles/cursor-skills`
(common managed-config pattern). Landlock evaluates rules against the
kernel-resolved path (the inode), not the syscall argument. So an allow
rule on `~/.cursor` doesn't cover the symlink target — naive "make it
work" code would grant `~/dotfiles` wholesale, silently widening the
sandbox to include `~/.ssh` if those are also under `~/dotfiles`.

**Defense.** `expandConfigDirWritable` walks symlink targets but
filters anything under `sensitiveHomeDirs` (`~/.ssh`, `~/.aws`,
`~/.gnupg`, `~/.config/gcloud`, …). Targets outside `$HOME` are
rejected entirely. Scope is tight: a dir-symlink grants the resolved
target subtree; a file-symlink grants the target's parent dir (for
atomic-rename siblings) — never the full ancestor.

**Where.** `pkg/seatbelt/modules/helpers.go::expandConfigDirWritable`.
Commit `Follow dotfile symlinks under config dirs`.

### T7 — Aide binary unreadable in re-exec child

**Attack.** The launcher `syscall.Exec`s `aide --sandbox-apply`. If
Landlock policy doesn't include the aide binary as readable, the second
exec inside the child (replacing aide with the actual agent) fails
EACCES.

**Defense.** Allow-list the aide binary path explicitly in the bootstrap
ruleset. Split FS rules from network rules so the binary allow can't be
narrowed by user config in the wrong tier.

**Where.** `internal/sandbox/linux.go` allow-list section. Commit
`Allow-list aide binary; split FS/net rulesets`.

### T8 — Missing `FS_REFER` — silent `EXDEV` on cross-dir renames

**Attack.** cargo, npm pack, git temp-rename, the standard atomic-
write-via-tmpfile-then-rename idiom — all do cross-directory renames
within the workspace. `landlock.RWDirs()` deliberately omits
`LANDLOCK_ACCESS_FS_REFER` (per its godoc). Without `REFER`, Landlock
returns synthetic `EXDEV` for any rename whose source/dest dirfds
differ, even inside the same allow-listed subtree. Indistinguishable
from a "real" cross-filesystem error; build tools fail with no hint.

**Defense.** Chain `.WithRefer()` onto every RWDirs rule. The kernel
still validates that the destination's parent has at least the source's
access rights, so `REFER` doesn't permit escape; it only relaxes the
synthetic dirfd-mismatch denial.

**Where.** `internal/sandbox/linux.go::applyLandlock`. Regression pin:
`TestLandlock_RenameAcrossDirs_RequiresRefer` reproduces the rustc
pattern. Commit `Grant FS_REFER on writable rules for renames`.

### T9 — `policy.Env` desync after `applyAgentEnv` injection

**Attack.** `applyAgentEnv` injects module-specific env vars (e.g.,
`CLAUDE_CONFIG_DIR`) into `cmd.Env`. But `policy.Env` was snapshotted
BEFORE that injection. The re-exec child deserialises `policy.Env` to
resolve capability-guarded paths — if a module injects a non-default
value, the child resolves paths from the stale snapshot and grants the
wrong Landlock subtree. Silent `EACCES` at runtime.

**Defense.** Sync `policy.Env` from the actual `cmd.Env` immediately
after `applyAgentEnv` returns, before launching the sandbox-apply
child.

**Where.** `internal/launcher/launcher.go` post-`applyAgentEnv` sync.
Regression pin: `TestApplyAgentEnv_InjectedKeysReflectedInPolicyEnv`.
Commit `Sync policy.Env after applyAgentEnv for child`.

### T10 — Old Landlock ABI → unsandboxed launch

**Attack.** Kernels older than Landlock ABI v5 (≈ Linux 6.7) don't
support all the access masks the policy uses. The path of least
resistance is "if Landlock errors, run without a sandbox." That's what
some projects do; aide does not.

**Defense.** Detect Landlock ABI before applying. If insufficient, fall
back to bwrap path-mount namespaces. bwrap is degraded vs. Landlock
(no inode-tree granularity) but is real sandboxing. Banner reports
`sandbox: degraded` so the user knows. Never launch unsandboxed because
of an old kernel.

**Where.** `internal/sandbox/tier_linux.go::ComputeIsolationTier`,
`internal/sandbox/linux.go::detectLandlockABI`. Commit `Fall back to
bwrap for old Landlock ABI`.

### T11 — `forkExecInPIDNamespace` looks like an architectural rule violation

**Attack.** Project convention flags subprocess-spawning code outside
`internal/launcher`. `forkExecInPIDNamespace` lives in
`internal/sandbox/seccomp_linux.go` and calls `ForkExec`. A future
contributor might "fix" this by moving the call to launcher — which
would break PID-namespace setup, since the init must already be the
re-exec'd `__sandbox-apply` child.

**Defense.** Explicit doc comment at the call site explaining the
exemption: the original launcher no longer exists at this point;
launcher pre-exec hooks fired before re-exec and intentionally do not
apply to the sandboxed grandchild.

**Where.** Comment block above `forkExecInPIDNamespace`. Commit
`Document forkExecInPIDNamespace exec exemption`.

### T12 — Unconditional `~/.config/aide` read

**Attack.** Older bootstrap unconditionally granted read on
`~/.config/aide` so the agent could see context. That directory holds
the decrypted secret-stores cache. A coding agent should never have
blanket access.

**Defense.** Drop the unconditional read. Specific guards (e.g. the
secrets module) decide what under `~/.config/aide` the agent needs and
grant only those subtrees.

**Where.** `internal/sandbox/linux.go` bootstrap. Commit `Drop
unconditional ~/.config/aide read`.

### T13 — Banner reports "sandbox: disabled" while sandbox is active

**Attack.** Not a security threat by itself, but a security-UX threat:
operators trust the banner. `execAgent` in the passthrough path built
`BannerData` without setting `IsolationTier`, so the template fell
through to the nil branch and rendered `sandbox: disabled` — even
though `sb.Apply` had just installed Landlock. A user who believed
their sandbox was off might escalate trust elsewhere (`--auto-approve`,
broader caps) to "compensate".

**Defense.** Compute `tier := sandbox.PlatformIsolationTier(policy)`
after `sb.Apply` returns and pass `&tier` in `BannerData` on both
launcher paths.

**Where.** Passthrough path in `internal/launcher/`. Regression pin:
`TestPassthrough_BannerReflectsActiveSandboxTier`. Commit `Populate
BannerData.IsolationTier in passthrough`.

## Threats accepted

Listed with rationale so a future reviewer can decide if the calculus
has changed.

### TA1 — PID-ns init fork under `__sandbox-apply`

**What.** When `AllowSubprocess=true`, the sandbox-apply child forks
once to create a PID-ns init that then execs the agent. This fork uses
`syscall.ForkExec` outside `internal/launcher`.

**Why accepted.** Required by PID-namespace semantics (init must be
PID 1 inside the namespace). The fork happens inside the seccomp-
restricted child, after policy is applied, so the child inherits all
restrictions. The exemption is documented at the call site (T11).

### TA2 — bwrap fallback has coarser subprocess containment

**What.** bwrap path-mount namespaces don't give per-syscall control,
so `AllowSubprocess=false` is not strictly enforced on the bwrap path.

**Why accepted.** bwrap is the kernel-old fallback. Banner reports
`sandbox: degraded` so the user sees the difference. Kernel upgrade
restores full enforcement.

### TA3 — Capability-resolved paths recomputed in the re-exec child

**What.** Capability guards resolve paths from the deserialised
`policy.Env` inside the re-exec child. A malicious env injection
between launcher serialisation and child deserialisation could mis-
resolve paths.

**Why accepted.** Serialisation/deserialisation happens via memfd
(T1) — fd-inherited, immutable post-creation, no window for an
attacker to mutate. Defended by the same mechanism as T1.

## Threats out of scope

### TO1 — Setuid binaries in writable paths

The sandbox does not block the agent from writing a file with the
setuid bit set, nor from executing it (setuid execution is gated by
mount-option `nosuid` if set on the writable mount; aide does not
impose this). Closing would need a separate seccomp rule denying
`chmod`/`fchmodat` with mode masks containing `S_ISUID`. Tracked as
future scope only.

### TO2 — TIOCSTI terminal injection

The agent can write to its controlling terminal via the `TIOCSTI`
ioctl, allowing pasted commands to surface as user input. The Linux
kernel's `dev.tty.legacy_tiocsti=0` sysctl closes this when set; aide
does not require it. Same residual risk as macOS Seatbelt today.

### TO3 — TOCTOU between policy generation and Landlock apply

Between `DeriveGrantedPathSet` (launcher process) and `applyLandlock`
(re-exec child), filesystem state can change — a symlink could be
swapped, a writable dir could move. The policy is recomputed in the
child from the same `policy.Env`, but the underlying filesystem is a
shared resource. Closing would need filesystem snapshotting, which is
out of scope.

### TO4 — Adversarial compromised aide binary

A compromised aide binary can write whatever policy it wants. Out of
scope; the security model assumes aide itself is trusted (the user
installs it via goreleaser-signed releases).

### TO5 — LLM-level prompt injection from tool responses

Out of scope for the kernel-side sandbox. The OS sandbox bounds the
blast radius of any *action* the agent takes in response to an
injection, but it has no view into *content* flowing into the agent's
context. Tracked separately under future network-filter / content-
inspection work.

## Open questions

1. **bwrap + `AllowSubprocess=false` reporting.** Currently this case
   reports `sandbox: degraded`. The user might assume subprocess
   containment is still active. Worth a stronger label (e.g.
   `sandbox: degraded; subprocess containment not enforced`)?

2. **Memfd policy integrity check.** Policy bytes have no checksum or
   HMAC. A kernel bug that corrupts the fd before the child reads
   would go undetected. Lowest-priority hypothetical; no observed
   instances. Worth a 4-byte CRC for cheap insurance?

3. **Transitive capability dependencies.** If guard A depends on guard
   B's paths being granted and the user disables B, A's rules still
   emit. Worth a graph-validation step at policy build time so
   misconfigurations fail loudly rather than silently.
