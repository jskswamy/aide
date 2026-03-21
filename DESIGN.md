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

**aide's unique combination:**
1. Automatic context resolution from git remote + directory patterns
2. Encrypted secrets with sops/age (YubiKey support)
3. Unified context = agent + credentials + MCP servers + env
4. Transparent wrapper (replaces typing `claude` or `gemini`)
5. Config-as-code — git-trackable, reproducible across machines/Docker/CI
6. Sandboxed agent execution — pre-defined security boundary eliminates approval fatigue

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
directory path patterns. When you need multi-context management, aide
handles the complexity. When you don't, it stays out of the way.

Key design principles:

- **Agents are just binaries.** All env vars, secrets, and MCP selection
  live on the context, not the agent definition.
- **Zero-config by default.** If one known agent binary is on PATH with
  its API key already in the environment, aide just exec's it.
- **Secrets never touch disk in plaintext.** Decrypted in-process using
  sops as a Go library, passed as env vars, ephemeral runtime files
  cleaned on exit.
- **One directory to track.** Everything lives under `$XDG_CONFIG_HOME/aide/`
  so you can `git init` the whole thing.

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

## Zero-Config Passthrough

When no config file exists, aide does not require setup:

1. Scan PATH for known agent binaries (`claude`, `gemini`, `codex`)
2. If exactly one found and it already has its API key in the environment,
   just exec it (pure passthrough — aide adds zero overhead)
3. If multiple found, show a helpful message:
   `"Multiple agents found: claude, codex. Use --agent to pick one, or run aide setup."`
4. If none found: `"No known agent binaries found on PATH. Install claude, gemini, or codex."`

aide adds value only when you need multi-context management. For single-agent,
single-context users with env vars already set, aide is invisible.

## Context Resolution

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

## Security Model

### Ephemeral Runtime Security

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

### Agent Sandboxing

**The problem:** Agentic development requires letting agents run autonomously —
reading files, writing code, running tests, installing packages. Without a
sandbox, every action triggers a permission prompt. Approval fatigue means
users end up blindly pressing "yes" and eventually approve something dangerous.

**The solution:** Define the security boundary upfront in the context config.
The agent runs freely within those bounds. No per-action prompts needed.

aide uses OS-native sandboxing:

| OS | Mechanism | Go Integration |
|----|-----------|---------------|
| **macOS** | `sandbox-exec` (Seatbelt profiles) | Generate `.sb` policy, invoke via `os/exec` |
| **Linux** | Landlock (kernel 5.13+) | `go-landlock` library — native Go, no CGo, no external binary |
| **Linux fallback** | bubblewrap (`bwrap`) | Invoke via `os/exec` (for older kernels without Landlock) |

#### Sandbox Policy in Config

Contexts define what the agent can access:

```yaml
contexts:
  personal:
    agent: claude
    sandbox:
      # Filesystem
      writable:
        - "{{ .project_root }}"          # Project directory
        - "{{ .runtime_dir }}"           # Ephemeral MCP configs
      readable:
        - "{{ .project_root }}"
        - "~/.gitconfig"
        - "~/.ssh/known_hosts"           # Git operations
      denied:
        - "~/.ssh/id_*"                  # Private keys
        - "~/.aws/credentials"           # Cloud credentials
        - "~/.config/aide/secrets/"      # Encrypted secrets (agent doesn't need these)

      # Network
      network: outbound                   # outbound | none | unrestricted
      # network_allow:                   # Future: domain allowlist
      #   - "api.anthropic.com"
      #   - "github.com"

      # Process
      allow_subprocess: true              # Agent needs to run tests, build, etc.

      # Environment
      clean_env: false                    # true = agent starts with only aide-injected vars
```

#### Sensible Defaults

If no `sandbox` block is defined, aide applies a **default sandbox policy**:

- **Writable:** project root + runtime dir + temp dirs
- **Readable:** project root + system binaries + common dotfiles (gitconfig, ssh/known_hosts)
- **Denied:** SSH private keys, cloud credentials, browser data, other projects
- **Network:** outbound allowed (agents need API access)
- **Subprocesses:** allowed (agents need to run tests/builds)

This means sandboxing is **on by default** — users opt out, not in. The default
policy is safe enough for most use cases. Power users can customize per-context.

#### Why This Matters

