# Declarative Agent Provisioning Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `aide sync` + `aide adopt` to reconcile declared plugins/MCP servers per context against the agent's installed state. Plan-then-apply UX, abort+rollback on failure, config-hash drift hint on launch.

**Architecture:** New `internal/provision/` package with a `Provisioner` interface implemented per agent. Shared engine handles plan computation, journal-based apply, and rollback. MCP file writes go through one shared helper; plugin install/uninstall shells out to each agent's CLI. State tracked in `~/.local/state/aide/managed.json`.

**Tech Stack:** Go 1.25; existing `internal/fsutil.AtomicWrite`; existing `internal/config` schema + YAML; existing `internal/homepath` for `~` expansion; cobra for new CLI commands; std `os/exec` for subprocess.

**Spec:** `docs/specs/2026-05-15-declarative-agent-provisioning-design.md`.

---

## File Structure

**New files (`internal/provision/`):**
- `provisioner.go` — `Provisioner` interface, `Plugin`, `MCPServer`, `Op`, `Plan` types
- `state.go` — `ManagedState` struct, atomic read/write of `managed.json`
- `confighash.go` — `ConfigHash(cfgPath string) (string, error)` for drift hint
- `plan.go` — desired/installed/managed diff → list of `Op`s
- `mcpfile.go` — shared read/merge/write of agent MCP config with `_aide_managed` marker
- `journal.go` — in-memory op journal for rollback
- `engine.go` — sync engine: walk plan, execute ops, rollback on error
- `provisioner_test.go`, `state_test.go`, `confighash_test.go`, `plan_test.go`, `mcpfile_test.go`, `journal_test.go`, `engine_test.go` — tests for each

**New files (`internal/provision/agents/`):** *(driver-per-agent — deferred for human review of capability matrix)*
- `claude.go`, `goose.go`, `codex.go`, `gemini.go`, `aider.go`, `amp.go`, `copilot.go`

**New CLI files:**
- `cmd/aide/sync.go` — `aide sync` command
- `cmd/aide/adopt.go` — `aide adopt` command
- `cmd/aide/provision_list.go` — `aide plugin list` / `aide mcp list`

**Modified files:**
- `internal/config/schema.go` — add `Plugin` type, `Plugins` map on `Config`, `Plugins []string` on `Context`
- `internal/config/validation.go` (or wherever validation lives) — validate `plugins.<name>.source` enum, validate `contexts.<x>.plugins` references
- `internal/launcher/launcher.go` or `internal/ui/banner.go` — add config-hash drift hint to launch banner

---

## Task 1: Plugin schema types

**Files:**
- Modify: `internal/config/schema.go` (append `Plugin` struct, `Source` constants, `Plugins map[string]Plugin` on `Config`, `Plugins []string` on `Context`)
- Modify: `internal/config/schema_test.go` (or create if missing) — round-trip YAML test

- [ ] **Step 1: Write the failing test**

Create `internal/config/plugin_schema_test.go`:

```go
package config_test

import (
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/config"
	"gopkg.in/yaml.v3"
)

func TestPluginRoundTrip(t *testing.T) {
	y := `
plugins:
  linear:
    source: marketplace
    name: linear@1.2
contexts:
  work:
    agent: claude
    plugins: [linear]
`
	var cfg config.Config
	if err := yaml.NewDecoder(strings.NewReader(y)).Decode(&cfg); err != nil {
		t.Fatalf("decode: %v", err)
	}
	p, ok := cfg.Plugins["linear"]
	if !ok {
		t.Fatal("linear plugin not parsed")
	}
	if p.Source != "marketplace" || p.Name != "linear@1.2" {
		t.Errorf("plugin = %+v", p)
	}
	ctx := cfg.Contexts["work"]
	if len(ctx.Plugins) != 1 || ctx.Plugins[0] != "linear" {
		t.Errorf("ctx.Plugins = %v", ctx.Plugins)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

`go test ./internal/config/ -run TestPluginRoundTrip` — expected FAIL ("Plugins field not found" or similar).

- [ ] **Step 3: Add types**

In `internal/config/schema.go`, after the `MCPServer` struct, append:

```go
// Plugin source enum values.
const (
	PluginSourceMarketplace = "marketplace"
	PluginSourceGit         = "git"
	PluginSourceLocal       = "local"
)

// ValidPluginSources lists allowed values for Plugin.Source.
var ValidPluginSources = []string{
	PluginSourceMarketplace,
	PluginSourceGit,
	PluginSourceLocal,
}

// Plugin declares a single agent plugin that can be referenced from
// contexts.<name>.plugins. Aide reconciles this declaration against the
// agent's installed plugin state via `aide sync`.
type Plugin struct {
	// Source is one of: marketplace, git, local. Validated at config load.
	Source string `yaml:"source"`
	// Name is the agent-interpreted reference. For marketplace, the
	// plugin name (optionally @version). For git, a repo URL with
	// optional @ref. For local, a filesystem path.
	Name string `yaml:"name"`
}
```

In the `Config` struct (top of file), add:

```go
	Plugins map[string]Plugin `yaml:"plugins,omitempty"`
```

In the `Context` struct, add (next to `MCPServers`):

```go
	Plugins []string `yaml:"plugins,omitempty"`
```

- [ ] **Step 4: Run test to verify it passes**

`go test ./internal/config/ -run TestPluginRoundTrip` — expected PASS.

- [ ] **Step 5: Run full config suite**

`go test ./internal/config/...` — expected PASS (no regressions).

- [ ] **Step 6: Commit**

```bash
git add internal/config/schema.go internal/config/plugin_schema_test.go
git commit -m "Add Plugin schema types to config"
```

---

## Task 2: Validate plugin source enum and references

**Files:**
- Modify: `internal/config/schema.go` (add `Validate` method or extend existing validation)
- Create or modify: `internal/config/validation_test.go`

- [ ] **Step 1: Write failing tests**

Append to `internal/config/plugin_schema_test.go` (or new file):

```go
func TestPluginSourceUnknownValueRejected(t *testing.T) {
	y := `
plugins:
  weird:
    source: bogus
    name: foo
contexts:
  work:
    agent: claude
`
	var cfg config.Config
	if err := yaml.NewDecoder(strings.NewReader(y)).Decode(&cfg); err != nil {
		t.Fatalf("decode: %v", err)
	}
	err := cfg.ValidatePlugins()
	if err == nil || !strings.Contains(err.Error(), "weird") || !strings.Contains(err.Error(), "bogus") {
		t.Errorf("expected error naming plugin and bad source, got %v", err)
	}
}

func TestContextPluginReferenceMustExist(t *testing.T) {
	y := `
plugins:
  linear:
    source: marketplace
    name: linear
contexts:
  work:
    agent: claude
    plugins: [linear, ghost]
`
	var cfg config.Config
	_ = yaml.NewDecoder(strings.NewReader(y)).Decode(&cfg)
	err := cfg.ValidatePlugins()
	if err == nil || !strings.Contains(err.Error(), "ghost") {
		t.Errorf("expected error naming undefined plugin reference, got %v", err)
	}
}

