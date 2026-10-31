# Shared Pi installation with isolated profile state

Date: 2026-10-30
Status: Independent Goldfish review completed; findings addressed in this revision; re-audit pending.
Implementation: Added in the local feature worktree; independent code review completed and all five findings addressed with regression tests. macOS Seatbelt fixture checks and a real Pi 1.3.0-to-1.4.0 managed update, RPC startup, state isolation, and upstream lock refusal pass; Linux verification remains pending (`AIDE-h4bm`). The separate plan is `docs/superpowers/plans/2026-10-30-pi-managed-install.md`.

## Intent

A user who installs Pi with `curl -fsSL https://pi.dev/install.sh | sh`
should be able to run `aide --profile work --agent pi` without moving Pi,
installing another copy, or disabling the sandbox. `pi update` must work
inside an aide session when its network and subprocess policies permit it.

Profiles selecting the same Pi executable share its installation and upgrade
cycle. The command above assumes `work` already selects isolated native state:
`--profile work` chooses an aide profile, not a Pi configuration directory.
For example, the existing configuration
`profiles: {work: {agent: pi, config_dir_name: work}}` makes aide set
`PI_CODING_AGENT_DIR=~/.pi-work`. Existing `config_dir` and `config_dirs.pi`
overrides remain available under their current validation rules.

This feature preserves configured isolation; it does not create, copy, or
migrate profile state. For profiles selecting isolated native directories,
configuration, credentials, and sessions must remain separate. Profiles using
global state keep their existing behavior and validation; sharing an
installation does not turn them into isolated profiles. Installation access
must never widen an isolated profile's access to another profile's private state.

## Current behavior and evidence

The installer inspected on 2026-10-30 defaults to a managed installation:

```text
~/.pi/agent/bin/pi
~/.pi/agent/install/managed-install.json
~/.pi/agent/install/current-version
~/.pi/agent/install/releases/<version>/node_modules/...
```

The launcher may be reached through a symlink in a PATH directory. It finds
its installation relative to its own resolved location, exports
`PI_MANAGED_INSTALL_ROOT`, and executes the selected release. Setting
`PI_CODING_AGENT_DIR` does not move the installation. The installer supports
custom locations and can install Node under
`${XDG_DATA_HOME:-$HOME/.local/share}/pi-node/`.

For an isolated work profile, aide normally sets
`PI_CODING_AGENT_DIR=~/.pi-work`. Its Pi sandbox module grants that directory,
not the default `~/.pi` tree containing the managed installation. This can
block startup. Linux's automatic grants for an executable and its resolved
parent directory do not cover the launcher's sibling `install/` tree.

The published `@earendil-works/pi-coding-agent` 1.4.0 updater was also inspected.
It locks updates under the managed root, creates staging directories, runs
`npm ci`, verifies the new executable, renames the release, atomically replaces
`current-version`, and prunes older releases. The updater's direct
release-management writes target the managed installation root. Its npm
subprocess can also write caches outside that root; those access needs remain
subject to existing toolchain grants and explicit policies. The inspected
updater does not rewrite the launcher or upgrade Node.

Upstream pruning retains the newly active release and the version performing
the update, not every version used by an open session. Successive updates can
therefore remove an older session's release directory. aide does not manage
release retention; an affected older session may need restarting. The effect
on running sessions and recovery have not been tested.

These observations are source inspection, not a completed sandbox reproduction.

## Goals and boundaries

- Automatically recognize the managed installation belonging to the selected
  Pi executable, including PATH symlinks and custom installation locations.
- Permit startup and Pi's own shared-install update process on macOS and Linux.
- Preserve isolation of profile configuration, credentials, and sessions.
- Keep existing non-managed Pi installation behavior unchanged.
- Do not add configuration, dependencies, per-profile binaries, or an aide updater.
- Do not copy or migrate existing Pi configuration or sessions.
- Do not bypass explicit network, subprocess, or denied-path policies.

## Approaches considered

### Retain one installer-managed installation — selected

Discover and grant the installation separately from profile state. This keeps
Pi's normal installation and update behavior without duplicating packages.
The user explicitly accepted that writable shared executable code lets a
profile affect code subsequently run by other profiles.

