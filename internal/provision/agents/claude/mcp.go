package claude

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jskswamy/aide/internal/provision"
)

// InstalledMCPServers queries `claude mcp get <name>` for each requested
// name and keeps only entries whose Scope line is `User config ...`.
// Missing names (claude exits non-zero with "No MCP server found ...")
// simply omit from the map — the engine treats that as "needs install"
// without surfacing a parse error.
//
// We don't shell out to `claude mcp list`: that output mixes user-scope,
// local-scope, project-scope, plugin-bundled (`plugin:*`), and built-in
// (`claude.ai *`) entries with no scope discriminator, and aide must
// only act on user-scope entries it owns. Querying name-by-name is
// O(N managed servers) but avoids that ambiguity.
//
// Binary missing surfaces as (empty, nil) — parallel to InstalledPlugins
// — so `aide mcp list` still renders the declared/managed view when
// claude isn't installed.
func (d *Driver) InstalledMCPServers(ctx provision.Context, names []string) (map[string]provision.MCPServer, error) {
	out := map[string]provision.MCPServer{}
	for _, name := range names {
		stdout, _, code, err := d.runner.Run(context.Background(), ctx.Env, "claude", "mcp", "get", name)
		if err != nil {
			// Binary missing or unrunnable — treat all queries as misses
			// rather than spamming the same error per name.
			return map[string]provision.MCPServer{}, nil
		}
		if code != 0 {
			// "No MCP server found with name: ..." → not installed.
			continue
		}
		entry, ok := parseMCPGet(name, stdout)
		if !ok {
			continue
		}
		// Filter to user scope so aide doesn't touch project-scope
		// (`.mcp.json`) or local-scope (per-project) entries the user
		// added through other paths.
		if !strings.HasPrefix(entry.scope, "User") {
			continue
		}
		out[name] = entry.server
	}
	return out, nil
}

// InstallMCPServer runs `claude mcp add-json --scope user <name> <json>`.
// add-json is the most reliable add path: it round-trips the same JSON
// shape claude stores internally (verified against `claude mcp add
// --transport http` writes, 2026-05-21), so there's no `-- arg`
// separator parsing or `--scope` positional ambiguity.
func (d *Driver) InstallMCPServer(ctx provision.Context, s provision.MCPServer) error {
	body, err := claudeMCPAddJSONBody(s)
	if err != nil {
		return fmt.Errorf("claude mcp install %q: %w", s.Key, err)
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("claude mcp install %q: marshal: %w", s.Key, err)
	}
	// Idempotency: claude's `mcp add-json` rejects names that already
	// exist. We pre-remove (tolerating "not found") so re-syncs and
	// rollback replays succeed. The pair runs atomically from claude's
	// POV — there's no `mcp upsert` to use here.
	_, _, _, _ = d.runner.Run(context.Background(), ctx.Env, "claude", "mcp", "remove", s.Key, "-s", "user")
	return provision.RunCLI(context.Background(), d.runner, ctx.Env,
		"claude mcp add-json "+s.Key,
		"claude", []string{"mcp", "add-json", "--scope", "user", s.Key, string(raw)})
}

// UninstallMCPServer runs `claude mcp remove <name> -s user`. The
// "No user-scoped MCP server found" stderr is tolerated so journal
// replay during rollback never fails. DefaultTolerateStderr's
// "not found" / "not installed" tokens don't match claude's wording
// (it says "No ... found"), so the claude-specific phrase is
// appended explicitly.
func (d *Driver) UninstallMCPServer(ctx provision.Context, name string) error {
	tolerate := append([]string{}, provision.DefaultTolerateStderr...)
	tolerate = append(tolerate, "No user-scoped MCP server found", "No MCP server found")
	return provision.RunCLI(context.Background(), d.runner, ctx.Env,
		"claude mcp remove "+name,
		"claude", []string{"mcp", "remove", name, "-s", "user"},
		tolerate...)
}

// claudeMCPAddJSONBody constructs the JSON payload `claude mcp
// add-json` expects. Transport rules (verified 2026-05-21 against
// claude 0.x):
//   - URL set, Command empty → `{"type":"http","url":...}` (or `"sse"`
//     if the URL path encodes it; aide always emits http and lets the
//     user override via add-json directly if they need SSE).
//   - Command set → `{"type":"stdio","command":...,"args":[...],"env":{...}}`.
//
// Headers and timeouts are deliberately not threaded through —
// MCPServer doesn't carry them today. Add as fields if needed.
func claudeMCPAddJSONBody(s provision.MCPServer) (map[string]any, error) {
	body := map[string]any{}
	switch {
	case s.URL != "" && s.Command == "":
		body["type"] = "http"
		body["url"] = s.URL
	case s.Command != "":
		body["type"] = "stdio"
		body["command"] = s.Command
		if len(s.Args) > 0 {
			body["args"] = s.Args
		}
		if len(s.Env) > 0 {
			body["env"] = s.Env
		}
	default:
		return nil, fmt.Errorf("server %q has neither URL nor Command", s.Key)
	}
	return body, nil
}

// mcpGetEntry is the parsed shape of one `claude mcp get` invocation.
type mcpGetEntry struct {
	scope  string
	server provision.MCPServer
}

// parseMCPGet extracts a server definition from `claude mcp get <name>`
// output. The format (verified 2026-05-21) looks like:
//
//	<name>:
//	  Scope: User config (available in all your projects)
//	  Status: ✗ Failed to connect
//	  Type: http
//	  URL: http://...
//
// For stdio:
//
//	<name>:
//	  Scope: Local config (private to you in this project)
//	  Type: stdio
//	  Command: node
//	  Args: script.js arg1 arg2
//	  Environment:
//	    KEY=value
//
// Args are split on whitespace — values containing spaces won't
// round-trip cleanly (aide would re-write each sync). Caller's
// responsibility to keep stdio args space-free until a quote-aware
// parser lands.
func parseMCPGet(name, stdout string) (mcpGetEntry, bool) {
	srv := provision.MCPServer{Key: name}
	entry := mcpGetEntry{server: srv}
	inEnv := false
	env := map[string]string{}
	for _, line := range strings.Split(stdout, "\n") {
		// "Environment:" header switches to KV-collect mode.
		if strings.TrimSpace(line) == "Environment:" {
			inEnv = true
			continue
		}
		if inEnv {
			kv := strings.TrimSpace(line)
			if kv == "" {
				inEnv = false
				continue
			}
			if eq := strings.IndexByte(kv, '='); eq > 0 {
				env[kv[:eq]] = kv[eq+1:]
				continue
			}
			inEnv = false
		}
		trim := strings.TrimSpace(line)
		colon := strings.IndexByte(trim, ':')
		if colon <= 0 {
			continue
		}
		key := trim[:colon]
		val := strings.TrimSpace(trim[colon+1:])
		switch key {
		case "Scope":
			entry.scope = val
		case "Type":
			// Type is implicit in our struct via URL vs Command;
			// no field to populate but worth capturing for future
			// strict-equality checks.
		case "URL":
			entry.server.URL = val
		case "Command":
			entry.server.Command = val
		case "Args":
			if val != "" {
				entry.server.Args = strings.Fields(val)
			}
		}
	}
	if len(env) > 0 {
		entry.server.Env = env
	}
	// A valid entry must have either URL or Command. The first line
	// of stdout is just "<name>:" — if parsing yielded neither field,
	// the output was likely an error or unexpected shape.
	if entry.server.URL == "" && entry.server.Command == "" {
		return mcpGetEntry{}, false
	}
	return entry, true
}
