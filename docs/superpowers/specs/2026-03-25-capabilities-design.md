# Capabilities: Task-Oriented Sandbox Permissions

**Goal:** Replace guard-level sandbox configuration with task-oriented "capabilities" that let users declare what they're doing, not which security rules to disable.

**Core principle:** Stop babysitting — enable full agent autonomy without worrying about safety. Every friction point is a reason someone disables the sandbox entirely.

---

## Problem

Users configure sandbox permissions by manipulating guards via YAML config or CLI. To debug an AWS + Kubernetes issue, a user must: check guards, unguard cloud-aws, unguard kubernetes, set KUBECONFIG, set AWS_PROFILE, verify policy, launch. Six steps, repeated for each tool combination. This causes fatigue — users disable the sandbox entirely, defeating its purpose.

## Solution

Capabilities are named permission bundles that map tasks to sandbox permissions. The user says `aide --with k8s aws`, not `unguard: [kubernetes, cloud-aws]`.

---

## Capability Definition

A capability is a recipe containing:

```yaml
capabilities:
  k8s:
    description: "Kubernetes cluster access"
    unguard: [kubernetes]
    readable: ["~/.kube"]
    env_allow: [KUBECONFIG]
    deny: []
```

### Fields

| Field | Type | Purpose |
|-------|------|---------|
| `description` | string | Human-readable purpose |
| `extends` | string | Single parent inheritance |
| `combines` | []string | Merge multiple capabilities |
| `unguard` | []string | Guards to disable |
| `readable` | []string | Paths to grant read access |
| `writable` | []string | Paths to grant write access |
| `deny` | []string | Paths to block within this capability |
| `env_allow` | []string | Environment variables to pass through to the agent |

### Inheritance

**`extends`** — single parent, child adds/overrides on top:

```yaml
capabilities:
  k8s-dev:
    extends: k8s
    readable: ["~/.kube/dev-config", "~/.kube/staging-config"]
    deny: ["~/.kube/prod-config"]
```

**`combines`** — merge multiple capabilities:

```yaml
capabilities:
  my-deploy:
    combines: [aws, k8s, docker]
    deny: ["~/.kube/prod-config"]
```

**Rules:**
- `extends` and `combines` are mutually exclusive on the same capability
- Max inheritance depth: 10 (catches accidental deep chains)
- Circular references are detected and rejected
- All referenced names must exist (built-in or user-defined)

---

## Built-in Capabilities (13)

### Cloud Providers

| Name | Unguards | Discovers | Env Allow |
|------|----------|-----------|-----------|
| `aws` | cloud-aws | `~/.aws/` configs, profiles | AWS_PROFILE, AWS_REGION, AWS_DEFAULT_REGION, AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY, AWS_SESSION_TOKEN, AWS_CONFIG_FILE, AWS_SHARED_CREDENTIALS_FILE |
| `gcp` | cloud-gcp | `~/.config/gcloud/`, service account JSONs | CLOUDSDK_CONFIG, GOOGLE_APPLICATION_CREDENTIALS, GCLOUD_PROJECT |
| `azure` | cloud-azure | `~/.azure/` | AZURE_CONFIG_DIR, AZURE_SUBSCRIPTION_ID |
| `digitalocean` | cloud-digitalocean | `~/.config/doctl/` | DIGITALOCEAN_ACCESS_TOKEN |
| `oci` | cloud-oci | `~/.oci/` | OCI_CLI_CONFIG_FILE |

### Containers & Registries

| Name | Unguards | Discovers | Env Allow |
|------|----------|-----------|-----------|
| `docker` | docker | `~/.docker/config.json` | DOCKER_CONFIG, DOCKER_HOST |

### Orchestration

| Name | Unguards | Discovers | Env Allow |
|------|----------|-----------|-----------|
| `k8s` | kubernetes | `~/.kube/` configs | KUBECONFIG |
| `helm` | kubernetes | `~/.config/helm/`, `~/.cache/helm/` | HELM_HOME, KUBECONFIG |

### Infrastructure as Code

| Name | Unguards | Discovers | Env Allow |
|------|----------|-----------|-----------|
| `terraform` | terraform | `~/.terraform.d/`, `.terraformrc` | TF_CLI_CONFIG_FILE, TF_VAR_* |
| `vault` | vault | `~/.vault-token` | VAULT_ADDR, VAULT_TOKEN, VAULT_TOKEN_FILE |

### SSH & Networking

| Name | Unguards | Discovers | Env Allow |
|------|----------|-----------|-----------|
| `ssh` | ssh-keys | `~/.ssh/` private keys | SSH_AUTH_SOCK |

