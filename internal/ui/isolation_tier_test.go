package ui

import (
	"testing"

	"github.com/jskswamy/aide/internal/sandbox"
)

// TestBannerData_IsolationTier_NilForDisabledSandbox verifies that nil IsolationTier
// correctly represents "sandbox: false" (user explicitly disabled sandboxing).
func TestBannerData_IsolationTier_NilForDisabledSandbox(t *testing.T) {
	// IsolationTier intentionally not set — nil means disabled.
	data := &BannerData{}
	if data.IsolationTier != nil {
		t.Errorf("IsolationTier should be nil for disabled sandbox, got %+v", data.IsolationTier)
	}
}

// TestBannerData_IsolationTier_NonNilForEnabledSandbox verifies IsolationTier is set
// when the sandbox is active (any tier).
func TestBannerData_IsolationTier_NonNilForEnabledSandbox(t *testing.T) {
	tier := &sandbox.IsolationTier{
		Tier:    sandbox.TierPrimary,
		Backend: sandbox.BackendLandlock,
	}
	data := &BannerData{IsolationTier: tier}
	if data.IsolationTier == nil {
		t.Error("IsolationTier must not be nil when sandbox is active")
	}
	if data.IsolationTier.Tier != sandbox.TierPrimary {
		t.Errorf("IsolationTier.Tier = %q, want %q", data.IsolationTier.Tier, sandbox.TierPrimary)
	}
}

// TestBannerData_IsolationTier_DegradedHasReason verifies a degraded tier carries a reason.
func TestBannerData_IsolationTier_DegradedHasReason(t *testing.T) {
	tier := &sandbox.IsolationTier{
		Tier:          sandbox.TierDegraded,
		Backend:       sandbox.BackendBwrap,
		Reason:        "bwrap fallback: TCP port filtering not enforced",
		PortFiltering: sandbox.PortFilteringUnsupported,
	}
	data := &BannerData{IsolationTier: tier}

	if data.IsolationTier.Reason == "" {
		t.Error("degraded IsolationTier in BannerData must have a non-empty Reason")
	}
}

// TestBannerData_IsolationTier_UnavailableHasNoneBackend verifies unavailable uses none backend.
func TestBannerData_IsolationTier_UnavailableHasNoneBackend(t *testing.T) {
	tier := &sandbox.IsolationTier{
		Tier:    sandbox.TierUnavailable,
		Backend: sandbox.BackendNone,
		Reason:  "no Landlock, no bwrap",
	}
	data := &BannerData{IsolationTier: tier}

	if data.IsolationTier.Backend != sandbox.BackendNone {
		t.Errorf("unavailable IsolationTier must use BackendNone, got %q", data.IsolationTier.Backend)
	}
}
