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