### Install Pi separately for every profile — rejected

Separate executable trees reduce cross-profile code modification, but duplicate
packages and create independent versions and upgrade cycles. This conflicts
with the user's requirement to keep one installation.

Allowing all of `~/.pi` or `~/.pi/agent` is not an acceptable shortcut: those
directories also hold private default-profile state.

## Access model

| Resource | Required access |
| --- | --- |
| Managed launcher | Read and execute; no new write grant |
| Managed installation root and descendants | Read, write, and execute |
| Required installer-managed Node runtime | Read and execute; no new write grant |
| Selected profile configuration and sessions | Existing profile-specific access |
| Default or other profile state outside the selected profile | No additional access |

Grant the installation root, not only the current release: staging, locking,
release activation, cleanup, and future releases all need access. Pi owns the
update process; aide only supplies sandbox access.

Discovery must use the executable aide actually selected, not an unrelated
ambient `PI_MANAGED_INSTALL_ROOT`. Resolve symlinks and validate the managed
marker and layout without executing a launcher to discover paths. A marker
alone must not authorize arbitrary access. Resolved grant targets must not
accidentally widen access into private profile state.

Initial support is limited to the inspected installer-generated `bin/pi`
launcher family with a sibling `install/` root and the `pi-managed-install`,
schema version 1, `releases-v1` marker. Custom locations and PATH symlinks are
supported when they preserve this relationship. A recognized launcher format
must establish the relationship statically; a filename, marker, marker
entrypoint path, or inherited environment value alone is insufficient. Do not
execute or interpret arbitrary wrapper scripts to obtain grant targets.
Unknown launcher formats are not automatically supported.

Validate the relationship among the selected launcher, marker, current-version
file, and active release executable, including their resolved paths. Reject
broken or escaping links and installation grants that include private
configuration, credentials, sessions, or another profile's native state
directory. The dedicated `install/` subtree inside the default agent directory
is allowed; this does not authorize granting that parent directory or its
private-state siblings. Do not infer installation write access from the
selected profile's state directory.

| Discovery result | Required behavior |
| --- | --- |
| Ordinary non-managed executable, including an unfamiliar wrapper with no managed candidate | Preserve existing launch behavior; add no managed-install grants |
| Recognized installer launcher, or marker at its expected sibling installation location, but missing, malformed, unsupported, or unsafe layout | Stop before executing Pi; add no grants; report the selected executable, rejected candidate, and actionable reason without reading or printing private state |
| Supported launcher and fully validated safe layout | Add only the installation, launcher, and required runtime grants in the access model |

Once a managed candidate is detected, validation failure must not silently
reclassify it as an ordinary non-managed installation. These checks establish
a supported layout and bounded access, not trusted authorship of executable
code; the shared-code tradeoff remains accepted.

Apply these access boundaries on macOS Seatbelt and both supported Linux
sandbox paths. Before adding installation, launcher, or Node grants, check
their resolved targets against explicit denied paths, including denied
descendants. If the selected backend cannot preserve a conflicting denial,
stop the launch before executing Pi, grant no additional access, and report
the conflicting path and backend. Do not silently drop the denial, disable
the sandbox, or switch backends. Landlock cannot subtract a denied file from
a recursively granted parent; such a managed-install conflict must therefore
fail closed. Repairing unrelated existing sandbox-policy limitations is
outside this feature.

## Security tradeoff

Writable shared code means profile isolation is not protection against a
malicious profile modifying Pi to read another profile's state when that
other profile later executes it. This is explicitly accepted. It does not
justify directly granting a running profile access to another profile's
configuration, credentials, or sessions.

## Required verification

Verify startup through both the direct launcher and a PATH symlink. Cover
custom managed roots, installer-managed Node, and existing non-managed installs.
Exercise an update that stages and verifies a release, switches the active
version, and launches that version in a subsequent process. Confirm that
Pi's update lock still prevents simultaneous updates.

Prove that another profile's configuration, credentials, and sessions remain
unreadable and unwritable, including default-profile state adjacent to the
installation. Test malformed markers, symlink escapes, and layouts overlapping
private state. Explicit denials must not be overridden.

