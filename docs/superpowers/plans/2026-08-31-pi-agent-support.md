# Pi Agent Support Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `provision.Provisioner` driver for Pi (`earendil-works/pi`, pi.dev) so `aide` can sandbox it, manage its extensions ("plugins"), and install hooks into it. MCP is explicitly out of scope — Pi has no first-party MCP support.

**Architecture:** A new `internal/provision/agents/pi` package following the exact shape of the existing `claude`/`gemini` drivers (`DriverBase` embedding, `init()` self-registration, `AgentDirProvider` for its `PI_CODING_AGENT_DIR` env var). Plugins are CLI-driven — Pi has a full, confirmed `pi install`/`pi remove`/`pi list` triad, unlike OpenCode. Hooks are generated TypeScript extension files dropped in `.pi/agent/extensions/` (global scope, matching every other hook-capable driver in this codebase), using the same generic `HookCodec`/`ReadHooks`/`WriteHooks` machinery `gemini` and the OpenCode driver use. A new seatbelt module handles sandboxing.

**Tech Stack:** Go, the existing `internal/provision` and `pkg/seatbelt` packages. No new dependencies.

## Global Constraints

- Follow the existing `provision.Provisioner` driver pattern exactly: embed `provision.DriverBase`, construct `Capabilities` in `New()`, self-register via `init()` calling `provision.RegisterProvisioner(New(provision.ExecRunner{}))`.
- Hooks are global-scope only (`~/.pi/agent/extensions/` or the `PI_CODING_AGENT_DIR`-overridden equivalent), matching every existing hook-capable driver — none of them write project-scope hooks today.
- Plugin operations (install/remove/list) always target user-level scope (no `-l` flag) — no existing driver in this codebase distinguishes project vs. global scope for plugin install, so this driver doesn't introduce that split either.
- Every `pi` invocation passes `--no-approve` explicitly so it never blocks on the project-trust prompt regardless of the working directory aide happens to invoke it from — this is what makes `RequiresTTY: false` actually true.
- All file writes use `fsutil.AtomicWrite` (never `os.WriteFile` directly), matching every existing driver.
- No new dependencies — everything here is either a shell-out (`provision.Runner`) or a generated static file.

---

### Task 1: Pi seatbelt sandbox module

**Files:**
- Create: `pkg/seatbelt/modules/pi.go`
- Modify: `pkg/seatbelt/modules/agents_test.go` (append test cases to the existing `TestAgentModules` table)
- Modify: `internal/launcher/agentcfg.go`

**Interfaces:**
- Produces: `modules.PiAgent() seatbelt.Module`

- [ ] **Step 1: Write the failing test**

Open `pkg/seatbelt/modules/agents_test.go` and add these two entries to the `tests` slice (insert after the last `"Copilot ..."` case, before the `"Cursor ..."` cases, keeping the loose alphabetical grouping the file already has):

```go
		{
			name:     "Pi defaults",
			module:   PiAgent(),
			wantName: "Pi Agent",
			wantContain: []string{
				"/home/user/.pi",
			},
		},
		{
			name:     "Pi env override",
			module:   PiAgent(),
			wantName: "Pi Agent",
			env:      []string{"PI_CODING_AGENT_DIR=/custom/pi-agent"},
			wantContain: []string{
				"/custom/pi-agent",
			},
			wantAbsent: []string{
				"/home/user/.pi",
			},
		},
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/seatbelt/modules/... -run TestAgentModules -v`
Expected: FAIL with `undefined: PiAgent`

- [ ] **Step 3: Write minimal implementation**

Create `pkg/seatbelt/modules/pi.go`:

```go
package modules

import "github.com/jskswamy/aide/pkg/seatbelt"

// PiAgent returns a module with Pi CLI agent sandbox rules.
// PI_CODING_AGENT_DIR is a confirmed, documented env var overriding
// Pi's whole config directory (default ~/.pi/agent — confirmed via
// `pi --help`'s Environment Variables section). The home-relative
// default is the parent ".pi" directory rather than ".pi/agent"
// itself, in case other ".pi/*" subdirectories exist that the env var
// doesn't cover (Pi was observed writing a lock file directly under
// ~/.pi/agent/ on every invocation, even a plain --help).
func PiAgent() seatbelt.Module {
	return NewSimpleAgent(AgentSpec{
		DisplayName:     "Pi Agent",
		SectionName:     "Pi",
		EnvKey:          "PI_CODING_AGENT_DIR",
		HomeRelDefaults: []string{".pi"},
	})
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/seatbelt/modules/... -run TestAgentModules -v`
Expected: PASS

