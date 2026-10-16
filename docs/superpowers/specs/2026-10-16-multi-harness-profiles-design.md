# Multi-harness profiles and repository configuration

Date: 2026-10-16
Status: Revised 2026-10-16 after codebase feasibility study; awaiting final approval; no runtime behavior implemented
Design capture: AIDE-82s
Feasibility study: see "Feasibility study findings" below
Related work: AIDE-dqx (Skillshare provisioning migration), AIDE-bwu (credential-injecting proxy)

## Intent

Aide should let a person use different coding harnesses and providers across personal, work, client, and open-source projects without repeating credentials, logging out between accounts, or accidentally crossing billing and security boundaries.

A profile expresses identity, credentials, environment, matching rules, policy, and a default harness. The harness is independently selectable. A project can prefer Pi while another project under the same profile prefers Claude Code. Different profiles can run the same harness simultaneously with separate native login state.

Provider subscription eligibility is not interchangeable with API access. Aide must not promise that a subscription works with an arbitrary harness; supported authentication methods must be checked against the harness and provider. The harness owns model selection and its native login flow.

## Why this change

The observed failure was a project-specific context selecting Pi without the secret and API-key environment mapping present in another context. An exact project path selected that sparse context instead of the broader work context. Contexts do not inherit credentials from one another.

This exposed an architectural usability problem, but not a complete absence of agent switching: aide already supports `--agent`. Source review also found that profile-directory injection happens before the agent override, allowing a launch to configure the original harness's directory rather than the selected harness's directory.

Separately, capabilities enabled in the main checkout's `.aide.yaml` are not automatically found from linked worktrees. Current discovery stops at the current checkout's `.git` boundary.

## Agreed decisions

- Evolve and rename the user-facing context concept to **profile**; do not maintain two competing identity concepts.
- Keep path/remote profile inference and default-profile fallback.
- Support temporary harness and credential selections, and separate explicit preference-saving operations.
- Remember each repository's default harness and selected credential references outside the repository, scoped by profile and normalized Git remote.
- Share those saved preferences across linked worktrees and independent checkouts with the same profile and remote.
- Let the harness own provider/model choices and persistence. Aide does not watch in-session changes or manage model defaults in version one.
- Give every supported profile/harness combination its own native configuration and login storage. Native login and token refresh remain harness responsibilities.
- Use age as the only credential backend in version one, behind a boundary that can later accommodate other credential providers.
- Inject only explicitly selected API credentials. Never silently use credentials from another profile or inherited shell authentication.
- Supply built-in provider-to-environment mappings and configurable custom/internal mappings, validated by the selected harness adapter.
- Keep credential-injecting proxies separate from this version. Direct environment injection is the initial transport.
- Inherit the main checkout's `.aide.yaml` in linked worktrees, merge a local file on top, let local scalar values win, and union/deduplicate capabilities.
- Support profiles only for harnesses with verified native profile-directory isolation. Stop unsupported launches rather than silently weaken isolation.
- Delegate resource management to Skillshare. Aide manages the profile-specific configuration integration, not a duplicate installer/synchronizer.
- Keep inexpensive compatibility; stop with clear instructions where translation is ambiguous.

Decisions added after the feasibility study (user-approved 2026-10-16):

- Gemini CLI is retired by Google (free/Pro/Ultra access ended 2026-06-18) and replaced by the closed-source Antigravity CLI. Both are **out of scope for version one**. Antigravity has no documented config-directory variable, caches login in the system keyring, and uses gcloud ADC headlessly, so it is "profiles unsupported until isolation is demonstrated", like OpenCode.
- Copilot is deferred until a two-home login test on macOS shows its keychain entry and `gh` fallback do not cross profiles.
- `.aide.yaml` may **reference** an existing profile by name but must **never define** one: profile definitions (credential bindings, secret stores, `profile_dir`, match rules) exist only in user configuration. This is enforced structurally by the loader, independent of the trust prompt.
- Precedence is ratified as: CLI flag > saved user preference > repository `.aide.yaml` > profile default. A saved preference that differs from the repository's harness setting prints a notice. No repository "lock harness" field in version one.
- A worktree-local `.aide.yaml` may subtract inherited capabilities through the existing `disabled_capabilities` field; no new subtraction feature is added.

