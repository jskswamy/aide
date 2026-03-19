# Epic 1: Core Config System (P0)

**Date:** 2026-03-18
**Status:** Draft
**Design Refs:** DD-3, DD-5, DD-6, DD-12, DD-18

## Purpose and Scope

This epic establishes the foundational config layer for aide. After completing
Tasks 1-5, the system can:

- Parse both minimal (flat) and full (multi-context) config formats
- Resolve XDG-compliant config and runtime paths
- Load and merge global config with per-project `.aide.yaml` overrides
- Detect the git remote URL and project root for any working directory
- Match the current working directory + git remote against context rules with
  deterministic specificity ranking

No secrets decryption, agent launching, or sandboxing is included here. Those
belong to Epic 2 and Epic 3. This epic produces pure data types, loaders,
and matchers that later epics depend on.

## Dependencies

```
gopkg.in/yaml.v3          # YAML parsing
github.com/gobwas/glob     # Glob pattern matching for context rules
github.com/adrg/xdg        # XDG base directory resolution (DD-3)
```

## Dependency Graph (within this epic)

```
T1 (schema types) --> T2 (XDG paths)
T2 --> T3 (config loader)
T1 --> T4 (git remote)
T3 + T4 --> T5 (context matcher)
```

---

## Go Types

All config types live in `internal/config/schema.go`. YAML tags use lowercase
with underscores to match the config file format shown in DESIGN.md.

```go
package config

// Config is the top-level configuration, supporting both minimal and full formats.
// When the YAML contains "agents" or "contexts" keys, it is the full (structured)
// format. Otherwise it is the minimal (flat) format treated as a single default context.
type Config struct {
    // --- Full format fields ---
    Agents         map[string]AgentDef  `yaml:"agents,omitempty"`
    MCP            *MCPConfig           `yaml:"mcp,omitempty"`
    Contexts       map[string]Context   `yaml:"contexts,omitempty"`
    DefaultContext string               `yaml:"default_context,omitempty"`

    // --- Minimal (flat) format fields ---
    // These are promoted to a synthetic "default" context during loading.
    Agent       string            `yaml:"agent,omitempty"`
    Env         map[string]string `yaml:"env,omitempty"`
    SecretsFile string            `yaml:"secrets_file,omitempty"`
    MCPServers  []string          `yaml:"mcp_servers,omitempty"`

    // --- Project override (populated by loader, not from YAML) ---
    // Holds .aide.yaml data to be merged on top of the matched context at
    // resolution time. Not serialized to YAML.
    ProjectOverride *ProjectOverride `yaml:"-"`
}

// IsMinimal returns true when the config uses the flat single-context format.
// Detection: if neither "agents" nor "contexts" maps are populated, it is minimal. (DD-12)
func (c *Config) IsMinimal() bool {
    return len(c.Agents) == 0 && len(c.Contexts) == 0
}

// AgentDef defines an agent binary. Agents carry no env or secrets (DD-5).
type AgentDef struct {
    Binary string `yaml:"binary"`
}

// Context holds everything needed to launch an agent in a specific environment.
// Env, secrets, and MCP selection live here, not on the agent (DD-5).
type Context struct {
    Match              []MatchRule       `yaml:"match,omitempty"`
    Agent              string            `yaml:"agent"`
    SecretsFile        string            `yaml:"secrets_file,omitempty"`
    Env                map[string]string `yaml:"env,omitempty"`
    MCPServers         []string          `yaml:"mcp_servers,omitempty"`
    MCPServerOverrides map[string]MCPServer `yaml:"mcp_server_overrides,omitempty"`
    Sandbox            *SandboxPolicy    `yaml:"sandbox,omitempty"`
}

// MatchRule is a single rule in a context's match list.
// Exactly one of Remote or Path should be set per rule.
type MatchRule struct {
    Remote     string `yaml:"remote,omitempty"`
    Path       string `yaml:"path,omitempty"`
    RemoteName string `yaml:"remote_name,omitempty"` // defaults to "origin"
}

// MCPConfig is the top-level MCP section, shared across all contexts.
type MCPConfig struct {
    Aggregator *MCPAggregator       `yaml:"aggregator,omitempty"`
    Servers    map[string]MCPServer  `yaml:"servers,omitempty"`
}

// MCPServer defines a single MCP server.
type MCPServer struct {
    Command string            `yaml:"command,omitempty"`
    URL     string            `yaml:"url,omitempty"`
    Args    []string          `yaml:"args,omitempty"`
    Env     map[string]string `yaml:"env,omitempty"`
}

// MCPAggregator defines an MCP aggregator (e.g. 1mcp).
type MCPAggregator struct {
    Command string `yaml:"command,omitempty"`
    URL     string `yaml:"url,omitempty"`
}

// SandboxPolicy defines the OS-native sandbox constraints for an agent.
type SandboxPolicy struct {
    Writable        []string `yaml:"writable,omitempty"`
    Readable        []string `yaml:"readable,omitempty"`
    Denied          []string `yaml:"denied,omitempty"`
    Network         string   `yaml:"network,omitempty"`          // "outbound" | "none" | "unrestricted"
    AllowSubprocess bool     `yaml:"allow_subprocess,omitempty"`
    CleanEnv        bool     `yaml:"clean_env,omitempty"`
}
```

