package config

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config is the top-level configuration, supporting both minimal and full formats.
// When the YAML contains "agents" or "contexts" keys, it is the full (structured)
// format. Otherwise it is the minimal (flat) format treated as a single default context.
type Config struct {
	// --- Full format fields ---
	Agents         map[string]AgentDef        `yaml:"agents,omitempty"`
	Contexts       map[string]Context         `yaml:"contexts,omitempty"`
	DefaultContext string                     `yaml:"default_context,omitempty"`
	Sandboxes      map[string]SandboxPolicy   `yaml:"sandboxes,omitempty"`
	CustomGuards   map[string]CustomGuard     `yaml:"custom_guards,omitempty"`
	GuardTypes     map[string]GuardType       `yaml:"guard_types,omitempty"`
	Capabilities  map[string]CapabilityDef `yaml:"capabilities,omitempty"`
	NeverAllow    []string                 `yaml:"never_allow,omitempty"`
	NeverAllowEnv []string                 `yaml:"never_allow_env,omitempty"`
	Plugins       PluginMap                `yaml:"plugins,omitempty"`
	// MCPServers is the v2 top-level mcp_servers map. The custom
	// MCPServerMap unmarshaller also accepts the legacy minimal-format
	// list-of-names form; those names are stored with empty MCPServer
	// values so normalizeMinimal can still propagate them as a
	// MCPServersList on the synthesised default context.
	MCPServers  MCPServerMap `yaml:"mcp_servers,omitempty"`
	Preferences *Preferences `yaml:"preferences,omitempty"`

	// --- Minimal (flat) format fields ---
	// These are promoted to a synthetic "default" context during loading.
	Agent       string            `yaml:"agent,omitempty"`
	Env         map[string]string `yaml:"env,omitempty"`
	Secret      string            `yaml:"secret,omitempty"`
	Sandbox     *SandboxPolicy    `yaml:"sandbox,omitempty"`
	Yolo        *bool             `yaml:"yolo,omitempty"`

	// --- Project override (populated by loader, not from YAML) ---
	// Holds .aide.yaml data to be merged on top of the matched context at
	// resolution time. Not serialized to YAML.
	ProjectOverride     *ProjectOverride `yaml:"-"`
	ProjectConfigPath   string           `yaml:"-"` // absolute path to .aide.yaml, if found
}

// IsMinimal returns true when the config uses the flat single-context format.
// Detection: if neither "agents" nor "contexts" maps are populated, it is minimal. (DD-12)
func (c *Config) IsMinimal() bool {
	return len(c.Agents) == 0 && len(c.Contexts) == 0
}

// CapabilityDef defines a reusable capability that grants filesystem and
// environment access. Capabilities can extend a single parent or combine
// multiple peers, and declare readable/writable/deny path lists plus
// environment variable allow-lists.
type CapabilityDef struct {
	Extends     string   `yaml:"extends,omitempty"`
	Combines    []string `yaml:"combines,omitempty"`
	Description string   `yaml:"description,omitempty"`
	Readable    []string `yaml:"readable,omitempty"`
	Writable    []string `yaml:"writable,omitempty"`
	Deny        []string `yaml:"deny,omitempty"`
	EnvAllow    []string `yaml:"env_allow,omitempty"`
	Allow       []string `yaml:"allow,omitempty"`
	// Ports is a capability-specific port list. Currently consumed only by
	// the ssh capability (capabilities.ssh.ports: [22, 2222]) where it is
	// unioned with auto-detected channels. Other capabilities ignore it.
	Ports []int `yaml:"ports,omitempty"`
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
	Secret             string               `yaml:"secret,omitempty"`
	Env                map[string]string    `yaml:"env,omitempty"`
	// MCPServers retains the v1 list-of-names form for the per-context
	// selection of which MCP servers (declared at the top level) are
	// enabled in this context. The v2 ContextOverride[MCPServer]
	// delta form is captured by MCPServersOverride; one of the two is
	// non-empty depending on how the user wrote the YAML.
	MCPServers         []string                    `yaml:"-"`
	MCPServersOverride *ContextOverride[MCPServer] `yaml:"-"`
	Sandbox            *SandboxRef          `yaml:"sandbox,omitempty"`
	Yolo               *bool                `yaml:"yolo,omitempty"`
	Capabilities       []string             `yaml:"capabilities,omitempty"`
	// Plugins is populated by UnmarshalYAML when the per-context
	// `plugins:` value is a mapping (v2 delta form). The yaml tag is
	// "-" because UnmarshalYAML handles it directly.
	Plugins *ContextOverride[PluginEntry] `yaml:"-"`
}

