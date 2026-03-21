# Epic 4: MCP System (P1) -- Tasks 16-18

**Date:** 2026-03-18
**Epic:** MCP System
**Priority:** P1
**Tasks:** T16, T17, T18
**Dependencies:** T10 (Agent Launcher from Epic 2)
**Design Decisions:** DD-8 (MCP Aggregator Support), DD-9 (MCP Servers Need Secrets)

---

## Overview

The MCP system enables aide to manage MCP (Model Context Protocol) server
definitions, generate agent-native MCP configuration files, and optionally
route all MCP traffic through an aggregator like 1mcp. MCP server definitions
live at the top level of `config.yaml`, contexts select which servers to
activate, and aide generates the appropriate configuration at launch time into
the ephemeral runtime directory.

### Data Flow

```
config.yaml (mcp.servers)
      |
      v
Context resolution (mcp_servers list + mcp_server_overrides)
      |
      v
Template resolution (secrets injected into env/args)
      |
      v
  +-- Has aggregator? --+
  |                      |
  YES                    NO
  |                      |
  v                      v
Generate aggregator    Generate native
config + start/use     per-agent config
aggregator             (e.g. .mcp.json)
  |                      |
  v                      v
Point agent at         Point agent at
aggregator endpoint    native config file
```

---

## Task 16: MCP Server Definitions with Secrets

### Goal

Parse MCP server definitions from `config.yaml`, resolve template syntax in
`env` and `args` fields against the context's decrypted secrets, and produce
fully-resolved server definitions ready for config generation.

### Config Schema

Top-level `mcp.servers` in `config.yaml`:

```yaml
mcp:
  servers:
    git:
      command: git-mcp
    context7:
      command: context7-mcp
      env:
        CONTEXT7_TOKEN: "{{ .secrets.context7_token }}"
    serena:
      command: serena-mcp
      args: ["--project", "{{ .project_root }}"]
      env:
        SERENA_LICENSE: "{{ .secrets.serena_license }}"
    things:
      command: things-mcp
```

Context-level selection and overrides:

```yaml
contexts:
  work:
    mcp_servers: [git, context7, serena]
    mcp_server_overrides:
      serena:
        args: ["--project", "{{ .project_root }}", "--mode", "strict"]
  personal:
    mcp_servers: [git, context7, serena, things]
```

### Go Types

**File:** `internal/mcp/types.go`

```go
package mcp

// MCPConfig represents the top-level mcp: block in config.yaml.
type MCPConfig struct {
    Aggregator *AggregatorConfig        `yaml:"aggregator,omitempty"`
    Servers    map[string]ServerDef      `yaml:"servers"`
}

// ServerDef represents a single MCP server definition.
// command + args define how to launch the server process.
// env holds environment variables, which may contain template syntax.
type ServerDef struct {
    Command string            `yaml:"command"`
    Args    []string          `yaml:"args,omitempty"`
    Env     map[string]string `yaml:"env,omitempty"`
}

// AggregatorConfig defines how to reach or start an MCP aggregator.
// Exactly one of Command or URL must be set.
type AggregatorConfig struct {
    Command string   `yaml:"command,omitempty"`
    Args    []string `yaml:"args,omitempty"`
    URL     string   `yaml:"url,omitempty"`
}

// ResolvedServer is a ServerDef after template resolution.
// All {{ .secrets.* }} and {{ .project_root }} placeholders have been
// replaced with actual values. This struct is identical to ServerDef
// but exists as a separate type to make the resolved/unresolved
// boundary explicit in function signatures.
type ResolvedServer struct {
    Command string
    Args    []string
    Env     map[string]string
}
```

### Server Selection and Override Merge

**File:** `internal/mcp/selection.go`

Implement a function that takes the global `mcp.servers` map, a context's
`mcp_servers` list, and the context's `mcp_server_overrides` map, and returns
the selected and merged server definitions.

