# OpenCode Agent Support Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `provision.Provisioner` driver for OpenCode (`anomalyco/opencode`, opencode.ai) so `aide` can sandbox it, manage its MCP servers, manage its plugins, and install hooks into it.

**Architecture:** A new `internal/provision/agents/opencode` package following the exact shape of the existing `codex`/`gemini` drivers (`DriverBase` embedding, `init()` self-registration). MCP and plugins are both file-edit against `~/.config/opencode/opencode.jsonc` (verified: OpenCode's own CLI can't scriptably manage either — see spec `docs/superpowers/specs/2026-08-31-opencode-pi-agent-support-design.md`). Hooks are generated JS plugin files dropped in `.opencode/plugin/` (confirmed to auto-load), using the codebase's existing generic `HookCodec`/`ReadHooks`/`WriteHooks` machinery. A new seatbelt module handles sandboxing.

**Tech Stack:** Go, `encoding/json`, `github.com/tailscale/hujson` (new dependency, for JSONC parsing), the existing `internal/provision` and `pkg/seatbelt` packages.

## Global Constraints

- Follow the existing `provision.Provisioner` driver pattern exactly: embed `provision.DriverBase`, construct `Capabilities` in `New()`, self-register via `init()` calling `provision.RegisterProvisioner(New(provision.ExecRunner{}))`.
- Hooks are global-scope only (`~/.config/opencode/...`), matching every existing hook-capable driver (`claude`, `gemini`, `hermes`) — none of them write project-scope hooks today, so this driver doesn't introduce that either.
- New dependency: `github.com/tailscale/hujson`, added via `go get` — no other new dependencies.
- All file writes use `fsutil.AtomicWrite` (never `os.WriteFile` directly) for crash safety, matching every existing driver.
- Every new Go file needs a package comment or doc comment matching the density of comments in the existing `gemini`/`codex` driver files (explain *why*, not *what*).

---

### Task 1: OpenCode seatbelt sandbox module

**Files:**
- Create: `pkg/seatbelt/modules/opencode.go`
- Modify: `pkg/seatbelt/modules/agents_test.go` (append test cases to the existing `TestAgentModules` table)
- Modify: `internal/launcher/agentcfg.go`

**Interfaces:**
- Produces: `modules.OpenCodeAgent() seatbelt.Module`

- [ ] **Step 1: Write the failing test**

Open `pkg/seatbelt/modules/agents_test.go` and add these two entries to the `tests` slice (insert after the `"Gemini env override"` case, before `"Copilot defaults"`, to keep agents grouped alphabetically-ish like the existing entries):

```go
		{
			name:     "OpenCode defaults",
			module:   OpenCodeAgent(),
			wantName: "OpenCode Agent",
			wantContain: []string{
				"/home/user/.config/opencode",
				"/home/user/.local/share/opencode",
				"/home/user/.local/state/opencode",
				"/home/user/.cache/opencode",
			},
		},
```

(OpenCode has no confirmed home-redirect env var — see spec — so there's no "env override" case, unlike Gemini/Codex/Copilot.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/seatbelt/modules/... -run TestAgentModules -v`
Expected: FAIL with `undefined: OpenCodeAgent`

- [ ] **Step 3: Write minimal implementation**

Create `pkg/seatbelt/modules/opencode.go`:

```go
package modules

import "github.com/jskswamy/aide/pkg/seatbelt"

// OpenCodeAgent returns a module with OpenCode CLI agent sandbox
// rules. OpenCode has no documented env var that redirects its whole
// config/data/cache home the way CLAUDE_CONFIG_DIR or GEMINI_HOME do
// (only OPENCODE_CONFIG, which points at a single file, not a
// directory) — EnvKey is left empty, matching how AgentSpec treats an
// empty EnvKey as "no override mechanism".
//
// Home-relative defaults verified directly (2026-08-31, opencode
// 1.18.18 via nix-shell): a single `opencode mcp add` invocation
// against a fresh $HOME populated all four directories below.
func OpenCodeAgent() seatbelt.Module {
	return NewSimpleAgent(AgentSpec{
		DisplayName: "OpenCode Agent",
		SectionName: "OpenCode",
		HomeRelDefaults: []string{
			".config/opencode",
			".local/share/opencode",
			".local/state/opencode",
			".cache/opencode",
		},
	})
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/seatbelt/modules/... -run TestAgentModules -v`
Expected: PASS

- [ ] **Step 5: Wire into the agent module resolver**

In `internal/launcher/agentcfg.go`, add `"opencode": modules.OpenCodeAgent,` to the `agentModuleResolvers` map (keep alphabetical order — it goes after `"gemini"` and before `"goose"`):

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
	"opencode":     modules.OpenCodeAgent,
}
```

- [ ] **Step 6: Run the full package tests**

Run: `go test ./pkg/seatbelt/... ./internal/launcher/... -v`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add pkg/seatbelt/modules/opencode.go pkg/seatbelt/modules/agents_test.go internal/launcher/agentcfg.go
git commit -m "Add OpenCode seatbelt sandbox module"
```

---

### Task 2: OpenCode MCP JSON handler

**Files:**
- Create: `internal/provision/mcp/opencodejson.go`
- Create: `internal/provision/mcp/opencodejson_test.go`
- Modify: `go.mod`, `go.sum` (new dependency)