func TestValidatePluginsHappyPath(t *testing.T) {
	y := `
plugins:
  linear: {source: marketplace, name: linear}
contexts:
  work:
    agent: claude
    plugins: [linear]
`
	var cfg config.Config
	_ = yaml.NewDecoder(strings.NewReader(y)).Decode(&cfg)
	if err := cfg.ValidatePlugins(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}
```

- [ ] **Step 2: Run to verify failure**

`go test ./internal/config/ -run TestPlugin -v` — expected FAIL ("ValidatePlugins not defined").

- [ ] **Step 3: Implement validation**

Add to `internal/config/schema.go` (or a new `internal/config/validation.go`):

```go
import "slices"

// ValidatePlugins checks that every Plugin.Source is one of
// ValidPluginSources and that every contexts.<x>.plugins entry
// references a defined top-level plugin. Returns nil on success.
func (c *Config) ValidatePlugins() error {
	for name, p := range c.Plugins {
		if !slices.Contains(ValidPluginSources, p.Source) {
			return fmt.Errorf("plugin %q has unknown source %q (allowed: %s)",
				name, p.Source, strings.Join(ValidPluginSources, ", "))
		}
		if p.Name == "" {
			return fmt.Errorf("plugin %q is missing required field: name", name)
		}
	}
	for ctxName, ctx := range c.Contexts {
		for _, ref := range ctx.Plugins {
			if _, ok := c.Plugins[ref]; !ok {
				return fmt.Errorf("context %q references undefined plugin %q (declare it under top-level `plugins:`)",
					ctxName, ref)
			}
		}
	}
	return nil
}
```

Ensure `import "strings"` is present (already is).

- [ ] **Step 4: Wire validation into config load**

Find where the config currently calls existing validation (search for `func.*Validate` in `internal/config/`). Add a `ValidatePlugins()` call alongside. If there's a top-level `Validate()` method, append to it. If not, add the call to wherever `Load`/`LoadFile`/`loadFile` runs validation. **Do not** silently skip on error — return it.

If there is no existing aggregate `Validate()`, add at the end of `loadFile` (or whichever function returns the parsed `*Config`):

```go
if err := cfg.ValidatePlugins(); err != nil {
    return nil, fmt.Errorf("config: %w", err)
}
```

- [ ] **Step 5: Run tests**

`go test ./internal/config/...` — expected PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/config/
git commit -m "Validate plugin source enum and context references"
```

---

## Task 3: Provisioner interface + shared types

**Files:**
- Create: `internal/provision/provisioner.go`
- Create: `internal/provision/provisioner_test.go`

- [ ] **Step 1: Write failing test (compile-time check)**

Create `internal/provision/provisioner_test.go`:

```go
package provision_test

import (
	"testing"

	"github.com/jskswamy/aide/internal/provision"
)

func TestPluginAndMCPServerZeroValue(t *testing.T) {
	var p provision.Plugin
	var m provision.MCPServer
	_ = p
	_ = m
}

func TestOpKindStrings(t *testing.T) {
	cases := map[provision.OpKind]string{
		provision.OpInstall:    "install",
		provision.OpUpdate:     "update",
		provision.OpUninstall:  "uninstall",
		provision.OpAdopt:      "adopt",
		provision.OpIgnore:     "ignore",
	}
	for k, want := range cases {
		if got := k.String(); got != want {
			t.Errorf("OpKind(%d).String() = %q, want %q", k, got, want)
		}
	}
}
```

- [ ] **Step 2: Run to verify failure**

`go test ./internal/provision/...` — expected FAIL ("no Go files").

- [ ] **Step 3: Implement**

Create `internal/provision/provisioner.go`:

```go
// Package provision reconciles declared plugins and MCP servers against
// an agent's installed state. See
// docs/specs/2026-05-15-declarative-agent-provisioning-design.md.
package provision

import "github.com/jskswamy/aide/internal/config"

// Plugin is a resolved plugin declaration ready for installation.
// Source matches one of config.ValidPluginSources.
type Plugin struct {
	// Key is the top-level plugins.<key> in config.yaml.
	Key    string
	Source string
	Name   string
}

// MCPServer is a resolved MCP server declaration. Shape mirrors
// config.MCPServer; duplicated here so internal/provision does not
// depend on YAML tags or future schema migrations.
type MCPServer struct {
	Key     string
	Command string
	URL     string
	Args    []string
	Env     map[string]string
}

// Context is the resolved-per-context input to a Provisioner. It
// carries the agent's name and the user's HOME so each driver can
// locate the agent's MCP config file.
type Context struct {
	Name    string // context name from config.Contexts
	Agent   string // resolved agent name (e.g. "claude")
	HomeDir string
}

// Provisioner is implemented per agent. The driver knows how to talk
// to one agent's CLI to list/install/uninstall plugins, and where the
// agent's MCP config file lives so the shared MCP helper can manage it.
type Provisioner interface {
	// Name returns the agent name, e.g. "claude".
	Name() string

	// SupportsPlugins reports whether this agent has a plugin system
	// that aide can reconcile.
	SupportsPlugins() bool

	// SupportsMCP reports whether this agent reads an MCP config file
	// that aide can manage.
	SupportsMCP() bool

	// MCPConfigPath returns the absolute path to the agent's MCP
	// config file. Called only when SupportsMCP() == true.
	MCPConfigPath(ctx Context) string

	// InstalledPlugins returns the agent's currently-installed plugins.
	// Called only when SupportsPlugins() == true.
	InstalledPlugins(ctx Context) ([]Plugin, error)

	// InstallPlugin invokes the agent's install path for one plugin.
	// Must be idempotent if possible — engine will not re-call on success.
	InstallPlugin(ctx Context, p Plugin) error

	// UninstallPlugin invokes the agent's uninstall path. Must succeed
	// even if the plugin is already absent (so rollback is safe).
	UninstallPlugin(ctx Context, name string) error
}

// OpKind enumerates the operations a sync plan can contain.
type OpKind int

const (
	OpInstall OpKind = iota
	OpUpdate
	OpUninstall
	OpAdopt
	OpIgnore
)

// String returns the lowercase op name used in plan output.
func (k OpKind) String() string {
	switch k {
	case OpInstall:
		return "install"
	case OpUpdate:
		return "update"
	case OpUninstall:
		return "uninstall"
	case OpAdopt:
		return "adopt"
	case OpIgnore:
		return "ignore"
	default:
		return "unknown"
	}
}

// Kind is the kind of resource an op acts on.
type Kind int

const (
	KindPlugin Kind = iota
	KindMCP
)

func (k Kind) String() string {
	switch k {
	case KindPlugin:
		return "plugin"
	case KindMCP:
		return "mcp"
	default:
		return "unknown"
	}
}

// Op is one element of a sync plan.
type Op struct {
	Kind     Kind
	OpKind   OpKind
	Name     string     // resource name (plugin key or mcp server key)
	Plugin   *Plugin    // populated for KindPlugin install/update/adopt
	MCP      *MCPServer // populated for KindMCP install/update/adopt
	OldMCP   *MCPServer // populated for KindMCP update (for rollback)
}

// Plan is the ordered list of operations to apply for one context.
type Plan struct {
	Context Context
	Ops     []Op
}

// HasMutations reports whether the plan would change anything when
// applied (any op other than OpIgnore counts).
func (p *Plan) HasMutations() bool {
	for _, o := range p.Ops {
		if o.OpKind != OpIgnore {
			return true
		}
	}
	return false
}

// ResolveContext builds a provision.Context from the resolved config
// context. Caller supplies HomeDir.
func ResolveContext(name string, ctx config.Context, homeDir string) Context {
	return Context{Name: name, Agent: ctx.Agent, HomeDir: homeDir}
}
```

- [ ] **Step 4: Run tests**

`go test ./internal/provision/...` — expected PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/provision/
git commit -m "Provision package: Provisioner interface + plan op types"
```

---

## Task 4: ManagedState + atomic JSON read/write

**Files:**
- Create: `internal/provision/state.go`
- Create: `internal/provision/state_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/provision/state_test.go`:

```go
package provision_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jskswamy/aide/internal/provision"
)

func TestLoadStateMissingFileReturnsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "managed.json")
	st, err := provision.LoadState(path)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if st == nil {
		t.Fatal("expected non-nil state")
	}
	if st.Contexts == nil {
		t.Error("expected non-nil Contexts map")
	}
}

func TestSaveStateRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "managed.json")
	st := &provision.ManagedState{
		Version:    1,
		ConfigHash: "sha256:abc",
		Contexts: map[string]*provision.ContextState{
			"work": {
				Plugins:    map[string]provision.ManagedItem{"linear": {Version: "1.2"}},
				MCPServers: map[string]provision.ManagedItem{"postgres": {}},
			},
		},
	}
	if err := provision.SaveState(path, st); err != nil {
		t.Fatalf("SaveState: %v", err)
	}
	got, err := provision.LoadState(path)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if got.ConfigHash != "sha256:abc" {
		t.Errorf("ConfigHash = %q", got.ConfigHash)
	}
	if got.Contexts["work"].Plugins["linear"].Version != "1.2" {
		t.Errorf("plugin version not preserved: %+v", got.Contexts["work"])
	}
}

func TestSaveStatePermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "managed.json")
	if err := provision.SaveState(path, &provision.ManagedState{Version: 1}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("perm = %o, want 0o600", perm)
	}
}
```

- [ ] **Step 2: Run to verify failure**

`go test ./internal/provision/ -run TestLoadState -v` — expected FAIL.

- [ ] **Step 3: Implement**

Create `internal/provision/state.go`:

```go
package provision

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/jskswamy/aide/internal/fsutil"
)

// StateVersion is the current managed.json schema version. Bump on
// breaking changes and add a migration in LoadState.
const StateVersion = 1

// ManagedItem records that aide installed a plugin or MCP server.
// Version is empty for MCP entries (they have no version concept).
type ManagedItem struct {
	InstalledAt time.Time `json:"installed_at,omitempty"`
	Version     string    `json:"version,omitempty"`
}

// ContextState is per-context managed item tracking.
type ContextState struct {
	Plugins    map[string]ManagedItem `json:"plugins,omitempty"`
	MCPServers map[string]ManagedItem `json:"mcp_servers,omitempty"`
}

// ManagedState is the on-disk shape of ~/.local/state/aide/managed.json.
// Only updated when a sync run completes successfully end-to-end.
type ManagedState struct {
	Version    int                      `json:"version"`
	ConfigHash string                   `json:"config_hash,omitempty"`
	SyncedAt   time.Time                `json:"synced_at,omitempty"`
	Contexts   map[string]*ContextState `json:"contexts,omitempty"`
}

// LoadState reads the state file at path. If the file is missing,
// returns an empty state (not an error) so first-time callers get a
// blank slate.
func LoadState(path string) (*ManagedState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &ManagedState{Version: StateVersion, Contexts: map[string]*ContextState{}}, nil
		}
		return nil, fmt.Errorf("provision: reading state %s: %w", path, err)
	}
	var st ManagedState
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, fmt.Errorf("provision: parsing state %s: %w", path, err)
	}
	if st.Contexts == nil {
		st.Contexts = map[string]*ContextState{}
	}
	return &st, nil
}

// SaveState atomically writes st to path. Parents are created with
// 0o750, the file ends at 0o600 (via fsutil.AtomicWrite).
func SaveState(path string, st *ManagedState) error {
	if st.Version == 0 {
		st.Version = StateVersion
	}
	if st.Contexts == nil {
		st.Contexts = map[string]*ContextState{}
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("provision: marshalling state: %w", err)
	}
	return fsutil.AtomicWrite(path, data)
}

// DefaultStatePath returns ~/.local/state/aide/managed.json given a
// home directory. Caller is responsible for HOME resolution.
func DefaultStatePath(homeDir string) string {
	return homeDir + "/.local/state/aide/managed.json"
}
```

- [ ] **Step 4: Run tests**

`go test ./internal/provision/ -run TestLoadState -v && go test ./internal/provision/ -run TestSaveState -v` — expected PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/provision/
git commit -m "Provision state file: ManagedState + atomic JSON I/O"
```

---

## Task 5: Config hash for drift detection

**Files:**
- Create: `internal/provision/confighash.go`
- Create: `internal/provision/confighash_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/provision/confighash_test.go`:

```go
package provision_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/provision"
)

func TestConfigHashStableForSameBytes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("hello: world\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a, err := provision.ConfigHash(path)
	if err != nil {
		t.Fatal(err)
	}
	b, err := provision.ConfigHash(path)
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Errorf("hashes differ: %s vs %s", a, b)
	}
	if !strings.HasPrefix(a, "sha256:") {
		t.Errorf("expected sha256: prefix, got %q", a)
	}
}

func TestConfigHashChangesWithBytes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	_ = os.WriteFile(path, []byte("a: 1\n"), 0o600)
	h1, _ := provision.ConfigHash(path)
	_ = os.WriteFile(path, []byte("a: 2\n"), 0o600)
	h2, _ := provision.ConfigHash(path)
	if h1 == h2 {
		t.Error("expected different hashes")
	}
}

func TestConfigHashMissingFileReturnsEmpty(t *testing.T) {
	h, err := provision.ConfigHash(filepath.Join(t.TempDir(), "nope.yaml"))
	if err != nil {
		t.Fatalf("missing config should not error, got %v", err)
	}
	if h != "" {
		t.Errorf("expected empty hash for missing file, got %q", h)
	}
}
```

- [ ] **Step 2: Run to verify failure**

`go test ./internal/provision/ -run TestConfigHash -v` — expected FAIL.

- [ ] **Step 3: Implement**

Create `internal/provision/confighash.go`:

```go
package provision

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
)

// ConfigHash returns "sha256:<hex>" for the bytes of path. If path
// does not exist, returns ("", nil) so the launch drift check can
// treat a missing config as "no drift" (first run).
func ConfigHash(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", fmt.Errorf("provision: hashing config %s: %w", path, err)
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}
```

- [ ] **Step 4: Run tests**

`go test ./internal/provision/ -run TestConfigHash -v` — expected PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/provision/
git commit -m "Provision: SHA-256 config hash for drift detection"
```

---

## Task 6: Plan computation

**Files:**
- Create: `internal/provision/plan.go`
- Create: `internal/provision/plan_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/provision/plan_test.go`:

```go
package provision_test

import (
	"testing"

	"github.com/jskswamy/aide/internal/provision"
)

func TestComputePlanInstall(t *testing.T) {
	desired := provision.Desired{
		Plugins: map[string]provision.Plugin{
			"linear": {Key: "linear", Source: "marketplace", Name: "linear"},
		},
	}
	installed := provision.Installed{}
	managed := provision.ContextState{}
	plan := provision.ComputePlan(provision.Context{Name: "work"}, desired, installed, managed)
	if len(plan.Ops) != 1 || plan.Ops[0].OpKind != provision.OpInstall {
		t.Fatalf("expected one install op, got %+v", plan.Ops)
	}
	if plan.Ops[0].Plugin == nil || plan.Ops[0].Plugin.Key != "linear" {
		t.Errorf("op.Plugin = %+v", plan.Ops[0].Plugin)
	}
}

func TestComputePlanUninstallWhenManagedButNotDesired(t *testing.T) {
	desired := provision.Desired{}
	installed := provision.Installed{Plugins: []string{"old-tool"}}
	managed := provision.ContextState{
		Plugins: map[string]provision.ManagedItem{"old-tool": {}},
	}
	plan := provision.ComputePlan(provision.Context{Name: "work"}, desired, installed, managed)
	if len(plan.Ops) != 1 || plan.Ops[0].OpKind != provision.OpUninstall {
		t.Fatalf("expected one uninstall op, got %+v", plan.Ops)
	}
}

func TestComputePlanIgnoreWhenInstalledButNotManaged(t *testing.T) {
	desired := provision.Desired{}
	installed := provision.Installed{Plugins: []string{"manual-tool"}}
	managed := provision.ContextState{}
	plan := provision.ComputePlan(provision.Context{Name: "work"}, desired, installed, managed)
	if len(plan.Ops) != 1 || plan.Ops[0].OpKind != provision.OpIgnore {
		t.Fatalf("expected one ignore op, got %+v", plan.Ops)
	}
}

