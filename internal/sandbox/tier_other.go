//go:build !darwin && !linux

package sandbox

// PlatformIsolationTier returns unavailable on unsupported platforms.
func PlatformIsolationTier(_ Policy) IsolationTier {
	return IsolationTier{
		Tier:    TierUnavailable,
		Backend: BackendNone,
		Reason:  "sandboxing not available on this platform",
	}
}
