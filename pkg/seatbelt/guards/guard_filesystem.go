// Filesystem guard for macOS Seatbelt profiles.
//
// Controls file system access with writable, readable, and denied paths.
// Denied paths support glob expansion.

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
	return "Project directory (read-write) and home directory (read-only) access"
}

func (g *filesystemGuard) Rules(ctx *seatbelt.Context) []seatbelt.Rule {
	if ctx == nil {
		return nil
	}

	var writable, readable []string
	if ctx.ProjectRoot != "" {
		writable = append(writable, ctx.ProjectRoot)
	}
	if ctx.HomeDir != "" {
		readable = append(readable, ctx.HomeDir)
	}
	if ctx.RuntimeDir != "" {
		writable = append(writable, ctx.RuntimeDir)
	}
	if ctx.TempDir != "" {
		writable = append(writable, ctx.TempDir)
	}

	return filesystemRules(writable, readable, ctx.ExtraDenied)
}

func filesystemRules(writable, readable, denied []string) []seatbelt.Rule {
	var rules []seatbelt.Rule

	if len(writable) > 0 {
		rules = append(rules, seatbelt.Raw(fmt.Sprintf("(allow file-read* file-write*\n    %s)", buildRequireAny(writable))))
	}
	if len(readable) > 0 {
		rules = append(rules, seatbelt.Raw(fmt.Sprintf("(allow file-read*\n    %s)", buildRequireAny(readable))))
	}
	if len(denied) > 0 {
		expanded := seatbelt.ExpandGlobs(denied)
		for _, p := range expanded {
			expr := seatbelt.Path(p)
			rules = append(rules,
				seatbelt.Raw(fmt.Sprintf("(deny file-read-data %s)", expr)),
				seatbelt.Raw(fmt.Sprintf("(deny file-write* %s)", expr)),
			)
		}
	}

	return rules
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