### Minimal vs Full Format Examples

**Minimal** (flat, single context -- DD-12):

```yaml
agent: claude
env:
  ANTHROPIC_API_KEY: "{{ .secrets.anthropic_api_key }}"
secrets_file: personal.enc.yaml
mcp_servers: [git, context7]
```

**Full** (multi-context -- DD-5):

```yaml
agents:
  claude:
    binary: claude
contexts:
  personal:
    match:
      - remote: "github.com/jskswamy/*"
    agent: claude
    secrets_file: personal.enc.yaml
    env:
      ANTHROPIC_API_KEY: "{{ .secrets.anthropic_api_key }}"
    mcp_servers: [git, context7]
default_context: personal
```

---

## Task 1: Go Module Init + Config Schema Types

### Context

This is the foundation. Initialize the Go module and define all config
schema types so that later tasks can import them.

### Steps

1. **Initialize Go module**

   ```bash
   cd /Users/subramk/source/github.com/jskswamy/aide
   go mod init github.com/jskswamy/aide
   ```

2. **Create `internal/config/schema.go`**

   File path: `internal/config/schema.go`

   Implement all types listed in the Go Types section above: `Config`,
   `AgentDef`, `Context`, `MatchRule`, `MCPConfig`, `MCPServer`,
   `MCPAggregator`, `SandboxPolicy`.

   Include the `IsMinimal()` method on `Config`.

3. **Create `internal/config/schema_test.go`**

   File path: `internal/config/schema_test.go`

   Tests (TDD -- write these first):

   | Test Name | Description |
   |-----------|-------------|
   | `TestConfig_IsMinimal_True` | Unmarshal a minimal YAML (just `agent` + `env`), assert `IsMinimal()` returns `true` |
   | `TestConfig_IsMinimal_False` | Unmarshal a full YAML (with `agents` and `contexts` maps), assert `IsMinimal()` returns `false` |
   | `TestConfig_UnmarshalMinimal` | Round-trip: marshal a minimal `Config`, unmarshal it back, compare fields |
   | `TestConfig_UnmarshalFull` | Round-trip: marshal a full `Config` with agents, contexts, MCP, sandbox, unmarshal and compare |
   | `TestMatchRule_RemoteOnly` | Unmarshal `{remote: "github.com/org/*"}`, assert `.Remote` is set, `.Path` is empty |
   | `TestMatchRule_PathOnly` | Unmarshal `{path: "~/work/*"}`, assert `.Path` is set, `.Remote` is empty |
   | `TestSandboxPolicy_Defaults` | Unmarshal empty sandbox YAML, assert zero values (no implicit defaults at schema level) |

4. **Add yaml.v3 dependency**

   ```bash
   go get gopkg.in/yaml.v3
   ```

### Verification

```bash
cd /Users/subramk/source/github.com/jskswamy/aide
go build ./internal/config/...
go test ./internal/config/... -run TestConfig -v
go vet ./internal/config/...
```

---

## Task 2: XDG Config Path Resolution

### Context

aide keeps everything under `$XDG_CONFIG_HOME/aide/` (DD-6). No data/config
split. Runtime dirs use `$XDG_RUNTIME_DIR` for ephemeral secrets (DD-10).
Use the `adrg/xdg` library for cross-platform correctness (DD-3).

### Steps

