//go:build !darwin && !linux

package sandbox

// NewSandbox returns a no-op sandbox on unsupported platforms.
func NewSandbox() Sandbox {
	return &noopSandbox{}
}

// PlatformGrantedPaths falls back to DeriveGrantedPathSet on platforms that
// have no OS-specific bootstrap paths to add.
func PlatformGrantedPaths(policy Policy) GrantedPathSet {
	return DeriveGrantedPathSet(policy)
}
