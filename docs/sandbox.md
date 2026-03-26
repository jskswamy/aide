# Sandbox

## The Short Version

```sh
aide status
```

This shows what capabilities are active, which sandbox guards are enforcing them, and whether the sandbox is enabled. For most users, that is all you need.

## Capabilities and Sandbox

aide separates **what** the agent can access from **how** that access is enforced:

- **Capabilities** handle WHAT the agent can access — network, filesystem paths, cloud credentials, toolchains, and more. You configure capabilities in your context config and aide translates them into the right set of guards. See [docs/capabilities.md](capabilities.md) for the full capability reference.

- **The sandbox** handles HOW it is enforced — on macOS, aide generates a Seatbelt profile at launch that locks the agent process to exactly the permissions its capabilities describe.

A practical example:

```yaml
contexts:
  work:
    capabilities:
      - docker
      - k8s
      - aws
```

aide reads those capabilities and produces a sandbox profile that grants Docker config access, kubeconfig reads, and AWS credential reads — nothing more.

## On by Default

If no `sandbox:` block appears in your config, aide applies a default policy automatically. With 10 guards active out of the box, the sandbox covers filesystem, network, credentials, cloud providers, toolchains, and common development tools.

Run `aide status` to see what is active for the current context.

| Category     | Default                  |
|--------------|--------------------------|
| Network      | Outbound allowed         |
| Subprocesses | Allowed                  |

## Agent Config Directories

aide auto-detects known agent config directories and adds them to the writable list so agents can store state, cache, and settings.

| Agent  | Env Override        | Default Path                          |
|--------|---------------------|---------------------------------------|
| Claude | `CLAUDE_CONFIG_DIR` | `~/.claude`, `~/.config/claude`, `~/Library/Application Support/Claude` |
| Codex  | `CODEX_HOME`        | `~/.codex`                            |
| Aider  | (none)              | `~/.aider`                            |
| Goose  | `GOOSE_PATH_ROOT`   | `~/.config/goose`, `~/.local/share/goose`, `~/.local/state/goose` |
| Amp    | `AMP_HOME`          | `~/.amp`, `~/.config/amp`             |
| Gemini | `GEMINI_HOME`       | `~/.gemini`                           |

## Customizing Per-Context

The preferred way to customize is through capabilities in your context config. For fine-grained control, you can also work with guards directly:

```yaml
contexts:
  work:
    sandbox:
      denied_extra: ["~/sensitive-data"]  # deny additional paths
      unguard: [dev-credentials]          # disable default guards
```

### Guard configuration fields

| Field | Purpose |
|-------|---------|
| `guards` | Override: use ONLY these guards (plus always guards) |
| `guards_extra` | Extend: add guards to the default set |
| `unguard` | Disable: remove guards from the active set |
| `denied` | Explicit path denies (for one-off paths) |
| `denied_extra` | Extend explicit denies without replacing defaults |

**Template variables**

| Variable             | Resolves to                              |
|----------------------|------------------------------------------|
| `{{ .project_root }}` | Absolute path of the project directory  |
| `{{ .runtime_dir }}`  | Agent runtime/state directory           |

**Network configuration**

Network can be set as a simple string or a structured block with port filtering:

```yaml
sandbox:
  network: outbound

# or with port filtering:
sandbox:
  network:
    mode: outbound
    allow_ports: [443, 80]
    deny_ports: [22]
```

## Named Profiles

Named profiles let you define a sandbox policy once and reference it by name across multiple contexts.

**Manage profiles**

```sh
aide sandbox create <name>
aide sandbox edit <name> \
  --add-denied ~/.gnupg \
  --network outbound
aide sandbox remove <name>
aide sandbox list
```

**Reference a profile in config**

```yaml
contexts:
  secure:
    sandbox: strict
```

aide loads the `strict` profile and applies it to the `secure` context.

## Yolo Mode and Sandbox

`yolo: true` in config or `--yolo` on the CLI disables the agent's own permission checks (e.g. Claude's `--dangerously-skip-permissions`). The OS sandbox remains fully active. Yolo mode does not weaken the sandbox; it only removes the agent's interactive approval prompts.

Use `--no-yolo` to override config-based yolo and restore agent permission checks.

## Disabling Sandbox

Set `sandbox: false` in your config to disable sandboxing entirely for that context. The agent runs with full filesystem and network access, subject only to OS-level user permissions.

```yaml
contexts:
  local-dev:
    sandbox: false
```

