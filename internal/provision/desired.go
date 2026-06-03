package provision

import (
	"fmt"
	"strings"

	"github.com/jskswamy/aide/internal/config"
)

// ResolveDesired flattens the polymorphic v2 schema into a per-context
// Desired struct containing marketplaces, plugins, and mcp_servers.
// Composition order:
//  1. Apply ContextOverride to top-level Plugins map.
//  2. Walk resolved entries, classifying by shape.
//  3. Same for MCPServers.
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
				Source: ParseSourceRef(key).Aide(),
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
				Source: ParseSourceRef(entry.Source).Classify(),
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

	// Hooks: apply HooksOverride then substitute {agent}.
	agentName := ctx.Agent
	resolvedEvents := map[string][]config.HookEntry{}
	for event, entries := range cfg.Hooks {
		cp := make([]config.HookEntry, len(entries))
		copy(cp, entries)
		resolvedEvents[event] = cp
	}
	if ctx.Hooks != nil {
		for _, event := range ctx.Hooks.Exclude {
			delete(resolvedEvents, event)
		}
		for event, entries := range ctx.Hooks.Extra {
			resolvedEvents[event] = append(resolvedEvents[event], entries...)
		}
	}
	for event, entries := range resolvedEvents {
		for _, e := range entries {
			desired.Hooks = append(desired.Hooks, Hook{
				Event:   event,
				Matcher: e.Matcher,
				Command: substituteHookVars(e.Command, agentName),
				Timeout: e.Timeout,
			})
		}
	}

	return desired, nil
}

// substituteHookVars replaces known template variables in cmd.
// Currently only {agent} is supported; unknown {var} patterns pass through unchanged.
func substituteHookVars(cmd, agentName string) string {
	return strings.ReplaceAll(cmd, "{agent}", agentName)
}

// keyAsSource / classifySource were inlined here historically; see
// sourceref.go for the canonical SourceRef helper that owns the
// transport-prefix vocabulary.
