// cmd/aide/usage_test.go
package main

import (
	"bytes"
	"encoding/json"
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
if [ -n "$FAKE_STDOUT" ]; then printf '%s\n' "$FAKE_STDOUT"; fi
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

func TestUsageCmd_ScopeFlags(t *testing.T) {
	_, home := setupUsageFixture(t)
	_, capture := fakeCcusage(t)
	t.Setenv("FAKE_STDOUT", `{"report":"ok"}`)
	for _, dir := range []string{".claude", ".claude-forge", ".codex", ".codex-forge", ".copilot", ".copilot-forge"} {
		if err := os.MkdirAll(filepath.Join(home, dir), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("CLAUDE_CONFIG_DIR", "/leak")
	cases := []struct {
		name, arg, claude, codex string
		args                     []string
		warning                  bool
	}{
		{name: "explicit profile overrides cwd", args: []string{"--profile", "default", "blocks", "--json"}, arg: "blocks --json", claude: filepath.Join(home, ".claude")},
		{name: "equals profile overrides cwd", args: []string{"--profile=default"}, arg: "daily", claude: filepath.Join(home, ".claude")},
		{name: "all before report", args: []string{"--all", "daily", "--json"}, arg: "daily --json", claude: filepath.Join(home, ".claude") + "," + filepath.Join(home, ".claude-forge"), codex: filepath.Join(home, ".codex") + "," + filepath.Join(home, ".codex-forge"), warning: true},
		{name: "all after separator belongs to ccusage", args: []string{"--", "--all"}, arg: "--all", claude: filepath.Join(home, ".claude-forge")},
		{name: "separator after scope stripped once", args: []string{"--profile=default", "--", "--", "daily"}, arg: "-- daily", claude: filepath.Join(home, ".claude")},
		{name: "unknown flag stops scope parsing", args: []string{"--json", "--profile", "default"}, arg: "--json --profile default", claude: filepath.Join(home, ".claude-forge")},
		{name: "report stops scope parsing", args: []string{"daily", "--all"}, arg: "daily --all", claude: filepath.Join(home, ".claude-forge")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := buildTestRoot()
			root.SetArgs(append([]string{"usage"}, tc.args...))
			var out, stderr bytes.Buffer
			root.SetOut(&out)
			root.SetErr(&stderr)
			if err := root.Execute(); err != nil {
				t.Fatalf("Execute: %v (stderr=%q)", err, stderr.String())
			}
			data, err := os.ReadFile(capture)
			if err != nil {
				t.Fatal(err)
			}
			got := captureEnv(t, string(data))
			if got["argv"] != tc.arg || got["CLAUDE_CONFIG_DIR"] != tc.claude {
				t.Errorf("argv=%q CLAUDE_CONFIG_DIR=%q, want %q %q", got["argv"], got["CLAUDE_CONFIG_DIR"], tc.arg, tc.claude)
			}
			if tc.codex != "" && got["CODEX_HOME"] != tc.codex {
				t.Errorf("CODEX_HOME = %q, want %q", got["CODEX_HOME"], tc.codex)
			}
			if tc.warning {
				if _, ok := got["PI_AGENT_DIR"]; ok {
					t.Error("--all must exclude PI_AGENT_DIR")
				}
				if got["COPILOT_HOME"] != filepath.Join(home, ".copilot") {
					t.Errorf("COPILOT_HOME = %q, want first profile's directory", got["COPILOT_HOME"])
				}
			}
			if strings.Contains(string(data), "/leak") {
				t.Errorf("outer env leaked: %s", data)
			}
			if has := strings.Contains(stderr.String(), "warning: COPILOT_HOME"); has != tc.warning {
				t.Errorf("stderr = %q, want warning=%t", stderr.String(), tc.warning)
			}
			if got := out.String(); got != "{\"report\":\"ok\"}\n" {
				t.Errorf("stdout = %q, want only child JSON", got)
			}
		})
	}
}

func TestUsageCmd_InvalidScopeFlags(t *testing.T) {
	setupUsageFixture(t)
	_, capture := fakeCcusage(t)
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"unknown profile", []string{"--profile", "missing"}, "unknown profile"},
		{"empty profile", []string{"--profile="}, "profile"},
		{"missing profile", []string{"--profile"}, "profile"},
		{"missing profile before separator", []string{"--profile", "--"}, "profile"},
		{"mutually exclusive all then profile", []string{"--all", "--profile=default"}, "--all"},
		{"mutually exclusive profile then all", []string{"--profile=default", "--all"}, "--all"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_ = os.Remove(capture)
			root := buildTestRoot()
			root.SilenceUsage = true // production's root setting
			root.SetArgs(append([]string{"usage"}, tc.args...))
			var out, stderr bytes.Buffer
			root.SetOut(&out)
			root.SetErr(&stderr)
			if err := root.Execute(); err == nil {
				t.Fatal("Execute succeeded, want scope error")
			}
			if !strings.Contains(stderr.String(), "aide usage: ") || !strings.Contains(stderr.String(), tc.want) {
				t.Errorf("stderr = %q, want scope error containing %q", stderr.String(), tc.want)
			}
			if out.Len() != 0 {
				t.Errorf("stdout = %q, want empty", out.String())
			}
			if _, err := os.Stat(capture); !os.IsNotExist(err) {
				t.Errorf("ccusage ran on invalid scope: stat error %v", err)
			}
		})
	}
}