- [ ] **Step 5: Wire into the agent module resolver**

In `internal/launcher/agentcfg.go`, add `"pi": modules.PiAgent,` to the `agentModuleResolvers` map (alphabetical order — after `"opencode"` if Task 5 of the OpenCode plan has already landed, otherwise after `"goose"`):

```go
var agentModuleResolvers = map[string]func() seatbelt.Module{
	"aider":        modules.AiderAgent,
	"amp":          modules.AmpAgent,
	"claude":       modules.ClaudeAgent,
	"codex":        modules.CodexAgent,
	"copilot":      modules.CopilotAgent,
	"cursor-agent": modules.CursorAgent,
	"gemini":       modules.GeminiAgent,
	"goose":        modules.GooseAgent,
	"pi":           modules.PiAgent,
}
```

- [ ] **Step 6: Run the full package tests**

Run: `go test ./pkg/seatbelt/... ./internal/launcher/... -v`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add pkg/seatbelt/modules/pi.go pkg/seatbelt/modules/agents_test.go internal/launcher/agentcfg.go
git commit -m "Add Pi seatbelt sandbox module"
```

---

### Task 2: Pi driver core — capabilities, AgentDir, plugins

**Files:**
- Create: `internal/provision/agents/pi/pi.go`
- Create: `internal/provision/agents/pi/pi_test.go`

**Interfaces:**
- Consumes: `provision.Plugin{Key, Source, Name}`, `provision.DriverBase`, `provision.Capabilities`, `provision.Runner`, `provision.RunCLI`, `provision.DefaultTolerateStderr`, `provision.AgentDirProvider` interface (`AgentDir(ctx Context) string`)
- Produces: `pi.New(r provision.Runner) *pi.Driver`; `(*Driver).AgentDir(ctx provision.Context) string` (Task 3's `piExtensionsDir` duplicates this same `PI_CODING_AGENT_DIR`-then-`.pi/agent`-fallback logic inline rather than calling the method, matching how claude's `hooks.go:settingsPath` duplicates `AgentDir`'s logic instead of calling it — the established pattern in this codebase, not an oversight)

- [ ] **Step 1: Write the failing tests**

Create `internal/provision/agents/pi/pi_test.go`:

```go
package pi_test

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/jskswamy/aide/internal/provision"
	pidriver "github.com/jskswamy/aide/internal/provision/agents/pi"
)

// fakeRunner records calls and returns scripted output.
type fakeRunner struct {
	stdout string
	stderr string
	code   int
	err    error
	calls  [][]string
}

func (f *fakeRunner) Run(_ context.Context, _ map[string]string, name string, args ...string) (string, string, int, error) {
	f.calls = append(f.calls, append([]string{name}, args...))
	return f.stdout, f.stderr, f.code, f.err
}

func TestPiCapabilities(t *testing.T) {
	d := pidriver.New(&fakeRunner{})
	if d.Name() != "pi" {
		t.Errorf("Name = %q", d.Name())
	}
	if !d.SupportsPlugins() || !d.SupportsHooks() {
		t.Error("Pi should support plugins and hooks")
	}
	if d.SupportsMCP() {
		t.Error("Pi should not support MCP — no first-party support exists")
	}
	if d.RequiresTTY() {
		t.Error("Pi should not require TTY")
	}
	shapes := d.SupportedSourceShapes()
	if len(shapes) != 1 || shapes[0] != provision.ShapeURLDirect {
		t.Errorf("Pi shapes = %v, want [url-direct]", shapes)
	}
}

func TestPiAgentDirDefault(t *testing.T) {
	d := pidriver.New(&fakeRunner{})
	ctx := provision.Context{HomeDir: "/home/u"}
	want := filepath.Join("/home/u", ".pi", "agent")
	if got := d.AgentDir(ctx); got != want {
		t.Errorf("AgentDir = %q, want %q", got, want)
	}
}