### Package Registries

| Name | Unguards | Discovers | Env Allow |
|------|----------|-----------|-----------|
| `npm` | npm, netrc | `~/.npmrc`, `~/.yarnrc` | NPM_TOKEN, NODE_AUTH_TOKEN |

---

## Activation

### Session-scoped (not persisted)

```bash
aide --with k8s docker         # enable for this session only
aide --with my-deploy          # custom capability
aide --without docker          # exclude a context-scoped capability
aide                           # no capabilities, default safe sandbox
```

### Context-scoped (persisted in config)

```yaml
contexts:
  work:
    capabilities: [k8s-dev, docker]
```

### Combined

CLI `--with` appends to context capabilities. CLI `--without` removes from context capabilities. Both are session-only — config is never modified.

### No mid-session escalation

Capabilities are resolved at session start and baked into the immutable Seatbelt profile. To change capabilities, start a new session. The agent cannot self-escalate — this is a hard security constraint.

---

## Security Model

### Three layers

1. **Capability grants** — what the capability allows (readable, writable, env_allow)
2. **Per-capability denies** — paths blocked within a capability (e.g., allow k8s but deny ~/.kube/prod-config)
3. **Global never_allow** — hard ceiling that no capability can override

### never_allow

A top-level config field. Paths listed here are always denied, regardless of what any capability grants.

```yaml
never_allow:
  - "~/.kube/prod-config"
  - "~/.aws/accounts/production"
  - "~/Documents/personal"
```

Implementation: `never_allow` paths are appended to `ExtraDenied` after all capability and sandbox resolution. Since Seatbelt uses deny-wins semantics, these paths are always blocked. All paths are symlink-resolved before being added to the profile.

### env_allow — credential warning, not blocking

`env_allow` passes environment variables through the sandbox boundary. Some of these contain credentials (AWS_SECRET_ACCESS_KEY, VAULT_TOKEN). Aide cannot block these — the capability is useless without them. Instead, aide warns at session start:

```
⚠ credentials exposed: AWS_SECRET_ACCESS_KEY, AWS_SESSION_TOKEN
```

The warning lists exact variable names so the user makes an informed choice.

### Composition safety

When multiple capabilities are activated together, their grants are merged (union of allows, deny-wins on conflicts). Aide warns when the combination spans credential access + network egress:

```
⚠ 3 capabilities combine credential + network access
```

### Repo-specified capabilities

`.aide.yaml` in a repo can specify `capabilities:`. These are treated as **requests, not grants**. On first encounter (or change), aide prompts:

```
This repo requests capabilities: docker, aws, k8s
Accept? [y/n/review]
```

User must explicitly accept. A `never_allow_request` config field can globally block specific capability requests from repos.

### never_allow for env vars

```yaml
never_allow_env:
  - VAULT_ROOT_TOKEN
  - PRODUCTION_DB_PASSWORD
```

These environment variables are stripped even if a capability lists them in `env_allow`.

---

## Resolution Order

```
Base defaults (deny-default sandbox)
  + capability effects (unguard, readable, writable, deny, env_allow)
    + explicit sandbox config (writable_extra, denied_extra, guards_extra, unguard)
      + never_allow paths (appended to ExtraDenied, unconditional)
      + never_allow_env (stripped from env, unconditional)
```

Explicit sandbox config always wins over capabilities. `never_allow` overrides everything.

---

## CLI Commands

### Viewing

```bash
aide cap list                     # all available (built-in + custom)
aide cap show k8s                 # details: grants, denies, env_allow, resolved chain
aide cap show k8s-dev             # shows inheritance chain
aide status                       # current context + active capabilities + what they grant
```

### Creating (interactive — default)

```bash
aide cap create
```

Guided flow:
1. Name the capability
2. Optionally extend a built-in
3. Aide discovers files on disk (scans ~/.kube/, ~/.aws/, etc.) and presents choices
4. User picks what to allow and deny
5. Aide detects relevant env vars from current shell
6. User reviews summary and confirms

### Creating (expert — flags)

```bash
aide cap create k8s-dev \
  --extends k8s \
  --readable "~/.kube/dev-config" \
  --deny "~/.kube/prod-config" \
  --env-allow KUBECONFIG
```

### Modifying

```bash
aide cap edit k8s-dev --add-readable "~/.kube/staging-config"
aide cap edit k8s-dev --add-deny "~/.kube/prod-config"
aide cap edit k8s-dev --remove-deny "~/.kube/staging-config"
aide cap edit k8s-dev --add-env-allow KUBECONFIG_EXTRA
```

