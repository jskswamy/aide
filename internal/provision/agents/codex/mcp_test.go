package codex_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/jskswamy/aide/internal/provision"
	"github.com/jskswamy/aide/internal/provision/agents/codex"
)

type scriptedRunner struct {
	responses map[string]scriptedResponse
	calls     [][]string
}

type scriptedResponse struct {
	stdout string
	stderr string
	code   int
	err    error
}

func (r *scriptedRunner) Run(_ context.Context, _ map[string]string, name string, args ...string) (string, string, int, error) {
	call := append([]string{name}, args...)
	r.calls = append(r.calls, call)
	key := name
	for _, a := range args {
		key += " " + a
	}
	if resp, ok := r.responses[key]; ok {
		return resp.stdout, resp.stderr, resp.code, resp.err
	}
	if resp, ok := r.responses[""]; ok {
		return resp.stdout, resp.stderr, resp.code, resp.err
	}
	return "", "", 0, nil
}

// TestCodexInstalledMCPServersHTTPDirectBody covers the simplest
// expected `codex mcp get <name> --json` shape: a flat body whose
// keys mirror the TOML schema aide already understands.
func TestCodexInstalledMCPServersHTTPDirectBody(t *testing.T) {
	r := &scriptedRunner{responses: map[string]scriptedResponse{
		"codex mcp get 1mcp --json": {stdout: `{"url":"http://127.0.0.1:3050/mcp"}`, code: 0},
	}}
	d := codex.New(r)
	got, err := d.InstalledMCPServers(provision.Context{}, []string{"1mcp"})
	if err != nil {
		t.Fatal(err)
	}
	if got["1mcp"].URL != "http://127.0.0.1:3050/mcp" {
		t.Errorf("URL = %q", got["1mcp"].URL)
	}
}

// TestCodexInstalledMCPServersStdioWrappedBody covers a wrapped shape
// {"name":..,"config":{...}} as a defensive fallback; if codex starts
// emitting envelopes, the parser already copes.
func TestCodexInstalledMCPServersStdioWrappedBody(t *testing.T) {
	r := &scriptedRunner{responses: map[string]scriptedResponse{
		"codex mcp get pg --json": {stdout: `{"name":"pg","config":{"command":"postgres-mcp","args":["--port","5432"],"env":{"PG":"x"}}}`, code: 0},
	}}
	d := codex.New(r)
	got, err := d.InstalledMCPServers(provision.Context{}, []string{"pg"})
	if err != nil {
		t.Fatal(err)
	}
	entry := got["pg"]
	if entry.Command != "postgres-mcp" {
		t.Errorf("Command = %q", entry.Command)
	}
	wantArgs := []string{"--port", "5432"}
	if !reflect.DeepEqual(entry.Args, wantArgs) {
		t.Errorf("Args = %v, want %v", entry.Args, wantArgs)
	}
	if entry.Env["PG"] != "x" {
		t.Errorf("Env = %v", entry.Env)
	}
}

// TestCodexInstalledMCPServersMissingExcluded confirms exit-non-zero
// names omit from the result rather than erroring (so the engine
// treats them as "needs install").
func TestCodexInstalledMCPServersMissingExcluded(t *testing.T) {
	r := &scriptedRunner{responses: map[string]scriptedResponse{
		"codex mcp get gone --json": {stderr: "no MCP server found with name gone", code: 1},
	}}
	d := codex.New(r)
	got, _ := d.InstalledMCPServers(provision.Context{}, []string{"gone"})
	if _, present := got["gone"]; present {
		t.Errorf("missing entry should omit, got %v", got)
	}
}

// TestCodexInstallMCPServerHTTPCmdline pins the HTTP add: pre-remove
// then `codex mcp add <name> --url <url>`.
func TestCodexInstallMCPServerHTTPCmdline(t *testing.T) {
	r := &scriptedRunner{}
	d := codex.New(r)
	err := d.InstallMCPServer(provision.Context{}, provision.MCPServer{
		Key: "1mcp", URL: "http://127.0.0.1:3050/mcp",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.calls) != 2 {
		t.Fatalf("expected 2 calls (remove + add), got %d: %v", len(r.calls), r.calls)
	}
	wantAdd := []string{"codex", "mcp", "add", "1mcp", "--url", "http://127.0.0.1:3050/mcp"}
	if !reflect.DeepEqual(r.calls[1], wantAdd) {
		t.Errorf("add call = %v, want %v", r.calls[1], wantAdd)
	}
}

// TestCodexInstallMCPServerStdioCmdline checks stdio: env flags
// before the `--` separator, then command and args.
func TestCodexInstallMCPServerStdioCmdline(t *testing.T) {
	r := &scriptedRunner{}
	d := codex.New(r)
	err := d.InstallMCPServer(provision.Context{}, provision.MCPServer{
		Key: "pg", Command: "postgres-mcp", Args: []string{"--port", "5432"},
		Env: map[string]string{"PG": "x"},
	})
	if err != nil {
		t.Fatal(err)
	}
	add := r.calls[1]
	// Find the -- separator; everything after is command + args.
	sep := -1
	for i, a := range add {
		if a == "--" {
			sep = i
			break
		}
	}
	if sep == -1 || sep+3 > len(add) {
		t.Fatalf("missing -- separator: %v", add)
	}
	wantTail := []string{"--", "postgres-mcp", "--port", "5432"}
	if !reflect.DeepEqual(add[sep:], wantTail) {
		t.Errorf("post-separator = %v, want %v", add[sep:], wantTail)
	}
	// Verify --env PG=x appears before the separator.
	var sawEnv bool
	for i := 0; i < sep; i++ {
		if add[i] == "--env" && i+1 < sep && add[i+1] == "PG=x" {
			sawEnv = true
		}
	}
	if !sawEnv {
		t.Errorf("--env PG=x missing before separator: %v", add[:sep])
	}
}