// MarshalYAML emits the Context with mcp_servers/plugins serialized
// from whichever shape is populated (legacy list, v2 override, etc.).
func (c Context) MarshalYAML() (interface{}, error) {
	out := map[string]interface{}{}
	if len(c.Match) > 0 {
		out["match"] = c.Match
	}
	if c.Agent != "" {
		out["agent"] = c.Agent
	}
	if c.Secret != "" {
		out["secret"] = c.Secret
	}
	if len(c.Env) > 0 {
		out["env"] = c.Env
	}
	if c.Sandbox != nil {
		out["sandbox"] = c.Sandbox
	}
	if c.Yolo != nil {
		out["yolo"] = c.Yolo
	}
	if len(c.Capabilities) > 0 {
		out["capabilities"] = c.Capabilities
	}
	if len(c.MCPServers) > 0 {
		out["mcp_servers"] = c.MCPServers
	} else if c.MCPServersOverride != nil {
		out["mcp_servers"] = c.MCPServersOverride
	}
	if c.Plugins != nil {
		out["plugins"] = c.Plugins
	}
	return out, nil
}

// UnmarshalYAML decodes a Context, routing the polymorphic
// `mcp_servers:` field into either MCPServers (legacy list-of-names)
// or MCPServersOverride (v2 delta block). Same for `plugins:` — the
// generic ContextOverride[PluginEntry] handles the v2 mapping form;
// the legacy `[]string` selection list is captured in PluginsList.
func (c *Context) UnmarshalYAML(node *yaml.Node) error {
	type rawCtx struct {
		Match        []MatchRule       `yaml:"match,omitempty"`
		Agent        string            `yaml:"agent"`
		Secret       string            `yaml:"secret,omitempty"`
		Env          map[string]string `yaml:"env,omitempty"`
		Sandbox      *SandboxRef       `yaml:"sandbox,omitempty"`
		Yolo         *bool             `yaml:"yolo,omitempty"`
		Capabilities []string          `yaml:"capabilities,omitempty"`
		MCPServers   yaml.Node         `yaml:"mcp_servers,omitempty"`
		Plugins      yaml.Node         `yaml:"plugins,omitempty"`
	}
	var r rawCtx
	if err := node.Decode(&r); err != nil {
		return err
	}
	c.Match = r.Match
	c.Agent = r.Agent
	c.Secret = r.Secret
	c.Env = r.Env
	c.Sandbox = r.Sandbox
	c.Yolo = r.Yolo
	c.Capabilities = r.Capabilities

	switch r.MCPServers.Kind {
	case 0:
		// absent
	case yaml.SequenceNode:
		if err := r.MCPServers.Decode(&c.MCPServers); err != nil {
			return fmt.Errorf("context.mcp_servers: %w", err)
		}
	case yaml.MappingNode:
		var ov ContextOverride[MCPServer]
		if err := r.MCPServers.Decode(&ov); err != nil {
			return fmt.Errorf("context.mcp_servers: %w", err)
		}
		c.MCPServersOverride = &ov
	default:
		return fmt.Errorf("context.mcp_servers: unsupported YAML kind %v", r.MCPServers.Kind)
	}

	switch r.Plugins.Kind {
	case 0:
		// absent
	case yaml.MappingNode:
		var ov ContextOverride[PluginEntry]
		if err := r.Plugins.Decode(&ov); err != nil {
			return fmt.Errorf("context.plugins: %w", err)
		}
		c.Plugins = &ov
	case yaml.SequenceNode:
		// Legacy: list-of-names selection. v2 stub — store in Capabilities-like
		// slice? We currently don't expose this on Context; tolerate by
		// dropping the list form silently. ResolveDesired in Task 9
		// reconstructs the desired set from the polymorphic top-level.
	default:
		return fmt.Errorf("context.plugins: unsupported YAML kind %v", r.Plugins.Kind)
	}
	return nil
}

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

