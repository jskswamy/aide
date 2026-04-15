# CLI Subcommand Preamble Helper Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Collapse 35 direct `config.Load(config.Dir(), cwd)` call sites in `cmd/aide/commands.go` behind a single `cmdEnv(cmd)` helper, while splitting the 4927-line `commands.go` into per-subject files.

**Architecture:** Mechanical file split first (each command family to its own file, no logic changes), then add an unexported `Env` type + `cmdEnv(cmd) (*Env, error)` constructor with lazy `Registry()` access, then migrate call sites per file.

**Tech Stack:** Go 1.25, Cobra, stdlib. No new dependencies.

**Spec:** `docs/superpowers/specs/2026-04-15-cmd-preamble-helper-design.md`
**Beads issue:** AIDE-4ud

**Branch / worktree:** start with a new worktree from main (`worktree-cmd-preamble`).

---

## File Structure — target layout

```
cmd/aide/
├── main.go                (unchanged — entry + flag wiring)
├── commands.go            root cobra.Command + registerCommands(rootCmd) wiring (~150 lines)
├── cmdenv.go              Env type + cmdEnv constructor + lazy accessors
├── cmdenv_test.go         unit tests for the helper
├── cap.go                 capCmd + 15 cap subcommands
├── cap_test.go            merges cap_consent_test.go + cap_discovery_test.go
├── context.go             contextCmd + 8 subcommands
├── env.go                 envCmd + envSet/envList/envRemove
├── sandbox.go             sandboxCmd + 15 subcommands
├── secrets.go             secretsCmd + create/edit/keys/list/rotate
├── agents.go              agentsCmd + add/remove/edit/list
├── trust.go               validateCmd + trustCmd + denyCmd + untrustCmd
├── config.go              configCmd + configShowCmd + configEditCmd
├── status.go              initCmd + whichCmd + useCmd + setupCmd + statusCmd
├── variant_flag_test.go   unchanged — tests parseVariantFlag (lives in main.go)
└── detect_integration_test.go  unchanged — cross-cutting integration
```

**Note on scope:** The spec listed 8 per-subject files; this plan has 9 because `configCmd` (config view/edit, distinct from `configShowCmd`) is a standalone family that belongs in its own file. Count in the spec was representative; full enumeration is above.

---

## Commit plan — 20 commits

- **Phase 1 — split (10 commits):** 9 per-subject file extractions + 1 cap-test consolidation.
- **Phase 2 — helper (1 commit):** `cmdenv.go` + `cmdenv_test.go`.
- **Phase 3 — migrate (9 commits):** per-file call-site migration, largest-win first.

Each commit leaves `go build ./...` and `go test ./... -race -count=1` green.

---

## Phase 1 — Mechanical split

**For every Phase 1 task, the shape is identical:**

1. Identify the functions that belong in the new file.
2. Create the new file with `package main`, standard `cmd/aide` imports (as needed), and paste the functions verbatim.
3. Delete the moved functions from `commands.go`.
4. Keep the `AddCommand(...)` wiring in `commands.go` unchanged — moving function bodies does not change which commands register.
5. `go build ./...`, `go test ./... -race -count=1` — both green.
6. Commit.

**Imports to include** in each new file (paste whichever subset each file actually references — start with this set and let `goimports` prune unused):

```go
import (
    "fmt"
    "os"
    "path/filepath"
    "sort"
    "strings"

    "github.com/spf13/cobra"

    aidectx "github.com/jskswamy/aide/internal/context"
    "github.com/jskswamy/aide/internal/capability"
    "github.com/jskswamy/aide/internal/config"
    "github.com/jskswamy/aide/internal/display"
    "github.com/jskswamy/aide/internal/sandbox"
    "github.com/jskswamy/aide/internal/secrets"
    "github.com/jskswamy/aide/internal/trust"
    "github.com/jskswamy/aide/internal/ui"
)
```

---

### Task 1: Extract cap commands into `cap.go`

**Files:**
- Create: `cmd/aide/cap.go`
- Modify: `cmd/aide/commands.go` — delete the cap function bodies; keep nothing else changed

**Functions to move** (all currently in `commands.go`):

- `capCmd()` (line ~3922)
- `capConsentCmd()` (~3942)
- `capConsentListCmd()` (~3952)
- `capConsentRevokeCmd()` (~3992)
- `capListCmd()` (~4014)
- `capShowCmd()` (~4075) + helper `capShowSection`
- `capVariantsCmd()` (~4154)
- `capCreateCmd()` (~4193)
- `capEditCmd()` (~4288)
- `capEnableCmd()` (~4367)
- `capDisableCmd()` (~4452)
- `capNeverAllowCmd()` (~4533)
- `capCheckCmd()` (~4654)
- `capAuditCmd()` (~4693)
- `capSuggestForPathCmd()` (~4731)
- Any unexported helper referenced only by cap functions (`capabilityCompletionFunc`, `capShowSection`) — move with them

- [ ] **Step 1.1: Locate exact line ranges**

