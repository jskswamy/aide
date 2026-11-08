package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_FullConfig(t *testing.T) {
	configDir := t.TempDir()
	projectDir := t.TempDir()

	configYAML := `agents:
  claude:
    binary: claude
contexts:
  personal:
    agent: claude
    env:
      KEY: value
    mcp_servers: [git]
  work:
    agent: claude
    env:
      KEY: work-value
default_context: personal
`
	if err := os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte(configYAML), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(configDir, projectDir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if len(cfg.Contexts) != 2 {
		t.Errorf("expected 2 contexts, got %d", len(cfg.Contexts))
	}
	if _, ok := cfg.Contexts["personal"]; !ok {
		t.Error("expected 'personal' context")
	}
	if _, ok := cfg.Contexts["work"]; !ok {
		t.Error("expected 'work' context")
	}
	if cfg.Agents["claude"].Binary != "claude" {
		t.Errorf("expected agent binary 'claude', got %q", cfg.Agents["claude"].Binary)
	}
}

func TestLoad_WithProjectOverride(t *testing.T) {
	configDir := t.TempDir()
	projectDir := t.TempDir()

	// Global config
	configYAML := `agent: claude
env:
  KEY: global-value
`
	if err := os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte(configYAML), 0644); err != nil {
		t.Fatal(err)
	}

	// Project override (.aide.yaml)
	overrideYAML := `agent: codex
env:
  PROJECT_KEY: project-value
mcp_servers: [git]
`
	if err := os.WriteFile(filepath.Join(projectDir, ".aide.yaml"), []byte(overrideYAML), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(configDir, projectDir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.ProjectOverride == nil {
		t.Fatal("expected ProjectOverride to be non-nil")
	}
	if cfg.ProjectOverride.Agent != "codex" {
		t.Errorf("expected override agent 'codex', got %q", cfg.ProjectOverride.Agent)
	}
	if cfg.ProjectOverride.Env["PROJECT_KEY"] != "project-value" {
		t.Errorf("expected override env PROJECT_KEY='project-value', got %q", cfg.ProjectOverride.Env["PROJECT_KEY"])
	}
	if len(cfg.ProjectOverride.MCPServers) != 1 || cfg.ProjectOverride.MCPServers[0] != "git" {
		t.Errorf("expected override mcp_servers [git], got %v", cfg.ProjectOverride.MCPServers)
	}
}

func TestLoad_ProjectOverrideOnlyAgent(t *testing.T) {
	configDir := t.TempDir()
	projectDir := t.TempDir()

	configYAML := `agent: claude
`
	if err := os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte(configYAML), 0644); err != nil {
		t.Fatal(err)
	}

	overrideYAML := `agent: codex
`
	if err := os.WriteFile(filepath.Join(projectDir, ".aide.yaml"), []byte(overrideYAML), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(configDir, projectDir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.ProjectOverride == nil {
		t.Fatal("expected ProjectOverride to be non-nil")
	}
	if cfg.ProjectOverride.Agent != "codex" {
		t.Errorf("expected override agent 'codex', got %q", cfg.ProjectOverride.Agent)
	}
	if len(cfg.ProjectOverride.Env) != 0 {
		t.Errorf("expected empty override env, got %v", cfg.ProjectOverride.Env)
	}
}

func TestLoad_NoConfigFile(t *testing.T) {
	configDir := t.TempDir()
	projectDir := t.TempDir()
	t.Setenv("PATH", t.TempDir())

	// No config.yaml written and no known agent on PATH
	cfg, err := Load(configDir, projectDir)
	if err != nil {
		t.Fatalf("expected no error for missing config, got %v", err)
	}

	// Should return empty config when no agent is on PATH
	if len(cfg.Agents) != 0 || len(cfg.Contexts) != 0 {
		t.Errorf("expected empty config, got agents=%d contexts=%d", len(cfg.Agents), len(cfg.Contexts))
	}
}

func TestLoad_EmptyConfigDirSynthesizesDefaultAndLoadsProjectConfig(t *testing.T) {
	configDir := t.TempDir()
	projectDir := t.TempDir()
	binDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(binDir, "claude"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)
	if err := os.WriteFile(filepath.Join(projectDir, ".aide.yaml"), []byte("capabilities:\n  - docker\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(configDir, projectDir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !cfg.SynthesizedDefault || !cfg.SynthesizedDefaultContext {
		t.Errorf("SynthesizedDefault=%v SynthesizedDefaultContext=%v, want true/true", cfg.SynthesizedDefault, cfg.SynthesizedDefaultContext)
	}
	if cfg.DefaultContext != DefaultProfileName {
		t.Errorf("DefaultContext = %q, want %q", cfg.DefaultContext, DefaultProfileName)
	}
	if len(cfg.Agents) != 0 {
		t.Errorf("expected cfg.Agents not mutated in memory by Load, got %v", cfg.Agents)
	}
	defCtx, ok := cfg.Contexts[DefaultProfileName]
	if !ok || defCtx.Agent != "claude" || defCtx.SelectsConfigDir() {
		t.Errorf("Contexts[default] = %+v (ok=%v), want agent=claude SelectsConfigDir=false", defCtx, ok)
	}
	if cfg.ProjectOverride == nil || len(cfg.ProjectOverride.Capabilities) != 1 || cfg.ProjectOverride.Capabilities[0] != "docker" {
		t.Errorf("expected .aide.yaml loaded even when configDir is empty, got %+v", cfg.ProjectOverride)
	}
}

func TestLoad_NoProjectOverride(t *testing.T) {
	configDir := t.TempDir()
	projectDir := t.TempDir()

	configYAML := `agent: claude
`
	if err := os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte(configYAML), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(configDir, projectDir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.ProjectOverride != nil {
		t.Error("expected ProjectOverride to be nil when no .aide.yaml exists")
	}
}

func TestLoad_InvalidYAML(t *testing.T) {
	configDir := t.TempDir()
	projectDir := t.TempDir()

	invalidYAML := `{invalid: yaml: [broken`
	if err := os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte(invalidYAML), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(configDir, projectDir)
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

func TestLoad_ProjectOverrideInParentDir(t *testing.T) {
	configDir := t.TempDir()

	// Create a git repo as the project root
	projectRoot := t.TempDir()
	gitDir := filepath.Join(projectRoot, ".git")
	if err := os.Mkdir(gitDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create a subdirectory
	subDir := filepath.Join(projectRoot, "sub", "deep")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Write global config
	configYAML := `agent: claude
`
	if err := os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte(configYAML), 0644); err != nil {
		t.Fatal(err)
	}

	// Write .aide.yaml in the project root (parent of subDir)
	overrideYAML := `agent: codex
`
	if err := os.WriteFile(filepath.Join(projectRoot, ".aide.yaml"), []byte(overrideYAML), 0644); err != nil {
		t.Fatal(err)
	}

	// Load from subDir — should walk up and find .aide.yaml in projectRoot
	cfg, err := Load(configDir, subDir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.ProjectOverride == nil {
		t.Fatal("expected ProjectOverride to be non-nil (found in parent dir)")
	}
	if cfg.ProjectOverride.Agent != "codex" {
		t.Errorf("expected override agent 'codex', got %q", cfg.ProjectOverride.Agent)
	}
}
