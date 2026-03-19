package launcher

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/secrets"
)

// mockExecer captures exec arguments instead of actually executing.
type mockExecer struct {
	binary string
	args   []string
	env    []string
	err    error // error to return from Exec
}

func (m *mockExecer) Exec(binary string, args []string, env []string) error {
	m.binary = binary
	m.args = args
	m.env = env
	return m.err
}

// repoTestdataDir returns the absolute path to the repo-root testdata/ directory.
func repoTestdataDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("unable to determine test file path")
	}
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "testdata")
}

// writeMinimalConfig writes a minimal config.yaml to the given configDir.
func writeMinimalConfig(t *testing.T, configDir string, content string) {
	t.Helper()
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// envValue looks up a key in a KEY=VALUE slice.
func envValue(env []string, key string) (string, bool) {
	prefix := key + "="
	for _, e := range env {
		if strings.HasPrefix(e, prefix) {
			return e[len(prefix):], true
		}
	}
	return "", false
}

func TestLauncher_MinimalConfig(t *testing.T) {
	configDir := t.TempDir()
	cwd := t.TempDir()

	writeMinimalConfig(t, configDir, `
agent: /usr/local/bin/my-agent
env:
  FOO: bar
  BAZ: qux
`)

	mock := &mockExecer{}
	l := &Launcher{
		Execer:    mock,
		ConfigDir: configDir,
	}

	if err := l.Launch(cwd, "", nil, false); err != nil {
		t.Fatalf("Launch failed: %v", err)
	}

	if mock.binary != "/usr/local/bin/my-agent" {
		t.Errorf("expected binary /usr/local/bin/my-agent, got %s", mock.binary)
	}

	foo, ok := envValue(mock.env, "FOO")
	if !ok || foo != "bar" {
		t.Errorf("expected FOO=bar in env, got ok=%v val=%q", ok, foo)
	}

	baz, ok := envValue(mock.env, "BAZ")
	if !ok || baz != "qux" {
		t.Errorf("expected BAZ=qux in env, got ok=%v val=%q", ok, baz)
	}
}

func TestLauncher_WithSecrets(t *testing.T) {
	td := repoTestdataDir(t)
	keyFile := filepath.Join(td, "age-key.txt")
	encFile := filepath.Join(td, "test-secrets.enc.yaml")

	// Verify the test fixtures exist before proceeding.
	if _, err := os.Stat(keyFile); err != nil {
		t.Skipf("test age key not found at %s: %v", keyFile, err)
	}
	if _, err := os.Stat(encFile); err != nil {
		t.Skipf("test encrypted secrets not found at %s: %v", encFile, err)
	}

	// Set up SOPS_AGE_KEY_FILE so DiscoverAgeKey finds our test key.
	t.Setenv("SOPS_AGE_KEY_FILE", keyFile)

	configDir := t.TempDir()
	cwd := t.TempDir()

	// Use absolute path to the encrypted secrets file.
	writeMinimalConfig(t, configDir, fmt.Sprintf(`
agent: /usr/local/bin/my-agent
secrets_file: %s
env:
  API_KEY: "{{ index .secrets \"anthropic_api_key\" }}"
  PLAIN: literal-value
`, encFile))

	mock := &mockExecer{}
	l := &Launcher{
		Execer:    mock,
		ConfigDir: configDir,
	}

	if err := l.Launch(cwd, "", nil, false); err != nil {
		t.Fatalf("Launch failed: %v", err)
	}

	apiKey, ok := envValue(mock.env, "API_KEY")
	if !ok {
		t.Fatal("expected API_KEY in env")
	}
	if apiKey == "" || strings.Contains(apiKey, "{{") {
		t.Errorf("expected resolved API_KEY, got %q", apiKey)
	}

	plain, ok := envValue(mock.env, "PLAIN")
	if !ok || plain != "literal-value" {
		t.Errorf("expected PLAIN=literal-value, got ok=%v val=%q", ok, plain)
	}
}

func TestLauncher_ArgsForwarded(t *testing.T) {
	configDir := t.TempDir()
	cwd := t.TempDir()

	writeMinimalConfig(t, configDir, `
agent: /usr/local/bin/my-agent
`)

	mock := &mockExecer{}
	l := &Launcher{
		Execer:    mock,
		ConfigDir: configDir,
	}

	extraArgs := []string{"--verbose", "--model", "opus"}
	if err := l.Launch(cwd, "", extraArgs, false); err != nil {
		t.Fatalf("Launch failed: %v", err)
	}

	// args[0] should be the binary, followed by extra args
	expectedArgs := append([]string{"/usr/local/bin/my-agent"}, extraArgs...)
	if len(mock.args) != len(expectedArgs) {
		t.Fatalf("expected %d args, got %d: %v", len(expectedArgs), len(mock.args), mock.args)
	}
	for i, want := range expectedArgs {
		if mock.args[i] != want {
			t.Errorf("args[%d] = %q, want %q", i, mock.args[i], want)
		}
	}
}

func TestLauncher_CleanEnvFlag(t *testing.T) {
	configDir := t.TempDir()
	cwd := t.TempDir()

	writeMinimalConfig(t, configDir, `
agent: /usr/local/bin/my-agent
env:
  CUSTOM_VAR: custom-value
`)

	// Set a non-essential env var to verify it gets filtered.
	t.Setenv("MY_RANDOM_VAR", "should-be-gone")

	mock := &mockExecer{}
	l := &Launcher{
		Execer:    mock,
		ConfigDir: configDir,
	}

	if err := l.Launch(cwd, "", nil, true); err != nil {
		t.Fatalf("Launch failed: %v", err)
	}

	// The non-essential var should NOT be in the env.
	if _, ok := envValue(mock.env, "MY_RANDOM_VAR"); ok {
		t.Error("expected MY_RANDOM_VAR to be filtered with cleanEnv=true")
	}

	// Essential vars like PATH and HOME should be present.
	if _, ok := envValue(mock.env, "PATH"); !ok {
		t.Error("expected PATH in env with cleanEnv=true")
	}

	// The custom var from config should be present.
	if val, ok := envValue(mock.env, "CUSTOM_VAR"); !ok || val != "custom-value" {
		t.Errorf("expected CUSTOM_VAR=custom-value, got ok=%v val=%q", ok, val)
	}
}

func TestLauncher_AgentOverride(t *testing.T) {
	configDir := t.TempDir()
	cwd := t.TempDir()

	// Full config with two agents.
	writeMinimalConfig(t, configDir, `
agents:
  claude:
    binary: /usr/local/bin/claude
  codex:
    binary: /usr/local/bin/codex
contexts:
  default:
    agent: claude
default_context: default
`)

	mock := &mockExecer{}
	l := &Launcher{
		Execer:    mock,
		ConfigDir: configDir,
	}

	// Override to codex.
	if err := l.Launch(cwd, "codex", nil, false); err != nil {
		t.Fatalf("Launch failed: %v", err)
	}

	if mock.binary != "/usr/local/bin/codex" {
		t.Errorf("expected binary /usr/local/bin/codex, got %s", mock.binary)
	}
}

func TestLauncher_ContextResolutionError(t *testing.T) {
	configDir := t.TempDir()
	cwd := t.TempDir()

	// Full config with no default context and no matching rules.
	writeMinimalConfig(t, configDir, `
agents:
  claude:
    binary: /usr/local/bin/claude
contexts:
  work:
    agent: claude
    match:
      - remote: "github.com/company/*"
`)

	mock := &mockExecer{}
	l := &Launcher{
		Execer:    mock,
		ConfigDir: configDir,
	}

	err := l.Launch(cwd, "", nil, false)
	if err == nil {
		t.Fatal("expected error when no context matches, got nil")
	}
	if !strings.Contains(err.Error(), "resolving context") {
		t.Errorf("expected context resolution error, got: %v", err)
	}
}

func TestLauncher_CleanupOnError(t *testing.T) {
	td := repoTestdataDir(t)
	keyFile := filepath.Join(td, "age-key.txt")

	// Verify the test fixture exists.
	if _, err := os.Stat(keyFile); err != nil {
		t.Skipf("test age key not found at %s: %v", keyFile, err)
	}

	// Point to a nonexistent secrets file so decryption fails.
	configDir := t.TempDir()
	cwd := t.TempDir()

	writeMinimalConfig(t, configDir, `
agent: /usr/local/bin/my-agent
secrets_file: /nonexistent/secrets.enc.yaml
`)

	// Set up age key so DiscoverAgeKey succeeds but DecryptSecretsFile fails.
	t.Setenv("SOPS_AGE_KEY_FILE", keyFile)

	mock := &mockExecer{}
	l := &Launcher{
		Execer:    mock,
		ConfigDir: configDir,
	}

	err := l.Launch(cwd, "", nil, false)
	if err == nil {
		t.Fatal("expected error from secrets decryption, got nil")
	}
	if !strings.Contains(err.Error(), "decrypting secrets") {
		t.Errorf("expected decrypting secrets error, got: %v", err)
	}

	// Verify the mock was NOT called (we didn't reach exec).
	if mock.binary != "" {
		t.Error("expected exec not to be called on error")
	}

	// We can't directly check the runtime dir is cleaned up because it's
	// internal to Launch, but we verify the error path completes without panic.
	// The runtime dir cleanup is invoked in the error path before returning.

	// Verify no stale runtime dirs remain for our PID by checking that
	// the runtime dir path pattern doesn't linger.
	base := os.Getenv("XDG_RUNTIME_DIR")
	if base == "" {
		base = os.TempDir()
	}
	entries, err := os.ReadDir(base)
	if err == nil {
		pidStr := fmt.Sprintf("aide-%d", os.Getpid())
		for _, entry := range entries {
			if entry.Name() == pidStr {
				t.Errorf("runtime dir %s was not cleaned up after error", entry.Name())
			}
		}
	}
}

// TestLauncher_WithSecrets_DiscoverKeyFromEnv verifies that secrets discovery
// uses the SOPS_AGE_KEY environment variable directly.
func TestLauncher_WithSecrets_DiscoverKeyFromEnv(t *testing.T) {
	td := repoTestdataDir(t)
	keyFile := filepath.Join(td, "age-key.txt")
	encFile := filepath.Join(td, "test-secrets.enc.yaml")

	// Verify fixtures exist.
	if _, err := os.Stat(keyFile); err != nil {
		t.Skipf("test age key not found: %v", err)
	}

	// Read the secret key directly and set it via env var.
	keyData, err := os.ReadFile(keyFile)
	if err != nil {
		t.Fatal(err)
	}
	var secretKey string
	for _, line := range strings.Split(string(keyData), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "AGE-SECRET-KEY-") {
			secretKey = line
			break
		}
	}
	if secretKey == "" {
		t.Fatal("could not find AGE-SECRET-KEY in test key file")
	}

	// Verify we can decrypt with this key directly (sanity check).
	identity := &secrets.AgeIdentity{
		Source:  secrets.SourceEnvKey,
		KeyData: secretKey,
	}
	_, err = secrets.DecryptSecretsFile(encFile, identity)
	if err != nil {
		t.Skipf("cannot decrypt test secrets (sops issue?): %v", err)
	}

	t.Setenv("SOPS_AGE_KEY", secretKey)
	// Clear SOPS_AGE_KEY_FILE to avoid conflicts.
	t.Setenv("SOPS_AGE_KEY_FILE", "")

	configDir := t.TempDir()
	cwd := t.TempDir()

	writeMinimalConfig(t, configDir, fmt.Sprintf(`
agent: /usr/local/bin/my-agent
secrets_file: %s
env:
  SECRET_VAL: "{{ index .secrets \"anthropic_api_key\" }}"
`, encFile))

	mock := &mockExecer{}
	l := &Launcher{
		Execer:    mock,
		ConfigDir: configDir,
	}

	if err := l.Launch(cwd, "", nil, false); err != nil {
		t.Fatalf("Launch failed: %v", err)
	}

	val, ok := envValue(mock.env, "SECRET_VAL")
	if !ok {
		t.Fatal("expected SECRET_VAL in env")
	}
	if val == "" {
		t.Error("expected non-empty SECRET_VAL")
	}
}