**Interfaces:**
- Consumes: `provision.MCPServer{Key, Command, URL, Args, Env}`, `provision.MCPHandler` interface (`Read(path string) (map[string]provision.MCPServer, map[string]bool, error)`, `Write(path string, desired map[string]provision.MCPServer) error`)
- Produces: `mcp.NewOpenCodeJSON() provision.MCPHandler`

- [ ] **Step 1: Add the hujson dependency**

Run: `go get github.com/tailscale/hujson` (from the repo root)
Expected: `go.mod`/`go.sum` updated with the new dependency; `go build ./...` still succeeds.

- [ ] **Step 2: Write the failing tests**

Create `internal/provision/mcp/opencodejson_test.go`:

```go
package mcp_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/jskswamy/aide/internal/provision"
	"github.com/jskswamy/aide/internal/provision/mcp"
)

func TestOpenCodeJSONReadMissingReturnsEmpty(t *testing.T) {
	h := mcp.NewOpenCodeJSON()
	got, mgd, err := h.Read(filepath.Join(t.TempDir(), "absent.jsonc"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 || len(mgd) != 0 {
		t.Errorf("expected empty, got %+v %+v", got, mgd)
	}
}

// TestOpenCodeJSONReadHandlesComments pins the JSONC requirement: a
// plain encoding/json.Unmarshal errors on the comment below, so this
// test fails without hujson.Standardize in Read.
func TestOpenCodeJSONReadHandlesComments(t *testing.T) {
	path := filepath.Join(t.TempDir(), "opencode.jsonc")
	body := `{
  // a user comment that plain encoding/json would choke on
  "_aide_managed": ["postgres"],
  "mcp": {
    "postgres": {"type": "local", "command": ["postgres-mcp", "--port", "5432"], "environment": {"PGUSER": "aide"}},
    "remote-one": {"type": "remote", "url": "https://example.com/mcp"}
  }
}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	h := mcp.NewOpenCodeJSON()
	got, mgd, err := h.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	pg := got["postgres"]
	if pg.Command != "postgres-mcp" || len(pg.Args) != 2 || pg.Args[0] != "--port" || pg.Args[1] != "5432" {
		t.Errorf("postgres = %+v", pg)
	}
	if pg.Env["PGUSER"] != "aide" {
		t.Errorf("postgres env = %+v", pg.Env)
	}
	remote := got["remote-one"]
	if remote.URL != "https://example.com/mcp" {
		t.Errorf("remote-one = %+v", remote)
	}
	if !mgd["postgres"] || mgd["remote-one"] {
		t.Errorf("managed = %+v", mgd)
	}
}

func TestOpenCodeJSONWritePreservesUnmanagedAndOtherKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "opencode.jsonc")
	body := `{"$schema": "https://opencode.ai/config.json", "mcp": {"user-added": {"type": "local", "command": ["manual"]}}}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	desired := map[string]provision.MCPServer{
		"postgres": {Key: "postgres", Command: "postgres-mcp", Args: []string{"--port", "9090"}},
	}
	if err := mcp.NewOpenCodeJSON().Write(path, desired); err != nil {
		t.Fatal(err)
	}

	raw, _ := os.ReadFile(path)
	var doc struct {
		Schema      string                    `json:"$schema"`
		AideManaged []string                  `json:"_aide_managed"`
		MCP         map[string]map[string]any `json:"mcp"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Schema != "https://opencode.ai/config.json" {
		t.Errorf("$schema not preserved: %q", doc.Schema)
	}
	if _, ok := doc.MCP["user-added"]; !ok {
		t.Error("user-added must survive")
	}
	pg, ok := doc.MCP["postgres"]
	if !ok {
		t.Fatal("postgres not written")
	}
	cmd, _ := pg["command"].([]any)
	if len(cmd) != 3 || cmd[0] != "postgres-mcp" || cmd[1] != "--port" || cmd[2] != "9090" {
		t.Errorf("postgres command = %v", cmd)
	}
	if len(doc.AideManaged) != 1 || doc.AideManaged[0] != "postgres" {
		t.Errorf("_aide_managed = %v", doc.AideManaged)
	}
}

