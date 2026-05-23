//go:build linux

package sandbox

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFileForTest(path, content string) error {
	return os.WriteFile(path, []byte(content), 0600)
}

func mkdirForTest(path string) error {
	return os.MkdirAll(path, 0700)
}

func TestNeedsAtomicWriteOverlay_EmptyReturnsFalse(t *testing.T) {
	if needsAtomicWriteOverlay(landlockPolicyJSON{}) {
		t.Error("empty policy should not require atomic-write overlay")
	}
	if needsAtomicWriteOverlay(landlockPolicyJSON{AgentAtomicWritableFiles: nil}) {
		t.Error("nil files should not require atomic-write overlay")
	}
	if needsAtomicWriteOverlay(landlockPolicyJSON{AgentAtomicWritableFiles: []string{"", "  "}}) {
		t.Error("blank-only entries should not require atomic-write overlay")
	}
}

func TestNeedsAtomicWriteOverlay_NonEmptyReturnsTrue(t *testing.T) {
	j := landlockPolicyJSON{
		AgentAtomicWritableFiles: []string{"/home/u/.claude.json"},
	}
	if !needsAtomicWriteOverlay(j) {
		t.Error("policy with atomic-writable file should require overlay")
	}
}

func TestUniqueExistingParents_OnlyExistingDirsReturned(t *testing.T) {
	tmp := t.TempDir()
	files := []string{
		filepath.Join(tmp, "config.json"),
		filepath.Join(tmp, "config2.json"),
		"/nonexistent/dir/file",
		"",
		"  ",
	}
	got := uniqueExistingParents(files)
	if len(got) != 1 {
		t.Fatalf("expected exactly 1 unique existing parent, got %d: %v", len(got), got)
	}
	if got[0] != filepath.Clean(tmp) {
		t.Errorf("expected parent %q, got %q", tmp, got[0])
	}
}

func TestRelUnder_HandlesEdgeCases(t *testing.T) {
	cases := []struct {
		parent, child string
		wantOK        bool
		wantRel       string
	}{
		{"/home/u", "/home/u", false, ""},                          // same path
		{"/home/u", "/home/u/.claude.json", true, ".claude.json"},  // child of home
		{"/home/u", "/home/user", false, ""},                       // prefix-but-not-parent
		{"/home/u", "/etc/passwd", false, ""},                      // unrelated
		{"/home/u", "/home/u/.config/x", true, ".config/x"},        // nested
		{"", "/home/u/x", false, ""},                               // empty parent
		{"/home/u", "", false, ""},                                 // empty child
	}
	for _, c := range cases {
		rel, ok := relUnder(c.parent, c.child)
		if ok != c.wantOK || rel != c.wantRel {
			t.Errorf("relUnder(%q, %q) = %q,%v ; want %q,%v",
				c.parent, c.child, rel, ok, c.wantRel, c.wantOK)
		}
	}
}

func TestSetupOverlayLayout_CreatesDirsAndStubs(t *testing.T) {
	runtimeDir := t.TempDir()
	homeDir := t.TempDir()
	atomic := filepath.Join(homeDir, ".claude.json")
	readable := filepath.Join(homeDir, ".mcp.json")
	if err := writeFileForTest(atomic, "{}"); err != nil {
		t.Fatalf("setup atomic: %v", err)
	}
	if err := writeFileForTest(readable, "{}"); err != nil {
		t.Fatalf("setup readable: %v", err)
	}

	layout, err := setupOverlayLayout(runtimeDir, homeDir, []string{atomic}, []string{readable})
	if err != nil {
		t.Fatalf("setupOverlayLayout: %v", err)
	}
	for _, d := range []string{layout.Root, layout.Lower, layout.Upper, layout.Work} {
		if info, err := os.Stat(d); err != nil || !info.IsDir() {
			t.Errorf("expected directory at %q (err=%v)", d, err)
		}
	}
	// Stubs should exist as regular files (bwrap --bind will mount on top).
	for _, rel := range []string{".claude.json", ".mcp.json"} {
		stub := filepath.Join(layout.Lower, rel)
		if info, err := os.Stat(stub); err != nil || info.IsDir() {
			t.Errorf("expected stub file at %q (err=%v)", stub, err)
		}
	}
}

