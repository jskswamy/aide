// cmd/aide/usage_test.go
package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/usage"
)

// usageHint is the verbatim InstallHint the not-found path must
// print, exactly once, to stderr.
const usageHint = "ccusage not found on PATH — install it: brew install ccusage · nixpkgs: pkgs.ccusage"

// setupUsageFixture builds a hermetic aide environment: an XDG config
// home with a default profile plus a forge profile matching
// <repo>/*, HOME pointed at a temp dir, and the process cwd inside
// the repo (so the CWD profile resolves to forge).
func setupUsageFixture(t *testing.T) (repo, home string) {
	t.Helper()
	dir := t.TempDir()
	repo = filepath.Join(dir, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	xdgHome := filepath.Join(dir, "xdg")
	aideDir := filepath.Join(xdgHome, "aide")
	if err := os.MkdirAll(filepath.Join(aideDir, "profiles"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeFile := func(path, body string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(filepath.Join(aideDir, "config.yaml"), "default_profile: default\n")
	writeFile(filepath.Join(aideDir, "profiles", "default.yaml"), "agent: claude\n")
	writeFile(filepath.Join(aideDir, "profiles", "forge.yaml"),
		"agent: claude\nconfig_dir_name: forge\nmatch:\n  - path: "+repo+"/*\n")
	home = t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdgHome)
	t.Setenv("HOME", home)
	t.Chdir(repo)
	return repo, home
}

// fakeCcusage installs a fake ccusage binary on a temp PATH dir. The
// fake records its argv plus its full environment into $CAPTURE,
// prints one line to stderr (so tests can assert what the ExitError
// path passes through), and exits with $FAKE_EXIT (default 0).
// /usr/bin:/bin stay on PATH so the script's `env` resolves.
func fakeCcusage(t *testing.T) (binDir, capture string) {
	t.Helper()
	binDir = t.TempDir()
	capture = filepath.Join(t.TempDir(), "capture")
	script := `#!/bin/sh
{
  printf 'argv=%s\n' "$*"
  env
} > "$CAPTURE"
printf 'child-err\n' >&2
exit "${FAKE_EXIT:-0}"
`
	if err := os.WriteFile(filepath.Join(binDir, "ccusage"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+"/usr/bin:/bin")
	t.Setenv("CAPTURE", capture)
	return binDir, capture
}

// captureEnv splits capture-file lines ("VAR=value", plus the fake's
// leading argv line) on the first "=" into a map.
func captureEnv(t *testing.T, data string) map[string]string {
	t.Helper()
	out := make(map[string]string)
	for _, line := range strings.Split(strings.TrimRight(data, "\n"), "\n") {
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			t.Fatalf("capture line %q has no '='", line)
		}
		out[k] = v
	}
	return out
}

func TestUsageCmd_Registered(t *testing.T) {
	root := buildTestRoot()
	for _, c := range root.Commands() {
		if c.Name() == "usage" {
			return
		}
	}
	t.Error("no 'usage' command registered on the root command")
}

func TestPassthroughArgs(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{"nil becomes daily", nil, []string{"daily"}},
		{"lone -- becomes daily", []string{"--"}, []string{"daily"}},
		{"args pass through unchanged", []string{"daily", "--json"}, []string{"daily", "--json"}},
		{"one leading -- stripped", []string{"--", "blocks", "--active"}, []string{"blocks", "--active"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := passthroughArgs(tc.in); !slices.Equal(got, tc.want) {
				t.Errorf("passthroughArgs(%v) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestUsageCmd_NotFoundPrintsHintAndExits1(t *testing.T) {
	setupUsageFixture(t)
	// PATH holds no ccusage: the binary lookup on the exec path fails.
	t.Setenv("PATH", t.TempDir())

	cmd := usageCmd()
	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)

	err := cmd.RunE(cmd, []string{"daily"})
	if err == nil {
		t.Fatal("RunE returned nil error, want *usage.NotFoundError")
	}
	var nf *usage.NotFoundError
	if !errors.As(err, &nf) {
		t.Fatalf("RunE error = %v (%T), want *usage.NotFoundError", err, err)
	}
	var ee interface{ ExitCode() int }
	if !errors.As(err, &ee) {
		t.Fatalf("error %v (%T) does not implement ExitCode()", err, err)
	}
	if ee.ExitCode() != 1 {
		t.Errorf("ExitCode() = %d, want 1", ee.ExitCode())
	}
	// RunE printed the hint itself, exactly once; SilenceErrors stops
	// cobra from printing anything more, so stderr holds nothing else.
	if got := errBuf.String(); got != usageHint+"\n" {
		t.Errorf("stderr = %q, want exactly %q", got, usageHint+"\n")
	}
	if got := outBuf.String(); got != "" {
		t.Errorf("stdout = %q, want empty", got)
	}
}

func TestUsageCmd_ScopesEnvToCWDProfile(t *testing.T) {
	_, home := setupUsageFixture(t)
	_, capture := fakeCcusage(t)
	// An outer session's managed var must be scrubbed, never leaked.
	t.Setenv("CLAUDE_CONFIG_DIR", "/leak")

	// Execute through the registered root: with DisableFlagParsing the
	// ccusage flags reach RunE untouched (an enabled parser would
	// reject --json as an unknown flag).
	root := buildTestRoot()
	root.SetArgs([]string{"usage", "daily", "--json"})
	var outBuf, errBuf bytes.Buffer
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v (stderr=%q)", err, errBuf.String())
	}

	data, err := os.ReadFile(capture)
	if err != nil {
		t.Fatalf("reading capture: %v", err)
	}
	got := captureEnv(t, string(data))
	if got["argv"] != "daily --json" {
		t.Errorf("argv = %q, want %q", got["argv"], "daily --json")
	}
	if want := filepath.Join(home, ".claude-forge"); got["CLAUDE_CONFIG_DIR"] != want {
		t.Errorf("CLAUDE_CONFIG_DIR = %q, want %q", got["CLAUDE_CONFIG_DIR"], want)
	}
	if want := filepath.Join(home, ".pi-forge", "sessions"); got["PI_AGENT_DIR"] != want {
		t.Errorf("PI_AGENT_DIR = %q, want %q", got["PI_AGENT_DIR"], want)
	}
	if strings.Contains(string(data), "/leak") {
		t.Errorf("capture contains leaked outer env: %q", string(data))
	}
}

// TestUsageCmd_ConfigErrorPrintsMessage pins the contract that RunE
// owns ALL error output: SilenceErrors on the leaf makes main.go
// print nothing for this command, so a config-load failure must be
// reported by RunE itself, prefixed "aide usage: ".
func TestUsageCmd_ConfigErrorPrintsMessage(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	xdgHome := filepath.Join(dir, "xdg")
	aideDir := filepath.Join(xdgHome, "aide")
	if err := os.MkdirAll(aideDir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", xdgHome)
	t.Setenv("HOME", t.TempDir())
	// Invalid YAML: config.Load returns a parse error (config.go),
	// not an empty-config fallback.
	if err := os.WriteFile(filepath.Join(aideDir, "config.yaml"),
		[]byte("default_profile: [unclosed\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cmd := usageCmd()
	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)
	err := cmd.RunE(cmd, []string{"daily"})
	if err == nil {
		t.Fatal("RunE returned nil error, want a config-load failure")
	}
	if got := errBuf.String(); !strings.Contains(got, "aide usage: ") {
		t.Errorf("stderr = %q, want it to contain %q followed by error text",
			got, "aide usage: ")
	}
	if got := outBuf.String(); got != "" {
		t.Errorf("stdout = %q, want empty", got)
	}

	// Through main.go's real exit path the run still exits non-zero:
	// a config error carries no ExitCode, so the silenced-leaf branch
	// falls back to 1 — and nothing prints the error a second time.
	root := buildTestRoot()
	root.SilenceUsage = true // mirror main.go's production root
	root.SetArgs([]string{"usage", "daily"})
	var outBuf2, errBuf2 bytes.Buffer
	root.SetOut(&outBuf2)
	root.SetErr(&errBuf2)
	if code := runMain(root); code == 0 {
		t.Error("runMain = 0, want non-zero for a config-load failure")
	}
	if got := errBuf2.String(); strings.Count(got, "aide usage: ") != 1 {
		t.Errorf("stderr = %q, want exactly one %q line", got, "aide usage: ")
	}
	if got := outBuf2.String(); got != "" {
		t.Errorf("stdout = %q, want empty", got)
	}
}

func TestUsageCmd_ExitCodePassthrough(t *testing.T) {
	setupUsageFixture(t)
	fakeCcusage(t)
	t.Setenv("FAKE_EXIT", "2")

	cmd := usageCmd()
	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)

	err := cmd.RunE(cmd, []string{"daily"})
	if err == nil {
		t.Fatal("RunE returned nil error, want *usage.ExitError")
	}
	var exitErr *usage.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("RunE error = %v (%T), want *usage.ExitError", err, err)
	}
	var ee interface{ ExitCode() int }
	if !errors.As(err, &ee) {
		t.Fatalf("error %v (%T) does not implement ExitCode()", err, err)
	}
	if ee.ExitCode() != 2 {
		t.Errorf("ExitCode() = %d, want 2", ee.ExitCode())
	}
	// The ExitError path prints nothing extra: stderr holds exactly
	// the child process's own output, which already reached the user.
	if got := errBuf.String(); got != "child-err\n" {
		t.Errorf("stderr = %q, want exactly the child's output %q", got, "child-err\n")
	}
	if got := outBuf.String(); got != "" {
		t.Errorf("stdout = %q, want empty", got)
	}
}
