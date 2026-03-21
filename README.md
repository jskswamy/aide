# aide

A CLI tool that resolves and launches the right coding agent with the right
credentials and sandbox based on project context. No manual switching between
API keys, agents, or permission configs across projects.

## The Problem

When using AI coding agents across personal, work, and open-source projects,
you manage:

- Different API keys and credentials (personal Anthropic key vs work Bedrock)
- Different agents per project (Claude for work, Codex for OSS)
- Risk of using the wrong credentials or license for a project
- Permission fatigue from agents asking to read/write files on every action

This requires manual switching or remembering which agent and credentials to
use for each project. Even with a single agent, switching between personal
and work credentials is error-prone.

## How aide Solves This

aide resolves context automatically based on git remote URL and directory path
patterns. It decrypts secrets in-process, applies an OS-native sandbox, and
exec's the agent — all from a single command.

```bash
cd ~/work/project && aide    # Launches claude with work Bedrock credentials
cd ~/oss/repo && aide        # Launches codex with personal OpenAI key
cd ~/experiment && aide      # Auto-detects agent on PATH, zero config needed
```

**What aide does:**

- **Zero config to start** — detects your agent on PATH and execs it directly
- **Automatic context switching** — matches git remote or directory to the right agent, credentials, and settings
- **Encrypted secrets** — API keys stored with age/sops encryption, decrypted in-process at launch, never plaintext on disk
- **Sandboxed by default** — agents run inside an OS-native filesystem and network sandbox (macOS sandbox-exec, Linux Landlock)
- **Any agent** — Claude, Gemini, Codex, Aider, Goose, Amp
- **Git-trackable config** — entire setup lives in one directory, safe to version control

## Install

```bash
# From source
go install github.com/jskswamy/aide/cmd/aide@latest

# Or build locally
git clone https://github.com/jskswamy/aide.git
cd aide && make build
# Binary at ./bin/aide
```

### Prerequisites

- Go 1.25+ (build only)
- One or more coding agents installed (`claude`, `codex`, `gemini`, `aider`, `goose`, `amp`)
- Optional: `age` + `age-plugin-yubikey` for encrypted secrets
- Optional: `sops` CLI for creating/editing encrypted secrets (not needed at runtime)

## Quick Start

### Zero Config

If you have one agent on PATH with its API key already in your environment:

```bash
aide
```

aide detects the agent binary, confirms the API key is set, and execs it
directly. No configuration file needed.

If multiple agents are found:

```bash
aide --agent claude
```

### First-Time Setup

```bash
aide setup
```

The interactive wizard will:
1. Detect or generate an age encryption key
2. Find agents on your PATH
3. Create a minimal `~/.config/aide/config.yaml`

### Bind a Directory to an Agent

```bash
aide use claude                          # Bind current directory to claude
aide use claude --match "~/work/*"       # Bind a glob pattern
aide use --context work                  # Add current directory to existing context
aide use claude --secret personal        # Also set the secrets file
aide use claude --sandbox strict         # Use a named sandbox profile
```

## Configuration

All configuration lives under `~/.config/aide/` (or `$XDG_CONFIG_HOME/aide/`
if the environment variable is set):

```
~/.config/aide/
  config.yaml                  # Global config
  secrets/
    personal.enc.yaml          # Encrypted secrets (age/sops)
    work.enc.yaml
```

Per-project overrides go in `.aide.yaml` at the project root (or git root).

### Minimal Config (single agent, single context)

For users with one agent and one set of credentials:

```yaml
# ~/.config/aide/config.yaml
agent: claude
env:
  ANTHROPIC_API_KEY: "{{ .secrets.anthropic_api_key }}"
secret: personal
```

The `agent` field maps to a binary name on PATH. Template syntax
`{{ .secrets.<key> }}` references keys in the encrypted secrets file.

### Multi-Context Config

For users switching between work, personal, and open-source projects:

```yaml
# ~/.config/aide/config.yaml

agents:
  claude:
    binary: claude
  codex:
    binary: codex

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

  personal:
    match:
      - remote: "github.com/jskswamy/*"
    agent: claude
    secret: personal
    env:
      ANTHROPIC_API_KEY: "{{ .secrets.anthropic_api_key }}"

  oss:
    match:
      - remote: "github.com/*"
    agent: codex
    secret: personal
    env:
      OPENAI_API_KEY: "{{ .secrets.openai_api_key }}"

default_context: personal
```

Contexts match by git remote URL patterns and directory path globs. The most
specific match wins. `default_context` is used when nothing matches.

### Per-Project Override

Drop a `.aide.yaml` in any project root to override the matched context:

```yaml
# ~/work/special-project/.aide.yaml
agent: gemini
env:
  CUSTOM_VAR: "value"
```

Project overrides take the highest priority in context resolution.

### Context Resolution Order