func TestBuildOverlayBwrapArgs_PopulatesLowerAndMountsOverlay(t *testing.T) {
	runtimeDir := t.TempDir()
	homeDir := t.TempDir()
	atomic := filepath.Join(homeDir, ".claude.json")
	if err := writeFileForTest(atomic, "{}"); err != nil {
		t.Fatalf("setup: %v", err)
	}
	layout, err := setupOverlayLayout(runtimeDir, homeDir, []string{atomic}, nil)
	if err != nil {
		t.Fatalf("setupOverlayLayout: %v", err)
	}

	args := buildOverlayBwrapArgs(layout, homeDir, []string{atomic}, nil, nil, true, NetworkOutbound)
	joined := strings.Join(args, " ")

	// The atomic file must be bind-mounted INTO lower at its relative path —
	// not bound at its host location. That's the overlay's whole point.
	wantBind := "--bind " + atomic + " " + filepath.Join(layout.Lower, ".claude.json")
	if !strings.Contains(joined, wantBind) {
		t.Errorf("expected %q in args, got: %s", wantBind, joined)
	}
	// The overlay must be mounted at $HOME with the synthetic lower.
	wantOverlay := "--overlay " + layout.Upper + " " + layout.Work + " " + homeDir
	if !strings.Contains(joined, wantOverlay) {
		t.Errorf("expected %q in args, got: %s", wantOverlay, joined)
	}
	wantOverlaySrc := "--overlay-src " + layout.Lower
	if !strings.Contains(joined, wantOverlaySrc) {
		t.Errorf("expected %q in args, got: %s", wantOverlaySrc, joined)
	}
	// Atomic file MUST NOT be bound at its host path on top of the overlay —
	// that would re-introduce the EBUSY problem.
	if strings.Contains(joined, "--bind "+atomic+" "+atomic) {
		t.Errorf("atomic file must not be bound at its host path on top of overlay: %s", joined)
	}
}

func TestBuildOverlayBwrapArgs_WritableDirsBoundOnTopOfOverlay(t *testing.T) {
	runtimeDir := t.TempDir()
	homeDir := t.TempDir()
	atomic := filepath.Join(homeDir, ".claude.json")
	if err := writeFileForTest(atomic, "{}"); err != nil {
		t.Fatalf("setup: %v", err)
	}
	writableDir := filepath.Join(homeDir, ".claude")
	if err := mkdirForTest(writableDir); err != nil {
		t.Fatalf("setup: %v", err)
	}
	layout, err := setupOverlayLayout(runtimeDir, homeDir, []string{atomic}, nil)
	if err != nil {
		t.Fatalf("setupOverlayLayout: %v", err)
	}

	args := buildOverlayBwrapArgs(layout, homeDir, []string{atomic}, nil, []string{writableDir}, true, NetworkOutbound)
	joined := strings.Join(args, " ")

	// Writable dirs are bind-mounted at their natural path AFTER the overlay
	// is mounted — so writes go straight to the real fs (no sync-back needed
	// for these). Rename within these dirs is fine because they're directory
	// mounts, not per-file mounts.
	wantWritable := "--bind " + writableDir + " " + writableDir
	if !strings.Contains(joined, wantWritable) {
		t.Errorf("expected writable dir bound on top of overlay: %q in args, got: %s", wantWritable, joined)
	}
}

func TestBuildOverlayBwrapArgs_ReadableFilesGoIntoLower(t *testing.T) {
	runtimeDir := t.TempDir()
	homeDir := t.TempDir()
	atomic := filepath.Join(homeDir, ".claude.json")
	readableFile := filepath.Join(homeDir, ".mcp.json")
	for _, f := range []string{atomic, readableFile} {
		if err := writeFileForTest(f, "{}"); err != nil {
			t.Fatalf("setup %s: %v", f, err)
		}
	}
	layout, err := setupOverlayLayout(runtimeDir, homeDir, []string{atomic}, []string{readableFile})
	if err != nil {
		t.Fatalf("setupOverlayLayout: %v", err)
	}

	args := buildOverlayBwrapArgs(layout, homeDir, []string{atomic}, []string{readableFile}, nil, true, NetworkOutbound)
	joined := strings.Join(args, " ")

	// Readable files: ro-bind into lower (not at host path).
	wantROBind := "--ro-bind " + readableFile + " " + filepath.Join(layout.Lower, ".mcp.json")
	if !strings.Contains(joined, wantROBind) {
		t.Errorf("readable file should be ro-bound into lower, expected %q in: %s", wantROBind, joined)
	}
}

func TestBuildOverlayBwrapArgs_ReadableDirsBoundOnTopOfOverlay(t *testing.T) {
	runtimeDir := t.TempDir()
	homeDir := t.TempDir()
	atomic := filepath.Join(homeDir, ".claude.json")
	if err := writeFileForTest(atomic, "{}"); err != nil {
		t.Fatalf("setup: %v", err)
	}
	readableDir := filepath.Join(homeDir, "docs")
	if err := mkdirForTest(readableDir); err != nil {
		t.Fatalf("setup: %v", err)
	}
	layout, err := setupOverlayLayout(runtimeDir, homeDir, []string{atomic}, []string{readableDir})
	if err != nil {
		t.Fatalf("setupOverlayLayout: %v", err)
	}

	args := buildOverlayBwrapArgs(layout, homeDir, []string{atomic}, []string{readableDir}, nil, true, NetworkOutbound)
	joined := strings.Join(args, " ")

	// Readable dirs are ro-bound at their natural path (post-overlay).
	wantROBind := "--ro-bind " + readableDir + " " + readableDir
	if !strings.Contains(joined, wantROBind) {
		t.Errorf("readable dir should be ro-bound on top of overlay, expected %q in: %s", wantROBind, joined)
	}
}

