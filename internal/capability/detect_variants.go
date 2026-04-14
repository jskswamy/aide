// Detector for variant-aware capabilities.
//
// DetectEvidence walks a capability's Variants, evaluates every
// marker against the project root, and returns a consent.Evidence
// naming the variants whose markers ALL fired. Callers pair the
// evidence with a consent.Store and a Prompter (see select.go in
// Task 7) to decide which variants actually activate.

package capability

import (
	"sort"

	"github.com/jskswamy/aide/internal/consent"
)

// DetectEvidence runs every variant's markers against projectRoot and
// returns evidence naming the variants whose markers ALL fired plus
// the full set of match results (deterministically ordered).
//
// A variant is considered selected only when every one of its markers
// matches. Variants with no markers are skipped — they must be pinned
// via config or flag.
//
// The returned Evidence.Variants is sorted ascending so that the
// downstream digest and consent-key computations are stable across
// process runs.
func DetectEvidence(cap Capability, projectRoot string) consent.Evidence {
	selected := make([]string, 0, len(cap.Variants))
	matches := make([]consent.MarkerMatch, 0)
	for _, v := range cap.Variants {
		if len(v.Markers) == 0 {
			continue
		}
		allMatch := true
		for _, m := range v.Markers {
			ok := m.Match(projectRoot)
			matches = append(matches, consent.MarkerMatch{
				Kind:    markerKind(m),
				Target:  m.MatchSummary(),
				Matched: ok,
			})
			if !ok {
				allMatch = false
			}
		}
		if allMatch {
			selected = append(selected, v.Name)
		}
	}
	sort.Strings(selected)
	return consent.Evidence{Variants: selected, Matches: matches}
}

func markerKind(m Marker) string {
	switch {
	case m.File != "":
		return "file"
	case m.Contains.File != "":
		return "contains"
	case m.GlobPath != "":
		return "glob"
	}
	return ""
}
