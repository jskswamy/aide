// Filesystem guard for macOS Seatbelt profiles.
//
// Controls file system access with writable project paths, scoped $HOME
// reads for development directories, and denied paths with glob expansion.

package guards

import (
	"fmt"
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
