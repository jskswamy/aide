# Antigravity (`agy`) profile config directories

Date: 2026-11-27
Status: Proposal, for maintainer discussion before any code
Related: 2026-10-16 multi-harness profiles design (admission criteria, "Profiles are not supported for harness" contract), docs/profiles-verification.md

## Problem

A profile that isolates its harness config refuses to run Antigravity:

```text
$ aide --profile work --agent agy
Error: profile "work": Profiles are not supported for harness "agy"
```

`work` sets `config_dir_name`, so aide asks every harness it launches under that profile for its own config dir. The `agy` descriptor in `internal/harness/harness.go` has no `ProfileEnvKey`, so `harnessProfile` (`internal/provision/profile.go`) returns `profileUnsupportedError`. The 2026-10-16 spec recorded Antigravity as "unsupported until isolation is demonstrated" because it had no documented config-home variable.

The only workarounds today are a second, duplicated profile without `config_dir_name` (all agy profiles then share `~/.gemini`), or a hand-written wrapper script per profile. Both lose aide's guarantees or its single source of truth.

## Goal and non-goals

Goal: let a profile run `agy` with its own Antigravity config dir, keeping everything aide already applies per profile (secrets, env, capabilities, sandbox, trust gate), and being explicit about what is not isolated.

Non-goals:

- Faking isolation by overriding `HOME` (rejected by the 2026-10-16 spec; also what third-party wrappers do).
- Isolating the Google sign-in. No agy mechanism does this today (see Research).
- Depending on any upstream change. The design must hold if none of the upstream issues move.

## Research (agy 1.5.18, macOS arm64, 2026-11-27)

### Config directory: works, via an undocumented flag

