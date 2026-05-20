//go:build darwin

package sandbox

// macOS always uses Seatbelt, so the tier is unconditionally primary.
func PlatformIsolationTier(_ Policy) IsolationTier {
	return IsolationTier{
		Tier:          TierPrimary,
		Backend:       BackendSeatbelt,
		PortFiltering: PortFilteringStrict,
	}
}