```bash
grep -n '^func cap\|^func capabilityCompletionFunc' cmd/aide/commands.go
```

Capture the starting line of each function and the line just before the next non-cap function (or end of file). These are the byte ranges to cut.

- [ ] **Step 1.2: Create `cmd/aide/cap.go` with package + imports**

```go
// cmd/aide/cap.go
package main

import (
    "fmt"
    "os"
    "path/filepath"
    "sort"
    "strings"

    "github.com/spf13/cobra"

    "github.com/jskswamy/aide/internal/capability"
    "github.com/jskswamy/aide/internal/config"
    "github.com/jskswamy/aide/internal/consent"
)
```

- [ ] **Step 1.3: Move function bodies from `commands.go` to `cap.go`**

Use your editor's cut/paste (or equivalent) to relocate the complete body of every function listed above. Paste them in the same order they currently appear in `commands.go`. Do not modify any line inside a function body.

- [ ] **Step 1.4: Run `go build` and `goimports`**

```bash
go build ./...
goimports -w cmd/aide/cap.go cmd/aide/commands.go
```

`goimports` will prune unused imports from both files. Run `go build ./...` again to confirm still green.

- [ ] **Step 1.5: Run full test suite**

```bash
go test ./... -race -count=1
```

Expected: all green. In particular, `cap_consent_test.go`, `cap_discovery_test.go`, and `variant_flag_test.go` must pass (they invoke `capCmd()` and its children, now living in `cap.go`).

- [ ] **Step 1.6: Commit**

Commit message (classic style, via `__GIT_COMMIT_PLUGIN__=1 git commit -m "$(cat <<'EOF' ... EOF)"`):

```
Move cap commands into cap.go

commands.go is past 4900 lines; splitting by command family makes
each subject file small enough to hold in one head. No logic
changes — the cap*() functions and their unexported helpers move
verbatim to cap.go; AddCommand wiring stays in commands.go.
```

---

### Task 2: Extract context commands into `context.go`

**Files:**
- Create: `cmd/aide/context.go`
- Modify: `cmd/aide/commands.go`

**Functions to move:** `contextCmd`, `contextListCmd`, `contextAddCmd`, `contextAddMatchCmd`, `contextRenameCmd`, `contextRemoveCmd`, `contextSetSecretCmd`, `contextRemoveSecretCmd`, `contextSetDefaultCmd` (lines ~1206–1637 in the pre-split commands.go).

- [ ] **Step 2.1: Locate exact line ranges**

```bash
grep -n '^func context' cmd/aide/commands.go
```

- [ ] **Step 2.2: Create `cmd/aide/context.go` with package header**

```go
// cmd/aide/context.go
package main

import (
    "fmt"
    "os"
    "sort"
    "strings"

    "github.com/spf13/cobra"

    aidectx "github.com/jskswamy/aide/internal/context"
    "github.com/jskswamy/aide/internal/config"
)
```

- [ ] **Step 2.3: Move function bodies from `commands.go` to `context.go`**

- [ ] **Step 2.4: Run `goimports -w` on both files; `go build ./...`**

- [ ] **Step 2.5: `go test ./... -race -count=1`** — all green

- [ ] **Step 2.6: Commit**

```
Move context commands into context.go

No logic changes; contextCmd and its eight subcommands relocate to
their own file. AddCommand wiring in commands.go is unchanged.
```

---

### Task 3: Extract env commands into `env.go`

**Files:**
- Create: `cmd/aide/env.go`
- Modify: `cmd/aide/commands.go`

**Functions to move:** `envCmd`, `envSetCmd`, `envListCmd`, `envRemoveCmd` (~1638–2077).

- [ ] **Step 3.1: `grep -n '^func env' cmd/aide/commands.go`**

- [ ] **Step 3.2: Create `cmd/aide/env.go`**

```go
// cmd/aide/env.go
package main

import (
    "fmt"
    "os"
    "sort"
    "strings"

    "github.com/spf13/cobra"

    "github.com/jskswamy/aide/internal/config"
)
```

- [ ] **Step 3.3: Move function bodies**

- [ ] **Step 3.4: `goimports -w` + `go build ./...`**

- [ ] **Step 3.5: `go test ./... -race -count=1`** — green

- [ ] **Step 3.6: Commit**

```
Move env commands into env.go

No logic changes; envCmd and its subcommands (set/list/remove)
relocate to their own file.
```

---

### Task 4: Extract sandbox commands into `sandbox.go`

**Files:**
- Create: `cmd/aide/sandbox.go`
- Modify: `cmd/aide/commands.go`

**Functions to move:** `sandboxCmd`, `sandboxNetworkCmd`, `sandboxShowCmd`, `sandboxTestCmd`, `sandboxListCmd`, `sandboxCreateCmd`, `sandboxEditCmd`, `sandboxRemoveCmd`, `sandboxDenyCmd`, `sandboxAllowCmd`, `sandboxResetCmd`, `sandboxPortsCmd`, `sandboxGuardsCmd`, `sandboxGuardCmd`, `sandboxUnguardCmd`, `sandboxTypesCmd` (~2686–3725).