func TestUsageCmd_AllPreservesChildExitCode(t *testing.T) {
	setupUsageFixture(t)
	fakeCcusage(t)
	t.Setenv("FAKE_EXIT", "7")
	cmd := usageCmd()
	var out, stderr bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&stderr)
	err := cmd.RunE(cmd, []string{"--all", "blocks", "--json"})
	var exitErr *usage.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 7 {
		t.Errorf("RunE error = %v, want child exit code 7", err)
	}
	if got := stderr.String(); got != "child-err\n" {
		t.Errorf("stderr = %q, want only child stderr", got)
	}
}

func TestUsageCmd_SetupWithoutCcusage(t *testing.T) {
	_, home := setupUsageFixture(t)
	t.Setenv("PATH", t.TempDir())
	root := buildTestRoot()
	root.SetArgs([]string{"usage", "--setup", "--force"})
	var out, stderr bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&stderr)
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v (stderr=%q)", err, stderr.String())
	}
	data, err := os.ReadFile(filepath.Join(home, ".claude", "ccusage.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"name": "pi-forge"`) || !strings.Contains(string(data), filepath.Join(home, ".pi-forge", "sessions")) {
		t.Errorf("unexpected stores: %s", data)
	}
}

func TestUsageCmd_ProfileConfigDoesNotLoadNamedPiStores(t *testing.T) {
	_, home := setupUsageFixture(t)
	_, capture := fakeCcusage(t)
	path := filepath.Join(home, ".claude", "ccusage.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	original := []byte(`{"claude":{"timezone":"UTC"},"pi":{"custom":true,"stores":[{"name":"pi-forge","path":"/other/sessions"}]},"future":[3]}`)
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	// The fake reads ccusage's effective config while the child runs.
	bin := strings.Split(os.Getenv("PATH"), string(os.PathListSeparator))[0]
	script := `#!/bin/sh
printf 'argv=%s\n' "$*" > "$CAPTURE"
while [ "$#" -gt 0 ]; do
  case "$1" in
    --config) shift; cat "$1" > "$CAPTURE.config"; printf '%s' "$1" > "$CAPTURE.path"; break ;;
    --config=*) cat "${1#--config=}" > "$CAPTURE.config"; printf '%s' "${1#--config=}" > "$CAPTURE.path"; break ;;
  esac
  shift
done
`
	if err := os.WriteFile(filepath.Join(bin, "ccusage"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		args   []string
		source string
	}{
		{"default", []string{"--profile", "default", "daily"}, path},
		{"cwd forge", []string{"daily"}, filepath.Join(home, ".claude-forge", "ccusage.json")},
		{"explicit config", []string{"--profile", "default", "daily", "--config", path}, path},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_ = os.Remove(capture + ".config")
			if tc.name == "cwd forge" {
				if err := os.MkdirAll(filepath.Dir(tc.source), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(tc.source, original, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			cmd := usageCmd()
			if err := cmd.RunE(cmd, tc.args); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(capture + ".config")
			if err != nil {
				t.Fatalf("child did not receive config: %v", err)
			}
			var root map[string]json.RawMessage
			if err := json.Unmarshal(data, &root); err != nil {
				t.Fatal(err)
			}
			var pi map[string]json.RawMessage
			if err := json.Unmarshal(root["pi"], &pi); err != nil {
				t.Fatal(err)
			}
			if _, ok := pi["stores"]; ok {
				t.Errorf("named stores leaked: %s", data)
			}
			if string(root["claude"]) != `{"timezone":"UTC"}` || string(pi["custom"]) != "true" || string(root["future"]) != "[3]" {
				t.Errorf("unrelated settings changed: %s", data)
			}
			originalAfter, err := os.ReadFile(tc.source)
			if err != nil || !bytes.Equal(originalAfter, original) {
				t.Errorf("source config changed: %s (%v)", originalAfter, err)
			}
			tempPath, err := os.ReadFile(capture + ".path")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(string(tempPath)); !os.IsNotExist(err) {
				t.Errorf("temporary config not removed: %v", err)
			}
		})
	}
	// Merged scope must still load the generated stores.
	_ = os.Remove(capture + ".config")
	cmd := usageCmd()
	if err := cmd.RunE(cmd, []string{"--all", "daily"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(capture + ".config"); !os.IsNotExist(err) {
		t.Errorf("--all unexpectedly received a temporary config: %v", err)
	}
}

func TestUsageCmd_SetupRejectsInvalidArguments(t *testing.T) {
	_, home := setupUsageFixture(t)
	t.Setenv("PATH", t.TempDir())
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"--force"}, "--force"},
		{[]string{"--setup", "daily"}, "--setup"},
		{[]string{"--setup", "--", "daily"}, "--setup"},
		{[]string{"--setup", "--all"}, "--setup"},
		{[]string{"--setup", "--profile=forge"}, "--setup"},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			root := buildTestRoot()
			root.SilenceUsage = true
			root.SetArgs(append([]string{"usage"}, tc.args...))
			var out, stderr bytes.Buffer
			root.SetOut(&out)
			root.SetErr(&stderr)
			if err := root.Execute(); err == nil || !strings.Contains(stderr.String(), tc.want) {
				t.Errorf("Execute error = %v, stderr=%q; want %q", err, stderr.String(), tc.want)
			}
			if _, err := os.Stat(filepath.Join(home, ".claude", "ccusage.json")); !os.IsNotExist(err) {
				t.Errorf("invalid setup wrote config: %v", err)
			}
		})
	}
}

