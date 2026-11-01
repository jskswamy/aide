# Skillshare

[Skillshare](https://skillshare.runkids.cc) installs skills, agents,
MCP servers, hooks and plugins into coding-agent config dirs. aide
profiles give each harness its own config dir (`~/.claude-work`,
`~/.codex-work`, ...). `aide skillshare targets` tells Skillshare about
those dirs, so `skillshare sync` installs your resources into exactly
the dirs aide launches with.

The integration is configuration-only. aide writes Skillshare's
`targets:` and nothing else; you keep using Skillshare to install,
update and sync. aide never runs `skillshare` itself.

## Generate the targets

```sh
skillshare init                    # once, if you have no Skillshare config
aide skillshare targets            # preview: the targets and what would change
aide skillshare targets --write    # merge them into Skillshare's config
skillshare sync                    # install resources into the profile dirs
```

Re-run `aide skillshare targets --write` after adding, removing or
re-pointing a profile, then `skillshare sync` again.

### What is written

One [account target](https://skillshare.runkids.cc/docs/reference/targets/configuration#agent-config-dir)
per profile and harness (`claude`, `codex`, `opencode`, `pi`) whose
config dir is the profile's own:

```yaml
targets:
  aide-work-claude:
    agent: claude
    config_dir: ~/.claude-work
  aide-work-codex:
    agent: codex
    config_dir: ~/.codex-work
  aide-work-opencode:
    agent: opencode
    config_dir: ~/.opencode-work
  aide-work-pi:
    agent: pi
    config_dir: ~/.pi-work
```

`config_dir` is the dir aide launches that harness with under that
profile (`config_dirs:` entry, else `config_dir:`, else
`~/.<harness>-<config_dir_name>`), computed by the same code the
launcher uses. Every profile gets all four harnesses, since
`--agent` can launch any profile under any of them; Skillshare
creates `<config_dir>/skills` when it syncs.

### Where

Skillshare's config file: `$SKILLSHARE_CONFIG` when set, else
`$XDG_CONFIG_HOME/skillshare/config.yaml`, else
`~/.config/skillshare/config.yaml`. `--write` needs the file to exist
(run `skillshare init` first). It writes atomically and keeps the
original as `config.yaml.bak` (`config.yaml.bak-<timestamp>` when that
exists). When nothing changes the file is not touched.

### Ownership

Targets named `aide-<profile>-<harness>` that declare an `agent:` are
aide's. aide updates their `agent:` and `config_dir:` (other keys you
add to them, such as `skills:` filters or `cli:`, are kept) and
removes the ones whose profile or dir no longer qualifies. Everything
else in the file is left alone: comments, `sources:`, `mcp:`,
`extras:`, and your own targets. Do not name your own account targets
`aide-...`.

If a removed target is still listed elsewhere (for example in
`mcp.targets`), the output says where; remove it there.

### What is not written

Each is listed in the output with the reason:

- **No config dir of its own**: a profile without `config_dir_name:`,
  `config_dir:` or `config_dirs:` runs every harness from its default
  dir (`~/.claude`, `~/.codex`, `~/.config/opencode`, `~/.pi/agent`).
  Skillshare forbids account targets at a harness's default dir; its
  plain `claude`, `codex`, `opencode` and `pi` targets cover it.
- **A dir set through `env:`**: a profile without those keys that sets
  `CLAUDE_CONFIG_DIR`, `CODEX_HOME`, `OPENCODE_CONFIG_DIR` or
  `PI_CODING_AGENT_DIR` in `env:` is reported for that harness; move the dir to `config_dirs:` to get a
  target.
- **A harness's default dir**: a `config_dirs:` entry pointing at the
  default dir (as `aide profile migrate --adopt` writes), same reason.
- **A dir two targets would share**: a `config_dir:` pin without
  `config_dirs:` runs every harness from one dir. Skillshare needs one
  dir per target, so give each harness its own `config_dirs:` entry.
- **A dir or name one of your own targets already uses.**
- **Other harnesses**: aide writes account targets for `claude`,
  `codex`, `opencode` and `pi` only (Skillshare also supports `omp`, which aide
  does not launch), so a profile whose agent is `copilot`, `gemini`,
  `omp`, ... is reported.

These are Skillshare limits, reported separately from login isolation:
whether a harness keeps profiles' logins apart is tracked in
[profiles-verification.md](profiles-verification.md).

## Credentials and MCP servers

Only target names, harness names and dirs are written: never a
credential, a `credentials:` binding's value or an `env:` value.

aide does not write MCP assignments in this version. Skillshare's
schema for them is documented: a server selects targets through
`mcp.targets` or its own `targets:` list, and secrets are environment
references, `{fromEnv: VARIABLE}` on `env`, `headers` and
`bearerToken`, which Skillshare writes as references and never
resolves ([MCP accounts](https://skillshare.runkids.cc/docs/reference/commands/mcp#accounts),
[sharing MCP servers](https://skillshare.runkids.cc/docs/how-to/daily-tasks/sharing-mcp)).
It is not emitted because `aide sync` still writes `mcp_servers:` into
the same native files (`<config_dir>/.claude.json`,
`config.toml`, `mcp.json`); two writers would fight. Until the
provisioning migration (below) moves MCP servers to Skillshare, either
keep MCP servers in aide, or assign them to `aide-*` targets in
Skillshare yourself and drop them from aide's config for those
profiles, using `fromEnv` references for any secret. The
variables must then be in the agent's environment: set them in the
profile's `env:` or `credentials:` bindings.

## Hooks, plugins and instructions

The same one-writer rule applies to hooks, plugins and shared
instructions: `aide sync` still provisions hooks and plugins. Manage
each kind in one tool per profile dir. Moving aide's provisioning to
Skillshare (export, then deprecate, then remove) is planned separately;
aide's provisioning stays until that migration preserves your
existing resources.

## Sandbox

Skillshare links skills (and Claude agents) into
`<config_dir>/skills/` and `<config_dir>/agents/` as symlinks to its
source tree. macOS Seatbelt and Linux Landlock check the resolved path,
so the sandboxed agent needs read access to that tree. The built-in
`skillshare` [capability](capabilities.md) gives it, on both platforms
and for every harness. It is auto-included whenever Skillshare's config
dir (`$XDG_CONFIG_HOME/skillshare`, else `~/.config/skillshare`) exists,
and grants read-only access, never write, to:

- `skills/` and `agents/` in that dir;
- `$XDG_STATE_HOME/skillshare/plugins` (else
  `~/.local/state/skillshare/plugins`), which Claude's settings and Pi's
  settings reference;
- every path under `sources:` (or the older `source:`) in Skillshare's
  config file (located as in [Where](#where)), unless it is relative,
  `$HOME`, `/`, outside `$HOME`, or overlaps a sensitive dir (`~/.ssh`,
  `~/.aws`, `~/.gnupg`, `~/.config/gcloud`, ...), a harness's default
  dir (`~/.claude`, `~/.codex`, `~/.pi/agent`, ...), any profile's
  config dir, or aide's config dir. Such a path is skipped.

Each path is granted by its symlink-resolved location (and its literal
one when that differs); missing dirs are skipped. Nothing else of
Skillshare's is readable through the capability: not `config.yaml`, the
`hooks/`, `mcp/` and `logs/` state, the backups in
`~/.local/share/skillshare` or `~/.cache/skillshare`.

To turn it off, add `disabled_capabilities: [skillshare]` to a project's
`.aide.yaml`, or launch with `--without skillshare`. `aide cap list`
shows it as enabled when it is active; `aide cap show skillshare` and
`aide status` list the paths it grants on this machine.
