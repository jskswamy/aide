## Unreleased

### 💥 Breaking changes

#### Shell-exported API keys no longer reach agents launched with a config

With a `config.yaml`, aide now starts the agent from a credential-clean
environment, with or without `--clean-env`. Provider API keys and tokens
(`ANTHROPIC_API_KEY`, `ANTHROPIC_AUTH_TOKEN`, `CLAUDE_CODE_OAUTH_TOKEN`,
`OPENAI_API_KEY`, `CODEX_API_KEY`, ...), provider and federation
selectors (`OPENAI_BASE_URL`, `CLAUDE_CODE_USE_BEDROCK`, ...), ambient
cloud credentials (`AWS_*`, `CLOUDSDK_*`, `GOOGLE_APPLICATION_CREDENTIALS`)
and `SOPS_AGE_KEY` / `SOPS_AGE_KEY_FILE` exported in your shell are
removed before the context's `env:` is applied. If you relied on an API
key exported in your shell, the agent now falls back to its own login.

- The replacement is `credentials:` bindings (see "Credential bindings
  select provider API keys per profile" below).
- The built-in `aws` and `gcp` capabilities re-admit the cloud
  variables they list in `env_allow`. Provider keys (including
  `AWS_BEARER_TOKEN_BEDROCK`) and your `scrub_env:` names are never
  re-admitted.
- New top-level `scrub_env:` adds your own names (or `PREFIX_*`
  patterns) to the scrubbed list.
- A context that selects its own config dir (`profile:`,
  `config_dir_name:`, `config_dir:`, `config_dirs:`) also drops exported
  agent dir redirects (`CLAUDE_CONFIG_DIR`, `CODEX_HOME`,
  `PI_CODING_AGENT_DIR`, ...) so they cannot bypass the profile.
- `aide sync` subprocesses drop the same names, `scrub_env:` included.
- Without a `config.yaml` (passthrough), only `SOPS_AGE_KEY` and
  `SOPS_AGE_KEY_FILE` are removed; passthrough is not isolation.
- aide matches names: unknown variable names, and files or keychains
  that cloud variables point at, are not scrubbed. See
  "Credential-clean environment" in `docs/configuration.md`.

#### More Claude Code and Azure credential variables are scrubbed

The credential-clean environment now also removes Claude Code's
remaining authentication inputs exported in your shell:
`CLAUDE_CODE_OAUTH_REFRESH_TOKEN`, `ANTHROPIC_IDENTITY_TOKEN`,
`ANTHROPIC_FOUNDRY_AUTH_TOKEN`, `ANTHROPIC_AWS_API_KEY`,
`ANTHROPIC_CUSTOM_HEADERS`, `CLAUDE_CODE_CLIENT_KEY` and its
passphrase (secret-bearing: set them through `credentials:`), plus
`ANTHROPIC_CONFIG_DIR`, `CLAUDE_CODE_OAUTH_SCOPES`,
`CLAUDE_CODE_CLIENT_CERT`, `CLAUDE_CODE_USE_ANTHROPIC_AWS`,
`CLAUDE_CODE_USE_MANTLE`, the `CLAUDE_CODE_SKIP_*_AUTH` switches,
`ANTHROPIC_AWS_BASE_URL`, `ANTHROPIC_AWS_WORKSPACE_ID` and
`ANTHROPIC_BEDROCK_MANTLE_BASE_URL` (selectors: set them in your
profile's `env:`). The Azure default-credential variables
(`AZURE_CLIENT_ID`, `AZURE_TENANT_ID`, `AZURE_CLIENT_SECRET`,
`AZURE_CLIENT_CERTIFICATE_PATH`, `AZURE_CLIENT_CERTIFICATE_PASSWORD`,
`AZURE_FEDERATED_TOKEN_FILE`, `AZURE_USERNAME`, `AZURE_PASSWORD`) are
scrubbed like `AWS_*`. The built-in `azure` capability now re-admits
all eight (as `aws` and `gcp` re-admit theirs), so a profile with
`capabilities: [azure]` keeps an exported service principal; without it
they are removed.

#### Pi's provider variables are scrubbed

The credential-clean environment now covers every provider variable
in Pi's provider docs. Removed from your shell's environment and, in a
profile, settable only through `credentials:`: `ANTHROPIC_OAUTH_TOKEN`,
`GOOGLE_CLOUD_API_KEY`, `CLOUDFLARE_API_KEY`, `COPILOT_GITHUB_TOKEN`,
`HF_TOKEN`, `AI_GATEWAY_API_KEY`, `ANT_LING_API_KEY`, `NVIDIA_API_KEY`,
`CEREBRAS_API_KEY`, `ZAI_API_KEY`, `ZAI_CODING_CN_API_KEY`,
`OPENCODE_API_KEY`, `RADIUS_API_KEY`, `TYPESAFE_API_KEY`,
`FIREWORKS_API_KEY`, `TOGETHER_API_KEY`, `BASETEN_API_KEY`,
`KIMI_API_KEY`, `META_API_KEY`, `MINIMAX_API_KEY`,
`MINIMAX_CN_API_KEY`, `MOONSHOT_API_KEY`, `QWEN_TOKEN_PLAN_API_KEY`,
`QWEN_TOKEN_PLAN_CN_API_KEY`, `XIAOMI_API_KEY` and the three
`XIAOMI_TOKEN_PLAN_*_API_KEY` names. Removed from the shell but
settable in a profile's `env:`: `AZURE_OPENAI_BASE_URL`,
`AZURE_OPENAI_RESOURCE_NAME`, `AZURE_OPENAI_DEPLOYMENT_NAME_MAP`,
`AZURE_OPENAI_API_VERSION`, `CLOUDFLARE_ACCOUNT_ID`,
`CLOUDFLARE_GATEWAY_ID` and `GOOGLE_CLOUD_LOCATION`. This applies to
every agent: if another tool the agent runs needed an exported
`HF_TOKEN` or `CLOUDFLARE_API_KEY`, bind it with `credentials:`.

A profile launch that selects its own config dir also drops an
exported `PI_CODING_AGENT_SESSION_DIR`, which would otherwise put
every Pi profile's sessions in one shared dir.

#### Codex profile launches scrub the keys custom providers name

A Codex custom model provider reads its key from the variable named by
`model_providers.<id>.env_key` in `config.toml` (and header values
from `env_http_headers`). aide now reads `config.toml` and the
`<name>.config.toml` profile files in `~/.codex`, an exported
`CODEX_HOME` and every profile's Codex dir, and removes the variables
they name from the environment every configured launch and `aide
sync` subprocess inherits from your shell, so an exported
`CORP_LLM_KEY` no longer reaches any agent. Bind such a key with a
custom `credentials:` entry (`env: [CORP_LLM_KEY]`,
`harnesses: [codex]`). Only names are read. When the Codex home being
launched has an invalid or unreadable file, the launch prints a note
naming the file and continues.

#### Profile dirs differing only by case are refused on macOS and Windows

Two profiles whose config dirs differ only by letter case (for example
`config_dir_name: work` and `config_dir_name: Work`) name one dir on a
case-insensitive filesystem, so they would share a login. The loader
already refused them once the dir existed; it now refuses them before
either dir is created, with the usual "profiles share a native config
dir" error. On a case-sensitive macOS volume such a pair is refused
too; give one of them a different name.

#### Provider keys in a profile's env: stop the launch unless translatable

A secret-bearing provider credential in a profile's `env:` (API keys
and tokens such as `ANTHROPIC_API_KEY`, `OPENAI_API_KEY`,
`CODEX_API_KEY`, `AWS_BEARER_TOKEN_BEDROCK`, or auth-file selectors such
as `ANTHROPIC_IDENTITY_TOKEN_FILE`) is no longer passed through as
general environment. Non-secret provider selectors and endpoints
(`CLAUDE_CODE_USE_BEDROCK`, `CLAUDE_CODE_USE_VERTEX`,
`ANTHROPIC_VERTEX_PROJECT_ID`, `OPENAI_BASE_URL`, ...) stay ordinary
`env:` configuration, with no warning; their shell copies are still
scrubbed.

- A value that is a single `{{ .secrets.<key> }}` reference, with the
  profile's `secret:` store, keeps working: it becomes an implicit
  credential binding selected by default, and aide prints a note once
  per run with the `credentials:` block to replace it with. Your
  config is never rewritten.
- Any other value (a literal key, a longer template, a reference
  without `secret:`) stops the launch with `provider credential in
  env`, naming the variable (never its value) and showing the
  `credentials:` block to use instead.
- A provider credential, selector or endpoint in `.aide.yaml` `env:`
  stops the launch: repository config cannot select provider
  credentials or redirect them to another endpoint. Set selectors and
  endpoints in your profile's `env:` instead.
- Cloud and `scrub_env:` names in `env:` still pass through with the
  existing warning.
- `aide env set` refuses provider API keys and tokens, pointing to a
  `credentials:` binding instead, and sets selectors and endpoints
  only with `--global`; its help examples no longer use keys.
- **Behavior change:** endpoint variables are now also scrubbed from
  the shell for configured launches: `ANTHROPIC_BASE_URL`,
  `ANTHROPIC_BEDROCK_BASE_URL`, `ANTHROPIC_VERTEX_BASE_URL`,
  `ANTHROPIC_FOUNDRY_BASE_URL`, `ANTHROPIC_FOUNDRY_RESOURCE`,
  `OPENAI_API_BASE`, `AZURE_OPENAI_ENDPOINT` and
  `GOOGLE_GEMINI_BASE_URL`. If you export one (for example a proxy
  URL), set it in the profile's `env:`.

#### `.aide.yaml` can no longer choose the secret store

A repository `.aide.yaml` (main checkout or worktree) that sets
`secret:` now fails to load with `profile not allowed in project
override: secret: belongs to a profile`, whether or not the file is
trusted. Before, it replaced the profile's own store, so a repository
could make a work launch decrypt your personal secrets. Move the
`secret:` line into the profile in your aide config.

#### `.aide.yaml` can no longer set an agent's config dir

A repository `.aide.yaml` whose `env:` sets an agent's config-dir
variable (`CLAUDE_CONFIG_DIR`, `CODEX_HOME`, `PI_CODING_AGENT_DIR`,
`COPILOT_HOME`, `GEMINI_HOME`), or another variable that moves an
agent's config or state (`AMP_HOME`, `CURSOR_CONFIG_DIR`,
`GOOSE_PATH_ROOT`, `OPENCODE_CONFIG`, `PI_CODING_AGENT_SESSION_DIR`),
now stops the launch with a `repository config cannot choose`
error: that agent config dir holds the login, so a
repository could otherwise pick, or share, the login a profile runs
under. `aide env set` refuses those names without `--global`. Give the
profile its dir in your aide config (`config_dir_name:`, `config_dir:`
or `config_dirs:`).

