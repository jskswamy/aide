# aide

One command. Right agent. Right credentials. Every project.

```bash
cd ~/work/project && aide    # Claude with work Bedrock credentials
cd ~/oss/repo && aide        # Codex with personal OpenAI key
cd ~/scratch && aide         # Auto-detects agent on PATH, zero config
```

## Scenarios

**Personal vs work credentials.**
You use Claude with AWS Bedrock at work and a personal Anthropic API key at home. Today that means juggling `CLAUDE_CONFIG_DIR`, wrapper scripts, or separate shell profiles. aide resolves the right credentials from the project directory. You run `aide` and get the correct key every time.

**Agent access to sensitive files.**
Coding agents run with your full user permissions. They can read `~/.ssh`, `~/.aws`, browser cookies. aide applies an OS-native sandbox before exec, constraining filesystem and network access to a declared policy. No per-action approval prompts.

**Team shares API keys.**
Someone commits a `.env`. Encrypted storage with per-person age keys prevents this. Each team member holds their own private key. Secrets decrypt in-process at launch and never exist as plaintext on disk.

## Quick Start

```bash
# Install from source
go install github.com/jskswamy/aide/cmd/aide@latest

# Or build locally
git clone https://github.com/jskswamy/aide.git
cd aide && make build   # Binary at ./bin/aide
```

Three commands to know:

```bash
aide                    # Resolve context and launch the agent
aide --agent claude     # Override agent selection
aide setup              # Interactive first-time configuration
```

No config file required. If one agent exists on PATH with its API key in the environment, `aide` launches it directly.

## How It Works

1. Run `aide` in a project directory.
2. aide checks the git remote URL and directory path against your config.
3. It finds the matching context: agent, credentials, and sandbox policy.
4. Secrets decrypt in-process via the sops Go library. Nothing hits disk.
5. aide applies an OS-native sandbox (macOS Seatbelt, Linux Landlock or bubblewrap).
6. aide execs the agent with the resolved environment.

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

Contexts match by git remote URL patterns and directory path globs. The most specific match wins. `default_context` is the fallback. See [docs/configuration.md](docs/configuration.md) for the full reference.

## Sandbox

Agents run inside an OS-native sandbox by default. You define the boundary once; the agent operates freely within it. No per-action permission prompts.

| Access | Paths |
|--------|-------|
| Writable | Project root, runtime dir, temp dirs |
| Readable | Home directory, system binaries |
| Denied | SSH private keys, cloud credentials, browser data |
| Network | Outbound allowed |
| Subprocesses | Allowed |

Customize per-context or disable entirely. See [docs/sandbox.md](docs/sandbox.md).

## Secrets

Secrets are sops-encrypted YAML files using age keys. aide handles the full lifecycle without requiring the `sops` CLI at runtime.

```bash
aide secrets create personal --age-key age1abc...   # Create (opens $EDITOR)
aide secrets edit personal                           # Decrypt, edit, re-encrypt
aide secrets rotate personal --add-key age1def...    # Add a recipient
```

Multiple recipients per file support laptop, desktop, YubiKey, and CI access without sharing private keys. See [docs/secrets.md](docs/secrets.md).

## Reproducibility

**Personal setup** tracked in git:

```bash
cd ~/.config/aide
git init && git add -A && git commit -m "aide config"
```

Encrypted secrets are safe to commit. Only holders of the age private key can decrypt.

**Team shared config:**

```bash
git clone git@github.com:team/aide-config.git ~/.config/aide
aide secrets rotate work --add-key $(age-keygen -y key.txt)
```

**Docker / CI:**

```dockerfile
COPY aide-config/ /root/.config/aide/
ENV SOPS_AGE_KEY=AGE-SECRET-KEY-1...
RUN aide --agent claude -- -p "run tests"
```

## Supported Agents

Claude, Codex, Aider, Goose, Amp. Any binary on PATH works as an agent target.

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
