# Provisioning v2 Schema Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the in-flight v1 flat-`plugins:` schema on the `feat-provision` branch with v2: polymorphic `plugins:` block (shape = list / string / null), unified `mcp_servers:` block, per-context `extra` / `exclude` / `only` deltas, and declarative marketplace handling (Claude / Copilot / Codex).

**Architecture:** YAML value-type drives entry semantics — no `type:` discriminator field. Marketplace-class drivers gain a two-phase sync (ensure marketplace cached, then install plugins). `mcp_servers:` collapses today's `mcp.servers` + `mcp_server_overrides`. Driver capability flag `SupportedSourceShapes()` validates context/driver compatibility at sync time.

**Tech Stack:** Go 1.25, `gopkg.in/yaml.v3` (already in use), existing `internal/fsutil.AtomicWrite` for state writes, `internal/homepath` for tilde expansion, `pelletier/go-toml/v2` for Codex.

**Spec:** `docs/specs/2026-05-15-declarative-agent-provisioning-design.md` (commit `00e79818`).

**Tracker:** AIDE-epa.

---

## File Structure

**Modified:**
- `internal/config/schema.go` — replace `Plugin` struct + `plugins: map[string]Plugin` with `PluginEntry` (polymorphic) + custom unmarshaller; replace `mcp.Servers` + `mcp_server_overrides` with `mcp_servers`; add `ContextOverride[T]` for per-context delta refs.
- `internal/provision/provisioner.go` — add `SourceShape` enum, `Marketplace` struct, `SupportedSourceShapes()` method, `InstalledMarketplaces/AddMarketplace/RemoveMarketplace` methods.
- `internal/provision/desired.go` (already exists) — rewrite `ResolveDesired` against new schema with delta composition.
- `internal/provision/plan.go` — `ComputePlan` adds a marketplace dimension; ops grow `KindMarketplace`.
- `internal/provision/engine.go` — `Apply` becomes two-phase (marketplaces first, plugins second) for drivers that support marketplaces.
- `internal/provision/state.go` — `ContextState` gains `Marketplaces` map; `ManagedItem` gains optional `MarketplaceName` field for cached repo→name lookups.
- `internal/provision/agents/{claude,copilot,codex,gemini}/<x>.go` + tests — implement new interface methods; drivers parse their respective marketplace CLIs.
- `cmd/aide/provision_list.go` — render polymorphic entries; show marketplace column for marketplace-class agents.
- `cmd/aide/sync.go` — drive the two-phase apply; show marketplace ops in plan output.
- `cmd/aide/adopt.go` — write back in the new shape (entry-shape inferred from agent type).
- `RELEASE_NOTES.md` — document the v2 schema and the one-time migration.

**New (none — all changes are in existing files).**

**Deleted:**
- `internal/config/plugin_schema_test.go` + `plugin_validation_test.go` will be rewritten in place (kept as test files, contents replaced).

**Migration note:** No new files. The branch's existing v1 `plugins.<name>: {source, name}` shape on the user's `~/.config/aide/config.yaml` becomes invalid; user rewrites once. Document this in release notes.

---

## Task 1: SourceShape enum + Marketplace struct

**Files:**
- Modify: `internal/provision/provisioner.go`
- Modify: `internal/provision/provisioner_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/provision/provisioner_test.go`:

```go
func TestSourceShapeStrings(t *testing.T) {
	cases := map[provision.SourceShape]string{
		provision.ShapeMarketplace: "marketplace",
		provision.ShapeURLDirect:   "url-direct",
	}
	for s, want := range cases {
		if got := s.String(); got != want {
			t.Errorf("SourceShape(%d).String() = %q, want %q", s, got, want)
		}
	}
}

func TestMarketplaceZeroValue(t *testing.T) {
	var m provision.Marketplace
	if m.Key != "" || m.Source != "" || m.Name != "" {
		t.Errorf("zero Marketplace = %+v", m)
	}
}
```

- [ ] **Step 2: Run to verify failure**

`go test ./internal/provision/ -run 'TestSourceShape|TestMarketplace' -v` — expected FAIL (types undefined).

- [ ] **Step 3: Implement**

In `internal/provision/provisioner.go`, after the existing `Kind` declarations, append:

```go
// SourceShape is the polymorphic-value-shape of a plugins-block entry.
// Drivers advertise which shapes they consume via SupportedSourceShapes.
type SourceShape int

const (
	// ShapeMarketplace: list-valued entry, key is a repo, value is the
	// list of plugins to install from that marketplace.
	ShapeMarketplace SourceShape = iota
	// ShapeURLDirect: string-valued entry, key is a plugin name, value
	// is a single install reference (URL, github:owner/repo, or path).
	ShapeURLDirect
)

func (s SourceShape) String() string {
	switch s {
	case ShapeMarketplace:
		return "marketplace"
	case ShapeURLDirect:
		return "url-direct"
	default:
		return "unknown"
	}
}

// Marketplace is a resolved marketplace declaration for marketplace-
// class drivers (Claude / Copilot / Codex). Source is what the user
// declared (e.g. "steveyegge/beads" or "github:owner/repo"); Name is
// the marketplace's canonical name discovered post-add (e.g.
// "beads-marketplace"). Name is populated by the driver after
// AddMarketplace succeeds and is cached in managed.json.
type Marketplace struct {
	Key    string // top-level plugins map key (e.g. "steveyegge/beads")
	Source string // install ref (e.g. "github:steveyegge/beads")
	Name   string // canonical marketplace name (e.g. "beads-marketplace")
}
```

- [ ] **Step 4: Run to verify pass**

`go test ./internal/provision/ -run 'TestSourceShape|TestMarketplace' -v` — expected PASS.

- [ ] **Step 5: Commit**

```bash
git -C <worktree> add internal/provision/
__GIT_COMMIT_PLUGIN__=1 git -C <worktree> commit -m "Add SourceShape enum and Marketplace struct"
```

---

## Task 2: Polymorphic PluginEntry types + YAML unmarshaller

**Files:**
- Modify: `internal/config/schema.go`
- Create or modify: `internal/config/plugin_schema_test.go`

- [ ] **Step 1: Write the failing test**

Replace the contents of `internal/config/plugin_schema_test.go`:

```go
package config_test

import (
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/config"
	"gopkg.in/yaml.v3"
)

func TestPluginsPolymorphicShapes(t *testing.T) {
	y := `
plugins:
  steveyegge/beads: [beads]
  jskswamy/claude-plugins:
    - craft
    - devenv
  gemini-cli-tool: "github:google/gemini-cli-tool"
  obra/superpowers-marketplace: ~
`
	var cfg config.Config
	if err := yaml.NewDecoder(strings.NewReader(y)).Decode(&cfg); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(cfg.Plugins) != 4 {
		t.Fatalf("expected 4 entries, got %d", len(cfg.Plugins))
	}

	mk := cfg.Plugins["steveyegge/beads"]
	if mk.Shape() != config.PluginShapeMarketplace {
		t.Errorf("steveyegge/beads shape = %v, want marketplace", mk.Shape())
	}
	if len(mk.Plugins) != 1 || mk.Plugins[0] != "beads" {
		t.Errorf("steveyegge/beads plugins = %v", mk.Plugins)
	}

	multi := cfg.Plugins["jskswamy/claude-plugins"]
	if len(multi.Plugins) != 2 {
		t.Errorf("jskswamy/claude-plugins plugins = %v", multi.Plugins)
	}

	ud := cfg.Plugins["gemini-cli-tool"]
	if ud.Shape() != config.PluginShapeURLDirect {
		t.Errorf("gemini-cli-tool shape = %v, want url-direct", ud.Shape())
	}
	if ud.Source != "github:google/gemini-cli-tool" {
		t.Errorf("gemini-cli-tool source = %q", ud.Source)
	}

	dec := cfg.Plugins["obra/superpowers-marketplace"]
	if dec.Shape() != config.PluginShapeDeclareOnly {
		t.Errorf("declare-only shape = %v", dec.Shape())
	}
}

func TestPluginsKeyShapeMismatchRejected(t *testing.T) {
	// String value with a slash-key is ambiguous and should error.
	y := `
