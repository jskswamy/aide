package modules

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/testutil"
	"github.com/jskswamy/aide/pkg/seatbelt"
)

func TestCursorAgent_Identity(t *testing.T) {
	mod := CursorAgent()
	if mod == nil {
		t.Fatal("CursorAgent() returned nil")
	}
	if got := mod.Name(); got != "Cursor Agent" {
		t.Errorf("Name() = %q, want %q", got, "Cursor Agent")
	}
}

// cursorAgentWithInstall stubs the install-dir resolver so install-dir branches
// run in CI without cursor-agent on PATH.
func cursorAgentWithInstall(activeVerDir, logsDir string) *cursorAgentModule {
	return &cursorAgentModule{
		resolveInstallDirs: func(_ string) (string, string, bool) {
			return activeVerDir, logsDir, true
		},
	}
}

func TestCursorAgent_Rules_InstallDirUsesResolvedLogsDir(t *testing.T) {
	activeVerDir := "/opt/cursor-agent/1.2.3"
	logsDir := "/opt/cursor-agent/logs"
	mod := cursorAgentWithInstall(activeVerDir, logsDir)
	ctx := &seatbelt.Context{HomeDir: "/home/user"}

	result := mod.Rules(ctx)

	found := false
	for _, r := range result.Rules {
		text := r.String()
		if strings.Contains(text, "opt/cursor-agent/logs") {
			found = true
		}
		if strings.Contains(text, ".local/share/cursor-agent/logs") {
			t.Errorf("Rules() must not contain hardcoded default logs path; got rule: %q", text)
		}
	}
	if !found {
		t.Errorf("Rules() must contain the resolved logsDir %q; rules: %v", logsDir, result.Rules)
	}
}

func TestCursorAgent_Rules_IncludesInstallDirs(t *testing.T) {
	activeVerDir := "/home/user/.local/share/cursor-agent/versions/1.2.3"
	logsDir := "/home/user/.local/share/cursor-agent/logs"
	mod := cursorAgentWithInstall(activeVerDir, logsDir)
	ctx := &seatbelt.Context{HomeDir: "/home/user"}

	result := mod.Rules(ctx)
	got := rulesToString(result.Rules)

	if !strings.Contains(got, activeVerDir) {
		t.Errorf("Rules() must contain activeVerDir %q; got:\n%s", activeVerDir, got)
	}
	if !strings.Contains(got, logsDir) {
		t.Errorf("Rules() must contain logsDir %q; got:\n%s", logsDir, got)
	}
}

// TestCursorAgent_Rules_ResolvesSymlinkedLogsDir pins the symlink-resolution
// contract for cursor's install dirs: macOS seatbelt fires file-write* policy
// on the kernel-resolved path, so a literal subpath rule for a symlinked
// logs dir wouldn't cover the actual write target. resolveInstallDirs
// already resolves the binary's parents, but if the logs dir itself
// is a symlink (rare but possible — user redirects logs to an external
// volume), the rule must reference the resolved target.
func TestCursorAgent_Rules_ResolvesSymlinkedLogsDir(t *testing.T) {
	root := testutil.CanonicalTempDir(t)
	realLogs := filepath.Join(root, "real-logs")
	linkedLogs := filepath.Join(root, "logs-link")
	if err := os.MkdirAll(realLogs, 0o755); err != nil {
		t.Fatalf("mkdir real-logs: %v", err)
	}
	if err := os.Symlink(realLogs, linkedLogs); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	activeVerDir := filepath.Join(root, "versions", "1.2.3")
	if err := os.MkdirAll(activeVerDir, 0o755); err != nil {
		t.Fatalf("mkdir activeVer: %v", err)
	}
	mod := cursorAgentWithInstall(activeVerDir, linkedLogs)
	ctx := &seatbelt.Context{HomeDir: "/home/user"}

	got := rulesToString(mod.Rules(ctx).Rules)

	if !strings.Contains(got, realLogs) {
		t.Errorf("Rules() must include resolved logs path %q; got:\n%s", realLogs, got)
	}
}

// CURSOR_CONFIG_DIR pointing to $HOME/.ssh must not produce any rule for ~/.ssh.
// This pins the never_allow contract: env-injection must not bypass credential guards.
func TestCursorAgent_Rules_RejectsCursorConfigDirOutsideHome(t *testing.T) {
	mod := CursorAgent()
	ctx := &seatbelt.Context{
		HomeDir: "/home/user",
		Env:     []string{"CURSOR_CONFIG_DIR=/home/user/.ssh"},
	}

	result := mod.Rules(ctx)
	got := rulesToString(result.Rules)

	if strings.Contains(got, ".ssh") {
		t.Errorf("Rules() must not emit any rule for ~/.ssh when CURSOR_CONFIG_DIR=$HOME/.ssh; got:\n%s", got)
	}
	// Default config dirs must still be present.
	if !strings.Contains(got, "/home/user/.cursor") {
		t.Errorf("Rules() must fall back to default config dirs; got:\n%s", got)
	}
}

