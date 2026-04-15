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

// globContainsMaxFiles caps the number of files a single GlobContains
// marker will scan. Prevents DoS via a wildcard that matches a huge
// directory.
const globContainsMaxFiles = 50

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

// Marker is a detection rule. Exactly one of File, Contains,
// GlobPath, DirExists, or GlobContains must be set.
type Marker struct {
	File         string
	Contains     ContainsSpec
	GlobPath     string
	DirExists    string           // directory at this relative path exists
	GlobContains GlobContainsSpec // any file matching Glob contains Pattern
}

// ContainsSpec describes a substring check within a file.
type ContainsSpec struct {
	File    string
	Pattern string
}

// GlobContainsSpec describes a combined glob + substring check.
// Useful for "any yaml at depth-0 or depth-1 contains apiVersion:".
type GlobContainsSpec struct {
	Glob    string
	Pattern string
}

// Validate ensures exactly one of Marker's field groups is set.
// Contains and GlobContains each require both of their sub-fields.
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
	if m.DirExists != "" {
		n++
	}
	if m.GlobContains.Glob != "" || m.GlobContains.Pattern != "" {
		if m.GlobContains.Glob == "" || m.GlobContains.Pattern == "" {
			return errors.New("marker: GlobContains requires both Glob and Pattern")
		}
		n++
	}
	if n != 1 {
		return errors.New("marker: exactly one of File, Contains, GlobPath, DirExists, or GlobContains must be set")
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
	if m.DirExists != "" {
		fi, err := os.Stat(filepath.Join(projectRoot, m.DirExists))
		return err == nil && fi.IsDir()
	}
	if m.GlobContains.Glob != "" {
		matches, _ := filepath.Glob(filepath.Join(projectRoot, m.GlobContains.Glob))
		if len(matches) > globContainsMaxFiles {
			matches = matches[:globContainsMaxFiles]
		}
		for _, p := range matches {
			if containsInBoundedFile(p, m.GlobContains.Pattern) {
				return true
			}
		}
		return false
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
	case m.DirExists != "":
		return m.DirExists + "/"
	case m.GlobContains.Glob != "":
		return m.GlobContains.Glob + ":" + m.GlobContains.Pattern
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

// AnyMarkerMatches reports whether at least one marker in ms matches
// somewhere under projectRoot. An empty list returns false. Use for
// top-level Capability.Markers (presence-of-evidence semantics).
func AnyMarkerMatches(projectRoot string, ms []Marker) bool {
	for _, m := range ms {
		if m.Match(projectRoot) {
			return true
		}
	}
	return false
}

// AllMarkersMatch reports whether every marker in ms matches under
// projectRoot. An empty list returns false. Use for Variant.Markers
// (specificity-of-evidence semantics).
func AllMarkersMatch(projectRoot string, ms []Marker) bool {
	if len(ms) == 0 {
		return false
	}
	for _, m := range ms {
		if !m.Match(projectRoot) {
			return false
		}
	}
	return true
}