- Anthropic's research found sandboxing reduced permission prompts by **84%**
- The security boundary is defined once in config, not negotiated per-action
- Agents can run autonomously — `aide` becomes a "launch and walk away" command
- Prevents prompt injection attacks from reading secrets or modifying system files
- Combined with ephemeral runtime (secrets die with process), this creates
  defense-in-depth: the agent can't read secrets it wasn't given, can't write
  outside its sandbox, and all ephemeral data is cleaned on exit

#### Platform Notes

- **macOS `sandbox-exec`:** Deprecated by Apple but still functional. agent-safehouse
  and Anthropic's sandbox-runtime both use it successfully. aide generates Seatbelt
  `.sb` profiles dynamically based on context config.
- **Linux Landlock:** Preferred on Linux. Self-sandboxing (the process restricts itself),
  no external binary needed. `go-landlock` library provides a clean Go API with
  graceful degradation on older kernels via `BestEffort()`.
- **Linux bubblewrap:** Fallback for kernels without Landlock. Uses Linux namespaces
  for filesystem isolation. Requires `bwrap` binary on PATH.

### Launch Flow

1. Read `config.yaml` (no secrets in this file, safe on disk)
2. Resolve context (git remote + path matching)
3. Decrypt secrets in memory (sops library call returns Go map)
4. Create `$XDG_RUNTIME_DIR/aide-<pid>/` (tmpfs, mode 0700)
5. Generate MCP/aggregator config with resolved secrets into temp dir
6. Build env vars (resolve templates against secrets map) — in memory only
7. Apply sandbox policy (generate platform-specific policy from context config)
8. Exec agent inside sandbox with env vars + MCP config path pointing to temp dir
9. On exit (normal or signal): `rm -rf` temp dir

### Access Control

Access is determined by age key possession:

- Only holders of a listed age private key (or YubiKey) can decrypt
- Multiple recipients per secrets file (e.g., laptop + desktop + CI)
- Rotation adds/removes recipients without re-entering secrets

### Age Key Discovery

Tried in order:

1. **YubiKey** (via `age-plugin-yubikey`) — hardware-bound, key never on disk
2. **`$SOPS_AGE_KEY` env var** — for CI/Docker (key in memory, not on disk)
3. **`$SOPS_AGE_KEY_FILE`** — custom key file location
4. **`$XDG_CONFIG_HOME/sops/age/keys.txt`** — default age key location

## Secrets Management

Secrets are stored as sops-encrypted YAML files in `$XDG_CONFIG_HOME/aide/secrets/`,
decrypted in-process at launch time using the sops Go library.

### Secrets File Format

```yaml
# $XDG_CONFIG_HOME/aide/secrets/personal.enc.yaml (before encryption)
anthropic_api_key: sk-ant-...
openai_api_key: sk-...
context7_token: ctx7-...
```

The `secret` field in context config is a filename resolved relative
to `$XDG_CONFIG_HOME/aide/secrets/`. Absolute paths are also supported.

### Secrets Lifecycle

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

## Reproducibility

### Pattern 1: Personal Git-Tracked Config

Track your entire aide setup in version control:

```bash
cd ~/.config/aide && git init && git add -A && git commit -m "aide config"
```

Encrypted secrets are safe to commit — only age key holders can decrypt.

### Pattern 2: Team Shared Config

Share config across a team, with per-person age keys:

```bash
git clone git@github.com:team/aide-config.git ~/.config/aide
aide secrets rotate work --add-key $(age-keygen -y key.txt)
```

### Pattern 3: Docker / CI

```dockerfile
COPY aide-config/ /root/.config/aide/
ENV SOPS_AGE_KEY=AGE-SECRET-KEY-1...
```

The `SOPS_AGE_KEY` env var provides the decryption key in memory without
writing a key file to the image.

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
    main.go             # Entry point, cobra root command
internal/
  config/
    config.go           # Config loading, merging
    schema.go           # Config types and validation
    paths.go            # XDG path resolution (via adrg/xdg)
    template.go         # text/template resolution for env vars
  context/
    resolver.go         # Context matching engine
    git.go              # Git remote detection + project root (git root)
  secrets/
    sops.go             # Sops decryption (library-based)
    manager.go          # Secrets lifecycle (create, edit, rotate)
    age.go              # Age key discovery (YubiKey + key file + env var)
  launcher/
    launcher.go         # Agent process launcher
    runtime.go          # Ephemeral runtime dir management
  sandbox/
    sandbox.go          # Sandbox interface and default policy
    darwin.go           # macOS sandbox-exec (Seatbelt profile generation)
    linux.go            # Linux Landlock (via go-landlock) + bwrap fallback
    policy.go           # Policy config parsing and defaults
  mcp/
    generator.go        # MCP config generation (per-agent native format)
    aggregator.go       # Aggregator config generation (1mcp, etc.)