func TestLauncher_ResolvesAgentFromPATH(t *testing.T) {
	configDir := t.TempDir()
	cwd := t.TempDir()

	// Config uses a bare agent name (not absolute path).
	writeMinimalConfig(t, configDir, `
agent: my-agent
env:
  FOO: bar
`)

	mock := &mockExecer{}
	l := &Launcher{
		Execer:    mock,
		ConfigDir: configDir,
		// Mock LookPath to resolve "my-agent" to an absolute path.
		LookPath: func(file string) (string, error) {
			if file == "my-agent" {
				return "/usr/local/bin/my-agent", nil
			}
			return "", fmt.Errorf("%s: not found", file)
		},
	}

	if err := l.Launch(cwd, "", nil, false); err != nil {
		t.Fatalf("Launch failed: %v", err)
	}

	// Binary should be resolved to the absolute path.
	if mock.binary != "/usr/local/bin/my-agent" {
		t.Errorf("expected binary /usr/local/bin/my-agent, got %s", mock.binary)
	}
}

func TestLauncher_AgentNotOnPATH(t *testing.T) {
	configDir := t.TempDir()
	cwd := t.TempDir()

	writeMinimalConfig(t, configDir, `
agent: nonexistent-agent
`)

	mock := &mockExecer{}
	l := &Launcher{
		Execer:    mock,
		ConfigDir: configDir,
		LookPath: func(file string) (string, error) {
			return "", fmt.Errorf("%s: not found", file)
		},
	}

	err := l.Launch(cwd, "", nil, false)
	if err == nil {
		t.Fatal("expected error when agent not on PATH, got nil")
	}
	if !strings.Contains(err.Error(), "not found on PATH") {
		t.Errorf("expected 'not found on PATH' error, got: %v", err)
	}
	if mock.binary != "" {
		t.Error("expected exec not to be called when agent not found")
	}
}