// MatchRule is a single rule in a context's match list.
// Exactly one of Remote or Path should be set per rule.
type MatchRule struct {
	Remote     string `yaml:"remote,omitempty"`
	Path       string `yaml:"path,omitempty"`
	RemoteName string `yaml:"remote_name,omitempty"` // defaults to "origin"
}

// MCPServerMap is the polymorphic top-level mcp_servers map. It
// decodes both the v2 mapping form and the legacy minimal-format
// list-of-names form (the latter is stashed on Config.MCPServerMinimal
// by the parent decoder so existing normalizeMinimal callers keep
// working until that path is rewritten).
type MCPServerMap map[string]MCPServer

// UnmarshalYAML decodes either a mapping or a sequence. Sequence form
// returns a sentinel map with a single empty-key entry whose source
// node is the original sequence; Config.UnmarshalYAML re-routes that
// into MCPServerMinimal. The simpler approach: detect sequence at the
// Config level. (Implemented in Config.UnmarshalYAML below.)
func (m *MCPServerMap) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.MappingNode:
		out := MCPServerMap{}
		for i := 0; i+1 < len(node.Content); i += 2 {
			key := node.Content[i].Value
			var srv MCPServer
			if err := node.Content[i+1].Decode(&srv); err != nil {
				return fmt.Errorf("mcp_servers[%q]: %w", key, err)
			}
			out[key] = srv
		}
		*m = out
		return nil
	case yaml.SequenceNode:
		// Minimal-format list of names; values must be stored
		// elsewhere. Decode names into a placeholder map with empty
		// MCPServer values so callers can re-route via the keys.
		out := MCPServerMap{}
		for _, c := range node.Content {
			out[c.Value] = MCPServer{}
		}
		*m = out
		return nil
	default:
		return fmt.Errorf("mcp_servers: expected mapping or sequence, got %v", node.Kind)
	}
}

// MCPServer defines a single MCP server.
type MCPServer struct {
	Command string            `yaml:"command,omitempty"`
	URL     string            `yaml:"url,omitempty"`
	Args    []string          `yaml:"args,omitempty"`
	Env     map[string]string `yaml:"env,omitempty"`
}

// PluginShape enumerates the value-shape categories for entries in the
// top-level `plugins:` block. Each entry has exactly one shape, picked
// at YAML-decode time from the value's type.
type PluginShape int

const (
	// PluginShapeMarketplace indicates a value that is a YAML sequence
	// of plugin names; the key is interpreted as a repo path
	// (owner/repo or full URL).
	PluginShapeMarketplace PluginShape = iota
	// PluginShapeURLDirect indicates a value that is a string install
	// reference; the key is a plugin name chosen by the user for
	// readability.
	PluginShapeURLDirect
	// PluginShapeDeclareOnly indicates a null value. The key is a
	// repo path for marketplace agents; signals "ensure marketplace
	// cached, install nothing from it."
	PluginShapeDeclareOnly
)

// String returns the lowercase shape name used in validation messages.
func (s PluginShape) String() string {
	switch s {
	case PluginShapeMarketplace:
		return "marketplace"
	case PluginShapeURLDirect:
		return "url-direct"
	case PluginShapeDeclareOnly:
		return "declare-only"
	default:
		return "unknown"
	}
}

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

// PluginEntryURLDirect constructs a URL-direct PluginEntry from the
// given install reference. Convenience for callers (e.g. adopt) that
// need to synthesise entries outside the YAML decode path.
func PluginEntryURLDirect(source string) PluginEntry {
	return PluginEntry{shape: PluginShapeURLDirect, Source: source}
}

// PluginEntryMarketplace constructs a marketplace PluginEntry from
// the given plugin list.
func PluginEntryMarketplace(plugins []string) PluginEntry {
	return PluginEntry{shape: PluginShapeMarketplace, Plugins: plugins}
}

// PluginEntryDeclareOnly constructs a declare-only PluginEntry.
func PluginEntryDeclareOnly() PluginEntry {
	return PluginEntry{shape: PluginShapeDeclareOnly}
}