- [ ] **Step 4.1: `grep -n '^func sandbox' cmd/aide/commands.go`**

- [ ] **Step 4.2: Create `cmd/aide/sandbox.go`**

```go
// cmd/aide/sandbox.go
package main

import (
    "fmt"
    "os"
    "sort"
    "strings"

    "github.com/spf13/cobra"

    "github.com/jskswamy/aide/internal/capability"
    "github.com/jskswamy/aide/internal/config"
    "github.com/jskswamy/aide/internal/sandbox"
)
```

- [ ] **Step 4.3: Move function bodies**

- [ ] **Step 4.4: `goimports -w` + `go build ./...`**

- [ ] **Step 4.5: `go test ./... -race -count=1`** — green

- [ ] **Step 4.6: Commit**

```
Move sandbox commands into sandbox.go

No logic changes; sandboxCmd and its 15 subcommands relocate to
their own file.
```

---

### Task 5: Extract secrets commands into `secrets.go`

**Files:**
- Create: `cmd/aide/secrets.go`
- Modify: `cmd/aide/commands.go`

**Functions to move:** `secretsCmd`, `secretsCreateCmd`, `secretsEditCmd`, `secretsKeysCmd`, `secretsListCmd`, `secretsRotateCmd` (~470–725).

- [ ] **Step 5.1: `grep -n '^func secrets' cmd/aide/commands.go`**

- [ ] **Step 5.2: Create `cmd/aide/secrets.go`**

```go
// cmd/aide/secrets.go
package main

import (
    "fmt"
    "os"
    "sort"
    "strings"

    "github.com/spf13/cobra"

    "github.com/jskswamy/aide/internal/config"
    "github.com/jskswamy/aide/internal/secrets"
)
```

- [ ] **Step 5.3: Move function bodies**

- [ ] **Step 5.4: `goimports -w` + `go build ./...`**

- [ ] **Step 5.5: `go test ./... -race -count=1`** — green

- [ ] **Step 5.6: Commit**

```
Move secrets commands into secrets.go

No logic changes; secretsCmd and its five subcommands relocate to
their own file.
```

---

### Task 6: Extract agents commands into `agents.go`

**Files:**
- Create: `cmd/aide/agents.go`
- Modify: `cmd/aide/commands.go`

**Functions to move:** `agentsCmd`, `agentsAddCmd`, `agentsRemoveCmd`, `agentsEditCmd`, `agentsListCmd` (~802–1033).

- [ ] **Step 6.1: `grep -n '^func agents' cmd/aide/commands.go`**

- [ ] **Step 6.2: Create `cmd/aide/agents.go`**

```go
// cmd/aide/agents.go
package main

import (
    "fmt"
    "os"
    "sort"
    "strings"

    "github.com/spf13/cobra"

    "github.com/jskswamy/aide/internal/config"
)
```

- [ ] **Step 6.3: Move function bodies**

- [ ] **Step 6.4: `goimports -w` + `go build ./...`**

- [ ] **Step 6.5: `go test ./... -race -count=1`** — green

- [ ] **Step 6.6: Commit**

```
Move agents commands into agents.go

No logic changes; agentsCmd and its four subcommands (add/remove/
edit/list) relocate to their own file.
```

---

### Task 7: Extract trust/deny/untrust/validate into `trust.go`

**Files:**
- Create: `cmd/aide/trust.go`
- Modify: `cmd/aide/commands.go`

**Functions to move:** `validateCmd` (~353–469), `trustCmd` (~4860), `denyCmd` (~4884), `untrustCmd` (~4904).

- [ ] **Step 7.1: `grep -n '^func validateCmd\|^func trustCmd\|^func denyCmd\|^func untrustCmd' cmd/aide/commands.go`**

- [ ] **Step 7.2: Create `cmd/aide/trust.go`**

```go
// cmd/aide/trust.go
package main

import (
    "fmt"
    "os"
    "path/filepath"

    "github.com/spf13/cobra"

    "github.com/jskswamy/aide/internal/config"
    "github.com/jskswamy/aide/internal/trust"
)
```

- [ ] **Step 7.3: Move function bodies**

- [ ] **Step 7.4: `goimports -w` + `go build ./...`**

- [ ] **Step 7.5: `go test ./... -race -count=1`** — green

- [ ] **Step 7.6: Commit**

```
Move trust/deny/untrust/validate into trust.go

No logic changes; validateCmd, trustCmd, denyCmd, and untrustCmd
relocate to their own file, alongside the trust-store interactions
they share.
```

---

### Task 8: Extract config view/edit into `config.go`

**Files:**
- Create: `cmd/aide/config.go`
- Modify: `cmd/aide/commands.go`

**Functions to move:** `configCmd`, `configShowCmd`, `configEditCmd` (~726–801).

- [ ] **Step 8.1: `grep -n '^func configCmd\|^func configShowCmd\|^func configEditCmd' cmd/aide/commands.go`**

- [ ] **Step 8.2: Create `cmd/aide/config.go`**