func TestLauncher_AbsolutePathSkipsLookPath(t *testing.T) {
	configDir := t.TempDir()
	cwd := t.TempDir()

	writeMinimalConfig(t, configDir, `
agent: /usr/local/bin/my-agent
`)

	lookPathCalled := false
	mock := &mockExecer{}
	l := &Launcher{
		Execer:    mock,
		ConfigDir: configDir,
		LookPath: func(file string) (string, error) {
			lookPathCalled = true
			return "", fmt.Errorf("should not be called")
		},
	}

	if err := l.Launch(cwd, "", nil, false); err != nil {
		t.Fatalf("Launch failed: %v", err)
	}

	if lookPathCalled {
		t.Error("LookPath should not be called for absolute paths")
	}
	if mock.binary != "/usr/local/bin/my-agent" {
		t.Errorf("expected binary /usr/local/bin/my-agent, got %s", mock.binary)
	}
}

func TestYoloArgs_Claude(t *testing.T) {
	args, err := YoloArgs("claude")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(args) != 1 || args[0] != "--dangerously-skip-permissions" {
		t.Errorf("expected [--dangerously-skip-permissions], got %v", args)
	}
}

func TestYoloArgs_Codex(t *testing.T) {
	args, err := YoloArgs("codex")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(args) != 1 || args[0] != "--full-auto" {
		t.Errorf("expected [--full-auto], got %v", args)
	}
}

