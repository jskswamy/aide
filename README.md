# aide

[![CI](https://github.com/jskswamy/aide/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/jskswamy/aide/actions/workflows/ci.yml)
[![Security](https://github.com/jskswamy/aide/actions/workflows/security.yml/badge.svg?branch=main)](https://github.com/jskswamy/aide/actions/workflows/security.yml)
[![Release](https://img.shields.io/github/v/release/jskswamy/aide)](https://github.com/jskswamy/aide/releases/latest)

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

Code-only mode. Your agent can read your code, run tests, hit the network — but it physically cannot touch your SSH keys, cloud credentials, or browser data. 10 guards active by default, zero configuration.

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

The first time aide encounters a `.aide.yaml`, it shows the contents and asks you to trust it:

```bash
aide trust    # approve the project config
aide deny     # block it permanently
```

**Create your own:**

```bash
aide cap create k8s-dev --extends k8s --deny ~/.kube/prod-config
aide --with k8s-dev docker    # dev clusters only, production blocked
```

19 built-in capabilities: `aws`, `gcp`, `azure`, `docker`, `k8s`, `helm`, `terraform`, `vault`, `ssh`, `npm`, `go`, `rust`, `python`, `ruby`, `java`, `github`, `gpg`, and more. Or define your own.

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
# Install latest release (macOS and Linux)
curl -sSfL https://raw.githubusercontent.com/jskswamy/aide/main/install.sh | sh

# Install to a specific directory
curl -sSfL https://raw.githubusercontent.com/jskswamy/aide/main/install.sh | sudo INSTALL_DIR=/usr/local/bin sh

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

### Passing args to your agent

Everything after `--` is forwarded directly to the agent:

```bash
# Add an MCP server through aide's sandbox
aide -- mcp add --transport http 1mcp "http://127.0.0.1:3050/mcp" --scope user

# Run a one-shot prompt
aide -- -p "fix the failing tests and commit"

# Start with a specific model
aide -- --model sonnet

# Resume the last conversation
aide -- --resume

# Combine with aide flags
aide --with docker -- -p "build and push the image"
```

This works with any agent — aide resolves context and sandbox, then execs the agent with your args appended.

## How It Works

1. Run `aide` in any project directory.
2. aide matches the git remote URL and directory path against your config.
3. It resolves the context: agent, credentials, capabilities, and sandbox policy.
4. Secrets decrypt in-process via the sops Go library. Nothing hits disk.
5. Capabilities translate to sandbox rules — each `--with` flag unlocks specific tool access while keeping everything else locked.
6. aide applies the sandbox via the platform-native enforcer — macOS Seatbelt or Linux Landlock (kernel ≥ 5.13, with full port enforcement on kernel ≥ 6.7) — and execs the agent inside it. When Landlock is unavailable, bubblewrap provides filesystem isolation. See [Supported Linux tier](docs/sandbox.md#supported-linux-tier-minimum-system-requirements) for details.

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
| `go` | Go toolchain (`~/go/`) |
| `rust` | Rust toolchain (`~/.cargo/`, `~/.rustup/`) |
| `python` | Python toolchain (`~/.pyenv/`) |
| `ruby` | Ruby toolchain (`~/.rbenv/`) |
| `java` | Java/JVM toolchain (`~/.sdkman/`, `~/.gradle/`, `~/.m2/`) |
| `github` | GitHub CLI credentials (`~/.config/gh/`) |
| `gpg` | GPG keys and signing (`~/.gnupg/`) |

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

**Need something not in the list?** Create your own in one command:

```bash
# Grant access to your Bazel cache
aide cap create bazel --writable ~/.cache/bazel --env-allow BAZEL_HOME

# Extend a built-in with project-specific restrictions
aide cap create k8s-dev --extends k8s --deny ~/.kube/prod-config

# Bundle capabilities for a workflow
aide cap create my-deploy --combines docker,k8s,aws
```

Custom capabilities work the same as built-ins. Use them with `--with`, persist them in `.aide.yaml`, or share them across projects via your global config:

```yaml
# ~/.config/aide/config.yaml
capabilities:
  bazel:
    writable: ["~/.cache/bazel"]
    env_allow: [BAZEL_HOME]

# .aide.yaml in a repo (shared with the team)
capabilities: [docker, k8s, bazel]
```

```bash
aide cap show bazel           # inspect what it grants
aide cap check bazel docker   # preview composition before launching
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

## Provisioning — stop reinstalling your plugins on every machine

If you've ever set up a new laptop and spent an hour running
`claude plugin install <foo>` ten times in a row, only to realise
two weeks later that you forgot the `commit-tools` plugin on your
work machine and that's why every commit message looks different
from your personal machine — this section is for you.

The same problem shows up across teammates. Onboarding a new
engineer to the project means writing a setup README that lists
"please install these plugins, then add this MCP server" and hoping
they read past step 3. Six months later, half the team is on a
different plugin set than the other half and nobody knows when it
diverged.

Declare it once in `config.yaml` and reconcile with one command:

```yaml
plugins:
  jskswamy/claude-plugins: [commit-tools, craft, refactor]

contexts:
  work:
    agent: claude
    profile: work          # → CLAUDE_CONFIG_DIR=~/.claude-work
```

```bash
aide sync --plan           # preview the diff
aide sync                  # apply (plugins land in ~/.claude-work/)
aide plugin list           # declared / installed / managed view
aide adopt                 # bring hand-installed items under management
```

The state file at `~/.local/state/aide/managed.json` is per-context,
so syncing your `work` context never disturbs your `personal` plugin
set. `aide which` shows a drift banner whenever your config drifts
ahead of what's installed, so the gap doesn't go unnoticed.

### The other half: profiles

The reason you can declare different plugin sets for different
projects is that each agent context can carry a `profile:` name.
Without it, every claude invocation reads from one shared
`~/.claude/` directory — your work plugins and your personal plugins
fight for the same slot, your client A's session history shows up
when you're working on client B's repo, and your "experimental"
MCP servers leak into the project where you're supposed to be on
your best behaviour.

`profile: work` tells aide's claude driver to set
`CLAUDE_CONFIG_DIR=~/.claude-work` everywhere: at launch, during
`aide sync`, during `aide adopt`, during plugin/MCP list. The
agent reads from and writes to that directory and that directory
only. Your personal claude state in `~/.claude/` stays untouched.
Switch context and it's a different dir entirely.

The same idea works for gemini (`GEMINI_HOME`), codex
(`CODEX_HOME`), copilot (`COPILOT_HOME`) — different env-var name,
same shape from your point of view. You write `profile: <name>`,
aide handles the per-agent env quirks.

See [docs/provisioning.md](docs/provisioning.md) for the full
reference.

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

## Diagnosing a failed run

When the agent exits with a cryptic message (or with no message at all), re-run aide with `--diagnose`:

```bash
aide --diagnose
```

aide will:

1. Run the agent normally with stdin/stdout passthrough.
2. Capture the agent's stderr (last 200 lines / 64 KB by default).
3. On exit, print a short summary and write a full markdown report to `~/.cache/aide/diagnose/<timestamp>-<id>.md` (or `$XDG_CACHE_HOME/aide/diagnose/...` if set).

The report is **redacted** — no secret values, no hostnames — and is suitable to paste into a GitHub issue.

For sandbox-related failures on macOS, add `--diagnose-trace` to additionally capture sandbox-deny events from `log show`:

```bash
aide --diagnose-trace
```

### Tweaking the stderr buffer

Two env vars override the defaults for one run:

```bash
AIDE_DIAGNOSE_STDERR_LINES=2000 AIDE_DIAGNOSE_STDERR_BYTES=524288 aide --diagnose
```

Whichever limit is reached first wins.

### Note

`--diagnose` switches the exec strategy from `syscall.Exec` (process replacement) to fork+exec so aide can stay alive to gather post-mortem data. Default runs are unaffected. `--diagnose-trace` is macOS-only in v1.

## Supported Agents

Aider, Amp, Claude, Codex, Copilot, Cursor, Gemini, Goose. Any binary on PATH works as an agent target.

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
- [Provisioning](docs/provisioning.md)
- [Configuration Reference](docs/configuration.md)
- [CLI Reference](docs/cli-reference.md)
- [Deployment](docs/deployment.md)

## Planned

### Network filtering

Allow or deny network access by domain, port, and MIME type. Today the sandbox controls network at the transport level (outbound yes/no, port filtering). Planned: domain-level rules so an agent can reach `github.com` but not arbitrary hosts, and MIME-type filtering to block binary downloads while allowing API calls.

### Command deny list

Block dangerous commands by name — `rm`, `sudo`, `chmod`, `kill`, etc. Instead of requiring users to know the binary path, aide resolves each command name through `$PATH` at sandbox build time and denies `process-exec` on the resolved binaries. One line in config:

```yaml
sandbox:
  deny_commands: [rm, sudo, chmod, kill, reboot]
```

## Contributors

Thanks to the people who've contributed to aide:

- Selvakumar Natesan ([@selvakn](https://github.com/selvakn)) — Linux Landlock/seccomp sandbox

See the full list on the [contributors graph](https://github.com/jskswamy/aide/graphs/contributors).

## License

[MIT](LICENSE)
