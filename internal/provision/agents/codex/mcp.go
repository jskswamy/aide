package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jskswamy/aide/internal/provision"
)

// InstalledMCPServers shells out to `codex mcp get <name> --json` for
// each requested name. Codex's `mcp list --json` shape isn't pinned
// in the public reference (developers.openai.com/codex/cli/reference,
// fetched 2026-05-21), so the per-name `get --json` path is the safest
// contract: each call returns the raw TOML body for one server in
// JSON form, which mirrors the well-known `[mcp_servers.<name>]`
// schema (command / url / args / env).
//
// IMPORTANT: untested against a live `codex` binary in this session
// (codex isn't installed on the dev machine). Behaviour was derived
// from docs + the TOML schema aide already understands via the
// existing codextoml handler. If `codex mcp get --json` returns a
// different envelope, the parser below needs adjustment.
//
// Binary missing → (empty, nil), parallel to InstalledPlugins.
func (d *Driver) InstalledMCPServers(ctx provision.Context, names []string) (map[string]provision.MCPServer, error) {
	out := map[string]provision.MCPServer{}
	for _, name := range names {
		stdout, stderr, code, err := d.runner.Run(context.Background(), ctx.Env, "codex", "mcp", "get", name, "--json")
		if err != nil {
			return map[string]provision.MCPServer{}, nil
		}
		if code != 0 {
			// "not found" / "not configured" → exclude from result.
			// Other errors fall through silently here; surface via
			// the install path if the entry is needed.
			if isMissing(stderr) {
				continue
			}
			continue
		}
		srv, ok := parseCodexMCPGetJSON(name, stdout)
		if !ok {
			continue
		}
		out[name] = srv
	}
	return out, nil
}

// InstallMCPServer runs `codex mcp add`. HTTP uses `--url <url>`;
// stdio uses `--env K=V` repeated + the `-- <command> <args...>`
// separator. Codex has a single configuration tier, so there is no
// `--scope` flag (verified against docs).
//
// Idempotency: codex's `mcp add` rejects names already present in
// the config. We pre-remove (tolerating not-found) so re-syncs and
// rollback replays succeed.
func (d *Driver) InstallMCPServer(ctx provision.Context, s provision.MCPServer) error {
	if s.URL == "" && s.Command == "" {
		return fmt.Errorf("server %q has neither URL nor Command", s.Key)
	}
	_, _, _, _ = d.runner.Run(context.Background(), ctx.Env, "codex", "mcp", "remove", s.Key)

	args := []string{"mcp", "add", s.Key}
	if s.URL != "" && s.Command == "" {
		args = append(args, "--url", s.URL)
	} else {
		for k, v := range s.Env {
			args = append(args, "--env", k+"="+v)
		}
		args = append(args, "--")
		args = append(args, s.Command)
		args = append(args, s.Args...)
	}
	return provision.RunCLI(context.Background(), d.runner, ctx.Env,
		"codex mcp add "+s.Key,
		"codex", args)
}

// UninstallMCPServer runs `codex mcp remove <name>`. Codex's missing-
// entry phrasing isn't pinned by docs; we tolerate the standard
// "not found" / "not installed" / "not configured" substrings plus a
// codex-specific "no MCP server" pattern as a defensive guess.
func (d *Driver) UninstallMCPServer(ctx provision.Context, name string) error {
	tolerate := append([]string{}, provision.DefaultTolerateStderr...)
	tolerate = append(tolerate, "no MCP server", "No MCP server")
	return provision.RunCLI(context.Background(), d.runner, ctx.Env,
		"codex mcp remove "+name,
		"codex", []string{"mcp", "remove", name},
		tolerate...)
}

// parseCodexMCPGetJSON decodes `codex mcp get <name> --json` output
// into an MCPServer. Expected shape mirrors the `[mcp_servers.<name>]`
// TOML body codex stores on disk: {command, url, args, env}. If codex
// wraps the body in an outer envelope ({"name":..,"config":..}), the
// fallback branch unwraps a common shape.
func parseCodexMCPGetJSON(name, stdout string) (provision.MCPServer, bool) {
	srv := provision.MCPServer{Key: name}
	var direct codexMCPBody
	if err := json.Unmarshal([]byte(stdout), &direct); err == nil && (direct.Command != "" || direct.URL != "") {
		srv.Command = direct.Command
		srv.URL = direct.URL
		srv.Args = direct.Args
		if len(direct.Env) > 0 {
			srv.Env = direct.Env
		}
		return srv, true
	}
	// Fallback: try {"name":..,"config":{command,url,args,env}}.
	var wrapped struct {
		Config codexMCPBody `json:"config"`
	}
	if err := json.Unmarshal([]byte(stdout), &wrapped); err == nil && (wrapped.Config.Command != "" || wrapped.Config.URL != "") {
		srv.Command = wrapped.Config.Command
		srv.URL = wrapped.Config.URL
		srv.Args = wrapped.Config.Args
		if len(wrapped.Config.Env) > 0 {
			srv.Env = wrapped.Config.Env
		}
		return srv, true
	}
	return provision.MCPServer{}, false
}

type codexMCPBody struct {
	Command string            `json:"command,omitempty"`
	URL     string            `json:"url,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
}

// isMissing reports whether stderr indicates the entry doesn't exist
// (so the engine treats it as "needs install" rather than failing).
func isMissing(stderr string) bool {
	s := strings.ToLower(stderr)
	return strings.Contains(s, "not found") ||
		strings.Contains(s, "not installed") ||
		strings.Contains(s, "not configured") ||
		strings.Contains(s, "no mcp server") ||
		strings.Contains(s, "no such server")
}