#### Profiles sharing an agent's global login no longer launch

Profiles with no config dir of their own (no `config_dir_name:`,
`config_dir:`, `config_dirs:`, or legacy `profile:`/`profile_dir:`,
and no `CLAUDE_CONFIG_DIR`-style dir variable in `env:`) all run each
agent from its global dir, such as `~/.claude`, so they share one
login. When two or more profiles are in that state, launching any of
them stops with `profiles share a native config dir: profiles [...]
all run claude from its global login ~/.claude`. Other profiles and
commands keep working; `aide status`, `aide which` and `aide validate`
report it, and `aide profile migrate` lists the profiles. The error
names the exact line to add to each profile but one (for example
`profiles.personal: add config_dir_name: personal`); the default
profile keeps the global login (unless its own agent cannot select a
config dir, then another profile does), and each of the others needs a fresh
login in its new dir. A profile whose own agent cannot select a config
dir (such as aider) never blocks another profile's launch on its own
agent, but launching it with `--agent claude` is refused while another
profile uses or pins `~/.claude`; the error then suggests launching
without `--agent` or an `env:` `CLAUDE_CONFIG_DIR`. Setting a dir
variable such as `CLAUDE_CONFIG_DIR` in `env:` to the default dir does
not count as a dir of its own.

`aide profile create`, `aide use` and `aide setup` now give a new
profile its own `config_dir_name:` when another profile already uses
that agent's global login, and say so; the first profile on an agent
stays on its global login. A dir name another profile already uses,
or whose dirs another profile already runs from, gets a numeric suffix
(`my-repo-2`).

A config that pins a profile to an agent's global dir (for example
`config_dirs: {claude: ~/.claude}`) while another profile still has no
config dir no longer loads: the pinned profile would look isolated
while sharing that login. The error names both profiles and the fix.
The same holds for a dir variable in a profile's `env:`: two profiles
setting `CLAUDE_CONFIG_DIR: ~/.claude-shared`, or one naming another
profile's dir (such as `~/.claude-work` from `config_dir_name: work`),
no longer load; the error names the profile whose `env:` to change.
Commands that edit `config.yaml` (such as `aide env set --global`)
refuse, with `refusing to write config: it would not load`, a change
that would leave a config aide then refuses to load, and keep the
file as it was.

### ✨ New

#### A built-in bivouac capability

When `~/.config/bivouac` exists, aide now turns on a `bivouac`
capability by itself, so `bivouac list`, `bivouac status` and
`bivouac session merge` run inside the sandbox. A merge used to stop
after advancing `main`, with access denied to
`~/.local/state/herdr/client/endpoints.json`. Before this, `list` and `status` both failed with
`open ~/.local/state/.bivouac-state.json.lock: operation not permitted`,
because that lock file sits beside the state directory, not inside it.

