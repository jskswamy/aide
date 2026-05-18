//go:build linux

package modules

import (
	"path/filepath"

	"github.com/jskswamy/aide/pkg/seatbelt"
)

var (
	_ seatbelt.LinuxPathProvider        = (*claudeAgentModule)(nil)
	_ seatbelt.LinuxAtomicWriteProvider = (*claudeAgentModule)(nil)
)

// A path must appear in exactly one of LinuxReadable / LinuxWritable. Listing
// it in both causes the bwrap backend to emit --bind followed by --ro-bind,
// leaving the path read-only inside the sandbox (EROFS on descendant writes).

func (m *claudeAgentModule) LinuxReadable(ctx *seatbelt.Context) []string {
	home := ctx.HomeDir
	return []string{
		filepath.Join(home, ".claude.json"),
		filepath.Join(home, ".mcp.json"),
	}
}

func (m *claudeAgentModule) LinuxWritable(ctx *seatbelt.Context) []string {
	home := ctx.HomeDir
	configDirs := resolveConfigDirs(ctx, "CLAUDE_CONFIG_DIR", []string{
		filepath.Join(home, ".claude"),
		filepath.Join(home, ".config", "claude"),
	})
	paths := make([]string, 0, len(configDirs)+3)
	paths = append(paths, configDirs...)
	paths = append(paths,
		filepath.Join(home, ".cache", "claude"),
		filepath.Join(home, ".local", "state", "claude"),
		filepath.Join(home, ".local", "share", "claude"),
	)
	return paths
}

// LinuxAtomicWritableFiles declares files Claude rewrites with an open-tmp +
// rename pattern. The Linux backend overlays each file's parent with a tmpfs
// and bind-mounts only the listed files, so $HOME is not made broadly writable.
func (m *claudeAgentModule) LinuxAtomicWritableFiles(ctx *seatbelt.Context) []string {
	if ctx == nil || ctx.HomeDir == "" {
		return nil
	}
	return []string{
		filepath.Join(ctx.HomeDir, ".claude.json"),
	}
}
