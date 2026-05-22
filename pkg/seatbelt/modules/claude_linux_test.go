//go:build linux

package modules

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/jskswamy/aide/pkg/seatbelt"
)

func TestClaudeAgent_LinuxAtomicWritableFiles_DeclaresClaudeJSON(t *testing.T) {
	mod := ClaudeAgent()
	provider, ok := mod.(seatbelt.LinuxAtomicWriteProvider)
	if !ok {
		t.Fatal("ClaudeAgent must implement seatbelt.LinuxAtomicWriteProvider so the Linux backend can isolate ~/.claude.json's parent dir without exposing the rest of $HOME")
	}
	ctx := &seatbelt.Context{HomeDir: "/home/u"}
	files := provider.LinuxAtomicWritableFiles(ctx)
	want := "/home/u/.claude.json"
	found := false
	for _, f := range files {
		if f == want {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected %q in atomic-writable files, got %v", want, files)
	}
}

// TestClaudeAgent_LinuxReadablePathsInRules asserts the module's Linux readable
// paths are returned via GuardResult.Readable (single-pipeline contract; no
// separate LinuxPathProvider interface).
func TestClaudeAgent_LinuxReadablePathsInRules(t *testing.T) {
	mod := ClaudeAgent()
	ctx := &seatbelt.Context{HomeDir: "/home/u", GOOS: "linux"}
	result := mod.Rules(ctx)

	// ~/.claude.json must NOT appear — it is in LinuxAtomicWritableFiles and
	// the overlay's --bind already grants read+write; a --ro-bind-try here
	// would produce undefined mount-stacking behavior.
	for _, p := range result.Readable {
		if p == filepath.Join("/home/u", ".claude.json") {
			t.Errorf("Readable: ~/.claude.json must not appear (it is in LinuxAtomicWritableFiles)")
		}
	}
	// ~/.mcp.json must be readable.
	mcpJSON := filepath.Join("/home/u", ".mcp.json")
	found := false
	for _, p := range result.Readable {
		if p == mcpJSON {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Readable: expected %q, got %v", mcpJSON, result.Readable)
	}
}

// TestClaudeAgent_LinuxWritablePathsInRules asserts the module's Linux writable
// paths are returned via GuardResult.Writable.
func TestClaudeAgent_LinuxWritablePathsInRules(t *testing.T) {
	mod := ClaudeAgent()
	ctx := &seatbelt.Context{HomeDir: "/home/u", GOOS: "linux"}
	result := mod.Rules(ctx)

	if len(result.Writable) == 0 {
		t.Fatal("Writable: expected at least one path, got none")
	}
	// ~/.claude must be writable so the agent can persist session state.
	claudeDir := filepath.Join("/home/u", ".claude")
	found := false
	for _, p := range result.Writable {
		if p == claudeDir {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Writable: expected %q in paths, got %v", claudeDir, result.Writable)
	}
	// No path should appear in both Readable and Writable (causes EROFS in
	// the bwrap backend).
	rset := make(map[string]bool, len(result.Readable))
	for _, p := range result.Readable {
		rset[p] = true
	}
	for _, p := range result.Writable {
		if rset[p] {
			t.Errorf("%q appears in both Readable and Writable — bwrap backend will leave it read-only", p)
		}
	}
	// Each writable path must be under $HOME.
	for _, p := range result.Writable {
		if !strings.HasPrefix(p, "/home/u") {
			t.Errorf("Writable: unexpected path outside HOME: %q", p)
		}
	}
}

// TestClaudeAgent_AtomicFilesNotInReadableOrWritable verifies the exclusivity
// invariant — overlay would emit conflicting bwrap binds if a path appeared
// in both LinuxAtomicWritableFiles and the GuardResult path sets.
func TestClaudeAgent_AtomicFilesNotInReadableOrWritable(t *testing.T) {
	mod := ClaudeAgent()
	ap, ok := mod.(seatbelt.LinuxAtomicWriteProvider)
	if !ok {
		t.Fatal("ClaudeAgent must implement seatbelt.LinuxAtomicWriteProvider")
	}
	ctx := &seatbelt.Context{HomeDir: "/home/u", GOOS: "linux"}

	result := mod.Rules(ctx)
	atomic := ap.LinuxAtomicWritableFiles(ctx)
	atomicSet := make(map[string]bool, len(atomic))
	for _, f := range atomic {
		atomicSet[f] = true
	}
	for _, p := range result.Readable {
		if atomicSet[p] {
			t.Errorf("%q appears in both Readable and LinuxAtomicWritableFiles — overlay will emit conflicting bwrap binds", p)
		}
	}
	for _, p := range result.Writable {
		if atomicSet[p] {
			t.Errorf("%q appears in both Writable and LinuxAtomicWritableFiles — overlay will emit conflicting bwrap binds", p)
		}
	}
}

func TestClaudeAgent_LinuxAtomicWritableFiles_NilContext(t *testing.T) {
	mod := ClaudeAgent()
	provider, ok := mod.(seatbelt.LinuxAtomicWriteProvider)
	if !ok {
		t.Fatal("ClaudeAgent must implement seatbelt.LinuxAtomicWriteProvider")
	}
	if got := provider.LinuxAtomicWritableFiles(nil); got != nil {
		t.Errorf("expected nil for nil context, got %v", got)
	}
	if got := provider.LinuxAtomicWritableFiles(&seatbelt.Context{HomeDir: ""}); got != nil {
		t.Errorf("expected nil for empty HomeDir, got %v", got)
	}
}
