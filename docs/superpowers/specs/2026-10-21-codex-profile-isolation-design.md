# Codex profile isolation and native authentication

Date: 2026-10-21
Status: Approved 2026-10-21; implementation and verification authorized
Design task: AIDE-9y9p
Runtime verification: AIDE-a8nt.11 (Admit Codex for profiles)

## Intent

Any developer should be able to launch Codex under an aide identity such as
personal, work, or client, with that identity's native state, credentials,
and sandbox policy. Switching harnesses must preserve the selected identity.
Personal ChatGPT login and work API credentials are examples, not special
profile types or hard-coded authentication policies.

aide owns identity selection, native directory selection, credential delivery,
and its sandbox boundary. Codex owns provider and model configuration,
authentication flows, credential refresh, and interpretation of its arguments.

## Agreed decisions

- Reuse aide's existing profile and credential-route mechanisms.
- Isolate native Codex state through the selected profile's `CODEX_HOME`.
- Deliver applicable configured credentials and their associated endpoint or
  environment settings through existing credential bindings.
- When no default or saved credential route applies to Codex, still launch
  Codex in the selected profile's home. Preserve the existing notice; Codex
  may use a login already there, or the developer may use its native login flow.
- Do not fall back to another aide profile or the global Codex home to obtain
  a login. aide does not initiate login, implement OAuth, or refresh tokens.
- Native Codex settings profiles work through existing argument passthrough.
  Add no aide schema, persistence, selection logic, or provisioning for them.
- The same ownership boundary applies to other harnesses; this work verifies
  Codex rather than redesigning all adapters.

## Existing implementation and remaining work

The repository already has the relevant primitives:

| Responsibility | Existing implementation |
| --- | --- |
| Per-harness native home and alias validation | `internal/config/profile_dirs.go` |
| Selected harness's directory environment | `internal/provision/profile.go` |
| Credential bindings and route selection | `internal/config/credentials.go` and existing launcher integration |
| Custom Codex provider credential-name discovery | `internal/launcher/codex_env_keys.go` |
| Codex sandbox directory selection | `pkg/seatbelt/modules/codex.go` and shared agent module helpers |
| Profile-aware MCP/plugin configuration destination | `internal/provision/agents/codex/codex.go` |

This is a contract and verification design, not a new profile implementation.
Reuse passing behavior and tests. Change product code only for demonstrated
gaps against this contract, after design approval and implementation planning.

The existing admission task records automated checks and Linux probe evidence,
but real login storage, refresh, concurrency, and authentication precedence
remain pending. A probe replacing Codex cannot establish native authentication
behavior. Keep Codex marked pending until the existing admission criteria pass.

## Launch behavior

Resolve the aide profile and final harness using existing precedence rules.
Resolve the native directory for that final harness, including `--agent codex`
when the profile normally launches another harness. Use the existing directory
resolver in launch and provisioning; do not introduce a second resolver.

For explicit isolated profiles, existing configuration can express:

```yaml
profiles:
  personal:
    agent: codex
    config_dir_name: personal
  work:
    agent: codex
    config_dir_name: work
default_profile: personal
```

The respective homes are `~/.codex-personal` and `~/.codex-work`. Existing
`config_dirs.codex` overrides remain available. Merely naming an outer aide
profile does not replace the existing rules for selecting a native directory.
Preserve legacy/global-directory compatibility and existing shared-directory
guards; this design adds no automatic migration or copying of login state.

Apply the selected profile's policy and existing environment scrubbing before
injecting selected credentials. Unrelated inherited provider credentials must
not silently choose another identity. Preserve the current documented bounds
of custom variable discovery: known Codex homes and their configuration files;
unknown external homes and arbitrary shell variables are not inferred.
Preserve current diagnostics for unreadable or malformed configuration.

| Credential outcome | Launch behavior |
| --- | --- |
| Applicable default or saved route | Inject its configured credentials/settings and launch |
| No route, or no applicable default/saved route | Launch in the same native home, preserving the existing notice where applicable |
| Explicit incompatible `--credential` override | Preserve the existing validation error |
| Selected credential cannot be resolved/decrypted, or configuration is invalid | Preserve the existing error; do not disguise failure as login fallback |
| Explicit native-login mode | Preserve existing `--auth login` semantics and use the selected home |

