//go:build !darwin

package diag

import (
	"errors"
	"time"
)

// CollectDenials is a stub on non-Darwin. The trace path relies on
// macOS's `log show` command which is not portable.
func CollectDenials(int, time.Duration) (string, []Denial, error) {
	return "trace mode is macOS-only in v1", nil, errors.New("unsupported")
}
