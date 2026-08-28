# Sandbox Agent Directory Grant Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `aide sandbox allow <dir>` also grants the directory in Claude's own `additionalDirectories` permission list, and `aide sandbox deny <dir>` reciprocally revokes it, so the manual "edit `.claude/settings.local.json` by hand" step goes away.

**Architecture:** A new optional `provision.DirectoryGranter` interface (type-asserted from the registered `Provisioner`, the same pattern already used for `provision.AgentDirProvider`/`provision.HookInstaller`) is implemented only by the `claude` driver, editing `settings.json`/`settings.local.json` directly — the same file-edit approach `hooks.go`/`WriteStatusLine` already use because Claude has no persistent CLI surface for this. `sandboxAllowCmd`/`sandboxDenyCmd` call it, best-effort, right after their existing aide-side sandbox-policy mutation.

**Tech Stack:** Go, `github.com/spf13/cobra`, existing `internal/provision`, `internal/fsutil`, `internal/config`, `internal/context` (aliased `aidectx`) packages. No new dependencies.

## Global Constraints

- Scoped to `sandbox allow` and `sandbox deny` only — no launch-time flag injection, no changes to `sandbox create`/`edit`/`remove`/named profiles.
- Only the `claude` driver implements the new capability in this pass; every other agent is unaffected (no `DriverBase` changes needed — this follows the `AgentDirProvider` pattern of a pure optional interface, not a `Capabilities` flag + stub).
- The agent-side grant/revoke is best-effort: a failure there is printed as a warning to stderr, never fails the command. The aide-side sandbox policy write (already existing behavior) is unaffected and is the security-relevant part.
- Both commands gain a `--no-agent-grant` bool flag (default `false`) that skips the agent-side call entirely.
- `sandbox deny <path>` removes any `additionalDirectories` entry equal to `path` or nested under it, but never an entry that is merely a *parent* of `path`.
- Deviation from the design spec's sketch (`docs/superpowers/specs/2026-08-28-sandbox-agent-directory-grant-design.md`): that doc proposed adding `GrantDirectory`/`RevokeDirectory` directly to the `Provisioner` interface with `DriverBase` no-op stubs. Reading `internal/provision/provisioner.go` during planning found the codebase already has a cleaner precedent for this exact shape — `AgentDirProvider`/`ResolveAgentDir` — a standalone optional interface, type-asserted only where needed, no `Capabilities` flag or `DriverBase` stub required. This plan follows that precedent instead. Behavior and CLI surface are unchanged from the spec; only the internal interface shape differs.

---

### Task 1: Extract `readSettingsFile` in the claude driver

**Files:**
- Modify: `internal/provision/agents/claude/hooks.go:185-202` (the `readSettings` function)
- Test: `internal/provision/agents/claude/hooks_test.go` (existing tests — no new test, this task must leave them green)

**Interfaces:**
- Consumes: nothing new.
- Produces: `readSettingsFile(path string) (map[string]interface{}, error)` — a path-parameterized JSON reader that Task 2's `directories.go` will call directly (directory grants need to read a *different* file than `settingsPath(ctx)` for project scope, so the existing `ctx`-only `readSettings` isn't enough).

This is a pure refactor (extract a helper, no behavior change for existing callers) — there is no new behavior to write a failing test for. The verification step is running the existing suite and confirming it's still green.

- [ ] **Step 1: Extract the file-reading logic out of `readSettings`**

Replace the current `readSettings` function (`internal/provision/agents/claude/hooks.go:185-202`):

```go
func readSettings(ctx provision.Context) (map[string]interface{}, error) {
	path := settingsPath(ctx)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]interface{}{}, nil
		}
		return nil, fmt.Errorf("claude hooks: read %s: %w", path, err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("claude hooks: parse %s: %w", path, err)
	}
	if m == nil {
		m = map[string]interface{}{}
	}
	return m, nil
}
```

with:

```go
func readSettings(ctx provision.Context) (map[string]interface{}, error) {
	return readSettingsFile(settingsPath(ctx))
}

// readSettingsFile reads and parses the JSON object at path. A missing
// file returns an empty map rather than an error so callers can treat
// "no settings file yet" the same as "empty settings".
func readSettingsFile(path string) (map[string]interface{}, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]interface{}{}, nil
		}
		return nil, fmt.Errorf("claude settings: read %s: %w", path, err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("claude settings: parse %s: %w", path, err)
	}
	if m == nil {
		m = map[string]interface{}{}
	}
	return m, nil
}
```

