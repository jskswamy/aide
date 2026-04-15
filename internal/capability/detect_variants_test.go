package capability

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectEvidence_MatchesAllFiringVariants(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "uv.lock"), nil, 0o600)
	_ = os.WriteFile(filepath.Join(dir, "environment.yml"), nil, 0o600)

	c := Capability{
		Name: "python",
		Variants: []Variant{
			{Name: "uv", Markers: []Marker{{File: "uv.lock"}}},
			{Name: "conda", Markers: []Marker{{File: "environment.yml"}}},
			{Name: "poetry", Markers: []Marker{{File: "poetry.lock"}}},
		},
	}
	ev := DetectEvidence(os.DirFS(dir), c)
	wantVariants := map[string]bool{"uv": true, "conda": true}
	if len(ev.Variants) != len(wantVariants) {
		t.Fatalf("len(Variants) = %d, want %d (%v)", len(ev.Variants), len(wantVariants), ev.Variants)
	}
	for _, v := range ev.Variants {
		if !wantVariants[v] {
			t.Errorf("unexpected variant detected: %s", v)
		}
	}
}

func TestDetectEvidence_NoMatches_EmptyVariants(t *testing.T) {
	dir := t.TempDir()
	c := Capability{
		Name: "python",
		Variants: []Variant{
			{Name: "uv", Markers: []Marker{{File: "uv.lock"}}},
		},
	}
	ev := DetectEvidence(os.DirFS(dir), c)
	if len(ev.Variants) != 0 {
		t.Errorf("len(Variants) = %d, want 0", len(ev.Variants))
	}
}

// A variant with ALL markers required — missing one disqualifies the variant.
func TestDetectEvidence_AllMarkersRequiredPerVariant(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "uv.lock"), nil, 0o600)
	// Variant "uv" requires BOTH uv.lock AND pyproject.toml substring.
	// Only uv.lock is present, so uv must NOT be selected.
	c := Capability{
		Name: "python",
		Variants: []Variant{
			{Name: "uv", Markers: []Marker{
				{File: "uv.lock"},
				{Contains: ContainsSpec{File: "pyproject.toml", Pattern: "[tool.uv]"}},
			}},
		},
	}
	ev := DetectEvidence(os.DirFS(dir), c)
	if len(ev.Variants) != 0 {
		t.Errorf("variant selected despite missing marker; got %v", ev.Variants)
	}
	// Matches should still be recorded — two MarkerMatch entries (one matched, one not).
	if len(ev.Matches) != 2 {
		t.Errorf("len(Matches) = %d, want 2", len(ev.Matches))
	}
}

// Variants with zero markers are never selected by detection.
func TestDetectEvidence_SkipsVariantsWithoutMarkers(t *testing.T) {
	dir := t.TempDir()
	c := Capability{
		Name: "python",
		Variants: []Variant{
			{Name: "venv"}, // no markers — skipped
		},
	}
	ev := DetectEvidence(os.DirFS(dir), c)
	if len(ev.Variants) != 0 {
		t.Errorf("variant with no markers was selected: %v", ev.Variants)
	}
}

// Result Variants list is sorted for deterministic digest downstream.
func TestDetectEvidence_SortedVariants(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "uv.lock"), nil, 0o600)
	_ = os.WriteFile(filepath.Join(dir, "environment.yml"), nil, 0o600)
	c := Capability{
		Name: "python",
		Variants: []Variant{
			// Declared in non-sorted order: uv before conda.
			{Name: "uv", Markers: []Marker{{File: "uv.lock"}}},
			{Name: "conda", Markers: []Marker{{File: "environment.yml"}}},
		},
	}
	ev := DetectEvidence(os.DirFS(dir), c)
	if len(ev.Variants) != 2 {
		t.Fatalf("len(Variants) = %d, want 2", len(ev.Variants))
	}
	if ev.Variants[0] != "conda" || ev.Variants[1] != "uv" {
		t.Errorf("Variants = %v, want [conda uv] (sorted)", ev.Variants)
	}
}
