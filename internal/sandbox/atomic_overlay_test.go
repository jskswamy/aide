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

func TestPathInside_HandlesEdgeCases(t *testing.T) {
	cases := []struct {
		child, parent string
		want          bool
	}{
		{"/home/u", "/home/u", true},
		{"/home/u/.claude.json", "/home/u", true},
		{"/home/user", "/home/u", false},
		{"/etc/passwd", "/home/u", false},
		{"/home/u/.config/", "/home/u/.config", true},
	}
	for _, c := range cases {
		if got := pathInside(c.child, c.parent); got != c.want {
			t.Errorf("pathInside(%q, %q) = %v, want %v", c.child, c.parent, got, c.want)
		}
	}
}

func TestExpandBinaryPaths_EmptyReturnsNil(t *testing.T) {
	if got := expandBinaryPaths(""); got != nil {
		t.Errorf("expected nil for empty bin, got %v", got)
	}
}

func TestExpandBinaryPaths_IncludesPathAndDir(t *testing.T) {
	got := expandBinaryPaths("/usr/bin/echo")
	want := []string{"/usr/bin/echo", "/usr/bin"}
	if len(got) < len(want) {
		t.Fatalf("expected at least %d entries, got %d: %v", len(want), len(got), got)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("entry %d = %q, want %q", i, got[i], w)
		}
	}
}

func TestBuildAtomicWriteOverlayArgs_BindsAtomicFileAndOverlaysParent(t *testing.T) {
	tmp := t.TempDir()
	atomicFile := filepath.Join(tmp, "config.json")
	if err := writeFileForTest(atomicFile, "x"); err != nil {
		t.Fatalf("setup: %v", err)
	}

	j := landlockPolicyJSON{
		AgentAtomicWritableFiles: []string{atomicFile},
	}
	gps := GrantedPathSet{}
	args := buildAtomicWriteOverlayArgs(j, gps, "", "")

	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--tmpfs "+filepath.Clean(tmp)) {
		t.Errorf("expected --tmpfs over parent %q in args, got: %s", tmp, joined)
	}
	if !strings.Contains(joined, "--bind "+atomicFile+" "+atomicFile) {
		t.Errorf("expected --bind for atomic file %q in args, got: %s", atomicFile, joined)
	}
}

func TestBuildAtomicWriteOverlayArgs_RestoresWritableAndReadableUnderParent(t *testing.T) {
	parent := t.TempDir()
	atomicFile := filepath.Join(parent, "config.json")
	if err := writeFileForTest(atomicFile, "x"); err != nil {
		t.Fatalf("setup: %v", err)
	}

	writableUnderParent := filepath.Join(parent, "subdir-rw")
	if err := mkdirForTest(writableUnderParent); err != nil {
		t.Fatalf("setup: %v", err)
	}
	readableUnderParent := filepath.Join(parent, "subdir-ro")
	if err := mkdirForTest(readableUnderParent); err != nil {
		t.Fatalf("setup: %v", err)
	}
	unrelated := filepath.Join(t.TempDir(), "elsewhere")
	if err := mkdirForTest(unrelated); err != nil {
		t.Fatalf("setup: %v", err)
	}

	j := landlockPolicyJSON{AgentAtomicWritableFiles: []string{atomicFile}}
	gps := GrantedPathSet{
		Writable: []string{writableUnderParent, unrelated},
		Readable: []string{readableUnderParent},
	}

	args := buildAtomicWriteOverlayArgs(j, gps, "", "")
	joined := strings.Join(args, " ")

	if !strings.Contains(joined, "--bind-try "+writableUnderParent+" "+writableUnderParent) {
		t.Errorf("expected --bind-try for writable subpath, got: %s", joined)
	}
	if !strings.Contains(joined, "--ro-bind-try "+readableUnderParent+" "+readableUnderParent) {
		t.Errorf("expected --ro-bind-try for readable subpath, got: %s", joined)
	}
	if strings.Contains(joined, "--bind-try "+unrelated) {
		t.Errorf("unrelated path outside parent should not be re-bound under overlay, got: %s", joined)
	}
}

func TestBuildAtomicWriteOverlayArgs_RestoresAgentBinaryUnderParent(t *testing.T) {
	parent := t.TempDir()
	atomicFile := filepath.Join(parent, "config.json")
	if err := writeFileForTest(atomicFile, "x"); err != nil {
		t.Fatalf("setup: %v", err)
	}

	binDir := filepath.Join(parent, "bin")
	if err := mkdirForTest(binDir); err != nil {
		t.Fatalf("setup: %v", err)
	}
	agentBin := filepath.Join(binDir, "agent")
	if err := writeFileForTest(agentBin, "#!/bin/sh\n"); err != nil {
		t.Fatalf("setup: %v", err)
	}

	j := landlockPolicyJSON{AgentAtomicWritableFiles: []string{atomicFile}}
	gps := GrantedPathSet{}
	args := buildAtomicWriteOverlayArgs(j, gps, "", agentBin)
	joined := strings.Join(args, " ")

	if !strings.Contains(joined, "--ro-bind "+agentBin) {
		t.Errorf("expected agent binary to be restored under overlay, got: %s", joined)
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
		t.Errorf("expected parent dir %q in ExtraWritable so Landlock allows tmpfs writes, got: %v",
			tmp, p.ExtraWritable)
	}
}
