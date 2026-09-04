package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/tailscale/hujson"

	"github.com/jskswamy/aide/internal/fsutil"
	"github.com/jskswamy/aide/internal/provision"
)

// NewOpenCodeJSON returns the handler for OpenCode's config file
// (`~/.config/opencode/opencode.jsonc`, or the project-root
// equivalent). MCP servers live under the top-level `"mcp"` key.
//
// OpenCode's own CLI is not usable here: `opencode mcp add` has no
// flag for a local/stdio server's command (only --url/--env/--header,
// confirmed via `opencode mcp add --help`), and `opencode mcp list`
// live-connects to every configured server printing ANSI health-check
// output with no --json flag (confirmed by observing it attempt a
// real connection to a dummy URL). File-edit is the only reliable,
// non-interactive path — see
// docs/superpowers/specs/2026-08-31-opencode-pi-agent-support-design.md.
//
// The config file is JSONC (comments allowed) — confirmed OpenCode
// itself writes it as `opencode.jsonc`. Read runs hujson.Standardize
// before unmarshaling so a user's hand-added comments don't break
// parsing; Write always emits plain JSON via json.MarshalIndent
// (comments are not preserved across a write, same as every other
// file-edit handler in this package — none of them preserve original
// formatting either).
func NewOpenCodeJSON() provision.MCPHandler { return openCodeJSON{} }

type openCodeJSON struct{}

// openCodeServerBody is the on-disk shape for one MCP server.
// Confirmed directly (2026-08-31): hand-writing a "type":"local" entry
// with a "command" array and "environment" map and re-reading it via
// `opencode debug config` round-tripped byte-for-byte with no errors.
type openCodeServerBody struct {
	Type        string            `json:"type,omitempty"`
	Command     []string          `json:"command,omitempty"`
	URL         string            `json:"url,omitempty"`
	Environment map[string]string `json:"environment,omitempty"`
}

// Read implements provision.MCPHandler.
func (openCodeJSON) Read(path string) (map[string]provision.MCPServer, map[string]bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]provision.MCPServer{}, map[string]bool{}, nil
		}
		return nil, nil, fmt.Errorf("provision/mcp: reading %s: %w", path, err)
	}
	standardized, err := hujson.Standardize(data)
	if err != nil {
		return nil, nil, fmt.Errorf("provision/mcp: parsing %s: %w", path, err)
	}
	var doc struct {
		AideManaged []string                   `json:"_aide_managed,omitempty"`
		MCP         map[string]json.RawMessage `json:"mcp,omitempty"`
	}
	if err := json.Unmarshal(standardized, &doc); err != nil {
		return nil, nil, fmt.Errorf("provision/mcp: parsing %s: %w", path, err)
	}
	servers := map[string]provision.MCPServer{}
	for key, raw := range doc.MCP {
		var body openCodeServerBody
		if err := json.Unmarshal(raw, &body); err != nil {
			return nil, nil, fmt.Errorf("provision/mcp: parsing server %q: %w", key, err)
		}
		s := provision.MCPServer{Key: key, URL: body.URL, Env: body.Environment}
		if len(body.Command) > 0 {
			s.Command = body.Command[0]
			if len(body.Command) > 1 {
				s.Args = body.Command[1:]
			}
		}
		servers[key] = s
	}
	managed := map[string]bool{}
	for _, k := range doc.AideManaged {
		managed[k] = true
	}
	return servers, managed, nil
}

// Write implements provision.MCPHandler.
func (openCodeJSON) Write(path string, desired map[string]provision.MCPServer) error {
	existing := map[string]json.RawMessage{}
	if data, err := os.ReadFile(path); err == nil {
		standardized, serr := hujson.Standardize(data)
		if serr != nil {
			return fmt.Errorf("provision/mcp: parsing existing %s: %w", path, serr)
		}
		if err := json.Unmarshal(standardized, &existing); err != nil {
			return fmt.Errorf("provision/mcp: parsing existing %s: %w", path, err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("provision/mcp: reading %s: %w", path, err)
	}

	prevServers := map[string]json.RawMessage{}
	if raw, ok := existing["mcp"]; ok {
		_ = json.Unmarshal(raw, &prevServers)
	}
	prevManaged := []string{}
	if raw, ok := existing["_aide_managed"]; ok {
		_ = json.Unmarshal(raw, &prevManaged)
	}
	newServers, newManaged, err := reconcile(prevServers, prevManaged, desired, openCodeServerBodyAny)
	if err != nil {
		return err
	}

	managedRaw, _ := json.Marshal(newManaged)
	serversRaw, _ := json.Marshal(newServers)
	existing["_aide_managed"] = managedRaw
	existing["mcp"] = serversRaw

	out, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return fmt.Errorf("provision/mcp: marshalling %s: %w", path, err)
	}
	return fsutil.AtomicWrite(path, out)
}

// openCodeServerBodyAny converts an MCPServer into OpenCode's on-disk
// server-body shape, for use as reconcile's body func.
func openCodeServerBodyAny(s provision.MCPServer) any {
	body := openCodeServerBody{Environment: s.Env}
	if s.Command != "" {
		body.Type = "local"
		body.Command = append([]string{s.Command}, s.Args...)
	}
	if s.URL != "" {
		body.Type = "remote"
		body.URL = s.URL
	}
	return body
}
