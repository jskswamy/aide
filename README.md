# aide

Stop babysitting your agent.

One command. Any agent. Sandboxed, reproducible, zero decision fatigue.

---

You planned the work. You know what needs to happen. But instead of letting your agent execute, you're stuck evaluating every file read, every shell command, every network call. That's not autonomy — that's babysitting with extra steps.

aide fixes three things:

### Sandbox — stop choosing between scary and exhausting

Without aide, you either skip all permissions and hope for the best:

```bash
claude --dangerously-skip-permissions  # what could go wrong?
```

Or you click "allow" on every. single. action. File read? Allow. Shell command? Allow. Network call? Allow. Two hundred times a session.

With aide, the agent runs inside OS-native guardrails — no config, no prompts:

```bash
aide    # agent launches sandboxed automatically
```

```
🔧 aide · work (claude)
   📁 github.com/acme/api
   🛡 sandbox: network outbound, code-only
```

Code-only mode. Your agent can read your code, run tests, hit the network — but it physically cannot touch your SSH keys, cloud credentials, or browser data. 20 guards active by default, zero configuration.

**Ready to deploy?** Tell aide what you're doing:

```bash
aide --with docker          # build and push images
aide --with docker k8s      # deploy to your cluster
aide --with docker k8s gcp  # debug cloud infra too
```

```
🔧 aide · work (claude)
   📁 github.com/acme/api
   🛡 sandbox: network outbound
      ✓ docker     ~/.docker/config.json
      ✓ k8s        ~/.kube/config (KUBECONFIG)
      ✓ gcp        ~/.config/gcloud

      ⚠ credentials exposed: GOOGLE_APPLICATION_CREDENTIALS
```

Each capability unlocks exactly what the agent needs — nothing more. Docker gets registry creds. Kubernetes gets kubeconfig. GCP gets gcloud auth. Everything else stays locked.

**Protect what matters:**

```bash
aide cap never-allow ~/.kube/prod-config
aide cap never-allow --env PRODUCTION_DB_PASSWORD
```

Now no capability — not even `k8s` — can ever read your production kubeconfig. The agent sees your dev and staging clusters but production is a hard wall:

```
🔧 aide · work (claude)
   🛡 sandbox: network outbound
      ✓ k8s        ~/.kube/dev-config, ~/.kube/staging-config
      ✗ denied     ~/.kube/prod-config (never-allow)
```

**Make it permanent for a project:**

```yaml
# .aide.yaml in your repo root
capabilities: [docker, k8s, gcp]
```

No flags needed next time — `aide` picks up the capabilities from your config.

**Create your own:**

```bash
aide cap create k8s-dev --extends k8s --deny ~/.kube/prod-config
aide --with k8s-dev docker    # dev clusters only, production blocked
```

12 built-in capabilities: `aws`, `gcp`, `azure`, `docker`, `k8s`, `helm`, `terraform`, `vault`, `ssh`, `npm`, and more. Or define your own.

### Unified UX — one command, any agent

Without aide, every agent is its own world:

```bash
claude                                          # Anthropic API key
CLAUDE_CODE_USE_BEDROCK=1 AWS_PROFILE=work claude  # or Bedrock?
codex --provider anthropic                      # different CLI entirely
aider --model claude-3.5-sonnet                 # yet another interface
```

Different CLIs. Different config formats. Different env vars. Switch agents, rewire everything.

With aide, you configure once and forget:

```bash
cd ~/work/project && aide    # Claude with work Bedrock credentials
cd ~/oss/repo && aide        # Aider with personal Anthropic key
cd ~/scratch && aide         # auto-detects agent on PATH, zero config
```

Same command everywhere. aide resolves the right agent, credentials, and sandbox from your project directory.

### Reproducibility — secrets that don't leak

Without aide:

```bash
# The classic footgun
echo 'ANTHROPIC_API_KEY=sk-ant-...' >> .env
git add -A && git commit -m "update config"  # oops
```

API keys in `.env` files. Wrapper scripts with hardcoded tokens. A new machine means an hour of setup.

With aide, secrets are encrypted at rest and never exist as plaintext on disk:

```bash
aide secrets create personal --age-key age1abc...   # encrypted with your key
```

Config and encrypted secrets live in git. Clone your config on a new machine and you're done. No shared secrets, no plaintext on disk.

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
aide                        # Resolve context and launch the agent (sandboxed)
aide setup                  # Interactive first-time configuration
aide --with k8s docker      # Enable capabilities for this session
aide cap list               # See all available capabilities
```

No config file required. If one agent exists on PATH with its API key in the environment, `aide` launches it sandboxed — zero setup.

## How It Works

1. Run `aide` in any project directory.
2. aide matches the git remote URL and directory path against your config.
3. It resolves the context: agent, credentials, capabilities, and sandbox policy.
4. Secrets decrypt in-process via the sops Go library. Nothing hits disk.
5. Capabilities translate to sandbox rules — each `--with` flag unlocks specific tool access while keeping everything else locked.
6. aide applies the sandbox via macOS Seatbelt and execs the agent inside it. Linux sandbox support (Landlock) is planned.

No config file? aide detects your agent on PATH and launches it directly.

## Capabilities

Capabilities are task-oriented permission bundles. Instead of configuring low-level sandbox rules, you declare what you're doing:

| Capability | What it unlocks |
|------------|----------------|
| `aws` | AWS CLI credentials (`~/.aws/`) |
| `gcp` | Google Cloud credentials (`~/.config/gcloud/`) |
| `azure` | Azure CLI credentials (`~/.azure/`) |
| `docker` | Docker registry credentials (`~/.docker/`) |
| `k8s` | Kubernetes cluster access (`~/.kube/`) |
| `helm` | Helm charts and releases |
| `terraform` | Terraform state and providers |
| `vault` | HashiCorp Vault access |
| `ssh` | SSH keys and agent |
| `npm` | npm/yarn registry credentials |

**Session-scoped** (this launch only):

```bash
aide --with k8s docker
```

**Context-scoped** (always for this project):

```yaml
# ~/.config/aide/config.yaml
contexts:
  work:
    agent: claude
    capabilities: [docker, k8s, gcp]
    secret: work
    env:
      ANTHROPIC_API_KEY: "{{ .secrets.anthropic_api_key }}"
```

**Custom capabilities:**

```bash
aide cap create k8s-dev --extends k8s --deny ~/.kube/prod-config
aide cap show k8s-dev     # see what it grants
aide cap check k8s-dev docker  # preview composition before launching
```

**Global protection:**

```bash
aide cap never-allow ~/.kube/prod-config      # no capability can ever read this
aide cap never-allow --env VAULT_ROOT_TOKEN   # this env var is always stripped
```

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
    capabilities: [docker, k8s, aws]
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

Claude, Copilot, Codex, Aider, Goose, Amp, Gemini. Any binary on PATH works as an agent target.

## Development

```bash
nix develop                 # Full dev environment with all tools
make build                  # Build to ./bin/aide
make test                   # Run tests
make lint                   # Run golangci-lint
```

## Documentation

- [Getting Started](docs/getting-started.md)
- [Capabilities](docs/capabilities.md)
- [Contexts](docs/contexts.md)
- [Environment Variables](docs/environment.md)
- [Secrets](docs/secrets.md)
- [Sandbox](docs/sandbox.md)
- [Configuration Reference](docs/configuration.md)
- [CLI Reference](docs/cli-reference.md)
- [Deployment](docs/deployment.md)

## License

[MIT](LICENSE)
