# Getting a Scrubbed Provider Key Back

*   **Author:** Krishnaswamy Subramanian
*   **Status:** Superseded (2026-11-10) by
    [Gateways for Every Harness](./gateway-redesign.md). Kept as prior art.
    A seam analysis of this design found two gaps that a per-harness route
    and slot model fixes better than patching this one: a provider key
    taking over Codex or OpenCode from a gateway leaves the harness config
    file still selecting the gateway, so the harness calls it without a
    token; and `IsLLMProvider` cannot see user `provider_env:` keys, so a
    custom key and a gateway would both inject on the same harness.
*   **Date:** 2026-11-10

--------------------------------------------------------------------------------

## Overview

aide removes provider API keys (`ANTHROPIC_API_KEY`, `OPENCODE_API_KEY`,
`GROQ_API_KEY` and about 60 others) from the environment before it starts a
coding agent. This keeps a key exported in the user's shell from reaching every
harness, MCP server, plugin and install script running in the sandbox. The
scrubbing is correct, and this design keeps it.

What is broken is the way back. A user who wants a harness to use a key must
today hand-write a `credentials:` binding in their profile YAML and put the key
into an encrypted store with `aide secrets edit`. The prompt aide shows at
launch defaults to throwing the key away, and `aide env set` refuses provider
keys and prints a YAML template.

This design makes two existing entry points restore a key without YAML:

1.  **The launch prompt** becomes the main path. Provider docs tell users to
    `export XYZ_API_KEY=...`, so users arrive with the key in their shell. When
    aide strips a key the launched harness does not already receive, it offers
    to save it (encrypted) and bind it to the profile, with save as the default.
2.  **`aide env set <PROVIDER_VAR>`** becomes the scripted path for terminals
    without prompts and for CI. It saves and binds the key at profile level.
    `aide env remove <VAR>` undoes it, and `aide env list` shows it.

Encryption uses the existing age-backed secret stores. Users never answer a
question about age: on a terminal, a key is generated on first use and one
notice says where it lives and why it must be backed up.

## The Problem

From aide's position there are three ways a harness gets access to a model:

| Case | What the harness needs | aide's role | State today |
| --- | --- | --- | --- |
| **Login** (subscription or OAuth, inside the harness) | A token in its own config dir | Keep each profile's config dir separate | Works |
| **Known provider + API key** (OpenCode Go, OpenRouter, Groq, Anthropic, ...) | One env var; the harness already knows the URL | Store the key and pass the variable to the chosen harnesses | **Broken: YAML only** |
| **Custom endpoint + key** (self-hosted LiteLLM, corporate gateway) | URL, key, protocol, often a harness config file | Store the key, write harness configs, sync models | Works (`aide gateway`) |

The second case is the one users hit first. The trigger was following
OpenCode's guide for Pi: `export OPENCODE_API_KEY=...`, then
`pi --model opencode-go/<model>`. Under aide:

1.  **The key disappears.** aide scrubs `OPENCODE_API_KEY`, and Pi reports that
    it has no key.
2.  **The prompt points the wrong way.** If this launch has no credential
    bindings at all, aide shows a four-option prompt whose default (`[4] Strip
    and continue`) discards the key. If the profile already passes any other
    key to this launch, there is no prompt at all
    (`internal/launcher/launcher.go:320`): the key is stripped silently.
3.  **The obvious command refuses.** `aide env set OPENCODE_API_KEY ...` exits
    with a YAML template to paste (`cmd/aide/env.go:418`).
4.  **The real path needs internals.** The user must learn the
    `credentials:` schema (`backend`, `store`, `key`, `env`, `harnesses`),
    `default_credentials`, the age secret store, and harness pinning.

Several quieter failures sit in the same area:

*   **A rotated key is ignored.** Once a variable is bound, aide keeps injecting
    the stored value and strips the new one from the shell, with no message.
*   **A binding for another harness blocks the prompt.** A key bound only for
    Claude is stripped under Pi with no prompt.
*   **Making a provider a known name breaks configs.** The launch prompt names a
    binding after the variable (`opencode_api_key`). If `opencode` later becomes
    a known provider name, the next save creates a second binding for the same
    variable, and `checkDefaultRoutes` (`internal/config/credentials.go:289`)
    then refuses to load the profile.
*   **A provider key can silently displace a gateway, or be displaced by one.**
    At launch, a provider key and a gateway cannot both serve one harness; the
    order of `default_credentials` decides which wins
    (`internal/config/gateway.go:353-404`). Nothing reports this.
*   **A failed save loses the old key.** The secret is written before the config
    is validated, and the old value is not kept. The same defect exists in
    `aide gateway add --force` (AIDE-yqk7).

## Goals

*   **One Enter at launch.** A user who exported a provider key and runs `aide`
    on a terminal can save it, encrypted and bound to the profile, by pressing
    Enter once. Before saving, the prompt names the profile, the harnesses and
    the key's last four characters. After saving, aide prints the undo command
    and says the shell export is no longer needed.
*   **One command off a terminal.** `aide env set <VAR> --from-env` or
    `--key-stdin` does the same with no prompt. Missing input fails with a
    non-zero exit and names the flag to use.
*   **Zero YAML.** Saving, listing and removing a provider key never requires
    the user to read or edit a config file.
*   **The launch notices every stripped key.** On a terminal, aide prompts for
    each provider variable in the shell that the launched harness does not
    already receive, including a rotated value and a binding that serves only
    other harnesses. Off a terminal it prints one note per stripped variable
    with the exact command to bind it.
*   **No duplicate bindings.** Saving finds an existing binding by the variable
    it injects (including bindings created by gateways and `for:` routes) and
    updates it. After any save, the profile config loads.
*   **No silent gateway changes.** Any change to which credential a harness
    receives is shown per harness before writing. A save that has no effect is
    never reported as working.
*   **A failed save keeps the old key.** If any step fails, the previous secret
    value is restored.
