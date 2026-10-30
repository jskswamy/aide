package modules

import (
	"os"
	"path/filepath"
	"slices"
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
	".config/gh",
	".azure",
	".kube",
	".docker",
	".netrc",
	".git-credentials",
}

// SensitiveHomeDirs returns the home-relative, slash-separated paths
// that hold credentials or private keys and must never be granted
// through a config-dir override or a tool's source path.
func SensitiveHomeDirs() []string { return slices.Clone(sensitiveHomeDirs) }

// protectedSymlinkHomeDirs are home-relative paths that symlink widening
// inside an agent's config dir must never grant write access to (nor to any
// ancestor that contains them, such as ~/.config or ~/.local/share). They
// include aide's own dirs, Skillshare's dirs, and every harness's default
// config/state dirs.
var protectedSymlinkHomeDirs = []string{
	".config/aide",
	".local/share/aide",
	".local/state/aide",
	".cache/aide",
	".config/skillshare",
	".local/state/skillshare",
	".local/share/skillshare",
	".cache/skillshare",
	".aider",
	".amp",
	".config/amp",
	".claude",
	".config/claude",
	"Library/Application Support/Claude",
	".codex",
	".copilot",
	".config/.copilot",
	".config/copilot",
	".local/state/.copilot",
	".local/state/copilot",
	".cursor",
	".config/cursor",
	".gemini",
	".config/gemini",
	".config/goose",
	".local/share/goose",
	".local/state/goose",
	".config/opencode",
	".local/share/opencode",
	".local/state/opencode",
	".cache/opencode",
	".pi",
}