(The error-message prefix changes from `"claude hooks:"` to `"claude settings:"` since the function is no longer hooks-specific. No test asserts on that exact string — confirmed via `grep -n "claude hooks:" internal/provision/agents/claude/*_test.go`, which has no matches.)

- [ ] **Step 2: Run the existing claude package tests to confirm no regression**

Run: `go test ./internal/provision/agents/claude/...`
Expected: PASS (all existing tests, unchanged)

- [ ] **Step 3: Commit**

```bash
git add internal/provision/agents/claude/hooks.go
git commit -m "Extract readSettingsFile helper in claude driver"
```

---

### Task 2: Add `DirectoryGranter` and implement it for Claude

**Files:**
- Modify: `internal/provision/provisioner.go` (add the `DirectoryGranter` interface near `AgentDirProvider`, `internal/provision/provisioner.go:227-241`)
- Create: `internal/provision/agents/claude/directories.go`
- Test: `internal/provision/agents/claude/directories_test.go`

**Interfaces:**
- Consumes: `readSettingsFile(path string) (map[string]interface{}, error)` from Task 1; `provision.Context{Name, Agent, HomeDir, ProjectRoot, Env}` (existing struct, `internal/provision/provisioner.go:69-81`); `fsutil.AtomicWrite(path string, data []byte) error` (existing, `internal/fsutil/atomicwrite.go:31`).
- Produces: `provision.DirectoryGranter` interface with `GrantDirectory(ctx Context, path string, write bool) error` and `RevokeDirectory(ctx Context, path string) error`. `*claude.Driver` implements both. Task 3 and Task 4 depend on this exact interface and on `(*claude.Driver)` satisfying it (verified via a compile-time assertion in `directories.go`).

- [ ] **Step 1: Write the failing tests**

Create `internal/provision/agents/claude/directories_test.go`:

```go
package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/jskswamy/aide/internal/provision"
)

func TestGrantDirectory_GlobalScope_FreshFile(t *testing.T) {
	home := t.TempDir()
	ctx := provision.Context{HomeDir: home}
	d := &Driver{}

	if err := d.GrantDirectory(ctx, "/repo/a", false); err != nil {
		t.Fatalf("GrantDirectory: %v", err)
	}

	got := readAdditionalDirectoriesFromDisk(t, filepath.Join(home, ".claude", "settings.json"))
	if len(got) != 1 || got[0] != "/repo/a" {
		t.Fatalf("additionalDirectories = %v, want [/repo/a]", got)
	}
}

func TestGrantDirectory_ProjectScope_WritesSettingsLocal(t *testing.T) {
	project := t.TempDir()
	ctx := provision.Context{HomeDir: t.TempDir(), ProjectRoot: project}
	d := &Driver{}

	if err := d.GrantDirectory(ctx, "/repo/a", false); err != nil {
		t.Fatalf("GrantDirectory: %v", err)
	}

	path := filepath.Join(project, ".claude", "settings.local.json")
	got := readAdditionalDirectoriesFromDisk(t, path)
	if len(got) != 1 || got[0] != "/repo/a" {
		t.Fatalf("additionalDirectories = %v, want [/repo/a]", got)
	}
}

func TestGrantDirectory_PreservesExistingKeys(t *testing.T) {
	home := t.TempDir()
	settingsFile := filepath.Join(home, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settingsFile), 0o755); err != nil {
		t.Fatal(err)
	}
	existing := `{"permissions":{"allow":["Bash(git *)"]},"model":"sonnet"}`
	if err := os.WriteFile(settingsFile, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := provision.Context{HomeDir: home}
	d := &Driver{}

	if err := d.GrantDirectory(ctx, "/repo/a", false); err != nil {
		t.Fatalf("GrantDirectory: %v", err)
	}

	raw := readRawSettings(t, settingsFile)
	if raw["model"] != "sonnet" {
		t.Errorf("model key lost: %v", raw)
	}
	perms, _ := raw["permissions"].(map[string]interface{})
	allow, _ := perms["allow"].([]interface{})
	if len(allow) != 1 || allow[0] != "Bash(git *)" {
		t.Errorf("allow list lost: %v", perms)
	}
	dirs := readAdditionalDirectoriesFromDisk(t, settingsFile)
	if len(dirs) != 1 || dirs[0] != "/repo/a" {
		t.Errorf("additionalDirectories = %v", dirs)
	}
}

func TestGrantDirectory_NoDuplicateOnRepeat(t *testing.T) {
	home := t.TempDir()
	ctx := provision.Context{HomeDir: home}
	d := &Driver{}

	mustGrant(t, d, ctx, "/repo/a")
	mustGrant(t, d, ctx, "/repo/a")

	got := readAdditionalDirectoriesFromDisk(t, filepath.Join(home, ".claude", "settings.json"))
	if len(got) != 1 {
		t.Fatalf("additionalDirectories = %v, want exactly one entry", got)
	}
}

func TestRevokeDirectory_ExactMatchRemoved(t *testing.T) {
	home := t.TempDir()
	ctx := provision.Context{HomeDir: home}
	d := &Driver{}
	mustGrant(t, d, ctx, "/repo/a")

	if err := d.RevokeDirectory(ctx, "/repo/a"); err != nil {
		t.Fatalf("RevokeDirectory: %v", err)
	}

	got := readAdditionalDirectoriesFromDisk(t, filepath.Join(home, ".claude", "settings.json"))
	if len(got) != 0 {
		t.Fatalf("additionalDirectories = %v, want empty", got)
	}
}

func TestRevokeDirectory_NestedPathRemoved(t *testing.T) {
	home := t.TempDir()
	ctx := provision.Context{HomeDir: home}
	d := &Driver{}
	mustGrant(t, d, ctx, "/repo/a/sub")

	if err := d.RevokeDirectory(ctx, "/repo/a"); err != nil {
		t.Fatalf("RevokeDirectory: %v", err)
	}

	got := readAdditionalDirectoriesFromDisk(t, filepath.Join(home, ".claude", "settings.json"))
	if len(got) != 0 {
		t.Fatalf("additionalDirectories = %v, want empty (nested path should be removed)", got)
	}
}

func TestRevokeDirectory_SiblingWithSharedPrefixUntouched(t *testing.T) {
	home := t.TempDir()
	ctx := provision.Context{HomeDir: home}
	d := &Driver{}
	mustGrant(t, d, ctx, "/repo/bar")
	mustGrant(t, d, ctx, "/repo/barbaz")

	if err := d.RevokeDirectory(ctx, "/repo/bar"); err != nil {
		t.Fatalf("RevokeDirectory: %v", err)
	}

	got := readAdditionalDirectoriesFromDisk(t, filepath.Join(home, ".claude", "settings.json"))
	if len(got) != 1 || got[0] != "/repo/barbaz" {
		t.Fatalf("additionalDirectories = %v, want [/repo/barbaz] (sibling must survive)", got)
	}
}

func TestRevokeDirectory_ParentOfDeniedPathUntouched(t *testing.T) {
	home := t.TempDir()
	ctx := provision.Context{HomeDir: home}
	d := &Driver{}
	mustGrant(t, d, ctx, "/repo")

	if err := d.RevokeDirectory(ctx, "/repo/a"); err != nil {
		t.Fatalf("RevokeDirectory: %v", err)
	}

	got := readAdditionalDirectoriesFromDisk(t, filepath.Join(home, ".claude", "settings.json"))
	if len(got) != 1 || got[0] != "/repo" {
		t.Fatalf("additionalDirectories = %v, want [/repo] (parent grant must survive)", got)
	}
}

func TestRevokeDirectory_NoopWhenFileMissing(t *testing.T) {
	home := t.TempDir()
	ctx := provision.Context{HomeDir: home}
	d := &Driver{}

	if err := d.RevokeDirectory(ctx, "/repo/a"); err != nil {
		t.Fatalf("RevokeDirectory on missing file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "settings.json")); err == nil {
		t.Fatalf("RevokeDirectory should not create a settings file when nothing changed")
	}
}

func mustGrant(t *testing.T, d *Driver, ctx provision.Context, path string) {
	t.Helper()
	if err := d.GrantDirectory(ctx, path, false); err != nil {
		t.Fatalf("setup GrantDirectory(%s): %v", path, err)
	}
}

func readRawSettings(t *testing.T, path string) map[string]interface{} {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return raw
}

func readAdditionalDirectoriesFromDisk(t *testing.T, path string) []string {
	t.Helper()
	return readAdditionalDirectories(readRawSettings(t, path))
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/provision/agents/claude/... -run 'TestGrantDirectory|TestRevokeDirectory'`
Expected: FAIL — compile error (`Driver.GrantDirectory` / `Driver.RevokeDirectory` / `readAdditionalDirectories` undefined)

- [ ] **Step 3: Add the `DirectoryGranter` interface**

