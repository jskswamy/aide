// Filesystem guard for macOS Seatbelt profiles.
//
// Controls file system access with writable project paths, scoped $HOME
// reads for development directories, and denied paths with glob expansion.

package guards

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/jskswamy/aide/internal/fsutil"
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
	// Mirror the read-side contract for writes: macOS seatbelt fires
	// file-write* policy on the kernel-resolved target, so for each
	// declared symlink we also include its resolved path (when safely
	// under $HOME) in the require-any block.
	for _, p := range ctx.ExtraWritable {
		if resolved, ok := safeResolvedWidening(p, home); ok {
			writable = append(writable, resolved)
		}
	}

	var rules []seatbelt.Rule

	// Writable paths
	if len(writable) > 0 {
		rules = append(rules, seatbelt.AllowRule(
			fmt.Sprintf("(allow file-read* file-write*\n    %s)", buildRequireAny(writable))))
	}

	// Scoped $HOME reads — narrow baseline only
	if home != "" {
		rules = append(rules,
			// aide's own paths
			seatbelt.SectionAllow("aide configuration (read-only)"),
			seatbelt.AllowRule(`(allow file-read*
    `+seatbelt.HomeSubpath(home, ".config/aide")+`
)`),
			seatbelt.SectionAllow("aide data (read-write)"),
			seatbelt.AllowRule(`(allow file-read* file-write*
    `+seatbelt.HomeSubpath(home, ".local/share/aide")+`
)`),

			// Build caches (read-write)
			seatbelt.SectionAllow("Build caches (read-write)"),
			seatbelt.AllowRule(`(allow file-read* file-write*
    `+seatbelt.HomeSubpath(home, ".cache")+`
    `+seatbelt.HomeSubpath(home, "Library/Caches")+`
)`),

			// Home directory listing and broad metadata traversal
			seatbelt.SectionAllow("Home directory traversal"),
			seatbelt.AllowRule(`(allow file-read-data
    `+seatbelt.HomeLiteral(home, "")+`
)`),
			seatbelt.AllowRule(`(allow file-read-metadata
    `+seatbelt.HomeSubpath(home, "")+`
)`),
		)

		// ExtraReadable — adds allow rules AND serves as deny opt-out.
		//
		// macOS seatbelt matches file-read* policy on the kernel-resolved
		// path, not the literal syscall argument. When the user's path is
		// a symlink (the home-manager / nix-darwin / stow / chezmoi
		// pattern), a literal-only rule never fires because the kernel
		// asks the policy about the resolved target. For each declared
		// path we emit the literal rule and, if EvalSymlinks resolves to
		// a different path under $HOME, an additional rule for the target.
		// Resolved targets outside $HOME are NOT widened here — they need
		// upstream validation (AIDE-mu8 escape hatch) and surfacing in
		// aide cap show so the user can see what's being granted.
		if len(ctx.ExtraReadable) > 0 {
			for _, p := range ctx.ExtraReadable {
				rules = append(rules,
					seatbelt.AllowRule(fmt.Sprintf(`(allow file-read* %s)`, seatbelt.Path(p))))
				if resolved, ok := safeResolvedWidening(p, home); ok {
					rules = append(rules,
						seatbelt.AllowRule(fmt.Sprintf(`(allow file-read* %s)`, seatbelt.Path(resolved))))
				}
			}
		}
	}

	// Non-filesystem operations (from capability Allow field)
	if len(ctx.ExtraAllow) > 0 {
		rules = append(rules, seatbelt.SectionAllow("Non-filesystem operations (via capability)"))
		for _, op := range ctx.ExtraAllow {
			rules = append(rules, seatbelt.AllowRule(fmt.Sprintf("(allow %s)", op)))
		}
	}

	// Denied paths.
	//
	// Resolution is asymmetric to the allow side. For allows we gate
	// under-$HOME widening to avoid silently broadening the sandbox;
	// for denies we widen to the EvalSymlinks-resolved target REGARDLESS
	// of where it lives, because (a) the kernel matches deny rules on
	// the resolved path so without it a symlink-fronted secret stays
	// writable through the link, and (b) over-denying has no security
	// downside — the user's intent is protection.
	if len(ctx.ExtraDenied) > 0 {
		expanded := seatbelt.ExpandGlobs(ctx.ExtraDenied)
		denyTargets := make([]string, 0, len(expanded)*2)
		seen := make(map[string]bool, len(expanded)*2)
		for _, p := range expanded {
			if !seen[p] {
				seen[p] = true
				denyTargets = append(denyTargets, p)
			}
			if resolved := fsutil.ResolveOrSelf(p); resolved != p && !seen[resolved] {
				seen[resolved] = true
				denyTargets = append(denyTargets, resolved)
			}
		}
		for _, p := range denyTargets {
			expr := seatbelt.Path(p)
			rules = append(rules,
				seatbelt.DenyRule(fmt.Sprintf("(deny file-read-data %s)", expr)),
				seatbelt.DenyRule(fmt.Sprintf("(deny file-write* %s)", expr)),
			)
		}
	}

	return seatbelt.GuardResult{Rules: rules}
}


// safeResolvedWidening returns (resolved, true) when path is a symlink whose
// EvalSymlinks-resolved target differs from path and lies strictly under
// home. Returns ("", false) for non-symlinks, missing paths, or targets that
// would escape $HOME. The under-$HOME gate is the safety ceiling for the
// filesystem guard; outside-$HOME widening is a deliberate user opt-in that
// belongs upstream at config load (AIDE-mu8), not silently in rule emission.
func safeResolvedWidening(path, home string) (string, bool) {
	if home == "" {
		return "", false
	}
	resolved := fsutil.ResolveOrSelf(path)
	if resolved == path {
		return "", false
	}
	if !isUnderDir(resolved, home) {
		return "", false
	}
	return resolved, true
}

// isUnderDir reports whether path is dir itself or strictly under it,
// using filepath separator semantics so /home/userX is not under /home/user.
func isUnderDir(path, dir string) bool {
	if path == dir {
		return true
	}
	return strings.HasPrefix(path, dir+string(filepath.Separator))
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
