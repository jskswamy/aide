// Package usage_test exercises the usage backend seam: the ccusage
// env-var mapping derived from harness and config data, env
// scrubbing that stops an outer session leaking across profiles,
// CWD profile resolution, and the backend exec path.
package usage_test

import (
	"bytes"
	"errors"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/config"
	"github.com/jskswamy/aide/internal/usage"
)

// assignments splits "VAR=value" strings on the first "=" into a map
// for order-independent comparison.
func assignments(t *testing.T, env []string) map[string]string {
	t.Helper()
	out := make(map[string]string, len(env))
	for _, e := range env {
		k, v, ok := strings.Cut(e, "=")
		if !ok {
			t.Fatalf("assignment %q has no '='", e)
		}
		out[k] = v
	}
	return out
}

func TestProfileEnv_IsolatedProfile(t *testing.T) {
	ctx := config.Context{Profile: "forge"}
	got := usage.ProfileEnv(usage.DefaultBackend(), ctx, "/h")
	// Every value is ctx.NativeConfigDir(h, "/h") plus the mapping's
	// suffix, so the profile's paths agree with aide launches:
	// pi sessions live under <PI_CODING_AGENT_DIR>/sessions, and the
	// isolated opencode DB is <OPENCODE_CONFIG_DIR>/opencode.db.
	want := map[string]string{
		"CLAUDE_CONFIG_DIR": "/h/.claude-forge",
		"CODEX_HOME":        "/h/.codex-forge",
		"COPILOT_HOME":      "/h/.copilot-forge",
		"PI_AGENT_DIR":      "/h/.pi-forge/sessions",
		"OPENCODE_DATA_DIR": "/h/.opencode-forge",
		"GEMINI_DATA_DIR":   "/h/.gemini-forge/tmp",
	}
	if m := assignments(t, got); !maps.Equal(m, want) {
		t.Errorf("ProfileEnv = %v, want %v", m, want)
	}
	// Output is deterministic: assignments sorted by var name.
	if !slices.IsSorted(got) {
		t.Errorf("ProfileEnv output not sorted by var name: %v", got)
	}
}

func TestProfileEnv_DefaultProfileUsesHarnessDefaults(t *testing.T) {
	ctx := config.Context{} // no config dir selected
	m := assignments(t, usage.ProfileEnv(usage.DefaultBackend(), ctx, "/h"))
	want := map[string]string{
		"CLAUDE_CONFIG_DIR": "/h/.claude",
		"PI_AGENT_DIR":      "/h/.pi/agent/sessions",
		"OPENCODE_DATA_DIR": "/h/.local/share/opencode", // NOT .config/opencode
		"COPILOT_HOME":      "/h/.copilot",
	}
	for k, v := range want {
		if m[k] != v {
			t.Errorf("%s = %q, want %q", k, m[k], v)
		}
	}
}

func TestProfileEnv_SetsMissingPaths(t *testing.T) {
	home := t.TempDir() // nothing inside
	ctx := config.Context{Profile: "forge"}
	m := assignments(t, usage.ProfileEnv(usage.DefaultBackend(), ctx, home))
	// No existence check filters the dir out: setting a missing path is
	// the no-wrong-data guarantee.
	if want := filepath.Join(home, ".claude-forge"); m["CLAUDE_CONFIG_DIR"] != want {
		t.Errorf("CLAUDE_CONFIG_DIR = %q, want %q", m["CLAUDE_CONFIG_DIR"], want)
	}
}

func TestProfileEnv_SkipsHarnessesWithoutProfileSupport(t *testing.T) {
	ctx := config.Context{Profile: "forge"}
	got := usage.ProfileEnv(usage.DefaultBackend(), ctx, "/h")
	// Harnesses where Descriptor.ProfileEnvKey == "" (aider, amp, ...)
	// must contribute nothing — no assignment whose Var is "" — and
	// must not panic.
	if len(got) != len(usage.DefaultBackend().HarnessEnv) {
		t.Errorf("ProfileEnv emitted %d assignments, want one per mapped harness (%d)",
			len(got), len(usage.DefaultBackend().HarnessEnv))
	}
	for _, e := range got {
		k, v, _ := strings.Cut(e, "=")
		if k == "" || v == "" {
			t.Errorf("assignment %q has an empty var or value", e)
		}
	}
}