## Scope and non-goals

Version one covers profile selection, harness selection, isolated native state, age-backed API credential selection, repository preference persistence, worktree configuration inheritance, and migration.

It does not implement provider/model selection, subscription OAuth flows, token refresh, multiple accounts for the same provider within a profile, credential proxies, new secret-manager backends, fake HOME environments, or Skillshare library integration. It does not automatically select a Claude model, including an `auto` argument: native harness behavior remains authoritative.

Home Manager configuration updates are outside this implementation scope. They can be made separately after the design lands.

## Terminology and data responsibilities

### Profile

A named identity and policy boundary, such as `personal`, `work`, or `client-a`. It contains:

- Path/remote match rules and a default harness.
- Credential references and explicit default credential selection, if configured.
- General environment, capabilities, sandbox policy, and other existing context policy.
- Per-harness configuration-directory associations.

A profile is not a provider, model, subscription, sandbox-policy preset, or single API key. Existing named sandbox-policy presets remain a distinct concept and should be labeled as sandbox policies to avoid ambiguity.

### Harness adapter

Describes the binary, native configuration-directory mechanism, authentication-isolation capabilities, accepted credential mappings, sandbox paths, and supported launch behavior. Capability checks must use the same adapter information as launch, status, and integration configuration.

### Provider credential

A profile-local named credential binding, such as `openrouter`, referring to an age secret-store entry. Built-in mappings know conventional environment names such as `OPENROUTER_API_KEY`; custom mappings declare their environment names and compatible harnesses explicitly. Necessary endpoint/environment settings for internal providers may accompany a custom binding. Arbitrary endpoints must not receive unrelated credentials.

One account per provider per profile is sufficient initially. API authentication and native subscription login are distinct modes, not interchangeable credentials. Alternate identities can use separate profiles.

### Repository preferences

User-local preferences keyed by `(profile, normalized Git remote)`, containing a default harness and explicitly selected credential references. They contain no resolved secrets and no model preferences.

The remote normalizer should reuse existing SSH/HTTPS normalization where correct. Do not assume remote identity is globally independent of host or strip organization/repository identity. Changing a remote must not silently transfer old preferences to a different repository.

## Proposed UX and configuration shape

The following is illustrative design syntax, not currently implemented commands or a finalized schema.

```sh
# Infer profile, use repository preferences, then profile defaults.
aide

# One-time selection: retain profile and change this launch only.
aide --agent pi --credential openrouter

# Explicitly select a different identity for this launch.
aide --profile personal --agent pi

# Persist repository defaults without launching or modifying the repository.
aide project set --agent pi --credential openrouter

# Use isolated native login state without injecting provider API keys.
aide --agent pi --auth login
```

Illustrative profile data:

```yaml
profiles:
  work:
    default_harness: claude
    match:
      - path: ~/work/**
    credentials:
      anthropic:
        backend: age
        store: work
        key: anthropic_api_key
      openrouter:
        backend: age
        store: work
        key: openrouter_api_key
    default_credentials: [anthropic]
```

The final field and flag names, user-local preference-file location, and custom mapping representation are builder choices to propose in the implementation plan. Use an atomic, versioned user-local format; never write resolved keys or project preferences into `.aide.yaml` as a side effect of saving defaults.

Preference updates must support explicit clearing, including an empty credential selection for native-login launches. One-time overrides must not mutate preferences. A credential selection for a launch replaces, rather than accidentally unions with, the saved/default API selection. Multiple explicitly selected credentials can be supported without exposing every profile secret.

## Resolution and launch pipeline

