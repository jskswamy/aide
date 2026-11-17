# Ponytail Code Review Skill

Review a change like the senior developer who will be paged when it breaks.
Order of importance: **correct, safe, holds under load, tested, fast, lean**.
Lean still matters: every extra line must be read, tested, and fixed later.

---

## 1. Understand First

- Read the diff, then read the connected code it touches: callers of every changed function, the functions it calls, the tests, and the scoped documentation.
- Trace the real data flow: where input enters, how it is validated, what state is mutated or stored, and what leaves the package.
- A change can break code it does not touch. When a function signature, return value, error contract, or behavior changes, check every caller in the repository.
- Judge scale and concurrency against `aide`'s runtime: CLI execution spawning sandboxed agent subprocesses, concurrent local HTTP proxy requests through `internal/gateway/`, and concurrent filesystem/lock access across multiple running `aide` instances.

---

## 2. Look For (in Priority Order)

1. **Bug:**
   - Wrong result, panic/crash, nil pointer dereference, or missed edge case (empty slice/map, zero value, single/last item, symlink, missing file, non-UTF8 or whitespace paths).
   - Partial state mutation on error paths (mutating a struct, file, or YAML AST before returning an error midway through a function).
   - A caller broken by a changed contract, or a bug fix applied in only one caller while the shared function stays broken.
   - Platform divergence where `_darwin.go` and `_linux.go` behave differently on the same logical input.
2. **Risk:**
   - Security holes (unvalidated external inputs reaching sandbox rules or `exec.Command`, secret leakage in errors/logs/proxy traffic, bypassing `.aide.yaml` trust checks).
   - Data loss (swallowed write/close/rename errors, non-atomic file updates to user config or approval stores, missing cleanup of ephemeral runtime files).
3. **Scale & Concurrency:**
   - Check-then-write (TOCTOU) races on files, trust hashes, or state directories.
   - Goroutine leaks, unclosed HTTP response bodies or listeners in `internal/gateway/`, leaked file descriptors, or unguarded concurrent map/slice writes.
   - Unbounded memory growth or $O(n^2)$ work over large config/rule/usage sets.
4. **Missing Test:**
   - Risky new logic (a security branch, a rule generator, a parser, a state write, or a bug fix) with no test that fails when that logic breaks.
   - Require one good behavior/regression test rather than line-coverage padding. Flag any `time.Sleep` in tests (use channels, tickers, or test clocks).
5. **Speed:**
   - Repeated subprocess spawning, redundant disk/network I/O, or repeated YAML/JSON parsing inside a loop.
6. **Lean:**
   - Code that should not exist or should be smaller (see `.greptile/skills/refactor-scan.md` for detailed refactoring and helper-reuse checks):
     - **Delete:** dead code, unused options, speculative features.
     - **Reuse:** existing repository helpers instead of reimplementing.
     - **Stdlib / Native:** standard library or OS features instead of custom helpers or new dependencies for a few lines.
     - **YAGNI:** interfaces with a single implementation, pass-through wrappers, or config fields nobody sets.
     - **Merge:** near-copies that must change together.
     - **Split:** one function doing several unrelated jobs (split by job, never by line count).

---

## 3. Check Before You Report

- **Concrete case required:** Every finding must state a concrete trigger scenario: *"This input or state at `file:line` leads to this wrong result."* No concrete case, no finding.
- **Verify against the tree:** Re-read the surrounding lines to confirm the caller exists, the value can actually be nil/empty at that point, or the helper is genuinely unused.
- **Respect intentional shortcuts:** A comment marked `// shortcut: <the limit>, <when to upgrade>` (or `// ponytail:`) is a documented engineering decision, not a finding, unless expected usage already crosses the stated limit.
- **Smallest fix:** Propose the smallest change that solves the problem. Prefer fixes that delete code or call an existing helper over adding new layers, interfaces, or configuration.
- **Zero style taste:** Do not post cosmetic formatting comments, vague worries, or "consider doing X" suggestions.

---

## 4. Output Format

Use plain, direct English with short sentences.

Start the review with:
`What this change does:` in two or three sentences.

If security-relevant paths are touched, include the `## Threat-Model Sweep` section from `.greptile/skills/sandbox-threat-model.md`.

Then list findings in three groups (omit any group that has zero findings):
- **`### Must fix`**: bugs, security/sandbox threats, credential exposure, data loss, races, or resource leaks.
- **`### Should fix`**: risky code without a test, missing documentation updates (per `.greptile/skills/doc-sync-audit.md`), real duplication or missed helper reuse, design-decision or package-seam violations, functions mixing unrelated jobs, or dead/YAGNI code.
- **`### Nice to have`**: small simplifications or minor idiomatic cleanups.

Number findings continuously across all groups (`1, 2, 3...`) so the maintainer can reference them by number. Every finding must include all four parts (1-2 sentences each):

```markdown
1. **<Short plain-English title>** (`path/to/file.go:L40-52`)
   - **What this is:** <What this function or block does in the system.>
   - **Problem:** <The concrete input, error path, or duplication and what breaks.>
   - **Fix:** <The smallest code change that fixes it, naming any existing helper to call.>
   - **If we skip it:** <The concrete impact on users, security posture, or maintenance.>
```

End the review with:
- `Verdict: Ship.` or `Verdict: Fix <numbers> first.`
- `Lean: -<N> lines possible.` (include whenever lean/duplication/dead-code findings exist)
- `Confidence: X/5` (show deduction arithmetic when `< 5`)

If no issues are found: output `What this change does:`, the `## Threat-Model Sweep` (if triggered), `Looks good. Ship.`, and `Confidence: 5/5`.
