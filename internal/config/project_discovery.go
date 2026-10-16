package config

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ErrMainCheckoutUnavailable reports a linked worktree whose main
// checkout cannot be read, so the policy it may pass on is unknown.
var ErrMainCheckoutUnavailable = errors.New("main checkout unavailable")

// ProjectDiscovery is where .aide.yaml files apply for a directory. It
// is the single discovery routine behind both loading and writing.
//
// Outcomes:
//   - main checkout, plain repository or no git: the nearest
//     .aide.yaml up to the checkout root applies once;
//   - linked worktree: the main checkout's .aide.yaml (found through
//     git, never by walking parents) is inherited, then the
//     worktree's own applies on top;
//   - worktree of a bare repository: there is no main checkout file,
//     nothing is inherited;
//   - submodule: its own repository, never inherits the superproject;
//   - linked worktree whose main checkout is gone or unreadable: Err
//     wraps ErrMainCheckoutUnavailable and names the path.
type ProjectDiscovery struct {
	// CheckoutRoot is the root of the checkout containing the start
	// directory (where .git is), or "" outside git.
	CheckoutRoot string
	// LocalPath is the nearest .aide.yaml in this checkout, or "".
	LocalPath string
	// MainCheckoutRoot is the main worktree's root when the checkout
	// is a linked worktree, else "".
	MainCheckoutRoot string
	// MainPath is MainCheckoutRoot's .aide.yaml when it exists.
	MainPath string
	// Err is set when a linked worktree's main checkout cannot be read.
	Err error
	// Warning explains why nothing was inherited from a repository the
	// checkout claims to belong to but is not registered with.
	Warning string
}

// LocalWritePath is where this checkout's .aide.yaml is written: the
// existing file, else the checkout root, else startDir.
func (d ProjectDiscovery) LocalWritePath(startDir string) string {
	switch {
	case d.LocalPath != "":
		return d.LocalPath
	case d.CheckoutRoot != "":
		return filepath.Join(d.CheckoutRoot, ProjectConfigFileName)
	default:
		return filepath.Join(startDir, ProjectConfigFileName)
	}
}

// anchorRoot is the directory relative paths in the .aide.yaml at path
// are interpreted against: the root of the checkout containing the
// file's symlink-resolved directory (the location trust keys on),
// asked of git from there, never the lexical path aide reached it by.
// Outside any repository it is that resolved directory. It fails when
// neither can be determined.
func anchorRoot(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	dir, err := filepath.EvalSymlinks(filepath.Dir(abs))
	if err != nil {
		return "", err
	}
	out, err := gitOutput(dir, "rev-parse", "--show-toplevel")
	switch {
	case err == nil:
		top := strings.TrimSpace(out)
		if resolved, rerr := filepath.EvalSymlinks(top); rerr == nil {
			top = resolved
		}
		return top, nil
	case strings.Contains(err.Error(), "not a git repository"):
		return dir, nil
	default:
		return "", err
	}
}