### Enabling/disabling in context

```bash
aide cap enable k8s-dev            # persist in current context config
aide cap disable k8s-dev           # remove from current context config
```

### Global denies

```bash
aide cap never-allow "~/.kube/prod-config"
aide cap never-allow --env VAULT_ROOT_TOKEN
aide cap never-allow --list
aide cap never-allow --remove "~/.kube/prod-config"
```

### Auditing

```bash
aide cap audit                     # all active capabilities with full resolved permissions
aide cap check aws k8s docker      # preview composition before launching
```

---

## Project Detection

On first run in a project (or when project files change), aide scans for tool markers:

| Marker | Suggests |
|--------|----------|
| `Dockerfile`, `docker-compose.yaml` | docker |
| `*.tf` files | terraform |
| `k8s/`, `manifests/`, YAML with `apiVersion` | k8s |
| AWS SDK in go.mod/requirements.txt/package.json | aws |
| `package.json` | npm |
| `.vault-token`, `vault.hcl` | vault |
| GCP SDK imports | gcp |

Detection **suggests, never auto-enables**:

```
Detected: Dockerfile, k8s manifests, AWS SDK
Suggested: aide --with docker k8s aws
```

User must explicitly accept. Choices are remembered per directory for subsequent sessions.

---

## Auto-approve Mode

### Flag

```bash
aide --auto-approve       # enable
aide --no-auto-approve    # override config, disable
```

### Config

```yaml
auto_approve: true
```

Flag overrides config. One flag, one config key, no ambiguity.

### Banner

Auto-approve is always the **last line** in the banner, rendered in red bold:

```
⚡ AUTO-APPROVE — all agent actions execute without confirmation
```

---

## Banner Design

The banner shifts from guard-centric to capability-centric. It shows what the agent CAN do, not what's being blocked.

### Symbols

| Symbol | Color | Meaning |
|--------|-------|---------|
| `✓` | green | Capability active |
| `✗` | red | Path denied (never-allow) |
| `○` | dim | Capability disabled this session (--without) |
| `⚠` | yellow | Warning (credentials exposed, composition risk) |
| `⚡` | red bold | Auto-approve mode — always last line |
| `← --with` | dim | Source: session-scoped |
| `← --without` | dim | Source: session exclusion |

### Examples

**Code-only (no capabilities):**

```
🔧 aide · work (claude)
   📁 github.com/acme/api
   🛡 sandbox: network outbound, code-only
```

**With capabilities:**

```
🔧 aide · infra (claude)
   📁 github.com/acme/infra
   🔐 secret: work
   📦 env: ANTHROPIC_API_KEY ← secrets.api_key
          AWS_PROFILE = staging
   🛡 sandbox: network outbound
      ✓ k8s-dev   ~/.kube/dev-config, ~/.kube/staging-config
      ✓ aws       ~/.aws/ (AWS_PROFILE=staging)
      ✓ docker    ~/.docker/config.json
      ✗ denied    ~/.kube/prod-config (never-allow)

      ⚠ credentials exposed: AWS_SECRET_ACCESS_KEY, AWS_SESSION_TOKEN

   run `aide status` for full details
```

**Mixed sources + auto-approve (`aide --with vault --without docker --auto-approve`):**

```
🔧 aide · infra (claude)
   📁 github.com/acme/infra
   🛡 sandbox: network outbound
      ✓ k8s-dev   ~/.kube/dev-config, ~/.kube/staging-config
      ✓ aws       ~/.aws/ (AWS_PROFILE=staging)
      ✓ vault     ~/.vault-token (VAULT_ADDR)            ← --with
      ○ docker    disabled for this session               ← --without
      ✗ denied    ~/.kube/prod-config (never-allow)

      ⚠ credentials exposed: AWS_SECRET_ACCESS_KEY, VAULT_TOKEN

   ⚡ AUTO-APPROVE — all agent actions execute without confirmation
```

---

## Sandbox Error Interception

When a sandbox permission error occurs during a session, a **Claude Code plugin hook** detects it and suggests the appropriate capability. This is implemented as a PostToolUse hook on the Bash tool, not as aide-level stderr monitoring.

**Why a plugin hook, not aide-level interception:**
- No stdout interleaving — suggestion appears within the agent's conversation flow
- Hook has structured access to tool output (no fragile regex on raw stderr)
- Works within the agent's existing UI, not as a terminal injection
- Can query aide's capability registry for the exact fix command