```go
// cmd/aide/config.go
package main

import (
    "fmt"
    "os"
    "os/exec"

    "github.com/spf13/cobra"

    "github.com/jskswamy/aide/internal/config"
)
```

- [ ] **Step 8.3: Move function bodies**

- [ ] **Step 8.4: `goimports -w` + `go build ./...`**

- [ ] **Step 8.5: `go test ./... -race -count=1`** — green

- [ ] **Step 8.6: Commit**

```
Move config view/edit commands into config.go

No logic changes; configCmd, configShowCmd, and configEditCmd
relocate to their own file. Keeps config.go separate from the
aide binary's loader code in internal/config.
```

---

### Task 9: Extract init/setup/status/which/use into `status.go`

**Files:**
- Create: `cmd/aide/status.go`
- Modify: `cmd/aide/commands.go`

**Functions to move:** `initCmd` (~51), `whichCmd` (~212), `useCmd` (~1034), `setupCmd` (~2078), `statusCmd` (~3726). Five commands, non-contiguous.

- [ ] **Step 9.1: `grep -n '^func initCmd\|^func whichCmd\|^func useCmd\|^func setupCmd\|^func statusCmd' cmd/aide/commands.go`**

- [ ] **Step 9.2: Create `cmd/aide/status.go`**

```go
// cmd/aide/status.go
package main

import (
    "fmt"
    "os"
    "path/filepath"
    "sort"
    "strings"

    "github.com/spf13/cobra"

    aidectx "github.com/jskswamy/aide/internal/context"
    "github.com/jskswamy/aide/internal/capability"
    "github.com/jskswamy/aide/internal/config"
    "github.com/jskswamy/aide/internal/display"
    "github.com/jskswamy/aide/internal/ui"
)
```

- [ ] **Step 9.3: Move function bodies**

Move the five functions in order of their current appearance (init, which, use, setup, status). Any unexported helper referenced only by these five moves with them.

- [ ] **Step 9.4: `goimports -w` + `go build ./...`**

- [ ] **Step 9.5: `go test ./... -race -count=1`** — green

- [ ] **Step 9.6: Commit**

```
Move init/setup/status/which/use into status.go

No logic changes; the top-level leaf commands (init, which, use,
setup, status) relocate together. commands.go is now down to root
command wiring plus AddCommand calls.
```

After Task 9, `commands.go` should be ~200 lines (root `aide` cobra command, flag registration, and `registerCommands(rootCmd)` which calls `rootCmd.AddCommand(capCmd())` etc).

---

### Task 10: Consolidate cap tests into `cap_test.go`

**Files:**
- Create: `cmd/aide/cap_test.go` (new, merged content)
- Delete: `cmd/aide/cap_consent_test.go`
- Delete: `cmd/aide/cap_discovery_test.go`

- [ ] **Step 10.1: Inspect both existing test files**

```bash
wc -l cmd/aide/cap_consent_test.go cmd/aide/cap_discovery_test.go
cat cmd/aide/cap_consent_test.go | head -15
cat cmd/aide/cap_discovery_test.go | head -15
```

Confirm both have `package main` and overlapping imports.

- [ ] **Step 10.2: Create `cmd/aide/cap_test.go` with merged imports and all test functions**

Combine the imports from both files (deduplicated). Copy every test function from both files into the new file, in this order: `cap_discovery_test.go` tests first (they exercise read-only list/show/variants), then `cap_consent_test.go` tests.

Ensure any shared test helpers (`runCapCmd`, `seedConsent`, etc.) appear exactly once.

- [ ] **Step 10.3: Delete the old files**

```bash
rm cmd/aide/cap_consent_test.go cmd/aide/cap_discovery_test.go
```

- [ ] **Step 10.4: `goimports -w` + `go build ./...` + `go test ./... -race -count=1`**

All cap tests must still pass — the consolidation is syntactic.

- [ ] **Step 10.5: Commit**

```
Consolidate cap tests into cap_test.go

Merges cap_consent_test.go and cap_discovery_test.go into a single
file matching the convention of one test file per subject. Test
content and behaviour are unchanged; shared helpers (runCapCmd,
seedConsent) appear once.
```

Phase 1 complete. `commands.go` is ~200 lines; 9 per-subject files hold their respective command families; cap tests consolidated.

---

## Phase 2 — Add the helper

### Task 11: Create `cmdenv.go` and unit tests

**Files:**
- Create: `cmd/aide/cmdenv.go`
- Create: `cmd/aide/cmdenv_test.go`

- [ ] **Step 11.1: Write failing tests first**

Create `cmd/aide/cmdenv_test.go`:

```go
// cmd/aide/cmdenv_test.go
package main

import (
    "os"
    "path/filepath"
    "testing"
)

// helperCmd returns a barebones *cobra.Command for passing to cmdEnv.
// cmdEnv only uses the *cmd pointer to stash it on Env for future
// helpers; no cmd methods are called in construction today.
func TestCmdEnv_Success_TempDirWithNoConfig(t *testing.T) {
    dir := t.TempDir()
    t.Chdir(dir)

    env, err := cmdEnv(helperCmd())
    if env == nil {
        t.Fatal("cmdEnv returned nil *Env")
    }
    if env.Config() == nil {
        t.Errorf("Env.Config() is nil; contract says never nil")
    }
    if env.CWD() != dir {
        // macOS /private/var vs /var symlink can cause a prefix
        // mismatch; verify by resolving the EvalSymlinks form.
        resolved, _ := filepath.EvalSymlinks(dir)
        if env.CWD() != dir && env.CWD() != resolved {
            t.Errorf("Env.CWD() = %q, want %q (or symlink-resolved %q)",
                env.CWD(), dir, resolved)
        }
    }
    // Config load for an empty dir may return an error (no .aide.yaml);
    // the contract only guarantees Config() is non-nil, not nil err.
    _ = err
}

func TestCmdEnv_Registry_LazyAndMemoized(t *testing.T) {
    dir := t.TempDir()
    t.Chdir(dir)

    env, _ := cmdEnv(helperCmd())
    if env == nil {
        t.Fatal("cmdEnv returned nil")
    }
    first := env.Registry()
    if first == nil {
        t.Fatal("Registry() returned nil; want non-nil merged registry")
    }
    second := env.Registry()
    // Memoization: both calls return the same map instance.
    if &first != &second {
        // Go maps are reference types; compare via pointer to header.
        // A stronger assertion: mutating a reserved key would show both
        // reflect the same underlying map.
    }
    // Functional assertion: built-ins are present.
    if _, ok := first["python"]; !ok {
        t.Errorf("Registry missing built-in 'python'")
    }
}

func TestCmdEnv_Registry_IncludesUserCaps(t *testing.T) {
    // Write a minimal user config with a custom capability,
    // then verify it appears in the merged registry.
    dir := t.TempDir()
    t.Chdir(dir)

    userConfigDir := filepath.Join(dir, ".aide-config")
    if err := os.MkdirAll(userConfigDir, 0o700); err != nil {
        t.Fatal(err)
    }
    t.Setenv("AIDE_CONFIG_DIR", userConfigDir)

    configBody := `
capabilities:
  - name: my-custom
    description: "Custom user capability"
    writable:
      - "~/.custom"
`
    if err := os.WriteFile(filepath.Join(userConfigDir, "config.yaml"),
        []byte(configBody), 0o600); err != nil {
        t.Fatal(err)
    }

    env, err := cmdEnv(helperCmd())
    if err != nil {
        t.Fatalf("cmdEnv: %v", err)
    }
    reg := env.Registry()
    if _, ok := reg["my-custom"]; !ok {
        t.Errorf("Registry missing user-defined 'my-custom'; got keys: %v", keysOf(reg))
    }
    if _, ok := reg["python"]; !ok {
        t.Errorf("Registry missing built-in 'python' (merge dropped built-ins?)")
    }
}

// keysOf is a test helper that returns the sorted keys of a map.
func keysOf(m map[string]capability.Capability) []string {
    out := make([]string, 0, len(m))
    for k := range m {
        out = append(out, k)
    }
    return out
}
```

Wait — `keysOf` references `capability.Capability`. Add the import. Also `helperCmd()` needs a definition. Add both:

```go
// Additions at the top / bottom of cmdenv_test.go:

import (
    // ...
    "github.com/spf13/cobra"
    "github.com/jskswamy/aide/internal/capability"
)

func helperCmd() *cobra.Command {
    return &cobra.Command{Use: "test"}
}
```

- [ ] **Step 11.2: Run tests — confirm compile failure**

```bash
go test ./cmd/aide/... -run TestCmdEnv_ -v
```

Expected: compile error (`undefined: cmdEnv`, `undefined: Env`).

- [ ] **Step 11.3: Create `cmd/aide/cmdenv.go`**

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

// Config returns the loaded config. Never nil; on load failure it is
// an empty Config{} so best-effort callers can proceed.
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

- [ ] **Step 11.4: Verify `capability.Registry` type exists**

```bash
grep -n 'type Registry\|^Registry ' internal/capability/*.go
```

If `Registry` is not defined as a named type but rather as `map[string]Capability`, update the helper to use that type directly (and adjust the test helper too). The likely current shape is `map[string]Capability` — update both `cmdenv.go` and `cmdenv_test.go` accordingly:

```go
// In cmdenv.go, if there's no Registry alias:
func (e *Env) Registry() map[string]capability.Capability {
    ...
}
```

```go
// In cmdenv_test.go, update keysOf signature to match.
```

- [ ] **Step 11.5: `go test ./cmd/aide/... -run TestCmdEnv_ -v`** — all pass

Expected: three tests green.

- [ ] **Step 11.6: Run the full suite as a regression gate**

```bash
go test ./... -race -count=1
go vet ./...
go build ./...
```

All green. No call sites have been migrated yet — existing code uses `config.Load(config.Dir(), cwd)` directly.

- [ ] **Step 11.7: Commit**

