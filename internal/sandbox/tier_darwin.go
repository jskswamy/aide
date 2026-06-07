//go:build darwin

package sandbox

// PlatformIsolationTier returns the macOS isolation tier. Seatbelt is
// always available on macOS, so the tier is unconditionally primary.
func PlatformIsolationTier(_ Policy) IsolationTier {
	return IsolationTier{
		Tier:          TierPrimary,
		Backend:       BackendSeatbelt,
		PortFiltering: PortFilteringStrict,
	}
}