1. Discover Git identity, current worktree, and main checkout, without interpreting repository files as trusted policy yet.
2. Select the profile: explicit override, otherwise current path/remote scoring, otherwise configured default. Preserve current matching precedence, including exact-path priority over exact-remote matching.
3. Look up user-local preferences for the selected profile and normalized remote.
4. Discover and trust-gate the main and local `.aide.yaml` files before applying their contents.
5. Resolve the final harness and credential selection using the precedence below.
6. Check that the selected harness supports profiles and the selected authentication/mappings. Stop before decrypting secrets if it does not.
7. Derive that profile's directory for the **final** harness, honoring preserved migration associations.
8. Construct a credential-clean launch environment, then resolve only explicitly selected API bindings from age and add permitted general environment.
9. Build sandbox paths from the same resolved directories and trusted policy. Ensure launch, status/preview, and Skillshare configuration agree.
10. Launch the harness. It owns model selection, native login, token refresh, and in-session behavior.

Precedence (ratified by the user 2026-10-16):

- Profile identity: explicit launch selection > existing matcher > default profile. Repository configuration cannot silently switch identity or choose another profile's secret store.
- Harness: explicit launch override > saved user-local repository preference > trusted merged repository harness setting > profile default.
- API credential selection: explicit launch selection/auth mode > saved user-local repository selection > explicitly configured profile selection. No ambient-shell fallback.
- Other permitted repository settings: profile baseline, then main-checkout file, then worktree-local file, subject to existing trust and non-overridable safety rules.

The saved harness and credential fields resolve independently; changing a harness does not imply changing identity. Validate the resulting combination and fail clearly if incompatible. Missing profile defaults or credentials require setup guidance, not inference from another profile.

For non-Git directories or repositories without the chosen remote, launch may use profile defaults and temporary overrides; repository preference saving must explain the missing identity rather than invent a remote key. Version one uses `origin` for saved identity; existing match rules with `remote_name` continue working for profile inference. Warn about missing/ambiguous identity without exposing credentials embedded in remote URLs.

## API credential safety

The environment is an exposure boundary, not merely a convenience mapping:

- Remove inherited known provider API keys, bearer/OAuth tokens, authentication file selectors, and harness directory redirects that could bypass the selected profile.
- Include configured custom authentication variable names in filtering. Unknown arbitrary variables cannot be classified perfectly; document this limit and centralize the registry.
- General environment and legacy `env:` mappings must not reintroduce unselected provider credentials. Translate known legacy credential entries into explicit bindings during migration or stop with guidance.
- A native-login launch injects no provider API keys. Login state must be read only from the selected profile/harness's native storage, with no silent global fallback.
- Missing selected bindings or secret entries stop launch. A profile without a provider key does not borrow another profile's key.
- Do not log resolved values, include them in previews, or write them into generated Skillshare configuration.
- Unrelated secrets, such as MCP tokens, are not automatically included by selecting a provider. Their explicit resource-scoped environment references must be accounted for separately in the Skillshare integration.

The original goal of detecting every unconfigured provider switch cannot be fully enforced by aide while the harness owns in-session selection. Aide can deny credential exposure and refuse missing selections at launch; the harness reports missing authentication after an in-session switch. Built-in provider integrations with ambient cloud credentials or system keychains require separate isolation verification.

Direct API keys remain visible to the harness and its subprocesses. Native tokens may also be accessible wherever the harness can read them. This version prevents accidental cross-profile authentication; it does not make credentials inaccessible to an untrusted harness. The separate proxy project addresses reducing raw credential exposure.

## Harness support assessment and admission criteria

Source-level inventory, not a claim of end-to-end verified isolation:

| Harness | Existing aide mechanism | Version-one profile disposition |
| --- | --- | --- |
| Claude Code | `CLAUDE_CONFIG_DIR` | Candidate; verify login/API precedence and all auth storage |
| Codex | `CODEX_HOME` | Candidate; verify tokens, refresh, and shared paths |
| Copilot | `COPILOT_HOME` | Deferred: keychain service `copilot-cli` is not shown to be per-home and a `gh auth token` fallback reads global state; needs a two-home macOS test |
| Gemini / Antigravity | aide uses `GEMINI_HOME`; upstream Gemini CLI actually reads `GEMINI_CLI_HOME` (state under `<dir>/.gemini/`); Antigravity has none documented | Deferred: Gemini CLI is retired; Antigravity unsupported until isolation is demonstrated. Existing aide Gemini driver variable is wrong upstream; leave a note, do not invest |
| Pi | `PI_CODING_AGENT_DIR` | Candidate; verify native login and API precedence |
| OpenCode | `OPENCODE_CONFIG` selects a file; no full profile adapter | Profiles unsupported until verified isolation is added |
| Cursor Agent | Sandbox directory handling, no profile adapter | Profiles unsupported |
| Aider | Basic sandbox adapter, no profile mechanism | Profiles unsupported |
| Amp | Sandbox references `AMP_HOME`, no provision profile adapter | Profiles unsupported |
| Goose | Sandbox references `GOOSE_PATH_ROOT`, no provision profile adapter | Profiles unsupported |