func TestBuildOverlayBwrapArgs_NonHomePathsAtNaturalLocations(t *testing.T) {
	runtimeDir := t.TempDir()
	homeDir := t.TempDir()
	atomic := filepath.Join(homeDir, ".claude.json")
	if err := writeFileForTest(atomic, "{}"); err != nil {
		t.Fatalf("setup: %v", err)
	}
	// /tmp is a writable path outside $HOME; it should be bound at its
	// natural location, not into the overlay.
	outsideWritable := "/tmp"
	outsideReadable := "/etc"
	layout, err := setupOverlayLayout(runtimeDir, homeDir, []string{atomic}, nil)
	if err != nil {
		t.Fatalf("setupOverlayLayout: %v", err)
	}

	args := buildOverlayBwrapArgs(layout, homeDir, []string{atomic},
		[]string{outsideReadable}, []string{outsideWritable}, true, NetworkOutbound)
	joined := strings.Join(args, " ")

	if !strings.Contains(joined, "--bind "+outsideWritable+" "+outsideWritable) {
		t.Errorf("non-home writable should be bound at natural location: %s", joined)
	}
	if !strings.Contains(joined, "--ro-bind "+outsideReadable+" "+outsideReadable) {
		t.Errorf("non-home readable should be ro-bound at natural location: %s", joined)
	}
}

func TestBuildOverlayBwrapArgs_SkipsNonExistentPaths(t *testing.T) {
	runtimeDir := t.TempDir()
	homeDir := t.TempDir()
	atomic := filepath.Join(homeDir, ".claude.json")
	if err := writeFileForTest(atomic, "{}"); err != nil {
		t.Fatalf("setup: %v", err)
	}
	missing := filepath.Join(homeDir, ".does-not-exist")
	missingDir := filepath.Join(homeDir, "ghost-dir")
	layout, err := setupOverlayLayout(runtimeDir, homeDir, []string{atomic}, nil)
	if err != nil {
		t.Fatalf("setupOverlayLayout: %v", err)
	}

	args := buildOverlayBwrapArgs(layout, homeDir, []string{atomic},
		[]string{missing}, []string{missingDir}, true, NetworkOutbound)
	joined := strings.Join(args, " ")

	if strings.Contains(joined, missing) {
		t.Errorf("missing readable path should be skipped, got: %s", joined)
	}
	if strings.Contains(joined, missingDir) {
		t.Errorf("missing writable path should be skipped, got: %s", joined)
	}
}

func TestBuildOverlayBwrapArgs_UnsharePidAndNetwork(t *testing.T) {
	runtimeDir := t.TempDir()
	homeDir := t.TempDir()
	atomic := filepath.Join(homeDir, ".claude.json")
	if err := writeFileForTest(atomic, "{}"); err != nil {
		t.Fatalf("setup: %v", err)
	}
	layout, err := setupOverlayLayout(runtimeDir, homeDir, []string{atomic}, nil)
	if err != nil {
		t.Fatalf("setupOverlayLayout: %v", err)
	}

	args := buildOverlayBwrapArgs(layout, homeDir, []string{atomic}, nil, nil, false, NetworkNone)
	joined := strings.Join(args, " ")

	if !strings.Contains(joined, "--unshare-pid") {
		t.Errorf("AllowSubprocess=false should include --unshare-pid, got: %s", joined)
	}
	if !strings.Contains(joined, "--unshare-net") {
		t.Errorf("Network=none should include --unshare-net, got: %s", joined)
	}
}

func TestPolicyFromJSON_AddsOverlayedParentsToWritable(t *testing.T) {
	tmp := t.TempDir()
	atomicFile := filepath.Join(tmp, "config.json")
	if err := writeFileForTest(atomicFile, "x"); err != nil {
		t.Fatalf("setup: %v", err)
	}

	j := landlockPolicyJSON{
		AgentAtomicWritableFiles: []string{atomicFile},
	}
	p := policyFromJSON(j)

	found := false
	for _, w := range p.ExtraWritable {
		if w == filepath.Clean(tmp) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected parent dir %q in ExtraWritable so Landlock allows overlay writes, got: %v",
			tmp, p.ExtraWritable)
	}
}
