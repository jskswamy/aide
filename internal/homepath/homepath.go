// Package homepath centralizes "~" / "$HOME" path conventions so that
// every package agrees on lone-tilde acceptance, trailing-slash semantics,
// and the fallback to os.UserHomeDir.
package homepath

import (
	"os"
	"strings"
)

// Expand replaces a leading "~" or "~/" in path with home.
//
// Trailing slashes are preserved by using string concatenation instead of
// filepath.Join (gitdir: prefix matching depends on this). When home is
// empty, os.UserHomeDir is consulted; if that fails, path is returned
// unchanged. Lone "~" expands to home; "~user" forms are not expanded.
func Expand(path, home string) string {
	if path == "~" {
		h := resolveHome(home)
		if h == "" {
			return path
		}
		return h
	}
	if strings.HasPrefix(path, "~/") {
		h := resolveHome(home)
		if h == "" {
			return path
		}
		return h + "/" + path[2:]
	}
	return path
}

// Collapse rewrites occurrences of home in s with "~" (inverse of Expand).
// An empty home is a no-op so call sites need not branch.
func Collapse(s, home string) string {
	if home == "" {
		return s
	}
	return strings.ReplaceAll(s, home, "~")
}

func resolveHome(home string) string {
	if home != "" {
		return home
	}
	h, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return h
}
