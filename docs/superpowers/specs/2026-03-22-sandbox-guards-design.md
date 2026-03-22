# Sandbox Guards

**Date:** 2026-03-22
**Status:** Approved

## Problem

The default sandbox deny list is incomplete. It blocks `~/.aws/credentials` and `~/.azure` but misses DigitalOcean, OCI, Kubernetes, Terraform, Vault, browser data, password managers, and dev tool tokens. Users have to manually add each path to `denied_extra`. Managing individual paths is tedious and error-prone.

SSH deny uses `~/.ssh/id_*` which catches public keys too, and misses non-standard private key filenames.

The seatbelt modules that provide toolchain and integration support (Node, Git, Keychain, Nix) are hardcoded in the profile builder with no way to configure them.

## Design

### Core Principle

**Everything is a guard module.** A guard is a seatbelt module that produces the complete set of rules — both deny and allow — for the resources it manages. Guards are the single system for all sandbox access decisions.

### Guard Interface

Guards extend the existing seatbelt `Module` interface with metadata:

```go
// pkg/seatbelt/module.go

// Module contributes Seatbelt rules to a profile. (unchanged)
type Module interface {
    Name() string
    Rules(ctx *Context) []Rule
}

// Guard is a Module with metadata for the guard system.
type Guard interface {
    Module
    Type() string        // "always", "default", "opt-in"
    Description() string // human-readable, shown in CLI
}
```

### Context

The seatbelt `Context` carries runtime information to guards:

```go
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

// EnvLookup searches ctx.Env for a KEY=VALUE entry and returns the value.
// Returns ("", false) if not found. Guards use this instead of os.Getenv().
func (c *Context) EnvLookup(key string) (string, bool)
```

Guards use `ctx.Env`, `ctx.GOOS`, and `ctx.EnvLookup()` instead of calling `runtime.GOOS` or `os.Getenv()` directly. This makes guards testable with injected values.

The `Network`, `AllowPorts`, `DenyPorts`, and `ExtraDenied` fields are set by the profile builder from the `Policy` struct before rendering. They are consumed by the `network` and `filesystem` always-guards respectively.

### Guard Types

Guard types describe configuration behavior. They answer: "can I change this?"

| Type | Default State | Meaning |
|------|--------------|---------|
| **always** | active | Agent needs this to function. Cannot be disabled via `unguard:`. |
| **default** | active | Protects important data. On by default, can be disabled. |
| **opt-in** | inactive | Extra restriction. Off by default, user chooses to enable. |

### Built-in Guards

#### Always (cannot be disabled)

| Guard | Purpose |
|-------|---------|
| `base` | `(version 1)`, `(deny default)` |
| `system-runtime` | OS binaries, devices, mach services, temp dirs, process rules |
| `network` | Network access mode, port filtering |
| `filesystem` | Project root (rw), home dir (ro), user-configured `denied:` paths |
| `keychain` | macOS Keychain access — Security framework mach-lookup, file access, IPC |
| `node-toolchain` | npm/yarn/pnpm/nvm/corepack/Prisma/Turborepo paths |
| `nix-toolchain` | Nix store, nix profile paths |
| `git-integration` | Git config (read-only), SSH config/known_hosts |

#### Default (active by default, can disable)

| Guard | Paths | Env Override |
|-------|-------|-------------|
| `ssh-keys` | `~/.ssh` (deny), `known_hosts`/`config` (allow) | n/a |
| `cloud-aws` | `~/.aws/credentials`, `~/.aws/config`, `~/.aws/sso/cache`, `~/.aws/cli/cache` | `AWS_SHARED_CREDENTIALS_FILE`, `AWS_CONFIG_FILE` |
| `cloud-gcp` | `~/.config/gcloud` | `CLOUDSDK_CONFIG`, `GOOGLE_APPLICATION_CREDENTIALS` |
| `cloud-azure` | `~/.azure` | `AZURE_CONFIG_DIR` |
| `cloud-digitalocean` | `~/.config/doctl` | n/a |
| `cloud-oci` | `~/.oci` | `OCI_CLI_CONFIG_FILE` |
| `kubernetes` | `~/.kube/config` | `KUBECONFIG` (colon-separated: each path denied individually) |
| `terraform` | `~/.terraform.d/credentials.tfrc.json`, `~/.terraformrc` | `TF_CLI_CONFIG_FILE` |
| `vault` | `~/.vault-token` | `VAULT_TOKEN_FILE` |
| `browsers` | See browser paths table below | n/a |
| `password-managers` | See password manager paths table below | n/a |
| `aide-secrets` | `~/.config/aide/secrets` | n/a |

