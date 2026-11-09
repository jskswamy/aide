package launcher

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/jskswamy/aide/internal/fsutil"
	"github.com/jskswamy/aide/internal/homepath"
	"github.com/jskswamy/aide/internal/sandbox"
	"github.com/jskswamy/aide/pkg/seatbelt/modules"
)

// ompProfileConfigRoot derives the home-relative PI_CONFIG_DIR value from
// PI_CODING_AGENT_DIR in env for an OMP launch whose profile selects an
// isolated config dir. Setting PI_CONFIG_DIR alongside PI_CODING_AGENT_DIR
// relocates OMP's root-level state (.env, auth-broker.token, install-id,
// plugins, logs, and daemon socket) into the profile directory as well.
// When PI_CODING_AGENT_DIR points at the global default (~/.omp/agent),
// "" is returned so PI_CONFIG_DIR stays at its default (.omp).
func ompProfileConfigRoot(env []string, home string, blocked []string) (string, error) {
	if slices.Contains(blocked, "PI_CONFIG_DIR") {
		return "", errors.New("omp profile config root is blocked by never_allow_env")
	}
	var dir string
	for _, e := range env {
		if val, ok := strings.CutPrefix(e, "PI_CODING_AGENT_DIR="); ok && val != "" {
			dir = val
		}
	}
	if !filepath.IsAbs(dir) {
		return "", fmt.Errorf("omp profile config dir is not absolute: %s", dir)
	}
	cleanHome := filepath.Clean(home)
	cleanDir := filepath.Clean(dir)
	if cleanDir == filepath.Join(cleanHome, ".omp", "agent") {
		return "", nil
	}
	rel, err := filepath.Rel(cleanHome, cleanDir)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("omp profile config dir is outside home: %s", dir)
	}
	return filepath.ToSlash(rel), nil
}

// prepareOMPSandboxDirs ensures the directories OmpAgent grants on Linux
// exist before Landlock or bubblewrap applies. Landlock binds rules to
// existing directory inodes (skipping missing paths while $HOME stays
// non-writable), and bubblewrap's --bind-try + --remount-ro / has the same
// constraint, so a brand-new profile directory or ~/.omp/natives addon
// cache would otherwise fail with EACCES/EROFS on first launch.
func prepareOMPSandboxDirs(policy *sandbox.Policy, goos string) error {
	if goos != "linux" || policy == nil || policy.HomeDir == "" || !filepath.IsAbs(policy.HomeDir) {
		return nil
	}
	home := filepath.Clean(policy.HomeDir)
	var override string
	for _, e := range policy.Env {
		if val, ok := strings.CutPrefix(e, "PI_CODING_AGENT_DIR="); ok && val != "" {
			override = homepath.Expand(val, home)
		}
	}
	var dirs []string
	if override != "" {
		if isSafeOMPSandboxDir(home, override) {
			dirs = append(dirs, override)
		}
		for _, sub := range []string{
			filepath.Join(home, ".omp", "natives"),
			filepath.Join(home, ".omp", "logs"),
			filepath.Join(home, ".omp", "run"),
		} {
			if isSafeOMPSandboxDir(home, sub) {
				dirs = append(dirs, sub)
			}
		}
	} else {
		for _, sub := range []string{
			filepath.Join(home, ".omp", "agent"),
			filepath.Join(home, ".omp", "natives"),
		} {
			if isSafeOMPSandboxDir(home, sub) {
				dirs = append(dirs, sub)
			}
		}
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return fmt.Errorf("creating OMP sandbox directory %q: %w", d, err)
		}
	}
	return nil
}

func isSafeOMPSandboxDir(home, dir string) bool {
	if home == "" || dir == "" || !filepath.IsAbs(home) || !filepath.IsAbs(dir) {
		return false
	}
	if fsutil.CheckSymlinkCycle(dir) != nil {
		return false
	}
	resolvedHome := fsutil.ResolveNearest(home)
	resolvedDir := fsutil.ResolveNearest(dir)
	if !fsutil.IsUnderDir(resolvedDir, resolvedHome) || filepath.Clean(resolvedDir) == filepath.Clean(resolvedHome) {
		return false
	}
	for _, rel := range modules.SensitiveHomeDirs() {
		sens := fsutil.ResolveNearest(filepath.Join(home, filepath.FromSlash(rel)))
		if fsutil.IsUnderDir(resolvedDir, sens) || fsutil.IsUnderDir(sens, resolvedDir) {
			return false
		}
	}
	return true
}