```

## Dependencies

- `github.com/spf13/cobra` for CLI framework
- `gopkg.in/yaml.v3` for YAML parsing
- `github.com/gobwas/glob` for glob matching
- `github.com/getsops/sops/v3` for in-process secret decryption
- `github.com/adrg/xdg` for XDG base directory resolution
- `github.com/landlock-lsm/go-landlock` for Linux Landlock sandboxing (native Go, no CGo)
- Shells out to `git` for remote detection
- Shells out to `sandbox-exec` on macOS for Seatbelt sandboxing
- Shells out to `bwrap` on Linux as Landlock fallback (optional)

## Existing Infrastructure (from nixos-config)

- `sops` CLI already installed (used for encrypting secrets; not needed at runtime)
- `age` + `age-plugin-yubikey` already installed
- Current `cctx` overlay at `overlays/80-cctx.nix` (will be replaced by aide)

## Design Decisions

Decisions made during UX study (March 2026), with rationale preserved for
future reference.

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

### DD-4: Sops as Go Library, Not CLI
**Decision:** Use `github.com/getsops/sops/v3/decrypt` for in-process decryption.
**Why:** Removes `sops` binary as a runtime dependency. 79+ projects already
import it (FluxCD, Terragrunt, etc.). The `decrypt.File()` API is clean.
`sops` CLI is still useful for encrypting secrets but not needed at runtime.
**Alternatives considered:** Shelling out to `sops exec-env` (adds runtime dep).

### DD-5: Env/Secrets on Context, Not Agent
**Decision:** Agents are just binary definitions. All env vars, secrets, and
MCP selection live on the context.
**Why:** Same `claude` binary can be used with personal API key OR work Bedrock
credentials. Agent-level env templates were misleading — they looked like they
hardcoded one key but actually varied by context's secret. Moving env to
context makes the data flow explicit.
**UX issue surfaced:** Work context uses `CLAUDE_CODE_USE_BEDROCK`, not
`ANTHROPIC_API_KEY`. Agent-level env would force defining keys not all contexts need.

### DD-6: Single XDG Directory (No Config/Data Split)
**Decision:** Keep everything under `$XDG_CONFIG_HOME/aide/` — config.yaml AND
secrets/*.enc.yaml in the same directory.
**Why:** Splitting config and secrets across XDG_CONFIG_HOME and XDG_DATA_HOME
breaks reproducibility. Users need to manage two directories to git-track their
setup. With a single directory: `cd ~/.config/aide && git init` gives full
version control. Encrypted secrets are safe to commit.
**Trade-off:** Technically XDG spec says data goes in XDG_DATA_HOME. We prioritize
practical reproducibility over spec pedantry.

### DD-7: Zero-Config Passthrough
**Decision:** When no config exists, detect agent on PATH and exec it directly.
**Why:** aide must not make things worse for simple setups. If a user has one
agent with env vars already set, `aide` should be a transparent passthrough.
Value comes from multi-context management, not from existing single-context setups.
**Behavior:** Single agent on PATH → exec immediately. Multiple agents → helpful
error with `--agent` hint. No agents → error with install guidance.

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

### DD-12: Minimal Config Format
**Decision:** Flat config (no `agents`/`contexts` keys) is treated as a single
default context. Agent name implies binary name.
**Why:** A user with one agent and one context shouldn't need 20+ lines of YAML.
`agent: claude` + `env:` + `secret:` is the minimal viable config.
Multi-context uses the full format. Similar to how docker-compose handles
single vs multi-service.

### DD-13: Age Key — Support Both YubiKey and Key File
**Decision:** Try YubiKey first, then env var, then key file.
**Why:** Planning to open-source. YubiKey is most secure (hardware-bound) but
not everyone has one. Key files work for CI/Docker. `SOPS_AGE_KEY` env var
works for ephemeral environments. Supporting all three maximizes adoption.

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

### DD-15: Default Sandbox Policy
**Decision:** If no `sandbox` block in context config, apply a default policy:
writable = project root + runtime dir + temp; readable = project + system bins +
gitconfig + ssh/known_hosts; denied = SSH private keys, cloud creds, browser data;
network = outbound; subprocesses = allowed.
**Why:** Security should not require configuration. The default must be safe enough
for most use cases while not breaking agent functionality (tests, builds, git ops).
Power users can tighten or loosen per-context.

### DD-16: Forward All Remaining CLI Args to Agent
**Decision:** Everything after aide's own flags is forwarded to the agent binary.
e.g., `aide --context work --model opus -p "fix bug"` becomes `claude --model opus -p "fix bug"`.
**Why:** aide is a transparent wrapper. Users should interact with agents normally
and aide just ensures the right env/context. No `--` separator required — aide
consumes its known flags and passes the rest.

### DD-17: Environment Inheritance — Inherit All + Clean Env Flag
**Decision:** Default: agent inherits ALL current shell env vars, aide adds/overrides
specific vars. `--clean-env` flag or `sandbox.clean_env: true` in config starts
the agent with only aide-injected env vars.
**Why:** Inherit-all is most compatible (PATH, SHELL, TERM, etc. all work).
Clean env is available for high-security contexts where env var leakage is a concern.
Config sets per-context default, flag overrides at runtime.

### DD-18: Project Root = Git Root
**Decision:** Walk up from cwd to find `.git/`. That directory is the project root.
Falls back to cwd if not a git repo.
**Why:** Most projects are git repos. This determines `.aide.yaml` lookup path,
sandbox writable paths, and `{{ .project_root }}` template variable.
**Behavior:** `git rev-parse --show-toplevel` or manual walk-up.

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

## Epic Breakdown

### Epic 1: Core Config System (P0)

0. Build harness bootstrap (go mod, Makefile, lint, test fixtures)
1. Config schema types
2. XDG config path resolution (via adrg/xdg)
3. Config loader (global + project merge, minimal format detection)
4. Git remote detection + project root
5. Context matching engine (glob, specificity, fallback)

### Epic 2: Secrets & Agent Launch (P0)

6. Age key discovery (YubiKey + key file + env var)
7. Sops decryption via library
8. Template resolution for env vars
9. Ephemeral runtime dir management
10. Agent launcher (resolve + exec with env + cleanup)
11. Zero-config passthrough (agent detection, no-config launch)

### Epic 3: Agent Sandboxing (P0)

12. Sandbox interface and default policy
13. macOS sandbox-exec (Seatbelt profile generation)
14. Linux Landlock sandboxing (via go-landlock + bwrap fallback)
15. Sandbox policy config parsing (from context config)

### Epic 4: MCP System (P1)

16. MCP server definitions with secrets
17. MCP config generation (per-agent native format)
18. MCP aggregator support (1mcp config generation)

### Epic 5: Secrets Lifecycle (P1)

19. `aide secrets create`
20. `aide secrets edit`
21. `aide secrets list`
22. `aide secrets rotate`

### Epic 6: CLI UX (P2)

23. `aide which` (context introspection with conflict info)
24. `aide validate` (config validation with actionable errors)
25. `aide init` (project override creation)
26. `aide setup` (interactive wizard with age key generation)
27. `aide contexts` + `aide agents` (list commands)
28. Verbose mode (`-v` flag for debug output)
29. Shell completions (bash, zsh, fish via cobra)
30. First-run guidance (no config detected message)

### Epic 7: Distribution (P2)

31. Nix overlay and packaging (replace cctx)

### Dependency Graph

```
Epic 1: Core Config
  T1 (schema) -> T2 (xdg) -> T3 (loader)
  T1 -> T4 (git remote)
  T3 + T4 -> T5 (context matcher)

Epic 2: Secrets & Launch
  T6 (age discovery) -> T7 (sops decrypt) -> T8 (template)
  T8 + T3 -> T9 (runtime dir) -> T10 (launcher)
  T5 + T10 -> T11 (zero-config passthrough)

Epic 3: Sandboxing
  T12 (sandbox interface + defaults) -> T13 (macOS sandbox-exec)
  T12 -> T14 (Linux Landlock + bwrap)
  T12 + T3 -> T15 (sandbox policy from config)
  T15 -> T10 (launcher uses sandbox)

Epic 4: MCP
  T10 -> T16 (mcp definitions) -> T17 (mcp gen) -> T18 (aggregator)

Epic 5: Secrets Lifecycle
  T6 + T7 -> T19 (create) -> T20 (edit)
  T3 -> T21 (list)
  T7 -> T22 (rotate)

Epic 6: CLI UX
  T5 -> T23 (which)
  T3 -> T24 (validate)
  T3 -> T25 (init)
  T6 -> T26 (setup)
  T3 -> T27 (list cmds)
  T5 -> T28 (verbose)
  T3 -> T29 (completions)
  T11 -> T30 (first-run)

Epic 7: Distribution
  T10 -> T31 (nix overlay)
```
