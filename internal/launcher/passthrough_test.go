package launcher

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"go.uber.org/mock/gomock"

	"github.com/jskswamy/aide/internal/config"
	"github.com/jskswamy/aide/internal/launcher/mocks"
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

func TestZeroConfigLaunch_SingleAgent(t *testing.T) {
	requireClaudeHome(t)
	ctrl := gomock.NewController(t)
	var capturedBinary string
	var capturedArgs []string
	mockExec := mocks.NewMockExecer(ctrl)
	mockExec.EXPECT().
		Exec(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(binary string, args []string, _ []string) error {
			capturedBinary = binary
			capturedArgs = args
			return nil
		})
	cfgDir := t.TempDir()
	l := &Launcher{
		Execer:    mockExec,
		ConfigDir: cfgDir,
		LookPath:  mockLookPath(map[string]string{"claude": "/usr/local/bin/claude"}),
	}

	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	binDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(binDir, "claude"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)

	err := l.Launch(t.TempDir(), "", nil, false, false, nil, nil)
	if err != nil {
		t.Fatalf("Launch failed: %v", err)
	}

	innerBinary, _ := unwrapSandbox(t, capturedBinary, capturedArgs)
	if filepath.Base(innerBinary) != "claude" {
		t.Errorf("expected inner binary claude, got %s", innerBinary)
	}
	if _, err := os.Stat(filepath.Join(cfgDir, "profiles", "default.yaml")); err != nil {
		t.Errorf("expected profiles/default.yaml created on launch: %v", err)
	}
}