**Meta-guard `cloud`:** Expands to `cloud-aws` + `cloud-gcp` + `cloud-azure` + `cloud-digitalocean` + `cloud-oci`.

**Meta-guard `all-default`:** All guards with type `default`.

#### Opt-in (inactive by default)

| Guard | Paths | Env Override |
|-------|-------|-------------|
| `docker` | `~/.docker/config.json` | `DOCKER_CONFIG` |
| `github-cli` | `~/.config/gh` | n/a |
| `npm` | `~/.npmrc`, `~/.yarnrc` | n/a |
| `netrc` | `~/.netrc` | n/a |
| `vercel` | `~/.config/vercel` | n/a |

### Browser Paths (OS-Aware)

The `browsers` guard uses `ctx.GOOS` to emit platform-specific deny rules:

| Browser | macOS | Linux |
|---------|-------|-------|
| Chrome | `~/Library/Application Support/Google/Chrome` | `~/.config/google-chrome` |
| Firefox | `~/Library/Application Support/Firefox` | `~/.mozilla/firefox` |
| Safari | `~/Library/Safari` | n/a |
| Brave | `~/Library/Application Support/BraveSoftware` | `~/.config/BraveSoftware` |
| Edge | `~/Library/Application Support/Microsoft Edge` | `~/.config/microsoft-edge` |
| Arc | `~/Library/Application Support/Arc` | n/a |
| Chromium | `~/Library/Application Support/Chromium` | `~/.config/chromium` |
| Chromium (snap) | n/a | `~/snap/chromium` |

### Password Manager Paths

The `password-managers` guard denies CLI password store paths only:

| Tool | Path |
|------|------|
| 1Password CLI | `~/.config/op`, `~/.op` |
| Bitwarden CLI | `~/.config/Bitwarden CLI` |
| pass | `~/.password-store` |
| gopass | `~/.config/gopass` |
| GPG private keys | `~/.gnupg/private-keys-v1.d`, `~/.gnupg/secring.gpg` |

`~/Library/Keychains` is **not** included here. The `keychain` guard (type: `always`) manages macOS Keychain access with proper allow rules for Security framework, mach services, and IPC. Agents need keychain access for OAuth authentication.

### SSH Keys: Self-Contained Guard

The `ssh-keys` guard produces both deny and allow rules:

**Denied:** `~/.ssh` (entire directory via `subpath`)

**Allowed** (via `literal`, more specific than `subpath` — wins in seatbelt):
- `~/.ssh/known_hosts` (git host verification)
- `~/.ssh/config` (git host aliases)
- `~/.ssh` (directory listing for metadata traversal)

This catches non-standard private key filenames (`work_key`, `github`, etc.) while keeping git operations functional. Since the guard controls both sides, there is no specificity conflict.

### Env Var Override Behavior

When a guard checks an env var override:
- If set and non-empty: resolved path **replaces** (not appends to) the default path for that specific entry
- Other default paths in the same guard remain unchanged
- Example: `AWS_SHARED_CREDENTIALS_FILE=/custom/creds` → guard denies `/custom/creds` instead of `~/.aws/credentials`, but still denies `~/.aws/config`, `~/.aws/sso/cache`, `~/.aws/cli/cache`

### Agent Modules

Agent-specific modules (e.g., `ClaudeAgent`) are not guards. They are selected by the launcher based on which agent is being run and added to the profile after all guards. They implement the `Module` interface.

### Config Schema

