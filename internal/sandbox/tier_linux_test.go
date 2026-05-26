//go:build linux

package sandbox

import "testing"

// TestDetectKernelCapabilities_DoesNotPanic ensures the probe runs without panic
// and returns plausible values on the test host.
func TestDetectKernelCapabilities_DoesNotPanic(t *testing.T) {
	caps := DetectKernelCapabilities()
	t.Logf("KernelCapabilities: Landlock=%v ABI=%d Bwrap=%v Release=%s",
		caps.LandlockEnabled, caps.LandlockABI, caps.BwrapAvailable, caps.KernelRelease)
	// ABI should only be non-zero when Landlock is enabled.
	if !caps.LandlockEnabled && caps.LandlockABI != 0 {
		t.Errorf("LandlockABI=%d but LandlockEnabled=false", caps.LandlockABI)
	}
}

// TestComputeIsolationTier_LandlockABI4_NoPortRules verifies primary tier without port filtering.
func TestComputeIsolationTier_LandlockABI4_NoPortRules(t *testing.T) {
	caps := KernelCapabilities{LandlockEnabled: true, LandlockABI: 4}
	policy := Policy{}
	tier := ComputeIsolationTier(caps, policy)

	if tier.Tier != TierPrimary {
		t.Errorf("Tier = %q, want %q", tier.Tier, TierPrimary)
	}
	if tier.Backend != BackendLandlock {
		t.Errorf("Backend = %q, want %q", tier.Backend, BackendLandlock)
	}
	if tier.PortFiltering != PortFilteringUnsupported {
		t.Errorf("PortFiltering = %q, want %q", tier.PortFiltering, PortFilteringUnsupported)
	}
	if tier.Reason != "" {
		t.Errorf("primary tier should have empty Reason, got %q", tier.Reason)
	}
}

// TestComputeIsolationTier_LandlockABI4_WithPortRules verifies primary/strict on ABI≥4 with port rules.
func TestComputeIsolationTier_LandlockABI4_WithPortRules(t *testing.T) {
	caps := KernelCapabilities{LandlockEnabled: true, LandlockABI: 4}
	policy := Policy{AllowPorts: []int{443}}
	tier := ComputeIsolationTier(caps, policy)

	if tier.Tier != TierPrimary {
		t.Errorf("Tier = %q, want %q", tier.Tier, TierPrimary)
	}
	if tier.PortFiltering != PortFilteringStrict {
		t.Errorf("PortFiltering = %q, want %q", tier.PortFiltering, PortFilteringStrict)
	}
}

// TestComputeIsolationTier_LandlockABI5_WithPortRules verifies primary/strict on ABI5.
func TestComputeIsolationTier_LandlockABI5_WithPortRules(t *testing.T) {
	caps := KernelCapabilities{LandlockEnabled: true, LandlockABI: 5}
	policy := Policy{DenyPorts: []int{22}}
	tier := ComputeIsolationTier(caps, policy)

	if tier.Tier != TierPrimary {
		t.Errorf("Tier = %q, want %q", tier.Tier, TierPrimary)
	}
	if tier.PortFiltering != PortFilteringStrict {
		t.Errorf("PortFiltering = %q, want %q", tier.PortFiltering, PortFilteringStrict)
	}
}

// TestComputeIsolationTier_LandlockABI3_WithPortRules verifies degraded when ABI<4 and port rules present.
func TestComputeIsolationTier_LandlockABI3_WithPortRules(t *testing.T) {
	caps := KernelCapabilities{LandlockEnabled: true, LandlockABI: 3, KernelRelease: "6.2.0"}
	policy := Policy{AllowPorts: []int{443}}
	tier := ComputeIsolationTier(caps, policy)

	if tier.Tier != TierDegraded {
		t.Errorf("Tier = %q, want %q", tier.Tier, TierDegraded)
	}
	if tier.Backend != BackendLandlock {
		t.Errorf("Backend = %q, want %q", tier.Backend, BackendLandlock)
	}
	if tier.PortFiltering != PortFilteringDegraded {
		t.Errorf("PortFiltering = %q, want %q", tier.PortFiltering, PortFilteringDegraded)
	}
	if tier.Reason == "" {
		t.Error("degraded tier must have a non-empty Reason")
	}
}

// TestComputeIsolationTier_LandlockABI3_NoPortRules verifies primary tier on ABI3 without port rules.
func TestComputeIsolationTier_LandlockABI3_NoPortRules(t *testing.T) {
	caps := KernelCapabilities{LandlockEnabled: true, LandlockABI: 3}
	policy := Policy{}
	tier := ComputeIsolationTier(caps, policy)

	if tier.Tier != TierPrimary {
		t.Errorf("Tier = %q, want %q (Landlock without port rules is still primary)", tier.Tier, TierPrimary)
	}
}

// TestComputeIsolationTier_BwrapOnly_NoPortRules verifies degraded when only bwrap is available.
func TestComputeIsolationTier_BwrapOnly_NoPortRules(t *testing.T) {
	caps := KernelCapabilities{LandlockEnabled: false, BwrapAvailable: true}
	policy := Policy{}
	tier := ComputeIsolationTier(caps, policy)

	if tier.Tier != TierDegraded {
		t.Errorf("Tier = %q, want %q", tier.Tier, TierDegraded)
	}
	if tier.Backend != BackendBwrap {
		t.Errorf("Backend = %q, want %q", tier.Backend, BackendBwrap)
	}
	if tier.Reason == "" {
		t.Error("bwrap degraded tier must have a non-empty Reason")
	}
}

// TestComputeIsolationTier_BwrapOnly_WithPortRules verifies degraded port filtering
// is reported when port rules are configured but bwrap (the only backend) cannot
// enforce them.
func TestComputeIsolationTier_BwrapOnly_WithPortRules(t *testing.T) {
	caps := KernelCapabilities{LandlockEnabled: false, BwrapAvailable: true}
	policy := Policy{AllowPorts: []int{443}}
	tier := ComputeIsolationTier(caps, policy)

	if tier.Tier != TierDegraded {
		t.Errorf("Tier = %q, want %q", tier.Tier, TierDegraded)
	}
	if tier.PortFiltering != PortFilteringDegraded {
		t.Errorf("PortFiltering = %q, want %q", tier.PortFiltering, PortFilteringDegraded)
	}
	if tier.Reason == "" {
		t.Error("bwrap-with-port-rules degraded tier must carry a Reason")
	}
}

// TestComputeIsolationTier_NeitherAvailable verifies unavailable when nothing is present.
func TestComputeIsolationTier_NeitherAvailable(t *testing.T) {
	caps := KernelCapabilities{LandlockEnabled: false, BwrapAvailable: false}
	policy := Policy{}
	tier := ComputeIsolationTier(caps, policy)

	if tier.Tier != TierUnavailable {
		t.Errorf("Tier = %q, want %q", tier.Tier, TierUnavailable)
	}
	if tier.Backend != BackendNone {
		t.Errorf("Backend = %q, want %q", tier.Backend, BackendNone)
	}
}

// TestPlatformIsolationTier_ReturnsValidTier exercises the full platform detection path.
func TestPlatformIsolationTier_ReturnsValidTier(t *testing.T) {
	tier := PlatformIsolationTier(Policy{})
	validTiers := map[string]bool{TierPrimary: true, TierDegraded: true, TierUnavailable: true}
	if !validTiers[tier.Tier] {
		t.Errorf("PlatformIsolationTier returned invalid Tier %q", tier.Tier)
	}
	t.Logf("PlatformIsolationTier: %+v", tier)
}
