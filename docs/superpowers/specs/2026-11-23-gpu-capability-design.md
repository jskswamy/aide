# GPU Capability Design (macOS + Linux)

**Date:** 2026-11-23
**Status:** Approved 2026-11-23 — EGM findings addressed (2 rounds)
**Tracks:** AIDE-750 (rewritten to this scope). Supersedes the macOS-only design in
`docs/superpowers/specs/2026-04-14-generic-toolchain-detection-design.md`, section "GPU capability".
**Related:** AIDE-362 (`aide doctor` command suggests `--with gpu` when GPU libraries are imported; depends on this).

## Problem

Agents running under aide's sandbox cannot use the GPU.

- **macOS (reproduced):** under aide's generated profile, Metal finds no GPU device and
  OpenGL fails to choose a pixel format (`CGLChoosePixelFormat` returns error 10002,
  the "cannot choose pixel format" error users report from ManimGL). See Evidence.
- **Linux (inferred from code, not reproduced; no Linux GPU hardware was available):**
  on the Landlock backend (the default, see Background), GPU device nodes in `/dev`
  are already reachable, but GPU libraries cannot read the sysfs and driver-registry
  paths they use to find the GPU. On the bubblewrap fallback, the sandbox gets a fresh
  minimal `/dev` with no GPU nodes at all.

ML workloads (PyTorch MPS/CUDA/ROCm, JAX, llama.cpp) and offscreen rendering (ManimGL,
headless OpenGL/Vulkan) therefore fail or fall back to the CPU.

## Background: how aide's sandbox works today

- **Capability:** a named bundle of sandbox grants, turned on with `aide --with <name>`
  or `capabilities: [<name>]` in config. Fields include `readable` (extra read-only
  paths), `env_allow` (environment variables passed to the agent) and `allow`
  (raw macOS sandbox rules; ignored on Linux). Capabilities can inherit with `extends`.
  Built-in capabilities live in `internal/capability/builtin.go`. A user capability
  with the same name as a builtin is merged into it (`mergeCapability`). The `clipboard`
  builtin is the precedent for a capability made only of `allow` rules.
- **Flow:** resolved capabilities are flattened into sandbox overrides
  (`ToSandboxOverrides`), merged into the config-level sandbox policy (`ApplyOverrides`),
  then converted into the runtime `Policy` the OS backend enforces.
- **macOS backend:** a seatbelt profile; `allow` strings are emitted as `(allow …)` rules.
- **Linux backends:** aide picks one at launch (`internal/sandbox/tier_linux.go`).
  **Landlock** is used unless it is absent, or its ABI is < 4 and the policy needs
  `network: none` (sandbox network mode "none"); in those cases aide falls back to
  **bubblewrap (bwrap)**.
  - Landlock already grants read-write and device ioctls on all of `/dev`, so device
    nodes are reachable for every agent. Landlock only restricts further: normal Unix
    permissions still apply, so `/dev/dri/renderD*` and `/dev/kfd` work only if the
    user is in the host's `render` (or `video`) group. NVIDIA nodes are usually
    world-accessible. Its default reads are `/usr`, `/bin`, `/lib*`,
    `/proc`, `/run`, `/nix/store`, a few `/etc` files and only `/sys/fs/cgroup` from sysfs.
  - bwrap mounts a fresh minimal `/dev` (`--dev /dev`) without GPU nodes.
- **Project trust gate:** a project's `.aide.yaml` contributes nothing until the user
  trusts it (`internal/launcher/project_gate.go`).

## Goals

1. `aide --with gpu` (or `capabilities: [gpu]`) enables GPU **compute** and
   **offscreen rendering** on macOS (Apple Silicon) and on Linux hosts using the
   Landlock backend (Intel, AMD, NVIDIA).
2. One capability for all vendors. The user never states which GPU they have.

## Non-goals

- GPU access under the bwrap fallback (tracked as a follow-up; see Risks).
- On-screen display (macOS WindowServer, X11/Wayland sockets).
- Intel Macs with AMD/Intel GPUs (different IOKit user-client classes).
- A user-facing `devices:` capability field.
- Narrowing Landlock's existing all-`/dev` ioctl grant (pre-existing; separate issue).
- Changing host group membership (`render`/`video`).

## Anti-goals

- **Never auto-activated.** No project-marker detection and no consent prompt. A project
  that imports torch or moderngl must not turn `gpu` on.
- The sandbox layer (`internal/sandbox`) must not check capability names.

## User-facing configuration

```sh
aide --with gpu            # this session only
aide --without gpu         # drop it for one session
aide cap show gpu          # what it grants on this OS
aide sandbox test          # print the generated profile / granted paths
```