func TestComputePlanMCPUpdate(t *testing.T) {
	desired := provision.Desired{
		MCPServers: map[string]provision.MCPServer{
			"postgres": {Key: "postgres", Command: "postgres-mcp", Args: []string{"--port", "9090"}},
		},
	}
	installed := provision.Installed{
		MCPServers: map[string]provision.MCPServer{
			"postgres": {Key: "postgres", Command: "postgres-mcp", Args: []string{"--port", "5432"}},
		},
	}
	managed := provision.ContextState{
		MCPServers: map[string]provision.ManagedItem{"postgres": {}},
	}
	plan := provision.ComputePlan(provision.Context{Name: "work"}, desired, installed, managed)
	if len(plan.Ops) != 1 || plan.Ops[0].OpKind != provision.OpUpdate {
		t.Fatalf("expected one update op, got %+v", plan.Ops)
	}
	if plan.Ops[0].OldMCP == nil || plan.Ops[0].OldMCP.Args[1] != "5432" {
		t.Errorf("OldMCP not captured: %+v", plan.Ops[0].OldMCP)
	}
}
```

- [ ] **Step 2: Run to verify failure**

`go test ./internal/provision/ -run TestComputePlan -v` — expected FAIL.

- [ ] **Step 3: Implement**

Create `internal/provision/plan.go`:

```go
package provision

import (
	"slices"
	"sort"
)

// Desired is the resolved configuration for one context.
type Desired struct {
	Plugins    map[string]Plugin
	MCPServers map[string]MCPServer
}

// Installed is what the agent currently reports.
type Installed struct {
	// Plugins is the list of installed plugin keys (names match the
	// top-level config keys when possible).
	Plugins []string
	// MCPServers maps key → server config as registered in the agent.
	MCPServers map[string]MCPServer
}

// ComputePlan diffs desired/installed/managed and returns the ordered
// op list. Sort order: installs, updates, uninstalls, then ignored
// unmanaged items. Within each bucket, names sort alphabetically for
// deterministic plan output.
func ComputePlan(ctx Context, desired Desired, installed Installed, managed ContextState) Plan {
	if managed.Plugins == nil {
		managed.Plugins = map[string]ManagedItem{}
	}
	if managed.MCPServers == nil {
		managed.MCPServers = map[string]ManagedItem{}
	}

	var installs, updates, uninstalls, ignores []Op

	// --- Plugins ---
	for key, p := range desired.Plugins {
		if !slices.Contains(installed.Plugins, key) {
			pp := p
			installs = append(installs, Op{
				Kind: KindPlugin, OpKind: OpInstall, Name: key, Plugin: &pp,
			})
			continue
		}
		// Installed and desired: detect version drift via Name field.
		// Lookup is by key in managed state; if version recorded differs
		// from desired Name's version part, mark as update.
		if mi, ok := managed.Plugins[key]; ok && mi.Version != "" && mi.Version != extractVersion(p.Name) {
			pp := p
			updates = append(updates, Op{
				Kind: KindPlugin, OpKind: OpUpdate, Name: key, Plugin: &pp,
			})
		}
	}
	for key := range managed.Plugins {
		if _, stillDesired := desired.Plugins[key]; !stillDesired {
			uninstalls = append(uninstalls, Op{
				Kind: KindPlugin, OpKind: OpUninstall, Name: key,
			})
		}
	}
	for _, key := range installed.Plugins {
		if _, isDesired := desired.Plugins[key]; isDesired {
			continue
		}
		if _, isManaged := managed.Plugins[key]; isManaged {
			continue
		}
		ignores = append(ignores, Op{
			Kind: KindPlugin, OpKind: OpIgnore, Name: key,
		})
	}

	// --- MCP servers ---
	for key, m := range desired.MCPServers {
		cur, present := installed.MCPServers[key]
		if !present {
			mm := m
			installs = append(installs, Op{
				Kind: KindMCP, OpKind: OpInstall, Name: key, MCP: &mm,
			})
			continue
		}
		if !mcpEqual(cur, m) {
			mm := m
			old := cur
			updates = append(updates, Op{
				Kind: KindMCP, OpKind: OpUpdate, Name: key, MCP: &mm, OldMCP: &old,
			})
		}
	}
	for key := range managed.MCPServers {
		if _, stillDesired := desired.MCPServers[key]; !stillDesired {
			uninstalls = append(uninstalls, Op{
				Kind: KindMCP, OpKind: OpUninstall, Name: key,
			})
		}
	}
	for key := range installed.MCPServers {
		if _, isDesired := desired.MCPServers[key]; isDesired {
			continue
		}
		if _, isManaged := managed.MCPServers[key]; isManaged {
			continue
		}
		ignores = append(ignores, Op{
			Kind: KindMCP, OpKind: OpIgnore, Name: key,
		})
	}

	sortOps := func(o []Op) { sort.Slice(o, func(i, j int) bool { return o[i].Name < o[j].Name }) }
	sortOps(installs)
	sortOps(updates)
	sortOps(uninstalls)
	sortOps(ignores)

	all := append(installs, updates...)
	all = append(all, uninstalls...)
	all = append(all, ignores...)
	return Plan{Context: ctx, Ops: all}
}

// extractVersion returns the substring after "@" in name, or "" if absent.
// Example: "linear@1.2" -> "1.2", "linear" -> "".
func extractVersion(name string) string {
	for i := len(name) - 1; i >= 0; i-- {
		if name[i] == '@' {
			return name[i+1:]
		}
	}
	return ""
}

// mcpEqual reports whether two MCP server configs are equivalent for
// plan purposes. Compares Command, URL, Args (order-sensitive), Env
// (order-insensitive).
func mcpEqual(a, b MCPServer) bool {
	if a.Command != b.Command || a.URL != b.URL {
		return false
	}
	if !slices.Equal(a.Args, b.Args) {
		return false
	}
	if len(a.Env) != len(b.Env) {
		return false
	}
	for k, v := range a.Env {
		if b.Env[k] != v {
			return false
		}
	}
	return true
}
```

- [ ] **Step 4: Run tests**

`go test ./internal/provision/ -run TestComputePlan -v` — expected PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/provision/
git commit -m "Provision: ComputePlan with desired/installed/managed diff"
```

---

## Task 7: MCP file helper with `_aide_managed` marker

**Files:**
- Create: `internal/provision/mcpfile.go`
- Create: `internal/provision/mcpfile_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/provision/mcpfile_test.go`:

```go
package provision_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/jskswamy/aide/internal/provision"
)

func TestReadMCPFileMissingReturnsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "absent.json")
	got, mgd, err := provision.ReadMCPFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 || len(mgd) != 0 {
		t.Errorf("expected empty for missing file, got %+v %+v", got, mgd)
	}
}

func TestReadMCPFileWithManaged(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp.json")
	body := `{
  "_aide_managed": ["postgres"],
  "mcpServers": {
    "postgres": {"command": "postgres-mcp", "args": ["--port", "5432"]},
    "user-added": {"command": "manual"}
  }
}`
	_ = os.WriteFile(path, []byte(body), 0o600)
	got, mgd, err := provision.ReadMCPFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 servers, got %d", len(got))
	}
	if !mgd["postgres"] || mgd["user-added"] {
		t.Errorf("managed set = %+v", mgd)
	}
}

func TestWriteMCPFilePreservesUnmanaged(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp.json")
	body := `{
  "mcpServers": {
    "user-added": {"command": "manual"}
  }
}`
	_ = os.WriteFile(path, []byte(body), 0o600)

	desired := map[string]provision.MCPServer{
		"postgres": {Key: "postgres", Command: "postgres-mcp", Args: []string{"--port", "9090"}},
	}
	if err := provision.WriteMCPFile(path, desired); err != nil {
		t.Fatal(err)
	}

	raw, _ := os.ReadFile(path)
	var doc struct {
		AideManaged []string                          `json:"_aide_managed"`
		Servers     map[string]map[string]interface{} `json:"mcpServers"`
	}
	_ = json.Unmarshal(raw, &doc)
	if _, ok := doc.Servers["user-added"]; !ok {
		t.Error("user-added entry must survive write")
	}
	if _, ok := doc.Servers["postgres"]; !ok {
		t.Error("postgres entry not written")
	}
	if len(doc.AideManaged) != 1 || doc.AideManaged[0] != "postgres" {
		t.Errorf("_aide_managed = %v", doc.AideManaged)
	}
}

func TestWriteMCPFileRemovesPreviouslyManaged(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp.json")
	body := `{
  "_aide_managed": ["old", "stay"],
  "mcpServers": {
    "old":  {"command": "x"},
    "stay": {"command": "y"}
  }
}`
	_ = os.WriteFile(path, []byte(body), 0o600)

	desired := map[string]provision.MCPServer{
		"stay": {Key: "stay", Command: "y"},
	}
	if err := provision.WriteMCPFile(path, desired); err != nil {
		t.Fatal(err)
	}

	raw, _ := os.ReadFile(path)
	var doc struct {
		AideManaged []string                          `json:"_aide_managed"`
		Servers     map[string]map[string]interface{} `json:"mcpServers"`
	}
	_ = json.Unmarshal(raw, &doc)
	if _, gone := doc.Servers["old"]; gone {
		t.Error("old should have been removed (was managed by aide)")
	}
	if _, kept := doc.Servers["stay"]; !kept {
		t.Error("stay should be preserved")
	}
	if len(doc.AideManaged) != 1 || doc.AideManaged[0] != "stay" {
		t.Errorf("_aide_managed = %v", doc.AideManaged)
	}
}
```