func TestPiAgentDirEnvOverride(t *testing.T) {
	d := pidriver.New(&fakeRunner{})
	ctx := provision.Context{HomeDir: "/home/u", Env: map[string]string{"PI_CODING_AGENT_DIR": "/custom/pi-agent"}}
	if got := d.AgentDir(ctx); got != "/custom/pi-agent" {
		t.Errorf("AgentDir = %q, want /custom/pi-agent", got)
	}
}

func TestPiInstallPlugin(t *testing.T) {
	r := &fakeRunner{}
	d := pidriver.New(r)
	if err := d.InstallPlugin(provision.Context{}, provision.Plugin{Key: "x", Name: "npm:@foo/bar"}); err != nil {
		t.Fatal(err)
	}
	want := []string{"pi", "install", "npm:@foo/bar", "--no-approve"}
	if !reflect.DeepEqual(r.calls[0], want) {
		t.Errorf("call = %v, want %v", r.calls[0], want)
	}
}

func TestPiInstallPluginFailure(t *testing.T) {
	r := &fakeRunner{code: 1, stderr: "boom"}
	d := pidriver.New(r)
	if err := d.InstallPlugin(provision.Context{}, provision.Plugin{Key: "x", Name: "npm:@foo/bar"}); err == nil {
		t.Fatal("expected error on non-zero exit")
	}
}

func TestPiUninstallPlugin(t *testing.T) {
	r := &fakeRunner{}
	d := pidriver.New(r)
	if err := d.UninstallPlugin(provision.Context{}, "npm:@foo/bar"); err != nil {
		t.Fatal(err)
	}
	want := []string{"pi", "remove", "npm:@foo/bar", "--no-approve"}
	if !reflect.DeepEqual(r.calls[0], want) {
		t.Errorf("call = %v, want %v", r.calls[0], want)
	}
}

// TestPiUninstallMissingIsOK pins the tolerated-stderr fix: Pi's own
// wording ("No matching package found for X") does NOT contain any
// substring in provision.DefaultTolerateStderr ("not installed", "not
// found", "not configured") — confirmed 2026-08-31 by running `pi
// remove` against a package that was never installed. Without the
// extra tolerated substring below, this test fails.
func TestPiUninstallMissingIsOK(t *testing.T) {
	r := &fakeRunner{code: 1, stderr: "No matching package found for npm:@nonexistent/pkg"}
	d := pidriver.New(r)
	if err := d.UninstallPlugin(provision.Context{}, "npm:@nonexistent/pkg"); err != nil {
		t.Errorf("missing package should be treated as success: %v", err)
	}
}

// TestPiInstalledPluginsParsesTwoLinePerEntryOutput pins the real `pi
// list` shape confirmed 2026-08-31 by installing a real local
// extension against an isolated $HOME: each entry is TWO lines — a
// 2-space-indented declared source, then a 4-space-indented resolved
// absolute path. provision.ParsePluginList (one entry per line) can't
// parse this; the Pi driver needs its own two-line-aware parser.
func TestPiInstalledPluginsParsesTwoLinePerEntryOutput(t *testing.T) {
	stdout := "User packages:\n" +
		"  npm:@foo/bar\n" +
		"    /home/u/.pi/agent/packages/npm/@foo/bar\n" +
		"  ../../../dummy_ext\n" +
		"    /private/tmp/scratch/dummy_ext\n"
	r := &fakeRunner{stdout: stdout}
	d := pidriver.New(r)
	got, err := d.InstalledPlugins(provision.Context{})
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, p := range got {
		names = append(names, p.Name)
	}
	want := []string{"npm:@foo/bar", "../../../dummy_ext"}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("names = %v, want %v", names, want)
	}
}