Provisioning additionally contains a Hermes adapter and uses `cursor`, while launcher discovery uses `cursor-agent`. Reconcile adapter naming and discovery rather than inferring full support from registration. Custom binaries must explicitly satisfy the profile contract; arbitrary PATH discovery is not proof of isolation.

Before admitting a candidate, verify against its actual version/documentation and exercise configuration paths, login/token storage, refresh, API-vs-login precedence, ambient fallbacks, concurrent profiles, and sandbox behavior on supported operating systems. Shared skills directories are not authentication stores, but still matter for resource/policy leakage and must be documented or restricted.

For a selected unsupported harness, stop with an error such as:

```text
Profiles are not supported for harness "opencode".
```

Do not add fake HOME isolation, an automatic global fallback, or a new `--no-profile` escape hatch in this version. Existing explicit passthrough behavior outside profile-aware launching is not an isolation guarantee and must not become an implicit fallback. Later harness additions are independent admission work, not permission to weaken the contract now.

## Linked worktree configuration

Resolve the main checkout through Git worktree/common-directory information, not by walking filesystem parents or assuming a worktree is nested under the main checkout.

- Main checkout launch: apply its `.aide.yaml` once.
- Linked worktree launch: inherit the main checkout file, then merge any local `.aide.yaml` on top.
- Scalar settings: worktree-local values win within repository configuration.
- Capabilities: stable union with deduplication. No new inherited-capability subtraction feature in version one.
- Environment maps: key-wise merge, local key wins, still subject to credential-selection restrictions.
- Remaining list/policy fields: preserve existing merge semantics where safe; explicitly describe security-sensitive grants in the preview rather than silently broadening them.
- Trust approval applies separately to each file and its contents. Changed inherited settings invalidate the corresponding approval. An untrusted file must not contribute grants before approval.
- Relative paths are interpreted against the checkout containing the declaring file, not the shell's current directory. This preserves inherited path meaning; users wanting current-worktree-relative paths can declare them locally. This is a builder recommendation requiring regression coverage.
- Missing main `.aide.yaml` is normal and contributes no settings. An inaccessible/deleted main checkout with potentially inherited policy is an explicit diagnostic, not silent partial inheritance. Bare/worktree-only arrangements need a documented outcome before implementation.

Independent clones share remote-keyed saved preferences but do not inherit each other's `.aide.yaml`. Do not fetch a main checkout file from a remote or another arbitrary clone.

Worktree-local configuration can therefore change permitted harness/policy settings, but cannot select a different profile's identity or inject arbitrary unselected provider credentials. Profile matching still uses the launch location and existing Git rules.

## Skillshare integration boundary

Aide owns profile identity, native harness directories, credential retrieval, launch environment, sandbox policy, and repository preferences. Skillshare owns skills, tools/MCP connections, and delegated resources such as agents, hooks, plugins, and shared instructions once migrated.

The integration is configuration-only: declare named account targets with `agent` and `config_dir`, per-target resource filters, MCP assignments, and environment references. Aide's launch directory must equal Skillshare's target directory for that profile/harness. Preserve unrelated user-managed configuration.

Skillshare documents account targets for Claude, Codex, Pi, and OMP. This list is not identical to aide's candidate profile support: do not assume Copilot or Gemini have the same Skillshare account interface. Validate each resource destination and report unsupported integration separately from authentication support.

