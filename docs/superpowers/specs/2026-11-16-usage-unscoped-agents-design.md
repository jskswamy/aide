# Keep agents without profile support out of named-profile usage reports

Status: Candidate (shaping) · Date: 2026-11-16
Amends: `2026-11-09-aide-usage-command-design.md` (Non-Goals, Anti-Goals)

## Problem

`aide usage --profile work` shows Antigravity (`agy`) usage. So do the
`firmus` and default profile reports, and the numbers are identical. The
same 10 days of `antigravity` rows appear in every profile.

Observed on 2026-11-16 with ccusage 20.0.37 (`daily --json --by-agent`):

| Report | antigravity rows | other agents |
| --- | --- | --- |
| default profile | 10 | claude, opencode, pi, codex |
| `--profile work` | 10 | claude, pi |
| `--profile firmus` | 10 | claude, opencode, pi |
| `--all` | 10 | all |

This breaks the usage spec's anti-goal: *"Failure is acceptable;
wrong-profile data is not. … never fall back to another profile's (or
the global) dir."*

## Cause

ccusage's unified report reads every agent it knows (claude, codex,
opencode, amp, droid, codebuff, cline, hermes, pi, goose, kilo, kimi,
qwen, openclaw, copilot, gemini, antigravity, grok, zcode). aide pins a
data dir only for the six agents in `ccusageBackend.HarnessEnv`. For
every other agent, ccusage falls back to its global default dir, which
for Antigravity is `~/.gemini/antigravity*`.

The usage spec placed non-profile harnesses out of scope on the
assumption that *"Antigravity is not an aide harness; ccusage's own
default discovery covers it."* Commit `de03bdb` made `agy` an aide
harness, which invalidated that assumption. The same leak applies to
any other agent ccusage reads but aide cannot scope, such as amp,
goose or hermes.

Users cannot fix this themselves, and should not have to. aide already
knows which harnesses support profiles (`harness.Descriptor.SupportsProfiles`).

## Outcome

* A **named profile's** report (`--profile X`, or a folder that matches a
  profile with its own config dirs) contains only agents whose data aide
  can scope to that profile. Agents without per-profile data report
  "no data" there.
* The **default profile** (which uses the harnesses' shared, global
  dirs) keeps showing those agents' usage. Their global dir *is* the
  default profile's dir, which matches how `NativeConfigDir` already
  treats a profile that selects no config dir.
* `--all` is unchanged and shows shared agents once.
* The user configures nothing.

Known consequence: agy sessions launched under a named profile still
write to `~/.gemini`, so their usage is counted under the default
profile. Per-profile agy accounts are parked separately (AIDE-wisp-111).

## Design

Owning module: `internal/usage`, the ccusage backend seam. No change to
`internal/harness`, `internal/config` or `cmd/aide`, and no change to any
public or cross-module contract.

1. Add one field to `Backend`:

   ```go
   // UnscopedVars are the backend's data-dir vars for agents aide
   // cannot scope per profile. A named profile points each at a path
   // that cannot exist, so the backend reports no data rather than
   // the global dir.
   UnscopedVars []string
   ```

   For `ccusageBackend`, this lists the data-dir variables from ccusage's
   environment-variable docs whose agents are not in `HarnessEnv`:
   `ANTIGRAVITY_DATA_DIR`, `AMP_DATA_DIR`, `DROID_SESSIONS_DIR`,
   `CODEBUFF_DATA_DIR`, `CLINE_DATA_DIR`, `HERMES_HOME`, `GOOSE_PATH_ROOT`,
   `OPENCLAW_DIR`, `KILO_DATA_DIR`, `KIMI_DATA_DIR`, `QWEN_DATA_DIR`,
   `GROK_HOME`, `ZCODE_HOME`.

2. `ProfileEnv`: when `ctx.SelectsConfigDir()` is true, append
   `VAR=/dev/null/aide-unscoped` for each `UnscopedVars` entry. A path
   under `/dev/null` can never be a directory. When `SelectsConfigDir()`
   is false (the default profile), it emits nothing for these vars.