```go
// SelectServers returns the server definitions for the given context.
//
// Algorithm:
//   1. For each name in selectedNames, look up the global definition.
//      Return an error if a name is not found in globalServers.
//   2. If an override exists for that name, deep-merge the override
//      on top of the global definition:
//      - override.Command replaces global.Command (if non-empty)
//      - override.Args replaces global.Args entirely (if non-nil)
//      - override.Env merges with global.Env (override keys win)
//   3. Return map[string]ServerDef of selected+merged servers.
func SelectServers(
    globalServers map[string]ServerDef,
    selectedNames []string,
    overrides map[string]ServerDef,
) (map[string]ServerDef, error)
```

### Template Resolution

**File:** `internal/mcp/resolve.go`

Uses `internal/config/template.go` (from T8) to resolve template placeholders.
The template context includes:

| Variable | Source |
|----------|--------|
| `.secrets.<key>` | Decrypted secrets map from context's `secret` |
| `.project_root` | Git root of current working directory |
| `.runtime_dir` | Ephemeral runtime directory path |

```go
// ResolveServer resolves all template placeholders in a ServerDef.
// It processes command, each element of args, and each value in env.
// Returns a ResolvedServer with all templates expanded.
// Returns an error if any template references a missing key.
func ResolveServer(def ServerDef, templateCtx map[string]interface{}) (ResolvedServer, error)

// ResolveAll resolves a map of selected ServerDefs into ResolvedServers.
func ResolveAll(servers map[string]ServerDef, templateCtx map[string]interface{}) (map[string]ResolvedServer, error)
```

### Error Handling

- Unknown server name in `mcp_servers` list:
  `"MCP server 'foo' referenced in context 'work' not found in mcp.servers. Available: git, context7, serena, things"`
- Template resolution failure:
  `"MCP server 'context7' env var 'CONTEXT7_TOKEN': key 'context7_token' not found in secrets. Available keys: anthropic_api_key, aws_profile"`

### File Paths

| File | Purpose |
|------|---------|
| `internal/mcp/types.go` | Type definitions for MCPConfig, ServerDef, AggregatorConfig, ResolvedServer |
| `internal/mcp/selection.go` | SelectServers: filter + merge overrides |
| `internal/mcp/resolve.go` | ResolveServer/ResolveAll: template expansion |
| `internal/mcp/selection_test.go` | Tests for selection and merge |
| `internal/mcp/resolve_test.go` | Tests for template resolution |

### TDD Tests

**File:** `internal/mcp/selection_test.go`

| Test | Description |
|------|-------------|
| `TestSelectServers_BasicSelection` | Given 4 global servers and `mcp_servers: [git, context7]`, returns only those 2 servers with unchanged definitions. |
| `TestSelectServers_UnknownServer` | `mcp_servers: [nonexistent]` returns error listing available servers. |
| `TestSelectServers_OverrideArgs` | Override replaces `args` entirely. Global serena has `args: ["--project", "X"]`, override has `args: ["--project", "X", "--mode", "strict"]`. Result has 3 args. |
| `TestSelectServers_OverrideEnv` | Override merges env. Global has `KEY1: val1`. Override has `KEY2: val2`. Result has both keys. Override key wins on conflict. |
| `TestSelectServers_OverrideCommand` | Non-empty override command replaces global command. Empty override command keeps global. |
| `TestSelectServers_EmptySelection` | Empty `mcp_servers` list returns empty map (no error). |
| `TestSelectServers_NoOverrides` | Nil overrides map works without error. |

**File:** `internal/mcp/resolve_test.go`

| Test | Description |
|------|-------------|
| `TestResolveServer_SecretsInEnv` | Server with `env: {TOKEN: "{{ .secrets.my_token }}"}` resolves to actual secret value. |
| `TestResolveServer_ProjectRootInArgs` | Server with `args: ["--project", "{{ .project_root }}"]` resolves to `/home/user/myproject`. |
| `TestResolveServer_NoTemplates` | Server with literal values passes through unchanged. |
| `TestResolveServer_MissingSecret` | Template referencing `.secrets.nonexistent` returns error with available keys. |
| `TestResolveServer_MultipleTemplatesInOneValue` | Value like `"{{ .project_root }}/{{ .secrets.subdir }}"` resolves both. |
| `TestResolveAll_MixedServers` | Mix of servers with and without templates all resolve correctly. |