In `internal/provision/provisioner.go`, immediately after the `AgentDirProvider`/`ResolveAgentDir` block (after line 241, before the `HookInstaller` doc comment at line 243), add:

```go
// DirectoryGranter is implemented by drivers that can add/remove a
// directory from the agent's own permission store — distinct from
// aide's OS-level sandbox, which is what readable_extra/writable_extra
// already control. Optional: drivers that don't implement it are
// skipped via a type assertion, the same pattern as AgentDirProvider.
type DirectoryGranter interface {
	// GrantDirectory adds path to the agent's own allow-list so the
	// agent's internal permission checks (not just the OS sandbox)
	// permit tool access to it. write is passed through for agents
	// whose permission model distinguishes read/write extra paths;
	// implementations that don't make that distinction ignore it.
	GrantDirectory(ctx Context, path string, write bool) error
	// RevokeDirectory removes path, and anything nested under it,
	// from the agent's own allow-list. A parent of path is left
	// untouched. Must be a no-op (nil error) if nothing matches.
	RevokeDirectory(ctx Context, path string) error
}
```

- [ ] **Step 4: Implement the claude driver**

Create `internal/provision/agents/claude/directories.go`:

```go
package claude

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/jskswamy/aide/internal/fsutil"
	"github.com/jskswamy/aide/internal/provision"
)

var _ provision.DirectoryGranter = (*Driver)(nil)

// directorySettingsPath returns the settings.json aide should edit for
// a directory grant. Project-scoped calls (ctx.ProjectRoot set) target
// the project-local settings.local.json, matching how Claude itself
// splits project vs user settings. Global-scoped calls fall back to
// the same profile-aware path hooks/statusline already use.
func directorySettingsPath(ctx provision.Context) string {
	if ctx.ProjectRoot != "" {
		return filepath.Join(ctx.ProjectRoot, ".claude", "settings.local.json")
	}
	return settingsPath(ctx)
}

// GrantDirectory implements provision.DirectoryGranter. write is
// accepted for interface parity but unused: Claude's
// permissions.additionalDirectories does not distinguish read/write
// access.
func (d *Driver) GrantDirectory(ctx provision.Context, path string, _ bool) error {
	settingsFile := directorySettingsPath(ctx)
	raw, err := readSettingsFile(settingsFile)
	if err != nil {
		return err
	}
	dirs := readAdditionalDirectories(raw)
	for _, existing := range dirs {
		if existing == path {
			return nil
		}
	}
	writeAdditionalDirectories(raw, append(dirs, path))
	return writeSettingsFile(settingsFile, raw)
}

// RevokeDirectory implements provision.DirectoryGranter. It removes
// any additionalDirectories entry equal to path or nested under it;
// entries that are merely a parent of path are left untouched.
func (d *Driver) RevokeDirectory(ctx provision.Context, path string) error {
	settingsFile := directorySettingsPath(ctx)
	raw, err := readSettingsFile(settingsFile)
	if err != nil {
		return err
	}
	dirs := readAdditionalDirectories(raw)
	kept := make([]string, 0, len(dirs))
	changed := false
	for _, existing := range dirs {
		if isWithin(path, existing) {
			changed = true
			continue
		}
		kept = append(kept, existing)
	}
	if !changed {
		return nil
	}
	writeAdditionalDirectories(raw, kept)
	return writeSettingsFile(settingsFile, raw)
}

// readAdditionalDirectories returns permissions.additionalDirectories
// as a []string, or nil if unset/malformed.
func readAdditionalDirectories(raw map[string]interface{}) []string {
	perms, _ := raw["permissions"].(map[string]interface{})
	rawDirs, _ := perms["additionalDirectories"].([]interface{})
	out := make([]string, 0, len(rawDirs))
	for _, dv := range rawDirs {
		if s, ok := dv.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// writeAdditionalDirectories sets permissions.additionalDirectories,
// creating the permissions object if needed and preserving its other
// keys (allow, deny, etc.).
func writeAdditionalDirectories(raw map[string]interface{}, dirs []string) {
	perms, ok := raw["permissions"].(map[string]interface{})
	if !ok {
		perms = map[string]interface{}{}
		raw["permissions"] = perms
	}
	list := make([]interface{}, len(dirs))
	for i, dv := range dirs {
		list[i] = dv
	}
	perms["additionalDirectories"] = list
}

// isWithin reports whether child is parent itself or nested under it.
// Uses filepath.Rel the same way pathUnderHome in
// pkg/seatbelt/guards/guard_git_integration.go does, rather than a
// string-prefix check, so a sibling that merely shares a prefix (e.g.
// /foo/bar vs /foo/barbaz) is not misclassified as nested.
func isWithin(parent, child string) bool {
	if parent == child {
		return true
	}
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func writeSettingsFile(path string, raw map[string]interface{}) error {
	data, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return fmt.Errorf("claude settings: marshal: %w", err)
	}
	return fsutil.AtomicWrite(path, data)
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/provision/agents/claude/... -run 'TestGrantDirectory|TestRevokeDirectory' -v`
Expected: PASS (all 9 new tests)