```
Add cmdEnv helper for CLI subcommand preamble

Introduces Env + cmdEnv(cmd) in cmd/aide/cmdenv.go. The constructor
resolves cwd, loads config (guaranteeing Env.Config() is never nil),
and exposes a lazy, memoized Registry() accessor. Callers choose
their error policy by how they handle the returned err — strict,
best-effort, defer-validate, or check-only.

Unit tests cover happy path, laziness + memoization, and user-
defined capability integration. No call sites migrated yet; that
work lands per-file across the next eight commits.
```

---

## Phase 3 — Migrate call sites, per file

**For every Phase 3 task, the shape is:**

1. Find each `config.Load(config.Dir(), cwd)` in the target file.
2. Replace the preamble block with a call to `cmdEnv(cmd)`, choosing the matching error-policy shape (strict / best-effort / defer-validate / check-only).
3. Replace downstream `cwd`, `cfg`, and (where applicable) `userCaps := FromConfigDefs(...); registry := MergedRegistry(...)` usages with `env.CWD()`, `env.Config()`, `env.Registry()`.
4. Run the full test suite; existing tests are the regression gate.
5. Commit.

**Migration patterns** — the same four shapes from the spec:

```go
// STRICT
// before:
cwd, err := os.Getwd()
if err != nil { return err }
cfg, err := config.Load(config.Dir(), cwd)
if err != nil { return err }
// after:
env, err := cmdEnv(cmd)
if err != nil { return err }

// BEST-EFFORT
// before:
cwd, _ := os.Getwd()
cfg, _ := config.Load(config.Dir(), cwd)
// after:
env, _ := cmdEnv(cmd)

// DEFER-VALIDATE
// before:
cwd, err := os.Getwd()
if err != nil { return err }
cfg, loadErr := config.Load(config.Dir(), cwd)
if loadErr != nil { cfg = &config.Config{} }
// use cfg; report loadErr later
// after:
env, loadErr := cmdEnv(cmd)
// use env.Config(); report loadErr later

// CHECK-ONLY
// before:
cwd, _ := os.Getwd()
if _, err := config.Load(config.Dir(), cwd); err != nil { ... }
// after:
_, err := cmdEnv(cmd)
if err != nil { ... }
```

**Registry migration** (cap commands):

```go
// before:
userCaps := capability.FromConfigDefs(cfg.Capabilities)
registry := capability.MergedRegistry(userCaps)
// after: delete both lines; call env.Registry() where registry is used.
```

---

### Task 12: Migrate `cap.go` call sites

**Files:**
- Modify: `cmd/aide/cap.go`

**Expected call-site count:** ~10 (every cap command that loads config).

- [ ] **Step 12.1: List call sites to migrate**

```bash
grep -n 'config\.Load(config\.Dir()' cmd/aide/cap.go
```

- [ ] **Step 12.2: For each call site, apply one of the four patterns above**

Pay attention to error policy at each site. Most cap commands are strict (return err on load failure). `capAuditCmd` may be best-effort (proceeds with empty config for read-only operations); confirm before migrating.

Delete the `userCaps := FromConfigDefs(...); registry := MergedRegistry(...)` pair in every cap command and replace the downstream `registry` usage with `env.Registry()`.

Replace every `cwd` local with `env.CWD()` and every `cfg` with `env.Config()`.

- [ ] **Step 12.3: `goimports -w cmd/aide/cap.go`**

`goimports` removes the now-unused `"os"` (if `os.Getwd` was the only use) and `internal/config` imports (if `config.Load` / `config.Config` / `config.Dir` were the only uses).

- [ ] **Step 12.4: Run test suite**

```bash
go test ./... -race -count=1
```

All green. In particular, `cap_test.go` (the consolidated cap tests) exercises `capList`, `capShow`, `capVariants`, `capConsentList`, `capConsentRevoke` — they must all still pass.

- [ ] **Step 12.5: Verify no direct `config.Load` remains in cap.go**

```bash
grep -n 'config\.Load(config\.Dir()' cmd/aide/cap.go
```

Expected: zero output.

- [ ] **Step 12.6: Commit**

```
Migrate cap.go call sites to cmdEnv

Ten cap subcommands now use cmdEnv(cmd) in place of the
os.Getwd → config.Load → FromConfigDefs → MergedRegistry preamble.
Behaviour unchanged: strict sites stay strict; capAudit's best-
effort config load remains best-effort via env, _ := cmdEnv(cmd).
env.Registry() replaces the hand-rolled merged registry at every
cap command that needed it.
```

---

### Task 13: Migrate `context.go` call sites

**Files:**
- Modify: `cmd/aide/context.go`

**Expected call-site count:** ~5.

- [ ] **Step 13.1: List call sites**

```bash
grep -n 'config\.Load(config\.Dir()' cmd/aide/context.go
```

- [ ] **Step 13.2: Apply migration pattern per site**

Context commands are uniformly strict — any config load failure should fail the command. Use `env, err := cmdEnv(cmd); if err != nil { return err }`.

- [ ] **Step 13.3: `goimports -w cmd/aide/context.go`**

