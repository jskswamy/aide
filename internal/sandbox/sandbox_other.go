//go:build !darwin

package sandbox

// NewSandbox returns a no-op sandbox on unsupported platforms.
func NewSandbox() Sandbox {
	return &noopSandbox{}
}