1. **Create `internal/config/paths.go`**

   File path: `internal/config/paths.go`

   ```go
   package config

   import (
       "fmt"
       "os"
       "path/filepath"

       "github.com/adrg/xdg"
   )

   const appName = "aide"

   // --- Parameterized variants (testable, no dependency on cached xdg values) ---

   // ConfigDirFrom returns the aide config directory under the given base.
   // Production callers pass xdg.ConfigHome; tests pass a temp dir.
   func ConfigDirFrom(base string) string {
       return filepath.Join(base, appName)
   }

   // SecretsDirFrom returns the secrets directory under the given base.
   func SecretsDirFrom(base string) string {
       return filepath.Join(ConfigDirFrom(base), "secrets")
   }

   // RuntimeDirFrom returns a per-process ephemeral directory under the given base.
   func RuntimeDirFrom(base string, pid int) string {
       return filepath.Join(base, fmt.Sprintf("%s-%d", appName, pid))
   }

   // ConfigFilePathFrom returns the global config file path under the given base.
   func ConfigFilePathFrom(base string) string {
       return filepath.Join(ConfigDirFrom(base), "config.yaml")
   }

   // ResolveSecretsFilePathFrom resolves a secrets_file value to an absolute path.
   // If the value is already absolute, return it as-is.
   // Otherwise, resolve relative to SecretsDirFrom(base).
   func ResolveSecretsFilePathFrom(base, secretsFile string) string {
       if filepath.IsAbs(secretsFile) {
           return secretsFile
       }
       return filepath.Join(SecretsDirFrom(base), secretsFile)
   }

   // --- Convenience wrappers (use adrg/xdg cached values) ---

   // ConfigDir returns the aide config directory.
   // $XDG_CONFIG_HOME/aide/ (typically ~/.config/aide/)
   // Everything lives here: config.yaml and secrets/ (DD-6).
   func ConfigDir() string {
       return ConfigDirFrom(xdg.ConfigHome)
   }

   // SecretsDir returns the directory for encrypted secrets files.
   // $XDG_CONFIG_HOME/aide/secrets/
   func SecretsDir() string {
       return SecretsDirFrom(xdg.ConfigHome)
   }

   // RuntimeDir returns a per-process ephemeral directory on tmpfs.
   // $XDG_RUNTIME_DIR/aide-<pid>/
   // Used for generated MCP configs and other material that must not persist (DD-10).
   // The caller is responsible for creating and cleaning this directory.
   func RuntimeDir(pid int) string {
       return RuntimeDirFrom(xdg.RuntimeDir, pid)
   }

   // ConfigFilePath returns the path to the global config file.
   // $XDG_CONFIG_HOME/aide/config.yaml
   func ConfigFilePath() string {
       return ConfigFilePathFrom(xdg.ConfigHome)
   }

   // ProjectConfigFileName is the per-project override filename.
   const ProjectConfigFileName = ".aide.yaml"

   // ResolveSecretsFilePath resolves a secrets_file value to an absolute path.
   // If the value is already absolute, return it as-is.
   // Otherwise, resolve relative to SecretsDir().
   func ResolveSecretsFilePath(secretsFile string) string {
       return ResolveSecretsFilePathFrom(xdg.ConfigHome, secretsFile)
   }
   ```

2. **Create `internal/config/paths_test.go`**

   File path: `internal/config/paths_test.go`

   Tests:

   | Test Name | Description |
   |-----------|-------------|
   | `TestConfigDirFrom` | `ConfigDirFrom("/tmp/xdg")` returns `"/tmp/xdg/aide"` |
   | `TestConfigDir_Default` | Assert `ConfigDir()` ends with `.config/aide` (uses xdg cached value; no env mutation) |
   | `TestSecretsDirFrom` | `SecretsDirFrom("/tmp/xdg")` returns `"/tmp/xdg/aide/secrets"` |
   | `TestRuntimeDirFrom_ContainsPID` | `RuntimeDirFrom("/tmp/run", 12345)` returns `"/tmp/run/aide-12345"` |
   | `TestConfigFilePathFrom` | `ConfigFilePathFrom("/tmp/xdg")` returns `"/tmp/xdg/aide/config.yaml"` |
   | `TestResolveSecretsFilePathFrom_Relative` | `ResolveSecretsFilePathFrom("/tmp/xdg", "personal.enc.yaml")` returns `"/tmp/xdg/aide/secrets/personal.enc.yaml"` |
   | `TestResolveSecretsFilePathFrom_Absolute` | `ResolveSecretsFilePathFrom("/tmp/xdg", "/custom/path/keys.yaml")` returns `"/custom/path/keys.yaml"` unchanged |

   **Important:** `adrg/xdg` caches `XDG_CONFIG_HOME` at init time, so
   `t.Setenv()` will NOT affect its cached values. To make path functions
   testable, do NOT rely on `adrg/xdg` directly in tests. Instead:

   - Add an optional `configDir` parameter (or a `PathResolver` struct) so
     tests can inject custom base paths without depending on env vars.
   - Production code calls the zero-arg convenience functions (which use
     `adrg/xdg` internally). Tests call the parameterized variants.

   For example:

   ```go
   // ConfigDirFrom returns the aide config directory under the given base.
   // Production callers pass xdg.ConfigHome; tests pass a temp dir.
   func ConfigDirFrom(base string) string {
       return filepath.Join(base, appName)
   }

   // ConfigDir is the convenience wrapper for production use.
   func ConfigDir() string {
       return ConfigDirFrom(xdg.ConfigHome)
   }
   ```

   Tests then call `ConfigDirFrom(t.TempDir())` and assert against the
   returned path without any env mutation.

