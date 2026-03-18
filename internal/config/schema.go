package config

// Config is the top-level configuration, supporting both minimal and full formats.
// When the YAML contains "agents" or "contexts" keys, it is the full (structured)
// format. Otherwise it is the minimal (flat) format treated as a single default context.
type Config struct {
	// --- Full format fields ---
	Agents         map[string]AgentDef `yaml:"agents,omitempty"`
	MCP            *MCPConfig          `yaml:"mcp,omitempty"`
	Contexts       map[string]Context  `yaml:"contexts,omitempty"`
	DefaultContext string              `yaml:"default_context,omitempty"`

	// --- Minimal (flat) format fields ---
	// These are promoted to a synthetic "default" context during loading.
	Agent       string            `yaml:"agent,omitempty"`
	Env         map[string]string `yaml:"env,omitempty"`
	SecretsFile string            `yaml:"secrets_file,omitempty"`
	MCPServers  []string          `yaml:"mcp_servers,omitempty"`
	Sandbox     *SandboxPolicy    `yaml:"sandbox,omitempty"`

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
	Match              []MatchRule          `yaml:"match,omitempty"`
	Agent              string               `yaml:"agent"`
	SecretsFile        string               `yaml:"secrets_file,omitempty"`
	Env                map[string]string    `yaml:"env,omitempty"`
	MCPServers         []string             `yaml:"mcp_servers,omitempty"`
	MCPServerOverrides map[string]MCPServer `yaml:"mcp_server_overrides,omitempty"`
	Sandbox            *SandboxPolicy       `yaml:"sandbox,omitempty"`
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
	Servers    map[string]MCPServer `yaml:"servers,omitempty"`
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
	Network         string   `yaml:"network,omitempty"`           // "outbound" | "none" | "unrestricted"
	AllowSubprocess *bool    `yaml:"allow_subprocess,omitempty"`
	CleanEnv        *bool    `yaml:"clean_env,omitempty"`
}

// ProjectOverride holds per-project override data from .aide.yaml.
// It is NOT stored as a context. Instead, the resolver merges it on top
// of whichever global context matches.
type ProjectOverride struct {
	Agent       string            `yaml:"agent,omitempty"`
	Env         map[string]string `yaml:"env,omitempty"`
	SecretsFile string            `yaml:"secrets_file,omitempty"`
	MCPServers  []string          `yaml:"mcp_servers,omitempty"`
	Sandbox     *SandboxPolicy    `yaml:"sandbox,omitempty"`
}
