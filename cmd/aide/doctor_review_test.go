package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/config"
	"github.com/jskswamy/aide/internal/trust"
)

// A rule that only the project file has must not produce a global fix.
func TestDoctorReview_RedundantRuleFromProjectFileTargetsProject(t *testing.T) {
	selectionEnv(t, "agents:\n  claude: {binary: claude}\nprofiles:\n  work:\n    agent: claude\n    sandbox:\n      readable_extra: [\"~/docs\"]\ndefault_profile: work\n")
	home, _ := os.UserHomeDir()
	if err := os.MkdirAll(filepath.Join(home, "docs", "api"), 0o750); err != nil {
		t.Fatal(err)
	}
	repo, _ := os.Getwd()
	body := "sandbox:\n  readable_extra: [\"~/docs/api\"]\n"
	path := filepath.Join(repo, ".aide.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := trust.DefaultStore().Trust(path, []byte(body)); err != nil {
		t.Fatal(err)
	}
	for _, f := range findingsByID(t)["sandbox.redundant-allow"] {
		fix, _ := f["fix"].(map[string]any)
		if slices.Contains(names(fix["argv"]), "--global") {
			t.Errorf("global fix for a rule only the project file has: %v", f)
		}
	}
}

func TestDoctorReview_CredentialBindingKeyMissing(t *testing.T) {
	selectionEnv(t, `
agents:
  claude: {binary: claude}
profiles:
  work:
    agent: claude
    credentials:
      anthropic: {backend: age, store: creds, key: anthropic_api_key}
    default_credentials: [anthropic]
default_profile: work
`)
	path := config.ResolveSecretPath("creds")
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("other: ENC[x]\nsops: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	f := findingsByID(t)["credential.key-missing"]
	if len(f) != 1 || f[0]["severity"] != "error" {
		t.Errorf("credential.key-missing = %v, want one error", f)
	}
}

func TestDoctorReview_LiteralSecretsTextIsNotATemplate(t *testing.T) {
	selectionEnv(t, `
agents:
  claude: {binary: claude}
profiles:
  work:
    agent: claude
    secret: work
    env:
      URL: "https://host/.secrets.token"
default_profile: work
`)
	path := config.ResolveSecretPath("work")
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("present: ENC[x]\nsops: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if f := findingsByID(t)["secret.key-missing"]; len(f) != 0 {
		t.Errorf("secret.key-missing = %v for a literal value", f)
	}
}

func TestDoctorReview_TrustStepNamesTheFile(t *testing.T) {
	selectionEnv(t, "agents:\n  claude: {binary: claude}\nprofiles:\n  work: {agent: claude}\ndefault_profile: work\n")
	repo, _ := os.Getwd()
	if err := os.WriteFile(filepath.Join(repo, ".aide.yaml"), []byte("capabilities: [go]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(repo, "pkg")
	if err := os.MkdirAll(sub, 0o750); err != nil {
		t.Fatal(err)
	}
	t.Chdir(sub)
	f := findingsByID(t)["project.untrusted"]
	if len(f) != 1 {
		t.Fatalf("project.untrusted = %v", f)
	}
	fix, _ := f[0]["fix"].(map[string]any)
	steps := names(fix["steps"])
	last := steps[len(steps)-1]
	if !strings.HasPrefix(last, "Run: aide trust ") || !strings.HasSuffix(last, filepath.Join("repo", ".aide.yaml")) {
		t.Errorf("last step = %q, want aide trust with the file's path", last)
	}
}

func TestDoctorReview_NoMigrationNudgeForDoctor(t *testing.T) {
	if shouldNudgeForCmd(doctorCmd()) {
		t.Error("aide doctor must not run the migration reminder, which writes state")
	}
}

func TestDoctorReview_RelativeProjectRulesKeepTheirText(t *testing.T) {
	selectionEnv(t, "agents:\n  claude: {binary: claude}\nprofiles:\n  work: {agent: claude}\ndefault_profile: work\n")
	repo, _ := os.Getwd()
	if err := os.MkdirAll(filepath.Join(repo, "data", "api"), 0o750); err != nil {
		t.Fatal(err)
	}
	body := "sandbox:\n  readable_extra: [data, data/api]\n"
	path := filepath.Join(repo, ".aide.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := trust.DefaultStore().Trust(path, []byte(body)); err != nil {
		t.Fatal(err)
	}
	for _, f := range findingsByID(t)["sandbox.redundant-allow"] {
		fix, _ := f["fix"].(map[string]any)
		for _, a := range names(fix["argv"]) {
			if strings.HasPrefix(a, "/") {
				t.Errorf("fix uses a rewritten absolute path %q; the file says data/api: %v", a, f)
			}
		}
	}
}

// Relative rules in a nested .aide.yaml resolve against the checkout
// root, as the loader does, not the file's own directory.
func TestDoctorReview_RelativeRulesUseCheckoutRoot(t *testing.T) {
	selectionEnv(t, "agents:\n  claude: {binary: claude}\nprofiles:\n  work: {agent: claude}\ndefault_profile: work\n")
	repo, _ := os.Getwd()
	sub := filepath.Join(repo, "sub")
	for _, p := range []string{filepath.Join(sub, "data", "api"), filepath.Join(repo, "data", "api")} {
		if err := os.MkdirAll(p, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	body := "sandbox:\n  readable_extra: [" + filepath.Join(sub, "data") + ", data/api]\n"
	path := filepath.Join(sub, ".aide.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := trust.DefaultStore().Trust(path, []byte(body)); err != nil {
		t.Fatal(err)
	}
	t.Chdir(sub)
	if f := findingsByID(t)["sandbox.redundant-allow"]; len(f) != 0 {
		t.Errorf("data/api means <repo>/data/api, not under sub/data; got %v", f)
	}
}

// Launch refuses a selected keyless gateway off loopback; doctor says so.
func TestDoctorReview_KeylessRemoteGatewayFails(t *testing.T) {
	selectionEnv(t, `
agents:
  claude: {binary: claude}
profiles:
  work:
    agent: claude
    gateways:
      corp: {base_url: "https://llm.corp.example", no_auth: true, agents: [claude]}
    gateway_choice: {claude: corp}
default_profile: work
`)
	got, err := execDoctor(t)
	if !findingIDs(got)["gateway.target-refused"] {
		t.Fatalf("findings = %v, want gateway.target-refused", got["findings"])
	}
	if err == nil {
		t.Error("doctor: want an error exit, since launch would refuse the gateway")
	}
}
