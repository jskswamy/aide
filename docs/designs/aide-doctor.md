# aide doctor

*   **Author:** Krishnaswamy Subramanian
*   **Status:** Approved
*   **Date:** 2026-11-24

--------------------------------------------------------------------------------

## Overview

`aide doctor` is a new command that reports what is wrong with aide's setup for the current directory and how to fix it. It shows the current directory first: which profile matched and why, the agent, the secret store name, capabilities, guards, the trust state of `.aide.yaml` and project markers. Then it lists *findings* for this directory and for the rest of the config. Each finding has a severity (error, warning or tip) and either a fix, which is an ordinary `aide …` command, or written steps. A fix that would give the agent more access is marked as widening access. With `--fix`, doctor asks before each fix and runs the ones the user accepts.

This design also covers the groundwork doctor needs. validate's checks are written inline and return strings, and the view of the current directory is computed separately by `which`, `status` and launch.

## Terms

*   **Profile:** a named set of agent, secrets, env, capabilities and sandbox settings, chosen per directory by match rules.
*   **Config layers:** where a setting is written. The *global* layer is the user's config under `~/.config/aide/` (`config.yaml`, `profiles/<name>.yaml`, named sandboxes). The *project* layers are `.aide.yaml` files: `local` is the file in the checkout aide runs in, `inherited` is the main checkout's file when aide runs in a linked git worktree (`internal/config/project_layers.go`).
*   **Trust:** a project `.aide.yaml` applies only after the user approves its exact contents with `aide trust`. Approval is stored per user and tied to the file's bytes, so any edit makes the file untrusted until it is approved again. aide's own write commands re-approve a file after writing it if it was trusted before (`internal/config/project_writer.go:25-49`).
*   **Capability:** a named bundle of sandbox paths and env vars for a toolchain (`go`, `python`, `gpu`). **Project markers** are files that suggest one (`go.mod`, `uv.lock`). **Variants** are toolchain-specific forms of a capability chosen from project evidence, with the user's choice saved in the **consent store**.
*   **Guards:** built-in sandbox rule sets that protect sensitive paths; some are on by default, others are opt-in.
*   **Agent permission store:** the agent's own list of allowed directories (for example Claude Code's `additionalDirectories`), which `aide sandbox allow` also updates.
*   **Legacy layout:** the old single-file config that `aide config migrate` converts to the modular layout.

## The Problem

aide's diagnostics are spread over several commands, and none gives one answer:

*   **`aide validate`** (`cmd/aide/trust.go:19`) checks the whole config file but knows nothing about the current directory. Its checks are written inline in the command and produce plain strings, so no other command can use them.
*   **`aide which` and `aide status`** (`cmd/aide/status.go`) show the current directory's profile and print warnings. `status` builds the sandbox policy with the current directory as the project root (`status.go:811,882`) while `which` uses the real project root (`status.go:291`), so in a subdirectory they disagree. `status` also resolves capabilities without variants and builds its policy without the capabilities' sandbox overrides (`status.go:796-811`), so its guard count and network mode can differ from launch. `which` copies launch's capability and policy steps by hand (`status.go:292-396` against `internal/launcher/launcher.go:448-585`).
*   **`aide sandbox show` and `aide sandbox guards`** show the policy without saying whether anything is wrong with it.
*   **`aide cap suggest-for-path` and the launch banner** detect project markers.
*   **The Claude Code plugin's `/aide doctor`** (`plugins/claude-code/commands/aide-doctor.md`) and its `config-review` skill ask the model to run `which`, `validate`, `sandbox show` and `sandbox guards`, read their text, and decide severities and fix classes itself. That only works inside Claude Code, can give different answers on different runs, and already disagrees with Go: the plugin calls a missing agent binary an error, while validate calls it a warning.

Nothing points out config that works but could be simpler, such as an allow rule already covered by a broader one, and nothing offers to fix problems. A fixer that edits YAML directly would be unsafe: it would either re-approve a shared `.aide.yaml` as a side effect or silently remove its approval.

## Goals

*   **One list of checks.** `aide validate`, `aide which`, `aide status` and `aide doctor` render findings from the same check functions. No check is implemented twice, and no command reads another command's text output.
*   **validate's output stays the same.** Its output, sort order, summary line and exit codes are locked by golden tests written before any code moves. Every other mention of validate in this document refers back to this goal.
*   **Doctor sees what launch sees.** Capability resolution and the sandbox policy come from one function that launch also calls (see Technical Plan, "Directory view"). Where launch would ask a question (project trust, variant consent, gateway choice), doctor does not ask: it shows what launch does when nothing is answered and reports the pending question as a finding. A test compares the policy doctor builds with the one launch builds.
*   **Typed findings.** Each finding has a stable ID, a subject, a severity, whether its fix widens access, the config layer and file it comes from, the profile it concerns, a message, and either a fix command or written steps.
*   **Doctor works on a broken config.** A config that fails to load is reported as the first error, with the file, the reason and the line when the parser reports one. Checks that don't need the config (project markers, trust state) still run, and `--fix` is disabled.
*   **Machine-readable output.** `--format json` with a `schema_version`. The plugin's `/aide doctor` and its skills read it instead of classifying findings themselves.
*   **Fixes are commands, and each one is asked.** Each fix is an `aide …` command a user could type, so it gets that command's validation and trust handling. `--fix` asks before every fix on a terminal; without a terminal it runs nothing and prints the commands. Findings with no command come with written steps.
*   **Config-shape suggestions that cannot widen access by mistake.** A rule is reported as redundant only when a parent rule in the same list already covers it.

## Non-Goals

