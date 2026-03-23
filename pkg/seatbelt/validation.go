package seatbelt

import (
	"fmt"
	"strings"
)

// ValidationResult holds errors and warnings from validation operations.
// Used across all validation sites for consistent error reporting.
type ValidationResult struct {
	Errors   []string
	Warnings []string
}

// AddError adds a formatted error message.
func (r *ValidationResult) AddError(format string, args ...interface{}) {
	r.Errors = append(r.Errors, fmt.Sprintf(format, args...))
}

// AddWarning adds a formatted warning message.
func (r *ValidationResult) AddWarning(format string, args ...interface{}) {
	r.Warnings = append(r.Warnings, fmt.Sprintf(format, args...))
}

// Err returns the first error as an error, or nil if no errors.
func (r *ValidationResult) Err() error {
	if len(r.Errors) == 0 {
		return nil
	}
	return fmt.Errorf("%s", r.Errors[0])
}

// OK returns true if there are no errors.
func (r *ValidationResult) OK() bool {
	return len(r.Errors) == 0
}

// Merge combines another ValidationResult into this one.
func (r *ValidationResult) Merge(other *ValidationResult) {
	if other == nil {
		return
	}
	r.Errors = append(r.Errors, other.Errors...)
	r.Warnings = append(r.Warnings, other.Warnings...)
}

// String returns a human-readable summary of errors and warnings.
func (r *ValidationResult) String() string {
	var parts []string
	for _, e := range r.Errors {
		parts = append(parts, "error: "+e)
	}
	for _, w := range r.Warnings {
		parts = append(parts, "warning: "+w)
	}
	return strings.Join(parts, "; ")
}
