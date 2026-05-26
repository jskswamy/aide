package sandbox

// Tier level constants: TierPrimary means full enforcement, TierDegraded means
// partial (policy-declared restrictions may be silently unenforced), and
// TierUnavailable means no OS-level sandbox is active.
const (
	TierPrimary     = "primary"
	TierDegraded    = "degraded"
	TierUnavailable = "unavailable"
)

// Backend constants identify which sandboxing mechanism is active.
const (
	BackendLandlock = "landlock"
	BackendBwrap    = "bwrap"
	BackendSeatbelt = "seatbelt"
	BackendNone     = "none"
)

// PortFiltering constants describe TCP port enforcement quality.
const (
	PortFilteringStrict      = "strict"
	PortFilteringDegraded    = "degraded"
	PortFilteringUnsupported = "unsupported"
)

// IsolationTier captures the effective OS-level sandbox strength for a launch.
// Computed once per invocation and read by status, banner, and diagnose
// surfaces so they all report the same thing.
//
// Invariants enforced by ComputeIsolationTier (Linux) / tier_darwin (macOS):
//   - Primary     ⇒ Backend ∈ {Landlock, Seatbelt}
//   - Degraded    ⇒ Reason != ""
//   - Unavailable ⇒ Backend == None
//
// KernelABI is the highest Landlock ABI exposed by the running kernel; 0 means
// Landlock is absent or we're not on Linux. PortFiltering reports whether
// configured port rules are enforced ("strict"), silently downgraded
// ("degraded"), or simply not applicable ("unsupported").
type IsolationTier struct {
	Tier          string
	Backend       string
	Reason        string
	KernelABI     int
	PortFiltering string
}