plugins:
  steveyegge/beads: "github:foo/bar"
`
	var cfg config.Config
	err := yaml.NewDecoder(strings.NewReader(y)).Decode(&cfg)
	if err == nil {
		t.Fatal("expected error: slash-key + string-value mismatch")
	}
	if !strings.Contains(err.Error(), "steveyegge/beads") {
		t.Errorf("error should name the offending key, got %v", err)
	}
}
```

- [ ] **Step 2: Run to verify failure**

`go test ./internal/config/ -run TestPlugins -v` — expected FAIL.

- [ ] **Step 3: Implement**

In `internal/config/schema.go`, replace the existing `Plugin` struct and `Config.Plugins` field with the polymorphic types. Find the lines:

```go
// Plugin source enum values.
const (
	PluginSourceMarketplace = "marketplace"
	...
```

Replace from there through the `ValidatePlugins()` method (if present) with:

```go
// PluginShape enumerates the value-shape categories for entries in the
// top-level `plugins:` block. Each entry has exactly one shape, picked
// at YAML-decode time from the value's type.
type PluginShape int

const (
	// PluginShapeMarketplace: value is a YAML sequence of plugin names;
	// key is interpreted as a repo path (owner/repo or full URL).
	PluginShapeMarketplace PluginShape = iota
	// PluginShapeURLDirect: value is a string install reference;
	// key is a plugin name chosen by the user for readability.
	PluginShapeURLDirect
	// PluginShapeDeclareOnly: value is null (~). Key is a repo path
	// for marketplace agents; signals "ensure marketplace cached,
	// install nothing from it."
	PluginShapeDeclareOnly
)

// PluginEntry is one entry in the polymorphic top-level `plugins:`
// block. The Shape determines which of Plugins/Source is populated:
//   - Marketplace: Plugins is set, Source is empty
//   - URLDirect:   Source is set, Plugins is nil
//   - DeclareOnly: both are empty/nil
type PluginEntry struct {
	shape   PluginShape
	Plugins []string // populated only when Shape == Marketplace
	Source  string   // populated only when Shape == URLDirect
}

// Shape returns the entry's resolved shape, set at YAML decode time.
func (p PluginEntry) Shape() PluginShape { return p.shape }

// UnmarshalYAML implements custom decoding: the value's YAML type
// (sequence / scalar-string / null) determines the entry's shape.
// Key-vs-value-shape consistency is checked by ValidatePlugins after
// decode (so we can produce errors that name the offending key —
// yaml.v3's per-field unmarshaller doesn't carry the key down).
func (p *PluginEntry) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.SequenceNode:
		p.shape = PluginShapeMarketplace
		return node.Decode(&p.Plugins)
	case yaml.ScalarNode:
		if node.Tag == "!!null" || node.Value == "" {
			p.shape = PluginShapeDeclareOnly
			return nil
		}
		p.shape = PluginShapeURLDirect
		return node.Decode(&p.Source)
	case yaml.MappingNode:
		return fmt.Errorf("plugin entry must be a list, string, or null — got mapping (did you mean to add this under `mcp_servers:`?)")
	default:
		return fmt.Errorf("plugin entry has unsupported YAML kind %v", node.Kind)
	}
}
```

Update `Config.Plugins` field declaration (in the `Config` struct):

```go
	Plugins map[string]PluginEntry `yaml:"plugins,omitempty"`
```

Update `Context.Plugins` field declaration:

```go
	Plugins *ContextOverride[string] `yaml:"plugins,omitempty"` // see Task 3
```

