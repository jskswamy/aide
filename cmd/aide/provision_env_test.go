package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/config"
	"github.com/jskswamy/aide/internal/provision"
)

// unsetEnv clears key for the test and restores it afterwards.
func unsetEnv(t *testing.T, key string) {
	t.Helper()
	t.Setenv(key, "")
	if err := os.Unsetenv(key); err != nil {
		t.Fatal(err)
	}
}

// A CLAUDE_CONFIG_DIR exported in the shell reaches the launched agent,
// so sync must target it too, sandboxed or not.
func TestResolveProvisionContext_InheritsExportedProfileEnv(t *testing.T) {
	for name, sb := range map[string]*config.SandboxRef{
		"sandboxed":   nil,
		"unsandboxed": {Disabled: true},
	} {
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			exported := filepath.Join(home, "claude-elsewhere")
			t.Setenv("CLAUDE_CONFIG_DIR", exported)
			ctx := config.Context{Agent: "claude", Sandbox: sb}
			pCtx, err := resolveProvisionContext(&config.Config{}, "c", ctx, home, home)
			if err != nil {
				t.Fatal(err)
			}
			if got := pCtx.Env["CLAUDE_CONFIG_DIR"]; got != exported {
				t.Errorf("CLAUDE_CONFIG_DIR = %q, want exported %q", got, exported)
			}
		})
	}
}

func TestResolveProvisionContext_ContextEnvBeatsExported(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, "exported"))
	want := filepath.Join(home, "declared")
	ctx := config.Context{Agent: "claude", Env: map[string]string{"CLAUDE_CONFIG_DIR": want}}
	pCtx, err := resolveProvisionContext(&config.Config{}, "c", ctx, home, home)
	if err != nil {
		t.Fatal(err)
	}
	if got := pCtx.Env["CLAUDE_CONFIG_DIR"]; got != want {
		t.Errorf("CLAUDE_CONFIG_DIR = %q, want context value %q", got, want)
	}
}

func TestResolveProvisionContext_InheritsExportedPiDir(t *testing.T) {
	home := t.TempDir()
	exported := filepath.Join(home, "pi-elsewhere")
	t.Setenv("PI_CODING_AGENT_DIR", exported)
	pCtx, err := resolveProvisionContext(&config.Config{}, "c", config.Context{Agent: "pi"}, home, home)
	if err != nil {
		t.Fatal(err)
	}
	if got := pCtx.Env["PI_CODING_AGENT_DIR"]; got != exported {
		t.Errorf("PI_CODING_AGENT_DIR = %q, want exported %q", got, exported)
	}
}

// Every provisioning entry point (sync, plugin/mcp list, statusline,
// agent grants) resolves its provision context here, so this is where
// the user's scrub_env: reaches provisioning subprocesses.
func TestResolveProvisionContext_AppliesScrubEnvToProvisionSubprocesses(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MY_PROVIDER_TOKEN", "custom-shell")
	cfg := &config.Config{ScrubEnv: []string{"MY_PROVIDER_TOKEN"}}
	pCtx, err := resolveProvisionContext(cfg, "c", config.Context{Agent: "claude"}, home, home)
	if err != nil {
		t.Fatal(err)
	}
	out, _, code, err := provision.ExecRunner{}.Run(context.Background(), pCtx, "env")
	if err != nil || code != 0 {
		t.Fatalf("env: code=%d err=%v", code, err)
	}
	if strings.Contains(out, "MY_PROVIDER_TOKEN=") {
		t.Error("scrub_env name reached a provisioning subprocess")
	}
}

// A custom Codex provider key named in ~/.codex/config.toml is a
// configured credential name: provisioning subprocesses drop it too.
func TestResolveProvisionContext_ScrubsCodexProviderKeys(t *testing.T) {
	home := t.TempDir()
	unsetEnv(t, "CODEX_HOME")
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".codex", "config.toml"),
		[]byte("[model_providers.corp]\nenv_key = \"CORP_LLM_KEY\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CORP_LLM_KEY", "corp-shell")
	pCtx, err := resolveProvisionContext(&config.Config{}, "c", config.Context{Agent: "claude"}, home, home)
	if err != nil {
		t.Fatal(err)
	}
	out, _, code, err := provision.ExecRunner{}.Run(context.Background(), pCtx, "env")
	if err != nil || code != 0 {
		t.Fatalf("env: code=%d err=%v", code, err)
	}
	if strings.Contains(out, "CORP_LLM_KEY=") {
		t.Error("a Codex env_key name reached a provisioning subprocess")
	}
}