### Verification

```bash
go test ./internal/mcp/ -run 'TestSelectServers|TestResolveServer|TestResolveAll' -v
```

---

## Task 17: MCP Config Generation (Per-Agent Native Format)

### Goal

Generate the native MCP configuration file for each supported agent and write
it to the ephemeral runtime directory. This is the fallback path when no
aggregator is configured.

### Native Format: Claude `.mcp.json`

Claude Code reads MCP server configuration from `.mcp.json`. The file uses
this structure:

```json
{
  "mcpServers": {
    "git": {
      "command": "git-mcp",
      "args": [],
      "env": {}
    },
    "context7": {
      "command": "context7-mcp",
      "args": [],
      "env": {
        "CONTEXT7_TOKEN": "actual-decrypted-token"
      }
    },
    "serena": {
      "command": "serena-mcp",
      "args": ["--project", "/home/user/myproject"],
      "env": {
        "SERENA_LICENSE": "actual-license-key"
      }
    }
  }
}
```

### Generator Interface

**File:** `internal/mcp/generator.go`

```go
package mcp

import "io"

// Generator produces agent-native MCP configuration from resolved servers.
type Generator interface {
    // Generate writes the MCP config for a specific agent format to w.
    Generate(servers map[string]ResolvedServer, w io.Writer) error

    // Filename returns the config filename for this agent format.
    // e.g., ".mcp.json" for Claude.
    Filename() string
}

// NewGenerator returns the appropriate Generator for the given agent name.
// Returns an error if the agent is not supported for native MCP config.
func NewGenerator(agentName string) (Generator, error)
```

### Claude Generator

**File:** `internal/mcp/generator_claude.go`

```go
// ClaudeGenerator produces .mcp.json format.
type ClaudeGenerator struct{}

func (g *ClaudeGenerator) Filename() string {
    return ".mcp.json"
}

func (g *ClaudeGenerator) Generate(servers map[string]ResolvedServer, w io.Writer) error {
    // Convert map[string]ResolvedServer to the Claude JSON structure:
    // {
    //   "mcpServers": {
    //     "<name>": {
    //       "command": "...",
    //       "args": [...],
    //       "env": {...}
    //     }
    //   }
    // }
    //
    // Rules:
    // - args: write as empty array [] if nil/empty (not omitted)
    // - env: write as empty object {} if nil/empty (not omitted)
    // - JSON must be pretty-printed (indented) for debuggability
    // - Use encoding/json with SetIndent("", "  ")
}
```

### Writing to Runtime Directory

**File:** `internal/mcp/writer.go`

```go
// WriteNativeConfig generates the agent-native MCP config file and writes
// it to the runtime directory. Returns the absolute path of the written file.
//
// The runtime directory is expected to already exist (created by T9).
// The file is written with mode 0600 (owner read/write only).
//
// For Claude, the file is written as <runtimeDir>/.mcp.json.
// The launcher (T10) must set the working directory or pass the config
// path so Claude discovers it.
func WriteNativeConfig(
    agentName string,
    servers map[string]ResolvedServer,
    runtimeDir string,
) (configPath string, err error)
```

### Agent Config Discovery

Each agent discovers MCP config differently:

| Agent | Config File | Discovery Mechanism |
|-------|------------|---------------------|
| Claude | `.mcp.json` | Looks in CWD, then walks up to home. aide writes to runtime dir and sets `--mcp-config` flag or symlinks from project root. |

For Claude, the launcher should pass `--mcp-config <path>` pointing to the
runtime directory's `.mcp.json`. If the agent does not support a flag for
config path, aide can set an env var or symlink. The exact mechanism is
agent-specific and encapsulated in the generator.

