package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/output"
	"github.com/jskswamy/aide/internal/prefs"
)

func runProject(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := projectCmd()
	output.RegisterFlag(cmd)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func savedFor(t *testing.T, path, profile string) (prefs.Preference, bool) {
	t.Helper()
	f, err := prefs.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return f.Get(prefs.Key{Profile: profile, Remote: "github.com/org/repo"})
}

func TestProjectSet_SavesHarnessAndCredentialsWithoutTouchingRepoOrConfig(t *testing.T) {
	path := selectionEnv(t, selectionTestConfig)
	cfgPath := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "aide", "config.yaml")
	cfgBefore, _ := os.ReadFile(cfgPath)

	out, err := runProject(t, "set", "--agent", "pi", "--credential", "openai")
	if err != nil {
		t.Fatalf("project set: %v\n%s", err, out)
	}
	p, ok := savedFor(t, path, "work")
	if !ok || p.Harness != "pi" || p.Credentials == nil || !slices.Equal(*p.Credentials, []string{"openai"}) {
		t.Errorf("saved = %+v, %v", p, ok)
	}
	if !strings.Contains(out, "github.com/org/repo") || !strings.Contains(out, `"work"`) {
		t.Errorf("output should name the profile and repository:\n%s", out)
	}
	if _, err := os.Stat(".aide.yaml"); !os.IsNotExist(err) {
		t.Error("project set wrote .aide.yaml")
	}
	if cfgAfter, _ := os.ReadFile(cfgPath); !bytes.Equal(cfgBefore, cfgAfter) {
		t.Error("project set changed config.yaml")
	}
	entries, _ := os.ReadDir(".")
	for _, e := range entries {
		if e.Name() != ".git" {
			t.Errorf("project set created %s in the repository", e.Name())
		}
	}

	// A second set changes only what it names.
	if out, err := runProject(t, "set", "--credential", "anthropic"); err != nil {
		t.Fatalf("project set: %v\n%s", err, out)
	}
	p, _ = savedFor(t, path, "work")
	if p.Harness != "pi" || !slices.Equal(*p.Credentials, []string{"anthropic"}) {
		t.Errorf("after second set = %+v", p)
	}
}

func TestProjectSet_NoCredentialsSavesExplicitEmptySelection(t *testing.T) {
	path := selectionEnv(t, selectionTestConfig)
	if out, err := runProject(t, "set", "--no-credentials"); err != nil {
		t.Fatalf("project set: %v\n%s", err, out)
	}
	p, ok := savedFor(t, path, "work")
	if !ok || p.Credentials == nil || len(*p.Credentials) != 0 {
		t.Errorf("saved = %+v, want explicit empty selection", p)
	}
	got := runSelectionJSON(t, whichCmd())
	if got["credential_source"] != "preference" {
		t.Errorf("credential_source = %v, want preference", got["credential_source"])
	}
	if _, ok := got["credential_selection"]; ok {
		t.Errorf("explicit empty selection still selected %v", got["credential_selection"])
	}
}

func TestProjectSet_ScopedToSelectedProfile(t *testing.T) {
	path := selectionEnv(t, selectionTestConfig)
	if out, err := runProject(t, "set", "--profile", "personal", "--agent", "claude"); err != nil {
		t.Fatalf("project set: %v\n%s", err, out)
	}
	if _, ok := savedFor(t, path, "work"); ok {
		t.Error("--profile personal saved under work")
	}
	if p, _ := savedFor(t, path, "personal"); p.Harness != "claude" {
		t.Errorf("personal = %+v", p)
	}
}

func TestProjectSet_RejectsInvalidSelections(t *testing.T) {
	path := selectionEnv(t, selectionTestConfig)
	for name, args := range map[string][]string{
		"nothing to set":     {"set"},
		"unknown credential": {"set", "--credential", "nope"},
		"other profile's":    {"set", "--profile", "personal", "--credential", "anthropic"},
		"unknown harness":    {"set", "--agent", "no-such-harness-xyz"},
		"conflicting":        {"set", "--credential", "openai", "--no-credentials"},
		"unknown profile":    {"set", "--profile", "ghost", "--agent", "pi"},
	} {
		t.Run(name, func(t *testing.T) {
			if out, err := runProject(t, args...); err == nil {
				t.Errorf("project %v succeeded:\n%s", args, out)
			}
		})
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("a rejected set wrote the preferences file")
	}
}