func TestYoloArgs_AbsolutePath(t *testing.T) {
	// Agent specified as full path should still match by basename.
	args, err := YoloArgs("/usr/local/bin/claude")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(args) != 1 || args[0] != "--dangerously-skip-permissions" {
		t.Errorf("expected [--dangerously-skip-permissions], got %v", args)
	}
}

func TestYoloArgs_UnsupportedAgent(t *testing.T) {
	_, err := YoloArgs("vim")
	if err == nil {
		t.Fatal("expected error for unsupported agent, got nil")
	}
	if !strings.Contains(err.Error(), "--yolo not supported") {
		t.Errorf("expected '--yolo not supported' error, got: %v", err)
	}
}

func TestLauncher_YoloInjectsFlag(t *testing.T) {
	configDir := t.TempDir()
	cwd := t.TempDir()

	writeMinimalConfig(t, configDir, `
agent: claude
`)

	mock := &mockExecer{}
	l := &Launcher{
		Execer:    mock,
		ConfigDir: configDir,
		LookPath: func(file string) (string, error) {
			if file == "claude" {
				return "/usr/local/bin/claude", nil
			}
			return "", fmt.Errorf("not found")
		},
		Yolo: true,
	}

	if err := l.Launch(cwd, "", []string{"--model", "opus"}, false); err != nil {
		t.Fatalf("Launch failed: %v", err)
	}

	// args[0] is binary, then yolo flag, then user args.
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

func TestLauncher_YoloUnsupportedAgent(t *testing.T) {
	configDir := t.TempDir()
	cwd := t.TempDir()

	writeMinimalConfig(t, configDir, `
agent: vim
`)

	mock := &mockExecer{}
	l := &Launcher{
		Execer:    mock,
		ConfigDir: configDir,
		Yolo:      true,
	}

	err := l.Launch(cwd, "", nil, false)
	if err == nil {
		t.Fatal("expected error for unsupported yolo agent, got nil")
	}
	if !strings.Contains(err.Error(), "--yolo not supported") {
		t.Errorf("expected '--yolo not supported' error, got: %v", err)
	}
}
