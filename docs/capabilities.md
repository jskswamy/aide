# Capabilities

You're debugging a failing deployment. You need kubectl, Docker, and AWS access.
Without capabilities, you'd configure sandbox guards individually - figure out
which guards to unguard, which paths to make readable, which env vars to pass
through. With capabilities:

```
aide --with k8s docker aws
```

Three words. The sandbox opens exactly the right paths, passes exactly the right
environment variables, and denies everything else.

## What Are Capabilities

Capabilities are task-oriented permission bundles that map what you're doing to
what the agent can access. Instead of configuring which security rules to
disable, you declare what tools you need.

Each capability bundles:

- **Paths** to make readable or writable (e.g., `~/.kube/`)
- **Environment variables** to pass through the sandbox boundary (e.g., `KUBECONFIG`)
- **Denies** to block specific paths within the capability's scope

The sandbox remains deny-default. Capabilities punch precise holes in it.

## Built-in Capabilities

aide ships with 24 built-in capabilities covering cloud providers, containers,
orchestration, infrastructure tools, language runtimes, developer tools, and
network access.

### Cloud Providers

| Capability | What it unlocks | Paths | Key env vars |
|------------|----------------|-------|-------------|
| `aws` | AWS CLI credentials | `~/.aws/` | `AWS_PROFILE`, `AWS_REGION`, `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_SESSION_TOKEN` |
| `gcp` | Google Cloud CLI | `~/.config/gcloud/` | `CLOUDSDK_CONFIG`, `GOOGLE_APPLICATION_CREDENTIALS` |
| `azure` | Azure CLI | `~/.azure/` | `AZURE_CONFIG_DIR` |
| `digitalocean` | DigitalOcean CLI | `~/.config/doctl/` | `DIGITALOCEAN_ACCESS_TOKEN` |
| `oci` | Oracle Cloud CLI | `~/.oci/` | `OCI_CLI_CONFIG_FILE` |

