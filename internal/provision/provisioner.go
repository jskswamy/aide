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

	// SupportsHooks reports whether this agent supports hook injection
	// via the HookInstaller interface.
	SupportsHooks() bool

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

// MCPInstaller is the CLI-driven counterpart to MCPHandler. Drivers
// implement this when the agent has its own `mcp add/remove/list` CLI
// surface that aide should drive — same pattern as
// InstallPlugin/UninstallPlugin/InstalledPlugins. The engine prefers
// MCPInstaller over MCPHandler when both are available.
//
// Why both? File-edit drivers (gemini, copilot, codex) work fine with
// MCPHandler because their config formats are stable and aide owns the
// reconcile. Claude, by contrast, exposes scope/user/project semantics
// (`--scope user|local|project`) and validates entries beyond what's
// in the JSON on disk — `type: http` is required, file paths drift
// across versions. Driving claude's own CLI keeps aide insulated from
// those internals, exactly like InstallPlugin/UninstallPlugin does for
// plugins.
type MCPInstaller interface {
	// InstalledMCPServers reports which of names are currently
	// installed under the scope this driver manages. Implementations
	// should ignore entries from other scopes (e.g. project-scope
	// `.mcp.json`, plugin-bundled, built-in remotes) so aide's diff
	// stays focused on what aide actually owns.
	//
	// The caller passes a bounded name list — typically
	// (desired ∪ managed) — so drivers needn't enumerate every server
	// the agent knows about. Names not installed simply omit from the
	// returned map; an error is reserved for actual failure (binary
	// missing returns (empty, nil), parallel to InstalledPlugins).
	InstalledMCPServers(ctx Context, names []string) (map[string]MCPServer, error)

	// InstallMCPServer registers s in the agent. Must be idempotent
	// for rollback safety (re-running after a partial failure is OK).
	InstallMCPServer(ctx Context, s MCPServer) error

	// UninstallMCPServer removes name. Must succeed even if name is
	// already absent so the journal can replay without failing.
	UninstallMCPServer(ctx Context, name string) error
}

// MCPHandler is the per-format MCP read/write interface. Each driver
// picks an implementation matching its agent's config-file format
// (JSON-flat for Gemini/Copilot, TOML for Codex, and so on).
// Implementations live in internal/provision/mcp/. Claude uses the
// CLI-driven MCPInstaller path instead.
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

// Hook is aide's internal, agent-neutral hook representation.
// Drivers receive a []Hook and translate to their native format.
type Hook struct {
	Event   string // normalized event name, e.g. "pre_tool"
	Matcher string // normalized matcher, e.g. "shell"; empty = all tools
	Command string // after {agent} substitution
	Timeout int    // seconds; 0 = driver default
}

// TemplateVar describes one substitution variable in hook commands.
type TemplateVar struct {
	Name        string
	Description string
}

// HookTemplateVars is the registry of hook command template variables.
// Drives CLI help and interactive prompts.
var HookTemplateVars = []TemplateVar{
	{Name: "agent", Description: "replaced with the agent name for each context"},
}

// HookInstaller is the file-edit interface for hook management. Drivers
// implement it when the agent has no CLI surface for hook management.
// Ownership is tracked in managed.json (not via in-file markers).
type HookInstaller interface {
	// ReadHooks returns all hooks from the agent's config — both aide-managed
	// and user-added. Used for adoption detection and drift comparison.
	// For file-based formats (Gemini, Copilot, Hermes) that use naming
	// prefixes (aide_*) as ownership, only aide-prefixed entries are returned;
	// scanning arbitrary user scripts is not supported for those formats.
	ReadHooks(ctx Context) ([]Hook, error)
	// WriteHooks atomically reconciles hooks in the agent's config.
	// prevManaged is the set aide previously managed (entries to remove);
	// desired is the new set aide should manage (entries to add).
	// User-added hooks not in prevManaged are left untouched.
	WriteHooks(ctx Context, prevManaged []Hook, desired []Hook) error
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
	KindHook
)

func (k Kind) String() string {
	switch k {
	case KindPlugin:
		return "plugin"
	case KindMCP:
		return "mcp"
	case KindMarketplace:
		return "marketplace"
	case KindHook:
		return "hook"
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
	Hook        *Hook        // populated for KindHook install/uninstall
}

// Plan is the ordered list of operations to apply for one context.
type Plan struct {
	Context     Context
	Ops         []Op
	HookManaged []ManagedHook // hooks managed before this plan (for WriteHooks removal)
	HookDesired []Hook        // full desired hook set (for WriteHooks addition)
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
// When ctx.Profile is set, validates and injects the driver's
// per-profile env var.
func ResolveContext(name string, ctx config.Context, homeDir, projectRoot string, env map[string]string) (Context, error) {
	out := Context{
		Name:        name,
		Agent:       ctx.Agent,
		HomeDir:     homeDir,
		ProjectRoot: projectRoot,
		Env:         env,
	}
	if ctx.Profile == "" && ctx.ProfileDir == "" {
		return out, nil
	}
	// Clone env before mutating so the caller's map is not modified.
	cloned := make(map[string]string, len(out.Env)+1)
	for k, v := range out.Env {
		cloned[k] = v
	}
	merged, err := InjectProfileEnv(ctx, cloned, homeDir)
	if err != nil {
		return out, err
	}
	out.Env = merged
	return out, nil
}