- [ ] **Step 2: Run to verify failure**

`go test ./internal/provision/ -run TestMCPFile -v && go test ./internal/provision/ -run TestReadMCPFile -v && go test ./internal/provision/ -run TestWriteMCPFile -v` — expected FAIL.

- [ ] **Step 3: Implement**

Create `internal/provision/mcpfile.go`:

```go
package provision

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"

	"github.com/jskswamy/aide/internal/fsutil"
)

// mcpFileShape is the on-disk JSON we read/write. Many agents use
// `mcpServers` as the top-level key (Claude Code), some use other names
// — driver chooses the path, but the shape is uniform enough that one
// implementation covers them. Drivers that need a different top-level
// key can provide their own shape implementation; this one is the
// default.
type mcpFileShape struct {
	AideManaged []string                   `json:"_aide_managed,omitempty"`
	Servers     map[string]json.RawMessage `json:"mcpServers,omitempty"`
}

// ReadMCPFile parses the agent's MCP config file at path. Returns the
// full server map and the set of keys aide owns (per the
// `_aide_managed` marker). Missing files return empty maps without
// error.
func ReadMCPFile(path string) (servers map[string]MCPServer, managed map[string]bool, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]MCPServer{}, map[string]bool{}, nil
		}
		return nil, nil, fmt.Errorf("provision: reading MCP file %s: %w", path, err)
	}
	var doc mcpFileShape
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, nil, fmt.Errorf("provision: parsing MCP file %s: %w", path, err)
	}

	servers = map[string]MCPServer{}
	for key, raw := range doc.Servers {
		var s MCPServer
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, nil, fmt.Errorf("provision: parsing MCP server %q: %w", key, err)
		}
		s.Key = key
		servers[key] = s
	}

	managed = map[string]bool{}
	for _, k := range doc.AideManaged {
		managed[k] = true
	}
	return servers, managed, nil
}

// WriteMCPFile reconciles the desired set into path. Entries managed
// by aide are added/updated/removed to match desired. Entries the user
// added manually (not in `_aide_managed`) survive untouched. The
// `_aide_managed` marker is rewritten to exactly the keys of desired.
func WriteMCPFile(path string, desired map[string]MCPServer) error {
	// Re-read raw to preserve unknown top-level fields. Some agents
	// store more than `_aide_managed` and `mcpServers`; we round-trip
	// any extras via a generic map.
	existing := map[string]json.RawMessage{}
	if data, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(data, &existing); err != nil {
			return fmt.Errorf("provision: parsing existing MCP file %s: %w", path, err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("provision: reading MCP file %s: %w", path, err)
	}

	prevServers := map[string]json.RawMessage{}
	if raw, ok := existing["mcpServers"]; ok {
		_ = json.Unmarshal(raw, &prevServers)
	}
	prevManaged := []string{}
	if raw, ok := existing["_aide_managed"]; ok {
		_ = json.Unmarshal(raw, &prevManaged)
	}
	wasManaged := map[string]bool{}
	for _, k := range prevManaged {
		wasManaged[k] = true
	}

	// Build new server map: keep non-managed entries, drop previously-managed
	// entries that are no longer desired, write new/updated desired entries.
	newServers := map[string]json.RawMessage{}
	for key, raw := range prevServers {
		if wasManaged[key] {
			continue // managed: aide will rewrite if still desired
		}
		newServers[key] = raw
	}
	newManaged := make([]string, 0, len(desired))
	for key, s := range desired {
		// Marshal in shape compatible with mcpServers entries.
		body := map[string]any{}
		if s.Command != "" {
			body["command"] = s.Command
		}
		if s.URL != "" {
			body["url"] = s.URL
		}
		if len(s.Args) > 0 {
			body["args"] = s.Args
		}
		if len(s.Env) > 0 {
			body["env"] = s.Env
		}
		raw, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("provision: marshalling MCP server %q: %w", key, err)
		}
		newServers[key] = raw
		newManaged = append(newManaged, key)
	}
	sort.Strings(newManaged)

	// Round-trip back into existing's top-level shape.
	managedRaw, _ := json.Marshal(newManaged)
	serversRaw, _ := json.Marshal(newServers)
	existing["_aide_managed"] = managedRaw
	existing["mcpServers"] = serversRaw

	out, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return fmt.Errorf("provision: marshalling MCP file: %w", err)
	}
	return fsutil.AtomicWrite(path, out)
}
```

- [ ] **Step 4: Run tests**

`go test ./internal/provision/...` — expected PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/provision/
git commit -m "Provision: MCP file read/write with _aide_managed marker"
```

---

## Task 8: Journal-based rollback engine skeleton

**Files:**
- Create: `internal/provision/journal.go`
- Create: `internal/provision/journal_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/provision/journal_test.go`:

```go
package provision_test

import (
	"errors"
	"testing"

	"github.com/jskswamy/aide/internal/provision"
)

func TestJournalRollbackInReverseOrder(t *testing.T) {
	var order []string
	j := &provision.Journal{}
	j.Record(func() error { order = append(order, "undo-1"); return nil })
	j.Record(func() error { order = append(order, "undo-2"); return nil })
	j.Record(func() error { order = append(order, "undo-3"); return nil })
	if err := j.Rollback(); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	want := []string{"undo-3", "undo-2", "undo-1"}
	if len(order) != 3 || order[0] != want[0] || order[1] != want[1] || order[2] != want[2] {
		t.Errorf("order = %v, want %v", order, want)
	}
}

func TestJournalRollbackContinuesOnInverseFailure(t *testing.T) {
	var attempts []string
	j := &provision.Journal{}
	j.Record(func() error { attempts = append(attempts, "ok-1"); return nil })
	j.Record(func() error { attempts = append(attempts, "fail-2"); return errors.New("boom") })
	j.Record(func() error { attempts = append(attempts, "ok-3"); return nil })
	err := j.Rollback()
	if err == nil {
		t.Fatal("expected aggregate error from rollback")
	}
	if len(attempts) != 3 {
		t.Errorf("expected all 3 inverses attempted, got %v", attempts)
	}
}
```

- [ ] **Step 2: Run to verify failure**

`go test ./internal/provision/ -run TestJournal -v` — expected FAIL.

- [ ] **Step 3: Implement**

Create `internal/provision/journal.go`:

```go
package provision

import (
	"errors"
	"fmt"
	"strings"
)

