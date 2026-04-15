// Package capability's project-detection surface.
//
// DetectProject walks the built-in registry and returns the names of
// capabilities whose top-level Markers match under fsys. Intended for
// aide cap suggest and launcher startup hints.
//
// Suggestions are returned in a deterministic order (the registry's
// sorted key order), so callers can rely on stable output for goldens
// and human display.

package capability

import (
	"io/fs"
	"sort"
)

// DetectProject returns built-in capability names whose top-level
// Markers match somewhere under fsys. fsys is typically
// os.DirFS(projectRoot) in production.
func DetectProject(fsys fs.FS) []string {
	b := Builtins()
	names := make([]string, 0, len(b))
	for name := range b {
		names = append(names, name)
	}
	sort.Strings(names)

	var out []string
	for _, name := range names {
		c := b[name]
		if len(c.Markers) == 0 {
			continue
		}
		if AnyMarkerMatches(fsys, c.Markers) {
			out = append(out, name)
		}
	}
	return out
}
