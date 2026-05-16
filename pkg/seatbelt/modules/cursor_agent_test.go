package modules

import (
	"strings"
	"testing"

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

// TestDeriveCursorInstallDirs verifies the Linux/macOS install layout:
//
//	~/.local/share/cursor-agent/versions/<ver>/cursor-agent  (binary)
//	~/.local/share/cursor-agent/logs                         (logs sibling)
func TestDeriveCursorInstallDirs(t *testing.T) {
	home := "/home/user"
	cases := []struct {
		name             string
		resolvedBinary   string
		wantActiveVerDir string
		wantLogsDir      string
	}{
		{
			name:             "Linux user install",
			resolvedBinary:   "/home/user/.local/share/cursor-agent/versions/2026.05.09-abc/cursor-agent",
			wantActiveVerDir: "/home/user/.local/share/cursor-agent/versions/2026.05.09-abc",
			wantLogsDir:      "/home/user/.local/share/cursor-agent/logs",
		},
		{
			name:             "macOS user install (same layout as Linux)",
			resolvedBinary:   "/Users/jane/.local/share/cursor-agent/versions/2026.05.09-abc/cursor-agent",
			wantActiveVerDir: "/Users/jane/.local/share/cursor-agent/versions/2026.05.09-abc",
			wantLogsDir:      "/Users/jane/.local/share/cursor-agent/logs",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			activeVerDir, logsDir, ok := deriveCursorInstallDirs(tc.resolvedBinary, home)
			if !ok {
				t.Fatalf("ok = false (resolved=%q)", tc.resolvedBinary)
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

func TestCursorAgent_NilContext(_ *testing.T) {
	mod := CursorAgent()
	_ = mod.Rules(nil)
	_ = mod.Rules(&seatbelt.Context{})
}
