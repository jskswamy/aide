package guards_test

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/jskswamy/aide/pkg/seatbelt"
	"github.com/jskswamy/aide/pkg/seatbelt/guards"
)

// nixStoreOverride sets TestNixStoreDir to a fake store dir for the duration
// of the test and restores it on cleanup.
func nixStoreOverride(t *testing.T, dir string) {
	t.Helper()
	orig := *guards.TestNixStoreDir
	*guards.TestNixStoreDir = dir
	t.Cleanup(func() { *guards.TestNixStoreDir = orig })
}

// TestGuard_NixToolchain_PopulatesWritableAndReadableForLandlock pins the
// cross-platform grant split: read+write nix user dirs must be in Writable,
// and channel/config dirs must be in Readable (not Writable). Both must be
// absent from the other slice.
func TestGuard_NixToolchain_PopulatesWritableAndReadableForLandlock(t *testing.T) {
	nixStoreOverride(t, t.TempDir())

	home := t.TempDir()
	g := guards.NixToolchainGuard()
	result := g.Rules(&seatbelt.Context{HomeDir: home})

	mustBeWritable := []string{
		filepath.Join(home, ".nix-profile"),
		filepath.Join(home, ".local", "state", "nix"),
		filepath.Join(home, ".cache", "nix"),
	}
	for _, p := range mustBeWritable {
		if !slices.Contains(result.Writable, p) {
			t.Errorf("expected %q in result.Writable; got %v", p, result.Writable)
		}
		if slices.Contains(result.Readable, p) {
			t.Errorf("writable path must not be in Readable: %q", p)
		}
	}

	mustBeReadable := []string{
		filepath.Join(home, ".nix-defexpr"),
		filepath.Join(home, ".config", "nix"),
	}
	for _, p := range mustBeReadable {
		if !slices.Contains(result.Readable, p) {
			t.Errorf("expected %q in result.Readable; got %v", p, result.Readable)
		}
		if slices.Contains(result.Writable, p) {
			t.Errorf("read-only path must not be in Writable: %q", p)
		}
	}
}

// TestGuard_NixToolchain_SkippedWhenNixNotInstalled verifies the guard returns
// a Skipped entry and no rules when the nix store is absent.
func TestGuard_NixToolchain_SkippedWhenNixNotInstalled(t *testing.T) {
	nixStoreOverride(t, "/nonexistent/nix/store")

	g := guards.NixToolchainGuard()
	result := g.Rules(&seatbelt.Context{HomeDir: t.TempDir()})

	if len(result.Rules) != 0 {
		t.Error("expected no Rules when nix store absent")
	}
	if len(result.Skipped) == 0 {
		t.Error("expected Skipped entry when nix store absent")
	}
	if len(result.Writable) != 0 || len(result.Readable) != 0 {
		t.Error("expected no Writable/Readable when nix not installed")
	}
}

// TestGuard_NixToolchain_RulesContent verifies the Seatbelt rule content when
// nix is present: daemon socket, read-write user paths, read-only config paths.
func TestGuard_NixToolchain_RulesContent(t *testing.T) {
	nixStoreOverride(t, t.TempDir())

	home := t.TempDir()
	g := guards.NixToolchainGuard()
	result := g.Rules(&seatbelt.Context{HomeDir: home})

	if len(result.Rules) == 0 {
		t.Fatal("expected rules when nix store present")
	}
	output := renderTestRules(result.Rules)
	for _, want := range []string{
		"network-outbound",
		"/nix/var/nix/daemon-socket/socket",
		".nix-profile",
		".local/state/nix",
		".cache/nix",
		".nix-defexpr",
		".config/nix",
		"file-read* file-write*",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("expected rules to contain %q", want)
		}
	}
}
