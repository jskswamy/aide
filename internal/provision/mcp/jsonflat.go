// Package mcp provides per-format MCP config-file handlers used by
// agent provisioning drivers. Each handler reads/writes one agent's
// config file format while preserving entries the user added by hand
// (tracked via the _aide_managed marker).
package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/jskswamy/aide/internal/fsutil"
	"github.com/jskswamy/aide/internal/provision"
)

// NewJSONFlat returns the handler for agents that store MCP servers
// as a flat top-level `mcpServers` map in JSON. Used by Gemini
// (`~/.gemini/settings.json`) and Copilot (`~/.copilot/mcp-config.json`).
func NewJSONFlat() provision.MCPHandler { return jsonFlat{} }

type jsonFlat struct{}

type jsonFlatShape struct {
	AideManaged []string                   `json:"_aide_managed,omitempty"`
	Servers     map[string]json.RawMessage `json:"mcpServers,omitempty"`
}

// Read implements provision.MCPHandler.
func (jsonFlat) Read(path string) (map[string]provision.MCPServer, map[string]bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]provision.MCPServer{}, map[string]bool{}, nil
		}
		return nil, nil, fmt.Errorf("provision/mcp: reading %s: %w", path, err)
	}
	var doc jsonFlatShape
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, nil, fmt.Errorf("provision/mcp: parsing %s: %w", path, err)
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
func (jsonFlat) Write(path string, desired map[string]provision.MCPServer) error {
	existing := map[string]json.RawMessage{}
	if data, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(data, &existing); err != nil {
			return fmt.Errorf("provision/mcp: parsing existing %s: %w", path, err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("provision/mcp: reading %s: %w", path, err)
	}

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

// serverBody converts an MCPServer into the JSON body shape (only
// non-empty fields are emitted).
func serverBody(s provision.MCPServer) map[string]any {
	body := map[string]any{}
	if s.Command != "" {
		body["command"] = s.Command
	}
	if s.URL != "" {
		body["url"] = s.URL
	}
	if len(s.Args) > 0 {
		body["args"] = s.Args
	}
	if len(s.Env) > 0 {
		body["env"] = s.Env
	}
	return body
}