func TestProjectSet_RefusesWithoutRepositoryIdentity(t *testing.T) {
	path := selectionEnv(t, selectionTestConfig)
	t.Chdir(t.TempDir())
	out, err := runProject(t, "set", "--agent", "pi")
	if err == nil || !strings.Contains(err.Error(), "origin remote") {
		t.Fatalf("project set outside a repository: err = %v\n%s", err, out)
	}
	if _, err := runProject(t, "clear"); err == nil {
		t.Error("project clear outside a repository should refuse")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("preferences file written without a repository identity")
	}
}

func TestProjectClear_ClearsFieldsIndependently(t *testing.T) {
	path := selectionEnv(t, selectionTestConfig)
	if out, err := runProject(t, "set", "--agent", "pi", "--credential", "openai"); err != nil {
		t.Fatalf("project set: %v\n%s", err, out)
	}
	if out, err := runProject(t, "clear", "--credential"); err != nil {
		t.Fatalf("project clear: %v\n%s", err, out)
	}
	p, ok := savedFor(t, path, "work")
	if !ok || p.Harness != "pi" || p.Credentials != nil {
		t.Errorf("after clear --credential = %+v, want harness kept and credentials unset", p)
	}
	if out, err := runProject(t, "clear"); err != nil {
		t.Fatalf("project clear: %v\n%s", err, out)
	}
	if _, ok := savedFor(t, path, "work"); ok {
		t.Error("clear without flags left a preference")
	}
}

func TestProjectShow_JSON(t *testing.T) {
	selectionEnv(t, selectionTestConfig)
	if out, err := runProject(t, "set", "--agent", "pi", "--no-credentials"); err != nil {
		t.Fatalf("project set: %v\n%s", err, out)
	}
	out, err := runProject(t, "show", "--format", "json")
	if err != nil {
		t.Fatalf("project show: %v\n%s", err, out)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, out)
	}
	if got["profile"] != "work" || got["repository"] != "github.com/org/repo" || got["harness"] != "pi" {
		t.Errorf("show = %v", got)
	}
	if creds, ok := got["credentials"].([]any); !ok || len(creds) != 0 {
		t.Errorf("credentials = %#v, want explicit []", got["credentials"])
	}

	if _, err := runProject(t, "clear", "--credential"); err != nil {
		t.Fatal(err)
	}
	out, _ = runProject(t, "show", "--format", "json")
	_ = json.Unmarshal([]byte(out), &got)
	if v, ok := got["credentials"]; !ok || v != nil {
		t.Errorf("unset credentials = %#v, want null", v)
	}
}

func TestProjectShow_HumanWithoutRepository(t *testing.T) {
	selectionEnv(t, selectionTestConfig)
	t.Chdir(t.TempDir())
	out, err := runProject(t, "show")
	if err != nil {
		t.Fatalf("project show: %v", err)
	}
	if !strings.Contains(out, "origin remote") {
		t.Errorf("show should explain the missing identity:\n%s", out)
	}
}

func TestProjectClearReset_RecoversCorruptFile(t *testing.T) {
	path := selectionEnv(t, selectionTestConfig)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{not json`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := runProject(t, "clear"); err == nil || !strings.Contains(err.Error(), "--reset") {
		t.Fatalf("clear on a corrupt file: err = %v, want the --reset remedy", err)
	}
	out, err := runProject(t, "clear", "--reset")
	if err != nil {
		t.Fatalf("clear --reset: %v\n%s", err, out)
	}
	matches, _ := filepath.Glob(path + ".corrupt-*")
	if len(matches) != 1 || !strings.Contains(out, matches[0]) {
		t.Errorf("clear --reset should move the file aside and say where (%v):\n%s", matches, out)
	}
	if out, err := runProject(t, "set", "--agent", "pi"); err != nil {
		t.Fatalf("set after reset: %v\n%s", err, out)
	}
}

func TestProjectClearReset_ConflictsWithFieldFlags(t *testing.T) {
	selectionEnv(t, selectionTestConfig)
	if _, err := runProject(t, "clear", "--reset", "--agent"); err == nil {
		t.Error("--reset with --agent should be refused")
	}
}

func TestProjectClear_NothingSavedWritesNothing(t *testing.T) {
	path := selectionEnv(t, selectionTestConfig)
	out, err := runProject(t, "clear")
	if err != nil || !strings.Contains(out, "Nothing saved") {
		t.Fatalf("clear: %v\n%s", err, out)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("clear with nothing saved created the preferences file")
	}
}
