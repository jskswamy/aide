// Package explain builds a reality-aware, read-only description of how to
// configure aide: embedded recipes, the user's current config, and (later)
// the command schema. It never resolves or decrypts secrets.
package explain

// ConfigState is a redacted, read-only snapshot of the user's actual config.
type ConfigState struct {
	Loaded          bool           `json:"loaded"`
	DefaultContext  string         `json:"default_context,omitempty"`
	Contexts        []ContextState `json:"contexts,omitempty"`
	TopLevelMCP     []MCPState     `json:"top_level_mcp,omitempty"`
	TopLevelHooks   []string       `json:"top_level_hooks,omitempty"`
}

// ContextState describes one context with secret values redacted.
type ContextState struct {
	Name         string   `json:"name"`
	Agent        string   `json:"agent,omitempty"`
	Profile      string   `json:"profile,omitempty"`
	Secret       string   `json:"secret,omitempty"` // store name, e.g. "firmus"
	Matches      []string `json:"matches,omitempty"`
	Capabilities []string `json:"capabilities,omitempty"`
	Env          []EnvRef `json:"env,omitempty"`
	MCPServers   []string `json:"mcp_servers,omitempty"`
	SandboxNote  string   `json:"sandbox_note,omitempty"`
	Hooks        []string `json:"hooks,omitempty"`
}

// EnvRef is a redacted environment entry. At most one of SecretRef / Redacted
// / Template is meaningful: a {{ .secrets.X }} value yields SecretRef=X; a true
// literal yields Redacted=true and its value is never echoed (T1); a non-secret
// template (e.g. {{ .project_root }}/x) is safe to show and is carried verbatim
// in Template. Empty values yield none of the three.
type EnvRef struct {
	Key       string `json:"key"`
	SecretRef string `json:"secret_ref,omitempty"`
	Redacted  bool   `json:"redacted,omitempty"`
	Template  string `json:"template,omitempty"`
}

// MCPState describes a declared MCP server with redacted env.
type MCPState struct {
	Name      string   `json:"name"`
	Transport string   `json:"transport"` // "stdio" or "http"
	Env       []EnvRef `json:"env,omitempty"`
}

// Document is the full explain payload: current state plus narrative recipes.
// Renderers consume it.
type Document struct {
	State   ConfigState `json:"state"`
	Recipes []Recipe    `json:"recipes,omitempty"`
}

// Recipe is one task-oriented narrative how-to. Body is embedded markdown.
type Recipe struct {
	Topic string `json:"topic"`
	Title string `json:"title"`
	Body  string `json:"body"`
}
