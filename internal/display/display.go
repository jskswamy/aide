// Package display provides shared formatting and display helper functions
// used by both the CLI commands and the launcher banner.
package display

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// NetworkDisplayName converts a raw network mode to a user-friendly label.
func NetworkDisplayName(mode string) string {
	switch mode {
	case "outbound":
		return "outbound only"
	case "none":
		return "none"
	case "unrestricted":
		return "unrestricted"
	default:
		return mode
	}
}

var (
	reSecretsDot   = regexp.MustCompile(`\{\{\s*\.secrets\.(\w+)\s*\}\}`)
	reSecretsIndex = regexp.MustCompile(`\{\{\s*index\s+\.secrets\s+"(\w+)"\s*\}\}`)
)

// ClassifyEnvSource determines the source type of an env template value.
// Returns a human-readable source label and the secret key name (if any).
func ClassifyEnvSource(val string) (source string, secretKey string) {
	if m := reSecretsDot.FindStringSubmatch(val); m != nil {
		return fmt.Sprintf("from secrets.%s", m[1]), m[1]
	}
	if m := reSecretsIndex.FindStringSubmatch(val); m != nil {
		return fmt.Sprintf("from secrets.%s", m[1]), m[1]
	}
	if strings.Contains(val, ".project_root") {
		return "from project_root", ""
	}
	if strings.Contains(val, ".runtime_dir") {
		return "from runtime_dir", ""
	}
	if strings.Contains(val, "{{") {
		return "template", ""
	}
	return "literal", ""
}

// ResolveEnvDisplay returns a display-friendly value for an env var.
// If the value comes from a secret, it redacts the resolved secret value.
func ResolveEnvDisplay(val, _, secretKey string, secretMap map[string]string) string {
	if secretKey != "" && secretMap != nil {
		if sv, ok := secretMap[secretKey]; ok {
			return RedactValue(sv)
		}
	}
	return val
}

// RedactValue shows the first 8 characters of a value then ***.
func RedactValue(s string) string {
	if len(s) <= 8 {
		return s + "***"
	}
	return s[:8] + "***"
}

// EnvAnnotation returns a display annotation for a config env value.
func EnvAnnotation(val string) string {
	if m := reSecretsDot.FindStringSubmatch(val); m != nil {
		return fmt.Sprintf("\u2190 secrets.%s", m[1])
	}
	if strings.Contains(val, ".project_root") {
		return "\u2190 project_root"
	}
	if strings.Contains(val, ".runtime_dir") {
		return "\u2190 runtime_dir"
	}
	if strings.Contains(val, "{{") {
		return "\u2190 template"
	}
	return fmt.Sprintf("= %s", val)
}

// SplitCommaList splits a comma-separated string into trimmed non-empty parts.
func SplitCommaList(s string) []string {
	var result []string
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}

// RemoveFromSlice returns a new slice with all occurrences of item removed.
func RemoveFromSlice(slice []string, item string) []string {
	var result []string
	for _, s := range slice {
		if s != item {
			result = append(result, s)
		}
	}
	return result
}

// ExpandHome expands a leading ~/ to the user's home directory.
func ExpandHome(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return filepath.Join(home, path[2:])
	}
	return path
}
