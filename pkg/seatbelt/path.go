package seatbelt

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Path returns the Seatbelt path expression for a filesystem path.
// Directories use (subpath ...), files use (literal ...).
func Path(p string) string {
	info, err := os.Stat(p)
	if err == nil && info.IsDir() {
		return fmt.Sprintf(`(subpath "%s")`, p)
	}
	return fmt.Sprintf(`(literal "%s")`, p)
}

// HomeSubpath returns (subpath "<home>/<rel>") for use in profile rules.
func HomeSubpath(home, rel string) string { return homeExpr("subpath", home, rel) }

// HomeLiteral returns (literal "<home>/<rel>") for use in profile rules.
func HomeLiteral(home, rel string) string { return homeExpr("literal", home, rel) }

// HomePrefix returns (prefix "<home>/<rel>") for use in profile rules.
func HomePrefix(home, rel string) string { return homeExpr("prefix", home, rel) }

func homeExpr(form, home, rel string) string {
	return fmt.Sprintf(`(%s "%s")`, form, filepath.Join(home, rel))
}

// SubpathWithParentMetadata returns two rules: (allow file-read* (subpath ...))
// for the given path, and (allow file-read-metadata (literal ...)) for its
// parent directory. Seatbelt subpath rules don't grant lstat on the parent
// itself, which breaks filepath.EvalSymlinks traversal.
func SubpathWithParentMetadata(path string) []Rule {
	return []Rule{
		AllowSubpath(path, "file-read*"),
		AllowLiteral(filepath.Dir(path), "file-read-metadata"),
	}
}

// ExpandGlobs expands glob patterns in a list of paths.
// Non-glob paths are passed through unchanged.
func ExpandGlobs(patterns []string) []string {
	var result []string
	for _, p := range patterns {
		if strings.ContainsAny(p, "*?[") {
			matches, _ := filepath.Glob(p)
			result = append(result, matches...)
		} else {
			result = append(result, p)
		}
	}
	return result
}

// ExpandHome replaces a leading "~" or "~/" in path with home.
// Trailing slashes are preserved by using string concatenation instead of
// filepath.Join. When home is empty, os.UserHomeDir is consulted; if that
// fails, path is returned unchanged. Lone "~" expands to home; "~user" forms
// are not expanded.
func ExpandHome(path, home string) string {
	if path == "~" {
		h := resolveHomeDir(home)
		if h == "" {
			return path
		}
		return h
	}
	if strings.HasPrefix(path, "~/") {
		h := resolveHomeDir(home)
		if h == "" {
			return path
		}
		return h + "/" + path[2:]
	}
	return path
}

func resolveHomeDir(home string) string {
	if home != "" {
		return home
	}
	h, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return h
}

// ResolveOrSelf returns filepath.EvalSymlinks(path) when resolution succeeds,
// otherwise the input path unchanged.
func ResolveOrSelf(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	return path
}

// IsUnderDir reports whether path is dir itself or nested below it, using
// path-separator-aware comparison after filepath.Clean.
func IsUnderDir(path, dir string) bool {
	p := filepath.Clean(path)
	d := filepath.Clean(dir)
	if p == d {
		return true
	}
	return strings.HasPrefix(p, d+string(filepath.Separator))
}

// ResolveWidening reports how a capability path classifies once symlinks are
// followed: (resolved target, did it differ from the input, does the resolved
// target live under home).
func ResolveWidening(path, home string) (resolved string, changed, underHome bool) {
	resolved = ResolveOrSelf(path)
	if resolved == path {
		return path, false, home != "" && IsUnderDir(path, home)
	}
	underHome = home != "" && IsUnderDir(resolved, home)
	return resolved, true, underHome
}

