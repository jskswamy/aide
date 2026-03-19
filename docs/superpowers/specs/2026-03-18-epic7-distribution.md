# Epic 7: Distribution -- Task 31: Nix Overlay and Packaging

**Date:** 2026-03-18
**Epic:** 7 (Distribution, P2)
**Task:** 31 -- Nix overlay and packaging (replace cctx)
**Dependencies:** Task 10 (agent launcher) must be complete; aide must build as a Go binary.

## Objective

Package `aide` as a Nix derivation with a flake, provide an overlay that
integrates into the existing nixos-config repository, and retire the current
`overlays/80-cctx.nix` overlay. After this task, `nix build` produces a
working `aide` binary and the overlay makes it available system-wide on both
darwin and linux machines.

## Background

The existing nixos-config infrastructure already provides:

- `sops` CLI (used for encrypting secrets; not needed at aide runtime, but
  available on the system for `aide secrets create/edit`)
- `age` and `age-plugin-yubikey` (already installed system-wide)
- A cctx overlay at `overlays/80-cctx.nix` that this work replaces

aide is a Go CLI tool. It shells out to `git` at runtime (for remote
detection and project root resolution). It uses `age` indirectly through the
sops Go library, but `age` and `age-plugin-yubikey` must be discoverable on
PATH for YubiKey-based decryption to work (the age plugin protocol requires
the plugin binary on PATH).

## Deliverables

### 1. `flake.nix` at the aide repository root

**File:** `/Users/subramk/source/github.com/jskswamy/aide/flake.nix`

```nix
{
  description = "aide - Universal Coding Agent Context Manager";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = nixpkgs.legacyPackages.${system};
      in
      {
        packages = {
          aide = pkgs.callPackage ./nix/package.nix { };
          default = self.packages.${system}.aide;
        };

        devShells.default = pkgs.mkShell {
          inputsFrom = [ self.packages.${system}.aide ];
          packages = with pkgs; [
            go
            gopls
            gotools       # goimports, etc.
            golangci-lint
            sops
            age
          ];
        };
      }
    ) // {
      overlays.default = final: prev: {
        aide = final.callPackage ./nix/package.nix { };
      };

      # Home-manager module (Step 8) -- export so consumers can:
      #   imports = [ inputs.aide.hmModules.default ];
      hmModules.default = import ./nix/hm-module.nix;
    };
}
```

Key points:
- `eachDefaultSystem` covers `x86_64-linux`, `aarch64-linux`,
  `x86_64-darwin`, `aarch64-darwin`.
- The overlay is defined outside `eachDefaultSystem` so it is
  system-independent (the consuming flake provides `final`/`prev` with the
  correct `system`).
- A `devShells.default` is provided so contributors can `nix develop` to get
  a full development environment.

### 2. Package derivation

**File:** `/Users/subramk/source/github.com/jskswamy/aide/nix/package.nix`

```nix
{ lib
, buildGoModule
, git
, makeWrapper
, installShellFiles
}:

buildGoModule rec {
  pname = "aide";
  version = "0.1.0";  # Update on release

  src = lib.cleanSource ./..;

  # After first build, replace this with the real hash from the error message.
  # Run: nix build 2>&1 | grep 'got:' to obtain it.
  vendorHash = null;  # null if using vendored deps (go mod vendor)
  # vendorHash = "sha256-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=";

  # Subpackage to build
  subPackages = [ "cmd/aide" ];

  nativeBuildInputs = [ makeWrapper installShellFiles ];

  # git is needed at runtime for context resolution (git remote get-url, git rev-parse)
  buildInputs = [ git ];

  ldflags = [
    "-s" "-w"
    "-X main.version=${version}"
    "-X main.commit=${if src ? rev then src.rev else "dev"}"
  ];

  postInstall = ''
    # Wrap the binary so git is always on PATH
    wrapProgram $out/bin/aide \
      --prefix PATH : ${lib.makeBinPath [ git ]}

    # Generate and install shell completions
    # (aide must support 'completion' subcommand via cobra)
    $out/bin/aide completion bash > aide.bash 2>/dev/null || true
    $out/bin/aide completion zsh > _aide 2>/dev/null || true
    $out/bin/aide completion fish > aide.fish 2>/dev/null || true
    installShellCompletion --bash aide.bash || true
    installShellCompletion --zsh _aide || true
    installShellCompletion --fish aide.fish || true
  '';

  meta = with lib; {
    description = "Universal Coding Agent Context Manager";
    longDescription = ''
      aide automatically resolves and launches the right coding agent
      (Claude, Gemini, Codex, etc.) with the correct context based on
      project configuration. No manual switching.
    '';
    homepage = "https://github.com/jskswamy/aide";
    license = licenses.mit;  # Adjust to actual license
    maintainers = [ ];
    mainProgram = "aide";
    platforms = platforms.linux ++ platforms.darwin;
  };
}
```