*   **No preflight on launch.** The April spec's "Preflight diagnostics" (`docs/superpowers/specs/2026-04-14-generic-toolchain-detection-design.md`) is postponed.
*   **Doctor never runs `aide trust`.** It never approves an untrusted or edited file. A fix that writes an already-trusted `.aide.yaml` re-approves it, because the underlying command does; the fix lists this as a side effect.
*   **No unattended fixes.** No `--yes` or `--force` flag; every fix needs a yes on a terminal.
*   **No writes to the consent store or the agent's permission store** except through an `aide …` command whose side effects the fix lists.
*   **No merge with `internal/diag`.** `aide --diagnose` stays the report for a run that already failed. The two commands' help text points to each other.
*   **No decrypting secrets.** Doctor never resolves secret values (unlike `aide which --resolve`).
*   **No muting findings in config** in any stage of this design.

## Open Questions

*   **Redundant rules across layers:** a rule in a project `.aide.yaml` that a global rule already covers is not reported (see "Config-shape suggestions"). Is a tip useful there, given the project file may be shared with people whose global config differs?

## Critical User Journeys (CUJs)

### CUJ 1: Check a project before starting work

*   **Given**: a project directory with a trusted `.aide.yaml` and a global config with three profiles.
*   **When**: the user runs `aide doctor` in a terminal outside any aide session.
*   **Then**: a count line ("1 error, 2 warnings, 3 tips") comes first. Next, the directory section shows the matched profile and why, agent, secret store name, active capabilities and guards, trust state and project markers, matching what `aide which` shows and what launch would use. Findings for this directory follow, then findings for the other profiles. The exit code is 1 if any finding is an error, else 0.

### CUJ 2: Apply fixes

*   **Given**: the project has a trusted `.aide.yaml`. Doctor reports a redundant allow rule in the current profile's own sandbox block, a capability whose markers match the project but which is not enabled (fix `aide cap enable go`, widens access), and another profile with no match rules (steps only).
*   **When**: the user runs `aide doctor --fix` in a terminal.
*   **Then**: doctor asks before each fix, showing the command, the file it changes and its side effects. For `aide cap enable go` it also says that the fix widens access, names the marker file (`go.mod`), lists the paths the capability opens, and says that the trusted `.aide.yaml` is re-approved after the write. For the profile without match rules it prints the steps (`aide profile bind`). After each fix it re-runs the checks, and at the end it reports what is left.

### CUJ 3: Fix without a terminal

*   **Given**: the same findings, with doctor run from a script.
*   **When**: `aide doctor --fix --format json` runs without a TTY.
*   **Then**: no fix runs. Each fix is listed with its command, and findings without a command show their steps. `fix_results` marks every fix `not-run` with the reason "no terminal".

### CUJ 4: The config won't load

*   **Given**: `config.yaml` has a YAML syntax error.
*   **When**: the user runs `aide doctor`.
*   **Then**: the first finding is an error naming the file, the reason and the line when the parser reports one. Project markers and trust state are still reported. `--fix` refuses to run and says why.

### CUJ 5: The plugin asks doctor

*   **Given**: Claude Code running inside an aide sandbox, with the aide plugin.
*   **When**: the user runs `/aide doctor` or the `config-review` skill triggers.
*   **Then**: it runs `aide doctor --format json` and explains the findings, instead of running four commands and classifying their text. Doctor sees `AIDE_SANDBOX=on`, marks the report as taken from inside the sandbox and refuses `--fix`; the plugin gives each fix's command for the user to run in a separate terminal. On Linux the sandbox cannot read `~/.config/aide`, and the report says so instead of reporting a broken config. With an aide that has no `doctor`, the plugin shows the raw command output without severities and suggests upgrading.

## Technical Plan (Architecture)

### What exists today

*   **Selection is shared.** `previewSelection` (`cmd/aide/selection_flags.go:168`) calls `launcher.ResolveSelection`, which `Launch` also calls. `which`, `status` and `sandbox show` go through it. Previews pass "not interactive", so selection never prompts for them; launch passes the real terminal. Selection returns `rc` (the resolved profile), the project trust gate (`res.Gate`: untrusted or denied files in `Gate.Files`, and `Gate.Warnings`) and notices. `previewSelection` merges `Gate.Warnings` into the notices (`selection_flags.go:174`).
*   **The capability and sandbox step is copied.** Launch merges capability names, resolves the sandbox reference, resolves capabilities with variants, applies their sandbox overrides and applies `-N` (`internal/launcher/launcher.go:448-509`); it builds the env and runs the keyless-gateway checks (511-570), then builds the policy (586) and adds the agent module, agent env and env to it (593-608). `which` repeats the capability steps by hand with `Interactive: false` and no consent store (`cmd/aide/status.go:292-396`), so it shows default variants even where the user has saved a choice. `status` resolves capabilities without variants, builds its policy without the capabilities' overrides, and uses the current directory as project root (`status.go:796-811, 882`).
*   **validate's checks are inline** in `validateCmd` (`cmd/aide/trust.go:32-96`) with printing and sorting at `trust.go:98-127`. It does not check capability names.
*   **Warnings are untyped strings.** Sources doctor can reproduce without decrypting: `PolicyFromConfig` path warnings, `capability.CredentialWarnings`, `capability.CompositionWarnings`, selection notices (including gate warnings). Launch prints others that doctor defers: `nativeAuthNotice` and `codexConfigNoticeFor` (launcher.go:303, 309), `sel.ChoiceNotices`, the credential skipped and ignored notes (340-346) and `warnLegacyCredentialEnv` (430). The withheld-gateway notes (357-358) need decryption and stay launch-only.
*   **Rule removal exists only for named sandboxes:** `aide sandbox edit <name>` has `--remove-readable`, `--remove-writable` and `--remove-denied`. `aide sandbox allow` adds a path to a profile's own sandbox block (`readable_extra`, or `writable_extra` with `--write`) and also grants it in the agent's permission store (`cmd/aide/sandbox.go:769-823`). It writes the local `.aide.yaml` by default, or the global profile with `--global --profile <name>`; `--profile` without `--global` is rejected (`cmd/aide/scoped_mutation.go:25-30`). `aide cap enable` writes the local `.aide.yaml` by default and creates it if missing (`cmd/aide/cap.go:720-743`).

