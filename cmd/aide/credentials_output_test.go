package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/jskswamy/aide/internal/output"
)

const credentialConfig = `
default_profile: work
profiles:
  work:
    agent: claude
    secret: work
    env:
      OPENROUTER_API_KEY: "{{ .secrets.openrouter_api_key }}"
    credentials:
      anthropic: {backend: age, store: work, key: anthropic_api_key}
    default_credentials: [anthropic]
`

// runJSON runs a which/status command with --format json against
// credentialConfig and returns the decoded output.
func runJSON(t *testing.T, cmd *cobra.Command) map[string]any {
	t.Helper()
	return runJSONWith(t, cmd, credentialConfig)
}

// runJSONWith is runJSON against the config file body cfgBody.
func runJSONWith(t *testing.T, cmd *cobra.Command, cfgBody string) map[string]any {
	t.Helper()
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Setenv("HOME", t.TempDir())
	t.Chdir(t.TempDir())
	cfgDir := filepath.Join(xdg, "aide")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "config.yaml"), []byte(cfgBody), 0o600); err != nil {
		t.Fatal(err)
	}
	output.RegisterFlag(cmd)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--format", "json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v\n%s", err, out.String())
	}
	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, out.String())
	}
	return got
}

func TestWhichAndStatus_JSONNameCredentialBindings(t *testing.T) {
	for name, cmd := range map[string]func() *cobra.Command{"which": whichCmd, "status": statusCmd} {
		t.Run(name, func(t *testing.T) {
			got := runJSON(t, cmd())
			if got["auth_mode"] != "api" {
				t.Errorf("auth_mode = %v, want api", got["auth_mode"])
			}
			creds, _ := json.Marshal(got["credentials"])
			// The provider credentials are gateways now: the launched harness's
			// choice is listed, and Pi's choice is the translated env: key.
			for _, w := range []string{`"anthropic"`, `"ANTHROPIC_API_KEY"`} {
				if !strings.Contains(string(creds), w) {
					t.Errorf("credentials %s lacks %s", creds, w)
				}
			}
			if views, _ := json.Marshal(got["gateway_choices"]); !strings.Contains(string(views), `"others":1`) {
				t.Errorf("gateway_choices %s lack the translated env: key's gateway among the others", views)
			}
			if env, _ := json.Marshal(got["env_items"]); strings.Contains(string(env), "OPENROUTER_API_KEY") {
				t.Errorf("translated legacy credential still listed as env: %s", env)
			}
		})
	}
}

const gatewayVisibilityConfig = `
default_profile: work
profiles:
  work:
    agent: claude
    gateways:
      corp: {base_url: "https://llm.corp/v1", store: work, key: corp, harnesses: [claude, pi]}
      ollama: {base_url: "http://127.0.0.1:11434", harnesses: [pi]}
    gateway_choice:
      claude: corp
`

func TestWhichAndStatus_JSONGatewayChoices(t *testing.T) {
	for name, cmd := range map[string]func() *cobra.Command{"which": whichCmd, "status": statusCmd} {
		t.Run(name, func(t *testing.T) {
			got := runJSONWith(t, cmd(), gatewayVisibilityConfig)
			views, _ := got["gateway_choices"].([]any)
			byHarness := map[string]map[string]any{}
			for _, v := range views {
				m, _ := v.(map[string]any)
				h, _ := m["harness"].(string)
				byHarness[h] = m
			}
			claude := byHarness["claude"]
			if claude["choice"] != "corp" || claude["source"] != "profile" || claude["kind"] != "gateway" ||
				claude["store"] != "work" || claude["key"] != "corp" || claude["key_env"] != "ANTHROPIC_AUTH_TOKEN" ||
				claude["url"] != "https://llm.corp" {
				t.Errorf("claude view = %v", claude)
			}
			// Legacy gateways keep today's rule: pi starts on the first
			// marked gateway and has the other in /model.
			if pi := byHarness["pi"]; pi["choice"] != "corp" || pi["startup"] != true || pi["others"] != float64(1) || pi["key_env"] != "AIDE_GW_CORP_KEY" {
				t.Errorf("pi view = %v, want corp (startup) with 1 in /model", pi)
			}
			if v := byHarness["codex"]; v["source"] != "no-candidates" {
				t.Errorf("codex view = %v", v)
			}
		})
	}
}

func TestWhich_HumanShowsGatewayBlockAndSummary(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Setenv("HOME", t.TempDir())
	t.Chdir(t.TempDir())
	if err := os.MkdirAll(filepath.Join(xdg, "aide"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(xdg, "aide", "config.yaml"), []byte(gatewayVisibilityConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := whichCmd()
	output.RegisterFlag(cmd)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(nil)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v\n%s", err, out.String())
	}
	for _, w := range []string{"gateway: corp → https://llm.corp   (profile work)", "Gateways (profile work)", "claude    corp  profile work        work/corp → ANTHROPIC_AUTH_TOKEN", "pi        corp  profile work        work/corp → AIDE_GW_CORP_KEY  +1 in /model"} {
		if !strings.Contains(out.String(), w) {
			t.Errorf("which output lacks %q:\n%s", w, out.String())
		}
	}
}