func TestUsageCmd_SetupConfirmation(t *testing.T) {
	_, home := setupUsageFixture(t)
	t.Setenv("PATH", t.TempDir())
	oldTTY := isStdinTTY
	t.Cleanup(func() { isStdinTTY = oldTTY })
	path := filepath.Join(home, ".claude", "ccusage.json")
	for _, tc := range []struct {
		name, input          string
		interactive, written bool
	}{
		{"nonTTY refuses", "y\n", false, false},
		{"decline leaves file alone", "n\n", true, false},
		{"accept writes", "y\n", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isStdinTTY = func() bool { return tc.interactive }
			root := buildTestRoot()
			root.SilenceUsage = true
			root.SetArgs([]string{"usage", "--setup"})
			root.SetIn(strings.NewReader(tc.input))
			var out, stderr bytes.Buffer
			root.SetOut(&out)
			root.SetErr(&stderr)
			err := root.Execute()
			if tc.interactive && err != nil || !tc.interactive && (err == nil || !strings.Contains(stderr.String(), "--force")) {
				t.Errorf("Execute = %v (stderr=%q)", err, stderr.String())
			}
			_, statErr := os.Stat(path)
			if tc.written && statErr != nil || !tc.written && !os.IsNotExist(statErr) {
				t.Errorf("config stat = %v, want written=%t", statErr, tc.written)
			}
		})
	}
}

func TestUsageCmd_HelpAndPassthrough(t *testing.T) {
	setupUsageFixture(t)
	t.Setenv("PATH", t.TempDir())
	root := buildTestRoot()
	root.SetArgs([]string{"usage", "--help"})
	var out, stderr bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&stderr)
	if err := root.Execute(); err != nil {
		t.Fatalf("help: %v (stderr=%q)", err, stderr.String())
	}
	for _, fragment := range []string{"--all", "--profile", "--setup", "--force", "--", "ccusage"} {
		if !strings.Contains(out.String(), fragment) {
			t.Errorf("help missing %q: %s", fragment, out.String())
		}
	}
	_, capture := fakeCcusage(t)
	root = buildTestRoot()
	root.SetArgs([]string{"usage", "--", "--help"})
	root.SetOut(&out)
	root.SetErr(&stderr)
	if err := root.Execute(); err != nil {
		t.Fatalf("ccusage help: %v (stderr=%q)", err, stderr.String())
	}
	data, err := os.ReadFile(capture)
	if err != nil || !strings.Contains(string(data), "argv=--help\n") {
		t.Errorf("ccusage argv: %q, error %v", data, err)
	}
}

func TestUsageCmd_ProfileCompletion(t *testing.T) {
	setupUsageFixture(t)
	t.Setenv("PATH", t.TempDir())
	for _, args := range [][]string{{"__complete", "usage", "--profile", "f"}, {"__complete", "usage", "--profile=f"}} {
		root := buildTestRoot()
		root.SetArgs(args)
		var out, stderr bytes.Buffer
		root.SetOut(&out)
		root.SetErr(&stderr)
		if err := root.Execute(); err != nil {
			t.Fatalf("completion %v: %v (stderr=%q)", args, err, stderr.String())
		}
		if !strings.Contains(out.String(), "forge\n") {
			t.Errorf("completion %v = %q; want forge", args, out.String())
		}
	}
	for _, args := range [][]string{{"__complete", "usage", "--", "--profile", "f"}, {"__complete", "usage", "daily", "--profile", "f"}} {
		root := buildTestRoot()
		root.SetArgs(args)
		var out bytes.Buffer
		root.SetOut(&out)
		if err := root.Execute(); err != nil {
			t.Fatalf("completion %v: %v", args, err)
		}
		if strings.Contains(out.String(), "forge") {
			t.Errorf("completion after report/separator %v unexpectedly suggested profile: %q", args, out.String())
		}
	}
}