**Design decisions for the derivation:**

- **`buildGoModule`** is the standard Nix builder for Go projects. It handles
  Go module fetching, vendoring, and reproducible builds.
- **`vendorHash`**: Set to `null` if the repo vendors dependencies (`go mod
  vendor`). Otherwise, after the first build attempt, Nix will print the
  correct hash -- paste it in. This is the normal Nix Go workflow.
- **`makeWrapper`** + `wrapProgram`: Ensures `git` is on PATH at runtime even
  when aide is installed in a minimal environment. Without this, `git remote
  get-url` calls would fail.
- **Shell completions**: Generated at build time via cobra's completion
  subcommand and installed using `installShellFiles`. The `|| true` guards
  handle the case where the completion subcommand is not yet implemented.
- **`lib.cleanSource`**: Filters out `.git`, result symlinks, and other
  non-source files so the Nix store path is deterministic.

### 3. Overlay for nixos-config integration

**File (in nixos-config repo):** `overlays/80-aide.nix`

This file replaces the existing `overlays/80-cctx.nix`.

```nix
# overlays/80-aide.nix
# Replaces overlays/80-cctx.nix (cctx is succeeded by aide)
final: prev: {
  aide = final.callPackage (builtins.fetchGit {
    url = "https://github.com/jskswamy/aide";
    ref = "main";
    # Pin to a specific rev for reproducibility:
    # rev = "abc123...";
  } + "/nix/package.nix") { };
}
```

**Alternative: Use the flake overlay directly.** If the nixos-config already
uses flakes, the preferred approach is to add aide as a flake input and apply
its overlay:

```nix
# In nixos-config flake.nix inputs:
inputs.aide.url = "github:jskswamy/aide";

# In the nixosConfiguration or darwinConfiguration:
nixpkgs.overlays = [
  inputs.aide.overlays.default
  # ... other overlays
];
```

This is cleaner than `builtins.fetchGit` because it uses the flake lock file
for pinning.

**Removal of cctx:** Delete `overlays/80-cctx.nix` and remove any references
to the `cctx` package from system/home-manager configurations.

### 4. System package installation

In the nixos-config, add `aide` to the system packages where `cctx` was
previously listed:

```nix
# In the system or home-manager config where cctx was listed:
environment.systemPackages = with pkgs; [
  # ... other packages
  aide          # Replaces cctx
  # cctx        # REMOVED
];
```

### 5. Optional: Home-manager module

**File:** `/Users/subramk/source/github.com/jskswamy/aide/nix/hm-module.nix`

A home-manager module allows declarative configuration of aide through Nix.
This is optional -- aide works fine with its own `config.yaml` -- but provides
a Nix-native configuration path for users who prefer it.

```nix
{ config, lib, pkgs, ... }:

let
  cfg = config.programs.aide;
  yamlFormat = pkgs.formats.yaml { };
in
{
  options.programs.aide = {
    enable = lib.mkEnableOption "aide - Universal Coding Agent Context Manager";

    package = lib.mkOption {
      type = lib.types.package;
      default = pkgs.aide;
      defaultText = lib.literalExpression "pkgs.aide";
      description = "The aide package to install.";
    };

    settings = lib.mkOption {
      type = yamlFormat.type;
      default = { };
      description = ''
        Configuration written to $XDG_CONFIG_HOME/aide/config.yaml.
        See https://github.com/jskswamy/aide for schema.
      '';
      example = lib.literalExpression ''
        {
          agent = "claude";
          env = {
            ANTHROPIC_API_KEY = "{{ .secrets.anthropic_api_key }}";
          };
          secrets_file = "personal.enc.yaml";
        }
      '';
    };

    extraConfig = lib.mkOption {
      type = lib.types.lines;
      default = "";
      description = ''
        Extra configuration appended verbatim to config.yaml.
        Use this for YAML that is hard to express in Nix attribute sets.

        WARNING: This is a power-user escape hatch. The raw YAML lines are
        appended after the generated settings block, so duplicate keys or
        conflicting structure will produce invalid YAML. Prefer the
        `settings` option for any configuration that can be expressed as
        a Nix attribute set.
      '';
    };
  };

  config = lib.mkIf cfg.enable {
    home.packages = [ cfg.package ];

    xdg.configFile."aide/config.yaml" = lib.mkIf (cfg.settings != { }) {
      source = yamlFormat.generate "aide-config.yaml" cfg.settings;
    };
  };
}
```

