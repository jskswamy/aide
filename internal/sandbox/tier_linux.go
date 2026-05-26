//go:build linux

package sandbox

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	ll "github.com/landlock-lsm/go-landlock/landlock/syscall"
)

// KernelCapabilities records what the current Linux kernel can enforce.
type KernelCapabilities struct {
	LandlockEnabled bool
	LandlockABI     int
	BwrapAvailable  bool
	KernelRelease   string
}

// DetectKernelCapabilities probes — it does not apply any restriction.
func DetectKernelCapabilities() KernelCapabilities {
	caps := KernelCapabilities{}

	if data, err := os.ReadFile("/sys/kernel/security/lsm"); err == nil {
		for lsm := range strings.SplitSeq(strings.TrimSpace(string(data)), ",") {
			if strings.TrimSpace(lsm) == "landlock" {
				caps.LandlockEnabled = true
				break
			}
		}
	}

	if caps.LandlockEnabled {
		caps.LandlockABI = probeLandlockABI()
	}

	if _, err := exec.LookPath("bwrap"); err == nil {
		caps.BwrapAvailable = true
	}

	if rel, err := os.ReadFile("/proc/sys/kernel/osrelease"); err == nil {
		caps.KernelRelease = strings.TrimSpace(string(rel))
	}

	return caps
}

// probeLandlockABI returns 0 on error — conservative when the kernel claims
// Landlock but won't report a version.
func probeLandlockABI() int {
	v, err := ll.LandlockGetABIVersion()
	if err != nil {
		return 0
	}
	return v
}

// ComputeIsolationTier maps (kernel caps, policy) → IsolationTier.
//
//	ABI ≥ 4                                  → primary  / strict (or unsupported if no port rules)
//	ABI 1-3 with port rules or network=none  → degraded / degraded
//	ABI 1-3 otherwise                        → primary  / unsupported
//	bwrap only, port rules                   → degraded / degraded
//	bwrap only, no port rules                → degraded / unsupported
//	neither                                  → unavailable / none
func ComputeIsolationTier(caps KernelCapabilities, policy Policy) IsolationTier {
	hasPortRules := len(policy.AllowPorts) > 0 || len(policy.DenyPorts) > 0
	// network=none needs Landlock ABI≥4 (ConnectTCP) for the same reason
	// per-port allow-lists do — BestEffort silently strips network gating on
	// older ABIs.
	requiresNetworkABI4 := hasPortRules || policy.Network == NetworkNone

	if caps.LandlockEnabled {
		if caps.LandlockABI >= 4 {
			pf := PortFilteringStrict
			if !hasPortRules {
				pf = PortFilteringUnsupported
			}
			return IsolationTier{
				Tier:          TierPrimary,
				Backend:       BackendLandlock,
				KernelABI:     caps.LandlockABI,
				PortFiltering: pf,
			}
		}
		// ABI < 4 cannot enforce per-port TCP filtering or network=none.
		if requiresNetworkABI4 {
			// Bwrap can enforce network=none via --unshare-net, but cannot
			// do per-port TCP filtering. Prefer the bwrap backend in the
			// narrow case where the kernel's Landlock gap is exactly
			// "network=none" and bwrap is installed; otherwise stay on
			// Landlock-degraded so the caller surfaces a hard error rather
			// than silently mis-enforcing the policy.
			if !hasPortRules && policy.Network == NetworkNone && caps.BwrapAvailable {
				return IsolationTier{
					Tier:          TierDegraded,
					Backend:       BackendBwrap,
					KernelABI:     caps.LandlockABI,
					Reason:        fmt.Sprintf("kernel %s (Landlock ABI %d < 4): network=none not enforceable by Landlock; using bubblewrap (--unshare-net)", caps.KernelRelease, caps.LandlockABI),
					PortFiltering: PortFilteringUnsupported,
				}
			}
			reason := fmt.Sprintf("kernel %s (Landlock ABI %d < 4): TCP port filtering not enforced", caps.KernelRelease, caps.LandlockABI)
			if !hasPortRules && policy.Network == NetworkNone {
				reason = fmt.Sprintf("kernel %s (Landlock ABI %d < 4): network=none not enforced (requires ABI >= 4)", caps.KernelRelease, caps.LandlockABI)
			}
			return IsolationTier{
				Tier:          TierDegraded,
				Backend:       BackendLandlock,
				KernelABI:     caps.LandlockABI,
				Reason:        reason,
				PortFiltering: PortFilteringDegraded,
			}
		}
		return IsolationTier{
			Tier:          TierPrimary,
			Backend:       BackendLandlock,
			KernelABI:     caps.LandlockABI,
			PortFiltering: PortFilteringUnsupported,
		}
	}

	if caps.BwrapAvailable {
		reason := "no Landlock in kernel: using bubblewrap for filesystem isolation"
		pf := PortFilteringUnsupported
		if hasPortRules {
			reason = "bwrap fallback: TCP port filtering not enforced (Landlock absent)"
			pf = PortFilteringDegraded
		}
		return IsolationTier{
			Tier:          TierDegraded,
			Backend:       BackendBwrap,
			Reason:        reason,
			PortFiltering: pf,
		}
	}

	return IsolationTier{
		Tier:    TierUnavailable,
		Backend: BackendNone,
		Reason:  "no Landlock and no bwrap: OS-level isolation unavailable",
	}
}

// PlatformIsolationTier probes the running kernel and returns the effective
// IsolationTier for the given policy.
func PlatformIsolationTier(policy Policy) IsolationTier {
	return ComputeIsolationTier(DetectKernelCapabilities(), policy)
}