// DiscoverProject finds the .aide.yaml files that apply in startDir.
func DiscoverProject(startDir string) ProjectDiscovery {
	var d ProjectDiscovery
	gitEntry := ""
	for dir := startDir; ; {
		if d.LocalPath == "" {
			candidate := filepath.Join(dir, ProjectConfigFileName)
			if _, err := os.Stat(candidate); err == nil {
				d.LocalPath = candidate
			}
		}
		if _, err := os.Lstat(filepath.Join(dir, ".git")); err == nil {
			d.CheckoutRoot = dir
			gitEntry = filepath.Join(dir, ".git")
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	if gitEntry == "" {
		return d
	}
	// A .git directory is a main checkout (or plain repository); only
	// a .git file can be a linked worktree.
	if fi, err := os.Stat(gitEntry); err == nil && fi.IsDir() {
		return d
	}
	main, warning, err := mainCheckoutOf(d.CheckoutRoot, gitEntry)
	d.Warning = warning
	if err != nil {
		d.Err = err
		return d
	}
	if main == "" {
		return d
	}
	d.MainCheckoutRoot = main
	candidate := filepath.Join(main, ProjectConfigFileName)
	switch _, err := os.Stat(candidate); {
	case err == nil:
		d.MainPath = candidate
	case !errors.Is(err, os.ErrNotExist):
		d.Err = mainUnavailable(d.CheckoutRoot, candidate, err)
	}
	return d
}

// mainCheckoutOf returns the main checkout root of the linked worktree
// at root, or "" when root is not a linked worktree (a submodule), its
// repository is bare, or the repository does not register root as one
// of its worktrees (then warning says why nothing is inherited).
func mainCheckoutOf(root, gitFile string) (main, warning string, err error) {
	out, err := gitOutput(root, "rev-parse", "--git-dir", "--git-common-dir")
	if err != nil {
		// git failed or is missing. A submodule inherits nothing; a
		// linked worktree whose repository data is gone (main checkout
		// deleted or moved) is a diagnostic, not silent partial
		// inheritance.
		gitDir := gitDirPointer(gitFile)
		if !isLinkedWorktreeGitDir(gitDir) {
			return "", "", nil
		}
		return "", "", mainUnavailable(root, missingMainPath(gitDir), err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 {
		return "", "", fmt.Errorf("unexpected git rev-parse output in %s: %q", root, out)
	}
	gitDir, commonDir := absUnder(root, lines[0]), absUnder(root, lines[1])
	if sameDir(gitDir, commonDir) {
		return "", "", nil // not a linked worktree: a submodule
	}
	list, err := gitOutput(root, "worktree", "list", "--porcelain")
	if err != nil {
		return "", "", mainUnavailable(root, commonDir, err)
	}
	if !registeredWorktree(root, gitDir, commonDir, list) {
		return "", fmt.Sprintf("%s is not a registered worktree of %s (its .git file points at %s); "+
			"no .aide.yaml is inherited from that repository", root, commonDir, gitDir), nil
	}
	main, bare := firstWorktree(list)
	if bare || main == "" {
		return "", "", nil
	}
	if fi, err := os.Stat(main); err != nil || !fi.IsDir() {
		if err == nil {
			err = errors.New("not a directory")
		}
		return "", "", mainUnavailable(root, main, err)
	}
	return main, "", nil
}

// registeredWorktree reports whether the repository at commonDir
// records root as one of its linked worktrees, so a .git file cannot
// borrow another repository's configuration by naming it: the
// administrative dir must lie in <commonDir>/worktrees/, its gitdir
// file must point back at root's .git, and git worktree list must name
// root.
func registeredWorktree(root, gitDir, commonDir, porcelain string) bool {
	if !sameDir(filepath.Dir(gitDir), filepath.Join(commonDir, "worktrees")) {
		return false
	}
	back, err := os.ReadFile(filepath.Join(gitDir, "gitdir")) // #nosec G304 -- git's own worktree record
	if err != nil || !sameDir(absUnder(gitDir, strings.TrimSpace(string(back))), filepath.Join(root, ".git")) {
		return false
	}
	for _, line := range strings.Split(porcelain, "\n") {
		if p, ok := strings.CutPrefix(line, "worktree "); ok && sameDir(p, root) {
			return true
		}
	}
	return false
}

func mainUnavailable(worktree, path string, cause error) error {
	return fmt.Errorf("%w: linked worktree %s inherits .aide.yaml from its main checkout, "+
		"but %s cannot be read (%v). Restore it; if the main checkout moved run "+
		"`git worktree repair` from it, if it was deleted run `git worktree prune` "+
		"from a remaining checkout; or launch with --ignore-project-config",
		ErrMainCheckoutUnavailable, worktree, path, cause)
}

// gitOutput runs git in dir, ignoring GIT_DIR-style overrides from the
// environment so the answer is about dir.
func gitOutput(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...) // #nosec G204 -- fixed git subcommands
	// Untranslated messages: callers match "not a git repository".
	cmd.Env = append(cmd.Env, "LC_ALL=C")
	for _, kv := range os.Environ() {
		if name, _, _ := strings.Cut(kv, "="); name == "GIT_DIR" || name == "GIT_WORK_TREE" ||
			name == "GIT_COMMON_DIR" || name == "GIT_INDEX_FILE" || name == "LC_ALL" {
			continue
		}
		cmd.Env = append(cmd.Env, kv)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return "", fmt.Errorf("%w: %s", err, msg)
		}
		return "", err
	}
	return string(out), nil
}

// gitDirPointer reads the "gitdir: <path>" line of a .git file.
func gitDirPointer(gitFile string) string {
	data, err := os.ReadFile(gitFile) // #nosec G304 -- the checkout's own .git file
	if err != nil {
		return ""
	}
	p, ok := strings.CutPrefix(strings.TrimSpace(string(data)), "gitdir:")
	if !ok {
		return ""
	}
	return absUnder(filepath.Dir(gitFile), strings.TrimSpace(p))
}

// isLinkedWorktreeGitDir reports whether a .git file's target looks
// like a linked worktree's administrative directory
// (<repo>/worktrees/<name>) rather than a submodule's
// (<super>/.git/modules/<name>).
func isLinkedWorktreeGitDir(gitDir string) bool {
	return filepath.Base(filepath.Dir(gitDir)) == "worktrees"
}

// missingMainPath names the main checkout a worktree's administrative
// directory belongs to: <main>/.git/worktrees/<name> -> <main>.
func missingMainPath(gitDir string) string {
	if gitDir == "" {
		return "the repository"
	}
	repo := filepath.Dir(filepath.Dir(gitDir))
	if filepath.Base(repo) == ".git" {
		return filepath.Dir(repo)
	}
	return repo
}

// firstWorktree parses `git worktree list --porcelain`: the first
// entry is the main worktree, or the bare repository.
func firstWorktree(porcelain string) (path string, bare bool) {
	sc := bufio.NewScanner(strings.NewReader(porcelain))
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			break
		}
		if p, ok := strings.CutPrefix(line, "worktree "); ok {
			path = p
		}
		if line == "bare" {
			bare = true
		}
	}
	return path, bare
}

func absUnder(base, p string) string {
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	return filepath.Join(base, p)
}

func sameDir(a, b string) bool {
	ra, errA := filepath.EvalSymlinks(a)
	rb, errB := filepath.EvalSymlinks(b)
	if errA != nil || errB != nil {
		return filepath.Clean(a) == filepath.Clean(b)
	}
	return ra == rb
}
