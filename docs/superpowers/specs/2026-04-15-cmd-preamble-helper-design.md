# CLI Subcommand Preamble Helper Design

**Date:** 2026-04-15
**Status:** Proposed
**Beads issue:** AIDE-4ud
**Related:** `2026-04-15-detect-unification-design.md` (AIDE-c7o, now landed)

## Problem

`cmd/aide/commands.go` is 4800+ lines and contains 35 direct calls to
`config.Load(config.Dir(), cwd)` across most subcommands. Ten of
those sites additionally construct the capability registry via the
same four-line quartet:

```go
cwd, err := os.Getwd()
cfg, err := config.Load(config.Dir(), cwd)
userCaps := capability.FromConfigDefs(cfg.Capabilities)
registry := capability.MergedRegistry(userCaps)
```

This is shotgun-surgery territory: when registry construction
changes (e.g. adding a validation pass, switching to a lazy-loaded
registry, or wiring a new derived field), every cap command's RunE
needs an identical edit. The file's size compounds the problem —
reviewers have to scroll through thousands of lines to verify that
every call site applies the change identically.

## Goals

1. **Single preamble primitive.** One helper — `cmdEnv(cmd)` — that
   resolves cwd, loads config, and exposes the merged capability
   registry lazily.
2. **Uniform call-site shape across all 35 sites.** The helper
   returns `(env, err)`; callers apply their own error policy
   (strict, best-effort, defer-validate, check-only) by choosing
   what to do with `err`.
3. **File health.** Break `commands.go` into per-subject files so
   each one stays under ~500 lines and a single-topic change
   touches a single file.
4. **Regression-clean migration.** Existing tests are the
   regression gate — no test modifications. Every command still
   accepts the same flags and produces the same output.

## Non-goals

- **Typed errors for config.Load failures.** Tracked separately as
  AIDE-ep5; none of the current callers distinguish error kinds,
  so introducing classification now would be speculative.
- **Refactoring `internal/config`.** The preamble helper wraps
  `config.Load` post-hoc; no change to the config package.
- **Touching non-`cmd/aide` code.** The launcher, sandbox, and
  capability packages have their own loading paths that are outside
  this scope.
- **Fluent builder / options pattern.** Evaluated and rejected
  during brainstorming; a single function with lazy accessors is
  simpler and covers every caller need.

## Design

### Placement

The helper lives in a new file, `cmd/aide/cmdenv.go`. `commands.go`
is split into per-subject files to keep each under ~500 lines:

```
cmd/aide/
├── main.go              entry point (unchanged)
├── commands.go          root command + AddCommand wiring (~150 lines after split)
├── cmdenv.go            Env type + cmdEnv constructor + lazy accessors
├── cmdenv_test.go       unit tests for the helper
├── cap.go               cap + all cap subcommands
├── cap_test.go          consolidates cap_consent_test.go + cap_discovery_test.go
├── context.go           context commands
├── context_test.go
├── env.go               env commands
├── env_test.go
├── sandbox.go           sandbox commands
├── sandbox_test.go
├── secrets.go           secrets commands
├── secrets_test.go
├── agents.go            agents commands
├── agents_test.go
├── trust.go             trust/deny/untrust/validate
├── trust_test.go
├── status.go            init/setup/status/which/use
├── status_test.go
├── variant_flag_test.go     stays — tests parseVariantFlag (lives in main.go)
└── detect_integration_test.go  stays — cross-cutting integration
```

Naming follows Go stdlib convention: descriptive filenames without
a `cmd_` prefix. The package context (`cmd/aide`) already implies
the CLI scope.

### The helper

```go
// cmd/aide/cmdenv.go
package main

import (
    "os"

    "github.com/spf13/cobra"

    "github.com/jskswamy/aide/internal/capability"
    "github.com/jskswamy/aide/internal/config"
)

// Env captures the typical CLI subcommand preamble: cwd + loaded
// config, plus lazy access to the merged capability registry.
//
// Contract: after cmdEnv returns, Env.Config() is always non-nil.
// Any load failure is returned as err; callers choose their policy:
//
//   - strict:         if err != nil { return err }
//   - best-effort:    env, _ := cmdEnv(cmd)
//   - defer-validate: env, loadErr := cmdEnv(cmd); report loadErr later
//   - check-only:     _, err := cmdEnv(cmd); if err != nil { ... }
type Env struct {
    cmd      *cobra.Command
    cwd      string
    cfg      *config.Config
    registry capability.Registry
    regBuilt bool
}

// cmdEnv resolves the working directory and loads the aide config.
// On filesystem failure (e.g. os.Getwd errors) Env.Config() still
// returns a non-nil empty Config so callers can proceed safely.
func cmdEnv(cmd *cobra.Command) (*Env, error) {
    cwd, err := os.Getwd()
    if err != nil {
        return &Env{cmd: cmd, cfg: &config.Config{}}, err
    }
    cfg, loadErr := config.Load(config.Dir(), cwd)
    if cfg == nil {
        cfg = &config.Config{}
    }
    return &Env{cmd: cmd, cwd: cwd, cfg: cfg}, loadErr
}

// CWD returns the working directory captured at construction.
func (e *Env) CWD() string { return e.cwd }

// Config returns the loaded config. Never nil; on load failure it
// is an empty Config{} so best-effort callers can proceed.
func (e *Env) Config() *config.Config { return e.cfg }

// Registry returns the merged capability registry (built-ins plus
// user-defined capabilities). Built on first call and memoized;
// non-cap commands that never call Registry pay no construction
// cost.
func (e *Env) Registry() capability.Registry {
    if !e.regBuilt {
        userCaps := capability.FromConfigDefs(e.cfg.Capabilities)
        e.registry = capability.MergedRegistry(userCaps)
        e.regBuilt = true
    }
    return e.registry
}
```