## Trust Gate

When a `.aide.yaml` file exists in a project, aide checks its trust status before applying it. This prevents untrusted project configs from modifying your sandbox policy.

| Status | Behavior |
|--------|----------|
| Trusted | Config applies normally |
| Untrusted | Config is shown but not applied; a warning prints |
| Denied | Config is silently skipped |

Manage trust with:

```sh
aide trust      # trust the .aide.yaml in the current directory
aide deny       # permanently block it
aide untrust    # remove trust without blocking
```

To launch without applying `.aide.yaml` regardless of trust status:

```sh
aide --ignore-project-config
```

When aide itself modifies `.aide.yaml` (via `aide cap enable`, `aide sandbox allow`, etc.), it auto-re-trusts the file if the previous version was trusted.

## Under the Hood

Capabilities are the user-facing concept, but under the hood aide translates them into **guards** — small, composable policy modules that each protect a specific resource. Guards are the low-level mechanism that generates the actual sandbox profile.

### Guard inventory

aide ships with 10 built-in guards across two tiers:

**Always guards** (7) — cannot be disabled, form the baseline policy:

`base`, `system-runtime`, `network`, `filesystem`, `keychain`, `node-toolchain`, `nix-toolchain`

**Default guards** (3) — active out of the box, can be disabled with `unguard`:

`project-secrets`, `dev-credentials`, `aide-secrets`

### Guard commands

These commands are available for power users who want to inspect or adjust guards directly:

```bash
aide sandbox guards                    # List all guards with status
aide sandbox guard dev-credentials     # Enable a guard
aide sandbox unguard dev-credentials          # Disable a guard
aide sandbox types                     # List guard types
aide sandbox test                      # Preview generated sandbox profile
```

All commands accept `--context <name>` to target a specific context.

### Quick CLI adjustments

```sh
# Add a path to the deny list
aide sandbox deny <path>

# Restrict outbound to specific ports
aide sandbox ports 443 8080

# Set network mode
aide sandbox network outbound
aide sandbox network none
aide sandbox network unrestricted

# Revert sandbox config for the context to defaults
aide sandbox reset
```

## Platform Details

| Platform           | Mechanism                              | Notes                                         |
|--------------------|----------------------------------------|-----------------------------------------------|
| macOS              | `sandbox-exec` (Seatbelt)             | Generates a `.sb` profile dynamically at run time |
| Linux              | —                                      | Planned, not yet implemented                  |

Currently only macOS is supported. Linux sandbox support (e.g. Landlock, bubblewrap) is planned but not yet implemented.

## Debugging

Inspect the effective policy or validate the generated platform profile for a context.

```sh
# Print the effective sandbox policy as aide resolves it
aide sandbox show
aide sandbox show --context myproject

# Generate and display the platform-specific sandbox profile
aide sandbox test
aide sandbox test --context myproject
```

`aide sandbox show` prints the merged policy (defaults + profile + inline + extra fields). `aide sandbox test` outputs the raw Seatbelt `.sb` profile on macOS, which is useful for confirming that paths resolve correctly before running an agent.

## Using the Seatbelt Library

The macOS sandbox implementation lives in `pkg/seatbelt`, a standalone Go library. You can import it into your own projects to build Seatbelt profiles without using aide's CLI.

```go
import (
    "github.com/jskswamy/aide/pkg/seatbelt"
    "github.com/jskswamy/aide/pkg/seatbelt/guards"
)

// Get all default guards
activeGuards := guards.ResolveActiveGuards(guards.DefaultGuardNames())

p := seatbelt.New(homeDir).
    WithContext(func(c *seatbelt.Context) {
        c.ProjectRoot = projectRoot
        c.GOOS = runtime.GOOS
        c.Network = "outbound"
    })

for _, g := range activeGuards {
    p.Use(g)
}

profile, err := p.Render()
```

Available guard constructors: `guards.AllGuards()` returns all registered guards. Individual constructors follow the pattern `guards.BaseGuard()`, `guards.DevCredentialsGuard()`, `guards.ProjectSecretsGuard()`, etc.

## Attribution

The Seatbelt rules in `pkg/seatbelt` port the shell scripts from [agent-safehouse](https://github.com/eugene1g/agent-safehouse) as a Go library by Eugene Goldin. agent-safehouse provides composable Seatbelt policy profiles for AI coding agents and has validated profiles for 14 agents. The Go port makes these rules available as a library for Go applications that embed sandbox enforcement.
