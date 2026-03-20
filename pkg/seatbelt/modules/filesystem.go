// Filesystem module for macOS Seatbelt profiles.
//
// Controls file system access with writable, readable, and denied paths.
// Denied paths support glob expansion.
package modules

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

type filesystemModule struct {
	cfg FilesystemConfig
}

// Filesystem returns a module that controls filesystem access.
func Filesystem(cfg FilesystemConfig) seatbelt.Module {
	return &filesystemModule{cfg: cfg}
}

func (m *filesystemModule) Name() string { return "Filesystem" }

func (m *filesystemModule) Rules(_ *seatbelt.Context) []seatbelt.Rule {
	var rules []seatbelt.Rule

	if len(m.cfg.Writable) > 0 {
		rules = append(rules, m.writableRule())
	}
	if len(m.cfg.Readable) > 0 {
		rules = append(rules, m.readableRule())
	}
	if len(m.cfg.Denied) > 0 {
		rules = append(rules, m.deniedRules()...)
	}

	return rules
}

func (m *filesystemModule) writableRule() seatbelt.Rule {
	return seatbelt.Raw(fmt.Sprintf("(allow file-read* file-write*\n    %s)", buildRequireAny(m.cfg.Writable)))
}

func (m *filesystemModule) readableRule() seatbelt.Rule {
	return seatbelt.Raw(fmt.Sprintf("(allow file-read*\n    %s)", buildRequireAny(m.cfg.Readable)))
}

func (m *filesystemModule) deniedRules() []seatbelt.Rule {
	expanded := seatbelt.ExpandGlobs(m.cfg.Denied)
	var rules []seatbelt.Rule
	for _, p := range expanded {
		expr := seatbelt.SeatbeltPath(p)
		rules = append(rules,
			seatbelt.Raw(fmt.Sprintf("(deny file-read-data %s)", expr)),
			seatbelt.Raw(fmt.Sprintf("(deny file-write* %s)", expr)),
		)
	}
	return rules
}

func buildRequireAny(paths []string) string {
	if len(paths) == 1 {
		return seatbelt.SeatbeltPath(paths[0])
	}
	var exprs []string
	for _, p := range paths {
		exprs = append(exprs, "    "+seatbelt.SeatbeltPath(p))
	}
	return fmt.Sprintf("(require-any\n%s)", strings.Join(exprs, "\n"))
}