3. **Add xdg dependency**

   ```bash
   go get github.com/adrg/xdg
   ```

### Verification

```bash
go test ./internal/config/... -run "TestConfigDir|TestConfigFilePathFrom|TestSecretsDirFrom|TestRuntimeDirFrom|TestResolveSecretsFilePath" -v
```

---

## Task 3: Config Loader (Global + Project Merge)

### Context

The loader reads `config.yaml` from the aide config directory and optionally
merges a per-project `.aide.yaml`. It must detect the config format (minimal
vs full) and normalize minimal configs into the full internal representation
so downstream code only deals with one shape (DD-12).

### Steps

1. **Create `internal/config/loader.go`**

   File path: `internal/config/loader.go`

   ```go
   package config

   import (
       "fmt"
       "os"
       "path/filepath"

       "gopkg.in/yaml.v3"
   )

   // Load reads the global config from configDir/config.yaml and optionally
   // merges a project-level .aide.yaml found in projectDir.
   //
   // configDir: typically ConfigDir() ($XDG_CONFIG_HOME/aide/)
   // projectDir: typically ProjectRoot(cwd) (git root or cwd)
   //
   // The returned Config is always in normalized (full) form. If the global
   // config was minimal, it is expanded into a single "default" context.
   func Load(configDir, projectDir string) (*Config, error) {
       globalPath := filepath.Join(configDir, "config.yaml")
       cfg, err := loadFile(globalPath)
       if err != nil {
           return nil, fmt.Errorf("loading global config: %w", err)
       }

       // Normalize minimal format into full format (DD-12)
       if cfg.IsMinimal() {
           cfg = normalizeMinimal(cfg)
       }

       // Merge project override if present
       projectPath := filepath.Join(projectDir, ProjectConfigFileName)
       if _, statErr := os.Stat(projectPath); statErr == nil {
           override, err := loadFile(projectPath)
           if err != nil {
               return nil, fmt.Errorf("loading project config %s: %w", projectPath, err)
           }
           cfg = mergeProjectOverride(cfg, override)
       }

       return cfg, nil
   }

   // loadFile reads and unmarshals a single YAML config file.
   func loadFile(path string) (*Config, error) {
       data, err := os.ReadFile(path)
       if err != nil {
           return nil, err
       }
       var cfg Config
       if err := yaml.Unmarshal(data, &cfg); err != nil {
           return nil, fmt.Errorf("parsing %s: %w", path, err)
       }
       return &cfg, nil
   }

   // normalizeMinimal converts a flat (minimal) config into the full
   // internal representation with a single "default" context.
   func normalizeMinimal(cfg *Config) *Config {
       agentName := cfg.Agent
       if agentName == "" {
           agentName = "default"
       }
       return &Config{
           Agents: map[string]AgentDef{
               agentName: {Binary: agentName},
           },
           Contexts: map[string]Context{
               "default": {
                   Agent:       agentName,
                   Env:         cfg.Env,
                   SecretsFile: cfg.SecretsFile,
                   MCPServers:  cfg.MCPServers,
               },
           },
           MCP:            cfg.MCP,
           DefaultContext: "default",
       }
   }

   // ProjectOverride holds per-project override data from .aide.yaml.
   // It is NOT stored as a context. Instead, the resolver merges it on top
   // of whichever global context matches (DD: "global defaults < context config
   // < project override").
   type ProjectOverride struct {
       Agent       string            `yaml:"agent,omitempty"`
       Env         map[string]string `yaml:"env,omitempty"`
       SecretsFile string            `yaml:"secrets_file,omitempty"`
       MCPServers  []string          `yaml:"mcp_servers,omitempty"`
       Sandbox     *SandboxPolicy    `yaml:"sandbox,omitempty"`
   }

   // mergeProjectOverride extracts the project override from a parsed .aide.yaml
   // and attaches it to the config. It does NOT create a "__project__" context.
   // The resolver (Task 5) is responsible for layering this on top of the matched
   // context at resolution time.
   func mergeProjectOverride(base *Config, override *Config) *Config {
       base.ProjectOverride = &ProjectOverride{
           Agent:       override.Agent,
           Env:         override.Env,
           SecretsFile: override.SecretsFile,
           MCPServers:  override.MCPServers,
       }
       return base
   }
   ```