func TestScrubEnv(t *testing.T) {
	in := []string{"PATH=/p", "CLAUDE_CONFIG_DIR=/leak", "PI_CODING_AGENT_DIR=/leak",
		"OPENCODE_CONFIG_DIR=/leak", "GEMINI_HOME=/leak", "XDG_DATA_HOME=/leak",
		"CODEX_HOME=/leak", "PI_AGENT_DIR=/leak", "HOME=/h"}
	got := usage.ScrubEnv(in, usage.DefaultBackend())
	want := []string{"PATH=/p", "HOME=/h"} // order preserved
	if !slices.Equal(got, want) {
		t.Errorf("ScrubEnv = %v, want %v", got, want)
	}
}

func TestResolveCWDProfile_MatchesPathRule(t *testing.T) {
	dir := t.TempDir()
	repo := filepath.Join(dir, "repo")
	sub := filepath.Join(repo, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	elsewhere := filepath.Join(dir, "elsewhere")
	if err := os.MkdirAll(elsewhere, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)

	// config.Dir() reads from $XDG_CONFIG_HOME/aide/, so point XDG
	// at a temp dir and write the global config there; HOME is
	// temped too so load-time profile-dir checks stay hermetic.
	xdgHome := filepath.Join(dir, "xdg")
	aideDir := filepath.Join(xdgHome, "aide")
	if err := os.MkdirAll(filepath.Join(aideDir, "profiles"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", xdgHome)
	t.Setenv("HOME", filepath.Join(dir, "home"))

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

	// A cwd under the repo matches forge's path rule, so the usage
	// run targets the same profile a launch there would.
	cfg, err := config.Load(config.Dir(), sub)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	name, ctx, err := usage.ResolveCWDProfile(cfg, sub)
	if err != nil {
		t.Fatalf("ResolveCWDProfile: %v", err)
	}
	if name != "forge" {
		t.Errorf("name = %q, want %q", name, "forge")
	}
	if ctx.Profile != "forge" {
		t.Errorf("ctx.Profile = %q, want %q", ctx.Profile, "forge")
	}

	// A cwd no rule matches falls back to the default profile.
	cfg, err = config.Load(config.Dir(), elsewhere)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	name, _, err = usage.ResolveCWDProfile(cfg, elsewhere)
	if err != nil {
		t.Fatalf("ResolveCWDProfile: %v", err)
	}
	if name != "default" {
		t.Errorf("name = %q, want %q", name, "default")
	}
}

func TestRun_NotFound(t *testing.T) {
	b := usage.DefaultBackend()
	b.Binary = "ccusage-no-such-binary-xyz"
	err := usage.Run(b, nil, nil, nil, io.Discard, io.Discard)
	var nf *usage.NotFoundError
	if !errors.As(err, &nf) {
		t.Fatalf("Run error = %v (%T), want *usage.NotFoundError", err, err)
	}
	want := "ccusage not found on PATH — install it: brew install ccusage · nixpkgs: pkgs.ccusage"
	if err.Error() != want {
		t.Errorf("err.Error() = %q, want %q", err.Error(), want)
	}
	if nf.ExitCode() != 1 {
		t.Errorf("NotFoundError.ExitCode() = %d, want 1", nf.ExitCode())
	}
}

func TestRun_PassthroughArgsEnvAndExitCode(t *testing.T) {
	dir := t.TempDir()
	capture := filepath.Join(dir, "capture")
	fake := filepath.Join(dir, "ccusage-fake")
	// The fake backend records what it received: its argv, the
	// profile's env var, and proves stdio is wired through.
	script := `#!/bin/sh
{
  printf 'argv=%s\n' "$*"
  printf 'CLAUDE_CONFIG_DIR=%s\n' "$CLAUDE_CONFIG_DIR"
} > "$CAPTURE"
printf 'out\n'
printf 'err\n' >&2
exit 3
`
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	b := usage.DefaultBackend()
	b.Binary = "ccusage-fake"
	// Run treats env as the child's whole environment, so CAPTURE
	// rides along with the profile assignment.
	env := []string{
		"CLAUDE_CONFIG_DIR=/h/.claude-forge",
		"CAPTURE=" + capture,
	}
	var outBuf, errBuf bytes.Buffer
	err := usage.Run(b, []string{"daily", "--json"}, env,
		strings.NewReader(""), &outBuf, &errBuf)
	var ee *usage.ExitError
	if !errors.As(err, &ee) {
		t.Fatalf("Run error = %v (%T), want *usage.ExitError", err, err)
	}
	if ee.ExitCode() != 3 {
		t.Errorf("ExitError.ExitCode() = %d, want 3", ee.ExitCode())
	}

	data, readErr := os.ReadFile(capture)
	if readErr != nil {
		t.Fatalf("reading capture: %v", readErr)
	}
	got := assignments(t, strings.Split(strings.TrimRight(string(data), "\n"), "\n"))
	if got["argv"] != "daily --json" {
		t.Errorf("argv = %q, want %q", got["argv"], "daily --json")
	}
	if got["CLAUDE_CONFIG_DIR"] != "/h/.claude-forge" {
		t.Errorf("CLAUDE_CONFIG_DIR = %q, want %q",
			got["CLAUDE_CONFIG_DIR"], "/h/.claude-forge")
	}

	if !strings.Contains(outBuf.String(), "out") {
		t.Errorf("stdout = %q, want the fake's print", outBuf.String())
	}
	if !strings.Contains(errBuf.String(), "err") {
		t.Errorf("stderr = %q, want the fake's print", errBuf.String())
	}
}

func TestRun_SignaledChildMapsTo128PlusN(t *testing.T) {
	dir := t.TempDir()
	fake := filepath.Join(dir, "ccusage-fake")
	// SIGPIPE is the signal from the actual finding (aide usage
	// --json | head); on this machine (Go 1.26/darwin) it reliably
	// kills /bin/sh — probed: ExitCode() == -1, WaitStatus signal 13.
	// SIGKILL probed equally reliable (137).
	script := "#!/bin/sh\nkill -PIPE $$\n"
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	b := usage.DefaultBackend()
	b.Binary = "ccusage-fake"
	err := usage.Run(b, nil, nil, nil, io.Discard, io.Discard)
	var ee *usage.ExitError
	if !errors.As(err, &ee) {
		t.Fatalf("Run error = %v (%T), want *usage.ExitError", err, err)
	}
	if ee.ExitCode() != 128+13 {
		t.Errorf("ExitError.ExitCode() = %d, want %d (128 + SIGPIPE)",
			ee.ExitCode(), 128+13)
	}
}

func TestRun_LookPathPermissionIsNotNotFound(t *testing.T) {
	dir := t.TempDir()
	fake := filepath.Join(dir, "ccusage")
	// ccusage present but without the executable bit. Observed
	// behavior (Go 1.26/darwin, probed): for a BARE name resolved by
	// PATH search, exec.LookPath masks this case as exec.ErrNotFound;
	// the os.ErrPermission refusal surfaces only when the name
	// contains a path separator, so Binary carries the full path.
	if err := os.WriteFile(fake, []byte("#!/bin/sh\nexit 0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	b := usage.DefaultBackend()
	b.Binary = fake
	err := usage.Run(b, nil, nil, nil, io.Discard, io.Discard)
	if err == nil {
		t.Fatal("Run succeeded for a non-executable backend")
	}
	var nf *usage.NotFoundError
	if errors.As(err, &nf) {
		t.Fatalf("Run error = %v (*usage.NotFoundError); a permission refusal must not carry the install hint", err)
	}
	if strings.Contains(err.Error(), b.InstallHint) {
		t.Errorf("error %q contains the install hint", err)
	}
}

func TestRun_LookPathDotInPathIsNotNotFound(t *testing.T) {
	dir := t.TempDir()
	// An executable ccusage reachable only through "." in PATH:
	// exec.LookPath refuses with exec.ErrDot (probed on Go
	// 1.26/darwin), which must not become a NotFoundError either.
	if err := os.WriteFile(filepath.Join(dir, "ccusage"),
		[]byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	t.Setenv("PATH", ".")

	b := usage.DefaultBackend()
	err := usage.Run(b, nil, nil, nil, io.Discard, io.Discard)
	if err == nil {
		t.Fatal("Run succeeded for a backend resolved through . in PATH")
	}
	var nf *usage.NotFoundError
	if errors.As(err, &nf) {
		t.Fatalf("Run error = %v (*usage.NotFoundError); an ErrDot refusal must not carry the install hint", err)
	}
	if strings.Contains(err.Error(), b.InstallHint) {
		t.Errorf("error %q contains the install hint", err)
	}
}
