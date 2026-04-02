# aide Review Rules

This project is a security-critical CLI tool that sandboxes AI coding agents
using OS-native mechanisms (macOS Seatbelt, planned Linux Landlock). Every
change is evaluated through three lenses: correctness, architecture, and
security posture.

## Review Philosophy

aide's core principle: enable full agent autonomy without babysitting. The
sandbox is enablement, not restriction. Changes that weaken the sandbox
without explicit user consent are the highest-severity finding.

Two-stage review pipeline: Greptile runs first, human review follows. If a
PR cannot pass automated review, it never reaches a human. Be thorough but
precise — false positives waste maintainer time.

---

## Gate 1: Code Quality

### Go Standards
- All code must be gofmt-formatted
- Zero lint warnings (errcheck, govet, staticcheck, unused, misspell, revive, gocritic, exhaustive, nolintlint)
- No `//nolint` without a specific linter name and explanation
- Error returns must be checked (except fmt.Fprint* family)

### Testing
- All new public functions require unit tests
- All bug fixes require a regression test
- Tests must use `t.Helper()` for test helpers
- No `time.Sleep` in tests — use channels, tickers, or test clocks
- Integration tests tagged with `//go:build integration`

### Code Coverage
- PRs must not decrease overall test coverage
- New packages must have at least 70% line coverage
- Security-critical packages (`pkg/seatbelt/`, `internal/sandbox/`, `internal/secrets/`) must maintain at least 80% coverage
- Flag any PR that adds code to security-critical packages without corresponding test coverage

### Documentation
- No AI-generated marketing prose (seamlessly, robust, leverage, cutting-edge, etc.)
- PR descriptions must state what changed and why
- AI usage must be disclosed per AI_POLICY.md

### Commit Hygiene
- Atomic commits: one logical change per commit
- Imperative mood subject line, 50 char limit
- No AI co-author attributions in commit messages
- No merge conflict markers in committed code

---

## Gate 2: Architectural Integrity

### CRITICAL — Flag any PR that changes these architectural boundaries:

**Trust boundaries:**
- Changes to how sandbox profiles are generated (`pkg/seatbelt/`, `internal/sandbox/`)
- Changes to capability resolution order (base defaults + capability effects + explicit sandbox config + never_allow)
- Changes to guard activation logic (always/default/opt-in guard types)
- Changes to the module interface (`Name()`, `Rules()`, `Type()`, `Description()`)

**Data flow boundaries:**
- Changes to how secrets are decrypted or passed to agents (`internal/secrets/`)
- Changes to context resolution logic (`internal/context/`)
- Changes to how environment variables cross the sandbox boundary
- Changes to the launcher exec path (`internal/launcher/`)

**Interface contracts:**
- New or modified public API in `pkg/seatbelt/` (this is a public library, breaking changes affect external consumers)
- Changes to CLI command signatures or flag semantics (`cmd/aide/`)
- Changes to config file schema (`internal/config/`)
- Changes to `.aide.yaml` project config format

### Structural changes that require architectural review:

- Any new package under `internal/` or `pkg/`
- Moving code between `internal/` and `pkg/` (changes visibility boundary)
- Adding new dependencies to `go.mod` (especially security-sensitive ones: crypto, exec, net, os)
- Changes to the Makefile build pipeline or CI workflows
- Changes to guard count (adding/removing/renaming guards)
- Changes to capability definitions or the capability inheritance model (`extends`, `combines`)
- Any file that grows beyond 500 lines (signal it may need splitting)

### Cross-cutting concerns:

- Changes touching 5+ packages in a single PR — flag for decomposition review
- Changes to error handling patterns (especially in sandbox/secrets paths where errors affect security posture)
- Any new `os.Exec`, `exec.Command`, or subprocess spawning outside `internal/launcher/`

---

## Gate 3: Threat Model — Security Posture Scoring

Every PR is evaluated for its impact on aide's attack surface. The agent must
never gain access to resources without explicit user consent.

### Scoring framework

Rate each PR on a 0-5 threat scale:

| Score | Label | Meaning | Action |
|-------|-------|---------|--------|
| 0 | None | No security-relevant changes | Auto-pass |
| 1 | Minimal | Cosmetic changes to security code, no behavioral change | Note in review |
| 2 | Low | New allow rules scoped to a specific capability | Review capability scope |
| 3 | Medium | Changes to guard logic, new environment variable passthrough, new file access patterns | Requires maintainer review |
| 4 | High | Changes to sandbox profile generation, secret handling, trust boundaries, or exec paths | Requires maintainer + security review |
| 5 | Critical | Weakens deny-default posture, bypasses never_allow, adds mid-session escalation, or exposes secrets | Block merge, escalate immediately |