2. **Create `internal/config/loader_test.go`**

   File path: `internal/config/loader_test.go`

   Tests use `t.TempDir()` to create throwaway directories with YAML files.

   | Test Name | Description |
   |-----------|-------------|
   | `TestLoad_MinimalConfig` | Write a minimal config.yaml to temp dir, call `Load()`, assert result has a "default" context with the correct agent and env |
   | `TestLoad_FullConfig` | Write a full config.yaml with agents + contexts, call `Load()`, assert contexts map is populated correctly |
   | `TestLoad_MinimalNormalization` | Write minimal config, load, assert `IsMinimal()` is false on the result (it was normalized) |
   | `TestLoad_ProjectOverride` | Write global config.yaml + .aide.yaml in project dir, assert `cfg.ProjectOverride` is non-nil with overridden agent |
   | `TestLoad_ProjectOverrideMergesEnv` | Global context has `env: {A: 1}`, project .aide.yaml has `env: {B: 2}`. Resolve the config. Assert the merged context env has BOTH `A: 1` (inherited from matched context) AND `B: 2` (from project override) |
   | `TestLoad_ProjectOverrideSecretsFile` | Global context has `secrets_file: global.enc.yaml`, project .aide.yaml has `secrets_file: project.enc.yaml`. Assert resolved context's `SecretsFile` is `"project.enc.yaml"` |
   | `TestLoad_NoProjectOverride` | No .aide.yaml in project dir, assert `cfg.ProjectOverride` is nil, no error |
   | `TestLoad_MissingGlobalConfig` | No config.yaml exists, assert error is returned |
   | `TestLoad_InvalidYAML` | Write broken YAML, assert error contains parsing info |
   | `TestLoad_MCPPreserved` | Write config with `mcp` section, assert `MCP.Servers` map is populated after load |
   | `TestNormalizeMinimal_AgentNameDefault` | Minimal config with no `agent` field, assert agent defaults to "default" |
   | `TestNormalizeMinimal_PreservesMCPServers` | Minimal config with `mcp_servers: [git]`, assert normalized context has `MCPServers: ["git"]` |

### Verification

```bash
go test ./internal/config/... -run TestLoad -v
go test ./internal/config/... -run TestNormalize -v
```

---

## Task 4: Git Remote Detection + Project Root

### Context

aide uses the git remote URL and the git repository root to match contexts
and to set `{{ .project_root }}` (DD-18). This task shells out to `git` for
remote detection and root finding. It also provides a URL normalizer so
`git@github.com:org/repo.git` and `https://github.com/org/repo` both yield
`github.com/org/repo`.

### Steps

1. **Create `internal/context/git.go`**

   File path: `internal/context/git.go`

   ```go
   package context

   import (
       "fmt"
       "os/exec"
       "path/filepath"
       "strings"
   )

   // DetectRemote runs `git remote get-url <remoteName>` in the given directory.
   // Returns the raw remote URL or empty string if not a git repo or no remote.
   // remoteName defaults to "origin" if empty.
   // This function never returns an error for "not a git repo" -- it returns "".
   func DetectRemote(dir string, remoteName string) string {
       if remoteName == "" {
           remoteName = "origin"
       }
       cmd := exec.Command("git", "remote", "get-url", remoteName)
       cmd.Dir = dir
       out, err := cmd.Output()
       if err != nil {
           return ""
       }
       return strings.TrimSpace(string(out))
   }

   // ParseRemoteHost normalizes a git remote URL into a canonical
   // "host/owner/repo" form. Strips protocol, user prefix, and .git suffix.
   //
   // Examples:
   //   "git@github.com:jskswamy/aide.git" -> "github.com/jskswamy/aide"
   //   "https://github.com/jskswamy/aide.git" -> "github.com/jskswamy/aide"
   //   "ssh://git@github.com/jskswamy/aide" -> "github.com/jskswamy/aide"
   //   "https://github.com/jskswamy/aide" -> "github.com/jskswamy/aide"
   //
   // Returns empty string for empty input.
   func ParseRemoteHost(rawURL string) string {
       if rawURL == "" {
           return ""
       }

       url := rawURL

       // Strip .git suffix
       url = strings.TrimSuffix(url, ".git")

       // Handle SSH shorthand: git@host:owner/repo
       if strings.Contains(url, "@") && strings.Contains(url, ":") && !strings.Contains(url, "://") {
           // git@github.com:owner/repo
           parts := strings.SplitN(url, "@", 2)
           hostAndPath := parts[1]
           // Replace first : with /
           hostAndPath = strings.Replace(hostAndPath, ":", "/", 1)
           return hostAndPath
       }

       // Handle protocol URLs: https://host/path, ssh://user@host/path
       for _, prefix := range []string{"https://", "http://", "ssh://", "git://"} {
           if strings.HasPrefix(url, prefix) {
               url = strings.TrimPrefix(url, prefix)
               // Strip user@ if present
               if atIdx := strings.Index(url, "@"); atIdx != -1 {
                   url = url[atIdx+1:]
               }
               return url
           }
       }

       return url
   }

   // ProjectRoot finds the git repository root by walking up from cwd.
   // Falls back to cwd if not inside a git repo (DD-18).
   func ProjectRoot(cwd string) string {
       cmd := exec.Command("git", "rev-parse", "--show-toplevel")
       cmd.Dir = cwd
       out, err := cmd.Output()
       if err != nil {
           // Not a git repo, fall back to cwd
           abs, absErr := filepath.Abs(cwd)
           if absErr != nil {
               return cwd
           }
           return abs
       }
       return strings.TrimSpace(string(out))
   }
   ```