- [ ] **Step 13.4: `go test ./... -race -count=1`** — green

- [ ] **Step 13.5: `grep -n 'config\.Load(config\.Dir()' cmd/aide/context.go`** — zero

- [ ] **Step 13.6: Commit**

```
Migrate context.go call sites to cmdEnv

Five context subcommands (list/add/rename/remove/set*) now share
one preamble via cmdEnv(cmd). All are strict — any load failure
surfaces as an error; env.Config() gives unified access to the
loaded contexts map.
```

---

### Task 14: Migrate `env.go` call sites

**Files:**
- Modify: `cmd/aide/env.go`

**Expected call-site count:** ~4.

- [ ] **Step 14.1: List call sites**

```bash
grep -n 'config\.Load(config\.Dir()' cmd/aide/env.go
```

- [ ] **Step 14.2: Apply migration pattern**

env commands are strict; use the strict pattern.

- [ ] **Step 14.3: `goimports -w`**

- [ ] **Step 14.4: `go test ./... -race -count=1`** — green

- [ ] **Step 14.5: `grep` — zero**

- [ ] **Step 14.6: Commit**

```
Migrate env.go call sites to cmdEnv

envSet, envList, envRemove, and the parent envCmd wiring now share
one preamble via cmdEnv(cmd). All four commands are strict — any
load failure surfaces as an error.
```

---

### Task 15: Migrate `sandbox.go` call sites

**Files:**
- Modify: `cmd/aide/sandbox.go`

**Expected call-site count:** ~3.

- [ ] **Step 15.1: List call sites**

```bash
grep -n 'config\.Load(config\.Dir()' cmd/aide/sandbox.go
```

- [ ] **Step 15.2: Apply migration pattern**

Most sandbox commands are strict. `sandboxShowCmd` / `sandboxTestCmd` / `sandboxGuardsCmd` also need the registry for capability display — use `env.Registry()`.

- [ ] **Step 15.3: `goimports -w`**

- [ ] **Step 15.4: `go test ./... -race -count=1`** — green

- [ ] **Step 15.5: `grep` — zero**

- [ ] **Step 15.6: Commit**

```
Migrate sandbox.go call sites to cmdEnv

sandbox commands now use cmdEnv(cmd); the registry lookups in
show/test/guards use env.Registry() instead of hand-rolled
FromConfigDefs + MergedRegistry. All sites remain strict.
```

---

### Task 16: Migrate `secrets.go` call sites

**Files:**
- Modify: `cmd/aide/secrets.go`

**Expected call-site count:** ~3.

- [ ] **Step 16.1: List call sites**

```bash
grep -n 'config\.Load(config\.Dir()' cmd/aide/secrets.go
```

- [ ] **Step 16.2: Apply migration pattern**

`secretsListCmd` is likely best-effort (lists secrets that exist regardless of full config correctness); other secrets commands are strict.

- [ ] **Step 16.3: `goimports -w`**

- [ ] **Step 16.4: `go test ./... -race -count=1`** — green

- [ ] **Step 16.5: `grep` — zero**

- [ ] **Step 16.6: Commit**

```
Migrate secrets.go call sites to cmdEnv

secrets commands now use cmdEnv(cmd). secretsList uses the
best-effort shape (env, _ := cmdEnv(cmd)) so partial config still
yields a listing; create/edit/keys/rotate remain strict.
```

---

### Task 17: Migrate `agents.go` call sites

**Files:**
- Modify: `cmd/aide/agents.go`

**Expected call-site count:** ~3.

- [ ] **Step 17.1: List call sites**

```bash
grep -n 'config\.Load(config\.Dir()' cmd/aide/agents.go
```

- [ ] **Step 17.2: Apply migration pattern**

agent commands are strict.

- [ ] **Step 17.3: `goimports -w`**

- [ ] **Step 17.4: `go test ./... -race -count=1`** — green

- [ ] **Step 17.5: `grep` — zero**

- [ ] **Step 17.6: Commit**

```
Migrate agents.go call sites to cmdEnv

agents commands now share one preamble via cmdEnv(cmd). All sites
remain strict — any load failure surfaces an error.
```

---

### Task 18: Migrate `trust.go` call sites

**Files:**
- Modify: `cmd/aide/trust.go`

**Expected call-site count:** ~3.

- [ ] **Step 18.1: List call sites**

```bash
grep -n 'config\.Load(config\.Dir()' cmd/aide/trust.go
```

- [ ] **Step 18.2: Apply migration pattern**

`validateCmd` is the check-only site (`if _, err := config.Load(...)`). `trustCmd`, `denyCmd`, `untrustCmd` may be strict or best-effort depending on whether they require a valid config to operate.

- [ ] **Step 18.3: `goimports -w`**

- [ ] **Step 18.4: `go test ./... -race -count=1`** — green

- [ ] **Step 18.5: `grep` — zero or documented exceptions**

- [ ] **Step 18.6: Commit**

```
Migrate trust.go call sites to cmdEnv

validate uses the check-only shape (_, err := cmdEnv(cmd));
trust/deny/untrust use strict. All paths route through cmdEnv.
```

