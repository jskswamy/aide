package seatbelt

import (
	"fmt"
	"strings"
)

// AllowSubpath returns an allow rule for the given file operations on
// path and all paths beneath it: (allow <ops...> (subpath "<path>")).
//
// The path is quoted with %q so embedded double quotes and backslashes
// are escaped into valid sexp literal syntax — replaces ad-hoc
// fmt.Sprintf("...%s...") sites that broke on weird paths.
//
// Note: macOS seatbelt fires file-write* policy on the kernel-resolved
// path. If you need symlinked dotfiles installs to work, resolve the
// path with fsutil.ResolveOrSelf before calling this — the builder
// itself does not auto-resolve (callers may want the literal form for
// some rules).
func AllowSubpath(path string, ops ...string) Rule {
	return AllowRule(fmt.Sprintf(`(allow %s (subpath %q))`, strings.Join(ops, " "), path))
}

// DenySubpath is the (deny ...) counterpart of AllowSubpath.
func DenySubpath(path string, ops ...string) Rule {
	return DenyRule(fmt.Sprintf(`(deny %s (subpath %q))`, strings.Join(ops, " "), path))
}

// AllowLiteral returns an allow rule for a single file path:
// (allow <ops...> (literal "<path>")).
func AllowLiteral(path string, ops ...string) Rule {
	return AllowRule(fmt.Sprintf(`(allow %s (literal %q))`, strings.Join(ops, " "), path))
}

// DenyLiteral is the (deny ...) counterpart of AllowLiteral.
func DenyLiteral(path string, ops ...string) Rule {
	return DenyRule(fmt.Sprintf(`(deny %s (literal %q))`, strings.Join(ops, " "), path))
}