### Components

```text
                  ┌──────────────────────────────────────┐
  cmd/aide        │ validate   which   status   doctor   │──── render ────┐
                  └────┬─────────────┬───────────────┬───┘                │
                       │             │               │ --fix              ▼
                       ▼             ▼               ▼              internal/doctor
           doctor.ConfigChecks  previewSelection  "aide …" child    Report → text / JSON
           (cfg) []Finding      + PlanProfileCredentials  processes
                                + launcher.ResolveSandbox
                                + sandbox.PolicyFromConfig
                                     │
                                     ▼
                        Launch calls the same ResolveSandbox
```

1.  **`internal/doctor` (new package): findings, with no cobra or terminal dependency.**
    *   A finding has `id`, `subject`, `severity` (`error`, `warning`, `tip`), `widens_access` (true when applying its fix gives the agent more access; `false` for findings with written steps), `layer` and `source` (the file), `profile`, `message`, and a `fix` with `argv`, `side_effects` and `steps` (a finding has either `argv` or `steps`).
    *   Every finding ID has exactly one producer (see "Finding catalog"). Where two producers notice the same problem, the catalog names the owner and doctor drops the other.
    *   `ConfigChecks(cfg, lookPath)` holds validate's current checks, moved unchanged. It knows nothing about the current profile; doctor does the filtering.
    *   `fix.go` maps a finding to its command (item 7) and is used from stage 1; `run.go` runs fixes and is added in stage 3.
    *   `Report` holds the directory view, findings, counts and fix results, and renders through `output.Emit` (the shared `--format human|json` flag with its `AIDE_FORMAT` default).
2.  **Directory view: one shared capability and sandbox step.**
    *   New `launcher.ResolveSandbox(cfg, rc, in) (SandboxResolution, error)` holds launch's lines 448-509: merge capability names, resolve the sandbox reference, resolve capabilities with variants, apply their overrides, apply `-N`. `rc` is the resolved profile from selection, after `PlanProfileCredentials` has set its context (`launcher.go:332`).
    *   `in` has: `CWD`, `HomeDir`, `WithCaps`, `WithoutCaps`, `UnrestrictedNetwork`, `VariantOverrides`, `Interactive`, `Prompter`, `AutoYes` and `ConsentStore`. Pins from `.aide.yaml` are read from `cfg` inside.
    *   `SandboxResolution` has: the capability set, overrides, provenance, the capabilities the profile itself lists (`contextCapSet`), the sandbox config before and after overrides (the "before" lists feed the banner, `launcher.go:470-475`), and whether the sandbox is disabled. Callers pass the project root (`aidectx.ProjectRoot(cwd)`) and compute composition warnings and project markers themselves.
    *   It does not build the policy. Each caller calls `sandbox.PolicyFromConfig` at its own point, so launch keeps building the policy at line 586, after the env checks, and its errors come in today's order. Previews pass the placeholder runtime directory `/tmp/aide-preview` that `which` uses today.
    *   On error it returns what it resolved so far together with the error, so `which` keeps showing partial data as it does now.
    *   Previews pass the consent store read-only and `Interactive: false`, so `which`, `status` and doctor show the variants launch would use. This changes `which` for users with saved variant choices (see Stages).
    *   `Launch`, `which`, `status` and `doctor` call it. The trust and gateway-choice prompts stay in selection and credential planning; the variant-consent prompt moves with the capability step into `ResolveSandbox`, with launch passing its real prompter as today.
    *   Where launch would ask, doctor reports instead: an untrusted `.aide.yaml`, a variant still on its non-interactive default (`Provenance.Reason == "default:non-interactive"`), and a gateway choice not yet made. The view shows what launch does when the question is not answered.
3.  **`aide validate` becomes a printer** over `ConfigChecks` (see Goals). Its sorting and printing stay in `cmd/aide/trust.go`.
4.  **`aide doctor` (`cmd/aide/doctor.go`, new).**
    *   Loads config so that a load failure becomes the finding `config.load-failed` instead of an exit.
    *   Runs `previewSelection`, then `PlanProfileCredentials` without a prompter (as `which` does, `status.go:224`; it does not decrypt), then `ResolveSandbox` and `PolicyFromConfig`.
    *   Output order: the count line; the directory section (profile and `rc.MatchReason`, agent, secret store name, capabilities with variants, guards, network mode, trust state of each `.aide.yaml` layer, project markers); findings for the current profile; then `ConfigChecks` findings for the other profiles. The directory section points to `aide status` and `aide sandbox show` for detail.
    *   Other profiles get config-wide checks only; no sandbox policy is built for them.
    *   Exit code: 1 if any finding is an error, else 0.
    *   Never decrypts. `aide which --resolve` keeps decrypting as a separate step in `cmd/aide`.
