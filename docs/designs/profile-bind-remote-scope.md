# Interactive Remote Scope Selection & Conflict Resolution for Profile Binding

*   **Author:** jskswamy
*   **Status:** Draft
*   **Date:** 2026-11-01

--------------------------------------------------------------------------------

## Overview

`aide` selects a user profile (`config.yaml` under `profiles:` or `contexts:`) by scoring `match:` rules against the current working directory (`cwd`) and its git remote URL in [`Resolve`](../../internal/context/resolver.go#L69-L85). While the matcher in [`scoreRemoteRule`](../../internal/context/resolver.go#L412-L436) already supports glob patterns such as `remote: github.com/org/*` and `remote: gitlab.com/org/team/*`, the CLI commands that create match rules—[`contextBindCmd`](../../cmd/aide/profile.go#L174-L284) (`aide profile bind`), [`runCreateWizard`](../../cmd/aide/profile.go#L399-L514) (`aide profile create`), and [`handleEmptyState`](../../internal/launcher/empty_state.go#L51-L119) (first-run prompt)—only generate exact repository rules (`github.com/org/repo`) or exact folder path rules (`cwd`) via [`autoDetectMatchRule`](../../cmd/aide/match_rule.go#L20-L25).

This design updates the profile binding workflow so users can select any level of their git remote hierarchy (exact repository, team/subgroup glob, or organization glob) or the local folder path via an interactive prompt or explicit CLI flags, and resolves cross-profile rule conflicts interactively before saving `config.yaml`.

## The Problem

1. **Per-Repository Binding Repetition:** Developers who work across many repositories under the same GitHub organization or GitLab group must run `aide profile bind <profile>` in every new repository checkout, or manually edit `~/.config/aide/config.yaml` to replace `github.com/org/repo` with `github.com/org/*`.
2. **Rigid CLI Flags:** `aide profile bind` only exposes boolean `--remote` and `--path` flags, offering no CLI mechanism to bind an organization (`host/org/*`) or nested team/subgroup (`host/org/team/*`).
3. **Silent Shadowing and Unresolved Rule Conflicts:** [`findExistingBind`](../../cmd/aide/match_rule.go#L53-L65) only checks whether another profile has the *exact* same rule string. If a user binds `remote: github.com/org/*` (specificity tier `100`) to profile `work`, but another profile `personal` has `path: ~/source/*` (specificity tier `200`) or `remote: github.com/org/this-repo` (specificity tier `250`), `aide profile bind` writes the new rule and prints a success message even though `personal` continues to win when `aide` is launched in that directory.

## Goals

*   **Interactive Scope Selection:** Present a numbered menu when binding a git repository on an interactive TTY (without scope flags) across `aide profile bind`, `aide profile create`, and the empty-state launch prompt, ordered from narrowest to broadest:
    1. Exact repository (`host/org/.../repo`, default `[1]`)
    2. Each parent team/subgroup prefix (`host/org/team/*`) up to the top-level organization (`host/org/*`)
    3. Exact folder path (`cwd`)
*   **Explicit Scope Flags (Breaking Change to `--remote`):** Replace the boolean `--remote` and `--path` flags on `aide profile bind` with `--scope <value>` so non-interactive scripts and CLI users can target repository, organization, team prefix, or folder path directly without prompting.
*   **Pre-Persist Cross-Profile Conflict Resolution:** Evaluate the candidate rule against all profiles in `cfg.Contexts` before writing `config.yaml`. If another profile has an identical rule, a higher-scoring rule that shadows the target profile at `cwd`, or narrower rules overlapped by a newly bound prefix glob, prompt the user with concrete resolution choices.
*   **Same-Profile Redundancy Pruning:** Automatically remove narrower remote rules within the same profile (matching the same `remote_name` git remote alias field on `config.MatchRule`, which defaults to `origin`) when a broader remote prefix glob covering them is added to that profile.
*   **Hermetic Testability:** Share a single buffered input stream across multi-step prompts in `cmd/aide` and `internal/launcher` so interactive scope selection and conflict resolution can be unit-tested deterministically.

## Non-Goals

*   **New `config.yaml` Schema Fields:** We will not add `org:` or `team:` keys to [`config.MatchRule`](../../internal/config/config.go); all remote prefix rules are persisted as standard `remote: "<host>/<prefix>/*"` entries.
*   **Host-Wide Wildcards:** The CLI will never offer or generate bare host globs (`github.com/*`).
*   **Org-Scoped Repository Preferences or `.aide.yaml`:** `aide project set` ([`internal/prefs/prefs.go`](../../internal/prefs/prefs.go)) and `.aide.yaml` remain scoped to individual repositories/checkouts.

## Open Questions

*   **None.**

## Critical User Journeys (CUJs)

### CUJ 1: Interactive Organization Binding with Same-Profile Pruning

*   **Given**: Profile `work` already has `remote: github.com/acme/service-a`. The user is in `/Users/dev/source/github.com/acme/service-b` (whose `origin` remote is `git@github.com:acme/service-b.git`) on an interactive terminal, and no other profile matches `/Users/dev/source/github.com/acme/service-b`.
*   **When**: The user runs `aide profile bind work` and selects `[2] All repositories in github.com/acme/*`.
*   **Then**: `aide` adds `remote: github.com/acme/*` to `work`, prunes the redundant `remote: github.com/acme/service-a` rule from `work`, writes `config.yaml`, and prints confirmation that `work` is bound (`matched by remote github.com/acme/*`).

### CUJ 2: Interactive Nested Group Binding (GitLab Subgroup)

*   **Given**: The user is in a repository whose `origin` remote normalizes to `gitlab.com/acme/platform/auth/token-service`.
*   **When**: The user runs `aide profile bind work` on an interactive terminal.
*   **Then**: `aide` presents:
    1. `This repository only (gitlab.com/acme/platform/auth/token-service)`
    2. `All repositories in gitlab.com/acme/platform/auth/*`
    3. `All repositories in gitlab.com/acme/platform/*`
    4. `All repositories in gitlab.com/acme/*`
    5. `This folder path only (<cwd>)`
    Selecting `[2]` binds `remote: gitlab.com/acme/platform/auth/*`.

### CUJ 3: Resolving a Shadowing Conflict Against Another Profile's Path Glob

*   **Given**: Profile `personal` has `path: ~/source/*` (score `200 + len`). The user is inside `~/source/github.com/acme/billing` and runs `aide profile bind work`, choosing `remote: github.com/acme/*` (score `100 + len`).
*   **When**: `aide` evaluates the candidate config before saving and detects that `personal` (`path: ~/source/*`) still outscores `work` (`remote: github.com/acme/*`) for `cwd`.
*   **Then**: `aide` prompts the user to choose how to resolve the conflict (defaulting to `[1]` on Enter):
    1. Remove `path: ~/source/*` from `personal` so `work` applies here
    2. Also bind `remote: github.com/acme/billing` (score `250 + len`) to `work` so `work` overrides `personal` in this repository while keeping `path: ~/source/*` on `personal`
    3. Keep both as-is (save `remote: github.com/acme/*` on `work`, leaving `personal` active in `cwd`)
    4. Cancel (`[c]`) without saving

### CUJ 4: Resolving Narrower Rules on Another Profile When Binding an Organization

*   **Given**: Profile `personal` has `remote: github.com/acme/OSS-tool` (score `250 + len`). The user is in `github.com/acme/internal-api` and binds `work` to `remote: github.com/acme/*`.
*   **When**: `aide` checks all profiles before saving and detects that `personal`'s `github.com/acme/OSS-tool` falls under `github.com/acme/*`.
*   **Then**: `aide` asks whether to (defaulting to `[1]` on Enter):
    1. Keep `github.com/acme/OSS-tool` on `personal` as a single-repository override
    2. Remove `github.com/acme/OSS-tool` from `personal` so `work` covers all of `github.com/acme/*`
    3. Cancel (`[c]`) without saving

--------------------------------------------------------------------------------

## Technical Plan (Architecture)

### 1. End-to-End Binding & Conflict Resolution Flow

```mermaid
flowchart TD
    Start["User invokes bind / create / empty-state [1] or [2]"] --> Target["Resolve target profile name"]
    Target --> Detect["Inspect cwd & aidectx.CurrentRemote(cwd)"]
    Detect --> BuildOpts["Build ordered scope options:<br/>1. repo (host/org/.../repo)<br/>2..N. parent prefixes (host/org/team/* -> host/org/*)<br/>last. path (cwd)"]
    BuildOpts --> CheckScopeFlag{"--scope flag set?"}
    CheckScopeFlag -- Yes --> MatchFlag["Validate & select matching scope option"]
    CheckScopeFlag -- No --> CheckTTY{"Interactive TTY &<br/>git remote present?"}
    CheckTTY -- No --> DefaultOpt["Select option [1] (repo if git remote, else path)"]
    CheckTTY -- Yes --> PromptScope["Prompt numbered menu [1..N] (default [1])"]
    MatchFlag --> PreCheck["Run same-profile pre-check against targetProfile"]
    DefaultOpt --> PreCheck
    PromptScope --> PreCheck
    PreCheck --> ExactCheck{"Stage A: Exact collision on<br/>another profile?"}
    ExactCheck -- Yes --> PromptExact["TTY: Prompt [1] Move to target or [c] Cancel<br/>Non-TTY: Return collision error"]
    ExactCheck -- No --> ShadowCheck
    PromptExact --> ShadowCheck{"Stage B: Other profile(s) outscore<br/>target at cwd?"}
    ShadowCheck -- Yes --> PromptShadow["TTY: Prompt [1] Remove shadowing rule(s),<br/>[2] Also bind override rule (if eligible),<br/>[3] Keep both, or [c] Cancel<br/>Non-TTY: Return shadowing error"]
    ShadowCheck -- No --> OverlapCheck
    PromptShadow --> OverlapCheck{"Stage C: New rule is prefix glob &<br/>other profiles have narrower rules?"}
    OverlapCheck -- Yes --> PromptOverlap["TTY: Prompt [1] Keep as overrides,<br/>[2] Remove from other profile(s), or [c] Cancel<br/>Non-TTY: Keep as overrides"]
    OverlapCheck -- No --> PruneSame
    PromptOverlap --> PruneSame["Stage D: Prune redundant narrower remote rules on targetProfile"]
    PruneSame --> Save["Persist via config.WriteConfig(cfg)"]
```

### 2. Scope Option Generation (`cmd/aide/match_rule.go`)

Given `cwd` and its normalized git remote from [`aidectx.CurrentRemote`](../../internal/context/git.go#L46-L48) (`host[:port]/path`, or `""` when `cwd` has no host-based `origin` remote), the scope builder generates an ordered list of candidate bind scopes:

*   **Non-git directory or local-only remote:** Produces a single folder-path option (`path: <cwd>`, shorthand `"path"`).
*   **Git repository with a host remote:**
    1.  **Exact repository (option `[1]`, default):** Matches `remote: <host>/<path>` (shorthand `"repo"`).
    2.  **Parent team/subgroup and organization prefixes (options `[2..N]`):** Walks from the deepest parent group (`<host>/<org>/<team>/*`, shorthand `"<org>/<team>"`) up to the top-level organization owner (`<host>/<org>/*`, shorthand `"org"`).
    3.  **Exact folder path (final option):** Matches `path: <cwd>` (shorthand `"path"`).
*   **Host-only safety invariant:** Prefix generation stops at the first path segment under `<host>` (`<host>/<org>/*`) and never produces bare `<host>/*`. For a two-segment remote (`<host>/<repo>` with no organization segment), no prefix globs are generated and only the exact repository and folder path options appear.

### 3. CLI `--scope` Flag & Shell Completion (`cmd/aide/profile.go`)

*   On [`contextBindCmd`](../../cmd/aide/profile.go#L174-L284), replace the boolean `--path` and `--remote` flags with `--scope <value>`.
*   Flag resolution matches the user's `--scope` value directly against the generated options for `cwd`:
    *   Matches any option's shorthand (`"repo"`, `"org"`, `"path"`, or relative subgroup path `"<org>/<subteam>"`) or its canonicalized remote rule (with or without a trailing `/*`).
    *   If `"repo"` or `"org"` is requested when that scope does not exist for `cwd` (for example, `"repo"` outside a git repository, or `"org"` on a 2-segment `<host>/<repo>` remote), or if an unrecognized prefix is passed, `aide` returns an error listing the valid `--scope` values for `cwd`.
*   Dynamic Cobra flag completion on `--scope` inspects `cwd` and suggests each valid shorthand and remote prefix for the current repository.

### 4. Pre-Persist Cross-Profile Conflict Analysis & Resolution (`cmd/aide/match_rule.go` & `internal/context/resolver.go`)

[`internal/context/resolver.go`](../../internal/context/resolver.go#L323-L331) exports `ScoreRule` and `CanonicalizeRemotePattern` so `cmd/aide` evaluates rule specificity and remote equivalence with the exact same logic used at launch time. All remote rule comparisons normalize empty `RemoteName` values to [`aidectx.DefaultRemoteName`](../../internal/context/git.go#L16) (`"origin"`).

Before writing `config.yaml`, the bind workflow runs a same-profile pre-check followed by four resolution stages (Stages A–D), where interactive prompts default to `[1]` on Enter and `[c]` cancels without writing:

*   **Same-Profile Pre-Check:**
    *   If `targetProfile` already has an identical rule or a broader remote prefix rule (with the same effective `remote_name`) that covers `newRule`, and no other profile outscores `targetProfile` at `cwd`, `aide` prints `This folder is already bound to profile "<targetProfile>".` and exits as a no-op.
*   **Stage A — Exact Cross-Profile Collision:**
    *   Triggered when another profile `otherProfile` has a rule equivalent to `newRule` (matching path, or matching effective `remote_name` and canonicalized remote pattern).
    *   In non-TTY mode: returns an error stating that the rule is already bound to `otherProfile`.
    *   In TTY mode: prompts `[1] Move rule from "<otherProfile>" to "<targetProfile>"` (default) or `[c] Cancel`.
*   **Stage B — Shadowing Conflict at `cwd`:**
    *   Triggered when one or more rules across other profiles match `cwd` with a score that beats `(newScore, targetProfile)` under [`bestMatch`](../../internal/context/resolver.go#L155-L181)'s tie-breaking rules (`otherScore > newScore`, or `otherScore == newScore && otherProfile < targetProfile`).
    *   In non-TTY mode: returns an error naming the highest-scoring shadowing profile and rule.
    *   In TTY mode: presents the resolution menu:
        *   `[1] Remove shadowing rule(s) (<list>) from other profile(s) so "<targetProfile>" applies here` (default; removes all rules on other profiles that outscore `(newScore, targetProfile)` at `cwd`).
        *   `[2] Also bind <overrideRule> to "<targetProfile>" so "<targetProfile>" overrides "<otherProfile>" here` — included only when a narrower rule for `cwd` (`remote: <repo>` at tier `250`, else `path: <cwd>` at tier `300`) does not collide with an existing rule on another profile and its `(overrideScore, targetProfile)` strictly beats the highest shadowing `(conflictScore, otherProfile)`.
        *   `[3] Keep both as-is (save "<newRule>" on "<targetProfile>", leaving "<otherProfile>" active for this folder)`.
        *   `[c] Cancel`.
*   **Stage C — Narrower Overlapping Rules on Other Profiles:**
    *   Triggered when `newRule` is a remote prefix glob (`<prefix>/*`) and other profiles contain narrower remote rules on the same effective `remote_name` covered by `<prefix>/*` (excluding rules already handled in Stages A–B).
    *   In non-TTY mode: preserves the other profiles' narrower rules as overrides without error.
    *   In TTY mode: prompts `[1] Keep existing rules on other profiles as overrides for those repositories` (default), `[2] Remove overlapping rules (<list>) from other profiles so "<targetProfile>" covers all of <newRule.Remote>`, or `[c] Cancel`.
*   **Stage D — Same-Profile Redundancy Pruning:**
    *   When `newRule` is a remote prefix glob (`<prefix>/*`), removes any narrower remote rule on `targetProfile` (with the same effective `remote_name`) covered by `<prefix>/*`, except when Stage B option `[2]` explicitly added an exact repository rule alongside `newRule`.

### 5. Shared Buffered Input & Testable TTY Boundary (`cmd/aide/profile.go` & `internal/launcher/empty_state.go`)

*   All sequential prompts within a command execution (`handleEmptyState` -> `contextBindCmd` -> `runCreateWizard` -> scope prompt -> conflict prompts) share a single `*bufio.Reader` and an injectable TTY check.
*   Because Go's `bufio.NewReader(r)` returns `r` directly when `r` is already a `*bufio.Reader`, passing the active `*bufio.Reader` across command boundaries via `cmd.SetIn(reader)` (and threading the prompt handle from `contextBindCmd` into `runCreateWizard`) prevents buffered lines from being dropped in unit tests or piped multi-line input.

--------------------------------------------------------------------------------

## Alternatives Considered

### Alternative A: Dedicated `org:` Key in `config.MatchRule` (`config.yaml` Schema)

*   **Pros**: Makes organization matching a named YAML field (`match: [{org: github.com/acme}]`).
*   **Cons**: Duplicates [`scoreRemoteRule`](../../internal/context/resolver.go#L412-L436), which already evaluates `remote: github.com/acme/*` across SSH, HTTPS, and SCP git URLs; requires schema changes across [`internal/config/config.go`](../../internal/config/config.go) and [`cmd/aide/profile.go`](../../cmd/aide/profile.go); and still cannot represent nested GitLab teams/subgroups (`gitlab.com/acme/payments/*`) without adding more schema keys.
*   **Reason for Rejection**: Violates YAGNI. The existing `remote:` glob matcher already supports arbitrary prefix levels; the missing capability was strictly in CLI binding and conflict resolution.

### Alternative B: Mutually Exclusive Boolean Flags (`--repo`, `--org`, `--path`)

*   **Pros**: Slightly shorter flag names for the three fixed scopes (`--repo`, `--org`, `--path`).
*   **Cons**: Cannot express intermediate subgroup levels (such as `gitlab.com/acme/payments/*` for a repository at `gitlab.com/acme/payments/api`) in non-interactive scripts, and requires N-way mutual-exclusion validation across boolean flags.
*   **Reason for Rejection**: A single `--scope <value>` flag (`repo`, `org`, `path`, or a parent prefix) paired with dynamic shell tab-completion handles both the common shorthands and nested group hierarchies with one flag.

### Alternative C: Raising `specificityRemote` (`100`) Above `specificityPathGlob` (`200`) in `resolver.go`

*   **Pros**: Would make `remote: github.com/acme/*` automatically outscore a broad directory glob such as `path: ~/source/*` without prompting during `bind`.
*   **Cons**: Alters profile resolution precedence for existing `config.yaml` files that rely on directory globs (`path: ~/work/*`) taking priority over broad remote globs, and still cannot resolve shadowing by exact repository rules (`250`) or exact path rules (`300`) on other profiles.
*   **Reason for Rejection**: Pre-persist conflict analysis during `bind` catches all shadowing cases (path globs, exact remotes, and exact paths) without silently changing runtime resolution behavior for existing configurations.

--------------------------------------------------------------------------------

## Non-Functional Requirements

*   **Security & Privacy**: Remote URLs containing embedded credentials (`https://user:token@github.com/org/repo.git`) must be normalized via [`ParseRemoteHost`](../../internal/context/git.go#L81-L90) before generating scope options, displaying prompts, or writing `config.yaml` so tokens are never echoed to the terminal or persisted to disk.
*   **Performance & Latency**: Pre-persist conflict analysis runs in-memory over `cfg.Contexts` and reuses a single cached [`GitRemotes`](../../internal/context/git.go#L27-L40) lookup, invoking `git remote get-url` at most once per command execution.
*   **Rollout & Backward Compatibility**: Existing `config.yaml` files require no migration because `remote: <host>/<prefix>/*` is already supported by `Resolve`. On the CLI, replacing `--remote` and `--path` on `aide profile bind` with `--scope` is an intentional breaking flag change; non-TTY invocations without flags (`aide profile bind <name>`) preserve their existing behavior of binding the exact repository remote (or folder path outside a git repo).

--------------------------------------------------------------------------------

## Detailed Implementation Plan

### Phase 1: Context Resolver Exports & Unit Tests

#### 1. [`internal/context/resolver.go`](../../internal/context/resolver.go) (Modify)

*   **Type / Scope**: Go Package (`internal/context`)
*   **Rationale**: Expose rule scoring and remote pattern canonicalization so `cmd/aide` can evaluate candidate rules and compare remote patterns without duplicating logic.
*   **Proposed Changes**:
    *   Export `ScoreRule(rule *config.MatchRule, cwd string, remotes RemoteLookup) int` (delegating to `scoreRule`).
    *   Export `CanonicalizeRemotePattern(pattern string) string` (delegating to `canonicalizeRemotePattern`).

#### 2. [`internal/context/resolver_test.go`](../../internal/context/resolver_test.go) (Modify)

*   **Type / Scope**: Unit Test (`internal/context`)
*   **Rationale**: Verify the exported `ScoreRule` and `CanonicalizeRemotePattern` functions across all specificity tiers.
*   **Proposed Changes**:
    *   Add unit tests verifying `ScoreRule` ordering for exact path (`300`), exact remote (`250`), path glob (`200`), remote prefix glob (`100`), and non-matching rules (`0`), and `CanonicalizeRemotePattern` normalization for URL, SCP, exact, and glob patterns.
*   **Phase 1 Verification Checkpoint**: `go test ./internal/context/...`

---

### Phase 2: Scope Generation, Pruning & Conflict Resolution Engine

#### 3. [`cmd/aide/match_rule.go`](../../cmd/aide/match_rule.go) (Modify)

*   **Type / Scope**: CLI Helper Module (`package main`)
*   **Rationale**: Implement candidate scope generation, `--scope` flag matching, interactive scope selection, remote coverage/pruning, and the pre-persist conflict resolution workflow.
*   **Proposed Changes**:
    *   Define `bindScopeOption`:
        *   `Rule config.MatchRule`
        *   `Label string`
        *   `Desc string` (`fmt.Sprintf("by remote %s", ...)` for remote rules; `fmt.Sprintf("by path %s", cwd)` for path rules)
        *   `Shorthand string` (`"repo"`, `"org"`, `"<org>/<subteam>"`, or `"path"`)
    *   Implement `buildBindScopeOptions(cwd, remote string) []bindScopeOption`:
        *   When `remote == ""`, return `[]bindScopeOption{{Rule: config.MatchRule{Path: cwd}, Label: fmt.Sprintf("This folder path only (%s)", cwd), Desc: fmt.Sprintf("by path %s", cwd), Shorthand: "path"}}`.
        *   When `remote != ""`, split `remote` on `/` into `segments`:
            *   Option `[1]`: `{Rule: config.MatchRule{Remote: remote}, Label: fmt.Sprintf("This repository only (%s)", remote), Desc: fmt.Sprintf("by remote %s", remote), Shorthand: "repo"}`.
            *   For `i := len(segments) - 1; i >= 2; i--`: let `prefix = strings.Join(segments[:i], "/") + "/*"`, `shorthand = "org"` when `i == 2` else `strings.Join(segments[1:i], "/")`, and append `{Rule: config.MatchRule{Remote: prefix}, Label: fmt.Sprintf("All repositories in %s", prefix), Desc: fmt.Sprintf("by remote %s", prefix), Shorthand: shorthand}`.
            *   Final option: `{Rule: config.MatchRule{Path: cwd}, Label: fmt.Sprintf("This folder path only (%s)", cwd), Desc: fmt.Sprintf("by path %s", cwd), Shorthand: "path"}`.
    *   Implement `resolveScopeFlag(flagVal, cwd, remote string, opts []bindScopeOption) (config.MatchRule, string, error)`:
        *   Trim whitespace and normalize `flagVal`. Match against each `opt` in `opts` where `flagVal == opt.Shorthand` or, for remote options (`opt.Rule.Remote != ""`), `aidectx.CanonicalizeRemotePattern(flagVal)` matches `opt.Rule.Remote` or `strings.TrimSuffix(opt.Rule.Remote, "/*")`.
        *   If unmatched, return an error listing the valid `--scope` values from `opts` (and explaining if `"repo"` or `"org"` is unavailable because `cwd` has no git remote or no organization segment).
    *   Implement `selectBindScope(cwd, scopeFlag string, pio *promptIO) (config.MatchRule, string, error)`:
        *   Compute `remote := aidectx.CurrentRemote(cwd)` and `opts := buildBindScopeOptions(cwd, remote)`.
        *   If `scopeFlag != ""`, return `resolveScopeFlag(scopeFlag, cwd, remote, opts)`.
        *   If `!pio.tty || len(opts) == 1`, return `opts[0].Rule, opts[0].Desc, nil`.
        *   Otherwise print the numbered menu (`[1]..[len(opts)]`, `Choose [1]: `) to `pio.out`, read from `pio.reader` (defaulting empty input to `1`), and return the chosen option's `Rule` and `Desc`.
    *   Update `effectiveRemoteName(r config.MatchRule) string` (returning `aidectx.DefaultRemoteName` when `r.RemoteName == ""` else `r.RemoteName`) and `remoteIdentity(remote string) string` (delegating to `aidectx.CanonicalizeRemotePattern(remote)`):
        *   Update `rulesCollide(a, b config.MatchRule) bool` to compare `effectiveRemoteName(a) == effectiveRemoteName(b)` and `remoteIdentity(a.Remote) == remoteIdentity(b.Remote)` when both are remote rules, and `a.Path != "" && a.Path == b.Path` when both are path rules.
    *   Implement `remoteRuleCovers(broad, narrow config.MatchRule) bool` and `pruneSubsumedRemoteRules(rules []config.MatchRule, broad config.MatchRule) ([]config.MatchRule, []config.MatchRule)`:
        *   `remoteRuleCovers(broad, narrow)` returns `true` iff `broad.Remote != ""`, `narrow.Remote != ""`, `effectiveRemoteName(broad) == effectiveRemoteName(narrow)`, `canonBroad := remoteIdentity(broad.Remote)` ends with `"/*"`, `canonNarrow := remoteIdentity(narrow.Remote)` is not equal to `canonBroad`, and `strings.HasPrefix(canonNarrow, strings.TrimSuffix(canonBroad, "/*")+"/")`.
    *   Implement `resolveAndApplyBind(cfg *config.Config, targetProfile string, newRule config.MatchRule, cwd string, remotes aidectx.RemoteLookup, pio *promptIO) (applied bool, finalDesc string, err error)`:
        *   Returns `applied = false, err = nil` when `targetProfile` already covers `newRule` and no other profile shadows `targetProfile` at `cwd` (caller prints `This folder is already bound to profile "<targetProfile>".` and skips `WriteConfig`).
        *   Returns `applied = false, err = fmt.Errorf("aborted")` when the user selects `[c]` at any conflict prompt.
        *   Executes the same-profile pre-check and Stages A–D:
            *   **Stage A (Exact Collision)**: If `findExistingBind(cfg, targetProfile, newRule)` finds `existing`, error in non-TTY mode (`folder already bound to profile %q; re-run interactively to resolve`); in TTY mode prompt `[1] Move rule from "<other>" to "<target>"` (default on Enter) or `[c] Cancel`.
            *   **Stage B (Shadowing at `cwd`)**: Collect all rules `(otherProfile, r, score)` across `cfg.Contexts` (where `otherProfile != targetProfile`) that beat `(newScore, targetProfile)` at `cwd` under `bestMatch`. If non-empty:
                *   In non-TTY mode: return an error naming the highest-scoring shadowing profile and rule.
                *   In TTY mode: check if an override candidate (`{Remote: CurrentRemote(cwd)}` at tier `250`, else `{Path: cwd}` at tier `300`) is eligible (does not collide with another profile's rule and `(overrideScore, targetProfile)` beats the highest shadowing `(conflictScore, otherProfile)`). Prompt with `[1] Remove shadowing rule(s) ...` (default on Enter), `[2] Also bind <overrideRule> ...` (if eligible), `[3] Keep both as-is ...`, `[c] Cancel` (renumbering `[2] Keep both as-is` when no override candidate is eligible).
            *   **Stage C (Narrower Overlaps on Other Profiles)**: When `newRule` is a remote prefix glob (`/*`), collect narrower remote rules on other profiles where `remoteRuleCovers(newRule, r)` is true. If non-empty and `pio.tty`, prompt `[1] Keep existing rules on other profiles as overrides` (default on Enter), `[2] Remove overlapping rules from other profiles`, or `[c] Cancel`. In non-TTY mode, default to `[1]` without error.
            *   **Stage D (Same-Profile Pruning & Append)**: Prune narrower remote rules on `targetProfile` covered by `newRule` via `pruneSubsumedRemoteRules` (preserving any exact repo override added by Stage B `[2]`), append `newRule` (if not already present) and any Stage B override rule to `cfg.Contexts[targetProfile]`, and return `applied = true`.

#### 4. [`cmd/aide/match_rule_test.go`](../../cmd/aide/match_rule_test.go) (Modify)

*   **Type / Scope**: Unit Test (`package main`)
*   **Rationale**: Unit-test scope option generation, `--scope` resolution, `RemoteName`-aware collision/coverage, and same-profile pruning.
*   **Proposed Changes**:
    *   Add tests for `buildBindScopeOptions` (non-git folder, 2-segment `host/repo`, 3-segment `github.com/org/repo`, 4+ segment `gitlab.com/org/team/sub/repo`, and host-only guard).
    *   Add tests for `resolveScopeFlag` (`repo`, `org`, `path`, full prefix `gitlab.com/org/team/*`, without wildcard `gitlab.com/org/team`, relative shorthand `org/team`, and invalid scopes).
    *   Add tests for `rulesCollide` with differing `RemoteName`, `remoteRuleCovers`, and `pruneSubsumedRemoteRules`.
*   **Phase 2 Verification Checkpoint**: `go test ./cmd/aide/... -run "TestAutoDetect|TestRulesCollide|TestFindExisting|TestRemoveMatch|TestBuildBindScope|TestResolveScope|TestRemoteRuleCovers|TestPruneSubsumed"`

---

### Phase 3: CLI Integration, Shared `promptIO` & Command Tests

#### 5. [`cmd/aide/profile.go`](../../cmd/aide/profile.go) (Modify)

*   **Type / Scope**: CLI Commands (`package main`)
*   **Rationale**: Wire `--scope`, dynamic tab completion, shared `promptIO`, and `resolveAndApplyBind` into `aide profile bind` and `aide profile create`.
*   **Proposed Changes**:
    *   Define `type promptIO struct { reader *bufio.Reader; out io.Writer; tty bool }`, helper `newPromptIO(cmd *cobra.Command) *promptIO`, and package-level `var isInputTTY = func(cmd *cobra.Command) bool` (checking `term.IsTerminal(int(os.Stdin.Fd()))` when `cmd.InOrStdin() == os.Stdin`, overridable in tests). `newPromptIO` wraps `cmd.InOrStdin()` with `bufio.NewReader(cmd.InOrStdin())` (which reuses an existing `*bufio.Reader` if one was set via `cmd.SetIn`).
    *   Update `pickExistingContext(pio *promptIO, cfg *config.Config)` to read from `pio.reader` and check `pio.tty`.
    *   Update `createOptions` to add `scope string` and `pio *promptIO`.
    *   In [`contextBindCmd`](../../cmd/aide/profile.go#L174-L284):
        *   Replace `--path` and `--remote` flags with `--scope <value>` and register shell completion via `RegisterFlagCompletionFunc("scope", ...)`.
        *   Construct `pio := newPromptIO(cmd)`. When the named profile does not exist and the user answers `y` to create it, call `runCreateWizard(cmd, name, createOptions{here: tristateYes, scope: scopeFlag, pio: pio})`.
        *   Call `selectBindScope(cwd, scopeFlag, pio)` and `resolveAndApplyBind(cfg, name, rule, cwd, aidectx.GitRemotes(cwd), pio)`. Write `config.yaml` and print `Bound this folder to profile %q (matched %s)` only when `applied == true`.
    *   In [`runCreateWizard`](../../cmd/aide/profile.go#L399-L514):
        *   Reuse `opts.pio` when non-nil, else construct `newPromptIO(cmd)`.
        *   Insert `cfg.Contexts[name] = newCtx` before binding. When `bindHere` is true, run `selectBindScope(cwd, opts.scope, pio)` and `resolveAndApplyBind(cfg, name, rule, cwd, aidectx.GitRemotes(cwd), pio)`. If either returns an error (such as `[c] Cancel`), return the error immediately without calling `isolateNewProfile` or `config.WriteConfig`.
        *   After `resolveAndApplyBind` succeeds, call `isolateNewProfile(out, cfg, name, &newCtx)` (updating `cfg.Contexts[name]` with the isolated `Profile` dir settings while preserving the bound `Match` slice) and `config.WriteConfig(cfg)`.

#### 6. [`cmd/aide/empty_state_actions.go`](../../cmd/aide/empty_state_actions.go) & [`internal/launcher/empty_state.go`](../../internal/launcher/empty_state.go) (Modify)

*   **Type / Scope**: Empty-State Prompt & Adapter
*   **Rationale**: Preserve buffered input bytes when `handleEmptyState` delegates to `emptyStateAdapter.Bind` or `Create`.
*   **Proposed Changes**:
    *   In [`internal/launcher/empty_state.go`](../../internal/launcher/empty_state.go#L69), pass `reader` (`*bufio.Reader`) or expose `SetIn` on `a.cmd` in [`cmd/aide/empty_state_actions.go`](../../cmd/aide/empty_state_actions.go#L21-L35) by calling `bind.SetIn(a.cmd.InOrStdin())` in `emptyStateAdapter.Bind`.

#### 7. [`cmd/aide/context_bind_test.go`](../../cmd/aide/context_bind_test.go) & [`cmd/aide/context_create_test.go`](../../cmd/aide/context_create_test.go) (Modify)

*   **Type / Scope**: Integration / Command Tests (`package main`)
*   **Rationale**: Verify `--scope` flags, interactive scope selection, and all conflict resolution flows across `bind` and `create`.
*   **Proposed Changes**:
    *   In `context_bind_test.go`:
        *   Update existing `--path` and `--remote` tests to `--scope path`, `--scope repo`, `--scope org`, and subgroup prefixes.
        *   Add interactive TTY tests (overriding `isInputTTY` and passing multi-line input via `cmd.SetIn`) covering:
            *   Selecting `[2]` org scope (`github.com/acme/*`) and pruning an existing `github.com/acme/repo1` rule on the same profile.
            *   Stage A exact collision resolution (`[1]` move rule from `other` to `target`, and `[c]` cancel).
            *   Stage B shadowing conflict resolution (`path: <dir>/*` on `personal` vs `remote: github.com/acme/*` on `work`) via `[1]` remove shadowing rule from `personal`, `[2]` also bind `remote: github.com/acme/repo` on `work`, and `[3]` keep both.
            *   Stage C narrower overlapping rule resolution (`remote: github.com/acme/other` on `personal` when binding `remote: github.com/acme/*` on `work`) via `[1]` keep as override and `[2]` remove from `personal`.
    *   In `context_create_test.go`:
        *   Add an interactive TTY test verifying `aide profile create` in a git repo prompts for match scope and binds the chosen organization prefix.
*   **Phase 3 Verification Checkpoint**: `go test ./cmd/aide/... ./internal/launcher/...`

---

### Phase 4: User Documentation Updates

#### 8. [`docs/cli-reference.md`](../cli-reference.md), [`docs/profiles.md`](../profiles.md), and [`docs/getting-started.md`](../getting-started.md) (Modify)

*   **Type / Scope**: User Documentation
*   **Rationale**: Update CLI flag tables and examples from `--path`/`--remote` to `--scope` and document the interactive scope and conflict resolution prompts.
*   **Proposed Changes**:
    *   Replace `--path` and `--remote` in `aide profile bind` documentation with `--scope repo`, `--scope org`, `--scope <prefix>`, and `--scope path`, and show an example of the interactive scope and conflict resolution prompts.
*   **Phase 4 Verification Checkpoint**: `go test ./... && golangci-lint run`

### Verification Plan

*   **Compilation/Build**: `go build ./cmd/aide`
*   **Unit Tests**: `go test ./internal/context/... ./cmd/aide/...`
*   **Full Test Suite**: `go test ./...`
*   **Static Analysis & Linting**: `golangci-lint run`

--------------------------------------------------------------------------------

<!-- Provenance: Indicates initial structural drafting via EGM. -->

*Drafted with EGM*
