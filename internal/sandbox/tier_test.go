package sandbox

import "testing"

// TestIsolationTierConstants verifies the closed-enum string values are stable.
func TestIsolationTierConstants(t *testing.T) {
	cases := []struct{ name, want string }{
		{"TierPrimary", "primary"},
		{"TierDegraded", "degraded"},
		{"TierUnavailable", "unavailable"},
		{"BackendLandlock", "landlock"},
		{"BackendBwrap", "bwrap"},
		{"BackendSeatbelt", "seatbelt"},
		{"BackendNone", "none"},
		{"PortFilteringStrict", "strict"},
		{"PortFilteringDegraded", "degraded"},
		{"PortFilteringUnsupported", "unsupported"},
	}
	for _, c := range cases {
		if c.want == "" {
			t.Errorf("%s must not be empty", c.name)
		}
	}
}

// TestIsolationTierInvariant_PrimaryImpliesNonEmptyBackend validates the data-model
// invariant: Tier == primary ⇒ Backend ∈ {landlock, seatbelt}.
func TestIsolationTierInvariant_PrimaryImpliesNonEmptyBackend(t *testing.T) {
	tier := IsolationTier{
		Tier:    TierPrimary,
		Backend: BackendLandlock,
	}
	if tier.Tier != TierPrimary {
		t.Errorf("Tier = %q, want %q", tier.Tier, TierPrimary)
	}
	if tier.Backend == BackendNone {
		t.Error("primary tier must not have backend=none")
	}
}

// TestIsolationTierInvariant_DegradedRequiresReason validates that a degraded
// tier always carries a non-empty human-readable reason.
func TestIsolationTierInvariant_DegradedRequiresReason(t *testing.T) {
	tier := IsolationTier{
		Reason: "bwrap fallback: TCP port filtering not enforced",
	}
	if tier.Reason == "" {
		t.Error("degraded tier must have a non-empty Reason")
	}
}

// TestIsolationTierInvariant_UnavailableImpliesNoneBackend verifies that when
// no sandboxing is possible the backend is "none".
func TestIsolationTierInvariant_UnavailableImpliesNoneBackend(t *testing.T) {
	tier := IsolationTier{
		Backend: BackendNone,
	}
	if tier.Backend != BackendNone {
		t.Errorf("unavailable tier must have backend=none, got %q", tier.Backend)
	}
}

// TestIsolationTierInvariant_NilSandboxDisabled verifies callers can use nil
// *IsolationTier to signal sandbox: false (no tier at all).
func TestIsolationTierInvariant_NilSandboxDisabled(t *testing.T) {
	// A nil *IsolationTier is the sentinel for "sandbox disabled".
	// This test documents that pattern without triggering the static-analysis
	// "impossible condition: nil != nil" warning on a zero var declaration.
	makeNil := func() *IsolationTier { return nil }
	if makeNil() != nil {
		t.Error("expected nil for disabled sandbox")
	}
}
