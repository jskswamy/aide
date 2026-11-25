package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/doctor"
	"github.com/jskswamy/aide/internal/trust"
)

func suggestedGo(t *testing.T) map[string]any {
	t.Helper()
	for _, f := range findingsByID(t)["capability.suggested"] {
		if f["subject"] == "go" {
			return f
		}
	}
	t.Fatal("no capability.suggested finding for go")
	return nil
}

func TestDoctorFix_SuggestsCapabilityFromMarker(t *testing.T) {
	selectionEnv(t, "agents:\n  claude: {binary: claude}\nprofiles:\n  work: {agent: claude}\ndefault_profile: work\n")
	repo, _ := os.Getwd()
	if err := os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	f := suggestedGo(t)
	fix, _ := f["fix"].(map[string]any)
	if f["widens_access"] != true || len(names(fix["argv"])) != 0 || !slices.Contains(names(fix["steps"]), "Run: aide trust") {
		t.Errorf("without a trusted .aide.yaml: finding = %v, want steps ending in aide trust", f)
	}

	body := "capabilities: [nix]\n"
	path := filepath.Join(repo, ".aide.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := trust.DefaultStore().Trust(path, []byte(body)); err != nil {
		t.Fatal(err)
	}
	f = suggestedGo(t)
	fix, _ = f["fix"].(map[string]any)
	if !slices.Equal(names(fix["argv"]), []string{"aide", "cap", "enable", "go"}) {
		t.Errorf("with a trusted .aide.yaml: argv = %v, want aide cap enable go", fix["argv"])
	}
}

func TestDoctorFix_RedundantAllowInProfileBlock(t *testing.T) {
	selectionEnv(t, `
agents:
  claude: {binary: claude}
profiles:
  work:
    agent: claude
    sandbox:
      readable_extra: ["~/docs", "~/docs/api"]
default_profile: work
`)
	home, _ := os.UserHomeDir()
	if err := os.MkdirAll(filepath.Join(home, "docs", "api"), 0o750); err != nil {
		t.Fatal(err)
	}
	f := findingsByID(t)["sandbox.redundant-allow"]
	if len(f) != 1 || f[0]["subject"] != "~/docs/api" {
		t.Fatalf("sandbox.redundant-allow = %v, want one for ~/docs/api", f)
	}
	fix, _ := f[0]["fix"].(map[string]any)
	want := []string{"aide", "sandbox", "allow", "--remove", "~/docs/api", "--global", "--profile", "work"}
	if !slices.Equal(names(fix["argv"]), want) {
		t.Errorf("argv = %v, want %v", fix["argv"], want)
	}
}

func TestDoctorFix_AppliesAcceptedFixInProcess(t *testing.T) {
	dir := isolatedConfigDir(t)
	writeClaudeAgentConfig(t, dir, "claude")
	prof := filepath.Join(dir, "xdg", "aide", "profiles", "work.yaml")
	if err := os.WriteFile(prof, []byte("agent: claude\nsandbox:\n  readable_extra: [\"~/docs\", \"~/docs/api\"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "docs", "api"), 0o750); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AIDE_SANDBOX", "")
	restore := doctorFixHooks
	t.Cleanup(func() { doctorFixHooks = restore })
	var asked []string
	doctorFixHooks.interactive = func() bool { return true }
	doctorFixHooks.ask = func(f doctor.Finding) bool { asked = append(asked, f.Subject); return true }
	doctorFixHooks.run = func(argv []string) (string, int, error) {
		out, err := runSandboxCmd(t, argv[2:]...) // argv: aide sandbox …
		if err != nil {
			return out + err.Error(), 1, nil
		}
		return out, 0, nil
	}

	got, _ := execDoctor(t, "--fix")
	results, _ := got["fix_results"].([]any)
	if len(asked) != 1 || asked[0] != "~/docs/api" || len(results) != 1 {
		t.Fatalf("asked = %v, results = %v", asked, results)
	}
	if r, _ := results[0].(map[string]any); r["status"] != "ran" {
		t.Errorf("result = %v", r)
	}
	if b, _ := os.ReadFile(prof); strings.Contains(string(b), "~/docs/api") {
		t.Errorf("profile still has ~/docs/api:\n%s", b)
	}
	if len(findingIDs(got)) != 0 && findingIDs(got)["sandbox.redundant-allow"] {
		t.Errorf("report after --fix still lists the fixed rule: %v", got["findings"])
	}
}

func TestDoctorFix_RefusedInsideSandbox(t *testing.T) {
	selectionEnv(t, "profiles:\n  work: {agent: claude}\ndefault_profile: work\n")
	t.Setenv("AIDE_SANDBOX", "on")
	if _, err := execDoctor(t, "--fix"); err == nil || !strings.Contains(err.Error(), "inside an aide sandbox") {
		t.Errorf("err = %v, want a refusal inside the sandbox", err)
	}
}