The `bedrock` and `vertex` [gateways](configuration.md#gateways) open `aws`
and `gcp` for the launch that selects them, and only that launch. Adding
one warns that this grants the whole capability.

### Containers and Orchestration

| Capability | What it unlocks | Paths | Key env vars |
|------------|----------------|-------|-------------|
| `docker` | Docker registry and daemon | `~/.docker/` | `DOCKER_CONFIG`, `DOCKER_HOST` |
| `k8s` | Kubernetes cluster access | `~/.kube/` | `KUBECONFIG` |
| `helm` | Helm charts and releases | `~/.kube/`, `~/.config/helm/`, `~/.cache/helm/` | `HELM_HOME`, `KUBECONFIG` |

### Infrastructure and Access

| Capability | What it unlocks | Paths | Key env vars |
|------------|----------------|-------|-------------|
| `terraform` | Terraform state and providers | `~/.terraform.d/` | `TF_CLI_CONFIG_FILE` |
| `vault` | HashiCorp Vault secrets | `~/.vault-token` | `VAULT_ADDR`, `VAULT_TOKEN` |
| `ssh` | SSH keys and agent forwarding | `~/.ssh/` | `SSH_AUTH_SOCK` |
| `npm` | npm and yarn registries | `~/.npmrc`, `~/.yarnrc` | `NPM_TOKEN`, `NODE_AUTH_TOKEN` |

### Language Runtimes

| Capability | What it unlocks | Paths | Key env vars |
|------------|----------------|-------|-------------|
| `go` | Go toolchain | `~/go/` | `GOPATH`, `GOROOT`, `GOBIN` |
| `rust` | Rust toolchain | `~/.cargo/`, `~/.rustup/` | `CARGO_HOME`, `RUSTUP_HOME` |
| `python` | Python toolchain | `~/.pyenv/` | `PYENV_ROOT`, `VIRTUAL_ENV` |
| `ruby` | Ruby toolchain | `~/.rbenv/` | `RBENV_ROOT`, `GEM_HOME` |
| `java` | Java/JVM toolchain | `~/.sdkman/`, `~/.gradle/`, `~/.m2/` | `JAVA_HOME`, `SDKMAN_DIR` |

### Developer Tools

| Capability | What it unlocks | Paths | Key env vars |
|------------|----------------|-------|-------------|
| `github` | GitHub CLI and credentials | `~/.config/gh/` | `GITHUB_TOKEN`, `GH_TOKEN` |
| `git-remote` | Retired: grants nothing. Git remotes follow the sandbox's network rules (outbound by default; with `aide sandbox ports`, include 443 for HTTPS). Kept so configs that list it still load; remove it with `aide cap disable git-remote` | - | - |
| `gpg` | GPG keys and signing | `~/.gnupg/` | `GNUPGHOME` |
| `clipboard` | Read/write access to the system clipboard (macOS only) | - | - |
| `gpu` | GPU compute and offscreen rendering (Metal, OpenGL, CUDA, ROCm, Vulkan); see [GPU](#gpu) | Linux only: GPU sysfs, driver registries, `/opt/rocm*` | Linux only: `CUDA_VISIBLE_DEVICES`, `NVIDIA_VISIBLE_DEVICES`, `HIP_VISIBLE_DEVICES`, `ROCR_VISIBLE_DEVICES` |
| `ccstatusline` | Read access to ccstatusline's settings, for `aide statusline` invoked from a ccstatusline Custom Command widget | `~/.config/ccstatusline/settings.json` | - |
| `bivouac` | Read-only access to bivouac config and read/write access to bivouac/herdr state (`ssh` stays behind `--with ssh`) | `~/.config/bivouac` (read), `~/.local/state/bivouac`, `~/.local/state/.bivouac-state.json.lock`, `~/.local/state/herdr/client` | - |
| `ponytail` | Read/write access to ponytail's band task ledger | `~/.config/ponytail/band` | - |
| `rtk` | Read/write access to rtk's tracking database, tee spool and config, so its command-rewrite hook records savings under the sandbox | macOS: `~/Library/Application Support/rtk`; Linux: `~/.local/share/rtk`, `~/.config/rtk` (read) | - |
| `skillshare` | Read-only access to the skills, agents and plugins [Skillshare](skillshare.md) links into agent config dirs | `skills/` and `agents/` in `$XDG_CONFIG_HOME/skillshare` (else `~/.config/skillshare`), `$XDG_STATE_HOME/skillshare/plugins` (else `~/.local/state/skillshare/plugins`), and the safe `sources:` paths of Skillshare's config | - |
| `terminal-browser` | `terminal-browser` CLI data, state/runtime Unix sockets, read-only config, and interop dirs | Existing `terminal-browser-(dev-)?<8-hex>` dirs under XDG data/state/runtime, `~/.local/{state,share}/terminal-browser-interop` (or `$TERMINAL_BROWSER_INTEROP_DIR`), and `$XDG_CONFIG_HOME/terminal-browser` (or `$TERMINAL_BROWSER_CONFIG_DIR`, read-only) | `TERMINAL_BROWSER_CONFIG_DIR`, `TERMINAL_BROWSER_INTEROP_DIR`, `TERMINAL_BROWSER_INTEROP_TARGET`, `XDG_DATA_HOME`, `XDG_STATE_HOME` |

Five built-ins auto-enable themselves when their tool is set up:
`ccstatusline` when `~/.config/ccstatusline/settings.json` exists, `bivouac`
when `~/.config/bivouac` exists, `ponytail` when `~/.config/ponytail` or a
Skillshare ponytail plugin exists, `rtk` when its data dir (`~/Library/Application
Support/rtk` on macOS, `~/.local/share/rtk` on Linux) exists, and `skillshare` when Skillshare's config
dir (`$XDG_CONFIG_HOME/skillshare`, else `~/.config/skillshare`) exists. They
are added to the effective capability set (and shown as enabled by `aide cap
list`) without `--with` or a `capabilities:` entry. Opt out like any other
capability: `--without skillshare` for a session, or `disabled_capabilities:
[skillshare]` in `.aide.yaml`. Every other built-in requires explicit opt-in.

`terminal-browser` is **explicit opt-in** (`aide --with terminal-browser` or
`capabilities: [terminal-browser]`): on macOS it grants `signal (target others)`
(so `kill(pid, 0)` liveness checks detect host instances without deleting their
live sockets) and `network-outbound (remote tcp "localhost:*")` (so
`agent-browser` can reach Chromium's ephemeral local DevTools CDP port even
under `network: none` or `allow_ports`). Because macOS Seatbelt cannot scope
`signal` to a single PID or `localhost:*` to a single ephemeral port, `aide`
never enables it automatically. It resolves concrete existing
`terminal-browser-(dev-)?<8-hex>` directories in Go so grants work identically
on macOS Seatbelt and Linux Landlock, keeps its config directory read-only,
validates env-derived overrides against sensitive and system directories
(requiring writable overrides to stay strictly inside `$HOME`,
`/run/user/<uid>`, or the user's temp directory), and emits scoped macOS
`network-outbound` and `network-bind` Unix-socket rules for hashed state,
runtime, and interop directories. On Linux `bwrap` with `network: none`
(`--unshare-net`), filesystem-backed Unix sockets work via bind mounts, whereas
abstract Unix sockets cannot cross the network namespace.

`skillshare` grants read access only, to the symlink-resolved paths (and
the literal path when it differs), and skips dirs that do not exist. It
never grants Skillshare's `config.yaml`, its `hooks/`, `mcp/` or `logs/`
state, its backups (`~/.local/share/skillshare`) or its cache. A `sources:`
path is refused when it is relative, `$HOME`, `/`, outside `$HOME`, or
overlaps a sensitive dir (`~/.ssh`, `~/.aws`, ...), a harness's default
config dir, any profile's config dir, or aide's config dir.

### GPU

`gpu` lets the agent use the GPU for compute (PyTorch, JAX, llama.cpp) and
offscreen rendering (ManimGL, headless OpenGL or Vulkan). It is never
enabled automatically, even when a project imports `torch` or `moderngl`:
use `aide --with gpu` or `capabilities: [gpu]`. One capability covers
every GPU vendor.

```bash
aide --with gpu            # this session only
aide cap show gpu          # what it grants on this OS
aide sandbox test --with gpu
```

```yaml
# In your profile or .aide.yaml
capabilities: [gpu]
```

To define your own capability on top of it:

```yaml
# ~/.config/aide/sandbox.yaml
capabilities:
  my-ml: { extends: gpu, env_allow: [HF_HOME] }
```

**macOS (Apple Silicon).** Adds two sandbox rules: opening the Apple GPU
driver and IOSurface (`AGXDeviceUserClient`, `IOSurfaceRootUserClient`),
and looking up the Metal shader compiler (`com.apple.MTLCompilerService`).
Tested with Metal compute and an offscreen OpenGL context. Intel Macs and
on-screen windows are not covered. If a framework still fails, check that
both rules appear in `aide sandbox test --with gpu` and report the error.

**Linux (Landlock).** Untested on real GPU hardware: the paths come from
vendor, Mesa and Flatpak documentation. Landlock already lets every agent
open `/dev`, so `gpu` adds only read-only paths that GPU libraries use to
find the GPU and its driver:

| Path | Used by |
|------|---------|
| `/sys/class/drm`, `/sys/dev/char`, `/sys/bus/pci/devices`, `/sys/devices/pci*` | Mesa and libdrm finding render nodes and PCI IDs |
| `/sys/class/kfd`, `/sys/devices/virtual/kfd` | AMD ROCm GPU topology |
| `/etc/OpenCL/vendors`, `/etc/vulkan/icd.d`, `/etc/glvnd/egl_vendor.d` | Picking the OpenCL, Vulkan and EGL driver |
| `/opt/rocm*` | ROCm install |

Paths that do not exist on your machine are skipped. Globs skip silently;
plain paths print `skipped: <path> (not found)`, so an NVIDIA host shows a
few warnings for the AMD paths. These are harmless.

Limits on Linux:

- **Host permissions still apply.** Intel and AMD render nodes
  (`/dev/dri/renderD*`, `/dev/kfd`) need your user in the `render` (or
  `video`) group.
- **NVIDIA device files created on first use** (`/dev/nvidia-uvm`,
  `/dev/nvidia-caps/*`) cannot be created inside the sandbox, because
  `nvidia-modprobe` is setuid and Landlock blocks privilege gain. Create
  them on the host before launching: run `nvidia-modprobe -u -c=0`, enable
  `nvidia-persistenced`, or add udev rules.
- **bubblewrap fallback gets no GPU.** When aide falls back to bwrap (see
  `aide sandbox show`, line "Backend"), the sandbox has a fresh `/dev`
  without GPU nodes, so programs fall back to the CPU.
- **PCI GPUs only.** GPUs under `/sys/devices/platform` (ARM boards) or
  virtio GPUs in VMs are not covered.
- **The sandbox-level `readable:` key replaces the default read list**, and
  then capability reads, including `gpu`'s, are ignored. Use `readable_extra:`
  instead.
- The PCI paths also show read-only information about other PCI devices.

When the GPU does not work under `aide --with gpu`, please open an issue
with: the output of `aide sandbox show` and `aide sandbox test --with gpu`,
the failing command and its error, and `ls -l /dev/dri /dev/nvidia* /dev/kfd`.

### Network

| Capability | What it unlocks | Paths | Key env vars |
|------------|----------------|-------|-------------|
| `network` | Unrestricted network access (inbound and outbound), overriding the default outbound-only sandbox network mode | - | - |

Run `aide cap list` to see all available capabilities including any custom ones
you've defined. Run `aide cap show <name>` to inspect a specific capability's
full permissions.

## Activation

Capabilities can be activated for a single session or persisted in your profile
config.

### Session-scoped: --with

```bash
aide --with k8s docker         # enable for this session
aide --with my-deploy          # works with custom capabilities too
```

`--with` does not modify your config. When the session ends, the capabilities
are gone.

### Profile-scoped: config

```yaml
# ~/.config/aide/profiles/work.yaml
capabilities: [k8s-dev, docker]
```

Every time you run `aide` in a directory matching the `work` profile, these
capabilities activate automatically.

### Excluding: --without

```bash
aide --without docker          # disable docker for this session
```

If your profile config includes `docker` but you don't need it right now,
`--without` removes it for this session only. Config is never modified.

### How they combine

CLI `--with` appends to profile capabilities. CLI `--without` removes from
profile capabilities. The result is resolved once at session start and baked
into the immutable Seatbelt profile. The agent cannot escalate permissions
mid-session - this is a hard security constraint. To change capabilities, start
a new session.

## Custom Capabilities

Built-in capabilities cover common tools. For your specific workflows, define
custom capabilities that extend, restrict, or combine them.

### Extending a built-in

Create a `k8s-dev` capability that inherits everything from `k8s` but adds
specific config paths and blocks production:

```bash
aide cap create k8s-dev \
  --extends k8s \
  --readable "~/.kube/dev-config" \
  --readable "~/.kube/staging-config" \
  --deny "~/.kube/prod-config"
```

This produces a config entry in `~/.config/aide/sandbox.yaml`:

```yaml
# ~/.config/aide/sandbox.yaml
capabilities:
  k8s-dev:
    extends: k8s
    readable: ["~/.kube/dev-config", "~/.kube/staging-config"]
    deny: ["~/.kube/prod-config"]
```

The child inherits all of the parent's grants (paths, env vars, guard changes)
and adds its own on top.

### Combining multiple capabilities

Bundle several capabilities into one name for a common workflow:

```yaml
# ~/.config/aide/sandbox.yaml
capabilities:
  my-deploy:
    combines: [aws, k8s, docker]
    deny: ["~/.kube/prod-config"]
```

Now `aide --with my-deploy` activates all three plus the production deny.

### Rules

- `extends` and `combines` are mutually exclusive on the same capability
- Maximum inheritance depth is 10 (catches accidental deep chains)
- Circular references are detected and rejected
- All referenced names must exist (built-in or user-defined)

### Management commands

```bash
aide cap create                # interactive guided flow
aide cap create k8s-dev \      # expert mode with flags
  --extends k8s \
  --readable "~/.kube/dev-config"

aide cap show k8s-dev          # inspect grants, denies, inheritance chain

aide cap edit k8s-dev \        # modify an existing capability
  --add-readable "~/.kube/staging-config" \
  --add-deny "~/.kube/prod-config" \
  --remove-deny "~/.kube/old-config" \
  --add-env-allow KUBECONFIG_EXTRA

aide cap enable k8s-dev        # persist in current profile config
aide cap disable k8s-dev       # remove from current profile config
```

## Protection

### never_allow: the hard ceiling

Some paths should never be accessible, regardless of which capabilities are
active. `never_allow` is a field in `~/.config/aide/sandbox.yaml` that overrides everything.

```yaml
# ~/.config/aide/sandbox.yaml
never_allow:
  - "~/.kube/prod-config"
  - "~/.aws/accounts/production"
  - "~/Documents/personal"
```

Implementation detail: `never_allow` paths are appended to the deny list after
all capability resolution. All paths are symlink-resolved before being added
to the profile.

**Platform note:** on macOS, Seatbelt's deny-wins semantics enforce this
unconditionally, even for a path nested inside an already-granted directory.
On Linux, bubblewrap does the same. Landlock, the primary Linux backend, has
no deny rule, so aide blocks reading files inside a **read-only** grant by
granting that directory's entries one by one (while keeping directory listing
`LANDLOCK_ACCESS_FS_READ_DIR` on the split directory so tools such as `ssh`
`Include` globs can still discover allowed siblings) and leaving the denied
ones out. Three consequences:

- Directory listing (`readdir`) remains permitted under a split read-only
  directory (including denied subdirectories beneath it), while reading any
  denied file's contents (`LANDLOCK_ACCESS_FS_READ_FILE`) is blocked.
- A file or directory created inside a split directory after launch is not
  readable until the next launch.
- A denied path inside a **writable** grant (the project directory, for
  example) is not enforced under Landlock. Splitting it would stop the agent
  creating files there. `aide sandbox test` marks these entries
  `# not enforced`.

**Example: protecting production kubeconfig**

You use `k8s` regularly for development. Your `~/.kube/` directory contains both
dev and production configs. Without protection, `aide --with k8s` would make the
entire directory readable - including production credentials.

```yaml
# ~/.config/aide/sandbox.yaml
never_allow:
  - "~/.kube/prod-config"

capabilities:
  k8s-dev:
    extends: k8s
    readable: ["~/.kube/dev-config", "~/.kube/staging-config"]
```

Even if someone later creates a broader capability that tries to read
`~/.kube/prod-config`, the `never_allow` rule blocks it on macOS and under
bubblewrap. The built-in `k8s` capability makes `~/.kube` writable, so under
Landlock the rule is not enforced while `k8s` is active. That is why `k8s-dev`
above grants only the specific dev/staging files: a narrow grant doesn't need
`never_allow` to carve an exception out of it.

### never_allow_env: blocking sensitive variables

```yaml
# ~/.config/aide/sandbox.yaml
never_allow_env:
  - VAULT_ROOT_TOKEN
  - PRODUCTION_DB_PASSWORD
```

These environment variables are stripped even if a capability lists them in
`env_allow`. If the `vault` capability includes `VAULT_TOKEN` in its env_allow
list but you've added `VAULT_TOKEN` to `never_allow_env`, the variable will not
pass through.

### CLI management

```bash
aide cap never-allow "~/.kube/prod-config"         # add a path
aide cap never-allow --env VAULT_ROOT_TOKEN         # add an env var
aide cap never-allow --list                         # show all never-allow rules
aide cap never-allow --remove "~/.kube/prod-config" # remove a path
```

## Credential Warnings

Some capabilities expose environment variables that contain credentials -
`AWS_SECRET_ACCESS_KEY`, `VAULT_TOKEN`, `DIGITALOCEAN_ACCESS_TOKEN`. aide cannot
block these variables without breaking the capability (the tool is useless
without its credentials). Instead, aide warns at session start:

```
⚠ credentials exposed: AWS_SECRET_ACCESS_KEY, AWS_SESSION_TOKEN
```

The warning lists the exact variable names so you make an informed choice. When
multiple capabilities combine credential access with network egress, aide adds a
composition warning:

```
⚠ 3 capabilities combine credential + network access
```

## CLI Reference

All capability management lives under `aide cap`.

### Viewing

| Command | Purpose |
|---------|---------|
| `aide cap list` | All available capabilities (built-in + custom) |
| `aide cap show <name>` | Details: grants, denies, env_allow, resolved inheritance chain |

### Creating and editing

| Command | Purpose |
|---------|---------|
| `aide cap create` | Interactive guided flow |
| `aide cap create <name> [flags]` | Expert mode with `--extends`, `--readable`, `--writable`, `--deny`, `--env-allow` |
| `aide cap edit <name> [flags]` | Modify with `--add-readable`, `--add-deny`, `--remove-deny`, `--add-env-allow` |

### Enabling and disabling

| Command | Purpose |
|---------|---------|
| `aide cap enable <name>` | Persist capability in current profile config |
| `aide cap disable <name>` | Remove capability from current profile config |

### Protection

| Command | Purpose |
|---------|---------|
| `aide cap never-allow <path>` | Add a path to the global deny list |
| `aide cap never-allow --env <var>` | Add an env var to the global deny list |
| `aide cap never-allow --list` | Show all never-allow rules |
| `aide cap never-allow --remove <path>` | Remove a never-allow rule |

### Auditing and inspection

| Command | Purpose |
|---------|---------|
| `aide cap check <cap1> [cap2...]` | Preview composition - shows merged permissions before launching |
| `aide cap audit` | All active capabilities with full resolved permissions |
| `aide cap suggest-for-path <path>` | Map a path to the capability that would grant access (used by sandbox error interception) |

## Banner

When capabilities are active, the session banner shows what the agent can
access. The banner is capability-centric - it shows what's granted, not what's
blocked.

### Symbols

| Symbol | Meaning |
|--------|---------|
| `✓` (green) | Capability active |
| `✗` (red) | Path denied (never-allow) |
| `○` (dim) | Capability disabled this session via `--without` |
| `⚠` (yellow) | Warning - credentials exposed or composition risk |
| `⚡` (red bold) | Auto-approve mode active - always the last line |

### Example banner

```
aide · infra (claude)
   github.com/acme/infra
   sandbox: network outbound
      ✓ k8s-dev   ~/.kube/dev-config, ~/.kube/staging-config
      ✓ aws       ~/.aws/ (AWS_PROFILE=staging)
      ✓ docker    ~/.docker/config.json
      ✗ denied    ~/.kube/prod-config (never-allow)

      ⚠ credentials exposed: AWS_SECRET_ACCESS_KEY, AWS_SESSION_TOKEN
```

With mixed sources (`aide --with vault --without docker --auto-approve`):

```
aide · infra (claude)
   github.com/acme/infra
   sandbox: network outbound
      ✓ k8s-dev   ~/.kube/dev-config, ~/.kube/staging-config
      ✓ aws       ~/.aws/ (AWS_PROFILE=staging)
      ✓ vault     ~/.vault-token (VAULT_ADDR)            ← --with
      ○ docker    disabled for this session               ← --without
      ✗ denied    ~/.kube/prod-config (never-allow)

      ⚠ credentials exposed: AWS_SECRET_ACCESS_KEY, VAULT_TOKEN

   ⚡ AUTO-APPROVE - all agent actions execute without confirmation
```

The `← --with` and `← --without` annotations appear only for session-scoped
overrides, so you can tell at a glance what came from config versus CLI flags.