// CURSOR_CONFIG_DIR pointing to a safe path under $HOME must be accepted.
func TestCursorAgent_Rules_AcceptsCursorConfigDirUnderHome(t *testing.T) {
	mod := CursorAgent()
	ctx := &seatbelt.Context{
		HomeDir: "/home/user",
		Env:     []string{"CURSOR_CONFIG_DIR=/home/user/my-cursor-config"},
	}

	result := mod.Rules(ctx)
	got := rulesToString(result.Rules)

	if !strings.Contains(got, "/home/user/my-cursor-config") {
		t.Errorf("Rules() must accept safe CURSOR_CONFIG_DIR; got:\n%s", got)
	}
	if strings.Contains(got, "/home/user/.cursor\"") {
		t.Errorf("Rules() must not include default ~/.cursor when safe override is set; got:\n%s", got)
	}
}

// TestDeriveCursorInstallDirs verifies the Linux/macOS install layout and
// trusted-prefix rejection:
//
//	~/.local/share/cursor-agent/versions/<ver>/cursor-agent  (binary)
//	~/.local/share/cursor-agent/logs                         (logs sibling)
//
// The Cursor desktop IDE bundle (/Applications/Cursor.app) does NOT ship the
// standalone CLI, so it is intentionally rejected.
func TestDeriveCursorInstallDirs(t *testing.T) {
	home := "/home/user"
	macHome := "/Users/jane"
	cases := []struct {
		name             string
		home             string
		resolvedBinary   string
		wantOK           bool
		wantActiveVerDir string
		wantLogsDir      string
	}{
		{
			name:             "Linux user install",
			home:             home,
			resolvedBinary:   "/home/user/.local/share/cursor-agent/versions/2026.05.09-abc/cursor-agent",
			wantOK:           true,
			wantActiveVerDir: "/home/user/.local/share/cursor-agent/versions/2026.05.09-abc",
			wantLogsDir:      "/home/user/.local/share/cursor-agent/logs",
		},
		{
			name:             "macOS user install (same layout as Linux)",
			home:             macHome,
			resolvedBinary:   "/Users/jane/.local/share/cursor-agent/versions/2026.05.09-abc/cursor-agent",
			wantOK:           true,
			wantActiveVerDir: "/Users/jane/.local/share/cursor-agent/versions/2026.05.09-abc",
			wantLogsDir:      "/Users/jane/.local/share/cursor-agent/logs",
		},
		{
			name:           "macOS Cursor.app bundle is not the standalone CLI; rejected",
			home:           macHome,
			resolvedBinary: "/Applications/Cursor.app/Contents/MacOS/cursor-agent",
			wantOK:         false,
		},
		{
			name:           "untrusted /tmp path rejected",
			home:           home,
			resolvedBinary: "/tmp/evil/cursor-agent",
			wantOK:         false,
		},
		{
			name:           "look-alike directory cursor-agent-evil rejected",
			home:           home,
			resolvedBinary: "/home/user/.local/share/cursor-agent-evil/versions/1.0/cursor-agent",
			wantOK:         false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			activeVerDir, logsDir, ok := deriveCursorInstallDirs(tc.resolvedBinary, tc.home)

			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v (resolved=%q)", ok, tc.wantOK, tc.resolvedBinary)
			}
			if !tc.wantOK {
				return
			}
			if activeVerDir != tc.wantActiveVerDir {
				t.Errorf("activeVerDir = %q, want %q", activeVerDir, tc.wantActiveVerDir)
			}
			if logsDir != tc.wantLogsDir {
				t.Errorf("logsDir = %q, want %q", logsDir, tc.wantLogsDir)
			}
		})
	}
}

// isTrustedInstallDir accepts only the cross-platform Cursor CLI install
// location. The Cursor desktop IDE bundle is intentionally rejected because
// it does not ship the standalone CLI.
func TestIsTrustedInstallDir_AcceptsTrustedPrefixes(t *testing.T) {
	home := "/home/user"
	cases := []struct {
		dir     string
		trusted bool
	}{
		{"/home/user/.local/share/cursor-agent/versions/1.2.3", true},
		{"/Applications/Cursor.app/Contents/MacOS", false},
		{"/home/user/Applications/Cursor.app/Contents/MacOS", false},
		{"/home/user/.ssh", false},
		{"/tmp/cursor-agent", false},
		{"/home/user/.local/share/cursor-agent-evil/versions/1.0", false},
	}
	for _, tc := range cases {
		if got := isTrustedInstallDir(tc.dir, home); got != tc.trusted {
			t.Errorf("isTrustedInstallDir(%q, %q) = %v, want %v", tc.dir, home, got, tc.trusted)
		}
	}
}

func TestCursorAgent_NilContext(_ *testing.T) {
	mod := CursorAgent()
	_ = mod.Rules(nil)
	_ = mod.Rules(&seatbelt.Context{})
}