- Read access to `~/.config/bivouac`. It is read-only on purpose:
  `base.pkl` there is evaluated by pkl on your machine.
- Read/write access to `~/.local/state/bivouac`, the lock file, and
  herdr's saved machines in `~/.local/state/herdr/client`.
- Binding a unix socket in the per-user temp dir, which `herdr
  --machine` uses for its ssh connection. The connection itself still
  needs `--with ssh`.
- No ssh, no `~/.pkl/cache`, no network. Add ssh with `--with ssh`.
- Turn it off with `--without bivouac`, or add it by hand with
  `--with bivouac`.
- Default XDG locations only. `up`, `down` and `provision` are not
  covered yet; they may need `~/.pkl/cache` and more.

#### codebase-memory-mcp starts inside the macOS sandbox

The default macOS sandbox now lets the agent open and list the directory
that holds your home directory (`/Users`). codebase-memory-mcp checks
every parent of its cache directory before it starts, and it failed with
`ancestry component validation failed` because that one directory could
be read for metadata but not opened.

- Only that directory's entry names are visible. Nothing inside any
  account's home is readable that was not readable before.
- A home outside `/Users` gets the same single-directory grant for its
  parent.
- A cache outside `~/.cache` (`CBM_CACHE_DIR`) still needs your own
  `readable_extra` and `writable_extra` entries.
- Linux is unchanged.
- See the 2026-10-18 codebase-memory spec in `docs/superpowers/specs/`.

#### aide profile manages profiles; aide context is a deprecated alias

`aide profile` has the subcommands `aide context` had: `list`, `bind`,
`create`, `rename`, `remove`, `set-secret`, `remove-secret` and
`set-default`. Its messages say "profile" (`Created profile "work"`,
`profile "ghost" not found`).

- `aide context ...` still works, with a one-line notice on stderr
  (`aide context is deprecated; use aide profile (same subcommands)`);
  stdout, including `--format json`, is unchanged. It is hidden from
  `aide --help`.
- Every command that took `--context` now takes `--profile` for the
  same selection. `--context` still works, is hidden from help and
  prints `--context is deprecated; use --profile` to stderr; passing
  both with different names stops with `name different profiles`.
- `aide profile set-default --profile <name>` names the profile to make
  the default (the positional argument still works).
- Shell completion offers profile names for `--profile`, `--adopt` and
  the profile argument of `bind`, `rename`, `remove` and `set-default`.
- Hints that pointed at `aide context ...` now point at `aide profile
  ...`; the `set-secret` hint no longer suggests a `--global` flag that
  command never had.
- Human-readable output says "profile" where it said "context": the
  `Profile` row in the `aide which` banner, `Profile: <name>` headers in
  `aide status`, `env list`, `cap audit`, `plugin list`, `mcp list` and
  `hook list`, `aide use`, `aide validate`, `aide sync --plan`, the
  drift banner (`⚠ profile "work": ...`) and the no-match menu. JSON
  keys are unchanged (see below). The statusline module keeps its name,
  `context`.
- The Claude Code plugin's `/aide context` command is now `/aide
  profile` (`/aide context` stays as a deprecated alias that says so);
  the docs and plugin text say "profile" throughout, and
  `docs/contexts.md` is now `docs/profiles.md`.

#### aide profile migrate converts config.yaml to profiles:

`aide profile migrate` prints your `config.yaml` converted to the
`profiles:` schema and what changes; nothing is written.
`aide profile migrate --write` keeps the original as
`config.yaml.bak` (or `config.yaml.bak-<timestamp>` if that exists)
and writes the new file atomically.

- Converts `contexts:` to `profiles:`, `default_context:` to
  `default_profile:`, and in each profile `profile:` to
  `config_dir_name:` and `profile_dir:` to `config_dir:`, so every
  native login dir stays where it is.
- Provider keys in `env:` templated from the profile's own store
  (`ANTHROPIC_API_KEY: "{{ .secrets.anthropic_key }}"`) become
  `credentials:` bindings selected by `default_credentials:`. Custom
  names are limited to the profile's agent with `harnesses:` (a
  profile whose agent is not a known agent stops with a message to
  move that key by hand). Values are never read or printed. A key that
  cannot be translated stops the migration with the replacement block
  to write.
- Comments and unrelated keys are kept; blank lines and indentation are
  normalized. The result is checked as a config, and every profile's
  `env:` must match the original except the moved keys, before it is
  written.
- A `contexts:`/`profiles:` section that uses YAML anchors, aliases or
  merge keys (`&name`, `*name`, `<<:`) stops with `cannot migrate
  shared YAML nodes`, since editing a shared node would change other
  profiles too; expand it by hand and re-run.
- The preview prints the whole config, including literal `env:`
  values.
- Profiles that use an agent's global login (no config dir of their
  own, so they run from e.g. `~/.claude` and share one login) are
  listed and left unchanged. `--adopt <profile>` keeps the global login
  of that profile's own agent by adding `config_dir_name: <profile>`
  and `config_dirs: {<agent>: <default dir>}`. Because of the
  `config_dir_name:`, launching that profile under another agent
  (`--agent codex`, ...) then uses a fresh `~/.<agent>-<profile>` dir
  that needs its own login, not that agent's global login; the preview
  lists those dirs. Adopting two profiles onto the same dir is refused,
  and so is adopting one while another profile still has no config dir
  of its own (it would keep running from the adopted dir). No login dir
  is copied.
- `--write` also updates provisioning state for profiles synced against
  the old file, so the rewrite is not reported as drift and nothing aide
  installed is treated as new. Profiles that had already drifted still
  report it.
- `.aide.yaml` files are never changed, and launching aide never
  rewrites the config.

#### Credential bindings select provider API keys per profile

A profile can name its provider API keys under `credentials:` and pick
the ones to inject with `default_credentials:`:

```yaml
profiles:
  work:
    agent: claude
    credentials:
      anthropic: {backend: age, store: work, key: anthropic_api_key}
    default_credentials: [anthropic]
