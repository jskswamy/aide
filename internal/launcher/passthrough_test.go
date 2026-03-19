package launcher

import (
	"fmt"
	"os"
	"path/filepath"
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

	err := l.Passthrough(t.TempDir(), "", nil)
	if err != nil {
		t.Fatalf("Passthrough failed: %v", err)
	}

	if mock.binary != "/usr/local/bin/claude" {
		t.Errorf("expected binary /usr/local/bin/claude, got %s", mock.binary)
	}
}

func TestPassthrough_SingleAgentWithArgs(t *testing.T) {
	mock := &mockExecer{}
	l := &Launcher{
		Execer:   mock,
		LookPath: mockLookPath(map[string]string{"codex": "/usr/bin/codex"}),
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	extraArgs := []string{"--model", "opus", "help me"}
	err := l.Passthrough(t.TempDir(), "", extraArgs)
	if err != nil {
		t.Fatalf("Passthrough failed: %v", err)
	}

	expected := []string{"/usr/bin/codex", "--model", "opus", "help me"}
	if len(mock.args) != len(expected) {
		t.Fatalf("expected %d args, got %d: %v", len(expected), len(mock.args), mock.args)
	}
	for i, want := range expected {
		if mock.args[i] != want {
			t.Errorf("args[%d] = %q, want %q", i, mock.args[i], want)
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

	// With --agent codex, should launch codex directly even though multiple found.
	err := l.Passthrough(t.TempDir(), "codex", []string{"--help"})
	if err != nil {
		t.Fatalf("Passthrough with --agent failed: %v", err)
	}

	if mock.binary != "/usr/local/bin/codex" {
		t.Errorf("expected binary /usr/local/bin/codex, got %s", mock.binary)
	}
	expected := []string{"/usr/local/bin/codex", "--help"}
	if len(mock.args) != len(expected) {
		t.Fatalf("expected %d args, got %d: %v", len(expected), len(mock.args), mock.args)
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

	err := l.Passthrough(t.TempDir(), "", []string{"--model", "opus"})
	if err != nil {
		t.Fatalf("Passthrough failed: %v", err)
	}

	expected := []string{"/usr/local/bin/claude", "--dangerously-skip-permissions", "--model", "opus"}
	if len(mock.args) != len(expected) {
		t.Fatalf("expected %d args, got %d: %v", len(expected), len(mock.args), mock.args)
	}
	for i, want := range expected {
		if mock.args[i] != want {
			t.Errorf("args[%d] = %q, want %q", i, mock.args[i], want)
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

	err := l.Passthrough(t.TempDir(), "", nil)
	if err == nil {
		t.Fatal("expected error for unsupported yolo agent")
	}
	if !strings.Contains(err.Error(), "--yolo not supported") {
		t.Errorf("expected '--yolo not supported' error, got: %v", err)
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
