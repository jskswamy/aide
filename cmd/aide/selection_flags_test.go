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

	"github.com/jskswamy/aide/internal/launcher"
	"github.com/jskswamy/aide/internal/output"
	"github.com/jskswamy/aide/internal/prefs"
	"github.com/jskswamy/aide/internal/trust"
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
    config_dir_name: personal
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

func TestPrompt_ShowsSavedHarness(t *testing.T) {
	path := selectionEnv(t, selectionTestConfig)
	if err := prefs.Update(path, func(f *prefs.File) error {
		f.Set(prefs.Key{Profile: "work", Remote: "github.com/org/repo"}, prefs.Preference{Harness: "pi"})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	got := runSelectionJSON(t, promptCmd())
	line, _ := got["line"].(string)
	if pi := launcher.ResolveAgentIcon("pi", nil); pi == "" || !strings.Contains(line, pi) {
		t.Errorf("prompt %q should show pi's icon %q", line, pi)
	}
}

func TestProfileScopedCommandsAcceptProfile(t *testing.T) {
	for name, cmd := range map[string]*cobra.Command{
		"cap list":          capListCmd(),
		"cap audit":         capAuditCmd(),
		"env list":          envListCmd(),
		"hook list":         hookListCmd(),
		"plugin list":       pluginListCmd(),
		"mcp list":          mcpListCmd(),
		"sandbox show":      sandboxShowCmd(),
		"sandbox test":      sandboxTestCmd(),
		"sync":              syncCmd(),
		"adopt":             adoptCmd(),
		"statusline":        statuslineCmd(),
		"statusline claude": statuslineAgentCmd("claude"),
	} {
		if cmd.Flags().Lookup("profile") == nil {
			t.Errorf("%s has no --profile", name)
		}
	}
}

func TestEnvList_ProfileSelectsLikeContext(t *testing.T) {
	selectionEnv(t, `
profiles:
  work: {agent: claude, env: {WORK_ONLY: "1"}}
  personal: {agent: claude, env: {PERSONAL_ONLY: "1"}}
default_profile: work
`)
	run := func(args ...string) (string, error) {
		cmd := envListCmd()
		output.RegisterFlag(cmd)
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		cmd.SetArgs(args)
		err := cmd.Execute()
		return out.String(), err
	}
	out, err := run("--profile", "personal")
	if err != nil || !strings.Contains(out, "PERSONAL_ONLY") || strings.Contains(out, "WORK_ONLY") {
		t.Errorf("env list --profile personal: %v\n%s", err, out)
	}
	if _, err := run("--profile", "personal", "--context", "work"); err == nil {
		t.Error("--profile and --context naming different profiles should conflict")
	}
	if out, err := run("--profile", "personal", "--context", "personal"); err != nil {
		t.Errorf("agreeing --profile and --context: %v\n%s", err, out)
	}
}

// writeProjectConfig writes .aide.yaml into the working directory and,
// when trusted, trusts it in the default store.
func writeProjectConfig(t *testing.T, body string, trusted bool) {
	t.Helper()
	if err := os.WriteFile(".aide.yaml", []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if trusted {
		abs, _ := filepath.Abs(".aide.yaml")
		if err := trust.DefaultStore().Trust(abs, []byte(body)); err != nil {
			t.Fatal(err)
		}
	}
}

func TestWhichStatus_UntrustedProjectConfigContributesNothing(t *testing.T) {
	selectionEnv(t, strings.Replace(selectionTestConfig,
		"openai: {backend: age, store: work, key: openai_api_key}",
		"openai: {backend: age, store: work, key: openai_api_key, harnesses: [claude]}", 1))
	writeProjectConfig(t, "agent: pi\nprofile: personal\n", false)

	for name, cmd := range map[string]*cobra.Command{"which": whichCmd(), "status": statusCmd()} {
		got := runSelectionJSON(t, cmd)
		agent := got["agent_name"]
		if agent == nil {
			agent = got["agent"]
		}
		if agent != "claude" || got["harness_source"] != "profile" {
			t.Errorf("%s: agent %v from %v, want claude from profile as at launch", name, agent, got["harness_source"])
		}
		for _, key := range []string{"warnings", "notices"} {
			for _, w := range names(got[key]) {
				if strings.Contains(w, "personal") || strings.Contains(w, `"pi"`) {
					t.Errorf("%s: untrusted .aide.yaml produced %s %q", name, key, w)
				}
			}
		}
	}

	// openai allows only claude: judged against the untrusted pi it
	// would be refused.
	if out, err := runProject(t, "set", "--credential", "openai"); err != nil {
		t.Errorf("project set judged against the untrusted agent: %v\n%s", err, out)
	}
}

func TestWhichStatus_ReportNotices(t *testing.T) {
	path := selectionEnv(t, selectionTestConfig)
	if err := prefs.Update(path, func(f *prefs.File) error {
		f.Set(prefs.Key{Profile: "work", Remote: "github.com/org/repo"}, prefs.Preference{Harness: "claude"})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	writeProjectConfig(t, "agent: pi\nprofile: personal\n", true)

	which := runSelectionJSON(t, whichCmd())
	status := runSelectionJSON(t, statusCmd())
	for name, list := range map[string][]string{"which warnings": names(which["warnings"]), "status notices": names(status["notices"])} {
		joined := strings.Join(list, "\n")
		if !strings.Contains(joined, "--profile personal") {
			t.Errorf("%s lack the profile notice:\n%s", name, joined)
		}
		if !strings.Contains(joined, `harness "claude" saved`) {
			t.Errorf("%s lack the saved-harness notice:\n%s", name, joined)
		}
	}
}

func TestPassthroughSelectionError(t *testing.T) {
	for name, tc := range map[string]struct {
		f       selectionFlags
		in      launcher.SelectionInputs
		refused bool
	}{
		"none":       {},
		"profile":    {f: selectionFlags{profile: "work"}, refused: true},
		"credential": {in: launcher.SelectionInputs{CredentialsSet: true}, refused: true},
		"auth api":   {in: launcher.SelectionInputs{Auth: launcher.AuthAPI}, refused: true},
		"auth login": {in: launcher.SelectionInputs{Auth: launcher.AuthLogin}},
	} {
		err := passthroughSelectionError(&tc.f, tc.in, "/cfg/config.yaml")
		if (err != nil) != tc.refused {
			t.Errorf("%s: err = %v, refused want %v", name, err, tc.refused)
		}
	}
}

// Every command that takes --context also takes --profile, and
// --context is hidden as the deprecated alias.
func TestContextFlagIsDeprecatedAliasEverywhere(t *testing.T) {
	n := 0
	walkCommands(buildTestRoot(), func(c *cobra.Command) {
		ctx := c.Flags().Lookup("context")
		if ctx == nil {
			return
		}
		n++
		if c.Flags().Lookup("profile") == nil {
			t.Errorf("%s has --context but no --profile", c.CommandPath())
		}
		if !ctx.Hidden {
			t.Errorf("%s: --context is not hidden", c.CommandPath())
		}
	})
	if n == 0 {
		t.Fatal("no command has --context; the walk found nothing")
	}
}

func TestContextFlagPrintsDeprecationOnStderr(t *testing.T) {
	selectionEnv(t, `
profiles:
  work: {agent: claude, env: {WORK_ONLY: "1"}}
default_profile: work
`)
	cmd := envListCmd()
	output.RegisterFlag(cmd)
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs([]string{"--context", "work", "--format", "json"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(errOut.String(), "--context is deprecated; use --profile") {
		t.Errorf("stderr = %q", errOut.String())
	}
	var v any
	if err := json.Unmarshal(out.Bytes(), &v); err != nil {
		t.Errorf("stdout is not JSON: %v\n%s", err, out.String())
	}
}

// which/status report a launch the shared global login would refuse,
// and validate fails on it, while the config itself still loads.
func TestWhichStatusValidate_ReportSharedGlobalLogin(t *testing.T) {
	selectionEnv(t, `
profiles:
  work: {agent: claude}
  personal: {agent: claude}
default_profile: work
`)
	for name, cmd := range map[string]*cobra.Command{"which": whichCmd(), "status": statusCmd()} {
		got := runSelectionJSON(t, cmd)
		var found bool
		for _, key := range []string{"warnings", "notices"} {
			for _, w := range names(got[key]) {
				found = found || (strings.Contains(w, "launch would fail") && strings.Contains(w, `"personal"`))
			}
		}
		if !found {
			t.Errorf("%s: no shared global login notice in %v", name, got)
		}
	}

	cmd := validateCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(nil)
	if err := cmd.Execute(); err == nil || !strings.Contains(out.String(), "~/.claude") {
		t.Errorf("validate: err = %v, want a shared-login error\n%s", err, out.String())
	}
}