func TestZeroConfigLaunch_SingleAgentWithArgs(t *testing.T) {
	ctrl := gomock.NewController(t)
	var capturedBinary string
	var capturedArgs []string
	mockExec := mocks.NewMockExecer(ctrl)
	mockExec.EXPECT().
		Exec(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(binary string, args []string, _ []string) error {
			capturedBinary = binary
			capturedArgs = args
			return nil
		})
	binDir := t.TempDir()
	codexBin := filepath.Join(binDir, "codex")
	if err := os.WriteFile(codexBin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)

	l := &Launcher{
		Execer:    mockExec,
		ConfigDir: t.TempDir(),
		LookPath:  mockLookPath(map[string]string{"codex": codexBin}),
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	extraArgs := []string{"--model", "opus", "help me"}
	err := l.Launch(t.TempDir(), "", extraArgs, false, false, nil, nil)
	if err != nil {
		t.Fatalf("Launch failed: %v", err)
	}

	_, innerArgs := unwrapSandbox(t, capturedBinary, capturedArgs)
	expected := []string{codexBin, "--model", "opus", "help me"}
	if len(innerArgs) != len(expected) {
		t.Fatalf("expected %d inner args, got %d: %v", len(expected), len(innerArgs), innerArgs)
	}
	for i, want := range expected {
		if innerArgs[i] != want {
			t.Errorf("innerArgs[%d] = %q, want %q", i, innerArgs[i], want)
		}
	}
}

func TestZeroConfigLaunch_NoAgents(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockExec := mocks.NewMockExecer(ctrl)
	t.Setenv("PATH", t.TempDir())
	l := &Launcher{
		Execer:    mockExec,
		ConfigDir: t.TempDir(),
		LookPath:  mockLookPath(map[string]string{}),
	}

	err := l.Launch(t.TempDir(), "", nil, false, false, nil, nil)
	if err == nil {
		t.Fatal("expected error when no agents found, got nil")
	}
	if !strings.Contains(err.Error(), "no known agents found on PATH") {
		t.Errorf("expected 'no known agents found on PATH' error, got: %v", err)
	}
}

func TestZeroConfigLaunch_MultipleAgents_InteractivePromptAndNonTTY(t *testing.T) {
	requireClaudeHome(t)
	binDir := t.TempDir()
	claudeBin := filepath.Join(binDir, "claude")
	codexBin := filepath.Join(binDir, "codex")
	for _, b := range []string{claudeBin, codexBin} {
		if err := os.WriteFile(b, []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", binDir)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	// 1. Interactive TTY prompts once and saves user choice ("codex") to profiles/default.yaml.
	ctrl := gomock.NewController(t)
	var capturedBin string
	var capturedArgs []string
	mockExec := mocks.NewMockExecer(ctrl)
	mockExec.EXPECT().
		Exec(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(binary string, args []string, _ []string) error {
			capturedBin = binary
			capturedArgs = args
			return nil
		})
	cfgDir := t.TempDir()
	var stderr bytes.Buffer
	l := &Launcher{
		Execer:      mockExec,
		ConfigDir:   cfgDir,
		Interactive: true,
		Stdin:       strings.NewReader("codex\n"),
		StdinIsTTY:  func() bool { return true },
		Stderr:      &stderr,
		LookPath: mockLookPath(map[string]string{
			"claude": claudeBin,
			"codex":  codexBin,
		}),
	}
	if err := l.Launch(t.TempDir(), "", nil, false, false, nil, nil); err != nil {
		t.Fatalf("interactive Launch failed: %v", err)
	}
	if !strings.Contains(stderr.String(), "Detected agents on PATH: claude, codex") {
		t.Errorf("expected multi-agent prompt on stderr, got:\n%s", stderr.String())
	}
	innerBin, _ := unwrapSandbox(t, capturedBin, capturedArgs)
	if innerBin != codexBin {
		t.Errorf("innerBin = %q, want %q", innerBin, codexBin)
	}
	cfg, err := config.Load(cfgDir, "")
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if cfg.Contexts["default"].Agent != "codex" || len(cfg.Agents) != 2 {
		t.Errorf("persisted default profile agent=%q agents=%v", cfg.Contexts["default"].Agent, cfg.Agents)
	}

	// 2. Non-TTY auto-selects DefaultAgentCandidate ("claude") without prompting.
	ctrl2 := gomock.NewController(t)
	mockExec2 := mocks.NewMockExecer(ctrl2)
	mockExec2.EXPECT().Exec(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
	cfgDir2 := t.TempDir()
	var stderr2 bytes.Buffer
	l2 := &Launcher{
		Execer:      mockExec2,
		ConfigDir:   cfgDir2,
		Interactive: false,
		Stderr:      &stderr2,
		LookPath: mockLookPath(map[string]string{
			"claude": claudeBin,
			"codex":  codexBin,
		}),
	}
	if err := l2.Launch(t.TempDir(), "", nil, false, false, nil, nil); err != nil {
		t.Fatalf("non-TTY Launch failed: %v", err)
	}
	if strings.Contains(stderr2.String(), "Default agent for profile") {
		t.Errorf("unexpected prompt in non-TTY mode:\n%s", stderr2.String())
	}
	cfg2, err := config.Load(cfgDir2, "")
	if err != nil {
		t.Fatalf("config.Load 2: %v", err)
	}
	if cfg2.Contexts["default"].Agent != "claude" {
		t.Errorf("non-TTY default profile agent = %q, want claude", cfg2.Contexts["default"].Agent)
	}
}

func TestZeroConfigLaunch_AgentOverride(t *testing.T) {
	ctrl := gomock.NewController(t)
	var capturedBinary string
	var capturedArgs []string
	mockExec := mocks.NewMockExecer(ctrl)
	mockExec.EXPECT().
		Exec(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(binary string, args []string, _ []string) error {
			capturedBinary = binary
			capturedArgs = args
			return nil
		})
	binDir := t.TempDir()
	claudeBin := filepath.Join(binDir, "claude")
	codexBin := filepath.Join(binDir, "codex")
	for _, b := range []string{claudeBin, codexBin} {
		if err := os.WriteFile(b, []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", binDir)
	l := &Launcher{
		Execer:    mockExec,
		ConfigDir: t.TempDir(),
		LookPath: mockLookPath(map[string]string{
			"claude": claudeBin,
			"codex":  codexBin,
		}),
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	// With --agent codex, should launch codex directly even though multiple found.
	err := l.Launch(t.TempDir(), "codex", []string{"--help"}, false, false, nil, nil)
	if err != nil {
		t.Fatalf("Launch with --agent failed: %v", err)
	}

	innerBinary, innerArgs := unwrapSandbox(t, capturedBinary, capturedArgs)
	if innerBinary != codexBin {
		t.Errorf("expected inner binary %s, got %s", codexBin, innerBinary)
	}
	expected := []string{codexBin, "--help"}
	if len(innerArgs) != len(expected) {
		t.Fatalf("expected %d inner args, got %d: %v", len(expected), len(innerArgs), innerArgs)
	}
}

func TestZeroConfigLaunch_AgentOverrideNotOnPath(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockExec := mocks.NewMockExecer(ctrl)
	t.Setenv("PATH", t.TempDir())
	l := &Launcher{
		Execer:    mockExec,
		ConfigDir: t.TempDir(),
		LookPath:  mockLookPath(map[string]string{}), // nothing on PATH
	}

	err := l.Launch(t.TempDir(), "nonexistent", nil, false, false, nil, nil)
	if err == nil {
		t.Fatal("expected error for agent not on PATH, got nil")
	}
	if !strings.Contains(err.Error(), "unknown agent") {
		t.Errorf("expected 'unknown agent' error, got: %v", err)
	}
}

func TestDefaultProfile_LegacyDirAndIgnoredOverrideNotices(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	legacyDir := filepath.Join(home, ".claude-default")
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfgDir := t.TempDir()
	profDir := filepath.Join(cfgDir, "profiles")
	if err := os.MkdirAll(profDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "config.yaml"), []byte("agents:\n  claude:\n    binary: claude\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(profDir, "default.yaml"), []byte("agent: claude\nconfig_dir_name: custom\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(cfgDir, "")
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	l := &Launcher{
		ConfigDir: cfgDir,
		LookPath:  mockLookPath(map[string]string{"claude": "/usr/local/bin/claude"}),
	}
	res, err := l.ResolveSelection(cfg, t.TempDir(), home, SelectionInputs{})
	if err != nil {
		t.Fatalf("ResolveSelection: %v", err)
	}
	joined := strings.Join(res.Notices, "\n")
	if !strings.Contains(joined, `profile "default" always uses native harness config directories; ignoring config_dir override in profiles/default.yaml`) {
		t.Errorf("missing ignored config_dir notice in:\n%s", joined)
	}
	if !strings.Contains(joined, `profile "default" now uses ~/.claude; delete legacy directory ~/.claude-default if you no longer need it`) {
		t.Errorf("missing legacy directory cleanup notice in:\n%s", joined)
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

func TestZeroConfigLaunch_YoloInjectsFlag(t *testing.T) {
	requireClaudeHome(t)
	ctrl := gomock.NewController(t)
	var capturedBinary string
	var capturedArgs []string
	mockExec := mocks.NewMockExecer(ctrl)
	mockExec.EXPECT().
		Exec(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(binary string, args []string, _ []string) error {
			capturedBinary = binary
			capturedArgs = args
			return nil
		})
	binDir := t.TempDir()
	claudeBin := filepath.Join(binDir, "claude")
	if err := os.WriteFile(claudeBin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)
	l := &Launcher{
		Execer:    mockExec,
		ConfigDir: t.TempDir(),
		LookPath:  mockLookPath(map[string]string{"claude": claudeBin}),
		Yolo:      true,
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	err := l.Launch(t.TempDir(), "", []string{"--model", "opus"}, false, false, nil, nil)
	if err != nil {
		t.Fatalf("Launch failed: %v", err)
	}

	expected := []string{claudeBin, "--dangerously-skip-permissions", "--model", "opus"}
	_, innerArgs := unwrapSandbox(t, capturedBinary, capturedArgs)
	if len(innerArgs) != len(expected) {
		t.Fatalf("expected %d inner args, got %d: %v", len(expected), len(innerArgs), innerArgs)
	}
	for i, want := range expected {
		if innerArgs[i] != want {
			t.Errorf("innerArgs[%d] = %q, want %q", i, innerArgs[i], want)
		}
	}
}

func TestZeroConfigLaunch_YoloOpenCodeSubcommandRun(t *testing.T) {
	ctrl := gomock.NewController(t)
	var capturedBinary string
	var capturedArgs []string
	mockExec := mocks.NewMockExecer(ctrl)
	mockExec.EXPECT().
		Exec(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(binary string, args []string, _ []string) error {
			capturedBinary = binary
			capturedArgs = args
			return nil
		})
	binDir := t.TempDir()
	opencodeBin := filepath.Join(binDir, "opencode")
	if err := os.WriteFile(opencodeBin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)
	l := &Launcher{
		Execer:    mockExec,
		ConfigDir: t.TempDir(),
		LookPath:  mockLookPath(map[string]string{"opencode": opencodeBin}),
		Yolo:      true,
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	err := l.Launch(t.TempDir(), "opencode", []string{"run", "-m", "openrouter/openai/gpt-4o-mini", "hello"}, false, false, nil, nil)
	if err != nil {
		t.Fatalf("Launch failed: %v", err)
	}

	expected := []string{opencodeBin, "run", "--auto", "--standalone", "-m", "openrouter/openai/gpt-4o-mini", "hello"}
	_, innerArgs := unwrapSandbox(t, capturedBinary, capturedArgs)
	if !slices.Equal(innerArgs, expected) {
		t.Fatalf("innerArgs = %v, want %v", innerArgs, expected)
	}
}

// Built-in default profile selects no isolated config dir, so it never invents
// a custom database path; the --standalone rule still applies, after the leaf.
func TestZeroConfigLaunch_OpenCodeStandaloneWithoutProfileDB(t *testing.T) {
	for name, tc := range map[string]struct{ args, want []string }{
		"auth list":   {[]string{"auth", "list"}, []string{"auth", "list", "--standalone"}},
		"debug paths": {[]string{"debug", "paths"}, []string{"debug", "paths"}},
	} {
		t.Run(name, func(t *testing.T) {
			unsetEnvForTest(t, "OPENCODE_DB")
			ctrl := gomock.NewController(t)
			var capturedBinary string
			var capturedArgs, capturedEnv []string
			mockExec := mocks.NewMockExecer(ctrl)
			mockExec.EXPECT().
				Exec(gomock.Any(), gomock.Any(), gomock.Any()).
				DoAndReturn(func(binary string, args, env []string) error {
					capturedBinary, capturedArgs, capturedEnv = binary, args, env
					return nil
				})
			binDir := t.TempDir()
			opencodeBin := filepath.Join(binDir, "opencode")
			if err := os.WriteFile(opencodeBin, []byte("#!/bin/sh\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", binDir)
			l := &Launcher{
				Execer:    mockExec,
				ConfigDir: t.TempDir(),
				LookPath:  mockLookPath(map[string]string{"opencode": opencodeBin}),
			}
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

			if err := l.Launch(t.TempDir(), "opencode", tc.args, false, false, nil, nil); err != nil {
				t.Fatalf("Launch failed: %v", err)
			}
			_, innerArgs := unwrapSandbox(t, capturedBinary, capturedArgs)
			if want := append([]string{opencodeBin}, tc.want...); !slices.Equal(innerArgs, want) {
				t.Errorf("innerArgs = %v, want %v", innerArgs, want)
			}
			if v, ok := envValue(capturedEnv, "OPENCODE_DB"); ok && runtime.GOOS != "linux" {
				t.Errorf("OPENCODE_DB = %q, want it unset on non-Linux", v)
			}
		})
	}
}

func TestZeroConfigLaunch_NoYoloOverridesYolo(t *testing.T) {
	requireClaudeHome(t)
	ctrl := gomock.NewController(t)
	var capturedBinary string
	var capturedArgs []string
	mockExec := mocks.NewMockExecer(ctrl)
	mockExec.EXPECT().
		Exec(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(binary string, args []string, _ []string) error {
			capturedBinary = binary
			capturedArgs = args
			return nil
		})
	binDir := t.TempDir()
	claudeBin := filepath.Join(binDir, "claude")
	if err := os.WriteFile(claudeBin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)
	var stderrBuf bytes.Buffer
	l := &Launcher{
		Execer:    mockExec,
		ConfigDir: t.TempDir(),
		LookPath:  mockLookPath(map[string]string{"claude": claudeBin}),
		Yolo:      true,
		NoYolo:    true,
		Stderr:    &stderrBuf,
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	err := l.Launch(t.TempDir(), "", nil, false, false, nil, nil)
	if err != nil {
		t.Fatalf("Launch failed: %v", err)
	}

	_, innerArgs := unwrapSandbox(t, capturedBinary, capturedArgs)
	for _, a := range innerArgs {
		if a == "--dangerously-skip-permissions" {
			t.Error("--no-yolo should suppress yolo flag")
		}
	}
	if strings.Contains(stderrBuf.String(), "yolo mode enabled") {
		t.Error("--no-yolo should suppress yolo warning")
	}
}

func TestZeroConfigLaunch_YoloWarningShown(t *testing.T) {
	requireClaudeHome(t)
	ctrl := gomock.NewController(t)
	mockExec := mocks.NewMockExecer(ctrl)
	mockExec.EXPECT().Exec(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
	binDir := t.TempDir()
	claudeBin := filepath.Join(binDir, "claude")
	if err := os.WriteFile(claudeBin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)
	var stderrBuf bytes.Buffer
	l := &Launcher{
		Execer:    mockExec,
		ConfigDir: t.TempDir(),
		LookPath:  mockLookPath(map[string]string{"claude": claudeBin}),
		Yolo:      true,
		Stderr:    &stderrBuf,
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	err := l.Launch(t.TempDir(), "", nil, false, false, nil, nil)
	if err != nil {
		t.Fatalf("Launch failed: %v", err)
	}

	if !strings.Contains(stderrBuf.String(), "AUTO-APPROVE") {
		t.Errorf("expected AUTO-APPROVE warning in stderr, got:\n%s", stderrBuf.String())
	}
}

func TestZeroConfigLaunch_YoloUnsupportedAgent(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockExec := mocks.NewMockExecer(ctrl)
	binDir := t.TempDir()
	aiderBin := filepath.Join(binDir, "aider")
	if err := os.WriteFile(aiderBin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)
	l := &Launcher{
		Execer:    mockExec,
		ConfigDir: t.TempDir(),
		LookPath:  mockLookPath(map[string]string{"aider": aiderBin}),
		Yolo:      true,
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	err := l.Launch(t.TempDir(), "", nil, false, false, nil, nil)
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

// Cursor's installer also drops a shorter "agent" symlink; recognising it would
// shadow other tools, so only the full "cursor-agent" name should match.
func TestIsKnownAgent_CursorAgent(t *testing.T) {
	if !IsKnownAgent("cursor-agent") {
		t.Error("expected cursor-agent to be known")
	}
	if IsKnownAgent("agent") {
		t.Error("expected agent alias NOT to be known")
	}
}

func TestScanAgents_FindsCursorAgent(t *testing.T) {
	available := map[string]string{
		"cursor-agent": "/home/user/.local/bin/cursor-agent",
	}
	result := ScanAgents(mockLookPath(available))
	if result.Found["cursor-agent"] != "/home/user/.local/bin/cursor-agent" {
		t.Errorf("expected cursor-agent to be found, got: %v", result.Found)
	}
}

func TestZeroConfigLaunch_NoConfigNoAgents_ErrorListsCursorAgent(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockExec := mocks.NewMockExecer(ctrl)
	t.Setenv("PATH", t.TempDir())
	l := &Launcher{
		Execer:    mockExec,
		ConfigDir: t.TempDir(),
		LookPath:  mockLookPath(map[string]string{}),
	}
	err := l.Launch(t.TempDir(), "", nil, false, false, nil, nil)
	if err == nil {
		t.Fatal("expected error when no agents on PATH, got nil")
	}
	if !strings.Contains(err.Error(), "cursor-agent") {
		t.Errorf("error message should mention cursor-agent, got: %s", err.Error())
	}
}

