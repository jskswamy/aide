package mcp

import (
	"errors"
	"fmt"
	"os"
	"sort"

	"github.com/pelletier/go-toml/v2"

	"github.com/jskswamy/aide/internal/fsutil"
	"github.com/jskswamy/aide/internal/provision"
)

// NewCodexTOML returns the handler for Codex's TOML config file
// (`~/.codex/config.toml`). MCP servers live under
// `[mcp_servers.<name>]` tables. The aide-managed marker is stored as
// a top-level key `_aide_managed_mcp = ["..."]` to avoid colliding
// with codex's own keys.
func NewCodexTOML() provision.MCPHandler { return codexTOML{} }

type codexTOML struct{}

// codexServerBody is the on-disk shape for one MCP server in
// Codex's TOML. We use lowercase TOML keys.
type codexServerBody struct {
	Command string            `toml:"command,omitempty"`
	URL     string            `toml:"url,omitempty"`
	Args    []string          `toml:"args,omitempty"`
	Env     map[string]string `toml:"env,omitempty"`
}

// Read implements provision.MCPHandler.
func (codexTOML) Read(path string) (map[string]provision.MCPServer, map[string]bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]provision.MCPServer{}, map[string]bool{}, nil
		}
		return nil, nil, fmt.Errorf("provision/mcp: reading %s: %w", path, err)
	}
	top := map[string]any{}
	if err := toml.Unmarshal(data, &top); err != nil {
		return nil, nil, fmt.Errorf("provision/mcp: parsing %s: %w", path, err)
	}

	servers := map[string]provision.MCPServer{}
	if raw, ok := top["mcp_servers"]; ok {
		if m, ok := raw.(map[string]any); ok {
			for key, val := range m {
				body, ok := val.(map[string]any)
				if !ok {
					continue
				}
				s := provision.MCPServer{Key: key}
				if v, ok := body["command"].(string); ok {
					s.Command = v
				}
				if v, ok := body["url"].(string); ok {
					s.URL = v
				}
				if arr, ok := body["args"].([]any); ok {
					for _, a := range arr {
						if str, ok := a.(string); ok {
							s.Args = append(s.Args, str)
						}
					}
				}
				if env, ok := body["env"].(map[string]any); ok {
					s.Env = map[string]string{}
					for k, v := range env {
						if str, ok := v.(string); ok {
							s.Env[k] = str
						}
					}
				}
				servers[key] = s
			}
		}
	}

	managed := map[string]bool{}
	if raw, ok := top["_aide_managed_mcp"]; ok {
		if arr, ok := raw.([]any); ok {
			for _, k := range arr {
				if s, ok := k.(string); ok {
					managed[s] = true
				}
			}
		}
	}
	return servers, managed, nil
}

// Write implements provision.MCPHandler.
func (codexTOML) Write(path string, desired map[string]provision.MCPServer) error {
	top := map[string]any{}
	if data, err := os.ReadFile(path); err == nil {
		if err := toml.Unmarshal(data, &top); err != nil {
			return fmt.Errorf("provision/mcp: parsing existing %s: %w", path, err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("provision/mcp: reading %s: %w", path, err)
	}

	prevManaged := map[string]bool{}
	if raw, ok := top["_aide_managed_mcp"]; ok {
		if arr, ok := raw.([]any); ok {
			for _, k := range arr {
				if s, ok := k.(string); ok {
					prevManaged[s] = true
				}
			}
		}
	}

	servers := map[string]any{}
	if raw, ok := top["mcp_servers"]; ok {
		if m, ok := raw.(map[string]any); ok {
			for key, val := range m {
				if prevManaged[key] {
					continue
				}
				servers[key] = val
			}
		}
	}
	newManaged := make([]string, 0, len(desired))
	for key, s := range desired {
		body := codexServerBody{
			Command: s.Command,
			URL:     s.URL,
			Args:    s.Args,
			Env:     s.Env,
		}
		// Convert to map[string]any via round-trip to keep the top-level
		// generic and let go-toml emit one consistent table format.
		raw, err := toml.Marshal(body)
		if err != nil {
			return fmt.Errorf("provision/mcp: marshalling server %q: %w", key, err)
		}
		decoded := map[string]any{}
		if err := toml.Unmarshal(raw, &decoded); err != nil {
			return fmt.Errorf("provision/mcp: round-tripping server %q: %w", key, err)
		}
		servers[key] = decoded
		newManaged = append(newManaged, key)
	}
	sort.Strings(newManaged)

	top["mcp_servers"] = servers
	// Convert []string to []any so toml emits string array.
	managedAny := make([]any, len(newManaged))
	for i, k := range newManaged {
		managedAny[i] = k
	}
	top["_aide_managed_mcp"] = managedAny

	out, err := toml.Marshal(top)
	if err != nil {
		return fmt.Errorf("provision/mcp: marshalling %s: %w", path, err)
	}
	return fsutil.AtomicWrite(path, out)
}
