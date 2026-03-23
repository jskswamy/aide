package guards

import (
	"fmt"
	"strings"

	"github.com/jskswamy/aide/pkg/seatbelt"
)

// DenyDir denies read+write to a directory tree using (subpath ...).
func DenyDir(path string) []seatbelt.Rule {
	return []seatbelt.Rule{
		seatbelt.RestrictRule(fmt.Sprintf(`(deny file-read-data (subpath "%s"))`, path)),
		seatbelt.RestrictRule(fmt.Sprintf(`(deny file-write* (subpath "%s"))`, path)),
	}
}

// DenyFile denies read+write to a single file using (literal ...).
func DenyFile(path string) []seatbelt.Rule {
	return []seatbelt.Rule{
		seatbelt.RestrictRule(fmt.Sprintf(`(deny file-read-data (literal "%s"))`, path)),
		seatbelt.RestrictRule(fmt.Sprintf(`(deny file-write* (literal "%s"))`, path)),
	}
}

// AllowReadFile allows reading a single file using (literal ...).
func AllowReadFile(path string) seatbelt.Rule {
	return seatbelt.GrantRule(fmt.Sprintf(`(allow file-read* (literal "%s"))`, path))
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

// CloudGuardNames returns names of all cloud guards (for "cloud" meta-guard).
func CloudGuardNames() []string {
	return []string{"cloud-aws", "cloud-gcp", "cloud-azure", "cloud-digitalocean", "cloud-oci"}
}