No Skillshare library embedding, background management API, installer duplication, or automatic synchronization is required by this design. Users continue using Skillshare to install, update, and sync resources. Revisit tighter integration only if concrete limitations emerge.

AIDE-dqx already records a deprecate/export/remove migration. Extend that work instead of duplicating it. Do not remove aide provisioning before migration preserves existing resources. Resolve single-writer ownership for instructions/hooks/plugins, and preserve sandbox access to symlink-resolved Skillshare sources.

## Context-to-profile migration and compatibility

Today, a context owns identity/policy while nested `profile:` and `profile_dir:` only select a harness configuration directory. The new outer profile replaces the identity context; the nested legacy field is not a second profile concept.

Low-complexity compatibility:

- Normalize legacy `contexts:` and `default_context:` into the new representation at the loading boundary.
- Keep `aide context` as a deprecated command alias and temporarily expose both `AIDE_PROFILE` and legacy `AIDE_CONTEXT` with the same identity value.
- Reject conflicting mixed old/new declarations with actionable migration guidance.
- Preserve context names as profile names unless explicitly renamed.
- Preserve existing native directory associations, including legacy context names differing from nested `profile:` values and explicit `profile_dir:` paths. Never silently replace a populated login directory or require re-login as a side effect of renaming terminology.
- Previously unisolated contexts using global native state require an explicit adoption decision. Never silently copy one global login into multiple profiles or pretend multiple identities are isolated when paths alias.
- Detect shared/aliased configuration paths between profiles, including symlinks, and stop until the user separates or deliberately migrates them.
- Explicit saves/migration write the new format; launch does not rewrite configuration automatically. Preview migration and preserve unrelated fields.

Provisioning state currently uses a `contexts` ownership map. Read/translate it without losing ownership, drift hashes, or treating managed resources as new. Coordinate state migration with Skillshare handoff. Status/JSON output, shell completions, environment markers, docs, examples, approval storage, sandbox grants, and extension scripts all require impact review.

Internal Go package/type renames can be incremental. Supporting both complete semantic models indefinitely is not a goal. Clear migration instructions are preferable to elaborate ambiguous compatibility.

## Evidence and verification required before implementation claims

Current source evidence:

- `internal/launcher/launcher.go`: profile injection precedes `--agent`; normal environment starts from `os.Environ()`.
- `internal/provision/profile.go`: profile directory derivation and optional override.
- `internal/context/git.go`: Git remote detection/normalization primitives.
- `internal/config/config.go`: `.aide.yaml` discovery stops at the current `.git` boundary.
- `internal/launcher/passthrough.go`: ten launcher-recognized harnesses.
- `internal/provision/agents/`: additional registry discrepancies and advertised profile keys.
- `internal/provision/state.go`: context-keyed provisioning ownership.

Acceptance coverage must demonstrate:

1. Profile inference/fallback remains stable; an explicit profile selection wins.
2. Harness override is resolved before directory/env/sandbox construction.
3. Personal and work can run the same admitted harness concurrently without authentication crossover.
4. Only selected profile API keys reach a launch; inherited keys/tokens and unselected keys do not.
5. Native login persists per profile/harness and no login flow is reimplemented in aide.
6. One-time changes do not save preferences; explicit saves survive restarts without modifying the repository or storing values.
7. Normalized SSH/HTTPS repository identity shares preferences across worktrees under the same profile, not across profiles.
8. Main-checkout capabilities appear in worktrees; local configuration merges correctly and both trust decisions are enforced.
9. Unsupported harness profile launches stop before secret decryption or provisioning side effects.
10. Old configurations migrate without lost logins or resource ownership; ambiguous cases fail with instructions.
11. Skillshare directory/secret references agree with launch and no plaintext credential is emitted into resource configuration.
12. macOS and Linux sandbox grants cover resolved target paths without granting other profiles' authentication directories by default.

No tests run in this documentation phase establish these runtime guarantees. They are required outcomes for subsequent implementation.

## Feasibility study findings (2026-10-16)