5.  **Inside a sandbox.** Launch sets `AIDE_SANDBOX=on` for sandboxed sessions and `off` for profiles with the sandbox disabled (`launcher.go:100-104`). Only `on` counts as inside. Then doctor marks the report as taken from inside the sandbox and refuses `--fix`, because the sandbox denies writes to `~/.config/aide`. If loading a config file fails with a permission error (`errors.Is(err, fs.ErrPermission)`) while `AIDE_SANDBOX=on`, doctor reports `doctor.config-unreadable` (run `aide doctor` in a separate terminal) instead of `config.load-failed`. The sandbox is expected to deny these reads on Linux and to allow them on macOS; neither is tested yet, and the rule above handles both.
6.  **Widening access.** Each check sets `widens_access` for its own fix: enabling a capability, opening a path, a guard change that opens paths or a network change is `true`; removing a redundant rule is `false`. It is shown in the prompt and the JSON; there is no classifier. Findings with no command (an untrusted or denied `.aide.yaml`, a missing secret file, a broken template, a missing binary, a rule in the `inherited` layer or in a capability definition) carry written steps instead.
7.  **Fix commands.** One function in `internal/doctor/fix.go` maps a finding to a command, with a table test per row. `argv[0]` is replaced by the running binary (`os.Executable()`) when run.

    | Finding | Where | Command |
    |---|---|---|
    | redundant rule | current profile's block in the global config | `aide sandbox allow --remove [--write] <path> --global --profile <profile>` |
    | redundant rule | local `.aide.yaml` | `aide sandbox allow --remove [--write] <path>`, run in the directory whose `.aide.yaml` it targets |
    | redundant rule | named sandbox | `aide sandbox edit <name> --remove-readable\|--remove-writable <path>` |
    | redundant rule | `inherited` `.aide.yaml`, capability definition | written steps |
    | suggested capability | local `.aide.yaml` exists and is trusted | `aide cap enable <name>` in that directory (re-approves the file: listed as a side effect) |
    | suggested capability | no local `.aide.yaml`, or it is untrusted | written steps: run `aide cap enable <name>`, review `.aide.yaml`, run `aide trust` |

    Paths are passed exactly as written in the config, as separate arguments with no shell, so `~` is not expanded and the command matches the stored entry.
8.  **`--fix`.**
    *   Runs each command as a child process with the same working directory (or the target directory, for a local `.aide.yaml`), stdin closed, `AIDE_FORMAT` unset, and stdout and stderr captured into `fix_results`.
    *   Every fix asks on a terminal, one at a time, showing the command, the layer, the file, the side effects and whether it widens access. Without a terminal nothing runs. Findings without a command print their steps.
    *   After each fix doctor re-runs all checks. It attempts only findings that existed at the start, each at most once; findings that first appear after a fix are reported with status `not-run`. The first fix that fails stops the run.
    *   The whole run is refused when the config failed to load, when the config is in the legacy layout (`cfg.LegacyMonolithic`), when a gateway migration is pending (`cfg.NeedsGatewayMigration`), or inside a sandbox. A single fix is skipped when its target `.aide.yaml` is listed in `res.Gate.Files` (untrusted or denied).
    *   `fix_results` status: `ran` (exit 0), `failed` (non-zero; stops the run), `skipped` (refused before running, with a reason), `not-run` (declined, no terminal, new after a fix, or left after a stop).