- [ ] **Step 6: Run the full claude package suite to confirm no regression**

Run: `go test ./internal/provision/...`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/provision/provisioner.go internal/provision/agents/claude/directories.go internal/provision/agents/claude/directories_test.go
git commit -m "Add DirectoryGranter and implement it for Claude"
```

---

### Task 3: Wire `--no-agent-grant` into `sandbox allow`/`sandbox deny`

**Files:**
- Create: `cmd/aide/sandbox_agent_grant.go`
- Modify: `cmd/aide/sandbox.go:664-704` (`sandboxAllowCmd`), `cmd/aide/sandbox.go:633-662` (`sandboxDenyCmd`)

**Interfaces:**
- Consumes: `provision.DirectoryGranter` and `*claude.Driver`'s implementation of it (Task 2); `provision.ProvisionerFor(agentName string) (Provisioner, bool)` (existing, `internal/provision/registry.go:21`); `provision.InjectProfileEnv(ctx config.Context, env map[string]string, homeDir string) (map[string]string, error)` (existing, `internal/provision/profile.go:93`); `aidectx.Resolve`, `aidectx.DetectRemote`, `aidectx.ProjectRoot` (existing, already imported in `sandbox.go` as `aidectx`); `config.Load(dir, cwd string) (*config.Config, error)` (existing).
- Produces: `grantAgentDirectory(stdout, stderr io.Writer, global bool, path string, write bool)` and `revokeAgentDirectory(stdout, stderr io.Writer, global bool, path string)` — called from `sandboxAllowCmd`/`sandboxDenyCmd`'s `RunE`. Task 4's tests exercise these indirectly through the `sandbox allow`/`sandbox deny` CLI commands.

There's no new *testable-in-isolation* behavior to TDD here in the narrow sense — the meaningful behavior (does the right file get written) is already covered by Task 2's tests, and this task is wiring. Task 4 writes the failing end-to-end tests first and this task makes them pass, so steps below build the wiring, then Task 4 is the red/green cycle for it. If you're executing tasks in order, skip ahead to Task 4's Step 1 to write the failing tests before Step 1 here — or, if executing strictly in file order, come back to run Task 4's tests after Step 4 below.

- [ ] **Step 1: Create the agent-grant helper file**

Create `cmd/aide/sandbox_agent_grant.go`:

```go
// Package main provides the aide CLI commands.
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/jskswamy/aide/internal/config"
	aidectx "github.com/jskswamy/aide/internal/context"
	"github.com/jskswamy/aide/internal/provision"
)

// resolveAgentGrantContext resolves the current context's agent driver
// and the provision.Context to call it with, scoped to match the
// --global/--context flags already used for the aide-side sandbox
// mutation. ok=false with a nil error means "nothing to do" (agent
// unknown to aide, or it doesn't implement DirectoryGranter) — not a
// failure the caller should report.
func resolveAgentGrantContext(global bool) (granter provision.DirectoryGranter, agentName string, pctx provision.Context, ok bool, err error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, "", provision.Context{}, false, fmt.Errorf("getting working directory: %w", err)
	}
	cfg, err := config.Load(config.Dir(), cwd)
	if err != nil {
		return nil, "", provision.Context{}, false, fmt.Errorf("loading config: %w", err)
	}
	remoteURL := aidectx.DetectRemote(cwd, "origin")
	resolved, err := aidectx.Resolve(cfg, cwd, remoteURL)
	if err != nil {
		return nil, "", provision.Context{}, false, fmt.Errorf("resolving context: %w", err)
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, "", provision.Context{}, false, fmt.Errorf("resolving home directory: %w", err)
	}
	mergedEnv, err := provision.InjectProfileEnv(resolved.Context, resolved.Context.Env, homeDir)
	if err != nil {
		return nil, "", provision.Context{}, false, fmt.Errorf("context %q: %w", resolved.Name, err)
	}
	prov, found := provision.ProvisionerFor(resolved.Context.Agent)
	if !found {
		return nil, resolved.Context.Agent, provision.Context{}, false, nil
	}
	g, ok := prov.(provision.DirectoryGranter)
	if !ok {
		return nil, resolved.Context.Agent, provision.Context{}, false, nil
	}
	pctx = provision.Context{
		Name:    resolved.Name,
		Agent:   resolved.Context.Agent,
		HomeDir: homeDir,
		Env:     mergedEnv,
	}
	if !global {
		pctx.ProjectRoot = aidectx.ProjectRoot(cwd)
	}
	return g, resolved.Context.Agent, pctx, true, nil
}

