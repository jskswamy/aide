package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/config"
)

// findingsByID runs doctor and indexes its findings.
func findingsByID(t *testing.T) map[string][]map[string]any {
	t.Helper()
	got, _ := execDoctor(t)
	out := map[string][]map[string]any{}
	list, _ := got["findings"].([]any)
	for _, f := range list {
		m, _ := f.(map[string]any)
		id, _ := m["id"].(string)
		out[id] = append(out[id], m)
	}
	return out
}

func TestDoctorHere_AgentNotFoundOwnsTheMissingBinary(t *testing.T) {
	selectionEnv(t, "agents:\n  claude: {binary: aide-golden-missing}\nprofiles:\n  work: {agent: claude}\ndefault_profile: work\n")
	home, _ := os.UserHomeDir()
	fakeAgentBin(t, home)
	f := findingsByID(t)
	if len(f["agent.not-found"]) != 1 || f["agent.not-found"][0]["severity"] != "error" {
		t.Errorf("agent.not-found = %v, want one error", f["agent.not-found"])
	}
	if len(f["config.agent-not-on-path"]) != 0 {
		t.Errorf("config.agent-not-on-path = %v, want it dropped for the current agent", f["config.agent-not-on-path"])
	}
}

func TestDoctorHere_UnknownCapabilityFailsTheSandbox(t *testing.T) {
	selectionEnv(t, "agents:\n  claude: {binary: claude}\nprofiles:\n  work: {agent: claude, capabilities: [goo]}\ndefault_profile: work\n")
	if f := findingsByID(t); len(f["sandbox.resolve-failed"]) != 1 {
		t.Errorf("findings = %v, want sandbox.resolve-failed", f)
	}
}

func TestDoctorHere_UntrustedProjectConfig(t *testing.T) {
	selectionEnv(t, "agents:\n  claude: {binary: claude}\nprofiles:\n  work: {agent: claude}\ndefault_profile: work\n")
	repo, _ := os.Getwd()
	if err := os.WriteFile(filepath.Join(repo, ".aide.yaml"), []byte("capabilities: [go]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	f := findingsByID(t)
	if len(f["project.untrusted"]) != 1 {
		t.Fatalf("findings = %v, want project.untrusted", f)
	}
	fix, _ := f["project.untrusted"][0]["fix"].(map[string]any)
	steps := names(fix["steps"])
	if len(steps) == 0 || !strings.HasPrefix(steps[len(steps)-1], "Run: aide trust ") {
		t.Errorf("steps = %v, want review then aide trust", steps)
	}
}

func TestDoctorHere_SecretKeyMissingWithoutDecrypting(t *testing.T) {
	selectionEnv(t, `
agents:
  claude: {binary: claude}
profiles:
  work:
    agent: claude
    secret: work
    env:
      TOKEN: "{{ .secrets.token }}"
      OTHER: "{{ .secrets.present }}"
default_profile: work
`)
	path := config.ResolveSecretPath("work")
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	// Key names are in clear in a sops file; values never read.
	if err := os.WriteFile(path, []byte("present: ENC[AES256_GCM,data:x]\nsops: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	f := findingsByID(t)
	if len(f["secret.key-missing"]) != 1 || f["secret.key-missing"][0]["subject"] != "work.TOKEN" {
		t.Errorf("secret.key-missing = %v, want one for TOKEN", f["secret.key-missing"])
	}
}

func TestDoctorHere_VariantConsentPending(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	selectionEnv(t, "agents:\n  claude: {binary: claude}\nprofiles:\n  work: {agent: claude, capabilities: [python]}\ndefault_profile: work\n")
	repo, _ := os.Getwd()
	if err := os.WriteFile(filepath.Join(repo, "uv.lock"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if f := findingsByID(t); len(f["capability.consent-pending"]) != 1 {
		t.Errorf("findings = %v, want capability.consent-pending for python", f)
	}
}