> **Implementation Advisory: Verify `--mcp-config` flag.**
> The spec assumes `--mcp-config` is a valid Claude CLI flag. The implementer
> must verify this against Claude's actual CLI documentation before
> implementation. If `--mcp-config` does not exist, use the following fallback
> strategy: aide should symlink the generated config to the location Claude
> expects (e.g., `~/.claude/.mcp.json` for global scope or a project-level
> `.mcp.json` in the working directory), and register a cleanup step to remove
> the symlink on exit (normal, signal, or stale-dir cleanup). The
> `LaunchHints` struct should be updated accordingly -- if symlinking is used,
> `ExtraArgs` may be empty and the symlink path should be tracked for cleanup
> via a new `CleanupPaths []string` field or similar mechanism.

```go
// LaunchHints returns agent-specific flags or env vars needed to point
// the agent at the generated MCP config file.
// For Claude: returns ["--mcp-config", "<configPath>"]
func (g *ClaudeGenerator) LaunchHints(configPath string) LaunchHints

type LaunchHints struct {
    ExtraArgs []string          // Additional CLI args for the agent
    ExtraEnv  map[string]string // Additional env vars for the agent
}
```

### Extending to Other Agents

When adding support for Gemini, Codex, or other agents:

1. Create `internal/mcp/generator_<agent>.go` implementing `Generator`.
2. Register it in `NewGenerator()`.
3. Each generator knows its own config file format and filename.

### File Paths

| File | Purpose |
|------|---------|
| `internal/mcp/generator.go` | Generator interface, NewGenerator factory, LaunchHints type |
| `internal/mcp/generator_claude.go` | Claude `.mcp.json` generator |
| `internal/mcp/writer.go` | WriteNativeConfig: orchestrates generation + file write |
| `internal/mcp/generator_claude_test.go` | Tests for Claude config generation |
| `internal/mcp/writer_test.go` | Tests for file writing |

### TDD Tests

**File:** `internal/mcp/generator_claude_test.go`

| Test | Description |
|------|-------------|
| `TestClaudeGenerator_EmptyServers` | No servers produces `{"mcpServers": {}}`. |
| `TestClaudeGenerator_SingleServer` | One server with command only. Verify `args` is `[]` and `env` is `{}` in output. |
| `TestClaudeGenerator_MultipleServers` | Three servers with various args/env. Parse output JSON and verify all fields. |
| `TestClaudeGenerator_EnvValues` | Server with env containing resolved secret values. Verify values appear in output JSON. |
| `TestClaudeGenerator_Filename` | `Filename()` returns `.mcp.json`. |
| `TestClaudeGenerator_LaunchHints` | Given config path `/tmp/aide-12345/.mcp.json`, returns `--mcp-config /tmp/aide-12345/.mcp.json` in ExtraArgs. |
| `TestClaudeGenerator_OutputIsValidJSON` | Output parses without error via `json.Unmarshal`. |
| `TestClaudeGenerator_PrettyPrinted` | Output contains newlines and indentation (not minified). |

**File:** `internal/mcp/writer_test.go`

| Test | Description |
|------|-------------|
| `TestWriteNativeConfig_CreatesFile` | File exists at expected path after call. |
| `TestWriteNativeConfig_FilePermissions` | File has mode 0600. |
| `TestWriteNativeConfig_ContentMatchesGenerator` | File content matches what generator produces. |
| `TestWriteNativeConfig_UnknownAgent` | Returns error for unsupported agent name. |
| `TestWriteNativeConfig_ReturnsAbsolutePath` | Returned path is absolute. |

### Verification

```bash
go test ./internal/mcp/ -run 'TestClaudeGenerator|TestWriteNativeConfig' -v
```

---

## Task 18: MCP Aggregator Support (1mcp Config Generation)

### Goal

When an aggregator is configured in `mcp.aggregator`, aide generates the
aggregator's config file, optionally starts the aggregator process, and points
the agent at the aggregator's endpoint instead of individual MCP servers.

### Aggregator Modes

There are two ways to configure an aggregator:

#### Mode 1: Command-Based (aide starts the aggregator)

```yaml
mcp:
  aggregator:
    command: 1mcp
    args: ["--config", "{{ .runtime_dir }}/1mcp-config.json"]
```

aide starts 1mcp as a child process, generates its config, and points the
agent at the aggregator's stdio or URL endpoint.

#### Mode 2: URL-Based (aggregator already running)

