# aide CLI Reference

## aide

```
aide [flags] [-- agent-args...]
```

Resolves profile, decrypts secrets, and launches a coding agent. Without a
config file, auto-detects agents on PATH and passes arguments through. With a
config file, resolves the matching profile and applies environment, secrets, and
sandbox policy.

**Persistent Flags** (available to all subcommands):

| Flag | Default | Description |
|------|---------|-------------|
| `--resolve` | false | Show detailed startup info (agent path, env sources, sandbox) |
| `--format` | human | Output format for read/query commands: `human` or `json`. Also settable via `$AIDE_FORMAT`. |

**Local Flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--agent <name>` | | Override which agent (harness) to launch, for this run only |
| `--profile <name>` | | Use this profile instead of the one matched for the directory |
| `--credential <name>` | | Inject this credential binding (repeatable); replaces the saved and default selection for this run. `--credential=` selects none |
| `--auth <mode>` | api | `api` injects the selected credentials; `login` injects none so the harness uses its native login |
| `--with <caps>` | | Activate capabilities for this session (e.g., `--with k8s,docker`) |
| `--without <caps>` | | Disable profile capabilities for this session |
| `--auto-approve` | false | Run agent without permission checks (replaces `--yolo`) |
| `--no-auto-approve` | false | Override config: require permission checks |
| `--yolo` | false | Alias for `--auto-approve` (kept for backwards compatibility) |
| `--no-yolo` | false | Alias for `--no-auto-approve` (kept for backwards compatibility) |
| `--clean-env` | false | Start agent with only essential environment variables |
| `--ignore-project-config` | false | Launch without applying .aide.yaml |
| `--unrestricted-network`, `-N` | false | Allow unrestricted network access, ignoring config port rules |
| `--diagnose` | false | Wrap the run and write a redacted post-mortem on exit |
| `--diagnose-trace` | false | Implies `--diagnose`; also capture macOS sandbox denials (Darwin only) |
| `--variant <cap=variant>` | | Pin variants for capabilities in `--with` (repeatable; must match a `--with` capability) |
| `--yes` | false | Auto-approve variant consent prompts (non-interactive workflows) |

```
aide -- --no-permissions
```

One-time flags never change saved preferences. Precedence:

- Profile: `--profile`, else match rules, else `default_profile`.
  `.aide.yaml` `profile:` never selects a profile; a mismatch prints a
  note suggesting `--profile`.
- Harness: `--agent`, else the repository preference saved with
  `aide project set`, else a trusted `.aide.yaml` `agent:`, else the
  profile's `agent:`.
- Credentials: `--credential` / `--auth login`, else the saved
  selection, else `default_credentials:`.

---

## aide which

```
aide which
```

Shows which profile matches the current directory. Accepts the persistent
`--resolve` flag from the root command to also decrypt and display secret keys
and expanded env values.

```
aide which --resolve
```

Pass `--agent <name>` to report what `aide --agent <name>` would launch:
that agent, and the profile directory it gets when the profile sets
`profile:`.

```
aide which --agent pi
```

---

## aide status

```
aide status
```

Shows current profile, active capabilities, sandbox state, and auto-approve
status for the working directory.

```
aide status
```

`--agent <name>` works as for `aide which`.

`aide which` and `aide status` also accept `--profile`, `--credential`
and `--auth`, and report where each choice came from
(`profile_source`, `harness_source`, `credential_source` and
`credential_selection` in `--format json`).

---

## aide project

User-local preferences for the current repository, keyed by profile
and the normalized `origin` remote, stored in
`~/.local/state/aide/preferences.json`. Worktrees and clones of the
same repository share them under the same profile. They hold names
only, never secret values, and never touch `.aide.yaml` or
`config.yaml`. In a directory without an `origin` remote, `set` and
`clear` refuse; launches use the profile defaults.

```
aide project set [--profile <name>] [--agent <harness>] [--credential <name>]... [--no-credentials]
aide project clear [--profile <name>] [--agent] [--credential]
aide project clear --reset
aide project show [--profile <name>] [--format json]
```

- `set` changes only the fields it names and checks them the way a
  launch would (harness known and supported by the profile's config
  dir, credentials defined in the profile and allowed for the
  harness). `--no-credentials` saves an explicit empty selection, for
  native-login launches.
- `clear` with `--agent` or `--credential` clears that field; with
  neither, both. Cleared fields fall back to `.aide.yaml` and the
  profile defaults.
- `clear --reset` moves the whole preferences file aside to
  `<file>.corrupt-<timestamp>` (nothing is deleted) and starts empty;
  use it when aide reports the file unreadable.
- `show --format json` prints `profile`, `repository`, `harness`,
  `credentials` (`null` when unset, `[]` for an explicit empty
  selection) and `file`.

---

## aide prompt

```
aide prompt [--starship-config]
```

Output a compact profile line for shell prompt integration.

**Flags:**
- `--starship-config`: Print a ready-to-use `starship.toml` snippet to stdout

**Output format:**
```
profile [profile-icon] [agent-icon] [status]
```

Status indicators:
- `🛡`: sandbox is active
- `⚠`: `.aide.yaml` is present but untrusted (run `aide trust`)
- `🚫`: `.aide.yaml` has been denied

**Exits 1** with no output when no profile matches the current directory. Starship hides the module automatically on non-zero exit.

**Starship setup:**
```bash
aide prompt --starship-config >> ~/.config/starship.toml
```

---

## aide validate

```
aide validate
```

Checks config structure, verifies agent binaries exist on PATH, confirms secret
files exist on disk, and flags profile match rules that can never activate.
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
sandbox policy. If profiles already exist, offers to reuse or inherit from one.

```
aide setup
```

---

## aide use

```
aide use [agent] [flags]
```

Binds the current directory to an agent or profile in global config. Without
`--profile`, creates or updates a profile named after the current directory
basename.

| Flag | Description |
|------|-------------|
| `--match <pattern>` | Glob pattern to match instead of CWD |
| `--profile <name>` | Add a match rule to an existing named profile |
| `--secret <name>` | Set a secret on the profile |
| `--sandbox <profile>` | Set a sandbox profile name (e.g. strict, none) |

```
aide use claude --match "~/work/*" --secret personal
```

---

## aide cap

Manage capabilities -- reusable bundles of sandbox paths, environment
allowlists, and deny rules that can be composed and activated per session.

### aide cap list

```
aide cap list [flags]
```

Lists all capabilities (built-in and user-defined).

| Flag | Description |
|------|-------------|
| `--profile <name>` | Show enabled/disabled status for a specific profile instead of the current one |

```
aide cap list
```

### aide cap show

```
aide cap show <name>
```

Shows capability details with resolved inheritance.

```
aide cap show k8s
```

### aide cap create

```
aide cap create <name> [flags]
```

Creates a custom capability.

| Flag | Description |
|------|-------------|
| `--extends <cap>` | Inherit from an existing capability |
| `--combines <caps>` | Compose multiple capabilities together |
| `--readable <path>` | Add a readable path (repeatable) |
| `--writable <path>` | Add a writable path (repeatable) |
| `--deny <path>` | Add a denied path (repeatable) |
| `--env-allow <var>` | Allow an environment variable (repeatable) |
| `--description <text>` | Human-readable description |

```
aide cap create my-k8s --extends k8s --writable ~/.kube --description "K8s with local kubeconfig"
```

### aide cap edit

```
aide cap edit <name> [flags]
```

Modifies a user-defined capability. Cannot edit built-in capabilities.

| Flag | Description |
|------|-------------|
| `--add-readable <path>` | Add a readable path (repeatable) |
| `--add-writable <path>` | Add a writable path (repeatable) |
| `--add-deny <path>` | Add a denied path (repeatable) |
| `--remove-deny <path>` | Remove a denied path (repeatable) |
| `--add-env-allow <var>` | Allow an environment variable (repeatable) |
| `--remove-env-allow <var>` | Remove an environment variable allowance (repeatable) |
| `--description <text>` | Update description |

```
aide cap edit my-k8s --add-writable /tmp/helm-cache --description "K8s with Helm"
```

### aide cap enable

```
aide cap enable <capability>[,capability...] [flags]
```

Adds one or more capabilities to the current profile (or globally with `--global`).

| Flag | Description |
|------|-------------|
| `--global` | Apply to user-level config instead of the project |
| `--profile <name>` | Target a specific profile name (requires `--global`) |
| `--main` | In a linked git worktree, edit the main checkout's `.aide.yaml` instead of the worktree's own |

```
aide cap enable docker
aide cap enable docker,k8s
```

### aide cap disable

```
aide cap disable <capability>[,capability...] [flags]
```

Removes one or more capabilities from the current profile (or globally with `--global`).

| Flag | Description |
|------|-------------|
| `--global` | Apply to user-level config instead of the project |
| `--profile <name>` | Target a specific profile name (requires `--global`) |
| `--main` | In a linked git worktree, edit the main checkout's `.aide.yaml` instead of the worktree's own |

```
aide cap disable docker
aide cap disable docker,k8s
```

### aide cap never-allow

```
aide cap never-allow <path> [flags]
```

Adds a global deny path that applies across all capabilities and profiles.

| Flag | Description |
|------|-------------|
| `--env` | Treat argument as an environment variable name instead of a path |
| `--list` | List current global deny entries |
| `--remove` | Remove the entry instead of adding it |

```
aide cap never-allow ~/.ssh/id_ed25519
aide cap never-allow --env AWS_SECRET_ACCESS_KEY
aide cap never-allow --list
aide cap never-allow --remove /tmp/scratch
```

### aide cap check

```
aide cap check <capability>[,capability...]
```

Previews the composed result of one or more capabilities without modifying
config. Shows the merged readable, writable, denied paths and env allowlist.
Capabilities are comma-separated, not space-separated.

```
aide cap check k8s,docker
```

### aide cap audit

```
aide cap audit [flags]
```

Shows the current profile's resolved capabilities -- the effective set of
paths and environment allowlists after composing all enabled capabilities.

| Flag | Description |
|------|-------------|
| `--profile <name>` | Audit a specific profile instead of the current one |

```
aide cap audit
```

### aide cap suggest-for-path

```
aide cap suggest-for-path <path>
```

Maps a filesystem path to capability names that would grant access to it.

```
aide cap suggest-for-path /var/run/docker.sock
```

---

## aide profile

Manages profiles. Earlier releases called them contexts: `aide context
<subcommand>` still works as a deprecated alias (it prints `aide context
is deprecated; use aide profile (same subcommands)` to stderr), and
`--context <name>` still works on every command that took it, as a
hidden, deprecated alias of `--profile <name>`.

### aide profile list

```
aide profile list
```

Lists all configured profiles with their agent, secret, match rules, and env var keys.

```
aide profile list
```

### aide profile bind

```
aide profile bind <name>
```

Attach the current folder to an existing profile. Auto-detects the match rule
(the normalized `origin` remote, e.g. `github.com/org/repo`, when the folder
is a git repo whose origin has a host; exact folder path otherwise). Override
with `--path` or `--remote`. Credentials embedded in the remote URL are never
saved.

| Flag | Description |
|------|-------------|
| `--path` | Force exact folder path match. |
| `--remote` | Force git remote match (errors if not a git repo). |

```
aide profile bind work               # auto-detect: git remote if repo, else folder path
aide profile bind work --path        # force exact folder path match
aide profile bind                    # interactive picker over existing profiles
```

### aide profile create

```
aide profile create [name]
```

Create a new profile. In TTY mode, prompts for any missing fields. In
non-interactive use, supply `--agent` and either `--here` or `--no-here`.

| Flag | Description |
|------|-------------|
| `--agent <name>` | Set the agent without prompting. |
| `--secret-store <name>` | Bind a secret store at create time. |
| `--here` | Bind this folder as a match rule (auto-detect). |
| `--no-here` | Skip cwd binding entirely. |

```
aide profile create work --agent claude --secret-store personal --here
aide profile create work --agent claude --no-here
```

### aide profile set-secret

```
aide profile set-secret <secret-name> [--profile name]
```

Sets the secret on a profile.

| Flag | Description |
|------|-------------|
| `--profile <name>` | Target profile (default: CWD-matched) |

```
aide profile set-secret personal --profile work
```

### aide profile remove-secret

```
aide profile remove-secret [--profile name]
```

Removes the secret from a profile. Warns if env vars reference secret templates.

| Flag | Description |
|------|-------------|
| `--profile <name>` | Target profile (default: CWD-matched) |

```
aide profile remove-secret --profile work
```

### aide profile migrate

```
aide profile migrate [--write] [--adopt <profile>]...
```

Converts `config.yaml` from `contexts:` to `profiles:`. Without `--write`
it prints the converted file and a summary; nothing is written. With
`--write` it keeps the original as `config.yaml.bak` (or
`config.yaml.bak-<timestamp>` when that exists), writes the new file
atomically and updates provisioning state so the rewrite is not reported
as drift. `.aide.yaml` is never changed. A `contexts:` section using YAML anchors, aliases or merge keys is
refused; expand it by hand first.

| Flag | Description |
|------|-------------|
| `--write` | Write the migrated config (default: preview only) |
| `--adopt <profile>` | Keep this profile on its own agent's global login (e.g. `~/.claude`) by adding `config_dir_name: <profile>` and `config_dirs: {<agent>: <default dir>}`. Under `--agent <other agent>` the profile then uses a fresh `~/.<agent>-<profile>` dir with its own login; the preview lists them. Repeatable, but two profiles cannot adopt the same dir, and a profile cannot adopt while another profile still has no config dir of its own |

See [Profiles § Migrating from contexts](profiles.md#migrating-from-contexts).

### aide profile set-default

```
aide profile set-default [name] [--profile name]
```

Sets the fallback profile used when no match rules apply. Without an argument,
uses `--profile`, else the CWD-matched profile.

```
aide profile set-default personal
```

### aide profile rename

```
aide profile rename <old-name> <new-name>
```

Renames a profile, updating the default if needed.

```
aide profile rename myproject acme
```

### aide profile remove

```
aide profile remove <name>
```

Removes a profile. Warns if the profile's agent is then unreferenced.

```
aide profile remove old-project
```

---

## aide env

### aide env set

```
aide env set KEY [VALUE] [flags]
```

Sets an environment variable on a profile. Provide a literal value as the
second argument, or use `--secret-key`, `--secret-store`, or `--pick` to
generate a template referencing a key in the profile's bound secret store.
`--global` is required when using any secret flag.

| Flag | Description |
|------|-------------|
| `--secret-key <key>` | Reference a key inside the bound (or explicit) secret store |
| `--secret-store <name>` | Override which secret store to read from (without changing the profile binding) |
| `--pick` | Interactively pick a key from the resolved store |
| `--profile <name>` | Target profile (default: CWD-matched) |
| `--global` | Apply to user-level config instead of the project |
| `--main` | In a linked git worktree, edit the main checkout's `.aide.yaml` instead of the worktree's own |

```
aide env set ANTHROPIC_API_KEY --secret-key api_key --profile work --global
```

### aide env list

```
aide env list [--profile name]
```

Lists env vars for a profile, annotating each with its source (literal value,
secret key, or template).

| Flag | Description |
|------|-------------|
| `--profile <name>` | Target profile (default: CWD-matched) |

```
aide env list --profile work
```

### aide env remove

```
aide env remove KEY [--profile name]
```

Removes an env var from a profile.

| Flag | Description |
|------|-------------|
| `--profile <name>` | Target profile (default: CWD-matched) |
| `--global` | Apply to user-level config instead of the project |
| `--main` | In a linked git worktree, edit the main checkout's `.aide.yaml` instead of the worktree's own |

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

Lists all secrets files with recipients and which profiles reference each file.

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

## aide trust

```
aide trust [path]
```

Trusts a `.aide.yaml` (default: the one in the current directory). The approval is keyed on the file's location (parent directories symlink-resolved; a symlinked `.aide.yaml` is approved as itself, never via its target) and its contents, so any modification resets trust. In a linked git worktree the main checkout's file is approved separately; pass its path.

```
aide trust
aide trust ~/src/project/.aide.yaml
```

---

## aide deny

```
aide deny [path]
```

Permanently blocks a `.aide.yaml` (default: the one in the current directory) from being applied, regardless of content changes. `aide trust` on the file lifts the block.

```
aide deny
```

---

## aide untrust

```
aide untrust [path]
```

Removes trust for a `.aide.yaml` (default: the one in the current directory) without blocking it. The next launch will show the untrusted warning.

```
aide untrust
```

---

## aide sandbox

### aide sandbox show

```
aide sandbox show [--profile name]
```

Shows the effective sandbox policy (writable, readable, denied paths; network
mode) for the current or named profile.

| Flag | Description |
|------|-------------|
| `--profile <name>` | Show policy for a specific profile |

```
aide sandbox show --profile work
```

### aide sandbox test

```
aide sandbox test [--profile name]
```

Generates and prints the platform-specific sandbox profile without launching the
agent. Useful for auditing what the OS sandbox will enforce.

| Flag | Description |
|------|-------------|
| `--profile <name>` | Generate the sandbox profile for a specific profile |

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

Removes a named sandbox profile. Warns if any profiles reference it.

```
aide sandbox remove strict
```

### aide sandbox allow

```
aide sandbox allow <path> [--write] [--profile name]
```

Adds a path to `readable_extra` (default) or `writable_extra` for a profile's
inline sandbox policy. When the profile's agent supports it (currently
Claude), the path is also granted in the agent's own permission store; pass
`--no-agent-grant` to skip this.

| Flag | Description |
|------|-------------|
| `--write` | Add to writable_extra instead of readable_extra |
| `--profile <name>` | Target profile (default: CWD-matched) |
| `--no-agent-grant` | Skip granting/revoking this path in the agent's own permission store (e.g. Claude's additionalDirectories) |

```
aide sandbox allow ~/shared-docs --write
```

### aide sandbox deny

```
aide sandbox deny <path> [--profile name]
```

Adds a path to `denied_extra` for a profile's inline sandbox policy. When the
profile's agent supports it (currently Claude), the path is also revoked from
the agent's own permission store; pass `--no-agent-grant` to skip this.

| Flag | Description |
|------|-------------|
| `--profile <name>` | Target profile (default: CWD-matched) |
| `--no-agent-grant` | Skip granting/revoking this path in the agent's own permission store (e.g. Claude's additionalDirectories) |

```
aide sandbox deny ~/private
```

### aide sandbox network

```
aide sandbox network <mode> [--profile name]
```

Sets the network mode for a profile's inline sandbox policy. Mode must be one of
`outbound`, `none`, or `unrestricted`.

| Flag | Description |
|------|-------------|
| `--profile <name>` | Target profile (default: CWD-matched) |

```
aide sandbox network none --profile restricted
```

### aide sandbox ports

```
aide sandbox ports <port>... [--profile name]
```

Sets the allowed outbound ports for a profile's inline sandbox policy (implies
network mode `outbound`).

| Flag | Description |
|------|-------------|
| `--profile <name>` | Target profile (default: CWD-matched) |

```
aide sandbox ports 443 8080
```

### aide sandbox reset

```
aide sandbox reset [--profile name]
```

Resets a profile's sandbox configuration to defaults by removing its inline
sandbox ref.

| Flag | Description |
|------|-------------|
| `--profile <name>` | Target profile (default: CWD-matched) |

```
aide sandbox reset
```

---

## aide agents

### aide agents list

```
aide agents list
```

Lists configured agents (with binary path and which profiles use them) and
agents detected on PATH that are not yet configured.

Supported agents: Claude, Copilot, Codex, Aider, Goose, Amp, Gemini.

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

Removes an agent from config. Warns if any profiles still reference it.

```
aide agents remove old-agent
```

---

## aide sync

```
aide sync [--profile <name>] [--plan] [--yes] [--force-secrets]
```

Reconciles the declared `plugins:` and `mcp_servers:` sets for a
profile against what is installed in the agent. See
[Provisioning](provisioning.md) for the full model.

Flags:
- `--profile <name>`: operate on a specific profile (default: match by CWD).
- `--plan`: print the plan, exit without mutating state.
- `--yes`: non-interactive (skip confirmation).
- `--force-secrets`: force re-resolving secret-templated MCP env values
  even if neither `config.yaml` nor the encrypted secrets file changed.
  Sync normally skips decryption in that case, trusting the agent's
  already-installed values; use this to repair a manually-edited/corrupted
  installed value.

State is persisted to `~/.local/state/aide/managed.json` atomically and
only on full success. On any failure, the engine rolls back via an
in-memory inverse journal.

---

## aide skillshare targets

```
aide skillshare targets [--write]
```

Generates [Skillshare](https://skillshare.runkids.cc) account targets
(`agent:` + `config_dir:`), named `aide-<profile>-<harness>`, for every
profile and harness (`claude`, `codex`, `pi`) that launches from a
config dir of its own. Without `--write` it prints the targets, what
would change, and what is not written and why; nothing is written.
With `--write` it merges them into Skillshare's config
(`$SKILLSHARE_CONFIG`, else `$XDG_CONFIG_HOME/skillshare/config.yaml`,
else `~/.config/skillshare/config.yaml`) atomically, keeping the
original as `config.yaml.bak`, then tells you to run `skillshare sync`.
Unrelated configuration and your own targets are kept; stale `aide-*`
targets are removed. No credential is written.

| Flag | Description |
|------|-------------|
| `--write` | Merge the targets into Skillshare's config (default: preview only) |

See [Skillshare](skillshare.md).

---

## aide adopt

```
aide adopt [--profile <name>] [--yes]
```

Walks agent-installed but undeclared plugins / MCP servers /
marketplaces and prompts to add them to `config.yaml`, then records
them as managed in state. Use this to bring a hand-installed setup
under aide management.

Flags:
- `--profile <name>`: defaults to current-CWD match.
- `--yes`: accept everything without prompting.

---

## aide plugin

### aide plugin list

```
aide plugin list [--profile <name>]
```

Three-column view (declared / installed / managed) per profile. For
marketplace-class agents, includes a MARKETPLACES section first with
the agent's canonical marketplace name and any installed-but-unmanaged
entries flagged as `unmanaged`.

---

## aide mcp

### aide mcp list

```
aide mcp list [--profile <name>]
```

Three-column view (declared / installed / managed) for MCP servers.
Same shape as `aide plugin list`.

---

## aide hook

### aide hook list

```
aide hook list [--profile <name>]
```

Shows hooks declared in `config.yaml` for the profile, with a MANAGED column
indicating whether the hook is currently written to the agent's config. The
list is sourced from the config file, not from the agent's live state, so it
reflects what aide knows about rather than what the agent actually runs.

Flags:
- `--profile <name>`: operate on a specific profile (default: match by CWD).

---

### aide hook add

```
aide hook add --event <event> --command <cmd> [--matcher <matcher>] [--timeout <seconds>] [--profile <name>]
```

Appends a hook entry to `config.yaml` under the profile's `hooks.extra`
block. Does not write anything to the agent: run `aide sync` to apply.

Flags:
- `--event <event>`: hook event (required): `pre_tool`, `post_tool`, `session_start`, `session_end`, `notification`, `stop`.
- `--command <cmd>`: shell command to run (required). Shell metacharacters (`;|&\`$(){}` etc.) are rejected to prevent injection.
- `--matcher <matcher>`: narrow the hook to a tool class (optional). `shell` targets the Bash/shell tool only (mapped to the agent's native name, e.g. `Bash` for Claude).
- `--timeout <seconds>`: override the agent default timeout (optional, 0 = driver default).
- `--profile <name>`: defaults to CWD match.

---

### aide hook remove

```
aide hook remove --event <event> --command <cmd> [--matcher <matcher>] [--profile <name>]
```

Removes the matching hook entry from `config.yaml`. Does not touch the agent
until you run `aide sync`.

Flags mirror `hook add`. The hook is identified by the `(event, matcher,
command)` triple: all three must match the entry to remove.

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

## aide statusline

### aide statusline [agent]

```
aide statusline [agent] [flags]
aide statusline claude [flags]
```

Renders the current aide session state as a compact statusline string for use
with a coding agent's statusline feature (e.g. Claude Code's `statusLine`
setting). Reads session state from `AIDE_*` environment variables injected by
aide before exec. `claude` is currently the only agent with rendering
support; any other resolved agent name (explicit `--agent`, positional
argument, or `AIDE_AGENT`) returns an error instead of silently rendering.