1. `.aide.yaml` in project root (highest priority)
2. Exact path match > glob path match > remote match
3. Longer/more specific patterns win over shorter ones
4. `default_context` fallback

### Optional Secrets

`secret` is optional. If API keys are already in your shell environment
(via direnv, `.envrc`, exports), aide works without sops. Env values without
`{{ }}` template syntax pass through as literals:

```yaml
contexts:
  work:
    agent: claude
    env:
      CLAUDE_CODE_USE_BEDROCK: "1"
```

### Preferences

Global display settings for startup info banner:

```yaml
# In config.yaml (top-level)
preferences:
  show_info: true       # Show startup banner (default: true)
  info_style: compact   # compact | boxed | clean (default: compact)
  info_detail: normal   # normal | detailed (default: normal)
```

Can also be overridden per-project in `.aide.yaml`:

```yaml
# .aide.yaml
preferences:
  info_detail: detailed  # Always show detailed info for this project
```

## Usage

### Launching an Agent

```bash
aide                              # Auto-resolve context and launch
aide --agent claude               # Override agent selection
aide --resolve                    # Show detailed startup info then launch
aide -- --model opus -p "fix it"  # Forward arguments to the agent
aide --yolo                       # Skip agent permissions (sandbox still applies)
aide --clean-env                  # Launch with only aide-injected env vars
```

The `--yolo` flag maps to agent-specific permission flags (e.g.,
`--dangerously-skip-permissions` for Claude, `--full-auto` for Codex). The
OS-native sandbox still applies regardless.

Everything after `--` is forwarded directly to the agent binary.

### Inspecting Context

```bash
aide which                        # Show matched context and match reason
aide which --resolve              # Show resolved environment values
aide validate                     # Validate config structure and references
```

`aide which` shows what matched, what else could have matched, and why. Useful
for debugging context resolution without launching.

### Managing Agents

```bash
aide agents list                  # Show configured and auto-detected agents
aide agents add gemini --binary /usr/local/bin/gemini
aide agents edit claude --binary /opt/claude/bin/claude
aide agents remove codex
```

Agents are binary definitions only. All credentials, env vars, and settings
live on contexts, not agents. The same `claude` binary can be used with
different API keys across contexts.

### Managing Contexts

```bash
aide context list                 # List all configured contexts
aide context add                  # Add a new context interactively
aide context add-match work       # Add a match rule to an existing context
aide context rename work corp     # Rename a context
aide context remove oss           # Remove a context
```

### Managing Environment Variables

```bash
aide env list                              # List env vars for CWD-matched context
aide env list --context work               # List env vars for a specific context
aide env set API_KEY sk-ant-xxx            # Set a literal value
aide env set API_KEY --from-secret api_key # Set as template referencing a secret
aide env set API_KEY --from-secret         # Interactive secret key picker
aide env set REGION us-west-2 --context work  # Target a specific context
```

The `--from-secret` flag generates `{{ .secrets.<key> }}` template syntax
automatically, avoiding manual template string entry.

### Managing Secrets

Secrets are sops-encrypted YAML files using age keys. aide handles the full
lifecycle without requiring the `sops` CLI at runtime.

```bash
aide secrets list                 # List encrypted secrets files
aide secrets create personal \
  --age-key age1abc...            # Create new file (opens $EDITOR)
aide secrets edit personal        # Decrypt -> $EDITOR -> re-encrypt
aide secrets keys personal        # Show key names (not values)
aide secrets rotate personal \
  --add-key age1abc...            # Add a recipient (e.g., new machine)
aide secrets rotate personal \
  --remove-key age1xyz...         # Remove a recipient
```

**Age key discovery** (tried in order):

1. YubiKey via `age-plugin-yubikey` — hardware-bound, key never leaves device
2. `$SOPS_AGE_KEY` environment variable — for CI/Docker
3. `$SOPS_AGE_KEY_FILE` — custom key file path
4. `$XDG_CONFIG_HOME/sops/age/keys.txt` — default sops key location

**Create flow:** aide detects the age public key, opens `$EDITOR` with a YAML
template, encrypts the result, and writes it to
`$XDG_CONFIG_HOME/aide/secrets/<name>.enc.yaml`. Plaintext is held in a tmpfs
temp file during editing, never on persistent disk.

**Edit flow:** decrypt in-process to a secure temp file, open `$EDITOR`,
re-encrypt on save, remove temp file.

**Rotate flow:** add or remove age public key recipients on an existing
encrypted file without exposing plaintext.

### Sandbox

Sandboxing is on by default with sensible defaults. Agents run freely within
the sandbox boundary. No per-action permission prompts.

**Default policy:**

| Access | Paths |
|--------|-------|
| Writable | Project root, runtime dir, temp dirs |
| Readable | Home directory, system binaries |
| Denied | SSH private keys, cloud credentials, browser data |
| Network | Outbound allowed |
| Subprocesses | Allowed |

