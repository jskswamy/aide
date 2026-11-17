# Refactoring, Dead Code, DRY, and Go Idioms Skill

Use this skill on every Go code change (`cmd/**`, `internal/**`, `pkg/**`) to detect dead code, missed helper reuse, structural duplication, and Go anti-patterns. Report actionable findings under `### Should fix` (or `### Nice to have` for minor simplifications) in the Ponytail output format, and include line savings in `Lean: -<N> lines possible.`

---

## 1. Dead and Orphaned Code Detection

When a PR modifies, replaces, or deletes functions, types, or branches, trace their former callees and types across the package:
- Flag unexported functions, methods, struct fields, constants, or error variables that lost their last production caller in this diff.
- Flag exported functions or types in `internal/` packages that have zero callers across the repository after this diff.
- Flag commented-out code blocks or unreachable branches (`if` conditions that are tautologically false given prior checks).

---

## 2. Existing Helper Reuse (Stop Reimplementation)

Before accepting new utility code in a PR, check whether `aide` already provides the helper in its shared `internal/` packages or the target package's own `helpers.go`:

| Shared Package | Capabilities Provided (Reuse Instead of Reimplementing) |
|---|---|
| `internal/fsutil` | Atomic file writes, directory creation, file copy/existence checks, safe path operations |
| `internal/homepath` | Home directory resolution, tilde (`~`) expansion, `$HOME`-relative path normalization |
| `internal/hashutil` | Content-addressed SHA-256 hashing for files and byte slices |
| `internal/sliceutil` | Slice deduplication, filtering, containment, and set operations |
| `internal/yamlnode` | `gopkg.in/yaml.v3` AST node lookup, insertion, mutation, and comment-preserving edits |
| `internal/redact` | Secret and token redaction in strings, maps, and diagnostic output |
| `internal/envscrub` | Environment variable filtering, `never_allow_env` scrubbing, and key=value slice manipulation |
| `internal/statedir` | XDG state, config, and runtime directory path resolution |

If new code inlines or reimplements logic covered by these packages (or by Go standard library functions such as `slices.*`, `maps.*`, `filepath.*`, `strings.*`, `errors.Join`), flag it under `### Should fix` and name the exact function to call.

---

## 3. DRY Violations and Structural Smells

Evaluate the diff across files and packages for these specific structural smells:

1. **Logic Duplication (DRY):** Identical or semantically equivalent blocks (>= 5 lines) appearing in multiple places in the diff or copied from a sibling file in the same package.
2. **Knowledge / Policy Duplication (Split Brain):** The same domain rule, path list, capability constraint, or validation check encoded independently in two packages (for example, duplicating sandbox path resolution in both `cmd/aide/` and `internal/sandbox/`, or duplicating gateway auth rules in both `internal/config/` and `internal/gateway/`).
3. **Structural Duplication:** Declaring a new struct or DTO that duplicates fields and purpose of an existing type in `internal/config/`, `internal/harness/`, or `internal/provision/`.
4. **Peer Convention Divergence:** When adding a new provider, guard, capability, harness, or CLI subcommand to a directory with 2+ existing peers, check constructor signatures, error wrapping style, registration order (sorted vs insertion order), and test layout (table-driven tests). Unjustified divergence from sibling files is a `Should fix` finding.
5. **Feature Envy & Inappropriate Intimacy:** A function in package A that mostly inspects or mutates fields of package B's types instead of living as a method in package B.
6. **Data Clumps:** The same 3+ parameters passed together across multiple function signatures instead of using an existing context/options struct.
7. **Repeated Type/Kind Switches:** Adding a new `switch x.Type` or `switch x.Kind` chain when 2+ other functions in the package already switch on the same discriminant (consolidate into a method or registry lookup).

---

## 4. Go Idiom and Reliability Checks

Flag only patterns with correctness, error-handling, or maintenance impact:
- **Broken error chains:** Using `fmt.Errorf("...: %v", err)` or `err.Error()` instead of `%w`, which breaks `errors.Is` and `errors.As` for callers.
- **Verbose or inconsistent error prefixes:** `aide` uses terse Go error prefixes (`"load config: %w"`, not `"failed to load the configuration file: %w"`).
- **Unmanaged goroutines:** Starting a `go func()` without a `context.Context` cancellation path, `sync.WaitGroup`, or deterministic shutdown channel.
- ** Premature interfaces (YAGNI):** Defining a new interface in the producer package with only a single implementation and no external mock requirement. In Go, accept small interfaces at the consumer boundary only when multiple implementations or test doubles are needed.
- **Raw strings/ints for domain enums:** Using raw string literals across files where the package defines a typed constant.