`Env` is exported within the `cmd/aide` package (values flow
between files); `cmdEnv` (the constructor) is unexported and is
the only entry point. The stored `cmd *cobra.Command` is reserved
for future helpers (e.g. `env.Stdout()`, `env.Flag()`); no
consumer uses it yet.

### Call-site shapes

Four patterns cover all 35 sites. All share the same single
`cmdEnv(cmd)` call:

```go
// Strict (28 sites):
env, err := cmdEnv(cmd)
if err != nil {
    return err
}
// use env.CWD(), env.Config(), optionally env.Registry()

// Best-effort (4 sites):
env, _ := cmdEnv(cmd)
// env.Config() is always safe to access

// Defer-validate (2 sites):
env, loadErr := cmdEnv(cmd)
// proceed with env.Config(); report loadErr in output

// Check-only (1 site):
_, err := cmdEnv(cmd)
if err != nil {
    // report, don't proceed
}
```

### Why no fluent builder or load modes

The brainstorming session evaluated two richer shapes and rejected
both:

- **`newCmdEnv(cmd).Load()` split:** the split exists in classic
  builder patterns to allow configuration between construction
  and loading. Once `WithRegistry` was dropped, no configuration
  step remained, so `newCmdEnv(cmd)` became a pure pointer-capture
  that forced callers to write two lines for one operation.
- **Multiple load modes (`Load`, `LoadBestEffort`, `LoadDeferred`):**
  these encoded caller policy in the loader. The caller's policy
  is a caller concern; the loader just needs to return
  `(env, err)` with a guaranteed non-nil `Config()`. Policy-in-loader
  added API surface without removing any caller decision.

One function, one return shape, four caller behaviors.

### Registry laziness

`Registry()` memoizes on first call. Non-cap commands that never
call `Registry()` pay no construction cost. The alternative (eager
build during `cmdEnv`) is simpler internally but wastes
microseconds on the ~20 config-only commands. Both the `.WithRegistry()`
opt-in approach and eager construction were considered; laziness
is the lowest-ceremony middle ground.

The one guardrail worth documenting: if a caller attempts to use
`Registry()` on an `Env` whose `Config()` is the empty fallback,
the registry will contain only built-ins (no user-defined caps).
This is the correct behavior — an unloaded config has no user caps
to merge — and is covered by the unit tests.

## Threat Model

This refactor does not change the trust boundary or introduce any
new privileged surface.

**T1 — Behavioral drift during migration.** Risk: a call site
changes observable behavior due to error-handling mistranscription.
Mitigation: the existing test suite is the regression gate.
Per-file migration (Phase 3) gives atomic commits that can be
bisected.

**T2 — Memoization race.** Risk: two goroutines calling
`Registry()` race on `regBuilt`. Mitigation: every `Env` is
constructed per-command per-invocation and used from a single
goroutine. Cobra's `RunE` is not called concurrently for one
invocation. The `go test -race -count=3` sweep in the migration
acceptance confirms no race surfaces.

**T3 — Silent swallowing.** Risk: migrating a strict site to
best-effort by accident (writing `env, _ :=` where the original
had `if err { return err }`). Mitigation: the existing tests cover
happy paths; negative tests that specifically exercise load
failure should be added for any site where the original error path
produced observable output. In practice, reviewers comparing the
call-site diff to the original will catch this.

## Testing

### Unit tests (cmdenv_test.go)

- `TestCmdEnv_Success` — temp dir with valid config: `CWD()`
  matches, `Config()` returns the parsed config, err is nil.
- `TestCmdEnv_NoConfigDir` — fresh temp dir with no config files:
  `Config()` returns a non-nil empty `Config{}`; err is the load
  error.
- `TestCmdEnv_Registry_Lazy` — construct an Env; immediately call
  `Registry()`; verify it built the merged registry. Call
  `Registry()` a second time; verify the returned value is the
  same pointer / map (no rebuild).
- `TestCmdEnv_Registry_UserCaps` — config with a custom capability
  declaration; `Registry()` includes it alongside built-ins.
- `TestCmdEnv_WorkingDirectoryError` — simulate `os.Getwd` failure
  (documented as impractical on Darwin; skip the test on
  platforms where forcing the failure is not feasible, or inject
  via a test hook if one is added).

