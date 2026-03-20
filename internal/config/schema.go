package config

import "fmt"

// Config is the top-level configuration, supporting both minimal and full formats.
// When the YAML contains "agents" or "contexts" keys, it is the full (structured)
// format. Otherwise it is the minimal (flat) format treated as a single default context.
type Config struct {
	// --- Full format fields ---
	Agents         map[string]AgentDef        `yaml:"agents,omitempty"`
	MCP            *MCPConfig                 `yaml:"mcp,omitempty"`
	Contexts       map[string]Context         `yaml:"contexts,omitempty"`
	DefaultContext string                     `yaml:"default_context,omitempty"`
	Sandboxes      map[string]SandboxPolicy   `yaml:"sandboxes,omitempty"`

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
	Sandbox            *SandboxRef          `yaml:"sandbox,omitempty"`
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

// NetworkPolicy defines the network access policy for a sandboxed agent.
// It supports both a simple string form (e.g. "outbound") and a structured
// form with port filtering (DD-19).
type NetworkPolicy struct {
	Mode       string `yaml:"mode,omitempty"`
	AllowPorts []int  `yaml:"allow_ports,omitempty"`
	DenyPorts  []int  `yaml:"deny_ports,omitempty"`
}

// UnmarshalYAML handles both `network: outbound` (string) and
// `network: {mode: outbound, allow_ports: [443]}` (map) forms.
func (n *NetworkPolicy) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var s string
	if err := unmarshal(&s); err == nil {
		n.Mode = s
		return nil
	}
	type alias NetworkPolicy
	return unmarshal((*alias)(n))
}

// SandboxPolicy defines the OS-native sandbox constraints for an agent.
// A nil pointer means "use defaults". The bool variant (sandbox: false)
// is handled during YAML unmarshalling by setting Disabled = true.
type SandboxPolicy struct {
	// Disabled is true when the user writes `sandbox: false`.
	Disabled bool `yaml:"-"`

	Writable        []string       `yaml:"writable,omitempty"`
	Readable        []string       `yaml:"readable,omitempty"`
	Denied          []string       `yaml:"denied,omitempty"`
	WritableExtra   []string       `yaml:"writable_extra,omitempty"`
	ReadableExtra   []string       `yaml:"readable_extra,omitempty"`
	DeniedExtra     []string       `yaml:"denied_extra,omitempty"`
	Network         *NetworkPolicy `yaml:"network,omitempty"`
	AllowSubprocess *bool          `yaml:"allow_subprocess,omitempty"`
	CleanEnv        *bool          `yaml:"clean_env,omitempty"`
}

// UnmarshalYAML handles both `sandbox: false` (bool) and `sandbox: { ... }` (map).
func (s *SandboxPolicy) UnmarshalYAML(unmarshal func(interface{}) error) error {
	// Try bool first
	var b bool
	if err := unmarshal(&b); err == nil {
		if !b {
			s.Disabled = true
			return nil
		}
		return fmt.Errorf("sandbox: expected false or a mapping, got true")
	}

	// Otherwise decode as struct (use alias to avoid recursion)
	type alias SandboxPolicy
	return unmarshal((*alias)(s))
}

// SandboxRef references a sandbox configuration. A context uses this to
// point to either a named profile (from Config.Sandboxes), an inline policy,
// or to disable sandboxing entirely.
type SandboxRef struct {
	// Disabled is true when the user writes `sandbox: false`.
	Disabled bool `yaml:"-"`

	// ProfileName references a named profile from Config.Sandboxes.
	// Special values: "default" uses DefaultPolicy, "none" disables sandbox.
	ProfileName string `yaml:"profile,omitempty"`

	// Inline is an inline sandbox policy definition.
	Inline *SandboxPolicy `yaml:"inline,omitempty"`
}

// UnmarshalYAML handles multiple forms:
//   - `sandbox: false` (bool) — disables sandbox
//   - `sandbox: "profile-name"` (string) — references a named profile
//   - `sandbox: { profile: "name" }` — references a named profile via mapping
//   - `sandbox: { writable: [...], network: ... }` — inline policy (SandboxPolicy fields)
func (s *SandboxRef) UnmarshalYAML(unmarshal func(interface{}) error) error {
	// Try bool first
	var b bool
	if err := unmarshal(&b); err == nil {
		if !b {
			s.Disabled = true
			return nil
		}
		return fmt.Errorf("sandbox: expected false, a string, or a mapping, got true")
	}

	// Try string (profile name)
	var str string
	if err := unmarshal(&str); err == nil {
		s.ProfileName = str
		return nil
	}

	// Try as SandboxRef struct first (has "profile" or "inline" keys)
	type alias SandboxRef
	var ref alias
	if err := unmarshal(&ref); err == nil && (ref.ProfileName != "" || ref.Inline != nil) {
		*s = SandboxRef(ref)
		return nil
	}

	// Fall back to treating the entire mapping as an inline SandboxPolicy.
	// This handles the common case: sandbox: { writable: [...], network: outbound }
	var sp SandboxPolicy
	if err := unmarshal(&sp); err != nil {
		return fmt.Errorf("sandbox: cannot decode as ref or inline policy: %w", err)
	}
	s.Inline = &sp
	return nil
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