// Journal records inverse-op closures during a sync. On Rollback the
// closures run in reverse insertion order. Inverse failures don't stop
// rollback — they are collected and surfaced as one aggregate error.
type Journal struct {
	inverses []func() error
}

// Record appends an inverse closure. Call this AFTER the forward op
// succeeds so a failing forward op does not record a phantom inverse.
func (j *Journal) Record(inverse func() error) {
	j.inverses = append(j.inverses, inverse)
}

// Rollback runs every recorded inverse in reverse order. Returns nil
// if all succeeded, otherwise a wrapped aggregate error listing each
// inverse failure.
func (j *Journal) Rollback() error {
	if len(j.inverses) == 0 {
		return nil
	}
	var failures []string
	for i := len(j.inverses) - 1; i >= 0; i-- {
		if err := j.inverses[i](); err != nil {
			failures = append(failures, fmt.Sprintf("inverse[%d]: %v", i, err))
		}
	}
	if len(failures) == 0 {
		return nil
	}
	return errors.New("rollback partial: " + strings.Join(failures, "; "))
}

// Len returns the number of recorded inverses (for plan-output messages).
func (j *Journal) Len() int { return len(j.inverses) }
```

- [ ] **Step 4: Run tests**

`go test ./internal/provision/ -run TestJournal -v` — expected PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/provision/
git commit -m "Provision: in-memory rollback journal"
```

---

## Task 9: Sync engine (orchestrates plan + journal + provisioner)

**Files:**
- Create: `internal/provision/engine.go`
- Create: `internal/provision/engine_test.go`

- [ ] **Step 1: Write failing tests with a fake provisioner**

Create `internal/provision/engine_test.go`:

```go
package provision_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/provision"
)

// fakeProv records every call and lets tests inject errors.
type fakeProv struct {
	name            string
	supportsPlugins bool
	supportsMCP     bool
	mcpPath         string
	installed       []provision.Plugin
	installedMCP    map[string]provision.MCPServer

	installErr   error
	uninstallErr error
	mcpWriteErr  error

	called []string
}

func (f *fakeProv) Name() string                                   { return f.name }
func (f *fakeProv) SupportsPlugins() bool                          { return f.supportsPlugins }
func (f *fakeProv) SupportsMCP() bool                              { return f.supportsMCP }
func (f *fakeProv) MCPConfigPath(_ provision.Context) string       { return f.mcpPath }
func (f *fakeProv) InstalledPlugins(_ provision.Context) ([]provision.Plugin, error) {
	return f.installed, nil
}
func (f *fakeProv) InstallPlugin(_ provision.Context, p provision.Plugin) error {
	f.called = append(f.called, "install:"+p.Key)
	return f.installErr
}
func (f *fakeProv) UninstallPlugin(_ provision.Context, name string) error {
	f.called = append(f.called, "uninstall:"+name)
	return f.uninstallErr
}

func TestSyncInstallsDeclaredPlugin(t *testing.T) {
	fp := &fakeProv{name: "claude", supportsPlugins: true}
	desired := provision.Desired{
		Plugins: map[string]provision.Plugin{
			"linear": {Key: "linear", Source: "marketplace", Name: "linear"},
		},
	}
	plan := provision.ComputePlan(provision.Context{Name: "work", Agent: "claude"}, desired, provision.Installed{}, provision.ContextState{})
	res, err := provision.Apply(fp, plan, provision.ApplyOptions{})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(fp.called) != 1 || fp.called[0] != "install:linear" {
		t.Errorf("calls = %v", fp.called)
	}
	if res.Performed != 1 {
		t.Errorf("performed = %d, want 1", res.Performed)
	}
}

func TestSyncRollsBackOnPluginInstallFailure(t *testing.T) {
	fp := &fakeProv{name: "claude", supportsPlugins: true, installErr: errors.New("network down")}
	desired := provision.Desired{
		Plugins: map[string]provision.Plugin{
			"a": {Key: "a", Source: "marketplace", Name: "a"},
			"b": {Key: "b", Source: "marketplace", Name: "b"},
		},
	}
	plan := provision.ComputePlan(provision.Context{Name: "work", Agent: "claude"}, desired, provision.Installed{}, provision.ContextState{})
	_, err := provision.Apply(fp, plan, provision.ApplyOptions{})
	if err == nil {
		t.Fatal("expected sync to fail")
	}
	if !strings.Contains(err.Error(), "install plugin") {
		t.Errorf("error %q should name failing op kind", err)
	}
}

func TestSyncCapabilityMismatchErrors(t *testing.T) {
	fp := &fakeProv{name: "aider", supportsPlugins: false}
	desired := provision.Desired{
		Plugins: map[string]provision.Plugin{
			"x": {Key: "x", Source: "marketplace", Name: "x"},
		},
	}
	plan := provision.ComputePlan(provision.Context{Name: "work", Agent: "aider"}, desired, provision.Installed{}, provision.ContextState{})
	_, err := provision.Apply(fp, plan, provision.ApplyOptions{})
	if err == nil || !strings.Contains(err.Error(), "does not support plugins") {
		t.Errorf("expected capability error, got %v", err)
	}
}
```

- [ ] **Step 2: Run to verify failure**

`go test ./internal/provision/ -run TestSync -v` — expected FAIL.

- [ ] **Step 3: Implement engine**

Create `internal/provision/engine.go`:

```go
package provision

import (
	"fmt"
)

// ApplyOptions tweaks Apply's behavior. Adopt is per-op (handled by the
// caller building the plan); the engine itself just executes whatever
// ops are in the plan.
type ApplyOptions struct {
	// MCPFileRead/Write allow tests to inject IO. When nil, the real
	// ReadMCPFile / WriteMCPFile are used.
	MCPRead  func(path string) (map[string]MCPServer, map[string]bool, error)
	MCPWrite func(path string, desired map[string]MCPServer) error
}

// ApplyResult summarises what an Apply run did.
type ApplyResult struct {
	Performed int      // successful mutating ops (excludes OpIgnore)
	Skipped   int      // OpIgnore ops
	Failed    string   // empty if success; otherwise the failing op description
	RolledBack int     // inverse ops run during rollback
}

// Apply walks the plan and executes each op against prov. On any
// failure the engine rolls back via the journal and returns the error.
// On success it returns the result; the caller persists state.
func Apply(prov Provisioner, plan Plan, opts ApplyOptions) (ApplyResult, error) {
	read := opts.MCPRead
	if read == nil {
		read = ReadMCPFile
	}
	write := opts.MCPWrite
	if write == nil {
		write = WriteMCPFile
	}

	var res ApplyResult
	j := &Journal{}

	for _, op := range plan.Ops {
		switch op.OpKind {
		case OpIgnore:
			res.Skipped++
			continue
		case OpAdopt:
			// Adoption mutates config.yaml — handled outside the engine.
			// Treat as skipped here; the CLI layer writes config and
			// adds the resulting items to managed state directly.
			res.Skipped++
			continue
		}

		switch op.Kind {
		case KindPlugin:
			if !prov.SupportsPlugins() {
				if err := j.Rollback(); err != nil {
					return res, fmt.Errorf("capability mismatch: agent %q does not support plugins; rollback: %w", prov.Name(), err)
				}
				return res, fmt.Errorf("capability mismatch: agent %q does not support plugins (declared plugin: %q)", prov.Name(), op.Name)
			}
			switch op.OpKind {
			case OpInstall, OpUpdate:
				if err := prov.InstallPlugin(plan.Context, *op.Plugin); err != nil {
					_ = j.Rollback()
					return res, fmt.Errorf("install plugin %q: %w", op.Name, err)
				}
				name := op.Name
				j.Record(func() error { return prov.UninstallPlugin(plan.Context, name) })
				res.Performed++
			case OpUninstall:
				if err := prov.UninstallPlugin(plan.Context, op.Name); err != nil {
					_ = j.Rollback()
					return res, fmt.Errorf("uninstall plugin %q: %w", op.Name, err)
				}
				// No inverse for uninstall — we don't know enough to
				// reinstall the prior version. Document this gap.
				res.Performed++
			}

		case KindMCP:
			if !prov.SupportsMCP() {
				_ = j.Rollback()
				return res, fmt.Errorf("capability mismatch: agent %q does not support MCP (declared server: %q)", prov.Name(), op.Name)
			}
			path := prov.MCPConfigPath(plan.Context)
			prev, _, err := read(path)
			if err != nil {
				_ = j.Rollback()
				return res, fmt.Errorf("read MCP file: %w", err)
			}
			// Capture snapshot for rollback.
			snapshot := copyMCPMap(prev)
			j.Record(func() error { return write(path, snapshot) })

			// Apply the change in-place against prev:
			switch op.OpKind {
			case OpInstall, OpUpdate:
				prev[op.Name] = *op.MCP
			case OpUninstall:
				delete(prev, op.Name)
			}
			if err := write(path, prev); err != nil {
				_ = j.Rollback()
				return res, fmt.Errorf("%s MCP %q: %w", op.OpKind, op.Name, err)
			}
			res.Performed++
		}
	}

	return res, nil
}

func copyMCPMap(in map[string]MCPServer) map[string]MCPServer {
	out := make(map[string]MCPServer, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
```

