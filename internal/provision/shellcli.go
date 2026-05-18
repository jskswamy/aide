package provision

import (
	"context"
	"fmt"
	"strings"
)

// DefaultTolerateStderr is the standard set of stderr substrings that
// drivers treat as success (typically for rollback-safety: an op that
// uninstalls/removes an already-absent thing). Drivers can pass extra
// tokens in addition to or in place of this set.
var DefaultTolerateStderr = []string{"not installed", "not found", "not configured"}

// RunCLI is the uniform invocation shape for driver Install/Uninstall/
// AddMarketplace/RemoveMarketplace plumbing. opDesc is the human-
// readable subject inserted into error messages (e.g. "claude plugin
// install foo"). It wraps a runner error as `opDesc: %w` and a
// non-zero exit as `opDesc: exit <n>: <stderr>`, except when stderr
// contains any of the tolerate tokens — in that case it returns nil
// so rollback paths can safely remove already-absent state.
//
// Codex is intentionally not migrated to this helper: it edits TOML
// directly and never shells out.
func RunCLI(ctx context.Context, r Runner, env map[string]string, opDesc string, bin string, args []string, tolerate ...string) error {
	_, stderr, code, err := r.Run(ctx, env, bin, args...)
	if err != nil {
		return fmt.Errorf("%s: %w", opDesc, err)
	}
	if code != 0 {
		for _, tok := range tolerate {
			if tok != "" && strings.Contains(stderr, tok) {
				return nil
			}
		}
		return fmt.Errorf("%s: exit %d: %s", opDesc, code, stderr)
	}
	return nil
}
