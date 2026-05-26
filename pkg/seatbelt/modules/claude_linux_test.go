//go:build linux

package modules

import (
	"path/filepath"
	"testing"

	"github.com/jskswamy/aide/pkg/seatbelt"
)

func TestAugmentLinuxPaths_GrantsClaudeStateDirs(t *testing.T) {
	home := t.TempDir()
	ctx := &seatbelt.Context{HomeDir: home}
	result := &seatbelt.GuardResult{}

	augmentLinuxPaths(ctx, result)

	want := []string{
		filepath.Join(home, ".claude"),
		filepath.Join(home, ".claude.json"),
		filepath.Join(home, ".cache", "claude"),
		filepath.Join(home, ".local", "state", "claude"),
		filepath.Join(home, ".local", "share", "claude"),
	}
	for _, p := range want {
		if !contains(result.Writable, p) {
			t.Errorf("Writable missing %q; got %v", p, result.Writable)
		}
	}
	if !contains(result.Readable, filepath.Join(home, ".mcp.json")) {
		t.Errorf("Readable missing ~/.mcp.json; got %v", result.Readable)
	}
}

func TestAugmentLinuxPaths_NilContextIsNoOp(t *testing.T) {
	result := &seatbelt.GuardResult{}
	augmentLinuxPaths(nil, result)
	if len(result.Writable) != 0 || len(result.Readable) != 0 {
		t.Errorf("nil ctx must be a no-op; got writable=%v readable=%v", result.Writable, result.Readable)
	}
}

func TestAugmentLinuxPaths_EmptyHomeDirIsNoOp(t *testing.T) {
	ctx := &seatbelt.Context{HomeDir: ""}
	result := &seatbelt.GuardResult{}
	augmentLinuxPaths(ctx, result)
	if len(result.Writable) != 0 || len(result.Readable) != 0 {
		t.Errorf("empty HomeDir must be a no-op; got writable=%v readable=%v", result.Writable, result.Readable)
	}
}

func contains(list []string, target string) bool {
	for _, p := range list {
		if p == target {
			return true
		}
	}
	return false
}
