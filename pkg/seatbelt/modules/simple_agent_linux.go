//go:build linux

package modules

import "github.com/jskswamy/aide/pkg/seatbelt"

// augmentSimpleAgentLinuxPaths grants a SimpleAgent's config dirs as Landlock
// writable paths. Landlock never reads GuardResult.Rules (Seatbelt DSL), so
// without this every SimpleAgent-based agent runs on Linux with no write
// access to its own config. Each dir goes through expandConfigDirWritable so
// symlinked dotfiles resolve to inodes Landlock will actually grant.
func augmentSimpleAgentLinuxPaths(ctx *seatbelt.Context, dirs []string, result *seatbelt.GuardResult) {
	if ctx == nil || ctx.HomeDir == "" {
		return
	}
	for _, dir := range dirs {
		result.Writable = append(result.Writable, expandConfigDirWritable(ctx.HomeDir, dir)...)
	}
}
