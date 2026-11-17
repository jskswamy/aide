# aide Code Review Orchestrator

`aide` is a security-critical CLI that sandboxes AI coding agents using OS-native isolation (macOS Seatbelt and Linux Landlock + seccomp + bubblewrap) and a local credential-injecting gateway proxy.

This file orchestrates four composable review skills located in `.greptile/skills/` (loaded via `.greptile/files.json`).

---

## 1. Review Priority Order

Evaluate every Pull Request using the priority order defined in `.greptile/skills/ponytail-review.md` (`Bug -> Risk -> Scale & Concurrency -> Missing Test -> Documentation Sync -> Speed -> Lean & Refactoring`).

Do **not** report formatting, linter (`golangci-lint`, `gosec`), commit-message length, or banned prose words; those are enforced deterministically by pre-commit hooks and GitHub Actions CI.

---

## 2. Composable Skills to Execute

Execute each applicable skill below and consolidate all findings into a single review response:

1. **`.greptile/skills/sandbox-threat-model.md`** (Mandatory when security-relevant paths are touched):
   - Evaluate the diff across all 5 trust boundaries (OS Sandbox, Credential-Injecting Gateway, Ephemeral Secrets & Env Scrubbing, Untrusted `.aide.yaml` Trust Gate & Provisioning, Subprocess Execution) and verify compliance with `DD-1` through `DD-33`.
   - Output the required `## Threat-Model Sweep` section (`Score: X/5`, `Attack surface change`, `Boundary Input Trace` table, and `Vectors` table).
2. **`.greptile/skills/ponytail-review.md`** (Mandatory on all PRs):
   - Trace callers and callees of changed functions.
   - Require a concrete failure scenario for every finding (*"Input/state X at `file:line` leads to wrong outcome Y"*). Respect `// shortcut:` comments that state their limit.
   - Format the entire review using the Ponytail output structure (`What this change does:`, `## Threat-Model Sweep` when triggered, numbered findings grouped under `### Must fix`, `### Should fix`, `### Nice to have` with **What this is / Problem / Fix / If we skip it**, `Verdict:`, `Lean: -<N> lines possible.`, and `Confidence: X/5`).
3. **`.greptile/skills/refactor-scan.md`** (Mandatory on all Go code diffs):
   - Check for dead/orphaned code left behind by the PR, missed helper reuse (`internal/fsutil`, `homepath`, `hashutil`, `sliceutil`, `yamlnode`, `redact`, `envscrub`, `statedir`), DRY violations (Logic, Knowledge/Split-Brain, Structural), peer convention divergence, and Go idiom defects (`%v` instead of `%w`, unmanaged goroutines).
4. **`.greptile/skills/doc-sync-audit.md`** (Mandatory when CLI/config/capabilities/sandbox/architecture or docs change):
   - Verify required documentation updates in `docs/`, `README.md`, and `DESIGN.md`, and audit modified docs for `MISSING`, `WRONG`, `STALE`, or `MISLEADING` claims against the code.

---

## 3. Confidence Score Coupling

The reported confidence score (`1-5`) is bounded by unresolved findings:

> `confidence = min(intrinsic_confidence, 5 - count_of_must_fix_findings - count_of_unmitigated_threat_vectors_at_3_or_above)`

- `5/5` is never reportable while any `Must fix` finding or Threat-Model score `>= 3/5` is open.
- When `confidence < 5`, show the arithmetic in the `Confidence:` footer line (starting value, each deduction with finding/vector citation, final score).
