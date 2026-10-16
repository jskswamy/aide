package launcher

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func writeCodexConfig(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

const codexCustomProviders = `
model = "gpt-5"
model_provider = "corp"

[model_providers.corp]
name = "Corp gateway"
base_url = "https://llm.corp.example/v1"
env_key = "CORP_LLM_KEY"
env_http_headers = { "X-Corp-Token" = "CORP_LLM_HEADER_TOKEN" }

[model_providers.azure]
base_url = "https://x.openai.azure.com/openai"
env_key = "AZURE_OPENAI_API_KEY"
experimental_bearer_token = "never-printed"

[mcp_servers.docs]
url = "https://mcp.example"
bearer_token_env_var = "DOCS_MCP_TOKEN"
`

func TestCodexProviderEnvKeys(t *testing.T) {
	dir := t.TempDir()
	writeCodexConfig(t, dir, "config.toml", codexCustomProviders)
	writeCodexConfig(t, dir, "fast.config.toml", "[model_providers.fast]\nenv_key = \"FAST_KEY\"\n")

	names, notice := codexProviderEnvKeys(dir)

	want := []string{"AZURE_OPENAI_API_KEY", "CORP_LLM_HEADER_TOKEN", "CORP_LLM_KEY", "FAST_KEY"}
	if !slices.Equal(names, want) {
		t.Errorf("names = %v, want %v", names, want)
	}
	if notice != "" {
		t.Errorf("notice = %q, want none", notice)
	}
}

func TestCodexProviderEnvKeys_MissingFileIsSilent(t *testing.T) {
	names, notice := codexProviderEnvKeys(t.TempDir())
	if len(names) != 0 || notice != "" {
		t.Errorf("missing config.toml: names = %v, notice = %q; want nothing", names, notice)
	}
}

func TestCodexProviderEnvKeys_InvalidFileGivesNotice(t *testing.T) {
	dir := t.TempDir()
	writeCodexConfig(t, dir, "config.toml", "[model_providers.corp]\nenv_key = \"CORP_LLM_KEY\"\napi_secret = sk-unquoted-secret\n")

	names, notice := codexProviderEnvKeys(dir)

	if len(names) != 0 {
		t.Errorf("names = %v from an invalid file", names)
	}
	if !strings.Contains(notice, filepath.Join(dir, "config.toml")) || !strings.Contains(notice, "not scrubbed") {
		t.Errorf("notice = %q, want the file and what is not scrubbed", notice)
	}
	if strings.Contains(notice, "sk-unquoted-secret") {
		t.Errorf("notice reveals file content: %q", notice)
	}
}

// A Codex profile launch scrubs the custom provider keys its CODEX_HOME
// config.toml names from the shell.
func TestLauncher_CodexProfileScrubsCustomProviderKeys(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeCodexConfig(t, filepath.Join(home, ".codex-work"), "config.toml", codexCustomProviders)
	t.Setenv("CORP_LLM_KEY", "corp-shell-key")
	t.Setenv("CORP_LLM_HEADER_TOKEN", "corp-shell-header")
	t.Setenv("DOCS_MCP_TOKEN", "mcp-shell-token")

	env, stderr := launchCapturingEnvAndStderr(t, `
agents:
  codex:
    binary: codex
profiles:
  work:
    agent: codex
    config_dir_name: work
default_profile: work
`, "")

	for _, k := range []string{"CORP_LLM_KEY", "CORP_LLM_HEADER_TOKEN"} {
		if v, ok := envValue(env, k); ok {
			t.Errorf("shell %s reached a Codex profile launch: %q", k, v)
		}
	}
	// MCP tokens are resource credentials, not provider keys.
	if v, _ := envValue(env, "DOCS_MCP_TOKEN"); v != "mcp-shell-token" {
		t.Errorf("DOCS_MCP_TOKEN = %q, want it kept", v)
	}
	if strings.Contains(stderr, "never-printed") {
		t.Error("stderr reveals config.toml content")
	}
}

func TestLauncher_CodexInvalidConfigNotice(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeCodexConfig(t, filepath.Join(home, ".codex-work"), "config.toml", "not = [valid")

	_, stderr := launchCapturingEnvAndStderr(t, `
agents:
  codex:
    binary: codex
profiles:
  work:
    agent: codex
    config_dir_name: work
default_profile: work
`, "")
	if !strings.Contains(stderr, "aide: note: "+filepath.Join(home, ".codex-work", "config.toml")) {
		t.Errorf("no invalid-config notice:\n%s", stderr)
	}
}

func TestCodexProviderEnvKeys_SkipsProcessEssentials(t *testing.T) {
	dir := t.TempDir()
	writeCodexConfig(t, dir, "config.toml", "[model_providers.a]\nenv_key = \"PATH\"\n[model_providers.b]\nenv_key = \"AIDE_X\"\n[model_providers.c]\nenv_key = \"OK_KEY\"\n")
	if names, _ := codexProviderEnvKeys(dir); !slices.Equal(names, []string{"OK_KEY"}) {
		t.Errorf("names = %v, want only OK_KEY", names)
	}
}

// Custom Codex keys are configured credential names: every configured
// launch drops them from the shell, whichever profile's Codex home (or
// the default ~/.codex, or an exported CODEX_HOME) names them.
func TestLauncher_CodexProviderKeysScrubbedForEveryLaunch(t *testing.T) {
	cfgYAML := `
agents:
  codex:
    binary: codex
  claude:
    binary: claude
profiles:
  work:
    agent: codex
    config_dir_name: work
  other:
    agent: claude
    config_dir_name: other
  plain:
    agent: codex
default_profile: work
`
	for name, tc := range map[string]struct {
		configDir string // home-relative; "" means an exported CODEX_HOME
		profile   string
	}{
		"another profile's home":          {".codex-work", "other"},
		"default home, profile-less":      {".codex", "plain"},
		"exported CODEX_HOME, no profile": {"", "plain"},
	} {
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			dir := filepath.Join(home, tc.configDir)
			if tc.configDir == "" {
				dir = filepath.Join(home, "exported-codex")
				t.Setenv("CODEX_HOME", dir)
			} else {
				unsetEnvForTest(t, "CODEX_HOME")
			}
			writeCodexConfig(t, dir, "config.toml", codexCustomProviders)
			t.Setenv("CORP_LLM_KEY", "corp-shell-key")

			env, _ := launchCapturingEnvAndStderr(t, strings.Replace(cfgYAML, "default_profile: work", "default_profile: "+tc.profile, 1), "")
			if v, ok := envValue(env, "CORP_LLM_KEY"); ok {
				t.Errorf("shell CORP_LLM_KEY reached the %s launch: %q", tc.profile, v)
			}
		})
	}
}

// A binding can still inject a scrubbed custom Codex key.
func TestLauncher_CodexProviderKeyFromBinding(t *testing.T) {
	store, values := fixtureStore(t)
	home := t.TempDir()
	writeCodexConfig(t, filepath.Join(home, ".codex-work"), "config.toml", codexCustomProviders)
	t.Setenv("CORP_LLM_KEY", "corp-shell-key")

	env, _, err := launchCredentialsIn(t, home, `
profiles:
  work:
    agent: codex
    config_dir_name: work
    credentials:
      corp: {backend: age, store: `+store+`, key: openai_api_key, env: [CORP_LLM_KEY], harnesses: [codex]}
    default_credentials: [corp]
default_profile: work
`, "", "", true)
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	if v, _ := envValue(env, "CORP_LLM_KEY"); v != values["openai_api_key"] {
		t.Error("CORP_LLM_KEY is not the bound value")
	}
}

func unsetEnvForTest(t *testing.T, key string) {
	t.Helper()
	t.Setenv(key, "")
	if err := os.Unsetenv(key); err != nil {
		t.Fatal(err)
	}
}
