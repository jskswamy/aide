# aide Configuration

## Config Location

aide reads its global configuration from `~/.config/aide/config.yaml`. If `$XDG_CONFIG_HOME` is set, aide uses `$XDG_CONFIG_HOME/aide/config.yaml` instead.

Encrypted secret files live under `~/.config/aide/secrets/`.

To override settings for a specific project, place a `.aide.yaml` file in the project root. aide walks up from the current directory and stops at the git root when searching for this file.

---

## Minimal Format

A flat file with no `agents:`, `profiles:` or (legacy) `contexts:` keys. aide treats it as a single default profile.

```yaml
agent: claude
env:
  ANTHROPIC_API_KEY: "{{ .secrets.anthropic_api_key }}"
secret: personal
yolo: true          # optional: skip agent permission checks
auto_approve: true  # alias for yolo
```

aide registers the agent binary automatically using the name provided. Credentials belong here, not on a separate agent definition.

---

## Full Format

Use the full format for multiple projects with different agents or credentials.

Earlier releases called profiles contexts. `contexts:`/`default_context:`
still load (with `profile:`/`profile_dir:` inside each entry); a file
must use one form or the other. `aide profile migrate` previews the
conversion and `aide profile migrate --write` applies it (see
[Profiles § Migrating from contexts](profiles.md#migrating-from-contexts)).

```yaml
agents:
  claude:
    binary: claude
    icon: "🤖"
  aider:
    binary: aider

profiles:
  work:
    match:
      - remote: "github.com/acme/*"
      - path: "~/work/*"
    icon: "💼"
    agent: claude
    secret: work
    env:
      ANTHROPIC_API_KEY: "{{ .secrets.anthropic_api_key }}"
    sandbox:
      network: outbound
      writable:
        - "{{ .project_root }}"

  personal:
    match:
      - remote: "github.com/myuser/*"
    icon: "🏠"
    agent: claude
    secret: personal
    env:
      ANTHROPIC_API_KEY: "{{ .secrets.anthropic_api_key }}"
    sandbox: false

  open-source:
    match:
      - path: "~/oss/*"
    agent: aider
    secret: oss
    env:
      OPENAI_API_KEY: "{{ .secrets.openai_api_key }}"
    sandbox: strict

  infra:
    match:
      - path: "~/infra/*"
    agent: claude
    secret: work
    capabilities: [docker, k8s]
    auto_approve: true

default_profile: personal

preferences:
  show_info: true
  info_style: compact
  info_detail: normal
```

---

## Agent Definitions

The `agents:` block maps names to binaries. The agent name is the map key. `binary:` is the executable name or absolute path; if omitted, aide uses the agent name as the binary. Agents are binary definitions only. Credentials and environment variables belong on profiles.

Known agents that aide detects automatically on `PATH`: `claude`, `codex`, `aider`, `goose`, `amp`, `gemini`, `copilot`, `cursor-agent`.

```yaml
agents:
  claude:
    binary: claude
    icon: "🤖"        # optional; shown in banner and prompt
  aider:
    binary: aider
```

The `icon` field (optional) sets a Unicode symbol displayed alongside the agent name in the banner and `aide prompt` output.

---

## Top-Level Config Fields

In addition to `agents:`, `profiles:`, `default_profile:`, and `preferences:`, the following top-level fields are available:

- `capabilities:` (map) - User-defined capability definitions. Each key is a capability name mapping to a `CapabilityDef`. Example:

```yaml
capabilities:
  k8s-dev:
    extends: k8s
    readable: ["~/.kube/dev-config"]
    deny: ["~/.kube/prod-config"]
    env_allow: [KUBECONFIG]
```

- `never_allow:` (list) - Paths that no capability can ever access. These are enforced globally regardless of which capabilities are active.
- `never_allow_env:` (list) - Environment variables always stripped from the agent process, even if a capability would otherwise permit them.
- `scrub_env:` (list) - Extra variable names added to the credential scrub registry (see [Credential-clean environment](#credential-clean-environment)). An entry ending in `*` matches a prefix (`CORP_LLM_*`).

- `hooks:` (map) - Declarative hook set. Keys are normalized event names (`pre_tool`, `post_tool`, `session_start`, `session_end`, `notification`, `stop`); values are lists of hook entries:

```yaml
hooks:
  pre_tool:
    - command: "rtk hook {agent}"
      matcher: shell   # optional: "shell" → agent-native name (e.g. Bash)
      timeout: 30      # optional: seconds; 0 = driver default
```

The `{agent}` template variable is replaced with the resolved agent name for each profile. Reconciled by `aide sync`. See [Hooks](#hooks) below.

- `plugins:` (map) - Declarative plugin set per agent. Value shape per entry decides the meaning: list = marketplace + plugin names, string = URL-direct install ref, null = declare-only marketplace. Reconciled by `aide sync`. See [Provisioning](provisioning.md).
- `mcp_servers:` (map) - Declarative MCP server set. Each entry is an inline table with `command`+`args` (stdio) or `url` (HTTP), plus optional `env`. Reconciled by `aide sync`.
- `statusline:` (map) - Configures the `aide statusline claude` renderer. Controls module order, per-module icons, state strings, and disabled state. Field-by-field merge with the project override; `order` is replaced wholesale when set in the project.

```yaml
statusline:
  order: [sandbox, network, caps, trust, context]
  sandbox:
    on: "🔒"
    off: "🔓"
    unmanaged: "💀"   # shown when AIDE_SANDBOX is absent entirely
  network:
    outbound: "🌐"
    unrestricted: "🌍"
    unmanaged: "🌫️"   # shown when AIDE_NETWORK_MODE is absent entirely
  caps:
    icon: "⚡"        # prefix; value is comma-joined capability list
  trust:
    untrusted: "⚠️"  # only shown when .aide.yaml is untrusted
  context:            # the matched profile's name (module keeps its name)
    icon: "📁"
  auto_approve:
    value: "🚨"       # always shown when active; disabled: true has no effect
```

Setting any state value to `""` hides that state silently (e.g. `sandbox.off: ""` hides the module when sandbox is disabled). Each module also accepts `disabled: true` to remove it from output. The `agent` key is accepted but ignored by the renderer (reserved for a future module).

The `sandbox`/`network` modules also accept an `unmanaged:` value, rendered
when the corresponding `AIDE_SANDBOX`/`AIDE_NETWORK_MODE` env var is absent
entirely rather than set to `off`/`unrestricted`; this distinguishes "aide
didn't launch this session at all" from "aide launched it with the sandbox
explicitly disabled." Defaults to `"💀"` for `sandbox` and `"🌫️"` for
`network`.

- `sandboxes:` (map) - Named sandbox profiles referenced from profiles (`sandbox: <name>`).
- `custom_guards:` and `guard_types:` (advanced) - Define custom seatbelt guard modules. Most users should not need these.

---

## Credential-clean environment

A configured launch (any `config.yaml`) does not hand the agent the
credentials exported in your shell. Before the profile's `env:`
is applied, aide removes from the inherited environment, whether or
not `--clean-env` is set:

- provider API keys and tokens (`ANTHROPIC_API_KEY`,
  `ANTHROPIC_AUTH_TOKEN`, `CLAUDE_CODE_OAUTH_TOKEN`, `OPENAI_API_KEY`,
  `CODEX_API_KEY`, `CODEX_ACCESS_TOKEN`, `GEMINI_API_KEY`, ...) and
  provider selectors and endpoints (`OPENAI_BASE_URL`,
  `ANTHROPIC_BASE_URL`, `ANTHROPIC_BEDROCK_BASE_URL`,
  `CLAUDE_CODE_USE_BEDROCK`, `CLAUDE_CODE_USE_VERTEX`,
  `AWS_BEARER_TOKEN_BEDROCK`, Anthropic federation variables, ...);
- Claude Code's other authentication inputs: `CLAUDE_CODE_OAUTH_REFRESH_TOKEN`,
  `ANTHROPIC_PROFILE`, `ANTHROPIC_CONFIG_DIR`, `ANTHROPIC_IDENTITY_TOKEN[_FILE]`,
  the workload-identity federation IDs, `ANTHROPIC_CUSTOM_HEADERS`,
  the mTLS client certificate and key (`CLAUDE_CODE_CLIENT_*`), and every
  `CLAUDE_CODE_USE_*` / `CLAUDE_CODE_SKIP_*_AUTH` provider switch;
- Pi's provider keys and endpoints (every variable in Pi's provider
  docs): `ANTHROPIC_OAUTH_TOKEN`, `AZURE_OPENAI_API_KEY`,
  `GOOGLE_CLOUD_API_KEY`, `CLOUDFLARE_API_KEY`, `COPILOT_GITHUB_TOKEN`,
  `HF_TOKEN`, `AI_GATEWAY_API_KEY`, the `*_API_KEY` of DeepSeek,
  NVIDIA, Cerebras, ZAI, OpenCode, Radius, TypeSafe, Fireworks,
  Together, Baseten, Kimi, Meta, MiniMax, Moonshot, Qwen and Xiaomi
  (secret-bearing), and `AZURE_OPENAI_BASE_URL`,
  `AZURE_OPENAI_RESOURCE_NAME`, `AZURE_OPENAI_DEPLOYMENT_NAME_MAP`,
  `AZURE_OPENAI_API_VERSION`, `CLOUDFLARE_ACCOUNT_ID`,
  `CLOUDFLARE_GATEWAY_ID`, `GOOGLE_CLOUD_LOCATION` (selectors);
- ambient cloud credentials: `AWS_*`, `CLOUDSDK_*`,
  `GOOGLE_APPLICATION_CREDENTIALS`, `GOOGLE_CLOUD_PROJECT`,
  `GCLOUD_PROJECT`, and the Azure default-credential variables
  (`AZURE_CLIENT_ID`, `AZURE_TENANT_ID`, `AZURE_CLIENT_SECRET`,
  `AZURE_CLIENT_CERTIFICATE_PATH`, `AZURE_FEDERATED_TOKEN_FILE`, ...);
- aide's own secret material: `SOPS_AGE_KEY`, `SOPS_AGE_KEY_FILE`;
- names you list in `scrub_env:`;
- when the profile selects its own config dir (`profile:`,
  `config_dir_name:`, `config_dir:` or `config_dirs:`), every harness
  config-dir redirect (`CLAUDE_CONFIG_DIR`, `CODEX_HOME`,
  `PI_CODING_AGENT_DIR`, `PI_CODING_AGENT_SESSION_DIR`, `COPILOT_HOME`,
  `OPENCODE_CONFIG`, ...).
  Without one, a redirect you export is kept as your global choice.

The full list lives in `internal/envscrub`. What still reaches the
agent:

- the profile's selected `credentials:` bindings (see "Credential
  bindings" below);
- values the profile sets in `env:`. A provider API key or token there
  is no longer passed through `env:`: see "Legacy credentials in
  `env:`" below. Provider selectors and endpoints
  (`CLAUDE_CODE_USE_BEDROCK`, `ANTHROPIC_VERTEX_PROJECT_ID`,
  `OPENAI_BASE_URL`, ...) are ordinary configuration there. Other
  scrubbed names (cloud variables, `scrub_env:` names) are passed on
  with a warning naming the variable (never its value).
- cloud variables a capability grants through `env_allow` (the
  built-in `aws`, `gcp` and `azure` capabilities re-admit their variables).
  Provider keys (including those in a cloud namespace, such as
  `AWS_BEARER_TOKEN_BEDROCK`), secret material and `scrub_env:` names
  are never re-admitted this way.

Limits: aide matches names. A credential exported under a name the
registry does not know passes through unless you add it to
`scrub_env:`, and files or keychains a scrubbed variable would have
pointed at (`~/.aws/credentials`, gcloud application-default
credentials, system keychains) are not removed; only the sandbox
controls whether the agent can read them. Provisioning subprocesses
(`aide sync`, plugin/MCP listing) drop the same names, including
`scrub_env:`.

Claude Code has two authentication sources no environment scrub
reaches:

- `apiKeyHelper` in Claude's settings is a command Claude runs for a
  key; that key outranks the profile's native login (only an injected
  `ANTHROPIC_API_KEY` or `ANTHROPIC_AUTH_TOKEN`, or a `CLAUDE_CODE_USE_*`
  provider, ranks above it). A profile launch reads the profile dir's
  `settings.json` and prints `aide: note: <dir>/settings.json sets
  apiKeyHelper ...` (naming the setting, never its command). aide does
  not remove it; delete it from that file if the profile should use its
  login. Managed settings and a project's `.claude/settings.json` can
  also set it and are not checked.
- A keyless Claude Console sign-in and `ant` CLI profiles are stored in
  `~/.config/anthropic`, outside `CLAUDE_CONFIG_DIR`, so every profile
  that can read it shares them. The sandbox does not grant that dir.
  To keep them per profile, set `ANTHROPIC_CONFIG_DIR` in the
  profile's `env:` to a dir inside its config dir.

Pi keeps logins and saved API keys in `auth.json` inside its config
dir (`PI_CODING_AGENT_DIR`, so one per profile), and uses a stored
entry ahead of the environment for that provider. A selected
`credentials:` binding therefore does not win in Pi for a provider the
profile has logged in to. A Pi launch that injects a binding reads
the `auth.json` of the config dir it uses (the profile's own dir, else
an exported `PI_CODING_AGENT_DIR`, else `~/.pi/agent`; provider names
only, never the stored values) and prints `aide: note: <dir>/auth.json has a stored Pi login
for <provider>: Pi uses it ahead of the selected API key ...`. The
launch continues; run `/logout` for that provider in Pi to use the
key, or launch with `--auth login` to use the login. Providers whose
`auth.json` id aide does not know (Bedrock, Vertex, custom names)
are not checked. Pi's Vertex provider without `GOOGLE_CLOUD_API_KEY` uses gcloud
application-default credentials
(`~/.config/gcloud/application_default_credentials.json`). That file
is one per user, not per profile: aide does not isolate it. The
sandbox grants `~/.config/gcloud` only with the `gcp` capability, so
every Pi profile with `gcp` (or launched without the sandbox) shares
the same Google login.

Codex reads a custom model provider's key from the variable its
`config.toml` names in `model_providers.<id>.env_key` (and header
values from `env_http_headers`), so those names are not in the
registry. aide treats them like credential binding names: every
configured launch (any harness) and every provisioning subprocess
scrubs, from the inherited environment, each such name declared in
any Codex home aide knows of: `~/.codex`, an exported `CODEX_HOME`,
and every profile's own Codex dir, each with its `config.toml` and
profile files (`<name>.config.toml`). Only the names are read, never
values or `experimental_bearer_token`; names of process essentials
(`PATH`, `HOME`, ...) and `AIDE_*` are ignored. A missing file
declares nothing. When the Codex home being launched has an
unreadable or invalid file, the launch prints `aide: note:
<dir>/config.toml is not valid TOML (line N): custom model provider
keys it names are not scrubbed from the shell` and continues. To
inject such a key, bind it with a custom `credentials:` entry
(`env: [CORP_LLM_KEY]`, `harnesses: [codex]`). Limits: a project's
`.codex/config.toml` and Codex homes outside these are not read; list
names you always want removed in `scrub_env:`. MCP server tokens
(`bearer_token_env_var`) are resource credentials and are left alone.

The no-config passthrough (`aide` with no `config.yaml`) only removes
`SOPS_AGE_KEY` and `SOPS_AGE_KEY_FILE`; shell API keys still reach the
agent there. Passthrough is not an isolation guarantee.

Every env name a `credentials:` binding declares (in any profile) is
also scrubbed from the inherited environment, so a custom key exported
in your shell cannot reach an agent that did not select it.

---

## Credential bindings

A profile names its provider API keys under `credentials:` and picks
the ones a launch injects with `default_credentials:`. Each binding
refers to one key in an age-encrypted secret store (the same stores
`secret:` names; see `aide secrets`).

```yaml
profiles:
  work:
    agent: claude
    credentials:
      anthropic:
        backend: age            # the only backend
        store: work             # secret store, named like secret:
        key: anthropic_api_key  # key inside that store
      openrouter:
        backend: age
        store: work
        key: openrouter_api_key
        harnesses: [pi]         # optional: only for these agents
      corp:                     # custom name: env: and harnesses: required
        backend: age
        store: corp
        key: llm_token
        env: [CORP_LLM_TOKEN]
        harnesses: [pi]
    default_credentials: [anthropic]
```

- Built-in names and the variable each sets: `anthropic`
  (`ANTHROPIC_API_KEY`), `openai` (`OPENAI_API_KEY`), `openrouter`
  (`OPENROUTER_API_KEY`), `codex` (`CODEX_API_KEY`), `gemini`
  (`GEMINI_API_KEY`). `env:` on a built-in replaces its variable
  names. A built-in without `harnesses:` is usable with every agent.
- Any other name is custom and must list `env:` and `harnesses:`.
  `harnesses:` matches the agent an `agents:` entry runs (`my-claude`
  with `binary: claude` is `claude`).
- A binding cannot set `PATH`, `HOME` and other process essentials,
  `AIDE_*`, `SOPS_AGE_KEY*`, or an agent config-dir variable such as
  `CLAUDE_CONFIG_DIR`.
- Only the selected bindings are injected, and only their stores are
  decrypted. Unselected bindings and other profiles' bindings never
  are: a profile without a key does not borrow one from another
  profile.
- A launch stops before anything runs when a selected binding is not
  defined in the profile, or its store or key is missing, or
  `--credential` names one restricted to other agents.
- `default_credentials:` and a saved selection (`aide project set
  --credential`) skip bindings restricted to other agents, so a profile
  that runs several agents lists one binding per agent:
  `default_credentials: [anthropic, openrouter]` with `anthropic`
  restricted to `harnesses: [claude]` and `openrouter` to
  `harnesses: [pi]` gives Claude only `ANTHROPIC_API_KEY` and Pi only
  `OPENROUTER_API_KEY`. `aide which --format json` shows the full
  selection under `credential_selection` and the injected bindings
  under `credentials`.
- Selected bindings are set after the profile's `env:`, so they win
  over a general `env:` value; `never_allow_env:` still removes them.
- The banner, `aide which` and `aide status` show binding names and
  variable names only (`--format json`: `credentials` with `name` and
  `env`, and `auth_mode`), never values.
- Loading fails with `invalid credentials` when `backend:` is not
  `age`, `store:` or `key:` is missing, a custom name lacks `env:` or
  `harnesses:`, an agent in `harnesses:` is unknown, or
  `default_credentials:` names an undefined binding.
- `.aide.yaml` cannot declare `credentials:` or
  `default_credentials:`, and a provider credential, selector or
  endpoint in its `env:` stops the launch (an endpoint there could
  redirect the profile's key to a host the repository chose).

### Legacy credentials in `env:`

A secret-bearing provider credential in a profile's `env:` (API keys
and tokens such as `ANTHROPIC_API_KEY`, `OPENAI_API_KEY`,
`CODEX_API_KEY`, `AWS_BEARER_TOKEN_BEDROCK`, or auth-file selectors such
as `ANTHROPIC_IDENTITY_TOKEN_FILE`) is handled at launch, without
changing your config file. Selectors and endpoints are not affected.

- `ANTHROPIC_API_KEY: "{{ .secrets.anthropic_api_key }}"`, a single
  secret reference with the profile's `secret:` store, becomes an
  implicit binding that is selected by default, so the launch behaves
  as before. aide prints a note once per run with the `credentials:`
  block to replace it with.
- Any other value (a literal key, a longer template, or a secret
  reference without `secret:`) stops the launch with the
  `credentials:` block to use instead. Values are never printed.

`aide env set` refuses these names and shows the `credentials:` block
to declare instead. It sets selectors and endpoints only with
`--global`, since `.aide.yaml` cannot carry them.

---

## Control Environment Variables

Alongside `AIDE_SANDBOX`, `AIDE_NETWORK_MODE`, and `AIDE_INFO_STYLE` (used
elsewhere in this doc to detect or override aide's runtime state), aide
also honors:

- `AIDE_PROFILE` and `AIDE_CONTEXT` (set by aide; read only by
  `aide statusline`) - The name of
  the resolved profile, exported to the agent and its child processes.
  Both carry the same value; `AIDE_CONTEXT` remains for scripts written
  against earlier releases.
- `AIDE_FORMAT` - Sets the default output format (`human` or `json`) for
  read/query commands (`aide which`, `aide cap list`, `aide status`,
  etc.) when `--format` isn't passed explicitly. Precedence: explicit
  `--format` flag > `AIDE_FORMAT` > `human`.

---

## Profile Fields

- `match:`: list of path or remote rules that activate the profile. Each rule sets one of `path:` (glob against CWD) or `remote:` (glob against the normalized git remote URL). `remote_name:` picks the remote a rule matches against and defaults to `origin`.
- `agent:`: agent name; must exist in `agents:`.
- `secret:`: secret file name resolved under `~/.config/aide/secrets/`.
- `env:`: environment variables passed to the agent; supports Go template syntax for secret injection.
- `credentials:` / `default_credentials:`: named provider API key bindings and the ones injected by default. See [Credential bindings](#credential-bindings).
- `icon:` (string, optional): Unicode symbol displayed alongside the profile name in the banner and `aide prompt` output.
- `config_dir_name:` (string, optional; `profile:` under legacy `contexts:`): names the profile's native agent config dirs. Driver derives the agent's config-dir env var (e.g. `CLAUDE_CONFIG_DIR=~/.claude-<name>`) and injects it at launch + sync + adopt + list. Avoids hand-rolling per-agent env vars. See [Provisioning § Profile interaction](provisioning.md#profile-interaction) and [Profiles](profiles.md).
- `config_dir:` (string, optional; `profile_dir:` under legacy `contexts:`): override the derived `~/.<agent>-<name>` path with an explicit absolute or HOME-rooted path for every agent.
- `config_dirs:` (map, optional): per-agent config dir, e.g. `{claude: ~/.claude-work}`; needs `config_dir_name:` as well.
- `plugins:` / `mcp_servers:` (per-profile overrides, optional): mapping with `extra:` / `exclude:` / `only:` deltas applied on top of the top-level set. See [Provisioning](provisioning.md).
- `hooks:` (per-profile override, optional): delta applied on top of the top-level `hooks:` map.

```yaml
profiles:
  work:
    hooks:
      exclude: [session_start]   # drop a top-level event entirely
      extra:
        pre_tool:
          - command: "notify work-hook {agent}"
```
- `capabilities:` (list) - Capability names to activate for this profile (e.g. `[docker, k8s]`). See [Capabilities](capabilities.md) for details.
- `yolo:` (bool, optional): skip agent permission checks for this profile. The agent-specific flag is injected automatically (e.g. `--dangerously-skip-permissions` for Claude). The OS sandbox remains active.
- `sandbox:`: accepts `false` (disable), a string profile name (e.g. `strict`), or an inline policy mapping:

```yaml
sandbox:
  writable:
    - "{{ .project_root }}"
  network: outbound
  allow_subprocess: true
```

---

## Preferences

The top-level `preferences:` block controls the startup display.

- `show_info:` (bool, default `true`). Show the startup banner before launching the agent.
- `info_style:` (`compact` | `boxed` | `clean`, default `compact`). Banner style.
- `info_detail:` (`normal` | `detailed`, default `normal`). Banner verbosity.
- `yolo:` (bool, optional). Global default for yolo mode. Profile-level and project-level `yolo:` override this.

Override any of these per-project in `.aide.yaml` under a `preferences:` key.

---

## Per-Project Override

`.aide.yaml` supports: `agent`, `env`, `sandbox`, `preferences`, `yolo`, `capabilities`, `disabled_capabilities`, `statusline`, `mcp_servers`, `capability_variants`. aide merges it on top of the matched global profile. It cannot set `secret:`; set the secret store in the profile.

- `env:` merges additively; project values win on key conflicts.
- All other fields replace the matched profile value entirely.
- In a linked git worktree, the main checkout's `.aide.yaml` applies
  first; see [Linked git worktrees](#linked-git-worktrees).

Relative sandbox paths (`writable`, `readable`, `denied` and their
`_extra` forms) are interpreted against the root of the checkout that
contains the declaring `.aide.yaml`, not the directory you launch from
(and not the file's own directory when the file sits in a
subdirectory of the checkout). The checkout is found from the file's
real location (symlinks in its directory resolved), the same location
trust is keyed on, so a repository that symlinks to another one's
directory cannot re-anchor its paths. Outside git, paths anchor at the
file's real directory; if git cannot be run there, a file with
relative paths is not applied and aide prints a warning.
Paths starting with `/`, `~`, `$` or `{{` are left as written.

### Trust gate

`.aide.yaml` files are not applied until trusted. On first encounter, aide shows the file's contents and skips it. Run `aide trust` to approve the file, or `aide deny` to block it permanently. See [Sandbox](sandbox.md#trust-gate) for details.

Trust is recorded per file, keyed on where the file is found (parent
directories symlink-resolved) and its contents: editing a file
withdraws its approval. A `.aide.yaml` that is a symlink is approved
at its own location, never through the file it points to; the trust
banner shows both. `aide trust`,
`aide deny` and `aide untrust` take an optional path for a file outside
the current directory.

### Linked git worktrees

A linked worktree (`git worktree add`) inherits the main checkout's
`.aide.yaml`, then applies its own on top. aide finds the main checkout
through git (`git rev-parse --git-common-dir` and `git worktree list`),
not by looking in parent directories, so the worktree can live anywhere.

| Launch from | Files applied |
|---|---|
| Main checkout (or a plain clone) | its own `.aide.yaml`, once |
| Linked worktree | main checkout's `.aide.yaml`, then the worktree's |
| Worktree of a bare repository | the worktree's own only; a bare repository has no main checkout file |
| Submodule | the submodule's own only; it never inherits the superproject's |

When both files apply, the worktree's file wins:

- Scalars (`agent`, `secret`, `yolo`, `profile`, sandbox `network`,
  `writable`, `readable`, `denied`, `guards`, ...) — the worktree's value
  replaces the inherited one.
- `capabilities` — a stable union, without duplicates. The worktree's
  `disabled_capabilities` may remove inherited capabilities, and a
  worktree capability re-enables one the main checkout disabled.
- `env` — merged key by key, the worktree's key wins. Provider
  credentials, selectors and endpoints stay forbidden in both files.
- Sandbox `_extra` lists, `unguard` — combined.
- `capability_variants` — merged per capability, the worktree wins.

Each file is trusted separately. An untrusted or denied file contributes
nothing; the other file still applies if it is trusted. Changing the main
checkout's file withdraws its approval in every worktree. To approve the
inherited file from a worktree, run `aide trust <main checkout>/.aide.yaml`
(the banner prints the exact command).

The launch banner, `aide which` and `aide status` list each untrusted or
denied file separately, and list the security-sensitive grants that come
from the main checkout's file (capabilities, writable and readable paths,
replaced guard or deny lists, extra `allow` rules, network mode,
removed guards, `allow_subprocess`, `clean_env: false`, MCP servers, a
disabled sandbox, auto-approve) labeled
with that file. In `--format json` these are `trust_files` and
`inherited_grants` (`aide status`: `project_trust`, `inherited_grants`).

aide inherits only when the repository itself records the checkout as
one of its worktrees (the worktree's administrative directory lies in
the repository's `worktrees/`, points back at this checkout, and `git
worktree list` names it). A directory whose `.git` file merely claims
to belong to another repository inherits nothing, with a warning.

A missing main checkout `.aide.yaml` is normal. If the main checkout
itself is gone or unreadable (deleted or moved), aide stops with a
diagnostic naming the missing path rather than launching with partial
policy: restore it, run `git worktree repair` from the moved checkout or
`git worktree prune` after deleting it, or launch with
`--ignore-project-config`.

`aide cap enable|disable` and `aide env set|remove` in a linked worktree
edit the worktree's own `.aide.yaml` (created if absent); pass `--main`
to edit the main checkout's file instead. Edits keep the file as
written: inherited settings are never copied into it.

---

## Validation

`aide validate` checks:

- Agent names in profiles exist in `agents:`.
- Secret files exist on disk.
- Sandbox profile names are defined in `sandboxes:`.
- Template syntax in `env:` values is valid.
- `env:` values referencing `.secrets.*` have a `secret:` configured on that profile.

Warnings print separately from hard errors. The command exits non-zero on any error.

---

## Hooks

Hooks inject commands into agent lifecycle events. aide writes them to the
agent's config file during `aide sync`; the agent runs them, not aide.

### Supported events

| Event | Claude | Gemini | Cursor | Copilot | Hermes | OpenCode | Pi |
|-------|--------|--------|--------|---------|--------|----------|----|
| `pre_tool` | `PreToolUse` | `BeforeTool` (script) | `preToolUse` | `PreToolUse` (file) | `pre_tool_call` (plugin) | `tool.execute.before` | `tool_call` |
| `post_tool` | `PostToolUse` | - | - | - | - | `tool.execute.after` | `tool_result` |
| `session_start` | `SessionStart` | - | - | - | - | `session.created` (event) | `session_start` |
| `session_end` | `SessionEnd` | - | - | - | - | `dispose` | `session_shutdown` |
| `notification` | `Notification` | - | - | - | - | - | - |
| `stop` | `Stop` | - | - | - | - | - | - |

Events not supported by an agent are silently skipped during sync.

OpenCode's `session_start` hook skips subagent sessions, and its
`session_end` hook runs when OpenCode exits normally, not when it is
killed. Pi's session hooks skip extension reloads. As with Claude, both
also fire when you switch to a new or resumed session.

### Supported matchers

| Matcher | Claude | Cursor | OpenCode | Pi |
|---------|--------|--------|----------|----|
| `shell` | `Bash` | `Shell` | `bash` | `bash` |

Matchers not supported by an agent are ignored. Claude, OpenCode and Pi
pass any other matcher through as a native tool name. OpenCode and Pi
have no matcher field, so their generated hook scripts check the tool
name themselves and return early for other tools.

### Storage formats per agent

- **Claude** - merged into `~/.claude/settings.json` (or `$CLAUDE_CONFIG_DIR/settings.json`) under the `hooks` key. Ownership is tracked in `managed.json`; user-added entries are never touched.
- **Gemini** - individual `aide_<hash>.sh` scripts written to `~/.gemini/hooks/`. Only `pre_tool` is supported.
- **Cursor** - merged into `~/.cursor/hooks.json`. Ownership is tracked in `managed.json`; user-added entries are never touched.
- **Copilot** - individual `aide-<hash>.json` files written to `~/.config/copilot/hooks/`. Only `pre_tool` is supported.
- **Hermes** - Python plugin directories written to `~/.hermes/plugins/aide_<hash>/` (an `__init__.py` + `plugin.yaml` per hook). Only `pre_tool` is supported.
- **OpenCode** - individual `aide-<hash>.js` plugin files written to `~/.config/opencode/plugin/`.
- **Pi** - individual `aide-<hash>.ts` extension files written to `~/.pi/agent/extensions/` (or under `$PI_CODING_AGENT_DIR`).
- **Codex** - no hook support.

### The `{agent}` template variable

The command field supports `{agent}`, replaced with the profile's resolved
agent name at sync time:

```yaml
hooks:
  pre_tool:
    - command: "rtk hook {agent}"
```

This lets a single top-level hook declaration work across all your profiles
without per-profile duplication.

### Management

```bash
aide hook list                                          # show declared hooks
aide hook add --event pre_tool --command "rtk hook {agent}"  # add a hook
aide hook remove --event pre_tool --command "rtk hook {agent}"  # remove a hook
aide sync                                               # write to agent config
```

---

## Viewing and Editing

```
aide config show    # print the global config file
aide config edit    # open $EDITOR, validate after save
```
