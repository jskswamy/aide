# README Rewrite Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rewrite README.md to lead with the "stop babysitting your agent" framing, using before/after contrast around three pillars (sandbox, unified UX, reproducibility).

**Architecture:** Single file rewrite of `README.md`. All existing content preserved, reorganized into 9 sections per spec. New content: tagline, problem statement, before/after blocks.

**Tech Stack:** Markdown

**Spec:** `docs/superpowers/specs/2026-03-23-readme-rewrite-design.md`

---

### Task 1: Write the new README

**Files:**
- Modify: `README.md`

The README follows the 9-section structure from the spec. Below is the complete content to write.

- [ ] **Step 1: Replace README.md with the rewritten content**

```markdown
# aide

Stop babysitting your agent.

One command. Any agent. Sandboxed, reproducible, zero decision fatigue.

---

You planned the work. You know what needs to happen. But instead of letting your agent execute, you're stuck evaluating every file read, every shell command, every network call. That's not autonomy — that's babysitting with extra steps.

aide fixes three things:

### Sandbox

| Without aide | With aide |
|-------------|-----------|
| Skip all permissions and pray nothing touches your SSH keys. Or click "allow" 200 times per session. | 20 OS-native guards active by default. SSH keys, cloud credentials, browser data, password stores — blocked. The agent runs free inside guardrails. No prompts, no prayer. |

### Unified UX

| Without aide | With aide |
|-------------|-----------|
| Each agent has its own CLI, config format, env vars. Switching agents means rewiring your workflow. | `aide` resolves the right agent, credentials, and sandbox from your project directory. One command, every project. Swap agents without changing how you work. |

### Reproducibility

| Without aide | With aide |
|-------------|-----------|
| API keys in `.env` files, shell profiles, wrapper scripts. One bad commit and they're leaked. New machine means hours of setup. | Secrets are sops-encrypted with age keys. Config and encrypted secrets live in git. `git clone` your config on a new machine and you're done. |

## Quick Start

```bash
# Install latest release (macOS)
curl -sSfL https://raw.githubusercontent.com/jskswamy/aide/main/install.sh | sh

# Install to a specific directory
curl -sSfL https://raw.githubusercontent.com/jskswamy/aide/main/install.sh | INSTALL_DIR=/usr/local/bin sudo sh

# Install a specific version
curl -sSfL https://raw.githubusercontent.com/jskswamy/aide/main/install.sh | VERSION=v0.1.0 sh

# Install from source
go install github.com/jskswamy/aide/cmd/aide@latest

# Or build locally
git clone https://github.com/jskswamy/aide.git
cd aide && make build   # Binary at ./bin/aide
```

Four commands to know:

```bash
aide                    # Resolve context and launch the agent (sandboxed)
aide setup              # Interactive first-time configuration
aide --agent codex      # Override agent selection
aide sandbox guards     # See what the sandbox protects
```

No config file required. If one agent exists on PATH with its API key in the environment, `aide` launches it sandboxed — zero setup.

## How It Works

1. Run `aide` in any project directory.
2. aide matches the git remote URL and directory path against your config.
3. It resolves the context: agent, credentials, and sandbox policy.
4. Secrets decrypt in-process via the sops Go library. Nothing hits disk.
5. aide applies 20 guards via macOS Seatbelt, blocking access to sensitive data. Linux sandbox support (Landlock) is planned.
6. aide execs the agent with the resolved environment inside the sandbox.

No config file? aide detects your agent on PATH and launches it directly.

## Configuration

All config lives under `~/.config/aide/` (or `$XDG_CONFIG_HOME/aide/`).

**Minimal config:**

```yaml
agent: claude
secret: personal
env:
  ANTHROPIC_API_KEY: "{{ .secrets.anthropic_api_key }}"
```

**Multi-context config:**

```yaml
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
      - remote: "github.com/myuser/*"
    agent: codex
    secret: personal
    env:
      OPENAI_API_KEY: "{{ .secrets.openai_api_key }}"

  oss:
    match:
      - remote: "github.com/*"
    agent: aider
    secret: personal
    env:
      ANTHROPIC_API_KEY: "{{ .secrets.anthropic_api_key }}"