// grantAgentDirectory best-effort grants path in the current context's
// agent's own permission store, on top of the aide-side sandbox grant
// that has already succeeded by the time this runs. Errors are
// printed as warnings, never returned — the OS-level sandbox grant is
// the security-relevant part and is unaffected by this failing.
func grantAgentDirectory(stdout, stderr io.Writer, global bool, path string, write bool) {
	granter, agentName, pctx, ok, err := resolveAgentGrantContext(global)
	if err != nil {
		fmt.Fprintf(stderr, "warning: could not resolve agent for directory grant: %v\n", err)
		return
	}
	if !ok {
		return
	}
	if err := granter.GrantDirectory(pctx, path, write); err != nil {
		fmt.Fprintf(stderr, "warning: could not add %s to %s's settings: %v\n", path, agentName, err)
		return
	}
	fmt.Fprintf(stdout, "Added %s to %s's additionalDirectories\n", path, agentName)
}

// revokeAgentDirectory is the deny-side counterpart to grantAgentDirectory.
func revokeAgentDirectory(stdout, stderr io.Writer, global bool, path string) {
	granter, agentName, pctx, ok, err := resolveAgentGrantContext(global)
	if err != nil {
		fmt.Fprintf(stderr, "warning: could not resolve agent for directory revoke: %v\n", err)
		return
	}
	if !ok {
		return
	}
	if err := granter.RevokeDirectory(pctx, path); err != nil {
		fmt.Fprintf(stderr, "warning: could not remove %s from %s's settings: %v\n", path, agentName, err)
		return
	}
	fmt.Fprintf(stdout, "Removed %s from %s's additionalDirectories\n", path, agentName)
}
```

- [ ] **Step 2: Wire `sandboxAllowCmd`**

In `cmd/aide/sandbox.go`, replace the current `sandboxAllowCmd` (lines 664-704):

```go
func sandboxAllowCmd() *cobra.Command {
	var contextName string
	var global bool
	var write bool
	var noAgentGrant bool
	cmd := &cobra.Command{
		Use:          "allow <path>",
		Short:        "Add a path to readable_extra or writable_extra (project-level by default)",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			path := args[0]
			listName := "readable_extra"
			if write {
				listName = "writable_extra"
			}
			apply := func(sp *config.SandboxPolicy) {
				if write {
					sp.WritableExtra = append(sp.WritableExtra, path)
				} else {
					sp.ReadableExtra = append(sp.ReadableExtra, path)
				}
			}
			if err := runScopedMutation(cmd.OutOrStdout(), global, contextName, scopedMutation{
				contextMutate: func(ctx *config.Context) error {
					apply(ensureInlineSandbox(ctx))
					return nil
				},
				projectMutate: func(po *config.ProjectOverride) error {
					apply(ensureProjectSandbox(po))
					return nil
				},
				successGlobal:  func(ctxName string) string { return fmt.Sprintf("Added %s to %s for context %q (global)", path, listName, ctxName) },
				successProject: func(poPath string) string { return fmt.Sprintf("Added %s to %s in project (%s)", path, listName, poPath) },
			}); err != nil {
				return err
			}
			if !noAgentGrant {
				grantAgentDirectory(cmd.OutOrStdout(), cmd.ErrOrStderr(), global, path, write)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&global, "global", false, "Apply to user-level config instead of project")
	cmd.Flags().StringVar(&contextName, "context", "", "Target context name (requires --global)")
	cmd.Flags().BoolVar(&write, "write", false, "add to writable_extra instead of readable_extra")
	cmd.Flags().BoolVar(&noAgentGrant, "no-agent-grant", false, "skip granting this path in the agent's own permission store")
	return cmd
}
```

This adds one closure variable (`noAgentGrant`, declared alongside `contextName`/`global`/`write`), one new flag registration, and the post-mutation `if !noAgentGrant { grantAgentDirectory(...) }` block.

- [ ] **Step 3: Wire `sandboxDenyCmd`**

In `cmd/aide/sandbox.go`, replace the current `sandboxDenyCmd` (lines 633-662):

```go
func sandboxDenyCmd() *cobra.Command {
	var contextName string
	var global bool
	var noAgentGrant bool
	cmd := &cobra.Command{
		Use:          "deny <path>",
		Short:        "Add a path to the denied_extra list (project-level by default)",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			path := args[0]
			if err := runScopedMutation(cmd.OutOrStdout(), global, contextName, scopedMutation{
				contextMutate: func(ctx *config.Context) error {
					sp := ensureInlineSandbox(ctx)
					sp.DeniedExtra = append(sp.DeniedExtra, path)
					return nil
				},
				projectMutate: func(po *config.ProjectOverride) error {
					sp := ensureProjectSandbox(po)
					sp.DeniedExtra = append(sp.DeniedExtra, path)
					return nil
				},
				successGlobal:  func(ctxName string) string { return fmt.Sprintf("Added %s to denied_extra for context %q (global)", path, ctxName) },
				successProject: func(poPath string) string { return fmt.Sprintf("Added %s to denied_extra in project (%s)", path, poPath) },
			}); err != nil {
				return err
			}
			if !noAgentGrant {
				revokeAgentDirectory(cmd.OutOrStdout(), cmd.ErrOrStderr(), global, path)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&global, "global", false, "Apply to user-level config instead of project")
	cmd.Flags().StringVar(&contextName, "context", "", "Target context name (requires --global)")
	cmd.Flags().BoolVar(&noAgentGrant, "no-agent-grant", false, "skip revoking this path in the agent's own permission store")
	return cmd
}
```

Note: `sandboxDenyCmd` declares its own separate `var noAgentGrant bool`, just like `sandboxAllowCmd` does in Step 2 — each cobra command function has its own closure-local flag variables (this mirrors how `contextName`/`global` are already independently declared per-command in this file, not shared).

- [ ] **Step 4: Build to confirm it compiles**

Run: `go build ./...`
Expected: success (no output)

Do not commit yet — Task 4 adds the tests that exercise this wiring. Commit at the end of Task 4 once both the wiring and its tests are in place and green.

---

### Task 4: End-to-end tests for the CLI wiring

**Files:**
- Create: `cmd/aide/sandbox_test.go`

**Interfaces:**
- Consumes: `sandboxCmd()` (existing, `cmd/aide/sandbox.go:23`); `isolatedConfigDir(t *testing.T) string` (existing test helper, `cmd/aide/context_bind_test.go:27` — sets `HOME`, `XDG_CONFIG_HOME`, chdirs to a fresh tempdir); the `claude` and `codex` drivers, both already registered via blank import in `cmd/aide/provision_drivers.go`.
- Produces: nothing further downstream — this is the last task.

- [ ] **Step 1: Write the failing tests**

Create `cmd/aide/sandbox_test.go`:

```go
package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runSandboxCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var buf bytes.Buffer
	cmd := sandboxCmd()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return buf.String(), err
}

// writeClaudeAgentConfig writes a minimal global config.yaml declaring
// one context ("work", the default) using the given agent name.
func writeClaudeAgentConfig(t *testing.T, dir, agent string) {
	t.Helper()
	yaml := "default_context: work\ncontexts:\n  work:\n    agent: " + agent + "\n"
	if err := os.WriteFile(filepath.Join(dir, "xdg", "aide", "config.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readAdditionalDirs(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	perms, _ := raw["permissions"].(map[string]interface{})
	rawDirs, _ := perms["additionalDirectories"].([]interface{})
	out := make([]string, 0, len(rawDirs))
	for _, d := range rawDirs {
		if s, ok := d.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func TestSandboxAllow_GrantsClaudeProjectDirectory(t *testing.T) {
	dir := isolatedConfigDir(t)
	writeClaudeAgentConfig(t, dir, "claude")

	out, err := runSandboxCmd(t, "allow", "/repo/extra")
	if err != nil {
		t.Fatalf("sandbox allow: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Added /repo/extra to claude's additionalDirectories") {
		t.Errorf("missing agent-grant confirmation; got:\n%s", out)
	}

	got := readAdditionalDirs(t, filepath.Join(dir, ".claude", "settings.local.json"))
	if len(got) != 1 || got[0] != "/repo/extra" {
		t.Fatalf("additionalDirectories = %v, want [/repo/extra]", got)
	}
}

func TestSandboxAllow_GlobalScope_WritesUserSettingsNotProject(t *testing.T) {
	dir := isolatedConfigDir(t)
	writeClaudeAgentConfig(t, dir, "claude")

	out, err := runSandboxCmd(t, "allow", "/repo/extra", "--global")
	if err != nil {
		t.Fatalf("sandbox allow: %v\n%s", err, out)
	}

	got := readAdditionalDirs(t, filepath.Join(dir, ".claude", "settings.json"))
	if len(got) != 1 || got[0] != "/repo/extra" {
		t.Fatalf("additionalDirectories = %v, want [/repo/extra]", got)
	}
	if _, statErr := os.Stat(filepath.Join(dir, ".claude", "settings.local.json")); statErr == nil {
		t.Errorf("global grant should not touch project settings.local.json")
	}
}

func TestSandboxAllow_NoAgentGrantFlagSkipsClaudeWrite(t *testing.T) {
	dir := isolatedConfigDir(t)
	writeClaudeAgentConfig(t, dir, "claude")

	out, err := runSandboxCmd(t, "allow", "/repo/extra", "--no-agent-grant")
	if err != nil {
		t.Fatalf("sandbox allow: %v\n%s", err, out)
	}
	if strings.Contains(out, "additionalDirectories") {
		t.Errorf("expected no agent-grant output with --no-agent-grant; got:\n%s", out)
	}
	if _, statErr := os.Stat(filepath.Join(dir, ".claude", "settings.local.json")); statErr == nil {
		t.Errorf("--no-agent-grant should not create Claude's settings.local.json")
	}
}

func TestSandboxAllow_UnsupportedAgentSkipsSilently(t *testing.T) {
	dir := isolatedConfigDir(t)
	writeClaudeAgentConfig(t, dir, "codex")

	out, err := runSandboxCmd(t, "allow", "/repo/extra")
	if err != nil {
		t.Fatalf("sandbox allow: %v\n%s", err, out)
	}
	if strings.Contains(out, "additionalDirectories") {
		t.Errorf("codex has no DirectoryGranter; expected no agent-grant output, got:\n%s", out)
	}
}

func TestSandboxDeny_RevokesClaudeProjectDirectory(t *testing.T) {
	dir := isolatedConfigDir(t)
	writeClaudeAgentConfig(t, dir, "claude")

	if out, err := runSandboxCmd(t, "allow", "/repo/extra/sub"); err != nil {
		t.Fatalf("sandbox allow: %v\n%s", err, out)
	}

	out, err := runSandboxCmd(t, "deny", "/repo/extra")
	if err != nil {
		t.Fatalf("sandbox deny: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Removed /repo/extra from claude's additionalDirectories") {
		t.Errorf("missing agent-revoke confirmation; got:\n%s", out)
	}

	got := readAdditionalDirs(t, filepath.Join(dir, ".claude", "settings.local.json"))
	if len(got) != 0 {
		t.Fatalf("additionalDirectories = %v, want empty (nested grant should be revoked)", got)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./cmd/aide/... -run 'TestSandboxAllow|TestSandboxDeny' -v`
Expected: FAIL — either a compile error (if Task 3 wasn't done yet) or assertion failures (missing `additionalDirectories` output/files) if Task 3's wiring isn't present.

If Task 3 has already been completed (as laid out above), these should mostly PASS already except for wording — check the exact confirmation string. `grantAgentDirectory`/`revokeAgentDirectory` in Task 3 use `agentName` from `resolved.Context.Agent`, which is the raw config value (`"claude"`, lowercase) — the assertions above use `claude's additionalDirectories` (lowercase) to match. If a test fails only on this string, that's the expected first-run signal to confirm the wiring is live; fix the test or the format string so they agree, then re-run.

- [ ] **Step 3: Make any remaining fixes so the tests pass**

If Task 3 was completed first, this step is just running the tests and confirming green. If not, go back and complete Task 3's Steps 1-4 now.

Run: `go test ./cmd/aide/... -run 'TestSandboxAllow|TestSandboxDeny' -v`
Expected: PASS (all 5 tests)

- [ ] **Step 4: Run the full test suite**

Run: `go test ./...`
Expected: PASS (no regressions anywhere)

- [ ] **Step 5: Commit**

```bash
git add cmd/aide/sandbox_agent_grant.go cmd/aide/sandbox.go cmd/aide/sandbox_test.go
git commit -m "Sync directory grants to Claude on sandbox allow/deny"
```
