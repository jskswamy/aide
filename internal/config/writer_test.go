package config

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestWriteConfig_FullFormat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	allowSub := true
	cfg := &Config{
		Agents: map[string]AgentDef{
			"claude":  {Binary: "claude"},
			"copilot": {Binary: "gh-copilot"},
		},
		MCPServers: MCPServerMap{
			"myserver": {Command: "npx", Args: []string{"--yes", "mcp-server"}},
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

	// Verify MCP servers
	if len(loaded.MCPServers) == 0 {
		t.Fatal("expected MCPServers to be non-empty")
	}
	if loaded.MCPServers["myserver"].Command != "npx" {
		t.Errorf("MCP server command = %q, want %q", loaded.MCPServers["myserver"].Command, "npx")
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

	// WriteConfigTo promotes an in-memory minimal Config into the split layout (profiles/default.yaml)
	loaded, err := Load(dir, "")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	defCtx, ok := loaded.Contexts["default"]
	if !ok {
		t.Fatal("expected 'default' profile to be written in profiles/default.yaml")
	}
	if defCtx.Agent != "claude" {
		t.Errorf("agent = %q, want %q", defCtx.Agent, "claude")
	}
	if defCtx.Env["KEY"] != "value" {
		t.Errorf("env KEY = %q, want %q", defCtx.Env["KEY"], "value")
	}
	if defCtx.Secret != "secrets" {
		t.Errorf("secret = %q, want %q", defCtx.Secret, "secrets")
	}
}

func TestWriteConfig_AtomicOnError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("read-only directory test not reliable on Windows")
	}
	if os.Getuid() == 0 {
		t.Skip("read-only directory test not reliable when running as root")
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

	// Verify content via Load
	loaded, err := Load(nested, "")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.Contexts["default"].Agent != "claude" {
		t.Errorf("agent = %q, want %q", loaded.Contexts["default"].Agent, "claude")
	}
}

func TestWriteConfig_Roundtrip(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, "config")
	if err := os.MkdirAll(filepath.Join(configDir, "profiles"), 0755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	// Write initial split config files manually
	coreYAML := `agents:
  claude:
    binary: claude
  copilot:
    binary: gh-copilot
default_profile: work
`
	provYAML := `mcp_servers:
  myserver:
    command: npx
    args:
      - "--yes"
      - mcp-server
`
	workYAML := `agent: claude
env:
  API_KEY: secret123
`
	personalYAML := `agent: copilot
mcp_servers:
  - myserver
`
	configPath := filepath.Join(configDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(coreYAML), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "provisioning.yaml"), []byte(provYAML), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "profiles", "work.yaml"), []byte(workYAML), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "profiles", "personal.yaml"), []byte(personalYAML), 0644); err != nil {
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

func TestWriteConfig_RenameFailure_CleansTmpFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink/permission test not reliable on Windows")
	}

	dir := t.TempDir()

	// Create a subdirectory at the target path so rename(file, dir) fails
	targetPath := filepath.Join(dir, "config.yaml")
	if err := os.MkdirAll(targetPath, 0755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	// Put a file inside so the directory isn't empty (rename won't overwrite non-empty dir)
	if err := os.WriteFile(filepath.Join(targetPath, "blocker"), []byte("x"), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg := &Config{Agent: "claude"}
	err := WriteConfigTo(cfg, targetPath)
	if err == nil {
		t.Fatal("expected error when rename target is a non-empty directory, got nil")
	}

	// Verify the .tmp file was cleaned up
	tmpPath := targetPath + ".tmp"
	if _, statErr := os.Stat(tmpPath); !os.IsNotExist(statErr) {
		t.Errorf("expected .tmp file to be cleaned up, but it still exists")
	}
}

func TestWriteConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	cfg := &Config{DefaultContext: "personal"}
	err := WriteConfig(cfg)
	if err != nil {
		t.Fatalf("WriteConfig() error = %v", err)
	}

	path := FilePath()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Errorf("config file not created at %s", path)
	}
}

func TestWriteConfig_RefusesConfigThatFailedToLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	orig := []byte("contexts:\n  work:\n    agent: claude\n")
	if err := os.WriteFile(path, orig, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := FailedLoad(errors.New("boom"))
	cfg.Agents = map[string]AgentDef{"foo": {Binary: "foo"}}
	err := WriteConfigTo(cfg, path)
	if !errors.Is(err, ErrConfigNotLoaded) || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("WriteConfigTo() error = %v, want ErrConfigNotLoaded wrapping the load error", err)
	}
	got, _ := os.ReadFile(path)
	if !bytes.Equal(got, orig) {
		t.Errorf("config.yaml changed:\n%s", got)
	}
}