- [ ] **Step 4: Run tests**

`go test ./internal/provision/ -run TestSync -v` — expected PASS.

- [ ] **Step 5: Run full provision suite**

`go test ./internal/provision/...` — expected PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/provision/
git commit -m "Provision: sync engine with journal-based rollback"
```

---

## Task 10: Launch drift banner hook

**Files:**
- Create: `internal/provision/drift.go`
- Create: `internal/provision/drift_test.go`
- Modify: `internal/ui/banner.go` (find existing render path, splice in drift line)

- [ ] **Step 1: Write failing tests**

Create `internal/provision/drift_test.go`:

```go
package provision_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jskswamy/aide/internal/provision"
)

func TestDriftWhenConfigChangedSinceSync(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	statePath := filepath.Join(dir, "managed.json")

	_ = os.WriteFile(cfgPath, []byte("a: 1\n"), 0o600)
	h, _ := provision.ConfigHash(cfgPath)
	_ = provision.SaveState(statePath, &provision.ManagedState{Version: 1, ConfigHash: h})

	got, err := provision.DriftStatus(cfgPath, statePath)
	if err != nil {
		t.Fatal(err)
	}
	if got != provision.DriftNone {
		t.Errorf("expected DriftNone immediately after sync, got %v", got)
	}

	// Mutate config.
	_ = os.WriteFile(cfgPath, []byte("a: 2\n"), 0o600)
	got, err = provision.DriftStatus(cfgPath, statePath)
	if err != nil {
		t.Fatal(err)
	}
	if got != provision.DriftConfigChanged {
		t.Errorf("expected DriftConfigChanged after config edit, got %v", got)
	}
}

