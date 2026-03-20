package launcher

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// mockLookPath creates a LookPathFunc that finds only the given binaries.
func mockLookPath(available map[string]string) LookPathFunc {
	return func(file string) (string, error) {
		if path, ok := available[file]; ok {
			return path, nil
		}
		return "", fmt.Errorf("%s: not found", file)
	}
}

func TestPassthrough_SingleAgent(t *testing.T) {
	mock := &mockExecer{}
	l := &Launcher{
		Execer:   mock,
		LookPath: mockLookPath(map[string]string{"claude": "/usr/local/bin/claude"}),
	}

	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	err := l.Passthrough(t.TempDir(), "", nil)
	if err != nil {
		t.Fatalf("Passthrough failed: %v", err)
	}

	innerBinary, _ := unwrapSandbox(t, mock.binary, mock.args)
	if innerBinary != "/usr/local/bin/claude" {
		t.Errorf("expected inner binary /usr/local/bin/claude, got %s", innerBinary)
	}
}

func TestPassthrough_SingleAgentWithArgs(t *testing.T) {
	mock := &mockExecer{}
	l := &Launcher{
		Execer:   mock,
		LookPath: mockLookPath(map[string]string{"codex": "/usr/bin/codex"}),
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	extraArgs := []string{"--model", "opus", "help me"}
	err := l.Passthrough(t.TempDir(), "", extraArgs)
	if err != nil {
		t.Fatalf("Passthrough failed: %v", err)
	}

	_, innerArgs := unwrapSandbox(t, mock.binary, mock.args)
	expected := []string{"/usr/bin/codex", "--model", "opus", "help me"}
	if len(innerArgs) != len(expected) {
		t.Fatalf("expected %d inner args, got %d: %v", len(expected), len(innerArgs), innerArgs)
	}
	for i, want := range expected {
		if innerArgs[i] != want {
			t.Errorf("innerArgs[%d] = %q, want %q", i, innerArgs[i], want)
		}
	}
}

func TestPassthrough_NoAgents(t *testing.T) {
	mock := &mockExecer{}
	l := &Launcher{
		Execer:   mock,
		LookPath: mockLookPath(map[string]string{}),
	}

	err := l.Passthrough(t.TempDir(), "", nil)
	if err == nil {
		t.Fatal("expected error when no agents found, got nil")
	}
	if !strings.Contains(err.Error(), "no config found") {
		t.Errorf("expected 'no config found' error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "aide init") {
		t.Errorf("expected install guidance with 'aide init', got: %v", err)
	}
}

func TestPassthrough_MultipleAgents(t *testing.T) {
	mock := &mockExecer{}
	l := &Launcher{
		Execer: mock,
		LookPath: mockLookPath(map[string]string{
			"claude": "/usr/local/bin/claude",
			"codex":  "/usr/local/bin/codex",
		}),
	}

	err := l.Passthrough(t.TempDir(), "", nil)
	if err == nil {
		t.Fatal("expected error when multiple agents found, got nil")
	}
	if !strings.Contains(err.Error(), "multiple agents") {
		t.Errorf("expected 'multiple agents' error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "--agent") {
		t.Errorf("expected --agent hint, got: %v", err)
	}
}

func TestPassthrough_AgentOverride(t *testing.T) {
	mock := &mockExecer{}
	l := &Launcher{
		Execer: mock,
		LookPath: mockLookPath(map[string]string{
			"claude": "/usr/local/bin/claude",
			"codex":  "/usr/local/bin/codex",
		}),
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	// With --agent codex, should launch codex directly even though multiple found.
	err := l.Passthrough(t.TempDir(), "codex", []string{"--help"})
	if err != nil {
		t.Fatalf("Passthrough with --agent failed: %v", err)
	}

	innerBinary, innerArgs := unwrapSandbox(t, mock.binary, mock.args)
	if innerBinary != "/usr/local/bin/codex" {
		t.Errorf("expected inner binary /usr/local/bin/codex, got %s", innerBinary)
	}
	expected := []string{"/usr/local/bin/codex", "--help"}
	if len(innerArgs) != len(expected) {
		t.Fatalf("expected %d inner args, got %d: %v", len(expected), len(innerArgs), innerArgs)
	}
}

func TestPassthrough_AgentOverrideNotOnPath(t *testing.T) {
	mock := &mockExecer{}
	l := &Launcher{
		Execer:   mock,
		LookPath: mockLookPath(map[string]string{}), // nothing on PATH
	}

	err := l.Passthrough(t.TempDir(), "nonexistent", nil)
	if err == nil {
		t.Fatal("expected error for agent not on PATH, got nil")
	}
	if !strings.Contains(err.Error(), "not found on PATH") {
		t.Errorf("expected 'not found on PATH' error, got: %v", err)
	}
	if mock.binary != "" {
		t.Error("expected exec not to be called for missing agent")
	}
}

func TestPassthrough_FirstRunSentinel(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	if !IsFirstRun() {
		t.Error("expected IsFirstRun=true before passthrough")
	}

	mock := &mockExecer{}
	l := &Launcher{
		Execer:   mock,
		LookPath: mockLookPath(map[string]string{"claude": "/usr/local/bin/claude"}),
	}

	err := l.Passthrough(t.TempDir(), "", nil)
	if err != nil {
		t.Fatalf("Passthrough failed: %v", err)
	}

	sentinel := filepath.Join(configHome, "aide", ".first-run-done")
	data, err := os.ReadFile(sentinel)
	if err != nil {
		t.Fatalf("expected sentinel file at %s: %v", sentinel, err)
	}
	if !strings.Contains(string(data), "claude") {
		t.Errorf("expected sentinel to contain agent name, got %q", string(data))
	}

	if IsFirstRun() {
		t.Error("expected IsFirstRun=false after passthrough wrote sentinel")
	}
}

func TestScanAgents(t *testing.T) {
	available := map[string]string{
		"claude": "/usr/local/bin/claude",
		"aider":  "/usr/local/bin/aider",
	}

	result := ScanAgents(mockLookPath(available))

	if len(result.Found) != 2 {
		t.Fatalf("expected 2 agents found, got %d", len(result.Found))
	}
	if result.Found["claude"] != "/usr/local/bin/claude" {
		t.Errorf("expected claude path, got %q", result.Found["claude"])
	}
	if result.Found["aider"] != "/usr/local/bin/aider" {
		t.Errorf("expected aider path, got %q", result.Found["aider"])
	}
}

func TestPassthrough_YoloInjectsFlag(t *testing.T) {
	mock := &mockExecer{}
	l := &Launcher{
		Execer:   mock,
		LookPath: mockLookPath(map[string]string{"claude": "/usr/local/bin/claude"}),
		Yolo:     true,
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	err := l.Passthrough(t.TempDir(), "", []string{"--model", "opus"})
	if err != nil {
		t.Fatalf("Passthrough failed: %v", err)
	}

	expected := []string{"/usr/local/bin/claude", "--dangerously-skip-permissions", "--model", "opus"}
	_, innerArgs := unwrapSandbox(t, mock.binary, mock.args)
	if len(innerArgs) != len(expected) {
		t.Fatalf("expected %d inner args, got %d: %v", len(expected), len(innerArgs), innerArgs)
	}
	for i, want := range expected {
		if innerArgs[i] != want {
			t.Errorf("innerArgs[%d] = %q, want %q", i, innerArgs[i], want)
		}
	}
}

func TestPassthrough_YoloUnsupportedAgent(t *testing.T) {
	mock := &mockExecer{}
	l := &Launcher{
		Execer:   mock,
		LookPath: mockLookPath(map[string]string{"aider": "/usr/local/bin/aider"}),
		Yolo:     true,
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	err := l.Passthrough(t.TempDir(), "", nil)
	if err == nil {
		t.Fatal("expected error for unsupported yolo agent")
	}
	if !strings.Contains(err.Error(), "--yolo not supported") {
		t.Errorf("expected '--yolo not supported' error, got: %v", err)
	}
}

func TestPassthrough_AppliesSandbox(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("sandbox-exec only available on macOS")
	}

	mock := &mockExecer{}
	cwd := t.TempDir()
	l := &Launcher{
		Execer:   mock,
		LookPath: mockLookPath(map[string]string{"claude": "/usr/local/bin/claude"}),
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	err := l.Passthrough(cwd, "", nil)
	if err != nil {
		t.Fatalf("Passthrough failed: %v", err)
	}

	// On darwin, the binary should be rewritten to sandbox-exec
	if mock.binary != "/usr/bin/sandbox-exec" {
		t.Errorf("expected binary /usr/bin/sandbox-exec, got %s", mock.binary)
	}
	// Args should include -f <profile> and the original binary
	if len(mock.args) < 4 {
		t.Fatalf("expected at least 4 args (sandbox-exec -f <profile> <binary>), got %d: %v", len(mock.args), mock.args)
	}
	if mock.args[0] != "sandbox-exec" {
		t.Errorf("args[0] = %q, want %q", mock.args[0], "sandbox-exec")
	}
	if mock.args[1] != "-f" {
		t.Errorf("args[1] = %q, want %q", mock.args[1], "-f")
	}
	// args[2] should be the profile path
	if !strings.Contains(mock.args[2], "sandbox.sb") {
		t.Errorf("args[2] = %q, expected sandbox.sb profile path", mock.args[2])
	}
	// args[3] should be the original binary
	if mock.args[3] != "/usr/local/bin/claude" {
		t.Errorf("args[3] = %q, want %q", mock.args[3], "/usr/local/bin/claude")
	}
}

func TestPassthrough_ExecAgent_UsesCwd(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("sandbox-exec only available on macOS")
	}

	mock := &mockExecer{}
	cwd := t.TempDir()
	l := &Launcher{
		Execer:   mock,
		LookPath: mockLookPath(map[string]string{"claude": "/usr/local/bin/claude"}),
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	err := l.Passthrough(cwd, "", nil)
	if err != nil {
		t.Fatalf("Passthrough failed: %v", err)
	}

	// The sandbox profile should be written; read it and verify it contains the cwd
	// as a writable path (not "." or some other hardcoded value).
	if len(mock.args) < 3 {
		t.Fatalf("expected sandbox args, got: %v", mock.args)
	}
	profilePath := mock.args[2]
	profileData, err := os.ReadFile(profilePath)
	if err != nil {
		t.Fatalf("failed to read sandbox profile at %s: %v", profilePath, err)
	}
	profile := string(profileData)

	// The profile should contain the actual cwd (project root) as a writable subpath
	if !strings.Contains(profile, cwd) {
		t.Errorf("sandbox profile does not contain cwd %q;\nprofile:\n%s", cwd, profile)
	}
}

func TestPassthrough_NoOptOut_AlwaysSandboxed(t *testing.T) {
	// Verify that execAgent always applies sandbox — there is no parameter or
	// field on Launcher that can disable sandbox in passthrough mode.
	mock := &mockExecer{}
	cwd := t.TempDir()
	l := &Launcher{
		Execer:   mock,
		LookPath: mockLookPath(map[string]string{"claude": "/usr/local/bin/claude"}),
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	// Even without Yolo, sandbox should be applied
	err := l.Passthrough(cwd, "claude", nil)
	if err != nil {
		t.Fatalf("Passthrough failed: %v", err)
	}

	// The binary should be a sandbox wrapper, not the agent directly
	innerBin, _ := unwrapSandbox(t, mock.binary, mock.args)
	if innerBin != "/usr/local/bin/claude" {
		t.Errorf("expected inner binary /usr/local/bin/claude, got %s", innerBin)
	}
	if mock.binary == "/usr/local/bin/claude" {
		t.Error("expected sandbox wrapping, but agent was executed directly")
	}
}

func TestIsKnownAgent(t *testing.T) {
	if !IsKnownAgent("claude") {
		t.Error("expected claude to be known")
	}
	if !IsKnownAgent("codex") {
		t.Error("expected codex to be known")
	}
	if IsKnownAgent("vim") {
		t.Error("expected vim to be unknown")
	}
	if IsKnownAgent("") {
		t.Error("expected empty string to be unknown")
	}
}