2. **Create `internal/context/git_test.go`**

   File path: `internal/context/git_test.go`

   | Test Name | Description |
   |-----------|-------------|
   | `TestParseRemoteHost_SSHShorthand` | Input `"git@github.com:jskswamy/aide.git"`, expect `"github.com/jskswamy/aide"` |
   | `TestParseRemoteHost_HTTPS` | Input `"https://github.com/jskswamy/aide.git"`, expect `"github.com/jskswamy/aide"` |
   | `TestParseRemoteHost_HTTPSNoGit` | Input `"https://github.com/jskswamy/aide"`, expect `"github.com/jskswamy/aide"` |
   | `TestParseRemoteHost_SSHProtocol` | Input `"ssh://git@github.com/jskswamy/aide"`, expect `"github.com/jskswamy/aide"` |
   | `TestParseRemoteHost_Empty` | Input `""`, expect `""` |
   | `TestParseRemoteHost_GitProtocol` | Input `"git://github.com/org/repo.git"`, expect `"github.com/org/repo"` |
   | `TestDetectRemote_RealGitRepo` | Create a temp git repo with a remote, call `DetectRemote()`, assert URL matches |
   | `TestDetectRemote_NotAGitRepo` | Call `DetectRemote()` on `/tmp`, assert returns `""` (no error) |
   | `TestDetectRemote_NoRemote` | Create a temp git repo with no remotes, assert returns `""` |
   | `TestProjectRoot_GitRepo` | Create a temp git repo, call `ProjectRoot()` from a subdirectory, assert returns the repo root |
   | `TestProjectRoot_NotGitRepo` | Call `ProjectRoot()` on a plain temp dir, assert returns that dir (fallback) |

   **Setup helpers for git tests:** use `t.TempDir()` + `git init` + `git remote add`
   via `exec.Command` in test setup. Tag these tests with a build constraint or
   skip if `git` is not on PATH:

   ```go
   func skipIfNoGit(t *testing.T) {
       t.Helper()
       if _, err := exec.LookPath("git"); err != nil {
           t.Skip("git not found on PATH")
       }
   }
   ```

### Verification

```bash
go test ./internal/context/... -run TestParseRemoteHost -v
go test ./internal/context/... -run TestDetectRemote -v
go test ./internal/context/... -run TestProjectRoot -v
```

---

## Task 5: Context Matching Engine

### Context

Given a loaded `Config`, the current working directory, and the git remote URL,
the resolver must pick the single best-matching context. Specificity rules from
DESIGN.md:

1. Project `.aide.yaml` override is merged on top of the matched context (highest priority)
2. Exact path match > glob path match > remote match
3. Longer patterns are more specific (longer glob > shorter glob)
4. Falls back to `default_context` if nothing matches

Uses `github.com/gobwas/glob` for glob compilation and matching.

### Steps

