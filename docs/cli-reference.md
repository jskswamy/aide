# aide CLI Reference

## aide

```
aide [flags] [-- agent-args...]
```

Resolves context, decrypts secrets, and launches a coding agent. Without a
config file, auto-detects agents on PATH and passes arguments through. With a
config file, resolves the matching context and applies environment, secrets, and
sandbox policy.

**Persistent Flags** (available to all subcommands):

| Flag | Default | Description |
|------|---------|-------------|
| `--resolve` | false | Show detailed startup info (agent path, env sources, sandbox) |

**Local Flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--agent <name>` | | Override which agent to launch |
| `--yolo` | false | Skip agent permission checks; sandbox still applies |
| `--no-yolo` | false | Disable yolo mode (overrides config `yolo:` and `--yolo` flag) |
| `--clean-env` | false | Start agent with only essential environment variables |

```
aide -- --no-permissions
```

---

## aide which

```
aide which
```

Shows which context matches the current directory. Accepts the persistent
`--resolve` flag from the root command to also decrypt and display secret keys
and expanded env values.

```
aide which --resolve
```

---

## aide validate

```
aide validate
```

Checks config structure, verifies agent binaries exist on PATH, confirms secret
files exist on disk, and flags context match rules that can never activate.
Exits non-zero if errors are found.

```
aide validate
```

---

## aide init

```
aide init [--force]
```

Creates an initial config file. Detects agents on PATH and prompts for the
primary agent. Optionally sets up a secrets file. With `--force`, backs up the
existing config to `<path>.bak` before overwriting.

| Flag | Description |
|------|-------------|
| `--force` | Overwrite existing config, creating a .bak backup |

```
aide init --force
```

---

## aide setup

```
aide setup
```

Interactive guided wizard for the current directory. Steps through agent
selection, secrets selection or creation, environment variable wiring, and
sandbox policy. If contexts already exist, offers to reuse or inherit from one.

```
aide setup
```

---

## aide use

```
aide use [agent] [flags]
```

Binds the current directory to an agent or context in global config. Without
`--context`, creates or updates a context named after the current directory
basename.

| Flag | Description |
|------|-------------|
| `--match <pattern>` | Glob pattern to match instead of CWD |
| `--context <name>` | Add a match rule to an existing named context |
| `--secret <name>` | Set a secret on the context |
| `--sandbox <profile>` | Set a sandbox profile name (e.g. strict, none) |

```
aide use claude --match "~/work/*" --secret personal
```

---

## aide context

### aide context list

```
aide context list
```

Lists all configured contexts with their agent, secret, match rules, and env var keys.

```
aide context list
```

### aide context add

```
aide context add
```

Interactive prompt to create a new context: name, agent, match rule, and optional secret.

```
aide context add
```

### aide context add-match

```
aide context add-match [--context name]
```

Adds a match rule to a context via interactive prompt. Defaults to the CWD-matched context.

| Flag | Description |
|------|-------------|
| `--context <name>` | Target context (default: CWD-matched) |

```
aide context add-match --context work
```

### aide context set-secret

```
aide context set-secret <secret-name> [--context name]
```

Sets the secret on a context.

| Flag | Description |
|------|-------------|
| `--context <name>` | Target context (default: CWD-matched) |

```
aide context set-secret personal --context work
```

### aide context remove-secret

```
aide context remove-secret [--context name]
```

Removes the secret from a context. Warns if env vars reference secret templates.

| Flag | Description |
|------|-------------|
| `--context <name>` | Target context (default: CWD-matched) |

```
aide context remove-secret --context work
```

### aide context set-default

```
aide context set-default [name]
```

Sets the fallback context used when no match rules apply. Without an argument,
uses the CWD-matched context.

```
aide context set-default personal
```

### aide context rename

```
aide context rename <old-name> <new-name>
```

Renames a context, updating the default if needed.

```
aide context rename myproject acme
```

### aide context remove

```
aide context remove <name>
```

Removes a context. Warns if the context's agent is then unreferenced.

```
aide context remove old-project
```

---

## aide env

### aide env set

```
aide env set KEY [VALUE] [flags]
```

Sets an environment variable on a context. Provide a literal value as the
second argument or use `--from-secret` to generate a template referencing a
secret key. When `--from-secret` has no value, an interactive picker is shown.

| Flag | Description |
|------|-------------|
| `--from-secret [key]` | Template from secret key; omit key for interactive picker |
| `--context <name>` | Target context (default: CWD-matched) |

```
aide env set ANTHROPIC_API_KEY --from-secret api_key --context work
```

### aide env list

```
aide env list [--context name]
```

Lists env vars for a context, annotating each with its source (literal value,
secret key, or template).

| Flag | Description |
|------|-------------|
| `--context <name>` | Target context (default: CWD-matched) |

```
aide env list --context work
```

### aide env remove

```
aide env remove KEY [--context name]
```

Removes an env var from a context.

| Flag | Description |
|------|-------------|
| `--context <name>` | Target context (default: CWD-matched) |

```
aide env remove ANTHROPIC_API_KEY
```

---

## aide secrets

### aide secrets create

```
aide secrets create <name> --age-key <key>
```

Creates a new encrypted secrets file at `~/.config/aide/secrets/<name>.enc.yaml`.

| Flag | Description |
|------|-------------|
| `--age-key <key>` | Age public key for encryption (required) |

```
aide secrets create personal --age-key age1...
```

### aide secrets edit

```
aide secrets edit <name>
```

Opens the secrets file in your editor (decrypts to a temp file, re-encrypts on
save). Shows added and removed keys after editing.

```
aide secrets edit personal
```

### aide secrets keys

```
aide secrets keys <name>
```

Lists key names in an encrypted secrets file without revealing values.

```
aide secrets keys personal
```

### aide secrets list

```
aide secrets list
```

Lists all secrets files with recipients and which contexts reference each file.

```
aide secrets list
```

### aide secrets rotate

```
aide secrets rotate <name> [--add-key key] [--remove-key key]
```

Rotates age recipients for a secrets file. At least one of `--add-key` or
`--remove-key` is required.

| Flag | Description |
|------|-------------|
| `--add-key <key>` | Age public key to add as recipient (repeatable) |
| `--remove-key <key>` | Age public key to remove as recipient (repeatable) |

```
aide secrets rotate personal --add-key age1new... --remove-key age1old...
```

---

## aide sandbox

### aide sandbox show

```
aide sandbox show [--context name]
```

Shows the effective sandbox policy (writable, readable, denied paths; network
mode) for the current or named context.

| Flag | Description |
|------|-------------|
| `--context <name>` | Show policy for a specific context |

```
aide sandbox show --context work
```

### aide sandbox test

```
aide sandbox test [--context name]
```

Generates and prints the platform-specific sandbox profile without launching the
agent. Useful for auditing what the OS sandbox will enforce.

| Flag | Description |
|------|-------------|
| `--context <name>` | Generate profile for a specific context |

```
aide sandbox test
```

### aide sandbox list

```
aide sandbox list
```

Lists all named sandbox profiles defined in config, plus the built-in `default`
profile.

```
aide sandbox list
```

### aide sandbox create

```
aide sandbox create <name> [--from profile]
```

Creates a new named sandbox profile interactively (writable paths, denied paths,
network mode). Optionally inherits from an existing profile.

| Flag | Description |
|------|-------------|
| `--from <profile>` | Base profile to inherit from |

```
aide sandbox create strict --from default
```

### aide sandbox edit

```
aide sandbox edit <name> [flags]
```

Edits an existing named sandbox profile by adding or removing paths and setting
network mode. Cannot edit built-in profiles.

| Flag | Description |
|------|-------------|
| `--add-writable <path>` | Add a writable path (repeatable) |
| `--add-readable <path>` | Add a readable path (repeatable) |
| `--add-denied <path>` | Add a denied path (repeatable) |
| `--remove-writable <path>` | Remove a writable path (repeatable) |
| `--remove-readable <path>` | Remove a readable path (repeatable) |
| `--remove-denied <path>` | Remove a denied path (repeatable) |
| `--network <mode>` | Set network mode (outbound, none, unrestricted) |

```
aide sandbox edit strict --add-denied ~/Downloads --network none
```

### aide sandbox remove

```
aide sandbox remove <name>
```

Removes a named sandbox profile. Warns if any contexts reference it.

```
aide sandbox remove strict
```

### aide sandbox allow

```
aide sandbox allow <path> [--write] [--context name]
```

Adds a path to `readable_extra` (default) or `writable_extra` for a context's
inline sandbox policy.

| Flag | Description |
|------|-------------|
| `--write` | Add to writable_extra instead of readable_extra |
| `--context <name>` | Target context (default: CWD-matched) |

```
aide sandbox allow ~/shared-docs --write
```

### aide sandbox deny

```
aide sandbox deny <path> [--context name]
```

Adds a path to `denied_extra` for a context's inline sandbox policy.

| Flag | Description |
|------|-------------|
| `--context <name>` | Target context (default: CWD-matched) |

```
aide sandbox deny ~/private
```

### aide sandbox network

```
aide sandbox network <mode> [--context name]
```

Sets the network mode for a context's inline sandbox policy. Mode must be one of
`outbound`, `none`, or `unrestricted`.

| Flag | Description |
|------|-------------|
| `--context <name>` | Target context (default: CWD-matched) |

```
aide sandbox network none --context restricted
```

### aide sandbox ports

```
aide sandbox ports <port>... [--context name]
```

Sets the allowed outbound ports for a context's inline sandbox policy (implies
network mode `outbound`).

| Flag | Description |
|------|-------------|
| `--context <name>` | Target context (default: CWD-matched) |

```
aide sandbox ports 443 8080
```

### aide sandbox reset

```
aide sandbox reset [--context name]
```

Resets a context's sandbox configuration to defaults by removing its inline
sandbox ref.

| Flag | Description |
|------|-------------|
| `--context <name>` | Target context (default: CWD-matched) |

```
aide sandbox reset
```

---

## aide agents

### aide agents list

```
aide agents list
```

Lists configured agents (with binary path and which contexts use them) and
agents detected on PATH that are not yet configured.

```
aide agents list
```

### aide agents add

```
aide agents add <name> [--binary path]
```

Registers a new agent in config. If `--binary` is omitted, the binary name
defaults to the agent name.

| Flag | Description |
|------|-------------|
| `--binary <path>` | Binary name or path (defaults to agent name) |

```
aide agents add my-agent --binary /usr/local/bin/my-agent
```

### aide agents edit

```
aide agents edit <name> --binary <path>
```

Updates an agent's binary path. `--binary` is required.

| Flag | Description |
|------|-------------|
| `--binary <path>` | New binary name or path (required) |

```
aide agents edit claude --binary /opt/homebrew/bin/claude
```

### aide agents remove

```
aide agents remove <name>
```

Removes an agent from config. Warns if any contexts still reference it.

```
aide agents remove old-agent
```

---

## aide config

### aide config show

```
aide config show
```

Prints the contents of the config file with its path as a header.

```
aide config show
```

### aide config edit

```
aide config edit
```

Opens the config file in `$EDITOR` (falls back to `$VISUAL`, then `vi`).
Validates the config after saving.

```
aide config edit
```

---

## aide completion

Generates shell completion scripts. Supported shells: bash, zsh, fish, powershell.

```
aide completion bash|zsh|fish|powershell
```

Outputs shell completion script for the specified shell. Source the output or
follow the instructions printed with the script.

```
aide completion fish | source
```
