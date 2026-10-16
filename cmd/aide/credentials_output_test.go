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
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Setenv("HOME", t.TempDir())
	t.Chdir(t.TempDir())
	cfgDir := filepath.Join(xdg, "aide")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "config.yaml"), []byte(credentialConfig), 0o600); err != nil {
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
			for _, w := range []string{`"anthropic"`, `"ANTHROPIC_API_KEY"`, `"openrouter"`, `"OPENROUTER_API_KEY"`} {
				if !strings.Contains(string(creds), w) {
					t.Errorf("credentials %s lacks %s", creds, w)
				}
			}
			if env, _ := json.Marshal(got["env_items"]); strings.Contains(string(env), "OPENROUTER_API_KEY") {
				t.Errorf("translated legacy credential still listed as env: %s", env)
			}
		})
	}
}