### Migration regression gate

The existing test suite is the primary gate. Per-file migration
commits (Phase 3) must not require any test modifications. Any
test that starts failing during migration indicates a call-site
drift; fix the migration, not the test.

### Acceptance sweeps

After Phase 3 completes, two grep commands verify the outcome:

```bash
# Cap commands are fully migrated:
grep -rn 'config.Load(config.Dir()' cmd/aide/cap.go
# Expected: zero

# Total direct calls collapse to a small idiosyncratic set:
grep -c 'config.Load(config.Dir()' cmd/aide/*.go
# Expected: 0 (or ≤2 with explicit "why inline" comments at each site)
```

## Migration

Three phases, 18 commits total:

### Phase 1 — Mechanical split (9 commits)

Move command groups out of `commands.go` into per-subject files.
No logic changes. Each commit:

1. Move cap commands into `cap.go`.
2. Move context commands into `context.go`.
3. Move env commands into `env.go`.
4. Move sandbox commands into `sandbox.go`.
5. Move secrets commands into `secrets.go`.
6. Move agents commands into `agents.go`.
7. Move trust/deny/untrust/validate into `trust.go`.
8. Move init/setup/status/which/use into `status.go`.
9. Consolidate cap tests into `cap_test.go` (merges
   `cap_consent_test.go` + `cap_discovery_test.go`).

After each commit: `go build ./...`, `go test ./... -race -count=1`,
both green. `commands.go` shrinks to ~150 lines.

### Phase 2 — Add the helper (1 commit)

Create `cmd/aide/cmdenv.go` (the code in this spec) and
`cmd/aide/cmdenv_test.go` (unit tests). No call-site migrations.
Tests pass.

### Phase 3 — Per-file call-site migration (8 commits)

One commit per file, in this order (largest-win first):

1. `cap.go` — ~10 sites (the biggest win: cap-family is the entire
   shotgun-surgery target).
2. `context.go` — ~5 sites.
3. `env.go` — ~4 sites.
4. `sandbox.go` — ~3 sites.
5. `secrets.go` — ~3 sites.
6. `agents.go` — ~3 sites.
7. `trust.go` — ~3 sites.
8. `status.go` — ~4 sites.

Each commit verified with `go test ./... -race -count=1`. Any test
failure rolls back the commit and fixes the migration before
reattempting.

### Post-migration

Run the acceptance sweeps (above) and close AIDE-4ud.

## Alternatives considered

**Two helpers (`loadCapEnv` + `loadCLIConfig`) plus inline
exceptions.** Evaluated first: two functions for the two broad
cases (cap-family vs config-only), with 2-3 idiosyncratic sites
staying inline. Rejected during brainstorming — the user correctly
noted that partial cleanups tend to stay partial, and a single
helper covering all 35 sites is easier to audit than two helpers
plus inline escape hatches.

**Fluent builder (`newCmdEnv(cmd).WithRegistry().Load()`).** Rich
builder pattern. Rejected — once `WithRegistry` is dropped
(laziness replaces it) and `Load()` is collapsed into
construction, nothing's left to chain. One function covers every
shape.

**Multiple load modes (`Load`, `LoadBestEffort`, `LoadDeferred`,
`LoadCheckOnly`).** Encoded caller policy in the loader. Rejected
— policy is a caller concern; the loader just returns `(env, err)`
with a guaranteed non-nil `Config()`.

**Options struct (`cmdEnv(cmd, WithRegistry(), Strict())`).**
Conceptually similar to the fluent builder. Rejected for the same
reason — no behavior toggles remained once laziness and single-mode
loading were adopted.

**Keep `commands.go` monolithic, only extract the helper.**
Rejected — the file is already 4800 lines and shotgun surgery is
compounded by sheer distance between call sites. Doing the split
separately would churn the same RunE bodies twice.

## Rollout

1. Phase 1 commits (mechanical split) — trivial review per commit;
   any reviewer can verify "no logic changed" via a content diff.
2. Phase 2 commit (helper + unit tests) — review the helper code
   and tests together.
3. Phase 3 commits (per-file migration) — each commit is a focused
   diff of a single command family; reviewers scan for
   error-handling drift (strict sites kept strict, best-effort
   kept best-effort).
4. Acceptance sweep.
5. Close AIDE-4ud. AIDE-ep5 (typed config errors) remains open as
   an independent follow-up.

## Open questions

- **Registry ordering determinism.** `MergedRegistry` returns a
  `map[string]Capability`; callers that iterate will see Go's
  map-iteration randomization. Existing code already handles this
  (callers sort explicitly when ordered output matters). No change
  required in this refactor.
- **Is `storeRef` or similar context worth caching on `Env`?**
  Some call sites additionally compute `remoteURL` or
  `aidectx.Resolve(...)` after the preamble. Those are per-command
  concerns and don't belong in the generic helper; keeping them
  inline at their call sites preserves locality.
