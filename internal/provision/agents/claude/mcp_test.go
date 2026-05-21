package claude_test

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/jskswamy/aide/internal/provision"
	"github.com/jskswamy/aide/internal/provision/agents/claude"
)

// scriptedRunner returns canned stdout/stderr/code per command. Keys
// are the joined argv ("claude mcp get foo"); a "" key is the default.
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

// TestClaudeInstalledMCPServersUserScopeOnly pins the filter: only
// user-scope entries flow through. Local-scope / project-scope /
// not-found are excluded so aide doesn't try to manage what it
// doesn't own.
func TestClaudeInstalledMCPServersUserScopeOnly(t *testing.T) {
	r := &scriptedRunner{responses: map[string]scriptedResponse{
		"claude mcp get user-http": {stdout: "user-http:\n  Scope: User config (available in all your projects)\n  Type: http\n  URL: http://127.0.0.1:9000\n", code: 0},
		"claude mcp get local-only": {stdout: "local-only:\n  Scope: Local config (private to you in this project)\n  Type: stdio\n  Command: foo\n", code: 0},
		"claude mcp get missing":   {stderr: `No MCP server found with name: "missing"`, code: 1},
	}}
	d := claude.New(r)
	got, err := d.InstalledMCPServers(provision.Context{}, []string{"user-http", "local-only", "missing"})
	if err != nil {
		t.Fatal(err)
	}
	if _, present := got["user-http"]; !present {
		t.Errorf("user-scope entry missing from result: %+v", got)
	}
	if _, present := got["local-only"]; present {
		t.Errorf("local-scope entry must be filtered out: %+v", got)
	}
	if _, present := got["missing"]; present {
		t.Errorf("missing entry must omit (not error): %+v", got)
	}
	if got["user-http"].URL != "http://127.0.0.1:9000" {
		t.Errorf("URL not parsed: %+v", got["user-http"])
	}
}

// TestClaudeInstalledMCPServersStdioRoundTrip pins the stdio parser:
// command, args, env all populate.
func TestClaudeInstalledMCPServersStdioRoundTrip(t *testing.T) {
	out := "my-pg:\n" +
		"  Scope: User config (available in all your projects)\n" +
		"  Type: stdio\n" +
		"  Command: postgres-mcp\n" +
		"  Args: --port 5432 --db app\n" +
		"  Environment:\n" +
		"    PGPASSWORD=secret\n" +
		"    PGUSER=app\n"
	r := &scriptedRunner{responses: map[string]scriptedResponse{
		"claude mcp get my-pg": {stdout: out, code: 0},
	}}
	d := claude.New(r)
	got, err := d.InstalledMCPServers(provision.Context{}, []string{"my-pg"})
	if err != nil {
		t.Fatal(err)
	}
	entry := got["my-pg"]
	if entry.Command != "postgres-mcp" {
		t.Errorf("Command = %q", entry.Command)
	}
	wantArgs := []string{"--port", "5432", "--db", "app"}
	if !reflect.DeepEqual(entry.Args, wantArgs) {
		t.Errorf("Args = %v, want %v", entry.Args, wantArgs)
	}
	if entry.Env["PGPASSWORD"] != "secret" || entry.Env["PGUSER"] != "app" {
		t.Errorf("Env = %v", entry.Env)
	}
}

// TestClaudeInstallMCPServerHTTPCallsAddJSON verifies the install
// path: pre-remove (tolerated), then `mcp add-json --scope user`.
func TestClaudeInstallMCPServerHTTPCallsAddJSON(t *testing.T) {
	r := &scriptedRunner{}
	d := claude.New(r)
	err := d.InstallMCPServer(provision.Context{}, provision.MCPServer{
		Key: "1mcp", URL: "http://127.0.0.1:3050/mcp",
	})
	if err != nil {
		t.Fatal(err)
	}
	// Expect exactly two calls: remove (idempotency) then add-json.
	if len(r.calls) != 2 {
		t.Fatalf("expected 2 calls, got %d: %v", len(r.calls), r.calls)
	}
	wantRemove := []string{"claude", "mcp", "remove", "1mcp", "-s", "user"}
	if !reflect.DeepEqual(r.calls[0], wantRemove) {
		t.Errorf("pre-remove call = %v, want %v", r.calls[0], wantRemove)
	}
	addCall := r.calls[1]
	if len(addCall) < 6 || addCall[0] != "claude" || addCall[1] != "mcp" || addCall[2] != "add-json" {
		t.Fatalf("add-json call shape unexpected: %v", addCall)
	}
	// Last arg is the JSON body — must include type:http + url.
	var body map[string]any
	if err := json.Unmarshal([]byte(addCall[len(addCall)-1]), &body); err != nil {
		t.Fatalf("add-json body not valid JSON: %v (%q)", err, addCall[len(addCall)-1])
	}
	if body["type"] != "http" || body["url"] != "http://127.0.0.1:3050/mcp" {
		t.Errorf("add-json body = %v, want type=http url=<the url>", body)
	}
}

// TestClaudeInstallMCPServerStdioJSONShape verifies stdio entries get
// the right add-json body (command + args + env).
func TestClaudeInstallMCPServerStdioJSONShape(t *testing.T) {
	r := &scriptedRunner{}
	d := claude.New(r)
	err := d.InstallMCPServer(provision.Context{}, provision.MCPServer{
		Key: "pg", Command: "postgres-mcp", Args: []string{"--port", "5432"},
		Env: map[string]string{"PGPASSWORD": "x"},
	})
	if err != nil {
		t.Fatal(err)
	}
	addCall := r.calls[len(r.calls)-1]
	var body map[string]any
	if err := json.Unmarshal([]byte(addCall[len(addCall)-1]), &body); err != nil {
		t.Fatal(err)
	}
	if body["type"] != "stdio" || body["command"] != "postgres-mcp" {
		t.Errorf("body = %v", body)
	}
	args, _ := body["args"].([]any)
	if len(args) != 2 || args[0] != "--port" || args[1] != "5432" {
		t.Errorf("args = %v", args)
	}
	env, _ := body["env"].(map[string]any)
	if env["PGPASSWORD"] != "x" {
		t.Errorf("env = %v", env)
	}
}

// TestClaudeUninstallMCPServerTolerated checks rollback safety: a
// missing entry doesn't error.
func TestClaudeUninstallMCPServerTolerated(t *testing.T) {
	r := &scriptedRunner{responses: map[string]scriptedResponse{
		"": {code: 1, stderr: "No user-scoped MCP server found with name: foo"},
	}}
	d := claude.New(r)
	if err := d.UninstallMCPServer(provision.Context{}, "foo"); err != nil {
		t.Errorf("missing server uninstall should be tolerated: %v", err)
	}
}
