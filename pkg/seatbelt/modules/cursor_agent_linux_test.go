//go:build linux

package modules

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/jskswamy/aide/pkg/seatbelt"
)

func TestAugmentCursorLinuxPaths_NilContext(t *testing.T) {
	result := &seatbelt.GuardResult{}

	augmentCursorLinuxPaths(nil, []string{"/home/user/.config/cursor"}, "/logs", "/ver", result)
	augmentCursorLinuxPaths(&seatbelt.Context{}, []string{"/home/user/.config/cursor"}, "/logs", "/ver", result)

	if len(result.Writable) != 0 || len(result.Allowed) != 0 {
		t.Errorf("augmentCursorLinuxPaths with nil/empty context must not append paths; Writable=%v Allowed=%v",
			result.Writable, result.Allowed)
	}
}

func TestAugmentCursorLinuxPaths_ConfigDirsAreWritable(t *testing.T) {
	ctx := &seatbelt.Context{HomeDir: "/home/user"}
	configDirs := []string{"/home/user/.config/cursor", "/home/user/.cursor"}
	result := &seatbelt.GuardResult{}

	augmentCursorLinuxPaths(ctx, configDirs, "", "", result)

	if len(result.Writable) != 2 {
		t.Fatalf("Writable = %v, want 2 entries", result.Writable)
	}
	for _, dir := range configDirs {
		found := false
		for _, w := range result.Writable {
			if w == dir {
				found = true
			}
		}
		if !found {
			t.Errorf("Writable must contain %q; got %v", dir, result.Writable)
		}
	}
}

func TestAugmentCursorLinuxPaths_LogsDirIsWritable(t *testing.T) {
	ctx := &seatbelt.Context{HomeDir: "/home/user"}
	logsDir := "/home/user/.local/share/cursor-agent/logs"
	result := &seatbelt.GuardResult{}

	augmentCursorLinuxPaths(ctx, nil, logsDir, "", result)

	found := false
	for _, w := range result.Writable {
		if w == logsDir {
			found = true
		}
	}
	if !found {
		t.Errorf("Writable must contain logsDir %q; got %v", logsDir, result.Writable)
	}
}

func TestAugmentCursorLinuxPaths_ActiveVerDirIsAllowed(t *testing.T) {
	ctx := &seatbelt.Context{HomeDir: "/home/user"}
	activeVerDir := "/home/user/.local/share/cursor-agent/versions/1.2.3"
	result := &seatbelt.GuardResult{}

	augmentCursorLinuxPaths(ctx, nil, "", activeVerDir, result)

	found := false
	for _, a := range result.Allowed {
		if a == activeVerDir {
			found = true
		}
	}
	if !found {
		t.Errorf("Allowed must contain activeVerDir %q; got %v", activeVerDir, result.Allowed)
	}
	// Verify activeVerDir is not added to Writable
	for _, w := range result.Writable {
		if w == activeVerDir {
			t.Errorf("activeVerDir must not be Writable; found in Writable: %v", result.Writable)
		}
	}
}

func TestAugmentCursorLinuxPaths_EmptyLogsDirSkipped(t *testing.T) {
	ctx := &seatbelt.Context{HomeDir: "/home/user"}
	result := &seatbelt.GuardResult{}

	augmentCursorLinuxPaths(ctx, nil, "", "", result)

	if len(result.Writable) != 0 || len(result.Allowed) != 0 {
		t.Errorf("empty dirs must not append anything; Writable=%v Allowed=%v",
			result.Writable, result.Allowed)
	}
}

