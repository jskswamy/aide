//go:build linux

package sandbox

import (
	"os"
	"testing"
)

// TestMain dispatches subprocess helper modes used by the policy-fd
// exec-boundary integration test before any test runs. When the env var
// is unset (the common case), it falls through to the normal test runner.
func TestMain(m *testing.M) {
	dispatchPolicyFDHelper()
	os.Exit(m.Run())
}
