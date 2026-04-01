# aide — Universal Coding Agent Context Manager

A Go CLI tool that automatically resolves and launches the right coding
agent (Claude, Gemini, Codex, etc.) with the correct context based on
project configuration. No manual switching — just run `aide` in any
project directory.

## Product Pitch

One command to launch the right AI coding agent with the right credentials,
everywhere.

Working across personal, work, and open-source projects with different AI
agents and API keys? aide figures out which agent, credentials, and MCP
servers to use based on where you are — automatically.

- **Zero config to start** — just run `aide`, it finds your agent and launches it
- **Automatic context switching** — different projects get different credentials,
  MCP servers, and settings without you thinking about it
- **Secrets stay encrypted** — API keys protected with age/YubiKey, never
  plaintext on disk
- **Git-track your setup** — one `git clone` reproduces your entire config on
  any machine, Docker, or CI
- **Any agent** — Claude, Gemini, Codex, or whatever comes next
- **Sandboxed by default** — agents run in a filesystem/network sandbox so you
  can let them work autonomously without approval fatigue

## Competitive Landscape

No existing tool combines automatic context resolution + encrypted secrets +
MCP management. The landscape as of March 2026:

| Tool | What It Does | Gap vs aide |
|------|-------------|-------------|
| [CC-Switch](https://github.com/farion1231/cc-switch) | GUI for switching AI agent providers | Manual switching, no git-based context resolution, no encrypted secrets |
| [CCS](https://github.com/kaitranntt/ccs) | Claude Code Switcher (work/personal isolation) | API proxy approach, no sops/age, no per-project config, no MCP |
| [Agent Config Adapter](https://github.com/PrashamTrivedi/agent-config-adapter) | Convert configs between agent formats | Config portability only, not runtime context switching |
| [Claude Squad](https://github.com/smtg-ai/claude-squad) | Parallel agent sessions with tmux + worktrees | Parallelism, not credential/context resolution |
| [Agent Deck](https://github.com/asheshgoplani/agent-deck) | Terminal session manager with MCP socket pooling | Session management, not per-project context switching |
| [add-mcp](https://github.com/neondatabase/add-mcp) (Neon) | Install MCP server across agents with one command | One-shot installer, not a runtime context manager |
| direnv | Per-directory env vars | Plaintext secrets in `.envrc`, no agent/MCP awareness |
| [agent-safehouse](https://github.com/eugene1g/agent-safehouse) | macOS sandbox-exec profiles for AI agents | Sandboxing only, no context resolution, no secrets, shell scripts not a reusable library |
| [Anthropic sandbox-runtime](https://github.com/anthropic-experimental/sandbox-runtime) | sandbox-exec (macOS) + bwrap (Linux) + network proxy | TypeScript/Node, internal tooling, not a standalone CLI for end users |
| [OpenShell](https://github.com/NVIDIA/OpenShell) (NVIDIA) | K3s-in-Docker sandbox with Landlock + Seccomp + OPA L7 proxy + credential placeholder injection | Infrastructure-heavy (requires Docker + K3s), Linux only, no automatic context resolution, no git-trackable config, no encrypted secrets management, no macOS support |

**aide's unique combination:**
1. Automatic context resolution from git remote + directory patterns
2. Encrypted secrets with sops/age (YubiKey support)
3. Unified context = agent + credentials + MCP servers + env
4. Transparent wrapper (replaces typing `claude` or `gemini`)
5. Config-as-code — git-trackable, reproducible across machines/Docker/CI
6. Sandboxed agent execution — pre-defined security boundary eliminates approval fatigue

OpenShell is server-side sandbox infrastructure for teams running agents
in controlled environments. aide is a client-side launcher for developers
on their laptops. OpenShell provides L7 network inspection and a
credential-never-touch-agent model. aide provides zero-overhead OS-native
sandboxing, automatic context switching, and portable git-trackable
config with encrypted secrets.

## Problem

When using multiple coding agents across personal and work projects,
you end up managing:

- Different API keys / credentials (personal Anthropic key vs work Bedrock)
- Different MCP server configurations per project
- Different system prompts and permissions
- Risk of using the wrong license or credentials for a project
- Different aggregator setups (1mcp, etc.) per environment

Currently this requires manual switching or remembering which agent
and credentials to use for each project. Even with a single agent,
switching between personal and work credentials is error-prone.

## Solution

`aide` resolves context automatically based on git remote URL and
directory path patterns. Three pillars:

**Pillar 1: Sandbox** — Define the security boundary upfront; the agent
runs freely within it. OS-native sandboxing is on by default. No
per-action permission prompts. Launch and walk away.

**Pillar 2: Secrets & Portability** — API keys encrypted with age/YubiKey,
decrypted in-process, never plaintext on disk. One config directory,
git-trackable. One `git clone` reproduces your setup on any machine.

**Pillar 3: Context Resolution & Any Agent** — Automatic switching between
work, personal, and open-source contexts based on git remote and directory.
Works with Claude, Gemini, Codex, or whatever comes next.

Design principles:

- **Agents are just binaries.** All env vars, secrets, and MCP selection
  live on the context, not the agent definition.
- **Zero-config by default.** If one known agent binary is on PATH with
  its API key already in the environment, aide just exec's it.
- **Secrets never touch disk in plaintext.** Decrypted in-process using
  sops as a Go library, passed as env vars, ephemeral runtime files
  cleaned on exit.
- **One directory to track.** Everything lives under `$XDG_CONFIG_HOME/aide/`
  so you can `git init` the whole thing.

## Pillar 1: Sandbox

The sandbox exists to enable full agent autonomy, not to restrict
agents. The question is not "what should we block?" but "what does
the agent need to run unsupervised?" Anthropic's research found
sandboxing reduced permission prompts by 84%. aide uses OS-native
sandboxing -- on by default, users opt out not in.

### Capabilities

Capabilities are task-oriented permission bundles. Users declare
what they are doing, not which security rules to adjust.

```bash
aide --with k8s docker aws
```

Three words. The sandbox opens exactly the right paths, passes
exactly the right environment variables, and denies everything
else. See [docs/capabilities.md](docs/capabilities.md) for the
full reference.

aide ships with 19 built-in capabilities:

| Category | Capabilities |
|----------|-------------|
| Cloud providers | `aws`, `gcp`, `azure`, `digitalocean`, `oci` |
| Containers & orchestration | `docker`, `k8s`, `helm` |
| Infrastructure & access | `terraform`, `vault`, `ssh`, `npm` |
| Language runtimes | `go`, `rust`, `python`, `ruby`, `java` |
| Developer tools | `github`, `gpg` |

Each capability bundles readable/writable paths, environment
variables to pass through the sandbox boundary, and per-capability
denies.

**Custom capabilities.** Define your own with `extends` (single
parent inheritance) or `combines` (merge multiple capabilities).
`extends` and `combines` are mutually exclusive. Maximum
inheritance depth is 10; circular references are rejected.

**Activation scopes.** Session-scoped (`--with`/`--without`) for
one-off overrides. Context-scoped (config `capabilities:` list)
for persistent defaults. CLI appends to or removes from context
capabilities; the result is resolved once at session start.

**Hard ceilings.** `never_allow` blocks paths regardless of which
capabilities are active. `never_allow_env` strips environment
variables even if a capability lists them. Both override everything
because Seatbelt uses deny-wins semantics.

**No mid-session escalation.** Permissions are baked into an
immutable Seatbelt profile at launch. The agent cannot escalate
mid-session. To change capabilities, start a new session.

**Project detection.** aide scans for tool markers (`go.mod`,
`Dockerfile`, `*.tf`) and suggests capabilities. It never
auto-enables them.

**Resolution order.** Base defaults + capability effects + explicit
sandbox config + `never_allow`. Three layers of security:
capability grants, per-capability denies, global `never_allow`.

### Guards

Guards are modular seatbelt components that produce the complete
set of rules -- both deny and allow -- for the resources they
manage.

**Guard interface.** Each guard implements the Module interface
(`Name()`, `Rules()`) plus Guard metadata (`Type()`,
`Description()`).

**Guard types:**

| Type | Default state | Meaning |
|------|--------------|---------|
| always | active | Agent needs this to function. Cannot be disabled. |
| default | active | Protects important data. On by default, can be disabled. |
| opt-in | inactive | Extra restriction. Off by default, user enables. |

**Built-in guards.** aide ships with 10 guards across two tiers:

- **Always guards (7):** `base`, `system-runtime`, `network`,
  `filesystem`, `keychain`, `node-toolchain`, `nix-toolchain`.
  These form the baseline policy and cannot be disabled.

- **Default guards (3):** `project-secrets`, `dev-credentials`,
  `aide-secrets`. Active out of the box, can be disabled with
  `unguard`.

**Guard resolution:**

- No guard config: always + default guards activate.
- `guards:` set explicitly: always + listed guards only.
- `guards_extra:` extends the default set with additional guards.
- `unguard:` removes guards from the active set. Cannot unguard
  always guards.

**Agent modules are not guards.** Agent-specific modules (e.g.,
writable paths for Claude's config directory) are selected by the
launcher based on which agent runs. They are not part of the guard
system.

See [docs/sandbox.md](docs/sandbox.md) for the full guard
inventory and CLI commands.

### Seatbelt Library (`pkg/seatbelt/`)

A composable Go library that generates working macOS Seatbelt
profiles. Ported from
[agent-safehouse](https://github.com/eugene1g/agent-safehouse).

```go
p := seatbelt.New(homeDir).
    WithContext(func(c *seatbelt.Context) {
        c.ProjectRoot = projectRoot
        c.GOOS = runtime.GOOS
        c.Network = "outbound"
    })

for _, g := range guards.ResolveActiveGuards(guardNames) {
    p.Use(g)
}

profile, err := p.Render()
```

Key design decisions:

- **(deny default) baseline.** The profile starts with
  `(deny default)` and adds ~120 granular allow rules for system
  runtime. This is the only approach that works reliably --
  `(allow default)` with targeted denies hangs in practice.

- **Module interface.** Each module contributes rules to the
  profile. Guards, agent modules, and capability modules all
  implement the same interface.

- **Context struct.** Carries runtime information (project root,
  home directory, OS, network mode) to all modules during
  rendering.

- **Deferred deny rule ordering.** Filesystem deny rules render
  after all other modules so they can override any allow rule
  from any source.

- **Public library.** `pkg/seatbelt` is importable by other Go
  projects. No dependency on aide's CLI or config system.

### Platform Support

| Platform | Mechanism | Status |
|----------|-----------|--------|
| macOS | `sandbox-exec` (Seatbelt profiles) | Implemented |
| Linux | Landlock (kernel 5.13+) | Planned |
| Linux fallback | bubblewrap (`bwrap`) | Planned |

### Known Constraints

1. **Deny-wins-over-allow.** macOS Seatbelt: a deny rule always
   wins over an allow rule at the same specificity. Cannot use
   deny-broad + allow-narrow. Must use allow-broad + deny-narrow.
   All guard design follows this.

2. **`(allow default)` paradox.** Despite theoretical appeal,
   `(allow default)` with targeted denies hangs Claude Code's
   `-p` flag and TUI mode. `(deny default)` with ~120 granular
   allow rules works reliably.

3. **MCP sandbox escape.** MCP servers (e.g., desktop-commander)
   run as separate OS processes outside the sandbox. Commands
   spawned through them bypass all Seatbelt restrictions. Needs
   architectural fix.

4. **sandbox-exec deprecation.** Apple deprecated `sandbox-exec`
   but it remains functional. Used by Anthropic's sandbox-runtime
   and agent-safehouse.

5. **Discovery phase** (planned). Some guards need filesystem
   scanning before rule generation (e.g., SSH keys with
   non-standard filenames).

## Pillar 2: Secrets & Portability

API keys encrypted with age/YubiKey, decrypted in-process, never
plaintext on disk. Ephemeral runtime files cleaned on exit. One config
directory, git-trackable — one git clone reproduces your setup on any
machine.

### Encrypted Secrets

Secrets are stored as sops-encrypted YAML files in `$XDG_CONFIG_HOME/aide/secrets/`,
decrypted in-process at launch time using the sops Go library.

#### Secrets File Format

```yaml
# $XDG_CONFIG_HOME/aide/secrets/personal.enc.yaml (before encryption)
anthropic_api_key: sk-ant-...
openai_api_key: sk-...
context7_token: ctx7-...
```

The `secret` field in context config is a filename resolved relative
to `$XDG_CONFIG_HOME/aide/secrets/`. Absolute paths are also supported.

#### Secrets Lifecycle

aide manages the full secrets lifecycle — no need to use `sops` CLI directly:

```bash
aide secrets create personal          # Create new encrypted secrets file
aide secrets edit work                # Decrypt -> $EDITOR -> re-encrypt
aide secrets list                     # List available secrets files
aide secrets rotate work --add-key $(age-keygen -y key.txt)   # Add recipient
aide secrets rotate work --remove-key <age-pubkey>            # Remove recipient
```

**Create flow:**
1. Detect age public key (from YubiKey, env var, or key file)
2. Open `$EDITOR` with a YAML template
3. Encrypt with sops and write to `$XDG_CONFIG_HOME/aide/secrets/<name>.enc.yaml`
4. Plaintext never written to persistent disk (uses tmpfs temp file)

**Edit flow:**
1. Decrypt in-process to a secure temp file (in `$XDG_RUNTIME_DIR`)
2. Open `$EDITOR`
3. Re-encrypt on save, remove temp file
4. Equivalent to `sops edit` but with aide's path resolution

**Rotate flow:**
Uses sops library to update the recipient list (age public keys) on an
existing encrypted file without exposing plaintext.

### Ephemeral Runtime

ALL decrypted material must die with the process. Nothing persists.

**What lives where:**

| Material | Location | Lifetime |
|---|---|---|
| Decrypted secrets | Process memory only | Dies with process |
| Env vars for child | Passed to exec'd agent | Dies with agent process |
| Generated MCP/aggregator configs | `$XDG_RUNTIME_DIR/aide-<pid>/` (tmpfs, mode 0700) | Cleaned on exit |
| Config, encrypted secrets | `$XDG_CONFIG_HOME/aide/` | Persistent, safe on disk |

**Cleanup guarantees:**

- Signal handlers registered for SIGTERM, SIGINT, SIGQUIT, SIGHUP — all trigger
  cleanup of the runtime directory before exit.
- `defer` cleanup in the main launch path for normal exit.
- SIGKILL edge case: tmpfs (`$XDG_RUNTIME_DIR`) cleans on reboot. Next aide
  launch detects and cleans stale `aide-*` dirs.
- Decrypted secrets are NEVER written to `$XDG_CONFIG_HOME`.

#### Launch Flow

1. Read `config.yaml` (no secrets in this file, safe on disk)
2. Resolve context (git remote + path matching)
3. Decrypt secrets in memory (sops library call returns Go map)
4. Create `$XDG_RUNTIME_DIR/aide-<pid>/` (tmpfs, mode 0700)
5. Generate MCP/aggregator config with resolved secrets into temp dir
6. Build env vars (resolve templates against secrets map) — in memory only
7. Apply sandbox policy (generate platform-specific policy from context config)
8. Exec agent inside sandbox with env vars + MCP config path pointing to temp dir
9. On exit (normal or signal): `rm -rf` temp dir

### Age Key Discovery

Tried in order:

1. **YubiKey** (via `age-plugin-yubikey`) — hardware-bound, key never on disk
2. **`$SOPS_AGE_KEY` env var** — for CI/Docker (key in memory, not on disk)
3. **`$SOPS_AGE_KEY_FILE`** — custom key file location
4. **`$XDG_CONFIG_HOME/sops/age/keys.txt`** — default age key location

### Access Control

Access is determined by age key possession:

- Only holders of a listed age private key (or YubiKey) can decrypt
- Multiple recipients per secrets file (e.g., laptop + desktop + CI)
- Rotation adds/removes recipients without re-entering secrets

### Reproducibility

#### Pattern 1: Personal Git-Tracked Config

Track your entire aide setup in version control:

```bash
cd ~/.config/aide && git init && git add -A && git commit -m "aide config"
```

Encrypted secrets are safe to commit — only age key holders can decrypt.

#### Pattern 2: Team Shared Config

Share config across a team, with per-person age keys:

```bash
git clone git@github.com:team/aide-config.git ~/.config/aide
aide secrets rotate work --add-key $(age-keygen -y key.txt)
```

#### Pattern 3: Docker / CI

```dockerfile
COPY aide-config/ /root/.config/aide/
ENV SOPS_AGE_KEY=AGE-SECRET-KEY-1...
```

The `SOPS_AGE_KEY` env var provides the decryption key in memory without
writing a key file to the image.

## Pillar 3: Context Resolution & Any Agent

aide works with any coding agent — Claude, Gemini, Codex, or whatever
comes next. Agents are just binaries. aide resolves which agent,
credentials, and sandbox policy to use based on where you are.

### Zero-Config Passthrough

When no config file exists, aide does not require setup:

1. Scan PATH for known agent binaries (`claude`, `gemini`, `codex`)
2. If exactly one found and it already has its API key in the environment,
   just exec it (pure passthrough — aide adds zero overhead)
3. If multiple found, show a helpful message:
   `"Multiple agents found: claude, codex. Use --agent to pick one, or run aide setup."`
4. If none found: `"No known agent binaries found on PATH. Install claude, gemini, or codex."`

aide adds value only when you need multi-context management. For single-agent,
single-context users with env vars already set, aide is invisible.

### Context Matching

1. Check for `.aide.yaml` in current directory (walk up to git root)
2. Detect git remote URL: `git remote get-url origin`
3. Match against `contexts[].match` rules (most specific wins)
4. Fall back to `default_context`
5. Merge: global defaults < context config < project override

### Match Specificity

- Exact path match > glob path match > remote match
- Longer patterns are more specific (path > remote, longer glob > shorter)
- Project `.aide.yaml` always wins

### Edge Cases

- **No git remote:** Gracefully fall back to path matching only. Not an error.
- **Multiple matches:** Most specific wins per the rules above.
- **Multiple git remotes:** Check `origin` by default. Configurable via
  `match.remote_name` if needed.
- **`aide which`:** Shows what matched AND what else could have matched, so
  users can debug context resolution without guessing.

## Config Layout

Everything lives under `$XDG_CONFIG_HOME/aide/` (defaults to `~/.config/aide/`).
No XDG_DATA_HOME split — this keeps config and secrets together so the entire
aide setup can be git-tracked as a single repository.

```
$XDG_CONFIG_HOME/aide/
  config.yaml                  # Global config (agents, contexts, MCP, defaults)
  secrets/
    personal.enc.yaml          # Encrypted secrets for personal context
    work.enc.yaml              # Encrypted secrets for work context
```

Per-project overrides are optional:

```
.aide.yaml                     # In project root, overrides context settings
```

This enables full reproducibility:

```bash
cd ~/.config/aide && git init && git add -A && git commit -m "initial aide config"
```

## Config Format

### Minimal Config (Single Context)

Single-context users do not need the full agents/contexts structure. When aide
detects a flat config (no `agents` or `contexts` keys), it treats the file as
a single default context.

Agent name is enough — aide assumes binary name matches agent name unless
overridden.

```yaml
# $XDG_CONFIG_HOME/aide/config.yaml — minimal
agent: claude
env:
  ANTHROPIC_API_KEY: "{{ .secrets.anthropic_api_key }}"
secret: personal
mcp_servers: [git, context7]
```

### Full Config (Multi-Context)

```yaml
# $XDG_CONFIG_HOME/aide/config.yaml

# Agent definitions — just binary mappings. No env or secrets here.
agents:
  claude:
    binary: claude
  gemini:
    binary: gemini
  codex:
    binary: codex

# MCP server definitions (top-level, shared across contexts)
mcp:
  aggregator:
    command: 1mcp
    # or: url: http://localhost:3000
  servers:
    git:
      command: git-mcp
    context7:
      command: context7-mcp
      env:
        CONTEXT7_TOKEN: "{{ .secrets.context7_token }}"
    serena:
      command: serena-mcp
      args: ["--project", "{{ .project_root }}"]
      env:
        SERENA_LICENSE: "{{ .secrets.serena_license }}"
    things:
      command: things-mcp

# Context definitions — matched by git remote or directory
contexts:
  work:
    match:
      - remote: "github.com/work-org/*"
      - path: "~/work/*"
    agent: claude
    secret: work
    env:
      CLAUDE_CODE_USE_BEDROCK: "1"
      AWS_PROFILE: "{{ .secrets.aws_profile }}"
    mcp_servers: [git, context7, serena]
    capabilities: [k8s-dev, docker]
    sandbox:
      guards_extra: [company-tokens]
      denied_extra: ["~/sensitive"]
      network:
        mode: outbound
    mcp_server_overrides:
      serena:
        args: ["--project", "{{ .project_root }}", "--mode", "strict"]

  personal:
    match:
      - remote: "github.com/jskswamy/*"
      - path: "~/source/github.com/jskswamy/*"
    agent: claude
    secret: personal
    env:
      ANTHROPIC_API_KEY: "{{ .secrets.anthropic_api_key }}"
    mcp_servers: [git, context7, serena, things]

  oss:
    match:
      - remote: "github.com/*"     # catch-all for other GitHub repos
    agent: claude
    secret: personal
    env:
      ANTHROPIC_API_KEY: "{{ .secrets.anthropic_api_key }}"

# Default context when no match
default_context: personal

# Global protection
never_allow:
  - "~/.kube/prod-config"

# Custom capabilities
capabilities:
  k8s-dev:
    extends: k8s
    readable: ["~/.kube/dev-config"]
    deny: ["~/.kube/prod-config"]
```

### Why Env Lives on Context, Not Agent

Agents are just binary definitions (name to binary path). All env vars,
secret, and MCP server selection live on the context. This avoids
confusion where agent-level env templates would be misleading — for example,
a work context uses Bedrock (CLAUDE_CODE_USE_BEDROCK), not ANTHROPIC_API_KEY,
even though both use the same `claude` binary.

### Optional Secrets (Env Passthrough)

`secret` is optional. If a user already has `ANTHROPIC_API_KEY` in their
shell environment (via `.envrc`, direnv, etc.), aide works without sops. Env
values without `{{ }}` template syntax pass through as literals. This supports
zero-config and gradual adoption.

```yaml
# No secret needed — CLAUDE_CODE_USE_BEDROCK is a literal
contexts:
  work:
    agent: claude
    env:
      CLAUDE_CODE_USE_BEDROCK: "1"
```

### Per-Project Override (`.aide.yaml`)

```yaml
# .aide.yaml (in project root)
agent: gemini
mcp_servers:
  - git
  - custom-server
env:
  CUSTOM_VAR: "value"
```

## MCP System

MCP is a top-level config section with server definitions shared across contexts.
Contexts select which servers to activate.

### Server Definitions

Servers support secrets via template syntax, just like context env vars:

```yaml
mcp:
  servers:
    git:
      command: git-mcp
    context7:
      command: context7-mcp
      env:
        CONTEXT7_TOKEN: "{{ .secrets.context7_token }}"
    serena:
      command: serena-mcp
      args: ["--project", "{{ .project_root }}"]
      env:
        SERENA_LICENSE: "{{ .secrets.serena_license }}"
    things:
      command: things-mcp
```

### Aggregator Support

If an aggregator is configured (e.g., 1mcp), aide generates aggregator config
and points the agent at it. If no aggregator, aide generates native per-agent
MCP config (e.g., `.mcp.json` for Claude).

```yaml
mcp:
  aggregator:
    command: 1mcp
    # or: url: http://localhost:3000
  servers: ...
```

### Context-Level MCP Selection

Contexts select which servers to activate and can override server settings:

```yaml
contexts:
  personal:
    mcp_servers: [git, context7, serena, things]
  work:
    mcp_servers: [git, context7, serena]
    mcp_server_overrides:
      serena:
        args: ["--project", "{{ .project_root }}", "--mode", "strict"]
```

## Trust Gate

`.aide.yaml` project overrides are untrusted by default. aide uses
content-addressed hashing (identical to direnv) to gate project config.

### Trust Check on Launch

1. Compute `SHA-256(absolute_path + "\n" + file_contents)` → fileHash
2. Compute `SHA-256(absolute_path + "\n")` → pathHash
3. If deny file exists for pathHash → silently skip `.aide.yaml`
4. If trust file exists for fileHash → apply `.aide.yaml`
5. Otherwise → show contents, do not apply, suggest `aide trust`

### Three States

| State | Behavior |
|-------|----------|
| Trusted | Config applies normally |
| Untrusted | Config shown but not applied; warning printed |
| Denied | Config silently skipped |

### Key Design Choices

- **Deny is path-based, not content-based.** Prevents cycling `.aide.yaml`
  contents to escape a deny.
- **Auto-re-trust** when aide itself modifies `.aide.yaml` (via
  `aide cap enable`, etc.), guarded by pre-modification hash check.
- **Trust prefix** (`aide trust --path ~/source`) for bulk approval of
  repos you control.
- **Storage:** `$XDG_DATA_HOME/aide/trust/` and
  `$XDG_DATA_HOME/aide/deny/`

### CLI

```bash
aide trust                    # trust .aide.yaml in current directory
aide deny                     # permanently block it
aide untrust                  # remove trust without blocking
aide --ignore-project-config  # launch without applying .aide.yaml
```

## Error Messages

aide provides detailed, actionable error messages:

- **Decryption failure:**
  `"Failed to decrypt secrets/work.enc.yaml: age identity not found. Is your YubiKey plugged in? Check aide setup for key configuration."`

- **Template resolution failure:**
  `"Template error in contexts.work.env.AWS_PROFILE: key 'aws_profile' not found in secrets/work.enc.yaml. Available keys: anthropic_api_key, aws_region"`

- **Context resolution conflict:**
  `"Multiple contexts matched: work (via remote github.com/work-org/repo), personal (via path ~/work/repo). Most specific wins: work. Use --context to override."`

- **No agent found:**
  `"No known agent binaries found on PATH. Install one of: claude, gemini, codex. Or set agents.<name>.binary in config."`

## CLI Interface

```bash
aide                    # Auto-resolve context and launch agent
aide --agent claude     # Override agent selection
aide --context work     # Override context
aide -v                 # Verbose: show resolution steps before launching
aide --clean-env        # Launch agent with clean environment (only aide-injected vars)
aide which              # Show resolved context, match reasoning, and alternatives
aide validate           # Validate config without launching
aide init               # Create .aide.yaml in current project
aide setup              # Interactive wizard (age key generation, first config)
aide contexts           # List all configured contexts
aide agents             # List all configured agents
aide completion bash    # Generate shell completions

# Secrets management
aide secrets create <name>             # Create new encrypted secrets file
aide secrets edit <name>               # Edit existing secrets file
aide secrets list                      # List available secrets files
aide secrets rotate <name> [flags]     # Add/remove age key recipients

# Args forwarded to agent — everything aide doesn't recognize passes through
aide --context work --model opus -p "fix the bug"
# → resolves work context, launches: claude --model opus -p "fix the bug"

# Capabilities
aide cap list                    # All available capabilities
aide cap show <name>             # Inspect a capability
aide cap create <name> [flags]   # Create custom capability
aide cap enable <name>           # Persist in current context
aide cap disable <name>          # Remove from current context
aide cap check <caps>            # Preview composition
aide cap audit                   # Full resolved permissions
aide cap never-allow <path>      # Add global deny

# Sandbox
aide sandbox guards              # List all guards with status
aide sandbox guard <name>        # Enable a guard
aide sandbox unguard <name>      # Disable a guard
aide sandbox types               # List guard types
aide sandbox test                # Preview generated profile
aide sandbox show                # Print effective policy
aide sandbox deny <path>         # Add path deny
aide sandbox network <mode>      # Set network mode
aide sandbox reset               # Revert to defaults

# Trust
aide trust                       # Trust .aide.yaml
aide deny                        # Block .aide.yaml
aide untrust                     # Remove trust

# Status
aide status                      # Current context + capabilities + sandbox
aide --with k8s,docker           # Session-scoped capabilities
aide --without docker            # Exclude capability for session
aide --yolo                      # Auto-approve agent actions
```

### Setup Wizard

`aide setup` handles first-time configuration:

1. Detect if age key exists (YubiKey, key file, env var)
2. Offer to generate an age key or configure YubiKey
3. "Skip" path for users who do not need secrets management
4. Create a minimal config.yaml based on detected agents on PATH

## Architecture

```
cmd/
  aide/
    main.go               # Entry point, cobra root command
internal/
  capability/
    capability.go         # Capability types, resolution, inheritance
    builtin.go            # 19 built-in capability definitions
    detect.go             # Project marker detection
    config.go             # Capability config management
  config/
    config.go             # Config loading, merging, normalization
    schema.go             # Config types and validation
    paths.go              # XDG path resolution (via adrg/xdg)
    template.go           # text/template resolution for env vars
  context/
    resolver.go           # Context matching engine
    git.go                # Git remote detection + project root
  secrets/
    sops.go               # Sops decryption (library-based)
    manager.go            # Secrets lifecycle (create, edit, rotate)
    age.go                # Age key discovery
  launcher/
    launcher.go           # Full launch flow (config-based)
    passthrough.go        # Zero-config passthrough flow
    runtime.go            # Ephemeral runtime dir management
  sandbox/
    sandbox.go            # Sandbox interface and default policy
    darwin.go             # macOS Seatbelt profile generation
    linux.go              # Linux Landlock + bwrap (planned)
    policy.go             # Policy resolution from config
  trust/
    trust.go              # Content-addressed trust store
  ui/
    banner.go             # Startup banner rendering
    formatter.go          # Output formatting
pkg/seatbelt/
    profile.go            # Profile builder (composable modules)
    module.go             # Module/Guard interfaces
    context.go            # Runtime context for modules
    rule.go               # Seatbelt rule types
  guards/
    registry.go           # Guard registry
    guard_base.go         # (deny default), (version 1)
    guard_system_runtime.go  # ~120 OS rules
    guard_network.go      # Network modes + port filtering
    guard_filesystem.go   # Project rw, home ro, user denies
    guard_keychain.go     # macOS Keychain access
    guard_node_toolchain.go  # npm/yarn/pnpm paths
    guard_nix_toolchain.go   # Nix store paths
    guard_git_integration.go # Git config, SSH config
    guard_project_secrets.go # .env, .git/hooks protection
    guard_dev_credentials.go # Dev credential files
    guard_aide_secrets.go    # aide secrets directory
  agents/
    claude.go             # Claude Code config paths
    codex.go              # Codex config paths
```

## Dependencies

- `filippo.io/age` for age encryption/decryption primitives
- `github.com/adrg/xdg` for XDG base directory resolution
- `github.com/fatih/color` for colored terminal output
- `github.com/getsops/sops/v3` for in-process secret decryption
- `github.com/go-git/go-git/v5` for pure-Go git operations (remote detection, project root)
- `github.com/gobwas/glob` for glob matching
- `github.com/landlock-lsm/go-landlock` for Linux Landlock sandboxing (native Go, no CGo)
- `github.com/spf13/cobra` for CLI framework
- `gopkg.in/yaml.v3` for YAML parsing
- Shells out to `sandbox-exec` on macOS for Seatbelt sandboxing
- Shells out to `bwrap` on Linux as Landlock fallback (optional)

## Design Decisions

Decisions made during UX study (March 2026), with rationale preserved for
future reference.

### Sandbox Decisions

### DD-14: Sandboxing — OS-Native, On by Default
**Decision:** Sandbox agents using OS-native mechanisms (sandbox-exec on macOS,
Landlock on Linux, bwrap as fallback). Sandboxing is ON by default with sensible
defaults. Users customize or opt out, not opt in.
**Why:** Approval fatigue is the #1 barrier to agentic development. Users pressing
"yes" repeatedly will eventually approve something dangerous. Pre-defining the
security boundary in config eliminates per-action prompts entirely. Anthropic's
research showed 84% reduction in permission prompts with sandboxing.
**Alternatives considered:** Docker (too heavy, requires daemon), seccomp-only
(syscall-level, too low for our needs), no sandboxing (defeats the purpose of
autonomous agents).
**Platform strategy:** Landlock is preferred on Linux (native Go library, no
external binary, self-sandboxing). sandbox-exec on macOS (deprecated but
functional, used by Anthropic and agent-safehouse). bwrap as Linux fallback
for older kernels without Landlock support.

### DD-15: Default Sandbox Policy — Guards-Based
**Decision:** Default policy activates 7 always guards (base, system-runtime,
network, filesystem, keychain, node-toolchain, nix-toolchain) plus 3 default
guards (project-secrets, dev-credentials, aide-secrets). Network defaults to
outbound. Subprocesses allowed.
**Why:** The original DD-15 described path-based defaults (writable/readable/denied
lists). The guard system replaced this with composable modules that each own
their resource's security. Guards are easier to reason about, test independently,
and extend.
**Replaces:** Original DD-15 which listed explicit path defaults.

### DD-22: Agent Config Dir Resolver
**Decision:** Each agent has a config dir resolver — a function that reads agent-specific env vars (`CLAUDE_CONFIG_DIR`, `CODEX_HOME`, etc.) and adds the appropriate directories to the sandbox writable list. New agents only need a resolver function and a registry entry.
**Why:** Hardcoding agent config paths in the sandbox policy doesn't scale as new agents are added and breaks when users customize agent config locations via env vars. The resolver makes the sandbox self-configuring based on the actual runtime environment.

### DD-23: Path Validation at Launch
**Decision:** User-specified sandbox paths (`writable_extra`, `denied_extra`, etc.) are validated at launch time. Non-existent literal paths are skipped with a warning shown in the startup banner. Glob patterns pass through without validation.
**Why:** Silently including non-existent paths wastes sandbox policy space and confuses users debugging sandbox issues. Skipping with a warning is better than a hard failure — the rest of the config is still valid and the agent can still launch.

### DD-26: Deny-Wins Semantics
**Decision:** All guard design uses allow-broad + deny-narrow pattern.
**Why:** macOS Seatbelt: deny always wins over allow at the same specificity
level. A `(deny file-read-data (subpath ~/.ssh))` cannot be overridden by
`(allow file-read* (literal ~/.ssh/known_hosts))`. Discovered empirically on
macOS 26.4 (2026-03-24). This forces the allow-broad + deny-narrow pattern
throughout all guard implementations.
**Alternatives considered:** deny-broad + allow-narrow (broken by Seatbelt
semantics), separate deny/allow evaluation phases (not supported by Seatbelt).

### DD-27: (allow default) Paradox
**Decision:** Use `(deny default)` + ~120 granular allow rules as the
Seatbelt baseline, not `(allow default)` with targeted denies.
**Why:** `(allow default)` with targeted denies hangs Claude Code's `-p` flag
and TUI mode, despite working for `--version`. Root cause is sandbox-exec
infrastructure, not Seatbelt rule logic. The `(deny default)` approach from
agent-safehouse works reliably across all agent modes.
**Alternatives considered:** `(allow default)` (hangs agents), no sandbox
(defeats purpose).

### DD-28: Everything Is a Guard
**Decision:** Guards are the single system for all sandbox access decisions.
Each guard produces the complete set of rules — both deny and allow — for
the resources it manages. Three types (always/default/opt-in) determine
configurability.
**Why:** Replaced a split system where some rules came from seatbelt modules
and others from policy fields. Unifying into guards makes the system easier
to reason about, test, and extend. Each guard owns its resource completely.
**Alternatives considered:** Separate module + policy systems (harder to
reason about, rules from two sources could conflict).

### DD-29: Capabilities as Abstraction Layer
**Decision:** Capabilities resolve to the same sandbox fields (writable,
readable, denied, env_allow) that existed before. They are an abstraction
on top of guards, not a replacement.
**Why:** Users should not need to understand guards, seatbelt rules, or
sandbox internals. Capabilities translate task intent into security policy.
All new config fields use `omitempty` — existing configs parse identically,
no migration needed.
**Alternatives considered:** Replacing guards entirely (breaks power users
who need fine-grained control), capability-only system (insufficient
granularity for complex policies).

### DD-30: Never-Allow Hard Ceiling
**Decision:** `never_allow` paths are appended to the deny list after all
capability and sandbox resolution. No capability can override them.
**Why:** Some paths should never be accessible regardless of configuration.
Since Seatbelt uses deny-wins semantics, appending these paths last
guarantees they are always blocked. All paths are symlink-resolved before
profile generation. Contract tested.
**Alternatives considered:** Per-capability deny-only (no global override),
validation-time blocking (doesn't prevent runtime composition from
accidentally granting access).

### DD-31: No Mid-Session Escalation
**Decision:** Capabilities are resolved at session start and baked into the
immutable Seatbelt profile. The agent cannot request additional permissions
mid-session.
**Why:** Prevents prompt injection attacks from escalating sandbox
permissions. To change capabilities, start a new session. This is a hard
security constraint — the Seatbelt profile is written to disk and loaded
by sandbox-exec at process start; there is no mechanism to modify it after.
**Alternatives considered:** Dynamic permission grants (breaks Seatbelt's
static profile model), agent-requested capability prompts (social
engineering attack surface).

### Secrets & Portability Decisions

### DD-4: Sops as Go Library, Not CLI
**Decision:** Use `github.com/getsops/sops/v3/decrypt` for in-process decryption.
**Why:** Removes `sops` binary as a runtime dependency. 79+ projects already
import it (FluxCD, Terragrunt, etc.). The `decrypt.File()` API is clean.
`sops` CLI is still useful for encrypting secrets but not needed at runtime.
**Alternatives considered:** Shelling out to `sops exec-env` (adds runtime dep).

### DD-6: Single XDG Directory (No Config/Data Split)
**Decision:** Keep everything under `$XDG_CONFIG_HOME/aide/` — config.yaml AND
secrets/*.enc.yaml in the same directory.
**Why:** Splitting config and secrets across XDG_CONFIG_HOME and XDG_DATA_HOME
breaks reproducibility. Users need to manage two directories to git-track their
setup. With a single directory: `cd ~/.config/aide && git init` gives full
version control. Encrypted secrets are safe to commit.
**Trade-off:** Technically XDG spec says data goes in XDG_DATA_HOME. We prioritize
practical reproducibility over spec pedantry.

### DD-10: Ephemeral Runtime Security
**Decision:** All decrypted material dies with the process. Generated configs
go to `$XDG_RUNTIME_DIR/aide-<pid>/` (tmpfs, mode 0700) with signal handler
cleanup.
**Why:** sops-style security model. API keys and MCP server tokens in generated
config files must not persist on disk. tmpfs ensures cleanup even on crash.
Signal handlers cover normal termination. Stale dir cleanup on next launch
covers SIGKILL edge case.

### DD-11: Optional Secrets (Gradual Adoption)
**Decision:** `secret` is optional. Env values without `{{ }}` syntax
pass through as literals.
**Why:** Not everyone needs sops. Users with API keys already in their shell
env (direnv, .envrc, exports) should be able to use aide without setting up
age keys. This supports zero-config and gradual adoption — start with passthrough,
add encryption later.

### DD-13: Age Key — Support Both YubiKey and Key File
**Decision:** Try YubiKey first, then env var, then key file.
**Why:** Planning to open-source. YubiKey is most secure (hardware-bound) but
not everyone has one. Key files work for CI/Docker. `SOPS_AGE_KEY` env var
works for ephemeral environments. Supporting all three maximizes adoption.

### DD-17: Environment Inheritance — Inherit All + Clean Env Flag
**Decision:** Default: agent inherits ALL current shell env vars, aide adds/overrides
specific vars. `--clean-env` flag or `sandbox.clean_env: true` in config starts
the agent with only aide-injected env vars.
**Why:** Inherit-all is most compatible (PATH, SHELL, TERM, etc. all work).
Clean env is available for high-security contexts where env var leakage is a concern.
Config sets per-context default, flag overrides at runtime.

### Context & Agent Decisions

### DD-5: Env/Secrets on Context, Not Agent
**Decision:** Agents are just binary definitions. All env vars, secrets, and
MCP selection live on the context.
**Why:** Same `claude` binary can be used with personal API key OR work Bedrock
credentials. Agent-level env templates were misleading — they looked like they
hardcoded one key but actually varied by context's secret. Moving env to
context makes the data flow explicit.
**UX issue surfaced:** Work context uses `CLAUDE_CODE_USE_BEDROCK`, not
`ANTHROPIC_API_KEY`. Agent-level env would force defining keys not all contexts need.

### DD-7: Zero-Config Passthrough
**Decision:** When no config exists, detect agent on PATH and exec it directly.
**Why:** aide must not make things worse for simple setups. If a user has one
agent with env vars already set, `aide` should be a transparent passthrough.
Value comes from multi-context management, not from existing single-context setups.
**Behavior:** Single agent on PATH → exec immediately. Multiple agents → helpful
error with `--agent` hint. No agents → error with install guidance.

### DD-12: Minimal Config Format
**Decision:** Flat config (no `agents`/`contexts` keys) is treated as a single
default context. Agent name implies binary name.
**Why:** A user with one agent and one context shouldn't need 20+ lines of YAML.
`agent: claude` + `env:` + `secret:` is the minimal viable config.
Multi-context uses the full format. Similar to how docker-compose handles
single vs multi-service.

### DD-16: Forward All Remaining CLI Args to Agent
**Decision:** Everything after aide's own flags is forwarded to the agent binary.
e.g., `aide --context work --model opus -p "fix bug"` becomes `claude --model opus -p "fix bug"`.
**Why:** aide is a transparent wrapper. Users should interact with agents normally
and aide just ensures the right env/context. No `--` separator required — aide
consumes its known flags and passes the rest.

### DD-18: Project Root = Git Root
**Decision:** Walk up from cwd to find `.git/`. That directory is the project root.
Falls back to cwd if not a git repo.
**Why:** Most projects are git repos. This determines `.aide.yaml` lookup path,
sandbox writable paths, and `{{ .project_root }}` template variable.
**Behavior:** `git rev-parse --show-toplevel` or manual walk-up.

### DD-24: Default Context Auto-Set
**Decision:** When `aide use`, `aide setup`, or `aide context add` creates the first explicit context and no `default_context` is configured, it's automatically set to that context.
**Why:** Users transitioning from a minimal flat config to a multi-context config would otherwise lose the zero-config "works everywhere" behavior. Auto-setting the default preserves that behavior without requiring an extra manual step.

### DD-32: Trust Gate for .aide.yaml
**Decision:** `.aide.yaml` project overrides are untrusted by default.
Trust uses content-addressed SHA-256 hashing (direnv model). Deny is
path-based (not content-based).
**Why:** A cloned repo can add capabilities, unguard guards, add writable
paths, and set MCP servers. Without trust gating, any repo can modify your
sandbox policy. Path-based deny prevents cycling file contents to escape a
deny. Auto-re-trust on aide-initiated changes is guarded by
pre-modification hash check to prevent silent re-trust after external
tampering.
**Alternatives considered:** Always trust (insecure), always prompt
(annoying), git-signature-based trust (too complex, not all repos are
signed).

### UX & CLI Decisions

### DD-1: CLI Framework — Cobra
**Decision:** Use `github.com/spf13/cobra` for CLI.
**Why:** Subcommands (which, init, setup, secrets, contexts, agents) map naturally.
Built-in help generation. Most popular Go CLI framework.
**Alternatives considered:** urfave/cli (lighter but less ecosystem), stdlib flag
(too manual for this many subcommands).

### DD-2: Template Engine — Go text/template
**Decision:** Use Go's `text/template` for `{{ .secrets.xxx }}` resolution.
**Why:** Native to Go, zero dependency. Config values are template strings
resolved against a secrets map at launch time.
**Alternatives considered:** Simple string replace (less flexible), envsubst-style
`$VAR` syntax (different from established config patterns).

### DD-3: XDG Resolution — adrg/xdg Library
**Decision:** Use `github.com/adrg/xdg` for XDG directory resolution.
**Why:** Planning to open-source; library handles cross-platform edge cases.
Only ~5 lines to do manually, but the library is more robust.
**Alternatives considered:** Manual `os.Getenv("XDG_CONFIG_HOME")` with fallback.

### DD-8: MCP Aggregator Support
**Decision:** Support MCP aggregators (like 1mcp) as a first-class concept.
**Why:** Starting N individual MCP servers per agent launch is wasteful. Tools
like 1mcp aggregate multiple servers behind a single proxy. aide generates the
aggregator's config and points the agent at it.
**Fallback:** If no aggregator configured, generate native per-agent MCP config.

### DD-9: MCP Servers Need Secrets
**Decision:** MCP server definitions support env vars with template syntax,
resolved against the context's secrets.
**Why:** Many MCP servers need credentials (context7 tokens, serena licenses).
Without this, users would need to set these separately, defeating the purpose
of unified context management.

### DD-19: Verbose Flag for Debug Output
**Decision:** `-v/--verbose` flag shows context resolution steps, matched rules,
secrets file path (not contents), sandbox policy, env var names (redacted values).
**Why:** Users need to debug "why did aide pick work context?" without guessing.
`aide which` shows the result; `-v` shows the process.

### DD-20: Strict Config Validation + aide validate Command
**Decision:** Fail on structural errors (missing agent binary, bad YAML, broken
references). Add `aide validate` command to check config without launching. Validation
also runs on every launch. Errors must be easy to understand with fix suggestions.
**Why:** Typos in context names or agent references should fail loudly, not silently
do nothing. `aide validate` catches issues before you're in the middle of work.

### DD-21: Shell Completions via Cobra
**Decision:** Use cobra's built-in completion generation for bash, zsh, fish.
Complete context names, agent names, secrets file names.
**Why:** Minimal effort (cobra generates them), big UX improvement for daily use.

### DD-25: Preferences System
**Decision:** Global display and behavior settings live in a `preferences:` block at config top level. Currently controls: `show_info` (bool), `info_style` (compact|boxed|clean), `info_detail` (normal|detailed). Project-level `.aide.yaml` can override field-by-field.
**Why:** Startup banners and info display are personal preferences that shouldn't be buried in context config or require CLI flags. A top-level `preferences:` block keeps them separate from context/agent config and allows per-project overrides for cases where a project needs different verbosity.