1. **Create `internal/context/resolver.go`**

   File path: `internal/context/resolver.go`

   ```go
   package context

   import (
       "fmt"
       "os"
       "path/filepath"
       "strings"

       "github.com/gobwas/glob"
       "github.com/jskswamy/aide/internal/config"
   )

   // MatchResult describes why a context was selected.
   type MatchResult struct {
       ContextName string     // name of the matched context
       MatchedRule *config.MatchRule // the specific rule that matched (nil for project/default)
       Reason      string     // human-readable reason ("project override", "path glob", etc.)
       Specificity int        // higher = more specific
   }

   // Specificity tiers. Within a tier, longer pattern string = higher specificity.
   const (
       SpecificityDefault   = 0
       SpecificityRemote    = 100
       SpecificityPathGlob  = 200
       SpecificityPathExact = 300
       SpecificityProject   = 1000
   )

   // Resolve picks the best matching context from cfg for the given cwd and remoteURL.
   // If cfg.ProjectOverride is set, it is merged ON TOP of the matched context
   // (global defaults < context config < project override, per DD).
   //
   // cwd: absolute path to the current working directory
   // remoteURL: normalized remote (output of ParseRemoteHost), may be empty
   //
   // Returns the matched context name, the Context value, and a MatchResult for
   // diagnostic purposes. Returns an error only if no context can be resolved at all
   // (no matches and no default_context).
   func Resolve(cfg *config.Config, cwd string, remoteURL string) (string, config.Context, MatchResult, error) {
       // 1. Score all contexts to find the best match
       var best *MatchResult
       for name, ctx := range cfg.Contexts {
           for i := range ctx.Match {
               rule := &ctx.Match[i]
               score := scoreRule(rule, cwd, remoteURL)
               if score > 0 {
                   if best == nil || score > best.Specificity {
                       best = &MatchResult{
                           ContextName: name,
                           MatchedRule: rule,
                           Specificity: score,
                       }
                   }
               }
           }
       }

       var matchedName string
       var matchedCtx config.Context
       var result MatchResult

       if best != nil {
           best.Reason = describeMatch(best)
           matchedName = best.ContextName
           matchedCtx = cfg.Contexts[best.ContextName]
           result = *best
       } else if cfg.DefaultContext != "" {
           if ctx, ok := cfg.Contexts[cfg.DefaultContext]; ok {
               matchedName = cfg.DefaultContext
               matchedCtx = ctx
               result = MatchResult{
                   ContextName: cfg.DefaultContext,
                   Reason:      fmt.Sprintf("default_context (%s)", cfg.DefaultContext),
                   Specificity: SpecificityDefault,
               }
           }
       }

       if matchedName == "" {
           return "", config.Context{}, MatchResult{}, fmt.Errorf(
               "no context matched for cwd=%s remote=%s and no default_context configured",
               cwd, remoteURL,
           )
       }

       // 2. Apply project override on top of the matched context (merge, not replace).
       // Fields set in the override replace matched context fields; unset fields
       // keep the matched context's values.
       if po := cfg.ProjectOverride; po != nil {
           if po.Agent != "" {
               matchedCtx.Agent = po.Agent
           }
           if po.SecretsFile != "" {
               matchedCtx.SecretsFile = po.SecretsFile
           }
           if len(po.MCPServers) > 0 {
               matchedCtx.MCPServers = po.MCPServers
           }
           if po.Sandbox != nil {
               matchedCtx.Sandbox = po.Sandbox
           }
           // Env: project env merges on top of context env (project wins on conflict)
           if len(po.Env) > 0 {
               merged := make(map[string]string, len(matchedCtx.Env)+len(po.Env))
               for k, v := range matchedCtx.Env {
                   merged[k] = v
               }
               for k, v := range po.Env {
                   merged[k] = v
               }
               matchedCtx.Env = merged
           }
           result.Reason = fmt.Sprintf("project override on top of %s", result.Reason)
           result.Specificity = SpecificityProject
       }

       return matchedName, matchedCtx, result, nil
   }

   // scoreRule returns a specificity score for a single match rule, or 0 if it
   // does not match.
   func scoreRule(rule *config.MatchRule, cwd string, remoteURL string) int {
       if rule.Path != "" {
           return scorePathRule(rule.Path, cwd)
       }
       if rule.Remote != "" {
           return scoreRemoteRule(rule.Remote, remoteURL)
       }
       return 0
   }

   // scorePathRule scores a path match rule against cwd.
   // Expands ~ to home directory. Exact match gets SpecificityPathExact + len,
   // glob match gets SpecificityPathGlob + len.
   func scorePathRule(pattern string, cwd string) int {
       expanded := expandTilde(pattern)

       // Try exact match
       absPattern, _ := filepath.Abs(expanded)
       if absPattern == cwd {
           return SpecificityPathExact + len(pattern)
       }

       // Try glob match
       g, err := glob.Compile(expanded, filepath.Separator)
       if err != nil {
           return 0
       }
       if g.Match(cwd) {
           return SpecificityPathGlob + len(pattern)
       }
       return 0
   }

   // scoreRemoteRule scores a remote match rule against a normalized remote URL.
   func scoreRemoteRule(pattern string, remoteURL string) int {
       if remoteURL == "" {
           return 0
       }

       // Exact match
       if pattern == remoteURL {
           return SpecificityRemote + len(pattern) + 50 // bonus for exact
       }

       // Glob match
       g, err := glob.Compile(pattern)
       if err != nil {
           return 0
       }
       if g.Match(remoteURL) {
           return SpecificityRemote + len(pattern)
       }
       return 0
   }

   // expandTilde replaces a leading ~ with the user's home directory.
   func expandTilde(path string) string {
       if !strings.HasPrefix(path, "~") {
           return path
       }
       home, err := os.UserHomeDir()
       if err != nil {
           return path
       }
       return filepath.Join(home, path[1:])
   }

   // describeMatch produces a human-readable description of why a rule matched.
   func describeMatch(m *MatchResult) string {
       if m.MatchedRule == nil {
           return "default"
       }
       if m.MatchedRule.Path != "" {
           if m.Specificity >= SpecificityPathExact {
               return fmt.Sprintf("exact path match: %s", m.MatchedRule.Path)
           }
           return fmt.Sprintf("path glob match: %s", m.MatchedRule.Path)
       }
       if m.MatchedRule.Remote != "" {
           return fmt.Sprintf("remote match: %s", m.MatchedRule.Remote)
       }
       return "unknown"
   }
   ```

   **Note:** The `"os"` import is needed for `expandTilde` (included in the import block above).