9.  **Config-shape suggestion: `sandbox.redundant-allow`.**
    *   A rule is redundant when another rule in the **same list** of the **same block** (`readable_extra` or `writable_extra`) is a strict parent of it after both are resolved through symlinks, and no deny rule or active guard covers a path under the parent that contains or equals the child. A read rule under a write rule is not redundant (different list). Rules with templates or project-relative paths are skipped.
    *   Paths are resolved with `filepath.EvalSymlinks`, falling back to `filepath.Clean` on error (as the sandbox's own `resolveSymlink` does, `internal/sandbox/sandbox.go:453`), and compared with `fsutil.IsUnderDir` (`internal/fsutil/under.go:21`) plus `path != parent`.
    *   It is checked for the current profile only.
    *   Its fix needs `--remove` on `aide sandbox allow` (boolean; `--write` selects `writable_extra`). It removes the exact entry from the existing block, errors if the entry is absent, and never converts a named sandbox to an inline block (it does not call `ensureInlineSandbox`); a profile using a named sandbox is routed to `sandbox edit`. It changes the config only: the agent's permission store keeps the entry, which stays correct because the parent rule still grants the path. `sandbox deny` gets no `--remove`: no finding needs it.
10. **Plugin.**
    *   `/aide doctor` and `config-review` run `aide doctor --format json` and explain its findings. Inside the sandbox they give each fix's command for the user to run in a separate terminal, as the skills already instruct.
    *   `context-doctor` and `secrets-doctor` use the directory section and their related findings as a starting point. `sandbox-doctor` stays on runtime denials (`aide sandbox test`, `aide --diagnose`).
    *   The plugin's `/aide doctor` today also reports overlapping match rules, no sandbox override, unrestricted network, guards worth enabling and secrets with one recipient. Each stays in the plugin until doctor emits it. Invalid config syntax and secrets used without a secret store are already covered by `config.load-failed` and `config.secret-without-store`.
    *   With an aide that has no `doctor`, or a newer `schema_version` than the plugin knows, the plugin shows the raw command output without severities and suggests upgrading.

### Finding catalog (stage 1)

Findings with written steps have `widens_access: false`.

| ID | Producer | Severity | Widens access | Subject | Fix |
|---|---|---|---|---|---|
| `config.load-failed` | doctor | error | n/a (steps) | file | Steps: the parser's reason and line |
| `doctor.config-unreadable` | doctor | warning | n/a (steps) | file | Steps: run `aide doctor` outside the sandbox |
| `config.unknown-agent` | ConfigChecks | error | n/a (steps) | profile | Steps: add the agent or change the profile's agent |
| `config.secret-missing` | ConfigChecks | error | n/a (steps) | profile | Steps: create the secret file or `aide profile remove-secret` |
| `config.sandbox-ref-invalid` | ConfigChecks | error | n/a (steps) | profile | Steps: fix the sandbox name. For the current profile, owned by `sandbox.resolve-failed` |
| `config.template-invalid` | ConfigChecks | error | n/a (steps) | profile + env var | Steps: the template error |
| `config.secret-without-store` | ConfigChecks | error | n/a (steps) | profile + env var | Steps: `aide profile set-secret` |
| `config.no-match-rules` | ConfigChecks | warning | n/a (steps) | profile | Steps: `aide profile bind` |
| `config.agent-not-on-path` | ConfigChecks | warning | n/a (steps) | agent | Steps: install it or set `binary`. Doctor drops it for the current profile's agent |
| `agent.not-found` | doctor (current profile, `launcher.ResolveAgentBinary` then `PATH`) | error | n/a (steps) | agent | Steps: install it; launch would fail. An agent missing from the agents map is `config.unknown-agent` |
| `secret.key-missing` | doctor (current profile, `secrets.KeyNames`, no decryption) | error | n/a (steps) | env var | Steps: add the key to the secret file |
| `project.parse-failed` | selection (`cfg.ProjectConfigErr`) | error | n/a (steps) | `.aide.yaml` | Steps: the parser's reason |
| `project.untrusted` | selection (`res.Gate.Files`) | warning | n/a (steps) | `.aide.yaml` | Steps: review the file, then `aide trust` |
| `project.denied` | selection (`res.Gate.Files`) | tip | n/a (steps) | `.aide.yaml` | Steps: the file is denied and ignored; `aide untrust` to be asked again |
| `selection.notice` | selection (`res.Notices`, including gate warnings) | warning | n/a (steps) | profile | The notice, e.g. "launch would fail: …" |
| `sandbox.resolve-failed` | ResolveSandbox | error | n/a (steps) | profile | Steps: the error (unknown capability, failed variant selection, bad sandbox reference); launch would fail |
| `sandbox.policy-failed` | PolicyFromConfig | error | n/a (steps) | profile | Steps: the error; launch would fail |
| `capability.consent-pending` | ResolveSandbox (provenance) | tip | n/a (steps) | capability | Steps: run `aide` once to choose, or `aide cap variants` |
| `credential.choice-pending` | PlanProfileCredentials (`Source == SourceUnset`, harness not `ReachMulti`) | warning | n/a (steps) | profile | Steps: `aide gateway use <name\|none> --agent <harness>`; a non-interactive launch fails until then |
| `credential.store-missing` / `credential.key-missing` | PlanProfileCredentials bindings (`KeyNames`, no decryption) | error | n/a (steps) | binding | Steps: create the store or add the key |
| `gateway.target-refused` | `config.ValidateGatewayTarget` on selected gateways | error | n/a (steps) | gateway | The error; launch would fail |
| `credential.warning` | CredentialWarnings | warning | n/a (steps) | env var | The warning |
| `capability.composition` | CompositionWarnings | warning | n/a (steps) | capabilities | The warning |
| `sandbox.path-warning` | PolicyFromConfig | tip | n/a (steps) | path | The warning |
| `sandbox.paths-missing` | PolicyFromConfig ("skipped:" warnings) | tip | n/a (steps) | profile | One tip counting granted paths that do not exist; steps: `aide sandbox show` |
| `credential.plan-failed` | PlanProfileCredentials | error | n/a (steps) | profile | The error; launch would fail |
| `selection.failed` | selection | error | n/a (steps) | — | No profile can be selected; steps: `aide which` |
| `capability.suggested` | ResolveSandbox (markers); for `gpu`, doctor's own GPU-library markers, since `gpu` has none by design | tip | yes | capability | See item 7 |
| `capability.path-denied` | doctor (capability paths vs `denied_extra`) | warning | n/a (steps) | capability:path | Steps: drop the deny, or `aide cap disable <name>` |
| `profile.match-tie` | `context.TiedMatches` | warning | n/a (steps) | tied profiles | Steps: make one rule more specific (`aide profile bind <profile> --scope repo`) |
| `sandbox.network-unrestricted` | PolicyFromConfig | tip | n/a (steps) | profile | Steps: `aide sandbox network outbound`, `aide sandbox ports 443`, or `aide cap disable network` |
| `sandbox.opt-in-guards` | `guards.AllGuards` | tip | n/a (steps) | profile | Steps: `aide sandbox guards`, `aide sandbox guard <name>` |
| `secret.single-recipient` | `secrets.ListRecipients` (sops metadata, no decryption) | tip | n/a (steps) | store | Steps: `aide secrets rotate <store> --add-key <key>` |
| `sandbox.default-policy` | doctor (no sandbox in profile or project) | tip | n/a (steps) | profile | Steps: `aide sandbox show`, then allow, deny or guard |

Stage 2 adds `sandbox.redundant-allow` (tip, `widens_access: false`, fix per item 7). Each later check story adds its rows here with producer, severity, `widens_access` and fix; the AIDE-5mk stories cover overlapping match rules, no sandbox override, unrestricted network, guards worth enabling, single-recipient secrets and the variant cross-check.

### How doctor fits with the existing commands

Doctor is the only command that grades findings and offers fixes. Its directory section stays short and points to the commands below for detail.

| Command | Question it answers | Change with doctor |
|---|---|---|
| `validate` | Is my config file valid? | None visible; prints from `doctor.ConfigChecks` |
| `which` | Which profile matches here, and why? | Calls `launcher.ResolveSandbox`; shows saved variant choices |
| `status` | What will this profile give the agent? | Calls `launcher.ResolveSandbox`, so its policy, guard count and network mode match launch, including from a subdirectory |
| `sandbox show` / `test` | What exactly will the sandbox allow? | None; doctor points here for policy detail |
| `sandbox guards` | Which guards exist and which are on? | None |
| `cap audit` / `check` / `list` | What does a capability add? | None in this design; moving `cap list` and `cap audit` onto `ResolveSandbox` together is a follow-up |
| `explain` | How do I configure X? | None; a finding's written steps can name an `explain` topic |
| `aide --diagnose` | Why did my last run fail? | None; help texts point to each other |
| **`doctor`** | **What is wrong here, and how do I fix it?** | New |

### JSON contract

The contract is the JSON tags on `internal/doctor`'s report types, locked by a golden test of `aide doctor --format json` (`cmd/aide/testdata/golden/doctor_capabilities.json`).

```json
{
  "schema_version": 1,
  "aide_version": "3.7.1",
  "inside_sandbox": false,
  "directory": { "cwd": "…", "profile": "work", "match_reason": "path ~/src/work",
                 "agent": "claude", "secret_store": "work", "capabilities": [{"name": "go", "variants": []}],
                 "guards": ["…"], "network": "outbound",
                 "project_config": [{"layer": "local", "path": "…/.aide.yaml", "state": "trusted"}],
                 "detected_capabilities": ["git-remote"] },
  "counts": { "errors": 0, "warnings": 1, "tips": 1 },
  "findings": [
    { "id": "sandbox.redundant-allow", "subject": "~/src/a/b", "severity": "tip",
      "widens_access": false, "layer": "global", "source": "~/.config/aide/profiles/work.yaml",
      "profile": "work", "message": "~/src/a/b is already covered by ~/src/a",
      "fix": { "argv": ["aide", "sandbox", "allow", "--remove", "~/src/a/b", "--global", "--profile", "work"],
               "side_effects": [], "steps": [] } },
    { "id": "project.untrusted", "subject": "…/.aide.yaml", "severity": "warning",
      "widens_access": false, "layer": "local", "source": "…/.aide.yaml", "profile": "work",
      "message": "project config is not trusted and does not apply",
      "fix": { "argv": [], "side_effects": [], "steps": ["Review …/.aide.yaml", "Run: aide trust"] } }
  ],
  "fix_results": [
    { "id": "sandbox.redundant-allow", "subject": "~/src/a/b", "status": "ran",
      "exit_code": 0, "output": "…", "reason": "" }
  ]
}
```

`match_reason` is `rc.MatchReason` as launch prints it (free text). `project_config[].state` is `trusted`, `untrusted` or `denied`. Adding fields is allowed within a schema version; renaming or removing one bumps `schema_version`. The report contains no decrypted secret values and no env var values. It does contain paths (cwd, home, config files), profile names, match reasons (which can include repository URLs), env var names and secret key names.

### Stages and stories

1.  **Stage 0: groundwork**, as separate stories in this order:
    1.  Golden tests: validate's output and exit codes; `which` and `status` from a project root; the sandbox config and policy launch builds for a set of fixtures, including one with `-N`, read through a test hook on `Launcher` (`PolicyHook func(*sandbox.Policy)`, called just before the sandbox is applied).
    2.  `doctor.ConfigChecks`, with validate printing from it. Goldens unchanged.
    3.  `launcher.ResolveSandbox`, with `Launch` calling it. Goldens unchanged.
    4.  `which` calls it. Intended change, with its own test: `which` shows saved variant choices. Other goldens unchanged.
    5.  `status` calls it. Intended change, with its own test: from a subdirectory, and for profiles with capabilities, `status` now matches launch.
2.  **Stage 1: `aide doctor` report.** The command, the stage 1 finding catalog, the fix mapping (commands are shown, not run), text and JSON output, broken-config and inside-sandbox handling, exit codes.
3.  **Stage 2: `sandbox.redundant-allow`** and `--remove` on `sandbox allow`.
4.  **Stage 3: `--fix`**, the child runner.
5.  **Stage 4: plugin.** `/aide doctor` and `config-review` read the JSON; the plugin keeps each of its own checks until doctor emits it.

### Files

| File | Stage | Change |
|---|---|---|
| `internal/doctor/finding.go`, `report.go` | 0, 1 | New: findings, report, text and JSON rendering |
| `internal/doctor/configchecks.go` | 0 | New: validate's checks, moved |
| `cmd/aide/trust.go` | 0 | validate prints from `ConfigChecks` |
| `internal/launcher/sandbox_resolve.go` | 0 | New: `ResolveSandbox` |
| `internal/launcher/launcher.go` | 0 | `Launch` calls `ResolveSandbox` (lines 448-509 move); `PolicyHook` field |
| `cmd/aide/status.go` | 0 | `which` and `status` call `ResolveSandbox` |
| `internal/doctor/fix.go` | 1 | New: finding-to-command mapping |
| `cmd/aide/doctor.go`, `cmd/aide/main.go` | 1 | New command, registered |
| `internal/doctor/redundant.go` | 2 | New: redundant-allow check |
| `cmd/aide/sandbox.go` | 2 | `--remove` on `sandbox allow` |
| `internal/doctor/run.go` | 3 | New: child runner and prompt loop |
| `plugins/claude-code/commands/aide-doctor.md`, `skills/config-review`, `skills/context-doctor`, `skills/secrets-doctor` | 4 | Read `aide doctor --format json` |
| `docs/cli-reference.md`, `docs/sandbox.md` | 1, 2 | Document `aide doctor` and `sandbox allow --remove` |
| Golden fixtures under `cmd/aide/testdata/` and `internal/launcher/testdata/` | 0 | New |

### Testing strategy

*   Golden tests from stage 0 run on every later stage.
*   Launch (through `PolicyHook`, with a fake exec) and doctor build the same policy for the same fixtures, ignoring the fields launch adds afterwards: `Env`, `AgentModule`, agent env, the runtime directory and the pi/omp private directories.
*   `which`, `status` and `doctor` show the same profile, capabilities, variants and policy for the same fixture, from the project root and from a subdirectory.
*   Every finding in the catalog has a table test with a fixture that triggers it and one that doesn't. The missing-binary case checks that only `agent.not-found` appears for the current profile; an unknown capability triggers `sandbox.resolve-failed`.
*   Doctor never decrypts: the secrets decryption backend is a stub that fails the test if called; `secrets.KeyNames` reads key names only.
*   A golden of `aide doctor --format json` locks the JSON contract.
*   The fix-command mapping has a row per line of the item 7 table, including a directory that resolves to a different profile than the finding's, the `--write` case, and `cap enable` with a trusted, untrusted and missing `.aide.yaml`.
*   Redundant-allow tests: same list only, deny or guard under the parent, symlinked directories, `/tmp` vs `/private/tmp`, a rule equal to its parent, template paths skipped.
*   `--fix` tests use a fake executable for the child to check that every fix asks, that nothing runs without a terminal, what is skipped, that new findings are `not-run`, and the stop on first failure. One integration test runs the real binary on a temporary config.
*   With `AIDE_SANDBOX=on`, `--fix` is refused and a permission error on load becomes `doctor.config-unreadable`; with `off`, both behave normally.

## Risks & Mitigations

| Risk | Mitigation | Test |
|---|---|---|
| Moving launch's sandbox step changes launch behaviour (for example `-N` lost) | Goldens of the policy before the move, including a `-N` fixture; `Launch` passes every input it used before; the policy is still built at the same point | Stage 0 goldens pass unchanged |
| Doctor's view drifts from launch | Both call `ResolveSandbox` | Policy comparison test |
| Doctor's view differs when launch would ask a question | Doctor shows the unanswered outcome and reports the question as a finding | Fixtures with an untrusted `.aide.yaml`, pending variant consent, pending gateway choice |
| A fix widens access without the user noticing | Every fix asks; widening fixes say so in the prompt; nothing runs without a terminal | `--fix` tests |
| A fix edits the wrong profile or file, or writes a file that then does not apply | Global fixes carry `--global --profile <profile>`; local fixes run in the target directory; `cap enable` is offered only for a trusted `.aide.yaml` | Mapping tests |
| Fixes interfere with each other or loop | Re-run checks after each fix; each finding attempted once; stop on first failure | `--fix` tests |
| A child command prompts or prints into the JSON | stdin closed, `AIDE_FORMAT` unset, output captured | `--fix` tests |
| An older plugin reads a newer JSON | `schema_version`; unknown versions fall back to raw output | Plugin fallback check |
| A repository plants markers to get capabilities suggested | `capability.suggested` is marked as widening access, names the marker file, and is only applied after a yes | Catalog test |
| Doctor inside the sandbox reports a readable config as broken, or tries to write | `AIDE_SANDBOX=on` refuses `--fix`; a permission error becomes `doctor.config-unreadable` | Inside-sandbox test |

## Alternatives Considered

### Alternative A: Grow `aide validate` into the doctor

*   **Pros**: No new command.
*   **Cons**: validate checks the config file; doctor reports on a directory. Merging them mixes both in one output, and adding tips or changing exit codes breaks scripts and CI that run `aide validate`.
*   **Reason for Rejection**: Keeping validate's output and exit codes stable is worth more than saving a command name.

### Alternative B: Add `--check` to `aide which`

*   **Pros**: Smallest change; `which` already resolves the current directory.
*   **Cons**: `which` answers "which profile matches here". Config-wide checks, a JSON contract and fixes don't fit that question, and its output would change for every existing user.
*   **Reason for Rejection**: It changes a command people use for something else.

### Alternative C: Doctor only prints fix commands; the user or the plugin runs them

*   **Pros**: No child-process runner, prompts or loop. Every fix still runs through a command that already validates and handles trust.
*   **Cons**: Applying several fixes means copying commands one by one, and users outside Claude Code get no help applying them.
*   **Reason for Rejection**: `--fix` saves that work, and running the same printed commands keeps the same safety. Stages 1 and 2 deliver the print-only behaviour first, so this alternative is the path to `--fix`, not a competitor.

### Alternative D: `--fix` edits YAML directly

*   **Pros**: No new commands needed.
*   **Cons**: aide's project writer re-approves `.aide.yaml` after writing, and writing without it removes approval; doctor would also have to handle layer merging, list deletion and the legacy layout itself.
*   **Reason for Rejection**: Commands already handle trust, scope and validation, and a direct writer would have to reimplement all of it.

### Alternative E: Exit 0 clean, 1 on warnings, 2 on errors (the April spec)

*   **Pros**: A script can tell "warnings found" from the exit code alone.
*   **Cons**: Any warning, such as an agent binary not on PATH on a CI runner, fails a job running under `set -e`. It differs from validate.
*   **Reason for Rejection**: Matching validate keeps scripts predictable, and the JSON carries the counts for anyone who needs warnings.

### Alternative F: Preflight on every launch now

*   **Pros**: Catches problems without the user running doctor.
*   **Cons**: Adds time to every launch, risks warnings that train users to ignore them, and needs an opt-out. The April spec lists these risks (T5 and T6 in `docs/superpowers/specs/2026-04-14-generic-toolchain-detection-design.md`).
*   **Reason for Rejection**: Postponed until doctor's checks exist and their cost is known.

### Alternative G: Merge with `aide --diagnose` (`internal/diag`)

*   **Pros**: One diagnostics feature.
*   **Cons**: `--diagnose` explains a run that already failed (exit code, stderr, sandbox denials); doctor checks setup before a run. They share no inputs and no output format.
*   **Reason for Rejection**: Two separate tools with linked help text serve both cases with less coupling.

### Alternative H: Split `Launch` into `Plan` and `Execute`

*   **Pros**: Doctor would use launch's own code for everything, including credentials and the banner.
*   **Cons**: Launch asks three questions (project trust, variant consent, gateway choice) in the middle of working out the profile and policy, and each answer changes the result. Config migrations change the config before selection, and the policy includes decrypted env values. A clean split needs a pre-plan step, one prompter for all three questions, re-planning after each answer and a policy comparison that leaves out env: a redesign of aide's riskiest code path.
*   **Reason for Rejection**: Only the capability and sandbox step actually differs between `which`, `status` and launch, and sharing that step gives doctor launch's answer for everything its directory section shows.

## Non-Functional Requirements

*   **Security & privacy:** doctor never decrypts secrets; the report holds no secret or env values (see "JSON contract" for what it does hold). Fixes run as ordinary `aide …` commands with the user's own permissions, every fix needs a yes on a terminal, and no flag skips that.
*   **Performance:** the shared sandbox step reads local files only, with no network access and no child processes; doctor without `--fix` adds one `exec.LookPath` per agent.
*   **Compatibility:** validate's output does not change (see Goals). Two outputs change on purpose: `which` shows saved variant choices, and `status` matches launch from a subdirectory and for profiles with capabilities (see Stages and stories). The JSON is versioned with `schema_version`.

## Additional Resources

*   `docs/superpowers/specs/2026-04-14-generic-toolchain-detection-design.md`, "Preflight diagnostics" (postponed by this design)
*   `docs/superpowers/specs/2026-03-22-claude-code-plugin-design.md` (the plugin's `/aide doctor`)
*   Beads epic AIDE-5mk, "aide doctor", and its stories

## Future Improvements

*   **Preflight on launch** (AIDE-8uw), once checks exist and their cost is measured.
*   **`--strict`**, to exit non-zero on warnings for CI.
*   **Muting findings by ID** in config, as a trust-gated change.
*   **Sandbox policies for every profile**, not only the current directory's.
*   **`cap list` and `cap audit` on `ResolveSandbox`**, moved together so they keep agreeing.
*   **The launch notices doctor defers** (native auth, Codex config, stale gateway choice, credential skipped and ignored notes, legacy credential env), as catalog rows.

--------------------------------------------------------------------------------

## Shaping Record

*   **Evidence:** grounded at revision `e01d76a` on `main`, rechecked 2026-11-25 (`launcher.go:448-585`, `status.go:292-396, 796-811`, `commands.go:80`, `scoped_mutation.go:25-30`). Inspected directly and checked against the code index (refreshed 2026-11-24): `cmd/aide/trust.go`, `cmd/aide/status.go`, `cmd/aide/selection_flags.go`, `cmd/aide/sandbox.go`, `cmd/aide/cap.go`, `cmd/aide/scoped_mutation.go`, `internal/launcher/launcher.go`, `internal/launcher/preview.go`, `internal/capability/detect.go`, `internal/config/project_writer.go`, `internal/config/project_layers.go`, `internal/output/output.go`, `internal/diag`, `plugins/claude-code/commands/aide-doctor.md` and the five plugin skills. Call sites of every warning producer were taken from the index.
*   **Coverage gaps:** whether sops key names can be read without decrypting (Open Questions); macOS sandbox read access to `~/.config/aide` from inside a session was not tested.
*   **Ideation:** EGM brainstorm (seven reviewers) and EGM draft; detailed task planning is left to craft:build.
*   **Review mode:** Full (user selected, 2026-11-24): EGM review plus Ponytail review.
*   **EGM review (round 1, consolidated, stages 5 to 8):** the author kept the full scope and decided that the fix class is set by access change, with a policy-preserving fix to a trusted `.aide.yaml` Safe and the re-approval listed as a side effect.
*   **EGM stage 9 and Ponytail review (round 2):** both found that splitting `Launch` cannot keep the trust prompt's effect on selection, and that the redundant-rule check could never fire as specified. The author chose to share only the capability and sandbox step (2026-11-25). The redundant-rule check now compares by list and path containment, `aide trust` is a written step, `AIDE_SANDBOX` is checked for `on`, the fix mapping uses `--profile` and the real scope rules, and `--remove` is limited to `sandbox allow`.
*   **Round 3 (EGM stage 9 and Ponytail re-review, 2026-11-25):** no blockers. Resolved: `-N` and the other inputs of `ResolveSandbox`; policy building stays at its point in launch; errors from the shared step become `sandbox.resolve-failed` and `sandbox.policy-failed`; previews read the consent store; `credential.choice-pending` detection; trust findings owned by selection, with denied and unparsable files covered; `cap enable` offered only for a trusted `.aide.yaml`; `--remove` changes config only; `cap audit` left for a follow-up; `match_reason` replaces the `matched_by` enum; the macOS read claim marked untested; `secrets.KeyNames` closes the secret-key question.
*   **Author decision (2026-11-25):** `--fix` asks before every fix; the Safe class and its classifier are dropped, and each check marks its fix with `widens_access`.
*   **Review status:** Complete. EGM stages 5 to 9 (stage 9 re-audited once, cap reached) and two Ponytail rounds; all findings addressed in this revision. | **Final approval:** Krishnaswamy Subramanian, 2026-11-25.

--------------------------------------------------------------------------------

*Drafted with EGM*
