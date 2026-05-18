package provision

import (
	"fmt"
	"strings"

	"github.com/jskswamy/aide/internal/config"
)

// ResolveDesired flattens the polymorphic v2 schema into a per-context
// Desired struct containing marketplaces, plugins, and mcp_servers.
// Composition order:
//   1. Apply ContextOverride to top-level Plugins map.
//   2. Walk resolved entries, classifying by shape.
//   3. Same for MCPServers.
func ResolveDesired(cfg *config.Config, contextName string) (Desired, error) {
	if cfg == nil {
		return Desired{}, fmt.Errorf("provision: nil config")
	}
	ctx, ok := cfg.Contexts[contextName]
	if !ok {
		return Desired{}, fmt.Errorf("provision: context %q not found", contextName)
	}

	// Plugins: apply per-context override over the top-level map.
	topPlugins := map[string]config.PluginEntry(cfg.Plugins)
	resolvedPlugins := ApplyOverride(topPlugins, ctx.Plugins)

	// MCP servers: apply per-context override over the top-level map.
	topMCP := map[string]config.MCPServer(cfg.MCPServers)
	resolvedMCP := ApplyOverride(topMCP, ctx.MCPServersOverride)

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
					Name:   plugin + "@" + key,
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

	// Legacy per-context MCPServers list: filter desired.MCPServers
	// to the selected subset if the user wrote the list-of-names form.
	// (v2 ContextOverride form is already applied via ApplyOverride.)
	if len(ctx.MCPServers) > 0 && ctx.MCPServersOverride == nil {
		filtered := map[string]MCPServer{}
		for _, name := range ctx.MCPServers {
			if v, ok := desired.MCPServers[name]; ok {
				filtered[name] = v
			} else if v, ok := topMCP[name]; ok {
				// Pre-existing top-level entry not yet copied (no override
				// hit). Include it.
				filtered[name] = MCPServer{
					Key: name, Command: v.Command, URL: v.URL, Args: v.Args, Env: v.Env,
				}
			}
		}
		desired.MCPServers = filtered
	}
	return desired, nil
}

// keyAsSource returns the install ref string for a marketplace key.
// Bare "owner/repo" gets a "github:" prefix; full URLs pass through.
func keyAsSource(key string) string {
	for _, prefix := range []string{"github:", "git:", "https://", "http://"} {
		if strings.HasPrefix(key, prefix) {
			return key
		}
	}
	return "github:" + key
}

// classifySource picks a Plugin.Source label for a URLDirect entry.
func classifySource(src string) string {
	switch {
	case strings.HasPrefix(src, "github:"):
		return "marketplace"
	case strings.HasPrefix(src, "git:"):
		return "git"
	case strings.HasPrefix(src, "/"):
		return "local"
	default:
		return "marketplace"
	}
}
