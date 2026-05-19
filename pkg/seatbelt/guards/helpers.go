package guards

import (
	"os"
	"strings"

	"github.com/jskswamy/aide/internal/fsutil"
	"github.com/jskswamy/aide/pkg/seatbelt"
)

// DenyDir denies read+write to a directory tree using (subpath ...).
func DenyDir(path string) []seatbelt.Rule {
	return []seatbelt.Rule{
		seatbelt.DenySubpath(path, "file-read-data"),
		seatbelt.DenySubpath(path, "file-write*"),
	}
}

// DenyFile denies read+write to a single file using (literal ...).
func DenyFile(path string) []seatbelt.Rule {
	return []seatbelt.Rule{
		seatbelt.DenyLiteral(path, "file-read-data"),
		seatbelt.DenyLiteral(path, "file-write*"),
	}
}

// AllowReadFile allows reading a single file using (literal ...).
func AllowReadFile(path string) seatbelt.Rule {
	return seatbelt.AllowLiteral(path, "file-read*")
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


// ResolveSymlink resolves symlinks in a path, returning the original
// path if resolution fails. Thin wrapper over fsutil.ResolveOrSelf so
// the symlink-resolution contract has one implementation in the repo.
func ResolveSymlink(path string) string {
	return fsutil.ResolveOrSelf(path)
}
