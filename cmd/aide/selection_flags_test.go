package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/jskswamy/aide/internal/output"
	"github.com/jskswamy/aide/internal/prefs"
)

const selectionTestConfig = `
agents:
  claude: {binary: claude}
  pi: {binary: pi}
profiles:
  work:
    agent: claude
    credentials:
      anthropic: {backend: age, store: work, key: anthropic_api_key}
      openai: {backend: age, store: work, key: openai_api_key}
    default_credentials: [anthropic]
  personal:
    agent: pi
default_profile: work
`

// selectionEnv isolates HOME and the config, writes configYAML and
// makes the working directory a git repository with an origin remote.
// It returns the preferences file path.
func selectionEnv(t *testing.T, configYAML string) string {
	t.Helper()
	dir := isolatedConfigDir(t)
	if err := os.WriteFile(filepath.Join(dir, "xdg", "aide", "config.yaml"), []byte(configYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(dir, "repo")
	for _, args := range [][]string{{"init", "-q", repo}, {"-C", repo, "remote", "add", "origin", "git@github.com:org/repo.git"}} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	t.Chdir(repo)
	return prefs.DefaultPath(dir)
}

func runSelectionJSON(t *testing.T, cmd *cobra.Command, args ...string) map[string]any {
	t.Helper()
	output.RegisterFlag(cmd)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(append([]string{"--format", "json"}, args...))
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v\noutput: %s", err, out.String())
	}
	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v\noutput: %s", err, out.String())
	}
	return got
}

func names(v any) []string {
	var out []string
	list, _ := v.([]any)
	for _, x := range list {
		s, _ := x.(string)
		out = append(out, s)
	}
	return out
}

func TestWhich_ReportsSelectionSources(t *testing.T) {
	selectionEnv(t, selectionTestConfig)
	got := runSelectionJSON(t, whichCmd())
	if got["context_name"] != "work" || got["profile_source"] != "default" ||
		got["harness_source"] != "profile" || got["credential_source"] != "profile" {
		t.Errorf("sources = %v/%v/%v for %v", got["profile_source"], got["harness_source"], got["credential_source"], got["context_name"])
	}
	if sel := names(got["credential_selection"]); !slices.Equal(sel, []string{"anthropic"}) {
		t.Errorf("credential_selection = %v, want [anthropic]", sel)
	}
}

func TestWhich_HonorsSavedPreferenceAndFlags(t *testing.T) {
	path := selectionEnv(t, selectionTestConfig)
	if err := prefs.Update(path, func(f *prefs.File) error {
		f.Set(prefs.Key{Profile: "work", Remote: "github.com/org/repo"},
			prefs.Preference{Harness: "pi", Credentials: prefs.Selection("openai")})
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	got := runSelectionJSON(t, whichCmd())
	if got["agent_name"] != "pi" || got["harness_source"] != "preference" || got["credential_source"] != "preference" {
		t.Errorf("saved preference not applied: agent %v from %v, credentials from %v", got["agent_name"], got["harness_source"], got["credential_source"])
	}
	if sel := names(got["credential_selection"]); !slices.Equal(sel, []string{"openai"}) {
		t.Errorf("credential_selection = %v, want [openai]", sel)
	}

	got = runSelectionJSON(t, whichCmd(), "--agent", "claude", "--credential", "anthropic")
	if got["agent_name"] != "claude" || got["harness_source"] != "flag" || got["credential_source"] != "flag" {
		t.Errorf("flags did not win: agent %v from %v, credentials from %v", got["agent_name"], got["harness_source"], got["credential_source"])
	}

	got = runSelectionJSON(t, whichCmd(), "--profile", "personal")
	if got["context_name"] != "personal" || got["profile_source"] != "flag" || got["agent_name"] != "pi" || got["harness_source"] != "profile" {
		t.Errorf("--profile personal: got %v from %v, agent %v from %v", got["context_name"], got["profile_source"], got["agent_name"], got["harness_source"])
	}
}

func TestStatus_ReportsSelectionSources(t *testing.T) {
	selectionEnv(t, selectionTestConfig)
	got := runSelectionJSON(t, statusCmd(), "--profile", "work", "--auth", "login")
	if got["profile_source"] != "flag" || got["harness_source"] != "profile" || got["credential_source"] != "auth" {
		t.Errorf("sources = %v/%v/%v", got["profile_source"], got["harness_source"], got["credential_source"])
	}
	if _, ok := got["credential_selection"]; ok {
		t.Errorf("login selects no credentials, got %v", got["credential_selection"])
	}
}

func TestStatus_HumanSelectionLine(t *testing.T) {
	selectionEnv(t, selectionTestConfig)
	cmd := statusCmd()
	output.RegisterFlag(cmd)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--profile", "personal"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "profile from flag, harness from profile, credentials from profile") {
		t.Errorf("missing selection line:\n%s", out.String())
	}
}

func TestSelectionFlags_RejectUnknownAuth(t *testing.T) {
	selectionEnv(t, selectionTestConfig)
	cmd := whichCmd()
	cmd.SetArgs([]string{"--auth", "oauth"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "login") {
		t.Fatalf("error = %v, want --auth guidance", err)
	}
}
