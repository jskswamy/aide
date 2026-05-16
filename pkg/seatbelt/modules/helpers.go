package modules

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/jskswamy/aide/pkg/seatbelt"
)

// resolveConfigDirs returns config directories for an agent.
// When envKey is set to a non-empty value, only that path is returned.
// Otherwise, candidates that exist on disk or are under homeDir are returned.
func resolveConfigDirs(ctx *seatbelt.Context, envKey string, candidates []string) []string {
	if envKey != "" {
		if dir, ok := ctx.EnvLookup(envKey); ok && dir != "" {
			return []string{dir}
		}
	}
	var dirs []string
	for _, p := range candidates {
		if seatbelt.ExistsOrUnderHome(ctx.HomeDir, p) {
			dirs = append(dirs, p)
		}
	}
	return dirs
}

// sensitiveHomeDirs are home-relative paths that must never be accepted as a
// config-dir override; they hold credentials or private keys.
var sensitiveHomeDirs = []string{
	".ssh",
	".aws",
	".gnupg",
	".gpg",
	".config/gcloud",
	".azure",
	".kube",
	".docker",
	".netrc",
	".git-credentials",
}

// resolveConfigDirsAdditive is like resolveConfigDirs but xdgCandidate is
// always appended to the result (augmenting rather than replacing defaults).
// Both the overrideKey value and xdgCandidate are validated with
// isSafeConfigOverride before use; unsafe values are silently dropped so that
// XDG_CONFIG_HOME=$HOME/.ssh cannot inject a writable rule for ~/.ssh/cursor.
func resolveConfigDirsAdditive(ctx *seatbelt.Context, overrideKey, xdgCandidate string, defaults []string) []string {
	home := ctx.HomeDir

	if overrideKey != "" {
		if dir, ok := ctx.EnvLookup(overrideKey); ok && dir != "" {
			if isSafeConfigOverride(home, dir) {
				return []string{dir}
			}
		}
	}

	dirs := make([]string, len(defaults))
	copy(dirs, defaults)
	if isSafeConfigOverride(home, xdgCandidate) {
		dirs = append(dirs, xdgCandidate)
	}
	return dirs
}

// isSafeConfigOverride returns true when dir is under $HOME and does not
// overlap any entry in sensitiveHomeDirs.
func isSafeConfigOverride(home, dir string) bool {
	if !isUnderHome(home, dir) {
		return false
	}
	for _, sensitive := range sensitiveHomeDirs {
		sensitiveAbs := filepath.Join(home, sensitive)
		if dir == sensitiveAbs || strings.HasPrefix(dir, sensitiveAbs+string(filepath.Separator)) {
			return false
		}
	}
	return true
}

// isUnderHome uses a separator-aware prefix check to avoid false positives
// such as /home/user-other matching /home/user.
func isUnderHome(home, path string) bool {
	return strings.HasPrefix(path, home+string(filepath.Separator))
}

// configDirRules generates file-read* file-write* Allow rules for each dir.
func configDirRules(sectionName string, dirs []string) []seatbelt.Rule {
	if len(dirs) == 0 {
		return nil
	}
	rules := []seatbelt.Rule{seatbelt.SectionAllow(sectionName + " config")}
	for _, dir := range dirs {
		rules = append(rules, seatbelt.AllowRule(fmt.Sprintf(
			`(allow file-read* file-write* (subpath %q))`, dir,
		)))
	}
	return rules
}
