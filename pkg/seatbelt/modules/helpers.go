package modules

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/jskswamy/aide/internal/fsutil"
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

// configDirRules generates file-read* file-write* Allow rules for agent
// config directories. Each canonical dir gets a subpath rule.
//
// macOS seatbelt fires file-write* policy on the kernel-resolved path,
// not the literal syscall argument. So when a dotfiles tool (home-manager,
// stow, chezmoi-in-symlink-mode, dotbot) symlinks either the whole config
// dir or individual files inside it into a git repo, the canonical
// (subpath ...) rule alone does not cover writes — the resolved target is
// outside the rule's scope and the kernel denies the write.
//
// To cover both patterns, this function also:
//
//  1. Resolves each canonical dir via filepath.EvalSymlinks. If the
//     resolved path differs and passes isSafeConfigOverride (under $HOME,
//     not under sensitiveHomeDirs), emit an additional subpath rule for
//     it. This covers whole-dir symlinks.
//
//  2. Walks the dir at depth 1 (after resolution). For each entry that is
//     itself a symlink, take the resolved target's parent directory and
//     allow-list it if safe. Parent-dir scope (not file literal) is
//     required so atomic-write tmp+rename siblings under the dotfiles
//     repo also work — the very pattern that triggered issue #12.
//
// Targets outside $HOME are deliberately NOT widened. Dotfiles repos at
// ~/Code/dotfiles, ~/dotfiles, etc. work; /Volumes or /opt placements
// don't. If a user reports breakage we revisit the safety gate.
//
// Empirical verification of the kernel-resolved-path behavior is stored
// in bd memory macos-seatbelt-file-write-rules-match-the-kernel.
func configDirRules(sectionName, home string, dirs []string) []seatbelt.Rule {
	if len(dirs) == 0 {
		return nil
	}

	paths := newPrefixClosedSet()
	for _, dir := range dirs {
		paths.add(dir)
		resolved := fsutil.ResolveOrSelf(dir)
		if resolved != dir && isSafeConfigOverride(home, resolved) {
			paths.add(resolved)
		}
		for _, parent := range collectSymlinkTargetParents(home, resolved) {
			paths.add(parent)
		}
	}

	rules := []seatbelt.Rule{
		seatbelt.SectionAllow(sectionName + " config"),
	}
	for _, p := range paths.values() {
		rules = append(rules, seatbelt.AllowSubpath(p, "file-read*", "file-write*"))
	}
	return rules
}

// collectSymlinkTargetParents walks dir at depth 1 and returns the safe
// parent-directory paths for each top-level symlink entry. The parent
// (not the file literal) is used so atomic-write tmp+rename siblings in
// the dotfiles repo also work — the exact pattern that triggered #12.
//
// Missing/unreadable dirs and broken symlinks yield no parents. Parents
// outside $HOME or under sensitiveHomeDirs are filtered by
// isSafeConfigOverride.
func collectSymlinkTargetParents(home, dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var parents []string
	for _, e := range entries {
		if e.Type()&os.ModeSymlink == 0 {
			continue
		}
		link := filepath.Join(dir, e.Name())
		target := fsutil.ResolveOrSelf(link)
		if target == link {
			// Broken symlink — ResolveOrSelf falls back to the input on
			// EvalSymlinks error. Skip; we don't widen the sandbox to a
			// path that doesn't exist.
			continue
		}
		parent := filepath.Dir(target)
		if !isSafeConfigOverride(home, parent) {
			continue
		}
		parents = append(parents, parent)
	}
	return parents
}

// prefixClosedSet collects path strings while suppressing entries whose
// (subpath ...) coverage is already implied by an earlier entry. Order
// of insertion is preserved on iteration so emitted rules remain
// deterministic.
type prefixClosedSet struct {
	entries []string
	seen    map[string]bool
}

func newPrefixClosedSet() *prefixClosedSet {
	return &prefixClosedSet{seen: map[string]bool{}}
}

func (s *prefixClosedSet) add(p string) {
	if s.seen[p] {
		return
	}
	for _, existing := range s.entries {
		if p == existing || strings.HasPrefix(p, existing+string(filepath.Separator)) {
			return
		}
	}
	s.seen[p] = true
	s.entries = append(s.entries, p)
}

func (s *prefixClosedSet) values() []string { return s.entries }
