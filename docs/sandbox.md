# Sandbox

## Why Sandbox

Coding agents read files, run commands, and install packages. Without a defined security boundary, every action that touches the filesystem or network triggers a permission prompt. Users press "yes" repeatedly until they approve something dangerous.

aide defines the security boundary once, upfront. The agent runs freely inside it. No per-action prompts, no approval fatigue.

## On by Default

If no `sandbox:` block appears in your config, aide applies a default policy automatically.

| Category         | Default                                      |
|------------------|----------------------------------------------|
| Writable         | Project root, runtime dir, temp dirs         |
| Readable         | Home directory, system binaries              |
| Denied           | `~/.ssh/id_*`, `~/.aws/credentials`, `~/.azure`, `~/.config/gcloud`, `~/.config/aide/secrets`, browser data |
| Network          | Outbound allowed                             |
| Subprocesses     | Allowed                                      |

## Agent Config Directories

aide auto-detects known agent config directories and adds them to the writable list so agents can store state, cache, and settings.

| Agent  | Env Override        | Default Path                          |
|--------|---------------------|---------------------------------------|
| Claude | `CLAUDE_CONFIG_DIR` | `~/.claude`                           |
| Codex  | `CODEX_HOME`        | `~/.codex`                            |
| Aider  | (none)              | `~/.aider`                            |
| Goose  | `GOOSE_PATH_ROOT`   | `~/.config/goose`, `~/.local/share/goose`, `~/.local/state/goose` |
| Amp    | `AMP_HOME`          | `~/.amp`, `~/.config/amp`             |
| Gemini | `GEMINI_HOME`       | `~/.gemini`                           |

## Customizing Per-Context

Define an inline sandbox policy inside a named context block in your config file.

```yaml
contexts:
  myproject:
    sandbox:
      writable:
        - "{{ .project_root }}"
        - "{{ .runtime_dir }}"
      readable:
        - /usr/local/share/certs
      denied:
        - ~/.gnupg
      network: outbound
      allow_subprocess: true
      clean_env: false
```

**Template variables**

| Variable             | Resolves to                              |
|----------------------|------------------------------------------|
| `{{ .project_root }}` | Absolute path of the project directory  |
| `{{ .runtime_dir }}`  | Agent runtime/state directory           |

**`_extra` suffixes**

Use `_extra` suffixes to extend the default policy rather than replace it. aide merges the extra list with the defaults.

```yaml
sandbox:
  writable_extra:
    - /mnt/shared/data
  readable_extra:
    - /etc/myapp
  denied_extra:
    - ~/.netrc
```

## Quick CLI Adjustments

All commands accept `--context <name>` to target a specific context.

```sh
# Add a path as readable
aide sandbox allow <path>

# Add a path as writable
aide sandbox allow --write <path>

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

CLI adjustments write to the `_extra` fields of the relevant context, leaving the base policy intact.

## Named Profiles

Named profiles let you define a sandbox policy once and reference it by name across multiple contexts.

**Manage profiles**

```sh
aide sandbox create <name>
aide sandbox edit <name> \
  --add-writable /data \
  --add-readable /etc/ssl \
  --add-denied ~/.gnupg \
  --remove-writable /tmp \
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

aide loads the `strict` profile and applies it to the `secure` context. Inline `_extra` fields still work alongside a named profile.

## Disabling Sandbox

Set `sandbox: false` in your config to disable sandboxing entirely for that context. The agent runs with full filesystem and network access, subject only to OS-level user permissions.

```yaml
contexts:
  local-dev:
    sandbox: false
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
    "github.com/jskswamy/aide/pkg/seatbelt/modules"
)

profile := seatbelt.New(homeDir).Use(
    modules.Base(),
    modules.SystemRuntime(),
    modules.Network(modules.NetworkOpen),
    modules.Filesystem(modules.FilesystemConfig{
        Writable: []string{projectRoot, tmpDir},
        Denied:   []string{"~/.ssh/id_*"},
    }),
    modules.NodeToolchain(),
    modules.ClaudeAgent(),
)
sbText, err := profile.Render()
```

Available modules: `Base`, `SystemRuntime`, `Network`, `Filesystem`, `NodeToolchain`, `NixToolchain`, `GitIntegration`, `KeychainIntegration`, `ClaudeAgent`.

## Attribution

The Seatbelt rules in `pkg/seatbelt` port the shell scripts from [agent-safehouse](https://github.com/eugene1g/agent-safehouse) as a Go library by Eugene Goldin. agent-safehouse provides composable Seatbelt policy profiles for AI coding agents and has validated profiles for 14 agents. The Go port makes these rules available as a library for Go applications building sandbox support.