```

- Each binding is one key in an age-encrypted secret store (named like
  `secret:`). Built-in names set the conventional variable:
  `anthropic`, `openai`, `openrouter`, `codex`, `gemini`. Other names
  must declare `env:` and `harnesses:`; `harnesses:` optionally
  restricts a built-in too.
- Only selected bindings are injected and only their stores are
  decrypted. Bindings resolve from the selected profile only.
- The launch stops before anything runs when a selected binding is
  undefined, its store or key is missing, or `--credential` names one
  that does not allow the launching agent. `default_credentials:` and a
  saved selection skip bindings restricted to other agents, so one
  profile can list a key for each agent it runs.
- A config with a malformed binding fails to load with `invalid
  credentials` (backend other than `age`, missing `store:`/`key:`,
  custom name without `env:`/`harnesses:`, unknown agent, undefined
  `default_credentials:` entry).
- Env names a binding declares are scrubbed from the inherited shell
  environment, like `scrub_env:`. A binding cannot set `PATH`, `HOME`
  and other process essentials, `AIDE_*`, the age key variables, or an
  agent config-dir variable such as `CLAUDE_CONFIG_DIR`.
- `harnesses:` matches the agent an `agents:` entry runs, so an alias
  such as `my-claude` with `binary: claude` counts as `claude`.
- Routes: `for: {claude: {env: [...], set: {...}}, pi: {...}}` gives one
  binding a route per agent, carrying the endpoint and provider
  switches with the key, so `--agent` alone picks the right credential.
  A binding without `store:`/`key:` is keyless (e.g. Bedrock with
  `capabilities: [aws]`). A provider endpoint in a profile's `env:`
  now stops a launch that injects a key from `credentials:` unless a
  selected route sets it (`provider endpoint outside the credential
  route`); `aide profile migrate` moves legacy endpoints into the
  route. `default_credentials:` that collide on a variable for the
  same agent fail to load, and an agent with no route prints a note
  that it uses its own login.
- `aide --diagnose` lists the secret stores of the selected bindings
  (paths only).
- The launch banner, `aide which` and `aide status` list binding and
  variable names, never values. `--format json` gains `credentials`
  (`name`, `env`) and `auth_mode`; `aide status` also gains
  `credential_error`.

#### Choose the profile, credentials and auth mode for one launch

`aide --profile <name>` launches with that profile instead of the one
matched for the directory. `aide --credential <name>` (repeatable)
injects those credential bindings for this run, replacing the saved
and default selection rather than adding to it; `--credential=` selects
none. `aide --auth login` injects no credentials so the agent uses
its own login; `--auth api` is the default. None of these flags ever
change saved preferences.

- Precedence: profile from `--profile`, else match rules, else
  `default_profile`. Agent from `--agent`, else the repository's
  saved preference (`aide project set`), else a trusted `.aide.yaml`
  `agent:`, else the profile's `agent:`. Credentials from
  `--credential`/`--auth login`, else the saved selection, else
  `default_credentials:`. A note is printed when a saved agent
  overrides a different `.aide.yaml` agent.
- `aide which` and `aide status` accept the same four flags and report
  how each choice was made: `--format json` gains `profile_source`
  (`flag`, `match`, `default`, `prompt`), `harness_source` (`flag`,
  `preference`, `repository`, `profile`), `credential_source` (`flag`,
  `auth`, `preference`, `profile`) and `credential_selection` (binding
  names only); `aide status` also gains `notices` and a `Selected by:`
  line.
- `--profile`, `--agent` and `--credential` complete profile, agent
  and credential names in shells; `--auth` completes `api`/`login`.
- Read-only commands that take `--context` also take `--profile` for
  the same selection: `aide cap list`, `aide cap audit`, `aide env
  list`, `aide hook list`, `aide plugin list`, `aide mcp list`,
  `aide sandbox show`, `aide sandbox test`, `aide sync`, `aide adopt`
  and `aide statusline`. Giving both with different names is an error.
- `aide prompt` and `aide statusline` show the agent saved for the
  repository, as a launch would run it. They select exactly as a
  launch does: a saved agent the launch would reject is not shown,
  and `aide statusline --profile <name>` applies the trusted
  `.aide.yaml` the way `aide --profile <name>` does, so a malformed
  `.aide.yaml` now makes it fail as the launch would.
- `aide sandbox show` and `aide sandbox test` select the profile and
  agent the way a launch does, including the agent saved for the
  repository, and print the notices a launch would print. `aide sync`,
  `aide adopt`, `aide plugin list`, `aide mcp list` and `aide hook
  list|remove` still act on the profile's own agent, and print a note
  when the repository saved a different agent.
- Without a `config.yaml`, `--profile`, `--credential` and `--auth api`
  stop with a pointer to `aide init`; `--auth login` is accepted, since
  without a config aide injects nothing and the agent uses its own
  login.
- When no profile matches and none is the default, the error now
  suggests `aide --profile <name>` for a one-off launch (it suggested
  `aide use`, which saves a match rule).

#### aide project saves a repository's agent and credentials

`aide project set --agent pi --credential openrouter` saves this
repository's default agent and credential selection under the
current profile (or `--profile <name>`). `aide project clear [--agent]
[--credential]` removes them and `aide project show [--format json]`
prints them.

- Preferences are user-local, in `preferences.json` in aide's state
  directory (`$XDG_STATE_HOME/aide`, default `~/.local/state/aide`),
  keyed by profile and the normalized `origin` remote, so worktrees and
  clones share them under one profile but not across profiles. They
  hold names only, never secret values or models, and never change
  `.aide.yaml` or `config.yaml`.
- `set` changes only the fields given and refuses an agent or
  credential the profile cannot launch. `--no-credentials` saves an
  explicit empty selection (native login); `clear --credential` unsets
  the selection so `default_credentials:` applies again.
- Outside a git repository, or without an `origin` remote, `set` and
  `clear` refuse with `no repository identity`.
- A preferences file written by a newer aide, or one aide cannot read,
  stops `aide project` and launches in repositories with an `origin`
  remote (`unsupported preferences file version` / `unreadable
  preferences file`) instead of being overwritten. The error names the
  file; `aide project clear --reset` moves it aside to
  `<file>.corrupt-<timestamp>` and starts empty.
- `aide which`, `aide status`, `aide project set` and the statusline
  apply the same `.aide.yaml` trust gate as a launch, so an untrusted
  or denied file contributes nothing to what they report or check.
  `aide statusline --profile <name>` also shows the saved agent.

#### Linked git worktrees inherit the main checkout's .aide.yaml

A linked worktree (`git worktree add`) now applies the main checkout's
`.aide.yaml`, then its own on top. aide finds the main checkout through
git, never by looking in parent directories. A main checkout, plain
clone, worktree of a bare repository or submodule applies only its own
file, once. See "Linked git worktrees" in `docs/configuration.md`.

- The worktree's scalars win; `capabilities` are a stable union and the
  worktree's `disabled_capabilities` may remove inherited ones; `env:`
  merges key by key (provider credentials, selectors and endpoints stay
  forbidden in both files); sandbox `_extra` lists combine.
- Each file is trusted on its own. An untrusted or denied file
  contributes nothing while the other still applies, and changing the
  main checkout's file withdraws its approval. The banner, `aide which`
  and `aide status` list each untrusted file separately (`trust_files`
  in `aide which --format json`, `project_trust` in `aide status`),
  name the command that approves an inherited file, and list grants
  inherited from the main checkout (capabilities, writable and readable
  paths, replaced guard or deny lists, `allow` rules, network,
  removed guards, `allow_subprocess`, `clean_env: false`, MCP servers,
  auto-approve) labeled with that file (`inherited_grants`). `aide
  prompt`, `aide cap list|audit`, `aide env list`, `aide sandbox
  show|test` and the statusline use the same per-file check, so an
  untrusted file never shows as active or restyles the statusline.
- Only a worktree the repository itself registers inherits: a `.git`
  file that merely names another repository inherits nothing and
  prints a warning.
- A worktree whose main checkout is deleted, moved or unreadable stops
  the launch with `main checkout unavailable`, naming the missing path
  and the remedies (restore it, `git worktree repair` / `git worktree
  prune`, or `--ignore-project-config`).
- Relative sandbox paths in any `.aide.yaml` now mean paths under the
  root of the checkout that really holds the file (its directory with
  symlinks resolved, asked of git), not the directory you launch from
  or a repository that merely links to it. If that checkout cannot be
  determined, a file with relative paths is not applied and aide
  warns.
- The project root (`{{ .ProjectRoot }}` in `env:` templates, the
  sandbox's writable root, the banner and variant consent) is now
  found the way the trust gate finds a checkout: symlinks resolved,
  and ignoring an inherited `GIT_DIR`/`GIT_WORK_TREE`. A non-git dir
  reached through a symlink gets its resolved path, and a bare-dotfiles
  setup (`GIT_DIR=~/.dotfiles GIT_WORK_TREE=~`) no longer makes all of
  `$HOME` the writable root.
- `aide cap enable|disable` and `aide env set|remove` in a worktree
  edit the worktree's own `.aide.yaml` (created if absent); `--main`
  edits the main checkout's. Project edits now keep a file as written
  instead of saving load-time changes into it.

#### aide trust, deny and untrust take a path

`aide trust [path]`, `aide deny [path]` and `aide untrust [path]` act
on the named `.aide.yaml` (default: the one in the current directory).
Approvals are keyed on where the file is found (its parent
directories symlink-resolved, so a checkout reached through a
symlinked directory shares one approval) and its contents. A
`.aide.yaml` that is itself a symlink needs its own approval at its
own location: approving the file it points to never approves the
link, and trust prompts and `aide trust` show both the link and its
target. Approvals recorded through a symlinked directory, and
approvals of a `.aide.yaml` that is itself a symlink, made before this
release are asked for again once.

#### aide skillshare targets points Skillshare at your profile dirs

`aide skillshare targets` generates Skillshare account targets
(`agent:` + `config_dir:`), named `aide-<profile>-<agent>`, for every
profile and agent (`claude`, `codex`, `pi`) that launches from a
config dir of its own, using the same dirs the launch uses. It
previews by default; `--write` merges them into Skillshare's config
(`$SKILLSHARE_CONFIG`, else `$XDG_CONFIG_HOME/skillshare/config.yaml`,
else `~/.config/skillshare/config.yaml`) atomically with a `.bak`,
keeping everything else and removing only stale `aide-*` targets. Run
`skillshare sync` afterwards; aide never runs it. Default dirs, dirs
two targets would share and agents without Skillshare account
targets are listed, not written. No credential is written, and MCP
assignments are not generated yet (see `docs/skillshare.md`). `aide
sync` provisioning is unchanged.

#### skillshare capability lets sandboxed agents read Skillshare's skills

A new built-in `skillshare` capability is auto-included whenever
Skillshare's config dir (`$XDG_CONFIG_HOME/skillshare`, else
`~/.config/skillshare`) exists. It grants read-only access, on macOS
and Linux, to Skillshare's `skills/` and `agents/`,
`~/.local/state/skillshare/plugins` (or `$XDG_STATE_HOME`'s), and the
`sources:` paths in Skillshare's config, by their symlink-resolved
paths, so the skills Skillshare links into a config dir are readable.
A `sources:` path that is relative, `$HOME`, `/`, outside `$HOME`, or
overlaps a sensitive dir, an agent default dir, a profile's config dir
or aide's config dir is not granted. Skillshare's `config.yaml`,
hooks, MCP state, logs, backups and cache are never granted. Opt out
with `disabled_capabilities: [skillshare]` in `.aide.yaml` or
`--without skillshare`. `disabled_capabilities:` now also stops the
`ccstatusline` capability's auto-inclusion.

### 🧰 CLI

#### --format json adds profile keys beside context keys

JSON output now names the profile under new keys with the same value
as the existing ones, which stay: `profile` beside `context` (`aide
status`, `env list`, `hook list`, `plugin list`, `mcp list`, `sandbox
show`, `cap audit`), `profile_name`/`profile_icon` beside
`context_name`/`context_icon` (`aide which`), and
`default_profile`/`profiles` beside `default_context`/`contexts`
(`aide explain`).

#### Provisioning state records ownership per profile

`managed.json` (aide's record of the plugins, MCP servers,
marketplaces and hooks it installed) now stores them under
`profiles` and is written as version 2. Files written by earlier
releases, keyed under `contexts`, are read as before and converted on
the next successful `aide sync` or `aide adopt`: nothing aide manages
is treated as new and no drift is reported by the conversion.

- A state file with a newer version stops `aide sync` with
  `unsupported provisioning state version` and tells you to upgrade
  aide; the file is left unchanged.
- Going back to an earlier aide after a sync is not supported: it
  ignores the `profiles` key and sees nothing as managed.

#### aide's state files honor XDG_STATE_HOME

Provisioning ownership (`managed.json`) and repository preferences
(`preferences.json`) now share one state directory:
`$XDG_STATE_HOME/aide` when `XDG_STATE_HOME` is set to an absolute
path, else `~/.local/state/aide` as before.

- A file that exists only at the old `~/.local/state/aide` location
  keeps being used there, so setting `XDG_STATE_HOME` never loses
  ownership records or saved preferences. Move the file into
  `$XDG_STATE_HOME/aide` to switch.
- aide now ignores a relative `XDG_CONFIG_HOME` or `XDG_DATA_HOME`,
  as the XDG spec requires, like `XDG_STATE_HOME`: config, trust and
  approval records fall back to `~/.config/aide` and
  `~/.local/share/aide` instead of a path relative to the current
  directory.

#### --agent now gets its own profile directory

In a context with `profile:`, `aide --agent pi` exported the context
agent's config directory (e.g. `CLAUDE_CONFIG_DIR=~/.claude-work`) and
gave Pi nothing, so Pi ran against its default `~/.pi`. The profile
directory now belongs to the agent that actually launches
(`PI_CODING_AGENT_DIR=~/.pi-work`).

- The profile is validated against that agent, so `env:` declaring its
  config-dir variable beside `profile:` is now rejected for `--agent`
  launches too.
- The original agent's config-dir variable is no longer passed to the
  agent you switched to.
- `aide which` and `aide status` accept `--agent` to show what such a
  launch would use.

#### Claude profile launches no longer reach the global ~/.claude.json on macOS

When `CLAUDE_CONFIG_DIR` is set (every Claude profile launch), Claude
Code keeps its account and project state in that dir's `.claude.json`.
The macOS sandbox now stops granting the global `~/.claude.json` and
`~/.claude.lock` in that case and grants the profile dir's own lock
(`~/.claude-<profile>.lock`) instead, so one profile cannot read
another login's account state. Launches without `CLAUDE_CONFIG_DIR`
are unchanged. `~/.cache/claude` and Claude's install paths stay
shared: they hold no login state. See "Claude Code's files outside the
config dir" in `docs/sandbox.md`.

#### Claude profile launches flag an apiKeyHelper aide cannot scrub

When the profile's Claude config dir has a `settings.json` that sets
`apiKeyHelper`, the launch prints:

```text
aide: note: ~/.claude-work/settings.json sets apiKeyHelper: Claude Code uses the key it returns ahead of this profile's native login, and aide cannot scrub it
```

The path is shown absolute; the helper's command is never printed. The
launch continues. See "Credential-clean environment" in
`docs/configuration.md` for this limit and for `~/.config/anthropic`.

#### Pi launches flag a stored login that outranks the selected key

Pi uses a provider's entry in its `auth.json` ahead of the
environment, so a key injected from `credentials:` is ignored for a
provider Pi has a stored login or key for. When a Pi launch injects a
binding and the `auth.json` of the config dir it uses (the profile's
own dir, else an exported `PI_CODING_AGENT_DIR`, else `~/.pi/agent`)
holds an entry for that provider, it prints:

```text
aide: note: ~/.pi-work/auth.json has a stored Pi login for openai: Pi uses it ahead of the selected API key (remove it with /logout in Pi, or launch with --auth login)
```

The path is shown absolute. Only provider names are read and printed,
never stored values. The launch continues. See "Credential-clean
environment" in `docs/configuration.md`.

#### aide sandbox test shows the profile's own config dir

`aide sandbox test --profile <name>` printed the sandbox for the
agent's profile-less default dir (on Linux `~/.config/aide/claude`)
instead of the profile's own (`~/.claude-<name>`). It now injects the
profile's config dir as a launch does, and stops with the launch's
error when the profile's dir settings are invalid.

#### Profiles work with agents: aliases of Claude, Codex and Pi

A profile whose `agent:` names an `agents:` entry, such as
`work-claude` with `binary: claude`, stopped with
a `Profiles are not supported` error naming `work-claude`. Profile
support now follows the entry's binary, so the launch gets the
agent's profile dir (`CLAUDE_CONFIG_DIR=~/.claude-<name>`) and its
sandbox module, which grants that dir. `aide which`, `aide status`,
`aide sandbox test` and `aide project set` resolve the alias the same
way.

#### remote_name in match rules now takes effect

A remote rule with `remote_name: upstream` was documented but ignored:
every remote rule was matched against `origin`. Each rule now matches
against the remote it names, so `remote_name: upstream` matches only
the `upstream` remote. Rules without `remote_name` still use `origin`.

#### Context matching is deterministic on ties

When two contexts scored equally for the same directory (for example,
identical remote rules), the winner could change from one run to the
next. The context whose name sorts first now always wins.

#### Remote matching normalizes host case, trailing slashes and ports

Remote URLs and remote rules are now compared in a normalized
`host[:port]/org/repo` form.

- Host names are case-insensitive: `GitHub.com/org/repo` matches a
  `github.com` remote, including in globs such as `GitHub.com/org/*`.
- A trailing `/` on a rule or a remote URL no longer prevents a match.
- **Behavior change:** a non-default port is now part of the identity.
  A rule `git.example.com/org/repo` no longer matches
  `ssh://git@git.example.com:2222/org/repo.git`; write the rule as
  `git.example.com:2222/org/repo` (or as the full URL). The same
  applies to globs: `git.example.com/*` becomes
  `git.example.com:2222/*`. A scheme's default port (`:22` for ssh,
  `:443` for https, `:80` for http, `:9418` for git) is dropped, so
  `ssh://git@github.com:22/org/repo.git` still matches
  `github.com/org/repo`.
- Match reasons in the launch banner, `aide which` and JSON
  `match_reason` no longer show credentials from a rule saved as a raw
  URL with a token; the rule is shown normalized.
- `aide context bind` saves remote rules in the normalized form
  (`github.com/org/repo`) instead of the raw URL, so tokens or
  passwords embedded in a remote URL are no longer copied into
  `config.yaml` or printed. A remote that is a local path (no host) is
  bound by folder path instead. Rules saved by earlier releases keep
  matching.

#### AIDE_PROFILE is exported alongside AIDE_CONTEXT

Launched agents and their child processes now see `AIDE_PROFILE` with
the resolved context name, the same value as `AIDE_CONTEXT`, which is
kept for existing scripts. `aide statusline` reads `AIDE_PROFILE` and
falls back to `AIDE_CONTEXT` for sessions started by older releases.

#### Clear error for profiles on agents without profile support

Selecting a context with `profile:` for an agent that cannot isolate a
profile now stops with a `Profiles are not supported` error that
names the agent (`"opencode"`). Before, OpenCode and Cursor reported a driver-level
message, and Aider, Amp, Goose and unknown agents reported `no
provisioner registered for agent`.

- Profiles remain supported for Claude, Codex, Pi, Copilot and Gemini.

#### aide sync works for cursor-agent contexts

A context with `agent: cursor-agent` (the name `aide` discovers and
sandboxes) failed `aide sync` and the plugin/hook commands with `no
provisioner registered for agent "cursor-agent"`, because the Cursor
driver was registered as `cursor`. Both names now reach the Cursor
driver, so its hooks are synced, and the launch banner shows the
Cursor icon for `cursor-agent`. Existing `agent: cursor` contexts and
their sync state are unchanged.

### ⚙️ Configuration

#### config.yaml accepts profiles: alongside the legacy contexts:

`config.yaml` can now declare `profiles:` and `default_profile:`
instead of `contexts:` and `default_context:`. Both load the same way;
existing `contexts:` files keep working and are not rewritten. Commands
that save the config write back whichever form the file used.

- Inside a `profiles:` entry the native config-dir keys are
  `config_dir_name:` (was `profile:`) and `config_dir:` (was
  `profile_dir:`). The old names are rejected there with
  `profiles.<name>: profile: is a contexts: key; under profiles: use
  config_dir_name:`; the new names under `contexts:` are rejected the
  same way. `sandbox: {profile: ...}` (a named sandbox policy) is
  unchanged.
- A file that mixes the two forms (e.g. `contexts:` with `profiles:`,
  or `default_context:` with `default_profile:`) stops with `config
  mixes profiles: and contexts: declarations`, naming the keys found
  and how to convert.
- **Behavior change:** two profiles (or contexts) whose agent config
  dirs would be the same directory now stop with `profiles share a
  native config dir`. This covers symlinks, a symlinked parent of a
  dir not created yet, and (for existing dirs) case-insensitive
  filesystems. Because `--agent` can launch any profile under any
  agent, every agent is checked: `work-claude` (agent claude) and
  `work-codex` (agent codex) both with `profile: work` collide on
  `~/.claude-work`. The error explains why and how to keep both
  logins: merge the two into one profile, or separate them with
  `config_dirs` (for example `profile: work-codex` plus
  `config_dirs: {codex: ~/.codex-work}` on `work-codex`, and
  `config_dirs: {codex: ~/.codex-work-claude}` on `work-claude`).
- New `config_dirs:` map on a profile (or context) sets the config dir
  for individual agents, e.g. `config_dirs: {claude: ~/.claude-work,
  codex: ~/.codex-acme}`. For each agent aide uses its `config_dirs`
  entry if present, else the single `config_dir:`/`profile_dir:` (which
  still applies to every agent), else `~/.<agent>-<config_dir_name>`.
  Keys must be agents with profile support (claude, codex, copilot,
  gemini, pi), paths must be under `$HOME`, and the profile must also
  set `config_dir_name:` (`profile:` under `contexts:`); otherwise
  loading stops with `invalid config_dirs`.
- `.aide.yaml` may name a profile with `profile: <name>`, but it never
  selects or switches the profile. When the launch resolves a
  different profile, or your config does not define that name, aide
  prints a note suggesting `aide --profile <name>` and carries on.
- `.aide.yaml` can never define a profile: `profiles:`, `contexts:`,
  `default_profile:`, `default_context:`, `config_dir_name:`,
  `config_dir:`, `config_dirs:`, `profile_dir:`, `match:`, `credentials:`,
  `default_credentials:` and `secret:` stop with `profile not allowed
  in project override: <key>: belongs to a profile`, whether or not the
  file is trusted.
- Commands that save the config (such as `aide agents add`) now refuse
  to write when the config failed to load, with `refusing to write
  config: it failed to load`. Before, some of them wrote an empty
  config over your `config.yaml`.

### 🔧 Internal

#### Multi-agent profile design

A draft design documents the proposed evolution from contexts to profiles,
profile-isolated agent authentication, selected API credentials, repository
preferences, worktree configuration inheritance, and Skillshare integration.

- Design only; no runtime behavior or configuration format has changed.
- See the 2026-10-16 profiles design spec in `docs/superpowers/specs/`.

## v2.3.0 (2026-10-15)

Pi and OpenCode join the supported agents, the Linux sandbox now gives
every agent its own config directory, plugin/hook/MCP sync is fixed
across agents and profiles, and the toolchain and dependencies are
patched for twelve reachable vulnerabilities.

### ✨ New agents

#### aide can now sandbox Pi

`aide` gains a `provision.Provisioner` driver for Pi
(`earendil-works/pi`, pi.dev), so `aide context create --agent pi` and
`aide launch` work the same way they do for the other supported agents.

- The sandbox grants `~/.pi`, or `$PI_CODING_AGENT_DIR` when set.
  `PI_CODING_AGENT_DIR` is also Pi's profile env key.
- MCP servers are synced into Pi's built-in `mcp.json`
  (`~/.pi/agent/mcp.json`, or under `$PI_CODING_AGENT_DIR`), which
  uses the same `mcpServers` shape as Gemini and Copilot.
- Plugins (Pi "packages") are synced through Pi's own `pi install`,
  `pi remove` and `pi list`, always at user scope.
- Hooks are installed as generated TypeScript extensions in
  `~/.pi/agent/extensions/` (global scope only), which Pi auto-loads.
  `pre_tool` maps to Pi's `tool_call` event and `post_tool` to
  `tool_result`.
- Project-local Pi config still needs Pi's one-time project-trust
  approval; `aide` does not grant it on your behalf.

#### aide can now sandbox OpenCode

`aide` gains a `provision.Provisioner` driver for OpenCode
(`anomalyco/opencode`, opencode.ai), so `aide context create --agent
opencode` and `aide launch` work the same way they do for the other
supported agents.

- MCP servers and plugins are synced by editing `opencode.jsonc`
  directly rather than shelling out to OpenCode's own CLI, which can't
  express non-npm plugin sources (git/local refs) and has no
  list/remove counterpart at all.
- Hooks are installed as generated JS plugin files dropped in
  `~/.config/opencode/plugin/` (global scope only; this driver does not
  write project-scope hooks), which OpenCode auto-loads.

### 🐧 Linux sandbox

#### Agents can write their own config under the Linux sandbox

On Linux, Landlock only grants paths an agent module lists as writable,
and the shared module behind most agents listed none. Pi, OpenCode,
Codex, Gemini, Aider, Amp and Goose ran with no write access to their
own config directory. They now get it, matching macOS.

- Symlinked config dirs resolve to their real targets before being
  granted, as they already did for Claude and cursor-agent.
- macOS sandbox profiles are unchanged.

#### aide sync on Linux now provisions the Claude config dir the sandbox uses

On Linux, a sandboxed Claude launch points `CLAUDE_CONFIG_DIR` at
`~/.config/aide/claude`, but `aide sync` and the Claude permission grant
behind `aide sandbox allow --global` kept writing Claude's default
`~/.claude.json` and `~/.claude/settings.json`. MCP servers, hooks and
directory grants that aide synced were therefore invisible to the
sandboxed agent.

- Sync and grants now apply the same launch-time redirect, so they write
  where the sandboxed Claude reads. A `profile:` or a `CLAUDE_CONFIG_DIR`
  you set yourself still wins, and contexts with `sandbox: false` keep
  using Claude's defaults.
- Run `aide sync` once after upgrading so already-declared MCP servers
  and hooks land in `~/.config/aide/claude`. macOS is unaffected.
- Sync now records which agent config dir it provisioned into. When that
  dir changes (this fix, or switching a context's `profile:`), managed
  MCP servers are reinstalled into the new dir instead of being assumed
  present. Expect each one to be reinstalled once on the first sync
  after upgrading.

#### aide statusline --install targets the sandboxed Claude config on Linux

On Linux, a sandboxed Claude reads its config from
`~/.config/aide/claude`, but `aide statusline claude --install` wrote
the status line into `~/.claude/settings.json`, so it never appeared.
It now writes to the same directory `aide sync` uses. Unsandboxed
contexts and macOS still use `~/.claude`.

#### Launch banner names the agent again under the Linux sandbox

On Linux the startup banner showed `claude → /path/to/aide` because it
was rendered after the sandbox rewrote the command into aide's own
re-exec wrapper. It now shows the agent binary, as on macOS.

### 🔌 Plugin, hook & MCP sync

#### aide sync no longer requires the age key for routine no-op runs

`aide sync` previously decrypted a context's age-encrypted secrets file on
every run, even when nothing about the declared config or the secrets
themselves had changed. That coupled routine sync runs (e.g. as part of a
home-manager activation) to age-key availability and plaintext-secret
handling for no reason.

- Sync now skips decryption entirely when neither `config.yaml` nor the
  encrypted secrets file changed since the last successful sync,
  substituting the already-installed MCP env values instead of
  re-resolving `{{ .secrets.X }}` templates.
- A new `--force-secrets` flag bypasses this gate on demand, forcing a
  real decrypt + re-resolve, the remedy if an installed secret value is
  manually edited or corrupted, since that drift is no longer auto-healed
  on every sync the way it used to be.

#### URL-direct plugins no longer reinstall on every sync

Agents that take plugins as bare install refs (OpenCode, and now Pi)
report installed plugins by that ref (`npm:@acme/hello`), never by the
readable key you chose in `config.yaml`. `aide sync` matched on the key,
so every run reinstalled each such plugin and listed its ref as
unmanaged, and removing an entry from config tried to uninstall the key,
which the agent didn't recognise, leaving the plugin installed.

- Sync and `aide plugin list` now credit a reported install ref to its
  config key, and uninstall by the ref aide recorded when installing it.
- Plugins synced before this fix are matched on the next sync, which
  also records their ref for later removal.

#### aide sync follows an exported agent config dir

If you export `CLAUDE_CONFIG_DIR` (or another agent's config-dir
variable such as `PI_CODING_AGENT_DIR`, `CODEX_HOME`, `GEMINI_HOME` or
`COPILOT_HOME`) in your shell rather than through `profile:`, the agent
reads that directory at launch, but `aide sync` still wrote hooks into
the default one. Sync and `aide statusline --install` now target the
exported directory. A value set in the context's `env:` or `profile:`
still takes precedence.

#### Codex, Copilot and Gemini profiles get their own MCP config and hooks

`aide sync` now writes Codex's `config.toml`, Copilot's
`mcp-config.json` and Gemini's hook scripts under `CODEX_HOME`,
`COPILOT_HOME` and `GEMINI_HOME` when the context's `profile:`, `env:`
or your shell sets them. Before, these files always went to `~/.codex`,
`~/.copilot` and `~/.gemini`, so an agent running under a profile never
saw the MCP servers or hooks aide synced for it.

#### Claude marketplaces keyed by a URL or local path now install their plugins

A Claude marketplace declared by URL or by local path, e.g.
`/abs/path/to/marketplace: [hello]`, was added but its plugins failed to
install with `Plugin "hello" not found in marketplace`, and every sync
tried to add the marketplace again. aide now matches the URL or path to
the name Claude registered the marketplace under and installs
`hello@<marketplace name>`.

#### Hook matchers now apply to OpenCode and Pi

A hook scoped with `matcher:` (e.g. `matcher: shell`) fired on every
tool call under OpenCode and Pi, because neither agent has a matcher
field and the generated hook scripts ignored it. The generated plugin
or extension now checks the tool name and only runs the command for the
matched tool (`shell` maps to `bash` in both). The same command can be
declared with and without a matcher, or under several matchers, each
getting its own file. Hooks without a matcher are unchanged.

#### session_start and session_end hooks now work for OpenCode and Pi

`session_start` and `session_end` hooks were silently skipped for
OpenCode and Pi. They are now installed:

- OpenCode: `session_start` runs when a top-level session is created
  (subagent sessions are skipped); `session_end` runs when OpenCode
  exits normally, not when it is killed.
- Pi: `session_start` and `session_end` map to Pi's `session_start`
  and `session_shutdown`, skipping extension reloads.
- As with Claude, both also fire when you switch to a new or resumed
  session.

#### aide plugin list labels URL-direct sources by type

`aide plugin list` showed `marketplace npm:@x/y` or
`marketplace file:///...` for plugins declared with a URL-direct
source, because every unrecognised source fell back to `marketplace`.
The DECLARED column now names the source type: `npm`, `file`, `url`
(http/https), `git`, `github` (including bare `owner/repo`) or
`local`. Plugins installed from a marketplace still show `marketplace`.

### 🧰 CLI

#### aide commands support --format json output

Read/query commands (`aide which`, `aide cap list`, `aide status`, etc.)
now support `--format json` for machine-readable output, alongside the
existing human-readable default.

- Every read/query command accepts `--format json` (default: `human`).
- The `$AIDE_FORMAT` environment variable sets the default when
  `--format` isn't passed explicitly (flag > env var > `human`).
- `aide explain`'s existing `--format human|agent|json` flag is
  unchanged.
- Errors under `--format json` are emitted as a `{"error": "..."}` JSON
  object instead of plain text.

#### aide which reports the real sandbox tier

`aide which` always printed `sandbox: disabled`, even for contexts
that launch under Landlock or Seatbelt, because only the launch path
evaluated the isolation tier. It now shows the same tier the launch
banner does, e.g. `primary (Landlock ABI 4)`, and `--format json`
includes `isolation_tier`.

#### aide sandbox allow/deny now sync Claude's own permission store

`aide sandbox allow <dir>` and `aide sandbox deny <dir>` previously only
touched aide's own OS-level sandbox policy (`readable_extra`/
`writable_extra`/`denied_extra`), leaving the same edit to be made by hand in
Claude Code's `settings.local.json`/`settings.json`
`permissions.additionalDirectories`. That manual step now happens
automatically as part of the same command.

- The agent-side edit is best-effort: it never fails the aide-side
  mutation, printing a warning instead if it can't be applied.
- Both commands gained a `--no-agent-grant` flag to opt out and keep the
  old aide-only behavior.
- The new `provision.DirectoryGranter` interface is the extension point;
  other agent drivers can implement it later to gain the same behavior.
- `sandbox deny` correctly revokes nested grants (e.g. denying `/repo`
  after allowing `/repo/sub`), not just exact path matches, without
  touching unrelated parent grants.

#### Auto-approve works with Pi and OpenCode

With `yolo: true` in your preferences or a context, or with
`--auto-approve`, launching Pi or OpenCode failed with `--yolo not
supported for agent`.

- OpenCode now gets its `--auto` flag.
- Pi never asks for permission, so it launches unchanged. Its
  `--approve` flag trusts project-local files, a separate decision
  aide leaves to you.

### 🔒 Security

#### Go 1.26.9 and patched dependencies resolve twelve vulnerabilities

govulncheck reported twelve vulnerabilities reachable from aide's code
in the Go standard library and in dependencies, failing the scheduled
security check. Release binaries and the Nix flake now build with
patched versions.

- Go toolchain 1.26.6 → 1.26.9 (`crypto/tls`, `net/http`,
  `html/template`, `net/textproto`). The Nix flake pins Go 1.26.9
  until nixpkgs ships it.
- `golang.org/x/net` v0.56.0 → v0.60.0,
  `go.opentelemetry.io/otel/sdk` v1.44.0 → v1.45.0 and
  `google.golang.org/grpc` v1.83.1 → v1.83.2.
