package guards_test

import (
	"strings"
	"testing"

	"github.com/jskswamy/aide/pkg/seatbelt"
	"github.com/jskswamy/aide/pkg/seatbelt/guards"
)

// TestGuardRobustness_NilContext verifies every guard handles nil context
// without panicking. Table-driven across all registered guards.
func TestGuardRobustness_NilContext(t *testing.T) {
	for _, g := range guards.AllGuards() {
		t.Run(g.Name(), func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("guard %q panicked on nil context: %v", g.Name(), r)
				}
			}()
			result := g.Rules(nil)
			_ = result
		})
	}
}

// TestGuardRobustness_EmptyHomeDir verifies every guard handles empty
// HomeDir without producing relative paths or panicking.
func TestGuardRobustness_EmptyHomeDir(t *testing.T) {
	for _, g := range guards.AllGuards() {
		t.Run(g.Name(), func(t *testing.T) {
			ctx := &seatbelt.Context{
				HomeDir:     "",
				ProjectRoot: "/project",
				GOOS:        "darwin",
			}
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("guard %q panicked on empty HomeDir: %v", g.Name(), r)
				}
			}()
			result := g.Rules(ctx)
			output := renderTestRules(result.Rules)

			// Guards that don't emit file paths, or emit only regex/non-home
			// paths, can skip path checks.
			if g.Name() == "base" || g.Name() == "network" || g.Name() == "system-runtime" {
				return
			}

			// No rule should contain a relative path
			for _, line := range strings.Split(output, "\n") {
				line = strings.TrimSpace(line)
				if !strings.Contains(line, `"`) {
					continue
				}
				// Extract quoted paths and check for relative ones
				for _, part := range strings.Split(line, `"`) {
					if len(part) <= 2 || strings.HasPrefix(part, "/") ||
						strings.HasPrefix(part, "(") ||
						strings.HasPrefix(part, ")") ||
						strings.HasPrefix(part, "*") ||
						strings.Contains(part, "apple") ||
						strings.Contains(part, "com.") ||
						strings.Contains(part, " ") {
						continue
					}
					// Looks like a relative file path
					if strings.Contains(part, ".") || strings.Contains(part, "/") {
						t.Errorf("guard %q emitted relative path with empty HomeDir: %q",
							g.Name(), part)
					}
				}
			}
		})
	}
}
