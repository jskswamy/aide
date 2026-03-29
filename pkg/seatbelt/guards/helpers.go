package guards

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jskswamy/aide/pkg/seatbelt"
)

// DenyDir denies read+write to a directory tree using (subpath ...).
func DenyDir(path string) []seatbelt.Rule {
	return []seatbelt.Rule{
		seatbelt.DenyRule(fmt.Sprintf(`(deny file-read-data (subpath "%s"))`, path)),
		seatbelt.DenyRule(fmt.Sprintf(`(deny file-write* (subpath "%s"))`, path)),
	}
}

// DenyFile denies read+write to a single file using (literal ...).
func DenyFile(path string) []seatbelt.Rule {
	return []seatbelt.Rule{
		seatbelt.DenyRule(fmt.Sprintf(`(deny file-read-data (literal "%s"))`, path)),
		seatbelt.DenyRule(fmt.Sprintf(`(deny file-write* (literal "%s"))`, path)),
	}
}

// AllowReadFile allows reading a single file using (literal ...).
func AllowReadFile(path string) seatbelt.Rule {
	return seatbelt.AllowRule(fmt.Sprintf(`(allow file-read* (literal "%s"))`, path))
}

// EnvOverridePath returns the env var value if set and non-empty, otherwise the
// home-relative default path resolved via ctx.HomePath.
func EnvOverridePath(ctx *seatbelt.Context, envKey, defaultPath string) string {
	if val, ok := ctx.EnvLookup(envKey); ok && val != "" {
		return val
	}
	return ctx.HomePath(defaultPath)
}

// SplitColonPaths splits a colon-separated path string, skipping empty segments.
func SplitColonPaths(s string) []string {
	parts := strings.Split(s, ":")
	var result []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

// dirExists returns true if path exists and is a directory.
func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// pathExists returns true if path exists (file or directory).
func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// ExpandTilde expands ~ and ~/ prefixes to the home directory.
// Uses string concatenation instead of filepath.Join to preserve
// trailing slashes, which is important for gitdir: prefix matching patterns.
func ExpandTilde(path, homeDir string) string {
	if strings.HasPrefix(path, "~/") {
		// Use string concatenation instead of filepath.Join to preserve
		// trailing slashes. filepath.Join cleans paths, stripping
		// trailing / which breaks gitdir: prefix matching patterns.
		return homeDir + "/" + path[2:]
	}
	if path == "~" {
		return homeDir
	}
	return path
}

// ResolveSymlink resolves symlinks in a path, returning the original path if resolution fails.
func ResolveSymlink(path string) string {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return path
	}
	return resolved
}
