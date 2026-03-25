# Capabilities

You're debugging a failing deployment. You need kubectl, Docker, and AWS access.
Without capabilities, you'd configure sandbox guards individually — figure out
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
- **Guards** to disable (e.g., the `kubernetes` guard)
- **Environment variables** to pass through the sandbox boundary (e.g., `KUBECONFIG`)
- **Denies** to block specific paths within the capability's scope

The sandbox remains deny-default. Capabilities punch precise holes in it.

## Built-in Capabilities

aide ships with 12 built-in capabilities covering cloud providers, containers,
orchestration, infrastructure tools, SSH, and package registries.

### Cloud Providers

| Capability | What it unlocks | Paths | Key env vars |
|------------|----------------|-------|-------------|
| `aws` | AWS CLI credentials | `~/.aws/` | `AWS_PROFILE`, `AWS_REGION`, `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_SESSION_TOKEN` |
| `gcp` | Google Cloud CLI | `~/.config/gcloud/` | `CLOUDSDK_CONFIG`, `GOOGLE_APPLICATION_CREDENTIALS` |
| `azure` | Azure CLI | `~/.azure/` | `AZURE_CONFIG_DIR` |
| `digitalocean` | DigitalOcean CLI | `~/.config/doctl/` | `DIGITALOCEAN_ACCESS_TOKEN` |
| `oci` | Oracle Cloud CLI | `~/.oci/` | `OCI_CLI_CONFIG_FILE` |

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

Run `aide cap list` to see all available capabilities including any custom ones
you've defined. Run `aide cap show <name>` to inspect a specific capability's
full permissions.

## Activation

Capabilities can be activated for a single session or persisted in your context
config.

### Session-scoped: --with

```bash
aide --with k8s docker         # enable for this session
aide --with my-deploy          # works with custom capabilities too
```

`--with` does not modify your config. When the session ends, the capabilities
are gone.

### Context-scoped: config

```yaml
contexts:
  work:
    capabilities: [k8s-dev, docker]
```

Every time you run `aide` in a directory matching the `work` context, these
capabilities activate automatically.

### Excluding: --without

```bash
aide --without docker          # disable docker for this session
```

If your context config includes `docker` but you don't need it right now,
`--without` removes it for this session only. Config is never modified.

### How they combine

CLI `--with` appends to context capabilities. CLI `--without` removes from
context capabilities. The result is resolved once at session start and baked
into the immutable Seatbelt profile. The agent cannot escalate permissions
mid-session — this is a hard security constraint. To change capabilities, start
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

This produces a config entry like:

```yaml
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

aide cap enable k8s-dev        # persist in current context config
aide cap disable k8s-dev       # remove from current context config
```

## Protection

### never_allow: the hard ceiling

Some paths should never be accessible, regardless of which capabilities are
active. `never_allow` is a top-level config field that overrides everything.

```yaml
never_allow:
  - "~/.kube/prod-config"
  - "~/.aws/accounts/production"
  - "~/Documents/personal"
```

Implementation detail: `never_allow` paths are appended to the deny list after
all capability resolution. Since Seatbelt uses deny-wins semantics, these paths
are always blocked. All paths are symlink-resolved before being added to the
profile.

**Example: protecting production kubeconfig**

You use `k8s` regularly for development. Your `~/.kube/` directory contains both
dev and production configs. Without protection, `aide --with k8s` would make the
entire directory readable — including production credentials.

```yaml
never_allow:
  - "~/.kube/prod-config"

capabilities:
  k8s-dev:
    extends: k8s
    readable: ["~/.kube/dev-config", "~/.kube/staging-config"]
```

Even if someone later creates a broader capability that tries to read
`~/.kube/prod-config`, the `never_allow` rule blocks it.

### never_allow_env: blocking sensitive variables

```yaml
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

Some capabilities expose environment variables that contain credentials —
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
| `aide cap enable <name>` | Persist capability in current context config |
| `aide cap disable <name>` | Remove capability from current context config |

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
| `aide cap check <cap1> [cap2...]` | Preview composition — shows merged permissions before launching |
| `aide cap audit` | All active capabilities with full resolved permissions |
| `aide cap suggest-for-path <path>` | Map a path to the capability that would grant access (used by sandbox error interception) |

## Banner

When capabilities are active, the session banner shows what the agent can
access. The banner is capability-centric — it shows what's granted, not what's
blocked.

### Symbols

| Symbol | Meaning |
|--------|---------|
| `✓` (green) | Capability active |
| `✗` (red) | Path denied (never-allow) |
| `○` (dim) | Capability disabled this session via `--without` |
| `⚠` (yellow) | Warning — credentials exposed or composition risk |
| `⚡` (red bold) | Auto-approve mode active — always the last line |

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

   ⚡ AUTO-APPROVE — all agent actions execute without confirmation
```

The `← --with` and `← --without` annotations appear only for session-scoped
overrides, so you can tell at a glance what came from config versus CLI flags.