*   **No questions about age.** Users never pick, enter or generate an age key
    by hand. On a terminal, one is generated on first use with a single notice.
    Off a terminal with no age key, aide fails with a one-line fix rather than
    creating a key that a throwaway CI runner would lose.
*   **Explicit profile when none matches.** When the folder matches no profile
    and aide would fall back to the shared built-in `default` profile, the
    prompt has no default: the user picks a profile, and Enter alone saves
    nothing.
*   **The key never leaks through aide.** It never appears in a flag value, a
    positional argument, command output or a log line.
*   **Any provider without a rebuild.** A user can declare a new provider
    variable and the harnesses that should receive it in user config. From
    then on it is scrubbed, detected and saved like a built-in one.

## Non-Goals

*   **Checking provider/harness compatibility.** aide does not know or check
    which protocols a harness speaks. The user decides which harnesses get a
    key. (Rejected by author.)
*   **Writing harness config files, choosing models, or managing in-harness
    logins.** Pi's `models.json`, `--model` and `/login` stay the user's and the
    harness's concern. aide's existing notice about a stored Pi login that
    overrides an injected key remains as is.
*   **Custom endpoints, base URLs and model sync.** These stay under
    `aide gateway`, unchanged.
*   **Removing or weakening scrubbing.**
*   **A new top-level command** such as `aide provider` or `aide credential`.
*   **Remembering "never ask for this variable".** A stripped key that stays
    exported is offered again on each launch; that is the reminder.
*   **Checking keys against the provider over the network.**
*   **Breaking YAML schema changes.** The only schema change is one additive,
    optional top-level key in user config (`provider_env:`). Existing
    hand-written `credentials:` bindings and `aide gateway` output keep working
    unchanged.
*   **Sharing keys across profiles automatically.** Profiles are the isolation
    boundary. aide never reuses or links a key between profiles on its own.

## Decisions Made During Drafting

*   **Gateway ordering: the new key moves first.** A saved key is placed ahead
    of gateways in `default_credentials`, so on each harness the user selected,
    the key takes over from a gateway. Gateways are not removed from the list,
    so they keep serving every harness the key does not select. The
    per-harness before/after summary is shown first; a terminal asks for
    confirmation and a non-terminal run needs `--force`.
*   **Reuse across profiles: separate copies, explicit friction.** Each profile
    stores its own copy. Sharing happens only when the user points `--store`
    and `--key` at an entry another profile uses, and even then aide names that
    profile and asks for confirmation (or `--force`). aide never compares a new
    value against other profiles' keys.
*   **Harness pre-selection: built-ins plus a user mapping.** The five built-in
    providers keep their harness lists. A new optional `provider_env:` map in
    user config lets the user add any variable and its harnesses without
    rebuilding aide. A variable listed there is also treated as a provider key:
    scrubbed, detected at launch and handled by `env set`. Any other provider
    variable pre-selects only the launched harness.
*   **`env set` key input flags:** `--from-env[=NAME]` and `--key-stdin`.

## Open Questions

*   None at this stage.

## Critical User Journeys (CUJs)

### CUJ 1: Following a provider's docs

*   **Given**: Profile `mypi` matches the folder, its agent is Pi, and the
    shell has `export OPENCODE_API_KEY=oc_sk_...k3Xz`. No binding injects that
    variable.
*   **When**: The user runs `aide` on a terminal.
*   **Then**: Before Pi starts, aide shows
    `[1] Save (…k3Xz) to profile "mypi" for pi (default) / [2] Use for this
    launch only / [3] Strip and continue`. On Enter, the key is encrypted into
    the profile's store, a binding with explicit `env:` and `harnesses:` is
    written, aide prints the undo command (`aide env remove
    OPENCODE_API_KEY`) and says the shell export is no longer needed, and Pi
    starts with the variable set.

### CUJ 2: Scripted setup

*   **Given**: No terminal (CI or a script); an age key already exists; the
    profile is chosen with `--context mypi`.
*   **When**: `printf '%s\n' "$KEY" | aide env set OPENCODE_API_KEY --key-stdin
    --harness pi --context mypi`.
*   **Then**: The key is saved and bound, the command prints the profile and
    binding name, and exits 0. Running it again with the same value prints
    "unchanged" and exits 0. With a different value it exits 3 unless
    `--force` is given.

### CUJ 3: A rotated key

*   **Given**: `OPENCODE_API_KEY` is bound for Pi in `mypi`; the user replaced
    the export with a new key.
*   **When**: The user runs `aide`.
*   **Then**: aide compares the shell value with the stored value in memory,
    sees they differ, and asks `Update stored OPENCODE_API_KEY (…k3Xz →
    …9QpL)?`. Off a terminal it prints one note and injects the stored value.

### CUJ 4: Taking over from a gateway

*   **Given**: Gateway `corp` serves Claude and Pi in profile `work`.
*   **When**: The user saves `ANTHROPIC_API_KEY` for Claude only.
*   **Then**: Before writing, aide shows `claude: gateway corp → anthropic`
    and nothing for Pi, and asks for confirmation. After saving, Claude uses
    the key and Pi still uses `corp`.

### CUJ 5: No profile matches the folder

*   **Given**: The folder matches no profile, so the launch falls back to the
    user's `default_profile` or the shared built-in `default` profile. Either
    fallback reaches every folder without its own profile.
*   **When**: aide offers to save an exported key.
*   **Then**: The save option has no default. The user must choose an
    existing profile or "create a profile for this folder". Enter alone saves
    nothing.

### CUJ 6: A provider aide does not know

*   **Given**: `~/.config/aide/sandbox.yaml` contains
    `provider_env: {DEEPSEEK_API_KEY: [pi, opencode]}`, and the shell exports
    `DEEPSEEK_API_KEY`.
*   **When**: The user runs `aide` under Pi.
*   **Then**: The variable is scrubbed, the save prompt appears with Pi and
    OpenCode pre-selected, and the binding is written as in CUJ 1.

### CUJ 7: A new machine

*   **Given**: The config and encrypted store were copied over, but the age
    key was not. `OPENCODE_API_KEY` is exported.
