# Test Coverage Improvement: Security-Critical Packages + Mockgen Migration

**Date:** 2026-04-02
**Goal:** Raise security-critical packages to 80%+ coverage, migrate all hand-written mocks to mockgen, exclude untestable CLI package from coverage calculation.

## Problem

aide enforces a two-tier coverage policy:
- 80% minimum for security-critical packages (`pkg/seatbelt`, `internal/sandbox`, `internal/secrets`)
- 60% minimum overall

Current state fails both thresholds:

| Package | Coverage | Threshold | Status |
|---------|----------|-----------|--------|
| `internal/secrets` | 66.2% | 80% | Fails |
| `internal/trust` | 68.4% | 80% (proposed) | Fails |
| `internal/launcher` | 69.3% | 80% (proposed) | Fails |
| `cmd/aide` | 0% | -- | Drags total to 39.3% |
| **Total** | **39.3%** | 60% | Fails |

Two root causes:
1. Functions that shell out to external processes (`$EDITOR`, `syscall.Exec`) lack test doubles.
2. `cmd/aide/` (4,976 lines of Cobra wiring and interactive prompts) is untestable without major refactoring and drags down the total.

## Approach

### Strategy C: Security-first depth + full mockgen migration

Focus test investment on packages where bugs have security consequences. Use mockgen for all interfaces -- new and existing. Exclude `cmd/aide/` from CI coverage calculation.

### Why mockgen over hand-written mocks

Hand-written mocks drift from their interfaces. A method signature change compiles fine but breaks test behavior silently. mockgen generates mocks from the interface definition via `go generate`, so mocks stay in sync with interfaces automatically. One command (`make generate`) updates all mocks across the project.

## Tooling

### Dependencies

Add `go.uber.org/mock` to `go.mod`. This provides:
- `mockgen` binary for code generation
- `gomock` package for test assertions (`EXPECT()`, `Return()`, `Times()`, etc.)

### Convention

- `//go:generate mockgen` directive lives next to each interface definition
- Generated mocks go to `<package>/mocks/` subdirectory
- Mock files named `mock_<interface>.go`
- `make generate` runs `go generate ./...`

### Nix devshell

Add `mockgen` to the devshell tools list so `nix develop` provides it.

## Interface Inventory

### Existing interfaces (4)

| Interface | Location | Methods | Hand-written mock? |
|-----------|----------|---------|-------------------|
| `Execer` | `internal/launcher/launcher.go:23` | `Exec(binary, args, env)` | Yes -- `mockExecer` in test file |
| `Sandbox` | `internal/sandbox/sandbox.go:18` | `Apply(cmd, policy, runtimeDir)`, `GenerateProfile(policy)` | No |
| `Module` | `pkg/seatbelt/module.go:10` | `Name()`, `Rules()` | Yes -- `testModule` in test file |
| `Guard` | `pkg/seatbelt/module.go:18` | Extends `Module` + `Type()`, `Description()` | Shares `testModule` |

### New interfaces (1)

| Interface | Location | Methods | Purpose |
|-----------|----------|---------|---------|
| `EditorRunner` | `internal/secrets/editor.go` (new file) | `Run(editor, args, stdin, stdout, stderr)` | Abstract editor invocation in `Create()` and `Edit()` |

### Mockgen directives (5 files)

```
internal/launcher/launcher.go       → internal/launcher/mocks/mock_execer.go
internal/sandbox/sandbox.go         → internal/sandbox/mocks/mock_sandbox.go
internal/secrets/editor.go          → internal/secrets/mocks/mock_editor.go
pkg/seatbelt/module.go              → pkg/seatbelt/mocks/mock_module.go
pkg/seatbelt/module.go              → pkg/seatbelt/mocks/mock_guard.go
```

## Package Changes

### `internal/secrets/` (66.2% to 80%+)

**New interface:** `EditorRunner` in `editor.go`.

```go
type EditorRunner interface {
    Run(editor string, args []string, stdin io.Reader, stdout, stderr io.Writer) error
}
```

Production implementation: `RealEditorRunner` wraps `exec.Command`.

**Refactor:** `Manager` struct gains an `EditorRunner` field. `Create()` and `Edit()` call the interface instead of `exec.Command` directly. Default to `RealEditorRunner` in production constructors.

**New tests:**
- `Create()` full flow with mock editor (mock writes YAML to temp file)
- `Edit()` full flow with mock editor (mock modifies temp file content)
- `resolveEditor()` -- test `$EDITOR`, `$VISUAL`, fallback order
- `validateName()` -- valid names, empty, special characters
- `validateContent()` -- valid YAML, empty, binary content
- `validateFlatYAML()` -- flat maps, nested maps (rejected), non-YAML
- `fileReadable()` -- exists and readable, exists but unreadable, missing
- `defaultKeyPath()` -- with/without XDG env var