// MarshalYAML emits a PluginEntry in the v2 polymorphic shape that
// round-trips through UnmarshalYAML. List for Marketplace, string for
// URL-direct, null for declare-only.
func (p PluginEntry) MarshalYAML() (interface{}, error) {
	switch p.shape {
	case PluginShapeMarketplace:
		return p.Plugins, nil
	case PluginShapeURLDirect:
		return p.Source, nil
	case PluginShapeDeclareOnly:
		return nil, nil
	}
	return nil, nil
}

// PluginMap is the polymorphic top-level plugins map. It exists as a
// named type so we can implement UnmarshalYAML at map level — yaml.v3
// skips a value-level UnmarshalYAML when the YAML value is null, so
// shape detection for `key: ~` (declare-only) must happen here.
type PluginMap map[string]PluginEntry

// UnmarshalYAML decodes a mapping node into the polymorphic plugin map.
// Iterates the mapping pairs explicitly so null-valued entries are
// detected (yaml.v3 skips per-value UnmarshalYAML on null values).
func (m *PluginMap) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("plugins: expected a mapping, got %v", node.Kind)
	}
	out := PluginMap{}
	for i := 0; i+1 < len(node.Content); i += 2 {
		keyNode := node.Content[i]
		valNode := node.Content[i+1]
		var entry PluginEntry
		if valNode.Kind == yaml.ScalarNode && (valNode.Tag == "!!null" || valNode.Value == "" || valNode.Value == "~") {
			entry.shape = PluginShapeDeclareOnly
		} else {
			if err := valNode.Decode(&entry); err != nil {
				return fmt.Errorf("plugins[%q]: %w", keyNode.Value, err)
			}
		}
		out[keyNode.Value] = entry
	}
	*m = out
	return nil
}

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
		paths := append(append([]string{}, ctx.Plugins.Exclude...), ctx.Plugins.Only...)
		for _, path := range paths {
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
	if strings.HasPrefix(key, "github:") || strings.HasPrefix(key, "git:") || strings.HasPrefix(key, "https://") || strings.HasPrefix(key, "http://") {
		return true
	}
	return strings.Count(key, "/") >= 1
}

