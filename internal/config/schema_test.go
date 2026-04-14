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
secret: personal
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
	if cfg.Secret != "personal" {
		t.Errorf("Secret = %q, want %q", cfg.Secret, "personal")
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
    secret: personal
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
	if ctx.Secret != "personal" {
		t.Errorf("Context.Secret = %q, want %q", ctx.Secret, "personal")
	}
	if cfg.DefaultContext != "personal" {
		t.Errorf("DefaultContext = %q, want %q", cfg.DefaultContext, "personal")
	}
}

func TestConfig_UnmarshalMinimal_RoundTrip(t *testing.T) {
	original := config.Config{
		Agent:      "claude",
		Env:        map[string]string{"KEY": "val"},
		Secret:     "test",
		MCPServers: []string{"git"},
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
	if decoded.Secret != original.Secret {
		t.Errorf("Secret = %q, want %q", decoded.Secret, original.Secret)
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
				Agent:  "claude",
				Secret: "work",
				Env:    map[string]string{"ORG": "acme"},
				MCPServers:  []string{"git"},
				MCPServerOverrides: map[string]config.MCPServer{
					"git": {Args: []string{"--quiet"}},
				},
				Match: []config.MatchRule{
					{Remote: "github.com/acme/*"},
				},
				Sandbox: &config.SandboxRef{Inline: &config.SandboxPolicy{
					Writable:        []string{"/tmp"},
					Readable:        []string{"/etc"},
					Denied:          []string{"/root"},
					Network:         &config.NetworkPolicy{Mode: "outbound"},
					AllowSubprocess: &allowSub,
					CleanEnv:        &cleanEnv,
				}},
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
	if ctx.Sandbox.Inline == nil {
		t.Fatal("Sandbox.Inline is nil")
	}
	if ctx.Sandbox.Inline.Network == nil || ctx.Sandbox.Inline.Network.Mode != "outbound" {
		t.Errorf("Sandbox.Inline.Network.Mode = %v, want %q", ctx.Sandbox.Inline.Network, "outbound")
	}
	if ctx.Sandbox.Inline.AllowSubprocess == nil || *ctx.Sandbox.Inline.AllowSubprocess != true {
		t.Errorf("Sandbox.Inline.AllowSubprocess = %v, want true", ctx.Sandbox.Inline.AllowSubprocess)
	}
	if ctx.Sandbox.Inline.CleanEnv == nil || *ctx.Sandbox.Inline.CleanEnv != false {
		t.Errorf("Sandbox.Inline.CleanEnv = %v, want false", ctx.Sandbox.Inline.CleanEnv)
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
	if sp.Network != nil {
		t.Errorf("Network = %v, want nil", sp.Network)
	}
	if sp.AllowSubprocess != nil {
		t.Errorf("AllowSubprocess = %v, want nil", sp.AllowSubprocess)
	}
	if sp.CleanEnv != nil {
		t.Errorf("CleanEnv = %v, want nil", sp.CleanEnv)
	}
}

func TestNetworkPolicy_UnmarshalString(t *testing.T) {
	input := `network: outbound`
	var sp config.SandboxPolicy
	if err := yaml.Unmarshal([]byte(input), &sp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if sp.Network == nil {
		t.Fatal("Network is nil")
	}
	if sp.Network.Mode != "outbound" {
		t.Errorf("Network.Mode = %q, want %q", sp.Network.Mode, "outbound")
	}
	if len(sp.Network.AllowPorts) != 0 {
		t.Errorf("Network.AllowPorts = %v, want empty", sp.Network.AllowPorts)
	}
	if len(sp.Network.DenyPorts) != 0 {
		t.Errorf("Network.DenyPorts = %v, want empty", sp.Network.DenyPorts)
	}
}

func TestNetworkPolicy_UnmarshalMap(t *testing.T) {
	input := `network:
  mode: outbound
  allow_ports: [443, 53]
`
	var sp config.SandboxPolicy
	if err := yaml.Unmarshal([]byte(input), &sp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if sp.Network == nil {
		t.Fatal("Network is nil")
	}
	if sp.Network.Mode != "outbound" {
		t.Errorf("Network.Mode = %q, want %q", sp.Network.Mode, "outbound")
	}
	if len(sp.Network.AllowPorts) != 2 || sp.Network.AllowPorts[0] != 443 || sp.Network.AllowPorts[1] != 53 {
		t.Errorf("Network.AllowPorts = %v, want [443 53]", sp.Network.AllowPorts)
	}
	if len(sp.Network.DenyPorts) != 0 {
		t.Errorf("Network.DenyPorts = %v, want empty", sp.Network.DenyPorts)
	}
}

func TestNetworkPolicy_UnmarshalString_AllModes(t *testing.T) {
	modes := []string{"none", "unrestricted", "outbound"}
	for _, mode := range modes {
		input := "network: " + mode
		var sp config.SandboxPolicy
		if err := yaml.Unmarshal([]byte(input), &sp); err != nil {
			t.Fatalf("unmarshal mode %q: %v", mode, err)
		}
		if sp.Network == nil {
			t.Fatalf("Network is nil for mode %q", mode)
		}
		if sp.Network.Mode != mode {
			t.Errorf("Network.Mode = %q, want %q", sp.Network.Mode, mode)
		}
	}
}

func TestSandboxPolicy_NetworkBackwardCompat(t *testing.T) {
	input := `
agent: claude
sandbox:
  writable: ["/tmp"]
  network: outbound
`
	var cfg config.Config
	if err := yaml.Unmarshal([]byte(input), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if cfg.Sandbox == nil {
		t.Fatal("Sandbox is nil")
	}
	if cfg.Sandbox.Network == nil {
		t.Fatal("Sandbox.Network is nil")
	}
	if cfg.Sandbox.Network.Mode != "outbound" {
		t.Errorf("Sandbox.Network.Mode = %q, want %q", cfg.Sandbox.Network.Mode, "outbound")
	}
}

func TestNetworkPolicy_UnmarshalMapWithDenyPorts(t *testing.T) {
	input := `network:
  mode: outbound
  allow_ports: [443, 53, 22]
  deny_ports: [8080]
`
	var sp config.SandboxPolicy
	if err := yaml.Unmarshal([]byte(input), &sp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if sp.Network == nil {
		t.Fatal("Network is nil")
	}
	if sp.Network.Mode != "outbound" {
		t.Errorf("Network.Mode = %q, want %q", sp.Network.Mode, "outbound")
	}
	if len(sp.Network.AllowPorts) != 3 {
		t.Errorf("Network.AllowPorts = %v, want [443 53 22]", sp.Network.AllowPorts)
	}
	if len(sp.Network.DenyPorts) != 1 || sp.Network.DenyPorts[0] != 8080 {
		t.Errorf("Network.DenyPorts = %v, want [8080]", sp.Network.DenyPorts)
	}
}

func TestProjectOverride_Unmarshal(t *testing.T) {
	input := `
agent: codex
env:
  PROJECT_KEY: "proj-val"
secret: project
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
	if po.Secret != "project" {
		t.Errorf("Secret = %q, want %q", po.Secret, "project")
	}
	if len(po.MCPServers) != 1 || po.MCPServers[0] != "context7" {
		t.Errorf("MCPServers = %v, want [context7]", po.MCPServers)
	}
	if po.Sandbox == nil {
		t.Fatal("Sandbox is nil")
	}
	if po.Sandbox.Network == nil || po.Sandbox.Network.Mode != "none" {
		t.Errorf("Sandbox.Network.Mode = %v, want %q", po.Sandbox.Network, "none")
	}
}

func TestPreferences_Unmarshal(t *testing.T) {
	input := `
preferences:
  show_info: true
  info_style: boxed
  info_detail: detailed
`
	var cfg config.Config
	if err := yaml.Unmarshal([]byte(input), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if cfg.Preferences == nil {
		t.Fatal("Preferences is nil")
	}
	if cfg.Preferences.ShowInfo == nil || !*cfg.Preferences.ShowInfo {
		t.Errorf("ShowInfo = %v, want true", cfg.Preferences.ShowInfo)
	}
	if cfg.Preferences.InfoStyle != "boxed" {
		t.Errorf("InfoStyle = %q, want %q", cfg.Preferences.InfoStyle, "boxed")
	}
	if cfg.Preferences.InfoDetail != "detailed" {
		t.Errorf("InfoDetail = %q, want %q", cfg.Preferences.InfoDetail, "detailed")
	}
}

func TestPreferences_Defaults(t *testing.T) {
	result := config.ResolvePreferences(nil, nil)
	if result.ShowInfo == nil || !*result.ShowInfo {
		t.Errorf("ShowInfo = %v, want true", result.ShowInfo)
	}
	if result.InfoStyle != "compact" {
		t.Errorf("InfoStyle = %q, want %q", result.InfoStyle, "compact")
	}
	if result.InfoDetail != "normal" {
		t.Errorf("InfoDetail = %q, want %q", result.InfoDetail, "normal")
	}
}

func TestPreferences_GlobalOverride(t *testing.T) {
	global := &config.Preferences{InfoStyle: "boxed"}
	result := config.ResolvePreferences(global, nil)
	if result.InfoStyle != "boxed" {
		t.Errorf("InfoStyle = %q, want %q", result.InfoStyle, "boxed")
	}
}

func TestPreferences_ProjectOverride(t *testing.T) {
	global := &config.Preferences{InfoStyle: "boxed"}
	project := &config.Preferences{InfoStyle: "clean"}
	result := config.ResolvePreferences(global, project)
	if result.InfoStyle != "clean" {
		t.Errorf("InfoStyle = %q, want %q", result.InfoStyle, "clean")
	}
}

func TestPreferences_PartialProjectOverride(t *testing.T) {
	f := false
	global := &config.Preferences{InfoStyle: "boxed", InfoDetail: "verbose"}
	project := &config.Preferences{ShowInfo: &f}
	result := config.ResolvePreferences(global, project)
	if result.ShowInfo == nil || *result.ShowInfo {
		t.Errorf("ShowInfo = %v, want false", result.ShowInfo)
	}
	if result.InfoStyle != "boxed" {
		t.Errorf("InfoStyle = %q, want %q", result.InfoStyle, "boxed")
	}
	if result.InfoDetail != "verbose" {
		t.Errorf("InfoDetail = %q, want %q", result.InfoDetail, "verbose")
	}
}

func TestPreferences_InvalidStyle(t *testing.T) {
	global := &config.Preferences{InfoStyle: "unknown"}
	result := config.ResolvePreferences(global, nil)
	if result.InfoStyle != "unknown" {
		t.Errorf("InfoStyle = %q, want %q", result.InfoStyle, "unknown")
	}
}

func TestSandboxPolicy_ExtraFields_Parse(t *testing.T) {
	input := `
writable_extra:
  - /tmp/myproject
  - /var/cache
readable_extra:
  - /opt/tools
  - /usr/local/share
denied_extra:
  - ~/.kube
  - ~/.terraform.d
network: outbound
`
	var sp config.SandboxPolicy
	if err := yaml.Unmarshal([]byte(input), &sp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(sp.WritableExtra) != 2 || sp.WritableExtra[0] != "/tmp/myproject" || sp.WritableExtra[1] != "/var/cache" {
		t.Errorf("WritableExtra = %v, want [/tmp/myproject /var/cache]", sp.WritableExtra)
	}
	if len(sp.ReadableExtra) != 2 || sp.ReadableExtra[0] != "/opt/tools" || sp.ReadableExtra[1] != "/usr/local/share" {
		t.Errorf("ReadableExtra = %v, want [/opt/tools /usr/local/share]", sp.ReadableExtra)
	}
	if len(sp.DeniedExtra) != 2 || sp.DeniedExtra[0] != "~/.kube" || sp.DeniedExtra[1] != "~/.terraform.d" {
		t.Errorf("DeniedExtra = %v, want [~/.kube ~/.terraform.d]", sp.DeniedExtra)
	}
	if sp.Network == nil || sp.Network.Mode != "outbound" {
		t.Errorf("Network = %v, want mode=outbound", sp.Network)
	}
	if len(sp.Writable) != 0 {
		t.Errorf("Writable = %v, want empty", sp.Writable)
	}
}

func TestPreferences_YoloField(t *testing.T) {
	input := `
preferences:
  yolo: true
`
	var cfg config.Config
	if err := yaml.Unmarshal([]byte(input), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if cfg.Preferences == nil {
		t.Fatal("Preferences is nil")
	}
	if cfg.Preferences.Yolo == nil || !*cfg.Preferences.Yolo {
		t.Errorf("Yolo = %v, want true", cfg.Preferences.Yolo)
	}
}

func TestContext_YoloField(t *testing.T) {
	input := `
agents:
  claude:
    binary: claude
contexts:
  work:
    agent: claude
    yolo: true
default_context: work
`
	var cfg config.Config
	if err := yaml.Unmarshal([]byte(input), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	ctx := cfg.Contexts["work"]
	if ctx.Yolo == nil || !*ctx.Yolo {
		t.Errorf("Context.Yolo = %v, want true", ctx.Yolo)
	}
}

func TestConfig_MinimalYoloField(t *testing.T) {
	input := `
agent: claude
yolo: true
`
	var cfg config.Config
	if err := yaml.Unmarshal([]byte(input), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if cfg.Yolo == nil || !*cfg.Yolo {
		t.Errorf("Config.Yolo = %v, want true", cfg.Yolo)
	}
}

func TestProjectOverride_YoloField(t *testing.T) {
	input := `
agent: codex
yolo: false
`
	var po config.ProjectOverride
	if err := yaml.Unmarshal([]byte(input), &po); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if po.Yolo == nil || *po.Yolo {
		t.Errorf("ProjectOverride.Yolo = %v, want false", po.Yolo)
	}
}

func TestResolveYolo_AllNil(t *testing.T) {
	got := config.ResolveYolo(nil, nil, nil)
	if got {
		t.Error("ResolveYolo(nil, nil, nil) = true, want false")
	}
}

func TestResolveYolo_PreferencesOnly(t *testing.T) {
	tr := true
	got := config.ResolveYolo(&tr, nil, nil)
	if !got {
		t.Error("ResolveYolo(true, nil, nil) = false, want true")
	}
}

func TestResolveYolo_ContextOverridesPreferences(t *testing.T) {
	tr := true
	f := false
	got := config.ResolveYolo(&tr, &f, nil)
	if got {
		t.Error("ResolveYolo(true, false, nil) = true, want false")
	}
}

func TestResolveYolo_ProjectOverridesAll(t *testing.T) {
	tr := true
	f := false
	got := config.ResolveYolo(&f, &tr, &f)
	if got {
		t.Error("ResolveYolo(false, true, false) = true, want false")
	}
}

func TestResolveYolo_ProjectTrue(t *testing.T) {
	f := false
	tr := true
	got := config.ResolveYolo(&f, &f, &tr)
	if !got {
		t.Error("ResolveYolo(false, false, true) = false, want true")
	}
}

func TestResolveYolo_ContextOnlyTrue(t *testing.T) {
	tr := true
	got := config.ResolveYolo(nil, &tr, nil)
	if !got {
		t.Error("ResolveYolo(nil, true, nil) = false, want true")
	}
}

func TestConfigRoundTrip_SandboxExtraFields(t *testing.T) {
	dir := t.TempDir()
	configPath := dir + "/config.yaml"

	original := &config.Config{
		Agent: "claude",
		Sandbox: &config.SandboxPolicy{
			Writable:      []string{"/tmp"},
			WritableExtra: []string{"/tmp/myproject"},
			Readable:      []string{"/etc"},
			ReadableExtra: []string{"/opt/tools"},
			Denied:        []string{"/root"},
			DeniedExtra:   []string{"~/.kube", "~/.terraform.d"},
			Network:       &config.NetworkPolicy{Mode: "outbound"},
		},
	}

	if err := config.WriteConfigTo(original, configPath); err != nil {
		t.Fatalf("WriteConfigTo() error = %v", err)
	}

	loaded, err := config.Load(dir, dir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	ctx, ok := loaded.Contexts["default"]
	if !ok {
		t.Fatal("expected 'default' context after loading minimal config")
	}
	sbRef := ctx.Sandbox
	if sbRef == nil {
		t.Fatal("Sandbox is nil after round-trip")
	}
	if sbRef.Inline == nil {
		t.Fatal("Sandbox.Inline is nil after round-trip")
	}
	sb := sbRef.Inline

	if len(sb.Writable) != 1 || sb.Writable[0] != "/tmp" {
		t.Errorf("Writable = %v, want [/tmp]", sb.Writable)
	}
	if len(sb.WritableExtra) != 1 || sb.WritableExtra[0] != "/tmp/myproject" {
		t.Errorf("WritableExtra = %v, want [/tmp/myproject]", sb.WritableExtra)
	}
	if len(sb.DeniedExtra) != 2 || sb.DeniedExtra[0] != "~/.kube" {
		t.Errorf("DeniedExtra = %v, want [~/.kube ~/.terraform.d]", sb.DeniedExtra)
	}
	if sb.Network == nil || sb.Network.Mode != "outbound" {
		t.Errorf("Network = %v, want mode=outbound", sb.Network)
	}
}

func TestCapabilityDef_Unmarshal(t *testing.T) {
	input := `
capabilities:
  git-ro:
    description: "Read-only Git access"
    readable:
      - "{{project}}/.git"
    writable: []
    deny:
      - "{{project}}/.git/config"
    env_allow:
      - GIT_DIR
  node-dev:
    extends: git-ro
    description: "Node.js development"
    writable:
      - "{{project}}/node_modules"
    env_allow:
      - NODE_ENV
      - NPM_TOKEN
  full-stack:
    combines:
      - git-ro
      - node-dev
    description: "Combined full-stack capability"
never_allow:
  - /etc/shadow
  - /etc/passwd
never_allow_env:
  - AWS_SECRET_ACCESS_KEY
  - GITHUB_TOKEN
agents:
  claude:
    binary: claude
contexts:
  dev:
    agent: claude
    capabilities:
      - git-ro
      - node-dev
`
	var cfg config.Config
	if err := yaml.Unmarshal([]byte(input), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Verify capabilities map
	if len(cfg.Capabilities) != 3 {
		t.Fatalf("len(Capabilities) = %d, want 3", len(cfg.Capabilities))
	}

	gitRO := cfg.Capabilities["git-ro"]
	if gitRO.Description != "Read-only Git access" {
		t.Errorf("git-ro.Description = %q, want %q", gitRO.Description, "Read-only Git access")
	}
	if len(gitRO.Readable) != 1 || gitRO.Readable[0] != "{{project}}/.git" {
		t.Errorf("git-ro.Readable = %v, want [{{project}}/.git]", gitRO.Readable)
	}
	if len(gitRO.Writable) != 0 {
		t.Errorf("git-ro.Writable = %v, want []", gitRO.Writable)
	}
	if len(gitRO.Deny) != 1 || gitRO.Deny[0] != "{{project}}/.git/config" {
		t.Errorf("git-ro.Deny = %v, want [{{project}}/.git/config]", gitRO.Deny)
	}
	if len(gitRO.EnvAllow) != 1 || gitRO.EnvAllow[0] != "GIT_DIR" {
		t.Errorf("git-ro.EnvAllow = %v, want [GIT_DIR]", gitRO.EnvAllow)
	}
	if gitRO.Extends != "" {
		t.Errorf("git-ro.Extends = %q, want empty", gitRO.Extends)
	}
	if len(gitRO.Combines) != 0 {
		t.Errorf("git-ro.Combines = %v, want empty", gitRO.Combines)
	}

	nodeDev := cfg.Capabilities["node-dev"]
	if nodeDev.Extends != "git-ro" {
		t.Errorf("node-dev.Extends = %q, want %q", nodeDev.Extends, "git-ro")
	}
	if nodeDev.Description != "Node.js development" {
		t.Errorf("node-dev.Description = %q, want %q", nodeDev.Description, "Node.js development")
	}
	if len(nodeDev.Writable) != 1 || nodeDev.Writable[0] != "{{project}}/node_modules" {
		t.Errorf("node-dev.Writable = %v, want [{{project}}/node_modules]", nodeDev.Writable)
	}
	if len(nodeDev.EnvAllow) != 2 || nodeDev.EnvAllow[0] != "NODE_ENV" || nodeDev.EnvAllow[1] != "NPM_TOKEN" {
		t.Errorf("node-dev.EnvAllow = %v, want [NODE_ENV NPM_TOKEN]", nodeDev.EnvAllow)
	}

	fullStack := cfg.Capabilities["full-stack"]
	if len(fullStack.Combines) != 2 || fullStack.Combines[0] != "git-ro" || fullStack.Combines[1] != "node-dev" {
		t.Errorf("full-stack.Combines = %v, want [git-ro node-dev]", fullStack.Combines)
	}
	if fullStack.Description != "Combined full-stack capability" {
		t.Errorf("full-stack.Description = %q, want %q", fullStack.Description, "Combined full-stack capability")
	}

	// Verify never_allow
	if len(cfg.NeverAllow) != 2 || cfg.NeverAllow[0] != "/etc/shadow" || cfg.NeverAllow[1] != "/etc/passwd" {
		t.Errorf("NeverAllow = %v, want [/etc/shadow /etc/passwd]", cfg.NeverAllow)
	}

	// Verify never_allow_env
	if len(cfg.NeverAllowEnv) != 2 || cfg.NeverAllowEnv[0] != "AWS_SECRET_ACCESS_KEY" || cfg.NeverAllowEnv[1] != "GITHUB_TOKEN" {
		t.Errorf("NeverAllowEnv = %v, want [AWS_SECRET_ACCESS_KEY GITHUB_TOKEN]", cfg.NeverAllowEnv)
	}

	// Verify context capabilities
	ctx := cfg.Contexts["dev"]
	if len(ctx.Capabilities) != 2 || ctx.Capabilities[0] != "git-ro" || ctx.Capabilities[1] != "node-dev" {
		t.Errorf("Context.Capabilities = %v, want [git-ro node-dev]", ctx.Capabilities)
	}
}

func TestProjectOverride_Capabilities(t *testing.T) {
	input := `
agent: codex
capabilities:
  - git-ro
  - docker
`
	var po config.ProjectOverride
	if err := yaml.Unmarshal([]byte(input), &po); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if po.Agent != "codex" {
		t.Errorf("Agent = %q, want %q", po.Agent, "codex")
	}
	if len(po.Capabilities) != 2 || po.Capabilities[0] != "git-ro" || po.Capabilities[1] != "docker" {
		t.Errorf("Capabilities = %v, want [git-ro docker]", po.Capabilities)
	}
}

// --- SandboxPolicy.UnmarshalYAML edge cases ---

func TestSandboxPolicy_UnmarshalYAML_BoolFalse(t *testing.T) {
	input := `sandbox: false`
	var cfg config.Config
	if err := yaml.Unmarshal([]byte(input), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if cfg.Sandbox == nil {
		t.Fatal("Sandbox is nil")
	}
	if !cfg.Sandbox.Disabled {
		t.Error("expected Sandbox.Disabled to be true for sandbox: false")
	}
}

func TestSandboxPolicy_UnmarshalYAML_BoolTrue_Error(t *testing.T) {
	input := `sandbox: true`
	var cfg config.Config
	err := yaml.Unmarshal([]byte(input), &cfg)
	if err == nil {
		t.Fatal("expected error for sandbox: true, got nil")
	}
}

func TestSandboxPolicy_UnmarshalYAML_AliasStruct(t *testing.T) {
	input := `
writable: ["/tmp"]
readable: ["/etc"]
network: outbound
`
	var sp config.SandboxPolicy
	if err := yaml.Unmarshal([]byte(input), &sp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if sp.Disabled {
		t.Error("expected Disabled=false for struct form")
	}
	if len(sp.Writable) != 1 || sp.Writable[0] != "/tmp" {
		t.Errorf("Writable = %v, want [/tmp]", sp.Writable)
	}
}

// --- SandboxRef.MarshalYAML edge cases ---

func TestSandboxRef_MarshalYAML_Disabled(t *testing.T) {
	ref := config.SandboxRef{Disabled: true}
	data, err := yaml.Marshal(&ref)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(data) != "false\n" {
		t.Errorf("MarshalYAML(disabled) = %q, want %q", string(data), "false\n")
	}
}

func TestSandboxRef_MarshalYAML_ProfileName(t *testing.T) {
	ref := config.SandboxRef{ProfileName: "strict"}
	data, err := yaml.Marshal(&ref)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out map[string]string
	if err := yaml.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if out["profile"] != "strict" {
		t.Errorf("profile = %q, want %q", out["profile"], "strict")
	}
}

func TestSandboxRef_MarshalYAML_Nil(t *testing.T) {
	ref := config.SandboxRef{}
	data, err := yaml.Marshal(&ref)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(data) != "null\n" {
		t.Errorf("MarshalYAML(empty) = %q, want %q", string(data), "null\n")
	}
}

func TestSandboxRef_MarshalYAML_Inline(t *testing.T) {
	ref := config.SandboxRef{
		Inline: &config.SandboxPolicy{
			Writable: []string{"/tmp"},
			Network:  &config.NetworkPolicy{Mode: "outbound"},
		},
	}
	data, err := yaml.Marshal(&ref)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// Should serialize the inline policy directly, not wrapped in "inline:"
	var sp config.SandboxPolicy
	if err := yaml.Unmarshal(data, &sp); err != nil {
		t.Fatalf("unmarshal result as SandboxPolicy: %v", err)
	}
	if len(sp.Writable) != 1 || sp.Writable[0] != "/tmp" {
		t.Errorf("Writable = %v, want [/tmp]", sp.Writable)
	}
}

// --- SandboxRef.UnmarshalYAML edge cases ---

func TestSandboxRef_UnmarshalYAML_BoolFalse(t *testing.T) {
	input := `sandbox: false`
	type wrapper struct {
		Sandbox *config.SandboxRef `yaml:"sandbox"`
	}
	var w wrapper
	if err := yaml.Unmarshal([]byte(input), &w); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if w.Sandbox == nil {
		t.Fatal("Sandbox is nil")
	}
	if !w.Sandbox.Disabled {
		t.Error("expected Disabled=true for sandbox: false")
	}
}

func TestSandboxRef_UnmarshalYAML_BoolTrue_Error(t *testing.T) {
	input := `sandbox: true`
	type wrapper struct {
		Sandbox *config.SandboxRef `yaml:"sandbox"`
	}
	var w wrapper
	err := yaml.Unmarshal([]byte(input), &w)
	if err == nil {
		t.Fatal("expected error for sandbox: true, got nil")
	}
}

func TestSandboxRef_UnmarshalYAML_String(t *testing.T) {
	input := `sandbox: "strict"`
	type wrapper struct {
		Sandbox *config.SandboxRef `yaml:"sandbox"`
	}
	var w wrapper
	if err := yaml.Unmarshal([]byte(input), &w); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if w.Sandbox == nil {
		t.Fatal("Sandbox is nil")
	}
	if w.Sandbox.ProfileName != "strict" {
		t.Errorf("ProfileName = %q, want %q", w.Sandbox.ProfileName, "strict")
	}
}

func TestSandboxRef_UnmarshalYAML_RefStruct(t *testing.T) {
	input := `sandbox:
  profile: "production"
`
	type wrapper struct {
		Sandbox *config.SandboxRef `yaml:"sandbox"`
	}
	var w wrapper
	if err := yaml.Unmarshal([]byte(input), &w); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if w.Sandbox == nil {
		t.Fatal("Sandbox is nil")
	}
	if w.Sandbox.ProfileName != "production" {
		t.Errorf("ProfileName = %q, want %q", w.Sandbox.ProfileName, "production")
	}
}

func TestSandboxRef_UnmarshalYAML_InlinePolicy(t *testing.T) {
	input := `sandbox:
  writable: ["/tmp"]
  network: outbound
`
	type wrapper struct {
		Sandbox *config.SandboxRef `yaml:"sandbox"`
	}
	var w wrapper
	if err := yaml.Unmarshal([]byte(input), &w); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if w.Sandbox == nil {
		t.Fatal("Sandbox is nil")
	}
	if w.Sandbox.Inline == nil {
		t.Fatal("Sandbox.Inline is nil")
	}
	if len(w.Sandbox.Inline.Writable) != 1 || w.Sandbox.Inline.Writable[0] != "/tmp" {
		t.Errorf("Inline.Writable = %v, want [/tmp]", w.Sandbox.Inline.Writable)
	}
	if w.Sandbox.Inline.Network == nil || w.Sandbox.Inline.Network.Mode != "outbound" {
		t.Errorf("Inline.Network = %v, want mode=outbound", w.Sandbox.Inline.Network)
	}
}

// --- ResolvePreferences project override edge cases ---

func TestResolvePreferences_ProjectOverridesShowInfo(t *testing.T) {
	tr := true
	f := false
	global := &config.Preferences{ShowInfo: &tr, InfoStyle: "boxed", InfoDetail: "verbose"}
	project := &config.Preferences{ShowInfo: &f}
	result := config.ResolvePreferences(global, project)
	if result.ShowInfo == nil || *result.ShowInfo {
		t.Errorf("ShowInfo = %v, want false (project override)", result.ShowInfo)
	}
	// Global style/detail should still apply
	if result.InfoStyle != "boxed" {
		t.Errorf("InfoStyle = %q, want %q", result.InfoStyle, "boxed")
	}
	if result.InfoDetail != "verbose" {
		t.Errorf("InfoDetail = %q, want %q", result.InfoDetail, "verbose")
	}
}

func TestResolvePreferences_ProjectOverridesInfoDetail(t *testing.T) {
	global := &config.Preferences{InfoDetail: "verbose"}
	project := &config.Preferences{InfoDetail: "minimal"}
	result := config.ResolvePreferences(global, project)
	if result.InfoDetail != "minimal" {
		t.Errorf("InfoDetail = %q, want %q", result.InfoDetail, "minimal")
	}
}

func TestResolvePreferences_ProjectOnlyNoGlobal(t *testing.T) {
	f := false
	project := &config.Preferences{ShowInfo: &f, InfoStyle: "clean", InfoDetail: "minimal"}
	result := config.ResolvePreferences(nil, project)
	if result.ShowInfo == nil || *result.ShowInfo {
		t.Errorf("ShowInfo = %v, want false", result.ShowInfo)
	}
	if result.InfoStyle != "clean" {
		t.Errorf("InfoStyle = %q, want %q", result.InfoStyle, "clean")
	}
	if result.InfoDetail != "minimal" {
		t.Errorf("InfoDetail = %q, want %q", result.InfoDetail, "minimal")
	}
}

func TestProjectOverride_DisabledCapabilities_RoundTrip(t *testing.T) {
	input := `
capabilities:
  - k8s
  - terraform
disabled_capabilities:
  - docker
`
	var po config.ProjectOverride
	if err := yaml.Unmarshal([]byte(input), &po); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(po.Capabilities) != 2 || po.Capabilities[0] != "k8s" {
		t.Errorf("expected capabilities [k8s terraform], got %v", po.Capabilities)
	}
	if len(po.DisabledCapabilities) != 1 || po.DisabledCapabilities[0] != "docker" {
		t.Errorf("expected disabled_capabilities [docker], got %v", po.DisabledCapabilities)
	}

	data, err := yaml.Marshal(&po)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var po2 config.ProjectOverride
	if err := yaml.Unmarshal(data, &po2); err != nil {
		t.Fatalf("re-unmarshal: %v", err)
	}
	if len(po2.DisabledCapabilities) != 1 || po2.DisabledCapabilities[0] != "docker" {
		t.Errorf("round-trip failed: got %v", po2.DisabledCapabilities)
	}
}

func TestSandboxOverrides_HasNetworkMode(t *testing.T) {
	o := config.SandboxOverrides{NetworkMode: "unrestricted"}
	if o.NetworkMode != "unrestricted" {
		t.Errorf("expected NetworkMode unrestricted, got %q", o.NetworkMode)
	}
}

func TestProjectOverride_ParsesCapabilityVariants(t *testing.T) {
	raw := []byte(`
capabilities:
  - python
  - node
capability_variants:
  python: [uv]
  node: [pnpm, corepack]
`)
	var po config.ProjectOverride
	if err := yaml.Unmarshal(raw, &po); err != nil {
		t.Fatal(err)
	}
	if got := po.CapabilityVariants["python"]; len(got) != 1 || got[0] != "uv" {
		t.Errorf("python variants = %v, want [uv]", got)
	}
	if got := po.CapabilityVariants["node"]; len(got) != 2 || got[0] != "pnpm" || got[1] != "corepack" {
		t.Errorf("node variants = %v, want [pnpm corepack]", got)
	}
}
