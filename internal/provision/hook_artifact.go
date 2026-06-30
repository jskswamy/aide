package provision

import (
	"crypto/sha256"
	"fmt"
	"strings"
)

// HookArtifact describes the naming and ownership signature of a provider's
// hook artifact (file or directory). All three providers (copilot, gemini,
// hermes) derive artifact names from sha256(command)[:8] with different
// prefixes and extensions; HookArtifact centralizes this logic.
//
// Example: copilot uses {"aide-", ".json"}, gemini uses {"aide_", ".sh"},
// hermes uses {"aide_", ""} (directory, no extension).
type HookArtifact struct {
	Prefix string // e.g. "aide-" or "aide_"
	Ext    string // e.g. ".json", ".sh", or "" (for directories)
}

// Name computes the artifact name for command, using the sha256[:8] prefix.
// For "rtk hook copilot" with prefix="aide-" and ext=".json",
// returns "aide-<hash>.json". For directories (ext=""), returns "aide-<hash>".
func (a HookArtifact) Name(command string) string {
	sum := sha256.Sum256([]byte(command))
	return fmt.Sprintf("%s%x%s", a.Prefix, sum[:8], a.Ext)
}

// Owns reports whether name matches this artifact's prefix/ext pattern.
// It handles both file artifacts (with extensions) and directory artifacts
// (where ext is empty). Examples:
//
//	HookArtifact{"aide-", ".json"}.Owns("aide-a1b2c3d4.json") // true
//	HookArtifact{"aide_", ".sh"}.Owns("aide_a1b2c3d4.sh")     // true
//	HookArtifact{"aide_", ""}.Owns("aide_a1b2c3d4")           // true (dir)
//	HookArtifact{"aide-", ".json"}.Owns("aide_a1b2c3d4.json") // false
func (a HookArtifact) Owns(name string) bool {
	if !strings.HasPrefix(name, a.Prefix) {
		return false
	}
	if a.Ext == "" {
		// For directory artifacts (no extension), we only check the prefix.
		// The remainder should be a 16-char hex string (sha256[:8] in hex).
		remainder := name[len(a.Prefix):]
		return len(remainder) == 16 && isHexString(remainder)
	}
	// For file artifacts, check both prefix and extension.
	if !strings.HasSuffix(name, a.Ext) {
		return false
	}
	// Extract the hash portion (between prefix and extension)
	// and verify it's valid hex. Format: prefix + 16-char hex + extension.
	hashStart := len(a.Prefix)
	hashEnd := len(name) - len(a.Ext)
	if hashEnd-hashStart != 16 {
		return false // hash portion must be exactly 16 hex chars
	}
	return isHexString(name[hashStart:hashEnd])
}

// isHexString reports whether s contains only hex digits [0-9a-f].
func isHexString(s string) bool {
	for _, ch := range s {
		if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'f') {
			return false
		}
	}
	return true
}
