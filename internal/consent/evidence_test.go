package consent

import "testing"

func TestEvidence_Digest_Deterministic(t *testing.T) {
	e1 := Evidence{
		Variants: []string{"uv", "conda"},
		Matches: []MarkerMatch{
			{Kind: "file", Target: "uv.lock", Matched: true},
			{Kind: "file", Target: "environment.yml", Matched: true},
		},
	}
	e2 := Evidence{
		Variants: []string{"conda", "uv"},
		Matches: []MarkerMatch{
			{Kind: "file", Target: "environment.yml", Matched: true},
			{Kind: "file", Target: "uv.lock", Matched: true},
		},
	}
	if e1.Digest() != e2.Digest() {
		t.Errorf("digest mismatch across equivalent orderings:\n%s\n%s",
			e1.Digest(), e2.Digest())
	}
}

func TestEvidence_Digest_ChangesOnMatchFlip(t *testing.T) {
	base := Evidence{
		Variants: []string{"uv"},
		Matches: []MarkerMatch{
			{Kind: "file", Target: "uv.lock", Matched: true},
		},
	}
	flipped := Evidence{
		Variants: []string{"uv"},
		Matches: []MarkerMatch{
			{Kind: "file", Target: "uv.lock", Matched: false},
		},
	}
	if base.Digest() == flipped.Digest() {
		t.Errorf("digest unchanged after match flip")
	}
}

func TestEvidence_Digest_ChangesOnVariantSetChange(t *testing.T) {
	base := Evidence{Variants: []string{"uv"}, Matches: nil}
	extended := Evidence{Variants: []string{"uv", "conda"}, Matches: nil}
	if base.Digest() == extended.Digest() {
		t.Errorf("digest unchanged after variant added")
	}
}

func TestEvidence_Digest_NoCollisionAcrossDelimiters(t *testing.T) {
	a := Evidence{
		Variants: nil,
		Matches: []MarkerMatch{
			{Kind: "file", Target: "a\x00b", Matched: true},
		},
	}
	b := Evidence{
		Variants: nil,
		Matches: []MarkerMatch{
			{Kind: "file\x00a", Target: "b", Matched: true},
		},
	}
	if a.Digest() == b.Digest() {
		t.Errorf("digest collided across delimiter-bearing targets: %s", a.Digest())
	}
}

func TestEvidence_Digest_IdenticalInputs(t *testing.T) {
	e := Evidence{
		Variants: []string{"uv", "conda"},
		Matches: []MarkerMatch{
			{Kind: "file", Target: "uv.lock", Matched: true},
			{Kind: "file", Target: "environment.yml", Matched: true},
		},
	}
	first := e.Digest()
	second := e.Digest()
	if first != second {
		t.Errorf("same Evidence produced different digests across calls: %s vs %s", first, second)
	}
}

func TestEvidence_Digest_EmptyIsStable(t *testing.T) {
	a := Evidence{}.Digest()
	b := Evidence{}.Digest()
	if a != b {
		t.Errorf("empty Evidence digest unstable: %s vs %s", a, b)
	}
	if len(a) != 64 { // hex-encoded SHA-256
		t.Errorf("empty digest length = %d, want 64", len(a))
	}
}