**Usage in home-manager config:**

```nix
imports = [ inputs.aide.hmModules.default ];

programs.aide = {
  enable = true;
  settings = {
    agent = "claude";
    mcp_servers = [ "git" "context7" ];
  };
};
```

To expose this module from the flake, add to `flake.nix` outputs:

```nix
hmModules.default = import ./nix/hm-module.nix;
```

## Runtime Dependency Matrix

| Dependency | Required? | How Provided | Notes |
|---|---|---|---|
| `git` | Yes | Wrapped into aide's PATH via `makeWrapper` | Used for `git remote get-url`, `git rev-parse --show-toplevel` |
| `age` | No (at runtime) | Already installed system-wide in nixos-config | sops Go library handles decryption in-process; age binary only needed if age identity is a YubiKey (plugin protocol) |
| `age-plugin-yubikey` | Optional | Already installed system-wide in nixos-config | Only needed if using YubiKey-based age identities; the plugin protocol requires the binary on PATH |
| `sops` | No (at runtime) | Already installed system-wide in nixos-config | aide uses sops as a Go library; the sops CLI is only used for manual `sops edit` workflows |
| Agent binaries (`claude`, `gemini`, `codex`) | Yes (at least one) | Installed separately by user | aide discovers them on PATH; not bundled |

**Why `age` and `age-plugin-yubikey` are not wrapped into aide's PATH:**
These are already installed system-wide in the nixos-config. Wrapping them
into aide would create duplicate entries and version skew. The system
installation is the source of truth. If aide were distributed as a standalone
binary outside nixos-config, the package could optionally wrap these in, but
for our use case the system provides them.

## File Inventory

Files to create in the aide repo:

| File | Purpose |
|---|---|
| `flake.nix` | Flake entry point with packages, devShell, overlay |
| `nix/package.nix` | Go build derivation for aide |
| `nix/hm-module.nix` | Optional home-manager module |
| `.envrc` | `use flake` for direnv integration (optional) |

Files to modify in nixos-config:

| File | Action |
|---|---|
| `overlays/80-cctx.nix` | Delete (replaced by aide) |
| `overlays/80-aide.nix` | Create (or use flake overlay in flake.nix inputs) |
| System/HM config referencing `cctx` | Replace `cctx` with `aide` in package lists |

## Step-by-Step Implementation

### Step 1: Create `nix/package.nix`

Create the directory and file:

```bash
mkdir -p /Users/subramk/source/github.com/jskswamy/aide/nix
```

Write the package derivation as specified in Deliverable 2 above.

### Step 2: Create `flake.nix`

Write the flake as specified in Deliverable 1 at the repository root.

Also create a `.envrc` for developer convenience:

**File:** `/Users/subramk/source/github.com/jskswamy/aide/.envrc`

```
use flake
```

And add `flake.lock` tracking (the lock file should be committed so CI and
other developers get reproducible builds):

```bash
git add flake.nix flake.lock nix/
```

### Step 3: Set `vendorHash`

Run the initial build to determine the correct vendor hash:

```bash
cd /Users/subramk/source/github.com/jskswamy/aide
nix build 2>&1
```

If using `go mod vendor` (vendored deps checked into the repo), set
`vendorHash = null` in `nix/package.nix`. Otherwise, the build will fail with
a message like:

```
hash mismatch in fixed-output derivation:
  specified: sha256-AAAA...
  got:       sha256-XXXX...
```

Copy the `got:` hash into `vendorHash` in `nix/package.nix`.

### Step 4: Verify the build

```bash
# Build aide
nix build

# Verify binary exists
ls -la result/bin/aide

# Verify --help works
result/bin/aide --help

# Verify aide can resolve context (git must be on PATH)
result/bin/aide which
```

Expected outcomes:
- `result/bin/aide` exists and is executable
- `--help` prints the cobra-generated help text without errors
- `which` resolves the current directory's context (or prints a sensible
  "no config" message)

### Step 5: Verify git is available to aide

```bash
# The wrapped binary should have git on PATH even in a pure nix shell
nix shell .#aide --command sh -c 'aide which 2>&1; echo "exit: $?"'
```

