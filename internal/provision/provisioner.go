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
// locate the agent's MCP config file. ProjectRoot is the project's
// root directory, used by drivers that write project-scope config
// files (e.g. Claude's `.mcp.json`).
type Context struct {
	Name        string // context name from config.Contexts
	Agent       string // resolved agent name (e.g. "claude")
	HomeDir     string
	ProjectRoot string // absolute path to project root; may be empty in tests
	// Env carries the context-resolved environment variables that
	// drivers must pass through to the agent subprocess. Critical for
	// multi-profile agents (e.g. CLAUDE_CONFIG_DIR pointing to a
	// per-context Claude profile). Drivers should pass this through
	// to Runner.Run; subprocesses inherit the parent process env first
	// and Env values then override per-key.
	Env map[string]string
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

	// RequiresTTY reports whether the driver's mutating operations
	// (plugin install/uninstall) need an interactive terminal. Drivers
	// that shell out to TUI-only commands return true so `aide sync
	// --yes` can short-circuit unattended runs with a clear error
	// instead of hanging on stdin.
	RequiresTTY() bool

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

	// MCPHandler returns the per-format MCP read/write handler this
	// driver uses. Called only when SupportsMCP() == true.
	MCPHandler(ctx Context) MCPHandler

	// SupportedSourceShapes lists the plugin-entry value shapes this
	// driver consumes. Marketplace drivers return [ShapeMarketplace];
	// URL-direct drivers return [ShapeURLDirect]; drivers without a
	// plugin concept return nil. Sync uses this to validate per-context
	// agent/shape compatibility.
	SupportedSourceShapes() []SourceShape

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
}

// MCPHandler is the per-format MCP read/write interface. Each driver
// picks an implementation matching its agent's config-file format
// (JSON-flat for Gemini/Copilot, JSON-nested for Claude user-scope,
// TOML for Codex, and so on). Implementations live in
// internal/provision/mcp/.
type MCPHandler interface {
	// Read parses the MCP config at path. Returns the full server
	// map, the set of keys aide owns (per the _aide_managed marker),
	// and any error. Missing files MUST return empty maps without
	// error.
	Read(path string) (servers map[string]MCPServer, managed map[string]bool, err error)

	// Write reconciles desired into path. Entries managed by aide
	// are added/updated/removed to match desired; entries the user
	// added manually (not in _aide_managed) survive untouched. Writes
	// MUST be atomic so rollback is safe.
	Write(path string, desired map[string]MCPServer) error
}

// OpKind enumerates the operations a sync plan can contain.
type OpKind int

// Op kinds — concrete values for OpKind.
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

// Resource kinds — concrete values for Kind.
const (
	KindPlugin Kind = iota
	KindMCP
	KindMarketplace
)

func (k Kind) String() string {
	switch k {
	case KindPlugin:
		return "plugin"
	case KindMCP:
		return "mcp"
	case KindMarketplace:
		return "marketplace"
	default:
		return "unknown"
	}
}

// SourceShape is the polymorphic-value-shape of a plugins-block entry.
// Drivers advertise which shapes they consume via SupportedSourceShapes.
type SourceShape int

const (
	// ShapeMarketplace indicates a list-valued entry whose key is a
	// repo and whose value is the list of plugins to install from
	// that marketplace.
	ShapeMarketplace SourceShape = iota
	// ShapeURLDirect indicates a string-valued entry whose key is a
	// plugin name and whose value is a single install reference
	// (URL, github:owner/repo, or path).
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

// Op is one element of a sync plan.
type Op struct {
	Kind        Kind
	OpKind      OpKind
	Name        string       // resource name (plugin key or mcp server key)
	Plugin      *Plugin      // populated for KindPlugin install/update/adopt
	MCP         *MCPServer   // populated for KindMCP install/update/adopt
	OldMCP      *MCPServer   // populated for KindMCP update (for rollback)
	Marketplace *Marketplace // populated for KindMarketplace install
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
// context. Caller supplies HomeDir, ProjectRoot, and the resolved env
// map (after template rendering and secret materialization, if any).
func ResolveContext(name string, ctx config.Context, homeDir, projectRoot string, env map[string]string) Context {
	return Context{
		Name:        name,
		Agent:       ctx.Agent,
		HomeDir:     homeDir,
		ProjectRoot: projectRoot,
		Env:         env,
	}
}
