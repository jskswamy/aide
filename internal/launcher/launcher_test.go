package launcher

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"go.uber.org/mock/gomock"

	"github.com/jskswamy/aide/internal/launcher/mocks"
	"github.com/jskswamy/aide/internal/secrets"
)

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

// unwrapSandbox extracts the inner binary and args from a sandbox-wrapped exec.
// On darwin: sandbox-exec -f <profile> <binary> <args...>
// On linux (bwrap):    bwrap <bwrap-args...> -- <binary> <args...>
// On linux (landlock): <aide> __sandbox-apply <policy> -- <binary> <args...>
// If not sandboxed, it returns the values as-is.
func unwrapSandbox(t *testing.T, binary string, args []string) (innerBinary string, innerArgs []string) {
	t.Helper()
	if runtime.GOOS == "darwin" && binary == "/usr/bin/sandbox-exec" {
		// args: [sandbox-exec, -f, <profile>, <inner-binary>, <inner-args...>]
		if len(args) < 4 {
			t.Fatalf("sandbox-exec args too short: %v", args)
		}
		return args[3], args[3:]
	}
	// Check for bwrap (Linux)
	if strings.HasSuffix(binary, "/bwrap") || binary == "bwrap" {
		// args: [bwrap, <bwrap-flags...>, --, <inner-binary>, <inner-args...>]
		for i, a := range args {
			if a == "--" && i+1 < len(args) {
				return args[i+1], args[i+1:]
			}
		}
		t.Fatalf("bwrap args missing -- separator: %v", args)
	}
	// Check for Landlock re-exec pattern: <aide> __sandbox-apply <policy> -- <binary> <args...>
	if len(args) >= 4 && args[1] == "__sandbox-apply" {
		for i, a := range args {
			if a == "--" && i+1 < len(args) {
				return args[i+1], args[i+1:]
			}
		}
		t.Fatalf("landlock args missing -- separator: %v", args)
	}
	return binary, args
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

	ctrl := gomock.NewController(t)
	var capturedBinary string
	var capturedArgs []string
	var capturedEnv []string
	mockExec := mocks.NewMockExecer(ctrl)
	mockExec.EXPECT().
		Exec(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(binary string, args []string, env []string) error {
			capturedBinary = binary
			capturedArgs = args
			capturedEnv = env
			return nil
		})
	l := &Launcher{
		Execer:    mockExec,
		ConfigDir: configDir,
	}

	if err := l.Launch(cwd, "", nil, false, false, nil, nil); err != nil {
		t.Fatalf("Launch failed: %v", err)
	}

	innerBinary, _ := unwrapSandbox(t, capturedBinary, capturedArgs)
	if innerBinary != "/usr/local/bin/my-agent" {
		t.Errorf("expected binary /usr/local/bin/my-agent, got %s", innerBinary)
	}

	foo, ok := envValue(capturedEnv, "FOO")
	if !ok || foo != "bar" {
		t.Errorf("expected FOO=bar in env, got ok=%v val=%q", ok, foo)
	}

	baz, ok := envValue(capturedEnv, "BAZ")
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
secret: %s
env:
  API_KEY: "{{ index .secrets \"anthropic_api_key\" }}"
  PLAIN: literal-value
`, encFile))

	ctrl := gomock.NewController(t)
	var capturedEnv []string
	mockExec := mocks.NewMockExecer(ctrl)
	mockExec.EXPECT().
		Exec(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(binary string, args []string, env []string) error {
			capturedEnv = env
			return nil
		})
	l := &Launcher{
		Execer:    mockExec,
		ConfigDir: configDir,
	}

	if err := l.Launch(cwd, "", nil, false, false, nil, nil); err != nil {
		t.Fatalf("Launch failed: %v", err)
	}

	apiKey, ok := envValue(capturedEnv, "API_KEY")
	if !ok {
		t.Fatal("expected API_KEY in env")
	}
	if apiKey == "" || strings.Contains(apiKey, "{{") {
		t.Errorf("expected resolved API_KEY, got %q", apiKey)
	}

	plain, ok := envValue(capturedEnv, "PLAIN")
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

	ctrl := gomock.NewController(t)
	var capturedBinary string
	var capturedArgs []string
	mockExec := mocks.NewMockExecer(ctrl)
	mockExec.EXPECT().
		Exec(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(binary string, args []string, env []string) error {
			capturedBinary = binary
			capturedArgs = args
			return nil
		})
	l := &Launcher{
		Execer:    mockExec,
		ConfigDir: configDir,
	}

	extraArgs := []string{"--verbose", "--model", "opus"}
	if err := l.Launch(cwd, "", extraArgs, false, false, nil, nil); err != nil {
		t.Fatalf("Launch failed: %v", err)
	}

	// Unwrap sandbox to get the inner binary and args.
	_, innerArgs := unwrapSandbox(t, capturedBinary, capturedArgs)
	// innerArgs[0] should be the binary, followed by extra args
	expectedArgs := append([]string{"/usr/local/bin/my-agent"}, extraArgs...)
	if len(innerArgs) != len(expectedArgs) {
		t.Fatalf("expected %d inner args, got %d: %v", len(expectedArgs), len(innerArgs), innerArgs)
	}
	for i, want := range expectedArgs {
		if innerArgs[i] != want {
			t.Errorf("innerArgs[%d] = %q, want %q", i, innerArgs[i], want)
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

	ctrl := gomock.NewController(t)
	var capturedEnv []string
	mockExec := mocks.NewMockExecer(ctrl)
	mockExec.EXPECT().
		Exec(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(binary string, args []string, env []string) error {
			capturedEnv = env
			return nil
		})
	l := &Launcher{
		Execer:    mockExec,
		ConfigDir: configDir,
	}

	if err := l.Launch(cwd, "", nil, true, false, nil, nil); err != nil {
		t.Fatalf("Launch failed: %v", err)
	}

	// The non-essential var should NOT be in the env.
	if _, ok := envValue(capturedEnv, "MY_RANDOM_VAR"); ok {
		t.Error("expected MY_RANDOM_VAR to be filtered with cleanEnv=true")
	}

	// Essential vars like PATH and HOME should be present.
	if _, ok := envValue(capturedEnv, "PATH"); !ok {
		t.Error("expected PATH in env with cleanEnv=true")
	}

	// The custom var from config should be present.
	if val, ok := envValue(capturedEnv, "CUSTOM_VAR"); !ok || val != "custom-value" {
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

	ctrl := gomock.NewController(t)
	var capturedBinary string
	var capturedArgs []string
	mockExec := mocks.NewMockExecer(ctrl)
	mockExec.EXPECT().
		Exec(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(binary string, args []string, env []string) error {
			capturedBinary = binary
			capturedArgs = args
			return nil
		})
	l := &Launcher{
		Execer:    mockExec,
		ConfigDir: configDir,
	}

	// Override to codex.
	if err := l.Launch(cwd, "codex", nil, false, false, nil, nil); err != nil {
		t.Fatalf("Launch failed: %v", err)
	}

	innerBinary, _ := unwrapSandbox(t, capturedBinary, capturedArgs)
	if innerBinary != "/usr/local/bin/codex" {
		t.Errorf("expected binary /usr/local/bin/codex, got %s", innerBinary)
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

	ctrl := gomock.NewController(t)
	mockExec := mocks.NewMockExecer(ctrl)
	l := &Launcher{
		Execer:    mockExec,
		ConfigDir: configDir,
	}

	err := l.Launch(cwd, "", nil, false, false, nil, nil)
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
secret: /nonexistent/secrets.enc.yaml
`)

	// Set up age key so DiscoverAgeKey succeeds but DecryptSecretsFile fails.
	t.Setenv("SOPS_AGE_KEY_FILE", keyFile)

	ctrl := gomock.NewController(t)
	mockExec := mocks.NewMockExecer(ctrl)
	// No EXPECT set: gomock will fail the test if Exec is called unexpectedly.
	l := &Launcher{
		Execer:    mockExec,
		ConfigDir: configDir,
	}

	err := l.Launch(cwd, "", nil, false, false, nil, nil)
	if err == nil {
		t.Fatal("expected error from secrets decryption, got nil")
	}
	if !strings.Contains(err.Error(), "decrypting secrets") {
		t.Errorf("expected decrypting secrets error, got: %v", err)
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
secret: %s
env:
  SECRET_VAL: "{{ index .secrets \"anthropic_api_key\" }}"
`, encFile))

	ctrl := gomock.NewController(t)
	var capturedEnv []string
	mockExec := mocks.NewMockExecer(ctrl)
	mockExec.EXPECT().
		Exec(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(binary string, args []string, env []string) error {
			capturedEnv = env
			return nil
		})
	l := &Launcher{
		Execer:    mockExec,
		ConfigDir: configDir,
	}

	if err := l.Launch(cwd, "", nil, false, false, nil, nil); err != nil {
		t.Fatalf("Launch failed: %v", err)
	}

	val, ok := envValue(capturedEnv, "SECRET_VAL")
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

	ctrl := gomock.NewController(t)
	var capturedBinary string
	var capturedArgs []string
	mockExec := mocks.NewMockExecer(ctrl)
	mockExec.EXPECT().
		Exec(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(binary string, args []string, env []string) error {
			capturedBinary = binary
			capturedArgs = args
			return nil
		})
	l := &Launcher{
		Execer:    mockExec,
		ConfigDir: configDir,
		// Mock LookPath to resolve "my-agent" to an absolute path.
		LookPath: func(file string) (string, error) {
			if file == "my-agent" {
				return "/usr/local/bin/my-agent", nil
			}
			return "", fmt.Errorf("%s: not found", file)
		},
	}

	if err := l.Launch(cwd, "", nil, false, false, nil, nil); err != nil {
		t.Fatalf("Launch failed: %v", err)
	}

	// Binary should be resolved to the absolute path (may be wrapped in sandbox).
	innerBin, _ := unwrapSandbox(t, capturedBinary, capturedArgs)
	if innerBin != "/usr/local/bin/my-agent" {
		t.Errorf("expected inner binary /usr/local/bin/my-agent, got %s", innerBin)
	}
}

func TestLauncher_AgentNotOnPATH(t *testing.T) {
	configDir := t.TempDir()
	cwd := t.TempDir()

	writeMinimalConfig(t, configDir, `
agent: nonexistent-agent
`)

	ctrl := gomock.NewController(t)
	mockExec := mocks.NewMockExecer(ctrl)
	// No EXPECT set: gomock will fail the test if Exec is called unexpectedly.
	l := &Launcher{
		Execer:    mockExec,
		ConfigDir: configDir,
		LookPath: func(file string) (string, error) {
			return "", fmt.Errorf("%s: not found", file)
		},
	}

	err := l.Launch(cwd, "", nil, false, false, nil, nil)
	if err == nil {
		t.Fatal("expected error when agent not on PATH, got nil")
	}
	if !strings.Contains(err.Error(), "not found on PATH") {
		t.Errorf("expected 'not found on PATH' error, got: %v", err)
	}
}

func TestLauncher_AbsolutePathSkipsLookPath(t *testing.T) {
	configDir := t.TempDir()
	cwd := t.TempDir()

	writeMinimalConfig(t, configDir, `
agent: /usr/local/bin/my-agent
`)

	lookPathCalled := false
	ctrl := gomock.NewController(t)
	var capturedBinary string
	var capturedArgs []string
	mockExec := mocks.NewMockExecer(ctrl)
	mockExec.EXPECT().
		Exec(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(binary string, args []string, env []string) error {
			capturedBinary = binary
			capturedArgs = args
			return nil
		})
	l := &Launcher{
		Execer:    mockExec,
		ConfigDir: configDir,
		LookPath: func(_ string) (string, error) {
			lookPathCalled = true
			return "", fmt.Errorf("should not be called")
		},
	}

	if err := l.Launch(cwd, "", nil, false, false, nil, nil); err != nil {
		t.Fatalf("Launch failed: %v", err)
	}

	if lookPathCalled {
		t.Error("LookPath should not be called for absolute paths")
	}
	innerBin, _ := unwrapSandbox(t, capturedBinary, capturedArgs)
	if innerBin != "/usr/local/bin/my-agent" {
		t.Errorf("expected inner binary /usr/local/bin/my-agent, got %s", innerBin)
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

	ctrl := gomock.NewController(t)
	var capturedBinary string
	var capturedArgs []string
	mockExec := mocks.NewMockExecer(ctrl)
	mockExec.EXPECT().
		Exec(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(binary string, args []string, env []string) error {
			capturedBinary = binary
			capturedArgs = args
			return nil
		})
	l := &Launcher{
		Execer:    mockExec,
		ConfigDir: configDir,
		LookPath: func(file string) (string, error) {
			if file == "claude" {
				return "/usr/local/bin/claude", nil
			}
			return "", fmt.Errorf("not found")
		},
		Yolo: true,
	}

	if err := l.Launch(cwd, "", []string{"--model", "opus"}, false, false, nil, nil); err != nil {
		t.Fatalf("Launch failed: %v", err)
	}

	// Unwrap sandbox to check inner args: binary, then yolo flag, then user args.
	_, innerArgs := unwrapSandbox(t, capturedBinary, capturedArgs)
	expected := []string{"/usr/local/bin/claude", "--dangerously-skip-permissions", "--model", "opus"}
	if len(innerArgs) != len(expected) {
		t.Fatalf("expected %d inner args, got %d: %v", len(expected), len(innerArgs), innerArgs)
	}
	for i, want := range expected {
		if innerArgs[i] != want {
			t.Errorf("innerArgs[%d] = %q, want %q", i, innerArgs[i], want)
		}
	}
}

func TestLauncher_AgentOverrideUnknown(t *testing.T) {
	configDir := t.TempDir()
	cwd := t.TempDir()

	writeMinimalConfig(t, configDir, `
agent: claude
`)

	ctrl := gomock.NewController(t)
	mockExec := mocks.NewMockExecer(ctrl)
	// No EXPECT set: gomock will fail the test if Exec is called unexpectedly.
	l := &Launcher{
		Execer:    mockExec,
		ConfigDir: configDir,
		LookPath: func(file string) (string, error) {
			// vim is not on PATH either
			if file == "vim" {
				return "", fmt.Errorf("not found")
			}
			return "/usr/bin/" + file, nil
		},
	}

	err := l.Launch(cwd, "vim", nil, false, false, nil, nil)
	if err == nil {
		t.Fatal("expected error for unknown agent override, got nil")
	}
	if !strings.Contains(err.Error(), "unknown agent") {
		t.Errorf("expected 'unknown agent' error, got: %v", err)
	}
}

func TestLauncher_AgentOverrideKnown(t *testing.T) {
	configDir := t.TempDir()
	cwd := t.TempDir()

	// Full config with both agents defined.
	writeMinimalConfig(t, configDir, `
agents:
  claude:
    binary: claude
  codex:
    binary: codex
contexts:
  default:
    agent: claude
default_context: default
`)

	ctrl := gomock.NewController(t)
	var capturedBinary string
	var capturedArgs []string
	mockExec := mocks.NewMockExecer(ctrl)
	mockExec.EXPECT().
		Exec(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(binary string, args []string, env []string) error {
			capturedBinary = binary
			capturedArgs = args
			return nil
		})
	l := &Launcher{
		Execer:    mockExec,
		ConfigDir: configDir,
		LookPath: func(file string) (string, error) {
			return "/usr/local/bin/" + file, nil
		},
	}

	err := l.Launch(cwd, "codex", nil, false, false, nil, nil)
	if err != nil {
		t.Fatalf("Launch with known agent override failed: %v", err)
	}
	innerBin, _ := unwrapSandbox(t, capturedBinary, capturedArgs)
	if innerBin != "/usr/local/bin/codex" {
		t.Errorf("expected inner binary /usr/local/bin/codex, got %s", innerBin)
	}
}

func TestLaunch_NoSandboxBlock_DefaultSandboxApplied(t *testing.T) {
	configDir := t.TempDir()
	cwd := t.TempDir()

	// Config has NO sandbox: block at all — Sandbox field will be nil.
	// With default-on sandbox, nil means default policy IS applied.
	writeMinimalConfig(t, configDir, `
agent: /usr/local/bin/my-agent
env:
  FOO: bar
`)

	ctrl := gomock.NewController(t)
	var capturedBinary string
	var capturedArgs []string
	mockExec := mocks.NewMockExecer(ctrl)
	mockExec.EXPECT().
		Exec(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(binary string, args []string, env []string) error {
			capturedBinary = binary
			capturedArgs = args
			return nil
		})
	l := &Launcher{
		Execer:    mockExec,
		ConfigDir: configDir,
	}

	if err := l.Launch(cwd, "", nil, false, false, nil, nil); err != nil {
		t.Fatalf("Launch failed: %v", err)
	}

	// With default-on sandbox, the binary should be wrapped by the platform sandbox.
	// On darwin: sandbox-exec, on linux: bwrap (or landlock re-exec).
	// Regardless of platform, the inner binary should be our agent.
	innerBinary, _ := unwrapSandbox(t, capturedBinary, capturedArgs)
	if innerBinary != "/usr/local/bin/my-agent" {
		t.Errorf("expected inner binary /usr/local/bin/my-agent, got %s", innerBinary)
	}
	// Verify the outer binary is a sandbox wrapper (not the agent directly)
	if capturedBinary == "/usr/local/bin/my-agent" {
		t.Error("expected sandbox wrapping (default-on), but agent was executed directly")
	}
}

func TestLaunch_ExplicitSandbox_Applied(t *testing.T) {
	configDir := t.TempDir()
	cwd := t.TempDir()

	// Config explicitly enables sandbox with writable paths.
	writeMinimalConfig(t, configDir, `
agent: /usr/local/bin/my-agent
sandbox:
  writable:
    - /tmp
  network: outbound
`)

	ctrl := gomock.NewController(t)
	var capturedBinary string
	var capturedArgs []string
	mockExec := mocks.NewMockExecer(ctrl)
	mockExec.EXPECT().
		Exec(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(binary string, args []string, env []string) error {
			capturedBinary = binary
			capturedArgs = args
			return nil
		})
	l := &Launcher{
		Execer:    mockExec,
		ConfigDir: configDir,
	}

	if err := l.Launch(cwd, "", nil, false, false, nil, nil); err != nil {
		t.Fatalf("Launch failed: %v", err)
	}

	// With explicit sandbox config, on darwin it should be wrapped with sandbox-exec.
	if runtime.GOOS == "darwin" {
		if capturedBinary != "/usr/bin/sandbox-exec" {
			t.Errorf("expected binary /usr/bin/sandbox-exec (sandbox applied), got %s", capturedBinary)
		}
		if len(capturedArgs) < 3 || capturedArgs[1] != "-f" {
			t.Errorf("expected sandbox-exec -f <profile> args, got %v", capturedArgs)
		}
	}
}

func TestLaunch_SandboxFalse_NoSandbox(t *testing.T) {
	configDir := t.TempDir()
	cwd := t.TempDir()

	// Config explicitly disables sandbox with `sandbox: false`.
	writeMinimalConfig(t, configDir, `
agent: /usr/local/bin/my-agent
sandbox: false
env:
  FOO: bar
`)

	ctrl2 := gomock.NewController(t)
	var capturedBinary2 string
	mockExec2 := mocks.NewMockExecer(ctrl2)
	mockExec2.EXPECT().
		Exec(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(binary string, args []string, env []string) error {
			capturedBinary2 = binary
			return nil
		})
	l := &Launcher{
		Execer:    mockExec2,
		ConfigDir: configDir,
	}

	if err := l.Launch(cwd, "", nil, false, false, nil, nil); err != nil {
		t.Fatalf("Launch failed: %v", err)
	}

	// With sandbox: false, the binary should be the agent directly, NOT sandbox-exec.
	if capturedBinary2 != "/usr/local/bin/my-agent" {
		t.Errorf("expected binary /usr/local/bin/my-agent (no sandbox), got %s", capturedBinary2)
	}
}

func TestLauncher_YoloUnsupportedAgent(t *testing.T) {
	configDir := t.TempDir()
	cwd := t.TempDir()

	writeMinimalConfig(t, configDir, `
agent: vim
`)

	ctrl := gomock.NewController(t)
	mockExec := mocks.NewMockExecer(ctrl)
	// No EXPECT set: gomock will fail the test if Exec is called unexpectedly.
	l := &Launcher{
		Execer:    mockExec,
		ConfigDir: configDir,
		Yolo:      true,
	}

	err := l.Launch(cwd, "", nil, false, false, nil, nil)
	if err == nil {
		t.Fatal("expected error for unsupported yolo agent, got nil")
	}
	if !strings.Contains(err.Error(), "--yolo not supported") {
		t.Errorf("expected '--yolo not supported' error, got: %v", err)
	}
}

func TestLaunch_BannerPrintsToStderr(t *testing.T) {
	configDir := t.TempDir()
	cwd := t.TempDir()
	writeMinimalConfig(t, configDir, `agent: /usr/local/bin/my-agent`)

	var stderrBuf bytes.Buffer
	ctrl := gomock.NewController(t)
	mockExec := mocks.NewMockExecer(ctrl)
	mockExec.EXPECT().Exec(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
	l := &Launcher{
		Execer:    mockExec,
		ConfigDir: configDir,
		Stderr:    &stderrBuf,
	}
	if err := l.Launch(cwd, "", nil, false, false, nil, nil); err != nil {
		t.Fatalf("Launch failed: %v", err)
	}
	if stderrBuf.Len() == 0 {
		t.Error("expected banner output on stderr")
	}
	if !strings.Contains(stderrBuf.String(), "aide") {
		t.Error("banner should contain 'aide'")
	}
}

func TestLaunch_ShowInfoFalse(t *testing.T) {
	configDir := t.TempDir()
	cwd := t.TempDir()
	writeMinimalConfig(t, configDir, "agents:\n  my-agent:\n    binary: /usr/local/bin/my-agent\ncontexts:\n  default:\n    agent: my-agent\ndefault_context: default\npreferences:\n  show_info: false\n")

	var stderrBuf bytes.Buffer
	ctrl := gomock.NewController(t)
	mockExec := mocks.NewMockExecer(ctrl)
	mockExec.EXPECT().Exec(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
	l := &Launcher{
		Execer:    mockExec,
		ConfigDir: configDir,
		Stderr:    &stderrBuf,
	}
	if err := l.Launch(cwd, "", nil, false, false, nil, nil); err != nil {
		t.Fatalf("Launch failed: %v", err)
	}
	if stderrBuf.Len() != 0 {
		t.Errorf("expected no banner with show_info: false, got: %s", stderrBuf.String())
	}
}

func TestLauncher_ConfigYoloEnabled(t *testing.T) {
	configDir := t.TempDir()
	cwd := t.TempDir()

	writeMinimalConfig(t, configDir, `
agent: claude
yolo: true
`)

	var stderrBuf bytes.Buffer
	ctrl := gomock.NewController(t)
	var capturedBinary string
	var capturedArgs []string
	mockExec := mocks.NewMockExecer(ctrl)
	mockExec.EXPECT().
		Exec(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(binary string, args []string, env []string) error {
			capturedBinary = binary
			capturedArgs = args
			return nil
		})
	l := &Launcher{
		Execer:    mockExec,
		ConfigDir: configDir,
		LookPath: func(file string) (string, error) {
			if file == "claude" {
				return "/usr/local/bin/claude", nil
			}
			return "", fmt.Errorf("not found")
		},
		Stderr: &stderrBuf,
	}

	if err := l.Launch(cwd, "", nil, false, false, nil, nil); err != nil {
		t.Fatalf("Launch failed: %v", err)
	}

	_, innerArgs := unwrapSandbox(t, capturedBinary, capturedArgs)
	found := false
	for _, a := range innerArgs {
		if a == "--dangerously-skip-permissions" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected --dangerously-skip-permissions in args, got %v", innerArgs)
	}
	if !strings.Contains(stderrBuf.String(), "AUTO-APPROVE") {
		t.Errorf("expected AUTO-APPROVE in banner, got: %s", stderrBuf.String())
	}
}

func TestLauncher_NoYoloOverridesConfigYolo(t *testing.T) {
	configDir := t.TempDir()
	cwd := t.TempDir()

	writeMinimalConfig(t, configDir, `
agent: claude
yolo: true
`)

	var stderrBuf bytes.Buffer
	ctrl := gomock.NewController(t)
	var capturedBinary string
	var capturedArgs []string
	mockExec := mocks.NewMockExecer(ctrl)
	mockExec.EXPECT().
		Exec(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(binary string, args []string, env []string) error {
			capturedBinary = binary
			capturedArgs = args
			return nil
		})
	l := &Launcher{
		Execer:    mockExec,
		ConfigDir: configDir,
		LookPath: func(file string) (string, error) {
			if file == "claude" {
				return "/usr/local/bin/claude", nil
			}
			return "", fmt.Errorf("not found")
		},
		NoYolo: true,
		Stderr: &stderrBuf,
	}

	if err := l.Launch(cwd, "", nil, false, false, nil, nil); err != nil {
		t.Fatalf("Launch failed: %v", err)
	}

	_, innerArgs := unwrapSandbox(t, capturedBinary, capturedArgs)
	for _, a := range innerArgs {
		if a == "--dangerously-skip-permissions" {
			t.Error("--no-yolo should suppress yolo flag injection")
		}
	}
	if strings.Contains(stderrBuf.String(), "yolo mode enabled") {
		t.Error("--no-yolo should suppress yolo warning")
	}
}

func TestLauncher_NoYoloOverridesCliYolo(t *testing.T) {
	configDir := t.TempDir()
	cwd := t.TempDir()

	writeMinimalConfig(t, configDir, `
agent: claude
`)

	ctrl := gomock.NewController(t)
	var capturedBinary2 string
	var capturedArgs2 []string
	mockExec2 := mocks.NewMockExecer(ctrl)
	mockExec2.EXPECT().
		Exec(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(binary string, args []string, env []string) error {
			capturedBinary2 = binary
			capturedArgs2 = args
			return nil
		})
	l := &Launcher{
		Execer:    mockExec2,
		ConfigDir: configDir,
		LookPath: func(file string) (string, error) {
			if file == "claude" {
				return "/usr/local/bin/claude", nil
			}
			return "", fmt.Errorf("not found")
		},
		Yolo:   true,
		NoYolo: true,
	}

	if err := l.Launch(cwd, "", nil, false, false, nil, nil); err != nil {
		t.Fatalf("Launch failed: %v", err)
	}

	_, innerArgs2 := unwrapSandbox(t, capturedBinary2, capturedArgs2)
	for _, a := range innerArgs2 {
		if a == "--dangerously-skip-permissions" {
			t.Error("--no-yolo should override --yolo")
		}
	}
}

func TestYoloSource(t *testing.T) {
	tr := true
	f := false

	tests := []struct {
		name     string
		cliFlag  bool
		pref     *bool
		ctx      *bool
		proj     *bool
		expected string
	}{
		{"cli flag", true, nil, nil, nil, "--yolo flag"},
		{"preferences", false, &tr, nil, nil, "preferences"},
		{"context", false, nil, &tr, nil, "context config"},
		{"project", false, nil, nil, &tr, ".aide.yaml"},
		{"cli beats all", true, &f, &f, &f, "--yolo flag"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := yoloSource(tt.cliFlag, tt.pref, tt.ctx, tt.proj)
			if got != tt.expected {
				t.Errorf("yoloSource() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestResolveEffectiveYolo(t *testing.T) {
	tr := true
	f := false

	tests := []struct {
		name     string
		yolo     bool
		noYolo   bool
		pref     *bool
		ctx      *bool
		proj     *bool
		expected bool
	}{
		{"all off", false, false, nil, nil, nil, false},
		{"cli yolo", true, false, nil, nil, nil, true},
		{"no-yolo overrides cli", true, true, nil, nil, nil, false},
		{"no-yolo overrides config", false, true, &tr, &tr, &tr, false},
		{"pref true", false, false, &tr, nil, nil, true},
		{"ctx true", false, false, nil, &tr, nil, true},
		{"ctx false overrides pref", false, false, &tr, &f, nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := &Launcher{Yolo: tt.yolo, NoYolo: tt.noYolo}
			got := l.resolveEffectiveYolo(tt.pref, tt.ctx, tt.proj)
			if got != tt.expected {
				t.Errorf("resolveEffectiveYolo() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestLauncher_YoloWarningContent(t *testing.T) {
	configDir := t.TempDir()
	cwd := t.TempDir()

	writeMinimalConfig(t, configDir, `
agent: claude
yolo: true
`)

	var stderrBuf bytes.Buffer
	ctrlYW := gomock.NewController(t)
	mockExecYW := mocks.NewMockExecer(ctrlYW)
	mockExecYW.EXPECT().Exec(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
	l := &Launcher{
		Execer:    mockExecYW,
		ConfigDir: configDir,
		LookPath: func(file string) (string, error) {
			if file == "claude" {
				return "/usr/local/bin/claude", nil
			}
			return "", fmt.Errorf("not found")
		},
		Stderr: &stderrBuf,
	}

	if err := l.Launch(cwd, "", nil, false, false, nil, nil); err != nil {
		t.Fatalf("Launch failed: %v", err)
	}

	out := stderrBuf.String()
	if !strings.Contains(out, "AUTO-APPROVE") {
		t.Errorf("expected AUTO-APPROVE in banner, got: %s", out)
	}
	if !strings.Contains(out, "without confirmation") {
		t.Errorf("expected 'without confirmation' in auto-approve banner, got: %s", out)
	}
}

func TestLauncher_ConfigYoloUnsupportedAgent(t *testing.T) {
	configDir := t.TempDir()
	cwd := t.TempDir()

	writeMinimalConfig(t, configDir, `
agent: vim
yolo: true
`)

	ctrlCYU := gomock.NewController(t)
	mockExecCYU := mocks.NewMockExecer(ctrlCYU)
	// No EXPECT set: gomock will fail the test if Exec is called unexpectedly.
	l := &Launcher{
		Execer:    mockExecCYU,
		ConfigDir: configDir,
	}

	err := l.Launch(cwd, "", nil, false, false, nil, nil)
	if err == nil {
		t.Fatal("expected error for unsupported yolo agent")
	}
	if !strings.Contains(err.Error(), "--yolo not supported") {
		t.Errorf("expected unsupported agent error, got: %v", err)
	}
}

func TestLauncher_ContextLevelYolo(t *testing.T) {
	configDir := t.TempDir()
	cwd := t.TempDir()

	writeMinimalConfig(t, configDir, `
agents:
  claude:
    binary: claude
contexts:
  work:
    agent: claude
    yolo: true
default_context: work
`)

	var stderrBuf bytes.Buffer
	ctrlCLY := gomock.NewController(t)
	var capturedBinaryCLY string
	var capturedArgsCLY []string
	mockExecCLY := mocks.NewMockExecer(ctrlCLY)
	mockExecCLY.EXPECT().
		Exec(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(binary string, args []string, env []string) error {
			capturedBinaryCLY = binary
			capturedArgsCLY = args
			return nil
		})
	l := &Launcher{
		Execer:    mockExecCLY,
		ConfigDir: configDir,
		LookPath: func(file string) (string, error) {
			if file == "claude" {
				return "/usr/local/bin/claude", nil
			}
			return "", fmt.Errorf("not found")
		},
		Stderr: &stderrBuf,
	}

	if err := l.Launch(cwd, "", nil, false, false, nil, nil); err != nil {
		t.Fatalf("Launch failed: %v", err)
	}

	_, innerArgs := unwrapSandbox(t, capturedBinaryCLY, capturedArgsCLY)
	found := false
	for _, a := range innerArgs {
		if a == "--dangerously-skip-permissions" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected yolo flag from context config, got %v", innerArgs)
	}
	if !strings.Contains(stderrBuf.String(), "AUTO-APPROVE") {
		t.Errorf("expected AUTO-APPROVE in banner for context-level yolo, got: %s", stderrBuf.String())
	}
}

func TestLaunch_ResolveFlagOverridesShowInfoFalse(t *testing.T) {
	configDir := t.TempDir()
	cwd := t.TempDir()
	writeMinimalConfig(t, configDir, "agents:\n  my-agent:\n    binary: /usr/local/bin/my-agent\ncontexts:\n  default:\n    agent: my-agent\ndefault_context: default\npreferences:\n  show_info: false\n")

	var stderrBuf bytes.Buffer
	ctrlRF := gomock.NewController(t)
	mockExecRF := mocks.NewMockExecer(ctrlRF)
	mockExecRF.EXPECT().Exec(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
	l := &Launcher{
		Execer:    mockExecRF,
		ConfigDir: configDir,
		Stderr:    &stderrBuf,
	}
	if err := l.Launch(cwd, "", nil, false, true, nil, nil); err != nil {
		t.Fatalf("Launch failed: %v", err)
	}
	if stderrBuf.Len() == 0 {
		t.Error("--resolve should override show_info: false")
	}
}