Static analysis of the codebase plus documentation/source review of each harness. Nothing was executed; runtime verification remains an admission requirement. Overall verdict: feasible, larger than first read in three places (worktree inheritance, credential-clean environment, rename).

### Corrections to the earlier draft

| Earlier claim | Finding |
| --- | --- |
| Profile injection precedes `--agent` | Confirmed: `internal/launcher/launcher.go:253` injects from the pre-override agent; the override is applied at `:261-275`. `aide status` and `PreviewSessionEnv` ignore `--agent` entirely and would keep the same blind spot. No existing test combines `profile:` with `--agent`. |
| Existing `remote_name` rules keep working | False. `MatchRule.RemoteName` (`internal/config/schema.go:272`) is parsed but never read; about 14 call sites hardcode `"origin"`. Honoring it needs the resolver to take a remote-lookup function instead of one URL. |
| Matching precedence is stable | Scores are exact path 300+len, exact remote 250+len, glob path 200+len, glob remote 100+len, default 0, but contexts are iterated from a Go map, so equal scores are non-deterministic (open: AIDE-2zj, AIDE-h54). Fix with sorted iteration. |
| Age-only backend behind a boundary | Secrets are sops-encrypted YAML decrypted with an age identity (`internal/secrets`), one file per context. There is no backend interface. A binding `(store, key)` maps onto `LoadSecretsMap`; `backend:` is validated to the single value `age`. |
| Worktree inheritance is a merge feature | It is architectural: `Config.ProjectOverride`, `Config.ProjectConfigPath` and trust info are single-valued; `FindProjectConfigForWrite` duplicates the discovery walker; `cmd/aide/prompt.go` duplicates the trust check; relative-path rewriting at load time does not exist. |
| Cursor has no profile adapter | A provision driver named `cursor` exists while the launcher and sandbox use `cursor-agent`; provision lookups for `cursor-agent` miss. Hermes has a driver but is not in `KnownAgents`. |
| "Profile" is one overloaded term | Two existing meanings: `Context.Profile`/`ProfileDir` (native config-dir selector) and `SandboxRef.ProfileName` (named sandbox policy). |
| Unsupported harnesses get a clear error | Harnesses with no driver (aider, amp, goose) currently report "no provisioner registered for agent"; the profile-unsupported message needs explicit handling. |

### Existing constraints the design must respect

- `ErrProfileNotProjectScoped` (`internal/provision/profile.go`) already forbids `profile:`/`profile_dir:` in `.aide.yaml`. Version one extends this: project scope may name an existing profile but never define one.
- The YAML loader ignores unknown keys, so a mixed `contexts:` plus `profiles:` file would pass silently; conflict detection must be explicit.
- `config_hash` in provisioning state hashes raw config bytes, and `StateVersion` is written but never checked on load. Renaming keys would drift every profile unless migration rehashes. No approval or sandbox grant is keyed by context name; trust keys are content-and-path hashes, so migration must never rewrite project files silently.
- Default launch and the no-config passthrough path (`internal/launcher/passthrough.go`) start from the full `os.Environ()`; provision subprocesses (`internal/provision/runner.go:30`, `cmd/aide/provision_list.go:90`) do too. `never_allow_env` runs after injection and matches exact names, so it cannot serve as the credential scrub. `SOPS_AGE_KEY` and `SOPS_AGE_KEY_FILE` must be scrubbed.
- Sandbox: a profile directory injected for the final agent is granted automatically and other profiles' directories are not; Claude's fixed `~/.claude.json` and `~/.cache/claude` grants are shared across profiles and need isolation verification. Resolved for Claude: see the Claude row in "Harness admission status".
- Git remote normalization strips URL userinfo and keeps host/org/repo, but drops ports, does not lowercase hosts, and does not trim trailing slashes; harden it before keying preferences on it. Persist only the normalized form.

### Effort by workstream