```yaml
contexts:
  work:
    sandbox:
      guards: [ssh-keys, cloud-aws]        # override: ONLY these + always guards
      guards_extra: [docker]               # extend: add to defaults
      unguard: [browsers]                  # remove from active set
      denied: [/custom/secret]             # explicit path deny → filesystem guard
      denied_extra: [/another/secret]      # extend explicit denies
      network: outbound
      allow_ports: [443, 80]
      deny_ports: [22]
      clean_env: false
```

```go
type SandboxPolicy struct {
    Guards      []string `yaml:"guards,omitempty"`
    GuardsExtra []string `yaml:"guards_extra,omitempty"`
    Unguard     []string `yaml:"unguard,omitempty"`
    Denied      []string `yaml:"denied,omitempty"`
    DeniedExtra []string `yaml:"denied_extra,omitempty"`
    Network     string   `yaml:"network,omitempty"`
    AllowPorts  []int    `yaml:"allow_ports,omitempty"`
    DenyPorts   []int    `yaml:"deny_ports,omitempty"`
    CleanEnv    *bool    `yaml:"clean_env,omitempty"`
}
```

### Guard Resolution

1. No `guards:` set → active = all `always` guards + all `default` guards
2. `guards:` set → active = all `always` guards + listed guards (replaces `default` set). Listing an `always` guard in `guards:` is silently deduplicated (no warning, no error).
3. `guards_extra:` set (no `guards:`) → active = always + default + guards_extra
4. Expand meta-guards (`cloud` → 5 individual guards)
5. Remove `unguard:` entries. Attempting to unguard an `always` guard → validation error
6. If both `guards:` and `guards_extra:` set → warn, ignore `guards_extra:`
7. If any guard name is not found in the registry (built-in or custom) → validation error: `unknown guard "typo-guard"`

### Unguard Semantics

- `unguard: [browsers]` — removes `browsers` from active set
- `unguard: [cloud]` — expands meta-guard, removes all `cloud-*` guards
- `unguard: [all-default]` — removes all default guards (leaves always guards active)
- `unguard: [base]` → error: `cannot unguard "base": type is always`
- `unguard: [docker]` when docker is not active → no-op, no error

### Denied Path Resolution

The `denied:` and `denied_extra:` config fields feed into `Policy.ExtraDenied`, which the `filesystem` guard consumes. These are user-specified explicit denies, independent of guard-derived denies.

- `denied:` alone → `ExtraDenied` = those paths
- `denied_extra:` alone → `ExtraDenied` = those paths (extends empty default)
- Both set → warn, `denied_extra:` ignored (same behavior as `guards:` + `guards_extra:`)

There is no "default" deny path list to replace — guard-derived denies come from guard modules, not from the `denied:` field. The `denied:` field exists for one-off paths that don't warrant a custom guard.

### Seatbelt Conflict Resolution

Apple Seatbelt resolves conflicts by specificity: `(literal ...)` beats `(subpath ...)`. When two rules match at the same specificity, deny wins.

Guards that produce both deny and allow rules for their own paths use this intentionally:
- `ssh-keys`: `(deny ... (subpath ~/.ssh))` + `(allow ... (literal ~/.ssh/known_hosts))` → literal wins, known_hosts is readable
- `keychain` (always): only produces allow rules. No guard denies `~/Library/Keychains`, so there is no conflict.

**Opt-in guards overriding always guards:** When the `npm` opt-in guard is active, it denies `~/.npmrc`. The `node-toolchain` always guard allows `~/.npmrc`. Both use `(literal ...)` at the same specificity — deny wins. This is intentional: the user explicitly activated the `npm` guard to restrict npm auth token access, accepting that npm operations requiring auth won't work in the sandbox. Opt-in guards render after always guards in the profile, but the outcome is determined by seatbelt's deny-wins-at-same-specificity rule, not by rendering order.

### Custom Guards

Users define custom guards in config. Custom guards implement the `Guard` interface dynamically from config values:

