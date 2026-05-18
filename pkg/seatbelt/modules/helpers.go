package modules

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/jskswamy/aide/internal/homepath"
	"github.com/jskswamy/aide/pkg/seatbelt"
)

// resolveConfigDirs returns directories for an agent given an env var
// override key and a list of default candidates. When the env var is
// set, only that path is returned (explicit override). Otherwise,
// candidates that exist or are under homeDir are returned.
//
// Env-var values are tilde-expanded against ctx.HomeDir before being
// returned — Seatbelt subpath rules require absolute paths to match
// the syscalls the agent will make. A literal "~/.claude-prod" in a
// rule never matches the absolute "/Users/.../.claude-prod" claude
// actually reads.
//
// Empty env var semantics: ctx.EnvLookup returns ("", true) for KEY=,
// but we treat empty as unset (fall through to defaults). This matches
// the previous resolver behavior where KEY= was treated as unset.
func resolveConfigDirs(ctx *seatbelt.Context, envKey string, candidates []string) []string {
	if envKey != "" {
		if dir, ok := ctx.EnvLookup(envKey); ok && dir != "" {
			return []string{homepath.Expand(dir, ctx.HomeDir)}
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
//
// Like resolveConfigDirs, the override-key value is tilde-expanded against
// ctx.HomeDir before validation so a literal "~/foo" lands as an absolute
// path that Seatbelt subpath rules can actually match.
func resolveConfigDirsAdditive(ctx *seatbelt.Context, overrideKey, xdgCandidate string, defaults []string) []string {
	home := ctx.HomeDir

	if overrideKey != "" {
		if dir, ok := ctx.EnvLookup(overrideKey); ok && dir != "" {
			expanded := homepath.Expand(dir, home)
			if isSafeConfigOverride(home, expanded) {
				return []string{expanded}
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

// configDirRules generates file-read* file-write* Allow rules for
// agent config directories. Each dir gets a subpath rule.
func configDirRules(sectionName string, dirs []string) []seatbelt.Rule {
	if len(dirs) == 0 {
		return nil
	}
	rules := []seatbelt.Rule{
		seatbelt.SectionAllow(sectionName + " config"),
	}
	for _, dir := range dirs {
		rules = append(rules, seatbelt.AllowRule(fmt.Sprintf(
			`(allow file-read* file-write* (subpath %q))`, dir,
		)))
	}
	return rules
}
