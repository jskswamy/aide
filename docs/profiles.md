# Profiles

## What is a Profile

A profile binds an agent, credentials, and sandbox policy to a set of directories or git remotes. When you run `aide`, it resolves which profile applies to your current working directory and launches the correct agent with the right environment.

Profiles were called contexts in earlier releases. A config that still
uses `contexts:` and `default_context:` loads unchanged; see
[Migrating from contexts](#migrating-from-contexts).

Profiles live in `~/.config/aide/config.yaml` under the `profiles:` key:

```yaml
agents:
  claude:
    binary: claude

profiles:
  work:
    agent: claude
    secret: work
    match:
      - path: ~/work/*
      - remote: "github.com/acme/*"
    config_dir_name: work      # claude runs from ~/.claude-work
    credentials:
      anthropic: {backend: age, store: work, key: ANTHROPIC_API_KEY}
    default_credentials: [anthropic]

default_profile: work
```

Each profile contains: `agent`, `secret`, `env`, `match`, `sandbox`, `capabilities`, and optionally `yolo`, `config_dir_name`, `config_dir`, `config_dirs`, `credentials` and `default_credentials` (see [Configuration](configuration.md)).

## Match Rules

Two rule types select a profile based on the current working directory or git remote.

**Path rules** match the working directory. Tilde expands to the home directory.

```yaml
match:
  - path: ~/work/*        # any subdirectory of ~/work
  - path: ~/work/acme     # exact directory
  - path: ~/clients/**    # recursive glob
```

**Remote rules** match the git remote URL. Each rule is checked against the `origin` remote unless `remote_name` selects another one; a rule with `remote_name: upstream` matches only the `upstream` remote.

```yaml
match:
  - remote: "github.com/acme/*"
  - remote: "github.com/acme/api"      # exact match
  - remote: "*.internal.corp/*"
    remote_name: upstream
```

Remote URLs and non-glob rules are normalized to `host[:port]/org/repo` before comparison, so SSH (`git@host:org/repo.git`), `ssh://` and `https://` forms all match the same rule. The host is lowercased, a trailing `.git` or `/` is dropped, a port is kept unless it is the scheme's default (`ssh://git@git.example.com:2222/org/repo.git` becomes `git.example.com:2222/org/repo`, while `ssh://git@github.com:22/org/repo.git` becomes `github.com/org/repo`), and user names, passwords or tokens embedded in the URL are discarded. Match reasons shown by `aide which`, `aide status` and the launch banner never include those credentials either. `aide profile bind` saves remote rules in this normalized form.

## Resolution Order

aide scores every match rule and picks the highest-scoring profile:

| Match type   | Base score | Tiebreaker |
|--------------|-----------|------------|
| Exact path   | 300 | + pattern length |
| Exact remote | 250 | + pattern length |
| Glob path    | 200 | + pattern length |
| Glob remote  | 100 | + pattern length |

An exact remote match (`remote: "github.com/acme/api"`) deliberately outranks a glob path match - it's more specific even though it's not a path rule. Longer patterns beat shorter patterns within the same tier. When two profiles still score equally, the one whose name sorts first wins. If no rule matches, `default_profile` is the fallback.

`.aide.yaml` does not add a fifth scoring tier - it's a field-by-field override merged on top of whichever profile wins the scoring above (some fields, like `env`, merge additively; others replace only if set). See [Per-Project Override](#per-project-override) below.

## Per-Project Override

Place `.aide.yaml` at the project root to override fields in the resolved profile for that project. aide merges it on top of the matched profile.

```yaml
# .aide.yaml
agent: claude
env:
  NODE_ENV: development
sandbox:
  writable_extra:
    - /tmp/build
```

Env variables merge additively; the project override wins on key conflicts. All other fields (including `yolo`) replace the matched value when set. `.aide.yaml` cannot set `secret:`: the secret store belongs to the profile.

## Capabilities

The `capabilities` field activates named capabilities for a profile. Capabilities grant the sandbox additional permissions (filesystem paths, environment variables, network access) for specific tools.

```yaml
profiles:
  infra:
    agent: claude
    match:
      - path: ~/infra/*
    capabilities: [docker, k8s, aws]
    secret: work
```

See [Capabilities](capabilities.md) for details on built-in and custom capabilities.

## Managing Profiles

```
aide profile list                         # list all profiles
aide profile create                       # create a new profile interactively
aide profile create work --agent claude --here  # scripted: create and bind CWD
aide profile bind work                    # attach this folder to an existing profile
aide profile bind work --path             # force exact folder path match
aide profile set-secret personal          # set secret (auto-detects CWD)
aide profile set-secret work --profile work
aide profile remove-secret                # clear secret (auto-detects CWD)
aide profile set-default work             # change the default fallback
aide profile rename work work-acme        # rename (updates default_profile too)
aide profile remove personal              # delete a profile
```

`aide profile list` marks the default profile:

```
personal
  Agent:  claude
  Match:  ~/personal/*

work (default)
  Agent:  claude
  Secret: work
  Match:  ~/work/*
```

## The `aide use` Shortcut

`aide use` creates or updates a profile named after the CWD basename and binds it in one step. The first profile created automatically becomes `default_profile`.

```
aide use claude                       # bind CWD to claude
aide use claude --match "~/work/*"    # bind a glob pattern instead of CWD
aide use claude --secret work         # also attach a secret
aide use claude --sandbox strict      # use a named sandbox profile
aide use --profile work               # add CWD as a match rule to existing profile
```

## Codex identities and native settings

Use separate native homes for personal, work, or client identities:

```yaml
profiles:
  personal:
    agent: codex
    config_dir_name: personal
  work:
    agent: claude
    config_dir_name: work
default_profile: personal
```

`aide --profile work --agent codex` launches Codex with
`CODEX_HOME=~/.codex-work`, even though work normally launches Claude.
The personal profile uses `~/.codex-personal`. Applicable credential routes
provide the selected keys and endpoint settings. When none of the default
or saved routes applies to Codex, aide still launches it in the work home
and notes that it uses its own login. Use the agent's native login flow
there; aide does not borrow the personal or global login.

An explicitly incompatible `--credential` selection or a failure to load
a selected credential remains an error. A missing applicable default is
different from a broken selected credential.

Codex settings remain native. For example:

```sh
aide --profile work --agent codex -- --profile deep-review
```

aide selects the work identity and passes `--profile deep-review` unchanged
to Codex. Codex loads the named settings according to its installed version
from the selected home; aide does not create or remember those settings.
Provider and model compatibility also belongs to the agent: supplying a
key or endpoint does not make an unsupported provider compatible.

Codex's real-account isolation admission is still
[pending verification](profiles-verification.md#codex). Automated launcher
checks establish environment and argument handling, not native login,
keyring, refresh, or concurrent-account behavior.

## Multi-Account Setups

A common need is keeping a personal and a work Claude account side by
side - different API keys or logins, different MCP servers, separate
conversation history, no cross-contamination. aide composes two
primitives to make this clean:

1. **One native config dir per profile.** `config_dir_name: work`
   makes aide set the agent's config-dir variable
   (`CLAUDE_CONFIG_DIR=~/.claude-work`, `CODEX_HOME=~/.codex-work`,
   ...) at launch, sync, adopt and list, so each profile keeps its own
   login, settings, MCP servers and history.
2. **One secret store per account** when you use API keys, selected
   with a `credentials:` binding. Without one, the agent uses the
   native login stored in that profile's config dir.

**Walkthrough - personal + work.**

Create one encrypted secret per account (see [Secrets - Quick
Start](secrets.md#quick-start-wiring-an-anthropic-api-key) for the
editor flow):

```sh
aide secrets create personal --age-key age1abc...
aide secrets create work     --age-key age1xyz...
```

Then declare the two profiles:

```yaml
profiles:
  personal:
    agent: claude
    match: [{path: ~/personal/*}]
    secret: personal
    config_dir_name: personal
    credentials:
      anthropic: {backend: age, store: personal, key: anthropic_api_key}
    default_credentials: [anthropic]
  work:
    agent: claude
    match: [{path: ~/work/*}]
    config_dir_name: work      # native login only, no API key
default_profile: personal
```

The first launch in each tree signs Claude Code in (or uses the key)
inside the matching config dir; from then on aide picks the right
state directory automatically based on CWD.

```sh
cd ~/personal/some-project && aide   # uses ~/.claude-personal
cd ~/work/some-project    && aide    # uses ~/.claude-work
aide --profile work                  # this launch only, from anywhere
```

Resulting layout:

```
~/.config/aide/secrets/personal.enc.yaml
~/.claude-personal/   ← Claude Code state for personal account
~/.claude-work/       ← Claude Code state for work account
```

The same pattern extends to N accounts (client-x, oss, ...) - just
add more profile blocks. Two profiles that would resolve to the same
config dir stop with `profiles share a native config dir`.

Per-profile login isolation for each agent is being verified by hand
on macOS and Linux; the steps and their status are in
[profiles-verification.md](profiles-verification.md).

To install skills and other Skillshare resources into each profile's
own config dirs, see [Skillshare](skillshare.md).

## Migrating from contexts

`aide profile migrate` prints your config converted to the `profiles:`
schema without writing anything; `aide profile migrate --write` writes
it, keeping the original as `config.yaml.bak`:

- `contexts:` becomes `profiles:` and `default_context:` becomes
  `default_profile:`. Profile names stay the same.
- Inside each profile `profile:` becomes `config_dir_name:` and
  `profile_dir:` becomes `config_dir:`, so every login dir stays where
  it is.
- Provider keys templated into `env:` from the profile's `secret:`
  store become `credentials:` bindings.
- Profiles with no config dir of their own run the agent from its
  global dir (such as `~/.claude`) and share that one login. They are
  listed and left alone; `--adopt <profile>` keeps the global login
  of its own agent for one of them by adding `config_dir_name:
  <profile>` and `config_dirs: {claude: ~/.claude}`. Launched under
  another agent (`--agent codex`), that profile then uses a fresh
  `~/.codex-<profile>` dir with its own login. Adopting is refused
  while another profile still has no config dir of its own.
- aide refuses to launch a profile that shares a global login with
  another profile; give all but one of them a `config_dir_name:`.
- A section that uses YAML anchors, aliases or merge keys is refused;
  expand it by hand and re-run.
- Provisioning state is updated so the rewrite is not reported as
  drift. `.aide.yaml` files are never changed, and a launch never
  rewrites the config.

`aide context ...` still works as a deprecated alias of `aide profile
...` (it prints a notice on stderr), and `--context` still works
wherever it did, as a deprecated alias of `--profile`.

## Default Profile

The default profile is the fallback when no match rule applies. Change it at any time:

```
aide profile set-default personal
```

`aide profile list` shows it with a `(default)` marker.

## Inspecting

`aide which` shows the matched profile and the reason for the match:

```
$ aide which
Profile:  work
Matched:  path glob match: ~/work/*
Agent:    claude
Secret:   work
```

`aide which --resolve` decrypts the secret and shows resolved environment variable values (redacted):

```
$ aide which --resolve
Profile:  work
Matched:  path glob match: ~/work/*
Agent:    claude (/usr/local/bin/claude)
Secret:   work  [keys: ANTHROPIC_API_KEY]
Env:
  ANTHROPIC_API_KEY  <- secret (sk-ant-........)
```