```yaml
custom_guards:
  audit-logs:
    type: default
    description: "Audit logs and compliance data"
    paths:
      - "~/.config/audit"
  company-tokens:
    type: opt-in
    description: "Company CLI credentials"
    paths:
      - "~/.config/company-cli"
    env_override: COMPANY_CONFIG_DIR
  internal-certs:
    type: default
    description: "Internal certificate store"
    paths:
      - "~/.internal/certs"
    allowed:
      - "~/.internal/certs/ca.pem"
```

Custom guards produce `(deny file-read-data (subpath ...))` and `(deny file-write* (subpath ...))` rules for their `paths`, and `(allow file-read* (literal ...))` rules for their `allowed` entries. The `literal` allow is more specific than the `subpath` deny, so allowed entries win in seatbelt — the same pattern as the `ssh-keys` guard.

The `env_override` field applies when the custom guard has a **single path** entry. When set, the env var value replaces that path. Custom guards with multiple `paths` entries cannot use `env_override` — validation rejects this combination because the replacement target is ambiguous.

Custom guard names must not collide with built-in guard names. Defining `custom_guards.ssh-keys` is a validation error: `custom guard "ssh-keys" conflicts with built-in guard`.

Custom guards are referenced the same way as built-in guards:

```yaml
contexts:
  work:
    sandbox:
      guards_extra: [audit-logs, company-tokens]
```

### Custom Guard Types

Users can create additional types that map to a built-in behavior:

```yaml
guard_types:
  compliance:
    behavior: default
    description: "Audit logs and compliance certificates"
```

The `behavior` field determines the default state. Valid values are `default` or `opt-in`. Custom guard types **cannot** use `behavior: always` — this prevents users from creating irremovable guards through indirection. Validation rejects it: `custom guard type "immutable" cannot use behavior "always"`.

Custom guards can reference custom types:

```yaml
custom_guards:
  audit-logs:
    type: compliance
    description: "Audit log files"
    paths:
      - "~/.config/audit"
```

Custom guards **cannot** use `type: always`. Only built-in guards can be `always` — this prevents user-defined config from creating irremovable guards. Validation rejects `type: always` on custom guards: `custom guard "audit-logs" cannot use type "always"`.

If a custom guard references a type that does not exist (neither built-in nor custom), validation rejects it: `unknown guard type "nonexistent"`.

Built-in types (`always`, `default`, `opt-in`) cannot be modified or removed.

**Example: Complete custom guard type workflow:**

```yaml
# 1. Define a custom type with behavior mapping
guard_types:
  compliance:
    behavior: default
    description: "Audit logs and compliance certificates"
  team-internal:
    behavior: opt-in
    description: "Team-specific credential stores"

# 2. Define custom guards using custom types
custom_guards:
  audit-logs:
    type: compliance            # inherits "default" behavior → active by default
    description: "SOC2 audit log directory"
    paths:
      - "~/.config/audit"
  compliance-certs:
    type: compliance            # same type, also active by default
    description: "Internal CA certificates"
    paths:
      - "~/.internal/certs"
    allowed:
      - "~/.internal/certs/ca.pem"   # readable even though parent is denied
  team-vault:
    type: team-internal         # inherits "opt-in" behavior → inactive by default
    description: "Team credential vault"
    paths:
      - "~/.config/team-vault"
    env_override: TEAM_VAULT_DIR

# 3. Reference in context config
contexts:
  work:
    sandbox:
      guards_extra: [team-vault]   # opt-in guard needs explicit activation
      # audit-logs and compliance-certs are active by default (type behavior: default)
```

`aide sandbox guards` output with custom types:

```
GUARD              TYPE           STATUS    PATHS
...
audit-logs         compliance     active    ~/.config/audit (custom)
compliance-certs   compliance     active    ~/.internal/certs (custom)
team-vault         team-internal  inactive  ~/.config/team-vault (custom)
```

`aide sandbox types` output:

```
TYPE           DEFAULT    DESCRIPTION
always         active     Agent needs this to function. Cannot be disabled.
default        active     Protects important data. On by default, can be disabled.
opt-in         inactive   Extra restriction. Off by default, user chooses to enable.
compliance     active     Audit logs and compliance certificates. (custom, behavior: default)
team-internal  inactive   Team-specific credential stores. (custom, behavior: opt-in)
```