func TestOpenCodeJSONWriteRemovesPreviouslyManaged(t *testing.T) {
	path := filepath.Join(t.TempDir(), "opencode.jsonc")
	body := `{
  "_aide_managed": ["old", "stay"],
  "mcp": {"old": {"type": "local", "command": ["x"]}, "stay": {"type": "local", "command": ["y"]}}
}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	desired := map[string]provision.MCPServer{
		"stay": {Key: "stay", Command: "y"},
	}
	if err := mcp.NewOpenCodeJSON().Write(path, desired); err != nil {
		t.Fatal(err)
	}

	raw, _ := os.ReadFile(path)
	var doc struct {
		AideManaged []string                  `json:"_aide_managed"`
		MCP         map[string]map[string]any `json:"mcp"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if _, gone := doc.MCP["old"]; gone {
		t.Error("old should have been removed")
	}
	if _, kept := doc.MCP["stay"]; !kept {
		t.Error("stay should be preserved")
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/provision/mcp/... -run TestOpenCodeJSON -v`
Expected: FAIL with `undefined: mcp.NewOpenCodeJSON`

- [ ] **Step 4: Write the implementation**

Create `internal/provision/mcp/opencodejson.go`:

```go
package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"

	"github.com/tailscale/hujson"

	"github.com/jskswamy/aide/internal/fsutil"
	"github.com/jskswamy/aide/internal/provision"
)

// NewOpenCodeJSON returns the handler for OpenCode's config file
// (`~/.config/opencode/opencode.jsonc`, or the project-root
// equivalent). MCP servers live under the top-level `"mcp"` key.
//
// OpenCode's own CLI is not usable here: `opencode mcp add` has no
// flag for a local/stdio server's command (only --url/--env/--header,
// confirmed via `opencode mcp add --help`), and `opencode mcp list`
// live-connects to every configured server printing ANSI health-check
// output with no --json flag (confirmed by observing it attempt a
// real connection to a dummy URL). File-edit is the only reliable,
// non-interactive path — see
// docs/superpowers/specs/2026-08-31-opencode-pi-agent-support-design.md.
//
// The config file is JSONC (comments allowed) — confirmed OpenCode
// itself writes it as `opencode.jsonc`. Read runs hujson.Standardize
// before unmarshaling so a user's hand-added comments don't break
// parsing; Write always emits plain JSON via json.MarshalIndent
// (comments are not preserved across a write, same as every other
// file-edit handler in this package — none of them preserve original
// formatting either).
func NewOpenCodeJSON() provision.MCPHandler { return openCodeJSON{} }

type openCodeJSON struct{}

// openCodeServerBody is the on-disk shape for one MCP server.
// Confirmed directly (2026-08-31): hand-writing a "type":"local" entry
// with a "command" array and "environment" map and re-reading it via
// `opencode debug config` round-tripped byte-for-byte with no errors.
type openCodeServerBody struct {
	Type        string            `json:"type,omitempty"`
	Command     []string          `json:"command,omitempty"`
	URL         string            `json:"url,omitempty"`
	Environment map[string]string `json:"environment,omitempty"`
}

// Read implements provision.MCPHandler.
func (openCodeJSON) Read(path string) (map[string]provision.MCPServer, map[string]bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]provision.MCPServer{}, map[string]bool{}, nil
		}
		return nil, nil, fmt.Errorf("provision/mcp: reading %s: %w", path, err)
	}
	standardized, err := hujson.Standardize(data)
	if err != nil {
		return nil, nil, fmt.Errorf("provision/mcp: parsing %s: %w", path, err)
	}
	var doc struct {
		AideManaged []string                   `json:"_aide_managed,omitempty"`
		MCP         map[string]json.RawMessage `json:"mcp,omitempty"`
	}
	if err := json.Unmarshal(standardized, &doc); err != nil {
		return nil, nil, fmt.Errorf("provision/mcp: parsing %s: %w", path, err)
	}
	servers := map[string]provision.MCPServer{}
	for key, raw := range doc.MCP {
		var body openCodeServerBody
		if err := json.Unmarshal(raw, &body); err != nil {
			return nil, nil, fmt.Errorf("provision/mcp: parsing server %q: %w", key, err)
		}
		s := provision.MCPServer{Key: key, URL: body.URL, Env: body.Environment}
		if len(body.Command) > 0 {
			s.Command = body.Command[0]
			if len(body.Command) > 1 {
				s.Args = body.Command[1:]
			}
		}
		servers[key] = s
	}
	managed := map[string]bool{}
	for _, k := range doc.AideManaged {
		managed[k] = true
	}
	return servers, managed, nil
}

