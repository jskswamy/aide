package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/jskswamy/aide/internal/testutil"
)

// Goldens lock the output of validate, which and status so that
// refactors behind them can be checked byte for byte. Regenerate with
// go test ./cmd/aide/ -run Golden -update.

func TestGolden_ValidateOK(t *testing.T) {
	selectionEnv(t, `
agents:
  claude: {binary: golden-claude}
profiles:
  work:
    agent: claude
    match:
      - path: ~/repo
default_profile: work
`)
	home, _ := os.UserHomeDir()
	fakeAgentBin(t, home, "golden-claude")
	out, err := runValidate(t)
	if err != nil {
		t.Fatalf("validate: %v\n%s", err, out)
	}
	testutil.AssertGolden(t, "validate_ok.txt", testutil.NormalizeGolden(t, out, map[string]string{home: "$HOME"}))
}

func TestGolden_ValidateErrors(t *testing.T) {
	selectionEnv(t, `
agents:
  claude: {binary: golden-claude}
  ghost: {binary: aide-golden-no-such-binary}
profiles:
  work:
    agent: claude
    match:
      - path: ~/repo
  unknown-agent:
    agent: nobody
    match:
      - path: /never/a
  missing-secret:
    agent: claude
    secret: missing-store
    match:
      - path: /never/b
  bad-template:
    agent: claude
    env:
      BROKEN: "{{ .bad"
    match:
      - path: /never/c
  secret-without-store:
    agent: claude
    env:
      TOKEN: "{{ .secrets.token }}"
    match:
      - path: /never/d
  bad-sandbox:
    agent: claude
    sandbox: no-such-sandbox
    match:
      - path: /never/e
  no-match: {agent: claude}
default_profile: work
`)
	home, _ := os.UserHomeDir()
	fakeAgentBin(t, home, "golden-claude")
	out, err := runValidate(t)
	if err == nil {
		t.Fatalf("validate: want an error, got nil\n%s", out)
	}
	got := out + "exit: error: " + err.Error() + "\n"
	testutil.AssertGolden(t, "validate_errors.txt", testutil.NormalizeGolden(t, got, map[string]string{home: "$HOME"}))
}

const goldenCapabilitiesConfig = `
agents:
  claude: {binary: golden-claude}
profiles:
  work:
    agent: claude
    capabilities: [go]
    sandbox:
      readable_extra: ["~/data"]
      writable_extra: ["~/out"]
      network:
        mode: outbound
        allow_ports: [443]
default_profile: work
`

func TestGolden_WhichStatus(t *testing.T) {
	basic := strings.NewReplacer("binary: claude", "binary: golden-claude", "binary: pi", "binary: golden-pi").Replace(selectionTestConfig)
	for _, fx := range []struct{ name, config string }{
		{"basic", basic},
		{"capabilities", goldenCapabilitiesConfig},
	} {
		t.Run(fx.name, func(t *testing.T) {
			selectionEnv(t, fx.config)
			home, _ := os.UserHomeDir()
			// claude and pi too: status looks agents up by harness name,
			// not by the configured binary.
			fakeAgentBin(t, home, "golden-claude", "golden-pi", "claude", "pi")
			for _, d := range []string{"data", "out", "go"} { // the policy drops paths that do not exist
				if err := os.MkdirAll(filepath.Join(home, d), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			for name, newCmd := range map[string]func() *cobra.Command{"which": whichCmd, "status": statusCmd} {
				first := goldenJSON(t, scrubHostProbes(runSelectionJSON(t, newCmd())))
				if second := goldenJSON(t, scrubHostProbes(runSelectionJSON(t, newCmd()))); second != first {
					t.Fatalf("%s output is not stable across runs\n--- first\n%s\n--- second\n%s", name, first, second)
				}
				testutil.AssertGolden(t, name+"_"+fx.name+".json", testutil.NormalizeGolden(t, first, map[string]string{home: "$HOME"}))
			}
		})
	}
}

// scrubHostProbes removes what which and status detect on the host
// rather than read from config: the nix-toolchain guard (active only
// where /nix/store exists) and the isolation tier, which depends on the
// OS and the kernel's Landlock support. What is left is the same on
// Linux and macOS, so one golden covers both.
func scrubHostProbes(v map[string]any) map[string]any {
	delete(v, "isolation_tier")
	delete(v, "sandbox_tier")
	if sb, ok := v["sandbox"].(map[string]any); ok {
		for _, key := range []string{"active", "skipped"} {
			list, _ := sb[key].([]any)
			kept := list[:0]
			for _, e := range list {
				if m, ok := e.(map[string]any); ok && m["name"] == "nix-toolchain" {
					continue
				}
				kept = append(kept, e)
			}
			if list != nil {
				sb[key] = kept
			}
		}
	}
	return v
}

func goldenJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return string(b) + "\n"
}

func runValidate(t *testing.T) (string, error) {
	t.Helper()
	cmd := validateCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{}) // nil would make cobra read the test binary's own flags
	err := cmd.Execute()
	return out.String(), err
}

// fakeAgentBin writes executable stubs to <dir>/bin and makes that
// directory the whole PATH (plus a git link, which selection needs),
// so goldens never depend on which agents the host has installed.
func fakeAgentBin(t *testing.T, dir string, names ...string) {
	t.Helper()
	bin := filepath.Join(dir, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(bin, n), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(git, filepath.Join(bin, "git")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
}
