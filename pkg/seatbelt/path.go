package seatbelt

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SeatbeltPath returns the Seatbelt path expression for a filesystem path.
// Directories use (subpath ...), files use (literal ...).
func SeatbeltPath(p string) string {
	info, err := os.Stat(p)
	if err == nil && info.IsDir() {
		return fmt.Sprintf(`(subpath "%s")`, p)
	}
	return fmt.Sprintf(`(literal "%s")`, p)
}

// HomeSubpath returns (subpath "<home>/<rel>") for use in profile rules.
func HomeSubpath(home, rel string) string {
	return fmt.Sprintf(`(subpath "%s")`, filepath.Join(home, rel))
}

// HomeLiteral returns (literal "<home>/<rel>") for use in profile rules.
func HomeLiteral(home, rel string) string {
	return fmt.Sprintf(`(literal "%s")`, filepath.Join(home, rel))
}

// HomePrefix returns (prefix "<home>/<rel>") for use in profile rules.
func HomePrefix(home, rel string) string {
	return fmt.Sprintf(`(prefix "%s")`, filepath.Join(home, rel))
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