**Piped mode** (when stdin is a pipe: a coding agent's invocation path):

1. Reads all of stdin (JSON from the invoking agent).
2. Reads `AIDE_*` env vars for session state.
3. Loads merged config (`~/.config/aide/config.yaml` → `.aide.yaml`).
4. Walks the configured module order, renders each active module to a string.
5. Joins with ` | ` and writes to stdout.

**TTY mode** (when stdin is a terminal: a human previewing the statusline
directly): stdin is not read at all in this mode. Since there's no
aide-launched session to read real `AIDE_*` env vars from, TTY mode instead
*simulates* one: it resolves the CWD-matched aide profile and computes what
a real `aide launch` would set (sandbox, network, capabilities, trust,
auto-approve) without actually launching: no secret decryption, no
interactive capability-variant prompts. The agent is resolved via
`--agent`/positional argument, `AIDE_AGENT`, the aide profile matched to the
current working directory, or `claude` as the final default.

**`--profile <name>`** forces this same simulation for an explicitly named
profile, in *both* TTY and piped mode: useful for checking what the
statusline would look like under a profile other than the one CWD would
match. Errors if the name doesn't exist. Unlike the CWD-matched TTY case,
a named profile is looked up directly with no `.aide.yaml` project-override
merge (matching `aide cap list`/`aide cap audit`'s treatment of a named
profile). When `--agent` isn't also given, the agent comes from the named
profile's own configuration.

Bare `aide statusline` (no positional argument, no `--agent`, no
`--profile`) auto-detects the agent: `AIDE_AGENT` when set (an
aide-launched session), otherwise the shape of piped stdin JSON identifies
Claude Code, otherwise (TTY only) the CWD-matched aide profile's configured
agent, otherwise `claude`.

| Flag | Description |
|------|-------------|
| `--agent <name>` | Coding agent to render for (default: auto-detected) |
| `--module <name>` | Render only this module; repeatable (`--module sandbox --module network`). Valid names: `sandbox`, `network`, `caps`, `trust`, `context` (the profile name), `auto_approve`. An unrecognized name is an error, not a silently blank widget. |
| `--install` | Patch the target profile's `settings.json` to set `statusLine.command` |
| `--remove` | Clear the `statusLine` key from the target profile's `settings.json` |
| `--profile <name>` | For `--install`/`--remove`: target a specific aide profile (default: matched by CWD), resolving the correct `CLAUDE_CONFIG_DIR` for profiles with their own config dir automatically. For render mode: preview that profile's simulated session instead of the CWD-matched one, in both TTY and piped mode. |

**Install behaviour:**

| Existing `statusLine.command` | Action |
|-------------------------------|--------|
| Not set | Set to `"aide statusline claude"` |
| `"aide statusline claude"` | No-op; prints confirmation |
| Anything else | Generate `~/.config/aide/statusline-wrapper.sh` that pipes stdin to both the existing command and aide; update settings to point to wrapper |

**Sample output:**
```
🔒 | 🌐 | ⚡ k8s,docker          # sandbox on, outbound, caps active
🔒 | 🌐 | ⚡ none | ⚠️           # untrusted .aide.yaml
🚨 | 🔓 | 🌍 | ⚡ none           # auto-approve on, sandbox off, unrestricted
```

```bash
aide statusline claude --install                    # one-time setup (CWD-matched profile)
aide statusline claude --install --profile work     # install for a specific profile
aide statusline claude --remove                     # undo
aide statusline claude --remove --profile work      # undo for a specific profile
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