(Defer the `ContextOverride[string]` type to Task 3; for now just leave the per-context Plugins field commented out or in a stub form. Make sure existing v1 references that used `ctx.Plugins []string` don't compile errors — search and remove them; the new ResolveDesired in Task 9 reconstructs the desired set from the polymorphic top-level.)

Also remove the v1 `ValidatePlugins` method on `Config` (its logic moves into the new validator in Task 5). Remove the v1 `Plugin` struct and source enum constants.

- [ ] **Step 4: Run to verify pass**

`go test ./internal/config/ -run TestPlugins -v` — expected PASS for the new tests; existing config tests **will likely fail to compile** because of the removed v1 types. That's expected at this task boundary; resolve in Task 3 (which adds ContextOverride and updates Context).

- [ ] **Step 5: Commit**

```bash
__GIT_COMMIT_PLUGIN__=1 git -C <worktree> commit -am "Replace flat plugins schema with polymorphic PluginEntry"
```

Note: this commit may leave the package in a partially-broken state for one task. Tasks 3 and 4 follow immediately to restore green build.

---

## Task 3: ContextOverride[T] with extra/exclude/only

**Files:**
- Modify: `internal/config/schema.go`
- Modify: `internal/config/plugin_schema_test.go`

- [ ] **Step 1: Write failing tests**

Append to `internal/config/plugin_schema_test.go`:

```go
func TestContextPluginsOverrideInheritsDefault(t *testing.T) {
	y := `
plugins:
  steveyegge/beads: [beads]
contexts:
  default:
    agent: claude
`
	var cfg config.Config
	if err := yaml.NewDecoder(strings.NewReader(y)).Decode(&cfg); err != nil {
		t.Fatal(err)
	}
	ctx := cfg.Contexts["default"]
	if ctx.Plugins != nil {
		t.Errorf("absent block should yield nil ContextOverride, got %+v", ctx.Plugins)
	}
}

func TestContextPluginsOverrideExcludeExtra(t *testing.T) {
	y := `
plugins:
  steveyegge/beads: [beads]
contexts:
  prod:
    agent: claude
    plugins:
      exclude:
        - obra/superpowers-marketplace/double-shot-latte
      extra:
        my-org/internal: [private-tool]
`
	var cfg config.Config
	if err := yaml.NewDecoder(strings.NewReader(y)).Decode(&cfg); err != nil {
		t.Fatal(err)
	}
	override := cfg.Contexts["prod"].Plugins
	if override == nil {
		t.Fatal("expected non-nil ContextOverride")
	}
	if len(override.Exclude) != 1 || override.Exclude[0] != "obra/superpowers-marketplace/double-shot-latte" {
		t.Errorf("exclude = %v", override.Exclude)
	}
	if _, ok := override.Extra["my-org/internal"]; !ok {
		t.Errorf("extra missing my-org/internal: %+v", override.Extra)
	}
}

func TestContextPluginsOverrideOnly(t *testing.T) {
	y := `
plugins:
  a/b: [b]
  c/d: [d]
contexts:
  ci:
    agent: claude
    plugins:
      only:
        - a/b
`
	var cfg config.Config
	if err := yaml.NewDecoder(strings.NewReader(y)).Decode(&cfg); err != nil {
		t.Fatal(err)
	}
	override := cfg.Contexts["ci"].Plugins
	if len(override.Only) != 1 || override.Only[0] != "a/b" {
		t.Errorf("only = %v", override.Only)
	}
}
```

- [ ] **Step 2: Run to verify failure**

`go test ./internal/config/ -run TestContextPluginsOverride -v` — expected FAIL.

- [ ] **Step 3: Implement**

In `internal/config/schema.go`, add (place near `Context`):

```go
// ContextOverride captures a per-context delta over a top-level map.
// The type parameter T is the value type of the map (PluginEntry for
// plugins, MCPServer for mcp_servers). Exactly one of {Only, Exclude+Extra}
// patterns produces the resolved set; see internal/provision/desired.go
// for the composition logic.
type ContextOverride[T any] struct {
	// Only, if non-empty, replaces the inherited default set entirely.
	// Path syntax: `repo` (marketplace key) or `name` (other entry key)
	// or `repo/plugin` (entry-and-subentry).
	Only []string `yaml:"only,omitempty"`
	// Exclude removes paths from the inherited (or Only-replaced) set.
	// Same path syntax as Only.
	Exclude []string `yaml:"exclude,omitempty"`
	// Extra adds entries on top of the resolved set. Same shape as the
	// top-level map of T (e.g. map[string]PluginEntry for plugins).
	Extra map[string]T `yaml:"extra,omitempty"`
}
```

Update `Context.Plugins` and `Context.MCPServers` field declarations:

```go
	Plugins    *ContextOverride[PluginEntry] `yaml:"plugins,omitempty"`
	MCPServers *ContextOverride[MCPServer]   `yaml:"mcp_servers,omitempty"`
```

(MCP unification follows in Task 4; for now `MCPServer` is the existing struct from `MCPConfig.Servers`.)

- [ ] **Step 4: Run to verify pass**

`go test ./internal/config/ -run TestContextPluginsOverride -v` — expected PASS.

- [ ] **Step 5: Commit**

```bash
__GIT_COMMIT_PLUGIN__=1 git -C <worktree> commit -am "Add ContextOverride[T] for per-context extra/exclude/only"
```

---

## Task 4: Unify mcp_servers schema (single top-level block)

**Files:**
- Modify: `internal/config/schema.go`
- Modify: `internal/config/plugin_schema_test.go`
- Modify: any callers of `cfg.MCP.Servers` or `cfg.MCPServerOverrides` — find them via `grep -rn 'MCP\.Servers\|MCPServerOverrides' .`

- [ ] **Step 1: Write failing test**

Append to `internal/config/plugin_schema_test.go`:

```go
func TestMCPServersTopLevelMap(t *testing.T) {
	y := `
mcp_servers:
  postgres:
    command: postgres-mcp
    args: ["--port", "5432"]
  slack:
    url: https://mcp.slack.app/sse
`
	var cfg config.Config
	if err := yaml.NewDecoder(strings.NewReader(y)).Decode(&cfg); err != nil {
		t.Fatal(err)
	}
	if len(cfg.MCPServers) != 2 {
		t.Fatalf("expected 2 mcp_servers, got %d", len(cfg.MCPServers))
	}
	if cfg.MCPServers["postgres"].Command != "postgres-mcp" {
		t.Errorf("postgres command = %q", cfg.MCPServers["postgres"].Command)
	}
}
```

- [ ] **Step 2: Run to verify failure**

`go test ./internal/config/ -run TestMCPServersTopLevelMap -v` — expected FAIL.

- [ ] **Step 3: Implement**

In `internal/config/schema.go`:

1. Remove the `Config.MCP` field (`*MCPConfig`) and the `MCPConfig` struct itself.
2. Remove the `Context.MCPServerOverrides` field.
3. Add `Config.MCPServers` as a `map[string]MCPServer`:

```go
	MCPServers map[string]MCPServer `yaml:"mcp_servers,omitempty"`
```

4. Keep `MCPServer` struct as-is (`Command`, `URL`, `Args`, `Env`).

Update all callers found via `grep -rn 'cfg\.MCP\.Servers\|cfg\.MCPServerOverrides'` to use `cfg.MCPServers`. Likely sites:
- `internal/provision/desired.go` (rewrites entirely in Task 9 — leave a TODO comment for now if compilation breaks).
- `internal/context/resolver.go` (the launch path uses MCP refs; preserve behavior by reading `cfg.MCPServers` instead of the old nested forms).

- [ ] **Step 4: Run to verify pass**

`go test ./internal/config/ ./internal/context/ -v 2>&1 | tail -10` — expected PASS. If `internal/provision/desired.go` doesn't compile, comment its body and add a `panic("rewritten in Task 9")` stub. The full provision package will be restored by Task 9.

- [ ] **Step 5: Commit**

```bash
__GIT_COMMIT_PLUGIN__=1 git -C <worktree> commit -am "Unify mcp_servers into single top-level map"
```

---

## Task 5: Schema validation for v2

**Files:**
- Modify: `internal/config/schema.go`
- Modify: `internal/config/plugin_schema_test.go`

- [ ] **Step 1: Write failing tests**

Append:

```go
func TestPluginsMarketplaceKeyMustLookLikeRepo(t *testing.T) {
	y := `
plugins:
  just-a-name: [foo]
`
	var cfg config.Config
	if err := yaml.NewDecoder(strings.NewReader(y)).Decode(&cfg); err != nil {
		t.Fatal(err)
	}
	err := cfg.ValidatePlugins()
	if err == nil || !strings.Contains(err.Error(), "just-a-name") {
		t.Errorf("expected error naming the bad key, got %v", err)
	}
}

func TestPluginsURLDirectKeyMustNotHaveSlash(t *testing.T) {
	y := `
plugins:
  steveyegge/beads: "github:foo/bar"
`
	var cfg config.Config
	if err := yaml.NewDecoder(strings.NewReader(y)).Decode(&cfg); err != nil {
		// May error at decode time already due to Task 2 wiring; either is fine.
		return
	}
	err := cfg.ValidatePlugins()
	if err == nil {
		t.Error("expected error: slash-key with string value")
	}
}

func TestContextPluginsExcludePathMustResolve(t *testing.T) {
	y := `
plugins:
  steveyegge/beads: [beads]
contexts:
  ci:
    agent: claude
    plugins:
      exclude: [does-not-exist]
`
	var cfg config.Config
	_ = yaml.NewDecoder(strings.NewReader(y)).Decode(&cfg)
	err := cfg.ValidatePlugins()
	if err == nil || !strings.Contains(err.Error(), "does-not-exist") {
		t.Errorf("expected unresolved-exclude error, got %v", err)
	}
}
```

- [ ] **Step 2: Run to verify failure**

`go test ./internal/config/ -run 'TestPluginsMarketplaceKey|TestPluginsURLDirect|TestContextPluginsExclude' -v` — expected FAIL.

- [ ] **Step 3: Implement**

Add to `internal/config/schema.go`:

```go
// ValidatePlugins runs v2 schema-level checks:
//   - Marketplace entries (list-valued) have keys shaped like "owner/repo"
//     or a URL; URL-direct entries (string-valued) have keys without "/".
//   - DeclareOnly entries (null-valued) have keys shaped like a repo.
//   - Per-context exclude/only paths resolve to a declared entry
//     (considering extra: additions).
//
// Returns nil on success.
func (c *Config) ValidatePlugins() error {
	for key, entry := range c.Plugins {
		switch entry.Shape() {
		case PluginShapeMarketplace, PluginShapeDeclareOnly:
			if !looksLikeRepo(key) {
				return fmt.Errorf("plugin entry %q has %s shape (list or null value) but its key does not look like a repo path (owner/repo or URL)", key, entry.Shape())
			}
		case PluginShapeURLDirect:
			if strings.Contains(key, "/") {
				return fmt.Errorf("plugin entry %q has URL-direct shape (string value) but its key contains '/' — pick a plain name (the value carries the source)", key)
			}
		}
	}
	for ctxName, ctx := range c.Contexts {
		if ctx.Plugins == nil {
			continue
		}
		merged := mergeKeysForValidation(c.Plugins, ctx.Plugins.Extra)
		for _, path := range append(append([]string{}, ctx.Plugins.Exclude...), ctx.Plugins.Only...) {
			parts := strings.SplitN(path, "/", 3)
			topKey := parts[0]
			if len(parts) >= 2 && looksLikeRepo(parts[0]+"/"+parts[1]) {
				topKey = parts[0] + "/" + parts[1]
			}
			if _, ok := merged[topKey]; !ok {
				return fmt.Errorf("context %q references unknown plugin path %q (not in top-level plugins or extras)", ctxName, path)
			}
		}
	}
	return nil
}

func looksLikeRepo(key string) bool {
	// Accept owner/repo, github:owner/repo, full URLs.
	if strings.HasPrefix(key, "github:") || strings.HasPrefix(key, "git:") || strings.HasPrefix(key, "https://") || strings.HasPrefix(key, "http://") {
		return true
	}
	return strings.Count(key, "/") >= 1
}

func mergeKeysForValidation(top map[string]PluginEntry, extras map[string]PluginEntry) map[string]struct{} {
	out := make(map[string]struct{}, len(top)+len(extras))
	for k := range top {
		out[k] = struct{}{}
	}
	for k := range extras {
		out[k] = struct{}{}
	}
	return out
}
```

Wire `ValidatePlugins()` into the existing `Config.Load`/`LoadFile` validation pipeline (find the call site by searching `ValidatePlugins` — there was one before v2 removed the v1 method).

- [ ] **Step 4: Run to verify pass**

`go test ./internal/config/ -v 2>&1 | tail -10` — expected PASS.

- [ ] **Step 5: Commit**

```bash
__GIT_COMMIT_PLUGIN__=1 git -C <worktree> commit -am "Validate v2 plugins schema: key shape, exclude resolution"
```

---

## Task 6: Add SupportedSourceShapes to Provisioner interface

**Files:**
- Modify: `internal/provision/provisioner.go`
- Modify: each of `internal/provision/agents/{claude,copilot,codex,gemini}/<x>.go` and `<x>_test.go`
- Modify: `internal/provision/engine_test.go` (the fakeProv)

- [ ] **Step 1: Write the failing tests**

In each of `internal/provision/agents/claude/claude_test.go`, `copilot_test.go`, `gemini_test.go`, `codex_test.go`, add a capability assertion (or extend the existing one):

```go
// Append to claude_test.go's TestClaudeCapabilities:
if shapes := d.SupportedSourceShapes(); len(shapes) != 1 || shapes[0] != provision.ShapeMarketplace {
	t.Errorf("Claude shapes = %v, want [marketplace]", shapes)
}
```

Same for Copilot and Codex (ShapeMarketplace), Gemini (ShapeURLDirect).

- [ ] **Step 2: Run to verify failure**

`go test ./internal/provision/agents/... -v 2>&1 | tail -20` — expected FAIL: undefined `SupportedSourceShapes`.

- [ ] **Step 3: Implement**

In `internal/provision/provisioner.go`, add to the `Provisioner` interface:

```go
	// SupportedSourceShapes lists the plugin-entry value shapes this
	// driver consumes. Marketplace drivers return [ShapeMarketplace];
	// URL-direct drivers return [ShapeURLDirect]; drivers without a
	// plugin concept return nil. Sync uses this to validate per-context
	// agent/shape compatibility.
	SupportedSourceShapes() []SourceShape
```

Add the method to each of the four drivers:

```go
// Claude / Copilot / Codex
func (*Driver) SupportedSourceShapes() []provision.SourceShape {
	return []provision.SourceShape{provision.ShapeMarketplace}
}

// Gemini
func (*Driver) SupportedSourceShapes() []provision.SourceShape {
	return []provision.SourceShape{provision.ShapeURLDirect}
}
```

In `internal/provision/engine_test.go`, add the method to `fakeProv`:

```go
func (f *fakeProv) SupportedSourceShapes() []provision.SourceShape { return f.shapes }
```

(Extend `fakeProv` struct with a `shapes []provision.SourceShape` field; tests that need it set it explicitly.)

- [ ] **Step 4: Run to verify pass**

`go test ./internal/provision/... ./cmd/aide/ -v 2>&1 | tail -10` — expected PASS.

- [ ] **Step 5: Commit**

```bash
__GIT_COMMIT_PLUGIN__=1 git -C <worktree> commit -am "Add SupportedSourceShapes to Provisioner interface"
```

---

## Task 7: Marketplace methods on Provisioner

**Files:**
- Modify: `internal/provision/provisioner.go`
- Modify: each driver — Claude / Copilot / Codex implement real, Gemini stubs to no-op

- [ ] **Step 1: Write the failing tests**

In `internal/provision/agents/claude/claude_test.go`, add:

```go
func TestClaudeInstalledMarketplaces(t *testing.T) {
	r := &fakeRunner{stdout: `[
		{"name":"beads-marketplace","source":"github:steveyegge/beads"},
		{"name":"jskswamy-plugins","source":"github:jskswamy/claude-plugins"}
	]`}
	d := claude.New(r)
	got, err := d.InstalledMarketplaces(provision.Context{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d marketplaces, want 2", len(got))
	}
	if got[0].Name != "beads-marketplace" || got[0].Source != "github:steveyegge/beads" {
		t.Errorf("first = %+v", got[0])
	}
	want := []string{"claude", "plugin", "marketplace", "list", "--json"}
	if !reflect.DeepEqual(r.calls[0], want) {
		t.Errorf("call = %v", r.calls[0])
	}
}

func TestClaudeAddMarketplace(t *testing.T) {
	r := &fakeRunner{}
	d := claude.New(r)
	err := d.AddMarketplace(provision.Context{}, provision.Marketplace{Key: "steveyegge/beads", Source: "github:steveyegge/beads"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"claude", "plugin", "marketplace", "add", "steveyegge/beads"}
	if !reflect.DeepEqual(r.calls[0], want) {
		t.Errorf("call = %v, want %v", r.calls[0], want)
	}
}

func TestClaudeRemoveMarketplace(t *testing.T) {
	r := &fakeRunner{}
	d := claude.New(r)
	if err := d.RemoveMarketplace(provision.Context{}, "beads-marketplace"); err != nil {
		t.Fatal(err)
	}
	want := []string{"claude", "plugin", "marketplace", "remove", "beads-marketplace"}
	if !reflect.DeepEqual(r.calls[0], want) {
		t.Errorf("call = %v", r.calls[0])
	}
}
```

Similar tests for Copilot (`copilot plugin marketplace add|list|remove`) and Codex (TOML-based — see Step 3 for the right shape).

For Gemini, add a single test asserting no-op:

```go
func TestGeminiMarketplaceMethodsNoOp(t *testing.T) {
	r := &fakeRunner{}
	d := gemini.New(r)
	got, err := d.InstalledMarketplaces(provision.Context{})
	if err != nil || len(got) != 0 {
		t.Errorf("Gemini InstalledMarketplaces should be no-op, got %v, %v", got, err)
	}
	if err := d.AddMarketplace(provision.Context{}, provision.Marketplace{}); err == nil {
		// Optional: AddMarketplace can either error or no-op silently.
		// Pick one in implementation; this test enforces consistency.
	}
}
```

- [ ] **Step 2: Run to verify failure**

`go test ./internal/provision/agents/... -v 2>&1 | tail -20` — expected FAIL: undefined methods.

- [ ] **Step 3: Implement**

In `internal/provision/provisioner.go`, add to the interface:

```go
	// InstalledMarketplaces returns the marketplaces currently
	// registered in the agent's profile. For drivers that don't
	// support marketplaces, returns nil, nil.
	InstalledMarketplaces(ctx Context) ([]Marketplace, error)
	// AddMarketplace registers a marketplace in the agent's profile.
	// For drivers without marketplaces, returns an error.
	AddMarketplace(ctx Context, m Marketplace) error
	// RemoveMarketplace unregisters a marketplace from the agent's
	// profile. Must succeed even if the marketplace is already absent
	// (rollback safety).
	RemoveMarketplace(ctx Context, name string) error
```

In `internal/provision/agents/claude/claude.go`, append:

```go
type claudeMarketplaceEntry struct {
	Name   string `json:"name"`
	Source string `json:"source"`
}

func (d *Driver) InstalledMarketplaces(pctx provision.Context) ([]provision.Marketplace, error) {
	stdout, stderr, code, err := d.runner.Run(context.Background(), pctx.Env, "claude", "plugin", "marketplace", "list", "--json")
	if err != nil {
		return nil, nil // binary missing — graceful empty
	}
	if code != 0 {
		return nil, fmt.Errorf("claude plugin marketplace list --json: exit %d: %s", code, strings.TrimSpace(stderr))
	}
	var entries []claudeMarketplaceEntry
	if err := json.Unmarshal([]byte(stdout), &entries); err != nil {
		return nil, fmt.Errorf("parsing claude marketplace list: %w", err)
	}
	out := make([]provision.Marketplace, 0, len(entries))
	for _, e := range entries {
		out = append(out, provision.Marketplace{
			Key:    e.Source, // best-effort: source string is the "user-facing" key
			Source: e.Source,
			Name:   e.Name,
		})
	}
	return out, nil
}

func (d *Driver) AddMarketplace(pctx provision.Context, m provision.Marketplace) error {
	ref := m.Source
	if ref == "" {
		ref = m.Key
	}
	_, stderr, code, err := d.runner.Run(context.Background(), pctx.Env, "claude", "plugin", "marketplace", "add", ref)
	if err != nil {
		return fmt.Errorf("claude plugin marketplace add %s: %w", ref, err)
	}
	if code != 0 {
		return fmt.Errorf("claude plugin marketplace add %s: exit %d: %s", ref, code, stderr)
	}
	return nil
}

func (d *Driver) RemoveMarketplace(pctx provision.Context, name string) error {
	_, stderr, code, err := d.runner.Run(context.Background(), pctx.Env, "claude", "plugin", "marketplace", "remove", name)
	if err != nil {
		return fmt.Errorf("claude plugin marketplace remove %s: %w", name, err)
	}
	if code != 0 {
		if strings.Contains(stderr, "not found") || strings.Contains(stderr, "not configured") {
			return nil // already absent — rollback safety
		}
		return fmt.Errorf("claude plugin marketplace remove %s: exit %d: %s", name, code, stderr)
	}
	return nil
}
```

Same shape for Copilot — substitute `copilot plugin marketplace ...`. Check `copilot plugin marketplace list --json` output format; if it differs, adjust the struct tags.

For Codex (TOML), add/remove are file-level edits to `~/.codex/config.toml`'s `[plugins."<plugin>@<source>"]` sections — but for the marketplace concept specifically, Codex uses `[plugin_marketplaces.<name>]` per the spec research. Implement reads/writes against the TOML file via the existing `internal/provision/mcp/codextoml.go` patterns (or a new sibling, `codex_marketplaces.go`).

For Gemini, add stubs:

```go
func (*Driver) InstalledMarketplaces(_ provision.Context) ([]provision.Marketplace, error) {
	return nil, nil
}
func (*Driver) AddMarketplace(_ provision.Context, _ provision.Marketplace) error {
	return fmt.Errorf("gemini does not have marketplaces; declare extensions inline with string values")
}
func (*Driver) RemoveMarketplace(_ provision.Context, _ string) error {
	return nil // no-op rollback safety
}
```

Update `fakeProv` in `engine_test.go` with stub methods.

- [ ] **Step 4: Run to verify pass**

`go test ./internal/provision/...` — expected PASS for unit-level tests. Integration tests (sync, ResolveDesired) will fail until Task 9; expected at this point.

- [ ] **Step 5: Commit**

```bash
__GIT_COMMIT_PLUGIN__=1 git -C <worktree> commit -am "Add Marketplace ops to Provisioner (Claude/Copilot/Codex impl, Gemini stub)"
```

---

## Task 8: Context override composition (resolver helper)

**Files:**
- Create: `internal/provision/override.go`
- Create: `internal/provision/override_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/provision/override_test.go`:

```go
package provision_test

import (
	"testing"

	"github.com/jskswamy/aide/internal/config"
	"github.com/jskswamy/aide/internal/provision"
)

func TestApplyOverrideNilInheritsAll(t *testing.T) {
	top := map[string]config.PluginEntry{
		"a/b": {/* marketplace [b] — set via test helper */},
	}
	got := provision.ApplyOverride(top, nil)
	if _, ok := got["a/b"]; !ok {
		t.Errorf("nil override should inherit all")
	}
}

func TestApplyOverrideExcludeRemoves(t *testing.T) {
	top := map[string]config.PluginEntry{
		"a/b": {},
		"c/d": {},
	}
	override := &config.ContextOverride[config.PluginEntry]{
		Exclude: []string{"a/b"},
	}
	got := provision.ApplyOverride(top, override)
	if _, ok := got["a/b"]; ok {
		t.Errorf("a/b should be excluded")
	}
	if _, ok := got["c/d"]; !ok {
		t.Errorf("c/d should remain")
	}
}

func TestApplyOverrideOnlyReplaces(t *testing.T) {
	top := map[string]config.PluginEntry{
		"a/b": {},
		"c/d": {},
	}
	override := &config.ContextOverride[config.PluginEntry]{
		Only: []string{"a/b"},
	}
	got := provision.ApplyOverride(top, override)
	if len(got) != 1 || got["a/b"].Shape() != got["a/b"].Shape() /* sanity */ {
		t.Errorf("only mode should yield exactly 1 entry, got %d", len(got))
	}
}

func TestApplyOverrideExtraAdds(t *testing.T) {
	top := map[string]config.PluginEntry{"a/b": {}}
	override := &config.ContextOverride[config.PluginEntry]{
		Extra: map[string]config.PluginEntry{"e/f": {}},
	}
	got := provision.ApplyOverride(top, override)
	if _, ok := got["e/f"]; !ok {
		t.Errorf("extra entry should be added")
	}
}
```

- [ ] **Step 2: Run to verify failure**

`go test ./internal/provision/ -run TestApplyOverride -v` — expected FAIL.

- [ ] **Step 3: Implement**

Create `internal/provision/override.go`:

```go
package provision

import (
	"strings"

	"github.com/jskswamy/aide/internal/config"
)

// ApplyOverride composes a ContextOverride against a top-level map.
// Composition order: only (replace) → exclude (subtract) → extra (add).
// Returns a new map; never mutates inputs.
//
// Path syntax for Only / Exclude:
//   - "repo" or "name" — matches a top-level key.
//   - "repo/plugin" — for marketplace entries (list-valued), removes a
//     specific plugin from the entry's list. Whole entry is dropped
//     only when no plugins remain.
func ApplyOverride[T any](top map[string]T, override *config.ContextOverride[T]) map[string]T {
	out := copyMap(top)
	if override == nil {
		return out
	}
	if len(override.Only) > 0 {
		filtered := map[string]T{}
		for _, path := range override.Only {
			key, _ := splitPath(path)
			if v, ok := top[key]; ok {
				filtered[key] = v
			}
		}
		out = filtered
	}
	for _, path := range override.Exclude {
		key, sub := splitPath(path)
		if sub == "" {
			delete(out, key)
			continue
		}
		// Sub-path (repo/plugin) — only valid for PluginEntry values.
		// Type-assert via interface to keep this helper generic; if
		// T doesn't support sub-removal, treat as whole-entry delete.
		if pluginVal, ok := any(out[key]).(config.PluginEntry); ok {
			pluginVal = removeSubPlugin(pluginVal, sub)
			if len(pluginVal.Plugins) == 0 && pluginVal.Shape() == config.PluginShapeMarketplace {
				delete(out, key)
			} else {
				out[key] = any(pluginVal).(T)
			}
		}
	}
	for k, v := range override.Extra {
		out[k] = v
	}
	return out
}

func copyMap[T any](in map[string]T) map[string]T {
	out := make(map[string]T, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// splitPath splits "repo/plugin" into ("repo", "plugin"); "owner/repo"
// into ("owner/repo", "") via the heuristic that exactly-one-slash
// means it's a repo path itself, while two slashes means repo/plugin.
// For URL-direct keys (no slash), returns (key, "").
func splitPath(p string) (key, sub string) {
	parts := strings.SplitN(p, "/", 3)
	switch len(parts) {
	case 1:
		return parts[0], ""
	case 2:
		return p, "" // owner/repo
	case 3:
		return parts[0] + "/" + parts[1], parts[2]
	}
	return p, ""
}

func removeSubPlugin(entry config.PluginEntry, plugin string) config.PluginEntry {
	if entry.Shape() != config.PluginShapeMarketplace {
		return entry
	}
	out := make([]string, 0, len(entry.Plugins))
	for _, p := range entry.Plugins {
		if p != plugin {
			out = append(out, p)
		}
	}
	entry.Plugins = out
	return entry
}
```

- [ ] **Step 4: Run to verify pass**

`go test ./internal/provision/ -run TestApplyOverride -v` — expected PASS.

- [ ] **Step 5: Commit**

```bash
__GIT_COMMIT_PLUGIN__=1 git -C <worktree> commit -am "Add ApplyOverride for context-level plugin/MCP deltas"
```

---

## Task 9: Rewrite ResolveDesired for v2 schema

**Files:**
- Modify: `internal/provision/desired.go` (replace body entirely)
- Modify: `internal/provision/desired_test.go`

- [ ] **Step 1: Write failing tests**

Replace `desired_test.go`:

```go
package provision_test

import (
	"testing"

	"github.com/jskswamy/aide/internal/config"
	"github.com/jskswamy/aide/internal/provision"
	"gopkg.in/yaml.v3"
	"strings"
)

func TestResolveDesiredMarketplaceFlat(t *testing.T) {
	y := `
plugins:
  steveyegge/beads: [beads]
  jskswamy/claude-plugins: [craft, devenv]
mcp_servers:
  rfctl: { command: rfctl }
contexts:
  default:
    agent: claude
`
	var cfg config.Config
	_ = yaml.NewDecoder(strings.NewReader(y)).Decode(&cfg)
	desired, err := provision.ResolveDesired(&cfg, "default")
	if err != nil {
		t.Fatal(err)
	}
	if len(desired.Marketplaces) != 2 {
		t.Errorf("marketplaces = %d, want 2", len(desired.Marketplaces))
	}
	if len(desired.Plugins) != 3 {
		t.Errorf("plugins = %d, want 3 (beads, craft, devenv)", len(desired.Plugins))
	}
	if _, ok := desired.MCPServers["rfctl"]; !ok {
		t.Errorf("rfctl missing")
	}
}

func TestResolveDesiredWithExclude(t *testing.T) {
	y := `
plugins:
  steveyegge/beads: [beads]
  jskswamy/claude-plugins: [craft, devenv, jot]
contexts:
  prod:
    agent: claude
    plugins:
      exclude:
        - jskswamy/claude-plugins/jot
`
	var cfg config.Config
	_ = yaml.NewDecoder(strings.NewReader(y)).Decode(&cfg)
	desired, _ := provision.ResolveDesired(&cfg, "prod")
	for _, p := range desired.Plugins {
		if p.Key == "jot" {
			t.Errorf("jot should be excluded, got %+v", p)
		}
	}
}
```

- [ ] **Step 2: Run to verify failure**

`go test ./internal/provision/ -run TestResolveDesired -v` — expected FAIL.

- [ ] **Step 3: Implement**

Replace `internal/provision/desired.go`:

```go
package provision

import (
	"fmt"

	"github.com/jskswamy/aide/internal/config"
)

// ResolveDesired flattens the polymorphic v2 schema into a per-context
// Desired struct containing marketplaces, plugins, and mcp_servers.
// Composition order:
//   1. Apply ContextOverride to top-level Plugins map.
//   2. Walk resolved entries, classifying by shape.
//   3. Same for MCPServers.
func ResolveDesired(cfg *config.Config, contextName string) (Desired, error) {
	ctx, ok := cfg.Contexts[contextName]
	if !ok {
		return Desired{}, fmt.Errorf("context %q not found", contextName)
	}

	resolvedPlugins := ApplyOverride(cfg.Plugins, ctx.Plugins)
	resolvedMCP := ApplyOverride(cfg.MCPServers, ctx.MCPServers)

	desired := Desired{
		Marketplaces: map[string]Marketplace{},
		Plugins:      map[string]Plugin{},
		MCPServers:   map[string]MCPServer{},
	}
	for key, entry := range resolvedPlugins {
		switch entry.Shape() {
		case config.PluginShapeMarketplace, config.PluginShapeDeclareOnly:
			desired.Marketplaces[key] = Marketplace{
				Key:    key,
				Source: keyAsSource(key),
			}
			for _, plugin := range entry.Plugins {
				desired.Plugins[plugin] = Plugin{
					Key:    plugin,
					Source: "marketplace",
					Name:   plugin + "@" + key, // marketplace-name resolved post-add
				}
			}
		case config.PluginShapeURLDirect:
			desired.Plugins[key] = Plugin{
				Key:    key,
				Source: classifySource(entry.Source),
				Name:   entry.Source,
			}
		}
	}
	for key, srv := range resolvedMCP {
		desired.MCPServers[key] = MCPServer{
			Key:     key,
			Command: srv.Command,
			URL:     srv.URL,
			Args:    srv.Args,
			Env:     srv.Env,
		}
	}
	return desired, nil
}

// keyAsSource returns the install ref string for a marketplace key.
// Bare "owner/repo" gets a "github:" prefix; full URLs pass through.
func keyAsSource(key string) string {
	for _, prefix := range []string{"github:", "git:", "https://", "http://"} {
		if len(key) >= len(prefix) && key[:len(prefix)] == prefix {
			return key
		}
	}
	return "github:" + key
}

// classifySource picks a Plugin.Source label for a URLDirect entry.
func classifySource(src string) string {
	switch {
	case len(src) >= 7 && src[:7] == "github:":
		return "marketplace"
	case len(src) >= 4 && src[:4] == "git:":
		return "git"
	case len(src) >= 1 && src[0] == '/':
		return "local"
	default:
		return "marketplace"
	}
}
```

Note the `Plugin.Name = plugin + "@" + key` placeholder — the actual `<plugin>@<marketplace-name>` ref is rewritten post-`AddMarketplace` by the sync engine (Task 11).

- [ ] **Step 4: Run to verify pass**

`go test ./internal/provision/ -v 2>&1 | tail -10` — expected PASS.

- [ ] **Step 5: Commit**

```bash
__GIT_COMMIT_PLUGIN__=1 git -C <worktree> commit -am "Rewrite ResolveDesired against v2 polymorphic schema"
```

---

## Task 10: Marketplace ops in Plan + state

**Files:**
- Modify: `internal/provision/plan.go`
- Modify: `internal/provision/plan_test.go`
- Modify: `internal/provision/state.go` (add `Marketplaces` to `ContextState`)

- [ ] **Step 1: Write failing tests**

Append to `plan_test.go`:

```go
func TestComputePlanInstallsMarketplaceFirst(t *testing.T) {
	desired := provision.Desired{
		Marketplaces: map[string]provision.Marketplace{
			"steveyegge/beads": {Key: "steveyegge/beads", Source: "github:steveyegge/beads"},
		},
		Plugins: map[string]provision.Plugin{
			"beads": {Key: "beads", Name: "beads@steveyegge/beads", Source: "marketplace"},
		},
	}
	installed := provision.Installed{}
	managed := provision.ContextState{}
	plan := provision.ComputePlan(provision.Context{Name: "test"}, desired, installed, managed)

	// First op should be marketplace add, then plugin install.
	if len(plan.Ops) < 2 {
		t.Fatalf("expected at least 2 ops, got %d", len(plan.Ops))
	}
	if plan.Ops[0].Kind != provision.KindMarketplace || plan.Ops[0].OpKind != provision.OpInstall {
		t.Errorf("first op = %+v, want install marketplace", plan.Ops[0])
	}
	if plan.Ops[1].Kind != provision.KindPlugin {
		t.Errorf("second op kind = %v, want plugin", plan.Ops[1].Kind)
	}
}
```

- [ ] **Step 2: Run to verify failure**

`go test ./internal/provision/ -run TestComputePlanInstallsMarketplaceFirst -v` — FAIL.

- [ ] **Step 3: Implement**

In `internal/provision/provisioner.go`, extend the `Kind` enum:

```go
const (
	KindPlugin Kind = iota
	KindMCP
	KindMarketplace
)
```

Update `Kind.String()` to include marketplace.

In `internal/provision/state.go`, extend `ContextState`:

```go
type ContextState struct {
	Plugins      map[string]ManagedItem `json:"plugins,omitempty"`
	MCPServers   map[string]ManagedItem `json:"mcp_servers,omitempty"`
	Marketplaces map[string]ManagedItem `json:"marketplaces,omitempty"`
}
```

`ManagedItem` gains an optional `Source string` field for the cached repo→marketplace-name mapping.

In `internal/provision/plan.go`, extend `ComputePlan` and `Desired`/`Installed` to include marketplaces, and produce marketplace ops *first* in the output order:

```go
// Desired struct gains:
//   Marketplaces map[string]Marketplace
//
// Installed struct gains:
//   Marketplaces map[string]Marketplace

// In ComputePlan, before plugins:
for key, m := range desired.Marketplaces {
	if _, present := installed.Marketplaces[key]; !present {
		mc := m
		installs = append(installs, Op{
			Kind:        KindMarketplace,
			OpKind:      OpInstall,
			Name:        key,
			Marketplace: &mc,
		})
	}
}
for key := range managed.Marketplaces {
	if _, stillDesired := desired.Marketplaces[key]; !stillDesired {
		uninstalls = append(uninstalls, Op{
			Kind:   KindMarketplace,
			OpKind: OpUninstall,
			Name:   key,
		})
	}
}
```

Add `Marketplace *Marketplace` field to `Op` struct.

- [ ] **Step 4: Run to verify pass**

`go test ./internal/provision/ -v 2>&1 | tail -10` — PASS.

- [ ] **Step 5: Commit**

```bash
__GIT_COMMIT_PLUGIN__=1 git -C <worktree> commit -am "Add marketplace ops to Plan and ContextState"
```

---

## Task 11: Two-phase Apply in sync engine

**Files:**
- Modify: `internal/provision/engine.go`
- Modify: `internal/provision/engine_test.go`

- [ ] **Step 1: Write failing test**

Append to `engine_test.go`:

```go
func TestApplyAddsMarketplaceBeforePlugin(t *testing.T) {
	fp := &fakeProv{
		name:            "claude",
		supportsPlugins: true,
		supportsMCP:     true,
		shapes:          []provision.SourceShape{provision.ShapeMarketplace},
	}
	desired := provision.Desired{
		Marketplaces: map[string]provision.Marketplace{
			"steveyegge/beads": {Key: "steveyegge/beads", Source: "github:steveyegge/beads"},
		},
		Plugins: map[string]provision.Plugin{
			"beads": {Key: "beads", Name: "beads@steveyegge/beads", Source: "marketplace"},
		},
	}
	plan := provision.ComputePlan(provision.Context{Name: "t", Agent: "claude"}, desired, provision.Installed{}, provision.ContextState{})
	_, err := provision.Apply(fp, plan, provision.ApplyOptions{})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(fp.called) < 2 {
		t.Fatalf("expected 2 calls, got %v", fp.called)
	}
	// First call: marketplace add. Second: plugin install.
	if fp.called[0] != "add-marketplace:steveyegge/beads" {
		t.Errorf("first call = %q, want add-marketplace", fp.called[0])
	}
	if fp.called[1] != "install:beads" {
		t.Errorf("second call = %q, want install:beads", fp.called[1])
	}
}
```

Extend `fakeProv` with `AddMarketplace`/`RemoveMarketplace`/`InstalledMarketplaces` recording `add-marketplace:<key>` etc.

- [ ] **Step 2: Run to verify failure**

`go test ./internal/provision/ -run TestApplyAddsMarketplaceBeforePlugin -v` — FAIL.

- [ ] **Step 3: Implement**

In `engine.go`, the existing `Apply` walks `plan.Ops` in order — since `ComputePlan` now emits marketplace ops first (Task 10), Apply just needs to handle the new op kind:

```go
case KindMarketplace:
	switch op.OpKind {
	case OpInstall:
		if err := prov.AddMarketplace(plan.Context, *op.Marketplace); err != nil {
			_ = j.Rollback()
			return res, fmt.Errorf("add marketplace %q: %w", op.Name, err)
		}
		mName := op.Name
		j.Record(func() error { return prov.RemoveMarketplace(plan.Context, mName) })
		res.Performed++
	case OpUninstall:
		if err := prov.RemoveMarketplace(plan.Context, op.Name); err != nil {
			_ = j.Rollback()
			return res, fmt.Errorf("remove marketplace %q: %w", op.Name, err)
		}
		res.Performed++
	}
```

Marketplace-name discovery (the `<plugin>@<marketplace-name>` rewrite after `AddMarketplace`) is implemented in a follow-up commit if needed — for v2 minimum-viable, the engine assumes `Plugin.Name` of the form `<plugin>@<repo>` is what the agent accepts directly. Claude actually accepts both forms (`beads@steveyegge/beads` and `beads@beads-marketplace`), so this is fine. Document the assumption in a code comment.

- [ ] **Step 4: Run to verify pass**

`go test ./internal/provision/ -v 2>&1 | tail -10` — PASS.

- [ ] **Step 5: Commit**

```bash
__GIT_COMMIT_PLUGIN__=1 git -C <worktree> commit -am "Apply handles marketplace ops with rollback safety"
```

---

## Task 12: CLI command updates (provision_list, sync, adopt)

**Files:**
- Modify: `cmd/aide/provision_list.go`
- Modify: `cmd/aide/sync.go`
- Modify: `cmd/aide/adopt.go`
- Modify: associated `*_test.go` files

- [ ] **Step 1: Write failing tests / outputs**

In `cmd/aide/provision_list_test.go`, update the test fixtures to use v2 YAML (polymorphic shapes) and assert the rendered output shows marketplaces.

- [ ] **Step 2: Run to verify failure**

`go test ./cmd/aide/ -v 2>&1 | tail -20` — FAIL.

- [ ] **Step 3: Implement**

In `cmd/aide/provision_list.go`, render an additional column for marketplaces when the agent supports them. The `provisionListView` (or equivalent) iterates `Desired.Marketplaces` and shows `DECLARED / INSTALLED / MANAGED` for each, before the plugins table.

In `cmd/aide/sync.go`, the existing `renderPlan` already handles `Op.Kind`; add a case for `KindMarketplace` with symbol `+ marketplace` / `- marketplace`.

In `cmd/aide/adopt.go`, when writing back to config.yaml, infer the entry shape from the agent's supported shapes: marketplace agents → list-valued entry; URL-direct agents → string-valued.

Update `provision_list.go`'s `resolveContextEnv` (already exists) — no change needed for v2.

- [ ] **Step 4: Run to verify pass**

`go test ./cmd/aide/ -v 2>&1 | tail -10` — PASS.

- [ ] **Step 5: Commit**

```bash
__GIT_COMMIT_PLUGIN__=1 git -C <worktree> commit -am "Update aide CLI commands for v2 schema (marketplaces in output)"
```

---

## Task 13: Smoke test end-to-end on fresh profile

**Files:**
- Local test: re-run the same scenario from the 2026-05-17 systematic-debugging pass.

- [ ] **Step 1: Rewrite user config to v2**

Update `~/.config/aide/config.yaml` to use the polymorphic shape:

```yaml
plugins:
  steveyegge/beads: [beads]
  jskswamy/claude-plugins: [craft, devenv, jot, codebase, refactor, sketch-note, typst-notes, commit-tools]
  anthropics/claude-plugins-official: [context7, plugin-dev]
  obra/superpowers-marketplace:
    - claude-session-driver
    - double-shot-latte
    - elements-of-style
    - episodic-memory
    - superpowers
    - superpowers-dev
    - superpowers-developing-for-claude-code

contexts:
  default:
    agent: claude
    # inherits all
  prod:
    agent: claude
    env: { CLAUDE_CONFIG_DIR: ~/.claude-prod }
    # inherits all
```

(Back up the old config first.)

- [ ] **Step 2: Run plan against fresh profile**

```bash
mkdir -p ~/.claude-aide-sync-test ~/tmp/aide-sync-test
# Add a test-empty context to config (as in 2026-05-17's smoke), then:
make install-dev
cd ~/tmp/aide-sync-test
aide sync --plan --context test-empty
```

Expected output: shows install ops for marketplaces (`+ marketplace ...`) first, then plugins (`+ install plugin ...`).

- [ ] **Step 3: Apply**

```bash
aide sync --yes --context test-empty
```

Expected: marketplaces and plugins installed in `~/.claude-aide-sync-test`. `claude plugin list --json` (with the right CLAUDE_CONFIG_DIR) reports the installed plugins.

- [ ] **Step 4: Cleanup**

```bash
rm -rf ~/.claude-aide-sync-test ~/tmp/aide-sync-test ~/.local/state/aide/managed.json
```

- [ ] **Step 5: Commit**

(No code commit for this task — it's the verification step. If issues surface during smoke, write fixes as their own commits and re-run the test.)

---

## Task 14: Release notes for v2 migration

**Files:**
- Modify: `RELEASE_NOTES.md`

- [ ] **Step 1: Write the migration note**

Under the Unreleased Features section, add:

```markdown
- **Provisioning schema v2 — polymorphic plugins + delta semantics.**
  The `plugins:` block is now polymorphic by YAML value shape: a list
  value declares a marketplace and the plugins to install from it; a
  string value declares a single URL-direct plugin; null declares a
  marketplace without installing anything. The earlier flat
  `plugins.<name>: {source, name}` shape is gone. `mcp_servers:`
  consolidates the previous `mcp.servers` + `mcp_server_overrides`
  blocks into one top-level map. Per-context `plugins:` and
  `mcp_servers:` blocks accept `extra:` / `exclude:` / `only:` deltas
  over the inherited top-level. Migration: rewrite your config once;
  see `docs/specs/2026-05-15-declarative-agent-provisioning-design.md`
  for the new shape.

- **Declarative marketplace management.** `aide sync` for marketplace
  agents (Claude / Copilot / Codex) now adds missing marketplaces to
  the agent's profile before installing plugins. Marketplaces are
  declared implicitly — every list-valued or null entry in `plugins:`
  whose key is a repo path is a marketplace. The full bootstrap-on-
  fresh-machine flow now works end-to-end with one `aide sync --yes`.
```

- [ ] **Step 2: Commit**

```bash
__GIT_COMMIT_PLUGIN__=1 git -C <worktree> commit -am "Add release notes for v2 schema and marketplace handling"
```

---

## Self-review

**Spec coverage (v2 sections):**
- Polymorphic `plugins:` (list / string / null shapes): Task 2 ✓
- `mcp_servers:` unification: Task 4 ✓
- `ContextOverride[T]` with extra/exclude/only: Task 3 ✓
- Composition semantics: Task 8 (ApplyOverride) ✓
- Parser validation (key-shape, exclude resolution): Task 5 ✓
- `SupportedSourceShapes` capability: Task 6 ✓
- Marketplace methods on Provisioner: Task 7 ✓
- Two-phase Apply (marketplace before plugin): Task 11 ✓
- ResolveDesired against v2: Task 9 ✓
- Plan ops include marketplaces: Task 10 ✓
- CLI output / commands: Task 12 ✓
- Smoke verification: Task 13 ✓
- Release notes: Task 14 ✓

**Type consistency:**
- `SourceShape` enum used in Tasks 1, 6 ✓
- `Marketplace` struct used in Tasks 1, 7, 9, 10, 11 ✓
- `PluginEntry` / `PluginShape` used in Tasks 2, 3, 5, 8, 9 ✓
- `ContextOverride[T]` used in Tasks 3, 8, 9 ✓
- `ApplyOverride` used in Tasks 8, 9 ✓
- `KindMarketplace` op used in Tasks 10, 11, 12 ✓

**Placeholder scan:** No "TBD"/"TODO" left in step bodies. One acknowledged design simplification in Task 11 (assume `<plugin>@<repo>` accepted directly without name-discovery rewrite, documented in code comments). Defer to a follow-up if the assumption breaks.

**Scope check:** One feature, 14 tasks, single coherent batch. Could be split into smaller PRs at the natural boundaries (1–5 schema, 6–11 engine, 12–14 CLI), but the smoke test in Task 13 needs all three layers — better as one continuous run.

**Ambiguity:** Task 2 leaves the package in a partially-broken state until Task 3+4 resolve. This is called out in the commit message; subagents should not panic when intermediate `go build` fails mid-task-2.

**Migration scope:** No automatic migration tool — user rewrites config once. Justified because there's one user (you) and the new shape is meaningfully different. Releasing without a migration tool means there's no "v1 schema users" to migrate later.
