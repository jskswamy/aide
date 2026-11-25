package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/jskswamy/aide/internal/output"
	"github.com/jskswamy/aide/internal/testutil"
)

func execDoctor(t *testing.T, args ...string) (map[string]any, error) {
	t.Helper()
	if _, set := os.LookupEnv("AIDE_SANDBOX"); !set {
		t.Setenv("AIDE_SANDBOX", "") // tests may themselves run inside an aide session
	}
	cmd := doctorCmd()
	output.RegisterFlag(cmd)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs(append([]string{"--format", "json"}, args...))
	err := cmd.Execute()
	var got map[string]any
	if jerr := json.Unmarshal(out.Bytes(), &got); jerr != nil {
		t.Fatalf("unmarshal: %v\noutput: %s", jerr, out.String())
	}
	return got, err
}

func findingIDs(r map[string]any) map[string]bool {
	ids := map[string]bool{}
	list, _ := r["findings"].([]any)
	for _, f := range list {
		m, _ := f.(map[string]any)
		id, _ := m["id"].(string)
		ids[id] = true
	}
	return ids
}

func TestDoctor_GoldenReport(t *testing.T) {
	t.Setenv("AIDE_SANDBOX", "")
	selectionEnv(t, goldenCapabilitiesConfig)
	home, _ := os.UserHomeDir()
	fakeAgentBin(t, home, "golden-claude", "claude")
	for _, d := range []string{"data", "out", "go"} {
		if err := os.MkdirAll(filepath.Join(home, d), 0o750); err != nil {
			t.Fatal(err)
		}
	}
	got, err := execDoctor(t)
	if err != nil {
		t.Fatalf("doctor: %v", err)
	}
	if got["schema_version"] != float64(1) {
		t.Errorf("schema_version = %v, want 1", got["schema_version"])
	}
	delete(got, "aide_version")
	testutil.AssertGolden(t, "doctor_capabilities.json", testutil.NormalizeGolden(t, goldenJSON(t, got), map[string]string{home: "$HOME"}))
}

func TestDoctor_ConfigErrorExitsNonZero(t *testing.T) {
	selectionEnv(t, `
profiles:
  work: {agent: nobody}
  other: {agent: claude, match: [{path: /x}]}
agents:
  claude: {binary: claude}
default_profile: work
`)
	got, err := execDoctor(t)
	if err == nil {
		t.Fatal("doctor: want an error exit with an error finding")
	}
	if !findingIDs(got)["config.unknown-agent"] {
		t.Errorf("findings = %v, want config.unknown-agent", got["findings"])
	}
	counts, _ := got["counts"].(map[string]any)
	if counts["errors"] != float64(1) {
		t.Errorf("counts = %v", counts)
	}
}

func TestDoctor_BrokenConfigIsAFinding(t *testing.T) {
	selectionEnv(t, "profiles: [not, a, map\n")
	repo, _ := os.Getwd()
	if err := os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := execDoctor(t)
	if err == nil {
		t.Fatal("doctor: want an error exit for a config that does not load")
	}
	if !findingIDs(got)["config.load-failed"] {
		t.Errorf("findings = %v, want config.load-failed", got["findings"])
	}
	dir, _ := got["directory"].(map[string]any)
	if dir["profile"] != "" || !slices.Contains(names(dir["detected_capabilities"]), "go") {
		t.Errorf("directory = %v, want no profile but the project's markers (go)", dir)
	}
}

func TestDoctor_InsideSandbox(t *testing.T) {
	selectionEnv(t, "profiles:\n  work: {agent: claude}\ndefault_profile: work\n")
	for value, want := range map[string]bool{"on": true, "off": false} {
		t.Setenv("AIDE_SANDBOX", value)
		got, _ := execDoctor(t)
		if got["inside_sandbox"] != want {
			t.Errorf("AIDE_SANDBOX=%s: inside_sandbox = %v, want %v", value, got["inside_sandbox"], want)
		}
	}
}
