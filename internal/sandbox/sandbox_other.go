//go:build !darwin && !linux

package sandbox

// NewSandbox returns a no-op sandbox on unsupported platforms.
func NewSandbox() Sandbox {
	return &noopSandbox{}
}
