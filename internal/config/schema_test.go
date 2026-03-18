package config_test

import (
	"testing"

	"github.com/jskswamy/aide/internal/config"
	"gopkg.in/yaml.v3"
)

func TestConfig_IsMinimal_True(t *testing.T) {
	input := `
agent: claude
env:
  ANTHROPIC_API_KEY: "sk-test"
secrets_file: personal.enc.yaml
mcp_servers: [git, context7]
`
	var cfg config.Config
	if err := yaml.Unmarshal([]byte(input), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !cfg.IsMinimal() {
		t.Error("expected IsMinimal() to return true for flat config")
	}
	if cfg.Agent != "claude" {
		t.Errorf("Agent = %q, want %q", cfg.Agent, "claude")
	}
	if cfg.Env["ANTHROPIC_API_KEY"] != "sk-test" {
		t.Errorf("Env[ANTHROPIC_API_KEY] = %q, want %q", cfg.Env["ANTHROPIC_API_KEY"], "sk-test")
	}
	if cfg.SecretsFile != "personal.enc.yaml" {
		t.Errorf("SecretsFile = %q, want %q", cfg.SecretsFile, "personal.enc.yaml")
	}
	if len(cfg.MCPServers) != 2 || cfg.MCPServers[0] != "git" || cfg.MCPServers[1] != "context7" {
		t.Errorf("MCPServers = %v, want [git context7]", cfg.MCPServers)
	}
}

func TestConfig_IsMinimal_False(t *testing.T) {
	input := `
agents:
  claude:
    binary: claude
contexts:
  personal:
    agent: claude
    secrets_file: personal.enc.yaml
    env:
      ANTHROPIC_API_KEY: "sk-test"
    mcp_servers: [git, context7]
default_context: personal
`
	var cfg config.Config
	if err := yaml.Unmarshal([]byte(input), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if cfg.IsMinimal() {
		t.Error("expected IsMinimal() to return false for full config")
	}
	if len(cfg.Agents) != 1 {
		t.Errorf("len(Agents) = %d, want 1", len(cfg.Agents))
	}
	if cfg.Agents["claude"].Binary != "claude" {
		t.Errorf("Agents[claude].Binary = %q, want %q", cfg.Agents["claude"].Binary, "claude")
	}
	if len(cfg.Contexts) != 1 {
		t.Errorf("len(Contexts) = %d, want 1", len(cfg.Contexts))
	}
	ctx := cfg.Contexts["personal"]
	if ctx.Agent != "claude" {
		t.Errorf("Context.Agent = %q, want %q", ctx.Agent, "claude")
	}
	if ctx.SecretsFile != "personal.enc.yaml" {
		t.Errorf("Context.SecretsFile = %q, want %q", ctx.SecretsFile, "personal.enc.yaml")
	}
	if cfg.DefaultContext != "personal" {
		t.Errorf("DefaultContext = %q, want %q", cfg.DefaultContext, "personal")
	}
}

func TestConfig_UnmarshalMinimal_RoundTrip(t *testing.T) {
	original := config.Config{
		Agent:       "claude",
		Env:         map[string]string{"KEY": "val"},
		SecretsFile: "test.enc.yaml",
		MCPServers:  []string{"git"},
	}
	data, err := yaml.Marshal(&original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded config.Config
	if err := yaml.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Agent != original.Agent {
		t.Errorf("Agent = %q, want %q", decoded.Agent, original.Agent)
	}
	if decoded.Env["KEY"] != "val" {
		t.Errorf("Env[KEY] = %q, want %q", decoded.Env["KEY"], "val")
	}
	if decoded.SecretsFile != original.SecretsFile {
		t.Errorf("SecretsFile = %q, want %q", decoded.SecretsFile, original.SecretsFile)
	}
	if len(decoded.MCPServers) != 1 || decoded.MCPServers[0] != "git" {
		t.Errorf("MCPServers = %v, want [git]", decoded.MCPServers)
	}
}

func TestConfig_UnmarshalFull_RoundTrip(t *testing.T) {
	allowSub := true
	cleanEnv := false
	original := config.Config{
		Agents: map[string]config.AgentDef{
			"claude": {Binary: "claude"},
		},
		MCP: &config.MCPConfig{
			Aggregator: &config.MCPAggregator{
				Command: "1mcp",
				URL:     "http://localhost:8080",
			},
			Servers: map[string]config.MCPServer{
				"git": {
					Command: "git-mcp",
					Args:    []string{"--verbose"},
					Env:     map[string]string{"GIT_DIR": "/tmp"},
				},
			},
		},
		Contexts: map[string]config.Context{
			"work": {
				Agent:       "claude",
				SecretsFile: "work.enc.yaml",
				Env:         map[string]string{"ORG": "acme"},
				MCPServers:  []string{"git"},
				MCPServerOverrides: map[string]config.MCPServer{
					"git": {Args: []string{"--quiet"}},
				},
				Match: []config.MatchRule{
					{Remote: "github.com/acme/*"},
				},
				Sandbox: &config.SandboxPolicy{
					Writable:        []string{"/tmp"},
					Readable:        []string{"/etc"},
					Denied:          []string{"/root"},
					Network:         "outbound",
					AllowSubprocess: &allowSub,
					CleanEnv:        &cleanEnv,
				},
			},
		},
		DefaultContext: "work",
	}

	data, err := yaml.Marshal(&original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded config.Config
	if err := yaml.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.Agents["claude"].Binary != "claude" {
		t.Errorf("Agents[claude].Binary = %q, want %q", decoded.Agents["claude"].Binary, "claude")
	}
	if decoded.MCP == nil {
		t.Fatal("MCP is nil")
	}
	if decoded.MCP.Aggregator.Command != "1mcp" {
		t.Errorf("MCP.Aggregator.Command = %q, want %q", decoded.MCP.Aggregator.Command, "1mcp")
	}
	if decoded.MCP.Servers["git"].Command != "git-mcp" {
		t.Errorf("MCP.Servers[git].Command = %q, want %q", decoded.MCP.Servers["git"].Command, "git-mcp")
	}

	ctx := decoded.Contexts["work"]
	if ctx.Agent != "claude" {
		t.Errorf("Context.Agent = %q, want %q", ctx.Agent, "claude")
	}
	if ctx.Sandbox == nil {
		t.Fatal("Sandbox is nil")
	}
	if ctx.Sandbox.Network != "outbound" {
		t.Errorf("Sandbox.Network = %q, want %q", ctx.Sandbox.Network, "outbound")
	}
	if ctx.Sandbox.AllowSubprocess == nil || *ctx.Sandbox.AllowSubprocess != true {
		t.Errorf("Sandbox.AllowSubprocess = %v, want true", ctx.Sandbox.AllowSubprocess)
	}
	if ctx.Sandbox.CleanEnv == nil || *ctx.Sandbox.CleanEnv != false {
		t.Errorf("Sandbox.CleanEnv = %v, want false", ctx.Sandbox.CleanEnv)
	}
	if len(ctx.MCPServerOverrides) != 1 {
		t.Errorf("len(MCPServerOverrides) = %d, want 1", len(ctx.MCPServerOverrides))
	}
	if decoded.DefaultContext != "work" {
		t.Errorf("DefaultContext = %q, want %q", decoded.DefaultContext, "work")
	}
}

func TestConfig_EmptyYAML_IsMinimal(t *testing.T) {
	var cfg config.Config
	if err := yaml.Unmarshal([]byte(""), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !cfg.IsMinimal() {
		t.Error("expected IsMinimal() to return true for empty YAML")
	}
}

func TestMatchRule_RemoteOnly(t *testing.T) {
	input := `remote: "github.com/org/*"`
	var rule config.MatchRule
	if err := yaml.Unmarshal([]byte(input), &rule); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if rule.Remote != "github.com/org/*" {
		t.Errorf("Remote = %q, want %q", rule.Remote, "github.com/org/*")
	}
	if rule.Path != "" {
		t.Errorf("Path = %q, want empty", rule.Path)
	}
}

func TestMatchRule_PathOnly(t *testing.T) {
	input := `path: "~/work/*"`
	var rule config.MatchRule
	if err := yaml.Unmarshal([]byte(input), &rule); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if rule.Path != "~/work/*" {
		t.Errorf("Path = %q, want %q", rule.Path, "~/work/*")
	}
	if rule.Remote != "" {
		t.Errorf("Remote = %q, want empty", rule.Remote)
	}
}

func TestSandboxPolicy_Defaults(t *testing.T) {
	input := `{}`
	var sp config.SandboxPolicy
	if err := yaml.Unmarshal([]byte(input), &sp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(sp.Writable) != 0 {
		t.Errorf("Writable = %v, want empty", sp.Writable)
	}
	if len(sp.Readable) != 0 {
		t.Errorf("Readable = %v, want empty", sp.Readable)
	}
	if len(sp.Denied) != 0 {
		t.Errorf("Denied = %v, want empty", sp.Denied)
	}
	if sp.Network != "" {
		t.Errorf("Network = %q, want empty", sp.Network)
	}
	if sp.AllowSubprocess != nil {
		t.Errorf("AllowSubprocess = %v, want nil", sp.AllowSubprocess)
	}
	if sp.CleanEnv != nil {
		t.Errorf("CleanEnv = %v, want nil", sp.CleanEnv)
	}
}

func TestProjectOverride_Unmarshal(t *testing.T) {
	input := `
agent: codex
env:
  PROJECT_KEY: "proj-val"
secrets_file: project.enc.yaml
mcp_servers: [context7]
sandbox:
  writable: ["/tmp/project"]
  network: none
`
	var po config.ProjectOverride
	if err := yaml.Unmarshal([]byte(input), &po); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if po.Agent != "codex" {
		t.Errorf("Agent = %q, want %q", po.Agent, "codex")
	}
	if po.Env["PROJECT_KEY"] != "proj-val" {
		t.Errorf("Env[PROJECT_KEY] = %q, want %q", po.Env["PROJECT_KEY"], "proj-val")
	}
	if po.SecretsFile != "project.enc.yaml" {
		t.Errorf("SecretsFile = %q, want %q", po.SecretsFile, "project.enc.yaml")
	}
	if len(po.MCPServers) != 1 || po.MCPServers[0] != "context7" {
		t.Errorf("MCPServers = %v, want [context7]", po.MCPServers)
	}
	if po.Sandbox == nil {
		t.Fatal("Sandbox is nil")
	}
	if po.Sandbox.Network != "none" {
		t.Errorf("Sandbox.Network = %q, want %q", po.Sandbox.Network, "none")
	}
}
