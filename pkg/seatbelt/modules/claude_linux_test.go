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

func TestClaudeAgent_LinuxReadable(t *testing.T) {
	mod := ClaudeAgent()
	provider, ok := mod.(seatbelt.LinuxPathProvider)
	if !ok {
		t.Fatal("ClaudeAgent must implement seatbelt.LinuxPathProvider")
	}
	ctx := &seatbelt.Context{HomeDir: "/home/u"}
	paths := provider.LinuxReadable(ctx)

	wantPaths := []string{
		filepath.Join("/home/u", ".claude.json"),
		filepath.Join("/home/u", ".mcp.json"),
	}
	for _, want := range wantPaths {
		found := false
		for _, p := range paths {
			if p == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("LinuxReadable: expected %q, got %v", want, paths)
		}
	}
}

func TestClaudeAgent_LinuxWritable(t *testing.T) {
	mod := ClaudeAgent()
	provider, ok := mod.(seatbelt.LinuxPathProvider)
	if !ok {
		t.Fatal("ClaudeAgent must implement seatbelt.LinuxPathProvider")
	}
	ctx := &seatbelt.Context{HomeDir: "/home/u"}
	paths := provider.LinuxWritable(ctx)

	if len(paths) == 0 {
		t.Fatal("LinuxWritable: expected at least one path, got none")
	}
	// ~/.claude must be writable so the agent can persist session state.
	claudeDir := filepath.Join("/home/u", ".claude")
	found := false
	for _, p := range paths {
		if p == claudeDir {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("LinuxWritable: expected %q in paths, got %v", claudeDir, paths)
	}
	// No path should appear in both readable and writable (causes EROFS).
	readable := provider.LinuxReadable(ctx)
	rset := make(map[string]bool, len(readable))
	for _, p := range readable {
		rset[p] = true
	}
	for _, p := range paths {
		if rset[p] {
			t.Errorf("LinuxWritable: %q appears in both readable and writable — bwrap backend will leave it read-only", p)
		}
	}
	// Each writable path must be under $HOME.
	for _, p := range paths {
		if !strings.HasPrefix(p, "/home/u") {
			t.Errorf("LinuxWritable: unexpected path outside HOME: %q", p)
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