func TestDriftMissingStateMeansNeverSynced(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	_ = os.WriteFile(cfgPath, []byte("a: 1\n"), 0o600)
	got, err := provision.DriftStatus(cfgPath, filepath.Join(dir, "absent.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got != provision.DriftNeverSynced {
		t.Errorf("expected DriftNeverSynced, got %v", got)
	}
}
```

- [ ] **Step 2: Run to verify failure**

`go test ./internal/provision/ -run TestDrift -v` — expected FAIL.

- [ ] **Step 3: Implement**

Create `internal/provision/drift.go`:

```go
package provision

// DriftKind reports the launch-time drift state for a context.
type DriftKind int

const (
	// DriftNone: config hash matches state file. Banner stays silent.
	DriftNone DriftKind = iota
	// DriftConfigChanged: config.yaml hash differs from last sync.
	// Banner shows a one-line hint.
	DriftConfigChanged
	// DriftNeverSynced: state file absent. First-run case. Banner
	// shows a hint only if config.yaml actually declares plugins or
	// MCP servers — left to the caller to gate.
	DriftNeverSynced
)

// DriftStatus compares the current config hash against the last-sync
// hash recorded in the state file. Returns one of the DriftKind
// constants. Does NOT shell out to the agent — this is the cheap
// launch-time check.
func DriftStatus(configPath, statePath string) (DriftKind, error) {
	st, err := LoadState(statePath)
	if err != nil {
		return DriftNone, err
	}
	if st.ConfigHash == "" {
		return DriftNeverSynced, nil
	}
	cur, err := ConfigHash(configPath)
	if err != nil {
		return DriftNone, err
	}
	if cur == "" {
		// Missing config — not our problem at drift level.
		return DriftNone, nil
	}
	if cur != st.ConfigHash {
		return DriftConfigChanged, nil
	}
	return DriftNone, nil
}

// DriftMessage returns a single human-readable line for the banner,
// or "" when there's nothing to say.
func DriftMessage(d DriftKind, contextName string) string {
	switch d {
	case DriftConfigChanged:
		return "⚠ context \"" + contextName + "\": config changed since last sync — run `aide sync`"
	case DriftNeverSynced:
		return "⚠ context \"" + contextName + "\": never synced — run `aide sync` to install declared plugins/MCP servers"
	default:
		return ""
	}
}
```

- [ ] **Step 4: Run tests**

`go test ./internal/provision/ -run TestDrift -v` — expected PASS.

- [ ] **Step 5: Wire into banner**

In `internal/ui/banner.go` (or wherever `RenderBanner` lives), find the render path that follows context resolution. After the existing context line, splice in a drift check. Pseudocode:

```go
import "github.com/jskswamy/aide/internal/provision"
...
configPath := config.FilePath()  // or however config path is resolved here
statePath  := provision.DefaultStatePath(homeDir)
if d, err := provision.DriftStatus(configPath, statePath); err == nil {
    if msg := provision.DriftMessage(d, data.ContextName); msg != "" {
        fmt.Fprintf(w, "%s%s\n", prefix, msg)
    }
}
```

The exact splice point depends on the banner code — locate the existing context-rendering block, add this immediately after.

- [ ] **Step 6: Run banner tests + build**

```bash
go test ./internal/ui/...
go build ./...
```

Expected: PASS / clean build.

- [ ] **Step 7: Commit**

```bash
git add internal/provision/ internal/ui/
git commit -m "Provision: launch-time drift banner via config-hash compare"
```

---

## Tasks 11–17: Per-agent drivers — verified 2026-05-16

Capability research complete: see `docs/specs/2026-05-16-agent-capability-research.md`. Verified ordering and effort:

| Order | Driver  | Tier | Effort | Why this order                                                         |
| ----- | ------- | ---- | ------ | ---------------------------------------------------------------------- |
| 11    | Gemini  | 1    | low    | Cleanest CLI for both plugins and MCP — best test bed for the contract |
| 12    | Copilot | 1    | low    | Same shape as Gemini for plugins; MCP via shared JSON helper           |
| 13    | Claude  | 2    | medium | Path-aware user-scope MCP reader; plugin install needs TTY/fallback    |
| 14    | Codex   | 2    | medium | TOML MCP handler (new format); plugin enable/disable via TOML toggle   |
| 15    | Goose   | 3    | medium | YAML config; unified plugins/MCP under `extensions:` map               |
| 16    | Amp     | 3    | medium | TS file-drop plugins; nested `amp.mcpServers` MCP key                  |
| 17    | Aider   | —    | skip   | No plugin or MCP support — driver returns `false` for both             |

### Prereq before Tier 2/3: refactor the MCP helper

Task 7 left us with `internal/provision/mcpfile.go` assuming JSON + flat `mcpServers` key. Tier 1 (Gemini, Copilot) uses that shape directly. Tier 2 and 3 don't — Claude nests under `projects.<path>.mcpServers`, Amp uses `amp.mcpServers`, Codex is TOML, Goose is YAML with unified extensions.

**Pre-Task-13 refactor:** Move the shared helper into `internal/provision/mcp/`:
- `mcp/jsonflat.go` — current implementation
- `mcp/claudejson.go` — path-aware nested-projects reader
- `mcp/amp.go` — flat JSON, dotted `amp.mcpServers` key
- `mcp/codextoml.go` — TOML parser/writer (`[mcp_servers.<name>]` tables)
- `mcp/gooseyaml.go` — YAML with `extensions:` map; treats stdio/sse/streamable_http entries as MCP

Add a small interface (`MCPHandler`) that each driver picks. Engine code (Task 9) calls through the interface, not the concrete helper.

### Provisioner interface additions

Add to the interface from Task 3:

```go
// RequiresTTY reports whether the agent's plugin install path needs an
// interactive terminal (Claude, Codex). When true, `aide sync --yes`
// fails fast with a clear error instead of letting the subprocess hang.
RequiresTTY() bool
```

Update existing drivers (none yet) to implement it; default `false`.

### Per-driver template

For each driver `internal/provision/agents/<agent>.go` (+ `_test.go`):

1. Declare the struct implementing `provision.Provisioner`.
2. `Name()` returns the agent's canonical name.
3. `SupportsPlugins()`, `SupportsMCP()` return the verified values from the capability matrix.
4. `RequiresTTY()` returns `true` for Claude and Codex, `false` otherwise.
5. `MCPConfigPath(ctx)` returns the agent's verified path (see fact sheets).
6. The driver's MCP-handler choice (which `mcp/*.go` to use) is set in the constructor.
7. `InstalledPlugins(ctx)` shells out to the agent's list command (or scans a directory for Amp; reads the extensions map for Goose).
8. `InstallPlugin`/`UninstallPlugin` use the agent's documented install/uninstall path (CLI shell-out for Gemini/Copilot; file-drop for Amp; YAML edit for Goose; TUI fallback warning for Claude/Codex).
9. Tests use `exec.LookPath` indirection (wrap subprocess calls behind a small `runner` interface for mocking) and skip when the real binary is absent.

### Per-driver verification ledger (cite during implementation)

| Driver  | Plugin install                                          | Plugin uninstall                            | Plugin list                                          | MCP path                                                       | MCP format & top-level key                                  |
| ------- | ------------------------------------------------------- | ------------------------------------------- | ---------------------------------------------------- | -------------------------------------------------------------- | ----------------------------------------------------------- |
| Gemini  | `gemini extensions install <github-url\|--path <dir>>`  | `gemini extensions uninstall <name>`        | `gemini extensions list`                             | `~/.gemini/settings.json`                                      | JSON, `mcpServers`                                          |
| Copilot | `copilot plugin install <name>@<marketplace>`           | `copilot plugin uninstall <name>`           | `copilot plugin list`                                | `~/.copilot/mcp-config.json`                                   | JSON, `mcpServers`                                          |
| Claude  | `claude plugin install <name>@<marketplace>` (TUI-leaning) | `claude plugin uninstall <name>@<marketplace>` | `claude plugin` (interactive UI; no `--json`)    | user: `~/.claude.json` (`projects.<path>.mcpServers`); project: `.mcp.json` (`mcpServers`) | JSON, path-aware                                            |
| Codex   | TUI `/plugins` or `npx codex-marketplace add <ref>`; toggle `enabled` in TOML | toggle `enabled = false` or remove section | `[plugins.*]` sections in `config.toml`              | `~/.codex/config.toml` (project: `.codex/config.toml`)         | TOML, `[mcp_servers.<name>]` tables                         |
| Goose   | edit YAML `extensions.<name>`                            | remove YAML key                              | scan `extensions:` map                                | `~/.config/goose/config.yaml`                                  | YAML, `extensions:` (plugins and MCP unified)               |
| Amp     | write `~/.config/amp/plugins/<name>.ts` (or workspace)   | delete file                                  | dir scan                                              | `~/.config/amp/settings.json`                                  | JSON, **`amp.mcpServers` (dotted key, not nested object)**  |
| Aider   | N/A                                                      | N/A                                          | N/A                                                   | N/A                                                            | N/A                                                         |

### Sandbox path corrections (out of scope here, file separately)

Research surfaced two pre-existing sandbox path bugs that don't block this feature but should be filed:

1. `pkg/seatbelt/modules/amp.go` lists `~/.amp/` as a user default. Per Amp docs, `~/.amp/` is a workspace convention (`.amp/` inside a project), not a user-home location. User config lives at `~/.config/amp/`.
2. `pkg/seatbelt/modules/aider.go` lists `~/.aider/` as a directory default. Aider uses dotfiles in `~` (e.g. `~/.aider.conf.yml`), not a directory.

Track these as separate beads issues; don't conflate with the provisioning feature.

---

## Task 18: `aide sync` command

**Files:**
- Create: `cmd/aide/sync.go`
- Create: `cmd/aide/sync_test.go`

Wire a cobra command that:
1. Loads config + resolves context.
2. Builds Desired from context (resolve plugin/mcp references).
3. Picks the right provisioner from a registry (`provision/registry.go`, populated by each driver's init).
4. Calls `InstalledPlugins` and `ReadMCPFile` to build `Installed`.
5. Loads `ManagedState` from `DefaultStatePath`.
6. Calls `ComputePlan`.
7. Renders the plan.
8. Prompts for confirmation (skipped under `--yes`).
9. Calls `Apply`.
10. Persists new state with updated `ConfigHash` and timestamps.

Code shape too large for this checkbox structure — implement after Tasks 1–10 land and the driver model is concrete.

---

## Task 19: `aide adopt` command

**Files:**
- Create: `cmd/aide/adopt.go`

Walks unmanaged items (computed via plan with empty desired set) and prompts per item. On accept:
1. Writes config.yaml additions via `internal/fsutil.AtomicWrite`.
2. Updates state to mark adopted items as managed.

---

## Task 20: `aide plugin list` / `aide mcp list`

**Files:**
- Create: `cmd/aide/provision_list.go`

Read-only commands that print three-column output: declared, installed, managed. No mutation. Useful before running sync to inspect state.

---

## Self-review

**Spec coverage:**
- Schema (top-level `plugins`, per-context references): Task 1, Task 2 ✓
- Provisioner interface, capability flags: Task 3 ✓
- Managed state file: Task 4 ✓
- Config hash drift: Task 5, Task 10 ✓
- Plan computation (install/update/uninstall/adopt/ignore): Task 6 ✓
- Shared MCP file with `_aide_managed`: Task 7 ✓
- Journal rollback: Task 8 ✓
- Sync engine (abort + rollback on failure): Task 9 ✓
- Launch drift banner: Task 10 ✓
- Per-agent drivers: Tasks 11–17 (deferred for capability-matrix research)
- `aide sync` CLI: Task 18 (after foundation)
- `aide adopt` CLI: Task 19 (after foundation)
- Plan-then-apply UX with confirmation: Task 18 (in CLI layer)
- Adopt config writeback: Task 19
- `aide plugin list` / `aide mcp list`: Task 20

**Type consistency:**
- `Plugin{Key, Source, Name}` used in Tasks 3, 6, 8, 9 ✓
- `MCPServer{Key, Command, URL, Args, Env}` used in Tasks 3, 6, 7, 9 ✓
- `OpKind` constants used in Tasks 3, 6, 9 ✓
- `ContextState{Plugins, MCPServers}` used in Tasks 4, 6 ✓
- `ManagedItem{InstalledAt, Version}` used in Tasks 4, 6 ✓

**Placeholders:** None — every code step has the actual code.

**Scope:** Foundation (Tasks 1–10) is one cohesive PR. Drivers + CLI (Tasks 11–20) split off into a second cycle that depends on Tasks 1–10 + capability-matrix research.