```yaml
# profile or .aide.yaml: always on
capabilities: [gpu]
```

```yaml
# ~/.config/aide/sandbox.yaml: a custom capability built on gpu
capabilities:
  my-ml: { extends: gpu, env_allow: [HF_HOME] }
```

`capabilities: [gpu]` in a project's `.aide.yaml` takes effect only after that file
passes the existing project trust gate.

## Alternatives considered

| Platform | Option | Trade-off | Outcome |
|---|---|---|---|
| macOS | Capability `allow` rules, as `clipboard` does | No new code paths; 2 rule strings | Chosen |
| macOS | New opt-in `gpu` seatbelt guard (2026-04-14 spec) | Adds a guard type that only one capability uses, duplicating what `allow` already does | Rejected |
| Linux | Read-only sysfs + driver-registry paths and env vars (this design) | Covers Landlock, the default backend; no new fields; bwrap hosts get no GPU | Chosen for v1 |
| Linux | New `devices` field bound with `--dev-bind-try` under bwrap | Only affects the bwrap fallback; ~10 field-copy sites; as a user-facing key it needs a hardened trust boundary (character devices such as `/dev/sg*` and `/dev/nvme*` also reach disks) | Deferred: follow-up, builtin-only internal field |
| Linux | `internal/sandbox` special-cases the name `gpu` | Less code, but the sandbox would depend on capability names | Rejected |
| Linux | bwrap `--dev-bind /dev /dev` when `gpu` is on | Smallest bwrap change, but exposes every host device (disks, input, USB) | Rejected |

## Design

### macOS

The `gpu` builtin carries two `allow` rules:

```
iokit-open (iokit-user-client-class "AGXDeviceUserClient" "IOSurfaceRootUserClient")
mach-lookup (global-name "com.apple.MTLCompilerService")
```

`AGXDeviceUserClient` opens the Apple GPU driver; `IOSurfaceRootUserClient` is needed
for GPU surfaces (OpenGL); `MTLCompilerService` compiles Metal shaders at runtime. These
rules are the minimal set for the two probes run on an M5 Max (see Evidence); they are not
proven sufficient for every framework. On Linux they are ignored, as for `clipboard`.

### Linux (Landlock backend)

The `gpu` builtin adds read-only paths and environment variables **only when built on
Linux**: its `Readable` and `env_allow` lists are chosen by OS inside
`internal/capability`, in a form tests can build for either OS. This is the first
OS-conditional builtin. macOS therefore never sees Linux paths, and
`internal/sandbox` stays unaware of capability names.

| Path | Why |
|---|---|
| `/sys/class/drm`, `/sys/dev/char` | Mesa/libdrm find DRM render nodes through these symlink directories |
| `/sys/bus/pci/devices`, `/sys/devices/pci*` | The PCI device directories the symlinks resolve to (vendor/device IDs). Landlock checks the resolved path, so the targets must be granted |
| `/sys/class/kfd`, `/sys/devices/virtual/kfd` | AMD ROCm reads GPU topology here |
| `/etc/OpenCL/vendors`, `/etc/vulkan/icd.d`, `/etc/glvnd/egl_vendor.d` | Driver registries telling OpenCL, Vulkan and EGL which driver library to load |
| `/opt/rocm*` | ROCm install (versioned directories, e.g. `/opt/rocm-6.2.0`, plus the `/opt/rocm` link) |

Not needed: `/proc/driver/nvidia` (covered by `/proc`), NixOS driver libraries in
`/run/opengl-driver` (covered by `/run`), `/usr/share/vulkan` (covered by `/usr`),
device nodes (Landlock already grants `/dev`).

`env_allow`: `CUDA_VISIBLE_DEVICES`, `NVIDIA_VISIBLE_DEVICES`, `HIP_VISIBLE_DEVICES`,
`ROCR_VISIBLE_DEVICES`. Driver debugging variables are left to users via `extends: gpu`.

**Sysfs scope decision.** `TestLinuxSystemReadable_SysfsScope` keeps the *always-on*
read set narrow (`/sys/fs/cgroup` only) so ordinary agents cannot read `/sys/class/net`,
`/sys/bus/usb` or `/sys/kernel/security`. `gpu` does not change that baseline; it adds
sysfs reads only when the user opts in, and only the PCI and DRM/KFD subtrees, not
`/sys/devices` as a whole. PCI subtrees still contain information about other PCI devices
(network cards, USB controllers), read-only. Accepted for an opt-in capability.

