package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestWriteConfig_FullFormat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	allowSub := true
	cfg := &Config{
		Agents: map[string]AgentDef{
			"claude": {Binary: "claude"},
			"copilot": {Binary: "gh-copilot"},
		},
		MCP: &MCPConfig{
			Servers: map[string]MCPServer{
				"myserver": {Command: "npx", Args: []string{"--yes", "mcp-server"}},
			},
		},
		Contexts: map[string]Context{
			"work": {
				Agent:      "claude",
				Secret:     "work",
				Env:        map[string]string{"FOO": "bar"},
				MCPServers: []string{"myserver"},
			},
			"personal": {
				Agent: "copilot",
				Sandbox: &SandboxRef{Inline: &SandboxPolicy{
					Network:         &NetworkPolicy{Mode: "outbound"},
					AllowSubprocess: &allowSub,
				}},
			},
		},
		DefaultContext: "work",
	}

	if err := WriteConfigTo(cfg, path); err != nil {
		t.Fatalf("WriteConfigTo() error = %v", err)
	}

	// Read it back using Load (pass dir as configDir, empty projectDir)
	loaded, err := Load(dir, "")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Verify agents
	if len(loaded.Agents) != 2 {
		t.Errorf("expected 2 agents, got %d", len(loaded.Agents))
	}
	if loaded.Agents["claude"].Binary != "claude" {
		t.Errorf("agent claude binary = %q, want %q", loaded.Agents["claude"].Binary, "claude")
	}

	// Verify contexts
	if len(loaded.Contexts) != 2 {
		t.Errorf("expected 2 contexts, got %d", len(loaded.Contexts))
	}
	if loaded.Contexts["work"].Agent != "claude" {
		t.Errorf("context work agent = %q, want %q", loaded.Contexts["work"].Agent, "claude")
	}
	if loaded.Contexts["work"].Env["FOO"] != "bar" {
		t.Errorf("context work env FOO = %q, want %q", loaded.Contexts["work"].Env["FOO"], "bar")
	}

	// Verify default context
	if loaded.DefaultContext != "work" {
		t.Errorf("default_context = %q, want %q", loaded.DefaultContext, "work")
	}

	// Verify MCP
	if loaded.MCP == nil {
		t.Fatal("expected MCP to be non-nil")
	}
	if loaded.MCP.Servers["myserver"].Command != "npx" {
		t.Errorf("MCP server command = %q, want %q", loaded.MCP.Servers["myserver"].Command, "npx")
	}
}

func TestWriteConfig_MinimalFormat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	cfg := &Config{
		Agent:  "claude",
		Env:    map[string]string{"KEY": "value"},
		Secret: "secrets",
	}

	if err := WriteConfigTo(cfg, path); err != nil {
		t.Fatalf("WriteConfigTo() error = %v", err)
	}

	// Load normalizes minimal to full format, so read back raw to verify minimal fields
	raw, err := loadFile(path)
	if err != nil {
		t.Fatalf("loadFile() error = %v", err)
	}

	if raw.Agent != "claude" {
		t.Errorf("agent = %q, want %q", raw.Agent, "claude")
	}
	if raw.Env["KEY"] != "value" {
		t.Errorf("env KEY = %q, want %q", raw.Env["KEY"], "value")
	}
	if raw.Secret != "secrets" {
		t.Errorf("secret = %q, want %q", raw.Secret, "secrets")
	}
	if !raw.IsMinimal() {
		t.Error("expected config to be minimal (no agents/contexts)")
	}
}

func TestWriteConfig_AtomicOnError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("read-only directory test not reliable on Windows")
	}

	dir := t.TempDir()
	original := filepath.Join(dir, "config.yaml")

	// Write an original config
	origCfg := &Config{Agent: "original"}
	if err := WriteConfigTo(origCfg, original); err != nil {
		t.Fatalf("initial WriteConfigTo() error = %v", err)
	}

	// Read original content
	origData, err := os.ReadFile(original)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	// Make the directory read-only so the .tmp file cannot be created
	readOnlyDir := filepath.Join(dir, "readonly")
	if err := os.MkdirAll(readOnlyDir, 0755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	readOnlyPath := filepath.Join(readOnlyDir, "config.yaml")

	// Write an original file in the read-only dir first
	if err := os.WriteFile(readOnlyPath, origData, 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	// Now make directory read-only
	if err := os.Chmod(readOnlyDir, 0555); err != nil {
		t.Fatalf("Chmod() error = %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(readOnlyDir, 0755) })

	// Try to write a new config — should fail
	newCfg := &Config{Agent: "modified"}
	err = WriteConfigTo(newCfg, readOnlyPath)
	if err == nil {
		t.Fatal("expected error writing to read-only directory, got nil")
	}

	// Verify original file is unchanged
	afterData, err := os.ReadFile(readOnlyPath)
	if err != nil {
		t.Fatalf("ReadFile() after failed write error = %v", err)
	}
	if string(afterData) != string(origData) {
		t.Errorf("original file was modified after failed write")
	}
}

func TestWriteConfig_CreatesDirectory(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, "a", "b", "c")
	path := filepath.Join(nested, "config.yaml")

	cfg := &Config{Agent: "claude"}
	if err := WriteConfigTo(cfg, path); err != nil {
		t.Fatalf("WriteConfigTo() error = %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected file to exist at %s, got error: %v", path, err)
	}

	// Verify content
	raw, err := loadFile(path)
	if err != nil {
		t.Fatalf("loadFile() error = %v", err)
	}
	if raw.Agent != "claude" {
		t.Errorf("agent = %q, want %q", raw.Agent, "claude")
	}
}

func TestWriteConfig_Roundtrip(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, "config")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	// Write an initial config file manually
	initial := `agents:
  claude:
    binary: claude
  copilot:
    binary: gh-copilot
contexts:
  work:
    agent: claude
    env:
      API_KEY: secret123
  personal:
    agent: copilot
    mcp_servers:
      - myserver
default_context: work
mcp:
  servers:
    myserver:
      command: npx
      args:
        - "--yes"
        - mcp-server
`
	configPath := filepath.Join(configDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(initial), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	// Load
	loaded, err := Load(configDir, "")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Write back
	if err := WriteConfigTo(loaded, configPath); err != nil {
		t.Fatalf("WriteConfigTo() error = %v", err)
	}

	// Load again
	reloaded, err := Load(configDir, "")
	if err != nil {
		t.Fatalf("second Load() error = %v", err)
	}

	// Compare key fields
	if len(reloaded.Agents) != len(loaded.Agents) {
		t.Errorf("agents count: got %d, want %d", len(reloaded.Agents), len(loaded.Agents))
	}
	if len(reloaded.Contexts) != len(loaded.Contexts) {
		t.Errorf("contexts count: got %d, want %d", len(reloaded.Contexts), len(loaded.Contexts))
	}
	if reloaded.DefaultContext != loaded.DefaultContext {
		t.Errorf("default_context: got %q, want %q", reloaded.DefaultContext, loaded.DefaultContext)
	}
	if reloaded.Contexts["work"].Env["API_KEY"] != "secret123" {
		t.Errorf("context work env API_KEY = %q, want %q", reloaded.Contexts["work"].Env["API_KEY"], "secret123")
	}
	if reloaded.Contexts["personal"].MCPServers[0] != "myserver" {
		t.Errorf("context personal mcp_servers[0] = %q, want %q", reloaded.Contexts["personal"].MCPServers[0], "myserver")
	}
}