```yaml
mcp:
  aggregator:
    url: http://localhost:3000
```

aide generates config and writes it where the running aggregator can read it,
or passes server definitions via the aggregator's API. The agent is pointed
at the URL.

### 1mcp Config Format

1mcp expects a JSON config listing the MCP servers to aggregate. Based on
1mcp's configuration format:

```json
{
  "mcpServers": {
    "git": {
      "command": "git-mcp",
      "args": [],
      "env": {}
    },
    "context7": {
      "command": "context7-mcp",
      "args": [],
      "env": {
        "CONTEXT7_TOKEN": "actual-token"
      }
    }
  }
}
```

The agent is then configured to connect to 1mcp as a single MCP server
instead of multiple individual servers.

### Aggregator Interface

**File:** `internal/mcp/aggregator.go`

```go
package mcp

// Aggregator manages the lifecycle of an MCP aggregator process
// and generates the agent-facing config to connect to it.
type Aggregator interface {
    // GenerateConfig writes the aggregator's own config file listing
    // all the MCP servers it should aggregate. Returns the config path.
    GenerateConfig(servers map[string]ResolvedServer, runtimeDir string) (configPath string, err error)

    // Start launches the aggregator process (command-based mode only).
    // Returns a handle for stopping the process on cleanup.
    // For URL-based mode, this is a no-op that returns nil.
    Start(configPath string, runtimeDir string) (AggregatorProcess, error)

    // AgentServerDef returns the MCP server definition that the agent
    // should use to connect to the aggregator. This replaces all
    // individual server definitions in the agent's MCP config.
    //
    // For command-based: returns a definition with command/args pointing
    // to the aggregator binary with the generated config.
    // For URL-based: returns a definition using the URL.
    AgentServerDef() ResolvedServer
}

// AggregatorProcess represents a running aggregator that needs cleanup.
type AggregatorProcess interface {
    // Stop terminates the aggregator process gracefully.
    Stop() error
    // PID returns the process ID (for logging/debug).
    PID() int
}
```

### 1mcp Aggregator Implementation

**File:** `internal/mcp/aggregator_1mcp.go`

```go
// OneMCPAggregator implements Aggregator for the 1mcp tool.
type OneMCPAggregator struct {
    config AggregatorConfig
}

func NewOneMCPAggregator(config AggregatorConfig) *OneMCPAggregator {
    return &OneMCPAggregator{config: config}
}
```

#### GenerateConfig

Writes a JSON config file to `<runtimeDir>/1mcp-config.json` with the
`mcpServers` structure shown above. File permissions: 0600.

#### Start (Command-Based)

1. Build the command: `1mcp --config <configPath>` (or however 1mcp accepts
   its config).
2. Start the process via `os/exec.Cmd`.
3. Capture stdout/stderr for logging.
4. Return an `AggregatorProcess` wrapping the `*exec.Cmd`.
5. The process is a child of aide and will be killed during cleanup.

#### AgentServerDef

For command-based mode, the agent's MCP config should list the aggregator
as a single server:

```json
{
  "mcpServers": {
    "1mcp": {
      "command": "1mcp",
      "args": ["--config", "/tmp/aide-12345/1mcp-config.json"]
    }
  }
}
```

For URL-based mode, the agent config points to the URL. The exact mechanism
depends on agent support for URL-based MCP servers. For Claude with SSE:

```json
{
  "mcpServers": {
    "1mcp": {
      "type": "sse",
      "url": "http://localhost:3000"
    }
  }
}
```

> **Implementation Advisory: Verify SSE/URL-based MCP config schema.**
> The JSON structure above (using `"type": "sse"` and `"url"`) is assumed
> based on common MCP patterns but has not been validated against Claude's
> actual MCP config schema. Before implementing, verify the exact schema
> Claude expects for URL/SSE-based MCP servers by consulting Claude's
> official documentation. The field names, nesting, and supported transport
> types (SSE vs streamable-http vs other) may differ from what is shown here.

### Orchestration

**File:** `internal/mcp/orchestrator.go`

This is the top-level entry point called by the launcher (T10). It decides
whether to use aggregator or native mode and produces the final result.