This verifies that `makeWrapper` correctly injects `git` into the PATH.
Without it, `aide which` would fail with "git: command not found" errors
when trying to detect the git remote.

### Step 6: Cross-platform verification

```bash
# Check that the package evaluates for all target systems
nix eval .#packages.x86_64-linux.aide.name
nix eval .#packages.aarch64-linux.aide.name
nix eval .#packages.x86_64-darwin.aide.name
nix eval .#packages.aarch64-darwin.aide.name
```

All four should return `"aide-0.1.0"` without evaluation errors. Actual
cross-compilation builds require the appropriate system or a cross-compiler,
but evaluation confirms the derivation is valid for all platforms.

### Step 7: Integrate into nixos-config

Choose one of the two overlay approaches:

**Option A: Flake overlay (preferred)**

In the nixos-config `flake.nix`:

```nix
{
  inputs.aide.url = "github:jskswamy/aide";
  # or for local development:
  # inputs.aide.url = "path:/Users/subramk/source/github.com/jskswamy/aide";

  outputs = { self, nixpkgs, aide, ... }: {
    # In your nixosConfigurations or darwinConfigurations:
    nixosConfigurations.myhost = nixpkgs.lib.nixosSystem {
      modules = [
        {
          nixpkgs.overlays = [ aide.overlays.default ];
          environment.systemPackages = with pkgs; [ aide ];
        }
      ];
    };
  };
}
```

**Option B: Standalone overlay file**

Create `overlays/80-aide.nix` as specified in Deliverable 3.

Then remove the old overlay:

```bash
rm overlays/80-cctx.nix
```

And update any module that referenced `pkgs.cctx` to use `pkgs.aide`.

### Step 8: Create optional home-manager module

Write `nix/hm-module.nix` as specified in Deliverable 5.

Add the module export to `flake.nix` outputs:

```nix
hmModules.default = import ./nix/hm-module.nix;
```

### Step 9: Final validation checklist

Run all of these from the aide repo root:

```bash
# 1. Clean build succeeds
nix build --rebuild

# 2. Binary is at expected path
test -x result/bin/aide && echo "PASS: binary exists" || echo "FAIL"

# 3. --help works
result/bin/aide --help > /dev/null 2>&1 && echo "PASS: --help" || echo "FAIL"

# 4. which subcommand resolves context
result/bin/aide which 2>&1 | head -5

# 5. Flake check passes (runs any tests defined in the flake)
nix flake check

# 6. Dev shell works
nix develop --command go version

# 7. All target platforms evaluate
for sys in x86_64-linux aarch64-linux x86_64-darwin aarch64-darwin; do
  nix eval ".#packages.${sys}.aide.name" 2>/dev/null && \
    echo "PASS: ${sys}" || echo "FAIL: ${sys}"
done
```

## Notes for Implementer

1. **`vendorHash` is the most common stumbling block.** If you are vendoring
   dependencies with `go mod vendor`, set `vendorHash = null`. Otherwise,
   determining the correct hash requires a **two-pass build**: first set
   `vendorHash = ""` (empty string) and run `nix build`. The build will fail
   with a hash mismatch error showing the real hash on the `got:` line. Copy
   that hash into `vendorHash` and rebuild. This two-pass workflow is standard
   for all Nix Go module packages.

2. **Shell completions may not work on first pass** if the `completion`
   subcommand has not been implemented yet (Epic 6, Task 29). The `|| true`
   guards in `postInstall` handle this gracefully -- completions will simply
   not be installed until that task is done.

3. **The `version` in `nix/package.nix`** should be updated when cutting
   releases. For development, `0.1.0` is fine. Consider using `builtins.readFile`
   from a VERSION file if you want a single source of truth.

4. **Local development loop:** Use `nix develop` to get a shell with Go and
   all tools. Use `go build ./cmd/aide` for fast iteration. Use `nix build`
   only to verify the Nix packaging is correct.

5. **The home-manager module is optional.** aide's own `config.yaml` is the
   primary configuration mechanism. The HM module is a convenience for users
   who prefer declarative Nix configuration. It can be deferred if time is
   tight.

6. **`age` and `age-plugin-yubikey` must be on the system PATH** for
   YubiKey-based decryption. The sops Go library invokes the age plugin
   protocol, which discovers plugins by looking for `age-plugin-*` binaries
   on PATH. Since these are already installed system-wide in nixos-config,
   no action is needed -- but if aide were distributed standalone, you would
   need to add these to the `wrapProgram` PATH.