2. **Create `internal/context/resolver_test.go`**

   File path: `internal/context/resolver_test.go`

   | Test Name | Description |
   |-----------|-------------|
   | `TestResolve_ProjectOverrideMerged` | Config has `ProjectOverride` with agent + env overrides, plus a matching global context with its own env. Assert: resolved context has the override's agent, env contains both global and override keys (override wins on conflict), and specificity is `SpecificityProject` |
   | `TestResolve_ExactPathBeatsGlob` | Two contexts: one matches cwd via exact path, one via glob; assert exact path wins |
   | `TestResolve_PathGlobBeatsRemote` | Context A matches via path glob, Context B via remote; assert A wins |
   | `TestResolve_LongerGlobWins` | Context A: `~/work/*`, Context B: `~/work/org/*`; cwd matches both; assert B wins (longer pattern) |
   | `TestResolve_RemoteMatch` | No path rules, one remote rule `"github.com/org/*"` matches remote `"github.com/org/repo"`; assert match |
   | `TestResolve_RemoteExactBeatsGlob` | Remote rule `"github.com/org/repo"` (exact) vs `"github.com/org/*"` (glob); assert exact wins |
   | `TestResolve_FallbackToDefault` | No rules match, `default_context` set; assert default context returned |
   | `TestResolve_NoMatchNoDefault` | No rules match, no `default_context`; assert error returned |
   | `TestResolve_EmptyRemoteGraceful` | Remote is `""`, path rules exist; assert path matching still works, remote rules skipped |
   | `TestResolve_TildeExpansion` | Path rule `~/projects/*`, cwd is `$HOME/projects/foo`; assert it matches |
   | `TestResolve_MultipleRulesOnContext` | Context has two match rules (one path, one remote); cwd matches path; assert correct specificity |

3. **Add glob dependency**

   ```bash
   go get github.com/gobwas/glob
   ```

### Verification

```bash
go test ./internal/context/... -run TestResolve -v
go test ./internal/... -v  # run all epic 1 tests together
go vet ./...
```

---

## Integration Check (All Tasks)

After all five tasks are complete, run the full verification:

```bash
cd /Users/subramk/source/github.com/jskswamy/aide

# All tests pass
go test ./internal/... -v

# No vet issues
go vet ./...

# Build succeeds (no main yet, just library packages)
go build ./internal/config/...
go build ./internal/context/...

# Module is tidy
go mod tidy
git diff go.mod go.sum  # should show only expected dependencies
```

### Files Created in This Epic

```
go.mod
go.sum
internal/config/schema.go
internal/config/schema_test.go
internal/config/paths.go
internal/config/paths_test.go
internal/config/loader.go
internal/config/loader_test.go
internal/context/git.go
internal/context/git_test.go
internal/context/resolver.go
internal/context/resolver_test.go
```

### Design Decision References

| Decision | Where Used |
|----------|-----------|
| DD-3 (adrg/xdg) | Task 2: `paths.go` uses `xdg.ConfigHome` and `xdg.RuntimeDir` |
| DD-5 (env on context, not agent) | Task 1: `AgentDef` has no `Env` field; `Context` has `Env`, `SecretsFile` |
| DD-6 (single XDG dir) | Task 2: `ConfigDir()` and `SecretsDir()` both under `$XDG_CONFIG_HOME/aide/` |
| DD-12 (minimal format) | Task 1: `IsMinimal()` method; Task 3: `normalizeMinimal()` in loader |
| DD-18 (project root = git root) | Task 4: `ProjectRoot()` calls `git rev-parse --show-toplevel`, falls back to cwd |
