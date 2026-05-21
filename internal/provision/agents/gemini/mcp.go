package gemini

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/jskswamy/aide/internal/provision"
)

// InstalledMCPServers parses `gemini mcp list` output and returns the
// subset matching names. Gemini has no `mcp get` subcommand, so we
// list once and filter rather than issuing one shell-out per name.
//
// Gemini's list output aggregates user-scope and project-scope
// without a per-line scope discriminator. Aide always installs with
// `--scope user`, so any name we put there will be present when we
// query — entries from project scope that happen to share a name are
// rare in practice. Removing them goes through `gemini mcp remove
// <name> -s user`, which only deletes the user-scope entry.
//
// Binary missing → (empty, nil), matching the InstalledPlugins
// convention.
func (d *Driver) InstalledMCPServers(ctx provision.Context, names []string) (map[string]provision.MCPServer, error) {
	stdout, stderr, code, err := d.runner.Run(context.Background(), ctx.Env, "gemini", "mcp", "list")
	if err != nil {
		return map[string]provision.MCPServer{}, nil
	}
	if code != 0 {
		return nil, fmt.Errorf("gemini mcp list: exit %d: %s", code, strings.TrimSpace(stderr))
	}
	parsed := parseGeminiMCPList(stdout)
	want := make(map[string]struct{}, len(names))
	for _, n := range names {
		want[n] = struct{}{}
	}
	out := map[string]provision.MCPServer{}
	for name, srv := range parsed {
		if _, ok := want[name]; ok {
			out[name] = srv
		}
	}
	return out, nil
}

// InstallMCPServer runs `gemini mcp add` with `--scope user`. HTTP/SSE
// pass `--transport <type> <name> <url>`; stdio passes `<name>
// <command> [args...]` plus `-e K=V` for each env entry. We pre-remove
// to make re-install idempotent (gemini errors when a name in the same
// scope already exists).
func (d *Driver) InstallMCPServer(ctx provision.Context, s provision.MCPServer) error {
	if s.URL == "" && s.Command == "" {
		return fmt.Errorf("server %q has neither URL nor Command", s.Key)
	}
	// Idempotency: tolerate the "no server found" stderr.
	_, _, _, _ = d.runner.Run(context.Background(), ctx.Env, "gemini", "mcp", "remove", s.Key, "-s", "user")

	args := []string{"mcp", "add", "--scope", "user"}
	if s.URL != "" && s.Command == "" {
		// Aide doesn't carry a transport field, so HTTP is the safe
		// default for URL servers. Users who need SSE today should
		// add via `gemini mcp add --transport sse` directly until
		// MCPServer.Type is added.
		args = append(args, "--transport", "http", s.Key, s.URL)
	} else {
		// stdio: -e flags before the positional command so gemini's
		// argv parser doesn't grab them as command args.
		for k, v := range s.Env {
			args = append(args, "-e", k+"="+v)
		}
		args = append(args, "--transport", "stdio", s.Key, s.Command)
		args = append(args, s.Args...)
	}
	return provision.RunCLI(context.Background(), d.runner, ctx.Env,
		"gemini mcp add "+s.Key,
		"gemini", args)
}

// UninstallMCPServer runs `gemini mcp remove <name> -s user`. Gemini
// uses the phrase "not found" in its stderr for missing entries,
// which DefaultTolerateStderr already covers.
func (d *Driver) UninstallMCPServer(ctx provision.Context, name string) error {
	return provision.RunCLI(context.Background(), d.runner, ctx.Env,
		"gemini mcp remove "+name,
		"gemini", []string{"mcp", "remove", name, "-s", "user"},
		provision.DefaultTolerateStderr...)
}

// geminiListLine matches one configured-server line from `gemini mcp
// list`. Examples (verified 2026-05-21 against gemini-cli 0.41.2):
//
//	✗ aide-probe: http://127.0.0.1:9991 (http) - Disconnected
//	✗ aide-stdio-probe: node script.js arg1 (stdio) - Disconnected
//	✓ ok-server: foo (stdio) - Ready
//
// Capture groups: 1=name, 2=command-or-url + args, 3=transport.
var geminiListLine = regexp.MustCompile(`^[✗✓✕✔] (\S+): (.+) \((stdio|http|sse)\) - .+$`)

// parseGeminiMCPList extracts entries from `gemini mcp list` output.
// Env vars are intentionally not parsed — list output omits them and
// settings.json access would defeat the CLI-driven contract. If
// aide-managed entries declare env, mcpEqual may flag spurious
// updates; users will see one extra re-install per sync, no data
// loss. Promote env to the list output upstream to fix cleanly.
func parseGeminiMCPList(out string) map[string]provision.MCPServer {
	servers := map[string]provision.MCPServer{}
	for _, line := range strings.Split(out, "\n") {
		m := geminiListLine.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			continue
		}
		name, body, transport := m[1], m[2], m[3]
		srv := provision.MCPServer{Key: name}
		switch transport {
		case "http", "sse":
			srv.URL = strings.TrimSpace(body)
		case "stdio":
			fields := strings.Fields(body)
			if len(fields) == 0 {
				continue
			}
			srv.Command = fields[0]
			if len(fields) > 1 {
				srv.Args = fields[1:]
			}
		}
		servers[name] = srv
	}
	return servers
}