*   **When**: The user runs `aide`.
*   **Then**: aide reports that the stored key cannot be decrypted and offers
    `[1] Re-save from your shell value (default) / [2] Use the shell value for
    this launch only / [3] Abort`. Option 1 generates a new age key (with the
    one-time notice) and re-encrypts the key.

### CUJ 8: Undo

*   **Given**: `OPENCODE_API_KEY` is bound in `mypi` and no other binding uses
    its stored key.
*   **When**: `aide env remove OPENCODE_API_KEY`.
*   **Then**: The binding and its `default_credentials` entry are removed and
    the stored key is deleted. If another binding uses the same store entry,
    the secret is kept and aide names that binding. For a gateway's variable,
    aide refuses and points to `aide gateway remove`.

## Technical Plan (Architecture)

### Components

```text
                    ┌──────────────────────── user config ────────────────────────┐
                    │ provider_env: {VAR: [harnesses]}   (new, optional)          │
                    │ scrub_env, profiles/<p>.yaml credentials: + default_creds  │
                    └──────────────┬──────────────────────────────────────────────┘
                                   │ read
   ┌───────────────────────┐   ┌───▼─────────────────────────────┐
   │ launch prompt         │   │ config: provider key knowledge  │
   │ (internal/launcher)   ├──►│  IsProviderKeyVar(cfg, VAR)     │
   └──────────┬────────────┘   │  PreselectHarnesses(cfg, VAR)   │
              │                │  BindingsForEnv(ctx, VAR)       │
   ┌──────────▼────────────┐   └───┬─────────────────────────────┘
   │ aide env set/list/rm  │       │
   │ (cmd/aide)            ├──┐    │
   └───────────────────────┘  │    │
                              ▼    ▼
               ┌────────────────────────────────────────┐
               │ providerkey: Plan → Apply (new pkg)    │
               │  1 find binding by VAR                 │
               │  2 build new config in memory, validate│
               │  3 per-harness before/after summary    │
               │  4 snapshot old secret                 │
               │  5 write secret, then config           │
               │  6 on failure restore old secret       │
               └──────────┬─────────────────┬───────────┘
                          │                 │
             ┌────────────▼───┐   ┌─────────▼──────────────┐
             │ secrets:       │   │ config.WriteConfigTo   │
             │ EnsureIdentity │   │ (validates on write)   │
             │ Get/SetKey     │   └────────────────────────┘
             └────────────────┘
```

### 1. Provider key knowledge (`internal/config`)

*   **`provider_env:`** is a new optional top-level map in
    `~/.config/aide/sandbox.yaml`, beside `scrub_env:`, from variable name to a
    list of harness names. It is read only from the user's own config files;
    `.aide.yaml` and project overrides cannot set it, so a repository cannot
    widen which harnesses receive the user's keys. Validation:
    the name must be a valid environment variable name, each harness must exist
    in `internal/harness`, and the name must not be a variable aide uses for its
    own purposes (config-dir redirects, endpoints, selectors).
*   **`IsProviderKeyVar(cfg, name)`** is true when `envscrub.IsProviderSecretName`
    is true or the name is a `provider_env:` key. Every place that today asks
    `envscrub.IsProviderSecretName` for scrubbing, detection or the `env set`
    branch uses this instead when it has a config.
*   **Scrubbing**: `CredentialScrubNames` adds `provider_env:` keys, the same
    way it adds `scrub_env:` names today.
*   **`PreselectHarnesses(cfg, name, launched)`** returns, in order: the
    `provider_env:` list for the variable; else the built-in list when the
    variable is one of the five built-in providers' variables; else only the
    launched harness. With no launched harness (`env set` off a terminal) and
    no list, the caller must pass `--harness`.
