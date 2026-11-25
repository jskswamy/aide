// Package schemas embeds the JSON Schema (Draft 2020-12) definitions for
// aide's YAML configuration files and provides helpers for yaml-language-server
// modeline headers.
package schemas

import (
	"bytes"
	"embed"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
)

// FS embeds all *.schema.json files in this directory.
//
//go:embed *.schema.json
var FS embed.FS

// Kind identifies one of aide's five YAML configuration file shapes.
type Kind string

// Supported schema kinds.
const (
	KindConfig       Kind = "config"
	KindProfile      Kind = "profile"
	KindSandbox      Kind = "sandbox"
	KindProvisioning Kind = "provisioning"
	KindProject      Kind = "project"
)

// DefaultBaseURL is the hosted GitHub raw base URL used in default modelines.
const DefaultBaseURL = "https://raw.githubusercontent.com/jskswamy/aide/main/schemas"

const modelinePrefix = "# yaml-language-server: $schema="
const yamlLSCommentPrefix = "# yaml-language-server:"

var utf8BOM = []byte{0xef, 0xbb, 0xbf}

var allKinds = []Kind{
	KindConfig,
	KindProfile,
	KindSandbox,
	KindProvisioning,
	KindProject,
}

// AllKinds returns all supported schema kinds in canonical order.
func AllKinds() []Kind {
	out := make([]Kind, len(allKinds))
	copy(out, allKinds)
	return out
}

// ParseKind converts a CLI/string name into a Kind.
func ParseKind(s string) (Kind, bool) {
	k := Kind(strings.ToLower(strings.TrimSpace(s)))
	for _, valid := range allKinds {
		if k == valid {
			return k, true
		}
	}
	return "", false
}

// Filename returns the schema filename for k (e.g. "profile.schema.json").
func Filename(k Kind) string {
	return string(k) + ".schema.json"
}

// KindForSplitRel maps a slash-separated relative path under ~/.config/aide/
// (such as "config.yaml", "sandbox.yaml", "provisioning.yaml", or
// "profiles/work.yaml") to its corresponding schema Kind.
func KindForSplitRel(rel string) Kind {
	switch {
	case rel == "sandbox.yaml":
		return KindSandbox
	case rel == "provisioning.yaml":
		return KindProvisioning
	case strings.HasPrefix(rel, "profiles/"):
		return KindProfile
	default:
		return KindConfig
	}
}

// Bytes returns the embedded JSON Schema bytes for k.
func Bytes(k Kind) ([]byte, error) {
	if _, ok := ParseKind(string(k)); !ok {
		return nil, fmt.Errorf("unknown schema kind %q", k)
	}
	return FS.ReadFile(Filename(k))
}

// DefaultURL returns the canonical hosted raw GitHub URL for k.
func DefaultURL(k Kind) string {
	return DefaultBaseURL + "/" + Filename(k)
}

// FileURI returns a properly percent-encoded file:// URI for an absolute path.
func FileURI(absPath string) string {
	p := filepath.ToSlash(absPath)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return (&url.URL{Scheme: "file", Path: p}).String()
}

// Modeline formats a yaml-language-server schema comment for target.
func Modeline(target string) string {
	return modelinePrefix + target
}

// HasModeline reports whether the leading comment block of data already
// contains a `# yaml-language-server:` directive.
func HasModeline(data []byte) bool {
	_, _, found := findLeadingModeline(data)
	return found
}

// ExtractModeline returns the raw leading `# yaml-language-server:...` line
// (without trailing newline) if present in the leading comment block of data.
func ExtractModeline(data []byte) (string, bool) {
	start, end, found := findLeadingModeline(data)
	if !found {
		return "", false
	}
	line := strings.TrimRight(string(data[start:end]), "\r\n")
	return line, true
}

// EnsureModeline returns data unchanged if its leading comment block already
// contains a `# yaml-language-server:` directive; otherwise it prepends
// `Modeline(target) + "\n"` at line 1 (above any leading `---` document marker).
func EnsureModeline(data []byte, target string) []byte {
	if HasModeline(data) {
		return data
	}
	return prependLine(data, Modeline(target))
}

// EnsureModelineLine ensures exactLine (e.g. from ExtractModeline) is present
// if data does not already have a `# yaml-language-server:` directive.
func EnsureModelineLine(data []byte, exactLine string) []byte {
	if exactLine == "" || HasModeline(data) {
		return data
	}
	return prependLine(data, exactLine)
}

// SetModeline replaces an existing `# yaml-language-server:` line in the
// leading comment block of data with `Modeline(target)`, or prepends one if
// absent.
func SetModeline(data []byte, target string) []byte {
	start, end, found := findLeadingModeline(data)
	if !found {
		return prependLine(data, Modeline(target))
	}
	nl := "\n"
	if end >= 2 && data[end-2] == '\r' && data[end-1] == '\n' {
		nl = "\r\n"
	} else if end == len(data) && (end == 0 || data[end-1] != '\n') {
		nl = ""
	}
	var buf bytes.Buffer
	buf.Grow(len(data) + len(target))
	buf.Write(data[:start])
	buf.WriteString(Modeline(target))
	buf.WriteString(nl)
	buf.Write(data[end:])
	return buf.Bytes()
}

func prependLine(data []byte, line string) []byte {
	var buf bytes.Buffer
	buf.Grow(len(line) + 1 + len(data))
	if bytes.HasPrefix(data, utf8BOM) {
		buf.Write(utf8BOM)
		data = data[len(utf8BOM):]
	}
	buf.WriteString(line)
	buf.WriteByte('\n')
	buf.Write(data)
	return buf.Bytes()
}

// findLeadingModeline scans lines from the top of data while they are blank or
// YAML comments (#...). It returns the byte offsets [start, end) (including
// trailing newline if any) of the first `# yaml-language-server:` line found
// before the first non-comment, non-blank line.
func findLeadingModeline(data []byte) (start, end int, found bool) {
	pos := 0
	if bytes.HasPrefix(data, utf8BOM) {
		pos = len(utf8BOM)
	}
	for pos < len(data) {
		lineStart := pos
		idx := bytes.IndexByte(data[pos:], '\n')
		var lineEnd int
		if idx == -1 {
			lineEnd = len(data)
		} else {
			lineEnd = pos + idx + 1
		}
		trimmed := strings.TrimSpace(string(data[lineStart:lineEnd]))
		if trimmed == "" {
			pos = lineEnd
			continue
		}
		if !strings.HasPrefix(trimmed, "#") {
			return 0, 0, false
		}
		if strings.HasPrefix(trimmed, yamlLSCommentPrefix) {
			return lineStart, lineEnd, true
		}
		pos = lineEnd
	}
	return 0, 0, false
}
