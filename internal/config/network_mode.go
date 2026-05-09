package config

import (
	"fmt"
	"slices"
	"strings"
)

// ValidNetworkModes is the canonical list of accepted Sandbox.Network.Mode
// values. Order matches what is shown to users in error messages and flag
// help text.
var ValidNetworkModes = []string{"outbound", "none", "unrestricted"}

// ValidateNetworkMode reports whether s names a known network mode.
// The error message lists every valid mode so callers can surface it
// directly to users.
func ValidateNetworkMode(s string) error {
	if slices.Contains(ValidNetworkModes, s) {
		return nil
	}
	return fmt.Errorf("invalid network mode %q (must be %s)", s, strings.Join(ValidNetworkModes, ", "))
}