The last three rows are builder decisions to preserve existing validation and
explicit override semantics. The agreed fallback concerns absence of an
applicable route, not permission to ignore a broken selected credential.

Codex decides what to do when its home has no login. aide must permit the
native login interaction but does not promise a particular slash command,
automatic prompt, or successful authentication without user action.

## Native Codex settings and providers

```sh
aide --profile work --agent codex -- --profile deep-review
```

The first profile selects aide's identity; arguments after `--` go unchanged
to Codex. Codex resolves its own settings profile inside the selected home
according to the installed Codex version. aide neither reads those settings
to choose a provider/model nor creates or remembers the settings profile.
Existing credential-name scanning is security-related environment handling,
not ownership of native settings.

OpenAI, OpenRouter, Amazon Bedrock, and custom services are possible developer
requirements, not a promised Codex compatibility matrix. Credential routes
may contain keys, endpoint settings, or existing keyless capability-based
configuration. Use existing route validation. aide does not translate APIs,
proxy requests, implement AWS signing, or make an unsupported provider work.
Native harness/provider compatibility and authentication precedence must be
verified for any combination claimed as supported.

## Verification and acceptance

Extend and record evidence in the existing
[Codex verification checklist](../../profiles-verification.md#codex), rather
than creating a competing checklist. Record OS, Codex version, storage mode,
test result, and sanitized observations. Never record keys or raw tokens.

Automated coverage must establish the final harness's home selection,
credential scrubbing and injection, default-route login fallback, preservation
of explicit credential errors, and sandbox access to the selected home.
Verify symlink-resolved homes through existing path helpers, including refusal
of aliases that would share identity state. Reuse existing tests; add only
coverage needed for a demonstrated gap. An existing passthrough assertion or
a smoke check is sufficient for native `--profile`; do not build a new feature.

Runtime admission requires real Codex on macOS and Linux, following the
existing checklist: two independent login homes under file/keyring/auto
storage, refresh in one without changing the other, simultaneous sessions,
API credential versus native-login precedence, and native login when a
default route does not apply. Exercise interactive and non-interactive modes
where credential behavior differs. Test selected-profile sandbox access and
denial of other identity homes on both platforms. Keep evidence limitations
explicit when credentials, keyring support, or a platform are unavailable;
do not mark unexecuted cases passing.

Shared skills such as `~/.agents/skills` are resources, not login isolation.
Preserve existing sandbox/resource grants and document that shared resources
do not imply shared authentication. No additional resource isolation is added.

Success means existing behavior meets this contract with recorded evidence,
or the smallest necessary fixes do so. Documentation must distinguish verified
support from pending native-runtime checks. The older multi-harness spec's
stale introductory status must not be used as proof that implemented features
are absent; update relevant admission/status wording when evidence warrants it.

Run code checks only if implementation changes follow, using the repository's
required `nix develop --command ...` toolchain. A documentation-only spec does
not require builds or live account mutations.

## Non-goals and failure conditions

No new profile abstraction, provider chooser, credential backend, login UI,
settings-profile persistence, automatic login migration, or dependencies.
Plugin installation modernization and hook support are separate work.

Failure includes launching the wrong native home after a harness override,
cross-profile credential leakage, silently using a global login instead of
the selected isolated home, blocking launch solely because defaults do not
serve Codex, swallowing credential errors, or claiming runtime isolation from
mock-only evidence.

## Builder discretion and approval boundary

The builder chooses the smallest test placement and fixes using current Go
packages and helpers. Retain existing CLI/schema behavior and notices unless
a proven defect requires a separately described adjustment. No additional
provider integrations are implied by this spec.

The user approved this document and authorized planning and implementation.
The implementation plan focuses on evidence gaps and necessary fixes; this
approval does not admit Codex, authorize live account changes, or complete
AIDE-a8nt.11.