**Missing paths.** Glob entries (`/sys/devices/pci*`, `/opt/rocm*`) are expanded at launch
and skip silently when nothing matches: the policy builder's path check
(`validateAndFilterPaths`, `internal/sandbox/policy.go`) skips globs, and the code that
derives the granted path set (`internal/sandbox/sandbox.go`) expands them and drops
unmatched ones. Plain paths that are absent produce the existing
`skipped: <path> (not found)` warning, as for any capability path. Expected on a typical
host: NVIDIA- or Intel-only hosts see up to 5 warnings (the two KFD paths and up to three
registries); AMD hosts with ROCm see 0–3. Accepted; turning these into globs is a
builder option if the noise is reported.

### Linux (bwrap fallback)

No change in v1. With `--with gpu`, the sysfs and registry reads apply (bwrap binds
them with `--ro-bind-try`), but no GPU device nodes exist inside the sandbox, so GPU
programs fall back to the CPU. `aide sandbox show` and the launch banner name the active
backend. Docs state this limit.

### What `gpu` changes, per backend

| Backend | Device nodes | Sysfs + registries | Env vars | Result |
|---|---|---|---|---|
| macOS seatbelt | n/a | n/a | n/a | Metal compute and CGL probes pass (M5 Max) |
| Linux Landlock | already granted (unchanged) | added | added | GPU expected to work (unverified on hardware) |
| Linux bwrap | none | added | added | No GPU (follow-up) |

## Evidence

**Code revision:** `4c3a225`. The spec file and `seams_report.md` are new, uncommitted files.

**Architecture (seams report, `seams_report.md`):** the capability → sandbox boundary is
one-way and narrow: `ToSandboxOverrides` → `ApplyOverrides` → `Policy`. This design adds a
new builtin, built per OS inside `internal/capability`; no new field crosses that boundary.

