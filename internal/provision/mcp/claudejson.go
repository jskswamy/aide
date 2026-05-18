package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"

	"github.com/jskswamy/aide/internal/fsutil"
	"github.com/jskswamy/aide/internal/provision"
)

// NewClaudeJSON returns the handler for Claude Code MCP config files.
// Claude writes MCP entries in two shapes:
//
//   - Project scope (`.mcp.json` at repo root): flat top-level
//     `mcpServers` map — same shape as Gemini.
//   - User scope (`~/.claude.json`): nested under
//     `projects.<projectRoot>.mcpServers`.
//
// The handler auto-detects which shape a file uses on Read (presence
// of top-level `projects` key with the configured projectRoot wins
// nested; otherwise treated as flat). On Write the handler keeps the
// existing shape; if creating a new file, it writes the flat shape
// when projectRoot is empty and nested otherwise.
func NewClaudeJSON(projectRoot string) provision.MCPHandler {
	return claudeJSON{projectRoot: projectRoot}
}

type claudeJSON struct {
	projectRoot string
}

// Read implements provision.MCPHandler.
func (c claudeJSON) Read(path string) (map[string]provision.MCPServer, map[string]bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]provision.MCPServer{}, map[string]bool{}, nil
		}
		return nil, nil, fmt.Errorf("provision/mcp: reading %s: %w", path, err)
	}
	top := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &top); err != nil {
		return nil, nil, fmt.Errorf("provision/mcp: parsing %s: %w", path, err)
	}

	// Nested shape: projects.<projectRoot>.mcpServers
	if raw, ok := top["projects"]; ok && c.projectRoot != "" {
		projects := map[string]json.RawMessage{}
		if err := json.Unmarshal(raw, &projects); err == nil {
			if entryRaw, present := projects[c.projectRoot]; present {
				return parseShape(entryRaw)
			}
		}
		// projects key present but no entry for our root → empty
		return map[string]provision.MCPServer{}, map[string]bool{}, nil
	}
	// Flat shape: top-level mcpServers
	return parseShape(data)
}

func parseShape(data []byte) (map[string]provision.MCPServer, map[string]bool, error) {
	var doc jsonFlatShape
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, nil, fmt.Errorf("provision/mcp: parsing shape: %w", err)
	}
	servers := map[string]provision.MCPServer{}
	for key, raw := range doc.Servers {
		var s provision.MCPServer
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, nil, fmt.Errorf("provision/mcp: parsing server %q: %w", key, err)
		}
		s.Key = key
		servers[key] = s
	}
	managed := map[string]bool{}
	for _, k := range doc.AideManaged {
		managed[k] = true
	}
	return servers, managed, nil
}

// Write implements provision.MCPHandler.
func (c claudeJSON) Write(path string, desired map[string]provision.MCPServer) error {
	existing := map[string]json.RawMessage{}
	if data, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(data, &existing); err != nil {
			return fmt.Errorf("provision/mcp: parsing existing %s: %w", path, err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("provision/mcp: reading %s: %w", path, err)
	}

	// Detect shape: if `projects` key is set and projectRoot non-empty,
	// write nested. Otherwise flat.
	_, hasProjects := existing["projects"]
	if hasProjects && c.projectRoot != "" {
		return c.writeNested(path, existing, desired)
	}
	if c.projectRoot != "" && len(existing) == 0 {
		// New file: prefer nested only if user scope was intended,
		// which we approximate by projectRoot+empty file. Be conservative
		// here — prefer flat unless we know we're writing user scope.
		// projectRoot-only is not enough; require explicit projects key.
		return c.writeFlat(path, existing, desired)
	}
	return c.writeFlat(path, existing, desired)
}

func (c claudeJSON) writeFlat(path string, existing map[string]json.RawMessage, desired map[string]provision.MCPServer) error {
	prevServers := map[string]json.RawMessage{}
	if raw, ok := existing["mcpServers"]; ok {
		_ = json.Unmarshal(raw, &prevServers)
	}
	prevManaged := []string{}
	if raw, ok := existing["_aide_managed"]; ok {
		_ = json.Unmarshal(raw, &prevManaged)
	}
	newServers, newManaged, err := reconcile(prevServers, prevManaged, desired)
	if err != nil {
		return err
	}
	managedRaw, _ := json.Marshal(newManaged)
	serversRaw, _ := json.Marshal(newServers)
	existing["_aide_managed"] = managedRaw
	existing["mcpServers"] = serversRaw
	out, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return fmt.Errorf("provision/mcp: marshalling %s: %w", path, err)
	}
	return fsutil.AtomicWrite(path, out)
}

func (c claudeJSON) writeNested(path string, existing map[string]json.RawMessage, desired map[string]provision.MCPServer) error {
	projects := map[string]json.RawMessage{}
	if raw, ok := existing["projects"]; ok {
		_ = json.Unmarshal(raw, &projects)
	}
	entry := map[string]json.RawMessage{}
	if raw, ok := projects[c.projectRoot]; ok {
		_ = json.Unmarshal(raw, &entry)
	}
	prevServers := map[string]json.RawMessage{}
	if raw, ok := entry["mcpServers"]; ok {
		_ = json.Unmarshal(raw, &prevServers)
	}
	prevManaged := []string{}
	if raw, ok := entry["_aide_managed"]; ok {
		_ = json.Unmarshal(raw, &prevManaged)
	}
	newServers, newManaged, err := reconcile(prevServers, prevManaged, desired)
	if err != nil {
		return err
	}
	managedRaw, _ := json.Marshal(newManaged)
	serversRaw, _ := json.Marshal(newServers)
	entry["_aide_managed"] = managedRaw
	entry["mcpServers"] = serversRaw
	entryRaw, _ := json.Marshal(entry)
	projects[c.projectRoot] = entryRaw
	projectsRaw, _ := json.Marshal(projects)
	existing["projects"] = projectsRaw
	out, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return fmt.Errorf("provision/mcp: marshalling %s: %w", path, err)
	}
	return fsutil.AtomicWrite(path, out)
}

// reconcile merges prev (managed-only entries dropped) with desired
// and returns the new server map plus the sorted managed-key list.
func reconcile(prevServers map[string]json.RawMessage, prevManaged []string, desired map[string]provision.MCPServer) (map[string]json.RawMessage, []string, error) {
	wasManaged := map[string]bool{}
	for _, k := range prevManaged {
		wasManaged[k] = true
	}
	newServers := map[string]json.RawMessage{}
	for key, raw := range prevServers {
		if wasManaged[key] {
			continue
		}
		newServers[key] = raw
	}
	newManaged := make([]string, 0, len(desired))
	for key, s := range desired {
		raw, err := json.Marshal(serverBody(s))
		if err != nil {
			return nil, nil, fmt.Errorf("provision/mcp: marshalling server %q: %w", key, err)
		}
		newServers[key] = raw
		newManaged = append(newManaged, key)
	}
	sort.Strings(newManaged)
	return newServers, newManaged, nil
}