```go
type CustomGuard struct {
    Type        string   `yaml:"type,omitempty"`
    Description string   `yaml:"description,omitempty"`
    Paths       []string `yaml:"paths"`
    EnvOverride string   `yaml:"env_override,omitempty"`
    Allowed     []string `yaml:"allowed,omitempty"`
}

type GuardType struct {
    Behavior    string `yaml:"behavior"`
    Description string `yaml:"description"`
}
```

Added to Config:

```go
type Config struct {
    // existing fields...
    CustomGuards map[string]CustomGuard `yaml:"custom_guards,omitempty"`
    GuardTypes   map[string]GuardType   `yaml:"guard_types,omitempty"`
}
```

### Guard File Import

For sharing guards across projects:

```yaml
guard_files:
  - guards.yaml                    # relative to config dir (~/.config/aide/)
  - /shared/team-guards.yaml       # absolute path
```

Guard files contain `custom_guards` and `guard_types` definitions. Merge order:

1. Guard files are processed in list order (first file, then second, etc.)
2. Config.yaml definitions are applied last
3. On name collision (same `custom_guards` key or `guard_types` key), the later definition wins

This means config.yaml always wins over guard files, and later guard files win over earlier ones.

### Policy Struct

The `Policy` struct carries guard selection and runtime context:

```go
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

    // Process behavior (applied by sandbox.Apply, not by guards)
    AllowSubprocess bool
    CleanEnv        bool
}
```

The profile builder copies Policy fields into Context before rendering. Guards read from Context, never from Policy directly.

### DefaultPolicy

```go
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
```

### Profile Composition

The profile builder in `darwin.go` composes the profile from active guards:

```go
func generateSeatbeltProfile(policy Policy) (string, error) {
    homeDir, _ := os.UserHomeDir()

    activeGuards := modules.ResolveActiveGuards(policy.Guards)

    p := seatbelt.New(homeDir).
        WithContext(func(c *seatbelt.Context) {
            c.ProjectRoot = policy.ProjectRoot
            c.TempDir = policy.TempDir
            c.RuntimeDir = policy.RuntimeDir
            c.Env = policy.Env
            c.GOOS = runtime.GOOS
            // Wire Policy fields to Context for always-guards
            c.Network = policy.Network
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

Guards render in type order: `always` first, then `default`, then `opt-in`. Within each type, guards render in registry order.

### Guard Registry

```go
// pkg/seatbelt/modules/registry.go

func AllGuards() []Guard
func GuardByName(name string) (Guard, bool)
func GuardsByType(typ string) []Guard
func ExpandGuardName(name string) []string    // meta-guard expansion
func DefaultGuardNames() []string             // all "always" + all "default" names
func ResolveActiveGuards(names []string) []Guard  // look up and order guards by name
```

### CLI

```bash
aide sandbox guards                          # List all guards with type, status, paths
aide sandbox guard cloud-aws                 # Add to guards_extra for CWD context
aide sandbox guard cloud-aws --context work  # Target specific context
aide sandbox unguard browsers                # Add to unguard for CWD context
aide sandbox unguard browsers --context work # Target specific context
aide sandbox types                           # List all types
aide sandbox types show default              # Show guards in a type
aide sandbox types add compliance \
  --behavior default \
  --description "Audit logs and compliance certs"
