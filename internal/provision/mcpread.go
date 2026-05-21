package provision

import (
	"fmt"
	"sort"
)

// ReadInstalledMCP returns the agent's currently-installed MCP servers
// for the names that aide cares about (typically the union of declared
// and managed). It prefers MCPInstaller (CLI-driven) over MCPHandler
// (file-edit), matching engine.Apply's dispatch — so callers see the
// same "installed" view sync uses to plan and apply.
//
// names bounds the query for CLI-driven drivers (claude's `mcp get
// <name>` doesn't have a list-all-user-scope variant; gemini and
// codex parse list output server-by-server). Empty names returns an
// empty map without invoking the driver.
//
// File-based handlers ignore names — they read the whole file. That
// asymmetry is intentional: the file is already loaded, scoping
// adds nothing.
func ReadInstalledMCP(prov Provisioner, ctx Context, names []string) (map[string]MCPServer, error) {
	if prov == nil || !prov.SupportsMCP() {
		return map[string]MCPServer{}, nil
	}
	if mi, ok := prov.(MCPInstaller); ok {
		if len(names) == 0 {
			return map[string]MCPServer{}, nil
		}
		got, err := mi.InstalledMCPServers(ctx, names)
		if err != nil {
			return nil, fmt.Errorf("listing installed MCP servers: %w", err)
		}
		if got == nil {
			got = map[string]MCPServer{}
		}
		return got, nil
	}
	handler := prov.MCPHandler(ctx)
	if handler == nil {
		return map[string]MCPServer{}, nil
	}
	got, _, err := handler.Read(prov.MCPConfigPath(ctx))
	if err != nil {
		return nil, fmt.Errorf("reading MCP config: %w", err)
	}
	if got == nil {
		got = map[string]MCPServer{}
	}
	return got, nil
}

// MCPQueryNames returns the deduplicated, sorted union of desired and
// managed MCP server names — the bounded name list ReadInstalledMCP
// expects for CLI-driven drivers. Returns nil when both sources are
// empty so callers can early-out without an empty slice.
func MCPQueryNames(desired map[string]MCPServer, managed map[string]ManagedItem) []string {
	if len(desired) == 0 && len(managed) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(desired)+len(managed))
	for k := range desired {
		seen[k] = struct{}{}
	}
	for k := range managed {
		seen[k] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