```go
// MCPResult contains everything the launcher needs to configure MCP.
type MCPResult struct {
    // LaunchHints to pass to the agent (extra args/env).
    Hints LaunchHints
    // Cleanup function to call on exit (stops aggregator, etc.).
    Cleanup func() error
}

// Setup is the top-level MCP orchestration function.
// Called by the launcher after context resolution and secret decryption.
//
// Algorithm:
//   1. SelectServers: filter global servers by context's mcp_servers list,
//      apply mcp_server_overrides.
//   2. ResolveAll: expand templates in all selected servers.
//   3. If aggregator configured:
//      a. GenerateConfig with resolved servers.
//      b. Start aggregator (command-based) or validate URL (URL-based).
//      c. Generate native agent config with single aggregator server entry.
//      d. Return LaunchHints pointing agent at native config + cleanup func.
//   4. If no aggregator:
//      a. WriteNativeConfig with all resolved servers.
//      b. Return LaunchHints pointing agent at native config + no-op cleanup.
func Setup(
    mcpConfig MCPConfig,
    selectedNames []string,
    overrides map[string]ServerDef,
    templateCtx map[string]interface{},
    agentName string,
    runtimeDir string,
) (*MCPResult, error)
```

### Fallback Behavior (DD-8)

Per DD-8, if no `mcp.aggregator` is configured, aide falls back to native
per-agent config. The `Setup` function handles this transparently:

```
mcp.aggregator present?
  YES -> aggregator path (generate aggregator config + agent points to aggregator)
  NO  -> native path (generate per-agent config with all servers)
```

### Cleanup Integration

The aggregator process must be stopped when aide exits. The `MCPResult.Cleanup`
function is called by the launcher's cleanup sequence (which also removes the
runtime directory). Order:

1. Stop aggregator process (if command-based)
2. Remove runtime directory (existing T9 cleanup)

Signal handlers (SIGTERM, SIGINT, etc.) must call `MCPResult.Cleanup` before
exiting.

### File Paths

| File | Purpose |
|------|---------|
| `internal/mcp/aggregator.go` | Aggregator interface, AggregatorProcess interface |
| `internal/mcp/aggregator_1mcp.go` | 1mcp implementation of Aggregator |
| `internal/mcp/orchestrator.go` | Setup function: top-level MCP orchestration |
| `internal/mcp/aggregator_1mcp_test.go` | Tests for 1mcp aggregator |
| `internal/mcp/orchestrator_test.go` | Tests for end-to-end orchestration |

### TDD Tests

**File:** `internal/mcp/aggregator_1mcp_test.go`

| Test | Description |
|------|-------------|
| `TestOneMCP_GenerateConfig_Structure` | Generated JSON has `mcpServers` key with correct server entries. |
| `TestOneMCP_GenerateConfig_FilePermissions` | Config file written with mode 0600. |
| `TestOneMCP_GenerateConfig_ResolvedValues` | Template-resolved values (not placeholders) appear in generated config. |
| `TestOneMCP_GenerateConfig_EmptyServers` | Empty server map produces valid JSON with empty `mcpServers`. |
| `TestOneMCP_AgentServerDef_CommandBased` | Command-based aggregator returns a ResolvedServer with command=`1mcp` and args including `--config` path. |
| `TestOneMCP_AgentServerDef_URLBased` | URL-based aggregator returns a ResolvedServer with the configured URL. |

**File:** `internal/mcp/orchestrator_test.go`

| Test | Description |
|------|-------------|
| `TestSetup_NoAggregator_NativeConfig` | No aggregator configured. Verify native `.mcp.json` is written with all resolved servers. Verify LaunchHints contain `--mcp-config` pointing to the file. |
| `TestSetup_NoAggregator_NoMCPServers` | Empty `mcp_servers` list with no aggregator. No config file generated, empty LaunchHints. |
| `TestSetup_WithAggregator_Command` | Command-based aggregator. Verify aggregator config is generated, agent config has single aggregator entry, cleanup function is non-nil. |
| `TestSetup_WithAggregator_URL` | URL-based aggregator. Verify agent config points to URL, cleanup is no-op. |
| `TestSetup_OverrideMerge` | Context with `mcp_server_overrides` modifying args. Verify merged values appear in generated config (both aggregator and native paths). |
| `TestSetup_TemplateResolution` | Servers with `{{ .secrets.x }}` templates. Verify resolved values in generated config, no template syntax remains. |
| `TestSetup_TemplateError` | Missing secret key. Verify descriptive error returned with available keys. |
| `TestSetup_CleanupRemovesAggregator` | Start command-based aggregator, call cleanup, verify process is stopped (use a mock or test binary). |