### `internal/trust/` (68.4% to 80%+)

No new interfaces needed. Pure function tests.

**New tests:**
- `DefaultStore()` -- with `XDG_DATA_HOME` set, without (falls back to `~/.local/share`), with empty string
- `Status.String()` -- `Trusted`, `Denied`, `Untrusted`, unknown value
- `atomicWrite()` -- successful write, write to read-only directory, empty content
- `fileExists()` -- file exists, directory exists (not a file), missing, permission denied

### `internal/launcher/` (69.3% to 80%+)

**Mock migration:** Replace hand-written `mockExecer` with mockgen-generated version. Update all tests that use it to use gomock API (`ctrl.Finish()`, `EXPECT()`, `Return()`).

**New tests for helper functions:**
- `filterEssentialEnv()` -- filter logic with various env slices
- `mergeEnv()` -- merge with overrides, empty inputs, duplicate keys
- `redactValue()` -- short values, long values, empty
- `resolveEffectiveYolo()` -- CLI flag vs config vs default precedence
- `stringSetDiff()` -- disjoint sets, overlapping, empty
- `yoloSource()` -- each source type
- `wrapTemplateError()` -- different template error types, nil error
- `applyTrustGate()` -- trusted, denied, untrusted states

### `pkg/seatbelt/` (88.3% -- maintain)

**Mock migration only.** Replace `testModule` with mockgen-generated `MockModule` and `MockGuard`. Update `profile_test.go` to use gomock API. No new tests needed.

### `internal/sandbox/` (81.2% -- maintain)

Generate mock for `Sandbox` interface. No test changes unless mock is already used elsewhere.

## CI Changes

### Exclude `cmd/aide/` from total coverage

The `cmd/aide/` package contains 4,976 lines of Cobra command handlers and interactive prompts. It is 0% covered and untestable without extracting business logic into separate packages.

Change the coverage calculation in `.github/workflows/ci.yml` to exclude it:

```bash
# Filter out cmd/aide from coverage total
grep -v "github.com/jskswamy/aide/cmd/" coverage.out > coverage-filtered.out
TOTAL=$(go tool cover -func=coverage-filtered.out | grep total | awk '{print $3}' | tr -d '%')
```

### Thresholds

- **Overall (excluding cmd/aide):** 60% minimum
- **Security-critical packages:** 80% minimum for `pkg/seatbelt`, `internal/sandbox`, `internal/secrets`

### `make generate` in CI

Add a CI step that runs `make generate` and checks for uncommitted changes. This catches forgotten mock regeneration:

```bash
make generate
git diff --exit-code -- '*/mocks/'
```

## Test patterns

### Mockgen test structure

```go
func TestCreate_Success(t *testing.T) {
    ctrl := gomock.NewController(t)

    mockEditor := mocks.NewMockEditorRunner(ctrl)
    mockEditor.EXPECT().
        Run(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
        DoAndReturn(func(editor string, args []string, _ io.Reader, _, _ io.Writer) error {
            // Simulate editor writing valid YAML to the temp file
            return os.WriteFile(args[0], []byte("api_key: sk-test-123\n"), 0600)
        })

    mgr := secrets.NewManager(secrets.WithEditor(mockEditor))
    err := mgr.CreateFromContent("test", []byte("api_key: sk-test-123\n"), pubKey)
    // ... assertions
}
```

### Environment variable tests

Use `t.Setenv()` (Go 1.17+) for tests that depend on environment variables. It restores the original value after the test:

```go
func TestResolveEditor_EDITOR(t *testing.T) {
    t.Setenv("EDITOR", "/usr/bin/nano")
    t.Setenv("VISUAL", "")
    got := resolveEditor()
    if got != "/usr/bin/nano" {
        t.Errorf("resolveEditor() = %q, want /usr/bin/nano", got)
    }
}
```

## Out of scope

- `cmd/aide/` -- business logic extraction is a separate task
- `internal/ui/` (58.6%) -- cosmetic output, no security impact
- Integration tests -- separate effort, not blocked by this work
- Coverage ratcheting (preventing regression) -- add after baseline established

## Success criteria

1. `internal/secrets` >= 80% coverage
2. `internal/trust` >= 80% coverage
3. `internal/launcher` >= 80% coverage
4. `pkg/seatbelt` and `internal/sandbox` maintain >= 80%
5. All interfaces have mockgen-generated mocks
6. No hand-written mock structs remain in test files
7. `make generate` produces all mocks, CI verifies no drift
8. Overall coverage (excluding `cmd/aide/`) >= 60%
9. All existing tests pass after mock migration