**macOS test (M5 Max, aide's generated profile from `aide sandbox test`, run with `sandbox-exec`):**

| Probe | aide profile today | With the 2 `allow` rules |
|---|---|---|
| Metal: create device, compile a shader at runtime, run a compute kernel | No device | Compute result correct |
| OpenGL 3.2 core offscreen context (CGL) | Error 10002 ("cannot choose pixel format") | Renderer "Apple M5 Max" |

Removal tests: without `AGXDeviceUserClient` no device is found; without
`IOSurfaceRootUserClient` OpenGL crashes; without `MTLCompilerService` shader compilation
fails. The WindowServer, `com.apple.CoreGraphics.*`, `com.apple.metal.*`, `IOAccelerator`
and framework reads proposed in the 2026-04-14 spec were not needed. The log line
`sandbox_extension_issue_file failed` appears for the executable's directory; it does not
affect the probe results.

**Linux (documentation only):**
- AMD ROCm container docs: `/dev/kfd` and `/dev/dri/renderD*` are required; `video`/`render` group.
- NVIDIA driver docs: `/dev/nvidiactl`, `/dev/nvidiaN`, `/dev/nvidia-uvm`, `/dev/nvidia-caps`.
- Flatpak: one `--device=dri` permission covers Mesa and NVIDIA nodes (prior art for a single capability).
- Mozilla bug 1384718 (https://bugzilla.mozilla.org/show_bug.cgi?id=1384718) and crosvm
  `src/linux/gpu.rs`: Mesa follows `/sys/dev/char/226:*` into `/sys/devices/pci…`.

**Not verified:** PyTorch MPS, MLX and llama.cpp on macOS; anything on Linux GPU hardware.

## Risks and mitigations

| Risk | Mitigation | Tracking |
|---|---|---|
| GPU drivers expose a large kernel ioctl surface | macOS: only when `gpu` is on. Linux Landlock: already exposed to every agent today (pre-existing, not changed here) | Follow-up: narrow Landlock's all-`/dev` ioctl grant |
| Linux paths are unverified on hardware | Docs mark Linux support as untested; paths chosen from vendor docs and Flatpak/Mesa practice | Follow-up: verify on NVIDIA and AMD hosts |
| bwrap hosts get no GPU | Docs state the limit; backend shown by `aide sandbox show` | Follow-up: builtin-only `Devices` field for bwrap |
| NVIDIA nodes created on first use (`/dev/nvidia-uvm`, `/dev/nvidia-caps/*`) are missing: CUDA creates them via the setuid `nvidia-modprobe`, which cannot gain privileges under Landlock's `no_new_privs` | Docs: create them on the host before launch (`nvidia-modprobe -u -c=0`, `nvidia-persistenced`, or udev rules) | Follow-up: verify on an NVIDIA host |
| User not in the host `render`/`video` group gets no GPU on Intel/AMD | Docs state the requirement | Accepted (host permission, outside aide) |
| GPUs under `/sys/devices/platform` or virtio (ARM SoCs, VMs) are not covered by the PCI sysfs grants | Docs state PCI GPUs only | Follow-up: hardware verification |
| PCI sysfs reads disclose other PCI device info | Opt-in only; read-only; narrower than `/sys/devices` | Accepted |
| User sets the sandbox-level `readable:` key (which replaces the default read list instead of adding to it); the `cfg.Readable` branch in `internal/sandbox/policy.go` then ignores capability reads, silently breaking `gpu` on Linux | Note in docs | Accepted (existing behaviour of `readable:`) |
| macOS frameworks need rules beyond the two probes | Manual probe documented; add rules when a framework is shown to need them | Follow-up: verify PyTorch MPS / MLX |

## Testing

- `gpu` builtin: resolves to the two `allow` rules on every OS; to the Linux `Readable`
  list and `env_allow` when built for `linux`; to no `Readable` and no `env_allow` entries
  for `darwin`.
- `aide cap show gpu`: lists the Linux paths and env vars on Linux and only the `allow`
  rules on macOS.
- `mergeCapability`: a user override of `gpu` (for example adding `env_allow`) keeps the
  builtin's `allow` and `Readable` entries.
- `extends: gpu` in a user capability inherits all of the above.
- macOS profile: with `--with gpu` the rendered profile contains both `allow` rules;
  without it, it does not.
- Linux grant derivation: a `Policy` built from `--with gpu` grants the sysfs and registry
  paths, with globs expanded and unmatched globs skipped without a warning.
- Regression: a project with torch or moderngl markers does not auto-enable `gpu`.
- Manual (macOS): the Metal and OpenGL probes succeed under `--with gpu`; steps documented
  in `docs/capabilities.md`.

## Left to the builder

- Test fixture layout and file placement.
- Documentation wording: a `gpu` row in `docs/capabilities.md` (including the bwrap limit
  and the host `render`/`video` group requirement) and a note in `docs/sandbox.md`.

## Review record

Review mode: Full (EGM + Ponytail), selected by the user on 2026-11-23.
Scope: one new builtin, OS-conditional inside `internal/capability`; no new cross-module fields.

**Ponytail (first draft):** 2 Major and 3 Nit findings, all fixed. The Major findings
(missing-path warnings; reach of a new field) no longer apply: the field was removed.

**EGM (Consolidated mode, stages 5–8; stage 9 not run, no implementation-plan section):**
reports `zoom_out_report.md`, `comprehension_report.md`, `critic_report.md`,
`intermediate_zoom_out_report.md`, `consolidated_review_report.md`.

| Action | Severity | Disposition |
|---|---|---|
| A1 Re-scope Linux to Landlock | Blocker | Fixed: user chose Landlock-first v1; `devices` deferred |
| A2 False "never exposes disks" claim | Blocker | Fixed: `devices` validation removed with the field |
| A3 Missing-path warnings decision | Major | Fixed: per-OS builtin; globs where natural; plain-path warnings accepted |
| A4 Broad sysfs reads vs existing test | Major | Fixed: PCI/DRM/KFD subtrees only; reasoning recorded |
| A5 Linux framing and risk wording | Major | Fixed: Problem, Background, per-backend table, Risks |
| A6 Test plan | Major | Fixed |
| A7 Over-claims | Major | Fixed |
| A8 Reach list for `Devices` | Major | Not applicable (field removed) |
| A9 Validation failure, trust posture | Major | Fixed: trust gate stated; validation removed |
| A10 Undefined baseline terms | Major | Fixed: Background section |
| N1–N11 | Nit | Fixed. N9 (NVIDIA lazy nodes) re-opened in round 2: it also affects Landlock (setuid helper under `no_new_privs`); now a documented risk with follow-up |

**EGM round 2 (re-audit; design, zoom-out, style, comprehension):** all round-1 Blockers and
Majors confirmed resolved. New Majors fixed: NVIDIA first-use nodes under Landlock (Risks);
`env_allow` OS rule made consistent (Linux-only); `render`/`video` group explained
(Background); sandbox-level `readable:` named; "data only" wording corrected. Nits fixed:
warning count, `aide cap show` test, `aide sandbox show` (not `test`) names the backend,
wording calibrations, platform/virtio GPUs noted. Re-audit cap (1) reached; no third round.

**Driver-registry and ROCm reads:** confirmed by the user at final approval.

**Final approval:** user, 2026-11-23. Linux ships in v1, marked untested on GPU hardware
(user choice over deferring it); bugs are fixed as they are reported.