// TestCursorAgentRules_Linux_WritableIncludesConfigDirs verifies the integration:
// Rules() on Linux populates Writable with the cursor config dirs so that
// Landlock (DeriveGrantedPathSet) grants write access to auth.json and state.
func TestCursorAgentRules_Linux_WritableIncludesConfigDirs(t *testing.T) {
	activeVerDir := "/home/user/.local/share/cursor-agent/versions/1.2.3"
	logsDir := "/home/user/.local/share/cursor-agent/logs"
	mod := cursorAgentWithInstall(activeVerDir, logsDir)
	ctx := &seatbelt.Context{HomeDir: "/home/user"}

	result := mod.Rules(ctx)

	wantWritable := []string{"/home/user/.cursor", logsDir}
	for _, want := range wantWritable {
		found := false
		for _, w := range result.Writable {
			if w == want {
				found = true
			}
		}
		if !found {
			t.Errorf("Writable must contain %q; got %v", want, result.Writable)
		}
	}
	// ~/.config/cursor (the XDG default) must also be writable — this is where auth.json lives
	xdgCursorDir := "/home/user/.config/cursor"
	foundXDG := false
	for _, w := range result.Writable {
		if w == xdgCursorDir {
			foundXDG = true
		}
	}
	if !foundXDG {
		t.Errorf("Writable must contain XDG cursor dir %q (auth.json lives here); got %v",
			xdgCursorDir, result.Writable)
	}

	// activeVerDir must be in Allowed (read-only), not Writable
	foundInAllowed := false
	for _, a := range result.Allowed {
		if a == activeVerDir {
			foundInAllowed = true
		}
	}
	if !foundInAllowed {
		t.Errorf("Allowed must contain activeVerDir %q; got %v", activeVerDir, result.Allowed)
	}
	for _, w := range result.Writable {
		if w == activeVerDir {
			t.Errorf("activeVerDir must not be in Writable; got %v", result.Writable)
		}
	}
}

// TestAugmentCursorLinuxPaths_FollowsSkillsSymlink uses real symlinks to pin
// the dotfiles workflow: when a user has symlinked ~/.cursor/skills to a
// dotfiles repo elsewhere under $HOME, the resolved target must end up in
// the Landlock writable allow-list. Without this, the kernel denies access
// at the resolved inode and skills/commands fail to load.
func TestAugmentCursorLinuxPaths_FollowsSkillsSymlink(t *testing.T) {
	home := t.TempDir()
	cursorDir := filepath.Join(home, ".cursor")
	if err := os.MkdirAll(cursorDir, 0o700); err != nil {
		t.Fatalf("setup ~/.cursor: %v", err)
	}
	skillsTarget := filepath.Join(home, "dotfiles", "cursor-skills")
	if err := os.MkdirAll(skillsTarget, 0o700); err != nil {
		t.Fatalf("setup skills target: %v", err)
	}
	if err := os.Symlink(skillsTarget, filepath.Join(cursorDir, "skills")); err != nil {
		t.Fatalf("setup symlink: %v", err)
	}

	ctx := &seatbelt.Context{HomeDir: home}
	result := &seatbelt.GuardResult{}
	augmentCursorLinuxPaths(ctx, []string{cursorDir}, "", "", result)

	if !slices.Contains(result.Writable, cursorDir) {
		t.Errorf("Writable must contain %q; got %v", cursorDir, result.Writable)
	}
	if !slices.Contains(result.Writable, skillsTarget) {
		t.Errorf("Writable must contain symlink target %q so Landlock allows skill reads/writes; got %v",
			skillsTarget, result.Writable)
	}
}

// TestCursorAgentRules_Linux_NoInstall verifies behaviour when cursor-agent
// is not installed (resolveInstallDirs returns ok=false): config dirs are
// still writable; no logsDir or activeVerDir are added.
func TestCursorAgentRules_Linux_NoInstall(t *testing.T) {
	mod := &cursorAgentModule{
		resolveInstallDirs: func(_ string) (string, string, bool) {
			return "", "", false
		},
	}
	ctx := &seatbelt.Context{HomeDir: "/home/user"}

	result := mod.Rules(ctx)

	if len(result.Writable) == 0 {
		t.Errorf("Writable must not be empty even when cursor-agent is absent; got %v", result.Writable)
	}
	xdgCursorDir := "/home/user/.config/cursor"
	found := false
	for _, w := range result.Writable {
		if w == xdgCursorDir {
			found = true
		}
	}
	if !found {
		t.Errorf("Writable must contain XDG cursor dir %q even without cursor-agent; got %v",
			xdgCursorDir, result.Writable)
	}
}
