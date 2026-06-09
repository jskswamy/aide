package guards_test

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/jskswamy/aide/pkg/seatbelt"
	"github.com/jskswamy/aide/pkg/seatbelt/guards"
)

// TestGuard_NodeToolchain_PopulatesWritableForLandlock pins the cross-platform
// grant: npm/pnpm/yarn dirs must appear in result.Writable, not only in the
// Seatbelt Rules s-expression. The Linux Landlock backend reads the structured
// Writable field; if the guard populated only Rules, npm install inside the
// sandbox would fail with EACCES on writes to ~/.npm, ~/.cache/npm, etc.
func TestGuard_NodeToolchain_PopulatesWritableForLandlock(t *testing.T) {
	home := t.TempDir()
	g := guards.NodeToolchainGuard()
	result := g.Rules(&seatbelt.Context{HomeDir: home})

	mustBeWritable := []string{
		filepath.Join(home, ".nvm"),
		filepath.Join(home, ".fnm"),
		filepath.Join(home, ".npm"),
		filepath.Join(home, ".cache", "npm"),
		filepath.Join(home, ".config", "pnpm"),
		filepath.Join(home, ".pnpm-store"),
		filepath.Join(home, ".yarn"),
		filepath.Join(home, ".cache", "yarn"),
		filepath.Join(home, ".cache", "node", "corepack"),
		filepath.Join(home, ".cache", "puppeteer"),
		filepath.Join(home, ".cache", "turbo"),
	}
	for _, p := range mustBeWritable {
		if !slices.Contains(result.Writable, p) {
			t.Errorf("expected %q in result.Writable (Landlock write grant); got %v", p, result.Writable)
		}
	}
	// Writable paths must not leak into Readable.
	for _, p := range mustBeWritable {
		if slices.Contains(result.Readable, p) {
			t.Errorf("writable path must not be in Readable: %q", p)
		}
	}
}