// profileDirPrefixes are the top-level $HOME directory prefixes used by
// aide's default per-profile config dir naming (~/.<harness>-<profile>).
var profileDirPrefixes = []string{
	".claude-",
	".codex-",
	".copilot-",
	".gemini-",
	".opencode-",
	".pi-",
	".amp-",
	".cursor-",
	".goose-",
	".aider-",
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

// isSafeConfigOverride returns true when dir is STRICTLY under $HOME and
// does not overlap any entry in sensitiveHomeDirs.
//
// "Strictly under" matters: widening a (subpath $HOME) rule would expose
// the entire home directory through the sandbox, which the rest of the
// system already pays attention to avoid (see filesystem guard's narrow
// baseline). The explicit dir != home guard expresses that requirement
// at the call site so future readers don't have to infer it from a
// missing-separator subtlety in the prefix check.
func isSafeConfigOverride(home, dir string) bool {
	if home == "" || !fsutil.IsUnderDir(dir, home) || filepath.Clean(dir) == filepath.Clean(home) {
		return false
	}
	for _, sensitive := range sensitiveHomeDirs {
		sensitiveAbs := filepath.Join(home, filepath.FromSlash(sensitive))
		if fsutil.IsUnderDir(dir, sensitiveAbs) || fsutil.IsUnderDir(sensitiveAbs, dir) {
			return false
		}
	}
	return true
}

// isSafeSymlinkWidening reports whether a resolved symlink target/scope
// reached from within ownDir may be granted write access. In addition to
// isSafeConfigOverride (strictly under $HOME and not overlapping any
// sensitive dir in either direction), it refuses:
//   - any path overlapping protectedSymlinkHomeDirs (aide's dirs, Skillshare's
//     dirs, or any harness's default config/state dir) unless covered by ownDir
//   - any path under another profile's ~/.<harness>-<profile> dir
func isSafeSymlinkWidening(home, ownDir, candidate string) bool {
	if !isSafeConfigOverride(home, candidate) {
		return false
	}
	ownClean := filepath.Clean(ownDir)
	ownResolved := fsutil.ResolveOrSelf(ownClean)
	isOwn := func(p string) bool {
		return (ownClean != "" && fsutil.IsUnderDir(p, ownClean)) ||
			(ownResolved != "" && fsutil.IsUnderDir(p, ownResolved))
	}
	if isOwn(candidate) {
		return true
	}
	for _, rel := range protectedSymlinkHomeDirs {
		protAbs := filepath.Join(home, filepath.FromSlash(rel))
		if isOwn(protAbs) {
			continue
		}
		if fsutil.IsUnderDir(candidate, protAbs) || fsutil.IsUnderDir(protAbs, candidate) {
			return false
		}
	}
	rel, err := filepath.Rel(filepath.Clean(home), filepath.Clean(candidate))
	if err != nil {
		return false
	}
	top := strings.SplitN(rel, string(filepath.Separator), 2)[0]
	for _, prefix := range profileDirPrefixes {
		if strings.HasPrefix(top, prefix) && len(top) > len(prefix) {
			profDir := filepath.Join(home, top)
			if !isOwn(profDir) {
				return false
			}
		}
	}
	return true
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
		if resolved != dir && isSafeSymlinkWidening(home, dir, resolved) {
			paths.add(resolved)
		}
		for _, target := range collectSymlinkTargetParents(home, resolved) {
			paths.add(target)
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

// expandConfigDirWritable BFS-walks `dir` and every symlink target reached
// from it, returning the resolved paths that must be added to the Landlock
// writable allow-list. Landlock evaluates rules on the kernel-resolved
// path, so a dotfiles symlink chain (~/.cursor/skills -> ~/.claude/skills,
// ~/.claude/skills/dd-apm -> ~/.agents/skills/dd-apm) needs every hop's
// target in the rule set or the agent gets EACCES at the resolved inode.
//
// Scope per symlink:
//   - target is a directory → grant the target itself (tight)
//   - target is a regular file → grant the parent (atomic-rename tmp+rename
//     siblings need parent write, per the macOS-side rationale in
//     collectSymlinkTargetParents)
//
// Visited-set on resolved paths bounds against loops. Regular subdirs are
// not descended — Landlock's recursive parent grant already covers them.
// isSafeConfigOverride filters every hop, so a malicious symlink to
// ~/.ssh or outside $HOME does not widen the sandbox.
func expandConfigDirWritable(home, dir string) []string {
	added := map[string]bool{dir: true}
	paths := []string{dir}
	visited := map[string]bool{}
	queue := []string{dir}

	addPath := func(p string) {
		if added[p] {
			return
		}
		added[p] = true
		paths = append(paths, p)
	}

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		resolved := fsutil.ResolveOrSelf(current)
		if visited[resolved] {
			continue
		}
		visited[resolved] = true

		if resolved != current && isSafeSymlinkWidening(home, dir, resolved) {
			addPath(resolved)
		}

		entries, err := os.ReadDir(resolved)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.Type()&os.ModeSymlink == 0 {
				continue
			}
			link := filepath.Join(resolved, e.Name())
			target := fsutil.ResolveOrSelf(link)
			if target == link {
				// Broken symlink: ResolveOrSelf falls back to input on
				// EvalSymlinks error. Skip; we don't widen the sandbox to
				// a path that doesn't exist.
				continue
			}
			info, statErr := os.Stat(target)
			if statErr != nil {
				continue
			}
			scope := target
			if !info.IsDir() {
				scope = filepath.Dir(target)
			}
			if !isSafeSymlinkWidening(home, dir, scope) {
				continue
			}
			addPath(scope)
			// Walk into the scope to find further symlinks. Both branches
			// of the dir-vs-file split produce a directory (a file's
			// parent is also a dir), so this is uniformly correct.
			if !visited[scope] {
				queue = append(queue, scope)
			}
		}
	}
	return paths
}

// collectSymlinkTargetParents walks dir at depth 1 and returns the safe
// target paths for each top-level symlink entry:
//   - when the resolved target is a directory, the target directory itself
//     (tight scope, matching expandConfigDirWritable on Linux)
//   - when the resolved target is a regular file, the target's parent
//     directory so atomic-write tmp+rename siblings in a dotfiles repo work
//     (the pattern that triggered #12)
//
// Missing/unreadable dirs and broken symlinks yield no paths. Targets
// outside $HOME or overlapping sensitive/protected/profile dirs are
// filtered by isSafeSymlinkWidening.
func collectSymlinkTargetParents(home, dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var targets []string
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
		info, statErr := os.Stat(target)
		if statErr != nil {
			continue
		}
		scope := target
		if !info.IsDir() {
			scope = filepath.Dir(target)
		}
		if !isSafeSymlinkWidening(home, dir, scope) {
			continue
		}
		targets = append(targets, scope)
	}
	return targets
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
		if fsutil.IsUnderDir(p, existing) {
			return
		}
	}
	s.seen[p] = true
	s.entries = append(s.entries, p)
}

func (s *prefixClosedSet) values() []string { return s.entries }