// Write implements provision.MCPHandler.
func (openCodeJSON) Write(path string, desired map[string]provision.MCPServer) error {
	existing := map[string]json.RawMessage{}
	if data, err := os.ReadFile(path); err == nil {
		standardized, serr := hujson.Standardize(data)
		if serr != nil {
			return fmt.Errorf("provision/mcp: parsing existing %s: %w", path, serr)
		}
		if err := json.Unmarshal(standardized, &existing); err != nil {
			return fmt.Errorf("provision/mcp: parsing existing %s: %w", path, err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("provision/mcp: reading %s: %w", path, err)
	}

	prevServers := map[string]json.RawMessage{}
	if raw, ok := existing["mcp"]; ok {
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
	newServers := map[string]json.RawMessage{}
	for key, raw := range prevServers {
		if wasManaged[key] {
			continue
		}
		newServers[key] = raw
	}
	newManaged := make([]string, 0, len(desired))
	for key, s := range desired {
		body := openCodeServerBody{Environment: s.Env}
		if s.URL != "" {
			body.Type = "remote"
			body.URL = s.URL
		} else {
			body.Type = "local"
			body.Command = append([]string{s.Command}, s.Args...)
		}
		raw, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("provision/mcp: marshalling server %q: %w", key, err)
		}
		newServers[key] = raw
		newManaged = append(newManaged, key)
	}
	sort.Strings(newManaged)

	managedRaw, _ := json.Marshal(newManaged)
	serversRaw, _ := json.Marshal(newServers)
	existing["_aide_managed"] = managedRaw
	existing["mcp"] = serversRaw

	out, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return fmt.Errorf("provision/mcp: marshalling %s: %w", path, err)
	}
	return fsutil.AtomicWrite(path, out)
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/provision/mcp/... -run TestOpenCodeJSON -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/provision/mcp/opencodejson.go internal/provision/mcp/opencodejson_test.go go.mod go.sum
git commit -m "Add OpenCode MCP JSON handler"
```

---

### Task 3: OpenCode driver — capabilities, MCP wiring, plugins

**Files:**
- Create: `internal/provision/agents/opencode/opencode.go`
- Create: `internal/provision/agents/opencode/opencode_test.go`

**Interfaces:**
- Consumes: `mcp.NewOpenCodeJSON() provision.MCPHandler` (Task 2), `provision.Plugin{Key, Source, Name}`, `provision.DriverBase`, `provision.Capabilities`, `fsutil.AtomicWrite`
- Produces: `opencode.New(r provision.Runner) *opencode.Driver`; `(*Driver).InstalledPlugins/InstallPlugin/UninstallPlugin` for Task 4 to leave untouched; a package-level `configPath(ctx provision.Context) string` and `readConfig`/`writeConfig` helpers for Task 4's hooks file to reuse if needed (it won't — hooks use a separate directory, not this config file).

- [ ] **Step 1: Write the failing tests**

Create `internal/provision/agents/opencode/opencode_test.go`:

```go
package opencode_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/jskswamy/aide/internal/provision"
	"github.com/jskswamy/aide/internal/provision/agents/opencode"
)

type fakeRunner struct{}

func (fakeRunner) Run(_ context.Context, _ map[string]string, name string, args ...string) (string, string, int, error) {
	return "", "", 0, nil
}

func TestOpenCodeCapabilities(t *testing.T) {
	d := opencode.New(fakeRunner{})
	if d.Name() != "opencode" {
		t.Errorf("Name = %q", d.Name())
	}
	if !d.SupportsPlugins() || !d.SupportsMCP() || !d.SupportsHooks() {
		t.Error("OpenCode should support plugins, MCP, and hooks")
	}
	if d.RequiresTTY() {
		t.Error("OpenCode should not require TTY")
	}
	shapes := d.SupportedSourceShapes()
	if len(shapes) != 1 || shapes[0] != provision.ShapeURLDirect {
		t.Errorf("OpenCode shapes = %v, want [url-direct]", shapes)
	}
}

func TestOpenCodeMCPConfigPath(t *testing.T) {
	d := opencode.New(fakeRunner{})
	ctx := provision.Context{HomeDir: "/home/u"}
	want := filepath.Join("/home/u", ".config", "opencode", "opencode.jsonc")
	if got := d.MCPConfigPath(ctx); got != want {
		t.Errorf("MCPConfigPath = %q, want %q", got, want)
	}
	if d.MCPHandler(ctx) == nil {
		t.Error("MCPHandler should not be nil — OpenCode is file-edit, not CLI-driven")
	}
}

func TestOpenCodeInstallPluginWritesArrayAndDedups(t *testing.T) {
	home := t.TempDir()
	ctx := provision.Context{HomeDir: home}
	d := opencode.New(fakeRunner{})

	if err := d.InstallPlugin(ctx, provision.Plugin{Key: "foo", Name: "my-npm-plugin"}); err != nil {
		t.Fatal(err)
	}
	// Installing the same ref again must not duplicate it.
	if err := d.InstallPlugin(ctx, provision.Plugin{Key: "foo", Name: "my-npm-plugin"}); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(filepath.Join(home, ".config", "opencode", "opencode.jsonc"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Plugin []string `json:"plugin"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(doc.Plugin, []string{"my-npm-plugin"}) {
		t.Errorf("plugin array = %v, want [my-npm-plugin]", doc.Plugin)
	}
}

func TestOpenCodeInstalledPluginsParsesArray(t *testing.T) {
	home := t.TempDir()
	ctx := provision.Context{HomeDir: home}
	d := opencode.New(fakeRunner{})

	if err := d.InstallPlugin(ctx, provision.Plugin{Key: "a", Name: "plugin-a"}); err != nil {
		t.Fatal(err)
	}
	if err := d.InstallPlugin(ctx, provision.Plugin{Key: "b", Name: "plugin-b"}); err != nil {
		t.Fatal(err)
	}

	got, err := d.InstalledPlugins(ctx)
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, p := range got {
		names = append(names, p.Name)
	}
	if len(names) != 2 {
		t.Fatalf("names = %v, want 2 entries", names)
	}
}

func TestOpenCodeUninstallPluginRemoves(t *testing.T) {
	home := t.TempDir()
	ctx := provision.Context{HomeDir: home}
	d := opencode.New(fakeRunner{})

	_ = d.InstallPlugin(ctx, provision.Plugin{Key: "a", Name: "plugin-a"})
	_ = d.InstallPlugin(ctx, provision.Plugin{Key: "b", Name: "plugin-b"})
	if err := d.UninstallPlugin(ctx, "plugin-a"); err != nil {
		t.Fatal(err)
	}

	got, err := d.InstalledPlugins(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "plugin-b" {
		t.Errorf("InstalledPlugins after uninstall = %+v", got)
	}
}

func TestOpenCodeUninstallMissingIsNoOp(t *testing.T) {
	home := t.TempDir()
	ctx := provision.Context{HomeDir: home}
	d := opencode.New(fakeRunner{})
	if err := d.UninstallPlugin(ctx, "never-installed"); err != nil {
		t.Errorf("uninstalling an absent plugin should be a no-op, got %v", err)
	}
}

func TestOpenCodeInstalledPluginsEmptyWhenConfigMissing(t *testing.T) {
	home := t.TempDir()
	ctx := provision.Context{HomeDir: home}
	d := opencode.New(fakeRunner{})
	got, err := d.InstalledPlugins(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty, got %+v", got)
	}
}

func TestOpenCodeMarketplaceMethodsNoOp(t *testing.T) {
	d := opencode.New(fakeRunner{})
	got, err := d.InstalledMarketplaces(provision.Context{})
	if err != nil || len(got) != 0 {
		t.Errorf("InstalledMarketplaces should be no-op, got %v, %v", got, err)
	}
	if err := d.RemoveMarketplace(provision.Context{}, "anything"); err != nil {
		t.Errorf("RemoveMarketplace should be no-op, got %v", err)
	}
	if err := d.AddMarketplace(provision.Context{}, provision.Marketplace{}); err == nil {
		t.Error("AddMarketplace should error — OpenCode has no marketplace concept")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/provision/agents/opencode/... -v`
Expected: FAIL with `no such package` / `undefined: opencode.New`

- [ ] **Step 3: Write the implementation**

Create `internal/provision/agents/opencode/opencode.go`:

```go
// Package opencode provides the provision.Provisioner driver for
// OpenCode (`anomalyco/opencode`, opencode.ai). See
// docs/superpowers/specs/2026-08-31-opencode-pi-agent-support-design.md
// for the capabilities verified directly against the binary.
package opencode

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/tailscale/hujson"

	"github.com/jskswamy/aide/internal/fsutil"
	"github.com/jskswamy/aide/internal/provision"
	"github.com/jskswamy/aide/internal/provision/mcp"
)

const agentName = "opencode"

// Driver implements provision.Provisioner for OpenCode. Capability
// stub methods are promoted from DriverBase. The Runner field is
// unused today (every operation is a file edit, not a shell-out) but
// kept for symmetry with other drivers and future use — the same
// choice the codex driver makes for the same reason.
type Driver struct {
	provision.DriverBase
	runner provision.Runner
}

// New returns a Driver using the supplied Runner.
func New(r provision.Runner) *Driver {
	return &Driver{
		DriverBase: provision.DriverBase{Caps: provision.Capabilities{
			AgentName:       agentName,
			SupportsPlugins: true,
			SupportsMCP:     true,
			RequiresTTY:     false,
			SourceShapes:    []provision.SourceShape{provision.ShapeURLDirect},
			SupportsHooks:   true,
		}},
		runner: r,
	}
}

func init() {
	provision.RegisterProvisioner(New(provision.ExecRunner{}))
}

// configPath returns ~/.config/opencode/opencode.jsonc. OpenCode has
// no confirmed env var for redirecting this path (only OPENCODE_CONFIG,
// which points at a single file, not a directory), so unlike claude's
// AgentDir this is not profile-aware.
func configPath(ctx provision.Context) string {
	return filepath.Join(ctx.HomeDir, ".config", "opencode", "opencode.jsonc")
}

// MCPConfigPath returns the same file plugins live in — OpenCode
// keeps both under one config file.
func (*Driver) MCPConfigPath(ctx provision.Context) string { return configPath(ctx) }

// MCPHandler returns the file-edit handler — see
// internal/provision/mcp/opencodejson.go for why OpenCode's own CLI
// isn't usable here.
func (*Driver) MCPHandler(_ provision.Context) provision.MCPHandler { return mcp.NewOpenCodeJSON() }

// readConfig reads and JSONC-normalizes opencode.jsonc into a generic
// map, the same read-whole-doc-mutate-one-key-write-whole-doc pattern
// codex's config.toml handling uses, so plugins and MCP (Task 2, a
// different key in the same file) never clobber each other's keys.
func readConfig(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]any{}, nil
		}
		return nil, fmt.Errorf("opencode: reading config %s: %w", path, err)
	}
	standardized, err := hujson.Standardize(data)
	if err != nil {
		return nil, fmt.Errorf("opencode: parsing config %s: %w", path, err)
	}
	doc := map[string]any{}
	if err := json.Unmarshal(standardized, &doc); err != nil {
		return nil, fmt.Errorf("opencode: parsing config %s: %w", path, err)
	}
	return doc, nil
}

func writeConfig(path string, doc map[string]any) error {
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("opencode: marshalling config %s: %w", path, err)
	}
	return fsutil.AtomicWrite(path, out)
}

// pluginRefs reads the "plugin" array (bare npm/git/local-path
// strings — confirmed via `opencode debug config`: locally-dropped
// hook plugins from .opencode/plugin/ show up here too, as file://
// URIs, alongside any explicitly-declared refs; they coexist without
// collision since aide only ever adds/removes plain string refs it
// itself declared).
func pluginRefs(doc map[string]any) []string {
	raw, ok := doc["plugin"].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func setPluginRefs(doc map[string]any, refs []string) {
	list := make([]any, len(refs))
	for i, r := range refs {
		list[i] = r
	}
	doc["plugin"] = list
}

// InstalledPlugins reads the "plugin" array directly. Binary-missing
// isn't a concept here (no shell-out), so a missing config file is
// simply "nothing installed", matching the InstalledPlugins
// convention of every other driver.
func (d *Driver) InstalledPlugins(ctx provision.Context) ([]provision.Plugin, error) {
	doc, err := readConfig(configPath(ctx))
	if err != nil {
		return nil, err
	}
	refs := pluginRefs(doc)
	out := make([]provision.Plugin, 0, len(refs))
	for _, ref := range refs {
		out = append(out, provision.Plugin{Key: ref, Name: ref})
	}
	return out, nil
}

// InstallPlugin appends p.Name to the "plugin" array if not already
// present. No CLI is shelled out to — see the package doc comment on
// opencodejson.go for why: `opencode plugin <module>` only accepts
// npm module names (confirmed via --help), can't express git/local
// refs, and has no list/remove counterpart at all.
func (d *Driver) InstallPlugin(ctx provision.Context, p provision.Plugin) error {
	path := configPath(ctx)
	doc, err := readConfig(path)
	if err != nil {
		return err
	}
	refs := pluginRefs(doc)
	for _, existing := range refs {
		if existing == p.Name {
			return nil
		}
	}
	setPluginRefs(doc, append(refs, p.Name))
	return writeConfig(path, doc)
}

// UninstallPlugin removes name from the "plugin" array. No-op (nil
// error) if not present, for rollback safety.
func (d *Driver) UninstallPlugin(ctx provision.Context, name string) error {
	path := configPath(ctx)
	doc, err := readConfig(path)
	if err != nil {
		return err
	}
	refs := pluginRefs(doc)
	kept := make([]string, 0, len(refs))
	for _, existing := range refs {
		if existing != name {
			kept = append(kept, existing)
		}
	}
	if len(kept) == len(refs) {
		return nil
	}
	setPluginRefs(doc, kept)
	return writeConfig(path, doc)
}

// InstalledMarketplaces is a no-op: OpenCode has no marketplace
// concept, only bare npm/git/local-path refs.
func (*Driver) InstalledMarketplaces(_ provision.Context) ([]provision.Marketplace, error) {
	return nil, nil
}

// AddMarketplace returns an error: OpenCode plugins are declared as
// URL-direct string entries, not via marketplaces.
func (*Driver) AddMarketplace(_ provision.Context, _ provision.Marketplace) error {
	return fmt.Errorf("opencode does not have marketplaces; declare plugins inline with string values")
}

// RemoveMarketplace is a no-op for rollback safety.
func (*Driver) RemoveMarketplace(_ provision.Context, _ string) error {
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/provision/agents/opencode/... -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/provision/agents/opencode/opencode.go internal/provision/agents/opencode/opencode_test.go
git commit -m "Add OpenCode driver core: capabilities, MCP, plugins"
```

---

### Task 4: OpenCode hooks

**Files:**
- Create: `internal/provision/agents/opencode/hooks.go`
- Create: `internal/provision/agents/opencode/hooks_test.go`
- Modify: `internal/provision/hook_artifact.go` (add `OpenCodeHookArtifact`)

**Interfaces:**
- Consumes: `provision.HookCodec` interface (`Match(name string) bool`, `Decode(path string) (provision.Hook, error)`, `Encode(dir string, h provision.Hook) error`, `Remove(path string) error`), `provision.ReadHooks(dir string, codec HookCodec) ([]Hook, error)`, `provision.WriteHooks(dir string, desired []Hook, codec HookCodec) error`, `provision.HookArtifact{Prefix, Ext}`, `provision.ValidateHookCommand`, `provision.ReverseLookup`
- Produces: `(*Driver).ReadHooks(ctx) ([]provision.Hook, error)`, `(*Driver).WriteHooks(ctx, prevManaged, desired) error` — completes the `provision.HookInstaller` interface for `opencode.Driver`

- [ ] **Step 1: Add the OpenCodeHookArtifact constant**

In `internal/provision/hook_artifact.go`, add to the `var` block after `HermesHookArtifact`:

```go
	// OpenCodeHookArtifact defines OpenCode's hook artifact shape:
	// aide-<hash>.js files, dropped in .opencode/plugin/ where OpenCode
	// auto-loads them (confirmed 2026-08-31: a probe plugin dropped
	// there printed to stderr and appeared in `opencode debug config`'s
	// resolved "plugin" array with plugin_origins[].scope: "local").
	OpenCodeHookArtifact = HookArtifact{Prefix: "aide-", Ext: ".js"}
```

- [ ] **Step 2: Write the failing tests**

Create `internal/provision/agents/opencode/hooks_test.go`:

```go
package opencode_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/provision"
	"github.com/jskswamy/aide/internal/provision/agents/opencode"
)

func TestOpenCodeWriteHooksThenRead(t *testing.T) {
	home := t.TempDir()
	ctx := provision.Context{HomeDir: home}
	d := opencode.New(fakeRunner{})

	hooks := []provision.Hook{
		{Event: "pre_tool", Command: "rtk hook opencode"},
	}
	if err := d.WriteHooks(ctx, nil, hooks); err != nil {
		t.Fatal(err)
	}
	got, err := d.ReadHooks(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Command != "rtk hook opencode" || got[0].Event != "pre_tool" {
		t.Errorf("ReadHooks = %+v", got)
	}

	entries, _ := os.ReadDir(filepath.Join(home, ".config", "opencode", "plugin"))
	hasPlugin := false
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "aide-") && strings.HasSuffix(e.Name(), ".js") {
			hasPlugin = true
		}
	}
	if !hasPlugin {
		t.Error("expected aide-*.js plugin file in ~/.config/opencode/plugin/")
	}
}

func TestOpenCodeWriteHooksRejectsMetacharacters(t *testing.T) {
	home := t.TempDir()
	ctx := provision.Context{HomeDir: home}
	d := opencode.New(fakeRunner{})

	err := d.WriteHooks(ctx, nil, []provision.Hook{{Event: "pre_tool", Command: "rtk hook; rm -rf ~"}})
	if err == nil {
		t.Error("expected error for command containing shell metacharacters")
	}
}

func TestOpenCodeWriteHooksClearsPrevious(t *testing.T) {
	home := t.TempDir()
	ctx := provision.Context{HomeDir: home}
	d := opencode.New(fakeRunner{})

	_ = d.WriteHooks(ctx, nil, []provision.Hook{{Event: "pre_tool", Command: "old-hook"}})
	_ = d.WriteHooks(ctx, nil, []provision.Hook{{Event: "pre_tool", Command: "new-hook"}})

	got, _ := d.ReadHooks(ctx)
	if len(got) != 1 || got[0].Command != "new-hook" {
		t.Errorf("ReadHooks = %+v, want [new-hook]", got)
	}
	entries, _ := os.ReadDir(filepath.Join(home, ".config", "opencode", "plugin"))
	count := 0
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "aide-") {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected 1 aide- plugin file, found %d", count)
	}
}

func TestOpenCodeWriteHooksSkipsUnsupportedEventSilently(t *testing.T) {
	home := t.TempDir()
	ctx := provision.Context{HomeDir: home}
	d := opencode.New(fakeRunner{})

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

func TestOpenCodePostToolEventMapsCorrectly(t *testing.T) {
	home := t.TempDir()
	ctx := provision.Context{HomeDir: home}
	d := opencode.New(fakeRunner{})

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
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/provision/agents/opencode/... -run TestOpenCodeWriteHooks -v`
Expected: FAIL — `d.WriteHooks`/`d.ReadHooks` undefined on `*opencode.Driver`

- [ ] **Step 4: Write the implementation**

Create `internal/provision/agents/opencode/hooks.go`:

```go
package opencode

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jskswamy/aide/internal/fsutil"
	"github.com/jskswamy/aide/internal/provision"
)

// openCodeEventMap maps aide's normalized hook events to OpenCode's
// native plugin hook names. Confirmed via OpenCode's plugin API docs:
// tool.execute.before/after are the direct PreToolUse/PostToolUse
// equivalents. session_start/session_end have no confirmed
// one-to-one native event (OpenCode's "event" hook fires on a wider
// range of payloads) — they're intentionally left unmapped for now;
// WriteHooks skips unmapped events silently rather than guessing at a
// filter condition that hasn't been verified.
var openCodeEventMap = map[string]string{
	"pre_tool":  "tool.execute.before",
	"post_tool": "tool.execute.after",
}

func openCodeHooksDir(ctx provision.Context) string {
	return filepath.Join(ctx.HomeDir, ".config", "opencode", "plugin")
}

// openCodeHookCodec implements provision.HookCodec for OpenCode's
// aide-*.js plugin files. The generated file's real behavior lives in
// a JS callback, which is impractical to parse back out — Decode
// instead reads two `// aide-*:` comment lines that carry the
// canonical (event, command) pair, the same "metadata survives the
// round-trip even though the executable body doesn't need to be
// re-parsed" approach gemini's hook codec takes with its `exec `
// line, just via comments instead of a directly-executable line.
type openCodeHookCodec struct{}

func (c *openCodeHookCodec) Match(name string) bool {
	return provision.OpenCodeHookArtifact.Owns(name)
}

func (c *openCodeHookCodec) Decode(path string) (provision.Hook, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return provision.Hook{}, err
	}
	var command, event string
	for _, line := range strings.Split(string(data), "\n") {
		switch {
		case strings.HasPrefix(line, "// aide-event: "):
			native := strings.TrimPrefix(line, "// aide-event: ")
			event = provision.ReverseLookup(openCodeEventMap, native, native)
		case strings.HasPrefix(line, "// aide-command: "):
			command = strings.TrimPrefix(line, "// aide-command: ")
		}
	}
	return provision.Hook{Event: event, Command: command}, nil
}

func (c *openCodeHookCodec) Encode(dir string, h provision.Hook) error {
	nativeEvent, ok := openCodeEventMap[h.Event]
	if !ok {
		return nil // unsupported event — skip silently
	}
	if err := provision.ValidateHookCommand(h.Command); err != nil {
		return fmt.Errorf("opencode hooks: %w", err)
	}
	name := provision.OpenCodeHookArtifact.Name(h.Command)
	script := "// Managed by aide. Do not edit manually.\n" +
		"// aide-event: " + nativeEvent + "\n" +
		"// aide-command: " + h.Command + "\n" +
		"export const AideHook = async () => ({\n" +
		"  \"" + nativeEvent + "\": async () => {\n" +
		"    await Bun.$`" + h.Command + "`\n" +
		"  },\n" +
		"})\n"
	if err := fsutil.AtomicWrite(filepath.Join(dir, name), []byte(script)); err != nil {
		return fmt.Errorf("opencode hooks: write plugin: %w", err)
	}
	return nil
}

func (c *openCodeHookCodec) Remove(path string) error {
	return os.Remove(path)
}

// ReadHooks returns aide-managed hooks by listing aide-*.js plugins.
func (d *Driver) ReadHooks(ctx provision.Context) ([]provision.Hook, error) {
	return provision.ReadHooks(openCodeHooksDir(ctx), &openCodeHookCodec{})
}

// WriteHooks removes all aide-*.js plugins and writes new ones for
// desired. prevManaged is unused for file-based formats; the aide-
// naming prefix is the ownership signal, same as gemini/hermes.
func (d *Driver) WriteHooks(ctx provision.Context, _ []provision.Hook, desired []provision.Hook) error {
	if err := os.MkdirAll(openCodeHooksDir(ctx), 0o750); err == nil {
		// MkdirAll here is redundant with provision.WriteHooks (which
		// also creates dir) but cheap and harmless; kept out only if it
		// causes lint noise. (No-op comment for the implementer: this
		// line is unnecessary — provision.WriteHooks already calls
		// os.MkdirAll(dir, 0o750) internally. Do not add it; call
		// provision.WriteHooks directly, as below.)
	}
	return provision.WriteHooks(openCodeHooksDir(ctx), desired, &openCodeHookCodec{})
}
```

**Note for the implementer:** the `os.MkdirAll` block above is a mistake to catch during review, not something to type in — `provision.WriteHooks` already creates the directory itself (see `internal/provision/hookcodec.go`). Write `WriteHooks` as simply:

```go
func (d *Driver) WriteHooks(ctx provision.Context, _ []provision.Hook, desired []provision.Hook) error {
	return provision.WriteHooks(openCodeHooksDir(ctx), desired, &openCodeHookCodec{})
}
```

and drop the `"os"` import if nothing else in the file needs it (it's still needed for `os.ReadFile`/`os.Remove` in the codec, so keep the import — just don't add the `MkdirAll` call).

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/provision/agents/opencode/... -v`
Expected: PASS (all tests in the package, both Task 3's and Task 4's)

- [ ] **Step 6: Commit**

```bash
git add internal/provision/hook_artifact.go internal/provision/agents/opencode/hooks.go internal/provision/agents/opencode/hooks_test.go
git commit -m "Add OpenCode hooks support"
```

---

### Task 5: Wire OpenCode into aide's registries

**Files:**
- Modify: `cmd/aide/provision_drivers.go`
- Modify: `internal/display/display.go`

**Interfaces:**
- Consumes: `opencode.New` (self-registers via blank import's `init()`, from Task 3)

- [ ] **Step 1: Add the blank import**

In `cmd/aide/provision_drivers.go`, add the import in alphabetical order:

```go
import (
	_ "github.com/jskswamy/aide/internal/provision/agents/claude"
	_ "github.com/jskswamy/aide/internal/provision/agents/codex"
	_ "github.com/jskswamy/aide/internal/provision/agents/copilot"
	_ "github.com/jskswamy/aide/internal/provision/agents/cursor"
	_ "github.com/jskswamy/aide/internal/provision/agents/gemini"
	_ "github.com/jskswamy/aide/internal/provision/agents/hermes"
	_ "github.com/jskswamy/aide/internal/provision/agents/opencode"
)
```

- [ ] **Step 2: Add the display icon**

In `internal/display/display.go`, add `"opencode": "🔷",` to `DefaultAgentIcons` (pick any icon not already used by another agent — 🔷 is a placeholder; if the repo has an icon-uniqueness lint or convention doc, follow it instead):

```go
var DefaultAgentIcons = map[string]string{
	"claude":   "🤖",
	"gemini":   "✨",
	"codex":    "📝",
	"copilot":  "✈️",
	"cursor":   "🖱",
	"opencode": "🔷",
}
```

- [ ] **Step 3: Verify the driver is registered**

Run: `go build ./... && go test ./cmd/aide/... ./internal/provision/... -v 2>&1 | tail -40`
Expected: build succeeds; no test failures. As an extra manual check, if `cmd/aide` has an existing test that calls `provision.AllProvisioners()` and asserts on the list, confirm `"opencode"` now appears (search first: `grep -rn "AllProvisioners" cmd/aide internal/provision/*_test.go`). If no such test exists, don't add one speculatively — this step is a manual sanity check, not a new test requirement.

- [ ] **Step 4: Run the full test suite**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: PASS, no build or vet errors anywhere in the module (this task's changes are wiring-only but touch shared files, so a full-module check is the right scope here, unlike the package-scoped checks in earlier tasks).

- [ ] **Step 5: Commit**

```bash
git add cmd/aide/provision_drivers.go internal/display/display.go
git commit -m "Register OpenCode as a provisionable agent"
```

---

## Self-Review Notes

Spec coverage check against `docs/superpowers/specs/2026-08-31-opencode-pi-agent-support-design.md`'s OpenCode section: sandbox (Task 1), MCP file-edit handler (Task 2), plugins file-edit (Task 3), hooks (Task 4), wiring (Task 5) — all covered. Marketplace non-goal explicitly covered by `TestOpenCodeMarketplaceMethodsNoOp`. The `AgentDirProvider`/`ProfileEnvKey` non-feature (no confirmed OpenCode home-redirect env var) is reflected by simply not implementing `AgentDir` and leaving `ProfileEnvKey` unset — nothing further needed.