default_context: personal
```

Contexts match git remote URL patterns and directory path globs. The most specific match wins. `default_context` is the fallback. See [docs/configuration.md](docs/configuration.md) for the full reference.

## Sandbox

Agents run inside an OS-native sandbox by default. No per-action permission prompts.

### What the sandbox protects

aide blocks access to sensitive data by default:

| Protected | Guards |
|-----------|--------|
| SSH private keys | Blocks `~/.ssh` (allows `known_hosts` and `config` for git) |
| Cloud credentials | AWS, GCP, Azure, DigitalOcean, Oracle Cloud |
| Infrastructure | Kubernetes config, Terraform credentials, Vault tokens |
| Browser data | Cookies, passwords, history (Chrome, Firefox, Safari, etc.) |
| Password managers | 1Password, Bitwarden, pass, gopass, GPG private keys |

The agent can still use macOS Keychain for its own authentication, read git config, and access Node.js/Nix toolchains. These are always-on guards that provide controlled access.

Need Docker or GitHub CLI credentials in the sandbox? Enable them:

```bash
aide sandbox guard docker
aide sandbox guard github-cli
```

Don't need browser protection? Disable it:

```bash
aide sandbox unguard browsers
```

The macOS Seatbelt rules port the shell scripts from [agent-safehouse](https://github.com/eugene1g/agent-safehouse) as a Go library. The `pkg/seatbelt` library is reusable in your own Go projects. See [docs/sandbox.md](docs/sandbox.md).

## Secrets

Secrets are sops-encrypted YAML files using age keys. aide handles the full lifecycle without requiring the `sops` CLI at runtime.

```bash
aide secrets create personal --age-key age1abc...   # Create (opens $EDITOR)
aide secrets edit personal                           # Decrypt, edit, re-encrypt
```

Secrets decrypt in-process at launch and never exist as plaintext on disk. See [docs/secrets.md](docs/secrets.md).

## Reproducibility

**Personal setup** tracked in git:

```bash
cd ~/.config/aide
git init && git add -A && git commit -m "aide config"
```

Encrypted secrets are safe to commit. Only holders of the age private key can decrypt.

**Docker / CI:**

```dockerfile
# Requires the agent binary (e.g. claude) to be installed and on PATH.
COPY aide-config/ /root/.config/aide/
ENV SOPS_AGE_KEY=AGE-SECRET-KEY-1...
RUN aide --agent claude -- -p "run tests"
```

## Supported Agents

Claude, Codex, Aider, Goose, Amp, Gemini. Any binary on PATH works as an agent target.

## Development

```bash
nix develop                 # Full dev environment with all tools
make build                  # Build to ./bin/aide
make test                   # Run tests
make lint                   # Run golangci-lint
```

## Documentation

- [Getting Started](docs/getting-started.md)
- [Contexts](docs/contexts.md)
- [Environment Variables](docs/environment.md)
- [Secrets](docs/secrets.md)
- [Sandbox](docs/sandbox.md)
- [Configuration Reference](docs/configuration.md)
- [CLI Reference](docs/cli-reference.md)
- [Deployment](docs/deployment.md)

## License

[MIT](LICENSE)
```

Key changes from current README:
- **New hook**: "Stop babysitting your agent" tagline
- **New problem statement**: Decision fatigue paragraph
- **New before/after blocks**: Three pillars with table format
- **Scenarios section dissolved**: Content absorbed into before/after blocks
- **Multi-context example rotates agents**: work=claude, personal=codex, oss=aider (was claude/claude/codex)
- **Quick start shows codex**: `aide --agent codex` instead of `aide --agent claude`
- **How It Works trimmed**: Focused on sandbox flow, added Landlock mention
- **Everything else preserved**: Install options, config, sandbox table, secrets, reproducibility, dev, docs, license

- [ ] **Step 2: Review the written file**

Read back `README.md` and verify:
1. All 9 sections present in order (hook, problem, before/after, quick start, how it works, config, sandbox, secrets, reproducibility, agents, dev, docs, license)
2. No content from current README was lost
3. Before/after tables render correctly in markdown
4. At least 2 different agents shown in examples
5. No AI hype language

- [ ] **Step 3: Commit**

```bash
git add README.md
/commit rewrite README with decision-fatigue framing and before/after pillars
```