aide sandbox types remove compliance         # Remove custom type only
```

`aide sandbox guards` output:

```
GUARD              TYPE        STATUS    PATHS
base               always      active    (deny default), (version 1)
system-runtime     always      active    /usr, /bin, /System/Library, ...
network            always      active    outbound
filesystem         always      active    ~/source/project (rw), ~ (ro)
keychain           always      active    ~/Library/Keychains, Security.framework
node-toolchain     always      active    ~/.nvm, ~/.npm, ~/.yarn, ...
nix-toolchain      always      active    /nix/store, ~/.nix-profile
git-integration    always      active    ~/.gitconfig, ~/.ssh/config
ssh-keys           default     active    ~/.ssh (deny), known_hosts/config (allow)
cloud-aws          default     active    ~/.aws/credentials, ~/.aws/config
cloud-gcp          default     active    ~/.config/gcloud
cloud-azure        default     active    ~/.azure
cloud-digitalocean default     active    ~/.config/doctl
cloud-oci          default     active    ~/.oci
kubernetes         default     active    ~/.kube/config
terraform          default     active    ~/.terraform.d/credentials.tfrc.json
vault              default     active    ~/.vault-token
browsers           default     active    Chrome, Firefox, Safari, ... (7 paths)
password-managers  default     active    1Password, Bitwarden, GPG keys (7 paths)
aide-secrets       default     active    ~/.config/aide/secrets
docker             opt-in      inactive  ~/.docker/config.json
github-cli         opt-in      inactive  ~/.config/gh
npm                opt-in      inactive  ~/.npmrc, ~/.yarnrc
netrc              opt-in      inactive  ~/.netrc
vercel             opt-in      inactive  ~/.config/vercel
```

`aide sandbox types` output:

```
TYPE        DEFAULT    DESCRIPTION
always      active     Agent needs this to function. Cannot be disabled.
default     active     Protects important data. On by default, can be disabled.
opt-in      inactive   Extra restriction. Off by default, user chooses to enable.
compliance  active     Audit logs and compliance certificates. (custom, behavior: default)
```

### Testing

| Test | Description |
|------|-------------|
| `TestGuard_Base` | base guard produces `(version 1)` and `(deny default)` |
| `TestGuard_SystemRuntime` | system-runtime guard produces OS binary, device, mach service rules |
| `TestGuard_Network_Outbound` | network guard emits `(allow network-outbound)` |
| `TestGuard_Network_PortFiltering` | network guard emits port allow/deny rules |
| `TestGuard_Filesystem_WritableReadable` | filesystem guard emits project rw, home ro rules |
| `TestGuard_Filesystem_UserDenied` | ExtraDenied paths emitted by filesystem guard |
| `TestGuard_Keychain_AllowRules` | keychain guard produces allow rules for Keychains, mach services, IPC |
| `TestGuard_NodeToolchain_Paths` | node-toolchain guard emits npm/yarn/nvm paths |
| `TestGuard_NixToolchain_Paths` | nix-toolchain guard emits nix store/profile paths |
| `TestGuard_GitIntegration_Paths` | git-integration guard emits gitconfig, ssh config paths |
| `TestGuard_SSHKeys_DenyAndAllow` | ssh-keys guard denies ~/.ssh, allows known_hosts/config |
| `TestGuard_CloudAWS_Default` | Default AWS paths denied |
| `TestGuard_CloudAWS_EnvOverride` | AWS_SHARED_CREDENTIALS_FILE replaces default credential path |
| `TestGuard_GCPEnvOverride` | CLOUDSDK_CONFIG replaces default gcloud path |
| `TestGuard_Browsers_Darwin` | macOS browser paths only on darwin |
| `TestGuard_Browsers_Linux` | Linux browser paths only on linux |
| `TestGuard_PasswordManagers_NoKeychains` | password-managers guard does NOT include ~/Library/Keychains |
| `TestGuard_MetaCloudExpands` | "cloud" expands to 5 individual guards |
| `TestGuard_UnguardRemoves` | Unguarding a default guard removes from active set |
| `TestGuard_UnguardAlways_Error` | Unguarding an always guard produces validation error |
| `TestGuard_UnguardInactiveNoop` | Unguarding an inactive guard is no-op |
| `TestGuard_GuardsExtraAdds` | guards_extra adds to default set |
| `TestGuard_GuardsOverridesDefault` | Explicit guards: replaces default, keeps always |
| `TestGuard_GuardsAndGuardsExtraWarns` | Both set → warning |
| `TestGuard_CustomGuard` | Custom guard from config resolves correctly |
| `TestGuard_CustomGuardAllowed` | Custom guard with allowed paths produces allow rules |
| `TestGuard_CustomGuardEnvOverride` | Custom guard env_override replaces default path |
| `TestGuard_CustomType` | Custom type with behavior field works |
| `TestGuard_CustomType_BuiltinImmutable` | Cannot remove built-in type |
| `TestGuard_DefaultPolicy` | DefaultPolicy returns always + default guard names |
| `TestProfile_NoKeychainConflict` | Full profile: keychain allow not overridden by any deny |
| `TestProfile_SSHAllowBeatsSubpathDeny` | literal allow on known_hosts beats subpath deny on .ssh |
| `TestProfile_NpmGuardOverridesToolchain` | When npm opt-in guard active, .npmrc is denied |
| `TestGuard_GuardFileImport` | Guard file with custom guards merged into config |
| `TestGuard_UnknownGuardName_Error` | Unknown guard name in guards: produces validation error |
| `TestGuard_CustomGuardNameCollision_Error` | Custom guard with built-in name produces validation error |
| `TestGuard_CustomGuardAlwaysType_Error` | Custom guard with type: always produces validation error |
| `TestGuard_CustomGuardUnknownType_Error` | Custom guard referencing nonexistent type produces validation error |
| `TestGuard_CustomGuardEnvOverrideMultiPath_Error` | Custom guard with env_override + multiple paths produces validation error |
| `TestGuard_AlwaysGuardDedup` | Listing always guard in guards: is silently deduplicated |
| `TestGuard_Kubernetes_ColonSeparatedKubeconfig` | KUBECONFIG with colon-separated paths denies each individually |
| `TestGuard_DeniedAndDeniedExtra` | Both denied: and denied_extra: warns, denied_extra: ignored |
| `TestGuard_GuardFileMergeOrder` | Later guard files override earlier ones, config.yaml wins |

### Implementation Structure

Each guard is a Go file in `pkg/seatbelt/modules/`:

| File | Guards |
|------|--------|
| `guard_base.go` | `base` |
| `guard_system_runtime.go` | `system-runtime` |
| `guard_network.go` | `network` |
| `guard_filesystem.go` | `filesystem` |
| `guard_keychain.go` | `keychain` |
| `guard_node_toolchain.go` | `node-toolchain` |
| `guard_nix_toolchain.go` | `nix-toolchain` |
| `guard_git_integration.go` | `git-integration` |
| `guard_ssh_keys.go` | `ssh-keys` |
| `guard_cloud.go` | `cloud-aws`, `cloud-gcp`, `cloud-azure`, `cloud-digitalocean`, `cloud-oci` |
| `guard_kubernetes.go` | `kubernetes` |
| `guard_terraform.go` | `terraform` |
| `guard_vault.go` | `vault` |
| `guard_browsers.go` | `browsers` |
| `guard_password_managers.go` | `password-managers` |
| `guard_aide_secrets.go` | `aide-secrets` |
| `guard_sensitive.go` | `docker`, `github-cli`, `npm`, `netrc`, `vercel` |
| `guard_custom.go` | Dynamic custom guard from config |
| `registry.go` | Guard registry |

### Files Changed

| File | Change |
|------|--------|
| `pkg/seatbelt/module.go` | Add `Guard` interface, extend `Context` with `Env`/`GOOS` |
| `pkg/seatbelt/modules/guard_*.go` | New: all guard modules (absorb existing module files) |
| `pkg/seatbelt/modules/registry.go` | New: guard registry |
| `internal/sandbox/sandbox.go` | Simplified Policy struct, simplified DefaultPolicy |
| `internal/sandbox/darwin.go` | Profile composition from guards |
| `internal/sandbox/policy.go` | PolicyFromConfig resolves guard selection |
| `internal/config/schema.go` | SandboxPolicy with guard fields, CustomGuard/GuardType structs |
| `cmd/aide/commands.go` | guard/unguard/types CLI subcommands |
| `internal/launcher/launcher.go` | Updated DefaultPolicy call |
| `internal/launcher/passthrough.go` | Updated DefaultPolicy call |
| **Removed** | `pkg/seatbelt/modules/base.go`, `system.go`, `network.go`, `filesystem.go`, `keychain.go`, `node.go`, `nix.go`, `git.go` (absorbed into guard files) |
