// Variant types and marker-matching logic for variant-aware capabilities.
//
// A Variant is a refinement of a Capability — a specific toolchain
// implementation (e.g. uv within python) with its own detection
// markers and path/env contributions. Markers describe how to detect
// the variant by scanning the project root; Match is the single
// boolean predicate.

package capability

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// markerMaxReadSize caps the bytes read from a project file when
// evaluating a Contains marker. Larger files are truncated for the
// purposes of matching only; we never read past this boundary.
const markerMaxReadSize = 64 * 1024

// Variant is a refinement of a Capability: a specific toolchain
// implementation (e.g. uv within python) with its own markers and
// path/env contributions.
type Variant struct {
	Name        string
	Description string
	Markers     []Marker
	Readable    []string
	Writable    []string
	EnvAllow    []string
	EnableGuard []string
}

// Marker is a detection rule. Exactly one of File, Contains, or
// GlobPath must be set.
type Marker struct {
	File     string
	Contains ContainsSpec
	GlobPath string
}

// ContainsSpec describes a substring check within a file.
type ContainsSpec struct {
	File    string
	Pattern string
}

// Validate ensures exactly one of Marker's three field groups is set.
// Contains requires both File and Pattern to be non-empty.
func (m Marker) Validate() error {
	n := 0
	if m.File != "" {
		n++
	}
	if m.Contains.File != "" || m.Contains.Pattern != "" {
		if m.Contains.File == "" || m.Contains.Pattern == "" {
			return errors.New("marker: Contains requires both File and Pattern")
		}
		n++
	}
	if m.GlobPath != "" {
		n++
	}
	if n != 1 {
		return errors.New("marker: exactly one of File, Contains, or GlobPath must be set")
	}
	return nil
}

// Match reports whether the marker matches within projectRoot.
func (m Marker) Match(projectRoot string) bool {
	if m.File != "" {
		fi, err := os.Stat(filepath.Join(projectRoot, m.File))
		return err == nil && !fi.IsDir()
	}
	if m.Contains.File != "" {
		return containsInBoundedFile(
			filepath.Join(projectRoot, m.Contains.File),
			m.Contains.Pattern,
		)
	}
	if m.GlobPath != "" {
		matches, _ := filepath.Glob(filepath.Join(projectRoot, m.GlobPath))
		return len(matches) > 0
	}
	return false
}

// MatchSummary returns a short human-readable label for a marker,
// suitable for consent prompts and log lines.
func (m Marker) MatchSummary() string {
	switch {
	case m.File != "":
		return m.File
	case m.Contains.File != "":
		return m.Contains.File + ":" + m.Contains.Pattern
	case m.GlobPath != "":
		return m.GlobPath
	}
	return "<empty-marker>"
}

// containsInBoundedFile reads up to markerMaxReadSize bytes from path
// and reports whether pattern appears as a substring. Unreadable files
// yield false; pattern matches truncated to the read boundary are
// accepted intentionally (matches Task 3 design doc).
func containsInBoundedFile(path, pattern string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()
	buf := make([]byte, markerMaxReadSize)
	n, _ := f.Read(buf)
	return strings.Contains(string(buf[:n]), pattern)
}
