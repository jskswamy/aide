package provision

import (
	"slices"
	"sort"
)

// Desired is the resolved configuration for one context.
type Desired struct {
	Plugins      map[string]Plugin
	MCPServers   map[string]MCPServer
	Marketplaces map[string]Marketplace
}

// Installed is what the agent currently reports.
type Installed struct {
	// Plugins is the list of installed plugin keys (names match the
	// top-level config keys when possible).
	Plugins []string
	// MCPServers maps key → server config as registered in the agent.
	MCPServers map[string]MCPServer
	// Marketplaces maps key → marketplace as registered in the agent.
	Marketplaces map[string]Marketplace
}

// ComputePlan diffs desired/installed/managed and returns the ordered
// op list. Sort order: installs, updates, uninstalls, then ignored
// unmanaged items. Within each bucket, names sort alphabetically for
// deterministic plan output.
func ComputePlan(ctx Context, desired Desired, installed Installed, managed ContextState) Plan {
	if managed.Plugins == nil {
		managed.Plugins = map[string]ManagedItem{}
	}
	if managed.MCPServers == nil {
		managed.MCPServers = map[string]ManagedItem{}
	}
	if managed.Marketplaces == nil {
		managed.Marketplaces = map[string]ManagedItem{}
	}

	var marketplaceInstalls, marketplaceUninstalls []Op
	for key, m := range desired.Marketplaces {
		if _, present := installed.Marketplaces[key]; present {
			continue
		}
		// Skip if already managed (avoid double-install across syncs).
		if _, mgd := managed.Marketplaces[key]; mgd {
			continue
		}
		mc := m
		marketplaceInstalls = append(marketplaceInstalls, Op{
			Kind:        KindMarketplace,
			OpKind:      OpInstall,
			Name:        key,
			Marketplace: &mc,
		})
	}
	for key := range managed.Marketplaces {
		if _, stillDesired := desired.Marketplaces[key]; !stillDesired {
			marketplaceUninstalls = append(marketplaceUninstalls, Op{
				Kind:   KindMarketplace,
				OpKind: OpUninstall,
				Name:   key,
			})
		}
	}

	var installs, updates, uninstalls, ignores []Op

	// Marketplaces installed in the agent but neither declared nor
	// managed by aide → ignore (surface as "unmanaged" in plan output).
	// Mirrors the same case for plugins and MCP servers below.
	for key := range installed.Marketplaces {
		if _, isDesired := desired.Marketplaces[key]; isDesired {
			continue
		}
		if _, isManaged := managed.Marketplaces[key]; isManaged {
			continue
		}
		ignores = append(ignores, Op{
			Kind: KindMarketplace, OpKind: OpIgnore, Name: key,
		})
	}

	// --- Plugins ---
	for key, p := range desired.Plugins {
		if !slices.Contains(installed.Plugins, key) {
			pp := p
			installs = append(installs, Op{
				Kind: KindPlugin, OpKind: OpInstall, Name: key, Plugin: &pp,
			})
			continue
		}
		// Installed and desired: detect version drift via Name field.
		// Lookup is by key in managed state; if version recorded differs
		// from desired Name's version part, mark as update.
		if mi, ok := managed.Plugins[key]; ok && mi.Version != "" && mi.Version != extractVersion(p.Name) {
			pp := p
			updates = append(updates, Op{
				Kind: KindPlugin, OpKind: OpUpdate, Name: key, Plugin: &pp,
			})
		}
	}
	for key := range managed.Plugins {
		if _, stillDesired := desired.Plugins[key]; !stillDesired {
			uninstalls = append(uninstalls, Op{
				Kind: KindPlugin, OpKind: OpUninstall, Name: key,
			})
		}
	}
	for _, key := range installed.Plugins {
		if _, isDesired := desired.Plugins[key]; isDesired {
			continue
		}
		if _, isManaged := managed.Plugins[key]; isManaged {
			continue
		}
		ignores = append(ignores, Op{
			Kind: KindPlugin, OpKind: OpIgnore, Name: key,
		})
	}

	// --- MCP servers ---
	for key, m := range desired.MCPServers {
		cur, present := installed.MCPServers[key]
		if !present {
			mm := m
			installs = append(installs, Op{
				Kind: KindMCP, OpKind: OpInstall, Name: key, MCP: &mm,
			})
			continue
		}
		if !mcpEqual(cur, m) {
			mm := m
			old := cur
			updates = append(updates, Op{
				Kind: KindMCP, OpKind: OpUpdate, Name: key, MCP: &mm, OldMCP: &old,
			})
		}
	}
	for key := range managed.MCPServers {
		if _, stillDesired := desired.MCPServers[key]; !stillDesired {
			uninstalls = append(uninstalls, Op{
				Kind: KindMCP, OpKind: OpUninstall, Name: key,
			})
		}
	}
	for key := range installed.MCPServers {
		if _, isDesired := desired.MCPServers[key]; isDesired {
			continue
		}
		if _, isManaged := managed.MCPServers[key]; isManaged {
			continue
		}
		ignores = append(ignores, Op{
			Kind: KindMCP, OpKind: OpIgnore, Name: key,
		})
	}

	sortOps := func(o []Op) { sort.Slice(o, func(i, j int) bool { return o[i].Name < o[j].Name }) }
	sortOps(marketplaceInstalls)
	sortOps(marketplaceUninstalls)
	sortOps(installs)
	sortOps(updates)
	sortOps(uninstalls)
	sortOps(ignores)

	// Marketplaces are added before plugins (a plugin can only install
	// from a marketplace that is already registered). Marketplace
	// removals run after plugin removals so we never strand a plugin
	// whose marketplace is gone.
	all := append([]Op{}, marketplaceInstalls...)
	all = append(all, installs...)
	all = append(all, updates...)
	all = append(all, uninstalls...)
	all = append(all, marketplaceUninstalls...)
	all = append(all, ignores...)
	return Plan{Context: ctx, Ops: all}
}

// extractVersion returns the substring after "@" in name, or "" if absent.
// Example: "linear@1.2" -> "1.2", "linear" -> "".
func extractVersion(name string) string {
	for i := len(name) - 1; i >= 0; i-- {
		if name[i] == '@' {
			return name[i+1:]
		}
	}
	return ""
}

// mcpEqual reports whether two MCP server configs are equivalent for
// plan purposes. Compares Command, URL, Args (order-sensitive), Env
// (order-insensitive).
func mcpEqual(a, b MCPServer) bool {
	if a.Command != b.Command || a.URL != b.URL {
		return false
	}
	if !slices.Equal(a.Args, b.Args) {
		return false
	}
	if len(a.Env) != len(b.Env) {
		return false
	}
	for k, v := range a.Env {
		if b.Env[k] != v {
			return false
		}
	}
	return true
}