**Hook behavior:**
1. PostToolUse fires after a Bash command completes
2. If exit code is non-zero and output contains "Operation not permitted" or "permission denied"
3. Extract the denied path from the error message
4. Map the path to a capability (e.g., `~/.kube/` → `k8s`)
5. Inject suggestion into the agent's context:

```
Sandbox blocked access to ~/.kube/config.
This requires the `k8s` capability. Exit and restart with:
  aide --with k8s [current-capabilities]
```

This requires the aide Claude Code plugin to ship a PostToolUse hook and have access to the capability registry (via `aide cap suggest-for-path <path>` CLI command).

---

## Architecture

### New package: `internal/capability/`

| File | Contents |
|------|----------|
| `capability.go` | Capability, ResolvedCapability, CapabilitySet types; Resolve() for inheritance/combine chains |
| `builtin.go` | 13 built-in capability definitions and registry |
| `detect.go` | Project detection (scan for Dockerfiles, k8s manifests, etc.) |
| `discover.go` | Filesystem discovery for `aide cap create` (scan ~/.kube/, ~/.aws/, etc.) |

### Config schema additions

```go
// New fields on Config
Capabilities map[string]CapabilityDef `yaml:"capabilities,omitempty"`
NeverAllow     []string               `yaml:"never_allow,omitempty"`
NeverAllowEnv  []string               `yaml:"never_allow_env,omitempty"`

// New fields on Context
Capabilities []string `yaml:"capabilities,omitempty"`

// New type
type CapabilityDef struct {
    Extends     string   `yaml:"extends,omitempty"`
    Combines    []string `yaml:"combines,omitempty"`
    Description string   `yaml:"description,omitempty"`
    Readable    []string `yaml:"readable,omitempty"`
    Writable    []string `yaml:"writable,omitempty"`
    Deny        []string `yaml:"deny,omitempty"`
    EnvAllow    []string `yaml:"env_allow,omitempty"`
}
```

### Data flow: `aide --with k8s docker`

```
CLI parses --with ["k8s", "docker"]
  → config.Load()
  → context.Resolve() (may have context-scoped capabilities)
  → merge CLI --with + context capabilities (dedup)
  → capability.ResolveAll(names, builtins, userDefined)
       walks extends/combines chains
       returns CapabilitySet
  → CapabilitySet.ToSandboxOverrides()
       produces unguard/readable/writable/deny/env_allow lists
  → merge into config.SandboxPolicy
  → append never_allow to ExtraDenied
  → strip never_allow_env from env
  → sandbox.PolicyFromConfig() (existing pipeline, unchanged)
  → generate Seatbelt profile
  → launch agent
```

### Backwards compatibility

All new config fields use `omitempty`. Existing configs parse identically. The `SandboxPolicy` struct is unchanged — capabilities resolve to the same fields (`Unguard`, `ReadableExtra`, `WritableExtra`, `DeniedExtra`). No migration needed.

Guards and raw sandbox config continue to work. Capabilities are an abstraction layer on top, not a replacement of the underlying machinery.

---

## Threat Model Summary

The capability model is a **net security improvement** because it reduces the primary real-world threat: users disabling the sandbox due to configuration complexity.

### Key mitigations

1. **never_allow is the last filter** — applied after all resolution, with contract tests
2. **Credential warnings at launch** — user sees exactly which env vars are exposed
3. **Composition warnings** — when capabilities spanning credentials + network are combined
4. **Repo capabilities are requests** — never auto-granted, always prompt user
5. **No mid-session escalation** — agent cannot trigger permission upgrades
6. **Resolved permissions always visible** — banner shows what's granted, `aide status` shows full detail
7. **Symlink resolution on never_allow** — paths are resolved before profile generation

### Accepted risks

- Agent can socially engineer user to restart with broader capabilities (mitigated by clear banner showing what each capability grants)
- `env_allow` passes credential-bearing variables (mitigated by warnings, user's explicit choice)
- Capability composition creates permission unions (mitigated by composition warnings and `aide cap check`)

---

## UX Study Findings Incorporated

1. **`aide status` command** — shows current context, capabilities, and what they grant
2. **Sandbox error interception** — maps permission denials to capability suggestions
3. **`--without` flag** — temporarily disable a context-scoped capability
4. **Shell tab-completion** for capability names
5. **Per-directory memory** — "last used capabilities" for quick re-launch
6. **Repo capability trust model** — `.aide.yaml` capabilities require explicit user acceptance
7. **Auto-approve terminology** — single `--auto-approve` flag, plain English in banner
8. **`aide cap check`** — preview capability composition before launching
9. **`aide cap audit`** — review all active capabilities with resolved permissions