3. `ScrubEnv`: no change. `usage.Run` uses `exec.Command`, which keeps
   the last value of a repeated variable, and `scopeEnv` comes after the
   user's environment, so a named profile's path always wins. A value the
   user exports (for example a custom `GOOSE_PATH_ROOT`) still applies to
   the default profile and `--all`.

4. `AllEnv`: no code change. It calls `ProfileEnv` for every profile and
   keeps only folders that exist (`os.Stat` plus `IsDir`), so the
   impossible path is dropped and ccusage reads the global dirs once. A
   test pins this behaviour.

The list of unscoped variables is a snapshot of ccusage 20.0.37. A future
ccusage agent leaks until someone adds its variable. A `shortcut:`
comment on the list records this limit.

### Evidence that the mechanism works

With ccusage 20.0.37 on this machine:

* `ccusage antigravity daily` returned 10 rows by default and 0 rows with
  `ANTIGRAVITY_DATA_DIR` set to a missing path. It returned 0 rows again
  with `/dev/null/aide-unscoped`, and the exit code was 0.
* The unified `ccusage daily --by-agent` report with that variable set
  listed claude, codex, omp, opencode, pi, pi-firmus and pi-work, with
  no antigravity. So a missing dir does not fall back to the global dir.

## Testing

`internal/usage/usage_test.go`:

* `TestProfileEnv_NamedProfileBlanksUnscopedAgents`: with
  `Context{Profile: "forge"}`, every `UnscopedVars` entry is assigned a
  path that does not exist.
* `TestProfileEnv_DefaultProfileLeavesUnscopedAgents`: with `Context{}`,
  no `UnscopedVars` entry is assigned.
* `TestAllEnv_OmitsUnscopedVars`: a config with a named profile gives
  `AllEnv` output that assigns no `UnscopedVars` entry.
* Update `TestProfileEnv_SkipsHarnessesWithoutProfileSupport`: it counts
  assignments against `len(HarnessEnv)` and must add `len(UnscopedVars)`
  for a named profile.

Manual check: run `aide usage --profile work daily --json --by-agent` and
confirm it contains no `antigravity` rows, while the default-profile run
still contains them.

## Out of scope

* Per-profile agy logins or data (parked: AIDE-wisp-111).
* Gemini's `GEMINI_HOME` versus `GEMINI_CLI_HOME` mismatch for launches
  (`internal/harness/harness.go`, comment on the gemini entry).
* Detecting new ccusage agents automatically.

## Architecture evidence

* Scope: `internal/usage` (usage.go `Backend`, `ccusageBackend`,
  `ProfileEnv`, `ScrubEnv`, `AllEnv`), `cmd/aide/usage.go` (caller,
  unchanged), `internal/harness` (`SupportsProfiles`, read-only),
  `internal/config` (`Context.SelectsConfigDir`, read-only).
* Revision: `213a14e`, clean tree. Inspected the source directly; the
  graph index was last refreshed 2026-11-15.
* Dependency direction is unchanged: usage → config, harness.
* Gaps: the list of ccusage environment variables comes from ccusage
  docs, and only antigravity was verified with real data.

## Review record

Review mode: Lightweight — user selected Ponytail only (2026-11-16)
Scope: one module (`internal/usage` backend seam); no public or
cross-module contract change; EGM intentionally skipped, not completed.
Evidence provenance: source inspected directly at `213a14e` (clean tree);
ccusage 20.0.37 behaviour tested on this machine.

Ponytail review of the candidate (proposed-design checks; no
implementation tests ran):

| # | Finding | Severity | Disposition |
| --- | --- | --- | --- |
| 1 | Scrubbing `UnscopedVars` hides user-set custom dirs in default/`--all` and is unnecessary (`exec.Command` keeps the last value) | Major | Fixed: step 3 dropped |
| 2 | `AllEnv` reasoning was wrong (the existence filter drops the path, not scrubbing) and untested | Major | Fixed: step 4 corrected, `TestAllEnv_OmitsUnscopedVars` added |

Re-review after fixes: no new findings.
Review status: Complete
Final approval: User approved 2026-11-16 (beads: epic AIDE-i3j4, story AIDE-i3j4.1)