func mergeKeysForValidation(top PluginMap, extras map[string]PluginEntry) map[string]struct{} {
	out := make(map[string]struct{}, len(top)+len(extras))
	for k := range top {
		out[k] = struct{}{}
	}
	for k := range extras {
		out[k] = struct{}{}
	}
	return out
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

// SandboxOverrides carries external overrides (typically from resolved
// capabilities) to merge into a SandboxPolicy before policy evaluation.
type SandboxOverrides struct {
	Unguard       []string
	ReadableExtra []string
	WritableExtra []string
	DeniedExtra   []string
	EnvAllow      []string
	EnableGuard   []string
	Allow         []string
	NetworkMode   string // "unrestricted" or "" (no override)
	SSHPorts      []int  // ports for the ssh guard, from capabilities.ssh.ports
}

// (SandboxPolicy continues below.)

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

	// Guard configuration
	Guards      []string `yaml:"guards,omitempty"`
	GuardsExtra []string `yaml:"guards_extra,omitempty"`
	Unguard     []string `yaml:"unguard,omitempty"`
	Allow       []string `yaml:"allow,omitempty"`

	// SSHPorts is the resolved port list for the ssh guard, populated from
	// capabilities.ssh.ports (and unioned with auto-detected channels at
	// guard time). Not directly settable in sandbox YAML.
	SSHPorts []int `yaml:"-"`
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

// MarshalYAML flattens the SandboxRef for clean YAML output.
// Inline policies are serialized directly (no "inline:" wrapper).
func (r SandboxRef) MarshalYAML() (interface{}, error) {
	if r.Disabled {
		return false, nil
	}
	if r.ProfileName != "" {
		return map[string]string{"profile": r.ProfileName}, nil
	}
	if r.Inline != nil {
		return r.Inline, nil
	}
	return nil, nil
}

// UnmarshalYAML handles multiple forms:
//   - `sandbox: false` (bool) — disables sandbox
//   - `sandbox: "profile-name"` (string) — references a named profile
//   - `sandbox: { profile: "name" }` — references a named profile via mapping
//   - `sandbox: { writable: [...], network: ... }` — inline policy (SandboxPolicy fields)
func (r *SandboxRef) UnmarshalYAML(unmarshal func(interface{}) error) error {
	// Try bool first
	var b bool
	if err := unmarshal(&b); err == nil {
		if !b {
			r.Disabled = true
			return nil
		}
		return fmt.Errorf("sandbox: expected false, a string, or a mapping, got true")
	}

	// Try string (profile name)
	var str string
	if err := unmarshal(&str); err == nil {
		r.ProfileName = str
		return nil
	}

	// Try as SandboxRef struct first (has "profile" or "inline" keys)
	type alias SandboxRef
	var ref alias
	if err := unmarshal(&ref); err == nil && (ref.ProfileName != "" || ref.Inline != nil) {
		*r = SandboxRef(ref)
		return nil
	}

	// Fall back to treating the entire mapping as an inline SandboxPolicy.
	// This handles the common case: sandbox: { writable: [...], network: outbound }
	var sp SandboxPolicy
	if err := unmarshal(&sp); err != nil {
		return fmt.Errorf("sandbox: cannot decode as ref or inline policy: %w", err)
	}
	r.Inline = &sp
	return nil
}

// CustomGuard defines a user-specified guard in the config file.
type CustomGuard struct {
	Type        string   `yaml:"type,omitempty"`
	Description string   `yaml:"description,omitempty"`
	Paths       []string `yaml:"paths"`
	EnvOverride string   `yaml:"env_override,omitempty"`
	Allowed     []string `yaml:"allowed,omitempty"`
}

// GuardType defines a custom guard type with behavior and description.
type GuardType struct {
	Behavior    string `yaml:"behavior"`
	Description string `yaml:"description"`
}

// ProjectOverride holds per-project override data from .aide.yaml.
// It is NOT stored as a context. Instead, the resolver merges it on top
// of whichever global context matches.
type ProjectOverride struct {
	Agent       string            `yaml:"agent,omitempty"`
	Env         map[string]string `yaml:"env,omitempty"`
	Secret      string            `yaml:"secret,omitempty"`
	MCPServers  []string          `yaml:"mcp_servers,omitempty"`
	Sandbox     *SandboxPolicy    `yaml:"sandbox,omitempty"`
	Preferences  *Preferences      `yaml:"preferences,omitempty"`
	Yolo         *bool             `yaml:"yolo,omitempty"`
	Capabilities []string          `yaml:"capabilities,omitempty"`
	DisabledCapabilities []string  `yaml:"disabled_capabilities,omitempty"`
	// CapabilityVariants pins specific variants for named capabilities,
	// bypassing the detection/consent flow. Keys must appear in
	// Capabilities (or be activated via --with). Values are variant
	// names declared on the built-in or user-defined capability.
	CapabilityVariants map[string][]string `yaml:"capability_variants,omitempty"`
}

// Preferences holds global display/behavior settings.
type Preferences struct {
	ShowInfo   *bool  `yaml:"show_info,omitempty"`
	InfoStyle  string `yaml:"info_style,omitempty"`
	InfoDetail string `yaml:"info_detail,omitempty"`
	Yolo       *bool  `yaml:"yolo,omitempty"`
}

// ResolvePreferences merges global and project preferences,
// applying defaults for unset fields.
func ResolvePreferences(global, project *Preferences) Preferences {
	t := true
	result := Preferences{
		ShowInfo:   &t,
		InfoStyle:  "compact",
		InfoDetail: "normal",
	}
	if global != nil {
		if global.ShowInfo != nil {
			result.ShowInfo = global.ShowInfo
		}
		if global.InfoStyle != "" {
			result.InfoStyle = global.InfoStyle
		}
		if global.InfoDetail != "" {
			result.InfoDetail = global.InfoDetail
		}
	}
	if project != nil {
		if project.ShowInfo != nil {
			result.ShowInfo = project.ShowInfo
		}
		if project.InfoStyle != "" {
			result.InfoStyle = project.InfoStyle
		}
		if project.InfoDetail != "" {
			result.InfoDetail = project.InfoDetail
		}
	}
	return result
}

// ResolveYolo resolves the effective yolo setting from three layers:
// preferences (global default), context-level, and project override.
// Later layers override earlier ones. If all are nil, returns false.
func ResolveYolo(preferences, context, project *bool) bool {
	result := false
	if preferences != nil {
		result = *preferences
	}
	if context != nil {
		result = *context
	}
	if project != nil {
		result = *project
	}
	return result
}