```bash
aide sandbox show                          # Effective policy for current directory
aide sandbox show --context work           # Policy for a specific context
aide sandbox test                          # Print platform-specific profile (for debugging)
aide sandbox test --context work           # Debug profile for a specific context
```

**Customize per-context** with inline policy:

```yaml
contexts:
  work:
    sandbox:
      writable:
        - "{{ .project_root }}"
        - "{{ .runtime_dir }}"
      readable:
        - "{{ .project_root }}"
        - "~/.gitconfig"
      denied:
        - "~/.ssh/id_*"
        - "~/.aws/credentials"
      network: outbound            # outbound | none | unrestricted
      allow_subprocess: true
      clean_env: false
```

Use `_extra` suffixes to extend defaults without replacing them:

```yaml
contexts:
  work:
    sandbox:
      writable_extra:
        - "/tmp/build-cache"
      denied_extra:
        - "~/.config/aide/secrets/"
```

**Quick adjustments** from the command line:

```bash
aide sandbox allow ~/.config/tool          # Add to readable_extra
aide sandbox allow --write /tmp/cache      # Add to writable_extra
aide sandbox deny ~/.ssh/id_ed25519        # Add to denied_extra
aide sandbox ports 443 8080                # Set allowed outbound ports
aide sandbox reset                         # Revert to defaults
```

All sandbox commands accept `--context <name>` to target a specific context.
Without it, they apply to the context matched by the current directory.

**Named profiles** for reuse across contexts:

```bash
aide sandbox list                 # List all named profiles
aide sandbox create strict        # Create a new profile (opens $EDITOR)
aide sandbox create strict --from default  # Create based on existing profile
aide sandbox edit strict          # Modify a profile
aide sandbox remove strict        # Delete a profile
```

Reference a profile in context config:

```yaml
contexts:
  work:
    sandbox: strict               # Use named profile
```

Disable sandboxing for a context:

```yaml
contexts:
  trusted:
    sandbox: false
```

### Config Management

```bash
aide config show                  # Print config file contents
aide config edit                  # Open config in $EDITOR
aide init                         # Create initial config interactively
aide init --force                 # Overwrite existing config (creates .bak backup)
```

### Shell Completions

```bash
# Bash
aide completion bash >> ~/.bashrc

# Zsh
aide completion zsh >> ~/.zshrc

# Fish
aide completion fish > ~/.config/fish/completions/aide.fish
```

## Reproducibility

### Personal Setup

Track your entire aide config in version control:

```bash
cd ~/.config/aide
git init && git add -A && git commit -m "aide config"
```

Encrypted secrets are safe to commit. Only holders of the age private key
can decrypt them.

### Team Shared Config

Share a config repo across a team. Each member adds their own age key:

```bash
git clone git@github.com:team/aide-config.git ~/.config/aide
aide secrets rotate work --add-key $(age-keygen -y key.txt)
```

### Docker / CI

```dockerfile
COPY aide-config/ /root/.config/aide/
ENV SOPS_AGE_KEY=AGE-SECRET-KEY-1...
RUN aide --agent claude -- -p "run tests"
```

The `SOPS_AGE_KEY` environment variable provides the decryption key in memory
without writing a key file to the image.

## Security Model

**Secrets never touch disk in plaintext.** Decrypted in-process via the sops
Go library, passed as environment variables to the agent process. Generated
runtime files (e.g., aggregator configs) are written to
`$XDG_RUNTIME_DIR/aide-<pid>/` (tmpfs, mode 0700) and cleaned on exit via
signal handlers for SIGTERM, SIGINT, SIGQUIT, and SIGHUP. On SIGKILL, tmpfs
cleans on reboot; the next aide launch detects and removes stale directories.

**OS-native sandboxing** constrains agent filesystem and network access:

| Platform | Mechanism | Integration |
|----------|-----------|-------------|
| macOS | sandbox-exec (Seatbelt profiles) | Generates `.sb` profiles dynamically from policy config |
| Linux (kernel 5.13+) | Landlock | Native Go via `go-landlock`, no CGo, no external binary |
| Linux (older kernels) | bubblewrap (`bwrap`) | Requires `bwrap` on PATH |

**Age key access control** — only holders of listed age private keys (or
YubiKey hardware tokens) can decrypt secrets files. Multiple recipients per
file support laptop, desktop, and CI access without sharing private keys.

## Development

```bash
make build          # Build to ./bin/aide
make test           # Run unit tests
make vet            # Run go vet
make lint           # Run golangci-lint (if installed)
make test-linux     # Run Linux-specific tests in Docker container
make test-all       # All of the above
```

Linux-specific code (Landlock, bubblewrap) requires a Linux environment. The
`make test-linux` target builds a Docker container and runs the full test suite
inside it.

## License

[MIT](LICENSE)