Cover denied installation roots and denied descendants, including a descendant
that does not yet exist. An unenforceable conflict must refuse launch before
Pi executes and identify the conflicting path and backend. A backend that can
enforce the denial must never expose the denied resource. Also cover conflicts
introduced by resolved launcher and Node targets.

Exercise all three discovery outcomes, unsupported marker versions,
unrecognized launchers beside a managed marker, and malformed or unsafe
candidates. Unrelated non-managed installs must receive no new grants;
rejected managed candidates must never execute.

Cover a profile with `config_dir_name`, supported custom directory overrides,
and a global-state profile. Preserve existing session-directory-redirect
scrubbing and add a Pi-specific negative case rather than another scrub
mechanism.

Run these checks against macOS and supported Linux backends. Tests should use
local installer-shaped fixtures and controlled update artifacts rather than
upgrading a developer's actual Pi installation.

## Questions for independent review

- Do the discovery fixtures cover the supported launcher family, custom
  locations, and each defined rejection outcome without trusting arbitrary
  shell declarations?
- Do rejection tests prove that invalid or private-state-overlapping managed
  candidates never execute, while unrelated non-managed installs retain
  their existing behavior?
- Which Node paths and resolved targets are needed for startup, npm subprocesses,
  and release verification under each backend?
- Can the chosen backend enforce equivalent grants for paths that do not yet
  exist, particularly staging and future release directories?
- Does any inherited session-directory override bypass existing profile
  isolation, and do the negative tests detect it?

## Reading list

Repository baseline:

- `internal/harness/harness.go`
- `internal/launcher/launcher.go`
- `internal/launcher/selection.go`
- `internal/launcher/launcher_test.go`
- `internal/launcher/scrub_test.go`
- `internal/config/profile_dirs.go`
- `pkg/seatbelt/modules/pi.go`
- `pkg/seatbelt/modules/simple_agent.go`
- `pkg/seatbelt/modules/profile_isolation_test.go`
- `internal/sandbox/darwin.go`
- `internal/sandbox/linux.go`
- `internal/sandbox/profile_grants_linux_test.go`
- `pkg/seatbelt/guards/guard_node_toolchain.go`

Upstream evidence, subject to change:

- Installer: https://pi.dev/install.sh
- Published package: https://registry.npmjs.org/@earendil-works/pi-coding-agent/-/pi-coding-agent-1.4.0.tgz
  (`dist/package-manager-cli.js` contains the inspected managed updater).

## Review handoff

Use this file as `TargetFile` for `egm-review`. It is a feature specification,
so the feature-spec-only preset applies to the whole document; no detailed
implementation range exists yet. Reviewers should read the referenced files.
Independent Goldfish review completed for the original 170-line revision.
All five consolidated findings have been addressed in this revision with user
approval. The amended text has not been independently re-audited; runtime
verification remains pending on Linux.
Implementation is present in the local feature worktree, with native unit/race
tests passing. After removing the surrounding session sandbox, all four macOS
Seatbelt fixture checks ran and passed without skips. That run exposed and
fixed a fixture working-directory issue and missing metadata-only traversal
rules for installation/runtime ancestors; no parent-directory contents were
granted. A subsequent real-package smoke test used official Pi release
manifests and standalone Node 22.23.4 in an isolated HOME: the built aide ran
Pi's real updater from 1.3.0 to 1.4.0, refused an actual proper-lockfile lock,
started the real RPC runtime, and blocked reads/writes of six synthetic
other/default-profile auth/config/session files. No model call or normal user
profile was used. No Linux runtime was available. The original reports retain
their historical verdicts and line references.

- [Consolidated review report](/Users/subramk/.pi/agent/sessions/--Users-subramk-source-github.com-jskswamy-aide--/subagent-artifacts/outputs/749ddc97-505b-4008-9918-4399b4e7639a/egm/consolidated_review_report.md)
- [Resolution record](/Users/subramk/.pi/agent/sessions/--Users-subramk-source-github.com-jskswamy-aide--/subagent-artifacts/outputs/749ddc97-505b-4008-9918-4399b4e7639a/egm/goldfish_review_report.md)