| Workstream | Effort |
| --- | --- |
| Fix `--agent` ordering (+ regression test, status/preview) | S |
| Deterministic matching; honor `remote_name`; central remote helper | S/M |
| `--profile`, `--credential`, `--auth` flags (fields on `Launcher`) | S/M |
| Credential-clean environment and scrub registry | M |
| Credential bindings (age) | M |
| Preferences store and remote normalizer | S/M |
| Unified harness descriptor (`pkg/seatbelt` cannot import `internal/`) | M (L if sandbox modules are rebuilt) |
| Context-to-profile rename | M-L (about 200 Go files, 56 docs; Go type renames optional) |
| Provisioning-state migration with rehash | M |
| Aliased-directory detection (`fsutil.ResolveOrSelf` plus nearest-ancestor resolution) | S |
| Worktree inheritance with per-file trust | L |
| Skillshare configuration integration (after state migration; AIDE-dqx is parked with no child tasks) | M |

### Harness admission status

Verdicts are from documentation and source reading; each admission still requires the runtime checks in the admission criteria.

| Harness | Status | Notes |
| --- | --- | --- |
| Claude Code | Pending verification: automated checks done, manual macOS/Linux steps in [`docs/profiles-verification.md`](../../profiles-verification.md#claude-code); not yet admitted | `CLAUDE_CONFIG_DIR` keys the macOS Keychain entry and `.credentials.json` per directory. A keyless Console sign-in and Anthropic profiles live in `~/.config/anthropic` (outside the directory). Env credentials outrank native login. Scrub: `ANTHROPIC_API_KEY`, `ANTHROPIC_AUTH_TOKEN`, `CLAUDE_CODE_OAUTH_TOKEN`, `ANTHROPIC_PROFILE`, `ANTHROPIC_FEDERATION_RULE_ID`, `ANTHROPIC_ORGANIZATION_ID`, `ANTHROPIC_IDENTITY_TOKEN_FILE`, `ANTHROPIC_SERVICE_ACCOUNT_ID`, `ANTHROPIC_WORKSPACE_ID`, `CLAUDE_CODE_USE_BEDROCK`/`_VERTEX`/`_FOUNDRY`, ambient `AWS_*` and Google ADC. Check `apiKeyHelper` in user settings. Vendor terms permit signing in to the unmodified binary with your own plan. Implemented (2026-10-16): the scrub registry also covers `CLAUDE_CODE_OAUTH_REFRESH_TOKEN`, `ANTHROPIC_IDENTITY_TOKEN`, Foundry and Claude Platform on AWS keys, `ANTHROPIC_CUSTOM_HEADERS`, mTLS client key/cert, `ANTHROPIC_CONFIG_DIR`, every `CLAUDE_CODE_USE_*`/`CLAUDE_CODE_SKIP_*_AUTH` switch and the Azure default credential chain. With `CLAUDE_CONFIG_DIR` set, Claude keeps `.claude.json` in the dir, so the macOS sandbox no longer grants the global `~/.claude.json`/`~/.claude.lock` and grants the dir's own `<dir>.lock`; `~/.cache/claude` stays shared (install/update cache, no login state, already inside the `~/.cache` grant). A profile launch prints a note when the profile dir's `settings.json` sets `apiKeyHelper`. `~/.config/anthropic` is not granted; `ANTHROPIC_CONFIG_DIR` inside the profile dir is the documented per-profile option (to be verified). |
| Codex | Admit with caveats | `CODEX_HOME` holds `auth.json`; the keyring entry is keyed by a hash of the canonical home, so there is no cross-profile leak. `CODEX_API_KEY` overrides all auth where enabled. `~/.agents/skills` is shared. Scrub `CODEX_API_KEY`, `CODEX_ACCESS_TOKEN`, `OPENAI_API_KEY`, `OPENAI_BASE_URL`, and any `env_key` a custom provider names. |
| Pi | Pending verification: automated checks done, manual macOS/Linux steps in [`docs/profiles-verification.md`](../../profiles-verification.md#pi); not yet admitted | `PI_CODING_AGENT_DIR` holds `auth.json`, refresh is lock-protected. A stored credential outranks env keys, so `--auth api` cannot guarantee an env key wins for a provider with a stored login. Ambient gcloud ADC fallback for Vertex. Longest scrub list (see Pi's `docs/providers.md`). Whether an Anthropic subscription may be used through Pi is unresolved with the vendor terms; aide does not advertise it and the checklist does not use it. Implemented (2026-10-16): the scrub registry covers every variable in Pi's provider docs and `pi --help` (secret-bearing keys and tokens; Azure OpenAI, Cloudflare and `GOOGLE_CLOUD_LOCATION` as selectors; Bedrock/Vertex ambient credentials stay re-admittable cloud names); `PI_CODING_AGENT_SESSION_DIR` is a Pi dir redirect, dropped for profile launches. Stored-login shadowing (decision): the launch continues and, when it injects a binding for a provider whose entry exists in the profile's `auth.json`, prints a note naming the provider only (`Pi uses it ahead of the selected API key`); providers without a documented `auth.json` id are not checked. With a profile dir, the Pi sandbox module grants only that dir (not `~/.pi` or other profiles' dirs) on both backends. Vertex ADC (`~/.config/gcloud`) is per user, granted only with the `gcp` capability, and shared by every profile that has it: documented limit. Agent-run evidence on Linux (pi 1.2.0, Landlock ABI 4, fake `api_key` entries, no OAuth login): a stored `anthropic` entry beat an injected `ANTHROPIC_API_KEY` (`pi auth print-api-key`), the note printed, `--auth login` injected nothing, and a probe in the verify-a sandbox read its own `auth.json` but was denied `~/.pi-verify-b/auth.json` and `~/.pi/agent/auth.json`. OAuth login, refresh, lock and macOS remain for the checklist. |
| Copilot | Deferred | See table above. |
| Gemini / Antigravity | Out of scope | See decisions above. |
| OpenCode, Cursor Agent, Aider, Amp, Goose | Profiles unsupported | Unchanged. |

Skillshare account targets support only `claude`, `codex`, `pi` and `omp`. They require absolute or `~` `config_dir` values that are not the agent default and are unique per target. Configuration-only integration (aide writes `targets:`, user runs `skillshare sync`) is workable; MCP environment-reference schema still needs confirmation.

### Staged delivery order

Each stage ships independently and carries its own `RELEASE_NOTES.md` entry when user-visible.

1. Fix `--agent` ordering with a regression test, fix status/preview, make matching deterministic.
2. Add `AIDE_PROFILE` next to `AIDE_CONTEXT`; centralize the remote helper; honor `remote_name`; harden remote normalization.
3. Loader normalization of `contexts:`/`default_context:`, explicit conflict detection, aliased-directory detection.
4. Scrub registry and credential-clean environment (launch, passthrough, provision subprocesses, sandbox), then credential bindings.
5. Preferences store, `aide project set`, and `--profile`/`--credential`/`--auth`.
6. Provisioning-state migration with rehash, then `aide profile` with `aide context` as a deprecated alias.
7. Harness admission in order Claude, Pi, Codex, each with its runtime verification.
8. Worktree inheritance with per-file trust, then Skillshare integration under AIDE-dqx.

Mark `2026-05-01-context-ux-redesign.md` as superseded by this design when stage 6 lands.

## Builder choices and remaining technical investigation

The implementation plan should settle final schema/flags, preference storage location, adapter registry ownership, migration versioning, exact protected environment registry, and non-capability merge details. Recommendations above are author proposals, not additional user approvals.

Verify candidate harness isolation from native documentation and controlled tests before marking support. Resolve absent main-checkout and bare-repository behavior, trust-cache identity for inherited files, shared native resource paths, and legacy global-login adoption without weakening the stated safety contract. Any investigation requiring a change to that contract returns to the user before implementation.

This document is the consolidated design, not an implementation plan. Obtain approval before planning or changing runtime code.

## References

- Skillshare account targets: https://skillshare.runkids.cc/docs/reference/targets/configuration#agent-config-dir
- Skillshare MCP accounts: https://skillshare.runkids.cc/docs/reference/commands/mcp#accounts
- Skillshare credentials/environment references: https://skillshare.runkids.cc/docs/how-to/daily-tasks/sharing-mcp
- Credential-proxy motivation (separate scope): https://tailscale.com/blog/hugging-face-intrusion