*   **`BindingsForEnv(ctx, name)`** (exported from today's `bindingFor`)
    returns every binding that injects the variable: through `env:`, the
    built-in names, any `for.<harness>.env`, or a binding synthesized from a
    gateway.

### 2. The shared save path (new package `internal/providerkey`)

One package used by both the launch prompt and `aide env set/remove`, so the
three current orderings of `default_credentials` collapse into one.

*   **`Plan(cfg, profile, name, harnesses, opts)`** computes everything without
    writing:
    *   **Lookup.** `BindingsForEnv`. Two or more matches: refuse and list them.
        A match synthesized from a gateway: refuse and point to
        `aide gateway`. One match: update only its `store`, `key` and harness
        list (`harnesses:`, or the keys of `for:` when the binding uses
        routes). No match: create a binding named by the provider's built-in
        name when one exists, else the lowercased variable. If that name is
        taken by a binding that injects something else, refuse.
    *   **Explicit fields.** New bindings always get `backend: age`, `store`,
        `key`, `env: [VAR]` and `harnesses:`, so they never depend on built-in
        defaults.
    *   **Ordering.** The binding is moved to the front of
        `default_credentials`. Nothing else is removed from the list.
    *   **Validation.** The planned config is validated in memory with the same
        checks `WriteConfigTo` runs (`ValidateForWrite`, `checkDefaultRoutes`,
        the endpoint-route check that rejects a stray `*_BASE_URL`).
    *   **Summary.** For every launchable harness, run
        `DefaultCredentialsForHarness` on the synthesized context before and
        after. Report each harness whose winning credential changes, for
        example `claude: gateway corp → anthropic`. When a selected harness
        would not receive the key after the change, the plan says so; it is
        never reported as working.
    *   **Shared-store guard.** If any other profile's binding uses the same
        `store` and `key`, the plan names it and requires confirmation.
    *   **Result:** a `Plan` value holding the new config, the summary, the
        store and key, and which confirmations it needs (gateway takeover,
        shared entry, value change).
*   **`Apply(plan, value, confirm)`** writes:
    1.  `secrets.EnsureIdentity` (section 3) to get an encryption key.
    2.  Read the current value at `store`/`key`, noting whether the key and the
        store file existed. If the store exists but cannot be decrypted, stop
        before writing anything.
    3.  Write the secret, then `WriteConfigTo`.
    4.  If the config write fails: write the old value back; if the key did
        not exist, delete it; if the store file did not exist, delete it.
        Return the original error.
*   **`Remove(cfg, profile, name)`**: drop the binding and its
    `default_credentials` entry, write the config, then delete the stored key
    only if no binding in any profile still uses it (`secretUser`). Refuse for
    gateway variables and for bindings that inject more than this variable.
*   **AIDE-yqk7.** `aide gateway add --force` uses the same snapshot-and-restore
    step, which fixes that bug.

### 3. Hiding age (`internal/secrets`)

*   **`EnsureIdentity(isTTY, notice io.Writer)`** returns the recipient to
    encrypt to, never prompting:
    *   One or more keys found: use the first one `DiscoverAgeKey` can use,
        with no picker.
    *   None found, terminal: generate one at the sops default path with
        `GenerateDefaultAgeKey` and print one notice: where the file is, that
        losing it makes every saved key unreadable, and that
        `aide secrets` shows the path again.
    *   None found, no terminal: fail with one line: "no encryption key yet:
        run aide once in a terminal, or set SOPS_AGE_KEY_FILE".
*   `ResolveRecipient`, which is interactive, stays for `aide secrets create`
    and `aide gateway add`.

### 4. Launch detection (`internal/launcher`)

*   **Trigger.** Replace the `len(credPlan.Bindings) == 0` condition
    (`launcher.go:320`). After the credential plan is resolved, aide classifies
    each non-empty `IsProviderKeyVar` variable in the inherited environment for
    the harness being launched:

    | Class | Detected by | Terminal | No terminal |
    | --- | --- | --- | --- |
    | injected, same value | a planned binding sets it; values equal | nothing | nothing |
    | injected, different value | values differ (constant-time compare, in memory) | update prompt | one note; stored value used |
    | bound, not for this harness | `BindingsForEnv` non-empty, none planned for this harness | "add pi to binding X?" | one note |
    | unbound | no binding | save prompt | one note with the `env set` command |
    | stored value undecryptable | resolving the plan failed to decrypt this key | recovery prompt (CUJ 7) | fail with one-line hint |

*   **The save prompt.** One prompt per variable in a fixed order:
    `[1] Save (…last4) to profile "<p>" for <harnesses>`, `[2] Use for this
    launch only`, `[3] Strip and continue`. `[1]` is the default when the
    profile came from a match rule or an explicit flag
    (`ResolvedContext.Source` is `match` or `flag`). When the profile is a
    fallback (`Source` is `default`: the user's `default_profile` or the
    built-in `default` profile), there is no default and `[1]` first asks for a
    profile (existing profiles, or create one for this folder). Pressing `h` at
    the prompt opens the harness picker with the pre-selection ticked. The
    summary from `Plan` is shown before `Apply` whenever it is not empty.
*   **Prompt conditions.** Prompts appear only when stdin and stderr are both
    terminals and `--auth login` was not given.
*   **Values.** The shell value is read from the inherited environment before
    scrubbing; the stored value is the one already decrypted for injection.
    Neither is logged or printed beyond the last four characters.
*   **Removed:** the old `[2] Create a new profile for this folder` option. It
    is now part of the profile choice in `[1]`.

### 5. `aide env` for provider variables (`cmd/aide/env.go`)

*   **One branch.** `set`, `list` and `remove` check
    `IsProviderKeyVar(cfg, name)` once and hand provider variables to
    `providerkey`.
*   **`env set <VAR>`**
    *   Scope is always a profile: `--context`, else the profile matched by the
        folder. Writing to the built-in `default` profile needs a terminal
        choice or an explicit `--context default`. `--global` is accepted and
        has no effect. A project-only flag is refused.
    *   Key input: a hidden prompt on a terminal, `--key-stdin` (first line),
        or `--from-env[=NAME]` (the variable itself, or `NAME`). A positional
        value exits 2 before anything is written and warns that the key is now
        in shell history. An empty or unset source exits 2.
    *   `--harness` (repeatable) replaces the pre-selection.
    *   Confirmations (gateway takeover, shared entry, value change): asked on a
        terminal, `--force` off one; otherwise exit 3.
*   **`env list`** adds rows for provider bindings: variable, `secret
    (binding <name> → <harnesses>)`, profile. JSON output gains a `source`
    field (`value` or `secret`).
*   **`env remove <VAR>`** calls `providerkey.Remove`.
*   **Messages.** `providerSecretEnvError` is removed; its case is now handled.
    The non-terminal launch note names `aide env set <VAR> --from-env`.

### Exit codes (`env set` / `env remove`)

| Code | Meaning |
| --- | --- |
| 0 | Saved, removed, or unchanged |
| 1 | Read, decrypt or write failure (after restore) |
| 2 | Usage error or refused input; nothing written |
| 3 | Confirmation needed but no terminal and no `--force` |

### Data flow at launch

```text
shell env ──► detect IsProviderKeyVar ──► classify vs credential plan (this harness)
                                              │
            ┌─────────────────────────────────┼───────────────────────────┐
            ▼                                 ▼                           ▼
       same value                     unbound / other harness /     undecryptable
       (inject stored)                rotated  ──► prompt            ──► recovery prompt
                                              │
                                    providerkey.Plan ──► summary ──► confirm
                                              │
                                    providerkey.Apply ──► secret + config written
                                              │
                                    scrub inherited copy; inject stored value ──► harness
```

## Alternatives Considered

### Alternative A: Check provider/harness compatibility and wire custom URLs

Add a protocol for each provider and harness (Anthropic Messages, OpenAI,
Gemini) and let a `--url` option wire a custom endpoint into every harness that
can speak its protocol.

*   **Pros**: Catches a key or endpoint that a harness cannot use before the
    user finds out at the first request.
*   **Cons**: aide would keep a matrix of providers × protocols × harnesses up
    to date, and passing environment variables cannot make an incompatible pair
    work anyway. The gateway writers assume the Anthropic protocol today
    (`internal/gateway/writers.go:428`, `:554`), so this would also reopen the
    gateway design.
*   **Reason for Rejection**: Rejected by author: the user knows which harness
    speaks to which provider, and aide's job is only to restore what scrubbing
    removed. Custom endpoints stay with `aide gateway`.

### Alternative B: A new `aide provider add|list|remove` command

A dedicated command, modelled on `aide gateway`, that prompts for provider,
key, store, profile and harnesses.

*   **Pros**: Easy to find in `aide --help`; one place to document.
*   **Cons**: A fourth name for credentials next to `credentials:`, `gateway`
    and `env`. It does not help the user who already exported a key and ran
    `aide`, which is what provider docs lead to.
*   **Reason for Rejection**: Rejected by author: the launch prompt meets users
    where the docs send them, and `aide env set` is the command people already
    try for an environment variable.

### Alternative C: Merge credentials and gateways into one "provider" concept

Treat a gateway as a credential plus a URL plus model sync, under one command
and one config section.

*   **Pros**: One idea to learn; removes the split that already shows in
    `--gateway` accepting plain credential bindings.
*   **Cons**: Needs a redesign of the gateway lifecycle, its config writers and
    its migration, which is much larger than the problem being solved.
*   **Reason for Rejection**: Rejected by author: out of proportion to the
    defect. The two stay separate; this design only defines how they order
    against each other.

### Alternative D: Write harness config files when a key is added

Have the save path also write Pi's `models.json`, OpenCode's config and similar
files so the harness is ready to use the provider.

*   **Pros**: Fewer manual steps after saving a key.
*   **Cons**: Ties aide to each harness's config format and version; a known
    provider needs none of it because the harness already knows the URL.
*   **Reason for Rejection**: Rejected by author: harness configuration is the
    user's and the harness's concern.

### Alternative E: Stop scrubbing provider keys

Pass the user's shell environment through unchanged.

*   **Pros**: No prompt and no setup at all.
*   **Cons**: Every key in the shell reaches every harness and every process it
    starts, including MCP servers, plugins and install scripts.
*   **Reason for Rejection**: Rejected by author: scrubbing is the security
    boundary this design preserves.

### Alternative F: Keep strip as the prompt's default

Leave `[Strip and continue]` as the default and only add the
`env set` path.

*   **Pros**: Pressing Enter can never store a secret.
*   **Cons**: The user who followed a provider's docs still ends with a
    harness that has no key, which is the failure this design exists to fix.
*   **Reason for Rejection**: Rejected by author. The risk of saving on Enter is
    handled instead by showing the profile, harnesses and last four characters
    on the default line, printing the undo command, and removing the default
    when the profile is the shared `default` fallback.

### Alternative G: Append the new key behind existing gateways

On a harness served by a gateway, keep the gateway winning and save the key
only for harnesses without one.

*   **Pros**: Adding a key can never stop a gateway from applying.
*   **Cons**: The user who wanted the key to take over sees "saved" while the
    harness still uses the gateway, and fixing it needs knowledge of the
    ordering or of `aide gateway use`.
*   **Reason for Rejection**: Rejected by author in favour of the key moving
    first, with the per-harness summary and a confirmation before any gateway
    stops applying.

### Alternative H: Leave gateway-served harnesses unticked

Show harnesses served by a gateway unselected in the picker; ticking one means
the key takes over.

*   **Pros**: Enter never changes a gateway's reach.
*   **Cons**: Off a terminal there is no picker, so a second rule is still
    needed; the picker gains another state to explain.
*   **Reason for Rejection**: Rejected by author: the confirmation in the chosen
    approach already prevents a silent change, with one rule for both terminal
    and scripted use.

### Alternative I: One shared provider-key store, or reuse by matching value

Store each provider variable once for all profiles, or link to another
profile's stored key when the value matches.

*   **Pros**: Save once and rotate once; a second profile needs no re-entry.
*   **Cons**: Profiles stop being separate. A replace in one profile changes
    others, and detecting a match means decrypting other profiles' stores.
*   **Reason for Rejection**: Rejected by author: profiles exist to isolate
    accounts. Sharing must be an explicit choice with friction, which
    `--store`/`--key` plus a confirmation provides.

### Alternative J: Pre-select only the launched harness, or derive harnesses from a per-harness list in code

Either never pre-select beyond the launched harness, or add a "variables this
harness reads" list to each harness in `internal/harness`.

*   **Pros**: The first needs no data at all. The second gives one source of
    truth and a test that every variable a harness reads is also scrubbed.
*   **Cons**: The first makes multi-harness setups one keypress longer every
    time. The second needs a release of aide whenever a harness adds a provider.
*   **Reason for Rejection**: Rejected by author in favour of the five built-in
    lists plus a user-editable `provider_env:` map, which covers new providers
    without rebuilding aide.

### Alternative K: Remember "never ask again" for a variable

Store a per-profile, per-variable choice to stop offering a stripped key.

*   **Pros**: Fewer repeated prompts for a key the user deliberately keeps out.
*   **Cons**: One more hidden piece of state that changes what launch does and
    has to be explained and reset.
*   **Reason for Rejection**: Rejected by author: a stripped key that stays
    exported is offered again on purpose. Saving it or removing the export
    stops the prompt.

--------------------------------------------------------------------------------

## Non-Functional Requirements

*   **Security & Privacy**
    *   A key enters aide only through a hidden prompt, stdin or a named
        environment variable, never through a flag value or positional
        argument.
    *   Output, logs, errors and prompts show at most a key's last four
        characters. Value comparison is constant-time and in memory; no hash or
        fingerprint is stored.
    *   `provider_env:` is honoured only from user config. A repository's
        `.aide.yaml` cannot add variables or widen harness lists.
    *   Scrubbing is unchanged for every variable aide scrubs today, and
        widened by `provider_env:` keys.
*   **Data safety**
    *   A failed save restores the previous secret value, deletes a key it
        created, and deletes a store file it created. A store that cannot be
        decrypted is never overwritten.
    *   Concurrent saves from two terminals are not locked against each other;
        the last config write wins, as with every other aide config command
        today.
*   **Performance**
    *   Launch decrypts the stored keys it already decrypts today. The new
        classification adds an environment scan and string comparisons only.
*   **Rollout & Rollback**
    *   The launch prompt's default changes from strip to save; release notes
        state this. Existing bindings, `default_credentials` and gateways keep
        their current behaviour until the user saves a key.
    *   Rolling back to an older aide keeps working configs: new bindings use
        only fields older versions already read. An older aide rejects an
        unknown `provider_env:` key in `sandbox.yaml`, so release notes tell
        users to remove it before downgrading.

--------------------------------------------------------------------------------

### Additional Resources

*   [docs/designs/gateway-redesign.md](./gateway-redesign.md): the design
    that supersedes this one.
*   [docs/designs/cli-launch-ux-improvements.md](./cli-launch-ux-improvements.md):
    the earlier launch UX design whose ambient-key section this replaces.
*   [docs/designs/builtin-default-profile.md](./builtin-default-profile.md):
    the fallback profile behaviour CUJ 5 depends on.
*   OpenCode's guide for using its API key with Pi (the triggering case).

### Future Improvements

*   **A per-harness list of variables in code.** If `provider_env:` entries
    turn out to be the same across users, move common ones into built-in
    harness data with a test that every variable a harness reads is scrubbed.
*   **Recovery for non-provider secrets on a new machine.** CUJ 7 recovers
    only keys that are exported in the shell.

--------------------------------------------------------------------------------

## Detailed Implementation Plan

Work is staged so each stage ships on its own and leaves aide working.

*   **Stage 0** (no user-visible change except the AIDE-yqk7 fix): files 1–9.
*   **Stage 1** (the way back: save prompt, `env set/list/remove`,
    `provider_env:`): files 10–16.
*   **Stage 2** (launch detection per harness, rotation, recovery): files
    17–20.
*   **Docs and release notes** ship with Stage 1 and are amended in Stage 2:
    files 21–28.

### Target Files to Create/Modify

#### 1. `internal/secrets/manager.go` (Modify)

*   **Type / Scope**: Go library, encrypted secret stores.
*   **Proposed Changes**:
    *   Add `GetKey(storeName, secretsDir, key) (value string, found bool,
        storeExists bool, err error)`, reusing `decryptForUpdate`.
    *   Add `ReplaceKey(storeName, secretsDir, key, value, recipient)
        (restore func() error, err error)`. It reads the current value and
        whether the key and store existed, writes the new value with
        `SetKey`, and returns a `restore` that writes the old value back,
        deletes a key that did not exist, or deletes a store file that did not
        exist. It refuses when the store exists but cannot be decrypted.

#### 2. `internal/secrets/manager_test.go` (Modify)

*   **Type / Scope**: Unit tests.
*   **Proposed Changes**: Cover `GetKey` for a missing store, missing key and
    present key; cover `ReplaceKey`+`restore` for an overwritten key, a new
    key in an existing store, a new store, and an undecryptable store (refused,
    file unchanged).

#### 3. `internal/secrets/age.go` (Modify)

*   **Type / Scope**: Go library, age identity handling.
*   **Proposed Changes**: Add `EnsureIdentity(isTTY bool, notice io.Writer)
    (recipient string, generated bool, err error)`:
    *   With one or more discovered keys, return the first one
        `DiscoverAgeKey` can use, with no prompt.
    *   With none and a terminal, call `GenerateDefaultAgeKey` and write the
        one-time notice (path, loss consequence, `aide secrets` to show it).
    *   With none and no terminal, return the error "no encryption key yet:
        run aide once in a terminal, or set SOPS_AGE_KEY_FILE".

    `ResolveRecipient` is unchanged.

#### 4. `internal/secrets/age_test.go` (Modify)

*   **Type / Scope**: Unit tests.
*   **Proposed Changes**: Cover the three `EnsureIdentity` branches with a
    temporary HOME and `SOPS_AGE_KEY_FILE`, and check the notice text is
    written once and never contains a private key.

#### 5. `internal/config/credentials.go` (Modify)

*   **Type / Scope**: Go library, credential bindings.
*   **Proposed Changes**:
    *   Export `BindingsForEnv(ctx Context, name string) []string`, returning
        every binding injecting `name` through `env:`, built-in names,
        `for.<harness>.env`, or a gateway-synthesized binding. It replaces the
        unexported `bindingFor`, whose callers take the first element.
    *   Add `(c *Config) IsProviderKeyVar(name string) bool`
        (`envscrub.IsProviderSecretName(name)` or a `provider_env:` key).
    *   Add `(c *Config) PreselectHarnesses(name, launched string) []string`
        with the order given in the Technical Plan.
    *   Add `BuiltinNameForEnv(name string) string`, returning the built-in
        binding name whose only variable is `name`, else empty.
    *   `CredentialScrubNames` also returns `provider_env:` keys.
    *   `validateCredentials` validates `provider_env:`: variable-name syntax,
        known harness names, and no config-dir redirect, endpoint or selector
        names.

#### 6. `internal/config/credentials_test.go` (Modify)

*   **Type / Scope**: Unit tests.
*   **Proposed Changes**: Table tests for `BindingsForEnv` (plain, routed,
    gateway-synthesized, multiple matches), `IsProviderKeyVar`,
    `PreselectHarnesses` (mapping, built-in, fallback, no launched harness),
    scrub-name inclusion, and each `provider_env:` validation error.

#### 7. `internal/providerkey/providerkey.go` (Create)

*   **Type / Scope**: New Go package; the single save path.
*   **Proposed Changes**:
    *   Types:
        *   `Request{Profile, Var string; Harnesses []string; Store, Key
            string}`, where `Store`/`Key` are set only when the user asked
            for them explicitly.
        *   `Plan{Config *config.Config; Binding string; Store, Key string;
            Summary []HarnessChange; NotReached []string; SharedWith
            []string; Takeover bool; Created bool}`.
        *   `HarnessChange{Harness, Before, After string}`.
    *   `func NewPlan(cfg *config.Config, req Request) (*Plan, error)`:
        lookup, refusals, explicit fields, move-to-front ordering, in-memory
        `ValidateForWrite`, before/after summary from
        `DefaultCredentialsForHarness` on the synthesized context, and the
        shared-store guard (`secretUser`-style scan of all profiles).
    *   `func Apply(p *Plan, value string, isTTY bool, notice io.Writer)
        error`: `EnsureIdentity`, `ReplaceKey`, `WriteConfigTo`, and calling
        `restore` on a config-write failure.
    *   `func Remove(cfg *config.Config, profile, name string) (Removal,
        error)`: drop the binding and its `default_credentials` entry, write
        the config, delete the key only when no binding in any profile uses
        it. Refuse gateway variables and multi-variable bindings.
    *   Exported errors for the CLI's exit codes: `ErrNeedsConfirmation`,
        `ErrAmbiguousBinding`, `ErrGatewayVar`, `ErrNameTaken`.
    *   Default store when the request names none: the profile's `secret:`,
        else the profile name. Default key: `lower(VAR)`.

#### 8. `internal/providerkey/providerkey_test.go` (Create)

*   **Type / Scope**: Unit tests with a temporary config dir and age key.
*   **Proposed Changes**:
    *   New binding with explicit fields; config reloads.
    *   Update of an existing binding, where only `store`, `key` and harnesses
        change.
    *   Two matching bindings: refused.
    *   Gateway variable: refused.
    *   Name taken: refused.
    *   Gateway takeover: summary content and `Takeover`.
    *   Harness not reached: `NotReached`.
    *   Shared store: `SharedWith`.
    *   Forced config-write failure: the old secret is restored (AIDE-yqk7
        regression).
    *   `Remove` with and without other users of the key.

#### 9. `cmd/aide/gateway.go` and `cmd/aide/gateway_test.go` (Modify)

*   **Type / Scope**: CLI and tests.
*   **Proposed Changes**: In `gateway add` (around lines 214–229), replace
    `SetKey` and the delete-on-failure rollback with `ReplaceKey` and
    `restore`. Add a test that forces the config write to fail after
    `--force` and checks that the previous token is still in the store. This
    closes AIDE-yqk7.

#### 10. `internal/config/schema.go` (Modify)

*   **Type / Scope**: Config schema.
*   **Proposed Changes**: Add `ProviderEnv map[string][]string
    \`yaml:"provider_env,omitempty"\`` to `Config`. Confirm `ProjectOverride`
    (line 796) has no such field, so `.aide.yaml` rejects it as unknown.

#### 11. `internal/config/split.go` and `internal/config/split_test.go` (Modify)

*   **Type / Scope**: Split config files and tests.
*   **Proposed Changes**: Add `ProviderEnv` to `SandboxFile`,
    `validSandboxFileKeys`, `IsEmpty`, and the merge and split copies (around
    lines 108 and 179). Test a round trip through `sandbox.yaml` and the
    duplicate-section error when it also appears in `config.yaml`.

#### 12. `internal/config/schema_test.go` (Modify)

*   **Type / Scope**: Unit tests.
*   **Proposed Changes**: Test that `provider_env:` in a project
    `.aide.yaml` is rejected.

#### 13. `internal/launcher/ambient_keys.go` (Modify)

*   **Type / Scope**: Launch prompt.
*   **Proposed Changes**:
    *   Replace the four-option prompt with `[1] Save (…last4) to profile
        "<p>" for <harnesses>`, `[2] Use for this launch only`, `[3] Strip and
        continue`.
    *   `[1]` is the default unless `rc.Source == SourceDefault`, in which case
        there is no default and `[1]` opens a profile picker (existing
        profiles, or create one for this folder, reusing the creation path
        that option `[2]` uses today).
    *   `h` opens the harness picker pre-filled from `PreselectHarnesses`.
    *   Saving goes through `providerkey.NewPlan` and `Apply`, showing the
        summary and asking for confirmation when `Takeover` or `SharedWith`
        is set.
    *   After saving, print the undo command and the "export no longer
        needed" line.
    *   `bindAmbientKeysToProfile` and `resolveAmbientBoundCredentials` are
        removed.
    *   Detection uses `cfg.IsProviderKeyVar` in place of
        `envscrub.IsProviderSecretName`.
    *   The non-terminal note names `aide env set <VAR> --from-env`.
    *   Prompts require both stdin and stderr to be terminals.

#### 14. `internal/launcher/ambient_keys_test.go` (Modify)

*   **Type / Scope**: Unit tests.
*   **Proposed Changes**: Replace tests of the old options. Cover:
    *   Enter saves on a matched profile.
    *   On a fallback profile, Enter saves nothing and the picker appears.
    *   `[2]` passes the value through and writes nothing.
    *   `[3]` strips.
    *   The takeover confirmation.
    *   The non-terminal note text.
    *   A `provider_env:` variable is detected.

#### 15. `cmd/aide/env.go` (Modify)

*   **Type / Scope**: CLI.
*   **Proposed Changes**:
    *   **`env set`.** For `IsProviderKeyVar` names, branch to a provider
        path:
        *   Profile from `--context` or the matched profile; a fallback
            profile needs a terminal choice or an explicit
            `--context <name>`. `--global` is a no-op.
        *   New flags `--key-stdin`, `--from-env[=NAME]`, `--harness`
            (repeatable), `--force` (already present for other uses where
            applicable), `--store`, `--key`.
        *   A positional value exits 2 with the shell-history warning.
        *   A hidden prompt on a terminal.
        *   Map `providerkey` errors to exit codes 1–3.
        *   Print the profile, binding name and summary.
    *   **`env list`.** Add provider binding rows and a `source` field in
        JSON.
    *   **`env remove`.** Call `providerkey.Remove` for provider names.
    *   Delete `providerSecretEnvError`.
    *   Update the `set` help text to describe the two kinds of variable.

#### 16. `cmd/aide/env_set_test.go`, `cmd/aide/env_list_test.go`, `cmd/aide/env_remove_test.go` (Modify, Modify, Create)

*   **Type / Scope**: CLI tests.
*   **Proposed Changes**: Cover CUJ 2 and CUJ 8:
    *   `--key-stdin` and `--from-env` (same name and `=NAME`).
    *   Positional value: exit 2, nothing written.
    *   No terminal and no source: exit 2.
    *   Same value: "unchanged", exit 0.
    *   Different value without `--force`: exit 3.
    *   Gateway takeover without `--force`: exit 3.
    *   A fallback profile without `--context` off a terminal: exit 2.
    *   List rows and the JSON `source` field.
    *   Remove keeps a shared key and deletes an unused one.
    *   Remove of a gateway variable: exit 2.

#### 17. `internal/launcher/credentials.go` (Modify)

*   **Type / Scope**: Credential resolution at launch.
*   **Proposed Changes**: `ResolveCredentialEnv` returns a typed
    `*DecryptError{Binding, Store, Env []string, Err}` when a store cannot be
    decrypted, so the launcher can offer recovery for that binding's
    variables. Also return the per-variable resolved values the classifier
    compares against; today's return value already provides them.

#### 18. `internal/launcher/launcher.go` (Modify)

*   **Type / Scope**: Launch flow.
*   **Proposed Changes**: Replace the `len(credPlan.Bindings) == 0` gate
    (around line 320):
    1.  Resolve the credential plan.
    2.  On a `*DecryptError` whose variables are exported, run the recovery
        prompt (CUJ 7) on a terminal, or fail with the one-line hint off one.
    3.  Classify each `IsProviderKeyVar` variable from the inherited
        environment against the resolved values for the active harness
        (`subtle.ConstantTimeCompare`).
    4.  Run the update, add-harness or save prompt per class.
    5.  Re-plan after any save.

    The scrub step is unchanged.

#### 19. `internal/launcher/launcher_test.go` (Modify)

*   **Type / Scope**: Launch tests.
*   **Proposed Changes**:
    *   A profile with an Anthropic binding still prompts for an exported
        `OPENCODE_API_KEY` (the original bug).
    *   Same value: no prompt.
    *   Rotated value: update prompt on a terminal, a note off one.
    *   Binding for another harness: add-harness prompt.
    *   Undecryptable store with the variable exported: recovery prompt.
    *   Undecryptable store with the variable not exported: today's error.
    *   `--auth login`: no prompts.

#### 20. `internal/launcher/ambient_keys.go` (Modify, Stage 2)

*   **Type / Scope**: Launch prompts.
*   **Proposed Changes**: Add the update prompt (`…old4 → …new4`), the
    add-harness prompt and the recovery prompt, all going through
    `providerkey`.

#### 21. `docs/configuration.md` (Modify)

*   **Type / Scope**: User docs.
*   **Proposed Changes**: Add `provider_env:` to the `sandbox.yaml` listing
    (line 10) and a subsection next to `scrub_env:` (around line 191) with an
    example and the user-config-only rule.

#### 22. `docs/secrets.md` (Modify)

*   **Type / Scope**: User docs.
*   **Proposed Changes**: Replace the hand-written Anthropic quick start with
    the two paths: export and launch, or `aide env set --from-env`. Document
    the age key notice, where the key lives, and backing it up.

#### 23. `docs/cli-reference.md` (Modify)

*   **Type / Scope**: User docs.
*   **Proposed Changes**: Document `env set/list/remove` behaviour for
    provider variables, the new flags, and the exit codes.

#### 24. `docs/environment.md` and `docs/profiles.md` (Modify)

*   **Type / Scope**: User docs.
*   **Proposed Changes**: Explain the two kinds of variable the agent sees
    (plain values and secret bindings). Point the credentials walkthrough at
    `aide env set` instead of hand-written YAML, keeping the YAML as the
    reference form.

#### 25. `RELEASE_NOTES.md` (Modify)

*   **Type / Scope**: Release notes.
*   **Proposed Changes**: Cover:
    *   The prompt default changed from strip to save.
    *   The fallback profile has no default.
    *   `env set` accepts provider keys.
    *   `provider_env:`, with the downgrade note.
    *   The AIDE-yqk7 fix.
    *   Launch now prompts even when other keys are bound.

#### 26. `docs/designs/cli-launch-ux-improvements.md` (Modify)

*   **Type / Scope**: Design docs.
*   **Proposed Changes**: Mark its ambient-key section as replaced by this
    design, with a link.

#### 27. `docs/designs/provider-key-restore.md` (Create, at finalization)

*   **Type / Scope**: Design doc.
*   **Proposed Changes**: The finalized copy of this draft.

#### 28. `docs/designs/SITEMAP.md` (Modify, at finalization)

*   **Type / Scope**: Design index.
*   **Proposed Changes**: Register the new design (link, date, author,
    components, description).

### Verification Plan

*   **Compilation/Build**: `nix develop --command go build ./...`
*   **Unit Tests**: `nix develop --command go test -race ./internal/secrets/...
    ./internal/config/... ./internal/providerkey/... ./internal/launcher/...
    ./cmd/aide/...`
*   **Static Analysis & Linting**: `nix develop --command golangci-lint run`
    and `nix develop --command gosec ./...`
*   **Test Coverage Requirement**: 85%+ statement coverage for
    `internal/providerkey`; every exit code and every launch class in the
    tables has a test.
*   **E2E / Manual**: On macOS with a fresh `HOME`:
    1.  `export OPENCODE_API_KEY=test`, run `aide` under Pi, and press Enter:
        Pi sees the variable, and `aide env list` shows the binding.
    2.  Change the export: the update prompt appears.
    3.  Run `aide env remove OPENCODE_API_KEY`: the binding and the key are
        gone.
    4.  Remove the age key and run `aide`: the recovery prompt appears.

<!-- Provenance: Indicates initial structural drafting via EGM. -->

*Drafted with EGM*