### Specific threat patterns to detect:

**Sandbox escape vectors (score 4-5):**
- Adding `(allow default)` anywhere in seatbelt profiles — this breaks the deny-default architecture
- Removing or weakening deny rules without adding equivalent protection
- Any path that allows the agent to modify its own sandbox profile
- Changes that allow MCP servers to bypass sandbox restrictions
- New `process-exec` allows outside the base guard's allowlist
- Any code path where the agent can influence which binary gets exec'd

**Credential exposure (score 3-5):**
- New environment variables passed through the sandbox boundary
- Changes to never_allow or never_allow_env logic
- Secrets decrypted to disk (even temporarily) rather than in-process
- New file read permissions for paths that commonly contain credentials (~/.ssh/, ~/.aws/, ~/.config/gcloud/, etc.)
- Agent gaining read access to aide's own config or secrets directory

**Privilege escalation (score 3-5):**
- Any mechanism for mid-session capability changes (aide's design forbids this)
- Changes to the trust model for `.aide.yaml` project configs
- Auto-enabling capabilities without user confirmation (aide suggests, never auto-enables)
- Changes to guard type from "always" to "default" or "opt-in" (weakens baseline)

**Information leakage (score 2-4):**
- Agent gaining access to git history of aide's config repo
- Logging or telemetry that could capture secrets or sandbox policy details
- Error messages that reveal sandbox policy structure to the agent
- New network allow rules that let the agent reach arbitrary hosts

### Present the threat assessment as:

```
Threat Score: X/5 (Label)
Attack Surface Change: [increased/unchanged/decreased]
Vectors Identified:
- [specific finding with file:line reference]
Recommendation: [pass/flag for maintainer/block]
```

---

## Gate 4: AI Slop Detection

AI-assisted contributions are welcome. Unreviewed AI dumps are not. This gate
detects code and prose that was generated by AI and submitted without human
review or editing.

### Code slop signals (flag when 3+ appear in the same PR):

**Structural tells:**
- Defensive programming where the codebase doesn't use it (nil checks on values that can never be nil, error wrapping on infallible operations)
- Unnecessary abstractions: interfaces with a single implementation, factory functions for types constructed once, options patterns on unexported types
- Generic variable names (`result`, `data`, `item`, `temp`, `val`) where the codebase uses domain-specific names
- Commented-out code with explanations like "// removed for now" or "// TODO: add back later"
- Functions that do one thing but are split into multiple helper functions for no reason

**Style tells (inconsistent with aide's codebase):**
- Verbose Go doc comments on unexported functions (aide uses comments only where logic is not self-evident)
- Error messages that read like English sentences ("failed to process the configuration file") where the codebase uses terse Go-style errors ("process config: %w")
- Unnecessary type assertions or type conversions that Go's type system already handles
- `else` blocks after `if err != nil { return }` (Go convention: early return, no else)
- Declaring variables far from their use, or declaring at function top rather than at point of need

**Copy-paste tells:**
- Code that duplicates existing utility functions in the codebase rather than calling them
- Reimplementing standard library functionality (custom string splitting, path joining, error types)
- Import paths that don't exist in the project's dependency tree (hallucinated packages)
- Function signatures that don't match the patterns used elsewhere in the same package
- Test code that uses `assert` libraries when the project uses standard `testing` package

**Prose tells (in comments, docs, PR descriptions):**
- Banned words: seamlessly, robust, leverage, utilize, comprehensive, cutting-edge, innovative, game-changing, world-class, best-in-class, next-generation, harness, unlock, empower, elevate, streamline, delve, unpack
- Banned patterns: "It's worth noting", "Let me explain", "In today's world", "As we know", "Ever wondered", "What if you could", "One might say", "It could be argued", "It's important to note", "as you can see", "keep in mind"
- Em dashes in markdown files
- Hedge phrases: "might want to consider", "it could be beneficial", "one approach would be"
- Excessive qualification: "It should be noted that...", "Generally speaking..."

### What to flag vs block:

| Signal count | Action |
|-------------|--------|
| 1-2 signals | Note in review, ask contributor to confirm they reviewed the code |
| 3-4 signals | Flag PR, request specific explanation of design choices |
| 5+ signals | Block PR, cite AI_POLICY.md, ask contributor to rewrite in their own voice |

### What NOT to flag:
- Clean, idiomatic code that happens to be AI-assisted (AI is welcome per AI_POLICY.md)
- Disclosed AI usage with evidence of human review and editing
- Boilerplate code (test setup, config parsing) where AI output matches project patterns

---

## Gate 5: Design Decision Compliance

aide has 32 documented design decisions (DD-1 through DD-32) in DESIGN.md.
PRs that violate these decisions must be blocked unless the PR explicitly
proposes changing the decision with rationale.

### Sandbox design decisions (violations are score 4-5 threats):

- **DD-14: Sandbox on by default.** Any PR that makes sandboxing opt-in instead of opt-out violates this. Users customize or opt out, never opt in.
- **DD-15: Guards-based default policy.** 7 always guards + 3 default guards. Any PR that changes guard types (e.g., making an always guard into a default guard) weakens the baseline.
- **DD-22: Agent config dir resolver.** New agents need a resolver function. Hardcoding agent config paths violates this.
- **DD-23: Path validation at launch.** Non-existent literal paths skip with warning, not hard failure. Glob patterns pass without validation.
- **DD-26: Deny-wins semantics.** All guard design MUST use allow-broad + deny-narrow. Any PR using deny-broad + allow-narrow is architecturally broken on macOS Seatbelt. Block immediately.
- **DD-27: (deny default) baseline.** The profile MUST start with `(deny default)`. Any `(allow default)` in seatbelt profiles breaks Claude Code. Block immediately.
- **DD-28: Everything is a guard.** All sandbox access decisions go through guards. No separate rule systems, no policy fields that bypass guards.
- **DD-29: Capabilities as abstraction.** Capabilities resolve to sandbox fields (writable, readable, denied, env_allow). They sit on top of guards, not beside them.
- **DD-30: never_allow hard ceiling.** `never_allow` paths append after ALL resolution. No capability can override. If a PR adds logic that checks capabilities before never_allow, block it.
- **DD-31: No mid-session escalation.** Seatbelt profile is immutable after launch. Any mechanism for runtime permission changes violates this. Block immediately.

### Secrets design decisions (violations are score 3-5 threats):

- **DD-4: Sops as Go library.** Decryption uses `sops/v3/decrypt` in-process. No shelling out to `sops` CLI at runtime.
- **DD-6: Single XDG directory.** Config and secrets in `$XDG_CONFIG_HOME/aide/`. No XDG_DATA_HOME split.
- **DD-10: Ephemeral runtime.** ALL decrypted material dies with the process. Generated configs go to `$XDG_RUNTIME_DIR/aide-<pid>/` (tmpfs, mode 0700). Signal handler cleanup for SIGTERM, SIGINT, SIGQUIT, SIGHUP. Any PR that writes decrypted secrets to persistent storage violates this.
- **DD-11: Optional secrets.** `secret` field is optional. Env values without `{{ }}` pass through as literals. No PR should make secrets mandatory.
- **DD-13: Age key discovery order.** YubiKey first, then `$SOPS_AGE_KEY`, then `$SOPS_AGE_KEY_FILE`, then default key file. Changing this order breaks the security hierarchy.
- **DD-17: Environment inheritance.** Default inherits ALL shell env vars. `--clean-env` flag for isolation. No PR should change the default to clean-env.

### Context & agent design decisions:

- **DD-5: Env on context, not agent.** Agents are just binary definitions. If a PR adds env vars, secrets, or MCP selection to agent definitions, it violates this.
- **DD-7: Zero-config passthrough.** Single agent on PATH with env vars set = transparent exec. aide must not require config for this case.
- **DD-12: Minimal config format.** Flat config (no `agents`/`contexts` keys) treated as single default context. No PR should break flat config parsing.
- **DD-16: Forward remaining CLI args.** Everything after aide's flags goes to the agent. No `--` separator required.
- **DD-18: Project root = git root.** Walk up from cwd to `.git/`. Falls back to cwd if not a git repo.

### Trust design decisions:

- **DD-32: Trust gate for .aide.yaml.** Untrusted by default. Content-addressed SHA-256. Deny is path-based (not content-based). Auto-re-trust only when aide itself modifies the file, guarded by pre-modification hash check. Any PR that auto-trusts project configs or uses content-based deny violates this.

### How to enforce:

1. When a PR touches code related to a design decision, cite the DD number and check compliance
2. If the PR violates a decision, block and quote the decision text
3. If the PR intentionally changes a decision, require that DESIGN.md is updated in the same PR with rationale — no silent design drift
4. Design decisions are not suggestions. They are load-bearing constraints discovered through testing and debugging. Treat violations as bugs, not style preferences.

---

## AI Policy Enforcement

Per AI_POLICY.md:
- Flag PRs with no AI disclosure that show signs of AI generation (verbose hedging prose, generic variable names, unnecessary abstractions)
- Flag PRs where review comments reveal the contributor cannot explain their own changes
- Do not flag for AI usage itself — AI is welcome. Flag for unreviewed AI output.

---

## Package-Specific Rules

### `pkg/seatbelt/` — Public Seatbelt Library
- This is a reusable library with external consumers. Treat as a public API.
- No dependencies on aide's internal packages
- Backward-compatible changes only (additions, not modifications to existing signatures)
- Deny rules must render after all allow rules (deferred deny rule ordering)
- Every new module must have tests that verify the generated Seatbelt profile text

### `internal/sandbox/` — Sandbox Orchestration
- Guard resolution must follow: always guards (cannot disable) + default guards (on by default) + opt-in guards
- Never auto-enable capabilities based on project detection — suggest only
- Capability resolution order is immutable: base + capabilities + explicit + never_allow

### `internal/secrets/` — Secret Management
- Secrets must never exist as plaintext on disk
- Decryption happens in-process via sops Go library only
- Ephemeral runtime files must have cleanup on exit
- No new secret storage backends without architectural review

### `internal/launcher/` — Agent Execution
- Single exec point for launching agents
- No subprocess spawning outside this package
- Environment must be fully resolved before exec
- Sandbox profile must be immutable before agent starts

### `internal/config/` — Configuration
- Config schema changes require migration path documentation
- New fields must have zero-value defaults that preserve current behavior
- `.aide.yaml` changes affect all users — flag for broad review

---

## Gate 6: Commit Quality Review

Review the PR's commit history for atomicity, grouping, and message
quality. Well-structured commits make review easier, bisect safer, and
revert cheaper.

### Commit atomicity

Each commit should represent one logical change. Flag commits that mix
unrelated concerns:

**Red flags:**
- A single commit touching files in 3+ unrelated packages without a
  clear cross-cutting reason (refactor, dependency update)
- Subject line requires "and" to describe two unrelated changes
  (e.g., "Fix auth bug and update dashboard layout")
- Mix of feature code and unrelated formatting/style changes
- Test files committed separately from the code they test

**Acceptable multi-package commits:**
- Dependency updates that ripple across packages
- Interface changes that require updating all implementations
- Rename/move refactors that touch many files mechanically

### Commit grouping

Evaluate whether the PR's commits tell a coherent story:

**Good grouping patterns:**
1. Tooling/dependency setup first, then implementation, then tests
2. Interface/type changes first, then implementations
3. One commit per logical concern, ordered by dependency

**Bad grouping patterns:**
- WIP commits ("wip", "fix", "more changes", "address review")
- Fixup commits that patch earlier commits in the same PR
- Interleaved concerns (feature A commit, feature B commit, feature A
  fix, feature B fix)

### Commit message quality

Check against the classic commit style (Chris Beams' 7 rules):

| Rule | Check |
|------|-------|
| Subject/body separated by blank line | Structural |
| Subject <= 50 characters | Count characters |
| Subject capitalized | First character uppercase |
| No trailing period on subject | Last character check |
| Imperative mood | "Add", "Fix", not "Added", "Fixes" |
| Body wrapped at 72 characters | Line length check |
| Body explains what and why, not how | Content check |

**Additional message checks:**
- No AI co-author attributions (Claude, Anthropic, GPT, OpenAI, Copilot)
- No internal tracker references (beads-*, task IDs, bd commands)
- No workflow artifacts (Phase 1, Step 2 of 4, dispatched to subagent)
- No acceptance criteria checklists copied from task trackers
- No verification command output pasted as proof

### Suggested commit regrouping

When commit quality issues are found, suggest how to regroup:

```
Commit Quality Issues:

1. Commit abc1234 "Fix bug and add feature" mixes two concerns
   Suggestion: Split into "Fix null check in auth" and
   "Add session timeout config"

2. Commits def5678 and ghi9012 are fixups of abc1234
   Suggestion: Squash into the original commit

3. Commit jkl3456 "wip" has no meaningful message
   Suggestion: Squash into the next logical commit or reword

Recommended commit structure:
  1. Fix null check in auth validation
  2. Add configurable session timeout
  3. Add tests for session timeout
```

### What NOT to flag:
- Large commits that are genuinely atomic (e.g., a single refactor
  touching many files mechanically)
- Merge commits from the project's standard merge strategy
- Commits with short subjects when the change is truly self-evident
  (e.g., "Fix typo in README")