### Verification

```bash
# Run all MCP tests
go test ./internal/mcp/ -v

# Run only aggregator tests
go test ./internal/mcp/ -run 'TestOneMCP|TestSetup' -v

# Verify no template placeholders leak into generated configs
# (this is covered by TestSetup_TemplateResolution but good to run manually)
go test ./internal/mcp/ -run 'TestSetup_TemplateResolution' -v
```

---

## Integration with Launcher (T10)

The launcher calls `mcp.Setup()` as part of step 5 in the launch flow
(from DESIGN.md):

```
1. Read config.yaml
2. Resolve context
3. Decrypt secrets in memory
4. Create runtime dir ($XDG_RUNTIME_DIR/aide-<pid>/)
5. ** MCP Setup ** <-- this epic
6. Build env vars
7. Apply sandbox policy
8. Exec agent with env + MCP config path
9. On exit: MCPResult.Cleanup() then rm -rf runtime dir
```

The launcher passes `MCPResult.Hints.ExtraArgs` to the agent binary and
`MCPResult.Hints.ExtraEnv` to the agent's environment. It registers
`MCPResult.Cleanup` in its signal handler chain.

---

## Security Considerations (DD-9, DD-10)

- Resolved secrets appear in generated config files (both native and
  aggregator). These files MUST live in the ephemeral runtime directory
  (`$XDG_RUNTIME_DIR/aide-<pid>/`, tmpfs, mode 0700).
- Config files are written with mode 0600.
- All generated files are cleaned up on exit (normal, signal, or via stale
  dir cleanup on next launch).
- Secrets are never logged. Verbose mode (`-v`) should log server names and
  commands but redact env values: `"MCP server 'context7': command=context7-mcp, env=[CONTEXT7_TOKEN=<redacted>]"`.

---

## Complete File Manifest

```
internal/mcp/
  types.go                    # T16: MCPConfig, ServerDef, AggregatorConfig, ResolvedServer
  selection.go                # T16: SelectServers (filter + merge overrides)
  selection_test.go           # T16: Tests for selection/merge
  resolve.go                  # T16: ResolveServer, ResolveAll (template expansion)
  resolve_test.go             # T16: Tests for template resolution
  generator.go                # T17: Generator interface, NewGenerator, LaunchHints
  generator_claude.go         # T17: Claude .mcp.json generator
  generator_claude_test.go    # T17: Tests for Claude generator
  writer.go                   # T17: WriteNativeConfig
  writer_test.go              # T17: Tests for writer
  aggregator.go               # T18: Aggregator interface, AggregatorProcess
  aggregator_1mcp.go          # T18: 1mcp implementation
  aggregator_1mcp_test.go     # T18: Tests for 1mcp aggregator
  orchestrator.go             # T18: Setup function (top-level entry point)
  orchestrator_test.go        # T18: End-to-end orchestration tests
```

---

## Implementation Order

1. **T16** first: types, selection, resolve. These are pure functions with no
   I/O dependencies (except template resolution reusing T8).
2. **T17** next: generator interface, Claude generator, writer. Depends on
   T16's `ResolvedServer` type.
3. **T18** last: aggregator interface, 1mcp implementation, orchestrator.
   Depends on both T16 and T17. The orchestrator wires everything together.

Each task is independently testable. T16 tests use only in-memory data. T17
tests write to `t.TempDir()`. T18 tests use `t.TempDir()` and may use a
mock aggregator process (a simple `sleep` binary or `exec.Command("true")`).