- `agy --gemini_dir=<dir>` relocates the whole tree: `antigravity-cli/` (history, conversation DB, logs, cache, updater state) and `config/` (settings, MCP config, projects, plugins, skills).
- Flag spelling differs by entry point. The main CLI accepts only the underscore forms `--gemini_dir` and `--app_data_dir`; `--gemini-dir` fails with `flags provided but not defined`. `agy acp --help` documents both spellings of `--gemini-dir` and `--app-data-dir` ("Path where Gemini files are stored", default `.gemini`), plus `--cli_dir_name`. The main CLI's `--help` lists none of them.
- The binary contains `ANTIGRAVITY_GEMINI_DIR`, but setting it had no effect on where agy wrote state.
- Tested through aide 3.8.0 with a per-profile wrapper: an MCP server added under one profile did not appear under another, and the real `~/.gemini/config/mcp_config.json` was unchanged.
- The flag is used in the wild: T3 Code runs one `agy acp --gemini-dir <dir> --app-data-dir <name>` process per account (upstream #1610, #1833, #1875).

### Sign-in: shared, regardless of directory

- The Google OAuth token is one OS keychain item: service `gemini`, account `antigravity` on macOS; one `gemini:antigravity` Credential Manager entry on Windows (maintainer-confirmed in upstream #1261).
- An empty `--gemini-dir` still authenticated from the shared keychain (`ChainedAuth: authenticated via keyring`), including with `ANTIGRAVITY_CREDENTIAL_SCOPE` set (present in the binary, no observed effect outside ACP).
- A maintainer stated on 2026-11-26 (upstream #1610) that empty `--gemini-dir` profiles prompt for sign-in as of 1.5.17. That was not reproduced on 1.5.18 with `agy models`; it may apply to `agy acp` only. Unresolved.

### Other credentials (from binary strings and the agy changelog; not tested)

| Item | Storage | Per directory? |
|---|---|---|
| Plugin sign-ins | keychain service `antigravity.plugins`, `plugin-secrets.json` when too large | Probably shared |
| MCP server OAuth tokens | `mcp_oauth_tokens.json` | Probably per directory |

### Identity alternatives that already isolate

- `GEMINI_API_KEY` from the profile's secrets: agy supports it (changelog). Billed per key, not against a subscription. Not tested through aide.
- `AGY_ADC_AUTH` with a per-profile `GOOGLE_APPLICATION_CREDENTIALS`: Application Default Credentials, mainly for Google Cloud / Gemini Enterprise. Not tested.

### Installation channels

- Official script: a single self-updating binary in `~/.local/bin`, updater state under the config dir.
- Homebrew: `antigravity-cli` is listed by `brew search`.
- Nix or other package managers: possible; the binary is read-only and cannot self-update.

### Upstream issues (google-antigravity/antigravity-cli)

| Issue | Subject | State on 2026-11-27 |
|---|---|---|
| [#155](https://github.com/google-antigravity/antigravity-cli/issues/155) | Config-home env var (equivalent to `GEMINI_CLI_HOME`) | Open, feature request, assigned, no maintainer reply |
| [#381](https://github.com/google-antigravity/antigravity-cli/issues/381) | Supported auth profile selection for wrappers | Open, feature request, assigned, no maintainer reply |
| [#1610](https://github.com/google-antigravity/antigravity-cli/issues/1610) | Sign-in is one keyring entry regardless of `--gemini-dir` | Open, bug, maintainer replied 2026-11-26 |
| [#1833](https://github.com/google-antigravity/antigravity-cli/issues/1833) | macOS keychain item not scoped per `--gemini-dir`; concurrent instances overwrite tokens | Open, bug |
| [#1695](https://github.com/google-antigravity/antigravity-cli/issues/1695) | Multiple Google accounts with account switching | Open, feature request |
| [#1261](https://github.com/google-antigravity/antigravity-cli/issues/1261) | Windows isolation question; maintainer confirms a single credential entry | Open |
| [#1875](https://github.com/google-antigravity/antigravity-cli/issues/1875) | Profiles sharing an `--app-data-dir` lose plugin data | Open, bug |

## Proposal

### 1. Flag-based profile dirs in the harness table

Add a field beside `ProfileEnvKey`:

```go
// ProfileFlag is the launch flag that relocates the harness's whole
// config tree (e.g. "--gemini_dir"). Used when no env var exists.
ProfileFlag string
```

The underscore spelling is the one both the main CLI and `agy acp` accept.

- `SupportsProfiles` becomes `ProfileEnvKey != "" || ProfileFlag != ""`.
- For a flag harness, the profile's dir is delivered as `--gemini_dir=<abs dir>` prepended to the launch args, alongside the existing `ApplyLaunchArgs` / `ApplyChoice` path, including management invocations (`agy mcp add` must land in the profile's dir).
- The dir follows the existing rules: `~/.gemini-<profile>` from `config_dir_name`, or `config_dirs.agy`, inside `$HOME`, never from `.aide.yaml`.
- `WithAgent`, `ValidateProfile` and the env-conflict check become env-or-flag aware: a user-passed `--gemini_dir` or `--gemini-dir` conflicts with a profile-selected one, as `env.CLAUDE_CONFIG_DIR` does today.
- Whether to also pass `--app_data_dir` needs a decision: with a per-profile `--gemini_dir`, the default relative app-data dir already resolves inside the profile's tree, which avoids upstream #1875 without a second flag.

### 2. Explicit acknowledgement of the shared sign-in

The profile must not look isolated while sharing a login (the rule enforced in `internal/config/profile_dirs.go`). A profile that gives agy its own dir must say so explicitly, per harness:

```yaml
# profiles/work.yaml
config_dir_name: work
unisolated_login: [agy]
```

Without the acknowledgement, aide refuses with an error naming the shared keychain item and this field.

The guiding constraint: aide is not weakened to support agy. Where agy cannot meet aide's contract, aide refuses with a clear error. `unisolated_login` is the only part of this proposal that touches an existing rule, and these six rules keep it inside aide's commitments:

1. **Declared in the profile file, keyed by harness.** Not in the global `agents:` section, which would silently apply to every profile (the automatic global fallback the 2026-10-16 spec bans). Not in `.aide.yaml` either (trust gate, DD-32).
2. **Only for harnesses aide cannot isolate.** `unisolated_login: [claude]` is a config error. This stops it becoming the general `--no-profile` escape hatch the spec rejects.
3. **Expires on its own.** When `aide doctor`'s sign-in probe shows the installed agy isolates sign-in per directory, the entry becomes an error ("agy now isolates sign-in; remove it from `unisolated_login`"), so nobody stays on a shared login by accident.
4. **Never looks isolated.** Every launch prints a notice, for example `agy: own config dir ~/.gemini-work; Google sign-in shared with other profiles (unisolated_login in work)`, and `aide status` / `aide doctor` show the same. The notice names the second-order risk: the profile's secrets are in the environment of a session whose Google identity, and probably plugin sign-ins, are shared across profiles.
5. **Everything else still applies.** Sandbox, guards, capabilities, credential scrubbing and the trust gate stay on. Only the sign-in is shared.
6. **Named for what you lose.** `unisolated_login`, not a neutral name such as `shared_config`, so a reader of the YAML sees the cost.

This proposal deliberately has no fallback that shares the whole `~/.gemini` when `--gemini_dir` is unavailable; that case refuses (section 3).

### 3. Capability detection, not version or install assumptions

aide must behave the same however agy was installed.

- Binary discovery is unchanged: PATH or `agents.<name>.binary`. No fixed paths.
- Before using the flag, aide probes the binary and caches the result per resolved binary path and `agy --version`. agy lists every undefined flag before doing any work, so `agy --gemini_dir=<tmp> --aide-probe-undefined` has no side effects: an error naming only `-aide-probe-undefined` means the flag is supported, one that also names `-gemini_dir` means it is not. (`--version` short-circuits flag validation, so it cannot be the probe.) If the flag is missing, aide refuses the profile with a clear error; it never falls back to the shared `~/.gemini`.
- `aide doctor` reports the agy version, whether `--gemini_dir` is supported, and whether an empty dir still authenticates from the shared keychain.
- When that last check reports isolated sign-in, `unisolated_login: [agy]` becomes a config error on that machine (rule 3). No code change or version table is needed.
- The sandbox module (`pkg/seatbelt/modules/antigravity.go`) allows the profile dirs in addition to `~/.gemini`. Whether it should stop allowing the default `~/.gemini` for isolated profiles is an open question below.

### 4. Side fixes found while testing

Both affect any agent defined under `agents:` with a `binary:`, not just agy:

- `aide status` resolves the agent name on PATH and ignores `binary:` (`cmd/aide/status.go`), reporting `(not found)`.
- `--yolo` checks support by agent name instead of the harness behind `binary:`, rejecting `agy-work`.

Both should resolve through `Config.HarnessOf`.

## Current state and future

The design targets the case where nothing upstream changes.

| Upstream outcome | aide behaviour | Change needed |
|---|---|---|
| Nothing changes (expected near term) | Per-profile config dirs, shared sign-in acknowledged per profile | None |
| Sign-in becomes per directory (#1610, #1833) | The probe detects it; `unisolated_login: [agy]` becomes an error, so users drop it | None |
| Config-home env var ships (#155) | Optionally switch the descriptor from `ProfileFlag` to `ProfileEnvKey` | One line |
| `--gemini_dir` removed or renamed | Probe fails; aide refuses the profile with a clear error | Feature unavailable until updated |

Today, identity separation is possible only with `GEMINI_API_KEY` or ADC from the profile's secrets; those remain the user's choice and need no new aide code.

## Consistency with aide's design

| Commitment | How this proposal keeps it |
|---|---|
| Custom profiles isolate harness config (DESIGN.md, DD-7) | Config is isolated through a real agy mechanism, not a fallback |
| No fake HOME isolation, no automatic global fallback, no escape hatch (2026-10-16 spec) | No HOME override; missing flag support refuses; `unisolated_login` is per harness and limited to harnesses aide cannot isolate |
| A profile never looks isolated while sharing a login | `unisolated_login` is required, displayed at launch, in status and in doctor |
| `.aide.yaml` cannot grant before trust (DD-32) | `unisolated_login` and config dirs are rejected there |
| Sandbox and guards on by default (DD-14, DD-28) | Unchanged for agy profiles; only the sign-in is shared (rule 5) |

## Verification before admission

Following docs/profiles-verification.md:

- Two profiles run concurrently with separate `--gemini_dir` (and, if used, `--app_data_dir`): config, MCP servers, skills and history stay separate.
- MCP OAuth tokens: sign into an OAuth MCP server under one profile and confirm the other does not see it.
- Plugin sign-ins: confirm whether they are shared, and state the result in the launch notice.
- Sandbox: from inside an aide-launched agy session, the session can write its own profile dir and cannot read paths aide denies.
- Install channels: official script and Homebrew at minimum; Nix where available.
- Self-update: whether a script-installed agy can update itself inside the sandbox (its binary dir is not writable), and what the user sees.

## Open questions

1. Should an isolated profile's sandbox still allow the default `~/.gemini`, or only its own dir? Denying it is stronger, but agy may read shared paths such as `~/.gemini/GEMINI.md` or `~/.agents/skills`.
2. If the maintainer judges that `unisolated_login` bends the "never look isolated while sharing a login" rule despite rules 1-6, it is dropped, and agy profiles with their own config dir wait for upstream sign-in isolation. aide is not weakened either way.
3. Should profiles with `secret:` set require an additional confirmation for `unisolated_login`, given that the shared Google identity and any plugin running under it see the profile's env?
4. Is relying on an undocumented flag acceptable at all, given that T3 Code and other wrappers already depend on it?