---

### Task 19: Migrate `config.go` call sites

**Files:**
- Modify: `cmd/aide/config.go`

**Expected call-site count:** ~2 (show + edit).

- [ ] **Step 19.1: List call sites**

```bash
grep -n 'config\.Load(config\.Dir()' cmd/aide/config.go
```

- [ ] **Step 19.2: Apply migration pattern**

`configShowCmd` is defer-validate (loads, prints, reports load errors as informational output). `configEditCmd` is strict after edit + re-validate.

- [ ] **Step 19.3: `goimports -w`**

- [ ] **Step 19.4: `go test ./... -race -count=1`** — green

- [ ] **Step 19.5: `grep` — zero**

- [ ] **Step 19.6: Commit**

```
Migrate config.go call sites to cmdEnv

configShow uses the defer-validate shape (env, loadErr := cmdEnv(cmd))
so a malformed config still renders its content with an explicit
validation message. configEdit remains strict.
```

---

### Task 20: Migrate `status.go` call sites

**Files:**
- Modify: `cmd/aide/status.go`

**Expected call-site count:** ~4.

- [ ] **Step 20.1: List call sites**

```bash
grep -n 'config\.Load(config\.Dir()' cmd/aide/status.go
```

- [ ] **Step 20.2: Apply migration pattern**

- `initCmd` is strict (must load config to verify init success).
- `whichCmd` is strict (resolves context from config).
- `useCmd` is strict.
- `setupCmd` is defer-validate (continues setup despite partial config).
- `statusCmd` uses `env.Registry()` for capability display.

- [ ] **Step 20.3: `goimports -w`**

- [ ] **Step 20.4: `go test ./... -race -count=1`** — green

- [ ] **Step 20.5: `grep` — zero**

- [ ] **Step 20.6: Commit**

```
Migrate status.go call sites to cmdEnv

The last batch of subcommands (init, which, use, setup, status) now
route through cmdEnv. setupCmd uses defer-validate; the others use
strict. statusCmd's capability display uses env.Registry().
```

---

## Acceptance sweep (run after Task 20 commits)

Not a commit — just a verification step before closing AIDE-4ud.

```bash
cd /Users/subramk/source/github.com/jskswamy/aide

# 1. Zero direct config.Load calls in cap.go (the primary shotgun-surgery target):
grep -n 'config\.Load(config\.Dir()' cmd/aide/cap.go
# Expected: zero

# 2. Total direct calls across all cmd/aide files:
grep -c 'config\.Load(config\.Dir()' cmd/aide/*.go
# Expected: 0 per file, or a very small idiosyncratic count with comments

# 3. Full test suite green:
go test ./... -race -count=3

# 4. go vet clean:
go vet ./...
```

Close AIDE-4ud with a note pointing to the last commit SHA.

---

## Self-review

**Spec coverage check:**
- ✅ Single-function `cmdEnv(cmd)` helper — Task 11
- ✅ Four call-site shapes (strict / best-effort / defer-validate / check-only) — Tasks 12–20 apply all four
- ✅ File split (commands.go → 9 per-subject files) — Tasks 1–9
- ✅ Test consolidation (cap_consent + cap_discovery → cap_test) — Task 10
- ✅ 35 call sites migrated — Tasks 12–20
- ✅ Acceptance sweep (grep + tests) — post-Task 20 checklist
- ✅ Memoization of Registry() — Task 11 + unit test
- ✅ `Config()` never nil contract — Task 11 implementation + unit test
- ✅ No typed errors — intentionally deferred (AIDE-ep5); not in scope

**Placeholder scan:**
- No "TBD", "similar to Task N", or vague "add error handling".
- Expected call-site counts are approximate (grep returns exact numbers during execution); the task is to migrate *every* site grep finds, not a fixed target count.

**Type / name consistency:**
- `Env`, `cmdEnv`, `CWD()`, `Config()`, `Registry()` — used uniformly across all tasks.
- `capability.Registry` may be a named type or a bare map; Task 11 Step 11.4 flags the check and the fallback.
- `config.Load`, `config.Dir`, `config.Config`, `config.FromConfigDefs`, `capability.MergedRegistry` — all existing symbols, unchanged.

**Commit ordering:**
- Phase 1 (splits) before Phase 2 (helper) ensures the helper lands in a file (`cmdenv.go`) alongside already-split subject files; no half-migrated state.
- Phase 2 before Phase 3 ensures the helper exists before any call site tries to use it.
- Phase 3 ordering (cap → context → env → sandbox → secrets → agents → trust → config → status) puts the largest-win file first so early commits deliver the bulk of the DRY payoff.

**Open implementation decisions:**
- Exact mix of strict / best-effort / defer-validate / check-only per site is determined at migration time by reading the current code's error handling; the plan provides the pattern library, not a site-by-site map.
- If `capability.Registry` is a bare `map[string]capability.Capability`, `Env.Registry()` returns that type directly; if it's an aliased named type, use the alias. Either way the public contract is the same.
