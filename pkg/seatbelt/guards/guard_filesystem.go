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

// FilesystemConfig specifies filesystem access rules.
type FilesystemConfig struct {
	// Writable paths get read+write access.
	Writable []string
	// Readable paths get read-only access.
	Readable []string
	// Denied paths are blocked for both read and write.
	// Supports glob patterns.
	Denied []string
}

// filesystemGuard reads paths from ctx fields.
type filesystemGuard struct{}

// FilesystemGuard returns a Guard that reads ctx.ProjectRoot, ctx.HomeDir,
// ctx.RuntimeDir, ctx.TempDir, and ctx.ExtraDenied for filesystem rules.
func FilesystemGuard() seatbelt.Guard { return &filesystemGuard{} }

func (g *filesystemGuard) Name() string        { return "filesystem" }
func (g *filesystemGuard) Type() string        { return "always" }
func (g *filesystemGuard) Description() string { return "project and home filesystem access" }

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

	cfg := FilesystemConfig{
		Writable: writable,
		Readable: readable,
		Denied:   ctx.ExtraDenied,
	}
	return filesystemRules(cfg)
}

// filesystemModule is the backward-compat wrapper.
type filesystemModule struct {
	cfg FilesystemConfig
}

// Filesystem returns a module that controls filesystem access.
func Filesystem(cfg FilesystemConfig) seatbelt.Module {
	return &filesystemModule{cfg: cfg}
}

func (m *filesystemModule) Name() string { return "Filesystem" }

func (m *filesystemModule) Rules(_ *seatbelt.Context) []seatbelt.Rule {
	return filesystemRules(m.cfg)
}

func filesystemRules(cfg FilesystemConfig) []seatbelt.Rule {
	var rules []seatbelt.Rule

	if len(cfg.Writable) > 0 {
		rules = append(rules, seatbelt.Raw(fmt.Sprintf("(allow file-read* file-write*\n    %s)", buildRequireAny(cfg.Writable))))
	}
	if len(cfg.Readable) > 0 {
		rules = append(rules, seatbelt.Raw(fmt.Sprintf("(allow file-read*\n    %s)", buildRequireAny(cfg.Readable))))
	}
	if len(cfg.Denied) > 0 {
		expanded := seatbelt.ExpandGlobs(cfg.Denied)
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
