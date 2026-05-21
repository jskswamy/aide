package gemini_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/jskswamy/aide/internal/provision"
	"github.com/jskswamy/aide/internal/provision/agents/gemini"
)

// scriptedRunner mirrors the claude_test.go helper — keyed canned
// responses by joined argv ("gemini mcp list"). "" is the fallback.
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

// TestGeminiInstalledMCPServersParsesHTTP pins the http branch of
// the list parser against real `gemini mcp list` output.
func TestGeminiInstalledMCPServersParsesHTTP(t *testing.T) {
	out := "Configured MCP servers:\n\n" +
		"✗ 1mcp: http://127.0.0.1:3050/mcp (http) - Disconnected\n"
	r := &scriptedRunner{responses: map[string]scriptedResponse{
		"gemini mcp list": {stdout: out, code: 0},
	}}
	d := gemini.New(r)
	got, err := d.InstalledMCPServers(provision.Context{}, []string{"1mcp"})
	if err != nil {
		t.Fatal(err)
	}
	if got["1mcp"].URL != "http://127.0.0.1:3050/mcp" {
		t.Errorf("URL = %q", got["1mcp"].URL)
	}
}

// TestGeminiInstalledMCPServersParsesStdio pins the stdio branch:
// first field becomes Command, remainder becomes Args.
func TestGeminiInstalledMCPServersParsesStdio(t *testing.T) {
	out := "Configured MCP servers:\n\n" +
		"✗ pg: postgres-mcp --port 5432 (stdio) - Disconnected\n"
	r := &scriptedRunner{responses: map[string]scriptedResponse{
		"gemini mcp list": {stdout: out, code: 0},
	}}
	d := gemini.New(r)
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
}

// TestGeminiInstalledMCPServersFiltersByNames confirms entries
// outside the bounded name list are dropped.
func TestGeminiInstalledMCPServersFiltersByNames(t *testing.T) {
	out := "Configured MCP servers:\n\n" +
		"✗ a: http://a (http) - Disconnected\n" +
		"✗ b: http://b (http) - Disconnected\n"
	r := &scriptedRunner{responses: map[string]scriptedResponse{
		"gemini mcp list": {stdout: out, code: 0},
	}}
	d := gemini.New(r)
	got, _ := d.InstalledMCPServers(provision.Context{}, []string{"a"})
	if _, present := got["b"]; present {
		t.Error("b should be filtered out: not in names")
	}
	if _, present := got["a"]; !present {
		t.Error("a should be returned")
	}
}

// TestGeminiInstallMCPServerHTTPCmdline checks the install path for
// HTTP: pre-remove then `gemini mcp add --scope user --transport http
// <name> <url>`.
func TestGeminiInstallMCPServerHTTPCmdline(t *testing.T) {
	r := &scriptedRunner{}
	d := gemini.New(r)
	err := d.InstallMCPServer(provision.Context{}, provision.MCPServer{
		Key: "1mcp", URL: "http://127.0.0.1:3050/mcp",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.calls) != 2 {
		t.Fatalf("expected 2 calls (pre-remove + add), got %d: %v", len(r.calls), r.calls)
	}
	wantAdd := []string{"gemini", "mcp", "add", "--scope", "user", "--transport", "http", "1mcp", "http://127.0.0.1:3050/mcp"}
	if !reflect.DeepEqual(r.calls[1], wantAdd) {
		t.Errorf("add call = %v, want %v", r.calls[1], wantAdd)
	}
}

// TestGeminiInstallMCPServerStdioWithEnv checks env flags precede the
// positional command so gemini's parser doesn't grab them as args.
func TestGeminiInstallMCPServerStdioWithEnv(t *testing.T) {
	r := &scriptedRunner{}
	d := gemini.New(r)
	err := d.InstallMCPServer(provision.Context{}, provision.MCPServer{
		Key: "pg", Command: "postgres-mcp", Args: []string{"--port", "5432"},
		Env: map[string]string{"PGPASSWORD": "x"},
	})
	if err != nil {
		t.Fatal(err)
	}
	addCall := r.calls[1]
	// Verify positional tail: --transport stdio <name> <command> <args...>
	// after the env flags. Env-flag order is non-deterministic (map),
	// so we anchor on the --transport marker.
	idx := -1
	for i, a := range addCall {
		if a == "--transport" {
			idx = i
			break
		}
	}
	if idx == -1 || idx+5 > len(addCall) {
		t.Fatalf("--transport block missing from %v", addCall)
	}
	want := []string{"--transport", "stdio", "pg", "postgres-mcp", "--port", "5432"}
	if !reflect.DeepEqual(addCall[idx:idx+6], want) {
		t.Errorf("transport block = %v, want %v", addCall[idx:idx+6], want)
	}
	// Verify -e flag is present.
	var sawEnv bool
	for i, a := range addCall {
		if a == "-e" && i+1 < len(addCall) && addCall[i+1] == "PGPASSWORD=x" {
			sawEnv = true
		}
	}
	if !sawEnv {
		t.Errorf("-e PGPASSWORD=x missing from %v", addCall)
	}
}
