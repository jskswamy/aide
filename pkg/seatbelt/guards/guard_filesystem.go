// Filesystem guard for macOS Seatbelt profiles.
//
// Controls file system access with writable project paths, scoped $HOME
// reads for development directories, and denied paths with glob expansion.

package guards

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jskswamy/aide/pkg/seatbelt"
)

// filesystemGuard reads paths from ctx fields.
type filesystemGuard struct{}

// FilesystemGuard returns a Guard that reads ctx.ProjectRoot, ctx.HomeDir,
// ctx.RuntimeDir, ctx.TempDir, and ctx.ExtraDenied for filesystem rules.
func FilesystemGuard() seatbelt.Guard { return &filesystemGuard{} }

func (g *filesystemGuard) Name() string        { return "filesystem" }
func (g *filesystemGuard) Type() string        { return "always" }
func (g *filesystemGuard) Description() string {
	return "Project directory (read-write) and scoped home directory (read-only) access"
}

func (g *filesystemGuard) Rules(ctx *seatbelt.Context) seatbelt.GuardResult {
	if ctx == nil {
		return seatbelt.GuardResult{}
	}

	home := ctx.HomeDir
	var writable []string

	if ctx.ProjectRoot != "" {
		writable = append(writable, ctx.ProjectRoot)
	}
	if ctx.RuntimeDir != "" {
		writable = append(writable, ctx.RuntimeDir)
	}
	if ctx.TempDir != "" {
		writable = append(writable, ctx.TempDir)
	}
	writable = append(writable, ctx.ExtraWritable...)

	var rules []seatbelt.Rule

	// Writable paths
	if len(writable) > 0 {
		rules = append(rules, seatbelt.AllowRule(
			fmt.Sprintf("(allow file-read* file-write*\n    %s)", buildRequireAny(writable))))
	}

	// Scoped $HOME reads — development paths only
	if home != "" {
		rules = append(rules,
			seatbelt.SectionAllow("Home development paths (read-only)"),
			seatbelt.AllowRule(`(allow file-read*
    `+seatbelt.HomeSubpath(home, ".config")+`
    `+seatbelt.HomeSubpath(home, ".cache")+`
    `+seatbelt.HomeSubpath(home, ".local")+`
    `+seatbelt.HomeSubpath(home, ".nix-profile")+`
    `+seatbelt.HomeSubpath(home, ".nix-defexpr")+`
    `+seatbelt.HomeSubpath(home, ".ssh")+`
    `+seatbelt.HomeSubpath(home, ".cargo")+`
    `+seatbelt.HomeSubpath(home, ".rustup")+`
    `+seatbelt.HomeSubpath(home, "go")+`
    `+seatbelt.HomeSubpath(home, ".pyenv")+`
    `+seatbelt.HomeSubpath(home, ".rbenv")+`
    `+seatbelt.HomeSubpath(home, ".sdkman")+`
    `+seatbelt.HomeSubpath(home, ".gradle")+`
    `+seatbelt.HomeSubpath(home, ".m2")+`
    `+seatbelt.HomeSubpath(home, ".gnupg")+`
    `+seatbelt.HomeSubpath(home, "Library/Keychains")+`
    `+seatbelt.HomeSubpath(home, "Library/Caches")+`
    `+seatbelt.HomeSubpath(home, "Library/Preferences")+`
)`),

			// Build cache and GPG directories (read-write) — Go, npm, pip
			// write to caches; GPG writes lock files, trustdb, random_seed.
			// Password-managers guard still denies private-keys-v1.d via deny-wins.
			seatbelt.SectionAllow("Build cache and GPG directories (read-write)"),
			seatbelt.AllowRule(`(allow file-read* file-write*
    `+seatbelt.HomeSubpath(home, "Library/Caches")+`
    `+seatbelt.HomeSubpath(home, ".cache")+`
    `+seatbelt.HomeSubpath(home, ".gnupg")+`
)`),

			// Dotfiles directly in $HOME (e.g., .gitconfig, .npmrc)
			seatbelt.SectionAllow("Home dotfiles"),
			seatbelt.AllowRule(fmt.Sprintf(`(allow file-read*
    (regex #"^%s/\.[^/]+$")
)`, home)),

			// Home directory listing and broad metadata traversal
			seatbelt.SectionAllow("Home directory traversal"),
			seatbelt.AllowRule(`(allow file-read-data
    `+seatbelt.HomeLiteral(home, "")+`
)`),
			seatbelt.AllowRule(`(allow file-read-metadata
    `+seatbelt.HomeSubpath(home, "")+`
)`),
		)

		// Symlink targets — dotfiles managed by stow/home-manager/etc.
		// Resolve symlinks in $HOME that point to directories under $HOME
		// and add their targets so the kernel can follow them.
		for _, dir := range resolveHomeDotfileSymlinks(home) {
			rules = append(rules,
				seatbelt.AllowRule(fmt.Sprintf(`(allow file-read* %s)`,
					seatbelt.Path(dir))))
		}

		// ExtraReadable — adds allow rules AND serves as deny opt-out
		if len(ctx.ExtraReadable) > 0 {
			for _, p := range ctx.ExtraReadable {
				rules = append(rules,
					seatbelt.AllowRule(fmt.Sprintf(`(allow file-read* %s)`, seatbelt.Path(p))))
			}
		}
	}

	// Denied paths
	if len(ctx.ExtraDenied) > 0 {
		expanded := seatbelt.ExpandGlobs(ctx.ExtraDenied)
		for _, p := range expanded {
			expr := seatbelt.Path(p)
			rules = append(rules,
				seatbelt.DenyRule(fmt.Sprintf("(deny file-read-data %s)", expr)),
				seatbelt.DenyRule(fmt.Sprintf("(deny file-write* %s)", expr)),
			)
		}
	}

	return seatbelt.GuardResult{Rules: rules}
}

// resolveHomeDotfileSymlinks scans dotfiles in $HOME for symlinks and returns
// the unique parent directories of their resolved targets (when under $HOME).
// This handles dotfile managers (stow, home-manager, etc.) that create symlinks
// like ~/.gitconfig → ~/dot-files/git/.gitconfig.
func resolveHomeDotfileSymlinks(home string) []string {
	entries, err := os.ReadDir(home)
	if err != nil {
		return nil
	}

	seen := make(map[string]bool)
	var dirs []string

	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, ".") || entry.IsDir() {
			continue
		}

		fullPath := filepath.Join(home, name)
		target, err := filepath.EvalSymlinks(fullPath)
		if err != nil || target == fullPath {
			continue // not a symlink or can't resolve
		}

		// Only add targets that live under $HOME (outside targets like
		// /nix/store are covered by broad system reads).
		if !strings.HasPrefix(target, home+"/") {
			continue
		}

		dir := filepath.Dir(target)
		if !seen[dir] {
			seen[dir] = true
			dirs = append(dirs, dir)
		}
	}

	return dirs
}

func buildRequireAny(paths []string) string {
	if len(paths) == 1 {
		return seatbelt.Path(paths[0])
	}
	var exprs []string
	for _, p := range paths {
		exprs = append(exprs, "    "+seatbelt.Path(p))
	}
	return fmt.Sprintf("(require-any\n%s)", strings.Join(exprs, "\n"))
}