// A write that Load would then refuse would lock aide out of the config
// (even `aide env remove` loads it first), so it is refused up front.
func TestWriteConfig_RefusesConfigLoadWouldRefuse(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "config.yaml")
	const before = "profiles:\n  a: {agent: claude, config_dir_name: work}\n  b: {agent: claude}\n"
	if err := os.WriteFile(path, []byte(before), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := &Config{ProfileSchema: true, Contexts: map[string]Context{
		"a": {Agent: "claude", Profile: "work"},
		"b": {Agent: "claude", Env: map[string]string{"CLAUDE_CONFIG_DIR": "~/.claude-work"}},
	}}
	if err := WriteConfigTo(cfg, path); !errors.Is(err, ErrProfileDirAlias) {
		t.Fatalf("WriteConfigTo() error = %v, want ErrProfileDirAlias", err)
	}
	if got, _ := os.ReadFile(path); string(got) != before {
		t.Errorf("refused write changed the file:\n%s", got)
	}
}

func TestWriteConfig_ScattersToSplitFilesAndPreservesUnchangedMtime(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	cfg := &Config{
		ProfileSchema:  true,
		DefaultContext: "work",
		Agents:         map[string]AgentDef{"claude": {Binary: "claude"}},
		NeverAllow:     []string{"~/.kube/prod"},
		Plugins:        PluginMap{"steveyegge/beads": PluginEntryMarketplace([]string{"beads"})},
		Contexts: map[string]Context{
			"work":     {Agent: "claude", Profile: "work"},
			"personal": {Agent: "claude", Profile: "personal"},
		},
	}
	if err := WriteConfigTo(cfg, path); err != nil {
		t.Fatalf("WriteConfigTo() error = %v", err)
	}

	// Verify all split files exist and config.yaml has no inline profiles/sandbox/plugins
	coreBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, avoid := range []string{"profiles:", "contexts:", "never_allow:", "plugins:"} {
		if strings.Contains(string(coreBytes), avoid) {
			t.Errorf("config.yaml still contains %q:\n%s", avoid, coreBytes)
		}
	}
	workPath := filepath.Join(dir, "profiles", "work.yaml")
	workBytes, err := os.ReadFile(workPath)
	if err != nil {
		t.Fatalf("missing profiles/work.yaml: %v", err)
	}
	if !strings.Contains(string(workBytes), "config_dir_name: work") {
		t.Errorf("profiles/work.yaml missing config_dir_name:\n%s", workBytes)
	}

	// Record mtime on profiles/work.yaml and sandbox.yaml, then update only personal
	sbxPath := filepath.Join(dir, "sandbox.yaml")
	past := time.Now().Add(-10 * time.Minute)
	_ = os.Chtimes(workPath, past, past)
	_ = os.Chtimes(sbxPath, past, past)
	workInfo1, _ := os.Stat(workPath)

	delete(cfg.Contexts, "personal")
	cfg.NeverAllow = nil // empty sandbox.yaml -> should be removed
	cfg.Plugins = nil    // empty provisioning.yaml -> should be removed
	if err := WriteConfigTo(cfg, path); err != nil {
		t.Fatalf("second WriteConfigTo() error = %v", err)
	}

	workInfo2, _ := os.Stat(workPath)
	if !workInfo1.ModTime().Equal(workInfo2.ModTime()) {
		t.Errorf("unchanged profiles/work.yaml was rewritten (mtime %v -> %v)", workInfo1.ModTime(), workInfo2.ModTime())
	}
	if _, err := os.Stat(filepath.Join(dir, "profiles", "personal.yaml")); !os.IsNotExist(err) {
		t.Errorf("deleted profile file personal.yaml still exists: %v", err)
	}
	if _, err := os.Stat(sbxPath); !os.IsNotExist(err) {
		t.Errorf("emptied sandbox.yaml still exists: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "provisioning.yaml")); !os.IsNotExist(err) {
		t.Errorf("emptied provisioning.yaml still exists: %v", err)
	}
}

func TestWriteConfig_RefusesLegacyMonolithicConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	orig := "profiles:\n  work:\n    agent: claude\n"
	if err := os.WriteFile(path, []byte(orig), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	loaded.DefaultContext = "work"
	err = WriteConfigTo(loaded, path)
	if !errors.Is(err, ErrLegacyMonolithicConfig) {
		t.Fatalf("WriteConfigTo() error = %v, want ErrLegacyMonolithicConfig", err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != orig {
		t.Errorf("config.yaml was modified despite refusal:\n%s", got)
	}
}

func TestWriteConfig_RejectsInvalidProfileFilename(t *testing.T) {
	dir := t.TempDir()
	cfg := &Config{
		ProfileSchema: true,
		Contexts: map[string]Context{
			"bad.name": {Agent: "claude"},
		},
	}
	err := WriteConfigTo(cfg, filepath.Join(dir, "config.yaml"))
	if !errors.Is(err, ErrInvalidProfileFilename) {
		t.Fatalf("WriteConfigTo() error = %v, want ErrInvalidProfileFilename", err)
	}
}

func TestWriteConfig_TwoSpaceIndentAndPreservesUntouchedFiles(t *testing.T) {
	dir := t.TempDir()
	profDir := filepath.Join(dir, "profiles")
	if err := os.MkdirAll(profDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Seed 2-space formatted files with comments, --- headers, and non-struct key order.
	const (
		coreSrc = "---\n# global config\nagents:\n  claude:\n    binary: claude\ndefault_profile: work\n"
		sbxSrc  = "---\n# sandbox rules\nnever_allow:\n  - ~/.kube/prod\ncapabilities:\n  k8s-dev:\n    extends: k8s\n"
		workSrc = "---\n# work profile\nagent: claude\nenv:\n  FOO: bar\n"
		persSrc = "---\n# personal profile\nagent: claude # personal agent\nconfig_dir_name: personal\nmatch:\n  - path: ~/personal/*\n"
	)
	files := map[string]string{
		filepath.Join(dir, "config.yaml"):  coreSrc,
		filepath.Join(dir, "sandbox.yaml"): sbxSrc,
		filepath.Join(profDir, "work.yaml"): workSrc,
		filepath.Join(profDir, "personal.yaml"): persSrc,
	}
	past := time.Now().Add(-10 * time.Minute).Truncate(time.Second)
	for p, body := range files {
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, past, past); err != nil {
			t.Fatal(err)
		}
	}

	cfg, err := Load(dir, "")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Mutate only personal profile.
	pers := cfg.Contexts["personal"]
	pers.Capabilities = []string{"k8s-dev"}
	pers.Env = map[string]string{"KEY": "val"}
	cfg.Contexts["personal"] = pers

	if err := WriteConfigTo(cfg, filepath.Join(dir, "config.yaml")); err != nil {
		t.Fatalf("WriteConfigTo() error = %v", err)
	}

	// Untouched files keep their exact bytes (comments, ---, key order) and mtime.
	for _, p := range []string{
		filepath.Join(dir, "config.yaml"),
		filepath.Join(dir, "sandbox.yaml"),
		filepath.Join(profDir, "work.yaml"),
	} {
		got, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != files[p] {
			t.Errorf("%s bytes changed:\ngot:\n%s\nwant:\n%s", filepath.Base(p), got, files[p])
		}
		info, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if !info.ModTime().Equal(past) {
			t.Errorf("%s mtime = %v, want %v", filepath.Base(p), info.ModTime(), past)
		}
	}

	// Mutated profile is written with 2-space indentation, preserving its comments, --- header, and key order.
	persBytes, err := os.ReadFile(filepath.Join(profDir, "personal.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	persOut := string(persBytes)
	if strings.Contains(persOut, "\n    ") {
		t.Errorf("personal.yaml contains 4-space indentation:\n%s", persOut)
	}
	for _, want := range []string{
		"---\n# personal profile\n",
		"agent: claude # personal agent\nconfig_dir_name: personal\n",
		"env:\n  KEY: val\n",
	} {
		if !strings.Contains(persOut, want) {
			t.Errorf("personal.yaml missing %q:\n%s", want, persOut)
		}
	}
}

func TestFailedLoad_NilErrorAndSameYAMLFallbacks(t *testing.T) {
	cfg := FailedLoad(nil)
	if err := ValidateForWrite(cfg, ""); !errors.Is(err, ErrConfigNotLoaded) {
		t.Fatalf("ValidateForWrite(FailedLoad(nil)) = %v, want ErrConfigNotLoaded", err)
	}

	sameCore := sameStructYAML[CoreConfig]([]byte("default_profile: work\n"))
	for _, bad := range []string{
		":::invalid",
		"unknown_key: true\n",
		"default_profile: work\n---\ndefault_profile: other\n",
	} {
		if sameCore([]byte(bad)) {
			t.Errorf("sameStructYAML(%q) = true, want false", bad)
		}
	}

	sameProf := sameProfileYAML("work", []byte("agent: claude\n"))
	for _, bad := range []string{
		":::invalid",
		"unknown_key: true\n",
		"agent: claude\n---\nagent: codex\n",
	} {
		if sameProf([]byte(bad)) {
			t.Errorf("sameProfileYAML(%q) = true, want false", bad)
		}
	}
}
