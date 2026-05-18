package provision

import (
	"errors"
	"fmt"
	"strings"
)

// Journal records inverse-op closures during a sync. On Rollback the
// closures run in reverse insertion order. Inverse failures don't stop
// rollback — they are collected and surfaced as one aggregate error.
type Journal struct {
	inverses []func() error
}

// Record appends an inverse closure. Call this AFTER the forward op
// succeeds so a failing forward op does not record a phantom inverse.
func (j *Journal) Record(inverse func() error) {
	j.inverses = append(j.inverses, inverse)
}

// Rollback runs every recorded inverse in reverse order. Returns nil
// if all succeeded, otherwise a wrapped aggregate error listing each
// inverse failure.
func (j *Journal) Rollback() error {
	if len(j.inverses) == 0 {
		return nil
	}
	var failures []string
	for i := len(j.inverses) - 1; i >= 0; i-- {
		if err := j.inverses[i](); err != nil {
			failures = append(failures, fmt.Sprintf("inverse[%d]: %v", i, err))
		}
	}
	if len(failures) == 0 {
		return nil
	}
	return errors.New("rollback partial: " + strings.Join(failures, "; "))
}

// Len returns the number of recorded inverses (for plan-output messages).
func (j *Journal) Len() int { return len(j.inverses) }