// TestPiInstalledPluginsEmptyState pins the real empty-state message
// confirmed 2026-08-31: `pi list` on a fresh $HOME prints exactly
// "No packages installed." (not the "NAME"/"No plugins" style header
// gemini/copilot use).
func TestPiInstalledPluginsEmptyState(t *testing.T) {
	r := &fakeRunner{stdout: "No packages installed.\n"}
	d := pidriver.New(r)
	got, err := d.InstalledPlugins(provision.Context{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty, got %+v", got)
	}
}

func TestPiInstalledPluginsBinaryMissing(t *testing.T) {
	r := &fakeRunner{err: context.DeadlineExceeded}
	d := pidriver.New(r)
	got, err := d.InstalledPlugins(provision.Context{})
	if err != nil || len(got) != 0 {
		t.Errorf("binary-missing should collapse to (nil, nil), got %v, %v", got, err)
	}
}

func TestPiMarketplaceMethodsNoOp(t *testing.T) {
	d := pidriver.New(&fakeRunner{})
	got, err := d.InstalledMarketplaces(provision.Context{})
	if err != nil || len(got) != 0 {
		t.Errorf("InstalledMarketplaces should be no-op, got %v, %v", got, err)
	}
	if err := d.RemoveMarketplace(provision.Context{}, "anything"); err != nil {
		t.Errorf("RemoveMarketplace should be no-op, got %v", err)
	}
	if err := d.AddMarketplace(provision.Context{}, provision.Marketplace{}); err == nil {
		t.Error("AddMarketplace should error — Pi has no marketplace concept")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/provision/agents/pi/... -v`
Expected: FAIL with `no such package` / `undefined: pi.New` (note: the test file imports the package as `pidriver` to avoid colliding with the standard-library-adjacent name `pi` as a local identifier — keep that alias)

- [ ] **Step 3: Write the implementation**

Create `internal/provision/agents/pi/pi.go`:

```go
// Package pi provides the provision.Provisioner driver for Pi
// (`earendil-works/pi`, pi.dev). See
// docs/superpowers/specs/2026-08-31-opencode-pi-agent-support-design.md
// for the capabilities verified directly against the binary
// (nixpkgs package `pi-coding-agent`, homepage confirmed as
// https://pi.dev/ via `nix eval nixpkgs#pi-coding-agent.meta.homepage`).
//
// MCP is intentionally unsupported: Pi ships no first-party MCP
// client, only third-party extensions with config formats
// earendil-works doesn't own.
package pi

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/jskswamy/aide/internal/provision"
)

const agentName = "pi"

// Driver implements provision.Provisioner for Pi. Capability stub
// methods are promoted from DriverBase.
type Driver struct {
	provision.DriverBase
	runner provision.Runner
}

// New returns a Driver using the supplied Runner. Pass
// provision.ExecRunner{} in production.
func New(r provision.Runner) *Driver {
	return &Driver{
		DriverBase: provision.DriverBase{Caps: provision.Capabilities{
			AgentName:       agentName,
			SupportsPlugins: true,
			RequiresTTY:     false,
			SourceShapes:    []provision.SourceShape{provision.ShapeURLDirect},
			SupportsHooks:   true,
			ProfileEnvKey:   "PI_CODING_AGENT_DIR",
		}},
		runner: r,
	}
}

func init() {
	provision.RegisterProvisioner(New(provision.ExecRunner{}))
}

var _ provision.AgentDirProvider = (*Driver)(nil)

// AgentDir returns the absolute config directory for this context.
// PI_CODING_AGENT_DIR is a confirmed env var (see `pi --help`'s
// Environment Variables section); its documented default is
// ~/.pi/agent.
func (d *Driver) AgentDir(ctx provision.Context) string {
	if dir, ok := ctx.Env["PI_CODING_AGENT_DIR"]; ok && dir != "" {
		return dir
	}
	return filepath.Join(ctx.HomeDir, ".pi", "agent")
}

// piNotFoundStderr is appended to provision.DefaultTolerateStderr for
// UninstallPlugin. Confirmed 2026-08-31: `pi remove` on a package that
// was never installed prints "No matching package found for X" (exit
// 1), which does not contain any of DefaultTolerateStderr's substrings
// ("not installed", "not found", "not configured").
const piNotFoundStderr = "No matching package found"

// InstallPlugin invokes `pi install <source> --no-approve`. The
// --no-approve flag is always passed so this never blocks on Pi's
// project-trust prompt regardless of aide's working directory — see
// the Global Constraints note on RequiresTTY.
func (d *Driver) InstallPlugin(ctx provision.Context, p provision.Plugin) error {
	return provision.RunCLI(context.Background(), d.runner, ctx.Env,
		"pi install "+p.Name,
		"pi", []string{"install", p.Name, "--no-approve"})
}

// UninstallPlugin invokes `pi remove <source> --no-approve`. Tolerates
// the standard rollback-safety stderr substrings plus Pi's own
// not-installed wording.
func (d *Driver) UninstallPlugin(ctx provision.Context, name string) error {
	tolerate := append(append([]string{}, provision.DefaultTolerateStderr...), piNotFoundStderr)
	return provision.RunCLI(context.Background(), d.runner, ctx.Env,
		"pi remove "+name,
		"pi", []string{"remove", name, "--no-approve"},
		tolerate...)
}

// InstalledPlugins shells out to `pi list --no-approve` and parses its
// two-line-per-entry output (confirmed 2026-08-31 by installing a real
// local extension against an isolated $HOME):
//
//	User packages:
//	  <declared source>
//	    <resolved absolute path>
//
// provision.ParsePluginList assumes one entry per line and can't be
// reused — this driver has its own parser. Binary-missing (Runner
// error) collapses to (nil, nil), matching the InstalledPlugins
// convention used by claude/gemini.
func (d *Driver) InstalledPlugins(ctx provision.Context) ([]provision.Plugin, error) {
	stdout, stderr, code, err := d.runner.Run(context.Background(), ctx.Env, "pi", "list", "--no-approve")
	if err != nil {
		return nil, nil
	}
	if code != 0 {
		return nil, fmt.Errorf("pi list: exit %d: %s", code, strings.TrimSpace(stderr))
	}
	return parsePiPackageList(stdout), nil
}

// parsePiPackageList extracts plugin entries from `pi list` output.
// A declared-source line has exactly two leading spaces; the resolved
// absolute path on the following line (four leading spaces) is
// discarded — aide only needs the declared source string back, since
// that's what InstallPlugin/UninstallPlugin take. Section headers
// ("User packages:", "Project packages:") have no leading spaces and
// are skipped; the empty-state message ("No packages installed.") has
// no leading spaces either and is likewise skipped.
func parsePiPackageList(out string) []provision.Plugin {
	var plugins []provision.Plugin
	for _, line := range strings.Split(out, "\n") {
		if !strings.HasPrefix(line, "  ") || strings.HasPrefix(line, "    ") {
			continue
		}
		source := strings.TrimSpace(line)
		if source == "" {
			continue
		}
		plugins = append(plugins, provision.Plugin{Key: source, Name: source})
	}
	return plugins
}

// InstalledMarketplaces is a no-op: Pi has no marketplace concept.
func (*Driver) InstalledMarketplaces(_ provision.Context) ([]provision.Marketplace, error) {
	return nil, nil
}

// AddMarketplace returns an error: Pi packages are declared as
// URL-direct string entries (npm:/git:/local path), not marketplaces.
func (*Driver) AddMarketplace(_ provision.Context, _ provision.Marketplace) error {
	return fmt.Errorf("pi does not have marketplaces; declare packages inline with string values")
}

// RemoveMarketplace is a no-op for rollback safety.
func (*Driver) RemoveMarketplace(_ provision.Context, _ string) error {
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/provision/agents/pi/... -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/provision/agents/pi/pi.go internal/provision/agents/pi/pi_test.go
git commit -m "Add Pi driver core: capabilities, AgentDir, plugins"
```

---

### Task 3: Pi hooks

**Files:**
- Create: `internal/provision/agents/pi/hooks.go`
- Create: `internal/provision/agents/pi/hooks_test.go`
- Modify: `internal/provision/hook_artifact.go` (add `PiHookArtifact`)

**Interfaces:**
- Consumes: `provision.HookCodec` interface, `provision.ReadHooks`/`provision.WriteHooks`, `provision.HookArtifact{Prefix, Ext}`, `provision.ValidateHookCommand`, `provision.ReverseLookup`, `(*Driver).AgentDir(ctx) string` (Task 2)
- Produces: `(*Driver).ReadHooks(ctx) ([]provision.Hook, error)`, `(*Driver).WriteHooks(ctx, prevManaged, desired) error` — completes `provision.HookInstaller` for `pi.Driver`

- [ ] **Step 1: Add the PiHookArtifact constant**

In `internal/provision/hook_artifact.go`, add to the `var` block (after `OpenCodeHookArtifact`, or after `HermesHookArtifact` if the OpenCode plan hasn't landed yet — either order is fine, this is an independent addition to the same var block):

```go
	// PiHookArtifact defines Pi's hook artifact shape: aide-<hash>.ts
	// files, dropped in ~/.pi/agent/extensions/ (or the
	// PI_CODING_AGENT_DIR-overridden equivalent) where Pi auto-discovers
	// them, per Pi's bundled extensions.md doc: "~/.pi/agent/extensions/*.ts
	// | Global (all projects)".
	PiHookArtifact = HookArtifact{Prefix: "aide-", Ext: ".ts"}
```

- [ ] **Step 2: Write the failing tests**

Create `internal/provision/agents/pi/hooks_test.go`:

```go
package pi_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/provision"
	pidriver "github.com/jskswamy/aide/internal/provision/agents/pi"
)

func TestPiWriteHooksThenRead(t *testing.T) {
	home := t.TempDir()
	ctx := provision.Context{HomeDir: home}
	d := pidriver.New(&fakeRunner{})

	hooks := []provision.Hook{
		{Event: "pre_tool", Command: "rtk hook pi"},
	}
	if err := d.WriteHooks(ctx, nil, hooks); err != nil {
		t.Fatal(err)
	}
	got, err := d.ReadHooks(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Command != "rtk hook pi" || got[0].Event != "pre_tool" {
		t.Errorf("ReadHooks = %+v", got)
	}

	entries, _ := os.ReadDir(filepath.Join(home, ".pi", "agent", "extensions"))
	hasExtension := false
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "aide-") && strings.HasSuffix(e.Name(), ".ts") {
			hasExtension = true
		}
	}
	if !hasExtension {
		t.Error("expected aide-*.ts extension file in ~/.pi/agent/extensions/")
	}
}

func TestPiWriteHooksUsesAgentDirOverride(t *testing.T) {
	home := t.TempDir()
	customDir := filepath.Join(home, "custom-pi-agent")
	ctx := provision.Context{HomeDir: home, Env: map[string]string{"PI_CODING_AGENT_DIR": customDir}}
	d := pidriver.New(&fakeRunner{})

	if err := d.WriteHooks(ctx, nil, []provision.Hook{{Event: "pre_tool", Command: "rtk hook pi"}}); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(customDir, "extensions"))
	if err != nil {
		t.Fatalf("expected extensions dir under PI_CODING_AGENT_DIR override: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("expected 1 extension file, found %d", len(entries))
	}
}

func TestPiWriteHooksRejectsMetacharacters(t *testing.T) {
	home := t.TempDir()
	ctx := provision.Context{HomeDir: home}
	d := pidriver.New(&fakeRunner{})

	err := d.WriteHooks(ctx, nil, []provision.Hook{{Event: "pre_tool", Command: "rtk hook; rm -rf ~"}})
	if err == nil {
		t.Error("expected error for command containing shell metacharacters")
	}
}

func TestPiWriteHooksClearsPrevious(t *testing.T) {
	home := t.TempDir()
	ctx := provision.Context{HomeDir: home}
	d := pidriver.New(&fakeRunner{})

	_ = d.WriteHooks(ctx, nil, []provision.Hook{{Event: "pre_tool", Command: "old-hook"}})
	_ = d.WriteHooks(ctx, nil, []provision.Hook{{Event: "pre_tool", Command: "new-hook"}})

	got, _ := d.ReadHooks(ctx)
	if len(got) != 1 || got[0].Command != "new-hook" {
		t.Errorf("ReadHooks = %+v, want [new-hook]", got)
	}
}

func TestPiPostToolEventMapsCorrectly(t *testing.T) {
	home := t.TempDir()
	ctx := provision.Context{HomeDir: home}
	d := pidriver.New(&fakeRunner{})

	if err := d.WriteHooks(ctx, nil, []provision.Hook{{Event: "post_tool", Command: "rtk hook post"}}); err != nil {
		t.Fatal(err)
	}
	got, err := d.ReadHooks(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Event != "post_tool" {
		t.Errorf("ReadHooks = %+v, want event=post_tool", got)
	}
}

func TestPiWriteHooksSkipsUnsupportedEventSilently(t *testing.T) {
	home := t.TempDir()
	ctx := provision.Context{HomeDir: home}
	d := pidriver.New(&fakeRunner{})

	if err := d.WriteHooks(ctx, nil, []provision.Hook{{Event: "totally_unknown_event", Command: "noop"}}); err != nil {
		t.Fatalf("unsupported event should be skipped silently, got %v", err)
	}
	got, err := d.ReadHooks(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("expected no hooks written for an unsupported event, got %+v", got)
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/provision/agents/pi/... -run TestPiWriteHooks -v`
Expected: FAIL — `d.WriteHooks`/`d.ReadHooks` undefined on `*pi.Driver`

- [ ] **Step 4: Write the implementation**

Create `internal/provision/agents/pi/hooks.go`:

```go
package pi

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jskswamy/aide/internal/fsutil"
	"github.com/jskswamy/aide/internal/provision"
)

// piEventMap maps aide's normalized hook events to Pi's native
// extension event names, confirmed directly against Pi's bundled
// extensions.md doc: "tool_call" fires before a tool executes and can
// block it (event.toolName, event.input; return { block, reason } to
// block — the direct PreToolUse equivalent); "tool_result" fires
// after execution and can modify the result (the direct PostToolUse
// equivalent). session_start/session_end have no confirmed
// one-to-one native mapping (the closest is "session_start" itself,
// but there's no single symmetric "session_end" — only
// "session_shutdown", which fires on session switch, not process
// exit) — left unmapped for now; WriteHooks skips unmapped events
// silently rather than guessing at an unverified mapping.
var piEventMap = map[string]string{
	"pre_tool":  "tool_call",
	"post_tool": "tool_result",
}

func piExtensionsDir(ctx provision.Context) string {
	agentDir := ctx.Env["PI_CODING_AGENT_DIR"]
	if agentDir == "" {
		agentDir = filepath.Join(ctx.HomeDir, ".pi", "agent")
	}
	return filepath.Join(agentDir, "extensions")
}

// piHookCodec implements provision.HookCodec for Pi's aide-*.ts
// extension files. Like the OpenCode codec, the generated file's real
// behavior lives in a callback that's impractical to parse back out —
// Decode instead reads two `// aide-*:` comment lines carrying the
// canonical (event, command) pair.
type piHookCodec struct{}

func (c *piHookCodec) Match(name string) bool {
	return provision.PiHookArtifact.Owns(name)
}

func (c *piHookCodec) Decode(path string) (provision.Hook, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return provision.Hook{}, err
	}
	var command, event string
	for _, line := range strings.Split(string(data), "\n") {
		switch {
		case strings.HasPrefix(line, "// aide-event: "):
			native := strings.TrimPrefix(line, "// aide-event: ")
			event = provision.ReverseLookup(piEventMap, native, native)
		case strings.HasPrefix(line, "// aide-command: "):
			command = strings.TrimPrefix(line, "// aide-command: ")
		}
	}
	return provision.Hook{Event: event, Command: command}, nil
}

func (c *piHookCodec) Encode(dir string, h provision.Hook) error {
	nativeEvent, ok := piEventMap[h.Event]
	if !ok {
		return nil // unsupported event — skip silently
	}
	if err := provision.ValidateHookCommand(h.Command); err != nil {
		return fmt.Errorf("pi hooks: %w", err)
	}
	name := provision.PiHookArtifact.Name(h.Command)
	// Extension shape confirmed against Pi's bundled extensions.md:
	// `export default function (pi: ExtensionAPI) { pi.on(event, ...) }`.
	// node:child_process is a documented available import ("Node.js
	// built-ins ... are also available").
	script := "// Managed by aide. Do not edit manually.\n" +
		"// aide-event: " + nativeEvent + "\n" +
		"// aide-command: " + h.Command + "\n" +
		"import { execSync } from \"node:child_process\";\n" +
		"import type { ExtensionAPI } from \"@earendil-works/pi-coding-agent\";\n\n" +
		"export default function (pi: ExtensionAPI) {\n" +
		"  pi.on(\"" + nativeEvent + "\", async () => {\n" +
		"    execSync(\"" + h.Command + "\", { stdio: \"inherit\" });\n" +
		"  });\n" +
		"}\n"
	if err := fsutil.AtomicWrite(filepath.Join(dir, name), []byte(script)); err != nil {
		return fmt.Errorf("pi hooks: write extension: %w", err)
	}
	return nil
}

func (c *piHookCodec) Remove(path string) error {
	return os.Remove(path)
}

// ReadHooks returns aide-managed hooks by listing aide-*.ts extensions.
func (d *Driver) ReadHooks(ctx provision.Context) ([]provision.Hook, error) {
	return provision.ReadHooks(piExtensionsDir(ctx), &piHookCodec{})
}

// WriteHooks removes all aide-*.ts extensions and writes new ones for
// desired. prevManaged is unused for file-based formats; the aide-
// naming prefix is the ownership signal, same as gemini/hermes/opencode.
func (d *Driver) WriteHooks(ctx provision.Context, _ []provision.Hook, desired []provision.Hook) error {
	return provision.WriteHooks(piExtensionsDir(ctx), desired, &piHookCodec{})
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/provision/agents/pi/... -v`
Expected: PASS (all tests in the package, both Task 2's and Task 3's)

- [ ] **Step 6: Commit**

```bash
git add internal/provision/hook_artifact.go internal/provision/agents/pi/hooks.go internal/provision/agents/pi/hooks_test.go
git commit -m "Add Pi hooks support"
```

---

### Task 4: Wire Pi into aide's registries

**Files:**
- Modify: `cmd/aide/provision_drivers.go`
- Modify: `internal/display/display.go`

**Interfaces:**
- Consumes: `pi.New` (self-registers via blank import's `init()`, from Task 2)

- [ ] **Step 1: Add the blank import**

In `cmd/aide/provision_drivers.go`, add the import in alphabetical order (note the package name is `pi`, so the import path's final segment collides with nothing else already imported there — no alias needed for a blank import):

```go
import (
	_ "github.com/jskswamy/aide/internal/provision/agents/claude"
	_ "github.com/jskswamy/aide/internal/provision/agents/codex"
	_ "github.com/jskswamy/aide/internal/provision/agents/copilot"
	_ "github.com/jskswamy/aide/internal/provision/agents/cursor"
	_ "github.com/jskswamy/aide/internal/provision/agents/gemini"
	_ "github.com/jskswamy/aide/internal/provision/agents/hermes"
	_ "github.com/jskswamy/aide/internal/provision/agents/pi"
)
```

(If the OpenCode plan has already landed, `opencode` sorts between `hermes` and `pi` alphabetically — keep the list sorted.)

- [ ] **Step 2: Add the display icon**

In `internal/display/display.go`, add `"pi": "🥧",` to `DefaultAgentIcons`:

```go
var DefaultAgentIcons = map[string]string{
	"claude":  "🤖",
	"gemini":  "✨",
	"codex":   "📝",
	"copilot": "✈️",
	"cursor":  "🖱",
	"pi":      "🥧",
}
```

(If the OpenCode plan has already landed, add this alongside the existing `"opencode"` entry rather than replacing it.)

- [ ] **Step 3: Verify the driver is registered and run the full test suite**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: PASS, no build or vet errors anywhere in the module (this task's changes are wiring-only but touch shared files, so a full-module check is the right scope here, unlike the package-scoped checks in earlier tasks).

- [ ] **Step 4: Commit**

```bash
git add cmd/aide/provision_drivers.go internal/display/display.go
git commit -m "Register Pi as a provisionable agent"
```

---

## Self-Review Notes

Spec coverage check against `docs/superpowers/specs/2026-08-31-opencode-pi-agent-support-design.md`'s Pi section: sandbox (Task 1), plugins CLI-driven with the confirmed two-line parser and the `piNotFoundStderr` tolerate fix (Task 2), `AgentDir`/`PI_CODING_AGENT_DIR` profile support (Task 2), hooks (Task 3), wiring (Task 4) — all covered. MCP non-goal is enforced by simply never setting `SupportsMCP: true` and never implementing `MCPConfigPath`/`MCPHandler` (they fall through to `DriverBase`'s stubs), verified by `TestPiCapabilities`'s explicit `if d.SupportsMCP() { t.Error(...) }` assertion. The "Pi project trust" known limitation from the spec is a documentation/behavior note, not a code task — it's already captured in the package doc comment on `pi.go` and requires no separate task, since `aide` deliberately does not automate trust decisions.
